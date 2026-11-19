package academias

import (
	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/jobs"
)

// ============================================================================
// Admin — academias
// ============================================================================

// RegisterAcademiaBatchAsync enfileira o cadastro de até 500 academias.
// A versão síncrona (/batch) tem limite de 5 por causa do bcrypt.
// A versão assíncrona não tem esse gargalo pois processa em background.
func RegisterAcademiaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeRegisterAcademiaBatch, 500)
}

func AtivarAcademiaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeAtivarAcademiaBatch, 500)
}

func DesativarAcademiaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeDesativarAcademiaBatch, 500)
}

func AtualizarDadosAcademiaBatchAsync(c *gin.Context) {
	apoio.EnqueueAsyncBatch(c, jobs.JobTypeAtualizarDadosAcademiaBatch, 200)
}
