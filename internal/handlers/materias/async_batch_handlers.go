package materias

import (
	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/jobs"
)

func CriarMateriaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeCriarMateriaBatch, 500)
}

func AtivarMateriaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeAtivarMateriaBatch, 1000)
}

func DesativarMateriaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeDesativarMateriaBatch, 1000)
}

func AtualizarDadosMateriaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeAtualizarDadosMateriaBatch, 1000)
}

func DeletarMateriaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeDeletarMateriaBatch, 1000)
}
