package handlers

import "testing"

const formulaEscolarRegularTeste = "(((([nota_professor,1_trimestre]+[prova_trimestral,1_trimestre])/2)+(([nota_professor,2_trimestre]+[prova_trimestral,2_trimestre])/2)+(([nota_professor,3_trimestre]+[prova_trimestral,3_trimestre])/2))/3)"

func notasEscolaresTeste(valor float64) map[string]map[string][]float64 {
	m := map[string]map[string][]float64{"nota_professor": {}, "prova_trimestral": {}}
	for _, p := range []string{"1_trimestre", "2_trimestre", "3_trimestre"} {
		m["nota_professor"][p] = []float64{valor}
		m["prova_trimestral"][p] = []float64{valor}
	}
	return m
}

func TestCategoriaZeroPorFaltas(t *testing.T) {
	if got := categoriaZeroPorFaltas("fundamental"); got != "nota_professor" {
		t.Fatalf("fundamental: %q", got)
	}
	if got := categoriaZeroPorFaltas("medio"); got != "nota_professor" {
		t.Fatalf("medio: %q", got)
	}
	if got := categoriaZeroPorFaltas("superior"); got != "exame_final" {
		t.Fatalf("superior: %q", got)
	}
}

func TestAplicarZeroPorFaltasEscolarZeraSoANotaDoProfessorDoPeriodoExcedido(t *testing.T) {
	notas := notasEscolaresTeste(10)
	zeradas, err := aplicarZeroPorFaltas(formulaEscolarRegularTeste, notas, "nota_professor", map[string]int{"2_trimestre": 6, "1_trimestre": 2}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(zeradas) != 1 || zeradas[0].Periodo != "2_trimestre" || zeradas[0].Categoria != "nota_professor" || zeradas[0].TotalFaltas != 6 || zeradas[0].LimiteFaltas != 5 {
		t.Fatalf("esperado só nota_professor/2_trimestre (6 > 5), obteve %+v", zeradas)
	}
	if notas["nota_professor"]["2_trimestre"][0] != 0 {
		t.Fatal("nota_professor do 2º trimestre deveria ser 0")
	}
	if notas["nota_professor"]["1_trimestre"][0] != 10 || notas["nota_professor"]["3_trimestre"][0] != 10 {
		t.Fatal("nota_professor de outros períodos não pode mudar")
	}
	for _, p := range []string{"1_trimestre", "2_trimestre", "3_trimestre"} {
		if notas["prova_trimestral"][p][0] != 10 {
			t.Fatalf("prova_trimestral de %s não pode ser afetada", p)
		}
	}
}

func TestAplicarZeroPorFaltasNoLimiteExatoNaoZera(t *testing.T) {
	notas := notasEscolaresTeste(10)
	zeradas, err := aplicarZeroPorFaltas(formulaEscolarRegularTeste, notas, "nota_professor", map[string]int{"2_trimestre": 5}, 5)
	if err != nil || len(zeradas) != 0 {
		t.Fatalf("total == limite não ultrapassa o limite: %+v %v", zeradas, err)
	}
	if notas["nota_professor"]["2_trimestre"][0] != 10 {
		t.Fatal("nota não deveria mudar")
	}
}

func TestAplicarZeroPorFaltasCriaNotaAusenteComoZero(t *testing.T) {
	notas := map[string]map[string][]float64{}
	zeradas, err := aplicarZeroPorFaltas(formulaEscolarRegularTeste, notas, "nota_professor", map[string]int{"3_trimestre": 9}, 5)
	if err != nil || len(zeradas) != 1 {
		t.Fatalf("esperado 1 período zerado: %+v %v", zeradas, err)
	}
	if got := notas["nota_professor"]["3_trimestre"]; len(got) != 1 || got[0] != 0 {
		t.Fatalf("nota ausente deveria virar 0, obteve %v", got)
	}
}

func TestAplicarZeroPorFaltasFormulaSemACategoriaNaoFazNada(t *testing.T) {
	// regra de exame de recurso e PAP não usam nota_professor
	for _, formula := range []string{"[exame_recurso,3_trimestre]", "[nota_pap,3_trimestre]"} {
		notas := map[string]map[string][]float64{}
		zeradas, err := aplicarZeroPorFaltas(formula, notas, "nota_professor", map[string]int{"3_trimestre": 50}, 5)
		if err != nil || len(zeradas) != 0 || len(notas) != 0 {
			t.Fatalf("%s: não deveria alterar nada: %+v %v %v", formula, zeradas, notas, err)
		}
	}
}

func TestAplicarZeroPorFaltasSuperiorZeraSoOExameFinal(t *testing.T) {
	formula := "(([nota_continua,1_semestre]*0.4)+([exame_final,1_semestre]*0.6))"
	notas := map[string]map[string][]float64{
		"nota_continua": {"1_semestre": {16}},
		"exame_final":   {"1_semestre": {14}},
	}
	zeradas, err := aplicarZeroPorFaltas(formula, notas, categoriaZeroPorFaltas("superior"), map[string]int{"1_semestre": 12}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(zeradas) != 1 || zeradas[0].Categoria != "exame_final" || zeradas[0].Periodo != "1_semestre" {
		t.Fatalf("esperado exame_final/1_semestre, obteve %+v", zeradas)
	}
	if notas["exame_final"]["1_semestre"][0] != 0 || notas["nota_continua"]["1_semestre"][0] != 16 {
		t.Fatalf("só o exame final deveria virar 0: %+v", notas)
	}
	got, err := calcularFormulaAvaliacao(formula, notas)
	if err != nil {
		t.Fatal(err)
	}
	if got < 6.39 || got > 6.41 {
		t.Fatalf("16*0.4 + 0*0.6 = 6.4, obteve %v", got)
	}
}

func TestAplicarZeroPorFaltasSuperiorPeriodoSemFaltasAcimaDoLimite(t *testing.T) {
	formula := "(([nota_continua,1_semestre]*0.4)+([exame_final,1_semestre]*0.6))"
	notas := map[string]map[string][]float64{"exame_final": {"1_semestre": {14}}}
	zeradas, _ := aplicarZeroPorFaltas(formula, notas, "exame_final", map[string]int{"1_semestre": 10, "2_semestre": 99}, 10)
	if len(zeradas) != 0 || notas["exame_final"]["1_semestre"][0] != 14 {
		t.Fatalf("exame final não deveria mudar: %+v %+v", zeradas, notas)
	}
}
