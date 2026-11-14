package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/bcrypt"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/middleware"
	"spuri/internal/services"
	"spuri/internal/utils"
)

// ============================================================================
// Verificação em duas etapas por e-mail (tarefa 122)
//
// Login:
//   POST /login                              → senha correta + 2FA ativo: devolve
//                                              {requer_dois_fatores, desafio_id,…}
//                                              SEM token e envia o código por e-mail
//   POST /login/dois-fatores/verificar       → código correto: emite o JWT
//   POST /login/dois-fatores/reenviar        → novo código (invalida o anterior)
//
// Gestão (autenticado):
//   GET  /dois-fatores
//   POST /dois-fatores/ativacao/solicitar    POST /dois-fatores/ativacao/confirmar
//   POST /dois-fatores/desativacao/solicitar POST /dois-fatores/desativacao/confirmar
//        (a desativação exige também a senha atual)
// ============================================================================

// doisFatoresSenderFactory cria o remetente do código. É uma variável de pacote
// para que os testes capturem o código sem depender de um provedor de e-mail.
var doisFatoresSenderFactory = func(c *gin.Context) services.DoisFatoresSender {
	return getEmailService(c)
}

func getDoisFatoresService(c *gin.Context) *services.DoisFatoresService {
	client := getDbClient(c)
	if client == nil {
		return nil
	}
	return services.NewDoisFatoresService(client.DB(), doisFatoresSenderFactory(c))
}

// ── Identidade lida da projeção ─────────────────────────────────────────────

// identidadeAuth reúne o que o 2FA precisa saber do usuário. SenhaHash nunca
// sai em nenhuma resposta.
type identidadeAuth struct {
	ID               uuid.UUID
	Tipo             string
	Nome             string
	Identificador    string // e-mail (admin) ou código (academia/estudante)
	Status           string
	Role             string // só admin
	Email            string
	EmailVerificado  bool
	SenhaHash        string
	DoisFatoresAtivo bool
}

func carregarIdentidadeAuth(ctx context.Context, conn *sqlx.DB, tipo string, id uuid.UUID) (*identidadeAuth, error) {
	ident := &identidadeAuth{ID: id, Tipo: tipo}
	var email sql.NullString
	var row *sql.Row
	switch tipo {
	case "admin":
		row = conn.QueryRowContext(ctx,
			`SELECT nome, email, status, role, senha_hash, email_verificado, dois_fatores_ativo
			   FROM projection_admins WHERE id = $1 AND status <> 'deletado'`, id)
		var role sql.NullString
		if err := row.Scan(&ident.Nome, &email, &ident.Status, &role, &ident.SenhaHash, &ident.EmailVerificado, &ident.DoisFatoresAtivo); err != nil {
			return nil, naoEncontradoOuErro(err)
		}
		ident.Role = role.String
		ident.Identificador = strings.TrimSpace(email.String) // admin identifica-se pelo e-mail
	case "academia":
		row = conn.QueryRowContext(ctx,
			`SELECT nome, codigo_academia, email, status, senha_hash, email_verificado, dois_fatores_ativo
			   FROM projection_academias WHERE id = $1 AND status <> 'deletado'`, id)
		if err := row.Scan(&ident.Nome, &ident.Identificador, &email, &ident.Status, &ident.SenhaHash, &ident.EmailVerificado, &ident.DoisFatoresAtivo); err != nil {
			return nil, naoEncontradoOuErro(err)
		}
	case "estudante":
		row = conn.QueryRowContext(ctx,
			`SELECT nome, codigo_estudante, email, status, senha_hash, COALESCE(email_verificado, FALSE), dois_fatores_ativo
			   FROM projection_estudantes WHERE id = $1 AND status <> 'deletado'`, id)
		if err := row.Scan(&ident.Nome, &ident.Identificador, &email, &ident.Status, &ident.SenhaHash, &ident.EmailVerificado, &ident.DoisFatoresAtivo); err != nil {
			return nil, naoEncontradoOuErro(err)
		}
	default:
		return nil, fmt.Errorf("tipo de usuário inválido: %s", tipo)
	}
	ident.Email = strings.TrimSpace(email.String)
	return ident, nil
}

// naoEncontradoOuErro devolve (nil, nil) quando o usuário não existe.
func naoEncontradoOuErro(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return errNaoEncontrado
	}
	return err
}

var errNaoEncontrado = errors.New("usuário não encontrado")

func carregarIdentidade(c *gin.Context, tipo string, id uuid.UUID) (*identidadeAuth, bool) {
	client := getDbClient(c)
	if client == nil {
		return nil, false
	}
	ident, err := carregarIdentidadeAuth(c.Request.Context(), client.DB(), tipo, id)
	if errors.Is(err, errNaoEncontrado) {
		return nil, true
	}
	if err != nil {
		log.Printf("❌ [2FA] erro ao carregar identidade (%s/%s): %v", tipo, id, err)
		utils.RespondWithInternalError(c, err)
		return nil, false
	}
	return ident, true
}

// ── Respostas ───────────────────────────────────────────────────────────────

func respostaDesafio(d *services.DoisFatoresDesafio) gin.H {
	return gin.H{
		"desafio_id":          d.ID,
		"metodo":              aggregates.MetodoDoisFatoresEmail,
		"email_mascarado":     maskEmail(d.EmailDestino),
		"expira_em_segundos":  d.ExpiraEmSegundos,
		"reenvio_em_segundos": d.ReenvioEmSegundos,
		"message":             fmt.Sprintf("Enviámos um código de 6 dígitos para %s.", maskEmail(d.EmailDestino)),
	}
}

// respondDoisFatoresError traduz os erros do serviço. statusCodigo é o status
// usado para código inválido/expirado/usado: 401 no login (a autenticação
// falhou) e 400 nos fluxos de ativação/desativação (a sessão continua válida).
func respondDoisFatoresError(c *gin.Context, err error, statusCodigo int) {
	var ag *services.ErrDoisFatoresAguardar
	var lim *services.ErrDoisFatoresLimiteEnvios
	var inc *services.ErrDoisFatoresCodigoIncorreto

	switch {
	case errors.As(err, &ag):
		c.Header("Retry-After", strconv.Itoa(ag.Segundos))
		data := gin.H{"retry_after_segundos": ag.Segundos}
		if ag.DesafioID != uuid.Nil {
			data["desafio_id"] = ag.DesafioID
			data["expira_em_segundos"] = ag.ExpiraEmSegundos
		}
		utils.RespondWithErrorData(c, http.StatusTooManyRequests, ag.Error(), err, data)
	case errors.As(err, &lim):
		c.Header("Retry-After", strconv.Itoa(lim.Segundos))
		utils.RespondWithErrorData(c, http.StatusTooManyRequests, lim.Error(), err, gin.H{"retry_after_segundos": lim.Segundos})
	case errors.As(err, &inc):
		utils.RespondWithErrorData(c, statusCodigo, inc.Error(), err, gin.H{"tentativas_restantes": inc.Restantes})
	case errors.Is(err, services.ErrDoisFatoresFormatoCodigo):
		utils.RespondWithError(c, http.StatusBadRequest, err.Error(), err)
	case errors.Is(err, services.ErrDoisFatoresEnvioFalhou):
		utils.RespondWithError(c, http.StatusServiceUnavailable, err.Error(), err)
	case errors.Is(err, services.ErrDoisFatoresSessaoExpirada):
		utils.RespondWithError(c, http.StatusUnauthorized, err.Error(), err)
	case errors.Is(err, services.ErrDoisFatoresDesafioInvalido),
		errors.Is(err, services.ErrDoisFatoresCodigoUsado),
		errors.Is(err, services.ErrDoisFatoresCodigoSubstituido),
		errors.Is(err, services.ErrDoisFatoresCodigoExpirado),
		errors.Is(err, services.ErrDoisFatoresTentativasExcedidas):
		utils.RespondWithError(c, statusCodigo, err.Error(), err)
	default:
		utils.RespondWithInternalError(c, err)
	}
}

// ── Limite de tentativas de senha (desativação) ─────────────────────────────

// Quem tem uma sessão aberta poderia tentar adivinhar a senha atual no fluxo de
// desativação. O limite abaixo é por usuário e em memória (por processo): é uma
// camada extra, não substitui o limite por desafio do serviço.
const (
	maxFalhasSenhaDoisFatores    = 5
	janelaFalhasSenhaDoisFatores = 15 * time.Minute
	tamanhoMaxMapaFalhasSenhaDF  = 10000
)

type falhasSenha struct {
	contagem int
	inicio   time.Time
}

type limitadorSenha struct {
	mu sync.Mutex
	m  map[string]*falhasSenha
}

var limitadorSenhaDoisFatores = &limitadorSenha{m: map[string]*falhasSenha{}}

// segundosBloqueado devolve >0 quando o usuário está bloqueado.
func (l *limitadorSenha) segundosBloqueado(chave string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.m[chave]
	if f == nil {
		return 0
	}
	restante := janelaFalhasSenhaDoisFatores - time.Since(f.inicio)
	if restante <= 0 {
		delete(l.m, chave)
		return 0
	}
	if f.contagem >= maxFalhasSenhaDoisFatores {
		return int(restante.Seconds()) + 1
	}
	return 0
}

func (l *limitadorSenha) registrarFalha(chave string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.m) > tamanhoMaxMapaFalhasSenhaDF {
		for k, v := range l.m {
			if time.Since(v.inicio) > janelaFalhasSenhaDoisFatores {
				delete(l.m, k)
			}
		}
	}
	f := l.m[chave]
	if f == nil || time.Since(f.inicio) > janelaFalhasSenhaDoisFatores {
		l.m[chave] = &falhasSenha{contagem: 1, inicio: time.Now()}
		return
	}
	f.contagem++
}

func (l *limitadorSenha) limpar(chave string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, chave)
}

// exigirSenhaAtual confere a senha atual do usuário autenticado. Devolve false
// (e já respondeu) quando a senha está errada ou o limite foi atingido.
func exigirSenhaAtual(c *gin.Context, ident *identidadeAuth, senha string) bool {
	chave := ident.Tipo + ":" + ident.ID.String()
	if seg := limitadorSenhaDoisFatores.segundosBloqueado(chave); seg > 0 {
		c.Header("Retry-After", strconv.Itoa(seg))
		utils.RespondWithErrorData(c, http.StatusTooManyRequests,
			"Muitas tentativas de senha incorreta. Aguarde alguns minutos e tente novamente.",
			nil, gin.H{"retry_after_segundos": seg})
		return false
	}
	if bcrypt.CompareHashAndPassword([]byte(ident.SenhaHash), []byte(senha)) != nil {
		limitadorSenhaDoisFatores.registrarFalha(chave)
		utils.RespondWithError(c, http.StatusUnauthorized, "senha incorreta", nil)
		return false
	}
	limitadorSenhaDoisFatores.limpar(chave)
	return true
}

// ── Login ───────────────────────────────────────────────────────────────────

// iniciarDoisFatoresNoLogin é chamado por Login depois de senha e status
// corretos e ANTES de emitir qualquer token (inclusive o restrito ao
// financeiro). Devolve true quando já respondeu à requisição (desafio enviado
// ou erro); false quando o usuário não usa 2FA e o login segue normalmente.
//
// Falha fechada: qualquer erro ao ler o estado do 2FA ou ao enviar o código
// termina em erro — nunca em token.
func iniciarDoisFatoresNoLogin(c *gin.Context, userID uuid.UUID, userType, nome string) bool {
	ident, ok := carregarIdentidade(c, userType, userID)
	if !ok {
		return true // carregarIdentidade já respondeu
	}
	if ident == nil || !ident.DoisFatoresAtivo {
		return false
	}
	if ident.Email == "" || !ident.EmailVerificado {
		utils.RespondWithError(c, http.StatusForbidden,
			"A verificação em duas etapas está ativa, mas não há um e-mail verificado para enviar o código. Entre em contato com o suporte.", nil)
		return true
	}
	svc := getDoisFatoresService(c)
	if svc == nil {
		return true
	}

	d, err := svc.Criar(c.Request.Context(), userID, userType, services.DoisFatoresFinalidadeLogin, ident.Email, nome, nil)
	if err != nil {
		var ag *services.ErrDoisFatoresAguardar
		if errors.As(err, &ag) && ag.DesafioID != uuid.Nil {
			// Já existe um código vivo enviado há instantes (ex.: duplo clique ou
			// voltou à tela de login). Reaproveita-o em vez de reenviar.
			c.JSON(http.StatusOK, gin.H{
				"requer_dois_fatores": true,
				"desafio_id":          ag.DesafioID,
				"metodo":              aggregates.MetodoDoisFatoresEmail,
				"email_mascarado":     maskEmail(ident.Email),
				"expira_em_segundos":  ag.ExpiraEmSegundos,
				"reenvio_em_segundos": ag.Segundos,
				"reutilizado":         true,
				"message":             fmt.Sprintf("Um código já foi enviado para %s há instantes. Use-o ou aguarde para pedir outro.", maskEmail(ident.Email)),
			})
			return true
		}
		respondDoisFatoresError(c, err, http.StatusUnauthorized)
		return true
	}

	resp := respostaDesafio(d)
	resp["requer_dois_fatores"] = true
	c.JSON(http.StatusOK, resp)
	return true
}

type verificarLoginDoisFatoresRequest struct {
	DesafioID string `json:"desafio_id" binding:"required"`
	Codigo    string `json:"codigo"     binding:"required"`
}

// VerificarLoginDoisFatores conclui o login: confere o código do desafio e, se
// correto, emite o mesmo JWT/resposta do login normal. O estado da conta é
// reavaliado aqui (pode ter mudado entre as duas etapas).
func VerificarLoginDoisFatores(c *gin.Context) {
	var req verificarLoginDoisFatoresRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	desafioID, err := uuid.Parse(strings.TrimSpace(req.DesafioID))
	if err != nil {
		utils.RespondWithError(c, http.StatusUnauthorized, services.ErrDoisFatoresDesafioInvalido.Error(), nil)
		return
	}
	svc := getDoisFatoresService(c)
	if svc == nil {
		return
	}

	d, err := svc.Verificar(c.Request.Context(), desafioID, req.Codigo, services.DoisFatoresFinalidadeLogin, nil)
	if err != nil {
		respondDoisFatoresError(c, err, http.StatusUnauthorized)
		return
	}

	ident, ok := carregarIdentidade(c, d.UserType, d.UserID)
	if !ok {
		return
	}
	if ident == nil {
		utils.RespondWithError(c, http.StatusUnauthorized, "usuário não encontrado", nil)
		return
	}
	msg, bloqueado := avaliarStatusLogin(ident.Tipo, ident.Status)
	if bloqueado {
		log.Printf("[INFO] [Login/2FA] Conta bloqueada por status — type: %s, identifier: %s, status: %s", ident.Tipo, ident.Identificador, ident.Status)
		utils.RespondWithError(c, http.StatusUnauthorized, msg, nil)
		return
	}
	responderLoginComSucesso(c, ident.ID, ident.Tipo, ident.Nome, ident.Identificador, ident.Role)
}

type reenviarLoginDoisFatoresRequest struct {
	DesafioID string `json:"desafio_id" binding:"required"`
}

// ReenviarLoginDoisFatores envia um código novo para um login pendente. Só vale
// dentro da janela desde a senha correta (DoisFatoresJanelaLogin); depois disso
// o usuário digita a senha de novo.
func ReenviarLoginDoisFatores(c *gin.Context) {
	var req reenviarLoginDoisFatoresRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	desafioID, err := uuid.Parse(strings.TrimSpace(req.DesafioID))
	if err != nil {
		utils.RespondWithError(c, http.StatusUnauthorized, services.ErrDoisFatoresDesafioInvalido.Error(), nil)
		return
	}
	svc := getDoisFatoresService(c)
	if svc == nil {
		return
	}

	origem, err := svc.ObterParaReenvio(c.Request.Context(), desafioID)
	if err != nil {
		respondDoisFatoresError(c, err, http.StatusUnauthorized)
		return
	}
	ident, ok := carregarIdentidade(c, origem.UserType, origem.UserID)
	if !ok {
		return
	}
	if ident == nil {
		utils.RespondWithError(c, http.StatusUnauthorized, services.ErrDoisFatoresDesafioInvalido.Error(), nil)
		return
	}
	if _, msg, bloqueado := func(tipo, status string) (bool, string, bool) { msg, blocked := avaliarStatusLogin(tipo, status); return false, msg, blocked }(ident.Tipo, ident.Status); bloqueado {
		utils.RespondWithError(c, http.StatusUnauthorized, msg, nil)
		return
	}
	if ident.Email == "" || !ident.EmailVerificado {
		utils.RespondWithError(c, http.StatusForbidden,
			"A verificação em duas etapas está ativa, mas não há um e-mail verificado para enviar o código. Entre em contato com o suporte.", nil)
		return
	}

	novo, err := svc.Criar(c.Request.Context(), ident.ID, ident.Tipo, services.DoisFatoresFinalidadeLogin, ident.Email, ident.Nome, &origem.IniciadoEm)
	if err != nil {
		respondDoisFatoresError(c, err, http.StatusUnauthorized)
		return
	}
	resp := respostaDesafio(novo)
	resp["requer_dois_fatores"] = true
	c.JSON(http.StatusOK, resp)
}

// ── Gestão autenticada ──────────────────────────────────────────────────────

func usuarioAutenticado(c *gin.Context) (uuid.UUID, string, bool) {
	userID, okID := middleware.GetUserID(c)
	userType, okType := middleware.GetUserType(c)
	if !okID || !okType || !(userType == "admin" || userType == "academia" || userType == "estudante") {
		utils.RespondWithUnauthorizedError(c)
		return uuid.Nil, "", false
	}
	return userID, userType, true
}

// ObterDoisFatores devolve o estado do 2FA do usuário autenticado.
func ObterDoisFatores(c *gin.Context) {
	userID, userType, ok := usuarioAutenticado(c)
	if !ok {
		return
	}
	ident, ok := carregarIdentidade(c, userType, userID)
	if !ok {
		return
	}
	if ident == nil {
		utils.RespondWithNotFoundError(c, "usuário")
		return
	}
	resp := gin.H{
		"ativo":            ident.DoisFatoresAtivo,
		"metodo":           aggregates.MetodoDoisFatoresEmail,
		"email_verificado": ident.EmailVerificado,
		"pode_ativar":      !ident.DoisFatoresAtivo && ident.Email != "" && ident.EmailVerificado,
	}
	if ident.Email != "" {
		resp["email_mascarado"] = maskEmail(ident.Email)
	} else {
		resp["email_mascarado"] = nil
	}
	c.JSON(http.StatusOK, resp)
}

// SolicitarAtivacaoDoisFatores envia o código de ativação (exige e-mail verificado).
func SolicitarAtivacaoDoisFatores(c *gin.Context) {
	userID, userType, ok := usuarioAutenticado(c)
	if !ok {
		return
	}
	ident, ok := carregarIdentidade(c, userType, userID)
	if !ok {
		return
	}
	if ident == nil {
		utils.RespondWithNotFoundError(c, "usuário")
		return
	}
	if ident.DoisFatoresAtivo {
		utils.RespondWithConflictError(c, "A verificação em duas etapas já está ativa.")
		return
	}
	if ident.Email == "" || !ident.EmailVerificado {
		utils.RespondWithError(c, http.StatusBadRequest, aggregates.ErrDoisFatoresEmailNaoVerificado.Error(), nil)
		return
	}
	svc := getDoisFatoresService(c)
	if svc == nil {
		return
	}
	d, err := svc.Criar(c.Request.Context(), userID, userType, services.DoisFatoresFinalidadeAtivacao, ident.Email, ident.Nome, nil)
	if err != nil {
		respondDoisFatoresError(c, err, http.StatusBadRequest)
		return
	}
	c.JSON(http.StatusOK, respostaDesafio(d))
}

type confirmarDoisFatoresRequest struct {
	DesafioID string `json:"desafio_id" binding:"required"`
	Codigo    string `json:"codigo"     binding:"required"`
	Senha     string `json:"senha"`
}

// ConfirmarAtivacaoDoisFatores confere o código e ativa o 2FA (evento no ledger).
func ConfirmarAtivacaoDoisFatores(c *gin.Context) {
	confirmarMudancaDoisFatores(c, true)
}

type solicitarDesativacaoRequest struct {
	Senha string `json:"senha" binding:"required"`
}

// SolicitarDesativacaoDoisFatores exige a senha atual e envia o código de desativação.
func SolicitarDesativacaoDoisFatores(c *gin.Context) {
	var req solicitarDesativacaoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("senha é obrigatória"))
		return
	}
	userID, userType, ok := usuarioAutenticado(c)
	if !ok {
		return
	}
	ident, ok := carregarIdentidade(c, userType, userID)
	if !ok {
		return
	}
	if ident == nil {
		utils.RespondWithNotFoundError(c, "usuário")
		return
	}
	if !ident.DoisFatoresAtivo {
		utils.RespondWithConflictError(c, "A verificação em duas etapas não está ativa.")
		return
	}
	if !exigirSenhaAtual(c, ident, req.Senha) {
		return
	}
	if ident.Email == "" || !ident.EmailVerificado {
		utils.RespondWithError(c, http.StatusForbidden,
			"Não há um e-mail verificado para enviar o código. Entre em contato com o suporte.", nil)
		return
	}
	svc := getDoisFatoresService(c)
	if svc == nil {
		return
	}
	d, err := svc.Criar(c.Request.Context(), userID, userType, services.DoisFatoresFinalidadeDesativacao, ident.Email, ident.Nome, nil)
	if err != nil {
		respondDoisFatoresError(c, err, http.StatusBadRequest)
		return
	}
	c.JSON(http.StatusOK, respostaDesafio(d))
}

// ConfirmarDesativacaoDoisFatores confere senha + código e desativa o 2FA.
func ConfirmarDesativacaoDoisFatores(c *gin.Context) {
	confirmarMudancaDoisFatores(c, false)
}

// confirmarMudancaDoisFatores concentra a confirmação de ativação/desativação.
//
// Ordem deliberada na desativação: a senha é conferida ANTES de consumir o
// código — uma senha errada não deve queimar o código nem as suas tentativas.
// O código é consumido ANTES de gravar o evento: se a gravação falhar, o usuário
// apenas pede outro código (nunca fica com um código reutilizável).
func confirmarMudancaDoisFatores(c *gin.Context, ativar bool) {
	var req confirmarDoisFatoresRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	userID, userType, ok := usuarioAutenticado(c)
	if !ok {
		return
	}
	desafioID, err := uuid.Parse(strings.TrimSpace(req.DesafioID))
	if err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, services.ErrDoisFatoresDesafioInvalido.Error(), nil)
		return
	}

	finalidade := services.DoisFatoresFinalidadeAtivacao
	if !ativar {
		finalidade = services.DoisFatoresFinalidadeDesativacao
		if strings.TrimSpace(req.Senha) == "" {
			utils.RespondWithValidationError(c, fmt.Errorf("senha é obrigatória"))
			return
		}
		ident, ok := carregarIdentidade(c, userType, userID)
		if !ok {
			return
		}
		if ident == nil {
			utils.RespondWithNotFoundError(c, "usuário")
			return
		}
		if !exigirSenhaAtual(c, ident, req.Senha) {
			return
		}
	}

	svc := getDoisFatoresService(c)
	if svc == nil {
		return
	}
	if _, err := svc.Verificar(c.Request.Context(), desafioID, req.Codigo, finalidade,
		&services.UsuarioDoisFatores{ID: userID, Tipo: userType}); err != nil {
		respondDoisFatoresError(c, err, http.StatusBadRequest)
		return
	}

	if err := aplicarEstadoDoisFatores(c, userID, userType, ativar); err != nil {
		switch {
		case errors.Is(err, aggregates.ErrDoisFatoresJaAtivo), errors.Is(err, aggregates.ErrDoisFatoresNaoAtivo):
			utils.RespondWithConflictError(c, err.Error())
		case errors.Is(err, aggregates.ErrDoisFatoresEmailNaoVerificado):
			utils.RespondWithError(c, http.StatusBadRequest, err.Error(), err)
		default:
			utils.RespondWithInternalError(c, err)
		}
		return
	}

	msg := "Verificação em duas etapas ativada com sucesso."
	if !ativar {
		msg = "Verificação em duas etapas desativada com sucesso."
	}
	c.JSON(http.StatusOK, gin.H{"message": msg, "dois_fatores_ativo": ativar})
}

// doisFatoresAggregate é o contrato comum dos três agregados.
type doisFatoresAggregate interface {
	AtivarDoisFatores() error
	DesativarDoisFatores() error
}

// aplicarEstadoDoisFatores carrega o agregado, aplica o comando e grava o
// evento (DoisFatoresAtivados/Desativados) com auditoria.
func aplicarEstadoDoisFatores(c *gin.Context, userID uuid.UUID, userType string, ativar bool) error {
	aggType := map[string]string{"admin": "Admin", "academia": "Academia", "estudante": "Estudante"}[userType]
	if aggType == "" {
		return fmt.Errorf("tipo de usuário inválido: %s", userType)
	}
	repository := getRepository(c)
	if repository == nil {
		return fmt.Errorf("repositório indisponível")
	}
	agg, err := repository.Load(userID, aggType)
	if err != nil {
		return err
	}
	df, ok := agg.(doisFatoresAggregate)
	if !ok {
		return fmt.Errorf("tipo de aggregate inesperado")
	}
	if ativar {
		err = df.AtivarDoisFatores()
	} else {
		err = df.DesativarDoisFatores()
	}
	if err != nil {
		return err
	}
	audit := db.AuditContext{UserID: userID.String(), UserType: userType, IP: c.ClientIP()}
	return repository.SaveWithAudit(agg, audit)
}
