package avaliacoes

import (
	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/jobs"
)

// ============================================================================
// Academia — avaliação final
// ============================================================================

func RegistrarAvaliacaoFinalBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeRegistrarAvaliacaoFinalBatch, 1000)
}
