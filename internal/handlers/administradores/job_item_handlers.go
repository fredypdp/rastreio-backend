package administradores

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/utils"
)

func AtivarAdminJobItem(c *gin.Context) {
	var req struct {
		ID string `json:"id"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil || req.ID == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter ao menos {id}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "id", Value: req.ID}}
	AtivarAdmin(c)
}

func DesativarAdminJobItem(c *gin.Context) {
	var req struct {
		ID string `json:"id"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil || req.ID == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter ao menos {id}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "id", Value: req.ID}}
	DesativarAdmin(c)
}
