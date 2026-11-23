package apoio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/services"
	"spuri/internal/utils"
)

// executarEnvioRecuperacao corre o envio do e-mail fora do ciclo da requisição,
// para que o tempo de resposta seja o mesmo com e sem conta. É uma variável
// apenas para os testes a executarem de forma síncrona.
var ExecutarEnvioRecuperacao = func(f func()) { go f() }

func RejectDedicatedContactFields(c *gin.Context) bool {
	var raw map[string]json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("body inválido"))
		return true
	}
	if _, ok := raw["telefone_encarregado"]; ok {
		msg := "O campo 'telefone_encarregado' não é aceito nesta rota. O telefone do encarregado pertence à conta do encarregado (PUT /me/telefone) ou é trocado pela academia (PUT /academia/estudante/:codigo/encarregado)."
		c.JSON(http.StatusBadRequest, gin.H{"error": "VALIDATION_ERROR", "message": msg, "details": []gin.H{{"field": "telefone_encarregado", "code": "campo_nao_permitido", "message": msg}}})
		return true
	}
	for _, field := range []string{"email", "telefone"} {
		if _, ok := raw[field]; ok {
			rota := "/me/email"
			if field == "telefone" {
				rota = "/me/telefone"
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": "VALIDATION_ERROR", "message": fmt.Sprintf("O campo '%s' não é aceito nesta rota. Use PUT %s para alterar o %s do usuário autenticado.", field, rota, field), "details": []gin.H{{"field": field, "code": "campo_nao_permitido", "message": fmt.Sprintf("Use PUT %s para alterar o %s.", rota, field)}}})
			return true
		}
	}
	b, _ := json.Marshal(raw)
	c.Request.Body = io.NopCloser(strings.NewReader(string(b)))
	return false
}

func RejectAcademiaDadosRestrictedFields(c *gin.Context) bool {
	var raw map[string]json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("body inválido"))
		return true
	}
	messages := map[string]string{
		"telefone":        "O campo 'telefone' não é aceito em PUT /academia/dados. Use PUT /me/telefone para alterar o telefone da academia pelo fluxo dedicado.",
		"email":           "O campo 'email' não é aceito em PUT /academia/dados. Use PUT /me/email para alterar o e-mail da academia pelo fluxo dedicado.",
		"anos_academicos": "O campo 'anos_academicos' não é aceito em PUT /academia/dados. Use POST/DELETE /academia/anos-academicos para adicionar ou remover anos acadêmicos.",
		"cursos":          "O campo 'cursos' não é aceito em PUT /academia/dados. Use as rotas /academia/curso para gerir cursos.",
		"type":            "O campo 'type' não é aceito em PUT /academia/dados. O tipo (público/privado) é definido apenas no cadastro da academia e não pode ser alterado posteriormente por nenhuma rota.",
		"nivel_escolar":   "O campo 'nivel_escolar' não é aceito em PUT /academia/dados. Use PUT /academia/nivel-escolar para alterar o nível escolar pelo fluxo dedicado, que valida dependências ativas antes de aplicar a mudança.",
		"nif":             "O campo 'nif' não é aceito em PUT /academia/dados. Use POST /academia/solicitacoes-nif para solicitar a alteração — a mudança só é aplicada após aprovação de um Admin (role adm ou fpp).",
	}
	for _, field := range []string{"telefone", "email", "anos_academicos", "cursos", "type", "nivel_escolar", "nif"} {
		if _, ok := raw[field]; ok {
			msg := messages[field]
			c.JSON(http.StatusBadRequest, gin.H{"error": "VALIDATION_ERROR", "message": msg, "details": []gin.H{{"field": field, "code": "campo_nao_permitido", "message": msg}}})
			return true
		}
	}
	b, _ := json.Marshal(raw)
	c.Request.Body = io.NopCloser(strings.NewReader(string(b)))
	return false
}

// Tarefa 123 (SEC-04) — entrega das credenciais temporárias.
//
// Toda conta criada por terceiros nasce com uma senha temporária de troca
// obrigatória (services.GenerateTempPassword). O evento é criado primeiro e o
// SMS é enviado DEPOIS do commit: falha de envio nunca desfaz a criação —
// devolve-se 201 com o objeto "credenciais" (enviada:false + motivo) e a conta
// fica elegível a reenvio. A senha em claro nunca entra em eventos (só o hash),
// nem em respostas, nem em logs.

// Motivos de "credenciais.enviada == false".
const (
	MotivoAmbienteNaoProducao    = "ambiente_nao_producao"
	motivoProvedorNaoConfigurado = "provedor_nao_configurado"
	MotivoFalhaEnvio             = "falha_envio"
	motivoEmFila                 = "em_fila"
	motivoTelefoneInvalido       = "telefone_invalido"
	MotivoCanalEmailIndisponivel = "canal_indisponivel"
)

// resultadoCredenciais é o objeto "credenciais" devolvido nas respostas de criação.
type ResultadoCredenciais struct {
	Canal   string `json:"canal"` // "sms" | "email"
	Enviada bool   `json:"enviada"`
	Motivo  string `json:"motivo,omitempty"`
}

// enviarCredenciaisSMS envia as credenciais pelo SMS padronizado. Só envia com
// ENV=production (mesma convenção do 2FA): fora disso não envia nem regista
// mensagem — com CREDENCIAIS_LOG_DEV=true (e nunca em produção) as credenciais
// vão para o log, para testes locais. reenvio escolhe o texto do SMS.
func EnviarCredenciaisSMS(ctx context.Context, c *gin.Context, ator AtorComunicacao, audit db.AuditContext,
	telefone, nomeCurto, identificador, senha string, reenvio bool) ResultadoCredenciais {

	if !strings.EqualFold(strings.TrimSpace(os.Getenv("ENV")), "production") {
		services.LogCredenciaisDev(identificador, senha)
		return ResultadoCredenciais{Canal: "sms", Motivo: MotivoAmbienteNaoProducao}
	}
	texto := services.MensagemCredenciaisSMS(nomeCurto, identificador, senha)
	if reenvio {
		texto = services.MensagemCredenciaisReenviadasSMS(nomeCurto, identificador, senha)
	}
	msg, err := EnviarSMSComunicacao(ctx, c, ator, telefone, texto, audit)
	switch {
	case errors.Is(err, ErrProvedorPadraoNaoConfigurado):
		return ResultadoCredenciais{Canal: "sms", Motivo: motivoProvedorNaoConfigurado}
	case err != nil || msg == nil || msg.Status != aggregates.StatusMensagemComunicacaoEnviada:
		// Sem telefone/senha no log: só a categoria da falha.
		log.Printf("[credenciais] SMS de credenciais não enviado (ator %s): falha de envio", ator.Tipo)
		return ResultadoCredenciais{Canal: "sms", Motivo: MotivoFalhaEnvio}
	}
	return ResultadoCredenciais{Canal: "sms", Enviada: true}
}

// (Parte 5) O telefone das credenciais do ESTUDANTE já não é escolhido aqui:
// vem da regra única de contato, services.ResolverContato /
// services.ResolverContatoDoEstudante, que no nível escolar usa sempre o
// encarregado e nunca cai no telefone do estudante.

// telefoneDestinoCredenciaisAcademia valida o telefone obrigatório da academia
// (destino do SMS de credenciais) e devolve-o normalizado (9XXXXXXXX).
func TelefoneDestinoCredenciaisAcademia(telefone *string) (string, error) {
	if telefone == nil || strings.TrimSpace(*telefone) == "" {
		return "", fmt.Errorf("telefone é obrigatório: é para ele que as credenciais da academia são enviadas")
	}
	destino, err := normalizarDestinatarioComunicacao(*telefone)
	if err != nil {
		return "", fmt.Errorf("telefone deve ser um número móvel angolano de 9 dígitos iniciado por 9 (ex.: 923456789), pois recebe as credenciais por SMS")
	}
	return destino, nil
}

// anexarDeveAlterarSenha acrescenta "deve_alterar_senha" a cada item de uma
// listagem (uma consulta por página, não uma por linha). O frontend usa-o para
// o indicador "credenciais não confirmadas" e para o botão "Reenviar
// credenciais". chave é o campo que identifica o item na listagem ("id",
// "codigo_estudante" ou "codigo_academia") e coluna a coluna SQL equivalente.
// Falha silenciosa: a lista nunca deixa de ser devolvida por causa deste indicador.
func AnexarDeveAlterarSenha(client *db.Client, tabela, coluna, chave string, itens []map[string]interface{}) {
	switch tabela + "/" + coluna {
	case "projection_estudantes/codigo_estudante", "projection_academias/codigo_academia", "projection_admins/id::text":
	default:
		return
	}
	ids := make([]string, 0, len(itens))
	for _, it := range itens {
		if id := fmt.Sprint(it[chave]); id != "" && id != "<nil>" {
			ids = append(ids, id)
		}
	}
	pendentes := map[string]bool{}
	if len(ids) > 0 {
		rows, err := client.DB().Query(`SELECT `+coluna+` FROM `+tabela+` WHERE deve_alterar_senha = TRUE AND `+coluna+` = ANY($1)`, pq.Array(ids))
		if err != nil {
			log.Printf("[WARN] anexarDeveAlterarSenha(%s): %v", tabela, err)
		} else {
			defer rows.Close()
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					pendentes[id] = true
				}
			}
		}
	}
	for _, it := range itens {
		it["deve_alterar_senha"] = pendentes[fmt.Sprint(it[chave])]
	}
}
