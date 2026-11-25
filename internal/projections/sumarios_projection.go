package projections

import (
	"database/sql"
	"fmt"

	"spuri/internal/db"

	"github.com/google/uuid"
)

type SumarioDTO struct {
	ID             string  `json:"id"`
	CodigoAcademia string  `json:"codigo_academia"`
	SumarioTitulo  string  `json:"sumario_titulo"`
	Descricao      *string `json:"descricao,omitempty"`
	Periodo        string  `json:"periodo"`
	AnoAcademico   string  `json:"ano_academico"`
	Nivel          string  `json:"nivel"`
	Type           string  `json:"type"`
	CursoID        *string `json:"curso_id,omitempty"`
	MateriaID      string  `json:"materia_id"`
	CriadoPor      *string `json:"criado_por,omitempty"`
	Status         string  `json:"status"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	Version        int     `json:"version"`
}

type SumariosProjection struct {
	client *db.Client
}

func NewSumariosProjection(client *db.Client) *SumariosProjection {
	return &SumariosProjection{client: client}
}

// ============================================================================
// Leitura
// ============================================================================

func (p *SumariosProjection) GetByID(id uuid.UUID) (*SumarioDTO, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("UUID inválido")
	}
	row := p.client.DB().QueryRow(`
		SELECT id, codigo_academia, sumario_titulo, descricao, periodo, ano_academico,
			nivel, type, curso_id, materia_id, criado_por, status, created_at, updated_at, version
		FROM projection_sumarios WHERE id = $1
	`, id)
	return scanSumario(row)
}

func (p *SumariosProjection) GetByAcademia(codigoAcademia string, materiaID, periodo, anoAcademico *string) ([]SumarioDTO, error) {
	query := `
		SELECT id, codigo_academia, sumario_titulo, descricao, periodo, ano_academico,
			nivel, type, curso_id, materia_id, criado_por, status, created_at, updated_at, version
		FROM projection_sumarios
		WHERE codigo_academia = $1 AND deleted_at IS NULL
	`
	args := []interface{}{codigoAcademia}
	if materiaID != nil {
		args = append(args, *materiaID)
		query += fmt.Sprintf(" AND materia_id = $%d", len(args))
	}
	if periodo != nil {
		args = append(args, *periodo)
		query += fmt.Sprintf(" AND periodo = $%d", len(args))
	}
	if anoAcademico != nil {
		args = append(args, *anoAcademico)
		query += fmt.Sprintf(" AND ano_academico = $%d", len(args))
	}
	query += " ORDER BY created_at DESC"

	rows, err := p.client.DB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSumarios(rows)
}

func scanSumario(row *sql.Row) (*SumarioDTO, error) {
	var dto SumarioDTO
	var descricao, cursoID, criadoPor sql.NullString
	err := row.Scan(
		&dto.ID, &dto.CodigoAcademia, &dto.SumarioTitulo, &descricao, &dto.Periodo, &dto.AnoAcademico,
		&dto.Nivel, &dto.Type, &cursoID, &dto.MateriaID, &criadoPor, &dto.Status,
		&dto.CreatedAt, &dto.UpdatedAt, &dto.Version,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if descricao.Valid {
		dto.Descricao = &descricao.String
	}
	if cursoID.Valid {
		dto.CursoID = &cursoID.String
	}
	if criadoPor.Valid {
		dto.CriadoPor = &criadoPor.String
	}
	return &dto, nil
}

func scanSumarios(rows *sql.Rows) ([]SumarioDTO, error) {
	var result []SumarioDTO
	for rows.Next() {
		var dto SumarioDTO
		var descricao, cursoID, criadoPor sql.NullString
		if err := rows.Scan(
			&dto.ID, &dto.CodigoAcademia, &dto.SumarioTitulo, &descricao, &dto.Periodo, &dto.AnoAcademico,
			&dto.Nivel, &dto.Type, &cursoID, &dto.MateriaID, &criadoPor, &dto.Status,
			&dto.CreatedAt, &dto.UpdatedAt, &dto.Version,
		); err != nil {
			return nil, err
		}
		if descricao.Valid {
			dto.Descricao = &descricao.String
		}
		if cursoID.Valid {
			dto.CursoID = &cursoID.String
		}
		if criadoPor.Valid {
			dto.CriadoPor = &criadoPor.String
		}
		result = append(result, dto)
	}
	return result, rows.Err()
}
