package aggregates

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"spuri/internal/utils"

	"github.com/google/uuid"
)

// Plano de Atualizações, Parte 4 — três novos tipos de login:
//
//	secretario  — trabalha PARA uma academia, com permissões escolhidas por ela;
//	professor   — trabalha PARA uma academia, com vínculos turma+matéria;
//	encarregado — não pertence a uma academia; tem educandos (estudantes do nível escolar).
//
// Os três são "contas de pessoa" com o mesmo ciclo de vida (criar, dados,
// senha, e-mail, 2FA, ativar/desativar/eliminar). Este arquivo guarda esse
// núcleo; secretario.go, professor.go e encarregado.go acrescentam o que é
// próprio de cada um. Os eventos comuns são genéricos (ContaPessoa*) e a
// projeção distingue o tipo por aggregate_type.

const (
	TipoContaSecretario  = "secretario"
	TipoContaProfessor   = "professor"
	TipoContaEncarregado = "encarregado"

	AggregateSecretario  = "Secretario"
	AggregateProfessor   = "Professor"
	AggregateEncarregado = "Encarregado"

	StatusContaAtivo    = "ativo"
	StatusContaInativo  = "inativo"
	StatusContaDeletado = "deletado"
)

// ContaPessoa é o estado comum às três contas.
type ContaPessoa struct {
	BaseAggregate

	Tipo             string
	Codigo           string
	Nome             string
	Genero           string
	DataNascimento   time.Time
	Email            *string
	Telefone         string
	SenhaHash        string
	Status           string
	EmailVerificado  bool
	DoisFatoresAtivo bool
	// AcademiaID/CodigoAcademia só existem para secretário e professor.
	AcademiaID     *uuid.UUID
	CodigoAcademia string
	CriadoPor      *uuid.UUID
	CreatedAt      time.Time
}

func novaBase() BaseAggregate {
	return BaseAggregate{ID: uuid.New(), Version: 0, UncommittedEvents: []DomainEvent{}}
}

// DadosContaPessoa são os dados de criação comuns.
type DadosContaPessoa struct {
	Codigo         string
	Nome           string
	Genero         string
	DataNascimento time.Time
	Email          *string
	Telefone       string
	SenhaHash      string
}

// validarTelefoneMovel exige o móvel angolano de 9 dígitos iniciado por 9, já
// normalizado: é o telefone que recebe as credenciais por SMS.
func validarTelefoneMovel(t string) error {
	n, err := utils.NormalizarTelefoneNacional(t)
	if err != nil {
		return fmt.Errorf("telefone inválido: %w", err)
	}
	if n != t {
		return fmt.Errorf("telefone deve estar no formato nacional de 9 dígitos (ex.: 923456789)")
	}
	return nil
}

// ValidarDadosContaPessoa aplica as regras de campo comuns às três contas.
func ValidarDadosContaPessoa(d DadosContaPessoa) error {
	if strings.TrimSpace(d.Codigo) == "" {
		return fmt.Errorf("código da conta é obrigatório")
	}
	if err := utils.ValidateNomeEstudante(d.Nome); err != nil {
		return err
	}
	if g := strings.ToLower(strings.TrimSpace(d.Genero)); g != "masculino" && g != "feminino" {
		return fmt.Errorf("gênero deve ser 'masculino' ou 'feminino'")
	}
	if d.DataNascimento.IsZero() {
		return fmt.Errorf("data de nascimento é obrigatória")
	}
	agora := time.Now()
	if d.DataNascimento.After(agora) {
		return fmt.Errorf("data de nascimento não pode estar no futuro")
	}
	if d.DataNascimento.AddDate(18, 0, 0).After(agora) {
		return fmt.Errorf("a pessoa deve ter pelo menos 18 anos")
	}
	if err := validarTelefoneMovel(d.Telefone); err != nil {
		return err
	}
	if d.Email != nil {
		if err := utils.ValidateEmail(*d.Email); err != nil {
			return err
		}
	}
	if len(d.SenhaHash) < 60 {
		return fmt.Errorf("senhaHash inválido: esperado hash bcrypt (mínimo 60 caracteres)")
	}
	return nil
}

// ── Eventos comuns ──────────────────────────────────────────────────────────

type ContaPessoaCriadaEvent struct {
	BaseEvent
	Tipo           string
	Codigo         string
	Nome           string
	Genero         string
	DataNascimento time.Time
	Email          *string
	Telefone       string
	SenhaHash      string
	AcademiaID     *uuid.UUID
	CodigoAcademia string
	// Permissoes só é preenchido para secretário (já normalizado).
	Permissoes []string
	CriadoPor  *uuid.UUID
	CreatedAt  time.Time
}

func (e *ContaPessoaCriadaEvent) GetPayload() interface{} { return e }
func (e *ContaPessoaCriadaEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

type ContaPessoaDadosAtualizadosEvent struct {
	BaseEvent
	Nome             *string
	Genero           *string
	DataNascimento   *time.Time
	Email            *string
	EmailAlterado    bool
	Telefone         *string
	TelefoneAlterado bool
	AtualizadoEm     time.Time
}

func (e *ContaPessoaDadosAtualizadosEvent) GetPayload() interface{} { return e }
func (e *ContaPessoaDadosAtualizadosEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

type ContaPessoaSenhaAlteradaEvent struct {
	BaseEvent
	NovaSenhaHash string
	Motivo        string
	ChangedAt     time.Time
}

func (e *ContaPessoaSenhaAlteradaEvent) GetPayload() interface{} { return e }
func (e *ContaPessoaSenhaAlteradaEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

type ContaPessoaAtivadaEvent struct {
	BaseEvent
	AtivadoPor *uuid.UUID
	AtivadoEm  time.Time
}

func (e *ContaPessoaAtivadaEvent) GetPayload() interface{} { return e }
func (e *ContaPessoaAtivadaEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

type ContaPessoaDesativadaEvent struct {
	BaseEvent
	Motivo        string
	DesativadoPor *uuid.UUID
	DesativadoEm  time.Time
}

func (e *ContaPessoaDesativadaEvent) GetPayload() interface{} { return e }
func (e *ContaPessoaDesativadaEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

type ContaPessoaDeletadaEvent struct {
	BaseEvent
	Motivo      string
	DeletadoPor *uuid.UUID
	DeletedAt   time.Time
}

func (e *ContaPessoaDeletadaEvent) GetPayload() interface{} { return e }
func (e *ContaPessoaDeletadaEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

// ── Apply dos eventos comuns ────────────────────────────────────────────────

func decodificar(event DomainEvent, destino interface{}) error {
	data, err := json.Marshal(event.GetPayload())
	if err != nil {
		return fmt.Errorf("%s: marshal: %w", event.GetEventType(), err)
	}
	if err := json.Unmarshal(data, destino); err != nil {
		return fmt.Errorf("%s: unmarshal: %w", event.GetEventType(), err)
	}
	return nil
}

// aplicarComum trata os eventos partilhados. Devolve tratado=false se o evento
// não é comum (o agregado concreto trata os seus próprios).
func (c *ContaPessoa) aplicarComum(event DomainEvent) (bool, error) {
	switch event.GetEventType() {
	case "ContaPessoaCriada":
		var ev ContaPessoaCriadaEvent
		if err := decodificar(event, &ev); err != nil {
			return true, err
		}
		c.Tipo = ev.Tipo
		c.Codigo = ev.Codigo
		c.Nome = ev.Nome
		c.Genero = ev.Genero
		c.DataNascimento = ev.DataNascimento
		c.Email = ev.Email
		c.Telefone = ev.Telefone
		c.SenhaHash = ev.SenhaHash
		c.AcademiaID = ev.AcademiaID
		c.CodigoAcademia = ev.CodigoAcademia
		c.CriadoPor = ev.CriadoPor
		c.CreatedAt = ev.CreatedAt
		c.Status = StatusContaAtivo
		return true, nil
	case "ContaPessoaDadosAtualizados":
		var ev ContaPessoaDadosAtualizadosEvent
		if err := decodificar(event, &ev); err != nil {
			return true, err
		}
		if ev.Nome != nil {
			c.Nome = *ev.Nome
		}
		if ev.Genero != nil {
			c.Genero = *ev.Genero
		}
		if ev.DataNascimento != nil {
			c.DataNascimento = *ev.DataNascimento
		}
		if ev.Email != nil {
			c.Email = ev.Email
			if ev.EmailAlterado {
				c.EmailVerificado = false
				// Sem e-mail verificado não há 2FA por e-mail.
				c.DoisFatoresAtivo = false
			}
		}
		if ev.Telefone != nil {
			c.Telefone = *ev.Telefone
		}
		return true, nil
	case "ContaPessoaSenhaAlterada":
		var ev ContaPessoaSenhaAlteradaEvent
		if err := decodificar(event, &ev); err != nil {
			return true, err
		}
		if ev.NovaSenhaHash == "" {
			return true, fmt.Errorf("ContaPessoaSenhaAlterada: NovaSenhaHash vazio no payload")
		}
		c.SenhaHash = ev.NovaSenhaHash
		return true, nil
	case "ContaPessoaAtivada":
		c.Status = StatusContaAtivo
		return true, nil
	case "ContaPessoaDesativada":
		c.Status = StatusContaInativo
		return true, nil
	case "ContaPessoaDeletada":
		c.Status = StatusContaDeletado
		return true, nil
	case "EmailVerificado":
		c.EmailVerificado = true
		return true, nil
	case "DoisFatoresAtivados":
		c.DoisFatoresAtivo = true
		return true, nil
	case "DoisFatoresDesativados":
		c.DoisFatoresAtivo = false
		return true, nil
	case "SenhaTemporariaDefinida":
		// Estado vive na projeção (deve_alterar_senha); o agregado só aceita o evento.
		return true, nil
	}
	return false, nil
}

func (c *ContaPessoa) levantar(ev DomainEvent) error {
	c.RaiseEvent(ev)
	_, err := c.aplicarComum(ev)
	return err
}

// ── Comandos comuns ─────────────────────────────────────────────────────────

func (c *ContaPessoa) criarComum(tipo string, d DadosContaPessoa, academiaID *uuid.UUID, codigoAcademia string, permissoes []string, criadoPor *uuid.UUID) error {
	if c.Status != "" {
		return fmt.Errorf("conta já foi criada")
	}
	if err := ValidarDadosContaPessoa(d); err != nil {
		return err
	}
	if tipo != TipoContaEncarregado && (academiaID == nil || strings.TrimSpace(codigoAcademia) == "") {
		return fmt.Errorf("%s precisa de uma academia", tipo)
	}
	return c.levantar(&ContaPessoaCriadaEvent{
		BaseEvent:      BaseEvent{EventType: "ContaPessoaCriada", AggregateID: c.ID},
		Tipo:           tipo,
		Codigo:         d.Codigo,
		Nome:           strings.TrimSpace(d.Nome),
		Genero:         strings.ToLower(strings.TrimSpace(d.Genero)),
		DataNascimento: d.DataNascimento,
		Email:          d.Email,
		Telefone:       d.Telefone,
		SenhaHash:      d.SenhaHash,
		AcademiaID:     academiaID,
		CodigoAcademia: codigoAcademia,
		Permissoes:     permissoes,
		CriadoPor:      criadoPor,
		CreatedAt:      time.Now(),
	})
}

// DefinirSenhaTemporaria registra que a conta nasceu com senha temporária.
func (c *ContaPessoa) DefinirSenhaTemporaria() error {
	return c.levantar(&SenhaTemporariaDefinidaEvent{
		BaseEvent:  BaseEvent{EventType: "SenhaTemporariaDefinida", AggregateID: c.ID},
		DefinidaEm: time.Now(),
	})
}

// AlterarSenhaComMotivo grava a troca de senha (mesmos motivos das demais contas).
func (c *ContaPessoa) AlterarSenhaComMotivo(novaSenhaHash, motivo string) error {
	if c.Status == StatusContaDeletado {
		return fmt.Errorf("conta indisponível")
	}
	if novaSenhaHash == "" {
		return fmt.Errorf("hash da nova senha não pode ser vazio")
	}
	return c.levantar(&ContaPessoaSenhaAlteradaEvent{
		BaseEvent:     BaseEvent{EventType: "ContaPessoaSenhaAlterada", AggregateID: c.ID},
		NovaSenhaHash: novaSenhaHash,
		Motivo:        motivo,
		ChangedAt:     time.Now(),
	})
}

// VerificarEmail marca o e-mail como verificado.
func (c *ContaPessoa) VerificarEmail() error {
	if c.EmailVerificado {
		return fmt.Errorf("email já verificado")
	}
	return c.levantar(&EmailVerificadoEvent{
		BaseEvent:  BaseEvent{EventType: "EmailVerificado", AggregateID: c.ID},
		VerifiedAt: time.Now(),
	})
}

// AtivarDoisFatores / DesativarDoisFatores: mesma regra das outras contas.
func (c *ContaPessoa) AtivarDoisFatores() error {
	if err := validarAtivacaoDoisFatores(c.DoisFatoresAtivo, c.Email != nil && *c.Email != "", c.EmailVerificado); err != nil {
		return err
	}
	return c.levantar(&DoisFatoresAtivadosEvent{
		BaseEvent: BaseEvent{EventType: "DoisFatoresAtivados", AggregateID: c.ID},
		Metodo:    MetodoDoisFatoresEmail,
		AtivadoEm: time.Now(),
	})
}

func (c *ContaPessoa) DesativarDoisFatores() error {
	if err := validarDesativacaoDoisFatores(c.DoisFatoresAtivo); err != nil {
		return err
	}
	return c.levantar(&DoisFatoresDesativadosEvent{
		BaseEvent:    BaseEvent{EventType: "DoisFatoresDesativados", AggregateID: c.ID},
		Metodo:       MetodoDoisFatoresEmail,
		DesativadoEm: time.Now(),
	})
}

// AtualizarDados altera os dados pessoais. Telefone e e-mail seguem as regras
// já existentes: mudar o e-mail zera a verificação.
func (c *ContaPessoa) AtualizarDados(nome, genero *string, dataNascimento *time.Time, email, telefone *string) error {
	if c.Status == StatusContaDeletado {
		return fmt.Errorf("conta indisponível")
	}
	ev := &ContaPessoaDadosAtualizadosEvent{
		BaseEvent:    BaseEvent{EventType: "ContaPessoaDadosAtualizados", AggregateID: c.ID},
		AtualizadoEm: time.Now(),
	}
	mudou := false
	if nome != nil {
		if err := utils.ValidateNomeEstudante(*nome); err != nil {
			return err
		}
		n := strings.TrimSpace(*nome)
		ev.Nome, mudou = &n, true
	}
	if genero != nil {
		g := strings.ToLower(strings.TrimSpace(*genero))
		if g != "masculino" && g != "feminino" {
			return fmt.Errorf("gênero deve ser 'masculino' ou 'feminino'")
		}
		ev.Genero, mudou = &g, true
	}
	if dataNascimento != nil {
		probe := DadosContaPessoa{Codigo: "x", Nome: "Probe Nome", Genero: "masculino", DataNascimento: *dataNascimento, Telefone: c.Telefone, SenhaHash: strings.Repeat("x", 60)}
		if err := ValidarDadosContaPessoa(probe); err != nil {
			return err
		}
		ev.DataNascimento, mudou = dataNascimento, true
	}
	if email != nil {
		if err := utils.ValidateEmail(*email); err != nil {
			return err
		}
		if c.Email == nil || *c.Email != *email {
			e := strings.TrimSpace(*email)
			ev.Email, ev.EmailAlterado, mudou = &e, true, true
		}
	}
	if telefone != nil {
		if err := validarTelefoneMovel(*telefone); err != nil {
			return err
		}
		if c.Telefone != *telefone {
			t := *telefone
			ev.Telefone, ev.TelefoneAlterado, mudou = &t, true, true
		}
	}
	if !mudou {
		return nil
	}
	return c.levantar(ev)
}

// Ativar reativa uma conta desativada.
func (c *ContaPessoa) Ativar(por *uuid.UUID) error {
	switch c.Status {
	case StatusContaAtivo:
		return fmt.Errorf("conta já está ativa")
	case StatusContaDeletado:
		return fmt.Errorf("conta eliminada não pode ser reativada")
	}
	return c.levantar(&ContaPessoaAtivadaEvent{
		BaseEvent:  BaseEvent{EventType: "ContaPessoaAtivada", AggregateID: c.ID},
		AtivadoPor: por, AtivadoEm: time.Now(),
	})
}

// Desativar bloqueia o acesso sem apagar a conta.
func (c *ContaPessoa) Desativar(por *uuid.UUID, motivo string) error {
	switch c.Status {
	case StatusContaInativo:
		return fmt.Errorf("conta já está inativa")
	case StatusContaDeletado:
		return fmt.Errorf("conta eliminada")
	}
	return c.levantar(&ContaPessoaDesativadaEvent{
		BaseEvent: BaseEvent{EventType: "ContaPessoaDesativada", AggregateID: c.ID},
		Motivo:    strings.TrimSpace(motivo), DesativadoPor: por, DesativadoEm: time.Now(),
	})
}

// Deletar elimina logicamente a conta (o ledger mantém o histórico).
func (c *ContaPessoa) Deletar(por *uuid.UUID, motivo string) error {
	if c.Status == StatusContaDeletado {
		return fmt.Errorf("conta já foi eliminada")
	}
	return c.levantar(&ContaPessoaDeletadaEvent{
		BaseEvent: BaseEvent{EventType: "ContaPessoaDeletada", AggregateID: c.ID},
		Motivo:    strings.TrimSpace(motivo), DeletadoPor: por, DeletedAt: time.Now(),
	})
}
