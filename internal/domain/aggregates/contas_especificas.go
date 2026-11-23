package aggregates

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"spuri/internal/permissoes"

	"github.com/google/uuid"
)

// ============================================================================
// Secretário
// ============================================================================

// Secretario trabalha para UMA academia, com as permissões que ela escolheu.
type Secretario struct {
	ContaPessoa
	// Permissoes é o conjunto NORMALIZADO (com as implicadas) — ver permissoes.Normalizar.
	Permissoes []string
}

func NewSecretario() *Secretario {
	return &Secretario{ContaPessoa: ContaPessoa{BaseAggregate: novaBase(), Tipo: TipoContaSecretario}}
}

func (s *Secretario) GetType() string { return AggregateSecretario }

type SecretarioPermissoesDefinidasEvent struct {
	BaseEvent
	Permissoes  []string
	DefinidoPor *uuid.UUID
	DefinidoEm  time.Time
}

func (e *SecretarioPermissoesDefinidasEvent) GetPayload() interface{} { return e }
func (e *SecretarioPermissoesDefinidasEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

func (s *Secretario) Apply(event DomainEvent) error {
	tratado, err := s.aplicarComum(event)
	if tratado || err != nil {
		if event.GetEventType() == "ContaPessoaCriada" && err == nil {
			var ev ContaPessoaCriadaEvent
			if err := decodificar(event, &ev); err != nil {
				return err
			}
			s.Permissoes = ev.Permissoes
		}
		return err
	}
	switch event.GetEventType() {
	case "SecretarioPermissoesDefinidas":
		var ev SecretarioPermissoesDefinidasEvent
		if err := decodificar(event, &ev); err != nil {
			return err
		}
		s.Permissoes = ev.Permissoes
		return nil
	}
	return fmt.Errorf("tipo de evento desconhecido: %s", event.GetEventType())
}

// Criar valida e levanta o evento de criação. Todas as permissões passam por
// permissoes.Normalizar: chave desconhecida (por exemplo, lançar notas) é erro.
func (s *Secretario) Criar(d DadosContaPessoa, academiaID uuid.UUID, codigoAcademia string, perms []string, criadoPor *uuid.UUID) error {
	norm, err := permissoes.Normalizar(perms)
	if err != nil {
		return err
	}
	if len(norm) == 0 {
		return fmt.Errorf("selecione pelo menos uma permissão para o secretário")
	}
	if err := s.criarComum(TipoContaSecretario, d, &academiaID, codigoAcademia, norm, criadoPor); err != nil {
		return err
	}
	s.Permissoes = norm
	return nil
}

// DefinirPermissoes substitui o conjunto de permissões.
func (s *Secretario) DefinirPermissoes(perms []string, por *uuid.UUID) error {
	if s.Status == StatusContaDeletado {
		return fmt.Errorf("conta eliminada")
	}
	norm, err := permissoes.Normalizar(perms)
	if err != nil {
		return err
	}
	if len(norm) == 0 {
		return fmt.Errorf("selecione pelo menos uma permissão para o secretário")
	}
	ev := &SecretarioPermissoesDefinidasEvent{
		BaseEvent:  BaseEvent{EventType: "SecretarioPermissoesDefinidas", AggregateID: s.ID},
		Permissoes: norm, DefinidoPor: por, DefinidoEm: time.Now(),
	}
	s.RaiseEvent(ev)
	return s.Apply(ev)
}

// ============================================================================
// Professor
// ============================================================================

// VinculoProfessor liga o professor a uma turma e a uma matéria dessa turma.
type VinculoProfessor struct {
	VinculoID   uuid.UUID
	CodigoTurma string
	MateriaID   uuid.UUID
}

type Professor struct {
	ContaPessoa
	Vinculos map[uuid.UUID]VinculoProfessor
}

func NewProfessor() *Professor {
	return &Professor{
		ContaPessoa: ContaPessoa{BaseAggregate: novaBase(), Tipo: TipoContaProfessor},
		Vinculos:    map[uuid.UUID]VinculoProfessor{},
	}
}

func (p *Professor) GetType() string { return AggregateProfessor }

type ProfessorVinculoAtribuidoEvent struct {
	BaseEvent
	VinculoID    uuid.UUID
	CodigoTurma  string
	MateriaID    uuid.UUID
	AtribuidoPor *uuid.UUID
	AtribuidoEm  time.Time
}

func (e *ProfessorVinculoAtribuidoEvent) GetPayload() interface{} { return e }
func (e *ProfessorVinculoAtribuidoEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

type ProfessorVinculoRemovidoEvent struct {
	BaseEvent
	VinculoID   uuid.UUID
	Motivo      string
	RemovidoPor *uuid.UUID
	RemovidoEm  time.Time
}

func (e *ProfessorVinculoRemovidoEvent) GetPayload() interface{} { return e }
func (e *ProfessorVinculoRemovidoEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

func (p *Professor) Apply(event DomainEvent) error {
	if tratado, err := p.aplicarComum(event); tratado || err != nil {
		return err
	}
	switch event.GetEventType() {
	case "ProfessorVinculoAtribuido":
		var ev ProfessorVinculoAtribuidoEvent
		if err := decodificar(event, &ev); err != nil {
			return err
		}
		p.Vinculos[ev.VinculoID] = VinculoProfessor{VinculoID: ev.VinculoID, CodigoTurma: ev.CodigoTurma, MateriaID: ev.MateriaID}
		return nil
	case "ProfessorVinculoRemovido":
		var ev ProfessorVinculoRemovidoEvent
		if err := decodificar(event, &ev); err != nil {
			return err
		}
		delete(p.Vinculos, ev.VinculoID)
		return nil
	}
	return fmt.Errorf("tipo de evento desconhecido: %s", event.GetEventType())
}

func (p *Professor) Criar(d DadosContaPessoa, academiaID uuid.UUID, codigoAcademia string, criadoPor *uuid.UUID) error {
	return p.criarComum(TipoContaProfessor, d, &academiaID, codigoAcademia, nil, criadoPor)
}

// TemVinculo diz se o par turma+matéria já está atribuído a este professor.
func (p *Professor) TemVinculo(codigoTurma string, materiaID uuid.UUID) bool {
	for _, v := range p.Vinculos {
		if v.CodigoTurma == codigoTurma && v.MateriaID == materiaID {
			return true
		}
	}
	return false
}

// AtribuirVinculo liga o professor ao par turma+matéria. A existência da turma
// e da matéria, e a regra "um professor por par", são verificadas pelo handler
// (precisam de outras projeções); aqui só a consistência do próprio agregado.
func (p *Professor) AtribuirVinculo(codigoTurma string, materiaID uuid.UUID, por *uuid.UUID) (uuid.UUID, error) {
	if p.Status != StatusContaAtivo {
		return uuid.Nil, fmt.Errorf("só é possível atribuir vínculos a um professor ativo")
	}
	codigoTurma = strings.TrimSpace(codigoTurma)
	if codigoTurma == "" || materiaID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("turma e matéria são obrigatórias")
	}
	if p.TemVinculo(codigoTurma, materiaID) {
		return uuid.Nil, fmt.Errorf("o professor já está vinculado a esta turma e matéria")
	}
	id := uuid.New()
	ev := &ProfessorVinculoAtribuidoEvent{
		BaseEvent: BaseEvent{EventType: "ProfessorVinculoAtribuido", AggregateID: p.ID},
		VinculoID: id, CodigoTurma: codigoTurma, MateriaID: materiaID, AtribuidoPor: por, AtribuidoEm: time.Now(),
	}
	p.RaiseEvent(ev)
	return id, p.Apply(ev)
}

// RemoverVinculo desfaz um vínculo do professor.
func (p *Professor) RemoverVinculo(vinculoID uuid.UUID, por *uuid.UUID, motivo string) error {
	if _, ok := p.Vinculos[vinculoID]; !ok {
		return fmt.Errorf("vínculo não encontrado para este professor")
	}
	ev := &ProfessorVinculoRemovidoEvent{
		BaseEvent: BaseEvent{EventType: "ProfessorVinculoRemovido", AggregateID: p.ID},
		VinculoID: vinculoID, Motivo: strings.TrimSpace(motivo), RemovidoPor: por, RemovidoEm: time.Now(),
	}
	p.RaiseEvent(ev)
	return p.Apply(ev)
}

// ============================================================================
// Encarregado
// ============================================================================

// Relações aceitas entre encarregado e educando. A relação é opcional; "outro"
// exige o texto (Plano, Parte 4).
const (
	RelacaoPai   = "pai"
	RelacaoMae   = "mae"
	RelacaoIrmao = "irmao"
	RelacaoIrma  = "irma"
	RelacaoOutro = "outro"
)

// ValidarRelacao normaliza a relação opcional. Devolve (relacao, relacaoOutro).
func ValidarRelacao(relacao, relacaoOutro *string) (*string, *string, error) {
	if relacao == nil || strings.TrimSpace(*relacao) == "" {
		if relacaoOutro != nil && strings.TrimSpace(*relacaoOutro) != "" {
			return nil, nil, fmt.Errorf("relacao_outro só pode ser informada quando a relação é 'outro'")
		}
		return nil, nil, nil
	}
	r := strings.ToLower(strings.TrimSpace(*relacao))
	switch r {
	case RelacaoPai, RelacaoMae, RelacaoIrmao, RelacaoIrma:
		if relacaoOutro != nil && strings.TrimSpace(*relacaoOutro) != "" {
			return nil, nil, fmt.Errorf("relacao_outro só pode ser informada quando a relação é 'outro'")
		}
		return &r, nil, nil
	case RelacaoOutro:
		if relacaoOutro == nil || strings.TrimSpace(*relacaoOutro) == "" {
			return nil, nil, fmt.Errorf("quando a relação é 'outro', descreva a relação em relacao_outro")
		}
		txt := strings.TrimSpace(*relacaoOutro)
		if len([]rune(txt)) > 60 {
			return nil, nil, fmt.Errorf("relacao_outro deve ter no máximo 60 caracteres")
		}
		return &r, &txt, nil
	}
	return nil, nil, fmt.Errorf("relação inválida: use pai, mae, irmao, irma ou outro")
}

// Educando é o vínculo ativo entre o encarregado e um estudante.
type Educando struct {
	EstudanteID     uuid.UUID
	CodigoEstudante string
	Relacao         *string
	RelacaoOutro    *string
}

type Encarregado struct {
	ContaPessoa
	Educandos map[uuid.UUID]Educando
}

func NewEncarregado() *Encarregado {
	return &Encarregado{
		ContaPessoa: ContaPessoa{BaseAggregate: novaBase(), Tipo: TipoContaEncarregado},
		Educandos:   map[uuid.UUID]Educando{},
	}
}

func (e *Encarregado) GetType() string { return AggregateEncarregado }

const (
	MotivoDesvinculoEstudante     = "estudante"
	MotivoDesvinculoTrocaAcademia = "troca_pela_academia"
	MotivoDesvinculoContaExcluida = "conta_do_estudante_excluida"
)

type EncarregadoEducandoVinculadoEvent struct {
	BaseEvent
	EstudanteID     uuid.UUID
	CodigoEstudante string
	Relacao         *string
	RelacaoOutro    *string
	VinculadoPor    *uuid.UUID
	VinculadoEm     time.Time
}

func (ev *EncarregadoEducandoVinculadoEvent) GetPayload() interface{} { return ev }
func (ev *EncarregadoEducandoVinculadoEvent) ToJSON() ([]byte, error) { return json.Marshal(ev) }

type EncarregadoEducandoDesvinculadoEvent struct {
	BaseEvent
	EstudanteID     uuid.UUID
	CodigoEstudante string
	Motivo          string
	DesvinculadoPor *uuid.UUID
	DesvinculadoEm  time.Time
}

func (ev *EncarregadoEducandoDesvinculadoEvent) GetPayload() interface{} { return ev }
func (ev *EncarregadoEducandoDesvinculadoEvent) ToJSON() ([]byte, error) { return json.Marshal(ev) }

type EncarregadoRelacaoAtualizadaEvent struct {
	BaseEvent
	EstudanteID  uuid.UUID
	Relacao      *string
	RelacaoOutro *string
	AtualizadoEm time.Time
}

func (ev *EncarregadoRelacaoAtualizadaEvent) GetPayload() interface{} { return ev }
func (ev *EncarregadoRelacaoAtualizadaEvent) ToJSON() ([]byte, error) { return json.Marshal(ev) }

func (e *Encarregado) Apply(event DomainEvent) error {
	if tratado, err := e.aplicarComum(event); tratado || err != nil {
		return err
	}
	switch event.GetEventType() {
	case "EncarregadoEducandoVinculado":
		var ev EncarregadoEducandoVinculadoEvent
		if err := decodificar(event, &ev); err != nil {
			return err
		}
		e.Educandos[ev.EstudanteID] = Educando{EstudanteID: ev.EstudanteID, CodigoEstudante: ev.CodigoEstudante, Relacao: ev.Relacao, RelacaoOutro: ev.RelacaoOutro}
		return nil
	case "EncarregadoEducandoDesvinculado":
		var ev EncarregadoEducandoDesvinculadoEvent
		if err := decodificar(event, &ev); err != nil {
			return err
		}
		delete(e.Educandos, ev.EstudanteID)
		return nil
	case "EncarregadoRelacaoAtualizada":
		var ev EncarregadoRelacaoAtualizadaEvent
		if err := decodificar(event, &ev); err != nil {
			return err
		}
		if ed, ok := e.Educandos[ev.EstudanteID]; ok {
			ed.Relacao, ed.RelacaoOutro = ev.Relacao, ev.RelacaoOutro
			e.Educandos[ev.EstudanteID] = ed
		}
		return nil
	}
	return fmt.Errorf("tipo de evento desconhecido: %s", event.GetEventType())
}

// Criar cria a conta do encarregado (sem academia).
func (e *Encarregado) Criar(d DadosContaPessoa, criadoPor *uuid.UUID) error {
	return e.criarComum(TipoContaEncarregado, d, nil, "", nil, criadoPor)
}

// VincularEducando liga o encarregado a um estudante (idempotente: repetir o
// mesmo vínculo ativo é erro explícito, não evento duplicado).
func (e *Encarregado) VincularEducando(estudanteID uuid.UUID, codigoEstudante string, relacao, relacaoOutro *string, por *uuid.UUID) error {
	if e.Status != StatusContaAtivo {
		return fmt.Errorf("só é possível vincular educandos a um encarregado ativo")
	}
	if estudanteID == uuid.Nil || strings.TrimSpace(codigoEstudante) == "" {
		return fmt.Errorf("estudante é obrigatório")
	}
	if _, ok := e.Educandos[estudanteID]; ok {
		return fmt.Errorf("o estudante já é educando deste encarregado")
	}
	rel, relOutro, err := ValidarRelacao(relacao, relacaoOutro)
	if err != nil {
		return err
	}
	ev := &EncarregadoEducandoVinculadoEvent{
		BaseEvent:   BaseEvent{EventType: "EncarregadoEducandoVinculado", AggregateID: e.ID},
		EstudanteID: estudanteID, CodigoEstudante: codigoEstudante, Relacao: rel, RelacaoOutro: relOutro,
		VinculadoPor: por, VinculadoEm: time.Now(),
	}
	e.RaiseEvent(ev)
	return e.Apply(ev)
}

// DesvincularEducando encerra o vínculo ativo com um estudante.
func (e *Encarregado) DesvincularEducando(estudanteID uuid.UUID, motivo string, por *uuid.UUID) error {
	ed, ok := e.Educandos[estudanteID]
	if !ok {
		return fmt.Errorf("o estudante não é educando deste encarregado")
	}
	ev := &EncarregadoEducandoDesvinculadoEvent{
		BaseEvent:   BaseEvent{EventType: "EncarregadoEducandoDesvinculado", AggregateID: e.ID},
		EstudanteID: estudanteID, CodigoEstudante: ed.CodigoEstudante, Motivo: motivo, DesvinculadoPor: por, DesvinculadoEm: time.Now(),
	}
	e.RaiseEvent(ev)
	return e.Apply(ev)
}

// AtualizarRelacao altera a relação com um educando.
func (e *Encarregado) AtualizarRelacao(estudanteID uuid.UUID, relacao, relacaoOutro *string) error {
	if _, ok := e.Educandos[estudanteID]; !ok {
		return fmt.Errorf("o estudante não é educando deste encarregado")
	}
	rel, relOutro, err := ValidarRelacao(relacao, relacaoOutro)
	if err != nil {
		return err
	}
	ev := &EncarregadoRelacaoAtualizadaEvent{
		BaseEvent:   BaseEvent{EventType: "EncarregadoRelacaoAtualizada", AggregateID: e.ID},
		EstudanteID: estudanteID, Relacao: rel, RelacaoOutro: relOutro, AtualizadoEm: time.Now(),
	}
	e.RaiseEvent(ev)
	return e.Apply(ev)
}
