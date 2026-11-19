package apoio

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/projections"
)

type filtrosEstudante struct {
	AnoLectivos           []string `json:"-"`
	AnoAcademicos         []string `json:"-"`
	CursoIDs              []string `json:"-"`
	Periodos              []string `json:"-"`
	MateriasDisciplinares []string `json:"-"`
	CodigosAcademia       []string `json:"-"`
	Categorias            []string `json:"-"`
}

func ParseFiltrosRegistrosEstudante(c *gin.Context, includeCategoria bool) (filtrosEstudante, error) {
	f := filtrosEstudante{
		AnoLectivos:           ParseMultiValueQueryParam(c, "ano_letivo"),
		AnoAcademicos:         ParseMultiValueQueryParam(c, "ano_academico"),
		CursoIDs:              ParseMultiValueQueryParam(c, "curso_id"),
		Periodos:              ParseMultiValueQueryParam(c, "periodo"),
		MateriasDisciplinares: ParseMultiValueQueryParam(c, "materia_disciplinar_id"),
		CodigosAcademia:       ParseMultiValueQueryParam(c, "codigo_academia"),
	}
	if includeCategoria {
		f.Categorias = ParseMultiValueQueryParam(c, "categoria")
	}

	for _, cursoID := range f.CursoIDs {
		if _, err := uuid.Parse(cursoID); err != nil {
			return f, fmt.Errorf("curso_id inválido")
		}
	}
	for _, materiaID := range f.MateriasDisciplinares {
		if _, err := uuid.Parse(materiaID); err != nil {
			return f, fmt.Errorf("materia_disciplinar_id inválido")
		}
	}

	return f, nil
}

func MatchesFiltroString(filtro []string, value string) bool {
	return len(filtro) == 0 || slices.Contains(filtro, value)
}

type MateriaMeta struct {
	CursoID string `json:"-"`
	periodo string
}

func GetMateriaMeta(materiasProj *projections.MateriasProjection, cache map[string]MateriaMeta, materiaID string) (MateriaMeta, error) {
	if meta, ok := cache[materiaID]; ok {
		return meta, nil
	}

	id, err := uuid.Parse(materiaID)
	if err != nil {
		cache[materiaID] = MateriaMeta{}
		return cache[materiaID], nil
	}

	m, err := materiasProj.GetByID(id)
	if err != nil || m == nil {
		cache[materiaID] = MateriaMeta{}
		return cache[materiaID], err
	}

	meta := MateriaMeta{}
	if m.CursoID != nil {
		meta.CursoID = m.CursoID.String()
	}
	if m.Periodo != nil {
		meta.periodo = *m.Periodo
	}
	cache[materiaID] = meta
	return meta, nil
}

func ParseMultiValueQueryParam(c *gin.Context, key string) []string {
	rawValues := c.QueryArray(key)
	if len(rawValues) == 0 {
		return nil
	}

	parsedValues := make([]string, 0, len(rawValues))
	seen := make(map[string]struct{}, len(rawValues))
	for _, raw := range rawValues {
		for _, part := range strings.Split(raw, ",") {
			value := strings.TrimSpace(part)
			if value == "" {
				continue
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			parsedValues = append(parsedValues, value)
		}
	}
	return parsedValues
}
