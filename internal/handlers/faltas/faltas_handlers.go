package faltas

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

func campoPresenteNoPayload(c *gin.Context, campo string) (bool, error) {
	body, err := c.GetRawData()
	if err != nil {
		return false, fmt.Errorf("payload inválido")
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return false, nil
	}
	_, ok := raw[campo]
	return ok, nil
}

func rejeitarCamposImutaveisFalta(c *gin.Context, campos ...string) bool {
	body, err := c.GetRawData()
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("payload inválido"))
		return true
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		return false
	}
	for _, campo := range campos {
		if _, ok := raw[campo]; ok {
			utils.RespondWithValidationError(c, fmt.Errorf("campo não suportado em falta: %s", campo))
			return true
		}
	}
	return false
}

// ============================================================================
// POST /academia/faltas-aluno
// ============================================================================

func RegistrarFaltas(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)

	var req struct {
		CodigoEstudante      string     `json:"codigo_estudante"       binding:"required"`
		Data                 utils.Date `json:"data"                binding:"required"`
		Periodo              string     `json:"periodo"             binding:"required"`
		MateriaDisciplinarID string     `json:"materia_disciplinar_id" binding:"required"`
		Quantidade           int        `json:"quantidade"             binding:"required,min=1"`
		Observacao           *string    `json:"observacao"`
		SumarioID            *uuid.UUID `json:"sumario_id"`
	}

	if err := apoio.DecodeStrictJSON(c, &req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf(
			"dados obrigatórios: codigo_estudante, data, periodo, materia_disciplinar_id e quantidade",
		))
		return
	}
	if strings.TrimSpace(req.CodigoEstudante) == "" || strings.TrimSpace(req.Periodo) == "" || strings.TrimSpace(req.MateriaDisciplinarID) == "" || req.Quantidade <= 0 {
		utils.RespondWithValidationError(c, fmt.Errorf("dados obrigatorios: codigo_estudante, data, periodo, materia_disciplinar_id e quantidade"))
		return
	}

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
	if err := apoio.ValidarMatriculaEmAndamento(estudanteDTO); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := apoio.ValidarObservacao(req.Observacao); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if req.Quantidade > 100 {
		utils.RespondWithValidationError(c, fmt.Errorf("quantidade de faltas deve ser no máximo 100"))
		return
	}

	materiaID, err := uuid.Parse(req.MateriaDisciplinarID)
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("materia_disciplinar_id inválido"))
		return
	}
	materiasProj := apoio.GetMateriasProjection(c)
	materiaDTO, _ := materiasProj.GetByID(materiaID)
	if materiaDTO == nil || materiaDTO.CodigoAcademia != academiaDTO.CodigoAcademia {
		utils.RespondWithForbiddenError(c, "materia não pertence a esta academia")
		return
	}

	tipoLetivo, err := apoio.InferirTipoLetivoMateria(materiaDTO.Type)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := apoio.ValidarDataNoPeriodoLetivo(apoio.GetDbClient(c), tipoLetivo, anoLectivo, req.Data.Time); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	periodosValidos, err := apoio.ResolverPeriodosValidos(c, tipoLetivo, materiaDTO.CursoID)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if tipoLetivo == aggregates.TipoSuperior && materiaDTO.Periodo != nil && req.Periodo != *materiaDTO.Periodo {
		utils.RespondWithValidationError(c, fmt.Errorf(
			"periodo '%s' invalido para a materia '%s'. Periodo definido: '%s'",
			req.Periodo, materiaDTO.Nome, *materiaDTO.Periodo,
		))
		return
	}

	// Inferir anoAcademico com bloqueio de incompatibilidade estudante x matéria
	anoAcademico, err := apoio.InferirAnoAcademicoFaltas(estudanteDTO.AnoEscolar, materiaDTO.AnosAcademicos, materiaDTO.Nome, estudanteDTO.AnoEscolarMedio)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	var sumarioTitulo *string
	if req.SumarioID != nil {
		sumario, err := apoio.GetSumariosProjection(c).GetByID(*req.SumarioID)
		if err != nil || sumario == nil {
			utils.RespondWithNotFoundError(c, "sumario")
			return
		}
		if sumario.CodigoAcademia != academiaDTO.CodigoAcademia {
			utils.RespondWithForbiddenError(c, "sumário não pertence a esta academia")
			return
		}
		if sumario.MateriaID != materiaID.String() || sumario.Periodo != req.Periodo || sumario.AnoAcademico != anoAcademico {
			utils.RespondWithValidationError(c, fmt.Errorf("sumário incompatível com a falta"))
			return
		}
		sumarioTitulo = &sumario.SumarioTitulo
	}

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

	err = estudante.RegistrarFalta(
		academiaDTO.CodigoAcademia,
		anoLectivo,
		anoAcademico,
		req.Periodo,
		req.Data.Time,
		materiaID,
		req.Quantidade,
		req.Observacao,
		userID,
		periodosValidos,
		aggregates.MaxQuantidadeFaltasPadrao,
		req.SumarioID, sumarioTitulo,
	)
	if err != nil {
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

	log.Printf("Faltas registradas: %s - %d em %s (ano_academico=%s)",
		req.CodigoEstudante, req.Quantidade, materiaDTO.Nome, anoAcademico)

	c.JSON(http.StatusCreated, gin.H{
		"message":          "faltas registradas com sucesso",
		"estudante":        req.CodigoEstudante,
		"materia":          materiaDTO.Nome,
		"quantidade":       req.Quantidade,
		"periodo":          req.Periodo,
		"periodos_validos": periodosValidos,
		"ano_academico":    anoAcademico,
	})
}

func CorrigirFalta(c *gin.Context) {
	if rejeitarCamposImutaveisFalta(c, "periodo") {
		return
	}
	userID, _ := middleware.GetUserID(c)
	faltaID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("id da falta inválido"))
		return
	}
	var req struct {
		Quantidade int        `json:"quantidade" binding:"required,min=1"`
		Observacao *string    `json:"observacao"`
		Motivo     string     `json:"motivo" binding:"required"`
		SumarioID  *uuid.UUID `json:"sumario_id"`
	}
	if err := apoio.DecodeStrictJSON(c, &req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	sumarioIDPresente, err := campoPresenteNoPayload(c, "sumario_id")
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if sumarioIDPresente && req.SumarioID == nil {
		utils.RespondWithValidationError(c, fmt.Errorf("sumario_id não pode ser definido como null; use o endpoint de desvínculo"))
		return
	}
	if req.Quantidade > 100 || strings.TrimSpace(req.Motivo) == "" {
		utils.RespondWithValidationError(c, fmt.Errorf("quantidade inválida ou motivo da correção ausente"))
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
	falta, err := apoio.GetFaltasProjection(c).GetByID(faltaID.String())
	if err != nil || falta == nil {
		utils.RespondWithNotFoundError(c, "falta")
		return
	}
	if falta.CodigoAcademia != academia.CodigoAcademia {
		utils.RespondWithForbiddenError(c, "falta não pertence a esta academia")
		return
	}
	estudanteDTO, err := apoio.GetEstudanteProjection(c).GetByCodigo(falta.CodigoEstudante)
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
	materiaID, err := uuid.Parse(falta.MateriaDisciplinarID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	var novoSumarioTitulo *string
	if sumarioIDPresente {
		sumario, err := apoio.GetSumariosProjection(c).GetByID(*req.SumarioID)
		if err != nil || sumario == nil {
			utils.RespondWithNotFoundError(c, "sumario")
			return
		}
		if sumario.CodigoAcademia != academia.CodigoAcademia || sumario.MateriaID != falta.MateriaDisciplinarID || sumario.Periodo != falta.Periodo || sumario.AnoAcademico != falta.AnoAcademico {
			utils.RespondWithValidationError(c, fmt.Errorf("sumário incompatível com a falta"))
			return
		}
		novoSumarioTitulo = &sumario.SumarioTitulo
	}
	if err := estudante.CorrigirFalta(faltaID, academia.CodigoAcademia, falta.AnoLectivo, falta.Periodo, falta.Data.Time, materiaID, req.Quantidade, req.Observacao, req.Motivo, userID, aggregates.MaxQuantidadeFaltasPadrao, sumarioIDPresente, req.SumarioID, novoSumarioTitulo); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := apoio.GetRepository(c).SaveWithAudit(estudante, db.AuditContext{UserID: userID.String(), UserType: "academia", IP: c.ClientIP()}); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "falta corrigida com sucesso", "id": faltaID})
}

// ============================================================================
// GET /faltas-estudante/:codigo
// ============================================================================

func GetFaltasEstudante(c *gin.Context) {
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
		utils.RespondWithForbiddenError(c, "Você só pode visualizar suas próprias faltas")
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

	faltasProj := apoio.GetFaltasProjection(c)
	faltas, err := faltasProj.GetByEstudante(codigoEstudante)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	filtros, err := apoio.ParseFiltrosRegistrosEstudante(c, false)
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

	faltasFiltradas := make([]interface{}, 0, len(faltas))
	materiasProj := apoio.GetMateriasProjection(c)
	materiaMetaCache := map[string]apoio.MateriaMeta{}
	for _, falta := range faltas {
		if !apoio.MatchesFiltroString(filtros.AnoLectivos, falta.AnoLectivo) ||
			!apoio.MatchesFiltroString(filtros.AnoAcademicos, falta.AnoAcademico) ||
			!apoio.MatchesFiltroString(filtros.Periodos, falta.Periodo) ||
			!apoio.MatchesFiltroString(filtros.MateriasDisciplinares, falta.MateriaDisciplinarID) ||
			!apoio.MatchesFiltroString(filtros.CodigosAcademia, falta.CodigoAcademia) {
			continue
		}

		if len(filtros.CursoIDs) > 0 {
			materiaMetaAtual, err := apoio.GetMateriaMeta(materiasProj, materiaMetaCache, falta.MateriaDisciplinarID)
			if err != nil {
				utils.RespondWithInternalError(c, err)
				return
			}
			if !apoio.MatchesFiltroString(filtros.CursoIDs, materiaMetaAtual.CursoID) {
				continue
			}
		}

		faltasFiltradas = append(faltasFiltradas, falta)
	}

	c.JSON(http.StatusOK, gin.H{
		"codigo_estudante": codigoEstudante,
		"nome":             estudante.Nome,
		"faltas":           faltasFiltradas,
		"total":            len(faltasFiltradas),
	})
}

func DesvincularSumarioFalta(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("id inválido"))
		return
	}
	f, err := apoio.GetFaltasProjection(c).GetByID(id.String())
	if err != nil || f == nil {
		utils.RespondWithNotFoundError(c, "falta")
		return
	}
	a, err := apoio.GetAcademiaProjection(c).GetByID(userID)
	if err != nil || a == nil || f.CodigoAcademia != a.CodigoAcademia {
		utils.RespondWithForbiddenError(c, "falta não pertence a esta academia")
		return
	}
	if f.SumarioID == nil {
		c.JSON(http.StatusOK, gin.H{"message": "falta já não possui sumário vinculado", "id": id})
		return
	}
	student, err := apoio.GetEstudanteProjection(c).GetByCodigo(f.CodigoEstudante)
	if err != nil || student == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}
	agg, err := apoio.GetRepository(c).Load(student.ID, "Estudante")
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	e, ok := agg.(*aggregates.Estudante)
	if !ok {
		utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
		return
	}
	mid, err := uuid.Parse(f.MateriaDisciplinarID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if err := e.CorrigirFalta(id, a.CodigoAcademia, f.AnoLectivo, f.Periodo, f.Data.Time, mid, f.Quantidade, f.Observacao, "Sumário desvinculado via endpoint dedicado", userID, aggregates.MaxQuantidadeFaltasPadrao, true, nil, nil); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := apoio.GetRepository(c).SaveWithAudit(e, db.AuditContext{UserID: userID.String(), UserType: "academia", IP: c.ClientIP()}); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "sumário desvinculado com sucesso", "id": id})
}
