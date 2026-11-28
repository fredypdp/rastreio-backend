package servicosextras

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"spuri/internal/db"
	"spuri/internal/finance"
	"spuri/internal/handlers/apoio"
	"spuri/internal/handlers/testsupport"
	"spuri/internal/projections"
)

// Testes de integração do catálogo fixo de serviços (Parte 7), pelos handlers.

func newJSONContext(method, path string, body any) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	buf, _ := json.Marshal(body)
	ctx.Request = httptest.NewRequest(method, path, bytes.NewReader(buf))
	ctx.Request.Header.Set("Content-Type", "application/json")
	return ctx, recorder
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("resposta não é JSON: %v: %s", err, rec.Body.String())
	}
}

func credenciaisAppyPayNeutras(academia string) finance.CredentialInput {
	return finance.CredentialInput{
		ContextoTipo: finance.ContextoAcademia, CodigoAcademia: academia, Provedor: finance.ProvedorAppyPay,
		Credenciais: map[string]string{"client_id": "client-handler", "client_secret": "secret-handler", "gpo_method": "GPO_HANDLER", "ref_method": "REF_HANDLER"},
	}
}

// appyPayMock devolve "Pending" na consulta ao vivo da cobrança (o pagamento
// ainda não aconteceu no provedor) ou "Success" (já foi pago), conforme pago.
type appyPayMock struct{ pago bool }

func (m appyPayMock) RoundTrip(req *http.Request) (*http.Response, error) {
	body := `{"id":"provider-charge-svc-` + uuid.NewString() + `","status":"Pending"}`
	id := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
	switch {
	case strings.Contains(req.URL.Path, "/oauth2/token"):
		body = `{"access_token":"test-token","expires_in":3600}`
	case req.Method == http.MethodGet && m.pago:
		body = `{"payment":{"id":"` + id + `","status":"Success","transactionEvents":[{"responseStatus":{"successful":true,"status":"Success","code":100,"source":"REF"}}]}}`
	case req.Method == http.MethodGet:
		body = `{"payment":{"id":"` + id + `","status":"Pending","transactionEvents":[]}}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// provedoresSetup liga o serviço financeiro de teste (provedor simulado) e
// devolve o cliente do banco e uma função que configura a AppyPay da academia.
func provedoresSetup(t *testing.T) (*db.Client, func(academia string)) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("ENV", "test")
	t.Setenv("APPYPAY_RESOURCE", "integration-resource")
	client := testsupport.IntegrationFinanceClient(t)
	anterior := apoio.FinanceiroService
	apoio.FinanceiroService = finance.NewService(client)
	apoio.FinanceiroService.SetHTTPClient(&http.Client{Transport: appyPayMock{}})
	t.Cleanup(func() { apoio.FinanceiroService = anterior })
	return client, func(academia string) {
		if _, _, err := apoio.FinanceiroService.ConfigureCredential(t.Context(), nil, credenciaisAppyPayNeutras(academia), uuid.NewString(), "academia", "127.0.0.1"); err != nil {
			t.Fatalf("configurar provedor: %v", err)
		}
	}
}

func avisoDe(t *testing.T, corpo map[string]any, chave string) string {
	t.Helper()
	v, ok := corpo[chave]
	if !ok || v == nil {
		return ""
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s não é um objeto: %v", chave, v)
	}
	if msg, _ := m["mensagem"].(string); msg == "" {
		t.Fatalf("o aviso %s deve trazer uma mensagem legível: %v", chave, m)
	}
	codigo, _ := m["codigo"].(string)
	return codigo
}

func listaDe(corpo map[string]any, chave string) []any {
	l, _ := corpo[chave].([]any)
	return l
}

// Parte 7: fluxo completo do catálogo fixo pelos handlers — a academia
// configura, o estudante pede, a academia aprova e as pendências respeitam os
// meses cobrados.

type cenarioServicos struct {
	t          *testing.T
	client     *db.Client
	academia   string
	idAcademia uuid.UUID
	idEst      uuid.UUID
	codigoEst  string
}

func novoCenarioServicos(t *testing.T) *cenarioServicos {
	client, configurar := provedoresSetup(t)
	academia := "SVC" + uuid.NewString()[:6]
	c := &cenarioServicos{t: t, client: client, academia: academia}
	c.idAcademia = testsupport.SeedAcademiaParaCategoriaServico(t, client, academia)
	c.idEst = testsupport.SeedEstudanteComAnoEscolar(t, client, academia, "7_ano_fundamental")
	if err := client.DB().QueryRow(`SELECT codigo_estudante FROM projection_estudantes WHERE id=$1`, c.idEst).Scan(&c.codigoEst); err != nil {
		t.Fatal(err)
	}
	configurar(academia)
	return c
}

func (c *cenarioServicos) comoAcademia(ctx *gin.Context) {
	ctx.Set("dbClient", c.client)
	ctx.Set("repository", db.NewAggregateRepository(c.client))
	ctx.Set("user_id", c.idAcademia)
	ctx.Set("user_type", "academia")
}

func (c *cenarioServicos) comoEstudante(ctx *gin.Context) {
	ctx.Set("dbClient", c.client)
	ctx.Set("repository", db.NewAggregateRepository(c.client))
	ctx.Set("user_id", c.idEst)
	ctx.Set("user_type", "estudante")
}

func (c *cenarioServicos) chamar(h gin.HandlerFunc, metodo, caminho string, corpo any, params gin.Params, ator func(*gin.Context)) (map[string]any, int) {
	c.t.Helper()
	ctx, rec := newJSONContext(metodo, caminho, corpo)
	ator(ctx)
	ctx.Params = params
	h(ctx)
	var out map[string]any
	decodeJSON(c.t, rec, &out)
	return out, rec.Code
}

func (c *cenarioServicos) academiaChama(h gin.HandlerFunc, metodo, caminho string, corpo any, params ...gin.Param) (map[string]any, int) {
	return c.chamar(h, metodo, caminho, corpo, params, c.comoAcademia)
}

func (c *cenarioServicos) estudanteChama(h gin.HandlerFunc, metodo, caminho string, corpo any, params ...gin.Param) (map[string]any, int) {
	return c.chamar(h, metodo, caminho, corpo, params, c.comoEstudante)
}

func dado(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	d, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("sem data: %v", out)
	}
	return d
}

// projetar alimenta a projeção da solicitação com os eventos dela (em produção
// o gerenciador de projeções faz isso em segundo plano).
func (c *cenarioServicos) projetar(solID string) {
	c.t.Helper()
	id, err := uuid.Parse(solID)
	if err != nil {
		c.t.Fatal(err)
	}
	eventos, err := db.NewAggregateRepository(c.client).GetEventHistory(id)
	if err != nil {
		c.t.Fatal(err)
	}
	p := projections.NewSolicitacaoServicoExtraProjection(c.client)
	for _, e := range eventos {
		if err := p.Handle(e); err != nil {
			c.t.Fatalf("projetar %s: %v", e.EventType, err)
		}
	}
}

func todosOsMeses() []int { return []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12} }

func TestIntegrationServicosTransporteDoCadastroAAprovacaoEPendencias(t *testing.T) {
	c := novoCenarioServicos(t)

	// A academia define os métodos e uma rota com veículo.
	if o, code := c.academiaChama(AtualizarTransporte, http.MethodPut, "/academia/servicos-extras/transporte",
		map[string]any{"metodos_pagamento": []string{"REF"}}); code != http.StatusOK {
		t.Fatalf("configurar transporte: %d %v", code, o)
	}
	o, code := c.academiaChama(CriarRota, http.MethodPost, "/academia/servicos-extras/transporte/rotas",
		map[string]any{"nome": "Rota Norte", "horario_embarque": "06:30", "preco_modo": "valor", "preco": 5000, "prazo_meses": todosOsMeses()})
	if code != http.StatusCreated {
		t.Fatalf("criar rota: %d %v", code, o)
	}
	rotaID := o["rota_id"].(string)
	// Rota sem veículo ainda não aparece para o estudante.
	catalogo := func() map[string]any {
		out, code := c.estudanteChama(apoio.ListarServicosExtrasCatalogoEstudante, http.MethodGet, "/estudante/servicos-extras/catalogo", nil)
		if code != http.StatusOK {
			t.Fatalf("catálogo: %d %v", code, out)
		}
		return out
	}
	if catalogo()["transporte"] != nil {
		t.Fatalf("transporte sem veículo não pode aparecer: %v", catalogo())
	}
	o, code = c.academiaChama(CriarVeiculo, http.MethodPost, "/academia/servicos-extras/transporte/rotas/"+rotaID+"/veiculos",
		map[string]any{"nome": "Autocarro 1"}, gin.Param{Key: "id", Value: rotaID})
	if code != http.StatusCreated {
		t.Fatalf("criar veículo: %d %v", code, o)
	}
	veiculoID := o["veiculo_id"].(string)
	// Segunda rota com veículo, para testar "veículo de outra rota".
	o2, _ := c.academiaChama(CriarRota, http.MethodPost, "/academia/servicos-extras/transporte/rotas",
		map[string]any{"nome": "Rota Sul", "horario_embarque": "07:00", "preco_modo": "gratuito", "prazo_meses": []int{1}})
	rota2 := o2["rota_id"].(string)
	o3, _ := c.academiaChama(CriarVeiculo, http.MethodPost, "/x", map[string]any{"nome": "Van"}, gin.Param{Key: "id", Value: rota2})
	veiculoOutraRota := o3["veiculo_id"].(string)

	cat := catalogo()
	tr, ok := cat["transporte"].(map[string]any)
	if !ok || len(tr["rotas"].([]any)) != 2 {
		t.Fatalf("catálogo do estudante: %v", cat)
	}
	transporteID := tr["id"].(string)

	// Preço geral com rota que tem preço próprio: 409.
	if o, code := c.academiaChama(AtualizarTransporte, http.MethodPut, "/x",
		map[string]any{"preco_geral_modo": "valor", "preco_geral": 100}); code != http.StatusConflict {
		t.Fatalf("preço geral com rota própria deveria ser 409: %d %v", code, o)
	}

	pedir := func(corpo map[string]any) (map[string]any, int) {
		return c.estudanteChama(SolicitarServicoExtra, http.MethodPost, "/estudante/servicos-extras/"+transporteID+"/solicitacao", corpo,
			gin.Param{Key: "id", Value: transporteID})
	}
	if o, code := pedir(map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("transporte sem rota: %d %v", code, o)
	}
	if o, code := pedir(map[string]any{"rota_id": rotaID, "veiculo_ids": []string{veiculoOutraRota}}); code != http.StatusBadRequest {
		t.Fatalf("veículo de outra rota: %d %v", code, o)
	}
	o, code = pedir(map[string]any{"rota_id": rotaID, "veiculo_ids": []string{veiculoID}})
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("pedir transporte: %d %v", code, o)
	}
	solID := idDaSolicitacao(t, o)
	c.projetar(solID)
	// Já tem inscrição ativa no transporte: não pode pedir outra rota.
	if o, code := pedir(map[string]any{"rota_id": rota2, "veiculo_ids": []string{veiculoOutraRota}}); code != http.StatusConflict {
		t.Fatalf("segunda inscrição ativa no transporte: %d %v", code, o)
	}

	// Aprovação (sem taxa) vincula direto.
	o, code = c.academiaChama(AprovarSolicitacaoServicoExtra, http.MethodPut, "/x", map[string]any{}, gin.Param{Key: "id", Value: solID})
	if code != http.StatusOK || o["status"] != "vinculada" {
		t.Fatalf("aprovar: %d %v", code, o)
	}
	c.projetar(solID)

	pend := func() []any {
		out, code := c.estudanteChama(MinhasPendenciasServicoExtra, http.MethodGet, "/x", nil, gin.Param{Key: "id", Value: solID})
		if code != http.StatusOK {
			t.Fatalf("pendências: %d %v", code, out)
		}
		l, _ := out["pendencias"].([]any)
		return l
	}
	hoje := time.Now()
	if p := pend(); len(p) != 1 || int(p[0].(map[string]any)["mes"].(float64)) != int(hoje.Month()) || p[0].(map[string]any)["valor"].(float64) != 5000 {
		t.Fatalf("pendência do mês atual: %v", p)
	}
	// Mês cobrado: o pagamento inicia (regressão: o solicitacao_id do corpo era ignorado).
	if o, code := c.estudanteChama(IniciarPagamentoServicoExtraObrigacao, http.MethodPost, "/x",
		map[string]any{"solicitacao_id": solID, "tipo_lancamento": "mensalidade", "ano": hoje.Year(), "mes": int(hoje.Month()), "metodo_pagamento": "REF"}); code != http.StatusCreated {
		t.Fatalf("pagar mês cobrado: %d %v", code, o)
	}
	// A academia tira o mês atual do prazo: a pendência some (mês não cobrado).
	var sem []int
	for _, m := range todosOsMeses() {
		if m != int(hoje.Month()) {
			sem = append(sem, m)
		}
	}
	if o, code := c.academiaChama(AtualizarRota, http.MethodPut, "/x",
		map[string]any{"nome": "Rota Norte", "horario_embarque": "06:30", "preco_modo": "valor", "preco": 5000, "prazo_meses": sem},
		gin.Param{Key: "id", Value: rotaID}); code != http.StatusOK {
		t.Fatalf("editar prazo da rota: %d %v", code, o)
	}
	if p := pend(); len(p) != 0 {
		t.Fatalf("mês removido do prazo não pode ser cobrado: %v", p)
	}
	// A cobrança que estava aberta nesse mês (não paga) foi cancelada junto.
	var cancelada int
	if err := c.client.DB().QueryRow(`SELECT count(*) FROM financeiro_cobrancas
		WHERE payload->>'codigo_inscricao_servico' = $1 AND lower(payload->>'status') LIKE 'cancel%'`, solID).Scan(&cancelada); err != nil || cancelada != 1 {
		t.Fatalf("cobrança do mês removido deveria estar cancelada: n=%d err=%v", cancelada, err)
	}
	// Pagar um mês fora do prazo é recusado.
	if o, code := c.estudanteChama(IniciarPagamentoServicoExtraObrigacao, http.MethodPost, "/x",
		map[string]any{"solicitacao_id": solID, "tipo_lancamento": "mensalidade", "ano": hoje.Year(), "mes": int(hoje.Month()), "metodo_pagamento": "REF"}); code != http.StatusConflict {
		t.Fatalf("pagar mês fora do prazo: %d %v", code, o)
	}
}

func idDaSolicitacao(t *testing.T, o map[string]any) string {
	t.Helper()
	if d, ok := o["data"].(map[string]any); ok {
		if id, _ := d["id"].(string); id != "" {
			return id
		}
	}
	if id, _ := o["id"].(string); id != "" {
		return id
	}
	t.Fatalf("a resposta não traz o id da solicitação: %v", o)
	return ""
}

func TestIntegrationServicosAtividadeColetivaExigeTurmaEGuardaMembro(t *testing.T) {
	c := novoCenarioServicos(t)

	o, code := c.academiaChama(CriarAtividade, http.MethodPost, "/academia/servicos-extras/atividades", map[string]any{
		"nome": "Coro", "instrutor": "Ana", "coletiva": true, "gratuito": true,
		"horarios": []map[string]any{{"dia_semana": 2, "hora_inicio": "14:00", "hora_fim": "15:00"}},
	})
	if code != http.StatusCreated {
		t.Fatalf("criar atividade: %d %v", code, o)
	}
	atividadeID := dado(t, o)["id"].(string)

	// Coletiva sem turma não é oferecida ao estudante.
	cat, _ := c.estudanteChama(apoio.ListarServicosExtrasCatalogoEstudante, http.MethodGet, "/x", nil)
	if l, _ := cat["atividades"].([]any); len(l) != 0 {
		t.Fatalf("coletiva sem turma não pode aparecer: %v", cat)
	}
	o, code = c.academiaChama(CriarTurmaAtividade, http.MethodPost, "/x", map[string]any{"nome": "Turma A"}, gin.Param{Key: "id", Value: atividadeID})
	if code != http.StatusCreated {
		t.Fatalf("criar turma: %d %v", code, o)
	}
	turmaID := dado(t, o)["turmas"].([]any)[0].(map[string]any)["id"].(string)
	cat, _ = c.estudanteChama(apoio.ListarServicosExtrasCatalogoEstudante, http.MethodGet, "/x", nil)
	if l, _ := cat["atividades"].([]any); len(l) != 1 {
		t.Fatalf("com turma deveria aparecer: %v", cat)
	}

	o, code = c.estudanteChama(SolicitarServicoExtra, http.MethodPost, "/x", map[string]any{}, gin.Param{Key: "id", Value: atividadeID})
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("pedir atividade: %d %v", code, o)
	}
	solID := idDaSolicitacao(t, o)
	c.projetar(solID)

	// Aprovar sem turma_id: recusado. Com turma de outra atividade: recusado.
	if o, code := c.academiaChama(AprovarSolicitacaoServicoExtra, http.MethodPut, "/x", map[string]any{}, gin.Param{Key: "id", Value: solID}); code != http.StatusBadRequest {
		t.Fatalf("aprovar coletiva sem turma: %d %v", code, o)
	}
	if o, code := c.academiaChama(AprovarSolicitacaoServicoExtra, http.MethodPut, "/x", map[string]any{"turma_id": uuid.NewString()}, gin.Param{Key: "id", Value: solID}); code != http.StatusBadRequest {
		t.Fatalf("aprovar com turma inexistente: %d %v", code, o)
	}
	o, code = c.academiaChama(AprovarSolicitacaoServicoExtra, http.MethodPut, "/x", map[string]any{"turma_id": turmaID}, gin.Param{Key: "id", Value: solID})
	if code != http.StatusOK || o["status"] != "vinculada" {
		t.Fatalf("aprovar com turma: %d %v", code, o)
	}
	c.projetar(solID)
	membros, code := c.academiaChama(ListarMembrosTurmaAtividade, http.MethodGet, "/x", nil, gin.Param{Key: "turma_id", Value: turmaID})
	if code != http.StatusOK || membros["total"].(float64) != 1 {
		t.Fatalf("membros após aprovar: %d %v", code, membros)
	}
	// Turma com membro não se apaga.
	if o, code := c.academiaChama(ExcluirTurmaAtividade, http.MethodDelete, "/x", nil, gin.Param{Key: "turma_id", Value: turmaID}); code != http.StatusConflict {
		t.Fatalf("apagar turma com membro: %d %v", code, o)
	}
	// Cancelar a inscrição tira o estudante da turma.
	if o, code := c.academiaChama(CancelarSolicitacaoServicoExtraAcademia, http.MethodPut, "/x", map[string]any{"motivo": "teste"}, gin.Param{Key: "id", Value: solID}); code != http.StatusOK {
		t.Fatalf("cancelar: %d %v", code, o)
	}
	membros, _ = c.academiaChama(ListarMembrosTurmaAtividade, http.MethodGet, "/x", nil, gin.Param{Key: "turma_id", Value: turmaID})
	if membros["total"].(float64) != 0 {
		t.Fatalf("membros após cancelar: %v", membros)
	}
}

func TestIntegrationServicosAtividadeNaoSaiDaAcademiaNemFicaSemRegra(t *testing.T) {
	c := novoCenarioServicos(t)
	// Paga sem meses, preço com 3 casas e horários cruzados: recusados.
	casos := map[string]map[string]any{
		"sem meses":       {"nome": "A", "instrutor": "X", "gratuito": false, "preco": 100, "metodos_pagamento": []string{"REF"}},
		"3 casas":         {"nome": "B", "instrutor": "X", "gratuito": false, "preco": 100.123, "prazo_meses": []int{1}, "metodos_pagamento": []string{"REF"}},
		"horário cruzado": {"nome": "C", "instrutor": "X", "gratuito": true, "horarios": []map[string]any{{"dia_semana": 1, "hora_inicio": "08:00", "hora_fim": "09:30"}, {"dia_semana": 1, "hora_inicio": "09:00", "hora_fim": "10:00"}}},
		"sem instrutor":   {"nome": "D", "gratuito": true},
	}
	for nome, corpo := range casos {
		if o, code := c.academiaChama(CriarAtividade, http.MethodPost, "/x", corpo); code != http.StatusBadRequest {
			t.Errorf("%s: esperava 400, veio %d %v", nome, code, o)
		}
	}
	// Outra academia não vê nem altera a atividade.
	o, code := c.academiaChama(CriarAtividade, http.MethodPost, "/x", map[string]any{"nome": "Judo", "instrutor": "Rui", "gratuito": true})
	if code != http.StatusCreated {
		t.Fatalf("criar: %d %v", code, o)
	}
	id := dado(t, o)["id"].(string)
	outra := novoCenarioServicos(t)
	if o, code := outra.academiaChama(GetAtividade, http.MethodGet, "/x", nil, gin.Param{Key: "id", Value: id}); code != http.StatusNotFound {
		t.Fatalf("atividade de outra academia: %d %v", code, o)
	}
	if o, code := outra.academiaChama(DesativarAtividade, http.MethodPut, "/x", nil, gin.Param{Key: "id", Value: id}); code != http.StatusNotFound {
		t.Fatalf("desativar atividade de outra academia: %d %v", code, o)
	}
}

// semearCatalogoParaPagador cria, direto nas tabelas do catálogo fixo (Parte 7),
// um Transporte pago (preço geral, 3 meses, taxa de inscrição) com métodos que
// o provedor pode ou não suportar, uma rota com veículo e uma atividade gratuita.
func semearCatalogoParaPagador(t *testing.T, client *db.Client, academia string) {
	t.Helper()
	transporte := uuid.New()
	rota := uuid.New()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := client.DB().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO servico_transporte (id, codigo_academia, preco_modo, preco_valor, prazo_modo, prazo_meses,
		valor_taxa_inscricao, metodos_pagamento, metodos_pagamento_taxa_inscricao)
		VALUES ($1,$2,'valor',5000,'meses','{1,2,3}',1000,'{"REF","XYZ"}','{"XYZ"}')`, transporte, academia)
	exec(`INSERT INTO servico_transporte_rotas (id, transporte_id, nome, horario_embarque) VALUES ($1,$2,'Rota 1','06:30')`, rota, transporte)
	exec(`INSERT INTO servico_transporte_veiculos (id, rota_id, nome) VALUES ($1,$2,'Autocarro 1')`, uuid.New(), rota)
	exec(`INSERT INTO servico_atividades (id, codigo_academia, nome, instrutor, gratuito) VALUES ($1,$2,'Biblioteca','Ana',true)`, uuid.New(), academia)
}

func atividadePorNome(t *testing.T, corpo map[string]any, nome string) map[string]any {
	t.Helper()
	itens, _ := corpo["atividades"].([]any)
	for _, it := range itens {
		if m, ok := it.(map[string]any); ok && m["nome"] == nome {
			return m
		}
	}
	t.Fatalf("atividade %q não veio na resposta: %v", nome, corpo)
	return nil
}

func TestIntegrationServicosFiltramMetodosParaOPagadorEMantemAVisaoDaAcademia(t *testing.T) {
	client, configurar := provedoresSetup(t)
	academia := "SVX" + uuid.NewString()[:6]
	idAcademia := testsupport.SeedAcademiaParaCategoriaServico(t, client, academia)
	idEstudante := testsupport.SeedEstudanteComAnoEscolar(t, client, academia, "7_ano_fundamental")
	semearCatalogoParaPagador(t, client, academia)

	catalogo := func() map[string]any {
		ctx, rec := newJSONContext(http.MethodGet, "/estudante/servicos-extras/catalogo", nil)
		ctx.Set("dbClient", client)
		ctx.Set("user_id", idEstudante)
		ctx.Set("user_type", "estudante")
		apoio.ListarServicosExtrasCatalogoEstudante(ctx)
		var corpo map[string]any
		decodeJSON(t, rec, &corpo)
		if rec.Code != http.StatusOK {
			t.Fatalf("catálogo: %d %s", rec.Code, rec.Body.String())
		}
		return corpo
	}
	publica := func() map[string]any {
		ctx, rec := newJSONContext(http.MethodGet, "/academia/servico/"+academia+"/servicos-extras", nil)
		ctx.Set("dbClient", client)
		ctx.Params = gin.Params{{Key: "codigo_academia", Value: academia}}
		apoio.ListarServicosExtrasPublico(ctx)
		var corpo map[string]any
		decodeJSON(t, rec, &corpo)
		return corpo
	}
	visaoDaAcademia := func() map[string]any {
		ctx, rec := newJSONContext(http.MethodGet, "/academia/servicos-extras", nil)
		ctx.Set("dbClient", client)
		ctx.Set("user_id", idAcademia)
		ctx.Set("user_type", "academia")
		apoio.ListarServicosExtrasAcademia(ctx)
		var corpo map[string]any
		decodeJSON(t, rec, &corpo)
		if rec.Code != http.StatusOK {
			t.Fatalf("visão da academia: %d %s", rec.Code, rec.Body.String())
		}
		return corpo
	}
	transporteDe := func(corpo map[string]any) map[string]any {
		t.Helper()
		m, ok := corpo["transporte"].(map[string]any)
		if !ok {
			t.Fatalf("o transporte não veio: %v", corpo)
		}
		return m
	}

	for nome, fonte := range map[string]func() map[string]any{"catálogo do estudante": catalogo, "lista pública": publica} {
		// Sem provedor ativo: ninguém consegue pagar; o aviso diz porquê.
		corpo := fonte()
		sv := transporteDe(corpo)
		if len(listaDe(sv, "metodos_pagamento")) != 0 || avisoDe(t, sv, "metodos_pagamento_aviso") != finance.AvisoProvedorNaoConfigurado {
			t.Fatalf("%s, sem provedor: %v", nome, sv)
		}
		if len(listaDe(sv, "metodos_pagamento_taxa_inscricao")) != 0 || avisoDe(t, sv, "metodos_pagamento_taxa_inscricao_aviso") != finance.AvisoProvedorNaoConfigurado {
			t.Fatalf("%s, taxa sem provedor: %v", nome, sv)
		}
		// Atividade gratuita: nada a pagar, nada a explicar.
		gratis := atividadePorNome(t, corpo, "Biblioteca")
		if _, ok := gratis["metodos_pagamento_aviso"]; ok {
			t.Fatalf("%s: atividade gratuita não tem aviso: %v", nome, gratis)
		}
	}

	configurar(academia)

	for nome, fonte := range map[string]func() map[string]any{"catálogo do estudante": catalogo, "lista pública": publica} {
		sv := transporteDe(fonte())
		if m := listaDe(sv, "metodos_pagamento"); len(m) != 1 || m[0] != "REF" {
			t.Fatalf("%s: com a AppyPay só REF deveria ficar: %v", nome, sv)
		}
		if _, ok := sv["metodos_pagamento_aviso"]; ok {
			t.Fatalf("%s: com método disponível não há aviso: %v", nome, sv)
		}
		// A taxa só tinha método não suportado: lista vazia + motivo correto.
		if len(listaDe(sv, "metodos_pagamento_taxa_inscricao")) != 0 || avisoDe(t, sv, "metodos_pagamento_taxa_inscricao_aviso") != finance.AvisoMetodosNaoSuportados {
			t.Fatalf("%s: taxa: %v", nome, sv)
		}
	}

	// A VISÃO DA ACADEMIA não é filtrada: mostra tudo o que ela configurou, para
	// poder corrigir (o frontend sinaliza o que o provedor não suporta).
	sv := transporteDe(visaoDaAcademia())
	if m := listaDe(sv, "metodos_pagamento"); len(m) != 2 {
		t.Fatalf("a academia deve ver os métodos configurados: %v", sv)
	}
	if _, ok := sv["metodos_pagamento_aviso"]; ok {
		t.Fatalf("a visão da academia não leva aviso do pagador: %v", sv)
	}
}

// recusadoPeloProvedor confirma que a recusa veio da validação contra o
// provedor ativo (e não de uma validação de formato, que também recusaria
// "XYZ", só que sem citar o provedor).
func recusadoPeloProvedor(t *testing.T, out map[string]any, code int, contexto string) {
	t.Helper()
	msg, _ := out["message"].(string)
	if code != http.StatusBadRequest || !strings.Contains(msg, "não é suportado pelo provedor AppyPay") {
		t.Fatalf("%s: esperava a recusa do provedor (400), obteve %d %v", contexto, code, out)
	}
}

func TestIntegrationServicosValidamMetodosNoProvedor(t *testing.T) {
	client, configurar := provedoresSetup(t)
	repository := db.NewAggregateRepository(client)
	academia := "SVV" + uuid.NewString()[:6]
	idAcademia := testsupport.SeedAcademiaParaCategoriaServico(t, client, academia)
	configurar(academia)
	comoAcademia := func(c *gin.Context) {
		c.Set("dbClient", client)
		c.Set("repository", repository)
		c.Set("user_id", idAcademia)
		c.Set("user_type", "academia")
	}
	criar := func(corpo map[string]any) (map[string]any, int) {
		c, r := newJSONContext(http.MethodPost, "/academia/servicos-extras/atividades", corpo)
		comoAcademia(c)
		CriarAtividade(c)
		var out map[string]any
		decodeJSON(t, r, &out)
		return out, r.Code
	}
	base := func(extra map[string]any) map[string]any {
		m := map[string]any{"nome": "Dança", "instrutor": "Ana", "coletiva": false, "gratuito": false, "preco": 5000, "prazo_meses": []int{1, 2}}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	// Método não suportado pelo provedor: recusado ao salvar (antes só na cobrança).
	o1, c1 := criar(base(map[string]any{"metodos_pagamento": []string{"XYZ"}}))
	recusadoPeloProvedor(t, o1, c1, "criar com método não suportado")
	o2, c2 := criar(base(map[string]any{"metodos_pagamento": []string{"REF"}, "valor_taxa_inscricao": 1000, "metodos_pagamento_taxa_inscricao": []string{"XYZ"}}))
	recusadoPeloProvedor(t, o2, c2, "criar com taxa de método não suportado")
	out, code := criar(base(map[string]any{"metodos_pagamento": []string{"REF"}}))
	if code != http.StatusCreated {
		t.Fatalf("criar com método suportado: %d %v", code, out)
	}
	id := out["data"].(map[string]any)["id"].(string)

	atualizar := func(corpo map[string]any) (map[string]any, int) {
		c, r := newJSONContext(http.MethodPut, "/academia/servicos-extras/atividades/"+id, corpo)
		comoAcademia(c)
		c.Params = gin.Params{{Key: "id", Value: id}}
		AtualizarAtividade(c)
		var o map[string]any
		decodeJSON(t, r, &o)
		return o, r.Code
	}
	o3, c3 := atualizar(map[string]any{"metodos_pagamento": []string{"XYZ"}})
	recusadoPeloProvedor(t, o3, c3, "atualizar para método não suportado")
	// Editar outro campo não é bloqueado pela validação de métodos.
	if o, code := atualizar(map[string]any{"instrutor": "Beatriz"}); code != http.StatusOK {
		t.Fatalf("atualizar o instrutor: %d %v", code, o)
	}
	if o, code := atualizar(map[string]any{"metodos_pagamento": []string{"REF", "GPO"}}); code != http.StatusOK {
		t.Fatalf("atualizar para métodos suportados: %d %v", code, o)
	}
}

// Mês removido do prazo NÃO cancela a cobrança que já tem pagamento no provedor:
// pagamento pendente (ou pago) nunca é tocado.
func TestIntegrationServicosMesRemovidoNaoToCaCobrancaComPagamento(t *testing.T) {
	c := novoCenarioServicos(t)
	hoje := time.Now()
	if _, code := c.academiaChama(AtualizarTransporte, http.MethodPut, "/x", map[string]any{"metodos_pagamento": []string{"REF"}}); code != http.StatusOK {
		t.Fatalf("configurar: %d", code)
	}
	o, code := c.academiaChama(CriarRota, http.MethodPost, "/x", map[string]any{"nome": "R", "horario_embarque": "06:30", "preco_modo": "valor", "preco": 5000, "prazo_meses": todosOsMeses()})
	if code != http.StatusCreated {
		t.Fatalf("rota: %d %v", code, o)
	}
	rotaID := o["rota_id"].(string)
	o, _ = c.academiaChama(CriarVeiculo, http.MethodPost, "/x", map[string]any{"nome": "V"}, gin.Param{Key: "id", Value: rotaID})
	veiculoID := o["veiculo_id"].(string)
	cat, _ := c.estudanteChama(apoio.ListarServicosExtrasCatalogoEstudante, http.MethodGet, "/x", nil)
	transporteID := cat["transporte"].(map[string]any)["id"].(string)

	o, code = c.estudanteChama(SolicitarServicoExtra, http.MethodPost, "/x", map[string]any{"rota_id": rotaID, "veiculo_ids": []string{veiculoID}}, gin.Param{Key: "id", Value: transporteID})
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("pedir: %d %v", code, o)
	}
	solID := idDaSolicitacao(t, o)
	if _, code := c.academiaChama(AprovarSolicitacaoServicoExtra, http.MethodPut, "/x", map[string]any{}, gin.Param{Key: "id", Value: solID}); code != http.StatusOK {
		t.Fatalf("aprovar: %d", code)
	}
	c.projetar(solID)
	if o, code := c.estudanteChama(IniciarPagamentoServicoExtraObrigacao, http.MethodPost, "/x",
		map[string]any{"solicitacao_id": solID, "tipo_lancamento": "mensalidade", "ano": hoje.Year(), "mes": int(hoje.Month()), "metodo_pagamento": "REF"}); code != http.StatusCreated {
		t.Fatalf("pagar: %d %v", code, o)
	}
	// O provedor passa a dizer que o pagamento foi feito (ainda sem a reconciliação aplicar).
	apoio.FinanceiroService.SetHTTPClient(&http.Client{Transport: appyPayMock{pago: true}})

	var sem []int
	for _, m := range todosOsMeses() {
		if m != int(hoje.Month()) {
			sem = append(sem, m)
		}
	}
	if o, code := c.academiaChama(AtualizarRota, http.MethodPut, "/x",
		map[string]any{"nome": "R", "horario_embarque": "06:30", "preco_modo": "valor", "preco": 5000, "prazo_meses": sem}, gin.Param{Key: "id", Value: rotaID}); code != http.StatusOK {
		t.Fatalf("editar prazo: %d %v", code, o)
	}
	var canceladas int
	if err := c.client.DB().QueryRow(`SELECT count(*) FROM financeiro_cobrancas
		WHERE payload->>'codigo_inscricao_servico' = $1 AND lower(payload->>'status') LIKE 'cancel%'`, solID).Scan(&canceladas); err != nil || canceladas != 0 {
		t.Fatalf("cobrança com pagamento no provedor não pode ser cancelada: n=%d err=%v", canceladas, err)
	}
}
