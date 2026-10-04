package aggregates

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MaxLimiteFaltasPorPeriodo é o maior limite de faltas por período aceito.
const MaxLimiteFaltasPorPeriodo = 500

// ConfiguracaoFaltasAggregateID devolve o ID determinístico do agregado
// ConfiguracaoFaltas de uma academia: existe no máximo UM por academia e cada
// nova definição é um novo evento sobre o MESMO agregado (nunca um agregado novo).
func ConfiguracaoFaltasAggregateID(codigoAcademia string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("spuri.faltas.configuracao."+strings.TrimSpace(codigoAcademia)))
}

// ConfiguracaoFaltas guarda duas regras independentes da academia:
//   - LimiteFaltasPorPeriodo: quantas faltas um estudante pode ter, por matéria e
//     período, antes de ultrapassar o limite (nil = sem limite);
//   - ReprovacaoPorFaltas: se, ao ultrapassar o limite, a avaliação final
//     automática lê a nota afetada como 0. Só pode ficar ligada se houver limite.
type ConfiguracaoFaltas struct {
	BaseAggregate
	CodigoAcademia         string
	LimiteFaltasPorPeriodo *int
	ReprovacaoPorFaltas    bool
	AtualizadoPor          uuid.UUID
	AtualizadoEm           time.Time
}

func NewConfiguracaoFaltas() *ConfiguracaoFaltas {
	return &ConfiguracaoFaltas{BaseAggregate: BaseAggregate{ID: uuid.New(), Version: 0, UncommittedEvents: []DomainEvent{}}}
}

func (c *ConfiguracaoFaltas) GetType() string { return "ConfiguracaoFaltas" }

type ConfiguracaoFaltasDefinidaEvent struct {
	BaseEvent
	CodigoAcademia         string
	LimiteFaltasPorPeriodo *int
	ReprovacaoPorFaltas    bool
	DefinidoPor            uuid.UUID
	DefinidoEm             time.Time
}

func (e *ConfiguracaoFaltasDefinidaEvent) GetPayload() interface{} { return e }
func (e *ConfiguracaoFaltasDefinidaEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

func (c *ConfiguracaoFaltas) Apply(event DomainEvent) error {
	switch event.GetEventType() {
	case "ConfiguracaoFaltasDefinida":
		data, err := json.Marshal(event.GetPayload())
		if err != nil {
			return fmt.Errorf("applyConfiguracaoFaltasDefinida: marshal: %w", err)
		}
		var ev ConfiguracaoFaltasDefinidaEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return fmt.Errorf("applyConfiguracaoFaltasDefinida: unmarshal: %w", err)
		}
		c.CodigoAcademia = ev.CodigoAcademia
		c.LimiteFaltasPorPeriodo = ev.LimiteFaltasPorPeriodo
		c.ReprovacaoPorFaltas = ev.ReprovacaoPorFaltas
		c.AtualizadoPor = ev.DefinidoPor
		c.AtualizadoEm = ev.DefinidoEm
		return nil
	default:
		return fmt.Errorf("tipo de evento desconhecido para ConfiguracaoFaltas: %s", event.GetEventType())
	}
}

// Definir substitui a configuração de faltas da academia. O chamador deve ter
// deixado c.ID igual a ConfiguracaoFaltasAggregateID(codigoAcademia) (via SetID
// quando o agregado é novo, ou via repository.Load quando já existe).
func (c *ConfiguracaoFaltas) Definir(codigoAcademia string, limite *int, reprovacaoPorFaltas bool, definidoPor uuid.UUID) error {
	codigoAcademia = strings.TrimSpace(codigoAcademia)
	if codigoAcademia == "" {
		return fmt.Errorf("codigo_academia é obrigatório")
	}
	if c.ID != ConfiguracaoFaltasAggregateID(codigoAcademia) {
		return fmt.Errorf("configuração de faltas deve usar o ID determinístico da academia")
	}
	if limite != nil && (*limite < 1 || *limite > MaxLimiteFaltasPorPeriodo) {
		return fmt.Errorf("limite_faltas_por_periodo deve estar entre 1 e %d", MaxLimiteFaltasPorPeriodo)
	}
	if reprovacaoPorFaltas && limite == nil {
		return fmt.Errorf("reprovacao_por_faltas exige limite_faltas_por_periodo definido")
	}
	var limiteCopia *int
	if limite != nil {
		v := *limite
		limiteCopia = &v
	}
	event := &ConfiguracaoFaltasDefinidaEvent{
		BaseEvent:              BaseEvent{EventType: "ConfiguracaoFaltasDefinida", AggregateID: c.ID},
		CodigoAcademia:         codigoAcademia,
		LimiteFaltasPorPeriodo: limiteCopia,
		ReprovacaoPorFaltas:    reprovacaoPorFaltas,
		DefinidoPor:            definidoPor,
		DefinidoEm:             time.Now(),
	}
	c.RaiseEvent(event)
	return c.Apply(event)
}
