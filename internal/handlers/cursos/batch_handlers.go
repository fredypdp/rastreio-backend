package cursos

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/utils"
)

// =============================================================================
// POST /academia/curso/batch — limite 50
// =============================================================================

func CriarCursoBatch(c *gin.Context) {
	type ReqCurso struct {
		Nome           string   `json:"nome"`
		Type           string   `json:"type"`
		AnosAcademicos []string `json:"anos_academicos"`
		Periodos       []string `json:"periodos"`
	}
	var reqs []ReqCurso
	if err := c.ShouldBindJSON(&reqs); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve ser um array de cursos", nil)
		return
	}
	if err := apoio.ValidarTamanhoBatch(len(reqs), 50); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, err.Error(), nil)
		return
	}

	results := make([]apoio.BatchItemResult, 0, len(reqs))
	for i, req := range reqs {
		rc := apoio.NewFakeContext(c)
		apoio.SetJSONBody(rc, req)
		CriarCurso(rc)
		results = append(results, apoio.ExtractResult(rc, i))
	}

	c.JSON(apoio.BatchHTTPStatus(results), apoio.NewBatchResponse(results))
}

// =============================================================================
// PUT /academia/curso/ativar/batch
// PUT /academia/curso/desativar/batch — limite 50
// =============================================================================

func AtivarCursoBatch(c *gin.Context) {
	apoio.RunIDParamBatch(c, 50, func(rc *gin.Context) { AtivarCurso(rc) })
}

func DesativarCursoBatch(c *gin.Context) {
	apoio.RunIDParamBatch(c, 50, func(rc *gin.Context) { DesativarCurso(rc) })
}

// =============================================================================
// DELETE /academia/curso/batch — limite 50
// =============================================================================

func DeletarCursoBatch(c *gin.Context) {
	type ReqDeletar struct {
		ID     string `json:"id"`
		Motivo string `json:"motivo"`
	}
	var reqs []ReqDeletar
	if err := c.ShouldBindJSON(&reqs); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve ser um array", nil)
		return
	}
	if err := apoio.ValidarTamanhoBatch(len(reqs), 50); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, err.Error(), nil)
		return
	}

	results := make([]apoio.BatchItemResult, 0, len(reqs))
	for i, req := range reqs {
		rc := apoio.NewFakeContext(c)
		rc.Params = gin.Params{gin.Param{Key: "id", Value: req.ID}}
		if req.Motivo != "" {
			apoio.SetJSONBody(rc, gin.H{"motivo": req.Motivo})
		}
		DeletarCurso(rc)
		results = append(results, apoio.ExtractResult(rc, i))
	}

	c.JSON(apoio.BatchHTTPStatus(results), apoio.NewBatchResponse(results))
}
