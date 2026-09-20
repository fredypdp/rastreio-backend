package finance

import (
	"context"
	"errors"
	"testing"
)

// Tarefa 111: ConsultarMesInicioCobranca é novo — antes não existia
// nenhuma forma de descobrir, antes de tentar remover, se já existe uma
// exceção de início de cobrança definida para o ano letivo.
// RemoveMesInicioCobranca já validava isso internamente (devolvendo
// ErrNotFound), mas o frontend não tinha como perguntar antecipadamente,
// então o botão "Remover início de cobrança" ficava sempre disponível
// mesmo quando não havia nada para remover.

func TestIntegrationConsultarMesInicioCobrancaSemExcecaoDefinida(t *testing.T) {
	client := integrationClient(t)
	service := NewService(client)
	academia := mensalidadeCodigo()
	seedMensalidadeAcademia(t, client, academia, "private", "fundamental", "2025_2026")

	if _, err := service.ConsultarMesInicioCobranca(context.Background(), academia, "2025_2026"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("esperava ErrNotFound quando nenhuma exceção foi definida para o ano letivo, obteve: %v", err)
	}
}

func TestIntegrationConsultarMesInicioCobrancaComExcecaoDefinida(t *testing.T) {
	client := integrationClient(t)
	service := NewService(client)
	academia := mensalidadeCodigo()
	seedMensalidadeAcademia(t, client, academia, "private", "fundamental", "2025_2026")

	in := MesInicioCobrancaInput{CodigoAcademia: academia, AnoLetivo: "2025_2026", MesInicio: 11}
	if err := service.DefinirMesInicioCobranca(context.Background(), in, "actor-teste", "academia", "127.0.0.1"); err != nil {
		t.Fatalf("DefinirMesInicioCobranca falhou: %v", err)
	}

	mes, err := service.ConsultarMesInicioCobranca(context.Background(), academia, "2025_2026")
	if err != nil {
		t.Fatalf("ConsultarMesInicioCobranca falhou depois de DefinirMesInicioCobranca: %v", err)
	}
	if mes != 11 {
		t.Fatalf("mes_inicio = %d, queria 11", mes)
	}

	// Outro ano letivo da mesma academia continua sem exceção — a consulta
	// é isolada por ano letivo, não só por academia.
	if _, err := service.ConsultarMesInicioCobranca(context.Background(), academia, "2026_2027"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("esperava ErrNotFound para um ano letivo sem exceção própria, obteve: %v", err)
	}
}

func TestIntegrationConsultarMesInicioCobrancaVoltaAErrNotFoundAposRemocao(t *testing.T) {
	client := integrationClient(t)
	service := NewService(client)
	academia := mensalidadeCodigo()
	seedMensalidadeAcademia(t, client, academia, "private", "fundamental", "2025_2026")

	in := MesInicioCobrancaInput{CodigoAcademia: academia, AnoLetivo: "2025_2026", MesInicio: 11}
	if err := service.DefinirMesInicioCobranca(context.Background(), in, "actor-teste", "academia", "127.0.0.1"); err != nil {
		t.Fatalf("DefinirMesInicioCobranca falhou: %v", err)
	}
	if err := service.RemoveMesInicioCobranca(context.Background(), academia, "2025_2026", "actor-teste", "academia", "127.0.0.1"); err != nil {
		t.Fatalf("RemoveMesInicioCobranca falhou: %v", err)
	}

	if _, err := service.ConsultarMesInicioCobranca(context.Background(), academia, "2025_2026"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("esperava ErrNotFound depois de remover a exceção, obteve: %v", err)
	}
}

func TestIntegrationConsultarMesInicioCobrancaExigeCodigoAcademiaEAnoLetivo(t *testing.T) {
	client := integrationClient(t)
	service := NewService(client)

	if _, err := service.ConsultarMesInicioCobranca(context.Background(), "", "2025_2026"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("esperava erro de validação (não ErrNotFound) para codigo_academia vazio, obteve: %v", err)
	}
	if _, err := service.ConsultarMesInicioCobranca(context.Background(), "ACAD1", "ano-invalido"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("esperava erro de validação (não ErrNotFound) para ano_letivo inválido, obteve: %v", err)
	}
}
