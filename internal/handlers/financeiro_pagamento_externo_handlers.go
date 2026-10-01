package handlers

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"spuri/internal/finance"
	"spuri/internal/utils"
)

// executarEfeitosPagamentoConfirmado aplica os efeitos de uma cobrança paga
// que vivem na camada de handlers (efetivação da matrícula, taxa de inscrição
// e lançamentos de serviço extra) — a mesma sequência de
// ConsultarCobrancaAppyPay e ReceberWebhookAppyPay. É idempotente e não faz
// nada quando a cobrança não está com status Success.
func executarEfeitosPagamentoConfirmado(c *gin.Context, identifier, actorID, actorType string) error {
	ctx := c.Request.Context()
	paga, err := FinanceiroService.CobrancaEstaPaga(ctx, identifier)
	if err != nil || !paga {
		return err
	}
	if codigo, err := FinanceiroService.CodigoSolicitacaoDaCobranca(ctx, identifier); err == nil && codigo != "" {
		if err := efetivarVinculoMatriculaPaga(c, codigo); err != nil {
			return err
		}
	}
	if codigo, tipo, mes, ano, err := FinanceiroService.DadosServicoExtraDaCobranca(ctx, identifier); err == nil && codigo != "" {
		switch tipo {
		case "taxa_inscricao":
			if err := efetivarVinculoServicoExtraPago(c, codigo); err != nil {
				return err
			}
		case "mensalidade", "preco_unico":
			_ = FinanceiroService.ConfirmarLancamentoServicoExtraPago(ctx, codigo, tipo, ano, mes, actorID, actorType, c.ClientIP())
		}
	}
	return nil
}

// bindJSONOpcional aceita corpo vazio (todos os campos são opcionais).
func bindJSONOpcional(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil && !errors.Is(err, io.EOF) {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return false
	}
	return true
}

// RegistrarPagamentoExternoCobranca marca uma cobrança da própria academia,
// ainda aguardando pagamento, como paga fora da plataforma e executa o mesmo
// pipeline de confirmação de um pagamento validado pela AppyPay.
func RegistrarPagamentoExternoCobranca(c *gin.Context) {
	var in finance.PagamentoExternoInput
	if !bindJSONOpcional(c, &in) {
		return
	}
	id, typ, own, ok := financeActor(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	if typ != "academia" {
		utils.RespondWithForbiddenError(c, "somente a academia dona pode marcar um pagamento como feito fora da plataforma")
		return
	}
	out, err := FinanceiroService.RegistrarPagamentoExternoCobranca(c.Request.Context(), own, c.Param("id"), in, id.String(), typ, c.ClientIP())
	if err != nil {
		// Pagamento já feito dentro da plataforma: completa os efeitos que
		// faltavam antes de recusar.
		if errors.Is(err, finance.ErrPagamentoExistente) {
			if effectErr := executarEfeitosPagamentoConfirmado(c, c.Param("id"), id.String(), typ); effectErr != nil {
				utils.RespondWithInternalError(c, effectErr)
				return
			}
		}
		financeError(c, err)
		return
	}
	if effectErr := executarEfeitosPagamentoConfirmado(c, c.Param("id"), id.String(), typ); effectErr != nil {
		utils.RespondWithInternalError(c, effectErr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// RegistrarPagamentoExternoMensalidades marca mensalidades pendentes de um
// estudante da própria academia como pagas fora da plataforma.
func RegistrarPagamentoExternoMensalidades(c *gin.Context) {
	var in finance.PagamentoExternoMensalidadesInput
	if c.ShouldBindJSON(&in) != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	id, typ, own, ok := financeActor(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	if typ != "academia" {
		utils.RespondWithForbiddenError(c, "somente a academia dona pode marcar um pagamento como feito fora da plataforma")
		return
	}
	in.CodigoEstudante = strings.TrimSpace(in.CodigoEstudante)
	if !academiaPossuiVinculoMensalidade(c, in.CodigoEstudante, own) {
		utils.RespondWithForbiddenError(c, "estudante não pertence a esta academia")
		return
	}
	out, err := FinanceiroService.RegistrarPagamentoExternoMensalidades(c.Request.Context(), own, in, id.String(), typ, c.ClientIP())
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}
