package finance

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- Chaves de teste (RSA 1024, o tamanho que o PayPay exige) ---------------

var (
	paypayChavesOnce                                           sync.Once
	paypayComerciante, paypayPlataforma, paypayOutraPlataforma *rsa.PrivateKey
)

func paypayChaves(t testing.TB) (comerciante, plataforma, outra *rsa.PrivateKey) {
	t.Helper()
	paypayChavesOnce.Do(func() {
		var err error
		if paypayComerciante, err = rsa.GenerateKey(rand.Reader, 1024); err != nil {
			panic(err)
		}
		if paypayPlataforma, err = rsa.GenerateKey(rand.Reader, 1024); err != nil {
			panic(err)
		}
		if paypayOutraPlataforma, err = rsa.GenerateKey(rand.Reader, 1024); err != nil {
			panic(err)
		}
	})
	return paypayComerciante, paypayPlataforma, paypayOutraPlataforma
}

func paypayPrivadaB64(t testing.TB, k *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

func paypayPublicaB64(t testing.TB, k *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

// credenciaisPayPayDeTeste são as credenciais como o formulário as envia.
func credenciaisPayPayDeTeste(t testing.TB, plataforma *rsa.PrivateKey) map[string]string {
	t.Helper()
	comerciante, _, _ := paypayChaves(t)
	return map[string]string{
		"partner_id":           "2088000000000001",
		"sale_product_code":    "123456789",
		"chave_privada":        paypayPrivadaB64(t, comerciante),
		"chave_publica_paypay": paypayPublicaB64(t, plataforma),
	}
}

func credencialPayPayDeTeste(t testing.TB) credencialProvedor {
	t.Helper()
	_, plataforma, _ := paypayChaves(t)
	v, err := (&paypayProvider{}).ValidarCredenciais(credenciaisPayPayDeTeste(t, plataforma))
	if err != nil {
		t.Fatal(err)
	}
	return credencialProvedor{Valores: v}
}

// ---- Servidor PayPay simulado ------------------------------------------------
//
// Faz do lado do servidor o que o PayPay faz: lê o corpo, descodifica os
// valores UMA vez, verifica a assinatura SHA1withRSA com a chave pública do
// comerciante e decifra o biz_content com a operação pública do RSA. Tudo com
// código independente do que está em teste.

type paypayChamadaRecebida struct {
	Servico string
	Params  map[string]string
	Biz     map[string]any
}

type paypayServidorFalso struct {
	t          testing.TB
	mu         sync.Mutex
	pubCliente *rsa.PublicKey
	chamadas   []paypayChamadaRecebida
	estado     map[string]string // out_trade_no -> estado devolvido pela consulta
	falhaFecho bool
	erros      map[string]string // serviço -> código de erro forçado
	valores    map[string]string // out_trade_no -> valor do pedido guardado (devolvido na consulta)
	http500    bool
	referencia int
}

func novoPayPayServidorFalso(t testing.TB) *paypayServidorFalso {
	comerciante, _, _ := paypayChaves(t)
	return &paypayServidorFalso{t: t, pubCliente: &comerciante.PublicKey, estado: map[string]string{}, erros: map[string]string{}, valores: map[string]string{}}
}

func (m *paypayServidorFalso) definirValor(pedido, valor string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.valores[pedido] = valor
}

func (m *paypayServidorFalso) definirEstado(pedido, estado string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.estado[pedido] = estado
}

func (m *paypayServidorFalso) chamadasDe(servico string) []paypayChamadaRecebida {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []paypayChamadaRecebida
	for _, c := range m.chamadas {
		if c.Servico == servico {
			out = append(out, c)
		}
	}
	return out
}

func (m *paypayServidorFalso) RoundTrip(req *http.Request) (*http.Response, error) {
	t := m.t
	corpo, _ := io.ReadAll(req.Body)
	if m.http500 {
		return respostaHTTP(req, http.StatusInternalServerError, `{}`), nil
	}
	if ct := req.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	var cru map[string]string
	if err := json.Unmarshal(corpo, &cru); err != nil {
		t.Errorf("corpo não é um objeto JSON de strings: %v", err)
		return respostaHTTP(req, 400, `{}`), nil
	}
	params := map[string]string{}
	for k, v := range cru {
		d, err := url.QueryUnescape(strings.ReplaceAll(v, "+", "%2B"))
		if err != nil {
			t.Errorf("valor %s não é urlencode válido: %v", k, err)
		}
		params[k] = d
	}
	for _, k := range []string{"charset", "biz_content", "partner_id", "service", "request_no", "format", "language", "version", "timestamp", "sign", "sign_type"} {
		if params[k] == "" {
			t.Errorf("parâmetro obrigatório %s ausente", k)
		}
	}
	if params["sign_type"] != "RSA" || params["charset"] != "UTF-8" || params["format"] != "JSON" || params["version"] != "1.0" || params["language"] != "pt" {
		t.Errorf("parâmetros fixos errados: %+v", params)
	}
	if _, err := time.Parse("2006-01-02 15:04:05", params["timestamp"]); err != nil {
		t.Errorf("timestamp inválido %q", params["timestamp"])
	}
	if len(params["request_no"]) < 8 || len(params["request_no"]) > 64 {
		t.Errorf("request_no inválido %q", params["request_no"])
	}
	// Assinatura: texto ordenado, sem vazios nem sign/sign_type, valores sem urlencode.
	chaves := []string{}
	for k, v := range params {
		if k != "sign" && k != "sign_type" && v != "" {
			chaves = append(chaves, k)
		}
	}
	sort.Strings(chaves)
	partes := []string{}
	for _, k := range chaves {
		partes = append(partes, k+"="+params[k])
	}
	soma := sha1.Sum([]byte(strings.Join(partes, "&")))
	sig, err := base64.StdEncoding.DecodeString(params["sign"])
	if err != nil || rsa.VerifyPKCS1v15(m.pubCliente, crypto.SHA1, soma[:], sig) != nil {
		t.Errorf("assinatura do pedido inválida (service=%s)", params["service"])
		return respostaHTTP(req, 200, `{"code":"E0001","msg":"Invalid signature"}`), nil
	}
	biz := paypayDecifrarComChavePublica(t, m.pubCliente, params["biz_content"])
	m.mu.Lock()
	m.chamadas = append(m.chamadas, paypayChamadaRecebida{Servico: params["service"], Params: params, Biz: biz})
	m.mu.Unlock()

	if codigo := m.erros[params["service"]]; codigo != "" {
		return respostaHTTP(req, 200, `{"code":"`+codigo+`","sub_code":"`+codigo+`.SUB","msg":"Business failed","sub_msg":"erro simulado do PayPay"}`), nil
	}
	pedido, _ := biz["out_trade_no"].(string)
	switch params["service"] {
	case "instant_trade":
		m.mu.Lock()
		m.referencia++
		ref := 100000000 + m.referencia
		m.mu.Unlock()
		resp := map[string]any{"out_trade_no": pedido, "trade_no": "PP" + pedido, "status": "P", "reference_id": ref, "entity_id": "00123", "trade_token": "token-secreto", "dynamic_link": "https://pay.example/x?token=token-secreto"}
		if ti, ok := biz["trade_info"].(map[string]any); ok {
			pedido, _ = ti["out_trade_no"].(string)
			resp["out_trade_no"], resp["trade_no"] = pedido, "PP"+pedido
			m.mu.Lock()
			m.valores[pedido], _ = ti["total_amount"].(string)
			m.mu.Unlock()
		}
		b, _ := json.Marshal(map[string]any{"code": "S0001", "msg": "Success", "biz_content": resp})
		return respostaHTTP(req, 200, string(b)), nil
	case "trade_query":
		m.mu.Lock()
		estado, valor := m.estado[pedido], m.valores[pedido]
		m.mu.Unlock()
		if estado == "" {
			estado = "WAIT_BUYER_PAY"
		}
		corpoQuery := map[string]any{"out_trade_no": pedido, "trade_no": "PP" + pedido, "status": estado}
		if valor != "" {
			corpoQuery["amount"] = valor
			corpoQuery["seller_actual_amount"] = "1.00" // líquido de taxas: nunca deve ser usado na comparação
		}
		b, _ := json.Marshal(map[string]any{"code": "S0001", "msg": "Success", "biz_content": corpoQuery})
		return respostaHTTP(req, 200, string(b)), nil
	case "trade_close":
		if m.falhaFecho {
			return respostaHTTP(req, 200, `{"code":"F0002","sub_code":"TRADE_HAS_SUCCESS","msg":"Business failed","sub_msg":"pedido já pago"}`), nil
		}
		m.mu.Lock()
		m.estado[pedido] = "TRADE_CLOSED"
		m.mu.Unlock()
		return respostaHTTP(req, 200, `{"code":"S0001","msg":"Success","biz_content":{"out_trade_no":"`+pedido+`","status":"TRADE_CLOSED"}}`), nil
	}
	t.Errorf("serviço inesperado %q", params["service"])
	return respostaHTTP(req, 400, `{}`), nil
}

func respostaHTTP(req *http.Request, status int, corpo string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(corpo)), Request: req}
}

// paypayDecifrarComChavePublica desfaz a cifra com chave privada do §2.4: por
// bloco, m = c^e mod n, e remove o preenchimento 00 01 FF..FF 00.
func paypayDecifrarComChavePublica(t testing.TB, pub *rsa.PublicKey, b64 string) map[string]any {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Errorf("biz_content não é base64: %v", err)
		return nil
	}
	k := (pub.N.BitLen() + 7) / 8
	if len(raw)%k != 0 {
		t.Errorf("biz_content cifrado tem %d bytes, não múltiplo de %d", len(raw), k)
		return nil
	}
	var claro bytes.Buffer
	for off := 0; off < len(raw); off += k {
		m := new(big.Int).Exp(new(big.Int).SetBytes(raw[off:off+k]), big.NewInt(int64(pub.E)), pub.N)
		em := m.FillBytes(make([]byte, k))
		if em[0] != 0x00 || em[1] != 0x01 {
			t.Errorf("preenchimento não é PKCS#1 tipo 1: %x", em[:4])
			return nil
		}
		i := 2
		for i < len(em) && em[i] == 0xFF {
			i++
		}
		if i < 10 || i >= len(em) || em[i] != 0x00 {
			t.Errorf("preenchimento PKCS#1 malformado")
			return nil
		}
		claro.Write(em[i+1:])
	}
	var biz map[string]any
	if err := json.Unmarshal(claro.Bytes(), &biz); err != nil {
		t.Errorf("biz_content decifrado não é JSON: %v (%q)", err, claro.String())
	}
	return biz
}

// paypayNotificacao monta o corpo de uma notificação como o PayPay a envia:
// formulário urlencoded, assinado com a chave privada DELE.
func paypayNotificacao(t testing.TB, plataforma *rsa.PrivateKey, campos map[string]string) []byte {
	t.Helper()
	chaves := []string{}
	for k, v := range campos {
		if v != "" && k != "sign" && k != "sign_type" {
			chaves = append(chaves, k)
		}
	}
	sort.Strings(chaves)
	partes := []string{}
	for _, k := range chaves {
		partes = append(partes, k+"="+campos[k])
	}
	soma := sha1.Sum([]byte(strings.Join(partes, "&")))
	sig, err := rsa.SignPKCS1v15(rand.Reader, plataforma, crypto.SHA1, soma[:])
	if err != nil {
		t.Fatal(err)
	}
	f := url.Values{}
	for k, v := range campos {
		f.Set(k, v)
	}
	f.Set("sign", base64.StdEncoding.EncodeToString(sig))
	f.Set("sign_type", "RSA")
	return []byte(f.Encode())
}

func camposNotificacao(pedido, estado, notifyID string) map[string]string {
	return map[string]string{
		"notify_type": "trade_status_sync", "notify_id": notifyID, "notify_time": "2026-11-26 10:00:00",
		"out_trade_no": pedido, "inner_trade_no": "PP" + pedido, "status": estado,
		"gmt_create": "2026-11-26 09:59:00", "payerIdentity": "9230*****", "amount": "1500.00",
	}
}

func paypayProvedorDeTeste(t testing.TB, servidor *paypayServidorFalso) *paypayProvider {
	t.Helper()
	if servidor == nil {
		return &paypayProvider{httpClient: &http.Client{Transport: roundTripperVazio{}}, agora: time.Now}
	}
	return &paypayProvider{httpClient: &http.Client{Transport: servidor}, agora: time.Now}
}

type roundTripperVazio struct{}

func (roundTripperVazio) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("sem rede")
}

// ---- Testes -------------------------------------------------------------------

func TestPayPayCifraEAssinaturaSaoVerificaveisPeloServidor(t *testing.T) {
	t.Setenv("PAYPAY_API_URL", "https://paypay.test/gateway")
	servidor := novoPayPayServidorFalso(t)
	p := paypayProvedorDeTeste(t, servidor)
	cred := credencialPayPayDeTeste(t)
	// Descrição longa e com caracteres especiais: força vários blocos RSA (117
	// bytes cada) e prova o urlencode de & = + espaço % e acentos.
	descricao := "Propinas Colégio Ágape & Filhos = 3 mensalidade(s) +100% — " + strings.Repeat("abc ", 40)
	resp, err := p.CriarCobranca(context.Background(), cred, pedidoCobranca{Valor: 1500, Moeda: "AOA", Descricao: descricao, IDComerciante: "TABC123456789", Metodo: MetodoREF, IPPagador: "41.223.10.5"})
	if err != nil {
		t.Fatal(err)
	}
	chamadas := servidor.chamadasDe("instant_trade")
	if len(chamadas) != 1 {
		t.Fatalf("instant_trade chamado %d vezes", len(chamadas))
	}
	biz := chamadas[0].Biz
	ti := biz["trade_info"].(map[string]any)
	if ti["subject"] != strings.TrimSpace(descricao) {
		t.Fatalf("subject não sobreviveu à cifra em blocos/urlencode: %q", ti["subject"])
	}
	if ti["out_trade_no"] != "TABC123456789" || ti["total_amount"] != "1500.00" || ti["price"] != "1500.00" || ti["currency"] != "AOA" || ti["quantity"] != "1" || ti["payee_identity"] != "2088000000000001" || ti["payee_identity_type"] != "1" {
		t.Fatalf("trade_info = %#v", ti)
	}
	pm := biz["pay_method"].(map[string]any)
	if pm["bank_code"] != "REF" || pm["pay_product_code"] != "31" || pm["amount"] != "1500.00" {
		t.Fatalf("pay_method = %#v", pm)
	}
	if _, temTelefone := pm["phone_num"]; temTelefone {
		t.Fatal("REF não leva telefone")
	}
	if biz["cashier_type"] != "SDK" || biz["payer_ip"] != "41.223.10.5" || biz["sale_product_code"] != "123456789" {
		t.Fatalf("biz = %#v", biz)
	}
	if resp.ID != "PPTABC123456789" || resp.Outcome.Status != "Pending" {
		t.Fatalf("resp = %+v", resp)
	}
	// Referência e entidade ficam ao nível de cima; token e link ficam de fora.
	if resp.Bruta["reference_id"] == nil || resp.Bruta["entity_id"] != "00123" {
		t.Fatalf("bruta sem referência/entidade: %#v", resp.Bruta)
	}
	for _, proibido := range []string{"trade_token", "dynamic_link"} {
		if _, ok := resp.Bruta[proibido]; ok {
			t.Fatalf("%s não pode ser guardado nem devolvido", proibido)
		}
	}
}

func TestPayPayExpressNormalizaTelefoneEMarcaMUL(t *testing.T) {
	t.Setenv("PAYPAY_API_URL", "https://paypay.test/gateway")
	servidor := novoPayPayServidorFalso(t)
	p := paypayProvedorDeTeste(t, servidor)
	cred := credencialPayPayDeTeste(t)
	for _, entrada := range []string{"923 456 789", "+244 923456789", "244923456789", "  923456789 "} {
		if _, err := p.CriarCobranca(context.Background(), cred, pedidoCobranca{Valor: 500, Moeda: "AOA", Descricao: "x", IDComerciante: "TEXP001", Metodo: MetodoGPO, Info: map[string]any{"phoneNumber": entrada}}); err != nil {
			t.Fatalf("telefone %q: %v", entrada, err)
		}
	}
	for _, c := range servidor.chamadasDe("instant_trade") {
		pm := c.Biz["pay_method"].(map[string]any)
		if pm["bank_code"] != "MUL" || pm["phone_num"] != "923456789" {
			t.Fatalf("pay_method Express = %#v", pm)
		}
		if c.Biz["payer_ip"] != "127.0.0.1" {
			t.Fatalf("sem IP válido deve usar 127.0.0.1, veio %v", c.Biz["payer_ip"])
		}
	}
	for _, invalido := range []string{"", "12345", "823456789", "9234567890", "abc"} {
		if _, err := p.CriarCobranca(context.Background(), cred, pedidoCobranca{Valor: 500, Moeda: "AOA", Descricao: "x", IDComerciante: "TEXP002", Metodo: MetodoGPO, Info: map[string]any{"phoneNumber": invalido}}); err == nil {
			t.Fatalf("telefone inválido %q deveria ser recusado antes de chamar o PayPay", invalido)
		}
	}
	if n := len(servidor.chamadasDe("instant_trade")); n != 4 {
		t.Fatalf("o PayPay recebeu %d pedidos; os inválidos nunca deviam sair daqui", n)
	}
}

func TestPayPayRegrasDePedido(t *testing.T) {
	p := paypayProvedorDeTeste(t, nil)
	if err := p.ValidarPedido(MetodoREF, 1000, nil); err == nil {
		t.Fatal("REF em exatamente 1000 Kz deve ser recusado (o PayPay exige acima de 1000)")
	}
	if err := p.ValidarPedido(MetodoREF, 1000.01, nil); err != nil {
		t.Fatalf("REF acima de 1000: %v", err)
	}
	if err := p.ValidarPedido(MetodoGPO, 50, map[string]any{"phoneNumber": "923456789"}); err != nil {
		t.Fatalf("Express não tem mínimo: %v", err)
	}
	if err := p.ValidarPedido(MetodoGPO, 50, map[string]any{"phoneNumber": "12"}); err == nil {
		t.Fatal("Express com telefone inválido deve ser recusado antes de registar nada")
	}
	if got := strings.Join(p.Metodos(), ","); got != "REF,GPO" {
		t.Fatalf("métodos = %s", got)
	}
	if _, err := p.CriarQRCode(context.Background(), credencialProvedor{}, pedidoQRCode{}); !errors.Is(err, ErrMetodoIndisponivel) {
		t.Fatalf("GPO_QR: %v", err)
	}
	if _, err := p.ResolverMetodo(credencialProvedor{}, "GPO_QR"); err == nil {
		t.Fatal("GPO_QR não é suportado")
	}
	if _, err := p.CriarCobranca(context.Background(), credencialPayPayDeTeste(t), pedidoCobranca{Valor: 5000, Moeda: "USD", Metodo: MetodoREF}); err == nil {
		t.Fatal("só AOA")
	}
}

func TestPayPayTraducaoDeEstados(t *testing.T) {
	casos := []struct{ estado, fc, fr, status, categoria string }{
		{"TRADE_FINISHED", "", "", "Success", ""},
		{"WAIT_BUYER_PAY", "", "", "Pending", ""},
		{"P", "", "", "Pending", ""},
		{"TRADE_CLOSED", "", "", "Expired", "tempo_esgotado"},
		{"TRADE_CLOSED", "E123", "saldo insuficiente", "Failed", "recusado_pelo_processador"},
		// TRADE_SUCCESS é só aviso: o PayPay recomenda não dar efeito de negócio.
		{"TRADE_SUCCESS", "", "", "", ""},
		{"REFUND_SUCCESS", "", "", "", ""},
		{"ALGO_NOVO", "", "", "", ""},
		{"", "", "", "", ""},
	}
	for _, c := range casos {
		got := paypayOutcomeDeEstado(c.estado, c.fc, c.fr)
		if got.Status != c.status || got.Categoria != c.categoria {
			t.Errorf("estado %q (%q/%q) -> %+v, queria %s/%s", c.estado, c.fc, c.fr, got, c.status, c.categoria)
		}
	}
	if got := paypayOutcomeDeEstado("TRADE_CLOSED", "E123", "saldo insuficiente"); got.Message != "saldo insuficiente" || got.Source != "E123" {
		t.Errorf("motivo da falha perdido: %+v", got)
	}
}

func TestPayPayConsultaFechoEErros(t *testing.T) {
	t.Setenv("PAYPAY_API_URL", "https://paypay.test/gateway")
	servidor := novoPayPayServidorFalso(t)
	p := paypayProvedorDeTeste(t, servidor)
	cred := credencialPayPayDeTeste(t)
	ctx := context.Background()
	ref := referenciaCobranca{IDProvedor: "PPT1", IDComerciante: "T1"}

	r, err := p.ConsultarCobranca(ctx, cred, ref)
	if err != nil || r.Outcome.Status != "Pending" || r.ID != "PPT1" {
		t.Fatalf("consulta pendente: %+v %v", r, err)
	}
	servidor.definirEstado("T1", "TRADE_FINISHED")
	if r, err = p.ConsultarCobranca(ctx, cred, ref); err != nil || r.Outcome.Status != "Success" {
		t.Fatalf("consulta paga: %+v %v", r, err)
	}
	servidor.definirEstado("T1", "TRADE_SUCCESS")
	if r, err = p.ConsultarCobranca(ctx, cred, ref); err != nil || r.Outcome.Status != "" {
		t.Fatalf("TRADE_SUCCESS não pode decidir o estado: %+v %v", r, err)
	}
	// A consulta usa o número do comerciante (out_trade_no), não o id do PayPay.
	for _, c := range servidor.chamadasDe("trade_query") {
		if c.Biz["out_trade_no"] != "T1" {
			t.Fatalf("trade_query biz = %#v", c.Biz)
		}
	}
	// Fecho.
	if err = p.CancelarCobranca(ctx, cred, ref); err != nil {
		t.Fatalf("fecho: %v", err)
	}
	if len(servidor.chamadasDe("trade_close")) != 1 {
		t.Fatal("trade_close não foi chamado")
	}
	servidor.falhaFecho = true
	if err = p.CancelarCobranca(ctx, cred, ref); !errors.Is(err, ErrUpstream) {
		t.Fatalf("fecho recusado deve devolver ErrUpstream: %v", err)
	}
	// Erro de negócio na CONSULTA nunca vira estado da cobrança: é erro.
	servidor.erros["trade_query"] = "E9999"
	if r, err = p.ConsultarCobranca(ctx, cred, ref); !errors.Is(err, ErrUpstream) || r.Outcome.Status != "" {
		t.Fatalf("consulta recusada: %+v %v", r, err)
	}
	// Erro de negócio na CRIAÇÃO é uma recusa: Failed com o motivo do PayPay.
	servidor.erros["instant_trade"] = "E0007"
	resp, err := p.CriarCobranca(ctx, cred, pedidoCobranca{Valor: 5000, Moeda: "AOA", Descricao: "x", IDComerciante: "T2", Metodo: MetodoREF})
	if err != nil || resp.Outcome.Status != "Failed" || resp.Outcome.Message != "erro simulado do PayPay" || resp.Outcome.Source != "E0007.SUB" || resp.ID != "" {
		t.Fatalf("recusa na criação: %+v %v", resp, err)
	}
	// HTTP 5xx: indisponível.
	servidor.http500 = true
	if _, err = p.CriarCobranca(ctx, cred, pedidoCobranca{Valor: 5000, Moeda: "AOA", Descricao: "x", IDComerciante: "T3", Metodo: MetodoREF}); !errors.Is(err, ErrUpstream) {
		t.Fatalf("HTTP 500: %v", err)
	}
	// Sem endereço da API configurado.
	t.Setenv("PAYPAY_API_URL", "")
	if err = p.CancelarCobranca(ctx, cred, ref); !errors.Is(err, errPayPayURLAusente) {
		t.Fatalf("sem PAYPAY_API_URL: %v", err)
	}
}

func TestPayPayEnderecoDaAPI(t *testing.T) {
	p := paypayProvedorDeTeste(t, nil)
	t.Setenv("ENV", "production")
	t.Setenv("PAYPAY_API_URL", "http://paypay.test/gateway")
	if _, err := paypayURLAPI(); err == nil {
		t.Fatal("em produção só https")
	}
	if err := p.ValidarAmbiente(); err == nil {
		t.Fatal("ValidarAmbiente deve recusar http em produção")
	}
	t.Setenv("PAYPAY_API_URL", "https://u:p@paypay.test/gateway")
	if _, err := paypayURLAPI(); err == nil {
		t.Fatal("endereço com utilizador/senha deve ser recusado")
	}
	t.Setenv("PAYPAY_API_URL", "https://paypay.test/gateway/")
	if u, err := paypayURLAPI(); err != nil || u != "https://paypay.test/gateway" {
		t.Fatalf("https válido: %q %v", u, err)
	}
	t.Setenv("PAYPAY_API_URL", "")
	if err := p.ValidarAmbiente(); err != nil {
		t.Fatalf("o endereço é opcional no arranque: %v", err)
	}
}

func TestPayPayValidarCredenciais(t *testing.T) {
	p := paypayProvedorDeTeste(t, nil)
	comerciante, plataforma, _ := paypayChaves(t)
	base := credenciaisPayPayDeTeste(t, plataforma)
	copia := func(alteracoes map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range alteracoes {
			out[k] = v
		}
		return out
	}

	norm, err := p.ValidarCredenciais(copia(map[string]string{"partner_id": " 2088000000000001 ", "sale_product_code": " 123456789 "}))
	if err != nil {
		t.Fatal(err)
	}
	if norm["partner_id"] != "2088000000000001" || norm["sale_product_code"] != "123456789" {
		t.Fatalf("normalização: %v", norm)
	}
	// PEM (com cabeçalhos e quebras de linha) e PKCS#1 também são aceites.
	derPriv := x509.MarshalPKCS1PrivateKey(comerciante)
	pemPriv := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: derPriv}))
	derPub, _ := x509.MarshalPKIXPublicKey(&plataforma.PublicKey)
	pemPub := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: derPub}))
	if _, err = p.ValidarCredenciais(copia(map[string]string{"chave_privada": pemPriv, "chave_publica_paypay": pemPub})); err != nil {
		t.Fatalf("PEM/PKCS#1: %v", err)
	}
	// PEM colado num campo de uma linha: as quebras de linha desapareceram.
	umaLinha := strings.ReplaceAll(strings.ReplaceAll(pemPriv, "\n", ""), "\r", "")
	umaLinhaPub := strings.ReplaceAll(pemPub, "\n", "")
	if _, err = p.ValidarCredenciais(copia(map[string]string{"chave_privada": umaLinha, "chave_publica_paypay": umaLinhaPub})); err != nil {
		t.Fatalf("PEM sem quebras de linha: %v", err)
	}
	// Base64 com quebras de linha (como o portal entrega).
	quebrado := base[("chave_privada")][:64] + "\n" + base["chave_privada"][64:]
	if _, err = p.ValidarCredenciais(copia(map[string]string{"chave_privada": quebrado})); err != nil {
		t.Fatalf("base64 com quebras: %v", err)
	}

	recusas := map[string]map[string]string{
		"partner com letras":          {"partner_id": "20A8"},
		"partner vazio":               {"partner_id": ""},
		"produto curto":               {"sale_product_code": "1234"},
		"produto com símbolo":         {"sale_product_code": "12345678!"},
		"chave privada lixo":          {"chave_privada": "isto-nao-e-uma-chave"},
		"chave privada vazia":         {"chave_privada": ""},
		"chave pública lixo":          {"chave_publica_paypay": "AAAA"},
		"chave pública é a sua":       {"chave_publica_paypay": paypayPublicaB64(t, comerciante)},
		"privada no lugar da pública": {"chave_publica_paypay": base["chave_privada"]},
	}
	for nome, alt := range recusas {
		if _, err := p.ValidarCredenciais(copia(alt)); err == nil {
			t.Errorf("%s: deveria ser recusado", nome)
		}
	}
	// Chave RSA de 512 bits é recusada (o gerador do Go já nem a cria; um valor
	// pequeno vindo de fora seria recusado pelo tamanho mínimo).
	if _, err := paypayChavePrivada(base["chave_publica_paypay"]); err == nil {
		t.Error("uma chave pública não pode passar por privada")
	}

	// Mascarar nunca mostra a privada nem as chaves por inteiro.
	m := p.Mascarar(norm)
	if _, ok := m["chave_privada"]; ok {
		t.Fatal("chave privada nunca é mascarada: simplesmente não existe na resposta")
	}
	for k, v := range m {
		if v == norm[k] || strings.Contains(v, norm["chave_privada"]) {
			t.Fatalf("campo %s não foi mascarado: %s", k, v)
		}
	}
	for _, c := range p.Campos() {
		if c.Chave == "chave_privada" && !c.Secreto {
			t.Fatal("a chave privada tem de ser campo secreto")
		}
	}
}

func TestPayPayWebhookAutenticacaoPorAssinatura(t *testing.T) {
	p := paypayProvedorDeTeste(t, nil)
	_, plataforma, outra := paypayChaves(t)
	cred := credencialPayPayDeTeste(t)
	credOutraConta := credencialProvedor{Valores: map[string]string{"chave_publica_paypay": paypayPublicaB64(t, outra)}}

	corpo := paypayNotificacao(t, plataforma, camposNotificacao("T100", "TRADE_FINISHED", "N1"))
	if i, ok := p.AutenticarWebhook(http.Header{}, corpo, []credencialProvedor{credOutraConta, cred}); !ok || i != 1 {
		t.Fatalf("assinatura válida: i=%d ok=%v (devia escolher o candidato cuja chave verifica)", i, ok)
	}
	// Corpo alterado depois de assinado (valor, estado, campo novo): recusado.
	for nome, alterar := range map[string]func(url.Values){
		"estado trocado":     func(v url.Values) { v.Set("status", "TRADE_CLOSED") },
		"valor trocado":      func(v url.Values) { v.Set("amount", "1.00") },
		"campo acrescentado": func(v url.Values) { v.Set("extra", "x") },
		"pedido trocado":     func(v url.Values) { v.Set("out_trade_no", "T999") },
	} {
		v, _ := url.ParseQuery(string(corpo))
		alterar(v)
		if _, ok := p.AutenticarWebhook(http.Header{}, []byte(v.Encode()), []credencialProvedor{cred}); ok {
			t.Errorf("%s: notificação adulterada foi aceite", nome)
		}
	}
	// Assinada por outra chave: recusada.
	falsa := paypayNotificacao(t, outra, camposNotificacao("T100", "TRADE_FINISHED", "N1"))
	if _, ok := p.AutenticarWebhook(http.Header{}, falsa, []credencialProvedor{cred}); ok {
		t.Fatal("notificação assinada por outra chave foi aceite")
	}
	// Sem assinatura / corpo vazio / lixo / sem candidatos: recusada.
	v, _ := url.ParseQuery(string(corpo))
	v.Del("sign")
	for nome, c := range map[string][]byte{"sem sign": []byte(v.Encode()), "vazio": nil, "lixo": []byte("%zz=%")} {
		if _, ok := p.AutenticarWebhook(http.Header{}, c, []credencialProvedor{cred}); ok {
			t.Errorf("%s foi aceite", nome)
		}
	}
	if _, ok := p.AutenticarWebhook(http.Header{}, corpo, nil); ok {
		t.Fatal("sem candidatos não há dono")
	}
	// O '+' do base64 que chegou sem urlencode (virou espaço) ainda verifica.
	semEscape := strings.ReplaceAll(string(corpo), "%2B", "+")
	if _, ok := p.AutenticarWebhook(http.Header{}, []byte(semEscape), []credencialProvedor{cred}); !ok {
		t.Fatal("sign com '+' sem urlencode devia verificar")
	}
	// Chave pública inválida num candidato não derruba os outros.
	lixo := credencialProvedor{Valores: map[string]string{"chave_publica_paypay": "lixo"}}
	if i, ok := p.AutenticarWebhook(http.Header{}, corpo, []credencialProvedor{lixo, cred}); !ok || i != 1 {
		t.Fatalf("candidato inválido: i=%d ok=%v", i, ok)
	}
}

func TestPayPayWebhookInterpretacao(t *testing.T) {
	p := paypayProvedorDeTeste(t, nil)
	_, plataforma, _ := paypayChaves(t)

	corpo := paypayNotificacao(t, plataforma, camposNotificacao("T100", "TRADE_FINISHED", "N-77"))
	id, payload, err := p.InterpretarWebhook("application/x-www-form-urlencoded", corpo)
	if err != nil || id != "T100" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if _, ok := payload["sign"]; ok {
		t.Fatal("a assinatura não deve ir para o payload")
	}
	if p.PedidoDoWebhook(corpo) != "T100" {
		t.Fatal("PedidoDoWebhook")
	}
	if p.ChaveEventoWebhook(payload) != "notify:N-77" {
		t.Fatalf("chave de evento = %q", p.ChaveEventoWebhook(payload))
	}
	// Duas notificações do mesmo pedido têm chaves diferentes (senão a segunda,
	// a que confirma o pagamento, seria descartada como repetição).
	_, outro, _ := p.InterpretarWebhook("", paypayNotificacao(t, plataforma, camposNotificacao("T100", "TRADE_SUCCESS", "N-78")))
	if p.ChaveEventoWebhook(outro) == p.ChaveEventoWebhook(payload) {
		t.Fatal("notificações diferentes do mesmo pedido não podem partilhar a chave")
	}
	// Sem notify_id: pedido + estado.
	semID := camposNotificacao("T100", "TRADE_CLOSED", "")
	_, pl, _ := p.InterpretarWebhook("", paypayNotificacao(t, plataforma, semID))
	if got := p.ChaveEventoWebhook(pl); got != "pedido:T100:TRADE_CLOSED" {
		t.Fatalf("chave sem notify_id = %q", got)
	}
	// Sem out_trade_no: inválida.
	if _, _, err = p.InterpretarWebhook("", []byte("status=TRADE_FINISHED")); err == nil {
		t.Fatal("notificação sem out_trade_no deve ser recusada")
	}

	// Resultado: só TRADE_FINISHED quita; TRADE_SUCCESS e reembolsos não.
	if o, tradeNo := p.ResultadoWebhook(payload); o.Status != "Success" || tradeNo != "PPT100" {
		t.Fatalf("finished: %+v %q", o, tradeNo)
	}
	if o, _ := p.ResultadoWebhook(outro); o.Status != "" {
		t.Fatalf("TRADE_SUCCESS não pode mudar o estado: %+v", o)
	}
	reembolso := camposNotificacao("T100", "TRADE_FINISHED", "N-90")
	reembolso["orig_out_trade_no"] = "T100"
	_, plReembolso, _ := p.InterpretarWebhook("", paypayNotificacao(t, plataforma, reembolso))
	if o, _ := p.ResultadoWebhook(plReembolso); o.Status != "" {
		t.Fatalf("notificação de reembolso não pode quitar a cobrança: %+v", o)
	}
	transferencia := camposNotificacao("T100", "TRADE_FINISHED", "N-91")
	transferencia["notify_type"] = "transfer_status_sync"
	_, plTransf, _ := p.InterpretarWebhook("", paypayNotificacao(t, plataforma, transferencia))
	if o, _ := p.ResultadoWebhook(plTransf); o.Status != "" {
		t.Fatalf("outro tipo de notificação não pode quitar: %+v", o)
	}
	fechado := camposNotificacao("T100", "TRADE_CLOSED", "N-92")
	fechado["failCode"], fechado["failReason"] = "E42", "recusado pelo banco"
	_, plFechado, _ := p.InterpretarWebhook("", paypayNotificacao(t, plataforma, fechado))
	if o, _ := p.ResultadoWebhook(plFechado); o.Status != "Failed" || o.Message != "recusado pelo banco" {
		t.Fatalf("fechado com motivo: %+v", o)
	}

	// Resposta exigida pelo PayPay: exatamente "success".
	if st, ct, corpoResp := p.RespostaWebhook(true); st != 200 || corpoResp != "success" || !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("resposta = %d %q %q", st, ct, corpoResp)
	}
	if p.UsaSegredoWebhook() {
		t.Fatal("o PayPay não usa segredo gerado pelo Spuri")
	}
	if r := p.RotasWebhook(); len(r) != 1 || r[0].Caminho != "notificacoes" || r[0].Metodo != MetodoQualquer {
		t.Fatalf("rotas = %+v", r)
	}
}

func TestPayPayPoliticasDeReconciliacaoSaoValidas(t *testing.T) {
	p := paypayProvedorDeTeste(t, nil)
	for _, m := range []string{MetodoGPO, MetodoREF} {
		pol, ok := p.PoliticaReconciliacao(m)
		if !ok || !pol.valida() {
			t.Fatalf("política do %s inválida: %+v", m, pol)
		}
		if got := politicaDaCobranca(p, m); got.IdadeMaxima != pol.IdadeMaxima {
			t.Fatalf("o núcleo não usou a política do PayPay para %s", m)
		}
	}
	gpo, _ := p.PoliticaReconciliacao(MetodoGPO)
	ref, _ := p.PoliticaReconciliacao(MetodoREF)
	if gpo.IdadeMaxima != 24*time.Hour || ref.IdadeMaxima != 30*24*time.Hour {
		t.Fatalf("idades máximas: GPO=%v REF=%v", gpo.IdadeMaxima, ref.IdadeMaxima)
	}
	if _, ok := p.PoliticaReconciliacao(MetodoGPOQR); ok {
		t.Fatal("GPO_QR não existe no PayPay")
	}
	// Consulta cedo e espaça com a idade.
	if i, _ := gpo.IntervaloPara(time.Minute); i != 3*time.Minute {
		t.Fatalf("intervalo inicial GPO = %v", i)
	}
	if _, ok := gpo.IntervaloPara(25 * time.Hour); ok {
		t.Fatal("Express deve sair da reconciliação depois de 24 h")
	}
}

func TestPayPayCatalogoDeclaraMetodosCamposEWebhook(t *testing.T) {
	var info InfoProvedor
	found := false
	for _, i := range NewService(nil).Provedores() {
		if i.ID == ProvedorPayPay {
			info, found = i, true
		}
	}
	if !found {
		t.Fatal("PayPay não está no catálogo")
	}
	if info.Nome != "PayPay" || info.Slug != "paypay" || strings.Join(info.Metodos, ",") != "REF,GPO" {
		t.Fatalf("info = %+v", info)
	}
	chaves := []string{}
	for _, c := range info.Campos {
		chaves = append(chaves, c.Chave)
		if !c.Obrigatorio {
			t.Fatalf("campo %s deveria ser obrigatório", c.Chave)
		}
	}
	if strings.Join(chaves, ",") != "partner_id,sale_product_code,chave_privada,chave_publica_paypay" {
		t.Fatalf("campos = %v", chaves)
	}
	if info.UsaSegredoWebhook || info.CabecalhoWebhook != "" {
		t.Fatalf("o PayPay não usa segredo/cabeçalho de webhook: %+v", info)
	}
	if strings.Join(info.Webhooks, ",") != "/webhooks/paypay/notificacoes" {
		t.Fatalf("webhooks = %v", info.Webhooks)
	}
	// O catálogo continua a listar a AppyPay primeiro, com as URLs de sempre.
	todos := NewService(nil).Provedores()
	if todos[0].ID != ProvedorAppyPay || strings.Join(todos[0].Webhooks, ",") != "/webhooks/appypay/gpo,/webhooks/appypay/ref" {
		t.Fatalf("AppyPay mudou: %+v", todos[0])
	}
}
