// internal/services/email_html_templates.go
package services

import (
	"bytes"
	"fmt"
	"html"
	"strings"
	"text/template"
	"time"
)

// ============================================================================
// DESIGN SYSTEM DO E-MAIL (Go) — porte 1:1 de src/lib/email/email-service.ts
// ----------------------------------------------------------------------------
// Este arquivo replica, no backend, o MESMO design system de e-mail que já
// existia no frontend (src/lib/email/email-service.ts): mesmas cores da
// marca, mesmo logótipo, mesmo invólucro (shell) com cabeçalho/rodapé, e os
// mesmos dois templates de "Verificação de E-mail" e "Senha Resetada".
//
// Motivo: o backend passou a ser responsável por enviar estes dois e-mails
// diretamente (ver SolicitarVerificacaoEmail / SolicitarRecuperacaoSenha em
// internal/handlers/auth_email_handlers.go), e o frontend deixou de os
// enviar. Para o e-mail entregue manter a MESMA identidade visual já
// validada, o HTML precisou de ser portado para Go — não existia, até esta
// tarefa, uma versão do design completo (com logótipo, dark-mode e os
// blocos de alerta) escrita em Go; só existia uma versão simplificada,
// inline em email_service.go, usada exclusivamente para o aviso de "nova
// instituição cadastrada" (renderAcademiaCadastradaHTML).
//
// Qualquer alteração de cor/tipografia deve ser feita AQUI e, em espelho,
// em email-service.ts no frontend — as duas cópias não se importam uma da
// outra (repositórios different), por isso divergem se só uma for editada.
// ============================================================================

const (
	emailTplBrand50       = "#ECF3FF"
	emailTplBrand100      = "#DDE9FF"
	emailTplBrandBlue     = "#465FFF" // brand-500 da plataforma
	emailTplBrandBlueDark = "#3641F5" // brand-600 (tom mais escuro para texto/links)
	emailTplLogoBlue      = "#0873BD" // azul do "pin" do logótipo
	emailTplNavy          = "#172741" // cor do wordmark "Spuri"
	emailTplWhite         = "#FFFFFF"
	emailTplGray50        = "#F9FAFB"
	emailTplGray100       = "#F2F4F7"
	emailTplGray200       = "#E4E7EC"
	emailTplGray400       = "#98A2B3"
	emailTplGray500       = "#667085"
	emailTplGray700       = "#344054"
	emailTplWarning50     = "#FFFAEB"
	emailTplWarning200    = "#FEDF89"
	emailTplWarning700    = "#B54708"
	emailTplWarningText   = "#8B5109"

	emailTplFontStack = "'Outfit','Segoe UI',-apple-system,BlinkMacSystemFont,Roboto,Helvetica,Arial,sans-serif"
	emailTplMonoStack = "'SFMono-Regular',Consolas,'Liberation Mono',Menlo,monospace"
)

// emailLogoURL espelha getLogoUrl() do frontend: usa a mesma variável de
// ambiente/base pública (FRONTEND_URL no backend, equivalente a
// NEXT_PUBLIC_APP_URL no frontend) e aponta para o MESMO ficheiro já
// publicado pelo frontend em /public/images/email/spuri-logo-email.png —
// esse ficheiro não deve ser removido do repositório do frontend, mesmo que
// deixe de ser referenciado por código TypeScript, pois o backend passa a
// depender dele.
func emailLogoURL(frontendURL string) string {
	return strings.TrimRight(frontendURL, "/") + "/images/email/spuri-logo-email.png"
}

// renderEmailButton porta renderButton() do frontend.
func renderEmailButton(href, label string) string {
	return fmt.Sprintf(`
  <table role="presentation" cellpadding="0" cellspacing="0" align="center" style="margin:28px auto 0;">
    <tr>
      <td style="border-radius:12px; background-color:%s;">
        <a href="%s" target="_blank"
           style="display:inline-block; padding:14px 34px; font-family:%s; font-size:15px; font-weight:600; color:%s !important; text-decoration:none; border-radius:12px;">
          %s
        </a>
      </td>
    </tr>
  </table>`, emailTplBrandBlue, href, emailTplFontStack, emailTplWhite, label)
}

// renderEmailPill porta renderPill() do frontend.
func renderEmailPill(label string) string {
	return fmt.Sprintf(`<span style="display:inline-block; padding:6px 14px; border-radius:999px; background-color:%s; border:1px solid %s; font-family:%s; font-size:12px; font-weight:600; color:%s;">%s</span>`,
		emailTplWarning50, emailTplWarning200, emailTplFontStack, emailTplWarning700, label)
}

// renderEmailAlert porta renderAlert() do frontend. tone deve ser "warning"
// ou "brand" (qualquer outro valor cai no visual "brand").
func renderEmailAlert(tone, title, bodyHTML string) string {
	bg, border, titleColor, textColor := emailTplBrand50, emailTplBrand100, emailTplBrandBlueDark, emailTplGray700
	if tone == "warning" {
		bg, border, titleColor, textColor = emailTplWarning50, emailTplWarning200, emailTplWarning700, emailTplWarningText
	}
	return fmt.Sprintf(`
  <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" class="spuri-alert" style="margin-top:16px; border-radius:16px; background-color:%s; border:1px solid %s;">
    <tr>
      <td style="padding:16px 20px;">
        <p style="margin:0 0 4px; font-family:%s; font-size:13px; font-weight:700; color:%s;">%s</p>
        <div style="font-family:%s; font-size:13px; line-height:1.65; color:%s;">%s</div>
      </td>
    </tr>
  </table>`, bg, border, emailTplFontStack, titleColor, title, emailTplFontStack, textColor, bodyHTML)
}

// emailShellData contém os campos nomeados usados por emailShellTemplate.
// Usar text/template (em vez de fmt.Sprintf) aqui é deliberado: o invólucro
// tem dezenas de valores repetidos (cores, fonte, ano), e um Sprintf
// posicional com essa quantidade de "%s" seria fácil de errar na ordem sem
// que go vet ou o build acusassem nada (os tipos batem, só o valor em cada
// posição estaria trocado). Nomear cada campo elimina essa classe de erro.
type emailShellData struct {
	Title        string
	Preheader    string
	PreheaderPad string
	BodyHTML     string
	LogoURL      string
	Year         int

	Brand     string
	BrandBlue string
	LogoBlue  string
	Navy      string
	White     string
	Gray50    string
	Gray100   string
	Gray200   string
	Gray400   string
	Gray500   string
	FontStack string
}

// emailShellTemplate porta renderEmailShell() do frontend, incluindo o
// suporte a dark mode via @media (prefers-color-scheme: dark) e o ajuste
// responsivo via @media (max-width: 620px).
var emailShellTemplate = template.Must(template.New("emailShell").Parse(`<!DOCTYPE html>
<html lang="pt-AO">
<head>
<meta charset="UTF-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0" />
<meta name="color-scheme" content="light" />
<meta name="supported-color-schemes" content="light" />
<title>{{.Title}}</title>
<!--[if mso]>
<style>table{border-collapse:collapse;}</style>
<![endif]-->
<style>
  @media only screen and (max-width: 620px) {
    .spuri-card { border-radius: 18px !important; }
    .spuri-px { padding-left: 22px !important; padding-right: 22px !important; }
    .spuri-h1 { font-size: 21px !important; }
    .spuri-pass { font-size: 22px !important; }
  }
  @media (prefers-color-scheme: dark) {
    .spuri-bg { background-color: #0C111D !important; }
    .spuri-card { background-color: #101828 !important; border-color: #1D2939 !important; }
    .spuri-header, .spuri-footer { background-color: #101828 !important; border-color: #1D2939 !important; }
    .spuri-heading, .spuri-brandname { color: #F5F7FA !important; }
    .spuri-body-text { color: #CDD5DF !important; }
    .spuri-muted { color: #98A2B3 !important; }
    .spuri-mono-box, .spuri-alert, .spuri-pass-box { background-color: #16202E !important; border-color: #1D2939 !important; }
    .spuri-mono-box a { color: #A9C1FF !important; }
    .spuri-pass { color: #F5F7FA !important; }
  }
</style>
</head>
<body style="margin:0; padding:0; background-color:{{.Gray50}}; -webkit-text-size-adjust:100%;">
  <div style="display:none; max-height:0; overflow:hidden; mso-hide:all; font-size:1px; line-height:1px; color:{{.Gray50}};">
    {{.Preheader}}{{.PreheaderPad}}
  </div>

  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" class="spuri-bg" style="background-color:{{.Gray50}};">
    <tr>
      <td align="center" style="padding: 40px 16px;">
        <table role="presentation" width="600" cellpadding="0" cellspacing="0" class="spuri-card" style="width:100%; max-width:600px; background-color:{{.White}}; border-radius:24px; border:1px solid {{.Gray200}};">

          <tr>
            <td style="border-radius:24px 24px 0 0; height:5px; line-height:5px; font-size:0; background-color:{{.BrandBlue}}; background-image:linear-gradient(90deg, {{.LogoBlue}}, {{.BrandBlue}});">&nbsp;</td>
          </tr>

          <tr>
            <td class="spuri-header spuri-px" style="padding: 30px 40px 22px; text-align:center; border-bottom:1px solid {{.Gray100}};">
              <img src="{{.LogoURL}}" width="34" height="47" alt="Spuri" style="display:block; margin:0 auto 8px; border:0;" />
              <span class="spuri-brandname" style="font-family:{{.FontStack}}; font-size:18px; font-weight:700; color:{{.Navy}}; letter-spacing:-0.01em;">Spuri</span>
            </td>
          </tr>

          <tr>
            <td class="spuri-px" style="padding: 40px;">
              {{.BodyHTML}}
            </td>
          </tr>

          <tr>
            <td class="spuri-footer spuri-px" style="background-color:{{.Gray50}}; padding: 26px 40px; text-align:center; border-top:1px solid {{.Gray100}}; border-radius:0 0 24px 24px;">
              <p class="spuri-brandname" style="margin:0 0 4px; font-family:{{.FontStack}}; font-size:13px; font-weight:600; color:{{.Navy}};">Spuri</p>
              <p class="spuri-muted" style="margin:0 0 10px; font-family:{{.FontStack}}; font-size:12px; color:{{.Gray500}};">Confiança e eficiência na gestão académica.</p>
              <p style="margin:0; font-family:{{.FontStack}}; font-size:12px;">
                <a href="mailto:spuriartipan@gmail.com" style="color:{{.BrandBlue}} !important; text-decoration:none;">spuriartipan@gmail.com</a>
              </p>
              <p class="spuri-muted" style="margin:14px 0 0; font-family:{{.FontStack}}; font-size:11px; color:{{.Gray400}};">© {{.Year}} Spuri. Todos os direitos reservados.</p>
            </td>
          </tr>

        </table>
      </td>
    </tr>
  </table>
</body>
</html>`))

// renderEmailShell porta renderEmailShell() do frontend. bodyHTML já deve
// vir pronto (composto por renderEmailButton/renderEmailAlert/etc. e por
// texto já escapado com html.EscapeString onde vier de dados do usuário) —
// esta função não escapa bodyHTML, pois ele é HTML por definição.
func renderEmailShell(preheader, title, bodyHTML, frontendURL string) string {
	data := emailShellData{
		Title:        html.EscapeString(title),
		Preheader:    html.EscapeString(preheader),
		PreheaderPad: strings.Repeat("&#8199;&zwnj;", 60),
		BodyHTML:     bodyHTML, // text/template não escapa nada; bodyHTML já vem pronto (montado internamente por este pacote, nunca direto do usuário)
		LogoURL:      emailLogoURL(frontendURL),
		Year:         time.Now().Year(),
		Brand:        emailTplBrand50,
		BrandBlue:    emailTplBrandBlue,
		LogoBlue:     emailTplLogoBlue,
		Navy:         emailTplNavy,
		White:        emailTplWhite,
		Gray50:       emailTplGray50,
		Gray100:      emailTplGray100,
		Gray200:      emailTplGray200,
		Gray400:      emailTplGray400,
		Gray500:      emailTplGray500,
		FontStack:    emailTplFontStack,
	}
	var buf bytes.Buffer
	// O template é compilado uma única vez em tempo de inicialização
	// (template.Must acima) — um erro de execução aqui só pode vir de um
	// campo em falta, o que get testado em email_html_templates_test.go.
	if err := emailShellTemplate.Execute(&buf, data); err != nil {
		// Nunca deve acontecer (template estático + dados sempre completos);
		// preservar o comportamento "nunca bloquear o envio" devolvendo um
		// HTML mínimo em vez de entrar em pânico.
		return fmt.Sprintf("<html><body>%s</body></html>", html.EscapeString(bodyHTML))
	}
	return buf.String()
}

// ============================================================================
// Template: Verificação de E-mail — porta sendVerificationEmail() do frontend
// ============================================================================

// renderVerificationEmailHTML monta o corpo + invólucro do e-mail de
// verificação, com a MESMA cópia e estrutura do frontend.
func renderVerificationEmailHTML(userName, verificationURL, frontendURL string) string {
	safeName := html.EscapeString(userName)
	safeURL := html.EscapeString(verificationURL)

	bodyHTML := fmt.Sprintf(`
      <p style="margin:0 0 6px; font-family:%s; font-size:12px; font-weight:700; letter-spacing:0.08em; text-transform:uppercase; color:%s;">Verificação de e-mail</p>
      <h1 class="spuri-h1 spuri-heading" style="margin:0 0 16px; font-family:%s; font-size:24px; line-height:1.3; font-weight:700; color:%s;">Confirme o seu e-mail</h1>
      <p class="spuri-body-text" style="margin:0; font-family:%s; font-size:15px; line-height:1.65; color:%s;">
        Olá, <strong style="color:%s;">%s</strong>! Obrigado por se registrar no Spuri.
        Para complementar o seu cadastro, precisamos verificar o seu endereço de e-mail.
      </p>

      <div style="text-align:center;">%s</div>

      <p class="spuri-muted" style="margin:28px 0 8px; font-family:%s; font-size:13px; color:%s;">
        Se o botão não funcionar, copie e cole este link no navegador:
      </p>
      <p class="spuri-mono-box" style="margin:0; padding:12px 16px; background-color:%s; border:1px solid %s; border-radius:10px;">
        <a href="%s" style="font-family:%s; font-size:12px; word-break:break-all; color:%s !important; text-decoration:none;">%s</a>
      </p>

      <div style="margin-top:20px; text-align:center;">%s</div>

      <p class="spuri-muted" style="margin:24px 0 0; font-family:%s; font-size:12px; line-height:1.6; color:%s; text-align:center;">
        Se você não solicitou esta verificação, por favor ignore este e-mail.
      </p>
    `,
		emailTplFontStack, emailTplBrandBlue,
		emailTplFontStack, emailTplNavy,
		emailTplFontStack, emailTplGray700,
		emailTplNavy, safeName,
		renderEmailButton(safeURL, "Verificar E-mail"),
		emailTplFontStack, emailTplGray500,
		emailTplGray50, emailTplGray100,
		safeURL, emailTplMonoStack, emailTplBrandBlueDark, safeURL,
		renderEmailPill("Este link expira em 24 horas"),
		emailTplFontStack, emailTplGray500,
	)

	return renderEmailShell(
		"Confirme o seu e-mail para ativar por completo a sua conta Spuri.",
		"Verificação de E-mail - Spuri",
		bodyHTML,
		frontendURL,
	)
}

// renderVerificationEmailText porta a versão em texto simples do mesmo
// e-mail (fallback para clientes sem suporte a HTML).
func renderVerificationEmailText(userName, verificationURL string) string {
	return fmt.Sprintf(
		"Olá %s! Para verificar seu e-mail, acesse: %s\n\nEste link expira em 24 horas.\n\nSe você não solicitou esta verificação, por favor ignore este e-mail.",
		userName, verificationURL,
	)
}

// ============================================================================
// Template: Recuperação de Senha — porta sendPasswordResetEmail() do frontend
// ----------------------------------------------------------------------------
// Diferença deliberada face ao frontend: o botão "Ir para Alteração de
// Senha" do template original aponta para `${FRONTEND_URL}/recuperar-senha/
// ${token}` — uma página que não existe no frontend (a troca já aconteceu
// no momento em que este e-mail é enviado, tanto no fluxo antigo quanto
// neste). Ver observação e decisão no documento de tarefa: o botão foi
// mantido apontando para a MESMA URL (fidelidade ao design pedido), agora
// direcionada para a página de login, que é o destino que realmente existe
// e faz sentido depois de uma senha temporária ser emitida.
// ============================================================================

// renderPasswordResetEmailHTML monta o corpo + invólucro do e-mail de senha
// resetada, com a MESMA cópia e estrutura do frontend (bloco de senha
// temporária em destaque, alerta de segurança, dica de boas práticas).
func renderPasswordResetEmailHTML(userName, tempPassword, loginURL, frontendURL string) string {
	safeName := html.EscapeString(userName)
	safePassword := html.EscapeString(tempPassword)

	bodyHTML := fmt.Sprintf(`
      <p style="margin:0 0 6px; font-family:%s; font-size:12px; font-weight:700; letter-spacing:0.08em; text-transform:uppercase; color:%s;">Recuperação de senha</p>
      <h1 class="spuri-h1 spuri-heading" style="margin:0 0 16px; font-family:%s; font-size:24px; line-height:1.3; font-weight:700; color:%s;">A sua senha foi redefinida</h1>
      <p class="spuri-body-text" style="margin:0; font-family:%s; font-size:15px; line-height:1.65; color:%s;">
        Olá, <strong style="color:%s;">%s</strong>! Sua senha foi resetada com sucesso. Geramos uma nova senha temporária para a sua conta.
      </p>

      <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" class="spuri-pass-box" style="margin-top:24px; border-radius:16px; background-color:%s; border:1px solid %s;">
        <tr>
          <td style="padding:22px; text-align:center;">
            <p style="margin:0 0 8px; font-family:%s; font-size:12px; font-weight:600; color:%s;">Sua senha temporária</p>
            <p class="spuri-pass" style="margin:0; font-family:%s; font-size:26px; font-weight:700; letter-spacing:0.05em; color:%s; word-break:break-all;">%s</p>
          </td>
        </tr>
      </table>

      <div style="text-align:center;">%s</div>

      %s

      %s

      <p class="spuri-muted" style="margin:24px 0 0; font-family:%s; font-size:12px; line-height:1.6; color:%s; text-align:center;">
        Se você não solicitou a recuperação de senha, entre em contato conosco imediatamente em
        <a href="mailto:spuriartipan@gmail.com" style="color:%s !important;">spuriartipan@gmail.com</a>.
      </p>
    `,
		emailTplFontStack, emailTplBrandBlue,
		emailTplFontStack, emailTplNavy,
		emailTplFontStack, emailTplGray700,
		emailTplNavy, safeName,
		emailTplBrand50, emailTplBrand100,
		emailTplFontStack, emailTplBrandBlueDark,
		emailTplMonoStack, emailTplNavy, safePassword,
		renderEmailButton(loginURL, "Ir para o Login"),
		renderEmailAlert("warning", "⚠️ Importante", "Por motivos de segurança, altere esta senha imediatamente após fazer login. Acesse seu perfil e vá em <strong>&ldquo;Alterar Senha&rdquo;</strong>."),
		renderEmailAlert("brand", "🔒 Dicas de Segurança", "Nunca compartilhe sua senha com ninguém. Use uma senha forte com letras, números e símbolos. Não use a mesma senha em diferentes serviços."),
		emailTplFontStack, emailTplGray500,
		emailTplBrandBlue,
	)

	return renderEmailShell(
		"Geramos uma nova senha temporária para a sua conta Spuri. Consulte com segurança dentro do e-mail.",
		"Senha Resetada - Spuri",
		bodyHTML,
		frontendURL,
	)
}

// renderPasswordResetEmailText porta a versão em texto simples do mesmo
// e-mail (fallback para clientes sem suporte a HTML).
func renderPasswordResetEmailText(userName, tempPassword, loginURL string) string {
	return fmt.Sprintf(`Olá %s!

Sua senha foi resetada com sucesso.

SENHA TEMPORÁRIA: %s

IMPORTANTE: Altere esta senha imediatamente após fazer login por motivos de segurança.

Você pode fazer login em:
%s

Se você não solicitou esta recuperação, entre em contato conosco imediatamente em spuriartipan@gmail.com.

---
© %d Spuri - Todos os direitos reservados
`, userName, tempPassword, loginURL, time.Now().Year())
}
