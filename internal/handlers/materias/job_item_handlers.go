package materias

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/utils"
)

func AtivarMateriaJobItem(c *gin.Context) {
	var req struct {
		ID string `json:"id"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil || req.ID == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter ao menos {id}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "id", Value: req.ID}}
	AtivarMateria(c)
}

func DesativarMateriaJobItem(c *gin.Context) {
	var req struct {
		ID string `json:"id"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil || req.ID == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter ao menos {id}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "id", Value: req.ID}}
	DesativarMateria(c)
}

func AtualizarDadosMateriaJobItem(c *gin.Context) {
	var req struct {
		ID string `json:"id"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil || req.ID == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter ao menos {id}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "id", Value: req.ID}}
	AtualizarDadosMateria(c)
}

func DeletarMateriaJobItem(c *gin.Context) {
	var req struct {
		ID string `json:"id"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil || req.ID == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter ao menos {id}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "id", Value: req.ID}}
	DeletarMateria(c)
}
