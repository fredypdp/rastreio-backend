package estudantes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/utils"
)

func RegisterEstudantePorAcademiaJobItem(c *gin.Context) {
	var item apoio.CadastroEstudanteJSONItem
	if err := apoio.BindJobItemWithoutLosingBody(c, &item); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter os mesmos campos textuais do cadastro de estudante", nil)
		return
	}

	req, declaracaoAnoAcademico, err := item.ToCadastroRequest()
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	files := map[string]apoio.UploadedPDF{}
	for field, pdf := range item.Arquivos {
		files[field] = pdf.ToUploadedPDF(field)
	}
	if len(files) > 0 {
		c.Set("permitir_pendencia_documentos_em_falha_storage", true)
	}

	registerEstudantePorAcademiaComRequestModo(c, req, files, declaracaoAnoAcademico, len(files) == 0)
}
