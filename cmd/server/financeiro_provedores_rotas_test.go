package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Rotas de provedores de pagamento (Tarefa 119): as rotas neutras existem, os
// aliases /financeiro/appypay/* continuam registrados (frontend antigo durante
// o deploy) e as URLs públicas de webhook da AppyPay — já configuradas no
// painel deles — não mudam.
func TestRotasProvedoresPagamentoNeutrasAliasesEWebhooks(t *testing.T) {
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
		// aliases deprecados
		"POST /financeiro/appypay/credenciais", "PUT /financeiro/appypay/credenciais/:id", "GET /financeiro/appypay/credenciais", "DELETE /financeiro/appypay/credenciais",
		"GET /financeiro/appypay/credenciais/:id/webhook-secret", "POST /financeiro/appypay/credenciais/:id/webhook-secret/rotacionar",
		"POST /financeiro/appypay/cobrancas", "POST /financeiro/appypay/qr-codes", "GET /financeiro/appypay/cobrancas/:id",
		"POST /financeiro/appypay/cobrancas/:id/cancelar", "POST /financeiro/appypay/cobrancas/:id/pago-externamente",
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
