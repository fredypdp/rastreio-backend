package turmas

import (
	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/jobs"
)

func CriarTurmaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeCriarTurmaBatch, 200)
}

func AdicionarEstudanteBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeAdicionarEstudanteBatch, 1000)
}

func AtivarTurmaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeAtivarTurmaBatch, 500)
}

func DesativarTurmaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeDesativarTurmaBatch, 500)
}

func AtualizarDadosTurmaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeAtualizarDadosTurmaBatch, 500)
}

func DeletarTurmaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeDeletarTurmaBatch, 500)
}

func RemoverEstudanteTurmaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeRemoverEstudanteTurmaBatch, 1000)
}
