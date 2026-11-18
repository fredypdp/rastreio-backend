package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/finance"
	"spuri/internal/projections"
)

type handlerAppyPayMockTransport struct{}

func (handlerAppyPayMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// provider_id único por criação (índice único em financeiro_cobrancas e
	// ledger append-only: um id fixo quebra a segunda execução no mesmo banco).
	body := `{"id":"provider-charge-handler-` + uuid.NewString() + `","status":"Pending"}`
	switch {
	case strings.Contains(req.URL.Path, "/oauth2/token"):
		body = `{"access_token":"test-token","expires_in":3600}`
	case req.Method == http.MethodGet:
		// Formato real de GET /charges/{id} (seção "Get a charge" da
		// documentação AppyPay): o status vem dentro de "payment.status",
		// nunca num campo solto "status" na raiz — diferente do corpo
		// simplificado usado acima para a criação da cobrança (que este
		// teste não usa para decidir nada). AcceptWebhook confirma um
		// webhook de sucesso com exatamente este GET antes de aplicar um
		// efeito irreversível (ver liveChargeStatus em
		// internal/finance/appypay.go); devolver aqui o mesmo resultado que
		// o webhook do teste relata (Success) reflete o cenário sendo
		// testado — a AppyPay já confirmou o pagamento.
		body = `{"payment":{"id":"` + req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:] + `","status":"Success","transactionEvents":[{"responseStatus":{"successful":true,"status":"Success","code":100,"source":"REF"}}]}}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// seedAcademiaParaMatriculaWebhook cria uma academia real (agregado ->
// event store -> projeção) para os testes de webhook AppyPay.
//
// ATENÇÃO — bug pré-existente corrigido nesta tarefa: esta função inseria a
// academia DIRETO via SQL em projection_academias, sem nenhum evento
// correspondente no event store (spuri_ledger). Isso funcionava para o
// PRÓPRIO teste (que nunca chama AcademiaProjection.Rebuild()), mas o
// estudante criado logo a seguir, via webhook, É gerado por um evento REAL
// (EstudanteCriadoComVinculo, referenciando esta academia pelo código). Se
// QUALQUER outro teste do pacote chamasse depois AcademiaProjection.Rebuild()
// (TRUNCATE + replay só dos eventos reais — ver admin_projection.go /
// academia_projection.go), a linha desta academia "fantasma" era apagada
// para sempre (sem evento para repô-la), deixando o estudante do webhook
// com uma academia órfã permanentemente. Qualquer EstudanteProjection.Rebuild()
// posterior (em QUALQUER outro teste, ex.: os de edição de BI) passava a
// falhar com "academia ainda não projetada" de forma aparentemente
// intermitente (na verdade dependia só da ordem de execução dos testes).
// Criar a academia pelo caminho normal de event sourcing resolve isso: ela
// passa a ser reconstruível por qualquer Rebuild() futuro, como qualquer
// outra academia real.
func seedAcademiaParaMatriculaWebhook(t *testing.T, client *db.Client, codigo string) {
	t.Helper()
	id := uuid.New()
	agg := &aggregates.Academia{}
	agg.SetID(id)
	nif := fmt.Sprintf("9%09d", time.Now().UnixNano()%1000000000)
	if err := agg.Criar("escola", "private", "Academia webhook", nif, codigo, "hash", "LUA", "endereco", nil, nil, nil, ptrString("fundamental"), nil, []string{"1_ano_fundamental"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := agg.DefinirAnoLetivo("2026_2027", "escolar", uuid.New()); err != nil {
		t.Fatal(err)
	}
	repository := db.NewAggregateRepository(client)
	if err := repository.SaveWithAudit(agg, db.AuditContext{UserID: "integration-test", UserType: "sistema", IP: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if err := projections.NewAcademiaProjection(client).Rebuild(); err != nil {
		t.Fatal(err)
	}
}

func ptrString(s string) *string { return &s }

func geraDigitos(n int) string {
	digitos := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, uuid.NewString())
	for len(digitos) < n {
		digitos += "0"
	}
	return digitos[:n]
}

// novaCobrancaAvulsaREF emite uma cobrança REF avulsa (sem mensalidade nem
// serviço extra) pelo caminho normal do serviço financeiro. Os testes de
// webhook, consulta e pagamento externo só precisam de "uma cobrança aguardando
// pagamento" de uma academia já configurada.
func novaCobrancaAvulsaREF(t *testing.T, service *finance.Service, academia string, valor float64) finance.ChargeResult {
	t.Helper()
	charge, err := service.CreateCharge(context.Background(), finance.ChargeRequest{
		ContextoTipo:          finance.ContextoAcademia,
		CodigoAcademia:        academia,
		Amount:                valor,
		Currency:              "AOA",
		Description:           "Cobrança avulsa de teste de integração",
		MerchantTransactionID: "HND" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		PaymentMethod:         "REF",
		PaymentInfo:           map[string]any{},
	}, "integration-test", "sistema", "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateCharge falhou: %v", err)
	}
	return charge
}

func integrationFinanceClient(t *testing.T) *db.Client {
	t.Helper()
	if os.Getenv("RUN_POSTGRES_INTEGRATION") != "1" {
		t.Skip("teste de integração requer RUN_POSTGRES_INTEGRATION=1 e PostgreSQL")
	}
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDir) })
	client, err := db.NewClient(db.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err = client.RunMigrations(); err != nil {
		t.Fatal(err)
	}
	return client
}

func TestIntegrationFinanceRejectsAcademyChargeOutsideScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	merchant := "INTSCOPE" + uuid.NewString()[:6]
	chargeID := uuid.New()
	payload, _ := json.Marshal(map[string]any{"status": "criada"})
	if _, err := client.DB().Exec(`INSERT INTO financeiro_cobrancas (id,merchant_transaction_id,contexto_tipo,codigo_academia,payload) VALUES ($1,$2,'academia','ACA1',$3)`, chargeID, merchant, payload); err != nil {
		t.Fatal(err)
	}

	previousService := FinanceiroService
	FinanceiroService = finance.NewService(client)
	t.Cleanup(func() { FinanceiroService = previousService })
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/financeiro/appypay/cobrancas/"+merchant, nil)
	ctx.Params = gin.Params{{Key: "id", Value: merchant}}
	ctx.Set("dbClient", client)
	ctx.Set("user_id", uuid.New())
	ctx.Set("user_type", "academia")
	ctx.Set("codigo_academia", "ACA2")

	ConsultarCobranca(ctx)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("academia externa recebeu status %d, quer 404: %s", recorder.Code, recorder.Body.String())
	}
}

func TestIntegrationFinanceRejectsNonFPPAdmins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	for _, role := range []string{"gerente", "adm"} {
		t.Run(role, func(t *testing.T) {
			adminID := uuid.New()
			if _, err := client.DB().Exec(`INSERT INTO projection_admins (id,nome,email,senha_hash,role,status,created_by) VALUES ($1,$2,$3,'hash',$4,'ativo',$1)`, adminID, role, role+"-"+uuid.NewString()+"@example.test", role); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = client.DB().Exec(`DELETE FROM projection_admins WHERE id=$1`, adminID) })
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/financeiro/appypay/cobrancas", bytes.NewBufferString(`{}`))
			ctx.Request.Header.Set("Content-Type", "application/json")
			ctx.Set("dbClient", client)
			ctx.Set("user_id", adminID)
			ctx.Set("user_type", "admin")

			CriarCobranca(ctx)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("admin %s recebeu status %d, quer 403: %s", role, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestIntegrationFinanceFPPAdminCannotCancelAcademyCharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	merchant := "INTCANCEL" + uuid.NewString()[:6]
	chargeID := uuid.New()
	payload, _ := json.Marshal(map[string]any{"status": "criada"})
	if _, err := client.DB().Exec(`INSERT INTO financeiro_cobrancas (id,merchant_transaction_id,contexto_tipo,codigo_academia,payload) VALUES ($1,$2,'academia','ACA-CANCEL',$3)`, chargeID, merchant, payload); err != nil {
		t.Fatal(err)
	}
	adminID := uuid.New()
	if _, err := client.DB().Exec(`INSERT INTO projection_admins (id,nome,email,senha_hash,role,status,created_by) VALUES ($1,'fpp-cancel',$2,'hash','fpp','ativo',$1)`, adminID, "fpp-cancel-"+uuid.NewString()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = client.DB().Exec(`DELETE FROM projection_admins WHERE id=$1`, adminID) })
	previousService := FinanceiroService
	FinanceiroService = finance.NewService(client)
	t.Cleanup(func() { FinanceiroService = previousService })
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/financeiro/appypay/cobrancas/"+merchant+"/cancelar", bytes.NewBufferString(`{}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: merchant}}
	ctx.Set("dbClient", client)
	ctx.Set("user_id", adminID)
	ctx.Set("user_type", "admin")

	CancelarCobranca(ctx)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("admin FPP recebeu status %d, quer 404: %s", recorder.Code, recorder.Body.String())
	}
}

func TestIntegrationReceberWebhookAppyPayConfirmaCobrancaEEhIdempotente(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	t.Setenv("ENV", "test")
	t.Setenv("APPYPAY_RESOURCE", "integration-resource")
	t.Setenv("JWT_SECRET", "test-only-secret-material-at-least-32")

	academia := "WH" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	seedAcademiaParaMatriculaWebhook(t, client, academia)

	service := finance.NewService(client)
	service.SetHTTPClient(&http.Client{Transport: handlerAppyPayMockTransport{}})
	_, webhookSecret, err := service.ConfigureCredential(context.Background(), nil, finance.CredentialInput{
		ContextoTipo:   finance.ContextoAcademia,
		CodigoAcademia: academia,
		Provedor:       finance.ProvedorAppyPay, Credenciais: map[string]string{"client_id": "integration-client", "client_secret": "integration-secret", "gpo_method": "GPO_INTEGRATION", "ref_method": "REF_INTEGRATION"},
	}, "integration-test", "sistema", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	charge := novaCobrancaAvulsaREF(t, service, academia, 750)

	previousService := FinanceiroService
	FinanceiroService = service
	t.Cleanup(func() { FinanceiroService = previousService })
	repository := db.NewAggregateRepository(client)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("dbClient", client)
		c.Set("repository", repository)
	})
	router.POST("/financeiro/appypay/webhooks/ref", ReceberWebhook(finance.ProvedorAppyPay, "REF"))

	eventID := charge.ProviderChargeID
	// Formato real de um webhook da AppyPay (ver seção "Merchant Webhooks" de
	// docs/Parceiros e integrações/AppyPay Documentação.md): o status vem
	// dentro de "responseStatus", nunca em um campo solto "status"/"state"
	// na raiz do payload.
	payload, _ := json.Marshal(map[string]any{
		"id":                    eventID,
		"merchantTransactionId": charge.MerchantTransactionID,
		"amount":                750,
		"responseStatus": map[string]any{
			"successful": true,
			"status":     "Success",
			"code":       100,
			"message":    "Transaction Approved",
			"source":     "REF",
		},
	})
	postWebhook := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/financeiro/appypay/webhooks/ref", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(finance.WebhookHeaderName, webhookSecret)
		router.ServeHTTP(rec, req)
		return rec
	}

	first := postWebhook()
	if first.Code != http.StatusOK {
		t.Fatalf("webhook retornou %d: %s", first.Code, first.Body.String())
	}
	var status string
	if err := client.DB().QueryRow(`SELECT payload->>'status' FROM financeiro_cobrancas WHERE id=$1`, charge.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "Success" {
		t.Fatalf("status da cobrança após o webhook = %q, queria Success", status)
	}
	second := postWebhook()
	if second.Code != http.StatusOK {
		t.Fatalf("webhook idempotente retornou %d: %s", second.Code, second.Body.String())
	}
	var recebidos int
	if err := client.DB().QueryRow(`SELECT COUNT(*) FROM spuri_ledger WHERE aggregate_type='Financeiro' AND event_type='WebhookProvedorRecebido' AND payload->>'event_id'=$1`, eventID).Scan(&recebidos); err != nil {
		t.Fatal(err)
	}
	if recebidos != 1 {
		t.Fatalf("webhook repetido gravou %d eventos no ledger, queria 1", recebidos)
	}
}
