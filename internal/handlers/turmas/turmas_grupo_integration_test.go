package turmas

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/cadastros"
	"spuri/internal/db"
	"spuri/internal/handlers/testsupport"
	"spuri/internal/projections"
)

// criarTurmaCRUD cria uma turma pelo repositório CRUD (como o handler faz).
func criarTurmaCRUD(t *testing.T, client *db.Client, codigo, codigoAcademia, nivel, turno string, tema *string) *cadastros.Turma {
	t.Helper()
	turma, err := cadastros.NovaTurma(codigo, codigoAcademia, nivel, nil, turno, tema)
	if err != nil {
		t.Fatal(err)
	}
	audit := db.AuditContext{UserID: uuid.New().String(), UserType: "academia"}
	if err := cadastros.NovoTurmasRepo(client).Criar(context.Background(), audit, turma); err != nil {
		t.Fatal(err)
	}
	return turma
}

func TestIntegrationRegistroDeGrupoDoQuartoAnoMedio(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	codigoAcademia := "GRP" + uuid.NewString()[:6]
	testsupport.SeedAcademiaParaCategoriaServico(t, client, codigoAcademia)
	proj := projections.NewTurmasProjection(client)
	repo := cadastros.NovoTurmasRepo(client)
	audit := db.AuditContext{UserID: uuid.New().String(), UserType: "academia"}

	// 4º ano médio: grupo com tema. A leitura é imediata (não há mais projeção).
	tema := "Sistema de gestão escolar"
	grupo := criarTurmaCRUD(t, client, "GRUPO_1", codigoAcademia, "4_ano_medio", "manha", &tema)

	dto, err := proj.GetByCodigoTurma("GRUPO_1", codigoAcademia)
	if err != nil || dto == nil {
		t.Fatalf("grupo não encontrado: %v", err)
	}
	if dto.TipoAgrupamento != "grupo" {
		t.Fatalf("tipo_agrupamento = %q, esperado grupo", dto.TipoAgrupamento)
	}
	if dto.TemaTrabalho == nil || *dto.TemaTrabalho != tema {
		t.Fatalf("tema_trabalho = %v, esperado %q", dto.TemaTrabalho, tema)
	}

	// Atualização do tema.
	novo := "Tema revisto"
	if _, err := repo.Atualizar(context.Background(), audit, grupo.ID, "TurmaDadosAtualizados", "",
		func(tm *cadastros.Turma) error { return tm.AtualizarDados(nil, nil, nil, &novo) }); err != nil {
		t.Fatal(err)
	}
	dto, _ = proj.GetByID(grupo.ID)
	if dto == nil || dto.TemaTrabalho == nil || *dto.TemaTrabalho != novo {
		t.Fatalf("tema deveria ser atualizado para %q, obteve %+v", novo, dto)
	}

	// Remoção do tema (string vazia).
	vazio := ""
	if _, err := repo.Atualizar(context.Background(), audit, grupo.ID, "TurmaDadosAtualizados", "",
		func(tm *cadastros.Turma) error { return tm.AtualizarDados(nil, nil, nil, &vazio) }); err != nil {
		t.Fatal(err)
	}
	dto, _ = proj.GetByID(grupo.ID)
	if dto == nil || dto.TemaTrabalho != nil {
		t.Fatalf("tema deveria ter sido removido, obteve %+v", dto)
	}

	// Listagem por academia também devolve o tipo.
	lista, err := proj.GetByAcademia(codigoAcademia)
	if err != nil || len(lista) != 1 || lista[0].TipoAgrupamento != "grupo" {
		t.Fatalf("GetByAcademia deveria devolver 1 grupo, obteve %+v (err=%v)", lista, err)
	}
}

func TestIntegrationRegistroDeTurmaNormalNaoTemTema(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)
	codigoAcademia := "TUR" + uuid.NewString()[:6]
	testsupport.SeedAcademiaParaCategoriaServico(t, client, codigoAcademia)
	proj := projections.NewTurmasProjection(client)

	criarTurmaCRUD(t, client, "TURMA_1", codigoAcademia, "3_ano_medio", "tarde", nil)

	dto, err := proj.GetByCodigoTurma("TURMA_1", codigoAcademia)
	if err != nil || dto == nil {
		t.Fatalf("turma não encontrada: %v", err)
	}
	if dto.TipoAgrupamento != "turma" || dto.TemaTrabalho != nil {
		t.Fatalf("turma comum deve ter tipo turma e sem tema, obteve %q / %v", dto.TipoAgrupamento, dto.TemaTrabalho)
	}
}

// --- fluxo HTTP completo (handlers reais + banco) ----------------------------

func ctxTurmaHandler(client *db.Client, academiaID uuid.UUID, metodo, rota, codigo string, body any) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	ctx.Request = httptest.NewRequest(metodo, rota, &buf)
	ctx.Request.Header.Set("Content-Type", "application/json")
	if codigo != "" {
		ctx.Params = gin.Params{{Key: "codigo", Value: codigo}}
	}
	ctx.Set("dbClient", client)
	ctx.Set("repository", db.NewAggregateRepository(client))
	ctx.Set("user_id", academiaID)
	ctx.Set("user_type", "academia")
	return ctx, rec
}

func TestIntegrationHandlersDeGrupoDoQuartoAnoMedioComTema(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := testsupport.IntegrationFinanceClient(t)
	codigoAcademia := "HGR" + uuid.NewString()[:6]
	academiaID := testsupport.SeedAcademiaParaCategoriaServico(t, client, codigoAcademia)
	proj := projections.NewTurmasProjection(client)

	// 1) POST /academia/turma — grupo do 4º ano médio com tema (com espaços nas pontas).
	ctx, rec := ctxTurmaHandler(client, academiaID, http.MethodPost, "/academia/turma", "", map[string]any{
		"codigo_turma": "G1", "nivel": "4_ano_medio", "turno": "manha", "tema_trabalho": "  Sistema de gestão escolar  ",
	})
	CriarTurma(ctx)
	if rec.Code != http.StatusCreated {
		t.Fatalf("criar grupo: %d %s", rec.Code, rec.Body.String())
	}
	var criada struct {
		Message         string    `json:"message"`
		ID              uuid.UUID `json:"id"`
		CodigoTurma     string    `json:"codigo_turma"`
		TipoAgrupamento string    `json:"tipo_agrupamento"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &criada)
	if criada.TipoAgrupamento != "grupo" || criada.Message != "grupo criado com sucesso" {
		t.Fatalf("resposta de criação inesperada: %+v", criada)
	}

	// O tema (já sem espaços) está no registro e a criação ficou auditada.
	dto, _ := proj.GetByID(criada.ID)
	if dto == nil || dto.TemaTrabalho == nil || *dto.TemaTrabalho != "Sistema de gestão escolar" {
		t.Fatalf("tema no registro = %+v", dto)
	}
	linhas, err := client.ListarAuditoria(context.Background(), cadastros.EntidadeTurma, criada.ID.String(), 10, 0)
	if err != nil || len(linhas) != 1 || linhas[0].Acao != "TurmaCriada" {
		t.Fatalf("esperada 1 linha de auditoria TurmaCriada: %+v %v", linhas, err)
	}

	// 2) PUT /academia/turma/:codigo/dados — rota real de atualização.
	ctx, rec = ctxTurmaHandler(client, academiaID, http.MethodPut, "/academia/turma/"+criada.CodigoTurma+"/dados", criada.CodigoTurma, map[string]any{"tema_trabalho": "Tema revisto"})
	AtualizarDadosTurma(ctx)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "grupo atualizado com sucesso") {
		t.Fatalf("atualizar tema: %d %s", rec.Code, rec.Body.String())
	}
	dto, _ = proj.GetByID(criada.ID)
	if dto == nil || dto.TemaTrabalho == nil || *dto.TemaTrabalho != "Tema revisto" || dto.TipoAgrupamento != "grupo" {
		t.Fatalf("registro após atualizar tema: %+v", dto)
	}

	// 3) Atualizar só o turno (tema omitido) não pode apagar o tema.
	ctx, rec = ctxTurmaHandler(client, academiaID, http.MethodPut, "/x", criada.CodigoTurma, map[string]any{"turno": "tarde"})
	AtualizarDadosTurma(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("atualizar turno: %d %s", rec.Code, rec.Body.String())
	}
	dto, _ = proj.GetByID(criada.ID)
	if dto == nil || dto.TemaTrabalho == nil || *dto.TemaTrabalho != "Tema revisto" || dto.Turno != "tarde" {
		t.Fatalf("omitir tema_trabalho não pode removê-lo: %+v", dto)
	}

	// 4) tema_trabalho "" remove o tema.
	ctx, rec = ctxTurmaHandler(client, academiaID, http.MethodPut, "/x", criada.CodigoTurma, map[string]any{"tema_trabalho": ""})
	AtualizarDadosTurma(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("remover tema: %d %s", rec.Code, rec.Body.String())
	}
	dto, _ = proj.GetByID(criada.ID)
	if dto == nil || dto.TemaTrabalho != nil {
		t.Fatalf("tema deveria ter sido removido: %+v", dto)
	}

	// 5) Tema fora do 4º ano médio é rejeitado com 400 e nada é gravado.
	ctx, rec = ctxTurmaHandler(client, academiaID, http.MethodPost, "/academia/turma", "", map[string]any{
		"codigo_turma": "T3A", "nivel": "3_ano_medio", "turno": "manha", "tema_trabalho": "Tema proibido",
	})
	CriarTurma(ctx)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "tema_trabalho") {
		t.Fatalf("tema numa turma do 3º ano deveria dar 400: %d %s", rec.Code, rec.Body.String())
	}
	if existente, _ := proj.GetByCodigoTurma("T3A", codigoAcademia); existente != nil {
		t.Fatal("a turma rejeitada não pode existir")
	}

	// 6) Turma comum continua a dizer "turma".
	ctx, rec = ctxTurmaHandler(client, academiaID, http.MethodPost, "/academia/turma", "", map[string]any{
		"codigo_turma": "T3B", "nivel": "3_ano_medio", "turno": "manha",
	})
	CriarTurma(ctx)
	var comum struct {
		Message         string `json:"message"`
		TipoAgrupamento string `json:"tipo_agrupamento"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &comum)
	if rec.Code != http.StatusCreated || comum.TipoAgrupamento != "turma" || comum.Message != "turma criada com sucesso" {
		t.Fatalf("turma comum: %d %+v", rec.Code, comum)
	}

	// 7) Código repetido na mesma academia: 400, e não cria outra.
	ctx, rec = ctxTurmaHandler(client, academiaID, http.MethodPost, "/academia/turma", "", map[string]any{
		"codigo_turma": "T3B", "nivel": "3_ano_medio", "turno": "tarde",
	})
	CriarTurma(ctx)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "já existe") {
		t.Fatalf("código repetido deveria dar 400: %d %s", rec.Code, rec.Body.String())
	}
}
