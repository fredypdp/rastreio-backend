package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"spuri/internal/db"
	"spuri/internal/finance"
	"spuri/internal/handlers/apoio"
)

// aplicarEfeitosPagamento executa os efeitos de negócio de uma cobrança
// confirmada como paga que NÃO vivem no pacote finance: vínculo e lançamento
// do serviço extra. A propina é confirmada dentro do
// próprio Service. É a definição única desses efeitos, usada pelo webhook, pela
// consulta manual e pelo job de reconciliação; todos os passos são idempotentes
// (reaplicar sobre uma cobrança já efetivada não muda nada), por isso o
// chamador pode repetir esta função até ela dar certo.
//
// identificador é o id interno, o id do provedor ou o id do comerciante da
// cobrança.
func aplicarEfeitosPagamento(c *gin.Context, identificador, actorID, actorType, ip string) error {
	ctx := c.Request.Context()
	if codigo, tipo, mes, ano, err := apoio.FinanceiroService.DadosServicoExtraDaCobranca(ctx, identificador); err == nil && codigo != "" {
		switch tipo {
		case "taxa_inscricao":
			if err := apoio.EfetivarVinculoServicoExtraPago(c, codigo); err != nil {
				return err
			}
		case "mensalidade", "preco_unico":
			_ = apoio.FinanceiroService.ConfirmarLancamentoServicoExtraPago(ctx, codigo, tipo, ano, mes, actorID, actorType, ip)
		}
	}
	return nil
}

// contextoDeSegundoPlano cria o contexto Gin sintético que os efeitos esperam
// (mesmo padrão do worker de jobs em lote), para executá-los fora de uma
// requisição HTTP.
func contextoDeSegundoPlano(ctx context.Context, client *db.Client, repo *db.AggregateRepository) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "/reconciliacao", nil).WithContext(ctx)
	req.RemoteAddr = "127.0.0.1:0"
	c.Request = req
	c.Set("dbClient", client)
	c.Set("repository", repo)
	return c
}

// NovosEfeitosPagamento devolve os efeitos de pagamento para o job de
// reconciliação do pacote finance (ver finance.EfeitosPagamento).
func NovosEfeitosPagamento(client *db.Client, repo *db.AggregateRepository) finance.EfeitosPagamento {
	return func(ctx context.Context, cob finance.CobrancaConfirmada) error {
		c := contextoDeSegundoPlano(ctx, client, repo)
		return aplicarEfeitosPagamento(c, cob.ID.String(), "pagamento:reconciliacao", "sistema", "reconciliacao")
	}
}
