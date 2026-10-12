// Package finance is the only package allowed to call a payment provider's
// HTTP API (AppyPay today; see provedor.go for the provider contract).
//
// Monetary contract: every monetary value in this package and in payment
// phases 2, 3 and 4 is float64. This deliberately mirrors AppyPay's
// number<double> contract. New internal fields (such as ValorMensalidade and
// ValorMatricula) must use float64 plus roundAmount and amountsEqual below;
// cents, strings, decimal libraries, and local rounding rules are forbidden.
package finance

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/projections"
	"spuri/internal/security"
)

const (
	ContextoSpuri    = "spuri"
	ContextoAcademia = "academia"
	AmbienteTeste    = "test"
	AmbienteProducao = "prod"
)

var (
	// ErrNotFound is returned when a financial resource does not exist in the
	// authorised financial context.
	ErrNotFound = errors.New("recurso financeiro não encontrado")
	// ErrUpstream means the payment provider could not be reached or returned an
	// invalid response. It is deliberately distinct from request validation errors.
	ErrUpstream = errors.New("serviço do provedor de pagamento indisponível")
	// ErrConflict denotes an idempotency key that is currently being processed.
	ErrConflict = errors.New("operação financeira equivalente em processamento")
)

const amountTolerance = 0.005

// roundAmount rounds a monetary amount to two decimal places using half away
// from zero (math.Round's documented rule). It is the sole monetary rounding
// rule for this module and the payment phases built on it.
func roundAmount(v float64) float64 { return math.Round(v*100) / 100 }

// amountsEqual compares monetary float64 values using half a cent of
// tolerance, avoiding direct floating-point equality for money.
func amountsEqual(a, b float64) bool { return math.Abs(a-b) <= amountTolerance }

func AmbienteAtual() string {
	if strings.EqualFold(os.Getenv("ENV"), "production") {
		return AmbienteProducao
	}
	return AmbienteTeste
}

type ChargeRequest struct {
	ContextoTipo          string         `json:"contexto_tipo"`
	CodigoAcademia        string         `json:"codigo_academia,omitempty"`
	Amount                float64        `json:"amount"`
	Currency              string         `json:"currency"`
	Description           string         `json:"description"`
	MerchantTransactionID string         `json:"merchantTransactionId,omitempty"`
	PaymentMethod         string         `json:"paymentMethod"`
	PaymentInfo           map[string]any `json:"paymentInfo,omitempty"`
	Options               map[string]any `json:"options,omitempty"`
	Notify                map[string]any `json:"notify,omitempty"`
	Async                 bool           `json:"async"`
	// These fields are auditable Spuri metadata and are not forwarded to AppyPay.
	Mensalidades               []MensalidadeSelecaoMes `json:"mensalidades,omitempty"`
	CodigoEstudante            string                  `json:"codigo_estudante,omitempty"`
	CodigoSolicitacao          string                  `json:"codigo_solicitacao,omitempty"`
	CodigoInscricaoServico     string                  `json:"codigo_inscricao_servico,omitempty"`
	TipoLancamentoServicoExtra string                  `json:"tipo_lancamento_servico_extra,omitempty"`
	MesReferencia              *int                    `json:"mes_referencia,omitempty"`
	AnoReferencia              *int                    `json:"ano_referencia,omitempty"`
}

type QRCodeRequest struct {
	ContextoTipo          string   `json:"contexto_tipo"`
	CodigoAcademia        string   `json:"codigo_academia,omitempty"`
	Amount                float64  `json:"amount"`
	Currency              string   `json:"currency"`
	Description           string   `json:"description"`
	MerchantTransactionID string   `json:"merchantTransactionId,omitempty"`
	QRCodeType            string   `json:"qrCodeType,omitempty"`
	MinAmount             *float64 `json:"minAmount,omitempty"`
	MaxTransactions       *int     `json:"maxTransactions,omitempty"`
	StartDate             string   `json:"startDate,omitempty"`
	EndDate               string   `json:"endDate,omitempty"`
	// Mensalidades is ledger metadata only; it is never sent to AppyPay.
	Mensalidades               []MensalidadeSelecaoMes `json:"mensalidades,omitempty"`
	CodigoEstudante            string                  `json:"codigo_estudante,omitempty"`
	CodigoSolicitacao          string                  `json:"codigo_solicitacao,omitempty"`
	CodigoInscricaoServico     string                  `json:"codigo_inscricao_servico,omitempty"`
	TipoLancamentoServicoExtra string                  `json:"tipo_lancamento_servico_extra,omitempty"`
	MesReferencia              *int                    `json:"mes_referencia,omitempty"`
	AnoReferencia              *int                    `json:"ano_referencia,omitempty"`
}

type ChargeResult struct {
	ID uuid.UUID `json:"id"`
	// Provedor identifica o PSP que emitiu a cobrança (ex.: APPYPAY).
	Provedor              string `json:"provedor,omitempty"`
	ProviderChargeID      string `json:"provider_charge_id,omitempty"`
	MerchantTransactionID string `json:"merchant_transaction_id"`
	Status                string `json:"status"`
	// CodigoProvedor/MensagemProvedor/FonteProvedor/CategoriaMotivo expõem o
	// motivo real devolvido pela AppyPay (responseStatus.code/message/source
	// de POST/webhook, ou o do último transactionEvent de GET /charges) toda
	// vez que a cobrança não está simplesmente aguardando pagamento — ver
	// extractProviderOutcome/applyProviderOutcome nesta mesma package.
	CodigoProvedor   *int           `json:"codigo_provedor,omitempty"`
	MensagemProvedor string         `json:"mensagem_provedor,omitempty"`
	FonteProvedor    string         `json:"fonte_provedor,omitempty"`
	CategoriaMotivo  string         `json:"categoria_motivo,omitempty"`
	Response         map[string]any `json:"response,omitempty"`
}

type QRCodeResult struct {
	ChargeResult
	QRCodeArr string `json:"qrCodeArr,omitempty"`
}

// CobrancaResumo é o resumo de uma cobrança devolvido pela listagem
// GET /financeiro/cobrancas. Deliberadamente não inclui payment_info,
// response nem qrCodeArr — esses detalhes completos continuam disponíveis
// apenas em GET /financeiro/appypay/cobrancas/:id, para quem já sabe o
// identificador da cobrança. Ver Problema 1 em
// docs/Lista de Tarefas/Problemas de Backend - Modulo de Pagamentos.md.
type CobrancaResumo struct {
	ID uuid.UUID `json:"id"`
	// Provedor é o PSP que emitiu a cobrança (APPYPAY, …; EXTERNO para
	// pagamentos registrados fora da plataforma).
	Provedor              string `json:"provedor,omitempty"`
	ProviderChargeID      string `json:"provider_charge_id,omitempty"`
	MerchantTransactionID string `json:"merchant_transaction_id"`
	ContextoTipo          string `json:"contexto_tipo"`
	CodigoAcademia        string `json:"codigo_academia,omitempty"`
	// Origem é derivada do payload da cobrança, nunca persistida
	// separadamente: "matricula" quando há codigo_solicitacao,
	// "mensalidade" quando há codigo_estudante (e não há
	// codigo_solicitacao), "avulsa" nos demais casos (cobrança criada
	// diretamente via POST /financeiro/appypay/cobrancas ou /appypay/qr-codes
	// sem vínculo a mensalidade nem matrícula).
	Origem string `json:"origem"`
	Status string `json:"status"`
	// CodigoProvedor/MensagemProvedor/FonteProvedor/CategoriaMotivo: ver o
	// comentário equivalente em ChargeResult, acima de CreateCharge. Mesma
	// origem de dado (payload["codigo_provedor"] etc.), lida aqui para a
	// listagem em vez de para uma única cobrança.
	CodigoProvedor   *int    `json:"codigo_provedor,omitempty"`
	MensagemProvedor string  `json:"mensagem_provedor,omitempty"`
	FonteProvedor    string  `json:"fonte_provedor,omitempty"`
	CategoriaMotivo  string  `json:"categoria_motivo,omitempty"`
	Valor            float64 `json:"valor"`
	Moeda            string  `json:"moeda,omitempty"`
	Descricao        string  `json:"descricao,omitempty"`
	// MensalidadesEmAberto é o subconjunto de Mensalidades cujo estado da
	// obrigação (financeiro_mensalidade_obrigacoes_eventos, mesma regra de
	// precedenciaEstado usada por estadoObrigacao/PendenciasSemCobranca)
	// ainda é EstadoPendente no momento da consulta. Só populado por
	// PreencherMensalidadesEmAberto (pagamentos_unificado.go), chamada
	// automaticamente ao final de ListCobrancas/ListCobrancasEstudante,
	// para Origem == "mensalidade" com Status terminal de falha
	// (Failed/Cancelled/Expired/falhada/cancelada — nunca para Success nem
	// para aguardando_pagamento, que já é auto-explicativo). Desde a
	// tarefa 73, a pendência sintética do mesmo mês também volta a
	// aparecer como item separado na listagem unificada (só cobranças "em
	// aberto" — aguardando_pagamento — continuam escondendo-a; ver
	// mesesComCobrancaRealVinculada), então este campo hoje é redundante
	// com aquele item na maioria dos casos — mantido por não quebrar
	// contrato de API e por continuar útil quando o chamador não pediu a
	// pendência sem cobrança (DeveIncluirPendenciasSemCobranca == false).
	MensalidadesEmAberto []MensalidadeSelecaoMes `json:"mensalidades_em_aberto,omitempty"`
	// MetodoPagamento reflete "GPO_QR" (não apenas "GPO") quando a cobrança
	// tem qr_code_type no payload — CreateGPOQRCode grava payment_method
	// como "GPO" internamente, então sem este ajuste a origem QR ficaria
	// indistinguível de um GPO comum nesta listagem.
	MetodoPagamento            string                  `json:"metodo_pagamento,omitempty"`
	CodigoEstudante            string                  `json:"codigo_estudante,omitempty"`
	CodigoSolicitacao          string                  `json:"codigo_solicitacao,omitempty"`
	CodigoInscricaoServico     string                  `json:"codigo_inscricao_servico,omitempty"`
	TipoLancamentoServicoExtra string                  `json:"tipo_lancamento_servico_extra,omitempty"`
	MesReferencia              *int                    `json:"mes_referencia,omitempty"`
	AnoReferencia              *int                    `json:"ano_referencia,omitempty"`
	Mensalidades               []MensalidadeSelecaoMes `json:"mensalidades,omitempty"`
	// AtualizadoEm é ponteiro (não time.Time) desde a unificação de
	// pendências sintéticas em ListarPagamentosUnificado
	// (pagamentos_unificado.go): um item sintetizado a partir de uma
	// pendência sintética nunca teve
	// nenhuma atividade real, então não há nenhum "atualizado em" honesto
	// para devolver — nil (omitido do JSON) em vez de inventar uma data.
	// Para uma cobrança real, continua sempre presente (a coluna é
	// NOT NULL em financeiro_cobrancas).
	AtualizadoEm *time.Time `json:"atualizado_em,omitempty"`
	// PagamentoExterno é true quando a academia marcou manualmente a cobrança
	// como paga fora da plataforma (evento CobrancaPagamentoExternoRegistrado).
	// Nesse caso Status é "Success" e ReferenciaExterna (opcional) guarda o
	// identificador do comprovativo informado pela academia.
	PagamentoExterno  bool   `json:"pagamento_externo,omitempty"`
	ReferenciaExterna string `json:"referencia_externa,omitempty"`
}

// CobrancaListResult é o resultado paginado de ListCobrancas.
type CobrancaListResult struct {
	Cobrancas []CobrancaResumo `json:"cobrancas"`
	Total     int              `json:"total"`
}

type Service struct {
	client     *db.Client
	repository *db.AggregateRepository
	projection *projections.FinanceiroProjection
	// provedores são os provedores de pagamento implementados, por ID. O
	// núcleo nunca fala com um PSP diretamente: sempre através de Provedor.
	provedores map[string]Provedor
}

func NewService(client *db.Client) *Service {
	var repo *db.AggregateRepository
	var projection *projections.FinanceiroProjection
	if client != nil {
		repo = db.NewAggregateRepository(client)
		projection = projections.NewFinanceiroProjection(client)
	}
	return &Service{client: client, repository: repo, projection: projection, provedores: novosProvedores(nil)}
}

// SetHTTPClient overrides the HTTP client used by every payment provider. It
// is intended for tests and controlled integrations that need a custom
// RoundTripper.
func (s *Service) SetHTTPClient(client *http.Client) {
	if client != nil {
		s.provedores = novosProvedores(client)
	}
}

func (s *Service) CreateCharge(ctx context.Context, in ChargeRequest, actorID, actorType, ip string) (ChargeResult, error) {
	if err := validateCharge(&in); err != nil {
		return ChargeResult{}, err
	}
	in.Amount = roundAmount(in.Amount)
	// Cobranças NOVAS sempre seguem o provedor ativo do contexto.
	prov, credential, err := s.loadCredentialAtiva(ctx, in.ContextoTipo, in.CodigoAcademia)
	if err != nil {
		return ChargeResult{}, err
	}
	method, err := prov.ResolverMetodo(credential, in.PaymentMethod)
	if err != nil {
		return ChargeResult{}, err
	}
	if !suportaMetodo(prov, method) {
		return ChargeResult{}, fmt.Errorf("%w: %s não é suportado pelo %s", ErrMetodoIndisponivel, method, prov.Nome())
	}
	if in.MerchantTransactionID == "" {
		in.MerchantTransactionID = merchantID()
	}
	if !validMerchantID(in.MerchantTransactionID) {
		return ChargeResult{}, errors.New("merchantTransactionId deve ser alfanumérico e ter no máximo 15 caracteres")
	}
	id := uuid.New()
	reserved, err := s.reserveCharge(ctx, in.MerchantTransactionID, id)
	if err != nil {
		return ChargeResult{}, err
	}
	if !reserved {
		result, err := s.existingChargeResult(ctx, in.MerchantTransactionID, in.ContextoTipo, in.CodigoAcademia)
		if err == nil {
			return result, nil
		}
		if errors.Is(err, ErrNotFound) {
			return ChargeResult{}, ErrConflict
		}
		return ChargeResult{}, err
	}
	payload := comProvedor(chargePayload(id, in, "", EstadoCobrancaAguardandoPagamento, nil), prov.ID())
	if err = s.record(ctx, id, aggregates.CobrancaSolicitada, payload, actorID, actorType, ip); err != nil {
		_ = s.releaseChargeReservation(ctx, in.MerchantTransactionID, id)
		return ChargeResult{}, err
	}
	resp, err := prov.CriarCobranca(ctx, credential, pedidoCobranca{Valor: in.Amount, Moeda: in.Currency, Descricao: in.Description, IDComerciante: in.MerchantTransactionID, Metodo: method, Info: in.PaymentInfo, Opcoes: in.Options, Notificar: in.Notify, Async: in.Async})
	if err != nil {
		_ = s.record(ctx, id, aggregates.CobrancaFalhou, comProvedor(chargePayload(id, in, "", "Failed", map[string]any{"error": "provider_request_failed"}), prov.ID()), actorID, actorType, ip)
		return ChargeResult{ID: id, Provedor: prov.ID(), MerchantTransactionID: in.MerchantTransactionID, Status: "Failed"}, err
	}
	response, providerID, outcome := resp.Bruta, resp.ID, resp.Outcome
	status := normalizeChargeStatus(outcome.Status)
	if status == "" {
		// O provedor respondeu 2xx sem nenhum campo de status reconhecível no
		// corpo — a cobrança foi aceita mas ainda não temos nenhuma
		// informação sobre sua resolução, exatamente o significado de
		// aguardando pagamento.
		status = EstadoCobrancaAguardandoPagamento
	}
	if method == MetodoREF && isSuccessfulChargeStatus(status) {
		// Uma referência de pagamento (REF) NUNCA se confirma de forma
		// síncrona: o cliente paga a referência depois, num multibanco/balcão,
		// fora desta chamada. A confirmação só pode legitimamente chegar
		// depois, por webhook ou consulta (AcceptWebhook/consultCharge —
		// nenhum dos dois é tocado por este bloco). Nunca confiar numa
		// classificação "Success" vinda da chamada de criação para REF: o
		// pior cenário de aceitar por engano é uma matrícula/mensalidade
		// efetivada sem pagamento real, e o custo de bloquear é zero, porque
		// um REF genuíno sempre confirma depois mesmo assim. Regra da
		// plataforma, válida para qualquer provedor.
		status = EstadoCobrancaAguardandoPagamento
	}
	created := comProvedor(chargePayload(id, in, providerID, status, response), prov.ID())
	applyProviderOutcome(created, outcome)
	if err = s.record(ctx, id, aggregates.CobrancaCriada, created, actorID, actorType, ip); err != nil {
		return ChargeResult{}, err
	}
	result := ChargeResult{ID: id, Provedor: prov.ID(), ProviderChargeID: providerID, MerchantTransactionID: in.MerchantTransactionID, Status: status, Response: response}
	if outcome.HasCode {
		code := outcome.Code
		result.CodigoProvedor = &code
	}
	result.MensagemProvedor, result.FonteProvedor, result.CategoriaMotivo = outcome.Message, outcome.Source, outcome.Categoria
	if isSuccessfulChargeStatus(status) {
		_ = s.confirmMensalidadeCharge(ctx, id, actorID, actorType, ip)
	}
	return result, nil
}

func (s *Service) CreateGPOQRCode(ctx context.Context, in QRCodeRequest, actorID, actorType, ip string) (QRCodeResult, error) {
	if err := validateQRCode(&in); err != nil {
		return QRCodeResult{}, err
	}
	in.Amount = roundAmount(in.Amount)
	if in.MinAmount != nil {
		v := roundAmount(*in.MinAmount)
		in.MinAmount = &v
	}
	if in.MerchantTransactionID == "" {
		in.MerchantTransactionID = merchantID()
	}
	if !validMerchantID(in.MerchantTransactionID) {
		return QRCodeResult{}, errors.New("merchantTransactionId deve ser alfanumérico e ter no máximo 15 caracteres")
	}
	typ := strings.ToUpper(in.QRCodeType)
	if typ == "" {
		typ = "SINGLE"
	}
	if typ != "SINGLE" && typ != "MULTIPLE" {
		return QRCodeResult{}, errors.New("qrCodeType inválido")
	}
	if typ == "MULTIPLE" && (in.MinAmount == nil || in.MaxTransactions == nil || in.StartDate == "" || in.EndDate == "") {
		return QRCodeResult{}, errors.New("QR MULTIPLE exige minAmount, maxTransactions, startDate e endDate")
	}
	prov, cred, err := s.loadCredentialAtiva(ctx, in.ContextoTipo, in.CodigoAcademia)
	if err != nil {
		return QRCodeResult{}, err
	}
	if !suportaMetodo(prov, MetodoGPOQR) {
		return QRCodeResult{}, fmt.Errorf("%w: %s não é suportado pelo %s", ErrMetodoIndisponivel, MetodoGPOQR, prov.Nome())
	}
	id := uuid.New()
	reserved, err := s.reserveCharge(ctx, in.MerchantTransactionID, id)
	if err != nil {
		return QRCodeResult{}, err
	}
	if !reserved {
		result, err := s.existingQRCodeResult(ctx, in.MerchantTransactionID, in.ContextoTipo, in.CodigoAcademia)
		if err == nil {
			return result, nil
		}
		if errors.Is(err, ErrNotFound) {
			return QRCodeResult{}, ErrConflict
		}
		return QRCodeResult{}, err
	}
	if err = s.record(ctx, id, aggregates.QRCodeSolicitado, comProvedor(qrCodePayload(id, in, typ, "", EstadoCobrancaAguardandoPagamento, nil), prov.ID()), actorID, actorType, ip); err != nil {
		_ = s.releaseChargeReservation(ctx, in.MerchantTransactionID, id)
		return QRCodeResult{}, err
	}
	resp, err := prov.CriarQRCode(ctx, cred, pedidoQRCode{Valor: in.Amount, Moeda: in.Currency, Descricao: in.Description, IDComerciante: in.MerchantTransactionID, Tipo: typ, ValorMinimo: in.MinAmount, MaxTransacoes: in.MaxTransactions, Inicio: in.StartDate, Fim: in.EndDate})
	if err != nil {
		_ = s.record(ctx, id, aggregates.QRCodeFalhou, comProvedor(qrCodePayload(id, in, typ, "", "Failed", map[string]any{"error": "provider_request_failed"}), prov.ID()), actorID, actorType, ip)
		return QRCodeResult{}, err
	}
	response, providerID, outcome := resp.Bruta, resp.ID, resp.Outcome
	status := normalizeChargeStatus(outcome.Status)
	if status == "" {
		// Mesmo raciocínio de CreateCharge: 2xx sem status reconhecível =
		// aceito, ainda sem resolução conhecida. (A regra "criar o QR não é
		// pagamento recebido" é aplicada pelo próprio provedor ao traduzir a
		// resposta — ver CriarQRCode do provedor.)
		status = EstadoCobrancaAguardandoPagamento
	}
	payload := comProvedor(qrCodePayload(id, in, typ, providerID, status, response), prov.ID())
	applyProviderOutcome(payload, outcome)
	if err = s.record(ctx, id, aggregates.QRCodeGerado, payload, actorID, actorType, ip); err != nil {
		return QRCodeResult{}, err
	}
	chargeResult := ChargeResult{ID: id, Provedor: prov.ID(), ProviderChargeID: providerID, MerchantTransactionID: in.MerchantTransactionID, Status: status, Response: response}
	if outcome.HasCode {
		code := outcome.Code
		chargeResult.CodigoProvedor = &code
	}
	chargeResult.MensagemProvedor, chargeResult.FonteProvedor, chargeResult.CategoriaMotivo = outcome.Message, outcome.Source, outcome.Categoria
	result := QRCodeResult{ChargeResult: chargeResult, QRCodeArr: resp.QRCode}
	if isSuccessfulChargeStatus(status) {
		_ = s.confirmMensalidadeCharge(ctx, id, actorID, actorType, ip)
	}
	return result, nil
}

func (s *Service) ConsultCharge(ctx context.Context, contexto, academia, identifier, actorID, actorType, ip string) (ChargeResult, error) {
	if identifier == "" {
		return ChargeResult{}, errors.New("id da cobrança é obrigatório")
	}
	row, err := s.loadCharge(ctx, identifier)
	if err != nil {
		return ChargeResult{}, err
	}
	if row.Contexto != contexto || row.Academia != academia {
		return ChargeResult{}, fmt.Errorf("%w: cobrança não encontrada no contexto", ErrNotFound)
	}
	result, err := s.consultCharge(ctx, row, actorID, actorType, ip)
	if err == nil && isSuccessfulChargeStatus(result.Status) {
		_ = s.confirmMensalidadeCharge(ctx, row.ID, actorID, actorType, ip)
	}
	return result, err
}

// CancelCharge only cancels the local Spuri charge record. AppyPay has no
// equivalent cancellation endpoint for REF, GPO, or GPO QR Codes, so a fresh
// provider consultation is mandatory before the cancellation is recorded.
func (s *Service) CancelCharge(ctx context.Context, contexto, academia, identifier, motivo, actorID, actorType, ip string) (ChargeResult, error) {
	if strings.TrimSpace(identifier) == "" {
		return ChargeResult{}, errors.New("id da cobrança é obrigatório")
	}
	row, err := s.loadCharge(ctx, identifier)
	if err != nil {
		return ChargeResult{}, err
	}
	if row.Contexto != contexto || row.Academia != academia || !canCancelCharge(row, academia, actorType) {
		return ChargeResult{}, fmt.Errorf("%w: cobrança não encontrada no contexto", ErrNotFound)
	}
	if isSuccessfulChargeStatus(row.Status) {
		return ChargeResult{ID: row.ID, ProviderChargeID: row.ProviderID, MerchantTransactionID: row.Merchant, Status: row.Status}, ErrPagamentoExistente
	}
	if isTerminalChargeStatus(row.Status) {
		return ChargeResult{ID: row.ID, ProviderChargeID: row.ProviderID, MerchantTransactionID: row.Merchant, Status: row.Status}, errors.New("cobrança já está em estado terminal e não pode ser cancelada")
	}
	current, err := s.consultCharge(ctx, row, actorID, actorType, ip)
	if err != nil {
		return current, err
	}
	if isSuccessfulChargeStatus(current.Status) {
		// O pagamento já existe no provedor mas ainda não tinha sido
		// confirmado localmente: reconcilia antes de recusar o cancelamento.
		s.reconciliarPagamentoDescoberto(ctx, row.ID, actorID, actorType, ip)
		return current, ErrPagamentoExistente
	}
	payload := make(map[string]any, len(row.Payload)+4)
	for key, value := range row.Payload {
		payload[key] = value
	}
	payload["charge_id"] = row.ID.String()
	payload["contexto_tipo"] = row.Contexto
	payload["codigo_academia"] = row.Academia
	payload["provider_charge_id"] = current.ProviderChargeID
	payload["status"] = "cancelada"
	payload["response"] = sanitize(current.Response)
	if motivo = strings.TrimSpace(motivo); motivo != "" {
		payload["motivo"] = motivo
	}
	if err = s.record(ctx, row.ID, aggregates.CobrancaCancelada, payload, actorID, actorType, ip); err != nil {
		return ChargeResult{}, err
	}
	return ChargeResult{ID: row.ID, ProviderChargeID: current.ProviderChargeID, MerchantTransactionID: row.Merchant, Status: "cancelada", Response: current.Response}, nil
}

// ListCobrancas lista cobranças de um contexto/academia, com filtros
// opcionais de estado e origem. turmaID, cursoID, anoAcademico e anoLetivo
// (introduzidos na tarefa 58) restringem adicionalmente o resultado às
// cobranças de mensalidade vinculadas a esse escopo (turma, curso, ano
// acadêmico ou ano letivo) — ver chargeIDsEscopoMensalidade. Como esse
// escopo só existe para cobranças de ORIGEM mensalidade, usar qualquer um
// desses quatro filtros exclui automaticamente cobranças de matrícula e
// avulsas do resultado; isso é intencional. Quando nenhum dos quatro é
// informado, o comportamento é idêntico ao anterior à tarefa 58.
// mes (tarefa 60) restringe adicionalmente a um mês de calendário (1-12)
// dentro do escopo — ver chargeIDsEscopoMensalidade. Só tem efeito quando
// combinado com pelo menos um dos quatro filtros de escopo acima; sozinho,
// não delimita o suficiente (haveria estudantes de vários anos letivos
// diferentes com cobranças naquele mesmo mês de calendário).
func (s *Service) ListCobrancas(ctx context.Context, contexto, academia string, estados, origens []string, turmaID, cursoID *uuid.UUID, anoAcademico, anoLetivo string, mes *int, limit, offset int) (*CobrancaListResult, error) {
	if s.client == nil {
		return nil, errors.New("serviço financeiro não inicializado")
	}
	where := "WHERE 1=1"
	args := []any{}
	i := 1
	if contexto != "" {
		where += fmt.Sprintf(" AND contexto_tipo=$%d", i)
		args = append(args, contexto)
		i++
	}
	if academia != "" {
		where += fmt.Sprintf(" AND codigo_academia=$%d", i)
		args = append(args, academia)
		i++
	}
	if len(estados) > 0 {
		where += fmt.Sprintf(" AND payload->>'status' = ANY($%d)", i)
		args = append(args, pq.Array(estadosCobrancaEquivalentes(estados)))
		i++
	}
	if len(origens) > 0 {
		clause, err := origensClause(origens)
		if err != nil {
			return nil, err
		}
		where += clause
	}
	if turmaID != nil || cursoID != nil || anoAcademico != "" || anoLetivo != "" {
		chargeIDs, err := s.chargeIDsEscopoMensalidade(ctx, academia, turmaID, cursoID, anoAcademico, anoLetivo, mes)
		if err != nil {
			return nil, err
		}
		where += fmt.Sprintf(" AND id = ANY($%d::uuid[])", i)
		args = append(args, pq.Array(chargeIDs))
		i++
	}
	var total int
	if err := s.client.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM financeiro_cobrancas "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT id, COALESCE(provider_charge_id,''), merchant_transaction_id, contexto_tipo, COALESCE(codigo_academia,''), payload, updated_at FROM financeiro_cobrancas %s ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, where, i, i+1)
	args = append(args, limit, offset)
	rows, err := s.client.DB().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CobrancaResumo{}
	for rows.Next() {
		dto, err := scanCobrancaResumo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, dto)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.PreencherMensalidadesEmAberto(ctx, out); err != nil {
		return nil, err
	}
	return &CobrancaListResult{Cobrancas: out, Total: total}, nil
}

// estadosCobrancaEquivalentes expande os valores de filtro "estado"
// informados pelo chamador para o conjunto completo de valores brutos que
// scanCobrancaResumo/normalizeChargeStatus tratam como equivalentes, antes
// de montar a cláusula SQL "payload->>'status' = ANY($n)" em ListCobrancas/
// ListCobrancasEstudante. Necessário porque esse filtro SQL compara o valor
// BRUTO gravado no ledger — e cobranças criadas antes desta tarefa ainda
// têm, no payload de eventos já gravados (o ledger é append-only, ver
// spuri_ledger), os valores antigos "solicitada"/"criada"/"Requested"/
// "Pending" em vez do estado canônico EstadoCobrancaAguardandoPagamento.
// Sem esta expansão, filtrar por estado=aguardando_pagamento encontraria só
// as cobranças criadas DEPOIS do deploy desta tarefa, escondendo qualquer
// cobrança antiga que ainda esteja nesse estado — inconsistente com o que
// scanCobrancaResumo mostra ao ler a mesma linha (que já normaliza na
// leitura).
//
// A mesma lacuna existia para "Failed" (bug relatado por Fredy: GET
// .../estudante/:codigo?estado=Failed devolvia vazio mesmo havendo
// cobranças falhadas do estudante, reproduzido também para academia/admin
// — ver tarefa 69). "Failed" é o valor cru que a própria AppyPay devolve
// quando o PROCESSADOR recusa a cobrança (docs/Parceiros e integrações/
// AppyPay Documentação.md). Antes da tarefa 69, CreateCharge/
// CreateGPOQRCode gravavam um valor local diferente, "falhada", quando a
// própria chamada HTTP à AppyPay falhava — nunca chegando a existir uma
// cobrança do lado do provedor, então a AppyPay nunca teve chance de
// devolver "Failed". Por decisão de Fredy, esta tarefa unifica os dois:
// daqui pra frente CreateCharge/CreateGPOQRCode gravam "Failed"
// diretamente nesse caso — "falhada" só continua a existir como o valor
// BRUTO de cobranças criadas antes do deploy desta tarefa (o ledger é
// append-only, imutável), exatamente a mesma situação de
// aguardando_pagamento/Pending/Requested/solicitada/criada acima, só que
// para o estado terminal Failed em vez do estado aguardando_pagamento.
//
// Qualquer outro valor de filtro (Success, Cancelled, Expired, ou qualquer
// string não reconhecida, incluindo EstadoPendente) passa inalterado — só
// "aguardando_pagamento" e "Failed" têm equivalência com valores brutos
// históricos diferentes de si mesmos.
func estadosCobrancaEquivalentes(estados []string) []string {
	out := make([]string, 0, len(estados))
	for _, estado := range estados {
		switch {
		case strings.EqualFold(strings.TrimSpace(estado), EstadoCobrancaAguardandoPagamento):
			out = append(out, EstadoCobrancaAguardandoPagamento, "Pending", "Requested", "solicitada", "criada")
		case strings.EqualFold(strings.TrimSpace(estado), "Failed"):
			out = append(out, "Failed", "falhada")
		default:
			out = append(out, estado)
		}
	}
	return out
}

// origensClause monta a cláusula SQL "AND (...)" que filtra
// financeiro_cobrancas pelo tipo de cobrança derivado do payload
// (mensalidade, matrícula ou avulsa) — a mesma derivação usada por
// scanCobrancaResumo. Devolve "" (sem filtro) quando origens está vazio.
// Extraída durante a tarefa 49 para ser compartilhada por ListCobrancas e
// ListCobrancasEstudante e nunca divergir entre as duas.
func origensClause(origens []string) (string, error) {
	if len(origens) == 0 {
		return "", nil
	}
	clauses := make([]string, 0, len(origens))
	for _, origem := range origens {
		switch origem {
		case "matricula":
			clauses = append(clauses, "COALESCE(payload->>'codigo_solicitacao','') <> ''")
		case "servico_extra":
			clauses = append(clauses, "(COALESCE(payload->>'codigo_solicitacao','') = '' AND COALESCE(payload->>'codigo_inscricao_servico','') <> '')")
		case "mensalidade":
			clauses = append(clauses, "(COALESCE(payload->>'codigo_solicitacao','') = '' AND COALESCE(payload->>'codigo_inscricao_servico','') = '' AND COALESCE(payload->>'codigo_estudante','') <> '')")
		case "avulsa":
			clauses = append(clauses, "(COALESCE(payload->>'codigo_solicitacao','') = '' AND COALESCE(payload->>'codigo_inscricao_servico','') = '' AND COALESCE(payload->>'codigo_estudante','') = '')")
		default:
			return "", fmt.Errorf("tipo de cobrança inválido: %s", origem)
		}
	}
	return " AND (" + strings.Join(clauses, " OR ") + ")", nil
}

// scanCobrancaResumo lê uma linha de financeiro_cobrancas (id,
// provider_charge_id, merchant_transaction_id, contexto_tipo,
// codigo_academia, payload, updated_at, nesta ordem exata) e deriva os
// campos resumidos a partir do payload persistido. Compartilhado por
// ListCobrancas e ListCobrancasEstudante para não duplicar a lógica de
// derivação de origem/método/mensalidades — extraído durante a tarefa 48 sem
// nenhuma mudança de comportamento em relação ao que ListCobrancas já fazia
// inline desde a tarefa 47.
func scanCobrancaResumo(rows *sql.Rows) (CobrancaResumo, error) {
	var dto CobrancaResumo
	var rawPayload []byte
	if err := rows.Scan(&dto.ID, &dto.ProviderChargeID, &dto.MerchantTransactionID, &dto.ContextoTipo, &dto.CodigoAcademia, &rawPayload, &dto.AtualizadoEm); err != nil {
		return CobrancaResumo{}, err
	}
	var payload map[string]any
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return CobrancaResumo{}, err
	}
	rawStatus, _ := payload["status"].(string)
	dto.Status = normalizeChargeStatus(rawStatus)
	dto.Provedor = provedorDoPayloadCobranca(payload)
	if codigo, ok := payload["codigo_provedor"].(float64); ok {
		c := int(codigo)
		dto.CodigoProvedor = &c
	}
	dto.MensagemProvedor, _ = payload["mensagem_provedor"].(string)
	dto.FonteProvedor, _ = payload["fonte_provedor"].(string)
	dto.CategoriaMotivo, _ = payload["categoria_motivo"].(string)
	dto.Valor, _ = payload["amount"].(float64)
	dto.Moeda, _ = payload["currency"].(string)
	dto.Descricao, _ = payload["description"].(string)
	dto.MetodoPagamento, _ = payload["payment_method"].(string)
	if qrType, ok := payload["qr_code_type"].(string); ok && qrType != "" {
		dto.MetodoPagamento = "GPO_QR"
	}
	dto.PagamentoExterno, _ = payload["pagamento_externo"].(bool)
	dto.ReferenciaExterna, _ = payload["pagamento_externo_referencia"].(string)
	dto.CodigoEstudante, _ = payload["codigo_estudante"].(string)
	dto.CodigoSolicitacao, _ = payload["codigo_solicitacao"].(string)
	dto.CodigoInscricaoServico, _ = payload["codigo_inscricao_servico"].(string)
	switch {
	case dto.CodigoSolicitacao != "":
		dto.Origem = "matricula"
	case dto.CodigoInscricaoServico != "":
		dto.Origem = "servico_extra"
	case dto.CodigoEstudante != "":
		dto.Origem = "mensalidade"
	default:
		dto.Origem = "avulsa"
	}
	if mesesRaw, ok := payload["mensalidades"]; ok && mesesRaw != nil {
		if b, err := json.Marshal(mesesRaw); err == nil {
			var meses []MensalidadeSelecaoMes
			if json.Unmarshal(b, &meses) == nil {
				dto.Mensalidades = meses
			}
		}
	}
	return dto, nil
}

// turmaID, cursoID, anoAcademico e anoLetivo (tarefa 58) filtram
// adicionalmente por escopo de mensalidade, igual a ListCobrancas. Como o
// escopo exige codigo_academia (ver escopoMensalidadeEstudantes), esses
// quatro filtros só têm efeito quando somenteAcademia não é nil (chamada de
// uma academia) — quando o estudante ou um admin FPP consultam sem
// restringir a academia, informar qualquer um desses quatro filtros
// devolve erro de validação, porque não há uma única academia para resolver
// o escopo de turma/curso contra o histórico do estudante.
// mes (tarefa 60) tem o mesmo efeito de ListCobrancas: só delimita a mais
// junto de um dos quatro filtros acima. Nenhum endpoint HTTP expõe este
// parâmetro para ListCobrancasEstudante ainda — a assinatura só ganhou o
// parâmetro para poder compartilhar chargeIDsEscopoMensalidade com
// ListCobrancas sem duplicar código; ver tarefa 60 para o contexto completo.
func (s *Service) ListCobrancasEstudante(ctx context.Context, codigoEstudante string, somenteAcademia *string, estados, origens []string, turmaID, cursoID *uuid.UUID, anoAcademico, anoLetivo string, mes *int, limit, offset int) (*CobrancaListResult, error) {
	if s.client == nil {
		return nil, errors.New("serviço financeiro não inicializado")
	}
	if codigoEstudante == "" {
		return nil, errors.New("código do estudante é obrigatório")
	}
	where := `WHERE (payload->>'codigo_estudante' = $1 OR payload->>'codigo_solicitacao' IN (SELECT codigo_solicitacao FROM projection_solicitacoes_matricula WHERE codigo_estudante_gerado = $1))`
	args := []any{codigoEstudante}
	i := 2
	if somenteAcademia != nil {
		where += fmt.Sprintf(" AND codigo_academia=$%d", i)
		args = append(args, *somenteAcademia)
		i++
	}
	if len(estados) > 0 {
		where += fmt.Sprintf(" AND payload->>'status' = ANY($%d)", i)
		args = append(args, pq.Array(estadosCobrancaEquivalentes(estados)))
		i++
	}
	if len(origens) > 0 {
		clause, err := origensClause(origens)
		if err != nil {
			return nil, err
		}
		where += clause
	}
	if turmaID != nil || cursoID != nil || anoAcademico != "" || anoLetivo != "" {
		academiaEscopo := ""
		if somenteAcademia != nil {
			academiaEscopo = *somenteAcademia
		}
		chargeIDs, err := s.chargeIDsEscopoMensalidade(ctx, academiaEscopo, turmaID, cursoID, anoAcademico, anoLetivo, mes)
		if err != nil {
			return nil, err
		}
		where += fmt.Sprintf(" AND id = ANY($%d::uuid[])", i)
		args = append(args, pq.Array(chargeIDs))
		i++
	}
	var total int
	if err := s.client.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM financeiro_cobrancas "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT id, COALESCE(provider_charge_id,''), merchant_transaction_id, contexto_tipo, COALESCE(codigo_academia,''), payload, updated_at FROM financeiro_cobrancas %s ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, where, i, i+1)
	args = append(args, limit, offset)
	rows, err := s.client.DB().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CobrancaResumo{}
	for rows.Next() {
		dto, err := scanCobrancaResumo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, dto)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.PreencherMensalidadesEmAberto(ctx, out); err != nil {
		return nil, err
	}
	return &CobrancaListResult{Cobrancas: out, Total: total}, nil
}

func canCancelCharge(row chargeRow, academia, actorType string) bool {
	switch actorType {
	case "admin":
		return row.Contexto == ContextoSpuri && row.Academia == ""
	case "academia":
		return row.Contexto == ContextoAcademia && row.Academia == academia
	default:
		return false
	}
}

func isSuccessfulChargeStatus(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "Success")
}

// isTerminalChargeStatus reporta se uma cobrança real chegou a um estado do
// qual nunca mais sai sozinha (não há mais nada que a AppyPay ou o Spuri
// vão fazer para resolvê-la). Cobre os dois estados locais ("cancelada" —
// cancelamento feito pelo Spuri; "falhada" — a própria chamada HTTP para a
// AppyPay falhou, sem chegar a existir uma cobrança do lado do provedor) e
// os quatro estados terminais documentados pela própria AppyPay e devolvidos
// verbatim (ver docs/Parceiros e integrações/AppyPay Documentação.md):
// Success (paga), Failed (recusada pelo processador), Cancelled (cancelada
// do lado da AppyPay) e Expired (referência REF expirou sem pagamento).
//
// Antes desta correção só "cancelada"/"falhada"/Success eram reconhecidos:
// uma cobrança devolvida pela AppyPay como Failed/Cancelled/Expired não era
// terminal aos olhos desta função, o que tinha dois efeitos colaterais
// reais — (1) CancelCharge não bloqueava uma segunda tentativa de
// cancelamento sobre uma cobrança já resolvida, podendo sobrescrever um
// status Failed genuíno com "cancelada", perdendo a razão real da falha; e
// (2) o SQL equivalente (chargeAbertaStatusExcluidos, mensalidade.go)
// tratava essa cobrança como "em aberto" para sempre. Precisa ficar
// manualmente sincronizada com chargeAbertaStatusExcluidos.
func isTerminalChargeStatus(status string) bool {
	trimmed := strings.TrimSpace(status)
	switch {
	case strings.EqualFold(trimmed, "cancelada"),
		strings.EqualFold(trimmed, "falhada"),
		strings.EqualFold(trimmed, "Failed"),
		strings.EqualFold(trimmed, "Cancelled"),
		strings.EqualFold(trimmed, "Expired"):
		return true
	default:
		return isSuccessfulChargeStatus(trimmed)
	}
}

// normalizeChargeStatus traduz o vocabulário histórico/bruto de status de
// uma cobrança real para os estados canônicos únicos que a API expõe,
// sempre que o valor de entrada tiver um equivalente canônico:
//
//   - EstadoCobrancaAguardandoPagamento (mensalidade.go): os estados locais
//     intermediários que o Spuri gravava antes da tarefa 66 ("solicitada",
//     gravado antes de qualquer chamada ao provedor; "criada", o fallback
//     usado quando o provedor responde 2xx sem nenhum campo de status) e os
//     estados brutos que a própria AppyPay documenta para esta mesma fase
//     ("Requested" e "Pending" — ver docs/Parceiros e integrações/AppyPay
//     Documentação.md).
//   - "Failed": desde a tarefa 69, também "falhada" — o valor local que
//     CreateCharge/CreateGPOQRCode gravavam antes desta tarefa quando a
//     própria chamada HTTP à AppyPay falhava (nunca chegando a existir uma
//     cobrança do lado do provedor, então a AppyPay nunca teve chance de
//     devolver "Failed" — ver estadosCobrancaEquivalentes, que resolve o
//     mesmo problema do lado do filtro SQL). Daqui pra frente
//     CreateCharge/CreateGPOQRCode já gravam "Failed" diretamente nesse caso;
//     "falhada" só volta a aparecer como o valor BRUTO de uma cobrança
//     criada antes do deploy desta tarefa — e mesmo assim nunca chega ao
//     chamador, porque normalizeChargeStatus a traduz aqui na leitura.
//
// Qualquer outro valor (Success, Cancelled, Expired, os dois canônicos
// acima, ou uma string não reconhecida) é devolvido inalterado — a função é
// idempotente e pode ser chamada tanto sobre um valor bruto recém-recebido
// da AppyPay quanto sobre um valor já gravado (histórico ou canônico).
//
// Entrada vazia é devolvida vazia: normalizeChargeStatus nunca decide um
// fallback por conta própria, porque o fallback correto depende do
// contexto de quem chama (ex.: CreateCharge trata "" como uma cobrança
// nova ainda sem informação = aguardando pagamento; já consultCharge trata
// "" como "o provedor não devolveu nada desta vez, mantém o status
// anterior").
func normalizeChargeStatus(raw string) string {
	trimmed := strings.TrimSpace(raw)
	switch {
	case trimmed == "":
		return ""
	case strings.EqualFold(trimmed, "Pending"),
		strings.EqualFold(trimmed, "Requested"),
		strings.EqualFold(trimmed, "solicitada"),
		strings.EqualFold(trimmed, "criada"):
		return EstadoCobrancaAguardandoPagamento
	case strings.EqualFold(trimmed, "falhada"):
		return "Failed"
	default:
		return trimmed
	}
}

// consultCharge is shared by normal consultation and cancellation's mandatory
// pre-check. A late Success after local cancellation is preserved as a
// reconciliation conflict and never changes the local cancelled status.
func (s *Service) consultCharge(ctx context.Context, row chargeRow, actorID, actorType, ip string) (ChargeResult, error) {
	// Pagamento registado manualmente pela academia (fora da plataforma): o
	// provedor continuará a reportar Pending/Expired para sempre, então uma
	// consulta nunca pode rebaixar esta cobrança de Success.
	if externo, _ := row.Payload["pagamento_externo"].(bool); externo && isSuccessfulChargeStatus(row.Status) {
		return ChargeResult{ID: row.ID, ProviderChargeID: row.ProviderID, MerchantTransactionID: row.Merchant, Status: row.Status}, nil
	}
	// Retroativo: a consulta segue o provedor gravado na cobrança, não o
	// provedor ativo do contexto.
	prov, cred, err := s.provedorDaCobranca(ctx, row)
	if err != nil {
		return ChargeResult{}, err
	}
	resp, err := prov.ConsultarCobranca(ctx, cred, referenciaCobranca{IDProvedor: row.ProviderID, IDComerciante: row.Merchant})
	if err != nil {
		return ChargeResult{}, err
	}
	response, outcome := resp.Bruta, resp.Outcome
	status := normalizeChargeStatus(outcome.Status)
	if status == "" {
		// O provedor não devolveu nenhum campo de status reconhecível desta vez
		// — mantém o status anterior (row.Status já vem normalizado por
		// loadCharge) em vez de assumir um novo estado.
		status = row.Status
	}
	previousResponse := row.Payload["response"]
	payload := make(map[string]any, len(row.Payload)+7)
	for key, value := range row.Payload {
		payload[key] = value
	}
	payload["provider_charge_id"] = first(resp.ID, row.ProviderID)
	payload["status"] = status
	payload["response"] = sanitize(response)
	comProvedor(payload, row.Provedor)
	applyProviderOutcome(payload, outcome)
	providerID := first(resp.ID, row.ProviderID)
	result := ChargeResult{ID: row.ID, Provedor: row.Provedor, ProviderChargeID: providerID, MerchantTransactionID: row.Merchant, Status: status, Response: response}
	if outcome.HasCode {
		code := outcome.Code
		result.CodigoProvedor = &code
	}
	result.MensagemProvedor, result.FonteProvedor, result.CategoriaMotivo = outcome.Message, outcome.Source, outcome.Categoria
	if strings.EqualFold(row.Status, "cancelada") && isSuccessfulChargeStatus(status) {
		// Keep the cancellation definitive in the read model. The provider
		// result is recorded for manual FPP reconciliation instead of silently
		// accepting a payment that raced with local cancellation.
		payload["status"] = "cancelada"
		payload["provider_status"] = status
		if err = s.record(ctx, row.ID, aggregates.CobrancaConflitoPosCancelamento, payload, actorID, actorType, ip); err != nil {
			return ChargeResult{}, err
		}
		result.Status = "cancelada"
		return result, nil
	}
	if status != row.Status || providerID != row.ProviderID || !sameJSON(payload["response"], previousResponse) {
		if err = s.record(ctx, row.ID, aggregates.CobrancaConsultada, payload, actorID, actorType, ip); err != nil {
			return ChargeResult{}, err
		}
	}
	return result, nil
}

func (s *Service) record(ctx context.Context, id uuid.UUID, event string, payload map[string]any, userID, userType, ip string) error {
	if strings.TrimSpace(userID) == "" {
		return errors.New("autor do evento financeiro é obrigatório")
	}
	agg := aggregates.NewFinanceiroWithID(id)
	clean, _ := sanitize(payload).(map[string]any)
	agg.Registrar(event, clean)
	if err := s.repository.WithContext(ctx).SaveWithAudit(agg, db.AuditContext{UserID: userID, UserType: userType, IP: ip}); err != nil {
		return err
	}
	return s.projection.ApplyLatestForAggregate(id)
}

// liveChargeStatus faz uma consulta ao vivo (GET /charges/{id}) e devolve o
// status autoritativo reportado pela AppyPay nesse exato momento. É a
// medida de segurança que a própria documentação da AppyPay recomenda antes
// de tratar um webhook de sucesso como definitivo — "Important: As a
// security measure, double check the transaction by calling the GET
// /charges endpoint" (secção "Merchant Webhooks"), reforçada na secção
// interna "Escopo do Módulo Financeiro Base": "Ao receber, é recomendado
// confirmar o estado com um GET /charges/{id} antes de aplicar efeitos de
// negócio irreversíveis".
//
// Não persiste nada por si própria — quem chama decide o que gravar a
// partir do status devolvido. O segundo valor devolvido é false quando a
// consulta em si falhou (upstream indisponível, timeout, credencial
// temporariamente inacessível) ou não devolveu nenhum status reconhecível
// — nesses casos quem chama deve cair para confiar no que o webhook
// reportou, em vez de bloquear a confirmação: um GET indisponível não pode
// deixar uma cobrança presa sem nunca confirmar um pagamento que a própria
// AppyPay já reportou como bem-sucedido no webhook (o mesmo raciocínio do
// Bug 1 já corrigido, em que o webhook nunca efetivava a matrícula).
func (s *Service) liveChargeStatus(ctx context.Context, charge chargeRow) (status string, ok bool) {
	prov, cred, err := s.provedorDaCobranca(ctx, charge)
	if err != nil {
		return "", false
	}
	resp, err := prov.ConsultarCobranca(ctx, cred, referenciaCobranca{IDProvedor: charge.ProviderID, IDComerciante: charge.Merchant})
	if err != nil {
		return "", false
	}
	status = normalizeChargeStatus(resp.Outcome.Status)
	return status, status != ""
}

// AcceptWebhook reserves its event id first in the dedicated idempotency index.
// If ledger persistence fails the reservation is removed, so a delivery retry is
// still processed. No charge side-effect is executed here.
func (s *Service) AcceptWebhook(ctx context.Context, provedor, metodo, eventID string, owner WebhookOwner, payload map[string]any) (accepted bool, confirmedSuccess bool, err error) {
	prov, err := s.ProvedorPorID(provedor)
	if err != nil {
		return false, false, err
	}
	metodo = strings.ToUpper(metodo)
	metodoValido := false
	for _, r := range prov.RotasWebhook() {
		if r.Metodo == metodo {
			metodoValido = true
		}
	}
	if !metodoValido || strings.TrimSpace(eventID) == "" {
		return false, false, errors.New("webhook inválido")
	}
	actor := prov.Slug() + ":webhook"
	res, err := s.client.DB().ExecContext(ctx, `INSERT INTO financeiro_webhooks_recebidos(provedor,event_id,metodo) VALUES($1,$2,$3) ON CONFLICT(provedor,event_id) DO NOTHING`, prov.ID(), eventID, metodo)
	if err != nil {
		return false, false, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return false, false, nil
	}
	data := map[string]any{"provedor": prov.ID(), "event_id": eventID, "metodo": metodo, "credential_id": owner.CredentialID.String(), "contexto_tipo": owner.ContextoTipo, "codigo_academia": owner.CodigoAcademia, "payload": sanitize(payload)}
	if err = s.record(ctx, uuid.New(), aggregates.WebhookProvedorRecebido, data, actor, "sistema", "webhook"); err != nil {
		_, _ = s.client.DB().ExecContext(ctx, `DELETE FROM financeiro_webhooks_recebidos WHERE provedor=$1 AND event_id=$2`, prov.ID(), eventID)
		return false, false, err
	}
	// Reflete no read model qualquer estado que o webhook reporte — sucesso
	// (Success) ou qualquer um dos outros três estados terminais que a
	// própria AppyPay documenta (Failed, Cancelled, Expired). Antes desta
	// correção só um webhook de sucesso atualizava a cobrança: um webhook
	// avisando que uma referência REF expirou ou que um GPO foi recusado
	// era gravado em WebhookProvedorRecebido (acima) mas nunca refletia em
	// financeiro_cobrancas, deixando a cobrança "presa" em
	// aguardando_pagamento até alguém consultá-la manualmente.
	outcome, payloadProviderID := prov.ResultadoWebhook(payload)
	if outcome.Status != "" || outcome.HasCode {
		normalized := normalizeChargeStatus(outcome.Status)
		success := isSuccessfulChargeStatus(normalized)
		if charge, loadErr := s.loadChargeDoProvedor(ctx, prov.ID(), eventID); loadErr == nil && charge.Contexto == owner.ContextoTipo && charge.Academia == owner.CodigoAcademia &&
			// Um webhook atrasado e não-bem-sucedido nunca sobrescreve uma
			// cobrança que já chegou a um estado terminal (ex.: já paga, já
			// cancelada) — só um sucesso tem tratamento de conflito próprio
			// (abaixo) que pode correr por cima de um estado terminal local.
			(success || !isTerminalChargeStatus(charge.Status)) &&
			// Cobrança que a academia marcou como paga fora da plataforma é
			// definitiva: nenhum webhook posterior (nem um Success cuja
			// consulta ao vivo ainda diga Pending) pode alterá-la.
			!cobrancaPagaExternamente(charge) {
			// Double-check: um webhook de sucesso "normal" (a cobrança não
			// está cancelada localmente) só é tratado como definitivo depois
			// de uma consulta ao vivo concordar. O caso de sucesso chegando
			// depois de um cancelamento local (abaixo, eventType
			// CobrancaConflitoPosCancelamento) já não dispara nenhum
			// efeito irreversível por si só — é só um registo de conflito
			// para reconciliação manual — então não precisa do double-check.
			if success && !strings.EqualFold(charge.Status, "cancelada") {
				if live, ok := s.liveChargeStatus(ctx, charge); ok {
					normalized = live
					success = isSuccessfulChargeStatus(live)
				}
			}
			updated := make(map[string]any, len(charge.Payload)+7)
			for k, v := range charge.Payload {
				updated[k] = v
			}
			updated["status"] = normalized
			if success {
				updated["status"] = "Success"
			}
			updated["provider_charge_id"] = first(payloadProviderID, charge.ProviderID)
			updated["response"] = sanitize(payload)
			comProvedor(updated, prov.ID())
			applyProviderOutcome(updated, outcome)
			eventType := aggregates.CobrancaConsultada
			if success && strings.EqualFold(charge.Status, "cancelada") {
				// A provider may still settle a REF/GPO/QR after Spuri's local
				// cancellation. Preserve cancellation and leave an explicit audit
				// conflict for FPP reconciliation.
				updated["status"] = "cancelada"
				updated["provider_status"] = "Success"
				eventType = aggregates.CobrancaConflitoPosCancelamento
			}
			if s.record(ctx, charge.ID, eventType, updated, actor, "sistema", "webhook") == nil && success && eventType == aggregates.CobrancaConsultada {
				confirmedSuccess = true
				_ = s.confirmMensalidadeCharge(ctx, charge.ID, actor, "sistema", "webhook")
			}
		}
	}
	return true, confirmedSuccess, nil
}

type chargeRow struct {
	ID                                               uuid.UUID
	ProviderID, Merchant, Contexto, Academia, Status string
	// Provedor é o PSP que emitiu a cobrança. Toda operação posterior sobre a
	// cobrança (consulta, cancelamento, webhook) usa este provedor — nunca o
	// provedor ativo do contexto, que pode ter mudado entretanto.
	Provedor string
	Payload  map[string]any
}

func (s *Service) loadCharge(ctx context.Context, identifier string) (chargeRow, error) {
	return s.loadChargeWhere(ctx, identifier, "")
}

// loadChargeDoProvedor localiza uma cobrança dentro de um provedor — os
// identificadores do provedor só são únicos dentro dele.
func (s *Service) loadChargeDoProvedor(ctx context.Context, provedor, identifier string) (chargeRow, error) {
	return s.loadChargeWhere(ctx, identifier, provedor)
}

func (s *Service) loadChargeWhere(ctx context.Context, identifier, provedor string) (chargeRow, error) {
	var r chargeRow
	var raw []byte
	err := s.client.DB().QueryRowContext(ctx, `SELECT id,COALESCE(provider_charge_id,''),merchant_transaction_id,contexto_tipo,COALESCE(codigo_academia,''),provedor,payload FROM financeiro_cobrancas WHERE (id::text=$1 OR provider_charge_id=$1 OR merchant_transaction_id=$1) AND ($2='' OR provedor=$2) ORDER BY (id::text=$1) DESC,(merchant_transaction_id=$1) DESC LIMIT 1`, identifier, provedor).Scan(&r.ID, &r.ProviderID, &r.Merchant, &r.Contexto, &r.Academia, &r.Provedor, &raw)
	if err != nil {
		return r, fmt.Errorf("%w: cobrança não encontrada", ErrNotFound)
	}
	if err = json.Unmarshal(raw, &r.Payload); err != nil {
		return r, err
	}
	rawStatus, _ := r.Payload["status"].(string)
	r.Status = normalizeChargeStatus(rawStatus)
	return r, nil
}

// provedorDaCobranca resolve o provedor e as credenciais de uma cobrança JÁ
// EMITIDA. É o mecanismo retroativo: mesmo que o contexto tenha trocado de
// provedor, a cobrança continua a falar com o provedor que a criou enquanto
// aguarda a resposta.
func (s *Service) provedorDaCobranca(ctx context.Context, row chargeRow) (Provedor, credencialProvedor, error) {
	prov, err := s.ProvedorPorID(row.Provedor)
	if err != nil {
		return nil, credencialProvedor{}, err
	}
	cred, err := s.loadCredentialDoProvedor(ctx, row.Contexto, row.Academia, prov.ID())
	if err != nil {
		return nil, credencialProvedor{}, err
	}
	return prov, cred, nil
}

// comProvedor grava no payload o provedor que originou o evento.
func comProvedor(payload map[string]any, provedor string) map[string]any {
	payload["provedor"] = provedor
	return payload
}

func (s *Service) reserveCharge(ctx context.Context, merchant string, chargeID uuid.UUID) (bool, error) {
	res, err := s.client.DB().ExecContext(ctx, `INSERT INTO financeiro_cobrancas_reservas (merchant_transaction_id,charge_id) VALUES ($1,$2) ON CONFLICT (merchant_transaction_id) DO NOTHING`, merchant, chargeID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

func (s *Service) releaseChargeReservation(ctx context.Context, merchant string, chargeID uuid.UUID) error {
	_, err := s.client.DB().ExecContext(ctx, `DELETE FROM financeiro_cobrancas_reservas WHERE merchant_transaction_id=$1 AND charge_id=$2`, merchant, chargeID)
	return err
}

// applyPersistedProviderFields preenche os campos de motivo de um
// ChargeResult a partir de um payload já persistido (financeiro_cobrancas),
// para que uma resposta idempotente (mesmo merchantTransactionId reenviado)
// devolva exatamente a mesma informação que a criação original devolveu —
// ver applyProviderOutcome, gravado neste mesmo payload em CreateCharge/
// CreateGPOQRCode/consultCharge/AcceptWebhook.
func applyPersistedProviderFields(result *ChargeResult, payload map[string]any) {
	if codigo, ok := payload["codigo_provedor"].(float64); ok {
		c := int(codigo)
		result.CodigoProvedor = &c
	}
	result.MensagemProvedor, _ = payload["mensagem_provedor"].(string)
	result.FonteProvedor, _ = payload["fonte_provedor"].(string)
	result.CategoriaMotivo, _ = payload["categoria_motivo"].(string)
}

func (s *Service) existingChargeResult(ctx context.Context, merchant, contexto, academia string) (ChargeResult, error) {
	row, err := s.loadCharge(ctx, merchant)
	if err != nil {
		return ChargeResult{}, err
	}
	if row.Contexto != contexto || row.Academia != academia {
		// merchantTransactionId is globally reserved, but its original result
		// must never be disclosed outside its financial context.
		return ChargeResult{}, ErrConflict
	}
	response, _ := row.Payload["response"].(map[string]any)
	result := ChargeResult{ID: row.ID, Provedor: row.Provedor, ProviderChargeID: row.ProviderID, MerchantTransactionID: row.Merchant, Status: row.Status, Response: response}
	applyPersistedProviderFields(&result, row.Payload)
	return result, nil
}

func (s *Service) existingQRCodeResult(ctx context.Context, merchant, contexto, academia string) (QRCodeResult, error) {
	row, err := s.loadCharge(ctx, merchant)
	if err != nil {
		return QRCodeResult{}, err
	}
	return qrCodeResultFromRow(row, contexto, academia)
}

func qrCodeResultFromRow(row chargeRow, contexto, academia string) (QRCodeResult, error) {
	if row.Contexto != contexto || row.Academia != academia {
		return QRCodeResult{}, ErrConflict
	}
	if _, ok := row.Payload["qr_code_type"].(string); !ok {
		return QRCodeResult{}, ErrConflict
	}
	response, _ := row.Payload["response"].(map[string]any)
	qr, _ := response["qrCodeArr"].(string)
	chargeResult := ChargeResult{ID: row.ID, Provedor: row.Provedor, ProviderChargeID: row.ProviderID, MerchantTransactionID: row.Merchant, Status: row.Status, Response: response}
	applyPersistedProviderFields(&chargeResult, row.Payload)
	return QRCodeResult{ChargeResult: chargeResult, QRCodeArr: qr}, nil
}

func sameJSON(a, b any) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}

func validateCharge(in *ChargeRequest) error {
	if err := validContext(in.ContextoTipo, in.CodigoAcademia); err != nil {
		return err
	}
	if !validAmount(in.Amount) || strings.TrimSpace(in.Description) == "" {
		return errors.New("amount e description são obrigatórios")
	}
	if roundAmount(in.Amount) != in.Amount {
		return errors.New("amount deve ter no máximo duas casas decimais")
	}
	if in.Currency == "" {
		in.Currency = "AOA"
	}
	if in.MerchantTransactionID != "" && !validMerchantID(in.MerchantTransactionID) {
		return errors.New("merchantTransactionId deve ser alfanumérico e ter no máximo 15 caracteres")
	}
	if strings.ToUpper(in.Currency) != "AOA" {
		return errors.New("currency deve ser AOA")
	}
	if len(in.Options) > 2 {
		return errors.New("options aceita no máximo duas chaves")
	}
	m := strings.ToUpper(in.PaymentMethod)
	if m != "GPO" && m != "REF" && !strings.HasPrefix(m, "GPO_") && !strings.HasPrefix(m, "REF_") {
		return errors.New("método de pagamento fora do escopo")
	}
	phone, phoneOK := in.PaymentInfo["phoneNumber"].(string)
	if strings.HasPrefix(m, "GPO") && (!phoneOK || strings.TrimSpace(phone) == "") {
		return errors.New("GPO exige paymentInfo.phoneNumber")
	}
	if strings.HasPrefix(m, "REF") && len(in.PaymentInfo) > 0 {
		// Duas formas válidas de paymentInfo para REF:
		//  1. Só dueDate (o caso introduzido nesta tarefa): a referência
		//     continua gerada pelo gateway (AppyPay escolhe
		//     referenceNumber), só o prazo de expiração é customizado —
		//     ver gerarCobranca em cobranca_geracao.go e o comentário sobre
		//     a hipótese ainda não confirmada contra o ambiente real da
		//     AppyPay.
		//  2. Os três campos completos (referenceNumber+dueDate+nib): a
		//     forma "referência gerada pelo comerciante" documentada pela
		//     AppyPay. Nenhum chamador atual usa esta forma — mantida por
		//     integridade caso um chamador futuro precise dela.
		_, hasDueDateOnly := in.PaymentInfo["dueDate"].(string)
		if hasDueDateOnly && len(in.PaymentInfo) == 1 {
			// válido: só dueDate.
		} else {
			for _, k := range []string{"referenceNumber", "dueDate", "nib"} {
				value, ok := in.PaymentInfo[k].(string)
				if !ok || strings.TrimSpace(value) == "" {
					return fmt.Errorf("REF com paymentInfo exige %s (ou apenas dueDate sozinho)", k)
				}
			}
		}
	}
	return nil
}

func validateQRCode(in *QRCodeRequest) error {
	if err := validContext(in.ContextoTipo, in.CodigoAcademia); err != nil {
		return err
	}
	if !validAmount(in.Amount) || strings.TrimSpace(in.Description) == "" {
		return errors.New("amount e description são obrigatórios")
	}
	if roundAmount(in.Amount) != in.Amount {
		return errors.New("amount deve ter no máximo duas casas decimais")
	}
	if in.Currency == "" {
		in.Currency = "AOA"
	}
	if !strings.EqualFold(in.Currency, "AOA") {
		return errors.New("currency deve ser AOA")
	}
	if in.MinAmount != nil {
		if !validAmount(*in.MinAmount) {
			return errors.New("minAmount deve ser maior que zero")
		}
		if roundAmount(*in.MinAmount) != *in.MinAmount {
			return errors.New("minAmount deve ter no máximo duas casas decimais")
		}
	}
	return nil
}

func validAmount(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0 }

func merchantID() string {
	b := make([]byte, 7)
	_, _ = rand.Read(b)
	return "T" + strings.ToUpper(hex.EncodeToString(b))
}

func validMerchantID(value string) bool {
	if len(value) == 0 || len(value) > 15 {
		return false
	}
	for _, r := range value {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func chargePayload(id uuid.UUID, in ChargeRequest, providerID, status string, response map[string]any) map[string]any {
	return map[string]any{"charge_id": id.String(), "contexto_tipo": in.ContextoTipo, "codigo_academia": in.CodigoAcademia, "codigo_estudante": in.CodigoEstudante, "codigo_solicitacao": in.CodigoSolicitacao,
		"codigo_inscricao_servico": in.CodigoInscricaoServico, "tipo_lancamento_servico_extra": in.TipoLancamentoServicoExtra, "mes_referencia": in.MesReferencia, "ano_referencia": in.AnoReferencia, "mensalidades": in.Mensalidades, "amount": roundAmount(in.Amount), "currency": in.Currency, "description": in.Description, "merchant_transaction_id": in.MerchantTransactionID, "payment_method": in.PaymentMethod, "payment_info": in.PaymentInfo, "options": in.Options, "notify": in.Notify, "async": in.Async, "provider_charge_id": providerID, "status": status, "response": sanitize(response)}
}

func qrCodePayload(id uuid.UUID, in QRCodeRequest, typ, providerID, status string, response map[string]any) map[string]any {
	var minAmount any
	if in.MinAmount != nil {
		minAmount = roundAmount(*in.MinAmount)
	}
	return map[string]any{"charge_id": id.String(), "contexto_tipo": in.ContextoTipo, "codigo_academia": in.CodigoAcademia, "codigo_estudante": in.CodigoEstudante, "codigo_solicitacao": in.CodigoSolicitacao,
		"codigo_inscricao_servico": in.CodigoInscricaoServico, "tipo_lancamento_servico_extra": in.TipoLancamentoServicoExtra, "mes_referencia": in.MesReferencia, "ano_referencia": in.AnoReferencia, "amount": roundAmount(in.Amount), "currency": in.Currency, "description": in.Description, "merchant_transaction_id": in.MerchantTransactionID, "provider_charge_id": providerID, "status": status, "payment_method": "GPO", "qr_code_type": typ, "mensalidades": in.Mensalidades, "min_amount": minAmount, "max_transactions": in.MaxTransactions, "start_date": in.StartDate, "end_date": in.EndDate, "response": sanitize(response)}
}

// providerOutcome carrega tudo o que a Spuri consegue aprender de uma única
// resposta/webhook da AppyPay sobre o resultado real de uma cobrança: o
// status usado para acionar a máquina de estados interna (Status, já
// resolvido — ver resolveOutcomeStatus), mais tudo o que é preciso para
// explicar "porquê" a um humano (Code/Message/Source, crus, exatamente como
// a AppyPay os enviou) e uma categoria de melhor esforço para filtragem
// programática (Categoria).
type providerOutcome struct {
	Status    string
	Code      int
	HasCode   bool
	Message   string
	Source    string
	Categoria string
}

func applyProviderOutcome(payload map[string]any, outcome providerOutcome) {
	if outcome.HasCode {
		payload["codigo_provedor"] = outcome.Code
	}
	if outcome.Message != "" {
		payload["mensagem_provedor"] = outcome.Message
	}
	if outcome.Source != "" {
		payload["fonte_provedor"] = outcome.Source
	}
	if outcome.Categoria != "" {
		payload["categoria_motivo"] = outcome.Categoria
	}
}

func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func sanitize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			l := strings.ToLower(k)
			if strings.Contains(l, "secret") || strings.Contains(l, "token") || strings.Contains(l, "apikey") || strings.Contains(l, "api_key") || strings.Contains(l, "authorization") {
				continue
			}
			out[k] = sanitize(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = sanitize(x[i])
		}
		return out
	default:
		return v
	}
}

// financeKeyLabel separa criptograficamente a chave financeira derivada de
// JWT_SECRET de qualquer outra chave derivada da mesma raiz (ex.:
// comunicação) — ver spuri/internal/security.
const financeKeyLabel = "spuri:finance-encryption:v1"

// key deriva a chave AES-256 atual (a partir de JWT_SECRET) usada para
// cifrar e decifrar todo segredo financeiro.
func key() ([]byte, error) {
	return security.DeriveKey(financeKeyLabel)
}

// ValidateEncryptionConfig validates the mandatory financial-secret key at
// startup, before a request can attempt to persist any credentials.
func ValidateEncryptionConfig() error {
	_, err := key()
	return err
}

func encrypt(v string) (string, error) {
	k, err := key()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(append(nonce, gcm.Seal(nil, nonce, []byte(v), nil)...)), nil
}

func decrypt(v string) (string, error) {
	k, err := key()
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext inválido")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	return string(plain), err
}

func constantTimeEqual(a, b string) bool { return hmac.Equal([]byte(a), []byte(b)) }
