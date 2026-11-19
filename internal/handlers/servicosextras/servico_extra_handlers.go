package servicosextras

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/handlers/apoio"
	"spuri/internal/middleware"
	"spuri/internal/utils"
)

func toggle(c *gin.Context, on bool) {
	s, id, ok := apoio.LoadServico(c)
	if !ok {
		return
	}
	var e error
	if on {
		e = s.Reativar(id)
	} else {
		e = s.Desativar(id)
	}
	if e != nil {
		utils.RespondWithValidationError(c, e)
		return
	}
	if e = apoio.GetRepository(c).SaveWithAudit(s, db.AuditContext{UserID: id.String(), UserType: "academia", IP: c.ClientIP()}); e != nil {
		utils.RespondWithInternalError(c, e)
		return
	}
	c.JSON(200, gin.H{"data": apoio.ServicoExtraToJSON(s)})
}

func DesativarServicoExtra(c *gin.Context) { toggle(c, false) }

func ReativarServicoExtra(c *gin.Context) { toggle(c, true) }

// DeletarServicoExtra remove logicamente um serviço extra.
//
// Regras de negócio (checadas ANTES de delegar ao aggregate, mesmo padrão
// de DeletarCurso/DeletarCategoriaServico):
//  1. Serviço deve pertencer à academia autenticada (loadServico)
//  2. Serviço deve estar inativo (checado pelo aggregate em Deletar)
//  3. Não pode haver solicitações pendentes, aprovadas-pendentes-de-
//     pagamento ou vinculadas para este serviço
//
// Evento ServicoExtraDeletado gravado no ledger (auditável).
func DeletarServicoExtra(c *gin.Context) {
	s, id, ok := apoio.LoadServico(c)
	if !ok {
		return
	}
	var req struct {
		Motivo string `json:"motivo"` // opcional, recomendado para auditoria
	}
	_ = c.ShouldBindJSON(&req)

	ativas, err := apoio.GetSolicitacoesServicoExtraProjection(c).CountAtivasPorServico(s.GetID())
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if ativas > 0 {
		utils.RespondWithValidationError(c, fmt.Errorf("não é possível deletar: %d inscrição(ões) pendente(s) ou ativa(s) neste serviço", ativas))
		return
	}

	if err := s.Deletar(id, req.Motivo); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := apoio.GetRepository(c).SaveWithAudit(s, db.AuditContext{UserID: id.String(), UserType: "academia", IP: c.ClientIP()}); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "serviço extra deletado com sucesso"})
}

func GetServicoExtra(c *gin.Context) {
	id, e := uuid.Parse(c.Param("id"))
	if e != nil {
		utils.RespondWithValidationError(c, e)
		return
	}
	x, e := apoio.GetServicosExtrasProjection(c).GetByID(id)
	if e != nil || x == nil {
		utils.RespondWithNotFoundError(c, "serviço extra")
		return
	}
	if role, _ := middleware.GetUserType(c); role != "admin" {
		codigo, _, ok := apoio.Academy(c)
		if !ok {
			return
		}
		if x.CodigoAcademia != codigo {
			utils.RespondWithForbiddenError(c, "serviço extra não pertence a esta academia")
			return
		}
	}
	c.JSON(200, gin.H{"data": x})
}
