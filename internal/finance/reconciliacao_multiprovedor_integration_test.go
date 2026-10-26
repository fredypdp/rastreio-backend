package finance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// PoliticaReconciliacao faz o provedor falso declarar uma política própria
// (extensão opcional ProvedorReconciliavel): consulta a cada minuto e desiste
// em 30 min, bem diferente da padrão de REF.
func (p *provedorFalso) PoliticaReconciliacao(string) (PoliticaReconciliacao, bool) {
	return PoliticaReconciliacao{
		Faixas:      []FaixaReconciliacao{{IdadeAte: 30 * time.Minute, Intervalo: time.Minute}},
		IdadeMaxima: 30 * time.Minute,
	}, true
}

// Prova a arquitetura para vários provedores: um segundo provedor entra no job
// só por implementar a interface Provedor (e, se quiser, declarar a política);
// cada um tem a sua política, o seu limitador e as suas credenciais.
func TestIntegrationReconciliacaoSuportaVariosProvedoresCadaUmComSuaPolitica(t *testing.T) {
	falso := registrarProvedorFalso(t)
	a, s := novoAmbienteRecon(t)
	ctx := context.Background()

	_, idAppyPay := novaCobrancaREFRecon(t, a, "cliente-mp")

	academiaFalso := "MPF" + uuid.NewString()[:8]
	configurarFalso(t, s, academiaFalso, "tok-falso")
	if _, err := s.DefinirProvedor(ctx, ContextoAcademia, academiaFalso, provedorFalsoID, "u", "academia", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	cobrancaFalso, err := novaCobrancaREF(t, s, academiaFalso, "mp")
	if err != nil {
		t.Fatal(err)
	}
	idFalso := cobrancaFalso.ID
	emitida := a.relogio.Now()

	// Cada cobrança segue a política do SEU provedor.
	if p := a.entrada(idFalso); p.Provedor != provedorFalsoID || !p.Proxima.Equal(emitida.Add(time.Minute)) {
		t.Fatalf("o provedor falso declara 1 min: %+v", p)
	}
	if p := a.entrada(idAppyPay); p.Provedor != ProvedorAppyPay || !p.Proxima.Equal(emitida.Add(10*time.Minute)) {
		t.Fatalf("a AppyPay usa a padrão de REF (10 min): %+v", p)
	}

	// Dois minutos depois só a do provedor falso venceu; é confirmada com as
	// credenciais DELE.
	falso.mu.Lock()
	falso.statusConsulta = "Success"
	falso.mu.Unlock()
	a.relogio.Avancar(2 * time.Minute)
	if n := s.reconc.processarVencidos(ctx); n != 1 {
		t.Fatalf("processadas = %d, queria 1 (só a do provedor falso)", n)
	}
	if a.statusDaCobranca(idFalso) != "Success" || s.reconc.reg.Existe(idFalso) {
		t.Fatal("a cobrança do provedor falso deveria ser confirmada e sair do registro")
	}
	falso.mu.Lock()
	consultas := append([]string(nil), falso.consultas...)
	falso.mu.Unlock()
	if len(consultas) != 1 || !strings.HasSuffix(consultas[0], "|tok-falso") {
		t.Fatalf("o provedor falso deveria ser consultado com as suas credenciais: %v", consultas)
	}
	if !s.reconc.reg.Existe(idAppyPay) || a.transporte.totalConsultas() != 0 {
		t.Fatal("a cobrança da AppyPay ainda não venceu e não pode ser consultada")
	}
	if s.reconc.limitador(provedorFalsoID) == s.reconc.limitador(ProvedorAppyPay) {
		t.Fatal("cada provedor precisa do seu limitador de taxa, concorrência e disjuntor")
	}
}
