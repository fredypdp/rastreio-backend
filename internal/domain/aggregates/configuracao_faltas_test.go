package aggregates

import (
	"testing"

	"github.com/google/uuid"
)

func novaConfiguracaoFaltas(codigo string) *ConfiguracaoFaltas {
	c := NewConfiguracaoFaltas()
	c.SetID(ConfiguracaoFaltasAggregateID(codigo))
	return c
}

func ptrInt(v int) *int { return &v }

func TestConfiguracaoFaltasIDEDeterministicoPorAcademia(t *testing.T) {
	if ConfiguracaoFaltasAggregateID("ACA1") != ConfiguracaoFaltasAggregateID("  ACA1 ") {
		t.Fatal("o ID deve ser o mesmo para o mesmo código (ignorando espaços nas pontas)")
	}
	if ConfiguracaoFaltasAggregateID("ACA1") == ConfiguracaoFaltasAggregateID("ACA2") {
		t.Fatal("academias diferentes devem ter IDs diferentes")
	}
}

func TestConfiguracaoFaltasDefinirLimiteEReprovacao(t *testing.T) {
	c := novaConfiguracaoFaltas("ACA1")
	if err := c.Definir("ACA1", ptrInt(5), true, uuid.New()); err != nil {
		t.Fatalf("definir limite 5 com reprovação: %v", err)
	}
	if c.LimiteFaltasPorPeriodo == nil || *c.LimiteFaltasPorPeriodo != 5 || !c.ReprovacaoPorFaltas {
		t.Fatalf("estado inesperado: %+v", c)
	}
}

func TestConfiguracaoFaltasLimiteSemReprovacaoEPermitido(t *testing.T) {
	c := novaConfiguracaoFaltas("ACA1")
	if err := c.Definir("ACA1", ptrInt(3), false, uuid.New()); err != nil {
		t.Fatalf("limite sem reprovação deve ser permitido: %v", err)
	}
	if c.ReprovacaoPorFaltas {
		t.Fatal("reprovação deveria estar desligada")
	}
}

func TestConfiguracaoFaltasReprovacaoExigeLimite(t *testing.T) {
	c := novaConfiguracaoFaltas("ACA1")
	if err := c.Definir("ACA1", nil, true, uuid.New()); err == nil {
		t.Fatal("reprovação por faltas sem limite deveria ser rejeitada")
	}
}

func TestConfiguracaoFaltasLimiteForaDoIntervalo(t *testing.T) {
	for _, v := range []int{0, -1, MaxLimiteFaltasPorPeriodo + 1} {
		c := novaConfiguracaoFaltas("ACA1")
		if err := c.Definir("ACA1", ptrInt(v), false, uuid.New()); err == nil {
			t.Fatalf("limite %d deveria ser rejeitado", v)
		}
	}
	c := novaConfiguracaoFaltas("ACA1")
	if err := c.Definir("ACA1", ptrInt(MaxLimiteFaltasPorPeriodo), false, uuid.New()); err != nil {
		t.Fatalf("limite máximo deveria ser aceito: %v", err)
	}
}

func TestConfiguracaoFaltasDesligarLimiteLimpaTudo(t *testing.T) {
	c := novaConfiguracaoFaltas("ACA1")
	_ = c.Definir("ACA1", ptrInt(5), true, uuid.New())
	if err := c.Definir("ACA1", nil, false, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if c.LimiteFaltasPorPeriodo != nil || c.ReprovacaoPorFaltas {
		t.Fatalf("sem limite e sem reprovação, esperado estado vazio: %+v", c)
	}
}

func TestConfiguracaoFaltasExigeIDDeterministico(t *testing.T) {
	c := NewConfiguracaoFaltas() // ID aleatório
	if err := c.Definir("ACA1", ptrInt(5), false, uuid.New()); err == nil {
		t.Fatal("agregado com ID aleatório deveria ser rejeitado")
	}
	c = novaConfiguracaoFaltas("ACA1")
	if err := c.Definir("OUTRA", ptrInt(5), false, uuid.New()); err == nil {
		t.Fatal("código de outra academia deveria ser rejeitado")
	}
	if err := c.Definir("  ", ptrInt(5), false, uuid.New()); err == nil {
		t.Fatal("código vazio deveria ser rejeitado")
	}
}

func TestConfiguracaoFaltasReplayDoLedger(t *testing.T) {
	origem := novaConfiguracaoFaltas("ACA1")
	_ = origem.Definir("ACA1", ptrInt(5), true, uuid.New())
	_ = origem.Definir("ACA1", ptrInt(8), false, uuid.New())
	r := novaConfiguracaoFaltas("ACA1")
	for _, ev := range origem.GetUncommittedEvents() {
		if err := r.Apply(ev); err != nil {
			t.Fatalf("replay: %v", err)
		}
	}
	if r.LimiteFaltasPorPeriodo == nil || *r.LimiteFaltasPorPeriodo != 8 || r.ReprovacaoPorFaltas {
		t.Fatalf("replay deveria terminar em limite 8 sem reprovação: %+v", r)
	}
}

func TestFabricaCriaConfiguracaoFaltas(t *testing.T) {
	agg, err := (&DefaultAggregateFactory{}).Create("ConfiguracaoFaltas")
	if err != nil || agg == nil || agg.GetType() != "ConfiguracaoFaltas" {
		t.Fatalf("fábrica deveria criar ConfiguracaoFaltas: %v %v", agg, err)
	}
}
