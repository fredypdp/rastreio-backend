package notas

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/utils"
)

func CriarCategoriaNotaJobItem(c *gin.Context) {
	CriarCategoriaNota(c)
}

func DeletarCategoriaNotaJobItem(c *gin.Context) {
	var req struct {
		Codigo string `json:"codigo"`
		Nome   string `json:"nome"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo}", nil)
		return
	}

	codigo := strings.TrimSpace(req.Codigo)
	if codigo == "" {
		codigo = strings.TrimSpace(req.Nome)
	}
	if codigo == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo}", nil)
		return
	}

	c.Params = gin.Params{gin.Param{Key: "codigo", Value: codigo}}
	DeletarCategoriaNota(c)
}
