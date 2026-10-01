package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"spuri/internal/db"
	"spuri/internal/finance"
	"spuri/internal/projections"
)

// handlerStatusTransport simula a AppyPay com um status de consulta mutável
// (GET /charges/{id}) e um provider id único por cobrança criada.
type handlerStatusTransport struct{ status *string }

func (t handlerStatusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := `{"id":"provider-charge-ext-` + uuid.NewString() + `","status":"Pending"}`
	switch {
	case strings.Contains(req.URL.Path, "/oauth2/token"):
		body = `{"access_token":"test-token","expires_in":3600}`
	case req.Method == http.MethodGet:
		providerID := strings.TrimPrefix(req.URL.EscapedPath(), "/v2.0/charges/")
		body = `{"payment":{"id":"` + providerID + `","status":"` + *t.status + `","transactionEvents":[{"responseStatus":{"successful":true,"status":"` + *t.status + `","source":"REF"}}]}}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

type pagamentoExternoHandlerFixture struct {
	client          *db.Client
	router          *gin.Engine
	academia        string
	codigo          string // solicitação de matrícula
	codigoEstudante string
	chargeID        string
	providerStatus  *string
}

func newPagamentoExternoHandlerRouter(client *db.Client, userType, academia string) *gin.Engine {
	repository := db.NewAggregateRepository(client)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("dbClient", client)
		c.Set("repository", repository)
		c.Set("user_id", uuid.New())
		c.Set("user_type", userType)
		c.Set("codigo_academia", academia)
	})
	router.POST("/financeiro/appypay/cobrancas/:id/pago-externamente", RegistrarPagamentoExternoCobranca)
	router.POST("/financeiro/appypay/cobrancas/:id/cancelar", CancelarCobrancaAppyPay)
	router.POST("/financeiro/mensalidades/obrigacoes/pago-externamente", RegistrarPagamentoExternoMensalidades)
	return router
}

// newMatriculaPendenteFixture cria uma academia, uma solicitação de matrícula
// aprovada aguardando pagamento e uma cobrança REF aguardando pagamento.
func newMatriculaPendenteFixture(t *testing.T) *pagamentoExternoHandlerFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	t.Setenv("ENV", "test")
	t.Setenv("APPYPAY_RESOURCE", "integration-resource")
	t.Setenv("JWT_SECRET", "test-only-secret-material-at-least-32")

	academia := "PE" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	seedAcademiaParaMatriculaWebhook(t, client, academia)
	codigo, codigoEstudante := seedSolicitacaoMatriculaPendenteComLedger(t, client, academia, 750)

	status := "Pending"
	service := finance.NewService(client)
	service.SetHTTPClient(&http.Client{Transport: handlerStatusTransport{status: &status}})
	if _, _, err := service.ConfigureCredential(context.Background(), nil, finance.CredentialInput{
		ContextoTipo: finance.ContextoAcademia, CodigoAcademia: academia, ClientID: "integration-client", ClientSecret: "integration-secret",
		GPOPaymentMethod: "GPO_INTEGRATION", REFPaymentMethod: "REF_INTEGRATION",
	}, "integration-test", "sistema", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	charge, err := service.IniciarPagamentoMatricula(context.Background(), finance.MatriculaPagamentoInput{CodigoSolicitacao: codigo, MetodoPagamento: "REF"}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	previous := FinanceiroService
	FinanceiroService = service
	t.Cleanup(func() { FinanceiroService = previous })

	return &pagamentoExternoHandlerFixture{
		client: client, router: newPagamentoExternoHandlerRouter(client, "academia", academia), academia: academia,
		codigo: codigo, codigoEstudante: codigoEstudante, chargeID: charge.Charge.ID.String(), providerStatus: &status,
	}
}

func (f *pagamentoExternoHandlerFixture) post(path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	f.router.ServeHTTP(rec, req)
	return rec
}

func (f *pagamentoExternoHandlerFixture) estudantes(t *testing.T) int {
	t.Helper()
	if err := projections.NewSolicitacaoMatriculaProjection(f.client).Rebuild(); err != nil {
		t.Fatal(err)
	}
	if err := projections.NewEstudanteProjection(f.client).Rebuild(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.client.DB().QueryRow(`SELECT COUNT(*) FROM projection_estudantes WHERE codigo_estudante=$1 AND codigo_academia=$2`, f.codigoEstudante, f.academia).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *pagamentoExternoHandlerFixture) eventos(t *testing.T, evento string) int {
	t.Helper()
	var n int
	if err := f.client.DB().QueryRow(`SELECT COUNT(*) FROM spuri_ledger WHERE aggregate_id=$1 AND event_type=$2`, f.chargeID, evento).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// O pagamento marcado como externo dispara os MESMOS efeitos do webhook: a
// matrícula é efetivada e o estudante criado, exatamente uma vez.
func TestIntegrationPagamentoExternoMatriculaEfetivaVinculoComoWebhook(t *testing.T) {
	f := newMatriculaPendenteFixture(t)

	rec := f.post("/financeiro/appypay/cobrancas/"+f.chargeID+"/pago-externamente", `{"referencia_externa":"DEP-123","observacao":"depósito no balcão"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out["status"] != "Success" {
		t.Fatalf("resposta inesperada: %s (%v)", rec.Body.String(), err)
	}
	if n := f.estudantes(t); n != 1 {
		t.Fatalf("a matrícula deveria ter sido efetivada (1 estudante), obteve %d", n)
	}
	var status string
	if err := f.client.DB().QueryRow(`SELECT status FROM projection_solicitacoes_matricula WHERE codigo_solicitacao=$1`, f.codigo).Scan(&status); err != nil || status != "aprovada" {
		t.Fatalf("status da solicitação = %q (%v), queria aprovada", status, err)
	}
	if n := f.eventos(t, "CobrancaPagamentoExternoRegistrado"); n != 1 {
		t.Fatalf("esperava 1 evento de pagamento externo, obteve %d", n)
	}

	// repetir é idempotente: 200, ja_registrado, sem segundo estudante nem segundo evento
	rec = f.post("/financeiro/appypay/cobrancas/"+f.chargeID+"/pago-externamente", `{}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ja_registrado":true`) {
		t.Fatalf("repetição: status = %d: %s", rec.Code, rec.Body.String())
	}
	if n := f.estudantes(t); n != 1 {
		t.Fatalf("a repetição duplicou o estudante: %d", n)
	}
	if n := f.eventos(t, "CobrancaPagamentoExternoRegistrado"); n != 1 {
		t.Fatalf("a repetição gravou outro evento: %d", n)
	}
	// uma cobrança paga fora da plataforma não pode ser cancelada
	rec = f.post("/financeiro/appypay/cobrancas/"+f.chargeID+"/cancelar", `{}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("cancelar cobrança paga: status = %d, queria 409: %s", rec.Code, rec.Body.String())
	}
}

// Se a AppyPay já confirmou o pagamento (aguardando confirmação local), o
// endpoint responde 409 — mas ainda completa os efeitos que o webhook faria.
func TestIntegrationPagamentoExternoComProvedorJaPagoRespondeConflitoECompletaEfeitos(t *testing.T) {
	f := newMatriculaPendenteFixture(t)
	*f.providerStatus = "Success"

	rec := f.post("/financeiro/appypay/cobrancas/"+f.chargeID+"/pago-externamente", `{}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, queria 409: %s", rec.Code, rec.Body.String())
	}
	if n := f.eventos(t, "CobrancaPagamentoExternoRegistrado"); n != 0 {
		t.Fatalf("não deveria existir evento de pagamento externo: %d", n)
	}
	if n := f.estudantes(t); n != 1 {
		t.Fatalf("o pagamento já confirmado no provedor deveria ter efetivado a matrícula, obteve %d estudantes", n)
	}
}

// Cancelar uma cobrança cujo pagamento já está no provedor devolve 409 e
// completa os efeitos (antes da correção a matrícula ficava sem efetivar).
func TestIntegrationCancelarCobrancaComPagamentoNoProvedorCompletaEfeitos(t *testing.T) {
	f := newMatriculaPendenteFixture(t)
	*f.providerStatus = "Success"

	rec := f.post("/financeiro/appypay/cobrancas/"+f.chargeID+"/cancelar", `{"motivo":"teste"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, queria 409: %s", rec.Code, rec.Body.String())
	}
	if n := f.estudantes(t); n != 1 {
		t.Fatalf("o pagamento descoberto no cancelamento deveria ter efetivado a matrícula, obteve %d estudantes", n)
	}
}

func TestIntegrationPagamentoExternoSoAcademiaDona(t *testing.T) {
	f := newMatriculaPendenteFixture(t)
	for _, role := range []string{"admin", "estudante", "gerente"} {
		router := newPagamentoExternoHandlerRouter(f.client, role, f.academia)
		for _, path := range []string{
			"/financeiro/appypay/cobrancas/" + f.chargeID + "/pago-externamente",
			"/financeiro/mensalidades/obrigacoes/pago-externamente",
		} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{"codigo_estudante":"X","meses":[{"ano_letivo":"2025_2026","mes":9}]}`))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s em %s: status = %d, queria 403: %s", role, path, rec.Code, rec.Body.String())
			}
		}
	}
	if n := f.eventos(t, "CobrancaPagamentoExternoRegistrado"); n != 0 {
		t.Fatalf("nada deveria ter sido gravado: %d", n)
	}
}

func TestIntegrationPagamentoExternoMensalidadesExigeVinculoDoEstudante(t *testing.T) {
	f := newMatriculaPendenteFixture(t)
	rec := f.post("/financeiro/mensalidades/obrigacoes/pago-externamente", `{"codigo_estudante":"EST-INEXISTENTE","meses":[{"ano_letivo":"2025_2026","mes":9}]}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, queria 403: %s", rec.Code, rec.Body.String())
	}
}
