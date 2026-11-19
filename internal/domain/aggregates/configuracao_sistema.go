package aggregates

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ConfiguracaoSistemaAggregateID devolve o ID determinístico do agregado de uma
// chave de configuração do sistema: existe no máximo UM por chave e cada nova
// definição é um novo evento sobre o MESMO agregado.
func ConfiguracaoSistemaAggregateID(chave string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("spuri.sistema.configuracao."+strings.TrimSpace(chave)))
}

// ConfiguracaoSistema é o valor, gravado no banco, de uma configuração
// operacional que antes só existia como variável de ambiente (Parte 3 do
// Plano de Atualizações). Para segredo, o valor NUNCA entra no agregado nem no
// evento: ele vive cifrado no cofre (configuracao_sistema_segredos) e o evento
// só registra que o segredo foi trocado, por quem e quando.
type ConfiguracaoSistema struct {
	BaseAggregate
	Chave         string
	Tipo          string
	Segredo       bool
	Definida      bool
	Valor         string // vazio quando Segredo
	AtualizadoPor uuid.UUID
	AtualizadoEm  time.Time
}

func NewConfiguracaoSistema() *ConfiguracaoSistema {
	return &ConfiguracaoSistema{BaseAggregate: BaseAggregate{ID: uuid.New(), Version: 0, UncommittedEvents: []DomainEvent{}}}
}

func (c *ConfiguracaoSistema) GetType() string { return "ConfiguracaoSistema" }

// ConfiguracaoSistemaDefinidaEvent: Valor fica vazio para segredo.
// ValorAnterior/TinhaValorAnterior servem à auditoria de configurações que
// não são segredo ("de X para Y").
type ConfiguracaoSistemaDefinidaEvent struct {
	BaseEvent
	Chave              string
	Tipo               string
	Segredo            bool
	Valor              string
	ValorAnterior      string
	TinhaValorAnterior bool
	DefinidoPor        uuid.UUID
	DefinidoEm         time.Time
}

func (e *ConfiguracaoSistemaDefinidaEvent) GetPayload() interface{} { return e }
func (e *ConfiguracaoSistemaDefinidaEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

// ConfiguracaoSistemaRemovidaEvent: a chave volta a ser lida só do ambiente.
type ConfiguracaoSistemaRemovidaEvent struct {
	BaseEvent
	Chave         string
	Tipo          string
	Segredo       bool
	ValorAnterior string
	RemovidoPor   uuid.UUID
	RemovidoEm    time.Time
}

func (e *ConfiguracaoSistemaRemovidaEvent) GetPayload() interface{} { return e }
func (e *ConfiguracaoSistemaRemovidaEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

func (c *ConfiguracaoSistema) Apply(event DomainEvent) error {
	data, err := json.Marshal(event.GetPayload())
	if err != nil {
		return fmt.Errorf("applyConfiguracaoSistema: marshal: %w", err)
	}
	switch event.GetEventType() {
	case "ConfiguracaoSistemaDefinida":
		var ev ConfiguracaoSistemaDefinidaEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return fmt.Errorf("applyConfiguracaoSistemaDefinida: unmarshal: %w", err)
		}
		c.Chave, c.Tipo, c.Segredo, c.Definida = ev.Chave, ev.Tipo, ev.Segredo, true
		c.Valor = ev.Valor
		c.AtualizadoPor, c.AtualizadoEm = ev.DefinidoPor, ev.DefinidoEm
		return nil
	case "ConfiguracaoSistemaRemovida":
		var ev ConfiguracaoSistemaRemovidaEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return fmt.Errorf("applyConfiguracaoSistemaRemovida: unmarshal: %w", err)
		}
		c.Chave, c.Tipo, c.Segredo, c.Definida = ev.Chave, ev.Tipo, ev.Segredo, false
		c.Valor = ""
		c.AtualizadoPor, c.AtualizadoEm = ev.RemovidoPor, ev.RemovidoEm
		return nil
	default:
		return fmt.Errorf("tipo de evento desconhecido para ConfiguracaoSistema: %s", event.GetEventType())
	}
}

// Definir grava o valor da chave. O chamador já validou o valor contra a lista
// aprovada e deixou c.ID igual a ConfiguracaoSistemaAggregateID(chave).
// Para segredo, valor TEM de vir vazio: o texto não pode entrar no ledger.
func (c *ConfiguracaoSistema) Definir(chave, tipo string, segredo bool, valor string, definidoPor uuid.UUID) error {
	chave = strings.TrimSpace(chave)
	if chave == "" {
		return fmt.Errorf("chave é obrigatória")
	}
	if c.ID != ConfiguracaoSistemaAggregateID(chave) {
		return fmt.Errorf("configuração do sistema deve usar o ID determinístico da chave")
	}
	if definidoPor == uuid.Nil {
		return fmt.Errorf("definido_por é obrigatório")
	}
	if segredo && valor != "" {
		return fmt.Errorf("o valor de um segredo não pode entrar no evento")
	}
	if !segredo && valor == "" {
		return fmt.Errorf("valor é obrigatório")
	}
	ev := &ConfiguracaoSistemaDefinidaEvent{
		BaseEvent:   BaseEvent{EventType: "ConfiguracaoSistemaDefinida", AggregateID: c.ID},
		Chave:       chave,
		Tipo:        tipo,
		Segredo:     segredo,
		Valor:       valor,
		DefinidoPor: definidoPor,
		DefinidoEm:  time.Now(),
	}
	if !segredo && c.Definida {
		ev.ValorAnterior, ev.TinhaValorAnterior = c.Valor, true
	}
	c.RaiseEvent(ev)
	return c.Apply(ev)
}

// Remover tira o valor do banco; a chave volta a ser lida do ambiente.
func (c *ConfiguracaoSistema) Remover(removidoPor uuid.UUID) error {
	if !c.Definida {
		return fmt.Errorf("não há valor gravado no banco para esta configuração")
	}
	if removidoPor == uuid.Nil {
		return fmt.Errorf("removido_por é obrigatório")
	}
	ev := &ConfiguracaoSistemaRemovidaEvent{
		BaseEvent:   BaseEvent{EventType: "ConfiguracaoSistemaRemovida", AggregateID: c.ID},
		Chave:       c.Chave,
		Tipo:        c.Tipo,
		Segredo:     c.Segredo,
		RemovidoPor: removidoPor,
		RemovidoEm:  time.Now(),
	}
	if !c.Segredo {
		ev.ValorAnterior = c.Valor
	}
	c.RaiseEvent(ev)
	return c.Apply(ev)
}
