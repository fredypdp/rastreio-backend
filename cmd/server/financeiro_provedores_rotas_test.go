package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Rotas de provedores de pagamento (Tarefas 119 e 120): as rotas neutras
// existem, os aliases deprecados /financeiro/appypay/* foram removidos e as
// URLs públicas de webhook da AppyPay — já configuradas no painel deles — não
// mudam.
func TestRotasProvedoresPagamentoNeutrasSemAliasesDeprecadosEWebhooks(t *testing.T) {
	router := setupRouter()
	registradas := map[string]bool{}
	for _, r := range router.Routes() {
		registradas[r.Method+" "+r.Path] = true
	}
	obrigatorias := []string{
		"GET /financeiro/provedores", "GET /financeiro/provedor", "PUT /financeiro/provedor",
		"POST /financeiro/credenciais", "PUT /financeiro/credenciais/:id", "GET /financeiro/credenciais", "DELETE /financeiro/credenciais",
		"GET /financeiro/credenciais/:id/webhook-secret", "POST /financeiro/credenciais/:id/webhook-secret/rotacionar",
		"POST /financeiro/cobrancas", "POST /financeiro/qr-codes", "GET /financeiro/cobrancas/:id",
		"POST /financeiro/cobrancas/:id/cancelar", "POST /financeiro/cobrancas/:id/pago-externamente", "GET /financeiro/cobrancas",
		// webhooks públicos da AppyPay: URLs inalteradas
		"POST /webhooks/appypay/gpo", "POST /webhooks/appypay/ref",
		// rota que compartilha prefixo com /financeiro/cobrancas/:id
		"GET /financeiro/cobrancas/estudante/:codigo",
	}
	for _, rota := range obrigatorias {
		if !registradas[rota] {
			t.Fatalf("rota %q não está registrada em setupRouter()", rota)
		}
	}
	// Nenhuma rota /financeiro/appypay/* sobrou (aliases removidos na Tarefa 120).
	// /webhooks/appypay/* NÃO conta: são as URLs públicas já configuradas na AppyPay.
	for rota := range registradas {
		if strings.Contains(rota, "/financeiro/appypay") {
			t.Fatalf("alias deprecado ainda registrado: %s", rota)
		}
	}
	// Rotas autenticadas nunca respondem 404 nem 2xx sem token.
	for _, rota := range []string{"GET /financeiro/provedores", "GET /financeiro/provedor", "PUT /financeiro/provedor", "POST /financeiro/credenciais", "DELETE /financeiro/credenciais"} {
		partes := strings.SplitN(rota, " ", 2)
		req := httptest.NewRequest(partes[0], partes[1], strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
			t.Fatalf("%s sem token deveria dar 401/403, deu %d: %s", rota, rec.Code, rec.Body.String())
		}
	}
}
