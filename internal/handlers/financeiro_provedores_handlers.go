package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/finance"
	"spuri/internal/utils"
)

// Handlers do módulo de provedores de pagamento (PSP): catálogo, provedor
// ativo de cada contexto (Spuri ou academia) e credenciais por provedor.
//
// A autorização é a mesma de todo o módulo financeiro (authorizeFinanceScope):
// a academia age sobre si mesma; o admin com permissão "fpp" consulta qualquer
// contexto, mas só escreve no contexto "spuri".

// ListarProvedores devolve o catálogo de provedores implementados, com os
// campos de credencial de cada um (para o frontend montar o formulário) e os
// métodos de pagamento que suportam. Não contém segredos.
func ListarProvedores(c *gin.Context) {
	c.JSON(http.StatusOK, FinanceiroService.Provedores())
}

// ConsultarProvedorContexto devolve o provedor ativo do contexto e, para cada
// provedor, se há credenciais e quantas cobranças pendentes.
func ConsultarProvedorContexto(c *gin.Context) {
	contexto := c.Query("contexto_tipo")
	academia := c.Query("codigo_academia")
	if !authorizeFinanceScope(c, &contexto, &academia, false) {
		utils.RespondWithForbiddenError(c, "sem permissão para consultar o provedor de pagamento")
		return
	}
	out, err := FinanceiroService.ConsultarProvedorContexto(c.Request.Context(), contexto, academia)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// DefinirProvedorContexto define o provedor (único) do contexto. Cobranças já
// emitidas continuam seguindo o provedor que as emitiu.
func DefinirProvedorContexto(c *gin.Context) {
	var in struct {
		ContextoTipo   string `json:"contexto_tipo"`
		CodigoAcademia string `json:"codigo_academia"`
		Provedor       string `json:"provedor"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	if !authorizeFinanceScope(c, &in.ContextoTipo, &in.CodigoAcademia, true) {
		utils.RespondWithForbiddenError(c, "sem permissão para definir o provedor de pagamento")
		return
	}
	id, t, _, ok := financeActor(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	out, err := FinanceiroService.DefinirProvedor(c.Request.Context(), in.ContextoTipo, in.CodigoAcademia, in.Provedor, id.String(), t, c.ClientIP())
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// CredencialCriada é a resposta exclusiva de POST .../credenciais: é a única
// vez que o segredo de webhook aparece "de graça" numa resposta, fora do GET
// .../webhook-secret dedicado — porque é a única oportunidade em que o
// usuário ainda não tem como consultá-lo de outra forma. Só vem preenchido
// para provedores que autenticam webhooks por segredo.
type CredencialCriada struct {
	finance.CredentialView
	WebhookSecret string `json:"webhook_secret,omitempty"`
}

// ConfigurarCredencial grava as credenciais de um provedor para o contexto.
func ConfigurarCredencial(c *gin.Context) {
	var in finance.CredentialInput
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	configurarCredencial(c, nil, in, http.StatusCreated, func(v finance.CredentialView, secret string) any {
		return CredencialCriada{CredentialView: v, WebhookSecret: secret}
	})
}

// AtualizarCredencial atualiza credenciais já existentes (pelo id).
func AtualizarCredencial(c *gin.Context) {
	idParam, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.RespondWithValidationError(c, errors.New("id inválido"))
		return
	}
	var in finance.CredentialInput
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	// O segundo retorno (segredo em texto plano) só vem preenchido quando a
	// credencial ainda não tinha nenhum segredo de webhook — não deveria
	// acontecer numa atualização de credencial já existente; se acontecer, o
	// usuário ainda pode recuperá-lo em seguida via GET .../webhook-secret.
	configurarCredencial(c, &idParam, in, http.StatusOK, func(v finance.CredentialView, _ string) any { return v })
}

func configurarCredencial(c *gin.Context, id *uuid.UUID, in finance.CredentialInput, status int, resposta func(finance.CredentialView, string) any) {
	if !authorizeFinanceScope(c, &in.ContextoTipo, &in.CodigoAcademia, true) {
		utils.RespondWithForbiddenError(c, "sem permissão para configurar estas credenciais")
		return
	}
	actorID, t, _, ok := financeActor(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	out, webhookSecret, err := FinanceiroService.ConfigureCredential(c.Request.Context(), id, in, actorID.String(), t, c.ClientIP())
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(status, resposta(out, webhookSecret))
}

// ListarCredenciais lista as credenciais (mascaradas) de todos os provedores
// do contexto; o filtro opcional ?provedor= restringe a um provedor.
func ListarCredenciais(c *gin.Context) {
	contexto := c.Query("contexto_tipo")
	academia := c.Query("codigo_academia")
	if !authorizeFinanceScope(c, &contexto, &academia, false) {
		utils.RespondWithForbiddenError(c, "sem permissão para consultar credenciais financeiras")
		return
	}
	out, err := FinanceiroService.ListCredentials(c.Request.Context(), contexto, academia)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if provedor := strings.ToUpper(strings.TrimSpace(c.Query("provedor"))); provedor != "" {
		filtrado := out[:0]
		for _, v := range out {
			if v.Provedor == provedor {
				filtrado = append(filtrado, v)
			}
		}
		out = filtrado
	}
	c.JSON(http.StatusOK, out)
}

// RemoverCredencial remove as credenciais de um provedor do contexto. Nunca é
// bloqueada: se o provedor tiver cobranças pendentes no contexto, as credenciais
// ficam arquivadas (snapshot) só para concluí-las. Responde 200 com o resumo
// (provedor, cobranças pendentes e se ficaram arquivadas).
func RemoverCredencial(c *gin.Context) {
	var in struct {
		ContextoTipo   string `json:"contexto_tipo"`
		CodigoAcademia string `json:"codigo_academia"`
		Provedor       string `json:"provedor"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	removerCredencial(c, in.ContextoTipo, in.CodigoAcademia, in.Provedor)
}

func removerCredencial(c *gin.Context, contexto, academia, provedor string) {
	if !authorizeFinanceScope(c, &contexto, &academia, true) {
		utils.RespondWithForbiddenError(c, "sem permissão para remover estas credenciais")
		return
	}
	id, t, _, ok := financeActor(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	resultado, err := FinanceiroService.RemoveCredential(c.Request.Context(), contexto, academia, provedor, id.String(), t, c.ClientIP())
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resultado)
}

// ConsultarSegredoWebhook devolve o segredo de webhook atual em texto plano.
// Só o dono do contexto (a própria academia, ou admin com permissão "fpp")
// pode consultar.
func ConsultarSegredoWebhook(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.RespondWithValidationError(c, errors.New("id inválido"))
		return
	}
	if !credentialScopeAuthorized(c, id, false) {
		return
	}
	secret, err := FinanceiroService.WebhookSecret(c.Request.Context(), id)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"webhook_secret": secret, "webhook_header_name": finance.WebhookHeaderName})
}

// RotacionarSegredoWebhook gera um novo segredo de webhook, invalidando o
// anterior imediatamente. Mesma autorização de ConsultarSegredoWebhook.
func RotacionarSegredoWebhook(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.RespondWithValidationError(c, errors.New("id inválido"))
		return
	}
	if !credentialScopeAuthorized(c, id, true) {
		return
	}
	actorID, actorType, _, ok := financeActor(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	secret, err := FinanceiroService.RotateWebhookSecret(c.Request.Context(), id, actorID.String(), actorType, c.ClientIP())
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"webhook_secret": secret, "webhook_header_name": finance.WebhookHeaderName})
}
