package services

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Tarefa 123 (SEC-07): sem Brevo configurado nenhum envio finge que correu bem
// e nenhum segredo (link com token, senha) vai para o log.

func capturarLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	antigo := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(antigo) })
	return &buf
}

func semBrevo(t *testing.T) {
	t.Helper()
	t.Setenv("BREVO_API_KEY", "")
	t.Setenv("EMAIL_FROM", "")
	t.Setenv("ENV", "test")
	t.Setenv("CREDENCIAIS_LOG_DEV", "")
}

func TestSendVerificationEmailSemBrevoFalhaFechadoSemLogarLink(t *testing.T) {
	semBrevo(t)
	buf := capturarLog(t)
	// db nil: o envio tem de falhar ANTES de tocar na base (nenhum token é gravado).
	err := NewEmailService(nil).SendVerificationEmail(uuid.New(), "estudante", "ana@example.com", "Ana")
	if !errors.Is(err, ErrEmailNaoConfigurado) {
		t.Fatalf("quer ErrEmailNaoConfigurado, obteve %v", err)
	}
	if strings.Contains(buf.String(), "/verificar-email/") {
		t.Fatalf("o link de verificação (contém o token) foi escrito no log: %s", buf.String())
	}
}

func TestSendPasswordResetLinkEmailSemBrevoFalhaFechado(t *testing.T) {
	semBrevo(t)
	buf := capturarLog(t)
	err := NewEmailService(nil).SendPasswordResetLinkEmail(uuid.New(), "admin", "ana@example.com", "Ana")
	if !errors.Is(err, ErrEmailNaoConfigurado) {
		t.Fatalf("quer ErrEmailNaoConfigurado, obteve %v", err)
	}
	if strings.Contains(buf.String(), "/recuperar-senha/") {
		t.Fatalf("o link de recuperação foi escrito no log: %s", buf.String())
	}
}

func TestSendAdminWelcomeEmailSemBrevoNaoLogaSenha(t *testing.T) {
	semBrevo(t)
	buf := capturarLog(t)
	err := NewEmailService(nil).SendAdminWelcomeEmail("ana@example.com", "Ana", "Ab3dEf7h", "adm")
	if !errors.Is(err, ErrEmailNaoConfigurado) {
		t.Fatalf("quer ErrEmailNaoConfigurado (o chamador não pode afirmar que enviou), obteve %v", err)
	}
	if strings.Contains(buf.String(), "Ab3dEf7h") {
		t.Fatalf("a senha temporária foi escrita no log: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "NÃO enviadas") {
		t.Fatalf("o log deveria registar apenas que as credenciais não foram enviadas: %s", buf.String())
	}
}

func TestSendAdminWelcomeEmailLogDevSoForaDeProducao(t *testing.T) {
	semBrevo(t)
	t.Setenv("CREDENCIAIS_LOG_DEV", "true")

	// Desenvolvimento + flag explícita: a única exceção, credenciais no log.
	buf := capturarLog(t)
	if err := NewEmailService(nil).SendAdminWelcomeEmail("ana@example.com", "Ana", "Ab3dEf7h", "adm"); err != nil {
		t.Fatalf("em desenvolvimento com a flag, esperava nil: %v", err)
	}
	if !strings.Contains(buf.String(), "Ab3dEf7h") {
		t.Fatalf("com a flag em desenvolvimento a senha deveria ir ao log: %s", buf.String())
	}

	// Produção: a flag é ignorada — erro e nenhum segredo no log.
	t.Setenv("ENV", "production")
	buf2 := capturarLog(t)
	err := NewEmailService(nil).SendAdminWelcomeEmail("ana@example.com", "Ana", "Zz9yXw8v", "adm")
	if !errors.Is(err, ErrEmailNaoConfigurado) {
		t.Fatalf("em produção quer ErrEmailNaoConfigurado, obteve %v", err)
	}
	if strings.Contains(buf2.String(), "Zz9yXw8v") {
		t.Fatalf("em produção a senha NUNCA pode ir ao log: %s", buf2.String())
	}
}

func TestEnviosRejeitamEmailVazio(t *testing.T) {
	semBrevo(t)
	svc := NewEmailService(nil)
	if err := svc.SendVerificationEmail(uuid.New(), "admin", "", "A"); err == nil {
		t.Error("verificação com e-mail vazio deveria falhar")
	}
	if err := svc.SendPasswordResetLinkEmail(uuid.New(), "admin", "", "A"); err == nil {
		t.Error("recuperação com e-mail vazio deveria falhar")
	}
	if err := svc.SendAdminWelcomeEmail("", "A", "x", "adm"); err == nil {
		t.Error("boas-vindas com e-mail vazio deveria falhar")
	}
}
