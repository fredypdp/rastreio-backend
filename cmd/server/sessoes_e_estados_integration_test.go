package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"spuri/internal/db"
	"spuri/internal/middleware"
	"spuri/internal/projections"
)

var reParametroRota = regexp.MustCompile(`[:*][A-Za-z0-9_]+`)

// criarEstudanteComSenhaTemporaria cadastra um estudante pela academia A e devolve
// (id, código, senha temporária).
func criarEstudanteComSenhaTemporaria(t *testing.T, fx *turmaVinculoFixture, nome string) (string, string, string) {
	t.Helper()
	sms := prepararSMS(t, fx, true)
	rec := postCadastro(t, fx, camposCadastro(nome))
	if rec.Code != http.StatusCreated {
		t.Fatalf("cadastro: %d %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec.Body.Bytes())
	id := dataMap(resp)["id"].(string)
	cod := codigoEstudante(resp)
	aplicarEventosEstudante(t, fx, id, 0)
	aplicarEventos(t, fx, id, 0, projections.NewEstudanteVinculosProjection(fx.client).Handle)
	m := reSenhaNoSMS.FindStringSubmatch(sms.ultimo(t).Conteudo)
	if m == nil {
		t.Fatal("o SMS não contém a senha temporária")
	}
	return id, cod, m[1]
}

// Tarefa 127 (SEC-04/SEC-10): a troca da senha TEMPORÁRIA encerra as sessões emitidas
// antes (quem entrou primeiro com a senha temporária perde o acesso) e o titular
// recebe um token novo, que já funciona em rotas completas.
func TestTrocaSenhaTemporariaEncerraSessoesAnterioresEDevolveNovoToken(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	id, cod, senha := criarEstudanteComSenhaTemporaria(t, fx, "Estudante Troca Temporaria")

	_, l1 := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": cod, "senha": senha})
	atacante, _ := l1["token"].(string)
	time.Sleep(1100 * time.Millisecond)
	_, l2 := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": cod, "senha": senha})
	titular, _ := l2["token"].(string)
	if atacante == "" || titular == "" {
		t.Fatalf("logins sem token: %v %v", l1, l2)
	}

	if rec, _ := chamarJSON(fx, http.MethodGet, "/dois-fatores", atacante, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("com a senha temporária só perfil/trocar senha/logout: esperava 403, obteve %d", rec.Code)
	}

	rec, corpo := chamarJSON(fx, http.MethodPut, "/alterar-senha", titular, map[string]any{"senha_atual": senha, "nova_senha": "MinhaSenhaNova#9"})
	if rec.Code != http.StatusOK {
		t.Fatalf("troca da senha temporária: %d %s", rec.Code, rec.Body.String())
	}
	novo, _ := corpo["token"].(string)
	if novo == "" {
		t.Fatalf("a troca da senha temporária deve devolver um token novo: %v", corpo)
	}
	aplicarEventosEstudante(t, fx, id, 0)

	rec, corpo = chamarJSON(fx, http.MethodGet, "/meu-perfil", atacante, nil)
	if rec.Code != http.StatusUnauthorized || corpo["code"] != middleware.CodigoSessaoInvalida {
		t.Fatalf("a sessão anterior à troca devia ser encerrada (401 %s): %d %s", middleware.CodigoSessaoInvalida, rec.Code, rec.Body.String())
	}
	if rec, _ := chamarJSON(fx, http.MethodGet, "/dois-fatores", novo, nil); rec.Code != http.StatusOK {
		t.Fatalf("o token novo devia aceder a rotas completas: %d %s", rec.Code, rec.Body.String())
	}

	// Uma troca normal (conta sem senha temporária) NÃO devolve token nem encerra sessões.
	rec, corpo = chamarJSON(fx, http.MethodPut, "/alterar-senha", novo, map[string]any{"senha_atual": "MinhaSenhaNova#9", "nova_senha": "OutraSenhaNova#8"})
	if rec.Code != http.StatusOK || corpo["token"] != nil {
		t.Fatalf("troca normal: %d %v", rec.Code, corpo)
	}
	aplicarEventosEstudante(t, fx, id, 0)
	if rec, _ := chamarJSON(fx, http.MethodGet, "/dois-fatores", novo, nil); rec.Code != http.StatusOK {
		t.Fatalf("a troca normal não deve encerrar a sessão atual: %d", rec.Code)
	}
}

// Todo 401 de autenticação traz o código SESSAO_INVALIDA; o 401 de negócio da senha
// atual errada NÃO o traz (senão o frontend terminaria a sessão por um engano).
func TestCodigoSessaoInvalidaSoNosErrosDeAutenticacao(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	_, cod, senha := criarEstudanteComSenhaTemporaria(t, fx, "Estudante Codigo Sessao")

	rec, corpo := chamarJSON(fx, http.MethodGet, "/meu-perfil", "", nil)
	if rec.Code != http.StatusUnauthorized || corpo["code"] != middleware.CodigoSessaoInvalida {
		t.Fatalf("sem token: %d %s", rec.Code, rec.Body.String())
	}
	rec, corpo = chamarJSON(fx, http.MethodGet, "/meu-perfil", "token-invalido", nil)
	if rec.Code != http.StatusUnauthorized || corpo["code"] != middleware.CodigoSessaoInvalida {
		t.Fatalf("token inválido: %d %s", rec.Code, rec.Body.String())
	}

	_, l := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": cod, "senha": senha})
	tok, _ := l["token"].(string)
	rec, corpo = chamarJSON(fx, http.MethodPut, "/alterar-senha", tok, map[string]any{"senha_atual": "SenhaErrada#123", "nova_senha": "NovaSenhaBoa#456"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("senha atual errada devia dar 401, deu %d", rec.Code)
	}
	if corpo["code"] == middleware.CodigoSessaoInvalida {
		t.Fatal("a senha atual errada não pode usar o código SESSAO_INVALIDA")
	}
}

// Falha fechada (Tarefa 127): se o estado da conta não puder ser consultado, o pedido é
// recusado com 503 — nunca aceite sem saber se a conta está bloqueada.
func TestAuthMiddlewareFalhaFechadoQuandoOBancoFalha(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	_ = fx
	cliente, err := db.NewClient(db.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	_ = cliente.Close() // consultas passam a falhar com "database is closed"

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("dbClient", cliente) })
	chegouAoHandler := false
	r.GET("/protegida", middleware.AuthMiddleware(), func(c *gin.Context) { chegouAoHandler = true; c.Status(http.StatusOK) })
	r.GET("/publica", middleware.OptionalAuthMiddleware(), func(c *gin.Context) {
		if _, autenticado := middleware.GetUserID(c); autenticado {
			c.Status(http.StatusAccepted)
			return
		}
		c.Status(http.StatusOK)
	})

	tok, _ := middleware.GenerateToken(fx.academia.ID, "academia")
	req := httptest.NewRequest(http.MethodGet, "/protegida", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || chegouAoHandler {
		t.Fatalf("esperava 503 sem chegar ao handler: %d %s", rec.Code, rec.Body.String())
	}

	// Rota pública com token: sem o estado da conta segue como ANÓNIMA (nunca autenticada).
	req = httptest.NewRequest(http.MethodGet, "/publica", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rota pública com banco em falha devia seguir anónima (200), obteve %d", rec.Code)
	}
}

// Varredura: para cada estado de conta que restringe o acesso, TODAS as rotas autenticadas
// do router real têm de recusar (401/403) — exceto a lista permitida desse estado. Cobre
// academia inativa, estudante deletado, estudante com senha temporária e estudante
// desvinculado (escrita).
func TestVarreduraDeRotasPorEstadoDaConta(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	estID, _, _ := criarEstudanteComSenhaTemporaria(t, fx, "Estudante Varredura Rotas")
	terceira := criarAcademiaCorrecao(t, fx.repository, time.Now().UnixNano(), "V")
	if err := projections.NewAcademiaProjection(fx.client).Rebuild(); err != nil {
		t.Fatal(err)
	}
	sql := func(q string, a ...any) {
		t.Helper()
		if _, err := fx.client.DB().Exec(q, a...); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}

	type rota struct{ metodo, padrao, caminho string }
	var autenticadas []rota
	for _, r := range fx.router.Routes() {
		caminho := reParametroRota.ReplaceAllString(r.Path, "x")
		req := httptest.NewRequest(r.Method, caminho, nil)
		rec := httptest.NewRecorder()
		fx.router.ServeHTTP(rec, req)
		if rec.Code == http.StatusUnauthorized { // sem token = 401 → rota autenticada
			autenticadas = append(autenticadas, rota{r.Method, r.Path, caminho})
		}
	}
	if len(autenticadas) < 100 {
		t.Fatalf("a varredura devia encontrar muitas rotas autenticadas, encontrou %d", len(autenticadas))
	}
	t.Logf("rotas autenticadas varridas: %d", len(autenticadas))

	chamar := func(r rota, token string) int {
		req := httptest.NewRequest(r.metodo, r.caminho, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		fx.router.ServeHTTP(rec, req)
		return rec.Code
	}
	exigir := func(estado string, token string, permitida func(r rota) bool, aceites ...int) {
		t.Helper()
		for _, r := range autenticadas {
			if permitida != nil && permitida(r) {
				continue
			}
			code := chamar(r, token)
			ok := false
			for _, a := range aceites {
				ok = ok || code == a
			}
			if !ok {
				t.Errorf("[%s] %s %s devolveu %d (esperado %v)", estado, r.metodo, r.padrao, code, aceites)
			}
		}
	}

	tokEst, _ := middleware.GenerateToken(uuidMust(t, estID), "estudante")

	// 1) senha temporária pendente: só perfil, trocar senha e logout.
	exigir("senha temporária", tokEst, func(r rota) bool {
		return (r.metodo == http.MethodGet && r.padrao == "/meu-perfil") ||
			(r.metodo == http.MethodPut && r.padrao == "/alterar-senha") ||
			(r.metodo == http.MethodPost && r.padrao == "/logout")
	}, http.StatusForbidden, http.StatusUnauthorized)

	// 2) desvinculado: leitura livre; escrita só a lista permitida.
	sql(`UPDATE projection_estudantes SET deve_alterar_senha = FALSE, status = 'inativo' WHERE id = $1`, estID)
	exigir("estudante inativo (escrita)", tokEst, func(r rota) bool {
		return r.metodo == http.MethodGet || r.metodo == http.MethodHead || r.metodo == http.MethodOptions ||
			middleware.RotaPermitidaEstudanteInativo(r.metodo, r.padrao)
	}, http.StatusForbidden, http.StatusUnauthorized)

	// 3) deletado: nada passa.
	sql(`UPDATE projection_estudantes SET status = 'deletado' WHERE id = $1`, estID)
	exigir("estudante deletado", tokEst, nil, http.StatusUnauthorized)

	// 4) academia inativa: nada passa (a academia só entra ativa).
	sql(`UPDATE projection_academias SET status = 'inativo' WHERE id = $1`, terceira.ID)
	tokAc, _ := middleware.GenerateToken(terceira.ID, "academia")
	exigir("academia inativa", tokAc, nil, http.StatusUnauthorized)
}

func uuidMust(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
