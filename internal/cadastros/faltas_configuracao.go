package cadastros

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"spuri/internal/db"
)

const EntidadeConfiguracaoFaltas = "ConfiguracaoFaltas"

// MaxLimiteFaltasPorPeriodo é o maior limite de faltas por período aceito.
const MaxLimiteFaltasPorPeriodo = 500

// ConfiguracaoFaltas guarda duas regras independentes da academia (Tarefa 135,
// regra 17; era o agregado ConfiguracaoFaltas):
//   - LimiteFaltasPorPeriodo: quantas faltas um estudante pode ter, por matéria
//     e período, antes de ultrapassar o limite (nil = sem limite);
//   - ReprovacaoPorFaltas: se, ao ultrapassar o limite, a avaliação final
//     automática lê a nota afetada como 0. Só pode ficar ligada se houver limite.
//
// Existe no máximo UMA por academia; a tabela de registro é
// projection_faltas_configuracao, com chave em codigo_academia.
type ConfiguracaoFaltas struct {
	CodigoAcademia         string
	LimiteFaltasPorPeriodo *int
	ReprovacaoPorFaltas    bool
	AtualizadoPor          uuid.UUID
	AtualizadoEm           time.Time
	Version                int
}

// NovaConfiguracaoFaltas valida a configuração (função pura).
func NovaConfiguracaoFaltas(codigoAcademia string, limite *int, reprovacaoPorFaltas bool, definidoPor uuid.UUID) (*ConfiguracaoFaltas, error) {
	codigoAcademia = strings.TrimSpace(codigoAcademia)
	if codigoAcademia == "" {
		return nil, regra("codigo_academia é obrigatório")
	}
	if limite != nil && (*limite < 1 || *limite > MaxLimiteFaltasPorPeriodo) {
		return nil, regra("limite_faltas_por_periodo deve estar entre 1 e %d", MaxLimiteFaltasPorPeriodo)
	}
	if reprovacaoPorFaltas && limite == nil {
		return nil, regra("reprovacao_por_faltas exige limite_faltas_por_periodo definido")
	}
	var copia *int
	if limite != nil {
		v := *limite
		copia = &v
	}
	return &ConfiguracaoFaltas{
		CodigoAcademia: codigoAcademia, LimiteFaltasPorPeriodo: copia,
		ReprovacaoPorFaltas: reprovacaoPorFaltas, AtualizadoPor: definidoPor,
	}, nil
}

func (c *ConfiguracaoFaltas) snapshot() map[string]any {
	var limite any
	if c.LimiteFaltasPorPeriodo != nil {
		limite = *c.LimiteFaltasPorPeriodo
	}
	return map[string]any{
		"codigo_academia":           c.CodigoAcademia,
		"limite_faltas_por_periodo": limite,
		"reprovacao_por_faltas":     c.ReprovacaoPorFaltas,
		"version":                   c.Version,
	}
}

// FaltasConfiguracaoRepo grava a configuração de faltas, com auditoria na mesma
// transação.
type FaltasConfiguracaoRepo struct{ client *db.Client }

func NovoFaltasConfiguracaoRepo(client *db.Client) *FaltasConfiguracaoRepo {
	return &FaltasConfiguracaoRepo{client: client}
}

// Definir substitui a configuração da academia (cria se não existir) e audita
// ("ConfiguracaoFaltasDefinida"), com o estado anterior em "antes". A academia
// precisa existir. A linha é bloqueada durante a gravação.
func (r *FaltasConfiguracaoRepo) Definir(ctx context.Context, audit db.AuditContext, c *ConfiguracaoFaltas) error {
	return r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		if err := exigirAcademia(ctx, tx, c.CodigoAcademia); err != nil {
			return err
		}
		var antes map[string]any
		anterior := &ConfiguracaoFaltas{CodigoAcademia: c.CodigoAcademia}
		var limite *int
		err := tx.QueryRowxContext(ctx, `
			SELECT limite_faltas_por_periodo, reprovacao_por_faltas, version
			FROM projection_faltas_configuracao WHERE codigo_academia = $1 FOR UPDATE`,
			c.CodigoAcademia).Scan(&limite, &anterior.ReprovacaoPorFaltas, &anterior.Version)
		switch {
		case err == nil:
			anterior.LimiteFaltasPorPeriodo = limite
			antes = anterior.snapshot()
		case isNoRows(err):
			// primeira definição
		default:
			return fmt.Errorf("carregar configuração de faltas: %w", err)
		}

		if err := tx.QueryRowxContext(ctx, `
			INSERT INTO projection_faltas_configuracao
				(codigo_academia, limite_faltas_por_periodo, reprovacao_por_faltas, atualizado_por, atualizado_em, version)
			VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP, 1)
			ON CONFLICT (codigo_academia) DO UPDATE SET
				limite_faltas_por_periodo = EXCLUDED.limite_faltas_por_periodo,
				reprovacao_por_faltas     = EXCLUDED.reprovacao_por_faltas,
				atualizado_por            = EXCLUDED.atualizado_por,
				atualizado_em             = CURRENT_TIMESTAMP,
				version                   = projection_faltas_configuracao.version + 1
			RETURNING atualizado_em, version`,
			c.CodigoAcademia, c.LimiteFaltasPorPeriodo, c.ReprovacaoPorFaltas, c.AtualizadoPor,
		).Scan(&c.AtualizadoEm, &c.Version); err != nil {
			return fmt.Errorf("gravar configuração de faltas: %w", err)
		}
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeConfiguracaoFaltas, EntidadeID: c.CodigoAcademia, AcademiaID: academiaDoAtor(audit),
			Acao: "ConfiguracaoFaltasDefinida", Antes: antes, Depois: c.snapshot(),
		})
	})
}
