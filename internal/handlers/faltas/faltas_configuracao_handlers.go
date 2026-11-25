package faltas

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"spuri/internal/cadastros"
	"spuri/internal/db"
	"spuri/internal/handlers/apoio"
	"spuri/internal/middleware"
	"spuri/internal/projections"
	"spuri/internal/utils"
)

// GetConfiguracaoFaltas devolve o limite de faltas e a reprovação por faltas da
// academia autenticada. Sem configuração salva, devolve limite nulo e reprovação desligada.
// Rota: GET /academia/faltas/configuracao
func GetConfiguracaoFaltas(c *gin.Context) {
	academiaID, _ := middleware.GetUserID(c)
	academiaDTO, err := apoio.GetAcademiaProjection(c).GetByID(academiaID)
	if err != nil || academiaDTO == nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	cfg, err := apoio.GetFaltasConfigProjection(c).GetByAcademia(academiaDTO.CodigoAcademia)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if cfg == nil {
		cfg = &projections.ConfiguracaoFaltasDTO{CodigoAcademia: academiaDTO.CodigoAcademia}
	}
	c.JSON(http.StatusOK, gin.H{"data": cfg})
}

// DefinirConfiguracaoFaltas substitui a configuração de faltas da academia.
// limite_faltas_por_periodo omitido ou null = sem limite (e a reprovação por
// faltas não pode ficar ligada). A configuração é CRUD (Tarefa 135): grava e
// audita na mesma transação, e a leitura seguinte já vê o valor novo.
// Rota: PUT /academia/faltas/configuracao
func DefinirConfiguracaoFaltas(c *gin.Context) {
	academiaID, _ := middleware.GetUserID(c)

	var req struct {
		LimiteFaltasPorPeriodo *int `json:"limite_faltas_por_periodo"`
		ReprovacaoPorFaltas    bool `json:"reprovacao_por_faltas"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	academiaDTO, err := apoio.GetAcademiaProjection(c).GetByID(academiaID)
	if err != nil || academiaDTO == nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	cfg, err := cadastros.NovaConfiguracaoFaltas(academiaDTO.CodigoAcademia, req.LimiteFaltasPorPeriodo, req.ReprovacaoPorFaltas, academiaID)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	audit := db.AuditContext{UserID: academiaID.String(), UserType: "academia", IP: c.ClientIP(), Ator: apoio.AtorDaRequisicao(c)}
	if err := cadastros.NovoFaltasConfiguracaoRepo(apoio.GetDbClient(c)).Definir(c.Request.Context(), audit, cfg); err != nil {
		apoio.RespondErroCadastro(c, err, "configuração de faltas")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "configuração de faltas salva com sucesso",
		"data": projections.ConfiguracaoFaltasDTO{
			CodigoAcademia:         academiaDTO.CodigoAcademia,
			LimiteFaltasPorPeriodo: cfg.LimiteFaltasPorPeriodo,
			ReprovacaoPorFaltas:    cfg.ReprovacaoPorFaltas,
			AtualizadoEm:           &cfg.AtualizadoEm,
		},
	})
}
