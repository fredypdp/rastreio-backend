package apoio

import (
	"errors"

	"github.com/gin-gonic/gin"

	"spuri/internal/cadastros"
	"spuri/internal/utils"
)

// RespondErroCadastro traduz o erro de uma operação de internal/cadastros na
// resposta HTTP: violação de regra → erro de validação (a mesma resposta que o
// agregado produzia ao recusar um comando), registro inexistente → não
// encontrado, qualquer outro → erro interno.
func RespondErroCadastro(c *gin.Context, err error, entidade string) {
	if regra, ok := cadastros.EhErroRegra(err); ok {
		utils.RespondWithValidationError(c, regra.Err)
		return
	}
	if conf, ok := cadastros.EhErroConflito(err); ok {
		utils.RespondWithConflictError(c, conf.Err.Error())
		return
	}
	if errors.Is(err, cadastros.ErrNaoEncontrado) {
		utils.RespondWithNotFoundError(c, entidade)
		return
	}
	utils.RespondWithInternalError(c, err)
}
