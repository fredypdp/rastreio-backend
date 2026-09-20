package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/finance"
)

// ConsultarMesInicioCobranca (GET, Tarefa 111) é novo — antes não existia
// nenhuma forma de descobrir, antes de tentar remover, se já existe uma
// exceção de início de cobrança definida para o ano letivo (ver
// RemoveMesInicioCobranca em internal/finance/mensalidade.go, que já fazia
// essa checagem internamente, mas só no momento da remoção). Mesmo
// contrato de escopo por academia que os endpoints de configuração
// vizinhos: a academia só consulta o PRÓPRIO início de cobrança.
func TestIntegrationConsultarMesInicioCobrancaHandlerRespeitaEscopo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	academiaDona := "INI" + uuid.NewString()[:6]
	academiaOutra := "OUT" + uuid.NewString()[:6]
	seedAcademiaParaRemocaoHandlers(t, client, academiaDona)

	previousService := FinanceiroService
	FinanceiroService = finance.NewService(client)
	t.Cleanup(func() { FinanceiroService = previousService })

	userID := uuid.New()
	newGetCtx := func(codigoAcademiaAtor, codigoAcademiaQuery, anoLetivo string) (*gin.Context, *httptest.ResponseRecorder) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		url := "/financeiro/mensalidades/inicio-cobranca?codigo_academia=" + codigoAcademiaQuery + "&ano_letivo=" + anoLetivo
		ctx.Request = httptest.NewRequest(http.MethodGet, url, nil)
		ctx.Set("dbClient", client)
		ctx.Set("user_id", userID)
		ctx.Set("user_type", "academia")
		ctx.Set("codigo_academia", codigoAcademiaAtor)
		return ctx, recorder
	}

	// 1) Sem nenhuma exceção definida ainda -> 404 (não 200 com valor
	// zerado, nem 500 — o frontend depende de distinguir isto para só
	// habilitar "Remover início de cobrança" quando há algo para remover).
	ctx, rec := newGetCtx(academiaDona, academiaDona, "2025_2026")
	ConsultarMesInicioCobranca(ctx)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("sem exceção definida: esperava 404, obteve %d: %s", rec.Code, rec.Body.String())
	}

	// 2) Define via o Service diretamente (mais rápido que passar pelo
	// handler de criação) e então consulta via o HANDLER, que é o que este
	// teste precisa validar.
	if err := FinanceiroService.DefinirMesInicioCobranca(ctx.Request.Context(), finance.MesInicioCobrancaInput{
		CodigoAcademia: academiaDona, AnoLetivo: "2025_2026", MesInicio: 11,
	}, uuid.NewString(), "academia", "127.0.0.1"); err != nil {
		t.Fatalf("DefinirMesInicioCobranca falhou: %v", err)
	}
	ctx, rec = newGetCtx(academiaDona, academiaDona, "2025_2026")
	ConsultarMesInicioCobranca(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("com exceção definida: esperava 200, obteve %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		MesInicio int `json:"mes_inicio"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.MesInicio != 11 {
		t.Fatalf("mes_inicio retornado = %d, queria 11", body.MesInicio)
	}

	// 3) Outra academia tentando consultar um codigo_academia que não é o
	// próprio -> 403 (nunca revela se a outra academia tem ou não exceção).
	ctx, rec = newGetCtx(academiaOutra, academiaDona, "2025_2026")
	ConsultarMesInicioCobranca(ctx)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outra academia consultando escopo alheio: esperava 403, obteve %d: %s", rec.Code, rec.Body.String())
	}

	// 4) ano_letivo ausente -> 400 de validação, nunca 404/500.
	ctx, rec = newGetCtx(academiaDona, academiaDona, "")
	ConsultarMesInicioCobranca(ctx)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ano_letivo ausente: esperava 400, obteve %d: %s", rec.Code, rec.Body.String())
	}
}
