package finance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"spuri/internal/domain/aggregates"
)

const (
	// MetodoPagamentoExterno é gravado em payment_method das cobranças criadas
	// para pendências pagas fora da plataforma.
	MetodoPagamentoExterno = "EXTERNO"

	maxObservacaoPagamentoExterno = 500
	maxReferenciaPagamentoExterno = 100
)

// ErrPagamentoExistente é devolvido quando uma cobrança/obrigação já foi paga
// (dentro ou fora da plataforma) ou tem um pagamento já confirmado pelo
// provedor, ainda aguardando confirmação local. Nesses casos ela não pode ser
// cancelada, anulada nem reativada. O handler mapeia este erro para HTTP 409.
var ErrPagamentoExistente = errors.New("esta cobrança já foi paga ou tem um pagamento aguardando confirmação, por isso não pode ser cancelada, anulada nem reativada")

// PagamentoExternoInput são os dados opcionais informados pela academia ao
// marcar uma cobrança existente como paga fora da plataforma.
type PagamentoExternoInput struct {
	Observacao        string `json:"observacao,omitempty"`
	ReferenciaExterna string `json:"referencia_externa,omitempty"`
}

// PagamentoExternoMensalidadesInput marca pendências de mensalidade (sem
// cobrança em aberto) como pagas fora da plataforma.
type PagamentoExternoMensalidadesInput struct {
	CodigoEstudante   string                  `json:"codigo_estudante"`
	Meses             []MensalidadeSelecaoMes `json:"meses"`
	Observacao        string                  `json:"observacao,omitempty"`
	ReferenciaExterna string                  `json:"referencia_externa,omitempty"`
}

// PagamentoExternoResultado é a resposta de ambos os endpoints de pagamento
// externo. JaRegistrado é true quando a mesma cobrança já tinha sido marcada
// antes (a operação é idempotente e não grava um segundo evento).
type PagamentoExternoResultado struct {
	ChargeResult
	JaRegistrado bool                    `json:"ja_registrado,omitempty"`
	Valor        float64                 `json:"valor,omitempty"`
	Meses        []MensalidadeSelecaoMes `json:"meses,omitempty"`
}

func normalizarCamposExternos(observacao, referencia *string) error {
	*observacao, *referencia = strings.TrimSpace(*observacao), strings.TrimSpace(*referencia)
	if utf8.RuneCountInString(*observacao) > maxObservacaoPagamentoExterno {
		return fmt.Errorf("observacao deve ter no máximo %d caracteres", maxObservacaoPagamentoExterno)
	}
	if utf8.RuneCountInString(*referencia) > maxReferenciaPagamentoExterno {
		return fmt.Errorf("referencia_externa deve ter no máximo %d caracteres", maxReferenciaPagamentoExterno)
	}
	return nil
}

func marcarPagamentoExterno(payload map[string]any, observacao, referencia, actorID string) {
	payload["pagamento_externo"] = true
	payload["pagamento_externo_em"] = time.Now().UTC().Format(time.RFC3339)
	payload["pagamento_externo_por"] = actorID
	if observacao != "" {
		payload["pagamento_externo_observacao"] = observacao
	}
	if referencia != "" {
		payload["pagamento_externo_referencia"] = referencia
	}
}

func copiarPayload(src map[string]any) map[string]any {
	out := make(map[string]any, len(src)+8)
	for k, v := range src {
		out[k] = v
	}
	return out
}

// payloadIndicaPagamento é true quando o payload de uma cobrança carrega
// qualquer sinal de pagamento: status Success, Success observado no provedor
// depois de um cancelamento local (provider_status) ou pagamento externo.
func payloadIndicaPagamento(payload map[string]any) bool {
	if status, _ := payload["status"].(string); isSuccessfulChargeStatus(normalizeChargeStatus(status)) {
		return true
	}
	if status, _ := payload["provider_status"].(string); isSuccessfulChargeStatus(normalizeChargeStatus(status)) {
		return true
	}
	externo, _ := payload["pagamento_externo"].(bool)
	return externo
}

// cobrancaPagaExternamente é true para uma cobrança Success marcada como paga
// fora da plataforma.
func cobrancaPagaExternamente(row chargeRow) bool {
	externo, _ := row.Payload["pagamento_externo"].(bool)
	return externo && isSuccessfulChargeStatus(row.Status)
}

// CobrancaEstaPaga informa se a cobrança está com status Success localmente.
func (s *Service) CobrancaEstaPaga(ctx context.Context, identifier string) (bool, error) {
	row, err := s.loadCharge(ctx, identifier)
	if err != nil {
		return false, err
	}
	return isSuccessfulChargeStatus(row.Status), nil
}

// statusAoVivoSeDisponivel consulta o provedor. Devolve verificada=false (sem
// erro) apenas quando o escopo não tem credencial AppyPay — não há provedor
// com que o pagamento possa conflitar. Qualquer outra falha bloqueia, porque
// sem consulta não se sabe se o pagador já pagou.
func (s *Service) statusAoVivoSeDisponivel(ctx context.Context, row chargeRow) (status string, verificada bool, err error) {
	cred, err := s.loadCredential(ctx, row.Contexto, row.Academia)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	path := "/charges/" + url.PathEscape(row.ProviderID)
	if row.ProviderID == "" {
		path = "/charges?merchantTransactionId=" + url.QueryEscape(row.Merchant)
	}
	response, err := s.callJSON(ctx, cred, http.MethodGet, path, nil, false)
	if err != nil {
		return "", false, fmt.Errorf("%w: não foi possível confirmar com a AppyPay se a cobrança já foi paga", ErrUpstream)
	}
	status = normalizeChargeStatus(extractProviderOutcome(response).Status)
	return status, status != "", nil
}

// reconciliarPagamentoDescoberto aplica, em melhor esforço, os efeitos
// financeiros de um pagamento que acabou de ser descoberto fora do fluxo
// normal (mensalidade e lançamento de serviço extra). Os efeitos de matrícula
// e taxa de inscrição vivem na camada de handlers.
func (s *Service) reconciliarPagamentoDescoberto(ctx context.Context, chargeID uuid.UUID, actorID, actorType, ip string) {
	_ = s.confirmMensalidadeCharge(ctx, chargeID, actorID, actorType, ip)
	codigo, tipo, mes, ano, err := s.DadosServicoExtraDaCobranca(ctx, chargeID.String())
	if err == nil && codigo != "" && (tipo == "mensalidade" || tipo == "preco_unico") {
		_ = s.ConfirmarLancamentoServicoExtraPago(ctx, codigo, tipo, ano, mes, actorID, actorType, ip)
	}
}

// RegistrarPagamentoExternoCobranca marca uma cobrança real da academia, ainda
// aguardando pagamento, como paga fora da plataforma. Antes de gravar consulta
// o provedor: se a AppyPay já reporta Success, o pagamento aconteceu dentro do
// ecossistema — reconcilia e devolve ErrPagamentoExistente.
func (s *Service) RegistrarPagamentoExternoCobranca(ctx context.Context, academia, identifier string, in PagamentoExternoInput, actorID, actorType, ip string) (PagamentoExternoResultado, error) {
	if err := normalizarCamposExternos(&in.Observacao, &in.ReferenciaExterna); err != nil {
		return PagamentoExternoResultado{}, err
	}
	if strings.TrimSpace(identifier) == "" {
		return PagamentoExternoResultado{}, errors.New("id da cobrança é obrigatório")
	}
	row, err := s.loadCharge(ctx, identifier)
	if err != nil {
		return PagamentoExternoResultado{}, err
	}
	if actorType != "academia" || strings.TrimSpace(academia) == "" || row.Contexto != ContextoAcademia || row.Academia != academia {
		return PagamentoExternoResultado{}, fmt.Errorf("%w: cobrança não encontrada no contexto", ErrNotFound)
	}
	resultado := PagamentoExternoResultado{ChargeResult: ChargeResult{ID: row.ID, ProviderChargeID: row.ProviderID, MerchantTransactionID: row.Merchant, Status: row.Status}}

	if externo, _ := row.Payload["pagamento_externo"].(bool); externo && isSuccessfulChargeStatus(row.Status) {
		// Repetição do mesmo pedido: não grava outro evento, só garante que a
		// confirmação de mensalidade (idempotente) foi aplicada.
		if err = s.confirmMensalidadeCharge(ctx, row.ID, actorID, actorType, ip); err != nil {
			return resultado, err
		}
		resultado.JaRegistrado = true
		return resultado, nil
	}
	if isSuccessfulChargeStatus(row.Status) {
		return resultado, ErrPagamentoExistente
	}
	if isTerminalChargeStatus(row.Status) {
		return resultado, errors.New("somente cobranças aguardando pagamento podem ser marcadas como pagas fora da plataforma")
	}

	live, verificada, err := s.statusAoVivoSeDisponivel(ctx, row)
	if err != nil {
		return resultado, err
	}
	if verificada && isSuccessfulChargeStatus(live) {
		current, err := s.consultCharge(ctx, row, actorID, actorType, ip)
		if err != nil {
			return resultado, err
		}
		s.reconciliarPagamentoDescoberto(ctx, row.ID, actorID, actorType, ip)
		return PagamentoExternoResultado{ChargeResult: current}, ErrPagamentoExistente
	}

	payload := copiarPayload(row.Payload)
	payload["status_anterior"] = row.Status
	payload["status"] = "Success"
	marcarPagamentoExterno(payload, in.Observacao, in.ReferenciaExterna, actorID)
	if err = s.record(ctx, row.ID, aggregates.CobrancaPagamentoExternoRegistrado, payload, actorID, actorType, ip); err != nil {
		return resultado, err
	}
	resultado.Status = "Success"
	if err = s.confirmMensalidadeCharge(ctx, row.ID, actorID, actorType, ip); err != nil {
		return resultado, err
	}
	return resultado, nil
}

func merchantExterno(id uuid.UUID) string {
	return "EXT" + strings.ToUpper(strings.ReplaceAll(id.String(), "-", ""))[:12]
}

// RegistrarPagamentoExternoMensalidades marca mensalidades pendentes (sem
// cobrança em aberto) como pagas fora da plataforma. Cria uma cobrança já
// paga — para que o pagamento apareça no histórico — e confirma as
// mensalidades pelo mesmo caminho do pagamento normal.
func (s *Service) RegistrarPagamentoExternoMensalidades(ctx context.Context, academia string, in PagamentoExternoMensalidadesInput, actorID, actorType, ip string) (PagamentoExternoResultado, error) {
	academia, in.CodigoEstudante = strings.TrimSpace(academia), strings.TrimSpace(in.CodigoEstudante)
	if actorType != "academia" || academia == "" || strings.TrimSpace(actorID) == "" {
		return PagamentoExternoResultado{}, errors.New("somente a academia pode registar um pagamento feito fora da plataforma")
	}
	if err := normalizarCamposExternos(&in.Observacao, &in.ReferenciaExterna); err != nil {
		return PagamentoExternoResultado{}, err
	}
	if in.CodigoEstudante == "" || len(in.Meses) == 0 {
		return PagamentoExternoResultado{}, errors.New("codigo_estudante e pelo menos um mês são obrigatórios")
	}
	seen := map[string]bool{}
	meses := make([]MensalidadeSelecaoMes, 0, len(in.Meses))
	total := 0.0
	for _, m := range in.Meses {
		m.AnoLetivo = strings.TrimSpace(m.AnoLetivo)
		if !anoLetivoValido(m.AnoLetivo) || !mesValido(m.Mes) {
			return PagamentoExternoResultado{}, errors.New("cada mês deve ter ano_letivo válido e mes entre 1 e 12")
		}
		key := fmt.Sprintf("%s:%d", m.AnoLetivo, m.Mes)
		if seen[key] {
			return PagamentoExternoResultado{}, errors.New("mês selecionado mais de uma vez")
		}
		seen[key] = true
		view, err := s.mesDevido(ctx, in.CodigoEstudante, academia, m.AnoLetivo, m.Mes)
		if err != nil {
			return PagamentoExternoResultado{}, err
		}
		estado, _, err := s.estadoObrigacao(ctx, in.CodigoEstudante, academia, m.AnoLetivo, m.Mes)
		if err != nil {
			return PagamentoExternoResultado{}, err
		}
		switch estado {
		case EstadoPago:
			return PagamentoExternoResultado{}, ErrPagamentoExistente
		case EstadoAnulado:
			return PagamentoExternoResultado{}, fmt.Errorf("mensalidade %s/%02d está anulada: reative-a antes de registar o pagamento", m.AnoLetivo, m.Mes)
		}
		aberta, err := s.mensalidadeTemCobrancaAberta(ctx, in.CodigoEstudante, academia, m.AnoLetivo, m.Mes)
		if err != nil {
			return PagamentoExternoResultado{}, err
		}
		if aberta {
			return PagamentoExternoResultado{}, fmt.Errorf("mensalidade %s/%02d já possui cobrança em aberto: marque essa cobrança como paga", m.AnoLetivo, m.Mes)
		}
		total += view.Valor
		meses = append(meses, MensalidadeSelecaoMes{AnoLetivo: m.AnoLetivo, Mes: m.Mes})
	}
	total = roundAmount(total)
	id := uuid.New()
	request := ChargeRequest{
		ContextoTipo:          ContextoAcademia,
		CodigoAcademia:        academia,
		Amount:                total,
		Currency:              "AOA",
		Description:           fmt.Sprintf("Propinas %s: %d mensalidade(s) — pagamento fora da plataforma", academia, len(meses)),
		MerchantTransactionID: merchantExterno(id),
		PaymentMethod:         MetodoPagamentoExterno,
		CodigoEstudante:       in.CodigoEstudante,
		Mensalidades:          meses,
	}
	payload := chargePayload(id, request, "", "Success", nil)
	payload["status_anterior"] = EstadoPendente
	marcarPagamentoExterno(payload, in.Observacao, in.ReferenciaExterna, actorID)
	if err := s.record(ctx, id, aggregates.CobrancaPagamentoExternoRegistrado, payload, actorID, actorType, ip); err != nil {
		return PagamentoExternoResultado{}, err
	}
	resultado := PagamentoExternoResultado{
		ChargeResult: ChargeResult{ID: id, MerchantTransactionID: request.MerchantTransactionID, Status: "Success"},
		Valor:        total,
		Meses:        meses,
	}
	if err := s.confirmMensalidadeCharge(ctx, id, actorID, actorType, ip); err != nil {
		return resultado, err
	}
	return resultado, nil
}

// garantirCobrancasSemPagamento falha com ErrPagamentoExistente quando alguma
// das cobranças mostra sinal de pagamento — local, externo ou, para as ainda
// abertas, confirmado ao vivo no provedor (Success ainda não confirmado
// localmente). Uma falha de comunicação com o provedor também bloqueia.
func (s *Service) garantirCobrancasSemPagamento(ctx context.Context, ids []string, actorID, actorType, ip string) error {
	for _, id := range ids {
		row, err := s.loadCharge(ctx, id)
		if err != nil {
			return err
		}
		if payloadIndicaPagamento(row.Payload) {
			s.reconciliarPagamentoDescoberto(ctx, row.ID, actorID, actorType, ip)
			return ErrPagamentoExistente
		}
		if isTerminalChargeStatus(row.Status) {
			continue
		}
		live, verificada, err := s.statusAoVivoSeDisponivel(ctx, row)
		if err != nil {
			return err
		}
		if verificada && isSuccessfulChargeStatus(live) {
			if _, err = s.consultCharge(ctx, row, actorID, actorType, ip); err != nil {
				return err
			}
			s.reconciliarPagamentoDescoberto(ctx, row.ID, actorID, actorType, ip)
			return ErrPagamentoExistente
		}
	}
	return nil
}

func (s *Service) idsDeCobrancas(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.client.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// garantirObrigacaoMensalidadeSemPagamento é a regra única de "nunca foi paga e
// não está aguardando confirmação" para anular/reativar uma mensalidade.
func (s *Service) garantirObrigacaoMensalidadeSemPagamento(ctx context.Context, estudante, academia, ano string, mes int, actorID, actorType, ip string) error {
	estado, _, err := s.estadoObrigacao(ctx, estudante, academia, ano, mes)
	if err != nil {
		return err
	}
	if estado == EstadoPago {
		return ErrPagamentoExistente
	}
	ids, err := s.idsDeCobrancas(ctx, `SELECT c.id::text FROM financeiro_mensalidade_cobrancas m JOIN financeiro_cobrancas c ON c.id=m.charge_id
		WHERE m.codigo_estudante=$1 AND m.codigo_academia=$2 AND m.ano_letivo=$3 AND m.mes=$4`, estudante, academia, ano, mes)
	if err != nil {
		return err
	}
	return s.garantirCobrancasSemPagamento(ctx, ids, actorID, actorType, ip)
}

// garantirObrigacaoServicoExtraSemPagamento é o equivalente para lançamentos
// de serviço extra (mensalidade e preço único).
func (s *Service) garantirObrigacaoServicoExtraSemPagamento(ctx context.Context, id, tipo string, ano, mes int, actorID, actorType, ip string) error {
	estado, err := s.estadoObrigacaoServicoExtra(ctx, id, tipo, ano, mes)
	if err != nil {
		return err
	}
	if estado == EstadoPago {
		return ErrPagamentoExistente
	}
	ids, err := s.idsDeCobrancas(ctx, `SELECT id::text FROM financeiro_cobrancas WHERE payload->>'codigo_inscricao_servico'=$1 AND payload->>'tipo_lancamento_servico_extra'=$2 AND COALESCE((payload->>'ano_referencia')::int,0)=$3 AND COALESCE((payload->>'mes_referencia')::int,0)=$4`, id, tipo, ano, mes)
	if err != nil {
		return err
	}
	return s.garantirCobrancasSemPagamento(ctx, ids, actorID, actorType, ip)
}
