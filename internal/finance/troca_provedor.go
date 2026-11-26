package finance

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"spuri/internal/domain/aggregates"
)

// Troca de provedor: o que acontece com as dívidas quando o contexto muda de
// provedor (AppyPay ↔ PayPay).
//
//  1. Cobrança nova para uma dívida que já tem cobrança em aberto no OUTRO
//     provedor: se esse provedor sabe fechar a cobrança (ProvedorCancelavel), ela
//     é fechada e cancelada e a nova segue; se não sabe (AppyPay), a cobrança
//     antiga continua paga-vel e por isso continua a bloquear a nova — cancelá-la
//     só no Spuri deixaria o pagador a poder pagar uma dívida "cancelada".
//  2. Duas cobranças confirmadas para a mesma dívida: a primeira quita; a
//     segunda fica marcada "pago em duplicidade" (aviso na listagem) e não
//     é aplicada em nenhuma outra dívida.

// motivoSubstituicao é o motivo gravado no cancelamento automático.
const motivoSubstituicao = "substituída por nova cobrança após a troca de provedor"

// liberarCobrancasDeOutroProvedor fecha, quando o provedor o permite, as
// cobranças em aberto (ids) que pertencem a um provedor diferente do ativo da
// academia. Cobranças do provedor ativo, ou de um provedor que não sabe fechar,
// ficam como estão (e continuam a bloquear a cobrança nova). Se, ao fechar,
// o provedor revelar que a cobrança já foi paga, devolve ErrPagamentoExistente.
func (s *Service) liberarCobrancasDeOutroProvedor(ctx context.Context, academia string, ids []string, actorID, actorType, ip string) error {
	if len(ids) == 0 {
		return nil
	}
	ativo, err := s.ProvedorAtivo(ctx, ContextoAcademia, academia)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil // sem provedor ativo não há cobrança nova a proteger
		}
		return err
	}
	for _, id := range ids {
		row, err := s.loadCharge(ctx, id)
		if err != nil {
			return err
		}
		if row.Provedor == ativo || row.Provedor == ProvedorExterno || !s.provedorPermiteFechar(row.Provedor) {
			continue
		}
		if isTerminalChargeStatus(row.Status) {
			continue
		}
		if _, err = s.cancelarCobranca(ctx, row, motivoSubstituicao, actorID, actorType, ip); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) idsAbertosMensalidade(ctx context.Context, estudante, academia, ano string, mes int) ([]string, error) {
	return s.idsDeCobrancas(ctx, `SELECT DISTINCT c.id::text FROM financeiro_mensalidade_cobrancas m JOIN financeiro_cobrancas c ON c.id=m.charge_id
		WHERE m.codigo_estudante=$1 AND m.codigo_academia=$2 AND m.ano_letivo=$3 AND m.mes=$4
		AND lower(COALESCE(c.payload->>'status','')) NOT IN (`+chargeAbertaStatusExcluidos+`)`, estudante, academia, ano, mes)
}

func (s *Service) idsAbertosServicoExtraObrigacao(ctx context.Context, id, tipo string, ano, mes int) ([]string, error) {
	return s.idsDeCobrancas(ctx, `SELECT id::text FROM financeiro_cobrancas WHERE payload->>'codigo_inscricao_servico'=$1 AND payload->>'tipo_lancamento_servico_extra'=$2 AND COALESCE((payload->>'ano_referencia')::int,0)=$3 AND COALESCE((payload->>'mes_referencia')::int,0)=$4 AND lower(COALESCE(payload->>'status','')) NOT IN (`+chargeAbertaStatusExcluidos+`)`, id, tipo, ano, mes)
}

func (s *Service) idsAbertosTaxaInscricao(ctx context.Context, solicitacaoID string) ([]string, error) {
	return s.idsDeCobrancas(ctx, `SELECT id::text FROM financeiro_cobrancas WHERE payload->>'codigo_inscricao_servico'=$1 AND lower(COALESCE(payload->>'status','')) NOT IN (`+chargeAbertaStatusExcluidos+`)`, solicitacaoID)
}

// ---- Duplicidade ----------------------------------------------------------

// verificarDuplicidade corre depois de um evento que leva uma cobrança a
// Success (criação síncrona, webhook ou consulta). Se OUTRA cobrança já paga
// quita a mesma dívida, esta é marcada como paga em duplicidade — o que
// impede confirmMensalidadeCharge de a aplicar. Melhor esforço: uma falha aqui
// nunca desfaz o evento já gravado.
func (s *Service) verificarDuplicidade(ctx context.Context, event string, payload map[string]any, id uuid.UUID, userID, userType, ip string) {
	if event != aggregates.CobrancaConsultada && event != aggregates.CobrancaCriada {
		return
	}
	if status, _ := payload["status"].(string); !isSuccessfulChargeStatus(status) {
		return
	}
	if dup, _ := payload["pago_em_duplicidade"].(bool); dup {
		return
	}
	if externo, _ := payload["pagamento_externo"].(bool); externo {
		return
	}
	outras, err := s.cobrancasPagasDaMesmaDivida(ctx, id, payload)
	if err != nil || len(outras) == 0 {
		return
	}
	updated := copiarPayload(payload)
	updated["pago_em_duplicidade"] = true
	updated["duplicidade_com"] = outras
	_ = s.record(ctx, id, aggregates.CobrancaConsultada, updated, userID, userType, ip)
}

// cobrancasPagasDaMesmaDivida devolve as outras cobranças já pagas (Success)
// que quitam a mesma obrigação desta cobrança.
func (s *Service) cobrancasPagasDaMesmaDivida(ctx context.Context, id uuid.UUID, payload map[string]any) ([]string, error) {
	if inscricao, _ := payload["codigo_inscricao_servico"].(string); strings.TrimSpace(inscricao) != "" {
		tipo, _ := payload["tipo_lancamento_servico_extra"].(string)
		ano, mes := numeroDoPayload(payload["ano_referencia"]), numeroDoPayload(payload["mes_referencia"])
		return s.idsDeCobrancas(ctx, `SELECT id::text FROM financeiro_cobrancas WHERE id<>$1 AND payload->>'codigo_inscricao_servico'=$2 AND COALESCE(payload->>'tipo_lancamento_servico_extra','')=$3 AND COALESCE((payload->>'ano_referencia')::int,0)=$4 AND COALESCE((payload->>'mes_referencia')::int,0)=$5 AND lower(COALESCE(payload->>'status',''))='success' AND COALESCE((payload->>'pago_em_duplicidade')::boolean,false)=false`, id, inscricao, tipo, ano, mes)
	}
	if len(mensalidadesDoPayload(payload)) == 0 {
		return nil, nil
	}
	return s.idsDeCobrancas(ctx, `SELECT DISTINCT o.charge_id::text FROM financeiro_mensalidade_cobrancas m
		JOIN financeiro_mensalidade_cobrancas o ON o.codigo_estudante=m.codigo_estudante AND o.codigo_academia=m.codigo_academia AND o.ano_letivo=m.ano_letivo AND o.mes=m.mes AND o.charge_id<>m.charge_id
		JOIN financeiro_cobrancas c ON c.id=o.charge_id
		WHERE m.charge_id=$1 AND lower(COALESCE(c.payload->>'status',''))='success' AND COALESCE((c.payload->>'pago_em_duplicidade')::boolean,false)=false`, id)
}

// numeroDoPayload lê um inteiro do payload venha ele de um evento recém-criado
// em memória (int, *int) ou já lido do ledger (float64 do JSON). Ausente: 0.
func numeroDoPayload(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case *int:
		if n != nil {
			return *n
		}
	case float64:
		return int(n)
	}
	return 0
}
