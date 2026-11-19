package finance

import (
	"testing"

	"spuri/internal/sysconfig"
)

// Parte 3: APPYPAY_RESOURCE e os parâmetros da reconciliação leem do banco e, se
// faltar, do ambiente. A reconciliação lê tudo uma vez no arranque.
func TestAppyPayResourceLeDoBancoOuDoAmbiente(t *testing.T) {
	t.Cleanup(sysconfig.Parar)
	t.Setenv("APPYPAY_RESOURCE", "")
	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{}, 0))
	if err := ValidateAppyPayResourceConfig(); err == nil {
		t.Fatal("sem valor em lugar nenhum deveria falhar no arranque")
	}
	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{"APPYPAY_RESOURCE": "11111111-2222-3333-4444-555555555555"}, 0))
	if err := ValidateAppyPayResourceConfig(); err != nil {
		t.Fatalf("valor só no banco deveria bastar: %v", err)
	}
	if v, _ := appyPayResource(); v != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("resource: %q", v)
	}
}

func TestConfigReconciliacaoLeDoBancoDepoisDoAmbiente(t *testing.T) {
	t.Cleanup(sysconfig.Parar)
	t.Setenv("RECONCILIACAO_CONCORRENCIA", "7")
	t.Setenv("RECONCILIACAO_RESYNC_MIN", "30")
	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{
		"RECONCILIACAO_ATIVA":                "false",
		"RECONCILIACAO_CONCORRENCIA":         "5",
		"RECONCILIACAO_CHAMADAS_POR_SEGUNDO": "1.5",
	}, 0))
	cfg := ConfigReconciliacaoDoAmbiente()
	if cfg.Ativa {
		t.Error("RECONCILIACAO_ATIVA=false no banco deveria desligar o job")
	}
	if cfg.Concorrencia != 5 {
		t.Errorf("concorrência: o banco (5) deveria vencer o ambiente (7): %d", cfg.Concorrencia)
	}
	if cfg.ChamadasPorSegundo != 1.5 {
		t.Errorf("chamadas por segundo: %v", cfg.ChamadasPorSegundo)
	}
	if cfg.Resync.Minutes() != 30 {
		t.Errorf("sem valor no banco vale o ambiente (30 min): %v", cfg.Resync)
	}
}
