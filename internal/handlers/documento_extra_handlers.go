package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/utils"
)

// validarRotuloDocumentoExtraDisponivel verifica, ANTES de gerar o evento
// (DocumentoExtraCriado/Atualizado/Reativado), se já existe outro documento
// extra ATIVO com o mesmo rótulo (ignorando maiúsculas/minúsculas) para esta
// academia.
//
// Pré-checagem obrigatória pelo mesmo motivo documentado em
// validarNomeCategoriaServicoDisponivel (categoria_servico_handlers.go): a
// unicidade (ux_documentos_extra_rotulo_ativo) é garantida pela PROJEÇÃO, não
// pelo ledger. Sem esta checagem, dois eventos colidentes são aceitos pelo
// ledger e o SEGUNDO trava o checkpoint da projeção "documentos_extra"
// permanentemente ao tentar aplicar o INSERT/UPDATE que viola o índice único
// — confirmado neste projeto via teste manual de ponta a ponta antes desta
// pré-checagem existir.
//
// Desde a migration 127_documentos_extra_anos_academicos.sql, uma definição
// pode se aplicar a VÁRIOS anos_academicos ao mesmo tempo; a unicidade
// deixou de ser por (academia, ano_academico, rótulo) e passou a ser apenas
// por (academia, rótulo) enquanto ativo=true — ver decisão registrada na
// própria migration.
func validarRotuloDocumentoExtraDisponivel(c *gin.Context, codigoAcademia, rotulo string, excluirID *uuid.UUID) error {
	rotulo = strings.TrimSpace(rotulo)
	docs, err := getDocumentosExtraProjection(c).GetByAcademia(codigoAcademia, true)
	if err != nil {
		return fmt.Errorf("erro ao verificar rótulo de documento extra: %v", err)
	}
	for _, existente := range docs {
		if excluirID != nil && existente.ID == *excluirID {
			continue
		}
		if strings.EqualFold(existente.Rotulo, rotulo) {
			return fmt.Errorf("já existe um documento extra ativo com este rótulo")
		}
	}
	return nil
}

type documentoExtraPayload struct {
	Rotulo         string   `json:"rotulo"`
	Tipo           string   `json:"tipo"`
	Obrigatorio    bool     `json:"obrigatorio"`
	AnosAcademicos []string `json:"anos_academicos"`
}

func bindDocumentoExtraPayload(c *gin.Context, r *documentoExtraPayload) error {
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(r); err != nil {
		return fmt.Errorf("dados invalidos")
	}
	return nil
}

func documentoExtraToJSON(doc *aggregates.DocumentoExtra) gin.H {
	return gin.H{
		"id":              doc.GetID(),
		"codigo_academia": doc.CodigoAcademia,
		"rotulo":          doc.Rotulo,
		"tipo":            doc.Tipo,
		"obrigatorio":     doc.Obrigatorio,
		"anos_academicos": doc.AnosAcademicos,
		"ativo":           doc.Ativo,
		"created_at":      doc.CreatedAt,
		"updated_at":      doc.UpdatedAt,
	}
}

func CriarDocumentoExtra(c *gin.Context) {
	var r documentoExtraPayload
	if err := bindDocumentoExtraPayload(c, &r); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	codigo, id, ok := academy(c)
	if !ok {
		return
	}
	// Pré-checagem de unicidade ANTES de gerar o evento — ver comentário em
	// validarRotuloDocumentoExtraDisponivel sobre por que isto é
	// obrigatório (checkpoint da projeção trava permanentemente, para TODAS
	// as academias, se um duplicado chegar a ser aceito no ledger).
	if err := validarRotuloDocumentoExtraDisponivel(c, codigo, r.Rotulo, nil); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	doc := aggregates.NewDocumentoExtra()
	if err := doc.Criar(codigo, r.Rotulo, r.Tipo, r.Obrigatorio, r.AnosAcademicos, id); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := getRepository(c).SaveWithAudit(doc, db.AuditContext{UserID: id.String(), UserType: "academia", IP: c.ClientIP()}); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "documento extra criado com sucesso", "data": documentoExtraToJSON(doc)})
}

func loadDocumentoExtra(c *gin.Context) (*aggregates.DocumentoExtra, uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("ID de documento extra inválido"))
		return nil, uuid.Nil, false
	}
	codigo, user, ok := academy(c)
	if !ok {
		return nil, user, false
	}
	x, err := getRepository(c).Load(id, "DocumentoExtra")
	if err != nil {
		utils.RespondWithNotFoundError(c, "documento extra")
		return nil, user, false
	}
	doc, ok := x.(*aggregates.DocumentoExtra)
	if !ok || doc.CodigoAcademia != codigo {
		utils.RespondWithForbiddenError(c, "documento extra não pertence a esta academia")
		return nil, user, false
	}
	return doc, user, true
}

func AtualizarDocumentoExtra(c *gin.Context) {
	var r documentoExtraPayload
	if err := bindDocumentoExtraPayload(c, &r); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	doc, id, ok := loadDocumentoExtra(c)
	if !ok {
		return
	}
	docID := doc.GetID()
	if err := validarRotuloDocumentoExtraDisponivel(c, doc.CodigoAcademia, r.Rotulo, &docID); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := doc.Atualizar(r.Rotulo, r.Tipo, r.Obrigatorio, r.AnosAcademicos, id); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := getRepository(c).SaveWithAudit(doc, db.AuditContext{UserID: id.String(), UserType: "academia", IP: c.ClientIP()}); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "documento extra atualizado com sucesso", "data": documentoExtraToJSON(doc)})
}

func toggleDocumentoExtra(c *gin.Context, ativar bool) {
	doc, id, ok := loadDocumentoExtra(c)
	if !ok {
		return
	}
	var err error
	if ativar {
		docID := doc.GetID()
		if err = validarRotuloDocumentoExtraDisponivel(c, doc.CodigoAcademia, doc.Rotulo, &docID); err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
		err = doc.Reativar(id)
	} else {
		err = doc.Desativar(id)
	}
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err = getRepository(c).SaveWithAudit(doc, db.AuditContext{UserID: id.String(), UserType: "academia", IP: c.ClientIP()}); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": documentoExtraToJSON(doc)})
}
func DesativarDocumentoExtra(c *gin.Context) { toggleDocumentoExtra(c, false) }
func ReativarDocumentoExtra(c *gin.Context)  { toggleDocumentoExtra(c, true) }

func ListarDocumentosExtraAcademia(c *gin.Context) {
	codigo, _, ok := academy(c)
	if !ok {
		return
	}
	ativosOnly := c.Query("ativos") == "true"
	docs, err := getDocumentosExtraProjection(c).GetByAcademia(codigo, ativosOnly)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"documentos_extra": docs, "total": len(docs)})
}

// ListarDocumentosExtraPublico expõe, sem autenticação, o catálogo de
// documentos extra ATIVOS de uma academia pelo codigo_academia na URL —
// mesmo padrão já usado por ListarServicosExtrasPublico
// (servico_extra_handlers.go) para o mesmo tipo de necessidade: a tela
// pública de matrícula (/matricula) precisa saber, em segundo plano e sem
// exigir login, se a academia escolhida tem documentos extra configurados
// para o ano acadêmico que o candidato está a escolher.
//
// Antes desta rota existir, a tela pública chamava
// ListarDocumentosExtraAcademia (que exige sessão de academia via academy(c))
// e recebia sempre 401/403 — silenciosamente ignorado pelo frontend — então
// documentos extra nunca apareciam na matrícula pública, independentemente
// da academia. Ver documento de tarefa para detalhes desta descoberta.
func ListarDocumentosExtraPublico(c *gin.Context) {
	docs, err := getDocumentosExtraProjection(c).GetByAcademia(c.Param("codigo_academia"), true)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"documentos_extra": docs, "total": len(docs)})
}
