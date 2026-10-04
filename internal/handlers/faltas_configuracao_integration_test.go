package handlers

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/projections"
)

func ctxConfigFaltas(client *db.Client, academiaID uuid.UUID, metodo string, body any) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	ctx.Request = httptest.NewRequest(metodo, "/academia/faltas/configuracao", &buf)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("dbClient", client)
	ctx.Set("repository", db.NewAggregateRepository(client))
	ctx.Set("user_id", academiaID)
	ctx.Set("user_type", "academia")
	return ctx, rec
}

// levarLedgerDaConfiguracaoParaProjecao aplica na projeção os eventos já gravados
// no ledger (o Projection Manager assíncrono não sobe em testes de handler).
func levarLedgerDaConfiguracaoParaProjecao(t *testing.T, client *db.Client, codigoAcademia string) {
	t.Helper()
	eventos, err := db.NewAggregateRepository(client).GetEventHistory(aggregates.ConfiguracaoFaltasAggregateID(codigoAcademia))
	if err != nil {
		t.Fatal(err)
	}
	proj := projections.NewFaltasConfiguracaoProjection(client)
	for _, ev := range eventos {
		if err := proj.Handle(ev); err != nil {
			t.Fatalf("projeção falhou: %v", err)
		}
	}
}

type respostaConfigFaltas struct {
	Data struct {
		Limite     *int `json:"limite_faltas_por_periodo"`
		Reprovacao bool `json:"reprovacao_por_faltas"`
	} `json:"data"`
}

func TestIntegrationConfiguracaoFaltasPutEGet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	codigo := "CFG" + uuid.NewString()[:6]
	academiaID := seedAcademiaParaCategoriaServico(t, client, codigo)

	// Sem configuração salva: GET devolve padrão (sem limite, reprovação desligada).
	ctx, rec := ctxConfigFaltas(client, academiaID, http.MethodGet, nil)
	GetConfiguracaoFaltas(ctx)
	var r respostaConfigFaltas
	if rec.Code != http.StatusOK {
		t.Fatalf("GET inicial: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if r.Data.Limite != nil || r.Data.Reprovacao {
		t.Fatalf("padrão deveria ser sem limite e sem reprovação: %+v", r.Data)
	}

	// Entradas inválidas.
	invalidos := []map[string]any{
		{"reprovacao_por_faltas": true},
		{"limite_faltas_por_periodo": 0},
		{"limite_faltas_por_periodo": 501},
		{"limite_faltas_por_periodo": -3, "reprovacao_por_faltas": true},
	}
	for _, body := range invalidos {
		ctx, rec = ctxConfigFaltas(client, academiaID, http.MethodPut, body)
		DefinirConfiguracaoFaltas(ctx)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("PUT %v deveria dar 400, deu %d: %s", body, rec.Code, rec.Body.String())
		}
	}

	// Salvar limite 5 + reprovação: a resposta já devolve os valores salvos.
	ctx, rec = ctxConfigFaltas(client, academiaID, http.MethodPut, map[string]any{"limite_faltas_por_periodo": 5, "reprovacao_por_faltas": true})
	DefinirConfiguracaoFaltas(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT válido: %d %s", rec.Code, rec.Body.String())
	}
	r = respostaConfigFaltas{}
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if r.Data.Limite == nil || *r.Data.Limite != 5 || !r.Data.Reprovacao {
		t.Fatalf("resposta do PUT deveria trazer limite 5 + reprovação: %+v", r.Data)
	}
	levarLedgerDaConfiguracaoParaProjecao(t, client, codigo)
	ctx, rec = ctxConfigFaltas(client, academiaID, http.MethodGet, nil)
	GetConfiguracaoFaltas(ctx)
	r = respostaConfigFaltas{}
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if r.Data.Limite == nil || *r.Data.Limite != 5 || !r.Data.Reprovacao {
		t.Fatalf("GET depois do PUT: %+v", r.Data)
	}

	// Segundo PUT substitui no MESMO agregado (versão 2) e desliga a reprovação.
	ctx, rec = ctxConfigFaltas(client, academiaID, http.MethodPut, map[string]any{"limite_faltas_por_periodo": 8, "reprovacao_por_faltas": false})
	DefinirConfiguracaoFaltas(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("segundo PUT: %d %s", rec.Code, rec.Body.String())
	}
	levarLedgerDaConfiguracaoParaProjecao(t, client, codigo)
	ctx, rec = ctxConfigFaltas(client, academiaID, http.MethodGet, nil)
	GetConfiguracaoFaltas(ctx)
	r = respostaConfigFaltas{}
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if r.Data.Limite == nil || *r.Data.Limite != 8 || r.Data.Reprovacao {
		t.Fatalf("GET depois do segundo PUT: %+v", r.Data)
	}
	repo := db.NewAggregateRepository(client)
	aggID := aggregates.ConfiguracaoFaltasAggregateID(codigo)
	eventos, err := repo.GetEventHistory(aggID)
	if err != nil || len(eventos) != 2 {
		t.Fatalf("os dois PUTs válidos devem virar 2 eventos no MESMO agregado: eventos=%d err=%v", len(eventos), err)
	}
	agg, err := repo.Load(aggID, "ConfiguracaoFaltas")
	if err != nil {
		t.Fatal(err)
	}
	cfg, ok := agg.(*aggregates.ConfiguracaoFaltas)
	if !ok || cfg.LimiteFaltasPorPeriodo == nil || *cfg.LimiteFaltasPorPeriodo != 8 || cfg.ReprovacaoPorFaltas {
		t.Fatalf("estado reconstruído do ledger inesperado: %+v", agg)
	}

	// Remover o limite (null) desliga tudo.
	ctx, rec = ctxConfigFaltas(client, academiaID, http.MethodPut, map[string]any{"limite_faltas_por_periodo": nil, "reprovacao_por_faltas": false})
	DefinirConfiguracaoFaltas(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT remover limite: %d %s", rec.Code, rec.Body.String())
	}
}

func TestIntegrationConfiguracaoFaltasIsoladaPorAcademia(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	codA := "ISA" + uuid.NewString()[:6]
	codB := "ISB" + uuid.NewString()[:6]
	idA := seedAcademiaParaCategoriaServico(t, client, codA)
	idB := seedAcademiaParaCategoriaServico(t, client, codB)

	ctx, rec := ctxConfigFaltas(client, idA, http.MethodPut, map[string]any{"limite_faltas_por_periodo": 4, "reprovacao_por_faltas": true})
	DefinirConfiguracaoFaltas(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT academia A: %d %s", rec.Code, rec.Body.String())
	}
	levarLedgerDaConfiguracaoParaProjecao(t, client, codA)

	ctx, rec = ctxConfigFaltas(client, idB, http.MethodGet, nil)
	GetConfiguracaoFaltas(ctx)
	var r respostaConfigFaltas
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if r.Data.Limite != nil || r.Data.Reprovacao {
		t.Fatalf("a academia B não pode herdar a configuração da A: %+v", r.Data)
	}
}

// --- cálculo completo da avaliação final com a configuração de faltas --------

func seedMateriaFundamental(t *testing.T, client *db.Client, codigoAcademia, nome string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := client.DB().Exec(`INSERT INTO projection_materias (id,nome,type,anos_academicos,codigo_academia,status,created_at,updated_at,version)
		VALUES ($1,$2,'fundamental','["7_ano_fundamental"]'::jsonb,$3,'ativo',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1)`, id, nome, codigoAcademia); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedNotaEscolar(t *testing.T, client *db.Client, estudante, academia, anoLectivo, periodo, categoria string, materia uuid.UUID, nota float64) {
	t.Helper()
	if _, err := client.DB().Exec(`INSERT INTO projection_notas
		(codigo_estudante,codigo_academia,ano_lectivo,periodo,materia_disciplinar_id,nota,event_id,version,tipo,categoria)
		VALUES ($1,$2,$3,$4,$5,$6,$7,1,'escolar',$8)`, estudante, academia, anoLectivo, periodo, materia, nota, uuid.New(), categoria); err != nil {
		t.Fatal(err)
	}
}

func seedFalta(t *testing.T, client *db.Client, estudante, academia, anoLectivo, periodo string, materia uuid.UUID, dia int, quantidade int) {
	t.Helper()
	data := time.Date(2026, time.March, dia, 0, 0, 0, 0, time.UTC)
	if _, err := client.DB().Exec(`INSERT INTO projection_faltas
		(codigo_estudante,codigo_academia,ano_lectivo,data,materia_disciplinar_id,quantidade,event_id,version,periodo)
		VALUES ($1,$2,$3,$4,$5,$6,$7,1,$8)`, estudante, academia, anoLectivo, data, materia, quantidade, uuid.New(), periodo); err != nil {
		t.Fatal(err)
	}
}

func definirConfigFaltasNaProjecao(t *testing.T, client *db.Client, academia string, limite *int, reprovacao bool) {
	t.Helper()
	if limite == nil {
		if _, err := client.DB().Exec(`DELETE FROM projection_faltas_configuracao WHERE codigo_academia=$1`, academia); err != nil {
			t.Fatal(err)
		}
		return
	}
	if _, err := client.DB().Exec(`INSERT INTO projection_faltas_configuracao (codigo_academia,limite_faltas_por_periodo,reprovacao_por_faltas,version)
		VALUES ($1,$2,$3,1) ON CONFLICT (codigo_academia) DO UPDATE SET limite_faltas_por_periodo=EXCLUDED.limite_faltas_por_periodo, reprovacao_por_faltas=EXCLUDED.reprovacao_por_faltas`,
		academia, *limite, reprovacao); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationAvaliacaoFinalZeraNotaDoProfessorPorFaltas(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	academia := "AVF" + uuid.NewString()[:6]
	seedAcademiaParaCategoriaServico(t, client, academia)
	estudante := "E" + uuid.NewString()[:6]
	const anoLectivo = "2026_2027"
	materia := seedMateriaFundamental(t, client, academia, "Matemática")
	outraMateria := seedMateriaFundamental(t, client, academia, "Português")
	for _, p := range []string{"1_trimestre", "2_trimestre", "3_trimestre"} {
		seedNotaEscolar(t, client, estudante, academia, anoLectivo, p, "nota_professor", materia, 10)
		seedNotaEscolar(t, client, estudante, academia, anoLectivo, p, "prova_trimestral", materia, 10)
	}
	regra := regraAvaliacaoFinalEscolarFixa(academia, "fundamental", "7_ano_fundamental", "normal", nil, "")
	if regra == nil {
		t.Fatal("regra escolar fixa do 7º ano não encontrada")
	}
	materias := []projections.MateriaDTO{{ID: materia, Nome: "Matemática", Type: "fundamental", CodigoAcademia: academia}}

	calcular := func() ([]aggregates.ResultadoMateriaAvaliacaoFinal, float64, bool) {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		ctx.Set("dbClient", client)
		res, nota, aprovado, _, _, err := calcularResultadoMateriasAvaliacaoFinal(ctx, estudante, academia, anoLectivo, "fundamental", "7_ano_fundamental", *regra, materias, nil)
		if err != nil {
			t.Fatalf("cálculo falhou: %v", err)
		}
		return res, nota, aprovado
	}
	proximo := func(a, b float64) bool { return math.Abs(a-b) < 0.01 }
	cinco, zero := 5, 0
	_ = zero

	// 1) Sem configuração: nada muda, mesmo com muitas faltas.
	seedFalta(t, client, estudante, academia, anoLectivo, "2_trimestre", materia, 2, 4)
	seedFalta(t, client, estudante, academia, anoLectivo, "2_trimestre", materia, 3, 2)
	res, nota, aprovado := calcular()
	if !proximo(nota, 10) || !aprovado || len(res[0].NotasZeradasPorFaltas) != 0 {
		t.Fatalf("sem configuração: nota=%v aprovado=%v zeradas=%+v", nota, aprovado, res[0].NotasZeradasPorFaltas)
	}

	// 2) Só o limite (sem reprovação por faltas): nada muda.
	definirConfigFaltasNaProjecao(t, client, academia, &cinco, false)
	res, nota, aprovado = calcular()
	if !proximo(nota, 10) || !aprovado || len(res[0].NotasZeradasPorFaltas) != 0 {
		t.Fatalf("limite sem reprovação: nota=%v aprovado=%v zeradas=%+v", nota, aprovado, res[0].NotasZeradasPorFaltas)
	}

	// 3) Limite 5 + reprovação, 6 faltas no 2º trimestre (4+2): nota do professor do
	// 2º trimestre vira 0 → (10 + (0+10)/2 + 10)/3 = 8,33 → reprovado (mínimo 10).
	definirConfigFaltasNaProjecao(t, client, academia, &cinco, true)
	res, nota, aprovado = calcular()
	if !proximo(nota, 25.0/3.0) || aprovado {
		t.Fatalf("limite excedido: nota=%v aprovado=%v", nota, aprovado)
	}
	z := res[0].NotasZeradasPorFaltas
	if len(z) != 1 || z[0].Categoria != "nota_professor" || z[0].Periodo != "2_trimestre" || z[0].TotalFaltas != 6 || z[0].LimiteFaltas != 5 {
		t.Fatalf("registro de nota zerada por faltas inesperado: %+v", z)
	}

	// 4) Exatamente no limite (5 faltas): não ultrapassa → nada muda.
	if _, err := client.DB().Exec(`UPDATE projection_faltas SET quantidade = 1 WHERE codigo_estudante=$1 AND materia_disciplinar_id=$2 AND quantidade = 2`, estudante, materia); err != nil {
		t.Fatal(err)
	}
	res, nota, aprovado = calcular()
	if !proximo(nota, 10) || !aprovado || len(res[0].NotasZeradasPorFaltas) != 0 {
		t.Fatalf("no limite exato: nota=%v aprovado=%v zeradas=%+v", nota, aprovado, res[0].NotasZeradasPorFaltas)
	}

	// 5) Faltas de outra matéria e de outro ano letivo não contam.
	seedFalta(t, client, estudante, academia, anoLectivo, "2_trimestre", outraMateria, 5, 20)
	seedFalta(t, client, estudante, academia, "2025_2026", "2_trimestre", materia, 6, 20)
	res, nota, aprovado = calcular()
	if !proximo(nota, 10) || !aprovado || len(res[0].NotasZeradasPorFaltas) != 0 {
		t.Fatalf("faltas alheias não podem contar: nota=%v aprovado=%v zeradas=%+v", nota, aprovado, res[0].NotasZeradasPorFaltas)
	}

	// 6) Faltas no 3º trimestre zeram só a nota do professor do 3º trimestre.
	seedFalta(t, client, estudante, academia, anoLectivo, "3_trimestre", materia, 8, 6)
	res, nota, aprovado = calcular()
	z = res[0].NotasZeradasPorFaltas
	if !proximo(nota, 25.0/3.0) || aprovado || len(z) != 1 || z[0].Periodo != "3_trimestre" {
		t.Fatalf("3º trimestre: nota=%v aprovado=%v zeradas=%+v", nota, aprovado, z)
	}

	// 7) Desligar a reprovação por faltas volta ao cálculo normal.
	definirConfigFaltasNaProjecao(t, client, academia, &cinco, false)
	_, nota, aprovado = calcular()
	if !proximo(nota, 10) || !aprovado {
		t.Fatalf("reprovação desligada: nota=%v aprovado=%v", nota, aprovado)
	}
}
