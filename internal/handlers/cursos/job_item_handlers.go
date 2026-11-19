package cursos

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/utils"
)

func AtivarCursoJobItem(c *gin.Context) {
	var req struct {
		ID string `json:"id"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil || req.ID == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter ao menos {id}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "id", Value: req.ID}}
	AtivarCurso(c)
}

func DesativarCursoJobItem(c *gin.Context) {
	var req struct {
		ID string `json:"id"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil || req.ID == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter ao menos {id}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "id", Value: req.ID}}
	DesativarCurso(c)
}

func AtualizarDadosCursoJobItem(c *gin.Context) {
	var req struct {
		ID string `json:"id"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil || req.ID == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter ao menos {id}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "id", Value: req.ID}}
	AtualizarDadosCurso(c)
}

func DeletarCursoJobItem(c *gin.Context) {
	var req struct {
		ID string `json:"id"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil || req.ID == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter ao menos {id}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "id", Value: req.ID}}
	DeletarCurso(c)
}
