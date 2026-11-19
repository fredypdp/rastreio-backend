package materias

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/handlers/apoio"
	"spuri/internal/utils"
)

// =============================================================================
// POST /academia/materia/batch — limite 100
// =============================================================================

func CriarMateriaBatch(c *gin.Context) {
	type ReqMateria struct {
		Nome           string     `json:"nome"`
		Type           string     `json:"type"`
		AnosAcademicos []string   `json:"anos_academicos"`
		CursoID        *uuid.UUID `json:"curso_id"`
	}
	var reqs []ReqMateria
	if err := c.ShouldBindJSON(&reqs); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve ser um array de matérias", nil)
		return
	}
	if err := apoio.ValidarTamanhoBatch(len(reqs), 100); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, err.Error(), nil)
		return
	}

	results := make([]apoio.BatchItemResult, 0, len(reqs))
	for i, req := range reqs {
		rc := apoio.NewFakeContext(c)
		apoio.SetJSONBody(rc, req)
		CriarMateria(rc)
		results = append(results, apoio.ExtractResult(rc, i))
	}

	c.JSON(apoio.BatchHTTPStatus(results), apoio.NewBatchResponse(results))
}

// =============================================================================
// PUT /academia/materia/ativar/batch
// PUT /academia/materia/desativar/batch
// DELETE /academia/materia/batch — limite 100
// =============================================================================

func AtivarMateriaBatch(c *gin.Context) {
	apoio.RunIDParamBatch(c, 100, func(rc *gin.Context) { AtivarMateria(rc) })
}

func DesativarMateriaBatch(c *gin.Context) {
	apoio.RunIDParamBatch(c, 100, func(rc *gin.Context) { DesativarMateria(rc) })
}

func DeletarMateriaBatch(c *gin.Context) {
	apoio.RunIDParamBatch(c, 100, func(rc *gin.Context) { DeletarMateria(rc) })
}
