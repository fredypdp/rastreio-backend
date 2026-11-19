package notas

import (
	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/jobs"
)

// ============================================================================
// Academia — notas
// ============================================================================

func RegistrarNotaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeRegistrarNotaBatch, 2000)
}

func CriarCategoriaNotaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeCriarCategoriaNotaBatch, 500)
}

func DeletarCategoriaNotaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeDeletarCategoriaNotaBatch, 500)
}
