package comunicacao

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/handlers/apoio"
	"spuri/internal/middleware"
	"spuri/internal/projections"
	"spuri/internal/services"
	"spuri/internal/utils"
)

// ── Helpers específicos deste módulo ──────────────────────────────────

// comunicacaoActor identifica quem está a chamar a rota: um admin (sem
// codigo_academia) ou uma academia autenticada (com o seu codigo_academia).
// Usado pelas rotas de duplo acesso (enviar mensagem, listar mensagens).
func comunicacaoActor(c *gin.Context) (userID uuid.UUID, userType string, codigoAcademia string, ok bool) {
	id, exists := middleware.GetUserID(c)
	if !exists {
		utils.RespondWithUnauthorizedError(c)
		return uuid.Nil, "", "", false
	}
	t, exists := middleware.GetUserType(c)
	if !exists {
		utils.RespondWithUnauthorizedError(c)
		return uuid.Nil, "", "", false
	}
	if t == "academia" {
		academia, err := apoio.GetAcademiaProjection(c).GetByID(id)
		if err != nil || academia == nil {
			utils.RespondWithForbiddenError(c, "academia não encontrada")
			return uuid.Nil, "", "", false
		}
		return id, "academia", academia.CodigoAcademia, true
	}
	return id, "admin", "", true
}

// resolverRemetenteAggregate carrega o RemetenteComunicacao existente do
// provedor (via ledger) ou cria um novo com o ID determinístico do
// provedor, caso ainda não exista nenhum. Nunca cria um agregado com ID
// aleatório para este tipo.
func resolverRemetenteAggregate(c *gin.Context, provedor string) (*aggregates.RemetenteComunicacao, error) {
	aggID, known := aggregates.RemetenteAggregateID(provedor)
	if !known {
		return nil, fmt.Errorf("provedor inválido: use GOSMS ou ZIETT")
	}
	loaded, err := apoio.GetRepository(c).Load(aggID, "RemetenteComunicacao")
	if err != nil {
		novo := aggregates.NewRemetenteComunicacao()
		novo.SetID(aggID)
		return novo, nil
	}
	agg, ok := loaded.(*aggregates.RemetenteComunicacao)
	if !ok {
		return nil, fmt.Errorf("tipo inesperado ao carregar RemetenteComunicacao")
	}
	return agg, nil
}

// ── 1. Cadastrar remetente (rota única — FPP admin, GOSMS ou ZIETT) ────

type criarRemetenteComunicacaoRequest struct {
	Provedor      string `json:"provedor" binding:"required"`
	Identificador string `json:"identificador" binding:"required"`
	TokenAPI      string `json:"token_api" binding:"required"`
}

// CriarRemetenteComunicacao cadastra (primeira vez) ou substitui
// (chamadas seguintes) as credenciais de envio de um provedor. Remetente é
// sempre global — não pertence a nenhuma academia — e existe no máximo um
// por provedor. Não chama nenhuma API do GoSMS/Ziett: apenas grava, no
// nosso próprio banco, o identificador (nome do Sender ID no GoSMS, ou
// UUID do remitter_id no Ziett) e o token de API, cifrado.
func CriarRemetenteComunicacao(c *gin.Context) {
	if err := apoio.VerificarPermissaoAdmin(c, "fpp"); err != nil {
		utils.RespondWithForbiddenError(c, "apenas administradores FPP podem cadastrar remetentes de comunicação")
		return
	}
	userID, exists := middleware.GetUserID(c)
	if !exists {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	var req criarRemetenteComunicacaoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	agg, err := resolverRemetenteAggregate(c, strings.ToUpper(strings.TrimSpace(req.Provedor)))
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	eraNovo := agg.Version == 0

	tokenCifrado, err := services.EncryptComunicacaoSegredo(req.TokenAPI)
	if err != nil {
		utils.RespondWithInternalError(c, fmt.Errorf("falha ao cifrar o token de API: %w", err))
		return
	}

	if err := agg.Configurar(req.Provedor, req.Identificador, tokenCifrado, userID); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	audit := db.AuditContext{UserID: userID.String(), UserType: "admin", IP: c.ClientIP()}
	if err := apoio.GetRepository(c).SaveWithAudit(agg, audit); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	status := http.StatusOK
	if eraNovo {
		status = http.StatusCreated
	}
	c.JSON(status, gin.H{
		"id":                   agg.ID,
		"provedor":             agg.Provedor,
		"identificador":        agg.Identificador,
		"token_configurado":    true,
		"configurado_por":      agg.ConfiguradoPor,
		"configurado_por_tipo": agg.ConfiguradoPorTipo,
		"created_at":           agg.CreatedAt,
		"updated_at":           agg.UpdatedAt,
	})
}

// ── 3. Listar remetentes (rota única — apenas administradores) ─────────

// ListarRemetentesComunicacao devolve os remetentes globais configurados
// (no máximo dois: um por provedor). Nunca inclui o token de API — apenas
// o indicador booleano token_configurado.
func ListarRemetentesComunicacao(c *gin.Context) {
	if err := apoio.VerificarPermissaoAdmin(c, "gerente"); err != nil {
		utils.RespondWithForbiddenError(c, "apenas administradores podem listar remetentes de comunicação")
		return
	}
	lista, err := apoio.GetRemetentesComunicacaoProjection(c).List()
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"remetentes": lista})
}

// ── 2. Enviar mensagem (rota única — administradores e academias) ──────

type enviarMensagemComunicacaoRequest struct {
	Destinatario string `json:"destinatario" binding:"required"`
	Conteudo     string `json:"conteudo" binding:"required"`
}

// EnviarMensagemComunicacao envia UMA mensagem para UM destinatário.
// Tenta primeiro o provedor definido como padrão (ver
// DefinirProvedorPadraoComunicacao); se esse provedor não tiver remetente
// configurado ou a tentativa de envio falhar, tenta automaticamente o
// outro provedor. A mensagem é sempre registada (sucesso ou falha) em
// projection_mensagens_comunicacao, com o detalhe de cada tentativa.
func EnviarMensagemComunicacao(c *gin.Context) {
	userID, userType, codigoAcademia, ok := comunicacaoActor(c)
	if !ok {
		return
	}

	var req enviarMensagemComunicacaoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	audit := db.AuditContext{UserID: userID.String(), UserType: userType, IP: c.ClientIP()}
	msg, err := apoio.EnviarSMSComunicacao(c.Request.Context(), c, apoio.AtorComunicacao{ID: userID, Tipo: userType, CodigoAcademia: codigoAcademia}, req.Destinatario, req.Conteudo, audit)
	if err != nil {
		var entrada apoio.ErroEntradaComunicacao
		if errors.Is(err, apoio.ErrProvedorPadraoNaoConfigurado) || errors.As(err, &entrada) {
			utils.RespondWithValidationError(c, err)
			return
		}
		utils.RespondWithInternalError(c, err)
		return
	}
	status := msg.Status

	resposta := gin.H{
		"id":                  msg.ID,
		"destinatario":        msg.Destinatario,
		"status":              msg.Status,
		"provedor_utilizado":  msg.ProvedorUtilizado,
		"mensagem_externa_id": msg.MensagemExternaID,
		"detalhes_tentativas": msg.DetalhesTentativas,
		"created_at":          msg.CreatedAt,
	}
	if status == aggregates.StatusMensagemComunicacaoFalhou {
		utils.RespondWithErrorData(c, http.StatusBadGateway, "não foi possível enviar a mensagem em nenhum dos provedores configurados", fmt.Errorf("todas as tentativas de envio falharam"), resposta)
		return
	}
	c.JSON(http.StatusCreated, resposta)
}

// ── 3. Listar mensagens (rota única — administradores e academias) ─────

// ListarMensagensComunicacao devolve o histórico de mensagens enviadas.
// Uma academia só vê as mensagens que ela própria enviou. Um administrador
// vê todas, com filtro opcional por codigo_academia (?codigo_academia=).
func ListarMensagensComunicacao(c *gin.Context) {
	_, userType, codigoAcademia, ok := comunicacaoActor(c)
	if !ok {
		return
	}
	filtro := projections.MensagemComunicacaoListFiltro{}
	if userType == "academia" {
		filtro.CodigoAcademia = codigoAcademia
	} else {
		filtro.CodigoAcademia = strings.TrimSpace(c.Query("codigo_academia"))
	}
	filtro.Limit, filtro.Offset = apoio.GetPaginationParams(c)

	lista, total, err := apoio.GetMensagensComunicacaoProjection(c).List(filtro)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"mensagens": lista, "total": total, "limit": filtro.Limit, "offset": filtro.Offset})
}

// ── 2.1 Provedor padrão (FPP admin) ─────────────────────────────────────

type definirProvedorPadraoComunicacaoRequest struct {
	ProvedorPadrao string `json:"provedor_padrao" binding:"required"`
}

// DefinirProvedorPadraoComunicacao define qual provedor (GOSMS ou ZIETT)
// o sistema tenta primeiro ao enviar uma mensagem. É uma configuração
// única e global — não existe um provedor padrão por academia.
func DefinirProvedorPadraoComunicacao(c *gin.Context) {
	if err := apoio.VerificarPermissaoAdmin(c, "fpp"); err != nil {
		utils.RespondWithForbiddenError(c, "apenas administradores FPP podem definir o provedor padrão")
		return
	}
	var req definirProvedorPadraoComunicacaoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	provedor := strings.ToUpper(strings.TrimSpace(req.ProvedorPadrao))
	if provedor != aggregates.ProvedorComunicacaoGoSMS && provedor != aggregates.ProvedorComunicacaoZiett {
		utils.RespondWithValidationError(c, fmt.Errorf("provedor_padrao deve ser GOSMS ou ZIETT"))
		return
	}
	userID, exists := middleware.GetUserID(c)
	if !exists {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	if _, err := apoio.GetDbClient(c).DB().Exec(`UPDATE projection_comunicacao_config SET provedor_padrao = $1, atualizado_por = $2, atualizado_em = now() WHERE id = 1`, provedor, userID); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"provedor_padrao": provedor})
}

// ConsultarProvedorPadraoComunicacao devolve o provedor padrão atual
// (ou null, se nenhum admin FPP o tiver definido ainda).
func ConsultarProvedorPadraoComunicacao(c *gin.Context) {
	if err := apoio.VerificarPermissaoAdmin(c, "fpp"); err != nil {
		utils.RespondWithForbiddenError(c, "apenas administradores FPP podem consultar o provedor padrão")
		return
	}
	var provedorPadrao sql.NullString
	var atualizadoEm sql.NullTime
	if err := apoio.GetDbClient(c).DB().QueryRow(`SELECT provedor_padrao, atualizado_em FROM projection_comunicacao_config WHERE id = 1`).Scan(&provedorPadrao, &atualizadoEm); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	resp := gin.H{"provedor_padrao": nil, "atualizado_em": nil}
	if provedorPadrao.Valid {
		resp["provedor_padrao"] = provedorPadrao.String
	}
	if atualizadoEm.Valid {
		resp["atualizado_em"] = atualizadoEm.Time
	}
	c.JSON(http.StatusOK, resp)
}
