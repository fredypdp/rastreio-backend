package avaliacoes

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/handlers/apoio"
	"spuri/internal/middleware"
	"spuri/internal/projections"
	"spuri/internal/utils"
)

// ============================================================================
// POST /academia/avaliacao-final
// ============================================================================

func RegistrarAvaliacaoFinal(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)

	var req struct {
		CodigoEstudante     string  `json:"codigo_estudante"          binding:"required"`
		AnoAcademicoAtual   string  `json:"nivel_ano_academico_atual" binding:"required"`
		ProximoAnoAcademico *string `json:"proximo_ano_academico,omitempty"`
		Type                string  `json:"type"`
		Observacao          *string `json:"observacao"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf(
			"campos obrigatórios: codigo_estudante, nivel_ano_academico_atual",
		))
		return
	}

	if req.ProximoAnoAcademico != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("proximo_ano_academico é calculado automaticamente pelo backend e não deve ser enviado"))
		return
	}

	// ── Academia ──────────────────────────────────────────────────────────────
	academiaProj := apoio.GetAcademiaProjection(c)
	academiaDTO, err := academiaProj.GetByID(userID)
	if err != nil || academiaDTO == nil {
		utils.RespondWithNotFoundError(c, "academia")
		return
	}

	// Ano letivo obrigatório — bloqueia registro se a academia não tiver configurado
	anoLectivo, err := apoio.ResolverAnoLetivoAcademia(academiaDTO.AnoLetivo, academiaDTO.CodigoAcademia)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	// ── Estudante ─────────────────────────────────────────────────────────────
	estudanteProj := apoio.GetEstudanteProjection(c)
	estudanteDTO, err := estudanteProj.GetByCodigo(req.CodigoEstudante)
	if err != nil || estudanteDTO == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}
	if estudanteDTO.CodigoAcademia == nil || *estudanteDTO.CodigoAcademia != academiaDTO.CodigoAcademia {
		utils.RespondWithForbiddenError(c, "estudante não pertence a esta academia")
		return
	}

	// FIX-COMPILE-02: EstudanteDTO armazena CursoMedioID e CursoSuperiorID como
	// *string (banco persiste UUID como texto). Converter para *uuid.UUID para
	// passar para validarNotasParaAprovacao e calcularProximoAnoCurso.
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

	tipoEnsino := apoio.InferirTipoEnsinoDoEstudante(estudanteDTO)
	switch tipoEnsino {
	case "fundamental":
		if err := utils.ValidateAnoFundamental(req.AnoAcademicoAtual); err != nil {
			utils.RespondWithValidationError(c, fmt.Errorf("nivel_ano_academico_atual inválido: %w", err))
			return
		}
	case "medio":
		if err := utils.ValidateAnoMedio(req.AnoAcademicoAtual); err != nil {
			utils.RespondWithValidationError(c, fmt.Errorf("nivel_ano_academico_atual inválido: %w", err))
			return
		}
	case "superior":
		periodoAtual, err := apoio.PeriodoSuperiorAtual(c, estudanteDTO, cursoSuperiorUUID)
		if err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
		req.AnoAcademicoAtual = periodoAtual
	}

	avaliacaoProj := apoio.GetAvaliacaoFinalProjection(c)
	if strings.TrimSpace(req.Type) == "" {
		req.Type = "normal"
	}
	regra, err := getRegraAvaliacaoFinal(c, academiaDTO.CodigoAcademia, tipoEnsino, req.AnoAcademicoAtual, req.Type)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	jaAvaliadoNoNivel, err := avaliacaoProj.ExistsByEstudanteAnoLetivoNivelType(
		req.CodigoEstudante,
		academiaDTO.CodigoAcademia,
		anoLectivo,
		tipoEnsino,
		req.AnoAcademicoAtual,
		req.Type,
	)
	if err != nil {
		utils.RespondWithInternalError(c, fmt.Errorf("erro ao verificar avaliação final existente no nível: %w", err))
		return
	}
	if jaAvaliadoNoNivel {
		utils.RespondWithValidationError(c, fmt.Errorf(
			"avaliação final já registrada para este estudante no nível %s do ano letivo %s",
			req.AnoAcademicoAtual,
			anoLectivo,
		))
		return
	}

	// O nível informado deve corresponder ao nível atual do estudante para evitar
	// finalizações indevidas quando um nível incorreto é enviado no payload.
	if err := apoio.ValidarNivelAtualDoEstudante(estudanteDTO, tipoEnsino, req.AnoAcademicoAtual); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	if regra.AplicaSeReprovadoEmType != nil {
		prev, err := avaliacaoProj.GetResultadoByType(req.CodigoEstudante, academiaDTO.CodigoAcademia, anoLectivo, tipoEnsino, req.AnoAcademicoAtual, *regra.AplicaSeReprovadoEmType)
		if err != nil || prev == nil || prev.Aprovado {
			utils.RespondWithValidationError(c, fmt.Errorf("avaliação '%s' exige reprovação anterior em '%s'", req.Type, *regra.AplicaSeReprovadoEmType))
			return
		}
	}
	formulaExecucao := regra.Formula
	if tipoEnsino == "superior" {
		formulaExecucao = apoio.PreencherPeriodoFormulaSuperior(regra.Formula, req.AnoAcademicoAtual)
	}
	notasFormula, err := carregarNotasFormula(c, req.CodigoEstudante, academiaDTO.CodigoAcademia, anoLectivo, regra.CategoriasEnvolvidas)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	notaFinal, err := apoio.CalcularFormulaAvaliacao(formulaExecucao, notasFormula)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	aprovado := notaFinal >= regra.NotaMinimaAprovacao

	// ── Cálculo do próximo nível (backend) ────────────────────────────────────
	var proximoAnoAcademico *string
	var semestreAvaliado, proximoSemestre *int
	var anoSuperiorAntes, anoSuperiorDepois *string
	var motivoProgressao *string
	switch tipoEnsino {
	case "fundamental":
		proximoAnoAcademico, err = apoio.CalcularProximoAnoFundamental(req.AnoAcademicoAtual, aprovado)
		if err == nil {
			motivoProgressao = apoio.MotivoProgressaoFundamentalSemOferta(aprovado, proximoAnoAcademico, academiaDTO.AnosAcademicos)
		}
	case "medio":
		proximoAnoAcademico, err = apoio.CalcularProximoAnoCurso(c, cursoMedioUUID, req.AnoAcademicoAtual, aprovado)
	case "superior":
		proximoSemestre, anoSuperiorDepois, err = apoio.CalcularProximoSemestreCurso(c, cursoSuperiorUUID, estudanteDTO.SemestreAtual, aprovado)
		semestreAvaliado = estudanteDTO.SemestreAtual
		anoSuperiorAntes = estudanteDTO.AnoSuperior
		proximoAnoAcademico = anoSuperiorDepois
	}
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	// ── Aggregate ─────────────────────────────────────────────────────────────
	repository := apoio.GetRepository(c)
	estudanteAgg, err := repository.Load(estudanteDTO.ID, "Estudante")
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	estudante, ok := estudanteAgg.(*aggregates.Estudante)
	if !ok {
		utils.RespondWithInternalError(c, fmt.Errorf("erro ao converter agregado estudante"))
		return
	}

	// Captura turmas atuais ANTES de registrar avaliação final.
	// A remoção será aplicada de forma determinística na projeção de turmas ao
	// processar este mesmo evento AvaliacaoFinalAnoAcademico.
	turmasAtuais := apoio.BuscarTurmasDoEstudante(c, req.CodigoEstudante, academiaDTO.CodigoAcademia)
	var turmaAtual *string
	if len(turmasAtuais) > 0 {
		turmaAtual = &turmasAtuais[0]
	}

	if err := estudante.RegistrarAvaliacaoFinal(
		academiaDTO.CodigoAcademia,
		anoLectivo,
		tipoEnsino,
		req.AnoAcademicoAtual,
		proximoAnoAcademico,
		turmaAtual,
		turmasAtuais,
		aprovado,
		req.Observacao,
		req.Type,
		notaFinal,
		regra.NotaMinimaAprovacao,
		&regra.ID,
		formulaExecucao,
		regra.AplicaSeReprovadoEmType,
		apoio.CursoSnapshotAvaliacaoFinal(tipoEnsino, cursoMedioUUID, cursoSuperiorUUID),
		motivoProgressao,
		nil,
		false,
		nil,
		aggregates.AvaliacaoFinalSuperiorProgressao{
			SemestreAtualAvaliado: semestreAvaliado,
			ProximoSemestreAtual:  proximoSemestre,
			AnoSuperiorAntes:      anoSuperiorAntes,
			AnoSuperiorDepois:     anoSuperiorDepois,
		},
	); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	audit := db.AuditContext{
		UserID:   userID.String(),
		UserType: "academia",
		IP:       c.ClientIP(),
	}
	if err := repository.SaveWithAudit(estudante, audit); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	resultado := "reprovado"
	if aprovado {
		if proximoAnoAcademico != nil {
			resultado = fmt.Sprintf("aprovado → %s", *proximoAnoAcademico)
		} else {
			resultado = "aprovado (ciclo finalizado)"
		}
	}

	response := gin.H{
		"message":               "avaliação final registrada com sucesso",
		"nivel":                 tipoEnsino,
		"type":                  req.Type,
		"nota_final":            notaFinal,
		"nota_minima_aprovacao": regra.NotaMinimaAprovacao,
		"resultado":             resultado,
		"turmas_removidas":      turmasAtuais,
	}
	if motivoProgressao != nil {
		response["motivo_progressao"] = *motivoProgressao
		response["sem_oferta_do_proximo_ano_academico_na_academia"] = true
	}
	c.JSON(http.StatusCreated, response)
}

func ListarAvaliacoes(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)
	avaliacaoProj := apoio.GetAvaliacaoFinalProjection(c)
	filtros, err := parseFiltrosAvaliacaoFinal(c)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	switch userType {
	case "estudante":
		estudanteProj := apoio.GetEstudanteProjection(c)
		estudante, err := estudanteProj.GetByID(userID)
		if err != nil || estudante == nil {
			utils.RespondWithNotFoundError(c, "estudante")
			return
		}
		avaliacoes, err := avaliacaoProj.GetByEstudante(estudante.CodigoEstudante)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		avaliacoes = filtrarAvaliacoesMemoria(avaliacoes, filtros)
		avaliacoes, totalGeral, limit, offset := apoio.PaginarAvaliacoesFinais(c, avaliacoes)
		c.JSON(http.StatusOK, gin.H{"avaliacoes": avaliacoes, "total": len(avaliacoes), "total_geral": totalGeral, "limit": limit, "offset": offset})

	case "academia":
		academiaProj := apoio.GetAcademiaProjection(c)
		academiaDTO, err := academiaProj.GetByID(userID)
		if err != nil || academiaDTO == nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if filtros.CodigoAcademia != nil && *filtros.CodigoAcademia != academiaDTO.CodigoAcademia {
			utils.RespondWithForbiddenError(c, "academia só pode consultar os próprios dados")
			return
		}
		filtros.CodigoAcademia = &academiaDTO.CodigoAcademia
		avaliacoes, err := avaliacaoProj.ListByFilters(filtros.CodigoAcademia, nil, filtros.toProjectionFilters())
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		avaliacoes, totalGeral, limit, offset := apoio.PaginarAvaliacoesFinais(c, avaliacoes)
		c.JSON(http.StatusOK, gin.H{"avaliacoes": avaliacoes, "total": len(avaliacoes), "total_geral": totalGeral, "limit": limit, "offset": offset})

	default: // admin
		if filtros.CodigoTurma != nil && filtros.CodigoAcademia == nil {
			utils.RespondWithValidationError(c, fmt.Errorf("filtro codigo_turma exige codigo_academia para consultas admin"))
			return
		}
		avaliacoes, err := avaliacaoProj.ListByFilters(filtros.CodigoAcademia, nil, filtros.toProjectionFilters())
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		avaliacoes, totalGeral, limit, offset := apoio.PaginarAvaliacoesFinais(c, avaliacoes)
		c.JSON(http.StatusOK, gin.H{"avaliacoes": avaliacoes, "total": len(avaliacoes), "total_geral": totalGeral, "limit": limit, "offset": offset})
	}
}

// ============================================================================
// GET /avaliacoes-estudante/:codigo
// Apenas academia e admin (middleware.RequireAcademiaOuAdmin aplicado na rota).
// Academia → verifica se o estudante pertence à academia antes de retornar.
// Admin    → acesso irrestrito.
// ============================================================================

func GetAvaliacoesFinaisEstudante(c *gin.Context) {
	codigoEstudante := c.Param("codigo")
	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)

	estudanteProj := apoio.GetEstudanteProjection(c)
	estudante, err := estudanteProj.GetByCodigo(codigoEstudante)
	if err != nil || estudante == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}

	// SEC-12/13 handler defense in depth: bloquear estudante deletado
	if estudante.Status == "deletado" {
		utils.RespondWithError(c, http.StatusUnauthorized, "conta indisponível. Entre em contato com a sua academia ou com o suporte.", nil)
		return
	}

	if userType == "academia" {
		academiaProj := apoio.GetAcademiaProjection(c)
		academiaDTO, _ := academiaProj.GetByID(userID)
		if academiaDTO == nil {
			utils.RespondWithForbiddenError(c, "academia não encontrada")
			return
		}
		// SEC-12 handler defense in depth: bloquear se academia inativa
		if academiaDTO.Status != "ativo" {
			utils.RespondWithError(c, http.StatusUnauthorized, "academia inativa. Entre em contato com o administrador.", nil)
			return
		}
		// SEC-15: forced filtering via vinculos projection (isola mesmo se codigo_academia mudou ou estudante inativo)
		temAcesso, _, err := apoio.AcessoAcademiaAoEstudante(c, academiaDTO.CodigoAcademia, estudante)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if temAcesso == apoio.SemAcesso {
			utils.RespondWithForbiddenError(c, "Estudante não possui vínculo com esta academia")
			return
		}
	}

	avaliacaoProj := apoio.GetAvaliacaoFinalProjection(c)
	avaliacoes, err := avaliacaoProj.GetByEstudante(codigoEstudante)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	avaliacoes, totalGeral, limit, offset := apoio.PaginarAvaliacoesFinais(c, avaliacoes)
	c.JSON(http.StatusOK, gin.H{
		"codigo_estudante": codigoEstudante,
		"nome":             estudante.Nome,
		"avaliacoes":       avaliacoes,
		"total":            len(avaliacoes),
		"total_geral":      totalGeral,
		"limit":            limit,
		"offset":           offset,
	})
}

// ============================================================================
// GET /aprovacoes
// Estudante → suas próprias aprovações
// Academia  → aprovações dos estudantes da academia
// Admin     → todas as aprovações do sistema
// ============================================================================

func ListarAprovacoes(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)
	avaliacaoProj := apoio.GetAvaliacaoFinalProjection(c)
	filtros, err := parseFiltrosAvaliacaoFinal(c)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	aprovado := true

	switch userType {
	case "estudante":
		estudanteProj := apoio.GetEstudanteProjection(c)
		estudante, err := estudanteProj.GetByID(userID)
		if err != nil || estudante == nil {
			utils.RespondWithNotFoundError(c, "estudante")
			return
		}
		aprovacoes, err := avaliacaoProj.GetAprovacoesByEstudante(estudante.CodigoEstudante)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		aprovacoes = filtrarAvaliacoesMemoria(aprovacoes, filtros)
		aprovacoes, totalGeral, limit, offset := apoio.PaginarAvaliacoesFinais(c, aprovacoes)
		c.JSON(http.StatusOK, gin.H{"aprovacoes": aprovacoes, "total": len(aprovacoes), "total_geral": totalGeral, "limit": limit, "offset": offset})

	case "academia":
		academiaProj := apoio.GetAcademiaProjection(c)
		academiaDTO, err := academiaProj.GetByID(userID)
		if err != nil || academiaDTO == nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if filtros.CodigoAcademia != nil && *filtros.CodigoAcademia != academiaDTO.CodigoAcademia {
			utils.RespondWithForbiddenError(c, "academia só pode consultar os próprios dados")
			return
		}
		filtros.CodigoAcademia = &academiaDTO.CodigoAcademia
		aprovacoes, err := avaliacaoProj.ListByFilters(filtros.CodigoAcademia, &aprovado, filtros.toProjectionFilters())
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		aprovacoes, totalGeral, limit, offset := apoio.PaginarAvaliacoesFinais(c, aprovacoes)
		c.JSON(http.StatusOK, gin.H{"aprovacoes": aprovacoes, "total": len(aprovacoes), "total_geral": totalGeral, "limit": limit, "offset": offset})

	default: // admin
		if filtros.CodigoTurma != nil && filtros.CodigoAcademia == nil {
			utils.RespondWithValidationError(c, fmt.Errorf("filtro codigo_turma exige codigo_academia para consultas admin"))
			return
		}
		aprovacoes, err := avaliacaoProj.ListByFilters(filtros.CodigoAcademia, &aprovado, filtros.toProjectionFilters())
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		aprovacoes, totalGeral, limit, offset := apoio.PaginarAvaliacoesFinais(c, aprovacoes)
		c.JSON(http.StatusOK, gin.H{"aprovacoes": aprovacoes, "total": len(aprovacoes), "total_geral": totalGeral, "limit": limit, "offset": offset})
	}
}

// ============================================================================
// GET /reprovacoes
// ============================================================================

func ListarReprovacoes(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)
	avaliacaoProj := apoio.GetAvaliacaoFinalProjection(c)
	filtros, err := parseFiltrosAvaliacaoFinal(c)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	aprovado := false

	switch userType {
	case "estudante":
		estudanteProj := apoio.GetEstudanteProjection(c)
		estudante, err := estudanteProj.GetByID(userID)
		if err != nil || estudante == nil {
			utils.RespondWithNotFoundError(c, "estudante")
			return
		}
		reprovacoes, err := avaliacaoProj.GetReprovacoesByEstudante(estudante.CodigoEstudante)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		reprovacoes = filtrarAvaliacoesMemoria(reprovacoes, filtros)
		reprovacoes, totalGeral, limit, offset := apoio.PaginarAvaliacoesFinais(c, reprovacoes)
		c.JSON(http.StatusOK, gin.H{"reprovacoes": reprovacoes, "total": len(reprovacoes), "total_geral": totalGeral, "limit": limit, "offset": offset})

	case "academia":
		academiaProj := apoio.GetAcademiaProjection(c)
		academiaDTO, err := academiaProj.GetByID(userID)
		if err != nil || academiaDTO == nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if filtros.CodigoAcademia != nil && *filtros.CodigoAcademia != academiaDTO.CodigoAcademia {
			utils.RespondWithForbiddenError(c, "academia só pode consultar os próprios dados")
			return
		}
		filtros.CodigoAcademia = &academiaDTO.CodigoAcademia
		reprovacoes, err := avaliacaoProj.ListByFilters(filtros.CodigoAcademia, &aprovado, filtros.toProjectionFilters())
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		reprovacoes, totalGeral, limit, offset := apoio.PaginarAvaliacoesFinais(c, reprovacoes)
		c.JSON(http.StatusOK, gin.H{"reprovacoes": reprovacoes, "total": len(reprovacoes), "total_geral": totalGeral, "limit": limit, "offset": offset})

	default: // admin
		if filtros.CodigoTurma != nil && filtros.CodigoAcademia == nil {
			utils.RespondWithValidationError(c, fmt.Errorf("filtro codigo_turma exige codigo_academia para consultas admin"))
			return
		}
		reprovacoes, err := avaliacaoProj.ListByFilters(filtros.CodigoAcademia, &aprovado, filtros.toProjectionFilters())
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		reprovacoes, totalGeral, limit, offset := apoio.PaginarAvaliacoesFinais(c, reprovacoes)
		c.JSON(http.StatusOK, gin.H{"reprovacoes": reprovacoes, "total": len(reprovacoes), "total_geral": totalGeral, "limit": limit, "offset": offset})
	}
}

type filtrosAvaliacaoFinal struct {
	Nivel             *string
	AnoLectivo        *string
	AnoAcademicoAtual *string
	CodigoTurma       *string
	CodigoAcademia    *string
	Type              *string
}

func parseFiltrosAvaliacaoFinal(c *gin.Context) (filtrosAvaliacaoFinal, error) {
	parse := func(name string) *string {
		value := strings.TrimSpace(c.Query(name))
		if value == "" {
			return nil
		}
		return &value
	}
	f := filtrosAvaliacaoFinal{
		Nivel:             parse("nivel"),
		AnoLectivo:        parse("ano_letivo"),
		AnoAcademicoAtual: parse("ano_academico_atual"),
		CodigoTurma:       parse("codigo_turma"),
		CodigoAcademia:    parse("codigo_academia"),
		Type:              parse("type"),
	}
	if parse("tipo_ensino") != nil {
		return f, fmt.Errorf("campo legado tipo_ensino não é aceito; use nivel")
	}
	if f.Nivel != nil {
		switch *f.Nivel {
		case "fundamental", "medio", "superior":
		default:
			return f, fmt.Errorf("nivel deve ser: fundamental, medio ou superior")
		}
	}
	return f, nil
}

func (f filtrosAvaliacaoFinal) toProjectionFilters() projections.AvaliacaoFinalFilters {
	return projections.AvaliacaoFinalFilters{
		TipoEnsino:        f.Nivel,
		AnoLectivo:        f.AnoLectivo,
		AnoAcademicoAtual: f.AnoAcademicoAtual,
		CodigoTurma:       f.CodigoTurma,
		Type:              f.Type,
	}
}

func filtrarAvaliacoesMemoria(in []projections.AvaliacaoFinalDTO, f filtrosAvaliacaoFinal) []projections.AvaliacaoFinalDTO {
	out := make([]projections.AvaliacaoFinalDTO, 0, len(in))
	for _, a := range in {
		if f.Nivel != nil && a.TipoEnsino != *f.Nivel {
			continue
		}
		if f.AnoLectivo != nil && a.AnoLectivo != *f.AnoLectivo {
			continue
		}
		if f.AnoAcademicoAtual != nil && a.AnoAcademicoAtual != *f.AnoAcademicoAtual {
			continue
		}
		if f.Type != nil && a.Type != *f.Type {
			continue
		}
		out = append(out, a)
	}
	return out
}

// validarNotasParaAprovacao verifica se todas as notas obrigatórias estão presentes.
func validarNotasParaAprovacao(
	c *gin.Context,
	codigoEstudante string,
	anoLectivo string,
	tipoEnsino string,
	anoAcademicoAtual string,
	codigoAcademia string,
	cursoMedioID *uuid.UUID,
	cursoSuperiorID *uuid.UUID,
) error {
	materiasProj := apoio.GetMateriasProjection(c)
	notasProj := apoio.GetNotasProjection(c)

	todasMaterias, err := materiasProj.GetByAcademia(codigoAcademia)
	if err != nil {
		return fmt.Errorf("erro ao carregar matérias: %w", err)
	}

	var materiasFiltradas []projections.MateriaDTO
	var periodosEsperados []string
	var categoriaEsperada string

	switch tipoEnsino {
	case "fundamental":
		for _, m := range todasMaterias {
			if m.Type != "fundamental" {
				continue
			}
			for _, a := range m.AnosAcademicos {
				if a == anoAcademicoAtual {
					materiasFiltradas = append(materiasFiltradas, m)
					break
				}
			}
		}
		periodosEsperados = []string{"1_trimestre", "2_trimestre", "3_trimestre"}
		categoriaEsperada = "nota_escola"

	case "medio":
		if cursoMedioID == nil {
			return fmt.Errorf("estudante não possui curso médio vinculado")
		}
		cursosProj := apoio.GetCursosProjection(c)
		cursoDTO, err := cursosProj.GetByID(*cursoMedioID)
		if err != nil || cursoDTO == nil {
			return fmt.Errorf("curso médio não encontrado")
		}
		if len(cursoDTO.Periodos) > 0 {
			periodosEsperados = cursoDTO.Periodos
		} else {
			periodosEsperados = []string{"1_trimestre", "2_trimestre", "3_trimestre"}
		}
		for _, m := range todasMaterias {
			if m.Type != "medio" || m.CursoID == nil || *m.CursoID != *cursoMedioID {
				continue
			}
			for _, a := range m.AnosAcademicos {
				if a == anoAcademicoAtual {
					materiasFiltradas = append(materiasFiltradas, m)
					break
				}
			}
		}
		categoriaEsperada = "nota_escola"

	case "superior":
		if cursoSuperiorID == nil {
			return fmt.Errorf("estudante não possui curso superior vinculado")
		}
		cursosProj := apoio.GetCursosProjection(c)
		cursoDTO, err := cursosProj.GetByID(*cursoSuperiorID)
		if err != nil || cursoDTO == nil {
			return fmt.Errorf("curso superior não encontrado")
		}
		if len(cursoDTO.Periodos) == 0 {
			return fmt.Errorf("curso superior não possui períodos configurados")
		}
		for _, m := range todasMaterias {
			if m.Type != "superior" || m.CursoID == nil || *m.CursoID != *cursoSuperiorID {
				continue
			}
			if m.Periodo == nil || *m.Periodo == "" {
				log.Printf("[avaliacao-final] matéria '%s' sem periodo definido — ignorada na validação", m.Nome)
				continue
			}
			for _, a := range m.AnosAcademicos {
				if a == anoAcademicoAtual {
					materiasFiltradas = append(materiasFiltradas, m)
					break
				}
			}
		}
		categoriaEsperada = "nota_exame"
	}

	if len(materiasFiltradas) == 0 {
		return nil
	}

	todasNotas, err := notasProj.GetByEstudante(codigoEstudante)
	if err != nil {
		return fmt.Errorf("erro ao carregar notas: %w", err)
	}

	type notaKey struct{ materiaID, periodo, categoria string }
	notasExistentes := make(map[notaKey]bool)
	for _, n := range todasNotas {
		if n.AnoLectivo == anoLectivo && n.Categoria == categoriaEsperada {
			notasExistentes[notaKey{n.MateriaDisciplinarID, n.Periodo, n.Categoria}] = true
		}
	}

	var faltando []string
	if tipoEnsino == "superior" {
		for _, materia := range materiasFiltradas {
			periodo := *materia.Periodo
			key := notaKey{materia.ID.String(), periodo, categoriaEsperada}
			if !notasExistentes[key] {
				faltando = append(faltando, fmt.Sprintf("matéria '%s' — %s", materia.Nome, periodo))
			}
		}
	} else {
		for _, materia := range materiasFiltradas {
			for _, periodo := range periodosEsperados {
				key := notaKey{materia.ID.String(), periodo, categoriaEsperada}
				if !notasExistentes[key] {
					faltando = append(faltando, fmt.Sprintf("matéria '%s' — %s", materia.Nome, periodo))
				}
			}
		}
	}

	if len(faltando) > 0 {
		return fmt.Errorf(
			"notas de '%s' ausentes: %s. Preencha 'observacao' para forçar aprovação",
			categoriaEsperada,
			strings.Join(faltando, "; "),
		)
	}

	return nil
}
