package finance

import (
	"context"
	"testing"
	"time"

	"spuri/internal/domain/aggregates"

	"github.com/google/uuid"
)

// --- política --------------------------------------------------------------

func TestPoliticaPadraoDeTodosOsMetodosEValida(t *testing.T) {
	for _, metodo := range []string{MetodoGPO, MetodoGPOQR, MetodoREF, "", "METODO_NOVO"} {
		if !politicaPadrao(metodo).valida() {
			t.Fatalf("política padrão de %q é inválida", metodo)
		}
	}
}

func TestPoliticaIntervaloPorFaixaEIdadeMaxima(t *testing.T) {
	// GPO: 2 min até 15 min; 10 min até 2 h; 1 h até 24 h; desiste depois de 24 h.
	p := politicaPadrao(MetodoGPO)
	casos := []struct {
		idade time.Duration
		quer  time.Duration
		ok    bool
	}{
		{0, 2 * time.Minute, true},
		{15 * time.Minute, 2 * time.Minute, true},
		{15*time.Minute + time.Second, 10 * time.Minute, true},
		{2 * time.Hour, 10 * time.Minute, true},
		{3 * time.Hour, time.Hour, true},
		{24 * time.Hour, time.Hour, true},
		{24*time.Hour + time.Second, 0, false},
	}
	for _, c := range casos {
		got, ok := p.IntervaloPara(c.idade)
		if ok != c.ok || got != c.quer {
			t.Errorf("IntervaloPara(%v) = %v,%v; queria %v,%v", c.idade, got, ok, c.quer, c.ok)
		}
	}
}

func TestPoliticaMaximoDeCadaMetodoEOrdemDeGrandeza(t *testing.T) {
	if politicaPadrao(MetodoGPO).IdadeMaxima != 24*time.Hour {
		t.Fatal("GPO deveria desistir em 24 h")
	}
	if politicaPadrao(MetodoGPOQR).IdadeMaxima != 30*24*time.Hour {
		t.Fatal("GPO_QR deveria desistir em 30 dias")
	}
	if politicaPadrao(MetodoREF).IdadeMaxima != 30*24*time.Hour {
		t.Fatal("REF deveria desistir em 30 dias")
	}
	if politicaPadrao("qualquer-outro").IdadeMaxima != politicaPadrao(MetodoREF).IdadeMaxima {
		t.Fatal("método desconhecido deve usar a política conservadora de REF")
	}
	// Método em minúsculas é normalizado.
	if politicaPadrao("gpo").IdadeMaxima != politicaPadrao(MetodoGPO).IdadeMaxima {
		t.Fatal("o método deve ser normalizado")
	}
}

func TestPoliticaProximaVerificacaoNuncaPassaDaIdadeMaxima(t *testing.T) {
	p := politicaPadrao(MetodoGPO)
	emitida := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)

	// Cobrança nova: próxima em 2 min.
	prox, ok := p.ProximaVerificacao(emitida, emitida)
	if !ok || !prox.Equal(emitida.Add(2*time.Minute)) {
		t.Fatalf("próxima = %v,%v", prox, ok)
	}
	// Perto do fim: 30 min antes do limite com intervalo de 1 h → última consulta no limite exato.
	base := emitida.Add(23*time.Hour + 30*time.Minute)
	prox, ok = p.ProximaVerificacao(emitida, base)
	if !ok || !prox.Equal(emitida.Add(24*time.Hour)) {
		t.Fatalf("a última consulta deve cair exatamente na idade máxima: %v,%v", prox, ok)
	}
	// Depois da última consulta (base == limite) não há mais nada a agendar.
	if _, ok = p.ProximaVerificacao(emitida, emitida.Add(24*time.Hour)); ok {
		t.Fatal("não deveria agendar depois da consulta feita na idade máxima")
	}
	if _, ok = p.ProximaVerificacao(emitida, emitida.Add(25*time.Hour)); ok {
		t.Fatal("não deveria agendar além da idade máxima")
	}
}

func TestPoliticaInvalidaOuDesligada(t *testing.T) {
	invalidas := []PoliticaReconciliacao{
		{},
		{IdadeMaxima: time.Hour},
		{Faixas: []FaixaReconciliacao{{IdadeAte: time.Hour, Intervalo: time.Minute}}},
		{IdadeMaxima: time.Hour, Faixas: []FaixaReconciliacao{{IdadeAte: time.Hour, Intervalo: 0}}},
		{IdadeMaxima: time.Hour, Faixas: []FaixaReconciliacao{{IdadeAte: time.Hour, Intervalo: time.Minute}, {IdadeAte: time.Minute, Intervalo: time.Minute}}},
	}
	for i, p := range invalidas {
		if p.valida() {
			t.Errorf("política inválida #%d aceita", i)
		}
	}
	if _, ok := (PoliticaReconciliacao{}).IntervaloPara(0); ok {
		t.Fatal("política desligada não pode consultar")
	}
}

// provedorReconciliavelTeste prova a extensão por provedor sem depender de PSP real.
type provedorReconciliavelTeste struct {
	Provedor
	pol PoliticaReconciliacao
	ok  bool
}

func (p provedorReconciliavelTeste) PoliticaReconciliacao(string) (PoliticaReconciliacao, bool) {
	return p.pol, p.ok
}

func TestPoliticaDoProvedorSubstituiAPadraoMasInvalidaCai(t *testing.T) {
	propria := PoliticaReconciliacao{Faixas: []FaixaReconciliacao{{IdadeAte: time.Hour, Intervalo: 30 * time.Second}}, IdadeMaxima: time.Hour}
	got := politicaDaCobranca(provedorReconciliavelTeste{pol: propria, ok: true}, MetodoREF)
	if got.IdadeMaxima != time.Hour {
		t.Fatalf("a política do provedor deveria valer: %+v", got)
	}
	// Provedor que declara política inválida, ou que não declara para o método, usa a padrão.
	for _, p := range []provedorReconciliavelTeste{{pol: PoliticaReconciliacao{}, ok: true}, {pol: propria, ok: false}} {
		if got := politicaDaCobranca(p, MetodoREF); got.IdadeMaxima != politicaPadrao(MetodoREF).IdadeMaxima {
			t.Fatalf("deveria cair na padrão: %+v", got)
		}
	}
	// A AppyPay (sem extensão) usa a padrão.
	if got := politicaDaCobranca(&appyPayProvider{}, MetodoGPO); got.IdadeMaxima != politicaPadrao(MetodoGPO).IdadeMaxima {
		t.Fatal("provedor sem extensão deve usar a padrão")
	}
}

func TestConfigReconciliacaoDoAmbiente(t *testing.T) {
	t.Setenv("RECONCILIACAO_ATIVA", "")
	if cfg := ConfigReconciliacaoDoAmbiente(); !cfg.Ativa || cfg.Concorrencia != 3 || cfg.TTLCredenciais != 6*time.Hour || cfg.Resync != 24*time.Hour {
		t.Fatalf("padrões inesperados: %+v", cfg)
	}
	t.Setenv("RECONCILIACAO_ATIVA", "false")
	t.Setenv("RECONCILIACAO_CONCORRENCIA", "7")
	t.Setenv("RECONCILIACAO_CHAMADAS_POR_SEGUNDO", "0.5")
	t.Setenv("RECONCILIACAO_CACHE_CREDENCIAIS_SEG", "30")
	t.Setenv("RECONCILIACAO_RESYNC_MIN", "15")
	cfg := ConfigReconciliacaoDoAmbiente()
	if cfg.Ativa || cfg.Concorrencia != 7 || cfg.ChamadasPorSegundo != 0.5 || cfg.TTLCredenciais != 30*time.Second || cfg.Resync != 15*time.Minute {
		t.Fatalf("ambiente não aplicado: %+v", cfg)
	}
	// RECONCILIACAO_RESYNC_MIN=0 desliga a recarga periódica.
	t.Setenv("RECONCILIACAO_RESYNC_MIN", "0")
	if ConfigReconciliacaoDoAmbiente().Resync != 0 {
		t.Fatal("RECONCILIACAO_RESYNC_MIN=0 deveria desligar a recarga")
	}
	// Valores inválidos mantêm o padrão.
	t.Setenv("RECONCILIACAO_CONCORRENCIA", "abc")
	t.Setenv("RECONCILIACAO_CHAMADAS_POR_SEGUNDO", "-1")
	cfg = ConfigReconciliacaoDoAmbiente()
	if cfg.Concorrencia != 3 || cfg.ChamadasPorSegundo != 2 {
		t.Fatalf("valores inválidos deveriam manter o padrão: %+v", cfg)
	}
}

// --- registro --------------------------------------------------------------

func TestRegistroVencidosMarcaEmVooENaoDevolveDuasVezes(t *testing.T) {
	r := novoRegistroPendentes()
	agora := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a, b := uuid.New(), uuid.New()
	r.Adicionar(pendenteReconciliacao{ID: a, Proxima: agora.Add(-time.Second)})
	r.Adicionar(pendenteReconciliacao{ID: b, Proxima: agora.Add(time.Hour)})

	venc := r.Vencidos(agora, 10)
	if len(venc) != 1 || venc[0].ID != a {
		t.Fatalf("vencidos = %+v", venc)
	}
	if again := r.Vencidos(agora, 10); len(again) != 0 {
		t.Fatal("uma cobrança em processamento não pode ser devolvida de novo")
	}
	// ProximaData ignora a que está em processamento.
	if prox, ok := r.ProximaData(); !ok || !prox.Equal(agora.Add(time.Hour)) {
		t.Fatalf("ProximaData = %v,%v", prox, ok)
	}
	// Concluir mantendo, com nova data, devolve-a ao ciclo.
	r.Concluir(a, func(p *pendenteReconciliacao) bool { p.Proxima = agora.Add(2 * time.Hour); return true })
	if !r.Existe(a) || r.Len() != 2 {
		t.Fatal("deveria continuar registrada")
	}
	// Concluir sem manter remove.
	r.Concluir(a, func(p *pendenteReconciliacao) bool { return false })
	if r.Existe(a) {
		t.Fatal("deveria ter saído do registro")
	}
}

func TestRegistroSemPendentesNaoTemProximaData(t *testing.T) {
	r := novoRegistroPendentes()
	if _, ok := r.ProximaData(); ok {
		t.Fatal("registro vazio não pode ter próxima data (o laço dorme sem timer)")
	}
}

func TestRegistroAdicionarRepetidoPreservaAgendaEFalhas(t *testing.T) {
	r := novoRegistroPendentes()
	id := uuid.New()
	quando := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	r.Adicionar(pendenteReconciliacao{ID: id, Proxima: quando, Falhas: 3, EfeitoPendente: true})
	r.Adicionar(pendenteReconciliacao{ID: id, Proxima: quando.Add(-time.Hour), ProviderID: "novo"})
	got, _ := r.ProximaData()
	if !got.Equal(quando) {
		t.Fatal("um evento repetido não pode antecipar a consulta")
	}
	v := r.Vencidos(quando, 1)
	if len(v) != 1 || v[0].Falhas != 3 || !v[0].EfeitoPendente || v[0].ProviderID != "novo" {
		t.Fatalf("estado inesperado: %+v", v)
	}
}

func TestRegistroRemoverSeLivreRespeitaProcessamento(t *testing.T) {
	r := novoRegistroPendentes()
	livre, emVoo := uuid.New(), uuid.New()
	agora := time.Now()
	r.Adicionar(pendenteReconciliacao{ID: livre, Proxima: agora.Add(time.Hour)})
	r.Adicionar(pendenteReconciliacao{ID: emVoo, Proxima: agora.Add(-time.Second)})
	_ = r.Vencidos(agora, 10) // emVoo passa a estar em processamento
	r.RemoverSeLivre(livre)
	r.RemoverSeLivre(emVoo)
	if r.Existe(livre) {
		t.Fatal("a livre deveria ter saído")
	}
	if !r.Existe(emVoo) {
		t.Fatal("a que o job está processando só pode ser removida pelo próprio job")
	}
}

func TestRegistroRepontarCredencialSoDoContextoEProvedor(t *testing.T) {
	r := novoRegistroPendentes()
	velha, nova := uuid.New(), uuid.New()
	mesmo := uuid.New()
	legado := uuid.New()
	outraAcademia := uuid.New()
	outroProvedor := uuid.New()
	outraCredencial := uuid.New()
	r.Adicionar(pendenteReconciliacao{ID: mesmo, Provedor: "P", Contexto: "academia", Academia: "A1", CredencialID: velha})
	r.Adicionar(pendenteReconciliacao{ID: legado, Provedor: "P", Contexto: "academia", Academia: "A1"}) // sem vínculo
	r.Adicionar(pendenteReconciliacao{ID: outraAcademia, Provedor: "P", Contexto: "academia", Academia: "A2", CredencialID: velha})
	r.Adicionar(pendenteReconciliacao{ID: outroProvedor, Provedor: "Q", Contexto: "academia", Academia: "A1", CredencialID: velha})
	r.Adicionar(pendenteReconciliacao{ID: outraCredencial, Provedor: "P", Contexto: "academia", Academia: "A1", CredencialID: uuid.New()})
	if n := r.RepontarCredencial("academia", "A1", "P", velha, nova); n != 2 {
		t.Fatalf("repontadas = %d, queria 2 (a vinculada e a legada)", n)
	}
	got := map[uuid.UUID]uuid.UUID{}
	for _, p := range r.Vencidos(time.Now().Add(time.Hour), 100) {
		got[p.ID] = p.CredencialID
	}
	if got[mesmo] != nova || got[legado] != nova {
		t.Fatal("as pendentes do contexto deveriam apontar para o snapshot")
	}
	if got[outraAcademia] != velha || got[outroProvedor] != velha {
		t.Fatal("outras academias/provedores não podem ser afetados")
	}
}

// --- limites, recuo, cache -------------------------------------------------

func TestRecuoExponencialComTeto(t *testing.T) {
	quer := []time.Duration{time.Minute, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour}
	for i, q := range quer {
		if got := recuo(i); got != q {
			t.Errorf("recuo(%d) = %v, queria %v", i, got, q)
		}
	}
}

func TestDisjuntorAbreAposErrosSeguidosEFechaNoSucesso(t *testing.T) {
	l := novoLimitadorProvedor(ConfigReconciliacaoPadrao())
	agora := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < falhasParaAbrirDisjuntor-1; i++ {
		l.registrarFalha(agora)
	}
	if l.esperaDisjuntor(agora) != 0 {
		t.Fatal("o disjuntor não deveria abrir antes do limite de falhas")
	}
	l.registrarFalha(agora)
	if got := l.esperaDisjuntor(agora); got != time.Minute {
		t.Fatalf("espera = %v, queria 1 min", got)
	}
	if l.esperaDisjuntor(agora.Add(time.Minute)) != 0 {
		t.Fatal("o disjuntor deveria fechar depois da pausa")
	}
	l.registrarFalha(agora)
	if got := l.esperaDisjuntor(agora); got != 2*time.Minute {
		t.Fatalf("a pausa deveria dobrar: %v", got)
	}
	l.registrarSucesso()
	if l.esperaDisjuntor(agora) != 0 {
		t.Fatal("um sucesso fecha o disjuntor")
	}
}

func TestDisjuntorPausaTemTetoDeQuinzeMinutos(t *testing.T) {
	l := novoLimitadorProvedor(ConfigReconciliacaoPadrao())
	agora := time.Now()
	for i := 0; i < 60; i++ {
		l.registrarFalha(agora)
	}
	if got := l.esperaDisjuntor(agora); got != 15*time.Minute {
		t.Fatalf("pausa = %v, queria o teto de 15 min", got)
	}
}

func TestLimitadorRespeitaConcorrenciaECancelamento(t *testing.T) {
	cfg := ConfigReconciliacaoPadrao()
	cfg.Concorrencia = 1
	cfg.ChamadasPorSegundo = 1000
	l := novoLimitadorProvedor(cfg)
	liberar, err := l.adquirir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancelar := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelar()
	if _, err := l.adquirir(ctx); err == nil {
		t.Fatal("a segunda aquisição deveria esperar e expirar (concorrência 1)")
	}
	liberar()
	liberar2, err := l.adquirir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	liberar2()
}

func TestCacheCredenciaisExpiraELimpa(t *testing.T) {
	c := novoCacheCredenciais()
	agora := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c.guardar("k", credencialProvedor{ID: uuid.New(), Provedor: "P"}, agora.Add(time.Minute))
	if _, ok := c.obter("k", agora); !ok {
		t.Fatal("deveria estar em cache")
	}
	if _, ok := c.obter("k", agora.Add(time.Minute)); ok {
		t.Fatal("deveria ter expirado")
	}
	c.guardar("k", credencialProvedor{}, agora.Add(time.Hour))
	c.remover("k")
	if _, ok := c.obter("k", agora); ok {
		t.Fatal("remover deveria descartar a entrada (usado quando o provedor devolve erro)")
	}
	c.guardar("k", credencialProvedor{}, agora.Add(time.Hour))
	c.limpar()
	if _, ok := c.obter("k", agora); ok {
		t.Fatal("limpar deveria esvaziar o cache")
	}
}

// --- extração do payload ---------------------------------------------------

func TestPendenteDoPayload(t *testing.T) {
	id := uuid.New()
	agora := time.Now()
	cred := uuid.New()
	base := func() map[string]any {
		return map[string]any{"provedor": "AppyPay", "provider_charge_id": "pc-1", "merchant_transaction_id": "m-1",
			"payment_method": "ref", "contexto_tipo": "academia", "codigo_academia": "A1", "credencial_id": cred.String()}
	}
	p, ok := pendenteDoPayload(id, aggregates.CobrancaCriada, base(), agora)
	if !ok || p.Metodo != MetodoREF || p.ProviderID != "pc-1" || p.CredencialID != cred || p.Academia != "A1" || !p.EmitidaEm.Equal(agora) {
		t.Fatalf("extração incorreta: %+v ok=%v", p, ok)
	}
	// QR Code vira GPO_QR por evento ou por payload.
	if p, _ := pendenteDoPayload(id, aggregates.QRCodeGerado, base(), agora); p.Metodo != MetodoGPOQR {
		t.Fatalf("QRCodeGerado deveria ser GPO_QR: %q", p.Metodo)
	}
	qr := base()
	qr["qr_code_type"] = "SINGLE"
	if p, _ := pendenteDoPayload(id, aggregates.CobrancaCriada, qr, agora); p.Metodo != MetodoGPOQR {
		t.Fatalf("payload de QR deveria ser GPO_QR: %q", p.Metodo)
	}
	// Evento legado sem provedor = AppyPay; sem credencial = uuid.Nil.
	legado := base()
	delete(legado, "provedor")
	delete(legado, "credencial_id")
	if p, ok := pendenteDoPayload(id, aggregates.CobrancaCriada, legado, agora); !ok || p.Provedor != ProvedorAppyPay || p.CredencialID != uuid.Nil {
		t.Fatalf("legado: %+v", p)
	}
	// Pagamento externo e cobrança sem id do provedor não entram.
	ext := base()
	ext["provedor"] = ProvedorExterno
	if _, ok := pendenteDoPayload(id, aggregates.CobrancaCriada, ext, agora); ok {
		t.Fatal("pagamento externo nunca é reconciliado")
	}
	sem := base()
	delete(sem, "provider_charge_id")
	if _, ok := pendenteDoPayload(id, aggregates.CobrancaCriada, sem, agora); ok {
		t.Fatal("sem id do provedor não há o que consultar")
	}
}

func TestObservarReconciliacaoSemJobAtivoEhNoOp(t *testing.T) {
	s := &Service{}
	s.reconc = novoReconciliador(s)
	s.observarReconciliacao(aggregates.CobrancaCriada, map[string]any{"provedor": "AppyPay", "provider_charge_id": "x", "status": "Pending"}, uuid.New())
	if s.ReconciliacaoPendentes() != 0 {
		t.Fatal("sem o job ativo nada deve ser registrado")
	}
	var nulo *Service
	nulo.observarReconciliacao(aggregates.CobrancaCriada, nil, uuid.New()) // não pode entrar em pânico
}
