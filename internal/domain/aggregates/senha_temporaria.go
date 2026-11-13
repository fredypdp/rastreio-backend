package aggregates

import (
	"encoding/json"
	"time"
)

// Tarefa 123 (SEC-04) — troca obrigatória da senha temporária.
//
// Toda conta criada por terceiros (estudante, academia, administrador) nasce
// com uma senha temporária. O evento SenhaTemporariaDefinida, igual nos três
// agregados (as projeções filtram por aggregate_type, como em "EmailVerificado"
// e nos eventos do 2FA), liga a flag deve_alterar_senha na projeção. A flag é
// consequência do ledger e é reconstruída em qualquer rebuild.
//
// Quem a desliga é o evento de troca de senha, conforme o motivo:
//   - alteracao_usuario     → desliga (o titular trocou a própria senha);
//   - reset_senha           → desliga e invalida as sessões anteriores;
//   - credenciais_reenviadas → LIGA de novo e invalida as sessões anteriores.

const (
	// MotivoSenhaAlteracaoUsuario — o próprio titular trocou a senha. É também
	// o valor assumido para eventos antigos, sem motivo.
	MotivoSenhaAlteracaoUsuario = "alteracao_usuario"
	// MotivoSenhaResetSenha — o titular redefiniu a senha pelo link de recuperação.
	MotivoSenhaResetSenha = "reset_senha"
	// MotivoSenhaCredenciaisReenviadas — a entidade que gere a conta redefiniu a
	// senha e reenviou credenciais temporárias.
	MotivoSenhaCredenciaisReenviadas = "credenciais_reenviadas"
)

// SenhaTemporariaDefinidaEvent registra que a conta nasceu com senha temporária.
type SenhaTemporariaDefinidaEvent struct {
	BaseEvent
	DefinidaEm time.Time
}

func (e *SenhaTemporariaDefinidaEvent) GetPayload() interface{} { return e }
func (e *SenhaTemporariaDefinidaEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }
