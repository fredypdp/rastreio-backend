package testsupport

import (
	"testing"

	"github.com/google/uuid"
	"spuri/internal/db"
)

func SeedAcademiaParaCategoriaServico(t *testing.T, client *db.Client, codigo string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := client.DB().Exec(`INSERT INTO projection_academias
		(id,nivel,nome,nif,codigo_academia,senha_hash,provincia,endereco,nivel_escolar,status,cursos,anos_academicos,type,ano_letivo,created_at)
		VALUES ($1,'escola','Academia categoria servico',$2,$3,'hash','LUA','endereco','fundamental','ativo','[]'::jsonb,'["7_ano_fundamental"]'::jsonb,'private','2026_2027',CURRENT_TIMESTAMP)`,
		id, GeraDigitos(10), codigo)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func SeedEstudanteComAnoEscolar(t *testing.T, client *db.Client, codigoAcademia, anoEscolarFundamental string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := client.DB().Exec(`
		INSERT INTO projection_estudantes (id, nome, codigo_estudante, senha_hash, telefone, codigo_academia, status, genero, data_nascimento, ano_escolar_fundamental, created_at, updated_at)
		VALUES ($1, 'Estudante Catálogo', $2, 'hash', $3, $4, 'ativo', 'feminino', '2012-05-10', $5, now(), now())
	`, id, "E"+GeraDigitos(6), "9"+GeraDigitos(8), codigoAcademia, anoEscolarFundamental)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
