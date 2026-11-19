package apoio

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
)

func ChaveAnoLetivoGlobal(tipo string) string { return "ano_letivo_atual_" + tipo }

func NivelAcademiaPorTipoAnoLetivo(tipo string) string {
	if tipo == "superior" {
		return "superior"
	}
	return "escola"
}

func BuscarAnoLetivoGlobalAtual(client *db.Client, tipo string) (string, error) {
	var anoLetivo sql.NullString
	err := client.DB().QueryRow(`SELECT ano_letivo_atual FROM projection_sistema_config WHERE chave = $1`, ChaveAnoLetivoGlobal(tipo)).Scan(&anoLetivo)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !anoLetivo.Valid {
		return "", nil
	}
	return strings.TrimSpace(anoLetivo.String), nil
}

func SalvarAnoLetivoGlobal(c *gin.Context, tipo string, anoLetivo string, userID uuid.UUID) error {
	client := GetDbClient(c)
	if client == nil {
		return fmt.Errorf("cliente de banco indisponível")
	}
	_, err := client.DB().Exec(`
		INSERT INTO projection_sistema_config (
			chave, valor, ano_letivo_atual, anos_letivos_lista, definido_por, updated_at, version
		) VALUES (
			$3::text, $1::text, $1::varchar(20),
			jsonb_build_array(jsonb_build_object('ano_letivo', $1::text, 'type', $4::text, 'definido_em', NOW(), 'definido_por', $2::text)),
			$2::uuid, NOW(), 1
		)
		ON CONFLICT (chave) DO UPDATE SET
			valor = EXCLUDED.valor,
			ano_letivo_atual = EXCLUDED.ano_letivo_atual,
			anos_letivos_lista = CASE
				WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(projection_sistema_config.anos_letivos_lista, '[]'::jsonb)) elem WHERE elem->>'ano_letivo' = EXCLUDED.ano_letivo_atual)
				THEN COALESCE(projection_sistema_config.anos_letivos_lista, '[]'::jsonb)
				ELSE COALESCE(projection_sistema_config.anos_letivos_lista, '[]'::jsonb) || jsonb_build_array(jsonb_build_object('ano_letivo', EXCLUDED.ano_letivo_atual, 'type', $4::text, 'definido_em', NOW(), 'definido_por', EXCLUDED.definido_por::text))
			END,
			definido_por = EXCLUDED.definido_por, updated_at = NOW(), version = COALESCE(projection_sistema_config.version, 0) + 1
	`, anoLetivo, userID, ChaveAnoLetivoGlobal(tipo), tipo)
	return err
}

func ProximoAnoLetivo(atual string) (string, error) {
	return ProximoAnoLetivoValidado(atual)
}
