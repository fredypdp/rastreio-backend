package academias

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/utils"
)

// =============================================================================
// POST /dominis/academia/register/batch — limite 50
//
// Permite que um admin registe até 50 academias numa única chamada HTTP.
// Cada item do array segue a mesma estrutura de POST /dominis/academia/register.
// A resposta indica sucesso/falha por item, com o codigo_academia gerado para
// cada academia criada com sucesso.
//
// Requer: Auth admin + role >= gerente
// =============================================================================

func RegisterAcademiaBatch(c *gin.Context) {
	var reqs []RegisterAcademiaRequest
	if err := c.ShouldBindJSON(&reqs); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve ser um array de academias", nil)
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
		RegisterAcademia(rc)
		results = append(results, apoio.ExtractResult(rc, i))
	}

	c.JSON(apoio.BatchHTTPStatus(results), apoio.NewBatchResponse(results))
}

// =============================================================================
// PUT /dominis/academia/ativar/batch — limite 50
//
// Activa até 50 academias de uma vez.
// Body: [{ "codigo": "LDA20261" }, { "codigo": "BGU20261" }, ...]
//
// Requer: Auth admin + role >= adm
// =============================================================================

func AtivarAcademiaBatch(c *gin.Context) {
	apoio.RunCodigoParamBatch(c, 50, func(rc *gin.Context) { AtivarAcademia(rc) })
}

// =============================================================================
// PUT /dominis/academia/desativar/batch — limite 50
//
// Desactiva até 50 academias de uma vez.
// O campo motivo é OBRIGATÓRIO por item — sem motivo, o item falha com 400.
//
// Body: [{ "codigo": "LDA20261", "motivo": "encerramento voluntário" }, ...]
//
// Requer: Auth admin + role >= adm
// =============================================================================

func DesativarAcademiaBatch(c *gin.Context) {
	type ReqDesativar struct {
		Codigo         string `json:"codigo"`
		CodigoAcademia string `json:"codigo_academia"`
		Motivo         string `json:"motivo"`
	}
	var reqs []ReqDesativar
	if err := c.ShouldBindJSON(&reqs); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve ser um array de {codigo, motivo}", nil)
		return
	}
	if err := apoio.ValidarTamanhoBatch(len(reqs), 50); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, err.Error(), nil)
		return
	}

	results := make([]apoio.BatchItemResult, 0, len(reqs))
	for i, req := range reqs {
		req.Codigo = apoio.FirstNonEmptyTrimmed(req.Codigo, req.CodigoAcademia)
		req.Motivo = strings.TrimSpace(req.Motivo)
		if req.Codigo == "" {
			results = append(results, apoio.BatchErr(i, fmt.Errorf("codigo é obrigatório")))
			continue
		}
		if req.Motivo == "" {
			results = append(results, apoio.BatchErr(i, fmt.Errorf("motivo é obrigatório para desativar a academia %q", req.Codigo)))
			continue
		}
		rc := apoio.NewFakeContext(c)
		rc.Params = gin.Params{gin.Param{Key: "codigo", Value: req.Codigo}}
		apoio.SetJSONBody(rc, gin.H{"motivo": req.Motivo})
		DesativarAcademia(rc)
		results = append(results, apoio.ExtractResult(rc, i))
	}

	c.JSON(apoio.BatchHTTPStatus(results), apoio.NewBatchResponse(results))
}
