package estudantes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"spuri/internal/handlers/apoio"
	"spuri/internal/jobs"
	"spuri/internal/utils"
)

// =============================================================================
// POST /academia/estudante/register/async — limite 100
// =============================================================================

func RegisterEstudanteBatch(c *gin.Context) {
	if apoio.RejectRemovedJSONFields(c) {
		return
	}
	if strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		registerEstudanteBatchMultipart(c)
		return
	}
	var payload struct {
		ComArquivo *bool                             `json:"com_arquivo"`
		Estudantes []apoio.CadastroEstudanteJSONItem `json:"estudantes"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve conter {com_arquivo:boolean, estudantes:[...]}", nil)
		return
	}
	if payload.ComArquivo == nil {
		utils.RespondWithValidationError(c, fmt.Errorf("com_arquivo é obrigatório"))
		return
	}
	if *payload.ComArquivo {
		utils.RespondWithValidationError(c, fmt.Errorf("com_arquivo true exige multipart/form-data"))
		return
	}
	enqueueCadastroEstudanteJSONSemArquivos(c, payload.Estudantes)
}

func enqueueCadastroEstudanteJSONSemArquivos(c *gin.Context, items []apoio.CadastroEstudanteJSONItem) {
	if err := apoio.ValidarTamanhoBatch(len(items), 100); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, err.Error(), nil)
		return
	}
	if len(items) == 0 {
		utils.RespondWithValidationError(c, fmt.Errorf("array não pode ser vazio"))
		return
	}
	apoio.EnqueueAsyncBatchPayload(c, jobs.JobTypeRegisterEstudanteBatch, items, len(items))
}

func registerEstudanteBatchMultipart(c *gin.Context) {
	if err := c.Request.ParseMultipartForm(64 << 20); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("multipart/form-data inválido"))
		return
	}
	if apoio.RejectRemovedMultipartFields(c) {
		return
	}
	if strings.TrimSpace(c.PostForm("com_arquivo")) != "true" {
		utils.RespondWithValidationError(c, fmt.Errorf("com_arquivo true é obrigatório para multipart/form-data"))
		return
	}
	estudantesRaw := c.PostForm("estudantes")
	if old, newf, ok := apoio.FindRemovedJSONFieldString(estudantesRaw); ok {
		apoio.RespondRemovedField(c, old, newf)
		return
	}
	var items []apoio.CadastroEstudanteJSONItem
	if err := json.Unmarshal([]byte(estudantesRaw), &items); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("campo estudantes deve ser JSON válido"))
		return
	}
	filesByCodigo := map[string]map[string]apoio.UploadedPDF{}
	knownFields := map[string]bool{}
	for _, f := range apoio.SolicitacaoDocFields {
		knownFields[f] = true
	}
	for field, fhs := range c.Request.MultipartForm.File {
		parts := strings.SplitN(field, ".", 2)
		if len(parts) != 2 || !knownFields[parts[1]] {
			utils.RespondWithValidationError(c, fmt.Errorf("campo de arquivo de lote inválido: %s", field))
			return
		}
		if len(fhs) != 1 {
			utils.RespondWithValidationError(c, fmt.Errorf("arquivo duplicado: %s", field))
			return
		}
		pdf, err := apoio.ReadAndValidatePDF(parts[1], fhs[0])
		if err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
		if filesByCodigo[parts[0]] == nil {
			filesByCodigo[parts[0]] = map[string]apoio.UploadedPDF{}
		}
		if _, exists := filesByCodigo[parts[0]][parts[1]]; exists {
			utils.RespondWithValidationError(c, fmt.Errorf("arquivo duplicado: %s", field))
			return
		}
		filesByCodigo[parts[0]][parts[1]] = pdf
	}
	if validarArquivosCadastroEstudanteBatch(c, items, filesByCodigo) {
		return
	}
	for i := range items {
		if filesByCodigo[items[i].CodigoTemporario] == nil {
			continue
		}
		items[i].Arquivos = map[string]apoio.AsyncUploadedPDF{}
		for field, pdf := range filesByCodigo[items[i].CodigoTemporario] {
			items[i].Arquivos[field] = apoio.NewAsyncUploadedPDF(pdf)
		}
	}
	enqueueCadastroEstudanteComArquivos(c, items)
}

func validarArquivosCadastroEstudanteBatch(c *gin.Context, items []apoio.CadastroEstudanteJSONItem, filesByCodigo map[string]map[string]apoio.UploadedPDF) bool {
	seen := map[string]bool{}
	for _, item := range items {
		if item.CodigoTemporario == "" {
			utils.RespondWithValidationError(c, fmt.Errorf("codigo_temporario é obrigatório no lote com arquivos"))
			return true
		}
		if seen[item.CodigoTemporario] {
			utils.RespondWithValidationError(c, fmt.Errorf("codigo_temporario duplicado: %s", item.CodigoTemporario))
			return true
		}
		seen[item.CodigoTemporario] = true
	}
	for codigo := range filesByCodigo {
		if !seen[codigo] {
			utils.RespondWithValidationError(c, fmt.Errorf("arquivo órfão para codigo_temporario: %s", codigo))
			return true
		}
	}
	return false
}

func enqueueCadastroEstudanteComArquivos(c *gin.Context, items []apoio.CadastroEstudanteJSONItem) {
	if err := apoio.ValidarTamanhoBatch(len(items), 100); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, err.Error(), nil)
		return
	}
	if len(items) == 0 {
		utils.RespondWithValidationError(c, fmt.Errorf("array não pode ser vazio"))
		return
	}
	apoio.EnqueueAsyncBatchPayload(c, jobs.JobTypeRegisterEstudanteBatch, items, len(items))
}

func processarCadastroEstudanteBatch(c *gin.Context, items []apoio.CadastroEstudanteJSONItem, filesByCodigo map[string]map[string]apoio.UploadedPDF, pendenteDocumentos bool) {
	if err := apoio.ValidarTamanhoBatch(len(items), 100); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, err.Error(), nil)
		return
	}
	seen := map[string]bool{}
	if filesByCodigo != nil {
		for _, item := range items {
			if item.CodigoTemporario == "" {
				utils.RespondWithValidationError(c, fmt.Errorf("codigo_temporario é obrigatório no lote com arquivos"))
				return
			}
			if seen[item.CodigoTemporario] {
				utils.RespondWithValidationError(c, fmt.Errorf("codigo_temporario duplicado: %s", item.CodigoTemporario))
				return
			}
			seen[item.CodigoTemporario] = true
		}
		for codigo := range filesByCodigo {
			if !seen[codigo] {
				utils.RespondWithValidationError(c, fmt.Errorf("arquivo órfão para codigo_temporario: %s", codigo))
				return
			}
		}
	}
	results := make([]apoio.BatchItemResult, 0, len(items))
	for i, item := range items {
		req, declaracaoAnoAcademico, err := item.ToCadastroRequest()
		if err != nil {
			results = append(results, apoio.BatchErr(i, err))
			continue
		}
		files := map[string]apoio.UploadedPDF{}
		if filesByCodigo != nil {
			files = filesByCodigo[item.CodigoTemporario]
		}
		rc := apoio.NewFakeContext(c)
		if filesByCodigo != nil {
			rc.Set("permitir_pendencia_documentos_em_falha_storage", true)
		}
		registerEstudantePorAcademiaComRequestModo(rc, req, files, declaracaoAnoAcademico, pendenteDocumentos)
		results = append(results, apoio.ExtractResult(rc, i))
	}
	c.JSON(apoio.BatchHTTPStatus(results), apoio.NewBatchResponse(results))
}
