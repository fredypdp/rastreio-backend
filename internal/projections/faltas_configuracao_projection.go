package projections

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"spuri/internal/db"
)

// FaltasConfiguracaoProjection materializa a configuração de faltas por academia.
type FaltasConfiguracaoProjection struct{ client *db.Client }

func NewFaltasConfiguracaoProjection(c *db.Client) *FaltasConfiguracaoProjection {
	return &FaltasConfiguracaoProjection{c}
}

func (p *FaltasConfiguracaoProjection) Name() string { return "faltas_configuracao" }

func (p *FaltasConfiguracaoProjection) GetLastProcessedEventID() (int64, error) {
	var v int64
	err := p.client.DB().QueryRow(`SELECT last_processed_event_id FROM projection_checkpoints WHERE projection_name=$1`, p.Name()).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}

func (p *FaltasConfiguracaoProjection) UpdateCheckpoint(id int64) error {
	_, err := p.client.DB().Exec(`INSERT INTO projection_checkpoints (projection_name,last_processed_event_id,last_processed_at,events_processed) VALUES($1,$2,CURRENT_TIMESTAMP,1) ON CONFLICT(projection_name) DO UPDATE SET last_processed_event_id=$2,last_processed_at=CURRENT_TIMESTAMP,events_processed=projection_checkpoints.events_processed+1`, p.Name(), id)
	return err
}

func (p *FaltasConfiguracaoProjection) Handle(e db.Event) error {
	if e.AggregateType != "ConfiguracaoFaltas" {
		return nil
	}
	if e.EventType == "ConfiguracaoFaltasDefinida" {
		return p.definida(e)
	}
	return nil
}

func (p *FaltasConfiguracaoProjection) Rebuild() error {
	if _, err := p.client.DB().Exec(`TRUNCATE projection_faltas_configuracao`); err != nil {
		return err
	}
	rows, err := p.client.DB().Query(`SELECT id,event_id,aggregate_id,aggregate_type,event_type,event_version,payload,metadata,occurred_at,recorded_at,ledger_hash,previous_hash FROM spuri_ledger WHERE aggregate_type='ConfiguracaoFaltas' ORDER BY id`)
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

// ConfiguracaoFaltasDTO é a configuração de faltas de uma academia.
type ConfiguracaoFaltasDTO struct {
	CodigoAcademia         string     `json:"codigo_academia"`
	LimiteFaltasPorPeriodo *int       `json:"limite_faltas_por_periodo"`
	ReprovacaoPorFaltas    bool       `json:"reprovacao_por_faltas"`
	AtualizadoEm           *time.Time `json:"atualizado_em,omitempty"`
}

func (p *FaltasConfiguracaoProjection) definida(e db.Event) error {
	var x struct {
		CodigoAcademia         string
		LimiteFaltasPorPeriodo *int
		ReprovacaoPorFaltas    bool
		DefinidoPor            uuid.UUID
		DefinidoEm             time.Time
	}
	if err := json.Unmarshal(e.Payload, &x); err != nil {
		return err
	}
	_, err := p.client.DB().Exec(`
		INSERT INTO projection_faltas_configuracao
			(codigo_academia, limite_faltas_por_periodo, reprovacao_por_faltas, atualizado_por, atualizado_em, version, last_event_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (codigo_academia) DO UPDATE SET
			limite_faltas_por_periodo = EXCLUDED.limite_faltas_por_periodo,
			reprovacao_por_faltas     = EXCLUDED.reprovacao_por_faltas,
			atualizado_por            = EXCLUDED.atualizado_por,
			atualizado_em             = EXCLUDED.atualizado_em,
			version                   = EXCLUDED.version,
			last_event_id             = EXCLUDED.last_event_id
	`, x.CodigoAcademia, x.LimiteFaltasPorPeriodo, x.ReprovacaoPorFaltas, x.DefinidoPor, x.DefinidoEm, e.EventVersion, e.EventID)
	return err
}

// GetByAcademia devolve a configuração da academia, ou nil se ela nunca definiu uma.
func (p *FaltasConfiguracaoProjection) GetByAcademia(codigoAcademia string) (*ConfiguracaoFaltasDTO, error) {
	var d ConfiguracaoFaltasDTO
	var limite sql.NullInt64
	var em sql.NullTime
	err := p.client.DB().QueryRow(`SELECT codigo_academia, limite_faltas_por_periodo, reprovacao_por_faltas, atualizado_em FROM projection_faltas_configuracao WHERE codigo_academia=$1`, codigoAcademia).
		Scan(&d.CodigoAcademia, &limite, &d.ReprovacaoPorFaltas, &em)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if limite.Valid {
		v := int(limite.Int64)
		d.LimiteFaltasPorPeriodo = &v
	}
	if em.Valid {
		t := em.Time
		d.AtualizadoEm = &t
	}
	return &d, nil
}
