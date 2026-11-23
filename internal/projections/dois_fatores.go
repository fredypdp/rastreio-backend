package projections

import (
	"database/sql"
	"fmt"

	"spuri/internal/db"
)

// Tarefa 122 — estado da verificação em duas etapas nas projeções.
//
// O estado vem dos eventos DoisFatoresAtivados / DoisFatoresDesativados do
// ledger; a coluna dois_fatores_ativo de cada projeção é consequência deles
// (e é reconstruída em qualquer rebuild, pois nasce FALSE por padrão).

// tabelasDoisFatores é uma whitelist fechada: o nome da tabela nunca vem do
// cliente, mas mesmo assim só estes valores chegam ao SQL montado abaixo.
var tabelasDoisFatores = map[string]bool{
	"projection_admins":     true,
	"projection_academias":  true,
	"projection_estudantes": true,
	// Parte 4: secretário, professor e encarregado (uma só tabela).
	"projection_contas_pessoa": true,
}

// execer abstrai *sqlx.DB / *sql.Tx para o UPDATE abaixo.
type execer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// setDoisFatoresAtivo grava dois_fatores_ativo na projeção do usuário dono do
// evento. É idempotente (mesmo valor) e atualiza version/last_event_id como os
// demais handlers da projeção.
func setDoisFatoresAtivo(conn execer, tabela string, ativo bool, event db.Event) error {
	if !tabelasDoisFatores[tabela] {
		return fmt.Errorf("tabela de projeção não permitida para 2FA: %s", tabela)
	}
	_, err := conn.Exec(
		`UPDATE `+tabela+`
		    SET dois_fatores_ativo = $1, version = $2, updated_at = CURRENT_TIMESTAMP, last_event_id = $3
		  WHERE id = $4`,
		ativo, event.EventVersion, event.EventID, event.AggregateID,
	)
	return err
}
