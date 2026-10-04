package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/middleware"
	"spuri/internal/projections"
	"spuri/internal/utils"
)

func getFaltasConfigProjection(c *gin.Context) *projections.FaltasConfiguracaoProjection {
	return projections.NewFaltasConfiguracaoProjection(getDbClient(c))
}

// GetConfiguracaoFaltas devolve o limite de faltas e a reprovação por faltas da
// academia autenticada. Sem configuração salva, devolve limite nulo e reprovação desligada.
// Rota: GET /academia/faltas/configuracao
func GetConfiguracaoFaltas(c *gin.Context) {
	academiaID, _ := middleware.GetUserID(c)
	academiaDTO, err := getAcademiaProjection(c).GetByID(academiaID)
	if err != nil || academiaDTO == nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	cfg, err := getFaltasConfigProjection(c).GetByAcademia(academiaDTO.CodigoAcademia)
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
// faltas não pode ficar ligada). A resposta devolve os valores salvos, porque a
// projeção é atualizada de forma assíncrona.
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

	academiaDTO, err := getAcademiaProjection(c).GetByID(academiaID)
	if err != nil || academiaDTO == nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	repository := getRepository(c)
	aggID := aggregates.ConfiguracaoFaltasAggregateID(academiaDTO.CodigoAcademia)
	var cfg *aggregates.ConfiguracaoFaltas
	existe, err := repository.Exists(aggID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if existe {
		loaded, err := repository.Load(aggID, "ConfiguracaoFaltas")
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		var ok bool
		cfg, ok = loaded.(*aggregates.ConfiguracaoFaltas)
		if !ok {
			utils.RespondWithInternalError(c, fmt.Errorf("tipo inesperado ao carregar ConfiguracaoFaltas"))
			return
		}
	} else {
		cfg = aggregates.NewConfiguracaoFaltas()
		cfg.SetID(aggID)
	}

	if err := cfg.Definir(academiaDTO.CodigoAcademia, req.LimiteFaltasPorPeriodo, req.ReprovacaoPorFaltas, academiaID); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	audit := db.AuditContext{UserID: academiaID.String(), UserType: "academia", IP: c.ClientIP()}
	if err := repository.SaveWithAudit(cfg, audit); err != nil {
		utils.RespondWithInternalError(c, err)
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
