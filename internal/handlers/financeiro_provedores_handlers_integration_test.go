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

// As rotas legadas /financeiro/appypay/credenciais* continuam aceitando o
// formato antigo e devolvendo os campos antigos (client_id_mask etc.) além dos
// novos, para o frontend anterior continuar funcionando durante o deploy.
func TestIntegrationHandlersCredenciaisLegadasAppyPayMantemFormatoAntigo(t *testing.T) {
	client, setActor := provedoresHandlersSetup(t)
	academia := "PRL" + uuid.NewString()[:6]
	seedAcademiaParaRemocaoHandlers(t, client, academia)

	ctx, rec := newDeleteJSONContext(http.MethodPost, "/financeiro/appypay/credenciais", map[string]any{
		"contexto_tipo": finance.ContextoAcademia, "codigo_academia": academia,
		"client_id": "client-legado", "client_secret": "secret-legado", "gpo_payment_method": "GPO_LEGADO", "ref_payment_method": "REF_LEGADO",
	})
	setActor(ctx, academia)
	ConfigurarCredencialAppyPay(ctx)
	var criada map[string]any
	decodeJSON(t, rec, &criada)
	if rec.Code != http.StatusCreated || criada["client_id_mask"] != "****gado" || criada["gpo_payment_method_mask"] != "****GADO" || criada["ref_payment_method_mask"] != "****GADO" || criada["webhook_secret"] == nil {
		t.Fatalf("formato legado: %d %s", rec.Code, rec.Body.String())
	}
	if criada["provedor"] != finance.ProvedorAppyPay {
		t.Fatalf("a rota legada tem de gravar o provedor APPYPAY: %s", rec.Body.String())
	}

	ctx, rec = newDeleteJSONContext(http.MethodGet, "/financeiro/appypay/credenciais?contexto_tipo=academia&codigo_academia="+academia, nil)
	setActor(ctx, academia)
	ListarCredenciaisAppyPay(ctx)
	var lista []map[string]any
	decodeJSON(t, rec, &lista)
	if rec.Code != http.StatusOK || len(lista) != 1 || lista[0]["client_id_mask"] != "****gado" {
		t.Fatalf("listagem legada: %d %s", rec.Code, rec.Body.String())
	}

	// A rota legada de remoção continua funcionando (sem pendentes).
	ctx, rec = newDeleteJSONContext(http.MethodDelete, "/financeiro/appypay/credenciais", map[string]any{"contexto_tipo": finance.ContextoAcademia, "codigo_academia": academia})
	setActor(ctx, academia)
	RemoverCredencialAppyPay(ctx)
	// c.Status(204) só chega ao recorder no flush; o status do writer é a fonte.
	if ctx.Writer.Status() != http.StatusNoContent {
		t.Fatalf("remoção legada: %d %s", ctx.Writer.Status(), rec.Body.String())
	}
}

// Remover credenciais de um provedor com cobranças pendentes devolve 409.
func TestIntegrationHandlersRemoverCredencialComCobrancaPendenteDevolve409(t *testing.T) {
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

	remover := func() (int, string) {
		ctx, rec := newDeleteJSONContext(http.MethodDelete, "/financeiro/credenciais", map[string]any{"contexto_tipo": finance.ContextoAcademia, "codigo_academia": academia, "provedor": finance.ProvedorAppyPay})
		setActor(ctx, academia)
		RemoverCredencial(ctx)
		return ctx.Writer.Status(), rec.Body.String()
	}
	if code, body := remover(); code != http.StatusConflict {
		t.Fatalf("remover com pendente: %d %s", code, body)
	}
	// Cancelando a cobrança pendente (consulta ao vivo permitida: provedor ainda configurado)…
	ctx, rec = newDeleteJSONContext(http.MethodPost, "/financeiro/cobrancas/x/cancelar", map[string]any{"contexto_tipo": finance.ContextoAcademia, "codigo_academia": academia})
	setActor(ctx, academia)
	ctx.Params = gin.Params{{Key: "id", Value: cobranca["merchant_transaction_id"].(string)}}
	// O mock do handler responde Success a consultas (cenário de pagamento já
	// feito), então o cancelamento é recusado e a cobrança fica resolvida
	// como paga: o que importa aqui é que, resolvida a pendência, a remoção
	// passa a ser permitida.
	CancelarCobranca(ctx)
	ctx, rec = newDeleteJSONContext(http.MethodGet, "/financeiro/cobrancas/x", nil)
	setActor(ctx, academia)
	ctx.Params = gin.Params{{Key: "id", Value: cobranca["merchant_transaction_id"].(string)}}
	ctx.Request.URL.RawQuery = "contexto_tipo=academia&codigo_academia=" + academia
	ConsultarCobranca(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("consultar cobrança: %d %s", rec.Code, rec.Body.String())
	}
	if code, body := remover(); code != http.StatusNoContent {
		t.Fatalf("remover sem pendentes: %d %s", code, body)
	}
}
