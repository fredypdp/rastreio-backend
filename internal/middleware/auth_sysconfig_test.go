package middleware

import (
	"testing"

	"spuri/internal/sysconfig"
)

// Parte 3: JWT_EXPIRY_HOURS passa a vir do banco e, se faltar, do ambiente.
// É lido a cada token emitido, então a alteração vale para os próximos logins.
func TestGetJWTExpiryHoursLeDoBancoDepoisDoAmbiente(t *testing.T) {
	t.Cleanup(sysconfig.Parar)

	t.Setenv("JWT_EXPIRY_HOURS", "10")
	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{"JWT_EXPIRY_HOURS": "2"}, 0))
	if got := getJWTExpiryHours(); got != 2 {
		t.Fatalf("o banco deveria vencer o ambiente: %d", got)
	}

	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{}, 0))
	if got := getJWTExpiryHours(); got != 10 {
		t.Fatalf("sem valor no banco deveria usar o ambiente: %d", got)
	}

	t.Setenv("JWT_EXPIRY_HOURS", "")
	if got := getJWTExpiryHours(); got != 24 {
		t.Fatalf("sem banco nem ambiente deveria usar 24: %d", got)
	}
}
