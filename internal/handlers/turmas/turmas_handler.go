package turmas

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/cadastros"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/handlers/apoio"
	"spuri/internal/middleware"
	"spuri/internal/projections"
	"spuri/internal/utils"
)

// artigoAgrupamento devolve "um grupo" no 4º ano médio e "uma turma" nos demais.
func artigoAgrupamento(nivel string) string {
	if aggregates.TipoAgrupamentoDoNivel(nivel) == "grupo" {
		return "um grupo"
	}
	return "uma turma"
}

func mensagemAgrupamentoCriado(nivel string) string {
	if aggregates.TipoAgrupamentoDoNivel(nivel) == "grupo" {
		return "grupo criado com sucesso"
	}
	return "turma criada com sucesso"
}

// CriarTurma cria uma nova turma para a academia autenticada.
// Rota: POST /academia/turmas
func CriarTurma(c *gin.Context) {
	academiaID, _ := middleware.GetUserID(c)

	var req struct {
		CodigoTurma string     `json:"codigo_turma" binding:"required"`
		Nivel       string     `json:"nivel"        binding:"required"`
		CursoID     *uuid.UUID `json:"curso_id"`
		Turno       string     `json:"turno"        binding:"required"`
		// TemaTrabalho é opcional e só vale para grupos do 4º ano médio.
		TemaTrabalho *string `json:"tema_trabalho"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	academiaProj := apoio.GetAcademiaProjection(c)
	academiaDTO, err := academiaProj.GetByID(academiaID)
	if err != nil || academiaDTO == nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	codigoTurmaNormalizado, err := aggregates.NormalizarCodigoTurma(req.CodigoTurma)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	req.CodigoTurma = codigoTurmaNormalizado

	turmasProj := apoio.GetTurmasProjection(c)
	existing, _ := turmasProj.GetByCodigoTurma(req.CodigoTurma, academiaDTO.CodigoAcademia)
	if existing != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("já existe %s com este código nesta academia", artigoAgrupamento(req.Nivel)))
		return
	}

	turma, err := cadastros.NovaTurma(
		req.CodigoTurma,
		academiaDTO.CodigoAcademia,
		req.Nivel,
		req.CursoID,
		req.Turno,
		req.TemaTrabalho,
	)
	if err != nil {
		apoio.RespondErroCadastro(c, err, "turma")
		return
	}

	audit := db.AuditContext{
		UserID:   academiaID.String(),
		UserType: "academia",
		IP:       c.ClientIP(),
		Ator:     apoio.AtorDaRequisicao(c),
	}
	if err := cadastros.NovoTurmasRepo(apoio.GetDbClient(c)).Criar(c.Request.Context(), audit, turma); err != nil {
		log.Printf("❌ [CriarTurma] Erro ao salvar: %v", err)
		apoio.RespondErroCadastro(c, err, "turma")
		return
	}

	log.Printf("✅ [CriarTurma] %s criada na academia %s", req.CodigoTurma, academiaDTO.CodigoAcademia)

	c.JSON(http.StatusCreated, gin.H{
		"message":          mensagemAgrupamentoCriado(req.Nivel),
		"id":               turma.ID,
		"codigo_turma":     req.CodigoTurma,
		"tipo_agrupamento": aggregates.TipoAgrupamentoDoNivel(req.Nivel),
	})
}

// ListarTurmasAcademia lista todas as turmas da academia autenticada.
// Rota: GET /academia/turmas
func ListarTurmasAcademia(c *gin.Context) {
	academiaID, _ := middleware.GetUserID(c)
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
		academiaDTO, err := academiaProj.GetByID(academiaID)
		if err != nil || academiaDTO == nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		codigoAcademia = academiaDTO.CodigoAcademia
	}

	turmasProj := apoio.GetTurmasProjection(c)
	turmas, err := turmasProj.ListByAcademia(codigoAcademia)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"turmas": turmas})
}

// GetTurmasEstudante retorna as turmas de um estudante com autorização por papel.
// Rota única: GET /turmas-estudante/:codigo
//   - estudante: apenas o próprio código
//   - academia: apenas estudantes da sua academia
//   - admin: qualquer estudante
func GetTurmasEstudante(c *gin.Context) {
	codigoEstudante := c.Param("codigo")

	estudanteProj := apoio.GetEstudanteProjection(c)
	estudante, err := estudanteProj.GetByCodigo(codigoEstudante)
	if err != nil || estudante == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}

	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)
	turmasProj := apoio.GetTurmasProjection(c)

	var turmas []*projections.TurmaDTO
	switch userType {
	case "estudante":
		if userID != estudante.ID {
			utils.RespondWithForbiddenError(c, "você só pode visualizar suas próprias turmas")
			return
		}
		turmas, err = turmasProj.ListByEstudante(codigoEstudante, estudante.CodigoAcademia)
	case "academia":
		academiaProj := apoio.GetAcademiaProjection(c)
		academiaDTO, errAcademia := academiaProj.GetByID(userID)
		if errAcademia != nil || academiaDTO == nil {
			utils.RespondWithNotFoundError(c, "academia")
			return
		}
		if estudante.CodigoAcademia == nil || *estudante.CodigoAcademia != academiaDTO.CodigoAcademia {
			utils.RespondWithForbiddenError(c, "estudante não pertence a esta academia")
			return
		}
		turmas, err = turmasProj.ListByEstudante(codigoEstudante, &academiaDTO.CodigoAcademia)
	case "admin":
		turmas, err = turmasProj.ListByEstudante(codigoEstudante, nil)
	default:
		utils.RespondWithForbiddenError(c, "tipo de usuário não autorizado")
		return
	}

	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"codigo_estudante": codigoEstudante,
		"nome":             estudante.Nome,
		"turmas":           turmas,
		"total":            len(turmas),
	})
}

// GetTurma retorna uma turma pelo código.
// Rota: GET /academia/turmas/:codigo
func GetTurma(c *gin.Context) {
	academiaID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)
	codigoTurma := c.Param("codigo")

	turmasProj := apoio.GetTurmasProjection(c)
	var (
		turma *projections.TurmaDTO
		err   error
	)
	if userType == "admin" {
		codigoAcademia := c.Query("codigo_academia")
		if codigoAcademia == "" {
			utils.RespondWithValidationError(c, fmt.Errorf("codigo_academia é obrigatório para admin"))
			return
		}
		turma, err = turmasProj.GetByCodigoTurma(codigoTurma, codigoAcademia)
	} else {
		academiaProj := apoio.GetAcademiaProjection(c)
		academiaDTO, errAcademia := academiaProj.GetByID(academiaID)
		if errAcademia != nil || academiaDTO == nil {
			utils.RespondWithInternalError(c, errAcademia)
			return
		}
		turma, err = turmasProj.GetByCodigoTurma(codigoTurma, academiaDTO.CodigoAcademia)
	}

	if err != nil || turma == nil {
		utils.RespondWithNotFoundError(c, "turma")
		return
	}

	c.JSON(http.StatusOK, turma)
}

// AtivarTurma ativa uma turma inativa da academia.
// Rota: PUT /academia/turmas/:codigo/ativar
func AtivarTurma(c *gin.Context) {
	academiaID, _ := middleware.GetUserID(c)
	codigoTurma := c.Param("codigo")

	academiaProj := apoio.GetAcademiaProjection(c)
	academiaDTO, err := academiaProj.GetByID(academiaID)
	if err != nil || academiaDTO == nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	turmasProj := apoio.GetTurmasProjection(c)
	turmaDTO, err := turmasProj.GetByCodigoTurma(codigoTurma, academiaDTO.CodigoAcademia)
	if err != nil || turmaDTO == nil {
		utils.RespondWithNotFoundError(c, "turma")
		return
	}

	audit := db.AuditContext{
		UserID:   academiaID.String(),
		UserType: "academia",
		IP:       c.ClientIP(),
		Ator:     apoio.AtorDaRequisicao(c),
	}
	if _, err := cadastros.NovoTurmasRepo(apoio.GetDbClient(c)).Atualizar(c.Request.Context(), audit, turmaDTO.ID, "TurmaAtivada", "",
		func(t *cadastros.Turma) error { return t.Ativar(academiaID) }); err != nil {
		log.Printf("❌ [AtivarTurma] Erro ao salvar: %v", err)
		apoio.RespondErroCadastro(c, err, "turma")
		return
	}

	log.Printf("✅ [AtivarTurma] %s ativada pela academia %s", codigoTurma, academiaDTO.CodigoAcademia)

	c.JSON(http.StatusOK, gin.H{
		"message":      "turma ativada com sucesso",
		"codigo_turma": codigoTurma,
	})
}

// DesativarTurma desativa uma turma ativa da academia.
// Rota: PUT /academia/turmas/:codigo/desativar
func DesativarTurma(c *gin.Context) {
	academiaID, _ := middleware.GetUserID(c)
	codigoTurma := c.Param("codigo")

	academiaProj := apoio.GetAcademiaProjection(c)
	academiaDTO, err := academiaProj.GetByID(academiaID)
	if err != nil || academiaDTO == nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	turmasProj := apoio.GetTurmasProjection(c)
	turmaDTO, err := turmasProj.GetByCodigoTurma(codigoTurma, academiaDTO.CodigoAcademia)
	if err != nil || turmaDTO == nil {
		utils.RespondWithNotFoundError(c, "turma")
		return
	}

	audit := db.AuditContext{
		UserID:   academiaID.String(),
		UserType: "academia",
		IP:       c.ClientIP(),
		Ator:     apoio.AtorDaRequisicao(c),
	}
	if _, err := cadastros.NovoTurmasRepo(apoio.GetDbClient(c)).Atualizar(c.Request.Context(), audit, turmaDTO.ID, "TurmaDesativada", "",
		func(t *cadastros.Turma) error { return t.Desativar(academiaID) }); err != nil {
		log.Printf("❌ [DesativarTurma] Erro ao salvar: %v", err)
		apoio.RespondErroCadastro(c, err, "turma")
		return
	}

	log.Printf("✅ [DesativarTurma] %s desativada pela academia %s", codigoTurma, academiaDTO.CodigoAcademia)

	c.JSON(http.StatusOK, gin.H{
		"message":      "turma desativada com sucesso",
		"codigo_turma": codigoTurma,
	})
}

type estudanteCompatibilidadeDTO struct {
	Codigo          string
	AnoEscolar      *string
	AnoEscolarMedio *string
	AnoSuperior     *string
	CursoMedioID    *string
	CursoSuperiorID *string
}

func validarCompatibilidadeTurmaComEstudantes(
	academiaAnosAcademicos []string,
	nivel string,
	cursoID *uuid.UUID,
	estudantes []estudanteCompatibilidadeDTO,
) error {
	for _, est := range estudantes {
		if err := apoio.ValidarCompatibilidadeEstudanteTurma(
			nil, nil,
			academiaAnosAcademicos,
			nivel,
			cursoID,
			est.AnoEscolar,
			est.AnoEscolarMedio,
			est.AnoSuperior,
			est.CursoMedioID,
			est.CursoSuperiorID,
		); err != nil {
			return fmt.Errorf(
				"estudante '%s' ficaria incompatível com os novos dados (%w)",
				est.Codigo,
				err,
			)
		}
	}
	return nil
}

// AdicionarEstudanteATurma adiciona um estudante à turma.
// Rota: POST /academia/turmas/:codigo/estudantes
//
// Validações de compatibilidade adicionadas:
//   - Fundamental: o ano_escolar_fundamental do estudante deve corresponder ao nivel da turma
//   - Médio: o ano_escolar_medio e o curso_medio_id do estudante devem
//     corresponder ao nivel e curso_id da turma
//   - Superior: o ano_superior e o curso_superior_id do estudante devem
//     corresponder ao nivel e curso_id da turma
func AdicionarEstudanteATurma(c *gin.Context) {
	academiaID, _ := middleware.GetUserID(c)
	codigoTurma := c.Param("codigo")

	var req struct {
		CodigoEstudante string `json:"codigo_estudante" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	academiaDTO, err := apoio.GetAcademiaProjection(c).GetByID(academiaID)
	if err != nil || academiaDTO == nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	estudanteDTO, err := apoio.GetEstudanteProjection(c).GetByCodigo(req.CodigoEstudante)
	if err != nil || estudanteDTO == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}
	if estudanteDTO.CodigoAcademia == nil || *estudanteDTO.CodigoAcademia != academiaDTO.CodigoAcademia {
		utils.RespondWithForbiddenError(c, "estudante não pertence a esta academia")
		return
	}

	if err := apoio.VincularEstudanteATurma(c, academiaDTO, req.CodigoEstudante, derefString(estudanteDTO.AnoEscolar), derefString(estudanteDTO.AnoEscolarMedio), derefString(estudanteDTO.AnoSuperior), derefString(estudanteDTO.CursoMedioID), derefString(estudanteDTO.CursoSuperiorID), codigoTurma, false, academiaID); err != nil {
		if errors.Is(err, apoio.ErrTurmaNaoEncontradaParaVinculo) {
			utils.RespondWithNotFoundError(c, "turma")
		} else {
			utils.RespondWithValidationError(c, err)
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":          "estudante adicionado à turma com sucesso",
		"codigo_turma":     codigoTurma,
		"codigo_estudante": req.CodigoEstudante,
	})
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// RemoverEstudanteDaTurma remove um estudante da turma.
// Rota: DELETE /academia/turmas/:codigo/estudantes/:codigoEstudante
func RemoverEstudanteDaTurma(c *gin.Context) {
	academiaID, _ := middleware.GetUserID(c)
	codigoTurma := c.Param("codigo")
	codigoEstudante := c.Param("codigoEstudante")

	academiaProj := apoio.GetAcademiaProjection(c)
	academiaDTO, err := academiaProj.GetByID(academiaID)
	if err != nil || academiaDTO == nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	turmasProj := apoio.GetTurmasProjection(c)
	turmaDTO, err := turmasProj.GetByCodigoTurma(codigoTurma, academiaDTO.CodigoAcademia)
	if err != nil || turmaDTO == nil {
		utils.RespondWithNotFoundError(c, "turma")
		return
	}

	anoLectivo, err := apoio.ResolverAnoLetivoAcademia(academiaDTO.AnoLetivo, academiaDTO.CodigoAcademia)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	_ = anoLectivo // o histórico por ano letivo não muda ao remover (ver Turma.RemoverEstudante)

	audit := db.AuditContext{
		UserID:   academiaID.String(),
		UserType: "academia",
		IP:       c.ClientIP(),
		Ator:     apoio.AtorDaRequisicao(c),
	}
	if _, err := cadastros.NovoTurmasRepo(apoio.GetDbClient(c)).Atualizar(c.Request.Context(), audit, turmaDTO.ID, "EstudanteRemovidoDaTurma", "",
		func(t *cadastros.Turma) error { return t.RemoverEstudante(codigoEstudante) }); err != nil {
		apoio.RespondErroCadastro(c, err, "turma")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":          "estudante removido da turma com sucesso",
		"codigo_turma":     codigoTurma,
		"codigo_estudante": codigoEstudante,
	})
}

// AtualizarTurma atualiza dados da turma.
// Rota: PUT /academia/turmas/:codigo
func AtualizarTurma(c *gin.Context) {
	academiaID, _ := middleware.GetUserID(c)
	codigoTurma := c.Param("codigo")

	var req struct {
		Nivel   *string    `json:"nivel"`
		CursoID *uuid.UUID `json:"curso_id"`
		Turno   *string    `json:"turno"`
		// TemaTrabalho: omitido = não altera; "" = remove o tema.
		TemaTrabalho *string `json:"tema_trabalho"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	academiaProj := apoio.GetAcademiaProjection(c)
	academiaDTO, err := academiaProj.GetByID(academiaID)
	if err != nil || academiaDTO == nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	turmasProj := apoio.GetTurmasProjection(c)
	turmaDTO, err := turmasProj.GetByCodigoTurma(codigoTurma, academiaDTO.CodigoAcademia)
	if err != nil || turmaDTO == nil {
		utils.RespondWithNotFoundError(c, "turma")
		return
	}

	// Se houver alteração de nível e/ou curso, valida todos os estudantes já
	// vinculados antes de persistir a mudança.
	nivelEfetivo := turmaDTO.Nivel
	if req.Nivel != nil {
		nivelEfetivo = *req.Nivel
	}
	cursoIDEfetivo := turmaDTO.CursoID
	if req.CursoID != nil {
		cursoIDEfetivo = req.CursoID
	}

	if (req.Nivel != nil || req.CursoID != nil) && len(turmaDTO.Estudantes) > 0 {
		estudanteProj := apoio.GetEstudanteProjection(c)
		estudantesParaValidar := make([]estudanteCompatibilidadeDTO, 0, len(turmaDTO.Estudantes))

		for _, codigoEstudante := range turmaDTO.Estudantes {
			estudanteDTO, err := estudanteProj.GetByCodigo(codigoEstudante)
			if err != nil {
				utils.RespondWithInternalError(c, err)
				return
			}
			if estudanteDTO == nil {
				utils.RespondWithValidationError(c, fmt.Errorf(
					"não é possível atualizar a turma: estudante '%s' não foi encontrado para validação de compatibilidade",
					codigoEstudante,
				))
				return
			}

			estudantesParaValidar = append(estudantesParaValidar, estudanteCompatibilidadeDTO{
				Codigo:          codigoEstudante,
				AnoEscolar:      estudanteDTO.AnoEscolar,
				AnoEscolarMedio: estudanteDTO.AnoEscolarMedio,
				AnoSuperior:     estudanteDTO.AnoSuperior,
				CursoMedioID:    estudanteDTO.CursoMedioID,
				CursoSuperiorID: estudanteDTO.CursoSuperiorID,
			})
		}

		if err := validarCompatibilidadeTurmaComEstudantes(
			academiaDTO.AnosAcademicos,
			nivelEfetivo,
			cursoIDEfetivo,
			estudantesParaValidar,
		); err != nil {
			utils.RespondWithValidationError(c, fmt.Errorf("não é possível atualizar turma: %w", err))
			return
		}
	}

	audit := db.AuditContext{
		UserID:   academiaID.String(),
		UserType: "academia",
		IP:       c.ClientIP(),
		Ator:     apoio.AtorDaRequisicao(c),
	}
	atualizada, err := cadastros.NovoTurmasRepo(apoio.GetDbClient(c)).Atualizar(c.Request.Context(), audit, turmaDTO.ID, "TurmaDadosAtualizados", "",
		func(t *cadastros.Turma) error {
			return t.AtualizarDados(req.Nivel, req.CursoID, req.Turno, req.TemaTrabalho)
		})
	if err != nil {
		apoio.RespondErroCadastro(c, err, "turma")
		return
	}

	mensagem := "turma atualizada com sucesso"
	if aggregates.TipoAgrupamentoDoNivel(atualizada.Nivel) == "grupo" {
		mensagem = "grupo atualizado com sucesso"
	}
	c.JSON(http.StatusOK, gin.H{"message": mensagem})
}

// DeletarTurma remove logicamente uma turma da academia.
// Rota: DELETE /academia/turmas/:codigo
func DeletarTurma(c *gin.Context) {
	academiaID, _ := middleware.GetUserID(c)
	codigoTurma := c.Param("codigo")

	var req struct {
		Motivo string `json:"motivo"`
	}
	_ = c.ShouldBindJSON(&req)

	academiaProj := apoio.GetAcademiaProjection(c)
	academiaDTO, err := academiaProj.GetByID(academiaID)
	if err != nil || academiaDTO == nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	turmasProj := apoio.GetTurmasProjection(c)
	turmaDTO, err := turmasProj.GetByCodigoTurma(codigoTurma, academiaDTO.CodigoAcademia)
	if err != nil || turmaDTO == nil {
		utils.RespondWithNotFoundError(c, "turma")
		return
	}

	audit := db.AuditContext{
		UserID:   academiaID.String(),
		UserType: "academia",
		IP:       c.ClientIP(),
		Ator:     apoio.AtorDaRequisicao(c),
	}
	if _, err := cadastros.NovoTurmasRepo(apoio.GetDbClient(c)).Atualizar(c.Request.Context(), audit, turmaDTO.ID, "TurmaDeletada", req.Motivo,
		func(t *cadastros.Turma) error { return t.Deletar() }); err != nil {
		log.Printf("❌ [DeletarTurma] Erro ao salvar: %v", err)
		apoio.RespondErroCadastro(c, err, "turma")
		return
	}

	log.Printf("✅ [DeletarTurma] %s deletada pela academia %s", codigoTurma, academiaDTO.CodigoAcademia)

	c.JSON(http.StatusOK, gin.H{
		"message":      "turma deletada com sucesso",
		"codigo_turma": codigoTurma,
		"auditavel":    true,
	})
}

// AtualizarDadosTurma é alias de AtualizarTurma — mantém compatibilidade com main.go.
func AtualizarDadosTurma(c *gin.Context) {
	AtualizarTurma(c)
}
