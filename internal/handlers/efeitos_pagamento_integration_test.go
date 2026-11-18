package handlers

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"spuri/internal/db"
	"spuri/internal/finance"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// O job de reconciliação confirma pagamentos FORA de uma requisição HTTP. Este
// teste prova que o adaptador dos efeitos (contexto Gin sintético) roda sem
// erro nesse contexto e que repetir é inofensivo (os efeitos são repetidos pelo
// job até darem certo). Uma cobrança avulsa não tem obrigação associada, então
// o adaptador não deve produzir nenhum efeito de negócio.
func TestIntegrationEfeitosPagamentoDoJobRodamForaDeUmaRequisicaoSemEfeitoIndevido(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	t.Setenv("ENV", "test")
	t.Setenv("APPYPAY_RESOURCE", "integration-resource")
	t.Setenv("JWT_SECRET", "test-only-secret-material-at-least-32")

	academia := "EJ" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	seedAcademiaParaMatriculaWebhook(t, client, academia)

	service := finance.NewService(client)
	service.SetHTTPClient(&http.Client{Transport: handlerAppyPayMockTransport{}})
	if _, _, err := service.ConfigureCredential(context.Background(), nil, finance.CredentialInput{
		ContextoTipo:   finance.ContextoAcademia,
		CodigoAcademia: academia,
		Provedor:       finance.ProvedorAppyPay, Credenciais: map[string]string{"client_id": "integration-client", "client_secret": "integration-secret", "gpo_method": "GPO_INTEGRATION", "ref_method": "REF_INTEGRATION"},
	}, "integration-test", "sistema", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	charge := novaCobrancaAvulsaREF(t, service, academia, 750)
	previousService := FinanceiroService
	FinanceiroService = service
	t.Cleanup(func() { FinanceiroService = previousService })

	var ledgerAntes int
	if err := client.DB().QueryRow(`SELECT COUNT(*) FROM spuri_ledger WHERE aggregate_type <> 'Financeiro'`).Scan(&ledgerAntes); err != nil {
		t.Fatal(err)
	}

	efeitos := NovosEfeitosPagamento(client, db.NewAggregateRepository(client))
	confirmada := finance.CobrancaConfirmada{
		ID:                    charge.ID,
		ProviderChargeID:      charge.ProviderChargeID,
		MerchantTransactionID: charge.MerchantTransactionID,
		Provedor:              finance.ProvedorAppyPay,
	}
	for _, tentativa := range []string{"primeira", "repetida (idempotente)"} {
		if err := efeitos(context.Background(), confirmada); err != nil {
			t.Fatalf("efeitos, %s: %v", tentativa, err)
		}
	}

	var ledgerDepois int
	if err := client.DB().QueryRow(`SELECT COUNT(*) FROM spuri_ledger WHERE aggregate_type <> 'Financeiro'`).Scan(&ledgerDepois); err != nil {
		t.Fatal(err)
	}
	if ledgerDepois != ledgerAntes {
		t.Fatalf("os efeitos de uma cobrança avulsa gravaram %d evento(s) fora do Financeiro", ledgerDepois-ledgerAntes)
	}
}
