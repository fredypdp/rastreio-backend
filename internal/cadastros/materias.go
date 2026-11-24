package cadastros

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"spuri/internal/db"
)

const EntidadeMateria = "MateriaDisciplinar"

// Materia é o registro de uma matéria disciplinar (fundamental, médio ou
// superior). Matéria superior exige curso e período.
type Materia struct {
	ID                      uuid.UUID
	Nome                    string
	Type                    string // "fundamental", "medio" ou "superior"
	AnosAcademicos          []string
	Periodo                 string // só superior
	PendenciaPermitida      bool
	PendenciaNivelConclusao *string // só superior
	CodigoAcademia          string
	CursoID                 *uuid.UUID
	Status                  string
	CreatedAt               time.Time
	UpdatedAt               time.Time
	Version                 int
	DeletedAt               *time.Time
}

// ============================================================================
// Regras (funções puras, sem banco)
// ============================================================================

// NovaMateria valida e monta uma matéria nova, ativa. O período (superior) é
// definido à parte, com DefinirPeriodo, como acontecia antes.
func NovaMateria(
	nome, tipo string,
	anosAcademicos []string,
	codigoAcademia string,
	cursoID *uuid.UUID,
	pendenciaPermitida *bool,
	pendenciaNivelConclusao *string,
) (*Materia, error) {
	if nome == "" {
		return nil, regra("nome é obrigatório")
	}
	if tipo != "fundamental" && tipo != "medio" && tipo != "superior" {
		return nil, regra("tipo deve ser 'fundamental', 'medio' ou 'superior'")
	}
	if codigoAcademia == "" {
		return nil, regra("codigo_academia é obrigatório")
	}
	if len(anosAcademicos) == 0 {
		return nil, regra("anos_academicos é obrigatório para matérias")
	}
	if tipo == "superior" && cursoID == nil {
		return nil, regra("curso_id é obrigatório para matérias do tipo 'superior'")
	}
	if (tipo == "fundamental" || tipo == "medio") && pendenciaPermitida != nil {
		return nil, regra("pendencia_permitida só está disponível para matérias do tipo 'superior'")
	}
	if (tipo == "fundamental" || tipo == "medio") && pendenciaNivelConclusao != nil {
		return nil, regra("pendencia_nivel_conclusao só está disponível para matérias do tipo 'superior'")
	}
	efetiva := tipo == "superior"
	if pendenciaPermitida != nil {
		efetiva = *pendenciaPermitida
	}
	return &Materia{
		ID:                      uuid.New(),
		Nome:                    nome,
		Type:                    tipo,
		AnosAcademicos:          anosAcademicos,
		PendenciaPermitida:      efetiva,
		PendenciaNivelConclusao: pendenciaNivelConclusao,
		CodigoAcademia:          codigoAcademia,
		CursoID:                 cursoID,
		Status:                  StatusAtivo,
	}, nil
}

// Ativar passa a matéria para ativa. Matéria superior exige período.
func (m *Materia) Ativar() error {
	if m.Status == StatusAtivo {
		return regra("matéria já está ativa")
	}
	if m.Type == "superior" && m.Periodo == "" {
		return regra("matérias superiores exigem periodo definido na criação antes da ativação")
	}
	m.Status = StatusAtivo
	return nil
}

// Desativar passa a matéria para inativa.
func (m *Materia) Desativar() error {
	if m.Status == StatusInativo {
		return regra("matéria já está inativa")
	}
	m.Status = StatusInativo
	return nil
}

// AtualizarDados atualiza nome, anos, curso, pendência e/ou nível de conclusão
// da pendência. nil não altera o campo.
func (m *Materia) AtualizarDados(nome *string, anosAcademicos []string, cursoID *uuid.UUID, pendenciaPermitida *bool, pendenciaNivelConclusao *string) error {
	if nome == nil && anosAcademicos == nil && cursoID == nil && pendenciaPermitida == nil && pendenciaNivelConclusao == nil {
		return regra("nenhum campo para atualizar")
	}
	if pendenciaPermitida != nil && (m.Type == "fundamental" || m.Type == "medio") {
		return regra("pendencia_permitida só está disponível para matérias do tipo 'superior'")
	}
	if pendenciaNivelConclusao != nil && (m.Type == "fundamental" || m.Type == "medio") {
		return regra("pendencia_nivel_conclusao só está disponível para matérias do tipo 'superior'")
	}
	if anosAcademicos != nil && len(anosAcademicos) == 0 {
		return regra("anos_academicos é obrigatório para matérias")
	}

	if nome != nil {
		m.Nome = *nome
	}
	if anosAcademicos != nil {
		m.AnosAcademicos = anosAcademicos
	}
	if cursoID != nil {
		m.CursoID = cursoID
	}
	if pendenciaPermitida != nil {
		m.PendenciaPermitida = *pendenciaPermitida
	}
	if pendenciaNivelConclusao != nil {
		m.PendenciaNivelConclusao = pendenciaNivelConclusao
	}
	return nil
}

// DefinirPeriodo define o período (só matéria superior).
func (m *Materia) DefinirPeriodo(periodo string) error {
	if m.Type != "superior" {
		return regra("período só pode ser definido para matérias do tipo 'superior'")
	}
	if periodo == "" {
		return regra("periodo não pode ser vazio")
	}
	m.Periodo = periodo
	return nil
}

// Deletar marca a matéria como deletada (exclusão lógica). Precisa estar inativa.
func (m *Materia) Deletar() error {
	if m.Status == StatusDeletado {
		return regra("matéria já está deletada")
	}
	if m.Status == StatusAtivo {
		return regra("desative a matéria antes de deletá-la")
	}
	agora := time.Now().UTC()
	m.Status = StatusDeletado
	m.DeletedAt = &agora
	return nil
}

// ============================================================================
// Persistência
// ============================================================================

// MateriasRepo grava matérias na tabela de registro, com auditoria na mesma
// transação.
type MateriasRepo struct{ client *db.Client }

func NovoMateriasRepo(client *db.Client) *MateriasRepo { return &MateriasRepo{client: client} }

func (m *Materia) snapshot() map[string]any {
	var curso any
	if m.CursoID != nil {
		curso = m.CursoID.String()
	}
	return map[string]any{
		"nome":                      m.Nome,
		"type":                      m.Type,
		"anos_academicos":           m.AnosAcademicos,
		"periodo":                   m.Periodo,
		"pendencia_permitida":       m.PendenciaPermitida,
		"pendencia_nivel_conclusao": m.PendenciaNivelConclusao,
		"codigo_academia":           m.CodigoAcademia,
		"curso_id":                  curso,
		"status":                    m.Status,
		"version":                   m.Version,
	}
}

func exigirCursoDaAcademia(ctx context.Context, tx *sqlx.Tx, cursoID uuid.UUID, codigoAcademia string) error {
	var codigo string
	err := tx.QueryRowxContext(ctx, `SELECT codigo_academia FROM projection_cursos WHERE id = $1`, cursoID).Scan(&codigo)
	if errors.Is(err, sql.ErrNoRows) {
		return regra("curso não encontrado")
	}
	if err != nil {
		return fmt.Errorf("verificar curso da matéria: %w", err)
	}
	if codigo != codigoAcademia {
		return regra("curso não pertence a esta academia")
	}
	return nil
}

// Criar insere a matéria (com o período, se houver) e audita ("MateriaCriada").
func (r *MateriasRepo) Criar(ctx context.Context, audit db.AuditContext, m *Materia) error {
	return r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		if err := exigirAcademia(ctx, tx, m.CodigoAcademia); err != nil {
			return err
		}
		if m.CursoID != nil {
			if err := exigirCursoDaAcademia(ctx, tx, *m.CursoID, m.CodigoAcademia); err != nil {
				return err
			}
		}
		var curso any
		if m.CursoID != nil {
			curso = m.CursoID.String()
		}
		err := tx.QueryRowxContext(ctx, `
			INSERT INTO projection_materias
				(id, nome, type, anos_academicos, codigo_academia, curso_id, periodo,
				 pendencia_permitida, pendencia_nivel_conclusao, status, version)
			VALUES ($1, $2, $3, $4::jsonb, $5, $6, NULLIF($7, ''), $8, $9, $10, 1)
			RETURNING created_at, updated_at, version`,
			m.ID, m.Nome, m.Type, jsonOuNulo(m.AnosAcademicos), m.CodigoAcademia, curso, m.Periodo,
			m.PendenciaPermitida, m.PendenciaNivelConclusao, m.Status,
		).Scan(&m.CreatedAt, &m.UpdatedAt, &m.Version)
		if err != nil {
			return fmt.Errorf("inserir matéria: %w", err)
		}
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeMateria, EntidadeID: m.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "MateriaCriada", Depois: m.snapshot(),
		})
	})
}

func carregarMateriaTx(ctx context.Context, tx *sqlx.Tx, id uuid.UUID) (*Materia, error) {
	var (
		m       Materia
		anos    []byte
		periodo sql.NullString
		nivel   sql.NullString
		curso   sql.NullString
		deleted sql.NullTime
	)
	err := tx.QueryRowxContext(ctx, `
		SELECT id, nome, type, anos_academicos, periodo, pendencia_permitida, pendencia_nivel_conclusao,
		       codigo_academia, curso_id::text, status, created_at, updated_at, version, deleted_at
		FROM projection_materias WHERE id = $1 FOR UPDATE`, id).Scan(
		&m.ID, &m.Nome, &m.Type, &anos, &periodo, &m.PendenciaPermitida, &nivel,
		&m.CodigoAcademia, &curso, &m.Status, &m.CreatedAt, &m.UpdatedAt, &m.Version, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNaoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("carregar matéria: %w", err)
	}
	if anos != nil {
		_ = json.Unmarshal(anos, &m.AnosAcademicos)
	}
	m.Periodo = periodo.String
	if nivel.Valid {
		s := nivel.String
		m.PendenciaNivelConclusao = &s
	}
	if curso.Valid {
		if u, err := uuid.Parse(curso.String); err == nil {
			m.CursoID = &u
		}
	}
	if deleted.Valid {
		t := deleted.Time
		m.DeletedAt = &t
	}
	return &m, nil
}

func gravarMateriaTx(ctx context.Context, tx *sqlx.Tx, m *Materia) error {
	var curso, deletedAt any
	if m.CursoID != nil {
		curso = m.CursoID.String()
	}
	if m.DeletedAt != nil {
		deletedAt = m.DeletedAt.UTC()
	}
	return tx.QueryRowxContext(ctx, `
		UPDATE projection_materias
		SET nome = $2, anos_academicos = $3::jsonb, curso_id = $4, periodo = NULLIF($5, ''),
		    pendencia_permitida = $6, pendencia_nivel_conclusao = $7, status = $8, deleted_at = $9,
		    version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING updated_at, version`,
		m.ID, m.Nome, jsonOuNulo(m.AnosAcademicos), curso, m.Periodo,
		m.PendenciaPermitida, m.PendenciaNivelConclusao, m.Status, deletedAt,
	).Scan(&m.UpdatedAt, &m.Version)
}

func idsMateriasDoCursoTx(ctx context.Context, tx *sqlx.Tx, cursoID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	if err := tx.SelectContext(ctx, &ids,
		`SELECT id FROM projection_materias WHERE curso_id = $1 ORDER BY created_at, id`, cursoID); err != nil {
		return nil, fmt.Errorf("listar matérias do curso: %w", err)
	}
	return ids, nil
}

// Atualizar carrega a matéria com bloqueio de linha, aplica fn (uma regra do
// domínio), grava e audita na mesma transação. Se fn recusar, nada é gravado.
// Se o curso da matéria mudar, o novo curso precisa existir e ser da mesma
// academia.
func (r *MateriasRepo) Atualizar(ctx context.Context, audit db.AuditContext, id uuid.UUID, acao, motivo string, fn func(*Materia) error) (*Materia, error) {
	var resultado *Materia
	err := r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		m, err := carregarMateriaTx(ctx, tx, id)
		if err != nil {
			return err
		}
		antes := m.snapshot()
		cursoAntes := m.CursoID
		if err := fn(m); err != nil {
			return err
		}
		if m.CursoID != nil && (cursoAntes == nil || *cursoAntes != *m.CursoID) {
			if err := exigirCursoDaAcademia(ctx, tx, *m.CursoID, m.CodigoAcademia); err != nil {
				return err
			}
		}
		if err := gravarMateriaTx(ctx, tx, m); err != nil {
			return fmt.Errorf("atualizar matéria: %w", err)
		}
		resultado = m
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeMateria, EntidadeID: m.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: acao, Antes: antes, Depois: m.snapshot(), Motivo: motivo,
		})
	})
	if err != nil {
		return nil, err
	}
	return resultado, nil
}
