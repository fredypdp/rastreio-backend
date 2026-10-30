package aggregates

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Tarefa 122 — regras do 2FA nos três agregados.

func novoAdminVerificado(t *testing.T, verificado bool) *Admin {
	t.Helper()
	telefone := "923456789"
	a := NewAdmin()
	if err := a.Criar("Admin Teste", "admin@example.test", &telefone, strings.Repeat("h", 60), "adm", nil); err != nil {
		t.Fatalf("Criar: %v", err)
	}
	if verificado {
		if err := a.VerificarEmail(); err != nil {
			t.Fatalf("VerificarEmail: %v", err)
		}
	}
	return a
}

func novaAcademiaParaDoisFatores(emailVerificado bool) *Academia {
	email := "academia@example.test"
	a := NewAcademia()
	a.Status = "ativo"
	a.Email = &email
	a.EmailVerificado = emailVerificado
	return a
}

func novoEstudanteParaDoisFatores(t *testing.T, emailVerificado bool) *Estudante {
	t.Helper()
	ano := "1_ano_fundamental"
	e := criarEstudanteComAnoParaTeste(t, &ano, nil, nil)
	email := "estudante@example.test"
	e.Email = &email
	e.EmailVerificado = emailVerificado
	return e
}

type comDoisFatores interface {
	AtivarDoisFatores() error
	DesativarDoisFatores() error
}

func TestAdminDoisFatoresExigeEmailVerificado(t *testing.T) {
	a := novoAdminVerificado(t, false)
	if err := a.AtivarDoisFatores(); !errors.Is(err, ErrDoisFatoresEmailNaoVerificado) {
		t.Fatalf("sem e-mail verificado deveria falhar com ErrDoisFatoresEmailNaoVerificado, obteve %v", err)
	}
	if a.DoisFatoresAtivo {
		t.Fatal("estado não deveria mudar quando a ativação é recusada")
	}
}

func TestAcademiaEEstudanteDoisFatoresExigemEmailVerificado(t *testing.T) {
	casos := map[string]comDoisFatores{
		"academia":  novaAcademiaParaDoisFatores(false),
		"estudante": novoEstudanteParaDoisFatores(t, false),
	}
	for nome, agg := range casos {
		if err := agg.AtivarDoisFatores(); !errors.Is(err, ErrDoisFatoresEmailNaoVerificado) {
			t.Errorf("%s: esperava ErrDoisFatoresEmailNaoVerificado, obteve %v", nome, err)
		}
	}

	// Sem e-mail algum (estudante cadastrado só com telefone).
	ano := "1_ano_fundamental"
	semEmail := criarEstudanteComAnoParaTeste(t, &ano, nil, nil)
	if err := semEmail.AtivarDoisFatores(); !errors.Is(err, ErrDoisFatoresEmailNaoVerificado) {
		t.Errorf("estudante sem e-mail: esperava ErrDoisFatoresEmailNaoVerificado, obteve %v", err)
	}
}

func TestDoisFatoresCicloAtivarDesativarNosTresAgregados(t *testing.T) {
	casos := map[string]comDoisFatores{
		"admin":     novoAdminVerificado(t, true),
		"academia":  novaAcademiaParaDoisFatores(true),
		"estudante": novoEstudanteParaDoisFatores(t, true),
	}
	for nome, agg := range casos {
		if err := agg.DesativarDoisFatores(); !errors.Is(err, ErrDoisFatoresNaoAtivo) {
			t.Errorf("%s: desativar sem estar ativo deveria falhar com ErrDoisFatoresNaoAtivo, obteve %v", nome, err)
		}
		if err := agg.AtivarDoisFatores(); err != nil {
			t.Fatalf("%s: ativar: %v", nome, err)
		}
		if err := agg.AtivarDoisFatores(); !errors.Is(err, ErrDoisFatoresJaAtivo) {
			t.Errorf("%s: ativar duas vezes deveria falhar com ErrDoisFatoresJaAtivo, obteve %v", nome, err)
		}
		if err := agg.DesativarDoisFatores(); err != nil {
			t.Fatalf("%s: desativar: %v", nome, err)
		}
		if err := agg.DesativarDoisFatores(); !errors.Is(err, ErrDoisFatoresNaoAtivo) {
			t.Errorf("%s: desativar duas vezes deveria falhar com ErrDoisFatoresNaoAtivo, obteve %v", nome, err)
		}
	}
}

// O estado é reconstruído pelos eventos (event sourcing): reaplicar o histórico
// num agregado novo tem de reproduzir o 2FA ativo/inativo.
func TestAdminDoisFatoresEventosReconstroemEstado(t *testing.T) {
	origem := novoAdminVerificado(t, true)
	if err := origem.AtivarDoisFatores(); err != nil {
		t.Fatal(err)
	}
	eventos := append([]DomainEvent{}, origem.UncommittedEvents...)

	reconstruido := NewAdmin()
	for _, ev := range eventos {
		if err := reconstruido.Apply(ev); err != nil {
			t.Fatalf("Apply(%s): %v", ev.GetEventType(), err)
		}
	}
	if !reconstruido.DoisFatoresAtivo {
		t.Fatal("o histórico com DoisFatoresAtivados deveria reconstruir o 2FA ativo")
	}

	if err := origem.DesativarDoisFatores(); err != nil {
		t.Fatal(err)
	}
	ultimo := origem.UncommittedEvents[len(origem.UncommittedEvents)-1]
	if ultimo.GetEventType() != "DoisFatoresDesativados" {
		t.Fatalf("último evento deveria ser DoisFatoresDesativados, é %s", ultimo.GetEventType())
	}
	if err := reconstruido.Apply(ultimo); err != nil {
		t.Fatal(err)
	}
	if reconstruido.DoisFatoresAtivo {
		t.Fatal("DoisFatoresDesativados deveria desligar o 2FA no estado reconstruído")
	}
}

func TestDoisFatoresEventosTemTipoEMetodo(t *testing.T) {
	a := novoAdminVerificado(t, true)
	if err := a.AtivarDoisFatores(); err != nil {
		t.Fatal(err)
	}
	ev, ok := a.UncommittedEvents[len(a.UncommittedEvents)-1].(*DoisFatoresAtivadosEvent)
	if !ok {
		t.Fatalf("evento inesperado: %T", a.UncommittedEvents[len(a.UncommittedEvents)-1])
	}
	if ev.GetEventType() != "DoisFatoresAtivados" || ev.Metodo != MetodoDoisFatoresEmail || ev.AggregateID != a.ID {
		t.Fatalf("evento incorreto: %+v", ev)
	}
	if ev.AtivadoEm.IsZero() || time.Since(ev.AtivadoEm) > time.Minute {
		t.Fatalf("AtivadoEm inválido: %v", ev.AtivadoEm)
	}
	raw, err := ev.ToJSON()
	if err != nil || !strings.Contains(string(raw), `"Metodo":"email"`) {
		t.Fatalf("payload inesperado: %s (%v)", raw, err)
	}
}

// ── Troca de e-mail com 2FA ativo ───────────────────────────────────────────

func TestAdminNaoAlteraEmailComDoisFatoresAtivo(t *testing.T) {
	a := novoAdminVerificado(t, true)
	if err := a.AtivarDoisFatores(); err != nil {
		t.Fatal(err)
	}
	novo := "outro@example.test"
	if err := a.AtualizarDados(nil, &novo, nil, uuid.New()); !errors.Is(err, ErrEmailBloqueadoPorDoisFatores) {
		t.Fatalf("trocar e-mail com 2FA ativo deveria falhar com ErrEmailBloqueadoPorDoisFatores, obteve %v", err)
	}
	if a.Email != "admin@example.test" || !a.EmailVerificado {
		t.Fatal("o e-mail e a verificação não podem mudar quando a troca é recusada")
	}

	// Reenviar o MESMO e-mail (ou mudar só o nome) continua permitido.
	mesmo := a.Email
	nome := "Admin Renomeado"
	if err := a.AtualizarDados(&nome, &mesmo, nil, uuid.New()); err != nil {
		t.Fatalf("manter o mesmo e-mail deveria ser permitido: %v", err)
	}

	// Depois de desativar o 2FA a troca volta a ser possível (e zera a verificação).
	if err := a.DesativarDoisFatores(); err != nil {
		t.Fatal(err)
	}
	if err := a.AtualizarDados(nil, &novo, nil, uuid.New()); err != nil {
		t.Fatalf("com o 2FA desativado a troca deveria ser permitida: %v", err)
	}
	if a.Email != novo || a.EmailVerificado {
		t.Fatalf("e-mail deveria mudar e perder a verificação: %s verificado=%v", a.Email, a.EmailVerificado)
	}
}

func TestAcademiaNaoAlteraEmailComDoisFatoresAtivo(t *testing.T) {
	a := novaAcademiaParaDoisFatores(true)
	if err := a.AtivarDoisFatores(); err != nil {
		t.Fatal(err)
	}
	novo := "outra@example.test"
	err := a.AtualizarDados(nil, nil, nil, nil, nil, nil, &novo, nil, nil, nil, nil)
	if !errors.Is(err, ErrEmailBloqueadoPorDoisFatores) {
		t.Fatalf("esperava ErrEmailBloqueadoPorDoisFatores, obteve %v", err)
	}
	if *a.Email != "academia@example.test" {
		t.Fatal("o e-mail não pode mudar quando a troca é recusada")
	}
	if err := a.DesativarDoisFatores(); err != nil {
		t.Fatal(err)
	}
	if err := a.AtualizarDados(nil, nil, nil, nil, nil, nil, &novo, nil, nil, nil, nil); err != nil {
		t.Fatalf("com o 2FA desativado a troca deveria ser permitida: %v", err)
	}
}

func TestEstudanteNaoAlteraEmailComDoisFatoresAtivo(t *testing.T) {
	e := novoEstudanteParaDoisFatores(t, true)
	if err := e.AtivarDoisFatores(); err != nil {
		t.Fatal(err)
	}
	novo := "novo-estudante@example.test"
	err := e.AtualizarDadosPessoais(nil, &novo, nil, nil, nil, nil, nil)
	if !errors.Is(err, ErrEmailBloqueadoPorDoisFatores) {
		t.Fatalf("esperava ErrEmailBloqueadoPorDoisFatores, obteve %v", err)
	}
	if *e.Email != "estudante@example.test" {
		t.Fatal("o e-mail não pode mudar quando a troca é recusada")
	}
	if err := e.DesativarDoisFatores(); err != nil {
		t.Fatal(err)
	}
	if err := e.AtualizarDadosPessoais(nil, &novo, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("com o 2FA desativado a troca deveria ser permitida: %v", err)
	}
}
