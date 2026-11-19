package turmas

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/utils"
)

func AdicionarEstudanteATurmaJobItem(c *gin.Context) {
	var req struct {
		CodigoTurma     string `json:"codigo_turma"`
		CodigoEstudante string `json:"codigo_estudante"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil || req.CodigoTurma == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo_turma, codigo_estudante}", nil)
		return
	}
	if req.CodigoEstudante == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo_turma, codigo_estudante}", nil)
		return
	}

	c.Params = gin.Params{gin.Param{Key: "codigo", Value: req.CodigoTurma}}
	AdicionarEstudanteATurma(c)
}

func AtivarTurmaJobItem(c *gin.Context) {
	var req struct {
		Codigo      string `json:"codigo"`
		CodigoTurma string `json:"codigo_turma"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo_turma}", nil)
		return
	}
	codigo := apoio.FirstNonEmptyTrimmed(req.Codigo, req.CodigoTurma)
	if codigo == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo_turma}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "codigo", Value: codigo}}
	AtivarTurma(c)
}

func DesativarTurmaJobItem(c *gin.Context) {
	var req struct {
		Codigo      string `json:"codigo"`
		CodigoTurma string `json:"codigo_turma"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo_turma}", nil)
		return
	}
	codigo := apoio.FirstNonEmptyTrimmed(req.Codigo, req.CodigoTurma)
	if codigo == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo_turma}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "codigo", Value: codigo}}
	DesativarTurma(c)
}

func AtualizarDadosTurmaJobItem(c *gin.Context) {
	var req struct {
		Codigo      string `json:"codigo"`
		CodigoTurma string `json:"codigo_turma"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo_turma}", nil)
		return
	}
	codigo := apoio.FirstNonEmptyTrimmed(req.Codigo, req.CodigoTurma)
	if codigo == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo_turma}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "codigo", Value: codigo}}
	AtualizarDadosTurma(c)
}

func DeletarTurmaJobItem(c *gin.Context) {
	var req struct {
		Codigo      string `json:"codigo"`
		CodigoTurma string `json:"codigo_turma"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo_turma}", nil)
		return
	}
	codigo := apoio.FirstNonEmptyTrimmed(req.Codigo, req.CodigoTurma)
	if codigo == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo_turma}", nil)
		return
	}
	c.Params = gin.Params{gin.Param{Key: "codigo", Value: codigo}}
	DeletarTurma(c)
}

func RemoverEstudanteTurmaJobItem(c *gin.Context) {
	var req struct {
		CodigoTurma     string `json:"codigo_turma"`
		CodigoEstudante string `json:"codigo_estudante"`
	}
	if err := apoio.BindJobItemWithoutLosingBody(c, &req); err != nil || req.CodigoTurma == "" || req.CodigoEstudante == "" {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {codigo_turma, codigo_estudante}", nil)
		return
	}
	c.Params = gin.Params{
		gin.Param{Key: "codigo", Value: req.CodigoTurma},
		gin.Param{Key: "codigoEstudante", Value: req.CodigoEstudante},
	}
	RemoverEstudanteDaTurma(c)
}
