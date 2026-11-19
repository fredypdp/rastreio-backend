package apoio

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/middleware"
	"spuri/internal/projections"
)

// ============================================================================
// POST /academia/estudante/register
// ============================================================================

// CadastroEstudanteAcademiaRequest — genero e data_nascimento são obrigatórios.
type CadastroEstudanteAcademiaRequest struct {
	Nome                string                                   `json:"nome"            binding:"required"`
	Genero              string                                   `json:"genero"          binding:"required"`
	DataNascimento      time.Time                                `json:"data_nascimento" binding:"required"`
	Email               string                                   `json:"email"`
	Telefone            string                                   `json:"telefone"`
	TelefoneEncarregado string                                   `json:"telefone_encarregado"`
	BilheteIdentidade   string                                   `json:"bilhete_identidade"`
	BilheteEncarregado  string                                   `json:"bilhete_identidade_encarregado"`
	AnoEscolar          string                                   `json:"ano_escolar_fundamental"`
	AnoEscolarMedio     string                                   `json:"ano_escolar_medio"`
	AnoSuperior         string                                   `json:"ano_superior"`
	CursoMedioID        string                                   `json:"curso_medio_id"`
	CursoSuperiorID     string                                   `json:"curso_superior_id"`
	Documentos          map[string]aggregates.DocumentoMatricula `json:"documentos,omitempty"`
	CodigoTurma         string                                   `json:"codigo_turma"`
}

func PodeAuditarEstudante(c *gin.Context, estudante *projections.EstudanteDTO) bool {
	if estudante == nil {
		return false
	}
	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)
	switch userType {
	case "admin":
		return true
	case "estudante":
		return userID == estudante.ID
	case "academia":
		academia, err := GetAcademiaProjection(c).GetByID(userID)
		return err == nil && academia != nil && EstudantePertenceAAcademia(estudante, academia.CodigoAcademia)
	default:
		return false
	}
}

func EstudantePertenceAAcademia(estudante *projections.EstudanteDTO, codigoAcademia string) bool {
	return estudante != nil && estudante.CodigoAcademia != nil && *estudante.CodigoAcademia == codigoAcademia
}

func parseCadastroEstudanteAsyncDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("data_nascimento deve ser YYYY-MM-DD anterior à data atual")
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("data_nascimento deve ser YYYY-MM-DD anterior à data atual")
	}
	return parsed, nil
}

type AsyncUploadedPDF struct {
	Field string `json:"field"`
	Data  []byte `json:"data"`
	Size  int64  `json:"size"`
}

func NewAsyncUploadedPDF(pdf UploadedPDF) AsyncUploadedPDF {
	return AsyncUploadedPDF{Field: pdf.Field, Data: pdf.Data, Size: pdf.Size}
}

func (pdf AsyncUploadedPDF) ToUploadedPDF(fallbackField string) UploadedPDF {
	field := strings.TrimSpace(pdf.Field)
	if field == "" {
		field = fallbackField
	}
	return UploadedPDF{Field: field, Data: pdf.Data, Size: pdf.Size}
}

type CadastroEstudanteJSONItem struct {
	CodigoTemporario       string                                   `json:"codigo_temporario"`
	Nome                   string                                   `json:"nome"`
	Genero                 string                                   `json:"genero"`
	DataNascimento         string                                   `json:"data_nascimento"`
	Email                  string                                   `json:"email"`
	Telefone               string                                   `json:"telefone"`
	TelefoneEncarregado    string                                   `json:"telefone_encarregado"`
	BilheteIdentidade      string                                   `json:"bilhete_identidade"`
	BilheteEncarregado     string                                   `json:"bilhete_identidade_encarregado"`
	AnoEscolar             string                                   `json:"ano_escolar_fundamental"`
	AnoEscolarMedio        string                                   `json:"ano_escolar_medio"`
	AnoSuperior            string                                   `json:"ano_superior"`
	CursoMedioID           string                                   `json:"curso_medio_id"`
	CursoSuperiorID        string                                   `json:"curso_superior_id"`
	CodigoTurma            string                                   `json:"codigo_turma"`
	DeclaracaoAnoAcademico string                                   `json:"declaracao_ano_academico"`
	Documentos             map[string]aggregates.DocumentoMatricula `json:"documentos"`
	Arquivos               map[string]AsyncUploadedPDF              `json:"arquivos,omitempty"`
}

func (item CadastroEstudanteJSONItem) ToCadastroRequest() (CadastroEstudanteAcademiaRequest, string, error) {
	dataNascimento, err := parseCadastroEstudanteAsyncDate(item.DataNascimento)
	if err != nil {
		return CadastroEstudanteAcademiaRequest{}, "", err
	}
	return CadastroEstudanteAcademiaRequest{
		Nome:                strings.TrimSpace(item.Nome),
		Genero:              strings.TrimSpace(item.Genero),
		DataNascimento:      dataNascimento,
		Email:               strings.TrimSpace(item.Email),
		Telefone:            strings.TrimSpace(item.Telefone),
		TelefoneEncarregado: strings.TrimSpace(item.TelefoneEncarregado),
		BilheteIdentidade:   strings.TrimSpace(item.BilheteIdentidade),
		BilheteEncarregado:  strings.TrimSpace(item.BilheteEncarregado),
		AnoEscolar:          strings.TrimSpace(item.AnoEscolar),
		AnoEscolarMedio:     strings.TrimSpace(item.AnoEscolarMedio),
		AnoSuperior:         strings.TrimSpace(item.AnoSuperior),
		CursoMedioID:        strings.TrimSpace(item.CursoMedioID),
		CursoSuperiorID:     strings.TrimSpace(item.CursoSuperiorID),
		CodigoTurma:         strings.TrimSpace(item.CodigoTurma),
		Documentos:          item.Documentos,
	}, strings.TrimSpace(item.DeclaracaoAnoAcademico), nil
}

func GetPaginationParams(c *gin.Context) (limit, offset int) {
	limitStr := c.Query("limit")
	offsetStr := c.Query("offset")

	limit = 50
	offset = 0

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = db.ValidateLimit(l)
		}
	}

	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	return limit, offset
}

func GetSolicitacaoEdicaoDadoEstudanteProjection(c *gin.Context) *projections.SolicitacaoEdicaoDadoEstudanteProjection {
	return projections.NewSolicitacaoEdicaoDadoEstudanteProjection(GetDbClient(c))
}
