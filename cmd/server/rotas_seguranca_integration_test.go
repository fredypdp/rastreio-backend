package main

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

// Tarefa 123 (SEC-01) — as rotas que devolviam o token em claro ao chamador
// deixaram de existir, e a de pedir verificação de e-mail passou a exigir login.
func TestSeguranca01RotasGerarTokenNaoExistem(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)

	for _, rota := range fx.router.Routes() {
		if strings.Contains(rota.Path, "gerar-token") {
			t.Fatalf("rota proibida registada: %s %s (devolvia o token ao chamador)", rota.Method, rota.Path)
		}
	}
	for _, caminho := range []string{"/email/gerar-token/recuperacao", "/email/gerar-token/verificacao"} {
		r, _ := chamarJSON(fx, http.MethodPost, caminho, "", map[string]any{"identificador": "x", "tipo": "admin"})
		if r.Code != http.StatusNotFound {
			t.Fatalf("POST %s: esperava 404, recebi %d: %s", caminho, r.Code, r.Body.String())
		}
	}
}

func TestSeguranca01SolicitarVerificacaoEmailExigeAutenticacao(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	// Antes estava fora do grupo autenticado: o handler lê user_id do contexto e
	// por isso falhava SEMPRE. Agora o middleware recusa quem não tem sessão.
	r, _ := chamarJSON(fx, http.MethodPost, "/email/verificar-email/solicitar", "", nil)
	if r.Code != http.StatusUnauthorized {
		t.Fatalf("sem token: esperava 401, recebi %d: %s", r.Code, r.Body.String())
	}
	// A rota com :token continua pública.
	r2, _ := chamarJSON(fx, http.MethodPost, "/email/verificar-email/tokeninexistente", "", nil)
	if r2.Code == http.StatusUnauthorized || r2.Code == http.StatusNotFound && !strings.Contains(r2.Body.String(), "token") {
		t.Fatalf("verificar-email/:token deveria continuar pública; recebi %d: %s", r2.Code, r2.Body.String())
	}
}

// Tarefa 123 (SEC-02) — nenhum limitador desativado (código morto) é referenciado.
func TestSeguranca02LimitadoresDesativadosForamRemovidos(t *testing.T) {
	for _, arquivo := range []string{"main.go", "../../internal/middleware/rate_limit.go"} {
		bruto, err := os.ReadFile(arquivo)
		if err != nil {
			t.Fatal(err)
		}
		for _, proibido := range []string{"GlobalRateLimit", "LoginRateLimit", "EmailRateLimit"} {
			if strings.Contains(string(bruto), proibido+"(") {
				t.Errorf("%s ainda referencia o limitador desativado %s", arquivo, proibido)
			}
		}
	}
}
