package cadastros

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
)

const EntidadeTurma = "Turma"

// Turma é o registro de uma turma (ou, no 4º ano médio, de um grupo de
// trabalho). Era o agregado Turma, em event sourcing; passou a CRUD na
// Tarefa 134 (Parte 2B, regra 17). A tabela de registro é projection_turmas.
type Turma struct {
	ID             uuid.UUID
	CodigoTurma    string
	CodigoAcademia string
	Nivel          string
	CursoID        *uuid.UUID
	Turno          string  // "manha", "tarde" ou "noite"
	TemaTrabalho   *string // só grupos do 4º ano médio
	Estudantes     []string
	// HistoricoEstudantesAnoLetivo guarda, por ano letivo, quem já esteve na
	// turma. Impede reescrever a identidade acadêmica: nível e curso não
	// mudam depois que a turma teve estudantes num ano letivo.
	HistoricoEstudantesAnoLetivo map[string][]string
	Status                       string
	StatusAlteradoPor            *uuid.UUID
	StatusAlteradoEm             *time.Time
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
	Version                      int
	DeletedAt                    *time.Time
}

// ============================================================================
// Regras (funções puras, sem banco)
// ============================================================================

func turnoValido(t string) bool { return t == "manha" || t == "tarde" || t == "noite" }

// NovaTurma valida e monta uma turma nova, ativa.
func NovaTurma(codigoTurma, codigoAcademia, nivel string, cursoID *uuid.UUID, turno string, temaTrabalho *string) (*Turma, error) {
	normalizado, err := aggregates.NormalizarCodigoTurma(codigoTurma)
	if err != nil {
		return nil, &ErroRegra{Err: err}
	}
	codigoAcademia = strings.TrimSpace(codigoAcademia)
	nivel = strings.TrimSpace(nivel)
	if normalizado == "" || codigoAcademia == "" || nivel == "" {
		return nil, regra("codigo_turma, codigo_academia e nivel são obrigatórios")
	}
	if !turnoValido(turno) {
		return nil, regra("turno deve ser 'manha', 'tarde' ou 'noite'")
	}
	tema, err := aggregates.NormalizarTemaTrabalho(nivel, temaTrabalho)
	if err != nil {
		return nil, &ErroRegra{Err: err}
	}
	return &Turma{
		ID:                           uuid.New(),
		CodigoTurma:                  normalizado,
		CodigoAcademia:               codigoAcademia,
		Nivel:                        nivel,
		CursoID:                      cursoID,
		Turno:                        turno,
		TemaTrabalho:                 tema,
		Estudantes:                   []string{},
		HistoricoEstudantesAnoLetivo: map[string][]string{},
		Status:                       StatusAtivo,
	}, nil
}

func (t *Turma) registrarStatus(por uuid.UUID) {
	agora := time.Now().UTC()
	t.StatusAlteradoPor = &por
	t.StatusAlteradoEm = &agora
}

// Ativar passa a turma para ativa.
func (t *Turma) Ativar(por uuid.UUID) error {
	if t.Status == StatusAtivo {
		return regra("turma já está ativa")
	}
	if t.Status == StatusDeletado {
		return regra("turma já está deletada")
	}
	t.Status = StatusAtivo
	t.registrarStatus(por)
	return nil
}

// Desativar passa a turma para inativa.
func (t *Turma) Desativar(por uuid.UUID) error {
	if t.Status == StatusInativo {
		return regra("turma já está inativa")
	}
	if t.Status == StatusDeletado {
		return regra("turma já está deletada")
	}
	t.Status = StatusInativo
	t.registrarStatus(por)
	return nil
}

func (t *Turma) temEstudante(codigo string) bool {
	for _, e := range t.Estudantes {
		if e == codigo {
			return true
		}
	}
	return false
}

// AdicionarEstudante põe o estudante na turma. anoLetivo (opcional) entra no
// histórico por ano letivo.
func (t *Turma) AdicionarEstudante(codigoEstudante, anoLetivo string) error {
	if codigoEstudante == "" {
		return regra("codigo_estudante é obrigatório")
	}
	if t.temEstudante(codigoEstudante) {
		return regra("estudante já pertence a esta turma")
	}
	t.Estudantes = append(t.Estudantes, codigoEstudante)
	t.registrarHistorico(codigoEstudante, anoLetivo)
	return nil
}

func (t *Turma) registrarHistorico(codigoEstudante, anoLetivo string) {
	if anoLetivo == "" {
		return
	}
	if t.HistoricoEstudantesAnoLetivo == nil {
		t.HistoricoEstudantesAnoLetivo = map[string][]string{}
	}
	for _, c := range t.HistoricoEstudantesAnoLetivo[anoLetivo] {
		if c == codigoEstudante {
			return
		}
	}
	t.HistoricoEstudantesAnoLetivo[anoLetivo] = append(t.HistoricoEstudantesAnoLetivo[anoLetivo], codigoEstudante)
	sort.Strings(t.HistoricoEstudantesAnoLetivo[anoLetivo])
}

// RemoverEstudante tira o estudante da turma. O histórico por ano letivo não
// é alterado: quem esteve na turma num ano continua registrado.
func (t *Turma) RemoverEstudante(codigoEstudante string) error {
	if !t.temEstudante(codigoEstudante) {
		return regra("estudante não pertence a esta turma")
	}
	novos := make([]string, 0, len(t.Estudantes))
	for _, e := range t.Estudantes {
		if e != codigoEstudante {
			novos = append(novos, e)
		}
	}
	t.Estudantes = novos
	return nil
}

func (t *Turma) temHistoricoLetivo() bool { return len(t.HistoricoEstudantesAnoLetivo) > 0 }

// AtualizarDados altera nível, curso, turno e tema_trabalho. nil não altera o
// campo; em temaTrabalho, string vazia remove o tema. Se o nível passar a ser
// diferente de 4_ano_medio, o tema existente é removido. Nível e curso não
// mudam em turma com histórico de anos letivos.
func (t *Turma) AtualizarDados(nivel *string, cursoID *uuid.UUID, turno *string, temaTrabalho *string) error {
	if turno != nil && !turnoValido(*turno) {
		return regra("turno deve ser 'manha', 'tarde' ou 'noite'")
	}
	nivelMudou := nivel != nil && *nivel != t.Nivel
	cursoMudou := cursoID != nil && (t.CursoID == nil || *t.CursoID != *cursoID)
	if t.temHistoricoLetivo() && (nivelMudou || cursoMudou) {
		return regra("não é possível alterar nivel ou curso_id de turma com histórico de anos letivos")
	}
	nivelEfetivo := t.Nivel
	if nivel != nil && strings.TrimSpace(*nivel) != "" {
		nivelEfetivo = strings.TrimSpace(*nivel)
	}

	novoTema := t.TemaTrabalho
	if temaTrabalho != nil {
		normalizado, err := aggregates.NormalizarTemaTrabalho(nivelEfetivo, temaTrabalho)
		if err != nil {
			return &ErroRegra{Err: err}
		}
		novoTema = normalizado
	} else if t.TemaTrabalho != nil && nivelEfetivo != aggregates.NivelGruposTrabalho {
		novoTema = nil
	}

	// Tudo validado: só agora o estado muda.
	if nivel != nil {
		t.Nivel = *nivel
	}
	if cursoID != nil {
		t.CursoID = cursoID
	}
	if turno != nil {
		t.Turno = *turno
	}
	t.TemaTrabalho = novoTema
	return nil
}

// Deletar marca a turma como deletada (exclusão lógica). Precisa estar
// inativa e sem estudantes. A validação de processos ativos é do handler.
func (t *Turma) Deletar() error {
	if t.Status == StatusDeletado {
		return regra("turma já está deletada")
	}
	if t.Status == StatusAtivo {
		return regra("desative a turma antes de deletá-la")
	}
	if len(t.Estudantes) > 0 {
		return regra("remova todos os estudantes antes de deletar a turma (%d restantes)", len(t.Estudantes))
	}
	agora := time.Now().UTC()
	t.Status = StatusDeletado
	t.DeletedAt = &agora
	return nil
}

// ============================================================================
// Persistência
// ============================================================================

// TurmasRepo grava turmas na tabela de registro, com auditoria na mesma
// transação.
type TurmasRepo struct{ client *db.Client }

func NovoTurmasRepo(client *db.Client) *TurmasRepo { return &TurmasRepo{client: client} }

func (t *Turma) snapshot() map[string]any {
	var curso any
	if t.CursoID != nil {
		curso = t.CursoID.String()
	}
	var tema any
	if t.TemaTrabalho != nil {
		tema = *t.TemaTrabalho
	}
	return map[string]any{
		"codigo_turma":    t.CodigoTurma,
		"codigo_academia": t.CodigoAcademia,
		"nivel":           t.Nivel,
		"curso_id":        curso,
		"turno":           t.Turno,
		"tema_trabalho":   tema,
		"estudantes":      t.Estudantes,
		"status":          t.Status,
		"version":         t.Version,
	}
}

func jsonTexto(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

const colunasTurma = `id, codigo_turma, codigo_academia, nivel, curso_id, turno, tema_trabalho, estudantes,
	COALESCE(historico_estudantes_ano_letivo, '{}'::jsonb), status, status_alterado_por, status_alterado_em,
	created_at, updated_at, version, deleted_at`

func scanTurma(row interface{ Scan(...any) error }) (*Turma, error) {
	var (
		t       Turma
		curso   uuid.NullUUID
		tema    sql.NullString
		estud   []byte
		hist    []byte
		por     uuid.NullUUID
		em      sql.NullTime
		deleted sql.NullTime
	)
	if err := row.Scan(&t.ID, &t.CodigoTurma, &t.CodigoAcademia, &t.Nivel, &curso, &t.Turno, &tema, &estud,
		&hist, &t.Status, &por, &em, &t.CreatedAt, &t.UpdatedAt, &t.Version, &deleted); err != nil {
		return nil, err
	}
	if curso.Valid {
		c := curso.UUID
		t.CursoID = &c
	}
	if tema.Valid {
		s := tema.String
		t.TemaTrabalho = &s
	}
	_ = json.Unmarshal(estud, &t.Estudantes)
	if t.Estudantes == nil {
		t.Estudantes = []string{}
	}
	_ = json.Unmarshal(hist, &t.HistoricoEstudantesAnoLetivo)
	if t.HistoricoEstudantesAnoLetivo == nil {
		t.HistoricoEstudantesAnoLetivo = map[string][]string{}
	}
	if por.Valid {
		p := por.UUID
		t.StatusAlteradoPor = &p
	}
	if em.Valid {
		e := em.Time
		t.StatusAlteradoEm = &e
	}
	if deleted.Valid {
		d := deleted.Time
		t.DeletedAt = &d
	}
	return &t, nil
}

// Criar insere a turma e audita ("TurmaCriada"). A academia precisa existir e
// o código da turma não pode estar em uso por outra turma não deletada da
// mesma academia (conferido dentro da transação).
func (r *TurmasRepo) Criar(ctx context.Context, audit db.AuditContext, t *Turma) error {
	return r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		if err := exigirAcademia(ctx, tx, t.CodigoAcademia); err != nil {
			return err
		}
		var existe bool
		if err := tx.QueryRowxContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM projection_turmas
			               WHERE codigo_academia = $1 AND codigo_turma = $2 AND status <> 'deletado')`,
			t.CodigoAcademia, t.CodigoTurma).Scan(&existe); err != nil {
			return fmt.Errorf("verificar código da turma: %w", err)
		}
		if existe {
			return regra("já existe %s com este código nesta academia", artigoAgrupamento(t.Nivel))
		}
		var curso any
		if t.CursoID != nil {
			curso = t.CursoID.String()
		}
		var tema any
		if t.TemaTrabalho != nil {
			tema = *t.TemaTrabalho
		}
		err := tx.QueryRowxContext(ctx, `
			INSERT INTO projection_turmas
				(id, codigo_turma, codigo_academia, nivel, curso_id, turno, tema_trabalho,
				 estudantes, historico_estudantes_ano_letivo, status, version, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, '[]'::jsonb, '{}'::jsonb, $8, 1, (NOW() AT TIME ZONE 'UTC'))
			RETURNING created_at, updated_at, version`,
			t.ID, t.CodigoTurma, t.CodigoAcademia, t.Nivel, curso, t.Turno, tema, t.Status,
		).Scan(&t.CreatedAt, &t.UpdatedAt, &t.Version)
		if err != nil {
			return fmt.Errorf("inserir turma: %w", err)
		}
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeTurma, EntidadeID: t.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "TurmaCriada", Depois: t.snapshot(),
		})
	})
}

func artigoAgrupamento(nivel string) string {
	if aggregates.TipoAgrupamentoDoNivel(nivel) == "grupo" {
		return "um grupo"
	}
	return "uma turma"
}

func carregarTurmaTx(ctx context.Context, tx *sqlx.Tx, id uuid.UUID) (*Turma, error) {
	t, err := scanTurma(tx.QueryRowxContext(ctx,
		`SELECT `+colunasTurma+` FROM projection_turmas WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNaoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("carregar turma: %w", err)
	}
	return t, nil
}

func gravarTurmaTx(ctx context.Context, tx *sqlx.Tx, t *Turma) error {
	var curso any
	if t.CursoID != nil {
		curso = t.CursoID.String()
	}
	var tema any
	if t.TemaTrabalho != nil {
		tema = *t.TemaTrabalho
	}
	var deletedAt, por, em any
	if t.DeletedAt != nil {
		deletedAt = t.DeletedAt.UTC()
	}
	if t.StatusAlteradoPor != nil {
		por = t.StatusAlteradoPor.String()
	}
	if t.StatusAlteradoEm != nil {
		em = t.StatusAlteradoEm.UTC()
	}
	return tx.QueryRowxContext(ctx, `
		UPDATE projection_turmas
		SET nivel = $2, curso_id = $3, turno = $4, tema_trabalho = $5,
		    estudantes = $6::jsonb, historico_estudantes_ano_letivo = $7::jsonb,
		    status = $8, status_alterado_por = $9, status_alterado_em = $10, deleted_at = $11,
		    version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING updated_at, version`,
		t.ID, t.Nivel, curso, t.Turno, tema, jsonTexto(t.Estudantes), jsonTexto(t.HistoricoEstudantesAnoLetivo),
		t.Status, por, em, deletedAt,
	).Scan(&t.UpdatedAt, &t.Version)
}

// Atualizar carrega a turma com bloqueio de linha, aplica fn (uma regra do
// domínio), grava e audita, tudo na mesma transação. Se fn recusar, nada é
// gravado. fn pode ser repetida em caso de conflito de serialização.
func (r *TurmasRepo) Atualizar(ctx context.Context, audit db.AuditContext, id uuid.UUID, acao, motivo string, fn func(*Turma) error) (*Turma, error) {
	var resultado *Turma
	err := r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		t, err := carregarTurmaTx(ctx, tx, id)
		if err != nil {
			return err
		}
		antes := t.snapshot()
		if err := fn(t); err != nil {
			return err
		}
		if err := gravarTurmaTx(ctx, tx, t); err != nil {
			return fmt.Errorf("atualizar turma: %w", err)
		}
		resultado = t
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeTurma, EntidadeID: t.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: acao, Antes: antes, Depois: t.snapshot(), Motivo: motivo,
		})
	})
	if err != nil {
		return nil, err
	}
	return resultado, nil
}

// idsTurmasDoCursoTx lista as turmas do curso que ainda não foram deletadas.
func idsTurmasDoCursoTx(ctx context.Context, tx *sqlx.Tx, cursoID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	if err := tx.SelectContext(ctx, &ids,
		`SELECT id FROM projection_turmas WHERE curso_id = $1 AND status <> 'deletado' ORDER BY codigo_turma FOR UPDATE`,
		cursoID); err != nil {
		return nil, fmt.Errorf("listar turmas do curso: %w", err)
	}
	return ids, nil
}
