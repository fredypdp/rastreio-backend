package handlers

import (
	"testing"

	"spuri/internal/projections"
)

// elegivelParaServicoExtra é a regra única usada tanto para o catálogo do
// estudante (ListarServicosExtrasCatalogoEstudante) quanto para aceitar ou
// rejeitar uma inscrição (SolicitarServicoExtra). strPtr já existe em
// turmas_handler_test.go, no mesmo pacote — reaproveitado aqui, não
// redeclarado.
func TestElegivelParaServicoExtra(t *testing.T) {
	casos := []struct {
		nome      string
		servico   projections.ServicoExtraDTO
		estudante projections.EstudanteDTO
		esperado  bool
	}{
		{
			nome:      "sem nenhuma restrição — disponível para qualquer estudante",
			servico:   projections.ServicoExtraDTO{},
			estudante: projections.EstudanteDTO{},
			esperado:  true,
		},
		{
			nome:      "restrito a anos fundamentais — estudante no ano certo",
			servico:   projections.ServicoExtraDTO{AnosAcademicosDisponiveis: []string{"7_ano_fundamental"}},
			estudante: projections.EstudanteDTO{AnoEscolar: strPtr("7_ano_fundamental")},
			esperado:  true,
		},
		{
			nome:      "restrito a anos fundamentais — estudante em outro ano",
			servico:   projections.ServicoExtraDTO{AnosAcademicosDisponiveis: []string{"7_ano_fundamental"}},
			estudante: projections.EstudanteDTO{AnoEscolar: strPtr("8_ano_fundamental")},
			esperado:  false,
		},
		{
			nome:      "restrito a anos fundamentais — estudante do médio (sem ano fundamental)",
			servico:   projections.ServicoExtraDTO{AnosAcademicosDisponiveis: []string{"7_ano_fundamental"}},
			estudante: projections.EstudanteDTO{},
			esperado:  false,
		},
		{
			nome:      "restrito a um curso do médio — estudante no curso e ano certos",
			servico:   projections.ServicoExtraDTO{CursosDisponiveis: []string{"curso-1|2_ano_medio"}},
			estudante: projections.EstudanteDTO{CursoMedioID: strPtr("curso-1"), AnoEscolarMedio: strPtr("2_ano_medio")},
			esperado:  true,
		},
		{
			nome:      "restrito a um curso do médio — estudante no curso certo, ano diferente",
			servico:   projections.ServicoExtraDTO{CursosDisponiveis: []string{"curso-1|2_ano_medio"}},
			estudante: projections.EstudanteDTO{CursoMedioID: strPtr("curso-1"), AnoEscolarMedio: strPtr("1_ano_medio")},
			esperado:  false,
		},
		{
			nome:      "restrito a um curso superior — estudante no curso e ano certos",
			servico:   projections.ServicoExtraDTO{CursosDisponiveis: []string{"curso-2|3_ano_superior"}},
			estudante: projections.EstudanteDTO{CursoSuperiorID: strPtr("curso-2"), AnoSuperior: strPtr("3_ano_superior")},
			esperado:  true,
		},
		{
			nome:      "restrito a curso do médio — estudante do superior no mesmo curso/ano nominal (tipos não se misturam na prática, id+sufixo do ano já bastam para diferenciar)",
			servico:   projections.ServicoExtraDTO{CursosDisponiveis: []string{"curso-1|2_ano_medio"}},
			estudante: projections.EstudanteDTO{CursoSuperiorID: strPtr("curso-1"), AnoSuperior: strPtr("2_ano_superior")},
			esperado:  false,
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := elegivelParaServicoExtra(&c.servico, &c.estudante); got != c.esperado {
				t.Fatalf("elegivelParaServicoExtra() = %v, queria %v", got, c.esperado)
			}
		})
	}
}
