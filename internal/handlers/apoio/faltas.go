package apoio

import (
	"github.com/gin-gonic/gin"
	"spuri/internal/projections"
)

func GetFaltasConfigProjection(c *gin.Context) *projections.FaltasConfiguracaoProjection {
	return projections.NewFaltasConfiguracaoProjection(GetDbClient(c))
}
