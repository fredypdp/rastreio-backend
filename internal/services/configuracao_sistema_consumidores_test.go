package services

import (
	"testing"

	"spuri/internal/sysconfig"
)

// Parte 3: o canal de e-mail (Brevo) e o cliente da Ziett leem do banco e, se
// faltar, do ambiente — e são lidos a cada uso, então a alteração vale na hora.
func TestBrevoLeChaveERemetenteDoBancoOuDoAmbiente(t *testing.T) {
	t.Cleanup(sysconfig.Parar)
	t.Setenv("BREVO_API_KEY", "")
	t.Setenv("EMAIL_FROM", "")

	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{}, 0))
	if BrevoConfigurado() {
		t.Fatal("sem banco nem ambiente o canal não está configurado")
	}

	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{"BREVO_API_KEY": "chave-do-banco", "EMAIL_FROM": "banco@exemplo.ao"}, 0))
	cfg, ok := loadBrevoConfig()
	if !ok || cfg.APIKey != "chave-do-banco" || cfg.SenderEmail != "banco@exemplo.ao" {
		t.Fatalf("deveria ler do banco: ok=%v cfg=%+v", ok, cfg)
	}

	// só a chave no banco e o remetente no ambiente: as duas origens se completam
	t.Setenv("EMAIL_FROM", "ambiente@exemplo.ao")
	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{"BREVO_API_KEY": "chave-do-banco"}, 0))
	cfg, ok = loadBrevoConfig()
	if !ok || cfg.SenderEmail != "ambiente@exemplo.ao" {
		t.Fatalf("remetente deveria cair para o ambiente: ok=%v cfg=%+v", ok, cfg)
	}
}

func TestZiettClientLeAChaveDoBancoOuDoAmbiente(t *testing.T) {
	t.Cleanup(sysconfig.Parar)
	t.Setenv("ZIETT_API_KEY", "")
	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{}, 0))
	if _, err := NewZiettClientFromEnv(); err == nil {
		t.Fatal("sem chave em lugar nenhum deveria falhar")
	}
	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{"ZIETT_API_KEY": "chave-ziett-do-banco"}, 0))
	if _, err := NewZiettClientFromEnv(); err != nil {
		t.Fatalf("chave só no banco deveria bastar: %v", err)
	}
}
