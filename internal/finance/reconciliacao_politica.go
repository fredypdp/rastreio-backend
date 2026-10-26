package finance

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Reconciliação de cobranças pendentes — política de reverificação.
//
// A reconciliação é a rede de segurança dos webhooks: de tempos em tempos o
// Spuri pergunta ao provedor "como está esta cobrança agora?" para as que
// continuam pendentes. Este arquivo decide QUANDO perguntar e QUANDO desistir.
// São funções puras (sem relógio, sem banco, sem rede), por provedor e método,
// para que o comportamento seja previsível e testável.
//
// Princípios:
//   - quanto mais velha a cobrança, mais espaçadas as consultas (backoff por
//     faixas de idade);
//   - existe uma idade máxima por método: depois dela a cobrança sai da
//     reconciliação automática (continua consultável manualmente) — evita
//     consultar para sempre referências que nunca serão pagas;
//   - cada provedor pode declarar a sua própria política (interface opcional
//     ProvedorReconciliavel); sem declarar, vale a política padrão do método.

// FaixaReconciliacao liga uma faixa de idade da cobrança ao intervalo entre
// consultas dentro dela.
type FaixaReconciliacao struct {
	// IdadeAte é o limite superior (inclusive) da idade da cobrança nesta faixa.
	IdadeAte time.Duration
	// Intervalo é o tempo entre duas consultas enquanto a cobrança está na faixa.
	Intervalo time.Duration
}

// PoliticaReconciliacao é a política de reverificação de um método de um
// provedor. As faixas devem estar em ordem crescente de IdadeAte.
type PoliticaReconciliacao struct {
	Faixas []FaixaReconciliacao
	// IdadeMaxima é a idade a partir da qual a cobrança deixa de ser
	// reconciliada automaticamente. Zero (ou negativo) desliga a
	// reconciliação deste método.
	IdadeMaxima time.Duration
}

// ProvedorReconciliavel é a extensão OPCIONAL de Provedor para declarar uma
// política própria de reconciliação por método canônico. Provedores que não a
// implementam (ou que devolvem ok=false para um método) usam a política padrão.
type ProvedorReconciliavel interface {
	PoliticaReconciliacao(metodo string) (PoliticaReconciliacao, bool)
}

// ativa informa se a política reconcilia alguma coisa.
func (p PoliticaReconciliacao) ativa() bool {
	return p.IdadeMaxima > 0 && len(p.Faixas) > 0
}

// valida garante que a política é coerente (faixas crescentes, intervalos
// positivos). Uma política inválida nunca é usada: cai na padrão.
func (p PoliticaReconciliacao) valida() bool {
	if !p.ativa() {
		return false
	}
	var anterior time.Duration
	for _, f := range p.Faixas {
		if f.Intervalo <= 0 || f.IdadeAte <= anterior {
			return false
		}
		anterior = f.IdadeAte
	}
	return true
}

// IntervaloPara devolve o intervalo entre consultas para uma cobrança com a
// idade dada. ok=false quando a idade já excede a máxima (ou a política está
// desligada). Passada a última faixa mas dentro da idade máxima, vale o
// intervalo da última faixa.
func (p PoliticaReconciliacao) IntervaloPara(idade time.Duration) (time.Duration, bool) {
	if !p.ativa() || idade > p.IdadeMaxima {
		return 0, false
	}
	if idade < 0 {
		idade = 0
	}
	for _, f := range p.Faixas {
		if idade <= f.IdadeAte {
			return f.Intervalo, true
		}
	}
	return p.Faixas[len(p.Faixas)-1].Intervalo, true
}

// ProximaVerificacao devolve o instante da próxima consulta de uma cobrança
// emitida em emitidaEm, dado que a verificação corrente acontece em base.
// A última consulta é sempre feita exatamente na idade máxima (nunca depois);
// ok=false quando não há mais nada a agendar.
func (p PoliticaReconciliacao) ProximaVerificacao(emitidaEm, base time.Time) (time.Time, bool) {
	intervalo, ok := p.IntervaloPara(base.Sub(emitidaEm))
	if !ok {
		return time.Time{}, false
	}
	proxima := base.Add(intervalo)
	if limite := emitidaEm.Add(p.IdadeMaxima); proxima.After(limite) {
		proxima = limite
	}
	if !proxima.After(base) {
		return time.Time{}, false
	}
	return proxima, true
}

// politicaPadrao é a política de um método canônico quando o provedor não
// declara a sua. Os valores priorizam NÃO PERDER um pagamento tardio: a idade
// máxima é generosa porque a validade real das referências não consta da
// documentação da AppyPay e porque uma consulta que continua pendente não custa
// nada ao banco (só uma chamada ao provedor); em compensação as consultas se
// espaçam muito com a idade. O webhook continua sendo o caminho principal; isto
// é a rede de segurança.
//
//   - GPO (pagamento por push no app do pagador): resolve em minutos, então
//     consulta cedo e desiste em 24 h.
//   - GPO_QR (QR Code): consulta de hora em hora no começo e espaça até
//     desistir em 30 dias.
//   - REF (referência pagável em caixa/multibanco) e qualquer outro método: o
//     pagador pode demorar dias; consulta espaçado e desiste em 30 dias.
func politicaPadrao(metodo string) PoliticaReconciliacao {
	const (
		min = time.Minute
		h   = time.Hour
		dia = 24 * time.Hour
	)
	switch strings.ToUpper(strings.TrimSpace(metodo)) {
	case MetodoGPO:
		return PoliticaReconciliacao{
			Faixas:      []FaixaReconciliacao{{IdadeAte: 15 * min, Intervalo: 2 * min}, {IdadeAte: 2 * h, Intervalo: 10 * min}, {IdadeAte: 24 * h, Intervalo: h}},
			IdadeMaxima: 24 * h,
		}
	case MetodoGPOQR:
		return PoliticaReconciliacao{
			Faixas:      []FaixaReconciliacao{{IdadeAte: h, Intervalo: 5 * min}, {IdadeAte: 12 * h, Intervalo: 30 * min}, {IdadeAte: 3 * dia, Intervalo: 2 * h}, {IdadeAte: 30 * dia, Intervalo: 12 * h}},
			IdadeMaxima: 30 * dia,
		}
	default:
		return PoliticaReconciliacao{
			Faixas:      []FaixaReconciliacao{{IdadeAte: h, Intervalo: 10 * min}, {IdadeAte: 6 * h, Intervalo: 30 * min}, {IdadeAte: 2 * dia, Intervalo: 2 * h}, {IdadeAte: 7 * dia, Intervalo: 6 * h}, {IdadeAte: 30 * dia, Intervalo: dia}},
			IdadeMaxima: 30 * dia,
		}
	}
}

// politicaDaCobranca resolve a política de um método de um provedor: a do
// provedor, se ele a declara e ela é válida; senão a padrão do método.
func politicaDaCobranca(p Provedor, metodo string) PoliticaReconciliacao {
	if r, ok := p.(ProvedorReconciliavel); ok {
		if pol, ok := r.PoliticaReconciliacao(metodo); ok && pol.valida() {
			return pol
		}
	}
	return politicaPadrao(metodo)
}

// ConfigReconciliacao reúne os parâmetros de execução do job. Os padrões são
// pensados para um banco serverless cobrado por tempo de computação (Neon):
// o job não acorda o banco enquanto nada muda de estado.
type ConfigReconciliacao struct {
	// Ativa liga o job. Em implantações com várias instâncias, ligue em UMA só
	// (ou aceite chamadas duplicadas ao provedor: a aplicação é idempotente).
	Ativa bool
	// Concorrencia é o máximo de consultas simultâneas por provedor.
	Concorrencia int
	// ChamadasPorSegundo limita a taxa de consultas por provedor.
	ChamadasPorSegundo float64
	// TTLCredenciais é por quanto tempo as credenciais decifradas ficam em
	// memória; é o que evita ler o banco a cada consulta (por isso precisa ser
	// bem maior que o intervalo entre consultas, que chega a horas). Credenciais
	// editadas ou removidas nesta instância invalidam o cache na hora, e um erro
	// do provedor descarta a entrada (a próxima consulta relê o cofre).
	TTLCredenciais time.Duration
	// Resync recarrega as pendentes do banco nesse intervalo: rede de segurança
	// contra desvio do registro (cobranças criadas por outra instância, falha ao
	// observar um evento). O padrão é uma vez por dia (uma consulta); zero
	// desliga (só no arranque).
	Resync time.Duration
	// TimeoutConsulta limita cada chamada ao provedor.
	TimeoutConsulta time.Duration
}

// ConfigReconciliacaoPadrao devolve os padrões.
func ConfigReconciliacaoPadrao() ConfigReconciliacao {
	return ConfigReconciliacao{
		Ativa:              true,
		Concorrencia:       3,
		ChamadasPorSegundo: 2,
		TTLCredenciais:     6 * time.Hour,
		Resync:             24 * time.Hour,
		TimeoutConsulta:    20 * time.Second,
	}
}

// ConfigReconciliacaoDoAmbiente lê os ajustes opcionais do ambiente:
//
//	RECONCILIACAO_ATIVA                  (padrão true; "false"/"0" desliga)
//	RECONCILIACAO_CONCORRENCIA           (padrão 3, por provedor)
//	RECONCILIACAO_CHAMADAS_POR_SEGUNDO   (padrão 2, por provedor)
//	RECONCILIACAO_CACHE_CREDENCIAIS_SEG  (padrão 21600 = 6 h)
//	RECONCILIACAO_RESYNC_MIN             (padrão 1440 = diário; 0 = só no arranque)
func ConfigReconciliacaoDoAmbiente() ConfigReconciliacao {
	cfg := ConfigReconciliacaoPadrao()
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("RECONCILIACAO_ATIVA"))); v == "false" || v == "0" || v == "no" || v == "nao" {
		cfg.Ativa = false
	}
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("RECONCILIACAO_CONCORRENCIA"))); err == nil && n > 0 {
		cfg.Concorrencia = n
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv("RECONCILIACAO_CHAMADAS_POR_SEGUNDO")), 64); err == nil && f > 0 {
		cfg.ChamadasPorSegundo = f
	}
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("RECONCILIACAO_CACHE_CREDENCIAIS_SEG"))); err == nil && n > 0 {
		cfg.TTLCredenciais = time.Duration(n) * time.Second
	}
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("RECONCILIACAO_RESYNC_MIN"))); err == nil && n >= 0 {
		cfg.Resync = time.Duration(n) * time.Minute
	}
	return cfg
}
