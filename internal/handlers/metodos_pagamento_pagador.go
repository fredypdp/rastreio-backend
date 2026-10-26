package handlers

import (
	"context"

	"spuri/internal/domain/aggregates"
	"spuri/internal/finance"
	"spuri/internal/projections"

	"github.com/gin-gonic/gin"
)

// Métodos de pagamento como o PAGADOR (estudante) os vê (Tarefa 121).
//
// Regra única, em finance.PagamentoDisponivel: só aparecem os métodos
// habilitados pela academia que o provedor ativo dela suporta; quando a lista
// fica vazia, um aviso (codigo + mensagem) explica o motivo. Os campos de aviso
// são aditivos: clientes antigos ignoram-nos.

// servicoExtraParaPagador é o serviço extra com os métodos já filtrados e os
// avisos de métodos indisponíveis (um para o pagamento do serviço, outro para a
// taxa de inscrição). Os campos do DTO são promovidos no JSON.
type servicoExtraParaPagador struct {
	projections.ServicoExtraDTO
	MetodosPagamentoAviso              *finance.AvisoMetodos `json:"metodos_pagamento_aviso,omitempty"`
	MetodosPagamentoTaxaInscricaoAviso *finance.AvisoMetodos `json:"metodos_pagamento_taxa_inscricao_aviso,omitempty"`
}

// servicosExtrasParaPagador adapta os serviços extras de UMA academia ao
// provedor ativo dela (uma única consulta ao provedor, para toda a lista).
func servicosExtrasParaPagador(ctx context.Context, academia string, itens []projections.ServicoExtraDTO) []servicoExtraParaPagador {
	disp := FinanceiroService.PagamentoDisponivel(ctx, academia)
	out := make([]servicoExtraParaPagador, 0, len(itens))
	for _, s := range itens {
		v := servicoExtraParaPagador{ServicoExtraDTO: s}
		if s.Pago {
			v.MetodosPagamento = disp.Filtrar(s.MetodosPagamento)
			v.MetodosPagamentoAviso = disp.Aviso(s.MetodosPagamento, v.MetodosPagamento)
		}
		if s.TemTaxaInscricao {
			v.MetodosPagamentoTaxaInscricao = disp.Filtrar(s.MetodosPagamentoTaxaInscricao)
			v.MetodosPagamentoTaxaInscricaoAviso = disp.Aviso(s.MetodosPagamentoTaxaInscricao, v.MetodosPagamentoTaxaInscricao)
		}
		out = append(out, v)
	}
	return out
}

// solicitacaoServicoExtraParaPagador ajusta a serialização de uma solicitação
// (já em gin.H) aos métodos que o estudante realmente pode usar para pagar a
// taxa de inscrição. disps guarda a consulta ao provedor por academia.
func solicitacaoServicoExtraParaPagador(ctx context.Context, disps map[string]finance.PagamentoDisponivel, s *aggregates.SolicitacaoServicoExtra, h gin.H) gin.H {
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
