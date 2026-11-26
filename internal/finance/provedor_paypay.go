// provedor_paypay.go concentra TUDO o que é específico do PayPay AO: pedido
// assinado com RSA, tradução dos estados, fecho de pedido e autenticação das
// notificações pela assinatura do próprio PayPay. O restante do pacote finance
// só fala com o PayPay através da interface Provedor (e das extensões
// opcionais declaradas em provedor.go).
//
// Fonte: docs/Integrações/PayPay API.md. Entram só os serviços de que a
// plataforma precisa: MULTICAIXA Express (§3.2, método interno GPO),
// MULTICAIXA Reference (§3.3, método interno REF), consultar pedido (§3.6),
// fechar pedido (§3.5) e notificações (§4). Ficam de fora: reembolso,
// transferências, Kwik e agente.
package finance

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" // #nosec G505 -- o PayPay exige SHA1withRSA (documentação §2.5.2)
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"spuri/internal/sysconfig"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MetodoQualquer é o "método" da rota única de notificações do PayPay: o
	// portal aceita um só endereço de notificação para todos os serviços, então
	// a rota não distingue Express de Reference (quem distingue é a cobrança).
	MetodoQualquer = "QUALQUER"

	paypayCodigoSucesso = "S0001"
	// paypayValorMinimoREF: o PayPay só aceita Reference acima de 1000 Kz (§3.3).
	paypayValorMinimoREF = 1000.0
	paypayLimiteResposta = 2 << 20
)

// Estados do PayPay (§3.6.3 e §4.1.4).
const (
	paypayAguardaPagamento = "WAIT_BUYER_PAY"
	paypayProcessando      = "P"
	paypayPagamentoOK      = "TRADE_SUCCESS"
	paypayLiquidado        = "TRADE_FINISHED"
	paypayFechado          = "TRADE_CLOSED"
)

// paypayProvider implementa Provedor (e ProvedorCancelavel,
// ProvedorComChaveDeEvento, ProvedorComPedidoNoWebhook, ProvedorValidaPedido,
// ProvedorReconciliavel) para o PayPay.
type paypayProvider struct {
	httpClient *http.Client
	agora      func() time.Time
}

func novoProvedorPayPay(client *http.Client) Provedor {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &paypayProvider{httpClient: client, agora: time.Now}
}

func (p *paypayProvider) ID() string   { return ProvedorPayPay }
func (p *paypayProvider) Nome() string { return "PayPay" }
func (p *paypayProvider) Slug() string { return "paypay" }

// Metodos: Express (GPO) e Reference (REF). O PayPay não tem QR Code GPO próprio
// no escopo desta integração (o Kwik, que tem, ficou de fora).
func (p *paypayProvider) Metodos() []string { return []string{MetodoREF, MetodoGPO} }

// Campos: o Endereço da API NÃO está aqui — é igual para todas as contas e vem
// da configuração do sistema (PAYPAY_API_URL), como o resource da AppyPay.
func (p *paypayProvider) Campos() []CampoCredencial {
	return []CampoCredencial{
		{Chave: "partner_id", Rotulo: "Partner ID", Obrigatorio: true, Ajuda: "Entregue pela equipa comercial do PayPay (só dígitos)"},
		{Chave: "sale_product_code", Rotulo: "Sale Product Code", Obrigatorio: true, Ajuda: "Entregue pela equipa comercial do PayPay (9 caracteres)"},
		{Chave: "chave_privada", Rotulo: "Chave privada RSA (1024 bits)", Obrigatorio: true, Secreto: true, Multilinha: true, Ajuda: "A que você gerou (formato PKCS#8, em base64 ou PEM). Nunca é mostrada de novo"},
		{Chave: "chave_publica_paypay", Rotulo: "Chave pública do PayPay", Obrigatorio: true, Multilinha: true, Ajuda: "A chave pública oficial do PayPay, descarregada em API Setting do portal (base64 ou PEM). Não confunda com a sua"},
	}
}

func (p *paypayProvider) ValidarCredenciais(v map[string]string) (map[string]string, error) {
	partner := strings.TrimSpace(v["partner_id"])
	if partner == "" || len(partner) > 32 || !soDigitos(partner) {
		return nil, errors.New("credenciais PayPay inválidas: o Partner ID deve conter só dígitos")
	}
	produto := strings.TrimSpace(v["sale_product_code"])
	if len(produto) != 9 || !soAlfanumerico(produto) {
		return nil, errors.New("credenciais PayPay inválidas: o Sale Product Code deve ter 9 caracteres")
	}
	priv, err := paypayChavePrivada(v["chave_privada"])
	if err != nil {
		return nil, fmt.Errorf("credenciais PayPay inválidas: chave privada: %w", err)
	}
	pub, err := paypayChavePublica(v["chave_publica_paypay"])
	if err != nil {
		return nil, fmt.Errorf("credenciais PayPay inválidas: chave pública do PayPay: %w", err)
	}
	// Uma chave pública igual à pública da chave privada indica que o utilizador
	// colou a sua própria chave no lugar da do PayPay (erro comum, §2.2).
	if priv.PublicKey.Equal(pub) {
		return nil, errors.New("credenciais PayPay inválidas: a chave pública informada é a SUA; informe a chave pública oficial do PayPay")
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"partner_id":           partner,
		"sale_product_code":    produto,
		"chave_privada":        base64.StdEncoding.EncodeToString(privDER),
		"chave_publica_paypay": base64.StdEncoding.EncodeToString(pubDER),
	}, nil
}

// Mascarar nunca inclui a chave privada.
func (p *paypayProvider) Mascarar(v map[string]string) map[string]string {
	return map[string]string{"partner_id": mask(v["partner_id"]), "sale_product_code": mask(v["sale_product_code"]), "chave_publica_paypay": mask(v["chave_publica_paypay"])}
}

// ValidarAmbiente: o endereço é opcional no arranque (o PayPay só passa a ser
// necessário quando uma conta o configura), mas se existir tem de ser válido.
func (p *paypayProvider) ValidarAmbiente() error {
	if _, err := paypayURLAPI(); err != nil && !errors.Is(err, errPayPayURLAusente) {
		return err
	}
	return nil
}

var errPayPayURLAusente = errors.New("PAYPAY_API_URL não configurada")

// paypayURLAPI devolve o endereço da API. https sempre; http só fora de
// produção (testes contra um servidor simulado local).
func paypayURLAPI() (string, error) {
	v := strings.TrimSpace(sysconfig.String("PAYPAY_API_URL"))
	if v == "" {
		return "", errPayPayURLAusente
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || u.User != nil {
		return "", errors.New("PAYPAY_API_URL inválida")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && AmbienteAtual() != AmbienteProducao) {
		return "", errors.New("PAYPAY_API_URL deve usar https")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func (p *paypayProvider) ResolverMetodo(_ credencialProvedor, solicitado string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(solicitado)) {
	case MetodoGPO:
		return MetodoGPO, nil
	case MetodoREF:
		return MetodoREF, nil
	}
	return "", errors.New("método de pagamento não suportado pelo PayPay")
}

// ValidarPedido aplica o limite do PayPay para Reference (§3.3) e o formato do
// telefone do Express (§3.2).
func (p *paypayProvider) ValidarPedido(metodo string, valor float64, info map[string]any) error {
	if metodo == MetodoREF && valor <= paypayValorMinimoREF {
		return fmt.Errorf("o PayPay só aceita pagamento por referência (REF) em valores acima de %.0f Kz", paypayValorMinimoREF)
	}
	if metodo == MetodoGPO {
		if _, err := paypayTelefone(info); err != nil {
			return err
		}
	}
	return nil
}

// PoliticaReconciliacao: o webhook é o caminho principal (o PayPay repete a
// notificação 7 vezes ao longo de cerca de 25 h); a reconciliação é a rede de
// segurança. Express resolve em minutos; Reference pode demorar dias.
func (p *paypayProvider) PoliticaReconciliacao(metodo string) (PoliticaReconciliacao, bool) {
	const (
		min = time.Minute
		h   = time.Hour
		dia = 24 * time.Hour
	)
	switch metodo {
	case MetodoGPO:
		return PoliticaReconciliacao{
			Faixas:      []FaixaReconciliacao{{IdadeAte: 15 * min, Intervalo: 3 * min}, {IdadeAte: 2 * h, Intervalo: 15 * min}, {IdadeAte: 24 * h, Intervalo: h}},
			IdadeMaxima: 24 * h,
		}, true
	case MetodoREF:
		return PoliticaReconciliacao{
			Faixas:      []FaixaReconciliacao{{IdadeAte: h, Intervalo: 15 * min}, {IdadeAte: 24 * h, Intervalo: h}, {IdadeAte: 7 * dia, Intervalo: 6 * h}, {IdadeAte: 30 * dia, Intervalo: dia}},
			IdadeMaxima: 30 * dia,
		}, true
	}
	return PoliticaReconciliacao{}, false
}

// ---- Pedidos --------------------------------------------------------------

func (p *paypayProvider) CriarCobranca(ctx context.Context, cred credencialProvedor, pedido pedidoCobranca) (respostaProvedor, error) {
	if pedido.Moeda != "" && !strings.EqualFold(pedido.Moeda, "AOA") {
		return respostaProvedor{}, errors.New("o PayPay só aceita a moeda AOA")
	}
	valor := strconv.FormatFloat(pedido.Valor, 'f', 2, 64)
	banco := "REF"
	pagamento := map[string]any{"pay_product_code": "31", "amount": valor}
	if pedido.Metodo == MetodoGPO {
		telefone, err := paypayTelefone(pedido.Info)
		if err != nil {
			return respostaProvedor{}, err
		}
		banco = "MUL"
		pagamento["phone_num"] = telefone
	}
	pagamento["bank_code"] = banco
	biz := map[string]any{
		"cashier_type":      "SDK",
		"payer_ip":          paypayIP(pedido.IPPagador),
		"sale_product_code": cred.Valores["sale_product_code"],
		"trade_info": map[string]any{
			"currency":            "AOA",
			"out_trade_no":        pedido.IDComerciante,
			"payee_identity":      cred.Valores["partner_id"],
			"payee_identity_type": "1",
			"price":               valor,
			"quantity":            "1",
			"subject":             truncarRunes(strings.TrimSpace(pedido.Descricao), 256),
			"total_amount":        valor,
		},
		"pay_method": pagamento,
	}
	resp, err := p.chamar(ctx, cred, "instant_trade", biz)
	if err != nil {
		return respostaProvedor{}, err
	}
	if !resp.sucesso() {
		// O PayPay recusou o pedido (dados inválidos, conta sem a modalidade
		// aprovada, valor abaixo do limite...). A cobrança não existe do lado
		// dele: falha, com o motivo que ele deu.
		return respostaProvedor{Outcome: resp.recusa(), Bruta: resp.bruta()}, nil
	}
	return respostaProvedor{ID: resp.texto("trade_no"), Outcome: paypayOutcomeDeEstado(resp.texto("status"), "", ""), Bruta: resp.bruta()}, nil
}

// CriarQRCode: o PayPay não oferece QR Code GPO nesta integração.
func (p *paypayProvider) CriarQRCode(context.Context, credencialProvedor, pedidoQRCode) (respostaProvedor, error) {
	return respostaProvedor{}, fmt.Errorf("%w: %s não é suportado pelo PayPay", ErrMetodoIndisponivel, MetodoGPOQR)
}

func (p *paypayProvider) ConsultarCobranca(ctx context.Context, cred credencialProvedor, ref referenciaCobranca) (respostaProvedor, error) {
	if strings.TrimSpace(ref.IDComerciante) == "" {
		return respostaProvedor{}, errors.New("cobrança PayPay sem identificador do comerciante")
	}
	resp, err := p.chamar(ctx, cred, "trade_query", map[string]any{"out_trade_no": ref.IDComerciante})
	if err != nil {
		return respostaProvedor{}, err
	}
	if !resp.sucesso() {
		// Uma consulta que falha NUNCA muda o estado da cobrança: não sabemos se
		// o pedido existe, só que a pergunta não foi respondida.
		return respostaProvedor{}, fmt.Errorf("%w: PayPay recusou a consulta (%s: %s)", ErrUpstream, resp.Code, resp.mensagem())
	}
	outcome := paypayOutcomeDeEstado(resp.texto("status"), "", "")
	// Valor do pedido segundo o PayPay (amount). O seller_actual_amount não se
	// usa: pode vir líquido de taxas e daria falsas divergências.
	if v, ok := valorDoPayload(resp.Biz["amount"]); ok {
		outcome.ValorProvedor = ptrFloat(v)
	}
	return respostaProvedor{ID: resp.texto("trade_no"), Outcome: outcome, Bruta: resp.bruta()}, nil
}

// CancelarCobranca fecha o pedido no PayPay (trade_close, §3.5): depois disto o
// pagador já não consegue pagar pela referência nem pelo pedido Express.
func (p *paypayProvider) CancelarCobranca(ctx context.Context, cred credencialProvedor, ref referenciaCobranca) error {
	if strings.TrimSpace(ref.IDComerciante) == "" {
		return errors.New("cobrança PayPay sem identificador do comerciante")
	}
	resp, err := p.chamar(ctx, cred, "trade_close", map[string]any{"out_trade_no": ref.IDComerciante})
	if err != nil {
		return err
	}
	if !resp.sucesso() {
		return fmt.Errorf("%w: o PayPay não fechou o pedido (%s: %s)", ErrUpstream, resp.Code, resp.mensagem())
	}
	return nil
}

// ---- Chamada assinada -----------------------------------------------------

type paypayResposta struct {
	Code    string
	SubCode string
	Msg     string
	SubMsg  string
	Biz     map[string]any
}

func (r paypayResposta) sucesso() bool { return r.Code == paypayCodigoSucesso }

func (r paypayResposta) texto(k string) string {
	switch v := r.Biz[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func (r paypayResposta) mensagem() string {
	for _, m := range []string{r.SubMsg, r.Msg} {
		if strings.TrimSpace(m) != "" {
			return m
		}
	}
	return "sem mensagem"
}

// recusa traduz um erro de negócio do PayPay para o vocabulário da plataforma.
func (r paypayResposta) recusa() providerOutcome {
	return providerOutcome{Status: "Failed", Message: r.mensagem(), Source: first(r.SubCode, r.Code), Categoria: "erro_desconhecido"}
}

// bruta é a resposta como a plataforma a guarda e devolve: os campos do pedido
// ao nível de cima (o front lê reference_id e entity_id diretamente) mais o
// código/mensagem do PayPay. trade_token e dynamic_link ficam de fora: servem
// para pagar pelo aplicativo PayPay, que esta integração não usa, e o link
// contém o token.
func (r paypayResposta) bruta() map[string]any {
	out := map[string]any{"code": r.Code, "sub_code": r.SubCode, "msg": r.Msg, "sub_msg": r.SubMsg}
	for k, v := range r.Biz {
		if k == "trade_token" || k == "dynamic_link" {
			continue
		}
		out[k] = v
	}
	return out
}

func (p *paypayProvider) chamar(ctx context.Context, cred credencialProvedor, servico string, biz map[string]any) (paypayResposta, error) {
	endereco, err := paypayURLAPI()
	if err != nil {
		return paypayResposta{}, err
	}
	priv, err := paypayChavePrivada(cred.Valores["chave_privada"])
	if err != nil {
		return paypayResposta{}, fmt.Errorf("chave privada PayPay inválida: %w", err)
	}
	corpo, err := paypayMontarPedido(priv, cred.Valores["partner_id"], servico, biz, p.agora())
	if err != nil {
		return paypayResposta{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endereco, bytes.NewReader(corpo))
	if err != nil {
		return paypayResposta{}, err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("Accept", "application/json")
	res, err := p.httpClient.Do(req)
	if err != nil {
		return paypayResposta{}, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, paypayLimiteResposta))
	if err != nil {
		return paypayResposta{}, fmt.Errorf("%w: não foi possível ler a resposta PayPay: %v", ErrUpstream, err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return paypayResposta{}, fmt.Errorf("%w: PayPay respondeu HTTP %d", ErrUpstream, res.StatusCode)
	}
	var envelope struct {
		Code    string          `json:"code"`
		SubCode string          `json:"sub_code"`
		Msg     string          `json:"msg"`
		SubMsg  string          `json:"sub_msg"`
		Biz     json.RawMessage `json:"biz_content"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Code == "" {
		return paypayResposta{}, fmt.Errorf("%w: resposta PayPay inválida", ErrUpstream)
	}
	out := paypayResposta{Code: envelope.Code, SubCode: envelope.SubCode, Msg: envelope.Msg, SubMsg: envelope.SubMsg, Biz: map[string]any{}}
	if len(envelope.Biz) > 0 && string(envelope.Biz) != "null" {
		bizRaw := []byte(envelope.Biz)
		// Defensivo: alguns gateways devolvem o objeto de negócio como texto JSON.
		var texto string
		if json.Unmarshal(bizRaw, &texto) == nil {
			bizRaw = []byte(texto)
		}
		if err := json.Unmarshal(bizRaw, &out.Biz); err != nil {
			return paypayResposta{}, fmt.Errorf("%w: biz_content do PayPay inválido", ErrUpstream)
		}
	}
	return out, nil
}

// paypayMontarPedido produz o corpo HTTP de um pedido (§2.3 a §2.6): objeto de
// negócio em JSON cifrado com a chave PRIVADA (em blocos), assinatura SHA1withRSA
// do texto ordenado e todos os valores com urlencode uma vez.
func paypayMontarPedido(priv *rsa.PrivateKey, partnerID, servico string, biz map[string]any, agora time.Time) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(biz); err != nil {
		return nil, err
	}
	bizJSON := bytes.TrimRight(buf.Bytes(), "\n")
	cifrado, err := paypayCifrarComChavePrivada(priv, bizJSON)
	if err != nil {
		return nil, err
	}
	requestNo := make([]byte, 16)
	if _, err = rand.Read(requestNo); err != nil {
		return nil, err
	}
	angola := time.FixedZone("WAT", 3600) // o PayPay exige o fuso de Angola (GMT+1)
	params := map[string]string{
		"charset":     "UTF-8",
		"biz_content": cifrado,
		"partner_id":  partnerID,
		"service":     servico,
		"request_no":  hex.EncodeToString(requestNo),
		"format":      "JSON",
		"language":    "pt",
		"version":     "1.0",
		"timestamp":   agora.In(angola).Format("2006-01-02 15:04:05"),
	}
	assinatura, err := paypayAssinar(priv, paypayTextoDaAssinatura(params))
	if err != nil {
		return nil, err
	}
	params["sign"] = assinatura
	params["sign_type"] = "RSA"
	codificado := make(map[string]string, len(params))
	for k, v := range params {
		codificado[k] = paypayURLEncode(v)
	}
	return json.Marshal(codificado)
}

// paypayURLEncode é o urlencode "rawurlencode" (espaço = %20).
func paypayURLEncode(v string) string {
	return strings.ReplaceAll(url.QueryEscape(v), "+", "%20")
}

// paypayTextoDaAssinatura: parâmetros ordenados pelo código ASCII do nome, sem
// valores vazios e sem sign/sign_type, no formato k1=v1&k2=v2 (§2.5.1). Os
// valores entram SEM urlencode.
func paypayTextoDaAssinatura(params map[string]string) string {
	chaves := make([]string, 0, len(params))
	for k, v := range params {
		if v == "" || strings.EqualFold(k, "sign") || strings.EqualFold(k, "sign_type") {
			continue
		}
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)
	partes := make([]string, len(chaves))
	for i, k := range chaves {
		partes[i] = k + "=" + params[k]
	}
	return strings.Join(partes, "&")
}

func paypayAssinar(priv *rsa.PrivateKey, texto string) (string, error) {
	soma := sha1.Sum([]byte(texto)) // #nosec G401 -- SHA1withRSA exigido pelo PayPay
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA1, soma[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func paypayVerificar(pub *rsa.PublicKey, texto, assinaturaB64 string) bool {
	// Um '+' do base64 que chegou sem urlencode vira espaço ao ler o formulário;
	// o base64 nunca contém espaços, então restaurá-lo é seguro.
	assinaturaB64 = strings.ReplaceAll(strings.TrimSpace(assinaturaB64), " ", "+")
	sig, err := base64.StdEncoding.DecodeString(assinaturaB64)
	if err != nil {
		return false
	}
	soma := sha1.Sum([]byte(texto)) // #nosec G401 -- SHA1withRSA exigido pelo PayPay
	return rsa.VerifyPKCS1v15(pub, crypto.SHA1, soma[:], sig) == nil
}

// paypayCifrarComChavePrivada faz a "cifra com chave privada" do §2.4: RSA
// PKCS#1 v1.5 (tipo 1) em blocos de k-11 bytes, concatenados e em base64. O
// preenchimento de tipo 1 é exatamente o de uma assinatura PKCS#1 v1.5 sem
// DigestInfo, por isso se usa SignPKCS1v15 com hash nulo.
func paypayCifrarComChavePrivada(priv *rsa.PrivateKey, dados []byte) (string, error) {
	bloco := priv.Size() - 11
	if bloco <= 0 {
		return "", errors.New("chave RSA demasiado pequena")
	}
	var out bytes.Buffer
	for off := 0; off < len(dados); off += bloco {
		fim := off + bloco
		if fim > len(dados) {
			fim = len(dados)
		}
		c, err := rsa.SignPKCS1v15(nil, priv, crypto.Hash(0), dados[off:fim])
		if err != nil {
			return "", err
		}
		out.Write(c)
	}
	return base64.StdEncoding.EncodeToString(out.Bytes()), nil
}

// ---- Chaves ---------------------------------------------------------------

// paypayMarcadorPEM apanha as linhas "-----BEGIN ...-----" e "-----END ...-----".
var paypayMarcadorPEM = regexp.MustCompile(`-----[A-Za-z0-9 ]+-----`)

// paypayDecodificarChave aceita a chave em base64 puro (como o portal do PayPay
// a entrega) ou em PEM. Os marcadores e todos os espaços/quebras de linha são
// removidos antes de decodificar, de modo que um PEM colado num campo que perde
// as quebras de linha também funciona.
func paypayDecodificarChave(raw string) ([]byte, error) {
	raw = strings.TrimSpace(paypayMarcadorPEM.ReplaceAllString(raw, ""))
	if raw == "" {
		return nil, errors.New("vazia")
	}
	compacto := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, raw)
	der, err := base64.StdEncoding.DecodeString(compacto)
	if err != nil {
		der, err = base64.RawStdEncoding.DecodeString(compacto)
	}
	if err != nil {
		return nil, errors.New("não é base64 nem PEM")
	}
	return der, nil
}

func paypayChavePrivada(raw string) (*rsa.PrivateKey, error) {
	der, err := paypayDecodificarChave(raw)
	if err != nil {
		return nil, err
	}
	var chave any
	if chave, err = x509.ParsePKCS8PrivateKey(der); err != nil {
		pk, e := x509.ParsePKCS1PrivateKey(der)
		if e != nil {
			return nil, errors.New("formato inválido (esperado PKCS#8 ou PKCS#1)")
		}
		chave = pk
	}
	priv, ok := chave.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("não é uma chave RSA")
	}
	if priv.N.BitLen() < 1024 {
		return nil, errors.New("a chave RSA deve ter pelo menos 1024 bits")
	}
	return priv, nil
}

func paypayChavePublica(raw string) (*rsa.PublicKey, error) {
	der, err := paypayDecodificarChave(raw)
	if err != nil {
		return nil, err
	}
	var chave any
	if chave, err = x509.ParsePKIXPublicKey(der); err != nil {
		pk, e := x509.ParsePKCS1PublicKey(der)
		if e != nil {
			return nil, errors.New("formato inválido (esperado X.509/PKIX)")
		}
		chave = pk
	}
	pub, ok := chave.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("não é uma chave RSA")
	}
	if pub.N.BitLen() < 1024 {
		return nil, errors.New("a chave RSA deve ter pelo menos 1024 bits")
	}
	return pub, nil
}

// ---- Estados --------------------------------------------------------------

// paypayOutcomeDeEstado traduz o estado do PayPay para o vocabulário da
// plataforma. Só TRADE_FINISHED (pagamento liquidado na conta do comerciante)
// vale como pago: o PayPay recomenda tratar TRADE_SUCCESS como aviso, sem efeito
// de negócio (§4.1.4). Estado desconhecido, ou sem efeito, devolve Status
// vazio — o núcleo então mantém o estado atual da cobrança.
func paypayOutcomeDeEstado(estado, failCode, failReason string) providerOutcome {
	switch strings.ToUpper(strings.TrimSpace(estado)) {
	case paypayLiquidado:
		return providerOutcome{Status: "Success"}
	case paypayAguardaPagamento, paypayProcessando:
		return providerOutcome{Status: "Pending"}
	case paypayFechado:
		if strings.TrimSpace(failCode) != "" || strings.TrimSpace(failReason) != "" {
			return providerOutcome{Status: "Failed", Message: failReason, Source: failCode, Categoria: "recusado_pelo_processador"}
		}
		return providerOutcome{Status: "Expired", Categoria: "tempo_esgotado"}
	}
	// TRADE_SUCCESS, REFUND_*, TRANSFER_* e desconhecidos: sem efeito.
	return providerOutcome{}
}

// ---- Webhooks -------------------------------------------------------------

// UsaSegredoWebhook: o PayPay assina as notificações com RSA; o Spuri não gera
// segredo nem cabeçalho.
func (p *paypayProvider) UsaSegredoWebhook() bool { return false }

// RotasWebhook: o portal do PayPay aceita um único endereço de notificação
// (API Setting) para todos os serviços.
func (p *paypayProvider) RotasWebhook() []RotaWebhook {
	return []RotaWebhook{{Caminho: "notificacoes", Metodo: MetodoQualquer}}
}

// AutenticarWebhook verifica a assinatura RSA da notificação (§4.1.3) com a
// chave pública do PayPay de cada credencial candidata. O núcleo já restringiu
// os candidatos à credencial que emitiu a cobrança, quando a conhece.
func (p *paypayProvider) AutenticarWebhook(_ http.Header, corpo []byte, candidatos []credencialProvedor) (int, bool) {
	campos, err := paypayCamposDoCorpo(corpo)
	if err != nil {
		return -1, false
	}
	assinatura := campos["sign"]
	if assinatura == "" {
		return -1, false
	}
	texto := paypayTextoDaAssinatura(campos)
	for i, c := range candidatos {
		pub, err := paypayChavePublica(c.Valores["chave_publica_paypay"])
		if err != nil {
			continue
		}
		if paypayVerificar(pub, texto, assinatura) {
			return i, true
		}
	}
	return -1, false
}

// paypayCamposDoCorpo lê o formulário (application/x-www-form-urlencoded, §4.1)
// e devolve o primeiro valor de cada campo.
func paypayCamposDoCorpo(corpo []byte) (map[string]string, error) {
	valores, err := url.ParseQuery(string(corpo))
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(valores))
	for k, v := range valores {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out, nil
}

// PedidoDoWebhook devolve o número de pedido do comerciante presente no corpo.
func (p *paypayProvider) PedidoDoWebhook(corpo []byte) string {
	campos, err := paypayCamposDoCorpo(corpo)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(campos["out_trade_no"])
}

func (p *paypayProvider) InterpretarWebhook(_ string, corpo []byte) (string, map[string]any, error) {
	campos, err := paypayCamposDoCorpo(corpo)
	if err != nil {
		return "", nil, err
	}
	payload := make(map[string]any, len(campos))
	for k, v := range campos {
		if k == "sign" || k == "sign_type" {
			continue // a assinatura já foi verificada; não vale a pena guardá-la
		}
		payload[k] = v
	}
	pedido := strings.TrimSpace(campos["out_trade_no"])
	if pedido == "" {
		return "", payload, errors.New("webhook sem out_trade_no")
	}
	return pedido, payload, nil
}

// ChaveEventoWebhook: o PayPay envia várias notificações por pedido
// (TRADE_SUCCESS, TRADE_FINISHED, TRADE_CLOSED) e repete cada uma até 7 vezes. A
// chave é o notify_id (igual nas repetições da mesma notificação); sem ele, o
// pedido mais o estado.
func (p *paypayProvider) ChaveEventoWebhook(payload map[string]any) string {
	if id, _ := payload["notify_id"].(string); strings.TrimSpace(id) != "" {
		return "notify:" + strings.TrimSpace(id)
	}
	pedido, _ := payload["out_trade_no"].(string)
	estado, _ := payload["status"].(string)
	return "pedido:" + pedido + ":" + estado
}

func (p *paypayProvider) ResultadoWebhook(payload map[string]any) (providerOutcome, string) {
	estado, _ := payload["status"].(string)
	failCode, _ := payload["failCode"].(string)
	failReason, _ := payload["failReason"].(string)
	tradeNo, _ := payload["inner_trade_no"].(string)
	// Reembolsos e transferências também notificam por este endereço: só as
	// notificações de estado de pedido interessam à cobrança.
	if tipo, _ := payload["notify_type"].(string); tipo != "" && tipo != "trade_status_sync" {
		return providerOutcome{}, tradeNo
	}
	if orig, _ := payload["orig_out_trade_no"].(string); strings.TrimSpace(orig) != "" {
		return providerOutcome{}, tradeNo // notificação de reembolso
	}
	outcome := paypayOutcomeDeEstado(estado, failCode, failReason)
	if v, ok := valorDoPayload(payload["amount"]); ok {
		outcome.ValorProvedor = ptrFloat(v)
	}
	return outcome, tradeNo
}

// RespostaWebhook: o PayPay exige exatamente "success", sem espaços nem HTML
// (§4). Sem essa resposta repete a notificação 7 vezes.
func (p *paypayProvider) RespostaWebhook(bool) (int, string, string) {
	return http.StatusOK, "text/plain; charset=utf-8", "success"
}

// ---- Utilidades -----------------------------------------------------------

func soDigitos(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func soAlfanumerico(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return s != ""
}

func truncarRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// paypayIP devolve um IP aceitável para o PayPay (texto de 6 a 32
// caracteres). Sem IP válido (execução em segundo plano, IPv6 curto como ::1),
// usa o endereço de retorno.
func paypayIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if h, _, err := net.SplitHostPort(ip); err == nil {
		ip = h
	}
	if parsed := net.ParseIP(ip); parsed != nil && len(ip) >= 6 && len(ip) <= 32 {
		return ip
	}
	return "127.0.0.1"
}

// paypayTelefone normaliza o telefone do MULTICAIXA Express (9 dígitos, §3.2):
// aceita +244, 244 e espaços.
func paypayTelefone(info map[string]any) (string, error) {
	bruto, _ := info["phoneNumber"].(string)
	var digitos strings.Builder
	for _, r := range bruto {
		if r >= '0' && r <= '9' {
			digitos.WriteRune(r)
		}
	}
	t := digitos.String()
	if len(t) == 12 && strings.HasPrefix(t, "244") {
		t = t[3:]
	}
	if len(t) != 9 || t[0] != '9' {
		return "", errors.New("telefone inválido para o MULTICAIXA Express: informe os 9 dígitos do número registado no Express")
	}
	return t, nil
}
