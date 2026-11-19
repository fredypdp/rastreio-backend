package cursos

import (
	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/jobs"
)

// ============================================================================
// Academia — cursos, matérias, turmas
// ============================================================================

func CriarCursoBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeCriarCursoBatch, 200)
}

func AtivarCursoBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeAtivarCursoBatch, 500)
}

func DesativarCursoBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeDesativarCursoBatch, 500)
}

func AtualizarDadosCursoBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeAtualizarDadosCursoBatch, 500)
}

func DeletarCursoBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeDeletarCursoBatch, 500)
}
