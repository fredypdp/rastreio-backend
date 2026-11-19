package administradores

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"spuri/internal/monitoring"
)

// GetSystemMetrics retorna snapshot das métricas do sistema.
// Endpoint: GET /admin/metrics
// Requer: RequireAdmin()
func GetSystemMetrics(c *gin.Context) {
	metrics := monitoring.GetMetrics()
	snapshot := metrics.GetSnapshot()

	c.JSON(http.StatusOK, gin.H{
		"metrics": snapshot,
	})
}
