package apoio

import (
	"crypto/rand"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/domain/aggregates"
	"spuri/internal/projections"
)

// Auxiliares do cadastro de estudante feito pela academia, de documentos e de
// upload de PDF. Antes viviam em solicitacao_matricula_handlers.go, removido
// junto com a matrícula pública (Plano, Parte 1).

const MaxPDFUploadBytes int64 = 10 << 20

var SolicitacaoDocFields = []string{"bi_estudante", "bi_encarregado", "cedula_estudante", "declaracao", "certificado_6_ano_fundamental", "certificado_9_ano_fundamental", "certificado_ensino_medio"}

var solicitacaoDocFieldSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(SolicitacaoDocFields))
	for _, field := range SolicitacaoDocFields {
		set[field] = struct{}{}
	}
	return set
}()

type UploadedPDF struct {
	Field string `json:"-"`
	Data  []byte `json:"-"`
	Size  int64  `json:"-"`
}

func StringPtrIfNotBlank(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func ValidateBIEncarregadoNaoConflitaComEscolar(c *gin.Context, biResp *string, excludeID *uuid.UUID) error {
	if biResp == nil || strings.TrimSpace(*biResp) == "" {
		return nil
	}
	var existente *projections.EstudanteDTO
	var err error
	if excludeID != nil {
		existente, err = GetEstudanteProjection(c).GetEscolarByBilheteIdentidadePrincipalExcludingID(*biResp, *excludeID)
	} else {
		existente, err = GetEstudanteProjection(c).GetEscolarByBilheteIdentidadePrincipal(*biResp)
	}
	if err != nil {
		return err
	}
	if existente != nil {
		return fmt.Errorf("bilhete_identidade_encarregado não pode coincidir com o bilhete_identidade principal de outro estudante escolar")
	}
	return nil
}

func ReadAndValidatePDF(field string, fh *multipart.FileHeader) (UploadedPDF, error) {
	if fh.Size > MaxPDFUploadBytes {
		return UploadedPDF{}, fmt.Errorf("%s deve ter no máximo 10MB", field)
	}
	if !strings.EqualFold(fh.Header.Get("Content-Type"), "application/pdf") {
		return UploadedPDF{}, fmt.Errorf("%s deve ser PDF", field)
	}
	if strings.ToLower(filepath.Ext(fh.Filename)) != ".pdf" {
		return UploadedPDF{}, fmt.Errorf("%s deve ter extensão .pdf", field)
	}
	file, err := fh.Open()
	if err != nil {
		return UploadedPDF{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxPDFUploadBytes+1))
	if err != nil {
		return UploadedPDF{}, err
	}
	if int64(len(data)) > MaxPDFUploadBytes {
		return UploadedPDF{}, fmt.Errorf("%s deve ter no máximo 10MB", field)
	}
	if len(data) < 4 || string(data[:4]) != "%PDF" {
		return UploadedPDF{}, fmt.Errorf("%s não possui assinatura PDF válida", field)
	}
	return UploadedPDF{Field: field, Data: data, Size: int64(len(data))}, nil
}

func parseOptionalCurso(c *gin.Context, raw, tipo, codigoAcademia string) (*uuid.UUID, error) {
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("curso_%s_id inválido", tipo)
	}
	curso, err := GetCursosProjection(c).GetByID(id)
	if err != nil {
		return nil, err
	}
	if curso == nil || curso.Type != tipo || curso.CodigoAcademia != codigoAcademia || curso.Status != "ativo" {
		return nil, fmt.Errorf("curso_%s_id inválido para a academia", tipo)
	}
	return &id, nil
}

func ValidarCursosMatriculaCommon(c *gin.Context, codigoAcademia, cursoMedioIDRaw, cursoSuperiorIDRaw string) (*uuid.UUID, *uuid.UUID, error) {
	cursoMedioID, err := parseOptionalCurso(c, cursoMedioIDRaw, "medio", codigoAcademia)
	if err != nil {
		return nil, nil, err
	}
	cursoSuperiorID, err := parseOptionalCurso(c, cursoSuperiorIDRaw, "superior", codigoAcademia)
	if err != nil {
		return nil, nil, err
	}
	return cursoMedioID, cursoSuperiorID, nil
}

func ValidarCamposArquivoMatricula(form *multipart.Form) error {
	if form == nil {
		return nil
	}
	for field := range form.File {
		if _, ok := solicitacaoDocFieldSet[field]; ok {
			continue
		}
		if strings.HasPrefix(field, extraDocFieldPrefix) {
			continue
		}
		return fmt.Errorf("campo de arquivo não suportado para matrícula: %s", field)
	}
	return nil
}

func DocumentosMatriculaParaValidacao(files map[string]UploadedPDF, declaracaoAnoAcademico string) map[string]aggregates.DocumentoMatricula {
	documentos := map[string]aggregates.DocumentoMatricula{}
	for field := range files {
		documento := aggregates.DocumentoMatricula{Path: field + ".pdf"}
		key, doc := DocumentoMatriculaNormalizadoComBase(field, declaracaoAnoAcademico, "", documento)
		documentos[key] = doc
	}
	return documentos
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func ParseBoundedInt(raw string, def, min, max int) int {
	if raw == "" {
		return def
	}
	var v int
	if _, err := fmt.Sscanf(raw, "%d", &v); err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func DocumentoMatriculaNormalizado(field, declaracaoAnoAcademico, downloadURL, path, fileURL string) (string, aggregates.DocumentoMatricula) {
	return DocumentoMatriculaNormalizadoComBase(field, declaracaoAnoAcademico, downloadURL, aggregates.DocumentoMatricula{Path: path, FileURL: fileURL})
}

func DocumentoMatriculaNormalizadoComBase(field, declaracaoAnoAcademico, downloadURL string, base aggregates.DocumentoMatricula) (string, aggregates.DocumentoMatricula) {
	tipo := strings.TrimSpace(firstNonEmpty(base.Tipo, field))
	ano := strings.TrimSpace(base.AnoAcademico)
	nivel := strings.TrimSpace(base.Nivel)
	if nivel == "" {
		nivel = "identificacao"
	}
	if field == "declaracao" || strings.HasPrefix(tipo, "declaracao_") {
		if ano == "" {
			ano = strings.TrimSpace(declaracaoAnoAcademico)
		}
		if ano != "" {
			tipo = aggregates.TipoDeclaracaoParaAno(ano)
			nivel = aggregates.NivelDoAnoAcademico(ano)
		} else {
			tipo = "declaracao"
			nivel = "escopo_desconhecido"
		}
	} else if strings.HasPrefix(field, "certificado_6_ano_fundamental") {
		ano, nivel = "6_ano_fundamental", "fundamental"
	} else if strings.HasPrefix(field, "certificado_9_ano_fundamental") {
		ano, nivel = "9_ano_fundamental", "fundamental"
	} else if field == "certificado_ensino_medio" {
		ano, nivel = "3_ano_medio", "medio"
	}
	if tipo == "" {
		tipo = field
	}
	docID := strings.TrimSpace(base.DocumentoID)
	if docID == "" {
		docID = uuid.NewString()
	}
	versao := base.Versao
	if versao == 0 {
		versao = 1
	}
	doc := base
	doc.DocumentoID = docID
	doc.Tipo = tipo
	doc.Nivel = nivel
	doc.AnoAcademico = ano
	doc.Versao = versao
	if downloadURL != "" {
		doc.DownloadURL = downloadURL
	}
	if nivel == "identificacao" {
		return tipo, doc
	}
	if nivel == "escopo_desconhecido" || ano == "" {
		return "escopo_desconhecido." + tipo, doc
	}
	return nivel + "." + ano + "." + tipo, doc
}

func StoragePathDocumentoEstudante(baseDir, field, codigoEstudante, declaracaoAnoAcademico string) (string, string) {
	key, doc := DocumentoMatriculaNormalizado(field, declaracaoAnoAcademico, "", "", "")
	_ = key
	if doc.Nivel == "identificacao" || doc.Nivel == "escopo_desconhecido" {
		return doc.Tipo, fmt.Sprintf("%s/identificacao/%s/%s.pdf", baseDir, doc.Tipo, doc.DocumentoID)
	}
	return doc.Tipo, fmt.Sprintf("%s/%s/%s/%s/%s.pdf", baseDir, doc.Nivel, doc.AnoAcademico, doc.Tipo, doc.DocumentoID)
}

// gerarCodigoSolicitacao devolve um código aleatório de 11 caracteres para as
// solicitações que ainda existem (edição de dados do estudante e alteração de
// NIF da academia). A unicidade é conferida por quem chama, na tabela da
// própria solicitação.
func GerarCodigoSolicitacao() (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	r := make([]byte, 11)
	if _, err := rand.Read(r); err != nil {
		return "", err
	}
	b := make([]byte, 11)
	for i := range r {
		b[i] = alphabet[int(r[i])%len(alphabet)]
	}
	return string(b), nil
}
