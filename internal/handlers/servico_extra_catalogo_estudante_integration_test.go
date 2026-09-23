package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
)

func seedEstudanteComAnoEscolar(t *testing.T, client *db.Client, codigoAcademia, anoEscolarFundamental string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := client.DB().Exec(`
		INSERT INTO projection_estudantes (id, nome, codigo_estudante, senha_hash, telefone, codigo_academia, status, genero, data_nascimento, ano_escolar_fundamental, created_at, updated_at)
		VALUES ($1, 'Estudante Catálogo', $2, 'hash', $3, $4, 'ativo', 'feminino', '2012-05-10', $5, now(), now())
	`, id, "E"+geraDigitos(6), "9"+geraDigitos(8), codigoAcademia, anoEscolarFundamental)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func seedServicoExtraCatalogo(t *testing.T, client *db.Client, codigoAcademia, nome string, anosDisponiveis []string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := client.DB().Exec(`
		INSERT INTO projection_servicos_extras
			(id, codigo_academia, nome, descricao, pago, metodos_pagamento, tem_taxa_inscricao, metodos_pagamento_taxa_inscricao,
			 anos_academicos_disponiveis, cursos_disponiveis, documento_obrigatorio, documento_instrucoes, detalhes_personalizados,
			 ativo, criado_por, created_at, updated_at, version, last_event_id)
		VALUES ($1, $2, $3, '', false, '{}', false, '{}', $4, '{}', false, '', '{}', true, $5, now(), now(), 1, $6)
	`, id, codigoAcademia, nome, pqArray(anosDisponiveis), uuid.New(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// Tarefa 112: ListarServicosExtrasCatalogoEstudante é novo — antes o
// catálogo do estudante (/servicos-extras/catalogo no frontend) usava a
// rota pública ListarServicosExtrasPublico, que devolve TODOS os serviços
// ativos da academia sem filtrar por elegibilidade (ano/curso do
// estudante). Esta rota nova filtra com a mesma regra que
// SolicitarServicoExtra já usava para aceitar/rejeitar a inscrição
// (elegivelParaServicoExtra) — o estudante só vê o que pode de fato
// solicitar.
func TestIntegrationListarServicosExtrasCatalogoEstudanteFiltraPorElegibilidade(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	academia := "CAT" + uuid.NewString()[:6]
	seedAcademiaParaCategoriaServico(t, client, academia)
	idEstudante := seedEstudanteComAnoEscolar(t, client, academia, "7_ano_fundamental")

	idElegivel := seedServicoExtraCatalogo(t, client, academia, "Xadrez", []string{"7_ano_fundamental"})
	idOutroAno := seedServicoExtraCatalogo(t, client, academia, "Robótica", []string{"9_ano_fundamental"})
	idSemRestricao := seedServicoExtraCatalogo(t, client, academia, "Biblioteca", nil)
	seedCategoriaServico(t, client, academia, "Atividades Extracurriculares")

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/estudante/servicos-extras/catalogo", nil)
	ctx.Set("dbClient", client)
	ctx.Set("user_id", idEstudante)
	ctx.Set("user_type", "estudante")

	ListarServicosExtrasCatalogoEstudante(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		ServicosExtras []struct {
			ID string `json:"id"`
		} `json:"servicos_extras"`
		CategoriasServico []struct {
			Nome string `json:"nome"`
		} `json:"categorias_servico"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.CategoriasServico) != 1 || body.CategoriasServico[0].Nome != "Atividades Extracurriculares" {
		t.Errorf("categorias_servico = %+v, queria 1 categoria \"Atividades Extracurriculares\" — o catálogo do estudante precisa dela para não depender de GET /academia/categorias-servico, que rejeita estudantes", body.CategoriasServico)
	}
	if body.Total != 2 {
		t.Fatalf("total = %d, queria 2 (elegível + sem restrição, sem o de outro ano)", body.Total)
	}
	ids := map[string]bool{}
	for _, s := range body.ServicosExtras {
		ids[s.ID] = true
	}
	if !ids[idElegivel.String()] {
		t.Errorf("serviço elegível (%s) não apareceu no catálogo", idElegivel)
	}
	if !ids[idSemRestricao.String()] {
		t.Errorf("serviço sem restrição (%s) não apareceu no catálogo", idSemRestricao)
	}
	if ids[idOutroAno.String()] {
		t.Errorf("serviço de outro ano (%s) apareceu no catálogo — deveria ter sido filtrado", idOutroAno)
	}
}

// pqArray formata um []string para o literal de array do Postgres
// ('{"a","b"}'), incluindo o caso nil/vazio ('{}') — usado só neste
// arquivo de teste para o INSERT direto em projection_servicos_extras.
func pqArray(items []string) string {
	if len(items) == 0 {
		return "{}"
	}
	out := "{"
	for i, it := range items {
		if i > 0 {
			out += ","
		}
		out += `"` + it + `"`
	}
	return out + "}"
}
