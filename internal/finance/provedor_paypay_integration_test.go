package finance

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Estes testes usam o serviço real (PostgreSQL, ledger, projeções) e trocam
// SÓ a rede: o PayPay é o servidor simulado de provedor_paypay_test.go, que
// verifica assinatura e decifra o biz_content de cada pedido.

const hostPayPayDeTeste = "paypay.test"

// transporteMisto encaminha os pedidos ao PayPay simulado ou ao mock da
// AppyPay, conforme o servidor de destino.
type transporteMisto struct {
	paypay  *paypayServidorFalso
	appypay http.RoundTripper
}

func (t transporteMisto) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == hostPayPayDeTeste {
		return t.paypay.RoundTrip(req)
	}
	return t.appypay.RoundTrip(req)
}

func ambientePayPay(t *testing.T) {
	t.Helper()
	provedorEnv(t)
	t.Setenv("PAYPAY_API_URL", "https://"+hostPayPayDeTeste+"/gateway")
}

func configurarPayPay(t *testing.T, s *Service, academia string) uuid.UUID {
	t.Helper()
	_, plataforma, _ := paypayChaves(t)
	view, secret, err := s.ConfigureCredential(context.Background(), nil, CredentialInput{ContextoTipo: ContextoAcademia, CodigoAcademia: academia, Provedor: ProvedorPayPay, Credenciais: credenciaisPayPayDeTeste(t, plataforma)}, "teste", "sistema", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if secret != "" {
		t.Fatalf("o PayPay não usa segredo de webhook, mas veio %q", secret)
	}
	return view.ID
}

func novaCobrancaPayPay(t *testing.T, s *Service, academia string, valor float64, metodo string) ChargeResult {
	t.Helper()
	info := map[string]any{}
	if metodo == "GPO" {
		info["phoneNumber"] = "923456789"
	}
	r, err := s.CreateCharge(context.Background(), ChargeRequest{ContextoTipo: ContextoAcademia, CodigoAcademia: academia, Amount: valor, Description: "Teste PayPay", PaymentMethod: metodo, PaymentInfo: info, MerchantTransactionID: integrationMerchant("PP")}, "teste", "sistema", "41.223.10.5")
	if err != nil {
		t.Fatalf("CreateCharge PayPay: %v", err)
	}
	return r
}

func estadoDaCobranca(t *testing.T, s *Service, id uuid.UUID) string {
	t.Helper()
	row, err := s.loadChargeByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return row.Status
}

// enviarNotificacaoPayPay faz o que o handler público faz: autentica pela
// assinatura, interpreta e aceita.
func enviarNotificacaoPayPay(t *testing.T, s *Service, corpo []byte) (accepted, confirmed bool, err error) {
	t.Helper()
	ctx := context.Background()
	owner, err := s.AuthenticateWebhook(ctx, ProvedorPayPay, http.Header{}, corpo)
	if err != nil {
		return false, false, err
	}
	prov, _ := s.ProvedorPorID(ProvedorPayPay)
	id, payload, err := prov.InterpretarWebhook("application/x-www-form-urlencoded", corpo)
	if err != nil {
		return false, false, err
	}
	return s.AcceptWebhook(ctx, ProvedorPayPay, MetodoQualquer, id, owner, payload)
}

func TestIntegrationPayPayCicloDeVidaDaCobrancaREFePorWebhook(t *testing.T) {
	ambientePayPay(t)
	client := integrationClient(t)
	servidor := novoPayPayServidorFalso(t)
	service := NewService(client)
	service.SetHTTPClient(&http.Client{Transport: servidor})
	academia := "PPY" + uuid.NewString()[:8]
	configurarPayPay(t, service, academia)
	_, plataforma, _ := paypayChaves(t)

	// As primeiras credenciais do contexto tornam o PayPay o provedor ativo.
	if ativo, err := service.ProvedorAtivo(context.Background(), ContextoAcademia, academia); err != nil || ativo != ProvedorPayPay {
		t.Fatalf("ativo = %q %v", ativo, err)
	}
	charge := novaCobrancaPayPay(t, service, academia, 1500, "REF")
	if charge.Provedor != ProvedorPayPay || charge.Status != EstadoCobrancaAguardandoPagamento || charge.ProviderChargeID != "PP"+charge.MerchantTransactionID {
		t.Fatalf("cobrança = %+v", charge)
	}
	if charge.Response["reference_id"] == nil || charge.Response["entity_id"] != "00123" {
		t.Fatalf("o pagador precisa de referência e entidade: %#v", charge.Response)
	}
	if _, ok := charge.Response["trade_token"]; ok {
		t.Fatal("trade_token não pode chegar ao chamador")
	}
	// O IP de quem pediu o pagamento chegou ao PayPay.
	if c := servidor.chamadasDe("instant_trade"); len(c) != 1 || c[0].Biz["payer_ip"] != "41.223.10.5" {
		t.Fatalf("pedido ao PayPay = %+v", c)
	}
	// Nada sensível ficou no ledger/projeção (o response guardado já vem sem token).
	var guardado string
	if err := client.DB().QueryRow(`SELECT payload::text FROM financeiro_cobrancas WHERE id=$1`, charge.ID).Scan(&guardado); err != nil {
		t.Fatal(err)
	}
	for _, proibido := range []string{"token-secreto", "dynamic_link", "BEGIN"} {
		if strings.Contains(guardado, proibido) {
			t.Fatalf("payload guardado contém %q", proibido)
		}
	}

	// 1) TRADE_SUCCESS é só aviso: aceite e registado, mas a cobrança não muda.
	aviso := paypayNotificacao(t, plataforma, camposNotificacao(charge.MerchantTransactionID, "TRADE_SUCCESS", "N-"+uuid.NewString()[:8]))
	accepted, confirmed, err := enviarNotificacaoPayPay(t, service, aviso)
	if err != nil || !accepted || confirmed {
		t.Fatalf("TRADE_SUCCESS: accepted=%v confirmed=%v err=%v", accepted, confirmed, err)
	}
	if estado := estadoDaCobranca(t, service, charge.ID); estado != EstadoCobrancaAguardandoPagamento {
		t.Fatalf("TRADE_SUCCESS não pode quitar: estado = %s", estado)
	}
	// A mesma notificação repetida pelo PayPay (mesmo notify_id) é descartada.
	if accepted, _, err = enviarNotificacaoPayPay(t, service, aviso); err != nil || accepted {
		t.Fatalf("repetição deveria ser descartada: accepted=%v err=%v", accepted, err)
	}

	// 2) TRADE_FINISHED assinado, mas a consulta ao vivo ainda diz "aguardando":
	// a dupla verificação impede a confirmação.
	finalizada := paypayNotificacao(t, plataforma, camposNotificacao(charge.MerchantTransactionID, "TRADE_FINISHED", "N-"+uuid.NewString()[:8]))
	accepted, confirmed, err = enviarNotificacaoPayPay(t, service, finalizada)
	if err != nil || !accepted || confirmed {
		t.Fatalf("TRADE_FINISHED sem confirmação ao vivo: accepted=%v confirmed=%v err=%v", accepted, confirmed, err)
	}
	if estado := estadoDaCobranca(t, service, charge.ID); estado == "Success" {
		t.Fatal("uma notificação sozinha não pode quitar sem a consulta ao vivo concordar")
	}

	// 3) O PayPay passa a reportar o pedido como liquidado: a notificação (outro
	// notify_id, mesma cobrança — não pode ser descartada como "repetida da
	// anterior") confirma o pagamento.
	servidor.definirEstado(charge.MerchantTransactionID, "TRADE_FINISHED")
	confirmacao := paypayNotificacao(t, plataforma, camposNotificacao(charge.MerchantTransactionID, "TRADE_FINISHED", "N-"+uuid.NewString()[:8]))
	accepted, confirmed, err = enviarNotificacaoPayPay(t, service, confirmacao)
	if err != nil || !accepted || !confirmed {
		t.Fatalf("TRADE_FINISHED confirmado: accepted=%v confirmed=%v err=%v", accepted, confirmed, err)
	}
	if estado := estadoDaCobranca(t, service, charge.ID); estado != "Success" {
		t.Fatalf("estado = %s, queria Success", estado)
	}
	// Uma cobrança paga já não se cancela.
	if _, err = service.CancelCharge(context.Background(), ContextoAcademia, academia, charge.ID.String(), "", "u", "academia", "ip"); !errors.Is(err, ErrPagamentoExistente) {
		t.Fatalf("cancelar cobrança paga: %v", err)
	}
	if len(servidor.chamadasDe("trade_close")) != 0 {
		t.Fatal("nunca se fecha no provedor uma cobrança já paga")
	}
}

func TestIntegrationPayPayExpressConsultaConfirmaEFalhaTerminal(t *testing.T) {
	ambientePayPay(t)
	client := integrationClient(t)
	servidor := novoPayPayServidorFalso(t)
	service := NewService(client)
	service.SetHTTPClient(&http.Client{Transport: servidor})
	academia := "PPE" + uuid.NewString()[:8]
	configurarPayPay(t, service, academia)
	ctx := context.Background()

	pago := novaCobrancaPayPay(t, service, academia, 800, "GPO")
	if c := servidor.chamadasDe("instant_trade"); c[len(c)-1].Biz["pay_method"].(map[string]any)["bank_code"] != "MUL" {
		t.Fatalf("Express deve usar MUL: %#v", c[len(c)-1].Biz)
	}
	servidor.definirEstado(pago.MerchantTransactionID, "TRADE_FINISHED")
	r, err := service.ConsultCharge(ctx, ContextoAcademia, academia, pago.ID.String(), "u", "academia", "ip")
	if err != nil || r.Status != "Success" {
		t.Fatalf("consulta paga: %+v %v", r, err)
	}

	// Pedido fechado pelo PayPay por falta de pagamento: terminal (Expired).
	expirada := novaCobrancaPayPay(t, service, academia, 2000, "REF")
	servidor.definirEstado(expirada.MerchantTransactionID, "TRADE_CLOSED")
	if r, err = service.ConsultCharge(ctx, ContextoAcademia, academia, expirada.ID.String(), "u", "academia", "ip"); err != nil || r.Status != "Expired" {
		t.Fatalf("consulta fechada: %+v %v", r, err)
	}
	if !isTerminalChargeStatus(r.Status) {
		t.Fatal("Expired é terminal")
	}

	// Uma consulta que o PayPay recusa não muda nada.
	pendente := novaCobrancaPayPay(t, service, academia, 2500, "REF")
	servidor.erros["trade_query"] = "E9999"
	if _, err = service.ConsultCharge(ctx, ContextoAcademia, academia, pendente.ID.String(), "u", "academia", "ip"); !errors.Is(err, ErrUpstream) {
		t.Fatalf("consulta recusada: %v", err)
	}
	if estado := estadoDaCobranca(t, service, pendente.ID); estado != EstadoCobrancaAguardandoPagamento {
		t.Fatalf("estado mudou para %s depois de uma consulta recusada", estado)
	}
}

func TestIntegrationPayPayCriacaoRecusadaGravaFalhaComMotivoDoProvedor(t *testing.T) {
	ambientePayPay(t)
	client := integrationClient(t)
	servidor := novoPayPayServidorFalso(t)
	service := NewService(client)
	service.SetHTTPClient(&http.Client{Transport: servidor})
	academia := "PPF" + uuid.NewString()[:8]
	configurarPayPay(t, service, academia)

	// Limites de pedido: recusados ANTES de qualquer registo e sem chamar o PayPay.
	_, err := service.CreateCharge(context.Background(), ChargeRequest{ContextoTipo: ContextoAcademia, CodigoAcademia: academia, Amount: 1000, Currency: "AOA", Description: "x", PaymentMethod: "REF"}, "u", "sistema", "ip")
	if err == nil || !strings.Contains(err.Error(), "acima de 1000") {
		t.Fatalf("REF de 1000 Kz: %v", err)
	}
	if _, err = service.CreateCharge(context.Background(), ChargeRequest{ContextoTipo: ContextoAcademia, CodigoAcademia: academia, Amount: 500, Currency: "AOA", Description: "x", PaymentMethod: "GPO", PaymentInfo: map[string]any{"phoneNumber": "123"}}, "u", "sistema", "ip"); err == nil {
		t.Fatal("telefone inválido deveria ser recusado")
	}
	if len(servidor.chamadasDe("instant_trade")) != 0 {
		t.Fatal("pedido impossível chegou ao PayPay")
	}
	var fantasmas int
	_ = client.DB().QueryRow(`SELECT COUNT(*) FROM financeiro_cobrancas WHERE codigo_academia=$1`, academia).Scan(&fantasmas)
	if fantasmas != 0 {
		t.Fatalf("pedidos recusados na validação deixaram %d cobranças fantasma", fantasmas)
	}
	// GPO_QR não é suportado pelo PayPay (recusado pelo núcleo, antes do provedor).
	if _, err = service.CreateGPOQRCode(context.Background(), QRCodeRequest{ContextoTipo: ContextoAcademia, CodigoAcademia: academia, Amount: 500, Currency: "AOA", Description: "x"}, "u", "sistema", "ip"); !errors.Is(err, ErrMetodoIndisponivel) {
		t.Fatalf("GPO_QR no PayPay: %v", err)
	}

	// O PayPay recusa o pedido: a cobrança fica Failed com o motivo dele.
	servidor.erros["instant_trade"] = "E0007"
	r, err := service.CreateCharge(context.Background(), ChargeRequest{ContextoTipo: ContextoAcademia, CodigoAcademia: academia, Amount: 5000, Currency: "AOA", Description: "x", PaymentMethod: "REF"}, "u", "sistema", "ip")
	if err != nil {
		t.Fatalf("recusa do PayPay não é erro de transporte: %v", err)
	}
	if r.Status != "Failed" || r.MensagemProvedor != "erro simulado do PayPay" || r.FonteProvedor != "E0007.SUB" || r.ProviderChargeID != "" {
		t.Fatalf("resultado = %+v", r)
	}
}

func TestIntegrationPayPayWebhookEAtribuidoAContaQueEmitiuACobranca(t *testing.T) {
	ambientePayPay(t)
	client := integrationClient(t)
	servidor := novoPayPayServidorFalso(t)
	service := NewService(client)
	service.SetHTTPClient(&http.Client{Transport: servidor})
	_, plataforma, outra := paypayChaves(t)
	a, b := "PWA"+uuid.NewString()[:8], "PWB"+uuid.NewString()[:8]
	credA, credB := configurarPayPay(t, service, a), configurarPayPay(t, service, b)
	// A chave pública do PayPay é a MESMA nas duas contas: a assinatura verifica
	// com qualquer uma. Só a cobrança diz de quem é a notificação.
	cobrancaA, cobrancaB := novaCobrancaPayPay(t, service, a, 3000, "REF"), novaCobrancaPayPay(t, service, b, 3000, "REF")
	ctx := context.Background()
	corpo := func(c ChargeResult) []byte {
		return paypayNotificacao(t, plataforma, camposNotificacao(c.MerchantTransactionID, "TRADE_FINISHED", "N-"+uuid.NewString()[:8]))
	}
	donoA, err := service.AuthenticateWebhook(ctx, ProvedorPayPay, http.Header{}, corpo(cobrancaA))
	if err != nil || donoA.CredentialID != credA || donoA.CodigoAcademia != a {
		t.Fatalf("dono da cobrança A = %+v %v (queria a credencial %s)", donoA, err, credA)
	}
	donoB, err := service.AuthenticateWebhook(ctx, ProvedorPayPay, http.Header{}, corpo(cobrancaB))
	if err != nil || donoB.CredentialID != credB || donoB.CodigoAcademia != b {
		t.Fatalf("dono da cobrança B = %+v %v", donoB, err)
	}
	// Assinada por outra chave: ninguém a aceita.
	falsa := paypayNotificacao(t, outra, camposNotificacao(cobrancaA.MerchantTransactionID, "TRADE_FINISHED", "N-falsa"))
	if _, err = service.AuthenticateWebhook(ctx, ProvedorPayPay, http.Header{}, falsa); err == nil {
		t.Fatal("notificação assinada por outra chave foi aceite")
	}
	// Corpo adulterado depois de assinado.
	adulterada := strings.Replace(string(corpo(cobrancaA)), "TRADE_FINISHED", "TRADE_CLOSED", 1)
	if _, err = service.AuthenticateWebhook(ctx, ProvedorPayPay, http.Header{}, []byte(adulterada)); err == nil {
		t.Fatal("notificação adulterada foi aceite")
	}
	// Um webhook da AppyPay (cabeçalho com segredo) não autentica no PayPay.
	if _, err = service.AuthenticateWebhook(ctx, ProvedorPayPay, http.Header{WebhookHeaderName: []string{"qualquer-segredo"}}, nil); err == nil {
		t.Fatal("sem corpo assinado não há autenticação")
	}
	// Notificação para um pedido desconhecido, bem assinada: autentica (a
	// assinatura é válida) mas não altera nenhuma cobrança.
	desconhecido := paypayNotificacao(t, plataforma, camposNotificacao("TNAOEXISTE001", "TRADE_FINISHED", "N-"+uuid.NewString()[:8]))
	accepted, confirmed, err := enviarNotificacaoPayPay(t, service, desconhecido)
	if err != nil || !accepted || confirmed {
		t.Fatalf("pedido desconhecido: accepted=%v confirmed=%v err=%v", accepted, confirmed, err)
	}
	if estadoDaCobranca(t, service, cobrancaA.ID) == "Success" || estadoDaCobranca(t, service, cobrancaB.ID) == "Success" {
		t.Fatal("uma notificação de pedido desconhecido alterou uma cobrança")
	}
	// Com o PayPay a confirmar só a cobrança A, só A fica paga.
	servidor.definirEstado(cobrancaA.MerchantTransactionID, "TRADE_FINISHED")
	if _, confirmed, err = enviarNotificacaoPayPay(t, service, corpo(cobrancaA)); err != nil || !confirmed {
		t.Fatalf("confirmação de A: %v %v", confirmed, err)
	}
	if estadoDaCobranca(t, service, cobrancaA.ID) != "Success" || estadoDaCobranca(t, service, cobrancaB.ID) == "Success" {
		t.Fatal("a notificação de A só pode quitar A")
	}
}

func TestIntegrationPayPayCancelarFechaNoProvedorEAConsultaNaoReabre(t *testing.T) {
	ambientePayPay(t)
	client := integrationClient(t)
	servidor := novoPayPayServidorFalso(t)
	service := NewService(client)
	service.SetHTTPClient(&http.Client{Transport: servidor})
	academia := "PPC" + uuid.NewString()[:8]
	configurarPayPay(t, service, academia)
	ctx := context.Background()

	// O PayPay recusa o fecho (ex.: o pagador acabou de pagar): nada é cancelado.
	c1 := novaCobrancaPayPay(t, service, academia, 4000, "REF")
	servidor.falhaFecho = true
	if _, err := service.CancelCharge(ctx, ContextoAcademia, academia, c1.ID.String(), "teste", "u", "academia", "ip"); !errors.Is(err, ErrUpstream) {
		t.Fatalf("fecho recusado: %v", err)
	}
	if estado := estadoDaCobranca(t, service, c1.ID); estado != EstadoCobrancaAguardandoPagamento {
		t.Fatalf("cobrança cancelada localmente apesar de o fecho ter falhado: %s", estado)
	}
	servidor.falhaFecho = false

	// Fecho aceite: cancelada aqui e fechada no PayPay, uma vez.
	r, err := service.CancelCharge(ctx, ContextoAcademia, academia, c1.ID.String(), "teste", "u", "academia", "ip")
	if err != nil || r.Status != "cancelada" {
		t.Fatalf("cancelamento: %+v %v", r, err)
	}
	if n := len(servidor.chamadasDe("trade_close")); n != 2 { // uma tentativa recusada + a que valeu
		t.Fatalf("trade_close chamado %d vezes, queria 2", n)
	}
	// Consultar depois NÃO reabre a cobrança: o PayPay agora diz TRADE_CLOSED
	// (que, lido sozinho, seria "Expired"), mas cancelada é definitivo.
	if r, err = service.ConsultCharge(ctx, ContextoAcademia, academia, c1.ID.String(), "u", "academia", "ip"); err != nil || r.Status != "cancelada" {
		t.Fatalf("consulta depois do cancelamento: %+v %v", r, err)
	}
	if estado := estadoDaCobranca(t, service, c1.ID); estado != "cancelada" {
		t.Fatalf("estado guardado = %s, queria cancelada", estado)
	}
	// Mesmo que o PayPay volte a dizer "aguardando", continua cancelada.
	servidor.definirEstado(c1.MerchantTransactionID, "WAIT_BUYER_PAY")
	if _, err = service.ConsultCharge(ctx, ContextoAcademia, academia, c1.ID.String(), "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	if estado := estadoDaCobranca(t, service, c1.ID); estado != "cancelada" {
		t.Fatalf("a consulta reabriu a cobrança cancelada: %s", estado)
	}
	// Pagamento tardio depois de cancelada: continua cancelada e fica o conflito
	// registado para reconciliação manual (comportamento anterior, preservado).
	servidor.definirEstado(c1.MerchantTransactionID, "TRADE_FINISHED")
	if _, err = service.ConsultCharge(ctx, ContextoAcademia, academia, c1.ID.String(), "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	var conflitos int
	_ = client.DB().QueryRow(`SELECT COUNT(*) FROM spuri_ledger WHERE aggregate_id=$1 AND event_type='CobrancaConflitoPosCancelamento'`, c1.ID).Scan(&conflitos)
	if conflitos == 0 || estadoDaCobranca(t, service, c1.ID) != "cancelada" {
		t.Fatalf("pagamento tardio: conflitos=%d estado=%s", conflitos, estadoDaCobranca(t, service, c1.ID))
	}

	// Cobrança que o PayPay já fechou sozinho: cancelar não chama trade_close de novo.
	c2 := novaCobrancaPayPay(t, service, academia, 4500, "REF")
	servidor.definirEstado(c2.MerchantTransactionID, "TRADE_CLOSED")
	antes := len(servidor.chamadasDe("trade_close"))
	if _, err = service.CancelCharge(ctx, ContextoAcademia, academia, c2.ID.String(), "", "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	if len(servidor.chamadasDe("trade_close")) != antes {
		t.Fatal("não se fecha no provedor um pedido que ele já fechou")
	}
}

// ---- Troca de provedor ----------------------------------------------------------

// cenarioTroca prepara uma academia com mensalidades de 1500 Kz (REF aceite
// pelos dois provedores), credenciais dos dois provedores, e o PayPay ativo.
type cenarioTroca struct {
	service   *Service
	servidor  *paypayServidorFalso
	academia  string
	estudante string
	mes       MensalidadeSelecaoMes
}

func novoCenarioTroca(t *testing.T) *cenarioTroca {
	t.Helper()
	ambientePayPay(t)
	client := integrationClient(t)
	servidor := novoPayPayServidorFalso(t)
	service := NewService(client)
	service.SetHTTPClient(&http.Client{Transport: transporteMisto{paypay: servidor, appypay: &capturingAppyPayTransport{}}})
	academia := mensalidadeCodigo()
	estudante := "EST-TRC-" + uuid.NewString()[:8]
	seedMensalidadeAcademia(t, client, academia, "private", "fundamental", "2025_2026")
	seedMensalidadeTurma(t, client, academia, "T-TRC", "2025_2026", estudante, nil)
	seedMensalidadeConfiguracao(t, client, academia, NivelFundamental, "6_ano_fundamental", nil, 1500, 7, time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC))
	if _, err := client.DB().Exec(`UPDATE financeiro_mensalidade_configuracoes SET metodos_pagamento='{REF,GPO}' WHERE codigo_academia=$1`, academia); err != nil {
		t.Fatal(err)
	}
	configurarPayPay(t, service, academia)
	configurarAppyPay(t, service, academia)
	if _, err := service.DefinirProvedor(context.Background(), ContextoAcademia, academia, ProvedorPayPay, "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	pendentes, err := service.ListMensalidades(context.Background(), estudante, &academia)
	if err != nil || len(pendentes) == 0 {
		t.Fatalf("mensalidades: %v %v", pendentes, err)
	}
	return &cenarioTroca{service: service, servidor: servidor, academia: academia, estudante: estudante, mes: MensalidadeSelecaoMes{AnoLetivo: pendentes[0].AnoLetivo, Mes: pendentes[0].Mes}}
}

func (c *cenarioTroca) pagar(metodo string) (MensalidadePagamentoView, error) {
	return c.service.IniciarPagamentoMensalidades(context.Background(), MensalidadePagamentoInput{CodigoEstudante: c.estudante, CodigoAcademia: c.academia, Meses: []MensalidadeSelecaoMes{c.mes}, MetodoPagamento: metodo, Telefone: "923456789"}, c.estudante, "estudante", "41.223.10.5")
}

func TestIntegrationTrocaPayPayParaAppyPayFechaACobrancaAbertaENaoBloqueiaANova(t *testing.T) {
	c := novoCenarioTroca(t)
	ctx := context.Background()
	antiga, err := c.pagar("REF")
	if err != nil {
		t.Fatal(err)
	}
	if antiga.Charge.Provedor != ProvedorPayPay {
		t.Fatalf("a cobrança devia nascer no PayPay: %+v", antiga.Charge)
	}
	// Mesmo mês, mesmo provedor: continua a haver uma só cobrança em aberto.
	if _, err = c.pagar("REF"); err == nil || !strings.Contains(err.Error(), "já possui cobrança em aberto") {
		t.Fatalf("segunda cobrança no mesmo provedor deve continuar bloqueada: %v", err)
	}
	if _, err = c.service.DefinirProvedor(ctx, ContextoAcademia, c.academia, ProvedorAppyPay, "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	nova, err := c.pagar("REF")
	if err != nil {
		t.Fatalf("cobrança nova na AppyPay depois da troca: %v", err)
	}
	if nova.Charge.Provedor != ProvedorAppyPay {
		t.Fatalf("a cobrança nova devia nascer na AppyPay: %+v", nova.Charge)
	}
	// A antiga foi fechada no PayPay e cancelada no Spuri.
	if n := len(c.servidor.chamadasDe("trade_close")); n != 1 {
		t.Fatalf("trade_close chamado %d vezes, queria 1", n)
	}
	if estado := estadoDaCobranca(t, c.service, antiga.Charge.ID); estado != "cancelada" {
		t.Fatalf("a cobrança antiga ficou %s, queria cancelada", estado)
	}
	// Só uma cobrança em aberto para o mês.
	abertas, err := c.service.idsAbertosMensalidade(ctx, c.estudante, c.academia, c.mes.AnoLetivo, c.mes.Mes)
	if err != nil || len(abertas) != 1 || abertas[0] != nova.Charge.ID.String() {
		t.Fatalf("abertas = %v %v", abertas, err)
	}
	// O motivo do cancelamento automático ficou registado.
	var motivo string
	_ = c.service.client.DB().QueryRow(`SELECT payload->>'motivo' FROM financeiro_cobrancas WHERE id=$1`, antiga.Charge.ID).Scan(&motivo)
	if motivo != motivoSubstituicao {
		t.Fatalf("motivo = %q", motivo)
	}
}

// Se o PayPay recusar o fecho (o pagador acabou de pagar), a cobrança nova NÃO é
// criada e a antiga continua aberta.
func TestIntegrationTrocaFechoRecusadoNaoCriaCobrancaNova(t *testing.T) {
	ctx := context.Background()
	c2 := novoCenarioTroca(t)
	var err error
	if _, err = c2.pagar("REF"); err != nil {
		t.Fatal(err)
	}
	c2.servidor.falhaFecho = true
	if _, err = c2.service.DefinirProvedor(ctx, ContextoAcademia, c2.academia, ProvedorAppyPay, "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	// O erro tem de ser o do fecho recusado (ErrUpstream) — e não o de "já possui
	// cobrança em aberto", que também impediria a nova mas por outra razão.
	if _, err = c2.pagar("REF"); !errors.Is(err, ErrUpstream) {
		t.Fatalf("com o fecho recusado pelo PayPay a cobrança nova não pode ser criada, e o erro deve ser o do fecho: %v", err)
	}
	abertas, _ := c2.service.idsAbertosMensalidade(ctx, c2.estudante, c2.academia, c2.mes.AnoLetivo, c2.mes.Mes)
	if len(abertas) != 1 {
		t.Fatalf("a cobrança antiga tem de continuar aberta: %v", abertas)
	}
}

func TestIntegrationTrocaAppyPayParaPayPayMantemACobrancaAbertaPorqueNaoSeFecha(t *testing.T) {
	c := novoCenarioTroca(t)
	ctx := context.Background()
	if _, err := c.service.DefinirProvedor(ctx, ContextoAcademia, c.academia, ProvedorAppyPay, "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	antiga, err := c.pagar("REF")
	if err != nil || antiga.Charge.Provedor != ProvedorAppyPay {
		t.Fatalf("cobrança AppyPay: %+v %v", antiga.Charge, err)
	}
	if _, err = c.service.DefinirProvedor(ctx, ContextoAcademia, c.academia, ProvedorPayPay, "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	// A AppyPay não sabe fechar: cancelá-la só no Spuri deixaria o pagador a
	// poder pagar uma dívida "cancelada". Fica em aberto e bloqueia a nova.
	if _, err = c.pagar("REF"); err == nil || !strings.Contains(err.Error(), "já possui cobrança em aberto") {
		t.Fatalf("a cobrança da AppyPay devia continuar a bloquear: %v", err)
	}
	if estado := estadoDaCobranca(t, c.service, antiga.Charge.ID); estado != EstadoCobrancaAguardandoPagamento {
		t.Fatalf("a cobrança da AppyPay mudou para %s", estado)
	}
	if len(c.servidor.chamadasDe("instant_trade")) != 0 {
		t.Fatal("nada devia ter chegado ao PayPay")
	}
	// A pendente continua a ser acompanhada junto da AppyPay.
	view, err := c.service.ConsultarProvedorContexto(ctx, ContextoAcademia, c.academia)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range view.Provedores {
		if p.ID == ProvedorAppyPay && p.CobrancasPendentes != 1 {
			t.Fatalf("pendentes da AppyPay = %d", p.CobrancasPendentes)
		}
	}
}

func TestIntegrationTrocaMetodosSaoOsDoProvedorAtivo(t *testing.T) {
	c := novoCenarioTroca(t)
	ctx := context.Background()
	if _, err := c.pagar("GPO_QR"); err == nil {
		t.Fatal("GPO_QR não está habilitado nesta academia")
	}
	// A academia habilita GPO_QR: o PayPay não o suporta, a AppyPay sim.
	if _, err := c.service.client.DB().Exec(`UPDATE financeiro_mensalidade_configuracoes SET metodos_pagamento='{REF,GPO,GPO_QR}' WHERE codigo_academia=$1`, c.academia); err != nil {
		t.Fatal(err)
	}
	disp := c.service.PagamentoDisponivel(ctx, c.academia)
	if got := strings.Join(disp.Filtrar([]string{"REF", "GPO", "GPO_QR"}), ","); got != "REF,GPO" {
		t.Fatalf("métodos com o PayPay ativo = %s", got)
	}
	if _, err := c.pagar("GPO_QR"); err == nil || !errors.Is(err, ErrMetodoIndisponivel) {
		t.Fatalf("GPO_QR com o PayPay ativo: %v", err)
	}
	if _, err := c.service.DefinirProvedor(ctx, ContextoAcademia, c.academia, ProvedorAppyPay, "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	disp = c.service.PagamentoDisponivel(ctx, c.academia)
	if got := strings.Join(disp.Filtrar([]string{"REF", "GPO", "GPO_QR"}), ","); got != "REF,GPO,GPO_QR" {
		t.Fatalf("métodos com a AppyPay ativa = %s", got)
	}
}

func TestIntegrationPagamentoEmDuplicidadeNaoAplicaASegundaCobranca(t *testing.T) {
	c := novoCenarioTroca(t)
	ctx := context.Background()
	mensalidades := []MensalidadeSelecaoMes{c.mes}
	emitir := func() ChargeResult {
		r, err := c.service.CreateCharge(ctx, ChargeRequest{ContextoTipo: ContextoAcademia, CodigoAcademia: c.academia, CodigoEstudante: c.estudante, Mensalidades: mensalidades, Amount: 1500, Currency: "AOA", Description: "Propina", PaymentMethod: "REF", MerchantTransactionID: integrationMerchant("DU")}, c.estudante, "estudante", "ip")
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	// Duas cobranças para a mesma mensalidade (a proteção contra isto vive
	// em IniciarPagamentoMensalidades; aqui simulam-se o duplo clique e a
	// corrida entre dois dispositivos, que a ultrapassam).
	a, b := emitir(), emitir()
	c.servidor.definirEstado(a.MerchantTransactionID, "TRADE_FINISHED")
	c.servidor.definirEstado(b.MerchantTransactionID, "TRADE_FINISHED")

	if r, err := c.service.ConsultCharge(ctx, ContextoAcademia, c.academia, a.ID.String(), "u", "academia", "ip"); err != nil || r.Status != "Success" {
		t.Fatalf("primeira: %+v %v", r, err)
	}
	row, _ := c.service.loadChargeByID(ctx, a.ID)
	if dup, _ := row.Payload["pago_em_duplicidade"].(bool); dup {
		t.Fatal("a primeira cobrança paga nunca é a duplicada")
	}
	if estado, _, err := c.service.estadoObrigacao(ctx, c.estudante, c.academia, c.mes.AnoLetivo, c.mes.Mes); err != nil || estado != EstadoPago {
		t.Fatalf("a primeira devia quitar a mensalidade: %s %v", estado, err)
	}

	if r, err := c.service.ConsultCharge(ctx, ContextoAcademia, c.academia, b.ID.String(), "u", "academia", "ip"); err != nil || r.Status != "Success" {
		t.Fatalf("segunda: %+v %v", r, err)
	}
	rowB, _ := c.service.loadChargeByID(ctx, b.ID)
	if dup, _ := rowB.Payload["pago_em_duplicidade"].(bool); !dup {
		t.Fatalf("a segunda cobrança paga devia ficar marcada em duplicidade: %+v", rowB.Payload)
	}
	// Continua Success (o dinheiro entrou), mas só a primeira quitou a dívida.
	if rowB.Status != "Success" {
		t.Fatalf("estado da duplicada = %s", rowB.Status)
	}
	var pagas int
	_ = c.service.client.DB().QueryRow(`SELECT COUNT(*) FROM financeiro_mensalidade_obrigacoes_eventos WHERE codigo_estudante=$1 AND codigo_academia=$2 AND tipo='paga'`, c.estudante, c.academia).Scan(&pagas)
	if pagas != 1 {
		t.Fatalf("eventos 'paga' para o estudante = %d, queria 1 (a duplicada não se aplica)", pagas)
	}
	// Não abateu NENHUMA outra dívida: as restantes mensalidades continuam pendentes.
	todas, err := c.service.ListMensalidades(ctx, c.estudante, &c.academia)
	if err != nil {
		t.Fatal(err)
	}
	pendentes, pagos := 0, 0
	for _, m := range todas {
		switch m.Estado {
		case EstadoPendente:
			pendentes++
		case EstadoPago:
			pagos++
		}
	}
	if pagos != 1 || pendentes == 0 {
		t.Fatalf("pagos=%d pendentes=%d: a duplicada abateu outra dívida", pagos, pendentes)
	}
	// A academia vê o aviso na listagem de cobranças.
	lista, err := c.service.ListCobrancas(ctx, ContextoAcademia, c.academia, nil, nil, nil, nil, "", "", nil, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	avisos := 0
	for _, r := range lista.Cobrancas {
		if r.ID == b.ID {
			if !r.PagoEmDuplicidade || len(r.DuplicidadeCom) != 1 || r.DuplicidadeCom[0] != a.ID.String() {
				t.Fatalf("resumo da duplicada = %+v", r)
			}
			avisos++
		}
		if r.ID == a.ID && r.PagoEmDuplicidade {
			t.Fatal("a primeira não pode aparecer como duplicada")
		}
	}
	if avisos != 1 {
		t.Fatalf("a duplicada não apareceu na listagem (avisos=%d)", avisos)
	}
	// Repetir a confirmação (webhook repetido, nova consulta) é idempotente.
	if _, err = c.service.ConsultCharge(ctx, ContextoAcademia, c.academia, b.ID.String(), "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	_ = c.service.client.DB().QueryRow(`SELECT COUNT(*) FROM financeiro_mensalidade_obrigacoes_eventos WHERE codigo_estudante=$1 AND codigo_academia=$2 AND tipo='paga'`, c.estudante, c.academia).Scan(&pagas)
	if pagas != 1 {
		t.Fatalf("depois de repetir a consulta há %d eventos 'paga'", pagas)
	}
}

func TestIntegrationPayPayPagamentoDivergenteNaoQuita(t *testing.T) {
	ambientePayPay(t)
	client := integrationClient(t)
	servidor := novoPayPayServidorFalso(t)
	service := NewService(client)
	service.SetHTTPClient(&http.Client{Transport: servidor})
	academia := "PPD" + uuid.NewString()[:8]
	configurarPayPay(t, service, academia)
	_, plataforma, _ := paypayChaves(t)
	ctx := context.Background()
	charge := novaCobrancaPayPay(t, service, academia, 1500, "REF")
	eventos := func() int {
		var n int
		_ = client.DB().QueryRow(`SELECT COUNT(*) FROM spuri_ledger WHERE aggregate_id=$1 AND event_type='CobrancaConsultada'`, charge.ID).Scan(&n)
		return n
	}
	resumo := func() CobrancaResumo {
		lista, err := service.ListCobrancas(ctx, ContextoAcademia, academia, nil, nil, nil, nil, "", "", nil, 50, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range lista.Cobrancas {
			if r.ID == charge.ID {
				return r
			}
		}
		t.Fatal("cobrança não listada")
		return CobrancaResumo{}
	}

	// O PayPay diz que o pedido está liquidado, mas com 1000 Kz em vez de 1500.
	servidor.definirEstado(charge.MerchantTransactionID, "TRADE_FINISHED")
	servidor.definirValor(charge.MerchantTransactionID, "1000.00")
	r, err := service.ConsultCharge(ctx, ContextoAcademia, academia, charge.ID.String(), "u", "academia", "ip")
	if err != nil || r.Status != EstadoCobrancaAguardandoPagamento {
		t.Fatalf("consulta divergente: %+v %v (não pode quitar)", r, err)
	}
	if res := resumo(); !res.PagamentoDivergente || res.ValorPagoProvedor == nil || *res.ValorPagoProvedor != 1000 || res.Valor != 1500 || res.Status != EstadoCobrancaAguardandoPagamento {
		t.Fatalf("resumo = %+v", res)
	}
	// Consultar de novo não enche o ledger de eventos iguais.
	antes := eventos()
	if _, err = service.ConsultCharge(ctx, ContextoAcademia, academia, charge.ID.String(), "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	if eventos() != antes {
		t.Fatalf("consulta repetida gravou %d evento(s) novo(s)", eventos()-antes)
	}
	// A notificação assinada também não quita (a consulta ao vivo discorda do valor).
	campos := camposNotificacao(charge.MerchantTransactionID, "TRADE_FINISHED", "N-"+uuid.NewString()[:8])
	campos["amount"] = "1000.00"
	accepted, confirmed, err := enviarNotificacaoPayPay(t, service, paypayNotificacao(t, plataforma, campos))
	if err != nil || !accepted || confirmed {
		t.Fatalf("webhook divergente: accepted=%v confirmed=%v err=%v", accepted, confirmed, err)
	}
	if estadoDaCobranca(t, service, charge.ID) != EstadoCobrancaAguardandoPagamento {
		t.Fatal("o webhook divergente quitou a cobrança")
	}
	// O que o webhook diz sozinho também conta: sem a consulta ao vivo (PayPay
	// indisponível) um valor diferente continua a não quitar.
	servidor.erros["trade_query"] = "E9999"
	campos2 := camposNotificacao(charge.MerchantTransactionID, "TRADE_FINISHED", "N-"+uuid.NewString()[:8])
	campos2["amount"] = "1000.00"
	if _, confirmed, err = enviarNotificacaoPayPay(t, service, paypayNotificacao(t, plataforma, campos2)); err != nil || confirmed {
		t.Fatalf("webhook divergente sem consulta: confirmed=%v err=%v", confirmed, err)
	}
	delete(servidor.erros, "trade_query")
	// Corrigido no PayPay: passa a quitar e a marca de divergência desaparece.
	servidor.definirValor(charge.MerchantTransactionID, "1500.00")
	if r, err = service.ConsultCharge(ctx, ContextoAcademia, academia, charge.ID.String(), "u", "academia", "ip"); err != nil || r.Status != "Success" {
		t.Fatalf("consulta corrigida: %+v %v", r, err)
	}
	if res := resumo(); res.PagamentoDivergente || res.ValorPagoProvedor != nil || res.Status != "Success" {
		t.Fatalf("resumo depois de corrigido = %+v", res)
	}
}
