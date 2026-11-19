package notas

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
// POST /academia/notas-aluno
// ============================================================================

func RegistrarNota(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)

	var req struct {
		CodigoEstudante      string  `json:"codigo_estudante"       binding:"required"`
		Periodo              string  `json:"periodo"                binding:"required"`
		MateriaDisciplinarID string  `json:"materia_disciplinar_id" binding:"required"`
		Tipo                 string  `json:"tipo"                   binding:"required"`
		Categoria            string  `json:"categoria"              binding:"required"`
		Nota                 float64 `json:"nota"`
		Observacao           *string `json:"observacao"`
	}

	if err := apoio.DecodeStrictJSON(c, &req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf(
			"campos obrigatorios: codigo_estudante, periodo, "+
				"materia_disciplinar_id, tipo, categoria, nota",
		))
		return
	}
	if strings.TrimSpace(req.CodigoEstudante) == "" || strings.TrimSpace(req.Periodo) == "" || strings.TrimSpace(req.MateriaDisciplinarID) == "" || strings.TrimSpace(req.Tipo) == "" || strings.TrimSpace(req.Categoria) == "" {
		utils.RespondWithValidationError(c, fmt.Errorf("campos obrigatorios: codigo_estudante, periodo, materia_disciplinar_id, tipo e categoria"))
		return
	}

	utils.Debugf("[nota-debug] payload recebido: estudante=%s materia_id=%s tipo=%s categoria=%s periodo=%q", req.CodigoEstudante, req.MateriaDisciplinarID, req.Tipo, req.Categoria, req.Periodo)

	// Academia autenticada
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

	// Validar tipo de nota vs tipo de academia
	tipoEsperado := map[string]string{
		"escola":   "escolar",
		"superior": "superior",
	}[academiaDTO.Nivel]

	if req.Tipo != tipoEsperado {
		utils.Debugf(
			"[nota-debug] tipo incompatível com academia: academia_tipo=%s tipo_esperado=%s tipo_recebido=%s estudante=%s periodo=%q",
			academiaDTO.Nivel, tipoEsperado, req.Tipo, req.CodigoEstudante, req.Periodo,
		)
		utils.RespondWithValidationError(c, fmt.Errorf(
			"academia do tipo '%s' so pode registrar notas do tipo '%s'",
			academiaDTO.Nivel, tipoEsperado,
		))
		return
	}

	// Estudante
	estudanteProj := apoio.GetEstudanteProjection(c)
	estudanteDTO, err := estudanteProj.GetByCodigo(req.CodigoEstudante)
	if err != nil || estudanteDTO == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}
	if estudanteDTO.CodigoAcademia == nil || *estudanteDTO.CodigoAcademia != academiaDTO.CodigoAcademia {
		utils.RespondWithForbiddenError(c, "estudante nao pertence a esta academia")
		return
	}
	if err := apoio.ValidarMatriculaEmAndamento(estudanteDTO); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := apoio.ValidarObservacao(req.Observacao); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	// Materia
	materiaID, err := uuid.Parse(req.MateriaDisciplinarID)
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("materia_disciplinar_id invalido"))
		return
	}
	materiasProj := apoio.GetMateriasProjection(c)
	materiaDTO, _ := materiasProj.GetByID(materiaID)
	if materiaDTO == nil || materiaDTO.CodigoAcademia != academiaDTO.CodigoAcademia {
		utils.RespondWithForbiddenError(c, "materia nao pertence a esta academia")
		return
	}

	if req.Tipo == aggregates.TipoSuperior {
		utils.Debugf(
			"[nota-debug] validando regras de superior: materia=%s materia_status=%s materia_periodo=%v periodo_recebido=%q",
			materiaDTO.Nome, materiaDTO.Status, materiaDTO.Periodo, req.Periodo,
		)
		if materiaDTO.Status != "ativo" {
			utils.RespondWithValidationError(c, fmt.Errorf(
				"materia '%s' esta inativa. Ative-a antes de registrar notas",
				materiaDTO.Nome,
			))
			return
		}
		if materiaDTO.Periodo == nil || *materiaDTO.Periodo == "" {
			utils.RespondWithValidationError(c, fmt.Errorf(
				"materia '%s' nao possui periodo definido",
				materiaDTO.Nome,
			))
			return
		}
		if req.Periodo != *materiaDTO.Periodo {
			utils.RespondWithValidationError(c, fmt.Errorf(
				"periodo '%s' invalido para a materia '%s'. Periodo definido: '%s'",
				req.Periodo, materiaDTO.Nome, *materiaDTO.Periodo,
			))
			return
		}
	}

	// Resolver periodos validos para este contexto
	periodosValidos, err := apoio.ResolverPeriodosValidos(c, req.Tipo, materiaDTO.CursoID)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	utils.Debugf(
		"[nota-debug] periodos válidos resolvidos: tipo=%s curso_id=%v materia=%s periodo_recebido=%q periodos_validos=%v",
		req.Tipo, materiaDTO.CursoID, materiaDTO.Nome, req.Periodo, periodosValidos,
	)

	// Inferir AnoAcademico
	anoAcademico, err := apoio.InferirAnoAcademicoParaNota(estudanteDTO.AnoEscolar, materiaDTO.AnosAcademicos, materiaDTO.Nome, estudanteDTO.AnoEscolarMedio)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	// Categorias disponíveis para o ano: no fluxo escolar vêm do padrão fixo do sistema;
	// no superior continuam sendo categorias configuráveis da academia.
	modeloCursoMedio := apoio.ModeloCursoMedioDaMateria(c, materiaDTO)
	categoriasAdicionais := carregarCategoriasDisponiveisParaAno(c, academiaDTO.CodigoAcademia, anoAcademico)
	if req.Tipo == aggregates.TipoEscolar {
		categoriasAdicionais = apoio.CodigosCategoriasEscolaresFixasParaAno(anoAcademico, modeloCursoMedio)
	}
	if err := apoio.ValidarEscalaNotaPorAnoAcademico(anoAcademico, req.Nota); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	// Aggregate e comando
	repository := apoio.GetRepository(c)
	estudanteAgg, err := repository.Load(estudanteDTO.ID, "Estudante")
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	estudante, ok := estudanteAgg.(*aggregates.Estudante)
	if !ok {
		utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
		return
	}

	err = estudante.RegistrarNota(
		academiaDTO.CodigoAcademia,
		anoLectivo,
		anoAcademico,
		req.Periodo,
		materiaID,
		req.Tipo,
		req.Categoria,
		req.Nota,
		req.Observacao,
		categoriasAdicionais,
		periodosValidos,
		userID,
		apoio.LimiteNotaPorAnoAcademico(anoAcademico),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "periodo") || strings.Contains(strings.ToLower(err.Error()), "período") {
			utils.Debugf(
				"[nota-debug] falha de validação de período no aggregate: estudante=%s materia_id=%s tipo=%s categoria=%s periodo=%q periodos_validos=%v erro=%v",
				req.CodigoEstudante, materiaID, req.Tipo, req.Categoria, req.Periodo, periodosValidos, err,
			)
		}
		utils.RespondWithValidationError(c, err)
		return
	}

	tipoEnsino := apoio.InferirTipoEnsinoDoEstudante(estudanteDTO)
	avaliacoesAutomaticas, err := apoio.TentarAvaliacoesFinaisAutomaticas(
		c,
		estudante,
		estudanteDTO,
		academiaDTO.CodigoAcademia,
		anoLectivo,
		tipoEnsino,
		anoAcademico,
		req.Categoria,
		&apoio.NotaFormulaOverlay{MateriaID: materiaID.String(), Categoria: req.Categoria, Periodo: req.Periodo, Nota: req.Nota},
	)
	if err != nil {
		utils.RespondWithInternalError(c, fmt.Errorf("erro ao avaliar automaticamente avaliação final: %w", err))
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

	log.Printf("Nota registrada: estudante=%s tipo=%s categoria=%s materia=%s", req.CodigoEstudante, req.Tipo, req.Categoria, materiaDTO.Nome)

	c.JSON(http.StatusCreated, gin.H{
		"message":                       "nota registrada com sucesso",
		"estudante":                     req.CodigoEstudante,
		"materia":                       materiaDTO.Nome,
		"tipo":                          req.Tipo,
		"categoria":                     req.Categoria,
		"nota":                          req.Nota,
		"ano_academico":                 anoAcademico,
		"periodo":                       req.Periodo,
		"periodos_validos":              periodosValidos,
		"avaliacoes_finais_automaticas": avaliacoesAutomaticas,
	})
}

// CorrigirNota emits a compensating event; it never mutates the original ledger record.
func CorrigirNota(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)
	notaID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("id da nota inválido"))
		return
	}
	var req struct {
		Nota       float64 `json:"nota"`
		Observacao *string `json:"observacao"`
		Motivo     string  `json:"motivo" binding:"required"`
	}
	if err := apoio.DecodeStrictJSON(c, &req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if strings.TrimSpace(req.Motivo) == "" {
		utils.RespondWithValidationError(c, fmt.Errorf("motivo da correção é obrigatório"))
		return
	}
	if err := apoio.ValidarObservacao(req.Observacao); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	academia, err := apoio.GetAcademiaProjection(c).GetByID(userID)
	if err != nil || academia == nil {
		utils.RespondWithNotFoundError(c, "academia")
		return
	}
	nota, err := apoio.GetNotasProjection(c).GetNotaByID(notaID)
	if err != nil || nota == nil {
		utils.RespondWithNotFoundError(c, "nota")
		return
	}
	if nota.CodigoAcademia != academia.CodigoAcademia {
		utils.RespondWithForbiddenError(c, "nota não pertence a esta academia")
		return
	}
	estudanteDTO, err := apoio.GetEstudanteProjection(c).GetByCodigo(nota.CodigoEstudante)
	if err != nil || estudanteDTO == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}
	agg, err := apoio.GetRepository(c).Load(estudanteDTO.ID, "Estudante")
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	estudante, ok := agg.(*aggregates.Estudante)
	if !ok {
		utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
		return
	}
	materiaID, err := uuid.Parse(nota.MateriaDisciplinarID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if err := estudante.CorrigirNota(notaID, academia.CodigoAcademia, nota.AnoLectivo, nota.Periodo, materiaID, nota.Tipo, nota.Categoria, req.Nota, req.Observacao, req.Motivo, userID, apoio.LimiteNotaPorAnoAcademico(nota.AnoAcademico)); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if _, err := apoio.TentarAvaliacoesFinaisAutomaticas(c, estudante, estudanteDTO, academia.CodigoAcademia, nota.AnoLectivo, apoio.InferirTipoEnsinoDoEstudante(estudanteDTO), nota.AnoAcademico, nota.Categoria, &apoio.NotaFormulaOverlay{MateriaID: materiaID.String(), Categoria: nota.Categoria, Periodo: nota.Periodo, Nota: req.Nota}); err != nil {
		utils.RespondWithInternalError(c, fmt.Errorf("erro ao recalcular avaliação final: %w", err))
		return
	}
	if err := apoio.GetRepository(c).SaveWithAudit(estudante, db.AuditContext{UserID: userID.String(), UserType: "academia", IP: c.ClientIP()}); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "nota corrigida com sucesso", "id": notaID})
}

// ============================================================================
// GET /notas-estudante/:codigo
// ============================================================================

func GetNotasEstudante(c *gin.Context) {
	codigoEstudante := c.Param("codigo")

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

	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)

	if userType == "estudante" && userID != estudante.ID {
		utils.RespondWithForbiddenError(c, "Você só pode visualizar suas próprias notas")
		return
	}

	var academiaDTO *projections.AcademiaDTO
	if userType == "academia" {
		academiaProj := apoio.GetAcademiaProjection(c)
		var err error
		academiaDTO, err = academiaProj.GetByID(userID)
		if err != nil || academiaDTO == nil {
			utils.RespondWithNotFoundError(c, "academia")
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

	notasProj := apoio.GetNotasProjection(c)
	notas, err := notasProj.GetByEstudante(codigoEstudante)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	filtros, err := apoio.ParseFiltrosRegistrosEstudante(c, true)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	if userType == "academia" {
		// SEC-15: forçar filtro por codigo_academia da academia autenticada
		filtros.CodigosAcademia = []string{academiaDTO.CodigoAcademia}
	}
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	notasFiltradas := make([]interface{}, 0, len(notas))
	materiasProj := apoio.GetMateriasProjection(c)
	materiaMetaCache := map[string]apoio.MateriaMeta{}
	for _, nota := range notas {
		if !apoio.MatchesFiltroString(filtros.AnoLectivos, nota.AnoLectivo) ||
			!apoio.MatchesFiltroString(filtros.AnoAcademicos, nota.AnoAcademico) ||
			!apoio.MatchesFiltroString(filtros.Periodos, nota.Periodo) ||
			!apoio.MatchesFiltroString(filtros.MateriasDisciplinares, nota.MateriaDisciplinarID) ||
			!apoio.MatchesFiltroString(filtros.CodigosAcademia, nota.CodigoAcademia) ||
			!apoio.MatchesFiltroString(filtros.Categorias, nota.Categoria) {
			continue
		}

		if len(filtros.CursoIDs) > 0 {
			materiaMetaAtual, err := apoio.GetMateriaMeta(materiasProj, materiaMetaCache, nota.MateriaDisciplinarID)
			if err != nil {
				utils.RespondWithInternalError(c, err)
				return
			}
			if !apoio.MatchesFiltroString(filtros.CursoIDs, materiaMetaAtual.CursoID) {
				continue
			}
		}

		notasFiltradas = append(notasFiltradas, nota)
	}

	c.JSON(http.StatusOK, gin.H{
		"codigo_estudante": codigoEstudante,
		"nome":             estudante.Nome,
		"notas":            notasFiltradas,
		"total":            len(notasFiltradas),
	})
}

// ============================================================================
// POST /academia/categorias-nota
// ============================================================================

// CriarCategoriaNota cria uma categoria de nota configurável para o ensino superior.
// Escolas usam o padrão avaliativo fixo do sistema e não podem manipular categorias.
func CriarCategoriaNota(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)

	var req struct {
		Codigo         string   `json:"codigo"           binding:"required"`
		Nome           string   `json:"nome"             binding:"required"`
		Descricao      *string  `json:"descricao"`
		AnosAcademicos []string `json:"anos_academicos" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("codigo, nome e anos_academicos são obrigatorios"))
		return
	}

	academiaProj := apoio.GetAcademiaProjection(c)
	academiaDTO, err := academiaProj.GetByID(userID)
	if err != nil || academiaDTO == nil {
		utils.RespondWithNotFoundError(c, "academia")
		return
	}
	if academiaDTO.Nivel != "superior" {
		utils.RespondWithValidationError(c, fmt.Errorf("categorias de nota escolares são fixas do sistema; crie categorias apenas para ensino superior"))
		return
	}

	categoriasProj := apoio.GetCategoriasNotaProjection(c)
	categoriasExistentes, _ := categoriasProj.GetCodigosByAcademia(academiaDTO.CodigoAcademia)

	repository := apoio.GetRepository(c)
	agg, err := repository.Load(userID, "Academia")
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	academia, ok := agg.(*aggregates.Academia)
	if !ok {
		utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
		return
	}

	if err := academia.AdicionarCategoriaNota(req.Codigo, req.Nome, req.Descricao, userID, categoriasExistentes, req.AnosAcademicos); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	if err := repository.Save(academia); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	log.Printf("Categoria de nota criada: %s (%s) para academia %s", req.Codigo, req.Nome, academiaDTO.CodigoAcademia)

	c.JSON(http.StatusCreated, gin.H{
		"message":   "categoria criada com sucesso",
		"categoria": req.Codigo,
	})
}

// ============================================================================
// GET /academia/categorias-nota
// ============================================================================

// ListarCategoriasNota retorna as categorias de nota da academia resolvida.
// Para escolas, retorna as categorias fixas do sistema; para superior, retorna as configuráveis.
// Disponível para academias, admins (informando codigo_academia) e estudantes vinculados à academia.
func ListarCategoriasNota(c *gin.Context) {
	academiaDTO, err := getAcademiaCategoriasNotaTarget(c)
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "nunca esteve vinculado") {
			utils.RespondWithForbiddenError(c, err.Error())
			return
		}
		if strings.Contains(msg, "deve informar") || strings.Contains(msg, "sem academia associada") {
			utils.RespondWithValidationError(c, err)
			return
		}
		if strings.Contains(msg, "estudante não encontrado") {
			utils.RespondWithNotFoundError(c, "estudante")
			return
		}
		utils.RespondWithInternalError(c, err)
		return
	}
	if academiaDTO == nil {
		utils.RespondWithNotFoundError(c, "academia")
		return
	}
	if academiaDTO.Nivel == "escola" {
		categorias := apoio.CategoriasEscolaresFixasDaAcademia(c, academiaDTO)
		c.JSON(http.StatusOK, gin.H{
			"categorias": categorias,
			"total":      len(categorias),
		})
		return
	}
	if academiaDTO.Nivel != "superior" {
		utils.RespondWithValidationError(c, fmt.Errorf("nivel da academia inválido para consulta de categorias de nota"))
		return
	}
	categoriasProj := apoio.GetCategoriasNotaProjection(c)
	categorias, err := categoriasProj.ListarPorAcademia(academiaDTO.CodigoAcademia)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"categorias": categorias,
		"total":      len(categorias),
	})
}

func getAcademiaCategoriasNotaTarget(c *gin.Context) (*projections.AcademiaDTO, error) {
	userType, _ := middleware.GetUserType(c)
	academiaProj := apoio.GetAcademiaProjection(c)

	if userType == "admin" {
		codigoAcademia := strings.TrimSpace(c.Query("codigo_academia"))
		if codigoAcademia == "" {
			return nil, fmt.Errorf("admin deve informar ?codigo_academia=CODIGO")
		}
		return academiaProj.GetByCodigo(codigoAcademia)
	}
	if userType == "estudante" {
		userID, _ := middleware.GetUserID(c)
		estudanteProj := apoio.GetEstudanteProjection(c)
		estudanteDTO, err := estudanteProj.GetByID(userID)
		if err != nil || estudanteDTO == nil {
			return nil, fmt.Errorf("estudante não encontrado")
		}

		codigoAcademia := strings.TrimSpace(c.Query("codigo_academia"))
		if codigoAcademia == "" && estudanteDTO.CodigoAcademia != nil {
			codigoAcademia = *estudanteDTO.CodigoAcademia
		}
		if codigoAcademia == "" {
			return nil, fmt.Errorf("estudante sem academia associada")
		}

		teveVinculo, err := estudanteTeveVinculoComAcademia(c, userID, estudanteDTO, codigoAcademia)
		if err != nil {
			return nil, err
		}
		if !teveVinculo {
			return nil, fmt.Errorf("estudante nunca esteve vinculado a esta academia")
		}
		return academiaProj.GetByCodigo(codigoAcademia)
	}

	userID, _ := middleware.GetUserID(c)
	return academiaProj.GetByID(userID)
}

func estudanteTeveVinculoComAcademia(c *gin.Context, estudanteID uuid.UUID, estudanteDTO *projections.EstudanteDTO, codigoAcademia string) (bool, error) {
	codigoAcademia = strings.TrimSpace(codigoAcademia)
	if codigoAcademia == "" {
		return false, nil
	}
	if estudanteDTO != nil && estudanteDTO.CodigoAcademia != nil && *estudanteDTO.CodigoAcademia == codigoAcademia {
		return true, nil
	}

	dbClient := apoio.GetDbClient(c)
	if dbClient == nil {
		return false, fmt.Errorf("cliente de banco indisponível")
	}

	var exists bool
	err := dbClient.DB().QueryRow(`
		SELECT EXISTS (
			SELECT 1
			FROM spuri_ledger
			WHERE aggregate_id = $1
			  AND aggregate_type = 'Estudante'
			  AND event_type IN ('EstudanteCriadoComVinculo', 'EstudanteReintegrado', 'EstudanteDesvinculadoDaAcademia')
			  AND payload->>'CodigoAcademia' = $2
		)
	`, estudanteID, codigoAcademia).Scan(&exists)
	return exists, err
}

// DeletarCategoriaNota remove (inativa) uma categoria de nota adicional da academia.
// Rota: DELETE /academia/categorias-nota/:codigo
func DeletarCategoriaNota(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)
	codigoCategoria := strings.TrimSpace(c.Param("codigo"))
	if codigoCategoria == "" {
		utils.RespondWithValidationError(c, fmt.Errorf("codigo da categoria é obrigatório"))
		return
	}

	academiaProj := apoio.GetAcademiaProjection(c)
	academiaDTO, err := academiaProj.GetByID(userID)
	if err != nil || academiaDTO == nil {
		utils.RespondWithNotFoundError(c, "academia")
		return
	}
	if academiaDTO.Nivel != "superior" {
		utils.RespondWithValidationError(c, fmt.Errorf("categorias de nota escolares são fixas do sistema e não podem ser removidas"))
		return
	}

	categoriasProj := apoio.GetCategoriasNotaProjection(c)
	categoriasExistentes, _ := categoriasProj.GetCodigosByAcademia(academiaDTO.CodigoAcademia)

	repository := apoio.GetRepository(c)
	agg, err := repository.Load(userID, "Academia")
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	academia, ok := agg.(*aggregates.Academia)
	if !ok {
		utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
		return
	}

	if err := academia.RemoverCategoriaNota(codigoCategoria, userID, categoriasExistentes); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	if err := repository.Save(academia); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "categoria removida com sucesso",
		"categoria": codigoCategoria,
	})
}

// carregarCategoriasDisponiveisParaAno retorna os códigos das categorias de nota
// configuradas pela academia para o ano acadêmico informado. Categorias sem
// anos_academicos ou sem correspondência com o ano são ignoradas, bloqueando o
// registro no aggregate.
func carregarCategoriasDisponiveisParaAno(c *gin.Context, codigoAcademia, anoAcademico string) []string {
	categoriasProj := apoio.GetCategoriasNotaProjection(c)
	categorias, err := categoriasProj.ListarPorAcademia(codigoAcademia)
	if err != nil {
		return []string{}
	}
	codigos := make([]string, 0, len(categorias))
	for _, cat := range categorias {
		for _, ano := range cat.AnosAcademicos {
			if ano == anoAcademico {
				codigos = append(codigos, cat.Codigo)
				break
			}
		}
	}
	return codigos
}

// validarNota verifica se a nota é maior ou igual a 0.
func validarNota(nota float64) error {
	if nota < 0 {
		return fmt.Errorf("nota deve ser maior ou igual a 0, recebido: %.2f", nota)
	}
	return nil
}

// NOTE: utils é importado via utils "spuri/internal/utils"
var _ = utils.RespondWithValidationError
