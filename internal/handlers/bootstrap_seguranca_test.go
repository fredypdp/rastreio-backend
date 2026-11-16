package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Tarefa 127 (SEC-11): token de bootstrap e senha do primeiro admin.
func chamarBootstrap(t *testing.T, headers map[string]string, corpo string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/bootstrap", strings.NewReader(corpo))
	ctx.Request.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		ctx.Request.Header.Set(k, v)
	}
	BootstrapAdminFPP(ctx)
	return rec
}

const corpoBootstrapSenhaFraca = `{"nome":"Admin FPP","email":"admin@dominio.test","telefone":"923456789","senha":"123"}`

func TestBootstrapSemTokenConfiguradoNoServidorDevolve403(t *testing.T) {
	t.Setenv("BOOTSTRAP_TOKEN", "")
	t.Setenv("BOOTSTRAP_TOKEN_DEV", "")
	t.Setenv("ENV", "production")
	rec := chamarBootstrap(t, map[string]string{"X-Bootstrap-Token": "qualquer"}, corpoBootstrapSenhaFraca)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("sem BOOTSTRAP_TOKEN no servidor a rota fica desativada (403): %d %s", rec.Code, rec.Body.String())
	}
}

func TestBootstrapTokenAusenteOuErradoDevolve401(t *testing.T) {
	t.Setenv("BOOTSTRAP_TOKEN", "token-de-bootstrap-correto")
	t.Setenv("ENV", "production")
	for nome, headers := range map[string]map[string]string{
		"sem header":              {},
		"token errado":            {"X-Bootstrap-Token": "token-de-bootstrap-errado"},
		"token com prefixo igual": {"X-Bootstrap-Token": "token-de-bootstrap-corret"},
		"header legado errado":    {"BOOTSTRAP_TOKEN": "outro"},
	} {
		rec := chamarBootstrap(t, headers, corpoBootstrapSenhaFraca)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: esperava 401, obteve %d %s", nome, rec.Code, rec.Body.String())
		}
	}
}

// Com o token certo (em qualquer dos dois headers) a senha fraca é recusada ANTES de
// qualquer acesso ao banco (422), nunca criando o admin.
func TestBootstrapSenhaFracaERecusadaComTokenCorreto(t *testing.T) {
	t.Setenv("BOOTSTRAP_TOKEN", "token-de-bootstrap-correto")
	t.Setenv("ENV", "production")
	for nome, headers := range map[string]map[string]string{
		"X-Bootstrap-Token":        {"X-Bootstrap-Token": "token-de-bootstrap-correto"},
		"BOOTSTRAP_TOKEN (legado)": {"BOOTSTRAP_TOKEN": "token-de-bootstrap-correto"},
	} {
		rec := chamarBootstrap(t, headers, corpoBootstrapSenhaFraca)
		if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
			t.Errorf("%s: esperava erro de validação da senha, obteve %d %s", nome, rec.Code, rec.Body.String())
		}
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden || rec.Code == http.StatusInternalServerError {
			t.Errorf("%s: o token correto devia passar a verificação: %d", nome, rec.Code)
		}
	}
}

func TestBootstrapTokenDevSoForaDeProducao(t *testing.T) {
	t.Setenv("BOOTSTRAP_TOKEN", "")
	t.Setenv("BOOTSTRAP_TOKEN_DEV", "token-dev")
	t.Setenv("ENV", "production")
	if rec := chamarBootstrap(t, map[string]string{"X-Bootstrap-Token": "token-dev"}, corpoBootstrapSenhaFraca); rec.Code != http.StatusForbidden {
		t.Fatalf("BOOTSTRAP_TOKEN_DEV não vale em produção (403): %d", rec.Code)
	}
	t.Setenv("ENV", "test")
	if rec := chamarBootstrap(t, map[string]string{"X-Bootstrap-Token": "token-dev"}, corpoBootstrapSenhaFraca); rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf("BOOTSTRAP_TOKEN_DEV devia valer fora de produção: %d", rec.Code)
	}
}
