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

func seedAcademiaParaCategoriaServico(t *testing.T, client *db.Client, codigo string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := client.DB().Exec(`INSERT INTO projection_academias
		(id,nivel,nome,nif,codigo_academia,senha_hash,provincia,endereco,nivel_escolar,status,cursos,anos_academicos,type,ano_letivo,created_at)
		VALUES ($1,'escola','Academia categoria servico',$2,$3,'hash','LUA','endereco','fundamental','ativo','[]'::jsonb,'["7_ano_fundamental"]'::jsonb,'private','2026_2027',CURRENT_TIMESTAMP)`,
		id, geraDigitos(10), codigo)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// seedCategoriaServico grava direto na projection (em vez de passar pelo
// fluxo real via CriarCategoriaServico/ledger) de propósito: uma escrita no
// ledger só fica visível numa projection depois que o Projection Manager
// (assíncrono, acordado por notifyLedgerWritten — ver internal/db/
// ledger_hook.go) processa o evento; um teste isolado do pacote handlers,
// sem subir esse worker, não tem como esperar por isso de forma
// determinística. GetCategoriaServico só lê da projection, então testar o
// endpoint não depende de como a linha chegou lá.
func seedCategoriaServico(t *testing.T, client *db.Client, codigoAcademia, nome string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := client.DB().Exec(`INSERT INTO projection_categorias_servico
		(id,codigo_academia,nome,ativo,created_at,updated_at,version)
		VALUES ($1,$2,$3,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1)`,
		id, codigoAcademia, nome)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// GetCategoriaServico é novo (Tarefa 110): antes não existia nenhuma rota
// para buscar uma única categoria de serviço pelo id — a tela de edição do
// frontend contornava isso buscando a lista inteira e filtrando pelo id no
// cliente. Este teste cobre, para o novo endpoint, o mesmo contrato de
// autorização que GetServicoExtra já tem: a academia dona vê; outra
// academia recebe 403; um id inexistente retorna 404.
func TestIntegrationGetCategoriaServicoRespeitaEscopoDaAcademia(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	academiaDona := "CAT" + uuid.NewString()[:6]
	academiaOutra := "OUT" + uuid.NewString()[:6]
	idDona := seedAcademiaParaCategoriaServico(t, client, academiaDona)
	idOutra := seedAcademiaParaCategoriaServico(t, client, academiaOutra)
	idCategoria := seedCategoriaServico(t, client, academiaDona, "Transporte")

	newGetCtx := func(id string, userID uuid.UUID) (*gin.Context, *httptest.ResponseRecorder) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/academia/categorias-servico/"+id, nil)
		ctx.Params = gin.Params{{Key: "id", Value: id}}
		ctx.Set("dbClient", client)
		ctx.Set("user_id", userID)
		ctx.Set("user_type", "academia")
		return ctx, recorder
	}

	// 1) A própria academia consegue buscar, e vê o nome correto.
	ctx, rec := newGetCtx(idCategoria.String(), idDona)
	GetCategoriaServico(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("get categoria (dona): esperava 200, obteve %d: %s", rec.Code, rec.Body.String())
	}
	var obtido struct {
		Data struct {
			Nome string `json:"nome"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &obtido); err != nil {
		t.Fatal(err)
	}
	if obtido.Data.Nome != "Transporte" {
		t.Fatalf("nome retornado = %q, queria %q", obtido.Data.Nome, "Transporte")
	}

	// 2) Outra academia recebe 403 (mesmo contrato de GetServicoExtra).
	ctx, rec = newGetCtx(idCategoria.String(), idOutra)
	GetCategoriaServico(ctx)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("get categoria (outra academia): esperava 403, obteve %d: %s", rec.Code, rec.Body.String())
	}

	// 3) Id inexistente -> 404.
	ctx, rec = newGetCtx(uuid.NewString(), idDona)
	GetCategoriaServico(ctx)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get categoria (inexistente): esperava 404, obteve %d: %s", rec.Code, rec.Body.String())
	}
}
