package middleware

import (
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Tarefa 123 (SEC-04): com a senha temporária ativa só existem três rotas, e
// comparam-se método E caminho exatos.
func TestRotaTrocaSenhaObrigatoriaAllowlistExata(t *testing.T) {
	permitidas := []struct{ metodo, caminho string }{
		{http.MethodPut, "/alterar-senha"},
		{http.MethodGet, "/meu-perfil"},
		{http.MethodPost, "/logout"},
	}
	for _, r := range permitidas {
		if !rotaTrocaSenhaObrigatoria(r.metodo, r.caminho) {
			t.Errorf("%s %s deveria estar permitida", r.metodo, r.caminho)
		}
	}
	proibidas := []struct{ metodo, caminho string }{
		{http.MethodGet, "/alterar-senha"},    // método errado
		{http.MethodPost, "/alterar-senha"},   // método errado
		{http.MethodPut, "/meu-perfil"},       // método errado
		{http.MethodGet, "/logout"},           // método errado
		{http.MethodGet, "/meu-perfil/extra"}, // caminho que apenas começa igual
		{http.MethodPut, "/alterar-senha/"},   // barra final: não é exata
		{http.MethodGet, "/dois-fatores"},
		{http.MethodPost, "/estudantes/ABC1234/credenciais/reenviar"},
		{http.MethodGet, "/"},
	}
	for _, r := range proibidas {
		if rotaTrocaSenhaObrigatoria(r.metodo, r.caminho) {
			t.Errorf("%s %s NÃO deveria estar permitida com senha temporária", r.metodo, r.caminho)
		}
	}
}

func TestSessaoInvalidadaPorReset(t *testing.T) {
	desde := time.Date(2026, 11, 12, 10, 0, 0, 700_000_000, time.UTC) // 10:00:00.7
	valido := func(t time.Time) sql.NullTime { return sql.NullTime{Time: t, Valid: true} }
	iat := func(t time.Time) *jwt.NumericDate { return jwt.NewNumericDate(t) }

	if sessaoInvalidadaPorReset(iat(desde.Add(-time.Hour)), sql.NullTime{}) {
		t.Error("sem sessoes_validas_desde nenhuma sessão é invalidada")
	}
	if !sessaoInvalidadaPorReset(iat(desde.Add(-time.Hour)), valido(desde)) {
		t.Error("token emitido antes do evento deve ser recusado")
	}
	if !sessaoInvalidadaPorReset(iat(desde.Add(-time.Second)), valido(desde)) {
		t.Error("token emitido 1 s antes do evento deve ser recusado")
	}
	if sessaoInvalidadaPorReset(iat(desde.Add(time.Second)), valido(desde)) {
		t.Error("token emitido depois do evento deve valer")
	}
	// O iat do JWT tem precisão de segundos: o mesmo segundo do evento vale, para
	// o titular que redefine a senha e entra de imediato não ser expulso.
	if sessaoInvalidadaPorReset(iat(time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)), valido(desde)) {
		t.Error("token emitido no mesmo segundo do evento não deve ser recusado")
	}
	if !sessaoInvalidadaPorReset(nil, valido(desde)) {
		t.Error("token sem iat não pode escapar da invalidação")
	}
}
