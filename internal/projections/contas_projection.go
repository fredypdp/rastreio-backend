package projections

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"spuri/internal/db"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ContasProjection mantém projection_contas_pessoa (secretário, professor e
// encarregado) e as tabelas de vínculos: professor ↔ (turma, matéria) e
// encarregado ↔ educando. Plano de Atualizações, Parte 4.
type ContasProjection struct {
	client *db.Client
}

func NewContasProjection(client *db.Client) *ContasProjection {
	return &ContasProjection{client: client}
}

func (p *ContasProjection) Name() string { return "contas" }

const tabelaContas = "projection_contas_pessoa"

// tiposAgregadoContas são os aggregate_type que esta projeção consome.
var tiposAgregadoContas = map[string]bool{"Secretario": true, "Professor": true, "Encarregado": true}

func (p *ContasProjection) GetLastProcessedEventID() (int64, error) {
	var lastID int64
	err := p.client.DB().QueryRow(
		`SELECT last_processed_event_id FROM projection_checkpoints WHERE projection_name = $1`, p.Name(),
	).Scan(&lastID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return lastID, err
}

func (p *ContasProjection) UpdateCheckpoint(eventID int64) error {
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

func (p *ContasProjection) Handle(event db.Event) error {
	// A exclusão da conta de um estudante encerra o vínculo com o encarregado.
	if event.AggregateType == "Estudante" && event.EventType == "EstudanteDeletado" {
		return p.handleEstudanteDeletado(event)
	}
	if !tiposAgregadoContas[event.AggregateType] {
		return nil
	}
	handlers := map[string]func(db.Event) error{
		"ContaPessoaCriada":               p.handleCriada,
		"ContaPessoaDadosAtualizados":     p.handleDadosAtualizados,
		"ContaPessoaSenhaAlterada":        p.handleSenhaAlterada,
		"ContaPessoaAtivada":              p.handleStatus("ativo"),
		"ContaPessoaDesativada":           p.handleStatus("inativo"),
		"ContaPessoaDeletada":             p.handleDeletada,
		"EmailVerificado":                 p.handleEmailVerificado,
		"SenhaTemporariaDefinida":         p.handleSenhaTemporaria,
		"DoisFatoresAtivados":             p.handleDoisFatores(true),
		"DoisFatoresDesativados":          p.handleDoisFatores(false),
		"SecretarioPermissoesDefinidas":   p.handlePermissoes,
		"ProfessorVinculoAtribuido":       p.handleVinculoAtribuido,
		"ProfessorVinculoRemovido":        p.handleVinculoRemovido,
		"EncarregadoEducandoVinculado":    p.handleEducandoVinculado,
		"EncarregadoEducandoDesvinculado": p.handleEducandoDesvinculado,
		"EncarregadoRelacaoAtualizada":    p.handleRelacaoAtualizada,
	}
	if h, ok := handlers[event.EventType]; ok {
		return h(event)
	}
	return nil
}

// Rebuild reconstrói as três tabelas a partir do ledger.
func (p *ContasProjection) Rebuild() error {
	log.Printf("[DEBUG] [contas] Rebuild iniciado")
	if _, err := p.client.DB().Exec(`TRUNCATE TABLE projection_contas_pessoa, projection_professor_vinculos, projection_encarregado_educandos`); err != nil {
		return fmt.Errorf("falha ao limpar projeção: %w", err)
	}
	rows, err := p.client.DB().Query(`
		SELECT id, event_id, aggregate_id, aggregate_type, event_type,
			event_version, payload, metadata, occurred_at, recorded_at,
			ledger_hash, previous_hash
		FROM spuri_ledger
		WHERE aggregate_type IN ('Secretario','Professor','Encarregado')
		   OR (aggregate_type = 'Estudante' AND event_type = 'EstudanteDeletado')
		ORDER BY id ASC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var event db.Event
		var prevHash sql.NullString
		if err := rows.Scan(&event.ID, &event.EventID, &event.AggregateID, &event.AggregateType,
			&event.EventType, &event.EventVersion, &event.Payload, &event.Metadata,
			&event.OccurredAt, &event.RecordedAt, &event.LedgerHash, &prevHash); err != nil {
			return err
		}
		if prevHash.Valid {
			event.PreviousHash = &prevHash.String
		}
		if err := p.Handle(event); err != nil {
			return fmt.Errorf("erro no evento %d: %w", event.ID, err)
		}
		count++
	}
	log.Printf("[DEBUG] [contas] Rebuild concluído: %d eventos", count)
	return rows.Err()
}

// ── Handlers ────────────────────────────────────────────────────────────────

func (p *ContasProjection) handleCriada(event db.Event) error {
	var pl struct {
		Tipo           string
		Codigo         string
		Nome           string
		Genero         string
		DataNascimento time.Time
		Email          *string
		Telefone       string
		SenhaHash      string
		AcademiaID     *uuid.UUID
		CodigoAcademia string
		Permissoes     []string
		CriadoPor      *uuid.UUID
		CreatedAt      time.Time
	}
	if err := json.Unmarshal(event.Payload, &pl); err != nil {
		return err
	}
	var academiaID, criadoPor interface{}
	if pl.AcademiaID != nil {
		academiaID = pl.AcademiaID.String()
	}
	if pl.CriadoPor != nil {
		criadoPor = pl.CriadoPor.String()
	}
	var codAcademia interface{}
	if pl.CodigoAcademia != "" {
		codAcademia = pl.CodigoAcademia
	}
	perms := pl.Permissoes
	if perms == nil {
		perms = []string{}
	}
	// ON CONFLICT DO NOTHING: replay seguro e uma violação de unicidade nunca
	// trava o Manager (o handler já valida código e e-mail antes de gravar o evento).
	_, err := p.client.DB().Exec(`
		INSERT INTO projection_contas_pessoa
			(id, tipo, codigo, nome, genero, data_nascimento, email, telefone, senha_hash, status,
			 academia_id, codigo_academia, permissoes, criado_por, created_at, updated_at, version, last_event_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'ativo',$10,$11,$12,$13,$14,CURRENT_TIMESTAMP,$15,$16)
		ON CONFLICT DO NOTHING
	`, event.AggregateID, pl.Tipo, pl.Codigo, pl.Nome, pl.Genero, pl.DataNascimento, pl.Email, pl.Telefone, pl.SenhaHash,
		academiaID, codAcademia, pq.Array(perms), criadoPor, pl.CreatedAt, event.EventVersion, event.EventID)
	return err
}

func (p *ContasProjection) handleDadosAtualizados(event db.Event) error {
	var pl struct {
		Nome             *string
		Genero           *string
		DataNascimento   *time.Time
		Email            *string
		EmailAlterado    bool
		Telefone         *string
		TelefoneAlterado bool
	}
	if err := json.Unmarshal(event.Payload, &pl); err != nil {
		return err
	}
	tx, err := p.client.DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if pl.Nome != nil {
		if _, err := tx.Exec(`UPDATE `+tabelaContas+` SET nome=$1 WHERE id=$2`, *pl.Nome, event.AggregateID); err != nil {
			return err
		}
	}
	if pl.Genero != nil {
		if _, err := tx.Exec(`UPDATE `+tabelaContas+` SET genero=$1 WHERE id=$2`, *pl.Genero, event.AggregateID); err != nil {
			return err
		}
	}
	if pl.DataNascimento != nil {
		if _, err := tx.Exec(`UPDATE `+tabelaContas+` SET data_nascimento=$1 WHERE id=$2`, *pl.DataNascimento, event.AggregateID); err != nil {
			return err
		}
	}
	if pl.Email != nil {
		var conflito int
		err := tx.QueryRow(`SELECT 1 FROM `+tabelaContas+` WHERE LOWER(email)=LOWER($1) AND id<>$2 AND status<>'deletado'`, *pl.Email, event.AggregateID).Scan(&conflito)
		if err == nil {
			log.Printf("[WARN] [contas] e-mail já em uso por outra conta; atualização de e-mail ignorada (evento %d)", event.ID)
		} else if err == sql.ErrNoRows {
			if _, err := tx.Exec(`UPDATE `+tabelaContas+` SET email=$1 WHERE id=$2`, *pl.Email, event.AggregateID); err != nil {
				return err
			}
			if pl.EmailAlterado {
				// Sem e-mail verificado não há 2FA por e-mail (mesma regra do agregado).
				if _, err := tx.Exec(`UPDATE `+tabelaContas+` SET email_verificado=FALSE, dois_fatores_ativo=FALSE WHERE id=$1`, event.AggregateID); err != nil {
					return err
				}
			}
		} else {
			return err
		}
	}
	if pl.Telefone != nil {
		if _, err := tx.Exec(`UPDATE `+tabelaContas+` SET telefone=$1 WHERE id=$2`, *pl.Telefone, event.AggregateID); err != nil {
			return err
		}
		if pl.TelefoneAlterado {
			if _, err := tx.Exec(`UPDATE `+tabelaContas+` SET telefone_verificado=FALSE WHERE id=$1`, event.AggregateID); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`UPDATE `+tabelaContas+` SET version=$1, updated_at=CURRENT_TIMESTAMP, last_event_id=$2 WHERE id=$3`,
		event.EventVersion, event.EventID, event.AggregateID); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *ContasProjection) handleSenhaAlterada(event db.Event) error {
	var pl struct {
		NovaSenhaHash string
		Motivo        string
	}
	if err := json.Unmarshal(event.Payload, &pl); err != nil {
		return err
	}
	if pl.NovaSenhaHash == "" {
		return fmt.Errorf("ContaPessoaSenhaAlterada: NovaSenhaHash vazio no payload")
	}
	return aplicarSenhaAlterada(p.client.DB(), tabelaContas, pl.NovaSenhaHash, pl.Motivo, event)
}

func (p *ContasProjection) handleStatus(status string) func(db.Event) error {
	return func(event db.Event) error {
		_, err := p.client.DB().Exec(`
			UPDATE `+tabelaContas+` SET status=$1, version=$2, updated_at=CURRENT_TIMESTAMP, last_event_id=$3
			WHERE id=$4 AND status <> 'deletado'`, status, event.EventVersion, event.EventID, event.AggregateID)
		return err
	}
}

func (p *ContasProjection) handleDeletada(event db.Event) error {
	var pl struct {
		DeletadoPor *uuid.UUID
		DeletedAt   time.Time
	}
	if err := json.Unmarshal(event.Payload, &pl); err != nil {
		return err
	}
	var por interface{}
	if pl.DeletadoPor != nil {
		por = pl.DeletadoPor.String()
	}
	tx, err := p.client.DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`
		UPDATE `+tabelaContas+` SET status='deletado', deleted_at=$1, deletado_por=$2,
			version=$3, updated_at=CURRENT_TIMESTAMP, last_event_id=$4
		WHERE id=$5 AND status <> 'deletado'`, pl.DeletedAt, por, event.EventVersion, event.EventID, event.AggregateID); err != nil {
		return err
	}
	// Eliminar o professor encerra os vínculos; eliminar o encarregado, os educandos.
	if _, err := tx.Exec(`UPDATE projection_professor_vinculos SET removido_em=$1, motivo_remocao='conta_eliminada'
		WHERE professor_id=$2 AND removido_em IS NULL`, pl.DeletedAt, event.AggregateID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE projection_encarregado_educandos SET desvinculado_em=$1, motivo_desvinculo='conta_eliminada'
		WHERE encarregado_id=$2 AND desvinculado_em IS NULL`, pl.DeletedAt, event.AggregateID); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *ContasProjection) handleEmailVerificado(event db.Event) error {
	_, err := p.client.DB().Exec(`UPDATE `+tabelaContas+` SET email_verificado=TRUE, version=$1, updated_at=CURRENT_TIMESTAMP, last_event_id=$2 WHERE id=$3`,
		event.EventVersion, event.EventID, event.AggregateID)
	return err
}

func (p *ContasProjection) handleSenhaTemporaria(event db.Event) error {
	return setSenhaTemporaria(p.client.DB(), tabelaContas, event)
}

func (p *ContasProjection) handleDoisFatores(ativo bool) func(db.Event) error {
	return func(event db.Event) error {
		return setDoisFatoresAtivo(p.client.DB(), tabelaContas, ativo, event)
	}
}

func (p *ContasProjection) handlePermissoes(event db.Event) error {
	var pl struct{ Permissoes []string }
	if err := json.Unmarshal(event.Payload, &pl); err != nil {
		return err
	}
	if pl.Permissoes == nil {
		pl.Permissoes = []string{}
	}
	_, err := p.client.DB().Exec(`UPDATE `+tabelaContas+` SET permissoes=$1, version=$2, updated_at=CURRENT_TIMESTAMP, last_event_id=$3 WHERE id=$4`,
		pq.Array(pl.Permissoes), event.EventVersion, event.EventID, event.AggregateID)
	return err
}

func (p *ContasProjection) handleVinculoAtribuido(event db.Event) error {
	var pl struct {
		VinculoID    uuid.UUID
		CodigoTurma  string
		MateriaID    uuid.UUID
		AtribuidoPor *uuid.UUID
		AtribuidoEm  time.Time
	}
	if err := json.Unmarshal(event.Payload, &pl); err != nil {
		return err
	}
	var codAcademia sql.NullString
	if err := p.client.DB().QueryRow(`SELECT codigo_academia FROM `+tabelaContas+` WHERE id=$1`, event.AggregateID).Scan(&codAcademia); err != nil {
		return fmt.Errorf("professor %s não encontrado na projeção: %w", event.AggregateID, err)
	}
	var por interface{}
	if pl.AtribuidoPor != nil {
		por = pl.AtribuidoPor.String()
	}
	_, err := p.client.DB().Exec(`
		INSERT INTO projection_professor_vinculos (id, professor_id, codigo_academia, codigo_turma, materia_id, atribuido_por, atribuido_em)
		VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
		pl.VinculoID, event.AggregateID, codAcademia.String, pl.CodigoTurma, pl.MateriaID, por, pl.AtribuidoEm)
	return err
}

func (p *ContasProjection) handleVinculoRemovido(event db.Event) error {
	var pl struct {
		VinculoID   uuid.UUID
		Motivo      string
		RemovidoPor *uuid.UUID
		RemovidoEm  time.Time
	}
	if err := json.Unmarshal(event.Payload, &pl); err != nil {
		return err
	}
	var por interface{}
	if pl.RemovidoPor != nil {
		por = pl.RemovidoPor.String()
	}
	_, err := p.client.DB().Exec(`UPDATE projection_professor_vinculos SET removido_em=$1, removido_por=$2, motivo_remocao=$3
		WHERE id=$4 AND removido_em IS NULL`, pl.RemovidoEm, por, pl.Motivo, pl.VinculoID)
	return err
}

func (p *ContasProjection) handleEducandoVinculado(event db.Event) error {
	var pl struct {
		EstudanteID     uuid.UUID
		CodigoEstudante string
		Relacao         *string
		RelacaoOutro    *string
		VinculadoPor    *uuid.UUID
		VinculadoEm     time.Time
	}
	if err := json.Unmarshal(event.Payload, &pl); err != nil {
		return err
	}
	var por interface{}
	if pl.VinculadoPor != nil {
		por = pl.VinculadoPor.String()
	}
	_, err := p.client.DB().Exec(`
		INSERT INTO projection_encarregado_educandos
			(encarregado_id, estudante_id, codigo_estudante, relacao, relacao_outro, vinculado_em, vinculado_por)
		VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
		event.AggregateID, pl.EstudanteID, pl.CodigoEstudante, pl.Relacao, pl.RelacaoOutro, pl.VinculadoEm, por)
	return err
}

func (p *ContasProjection) handleEducandoDesvinculado(event db.Event) error {
	var pl struct {
		EstudanteID     uuid.UUID
		Motivo          string
		DesvinculadoPor *uuid.UUID
		DesvinculadoEm  time.Time
	}
	if err := json.Unmarshal(event.Payload, &pl); err != nil {
		return err
	}
	var por interface{}
	if pl.DesvinculadoPor != nil {
		por = pl.DesvinculadoPor.String()
	}
	_, err := p.client.DB().Exec(`UPDATE projection_encarregado_educandos
		SET desvinculado_em=$1, desvinculado_por=$2, motivo_desvinculo=$3
		WHERE encarregado_id=$4 AND estudante_id=$5 AND desvinculado_em IS NULL`,
		pl.DesvinculadoEm, por, pl.Motivo, event.AggregateID, pl.EstudanteID)
	return err
}

func (p *ContasProjection) handleRelacaoAtualizada(event db.Event) error {
	var pl struct {
		EstudanteID  uuid.UUID
		Relacao      *string
		RelacaoOutro *string
	}
	if err := json.Unmarshal(event.Payload, &pl); err != nil {
		return err
	}
	_, err := p.client.DB().Exec(`UPDATE projection_encarregado_educandos SET relacao=$1, relacao_outro=$2
		WHERE encarregado_id=$3 AND estudante_id=$4 AND desvinculado_em IS NULL`,
		pl.Relacao, pl.RelacaoOutro, event.AggregateID, pl.EstudanteID)
	return err
}

func (p *ContasProjection) handleEstudanteDeletado(event db.Event) error {
	_, err := p.client.DB().Exec(`UPDATE projection_encarregado_educandos
		SET desvinculado_em=$1, motivo_desvinculo='conta_do_estudante_excluida'
		WHERE estudante_id=$2 AND desvinculado_em IS NULL`, event.OccurredAt, event.AggregateID)
	return err
}

// ============================================================================
// Consultas
// ============================================================================

// ContaDTO é a leitura de uma conta de secretário, professor ou encarregado.
type ContaDTO struct {
	ID               uuid.UUID  `json:"id"`
	Tipo             string     `json:"tipo"`
	Codigo           string     `json:"codigo"`
	Nome             string     `json:"nome"`
	Genero           string     `json:"genero"`
	DataNascimento   string     `json:"data_nascimento"`
	Email            *string    `json:"email,omitempty"`
	EmailVerificado  bool       `json:"email_verificado"`
	Telefone         string     `json:"telefone"`
	SenhaHash        string     `json:"-"`
	Status           string     `json:"status"`
	AcademiaID       *uuid.UUID `json:"academia_id,omitempty"`
	CodigoAcademia   *string    `json:"codigo_academia,omitempty"`
	Permissoes       []string   `json:"permissoes,omitempty"`
	DeveAlterarSenha bool       `json:"deve_alterar_senha"`
	DoisFatoresAtivo bool       `json:"dois_fatores_ativo"`
	CriadoPor        *uuid.UUID `json:"criado_por,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

const colunasConta = `id, tipo, codigo, nome, genero, to_char(data_nascimento,'YYYY-MM-DD'), email, email_verificado, telefone,
	senha_hash, status, academia_id, codigo_academia, permissoes, deve_alterar_senha, dois_fatores_ativo, criado_por, created_at`

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanConta(row scanner) (*ContaDTO, error) {
	var c ContaDTO
	var email, codAcademia sql.NullString
	var academiaID, criadoPor sql.NullString
	var perms pq.StringArray
	err := row.Scan(&c.ID, &c.Tipo, &c.Codigo, &c.Nome, &c.Genero, &c.DataNascimento, &email, &c.EmailVerificado, &c.Telefone,
		&c.SenhaHash, &c.Status, &academiaID, &codAcademia, &perms, &c.DeveAlterarSenha, &c.DoisFatoresAtivo, &criadoPor, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if email.Valid {
		c.Email = &email.String
	}
	if codAcademia.Valid {
		c.CodigoAcademia = &codAcademia.String
	}
	if academiaID.Valid {
		if id, err := uuid.Parse(academiaID.String); err == nil {
			c.AcademiaID = &id
		}
	}
	if criadoPor.Valid {
		if id, err := uuid.Parse(criadoPor.String); err == nil {
			c.CriadoPor = &id
		}
	}
	c.Permissoes = []string(perms)
	return &c, nil
}

// GetByID devolve a conta (qualquer status, inclusive deletada). tipo="" aceita qualquer tipo.
func (p *ContasProjection) GetByID(id uuid.UUID, tipo string) (*ContaDTO, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("UUID inválido")
	}
	q := `SELECT ` + colunasConta + ` FROM ` + tabelaContas + ` WHERE id = $1`
	args := []interface{}{id}
	if tipo != "" {
		q += ` AND tipo = $2`
		args = append(args, tipo)
	}
	return scanConta(p.client.DB().QueryRow(q, args...))
}

// GetByCodigoOrEmailParaLogin devolve a conta (não deletada) pelo código ou
// e-mail. Mesma regra das demais contas: o código vale sempre; o e-mail só
// depois de verificado (a decisão fica com o login, que sabe qual foi usado).
func (p *ContasProjection) GetByCodigoOrEmailParaLogin(identificador string) (*ContaDTO, error) {
	identificador = strings.TrimSpace(identificador)
	if identificador == "" {
		return nil, nil
	}
	return scanConta(p.client.DB().QueryRow(`SELECT `+colunasConta+` FROM `+tabelaContas+`
		WHERE status <> 'deletado' AND (UPPER(codigo) = UPPER($1) OR LOWER(email) = LOWER($1))
		ORDER BY (UPPER(codigo) = UPPER($1)) DESC LIMIT 1`, identificador))
}

// GetByEmail devolve a conta não deletada com o e-mail (qualquer tipo).
func (p *ContasProjection) GetByEmail(email string) (*ContaDTO, error) {
	return scanConta(p.client.DB().QueryRow(`SELECT `+colunasConta+` FROM `+tabelaContas+`
		WHERE LOWER(email) = LOWER($1) AND status <> 'deletado' LIMIT 1`, strings.TrimSpace(email)))
}

// GetEncarregadoPorContato procura um encarregado ativo pelo telefone OU e-mail
// (identificação do encarregado já existente no cadastro do estudante).
func (p *ContasProjection) GetEncarregadoPorContato(telefone, email string) (*ContaDTO, error) {
	telefone, email = strings.TrimSpace(telefone), strings.TrimSpace(email)
	if telefone == "" && email == "" {
		return nil, nil
	}
	return scanConta(p.client.DB().QueryRow(`SELECT `+colunasConta+` FROM `+tabelaContas+`
		WHERE tipo='encarregado' AND status <> 'deletado'
		  AND (($1 <> '' AND telefone = $1) OR ($2 <> '' AND LOWER(email) = LOWER($2)))
		ORDER BY created_at ASC LIMIT 1`, telefone, email))
}

// ListarDaAcademia lista secretários ou professores (não deletados) da academia.
func (p *ContasProjection) ListarDaAcademia(tipo, codigoAcademia string) ([]ContaDTO, error) {
	rows, err := p.client.DB().Query(`SELECT `+colunasConta+` FROM `+tabelaContas+`
		WHERE tipo=$1 AND codigo_academia=$2 AND status <> 'deletado' ORDER BY nome ASC`, tipo, codigoAcademia)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	saida := []ContaDTO{}
	for rows.Next() {
		c, err := scanConta(rows)
		if err != nil {
			return nil, err
		}
		if c != nil {
			saida = append(saida, *c)
		}
	}
	return saida, rows.Err()
}

// ContaExiste diz se o código já existe (qualquer status), para a geração de códigos.
func (p *ContasProjection) CodigoExiste(codigo string) (bool, error) {
	var n int
	err := p.client.DB().QueryRow(`SELECT COUNT(*) FROM `+tabelaContas+` WHERE codigo=$1`, codigo).Scan(&n)
	return n > 0, err
}

// VinculoProfessorDTO é um vínculo ativo professor ↔ (turma, matéria).
type VinculoProfessorDTO struct {
	ID             uuid.UUID `json:"id"`
	ProfessorID    uuid.UUID `json:"professor_id"`
	CodigoAcademia string    `json:"codigo_academia"`
	CodigoTurma    string    `json:"codigo_turma"`
	MateriaID      uuid.UUID `json:"materia_id"`
	AtribuidoEm    time.Time `json:"atribuido_em"`
}

// VinculosDoProfessor lista os vínculos ATIVOS do professor.
func (p *ContasProjection) VinculosDoProfessor(professorID uuid.UUID) ([]VinculoProfessorDTO, error) {
	rows, err := p.client.DB().Query(`SELECT id, professor_id, codigo_academia, codigo_turma, materia_id, atribuido_em
		FROM projection_professor_vinculos WHERE professor_id=$1 AND removido_em IS NULL ORDER BY codigo_turma, atribuido_em`, professorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	saida := []VinculoProfessorDTO{}
	for rows.Next() {
		var v VinculoProfessorDTO
		if err := rows.Scan(&v.ID, &v.ProfessorID, &v.CodigoAcademia, &v.CodigoTurma, &v.MateriaID, &v.AtribuidoEm); err != nil {
			return nil, err
		}
		saida = append(saida, v)
	}
	return saida, rows.Err()
}

// VinculoPorID devolve um vínculo ativo (nil se não existe ou já foi removido).
func (p *ContasProjection) VinculoPorID(id uuid.UUID) (*VinculoProfessorDTO, error) {
	var v VinculoProfessorDTO
	err := p.client.DB().QueryRow(`SELECT id, professor_id, codigo_academia, codigo_turma, materia_id, atribuido_em
		FROM projection_professor_vinculos WHERE id=$1 AND removido_em IS NULL`, id).
		Scan(&v.ID, &v.ProfessorID, &v.CodigoAcademia, &v.CodigoTurma, &v.MateriaID, &v.AtribuidoEm)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &v, err
}

// ProfessorDoPar devolve o professor ativo que tem o par (turma, matéria), ou uuid.Nil.
func (p *ContasProjection) ProfessorDoPar(codigoAcademia, codigoTurma string, materiaID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := p.client.DB().QueryRow(`SELECT professor_id FROM projection_professor_vinculos
		WHERE codigo_academia=$1 AND codigo_turma=$2 AND materia_id=$3 AND removido_em IS NULL`, codigoAcademia, codigoTurma, materiaID).Scan(&id)
	if err == sql.ErrNoRows {
		return uuid.Nil, nil
	}
	return id, err
}

// EducandoDTO é um vínculo ativo encarregado ↔ estudante.
type EducandoDTO struct {
	EstudanteID     uuid.UUID `json:"estudante_id"`
	CodigoEstudante string    `json:"codigo_estudante"`
	Relacao         *string   `json:"relacao,omitempty"`
	RelacaoOutro    *string   `json:"relacao_outro,omitempty"`
	VinculadoEm     time.Time `json:"vinculado_em"`
}

// EducandosDoEncarregado lista os educandos ativos.
func (p *ContasProjection) EducandosDoEncarregado(encarregadoID uuid.UUID) ([]EducandoDTO, error) {
	rows, err := p.client.DB().Query(`SELECT estudante_id, codigo_estudante, relacao, relacao_outro, vinculado_em
		FROM projection_encarregado_educandos WHERE encarregado_id=$1 AND desvinculado_em IS NULL ORDER BY vinculado_em`, encarregadoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	saida := []EducandoDTO{}
	for rows.Next() {
		var e EducandoDTO
		var rel, relOutro sql.NullString
		if err := rows.Scan(&e.EstudanteID, &e.CodigoEstudante, &rel, &relOutro, &e.VinculadoEm); err != nil {
			return nil, err
		}
		if rel.Valid {
			e.Relacao = &rel.String
		}
		if relOutro.Valid {
			e.RelacaoOutro = &relOutro.String
		}
		saida = append(saida, e)
	}
	return saida, rows.Err()
}

// EducandoAtivo devolve o vínculo ativo (nil se o encarregado não é encarregado do estudante).
func (p *ContasProjection) EducandoAtivo(encarregadoID uuid.UUID, codigoEstudante string) (*EducandoDTO, error) {
	var e EducandoDTO
	var rel, relOutro sql.NullString
	err := p.client.DB().QueryRow(`SELECT estudante_id, codigo_estudante, relacao, relacao_outro, vinculado_em
		FROM projection_encarregado_educandos
		WHERE encarregado_id=$1 AND UPPER(codigo_estudante)=UPPER($2) AND desvinculado_em IS NULL`, encarregadoID, codigoEstudante).
		Scan(&e.EstudanteID, &e.CodigoEstudante, &rel, &relOutro, &e.VinculadoEm)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if rel.Valid {
		e.Relacao = &rel.String
	}
	if relOutro.Valid {
		e.RelacaoOutro = &relOutro.String
	}
	return &e, nil
}

// EncarregadoDoEstudante devolve o encarregado ATIVO do estudante, ou nil.
func (p *ContasProjection) EncarregadoDoEstudante(estudanteID uuid.UUID) (*ContaDTO, error) {
	return scanConta(p.client.DB().QueryRow(`SELECT `+colunasConta+` FROM `+tabelaContas+`
		WHERE status <> 'deletado' AND id = (
			SELECT encarregado_id FROM projection_encarregado_educandos
			WHERE estudante_id=$1 AND desvinculado_em IS NULL LIMIT 1)`, estudanteID))
}
