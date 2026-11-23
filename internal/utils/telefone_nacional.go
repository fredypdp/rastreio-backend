package utils

import (
	"fmt"
	"regexp"
	"strings"
)

var telefoneNacionalRegex = regexp.MustCompile(`^9\d{8}$`)

// NormalizarTelefoneNacional devolve o móvel angolano no formato nacional de
// 9 dígitos iniciado por 9 (923456789), aceitando espaços, hífens, parênteses
// e os prefixos +244 / 244 / 0. É o formato que o envio de SMS exige.
func NormalizarTelefoneNacional(v string) (string, error) {
	clean := strings.TrimSpace(v)
	clean = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(clean)
	if strings.HasPrefix(clean, "+244") {
		clean = strings.TrimPrefix(clean, "+244")
	} else if strings.HasPrefix(clean, "244") && len(clean) == 12 {
		clean = strings.TrimPrefix(clean, "244")
	}
	if strings.HasPrefix(clean, "0") && len(clean) == 10 {
		clean = strings.TrimPrefix(clean, "0")
	}
	if !telefoneNacionalRegex.MatchString(clean) {
		return "", fmt.Errorf("deve ser um número móvel angolano válido, no formato nacional de 9 dígitos iniciado por 9 (ex.: 923456789), com ou sem prefixo +244")
	}
	return clean, nil
}
