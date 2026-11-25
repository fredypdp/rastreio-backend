package aggregates

import "testing"

func TestTipoAgrupamentoDoNivel(t *testing.T) {
	casos := map[string]string{
		"4_ano_medio":       "grupo",
		" 4_ano_medio ":     "grupo",
		"3_ano_medio":       "turma",
		"1_ano_medio":       "turma",
		"7_ano_fundamental": "turma",
		"1_semestre":        "turma",
	}
	for nivel, esperado := range casos {
		if got := TipoAgrupamentoDoNivel(nivel); got != esperado {
			t.Fatalf("TipoAgrupamentoDoNivel(%q) = %q, esperado %q", nivel, got, esperado)
		}
	}
}
