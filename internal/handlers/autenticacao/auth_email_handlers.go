package autenticacao

import (
	"errors"
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
	"spuri/internal/services"
	"spuri/internal/utils"
)

// ============================================================================
// Verificação de email
// ============================================================================

// VerificarEmail verifica email usando token.
//
// FIX-C3: estudante usa event sourcing via aggregate.VerificarEmail(),
// idêntico ao fluxo do Admin e Academia.
// FIX H4-RST-04: estrutura refatorada de if/if/if para switch — mais segura
// ao adicionar novos tipos de usuário (qualquer tipo não coberto cai no default).
func VerificarEmail(c *gin.Context) {
	token := c.Param("token")

	emailSvc := apoio.GetEmailService(c)
	tokenInfo, err := emailSvc.VerifyToken(token, "verificacao_email")
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	switch tokenInfo.UserType {

	// ── Admin: event sourcing ──────────────────────────────────────────────
	case "admin":
		repository := apoio.GetRepository(c)
		adminAgg, err := repository.Load(tokenInfo.UserID, "Admin")
		if err != nil {
			utils.RespondWithNotFoundError(c, "administrador")
			return
		}

		admin, ok := adminAgg.(*aggregates.Admin)
		if !ok {
			utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
			return
		}

		alreadyVerified := false
		if err := admin.VerificarEmail(); err != nil {
			if err.Error() == "email já verificado" {
				alreadyVerified = true
				log.Printf("[INFO] Email já estava verificado para admin: %s", tokenInfo.Email)
			} else {
				utils.RespondWithInternalError(c, err)
				return
			}
		}

		if !alreadyVerified {
			audit := db.AuditContext{
				UserID:   tokenInfo.UserID.String(),
				UserType: "admin",
				IP:       c.ClientIP(),
			}
			if err := repository.SaveWithAudit(admin, audit); err != nil {
				utils.RespondWithInternalError(c, err)
				return
			}
			log.Printf("Email verificado (event sourcing) para admin: %s", tokenInfo.Email)
		}

		msg := "Email verificado com sucesso!"
		if alreadyVerified {
			msg = "Email já estava verificado."
		}
		c.JSON(http.StatusOK, gin.H{"message": msg, "email": tokenInfo.Email})

	// ── Academia: event sourcing ───────────────────────────────────────────
	case "academia":
		repository := apoio.GetRepository(c)
		academiaAgg, err := repository.Load(tokenInfo.UserID, "Academia")
		if err != nil {
			utils.RespondWithNotFoundError(c, "academia")
			return
		}

		academia, ok := academiaAgg.(*aggregates.Academia)
		if !ok {
			utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
			return
		}

		alreadyVerified := false
		if err := academia.VerificarEmail(); err != nil {
			if err.Error() == "email já verificado" {
				alreadyVerified = true
				log.Printf("[INFO] Email já estava verificado para academia: %s", tokenInfo.Email)
			} else {
				utils.RespondWithInternalError(c, err)
				return
			}
		}

		if !alreadyVerified {
			audit := db.AuditContext{
				UserID:   tokenInfo.UserID.String(),
				UserType: "academia",
				IP:       c.ClientIP(),
			}
			if err := repository.SaveWithAudit(academia, audit); err != nil {
				utils.RespondWithInternalError(c, err)
				return
			}
			log.Printf("Email verificado (event sourcing) para academia: %s", tokenInfo.Email)
		}

		msg := "Email verificado com sucesso!"
		if alreadyVerified {
			msg = "Email já estava verificado."
		}
		c.JSON(http.StatusOK, gin.H{"message": msg, "email": tokenInfo.Email})

	// ── Estudante: event sourcing ──────────────────────────────────────────
	case "estudante":
		repository := apoio.GetRepository(c)
		estudanteAgg, err := repository.Load(tokenInfo.UserID, "Estudante")
		if err != nil {
			utils.RespondWithNotFoundError(c, "estudante")
			return
		}

		estudante, ok := estudanteAgg.(*aggregates.Estudante)
		if !ok {
			utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
			return
		}

		alreadyVerified := false
		if err := estudante.VerificarEmail(); err != nil {
			if err.Error() == "email já verificado" {
				alreadyVerified = true
				log.Printf("[INFO] Email já estava verificado para estudante: %s", tokenInfo.Email)
			} else {
				utils.RespondWithInternalError(c, err)
				return
			}
		}

		if !alreadyVerified {
			audit := db.AuditContext{
				UserID:   tokenInfo.UserID.String(),
				UserType: "estudante",
				IP:       c.ClientIP(),
			}
			if err := repository.SaveWithAudit(estudante, audit); err != nil {
				utils.RespondWithInternalError(c, err)
				return
			}
			log.Printf("Email verificado (event sourcing) para estudante: %s", tokenInfo.Email)
		}

		msg := "Email verificado com sucesso!"
		if alreadyVerified {
			msg = "Email já estava verificado."
		}
		c.JSON(http.StatusOK, gin.H{"message": msg, "email": tokenInfo.Email})

	case "secretario", "professor", "encarregado":
		verificarEmailContaPessoa(c, tokenInfo)

	default:
		utils.RespondWithValidationError(c, fmt.Errorf("tipo de usuário inválido no token: %s", tokenInfo.UserType))
	}
}

// ============================================================================
// Solicitar (backend gera token E envia email — sem passar token ao frontend)
// ============================================================================

// SolicitarVerificacaoEmail gera o token e envia o email de verificação diretamente.
// Usado por fluxos onde o backend controla o envio completo.
// Rota: POST /email/verificar-email/solicitar  (requer AuthMiddleware)
func SolicitarVerificacaoEmail(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)

	var email, nome string
	client := apoio.GetDbClient(c)

	switch userType {
	case "estudante":
		if err := client.DB().QueryRow(
			`SELECT COALESCE(email,''), nome FROM projection_estudantes WHERE id = $1`,
			userID,
		).Scan(&email, &nome); err != nil || email == "" {
			utils.RespondWithValidationError(c, fmt.Errorf("estudante não possui email cadastrado"))
			return
		}
	case "academia":
		if err := client.DB().QueryRow(
			`SELECT COALESCE(email,''), nome FROM projection_academias WHERE id = $1`,
			userID,
		).Scan(&email, &nome); err != nil || email == "" {
			utils.RespondWithValidationError(c, fmt.Errorf("academia não possui email cadastrado"))
			return
		}
	case "admin":
		if err := client.DB().QueryRow(
			`SELECT email, nome FROM projection_admins WHERE id = $1`,
			userID,
		).Scan(&email, &nome); err != nil {
			utils.RespondWithNotFoundError(c, "administrador")
			return
		}
	case "secretario", "professor", "encarregado":
		var errConta error
		email, nome, errConta = emailEnomeDaConta(c, userType, userID)
		if errConta != nil || email == "" {
			utils.RespondWithValidationError(c, fmt.Errorf("a conta não possui email cadastrado"))
			return
		}
	default:
		utils.RespondWithValidationError(c, fmt.Errorf("tipo de usuário inválido"))
		return
	}

	emailSvc := apoio.GetEmailService(c)

	// Gera token e envia o email diretamente — token NÃO retornado ao frontend
	if err := emailSvc.SendVerificationEmail(userID, userType, email, nome); err != nil {
		if errors.Is(err, services.ErrEmailNaoConfigurado) {
			// Canal de e-mail indisponível (Brevo não configurado): 503, sem
			// afirmar que algo foi enviado (SEC-07 — falha fechada).
			utils.RespondWithServiceUnavailable(c, fmt.Errorf("o envio de e-mail está temporariamente indisponível"))
			return
		}
		log.Printf("Erro ao enviar email de verificação: %v", err)
		utils.RespondWithInternalError(c, err)
		return
	}

	log.Printf("Email de verificação enviado (tipo %s)", userType)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Email de verificação enviado com sucesso!",
		"email":   email,
	})
}

// mensagemRecuperacaoSenha é a ÚNICA resposta pública de /recuperar-senha/solicitar
// (SEC-06): a mesma exista ou não a conta, tenha ou não o e-mail verificado.
const mensagemRecuperacaoSenha = "Se existir uma conta elegível, enviámos as instruções para o e-mail cadastrado."

// SolicitarRecuperacaoSenha (SEC-03 / SEC-06) envia por e-mail um LINK de uso
// único (1 hora) para o titular escolher a nova senha. NÃO altera a senha, não
// gera senha e não confirma se a conta existe.
// Rota: POST /email/recuperar-senha/solicitar  (pública — usa identificador no body)
//
// 'tipo' é OPCIONAL: quando omitido, tenta identificar o usuário como
// estudante, depois academia, depois admin, nessa ordem, parando no primeiro em
// que o identificador exista (mesma ordem usada pelas páginas públicas
// "/esqueci-senha", que nunca pedem o tipo de conta).
//
// Respostas:
//   - 200 sempre, com a mesma mensagem, exista a conta ou não, tenha ou não
//     e-mail verificado (não permite enumerar contas);
//   - 503 quando o canal de e-mail (Brevo) não está configurado — independe da
//     conta, logo não revela nada;
//   - 422 apenas para corpo inválido (identificador ausente / tipo inválido).
func SolicitarRecuperacaoSenha(c *gin.Context) {
	var req struct {
		Identificador string `json:"identificador" binding:"required"`
		Tipo          string `json:"tipo"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("identificador é obrigatório"))
		return
	}

	tiposParaTentar := []string{"estudante", "academia", "admin", "secretario", "professor", "encarregado"}
	if req.Tipo != "" {
		if req.Tipo != "estudante" && req.Tipo != "academia" && req.Tipo != "admin" && !ehContaPessoa(req.Tipo) {
			utils.RespondWithValidationError(c, fmt.Errorf("tipo deve ser 'estudante', 'academia', 'admin', 'secretario', 'professor' ou 'encarregado'"))
			return
		}
		tiposParaTentar = []string{req.Tipo}
	}

	if !services.BrevoConfigurado() {
		utils.RespondWithServiceUnavailable(c, fmt.Errorf("o envio de e-mail está temporariamente indisponível"))
		return
	}

	client := apoio.GetDbClient(c)

	var userID uuid.UUID
	var email, nome, tipoEncontrado string
	var emailVerificado, encontrado bool

	for _, tipo := range tiposParaTentar {
		var idStr, e, n string
		var verificado bool
		var errBusca error
		switch tipo {
		case "estudante":
			errBusca = client.DB().QueryRow(
				`SELECT id, COALESCE(email,''), nome, COALESCE(email_verificado, FALSE)
				 FROM projection_estudantes
				 WHERE codigo_estudante = $1 OR email = $1`,
				req.Identificador,
			).Scan(&idStr, &e, &n, &verificado)
		case "academia":
			errBusca = client.DB().QueryRow(
				`SELECT id, COALESCE(email,''), nome, COALESCE(email_verificado, FALSE)
				 FROM projection_academias
				 WHERE codigo_academia = $1 OR email = $1`,
				req.Identificador,
			).Scan(&idStr, &e, &n, &verificado)
		case "admin":
			errBusca = client.DB().QueryRow(
				`SELECT id, email, nome, COALESCE(email_verificado, FALSE)
				 FROM projection_admins WHERE email = $1`,
				req.Identificador,
			).Scan(&idStr, &e, &n, &verificado)
		case "secretario", "professor", "encarregado":
			if id, em, nm, ver, ok := buscarContaParaRecuperacao(c, tipo, req.Identificador); ok {
				idStr, e, n, verificado = id.String(), em, nm, ver
			} else {
				errBusca = errContaPessoaNaoEncontrada
			}
		}
		if errBusca != nil {
			continue // não encontrado neste tipo — tenta o próximo
		}
		userID, _ = uuid.Parse(idStr)
		email, nome, emailVerificado, tipoEncontrado = e, n, verificado, tipo
		encontrado = true
		break
	}

	// Conta elegível = existe, tem e-mail cadastrado e esse e-mail foi verificado.
	// Qualquer outro caso responde exatamente igual, sem enviar nada.
	if encontrado && email != "" && emailVerificado {
		emailSvc := apoio.GetEmailService(c)
		apoio.ExecutarEnvioRecuperacao(func() {
			if err := emailSvc.SendPasswordResetLinkEmail(userID, tipoEncontrado, email, nome); err != nil {
				// Sem e-mail nem identificador no log: só a causa técnica.
				log.Printf("[recuperacao-senha] falha ao enviar o link de recuperação (tipo %s): %v", tipoEncontrado, err)
			}
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": mensagemRecuperacaoSenha,
	})
}

// ============================================================================
// Reset de senha via token
// ============================================================================

// ResetarSenha redefine a senha usando token de recuperação.
func ResetarSenha(c *gin.Context) {
	token := c.Param("token")

	var req struct {
		NovaSenha string `json:"nova_senha" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("nova_senha é obrigatória"))
		return
	}

	// Valida ANTES de consumir o token (VerifyToken é de uso único): uma
	// senha muito curta não deve queimar o link de recuperação, senão o
	// usuário precisaria solicitar um novo email só por causa disso.
	// Gap encontrado nesta tarefa: este era o único fluxo de definição de
	// senha (troca própria, cadastro de academia, recuperação) que não
	// passava por ValidateSenha — a regra de tamanho mínimo não era
	// aplicada aqui antes desta correção.
	if err := utils.ValidateSenha(req.NovaSenha); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	// VerifyToken valida e consome o token — prevenindo replay attacks.
	emailSvc := apoio.GetEmailService(c)
	tokenInfo, err := emailSvc.VerifyToken(token, "recuperacao_senha")
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NovaSenha), bcrypt.DefaultCost)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	client := apoio.GetDbClient(c)

	// Depois de um reset concluído nenhum outro link de recuperação pendente
	// do mesmo utilizador pode continuar válido (SEC-03/SEC-09).
	invalidarPendentes := func() {
		if err := emailSvc.InvalidarTokensPendentes(tokenInfo.UserID, tokenInfo.UserType, "recuperacao_senha"); err != nil {
			log.Printf("[WARN] ResetarSenha: %v", err)
		}
	}

	switch tokenInfo.UserType {

	// ── Admin ──────────────────────────────────────────────────────────────
	case "admin":
		var emailVerificado bool
		if err = client.DB().QueryRow(
			`SELECT COALESCE(email_verificado, FALSE) FROM projection_admins WHERE id = $1`,
			tokenInfo.UserID,
		).Scan(&emailVerificado); err != nil {
			utils.RespondWithNotFoundError(c, "administrador")
			return
		}
		if !emailVerificado {
			utils.RespondWithForbiddenError(c, "Por favor, verifique seu email antes de resetar a senha")
			return
		}

		repository := apoio.GetRepository(c)
		adminAgg, err := repository.Load(tokenInfo.UserID, "Admin")
		if err != nil {
			utils.RespondWithNotFoundError(c, "administrador")
			return
		}
		admin, ok := adminAgg.(*aggregates.Admin)
		if !ok {
			utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
			return
		}
		if err := admin.AlterarSenha(string(hashedPassword), uuid.Nil, "reset_senha"); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		audit := db.AuditContext{UserID: "sistema", UserType: "sistema", IP: c.ClientIP()}
		if err := repository.SaveWithAudit(admin, audit); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		invalidarPendentes()
		log.Printf("Senha resetada (event sourcing) para admin: %s", tokenInfo.Email)
		c.JSON(http.StatusOK, gin.H{
			"message": "Senha redefinida com sucesso!",
			"email":   tokenInfo.Email,
		})

	// ── Academia ───────────────────────────────────────────────────────────
	case "academia":
		var emailVerificado bool
		if err = client.DB().QueryRow(
			`SELECT COALESCE(email_verificado, FALSE) FROM projection_academias WHERE id = $1`,
			tokenInfo.UserID,
		).Scan(&emailVerificado); err != nil {
			utils.RespondWithNotFoundError(c, "academia")
			return
		}
		if !emailVerificado {
			utils.RespondWithForbiddenError(c, "Por favor, verifique seu email antes de resetar a senha")
			return
		}

		repository := apoio.GetRepository(c)
		academiaAgg, err := repository.Load(tokenInfo.UserID, "Academia")
		if err != nil {
			utils.RespondWithNotFoundError(c, "academia")
			return
		}
		academia, ok := academiaAgg.(*aggregates.Academia)
		if !ok {
			utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
			return
		}
		if err := academia.AlterarSenha(string(hashedPassword), uuid.Nil, "reset_senha"); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		audit := db.AuditContext{UserID: "sistema", UserType: "sistema", IP: c.ClientIP()}
		if err := repository.SaveWithAudit(academia, audit); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		invalidarPendentes()
		log.Printf("Senha resetada (event sourcing) para academia: %s", tokenInfo.Email)
		c.JSON(http.StatusOK, gin.H{
			"message":         "Senha resetada com sucesso!",
			"proximos_passos": "Faça login e altere para uma senha segura.",
		})

	// ── Estudante ──────────────────────────────────────────────────────────
	case "estudante":
		var emailVerificado bool
		if err = client.DB().QueryRow(
			`SELECT COALESCE(email_verificado, FALSE) FROM projection_estudantes WHERE id = $1`,
			tokenInfo.UserID,
		).Scan(&emailVerificado); err != nil {
			utils.RespondWithNotFoundError(c, "estudante")
			return
		}
		if !emailVerificado {
			utils.RespondWithForbiddenError(c, "Por favor, verifique seu email antes de resetar a senha")
			return
		}

		repository := apoio.GetRepository(c)
		estudanteAgg, err := repository.Load(tokenInfo.UserID, "Estudante")
		if err != nil {
			utils.RespondWithNotFoundError(c, "estudante")
			return
		}
		estudante, ok := estudanteAgg.(*aggregates.Estudante)
		if !ok {
			utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
			return
		}
		// Motivo reset_senha: limpa deve_alterar_senha e encerra as sessões anteriores.
		if err := estudante.AlterarSenhaComMotivo(string(hashedPassword), aggregates.MotivoSenhaResetSenha); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		audit := db.AuditContext{UserID: "sistema", UserType: "sistema", IP: c.ClientIP()}
		if err := repository.SaveWithAudit(estudante, audit); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		invalidarPendentes()
		log.Printf("Senha resetada (event sourcing) para estudante: %s", tokenInfo.Email)
		c.JSON(http.StatusOK, gin.H{
			"message":         "Senha resetada com sucesso!",
			"proximos_passos": "Faça login com sua nova senha.",
		})

	case "secretario", "professor", "encarregado":
		resetarSenhaContaPessoa(c, tokenInfo, string(hashedPassword), invalidarPendentes)

	default:
		utils.RespondWithValidationError(c, fmt.Errorf("tipo de usuário inválido no token: %s", tokenInfo.UserType))
	}
}
