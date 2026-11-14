package projections

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"spuri/internal/db"
	"strings"
	"time"

	"github.com/google/uuid"
)

// EstudanteVinculosProjection mantém o histórico completo de vínculos
// estudante↔academia para isolamento de academia (SEC-15).
type EstudanteVinculosProjection struct {
	client *db.Client
}

func NewEstudanteVinculosProjection(client *db.Client) *EstudanteVinculosProjection {
	return &EstudanteVinculosProjection{client: client}
}

func (p *EstudanteVinculosProjection) Name() string { return "estudante_vinculos" }

// ============================================================================
// Interface Projection
// ============================================================================

func (p *EstudanteVinculosProjection) GetLastProcessedEventID() (int64, error) {
	var lastID int64
	err := p.client.DB().QueryRow(
		`SELECT last_processed_event_id FROM projection_checkpoints WHERE projection_name = $1`,
		p.Name(),
	).Scan(&lastID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return lastID, err
}

func (p *EstudanteVinculosProjection) UpdateCheckpoint(eventID int64) error {
	eventID = int64(db.ValidateOffset(int(eventID)))
	_, err := p.client.DB().Exec(`
		INSERT INTO projection_checkpoints
			(projection_name, last_processed_event_id, last_processed_at, events_processed)
		VALUES ($1, $2, CURRENT_TIMESTAMP, 1)
		ON CONFLICT (projection_name) DO UPDATE SET
			last_processed_event_id = $2,
			last_processed_at       = CURRENT_TIMESTAMP,
			events_processed        = projection_checkpoints.events_processed + 1
	`, p.Name(), eventID)
	return err
}

func (p *EstudanteVinculosProjection) Handle(event db.Event) error {
	if event.AggregateType != "Estudante" {
		return nil
	}
	switch event.EventType {
	case "EstudanteCriadoComVinculo":
		return p.handleEstudanteCriadoComVinculo(event)
	case "EstudanteReintegrado":
		return p.handleEstudanteReintegrado(event)
	case "EstudanteDesvinculadoDaAcademia":
		return p.handleEstudanteDesvinculadoDaAcademia(event)
	}
	return nil
}

// ============================================================================
// Rebuild
// ============================================================================

func (p *EstudanteVinculosProjection) Rebuild() error {
	log.Printf("[DEBUG] [estudante_vinculos] Rebuild iniciado")
	if err := p.clear(); err != nil {
		return fmt.Errorf("falha ao limpar projection_estudante_vinculos: %w", err)
	}

	rows, err := p.client.DB().Query(`
		SELECT id, event_id, aggregate_id, aggregate_type, event_type,
			event_version, payload, metadata, occurred_at, recorded_at,
			ledger_hash, previous_hash
		FROM spuri_ledger
		WHERE aggregate_type = 'Estudante'
		  AND event_type IN ('EstudanteCriadoComVinculo', 'EstudanteReintegrado', 'EstudanteDesvinculadoDaAcademia')
		ORDER BY id ASC
	`)
	if err != nil {
		return fmt.Errorf("erro ao buscar eventos para rebuild: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var event db.Event
		var prevHash sql.NullString
		if err := rows.Scan(
			&event.ID, &event.EventID, &event.AggregateID, &event.AggregateType,
			&event.EventType, &event.EventVersion, &event.Payload, &event.Metadata,
			&event.OccurredAt, &event.RecordedAt, &event.LedgerHash, &prevHash,
		); err != nil {
			return fmt.Errorf("erro ao escanear evento %d: %w", count, err)
		}
		if prevHash.Valid {
			event.PreviousHash = &prevHash.String
		}
		if err := p.Handle(event); err != nil {
			return fmt.Errorf("erro ao processar evento %d (type=%s): %w", event.ID, event.EventType, err)
		}
		count++
	}

	log.Printf("[DEBUG] [estudante_vinculos] Rebuild concluído: %d eventos processados", count)
	return rows.Err()
}

func (p *EstudanteVinculosProjection) clear() error {
	_, err := p.client.DB().Exec(`DELETE FROM projection_estudante_vinculos`)
	return err
}

// ============================================================================
// Handlers de eventos
// ============================================================================

func (p *EstudanteVinculosProjection) handleEstudanteCriadoComVinculo(event db.Event) error {
	var payload struct {
		Nome                     string    `json:"Nome"`
		CodigoEstudante          string    `json:"CodigoEstudante"`
		CodigoAcademia           string    `json:"CodigoAcademia"`
		TipoEnsino               string    `json:"TipoEnsino"`
		AnoEscolar               *string   `json:"AnoEscolar"`
		AnoEscolarMedio          *string   `json:"AnoEscolarMedio"`
		AnoSuperior              *string   `json:"AnoSuperior"`
		SemestreAtual            *int      `json:"SemestreAtual"`
		StatusEscolarFundamental string    `json:"StatusEscolarFundamental"`
		StatusEscolarMedio       string    `json:"StatusEscolarMedio"`
		StatusSuperior           string    `json:"StatusSuperior"`
		CreatedAt                time.Time `json:"CreatedAt"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("handleEstudanteCriadoComVinculo: parse error: %w", err)
	}

	// Determinar ano_academico baseado no tipo de ensino
	anoAcademico := ""
	tipoEnsino := strings.ToLower(strings.TrimSpace(payload.TipoEnsino))
	switch tipoEnsino {
	case "fundamental":
		if payload.AnoEscolar != nil && *payload.AnoEscolar != "" {
			anoAcademico = *payload.AnoEscolar
		} else if payload.StatusEscolarFundamental != "" {
			anoAcademico = payload.StatusEscolarFundamental
		}
	case "medio":
		if payload.AnoEscolarMedio != nil && *payload.AnoEscolarMedio != "" {
			anoAcademico = *payload.AnoEscolarMedio
		} else if payload.StatusEscolarMedio != "" {
			anoAcademico = payload.StatusEscolarMedio
		}
	case "superior":
		if payload.AnoSuperior != nil && *payload.AnoSuperior != "" {
			anoAcademico = *payload.AnoSuperior
		} else if payload.SemestreAtual != nil {
			anoAcademico = fmt.Sprintf("%d_semestre", *payload.SemestreAtual)
		} else if payload.StatusSuperior != "" {
			anoAcademico = payload.StatusSuperior
		}
	}

	// Se ainda não temos ano_academico, usar tipo_ensino como fallback
	if anoAcademico == "" {
		anoAcademico = tipoEnsino
	}

	_, err := p.client.DB().Exec(`
		INSERT INTO projection_estudante_vinculos (
			estudante_id, codigo_estudante, codigo_academia, tipo_ensino,
			ano_academico, evento_tipo, evento_version, vinculado_em
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (codigo_estudante, codigo_academia) WHERE desvinculado_em IS NULL
		DO UPDATE SET
			estudante_id     = EXCLUDED.estudante_id,
			tipo_ensino      = EXCLUDED.tipo_ensino,
			ano_academico    = EXCLUDED.ano_academico,
			evento_tipo      = EXCLUDED.evento_tipo,
			evento_version   = EXCLUDED.evento_version,
			vinculado_em     = EXCLUDED.vinculado_em,
			desvinculado_em  = NULL,
			deleted_at       = NULL,
			deleted_by       = NULL
	`,
		event.AggregateID, payload.CodigoEstudante, payload.CodigoAcademia,
		tipoEnsino, anoAcademico, "EstudanteCriadoComVinculo", event.EventVersion, event.RecordedAt,
	)
	if err != nil {
		return fmt.Errorf("handleEstudanteCriadoComVinculo: exec error: %w", err)
	}

	log.Printf("[DEBUG] [estudante_vinculos] Vínculo criado: estudante=%s academia=%s tipo=%s ano=%s (evento %d)",
		payload.CodigoEstudante, payload.CodigoAcademia, tipoEnsino, anoAcademico, event.ID)
	return nil
}

func (p *EstudanteVinculosProjection) handleEstudanteReintegrado(event db.Event) error {
	var payload struct {
		CodigoAcademia  string    `json:"CodigoAcademia"`
		CodigoEstudante string    `json:"CodigoEstudante"`
		TipoEnsino      string    `json:"TipoEnsino"`
		AnoEscolar      *string   `json:"AnoEscolar"`
		AnoEscolarMedio *string   `json:"AnoEscolarMedio"`
		AnoSuperior     *string   `json:"AnoSuperior"`
		SemestreAtual   *int      `json:"SemestreAtual"`
		ReintegradoAt   time.Time `json:"ReintegradoAt"`
		ReintegradoPor  uuid.UUID `json:"ReintegradoPor"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("handleEstudanteReintegrado: parse error: %w", err)
	}

	// Determinar ano_academico baseado no tipo de ensino
	anoAcademico := ""
	tipoEnsino := strings.ToLower(strings.TrimSpace(payload.TipoEnsino))
	switch tipoEnsino {
	case "fundamental":
		if payload.AnoEscolar != nil && *payload.AnoEscolar != "" {
			anoAcademico = *payload.AnoEscolar
		}
	case "medio":
		if payload.AnoEscolarMedio != nil && *payload.AnoEscolarMedio != "" {
			anoAcademico = *payload.AnoEscolarMedio
		}
	case "superior":
		if payload.AnoSuperior != nil && *payload.AnoSuperior != "" {
			anoAcademico = *payload.AnoSuperior
		} else if payload.SemestreAtual != nil {
			anoAcademico = fmt.Sprintf("%d_semestre", *payload.SemestreAtual)
		}
	}
	if anoAcademico == "" {
		anoAcademico = tipoEnsino
	}

	// Primeiro, fechar vínculo anterior ativo para esta academia (se houver)
	_, err := p.client.DB().Exec(`
		UPDATE projection_estudante_vinculos
		SET desvinculado_em = $1, deleted_at = $1, deleted_by = $2
		WHERE codigo_estudante = $3 AND codigo_academia = $4 AND desvinculado_em IS NULL
	`, payload.ReintegradoAt, payload.ReintegradoPor, payload.CodigoEstudante, payload.CodigoAcademia)
	if err != nil {
		return fmt.Errorf("handleEstudanteReintegrado: erro ao fechar vínculo anterior: %w", err)
	}

	// Inserir novo vínculo ativo
	_, err = p.client.DB().Exec(`
		INSERT INTO projection_estudante_vinculos (
			estudante_id, codigo_estudante, codigo_academia, tipo_ensino,
			ano_academico, evento_tipo, evento_version, vinculado_em
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (codigo_estudante, codigo_academia) WHERE desvinculado_em IS NULL
		DO UPDATE SET
			estudante_id     = EXCLUDED.estudante_id,
			tipo_ensino      = EXCLUDED.tipo_ensino,
			ano_academico    = EXCLUDED.ano_academico,
			evento_tipo      = EXCLUDED.evento_tipo,
			evento_version   = EXCLUDED.evento_version,
			vinculado_em     = EXCLUDED.vinculado_em,
			desvinculado_em  = NULL,
			deleted_at       = NULL,
			deleted_by       = NULL
	`,
		event.AggregateID, payload.CodigoEstudante, payload.CodigoAcademia,
		tipoEnsino, anoAcademico, "EstudanteReintegrado", event.EventVersion, payload.ReintegradoAt,
	)
	if err != nil {
		return fmt.Errorf("handleEstudanteReintegrado: exec error: %w", err)
	}

	log.Printf("[DEBUG] [estudante_vinculos] Reintegração: estudante=%s academia=%s tipo=%s ano=%s (evento %d)",
		payload.CodigoEstudante, payload.CodigoAcademia, tipoEnsino, anoAcademico, event.ID)
	return nil
}

func (p *EstudanteVinculosProjection) handleEstudanteDesvinculadoDaAcademia(event db.Event) error {
	var payload struct {
		CodigoAcademia  string    `json:"CodigoAcademia"`
		CodigoEstudante string    `json:"CodigoEstudante"`
		DesvinculadoPor uuid.UUID `json:"DesvinculadoPor"`
		DesvinculadoAt  time.Time `json:"DesvinculadoAt"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("handleEstudanteDesvinculadoDaAcademia: parse error: %w", err)
	}

	_, err := p.client.DB().Exec(`
		UPDATE projection_estudante_vinculos
		SET desvinculado_em = $1, deleted_at = $1, deleted_by = $2
		WHERE codigo_estudante = $3 AND codigo_academia = $4 AND desvinculado_em IS NULL
	`, payload.DesvinculadoAt, payload.DesvinculadoPor, payload.CodigoEstudante, payload.CodigoAcademia)
	if err != nil {
		return fmt.Errorf("handleEstudanteDesvinculadoDaAcademia: exec error: %w", err)
	}

	log.Printf("[DEBUG] [estudante_vinculos] Desvinculação: estudante=%s academia=%s (evento %d)",
		payload.CodigoEstudante, payload.CodigoAcademia, event.ID)
	return nil
}

// ============================================================================
// DTOs de leitura
// ============================================================================

type EstudanteVinculoDTO struct {
	ID                 int64      `json:"id"`
	EstudanteID        uuid.UUID  `json:"estudante_id"`
	CodigoEstudante    string     `json:"codigo_estudante"`
	CodigoAcademia     string     `json:"codigo_academia"`
	TipoEnsino         string     `json:"tipo_ensino"`
	AnoAcademico       string     `json:"ano_academico"`
	EventoTipo         string     `json:"evento_tipo"`
	EventoVersion      int        `json:"evento_version"`
	VinculadoEm        time.Time  `json:"vinculado_em"`
	DesvinculadoEm     *time.Time `json:"desvinculado_em,omitempty"`
	DeletedAt          *time.Time `json:"deleted_at,omitempty"`
	DeletedBy          *uuid.UUID `json:"deleted_by,omitempty"`
	VinculoAtivo       bool       `json:"vinculo_ativo"`
}

func scanEstudanteVinculo(row *sql.Row) (*EstudanteVinculoDTO, error) {
	var v EstudanteVinculoDTO
	var desvinculadoEm, deletedAt sql.NullTime
	var deletedBy sql.NullString
	err := row.Scan(
		&v.ID, &v.EstudanteID, &v.CodigoEstudante, &v.CodigoAcademia,
		&v.TipoEnsino, &v.AnoAcademico, &v.EventoTipo, &v.EventoVersion,
		&v.VinculadoEm, &desvinculadoEm, &deletedAt, &deletedBy,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if desvinculadoEm.Valid {
		v.DesvinculadoEm = &desvinculadoEm.Time
	}
	if deletedAt.Valid {
		v.DeletedAt = &deletedAt.Time
	}
	if deletedBy.Valid {
		if id, err := uuid.Parse(deletedBy.String); err == nil {
			v.DeletedBy = &id
		}
	}
	v.VinculoAtivo = v.DesvinculadoEm == nil
	return &v, nil
}

func scanEstudanteVinculoRows(rows *sql.Rows) ([]EstudanteVinculoDTO, error) {
	var vinculos []EstudanteVinculoDTO
	for rows.Next() {
		var v EstudanteVinculoDTO
		var desvinculadoEm, deletedAt sql.NullTime
		var deletedBy sql.NullString
		if err := rows.Scan(
			&v.ID, &v.EstudanteID, &v.CodigoEstudante, &v.CodigoAcademia,
			&v.TipoEnsino, &v.AnoAcademico, &v.EventoTipo, &v.EventoVersion,
			&v.VinculadoEm, &desvinculadoEm, &deletedAt, &deletedBy,
		); err != nil {
			return nil, err
		}
		if desvinculadoEm.Valid {
			v.DesvinculadoEm = &desvinculadoEm.Time
		}
		if deletedAt.Valid {
			v.DeletedAt = &deletedAt.Time
		}
		if deletedBy.Valid {
			if id, err := uuid.Parse(deletedBy.String); err == nil {
				v.DeletedBy = &id
			}
		}
		v.VinculoAtivo = v.DesvinculadoEm == nil
		vinculos = append(vinculos, v)
	}
	return vinculos, rows.Err()
}

// GetByEstudante retorna todo o histórico de vínculos de um estudante.
func (p *EstudanteVinculosProjection) GetByEstudante(codigoEstudante string) ([]EstudanteVinculoDTO, error) {
	rows, err := p.client.DB().Query(`
		SELECT id, estudante_id, codigo_estudante, codigo_academia,
		       tipo_ensino, ano_academico, evento_tipo, evento_version,
		       vinculado_em, desvinculado_em, deleted_at, deleted_by
		FROM projection_estudante_vinculos
		WHERE codigo_estudante = $1
		ORDER BY vinculado_em DESC
	`, codigoEstudante)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEstudanteVinculoRows(rows)
}

// GetByAcademia retorna todos os vínculos (ativos e históricos) de uma academia.
func (p *EstudanteVinculosProjection) GetByAcademia(codigoAcademia string) ([]EstudanteVinculoDTO, error) {
	rows, err := p.client.DB().Query(`
		SELECT id, estudante_id, codigo_estudante, codigo_academia,
		       tipo_ensino, ano_academico, evento_tipo, evento_version,
		       vinculado_em, desvinculado_em, deleted_at, deleted_by
		FROM projection_estudante_vinculos
		WHERE codigo_academia = $1
		ORDER BY vinculado_em DESC
	`, codigoAcademia)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEstudanteVinculoRows(rows)
}

// TemVinculoAtivo verifica se existe vínculo ATIVO entre estudante e academia.
func (p *EstudanteVinculosProjection) TemVinculoAtivo(codigoEstudante, codigoAcademia string) (bool, error) {
	var exists bool
	err := p.client.DB().QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM projection_estudante_vinculos
			WHERE codigo_estudante = $1
			  AND codigo_academia = $2
			  AND desvinculado_em IS NULL
		)
	`, codigoEstudante, codigoAcademia).Scan(&exists)
	return exists, err
}

// TeveVinculoAlgumVer verifica se o estudante JÁ TEVE vínculo com a academia
// (ativo ou histórico). Usado para autorizar acesso a dados históricos.
func (p *EstudanteVinculosProjection) TeveVinculoAlgumVez(codigoEstudante, codigoAcademia string) (bool, error) {
	var exists bool
	err := p.client.DB().QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM projection_estudante_vinculos
			WHERE codigo_estudante = $1
			  AND codigo_academia = $2
		)
	`, codigoEstudante, codigoAcademia).Scan(&exists)
	return exists, err
}

// GetByEstudanteAcademia retorna os vínculos de um estudante com uma academia
// específica (ativo e histórico), ordenados por vinculado_em DESC.
// Usado pelo helper acessoAcademiaAoEstudante para determinar o tipo de acesso.
func (p *EstudanteVinculosProjection) GetByEstudanteAcademia(estudanteID uuid.UUID, codigoAcademia string) ([]EstudanteVinculoDTO, error) {
	rows, err := p.client.DB().Query(`
		SELECT id, estudante_id, codigo_estudante, codigo_academia,
		       tipo_ensino, ano_academico, evento_tipo, evento_version,
		       vinculado_em, desvinculado_em, deleted_at, deleted_by
		FROM projection_estudante_vinculos
		WHERE estudante_id = $1 AND codigo_academia = $2
		ORDER BY vinculado_em DESC
	`, estudanteID, codigoAcademia)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEstudanteVinculoRows(rows)
}