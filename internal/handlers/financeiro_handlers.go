package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/finance"
	"spuri/internal/middleware"
	"spuri/internal/utils"
)

// FinanceiroService is initialised with the application database in initDB.
var FinanceiroService = finance.NewService(nil)

func financeActor(c *gin.Context) (uuid.UUID, string, string, bool) {
	id, ok := middleware.GetUserID(c)
	if !ok {
		return uuid.Nil, "", "", false
	}
	typeName, ok := middleware.GetUserType(c)
	if !ok {
		return uuid.Nil, "", "", false
	}
	return id, typeName, c.GetString("codigo_academia"), true
}
func financeAdminAllowed(c *gin.Context) bool {
	_, t, _, ok := financeActor(c)
	if !ok || t != "admin" {
		return false
	}
	return verificarPermissaoAdmin(c, "fpp") == nil
}

// authorizeFinanceScope autoriza o ator autenticado a agir sobre um contexto
// financeiro (contexto_tipo + codigo_academia). Uma academia só pode agir
// sobre o próprio contexto (forçado por referência). Um admin com permissão
// "fpp" pode sempre CONSULTAR (write=false) qualquer contexto, inclusive o de
// qualquer academia — mas nunca pode CRIAR, ATUALIZAR, REMOVER ou ROTACIONAR
// (write=true) as configurações financeiras de uma academia especificamente;
// essa capacidade de escrita continua disponível apenas para o contexto
// "spuri" (as configurações do próprio Spuri, que não pertencem a nenhuma
// academia) e para a própria academia sobre si mesma.
func authorizeFinanceScope(c *gin.Context, context *string, academy *string, write bool) bool {
	_, t, own, ok := financeActor(c)
	if !ok {
		return false
	}
	if t == "academia" {
		if *context != "" && *context != finance.ContextoAcademia {
			return false
		}
		if *academy != "" && *academy != own {
			return false
		}
		*context = finance.ContextoAcademia
		*academy = own
		return true
	}
	if t != "admin" || !financeAdminAllowed(c) {
		return false
	}
	if write && *context == finance.ContextoAcademia {
		return false
	}
	return true
}

// credentialScopeAuthorized resolve o contexto/academia dono de uma
// credencial de provedor pelo seu id e reaplica authorizeFinanceScope — o mesmo
// mecanismo que já garante que uma academia só mexe nas próprias credenciais
// e que um admin precisa da permissão "fpp" para consultar (e nunca pode
// escrever no contexto de uma academia). Usado pelas rotas de consulta e
// rotação do segredo de webhook: write=false para consulta, write=true para
// rotação (que gera um novo segredo, invalidando o anterior). Já escreve a
// resposta de erro (404 ou 403) no contexto quando retorna false.
func credentialScopeAuthorized(c *gin.Context, id uuid.UUID, write bool) bool {
	contexto, academia, err := FinanceiroService.CredentialScope(c.Request.Context(), id)
	if err != nil {
		financeError(c, err)
		return false
	}
	if !authorizeFinanceScope(c, &contexto, &academia, write) {
		utils.RespondWithForbiddenError(c, "sem permissão para esta credencial")
		return false
	}
	return true
}

func financeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, finance.ErrNotFound):
		utils.RespondWithNotFoundError(c, "recurso financeiro")
	case errors.Is(err, finance.ErrPagamentoExistente):
		utils.RespondWithConflictError(c, finance.ErrPagamentoExistente.Error())
	case errors.Is(err, finance.ErrConflict):
		utils.RespondWithConflictError(c, "operação financeira equivalente em processamento")
	case errors.Is(err, finance.ErrUpstream):
		utils.RespondWithServiceUnavailable(c, err)
	case errors.Is(err, finance.ErrCredenciaisEmUso):
		// Mecanismo retroativo: o provedor ainda tem cobranças pendentes.
		utils.RespondWithConflictError(c, err.Error())
	default:
		utils.RespondWithValidationError(c, err)
	}
}

func CriarCobranca(c *gin.Context) {
	var in finance.ChargeRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	// Fora do escopo de "configurações financeiras": criar uma cobrança não
	// é uma configuração de academia, é uma operação transacional pontual —
	// o admin FPP continua podendo emiti-la em qualquer contexto, como já
	// era o comportamento antes desta correção (write=false).
	if !authorizeFinanceScope(c, &in.ContextoTipo, &in.CodigoAcademia, false) {
		utils.RespondWithForbiddenError(c, "sem permissão para este contexto financeiro")
		return
	}
	id, t, _, ok := financeActor(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	out, err := FinanceiroService.CreateCharge(c.Request.Context(), in, id.String(), t, c.ClientIP())
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}
func GerarQRCode(c *gin.Context) {
	var in finance.QRCodeRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	// Fora do escopo de "configurações financeiras": gerar QR Code é uma
	// operação transacional de uma cobrança já existente, não uma
	// configuração de academia (write=false, preserva comportamento anterior).
	if !authorizeFinanceScope(c, &in.ContextoTipo, &in.CodigoAcademia, false) {
		utils.RespondWithForbiddenError(c, "sem permissão para este contexto financeiro")
		return
	}
	id, t, _, ok := financeActor(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	out, err := FinanceiroService.CreateGPOQRCode(c.Request.Context(), in, id.String(), t, c.ClientIP())
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}
func ConsultarCobranca(c *gin.Context) {
	contexto := c.Query("contexto_tipo")
	academia := c.Query("codigo_academia")
	if !authorizeFinanceScope(c, &contexto, &academia, false) {
		utils.RespondWithForbiddenError(c, "sem permissão para este contexto financeiro")
		return
	}
	id, t, _, ok := financeActor(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	out, err := FinanceiroService.ConsultCharge(c.Request.Context(), contexto, academia, c.Param("id"), id.String(), t, c.ClientIP())
	if err != nil {
		financeError(c, err)
		return
	}
	// A cobrança pode ser de matrícula: diferente de uma cobrança de
	// mensalidade (confirmada via confirmMensalidadeCharge dentro do
	// próprio ConsultCharge), a efetivação do vínculo de matrícula
	// (criação do estudante e transição da solicitação) não faz parte do
	// pacote financeiro e precisa ser acionada aqui, exatamente como já é
	// feito em ReceberWebhook e na criação síncrona da cobrança em
	// IniciarPagamentoMatricula. Sem isto, uma cobrança de matrícula que só
	// é confirmada pela AppyPay quando alguém consulta o status (fluxo
	// normal para GPO/REF, que nunca retornam "success" na criação) nunca
	// efetiva a matrícula.
	if strings.EqualFold(strings.TrimSpace(out.Status), "success") {
		if codigo, err := FinanceiroService.CodigoSolicitacaoDaCobranca(c.Request.Context(), c.Param("id")); err == nil && codigo != "" {
			if err := efetivarVinculoMatriculaPaga(c, codigo); err != nil {
				utils.RespondWithInternalError(c, err)
				return
			}
		}
		if codigo, tipo, mes, ano, err := FinanceiroService.DadosServicoExtraDaCobranca(c.Request.Context(), c.Param("id")); err == nil && codigo != "" {
			switch tipo {
			case "taxa_inscricao":
				if err := efetivarVinculoServicoExtraPago(c, codigo); err != nil {
					utils.RespondWithInternalError(c, err)
					return
				}
			case "mensalidade", "preco_unico":
				_ = FinanceiroService.ConfirmarLancamentoServicoExtraPago(c.Request.Context(), codigo, tipo, ano, mes, id.String(), t, c.ClientIP())
			}
		}
	}
	c.JSON(http.StatusOK, out)
}

// ListarCobrancas lista cobranças (mensalidade, matrícula ou avulsa)
// do contexto autorizado, com filtros opcionais por estado e origem e
// paginação — resolve o Problema 1 documentado em
// docs/Lista de Tarefas/Problemas de Backend - Modulo de Pagamentos.md.
// Mesma autorização de ConsultarCobranca/ListarCredenciais:
// uma academia só vê as próprias cobranças; um admin precisa da permissão
// "fpp" e pode consultar qualquer contexto/academia via query string.

// parseOptionalUUIDQuery lê um parâmetro de query opcional como UUID. Devolve
// nil quando o parâmetro não foi informado, e erro quando foi informado mas
// não é um UUID válido.
func parseOptionalUUIDQuery(c *gin.Context, param string) (*uuid.UUID, error) {
	raw := strings.TrimSpace(c.Query(param))
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s inválido", param)
	}
	return &id, nil
}

// parseOptionalMesQuery lê um parâmetro de query opcional como mês de
// calendário (1-12). Devolve nil quando o parâmetro não foi informado, e
// erro quando foi informado mas não é um inteiro entre 1 e 12.
func parseOptionalMesQuery(c *gin.Context, param string) (*int, error) {
	raw := strings.TrimSpace(c.Query(param))
	if raw == "" {
		return nil, nil
	}
	mes, err := strconv.Atoi(raw)
	if err != nil || mes < 1 || mes > 12 {
		return nil, fmt.Errorf("%s deve ser um mês entre 1 e 12", param)
	}
	return &mes, nil
}

func ListarCobrancas(c *gin.Context) {
	contexto := c.Query("contexto_tipo")
	academia := c.Query("codigo_academia")
	if !authorizeFinanceScope(c, &contexto, &academia, false) {
		utils.RespondWithForbiddenError(c, "sem permissão para este contexto financeiro")
		return
	}
	turmaID, err := parseOptionalUUIDQuery(c, "turma_id")
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	cursoID, err := parseOptionalUUIDQuery(c, "curso_id")
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	anoAcademico := c.Query("ano_academico")
	anoLetivo := c.Query("ano_letivo")
	mes, err := parseOptionalMesQuery(c, "mes")
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	limit := parseBoundedInt(c.Query("limit"), 50, 1, 1000)
	offset := parseBoundedInt(c.Query("offset"), 0, 0, 1_000_000)
	estados := c.QueryArray("estado")
	origens := c.QueryArray("tipo")
	// pendências sem cobrança só são computadas quando pelo menos um dos
	// quatro filtros de escopo (turma_id, curso_id, ano_academico,
	// ano_letivo) é informado junto de codigo_academia — sem isso, a
	// varredura seria sobre a academia inteira sem limite. mes (tarefa 60)
	// só refina esse escopo, nunca o substitui. Ver finance.PendenciasSemCobranca.
	//
	// Além do escopo, uma pendência sintética só entra na lista se ela não
	// for excluída pelos filtros estado/tipo — ver
	// finance.DeveIncluirPendenciasSemCobranca para o porquê: toda
	// pendência sintética é sempre estado="pendente" e tipo="mensalidade",
	// então pedir estado=Failed (por exemplo) deve excluí-las, não trazê-las
	// de qualquer forma.
	var pendencias []finance.MensalidadeMesView
	if (turmaID != nil || cursoID != nil || anoAcademico != "" || anoLetivo != "") && finance.DeveIncluirPendenciasSemCobranca(estados, origens) {
		pendencias, err = FinanceiroService.PendenciasSemCobranca(c.Request.Context(), academia, turmaID, cursoID, anoAcademico, anoLetivo, mes)
		if err != nil {
			financeError(c, err)
			return
		}
		pendencias, err = FinanceiroService.FiltrarPendenciasComCobrancaRealVinculada(c.Request.Context(), pendencias)
		if err != nil {
			financeError(c, err)
			return
		}
	}
	res, err := finance.ListarPagamentosUnificado(pendencias, func(limitCobrancas, offsetCobrancas int) (*finance.CobrancaListResult, error) {
		return FinanceiroService.ListCobrancas(c.Request.Context(), contexto, academia, estados, origens, turmaID, cursoID, anoAcademico, anoLetivo, mes, limitCobrancas, offsetCobrancas)
	}, limit, offset)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"pagamentos": res.Pagamentos, "total": len(res.Pagamentos), "total_geral": res.Total, "limit": limit, "offset": offset})
}

// ConsultarCobrancasEstudante lista TODAS as cobranças (mensalidade,
// matrícula ou avulsa) já associadas a um estudante, em qualquer estado —
// diferente de ListarCobrancas (academia/admin, dentro do próprio
// contexto), esta rota é acessível ao próprio estudante para consultar o seu
// histórico completo de pagamentos, exatamente como ConsultarMensalidadesEstudante
// já faz para as obrigações de mensalidade (mesmo desenho de autorização em
// três vias: estudante só o próprio código, academia só com vínculo e
// restrita à própria academia, admin com permissão "fpp").
func ConsultarCobrancasEstudante(c *gin.Context) {
	codigo := strings.TrimSpace(c.Param("codigo"))
	var estudanteID string
	err := getDBClient(c).DB().QueryRowContext(c.Request.Context(), `SELECT id::text FROM projection_estudantes WHERE codigo_estudante=$1`, codigo).Scan(&estudanteID)
	if err == sql.ErrNoRows {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	actorID, typ, own, ok := financeActor(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	var somenteAcademia *string
	switch typ {
	case "estudante":
		if actorID.String() != estudanteID {
			utils.RespondWithForbiddenError(c, "você só pode consultar os seus próprios pagamentos")
			return
		}
	case "academia":
		if !academiaPossuiVinculoMensalidade(c, codigo, own) {
			utils.RespondWithForbiddenError(c, "estudante não pertence a esta academia")
			return
		}
		somenteAcademia = &own
	case "admin":
		if !financeAdminAllowed(c) {
			utils.RespondWithForbiddenError(c, "sem permissão financeira FPP")
			return
		}
	default:
		utils.RespondWithForbiddenError(c, "sem permissão para consultar pagamentos")
		return
	}
	turmaID, err := parseOptionalUUIDQuery(c, "turma_id")
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	cursoID, err := parseOptionalUUIDQuery(c, "curso_id")
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	anoAcademico := c.Query("ano_academico")
	anoLetivo := c.Query("ano_letivo")
	limit := parseBoundedInt(c.Query("limit"), 50, 1, 1000)
	offset := parseBoundedInt(c.Query("offset"), 0, 0, 1_000_000)
	estados := c.QueryArray("estado")
	origens := c.QueryArray("tipo")
	// pendências sem cobrança são sempre calculadas aqui (sem exigir nenhum
	// filtro extra de escopo): esta consulta já está inerentemente
	// delimitada a UM estudante, então não há o mesmo risco de varredura
	// sem limite que existe em ListarCobrancas. Ver
	// finance.PendenciasSemCobrancaEstudante.
	//
	// Mesmo assim, um filtro de estado/tipo que exclua explicitamente
	// "pendente"/"mensalidade" deve excluir as pendências sintéticas do
	// resultado — ver finance.DeveIncluirPendenciasSemCobranca.
	var pendencias []finance.MensalidadeMesView
	if finance.DeveIncluirPendenciasSemCobranca(estados, origens) {
		pendencias, err = FinanceiroService.PendenciasSemCobrancaEstudante(c.Request.Context(), codigo, somenteAcademia)
		if err != nil {
			financeError(c, err)
			return
		}
		pendencias, err = FinanceiroService.FiltrarPendenciasComCobrancaRealVinculada(c.Request.Context(), pendencias)
		if err != nil {
			financeError(c, err)
			return
		}
	}
	// mes não é exposto como parâmetro de query nesta rota ainda (só em
	// GET /financeiro/cobrancas, tarefa 60) — passamos nil para manter o
	// comportamento anterior inalterado aqui.
	res, err := finance.ListarPagamentosUnificado(pendencias, func(limitCobrancas, offsetCobrancas int) (*finance.CobrancaListResult, error) {
		return FinanceiroService.ListCobrancasEstudante(c.Request.Context(), codigo, somenteAcademia, estados, origens, turmaID, cursoID, anoAcademico, anoLetivo, nil, limitCobrancas, offsetCobrancas)
	}, limit, offset)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"pagamentos": res.Pagamentos, "total": len(res.Pagamentos), "total_geral": res.Total, "limit": limit, "offset": offset})
}

// CancelarCobranca intentionally does not use authorizeFinanceScope:
// FPP admins may cancel only Spuri's own charges, never a charge belonging to
// an academy. The service repeats this ownership check before recording.
func CancelarCobranca(c *gin.Context) {
	var in struct {
		Motivo string `json:"motivo"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	id, actorType, ownAcademy, ok := financeActor(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	contexto, academia := "", ""
	switch actorType {
	case "academia":
		contexto, academia = finance.ContextoAcademia, ownAcademy
	case "admin":
		if !financeAdminAllowed(c) {
			utils.RespondWithForbiddenError(c, "sem permissão para cancelar esta cobrança")
			return
		}
		contexto = finance.ContextoSpuri
	default:
		utils.RespondWithForbiddenError(c, "sem permissão para cancelar esta cobrança")
		return
	}
	out, err := FinanceiroService.CancelCharge(c.Request.Context(), contexto, academia, c.Param("id"), in.Motivo, id.String(), actorType, c.ClientIP())
	if err != nil {
		// Um pagamento descoberto durante o cancelamento ainda precisa dos
		// efeitos de confirmação (matrícula, serviço extra) — o mesmo que o
		// webhook e a consulta executam.
		if errors.Is(err, finance.ErrPagamentoExistente) {
			if effectErr := executarEfeitosPagamentoConfirmado(c, c.Param("id"), id.String(), actorType); effectErr != nil {
				utils.RespondWithInternalError(c, effectErr)
				return
			}
		}
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// ReceberWebhook devolve o handler público de notificações de um provedor
// para um método (rota /webhooks/<slug>/<método>). Quem autentica a
// notificação, lê o corpo e define a resposta HTTP é o próprio provedor; o
// restante (idempotência, ledger, efeitos de negócio) é comum a todos.
func ReceberWebhook(provedorID, metodo string) gin.HandlerFunc {
	return func(c *gin.Context) {
		prov, err := FinanceiroService.ProvedorPorID(provedorID)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		owner, err := FinanceiroService.AuthenticateWebhook(c.Request.Context(), prov.ID(), c.Request.Header)
		if err != nil {
			c.Status(http.StatusUnauthorized)
			return
		}
		corpo, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		eventID, payload, err := prov.InterpretarWebhook(c.ContentType(), corpo)
		if err != nil || eventID == "" {
			c.Status(http.StatusBadRequest)
			return
		}
		_, confirmedSuccess, err := FinanceiroService.AcceptWebhook(c.Request.Context(), prov.ID(), metodo, eventID, owner, payload)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		if confirmedSuccess {
			if codigo, err := FinanceiroService.CodigoSolicitacaoDaCobranca(c.Request.Context(), eventID); err == nil && codigo != "" {
				if err := efetivarVinculoMatriculaPaga(c, codigo); err != nil {
					c.Status(http.StatusInternalServerError)
					return
				}
			}
			if codigo, tipo, mes, ano, err := FinanceiroService.DadosServicoExtraDaCobranca(c.Request.Context(), eventID); err == nil && codigo != "" {
				switch tipo {
				case "taxa_inscricao":
					if err := efetivarVinculoServicoExtraPago(c, codigo); err != nil {
						c.Status(http.StatusInternalServerError)
						return
					}
				case "mensalidade", "preco_unico":
					_ = FinanceiroService.ConfirmarLancamentoServicoExtraPago(c.Request.Context(), codigo, tipo, ano, mes, "pagamento:servico_extra", "sistema", c.ClientIP())
				}
			}
		}
		status, contentType, resposta := prov.RespostaWebhook(true)
		if resposta == "" {
			c.Status(status)
			return
		}
		c.Data(status, contentType, []byte(resposta))
	}
}
