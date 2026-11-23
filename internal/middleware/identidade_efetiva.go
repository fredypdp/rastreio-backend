package middleware

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"

	"spuri/internal/db"
	"spuri/internal/permissoes"
	"spuri/internal/utils"
)

// Plano de Atualizações, Parte 4 — identidade efetiva.
//
// O JWT de um secretário diz user_type="secretario"; o de um encarregado,
// "encarregado". Para o secretário trabalhar "em nome da academia" e o
// encarregado "em nome do educando" SEM reescrever as centenas de handlers que
// já tratam user_id como a academia/o estudante, este middleware, depois de
// validar a conta REAL (estado, sessão, senha temporária), substitui no
// contexto:
//
//	secretário  → user_type="academia",  user_id=<academia>,  codigo_academia
//	encarregado → user_type="estudante", user_id=<educando>   (cabeçalho X-Educando)
//
// e guarda quem realmente agiu em ator_id / ator_tipo (usado na auditoria).
//
// A substituição só acontece depois de uma PORTA com "negar por padrão":
//   - secretário: a rota tem de estar em permissoes.RegraDaRota e a permissão
//     exigida tem de estar no conjunto dele; rota ausente = negada;
//   - encarregado: só rotas do estudante, só do educando indicado, e conforme a
//     fase (ação enquanto o educando está no nível escolar; leitura depois);
//   - professor: só as rotas /professor/* e as de conta.
//
// As rotas de CONTA (logout, senha, perfil, e-mail, telefone, 2FA) nunca são
// substituídas: agem sobre a conta real de quem está autenticado.

// HeaderEducando é o cabeçalho com o código do educando em nome de quem o
// encarregado age.
const HeaderEducando = "X-Educando"

// Códigos de erro (campo "code") da porta de identidade.
const (
	CodigoSecretarioSemPermissao    = "SECRETARIO_SEM_PERMISSAO"
	CodigoAcaoReservadaAcademia     = "ACAO_RESERVADA_A_ACADEMIA"
	CodigoAcaoNegadaAoSecretario    = "ACAO_NAO_PERMITIDA_AO_SECRETARIO"
	CodigoRotaForaDoPerfil          = "ROTA_FORA_DO_PERFIL"
	CodigoEducandoObrigatorio       = "EDUCANDO_OBRIGATORIO"
	CodigoEducandoNaoVinculado      = "EDUCANDO_NAO_VINCULADO"
	CodigoEncarregadoSomenteLeitura = "ENCARREGADO_SOMENTE_LEITURA"
	CodigoAcademiaInativa           = "ACADEMIA_INATIVA"
)

// TiposContaPessoa são os três tipos novos (projection_contas_pessoa).
func ehContaPessoa(tipo string) bool {
	return tipo == "secretario" || tipo == "professor" || tipo == "encarregado"
}

// rotaDeContaPropria diz se a rota age sobre a conta REAL de quem chama.
func rotaDeContaPropria(method, fullPath string) bool {
	switch method + " " + fullPath {
	case "POST /logout", "PUT /alterar-senha", "GET /meu-perfil", "PUT /me/email", "PUT /me/telefone",
		"GET /dois-fatores",
		"POST /dois-fatores/ativacao/solicitar", "POST /dois-fatores/ativacao/confirmar",
		"POST /dois-fatores/desativacao/solicitar", "POST /dois-fatores/desativacao/confirmar",
		"POST /email/verificar-email/solicitar":
		return true
	}
	return false
}

// RotaDeContaPropria expõe a lista às varreduras de teste do router.
func RotaDeContaPropria(method, fullPath string) bool { return rotaDeContaPropria(method, fullPath) }

func responderPortaNegada(c *gin.Context, status int, code, mensagem string, extra gin.H) {
	dados := gin.H{"code": code}
	for k, v := range extra {
		dados[k] = v
	}
	utils.RespondWithErrorData(c, status, mensagem, nil, dados)
	c.Abort()
}

// GetAtor devolve quem realmente agiu quando a identidade foi substituída
// (secretário ou encarregado). ok=false quando o usuário age em nome próprio.
func GetAtor(c *gin.Context) (id uuid.UUID, tipo string, ok bool) {
	rawID, existe := c.Get("ator_id")
	if !existe {
		return uuid.Nil, "", false
	}
	id, _ = rawID.(uuid.UUID)
	rawTipo, _ := c.Get("ator_tipo")
	tipo, _ = rawTipo.(string)
	return id, tipo, id != uuid.Nil && tipo != ""
}

// GetPermissoesSecretario devolve o conjunto do secretário autenticado.
func GetPermissoesSecretario(c *gin.Context) []string {
	v, _ := c.Get("permissoes_secretario")
	p, _ := v.([]string)
	return p
}

// aplicarIdentidade é chamada pelo AuthMiddleware depois de a conta real
// passar nas verificações. Devolve false quando já respondeu (e abortou).
func aplicarIdentidade(c *gin.Context, claims *Claims) bool {
	method, fullPath := c.Request.Method, c.FullPath()
	real := claims.UserType

	// Tipos antigos e rotas de conta: identidade real, sem substituição.
	if !ehContaPessoa(real) || rotaDeContaPropria(method, fullPath) {
		c.Set("user_id", claims.UserID)
		c.Set("user_type", real)
		if ehContaPessoa(real) {
			definirCodigoAcademiaDaConta(c, claims.UserID, real)
		}
		return true
	}

	clientRaw, existe := c.Get("dbClient")
	if !existe {
		// Erro de configuração do router; o handler falhará com 500.
		c.Set("user_id", claims.UserID)
		c.Set("user_type", real)
		return true
	}
	client := clientRaw.(*db.Client)

	switch real {
	case "secretario":
		return aplicarSecretario(c, client, claims, method, fullPath)
	case "professor":
		return aplicarProfessor(c, client, claims, fullPath)
	default:
		return aplicarEncarregado(c, client, claims, method, fullPath)
	}
}

// AplicarIdentidadeOpcional é a versão para rotas públicas com visão ampliada
// (OptionalAuthMiddleware): o secretário enxerga o que a academia enxerga; os
// demais tipos novos seguem com a identidade real.
func aplicarIdentidadeOpcional(c *gin.Context, claims *Claims) {
	if claims.UserType != "secretario" {
		c.Set("user_id", claims.UserID)
		c.Set("user_type", claims.UserType)
		return
	}
	clientRaw, existe := c.Get("dbClient")
	if !existe {
		return
	}
	client := clientRaw.(*db.Client)
	acad, err := carregarAcademiaDoSecretario(client, claims.UserID)
	if err != nil || acad == nil || acad.statusAcademia != "ativo" {
		// Sem certeza: segue como anónimo (nunca com visão ampliada).
		return
	}
	definirIdentidadeAcademia(c, claims.UserID, acad)
}

// ── Secretário ──────────────────────────────────────────────────────────────

type academiaDoSecretario struct {
	academiaID     uuid.UUID
	codigoAcademia string
	permissoes     []string
	statusAcademia string
}

func carregarAcademiaDoSecretario(client *db.Client, secretarioID uuid.UUID) (*academiaDoSecretario, error) {
	var (
		a       academiaDoSecretario
		perms   pq.StringArray
		acadStr sql.NullString
		cod     sql.NullString
	)
	err := client.DB().QueryRow(`
		SELECT c.academia_id::text, c.codigo_academia, c.permissoes, COALESCE(a.status, 'inexistente')
		  FROM projection_contas_pessoa c
		  LEFT JOIN projection_academias a ON a.id = c.academia_id
		 WHERE c.id = $1 AND c.tipo = 'secretario'`, secretarioID).Scan(&acadStr, &cod, &perms, &a.statusAcademia)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	id, perr := uuid.Parse(acadStr.String)
	if perr != nil {
		return nil, fmt.Errorf("secretário sem academia válida")
	}
	a.academiaID, a.codigoAcademia, a.permissoes = id, cod.String, []string(perms)
	return &a, nil
}

func definirIdentidadeAcademia(c *gin.Context, secretarioID uuid.UUID, a *academiaDoSecretario) {
	c.Set("user_id", a.academiaID)
	c.Set("user_type", "academia")
	c.Set("codigo_academia", a.codigoAcademia)
	c.Set("ator_id", secretarioID)
	c.Set("ator_tipo", "secretario")
	c.Set("permissoes_secretario", a.permissoes)
}

func aplicarSecretario(c *gin.Context, client *db.Client, claims *Claims, method, fullPath string) bool {
	a, err := carregarAcademiaDoSecretario(client, claims.UserID)
	if err != nil || a == nil {
		log.Printf("❌ [AuthMiddleware] Falha ao carregar academia do secretário %s: %v", claims.UserID, err)
		utils.RespondWithError(c, http.StatusServiceUnavailable, "serviço temporariamente indisponível. Tente novamente em instantes.", nil)
		c.Abort()
		return false
	}
	if a.statusAcademia != "ativo" {
		responderPortaNegada(c, http.StatusForbidden, CodigoAcademiaInativa,
			"a academia deste secretário está inativa. Entre em contato com a academia.", nil)
		return false
	}
	regra, conhecida := permissoes.RegraDaRota(method, fullPath)
	switch {
	case !conhecida:
		// Negar por padrão: rota nova só é acessível ao secretário depois de
		// classificada em internal/permissoes/rotas.go.
		responderPortaNegada(c, http.StatusForbidden, CodigoRotaForaDoPerfil,
			"esta ação não está disponível para o perfil de secretário.", nil)
		return false
	case regra.Tipo == permissoes.Reservada:
		responderPortaNegada(c, http.StatusForbidden, CodigoAcaoReservadaAcademia,
			"esta ação é reservada à conta da academia.", nil)
		return false
	case regra.Tipo == permissoes.Negada:
		responderPortaNegada(c, http.StatusForbidden, CodigoAcaoNegadaAoSecretario,
			"esta ação não é permitida ao secretário (lançar e corrigir notas e faltas é função dos professores).", nil)
		return false
	case regra.Tipo == permissoes.ExigePermissao && !permissoes.Tem(a.permissoes, regra.Permissao):
		responderPortaNegada(c, http.StatusForbidden, CodigoSecretarioSemPermissao,
			"você não tem permissão para esta ação.", gin.H{"permissao_necessaria": regra.Permissao})
		return false
	}
	definirIdentidadeAcademia(c, claims.UserID, a)
	return true
}

// ── Professor ───────────────────────────────────────────────────────────────

func aplicarProfessor(c *gin.Context, client *db.Client, claims *Claims, fullPath string) bool {
	// Só as rotas /professor/* (e as de conta, tratadas antes).
	if !strings.HasPrefix(fullPath, "/professor/") {
		responderPortaNegada(c, http.StatusForbidden, CodigoRotaForaDoPerfil,
			"esta ação não está disponível para o perfil de professor.", nil)
		return false
	}
	var statusAcademia string
	err := client.DB().QueryRow(`
		SELECT COALESCE(a.status, 'inexistente')
		  FROM projection_contas_pessoa c LEFT JOIN projection_academias a ON a.id = c.academia_id
		 WHERE c.id = $1 AND c.tipo = 'professor'`, claims.UserID).Scan(&statusAcademia)
	if err != nil {
		utils.RespondWithError(c, http.StatusServiceUnavailable, "serviço temporariamente indisponível. Tente novamente em instantes.", nil)
		c.Abort()
		return false
	}
	if statusAcademia != "ativo" {
		responderPortaNegada(c, http.StatusForbidden, CodigoAcademiaInativa,
			"a academia deste professor está inativa. Entre em contato com a academia.", nil)
		return false
	}
	c.Set("user_id", claims.UserID)
	c.Set("user_type", "professor")
	definirCodigoAcademiaDaConta(c, claims.UserID, "professor")
	return true
}

// definirCodigoAcademiaDaConta publica codigo_academia para secretário e professor.
func definirCodigoAcademiaDaConta(c *gin.Context, id uuid.UUID, tipo string) {
	if tipo != "secretario" && tipo != "professor" {
		return
	}
	clientRaw, existe := c.Get("dbClient")
	if !existe {
		return
	}
	var cod sql.NullString
	if err := clientRaw.(*db.Client).DB().QueryRow(
		`SELECT codigo_academia FROM projection_contas_pessoa WHERE id = $1`, id).Scan(&cod); err == nil && cod.Valid {
		c.Set("codigo_academia", cod.String)
	}
}

// ── Encarregado ─────────────────────────────────────────────────────────────

// anosEscolares são os valores de ano_academico do período escolar. Servem para
// restringir, na fase de leitura, as notas e faltas ao nível escolar.
func anosEscolares() []string {
	var anos []string
	for i := 1; i <= 9; i++ {
		anos = append(anos, fmt.Sprintf("%d_ano_fundamental", i))
	}
	for i := 1; i <= 4; i++ {
		anos = append(anos, fmt.Sprintf("%d_ano_medio", i))
	}
	return anos
}

// rotaDeLeituraEncarregadoPosEscolar: depois de o educando concluir o nível
// escolar o encarregado só CONSULTA notas e faltas do período escolar.
func rotaDeLeituraEncarregadoPosEscolar(method, fullPath string) bool {
	if method != http.MethodGet {
		return false
	}
	return fullPath == "/notas-estudante/:codigo" || fullPath == "/faltas-estudante/:codigo"
}

// rotaNegadaAoEncarregado: rotas do estudante que são do próprio estudante.
func rotaNegadaAoEncarregado(method, fullPath string) bool {
	switch method + " " + fullPath {
	case "DELETE /estudante/conta", "DELETE /estudante/encarregado":
		return true
	}
	return false
}

func aplicarEncarregado(c *gin.Context, client *db.Client, claims *Claims, method, fullPath string) bool {
	// Rotas do próprio encarregado (sem educando).
	if strings.HasPrefix(fullPath, "/encarregado/") {
		c.Set("user_id", claims.UserID)
		c.Set("user_type", "encarregado")
		return true
	}
	codigo := strings.TrimSpace(c.GetHeader(HeaderEducando))
	if codigo == "" {
		responderPortaNegada(c, http.StatusForbidden, CodigoEducandoObrigatorio,
			"informe o educando no cabeçalho "+HeaderEducando+" (código do estudante).", nil)
		return false
	}
	var (
		estudanteID         uuid.UUID
		status, statusMedio string
		anoSuperior         sql.NullString
	)
	err := client.DB().QueryRow(`
		SELECT e.id, e.status, COALESCE(e.status_escolar_medio,''),
		       e.ano_superior
		  FROM projection_encarregado_educandos x
		  JOIN projection_estudantes e ON e.id = x.estudante_id
		 WHERE x.encarregado_id = $1 AND UPPER(x.codigo_estudante) = UPPER($2) AND x.desvinculado_em IS NULL`,
		claims.UserID, codigo).Scan(&estudanteID, &status, &statusMedio, &anoSuperior)
	if err == sql.ErrNoRows {
		responderPortaNegada(c, http.StatusForbidden, CodigoEducandoNaoVinculado,
			"este estudante não é educando da sua conta.", nil)
		return false
	}
	if err != nil {
		utils.RespondWithError(c, http.StatusServiceUnavailable, "serviço temporariamente indisponível. Tente novamente em instantes.", nil)
		c.Abort()
		return false
	}
	if status == "deletado" {
		responderPortaNegada(c, http.StatusForbidden, CodigoEducandoNaoVinculado, "a conta do educando está indisponível.", nil)
		return false
	}
	if rotaNegadaAoEncarregado(method, fullPath) {
		responderPortaNegada(c, http.StatusForbidden, CodigoRotaForaDoPerfil,
			"esta ação só pode ser feita pelo próprio estudante.", nil)
		return false
	}
	var anoSup *string
	if anoSuperior.Valid {
		anoSup = &anoSuperior.String
	}
	finalizouEscolar := utils.FinalizouNivelEscolar(statusMedio, anoSup)
	if finalizouEscolar {
		if !rotaDeLeituraEncarregadoPosEscolar(method, fullPath) {
			responderPortaNegada(c, http.StatusForbidden, CodigoEncarregadoSomenteLeitura,
				"o educando concluiu o nível escolar: o encarregado apenas consulta as notas e faltas do período escolar.", nil)
			return false
		}
		restringirAoPeriodoEscolar(c)
	} else if status == "inativo" && !rotaPermitidaEstudanteInativo(method, fullPath) {
		// Educando desvinculado: mesma regra de escrita do próprio estudante.
		responderPortaNegada(c, http.StatusForbidden, "CONTA_DESVINCULADA",
			"conta desvinculada: apenas consulta e pagamentos estão disponíveis", nil)
		return false
	}
	c.Set("user_id", estudanteID)
	c.Set("user_type", "estudante")
	c.Set("ator_id", claims.UserID)
	c.Set("ator_tipo", "encarregado")
	return true
}

// restringirAoPeriodoEscolar reescreve a query para limitar ano_academico aos
// anos do nível escolar, ignorando o que o cliente enviou nesse parâmetro.
func restringirAoPeriodoEscolar(c *gin.Context) {
	q := c.Request.URL.Query()
	q.Del("ano_academico")
	for _, ano := range anosEscolares() {
		q.Add("ano_academico", ano)
	}
	c.Request.URL.RawQuery = q.Encode()
}
