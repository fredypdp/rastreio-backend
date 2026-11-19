package apoio

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/projections"
	"spuri/internal/utils"
)

// Reprovação por faltas: quando a academia definiu o limite de faltas e ligou a
// reprovação por faltas, ao ultrapassar o limite (total > limite) numa matéria e
// período, SÓ a nota abaixo é lida como 0 no cálculo automático da avaliação final:
//   - ensino escolar (fundamental e médio): a nota do professor do período;
//   - ensino superior: o exame final.
//
// As demais notas não são afetadas. Para mudar qual nota é zerada, altere só estas
// duas constantes.
const (
	categoriaZeroPorFaltasEscolar  = "nota_professor"
	categoriaZeroPorFaltasSuperior = "exame_final"
)

func CategoriaZeroPorFaltas(tipoEnsino string) string {
	if tipoEnsino == "superior" {
		return categoriaZeroPorFaltasSuperior
	}
	return categoriaZeroPorFaltasEscolar
}

// aplicarZeroPorFaltas zera, em notas, cada referência (categoria, período) da
// fórmula cuja categoria seja a indicada e cujo total de faltas no período
// ultrapasse o limite. Devolve o que foi zerado (uma entrada por período).
// Períodos que a fórmula não referencia não geram nada.
func AplicarZeroPorFaltas(formula string, notas map[string]map[string][]float64, categoria string, totaisPorPeriodo map[string]int, limite int) ([]aggregates.NotaZeradaPorFaltas, error) {
	refs, err := referenciasFormulaAvaliacao(formula)
	if err != nil {
		return nil, fmt.Errorf("zero por faltas: %w", err)
	}
	var zeradas []aggregates.NotaZeradaPorFaltas
	for _, ref := range refs {
		if ref.Categoria != categoria {
			continue
		}
		total, ok := totaisPorPeriodo[ref.Periodo]
		if !ok || total <= limite {
			continue
		}
		if notas[ref.Categoria] == nil {
			notas[ref.Categoria] = map[string][]float64{}
		}
		notas[ref.Categoria][ref.Periodo] = []float64{0}
		zeradas = append(zeradas, aggregates.NotaZeradaPorFaltas{
			Categoria:    ref.Categoria,
			Periodo:      ref.Periodo,
			TotalFaltas:  total,
			LimiteFaltas: limite,
		})
	}
	return zeradas, nil
}

type NotaFormulaOverlay struct {
	MateriaID string
	Categoria string
	Periodo   string
	Nota      float64
}

type resultadoAvaliacaoAutomatica struct {
	Aprovado bool
}

func TentarAvaliacoesFinaisAutomaticas(
	c *gin.Context,
	estudante *aggregates.Estudante,
	estudanteDTO *projections.EstudanteDTO,
	codigoAcademia string,
	anoLectivo string,
	tipoEnsino string,
	anoAcademicoAtual string,
	categoriaAlterada string,
	overlay *NotaFormulaOverlay,
) ([]gin.H, error) {
	if tipoEnsino == "superior" {
		var cursoSuperiorUUID *uuid.UUID
		if estudanteDTO.CursoSuperiorID != nil {
			if parsed, err := uuid.Parse(*estudanteDTO.CursoSuperiorID); err == nil {
				cursoSuperiorUUID = &parsed
			}
		}
		periodoAtual, err := PeriodoSuperiorAtual(c, estudanteDTO, cursoSuperiorUUID)
		if err != nil {
			return nil, err
		}
		anoAcademicoAtual = periodoAtual
	}

	// O gatilho olha para a cadeia completa aplicável ao estudante. Regras raiz
	// são despertadas pela nota_despertadora da própria raiz; regras descendentes
	// fixas escolares (ex.: exame_recurso) também podem ser despertadas
	// diretamente pela própria categoria, desde que exista reprovação anterior.
	regras, err := ListarRegrasAvaliacaoFinalAplicaveis(c, codigoAcademia, tipoEnsino, anoAcademicoAtual, nil, cursoIDEstudantePorNivel(tipoEnsino, estudanteDTO))
	if err != nil {
		return nil, err
	}
	if len(regras) == 0 {
		return nil, nil
	}
	if err := validarCadeiaAvaliacaoFinalAplicavel(regras, codigoAcademia, tipoEnsino, anoAcademicoAtual); err != nil {
		return nil, err
	}
	var raiz *RegraAvaliacaoFinalDTO
	for i := range regras {
		if regras[i].AplicaSeReprovadoEmType == nil || strings.TrimSpace(*regras[i].AplicaSeReprovadoEmType) == "" {
			raiz = &regras[i]
			break
		}
	}
	categoriaAlterada = strings.TrimSpace(categoriaAlterada)
	periodoAlterado := ""
	if overlay != nil {
		periodoAlterado = overlay.Periodo
	}
	tiposDisparados := TiposAvaliacaoFinalDespertadosPorCategoria(regras, raiz, categoriaAlterada, periodoAlterado)
	if len(tiposDisparados) == 0 {
		return nil, nil
	}
	if overlay != nil {
		regraAplicavel := RegraAvaliacaoFinalDTO{}
		for _, regra := range regras {
			if tiposDisparados[regra.Type] {
				regraAplicavel = regra
				break
			}
		}
		aplicavel, err := materiaAlteradaAplicavelARegra(c, codigoAcademia, tipoEnsino, anoAcademicoAtual, regraAplicavel, estudanteDTO, overlay.MateriaID)
		if err != nil {
			return nil, err
		}
		if !aplicavel {
			return nil, nil
		}
	}

	avaliacaoProj := GetAvaliacaoFinalProjection(c)
	resultados := make([]gin.H, 0, len(regras))
	resultadosPorType := map[string]resultadoAvaliacaoAutomatica{}
	processadas := map[string]bool{}

	for {
		avancou := false
		for _, regra := range regras {
			if processadas[regra.Type] {
				continue
			}
			if !tiposDisparados[regra.Type] {
				continue
			}
			podeExecutar, encerrar, err := regraPodeExecutarAutomaticamente(
				avaliacaoProj,
				estudanteDTO.CodigoEstudante,
				codigoAcademia,
				anoLectivo,
				tipoEnsino,
				anoAcademicoAtual,
				regra,
				resultadosPorType,
			)
			if err != nil {
				return resultados, err
			}
			if encerrar {
				processadas[regra.Type] = true
				avancou = true
				continue
			}
			if !podeExecutar {
				continue
			}

			resultado, registrado, err := executarRegraAvaliacaoFinalAutomatica(
				c,
				estudante,
				estudanteDTO,
				codigoAcademia,
				anoLectivo,
				tipoEnsino,
				anoAcademicoAtual,
				regra,
				overlay,
			)
			if err != nil {
				return resultados, err
			}
			if !registrado {
				continue
			}

			resultados = append(resultados, resultado)
			if aprovado, ok := resultado["aprovado"].(bool); ok {
				resultadosPorType[regra.Type] = resultadoAvaliacaoAutomatica{Aprovado: aprovado}
			}
			processadas[regra.Type] = true
			avancou = true
		}
		if !avancou {
			break
		}
	}

	return resultados, nil
}

func TiposAvaliacaoFinalDespertadosPorCategoria(regras []RegraAvaliacaoFinalDTO, raiz *RegraAvaliacaoFinalDTO, categoriaAlterada, periodoAlterado string) map[string]bool {
	categoriaAlterada = strings.TrimSpace(categoriaAlterada)
	periodoAlterado = strings.TrimSpace(periodoAlterado)
	tiposDisparados := map[string]bool{}
	if regraDespertadaPorNota(raiz, categoriaAlterada, periodoAlterado) {
		tiposDisparados[raiz.Type] = true
		return tiposDisparados
	}
	for i := range regras {
		regra := &regras[i]
		if regra.AplicaSeReprovadoEmType != nil && regraDespertadaPorNota(regra, categoriaAlterada, periodoAlterado) {
			tiposDisparados[regra.Type] = true
		}
	}
	return tiposDisparados
}

func regraDespertadaPorNota(regra *RegraAvaliacaoFinalDTO, categoriaAlterada, periodoAlterado string) bool {
	if regra == nil || regra.NotaDespertadora == nil || categoriaAlterada != strings.TrimSpace(*regra.NotaDespertadora) {
		return false
	}
	periodoFechamento, err := periodoFechamentoCategoriaNaFormula(regra.Formula, categoriaAlterada)
	if err != nil || periodoFechamento == "" {
		return true
	}
	if periodoAlterado == "" {
		return false
	}
	return periodoAlterado == periodoFechamento
}

func periodoFechamentoCategoriaNaFormula(formula, categoria string) (string, error) {
	refs, err := referenciasFormulaAvaliacao(formula)
	if err != nil {
		return "", err
	}
	periodoFechamento := ""
	maiorOrdem := -1
	for _, ref := range refs {
		if strings.TrimSpace(ref.Categoria) != categoria {
			continue
		}
		periodo := strings.TrimSpace(ref.Periodo)
		ordem := ordemPeriodoAvaliacao(periodo)
		if ordem > maiorOrdem {
			maiorOrdem = ordem
			periodoFechamento = periodo
		}
	}
	return periodoFechamento, nil
}

func ordemPeriodoAvaliacao(periodo string) int {
	periodo = strings.TrimSpace(periodo)
	if periodo == "" {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(periodo, "%d_trimestre", &n); err == nil {
		return n
	}
	if _, err := fmt.Sscanf(periodo, "%d_semestre", &n); err == nil {
		return n
	}
	return 1
}

func cursoIDEstudantePorNivel(tipoEnsino string, estudanteDTO *projections.EstudanteDTO) *string {
	if estudanteDTO == nil {
		return nil
	}
	if tipoEnsino == "medio" {
		return estudanteDTO.CursoMedioID
	}
	if tipoEnsino == "superior" {
		return estudanteDTO.CursoSuperiorID
	}
	return nil
}

func materiaAlteradaAplicavelARegra(c *gin.Context, codigoAcademia, tipoEnsino, anoAcademicoAtual string, regra RegraAvaliacaoFinalDTO, estudanteDTO *projections.EstudanteDTO, materiaID string) (bool, error) {
	materiaID = strings.TrimSpace(materiaID)
	if materiaID == "" {
		return false, nil
	}
	materias, err := materiasAplicaveisAvaliacaoFinal(c, codigoAcademia, tipoEnsino, anoAcademicoAtual, regra, estudanteDTO)
	if err != nil {
		return false, err
	}
	for _, materia := range materias {
		if materia.ID.String() == materiaID {
			return true, nil
		}
	}
	return false, nil
}

func regraPodeExecutarAutomaticamente(
	avaliacaoProj *projections.AvaliacaoFinalProjection,
	codigoEstudante string,
	codigoAcademia string,
	anoLectivo string,
	tipoEnsino string,
	anoAcademicoAtual string,
	regra RegraAvaliacaoFinalDTO,
	resultadosPorType map[string]resultadoAvaliacaoAutomatica,
) (podeExecutar bool, encerrar bool, err error) {
	jaAvaliado, err := avaliacaoProj.ExistsByEstudanteAnoLetivoNivelType(
		codigoEstudante,
		codigoAcademia,
		anoLectivo,
		tipoEnsino,
		anoAcademicoAtual,
		regra.Type,
	)
	if err != nil {
		return false, false, fmt.Errorf("erro ao verificar avaliação final existente: %w", err)
	}
	if jaAvaliado {
		return false, true, nil
	}

	if regra.AplicaSeReprovadoEmType == nil || strings.TrimSpace(*regra.AplicaSeReprovadoEmType) == "" {
		return true, false, nil
	}

	dependencia := strings.TrimSpace(*regra.AplicaSeReprovadoEmType)
	if resultado, ok := resultadosPorType[dependencia]; ok {
		if resultado.Aprovado {
			return false, true, nil
		}
		return true, false, nil
	}

	prev, err := avaliacaoProj.GetResultadoByType(codigoEstudante, codigoAcademia, anoLectivo, tipoEnsino, anoAcademicoAtual, dependencia)
	if err != nil || prev == nil {
		return false, false, nil
	}
	if prev.Aprovado {
		return false, true, nil
	}
	return true, false, nil
}

func executarRegraAvaliacaoFinalAutomatica(
	c *gin.Context,
	estudante *aggregates.Estudante,
	estudanteDTO *projections.EstudanteDTO,
	codigoAcademia string,
	anoLectivo string,
	tipoEnsino string,
	anoAcademicoAtual string,
	regra RegraAvaliacaoFinalDTO,
	overlay *NotaFormulaOverlay,
) (gin.H, bool, error) {
	formulaExecucao := regra.Formula
	materiasAplicaveis, err := materiasAplicaveisAvaliacaoFinal(c, codigoAcademia, tipoEnsino, anoAcademicoAtual, regra, estudanteDTO)
	if err != nil {
		return nil, false, err
	}
	if len(materiasAplicaveis) == 0 {
		return nil, false, fmt.Errorf("nenhuma matéria aplicável encontrada para avaliação final")
	}
	resultadosMaterias, notaFinal, aprovado, aprovadoComPendencia, pendenciasGeradas, err := CalcularResultadoMateriasAvaliacaoFinal(
		c, estudanteDTO.CodigoEstudante, codigoAcademia, anoLectivo, tipoEnsino, anoAcademicoAtual, regra, materiasAplicaveis, overlay,
	)
	if err != nil {
		return nil, false, err
	}

	var cursoMedioUUID, cursoSuperiorUUID *uuid.UUID
	if estudanteDTO.CursoMedioID != nil {
		if parsed, err := uuid.Parse(*estudanteDTO.CursoMedioID); err == nil {
			cursoMedioUUID = &parsed
		}
	}
	if estudanteDTO.CursoSuperiorID != nil {
		if parsed, err := uuid.Parse(*estudanteDTO.CursoSuperiorID); err == nil {
			cursoSuperiorUUID = &parsed
		}
	}

	if err := ValidarNivelAtualDoEstudante(estudanteDTO, tipoEnsino, anoAcademicoAtual); err != nil {
		return nil, false, err
	}

	var proximoAnoAcademico *string
	var semestreAvaliado, proximoSemestre *int
	var anoSuperiorAntes, anoSuperiorDepois *string
	var motivoProgressao *string
	switch tipoEnsino {
	case "fundamental":
		proximoAnoAcademico, err = CalcularProximoAnoFundamental(anoAcademicoAtual, aprovado)
		if err == nil {
			academiaDTO, academiaErr := GetAcademiaProjection(c).GetByCodigo(codigoAcademia)
			if academiaErr != nil {
				err = academiaErr
			} else if academiaDTO != nil {
				motivoProgressao = MotivoProgressaoFundamentalSemOferta(aprovado, proximoAnoAcademico, academiaDTO.AnosAcademicos)
			}
		}
	case "medio":
		proximoAnoAcademico, err = CalcularProximoAnoCurso(c, cursoMedioUUID, anoAcademicoAtual, aprovado)
	case "superior":
		proximoSemestre, anoSuperiorDepois, err = CalcularProximoSemestreCurso(c, cursoSuperiorUUID, estudanteDTO.SemestreAtual, aprovado)
		semestreAvaliado = estudanteDTO.SemestreAtual
		anoSuperiorAntes = estudanteDTO.AnoSuperior
		proximoAnoAcademico = anoSuperiorDepois
	}
	if err != nil {
		return nil, false, err
	}

	turmasAtuais := BuscarTurmasDoEstudante(c, estudanteDTO.CodigoEstudante, codigoAcademia)
	var turmaAtual *string
	if len(turmasAtuais) > 0 {
		turmaAtual = &turmasAtuais[0]
	}

	if err := estudante.RegistrarAvaliacaoFinal(
		codigoAcademia,
		anoLectivo,
		tipoEnsino,
		anoAcademicoAtual,
		proximoAnoAcademico,
		turmaAtual,
		turmasAtuais,
		aprovado,
		nil,
		regra.Type,
		notaFinal,
		regra.NotaMinimaAprovacao,
		&regra.ID,
		formulaExecucao,
		regra.AplicaSeReprovadoEmType,
		CursoSnapshotAvaliacaoFinal(tipoEnsino, cursoMedioUUID, cursoSuperiorUUID),
		motivoProgressao,
		resultadosMaterias,
		aprovadoComPendencia,
		pendenciasGeradas,
		aggregates.AvaliacaoFinalSuperiorProgressao{
			SemestreAtualAvaliado: semestreAvaliado,
			ProximoSemestreAtual:  proximoSemestre,
			AnoSuperiorAntes:      anoSuperiorAntes,
			AnoSuperiorDepois:     anoSuperiorDepois,
		},
	); err != nil {
		if strings.Contains(err.Error(), "já registrada") {
			return nil, false, nil
		}
		return nil, false, err
	}

	resultado := gin.H{
		"type":                   regra.Type,
		"aprovado":               aprovado,
		"nota_final":             notaFinal,
		"nota_minima_aprovacao":  regra.NotaMinimaAprovacao,
		"proximo_ano_academico":  proximoAnoAcademico,
		"resultados_materias":    resultadosMaterias,
		"aprovado_com_pendencia": aprovadoComPendencia,
		"pendencias_geradas":     pendenciasGeradas,
	}
	if regra.NotaDespertadora != nil && strings.TrimSpace(*regra.NotaDespertadora) != "" {
		resultado["nota_despertadora"] = strings.TrimSpace(*regra.NotaDespertadora)
	}
	if overlay != nil {
		resultado["categoria_despertada"] = overlay.Categoria
		resultado["materia_despertadora_id"] = overlay.MateriaID
	}
	if motivoProgressao != nil {
		resultado["motivo_progressao"] = *motivoProgressao
		resultado["sem_oferta_do_proximo_ano_academico_na_academia"] = true
	}
	return resultado, true, nil
}

func CursoSnapshotAvaliacaoFinal(tipoEnsino string, cursoMedioUUID, cursoSuperiorUUID *uuid.UUID) *uuid.UUID {
	if tipoEnsino == "medio" {
		return cursoMedioUUID
	}
	if tipoEnsino == "superior" {
		return cursoSuperiorUUID
	}
	return nil
}

func materiasAplicaveisAvaliacaoFinal(c *gin.Context, codigoAcademia, tipoEnsino, anoAcademicoAtual string, regra RegraAvaliacaoFinalDTO, estudanteDTO *projections.EstudanteDTO) ([]projections.MateriaDTO, error) {
	todas, err := GetMateriasProjection(c).GetByAcademia(codigoAcademia)
	if err != nil {
		return nil, err
	}
	idsFiltro := map[string]bool{}
	for _, id := range regra.MateriasAplicaveis {
		idsFiltro[id.String()] = true
	}
	var cursoID *string
	if tipoEnsino == "medio" {
		cursoID = estudanteDTO.CursoMedioID
	} else if tipoEnsino == "superior" {
		cursoID = estudanteDTO.CursoSuperiorID
	}
	out := []projections.MateriaDTO{}
	for _, m := range todas {
		if m.Status != "ativo" || m.Type != tipoEnsino {
			continue
		}
		if len(idsFiltro) > 0 && !idsFiltro[m.ID.String()] {
			continue
		}
		if tipoEnsino == "fundamental" {
			if ContainsString(m.AnosAcademicos, anoAcademicoAtual) {
				out = append(out, m)
			}
			continue
		}
		if cursoID == nil || m.CursoID == nil || m.CursoID.String() != *cursoID {
			continue
		}
		if tipoEnsino == "medio" && ContainsString(m.AnosAcademicos, anoAcademicoAtual) {
			out = append(out, m)
		}
		if tipoEnsino == "superior" && m.Periodo != nil && *m.Periodo == anoAcademicoAtual {
			out = append(out, m)
		}
	}
	return out, nil
}

func CalcularResultadoMateriasAvaliacaoFinal(
	c *gin.Context,
	codigoEstudante, codigoAcademia, anoLectivo, tipoEnsino, anoAcademicoAtual string,
	regra RegraAvaliacaoFinalDTO,
	materias []projections.MateriaDTO,
	overlay *NotaFormulaOverlay,
) ([]aggregates.ResultadoMateriaAvaliacaoFinal, float64, bool, bool, []aggregates.MateriaPendenteGerada, error) {
	if regra.Fixed && regra.AplicaSeReprovadoEmType != nil {
		prev, err := GetAvaliacaoFinalProjection(c).GetResultadoByType(codigoEstudante, codigoAcademia, anoLectivo, tipoEnsino, anoAcademicoAtual, *regra.AplicaSeReprovadoEmType)
		if err != nil || prev == nil {
			return nil, 0, false, false, nil, fmt.Errorf("avaliação '%s' exige reprovação anterior em '%s'", regra.Type, *regra.AplicaSeReprovadoEmType)
		}
		var anteriores []aggregates.ResultadoMateriaAvaliacaoFinal
		_ = json.Unmarshal(prev.ResultadosMaterias, &anteriores)
		reprovadas := map[uuid.UUID]bool{}
		for _, r := range anteriores {
			if !r.Aprovado {
				reprovadas[r.MateriaID] = true
			}
		}
		filtradas := make([]projections.MateriaDTO, 0, len(materias))
		for _, m := range materias {
			if reprovadas[m.ID] {
				filtradas = append(filtradas, m)
			} else if overlay != nil && overlay.MateriaID == m.ID.String() {
				return nil, 0, false, false, nil, fmt.Errorf("exame_recurso não é permitido para matéria aprovada na avaliação final")
			}
		}
		materias = filtradas
		if len(materias) == 0 {
			return nil, 0, false, false, nil, fmt.Errorf("exame_recurso exige matéria reprovada na avaliação final anterior")
		}
	}
	// Reprovação por faltas (Tarefa 117): só atua se a academia definiu o limite
	// de faltas E ligou a reprovação por faltas.
	zeroPorFaltasAtivo := false
	limiteFaltas := 0
	cfgFaltas, err := GetFaltasConfigProjection(c).GetByAcademia(codigoAcademia)
	if err != nil {
		return nil, 0, false, false, nil, fmt.Errorf("erro ao carregar configuração de faltas: %w", err)
	}
	if cfgFaltas != nil && cfgFaltas.ReprovacaoPorFaltas && cfgFaltas.LimiteFaltasPorPeriodo != nil {
		zeroPorFaltasAtivo = true
		limiteFaltas = *cfgFaltas.LimiteFaltasPorPeriodo
	}
	resultados := make([]aggregates.ResultadoMateriaAvaliacaoFinal, 0, len(materias))
	var soma float64
	reprovadasPendenciaveis := []projections.MateriaDTO{}
	for _, materia := range materias {
		formulaExecucao := regra.Formula
		if tipoEnsino == "superior" {
			if materia.Periodo == nil || *materia.Periodo == "" {
				return nil, 0, false, false, nil, fmt.Errorf("matéria superior %s sem período definido", materia.ID)
			}
			formulaExecucao = PreencherPeriodoFormulaSuperior(regra.Formula, *materia.Periodo)
		}
		notasFormula, err := carregarNotasFormulaMateria(c, codigoEstudante, codigoAcademia, anoLectivo, materia.ID, regra.CategoriasEnvolvidas)
		if err != nil {
			return nil, 0, false, false, nil, err
		}
		if overlay != nil && overlay.MateriaID == materia.ID.String() {
			if notasFormula[overlay.Categoria] == nil {
				notasFormula[overlay.Categoria] = map[string][]float64{}
			}
			notasFormula[overlay.Categoria][overlay.Periodo] = append(notasFormula[overlay.Categoria][overlay.Periodo], overlay.Nota)
		}
		var notasZeradasPorFaltas []aggregates.NotaZeradaPorFaltas
		if zeroPorFaltasAtivo {
			totaisFaltas, errFaltas := GetFaltasProjection(c).SomarPorPeriodo(codigoEstudante, codigoAcademia, anoLectivo, materia.ID)
			if errFaltas != nil {
				return nil, 0, false, false, nil, fmt.Errorf("matéria %s: erro ao somar faltas: %w", materia.ID, errFaltas)
			}
			notasZeradasPorFaltas, errFaltas = AplicarZeroPorFaltas(formulaExecucao, notasFormula, CategoriaZeroPorFaltas(tipoEnsino), totaisFaltas, limiteFaltas)
			if errFaltas != nil {
				return nil, 0, false, false, nil, fmt.Errorf("matéria %s: %w", materia.ID, errFaltas)
			}
		}
		notasSubstituidasZero, err := SubstituirNotasAusentesPorZero(formulaExecucao, notasFormula)
		if err != nil {
			return nil, 0, false, false, nil, fmt.Errorf("matéria %s: %w", materia.ID, err)
		}
		nota, err := CalcularFormulaAvaliacao(formulaExecucao, notasFormula)
		if err != nil {
			return nil, 0, false, false, nil, fmt.Errorf("matéria %s: %w", materia.ID, err)
		}
		aprovada := nota >= regra.NotaMinimaAprovacao
		if !aprovada && materia.PendenciaPermitida {
			reprovadasPendenciaveis = append(reprovadasPendenciaveis, materia)
		}
		soma += nota
		resultados = append(resultados, aggregates.ResultadoMateriaAvaliacaoFinal{
			MateriaID:             materia.ID,
			NotaFinal:             nota,
			Aprovado:              aprovada,
			RegraAvaliacaoFinalID: &regra.ID,
			Type:                  regra.Type,
			FormulaSnapshot:       formulaExecucao,
			PendenciaPermitida:    materia.PendenciaPermitida,
			NotasSubstituidasZero: notasSubstituidasZero,
			NotasZeradasPorFaltas: notasZeradasPorFaltas,
		})
	}
	aprovado := true
	reprovadas := 0
	for _, r := range resultados {
		if !r.Aprovado {
			reprovadas++
			aprovado = false
		}
	}
	aprovadoComPendencia := false
	pendencias := []aggregates.MateriaPendenteGerada{}
	if !aprovado && tipoEnsino != "fundamental" && regra.LimiteMateriasPendentes != nil && reprovadas <= *regra.LimiteMateriasPendentes && reprovadas == len(reprovadasPendenciaveis) {
		aprovado = true
		aprovadoComPendencia = true
		for _, m := range reprovadasPendenciaveis {
			if m.CursoID != nil {
				pendencias = append(pendencias, aggregates.MateriaPendenteGerada{MateriaID: m.ID, CursoID: *m.CursoID, Nivel: tipoEnsino, Escopo: anoAcademicoAtual})
			}
		}
	}
	if len(resultados) == 0 {
		return nil, 0, false, false, nil, fmt.Errorf("nenhuma matéria aplicável recebeu nota-gatilho para avaliação final")
	}
	return resultados, soma / float64(len(resultados)), aprovado, aprovadoComPendencia, pendencias, nil
}

func referenciasFormulaAvaliacao(formula string) ([]aggregates.NotaReferenciaAvaliacaoFinal, error) {
	ast, _, err := ParseFormulaAvaliacao(formula)
	if err != nil {
		return nil, err
	}
	refs := []aggregates.NotaReferenciaAvaliacaoFinal{}
	seen := map[string]bool{}
	var walk func(*FormulaAST)
	walk = func(n *FormulaAST) {
		if n == nil {
			return
		}
		if n.Kind == FormulaASTReference {
			key := n.Categoria + "\x00" + n.Periodo
			if !seen[key] {
				seen[key] = true
				refs = append(refs, aggregates.NotaReferenciaAvaliacaoFinal{Categoria: n.Categoria, Periodo: n.Periodo})
			}
			return
		}
		walk(n.Left)
		walk(n.Right)
	}
	walk(ast)
	return refs, nil
}

func SubstituirNotasAusentesPorZero(formula string, notas map[string]map[string][]float64) ([]aggregates.NotaReferenciaAvaliacaoFinal, error) {
	refs, err := referenciasFormulaAvaliacao(formula)
	if err != nil {
		return nil, err
	}
	faltantes := []aggregates.NotaReferenciaAvaliacaoFinal{}
	for _, ref := range refs {
		if len(notas[ref.Categoria][ref.Periodo]) > 0 {
			continue
		}
		if notas[ref.Categoria] == nil {
			notas[ref.Categoria] = map[string][]float64{}
		}
		notas[ref.Categoria][ref.Periodo] = []float64{0}
		faltantes = append(faltantes, ref)
	}
	return faltantes, nil
}

func InferirTipoEnsinoDoEstudante(estudante *projections.EstudanteDTO) string {
	if estudante == nil {
		return "fundamental"
	}
	if estudante.CursoSuperiorID != nil || estudante.AnoSuperior != nil || estudante.StatusSuperior == "em_andamento" {
		return "superior"
	}
	if estudante.CursoMedioID != nil || estudante.AnoEscolarMedio != nil || estudante.StatusEscolarMedio == "em_andamento" {
		return "medio"
	}
	return "fundamental"
}

// ============================================================================
// GET /avaliacoes
// Estudante → suas avaliações
// Academia  → todas da academia (?nivel=fundamental|medio|superior)
// Admin     → todas do sistema  (?nivel=...)
// ============================================================================

func PaginarAvaliacoesFinais(c *gin.Context, avaliacoes []projections.AvaliacaoFinalDTO) ([]projections.AvaliacaoFinalDTO, int, int, int) {
	limit, offset := GetPaginationParams(c)
	limit = db.ValidateLimit(limit)
	total := len(avaliacoes)

	if offset >= total {
		return []projections.AvaliacaoFinalDTO{}, total, limit, offset
	}

	end := offset + limit
	if end > total {
		end = total
	}
	return avaliacoes[offset:end], total, limit, offset
}

// ============================================================================
// Helpers internos
// ============================================================================

// buscarTurmasDoEstudante retorna todas as turmas atuais do estudante.
func BuscarTurmasDoEstudante(c *gin.Context, codigoEstudante, codigoAcademia string) []string {
	turmasProj := GetTurmasProjection(c)
	turmas, err := turmasProj.ListByAcademia(codigoAcademia)
	if err != nil {
		log.Printf("[avaliacao-final] erro ao buscar turma atual do estudante %s: %v", codigoEstudante, err)
		return nil
	}
	result := make([]string, 0, 2)
	for _, turma := range turmas {
		for _, cod := range turma.Estudantes {
			if cod == codigoEstudante {
				result = append(result, turma.CodigoTurma)
				break
			}
		}
	}
	return result
}

// calcularProximoAnoFundamental calcula o próximo ano na sequência fixa
// 1_ano_fundamental..9_ano_fundamental.
func CalcularProximoAnoFundamental(
	nivelAtual string,
	aprovado bool,
) (*string, error) {
	sequenciaFundamental := []string{
		"1_ano_fundamental",
		"2_ano_fundamental",
		"3_ano_fundamental",
		"4_ano_fundamental",
		"5_ano_fundamental",
		"6_ano_fundamental",
		"7_ano_fundamental",
		"8_ano_fundamental",
		"9_ano_fundamental",
	}

	posAtual := -1
	for i, ano := range sequenciaFundamental {
		if ano == nivelAtual {
			posAtual = i
			break
		}
	}
	if posAtual == -1 {
		return nil, fmt.Errorf("nivel_atual '%s' não pertence à sequência do "+utils.RotuloEnsinoFundamentalGenerico+" (1_ano_fundamental..9_ano_fundamental)", nivelAtual)
	}

	if !aprovado {
		return nil, nil
	}

	if posAtual == len(sequenciaFundamental)-1 {
		return nil, nil
	}

	proximo := sequenciaFundamental[posAtual+1]
	return &proximo, nil
}

// calcularProximoAnoCurso calcula o próximo ano com base na sequência do curso (médio/superior).

const MotivoAcademiaSemOfertaProximoAnoFundamental = "academia_sem_oferta_do_proximo_ano_academico_fundamental"

func MotivoProgressaoFundamentalSemOferta(aprovado bool, proximoAnoAcademico *string, anosAcademicosAcademia []string) *string {
	if !aprovado || proximoAnoAcademico == nil {
		return nil
	}
	for _, ano := range anosAcademicosAcademia {
		if strings.TrimSpace(ano) == *proximoAnoAcademico {
			return nil
		}
	}
	motivo := MotivoAcademiaSemOfertaProximoAnoFundamental
	return &motivo
}

func CalcularProximoAnoCurso(
	c *gin.Context,
	cursoID *uuid.UUID,
	nivelAtual string,
	aprovado bool,
) (*string, error) {
	if cursoID == nil {
		return nil, fmt.Errorf("estudante não possui curso vinculado para este tipo de ensino")
	}

	cursosProj := GetCursosProjection(c)
	curso, err := cursosProj.GetByID(*cursoID)
	if err != nil || curso == nil {
		return nil, fmt.Errorf("curso do estudante não encontrado")
	}

	if curso.Status != "ativo" {
		return nil, fmt.Errorf("curso do estudante está inativo")
	}

	if len(curso.AnosAcademicos) == 0 {
		return nil, fmt.Errorf("curso '%s' não possui anos_academicos definidos", curso.Nome)
	}

	proximo, err := CalcularProximoAnoEmSequencia(curso.AnosAcademicos, nivelAtual, aprovado)
	if err != nil {
		return nil, fmt.Errorf("nivel_atual '%s' não pertence ao curso '%s'", nivelAtual, curso.Nome)
	}
	return proximo, nil
}

func CalcularProximoAnoEmSequencia(anosAcademicos []string, nivelAtual string, aprovado bool) (*string, error) {
	posAtual := -1
	for i, n := range anosAcademicos {
		if n == nivelAtual {
			posAtual = i
			break
		}
	}
	if posAtual == -1 {
		return nil, fmt.Errorf("nivel_atual '%s' não pertence à sequência acadêmica", nivelAtual)
	}
	if !aprovado || posAtual == len(anosAcademicos)-1 {
		return nil, nil
	}
	proximo := anosAcademicos[posAtual+1]
	return &proximo, nil
}

func PeriodoSuperiorAtual(c *gin.Context, estudante *projections.EstudanteDTO, cursoID *uuid.UUID) (string, error) {
	if estudante == nil || estudante.SemestreAtual == nil || *estudante.SemestreAtual < 1 {
		return "", fmt.Errorf("estudante superior não possui semestre_atual válido")
	}
	periodo := fmt.Sprintf("%d_semestre", *estudante.SemestreAtual)
	if cursoID == nil {
		return "", fmt.Errorf("estudante não possui curso superior vinculado")
	}
	curso, err := GetCursosProjection(c).GetByID(*cursoID)
	if err != nil || curso == nil {
		return "", fmt.Errorf("curso superior não encontrado")
	}
	if curso.Status != "ativo" {
		return "", fmt.Errorf("curso superior do estudante está inativo")
	}
	for _, p := range curso.Periodos {
		if p == periodo {
			return periodo, nil
		}
	}
	return "", fmt.Errorf("semestre_atual %d não existe nos periodos do curso superior '%s'", *estudante.SemestreAtual, curso.Nome)
}

func calcularAnoSuperiorPorSemestre(semestre int) string {
	ano := int(math.Ceil(float64(semestre) / 2.0))
	return fmt.Sprintf("%d_ano_superior", ano)
}

func CalcularProximoSemestreCurso(c *gin.Context, cursoID *uuid.UUID, semestreAtual *int, aprovado bool) (*int, *string, error) {
	if semestreAtual == nil || *semestreAtual < 1 {
		return nil, nil, fmt.Errorf("estudante superior não possui semestre_atual válido")
	}
	if cursoID == nil {
		return nil, nil, fmt.Errorf("estudante não possui curso superior vinculado")
	}
	curso, err := GetCursosProjection(c).GetByID(*cursoID)
	if err != nil || curso == nil {
		return nil, nil, fmt.Errorf("curso superior não encontrado")
	}
	periodoAtual := fmt.Sprintf("%d_semestre", *semestreAtual)
	pos := -1
	for i, p := range curso.Periodos {
		if p == periodoAtual {
			pos = i
			break
		}
	}
	if pos == -1 {
		return nil, nil, fmt.Errorf("semestre_atual %d não pertence ao curso '%s'", *semestreAtual, curso.Nome)
	}
	anoAtual := calcularAnoSuperiorPorSemestre(*semestreAtual)
	if !aprovado || pos == len(curso.Periodos)-1 {
		return nil, &anoAtual, nil
	}
	prox := *semestreAtual + 1
	anoDepois := calcularAnoSuperiorPorSemestre(prox)
	return &prox, &anoDepois, nil
}

func ValidarNivelAtualDoEstudante(estudante *projections.EstudanteDTO, tipoEnsino, nivelInformado string) error {
	if estudante == nil {
		return fmt.Errorf("estudante inválido")
	}

	var nivelAtual *string
	switch tipoEnsino {
	case "fundamental":
		nivelAtual = estudante.AnoEscolar
	case "medio":
		nivelAtual = estudante.AnoEscolarMedio
	case "superior":
		if estudante.SemestreAtual == nil || *estudante.SemestreAtual < 1 {
			return fmt.Errorf("estudante superior não possui semestre_atual válido")
		}
		esperado := fmt.Sprintf("%d_semestre", *estudante.SemestreAtual)
		if esperado != nivelInformado {
			return fmt.Errorf("nivel_ano_academico_atual incompatível: esperado semestre '%s', recebido '%s'", esperado, nivelInformado)
		}
		return nil
	}

	if nivelAtual == nil || strings.TrimSpace(*nivelAtual) == "" {
		return fmt.Errorf("estudante não possui nível acadêmico atual definido para '%s'", tipoEnsino)
	}
	if *nivelAtual != nivelInformado {
		return fmt.Errorf("nivel_ano_academico_atual incompatível: esperado '%s', recebido '%s'", *nivelAtual, nivelInformado)
	}
	return nil
}

type MateriaAplicavelEscopo struct {
	CursoID      *string     `json:"curso_id,omitempty"`
	AnoAcademico string      `json:"ano_academico"`
	Materias     []uuid.UUID `json:"materias"`
}

type RegraAvaliacaoFinalDTO struct {
	ID                       uuid.UUID                `json:"id"`
	CodigoAcademia           string                   `json:"codigo_academia"`
	Type                     string                   `json:"type" binding:"required"`
	Nome                     string                   `json:"nome" binding:"required"`
	Descricao                *string                  `json:"descricao,omitempty"`
	Nivel                    string                   `json:"nivel,omitempty"`
	AnosAcademicos           []string                 `json:"anos_academicos,omitempty"`
	NotaMinimaAprovacao      float64                  `json:"nota_minima_aprovacao" binding:"required"`
	CategoriasEnvolvidas     []string                 `json:"categorias_envolvidas,omitempty"`
	Formula                  string                   `json:"formula" binding:"required"`
	MateriasAplicaveis       []uuid.UUID              `json:"materias_aplicaveis,omitempty"`
	MateriasAplicaveisEscopo []MateriaAplicavelEscopo `json:"-"`
	LimiteMateriasPendentes  *int                     `json:"limite_materias_pendentes,omitempty"`
	AplicaSeReprovadoEmType  *string                  `json:"aplica_se_reprovado_em_type,omitempty"`
	Status                   string                   `json:"status"`
	Version                  int                      `json:"version"`
	NotaDespertadora         *string                  `json:"nota_despertadora,omitempty"`
	Source                   string                   `json:"source,omitempty"`
	Fixed                    bool                     `json:"fixed,omitempty"`
	Readonly                 bool                     `json:"readonly,omitempty"`
}

// UnmarshalJSON preserves the legacy public contract for fundamental rules
// (anos_academicos as []string), while accepting the new medio contract
// (items grouped by curso_id). Internally the medio scope is flattened as
// "<curso_id>|<ano_academico>" so existing rule-chain and uniqueness helpers
// can compare scopes deterministically until the projection is fully
// decomposed into an auxiliary scope table.
func (r *RegraAvaliacaoFinalDTO) UnmarshalJSON(b []byte) error {
	type alias RegraAvaliacaoFinalDTO
	var raw struct {
		*alias
		AnosAcademicos     json.RawMessage `json:"anos_academicos"`
		MateriasAplicaveis json.RawMessage `json:"materias_aplicaveis"`
	}
	raw.alias = (*alias)(r)
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if len(raw.AnosAcademicos) > 0 && string(raw.AnosAcademicos) != "null" {
		var anos []string
		if err := json.Unmarshal(raw.AnosAcademicos, &anos); err == nil {
			r.AnosAcademicos = anos
		} else {
			var porCurso []struct {
				CursoID        string   `json:"curso_id"`
				AnosAcademicos []string `json:"anos_academicos"`
			}
			if err := json.Unmarshal(raw.AnosAcademicos, &porCurso); err != nil {
				return fmt.Errorf("anos_academicos deve ser array de strings para fundamental ou array de objetos por curso para medio")
			}
			r.AnosAcademicos = r.AnosAcademicos[:0]
			cursosVistos := map[string]bool{}
			for _, item := range porCurso {
				cursoID := strings.TrimSpace(item.CursoID)
				if cursoID == "" {
					return fmt.Errorf("anos_academicos de médio exige curso_id")
				}
				if cursosVistos[cursoID] {
					return fmt.Errorf("anos_academicos de médio não aceita dois itens para o mesmo curso_id: %s", cursoID)
				}
				cursosVistos[cursoID] = true
				anosVistos := map[string]bool{}
				for _, ano := range item.AnosAcademicos {
					ano = strings.TrimSpace(ano)
					if ano == "" {
						return fmt.Errorf("anos_academicos de médio não pode conter ano vazio")
					}
					if anosVistos[ano] {
						return fmt.Errorf("ano_academico duplicado em anos_academicos do curso_id %s: %s", cursoID, ano)
					}
					anosVistos[ano] = true
					r.AnosAcademicos = append(r.AnosAcademicos, cursoID+"|"+ano)
				}
			}
		}
	}
	if len(raw.MateriasAplicaveis) > 0 && string(raw.MateriasAplicaveis) != "null" {
		var porEscopo []MateriaAplicavelEscopo
		if err := json.Unmarshal(raw.MateriasAplicaveis, &porEscopo); err != nil {
			return fmt.Errorf("materias_aplicaveis deve ser lista de objetos por escopo; lista simples global de IDs não é aceita")
		}
		r.MateriasAplicaveis = r.MateriasAplicaveis[:0]
		r.MateriasAplicaveisEscopo = porEscopo
		for _, item := range porEscopo {
			r.MateriasAplicaveis = append(r.MateriasAplicaveis, item.Materias...)
		}
	}
	return nil
}

func validarCadeiaAvaliacaoFinalAplicavel(regras []RegraAvaliacaoFinalDTO, codigoAcademia, tipoEnsino, anoAcademico string) error {
	raizes := 0
	tipos := map[string]bool{}
	for _, regra := range regras {
		tipos[regra.Type] = true
		if regra.AplicaSeReprovadoEmType == nil || strings.TrimSpace(*regra.AplicaSeReprovadoEmType) == "" {
			raizes++
		}
	}
	if raizes == 0 {
		return fmt.Errorf("nenhuma regra raiz de avaliação final encontrada para academia=%s nivel=%s ano=%s", codigoAcademia, tipoEnsino, anoAcademico)
	}
	if raizes > 1 {
		return fmt.Errorf("mais de uma regra raiz de avaliação final encontrada para academia=%s nivel=%s ano=%s", codigoAcademia, tipoEnsino, anoAcademico)
	}
	for _, regra := range regras {
		if regra.AplicaSeReprovadoEmType == nil || strings.TrimSpace(*regra.AplicaSeReprovadoEmType) == "" {
			continue
		}
		if !tipos[strings.TrimSpace(*regra.AplicaSeReprovadoEmType)] {
			return fmt.Errorf("regra type=%s depende de type fora da cadeia aplicável: %s", regra.Type, *regra.AplicaSeReprovadoEmType)
		}
	}
	return nil
}

func ListarRegrasAvaliacaoFinalAplicaveis(c *gin.Context, codigoAcademia, tipoEnsino, anoAcademico string, categoria *string, cursoID *string) ([]RegraAvaliacaoFinalDTO, error) {
	if tipoEnsino == "fundamental" || tipoEnsino == "medio" {
		// O modelo escolar é fixo do sistema. Mesmo quando a categoria informada não
		// desperta uma regra fixa (por exemplo, nota_professor), não devemos cair
		// para regras configuráveis/legadas da projeção, pois escolas não podem
		// criar, editar, remover nem sobrescrever regras oficiais.
		return RegrasAvaliacaoFinalEscolaresFixas(c, codigoAcademia, tipoEnsino, anoAcademico, categoria, cursoID), nil
	}
	query := `SELECT id,codigo_academia,type,nome,descricao,nivel,anos_academicos,nota_minima_aprovacao,categorias_envolvidas,formula,aplica_se_reprovado_em_type,materias_aplicaveis,limite_materias_pendentes,status,version,nota_despertadora
		FROM projection_regras_avaliacao_final
		WHERE codigo_academia=$1
		  AND nivel=$2
		  AND status='ativo'
		  AND (($2 <> 'fundamental' AND $2 <> 'medio') OR ($2 = 'fundamental' AND anos_academicos ? $3) OR ($2 = 'medio' AND EXISTS (SELECT 1 FROM jsonb_array_elements_text(anos_academicos) e(v) WHERE split_part(e.v, '|', 2) = $3)))`
	args := []interface{}{codigoAcademia, tipoEnsino, anoAcademico}
	if tipoEnsino == "medio" && cursoID != nil && strings.TrimSpace(*cursoID) != "" {
		args = append(args, strings.TrimSpace(*cursoID)+"|"+strings.TrimSpace(anoAcademico))
		query += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM jsonb_array_elements_text(anos_academicos) e(v) WHERE e.v = $%d)", len(args))
	}
	if categoria != nil && strings.TrimSpace(*categoria) != "" {
		args = append(args, strings.TrimSpace(*categoria))
		query += fmt.Sprintf(" AND ((aplica_se_reprovado_em_type IS NULL AND nota_despertadora = $%d) OR aplica_se_reprovado_em_type IS NOT NULL)", len(args))
	}
	query += ` ORDER BY CASE WHEN aplica_se_reprovado_em_type IS NULL THEN 0 ELSE 1 END, type`

	rows, err := GetDbClient(c).DB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RegraAvaliacaoFinalDTO{}
	for rows.Next() {
		r, err := ScanRegra(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func ScanRegra(rows rowScanner) (RegraAvaliacaoFinalDTO, error) {
	var r RegraAvaliacaoFinalDTO
	var anos, cats, materiasAplicaveis []byte
	var limitePendentes sql.NullInt64
	var formula string
	err := rows.Scan(&r.ID, &r.CodigoAcademia, &r.Type, &r.Nome, &r.Descricao, &r.Nivel, &anos, &r.NotaMinimaAprovacao, &cats, &formula, &r.AplicaSeReprovadoEmType, &materiasAplicaveis, &limitePendentes, &r.Status, &r.Version, &r.NotaDespertadora)
	_ = json.Unmarshal(anos, &r.AnosAcademicos)
	_ = json.Unmarshal(cats, &r.CategoriasEnvolvidas)
	r.MateriasAplicaveis = uuidListFromJSON(materiasAplicaveis)
	if limitePendentes.Valid {
		v := int(limitePendentes.Int64)
		r.LimiteMateriasPendentes = &v
	}
	r.Formula = formula
	return r, err
}

func uuidListFromJSON(b []byte) []uuid.UUID {
	var ss []string
	_ = json.Unmarshal(b, &ss)
	out := make([]uuid.UUID, 0, len(ss))
	for _, s := range ss {
		if id, err := uuid.Parse(s); err == nil {
			out = append(out, id)
		}
	}
	return out
}

type formulaASTKind int

const (
	FormulaASTNumber formulaASTKind = iota
	FormulaASTReference
	FormulaASTBinary
)

type FormulaAST struct {
	Kind      formulaASTKind
	Value     float64
	Categoria string
	Periodo   string
	Op        rune
	Left      *FormulaAST
	Right     *FormulaAST
}

type formulaParser struct {
	input string
	pos   int
}

const maxFormulaAvaliacaoLen = 1000

func ParseFormulaAvaliacao(formula string) (*FormulaAST, string, error) {
	formula = strings.TrimSpace(formula)
	if formula == "" {
		return nil, "", fmt.Errorf("formula não pode ser vazia")
	}
	if len(formula) > maxFormulaAvaliacaoLen {
		return nil, "", fmt.Errorf("formula excede o limite de %d caracteres", maxFormulaAvaliacaoLen)
	}
	p := &formulaParser{input: formula}
	ast, err := p.parseExpression()
	if err != nil {
		return nil, "", err
	}
	p.skipSpaces()
	if p.pos != len(p.input) {
		return nil, "", fmt.Errorf("token inválido na formula na posição %d", p.pos+1)
	}
	return ast, ast.String(), nil
}

func (p *formulaParser) parseExpression() (*FormulaAST, error) {
	left, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpaces()
		if !p.consume('+') && !p.consume('-') {
			return left, nil
		}
		op := rune(p.input[p.pos-1])
		right, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		left = &FormulaAST{Kind: FormulaASTBinary, Op: op, Left: left, Right: right}
	}
}

func (p *formulaParser) parseTerm() (*FormulaAST, error) {
	left, err := p.parseFactor()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpaces()
		if !p.consume('*') && !p.consume('/') {
			return left, nil
		}
		op := rune(p.input[p.pos-1])
		right, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		if op == '/' && right.Kind == FormulaASTNumber && right.Value == 0 {
			return nil, fmt.Errorf("divisão por zero não permitida")
		}
		left = &FormulaAST{Kind: FormulaASTBinary, Op: op, Left: left, Right: right}
	}
}

func (p *formulaParser) parseFactor() (*FormulaAST, error) {
	p.skipSpaces()
	if p.pos >= len(p.input) {
		return nil, fmt.Errorf("formula incompleta")
	}
	ch := rune(p.input[p.pos])
	if ch == '(' {
		p.pos++
		n, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		p.skipSpaces()
		if !p.consume(')') {
			return nil, fmt.Errorf("parêntese ')' esperado")
		}
		return n, nil
	}
	if ch == '[' {
		return p.parseReference()
	}
	if unicode.IsDigit(ch) {
		return p.parseNumber()
	}
	return nil, fmt.Errorf("token inválido na formula na posição %d", p.pos+1)
}

func (p *formulaParser) parseReference() (*FormulaAST, error) {
	p.pos++
	catStart := p.pos
	for p.pos < len(p.input) && isFormulaIdentRune(rune(p.input[p.pos])) {
		p.pos++
	}
	categoria := p.input[catStart:p.pos]
	if categoria == "" {
		return nil, fmt.Errorf("referência de nota exige categoria")
	}
	p.skipSpaces()
	periodo := ""
	if p.consume(',') {
		p.skipSpaces()
		perStart := p.pos
		for p.pos < len(p.input) && isFormulaIdentRune(rune(p.input[p.pos])) {
			p.pos++
		}
		periodo = p.input[perStart:p.pos]
		if periodo == "" {
			return nil, fmt.Errorf("referência de nota exige periodo")
		}
		p.skipSpaces()
	}
	if !p.consume(']') {
		return nil, fmt.Errorf("referência de nota deve terminar com ']'")
	}
	return &FormulaAST{Kind: FormulaASTReference, Categoria: categoria, Periodo: periodo}, nil
}

func (p *formulaParser) parseNumber() (*FormulaAST, error) {
	start := p.pos
	for p.pos < len(p.input) && unicode.IsDigit(rune(p.input[p.pos])) {
		p.pos++
	}
	if p.pos < len(p.input) && p.input[p.pos] == '.' {
		p.pos++
		if p.pos >= len(p.input) || !unicode.IsDigit(rune(p.input[p.pos])) {
			return nil, fmt.Errorf("número decimal inválido")
		}
		for p.pos < len(p.input) && unicode.IsDigit(rune(p.input[p.pos])) {
			p.pos++
		}
	}
	v, err := strconv.ParseFloat(p.input[start:p.pos], 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return nil, fmt.Errorf("número inválido")
	}
	return &FormulaAST{Kind: FormulaASTNumber, Value: v}, nil
}

func (p *formulaParser) skipSpaces() {
	for p.pos < len(p.input) && unicode.IsSpace(rune(p.input[p.pos])) {
		p.pos++
	}
}

func (p *formulaParser) consume(ch byte) bool {
	if p.pos < len(p.input) && p.input[p.pos] == ch {
		p.pos++
		return true
	}
	return false
}

func isFormulaIdentRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

func (n *FormulaAST) String() string {
	switch n.Kind {
	case FormulaASTNumber:
		return strconv.FormatFloat(n.Value, 'f', -1, 64)
	case FormulaASTReference:
		if n.Periodo == "" {
			return fmt.Sprintf("[%s]", n.Categoria)
		}
		return fmt.Sprintf("[%s,%s]", n.Categoria, n.Periodo)
	case FormulaASTBinary:
		return fmt.Sprintf("(%s%c%s)", n.Left.String(), n.Op, n.Right.String())
	}
	return ""
}

func CalcularFormulaAvaliacao(formula string, notas map[string]map[string][]float64) (float64, error) {
	ast, _, err := ParseFormulaAvaliacao(formula)
	if err != nil {
		return 0, err
	}
	return evalASTFormula(ast, notas)
}

func evalASTFormula(n *FormulaAST, notas map[string]map[string][]float64) (float64, error) {
	switch n.Kind {
	case FormulaASTNumber:
		return n.Value, nil
	case FormulaASTReference:
		vals := notas[n.Categoria][n.Periodo]
		if len(vals) == 0 {
			return 0, fmt.Errorf("nota ausente: categoria=%s periodo=%s", n.Categoria, n.Periodo)
		}
		var s float64
		for _, v := range vals {
			s += v
		}
		return s, nil
	case FormulaASTBinary:
		l, err := evalASTFormula(n.Left, notas)
		if err != nil {
			return 0, err
		}
		r, err := evalASTFormula(n.Right, notas)
		if err != nil {
			return 0, err
		}
		switch n.Op {
		case '+':
			return l + r, nil
		case '-':
			return l - r, nil
		case '*':
			return l * r, nil
		case '/':
			if r == 0 {
				return 0, fmt.Errorf("divisão por zero")
			}
			return l / r, nil
		}
	}
	return 0, fmt.Errorf("formula inválida")
}

func PreencherPeriodoFormulaSuperior(formula, periodo string) string {
	ast, _, err := ParseFormulaAvaliacao(formula)
	if err != nil {
		return formula
	}
	var walk func(*FormulaAST)
	walk = func(n *FormulaAST) {
		if n == nil {
			return
		}
		if n.Kind == FormulaASTReference && n.Periodo == "" {
			n.Periodo = periodo
		}
		walk(n.Left)
		walk(n.Right)
	}
	walk(ast)
	return ast.String()
}

func carregarNotasFormulaMateria(c *gin.Context, codigoEstudante, codigoAcademia, anoLectivo string, materiaID uuid.UUID, categorias []string) (map[string]map[string][]float64, error) {
	rows, err := GetDbClient(c).DB().Query(`SELECT categoria,periodo,nota FROM projection_notas WHERE codigo_estudante=$1 AND codigo_academia=$2 AND ano_lectivo=$3 AND materia_disciplinar_id=$4 AND categoria=ANY($5)`, codigoEstudante, codigoAcademia, anoLectivo, materiaID, pq.Array(categorias))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string][]float64{}
	for rows.Next() {
		var cat, per string
		var nota float64
		if err := rows.Scan(&cat, &per, &nota); err != nil {
			return nil, err
		}
		if out[cat] == nil {
			out[cat] = map[string][]float64{}
		}
		out[cat][per] = append(out[cat][per], nota)
	}
	return out, rows.Err()
}

const (
	CategoriaNotaPAP = "nota_pap"
)

type categoriaNotaEscolarFixa struct {
	Codigo string `json:"codigo"`
	Nome   string `json:"nome"`
}

func RegrasAvaliacaoFinalEscolaresFixas(c *gin.Context, codigoAcademia, tipoEnsino, anoAcademico string, categoria *string, cursoID *string) []RegraAvaliacaoFinalDTO {
	if tipoEnsino != "fundamental" && tipoEnsino != "medio" {
		return nil
	}
	var regras []RegraAvaliacaoFinalDTO
	modeloCursoMedio := modeloCursoMedioPorID(c, cursoID)
	if r := RegraAvaliacaoFinalEscolarFixa(codigoAcademia, tipoEnsino, anoAcademico, "normal", cursoID, modeloCursoMedio); r != nil {
		regras = append(regras, *r)
	}
	if r := RegraAvaliacaoFinalEscolarFixa(codigoAcademia, tipoEnsino, anoAcademico, "exame_recurso", cursoID, modeloCursoMedio); r != nil {
		regras = append(regras, *r)
	}
	if categoria != nil && strings.TrimSpace(*categoria) != "" && len(regras) > 0 {
		categoriaFiltro := strings.TrimSpace(*categoria)
		filtradas := make([]RegraAvaliacaoFinalDTO, 0, len(regras))
		for _, regra := range regras {
			if regra.NotaDespertadora != nil && strings.TrimSpace(*regra.NotaDespertadora) == categoriaFiltro {
				filtradas = append(filtradas, regra)
			}
		}
		return filtradas
	}
	return regras
}

func RegraAvaliacaoFinalEscolarFixa(codigoAcademia, tipoEnsino, anoAcademico, typ string, cursoID *string, modeloCursoMedio string) *RegraAvaliacaoFinalDTO {
	typ = strings.TrimSpace(typ)
	if typ == "" {
		typ = "normal"
	}
	if tipoEnsino != "fundamental" && tipoEnsino != "medio" {
		return nil
	}
	if tipoEnsino == "fundamental" && !isAnoFundamental(anoAcademico) {
		return nil
	}
	if tipoEnsino == "medio" && !IsAnoMedio(anoAcademico) {
		return nil
	}
	anoEscopo := anoAcademico
	if tipoEnsino == "medio" && cursoID != nil && strings.TrimSpace(*cursoID) != "" {
		anoEscopo = strings.TrimSpace(*cursoID) + "|" + anoAcademico
	}
	base := RegraAvaliacaoFinalDTO{
		ID:                 uuid.NewSHA1(uuid.NameSpaceOID, []byte("spuri:regra-escolar:"+tipoEnsino+":"+anoEscopo+":"+typ)),
		CodigoAcademia:     codigoAcademia,
		Type:               typ,
		Nivel:              tipoEnsino,
		AnosAcademicos:     []string{anoEscopo},
		Status:             "ativo",
		Version:            1,
		Source:             "system",
		Fixed:              true,
		Readonly:           true,
		MateriasAplicaveis: nil,
	}
	regular := "(((([nota_professor,1_trimestre]+[prova_trimestral,1_trimestre])/2)+(([nota_professor,2_trimestre]+[prova_trimestral,2_trimestre])/2)+(([nota_professor,3_trimestre]+[prova_trimestral,3_trimestre])/2))/3)"
	comExame := "(((([nota_professor,1_trimestre]+[prova_trimestral,1_trimestre])/2)+(([nota_professor,2_trimestre]+[prova_trimestral,2_trimestre])/2)+(([nota_professor,3_trimestre]+[exame_final,3_trimestre])/2))/3)"
	switch anoAcademico {
	case "1_ano_fundamental", "2_ano_fundamental", "3_ano_fundamental", "4_ano_fundamental", "5_ano_fundamental":
		if typ != "normal" {
			return nil
		}
		base.Nome = "Avaliação final"
		base.NotaMinimaAprovacao = 5
		base.CategoriasEnvolvidas = []string{"nota_professor", "prova_trimestral"}
		base.Formula = regular
		v := "prova_trimestral"
		base.NotaDespertadora = &v
	case "7_ano_fundamental", "8_ano_fundamental", "1_ano_medio", "2_ano_medio":
		if typ != "normal" {
			return nil
		}
		base.Nome = "Avaliação final"
		base.NotaMinimaAprovacao = 10
		base.CategoriasEnvolvidas = []string{"nota_professor", "prova_trimestral"}
		base.Formula = regular
		v := "prova_trimestral"
		base.NotaDespertadora = &v
	case "6_ano_fundamental", "9_ano_fundamental", "3_ano_medio":
		if typ == "normal" {
			base.Nome = "Avaliação final"
			if anoAcademico == "6_ano_fundamental" {
				base.NotaMinimaAprovacao = 5
			} else {
				base.NotaMinimaAprovacao = 10
			}
			base.CategoriasEnvolvidas = []string{"nota_professor", "prova_trimestral", "exame_final"}
			base.Formula = comExame
			v := "exame_final"
			base.NotaDespertadora = &v
		} else if typ == "exame_recurso" {
			base.Nome = "Exame de recurso"
			if anoAcademico == "6_ano_fundamental" {
				base.NotaMinimaAprovacao = 5
			} else {
				base.NotaMinimaAprovacao = 10
			}
			base.CategoriasEnvolvidas = []string{"exame_recurso"}
			base.Formula = "[exame_recurso,3_trimestre]"
			dep := "normal"
			base.AplicaSeReprovadoEmType = &dep
			v := "exame_recurso"
			base.NotaDespertadora = &v
		} else {
			return nil
		}
	case "4_ano_medio":
		if tipoEnsino != "medio" || typ != "normal" || strings.TrimSpace(modeloCursoMedio) != aggregates.ModeloCursoMedioTecnico {
			return nil
		}
		base.Nome = "Prova de Aptidão Profissional"
		base.NotaMinimaAprovacao = 10
		base.CategoriasEnvolvidas = []string{CategoriaNotaPAP}
		base.Formula = "[nota_pap,3_trimestre]"
		v := CategoriaNotaPAP
		base.NotaDespertadora = &v
	default:
		return nil
	}
	return &base
}

var categoriasEscolaresRegulares = []categoriaNotaEscolarFixa{
	{Codigo: "nota_professor", Nome: "Nota do professor/Avaliação contínua"},
	{Codigo: "prova_trimestral", Nome: "Prova trimestral"},
}

var categoriasEscolaresComExames = []categoriaNotaEscolarFixa{
	{Codigo: "nota_professor", Nome: "Nota do professor/Avaliação contínua"},
	{Codigo: "prova_trimestral", Nome: "Prova trimestral"},
	{Codigo: "exame_final", Nome: "Exame final"},
	{Codigo: "exame_recurso", Nome: "Exame de recurso"},
}

var categoriasEscolaresPAP = []categoriaNotaEscolarFixa{
	{Codigo: CategoriaNotaPAP, Nome: "Prova de Aptidão Profissional"},
}

func isAnoFundamental(ano string) bool {
	return strings.HasSuffix(strings.TrimSpace(ano), "_ano_fundamental")
}

func IsAnoMedio(ano string) bool { return strings.HasSuffix(strings.TrimSpace(ano), "_ano_medio") }

func categoriasEscolaresFixasParaAno(anoAcademico string, modeloCursoMedio string) []categoriaNotaEscolarFixa {
	ano := strings.TrimSpace(anoAcademico)
	if ano == "4_ano_medio" && strings.TrimSpace(modeloCursoMedio) == aggregates.ModeloCursoMedioTecnico {
		return categoriasEscolaresPAP
	}
	switch ano {
	case "6_ano_fundamental", "9_ano_fundamental", "3_ano_medio":
		return categoriasEscolaresComExames
	default:
		if isAnoFundamental(ano) || IsAnoMedio(ano) {
			return categoriasEscolaresRegulares
		}
		return nil
	}
}

func CodigosCategoriasEscolaresFixasParaAno(anoAcademico, modeloCursoMedio string) []string {
	cats := categoriasEscolaresFixasParaAno(anoAcademico, modeloCursoMedio)
	out := make([]string, 0, len(cats))
	for _, cat := range cats {
		out = append(out, cat.Codigo)
	}
	return out
}

func ValidarEscalaNotaPorAnoAcademico(anoAcademico string, nota float64) error {
	max := LimiteNotaPorAnoAcademico(anoAcademico)
	if nota < 0 || nota > max {
		return fmt.Errorf("nota %.2f fora da escala permitida para %s: use valores entre 0 e %.0f", nota, anoAcademico, max)
	}
	return nil
}

func LimiteNotaPorAnoAcademico(anoAcademico string) float64 {
	max := 20.0
	if strings.Contains(anoAcademico, "_ano_fundamental") {
		switch anoAcademico {
		case "1_ano_fundamental", "2_ano_fundamental", "3_ano_fundamental", "4_ano_fundamental", "5_ano_fundamental", "6_ano_fundamental":
			max = 10
		}
	}
	return max
}

func ModeloCursoMedioDaMateria(c *gin.Context, materiaDTO *projections.MateriaDTO) string {
	if materiaDTO == nil || materiaDTO.CursoID == nil {
		return ""
	}
	cursoDTO, err := GetCursosProjection(c).GetByID(*materiaDTO.CursoID)
	if err != nil || cursoDTO == nil || cursoDTO.Type != "medio" {
		return ""
	}
	return cursoDTO.Modelo
}

func modeloCursoMedioPorID(c *gin.Context, cursoID *string) string {
	if c == nil || cursoID == nil || strings.TrimSpace(*cursoID) == "" {
		return ""
	}
	id, err := uuid.Parse(strings.TrimSpace(*cursoID))
	if err != nil {
		return ""
	}
	cursoDTO, err := GetCursosProjection(c).GetByID(id)
	if err != nil || cursoDTO == nil || cursoDTO.Type != "medio" {
		return ""
	}
	return cursoDTO.Modelo
}

func CategoriasEscolaresFixasDaAcademia(c *gin.Context, academia *projections.AcademiaDTO) []interface{} {
	if academia == nil || academia.Nivel != "escola" {
		return nil
	}
	type item struct{ ano, modelo string }
	vistos := map[string]bool{}
	itens := []item{}
	add := func(ano, modelo string) {
		ano = strings.TrimSpace(ano)
		modelo = strings.TrimSpace(modelo)
		if ano == "" {
			return
		}
		key := ano + "|" + modelo
		if !vistos[key] {
			vistos[key] = true
			itens = append(itens, item{ano: ano, modelo: modelo})
		}
	}
	for _, ano := range academia.AnosAcademicos {
		add(ano, "")
	}
	if c != nil {
		if cursos, err := GetCursosProjection(c).GetByAcademia(academia.CodigoAcademia); err == nil {
			for _, curso := range cursos {
				if curso.Type != "medio" || curso.Status != "ativo" {
					continue
				}
				for _, ano := range curso.AnosAcademicos {
					add(ano, curso.Modelo)
				}
			}
		}
	}
	out := []interface{}{}
	for _, it := range itens {
		for _, cat := range categoriasEscolaresFixasParaAno(it.ano, it.modelo) {
			out = append(out, map[string]interface{}{
				"codigo_academia": academia.CodigoAcademia,
				"codigo":          cat.Codigo,
				"nome":            cat.Nome,
				"anos_academicos": []string{it.ano},
				"source":          "system",
				"fixed":           true,
				"readonly":        true,
				"status":          "ativo",
			})
		}
	}
	return out
}
