package aggregates

import (
	"fmt"
	"strings"
	"unicode"
)

// Funções puras de turmas e grupos. O agregado Turma deixou de existir (Parte
// 2B, regra 17): as turmas são CRUD em internal/cadastros. Estas funções
// continuam aqui porque a camada de projeções e os handlers as usam sem
// depender do pacote cadastros.

// NormalizarCodigoTurma normaliza codigo_turma para identificadores seguros.
// Espaços internos viram underscore; espaços antes/depois são descartados.
// Caracteres especiais diferentes de underscore são rejeitados.
func NormalizarCodigoTurma(codigoTurma string) (string, error) {
	codigoTurma = strings.TrimSpace(codigoTurma)
	if codigoTurma == "" {
		return "", fmt.Errorf("codigo_turma é obrigatório")
	}

	var b strings.Builder
	ultimoUnderscore := false
	for _, r := range codigoTurma {
		switch {
		case unicode.IsSpace(r):
			if !ultimoUnderscore {
				b.WriteRune('_')
				ultimoUnderscore = true
			}
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
			ultimoUnderscore = false
		case r == '_':
			b.WriteRune(r)
			ultimoUnderscore = true
		default:
			return "", fmt.Errorf("codigo_turma deve conter apenas letras, números, espaços ou underscore; espaços são convertidos para '_'")
		}
	}

	normalizado := strings.Trim(b.String(), "_")
	if normalizado == "" {
		return "", fmt.Errorf("codigo_turma deve conter pelo menos uma letra ou número")
	}
	return normalizado, nil
}

// ── Comandos ──────────────────────────────────────────────────────────────────

// NivelGruposTrabalho é o único ano acadêmico em que os estudantes não são
// organizados em turmas: são separados em grupos, cada um com um trabalho de
// tema próprio. O agregado continua sendo Turma (mesmo stream, mesmas rotas);
// apenas a apresentação muda para "grupo" e passa a existir tema_trabalho.
const NivelGruposTrabalho = "4_ano_medio"

const MaxTemaTrabalho = 200

// TipoAgrupamentoDoNivel devolve "grupo" para o 4º ano médio e "turma" nos demais.
func TipoAgrupamentoDoNivel(nivel string) string {
	if strings.TrimSpace(nivel) == NivelGruposTrabalho {
		return "grupo"
	}
	return "turma"
}

// NormalizarTemaTrabalho valida o tema contra o nível efetivo. Devolve nil quando
// o tema vem vazio (sem tema) e erro quando o nível não é de grupos.
func NormalizarTemaTrabalho(nivel string, tema *string) (*string, error) {
	if tema == nil {
		return nil, nil
	}
	valor := strings.TrimSpace(*tema)
	if valor == "" {
		return nil, nil
	}
	if strings.TrimSpace(nivel) != NivelGruposTrabalho {
		return nil, fmt.Errorf("tema_trabalho só é permitido em grupos do 4º ano médio")
	}
	if len([]rune(valor)) > MaxTemaTrabalho {
		return nil, fmt.Errorf("tema_trabalho deve ter no máximo %d caracteres", MaxTemaTrabalho)
	}
	return &valor, nil
}
