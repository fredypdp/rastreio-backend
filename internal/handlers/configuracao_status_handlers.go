package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"spuri/internal/projections"
	"spuri/internal/utils"
)

// ============================================================================
// GET /academia/configuracao-status
//
// Consolida em UMA única consulta tudo que o "Guia de Configuração" do painel
// da academia (front end: GuiaConfiguracoesSection.tsx via o hook
// useAcademiaConfiguracaoStatus.ts) precisa saber sobre o que já foi
// configurado e o que ainda falta.
//
// Antes desta rota, o front end fazia até 8 chamadas em paralelo (ano letivo,
// anos acadêmicos, cursos, matérias, turmas, estudantes, categorias de nota e
// regras de avaliação final) e calculava tudo no navegador. Esta rota faz o
// mesmo cálculo aqui no back end e devolve apenas os booleanos (e algumas
// contagens) já prontos, na MESMA semântica usada hoje pelo front end,
// incluindo as mesmas particularidades:
//
//   - o 4º ano médio é ignorado na cobertura de matérias/turmas
//     (replicando `ignoreFourthYearMedio` do front end);
//   - "regras-superiores" é considerado completo com QUALQUER regra ativa
//     (não só regras com nivel="superior"), pois o campo "type" de uma regra
//     é sempre preenchido — isso replica fielmente o comportamento atual do
//     front end, não é uma correção de uma eventual inconsistência dele;
//   - estudantes com status='deletado' nunca contam, igual ao que
//     GET /estudantes já faz hoje.
//
// O passo "email-verificacao" do guia NÃO é incluído aqui de propósito: ele já
// é derivado, no front end, de dados que o usuário logado já tem em memória
// (user.academia.email / email_verificado, vindos do contexto de sessão), sem
// precisar de nenhuma chamada de API — não há nada para mover ao back end
// nesse passo específico.
// ============================================================================
func GetConfiguracaoStatusAcademia(c *gin.Context) {
	academiaDTO, ok := academiaAutenticada(c)
	if !ok {
		return
	}

	nivel := academiaDTO.Nivel
	nivelEscolar := ""
	if academiaDTO.NivelEscolar != nil {
		nivelEscolar = *academiaDTO.NivelEscolar
	}

	isFundamental := nivel == "escola" && (nivelEscolar == "fundamental" || nivelEscolar == "misto")
	needsCourses := nivel == "superior" || (nivel == "escola" && (nivelEscolar == "medio" || nivelEscolar == "misto"))
	isSuperior := nivel == "superior"

	// ano-letivo já está disponível na própria academiaDTO, sem consulta
	// extra (equivalente a GET /academia/ano-letivo).
	anoLetivoCompleto := academiaDTO.AnoLetivo != nil && *academiaDTO.AnoLetivo != ""

	// cursos ativos — só é buscado quando o nível exige cursos, replicando a
	// mesma condição que hoje decide, no front end, se
	// academiaService.listarCursos(token) é chamado ou não.
	var cursosAtivos []projections.CursoDTO
	if needsCourses {
		cursos, err := getCursosProjection(c).GetByAcademia(academiaDTO.CodigoAcademia)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		for _, curso := range cursos {
			if isStatusAtivoOuVazio(curso.Status) {
				cursosAtivos = append(cursosAtivos, curso)
			}
		}
	}

	// matérias ativas (sempre buscadas: usadas no passo "materias" mesmo
	// quando needsCourses é falso, para cobertura do fundamental).
	materiasTodas, err := getMateriasProjection(c).GetByAcademia(academiaDTO.CodigoAcademia)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	var materiasAtivas []projections.MateriaDTO
	for _, m := range materiasTodas {
		if isStatusAtivoOuVazio(m.Status) {
			materiasAtivas = append(materiasAtivas, m)
		}
	}

	// turmas ativas (sempre buscadas: usadas em "turmas" e
	// "estudantes-turmas").
	turmasTodas, err := getTurmasProjection(c).GetByAcademia(academiaDTO.CodigoAcademia)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	var turmasAtivas []projections.TurmaDTO
	for _, t := range turmasTodas {
		if isStatusAtivoOuVazio(t.Status) {
			turmasAtivas = append(turmasAtivas, t)
		}
	}

	fundamentalYears := academiaDTO.AnosAcademicos

	hasFundamentalCoverage := func(pred func(year string) bool) bool {
		if !isFundamental {
			return true
		}
		if len(fundamentalYears) == 0 {
			return false
		}
		for _, y := range fundamentalYears {
			if !pred(y) {
				return false
			}
		}
		return true
	}

	hasCourseCoverage := func(pred func(curso projections.CursoDTO, year string) bool, ignoreFourthYearMedio bool) bool {
		if !needsCourses {
			return true
		}
		if len(cursosAtivos) == 0 {
			return false
		}
		for _, curso := range cursosAtivos {
			years := make([]string, 0, len(curso.AnosAcademicos))
			for _, y := range curso.AnosAcademicos {
				if ignoreFourthYearMedio && curso.Type == "medio" && y == "4_ano_medio" {
					continue
				}
				years = append(years, y)
			}
			if len(years) == 0 {
				return false
			}
			for _, y := range years {
				if !pred(curso, y) {
					return false
				}
			}
		}
		return true
	}

	materiaComplete := hasFundamentalCoverage(func(year string) bool {
		for _, m := range materiasAtivas {
			if m.Type == "fundamental" && contemAno(m.AnosAcademicos, year) {
				return true
			}
		}
		return false
	}) && hasCourseCoverage(func(curso projections.CursoDTO, year string) bool {
		if curso.Type == "superior" {
			if len(curso.Periodos) == 0 {
				return false
			}
			for _, periodo := range curso.Periodos {
				encontrada := false
				for _, m := range materiasAtivas {
					if m.Type == "superior" && m.CursoID != nil && *m.CursoID == curso.ID &&
						m.Periodo != nil && *m.Periodo == periodo && contemAno(m.AnosAcademicos, year) {
						encontrada = true
						break
					}
				}
				if !encontrada {
					return false
				}
			}
			return true
		}
		for _, m := range materiasAtivas {
			if m.Type == "medio" && m.CursoID != nil && *m.CursoID == curso.ID && contemAno(m.AnosAcademicos, year) {
				return true
			}
		}
		return false
	}, true)

	turmaComplete := hasFundamentalCoverage(func(year string) bool {
		for _, t := range turmasAtivas {
			if t.CursoID == nil && t.Nivel == year {
				return true
			}
		}
		return false
	}) && hasCourseCoverage(func(curso projections.CursoDTO, year string) bool {
		for _, t := range turmasAtivas {
			if t.CursoID != nil && *t.CursoID == curso.ID && t.Nivel == year {
				return true
			}
		}
		return false
	}, true)

	estudantesTurmasComplete := hasFundamentalCoverage(func(year string) bool {
		for _, t := range turmasAtivas {
			if t.CursoID == nil && t.Nivel == year && len(t.Estudantes) > 0 {
				return true
			}
		}
		return false
	}) && hasCourseCoverage(func(curso projections.CursoDTO, year string) bool {
		for _, t := range turmasAtivas {
			if t.CursoID != nil && *t.CursoID == curso.ID && t.Nivel == year && len(t.Estudantes) > 0 {
				return true
			}
		}
		return false
	}, true)

	// estudantes: contagem + existência por nível via agregação no banco, em
	// vez de trazer a lista inteira de estudantes (que hoje pode disparar
	// dezenas de chamadas paginadas no front end para academias grandes).
	// Replica exatamente a mesma exclusão de deletados que GET /estudantes já
	// aplica.
	var totalEstudantes, temFundamental, temMedio, temSuperior int
	err = getDbClient(c).DB().QueryRow(`
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE NULLIF(ano_escolar_fundamental, '') IS NOT NULL),
			COUNT(*) FILTER (WHERE NULLIF(ano_escolar_medio, '') IS NOT NULL OR curso_medio_id IS NOT NULL),
			COUNT(*) FILTER (WHERE NULLIF(ano_superior, '') IS NOT NULL OR curso_superior_id IS NOT NULL)
		FROM projection_estudantes
		WHERE codigo_academia = $1 AND status <> 'deletado'
	`, academiaDTO.CodigoAcademia).Scan(&totalEstudantes, &temFundamental, &temMedio, &temSuperior)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	var estudantesCompleto bool
	if isSuperior {
		estudantesCompleto = temSuperior > 0
	} else {
		fundamentalOK := (nivelEscolar != "fundamental" && nivelEscolar != "misto") || temFundamental > 0
		medioOK := (nivelEscolar != "medio" && nivelEscolar != "misto") || temMedio > 0
		estudantesCompleto = fundamentalOK && medioOK
	}

	steps := gin.H{
		"ano-letivo": gin.H{"completed": anoLetivoCompleto},
		"materias":   gin.H{"completed": materiaComplete},
		"turmas":     gin.H{"completed": turmaComplete},
		"estudantes": gin.H{
			"completed": estudantesCompleto,
			"total":     totalEstudantes,
		},
		"estudantes-turmas": gin.H{"completed": estudantesTurmasComplete},
	}

	if needsCourses {
		steps["cursos"] = gin.H{
			"completed":    len(cursosAtivos) > 0,
			"total_ativos": len(cursosAtivos),
		}
	}

	if isSuperior {
		categoriasAtivas, err := getCategoriasNotaProjection(c).ListarPorAcademia(academiaDTO.CodigoAcademia)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		categoriasCompleto := hasCourseCoverage(func(_ projections.CursoDTO, year string) bool {
			for _, cat := range categoriasAtivas {
				if contemAno(cat.AnosAcademicos, year) {
					return true
				}
			}
			return false
		}, false)

		var totalRegrasAtivas int
		if err := getDbClient(c).DB().QueryRow(`
			SELECT COUNT(*) FROM projection_regras_avaliacao_final
			WHERE codigo_academia = $1 AND status = 'ativo'
		`, academiaDTO.CodigoAcademia).Scan(&totalRegrasAtivas); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}

		steps["categorias-superiores"] = gin.H{"completed": categoriasCompleto}
		steps["regras-superiores"] = gin.H{
			"completed":    totalRegrasAtivas > 0,
			"total_ativas": totalRegrasAtivas,
		}
	}

	c.JSON(http.StatusOK, gin.H{"steps": steps})
}

func isStatusAtivoOuVazio(status string) bool { return status == "ativo" || status == "" }

func contemAno(anos []string, ano string) bool {
	for _, a := range anos {
		if a == ano {
			return true
		}
	}
	return false
}
