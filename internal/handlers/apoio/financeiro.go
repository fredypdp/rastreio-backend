package apoio

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/finance"
	"spuri/internal/middleware"
	"spuri/internal/utils"
)

// FinanceiroService is initialised with the application database in initDB.
var FinanceiroService = finance.NewService(nil)

func FinanceActor(c *gin.Context) (uuid.UUID, string, string, bool) {
	id, ok := middleware.GetUserID(c)
	if !ok {
		return uuid.Nil, "", "", false
	}
	typeName, ok := middleware.GetUserType(c)
	if !ok {
		return uuid.Nil, "", "", false
	}
	return id, typeName, c.GetString("codigo_academia"), true
}

func FinanceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, finance.ErrNotFound):
		utils.RespondWithNotFoundError(c, "recurso financeiro")
	case errors.Is(err, finance.ErrPagamentoExistente):
		utils.RespondWithConflictError(c, finance.ErrPagamentoExistente.Error())
	case errors.Is(err, finance.ErrConflict):
		utils.RespondWithConflictError(c, "operação financeira equivalente em processamento")
	case errors.Is(err, finance.ErrUpstream):
		utils.RespondWithServiceUnavailable(c, err)
	default:
		utils.RespondWithValidationError(c, err)
	}
}

func GetDBClient(c *gin.Context) *db.Client {
	v, _ := c.Get("dbClient")
	return v.(*db.Client)
}

// Métodos de pagamento como o PAGADOR (estudante) os vê (Tarefa 121).
//
// Regra única, em finance.PagamentoDisponivel: só aparecem os métodos
// habilitados pela academia que o provedor ativo dela suporta; quando a lista
// fica vazia, um aviso (codigo + mensagem) explica o motivo. Os campos de aviso
// são aditivos: clientes antigos ignoram-nos.

// solicitacaoServicoExtraParaPagador ajusta a serialização de uma solicitação
// (já em gin.H) aos métodos que o estudante realmente pode usar para pagar a
// taxa de inscrição. disps guarda a consulta ao provedor por academia.
func SolicitacaoServicoExtraParaPagador(ctx context.Context, disps map[string]finance.PagamentoDisponivel, s *aggregates.SolicitacaoServicoExtra, h gin.H) gin.H {
	if len(s.MetodosPagamentoTaxaInscricao) == 0 && s.ValorTaxaInscricao <= 0 {
		return h // sem taxa de inscrição: nada a pagar nem a explicar
	}
	disp, ok := disps[s.CodigoAcademia]
	if !ok {
		disp = FinanceiroService.PagamentoDisponivel(ctx, s.CodigoAcademia)
		disps[s.CodigoAcademia] = disp
	}
	metodos := disp.Filtrar(s.MetodosPagamentoTaxaInscricao)
	h["metodos_pagamento_taxa_inscricao"] = metodos
	if aviso := disp.Aviso(s.MetodosPagamentoTaxaInscricao, metodos); aviso != nil {
		h["metodos_pagamento_taxa_inscricao_aviso"] = aviso
	}
	return h
}
