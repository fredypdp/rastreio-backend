package projections

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"spuri/internal/db"

	"github.com/google/uuid"
)

// MateriaDTO — projeção lida do banco.
type MateriaDTO struct {
	ID                      uuid.UUID  `json:"id"`
	Nome                    string     `json:"nome"`
	Type                    string     `json:"type"`
	AnosAcademicos          []string   `json:"anos_academicos,omitempty"`
	Periodo                 *string    `json:"periodo,omitempty"`
	PendenciaPermitida      bool       `json:"pendencia_permitida"`
	PendenciaNivelConclusao *string    `json:"pendencia_nivel_conclusao,omitempty"`
	CodigoAcademia          string     `json:"codigo_academia"`
	CursoID                 *uuid.UUID `json:"curso_id,omitempty"`
	Status                  string     `json:"status"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	Version                 int        `json:"version"`
}

func (m MateriaDTO) MarshalJSON() ([]byte, error) {
	type materiaDTOAlias MateriaDTO
	if m.Type == "superior" {
		return json.Marshal(materiaDTOAlias(m))
	}
	type materiaEscolarDTO struct {
		ID             uuid.UUID  `json:"id"`
		Nome           string     `json:"nome"`
		Type           string     `json:"type"`
		AnosAcademicos []string   `json:"anos_academicos,omitempty"`
		Periodo        *string    `json:"periodo,omitempty"`
		CodigoAcademia string     `json:"codigo_academia"`
		CursoID        *uuid.UUID `json:"curso_id,omitempty"`
		Status         string     `json:"status"`
		CreatedAt      time.Time  `json:"created_at"`
		UpdatedAt      time.Time  `json:"updated_at"`
		Version        int        `json:"version"`
	}
	return json.Marshal(materiaEscolarDTO{
		ID: m.ID, Nome: m.Nome, Type: m.Type, AnosAcademicos: m.AnosAcademicos, Periodo: m.Periodo,
		CodigoAcademia: m.CodigoAcademia, CursoID: m.CursoID, Status: m.Status, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt, Version: m.Version,
	})
}

type MateriasProjection struct {
	client *db.Client
}

func NewMateriasProjection(client *db.Client) *MateriasProjection {
	return &MateriasProjection{client: client}
}

// Matérias são CRUD (Tarefa 133, Parte 2B): a tabela projection_materias é o
// registro oficial, gravada por internal/cadastros com auditoria em
// auditoria_crud. Esta projeção deixou de existir como projeção: não consome
// eventos nem é reconstruída; fica só a leitura que os demais módulos usam.

func (p *MateriasProjection) GetByID(id uuid.UUID) (*MateriaDTO, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("UUID inválido")
	}
	row := p.client.DB().QueryRow(`
		SELECT id, nome, type, anos_academicos, periodo, pendencia_permitida, pendencia_nivel_conclusao, codigo_academia, curso_id, status, created_at, updated_at, version
		FROM projection_materias WHERE id = $1
	`, id)
	return scanMateria(row)
}

func (p *MateriasProjection) GetByAcademia(codigoAcademia string) ([]MateriaDTO, error) {
	rows, err := p.client.DB().Query(`
		SELECT id, nome, type, anos_academicos, periodo, pendencia_permitida, pendencia_nivel_conclusao, codigo_academia, curso_id, status, created_at, updated_at, version
		FROM projection_materias
		WHERE codigo_academia = $1
			AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, codigoAcademia)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result, err := scanMaterias(rows)
	log.Printf("[DEBUG] %d matérias encontradas para academia %s", len(result), codigoAcademia)
	return result, err
}

func (p *MateriasProjection) GetByCurso(cursoID uuid.UUID) ([]MateriaDTO, error) {
	rows, err := p.client.DB().Query(`
		SELECT id, nome, type, anos_academicos, periodo, pendencia_permitida, pendencia_nivel_conclusao, codigo_academia, curso_id, status, created_at, updated_at, version
		FROM projection_materias
		WHERE curso_id = $1
			AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, cursoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMaterias(rows)
}

func (p *MateriasProjection) GetActiveByAcademia(codigoAcademia string) ([]MateriaDTO, error) {
	rows, err := p.client.DB().Query(`
		SELECT id, nome, type, anos_academicos, periodo, pendencia_permitida, pendencia_nivel_conclusao, codigo_academia, curso_id, status, created_at, updated_at, version
		FROM projection_materias
		WHERE codigo_academia = $1 AND status = 'ativo' AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, codigoAcademia)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMaterias(rows)
}

func scanMateria(row *sql.Row) (*MateriaDTO, error) {
	var dto MateriaDTO
	var anosJSON sql.NullString
	var cursoID sql.NullString
	var periodo sql.NullString
	var pendenciaNivelConclusao sql.NullString
	err := row.Scan(
		&dto.ID, &dto.Nome, &dto.Type, &anosJSON, &periodo, &dto.PendenciaPermitida, &pendenciaNivelConclusao,
		&dto.CodigoAcademia, &cursoID, &dto.Status,
		&dto.CreatedAt, &dto.UpdatedAt, &dto.Version,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if anosJSON.Valid && anosJSON.String != "" {
		json.Unmarshal([]byte(anosJSON.String), &dto.AnosAcademicos)
	}
	if cursoID.Valid {
		cid, _ := uuid.Parse(cursoID.String)
		dto.CursoID = &cid
	}
	if periodo.Valid {
		dto.Periodo = &periodo.String
	}
	if pendenciaNivelConclusao.Valid {
		dto.PendenciaNivelConclusao = &pendenciaNivelConclusao.String
	}
	return &dto, nil
}

func scanMaterias(rows *sql.Rows) ([]MateriaDTO, error) {
	var result []MateriaDTO
	for rows.Next() {
		var dto MateriaDTO
		var anosJSON sql.NullString
		var cursoID sql.NullString
		var periodo sql.NullString
		var pendenciaNivelConclusao sql.NullString
		if err := rows.Scan(
			&dto.ID, &dto.Nome, &dto.Type, &anosJSON, &periodo, &dto.PendenciaPermitida, &pendenciaNivelConclusao,
			&dto.CodigoAcademia, &cursoID, &dto.Status,
			&dto.CreatedAt, &dto.UpdatedAt, &dto.Version,
		); err != nil {
			continue
		}
		if anosJSON.Valid && anosJSON.String != "" {
			json.Unmarshal([]byte(anosJSON.String), &dto.AnosAcademicos)
		}
		if cursoID.Valid {
			cid, _ := uuid.Parse(cursoID.String)
			dto.CursoID = &cid
		}
		if periodo.Valid {
			dto.Periodo = &periodo.String
		}
		if pendenciaNivelConclusao.Valid {
			dto.PendenciaNivelConclusao = &pendenciaNivelConclusao.String
		}
		result = append(result, dto)
	}
	return result, rows.Err()
}

// nullOrString2 — helper legado para compatibilidade (não mais usado nos handlers)
func nullOrString2(b []byte) string {
	if len(b) == 0 || string(b) == "null" {
		return "NULL"
	}
	value := string(b)
	if !db.SafeString(value) {
		return "NULL"
	}
	return fmt.Sprintf("'%s'", value)
}
