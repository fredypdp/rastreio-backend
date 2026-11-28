package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/domain/aggregates"
	"spuri/internal/finance"
	"spuri/internal/handlers/apoio"
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
		return apoio.SolicitacaoServicoExtraParaPagador(context.Background(), disps, s, apoio.SolicitacaoServicoExtraToJSON(s))
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
