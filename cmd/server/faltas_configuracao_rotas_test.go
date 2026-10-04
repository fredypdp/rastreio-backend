package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// As rotas de configuração de faltas existem em setupRouter() e exigem autenticação:
// sem token, a resposta nunca pode ser 404 (rota ausente) nem 2xx (rota aberta).
func TestRotasConfiguracaoFaltasExistemEExigemAutenticacao(t *testing.T) {
	router := setupRouter()

	registradas := map[string]bool{}
	for _, r := range router.Routes() {
		registradas[r.Method+" "+r.Path] = true
	}
	for _, rota := range []string{"GET /academia/faltas/configuracao", "PUT /academia/faltas/configuracao"} {
		if !registradas[rota] {
			t.Fatalf("rota %q não está registrada em setupRouter()", rota)
		}
	}

	for _, metodo := range []string{http.MethodGet, http.MethodPut} {
		req := httptest.NewRequest(metodo, "/academia/faltas/configuracao", strings.NewReader(`{"limite_faltas_por_periodo":5,"reprovacao_por_faltas":true}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
			t.Fatalf("%s sem token deveria dar 401/403, deu %d: %s", metodo, rec.Code, rec.Body.String())
		}
	}
}
