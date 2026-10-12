// provedor_appypay.go concentra TUDO o que é específico da AppyPay: ambiente
// OAuth2, endpoints, token, tabela de códigos, tradução de respostas para os
// estados da plataforma e autenticação de webhooks. O restante do pacote
// finance só fala com a AppyPay através da interface Provedor.
package finance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Endpoints struct{ TokenURL, APIBaseURL string }

func EndpointsAtuais() Endpoints {
	if AmbienteAtual() == AmbienteProducao {
		return Endpoints{"https://login.microsoftonline.com/auth.appypay.co.ao/oauth2/token", "https://gwy-api.appypay.co.ao/v2.0"}
	}
	return Endpoints{"https://login.microsoftonline.com/appypaydev.onmicrosoft.com/oauth2/token", "https://gwy-api-tst.appypay.co.ao/v2.0"}
}

// appyPayResource retorna o "resource" (UUID) exigido pela AppyPay no pedido
// de token OAuth2 client_credentials. Ao contrário de client_id/client_secret,
// este valor identifica a própria API da AppyPay (audience do token), não o
// merchant que está a autenticar — por isso é o mesmo para todas as
// academias e para o Spuri no mesmo ambiente, e vem de variável de ambiente
// em vez de ser configurado por credencial.
func appyPayResource() (string, error) {
	v := strings.TrimSpace(os.Getenv("APPYPAY_RESOURCE"))
	if v == "" {
		return "", errors.New("APPYPAY_RESOURCE não configurada")
	}
	return v, nil
}

// ValidateAppyPayResourceConfig valida no arranque do servidor que
// APPYPAY_RESOURCE está definida, antes de qualquer tentativa de gerar um
// token AppyPay.
func ValidateAppyPayResourceConfig() error {
	_, err := appyPayResource()
	return err
}

type tokenEntry struct {
	value     string
	expiresAt time.Time
}

func (p *appyPayProvider) callJSON(ctx context.Context, cred credencialProvedor, method, path string, body any, async bool) (map[string]any, error) {
	token, err := p.token(ctx, cred)
	if err != nil {
		return nil, err
	}
	var data io.Reader
	if body != nil {
		raw, e := json.Marshal(body)
		if e != nil {
			return nil, e
		}
		data = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, EndpointsAtuais().APIBaseURL+path, data)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if async {
		req.Header.Set("Accept", asyncAccept)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer res.Body.Close()
	limited := io.LimitReader(res.Body, 2<<20)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("%w: não foi possível ler a resposta AppyPay: %v", ErrUpstream, err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: AppyPay respondeu HTTP %d", ErrUpstream, res.StatusCode)
	}
	out := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("%w: resposta AppyPay inválida: %v", ErrUpstream, err)
		}
	}
	return out, nil
}

func (p *appyPayProvider) token(ctx context.Context, cred credencialProvedor) (string, error) {
	p.mu.Lock()
	cached, ok := p.tokens[cred.ID]
	p.mu.Unlock()
	if ok && time.Until(cached.expiresAt) > time.Minute {
		return cached.value, nil
	}
	resource, err := appyPayResource()
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {cred.Valores["client_id"]}, "client_secret": {cred.Valores["client_secret"]}, "resource": {resource}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, EndpointsAtuais().TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("%w: não foi possível ler a resposta de token: %v", ErrUpstream, err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("%w: token AppyPay recusado: HTTP %d", ErrUpstream, res.StatusCode)
	}
	var out struct {
		AccessToken string          `json:"access_token"`
		ExpiresIn   flexibleSeconds `json:"expires_in"`
	}
	if err = json.Unmarshal(raw, &out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("%w: resposta de token AppyPay inválida", ErrUpstream)
	}
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = 3600
	}
	p.mu.Lock()
	p.tokens[cred.ID] = tokenEntry{out.AccessToken, time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)}
	p.mu.Unlock()
	return out.AccessToken, nil
}

// flexibleSeconds decodifica um campo de segundos (usado aqui para
// expires_in) que a AppyPay pode enviar como número JSON puro OU como
// string JSON contendo um número. O endpoint real de token da AppyPay
// (login.microsoftonline.com/{tenant}/oauth2/token, o endpoint "v1" do
// Azure AD, não o "v2.0") devolve expires_in como STRING
// (ex.: "expires_in": "3599") — documentado no exemplo de resposta da
// secção "Get a token" e comportamento conhecido do próprio Azure AD v1
// (distinto do endpoint v2.0, que usa número). Antes desta correção o
// campo era declarado como int puro: json.Unmarshal de uma string JSON
// para um campo int retorna erro, e token() tratava qualquer erro de
// unmarshal como falha de autenticação total — mesmo com access_token
// presente e válido no mesmo payload. Ver
// docs/Debbugs/Auditoria de conformidade AppyPay — autenticação e geração
// de cobrança (produção).md.
type flexibleSeconds int

func (f *flexibleSeconds) UnmarshalJSON(b []byte) error {
	var asInt int
	if err := json.Unmarshal(b, &asInt); err == nil {
		*f = flexibleSeconds(asInt)
		return nil
	}
	var asString string
	if err := json.Unmarshal(b, &asString); err != nil {
		return err
	}
	n, err := strconv.Atoi(strings.TrimSpace(asString))
	if err != nil {
		return err
	}
	*f = flexibleSeconds(n)
	return nil
}

func responseID(v map[string]any) string {
	for _, k := range []string{"id", "chargeId", "charge_id"} {
		if x, ok := v[k].(string); ok {
			return x
		}
	}
	// GET /charges/{id} (e GET /charges?merchantTransactionId=...) devolvem
	// tudo dentro de um envelope "payment" — ver
	// extractProviderOutcome, logo abaixo, para o mesmo problema aplicado ao
	// status.
	if payment, ok := v["payment"].(map[string]any); ok {
		for _, k := range []string{"id", "chargeId", "charge_id"} {
			if x, ok := payment[k].(string); ok {
				return x
			}
		}
	}
	return ""
}

func responseStatus(v map[string]any) string {
	for _, k := range []string{"status", "state"} {
		if x, ok := v[k].(string); ok {
			return x
		}
	}
	return ""
}

func extractProviderOutcome(v map[string]any) providerOutcome {
	if payment, ok := v["payment"].(map[string]any); ok {
		out := providerOutcome{}
		if s, ok := payment["status"].(string); ok {
			out.Status = s
		}
		if events, ok := payment["transactionEvents"].([]any); ok && len(events) > 0 {
			if last, ok := events[len(events)-1].(map[string]any); ok {
				if rs, ok := last["responseStatus"].(map[string]any); ok {
					applyResponseStatus(&out, rs)
				}
			}
		}
		if out.Status != "" || out.HasCode {
			resolveOutcomeStatus(&out)
			return out
		}
	}
	if rs, ok := v["responseStatus"].(map[string]any); ok {
		out := providerOutcome{}
		applyResponseStatus(&out, rs)
		if out.Status != "" || out.HasCode {
			resolveOutcomeStatus(&out)
			return out
		}
	}
	out := providerOutcome{Status: responseStatus(v)}
	resolveOutcomeStatus(&out)
	return out
}

// IsSuccessfulProviderPayload informa se um payload cru vindo da AppyPay —
// seja a resposta síncrona de POST /charges ou /qr-codes, seja o corpo de um
// webhook — representa um pagamento concluído com sucesso.
//
// É a MESMA extração/normalização usada por CreateCharge, CreateGPOQRCode e
// ConsultCharge (extractProviderOutcome + normalizeChargeStatus), incluindo
// a leitura correta de "responseStatus.status"/"responseStatus.successful",
// que é onde a AppyPay realmente coloca o status (nunca em campos soltos
// "status"/"state" na raiz do payload — ver seção "Merchant Webhooks" de
// docs/Parceiros e integrações/AppyPay Documentação.md). Qualquer código
// fora deste pacote que precise reagir a um sucesso da AppyPay (por exemplo,
// um handler HTTP de webhook decidindo se efetiva uma matrícula) deve
// chamar esta função em vez de reimplementar sua própria leitura do
// payload — ver docs/Debbugs/Auditoria de conformidade AppyPay (autenticação
// e geração de cobrança).md para o bug que isto substitui.
func IsSuccessfulProviderPayload(payload map[string]any) bool {
	return isSuccessfulChargeStatus(normalizeChargeStatus(extractProviderOutcome(payload).Status))
}

func applyResponseStatus(out *providerOutcome, rs map[string]any) {
	if s, ok := rs["status"].(string); ok {
		out.Status = s
	}
	if m, ok := rs["message"].(string); ok {
		out.Message = m
	}
	if src, ok := rs["source"].(string); ok {
		out.Source = src
	}
	switch c := rs["code"].(type) {
	case float64:
		out.Code = int(c)
		out.HasCode = true
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(c)); err == nil {
			out.Code = n
			out.HasCode = true
		}
	}
}

func resolveOutcomeStatus(out *providerOutcome) {
	if out.HasCode {
		if info, ok := appyPayCodeOutcomes[out.Code]; ok {
			out.Status = info.Estado
			out.Categoria = info.Categoria
			return
		}
		out.Categoria = "desconhecido"
		if out.Status == "" {
			out.Status = "Failed"
		}
	}
}

type appyPayCodeInfo struct {
	Estado    string
	Categoria string
}

var appyPayCodeOutcomes = map[int]appyPayCodeInfo{
	100: {"Success", ""}, 101: {"Pending", ""}, 102: {"Pending", ""}, 103: {"Success", ""}, 319: {"Pending", ""}, 1100: {"Success", ""},
	231: {"Cancelled", "recusado_pelo_cliente"}, 219: {"Cancelled", "recusado_pelo_cliente"},
	209: {"Cancelled", "saldo_insuficiente"}, 203: {"Cancelled", "saldo_insuficiente"}, 204: {"Cancelled", "saldo_insuficiente"},
	210: {"Cancelled", "tempo_esgotado"}, 211: {"Cancelled", "tempo_esgotado"},
	200: {"Cancelled", "recusado_pelo_processador"}, 206: {"Cancelled", "recusado_pelo_processador"}, 208: {"Cancelled", "recusado_pelo_processador"}, 217: {"Cancelled", "recusado_pelo_processador"}, 227: {"Cancelled", "recusado_pelo_processador"}, 230: {"Cancelled", "recusado_pelo_processador"}, 205: {"Cancelled", "recusado_pelo_processador"}, 207: {"Cancelled", "recusado_pelo_processador"}, 218: {"Cancelled", "recusado_pelo_processador"}, 226: {"Cancelled", "recusado_pelo_processador"},
	201: {"Cancelled", "recusado_pelo_emissor"}, 212: {"Cancelled", "recusado_pelo_emissor"}, 213: {"Cancelled", "recusado_pelo_emissor"}, 214: {"Cancelled", "recusado_pelo_emissor"}, 215: {"Cancelled", "recusado_pelo_emissor"}, 216: {"Cancelled", "recusado_pelo_emissor"}, 220: {"Cancelled", "recusado_pelo_emissor"}, 221: {"Cancelled", "recusado_pelo_emissor"}, 222: {"Cancelled", "recusado_pelo_emissor"}, 223: {"Cancelled", "recusado_pelo_emissor"}, 224: {"Cancelled", "recusado_pelo_emissor"}, 225: {"Cancelled", "recusado_pelo_emissor"}, 228: {"Cancelled", "recusado_pelo_emissor"}, 229: {"Cancelled", "recusado_pelo_emissor"}, 202: {"Cancelled", "recusado_pelo_emissor"},
	245: {"Expired", "referencia_expirada"}, 762: {"Failed", "referencia_invalida"}, 763: {"Failed", "referencia_duplicada"},
	233: {"Cancelled", "conta_invalida"}, 238: {"Cancelled", "recusado_pelo_processador"}, 239: {"Cancelled", "recusado_pelo_cliente"}, 240: {"Cancelled", "recusado_pelo_processador"}, 242: {"Cancelled", "conta_invalida"}, 243: {"Cancelled", "pin_invalido"}, 244: {"Cancelled", "erro_interno_provedor"}, 246: {"Failed", "erro_interno_provedor"}, 247: {"Failed", "erro_interno_provedor"}, 248: {"Failed", "erro_interno_provedor"}, 309: {"Failed", "erro_comunicacao"}, 310: {"Failed", "erro_interno_provedor"}, 311: {"Failed", "erro_interno_provedor"}, 312: {"Failed", "erro_interno_provedor"}, 313: {"Failed", "erro_interno_provedor"}, 314: {"Failed", "erro_comunicacao"}, 315: {"Failed", "erro_interno_provedor"}, 316: {"Failed", "erro_interno_provedor"}, 317: {"Failed", "erro_interno_provedor"}, 413: {"Failed", "erro_interno_provedor"}, 414: {"Failed", "erro_interno_provedor"}, 415: {"Failed", "erro_interno_provedor"}, 416: {"Failed", "erro_interno_provedor"}, 417: {"Failed", "erro_interno_provedor"}, 418: {"Failed", "erro_interno_provedor"}, 759: {"Failed", "valor_minimo"},
	249: {"Cancelled", "erro_interno_provedor"}, 318: {"Failed", "erro_interno_provedor"}, 301: {"Failed", "erro_interno_provedor"}, 302: {"Failed", "erro_interno_provedor"}, 306: {"Failed", "erro_comunicacao"}, 308: {"Failed", "erro_interno_provedor"}, 402: {"Failed", "erro_interno_provedor"}, 403: {"Failed", "erro_interno_provedor"}, 404: {"Failed", "erro_interno_provedor"}, 405: {"Failed", "erro_interno_provedor"}, 406: {"Failed", "erro_comunicacao"}, 407: {"Failed", "erro_comunicacao"}, 408: {"Failed", "erro_interno_provedor"}, 410: {"Failed", "erro_interno_provedor"}, 411: {"Failed", "erro_interno_provedor"}, 412: {"Failed", "erro_interno_provedor"}, 440: {"Failed", "erro_interno_provedor"}, 900: {"Failed", "erro_desconhecido"}, 901: {"Failed", "erro_desconhecido"}, 1101: {"Failed", "conta_inativa"}, 1102: {"Failed", "conta_inativa"}, 1103: {"Failed", "conta_inativa"}, 1104: {"Failed", "conta_inativa"}, 1105: {"Failed", "conta_inativa"}, -1: {"Failed", "erro_desconhecido"},
	717: {"Failed", "dados_invalidos"}, 718: {"Failed", "dados_invalidos"}, 719: {"Failed", "dados_invalidos"}, 720: {"Failed", "transacao_nao_encontrada"}, 726: {"Failed", "referencia_duplicada"}, 760: {"Failed", "dados_invalidos"}, 761: {"Failed", "dados_invalidos"}, 800: {"Failed", "dados_invalidos"}, 803: {"Failed", "dados_invalidos"}, 500: {"Failed", "erro_interno_provedor"}, 501: {"Failed", "erro_interno_provedor"}, 502: {"Failed", "erro_interno_provedor"}, 503: {"Failed", "erro_interno_provedor"}, 504: {"Failed", "erro_interno_provedor"}, 505: {"Failed", "erro_interno_provedor"}, 507: {"Failed", "erro_interno_provedor"}, 508: {"Failed", "erro_interno_provedor"}, 801: {"Failed", "dados_invalidos"}, 802: {"Failed", "dados_invalidos"},
}

const asyncAccept = "application/vnd.appypay.asyncapi+json"

// appyPayProvider implementa Provedor para a AppyPay. Tem o seu próprio
// cliente HTTP e cache de tokens OAuth2 — estado que antes vivia no Service.
type appyPayProvider struct {
	httpClient *http.Client
	mu         sync.Mutex
	tokens     map[uuid.UUID]tokenEntry
}

func novoProvedorAppyPay(client *http.Client) Provedor {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &appyPayProvider{httpClient: client, tokens: map[uuid.UUID]tokenEntry{}}
}

func (p *appyPayProvider) ID() string   { return ProvedorAppyPay }
func (p *appyPayProvider) Nome() string { return "AppyPay" }
func (p *appyPayProvider) Slug() string { return "appypay" }
func (p *appyPayProvider) Metodos() []string {
	return []string{MetodoREF, MetodoGPO, MetodoGPOQR}
}

// Campos: as chaves coincidem com os tipos de segredo já gravados no cofre
// (client_id, client_secret, gpo_method, ref_method), o que evita migrar
// ciphertexts existentes.
func (p *appyPayProvider) Campos() []CampoCredencial {
	return []CampoCredencial{
		{Chave: "client_id", Rotulo: "Client ID", Obrigatorio: true},
		{Chave: "client_secret", Rotulo: "Client Secret", Obrigatorio: true, Secreto: true},
		{Chave: "gpo_method", Rotulo: "Método de pagamento GPO", Obrigatorio: true, Ajuda: "Começa por GPO_ (ex.: GPO_xxxxxxxx-xxxx)"},
		{Chave: "ref_method", Rotulo: "Método de pagamento REF", Obrigatorio: true, Ajuda: "Começa por REF_ (ex.: REF_xxxx)"},
	}
}

func (p *appyPayProvider) ValidarCredenciais(v map[string]string) (map[string]string, error) {
	if strings.TrimSpace(v["client_id"]) == "" || strings.TrimSpace(v["client_secret"]) == "" || !strings.HasPrefix(v["gpo_method"], "GPO_") || !strings.HasPrefix(v["ref_method"], "REF_") {
		return nil, errors.New("credenciais AppyPay incompletas ou inválidas")
	}
	return map[string]string{"client_id": v["client_id"], "client_secret": v["client_secret"], "gpo_method": v["gpo_method"], "ref_method": v["ref_method"]}, nil
}

// Mascarar nunca inclui o client_secret.
func (p *appyPayProvider) Mascarar(v map[string]string) map[string]string {
	return map[string]string{"client_id": mask(v["client_id"]), "gpo_method": mask(v["gpo_method"]), "ref_method": mask(v["ref_method"])}
}

func (p *appyPayProvider) ValidarAmbiente() error { return ValidateAppyPayResourceConfig() }

// codigoMetodo traduz um método canônico para o código de paymentMethod
// contratado pela conta na AppyPay.
func (p *appyPayProvider) codigoMetodo(cred credencialProvedor, canonico string) string {
	switch canonico {
	case MetodoGPO, MetodoGPOQR:
		return cred.Valores["gpo_method"]
	case MetodoREF:
		return cred.Valores["ref_method"]
	}
	return ""
}

func (p *appyPayProvider) ResolverMetodo(cred credencialProvedor, solicitado string) (string, error) {
	requested := strings.TrimSpace(solicitado)
	if strings.EqualFold(requested, MetodoGPO) || strings.EqualFold(requested, cred.Valores["gpo_method"]) {
		return MetodoGPO, nil
	}
	if strings.EqualFold(requested, MetodoREF) || strings.EqualFold(requested, cred.Valores["ref_method"]) {
		return MetodoREF, nil
	}
	return "", errors.New("paymentMethod não contratado para esta conta")
}

func (p *appyPayProvider) CriarCobranca(ctx context.Context, cred credencialProvedor, pedido pedidoCobranca) (respostaProvedor, error) {
	body := map[string]any{"amount": pedido.Valor, "currency": pedido.Moeda, "description": pedido.Descricao, "merchantTransactionId": pedido.IDComerciante, "paymentMethod": p.codigoMetodo(cred, pedido.Metodo)}
	// paymentInfo/options/notify só entram no corpo quando têm conteúdo real.
	// body é um map[string]any bruto (não uma struct com `omitempty`), então
	// json.Marshal sempre serializa toda chave presente no map — mesmo que o
	// valor seja um map[string]any{} vazio (vira "{}", não é omitido). Isto
	// importa sobretudo para REF com referência gerada pelo gateway, onde a
	// documentação da AppyPay mostra o corpo sem a chave "paymentInfo": um
	// objeto vazio ali pode ser lido pela AppyPay como "o merchant tentou
	// enviar uma referência própria, mas sem os campos exigidos", em vez de
	// "nenhuma referência própria, gerar automaticamente". Ver
	// docs/Debbugs/Auditoria de conformidade AppyPay (autenticação e geração
	// de cobrança).md.
	if len(pedido.Info) > 0 {
		body["paymentInfo"] = pedido.Info
	}
	if len(pedido.Opcoes) > 0 {
		body["options"] = pedido.Opcoes
	}
	if len(pedido.Notificar) > 0 {
		body["notify"] = pedido.Notificar
	}
	response, err := p.callJSON(ctx, cred, http.MethodPost, "/charges", body, pedido.Async)
	if err != nil {
		return respostaProvedor{}, err
	}
	return respostaProvedor{ID: responseID(response), Outcome: extractProviderOutcome(response), Bruta: response}, nil
}

func (p *appyPayProvider) CriarQRCode(ctx context.Context, cred credencialProvedor, pedido pedidoQRCode) (respostaProvedor, error) {
	body := map[string]any{"amount": pedido.Valor, "currency": pedido.Moeda, "description": pedido.Descricao, "merchantTransactionId": pedido.IDComerciante, "paymentMethod": p.codigoMetodo(cred, MetodoGPOQR), "qrCodeType": pedido.Tipo}
	if pedido.Tipo == "MULTIPLE" {
		body["minAmount"] = *pedido.ValorMinimo
		body["maxTransactions"] = *pedido.MaxTransacoes
		body["startDate"] = pedido.Inicio
		body["endDate"] = pedido.Fim
	}
	response, err := p.callJSON(ctx, cred, http.MethodPost, "/qr-codes", body, false)
	if err != nil {
		return respostaProvedor{}, err
	}
	outcome := extractProviderOutcome(response)
	if outcome.HasCode && outcome.Code == 103 {
		// O código 103 na documentação da AppyPay ("Post a GPO QR Code",
		// resposta de exemplo) significa exclusivamente "QR code criado com
		// sucesso" — o próprio campo literal "status" da resposta real é
		// "Active" (QR pronto para ser escaneado), nunca "pago". Só existe
		// nesta chamada de CRIAÇÃO; uma vez o cliente realmente pagar
		// escaneando o QR, a confirmação chega depois por webhook ou
		// consulta, com um código diferente (100/1100) — aí sim tratado
		// como sucesso real de pagamento pela mesma appyPayCodeOutcomes.
		//
		// appyPayCodeOutcomes mapeia 103 para "Success" porque é esse o
		// bracket que a própria documentação da AppyPay usa para o código —
		// mas ali "Success" quer dizer "a operação de API pedida (criar o
		// QR) teve sucesso", não "o pagamento foi recebido". Sem esta
		// correção, a criação do QR seria tratada como pagamento recebido e
		// a matrícula/mensalidade seria efetivada sem pagamento real.
		//
		// Código/mensagem/fonte do provedor continuam preservados em
		// outcome para auditoria; só o status usado nas decisões de
		// pagamento é que nunca pode ser "Success" aqui.
		outcome.Status = EstadoCobrancaAguardandoPagamento
	}
	qr, _ := response["qrCodeArr"].(string)
	return respostaProvedor{ID: responseID(response), Outcome: outcome, Bruta: response, QRCode: qr}, nil
}

func (p *appyPayProvider) ConsultarCobranca(ctx context.Context, cred credencialProvedor, ref referenciaCobranca) (respostaProvedor, error) {
	path := "/charges/" + url.PathEscape(ref.IDProvedor)
	if ref.IDProvedor == "" {
		path = "/charges?merchantTransactionId=" + url.QueryEscape(ref.IDComerciante)
	}
	response, err := p.callJSON(ctx, cred, http.MethodGet, path, nil, false)
	if err != nil {
		return respostaProvedor{}, err
	}
	return respostaProvedor{ID: responseID(response), Outcome: extractProviderOutcome(response), Bruta: response}, nil
}

// UsaSegredoWebhook: a AppyPay só oferece, no painel de webhooks, um único
// par nome/valor de cabeçalho HTTP — nunca query parameter, nunca corpo do
// POST, e nunca campos separados de utilizador/senha para Basic Auth
// (confirmado pela AppyPay por e-mail). O Spuri gera um segredo por
// credencial e a AppyPay o devolve no cabeçalho WebhookHeaderName.
func (p *appyPayProvider) UsaSegredoWebhook() bool { return true }

func (p *appyPayProvider) RotasWebhook() []RotaWebhook {
	return []RotaWebhook{{Caminho: "gpo", Metodo: MetodoGPO}, {Caminho: "ref", Metodo: MetodoREF}}
}

// AutenticarWebhook compara, em tempo constante, o valor do cabeçalho fixo
// com o webhook_secret de cada credencial candidata. Nunca revela qual conta
// correspondeu.
func (p *appyPayProvider) AutenticarWebhook(h http.Header, candidatos []credencialProvedor) (int, bool) {
	candidate := h.Get(WebhookHeaderName)
	if candidate == "" {
		return -1, false
	}
	for i, c := range candidatos {
		if secret := c.Valores["webhook_secret"]; secret != "" && constantTimeEqual(candidate, secret) {
			return i, true
		}
	}
	return -1, false
}

func (p *appyPayProvider) InterpretarWebhook(contentType string, corpo []byte) (string, map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(corpo, &payload); err != nil {
		return "", nil, err
	}
	for _, k := range []string{"id", "merchantTransactionId", "merchant_transaction_id"} {
		if v, ok := payload[k].(string); ok && strings.TrimSpace(v) != "" {
			return v, payload, nil
		}
	}
	return "", payload, errors.New("webhook sem identificador")
}

func (p *appyPayProvider) ResultadoWebhook(payload map[string]any) (providerOutcome, string) {
	return extractProviderOutcome(payload), responseID(payload)
}

// RespostaWebhook: a AppyPay só exige HTTP 2xx; corpo vazio.
func (p *appyPayProvider) RespostaWebhook(aceito bool) (int, string, string) {
	if aceito {
		return http.StatusOK, "", ""
	}
	return http.StatusInternalServerError, "", ""
}
