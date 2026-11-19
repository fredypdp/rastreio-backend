// Package sysconfig resolve as configurações operacionais do sistema que o
// administrador (fpp) pode alterar pela tela de administração, sem novo deploy
// (Parte 3 do Plano de Atualizações).
//
// Ordem de leitura de cada chave: valor gravado no banco → variável de
// ambiente com o mesmo nome (compatibilidade com o que já está em produção).
// Só as chaves listadas em Definicoes podem ser gravadas; o que fica de fora
// (DATABASE_URL, JWT_SECRET, ENV, PORT, STORAGE_PROVIDER...) continua só no
// ambiente, porque é o que o sistema precisa para chegar ao banco ou é o que
// protege o próprio cofre de segredos.
//
// O pacote é uma folha de propósito: só importa a biblioteca padrão, uuid e
// internal/security. Assim utils, middleware, services, finance e storage
// podem lê-lo sem ciclo de import.
package sysconfig

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Tipo é o tipo do valor de uma configuração.
type Tipo string

const (
	TipoTexto    Tipo = "texto"
	TipoNumero   Tipo = "numero"
	TipoBooleano Tipo = "booleano"
	// TipoSegredo é um texto que nunca é devolvido em claro: a tela mostra só
	// que existe e só permite substituir.
	TipoSegredo Tipo = "segredo"
)

// Aplicacao diz quando uma alteração passa a valer.
type Aplicacao string

const (
	// AplicaImediato: vale na próxima leitura (no máximo TTLPadrao depois nas
	// outras instâncias).
	AplicaImediato Aplicacao = "imediato"
	// AplicaReinicio: o valor é lido uma única vez no arranque; só vale no
	// próximo reinício do servidor.
	AplicaReinicio Aplicacao = "reinicio"
)

// TamanhoMaximoValor limita o tamanho de qualquer valor gravado.
const TamanhoMaximoValor = 2000

// Definicao descreve uma configuração que pode ser gravada no banco.
type Definicao struct {
	Chave      string
	Grupo      string
	Descricao  string
	Tipo       Tipo
	Aplicacao  Aplicacao
	Observacao string // nuance que a tela mostra junto da chave
	Exemplo    string
	Inteiro    bool     // TipoNumero: só inteiros
	Min        *float64 // TipoNumero
	Max        *float64 // TipoNumero
	Opcoes     []string // TipoTexto com conjunto fechado de valores
	validador  func(string) (string, error)
}

// Segredo diz se o valor nunca pode aparecer em claro.
func (d Definicao) Segredo() bool { return d.Tipo == TipoSegredo }

func num(v float64) *float64 { return &v }

// definicoes é a lista aprovada (Parte 3 do plano). Para liberar mais uma
// chave basta acrescentar aqui e ler o valor com sysconfig.String; o resto
// (API, auditoria, cofre, tela) é genérico.
var definicoes = []Definicao{
	{
		Chave: "ALLOWED_ORIGINS", Grupo: "Segurança", Tipo: TipoTexto, Aplicacao: AplicaImediato,
		Descricao:  "Origens (sites) autorizadas a chamar a API pelo navegador (CORS), separadas por vírgula.",
		Exemplo:    "https://app.exemplo.ao,https://painel.exemplo.ao",
		Observacao: "Sem nenhuma origem definida, qualquer origem é aceita.",
		validador:  validarOrigens,
	},
	{
		Chave: "JWT_EXPIRY_HOURS", Grupo: "Segurança", Tipo: TipoNumero, Aplicacao: AplicaImediato,
		Descricao: "Duração, em horas, do token de login. Padrão: 24.",
		Inteiro:   true, Min: num(1), Max: num(720), Exemplo: "24",
		Observacao: "Vale só para logins feitos depois da alteração; sessões já abertas mantêm a validade com que foram emitidas.",
	},
	{
		Chave: "LOG_LEVEL", Grupo: "Segurança", Tipo: TipoTexto, Aplicacao: AplicaImediato,
		Descricao: "Nível de log. Com \"debug\" o servidor grava diagnósticos detalhados (podem conter dados de estudantes); mantenha em \"info\" em produção.",
		Opcoes:    []string{"info", "debug"}, Exemplo: "info",
	},
	{
		Chave: "EMAIL_FROM", Grupo: "E-mail (Brevo)", Tipo: TipoTexto, Aplicacao: AplicaImediato,
		Descricao:  "Endereço remetente dos e-mails do sistema.",
		Exemplo:    "nao-responder@exemplo.ao",
		Observacao: "Precisa estar verificado em app.brevo.com/settings/senders, senão o Brevo recusa o envio.",
		validador:  validarEmail,
	},
	{
		Chave: "BREVO_API_KEY", Grupo: "E-mail (Brevo)", Tipo: TipoSegredo, Aplicacao: AplicaImediato,
		Descricao: "Chave da API do Brevo usada para enviar e-mails.",
	},
	{
		Chave: "ZIETT_API_KEY", Grupo: "SMS (Ziett)", Tipo: TipoSegredo, Aplicacao: AplicaImediato,
		Descricao:  "Chave da API da Ziett usada no teste de envio de SMS do painel.",
		Observacao: "Os remetentes de comunicação de cada academia guardam as próprias chaves e não passam por aqui.",
	},
	{
		Chave: "APPYPAY_RESOURCE", Grupo: "Pagamentos (AppyPay)", Tipo: TipoTexto, Aplicacao: AplicaImediato,
		Descricao: "Identificador (UUID) da API da AppyPay exigido no pedido de token OAuth2.",
		Exemplo:   "00000000-0000-0000-0000-000000000000",
		validador: validarUUID,
	},
	{
		Chave: "RECONCILIACAO_ATIVA", Grupo: "Reconciliação de pagamentos", Tipo: TipoBooleano, Aplicacao: AplicaReinicio,
		Descricao: "Liga ou desliga o job que reconcilia cobranças pendentes com a AppyPay. Padrão: ligado.",
	},
	{
		Chave: "RECONCILIACAO_CONCORRENCIA", Grupo: "Reconciliação de pagamentos", Tipo: TipoNumero, Aplicacao: AplicaReinicio,
		Descricao: "Quantas cobranças são consultadas em paralelo, por provedor. Padrão: 3.",
		Inteiro:   true, Min: num(1), Max: num(20), Exemplo: "3",
	},
	{
		Chave: "RECONCILIACAO_CHAMADAS_POR_SEGUNDO", Grupo: "Reconciliação de pagamentos", Tipo: TipoNumero, Aplicacao: AplicaReinicio,
		Descricao: "Limite de chamadas por segundo ao provedor. Padrão: 2.",
		Min:       num(0.1), Max: num(50), Exemplo: "2",
	},
	{
		Chave: "RECONCILIACAO_CACHE_CREDENCIAIS_SEG", Grupo: "Reconciliação de pagamentos", Tipo: TipoNumero, Aplicacao: AplicaReinicio,
		Descricao: "Por quantos segundos as credenciais de provedor ficam em cache no job. Padrão: 21600 (6 horas).",
		Inteiro:   true, Min: num(1), Max: num(604800), Exemplo: "21600",
	},
	{
		Chave: "RECONCILIACAO_RESYNC_MIN", Grupo: "Reconciliação de pagamentos", Tipo: TipoNumero, Aplicacao: AplicaReinicio,
		Descricao: "Intervalo, em minutos, da ressincronização completa. Padrão: 1440 (diário). 0 = só no arranque.",
		Inteiro:   true, Min: num(0), Max: num(10080), Exemplo: "1440",
	},
	{
		Chave: "MEGA_EMAIL", Grupo: "Armazenamento de arquivos (Mega)", Tipo: TipoSegredo, Aplicacao: AplicaReinicio,
		Descricao:  "E-mail da conta Mega onde ficam os documentos.",
		Observacao: "O login no Mega é feito uma vez, no arranque do servidor.",
	},
	{
		Chave: "MEGA_PASSWORD", Grupo: "Armazenamento de arquivos (Mega)", Tipo: TipoSegredo, Aplicacao: AplicaReinicio,
		Descricao:  "Senha da conta Mega onde ficam os documentos.",
		Observacao: "O login no Mega é feito uma vez, no arranque do servidor.",
	},
	{
		Chave: "MEGA_ROOT_FOLDER", Grupo: "Armazenamento de arquivos (Mega)", Tipo: TipoTexto, Aplicacao: AplicaReinicio,
		Descricao: "Pasta raiz, dentro da conta Mega, sob a qual os arquivos são guardados.",
		Exemplo:   "spuri-producao", validador: validarPasta,
	},
}

// Definicoes devolve a lista aprovada, na ordem em que a tela a mostra.
func Definicoes() []Definicao {
	out := make([]Definicao, len(definicoes))
	copy(out, definicoes)
	return out
}

// Buscar devolve a definição de uma chave aprovada.
func Buscar(chave string) (Definicao, bool) {
	for _, d := range definicoes {
		if d.Chave == chave {
			return d, true
		}
	}
	return Definicao{}, false
}

// ErroValidacao é devolvido quando o valor recebido não serve para a chave.
type ErroValidacao struct{ Msg string }

func (e *ErroValidacao) Error() string { return e.Msg }

func invalido(format string, a ...any) error {
	return &ErroValidacao{Msg: fmt.Sprintf(format, a...)}
}

// Validar confere o valor contra a definição e devolve a forma normalizada que
// será gravada (sem espaços nas pontas, booleanos como "true"/"false",
// origens sem barra final).
func (d Definicao) Validar(valor string) (string, error) {
	v := strings.TrimSpace(valor)
	if v == "" {
		return "", invalido("%s: o valor não pode ser vazio; para voltar ao valor do ambiente, remova a configuração", d.Chave)
	}
	if len(v) > TamanhoMaximoValor {
		return "", invalido("%s: valor demasiado longo (máximo %d caracteres)", d.Chave, TamanhoMaximoValor)
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return "", invalido("%s: o valor não pode conter quebras de linha nem caracteres de controle", d.Chave)
		}
	}
	switch d.Tipo {
	case TipoBooleano:
		switch strings.ToLower(v) {
		case "true", "1", "sim", "yes":
			return "true", nil
		case "false", "0", "nao", "não", "no":
			return "false", nil
		}
		return "", invalido("%s: use true ou false", d.Chave)
	case TipoNumero:
		var n float64
		if d.Inteiro {
			i, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return "", invalido("%s: deve ser um número inteiro", d.Chave)
			}
			n, v = float64(i), strconv.FormatInt(i, 10)
		} else {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return "", invalido("%s: deve ser um número", d.Chave)
			}
			n, v = f, strconv.FormatFloat(f, 'f', -1, 64)
		}
		if d.Min != nil && n < *d.Min {
			return "", invalido("%s: o mínimo é %s", d.Chave, strconv.FormatFloat(*d.Min, 'f', -1, 64))
		}
		if d.Max != nil && n > *d.Max {
			return "", invalido("%s: o máximo é %s", d.Chave, strconv.FormatFloat(*d.Max, 'f', -1, 64))
		}
		return v, nil
	}
	if len(d.Opcoes) > 0 {
		lower := strings.ToLower(v)
		for _, o := range d.Opcoes {
			if lower == o {
				return o, nil
			}
		}
		return "", invalido("%s: use um destes valores: %s", d.Chave, strings.Join(d.Opcoes, ", "))
	}
	if d.validador != nil {
		return d.validador(v)
	}
	return v, nil
}

func validarOrigens(v string) (string, error) {
	var out []string
	for _, parte := range strings.Split(v, ",") {
		o := strings.TrimSpace(parte)
		if o == "" {
			continue
		}
		u, err := url.Parse(o)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
			(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return "", invalido("ALLOWED_ORIGINS: %q não é uma origem válida (use http://host ou https://host[:porta], sem caminho)", o)
		}
		out = append(out, u.Scheme+"://"+u.Host)
	}
	if len(out) == 0 {
		return "", invalido("ALLOWED_ORIGINS: informe ao menos uma origem")
	}
	if len(out) > 20 {
		return "", invalido("ALLOWED_ORIGINS: no máximo 20 origens")
	}
	return strings.Join(out, ","), nil
}

func validarEmail(v string) (string, error) {
	a, err := mail.ParseAddress(v)
	if err != nil || a.Name != "" || a.Address != v {
		return "", invalido("EMAIL_FROM: informe só um endereço de e-mail válido, sem nome")
	}
	return v, nil
}

func validarUUID(v string) (string, error) {
	id, err := uuid.Parse(v)
	if err != nil {
		return "", invalido("APPYPAY_RESOURCE: deve ser um UUID")
	}
	return id.String(), nil
}

func validarPasta(v string) (string, error) {
	for _, parte := range strings.Split(strings.Trim(v, "/"), "/") {
		if parte == ".." || parte == "." {
			return "", invalido("MEGA_ROOT_FOLDER: caminho inválido")
		}
	}
	return v, nil
}

// ErrNaoAprovada é devolvido quando se tenta gravar uma chave fora da lista.
var ErrNaoAprovada = errors.New("configuração não está na lista de configurações editáveis")
