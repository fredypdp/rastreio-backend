package projections

import (
	"fmt"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
)

// Tarefa 123 (SEC-04 / SEC-10) — estado da senha temporária nas projeções.
//
// deve_alterar_senha e sessoes_validas_desde são consequência dos eventos do
// ledger (SenhaTemporariaDefinida e os eventos de troca de senha), por isso
// qualquer rebuild os reconstrói. O instante de sessoes_validas_desde é o
// OccurredAt do evento — nunca CURRENT_TIMESTAMP — para que um rebuild não
// invalide todas as sessões de uma vez.

// setSenhaTemporaria liga deve_alterar_senha para o dono do evento.
func setSenhaTemporaria(conn execer, tabela string, event db.Event) error {
	if !tabelasDoisFatores[tabela] {
		return fmt.Errorf("tabela de projeção não permitida: %s", tabela)
	}
	_, err := conn.Exec(
		`UPDATE `+tabela+`
		    SET deve_alterar_senha = TRUE, version = $1, updated_at = CURRENT_TIMESTAMP, last_event_id = $2
		  WHERE id = $3`,
		event.EventVersion, event.EventID, event.AggregateID,
	)
	return err
}

// aplicarSenhaAlterada grava o novo hash e acerta as duas colunas conforme o
// motivo do evento:
//
//	alteracao_usuario (ou vazio, eventos antigos) → deve_alterar_senha = FALSE
//	reset_senha                                   → FALSE e sessões anteriores invalidadas
//	credenciais_reenviadas                        → TRUE  e sessões anteriores invalidadas
func aplicarSenhaAlterada(conn execer, tabela, novoHash, motivo string, event db.Event) error {
	if !tabelasDoisFatores[tabela] {
		return fmt.Errorf("tabela de projeção não permitida: %s", tabela)
	}
	switch motivo {
	case aggregates.MotivoSenhaCredenciaisReenviadas:
		_, err := conn.Exec(
			`UPDATE `+tabela+`
			    SET senha_hash = $1, deve_alterar_senha = TRUE, sessoes_validas_desde = $2,
			        version = $3, updated_at = CURRENT_TIMESTAMP, last_event_id = $4
			  WHERE id = $5`,
			novoHash, event.OccurredAt, event.EventVersion, event.EventID, event.AggregateID)
		return err
	case aggregates.MotivoSenhaResetSenha:
		_, err := conn.Exec(
			`UPDATE `+tabela+`
			    SET senha_hash = $1, deve_alterar_senha = FALSE, sessoes_validas_desde = $2,
			        version = $3, updated_at = CURRENT_TIMESTAMP, last_event_id = $4
			  WHERE id = $5`,
			novoHash, event.OccurredAt, event.EventVersion, event.EventID, event.AggregateID)
		return err
	default: // alteracao_usuario e eventos antigos sem motivo
		_, err := conn.Exec(
			`UPDATE `+tabela+`
			    SET senha_hash = $1, deve_alterar_senha = FALSE,
			        version = $2, updated_at = CURRENT_TIMESTAMP, last_event_id = $3
			  WHERE id = $4`,
			novoHash, event.EventVersion, event.EventID, event.AggregateID)
		return err
	}
}
