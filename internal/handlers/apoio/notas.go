package apoio

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/domain/aggregates"
	"spuri/internal/projections"
	"spuri/internal/utils"
)

func ValidarObservacao(observacao *string) error {
	if observacao != nil && len([]rune(*observacao)) > 2000 {
		return fmt.Errorf("observacao deve ter no máximo 2000 caracteres")
	}
	return nil
}

func ValidarMatriculaEmAndamento(estudante *projections.EstudanteDTO) error {
	switch InferirTipoEnsinoDoEstudante(estudante) {
	case "superior":
		if estudante.StatusSuperior != "em_andamento" {
			return fmt.Errorf("matrícula superior do estudante não está em andamento")
		}
	case "medio":
		if estudante.StatusEscolarMedio != "em_andamento" {
			return fmt.Errorf("matrícula no ensino médio do estudante não está em andamento")
		}
	default:
		if estudante.StatusEscolarFundamental != "em_andamento" {
			return fmt.Errorf("matrícula no " + utils.RotuloEnsinoFundamentalGenerico + " do estudante não está em andamento")
		}
	}
	return nil
}

// ============================================================================
// Helpers internos
// ============================================================================

// resolverPeriodosValidos retorna a lista de periodos aceitos para o registro
// de uma nota:
//   - tipo="escolar"  -> 3 trimestres fixos
//   - tipo="superior" -> periodos do curso ao qual a materia pertence
func ResolverPeriodosValidos(c *gin.Context, tipo string, cursoID *uuid.UUID) ([]string, error) {
	utils.Debugf("[nota-debug] resolverPeriodosValidos: tipo=%s curso_id=%v", tipo, cursoID)
	switch tipo {
	case aggregates.TipoEscolar:
		utils.Debugf("[nota-debug] tipo escolar: usando períodos fixos %v", aggregates.PeriodosEscolar)
		return aggregates.PeriodosEscolar, nil

	case aggregates.TipoSuperior:
		if cursoID == nil {
			return nil, fmt.Errorf("materia do tipo 'superior' deve estar associada a um curso")
		}
		cursosProj := GetCursosProjection(c)
		cursoDTO, err := cursosProj.GetByID(*cursoID)
		if err != nil {
			return nil, fmt.Errorf("erro ao buscar curso da materia: %w", err)
		}
		if cursoDTO == nil {
			return nil, fmt.Errorf("curso associado a materia nao encontrado")
		}
		if len(cursoDTO.Periodos) == 0 {
			return nil, fmt.Errorf(
				"o curso '%s' nao possui periodos configurados. "+
					"Atualize o curso via PUT /academia/cursos/:id com o campo 'periodos'",
				cursoDTO.Nome,
			)
		}
		utils.Debugf("[nota-debug] tipo superior: períodos do curso '%s' -> %v", cursoDTO.Nome, cursoDTO.Periodos)
		return cursoDTO.Periodos, nil

	default:
		utils.Debugf("[nota-debug] tipo de nota inválido ao resolver períodos: tipo=%s", tipo)
		return nil, fmt.Errorf("tipo de nota invalido: '%s'. Use 'escolar' ou 'superior'", tipo)
	}
}

// inferirAnoAcademicoParaNota determina o ano_academico correto para o registro de nota.
//   - anoEscolarEstudante != nil e nao vazio -> fundamental -> retorna anoEscolarEstudante
//   - caso contrario (medio ou superior) -> retorna o primeiro ano da matéria;
//     matérias do médio podem ter mais de um ano e, quando o estudante possui
//     ano médio preenchido, a função valida pertencimento antes de retornar.
func InferirAnoAcademicoParaNota(
	anoEscolarEstudante *string,
	nivelMateria []string,
	nomeMateria string,
	anoEscolarMedioEstudante ...*string,
) (string, error) {
	if anoEscolarEstudante != nil && strings.TrimSpace(*anoEscolarEstudante) != "" {
		anoEstudante := strings.TrimSpace(*anoEscolarEstudante)
		for _, anoMateria := range nivelMateria {
			if strings.TrimSpace(anoMateria) == anoEstudante {
				return anoEstudante, nil
			}
		}

		return "", fmt.Errorf(
			"o estudante está no ano acadêmico '%s', que não faz parte da matéria '%s' (anos permitidos: %v)",
			anoEstudante,
			nomeMateria,
			nivelMateria,
		)
	}

	if len(anoEscolarMedioEstudante) > 0 && anoEscolarMedioEstudante[0] != nil && strings.TrimSpace(*anoEscolarMedioEstudante[0]) != "" {
		anoEstudante := strings.TrimSpace(*anoEscolarMedioEstudante[0])
		for _, anoMateria := range nivelMateria {
			if strings.TrimSpace(anoMateria) == anoEstudante {
				return anoEstudante, nil
			}
		}
		return "", fmt.Errorf(
			"o estudante está no ano acadêmico médio '%s', que não faz parte da matéria '%s' (anos permitidos: %v)",
			anoEstudante, nomeMateria, nivelMateria,
		)
	}

	if len(nivelMateria) == 0 {
		return "", fmt.Errorf(
			"a materia '%s' nao possui anos_academicos definidos; impossivel inferir ano_academico",
			nomeMateria,
		)
	}
	return strings.TrimSpace(nivelMateria[0]), nil
}

// inferirAnoAcademicoFaltas é idêntico a inferirAnoAcademicoParaNota —
// reutilizado no handler de faltas para manter a mesma lógica.
func InferirAnoAcademicoFaltas(
	anoEscolarEstudante *string,
	nivelMateria []string,
	nomeMateria string,
	anoEscolarMedioEstudante ...*string,
) (string, error) {
	return InferirAnoAcademicoParaNota(anoEscolarEstudante, nivelMateria, nomeMateria, anoEscolarMedioEstudante...)
}
