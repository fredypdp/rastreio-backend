package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/finance"
)

type consultaCobrancaMockTransport struct{ status string }

func (t *consultaCobrancaMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// provider_id único por criação: financeiro_cobrancas tem índice único em
	// provider_id e o ledger é append-only, então um id fixo faria a segunda
	// execução da suíte no mesmo banco falhar (e envenenaria os Rebuild()).
	body := `{"id":"provider-charge-consulta-` + uuid.NewString() + `","status":"Pending"}`
	switch {
	case strings.Contains(req.URL.Path, "/oauth2/token"):
		body = `{"access_token":"test-token","expires_in":3600}`
	case req.Method == http.MethodGet:
		// GET /charges/{id}: devolve o id pedido na própria URL.
		body = `{"id":"` + req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:] + `","status":"` + t.status + `"}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// TestIntegrationConsultarCobrancaAppyPayPersisteSuccess reproduz o fluxo de
// polling: a academia consulta o status de uma cobrança GPO/REF pelo endpoint
// genérico GET /financeiro/appypay/cobrancas/:id (finance.Service.ConsultCharge,
// o mesmo caminho dos pagamentos de mensalidade). A cobrança é criada como
// "Pending" (comportamento real de GPO/REF: a criação nunca retorna "Success"
// de forma síncrona) e só depois passa a "Success" na consulta, que precisa
// ficar persistida no read model.
func TestIntegrationConsultarCobrancaAppyPayPersisteSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	t.Setenv("ENV", "test")
	t.Setenv("APPYPAY_RESOURCE", "integration-resource")
	t.Setenv("JWT_SECRET", "test-only-secret-material-at-least-32")

	academia := "WC" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	seedAcademiaParaMatriculaWebhook(t, client, academia)

	transport := &consultaCobrancaMockTransport{status: "Pending"}
	service := finance.NewService(client)
	service.SetHTTPClient(&http.Client{Transport: transport})
	if _, _, err := service.ConfigureCredential(context.Background(), nil, finance.CredentialInput{
		ContextoTipo: finance.ContextoAcademia, CodigoAcademia: academia,
		Provedor: finance.ProvedorAppyPay, Credenciais: map[string]string{"client_id": "integration-client", "client_secret": "integration-secret", "gpo_method": "GPO_INTEGRATION", "ref_method": "REF_INTEGRATION"},
	}, "integration-test", "sistema", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}

	charge := novaCobrancaAvulsaREF(t, service, academia, 750)
	if strings.EqualFold(charge.Status, "success") {
		t.Fatalf("cobrança REF retornou success na criação, cenário não reproduz o fluxo real (deveria ser Pending): %q", charge.Status)
	}

	previousService := FinanceiroService
	FinanceiroService = service
	t.Cleanup(func() { FinanceiroService = previousService })

	// AppyPay confirma o pagamento de forma assíncrona (pagamento na
	// referência bancária). A academia/candidato descobre isso consultando o
	// status da cobrança pelo endpoint genérico, exatamente como o teste da
	// tarefa 42 fez para mensalidades via ConsultCharge.
	transport.status = "Success"
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/financeiro/appypay/cobrancas/"+charge.ID.String(), nil)
	ctx.Params = gin.Params{{Key: "id", Value: charge.ID.String()}}
	ctx.Set("dbClient", client)
	ctx.Set("repository", db.NewAggregateRepository(client))
	ctx.Set("user_id", uuid.New())
	ctx.Set("user_type", "academia")
	ctx.Set("codigo_academia", academia)

	ConsultarCobranca(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("ConsultarCobrancaAppyPay retornou %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(strings.ToLower(recorder.Body.String()), "success") {
		t.Fatalf("consulta não refletiu o status Success da AppyPay: %s", recorder.Body.String())
	}

	// A AppyPay confirmou o pagamento (a consulta acima devolveu "Success"):
	// o estado tem de estar persistido em financeiro_cobrancas.
	var status string
	if err := client.DB().QueryRow(`SELECT payload->>'status' FROM financeiro_cobrancas WHERE id=$1`, charge.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "Success" {
		t.Fatalf("status persistido da cobrança = %q, esperado Success após a consulta confirmar o pagamento", status)
	}
}
