package aggregates

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Financeiro keeps no mutable business state; its read state is rebuilt solely
// from immutable events, using the same repository guarantees as all aggregates.
type Financeiro struct{ *BaseAggregate }
type EventoFinanceiro struct{ BaseEvent }

// Event names owned by the Financeiro ledger aggregate.
//
// Os eventos de provedor de pagamento (PSP) são neutros: o provedor que
// originou o evento vai no payload ("provedor"), nunca no nome. Os nomes
// antigos, prefixados com AppyPay, continuam existindo no ledger (que é
// imutável) e são aceitos pela projeção através de EventoFinanceiroCanonico.
//
// CobrancaCancelada é um cancelamento local do Spuri;
// CobrancaConflitoPosCancelamento registra um Success do provedor observado
// depois desse cancelamento local definitivo.
const (
	CredenciaisProvedorConfiguradas   = "CredenciaisProvedorConfiguradas"
	CredenciaisProvedorRemovidas      = "CredenciaisProvedorRemovidas"
	CredenciaisArquivadasPurgadas     = "CredenciaisArquivadasPurgadas"
	SegredoWebhookProvedorRotacionado = "SegredoWebhookProvedorRotacionado"
	ProvedorPagamentoDefinido         = "ProvedorPagamentoDefinido"
	CobrancaSolicitada                = "CobrancaSolicitada"
	CobrancaCriada                    = "CobrancaCriada"
	CobrancaFalhou                    = "CobrancaFalhou"
	CobrancaConsultada                = "CobrancaConsultada"
	CobrancaCancelada                 = "CobrancaCancelada"
	CobrancaConflitoPosCancelamento   = "CobrancaConflitoPosCancelamento"
	QRCodeSolicitado                  = "QRCodeSolicitado"
	QRCodeGerado                      = "QRCodeGerado"
	QRCodeFalhou                      = "QRCodeFalhou"
	WebhookProvedorRecebido           = "WebhookProvedorRecebido"
	MensalidadeConfigurada            = "MensalidadeConfigurada"
	MensalidadeConfiguracaoRemovida   = "MensalidadeConfiguracaoRemovida"
	MesInicioCobrancaDefinido         = "MesInicioCobrancaDefinido"
	MesInicioCobrancaRemovido         = "MesInicioCobrancaRemovido"
	ObrigacaoMensalidadeAnulada       = "ObrigacaoMensalidadeAnulada"
	ObrigacaoMensalidadeReativada     = "ObrigacaoMensalidadeReativada"
	MensalidadePaga                   = "MensalidadePaga"
	ObrigacaoServicoExtraAnulada      = "ObrigacaoServicoExtraAnulada"
	ObrigacaoServicoExtraReativada    = "ObrigacaoServicoExtraReativada"
	ServicoExtraLancamentoPago        = "ServicoExtraLancamentoPago"
	MatriculaConfigurada              = "MatriculaConfigurada"
	MatriculaConfiguracaoRemovida     = "MatriculaConfiguracaoRemovida"
)

// eventosFinanceirosLegados mapeia os nomes de evento gravados antes da
// Tarefa 119 (prefixados com AppyPay) para o nome neutro equivalente. Eventos
// com estes nomes existem para sempre no ledger e continuam sendo
// reprocessados pela projeção; nenhum código novo os grava.
var eventosFinanceirosLegados = map[string]string{
	"CredenciaisAppyPayConfiguradas":         CredenciaisProvedorConfiguradas,
	"CredenciaisAppyPayRemovidas":            CredenciaisProvedorRemovidas,
	"SegredoWebhookAppyPayRotacionado":       SegredoWebhookProvedorRotacionado,
	"CobrancaAppyPaySolicitada":              CobrancaSolicitada,
	"CobrancaAppyPayCriada":                  CobrancaCriada,
	"CobrancaAppyPayFalhou":                  CobrancaFalhou,
	"CobrancaAppyPayConsultada":              CobrancaConsultada,
	"CobrancaAppyPayCancelada":               CobrancaCancelada,
	"CobrancaAppyPayConflitoPosCancelamento": CobrancaConflitoPosCancelamento,
	"QRCodeAppyPaySolicitado":                QRCodeSolicitado,
	"QRCodeAppyPayGerado":                    QRCodeGerado,
	"QRCodeAppyPayFalhou":                    QRCodeFalhou,
	"WebhookAppyPayRecebido":                 WebhookProvedorRecebido,
}

// EventoFinanceiroCanonico devolve o nome neutro de um evento financeiro,
// traduzindo os nomes legados com prefixo AppyPay. Nomes que já são neutros
// (ou desconhecidos) voltam inalterados.
func EventoFinanceiroCanonico(eventType string) string {
	if canonical, ok := eventosFinanceirosLegados[eventType]; ok {
		return canonical
	}
	return eventType
}

// EventosFinanceirosLegados devolve os nomes legados (para allowlists e testes).
func EventosFinanceirosLegados() []string {
	out := make([]string, 0, len(eventosFinanceirosLegados))
	for legacy := range eventosFinanceirosLegados {
		out = append(out, legacy)
	}
	return out
}

// CobrancaPagamentoExternoRegistrado marca uma cobrança como paga FORA da
// plataforma (decisão manual da academia). O payload leva status "Success" e
// pagamento_externo=true, de modo que todo o pipeline posterior (confirmação
// de mensalidade, matrícula, serviço extra) trate a cobrança exatamente como
// uma paga dentro do ecossistema.
const CobrancaPagamentoExternoRegistrado = "CobrancaPagamentoExternoRegistrado"

func NewFinanceiro() *Financeiro { return NewFinanceiroWithID(uuid.New()) }
func NewFinanceiroWithID(id uuid.UUID) *Financeiro {
	return &Financeiro{BaseAggregate: &BaseAggregate{ID: id, UncommittedEvents: []DomainEvent{}}}
}
func (f *Financeiro) GetType() string { return "Financeiro" }
func (f *Financeiro) Registrar(eventType string, payload map[string]any) {
	f.RaiseEvent(&EventoFinanceiro{BaseEvent: BaseEvent{EventType: eventType, AggregateID: f.ID, Payload: payload}})
}
func (f *Financeiro) Apply(event DomainEvent) error {
	if event.GetAggregateID() == uuid.Nil {
		return fmt.Errorf("aggregate financeiro inválido")
	}
	if f.ID == uuid.Nil {
		f.ID = event.GetAggregateID()
	}
	f.Version++
	return nil
}
func (e EventoFinanceiro) ToJSON() ([]byte, error) { return json.Marshal(e) }

// ConfiguracaoMensalidade is the dedicated financial aggregate for one
// academy. It deliberately shares the Financeiro ledger stream type: the
// ledger therefore retains one audited, ordered event stream per academy,
// while the financial projection derives every read model from it.
//
// The aggregate has no mutable shortcut state. Reconfigurations, exemptions
// and future payment confirmations are all immutable events.
type ConfiguracaoMensalidade struct{ *BaseAggregate }

func NewConfiguracaoMensalidadeWithID(id uuid.UUID) *ConfiguracaoMensalidade {
	return &ConfiguracaoMensalidade{BaseAggregate: &BaseAggregate{ID: id, UncommittedEvents: []DomainEvent{}}}
}

func (c *ConfiguracaoMensalidade) GetType() string { return "Financeiro" }
func (c *ConfiguracaoMensalidade) Registrar(eventType string, payload map[string]any) {
	c.RaiseEvent(&EventoFinanceiro{BaseEvent: BaseEvent{EventType: eventType, AggregateID: c.ID, Payload: payload}})
}
func (c *ConfiguracaoMensalidade) Apply(event DomainEvent) error {
	if event.GetAggregateID() == uuid.Nil {
		return fmt.Errorf("aggregate de mensalidade invÃ¡lido")
	}
	if c.ID == uuid.Nil {
		c.ID = event.GetAggregateID()
	}
	c.Version++
	return nil
}
