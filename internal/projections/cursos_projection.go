package projections

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"spuri/internal/db"
	"time"

	"github.com/google/uuid"
)

type CursosProjection struct {
	client *db.Client
}

func NewCursosProjection(client *db.Client) *CursosProjection {
	return &CursosProjection{client: client}
}

// Cursos são CRUD (Tarefa 133, Parte 2B): a tabela projection_cursos é o
// registro oficial, gravada por internal/cadastros com auditoria em
// auditoria_crud. Esta projeção deixou de existir como projeção: não consome
// eventos nem é reconstruída; fica só a leitura que os demais módulos usam.

type CursoDTO struct {
	ID             uuid.UUID `json:"id"`
	Nome           string    `json:"nome"`
	Type           string    `json:"type"`
	Modelo         string    `json:"modelo,omitempty"`
	AnosAcademicos []string  `json:"anos_academicos"`
	Periodos       []string  `json:"periodos"`
	CodigoAcademia string    `json:"codigo_academia"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Version        int       `json:"version"`
}

func (p *CursosProjection) GetByID(id uuid.UUID) (*CursoDTO, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("UUID inválido")
	}
	var dto CursoDTO
	var anosJSON []byte
	var periodosJSON []byte
	err := p.client.DB().QueryRow(`
		SELECT id, nome, type, COALESCE(modelo, '') AS modelo, anos_academicos, periodos, codigo_academia, status, created_at, updated_at, version
		FROM projection_cursos WHERE id = $1
	`, id).Scan(
		&dto.ID, &dto.Nome, &dto.Type, &dto.Modelo, &anosJSON, &periodosJSON,
		&dto.CodigoAcademia, &dto.Status, &dto.CreatedAt, &dto.UpdatedAt, &dto.Version,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal(anosJSON, &dto.AnosAcademicos)
	if periodosJSON != nil {
		json.Unmarshal(periodosJSON, &dto.Periodos)
	}
	if dto.Periodos == nil {
		dto.Periodos = []string{}
	}
	return &dto, nil
}

func (p *CursosProjection) GetByAcademia(codigoAcademia string) ([]CursoDTO, error) {
	rows, err := p.client.DB().Query(`
		SELECT id, nome, type, COALESCE(modelo, '') AS modelo, anos_academicos, periodos, codigo_academia, status, created_at, updated_at, version
		FROM projection_cursos
		WHERE codigo_academia = $1
			AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, codigoAcademia)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cursos []CursoDTO
	for rows.Next() {
		var dto CursoDTO
		var anosJSON []byte
		var periodosJSON []byte
		if err := rows.Scan(
			&dto.ID, &dto.Nome, &dto.Type, &dto.Modelo, &anosJSON, &periodosJSON,
			&dto.CodigoAcademia, &dto.Status, &dto.CreatedAt, &dto.UpdatedAt, &dto.Version,
		); err != nil {
			continue
		}
		json.Unmarshal(anosJSON, &dto.AnosAcademicos)
		if periodosJSON != nil {
			json.Unmarshal(periodosJSON, &dto.Periodos)
		}
		if dto.Periodos == nil {
			dto.Periodos = []string{}
		}
		cursos = append(cursos, dto)
	}
	log.Printf("[DEBUG] %d cursos encontrados para academia %s", len(cursos), codigoAcademia)
	return cursos, rows.Err()
}

func (p *CursosProjection) ListByCurso(cursoID uuid.UUID) ([]CursoDTO, error) {
	rows, err := p.client.DB().Query(`
		SELECT id, nome, type, COALESCE(modelo, '') AS modelo, anos_academicos, periodos, codigo_academia, status, created_at, updated_at, version
		FROM projection_cursos
		WHERE id = $1
			AND (deleted_at IS NULL OR status != 'deletado')
		ORDER BY created_at DESC
	`, cursoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cursos []CursoDTO
	for rows.Next() {
		var dto CursoDTO
		var anosJSON, periodosJSON []byte
		if err := rows.Scan(
			&dto.ID, &dto.Nome, &dto.Type, &dto.Modelo, &anosJSON, &periodosJSON,
			&dto.CodigoAcademia, &dto.Status, &dto.CreatedAt, &dto.UpdatedAt, &dto.Version,
		); err != nil {
			continue
		}
		json.Unmarshal(anosJSON, &dto.AnosAcademicos)
		if periodosJSON != nil {
			json.Unmarshal(periodosJSON, &dto.Periodos)
		}
		if dto.Periodos == nil {
			dto.Periodos = []string{}
		}
		cursos = append(cursos, dto)
	}
	return cursos, rows.Err()
}
