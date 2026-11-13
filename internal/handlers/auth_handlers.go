package handlers

import (
	"log"
	"net/http"
	"spuri/internal/middleware"
	"spuri/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// mensagemCredenciaisInvalidas é a ÚNICA mensagem de falha de autenticação
// (usuário inexistente ou senha errada), para não permitir enumerar contas.
const mensagemCredenciaisInvalidas = "credenciais inválidas"

// LoginRequest é o corpo do único endpoint de autenticação do sistema.
//
// Campos:
//   - usuario: qualquer identificador único do usuário —
//     admin     → e-mail
//     academia  → código de academia ou e-mail
//     estudante → código de estudante ou e-mail (e-mail deve estar verificado)
//   - senha: senha em texto plano — comparada via bcrypt
//
// O tipo do usuário é inferido automaticamente pela busca em cascata
// (admin → academia → estudante), eliminando a necessidade de o cliente
// informar o campo "type" explicitamente.
type LoginRequest struct {
	Usuario string `json:"usuario" binding:"required"`
	Senha   string `json:"senha"   binding:"required"`
}

// Login é o único endpoint de autenticação do sistema.
// Atende todos os tipos de usuário: admin, academia e estudante.
//
// Segurança:
//   - Timing-safe: bcrypt é SEMPRE executado, mesmo quando o usuário não existe,
//     igualando o tempo de resposta a "senha errada" e prevenindo user enumeration.
//   - Busca em cascata: admin → academia → estudante. O primeiro resultado válido
//     é usado; os demais não são consultados.
//   - Status verificado ANTES de emitir o JWT — conta inativa nunca recebe token.
//   - Resposta genérica: não indica qual campo (usuário ou senha) estava errado.
//   - SenhaHash NUNCA incluída na resposta.
func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	// dummyHash: hash bcrypt válido usado quando o usuário não existe, para que
	// CompareHashAndPassword leve o mesmo tempo que no caso de senha errada,
	// prevenindo timing attack e user enumeration.
	const dummyHash = "$2a$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ012345"

	var (
		userID     uuid.UUID
		userName   string
		senhaHash  string
		identifier string // e-mail (admin) ou código (academia/estudante) — retornado no response
		userStatus string
		userType   string // inferido pela busca — "admin" | "academia" | "estudante"
		userRole   string // preenchido apenas para admin
		userFound  bool
		// emailNaoVerificado: login feito pelo e-mail ainda não verificado. Só é
		// comunicado DEPOIS de a senha estar correta (SEC-06): antes disso a
		// resposta seria diferente para quem acertou o identificador.
		emailNaoVerificado bool
	)

	// ── Busca em cascata: admin → academia → estudante ─────────────────────
	//
	// Ordem de prioridade deliberada:
	//   1. Admin   — identificador exclusivo: e-mail (não colide com academia/estudante)
	//   2. Academia — identificador: código ou e-mail
	//   3. Estudante — identificador: código, e-mail ou telefone
	//
	// A busca para assim que o primeiro resultado é encontrado.

	// ── 1. Admin ───────────────────────────────────────────────────────────
	adminProj := getAdminProjection(c)
	if adminProj == nil {
		return // getAdminProjection → getDbClient já abortou com 500
	}
	admin, err := adminProj.GetByEmailForLogin(req.Usuario)
	if err != nil {
		log.Printf("❌ [Login/admin] Erro ao buscar admin '%s': %v", req.Usuario, err)
		utils.RespondWithInternalError(c, err)
		return
	}
	if admin != nil {
		userID = admin.ID
		userName = admin.Nome
		senhaHash = admin.SenhaHash
		identifier = admin.Email
		userStatus = admin.Status
		userRole = admin.Role
		userType = "admin"
		userFound = true
	}

	// ── 2. Academia ────────────────────────────────────────────────────────
	if !userFound {
		academiaProj := getAcademiaProjection(c)
		if academiaProj == nil {
			return
		}
		academia, err := academiaProj.GetByCodigoOrEmail(req.Usuario)
		if err != nil {
			log.Printf("❌ [Login/academia] Erro ao buscar academia '%s': %v", req.Usuario, err)
			utils.RespondWithInternalError(c, err)
			return
		}
		if academia != nil {
			// Bloquear login via e-mail se ainda não verificado.
			// Login por código de academia (ex: AC1234) é sempre permitido —
			// o código é atribuído pelo sistema e não requer verificação adicional.
			emailNaoVerificado = academia.Email != nil && *academia.Email == req.Usuario && !academia.EmailVerificado

			userID = academia.ID
			userName = academia.Nome
			senhaHash = academia.SenhaHash
			identifier = academia.CodigoAcademia
			userStatus = academia.Status
			userType = "academia"
			userFound = true
		}
	}

	// ── 3. Estudante ───────────────────────────────────────────────────────
	// GetAuthByIdentificador aceita código ou e-mail.
	// Telefone removido — verificação de telefone ainda não existe no sistema.
	if !userFound {
		estudanteProj := getEstudanteProjection(c)
		if estudanteProj == nil {
			return
		}
		estudanteAuth, err := estudanteProj.GetAuthByIdentificador(req.Usuario)
		if err != nil {
			log.Printf("❌ [Login/estudante] Erro ao buscar estudante '%s': %v", req.Usuario, err)
			utils.RespondWithInternalError(c, err)
			return
		}
		if estudanteAuth != nil {
			// Bloquear login via e-mail se ainda não verificado.
			// Login por código de estudante (ex: ABC1234) é sempre permitido —
			// o código é atribuído pelo sistema e não requer verificação adicional.
			emailNaoVerificado = estudanteAuth.Email != nil && *estudanteAuth.Email == req.Usuario && !estudanteAuth.EmailVerificado

			userID = estudanteAuth.ID
			userName = estudanteAuth.Nome
			senhaHash = estudanteAuth.Hash
			identifier = estudanteAuth.Codigo
			userStatus = estudanteAuth.Status
			userType = "estudante"
			userFound = true
		}
	}

	// ── Comparação bcrypt (timing-safe) ────────────────────────────────────
	// Sempre executada — mesmo quando userFound=false — para que o tempo de
	// resposta seja idêntico entre "usuário não existe" e "senha errada".
	hashToCompare := senhaHash
	if !userFound {
		hashToCompare = dummyHash
	}
	bcryptErr := bcrypt.CompareHashAndPassword([]byte(hashToCompare), []byte(req.Senha))

	// Resposta deliberadamente genérica: não revela qual campo estava errado
	// nem qual tipo de usuário foi (ou não) encontrado.
	//
	// SEC-06: usuário inexistente e senha errada devolvem EXATAMENTE a mesma
	// resposta. O motivo real só vai para o log do servidor.
	if !userFound {
		log.Printf("[INFO] [Login] Falha de autenticação: usuário não encontrado")
		utils.RespondWithError(c, http.StatusUnauthorized, mensagemCredenciaisInvalidas, nil)
		return
	}
	if bcryptErr != nil {
		log.Printf("[INFO] [Login] Falha de autenticação: senha incorreta (tipo %s)", userType)
		utils.RespondWithError(c, http.StatusUnauthorized, mensagemCredenciaisInvalidas, nil)
		return
	}
	// Senha correta: só agora é seguro dizer que o e-mail ainda não foi verificado.
	if emailNaoVerificado {
		log.Printf("[INFO] [Login] %s tentou login com e-mail não verificado", userType)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "e-mail não verificado. Verifique sua caixa de entrada e confirme seu e-mail antes de fazer login.",
		})
		return
	}

	// ── Verificação de status ANTES de emitir o JWT ────────────────────────
	// Estudantes podem autenticar com qualquer status diferente de "inativo"
	// (ex.: finalizado/regularização), enquanto admins e academias continuam
	// exigindo status exatamente "ativo". A regra vive em avaliarStatusLogin,
	// compartilhada com a segunda etapa do login (2FA).
	acessoRestritoFinanceiro, msgBloqueio, statusBloqueado := avaliarStatusLogin(userType, userStatus)
	if statusBloqueado {
		log.Printf("[INFO] [Login] Conta bloqueada por status — type: %s, identifier: %s, status: %s",
			userType, identifier, userStatus)
		utils.RespondWithError(c, http.StatusUnauthorized, msgBloqueio, nil)
		return
	}

	// ── Verificação em duas etapas (tarefa 122) ────────────────────────────
	// Senha e status corretos. Se o usuário ativou o 2FA, NENHUM token é emitido
	// aqui (nem o restrito ao financeiro): envia-se o código por e-mail e a
	// resposta pede a segunda etapa. Qualquer falha termina em erro, nunca em token.
	if iniciarDoisFatoresNoLogin(c, userID, userType, userName) {
		return
	}

	responderLoginComSucesso(c, userID, userType, userName, identifier, userRole, acessoRestritoFinanceiro)
}

// avaliarStatusLogin aplica a regra de status do login (compartilhada entre o
// login direto e a segunda etapa do 2FA):
//   - admin/academia: só "ativo" autentica;
//   - estudante: qualquer status autentica; "inativo" recebe apenas o token
//     restrito ao financeiro (restrito=true).
//
// Devolve (restrito, mensagem, bloqueado).
func avaliarStatusLogin(userType, userStatus string) (bool, string, bool) {
	if userType == "estudante" {
		return userStatus == "inativo", "", false
	}
	if userStatus == "ativo" {
		return false, "", false
	}
	switch userType {
	case "admin":
		return false, "conta inativa. Entre em contato com o suporte.", true
	case "academia":
		return false, "academia inativa. Entre em contato com o administrador.", true
	}
	return false, "", true
}

// responderLoginComSucesso emite o JWT e escreve a resposta de login. É o ÚNICO
// ponto que emite token de login — usado pelo login direto e pela segunda etapa
// do 2FA, para que as duas respostas tenham exatamente o mesmo formato.
func responderLoginComSucesso(c *gin.Context, userID uuid.UUID, userType, userName, identifier, userRole string, acessoRestritoFinanceiro bool) {
	var (
		token string
		err   error
	)
	if acessoRestritoFinanceiro {
		token, err = middleware.GenerateRestrictedFinanceToken(userID, userType)
	} else {
		token, err = middleware.GenerateToken(userID, userType)
	}
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	log.Printf("✅ [Login] Autenticação bem-sucedida: %s (%s)", userName, userType)

	// ── Resposta — SenhaHash NUNCA incluída ────────────────────────────────
	resp := gin.H{
		"token": token,
		"nome":  userName,
		"type":  userType,
		// SEC-04: true enquanto a conta usa a senha temporária — o frontend leva
		// o titular direto para a troca obrigatória.
		"deve_alterar_senha": deveAlterarSenhaDe(c, userID, userType),
	}
	if userType == "admin" {
		resp["email"] = identifier
		resp["role"] = userRole
	} else {
		resp["codigo"] = identifier
	}

	c.JSON(http.StatusOK, resp)
}

// deveAlterarSenhaDe lê deve_alterar_senha da projeção do usuário. Em caso de
// erro devolve false: o AuthMiddleware impõe a regra de qualquer forma.
func deveAlterarSenhaDe(c *gin.Context, userID uuid.UUID, userType string) bool {
	var table string
	switch userType {
	case "admin":
		table = "projection_admins"
	case "academia":
		table = "projection_academias"
	case "estudante":
		table = "projection_estudantes"
	default:
		return false
	}
	var deve bool
	if err := getDbClient(c).DB().QueryRow(`SELECT deve_alterar_senha FROM `+table+` WHERE id::text = $1`, userID.String()).Scan(&deve); err != nil {
		return false
	}
	return deve
}

// Logout encerra a sessão no cliente.
//
// Como a API usa JWT stateless sem blacklist/revogação server-side,
// o logout efetivo consiste em o cliente descartar o token.
// Este endpoint existe para padronizar o fluxo no frontend e confirmar
// ao cliente que a operação de logout foi solicitada com sucesso.
func Logout(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "logout realizado com sucesso",
	})
}
