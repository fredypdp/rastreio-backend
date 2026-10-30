package services

import (
	"errors"
	"fmt"
	"html"
	"log"
	"os"
	"strings"
)

// Tarefa 122 — e-mail com o código da verificação em duas etapas.
//
// Canal: API HTTP do Brevo, o mesmo usado pelo e-mail de verificação.
// Diferente dos avisos "melhor esforço" (que só registam no log quando o Brevo
// não está configurado), aqui a falha é REAL: sem o e-mail o usuário não
// consegue concluir o login nem a ativação. Por isso, sem Brevo configurado
// devolve-se ErrEmailNaoConfigurado (falha fechada) — nunca se "finge" que
// enviou. Para desenvolvimento local existe a opção explícita
// DOIS_FATORES_LOG_CODIGO=true, que registra o código no log do servidor e é
// IGNORADA quando ENV=production.

// ErrEmailNaoConfigurado indica que o canal de e-mail não está configurado.
var ErrEmailNaoConfigurado = errors.New("serviço de e-mail não configurado")

// EnviarCodigoDoisFatores implementa DoisFatoresSender.
func (s *EmailService) EnviarCodigoDoisFatores(destinatario, nome, finalidade, codigo string, validadeMinutos int) error {
	if strings.TrimSpace(destinatario) == "" {
		return fmt.Errorf("email vazio")
	}

	cfg, ok := loadBrevoConfig()
	if !ok {
		if strings.EqualFold(strings.TrimSpace(os.Getenv("DOIS_FATORES_LOG_CODIGO")), "true") &&
			!strings.EqualFold(strings.TrimSpace(os.Getenv("ENV")), "production") {
			log.Printf("[2FA] ⚠️  Brevo não configurado — código de desenvolvimento para %s (%s): %s", destinatario, finalidade, codigo)
			return nil
		}
		return ErrEmailNaoConfigurado
	}

	assunto := assuntoCodigoDoisFatores(finalidade)
	htmlBody := renderDoisFatoresEmailHTML(nome, finalidade, codigo, validadeMinutos, s.frontendURL)
	textBody := renderDoisFatoresEmailText(nome, finalidade, codigo, validadeMinutos)

	if err := sendBrevoEmail(cfg, destinatario, nome, assunto, textBody, htmlBody); err != nil {
		return fmt.Errorf("brevo: %w", err)
	}
	return nil
}

func assuntoCodigoDoisFatores(finalidade string) string {
	switch finalidade {
	case DoisFatoresFinalidadeAtivacao:
		return "Ative a verificação em duas etapas - Spuri"
	case DoisFatoresFinalidadeDesativacao:
		return "Desative a verificação em duas etapas - Spuri"
	default:
		return "Seu código de verificação - Spuri"
	}
}

// textosFinalidadeDoisFatores devolve (título, frase de contexto) por finalidade.
func textosFinalidadeDoisFatores(finalidade string) (string, string) {
	switch finalidade {
	case DoisFatoresFinalidadeAtivacao:
		return "Confirme a ativação",
			"Recebemos um pedido para ativar a verificação em duas etapas na sua conta. Digite o código abaixo para concluir a ativação."
	case DoisFatoresFinalidadeDesativacao:
		return "Confirme a desativação",
			"Recebemos um pedido para desativar a verificação em duas etapas da sua conta. Digite o código abaixo para concluir a desativação."
	default:
		return "Seu código de acesso",
			"Alguém acabou de entrar na sua conta com a sua senha. Digite o código abaixo para concluir o acesso."
	}
}

// renderDoisFatoresEmailHTML monta o e-mail (invólucro de marca já existente).
// nome vem do usuário e é escapado; o código é composto só por dígitos.
func renderDoisFatoresEmailHTML(userName, finalidade, codigo string, validadeMinutos int, frontendURL string) string {
	titulo, contexto := textosFinalidadeDoisFatores(finalidade)
	safeName := html.EscapeString(userName)
	safeCodigo := html.EscapeString(codigo)

	bodyHTML := fmt.Sprintf(`
      <p style="margin:0 0 6px; font-family:%s; font-size:12px; font-weight:700; letter-spacing:0.08em; text-transform:uppercase; color:%s;">Verificação em duas etapas</p>
      <h1 class="spuri-h1 spuri-heading" style="margin:0 0 16px; font-family:%s; font-size:24px; line-height:1.3; font-weight:700; color:%s;">%s</h1>
      <p class="spuri-body-text" style="margin:0; font-family:%s; font-size:15px; line-height:1.65; color:%s;">
        Olá, <strong style="color:%s;">%s</strong>! %s
      </p>

      <p class="spuri-mono-box" style="margin:28px 0 0; padding:18px 16px; background-color:%s; border:1px solid %s; border-radius:12px; text-align:center; font-family:%s; font-size:34px; font-weight:700; letter-spacing:0.35em; color:%s;">%s</p>

      <div style="margin-top:20px; text-align:center;">%s</div>

      %s

      <p class="spuri-muted" style="margin:24px 0 0; font-family:%s; font-size:12px; line-height:1.6; color:%s; text-align:center;">
        Se não foi você, ignore este e-mail e altere a sua senha o quanto antes.
      </p>
    `,
		emailTplFontStack, emailTplBrandBlue,
		emailTplFontStack, emailTplNavy, html.EscapeString(titulo),
		emailTplFontStack, emailTplGray700,
		emailTplNavy, safeName, html.EscapeString(contexto),
		emailTplGray50, emailTplGray100, emailTplMonoStack, emailTplNavy, safeCodigo,
		renderEmailPill(fmt.Sprintf("Este código expira em %d minutos", validadeMinutos)),
		renderEmailAlert("warning", "Nunca partilhe este código",
			"A equipa do Spuri nunca pede este código por telefone, mensagem ou e-mail."),
		emailTplFontStack, emailTplGray500,
	)

	return renderEmailShell(
		"O seu código de verificação em duas etapas do Spuri.",
		assuntoCodigoDoisFatores(finalidade),
		bodyHTML,
		frontendURL,
	)
}

// renderDoisFatoresEmailText é a versão em texto simples.
func renderDoisFatoresEmailText(userName, finalidade, codigo string, validadeMinutos int) string {
	_, contexto := textosFinalidadeDoisFatores(finalidade)
	return fmt.Sprintf(
		"Olá %s! %s\n\nSeu código: %s\n\nEste código expira em %d minutos. Nunca partilhe este código: a equipa do Spuri nunca o pede.\n\nSe não foi você, ignore este e-mail e altere a sua senha o quanto antes.",
		userName, contexto, codigo, validadeMinutos,
	)
}
