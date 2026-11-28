package cadastros

import (
	"errors"
	"testing"
)

func TestValidarValorServico(t *testing.T) {
	ok := []float64{0.01, 15, 19.99, 5000, MaxValorServico}
	for _, v := range ok {
		if err := ValidarValorServico("preco", v); err != nil {
			t.Errorf("%v deveria valer: %v", v, err)
		}
	}
	ruins := []float64{0, -1, 15.999, 0.001, MaxValorServico + 1}
	for _, v := range ruins {
		if _, ok := EhErroRegra(ValidarValorServico("preco", v)); !ok {
			t.Errorf("%v deveria ser recusado como erro de regra", v)
		}
	}
}

func TestNormalizarMeses(t *testing.T) {
	m, err := NormalizarMeses("m", []int{9, 1, 12})
	if err != nil || len(m) != 3 || m[0] != 1 || m[2] != 12 {
		t.Fatalf("ordenação: %v %v", m, err)
	}
	for _, ruim := range [][]int{{0}, {13}, {1, 1}, {-2}} {
		if _, err := NormalizarMeses("m", ruim); err == nil {
			t.Errorf("%v deveria ser recusado", ruim)
		}
	}
}

func TestValidarHorarios(t *testing.T) {
	bom := []Horario{{1, "08:00", "09:00"}, {1, "09:00", "10:00"}, {7, "00:00", "23:59"}}
	if err := ValidarHorarios(bom); err != nil {
		t.Fatalf("horários válidos (encostados) recusados: %v", err)
	}
	ruins := map[string][]Horario{
		"dia 0":               {{0, "08:00", "09:00"}},
		"dia 8":               {{8, "08:00", "09:00"}},
		"fim antes":           {{2, "10:00", "09:00"}},
		"fim igual":           {{2, "10:00", "10:00"}},
		"formato":             {{2, "8:00", "09:00"}},
		"hora 24":             {{2, "24:00", "24:30"}},
		"cruzamento":          {{3, "08:00", "09:30"}, {3, "09:00", "10:00"}},
		"cruza fora de ordem": {{3, "09:00", "10:00"}, {3, "08:00", "09:30"}},
	}
	for nome, hs := range ruins {
		if err := ValidarHorarios(hs); err == nil {
			t.Errorf("%s deveria ser recusado", nome)
		}
	}
}

func TestTransporteGeralBloqueiaRotaComValorProprio(t *testing.T) {
	tr := NovoTransporte("ACA1")
	rota, err := tr.NovaRota("Rota 1", "06:30", ModoValor, 5000, []int{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	tr.Rotas = append(tr.Rotas, *rota)

	// Preço geral com rota que tem preço próprio: conflito (409).
	err = tr.DefinirPrecoGeral(ModoValor, 4000)
	if _, ok := EhErroConflito(err); !ok {
		t.Fatalf("esperava conflito, veio %v", err)
	}
	// Prazo geral com rota que tem prazo próprio: conflito.
	if _, ok := EhErroConflito(tr.DefinirPrazoGeral(ModoMeses, []int{1})); !ok {
		t.Fatal("prazo geral deveria conflitar com a rota")
	}
	// Limpar ('nenhum') nunca conflita.
	if err := tr.DefinirPrecoGeral(ModoNenhum, 0); err != nil {
		t.Fatal(err)
	}
}

func TestTransporteRotaNaoDefineOQueOGeralJaDefine(t *testing.T) {
	tr := NovoTransporte("ACA1")
	if err := tr.DefinirPrecoGeral(ModoValor, 3000); err != nil {
		t.Fatal(err)
	}
	if err := tr.DefinirPrazoGeral(ModoMeses, []int{3, 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.NovaRota("A", "07:00", ModoValor, 100, nil); err == nil {
		t.Error("rota com preço próprio deveria ser recusada com preço geral definido")
	}
	if _, err := tr.NovaRota("A", "07:00", "", 0, []int{5}); err == nil {
		t.Error("rota com prazo próprio deveria ser recusada com prazo geral definido")
	}
	r, err := tr.NovaRota("A", "07:00", "", 0, nil)
	if err != nil {
		t.Fatalf("rota sem valores próprios deve valer: %v", err)
	}
	tr.Rotas = append(tr.Rotas, *r)
	tr.Metodos = []string{"REF"}
	tr.Rotas[0].Veiculos = []Veiculo{{Nome: "V1", Ativo: true}}
	res := tr.Resolver(&tr.Rotas[0])
	if !res.Configurado() || res.Preco != 3000 || len(res.Meses) != 2 {
		t.Fatalf("resolução pelo geral: %+v", res)
	}
}

func TestTransporteResolverPrecoDaRotaEFaltas(t *testing.T) {
	tr := NovoTransporte("ACA1")
	r, _ := tr.NovaRota("A", "07:00", ModoValor, 2500, []int{1, 2, 3})
	r.Veiculos = []Veiculo{{Nome: "V1", Ativo: true}}
	tr.Metodos = []string{"REF"}
	res := tr.Resolver(r)
	if !res.Configurado() || res.Preco != 2500 || res.Gratuito || !res.MesCobrado(2) || res.MesCobrado(4) {
		t.Fatalf("resolução pela rota: %+v", res)
	}
	// Sem veículo ativo, a rota não fica disponível.
	r.Veiculos[0].Ativo = false
	if res := tr.Resolver(r); res.Configurado() {
		t.Fatal("rota sem veículo ativo não pode ser configurada")
	}
	// Rota sem preço nem prazo definidos (e sem geral): faltas listadas.
	vazia, _ := tr.NovaRota("B", "08:00", "", 0, nil)
	vazia.Veiculos = []Veiculo{{Nome: "V", Ativo: true}}
	if res := tr.Resolver(vazia); res.Configurado() || len(res.Problemas) < 2 {
		t.Fatalf("faltas: %+v", res.Problemas)
	}
	// Prazo geral gratuito: ninguém é cobrado em nenhuma rota.
	_ = tr.DefinirPrazoGeral(ModoGratuito, nil)
	g := tr.Resolver(r)
	r.Veiculos[0].Ativo = true
	g = tr.Resolver(r)
	if !g.Gratuito || g.MesCobrado(1) {
		t.Fatalf("prazo geral gratuito: %+v", g)
	}
}

func TestAtividadeRegras(t *testing.T) {
	disp := Disponibilidade{}
	hs := []Horario{{2, "14:00", "15:00"}}
	if _, err := NovaAtividade("A", "Judo", "Ana", false, false, 3000, []int{1, 2}, TaxaInscricao{}, []string{"REF"}, disp, hs); err != nil {
		t.Fatalf("atividade paga válida: %v", err)
	}
	if _, err := NovaAtividade("A", "Judo", "Ana", false, true, 0, nil, TaxaInscricao{}, nil, disp, hs); err != nil {
		t.Fatalf("atividade gratuita válida: %v", err)
	}
	ruins := map[string]func() error{
		"gratuita com preço": func() error {
			_, e := NovaAtividade("A", "J", "A", false, true, 10, nil, TaxaInscricao{}, nil, disp, nil)
			return e
		},
		"gratuita com meses": func() error {
			_, e := NovaAtividade("A", "J", "A", false, true, 0, []int{1}, TaxaInscricao{}, nil, disp, nil)
			return e
		},
		"paga sem meses": func() error {
			_, e := NovaAtividade("A", "J", "A", false, false, 10, nil, TaxaInscricao{}, []string{"REF"}, disp, nil)
			return e
		},
		"paga sem métodos": func() error {
			_, e := NovaAtividade("A", "J", "A", false, false, 10, []int{1}, TaxaInscricao{}, nil, disp, nil)
			return e
		},
		"sem instrutor": func() error {
			_, e := NovaAtividade("A", "J", " ", false, true, 0, nil, TaxaInscricao{}, nil, disp, nil)
			return e
		},
		"taxa sem métodos": func() error {
			_, e := NovaAtividade("A", "J", "A", false, true, 0, nil, TaxaInscricao{Valor: 500}, nil, disp, nil)
			return e
		},
		"métodos sem taxa": func() error {
			_, e := NovaAtividade("A", "J", "A", false, true, 0, nil, TaxaInscricao{Metodos: []string{"REF"}}, nil, disp, nil)
			return e
		},
		"método inválido": func() error {
			_, e := NovaAtividade("A", "J", "A", false, false, 10, []int{1}, TaxaInscricao{}, []string{"XYZ"}, disp, nil)
			return e
		},
		"preço com 3 casas": func() error {
			_, e := NovaAtividade("A", "J", "A", false, false, 10.123, []int{1}, TaxaInscricao{}, []string{"REF"}, disp, nil)
			return e
		},
	}
	for nome, f := range ruins {
		err := f()
		if err == nil {
			t.Errorf("%s deveria ser recusado", nome)
			continue
		}
		var regra *ErroRegra
		if !errors.As(err, &regra) {
			t.Errorf("%s: esperava erro de regra, veio %T", nome, err)
		}
	}
}

func TestAtividadeColetivaExigeTurmaAtiva(t *testing.T) {
	a, err := NovaAtividade("A", "Coro", "Ana", true, true, 0, nil, TaxaInscricao{}, nil, Disponibilidade{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Resolver().Configurado() {
		t.Fatal("coletiva sem turma não pode estar configurada")
	}
	a.Turmas = []TurmaAtividade{{Nome: "T1", Ativo: true}}
	if !a.Resolver().Configurado() {
		t.Fatalf("com turma ativa deveria estar configurada: %v", a.Resolver().Problemas)
	}
}

func TestMesesRemovidos(t *testing.T) {
	got := MesesRemovidos([]int{1, 2, 3, 4}, []int{2, 4, 5})
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("removidos = %v", got)
	}
	if len(MesesRemovidos([]int{1}, []int{1, 2})) != 0 {
		t.Fatal("adicionar mês não remove nada")
	}
}

func TestValidarDisponibilidade(t *testing.T) {
	if err := ValidarDisponibilidade(Disponibilidade{AnosAcademicos: []string{"7_ano_fundamental"}}); err != nil {
		t.Fatalf("ano fundamental válido: %v", err)
	}
	if err := ValidarDisponibilidade(Disponibilidade{AnosAcademicos: []string{"1_ano_medio"}}); err == nil {
		t.Fatal("médio não entra em anos_academicos_disponiveis")
	}
	if err := ValidarDisponibilidade(Disponibilidade{Cursos: []string{"nao-uuid|1_ano_medio"}}); err == nil {
		t.Fatal("curso_id inválido deveria ser recusado")
	}
}
