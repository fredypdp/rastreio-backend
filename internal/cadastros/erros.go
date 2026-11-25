// Package cadastros guarda os dados cadastrais que deixaram de ser event
// sourcing e passaram a CRUD com auditoria (regra 17 do Plano de Atualizações 1,
// Parte 2B, entrega 2B-1): hoje, cursos e matérias.
//
// Cada registro mora na sua tabela (a antiga projeção, agora tabela de
// registro: nada é reconstruído a partir do ledger) e TODA alteração grava, na
// mesma transação, uma linha em auditoria_crud (internal/db/auditoria_crud.go).
//
// As regras de negócio são funções puras sobre structs simples, sem banco, e
// ficam testáveis sozinhas; a persistência está em CursosRepo e MateriasRepo.
package cadastros

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrNaoEncontrado indica que o registro pedido não existe.
var ErrNaoEncontrado = errors.New("registro não encontrado")

// ErroRegra encapsula uma violação de regra de negócio (a que, em event
// sourcing, o agregado devolvia ao recusar um comando). O handler responde com
// erro de validação; qualquer outro erro é falha interna.
type ErroRegra struct{ Err error }

func (e *ErroRegra) Error() string { return e.Err.Error() }
func (e *ErroRegra) Unwrap() error { return e.Err }

func regra(format string, args ...any) error {
	return &ErroRegra{Err: fmt.Errorf(format, args...)}
}

// EhErroRegra devolve a violação de regra contida em err, se houver.
func EhErroRegra(err error) (*ErroRegra, bool) {
	var r *ErroRegra
	if errors.As(err, &r) {
		return r, true
	}
	return nil, false
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
