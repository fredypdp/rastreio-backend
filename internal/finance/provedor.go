package finance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// Este arquivo define o contrato entre o núcleo financeiro (Service) e os
// provedores de serviços de pagamento (PSP). O núcleo cuida do que é igual
// para qualquer provedor: validação, idempotência, ledger, máquina de estados
// da cobrança e efeitos de negócio (mensalidade, matrícula, serviço extra).
// Cada provedor vive no seu próprio arquivo (provedor_<nome>.go) e implementa
// apenas o que é específico dele: credenciais, chamadas HTTP, tradução das
// respostas para o vocabulário de estados da plataforma e autenticação/leitura
// dos webhooks.
//
// Para acrescentar um provedor novo (ex.: Intelize, PayPay):
//  1. crie provedor_<nome>.go com um tipo que implemente Provedor;
//  2. acrescente a sua fábrica em fabricasProvedores abaixo;
//  3. nada mais no núcleo muda — nem tabelas, nem eventos, nem rotas de
//     credenciais; as rotas de webhook são registradas a partir de
//     RotasWebhook().

// Identificadores dos provedores. São gravados no banco e nos eventos do
// ledger e portanto nunca podem mudar.
const (
	ProvedorAppyPay = "APPYPAY"
	// ProvedorExterno é o pseudo-provedor das cobranças registradas pela
	// academia como pagas FORA da plataforma (sem nenhum PSP por trás). Nunca
	// aparece no catálogo, não tem credenciais e nunca é consultado.
	ProvedorExterno = "EXTERNO"
)

// Métodos de pagamento canônicos da plataforma. São os valores guardados na
// configuração de mensalidade/matrícula das academias; cada provedor declara
// quais deles suporta e faz a tradução para os códigos próprios dele.
const (
	MetodoREF   = "REF"
	MetodoGPO   = "GPO"
	MetodoGPOQR = "GPO_QR"
)

// WebhookHeaderName é o cabeçalho HTTP padrão em que os provedores baseados em
// segredo compartilhado enviam o segredo de webhook. É fixo para toda a
// plataforma (todas as academias e o Spuri, em qualquer ambiente e provedor).
// Prefixado com o nome do produto, seguindo a convenção comum de cabeçalhos
// customizados (ex.: X-GitHub-*, X-Stripe-*). Se o nome precisar mudar no
// futuro, basta trocar esta constante — nenhum outro código depende do valor
// literal.
const WebhookHeaderName = "X-Spuri-Webhook-Secret"

var (
	// ErrProvedorDesconhecido: o identificador não corresponde a nenhum
	// provedor implementado.
	ErrProvedorDesconhecido = errors.New("provedor de pagamento desconhecido")
	// ErrProvedorNaoConfigurado: o contexto não definiu provedor ou o provedor
	// definido ainda não tem credenciais.
	ErrProvedorNaoConfigurado = errors.New("provedor de pagamento não configurado para este contexto")
	// ErrMetodoIndisponivel: o provedor ativo do contexto não suporta o método
	// de pagamento pedido.
	ErrMetodoIndisponivel = errors.New("método de pagamento indisponível no provedor do contexto")
	// ErrCredenciaisEmUso: o provedor ainda tem cobranças em andamento e as
	// suas credenciais não podem ser removidas (mecanismo retroativo).
	ErrCredenciaisEmUso = errors.New("o provedor ainda tem cobranças pendentes neste contexto")
)

// CampoCredencial descreve um campo de credencial exigido por um provedor. É
// devolvido no catálogo para que o frontend monte o formulário sem conhecer o
// provedor.
type CampoCredencial struct {
	Chave       string `json:"chave"`
	Rotulo      string `json:"rotulo"`
	Obrigatorio bool   `json:"obrigatorio"`
	// Secreto indica que o valor nunca é devolvido pela API (apenas mascarado).
	Secreto bool   `json:"secreto"`
	Ajuda   string `json:"ajuda,omitempty"`
}

// RotaWebhook liga um sub-caminho de /webhooks/<slug>/ a um método canônico.
type RotaWebhook struct {
	Caminho string `json:"caminho"`
	Metodo  string `json:"metodo"`
}

// InfoProvedor é a descrição pública (sem segredos) de um provedor.
type InfoProvedor struct {
	ID                string            `json:"id"`
	Nome              string            `json:"nome"`
	Slug              string            `json:"slug"`
	Metodos           []string          `json:"metodos"`
	Campos            []CampoCredencial `json:"campos_credenciais"`
	UsaSegredoWebhook bool              `json:"usa_segredo_webhook"`
	CabecalhoWebhook  string            `json:"cabecalho_webhook,omitempty"`
	Webhooks          []string          `json:"webhooks,omitempty"`
}

// credencialProvedor são as credenciais já decifradas de um provedor para um
// contexto. Valores é indexado pelas chaves de CampoCredencial.
type credencialProvedor struct {
	ID       uuid.UUID
	Provedor string
	Valores  map[string]string
}

// pedidoCobranca é a cobrança que o núcleo pede ao provedor. Metodo é sempre
// canônico (MetodoREF ou MetodoGPO). Info/Opcoes/Notificar são sacos de
// extensão que o provedor interpreta (ex.: telefone do pagador para GPO).
type pedidoCobranca struct {
	Valor         float64
	Moeda         string
	Descricao     string
	IDComerciante string
	Metodo        string
	Info          map[string]any
	Opcoes        map[string]any
	Notificar     map[string]any
	Async         bool
}

// pedidoQRCode é o pedido de QR Code de pagamento (método MetodoGPOQR).
type pedidoQRCode struct {
	Valor         float64
	Moeda         string
	Descricao     string
	IDComerciante string
	Tipo          string // SINGLE | MULTIPLE
	ValorMinimo   *float64
	MaxTransacoes *int
	Inicio, Fim   string
}

// referenciaCobranca identifica uma cobrança já emitida no provedor.
type referenciaCobranca struct {
	IDProvedor    string
	IDComerciante string
}

// respostaProvedor é a resposta de uma operação, já traduzida para o
// vocabulário da plataforma. Bruta guarda a resposta original (será
// sanitizada antes de ir ao ledger).
type respostaProvedor struct {
	ID      string
	Outcome providerOutcome
	Bruta   map[string]any
	QRCode  string
}

// Provedor é o contrato que cada PSP implementa.
type Provedor interface {
	ID() string
	Nome() string
	// Slug é o segmento usado nas URLs públicas de webhook (/webhooks/<slug>/…).
	Slug() string
	// Metodos lista os métodos canônicos suportados.
	Metodos() []string
	Campos() []CampoCredencial
	// ValidarCredenciais valida presença/formato; devolve o mapa normalizado.
	ValidarCredenciais(valores map[string]string) (map[string]string, error)
	// Mascarar devolve a versão exibível (sem segredos) das credenciais.
	Mascarar(valores map[string]string) map[string]string
	// ValidarAmbiente verifica, no arranque, a configuração de ambiente que o
	// provedor exige (variáveis de ambiente etc.).
	ValidarAmbiente() error

	// ResolverMetodo traduz o método pedido para um método canônico suportado
	// e contratado nas credenciais.
	ResolverMetodo(cred credencialProvedor, solicitado string) (string, error)
	CriarCobranca(ctx context.Context, cred credencialProvedor, p pedidoCobranca) (respostaProvedor, error)
	CriarQRCode(ctx context.Context, cred credencialProvedor, p pedidoQRCode) (respostaProvedor, error)
	ConsultarCobranca(ctx context.Context, cred credencialProvedor, ref referenciaCobranca) (respostaProvedor, error)

	// Webhooks. UsaSegredoWebhook indica que o Spuri gera e guarda um segredo
	// por credencial para autenticar as notificações. AutenticarWebhook
	// escolhe, entre as credenciais candidatas do provedor, a dona da
	// notificação (índice) — sem expor como cada provedor autentica.
	UsaSegredoWebhook() bool
	RotasWebhook() []RotaWebhook
	AutenticarWebhook(h http.Header, candidatos []credencialProvedor) (int, bool)
	// InterpretarWebhook extrai o identificador da notificação e o payload.
	InterpretarWebhook(contentType string, corpo []byte) (id string, payload map[string]any, err error)
	// ResultadoWebhook traduz o payload da notificação para o vocabulário da
	// plataforma e devolve o identificador do provedor presente nele.
	ResultadoWebhook(payload map[string]any) (providerOutcome, string)
	// RespostaWebhook devolve a resposta HTTP que o provedor espera.
	RespostaWebhook(aceito bool) (status int, contentType, corpo string)
}

// fabricaProvedor cria uma instância de provedor usando o cliente HTTP do
// Service (permite trocar o transporte nos testes sem estado global).
type fabricaProvedor func(client *http.Client) Provedor

// fabricasProvedores é a lista de provedores implementados.
var fabricasProvedores = []fabricaProvedor{
	novoProvedorAppyPay,
}

func novosProvedores(client *http.Client) map[string]Provedor {
	out := make(map[string]Provedor, len(fabricasProvedores))
	for _, nova := range fabricasProvedores {
		p := nova(client)
		out[p.ID()] = p
	}
	return out
}

// ValidarConfiguracaoProvedores é chamada no arranque do servidor: cada
// provedor valida a configuração de ambiente de que depende.
func ValidarConfiguracaoProvedores() error {
	provedores := novosProvedores(nil)
	for _, id := range idsProvedores(provedores) {
		p := provedores[id]
		if err := p.ValidarAmbiente(); err != nil {
			return fmt.Errorf("%s: %w", p.Nome(), err)
		}
	}
	return nil
}

func idsProvedores(m map[string]Provedor) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Provedores devolve o catálogo público dos provedores implementados.
func (s *Service) Provedores() []InfoProvedor {
	out := make([]InfoProvedor, 0, len(s.provedores))
	for _, id := range idsProvedores(s.provedores) {
		out = append(out, infoDoProvedor(s.provedores[id]))
	}
	return out
}

func infoDoProvedor(p Provedor) InfoProvedor {
	info := InfoProvedor{ID: p.ID(), Nome: p.Nome(), Slug: p.Slug(), Metodos: append([]string(nil), p.Metodos()...), Campos: p.Campos(), UsaSegredoWebhook: p.UsaSegredoWebhook()}
	if p.UsaSegredoWebhook() {
		info.CabecalhoWebhook = WebhookHeaderName
	}
	for _, r := range p.RotasWebhook() {
		info.Webhooks = append(info.Webhooks, "/webhooks/"+p.Slug()+"/"+r.Caminho)
	}
	return info
}

// RotasWebhookDoProvedor devolve as rotas de webhook de um provedor (vazio se
// o provedor não existir ou não usar webhooks).
func (s *Service) RotasWebhookDoProvedor(id string) []RotaWebhook {
	p, err := s.ProvedorPorID(id)
	if err != nil {
		return nil
	}
	return p.RotasWebhook()
}

// ProvedorPorID devolve o provedor implementado com o identificador dado.
func (s *Service) ProvedorPorID(id string) (Provedor, error) {
	p, ok := s.provedores[strings.ToUpper(strings.TrimSpace(id))]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrProvedorDesconhecido, id)
	}
	return p, nil
}

// SuportaMetodo informa se o provedor suporta o método canônico.
func suportaMetodo(p Provedor, metodo string) bool {
	for _, m := range p.Metodos() {
		if strings.EqualFold(m, metodo) {
			return true
		}
	}
	return false
}

// provedorPadraoLegado é o provedor assumido para registros anteriores à
// Tarefa 119, quando a AppyPay era o único provedor e o payload não trazia o
// campo "provedor".
const provedorPadraoLegado = ProvedorAppyPay

// provedorDoPayloadCobranca lê o provedor de pagamento de um payload de
// cobrança, assumindo o provedor legado quando o campo não existe.
func provedorDoPayloadCobranca(payload map[string]any) string {
	if p, _ := payload["provedor"].(string); p != "" {
		return p
	}
	return provedorPadraoLegado
}
