package aggregates

import (
	"encoding/json"
	"errors"
	"time"
)

// Tarefa 122 — Verificação em duas etapas (2FA).
//
// O estado "2FA ativo" é decidido por eventos no ledger (fonte de verdade),
// nunca por um campo solto: ativar/desativar fica auditável e a projeção pode
// ser reconstruída a qualquer momento. Os mesmos dois eventos são usados por
// Admin, Academia e Estudante — as projeções filtram por aggregate_type, como
// já acontece com "EmailVerificado".

// MetodoDoisFatoresEmail é o único método suportado hoje (código de 6 dígitos
// enviado por e-mail). O campo existe no evento para permitir novos métodos
// (SMS, aplicativo autenticador) sem alterar o formato do ledger.
const MetodoDoisFatoresEmail = "email"

var (
	// ErrDoisFatoresJaAtivo — ativar quando já está ativo.
	ErrDoisFatoresJaAtivo = errors.New("a verificação em duas etapas já está ativa")
	// ErrDoisFatoresNaoAtivo — desativar quando não está ativo.
	ErrDoisFatoresNaoAtivo = errors.New("a verificação em duas etapas não está ativa")
	// ErrDoisFatoresEmailNaoVerificado — ativar sem e-mail verificado.
	ErrDoisFatoresEmailNaoVerificado = errors.New("verifique o seu e-mail antes de ativar a verificação em duas etapas")
	// ErrEmailBloqueadoPorDoisFatores — alterar o e-mail com o 2FA ativo.
	//
	// Regra de segurança: o e-mail é o canal do segundo fator. Se pudesse ser
	// trocado com o 2FA ligado, quem tivesse uma sessão aberta redirecionaria os
	// códigos para um endereço seu. Para trocar o e-mail, o titular desativa
	// primeiro o 2FA (o que exige senha e código enviado ao e-mail atual).
	ErrEmailBloqueadoPorDoisFatores = errors.New("não é possível alterar o e-mail enquanto a verificação em duas etapas estiver ativa; o titular da conta deve desativá-la primeiro")
)

// DoisFatoresAtivadosEvent registra a ativação do 2FA.
type DoisFatoresAtivadosEvent struct {
	BaseEvent
	Metodo    string
	AtivadoEm time.Time
}

func (e *DoisFatoresAtivadosEvent) GetPayload() interface{} { return e }
func (e *DoisFatoresAtivadosEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

// DoisFatoresDesativadosEvent registra a desativação do 2FA.
type DoisFatoresDesativadosEvent struct {
	BaseEvent
	Metodo       string
	DesativadoEm time.Time
}

func (e *DoisFatoresDesativadosEvent) GetPayload() interface{} { return e }
func (e *DoisFatoresDesativadosEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

// validarAtivacaoDoisFatores concentra a regra de ativação (igual nos 3 agregados).
func validarAtivacaoDoisFatores(jaAtivo, emailPresente, emailVerificado bool) error {
	if jaAtivo {
		return ErrDoisFatoresJaAtivo
	}
	if !emailPresente || !emailVerificado {
		return ErrDoisFatoresEmailNaoVerificado
	}
	return nil
}

// validarDesativacaoDoisFatores concentra a regra de desativação.
func validarDesativacaoDoisFatores(ativo bool) error {
	if !ativo {
		return ErrDoisFatoresNaoAtivo
	}
	return nil
}
