package servicosextras

import (
	"testing"

	"spuri/internal/cadastros"
	"spuri/internal/handlers/apoio"
	"spuri/internal/handlers/testsupport"
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
		servico   cadastros.Disponibilidade
		estudante projections.EstudanteDTO
		esperado  bool
	}{
		{
			nome:      "sem nenhuma restrição — disponível para qualquer estudante",
			servico:   cadastros.Disponibilidade{},
			estudante: projections.EstudanteDTO{},
			esperado:  true,
		},
		{
			nome:      "restrito a anos fundamentais — estudante no ano certo",
			servico:   cadastros.Disponibilidade{AnosAcademicos: []string{"7_ano_fundamental"}},
			estudante: projections.EstudanteDTO{AnoEscolar: testsupport.StrPtr("7_ano_fundamental")},
			esperado:  true,
		},
		{
			nome:      "restrito a anos fundamentais — estudante em outro ano",
			servico:   cadastros.Disponibilidade{AnosAcademicos: []string{"7_ano_fundamental"}},
			estudante: projections.EstudanteDTO{AnoEscolar: testsupport.StrPtr("8_ano_fundamental")},
			esperado:  false,
		},
		{
			nome:      "restrito a anos fundamentais — estudante do médio (sem ano fundamental)",
			servico:   cadastros.Disponibilidade{AnosAcademicos: []string{"7_ano_fundamental"}},
			estudante: projections.EstudanteDTO{},
			esperado:  false,
		},
		{
			nome:      "restrito a um curso do médio — estudante no curso e ano certos",
			servico:   cadastros.Disponibilidade{Cursos: []string{"curso-1|2_ano_medio"}},
			estudante: projections.EstudanteDTO{CursoMedioID: testsupport.StrPtr("curso-1"), AnoEscolarMedio: testsupport.StrPtr("2_ano_medio")},
			esperado:  true,
		},
		{
			nome:      "restrito a um curso do médio — estudante no curso certo, ano diferente",
			servico:   cadastros.Disponibilidade{Cursos: []string{"curso-1|2_ano_medio"}},
			estudante: projections.EstudanteDTO{CursoMedioID: testsupport.StrPtr("curso-1"), AnoEscolarMedio: testsupport.StrPtr("1_ano_medio")},
			esperado:  false,
		},
		{
			nome:      "restrito a um curso superior — estudante no curso e ano certos",
			servico:   cadastros.Disponibilidade{Cursos: []string{"curso-2|3_ano_superior"}},
			estudante: projections.EstudanteDTO{CursoSuperiorID: testsupport.StrPtr("curso-2"), AnoSuperior: testsupport.StrPtr("3_ano_superior")},
			esperado:  true,
		},
		{
			nome:      "restrito a curso do médio — estudante do superior no mesmo curso/ano nominal (tipos não se misturam na prática, id+sufixo do ano já bastam para diferenciar)",
			servico:   cadastros.Disponibilidade{Cursos: []string{"curso-1|2_ano_medio"}},
			estudante: projections.EstudanteDTO{CursoSuperiorID: testsupport.StrPtr("curso-1"), AnoSuperior: testsupport.StrPtr("2_ano_superior")},
			esperado:  false,
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := apoio.ElegivelParaServicoExtra(c.servico, &c.estudante); got != c.esperado {
				t.Fatalf("elegivelParaServicoExtra() = %v, queria %v", got, c.esperado)
			}
		})
	}
}
