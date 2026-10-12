package finance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/projections"
)

// provedorFalso é um segundo PSP, só de teste, que prova que o núcleo é
// realmente independente de provedor: suporta apenas REF, autentica webhooks
// por um token no cabeçalho (sem segredo gerado pelo Spuri) e responde
// "success" em texto puro, como um PSP real baseado em assinatura faria.
const provedorFalsoID = "FAKEPSP"

type provedorFalso struct {
	mu             sync.Mutex
	statusConsulta string
	criacoes       []string // IDs de comerciante enviados ao provedor
	consultas      []string // "<idProvedor>|<token usado>"
}

func (p *provedorFalso) ID() string   { return provedorFalsoID }
func (p *provedorFalso) Nome() string { return "Fake PSP" }
func (p *provedorFalso) Slug() string { return "fakepsp" }
func (p *provedorFalso) Metodos() []string {
	return []string{MetodoREF}
}
func (p *provedorFalso) Campos() []CampoCredencial {
	return []CampoCredencial{{Chave: "token", Rotulo: "Token", Obrigatorio: true, Secreto: true}}
}
func (p *provedorFalso) ValidarCredenciais(v map[string]string) (map[string]string, error) {
	if strings.TrimSpace(v["token"]) == "" {
		return nil, errors.New("token obrigatório")
	}
	return map[string]string{"token": v["token"]}, nil
}
func (p *provedorFalso) Mascarar(v map[string]string) map[string]string {
	return map[string]string{"token": mask(v["token"])}
}
func (p *provedorFalso) ValidarAmbiente() error { return nil }
func (p *provedorFalso) ResolverMetodo(_ credencialProvedor, solicitado string) (string, error) {
	switch strings.ToUpper(solicitado) {
	case "REF":
		return MetodoREF, nil
	case "GPO":
		// Aceita traduzir GPO para provar que é o NÚCLEO quem recusa um método
		// que o provedor não declarou em Metodos().
		return MetodoGPO, nil
	}
	return "", errors.New("método desconhecido")
}
func (p *provedorFalso) CriarCobranca(_ context.Context, _ credencialProvedor, pedido pedidoCobranca) (respostaProvedor, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.criacoes = append(p.criacoes, pedido.IDComerciante)
	return respostaProvedor{ID: "fake-" + pedido.IDComerciante, Outcome: providerOutcome{Status: "Pending"}, Bruta: map[string]any{"id": "fake-" + pedido.IDComerciante}}, nil
}
func (p *provedorFalso) CriarQRCode(context.Context, credencialProvedor, pedidoQRCode) (respostaProvedor, error) {
	return respostaProvedor{}, errors.New("QR não suportado")
}
func (p *provedorFalso) ConsultarCobranca(_ context.Context, cred credencialProvedor, ref referenciaCobranca) (respostaProvedor, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.consultas = append(p.consultas, ref.IDProvedor+"|"+cred.Valores["token"])
	return respostaProvedor{ID: ref.IDProvedor, Outcome: providerOutcome{Status: p.statusConsulta}, Bruta: map[string]any{"id": ref.IDProvedor}}, nil
}
func (p *provedorFalso) UsaSegredoWebhook() bool { return false }
func (p *provedorFalso) RotasWebhook() []RotaWebhook {
	return []RotaWebhook{{Caminho: "notificacao", Metodo: MetodoREF}}
}

// AutenticarWebhook é deliberadamente PERMISSIVO: aceita o token próprio no
// cabeçalho X-Fake-Token e também qualquer segredo (token ou webhook_secret)
// enviado no cabeçalho padrão da plataforma. Assim, o único jeito de uma
// notificação de OUTRO provedor ser aceita aqui é o núcleo entregar-lhe
// credenciais que não são deste provedor — exatamente o que o filtro por
// provedor em AuthenticateWebhook impede.
func (p *provedorFalso) AutenticarWebhook(h http.Header, candidatos []credencialProvedor) (int, bool) {
	for i, c := range candidatos {
		if got := h.Get("X-Fake-Token"); got != "" && got == c.Valores["token"] {
			return i, true
		}
		got := h.Get(WebhookHeaderName)
		if got != "" && (got == c.Valores["token"] || got == c.Valores["webhook_secret"]) {
			return i, true
		}
	}
	return -1, false
}
func (p *provedorFalso) InterpretarWebhook(_ string, corpo []byte) (string, map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(corpo, &payload); err != nil {
		return "", nil, err
	}
	id, _ := payload["id"].(string)
	return id, payload, nil
}
func (p *provedorFalso) ResultadoWebhook(payload map[string]any) (providerOutcome, string) {
	status, _ := payload["status"].(string)
	id, _ := payload["id"].(string)
	return providerOutcome{Status: status}, id
}
func (p *provedorFalso) RespostaWebhook(bool) (int, string, string) {
	return http.StatusOK, "text/plain", "success"
}

// registrarProvedorFalso acrescenta o provedor falso às fábricas pelo tempo do
// teste. Tem de ser chamado ANTES de NewService.
func registrarProvedorFalso(t *testing.T) *provedorFalso {
	t.Helper()
	falso := &provedorFalso{statusConsulta: "Pending"}
	anteriores := fabricasProvedores
	fabricasProvedores = append(append([]fabricaProvedor{}, anteriores...), func(*http.Client) Provedor { return falso })
	t.Cleanup(func() { fabricasProvedores = anteriores })
	return falso
}

func provedorEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ENV", "test")
	t.Setenv("APPYPAY_RESOURCE", "integration-resource")
	t.Setenv("JWT_SECRET", "test-only-secret-material-at-least-32")
}

func provedorDaCobrancaNoBanco(t *testing.T, client *db.Client, id uuid.UUID) string {
	t.Helper()
	var p string
	if err := client.DB().QueryRow(`SELECT provedor FROM financeiro_cobrancas WHERE id=$1`, id).Scan(&p); err != nil {
		t.Fatal(err)
	}
	return p
}

func novaCobrancaREF(t *testing.T, s *Service, academia, prefix string) (ChargeResult, error) {
	t.Helper()
	return s.CreateCharge(context.Background(), ChargeRequest{ContextoTipo: ContextoAcademia, CodigoAcademia: academia, Amount: 100, Description: "Teste de provedor", PaymentMethod: "REF", MerchantTransactionID: integrationMerchant(prefix)}, "teste", "sistema", "127.0.0.1")
}

func configurarAppyPay(t *testing.T, s *Service, academia string) (credentialID uuid.UUID, webhookSecret string) {
	t.Helper()
	view, secret, err := s.ConfigureCredential(context.Background(), nil, CredentialInput{ContextoTipo: ContextoAcademia, CodigoAcademia: academia, Provedor: ProvedorAppyPay, Credenciais: map[string]string{"client_id": "integration-client", "client_secret": "integration-secret", "gpo_method": "GPO_INTEGRATION", "ref_method": "REF_INTEGRATION"}}, "teste", "sistema", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return view.ID, secret
}

func configurarFalso(t *testing.T, s *Service, academia, token string) uuid.UUID {
	t.Helper()
	view, _, err := s.ConfigureCredential(context.Background(), nil, CredentialInput{ContextoTipo: ContextoAcademia, CodigoAcademia: academia, Provedor: provedorFalsoID, Credenciais: map[string]string{"token": token}}, "teste", "sistema", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return view.ID
}

func TestProvedoresCatalogoDeclaraMetodosCamposEWebhooks(t *testing.T) {
	provedores := NewService(nil).Provedores()
	if len(provedores) == 0 || provedores[0].ID != ProvedorAppyPay {
		t.Fatalf("catálogo sem AppyPay: %+v", provedores)
	}
	info := provedores[0]
	if strings.Join(info.Metodos, ",") != "REF,GPO,GPO_QR" {
		t.Fatalf("métodos da AppyPay = %v", info.Metodos)
	}
	chaves := []string{}
	for _, c := range info.Campos {
		chaves = append(chaves, c.Chave)
		if c.Chave == "client_secret" && !c.Secreto {
			t.Fatal("client_secret tem de ser marcado como secreto")
		}
	}
	if strings.Join(chaves, ",") != "client_id,client_secret,gpo_method,ref_method" {
		t.Fatalf("campos = %v", chaves)
	}
	// URLs já configuradas no painel da AppyPay: não podem mudar.
	if strings.Join(info.Webhooks, ",") != "/webhooks/appypay/gpo,/webhooks/appypay/ref" {
		t.Fatalf("webhooks = %v", info.Webhooks)
	}
	if !info.UsaSegredoWebhook || info.CabecalhoWebhook != WebhookHeaderName {
		t.Fatalf("segredo de webhook = %+v", info)
	}
	if _, err := NewService(nil).ProvedorPorID("inexistente"); !errors.Is(err, ErrProvedorDesconhecido) {
		t.Fatalf("provedor desconhecido: %v", err)
	}
}

func TestIntegrationDefinirProvedorExigeCredenciaisEUnicoAtivo(t *testing.T) {
	provedorEnv(t)
	client := integrationClient(t)
	registrarProvedorFalso(t)
	service := NewService(client)
	ctx := context.Background()
	academia := "PRV" + uuid.NewString()[:8]

	if _, err := service.ProvedorAtivo(ctx, ContextoAcademia, academia); !errors.Is(err, ErrNotFound) {
		t.Fatalf("contexto novo não deve ter provedor ativo: %v", err)
	}
	if _, err := service.DefinirProvedor(ctx, ContextoAcademia, academia, ProvedorAppyPay, "u", "academia", "ip"); !errors.Is(err, ErrProvedorNaoConfigurado) || errors.Is(err, ErrNotFound) {
		t.Fatalf("definir sem credenciais deve falhar com ErrProvedorNaoConfigurado: %v", err)
	}
	if _, err := service.DefinirProvedor(ctx, ContextoAcademia, academia, "NAOEXISTE", "u", "academia", "ip"); !errors.Is(err, ErrProvedorDesconhecido) {
		t.Fatalf("provedor desconhecido: %v", err)
	}

	// As primeiras credenciais de um contexto sem provedor tornam o provedor ativo.
	configurarAppyPay(t, service, academia)
	if ativo, err := service.ProvedorAtivo(ctx, ContextoAcademia, academia); err != nil || ativo != ProvedorAppyPay {
		t.Fatalf("ativo = %q, %v", ativo, err)
	}
	// Configurar credenciais de OUTRO provedor não troca o ativo.
	configurarFalso(t, service, academia, "tok-1")
	if ativo, _ := service.ProvedorAtivo(ctx, ContextoAcademia, academia); ativo != ProvedorAppyPay {
		t.Fatalf("configurar credenciais de outro provedor trocou o ativo para %q", ativo)
	}
	res, err := service.DefinirProvedor(ctx, ContextoAcademia, academia, provedorFalsoID, "u", "academia", "ip")
	if err != nil {
		t.Fatal(err)
	}
	if res.ProvedorAnterior != ProvedorAppyPay || res.ProvedorAtivo == nil || *res.ProvedorAtivo != provedorFalsoID {
		t.Fatalf("resultado = %+v", res)
	}
	// Um único ativo: exatamente um provedor marcado como ativo.
	ativos := 0
	for _, p := range res.Provedores {
		if p.Ativo {
			ativos++
		}
		if !p.CredenciaisConfiguradas {
			t.Fatalf("provedor %s deveria ter credenciais", p.ID)
		}
	}
	if ativos != 1 {
		t.Fatalf("ativos = %d, queria exatamente 1", ativos)
	}
	// Idempotente: definir o mesmo provedor de novo não grava outro evento.
	var antes, depois int
	_ = client.DB().QueryRow(`SELECT COUNT(*) FROM spuri_ledger WHERE event_type='ProvedorPagamentoDefinido' AND payload->>'codigo_academia'=$1`, academia).Scan(&antes)
	if _, err = service.DefinirProvedor(ctx, ContextoAcademia, academia, provedorFalsoID, "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	_ = client.DB().QueryRow(`SELECT COUNT(*) FROM spuri_ledger WHERE event_type='ProvedorPagamentoDefinido' AND payload->>'codigo_academia'=$1`, academia).Scan(&depois)
	if antes != 1 || depois != 1 {
		t.Fatalf("eventos de definição de provedor: antes=%d depois=%d, queria 1 e 1", antes, depois)
	}
}

func TestIntegrationCobrancaNovaUsaProvedorAtivoEMetodoSuportado(t *testing.T) {
	provedorEnv(t)
	client := integrationClient(t)
	falso := registrarProvedorFalso(t)
	service := NewService(client)
	service.SetHTTPClient(&http.Client{Transport: &appyPayMockTransport{status: "Pending"}})
	ctx := context.Background()
	academia := "PRV" + uuid.NewString()[:8]

	configurarAppyPay(t, service, academia)
	a, err := novaCobrancaREF(t, service, academia, "PA")
	if err != nil {
		t.Fatal(err)
	}
	if a.Provedor != ProvedorAppyPay || provedorDaCobrancaNoBanco(t, client, a.ID) != ProvedorAppyPay {
		t.Fatalf("cobrança emitida pelo provedor errado: %q", a.Provedor)
	}
	var noPayload string
	_ = client.DB().QueryRow(`SELECT payload->>'provedor' FROM financeiro_cobrancas WHERE id=$1`, a.ID).Scan(&noPayload)
	if noPayload != ProvedorAppyPay {
		t.Fatalf("payload sem o provedor: %q", noPayload)
	}

	configurarFalso(t, service, academia, "tok-1")
	if _, err = service.DefinirProvedor(ctx, ContextoAcademia, academia, provedorFalsoID, "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	b, err := novaCobrancaREF(t, service, academia, "PB")
	if err != nil {
		t.Fatal(err)
	}
	if b.Provedor != provedorFalsoID || provedorDaCobrancaNoBanco(t, client, b.ID) != provedorFalsoID {
		t.Fatalf("cobrança nova deveria usar o provedor ativo, obteve %q", b.Provedor)
	}
	if len(falso.criacoes) != 1 || falso.criacoes[0] != b.MerchantTransactionID {
		t.Fatalf("o provedor ativo não recebeu a criação: %v", falso.criacoes)
	}

	// Método que o provedor ativo não declara em Metodos(): recusado pelo núcleo.
	_, err = service.CreateCharge(ctx, ChargeRequest{ContextoTipo: ContextoAcademia, CodigoAcademia: academia, Amount: 100, Description: "GPO", PaymentMethod: "GPO", PaymentInfo: map[string]any{"phoneNumber": "900000000"}, MerchantTransactionID: integrationMerchant("PC")}, "teste", "sistema", "127.0.0.1")
	if !errors.Is(err, ErrMetodoIndisponivel) {
		t.Fatalf("GPO no provedor que só suporta REF: %v", err)
	}
	// E QR code (método GPO_QR) idem.
	_, err = service.CreateGPOQRCode(ctx, QRCodeRequest{ContextoTipo: ContextoAcademia, CodigoAcademia: academia, Amount: 100, Description: "QR", MerchantTransactionID: integrationMerchant("PD")}, "teste", "sistema", "127.0.0.1")
	if !errors.Is(err, ErrMetodoIndisponivel) {
		t.Fatalf("QR no provedor sem GPO_QR: %v", err)
	}
}

// Mecanismo retroativo: depois de o contexto trocar de provedor, a cobrança
// pendente continua sendo consultada, confirmada por webhook e protegida pelo
// provedor que a emitiu.
func TestIntegrationRetroativoCobrancaPendenteSegueProvedorOriginalAposTroca(t *testing.T) {
	provedorEnv(t)
	client := integrationClient(t)
	falso := registrarProvedorFalso(t)
	service := NewService(client)
	appyMock := &appyPayMockTransport{status: "Pending"}
	service.SetHTTPClient(&http.Client{Transport: appyMock})
	ctx := context.Background()
	academia := "PRV" + uuid.NewString()[:8]

	appyCredID, appySecret := configurarAppyPay(t, service, academia)
	a, err := novaCobrancaREF(t, service, academia, "RA")
	if err != nil {
		t.Fatal(err)
	}
	configurarFalso(t, service, academia, "tok-fake")
	res, err := service.DefinirProvedor(ctx, ContextoAcademia, academia, provedorFalsoID, "u", "academia", "ip")
	if err != nil {
		t.Fatal(err)
	}
	if res.CobrancasPendentesNoAnterior != 1 {
		t.Fatalf("cobranças pendentes no provedor anterior = %d, queria 1", res.CobrancasPendentesNoAnterior)
	}
	b, err := novaCobrancaREF(t, service, academia, "RB")
	if err != nil {
		t.Fatal(err)
	}

	// 1. As credenciais do provedor anterior estão protegidas enquanto houver pendente.
	if err = service.RemoveCredential(ctx, ContextoAcademia, academia, ProvedorAppyPay, "u", "academia", "ip"); !errors.Is(err, ErrCredenciaisEmUso) {
		t.Fatalf("remover credenciais com cobrança pendente: %v", err)
	}
	if err = service.RemoveCredential(ctx, ContextoAcademia, academia, provedorFalsoID, "u", "academia", "ip"); !errors.Is(err, ErrCredenciaisEmUso) {
		t.Fatalf("o provedor ativo também está protegido: %v", err)
	}

	// 2. Consultar a cobrança antiga fala com a AppyPay, nunca com o provedor ativo.
	consulta, err := service.ConsultCharge(ctx, ContextoAcademia, academia, a.ID.String(), "u", "academia", "ip")
	if err != nil {
		t.Fatal(err)
	}
	if consulta.Provedor != ProvedorAppyPay || len(falso.consultas) != 0 {
		t.Fatalf("consulta da cobrança antiga: provedor=%q consultas no provedor ativo=%v", consulta.Provedor, falso.consultas)
	}
	// A cobrança nova fala com o provedor ativo, com as credenciais DELE.
	if _, err = service.ConsultCharge(ctx, ContextoAcademia, academia, b.ID.String(), "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	if len(falso.consultas) != 1 || !strings.HasSuffix(falso.consultas[0], "|tok-fake") {
		t.Fatalf("consulta da cobrança nova: %v", falso.consultas)
	}

	// 3. Webhook da AppyPay para a cobrança antiga continua autenticando e
	// confirmando, mesmo com a FAKEPSP como provedor ativo.
	owner, err := service.AuthenticateWebhook(ctx, ProvedorAppyPay, http.Header{WebhookHeaderName: []string{appySecret}})
	if err != nil || owner.CredentialID != appyCredID || owner.Provedor != ProvedorAppyPay {
		t.Fatalf("webhook da AppyPay após a troca: owner=%+v err=%v", owner, err)
	}
	// O webhook de sucesso só confirma depois de uma conferência ao vivo no
	// provedor DA COBRANÇA. A FAKEPSP (ativa) responderia "Pending" e
	// impediria a confirmação se a conferência fosse feita nela.
	appyMock.status = "Success"
	falso.statusConsulta = "Pending"
	accepted, confirmed, err := service.AcceptWebhook(ctx, ProvedorAppyPay, "REF", a.ProviderChargeID, owner, map[string]any{"id": a.ProviderChargeID, "responseStatus": map[string]any{"status": "Success", "code": float64(100)}})
	if err != nil || !accepted || !confirmed {
		t.Fatalf("webhook da cobrança antiga: accepted=%t confirmed=%t err=%v", accepted, confirmed, err)
	}
	var status string
	_ = client.DB().QueryRow(`SELECT payload->>'status' FROM financeiro_cobrancas WHERE id=$1`, a.ID).Scan(&status)
	if status != "Success" {
		t.Fatalf("status da cobrança antiga = %q", status)
	}
	// O provedor da cobrança não muda com os eventos seguintes.
	if p := provedorDaCobrancaNoBanco(t, client, a.ID); p != ProvedorAppyPay {
		t.Fatalf("provedor da cobrança mudou para %q", p)
	}

	// 4. Resolvida a última pendente da AppyPay, as credenciais podem ser
	// removidas sem tocar no provedor ativo.
	if err = service.RemoveCredential(ctx, ContextoAcademia, academia, ProvedorAppyPay, "u", "academia", "ip"); err != nil {
		t.Fatalf("remover credenciais sem pendentes: %v", err)
	}
	if ativo, _ := service.ProvedorAtivo(ctx, ContextoAcademia, academia); ativo != provedorFalsoID {
		t.Fatalf("remover o provedor inativo alterou o ativo para %q", ativo)
	}
}

func TestIntegrationWebhookNaoAutenticaEntreProvedores(t *testing.T) {
	provedorEnv(t)
	client := integrationClient(t)
	registrarProvedorFalso(t)
	service := NewService(client)
	ctx := context.Background()
	academia := "PRV" + uuid.NewString()[:8]
	_, appySecret := configurarAppyPay(t, service, academia)
	configurarFalso(t, service, academia, "tok-fake-"+uuid.NewString())

	if _, err := service.AuthenticateWebhook(ctx, provedorFalsoID, http.Header{WebhookHeaderName: []string{appySecret}}); err == nil {
		t.Fatal("o segredo da AppyPay autenticou notificação do outro provedor")
	}
	if _, err := service.AuthenticateWebhook(ctx, ProvedorAppyPay, http.Header{"X-Fake-Token": []string{"tok-fake"}}); err == nil {
		t.Fatal("o token do outro provedor autenticou notificação da AppyPay")
	}
	if _, err := service.AuthenticateWebhook(ctx, "NAOEXISTE", http.Header{}); !errors.Is(err, ErrProvedorDesconhecido) {
		t.Fatalf("provedor desconhecido: %v", err)
	}
	// O provedor falso não usa segredo gerado pelo Spuri: não deve existir webhook_secret.
	var segredos int
	_ = client.DB().QueryRow(`SELECT COUNT(*) FROM financeiro_segredos_provedor s JOIN financeiro_credenciais_provedor c ON c.id=s.credential_id WHERE c.codigo_academia=$1 AND c.provedor=$2 AND s.secret_type='webhook_secret'`, academia, provedorFalsoID).Scan(&segredos)
	if segredos != 0 {
		t.Fatalf("provedor sem segredo de webhook gerou %d segredo(s)", segredos)
	}
}

func TestIntegrationConfigurarMetodosExigeProvedorQueOsSuporte(t *testing.T) {
	provedorEnv(t)
	client := integrationClient(t)
	registrarProvedorFalso(t)
	service := NewService(client)
	ctx := context.Background()
	academia := "PRV" + uuid.NewString()[:8]

	if err := service.validarMetodosDoProvedor(ctx, academia, "propina", []string{"REF"}); err == nil || !strings.Contains(err.Error(), "provedor de pagamento") {
		t.Fatalf("sem provedor configurado: %v", err)
	}
	configurarAppyPay(t, service, academia)
	if err := service.validarMetodosDoProvedor(ctx, academia, "propina", []string{"REF", "GPO", "GPO_QR"}); err != nil {
		t.Fatalf("a AppyPay suporta os três métodos: %v", err)
	}
	configurarFalso(t, service, academia, "tok")
	if _, err := service.DefinirProvedor(ctx, ContextoAcademia, academia, provedorFalsoID, "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	if err := service.validarMetodosDoProvedor(ctx, academia, "propina", []string{"REF", "GPO"}); !errors.Is(err, ErrMetodoIndisponivel) {
		t.Fatalf("GPO não é suportado pelo provedor ativo: %v", err)
	}
	if err := service.validarMetodosDoProvedor(ctx, academia, "propina", []string{"REF"}); err != nil {
		t.Fatalf("REF é suportado: %v", err)
	}
}

// Eventos gravados antes da Tarefa 119 (nomes com prefixo AppyPay e sem o
// campo "provedor") continuam sendo reprocessados pela projeção.
func TestIntegrationProjecaoAceitaEventosLegadosAppyPay(t *testing.T) {
	provedorEnv(t)
	client := integrationClient(t)
	service := NewService(client)
	proj := projections.NewFinanceiroProjection(client)
	ctx := context.Background()
	academia := "LEG" + uuid.NewString()[:8]
	credID, chargeID := uuid.New(), uuid.New()
	agora := time.Now().UTC()

	aplicar := func(agregado uuid.UUID, tipo string, payload map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(payload)
		if err := proj.Handle(db.Event{AggregateID: agregado, AggregateType: "Financeiro", EventType: tipo, Payload: raw, OccurredAt: agora}); err != nil {
			t.Fatalf("%s: %v", tipo, err)
		}
	}
	aplicar(credID, "CredenciaisAppyPayConfiguradas", map[string]any{"credential_id": credID.String(), "contexto_tipo": ContextoAcademia, "codigo_academia": academia, "ambiente": AmbienteAtual(), "client_id_mask": "****1234", "gpo_payment_method_mask": "****GPO1", "ref_payment_method_mask": "****REF1"})
	aplicar(chargeID, "CobrancaAppyPayCriada", map[string]any{"merchant_transaction_id": integrationMerchant("LG"), "provider_charge_id": "leg-" + chargeID.String(), "contexto_tipo": ContextoAcademia, "codigo_academia": academia, "status": "Pending", "amount": 10.0})

	// Credenciais legadas viram a visão neutra e o provedor ativo é inferido.
	views, err := service.ListCredentials(ctx, ContextoAcademia, academia)
	if err != nil || len(views) != 1 {
		t.Fatalf("credenciais legadas: %v %v", views, err)
	}
	if views[0].Provedor != ProvedorAppyPay || !views[0].Ativo || views[0].CredenciaisMascaradas["client_id"] != "****1234" || views[0].CredenciaisMascaradas["ref_method"] != "****REF1" {
		t.Fatalf("visão da credencial legada = %+v", views[0])
	}
	if ativo, err := service.ProvedorAtivo(ctx, ContextoAcademia, academia); err != nil || ativo != ProvedorAppyPay {
		t.Fatalf("provedor inferido = %q, %v", ativo, err)
	}
	if p := provedorDaCobrancaNoBanco(t, client, chargeID); p != ProvedorAppyPay {
		t.Fatalf("cobrança legada sem provedor deveria ser APPYPAY, obteve %q", p)
	}

	// Seleção explícita posterior prevalece; remover as credenciais do provedor
	// ativo deixa o contexto sem provedor.
	aplicar(provedorAggregateID(ContextoAcademia, academia), "ProvedorPagamentoDefinido", map[string]any{"contexto_tipo": ContextoAcademia, "codigo_academia": academia, "provedor": provedorFalsoID, "definido_por": "u"})
	if ativo, _ := service.ProvedorAtivo(ctx, ContextoAcademia, academia); ativo != provedorFalsoID {
		t.Fatalf("seleção explícita não prevaleceu: %q", ativo)
	}
	aplicar(provedorAggregateID(ContextoAcademia, academia), "ProvedorPagamentoDefinido", map[string]any{"contexto_tipo": ContextoAcademia, "codigo_academia": academia, "provedor": ProvedorAppyPay, "definido_por": "u"})
	aplicar(credID, "CredenciaisAppyPayRemovidas", map[string]any{"credential_id": credID.String(), "contexto_tipo": ContextoAcademia, "codigo_academia": academia})
	if _, err := service.ProvedorAtivo(ctx, ContextoAcademia, academia); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sem credenciais o provedor não pode continuar ativo: %v", err)
	}
}
