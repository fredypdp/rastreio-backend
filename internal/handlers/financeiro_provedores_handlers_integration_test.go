package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/finance"
)

func provedoresHandlersSetup(t *testing.T) (*db.Client, func(ctx *gin.Context, academia string)) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("ENV", "test")
	t.Setenv("APPYPAY_RESOURCE", "integration-resource")
	client := integrationFinanceClient(t)
	previous := FinanceiroService
	FinanceiroService = finance.NewService(client)
	FinanceiroService.SetHTTPClient(&http.Client{Transport: handlerAppyPayMockTransport{}})
	t.Cleanup(func() { FinanceiroService = previous })
	userID := uuid.New()
	return client, func(ctx *gin.Context, academia string) {
		ctx.Set("dbClient", client)
		ctx.Set("user_id", userID)
		ctx.Set("user_type", "academia")
		ctx.Set("codigo_academia", academia)
	}
}

func credenciaisAppyPayNeutras(academia string) map[string]any {
	return map[string]any{
		"contexto_tipo": finance.ContextoAcademia, "codigo_academia": academia, "provedor": finance.ProvedorAppyPay,
		"credenciais": map[string]string{"client_id": "client-handler", "client_secret": "secret-handler", "gpo_method": "GPO_HANDLER", "ref_method": "REF_HANDLER"},
	}
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("resposta não é JSON: %v: %s", err, rec.Body.String())
	}
}

func TestIntegrationHandlersProvedorCatalogoEFluxoDaAcademia(t *testing.T) {
	client, setActor := provedoresHandlersSetup(t)
	academia := "PRH" + uuid.NewString()[:6]
	outra := "PRO" + uuid.NewString()[:6]
	seedAcademiaParaRemocaoHandlers(t, client, academia)

	// Catálogo.
	ctx, rec := newDeleteJSONContext(http.MethodGet, "/financeiro/provedores", nil)
	setActor(ctx, academia)
	ListarProvedores(ctx)
	var catalogo []finance.InfoProvedor
	decodeJSON(t, rec, &catalogo)
	if rec.Code != http.StatusOK || len(catalogo) == 0 || catalogo[0].ID != finance.ProvedorAppyPay || len(catalogo[0].Campos) != 4 {
		t.Fatalf("catálogo: %d %s", rec.Code, rec.Body.String())
	}

	// Definir o provedor sem credenciais é recusado (422/400 de validação), não 404.
	ctx, rec = newDeleteJSONContext(http.MethodPut, "/financeiro/provedor", map[string]any{"contexto_tipo": finance.ContextoAcademia, "codigo_academia": academia, "provedor": finance.ProvedorAppyPay})
	setActor(ctx, academia)
	DefinirProvedorContexto(ctx)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("definir provedor sem credenciais: %d %s", rec.Code, rec.Body.String())
	}

	// Credenciais neutras: resposta traz provedor, máscaras e o segredo de webhook (uma vez).
	ctx, rec = newDeleteJSONContext(http.MethodPost, "/financeiro/credenciais", credenciaisAppyPayNeutras(academia))
	setActor(ctx, academia)
	ConfigurarCredencial(ctx)
	var criada map[string]any
	decodeJSON(t, rec, &criada)
	mascaradas, _ := criada["credenciais_mascaradas"].(map[string]any)
	if rec.Code != http.StatusCreated || criada["provedor"] != finance.ProvedorAppyPay || criada["ativo"] != true || mascaradas["client_id"] != "****dler" || criada["webhook_secret"] == "" || criada["webhook_secret"] == nil {
		t.Fatalf("credencial neutra: %d %s", rec.Code, rec.Body.String())
	}
	if _, vazou := mascaradas["client_secret"]; vazou {
		t.Fatal("o client_secret não pode aparecer, nem mascarado")
	}

	// Estado do contexto.
	ctx, rec = newDeleteJSONContext(http.MethodGet, "/financeiro/provedor?contexto_tipo=academia&codigo_academia="+academia, nil)
	setActor(ctx, academia)
	ConsultarProvedorContexto(ctx)
	var estado finance.ProvedorContextoView
	decodeJSON(t, rec, &estado)
	if rec.Code != http.StatusOK || estado.ProvedorAtivo == nil || *estado.ProvedorAtivo != finance.ProvedorAppyPay || !estado.Provedores[0].CredenciaisConfiguradas {
		t.Fatalf("estado do contexto: %d %s", rec.Code, rec.Body.String())
	}

	// Outra academia não consulta nem define o provedor desta.
	ctx, rec = newDeleteJSONContext(http.MethodGet, "/financeiro/provedor?contexto_tipo=academia&codigo_academia="+academia, nil)
	setActor(ctx, outra)
	ConsultarProvedorContexto(ctx)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("academia alheia consultou o provedor: %d", rec.Code)
	}
	ctx, rec = newDeleteJSONContext(http.MethodPut, "/financeiro/provedor", map[string]any{"contexto_tipo": finance.ContextoAcademia, "codigo_academia": academia, "provedor": finance.ProvedorAppyPay})
	setActor(ctx, outra)
	DefinirProvedorContexto(ctx)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("academia alheia definiu o provedor: %d", rec.Code)
	}

	// Provedor inexistente.
	ctx, rec = newDeleteJSONContext(http.MethodPut, "/financeiro/provedor", map[string]any{"contexto_tipo": finance.ContextoAcademia, "codigo_academia": academia, "provedor": "NAOEXISTE"})
	setActor(ctx, academia)
	DefinirProvedorContexto(ctx)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("provedor inexistente: %d %s", rec.Code, rec.Body.String())
	}

	// Admin sem permissão FPP não escreve no contexto spuri.
	ctx, rec = newDeleteJSONContext(http.MethodPut, "/financeiro/provedor", map[string]any{"contexto_tipo": finance.ContextoSpuri, "provedor": finance.ProvedorAppyPay})
	ctx.Set("dbClient", client)
	ctx.Set("user_id", uuid.New())
	ctx.Set("user_type", "admin")
	DefinirProvedorContexto(ctx)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin sem permissão fpp definiu o provedor do spuri: %d", rec.Code)
	}
}

// Remover credenciais de um provedor com cobranças pendentes é PERMITIDO: a
// resposta (200) avisa quantas são e que as credenciais ficaram arquivadas
// (snapshot); sem pendentes responde 200 sem arquivar.
func TestIntegrationHandlersRemoverCredencialComCobrancaPendenteArquivaEAvisa(t *testing.T) {
	client, setActor := provedoresHandlersSetup(t)
	academia := "PRP" + uuid.NewString()[:6]
	seedAcademiaParaRemocaoHandlers(t, client, academia)

	ctx, rec := newDeleteJSONContext(http.MethodPost, "/financeiro/credenciais", credenciaisAppyPayNeutras(academia))
	setActor(ctx, academia)
	ConfigurarCredencial(ctx)
	if rec.Code != http.StatusCreated {
		t.Fatalf("configurar: %d %s", rec.Code, rec.Body.String())
	}
	ctx, rec = newDeleteJSONContext(http.MethodPost, "/financeiro/cobrancas", map[string]any{"contexto_tipo": finance.ContextoAcademia, "codigo_academia": academia, "amount": 50, "description": "Pendente", "paymentMethod": "REF"})
	setActor(ctx, academia)
	CriarCobranca(ctx)
	var cobranca map[string]any
	decodeJSON(t, rec, &cobranca)
	if rec.Code != http.StatusCreated || cobranca["provedor"] != finance.ProvedorAppyPay {
		t.Fatalf("criar cobrança: %d %s", rec.Code, rec.Body.String())
	}

	remover := func() (int, map[string]any) {
		ctx, rec := newDeleteJSONContext(http.MethodDelete, "/financeiro/credenciais", map[string]any{"contexto_tipo": finance.ContextoAcademia, "codigo_academia": academia, "provedor": finance.ProvedorAppyPay})
		setActor(ctx, academia)
		RemoverCredencial(ctx)
		var corpo map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &corpo)
		return ctx.Writer.Status(), corpo
	}
	code, corpo := remover()
	if code != http.StatusOK || corpo["provedor"] != finance.ProvedorAppyPay || corpo["cobrancas_pendentes"] != float64(1) || corpo["credenciais_arquivadas"] != true {
		t.Fatalf("remover com pendente: %d %v", code, corpo)
	}

	// O estado do contexto mostra o snapshot e a pendente.
	ctx, rec = newDeleteJSONContext(http.MethodGet, "/financeiro/provedor?contexto_tipo=academia&codigo_academia="+academia, nil)
	setActor(ctx, academia)
	ConsultarProvedorContexto(ctx)
	var estado finance.ProvedorContextoView
	decodeJSON(t, rec, &estado)
	if rec.Code != http.StatusOK || estado.ProvedorAtivo != nil || estado.Provedores[0].CredenciaisConfiguradas || !estado.Provedores[0].CredenciaisArquivadas || estado.Provedores[0].CobrancasPendentes != 1 {
		t.Fatalf("estado após a remoção com pendente: %d %s", rec.Code, rec.Body.String())
	}

	// A cobrança antiga continua consultável pelo snapshot (o mock responde Success).
	ctx, rec = newDeleteJSONContext(http.MethodGet, "/financeiro/cobrancas/x", nil)
	setActor(ctx, academia)
	ctx.Params = gin.Params{{Key: "id", Value: cobranca["merchant_transaction_id"].(string)}}
	ctx.Request.URL.RawQuery = "contexto_tipo=academia&codigo_academia=" + academia
	ConsultarCobranca(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("consultar a cobrança antiga com as credenciais arquivadas: %d %s", rec.Code, rec.Body.String())
	}
	// Resolvida a pendência, o snapshot some.
	ctx, rec = newDeleteJSONContext(http.MethodGet, "/financeiro/provedor?contexto_tipo=academia&codigo_academia="+academia, nil)
	setActor(ctx, academia)
	ConsultarProvedorContexto(ctx)
	decodeJSON(t, rec, &estado)
	if estado.Provedores[0].CredenciaisArquivadas || estado.Provedores[0].CobrancasPendentes != 0 {
		t.Fatalf("snapshot deveria ter sido descartado com a pendência resolvida: %s", rec.Body.String())
	}
	// Segunda remoção: não há mais credenciais.
	if code, _ = remover(); code != http.StatusNotFound {
		t.Fatalf("segunda remoção: %d", code)
	}
}

// limparProvedorDaAcademia apaga provedor ativo, credenciais e segredos de uma
// academia de seed fixo, para o teste ser repetível no mesmo banco.
func limparProvedorDaAcademia(t *testing.T, client *db.Client, academia string) {
	t.Helper()
	for _, q := range []string{
		`DELETE FROM financeiro_segredos_provedor WHERE credential_id IN (SELECT id FROM financeiro_credenciais_provedor WHERE codigo_academia=$1)`,
		`DELETE FROM financeiro_credenciais_provedor WHERE codigo_academia=$1`,
		`DELETE FROM financeiro_credenciais_arquivadas WHERE codigo_academia=$1`,
		`DELETE FROM financeiro_provedores_pagamento WHERE codigo_academia=$1`,
	} {
		if _, err := client.DB().Exec(q, academia); err != nil {
			t.Fatal(err)
		}
	}
}

// O status público da solicitação de matrícula só oferece ao pagador os métodos
// habilitados pela academia que o provedor ativo dela suporta.
func TestIntegrationHandlersStatusMatriculaFiltraMetodosPeloProvedorAtivo(t *testing.T) {
	client, setActor := provedoresHandlersSetup(t)
	codigo, _, _ := seedSolicitacaoMatriculaParaBusca(t, client)
	const academia = "ACA-BUSCA"
	// A academia do seed é fixa (ACA-BUSCA): sem limpar, uma segunda execução no
	// mesmo banco já começaria com provedor ativo. Limpa antes e depois.
	limparProvedorDaAcademia(t, client, academia)
	t.Cleanup(func() { limparProvedorDaAcademia(t, client, academia) })
	if _, err := client.DB().Exec(`UPDATE projection_solicitacoes_matricula SET metodos_pagamento_matricula=ARRAY['REF','XYZ'] WHERE codigo_solicitacao=$1`, codigo); err != nil {
		t.Fatal(err)
	}
	status := func() []any {
		ctx, rec := newDeleteJSONContext(http.MethodGet, "/solicitacoes-matricula/"+codigo+"/status", nil)
		ctx.Set("dbClient", client)
		ctx.Params = gin.Params{{Key: "codigo", Value: codigo}}
		ConsultarStatusSolicitacaoMatricula(ctx)
		var corpo map[string]any
		decodeJSON(t, rec, &corpo)
		metodos, _ := corpo["metodos_pagamento"].([]any)
		if rec.Code != http.StatusOK || metodos == nil {
			t.Fatalf("status da solicitação: %d %s", rec.Code, rec.Body.String())
		}
		return metodos
	}
	// Sem provedor ativo ninguém consegue pagar: lista vazia.
	if m := status(); len(m) != 0 {
		t.Fatalf("sem provedor ativo: %v", m)
	}
	ctx, rec := newDeleteJSONContext(http.MethodPost, "/financeiro/credenciais", credenciaisAppyPayNeutras(academia))
	setActor(ctx, academia)
	ConfigurarCredencial(ctx)
	if rec.Code != http.StatusCreated {
		t.Fatalf("configurar: %d %s", rec.Code, rec.Body.String())
	}
	// Com a AppyPay: REF (suportado) fica, XYZ (desconhecido) sai.
	if m := status(); len(m) != 1 || m[0] != "REF" {
		t.Fatalf("com a AppyPay: %v", m)
	}
}
