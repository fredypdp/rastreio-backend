package materias

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/cadastros"
	"spuri/internal/db"
	"spuri/internal/handlers/apoio"
	"spuri/internal/middleware"
	"spuri/internal/utils"
)

// ============================================================================
// POST /academia/materias
// ============================================================================

func CriarMateria(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)

	var req struct {
		Nome                    string     `json:"nome"            binding:"required"`
		Type                    *string    `json:"type"`
		AnosAcademicos          []string   `json:"anos_academicos"`
		CursoID                 *uuid.UUID `json:"curso_id"`
		Periodo                 *string    `json:"periodo"`
		PendenciaPermitida      *bool      `json:"pendencia_permitida"`
		PendenciaNivelConclusao *string    `json:"pendencia_nivel_conclusao"`
	}

	if err := apoio.DecodeStrictJSON(c, &req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if strings.TrimSpace(req.Nome) == "" {
		utils.RespondWithValidationError(c, fmt.Errorf("nome é obrigatório"))
		return
	}

	academiaProj := apoio.GetAcademiaProjection(c)
	academiaDTO, err := academiaProj.GetByID(userID)
	if err != nil || academiaDTO == nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	if academiaDTO.Status != "ativo" {
		utils.RespondWithForbiddenError(c, "Academia inativa nao pode criar materias")
		return
	}

	tipoMateria, err := resolverTipoMateria(academiaDTO.Nivel, academiaDTO.NivelEscolar, req.Type)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	var periodosCurso []string
	modeloCursoMedio := ""
	if (tipoMateria == "medio" || tipoMateria == "superior") && req.CursoID != nil {
		cursosProj := apoio.GetCursosProjection(c)
		cursoDTO, _ := cursosProj.GetByID(*req.CursoID)

		if cursoDTO == nil {
			utils.RespondWithNotFoundError(c, "curso")
			return
		}

		if cursoDTO.Status != "ativo" {
			utils.RespondWithValidationError(c, fmt.Errorf("curso inativo nao pode ter materias"))
			return
		}

		if cursoDTO.CodigoAcademia != academiaDTO.CodigoAcademia {
			utils.RespondWithForbiddenError(c, "Curso nao pertence a esta academia")
			return
		}
		periodosCurso = cursoDTO.Periodos
		if tipoMateria == "medio" {
			modeloCursoMedio = strings.TrimSpace(cursoDTO.Modelo)
		}

		// Para superior: garantir que o curso tem periodos definidos
		if tipoMateria == "superior" && len(cursoDTO.Periodos) == 0 {
			utils.RespondWithValidationError(c, fmt.Errorf(
				"o curso '%s' nao possui periodos definidos. "+
					"Atualize o curso antes de criar materias superiores",
				cursoDTO.Nome,
			))
			return
		}
	}

	if tipoMateria != "superior" && req.PendenciaPermitida != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("pendencia_permitida é exclusiva do ensino superior e não se aplica a matérias escolares"))
		return
	}
	if tipoMateria != "superior" && req.PendenciaNivelConclusao != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("pendencia_nivel_conclusao é exclusiva do ensino superior e não se aplica a matérias escolares"))
		return
	}
	if err := validarPendenciaNivelConclusao(tipoMateria, req.PendenciaNivelConclusao, req.AnosAcademicos, periodosCurso); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := validarAnosAcademicosMateria(tipoMateria, req.AnosAcademicos, modeloCursoMedio); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	materia, err := cadastros.NovaMateria(req.Nome, tipoMateria, req.AnosAcademicos, academiaDTO.CodigoAcademia, req.CursoID, req.PendenciaPermitida, req.PendenciaNivelConclusao)
	if err != nil {
		apoio.RespondErroCadastro(c, err, "materia")
		return
	}
	if tipoMateria != "superior" && req.Periodo != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("periodo só pode ser definido para matérias do tipo superior"))
		return
	}
	if tipoMateria == "superior" {
		if req.Periodo == nil || *req.Periodo == "" {
			utils.RespondWithValidationError(c, fmt.Errorf("periodo é obrigatório para matérias do tipo superior"))
			return
		}
		if len(periodosCurso) > 0 && !apoio.ContainsString(periodosCurso, *req.Periodo) {
			utils.RespondWithValidationError(c, fmt.Errorf(
				"periodo '%s' nao pertence ao curso vinculado. Periodos disponiveis: %v",
				*req.Periodo, periodosCurso,
			))
			return
		}
		if err := materia.DefinirPeriodo(*req.Periodo); err != nil {
			apoio.RespondErroCadastro(c, err, "materia")
			return
		}
	}

	audit := db.AuditContext{
		UserID:   userID.String(),
		UserType: "academia",
		IP:       c.ClientIP(),
	}
	if err := cadastros.NovoMateriasRepo(apoio.GetDbClient(c)).Criar(c.Request.Context(), audit, materia); err != nil {
		apoio.RespondErroCadastro(c, err, "materia")
		return
	}

	data := gin.H{
		"id":      materia.ID,
		"nome":    materia.Nome,
		"type":    materia.Type,
		"status":  materia.Status,
		"periodo": materia.Periodo,
	}
	if materia.Type == "superior" {
		data["pendencia_permitida"] = materia.PendenciaPermitida
		data["pendencia_nivel_conclusao"] = materia.PendenciaNivelConclusao
	}
	c.JSON(http.StatusCreated, gin.H{
		"message": "materia criada com sucesso",
		"data":    data,
	})
}

// ============================================================================
// GET /academia/materias
// ============================================================================

func ListarMaterias(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)

	academiaProj := apoio.GetAcademiaProjection(c)
	codigoAcademia := ""
	if userType == "admin" {
		codigoAcademia = c.Query("codigo_academia")
		if codigoAcademia == "" {
			utils.RespondWithValidationError(c, fmt.Errorf("codigo_academia é obrigatório para admin"))
			return
		}
		academiaDTO, err := academiaProj.GetByCodigo(codigoAcademia)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if academiaDTO == nil {
			utils.RespondWithNotFoundError(c, "academia")
			return
		}
	} else {
		academiaDTO, err := academiaProj.GetByID(userID)
		if err != nil || academiaDTO == nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		codigoAcademia = academiaDTO.CodigoAcademia
	}

	materiasProj := apoio.GetMateriasProjection(c)
	materias, err := materiasProj.GetByAcademia(codigoAcademia)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"materias": materias,
		"total":    len(materias),
	})
}

// ============================================================================
// PUT /academia/materias/:id/ativar
// ============================================================================

func AtivarMateria(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)

	materiaID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("ID de materia invalido"))
		return
	}

	materiasProj := apoio.GetMateriasProjection(c)
	materiaDTO, err := materiasProj.GetByID(materiaID)
	if err != nil || materiaDTO == nil {
		utils.RespondWithNotFoundError(c, "materia")
		return
	}

	academiaProj := apoio.GetAcademiaProjection(c)
	academiaDTO, _ := academiaProj.GetByID(userID)
	if academiaDTO == nil || academiaDTO.CodigoAcademia != materiaDTO.CodigoAcademia {
		utils.RespondWithForbiddenError(c, "Materia nao pertence a esta academia")
		return
	}

	audit := db.AuditContext{
		UserID:   userID.String(),
		UserType: "academia",
		IP:       c.ClientIP(),
	}
	materia, err := cadastros.NovoMateriasRepo(apoio.GetDbClient(c)).Atualizar(
		c.Request.Context(), audit, materiaID, "MateriaAtivada", "",
		func(m *cadastros.Materia) error { return m.Ativar() })
	if err != nil {
		apoio.RespondErroCadastro(c, err, "materia")
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "materia ativada com sucesso", "nome": materia.Nome})
}

// ============================================================================
// PUT /academia/materias/:id/desativar
// ============================================================================

func DesativarMateria(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)

	materiaID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("ID de materia invalido"))
		return
	}

	materiasProj := apoio.GetMateriasProjection(c)
	materiaDTO, err := materiasProj.GetByID(materiaID)
	if err != nil || materiaDTO == nil {
		utils.RespondWithNotFoundError(c, "materia")
		return
	}

	academiaProj := apoio.GetAcademiaProjection(c)
	academiaDTO, _ := academiaProj.GetByID(userID)
	if academiaDTO == nil || academiaDTO.CodigoAcademia != materiaDTO.CodigoAcademia {
		utils.RespondWithForbiddenError(c, "Materia nao pertence a esta academia")
		return
	}

	audit := db.AuditContext{
		UserID:   userID.String(),
		UserType: "academia",
		IP:       c.ClientIP(),
	}
	materia, err := cadastros.NovoMateriasRepo(apoio.GetDbClient(c)).Atualizar(
		c.Request.Context(), audit, materiaID, "MateriaDesativada", "",
		func(m *cadastros.Materia) error { return m.Desativar() })
	if err != nil {
		apoio.RespondErroCadastro(c, err, "materia")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "materia desativada com sucesso",
		"nome":    materia.Nome,
	})
}

// ============================================================================
// PUT /academia/materias/:id
// ============================================================================

func AtualizarDadosMateria(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)

	materiaID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("ID de matéria inválido"))
		return
	}

	var req struct {
		Nome                    *string          `json:"nome"`
		AnosAcademicos          *[]string        `json:"anos_academicos"`
		CursoID                 *uuid.UUID       `json:"curso_id"`
		Periodo                 *json.RawMessage `json:"periodo"`
		PendenciaPermitida      *bool            `json:"pendencia_permitida"`
		PendenciaNivelConclusao *string          `json:"pendencia_nivel_conclusao"`
	}

	if err := apoio.DecodeStrictJSON(c, &req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if req.Periodo != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("periodo não pode ser editado; defina-o no POST /academia/materia"))
		return
	}

	materiasProj := apoio.GetMateriasProjection(c)
	materiaDTO, err := materiasProj.GetByID(materiaID)
	if err != nil || materiaDTO == nil {
		utils.RespondWithNotFoundError(c, "matéria")
		return
	}

	academiaProj := apoio.GetAcademiaProjection(c)
	academiaDTO, _ := academiaProj.GetByID(userID)
	if academiaDTO == nil || academiaDTO.CodigoAcademia != materiaDTO.CodigoAcademia {
		utils.RespondWithForbiddenError(c, "Matéria não pertence a esta academia")
		return
	}

	var anos []string
	if req.AnosAcademicos != nil {
		anos = *req.AnosAcademicos
	}
	anosParaValidacao := materiaDTO.AnosAcademicos
	if req.AnosAcademicos != nil {
		anosParaValidacao = *req.AnosAcademicos
	}
	var periodosCurso []string
	modeloCursoMedio := ""
	cursoIDParaValidacao := materiaDTO.CursoID
	if req.CursoID != nil {
		cursoIDParaValidacao = req.CursoID
	}
	if (materiaDTO.Type == "medio" || materiaDTO.Type == "superior") && cursoIDParaValidacao != nil {
		cursosProj := apoio.GetCursosProjection(c)
		cursoDTO, err := cursosProj.GetByID(*cursoIDParaValidacao)
		if err == nil && cursoDTO != nil {
			periodosCurso = cursoDTO.Periodos
			if materiaDTO.Type == "medio" {
				modeloCursoMedio = strings.TrimSpace(cursoDTO.Modelo)
			}
		}
	}
	if materiaDTO.Type != "superior" && req.PendenciaPermitida != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("pendencia_permitida é exclusiva do ensino superior e não se aplica a matérias escolares"))
		return
	}
	if materiaDTO.Type != "superior" && req.PendenciaNivelConclusao != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("pendencia_nivel_conclusao é exclusiva do ensino superior e não se aplica a matérias escolares"))
		return
	}
	if err := validarPendenciaNivelConclusao(materiaDTO.Type, req.PendenciaNivelConclusao, anosParaValidacao, periodosCurso); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if req.AnosAcademicos != nil {
		if err := validarAnosAcademicosMateria(materiaDTO.Type, *req.AnosAcademicos, modeloCursoMedio); err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
	}
	audit := db.AuditContext{
		UserID:   userID.String(),
		UserType: "academia",
		IP:       c.ClientIP(),
	}
	materia, err := cadastros.NovoMateriasRepo(apoio.GetDbClient(c)).Atualizar(
		c.Request.Context(), audit, materiaID, "MateriaDadosAtualizados", "",
		func(m *cadastros.Materia) error {
			return m.AtualizarDados(req.Nome, anos, req.CursoID, req.PendenciaPermitida, req.PendenciaNivelConclusao)
		})
	if err != nil {
		apoio.RespondErroCadastro(c, err, "materia")
		return
	}

	response := gin.H{
		"message": "matéria atualizada com sucesso",
		"nome":    materia.Nome,
	}
	if materia.Type == "superior" {
		response["pendencia_permitida"] = materia.PendenciaPermitida
		response["pendencia_nivel_conclusao"] = materia.PendenciaNivelConclusao
	}
	c.JSON(http.StatusOK, response)
}

func resolverTipoMateria(nivelAcademia string, nivelEscolar *string, tipoReq *string) (string, error) {
	if nivelAcademia == "superior" {
		return "superior", nil
	}
	if nivelAcademia != "escola" || nivelEscolar == nil {
		return "", fmt.Errorf("não foi possível inferir o tipo da matéria para esta academia")
	}
	switch *nivelEscolar {
	case "fundamental":
		return "fundamental", nil
	case "medio":
		return "medio", nil
	case "misto":
		if tipoReq == nil {
			return "", fmt.Errorf("type é obrigatório para academia escolar de nível misto")
		}

		tipoNormalizado := strings.ToLower(strings.TrimSpace(*tipoReq))
		if tipoNormalizado != "fundamental" && tipoNormalizado != "medio" {
			return "", fmt.Errorf("type deve ser 'fundamental' ou 'medio' para academias mistas")
		}
		return tipoNormalizado, nil
	default:
		return "", fmt.Errorf("nivel_escolar inválido")
	}
}

func validarAnosAcademicosMateria(tipoMateria string, anosAcademicos []string, modeloCursoMedio string) error {
	if len(anosAcademicos) == 0 {
		return fmt.Errorf("anos_academicos é obrigatório para matérias")
	}
	if tipoMateria == "medio" && apoio.ContainsString(anosAcademicos, "4_ano_medio") {
		if strings.TrimSpace(modeloCursoMedio) != cadastros.ModeloCursoMedioTecnico || len(anosAcademicos) != 1 {
			return fmt.Errorf("4_ano_medio só é permitido para a matéria de PAP de curso médio técnico")
		}
	}
	return nil
}

func validarPendenciaNivelConclusao(tipoMateria string, nivel *string, anosAcademicos []string, periodosCurso []string) error {
	if nivel == nil {
		return nil
	}
	valor := strings.TrimSpace(*nivel)
	if valor == "" {
		return fmt.Errorf("pendencia_nivel_conclusao não pode ser vazio")
	}
	if tipoMateria == "fundamental" || tipoMateria == "medio" {
		return fmt.Errorf("pendencia_nivel_conclusao só está disponível para matérias do tipo 'superior'")
	}
	if tipoMateria == "superior" {
		if !strings.HasSuffix(valor, "_semestre") {
			return fmt.Errorf("pendencia_nivel_conclusao deve ser um semestre válido, como '1_semestre'")
		}
		if len(periodosCurso) > 0 && !apoio.ContainsString(periodosCurso, valor) {
			return fmt.Errorf("pendencia_nivel_conclusao deve existir nos períodos do curso")
		}
		return nil
	}
	return fmt.Errorf("type inválido para pendencia_nivel_conclusao")
}

// ============================================================================
// DELETE /academia/materias/:id
// ============================================================================

// DeletarMateria faz a exclusão lógica da matéria (status "deletado").
// A matéria deve estar inativa antes de ser deletada.
// Notas e faltas associadas são preservadas; a auditoria (MateriaDeletada)
// fica em auditoria_crud, gravada na mesma transação.
func DeletarMateria(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)

	materiaID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("ID de materia invalido"))
		return
	}

	materiasProj := apoio.GetMateriasProjection(c)
	materiaDTO, err := materiasProj.GetByID(materiaID)
	if err != nil || materiaDTO == nil {
		utils.RespondWithNotFoundError(c, "materia")
		return
	}

	academiaProj := apoio.GetAcademiaProjection(c)
	academiaDTO, _ := academiaProj.GetByID(userID)
	if academiaDTO == nil || academiaDTO.CodigoAcademia != materiaDTO.CodigoAcademia {
		utils.RespondWithForbiddenError(c, "Materia nao pertence a esta academia")
		return
	}

	audit := db.AuditContext{
		UserID:   userID.String(),
		UserType: "academia",
		IP:       c.ClientIP(),
	}
	materia, err := cadastros.NovoMateriasRepo(apoio.GetDbClient(c)).Atualizar(
		c.Request.Context(), audit, materiaID, "MateriaDeletada", "",
		func(m *cadastros.Materia) error { return m.Deletar() })
	if err != nil {
		apoio.RespondErroCadastro(c, err, "materia")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "materia deletada com sucesso",
		"nome":    materia.Nome,
	})
}

// ============================================================================
// GET /academia/materias/:id
// ============================================================================

// GetMateria retorna uma matéria pelo ID.
// Protegido por RequireAcademia — academia só pode ver suas próprias matérias.
func GetMateria(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)

	materiaID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("ID de matéria inválido"))
		return
	}

	materiasProj := apoio.GetMateriasProjection(c)
	materiaDTO, err := materiasProj.GetByID(materiaID)
	if err != nil || materiaDTO == nil {
		utils.RespondWithNotFoundError(c, "matéria")
		return
	}

	academiaProj := apoio.GetAcademiaProjection(c)
	if userType == "academia" {
		academiaDTO, _ := academiaProj.GetByID(userID)
		if academiaDTO == nil || academiaDTO.CodigoAcademia != materiaDTO.CodigoAcademia {
			utils.RespondWithForbiddenError(c, "matéria não pertence a esta academia")
			return
		}
	}

	c.JSON(http.StatusOK, materiaDTO)
}
