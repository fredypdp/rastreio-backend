package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"spuri/internal/utils"
)

// ============================================================================
// Auditoria do que fica em CRUD (Parte 2B, entrega 2B-1)
// ============================================================================
//
// O que vai para CRUD deixa de passar pelo ledger. Em troca, TODA alteração
// feita por um handler em CRUD grava, NA MESMA transação, uma linha em
// auditoria_crud (migration 142): quem, quando, o quê, antes e depois, motivo.
// A alteração e a auditoria entram juntas ou saem juntas — nunca uma sem a
// outra.
//
// Uso típico (handler em CRUD):
//
//	err := client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *Auditor) error {
//	    // 1) a alteração na tabela de registro
//	    if _, err := tx.ExecContext(ctx, `UPDATE cursos SET nome=$1 WHERE id=$2`, novo, id); err != nil {
//	        return err
//	    }
//	    // 2) a auditoria, na MESMA transação
//	    return aud.Registrar(ctx, EntradaAuditoria{
//	        EntidadeTipo: "Curso", EntidadeID: id, AcademiaID: academiaID,
//	        Acao: "CursoDadosAtualizados", Antes: antes, Depois: depois,
//	    })
//	})
//
// REGRAS
//   - fn pode ser executada de novo (retentativa em falha de serialização):
//     não faça nela efeitos fora do banco (e-mail, SMS, arquivo).
//   - Senha: o hash bcrypt fica só na tabela da conta. A auditoria diz quem
//     alterou e quando, SEM o hash; o código de verificação nunca entra. Chaves
//     proibidas em Antes/Depois fazem Registrar falhar (e a transação inteira
//     voltar atrás) — ver ChavesProibidasAuditoria.
//   - Ação administrativa continua usando AcaoAdminRegistrada (ledger).

// ChavesProibidasAuditoria são as chaves que nunca podem aparecer em
// Antes/Depois, em qualquer nível, sem diferenciar maiúsculas de minúsculas.
// A mesma lista existe na migration 142 (auditoria_crud_chave_proibida); um
// teste garante que as duas não divergem.
var ChavesProibidasAuditoria = []string{
	"senha", "senha_hash", "password", "password_hash", "hash_senha",
	"codigo", "codigo_verificacao", "codigo_2fa",
	"token", "segredo", "secret",
}

// ErrAuditoriaChaveProibida é devolvido quando Antes/Depois traz uma chave de
// ChavesProibidasAuditoria.
var ErrAuditoriaChaveProibida = errors.New("auditoria: chave proibida em antes/depois")

// EntradaAuditoria descreve uma alteração a auditar.
type EntradaAuditoria struct {
	EntidadeTipo string // ex.: "Curso" — obrigatório
	EntidadeID   string // id da entidade alterada — obrigatório
	AcademiaID   string // academia dona do dado (vazio = não se aplica)
	Acao         string // ex.: "CursoDadosAtualizados" — obrigatório
	Antes        any    // estado anterior (nil em criação); serializado em JSON
	Depois       any    // estado novo (nil em eliminação); serializado em JSON
	Motivo       string // opcional (ex.: motivo de desativação)
}

// Auditor grava auditoria dentro de uma transação já aberta. É criado por
// ComAuditoria; o código de domínio só o recebe.
type Auditor struct {
	tx    *sqlx.Tx
	audit AuditContext
	ator  *AtorAuditoria
}

// NovoAuditor cria um Auditor sobre uma transação existente, para handlers que
// já controlam a própria transação. A transação continua sendo de quem a abriu.
func NovoAuditor(tx *sqlx.Tx, audit AuditContext, ator *AtorAuditoria) *Auditor {
	return &Auditor{tx: tx, audit: audit, ator: ator}
}

// Registrar grava uma linha de auditoria na transação do Auditor. Se falhar, o
// chamador deve devolver o erro para que a transação toda seja desfeita.
func (a *Auditor) Registrar(ctx context.Context, e EntradaAuditoria) error {
	if a == nil || a.tx == nil {
		return errors.New("auditoria: sem transação")
	}
	if strings.TrimSpace(e.EntidadeTipo) == "" || strings.TrimSpace(e.EntidadeID) == "" || strings.TrimSpace(e.Acao) == "" {
		return errors.New("auditoria: entidade_tipo, entidade_id e acao são obrigatórios")
	}

	antes, err := serializarAuditoria(e.Antes)
	if err != nil {
		return fmt.Errorf("auditoria: antes: %w", err)
	}
	depois, err := serializarAuditoria(e.Depois)
	if err != nil {
		return fmt.Errorf("auditoria: depois: %w", err)
	}

	var academia any
	if strings.TrimSpace(e.AcademiaID) != "" {
		academia = e.AcademiaID
	}
	var emNomeID, emNomeTipo string
	if a.ator != nil {
		emNomeID, emNomeTipo = a.ator.ID, a.ator.Tipo
	}

	_, err = a.tx.ExecContext(ctx, `
		INSERT INTO auditoria_crud
			(entidade_tipo, entidade_id, academia_id, acao,
			 ator_id, ator_tipo, em_nome_de_id, em_nome_de_tipo, ip,
			 antes, depois, motivo, sequencia, hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 0, '')`,
		e.EntidadeTipo, e.EntidadeID, academia, e.Acao,
		a.audit.UserID, a.audit.UserType, emNomeID, emNomeTipo, a.audit.IP,
		antes, depois, e.Motivo,
	)
	if err != nil {
		return fmt.Errorf("auditoria: gravar: %w", err)
	}
	return nil
}

// serializarAuditoria converte o valor em JSON e recusa chaves proibidas. nil
// vira NULL no banco.
func serializarAuditoria(valor any) (any, error) {
	if valor == nil {
		return nil, nil
	}
	bruto, err := json.Marshal(valor)
	if err != nil {
		return nil, err
	}
	if string(bruto) == "null" {
		return nil, nil
	}
	var arvore any
	if err := json.Unmarshal(bruto, &arvore); err != nil {
		return nil, err
	}
	if chave := chaveProibida(arvore); chave != "" {
		return nil, fmt.Errorf("%w: %q", ErrAuditoriaChaveProibida, chave)
	}
	return string(bruto), nil
}

func chaveProibida(no any) string {
	switch v := no.(type) {
	case map[string]any:
		for k, filho := range v {
			for _, proibida := range ChavesProibidasAuditoria {
				if strings.EqualFold(k, proibida) {
					return k
				}
			}
			if achou := chaveProibida(filho); achou != "" {
				return achou
			}
		}
	case []any:
		for _, filho := range v {
			if achou := chaveProibida(filho); achou != "" {
				return achou
			}
		}
	}
	return ""
}

// ComAuditoria executa fn dentro de UMA transação serializable. A alteração
// feita por fn e a auditoria registrada por fn entram juntas no commit; se fn
// ou o commit falhar, nada fica (nem a alteração, nem a auditoria).
//
// Falhas de serialização/versão são repetidas, com o mesmo backoff limitado de
// SaveWithAudit; por isso fn tem de ser repetível (sem efeitos fora do banco).
func (c *Client) ComAuditoria(ctx context.Context, audit AuditContext, fn func(tx *sqlx.Tx, aud *Auditor) error) error {
	return c.ComAuditoriaComAtor(ctx, audit, nil, fn)
}

// ComAuditoriaComAtor é ComAuditoria para ações feitas "em nome de" outra
// conta: o ator (quem realmente agiu) vai para em_nome_de_id/em_nome_de_tipo.
func (c *Client) ComAuditoriaComAtor(ctx context.Context, audit AuditContext, ator *AtorAuditoria, fn func(tx *sqlx.Tx, aud *Auditor) error) error {
	if fn == nil {
		return errors.New("auditoria: fn é obrigatória")
	}
	const maxAttempts = 10
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = c.comAuditoriaUmaVez(ctx, audit, ator, fn)
		if err == nil || !(utils.IsSerializationFailure(err) || utils.IsAggregateVersionConflict(err)) {
			return err
		}
		backoff := time.Duration(15*(attempt+1)) * time.Millisecond
		jitter := time.Duration(rand.Intn(15*(attempt+1)+1)) * time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff + jitter):
		}
	}
	return err
}

func (c *Client) comAuditoriaUmaVez(ctx context.Context, audit AuditContext, ator *AtorAuditoria, fn func(tx *sqlx.Tx, aud *Auditor) error) error {
	if ator == nil {
		ator = audit.Ator
	}
	tx, err := c.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("erro ao iniciar transação: %w", err)
	}
	defer tx.Rollback()

	if err := fn(tx, NovoAuditor(tx, audit, ator)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("erro ao confirmar transação: %w", err)
	}
	return nil
}

// LinhaAuditoria é uma linha lida de auditoria_crud.
type LinhaAuditoria struct {
	AuditID      string          `db:"audit_id" json:"audit_id"`
	EntidadeTipo string          `db:"entidade_tipo" json:"entidade_tipo"`
	EntidadeID   string          `db:"entidade_id" json:"entidade_id"`
	AcademiaID   sql.NullString  `db:"academia_id" json:"-"`
	Acao         string          `db:"acao" json:"acao"`
	AtorID       string          `db:"ator_id" json:"ator_id"`
	AtorTipo     string          `db:"ator_tipo" json:"ator_tipo"`
	EmNomeDeID   string          `db:"em_nome_de_id" json:"em_nome_de_id,omitempty"`
	EmNomeDeTipo string          `db:"em_nome_de_tipo" json:"em_nome_de_tipo,omitempty"`
	IP           string          `db:"ip" json:"ip,omitempty"`
	Antes        json.RawMessage `db:"antes" json:"antes,omitempty"`
	Depois       json.RawMessage `db:"depois" json:"depois,omitempty"`
	Motivo       string          `db:"motivo" json:"motivo,omitempty"`
	OcorridoEm   time.Time       `db:"ocorrido_em" json:"ocorrido_em"`
	Sequencia    int             `db:"sequencia" json:"sequencia"`
}

// ListarAuditoria devolve a auditoria de uma entidade, da mais recente para a
// mais antiga. limit/offset passam por ValidateLimit/ValidateOffset.
func (c *Client) ListarAuditoria(ctx context.Context, entidadeTipo, entidadeID string, limit, offset int) ([]LinhaAuditoria, error) {
	var linhas []LinhaAuditoria
	err := c.db.SelectContext(ctx, &linhas, `
		SELECT audit_id::text AS audit_id, entidade_tipo, entidade_id, academia_id::text AS academia_id,
		       acao, ator_id, ator_tipo, em_nome_de_id, em_nome_de_tipo, ip,
		       COALESCE(antes, 'null'::jsonb) AS antes, COALESCE(depois, 'null'::jsonb) AS depois,
		       motivo, ocorrido_em, sequencia
		FROM auditoria_crud
		WHERE entidade_tipo = $1 AND entidade_id = $2
		ORDER BY sequencia DESC
		LIMIT $3 OFFSET $4`,
		entidadeTipo, entidadeID, ValidateLimit(limit), ValidateOffset(offset))
	if err != nil {
		return nil, fmt.Errorf("auditoria: listar: %w", err)
	}
	return linhas, nil
}

// VerificarCadeiaAuditoria confere a sequência, os hashes e o encadeamento da
// auditoria de uma entidade (verificar_cadeia_auditoria_crud, migration 142).
func (c *Client) VerificarCadeiaAuditoria(ctx context.Context, entidadeTipo, entidadeID string) (valida bool, mensagem string, err error) {
	var linha struct {
		IsValid bool   `db:"is_valid"`
		Message string `db:"message"`
	}
	err = c.db.GetContext(ctx, &linha,
		`SELECT is_valid, message FROM verificar_cadeia_auditoria_crud($1, $2)`,
		entidadeTipo, entidadeID)
	if err != nil {
		return false, "", fmt.Errorf("auditoria: verificar cadeia: %w", err)
	}
	return linha.IsValid, linha.Message, nil
}
