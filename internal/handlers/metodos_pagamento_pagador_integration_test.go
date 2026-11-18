package handlers

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/finance"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Tarefa 121: o pagador só vê os métodos que o provedor ativo da academia
// suporta, e uma lista vazia vem com o motivo.

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

func semearServicoPago(t *testing.T, client *db.Client, academia, nome string, metodos, metodosTaxa string, comTaxa bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := client.DB().Exec(`INSERT INTO projection_servicos_extras
		(id, codigo_academia, nome, descricao, pago, preco, tipo_cobranca, metodos_pagamento, tem_taxa_inscricao, valor_taxa_inscricao, metodos_pagamento_taxa_inscricao,
		 anos_academicos_disponiveis, cursos_disponiveis, documento_obrigatorio, documento_instrucoes, detalhes_personalizados, ativo, criado_por, created_at, updated_at, version, last_event_id)
		VALUES ($1,$2,$3,'',true,5000,'unico',$4,$5,$6,$7,'{}','{}',false,'','{}',true,$8,now(),now(),1,$9)`,
		id, academia, nome, metodos, comTaxa, func() any {
			if comTaxa {
				return 1000
			}
			return nil
		}(), metodosTaxa, uuid.New(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func servicoPorNome(t *testing.T, corpo map[string]any, nome string) map[string]any {
	t.Helper()
	itens, _ := corpo["servicos_extras"].([]any)
	for _, it := range itens {
		if m, ok := it.(map[string]any); ok && m["nome"] == nome {
			return m
		}
	}
	t.Fatalf("serviço %q não veio na resposta: %v", nome, corpo)
	return nil
}

func TestIntegrationHandlersServicosExtrasFiltramMetodosParaOPagadorEMantemAVisaoDaAcademia(t *testing.T) {
	client, setActor := provedoresHandlersSetup(t)
	academia := "SVX" + uuid.NewString()[:6]
	idAcademia := seedAcademiaParaCategoriaServico(t, client, academia)
	idEstudante := seedEstudanteComAnoEscolar(t, client, academia, "7_ano_fundamental")
	semearServicoPago(t, client, academia, "Transporte", `{"REF","XYZ"}`, `{"XYZ"}`, true)
	semearServicoPago(t, client, academia, "Só XYZ", `{"XYZ"}`, `{}`, false)
	if _, err := client.DB().Exec(`INSERT INTO projection_servicos_extras
		(id, codigo_academia, nome, descricao, pago, metodos_pagamento, tem_taxa_inscricao, metodos_pagamento_taxa_inscricao,
		 anos_academicos_disponiveis, cursos_disponiveis, documento_obrigatorio, documento_instrucoes, detalhes_personalizados, ativo, criado_por, created_at, updated_at, version, last_event_id)
		VALUES ($1,$2,'Biblioteca','',false,'{}',false,'{}','{}','{}',false,'','{}',true,$3,now(),now(),1,$4)`, uuid.New(), academia, uuid.New(), uuid.New()); err != nil {
		t.Fatal(err)
	}

	catalogo := func() map[string]any {
		ctx, rec := newDeleteJSONContext(http.MethodGet, "/estudante/servicos-extras/catalogo", nil)
		ctx.Set("dbClient", client)
		ctx.Set("user_id", idEstudante)
		ctx.Set("user_type", "estudante")
		ListarServicosExtrasCatalogoEstudante(ctx)
		var corpo map[string]any
		decodeJSON(t, rec, &corpo)
		if rec.Code != http.StatusOK {
			t.Fatalf("catálogo: %d %s", rec.Code, rec.Body.String())
		}
		return corpo
	}
	publica := func() map[string]any {
		ctx, rec := newDeleteJSONContext(http.MethodGet, "/academia/servico/"+academia+"/servicos-extras", nil)
		ctx.Set("dbClient", client)
		ctx.Params = gin.Params{{Key: "codigo_academia", Value: academia}}
		ListarServicosExtrasPublico(ctx)
		var corpo map[string]any
		decodeJSON(t, rec, &corpo)
		return corpo
	}
	visaoDaAcademia := func() map[string]any {
		ctx, rec := newDeleteJSONContext(http.MethodGet, "/academia/servicos-extras", nil)
		ctx.Set("dbClient", client)
		ctx.Set("user_id", idAcademia)
		ctx.Set("user_type", "academia")
		ListarServicosExtrasAcademia(ctx)
		var corpo map[string]any
		decodeJSON(t, rec, &corpo)
		if rec.Code != http.StatusOK {
			t.Fatalf("visão da academia: %d %s", rec.Code, rec.Body.String())
		}
		return corpo
	}

	for nome, fonte := range map[string]func() map[string]any{"catálogo do estudante": catalogo, "lista pública": publica} {
		// Sem provedor ativo: ninguém consegue pagar; o aviso diz porquê.
		corpo := fonte()
		sv := servicoPorNome(t, corpo, "Transporte")
		if len(listaDe(sv, "metodos_pagamento")) != 0 || avisoDe(t, sv, "metodos_pagamento_aviso") != finance.AvisoProvedorNaoConfigurado {
			t.Fatalf("%s, sem provedor: %v", nome, sv)
		}
		if len(listaDe(sv, "metodos_pagamento_taxa_inscricao")) != 0 || avisoDe(t, sv, "metodos_pagamento_taxa_inscricao_aviso") != finance.AvisoProvedorNaoConfigurado {
			t.Fatalf("%s, taxa sem provedor: %v", nome, sv)
		}
		// Serviço gratuito: nada a pagar, nada a explicar.
		gratis := servicoPorNome(t, corpo, "Biblioteca")
		if _, ok := gratis["metodos_pagamento_aviso"]; ok {
			t.Fatalf("%s: serviço gratuito não tem aviso: %v", nome, gratis)
		}
	}

	ctx, rec := newDeleteJSONContext(http.MethodPost, "/financeiro/credenciais", credenciaisAppyPayNeutras(academia))
	setActor(ctx, academia)
	ConfigurarCredencial(ctx)
	if rec.Code != http.StatusCreated {
		t.Fatalf("configurar: %d %s", rec.Code, rec.Body.String())
	}

	for nome, fonte := range map[string]func() map[string]any{"catálogo do estudante": catalogo, "lista pública": publica} {
		corpo := fonte()
		sv := servicoPorNome(t, corpo, "Transporte")
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
		so := servicoPorNome(t, corpo, "Só XYZ")
		if len(listaDe(so, "metodos_pagamento")) != 0 || avisoDe(t, so, "metodos_pagamento_aviso") != finance.AvisoMetodosNaoSuportados {
			t.Fatalf("%s: serviço só com método não suportado: %v", nome, so)
		}
	}

	// A VISÃO DA ACADEMIA não é filtrada: mostra tudo o que ela configurou, para
	// poder corrigir (o frontend sinaliza o que o provedor não suporta).
	sv := servicoPorNome(t, visaoDaAcademia(), "Transporte")
	if m := listaDe(sv, "metodos_pagamento"); len(m) != 2 {
		t.Fatalf("a academia deve ver os métodos configurados: %v", sv)
	}
	if _, ok := sv["metodos_pagamento_aviso"]; ok {
		t.Fatalf("a visão da academia não leva aviso do pagador: %v", sv)
	}
}

// recusadoPeloProvedor confirma que a recusa veio da validação contra o
// provedor ativo (e não da lista de métodos que o próprio agregado aceita, que
// também recusaria "XYZ", só que sem citar o provedor).
func recusadoPeloProvedor(t *testing.T, out map[string]any, code int, contexto string) {
	t.Helper()
	msg, _ := out["message"].(string)
	if code != http.StatusBadRequest || !strings.Contains(msg, "não é suportado pelo provedor AppyPay") {
		t.Fatalf("%s: esperava a recusa do provedor (400), obteve %d %v", contexto, code, out)
	}
}

func TestIntegrationHandlersCriarEAtualizarServicoExtraValidamMetodosNoProvedor(t *testing.T) {
	client, setActor := provedoresHandlersSetup(t)
	repository := db.NewAggregateRepository(client)
	academia := "SVV" + uuid.NewString()[:6]
	idAcademia := seedAcademiaParaCategoriaServico(t, client, academia)
	ctx, rec := newDeleteJSONContext(http.MethodPost, "/financeiro/credenciais", credenciaisAppyPayNeutras(academia))
	setActor(ctx, academia)
	ConfigurarCredencial(ctx)
	if rec.Code != http.StatusCreated {
		t.Fatalf("configurar: %d %s", rec.Code, rec.Body.String())
	}
	comoAcademia := func(c *gin.Context) {
		c.Set("dbClient", client)
		c.Set("repository", repository)
		c.Set("user_id", idAcademia)
		c.Set("user_type", "academia")
	}
	criar := func(corpo map[string]any) (*gin.Context, map[string]any, int) {
		c, r := newDeleteJSONContext(http.MethodPost, "/academia/servicos-extras", corpo)
		comoAcademia(c)
		CriarServicoExtra(c)
		var out map[string]any
		decodeJSON(t, r, &out)
		return c, out, r.Code
	}

	// Método não suportado pelo provedor: recusado ao salvar (antes só na cobrança).
	_, o1, c1 := criar(map[string]any{"nome": "Transporte", "pago": true, "preco": 5000, "tipo_cobranca": "unico", "metodos_pagamento": []string{"XYZ"}})
	recusadoPeloProvedor(t, o1, c1, "criar com método não suportado")
	_, o2, c2 := criar(map[string]any{"nome": "Passeio", "pago": true, "preco": 5000, "tipo_cobranca": "unico", "metodos_pagamento": []string{"REF"}, "tem_taxa_inscricao": true, "valor_taxa_inscricao": 1000, "metodos_pagamento_taxa_inscricao": []string{"XYZ"}})
	recusadoPeloProvedor(t, o2, c2, "criar com taxa de método não suportado")
	_, out, code := criar(map[string]any{"nome": "Transporte", "pago": true, "preco": 5000, "tipo_cobranca": "unico", "metodos_pagamento": []string{"REF"}})
	if code != http.StatusCreated {
		t.Fatalf("criar com método suportado: %d %v", code, out)
	}
	id := out["data"].(map[string]any)["id"].(string)

	atualizar := func(corpo map[string]any) (map[string]any, int) {
		c, r := newDeleteJSONContext(http.MethodPut, "/academia/servicos-extras/"+id, corpo)
		comoAcademia(c)
		c.Params = gin.Params{{Key: "id", Value: id}}
		AtualizarServicoExtra(c)
		var o map[string]any
		decodeJSON(t, r, &o)
		return o, r.Code
	}
	o3, c3 := atualizar(map[string]any{"metodos_pagamento": []string{"XYZ"}})
	recusadoPeloProvedor(t, o3, c3, "atualizar para método não suportado")
	// Editar outro campo não é bloqueado pela validação de métodos.
	if o, code := atualizar(map[string]any{"descricao": "Novo texto"}); code != http.StatusOK {
		t.Fatalf("atualizar a descrição: %d %v", code, o)
	}
	if o, code := atualizar(map[string]any{"metodos_pagamento": []string{"REF", "GPO"}}); code != http.StatusOK {
		t.Fatalf("atualizar para métodos suportados: %d %v", code, o)
	}
}

func TestIntegrationHandlersMensalidadesDoEstudanteTrazOCampoDeAvisos(t *testing.T) {
	client, _ := provedoresHandlersSetup(t)
	academia := "MEN" + uuid.NewString()[:6]
	codigo := "E" + uuid.NewString()[:6]
	estudanteID := seedEstudanteParaCobrancas(t, client, codigo, academia)
	ctx, rec := newDeleteJSONContext(http.MethodGet, "/financeiro/mensalidades/estudante/"+codigo, nil)
	ctx.Params = gin.Params{{Key: "codigo", Value: codigo}}
	ctx.Set("dbClient", client)
	ctx.Set("user_id", estudanteID)
	ctx.Set("user_type", "estudante")
	ConsultarMensalidadesEstudante(ctx)
	var corpo map[string]any
	decodeJSON(t, rec, &corpo)
	if rec.Code != http.StatusOK {
		t.Fatalf("mensalidades: %d %s", rec.Code, rec.Body.String())
	}
	// O campo existe sempre (objeto, vazio quando não há o que explicar), para
	// o cliente poder confiar nele sem checar a presença.
	if _, ok := corpo["metodos_pagamento_avisos_por_academia"].(map[string]any); !ok {
		t.Fatalf("faltou metodos_pagamento_avisos_por_academia (objeto): %v", corpo)
	}
	if _, ok := corpo["metodos_pagamento_por_academia"].(map[string]any); !ok {
		t.Fatalf("faltou metodos_pagamento_por_academia: %v", corpo)
	}
}

func TestIntegrationHandlersInscricoesDoEstudanteFiltramOsMetodosDaTaxa(t *testing.T) {
	client, setActor := provedoresHandlersSetup(t)
	academia := "INS" + uuid.NewString()[:6]
	solic := func(valor float64, metodos ...string) *aggregates.SolicitacaoServicoExtra {
		return &aggregates.SolicitacaoServicoExtra{CodigoAcademia: academia, ValorTaxaInscricao: valor, MetodosPagamentoTaxaInscricao: metodos}
	}
	disps := map[string]finance.PagamentoDisponivel{}
	aplicar := func(s *aggregates.SolicitacaoServicoExtra) gin.H {
		return solicitacaoServicoExtraParaPagador(context.Background(), disps, s, solicitacaoServicoExtraToJSON(s))
	}

	// Sem taxa de inscrição: a serialização fica intacta e nem consulta o provedor.
	if h := aplicar(solic(0)); h["metodos_pagamento_taxa_inscricao_aviso"] != nil || len(disps) != 0 {
		t.Fatalf("sem taxa não há o que filtrar nem explicar: %v", h)
	}
	// Com taxa e sem provedor.
	h := aplicar(solic(1000, "REF", "XYZ"))
	if m, _ := h["metodos_pagamento_taxa_inscricao"].([]string); len(m) != 0 {
		t.Fatalf("sem provedor: %v", h)
	}
	if a, _ := h["metodos_pagamento_taxa_inscricao_aviso"].(*finance.AvisoMetodos); a == nil || a.Codigo != finance.AvisoProvedorNaoConfigurado {
		t.Fatalf("aviso sem provedor: %v", h)
	}

	ctx, rec := newDeleteJSONContext(http.MethodPost, "/financeiro/credenciais", credenciaisAppyPayNeutras(academia))
	setActor(ctx, academia)
	ConfigurarCredencial(ctx)
	if rec.Code != http.StatusCreated {
		t.Fatalf("configurar: %d %s", rec.Code, rec.Body.String())
	}
	// A consulta ao provedor é guardada por academia: a lista de inscrições não
	// repete a leitura para cada solicitação (o retrato antigo continua valendo
	// dentro da mesma requisição, então começa-se uma requisição nova).
	if len(disps) != 1 {
		t.Fatalf("a consulta ao provedor deveria estar em cache por academia: %d", len(disps))
	}
	disps = map[string]finance.PagamentoDisponivel{}
	h = aplicar(solic(1000, "REF", "XYZ"))
	if m, _ := h["metodos_pagamento_taxa_inscricao"].([]string); len(m) != 1 || m[0] != "REF" {
		t.Fatalf("com a AppyPay só REF deveria ficar: %v", h)
	}
	if _, ok := h["metodos_pagamento_taxa_inscricao_aviso"]; ok {
		t.Fatalf("com método disponível não há aviso: %v", h)
	}
	_ = client
}
