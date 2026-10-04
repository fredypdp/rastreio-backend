package handlers

import (
	"fmt"

	"spuri/internal/domain/aggregates"
)

// Reprovação por faltas: quando a academia definiu o limite de faltas e ligou a
// reprovação por faltas, ao ultrapassar o limite (total > limite) numa matéria e
// período, SÓ a nota abaixo é lida como 0 no cálculo automático da avaliação final:
//   - ensino escolar (fundamental e médio): a nota do professor do período;
//   - ensino superior: o exame final.
//
// As demais notas não são afetadas. Para mudar qual nota é zerada, altere só estas
// duas constantes.
const (
	categoriaZeroPorFaltasEscolar  = "nota_professor"
	categoriaZeroPorFaltasSuperior = "exame_final"
)

func categoriaZeroPorFaltas(tipoEnsino string) string {
	if tipoEnsino == "superior" {
		return categoriaZeroPorFaltasSuperior
	}
	return categoriaZeroPorFaltasEscolar
}

// aplicarZeroPorFaltas zera, em notas, cada referência (categoria, período) da
// fórmula cuja categoria seja a indicada e cujo total de faltas no período
// ultrapasse o limite. Devolve o que foi zerado (uma entrada por período).
// Períodos que a fórmula não referencia não geram nada.
func aplicarZeroPorFaltas(formula string, notas map[string]map[string][]float64, categoria string, totaisPorPeriodo map[string]int, limite int) ([]aggregates.NotaZeradaPorFaltas, error) {
	refs, err := referenciasFormulaAvaliacao(formula)
	if err != nil {
		return nil, fmt.Errorf("zero por faltas: %w", err)
	}
	var zeradas []aggregates.NotaZeradaPorFaltas
	for _, ref := range refs {
		if ref.Categoria != categoria {
			continue
		}
		total, ok := totaisPorPeriodo[ref.Periodo]
		if !ok || total <= limite {
			continue
		}
		if notas[ref.Categoria] == nil {
			notas[ref.Categoria] = map[string][]float64{}
		}
		notas[ref.Categoria][ref.Periodo] = []float64{0}
		zeradas = append(zeradas, aggregates.NotaZeradaPorFaltas{
			Categoria:    ref.Categoria,
			Periodo:      ref.Periodo,
			TotalFaltas:  total,
			LimiteFaltas: limite,
		})
	}
	return zeradas, nil
}
