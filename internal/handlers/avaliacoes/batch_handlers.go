package avaliacoes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/utils"
)

// =============================================================================
// POST /academia/notas-aluno/batch — limite 200
// =============================================================================

// =============================================================================
// POST /academia/faltas-aluno/batch — limite 200
// =============================================================================

// =============================================================================
// POST /academia/avaliacao-final/batch — limite 100
// =============================================================================

func RegistrarAvaliacaoFinalBatch(c *gin.Context) {
	type ReqAvaliacao struct {
		CodigoEstudante     string  `json:"codigo_estudante"`
		TipoEnsino          string  `json:"tipo_ensino"`
		AnoAcademicoAtual   string  `json:"nivel_ano_academico_atual"`
		ProximoAnoAcademico *string `json:"proximo_ano_academico"`
		Aprovado            bool    `json:"aprovado"`
		Observacao          *string `json:"observacao"`
	}
	var reqs []ReqAvaliacao
	if err := c.ShouldBindJSON(&reqs); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve ser um array de avaliações", nil)
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
		RegistrarAvaliacaoFinal(rc)
		results = append(results, apoio.ExtractResult(rc, i))
	}

	c.JSON(apoio.BatchHTTPStatus(results), apoio.NewBatchResponse(results))
}
