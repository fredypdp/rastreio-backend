package services

import "testing"

func TestLoadBrevoConfigMissingAPIKeyDisabled(t *testing.T) {
	t.Setenv("BREVO_API_KEY", "")
	t.Setenv("EMAIL_FROM", "spuri@example.com")
	t.Setenv("EMAIL_USER", "spuri@example.com")

	_, ok := loadBrevoConfig()
	if ok {
		t.Fatal("esperava loadBrevoConfig desabilitado sem BREVO_API_KEY")
	}
}

func TestLoadBrevoConfigMissingSenderDisabled(t *testing.T) {
	t.Setenv("BREVO_API_KEY", "xkeysib-teste")
	t.Setenv("EMAIL_FROM", "")
	t.Setenv("EMAIL_USER", "")

	_, ok := loadBrevoConfig()
	if ok {
		t.Fatal("esperava loadBrevoConfig desabilitado sem EMAIL_FROM")
	}
}

// Tarefa 123 (D7): EMAIL_USER era a variável do SMTP eliminado. Deixa de ser
// remetente de fallback: sem EMAIL_FROM o Brevo fica desabilitado.
func TestLoadBrevoConfigIgnoresEmailUser(t *testing.T) {
	t.Setenv("BREVO_API_KEY", "xkeysib-teste")
	t.Setenv("EMAIL_FROM", "")
	t.Setenv("EMAIL_USER", "spuriartipan@gmail.com")

	if cfg, ok := loadBrevoConfig(); ok {
		t.Fatalf("EMAIL_USER não pode servir de remetente; obteve configuração ativa com %q", cfg.SenderEmail)
	}
}

func TestLoadBrevoConfigUsesEmailFrom(t *testing.T) {
	t.Setenv("BREVO_API_KEY", "xkeysib-teste")
	t.Setenv("EMAIL_FROM", "avisos@spuri.co")
	t.Setenv("EMAIL_USER", "spuriartipan@gmail.com")

	cfg, ok := loadBrevoConfig()
	if !ok {
		t.Fatal("esperava loadBrevoConfig habilitado")
	}
	if cfg.SenderEmail != "avisos@spuri.co" {
		t.Errorf("esperava o remetente de EMAIL_FROM, obteve %q", cfg.SenderEmail)
	}
}

func TestSendAcademiaCadastradaEmailBrevoDisabledDoesNotError(t *testing.T) {
	t.Setenv("BREVO_API_KEY", "")
	t.Setenv("EMAIL_FROM", "")
	t.Setenv("EMAIL_USER", "")

	svc := NewEmailService(nil)
	err := svc.SendAcademiaCadastradaEmailBrevo("admin@example.com", "Admin Teste", AcademiaCadastradaInfo{
		Nome: "Academia Teste",
	})
	if err != nil {
		t.Fatalf("sem BREVO_API_KEY/remetente configurados deveria retornar nil, obteve: %v", err)
	}
}

func TestSendAcademiaCadastradaEmailBrevoEmptyRecipientErrors(t *testing.T) {
	t.Setenv("BREVO_API_KEY", "xkeysib-teste")
	t.Setenv("EMAIL_FROM", "avisos@spuri.co")
	t.Setenv("EMAIL_USER", "")

	svc := NewEmailService(nil)
	err := svc.SendAcademiaCadastradaEmailBrevo("", "Admin Teste", AcademiaCadastradaInfo{Nome: "Academia Teste"})
	if err == nil {
		t.Fatal("esperava erro para destinatário vazio")
	}
}
