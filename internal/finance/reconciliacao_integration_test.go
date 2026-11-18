package finance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"spuri/internal/db"

	"github.com/google/uuid"
)

// transporteRecon simula a AppyPay: conta as consultas, pode falhar, devolve o
// status configurado e registra o client_id de cada pedido de token (prova de
// QUAIS credenciais foram usadas).
type transporteRecon struct {
	mu        sync.Mutex
	status    string
	falha     bool
	consultas int
	clientIDs []string
}

func (t *transporteRecon) definir(status string, falha bool) {
	t.mu.Lock()
	t.status, t.falha = status, falha
	t.mu.Unlock()
}

func (t *transporteRecon) totalConsultas() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.consultas
}

func (t *transporteRecon) clientes() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.clientIDs...)
}

func (t *transporteRecon) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	resposta := func(body string) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}
	switch {
	case strings.Contains(req.URL.Path, "/oauth2/token"):
		raw, _ := io.ReadAll(req.Body)
		form, _ := url.ParseQuery(string(raw))
		t.clientIDs = append(t.clientIDs, form.Get("client_id"))
		return resposta(`{"access_token":"test-token","expires_in":3600}`)
	case req.Method == http.MethodGet:
		t.consultas++
		if t.falha {
			return nil, errors.New("provedor fora do ar")
		}
		providerID := strings.TrimPrefix(req.URL.EscapedPath(), "/v2.0/charges/")
		rs := fmt.Sprintf(`{"successful":true,"status":%q,"source":"GPO"}`, t.status)
		return resposta(`{"payment":{"id":"` + providerID + `","status":"` + t.status + `","transactionEvents":[{"responseStatus":` + rs + `}]}}`)
	default:
		return resposta(`{"id":"provider-charge-` + uuid.NewString() + `","responseStatus":{"successful":true,"status":"Pending","source":"GPO"}}`)
	}
}

// relogioRecon é um relógio controlável.
type relogioRecon struct {
	mu sync.Mutex
	t  time.Time
}

func (r *relogioRecon) Now() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.t
}

func (r *relogioRecon) Definir(t time.Time) { r.mu.Lock(); r.t = t; r.mu.Unlock() }
func (r *relogioRecon) Avancar(d time.Duration) {
	r.mu.Lock()
	r.t = r.t.Add(d)
	r.mu.Unlock()
}

// efeitosRecon grava as chamadas de EfeitosPagamento e permite fazê-las falhar.
type efeitosRecon struct {
	mu              sync.Mutex
	chamadas        []CobrancaConfirmada
	falhasRestantes int
}

func (e *efeitosRecon) fn(_ context.Context, c CobrancaConfirmada) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.chamadas = append(e.chamadas, c)
	if e.falhasRestantes > 0 {
		e.falhasRestantes--
		return errors.New("efeito de negócio falhou")
	}
	return nil
}

func (e *efeitosRecon) total() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.chamadas)
}

type ambienteRecon struct {
	t          *testing.T
	service    *Service
	client     *db.Client
	transporte *transporteRecon
	relogio    *relogioRecon
	efeitos    *efeitosRecon
}

func (a *ambienteRecon) queryRow(q string, args ...any) *sql.Row {
	return a.client.DB().QueryRow(q, args...)
}

func (a *ambienteRecon) exec(q string, args ...any) error {
	_, err := a.client.DB().Exec(q, args...)
	return err
}

func novoAmbienteRecon(t *testing.T) (*ambienteRecon, *Service) {
	t.Helper()
	client := integrationClient(t)
	t.Setenv("ENV", "test")
	t.Setenv("APPYPAY_RESOURCE", "integration-resource")
	t.Setenv("JWT_SECRET", "test-only-secret-material-at-least-32")
	service := NewService(client)
	tr := &transporteRecon{status: "Pending"}
	service.SetHTTPClient(&http.Client{Transport: tr})
	rel := &relogioRecon{t: time.Now()}
	ef := &efeitosRecon{}
	service.SetEfeitosPagamento(ef.fn)
	service.reconc.agora = rel.Now
	service.reconc.jitter = func(time.Duration) time.Duration { return 0 }
	service.reconc.ativo.Store(true)
	// Os testes simulam horas em milissegundos: o limite de taxa real (por
	// segundo) só atrasaria a suíte. O limitador em si é testado à parte.
	service.reconc.cfg.ChamadasPorSegundo = 1000
	return &ambienteRecon{t: t, service: service, client: client, transporte: tr, relogio: rel, efeitos: ef}, service
}

// configurarCredencial cadastra (ou edita) as credenciais da academia.
func configurarCredencialRecon(t *testing.T, s *Service, academia, clientID string) CredentialView {
	t.Helper()
	view, _, err := s.ConfigureCredential(context.Background(), nil, CredentialInput{
		ContextoTipo: ContextoAcademia, CodigoAcademia: academia, Provedor: ProvedorAppyPay,
		Credenciais: map[string]string{"client_id": clientID, "client_secret": "segredo-" + clientID, "gpo_method": "GPO_INTEGRATION", "ref_method": "REF_INTEGRATION"},
	}, "integration-test", "sistema", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return view
}

// novaCobrancaREF cria uma academia com credenciais e uma cobrança REF pendente.
func novaCobrancaREFRecon(t *testing.T, a *ambienteRecon, clientID string) (academia string, cobranca uuid.UUID) {
	t.Helper()
	academia = "REC" + uuid.NewString()[:8]
	configurarCredencialRecon(t, a.service, academia, clientID)
	codigo := novaObrigacaoTeste(900)
	view, err := a.service.iniciarCobrancaTeste(context.Background(), academia, codigo, "REF", "")
	if err != nil {
		t.Fatal(err)
	}
	return academia, view.Charge.ID
}

func (a *ambienteRecon) statusDaCobranca(id uuid.UUID) string {
	var s string
	if err := a.queryRow(`SELECT COALESCE(payload->>'status','') FROM financeiro_cobrancas WHERE id=$1`, id).Scan(&s); err != nil {
		a.t.Fatal(err)
	}
	return s
}

func (a *ambienteRecon) eventosDaCobranca(id uuid.UUID) int {
	var n int
	if err := a.queryRow(`SELECT COUNT(*) FROM spuri_ledger WHERE aggregate_id=$1`, id).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n
}

func (a *ambienteRecon) entrada(id uuid.UUID) pendenteReconciliacao {
	a.t.Helper()
	r := a.service.reconc.reg
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.itens[id]
	if !ok {
		a.t.Fatalf("cobrança %s não está no registro", id)
	}
	return *p
}

func TestIntegrationReconciliacaoConfirmaPagamentoQuandoProvedorDizSuccess(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	_, id := novaCobrancaREFRecon(t, a, "cliente-a")

	if !s.reconc.reg.Existe(id) {
		t.Fatal("a cobrança criada deveria entrar no registro")
	}
	emitida := a.relogio.Now()
	if proxima := a.entrada(id).Proxima; !proxima.Equal(emitida.Add(10 * time.Minute)) {
		t.Fatalf("a primeira consulta de uma REF deveria ser em 10 min; proxima = %v", proxima.Sub(emitida))
	}
	// Antes da hora nada acontece.
	if n := s.reconc.processarVencidos(context.Background()); n != 0 {
		t.Fatalf("processadas antes da hora: %d", n)
	}

	a.transporte.definir("Success", false)
	a.relogio.Avancar(11 * time.Minute)
	if n := s.reconc.processarVencidos(context.Background()); n != 1 {
		t.Fatalf("processadas = %d, queria 1", n)
	}
	if got := a.statusDaCobranca(id); got != "Success" {
		t.Fatalf("status = %q, queria Success", got)
	}
	if s.reconc.reg.Existe(id) {
		t.Fatal("cobrança paga deve sair do registro")
	}
	if a.efeitos.total() != 1 || a.efeitos.chamadas[0].ID != id {
		t.Fatalf("os efeitos de pagamento deveriam ter sido disparados uma vez: %+v", a.efeitos.chamadas)
	}
	// Reprocessar não consulta nem dispara de novo.
	consultas := a.transporte.totalConsultas()
	a.relogio.Avancar(24 * time.Hour)
	s.reconc.processarVencidos(context.Background())
	if a.transporte.totalConsultas() != consultas || a.efeitos.total() != 1 {
		t.Fatal("cobrança resolvida não pode ser reconsultada nem reaplicada")
	}
}

func TestIntegrationReconciliacaoContinuaPendenteNaoGravaENaoLeOBanco(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	_, id := novaCobrancaREFRecon(t, a, "cliente-b")
	eventos := a.eventosDaCobranca(id)
	var atualizadoAntes time.Time
	if err := a.queryRow(`SELECT updated_at FROM financeiro_cobrancas WHERE id=$1`, id).Scan(&atualizadoAntes); err != nil {
		t.Fatal(err)
	}

	a.relogio.Avancar(11 * time.Minute)
	s.reconc.processarVencidos(context.Background())
	if a.transporte.totalConsultas() != 1 {
		t.Fatalf("consultas = %d, queria 1", a.transporte.totalConsultas())
	}
	var atualizadoDepois time.Time
	if err := a.queryRow(`SELECT updated_at FROM financeiro_cobrancas WHERE id=$1`, id).Scan(&atualizadoDepois); err != nil {
		t.Fatal(err)
	}
	if a.eventosDaCobranca(id) != eventos || !atualizadoDepois.Equal(atualizadoAntes) || a.statusDaCobranca(id) == "Success" {
		t.Fatal("continuar pendente não pode gravar nada")
	}
	p := a.entrada(id)
	if p.Falhas != 0 || !p.Proxima.Equal(a.relogio.Now().Add(10*time.Minute)) {
		t.Fatalf("deveria reagendar em 10 min sem falhas: %+v", p)
	}

	// A PROVA: sem acesso ao banco (conexões fechadas), uma nova verificação de
	// cobrança ainda pendente funciona, porque usa o registro e as credenciais
	// em cache. Só uma mudança de estado precisaria do banco.
	if err := s.client.DB().Close(); err != nil {
		t.Fatal(err)
	}
	a.relogio.Avancar(11 * time.Minute)
	if n := s.reconc.processarVencidos(context.Background()); n != 1 {
		t.Fatalf("processadas = %d", n)
	}
	if a.transporte.totalConsultas() != 2 {
		t.Fatalf("a verificação sem banco deveria ter consultado o provedor: %d", a.transporte.totalConsultas())
	}
	if p := a.entrada(id); p.Falhas != 0 {
		t.Fatalf("a verificação não pode ter falhado sem o banco: %+v", p)
	}
}

func TestIntegrationReconciliacaoErroDoProvedorNaoMudaEstadoEAbreDisjuntor(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	_, id := novaCobrancaREFRecon(t, a, "cliente-c")
	eventos := a.eventosDaCobranca(id)

	a.transporte.definir("Pending", true)
	a.relogio.Avancar(11 * time.Minute)
	s.reconc.processarVencidos(context.Background())
	if a.statusDaCobranca(id) == "Success" || a.eventosDaCobranca(id) != eventos {
		t.Fatal("um erro do provedor nunca muda a cobrança")
	}
	if p := a.entrada(id); p.Falhas != 1 {
		t.Fatalf("falhas = %d, queria 1", p.Falhas)
	}

	// Com o disjuntor aberto, nem chama o provedor. Abre-se o disjuntor pouco
	// antes de a cobrança vencer, para a verificação cair dentro da pausa.
	a.relogio.Avancar(9 * time.Minute)
	lim := s.reconc.limitador(ProvedorAppyPay)
	for i := 0; i < falhasParaAbrirDisjuntor; i++ {
		lim.registrarFalha(a.relogio.Now())
	}
	abertoAte := a.relogio.Now().Add(lim.esperaDisjuntor(a.relogio.Now()))
	consultas := a.transporte.totalConsultas()
	a.relogio.Avancar(90 * time.Second) // vencida (10 min) mas ainda dentro da pausa
	if !a.relogio.Now().Before(abertoAte) {
		t.Fatal("o teste precisa verificar dentro da pausa do disjuntor")
	}
	s.reconc.processarVencidos(context.Background())
	if a.transporte.totalConsultas() != consultas {
		t.Fatal("com o disjuntor aberto o provedor não pode ser consultado")
	}
	if p := a.entrada(id); !p.Proxima.Equal(abertoAte) {
		t.Fatalf("deveria esperar o disjuntor fechar: proxima=%v abertoAte=%v", p.Proxima, abertoAte)
	}

	// O provedor volta: a cobrança é confirmada.
	a.transporte.definir("Success", false)
	a.relogio.Avancar(2 * time.Minute)
	s.reconc.processarVencidos(context.Background())
	if a.statusDaCobranca(id) != "Success" || s.reconc.reg.Existe(id) {
		t.Fatal("com o provedor de volta a cobrança deveria ser confirmada")
	}
}

func TestIntegrationReconciliacaoRepeteEfeitosQueFalhamSemReconsultar(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	_, id := novaCobrancaREFRecon(t, a, "cliente-d")
	a.efeitos.falhasRestantes = 1
	a.transporte.definir("Success", false)

	a.relogio.Avancar(11 * time.Minute)
	s.reconc.processarVencidos(context.Background())
	if a.statusDaCobranca(id) != "Success" {
		t.Fatal("o pagamento deve ser gravado mesmo que os efeitos falhem")
	}
	p := a.entrada(id)
	if !p.EfeitoPendente || p.Falhas != 1 {
		t.Fatalf("deveria ficar com efeito pendente: %+v", p)
	}
	if !p.Proxima.Equal(a.relogio.Now().Add(time.Minute)) {
		t.Fatalf("os efeitos devem ser repetidos em 1 min: %v", p.Proxima.Sub(a.relogio.Now()))
	}

	consultas := a.transporte.totalConsultas()
	a.relogio.Avancar(2 * time.Minute)
	s.reconc.processarVencidos(context.Background())
	if a.efeitos.total() != 2 {
		t.Fatalf("efeitos chamados %d vezes, queria 2", a.efeitos.total())
	}
	if a.transporte.totalConsultas() != consultas {
		t.Fatal("repetir efeitos não pode reconsultar o provedor")
	}
	if s.reconc.reg.Existe(id) {
		t.Fatal("depois de aplicar os efeitos a cobrança sai do registro")
	}
}

func TestIntegrationReconciliacaoDesisteNaIdadeMaximaSemMudarEstado(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	_, id := novaCobrancaREFRecon(t, a, "cliente-e")
	emitida := a.relogio.Now()
	eventos := a.eventosDaCobranca(id)

	var ultima time.Time
	iteracoes := 0
	for s.reconc.reg.Existe(id) {
		if iteracoes++; iteracoes > 200 {
			t.Fatal("a reconciliação deveria ter desistido")
		}
		proxima, ok := s.reconc.reg.ProximaData()
		if !ok {
			t.Fatal("sem próxima data mas ainda registrada")
		}
		a.relogio.Definir(proxima)
		ultima = proxima
		s.reconc.processarVencidos(context.Background())
	}
	if !ultima.Equal(emitida.Add(30 * 24 * time.Hour)) {
		t.Fatalf("a última consulta deveria ser exatamente na idade máxima (30 dias): %v", ultima.Sub(emitida))
	}
	if iteracoes < 70 || iteracoes > 95 {
		t.Fatalf("número de consultas fora do esperado para a política REF: %d", iteracoes)
	}
	if a.transporte.totalConsultas() != iteracoes {
		t.Fatalf("consultas ao provedor = %d, iterações = %d", a.transporte.totalConsultas(), iteracoes)
	}
	if a.statusDaCobranca(id) == "Success" || a.eventosDaCobranca(id) != eventos {
		t.Fatal("desistir da reconciliação não pode alterar a cobrança")
	}
}

func TestIntegrationReconciliacaoCarregaPendentesNoArranque(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	s.reconc.ativo.Store(false) // simula cobranças criadas antes do arranque do job

	_, pendente := novaCobrancaREFRecon(t, a, "cliente-f1")
	_, antiga := novaCobrancaREFRecon(t, a, "cliente-f2")
	_, paga := novaCobrancaREFRecon(t, a, "cliente-f3")
	_, externa := novaCobrancaREFRecon(t, a, "cliente-f4")
	if s.reconc.reg.Len() != 0 {
		t.Fatal("com o job inativo nada é registrado")
	}
	if err := a.exec(`UPDATE financeiro_cobrancas SET emitida_em = emitida_em - interval '31 days' WHERE id=$1`, antiga); err != nil {
		t.Fatal(err)
	}
	if err := a.exec(`UPDATE financeiro_cobrancas SET payload = jsonb_set(payload,'{status}','"Success"') WHERE id=$1`, paga); err != nil {
		t.Fatal(err)
	}
	if err := a.exec(`UPDATE financeiro_cobrancas SET provedor='EXTERNO' WHERE id=$1`, externa); err != nil {
		t.Fatal(err)
	}

	s.reconc.ativo.Store(true)
	if err := s.reconc.carregar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !s.reconc.reg.Existe(pendente) {
		t.Fatal("a pendente dentro da janela deveria ser carregada")
	}
	for nome, id := range map[string]uuid.UUID{"fora da janela (31 dias)": antiga, "já paga": paga, "pagamento externo": externa} {
		if s.reconc.reg.Existe(id) {
			t.Fatalf("não deveria carregar: %s", nome)
		}
	}
	// A primeira verificação pós-arranque vem logo (webhooks podem ter sido perdidos).
	if p := a.entrada(pendente); p.Proxima.After(a.relogio.Now().Add(61*time.Second)) || p.Metodo != MetodoREF || p.CredencialID == uuid.Nil {
		t.Fatalf("dados carregados incorretos: %+v", p)
	}
}

func TestIntegrationReconciliacaoSaiDoRegistroQuandoOutroCaminhoResolve(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	academia, id := novaCobrancaREFRecon(t, a, "cliente-g")
	a.transporte.definir("Success", false)
	// Consulta manual (a mesma que o painel e o webhook usam).
	if _, err := s.ConsultCharge(context.Background(), ContextoAcademia, academia, id.String(), "u", "academia", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if s.reconc.reg.Existe(id) {
		t.Fatal("cobrança resolvida por consulta manual deve sair do registro")
	}
}

func TestIntegrationReconciliacaoCancelamentoRetiraDoRegistro(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	academia, id := novaCobrancaREFRecon(t, a, "cliente-h")
	if _, err := s.CancelCharge(context.Background(), ContextoAcademia, academia, id.String(), "teste", "u", "academia", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if s.reconc.reg.Existe(id) {
		t.Fatal("cobrança cancelada deve sair do registro")
	}
}

func TestIntegrationReconciliacaoLacoAcordaPorEventoEDormeSemTimer(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	// Relógio real: o laço usa timers reais.
	s.reconc.agora = time.Now
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	finalizado := make(chan struct{})
	go func() { s.reconc.executar(ctx); close(finalizado) }()

	time.Sleep(150 * time.Millisecond) // o laço dorme: registro vazio, sem timer
	if s.reconc.reg.Len() != 0 {
		t.Fatal("registro deveria estar vazio")
	}

	_, id := novaCobrancaREFRecon(t, a, "cliente-i") // acorda o laço por evento
	a.transporte.definir("Success", false)
	// Antecipa a data da cobrança para o laço (que dormia até +10 min) reagendar.
	s.reconc.reg.mu.Lock()
	s.reconc.reg.itens[id].Proxima = time.Now().Add(300 * time.Millisecond)
	s.reconc.reg.mu.Unlock()
	s.reconc.reg.sinalizar()

	limite := time.Now().Add(5 * time.Second)
	for s.reconc.reg.Existe(id) && time.Now().Before(limite) {
		time.Sleep(50 * time.Millisecond)
	}
	if s.reconc.reg.Existe(id) || a.statusDaCobranca(id) != "Success" || a.efeitos.total() != 1 {
		t.Fatalf("o laço deveria ter confirmado a cobrança (registro=%v status=%s efeitos=%d)", s.reconc.reg.Existe(id), a.statusDaCobranca(id), a.efeitos.total())
	}
	cancelar()
	select {
	case <-finalizado:
	case <-time.After(2 * time.Second):
		t.Fatal("o laço deveria terminar quando o contexto é cancelado")
	}
}

func TestIntegrationReconciliacaoEmitidaEmGravadaPelaProjecaoESobreviveAoRebuild(t *testing.T) {
	a, s := novoAmbienteRecon(t)
	_, id := novaCobrancaREFRecon(t, a, "cliente-j")
	var antes, primeiroEvento time.Time
	if err := a.queryRow(`SELECT emitida_em FROM financeiro_cobrancas WHERE id=$1`, id).Scan(&antes); err != nil {
		t.Fatal(err)
	}
	if err := a.queryRow(`SELECT MIN(occurred_at) FROM spuri_ledger WHERE aggregate_id=$1`, id).Scan(&primeiroEvento); err != nil {
		t.Fatal(err)
	}
	if !antes.Equal(primeiroEvento) {
		t.Fatalf("emitida_em (%v) deve ser o occurred_at do primeiro evento (%v)", antes, primeiroEvento)
	}
	// Um evento posterior não pode mover a emissão.
	a.transporte.definir("Pending", false)
	if _, err := s.ConsultCharge(context.Background(), ContextoAcademia, strings.TrimSpace(a.academiaDe(id)), id.String(), "u", "academia", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := s.projection.Rebuild(); err != nil {
		t.Fatal(err)
	}
	var depois time.Time
	if err := a.queryRow(`SELECT emitida_em FROM financeiro_cobrancas WHERE id=$1`, id).Scan(&depois); err != nil {
		t.Fatal(err)
	}
	if !depois.Equal(primeiroEvento) {
		t.Fatalf("depois do rebuild emitida_em = %v, queria %v", depois, primeiroEvento)
	}
}

func (a *ambienteRecon) academiaDe(id uuid.UUID) string {
	var ac string
	if err := a.queryRow(`SELECT COALESCE(codigo_academia,'') FROM financeiro_cobrancas WHERE id=$1`, id).Scan(&ac); err != nil {
		a.t.Fatal(err)
	}
	return ac
}
