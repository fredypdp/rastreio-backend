package finance

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func (a *ambienteRecon) credencialAtiva(academia string) uuid.UUID {
	var id uuid.UUID
	if err := a.queryRow(`SELECT id FROM financeiro_credenciais_provedor WHERE codigo_academia=$1`, academia).Scan(&id); err != nil {
		a.t.Fatal(err)
	}
	return id
}

func (a *ambienteRecon) credencialDaCobranca(id uuid.UUID) uuid.NullUUID {
	var c uuid.NullUUID
	if err := a.queryRow(`SELECT credencial_id FROM financeiro_cobrancas WHERE id=$1`, id).Scan(&c); err != nil {
		a.t.Fatal(err)
	}
	return c
}

func (a *ambienteRecon) arquivadas(academia string) (n int, snapshot uuid.UUID, motivo string) {
	if err := a.queryRow(`SELECT COUNT(*) FROM financeiro_credenciais_arquivadas WHERE codigo_academia=$1`, academia).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	if n > 0 {
		if err := a.queryRow(`SELECT credential_id, motivo FROM financeiro_credenciais_arquivadas WHERE codigo_academia=$1 ORDER BY arquivada_em DESC LIMIT 1`, academia).Scan(&snapshot, &motivo); err != nil {
			a.t.Fatal(err)
		}
	}
	return n, snapshot, motivo
}

func (a *ambienteRecon) segredosDe(id uuid.UUID) int {
	var n int
	if err := a.queryRow(`SELECT COUNT(*) FROM financeiro_segredos_provedor WHERE credential_id=$1`, id).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n
}

func TestIntegrationEditarCredenciaisComPendentesCriaSnapshotQueConcluiAsPendentes(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	academia, id := novaCobrancaREFRecon(t, a, "cliente-antigo")
	ativa := a.credencialAtiva(academia)
	if c := a.credencialDaCobranca(id); !c.Valid || c.UUID != ativa {
		t.Fatal("a cobrança deveria registrar a credencial emitente")
	}

	view := configurarCredencialRecon(t, s, academia, "cliente-novo")
	if !view.SnapshotCriado || view.CobrancasPendentes != 1 || view.ID != ativa {
		t.Fatalf("a edição deveria arquivar o estado anterior mantendo o ID ativo: %+v", view)
	}
	n, snapshot, motivo := a.arquivadas(academia)
	if n != 1 || motivo != "editada" || snapshot == ativa {
		t.Fatalf("arquivadas=%d motivo=%q snapshot=%v", n, motivo, snapshot)
	}
	if c := a.credencialDaCobranca(id); !c.Valid || c.UUID != snapshot {
		t.Fatalf("a pendente deveria apontar para o snapshot: %+v", c)
	}
	if a.segredosDe(snapshot) == 0 {
		t.Fatal("os segredos antigos deveriam estar copiados no cofre para o snapshot")
	}

	// A consulta da pendente usa os segredos ANTIGOS.
	antes := len(a.transporte.clientes())
	a.transporte.definir("Pending", false)
	if _, err := s.ConsultCharge(context.Background(), ContextoAcademia, academia, id.String(), "u", "academia", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	usados := a.transporte.clientes()[antes:]
	if len(usados) != 1 || usados[0] != "cliente-antigo" {
		t.Fatalf("a pendente deveria consultar com o cliente antigo; token pedido para %v", usados)
	}

	// Uma cobrança NOVA usa a credencial editada (o token em cache do cliente
	// antigo não pode ser reaproveitado para o ID ativo).
	antes = len(a.transporte.clientes())
	codigo2 := seedMatriculaPendente(t, a.client, academia, 900)
	if _, err := s.IniciarPagamentoMatricula(context.Background(), MatriculaPagamentoInput{CodigoSolicitacao: codigo2, MetodoPagamento: "REF"}, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	usados = a.transporte.clientes()[antes:]
	if len(usados) != 1 || usados[0] != "cliente-novo" {
		t.Fatalf("a nova cobrança deveria usar as credenciais editadas; token pedido para %v", usados)
	}

	// Resolver a última pendente que dependia do snapshot o descarta, com os segredos.
	a.transporte.definir("Success", false)
	if _, err := s.ConsultCharge(context.Background(), ContextoAcademia, academia, id.String(), "u", "academia", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if n, _, _ := a.arquivadas(academia); n != 0 {
		t.Fatalf("o snapshot deveria ter sido descartado: %d", n)
	}
	if a.segredosDe(snapshot) != 0 {
		t.Fatal("os segredos do snapshot deveriam ter sido limpos do cofre")
	}
}

func TestIntegrationEditarCredenciaisSemPendentesNaoCriaSnapshot(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	academia := "SNP" + uuid.NewString()[:8]
	configurarCredencialRecon(t, s, academia, "cliente-1")
	view := configurarCredencialRecon(t, s, academia, "cliente-2")
	if view.SnapshotCriado || view.CobrancasPendentes != 0 {
		t.Fatalf("sem pendentes não há o que preservar: %+v", view)
	}
	if n, _, _ := a.arquivadas(academia); n != 0 {
		t.Fatalf("arquivadas = %d", n)
	}
}

func TestIntegrationEditarCredenciaisComMesmosValoresNaoCriaSnapshot(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	academia, _ := novaCobrancaREFRecon(t, a, "cliente-igual")
	view := configurarCredencialRecon(t, s, academia, "cliente-igual")
	if view.SnapshotCriado {
		t.Fatal("reenviar os mesmos valores não altera nada: não deve criar snapshot")
	}
	if view.CobrancasPendentes != 1 {
		t.Fatalf("a resposta deveria informar a pendente: %+v", view)
	}
	if n, _, _ := a.arquivadas(academia); n != 0 {
		t.Fatalf("arquivadas = %d", n)
	}
}

func TestIntegrationSegundaEdicaoNaoCriaSnapshotOrfao(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	academia, id := novaCobrancaREFRecon(t, a, "cliente-v1")
	configurarCredencialRecon(t, s, academia, "cliente-v2")
	_, primeiro, _ := a.arquivadas(academia)

	// A pendente já usa o snapshot v1: a edição v2→v3 não tem pendente dependente.
	view := configurarCredencialRecon(t, s, academia, "cliente-v3")
	if view.SnapshotCriado {
		t.Fatal("nenhuma pendente depende da credencial ativa: não deve criar outro snapshot")
	}
	if view.CobrancasPendentes != 1 {
		t.Fatalf("o aviso deveria seguir informando a pendente: %+v", view)
	}
	n, snapshot, _ := a.arquivadas(academia)
	if n != 1 || snapshot != primeiro {
		t.Fatalf("deveria continuar só o primeiro snapshot: n=%d", n)
	}
	if c := a.credencialDaCobranca(id); c.UUID != primeiro {
		t.Fatal("a pendente deve continuar no primeiro snapshot (segredos v1)")
	}
}

func TestIntegrationSnapshotPorEdicaoSobreviveAoRebuildDaProjecao(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	academia, id := novaCobrancaREFRecon(t, a, "cliente-rb1")
	// Uma segunda cobrança já PAGA antes da edição: não depende mais de credencial
	// nenhuma e não pode ser reapontada para o snapshot (nem ao vivo, nem no replay).
	codigoPaga := seedMatriculaPendente(t, a.client, academia, 900)
	viewPaga, err := s.IniciarPagamentoMatricula(context.Background(), MatriculaPagamentoInput{CodigoSolicitacao: codigoPaga, MetodoPagamento: "REF"}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	paga := viewPaga.Charge.ID
	a.transporte.definir("Success", false)
	if _, err := s.ConsultCharge(context.Background(), ContextoAcademia, academia, paga.String(), "u", "academia", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	a.transporte.definir("Pending", false)
	ativa := a.credencialAtiva(academia)

	configurarCredencialRecon(t, s, academia, "cliente-rb2")
	_, snapshot, _ := a.arquivadas(academia)
	if c := a.credencialDaCobranca(paga); c.UUID != ativa {
		t.Fatalf("ao vivo: a cobrança já paga não deve ser reapontada ao snapshot: %+v", c)
	}

	if err := s.projection.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if c := a.credencialDaCobranca(paga); c.UUID != ativa {
		t.Fatalf("no replay: a cobrança já paga não deve ser reapontada ao snapshot: %+v", c)
	}
	n, depois, motivo := a.arquivadas(academia)
	if n != 1 || depois != snapshot || motivo != "editada" {
		t.Fatalf("o replay deveria reconstruir o snapshot: n=%d id=%v motivo=%q", n, depois, motivo)
	}
	if c := a.credencialDaCobranca(id); !c.Valid || c.UUID != snapshot {
		t.Fatalf("o replay deveria religar a pendente ao snapshot: %+v", c)
	}
}

func TestIntegrationReconciliacaoDepoisDeEdicaoUsaSegredosAntigos(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	academia, id := novaCobrancaREFRecon(t, a, "cliente-job-antigo")
	configurarCredencialRecon(t, s, academia, "cliente-job-novo")
	if c := a.entrada(id).CredencialID; c == a.credencialAtiva(academia) {
		t.Fatal("o registro do job deveria apontar a pendente para o snapshot")
	}

	antes := len(a.transporte.clientes())
	a.transporte.definir("Success", false)
	a.relogio.Avancar(11 * time.Minute)
	s.reconc.processarVencidos(context.Background())
	usados := a.transporte.clientes()[antes:]
	if len(usados) != 1 || usados[0] != "cliente-job-antigo" {
		t.Fatalf("o job deveria consultar com os segredos com que a cobrança foi emitida; token pedido para %v", usados)
	}
	if a.statusDaCobranca(id) != "Success" || s.reconc.reg.Existe(id) {
		t.Fatal("a cobrança deveria ser confirmada pelo job")
	}
	if n, _, _ := a.arquivadas(academia); n != 0 {
		t.Fatal("resolvida a última pendente, o snapshot deveria ser descartado")
	}
}

// O cache de credenciais do job (TTL de horas) não pode sobreviver a uma edição:
// sem pendentes não há snapshot, e uma cobrança NOVA emitida com a credencial
// editada seria verificada com os segredos antigos que ficaram em cache.
func TestIntegrationReconciliacaoCacheDeCredenciaisNaoSobreviveAEdicao(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	academia, primeira := novaCobrancaREFRecon(t, a, "cliente-cache-1")
	a.relogio.Avancar(11 * time.Minute)
	s.reconc.processarVencidos(context.Background())
	if len(s.reconc.creds.itens) == 0 {
		t.Fatal("a verificação deveria ter colocado as credenciais em cache")
	}
	// A cobrança é resolvida: ao editar não há pendentes, logo não há snapshot.
	a.transporte.definir("Success", false)
	if _, err := s.ConsultCharge(context.Background(), ContextoAcademia, academia, primeira.String(), "u", "academia", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	// Resolver uma cobrança não pode esvaziar o cache (esvaziar a cada pagamento
	// anularia a economia de não reler o cofre).
	if len(s.reconc.creds.itens) == 0 {
		t.Fatal("uma cobrança resolvida não deve esvaziar o cache de credenciais")
	}
	view := configurarCredencialRecon(t, s, academia, "cliente-cache-2")
	if view.SnapshotCriado {
		t.Fatal("sem pendentes não há snapshot")
	}
	if n := len(s.reconc.creds.itens); n != 0 {
		t.Fatalf("editar credenciais deve descartar o cache do job (restaram %d)", n)
	}
}
