package academias

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/utils"
)

func AtivarAcademiaJobItem(c *gin.Context) {
	var req struct {
		Codigo         string `json:"codigo"`
		CodigoAcademia string `json:"codigo_academia"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo}", nil)
		return
	}
	codigo := apoio.FirstNonEmptyTrimmed(req.Codigo, req.CodigoAcademia)
	if codigo == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "codigo", Value: codigo}}
	AtivarAcademia(c)
}

func DesativarAcademiaJobItem(c *gin.Context) {
	var req struct {
		Codigo         string `json:"codigo"`
		CodigoAcademia string `json:"codigo_academia"`
		Motivo         string `json:"motivo"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo, motivo}", nil)
		return
	}
	codigo := apoio.FirstNonEmptyTrimmed(req.Codigo, req.CodigoAcademia)
	if codigo == "" || req.Motivo == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo, motivo}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "codigo", Value: codigo}}
	DesativarAcademia(c)
}

func AtualizarDadosAcademiaJobItem(c *gin.Context) {
	AtualizarDadosAcademia(c)
}
