package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/services"
)

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
	motivoAmbienteNaoProducao    = "ambiente_nao_producao"
	motivoProvedorNaoConfigurado = "provedor_nao_configurado"
	motivoFalhaEnvio             = "falha_envio"
	motivoEmFila                 = "em_fila"
	motivoTelefoneInvalido       = "telefone_invalido"
	motivoCanalEmailIndisponivel = "canal_indisponivel"
)

// resultadoCredenciais é o objeto "credenciais" devolvido nas respostas de criação.
type resultadoCredenciais struct {
	Canal   string `json:"canal"` // "sms" | "email"
	Enviada bool   `json:"enviada"`
	Motivo  string `json:"motivo,omitempty"`
}

// enviarCredenciaisSMS envia as credenciais pelo SMS padronizado. Só envia com
// ENV=production (mesma convenção do 2FA): fora disso não envia nem regista
// mensagem — com CREDENCIAIS_LOG_DEV=true (e nunca em produção) as credenciais
// vão para o log, para testes locais. reenvio escolhe o texto do SMS.
func enviarCredenciaisSMS(ctx context.Context, c *gin.Context, ator atorComunicacao, audit db.AuditContext,
	telefone, nomeCurto, identificador, senha string, reenvio bool) resultadoCredenciais {

	if !strings.EqualFold(strings.TrimSpace(os.Getenv("ENV")), "production") {
		services.LogCredenciaisDev(identificador, senha)
		return resultadoCredenciais{Canal: "sms", Motivo: motivoAmbienteNaoProducao}
	}
	texto := services.MensagemCredenciaisSMS(nomeCurto, identificador, senha)
	if reenvio {
		texto = services.MensagemCredenciaisReenviadasSMS(nomeCurto, identificador, senha)
	}
	msg, err := enviarSMSComunicacao(ctx, c, ator, telefone, texto, audit)
	switch {
	case errors.Is(err, errProvedorPadraoNaoConfigurado):
		return resultadoCredenciais{Canal: "sms", Motivo: motivoProvedorNaoConfigurado}
	case err != nil || msg == nil || msg.Status != aggregates.StatusMensagemComunicacaoEnviada:
		// Sem telefone/senha no log: só a categoria da falha.
		log.Printf("[credenciais] SMS de credenciais não enviado (ator %s): falha de envio", ator.Tipo)
		return resultadoCredenciais{Canal: "sms", Motivo: motivoFalhaEnvio}
	}
	return resultadoCredenciais{Canal: "sms", Enviada: true}
}

// TelefoneDestinoCredenciaisEstudante escolhe o telefone que recebe as
// credenciais: superior → telefone do estudante; escolar → telefone do
// encarregado; sem nível definido, o que existir. O resultado é normalizado
// para o formato 9XXXXXXXX exigido pelo envio de SMS. Chama-se ANTES de criar a
// conta, para não criar contas cujas credenciais não podem ser entregues.
func TelefoneDestinoCredenciaisEstudante(anoFund, anoMedio, anoSuperior, telEstudante, telEncarregado *string) (string, error) {
	preenchido := func(v *string) bool { return v != nil && strings.TrimSpace(*v) != "" }
	var bruto *string
	switch {
	case preenchido(anoSuperior):
		bruto = telEstudante
	case preenchido(anoFund) || preenchido(anoMedio):
		bruto = telEncarregado
	case preenchido(telEstudante):
		bruto = telEstudante
	default:
		bruto = telEncarregado
	}
	if !preenchido(bruto) {
		return "", fmt.Errorf("não há telefone para enviar as credenciais do estudante: informe o telefone do estudante (ensino superior) ou do encarregado (ensino escolar)")
	}
	destino, err := normalizarDestinatarioComunicacao(*bruto)
	if err != nil {
		return "", fmt.Errorf("o telefone que recebe as credenciais deve ser um número móvel angolano de 9 dígitos iniciado por 9 (ex.: 923456789): %w", err)
	}
	return destino, nil
}

// telefoneDestinoCredenciaisAcademia valida o telefone obrigatório da academia
// (destino do SMS de credenciais) e devolve-o normalizado (9XXXXXXXX).
func telefoneDestinoCredenciaisAcademia(telefone *string) (string, error) {
	if telefone == nil || strings.TrimSpace(*telefone) == "" {
		return "", fmt.Errorf("telefone é obrigatório: é para ele que as credenciais da academia são enviadas")
	}
	destino, err := normalizarDestinatarioComunicacao(*telefone)
	if err != nil {
		return "", fmt.Errorf("telefone deve ser um número móvel angolano de 9 dígitos iniciado por 9 (ex.: 923456789), pois recebe as credenciais por SMS")
	}
	return destino, nil
}
