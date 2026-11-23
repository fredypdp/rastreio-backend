package aggregates

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func dadosValidos() DadosContaPessoa {
	return DadosContaPessoa{
		Codigo: "SC123456", Nome: "Maria da Silva", Genero: "feminino",
		DataNascimento: time.Now().AddDate(-30, 0, 0), Telefone: "923456789", SenhaHash: strings.Repeat("h", 60),
	}
}

// recarrega reproduz o Load: aplica os eventos de um agregado a outro novo.
func recarregaSecretario(t *testing.T, origem *Secretario) *Secretario {
	t.Helper()
	novo := NewSecretario()
	for _, ev := range origem.GetUncommittedEvents() {
		if err := novo.Apply(ev); err != nil {
			t.Fatalf("apply %s: %v", ev.GetEventType(), err)
		}
	}
	return novo
}

func TestSecretarioCriarNormalizaPermissoesERecarregaDoLedger(t *testing.T) {
	s := NewSecretario()
	acad := uuid.New()
	if err := s.Criar(dadosValidos(), acad, "LUA20261", []string{"sumarios.gerir"}, &acad); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.Permissoes, ","); got != "sumarios.gerir,sumarios.ver" {
		t.Fatalf("permissões normalizadas: %s", got)
	}
	r := recarregaSecretario(t, s)
	if r.Status != StatusContaAtivo || r.CodigoAcademia != "LUA20261" || len(r.Permissoes) != 2 {
		t.Fatalf("estado recarregado incorreto: %+v", r)
	}
	if err := r.DefinirPermissoes([]string{"turmas.ver"}, &acad); err != nil || len(r.Permissoes) != 1 {
		t.Fatalf("redefinir permissões: %v %v", err, r.Permissoes)
	}
}

func TestSecretarioNaoAceitaPermissaoInvalidaNemVazia(t *testing.T) {
	acad := uuid.New()
	for _, perms := range [][]string{{"notas.lancar"}, {"secretarios.gerir"}, {}, nil} {
		if err := NewSecretario().Criar(dadosValidos(), acad, "LUA20261", perms, &acad); err == nil {
			t.Errorf("permissões %v deveriam ser recusadas", perms)
		}
	}
}

func TestContaPessoaValidaCamposComuns(t *testing.T) {
	casos := map[string]func(*DadosContaPessoa){
		"telefone fixo":     func(d *DadosContaPessoa) { d.Telefone = "222123456" },
		"gênero inválido":   func(d *DadosContaPessoa) { d.Genero = "x" },
		"menor de idade":    func(d *DadosContaPessoa) { d.DataNascimento = time.Now().AddDate(-10, 0, 0) },
		"nasce no futuro":   func(d *DadosContaPessoa) { d.DataNascimento = time.Now().AddDate(1, 0, 0) },
		"e-mail inválido":   func(d *DadosContaPessoa) { e := "sem-arroba"; d.Email = &e },
		"hash curto demais": func(d *DadosContaPessoa) { d.SenhaHash = "curto" },
		"nome vazio":        func(d *DadosContaPessoa) { d.Nome = "" },
	}
	for nome, ajuste := range casos {
		d := dadosValidos()
		ajuste(&d)
		if err := ValidarDadosContaPessoa(d); err == nil {
			t.Errorf("%s deveria ser recusado", nome)
		}
	}
	if err := ValidarDadosContaPessoa(dadosValidos()); err != nil {
		t.Fatalf("dados válidos recusados: %v", err)
	}
}

func TestContaPessoaCicloDeVida(t *testing.T) {
	acad := uuid.New()
	s := NewSecretario()
	_ = s.Criar(dadosValidos(), acad, "LUA20261", []string{"turmas.ver"}, &acad)
	if err := s.Ativar(&acad); err == nil {
		t.Error("ativar uma conta já ativa deveria falhar")
	}
	if err := s.Desativar(&acad, "afastado"); err != nil || s.Status != StatusContaInativo {
		t.Fatalf("desativar: %v %s", err, s.Status)
	}
	if err := s.Ativar(&acad); err != nil || s.Status != StatusContaAtivo {
		t.Fatalf("reativar: %v %s", err, s.Status)
	}
	if err := s.Deletar(&acad, "saiu"); err != nil || s.Status != StatusContaDeletado {
		t.Fatalf("deletar: %v %s", err, s.Status)
	}
	if err := s.Ativar(&acad); err == nil {
		t.Error("conta eliminada não pode ser reativada")
	}
	if err := s.AlterarSenhaComMotivo(strings.Repeat("n", 60), MotivoSenhaCredenciaisReenviadas); err == nil {
		t.Error("conta eliminada não troca senha")
	}
}

func TestMudarEmailZeraVerificacaoEDoisFatores(t *testing.T) {
	acad := uuid.New()
	d := dadosValidos()
	e := "ana@exemplo.ao"
	d.Email = &e
	s := NewSecretario()
	_ = s.Criar(d, acad, "LUA20261", []string{"turmas.ver"}, &acad)
	if err := s.VerificarEmail(); err != nil {
		t.Fatal(err)
	}
	if err := s.AtivarDoisFatores(); err != nil || !s.DoisFatoresAtivo {
		t.Fatalf("2FA: %v", err)
	}
	novo := "outra@exemplo.ao"
	if err := s.AtualizarDados(nil, nil, nil, &novo, nil); err != nil {
		t.Fatal(err)
	}
	if s.EmailVerificado || s.DoisFatoresAtivo {
		t.Fatalf("trocar o e-mail deve zerar verificação e 2FA: %+v", s.ContaPessoa)
	}
}

func TestProfessorVinculosSaoUnicosPorPar(t *testing.T) {
	acad := uuid.New()
	p := NewProfessor()
	if err := p.Criar(dadosValidos(), acad, "LUA20261", &acad); err != nil {
		t.Fatal(err)
	}
	mat := uuid.New()
	id, err := p.AtribuirVinculo("1A", mat, &acad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.AtribuirVinculo("1A", mat, &acad); err == nil {
		t.Error("o mesmo par não pode ser atribuído duas vezes ao mesmo professor")
	}
	if _, err := p.AtribuirVinculo("1A", uuid.New(), &acad); err != nil {
		t.Errorf("outra matéria na mesma turma é permitida: %v", err)
	}
	if err := p.RemoverVinculo(id, &acad, "x"); err != nil || p.TemVinculo("1A", mat) {
		t.Fatalf("remover vínculo: %v", err)
	}
	if err := p.RemoverVinculo(uuid.New(), &acad, "x"); err == nil {
		t.Error("remover vínculo inexistente deveria falhar")
	}
	p.Status = StatusContaInativo
	if _, err := p.AtribuirVinculo("2B", mat, &acad); err == nil {
		t.Error("professor inativo não recebe vínculos")
	}
}

func TestRelacaoDoEncarregado(t *testing.T) {
	s := func(v string) *string { return &v }
	for _, ok := range []struct{ rel, outro *string }{{nil, nil}, {s("pai"), nil}, {s("mae"), nil}, {s("irmao"), nil}, {s("irma"), nil}, {s("OUTRO"), s("tio materno")}} {
		if _, _, err := ValidarRelacao(ok.rel, ok.outro); err != nil {
			t.Errorf("%v/%v deveria ser aceita: %v", ok.rel, ok.outro, err)
		}
	}
	for nome, c := range map[string]struct{ rel, outro *string }{
		"outro sem texto":           {s("outro"), nil},
		"outro com texto em branco": {s("outro"), s("  ")},
		"texto sem relação 'outro'": {s("pai"), s("tio")},
		"texto sem nenhuma relação": {nil, s("tio")},
		"relação inventada":         {s("vizinho"), nil},
		"texto longo demais":        {s("outro"), s(strings.Repeat("x", 61))},
	} {
		if _, _, err := ValidarRelacao(c.rel, c.outro); err == nil {
			t.Errorf("%s deveria ser recusada", nome)
		}
	}
}

func TestEncarregadoUmEducandoNaoSeVinculaDuasVezesERecarrega(t *testing.T) {
	e := NewEncarregado()
	if err := e.Criar(dadosValidos(), nil); err != nil {
		t.Fatal(err)
	}
	est := uuid.New()
	rel := "pai"
	if err := e.VincularEducando(est, "ABC1234", &rel, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.VincularEducando(est, "ABC1234", &rel, nil, nil); err == nil {
		t.Error("vincular o mesmo educando duas vezes deveria falhar")
	}
	// Recarrega do ledger.
	r := NewEncarregado()
	for _, ev := range e.GetUncommittedEvents() {
		if err := r.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.Educandos) != 1 || r.Educandos[est].Relacao == nil || *r.Educandos[est].Relacao != "pai" {
		t.Fatalf("educandos recarregados: %+v", r.Educandos)
	}
	if err := r.DesvincularEducando(est, MotivoDesvinculoEstudante, nil); err != nil || len(r.Educandos) != 0 {
		t.Fatalf("desvincular: %v", err)
	}
	if err := r.DesvincularEducando(est, MotivoDesvinculoEstudante, nil); err == nil {
		t.Error("desvincular quem não é educando deveria falhar")
	}
}
