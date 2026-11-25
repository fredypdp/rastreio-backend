package cadastros

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
)

const EntidadeSumario = "Sumario"

// Sumario é o registro de um sumário de aula. Era o agregado Sumario, em
// event sourcing; passou a CRUD na Tarefa 134 (Parte 2B, regra 17). A tabela
// de registro é projection_sumarios.
type Sumario struct {
	ID             uuid.UUID
	CodigoAcademia string
	SumarioTitulo  string
	Descricao      *string
	Periodo        string
	AnoAcademico   string
	Nivel          string
	Type           string // "escolar" ou "superior"
	CursoID        *uuid.UUID
	MateriaID      uuid.UUID
	CriadoPor      uuid.UUID
	Status         string // "ativo" ou "deletado"
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Version        int
	DeletedAt      *time.Time
}

// ============================================================================
// Regras (funções puras)
// ============================================================================

func validarTituloSumario(t string) (string, error) {
	t = strings.TrimSpace(t)
	if n := len(t); n < 3 || n > 200 {
		return "", regra("sumario_titulo deve ter entre 3 e 200 caracteres")
	}
	return t, nil
}

func validarDescricaoSumario(d *string) (*string, error) {
	if d == nil {
		return nil, nil
	}
	x := strings.TrimSpace(*d)
	if len(x) > 2000 {
		return nil, regra("descricao deve ter no máximo 2000 caracteres")
	}
	return &x, nil
}

// NovoSumario valida e monta um sumário novo, ativo.
func NovoSumario(titulo string, descricao *string, codigoAcademia, tipo, nivel, periodo, anoAcademico string,
	cursoID *uuid.UUID, materiaID, criadoPor uuid.UUID) (*Sumario, error) {
	titulo, err := validarTituloSumario(titulo)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(codigoAcademia) == "" {
		return nil, regra("codigo_academia é obrigatório")
	}
	if tipo != aggregates.TipoEscolar && tipo != aggregates.TipoSuperior {
		return nil, regra("type deve ser 'escolar' ou 'superior'")
	}
	if nivel != "fundamental" && nivel != "medio" && nivel != "superior" {
		return nil, regra("nivel deve ser 'fundamental', 'medio' ou 'superior'")
	}
	esperado := aggregates.TipoSuperior
	if nivel != "superior" {
		esperado = aggregates.TipoEscolar
	}
	if tipo != esperado {
		return nil, regra("type (%s) incoerente com nivel (%s)", tipo, nivel)
	}
	if strings.TrimSpace(periodo) == "" || strings.TrimSpace(anoAcademico) == "" {
		return nil, regra("periodo e ano_academico são obrigatórios")
	}
	if nivel == "fundamental" && cursoID != nil {
		return nil, regra("sumário de matéria fundamental não deve ter curso_id")
	}
	if nivel != "fundamental" && cursoID == nil {
		return nil, regra("sumário de matéria %s exige curso_id", nivel)
	}
	if materiaID == uuid.Nil || criadoPor == uuid.Nil {
		return nil, regra("materia_id e criado_por são obrigatórios")
	}
	descricao, err = validarDescricaoSumario(descricao)
	if err != nil {
		return nil, err
	}
	return &Sumario{
		ID: uuid.New(), CodigoAcademia: codigoAcademia, SumarioTitulo: titulo, Descricao: descricao,
		Periodo: periodo, AnoAcademico: anoAcademico, Nivel: nivel, Type: tipo, CursoID: cursoID,
		MateriaID: materiaID, CriadoPor: criadoPor, Status: StatusAtivo,
	}, nil
}

// AtualizarDados altera título e/ou descrição. nil não altera o campo.
func (s *Sumario) AtualizarDados(titulo, descricao *string) error {
	if s.Status == StatusDeletado {
		return regra("não é possível atualizar um sumário deletado")
	}
	if titulo == nil && descricao == nil {
		return regra("nenhum dado para atualizar")
	}
	var novoTitulo string
	if titulo != nil {
		t, err := validarTituloSumario(*titulo)
		if err != nil {
			return err
		}
		novoTitulo = t
	}
	novaDescricao, err := validarDescricaoSumario(descricao)
	if err != nil {
		return err
	}
	if titulo != nil {
		s.SumarioTitulo = novoTitulo
	}
	if descricao != nil {
		s.Descricao = novaDescricao
	}
	return nil
}

// Deletar marca o sumário como deletado (exclusão lógica). Nunca é bloqueado
// por faltas vinculadas: projection_faltas guarda o título do momento.
func (s *Sumario) Deletar() error {
	if s.Status == StatusDeletado {
		return regra("sumário já está deletado")
	}
	agora := time.Now().UTC()
	s.Status = StatusDeletado
	s.DeletedAt = &agora
	return nil
}

// ============================================================================
// Persistência
// ============================================================================

// SumariosRepo grava sumários na tabela de registro, com auditoria na mesma
// transação.
type SumariosRepo struct{ client *db.Client }

func NovoSumariosRepo(client *db.Client) *SumariosRepo { return &SumariosRepo{client: client} }

func (s *Sumario) snapshot() map[string]any {
	var desc, curso any
	if s.Descricao != nil {
		desc = *s.Descricao
	}
	if s.CursoID != nil {
		curso = s.CursoID.String()
	}
	return map[string]any{
		"codigo_academia": s.CodigoAcademia,
		"sumario_titulo":  s.SumarioTitulo,
		"descricao":       desc,
		"periodo":         s.Periodo,
		"ano_academico":   s.AnoAcademico,
		"nivel":           s.Nivel,
		"type":            s.Type,
		"curso_id":        curso,
		"materia_id":      s.MateriaID.String(),
		"status":          s.Status,
		"version":         s.Version,
	}
}

// Criar insere o sumário e audita ("SumarioCriado"). A academia precisa existir
// (sem chave estrangeira: ver migration 144).
func (r *SumariosRepo) Criar(ctx context.Context, audit db.AuditContext, s *Sumario) error {
	return r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		if err := exigirAcademia(ctx, tx, s.CodigoAcademia); err != nil {
			return err
		}
		var curso any
		if s.CursoID != nil {
			curso = s.CursoID.String()
		}
		err := tx.QueryRowxContext(ctx, `
			INSERT INTO projection_sumarios
				(id, codigo_academia, sumario_titulo, descricao, periodo, ano_academico,
				 nivel, type, curso_id, materia_id, criado_por, status, version)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,1)
			RETURNING created_at, updated_at, version`,
			s.ID, s.CodigoAcademia, s.SumarioTitulo, s.Descricao, s.Periodo, s.AnoAcademico,
			s.Nivel, s.Type, curso, s.MateriaID, s.CriadoPor, s.Status,
		).Scan(&s.CreatedAt, &s.UpdatedAt, &s.Version)
		if err != nil {
			return fmt.Errorf("inserir sumário: %w", err)
		}
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeSumario, EntidadeID: s.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "SumarioCriado", Depois: s.snapshot(),
		})
	})
}

func carregarSumarioTx(ctx context.Context, tx *sqlx.Tx, id uuid.UUID) (*Sumario, error) {
	var (
		s       Sumario
		desc    sql.NullString
		curso   uuid.NullUUID
		deleted sql.NullTime
	)
	err := tx.QueryRowxContext(ctx, `
		SELECT id, codigo_academia, sumario_titulo, descricao, periodo, ano_academico, nivel, type,
		       curso_id, materia_id, COALESCE(criado_por, '00000000-0000-0000-0000-000000000000'::uuid),
		       status, created_at, updated_at, version, deleted_at
		FROM projection_sumarios WHERE id = $1 FOR UPDATE`, id).Scan(
		&s.ID, &s.CodigoAcademia, &s.SumarioTitulo, &desc, &s.Periodo, &s.AnoAcademico, &s.Nivel, &s.Type,
		&curso, &s.MateriaID, &s.CriadoPor, &s.Status, &s.CreatedAt, &s.UpdatedAt, &s.Version, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNaoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("carregar sumário: %w", err)
	}
	if desc.Valid {
		d := desc.String
		s.Descricao = &d
	}
	if curso.Valid {
		c := curso.UUID
		s.CursoID = &c
	}
	if deleted.Valid {
		d := deleted.Time
		s.DeletedAt = &d
	}
	return &s, nil
}

// Atualizar carrega o sumário com bloqueio de linha, aplica fn, grava e
// audita na mesma transação.
func (r *SumariosRepo) Atualizar(ctx context.Context, audit db.AuditContext, id uuid.UUID, acao string, fn func(*Sumario) error) (*Sumario, error) {
	var resultado *Sumario
	err := r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		s, err := carregarSumarioTx(ctx, tx, id)
		if err != nil {
			return err
		}
		antes := s.snapshot()
		if err := fn(s); err != nil {
			return err
		}
		var deletedAt any
		if s.DeletedAt != nil {
			deletedAt = s.DeletedAt.UTC()
		}
		if err := tx.QueryRowxContext(ctx, `
			UPDATE projection_sumarios
			SET sumario_titulo = $2, descricao = $3, status = $4, deleted_at = $5,
			    version = version + 1, updated_at = CURRENT_TIMESTAMP
			WHERE id = $1 RETURNING updated_at, version`,
			s.ID, s.SumarioTitulo, s.Descricao, s.Status, deletedAt).Scan(&s.UpdatedAt, &s.Version); err != nil {
			return fmt.Errorf("atualizar sumário: %w", err)
		}
		resultado = s
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeSumario, EntidadeID: s.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: acao, Antes: antes, Depois: s.snapshot(),
		})
	})
	if err != nil {
		return nil, err
	}
	return resultado, nil
}
