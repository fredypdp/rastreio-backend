package aggregates

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Tarefa 123 (SEC-04): o evento SenhaTemporariaDefinida existe nos três agregados
// e o evento de senha do estudante carrega o motivo sem quebrar eventos antigos.
func TestDefinirSenhaTemporariaGeraEventoNosTresAgregados(t *testing.T) {
	adm := NewAdmin()
	aca := NewAcademia()
	est := NewEstudante()
	for nome, def := range map[string]struct {
		id  uuid.UUID
		fn  func() error
		evs func() []DomainEvent
	}{
		"admin":     {adm.ID, adm.DefinirSenhaTemporaria, adm.GetUncommittedEvents},
		"academia":  {aca.ID, aca.DefinirSenhaTemporaria, aca.GetUncommittedEvents},
		"estudante": {est.ID, est.DefinirSenhaTemporaria, est.GetUncommittedEvents},
	} {
		if err := def.fn(); err != nil {
			t.Fatalf("%s: %v", nome, err)
		}
		evs := def.evs()
		if len(evs) != 1 || evs[0].GetEventType() != "SenhaTemporariaDefinida" || evs[0].GetAggregateID() != def.id {
			t.Fatalf("%s: eventos inesperados %+v", nome, evs)
		}
	}
}

func TestEstudanteAlterarSenhaComMotivo(t *testing.T) {
	hash := "$2a$10$" + strings.Repeat("x", 53)
	est := NewEstudante()
	if err := est.AlterarSenha(hash); err != nil {
		t.Fatal(err)
	}
	if err := est.AlterarSenhaComMotivo(hash, MotivoSenhaCredenciaisReenviadas); err != nil {
		t.Fatal(err)
	}
	evs := est.GetUncommittedEvents()
	if len(evs) != 2 {
		t.Fatalf("esperava 2 eventos, houve %d", len(evs))
	}
	motivo := func(e DomainEvent) string {
		raw, _ := json.Marshal(e.GetPayload())
		var p struct{ Motivo string }
		_ = json.Unmarshal(raw, &p)
		return p.Motivo
	}
	if got := motivo(evs[0]); got != MotivoSenhaAlteracaoUsuario {
		t.Errorf("AlterarSenha deve registar o motivo %q, registou %q", MotivoSenhaAlteracaoUsuario, got)
	}
	if got := motivo(evs[1]); got != MotivoSenhaCredenciaisReenviadas {
		t.Errorf("motivo %q, quer %q", got, MotivoSenhaCredenciaisReenviadas)
	}
	if err := est.AlterarSenhaComMotivo("curto", MotivoSenhaResetSenha); err == nil {
		t.Error("hash curto deve ser recusado")
	}
}

// Eventos gravados ANTES da Tarefa 123 não têm "Motivo": têm de continuar a
// ser lidos (o ledger é imutável).
func TestEventoSenhaAlteradaAntigoSemMotivoContinuaLegivel(t *testing.T) {
	antigo := []byte(`{"EventType":"SenhaAlterada","NovaSenhaHash":"` + "$2a$10$" + strings.Repeat("y", 53) + `","AlteradaAt":"2026-01-01T00:00:00Z"}`)
	var ev SenhaAlteradaEvent
	if err := json.Unmarshal(antigo, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Motivo != "" {
		t.Errorf("motivo deveria vir vazio, veio %q", ev.Motivo)
	}
	novo, _ := json.Marshal(&SenhaAlteradaEvent{NovaSenhaHash: "h"})
	if strings.Contains(string(novo), "Motivo") {
		t.Errorf("Motivo vazio não deve ser serializado (omitempty): %s", novo)
	}
}
