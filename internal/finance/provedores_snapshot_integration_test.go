package finance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/projections"
)

// clienteCapturaTransport é um transporte de teste da AppyPay que emite um token
// DIFERENTE por credencial ("tok-<client_id>") e registra o token usado em cada
// chamada de API. Assim os testes provam COM QUAIS credenciais o Service falou
// com o provedor em cada operação, mesmo com o cache de tokens.
type clienteCapturaTransport struct {
	inner    *appyPayMockTransport
	mu       sync.Mutex
	chamadas []string // cabeçalho Authorization de cada chamada de API (não-token)
}

func (c *clienteCapturaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "/oauth2/token") && req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		form, _ := url.ParseQuery(string(raw))
		corpo := `{"access_token":"tok-` + form.Get("client_id") + `","expires_in":3600,"token_type":"Bearer"}`
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(corpo)), Request: req}, nil
	}
	c.mu.Lock()
	c.chamadas = append(c.chamadas, req.Header.Get("Authorization"))
	c.mu.Unlock()
	return c.inner.RoundTrip(req)
}

// ultimoToken devolve o token da última chamada de API.
func (c *clienteCapturaTransport) ultimoToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.chamadas) == 0 {
		return ""
	}
	return c.chamadas[len(c.chamadas)-1]
}

func configurarAppyPayComClientID(t *testing.T, s *Service, academia, clientID string) (uuid.UUID, string) {
	t.Helper()
	view, secret, err := s.ConfigureCredential(context.Background(), nil, CredentialInput{ContextoTipo: ContextoAcademia, CodigoAcademia: academia, Provedor: ProvedorAppyPay, Credenciais: map[string]string{"client_id": clientID, "client_secret": "secret-" + clientID, "gpo_method": "GPO_INTEGRATION", "ref_method": "REF_INTEGRATION"}}, "teste", "sistema", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return view.ID, secret
}

func contar(t *testing.T, client *db.Client, query string, args ...any) int {
	t.Helper()
	var n int
	if err := client.DB().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Remover credenciais com cobranças pendentes é permitido: as credenciais
// ficam arquivadas (snapshot) e continuam servindo SÓ para concluir essas
// cobranças; quando a última é resolvida, o snapshot é descartado.
func TestIntegrationSnapshotCredenciaisRemovidasComPendentesResolveRetroativamente(t *testing.T) {
	provedorEnv(t)
	client := integrationClient(t)
	captura := &clienteCapturaTransport{inner: &appyPayMockTransport{status: "Pending"}}
	service := NewService(client)
	service.SetHTTPClient(&http.Client{Transport: captura})
	ctx := context.Background()
	academia := "SNP" + uuid.NewString()[:8]
	velho := "client-velho-" + uuid.NewString()[:6]
	novo := "client-novo-" + uuid.NewString()[:6]

	velhoID, velhoSegredo := configurarAppyPayComClientID(t, service, academia, velho)
	a, err := novaCobrancaREF(t, service, academia, "SA")
	if err != nil {
		t.Fatal(err)
	}
	// A cobrança grava a credencial com que foi emitida (vínculo com o snapshot),
	// no banco e no evento do ledger.
	var emitente uuid.UUID
	_ = client.DB().QueryRow(`SELECT credencial_id FROM financeiro_cobrancas WHERE id=$1`, a.ID).Scan(&emitente)
	if emitente != velhoID {
		t.Fatalf("credencial emitente = %s, queria %s", emitente, velhoID)
	}
	if n := contar(t, client, `SELECT COUNT(*) FROM spuri_ledger WHERE aggregate_id=$1 AND payload->>'credencial_id'=$2`, a.ID, velhoID.String()); n == 0 {
		t.Fatal("o evento da cobrança não registrou a credencial emitente")
	}

	// 1. Remoção PERMITIDA, com aviso (pendentes) e snapshot.
	res, err := service.RemoveCredential(ctx, ContextoAcademia, academia, ProvedorAppyPay, "u", "academia", "ip")
	if err != nil || !res.CredenciaisArquivadas || res.CobrancasPendentes != 1 || res.Provedor != ProvedorAppyPay {
		t.Fatalf("remoção com pendente: %+v, %v", res, err)
	}
	if views, _ := service.ListCredentials(ctx, ContextoAcademia, academia); len(views) != 0 {
		t.Fatalf("credencial arquivada não pode aparecer na lista: %+v", views)
	}
	if n := contar(t, client, `SELECT COUNT(*) FROM financeiro_credenciais_arquivadas WHERE credential_id=$1`, velhoID); n != 1 {
		t.Fatalf("snapshot ausente: %d", n)
	}
	if n := contar(t, client, `SELECT COUNT(*) FROM financeiro_segredos_provedor WHERE credential_id=$1`, velhoID); n == 0 {
		t.Fatal("os segredos do snapshot têm de continuar no cofre")
	}
	if n := contar(t, client, `SELECT COUNT(*) FROM spuri_ledger WHERE aggregate_id=$1 AND event_type='CredenciaisProvedorRemovidas' AND payload->>'arquivada'='true'`, velhoID); n != 1 {
		t.Fatalf("evento de remoção sem a marca de arquivada: %d", n)
	}
	// Sem credenciais ativas: não há provedor ativo, o snapshot NÃO serve para
	// cobranças novas nem para definir provedor.
	if _, err = service.ProvedorAtivo(ctx, ContextoAcademia, academia); !errors.Is(err, ErrNotFound) {
		t.Fatalf("provedor ativo após remover as credenciais: %v", err)
	}
	if _, err = novaCobrancaREF(t, service, academia, "SB"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cobrança nova com credenciais só arquivadas deveria falhar: %v", err)
	}
	if _, err = service.DefinirProvedor(ctx, ContextoAcademia, academia, ProvedorAppyPay, "u", "academia", "ip"); !errors.Is(err, ErrProvedorNaoConfigurado) {
		t.Fatalf("definir provedor só com snapshot: %v", err)
	}
	estado, err := service.ConsultarProvedorContexto(ctx, ContextoAcademia, academia)
	if err != nil || estado.Provedores[0].CredenciaisConfiguradas || !estado.Provedores[0].CredenciaisArquivadas || estado.Provedores[0].CobrancasPendentes != 1 {
		t.Fatalf("estado do contexto: %+v, %v", estado.Provedores, err)
	}

	// 2. Credenciais NOVAS (outra conta) no mesmo provedor: viram as ativas, e a
	// cobrança antiga continua com as VELHAS (as que a emitiram).
	novoID, novoSegredo := configurarAppyPayComClientID(t, service, academia, novo)
	if novoID == velhoID {
		t.Fatal("credenciais novas não podem reaproveitar o id do snapshot")
	}
	b, err := novaCobrancaREF(t, service, academia, "SC")
	if err != nil {
		t.Fatal(err)
	}
	_ = client.DB().QueryRow(`SELECT credencial_id FROM financeiro_cobrancas WHERE id=$1`, b.ID).Scan(&emitente)
	if emitente != novoID {
		t.Fatalf("a cobrança nova deveria ter sido emitida pelas credenciais novas")
	}
	if _, err = service.ConsultCharge(ctx, ContextoAcademia, academia, a.ID.String(), "u", "academia", "ip"); err != nil {
		t.Fatalf("consulta da cobrança antiga com o snapshot: %v", err)
	}
	// A cobrança antiga falou com a AppyPay usando as credenciais VELHAS (as que a emitiram)…
	if got := captura.ultimoToken(); got != "Bearer tok-"+velho {
		t.Fatalf("a consulta da cobrança antiga usou %q, queria o token das credenciais arquivadas", got)
	}
	if _, err = service.ConsultCharge(ctx, ContextoAcademia, academia, b.ID.String(), "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	// …e a nova, as NOVAS.
	if got := captura.ultimoToken(); got != "Bearer tok-"+novo {
		t.Fatalf("a consulta da cobrança nova usou %q, queria o token das credenciais novas", got)
	}

	// 3. Webhooks: o segredo do snapshot ainda autentica a cobrança antiga; o
	// novo autentica o contexto novo.
	dono, err := service.AuthenticateWebhook(ctx, ProvedorAppyPay, http.Header{WebhookHeaderName: []string{velhoSegredo}})
	if err != nil || dono.CredentialID != velhoID {
		t.Fatalf("webhook do snapshot: %+v %v", dono, err)
	}
	if donoNovo, err := service.AuthenticateWebhook(ctx, ProvedorAppyPay, http.Header{WebhookHeaderName: []string{novoSegredo}}); err != nil || donoNovo.CredentialID != novoID {
		t.Fatalf("webhook das credenciais novas: %+v %v", donoNovo, err)
	}

	// 4. Resolver a única cobrança que dependia do snapshot descarta o snapshot.
	captura.inner.status = "Success"
	aceito, confirmado, err := service.AcceptWebhook(ctx, ProvedorAppyPay, "REF", a.ProviderChargeID, dono, map[string]any{"id": a.ProviderChargeID, "responseStatus": map[string]any{"status": "Success", "code": float64(100)}})
	if err != nil || !aceito || !confirmado {
		t.Fatalf("webhook da cobrança antiga: aceito=%t confirmado=%t err=%v", aceito, confirmado, err)
	}
	if n := contar(t, client, `SELECT COUNT(*) FROM financeiro_credenciais_arquivadas WHERE credential_id=$1`, velhoID); n != 0 {
		t.Fatalf("snapshot não descartado: %d", n)
	}
	if n := contar(t, client, `SELECT COUNT(*) FROM financeiro_segredos_provedor WHERE credential_id=$1`, velhoID); n != 0 {
		t.Fatalf("segredos do snapshot não limpos do cofre: %d", n)
	}
	if n := contar(t, client, `SELECT COUNT(*) FROM spuri_ledger WHERE aggregate_id=$1 AND event_type='CredenciaisArquivadasPurgadas'`, velhoID); n != 1 {
		t.Fatalf("evento de descarte no ledger: %d", n)
	}
	if _, err = service.AuthenticateWebhook(ctx, ProvedorAppyPay, http.Header{WebhookHeaderName: []string{velhoSegredo}}); err == nil {
		t.Fatal("segredo de um snapshot descartado continuou autenticando")
	}
	// As credenciais ativas nunca são tocadas pelo descarte.
	if _, err = service.AuthenticateWebhook(ctx, ProvedorAppyPay, http.Header{WebhookHeaderName: []string{novoSegredo}}); err != nil {
		t.Fatalf("o descarte afetou as credenciais ativas: %v", err)
	}
}

// Sem cobranças pendentes não há motivo para guardar nada: tudo é apagado.
func TestIntegrationRemoverCredenciaisSemPendentesNaoArquivaNada(t *testing.T) {
	provedorEnv(t)
	client := integrationClient(t)
	service := NewService(client)
	service.SetHTTPClient(&http.Client{Transport: &appyPayMockTransport{status: "Pending"}})
	academia := "SNS" + uuid.NewString()[:8]
	id, _ := configurarAppyPayComClientID(t, service, academia, "client-"+uuid.NewString()[:6])
	res, err := service.RemoveCredential(context.Background(), ContextoAcademia, academia, ProvedorAppyPay, "u", "academia", "ip")
	if err != nil || res.CredenciaisArquivadas || res.CobrancasPendentes != 0 {
		t.Fatalf("remoção sem pendentes: %+v, %v", res, err)
	}
	if contar(t, client, `SELECT COUNT(*) FROM financeiro_credenciais_arquivadas WHERE credential_id=$1`, id) != 0 || contar(t, client, `SELECT COUNT(*) FROM financeiro_segredos_provedor WHERE credential_id=$1`, id) != 0 {
		t.Fatal("sem pendentes nada deveria ficar arquivado nem no cofre")
	}
}

// Cobranças anteriores à Tarefa 120 não têm credencial emitente: a resolução cai
// para o snapshot mais recente do contexto + provedor.
func TestIntegrationSnapshotServeCobrancaLegadaSemCredencialEmitente(t *testing.T) {
	provedorEnv(t)
	client := integrationClient(t)
	captura := &clienteCapturaTransport{inner: &appyPayMockTransport{status: "Pending"}}
	service := NewService(client)
	service.SetHTTPClient(&http.Client{Transport: captura})
	ctx := context.Background()
	academia := "SNL" + uuid.NewString()[:8]
	cliente := "client-legado-" + uuid.NewString()[:6]
	configurarAppyPayComClientID(t, service, academia, cliente)
	a, err := novaCobrancaREF(t, service, academia, "SL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.DB().Exec(`UPDATE financeiro_cobrancas SET credencial_id=NULL WHERE id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if res, err := service.RemoveCredential(ctx, ContextoAcademia, academia, ProvedorAppyPay, "u", "academia", "ip"); err != nil || !res.CredenciaisArquivadas {
		t.Fatalf("remoção: %+v %v", res, err)
	}
	if _, err = service.ConsultCharge(ctx, ContextoAcademia, academia, a.ID.String(), "u", "academia", "ip"); err != nil {
		t.Fatalf("a cobrança legada deveria resolver pelo snapshot mais recente: %v", err)
	}
	if got := captura.ultimoToken(); got != "Bearer tok-"+cliente {
		t.Fatalf("a cobrança legada deveria falar com o provedor usando o snapshot, usou %q", got)
	}
	// A cobrança legada pendente segura o snapshot: um descarte agora não o apaga.
	if err = service.purgarArquivadasSemPendentes(ctx, ContextoAcademia, academia, ProvedorAppyPay); err != nil {
		t.Fatal(err)
	}
	if n := contar(t, client, `SELECT COUNT(*) FROM financeiro_credenciais_arquivadas WHERE contexto_tipo='academia' AND codigo_academia=$1`, academia); n != 1 {
		t.Fatalf("o snapshot foi descartado com uma cobrança legada ainda pendente: %d", n)
	}
}

// O pagador só vê os métodos que o provedor ativo da academia suporta.
func TestIntegrationMetodosDoPagadorSoOsDoProvedorAtivo(t *testing.T) {
	provedorEnv(t)
	client := integrationClient(t)
	registrarProvedorFalso(t)
	service := NewService(client)
	ctx := context.Background()
	academia := "MET" + uuid.NewString()[:8]

	ordenado := func(v []string) string {
		c := append([]string(nil), v...)
		sort.Strings(c)
		return strings.Join(c, ",")
	}
	// Sem provedor: ninguém consegue pagar.
	if got := service.FiltrarMetodosPorProvedor(ctx, academia, []string{"GPO", "REF"}); len(got) != 0 {
		t.Fatalf("sem provedor ativo: %v", got)
	}
	configurarAppyPay(t, service, academia)
	if got := service.FiltrarMetodosPorProvedor(ctx, academia, []string{"GPO", "REF", "GPO_QR", "XYZ"}); strings.Join(got, ",") != "GPO,REF,GPO_QR" {
		t.Fatalf("AppyPay (ordem preservada, método desconhecido removido): %v", got)
	}
	configurarFalso(t, service, academia, "tok")
	if _, err := service.DefinirProvedor(ctx, ContextoAcademia, academia, provedorFalsoID, "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	if got := service.FiltrarMetodosPorProvedor(ctx, academia, []string{"GPO", "REF", "GPO_QR"}); strings.Join(got, ",") != "REF" {
		t.Fatalf("provedor que só suporta REF: %v", got)
	}

	// MetodosPagamentoMensalidade (o que o estudante recebe) aplica o mesmo filtro.
	seedMensalidadeAcademia(t, client, academia, "private", "fundamental", "2025_2026")
	seedMensalidadeConfiguracao(t, client, academia, NivelFundamental, "6_ano_fundamental", nil, 1000, 7, time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC))
	if _, err := client.DB().Exec(`UPDATE financeiro_mensalidade_configuracoes SET metodos_pagamento=ARRAY['GPO','REF','GPO_QR'] WHERE codigo_academia=$1`, academia); err != nil {
		t.Fatal(err)
	}
	got, err := service.MetodosPagamentoMensalidade(ctx, academia)
	if err != nil || ordenado(got) != "REF" {
		t.Fatalf("métodos da propina para o estudante com o provedor REF-only: %v, %v", got, err)
	}
	if _, err = service.DefinirProvedor(ctx, ContextoAcademia, academia, ProvedorAppyPay, "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	if got, _ = service.MetodosPagamentoMensalidade(ctx, academia); ordenado(got) != "GPO,GPO_QR,REF" {
		t.Fatalf("métodos da propina com a AppyPay: %v", got)
	}
}

// Replay do ledger: a remoção "arquivada" cria o snapshot, o descarte o apaga e
// uma remoção antiga (sem a marca) continua só apagando a credencial.
func TestIntegrationProjecaoReplaySnapshotDeCredenciais(t *testing.T) {
	provedorEnv(t)
	client := integrationClient(t)
	proj := projections.NewFinanceiroProjection(client)
	agora := time.Now().UTC()
	academia := "RPL" + uuid.NewString()[:8]
	credArquivada, credLegada, cobranca, cobrancaSemCred := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	aplicar := func(agregado uuid.UUID, tipo string, payload map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(payload)
		if err := proj.Handle(db.Event{AggregateID: agregado, AggregateType: "Financeiro", EventType: tipo, Payload: raw, OccurredAt: agora}); err != nil {
			t.Fatalf("%s: %v", tipo, err)
		}
	}
	base := func(id uuid.UUID, ambiente string) map[string]any {
		return map[string]any{"credential_id": id.String(), "contexto_tipo": ContextoAcademia, "codigo_academia": academia, "ambiente": ambiente, "provedor": ProvedorAppyPay, "credenciais_mascaradas": map[string]any{"client_id": "****1234"}}
	}
	// Duas credenciais (ambientes diferentes para respeitar o índice único).
	aplicar(credArquivada, "CredenciaisProvedorConfiguradas", base(credArquivada, "test"))
	aplicar(credLegada, "CredenciaisProvedorConfiguradas", base(credLegada, "prod"))

	rem := base(credArquivada, "test")
	rem["arquivada"], rem["cobrancas_pendentes"], rem["removida_por"] = true, float64(2), "u"
	aplicar(credArquivada, "CredenciaisProvedorRemovidas", rem)
	aplicar(credLegada, "CredenciaisProvedorRemovidas", base(credLegada, "prod")) // evento antigo: sem "arquivada"

	if n := contar(t, client, `SELECT COUNT(*) FROM financeiro_credenciais_provedor WHERE codigo_academia=$1`, academia); n != 0 {
		t.Fatalf("as duas credenciais deviam ter saído da tabela ativa: %d", n)
	}
	var ambiente, payload string
	var pendentes int
	if err := client.DB().QueryRow(`SELECT ambiente,cobrancas_pendentes,payload::text FROM financeiro_credenciais_arquivadas WHERE credential_id=$1`, credArquivada).Scan(&ambiente, &pendentes, &payload); err != nil {
		t.Fatalf("snapshot da remoção arquivada: %v", err)
	}
	if ambiente != "test" || pendentes != 2 || !strings.Contains(payload, "****1234") {
		t.Fatalf("snapshot = %s %d %s", ambiente, pendentes, payload)
	}
	if n := contar(t, client, `SELECT COUNT(*) FROM financeiro_credenciais_arquivadas WHERE credential_id=$1`, credLegada); n != 0 {
		t.Fatal("remoção sem a marca não pode criar snapshot")
	}

	// Cobrança com e sem credencial emitente.
	evento := func(id uuid.UUID, cred any) map[string]any {
		p := map[string]any{"merchant_transaction_id": integrationMerchant("RP"), "contexto_tipo": ContextoAcademia, "codigo_academia": academia, "provedor": ProvedorAppyPay, "status": "Pending"}
		if cred != nil {
			p["credencial_id"] = cred
		}
		return p
	}
	aplicar(cobranca, "CobrancaCriada", evento(cobranca, credArquivada.String()))
	aplicar(cobrancaSemCred, "CobrancaCriada", evento(cobrancaSemCred, nil))
	var comCred, semCred uuid.NullUUID
	_ = client.DB().QueryRow(`SELECT credencial_id FROM financeiro_cobrancas WHERE id=$1`, cobranca).Scan(&comCred)
	_ = client.DB().QueryRow(`SELECT credencial_id FROM financeiro_cobrancas WHERE id=$1`, cobrancaSemCred).Scan(&semCred)
	if !comCred.Valid || comCred.UUID != credArquivada || semCred.Valid {
		t.Fatalf("credencial emitente projetada: com=%+v sem=%+v", comCred, semCred)
	}
	// Um evento seguinte (consulta) não apaga nem troca a credencial emitente.
	seguinte := evento(cobranca, nil)
	seguinte["merchant_transaction_id"] = nil
	var merchant string
	_ = client.DB().QueryRow(`SELECT merchant_transaction_id FROM financeiro_cobrancas WHERE id=$1`, cobranca).Scan(&merchant)
	seguinte["merchant_transaction_id"] = merchant
	aplicar(cobranca, "CobrancaConsultada", seguinte)
	_ = client.DB().QueryRow(`SELECT credencial_id FROM financeiro_cobrancas WHERE id=$1`, cobranca).Scan(&comCred)
	if !comCred.Valid || comCred.UUID != credArquivada {
		t.Fatal("o evento seguinte apagou a credencial emitente")
	}

	aplicar(credArquivada, "CredenciaisArquivadasPurgadas", map[string]any{"credential_id": credArquivada.String()})
	if n := contar(t, client, `SELECT COUNT(*) FROM financeiro_credenciais_arquivadas WHERE credential_id=$1`, credArquivada); n != 0 {
		t.Fatal("o evento de descarte não apagou o snapshot")
	}
}
