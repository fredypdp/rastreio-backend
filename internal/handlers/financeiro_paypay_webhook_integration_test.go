package handlers

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/finance"
	"spuri/internal/handlers/apoio"
	"spuri/internal/handlers/testsupport"
)

// handlerPayPayRoteado é um PayPay mínimo: a primeira chamada (criação do pedido)
// responde "P"; as seguintes (consultas ao vivo) respondem com o estado do
// teste. A verificação de assinatura e de cifra dos pedidos está coberta em
// internal/finance (provedor_paypay_test.go); aqui o que se prova é o ENDPOINT
// PÚBLICO de notificações.
type handlerPayPayRoteado struct {
	chamadas int
	estado   string
}

func (m *handlerPayPayRoteado) RoundTrip(req *http.Request) (*http.Response, error) {
	m.chamadas++
	body := `{"code":"S0001","msg":"Success","biz_content":{"out_trade_no":"x","trade_no":"PPH` + strings.ReplaceAll(uuid.NewString(), "-", "")[:10] + `","status":"P","reference_id":"123456789","entity_id":"00123"}}`
	if m.chamadas > 1 {
		body = `{"code":"S0001","msg":"Success","biz_content":{"out_trade_no":"x","trade_no":"PPQ","status":"` + m.estado + `"}}`
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func chaveRSA(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func notificacaoPayPayAssinada(t *testing.T, plataforma *rsa.PrivateKey, campos map[string]string) []byte {
	t.Helper()
	chaves := []string{}
	for k, v := range campos {
		if v != "" {
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

func TestIntegrationReceberWebhookPayPayAutenticaPorAssinaturaEConfirma(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := testsupport.IntegrationFinanceClient(t)
	t.Setenv("ENV", "test")
	t.Setenv("APPYPAY_RESOURCE", "integration-resource")
	t.Setenv("JWT_SECRET", "test-only-secret-material-at-least-32")
	t.Setenv("PAYPAY_API_URL", "https://paypay.test/gateway")

	academia := "PH" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	seedAcademiaParaMatriculaWebhook(t, client, academia)
	mock := &handlerPayPayRoteado{estado: "TRADE_FINISHED"}
	service := finance.NewService(client)
	service.SetHTTPClient(&http.Client{Transport: mock})

	comerciante, plataforma, intrusa := chaveRSA(t), chaveRSA(t), chaveRSA(t)
	privDER, _ := x509.MarshalPKCS8PrivateKey(comerciante)
	pubDER, _ := x509.MarshalPKIXPublicKey(&plataforma.PublicKey)
	if _, secret, err := service.ConfigureCredential(context.Background(), nil, finance.CredentialInput{ContextoTipo: finance.ContextoAcademia, CodigoAcademia: academia, Provedor: finance.ProvedorPayPay, Credenciais: map[string]string{
		"partner_id": "2088000000000001", "sale_product_code": "123456789",
		"chave_privada":        base64.StdEncoding.EncodeToString(privDER),
		"chave_publica_paypay": base64.StdEncoding.EncodeToString(pubDER),
	}}, "integration-test", "sistema", "127.0.0.1"); err != nil || secret != "" {
		t.Fatalf("credenciais PayPay: %v (secret=%q)", err, secret)
	}
	mock.chamadas = 0
	charge, err := service.CreateCharge(context.Background(), finance.ChargeRequest{ContextoTipo: finance.ContextoAcademia, CodigoAcademia: academia, Amount: 1500, Currency: "AOA", Description: "Cobrança avulsa PayPay", MerchantTransactionID: "HPP" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12], PaymentMethod: "REF"}, "integration-test", "sistema", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	previous := apoio.FinanceiroService
	apoio.FinanceiroService = service
	t.Cleanup(func() { apoio.FinanceiroService = previous })
	repository := db.NewAggregateRepository(client)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("dbClient", client); c.Set("repository", repository) })
	// Mesmo registo de rotas que cmd/server/main.go.
	registadas := []string{}
	for _, p := range service.Provedores() {
		for _, rota := range service.RotasWebhookDoProvedor(p.ID) {
			router.POST("/webhooks/"+p.Slug+"/"+rota.Caminho, ReceberWebhook(p.ID, rota.Metodo))
			registadas = append(registadas, "/webhooks/"+p.Slug+"/"+rota.Caminho)
		}
	}
	if !strings.Contains(strings.Join(registadas, ","), "/webhooks/paypay/notificacoes") {
		t.Fatalf("rota do PayPay não registada: %v", registadas)
	}

	campos := func(estado, notifyID string) map[string]string {
		return map[string]string{"notify_type": "trade_status_sync", "notify_id": notifyID, "out_trade_no": charge.MerchantTransactionID, "inner_trade_no": charge.ProviderChargeID, "status": estado, "amount": "1500.00", "notify_time": "2026-11-26 10:00:00"}
	}
	post := func(corpo []byte) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/webhooks/paypay/notificacoes", bytes.NewReader(corpo))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		router.ServeHTTP(rec, req)
		return rec
	}
	estado := func() string {
		var s string
		if err := client.DB().QueryRow(`SELECT payload->>'status' FROM financeiro_cobrancas WHERE id=$1`, charge.ID).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	// Assinada por outra chave: 401 e nada muda.
	if rec := post(notificacaoPayPayAssinada(t, intrusa, campos("TRADE_FINISHED", "N-"+uuid.NewString()[:8]))); rec.Code != http.StatusUnauthorized {
		t.Fatalf("notificação falsa devolveu %d, queria 401", rec.Code)
	}
	// Sem assinatura: 401.
	if rec := post([]byte(url.Values{"out_trade_no": {charge.MerchantTransactionID}, "status": {"TRADE_FINISHED"}}.Encode())); rec.Code != http.StatusUnauthorized {
		t.Fatalf("notificação sem assinatura devolveu %d, queria 401", rec.Code)
	}
	if estado() == "Success" {
		t.Fatal("notificações recusadas não podem alterar a cobrança")
	}
	// TRADE_SUCCESS (aviso): o PayPay exige a resposta exata "success", sem
	// espaços nem HTML, e a cobrança não muda.
	rec := post(notificacaoPayPayAssinada(t, plataforma, campos("TRADE_SUCCESS", "N-"+uuid.NewString()[:8])))
	if rec.Code != http.StatusOK || rec.Body.String() != "success" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("resposta ao PayPay = %d %q (%s)", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
	if estado() == "Success" {
		t.Fatal("TRADE_SUCCESS não pode quitar a cobrança")
	}
	// TRADE_FINISHED legítimo: confirma (a consulta ao vivo concorda).
	confirmacao := notificacaoPayPayAssinada(t, plataforma, campos("TRADE_FINISHED", "N-"+uuid.NewString()[:8]))
	rec = post(confirmacao)
	if rec.Code != http.StatusOK || rec.Body.String() != "success" {
		t.Fatalf("confirmação devolveu %d %q", rec.Code, rec.Body.String())
	}
	if estado() != "Success" {
		t.Fatalf("estado = %s, queria Success", estado())
	}
	// O PayPay repete a mesma notificação: continua a receber "success" e o
	// ledger guarda uma só.
	for i := 0; i < 3; i++ {
		if rec = post(confirmacao); rec.Code != http.StatusOK || rec.Body.String() != "success" {
			t.Fatalf("repetição %d devolveu %d %q", i, rec.Code, rec.Body.String())
		}
	}
	var recebidos int
	if err := client.DB().QueryRow(`SELECT COUNT(*) FROM spuri_ledger WHERE aggregate_type='Financeiro' AND event_type='WebhookProvedorRecebido' AND payload->>'cobranca'=$1 AND payload->>'provedor'='PAYPAY'`, charge.MerchantTransactionID).Scan(&recebidos); err != nil {
		t.Fatal(err)
	}
	if recebidos != 2 { // TRADE_SUCCESS + TRADE_FINISHED; as repetições não contam
		t.Fatalf("webhooks gravados = %d, queria 2", recebidos)
	}
}
