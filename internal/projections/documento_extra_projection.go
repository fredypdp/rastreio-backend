package projections

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"spuri/internal/db"
)

type DocumentoExtraProjection struct{ client *db.Client }

func NewDocumentoExtraProjection(c *db.Client) *DocumentoExtraProjection {
	return &DocumentoExtraProjection{c}
}
func (p *DocumentoExtraProjection) Name() string { return "documentos_extra" }
func (p *DocumentoExtraProjection) GetLastProcessedEventID() (int64, error) {
	var v int64
	err := p.client.DB().QueryRow(`SELECT last_processed_event_id FROM projection_checkpoints WHERE projection_name=$1`, p.Name()).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}
func (p *DocumentoExtraProjection) UpdateCheckpoint(id int64) error {
	_, err := p.client.DB().Exec(`INSERT INTO projection_checkpoints (projection_name,last_processed_event_id,last_processed_at,events_processed) VALUES($1,$2,CURRENT_TIMESTAMP,1) ON CONFLICT(projection_name) DO UPDATE SET last_processed_event_id=$2,last_processed_at=CURRENT_TIMESTAMP,events_processed=projection_checkpoints.events_processed+1`, p.Name(), id)
	return err
}
func (p *DocumentoExtraProjection) Handle(e db.Event) error {
	if e.AggregateType != "DocumentoExtra" {
		return nil
	}
	switch e.EventType {
	case "DocumentoExtraCriado":
		return p.created(e)
	case "DocumentoExtraAtualizado":
		return p.updated(e)
	case "DocumentoExtraDesativado":
		return p.active(e, false)
	case "DocumentoExtraReativado":
		return p.active(e, true)
	}
	return nil
}
func (p *DocumentoExtraProjection) Rebuild() error {
	if _, err := p.client.DB().Exec(`TRUNCATE projection_documentos_extra CASCADE`); err != nil {
		return err
	}
	rows, err := p.client.DB().Query(`SELECT id,event_id,aggregate_id,aggregate_type,event_type,event_version,payload,metadata,occurred_at,recorded_at,ledger_hash,previous_hash FROM spuri_ledger WHERE aggregate_type='DocumentoExtra' ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var e db.Event
		var prev sql.NullString
		if err = rows.Scan(&e.ID, &e.EventID, &e.AggregateID, &e.AggregateType, &e.EventType, &e.EventVersion, &e.Payload, &e.Metadata, &e.OccurredAt, &e.RecordedAt, &e.LedgerHash, &prev); err != nil {
			return err
		}
		if prev.Valid {
			e.PreviousHash = &prev.String
		}
		if err = p.Handle(e); err != nil {
			return err
		}
	}
	return rows.Err()
}

type DocumentoExtraDTO struct {
	ID             uuid.UUID `json:"id"`
	CodigoAcademia string    `json:"codigo_academia"`
	Rotulo         string    `json:"rotulo"`
	Tipo           string    `json:"tipo"`
	Obrigatorio    bool      `json:"obrigatorio"`
	AnosAcademicos []string  `json:"anos_academicos"`
	Ativo          bool      `json:"ativo"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (p *DocumentoExtraProjection) created(e db.Event) error {
	var x struct {
		CodigoAcademia string
		Rotulo         string
		Tipo           string
		Obrigatorio    bool
		AnosAcademicos []string
		CriadoPor      uuid.UUID
		CreatedAt      time.Time
	}
	if err := json.Unmarshal(e.Payload, &x); err != nil {
		return err
	}
	anosJSON, err := json.Marshal(x.AnosAcademicos)
	if err != nil {
		return err
	}
	_, err = p.client.DB().Exec(
		`INSERT INTO projection_documentos_extra(id,codigo_academia,rotulo,tipo,obrigatorio,anos_academicos,ativo,criado_por,created_at,updated_at,version,last_event_id)
		 VALUES($1,$2,$3,$4,$5,$6,true,$7,$8,$8,$9,$10) ON CONFLICT(id) DO NOTHING`,
		e.AggregateID, x.CodigoAcademia, x.Rotulo, x.Tipo, x.Obrigatorio, anosJSON, x.CriadoPor, x.CreatedAt, e.EventVersion, e.EventID)
	return err
}
func (p *DocumentoExtraProjection) updated(e db.Event) error {
	var x struct {
		Rotulo         string
		Tipo           string
		Obrigatorio    bool
		AnosAcademicos []string
	}
	if err := json.Unmarshal(e.Payload, &x); err != nil {
		return err
	}
	anosJSON, err := json.Marshal(x.AnosAcademicos)
	if err != nil {
		return err
	}
	_, err = p.client.DB().Exec(
		`UPDATE projection_documentos_extra SET rotulo=$1,tipo=$2,obrigatorio=$3,anos_academicos=$4,version=$5,last_event_id=$6,updated_at=CURRENT_TIMESTAMP WHERE id=$7`,
		x.Rotulo, x.Tipo, x.Obrigatorio, anosJSON, e.EventVersion, e.EventID, e.AggregateID)
	return err
}
func (p *DocumentoExtraProjection) active(e db.Event, ativo bool) error {
	_, err := p.client.DB().Exec(`UPDATE projection_documentos_extra SET ativo=$1,version=$2,last_event_id=$3,updated_at=CURRENT_TIMESTAMP WHERE id=$4`, ativo, e.EventVersion, e.EventID, e.AggregateID)
	return err
}
func (p *DocumentoExtraProjection) scan(row interface{ Scan(...interface{}) error }) (*DocumentoExtraDTO, error) {
	var d DocumentoExtraDTO
	var anosJSON []byte
	err := row.Scan(&d.ID, &d.CodigoAcademia, &d.Rotulo, &d.Tipo, &d.Obrigatorio, &anosJSON, &d.Ativo, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if len(anosJSON) > 0 {
		if err := json.Unmarshal(anosJSON, &d.AnosAcademicos); err != nil {
			return nil, fmt.Errorf("decodificar anos_academicos: %w", err)
		}
	}
	return &d, nil
}

const documentoExtraCols = `id,codigo_academia,rotulo,tipo,obrigatorio,anos_academicos,ativo,created_at,updated_at`

func (p *DocumentoExtraProjection) GetByID(id uuid.UUID) (*DocumentoExtraDTO, error) {
	d, err := p.scan(p.client.DB().QueryRow(`SELECT `+documentoExtraCols+` FROM projection_documentos_extra WHERE id=$1`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}
func (p *DocumentoExtraProjection) GetByAcademia(codigo string, ativosOnly bool) ([]DocumentoExtraDTO, error) {
	q := `SELECT ` + documentoExtraCols + ` FROM projection_documentos_extra WHERE codigo_academia=$1`
	if ativosOnly {
		q += ` AND ativo=true`
	}
	q += ` ORDER BY rotulo`
	rows, err := p.client.DB().Query(q, codigo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocumentoExtraDTO{}
	for rows.Next() {
		d, err := p.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listar documentos extra: %w", err)
	}
	return out, nil
}

// GetAtivosPorAnoAcademico retorna as definições ATIVAS desta academia cujo
// anos_academicos CONTÉM o ano_academico informado — usado no cadastro
// direto e na solicitação de matrícula para saber quais documentos extra
// validar/exigir para o ano específico do estudante. Uma mesma definição
// pode aparecer para mais de um ano_academico distinto (ex.: uma definição
// com anos_academicos=["9_ano_fundamental","1_ano_medio"] aparece tanto na
// consulta para "9_ano_fundamental" quanto para "1_ano_medio").
//
// O operador @> (containment) usa o índice GIN idx_documentos_extra_anos_academicos
// — ver migration 127_documentos_extra_anos_academicos.sql.
func (p *DocumentoExtraProjection) GetAtivosPorAnoAcademico(codigo, anoAcademico string) ([]DocumentoExtraDTO, error) {
	anoJSON, err := json.Marshal([]string{anoAcademico})
	if err != nil {
		return nil, err
	}
	rows, err := p.client.DB().Query(
		`SELECT `+documentoExtraCols+` FROM projection_documentos_extra WHERE codigo_academia=$1 AND anos_academicos @> $2::jsonb AND ativo=true ORDER BY rotulo`,
		codigo, anoJSON)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocumentoExtraDTO{}
	for rows.Next() {
		d, err := p.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listar documentos extra por ano acadêmico: %w", err)
	}
	return out, nil
}
