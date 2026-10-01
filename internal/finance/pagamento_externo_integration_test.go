package finance

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"spuri/internal/db"
)

// failingTransport simula o provedor indisponível.
type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("provedor indisponível (simulado)")
}

type extFixture struct {
	client    *db.Client
	service   *Service
	transport *appyPayMockTransport
	academia  string
	estudante string
	alvo      MensalidadeMesView
}

func (f *extFixture) meses() []MensalidadeSelecaoMes {
	return []MensalidadeSelecaoMes{{AnoLetivo: f.alvo.AnoLetivo, Mes: f.alvo.Mes}}
}

func newExtFixture(t *testing.T, comCredencial bool) *extFixture {
	t.Helper()
	t.Setenv("APPYPAY_RESOURCE", "integration-resource")
	client := integrationClient(t)
	service := NewService(client)
	academia := mensalidadeCodigo()
	estudante := "EST-EXT-" + uuid.NewString()[:8]
	seedMensalidadeAcademia(t, client, academia, "private", "fundamental", "2025_2026")
	seedMensalidadeTurma(t, client, academia, "T-EXT", "2025_2026", estudante, nil)
	seedMensalidadeConfiguracao(t, client, academia, NivelFundamental, "6_ano_fundamental", nil, 1000, 7, time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC))
	if _, err := client.DB().Exec(`UPDATE financeiro_mensalidade_configuracoes SET metodos_pagamento='{GPO}' WHERE codigo_academia=$1`, academia); err != nil {
		t.Fatal(err)
	}
	f := &extFixture{client: client, service: service, transport: &appyPayMockTransport{status: "Pending"}, academia: academia, estudante: estudante}
	if comCredencial {
		configureIntegrationCredential(t, service, ContextoAcademia, academia)
		service.SetHTTPClient(&http.Client{Transport: f.transport})
	}
	todas, err := service.ListMensalidades(context.Background(), estudante, &academia)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range todas {
		if m.Estado == EstadoPendente {
			f.alvo = m
			break
		}
	}
	if f.alvo.AnoLetivo == "" {
		t.Fatal("esperava pelo menos uma mensalidade pendente")
	}
	return f
}

// abrirCobranca cria, como o estudante, uma cobrança GPO aguardando pagamento
// para o mês alvo.
func (f *extFixture) abrirCobranca(t *testing.T) uuid.UUID {
	t.Helper()
	view, err := f.service.IniciarPagamentoMensalidades(context.Background(), MensalidadePagamentoInput{
		CodigoEstudante: f.estudante, CodigoAcademia: f.academia, Meses: f.meses(), MetodoPagamento: "GPO", Telefone: "923000000",
	}, f.estudante, "estudante", "127.0.0.1")
	if err != nil {
		t.Fatalf("IniciarPagamentoMensalidades falhou: %v", err)
	}
	if view.Charge.Status != EstadoCobrancaAguardandoPagamento {
		t.Fatalf("esperava %q, obteve %q", EstadoCobrancaAguardandoPagamento, view.Charge.Status)
	}
	return view.Charge.ID
}

func (f *extFixture) estado(t *testing.T) string {
	t.Helper()
	estado, _, err := f.service.estadoObrigacao(context.Background(), f.estudante, f.academia, f.alvo.AnoLetivo, f.alvo.Mes)
	if err != nil {
		t.Fatal(err)
	}
	return estado
}

func (f *extFixture) eventosObrigacao(t *testing.T, tipo string) int {
	t.Helper()
	var n int
	if err := f.client.DB().QueryRow(`SELECT count(*) FROM financeiro_mensalidade_obrigacoes_eventos WHERE codigo_estudante=$1 AND codigo_academia=$2 AND ano_letivo=$3 AND mes=$4 AND tipo=$5`,
		f.estudante, f.academia, f.alvo.AnoLetivo, f.alvo.Mes, tipo).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *extFixture) cobranca(t *testing.T, id uuid.UUID) map[string]any {
	t.Helper()
	row, err := f.service.loadCharge(context.Background(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	return row.Payload
}

func ledgerCount(t *testing.T, client *db.Client, aggregate uuid.UUID, evento string) int {
	t.Helper()
	var n int
	if err := client.DB().QueryRow(`SELECT count(*) FROM spuri_ledger WHERE aggregate_id=$1 AND event_type=$2`, aggregate, evento).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIntegrationPagamentoExternoMensalidadeSemCobranca(t *testing.T) {
	f := newExtFixture(t, false) // sem credencial AppyPay: o pagamento externo não depende do provedor
	ctx := context.Background()

	out, err := f.service.RegistrarPagamentoExternoMensalidades(ctx, f.academia, PagamentoExternoMensalidadesInput{
		CodigoEstudante: f.estudante, Meses: f.meses(), Observacao: "Pago em numerário na secretaria", ReferenciaExterna: "REC-001",
	}, uuid.NewString(), "academia", "127.0.0.1")
	if err != nil {
		t.Fatalf("RegistrarPagamentoExternoMensalidades falhou: %v", err)
	}
	if out.Status != "Success" || out.Valor != 1000 || len(out.Meses) != 1 {
		t.Fatalf("resultado inesperado: %+v", out)
	}
	if got := f.estado(t); got != EstadoPago {
		t.Fatalf("mês deveria estar %q depois do pagamento externo, obteve %q", EstadoPago, got)
	}
	if n := f.eventosObrigacao(t, "paga"); n != 1 {
		t.Fatalf("esperava 1 evento paga, obteve %d", n)
	}
	p := f.cobranca(t, out.ID)
	if p["status"] != "Success" || p["pagamento_externo"] != true || p["payment_method"] != MetodoPagamentoExterno || p["pagamento_externo_referencia"] != "REC-001" {
		t.Fatalf("payload da cobrança inesperado: %v", p)
	}
	var chargeID string
	if err = f.client.DB().QueryRow(`SELECT charge_id::text FROM financeiro_mensalidade_obrigacoes_eventos WHERE codigo_estudante=$1 AND codigo_academia=$2 AND ano_letivo=$3 AND mes=$4 AND tipo='paga'`,
		f.estudante, f.academia, f.alvo.AnoLetivo, f.alvo.Mes).Scan(&chargeID); err != nil || chargeID != out.ID.String() {
		t.Fatalf("evento paga deveria apontar para a cobrança externa %s, obteve %q (%v)", out.ID, chargeID, err)
	}
	// aparece na listagem com a marca de pagamento externo
	lista, err := f.service.ListCobrancas(ctx, ContextoAcademia, f.academia, nil, nil, nil, nil, "", "", nil, 50, 0)
	if err != nil {
		t.Fatalf("ListCobrancas falhou: %v", err)
	}
	achou := false
	for _, c := range lista.Cobrancas {
		if c.ID == out.ID {
			achou = c.PagamentoExterno && c.Status == "Success" && c.ReferenciaExterna == "REC-001" && c.MetodoPagamento == MetodoPagamentoExterno
		}
	}
	if !achou {
		t.Fatal("a cobrança externa deveria aparecer em ListCobrancas com pagamento_externo=true")
	}
	// repetir é recusado: o mês já está pago
	if _, err = f.service.RegistrarPagamentoExternoMensalidades(ctx, f.academia, PagamentoExternoMensalidadesInput{CodigoEstudante: f.estudante, Meses: f.meses()}, uuid.NewString(), "academia", "127.0.0.1"); !errors.Is(err, ErrPagamentoExistente) {
		t.Fatalf("segundo pagamento externo do mesmo mês deveria falhar com ErrPagamentoExistente, obteve %v", err)
	}
	// regra 2: mês pago não pode ser anulado nem reativado
	in := ObrigacaoMensalidadeInput{CodigoEstudante: f.estudante, CodigoAcademia: f.academia, AnoLetivo: f.alvo.AnoLetivo, Meses: []int{f.alvo.Mes}}
	if err = f.service.AnularObrigacoesMensalidade(ctx, in, uuid.NewString(), "academia", "127.0.0.1"); err == nil {
		t.Fatal("anular mensalidade paga fora da plataforma foi aceite")
	}
	if err = f.service.ReativarObrigacoesMensalidade(ctx, in, uuid.NewString(), "academia", "127.0.0.1"); err == nil {
		t.Fatal("reativar mensalidade paga fora da plataforma foi aceite")
	}
	// o estado sobrevive a um Rebuild do ledger (a projeção conhece o evento novo)
	if err = f.service.projection.Rebuild(); err != nil {
		t.Fatalf("Rebuild() falhou: %v", err)
	}
	if got := f.estado(t); got != EstadoPago {
		t.Fatalf("após Rebuild o mês deveria continuar %q, obteve %q", EstadoPago, got)
	}
	if p = f.cobranca(t, out.ID); p["pagamento_externo"] != true {
		t.Fatalf("após Rebuild a cobrança perdeu a marca de pagamento externo: %v", p)
	}
}

func TestIntegrationPagamentoExternoMensalidadeRecusaMesComCobrancaAberta(t *testing.T) {
	f := newExtFixture(t, true)
	f.abrirCobranca(t)
	_, err := f.service.RegistrarPagamentoExternoMensalidades(context.Background(), f.academia, PagamentoExternoMensalidadesInput{CodigoEstudante: f.estudante, Meses: f.meses()}, uuid.NewString(), "academia", "127.0.0.1")
	if err == nil {
		t.Fatal("pagamento externo por mês com cobrança aberta deveria exigir marcar a cobrança")
	}
	if got := f.estado(t); got != EstadoPendente {
		t.Fatalf("mês deveria continuar pendente, obteve %q", got)
	}
}

func TestIntegrationPagamentoExternoCobrancaAbertaSegueMesmoPipeline(t *testing.T) {
	f := newExtFixture(t, true)
	ctx := context.Background()
	id := f.abrirCobranca(t)

	out, err := f.service.RegistrarPagamentoExternoCobranca(ctx, f.academia, id.String(), PagamentoExternoInput{ReferenciaExterna: "TRF-77"}, uuid.NewString(), "academia", "127.0.0.1")
	if err != nil {
		t.Fatalf("RegistrarPagamentoExternoCobranca falhou: %v", err)
	}
	if out.Status != "Success" || out.JaRegistrado {
		t.Fatalf("resultado inesperado: %+v", out)
	}
	if got := f.estado(t); got != EstadoPago {
		t.Fatalf("mês deveria estar %q, obteve %q", EstadoPago, got)
	}
	p := f.cobranca(t, id)
	if p["status"] != "Success" || p["pagamento_externo"] != true || p["status_anterior"] != EstadoCobrancaAguardandoPagamento {
		t.Fatalf("payload inesperado: %v", p)
	}
	if n := f.eventosObrigacao(t, "paga"); n != 1 {
		t.Fatalf("esperava 1 evento paga, obteve %d", n)
	}

	// idempotente: repetir não grava outro evento nem outra confirmação
	again, err := f.service.RegistrarPagamentoExternoCobranca(ctx, f.academia, id.String(), PagamentoExternoInput{}, uuid.NewString(), "academia", "127.0.0.1")
	if err != nil || !again.JaRegistrado {
		t.Fatalf("repetição deveria devolver JaRegistrado=true sem erro: %+v %v", again, err)
	}
	if n := ledgerCount(t, f.client, id, "CobrancaPagamentoExternoRegistrado"); n != 1 {
		t.Fatalf("esperava exatamente 1 CobrancaPagamentoExternoRegistrado no ledger, obteve %d", n)
	}
	if n := ledgerCount(t, f.client, id, "MensalidadesCobrancaConfirmada"); n != 1 {
		t.Fatalf("esperava exatamente 1 MensalidadesCobrancaConfirmada no ledger, obteve %d", n)
	}

	// o provedor continua a dizer Pending: uma consulta NÃO pode rebaixar a cobrança
	consultada, err := f.service.ConsultCharge(ctx, ContextoAcademia, f.academia, id.String(), uuid.NewString(), "academia", "127.0.0.1")
	if err != nil || consultada.Status != "Success" {
		t.Fatalf("consulta deveria manter Success: %+v %v", consultada, err)
	}
	if p = f.cobranca(t, id); p["status"] != "Success" {
		t.Fatalf("consulta rebaixou a cobrança paga externamente: %v", p["status"])
	}
	// e a cobrança paga não pode ser cancelada
	if _, err = f.service.CancelCharge(ctx, ContextoAcademia, f.academia, id.String(), "tentativa", uuid.NewString(), "academia", "127.0.0.1"); !errors.Is(err, ErrPagamentoExistente) {
		t.Fatalf("cancelar cobrança paga deveria falhar com ErrPagamentoExistente, obteve %v", err)
	}
}

func TestIntegrationPagamentoExternoCobrancaJaPagaNoProvedorBloqueiaEReconcilia(t *testing.T) {
	f := newExtFixture(t, true)
	ctx := context.Background()
	id := f.abrirCobranca(t)
	f.transport.status = "Success" // o pagador pagou na AppyPay; o webhook ainda não chegou

	_, err := f.service.RegistrarPagamentoExternoCobranca(ctx, f.academia, id.String(), PagamentoExternoInput{}, uuid.NewString(), "academia", "127.0.0.1")
	if !errors.Is(err, ErrPagamentoExistente) {
		t.Fatalf("esperava ErrPagamentoExistente, obteve %v", err)
	}
	p := f.cobranca(t, id)
	if p["status"] != "Success" || p["pagamento_externo"] == true {
		t.Fatalf("a cobrança deveria ficar Success pelo provedor, sem marca de pagamento externo: %v", p)
	}
	if got := f.estado(t); got != EstadoPago {
		t.Fatalf("o pagamento descoberto deveria ter confirmado a mensalidade, obteve %q", got)
	}
	if n := ledgerCount(t, f.client, id, "CobrancaPagamentoExternoRegistrado"); n != 0 {
		t.Fatalf("não deveria existir evento de pagamento externo, obteve %d", n)
	}
}

func TestIntegrationPagamentoExternoCobrancaBloqueiaQuandoProvedorIndisponivel(t *testing.T) {
	f := newExtFixture(t, true)
	id := f.abrirCobranca(t)
	f.service.SetHTTPClient(&http.Client{Transport: failingTransport{}})

	_, err := f.service.RegistrarPagamentoExternoCobranca(context.Background(), f.academia, id.String(), PagamentoExternoInput{}, uuid.NewString(), "academia", "127.0.0.1")
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("sem confirmar com o provedor deveria falhar com ErrUpstream, obteve %v", err)
	}
	if n := ledgerCount(t, f.client, id, "CobrancaPagamentoExternoRegistrado"); n != 0 {
		t.Fatalf("nada deveria ter sido gravado, obteve %d eventos", n)
	}
}

func TestIntegrationPagamentoExternoCobrancaRecusaOutraAcademia(t *testing.T) {
	f := newExtFixture(t, true)
	id := f.abrirCobranca(t)
	_, err := f.service.RegistrarPagamentoExternoCobranca(context.Background(), "OUTRA-ACAD", id.String(), PagamentoExternoInput{}, uuid.NewString(), "academia", "127.0.0.1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("outra academia deveria receber ErrNotFound, obteve %v", err)
	}
	if _, err = f.service.RegistrarPagamentoExternoCobranca(context.Background(), f.academia, id.String(), PagamentoExternoInput{}, uuid.NewString(), "admin", "127.0.0.1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ator que não é academia deveria receber ErrNotFound, obteve %v", err)
	}
}

// Regra 2 — o pagador pagou no provedor mas a confirmação ainda não chegou:
// anular NÃO pode gravar a anulação (antes da correção ela era gravada antes
// de o cancelamento descobrir o pagamento).
func TestIntegrationAnularBloqueadoQuandoPagamentoAguardaConfirmacao(t *testing.T) {
	f := newExtFixture(t, true)
	ctx := context.Background()
	f.abrirCobranca(t)
	f.transport.status = "Success"

	in := ObrigacaoMensalidadeInput{CodigoEstudante: f.estudante, CodigoAcademia: f.academia, AnoLetivo: f.alvo.AnoLetivo, Meses: []int{f.alvo.Mes}}
	err := f.service.AnularObrigacoesMensalidade(ctx, in, uuid.NewString(), "academia", "127.0.0.1")
	if !errors.Is(err, ErrPagamentoExistente) {
		t.Fatalf("esperava ErrPagamentoExistente, obteve %v", err)
	}
	if n := f.eventosObrigacao(t, "anulada"); n != 0 {
		t.Fatalf("nenhuma anulação deveria ter sido gravada, obteve %d", n)
	}
	if got := f.estado(t); got != EstadoPago {
		t.Fatalf("o pagamento descoberto deveria ter confirmado a mensalidade, obteve %q", got)
	}
}

func TestIntegrationAnularCancelaCobrancaAbertaENaoPagaEReativaDepois(t *testing.T) {
	f := newExtFixture(t, true)
	ctx := context.Background()
	id := f.abrirCobranca(t) // continua Pending no provedor: ninguém pagou

	in := ObrigacaoMensalidadeInput{CodigoEstudante: f.estudante, CodigoAcademia: f.academia, AnoLetivo: f.alvo.AnoLetivo, Meses: []int{f.alvo.Mes}}
	if err := f.service.AnularObrigacoesMensalidade(ctx, in, uuid.NewString(), "academia", "127.0.0.1"); err != nil {
		t.Fatalf("anular mês nunca pago deveria funcionar: %v", err)
	}
	if got := f.estado(t); got != EstadoAnulado {
		t.Fatalf("esperava %q, obteve %q", EstadoAnulado, got)
	}
	if p := f.cobranca(t, id); p["status"] != "cancelada" {
		t.Fatalf("a cobrança aberta deveria ter sido cancelada, status=%v", p["status"])
	}
	if err := f.service.ReativarObrigacoesMensalidade(ctx, in, uuid.NewString(), "academia", "127.0.0.1"); err != nil {
		t.Fatalf("reativar mês anulado nunca pago deveria funcionar: %v", err)
	}
	if got := f.estado(t); got != EstadoPendente {
		t.Fatalf("esperava %q, obteve %q", EstadoPendente, got)
	}
}

func TestIntegrationAnularBloqueadoQuandoProvedorIndisponivel(t *testing.T) {
	f := newExtFixture(t, true)
	f.abrirCobranca(t)
	f.service.SetHTTPClient(&http.Client{Transport: failingTransport{}})

	in := ObrigacaoMensalidadeInput{CodigoEstudante: f.estudante, CodigoAcademia: f.academia, AnoLetivo: f.alvo.AnoLetivo, Meses: []int{f.alvo.Mes}}
	err := f.service.AnularObrigacoesMensalidade(context.Background(), in, uuid.NewString(), "academia", "127.0.0.1")
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("sem confirmar com o provedor deveria falhar com ErrUpstream, obteve %v", err)
	}
	if n := f.eventosObrigacao(t, "anulada"); n != 0 {
		t.Fatalf("nenhuma anulação deveria ter sido gravada, obteve %d", n)
	}
}

// Um Success tardio depois do cancelamento local (conflito) significa que a
// mensalidade foi paga: reativar deixa de ser permitido.
func TestIntegrationReativarBloqueadoAposPagamentoTardioDoProvedor(t *testing.T) {
	f := newExtFixture(t, true)
	ctx := context.Background()
	id := f.abrirCobranca(t)
	if _, err := f.service.CancelCharge(ctx, ContextoAcademia, f.academia, id.String(), "cancelada pela academia", uuid.NewString(), "academia", "127.0.0.1"); err != nil {
		t.Fatalf("CancelCharge falhou: %v", err)
	}
	in := ObrigacaoMensalidadeInput{CodigoEstudante: f.estudante, CodigoAcademia: f.academia, AnoLetivo: f.alvo.AnoLetivo, Meses: []int{f.alvo.Mes}}
	if err := f.service.AnularObrigacoesMensalidade(ctx, in, uuid.NewString(), "academia", "127.0.0.1"); err != nil {
		t.Fatalf("anular falhou: %v", err)
	}
	f.transport.status = "Success" // pagamento tardio no provedor
	if _, err := f.service.ConsultCharge(ctx, ContextoAcademia, f.academia, id.String(), uuid.NewString(), "academia", "127.0.0.1"); err != nil {
		t.Fatalf("ConsultCharge falhou: %v", err)
	}
	if n := ledgerCount(t, f.client, id, "CobrancaAppyPayConflitoPosCancelamento"); n != 1 {
		t.Fatalf("esperava 1 conflito pós-cancelamento, obteve %d", n)
	}
	err := f.service.ReativarObrigacoesMensalidade(ctx, in, uuid.NewString(), "academia", "127.0.0.1")
	if !errors.Is(err, ErrPagamentoExistente) {
		t.Fatalf("reativar após pagamento tardio deveria falhar com ErrPagamentoExistente, obteve %v", err)
	}
	if got := f.estado(t); got != EstadoAnulado {
		t.Fatalf("o mês deveria continuar %q, obteve %q", EstadoAnulado, got)
	}
}

func TestIntegrationCancelarCobrancaDescobreSuccessEReconcilia(t *testing.T) {
	f := newExtFixture(t, true)
	ctx := context.Background()
	id := f.abrirCobranca(t)
	f.transport.status = "Success"

	_, err := f.service.CancelCharge(ctx, ContextoAcademia, f.academia, id.String(), "tentativa", uuid.NewString(), "academia", "127.0.0.1")
	if !errors.Is(err, ErrPagamentoExistente) {
		t.Fatalf("esperava ErrPagamentoExistente, obteve %v", err)
	}
	if got := f.estado(t); got != EstadoPago {
		t.Fatalf("o pagamento descoberto no cancelamento deveria confirmar a mensalidade, obteve %q", got)
	}
}

// Serviço extra: mesmas regras, usando cobranças sintéticas (não é preciso
// montar uma inscrição completa porque as regras vivem no serviço financeiro).
func seedCobrancaServicoExtra(t *testing.T, f *extFixture, inscricao string, ano, mes int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	payload := map[string]any{
		"charge_id": id.String(), "contexto_tipo": ContextoAcademia, "codigo_academia": f.academia,
		"codigo_inscricao_servico": inscricao, "tipo_lancamento_servico_extra": "mensalidade",
		"ano_referencia": ano, "mes_referencia": mes, "amount": 500.0, "currency": "AOA",
		"merchant_transaction_id": merchantExterno(id), "payment_method": "GPO", "provider_charge_id": "",
		"status": EstadoCobrancaAguardandoPagamento,
	}
	if err := f.service.record(context.Background(), id, "CobrancaAppyPaySolicitada", payload, "teste", "sistema", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	return id
}

func contaEventosServicoExtra(t *testing.T, f *extFixture, inscricao, tipo string) int {
	t.Helper()
	var n int
	if err := f.client.DB().QueryRow(`SELECT count(*) FROM financeiro_servico_extra_obrigacoes_eventos WHERE solicitacao_id=$1 AND tipo=$2`, inscricao, tipo).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIntegrationServicoExtraAnularBloqueadoQuandoPagamentoAguardaConfirmacao(t *testing.T) {
	f := newExtFixture(t, true)
	ctx := context.Background()
	inscricao := uuid.NewString()
	seedCobrancaServicoExtra(t, f, inscricao, 2026, 3)
	f.transport.status = "Success"

	err := f.service.AnularObrigacaoServicoExtra(ctx, inscricao, "mensalidade", 2026, 3, "teste", uuid.NewString(), "academia", "127.0.0.1")
	if !errors.Is(err, ErrPagamentoExistente) {
		t.Fatalf("esperava ErrPagamentoExistente, obteve %v", err)
	}
	if n := contaEventosServicoExtra(t, f, inscricao, "anulada"); n != 0 {
		t.Fatalf("nenhuma anulação deveria ter sido gravada, obteve %d", n)
	}
	if n := contaEventosServicoExtra(t, f, inscricao, "paga"); n != 1 {
		t.Fatalf("o pagamento descoberto deveria ter gravado 1 lançamento pago, obteve %d", n)
	}
	// repetir a confirmação é idempotente (não duplica o evento)
	if err = f.service.ConfirmarLancamentoServicoExtraPago(ctx, inscricao, "mensalidade", 2026, 3, "teste", "sistema", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if n := contaEventosServicoExtra(t, f, inscricao, "paga"); n != 1 {
		t.Fatalf("confirmação repetida não deveria duplicar o evento, obteve %d", n)
	}
	if err = f.service.AnularObrigacaoServicoExtra(ctx, inscricao, "mensalidade", 2026, 3, "teste", uuid.NewString(), "academia", "127.0.0.1"); !errors.Is(err, ErrPagamentoExistente) {
		t.Fatalf("anular lançamento já pago deveria falhar com ErrPagamentoExistente, obteve %v", err)
	}
}

func TestIntegrationServicoExtraAnularCancelaCobrancaAbertaNaoPaga(t *testing.T) {
	f := newExtFixture(t, true)
	ctx := context.Background()
	inscricao := uuid.NewString()
	id := seedCobrancaServicoExtra(t, f, inscricao, 2026, 4) // provedor: Pending

	if err := f.service.AnularObrigacaoServicoExtra(ctx, inscricao, "mensalidade", 2026, 4, "teste", uuid.NewString(), "academia", "127.0.0.1"); err != nil {
		t.Fatalf("anular lançamento nunca pago deveria funcionar: %v", err)
	}
	if p := f.cobranca(t, id); p["status"] != "cancelada" {
		t.Fatalf("a cobrança aberta deveria ter sido cancelada, status=%v", p["status"])
	}
	if n := contaEventosServicoExtra(t, f, inscricao, "anulada"); n != 1 {
		t.Fatalf("esperava 1 anulação, obteve %d", n)
	}
	if err := f.service.ReativarObrigacaoServicoExtra(ctx, inscricao, "mensalidade", 2026, 4, "teste", uuid.NewString(), "academia", "127.0.0.1"); err != nil {
		t.Fatalf("reativar lançamento anulado nunca pago deveria funcionar: %v", err)
	}
}

// Um webhook "Success" tardio, com a consulta ao vivo ainda "Pending" (o
// provedor nunca viu o pagamento), não pode rebaixar uma cobrança que a
// academia marcou como paga fora da plataforma.
func TestIntegrationWebhookNaoRebaixaCobrancaPagaExternamente(t *testing.T) {
	f := newExtFixture(t, true)
	ctx := context.Background()
	id := f.abrirCobranca(t)
	row, err := f.service.loadCharge(ctx, id.String())
	if err != nil || row.ProviderID == "" {
		t.Fatalf("a cobrança deveria ter provider id: %+v %v", row, err)
	}
	if _, err = f.service.RegistrarPagamentoExternoCobranca(ctx, f.academia, id.String(), PagamentoExternoInput{}, uuid.NewString(), "academia", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	owner := WebhookOwner{CredentialID: uuid.New(), ContextoTipo: ContextoAcademia, CodigoAcademia: f.academia}
	payload := map[string]any{"id": row.ProviderID, "responseStatus": map[string]any{"successful": true, "status": "Success", "source": "GPO"}}
	if _, _, err = f.service.AcceptWebhook(ctx, "GPO", row.ProviderID, owner, payload); err != nil {
		t.Fatalf("AcceptWebhook falhou: %v", err)
	}
	p := f.cobranca(t, id)
	if p["status"] != "Success" || p["pagamento_externo"] != true {
		t.Fatalf("o webhook rebaixou/alterou a cobrança paga externamente: status=%v externo=%v", p["status"], p["pagamento_externo"])
	}
	if n := ledgerCount(t, f.client, id, "MensalidadesCobrancaConfirmada"); n != 1 {
		t.Fatalf("esperava 1 confirmação, obteve %d", n)
	}
}
