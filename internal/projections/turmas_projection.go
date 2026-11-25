package projections

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

type TurmasProjection struct {
	client *db.Client
}

func NewTurmasProjection(client *db.Client) *TurmasProjection {
	return &TurmasProjection{client: client}
}

func (p *TurmasProjection) Name() string { return "turmas" }

// ============================================================================
// Interface Projection
// ============================================================================

func (p *TurmasProjection) GetLastProcessedEventID() (int64, error) {
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

func (p *TurmasProjection) UpdateCheckpoint(eventID int64) error {
	eventID = int64(db.ValidateOffset(int(eventID)))
	_, err := p.client.DB().Exec(`
		INSERT INTO projection_checkpoints (projection_name, last_processed_event_id, last_processed_at, events_processed)
		VALUES ($1, $2, CURRENT_TIMESTAMP, 1)
		ON CONFLICT (projection_name) DO UPDATE SET
			last_processed_event_id = $2,
			last_processed_at = CURRENT_TIMESTAMP,
			events_processed = projection_checkpoints.events_processed + 1
	`, p.Name(), eventID)
	return err
}

// ============================================================================
// Efeito da avaliação final nas turmas
// ============================================================================
//
// As turmas são CRUD (Tarefa 134, regra 17): projection_turmas é a tabela de
// registro, não é mais reconstruída a partir do ledger. O que continua vindo do
// ledger é um EFEITO: quando um estudante é aprovado na avaliação final (evento
// que fica em event sourcing), ele sai das turmas do ano que terminou e entra
// numa turma do ano seguinte. Este "projetor" só aplica esse efeito, e agora o
// grava junto da auditoria (auditoria_crud), na mesma transação.
//
// Rebuild não repete o efeito: o registro já reflete o que foi aplicado, e
// repeti-lo desfaria correções feitas depois pela academia.

func (p *TurmasProjection) Handle(event db.Event) error {
	switch event.EventType {
	case "AvaliacaoFinalEscolar", "AvaliacaoFinalSuperior":
		log.Printf("[DEBUG] [turmas] Processando %s: %s", event.EventType, event.EventID)
		return p.handleAvaliacaoFinalAnoAcademico(event)
	}
	return nil
}

// Rebuild não faz nada: as turmas são CRUD e não vêm do ledger.
func (p *TurmasProjection) Rebuild() error {
	log.Printf("[DEBUG] [turmas] Rebuild ignorado: turmas são CRUD (Tarefa 134); nada a reconstruir")
	return nil
}

// atorDoEvento devolve quem agiu, segundo o metadata do evento; sem isso, o
// sistema.
func atorDoEvento(event db.Event) db.AuditContext {
	var md struct {
		UserID   string `json:"user_id"`
		UserType string `json:"user_type"`
		IP       string `json:"ip"`
	}
	_ = json.Unmarshal(event.Metadata, &md)
	if md.UserID == "" {
		return db.AuditContext{UserID: "sistema", UserType: "sistema"}
	}
	return db.AuditContext{UserID: md.UserID, UserType: md.UserType, IP: md.IP}
}

// academiaUUIDDoAtor devolve o id da academia quando quem agiu é a própria
// academia (a coluna academia_id da auditoria é UUID).
func academiaUUIDDoAtor(a db.AuditContext) string {
	if a.UserType == "academia" {
		if _, err := uuid.Parse(a.UserID); err == nil {
			return a.UserID
		}
	}
	return ""
}

func (p *TurmasProjection) handleAvaliacaoFinalAnoAcademico(event db.Event) error {
	var payload struct {
		CodigoEstudante        string   `json:"codigo_estudante"`
		CodigoAcademia         string   `json:"codigo_academia"`
		AnoLectivo             string   `json:"ano_lectivo"`
		ProximoAnoAcademico    *string  `json:"proximo_ano_academico"`
		CodigosTurmasRemovidas []string `json:"codigos_turmas_removidas"`
		Aprovado               bool     `json:"aprovado"`
		MotivoProgressao       *string  `json:"motivo_progressao"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("parse error AvaliacaoFinalAnoAcademico em turmas: %w", err)
	}
	if payload.CodigoEstudante == "" || event.EventType != "AvaliacaoFinalEscolar" ||
		!payload.Aprovado || len(payload.CodigosTurmasRemovidas) == 0 {
		return nil
	}

	ator := atorDoEvento(event)
	academia := academiaUUIDDoAtor(ator)
	motivo := fmt.Sprintf("avaliação final (evento %s)", event.EventID)
	return p.client.ComAuditoria(context.Background(), ator, func(tx *sqlx.Tx, aud *db.Auditor) error {
		ctx := context.Background()

		// 1) sai das turmas do ano que terminou (e fica no histórico do ano letivo).
		rows, err := tx.QueryxContext(ctx, `
			UPDATE projection_turmas
			SET estudantes = COALESCE(
				(
					SELECT jsonb_agg(val)
					FROM (SELECT jsonb_array_elements_text(estudantes::jsonb) AS val) sub
					WHERE val != $1
				),
				'[]'::jsonb
			),
			historico_estudantes_ano_letivo = CASE
				WHEN COALESCE(NULLIF($2, ''), '') = '' THEN historico_estudantes_ano_letivo
				ELSE jsonb_set(
					COALESCE(historico_estudantes_ano_letivo, '{}'::jsonb),
					ARRAY[$2]::text[],
					(
						SELECT to_jsonb(COALESCE(array_agg(DISTINCT v), ARRAY[]::text[]))
						FROM (
							SELECT jsonb_array_elements_text(
								COALESCE(historico_estudantes_ano_letivo -> $2, '[]'::jsonb)
							) AS v
							UNION ALL
							SELECT $1::text AS v
						) s
					),
					true
				)
			END,
			version = version + 1, updated_at = CURRENT_TIMESTAMP
			WHERE codigo_turma = ANY($3) AND codigo_academia = $4 AND deleted_at IS NULL
			RETURNING id`, payload.CodigoEstudante, payload.AnoLectivo,
			pq.Array(payload.CodigosTurmasRemovidas), payload.CodigoAcademia)
		if err != nil {
			return err
		}
		var removidas []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			removidas = append(removidas, id)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, id := range removidas {
			if err := aud.Registrar(ctx, db.EntradaAuditoria{
				EntidadeTipo: "Turma", EntidadeID: id.String(), AcademiaID: academia,
				Acao:   "EstudanteRemovidoDaTurmaPorAvaliacaoFinal",
				Depois: map[string]any{"codigo_estudante": payload.CodigoEstudante, "ano_letivo": payload.AnoLectivo, "codigo_academia": payload.CodigoAcademia},
				Motivo: motivo,
			}); err != nil {
				return err
			}
		}

		if payload.ProximoAnoAcademico == nil || *payload.ProximoAnoAcademico == "" {
			return nil
		}
		if payload.MotivoProgressao != nil && *payload.MotivoProgressao == "academia_sem_oferta_do_proximo_ano_academico_fundamental" {
			return nil
		}

		// 2) entra numa turma do ano seguinte (a mais compatível, balanceando a lotação).
		var destino uuid.UUID
		err = tx.QueryRowxContext(ctx, `
			WITH origem_info AS (
				SELECT t.codigo_turma, t.turno, t.curso_id
				FROM projection_turmas t
				WHERE t.codigo_turma = ANY($4)
				  AND t.codigo_academia = $1
				  AND t.deleted_at IS NULL
				ORDER BY t.codigo_turma ASC
				LIMIT 1
			),
			destinos_compativeis AS (
				SELECT t.id, t.codigo_turma,
					COALESCE(jsonb_array_length(COALESCE(t.estudantes::jsonb, '[]'::jsonb)), 0) AS qtd_estudantes
				FROM projection_turmas t
				JOIN origem_info o ON true
				WHERE t.codigo_academia = $1
				  AND t.nivel = $2
				  AND t.turno IS NOT DISTINCT FROM o.turno
				  AND t.curso_id IS NOT DISTINCT FROM o.curso_id
				  AND t.deleted_at IS NULL
			),
			destinos_gerais AS (
				SELECT t.id, t.codigo_turma,
					COALESCE(jsonb_array_length(COALESCE(t.estudantes::jsonb, '[]'::jsonb)), 0) AS qtd_estudantes
				FROM projection_turmas t
				WHERE t.codigo_academia = $1
				  AND t.nivel = $2
				  AND t.deleted_at IS NULL
			),
			destinos_priorizados AS (
				SELECT 1 AS prioridade, dc.id, dc.codigo_turma, dc.qtd_estudantes FROM destinos_compativeis dc
				UNION ALL
				SELECT 2 AS prioridade, dg.id, dg.codigo_turma, dg.qtd_estudantes
				FROM destinos_gerais dg
				WHERE NOT EXISTS (SELECT 1 FROM destinos_compativeis)
			),
			destinos_ranked AS (
				SELECT dp.id, dp.prioridade, dp.qtd_estudantes,
					ROW_NUMBER() OVER (PARTITION BY dp.prioridade ORDER BY dp.codigo_turma ASC) - 1 AS idx,
					COUNT(*) OVER (PARTITION BY dp.prioridade) AS total
				FROM destinos_priorizados dp
			),
			destino AS (
				SELECT dr.id
				FROM destinos_ranked dr
				CROSS JOIN origem_info o
				WHERE dr.total > 0
				  AND dr.idx = CASE
					WHEN dr.prioridade = 1 THEN ABS(hashtext(COALESCE(o.codigo_turma, $3))) % dr.total
					ELSE ABS(hashtext($3)) % dr.total
				  END
				ORDER BY dr.prioridade ASC, dr.qtd_estudantes ASC, dr.id ASC
				LIMIT 1
			)
			UPDATE projection_turmas t
			SET estudantes = CASE
					WHEN EXISTS (
						SELECT 1
						FROM jsonb_array_elements_text(COALESCE(t.estudantes::jsonb, '[]'::jsonb)) AS v(val)
						WHERE v.val = $3
					) THEN t.estudantes
					ELSE COALESCE(t.estudantes::jsonb, '[]'::jsonb) || to_jsonb($3::text)
				END,
				version = t.version + 1, updated_at = CURRENT_TIMESTAMP
			FROM destino d
			WHERE t.id = d.id
			RETURNING t.id`,
			payload.CodigoAcademia, *payload.ProximoAnoAcademico, payload.CodigoEstudante,
			pq.Array(payload.CodigosTurmasRemovidas)).Scan(&destino)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("nenhuma turma destino válida encontrada para estudante aprovado %s no nível %s", payload.CodigoEstudante, *payload.ProximoAnoAcademico)
		}
		if err != nil {
			return err
		}
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: "Turma", EntidadeID: destino.String(), AcademiaID: academia,
			Acao:   "EstudanteAdicionadoATurmaPorAvaliacaoFinal",
			Depois: map[string]any{"codigo_estudante": payload.CodigoEstudante, "proximo_ano_academico": *payload.ProximoAnoAcademico, "codigo_academia": payload.CodigoAcademia},
			Motivo: motivo,
		})
	})
}

// ============================================================================
// Queries de leitura
// ============================================================================

type TurmaDTO struct {
	ID                           uuid.UUID           `json:"id"`
	CodigoTurma                  string              `json:"codigo_turma"`
	CodigoAcademia               string              `json:"codigo_academia"`
	Nivel                        string              `json:"nivel"`
	CursoID                      *uuid.UUID          `json:"curso_id,omitempty"`
	Turno                        string              `json:"turno"`
	Estudantes                   []string            `json:"estudantes"`
	HistoricoEstudantesAnoLetivo map[string][]string `json:"historico_estudantes_ano_letivo"`
	Status                       string              `json:"status"`
	StatusAlteradoPor            *uuid.UUID          `json:"status_alterado_por,omitempty"`
	StatusAlteradoEm             *time.Time          `json:"status_alterado_em,omitempty"`
	CreatedAt                    time.Time           `json:"created_at"`
	UpdatedAt                    time.Time           `json:"updated_at"`
	Version                      int                 `json:"version"`
	// TipoAgrupamento é "grupo" no 4º ano médio e "turma" nos demais níveis.
	TipoAgrupamento string `json:"tipo_agrupamento"`
	// TemaTrabalho só é preenchido em grupos do 4º ano médio.
	TemaTrabalho *string `json:"tema_trabalho,omitempty"`
}

func (p *TurmasProjection) GetByID(id uuid.UUID) (*TurmaDTO, error) {
	row := p.client.DB().QueryRow(`
		SELECT id, codigo_turma, codigo_academia, nivel, curso_id, turno,
		       estudantes, historico_estudantes_ano_letivo, status, status_alterado_por, status_alterado_em,
		       created_at, updated_at, version, tema_trabalho
		FROM projection_turmas WHERE id = $1
	`, id)
	return scanTurmaRow(row)
}

func (p *TurmasProjection) GetByCodigoTurma(codigoTurma, codigoAcademia string) (*TurmaDTO, error) {
	row := p.client.DB().QueryRow(`
		SELECT id, codigo_turma, codigo_academia, nivel, curso_id, turno,
		       estudantes, historico_estudantes_ano_letivo, status, status_alterado_por, status_alterado_em,
		       created_at, updated_at, version, tema_trabalho
		FROM projection_turmas
		WHERE codigo_turma = $1 AND codigo_academia = $2
			AND deleted_at IS NULL
		LIMIT 1
	`, codigoTurma, codigoAcademia)
	return scanTurmaRow(row)
}

func (p *TurmasProjection) GetByAcademia(codigoAcademia string) ([]TurmaDTO, error) {
	rows, err := p.client.DB().Query(`
		SELECT id, codigo_turma, codigo_academia, nivel, curso_id, turno,
		       estudantes, historico_estudantes_ano_letivo, status, status_alterado_por, status_alterado_em,
		       created_at, updated_at, version, tema_trabalho
		FROM projection_turmas
		WHERE codigo_academia = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, codigoAcademia)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTurmas(rows)
}

// ListByAcademia é um alias de GetByAcademia — retorna ponteiros para compatibilidade com handlers.
func (p *TurmasProjection) ListByAcademia(codigoAcademia string) ([]*TurmaDTO, error) {
	turmas, err := p.GetByAcademia(codigoAcademia)
	if err != nil {
		return nil, err
	}
	result := make([]*TurmaDTO, len(turmas))
	for i := range turmas {
		result[i] = &turmas[i]
	}
	return result, nil
}

// ListByCurso retorna turmas vinculadas a um curso específico.
func (p *TurmasProjection) ListByCurso(cursoID uuid.UUID) ([]*TurmaDTO, error) {
	rows, err := p.client.DB().Query(`
		SELECT id, codigo_turma, codigo_academia, nivel, curso_id, turno,
		       estudantes, historico_estudantes_ano_letivo, status, status_alterado_por, status_alterado_em,
		       created_at, updated_at, version, tema_trabalho
		FROM projection_turmas
		WHERE curso_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, cursoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	turmas, err := scanTurmas(rows)
	if err != nil {
		return nil, err
	}
	result := make([]*TurmaDTO, len(turmas))
	for i := range turmas {
		result[i] = &turmas[i]
	}
	return result, nil
}

// ListByEstudante retorna turmas que contêm o código do estudante no array estudantes.
// Quando codigoAcademia != nil, limita o resultado à academia informada.
func (p *TurmasProjection) ListByEstudante(codigoEstudante string, codigoAcademia *string) ([]*TurmaDTO, error) {
	baseQuery := `
		SELECT id, codigo_turma, codigo_academia, nivel, curso_id, turno,
		       estudantes, historico_estudantes_ano_letivo, status, status_alterado_por, status_alterado_em,
		       created_at, updated_at, version, tema_trabalho
		FROM projection_turmas
		WHERE deleted_at IS NULL
		  AND EXISTS (
			  SELECT 1
			  FROM jsonb_array_elements_text(estudantes) AS e(codigo)
			  WHERE e.codigo = $1
		  )
	`

	var (
		rows *sql.Rows
		err  error
	)
	if codigoAcademia != nil {
		rows, err = p.client.DB().Query(baseQuery+` AND codigo_academia = $2 ORDER BY created_at DESC`, codigoEstudante, *codigoAcademia)
	} else {
		rows, err = p.client.DB().Query(baseQuery+` ORDER BY created_at DESC`, codigoEstudante)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	turmas, err := scanTurmas(rows)
	if err != nil {
		return nil, err
	}
	result := make([]*TurmaDTO, len(turmas))
	for i := range turmas {
		result[i] = &turmas[i]
	}
	return result, nil
}

func scanTurmaRow(row *sql.Row) (*TurmaDTO, error) {
	var dto TurmaDTO
	var cursoID sql.NullString
	var estudantesRaw []byte
	var historicoRaw []byte
	var statusAlteradoPor sql.NullString
	var statusAlteradoEm sql.NullTime
	var temaTrabalho sql.NullString

	err := row.Scan(
		&dto.ID, &dto.CodigoTurma, &dto.CodigoAcademia,
		&dto.Nivel, &cursoID, &dto.Turno,
		&estudantesRaw, &historicoRaw, &dto.Status,
		&statusAlteradoPor, &statusAlteradoEm,
		&dto.CreatedAt, &dto.UpdatedAt, &dto.Version, &temaTrabalho,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	dto.TipoAgrupamento = aggregates.TipoAgrupamentoDoNivel(dto.Nivel)
	if temaTrabalho.Valid {
		tema := temaTrabalho.String
		dto.TemaTrabalho = &tema
	}
	if cursoID.Valid {
		uid, _ := uuid.Parse(cursoID.String)
		dto.CursoID = &uid
	}

	if statusAlteradoPor.Valid {
		uid, _ := uuid.Parse(statusAlteradoPor.String)
		dto.StatusAlteradoPor = &uid
	}

	if statusAlteradoEm.Valid {
		t := statusAlteradoEm.Time
		dto.StatusAlteradoEm = &t
	}

	if len(estudantesRaw) > 0 {
		_ = json.Unmarshal(estudantesRaw, &dto.Estudantes)
	}
	if dto.Estudantes == nil {
		dto.Estudantes = []string{}
	}
	if len(historicoRaw) > 0 {
		_ = json.Unmarshal(historicoRaw, &dto.HistoricoEstudantesAnoLetivo)
	}
	if dto.HistoricoEstudantesAnoLetivo == nil {
		dto.HistoricoEstudantesAnoLetivo = map[string][]string{}
	}

	return &dto, nil
}

func scanTurmas(rows *sql.Rows) ([]TurmaDTO, error) {
	var result []TurmaDTO
	for rows.Next() {
		var dto TurmaDTO
		var cursoID sql.NullString
		var estudantesRaw []byte
		var historicoRaw []byte
		var statusAlteradoPor sql.NullString
		var statusAlteradoEm sql.NullTime
		var temaTrabalho sql.NullString

		if err := rows.Scan(
			&dto.ID, &dto.CodigoTurma, &dto.CodigoAcademia,
			&dto.Nivel, &cursoID, &dto.Turno,
			&estudantesRaw, &historicoRaw, &dto.Status,
			&statusAlteradoPor, &statusAlteradoEm,
			&dto.CreatedAt, &dto.UpdatedAt, &dto.Version, &temaTrabalho,
		); err != nil {
			continue
		}

		dto.TipoAgrupamento = aggregates.TipoAgrupamentoDoNivel(dto.Nivel)
		if temaTrabalho.Valid {
			tema := temaTrabalho.String
			dto.TemaTrabalho = &tema
		}
		if cursoID.Valid {
			uid, _ := uuid.Parse(cursoID.String)
			dto.CursoID = &uid
		}

		if statusAlteradoPor.Valid {
			uid, _ := uuid.Parse(statusAlteradoPor.String)
			dto.StatusAlteradoPor = &uid
		}

		if statusAlteradoEm.Valid {
			t := statusAlteradoEm.Time
			dto.StatusAlteradoEm = &t
		}

		if len(estudantesRaw) > 0 {
			_ = json.Unmarshal(estudantesRaw, &dto.Estudantes)
		}
		if dto.Estudantes == nil {
			dto.Estudantes = []string{}
		}
		if len(historicoRaw) > 0 {
			_ = json.Unmarshal(historicoRaw, &dto.HistoricoEstudantesAnoLetivo)
		}
		if dto.HistoricoEstudantesAnoLetivo == nil {
			dto.HistoricoEstudantesAnoLetivo = map[string][]string{}
		}

		result = append(result, dto)
	}
	return result, rows.Err()
}
