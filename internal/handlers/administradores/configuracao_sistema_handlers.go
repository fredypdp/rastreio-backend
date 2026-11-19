package administradores

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/middleware"
	"spuri/internal/services"
	"spuri/internal/sysconfig"
	"spuri/internal/utils"
)

func configuracaoSistemaService(c *gin.Context) *services.ConfiguracaoSistemaService {
	return services.NovaConfiguracaoSistemaService(apoio.GetDbClient(c), apoio.GetRepository(c).WithContext(c.Request.Context()))
}

// ListarConfiguracoesSistema devolve as configurações editáveis (lista aprovada
// em sysconfig.Definicoes) com o valor efetivo, a origem (banco, ambiente ou
// não definida) e quem alterou por último. Segredos saem só com máscara.
// Rota: GET /admin/sistema/configuracoes (adm e fpp).
func ListarConfiguracoesSistema(c *gin.Context) {
	itens, err := configuracaoSistemaService(c).Listar(c.Request.Context())
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": itens})
}

// DefinirConfiguracaoSistema grava o valor de uma configuração aprovada.
// Rota: PUT /admin/sistema/configuracoes/:chave (só fpp). Corpo: {"valor":"..."}.
// Para segredo, o valor substitui o atual e nunca é devolvido nem logado.
func DefinirConfiguracaoSistema(c *gin.Context) {
	adminID, ok := middleware.GetUserID(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	var req struct {
		Valor string `json:"valor"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("corpo inválido: envie {\"valor\": \"...\"}"))
		return
	}
	view, err := configuracaoSistemaService(c).Definir(c.Request.Context(), c.Param("chave"), req.Valor, adminID, c.ClientIP())
	if respondeErroConfiguracaoSistema(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": mensagemAplicacao("configuração salva", view), "data": view})
}

// RemoverConfiguracaoSistema tira o valor do banco; a chave volta a ser lida da
// variável de ambiente. Rota: DELETE /admin/sistema/configuracoes/:chave (só fpp).
func RemoverConfiguracaoSistema(c *gin.Context) {
	adminID, ok := middleware.GetUserID(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	view, err := configuracaoSistemaService(c).Remover(c.Request.Context(), c.Param("chave"), adminID, c.ClientIP())
	if respondeErroConfiguracaoSistema(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": mensagemAplicacao("configuração removida do banco; volta a valer a variável de ambiente", view), "data": view})
}

func mensagemAplicacao(base string, v services.ConfiguracaoSistemaView) string {
	if v.Aplicacao == string(sysconfig.AplicaReinicio) {
		return base + "; vale a partir do próximo reinício do servidor"
	}
	return base
}

// respondeErroConfiguracaoSistema traduz os erros do serviço; devolve true
// quando já respondeu.
func respondeErroConfiguracaoSistema(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	var ev *sysconfig.ErroValidacao
	switch {
	case errors.Is(err, sysconfig.ErrNaoAprovada):
		utils.RespondWithNotFoundError(c, "configuração")
	case errors.As(err, &ev):
		utils.RespondWithValidationError(c, err)
	case errors.Is(err, services.ErrConfiguracaoSemValorNoBanco):
		utils.RespondWithNotFoundError(c, "valor gravado no banco para esta configuração")
	default:
		utils.RespondWithInternalError(c, err)
	}
	return true
}
