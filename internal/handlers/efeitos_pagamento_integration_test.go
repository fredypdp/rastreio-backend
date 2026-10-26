package handlers

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"spuri/internal/db"
	"spuri/internal/finance"
	"spuri/internal/projections"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// O job de reconciliação confirma pagamentos FORA de uma requisição HTTP. Este
// teste prova que o adaptador dos efeitos (contexto Gin sintético) efetiva a
// matrícula exatamente como o webhook, e que repetir é inofensivo (os efeitos
// são repetidos pelo job até darem certo).
func TestIntegrationEfeitosPagamentoDoJobEfetivamMatriculaForaDeUmaRequisicao(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	t.Setenv("ENV", "test")
	t.Setenv("APPYPAY_RESOURCE", "integration-resource")
	t.Setenv("JWT_SECRET", "test-only-secret-material-at-least-32")

	academia := "EJ" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	seedAcademiaParaMatriculaWebhook(t, client, academia)
	codigo, codigoEstudante := seedSolicitacaoMatriculaPendenteComLedger(t, client, academia, 750)

	service := finance.NewService(client)
	service.SetHTTPClient(&http.Client{Transport: handlerAppyPayMockTransport{}})
	if _, _, err := service.ConfigureCredential(context.Background(), nil, finance.CredentialInput{
		ContextoTipo:   finance.ContextoAcademia,
		CodigoAcademia: academia,
		Provedor:       finance.ProvedorAppyPay, Credenciais: map[string]string{"client_id": "integration-client", "client_secret": "integration-secret", "gpo_method": "GPO_INTEGRATION", "ref_method": "REF_INTEGRATION"},
	}, "integration-test", "sistema", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	charge, err := service.IniciarPagamentoMatricula(context.Background(), finance.MatriculaPagamentoInput{CodigoSolicitacao: codigo, MetodoPagamento: "REF"}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	previousService := FinanceiroService
	FinanceiroService = service
	t.Cleanup(func() { FinanceiroService = previousService })

	efeitos := NovosEfeitosPagamento(client, db.NewAggregateRepository(client))
	confirmada := finance.CobrancaConfirmada{
		ID:                    charge.Charge.ID,
		ProviderChargeID:      charge.Charge.ProviderChargeID,
		MerchantTransactionID: charge.Charge.MerchantTransactionID,
		Provedor:              finance.ProvedorAppyPay,
	}
	for i, tentativa := range []string{"primeira", "repetida (idempotente)"} {
		if err := efeitos(context.Background(), confirmada); err != nil {
			t.Fatalf("efeitos, %s: %v", tentativa, err)
		}
		if err := projections.NewSolicitacaoMatriculaProjection(client).Rebuild(); err != nil {
			t.Fatal(err)
		}
		if err := projections.NewEstudanteProjection(client).Rebuild(); err != nil {
			t.Fatal(err)
		}
		var status string
		if err := client.DB().QueryRow(`SELECT status FROM projection_solicitacoes_matricula WHERE codigo_solicitacao=$1`, codigo).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "aprovada" {
			t.Fatalf("%s: status da solicitação = %q, queria aprovada", tentativa, status)
		}
		var estudantes int
		if err := client.DB().QueryRow(`SELECT COUNT(*) FROM projection_estudantes WHERE codigo_estudante=$1`, codigoEstudante).Scan(&estudantes); err != nil {
			t.Fatal(err)
		}
		if estudantes != 1 {
			t.Fatalf("%s (passo %d): estudantes = %d, queria 1", tentativa, i+1, estudantes)
		}
	}
}
