package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"spuri/internal/sysconfig"
)

func preflight(t *testing.T, origem string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(corsMiddleware())
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", origem)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// Parte 3: ALLOWED_ORIGINS vem do banco e, se faltar, do ambiente, lido a cada
// requisição — a alteração vale na hora, sem reiniciar.
func TestCorsLeAsOrigensDoBancoDepoisDoAmbiente(t *testing.T) {
	t.Cleanup(sysconfig.Parar)
	t.Setenv("ALLOWED_ORIGINS", "https://ambiente.exemplo.ao")

	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{"ALLOWED_ORIGINS": "https://banco.exemplo.ao"}, 0))
	if got := preflight(t, "https://banco.exemplo.ao").Header().Get("Access-Control-Allow-Origin"); got != "https://banco.exemplo.ao" {
		t.Fatalf("origem do banco deveria ser aceita: %q", got)
	}
	if got := preflight(t, "https://ambiente.exemplo.ao").Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("com valor no banco, a origem só do ambiente não vale: %q", got)
	}

	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{}, 0))
	if got := preflight(t, "https://ambiente.exemplo.ao").Header().Get("Access-Control-Allow-Origin"); got != "https://ambiente.exemplo.ao" {
		t.Fatalf("sem valor no banco deveria usar o ambiente: %q", got)
	}
}

func TestRotasConfiguracaoSistemaExistemEExigemAutenticacao(t *testing.T) {
	router := setupRouter()
	registradas := map[string]bool{}
	for _, r := range router.Routes() {
		registradas[r.Method+" "+r.Path] = true
	}
	rotas := []string{"GET /admin/sistema/configuracoes", "PUT /admin/sistema/configuracoes/:chave", "DELETE /admin/sistema/configuracoes/:chave"}
	for _, rota := range rotas {
		if !registradas[rota] {
			t.Fatalf("rota %q não está registrada em setupRouter()", rota)
		}
	}
	for metodo, caminho := range map[string]string{
		http.MethodGet:    "/admin/sistema/configuracoes",
		http.MethodPut:    "/admin/sistema/configuracoes/EMAIL_FROM",
		http.MethodDelete: "/admin/sistema/configuracoes/EMAIL_FROM",
	} {
		req := httptest.NewRequest(metodo, caminho, strings.NewReader(`{"valor":"a@b.ao"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s sem token deveria dar 401/403, deu %d", metodo, caminho, rec.Code)
		}
	}
}

// Leitura para adm e fpp; alteração só para fpp. Os middlewares de papel vivem na
// declaração da rota, então o teste confere o texto de main.go.
func TestPapeisDasRotasDeConfiguracaoSistema(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for rota, papel := range map[string]string{
		`adminSistema.GET("/sistema/configuracoes", middleware.RequireAdm(),`:           "leitura: adm e fpp",
		`adminSistema.PUT("/sistema/configuracoes/:chave", middleware.RequireFPP(),`:    "alterar: só fpp",
		`adminSistema.DELETE("/sistema/configuracoes/:chave", middleware.RequireFPP(),`: "remover: só fpp",
	} {
		if !strings.Contains(src, rota) {
			t.Errorf("main.go deveria declarar a rota com o papel certo (%s): %s", papel, rota)
		}
	}
}

// O sysconfig precisa carregar depois das migrations (a tabela nasce na 139) e antes de
// qualquer leitura de configuração: se a validação dos provedores voltasse para antes,
// um APPYPAY_RESOURCE gravado só no banco impediria o servidor de subir.
func TestSysconfigIniciaDepoisDasMigrationsEAntesDasValidacoes(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	pos := func(s string) int {
		i := strings.Index(src, s)
		if i < 0 {
			t.Fatalf("main.go não contém %q", s)
		}
		return i
	}
	migrations := pos("dbClient.RunMigrations()")
	iniciar := pos("sysconfig.Iniciar(")
	provedores := pos("finance.ValidarConfiguracaoProvedores()")
	storage := pos("func initStorage()")
	if !(migrations < iniciar && iniciar < provedores) {
		t.Fatalf("ordem esperada em initDB: migrations (%d) < sysconfig.Iniciar (%d) < ValidarConfiguracaoProvedores (%d)", migrations, iniciar, provedores)
	}
	if strings.Count(src, "finance.ValidarConfiguracaoProvedores()") != 1 {
		t.Fatal("ValidarConfiguracaoProvedores deve ser chamada uma única vez")
	}
	if iniciar > storage {
		t.Fatalf("sysconfig.Iniciar deve estar em initDB, antes de initStorage")
	}
}
