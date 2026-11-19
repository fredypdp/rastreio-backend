package autenticacao

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"spuri/internal/db"
	"spuri/internal/handlers/testsupport"
	"spuri/internal/middleware"
	"spuri/internal/projections"
	"spuri/internal/services"
)

// Tarefa 122 — fluxos HTTP da verificação em duas etapas, de ponta a ponta,
// contra PostgreSQL real (migration 134 aplicada pelo integrationFinanceClient)
// e com o caminho real de event sourcing (agregado → ledger → projeção).
// Cada teste cria os seus próprios usuários (uuid/código aleatórios): repetíveis
// no mesmo banco.

// ── Remetente falso: captura os códigos que iriam por e-mail ────────────────

type enviadoDF struct{ Destinatario, Finalidade, Codigo string }

type capturaDF struct {
	mu       sync.Mutex
	enviados []enviadoDF
	falhar   error
}

func (c *capturaDF) EnviarCodigoDoisFatores(destinatario, _ string, finalidade, codigo string, _ int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.falhar != nil {
		return c.falhar
	}
	c.enviados = append(c.enviados, enviadoDF{destinatario, finalidade, codigo})
	return nil
}

func (c *capturaDF) ultimo(t *testing.T) enviadoDF {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.enviados) == 0 {
		t.Fatal("nenhum código foi enviado por e-mail")
	}
	return c.enviados[len(c.enviados)-1]
}

func (c *capturaDF) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.enviados)
}

var regexSeisDigitos = regexp.MustCompile(`^[0-9]{6}$`)

func instalarCapturaDF(t *testing.T) *capturaDF {
	t.Helper()
	t.Setenv("JWT_SECRET", "segredo-de-teste-dois-fatores")
	captura := &capturaDF{}
	anterior := doisFatoresSenderFactory
	doisFatoresSenderFactory = func(*gin.Context) services.DoisFatoresSender { return captura }
	t.Cleanup(func() { doisFatoresSenderFactory = anterior })

	// Limite de senha (em memória) começa limpo em cada teste.
	limitadorSenhaDoisFatores.mu.Lock()
	limitadorSenhaDoisFatores.m = map[string]*falhasSenha{}
	limitadorSenhaDoisFatores.mu.Unlock()
	return captura
}

func routerDoisFatores(client *db.Client) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	repository := db.NewAggregateRepository(client)
	router.Use(func(c *gin.Context) {
		c.Set("dbClient", client)
		c.Set("repository", repository)
		c.Next()
	})
	router.POST("/login", Login)
	router.POST("/login/dois-fatores/verificar", VerificarLoginDoisFatores)
	router.POST("/login/dois-fatores/reenviar", ReenviarLoginDoisFatores)
	protected := router.Group("/")
	protected.Use(middleware.AuthMiddleware())
	{
		protected.PUT("/me/email", AtualizarMeuEmail)
		protected.GET("/dois-fatores", ObterDoisFatores)
		protected.POST("/dois-fatores/ativacao/solicitar", SolicitarAtivacaoDoisFatores)
		protected.POST("/dois-fatores/ativacao/confirmar", ConfirmarAtivacaoDoisFatores)
		protected.POST("/dois-fatores/desativacao/solicitar", SolicitarDesativacaoDoisFatores)
		protected.POST("/dois-fatores/desativacao/confirmar", ConfirmarDesativacaoDoisFatores)
	}
	return router
}

func chamar(router *gin.Engine, method, path, token string, body map[string]any) (*httptest.ResponseRecorder, map[string]any) {
	var buf bytes.Buffer
	if body != nil {
		raw, _ := json.Marshal(body)
		buf.Write(raw)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	router.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func exigirStatus(t *testing.T, rec *httptest.ResponseRecorder, esperado int) {
	t.Helper()
	if rec.Code != esperado {
		t.Fatalf("esperava status %d, obteve %d: %s", esperado, rec.Code, rec.Body.String())
	}
}

// emailNovoValido gera um e-mail aceito por utils.ValidateEmail (sem hífens).
func emailNovoValido() string {
	return "novo" + strings.ReplaceAll(uuid.NewString(), "-", "") + "@example.com"
}

func envelhecerDesafios(t *testing.T, client *db.Client, userID uuid.UUID) {
	t.Helper()
	if _, err := client.DB().Exec(`UPDATE dois_fatores_desafios SET created_at = created_at - INTERVAL '2 minutes' WHERE user_id = $1`, userID); err != nil {
		t.Fatal(err)
	}
}

// ── Seeds ───────────────────────────────────────────────────────────────────

func hashRapido(t *testing.T, senha string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}

// seedEstudanteDoisFatores grava direto na projeção (como os demais testes de
// handlers de leitura/login) um estudante com e-mail, senha e 2FA controlados.
func seedEstudanteDoisFatores(t *testing.T, client *db.Client, senha, status string, emailVerificado, doisFatores bool) (id uuid.UUID, codigo, email string) {
	t.Helper()
	academia := "DF" + uuid.NewString()[:6]
	testsupport.SeedAcademiaParaCategoriaServico(t, client, academia)
	id = uuid.New()
	codigo = "E" + testsupport.GeraDigitos(6)
	email = "estudante-" + uuid.NewString() + "@example.test"
	_, err := client.DB().Exec(`
		INSERT INTO projection_estudantes (id, nome, codigo_estudante, senha_hash, telefone, codigo_academia, status, genero, data_nascimento,
		                                   ano_escolar_fundamental, email, email_verificado, dois_fatores_ativo, created_at, updated_at)
		VALUES ($1, 'Estudante 2FA', $2, $3, $4, $5, $6, 'feminino', '2012-05-10', '7_ano_fundamental', $7, $8, $9, now(), now())`,
		id, codigo, hashRapido(t, senha), "9"+testsupport.GeraDigitos(8), academia, status, email, emailVerificado, doisFatores)
	if err != nil {
		t.Fatal(err)
	}
	return id, codigo, email
}

func seedAcademiaDoisFatores(t *testing.T, client *db.Client, senha string, doisFatores bool) (id uuid.UUID, codigo, email string) {
	t.Helper()
	codigo = "DFA" + uuid.NewString()[:6]
	id = testsupport.SeedAcademiaParaCategoriaServico(t, client, codigo)
	email = "academia-" + uuid.NewString() + "@example.test"
	if _, err := client.DB().Exec(
		`UPDATE projection_academias SET senha_hash = $2, email = $3, email_verificado = TRUE, dois_fatores_ativo = $4 WHERE id = $1`,
		id, hashRapido(t, senha), email, doisFatores); err != nil {
		t.Fatal(err)
	}
	return id, codigo, email
}

func loginAdminDF(t *testing.T, router *gin.Engine, email, senha string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	return chamar(router, http.MethodPost, "/login", "", map[string]any{"usuario": email, "senha": senha})
}

func ativarDoisFatoresViaHTTP(t *testing.T, router *gin.Engine, captura *capturaDF, token string) {
	t.Helper()
	rec, body := chamar(router, http.MethodPost, "/dois-fatores/ativacao/solicitar", token, nil)
	exigirStatus(t, rec, http.StatusOK)
	rec, _ = chamar(router, http.MethodPost, "/dois-fatores/ativacao/confirmar", token,
		map[string]any{"desafio_id": body["desafio_id"], "codigo": captura.ultimo(t).Codigo})
	exigirStatus(t, rec, http.StatusOK)
}

// ── Fluxo completo (admin, caminho real de eventos) ─────────────────────────

func TestIntegrationDoisFatoresFluxoCompletoAdmin(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	captura := instalarCapturaDF(t)
	router := routerDoisFatores(client)
	const senha = "SenhaForte#123"
	userID, email := seedAdminParaRecuperacao(t, client, senha, true)

	// 1) Sem 2FA o login é exatamente o de sempre (token direto).
	rec, body := loginAdminDF(t, router, email, senha)
	exigirStatus(t, rec, http.StatusOK)
	token, _ := body["token"].(string)
	if token == "" || body["requer_dois_fatores"] != nil {
		t.Fatalf("login sem 2FA deveria devolver só o token: %v", body)
	}
	if captura.total() != 0 {
		t.Fatal("login sem 2FA não pode enviar código")
	}

	// 2) Estado inicial.
	rec, body = chamar(router, http.MethodGet, "/dois-fatores", token, nil)
	exigirStatus(t, rec, http.StatusOK)
	if body["ativo"] != false || body["pode_ativar"] != true || body["email_verificado"] != true {
		t.Fatalf("estado inicial inesperado: %v", body)
	}

	// 3) Ativação: solicitar → código errado → código certo.
	rec, body = chamar(router, http.MethodPost, "/dois-fatores/ativacao/solicitar", token, nil)
	exigirStatus(t, rec, http.StatusOK)
	desafioAtivacao := body["desafio_id"]
	if body["email_mascarado"] == email || body["expira_em_segundos"] != float64(600) {
		t.Fatalf("resposta de solicitação inesperada: %v", body)
	}
	envio := captura.ultimo(t)
	if envio.Destinatario != email || envio.Finalidade != services.DoisFatoresFinalidadeAtivacao || !regexSeisDigitos.MatchString(envio.Codigo) {
		t.Fatalf("envio inesperado: %+v", envio)
	}
	codigoErrado := "000000"
	if envio.Codigo == codigoErrado {
		codigoErrado = "000001"
	}
	rec, body = chamar(router, http.MethodPost, "/dois-fatores/ativacao/confirmar", token,
		map[string]any{"desafio_id": desafioAtivacao, "codigo": codigoErrado})
	exigirStatus(t, rec, http.StatusBadRequest)
	if body["tentativas_restantes"] != float64(4) {
		t.Fatalf("deveria informar 4 tentativas restantes: %v", body)
	}
	rec, body = chamar(router, http.MethodPost, "/dois-fatores/ativacao/confirmar", token,
		map[string]any{"desafio_id": desafioAtivacao, "codigo": envio.Codigo})
	exigirStatus(t, rec, http.StatusOK)
	if body["dois_fatores_ativo"] != true {
		t.Fatalf("resposta de confirmação inesperada: %v", body)
	}
	applyAdminEvents(t, client, userID)

	// Reusar o mesmo código/desafio: recusado (uso único).
	rec, _ = chamar(router, http.MethodPost, "/dois-fatores/ativacao/confirmar", token,
		map[string]any{"desafio_id": desafioAtivacao, "codigo": envio.Codigo})
	exigirStatus(t, rec, http.StatusBadRequest)

	rec, body = chamar(router, http.MethodGet, "/dois-fatores", token, nil)
	exigirStatus(t, rec, http.StatusOK)
	if body["ativo"] != true || body["pode_ativar"] != false {
		t.Fatalf("estado após ativar: %v", body)
	}

	// 4) Trocar o e-mail com 2FA ativo é bloqueado (regra de segurança).
	rec, body = chamar(router, http.MethodPut, "/me/email", token, map[string]any{"email": emailNovoValido()})
	exigirStatus(t, rec, http.StatusBadRequest)
	if msg, _ := body["message"].(string); !regexp.MustCompile(`duas etapas`).MatchString(msg) {
		t.Fatalf("a mensagem deveria explicar o bloqueio: %v", body)
	}

	// 5) Login agora é em duas etapas: nenhum token na primeira resposta.
	rec, body = loginAdminDF(t, router, email, senha)
	exigirStatus(t, rec, http.StatusOK)
	if body["token"] != nil || body["requer_dois_fatores"] != true || body["desafio_id"] == nil {
		t.Fatalf("login com 2FA não pode emitir token: %v", body)
	}
	desafioLogin := body["desafio_id"]
	envioLogin := captura.ultimo(t)
	if envioLogin.Finalidade != services.DoisFatoresFinalidadeLogin || envioLogin.Destinatario != email {
		t.Fatalf("envio de login inesperado: %+v", envioLogin)
	}
	errado := "000000"
	if envioLogin.Codigo == errado {
		errado = "000001"
	}
	rec, _ = chamar(router, http.MethodPost, "/login/dois-fatores/verificar", "", map[string]any{"desafio_id": desafioLogin, "codigo": errado})
	exigirStatus(t, rec, http.StatusUnauthorized)
	rec, body = chamar(router, http.MethodPost, "/login/dois-fatores/verificar", "", map[string]any{"desafio_id": desafioLogin, "codigo": envioLogin.Codigo})
	exigirStatus(t, rec, http.StatusOK)
	tokenLogin, _ := body["token"].(string)
	if tokenLogin == "" || body["type"] != "admin" || body["email"] != email || body["role"] != "adm" {
		t.Fatalf("a segunda etapa deve devolver a mesma resposta do login normal: %v", body)
	}
	rec, _ = chamar(router, http.MethodGet, "/dois-fatores", tokenLogin, nil)
	exigirStatus(t, rec, http.StatusOK)
	// O código do login também é de uso único.
	rec, _ = chamar(router, http.MethodPost, "/login/dois-fatores/verificar", "", map[string]any{"desafio_id": desafioLogin, "codigo": envioLogin.Codigo})
	exigirStatus(t, rec, http.StatusUnauthorized)

	// 6) Desativação exige a senha atual E o código.
	semSenha, _ := chamar(router, http.MethodPost, "/dois-fatores/desativacao/solicitar", tokenLogin, map[string]any{})
	exigirStatus(t, semSenha, http.StatusBadRequest)
	enviosAntes := captura.total()
	rec, _ = chamar(router, http.MethodPost, "/dois-fatores/desativacao/solicitar", tokenLogin, map[string]any{"senha": "senha-errada"})
	exigirStatus(t, rec, http.StatusUnauthorized)
	if captura.total() != enviosAntes {
		t.Fatal("senha errada não pode disparar o envio do código")
	}
	rec, body = chamar(router, http.MethodPost, "/dois-fatores/desativacao/solicitar", tokenLogin, map[string]any{"senha": senha})
	exigirStatus(t, rec, http.StatusOK)
	desafioDes := body["desafio_id"]
	envioDes := captura.ultimo(t)
	if envioDes.Finalidade != services.DoisFatoresFinalidadeDesativacao {
		t.Fatalf("envio de desativação inesperado: %+v", envioDes)
	}
	// Código certo, mas sem senha / com senha errada: recusado — e o código NÃO é queimado.
	rec, _ = chamar(router, http.MethodPost, "/dois-fatores/desativacao/confirmar", tokenLogin, map[string]any{"desafio_id": desafioDes, "codigo": envioDes.Codigo})
	exigirStatus(t, rec, http.StatusBadRequest)
	rec, _ = chamar(router, http.MethodPost, "/dois-fatores/desativacao/confirmar", tokenLogin,
		map[string]any{"desafio_id": desafioDes, "codigo": envioDes.Codigo, "senha": "senha-errada"})
	exigirStatus(t, rec, http.StatusUnauthorized)
	rec, body = chamar(router, http.MethodPost, "/dois-fatores/desativacao/confirmar", tokenLogin,
		map[string]any{"desafio_id": desafioDes, "codigo": envioDes.Codigo, "senha": senha})
	exigirStatus(t, rec, http.StatusOK)
	if body["dois_fatores_ativo"] != false {
		t.Fatalf("resposta de desativação inesperada: %v", body)
	}
	applyAdminEvents(t, client, userID)

	// 7) Desativado: login volta a ser direto e o e-mail pode ser trocado.
	rec, body = loginAdminDF(t, router, email, senha)
	exigirStatus(t, rec, http.StatusOK)
	if body["token"] == nil || body["requer_dois_fatores"] != nil {
		t.Fatalf("após desativar o login deveria ser direto: %v", body)
	}
	rec, _ = chamar(router, http.MethodPut, "/me/email", body["token"].(string), map[string]any{"email": emailNovoValido()})
	exigirStatus(t, rec, http.StatusOK)
}

// ── Regras de ativação/desativação ──────────────────────────────────────────

func TestIntegrationDoisFatoresAtivacaoExigeEmailVerificado(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	captura := instalarCapturaDF(t)
	router := routerDoisFatores(client)
	const senha = "SenhaForte#123"
	_, email := seedAdminParaRecuperacao(t, client, senha, false)
	_, body := loginAdminDF(t, router, email, senha)
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("login do admin sem e-mail verificado deveria funcionar: %v", body)
	}

	rec, body := chamar(router, http.MethodGet, "/dois-fatores", token, nil)
	exigirStatus(t, rec, http.StatusOK)
	if body["pode_ativar"] != false || body["email_verificado"] != false {
		t.Fatalf("estado inesperado: %v", body)
	}
	rec, _ = chamar(router, http.MethodPost, "/dois-fatores/ativacao/solicitar", token, nil)
	exigirStatus(t, rec, http.StatusBadRequest)
	if captura.total() != 0 {
		t.Fatal("sem e-mail verificado nenhum código pode ser enviado")
	}
}

func TestIntegrationDoisFatoresConflitosDeEstado(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	captura := instalarCapturaDF(t)
	router := routerDoisFatores(client)
	const senha = "SenhaForte#123"
	userID, email := seedAdminParaRecuperacao(t, client, senha, true)
	_, body := loginAdminDF(t, router, email, senha)
	token := body["token"].(string)

	// Desativar sem estar ativo → 409.
	rec, _ := chamar(router, http.MethodPost, "/dois-fatores/desativacao/solicitar", token, map[string]any{"senha": senha})
	exigirStatus(t, rec, http.StatusConflict)

	ativarDoisFatoresViaHTTP(t, router, captura, token)
	applyAdminEvents(t, client, userID)

	// Ativar de novo → 409.
	rec, _ = chamar(router, http.MethodPost, "/dois-fatores/ativacao/solicitar", token, nil)
	exigirStatus(t, rec, http.StatusConflict)
}

func TestIntegrationDoisFatoresConfirmacaoDeOutroUsuarioRecusada(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	captura := instalarCapturaDF(t)
	router := routerDoisFatores(client)
	const senha = "SenhaForte#123"
	idA, emailA := seedAdminParaRecuperacao(t, client, senha, true)
	_, emailB := seedAdminParaRecuperacao(t, client, senha, true)
	_, corpoA := loginAdminDF(t, router, emailA, senha)
	_, corpoB := loginAdminDF(t, router, emailB, senha)

	rec, body := chamar(router, http.MethodPost, "/dois-fatores/ativacao/solicitar", corpoA["token"].(string), nil)
	exigirStatus(t, rec, http.StatusOK)
	codigoA := captura.ultimo(t).Codigo

	// B tenta concluir a ativação com o desafio e o código de A.
	rec, _ = chamar(router, http.MethodPost, "/dois-fatores/ativacao/confirmar", corpoB["token"].(string),
		map[string]any{"desafio_id": body["desafio_id"], "codigo": codigoA})
	exigirStatus(t, rec, http.StatusBadRequest)

	// A continua podendo concluir (o desafio dele não foi queimado por B).
	rec, _ = chamar(router, http.MethodPost, "/dois-fatores/ativacao/confirmar", corpoA["token"].(string),
		map[string]any{"desafio_id": body["desafio_id"], "codigo": codigoA})
	exigirStatus(t, rec, http.StatusOK)
	applyAdminEvents(t, client, idA)
}

func TestIntegrationDoisFatoresDesativacaoLimitaTentativasDeSenha(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	captura := instalarCapturaDF(t)
	router := routerDoisFatores(client)
	const senha = "SenhaForte#123"
	userID, email := seedAdminParaRecuperacao(t, client, senha, true)
	_, body := loginAdminDF(t, router, email, senha)
	token := body["token"].(string)
	ativarDoisFatoresViaHTTP(t, router, captura, token)
	applyAdminEvents(t, client, userID)

	for i := 0; i < maxFalhasSenhaDoisFatores; i++ {
		rec, _ := chamar(router, http.MethodPost, "/dois-fatores/desativacao/solicitar", token, map[string]any{"senha": "errada"})
		exigirStatus(t, rec, http.StatusUnauthorized)
	}
	// Atingido o limite, nem a senha CORRETA é aceita até a janela passar.
	rec, body := chamar(router, http.MethodPost, "/dois-fatores/desativacao/solicitar", token, map[string]any{"senha": senha})
	exigirStatus(t, rec, http.StatusTooManyRequests)
	if body["retry_after_segundos"] == nil || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("o 429 deveria informar quando tentar de novo: %v", body)
	}
}

// ── Login: estudante, academia e casos de segurança ─────────────────────────

func TestIntegrationDoisFatoresLoginEstudanteInativoSoRecebeTokenDepoisDoCodigo(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	captura := instalarCapturaDF(t)
	router := routerDoisFatores(client)
	const senha = "SenhaAluno#123"
	_, codigo, email := seedEstudanteDoisFatores(t, client, senha, "inativo", true, true)

	rec, body := loginAdminDF(t, router, codigo, senha)
	exigirStatus(t, rec, http.StatusOK)
	if body["token"] != nil || body["requer_dois_fatores"] != true {
		t.Fatalf("estudante inativo com 2FA não pode receber nem o token restrito antes do código: %v", body)
	}
	envio := captura.ultimo(t)
	if envio.Destinatario != email {
		t.Fatalf("o código deve ir para o e-mail do estudante: %+v", envio)
	}

	rec, body = chamar(router, http.MethodPost, "/login/dois-fatores/verificar", "", map[string]any{"desafio_id": body["desafio_id"], "codigo": envio.Codigo})
	exigirStatus(t, rec, http.StatusOK)
	token, _ := body["token"].(string)
	if token == "" || body["type"] != "estudante" || body["codigo"] != codigo {
		t.Fatalf("resposta inesperada: %v", body)
	}
	// SEC-12 (Tarefa 127): já não existe token restrito. O estudante desvinculado
	// recebe um token normal e continua a gerir o seu 2FA (rota de conta).
	rec, _ = chamar(router, http.MethodGet, "/dois-fatores", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("o estudante desvinculado deve poder consultar /dois-fatores: %d %s", rec.Code, rec.Body.String())
	}
}

func TestIntegrationDoisFatoresLoginAcademia(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	captura := instalarCapturaDF(t)
	router := routerDoisFatores(client)
	const senha = "SenhaAcademia#123"
	_, codigo, _ := seedAcademiaDoisFatores(t, client, senha, true)

	rec, body := loginAdminDF(t, router, codigo, senha)
	exigirStatus(t, rec, http.StatusOK)
	if body["token"] != nil || body["requer_dois_fatores"] != true {
		t.Fatalf("academia com 2FA não pode receber token na primeira etapa: %v", body)
	}
	rec, body = chamar(router, http.MethodPost, "/login/dois-fatores/verificar", "", map[string]any{"desafio_id": body["desafio_id"], "codigo": captura.ultimo(t).Codigo})
	exigirStatus(t, rec, http.StatusOK)
	if body["token"] == nil || body["type"] != "academia" || body["codigo"] != codigo {
		t.Fatalf("resposta inesperada: %v", body)
	}
}

// A conta pode ser desativada entre a senha e o código: a segunda etapa
// reavalia o status e não emite token.
func TestIntegrationDoisFatoresContaDesativadaEntreAsEtapasNaoRecebeToken(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	captura := instalarCapturaDF(t)
	router := routerDoisFatores(client)
	const senha = "SenhaAcademia#123"
	id, codigo, _ := seedAcademiaDoisFatores(t, client, senha, true)

	rec, body := loginAdminDF(t, router, codigo, senha)
	exigirStatus(t, rec, http.StatusOK)
	if _, err := client.DB().Exec(`UPDATE projection_academias SET status = 'inativo' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	rec, body = chamar(router, http.MethodPost, "/login/dois-fatores/verificar", "", map[string]any{"desafio_id": body["desafio_id"], "codigo": captura.ultimo(t).Codigo})
	exigirStatus(t, rec, http.StatusUnauthorized)
	if body["token"] != nil {
		t.Fatalf("conta desativada não pode receber token: %v", body)
	}
}

func TestIntegrationDoisFatoresSenhaErradaNaoEnviaCodigo(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	captura := instalarCapturaDF(t)
	router := routerDoisFatores(client)
	_, codigo, _ := seedEstudanteDoisFatores(t, client, "SenhaAluno#123", "ativo", true, true)

	rec, body := loginAdminDF(t, router, codigo, "senha-errada")
	exigirStatus(t, rec, http.StatusUnauthorized)
	if body["token"] != nil || body["desafio_id"] != nil || captura.total() != 0 {
		t.Fatalf("senha errada não pode gerar desafio nem e-mail: %v", body)
	}
}

func TestIntegrationDoisFatoresLoginSemEmailVerificadoFalhaFechado(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	captura := instalarCapturaDF(t)
	router := routerDoisFatores(client)
	const senha = "SenhaAluno#123"
	// 2FA ativo mas o e-mail não está verificado (estado que o sistema impede de
	// criar, mas que nunca pode virar um login sem segundo fator).
	_, codigo, _ := seedEstudanteDoisFatores(t, client, senha, "ativo", false, true)

	rec, body := loginAdminDF(t, router, codigo, senha)
	exigirStatus(t, rec, http.StatusForbidden)
	if body["token"] != nil || captura.total() != 0 {
		t.Fatalf("sem e-mail verificado não pode haver token: %v", body)
	}
}

func TestIntegrationDoisFatoresFalhaDeEnvioNoLoginNaoEmiteToken(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	captura := instalarCapturaDF(t)
	captura.falhar = errors.New("provedor de e-mail fora do ar")
	router := routerDoisFatores(client)
	const senha = "SenhaAluno#123"
	id, codigo, _ := seedEstudanteDoisFatores(t, client, senha, "ativo", true, true)

	rec, body := loginAdminDF(t, router, codigo, senha)
	exigirStatus(t, rec, http.StatusServiceUnavailable)
	if body["token"] != nil || body["desafio_id"] != nil {
		t.Fatalf("falha no envio não pode emitir token nem desafio: %v", body)
	}
	var n int
	if err := client.DB().Get(&n, `SELECT COUNT(*) FROM dois_fatores_desafios WHERE user_id = $1`, id); err != nil || n != 0 {
		t.Fatalf("não pode sobrar desafio de um código nunca entregue: %d (%v)", n, err)
	}

	// Recuperado o provedor, o login funciona de imediato.
	captura.falhar = nil
	rec, body = loginAdminDF(t, router, codigo, senha)
	exigirStatus(t, rec, http.StatusOK)
	if body["requer_dois_fatores"] != true {
		t.Fatalf("o login deveria pedir o código: %v", body)
	}
}

func TestIntegrationDoisFatoresLoginRepetidoReaproveitaEReenvioInvalidaOAnterior(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	captura := instalarCapturaDF(t)
	router := routerDoisFatores(client)
	const senha = "SenhaAluno#123"
	id, codigo, _ := seedEstudanteDoisFatores(t, client, senha, "ativo", true, true)

	rec, primeiro := loginAdminDF(t, router, codigo, senha)
	exigirStatus(t, rec, http.StatusOK)
	codigo1 := captura.ultimo(t).Codigo

	// Segundo login imediato (duplo clique / voltou à tela): reaproveita o desafio vivo.
	rec, segundo := loginAdminDF(t, router, codigo, senha)
	exigirStatus(t, rec, http.StatusOK)
	if segundo["desafio_id"] != primeiro["desafio_id"] || segundo["reutilizado"] != true || captura.total() != 1 {
		t.Fatalf("o login repetido deveria reaproveitar o desafio sem novo e-mail: %v (envios=%d)", segundo, captura.total())
	}

	// Reenvio dentro do intervalo mínimo: 429 com o tempo de espera.
	rec, body := chamar(router, http.MethodPost, "/login/dois-fatores/reenviar", "", map[string]any{"desafio_id": primeiro["desafio_id"]})
	exigirStatus(t, rec, http.StatusTooManyRequests)
	if body["retry_after_segundos"] == nil {
		t.Fatalf("o 429 deveria informar a espera: %v", body)
	}

	// Passado o intervalo: novo desafio, e o código antigo deixa de valer.
	envelhecerDesafios(t, client, id)
	rec, novo := chamar(router, http.MethodPost, "/login/dois-fatores/reenviar", "", map[string]any{"desafio_id": primeiro["desafio_id"]})
	exigirStatus(t, rec, http.StatusOK)
	if novo["desafio_id"] == primeiro["desafio_id"] {
		t.Fatal("o reenvio deve criar um desafio novo")
	}
	codigo2 := captura.ultimo(t).Codigo
	rec, _ = chamar(router, http.MethodPost, "/login/dois-fatores/verificar", "", map[string]any{"desafio_id": primeiro["desafio_id"], "codigo": codigo1})
	exigirStatus(t, rec, http.StatusUnauthorized)
	rec, body = chamar(router, http.MethodPost, "/login/dois-fatores/verificar", "", map[string]any{"desafio_id": novo["desafio_id"], "codigo": codigo2})
	exigirStatus(t, rec, http.StatusOK)
	if body["token"] == nil {
		t.Fatalf("o código novo deveria concluir o login: %v", body)
	}
}

func TestIntegrationDoisFatoresReenvioForaDaJanelaExigeSenhaDeNovo(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	instalarCapturaDF(t)
	router := routerDoisFatores(client)
	const senha = "SenhaAluno#123"
	id, codigo, _ := seedEstudanteDoisFatores(t, client, senha, "ativo", true, true)
	_, primeiro := loginAdminDF(t, router, codigo, senha)

	if _, err := client.DB().Exec(`UPDATE dois_fatores_desafios SET iniciado_em = NOW() - INTERVAL '31 minutes', created_at = created_at - INTERVAL '31 minutes' WHERE user_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	rec, _ := chamar(router, http.MethodPost, "/login/dois-fatores/reenviar", "", map[string]any{"desafio_id": primeiro["desafio_id"]})
	exigirStatus(t, rec, http.StatusUnauthorized)
}

func TestIntegrationDoisFatoresVerificarComDadosInvalidos(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	instalarCapturaDF(t)
	router := routerDoisFatores(client)

	for _, corpo := range []map[string]any{
		{},
		{"desafio_id": uuid.NewString()},
		{"codigo": "123456"},
	} {
		rec, _ := chamar(router, http.MethodPost, "/login/dois-fatores/verificar", "", corpo)
		exigirStatus(t, rec, http.StatusBadRequest)
	}
	rec, _ := chamar(router, http.MethodPost, "/login/dois-fatores/verificar", "", map[string]any{"desafio_id": "nao-e-uuid", "codigo": "123456"})
	exigirStatus(t, rec, http.StatusUnauthorized)
	rec, _ = chamar(router, http.MethodPost, "/login/dois-fatores/verificar", "", map[string]any{"desafio_id": uuid.NewString(), "codigo": "123456"})
	exigirStatus(t, rec, http.StatusUnauthorized)
	rec, _ = chamar(router, http.MethodPost, "/login/dois-fatores/verificar", "", map[string]any{"desafio_id": uuid.NewString(), "codigo": "12"})
	exigirStatus(t, rec, http.StatusBadRequest)
}

func TestIntegrationDoisFatoresRotasAutenticadasExigemToken(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	instalarCapturaDF(t)
	router := routerDoisFatores(client)
	for _, rota := range [][2]string{
		{http.MethodGet, "/dois-fatores"},
		{http.MethodPost, "/dois-fatores/ativacao/solicitar"},
		{http.MethodPost, "/dois-fatores/ativacao/confirmar"},
		{http.MethodPost, "/dois-fatores/desativacao/solicitar"},
		{http.MethodPost, "/dois-fatores/desativacao/confirmar"},
	} {
		rec, _ := chamar(router, rota[0], rota[1], "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s sem token deveria dar 401, deu %d", rota[0], rota[1], rec.Code)
		}
	}
}

// ── Projeções (três tipos) ──────────────────────────────────────────────────

func eventoDF(tipoAgregado, tipoEvento string, id uuid.UUID, versao int) db.Event {
	return db.Event{
		ID: int64(versao), EventID: uuid.New(), AggregateID: id, AggregateType: tipoAgregado,
		EventType: tipoEvento, EventVersion: versao, Payload: []byte(`{}`),
	}
}

func lerDoisFatoresAtivo(t *testing.T, client *db.Client, tabela string, id uuid.UUID) bool {
	t.Helper()
	var ativo bool
	if err := client.DB().Get(&ativo, `SELECT dois_fatores_ativo FROM `+tabela+` WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	return ativo
}

func TestIntegrationDoisFatoresProjecoesDosTresTipos(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)

	adminID, _ := seedAdminParaRecuperacao(t, client, "SenhaForte#123", true)
	acadID, _, _ := seedAcademiaDoisFatores(t, client, "SenhaAcademia#123", false)
	estID, _, _ := seedEstudanteDoisFatores(t, client, "SenhaAluno#123", "ativo", true, false)

	casos := []struct {
		nome    string
		tipo    string
		tabela  string
		id      uuid.UUID
		handler func(db.Event) error
	}{
		{"admin", "Admin", "projection_admins", adminID, projections.NewAdminProjection(client).Handle},
		{"academia", "Academia", "projection_academias", acadID, projections.NewAcademiaProjection(client).Handle},
		{"estudante", "Estudante", "projection_estudantes", estID, projections.NewEstudanteProjection(client).Handle},
	}
	for i, c := range casos {
		if lerDoisFatoresAtivo(t, client, c.tabela, c.id) {
			t.Fatalf("%s: começa desativado", c.nome)
		}
		if err := c.handler(eventoDF(c.tipo, "DoisFatoresAtivados", c.id, 100+i)); err != nil {
			t.Fatalf("%s: ativar: %v", c.nome, err)
		}
		if !lerDoisFatoresAtivo(t, client, c.tabela, c.id) {
			t.Errorf("%s: DoisFatoresAtivados deveria ligar a coluna", c.nome)
		}
		// Idempotente: reaplicar o mesmo evento não muda nada.
		if err := c.handler(eventoDF(c.tipo, "DoisFatoresAtivados", c.id, 100+i)); err != nil {
			t.Fatalf("%s: reaplicar: %v", c.nome, err)
		}
		if err := c.handler(eventoDF(c.tipo, "DoisFatoresDesativados", c.id, 101+i)); err != nil {
			t.Fatalf("%s: desativar: %v", c.nome, err)
		}
		if lerDoisFatoresAtivo(t, client, c.tabela, c.id) {
			t.Errorf("%s: DoisFatoresDesativados deveria desligar a coluna", c.nome)
		}
	}

	// A projeção de academia também processa o evento na variante transacional.
	tx, err := client.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := projections.NewAcademiaProjection(client).HandleTx(tx, eventoDF("Academia", "DoisFatoresAtivados", acadID, 200)); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if !lerDoisFatoresAtivo(t, client, "projection_academias", acadID) {
		t.Error("academia: HandleTx deveria ligar a coluna")
	}

	// Um evento de OUTRO tipo de agregado com o mesmo nome não afeta esta projeção.
	if err := projections.NewAdminProjection(client).Handle(eventoDF("Estudante", "DoisFatoresAtivados", adminID, 300)); err != nil {
		t.Fatal(err)
	}
	if lerDoisFatoresAtivo(t, client, "projection_admins", adminID) {
		t.Error("a projeção de admin não pode reagir a eventos de Estudante")
	}
}
