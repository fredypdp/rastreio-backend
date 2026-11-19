package documentos

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"spuri/internal/domain/aggregates"
	"spuri/internal/handlers/apoio"
	"spuri/internal/middleware"
	"spuri/internal/storage"
	"spuri/internal/utils"
)

func estudanteDocumentoProprioDownloadURL(campo string) string {
	return fmt.Sprintf("/estudante/documentos/%s/download", campo)
}

func academiaDocumentoProprioDownloadURL(campo string) string {
	return fmt.Sprintf("/academia/documentos/academia/%s/download", campo)
}

func academiaEstudanteDocumentoDownloadURL(codigoEstudante, campo string) string {
	return fmt.Sprintf("/academia/documentos/estudantes/%s/%s/download", codigoEstudante, campo)
}

// ListarMeusDocumentosEstudante returns the authenticated student's own
// document metadata without requiring the student to know/use the generic
// administrative lookup routes.
func ListarMeusDocumentosEstudante(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		utils.RespondWithForbiddenError(c, "estudante não autenticado")
		return
	}

	estudante, err := apoio.GetEstudanteProjection(c).GetByID(userID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if estudante == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"codigo_estudante": estudante.CodigoEstudante,
		"documentos":       documentosComDownloadEstudanteProprio(estudante.Documentos),
	})
}

// ListarDocumentosAcademia returns the authenticated academy's document
// inventory: its own formal document, documents of students linked to it, and
// documents attached to its enrollment requests.
func ListarDocumentosAcademia(c *gin.Context) {
	userType, _ := middleware.GetUserType(c)
	if userType != "academia" && userType != "admin" {
		utils.RespondWithForbiddenError(c, "apenas academias e administradores podem consultar documentos da academia")
		return
	}

	codigoAcademia := strings.TrimSpace(c.Query("codigo_academia"))
	if userType == "academia" {
		userID, _ := middleware.GetUserID(c)
		academia, err := apoio.GetAcademiaProjection(c).GetByID(userID)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if academia == nil {
			utils.RespondWithNotFoundError(c, "academia")
			return
		}
		codigoAcademia = academia.CodigoAcademia
	} else if codigoAcademia == "" {
		utils.RespondWithValidationError(c, fmt.Errorf("codigo_academia é obrigatório para administradores"))
		return
	}

	academia, err := apoio.GetAcademiaProjection(c).GetByCodigo(codigoAcademia)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if academia == nil {
		utils.RespondWithNotFoundError(c, "academia")
		return
	}

	estudantes, err := apoio.GetEstudanteProjection(c).GetByAcademia(codigoAcademia)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	estudantesDocs := make([]gin.H, 0, len(estudantes))
	for _, estudante := range estudantes {
		estudantesDocs = append(estudantesDocs, gin.H{
			"codigo_estudante": estudante.CodigoEstudante,
			"nome":             estudante.Nome,
			"status":           estudante.Status,
			"documentos":       documentosComDownloadEstudanteAcademia(estudante.CodigoEstudante, estudante.Documentos),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"codigo_academia": academia.CodigoAcademia,
		"documentos": gin.H{
			"academia": gin.H{
				"codigo_academia": academia.CodigoAcademia,
				"nome":            academia.Nome,
				"documentos":      documentosComDownloadAcademiaPropria(academia.CodigoAcademia),
			},
			"estudantes":       estudantesDocs,
			"total_estudantes": len(estudantesDocs),
		},
	})
}

// DownloadMeuDocumentoEstudante streams a document from the authenticated
// student's own scope.
func DownloadMeuDocumentoEstudante(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		utils.RespondWithForbiddenError(c, "estudante não autenticado")
		return
	}
	estudante, err := apoio.GetEstudanteProjection(c).GetByID(userID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if estudante == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}
	streamDocumentoEstudante(c, estudante.CodigoEstudante, strings.TrimSpace(c.Param("campo")))
}

// DownloadDocumentoAcademiaPropria streams the authenticated academy's own
// formal document. Admins may use codigo_academia as a query parameter.
func DownloadDocumentoAcademiaPropria(c *gin.Context) {
	codigoAcademia := strings.TrimSpace(c.Query("codigo_academia"))
	userType, _ := middleware.GetUserType(c)
	if userType == "academia" {
		userID, _ := middleware.GetUserID(c)
		academia, err := apoio.GetAcademiaProjection(c).GetByID(userID)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if academia == nil {
			utils.RespondWithNotFoundError(c, "academia")
			return
		}
		codigoAcademia = academia.CodigoAcademia
	} else if codigoAcademia == "" {
		utils.RespondWithValidationError(c, fmt.Errorf("codigo_academia é obrigatório para administradores"))
		return
	}
	streamDocumentoAcademiaPorCodigo(c, codigoAcademia, strings.TrimSpace(c.Param("campo")))
}

func DownloadDocumentoEstudanteAcademia(c *gin.Context) {
	streamDocumentoEstudante(c, strings.TrimSpace(c.Param("codigo")), strings.TrimSpace(c.Param("campo")))
}

func DownloadDocumentoSolicitacaoEdicaoEstudante(c *gin.Context) {
	streamDocumentoSolicitacaoEdicao(c, strings.TrimSpace(c.Param("codigo")), false)
}

func DownloadDocumentoSolicitacaoEdicaoAcademia(c *gin.Context) {
	streamDocumentoSolicitacaoEdicao(c, strings.TrimSpace(c.Param("codigo")), true)
}

// DownloadDocumentoEstudante streams a student document from the configured
// storage provider. The route is intentionally backend-owned so the front end
// does not need direct Mega credentials, links, or internal node IDs.
func DownloadDocumentoEstudante(c *gin.Context) {
	streamDocumentoEstudante(c, strings.TrimSpace(c.Param("codigo")), strings.TrimSpace(c.Param("campo")))
}

func streamDocumentoEstudante(c *gin.Context, codigoEstudante, campo string) {
	estudante, err := apoio.GetEstudanteProjection(c).GetByCodigo(codigoEstudante)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if estudante == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}
	if !canAccessEstudanteDocument(c, estudante.ID.String(), estudante.CodigoAcademia) {
		utils.RespondWithForbiddenError(c, "sem permissão para baixar documento deste estudante")
		return
	}
	doc, ok := documentoEstudantePorCampoEscopo(estudante.Documentos, campo, strings.TrimSpace(c.Query("nivel")), strings.TrimSpace(c.Query("ano_academico")))
	if !ok || strings.TrimSpace(doc.Path) == "" {
		utils.RespondWithNotFoundError(c, "documento")
		return
	}
	apoio.StreamDocumento(c, campo, doc)
}

func streamDocumentoSolicitacaoEdicao(c *gin.Context, codigoSolicitacao string, academiaURL bool) {
	sol, err := apoio.GetSolicitacaoEdicaoDadoEstudanteProjection(c).GetByCodigo(codigoSolicitacao)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		utils.RespondWithInternalError(c, err)
		return
	}
	if err != nil || sol == nil {
		utils.RespondWithNotFoundError(c, "solicitação")
		return
	}
	if !canAccessSolicitacaoEdicaoDocument(c, sol.CodigoEstudante, sol.CodigoAcademia) {
		utils.RespondWithForbiddenError(c, "sem permissão para baixar documento desta solicitação")
		return
	}
	doc := apoio.DocumentoSolicitacaoEdicao(sol, academiaURL)
	if strings.TrimSpace(doc.Path) == "" {
		utils.RespondWithNotFoundError(c, "documento")
		return
	}
	apoio.StreamDocumento(c, "documento_"+sol.Campo, doc)
}

// DownloadDocumentoAcademia streams formal academy documents, such as the
// alvará uploaded at registration, through the backend-owned storage boundary.
func DownloadDocumentoAcademia(c *gin.Context) {
	streamDocumentoAcademiaPorCodigo(c, strings.TrimSpace(c.Param("codigo")), strings.TrimSpace(c.Param("campo")))
}

func streamDocumentoAcademiaPorCodigo(c *gin.Context, codigoAcademia, campo string) {
	if strings.ToLower(campo) != "alvara" {
		utils.RespondWithNotFoundError(c, "documento")
		return
	}

	academia, err := apoio.GetAcademiaProjection(c).GetByCodigo(codigoAcademia)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if academia == nil {
		utils.RespondWithNotFoundError(c, "academia")
		return
	}
	if !canAccessAcademiaDocument(c, academia.CodigoAcademia) {
		utils.RespondWithForbiddenError(c, "sem permissão para baixar documento desta academia")
		return
	}

	path := fmt.Sprintf("%s/Documentação formal/alvara_%s.pdf", academia.CodigoAcademia, academia.CodigoAcademia)
	apoio.StreamDocumento(c, campo, aggregates.DocumentoMatricula{
		Path:        path,
		FileURL:     path,
		DownloadURL: apoio.AcademiaDocumentoDownloadURL(academia.CodigoAcademia, campo),
	})
}

// UploadDocumentoAcademia permite anexar — ou substituir — um documento
// formal da academia (hoje apenas "alvara") depois do cadastro, quando ele
// não foi enviado no momento do registo (POST /dominis/academia/cadastro ou
// POST /academia/cadastro, ambos com alvara agora opcional). Apenas a própria
// academia dona do codigo_academia pode enviá-lo; admins mantêm acesso somente
// para download. Reenviar para uma academia que já tem alvara substitui o
// arquivo existente (mesmo path determinístico usado no cadastro e no download).
func UploadDocumentoAcademia(c *gin.Context) {
	codigoAcademia := strings.TrimSpace(c.Param("codigo"))
	campo := strings.TrimSpace(c.Param("campo"))
	if strings.ToLower(campo) != "alvara" {
		utils.RespondWithNotFoundError(c, "documento")
		return
	}

	academia, err := apoio.GetAcademiaProjection(c).GetByCodigo(codigoAcademia)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if academia == nil {
		utils.RespondWithNotFoundError(c, "academia")
		return
	}
	if !canUploadAcademiaDocument(c, academia.CodigoAcademia) {
		utils.RespondWithForbiddenError(c, "apenas a própria academia pode enviar ou atualizar este documento")
		return
	}

	fh, err := c.FormFile("alvara")
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("alvara é obrigatório"))
		return
	}
	alvaraPDF, err := apoio.ReadAndValidatePDF("alvara", fh)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	provider := apoio.GetStorageProvider(c)
	if provider == nil {
		p, err := storage.NewStorageProvider()
		if err != nil {
			utils.RespondWithError(c, http.StatusServiceUnavailable, err.Error(), err)
			return
		}
		provider = p
	}

	dir := fmt.Sprintf("%s/Documentação formal", academia.CodigoAcademia)
	if err := provider.EnsureDir(dir); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	path := fmt.Sprintf("%s/alvara_%s.pdf", dir, academia.CodigoAcademia)
	// Delete-then-upload garante substituição limpa caso já exista um alvara
	// no mesmo path (correção de um envio anterior); ErrNotFound (primeiro
	// envio) é ignorado de propósito, mesmo padrão usado no resto do pacote.
	_ = provider.Delete(path)
	if _, err := provider.Upload(path, bytes.NewReader(alvaraPDF.Data), alvaraPDF.Size); err != nil {
		utils.RespondWithInternalError(c, fmt.Errorf("falha no upload do alvara: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":         "alvará enviado com sucesso",
		"codigo_academia": academia.CodigoAcademia,
		"download_url":    apoio.AcademiaDocumentoDownloadURL(academia.CodigoAcademia, "alvara"),
	})
}

func canAccessEstudanteDocument(c *gin.Context, estudanteID string, codigoAcademia *string) bool {
	userType, _ := middleware.GetUserType(c)
	userID, _ := middleware.GetUserID(c)
	switch userType {
	case "admin":
		return true
	case "estudante":
		return userID.String() == estudanteID
	case "academia":
		if codigoAcademia == nil {
			return false
		}
		academia, _ := apoio.GetAcademiaProjection(c).GetByID(userID)
		return academia != nil && academia.CodigoAcademia == *codigoAcademia
	default:
		return false
	}
}

func canAccessAcademiaDocument(c *gin.Context, codigoAcademia string) bool {
	userType, _ := middleware.GetUserType(c)
	if userType == "admin" {
		return true
	}
	if userType != "academia" {
		return false
	}
	userID, _ := middleware.GetUserID(c)
	academia, _ := apoio.GetAcademiaProjection(c).GetByID(userID)
	return academia != nil && academia.CodigoAcademia == codigoAcademia
}

// canUploadAcademiaDocument, diferente de canAccessAcademiaDocument, NÃO
// concede acesso a admin: enviar/substituir um documento formal da academia
// (ex.: alvará) é uma escrita nos dados da própria academia, exclusiva dela
// — admin pode consultar (canAccessAcademiaDocument), mas não alterar.
func canUploadAcademiaDocument(c *gin.Context, codigoAcademia string) bool {
	userType, _ := middleware.GetUserType(c)
	if userType != "academia" {
		return false
	}
	userID, _ := middleware.GetUserID(c)
	academia, _ := apoio.GetAcademiaProjection(c).GetByID(userID)
	return academia != nil && academia.CodigoAcademia == codigoAcademia
}

func canAccessSolicitacaoEdicaoDocument(c *gin.Context, codigoEstudante, codigoAcademia string) bool {
	userType, _ := middleware.GetUserType(c)
	userID, _ := middleware.GetUserID(c)
	switch userType {
	case "admin":
		return true
	case "estudante":
		estudante, _ := apoio.GetEstudanteProjection(c).GetByID(userID)
		return estudante != nil && estudante.CodigoEstudante == codigoEstudante
	case "academia":
		academia, _ := apoio.GetAcademiaProjection(c).GetByID(userID)
		return academia != nil && academia.CodigoAcademia == codigoAcademia
	default:
		return false
	}
}

func documentoEstudantePorCampoEscopo(documentos map[string]aggregates.DocumentoMatricula, campo, nivel, anoAcademico string) (aggregates.DocumentoMatricula, bool) {
	campo = strings.TrimSpace(campo)
	nivel = strings.TrimSpace(nivel)
	anoAcademico = strings.TrimSpace(anoAcademico)
	if campo == "" {
		return aggregates.DocumentoMatricula{}, false
	}
	if doc, ok := documentos[campo]; ok && documentoCorrespondeEscopo(doc, nivel, anoAcademico) {
		return doc, true
	}
	if nivel != "" && anoAcademico != "" {
		chaveEscopo := nivel + "." + anoAcademico + "." + campo
		if doc, ok := documentos[chaveEscopo]; ok {
			return doc, true
		}
	}
	for chave, doc := range documentos {
		if !documentoCorrespondeEscopo(doc, nivel, anoAcademico) {
			continue
		}
		if doc.Tipo == campo || strings.HasSuffix(chave, "."+campo) {
			return doc, true
		}
	}
	return aggregates.DocumentoMatricula{}, false
}

func documentoCorrespondeEscopo(doc aggregates.DocumentoMatricula, nivel, anoAcademico string) bool {
	if nivel != "" && doc.Nivel != nivel {
		return false
	}
	if anoAcademico != "" && doc.AnoAcademico != anoAcademico {
		return false
	}
	return true
}

func documentosComDownloadAcademiaPropria(codigoAcademia string) map[string]aggregates.DocumentoMatricula {
	return apoio.DocumentosComDownloadAcademiaComURL(codigoAcademia, func(campo string) string {
		return academiaDocumentoProprioDownloadURL(campo)
	})
}

func documentosComDownloadEstudanteProprio(documentos map[string]aggregates.DocumentoMatricula) map[string]aggregates.DocumentoMatricula {
	return apoio.DocumentosComDownload(documentos, func(campo string) string {
		return estudanteDocumentoProprioDownloadURL(campo)
	})
}

func documentosComDownloadEstudanteAcademia(codigoEstudante string, documentos map[string]aggregates.DocumentoMatricula) map[string]aggregates.DocumentoMatricula {
	return apoio.DocumentosComDownload(documentos, func(campo string) string {
		return academiaEstudanteDocumentoDownloadURL(codigoEstudante, campo)
	})
}
