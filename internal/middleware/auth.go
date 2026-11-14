package middleware

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"spuri/internal/db"
	"spuri/internal/utils"
)

type Claims struct {
	UserID     uuid.UUID `json:"user_id"`
	UserType   string    `json:"user_type"`
	jwt.RegisteredClaims
}

var jwtSecret []byte

func init() {
	secret := os.Getenv("JWT_SECRET")
	env := strings.ToLower(strings.TrimSpace(os.Getenv("ENV")))

	if secret == "" {
		secretBytes := make([]byte, 32)
		if _, err := rand.Read(secretBytes); err != nil {
			log.Fatalf("[FATAL] não foi possível gerar JWT_SECRET efêmero: %v", err)
		}
		jwtSecret = secretBytes
		log.Printf("[WARN] JWT_SECRET ausente; usando segredo efêmero até a validação de inicialização (%s)", env)
		return
	}
	jwtSecret = []byte(secret)
	log.Printf("✅ [JWT] Secret configurado (%d bytes)", len(jwtSecret))
}

// ValidateJWTConfig must be called by the executable after loading its
// environment. Package initialization cannot terminate unit-test processes.
func ValidateJWTConfig() error {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("ENV")))
	if strings.TrimSpace(os.Getenv("JWT_SECRET")) == "" && env != "development" && env != "test" {
		return fmt.Errorf("JWT_SECRET não configurado")
	}
	return nil
}

// getJWTExpiryHours retorna o número de horas de validade do token.
func getJWTExpiryHours() int {
	const defaultHours = 24
	hoursStr := os.Getenv("JWT_EXPIRY_HOURS")
	if hoursStr == "" {
		return defaultHours
	}
	hours, err := strconv.Atoi(hoursStr)
	if err != nil || hours <= 0 {
		log.Printf("⚠️  [JWT] JWT_EXPIRY_HOURS inválido ('%s'), usando padrão %dh", hoursStr, defaultHours)
		return defaultHours
	}
	return hours
}

func GenerateToken(userID uuid.UUID, userType string) (string, error) {
	expiryHours := getJWTExpiryHours()

	claims := Claims{
		UserID:     userID,
		UserType:   userType,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(expiryHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		log.Printf("❌ [GenerateToken] Erro ao assinar token: %v", err)
		return "", err
	}

	log.Printf("✅ [GenerateToken] Token gerado - UserID: %s, Type: %s, Expiry: %dh", userID, userType, expiryHours)
	return tokenString, nil
}

// CodigoSenhaTemporaria é o "code" devolvido (403) enquanto a conta ainda usa a
// senha temporária e tenta aceder a qualquer rota fora da lista abaixo. O
// frontend usa-o para redirecionar para a troca obrigatória (SEC-04).
const CodigoSenhaTemporaria = "SENHA_TEMPORARIA"

// rotaTrocaSenhaObrigatoria diz se a rota pode ser usada com a senha temporária
// ainda ativa: ler o próprio perfil, trocar a senha e encerrar a sessão — nada
// mais. Compara método E caminho exatos.
func rotaTrocaSenhaObrigatoria(method, path string) bool {
	switch {
	case method == http.MethodPut && path == "/alterar-senha":
		return true
	case method == http.MethodGet && path == "/meu-perfil":
		return true
	case method == http.MethodPost && path == "/logout":
		return true
	}
	return false
}

// sessaoInvalidadaPorReset diz se o token foi emitido antes de
// sessoes_validas_desde (reset de senha por link ou reenvio de credenciais). A
// comparação é feita em segundos, pois o "iat" do JWT não tem fração: um token
// emitido no mesmo segundo do evento não é descartado (o titular que acabou de
// redefinir a senha e entra de imediato não pode ser expulso).
func sessaoInvalidadaPorReset(emitidoEm *jwt.NumericDate, desde sql.NullTime) bool {
	if !desde.Valid {
		return false
	}
	if emitidoEm == nil {
		return true
	}
	return emitidoEm.Time.Before(desde.Time.Truncate(time.Second))
}

// rotaPermitidaEstudanteInativo define as rotas que um estudante com status
// "inativo" (desvinculado) pode acessar. Todas as leituras (GET/HEAD/OPTIONS)
// são permitidas. Para escrita, apenas uma allowlist explícita de rotas feitas
// para o estudante desvinculado ou de pagamento.
func rotaPermitidaEstudanteInativo(method, fullPath string) bool {
	// Consultar tudo o que o envolve (GET, HEAD, OPTIONS)
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	// Allowlist de rotas de escrita permitidas para estudante inativo
	switch method + " " + fullPath {
	// Conta
	case "POST /logout", "PUT /alterar-senha", "PUT /me/email", "PUT /me/telefone",
		"POST /dois-fatores/ativacao/solicitar", "POST /dois-fatores/ativacao/confirmar",
		"POST /dois-fatores/desativacao/solicitar", "POST /dois-fatores/desativacao/confirmar",
		"PUT /estudante/encarregado/telefone":
		return true
	// Pagamentos
	case "POST /financeiro/mensalidades/pagamento",
		"POST /financeiro/servicos-extras/taxa-inscricao/pagamento",
		"POST /financeiro/servicos-extras/obrigacao/pagamento":
		return true
	// Feitas para o estudante desvinculado
	case "PUT /estudante/bilhete-identidade",
		"POST /estudante/solicitacoes-status/revinculacao/:codigo_academia",
		"DELETE /estudante/conta":
		return true
	}
	return false
}

// AuthMiddleware valida o JWT e verifica se o status do usuário permite acesso.
//
// FIX AUTH-01 (E4-AA-02): além de validar assinatura e expiração, este middleware
// consulta a projeção de leitura correspondente ao userType e rejeita tokens de
// usuários cujo status não permite acesso. Isso garante que a desativação de um
// usuário tem efeito imediato — sem esperar a expiração natural do token.
//
// Regras de status por tipo (SEC-12/13):
//   - admin: só "ativo"
//   - academia: só "ativo" (exceto rotas de conta que não têm gate de estado)
//   - estudante: qualquer status EXCETO "deletado"; "inativo" tem escrita
//     restrita por método (rotaPermitidaEstudanteInativo)
//
// Fluxo:
//  1. Extrair e validar JWT (assinatura + expiração)
//  2. Obter dbClient do contexto (injetado pelo setupRouter)
//  3. Consultar status do usuário na projeção correspondente (todos os tipos)
//  4. Rejeitar com 401 se status não permitir acesso
//  5. Aplicar regra por método para estudante "inativo" (403 CONTA_DESVINCULADA)
//  6. Verificar reset de senha (SEC-10) e senha temporária (SEC-04)
//  7. Injetar user_id e user_type no contexto Gin
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		if authHeader == "" {
			log.Printf("❌ [AuthMiddleware] Token não fornecido - IP: %s", c.ClientIP())
			utils.RespondWithError(c, http.StatusUnauthorized, "token não fornecido", nil)
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			log.Printf("❌ [AuthMiddleware] Formato de token inválido (sem 'Bearer ')")
			utils.RespondWithError(c, http.StatusUnauthorized, "formato de token inválido", nil)
			c.Abort()
			return
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			log.Printf("❌ [AuthMiddleware] Token inválido ou expirado: %v - IP: %s", err, c.ClientIP())
			utils.RespondWithError(c, http.StatusUnauthorized, "token inválido ou expirado", nil)
			c.Abort()
			return
		}

		// FIX AUTH-01 (E4-AA-02): verificar status para TODOS os tipos de usuário,
		// incluindo admin. A verificação é universal — qualquer tipo de usuário
		// desativado é bloqueado aqui, independentemente de middlewares específicos
		// de role. Isso fecha o gap nas rotas do grupo "protected".
		//
		// Uma única consulta traz status, deve_alterar_senha e
		// sessoes_validas_desde (SEC-04 / SEC-10).
		estado, statusErr := consultarEstadoUsuario(c, claims.UserID, claims.UserType)

		// statusErr != nil significa que o usuário foi encontrado mas o status
		// bloqueia o acesso (deletado para todos, inativo para admin/academia).
		// Para estudante inativo, statusErr é nil (o estado vem em estado.Status).
		if statusErr != nil {
			log.Printf("❌ [AuthMiddleware] Usuário bloqueado por status - UserID: %s, Type: %s: %v",
				claims.UserID, claims.UserType, statusErr)
			utils.RespondWithError(c, http.StatusUnauthorized,
				"conta inativa ou não encontrada. Entre em contato com o suporte.", statusErr)
			c.Abort()
			return
		}

		if estado != nil {
			// SEC-10: reset de senha por link e reenvio de credenciais encerram as
			// sessões que já existiam.
			if sessaoInvalidadaPorReset(claims.IssuedAt, estado.SessoesValidasDesde) {
				log.Printf("❌ [AuthMiddleware] Sessão invalidada por redefinição de senha - UserID: %s, Type: %s", claims.UserID, claims.UserType)
				utils.RespondWithError(c, http.StatusUnauthorized, "sessão encerrada: faça login novamente", nil)
				c.Abort()
				return
			}
			// SEC-04: com a senha temporária ainda ativa, só perfil/trocar senha/logout.
			if estado.DeveAlterarSenha && !rotaTrocaSenhaObrigatoria(c.Request.Method, c.Request.URL.Path) {
				utils.RespondWithErrorData(c, http.StatusForbidden,
					"é obrigatório alterar a senha temporária antes de continuar", nil,
					gin.H{"code": CodigoSenhaTemporaria})
				c.Abort()
				return
			}
			// SEC-12: estudante "inativo" (desvinculado) — escrita restrita por método.
			// Leitura (GET/HEAD/OPTIONS) sempre permitida; escrita só allowlist.
			if claims.UserType == "estudante" && estado.Status == "inativo" &&
				!rotaPermitidaEstudanteInativo(c.Request.Method, c.FullPath()) {
				utils.RespondWithErrorData(c, http.StatusForbidden,
					"conta desvinculada: apenas consulta e pagamentos estão disponíveis", nil,
					gin.H{"code": "CONTA_DESVINCULADA"})
				c.Abort()
				return
			}
		}

		c.Set("user_id", claims.UserID)
		c.Set("user_type", claims.UserType)

		log.Printf("✅ [AuthMiddleware] Autenticado - UserID: %s, UserType: %s", claims.UserID, claims.UserType)
		c.Next()
	}
}

func statusInativo(err error) bool { _, ok := err.(*statusInativoError); return ok }
func rotaFinanceiraRestrita(path string) bool {
	return strings.HasPrefix(path, "/financeiro/mensalidades/estudante/") || path == "/financeiro/mensalidades/pagamento"
}

// OptionalAuthMiddleware autentica a requisição quando um token Bearer é enviado,
// mas permite o acesso sem Authorization. Tokens presentes continuam sendo
// validados para preservar o comportamento seguro de rotas que têm resposta
// ampliada para usuários autenticados.
func OptionalAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Next()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			log.Printf("❌ [OptionalAuthMiddleware] Formato de token inválido (sem 'Bearer ')")
			utils.RespondWithError(c, http.StatusUnauthorized, "formato de token inválido", nil)
			c.Abort()
			return
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			log.Printf("❌ [OptionalAuthMiddleware] Token inválido ou expirado: %v - IP: %s", err, c.ClientIP())
			utils.RespondWithError(c, http.StatusUnauthorized, "token inválido ou expirado", nil)
			c.Abort()
			return
		}

		estado, err := consultarEstadoUsuario(c, claims.UserID, claims.UserType)
		if err != nil {
			log.Printf("❌ [OptionalAuthMiddleware] Usuário inativo ou não encontrado - UserID: %s, Type: %s: %v",
				claims.UserID, claims.UserType, err)
			utils.RespondWithError(c, http.StatusUnauthorized,
				"conta inativa ou não encontrada. Entre em contato com o suporte.", err)
			c.Abort()
			return
		}
		if estado != nil {
			if sessaoInvalidadaPorReset(claims.IssuedAt, estado.SessoesValidasDesde) {
				utils.RespondWithError(c, http.StatusUnauthorized, "sessão encerrada: faça login novamente", nil)
				c.Abort()
				return
			}
			// Senha temporária ainda ativa: a rota é pública, mas a sessão não
			// ganha a visão ampliada de utilizador autenticado.
			if estado.DeveAlterarSenha {
				c.Next()
				return
			}
		}

		c.Set("user_id", claims.UserID)
		c.Set("user_type", claims.UserType)

		log.Printf("✅ [OptionalAuthMiddleware] Autenticado - UserID: %s, UserType: %s", claims.UserID, claims.UserType)
		c.Next()
	}
}

// estadoUsuario é o recorte da projeção que o middleware precisa em cada
// requisição: status (bloqueio imediato), deve_alterar_senha (SEC-04) e
// sessoes_validas_desde (SEC-10).
type estadoUsuario struct {
	Status              string
	DeveAlterarSenha    bool
	SessoesValidasDesde sql.NullTime
}

// consultarEstadoUsuario consulta a projeção correspondente ao userType numa só
// query e devolve (estado, erro):
//   - usuário inexistente → (nil, sql.ErrNoRows);
//   - status bloqueado    → (estado, *statusInativoError) — o estado vem junto
//     porque a sessão restrita do estudante inativo ainda precisa dele;
//   - erro de banco ou dbClient ausente → (nil, nil): degradação graciosa
//     documentada (FIX E4-CI-03), com log de aviso.
//
// FIX AUTH-01 (E4-AA-02): admin é verificado aqui, não apenas em RequireAdmin.
//
// FIX E4-QI-02: o nome da tabela vem de um switch fechado com constantes
// literais — nunca de entrada do usuário; o valor variável (userID) vai por
// placeholder $1. A comparação é feita por texto (id::text) para tolerar bases
// legadas com id TEXT.
func consultarEstadoUsuario(c *gin.Context, userID uuid.UUID, userType string) (*estadoUsuario, error) {
	clientRaw, exists := c.Get("dbClient")
	if !exists {
		// dbClient ausente (erro de configuração do router): deixa passar para não
		// bloquear o arranque — o handler seguinte falhará com 500 via getDbClient().
		log.Printf("⚠️ [AuthMiddleware] dbClient ausente no contexto ao verificar status — path: %s", c.Request.URL.Path)
		return nil, nil
	}
	client := clientRaw.(*db.Client)

	var table string
	switch userType {
	case "estudante":
		table = "projection_estudantes"
	case "academia":
		table = "projection_academias"
	case "admin":
		table = "projection_admins"
	default:
		// Tipo desconhecido — não bloqueamos; o middleware específico de role tratará.
		return nil, nil
	}

	var (
		est            estadoUsuario
		codigoAcademia sql.NullString
		err            error
	)
	if userType == "academia" {
		err = client.DB().QueryRow(
			`SELECT status, codigo_academia, deve_alterar_senha, sessoes_validas_desde
			   FROM projection_academias WHERE id::text = $1`, userID.String(),
		).Scan(&est.Status, &codigoAcademia, &est.DeveAlterarSenha, &est.SessoesValidasDesde)
		if codigoAcademia.Valid {
			c.Set("codigo_academia", codigoAcademia.String)
		}
	} else {
		err = client.DB().QueryRow(
			`SELECT status, deve_alterar_senha, sessoes_validas_desde FROM `+table+` WHERE id::text = $1`, userID.String(),
		).Scan(&est.Status, &est.DeveAlterarSenha, &est.SessoesValidasDesde)
	}
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		// FIX E4-CI-03: em caso de erro de banco permitimos a passagem com aviso.
		log.Printf("⚠️ [AuthMiddleware] Erro ao verificar status do usuário tipo %s: %v", userType, err)
		return nil, nil
	}
	return &est, finalizarVerificacaoStatusUsuario(userType, est.Status, nil)
}

func finalizarVerificacaoStatusUsuario(userType, status string, err error) error {
	if err == sql.ErrNoRows {
		return sql.ErrNoRows
	}
	if err != nil {
		log.Printf("⚠️ [AuthMiddleware] Erro ao verificar status do usuário tipo %s: %v", userType, err)
		// FIX E4-CI-03: em caso de erro de banco (ex: timeout), permitimos a passagem
		// com aviso para não derrubar o serviço inteiro por instabilidade pontual do BD.
		// Trade-off: durante degradação, usuários desativados podem passar — logado para
		// auditoria e alertas do SIEM.
		return nil
	}

	// SEC-13: "deletado" bloqueia para todos os tipos
	if status == "deletado" {
		return &statusInativoError{userType: userType, status: status}
	}

	// Regras por tipo de usuário
	statusBloqueado := false
	switch userType {
	case "admin":
		statusBloqueado = status != "ativo"
	case "academia":
		statusBloqueado = status != "ativo"
	case "estudante":
		// estudante inativo passa (bloqueio de escrita é no middleware por método)
		statusBloqueado = false
	}
	if statusBloqueado {
		return &statusInativoError{userType: userType, status: status}
	}
	return nil
}

// statusInativoError representa o erro de usuário inativo.
type statusInativoError struct {
	userType string
	status   string
}

func (e *statusInativoError) Error() string {
	return e.userType + " está com status: " + e.status
}

func RequireAcademia() gin.HandlerFunc {
	return func(c *gin.Context) {
		userType, exists := c.Get("user_type")
		if !exists || userType != "academia" {
			log.Printf("❌ [RequireAcademia] Acesso negado - UserType: %v", userType)
			utils.RespondWithError(c, http.StatusForbidden, "acesso negado: apenas academias", nil)
			c.Abort()
			return
		}
		c.Next()
	}
}

func RequireEstudante() gin.HandlerFunc {
	return func(c *gin.Context) {
		userType, exists := c.Get("user_type")
		if !exists || userType != "estudante" {
			log.Printf("❌ [RequireEstudante] Acesso negado - UserType: %v", userType)
			utils.RespondWithError(c, http.StatusForbidden, "acesso negado: apenas estudantes", nil)
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireAdmin está definido em admin_auth_middleware.go.
// Mantido aqui apenas como referência — não redeclarar.

// RequireAcademiaOuAdmin bloqueia estudantes — apenas academias e admins passam.
func RequireAcademiaOuAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		userType, exists := c.Get("user_type")
		if !exists {
			utils.RespondWithError(c, http.StatusForbidden, "acesso negado", nil)
			c.Abort()
			return
		}
		ut, _ := userType.(string)
		if ut != "academia" && ut != "admin" {
			utils.RespondWithError(c, http.StatusForbidden, "acesso negado: apenas academias e administradores", nil)
			c.Abort()
			return
		}
		c.Next()
	}
}

// GetUserID extrai o UUID do usuário autenticado do contexto Gin.
func GetUserID(c *gin.Context) (uuid.UUID, bool) {
	userID, exists := c.Get("user_id")
	if !exists {
		return uuid.Nil, false
	}
	uid, ok := userID.(uuid.UUID)
	if !ok {
		return uuid.Nil, false
	}
	return uid, true
}

// GetUserType extrai o tipo do usuário autenticado do contexto Gin.
func GetUserType(c *gin.Context) (string, bool) {
	userType, exists := c.Get("user_type")
	if !exists {
		return "", false
	}
	ut, ok := userType.(string)
	return ut, ok
}
