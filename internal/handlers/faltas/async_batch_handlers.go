package faltas

import (
	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/jobs"
)

// ============================================================================
// Academia — faltas
// ============================================================================

func RegistrarFaltasBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeRegistrarFaltasBatch, 2000)
}
