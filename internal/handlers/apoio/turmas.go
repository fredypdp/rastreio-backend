package apoio

import (
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/projections"
	"spuri/internal/utils"
)

var ErrTurmaNaoEncontradaParaVinculo = errors.New("turma não encontrada ou não pertence a esta academia")

// validarCompatibilidadeEstudanteTurma verifica se o estudante pode ser vinculado
// à turma com base no nível e curso.
func ValidarCompatibilidadeEstudanteTurma(
	_ interface{}, // reservado para compatibilidade futura
	_ interface{}, // reservado para compatibilidade futura
	academiaAnosAcademicos []string,
	nivel string,
	turmaCursoID *uuid.UUID,
	anoEscolar *string,
	anoEscolarMedio *string,
	anoSuperior *string,
	cursoMedioID *string,
	cursoSuperiorID *string,
) error {
	tipoEnsino := inferirTipoEnsinoPorNivel(nivel)

	switch tipoEnsino {
	case "fundamental":
		nivelValido := false
		for _, ano := range academiaAnosAcademicos {
			if ano == nivel {
				nivelValido = true
				break
			}
		}
		if !nivelValido {
			return fmt.Errorf(
				"o nível da turma '%s' não está configurado nos anos académicos desta academia",
				nivel,
			)
		}
		if anoEscolar == nil || *anoEscolar == "" {
			return fmt.Errorf(
				"estudante não possui ano escolar do "+utils.RotuloEnsinoFundamentalGenerico+" definido — configure o ano escolar antes de vincular à turma '%s'",
				nivel,
			)
		}
		if *anoEscolar != nivel {
			return fmt.Errorf(
				"estudante está na %s mas a turma é do nível %s",
				utils.RotuloClasseFundamental(*anoEscolar), utils.RotuloClasseFundamental(nivel),
			)
		}

	case "medio":
		if anoEscolarMedio == nil || *anoEscolarMedio == "" {
			return fmt.Errorf(
				"estudante não possui ano escolar médio definido — configure antes de vincular à turma '%s'",
				nivel,
			)
		}
		if *anoEscolarMedio != nivel {
			return fmt.Errorf(
				"estudante está no %s mas a turma é do nível %s",
				*anoEscolarMedio, nivel,
			)
		}
		if turmaCursoID == nil {
			return fmt.Errorf(
				"turma de nível médio '%s' não possui curso vinculado",
				nivel,
			)
		}
		if cursoMedioID == nil || *cursoMedioID == "" {
			return fmt.Errorf(
				"estudante não possui curso médio definido — configure antes de vincular à turma '%s'",
				nivel,
			)
		}
		if *cursoMedioID != turmaCursoID.String() {
			return fmt.Errorf(
				"o curso médio do estudante não corresponde ao curso da turma '%s'",
				nivel,
			)
		}

	case "superior":
		if anoSuperior == nil || *anoSuperior == "" {
			return fmt.Errorf(
				"estudante não possui ano superior definido — configure antes de vincular à turma '%s'",
				nivel,
			)
		}
		if *anoSuperior != nivel {
			return fmt.Errorf(
				"estudante está no %s mas a turma é do nível %s",
				*anoSuperior, nivel,
			)
		}
		if turmaCursoID == nil {
			return fmt.Errorf(
				"turma de nível superior '%s' não possui curso vinculado",
				nivel,
			)
		}
		if cursoSuperiorID == nil || *cursoSuperiorID == "" {
			return fmt.Errorf(
				"estudante não possui curso superior definido — configure antes de vincular à turma '%s'",
				nivel,
			)
		}
		if *cursoSuperiorID != turmaCursoID.String() {
			return fmt.Errorf(
				"o curso superior do estudante não corresponde ao curso da turma '%s'",
				nivel,
			)
		}

	default:
		return fmt.Errorf(
			"não foi possível determinar o tipo de ensino para o nível '%s' (use formato como '3_ano_fundamental', '1_ano_medio', '2_ano_superior')",
			nivel,
		)
	}

	return nil
}

// inferirTipoEnsinoPorNivel determina o tipo de ensino com base no formato do campo nivel da turma.
// Retorna "fundamental", "medio", "superior" ou "desconhecido".
func inferirTipoEnsinoPorNivel(nivel string) string {
	if len(nivel) > 16 && nivel[len(nivel)-16:] == "_ano_fundamental" {
		return "fundamental"
	}
	if len(nivel) > 9 && nivel[len(nivel)-9:] == "_ano_medio" {
		return "medio"
	}
	if len(nivel) > 12 && nivel[len(nivel)-12:] == "_ano_superior" {
		return "superior"
	}
	// Fallback para sufixos mais curtos
	for _, suf := range []string{"_ano_fundamental", "_ano_medio", "_ano_superior"} {
		if len(nivel) >= len(suf) && nivel[len(nivel)-len(suf):] == suf {
			switch suf {
			case "_ano_fundamental":
				return "fundamental"
			case "_ano_medio":
				return "medio"
			case "_ano_superior":
				return "superior"
			}
		}
	}
	return "desconhecido"
}

func VincularEstudanteATurma(
	c *gin.Context,
	academiaDTO *projections.AcademiaDTO,
	codigoEstudante string,
	anoEscolar, anoEscolarMedio, anoSuperior string,
	cursoMedioID, cursoSuperiorID string,
	codigoTurma string,
	ignorarChecagemDuplicidade bool,
	atuadoPor uuid.UUID,
) error {
	turmasProj := GetTurmasProjection(c)
	turmaDTO, err := turmasProj.GetByCodigoTurma(codigoTurma, academiaDTO.CodigoAcademia)
	if err != nil {
		return err
	}
	if turmaDTO == nil {
		return ErrTurmaNaoEncontradaParaVinculo
	}
	if turmaDTO.Status != "ativo" {
		return fmt.Errorf("turma '%s' está %s e não pode receber estudantes", codigoTurma, turmaDTO.Status)
	}

	if !ignorarChecagemDuplicidade {
		turmasAtuais, err := turmasProj.ListByEstudante(codigoEstudante, &academiaDTO.CodigoAcademia)
		if err != nil {
			return err
		}
		for _, t := range turmasAtuais {
			if t.ID != turmaDTO.ID && t.Status != "deletado" {
				return fmt.Errorf("estudante já pertence à turma '%s' e não pode ser vinculado a múltiplas turmas", t.CodigoTurma)
			}
		}
	}

	if err := ValidarCompatibilidadeEstudanteTurma(
		nil, nil,
		academiaDTO.AnosAcademicos,
		turmaDTO.Nivel,
		turmaDTO.CursoID,
		StringPtrIfNotBlank(anoEscolar),
		StringPtrIfNotBlank(anoEscolarMedio),
		StringPtrIfNotBlank(anoSuperior),
		StringPtrIfNotBlank(cursoMedioID),
		StringPtrIfNotBlank(cursoSuperiorID),
	); err != nil {
		return fmt.Errorf("estudante incompatível com esta turma: %w", err)
	}

	anoLectivo, err := ResolverAnoLetivoAcademia(academiaDTO.AnoLetivo, academiaDTO.CodigoAcademia)
	if err != nil {
		return err
	}

	audit := db.AuditContext{UserID: atuadoPor.String(), UserType: "academia", IP: c.ClientIP()}
	repository := GetRepository(c)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		agg, err := repository.Load(turmaDTO.ID, "Turma")
		if err != nil {
			return err
		}
		turma, ok := agg.(*aggregates.Turma)
		if !ok {
			return fmt.Errorf("erro ao converter agregado")
		}
		if err := turma.AdicionarEstudanteNoAnoLectivo(codigoEstudante, anoLectivo, atuadoPor); err != nil {
			return err
		}
		if err := repository.SaveWithAudit(turma, audit); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}
