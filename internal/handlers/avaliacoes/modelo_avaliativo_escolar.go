package avaliacoes

import (
	"spuri/internal/handlers/apoio"
	"spuri/internal/projections"
)

func anexarCategoriasEscolaresFixas(categorias []projections.CategoriaNotaDTO, academia *projections.AcademiaDTO) []interface{} {
	out := make([]interface{}, 0, len(categorias)+8)
	for _, cat := range categorias {
		out = append(out, cat)
	}
	return append(out, apoio.CategoriasEscolaresFixasDaAcademia(nil, academia)...)
}
