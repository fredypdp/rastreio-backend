package projections

import (
	"database/sql"
	"time"

	"spuri/internal/db"
)

// FaltasConfiguracaoProjection materializa a configuração de faltas por academia.
type FaltasConfiguracaoProjection struct{ client *db.Client }

func NewFaltasConfiguracaoProjection(c *db.Client) *FaltasConfiguracaoProjection {
	return &FaltasConfiguracaoProjection{c}
}

// ConfiguracaoFaltasDTO é a configuração de faltas de uma academia.
type ConfiguracaoFaltasDTO struct {
	CodigoAcademia         string     `json:"codigo_academia"`
	LimiteFaltasPorPeriodo *int       `json:"limite_faltas_por_periodo"`
	ReprovacaoPorFaltas    bool       `json:"reprovacao_por_faltas"`
	AtualizadoEm           *time.Time `json:"atualizado_em,omitempty"`
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
