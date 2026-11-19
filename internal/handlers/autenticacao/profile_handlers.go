package autenticacao

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/handlers/apoio"
	"spuri/internal/middleware"
	"spuri/internal/utils"
)

// AlterarSenha permite que o usuário autenticado altere sua própria senha.
func AlterarSenha(c *gin.Context) {
	userID, _ := c.Get("user_id")
	userType, _ := c.Get("user_type")

	var req struct {
		SenhaAtual string `json:"senha_atual" binding:"required"`
		NovaSenha  string `json:"nova_senha"  binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("senha_atual e nova_senha são obrigatórios"))
		return
	}
	if err := utils.ValidateSenha(req.NovaSenha); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	// SEC-04: a troca da senha temporária tem de produzir uma senha NOVA.
	if req.NovaSenha == req.SenhaAtual {
		utils.RespondWithValidationError(c, fmt.Errorf("a nova senha deve ser diferente da senha atual"))
		return
	}

	// Tarefa 127 (SEC-04/SEC-10): se a conta ainda tem a senha TEMPORÁRIA, a troca
	// encerra todas as sessões emitidas antes (quem entrou primeiro com a senha
	// temporária perde o acesso) e o titular recebe um token novo na resposta.
	motivo := aggregates.MotivoSenhaAlteracaoUsuario
	if uid, ok := userID.(uuid.UUID); ok {
		if tipo, ok := userType.(string); ok && contaComSenhaTemporaria(apoio.GetDbClient(c), tipo, uid) {
			motivo = aggregates.MotivoSenhaTrocaTemporaria
		}
	}

	// ── Admin ──────────────────────────────────────────────────────────────
	if userType == "admin" {
		uid := userID.(uuid.UUID)
		adminProj := apoio.GetAdminProjection(c)
		adminDTO, err := adminProj.GetByID(uid)
		if err != nil || adminDTO == nil {
			utils.RespondWithNotFoundError(c, "administrador")
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(adminDTO.SenhaHash), []byte(req.SenhaAtual)); err != nil {
			utils.RespondWithUnauthorizedError(c)
			return
		}
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NovaSenha), bcrypt.DefaultCost)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		repository := apoio.GetRepository(c)
		adminAgg, err := repository.Load(uid, "Admin")
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		admin, ok := adminAgg.(*aggregates.Admin)
		if !ok {
			utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
			return
		}
		if err := admin.AlterarSenha(string(hashedPassword), uid, motivo); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		audit := db.AuditContext{UserID: uid.String(), UserType: "admin", IP: c.ClientIP()}
		if err := repository.SaveWithAudit(admin, audit); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		log.Printf("Senha alterada (event sourcing) para admin: %v", uid)
		responderSenhaAlterada(c, motivo, uid, "admin")
		return
	}

	// ── Academia ───────────────────────────────────────────────────────────
	if userType == "academia" {
		uid := userID.(uuid.UUID)
		academiaProj := apoio.GetAcademiaProjection(c)
		academiaDTO, err := academiaProj.GetByID(uid)
		if err != nil || academiaDTO == nil {
			utils.RespondWithNotFoundError(c, "academia")
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(academiaDTO.SenhaHash), []byte(req.SenhaAtual)); err != nil {
			utils.RespondWithUnauthorizedError(c)
			return
		}
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NovaSenha), bcrypt.DefaultCost)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		repository := apoio.GetRepository(c)
		academiaAgg, err := repository.Load(uid, "Academia")
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		academia, ok := academiaAgg.(*aggregates.Academia)
		if !ok {
			utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
			return
		}
		if err := academia.AlterarSenha(string(hashedPassword), uid, motivo); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		audit := db.AuditContext{UserID: uid.String(), UserType: "academia", IP: c.ClientIP()}
		if err := repository.SaveWithAudit(academia, audit); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		log.Printf("Senha alterada (event sourcing) para academia: %v", uid)
		responderSenhaAlterada(c, motivo, uid, "academia")
		return
	}

	// ── Estudante ──────────────────────────────────────────────────────────
	if userType == "estudante" {
		uid := userID.(uuid.UUID)
		estudanteProj := apoio.GetEstudanteProjection(c)
		estudanteAuth, err := estudanteProj.GetAuthByID(uid)
		if err != nil || estudanteAuth == nil {
			utils.RespondWithNotFoundError(c, "estudante")
			return
		}
		// SEC-04: já não se exige estado "ativo" — um estudante recém-criado
		// (pendente de documentos ou matrícula) precisa de trocar a senha
		// temporária no primeiro acesso. Contas bloqueadas nem chegam aqui: o
		// AuthMiddleware recusa a sessão antes.
		if err := bcrypt.CompareHashAndPassword([]byte(estudanteAuth.Hash), []byte(req.SenhaAtual)); err != nil {
			utils.RespondWithUnauthorizedError(c)
			return
		}
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NovaSenha), bcrypt.DefaultCost)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		repository := apoio.GetRepository(c)
		estudanteAgg, err := repository.Load(uid, "Estudante")
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		estudante, ok := estudanteAgg.(*aggregates.Estudante)
		if !ok {
			utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
			return
		}
		if err := estudante.AlterarSenhaComMotivo(string(hashedPassword), motivo); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		audit := db.AuditContext{UserID: uid.String(), UserType: "estudante", IP: c.ClientIP()}
		if err := repository.SaveWithAudit(estudante, audit); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		log.Printf("Senha alterada (event sourcing) para estudante: %v", uid)
		responderSenhaAlterada(c, motivo, uid, "estudante")
		return
	}

	utils.RespondWithValidationError(c, fmt.Errorf("tipo de usuário inválido"))
}

// ============================================================================
// GET /meu-perfil
// ============================================================================

func GetMeuPerfil(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)

	switch userType {
	case "estudante":
		getPerfilEstudante(c, userID)
	case "academia":
		getPerfilAcademia(c, userID)
	case "admin":
		getPerfilAdmin(c, userID)
	default:
		utils.RespondWithValidationError(c, fmt.Errorf("tipo de usuário inválido"))
	}
}

func getPerfilEstudante(c *gin.Context, userID interface{}) {
	id, ok := userID.(uuid.UUID)
	if !ok {
		utils.RespondWithInternalError(c, fmt.Errorf("erro ao processar ID do usuário"))
		return
	}

	estudanteProj := apoio.GetEstudanteProjection(c)
	estudante, err := estudanteProj.GetByID(id)
	if err != nil || estudante == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}

	var academiaInfo *gin.H
	if estudante.CodigoAcademia != nil {
		academiaProj := apoio.GetAcademiaProjection(c)
		academia, _ := academiaProj.GetByCodigo(*estudante.CodigoAcademia)
		if academia != nil {
			academiaInfo = &gin.H{
				"codigo": academia.CodigoAcademia,
				"nome":   academia.Nome,
				"nivel":  academia.Nivel,
				"type":   academia.Type,
			}
		}
	}

	var cursoMedioInfo *gin.H
	var cursoSuperiorInfo *gin.H
	cursosProj := apoio.GetCursosProjection(c)

	if estudante.CursoMedioID != nil {
		cursoMedioUUID, err := uuid.Parse(*estudante.CursoMedioID)
		if err == nil {
			cursoMedio, _ := cursosProj.GetByID(cursoMedioUUID)
			if cursoMedio != nil {
				cursoMedioInfo = &gin.H{
					"id":     cursoMedio.ID,
					"nome":   cursoMedio.Nome,
					"type":   cursoMedio.Type,
					"status": cursoMedio.Status,
				}
			}
		}
	}

	if estudante.CursoSuperiorID != nil {
		cursoSuperiorUUID, err := uuid.Parse(*estudante.CursoSuperiorID)
		if err == nil {
			cursoSuperior, _ := cursosProj.GetByID(cursoSuperiorUUID)
			if cursoSuperior != nil {
				cursoSuperiorInfo = &gin.H{
					"id":     cursoSuperior.ID,
					"nome":   cursoSuperior.Nome,
					"type":   cursoSuperior.Type,
					"status": cursoSuperior.Status,
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"tipo":               "estudante",
		"deve_alterar_senha": deveAlterarSenhaDe(c, estudante.ID, "estudante"),
		"estudante": gin.H{
			"id":                             estudante.ID,
			"nome":                           estudante.Nome,
			"codigo_estudante":               estudante.CodigoEstudante,
			"email":                          estudante.Email,
			"telefone":                       estudante.Telefone,
			"email_verificado":               estudante.EmailVerificado,
			"bilhete_identidade":             estudante.BilheteIdentidade,
			"bilhete_identidade_encarregado": estudante.BilheteIdentidadeResp,
			"genero":                         estudante.Genero,
			"data_nascimento":                estudante.DataNascimento.Format("2006-01-02"),
			"codigo_academia":                estudante.CodigoAcademia,
			"academia_info":                  academiaInfo,
			"status":                         estudante.Status,
			"status_escolar_fundamental":     estudante.StatusEscolarFundamental,
			"status_escolar_medio":           estudante.StatusEscolarMedio,
			"status_superior":                estudante.StatusSuperior,
			"ano_escolar_fundamental":        estudante.AnoEscolar,
			"ano_escolar_medio":              estudante.AnoEscolarMedio,
			"ano_superior":                   estudante.AnoSuperior,
			"curso_medio":                    cursoMedioInfo,
			"curso_superior":                 cursoSuperiorInfo,
		},
	})
}

func getPerfilAcademia(c *gin.Context, userID interface{}) {
	id, ok := userID.(uuid.UUID)
	if !ok {
		utils.RespondWithInternalError(c, fmt.Errorf("erro ao processar ID do usuário"))
		return
	}
	academiaProj := apoio.GetAcademiaProjection(c)
	academia, err := academiaProj.GetByID(id)
	if err != nil || academia == nil {
		utils.RespondWithNotFoundError(c, "academia")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"tipo":               "academia",
		"deve_alterar_senha": deveAlterarSenhaDe(c, id, "academia"),
		"academia": gin.H{
			"id":                  academia.ID,
			"nivel":               academia.Nivel,
			"type":                academia.Type,
			"nome":                academia.Nome,
			"codigo_academia":     academia.CodigoAcademia,
			"provincia":           academia.Provincia,
			"endereco":            academia.Endereco,
			"telefone":            academia.Telefone,
			"telefone_verificado": academia.TelefoneVerificado,
			"email":               academia.Email,
			"website":             academia.Website,
			"nivel_escolar":       academia.NivelEscolar,
			"anos_academicos":     academia.AnosAcademicos,
			"status":              academia.Status,
			"cursos":              academia.Cursos,
			"email_verificado":    academia.EmailVerificado,
			"created_at":          academia.CreatedAt,
			"total_estudantes":    academia.TotalEstudantes,
		},
	})
}

func getPerfilAdmin(c *gin.Context, userID interface{}) {
	id, ok := userID.(uuid.UUID)
	if !ok {
		utils.RespondWithInternalError(c, fmt.Errorf("erro ao processar ID do usuário"))
		return
	}
	adminProj := apoio.GetAdminProjection(c)
	admin, err := adminProj.GetByID(id)
	if err != nil || admin == nil {
		utils.RespondWithNotFoundError(c, "administrador")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"tipo":               "admin",
		"deve_alterar_senha": deveAlterarSenhaDe(c, id, "admin"),
		"admin": gin.H{
			"id":               admin.ID,
			"nome":             admin.Nome,
			"email":            admin.Email,
			"role":             admin.Role,
			"status":           admin.Status,
			"email_verificado": admin.EmailVerificado,
			"created_at":       admin.CreatedAt,
		},
	})
}

// ============================================================================
// GET /buscar-usuario (admin only)
// ============================================================================

func BuscarUsuario(c *gin.Context) {
	userType, _ := middleware.GetUserType(c)
	if userType != "admin" {
		utils.RespondWithForbiddenError(c, "acesso restrito a administradores")
		return
	}

	idStr := c.Query("id")
	if idStr == "" {
		utils.RespondWithValidationError(c, fmt.Errorf("parâmetro 'id' é obrigatório"))
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("UUID inválido"))
		return
	}

	estudanteProj := apoio.GetEstudanteProjection(c)
	if est, _ := estudanteProj.GetByID(id); est != nil {
		c.JSON(http.StatusOK, gin.H{"tipo": "estudante", "usuario": est})
		return
	}

	academiaProj := apoio.GetAcademiaProjection(c)
	if aca, _ := academiaProj.GetByID(id); aca != nil {
		c.JSON(http.StatusOK, gin.H{"tipo": "academia", "usuario": aca})
		return
	}

	adminProj := apoio.GetAdminProjection(c)
	if adm, _ := adminProj.GetByID(id); adm != nil {
		c.JSON(http.StatusOK, gin.H{"tipo": "admin", "usuario": adm})
		return
	}

	utils.RespondWithNotFoundError(c, "usuário")
}

// contaComSenhaTemporaria indica se a conta ainda tem deve_alterar_senha ligado.
func contaComSenhaTemporaria(client *db.Client, userType string, id uuid.UUID) bool {
	tabelas := map[string]string{
		"admin":     "projection_admins",
		"academia":  "projection_academias",
		"estudante": "projection_estudantes",
	}
	tabela, ok := tabelas[userType]
	if !ok || client == nil {
		return false
	}
	var deve bool
	if err := client.DB().QueryRow(
		`SELECT COALESCE(deve_alterar_senha, FALSE) FROM `+tabela+` WHERE id = $1`, id,
	).Scan(&deve); err != nil {
		return false
	}
	return deve
}

// responderSenhaAlterada devolve a resposta de sucesso da troca de senha. Na
// troca da senha temporária as sessões anteriores foram encerradas, por isso a
// resposta traz um token novo (emitido depois do evento) para o próprio titular.
func responderSenhaAlterada(c *gin.Context, motivo string, uid uuid.UUID, userType string) {
	resposta := gin.H{"message": "Senha alterada com sucesso!"}
	if motivo == aggregates.MotivoSenhaTrocaTemporaria {
		if token, err := middleware.GenerateToken(uid, userType); err == nil {
			resposta["token"] = token
		} else {
			log.Printf("[WARN] AlterarSenha: não foi possível emitir novo token: %v", err)
		}
	}
	c.JSON(http.StatusOK, resposta)
}
