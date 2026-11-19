package turmas

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/handlers/apoio"
	"spuri/internal/utils"
)

// =============================================================================
// POST /academia/turma/batch — limite 50
// =============================================================================

func CriarTurmaBatch(c *gin.Context) {
	type ReqTurma struct {
		CodigoTurma string     `json:"codigo_turma"`
		Nivel       string     `json:"nivel"`
		CursoID     *uuid.UUID `json:"curso_id"`
		Turno       string     `json:"turno"`
	}
	var reqs []ReqTurma
	if err := c.ShouldBindJSON(&reqs); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve ser um array de turmas", nil)
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
		CriarTurma(rc)
		results = append(results, apoio.ExtractResult(rc, i))
	}

	c.JSON(apoio.BatchHTTPStatus(results), apoio.NewBatchResponse(results))
}

// =============================================================================
// PUT /academia/turma/ativar/batch
// PUT /academia/turma/desativar/batch — limite 50
// =============================================================================

func AtivarTurmaBatch(c *gin.Context) {
	apoio.RunCodigoParamBatch(c, 50, func(rc *gin.Context) { AtivarTurma(rc) })
}

func DesativarTurmaBatch(c *gin.Context) {
	apoio.RunCodigoParamBatch(c, 50, func(rc *gin.Context) { DesativarTurma(rc) })
}

// =============================================================================
// DELETE /academia/turma/batch — limite 50
// =============================================================================

func DeletarTurmaBatch(c *gin.Context) {
	type ReqDeletar struct {
		Codigo string `json:"codigo"`
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
		rc.Params = gin.Params{gin.Param{Key: "codigo", Value: req.Codigo}}
		if req.Motivo != "" {
			apoio.SetJSONBody(rc, gin.H{"motivo": req.Motivo})
		}
		DeletarTurma(rc)
		results = append(results, apoio.ExtractResult(rc, i))
	}

	c.JSON(apoio.BatchHTTPStatus(results), apoio.NewBatchResponse(results))
}

// =============================================================================
// POST /academia/turma/estudante/batch — limite 100
// =============================================================================

func AdicionarEstudanteBatch(c *gin.Context) {
	type ReqAdicionar struct {
		CodigoTurma     string `json:"codigo_turma"`
		CodigoEstudante string `json:"codigo_estudante"`
	}
	var reqs []ReqAdicionar
	if err := c.ShouldBindJSON(&reqs); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve ser um array", nil)
		return
	}
	if err := apoio.ValidarTamanhoBatch(len(reqs), 100); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, err.Error(), nil)
		return
	}

	results := make([]apoio.BatchItemResult, 0, len(reqs))
	for i, req := range reqs {
		if req.CodigoTurma == "" || req.CodigoEstudante == "" {
			results = append(results, apoio.BatchErr(i,
				fmt.Errorf("codigo_turma e codigo_estudante são obrigatórios")))
			continue
		}
		rc := apoio.NewFakeContext(c)
		rc.Params = gin.Params{gin.Param{Key: "codigo", Value: req.CodigoTurma}}
		apoio.SetJSONBody(rc, gin.H{"codigo_estudante": req.CodigoEstudante})
		AdicionarEstudanteATurma(rc)
		results = append(results, apoio.ExtractResult(rc, i))
	}

	c.JSON(apoio.BatchHTTPStatus(results), apoio.NewBatchResponse(results))
}

// =============================================================================
// DELETE /academia/turma/estudante/batch — limite 100
// =============================================================================

func RemoverEstudanteBatch(c *gin.Context) {
	type ReqRemover struct {
		CodigoTurma     string `json:"codigo_turma"`
		CodigoEstudante string `json:"codigo_estudante"`
	}
	var reqs []ReqRemover
	if err := c.ShouldBindJSON(&reqs); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve ser um array", nil)
		return
	}
	if err := apoio.ValidarTamanhoBatch(len(reqs), 100); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, err.Error(), nil)
		return
	}

	results := make([]apoio.BatchItemResult, 0, len(reqs))
	for i, req := range reqs {
		if req.CodigoTurma == "" || req.CodigoEstudante == "" {
			results = append(results, apoio.BatchErr(i,
				fmt.Errorf("codigo_turma e codigo_estudante são obrigatórios")))
			continue
		}
		rc := apoio.NewFakeContext(c)
		rc.Params = gin.Params{
			gin.Param{Key: "codigo", Value: req.CodigoTurma},
			gin.Param{Key: "codigoEstudante", Value: req.CodigoEstudante},
		}
		RemoverEstudanteDaTurma(rc)
		results = append(results, apoio.ExtractResult(rc, i))
	}

	c.JSON(apoio.BatchHTTPStatus(results), apoio.NewBatchResponse(results))
}
