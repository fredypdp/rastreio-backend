package utils

import "strings"

// Regras compartilhadas sobre o nível escolar do estudante (Plano de
// Atualizações, Partes 4 e 5): o encarregado só existe para quem está, ou
// esteve, no nível escolar (fundamental/médio) e só age enquanto o estudante
// não concluiu esse nível.

func preenchido(s *string) bool { return s != nil && strings.TrimSpace(*s) != "" }

// FinalizouNivelEscolar diz se o estudante concluiu o nível escolar: o ensino
// médio está "finalizado" ou já há matrícula no ensino superior. A partir daí
// o encarregado passa a ter só leitura do período escolar e o próprio
// estudante pode desvincular-se dele.
func FinalizouNivelEscolar(statusEscolarMedio string, anoSuperior *string) bool {
	return statusEscolarMedio == "finalizado" || preenchido(anoSuperior)
}

// TemNivelEscolar diz se o estudante pertence (ou pertenceu) ao nível escolar,
// isto é, se tem encarregado. Estudantes só do superior não têm.
func TemNivelEscolar(anoFundamental, anoMedio *string, statusFundamental, statusMedio string) bool {
	if preenchido(anoFundamental) || preenchido(anoMedio) {
		return true
	}
	return statusFundamental == "em_andamento" || statusFundamental == "finalizado" ||
		statusMedio == "em_andamento" || statusMedio == "finalizado"
}
