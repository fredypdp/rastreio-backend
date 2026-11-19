package projections

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"spuri/internal/db"
)

// ConfiguracaoSistemaProjection materializa as configurações do sistema gravadas
// no banco (Parte 3 do Plano de Atualizações). Uma linha por chave; remover uma
// configuração marca definida = FALSE em vez de apagar a linha, e cada evento só
// vale se a versão dele for maior que a da linha. Assim Handle é idempotente e
// converge para o último estado mesmo quando o evento é aplicado duas vezes
// (na hora, pelo serviço, e depois pelo gerenciador de projeções) ou fora de ordem.
type ConfiguracaoSistemaProjection struct{ client *db.Client }

func NewConfiguracaoSistemaProjection(c *db.Client) *ConfiguracaoSistemaProjection {
	return &ConfiguracaoSistemaProjection{c}
}

func (p *ConfiguracaoSistemaProjection) Name() string { return "configuracao_sistema" }

func (p *ConfiguracaoSistemaProjection) GetLastProcessedEventID() (int64, error) {
	var v int64
	err := p.client.DB().QueryRow(`SELECT last_processed_event_id FROM projection_checkpoints WHERE projection_name=$1`, p.Name()).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}

func (p *ConfiguracaoSistemaProjection) UpdateCheckpoint(id int64) error {
	_, err := p.client.DB().Exec(`INSERT INTO projection_checkpoints (projection_name,last_processed_event_id,last_processed_at,events_processed) VALUES($1,$2,CURRENT_TIMESTAMP,1) ON CONFLICT(projection_name) DO UPDATE SET last_processed_event_id=$2,last_processed_at=CURRENT_TIMESTAMP,events_processed=projection_checkpoints.events_processed+1`, p.Name(), id)
	return err
}

func (p *ConfiguracaoSistemaProjection) Handle(e db.Event) error {
	if e.AggregateType != "ConfiguracaoSistema" {
		return nil
	}
	switch e.EventType {
	case "ConfiguracaoSistemaDefinida":
		return p.definida(e)
	case "ConfiguracaoSistemaRemovida":
		return p.removida(e)
	}
	return nil
}

func (p *ConfiguracaoSistemaProjection) Rebuild() error {
	if _, err := p.client.DB().Exec(`TRUNCATE projection_configuracao_sistema`); err != nil {
		return err
	}
	rows, err := p.client.DB().Query(`SELECT id,event_id,aggregate_id,aggregate_type,event_type,event_version,payload,metadata,occurred_at,recorded_at,ledger_hash,previous_hash FROM spuri_ledger WHERE aggregate_type='ConfiguracaoSistema' ORDER BY id`)
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

func (p *ConfiguracaoSistemaProjection) definida(e db.Event) error {
	var x struct {
		Chave       string
		Tipo        string
		Segredo     bool
		Valor       string
		DefinidoPor uuid.UUID
		DefinidoEm  time.Time
	}
	if err := json.Unmarshal(e.Payload, &x); err != nil {
		return err
	}
	var valor any
	if !x.Segredo {
		valor = x.Valor
	}
	return p.gravar(x.Chave, x.Tipo, x.Segredo, true, valor, x.DefinidoPor, x.DefinidoEm, e)
}

func (p *ConfiguracaoSistemaProjection) removida(e db.Event) error {
	var x struct {
		Chave       string
		Tipo        string
		Segredo     bool
		RemovidoPor uuid.UUID
		RemovidoEm  time.Time
	}
	if err := json.Unmarshal(e.Payload, &x); err != nil {
		return err
	}
	return p.gravar(x.Chave, x.Tipo, x.Segredo, false, nil, x.RemovidoPor, x.RemovidoEm, e)
}

func (p *ConfiguracaoSistemaProjection) gravar(chave, tipo string, segredo, definida bool, valor any, por uuid.UUID, em time.Time, e db.Event) error {
	_, err := p.client.DB().Exec(`
		INSERT INTO projection_configuracao_sistema
			(chave, tipo, segredo, definida, valor, atualizado_por, atualizado_em, version, last_event_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (chave) DO UPDATE SET
			tipo           = EXCLUDED.tipo,
			segredo        = EXCLUDED.segredo,
			definida       = EXCLUDED.definida,
			valor          = EXCLUDED.valor,
			atualizado_por = EXCLUDED.atualizado_por,
			atualizado_em  = EXCLUDED.atualizado_em,
			version        = EXCLUDED.version,
			last_event_id  = EXCLUDED.last_event_id
		WHERE projection_configuracao_sistema.version < EXCLUDED.version`,
		chave, tipo, segredo, definida, valor, por, em, e.EventVersion, e.EventID)
	return err
}

// ConfiguracaoSistemaLinha é uma configuração gravada no banco, sem segredo
// (o valor de segredo nem passa por esta projeção).
type ConfiguracaoSistemaLinha struct {
	Chave        string
	Definida     bool
	Valor        *string
	AtualizadoEm time.Time
	PorID        *uuid.UUID
	PorNome      *string
}

// Listar devolve as linhas da projeção por chave, com o nome de quem alterou.
func (p *ConfiguracaoSistemaProjection) Listar() (map[string]ConfiguracaoSistemaLinha, error) {
	rows, err := p.client.DB().Query(`
		SELECT c.chave, c.definida, c.valor, c.atualizado_em, c.atualizado_por, a.nome
		FROM projection_configuracao_sistema c
		LEFT JOIN projection_admins a ON a.id = c.atualizado_por`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ConfiguracaoSistemaLinha{}
	for rows.Next() {
		var l ConfiguracaoSistemaLinha
		var valor, nome sql.NullString
		var por uuid.NullUUID
		if err := rows.Scan(&l.Chave, &l.Definida, &valor, &l.AtualizadoEm, &por, &nome); err != nil {
			return nil, err
		}
		if valor.Valid {
			l.Valor = &valor.String
		}
		if por.Valid {
			l.PorID = &por.UUID
		}
		if nome.Valid {
			l.PorNome = &nome.String
		}
		out[l.Chave] = l
	}
	return out, rows.Err()
}
