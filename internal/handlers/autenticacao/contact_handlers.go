package autenticacao

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/handlers/apoio"
	"spuri/internal/middleware"
	"spuri/internal/utils"
)

func AtualizarMeuEmail(c *gin.Context) { atualizarMeuContato(c, "email") }

func AtualizarMeuTelefone(c *gin.Context) { atualizarMeuContato(c, "telefone") }

func atualizarMeuContato(c *gin.Context, campo string) {
	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)
	var raw map[string]json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("body inválido"))
		return
	}
	v, ok := raw[campo]
	if !ok || len(raw) != 1 {
		utils.RespondWithValidationError(c, fmt.Errorf("forneça somente o campo %s", campo))
		return
	}
	var valor string
	if err := json.Unmarshal(v, &valor); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("%s deve ser uma string", campo))
		return
	}
	valor = strings.TrimSpace(valor)
	if campo == "email" {
		if err := utils.ValidateEmail(valor); err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
	} else {
		if err := utils.ValidatePhoneStrictNational(valor); err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
	}
	if campo == "email" && emailEmUsoPorOutro(c, valor, userID.String()) {
		utils.RespondWithConflictError(c, "email já cadastrado no sistema")
		return
	}
	repo := apoio.GetRepository(c)
	aggType := map[string]string{"estudante": "Estudante", "academia": "Academia", "admin": "Admin"}[userType]
	if aggType == "" {
		utils.RespondWithForbiddenError(c, "tipo de usuário não suportado")
		return
	}
	agg, err := repo.Load(userID, aggType)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	p := &valor
	switch a := agg.(type) {
	case *aggregates.Estudante:
		if campo == "email" {
			err = a.AtualizarDadosPessoais(nil, p, nil, nil, nil, nil, nil)
		} else {
			err = a.AtualizarDadosPessoais(nil, nil, p, nil, nil, nil, nil)
		}
	case *aggregates.Academia:
		if campo == "email" {
			err = a.AtualizarDados(nil, nil, nil, nil, nil, nil, p, nil, nil, nil, nil)
		} else {
			err = a.AtualizarDados(nil, nil, nil, nil, nil, p, nil, nil, nil, nil, nil)
		}
	case *aggregates.Admin:
		if campo == "email" {
			err = a.AtualizarDados(nil, p, nil, userID)
		} else {
			err = a.AtualizarDados(nil, nil, p, userID)
		}
	}
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := repo.SaveWithAudit(agg, db.AuditContext{UserID: userID.String(), UserType: userType, IP: c.ClientIP()}); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("%s atualizado com sucesso", campo)})
}

func emailEmUsoPorOutro(c *gin.Context, email, self string) bool {
	if e, _ := apoio.GetEstudanteProjection(c).GetByEmail(email); e != nil && e.ID.String() != self {
		return true
	}
	if a, _ := apoio.GetAcademiaProjection(c).GetByEmail(email); a != nil && a.ID.String() != self {
		return true
	}
	if a, _ := apoio.GetAdminProjection(c).GetByEmail(email); a != nil && a.ID.String() != self {
		return true
	}
	return false
}
