package finance

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"spuri/internal/domain/aggregates"

	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

// Job de reconciliação de cobranças pendentes.
//
// O QUE FAZ. De tempos em tempos pergunta ao provedor "como está esta
// cobrança agora?" para cada cobrança ainda pendente e, se ela mudou de estado
// (paga, falhada, cancelada, expirada), aplica a resposta pelo MESMO caminho
// da consulta manual (aplicarConsulta) e dispara os MESMOS efeitos do webhook
// (propina paga, serviço extra, liberação de snapshots).
// É a rede de segurança de webhooks perdidos, atrasados ou de servidor fora do
// ar, e a base para provedores sem webhook.
//
// COMO NÃO PESA NO BANCO (NeonDB cobra por tempo de computação ativo):
//   - as pendentes vivem num registro em memória (registroPendentes), carregado
//     por UMA consulta no arranque e mantido pelos eventos que Service.record já
//     grava; não há polling de tabela;
//   - o laço dorme sem timer enquanto não há pendentes e, havendo, dorme até a
//     próxima data de verificação (nunca em intervalo fixo);
//   - uma verificação que conclui "continua pendente" NÃO lê nem escreve no
//     banco: usa dados do registro e credenciais decifradas em cache (TTL);
//   - só uma mudança de estado toca o banco (lê a cobrança e grava o evento).
//
// SEGURANÇA. Um erro do provedor nunca muda o estado da cobrança. Há limite de
// taxa e de concorrência por provedor, disjuntor após erros seguidos, recuo
// exponencial por cobrança e idade máxima por método (política). Sem trava no
// banco: a aplicação é idempotente, então duas instâncias só duplicam
// consultas; em implantações com várias instâncias ligue o job em uma só
// (RECONCILIACAO_ATIVA=false nas demais).

// CobrancaConfirmada identifica uma cobrança que acabou de ser confirmada como
// paga, para os efeitos de negócio que vivem fora deste pacote.
type CobrancaConfirmada struct {
	ID                    uuid.UUID
	ProviderChargeID      string
	MerchantTransactionID string
	Provedor              string
}

// EfeitosPagamento aplica os efeitos de negócio de uma cobrança paga que não
// vivem no pacote finance (vínculo de serviço extra).
// Deve ser idempotente: é repetida até dar certo. Injetada por quem conhece
// esses módulos (ver handlers.NovosEfeitosPagamento).
type EfeitosPagamento func(ctx context.Context, c CobrancaConfirmada) error

const (
	reconciliacaoAtorID   = "sistema:reconciliacao"
	reconciliacaoAtorTipo = "sistema"
	reconciliacaoIP       = "reconciliacao"
	// falhasParaAbrirDisjuntor é quantos erros seguidos de um provedor o
	// disjuntor tolera antes de pausar as consultas a ele.
	falhasParaAbrirDisjuntor = 5
	maxVencidosPorCiclo      = 50
)

// limitadorProvedor concentra os limites de consulta de UM provedor: taxa,
// concorrência e disjuntor.
type limitadorProvedor struct {
	taxa *rate.Limiter
	sem  chan struct{}

	mu             sync.Mutex
	falhasSeguidas int
	abertoAte      time.Time
}

func novoLimitadorProvedor(cfg ConfigReconciliacao) *limitadorProvedor {
	conc := cfg.Concorrencia
	if conc <= 0 {
		conc = 1
	}
	cps := cfg.ChamadasPorSegundo
	if cps <= 0 {
		cps = 1
	}
	burst := int(cps) + 1
	return &limitadorProvedor{taxa: rate.NewLimiter(rate.Limit(cps), burst), sem: make(chan struct{}, conc)}
}

// esperaDisjuntor devolve quanto falta para o disjuntor fechar (0 = fechado).
func (l *limitadorProvedor) esperaDisjuntor(agora time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if agora.Before(l.abertoAte) {
		return l.abertoAte.Sub(agora)
	}
	return 0
}

func (l *limitadorProvedor) registrarSucesso() {
	l.mu.Lock()
	l.falhasSeguidas, l.abertoAte = 0, time.Time{}
	l.mu.Unlock()
}

func (l *limitadorProvedor) registrarFalha(agora time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.falhasSeguidas++
	if l.falhasSeguidas >= falhasParaAbrirDisjuntor {
		pausa := time.Minute << uint(minInt(l.falhasSeguidas-falhasParaAbrirDisjuntor, 4))
		if pausa > 15*time.Minute {
			pausa = 15 * time.Minute
		}
		l.abertoAte = agora.Add(pausa)
	}
}

// adquirir reserva uma vaga de concorrência e um token de taxa; devolve a
// função que libera a vaga.
func (l *limitadorProvedor) adquirir(ctx context.Context) (func(), error) {
	select {
	case l.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := l.taxa.Wait(ctx); err != nil {
		<-l.sem
		return nil, err
	}
	return func() { <-l.sem }, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// cacheCredenciais mantém em memória, por TTL, as credenciais decifradas das
// cobranças pendentes: é o que evita ler (e decifrar) o cofre a cada consulta.
// Os tokens OAuth dos provedores já vivem em memória pelo mesmo motivo.
type cacheCredenciais struct {
	mu    sync.Mutex
	itens map[string]entradaCredencial
}

type entradaCredencial struct {
	cred   credencialProvedor
	expira time.Time
}

func novoCacheCredenciais() *cacheCredenciais {
	return &cacheCredenciais{itens: make(map[string]entradaCredencial)}
}

func (c *cacheCredenciais) obter(chave string, agora time.Time) (credencialProvedor, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.itens[chave]
	if !ok || !agora.Before(e.expira) {
		delete(c.itens, chave)
		return credencialProvedor{}, false
	}
	return e.cred, true
}

func (c *cacheCredenciais) guardar(chave string, cred credencialProvedor, expira time.Time) {
	c.mu.Lock()
	c.itens[chave] = entradaCredencial{cred: cred, expira: expira}
	c.mu.Unlock()
}

func (c *cacheCredenciais) remover(chave string) {
	c.mu.Lock()
	delete(c.itens, chave)
	c.mu.Unlock()
}

func (c *cacheCredenciais) limpar() {
	c.mu.Lock()
	c.itens = make(map[string]entradaCredencial)
	c.mu.Unlock()
}

// reconciliador é o job. Vive dentro do Service (um por Service).
type reconciliador struct {
	s     *Service
	reg   *registroPendentes
	creds *cacheCredenciais
	ativo atomic.Bool

	mu          sync.Mutex
	cfg         ConfigReconciliacao
	efeitos     EfeitosPagamento
	limitadores map[string]*limitadorProvedor

	// agora e jitter são substituíveis em teste.
	agora  func() time.Time
	jitter func(time.Duration) time.Duration
}

func novoReconciliador(s *Service) *reconciliador {
	return &reconciliador{
		s:           s,
		reg:         novoRegistroPendentes(),
		creds:       novoCacheCredenciais(),
		cfg:         ConfigReconciliacaoPadrao(),
		limitadores: make(map[string]*limitadorProvedor),
		agora:       time.Now,
		jitter:      jitterAte10Porcento,
	}
}

// jitterAte10Porcento espalha as consultas para não baterem todas no mesmo
// instante (ex.: muitas cobranças criadas juntas).
func jitterAte10Porcento(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(d)/10 + 1))
}

func (r *reconciliador) config() ConfigReconciliacao {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg
}

func (r *reconciliador) limitador(provedor string) *limitadorProvedor {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.limitadores[provedor]
	if !ok {
		l = novoLimitadorProvedor(r.cfg)
		r.limitadores[provedor] = l
	}
	return l
}

// credenciaisAlteradas descarta o cache de credenciais (chamado quando as
// credenciais são editadas, removidas ou descartadas nesta instância).
func (r *reconciliador) credenciaisAlteradas() {
	if r != nil {
		r.creds.limpar()
	}
}

// SetEfeitosPagamento injeta os efeitos de negócio pós-pagamento que o job
// dispara quando confirma uma cobrança (ver EfeitosPagamento).
func (s *Service) SetEfeitosPagamento(fn EfeitosPagamento) {
	r := s.reconc
	r.mu.Lock()
	r.efeitos = fn
	r.mu.Unlock()
}

// ReconciliacaoPendentes devolve quantas cobranças o registro acompanha
// (observabilidade e testes).
func (s *Service) ReconciliacaoPendentes() int { return s.reconc.reg.Len() }

// IniciarReconciliacao carrega as pendentes (uma consulta) e inicia o laço em
// segundo plano; ele termina quando ctx é cancelado. Sem client (testes
// unitários) ou com cfg.Ativa=false não faz nada.
func (s *Service) IniciarReconciliacao(ctx context.Context, cfg ConfigReconciliacao) error {
	if s.client == nil || !cfg.Ativa {
		return nil
	}
	r := s.reconc
	r.mu.Lock()
	r.cfg = cfg
	r.limitadores = make(map[string]*limitadorProvedor)
	r.mu.Unlock()
	// Ativa antes de carregar: eventos que chegarem durante o carregamento
	// também entram no registro (Adicionar não duplica).
	r.ativo.Store(true)
	if err := r.carregar(ctx); err != nil {
		r.ativo.Store(false)
		return err
	}
	go r.executar(ctx)
	return nil
}

// observarReconciliacao mantém o registro em dia a partir dos eventos que o
// Service grava: entram as cobranças recém-criadas e saem as que chegaram a um
// estado terminal. Sem o job ativo é um no-op.
func (s *Service) observarReconciliacao(event string, payload map[string]any, id uuid.UUID) {
	if s == nil || s.reconc == nil || !s.reconc.ativo.Load() {
		return
	}
	r := s.reconc
	status := normalizeChargeStatus(stringDe(payload["status"]))
	switch event {
	case aggregates.CobrancaCriada, aggregates.QRCodeGerado:
		if isTerminalChargeStatus(status) {
			return
		}
		p, ok := pendenteDoPayload(id, event, payload, r.agora())
		if !ok {
			return
		}
		prov, err := s.ProvedorPorID(p.Provedor)
		if err != nil {
			return
		}
		proxima, ok := politicaDaCobranca(prov, p.Metodo).ProximaVerificacao(p.EmitidaEm, p.EmitidaEm)
		if !ok {
			return
		}
		p.Proxima = proxima.Add(r.jitter(proxima.Sub(p.EmitidaEm)))
		r.reg.Adicionar(p)
	case aggregates.CobrancaConsultada, aggregates.CobrancaCancelada, aggregates.CobrancaFalhou,
		aggregates.CobrancaPagamentoExternoRegistrado, aggregates.QRCodeFalhou, aggregates.CobrancaConflitoPosCancelamento:
		if isTerminalChargeStatus(status) || event == aggregates.CobrancaConflitoPosCancelamento {
			// Se o próprio job está processando a cobrança, é ele quem decide
			// (ex.: repetir efeitos que falharam); senão ela já foi resolvida
			// por outro caminho (webhook, consulta manual, cancelamento).
			r.reg.RemoverSeLivre(id)
		}
	}
}

func stringDe(v any) string {
	s, _ := v.(string)
	return s
}

// pendenteDoPayload extrai de um evento de criação o que o job precisa. Só
// cobranças reais de um provedor, com id do provedor, entram.
func pendenteDoPayload(id uuid.UUID, event string, payload map[string]any, agora time.Time) (pendenteReconciliacao, bool) {
	provedor := stringDe(payload["provedor"])
	if provedor == "" {
		provedor = ProvedorAppyPay
	}
	providerID := stringDe(payload["provider_charge_id"])
	if provedor == ProvedorExterno || providerID == "" {
		return pendenteReconciliacao{}, false
	}
	metodo := strings.ToUpper(stringDe(payload["payment_method"]))
	if event == aggregates.QRCodeGerado || stringDe(payload["qr_code_type"]) != "" {
		metodo = MetodoGPOQR
	}
	cred, _ := uuid.Parse(stringDe(payload["credencial_id"]))
	return pendenteReconciliacao{
		ID: id, Provedor: provedor, Metodo: metodo,
		Contexto: stringDe(payload["contexto_tipo"]), Academia: stringDe(payload["codigo_academia"]),
		ProviderID: providerID, Merchant: stringDe(payload["merchant_transaction_id"]),
		CredencialID: cred, EmitidaEm: agora,
	}, true
}

// carregar lê do banco, numa única consulta, as cobranças pendentes de
// provedores reais e as coloca no registro. As que já passaram da idade máxima
// da política ficam de fora (continuam consultáveis manualmente).
func (r *reconciliador) carregar(ctx context.Context) error {
	rows, err := r.s.client.DB().QueryContext(ctx, `SELECT id, COALESCE(provider_charge_id,''), merchant_transaction_id, contexto_tipo,
		COALESCE(codigo_academia,''), provedor, credencial_id,
		CASE WHEN payload->>'qr_code_type' IS NOT NULL THEN 'GPO_QR' ELSE upper(COALESCE(payload->>'payment_method','')) END,
		COALESCE(emitida_em, updated_at)
		FROM financeiro_cobrancas
		WHERE provedor <> $1 AND provider_charge_id IS NOT NULL
		AND lower(COALESCE(payload->>'status','')) NOT IN (`+chargeAbertaStatusExcluidos+`)`, ProvedorExterno)
	if err != nil {
		return err
	}
	defer rows.Close()
	agora := r.agora()
	carregadas, foraDaJanela := 0, 0
	for rows.Next() {
		var p pendenteReconciliacao
		var cred uuid.NullUUID
		if err := rows.Scan(&p.ID, &p.ProviderID, &p.Merchant, &p.Contexto, &p.Academia, &p.Provedor, &cred, &p.Metodo, &p.EmitidaEm); err != nil {
			return err
		}
		if cred.Valid {
			p.CredencialID = cred.UUID
		}
		prov, err := r.s.ProvedorPorID(p.Provedor)
		if err != nil {
			continue
		}
		if _, ok := politicaDaCobranca(prov, p.Metodo).ProximaVerificacao(p.EmitidaEm, agora); !ok {
			foraDaJanela++
			continue
		}
		// Depois de um arranque os webhooks podem ter sido perdidos enquanto o
		// servidor estava fora: a primeira verificação vem logo, espalhada em
		// ~1 minuto para não gerar uma rajada.
		p.Proxima = agora.Add(10*time.Second + time.Duration(rand.Int63n(int64(50*time.Second))))
		r.reg.Adicionar(p)
		carregadas++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	log.Printf("[RECONCILIACAO] %d cobrança(s) pendente(s) carregada(s); %d fora da janela de reconciliação", carregadas, foraDaJanela)
	return nil
}

// executar é o laço do job. Nunca consulta o banco para saber o que fazer:
// dorme até a próxima data do registro (ou sem timer se vazio) e acorda por
// aviso quando entra uma cobrança nova.
func (r *reconciliador) executar(ctx context.Context) {
	var resync <-chan time.Time
	if d := r.config().Resync; d > 0 {
		t := time.NewTicker(d)
		defer t.Stop()
		resync = t.C
	}
	for {
		if ctx.Err() != nil {
			return
		}
		r.processarVencidos(ctx)

		var timer <-chan time.Time
		var temporizador *time.Timer
		if proxima, ok := r.reg.ProximaData(); ok {
			espera := proxima.Sub(r.agora())
			if espera < 50*time.Millisecond {
				espera = 50 * time.Millisecond
			}
			temporizador = time.NewTimer(espera)
			timer = temporizador.C
		}
		select {
		case <-ctx.Done():
			if temporizador != nil {
				temporizador.Stop()
			}
			return
		case <-r.reg.acordar:
		case <-timer:
		case <-resync:
			if err := r.carregar(ctx); err != nil {
				log.Printf("[RECONCILIACAO] resync falhou: %v", err)
			}
		}
		if temporizador != nil {
			temporizador.Stop()
		}
	}
}

// processarVencidos verifica, em paralelo (limitado por provedor), todas as
// cobranças cuja data chegou, e devolve quantas processou.
func (r *reconciliador) processarVencidos(ctx context.Context) int {
	total := 0
	for {
		vencidos := r.reg.Vencidos(r.agora(), maxVencidosPorCiclo)
		if len(vencidos) == 0 {
			return total
		}
		var wg sync.WaitGroup
		for _, p := range vencidos {
			wg.Add(1)
			go func(p pendenteReconciliacao) {
				defer wg.Done()
				r.processar(ctx, p)
			}(p)
		}
		wg.Wait()
		total += len(vencidos)
		if ctx.Err() != nil {
			return total
		}
	}
}

// resultadoVerificacao é o desfecho de verificar uma cobrança.
type resultadoVerificacao struct {
	// Descartar tira a cobrança do registro sem mais nada a fazer.
	Descartar bool
	// Resolvida: chegou a um estado terminal (sai do registro, a menos que
	// haja efeitos pendentes).
	Resolvida bool
	// EfeitoPendente: gravada como paga, mas os efeitos de negócio falharam.
	EfeitoPendente bool
	// Adiar força a próxima verificação para daqui a esta duração (disjuntor).
	Adiar time.Duration
	Erro  error
}

func (r *reconciliador) processar(ctx context.Context, p pendenteReconciliacao) {
	res := r.verificar(ctx, p)
	agora := r.agora()
	var pol PoliticaReconciliacao
	if prov, err := r.s.ProvedorPorID(p.Provedor); err == nil {
		pol = politicaDaCobranca(prov, p.Metodo)
	}
	if res.Erro != nil && !errors.Is(res.Erro, context.Canceled) {
		log.Printf("[RECONCILIACAO] cobrança %s (%s): %v", p.ID, p.Provedor, res.Erro)
	}
	r.reg.Concluir(p.ID, func(atual *pendenteReconciliacao) bool {
		if res.Descartar || (res.Resolvida && !res.EfeitoPendente) {
			return false
		}
		atual.EfeitoPendente = res.EfeitoPendente
		if res.Erro != nil {
			atual.Falhas++
		} else {
			atual.Falhas = 0
		}
		if res.Adiar > 0 {
			atual.Proxima = agora.Add(res.Adiar)
			return true
		}
		if atual.EfeitoPendente {
			// Repetir os efeitos não depende da janela da política.
			atual.Proxima = agora.Add(recuo(atual.Falhas))
			return true
		}
		proxima, ok := pol.ProximaVerificacao(atual.EmitidaEm, agora)
		if !ok {
			log.Printf("[RECONCILIACAO] cobrança %s saiu da janela de reconciliação (idade máxima atingida)", atual.ID)
			return false
		}
		if atual.Falhas > 0 {
			if minimo := agora.Add(recuo(atual.Falhas)); minimo.After(proxima) {
				proxima = minimo
			}
		}
		atual.Proxima = proxima.Add(r.jitter(proxima.Sub(agora)))
		return true
	})
}

// recuo é o recuo exponencial após erros seguidos: 1, 2, 4, 8... min, até 1 h.
func recuo(falhas int) time.Duration {
	if falhas < 1 {
		falhas = 1
	}
	d := time.Minute << uint(minInt(falhas-1, 6))
	if d > time.Hour {
		d = time.Hour
	}
	return d
}

// verificar consulta uma cobrança e aplica o resultado. Só toca o banco se o
// estado mudou.
func (r *reconciliador) verificar(ctx context.Context, p pendenteReconciliacao) resultadoVerificacao {
	if p.EfeitoPendente {
		return r.aplicarEfeitos(ctx, p)
	}
	prov, err := r.s.ProvedorPorID(p.Provedor)
	if err != nil {
		return resultadoVerificacao{Descartar: true, Erro: err}
	}
	lim := r.limitador(p.Provedor)
	if espera := lim.esperaDisjuntor(r.agora()); espera > 0 {
		return resultadoVerificacao{Adiar: espera}
	}
	cred, err := r.credenciais(ctx, p)
	if err != nil {
		return resultadoVerificacao{Erro: err}
	}
	liberar, err := lim.adquirir(ctx)
	if err != nil {
		return resultadoVerificacao{Erro: err}
	}
	cfg := r.config()
	cctx, cancelar := context.WithTimeout(ctx, cfg.TimeoutConsulta)
	resp, err := prov.ConsultarCobranca(cctx, cred, referenciaCobranca{IDProvedor: p.ProviderID, IDComerciante: p.Merchant})
	cancelar()
	liberar()
	if err != nil {
		lim.registrarFalha(r.agora())
		// Pode ser credencial rotacionada/revogada no provedor: a próxima
		// tentativa relê o cofre em vez de insistir no valor em cache.
		r.creds.remover(chaveCredenciais(p))
		return resultadoVerificacao{Erro: err}
	}
	lim.registrarSucesso()

	status := normalizeChargeStatus(resp.Outcome.Status)
	if status == "" || !isTerminalChargeStatus(status) {
		return resultadoVerificacao{} // continua pendente: nada a gravar
	}

	// O estado mudou: agora sim lê a cobrança e aplica a resposta pelo mesmo
	// caminho da consulta manual.
	row, err := r.s.loadChargeByID(ctx, p.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return resultadoVerificacao{Descartar: true}
		}
		return resultadoVerificacao{Erro: err}
	}
	if isTerminalChargeStatus(row.Status) {
		// Outro caminho (webhook, consulta manual, cancelamento) já a resolveu
		// e, se foi paga, já aplicou os efeitos.
		return resultadoVerificacao{Resolvida: true}
	}
	result, err := r.s.aplicarConsulta(ctx, row, resp, reconciliacaoAtorID, reconciliacaoAtorTipo, reconciliacaoIP)
	if err != nil {
		return resultadoVerificacao{Erro: err}
	}
	if isSuccessfulChargeStatus(result.Status) {
		log.Printf("[RECONCILIACAO] cobrança %s confirmada como paga pelo provedor %s", p.ID, p.Provedor)
		return r.aplicarEfeitos(ctx, p)
	}
	return resultadoVerificacao{Resolvida: true}
}

// aplicarEfeitos executa o que um pagamento confirmado dispara: a propina e,
// via EfeitosPagamento, o serviço extra. Falhou = continua pendente
// de efeito e será repetido (tudo aqui é idempotente).
func (r *reconciliador) aplicarEfeitos(ctx context.Context, p pendenteReconciliacao) resultadoVerificacao {
	if err := r.s.confirmMensalidadeCharge(ctx, p.ID, reconciliacaoAtorID, reconciliacaoAtorTipo, reconciliacaoIP); err != nil {
		return resultadoVerificacao{Resolvida: true, EfeitoPendente: true, Erro: err}
	}
	r.mu.Lock()
	efeitos := r.efeitos
	r.mu.Unlock()
	if efeitos != nil {
		err := efeitos(ctx, CobrancaConfirmada{ID: p.ID, ProviderChargeID: p.ProviderID, MerchantTransactionID: p.Merchant, Provedor: p.Provedor})
		if err != nil {
			return resultadoVerificacao{Resolvida: true, EfeitoPendente: true, Erro: err}
		}
	}
	return resultadoVerificacao{Resolvida: true}
}

// chaveCredenciais identifica, no cache, as credenciais de uma cobrança: a
// credencial emitente (exata, ativa ou arquivada) ou, nas legadas sem vínculo,
// a ativa do contexto.
func chaveCredenciais(p pendenteReconciliacao) string {
	if p.CredencialID == uuid.Nil {
		return "ativa|" + p.Provedor + "|" + p.Contexto + "|" + p.Academia
	}
	return p.CredencialID.String()
}

// credenciais devolve as credenciais da cobrança, do cache quando possível. A
// chave é a credencial emitente (exata, ativa ou arquivada); cobranças legadas
// sem vínculo usam a credencial ativa do contexto.
func (r *reconciliador) credenciais(ctx context.Context, p pendenteReconciliacao) (credencialProvedor, error) {
	chave := chaveCredenciais(p)
	agora := r.agora()
	if c, ok := r.creds.obter(chave, agora); ok {
		return c, nil
	}
	c, err := r.s.credencialDaCobranca(ctx, chargeRow{ID: p.ID, Contexto: p.Contexto, Academia: p.Academia, Provedor: p.Provedor, CredencialID: p.CredencialID})
	if err != nil {
		return credencialProvedor{}, err
	}
	r.creds.guardar(chave, c, agora.Add(r.config().TTLCredenciais))
	return c, nil
}
