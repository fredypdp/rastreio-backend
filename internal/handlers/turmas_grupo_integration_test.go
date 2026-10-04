package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/projections"
)

// aplicarEventosDaTurmaNaProjecao leva os eventos ainda não persistidos do agregado
// até a projeção, na mesma ordem. Escritas reais no ledger só chegam à projeção
// pelo Projection Manager assíncrono, que não sobe em testes isolados do pacote.
func aplicarEventosDaTurmaNaProjecao(t *testing.T, proj *projections.TurmasProjection, turma *aggregates.Turma, versaoInicial int) int {
	t.Helper()
	versao := versaoInicial
	for _, ev := range turma.GetUncommittedEvents() {
		versao++
		payload, err := json.Marshal(ev.GetPayload())
		if err != nil {
			t.Fatal(err)
		}
		if err := proj.Handle(db.Event{
			EventID:       uuid.New(),
			AggregateID:   turma.ID,
			AggregateType: "Turma",
			EventType:     ev.GetEventType(),
			EventVersion:  versao,
			Payload:       payload,
			OccurredAt:    time.Now(),
		}); err != nil {
			t.Fatalf("projeção falhou em %s: %v", ev.GetEventType(), err)
		}
	}
	turma.ClearUncommittedEvents()
	return versao
}

func TestIntegrationProjecaoDeGrupoDoQuartoAnoMedio(t *testing.T) {
	client := integrationFinanceClient(t)
	codigoAcademia := "GRP" + uuid.NewString()[:6]
	seedAcademiaParaCategoriaServico(t, client, codigoAcademia)
	proj := projections.NewTurmasProjection(client)

	// 4º ano médio: grupo com tema.
	tema := "Sistema de gestão escolar"
	grupo := aggregates.NewTurma()
	if err := grupo.Criar("GRUPO_1", codigoAcademia, "4_ano_medio", nil, "manha", &tema, uuid.New()); err != nil {
		t.Fatal(err)
	}
	versao := aplicarEventosDaTurmaNaProjecao(t, proj, grupo, 0)

	dto, err := proj.GetByCodigoTurma("GRUPO_1", codigoAcademia)
	if err != nil || dto == nil {
		t.Fatalf("grupo não encontrado na projeção: %v", err)
	}
	if dto.TipoAgrupamento != "grupo" {
		t.Fatalf("tipo_agrupamento = %q, esperado grupo", dto.TipoAgrupamento)
	}
	if dto.TemaTrabalho == nil || *dto.TemaTrabalho != tema {
		t.Fatalf("tema_trabalho = %v, esperado %q", dto.TemaTrabalho, tema)
	}

	// Atualização do tema.
	novo := "Tema revisto"
	if err := grupo.AtualizarDados(nil, nil, nil, &novo, uuid.New()); err != nil {
		t.Fatal(err)
	}
	versao = aplicarEventosDaTurmaNaProjecao(t, proj, grupo, versao)
	dto, _ = proj.GetByID(grupo.ID)
	if dto == nil || dto.TemaTrabalho == nil || *dto.TemaTrabalho != novo {
		t.Fatalf("tema deveria ser atualizado para %q, obteve %+v", novo, dto)
	}

	// Remoção do tema (string vazia).
	vazio := ""
	if err := grupo.AtualizarDados(nil, nil, nil, &vazio, uuid.New()); err != nil {
		t.Fatal(err)
	}
	aplicarEventosDaTurmaNaProjecao(t, proj, grupo, versao)
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

func TestIntegrationProjecaoDeTurmaNormalNaoTemTema(t *testing.T) {
	client := integrationFinanceClient(t)
	codigoAcademia := "TUR" + uuid.NewString()[:6]
	seedAcademiaParaCategoriaServico(t, client, codigoAcademia)
	proj := projections.NewTurmasProjection(client)

	turma := aggregates.NewTurma()
	if err := turma.Criar("TURMA_1", codigoAcademia, "3_ano_medio", nil, "tarde", nil, uuid.New()); err != nil {
		t.Fatal(err)
	}
	aplicarEventosDaTurmaNaProjecao(t, proj, turma, 0)

	dto, err := proj.GetByCodigoTurma("TURMA_1", codigoAcademia)
	if err != nil || dto == nil {
		t.Fatalf("turma não encontrada: %v", err)
	}
	if dto.TipoAgrupamento != "turma" || dto.TemaTrabalho != nil {
		t.Fatalf("turma comum deve ter tipo turma e sem tema, obteve %q / %v", dto.TipoAgrupamento, dto.TemaTrabalho)
	}
}

// --- fluxo HTTP completo (handlers reais + ledger + projeção) -----------------

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

// projetarNovosEventosDaTurma leva para a projeção os eventos do ledger que ainda
// não foram aplicados (o Projection Manager assíncrono não sobe em testes de handler).
func projetarNovosEventosDaTurma(t *testing.T, client *db.Client, proj *projections.TurmasProjection, turmaID uuid.UUID, jaAplicados int) int {
	t.Helper()
	eventos, err := db.NewAggregateRepository(client).GetEventHistory(turmaID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range eventos[jaAplicados:] {
		if err := proj.Handle(ev); err != nil {
			t.Fatalf("projeção falhou em %s: %v", ev.EventType, err)
		}
	}
	return len(eventos)
}

func TestIntegrationHandlersDeGrupoDoQuartoAnoMedioComTema(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	codigoAcademia := "HGR" + uuid.NewString()[:6]
	academiaID := seedAcademiaParaCategoriaServico(t, client, codigoAcademia)
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

	// O tema (já sem espaços) tem de estar no evento gravado no ledger.
	eventos, err := db.NewAggregateRepository(client).GetEventHistory(criada.ID)
	if err != nil || len(eventos) != 1 {
		t.Fatalf("esperado 1 evento no ledger: %d %v", len(eventos), err)
	}
	var payload struct{ TemaTrabalho *string }
	_ = json.Unmarshal(eventos[0].Payload, &payload)
	if payload.TemaTrabalho == nil || *payload.TemaTrabalho != "Sistema de gestão escolar" {
		t.Fatalf("tema no evento TurmaCriada = %v", payload.TemaTrabalho)
	}
	aplicados := projetarNovosEventosDaTurma(t, client, proj, criada.ID, 0)

	// 2) PUT /academia/turma/:codigo/dados — rota real de atualização (AtualizarDadosTurma).
	ctx, rec = ctxTurmaHandler(client, academiaID, http.MethodPut, "/academia/turma/"+criada.CodigoTurma+"/dados", criada.CodigoTurma, map[string]any{"tema_trabalho": "Tema revisto"})
	AtualizarDadosTurma(ctx)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "grupo atualizado com sucesso") {
		t.Fatalf("atualizar tema: %d %s", rec.Code, rec.Body.String())
	}
	aplicados = projetarNovosEventosDaTurma(t, client, proj, criada.ID, aplicados)
	dto, _ := proj.GetByID(criada.ID)
	if dto == nil || dto.TemaTrabalho == nil || *dto.TemaTrabalho != "Tema revisto" || dto.TipoAgrupamento != "grupo" {
		t.Fatalf("projeção após atualizar tema: %+v", dto)
	}

	// 3) Atualizar só o turno (tema omitido) não pode apagar o tema.
	ctx, rec = ctxTurmaHandler(client, academiaID, http.MethodPut, "/x", criada.CodigoTurma, map[string]any{"turno": "tarde"})
	AtualizarDadosTurma(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("atualizar turno: %d %s", rec.Code, rec.Body.String())
	}
	aplicados = projetarNovosEventosDaTurma(t, client, proj, criada.ID, aplicados)
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
	projetarNovosEventosDaTurma(t, client, proj, criada.ID, aplicados)
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
		t.Fatal("a turma rejeitada não pode existir na projeção")
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
}
