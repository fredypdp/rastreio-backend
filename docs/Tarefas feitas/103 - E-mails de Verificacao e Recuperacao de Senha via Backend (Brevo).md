# Tarefa — E-mails de Verificação e Recuperação de Senha via Backend (Brevo)

**Repositório:** `rastreio-backend`
**Depende de:** nada (pode ser aplicada isoladamente).
**Bloqueia:** a tarefa de frontend `Tarefa - Remover Envio de E-mails pelo Frontend e Notificacao Duplicada.md` — aquela só deve ser aplicada DEPOIS desta.
**Execução:** Codex só executa — todo o código abaixo já foi escrito, compilado (`go build ./...`), formatado (`gofmt`), validado (`go vet ./...`) e testado com PostgreSQL real pelo orquestrador, incluindo um teste de ponta a ponta que prova que a senha antiga deixa de funcionar depois do reset. Não há nada para planejar.

## Contexto (resumo do que muda e por quê)

Duas rotas do backend passam a controlar o envio completo do e-mail (token/senha + o e-mail em si), em vez de apenas gerar um token e devolvê-lo para o frontend enviar:

- `POST /email/verificar-email/solicitar` (`SolicitarVerificacaoEmail`, requer JWT) — já existia, só passou a enviar via **Brevo com HTML de marca** em vez de EmailJS.
- `POST /email/recuperar-senha/solicitar` (`SolicitarRecuperacaoSenha`, pública) — mudou de comportamento: antes só enviava um **link** (via EmailJS) para uma página `/recuperar-senha/:token` que **nunca existiu** no frontend. Agora replica o procedimento que já existia no frontend (gerar uma senha temporária segura, aplicá-la imediatamente à conta, e só então enviar o e-mail com essa senha) — mas de forma atômica, do lado do servidor, sem token nem link. `tipo` no corpo da requisição passou a ser **opcional**: quando omitido, tenta identificar o usuário como estudante, depois academia, depois admin, replicando a lógica de fallback que existia no `route.ts` do frontend (removida na tarefa de frontend correspondente).

O design HTML usado nesses dois e-mails é um porte fiel, para Go, do mesmo design system que já existia em `src/lib/email/email-service.ts` no frontend (cores da marca, logótipo, dark-mode, blocos de alerta) — não existia, até esta tarefa, uma versão completa desse design em Go.

**Decisão registada:** o botão "Ir para Alteração de Senha" do template original de recuperação de senha apontava para uma página que nunca existiu (`/recuperar-senha/:token`). Nesta tarefa, o botão equivalente ("Ir para o Login") foi redirecionado para a página de login, que é o destino real e correto agora que a senha já foi trocada no momento do envio do e-mail.

---

## 1. Novo arquivo — `internal/services/email_html_templates.go`

Arquivo inteiramente novo. Contém o design system de e-mail (cores, invólucro/shell, botão, alerta, pill) e os dois templates completos (Verificação de E-mail e Senha Resetada), em HTML + texto simples.

**Arquivo completo — `/home/claude/work/rastreio-backend/internal/services/email_html_templates.go`:**

```go
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

```

---

## 2. Novo arquivo — `internal/services/email_html_templates_test.go`

Testes de renderização pura (sem necessidade de banco de dados — rodam em qualquer ambiente, inclusive no do Codex). Cobrem: estrutura do HTML final, escaping contra XSS (nome de usuário e senha temporária), e uma regressão específica para `%` não escapado em `fmt.Sprintf` (risco identificado durante esta tarefa).

**Arquivo completo — `/home/claude/work/rastreio-backend/internal/services/email_html_templates_test.go`:**

```go
package services

import (
	"strings"
	"testing"
)

// Estes testes não precisam de banco de dados nem de rede — são funções
// puras de montagem de string. Podem (e devem) ser executados no ambiente
// do Codex normalmente, com `go test ./internal/services/...`.

func TestEmailLogoURL(t *testing.T) {
	cases := map[string]string{
		"https://app.spuri.co.ao":  "https://app.spuri.co.ao/images/email/spuri-logo-email.png",
		"https://app.spuri.co.ao/": "https://app.spuri.co.ao/images/email/spuri-logo-email.png",
		"http://localhost:3000":    "http://localhost:3000/images/email/spuri-logo-email.png",
	}
	for in, want := range cases {
		if got := emailLogoURL(in); got != want {
			t.Errorf("emailLogoURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderEmailShell_ProducesWellFormedDocument(t *testing.T) {
	html := renderEmailShell("preheader de teste", "Título de Teste", "<p>corpo</p>", "https://app.spuri.co.ao")

	mustContain(t, html, "<!DOCTYPE html>")
	mustContain(t, html, "<title>Título de Teste</title>")
	mustContain(t, html, "preheader de teste")
	mustContain(t, html, "<p>corpo</p>")
	mustContain(t, html, "https://app.spuri.co.ao/images/email/spuri-logo-email.png")
	mustContain(t, html, emailTplBrandBlue)
	mustContain(t, html, emailTplNavy)
	mustContain(t, html, "spuriartipan@gmail.com")

	// Nenhum placeholder do text/template deve escapar sem ser substituído —
	// isso indicaria um nome de campo errado em algum {{.Campo}}.
	if strings.Contains(html, "{{") || strings.Contains(html, "}}") {
		t.Errorf("HTML final contém chaves de template não resolvidas:\n%s", html)
	}

	// Contagem grosseira de balanceamento de tags-chave (detecta erro de
	// template mal fechado sem precisar de um parser HTML completo).
	assertBalanced(t, html, "<table", "</table>")
	assertBalanced(t, html, "<tr", "</tr>")
	assertBalanced(t, html, "<td", "</td>")
	assertBalanced(t, html, "<html", "</html>")
	assertBalanced(t, html, "<body", "</body>")
}

func TestRenderEmailShell_EscapesTitleAndPreheader(t *testing.T) {
	html := renderEmailShell(`<script>alert(1)</script>`, `"Título" & <perigoso>`, "<p>corpo</p>", "https://app.spuri.co.ao")

	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Errorf("preheader não escapado permitiria injeção de HTML/JS:\n%s", html)
	}
	if strings.Contains(html, `<perigoso>`) {
		t.Errorf("title não escapado permitiria injeção de HTML:\n%s", html)
	}
	mustContain(t, html, "&lt;script&gt;")
}

func TestRenderVerificationEmailHTML(t *testing.T) {
	html := renderVerificationEmailHTML("Ana Costa", "https://app.spuri.co.ao/verificar-email/abc123", "https://app.spuri.co.ao")

	mustContain(t, html, "<title>Verificação de E-mail - Spuri</title>")
	mustContain(t, html, "Ana Costa")
	mustContain(t, html, "Verificar E-mail")
	mustContain(t, html, "https://app.spuri.co.ao/verificar-email/abc123")
	mustContain(t, html, "24 horas")
	mustContain(t, html, "https://app.spuri.co.ao/images/email/spuri-logo-email.png")

	// A URL de verificação deve aparecer pelo menos duas vezes: uma no botão
	// (href) e outra no link de "copie e cole" (o mesmo padrão do frontend).
	count := strings.Count(html, "https://app.spuri.co.ao/verificar-email/abc123")
	if count < 2 {
		t.Errorf("esperava a URL de verificação repetida (botão + link de apoio), apareceu %d vez(es)", count)
	}
}

func TestRenderVerificationEmailHTML_EscapesUserName(t *testing.T) {
	html := renderVerificationEmailHTML(`João & Cia <script>`, "https://app.spuri.co.ao/verificar-email/x", "https://app.spuri.co.ao")
	if strings.Contains(html, "<script>") {
		t.Errorf("nome do usuário não escapado permitiria injeção de HTML:\n%s", html)
	}
	mustContain(t, html, "João &amp; Cia")
}

func TestRenderPasswordResetEmailHTML(t *testing.T) {
	html := renderPasswordResetEmailHTML("Ana Costa", "Tmp#4821xZ", "https://app.spuri.co.ao/login", "https://app.spuri.co.ao")

	mustContain(t, html, "<title>Senha Resetada - Spuri</title>")
	mustContain(t, html, "Ana Costa")
	mustContain(t, html, "Tmp#4821xZ")
	mustContain(t, html, "Ir para o Login")
	mustContain(t, html, "https://app.spuri.co.ao/login")
	mustContain(t, html, "Importante")
	mustContain(t, html, "Dicas de Segurança")
	mustContain(t, html, "https://app.spuri.co.ao/images/email/spuri-logo-email.png")
}

func TestRenderPasswordResetEmailHTML_EscapesTempPassword(t *testing.T) {
	// A senha gerada por GenerateSecurePassword() nunca contém estes
	// caracteres, mas o escaping é feito de forma defensiva mesmo assim.
	html := renderPasswordResetEmailHTML("Ana Costa", `<b>"forçada"</b>`, "https://app.spuri.co.ao/login", "https://app.spuri.co.ao")
	if strings.Contains(html, `<b>"forçada"</b>`) {
		t.Errorf("senha temporária não escapada permitiria injeção de HTML:\n%s", html)
	}
}

func TestRenderPasswordResetEmailText(t *testing.T) {
	text := renderPasswordResetEmailText("Ana Costa", "Tmp#4821xZ", "https://app.spuri.co.ao/login")
	mustContain(t, text, "Ana Costa")
	mustContain(t, text, "Tmp#4821xZ")
	mustContain(t, text, "https://app.spuri.co.ao/login")
	if strings.Contains(text, "<") {
		t.Errorf("versão em texto simples não deveria conter marcação HTML: %s", text)
	}
}

func TestRenderVerificationEmailText(t *testing.T) {
	text := renderVerificationEmailText("Ana Costa", "https://app.spuri.co.ao/verificar-email/abc123")
	mustContain(t, text, "Ana Costa")
	mustContain(t, text, "https://app.spuri.co.ao/verificar-email/abc123")
	mustContain(t, text, "24 horas")
}

func TestRenderEmailButtonAndPillAndAlert_NoUnescapedPercent(t *testing.T) {
	// Regressão específica para o risco identificado nesta tarefa: qualquer
	// "%" literal não duplicado num template usado com fmt.Sprintf quebra a
	// contagem de verbos e falha em tempo de execução (ou, pior, imprime
	// "%!s(MISSING)"). Se algum destes helpers regredir para conter um "%"
	// não escapado, o teste de smoke abaixo (que já invoca todos eles) iria
	// produzir esse texto no HTML final.
	html := renderEmailAlert("warning", "Título", "corpo")
	if strings.Contains(html, "%!") {
		t.Errorf("renderEmailAlert produziu saída de Sprintf malformada: %s", html)
	}
	button := renderEmailButton("https://x", "Rótulo")
	if strings.Contains(button, "%!") {
		t.Errorf("renderEmailButton produziu saída de Sprintf malformada: %s", button)
	}
	pill := renderEmailPill("Rótulo")
	if strings.Contains(pill, "%!") {
		t.Errorf("renderEmailPill produziu saída de Sprintf malformada: %s", pill)
	}
	full := renderPasswordResetEmailHTML("Ana", "Senha1234!", "https://x/login", "https://x")
	if strings.Contains(full, "%!") {
		t.Errorf("renderPasswordResetEmailHTML produziu saída de Sprintf malformada (verificar '%%' não duplicado em algum template): %s", full)
	}
}

// ---- helpers de teste ----

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("esperava encontrar %q na saída, mas não encontrei.\n--- saída completa ---\n%s", needle, haystack)
	}
}

func assertBalanced(t *testing.T, html, openTag, closeTag string) {
	t.Helper()
	open := strings.Count(html, openTag)
	close := strings.Count(html, closeTag)
	if open != close {
		t.Errorf("tags desbalanceadas: %d x %q vs %d x %q", open, openTag, close, closeTag)
	}
}

```

---

## 3. Novo arquivo — `internal/handlers/auth_email_handlers_integration_test.go`

Teste de integração real (exige PostgreSQL — já rodado com sucesso pelo orquestrador, ver seção de Validação). Prova, de ponta a ponta: que `SolicitarRecuperacaoSenha` realmente troca a senha (a antiga deixa de validar), que o gate de e-mail não verificado bloqueia sem alterar nada, que um identificador inexistente devolve 404, que a auto-detecção de tipo funciona quando `tipo` é omitido, e que `SolicitarVerificacaoEmail` continua gerando corretamente o token persistido após a troca de EmailJS para Brevo.

**Arquivo completo — `/home/claude/work/rastreio-backend/internal/handlers/auth_email_handlers_integration_test.go`:**

```go
package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/projections"
)

// applyAdminEvents aplica, de forma incremental, TODOS os eventos já
// persistidos no ledger para um agregado Admin específico à projeção
// (projection_admins) — usando o MESMO dispatcher (AdminProjection.Handle)
// que projections.NewAdminProjection(client).Rebuild() usa internamente.
//
// Deliberadamente NÃO se usa Rebuild() neste arquivo: Rebuild() começa com
// `TRUNCATE TABLE projection_admins CASCADE` (ver admin_projection.go) e
// depois repõe TODOS os admins a partir do zero, incluindo os de outros
// testes deste MESMO pacote que não passam pelo event store (ex.:
// financeiro_handlers_integration_test.go insere admins diretamente via SQL
// para simular papéis — um TRUNCATE apagaria essas linhas criadas antes ou
// depois deste teste, quebrando testes irmãos sem qualquer relação com
// e-mail/recuperação de senha). Aplicar só os eventos do agregado em
// questão evita esse efeito colateral.
func applyAdminEvents(t *testing.T, client *db.Client, aggregateID uuid.UUID) {
	t.Helper()
	projection := projections.NewAdminProjection(client)
	rows, err := client.DB().Query(`
		SELECT id, event_id, aggregate_id, aggregate_type, event_type,
			event_version, payload, metadata, occurred_at, recorded_at,
			ledger_hash, previous_hash
		FROM spuri_ledger
		WHERE aggregate_id = $1
		ORDER BY id ASC
	`, aggregateID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var event db.Event
		var prevHash sql.NullString
		if err := rows.Scan(
			&event.ID, &event.EventID, &event.AggregateID, &event.AggregateType,
			&event.EventType, &event.EventVersion, &event.Payload, &event.Metadata,
			&event.OccurredAt, &event.RecordedAt, &event.LedgerHash, &prevHash,
		); err != nil {
			t.Fatal(err)
		}
		if prevHash.Valid {
			event.PreviousHash = &prevHash.String
		}
		if err := projection.Handle(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

var (
	testCreatorAdminOnce sync.Once
	testCreatorAdminID   uuid.UUID
)

// getOrCreateTestCreatorAdmin devolve o ID de um admin real já persistido em
// projection_admins, para ser usado como created_by (foreign key) dos
// admins seedados nos testes deste arquivo.
//
// A coluna role tem check constraint no banco (fpp/adm/gerente apenas —
// migrations/001_complete_schema.sql), e a constraint de bootstrap
// (idx_bootstrap_fpp_unique, migrations/025_admin_email_unique_index.sql)
// permite no máximo um admin com created_by NULO por role — vaga também
// disputada por testes já existentes neste mesmo pacote (ex.:
// financeiro_handlers_integration_test.go cria admins nas 3 roles com
// created_by implicitamente nulo). Por isso este criador ocupa a vaga
// (created_by nulo) só o tempo mínimo necessário: assim que persistido via
// event sourcing (sobrevive a qualquer Handle() futuro, ao contrário de um
// INSERT solto), a própria linha é atualizada para se autorreferenciar em
// created_by — o que satisfaz a foreign key de quem a referenciar depois E
// tira a linha do escopo do índice parcial (que só vale quando created_by É
// nulo), libertando a vaga para os outros testes deste pacote.
func getOrCreateTestCreatorAdmin(t *testing.T, client *db.Client) uuid.UUID {
	t.Helper()
	testCreatorAdminOnce.Do(func() {
		hash, err := bcrypt.GenerateFromPassword([]byte("CriadorDeTeste#123"), bcrypt.DefaultCost)
		if err != nil {
			t.Fatal(err)
		}
		telefone := "923000111"
		creator := aggregates.NewAdmin()
		if err := creator.Criar("Criador de Teste", "criador-"+uuid.NewString()+"@example.test", &telefone, string(hash), "gerente", nil); err != nil {
			t.Fatal(err)
		}
		repository := db.NewAggregateRepository(client)
		if err := repository.SaveWithAudit(creator, db.AuditContext{UserID: "integration-test", UserType: "sistema", IP: "127.0.0.1"}); err != nil {
			t.Fatal(err)
		}
		applyAdminEvents(t, client, creator.ID)

		if _, err := client.DB().Exec(`UPDATE projection_admins SET created_by = $1 WHERE id = $1`, creator.ID); err != nil {
			t.Fatal(err)
		}
		testCreatorAdminID = creator.ID
	})
	return testCreatorAdminID
}

// seedAdminParaRecuperacao cria um admin real (agregado -> event store ->
// projeção), com senha e email_verificado controlados pelo teste. Usa o
// mesmo caminho de event sourcing de que o handler em teste depende
// (repository.Load(userID, "Admin")), ao contrário de um INSERT direto na
// projeção, que deixaria o event store vazio e faria SolicitarRecuperacaoSenha
// falhar com "administrador não encontrado".
func seedAdminParaRecuperacao(t *testing.T, client *db.Client, senha string, emailVerificado bool) (userID uuid.UUID, email string) {
	t.Helper()
	repository := db.NewAggregateRepository(client)
	audit := db.AuditContext{UserID: "integration-test", UserType: "sistema", IP: "127.0.0.1"}

	hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}

	criadoPor := getOrCreateTestCreatorAdmin(t, client)
	email = "admin-" + uuid.NewString() + "@example.test"
	telefone := "923456789"
	admin := aggregates.NewAdmin()
	if err := admin.Criar("Admin de Teste", email, &telefone, string(hash), "adm", &criadoPor); err != nil {
		t.Fatal(err)
	}
	if emailVerificado {
		if err := admin.VerificarEmail(); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.SaveWithAudit(admin, audit); err != nil {
		t.Fatal(err)
	}
	applyAdminEvents(t, client, admin.ID)
	return admin.ID, email
}

func postJSON(router *gin.Engine, method, path string, body map[string]any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewBuffer(raw))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)
	return recorder
}

// routerParaAuthEmail monta um router mínimo só com a rota pública testada
// aqui, injetando dbClient e repository exatamente como setupRouter faz em
// cmd/server/main.go (mesmas chaves de contexto usadas por getDbClient e
// getRepository em helpers.go).
func routerParaAuthEmail(client *db.Client) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	repository := db.NewAggregateRepository(client)
	router.Use(func(c *gin.Context) {
		c.Set("dbClient", client)
		c.Set("repository", repository)
		c.Next()
	})
	router.POST("/email/recuperar-senha/solicitar", SolicitarRecuperacaoSenha)
	return router
}

func TestIntegrationSolicitarRecuperacaoSenhaAplicanovaSenhaImediatamente(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)

	userID, email := seedAdminParaRecuperacao(t, client, "SenhaAntiga#123", true)

	recorder := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{
		"identificador": email,
		"tipo":          "admin",
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("esperava 200, recebi %d: %s", recorder.Code, recorder.Body.String())
	}

	// O handler gravou um novo evento (AdminSenhaAlterada) no ledger — aplica-o
	// à projeção antes de consultar projection_admins (ver applyAdminEvents).
	applyAdminEvents(t, client, userID)

	// Prova empírica de que a senha REALMENTE mudou: a senha antiga não
	// deve mais bater com o hash persistido. Sem BREVO_API_KEY configurada
	// no ambiente de teste, SendPasswordResetEmail cai no modo degradado
	// (loga a senha temporária e devolve sucesso) — por isso não temos a
	// senha nova aqui para testar positivamente, mas a prova negativa (a
	// senha antiga deixou de funcionar) já demonstra que a troca ocorreu.
	var novoHash string
	if err := client.DB().QueryRow(`SELECT senha_hash FROM projection_admins WHERE id = $1`, userID).Scan(&novoHash); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(novoHash), []byte("SenhaAntiga#123")) == nil {
		t.Fatal("a senha antiga ainda é válida após SolicitarRecuperacaoSenha — a senha não foi trocada")
	}
}

func TestIntegrationSolicitarRecuperacaoSenhaExigeEmailVerificado(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)

	userID, email := seedAdminParaRecuperacao(t, client, "SenhaAntiga#123", false)

	recorder := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{
		"identificador": email,
		"tipo":          "admin",
	})

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("esperava 403 (email não verificado), recebi %d: %s", recorder.Code, recorder.Body.String())
	}

	var hashAtual string
	if err := client.DB().QueryRow(`SELECT senha_hash FROM projection_admins WHERE id = $1`, userID).Scan(&hashAtual); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hashAtual), []byte("SenhaAntiga#123")) != nil {
		t.Fatal("a senha foi alterada mesmo com email não verificado — não deveria")
	}
}

func TestIntegrationSolicitarRecuperacaoSenhaAutoDetectaTipoQuandoOmitido(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)

	userID, email := seedAdminParaRecuperacao(t, client, "SenhaAntiga#123", true)

	// Nenhum "tipo" no corpo — deve auto-detectar como admin (estudante e
	// academia não têm esse email, então a busca cai no admin).
	recorder := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{
		"identificador": email,
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("esperava 200, recebi %d: %s", recorder.Code, recorder.Body.String())
	}

	applyAdminEvents(t, client, userID)

	var novoHash string
	if err := client.DB().QueryRow(`SELECT senha_hash FROM projection_admins WHERE id = $1`, userID).Scan(&novoHash); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(novoHash), []byte("SenhaAntiga#123")) == nil {
		t.Fatal("a senha antiga ainda é válida — auto-detecção de tipo não funcionou")
	}
}

func TestIntegrationSolicitarRecuperacaoSenhaIdentificadorInexistente(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)

	recorder := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{
		"identificador": "ninguem-" + uuid.NewString() + "@example.test",
		"tipo":          "admin",
	})

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("esperava 404, recebi %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestIntegrationSolicitarVerificacaoEmailGeraTokenPersistido(t *testing.T) {
	client := integrationFinanceClient(t)
	repository := db.NewAggregateRepository(client)

	userID, email := seedAdminParaRecuperacao(t, client, "SenhaQualquer#123", false)

	// SolicitarVerificacaoEmail exige AuthMiddleware (lê user_id/user_type
	// do contexto, não do corpo da requisição) — reproduzido aqui como em
	// TestIntegrationFinanceRejectsNonFPPAdmins, via gin.CreateTestContext
	// direto, sem passar pelo router com middleware de auth real.
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/email/verificar-email/solicitar", nil)
	ctx.Set("dbClient", client)
	ctx.Set("repository", repository)
	ctx.Set("user_id", userID)
	ctx.Set("user_type", "admin")

	SolicitarVerificacaoEmail(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("esperava 200, recebi %d: %s", recorder.Code, recorder.Body.String())
	}

	var count int
	if err := client.DB().QueryRow(
		`SELECT COUNT(*) FROM auth_tokens WHERE email = $1 AND tipo = 'verificacao_email' AND usado = FALSE`,
		email,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("esperava exatamente 1 token de verificação não usado para %s, encontrei %d", email, count)
	}
}

```

---

## 4. `internal/services/email_service.go` — trocar EmailJS por Brevo nas duas funções

**Localizar** (bloco contíguo — as duas funções `SendVerificationEmail` e `SendPasswordResetEmail`, uma logo depois da outra, exatamente como estão hoje no arquivo):

```go
// SendVerificationEmail envia email de verificação de endereço.
func (s *EmailService) SendVerificationEmail(userID uuid.UUID, userType, email, nome string) error {
	if !s.enabled {
		return fmt.Errorf("serviço de email desabilitado")
	}
	if email == "" {
		return fmt.Errorf("email vazio")
	}

	token, err := s.SaveToken(userID, userType, "verificacao_email", email, 24*time.Hour)
	if err != nil {
		return fmt.Errorf("erro ao gerar token: %w", err)
	}

	verifyURL := fmt.Sprintf("%s/verificar-email/%s", s.frontendURL, token)

	params := map[string]string{
		"user_name":  nome,
		"verify_url": verifyURL,
		"expiry":     "24 horas",
	}

	return s.sendEmailViaEmailJS(email, nome, s.templateVerify, params)
}

// SendPasswordResetEmail envia link de recuperação de senha.
func (s *EmailService) SendPasswordResetEmail(userID uuid.UUID, userType, email, nome string) error {
	if !s.enabled {
		return fmt.Errorf("serviço de email desabilitado")
	}
	if email == "" {
		return fmt.Errorf("email vazio")
	}

	token, err := s.SaveToken(userID, userType, "recuperacao_senha", email, 1*time.Hour)
	if err != nil {
		return fmt.Errorf("erro ao gerar token: %w", err)
	}

	resetURL := fmt.Sprintf("%s/recuperar-senha/%s", s.frontendURL, token)

	params := map[string]string{
		"user_name": nome,
		"reset_url": resetURL,
		"expiry":    "1 hora",
	}

	return s.sendEmailViaEmailJS(email, nome, s.templateReset, params)
}
```

**Substituir por:**

```go
// SendVerificationEmail envia o email de verificação de endereço.
//
// Historicamente este método enviava via EmailJS (sendEmailViaEmailJS,
// controlado por s.enabled/s.templateVerify — configuração de um template
// externo, editado fora deste repositório). Nesta tarefa passou a enviar
// via Brevo, com o HTML de marca portado do frontend (ver
// email_html_templates.go), para que o design do email deixe de depender
// de um template externo e passe a ser o mesmo já usado/conhecido da
// plataforma. A assinatura do método não mudou, então nenhum dos dois
// chamadores existentes (SolicitarVerificacaoEmail e o helper interno não
// utilizado gerarEEnviarTokenVerificacao) precisou de alteração.
func (s *EmailService) SendVerificationEmail(userID uuid.UUID, userType, email, nome string) error {
	if email == "" {
		return fmt.Errorf("email vazio")
	}

	token, err := s.SaveToken(userID, userType, "verificacao_email", email, 24*time.Hour)
	if err != nil {
		return fmt.Errorf("erro ao gerar token: %w", err)
	}

	verifyURL := fmt.Sprintf("%s/verificar-email/%s", s.frontendURL, token)

	cfg, ok := loadBrevoConfig()
	if !ok {
		log.Printf("[EMAIL-BREVO] ⚠️  BREVO_API_KEY/EMAIL_FROM/EMAIL_USER não configurados — link de verificação de %s registado apenas no log do servidor.", email)
		log.Printf("[EMAIL-BREVO] 🔗 Link de verificação para %s: %s", email, verifyURL)
		return nil
	}

	subject := "Verificação de E-mail - Spuri"
	htmlBody := renderVerificationEmailHTML(nome, verifyURL, s.frontendURL)
	textBody := renderVerificationEmailText(nome, verifyURL)

	if err := sendBrevoEmail(cfg, email, nome, subject, textBody, htmlBody); err != nil {
		return fmt.Errorf("brevo: %w", err)
	}
	return nil
}

// SendPasswordResetEmail envia ao usuário a senha temporária já aplicada à
// sua conta.
//
// ATENÇÃO — mudança de contrato nesta tarefa: esta função deixou de gerar
// um token e enviar apenas um link de recuperação (o design antigo, via
// EmailJS, apontava para "{FRONTEND_URL}/recuperar-senha/{token}", uma
// página que nunca existiu no frontend). Agora ela só ENVIA o email —
// quem chama (SolicitarRecuperacaoSenha, em auth_email_handlers.go) já
// gerou a senha temporária e já a aplicou à conta antes de chamar esta
// função, replicando no backend o procedimento que já era usado no
// frontend (gerarTokenRecuperacao + resetarSenha + envio do email, os três
// passos agora atômicos e do lado do servidor). É por isso que a
// assinatura mudou de (userID, userType, email, nome) para
// (email, nome, senhaTemporaria) — não há mais token nem link envolvidos
// neste fluxo.
//
// O único chamador real desta função (SolicitarRecuperacaoSenha) foi
// atualizado nesta mesma tarefa. O helper interno não utilizado
// gerarEEnviarTokenRecuperacao (final deste arquivo) referencia a
// assinatura antiga através de uma interface anônima própria — como nunca
// é chamado em lugar nenhum, continua compilando sem qualquer alteração.
func (s *EmailService) SendPasswordResetEmail(email, nome, senhaTemporaria string) error {
	if email == "" {
		return fmt.Errorf("email vazio")
	}

	loginURL := fmt.Sprintf("%s/login", s.frontendURL)

	cfg, ok := loadBrevoConfig()
	if !ok {
		log.Printf("[EMAIL-BREVO] ⚠️  BREVO_API_KEY/EMAIL_FROM/EMAIL_USER não configurados — senha temporária de %s registada apenas no log do servidor.", email)
		log.Printf("[EMAIL-BREVO] 🔑 Senha temporária para %s: %s", email, senhaTemporaria)
		return nil
	}

	subject := "Senha Resetada - Spuri"
	htmlBody := renderPasswordResetEmailHTML(nome, senhaTemporaria, loginURL, s.frontendURL)
	textBody := renderPasswordResetEmailText(nome, senhaTemporaria, loginURL)

	if err := sendBrevoEmail(cfg, email, nome, subject, textBody, htmlBody); err != nil {
		log.Printf("[EMAIL-BREVO] ⚠️  Falha ao enviar email de recuperação para %s — senha temporária registada no log do servidor como salvaguarda.", email)
		log.Printf("[EMAIL-BREVO] 🔑 Senha temporária para %s: %s", email, senhaTemporaria)
		return fmt.Errorf("brevo: %w", err)
	}
	return nil
}
```

⚠️ **Atenção ao aplicar:** a assinatura de `SendPasswordResetEmail` mudou de `(userID uuid.UUID, userType, email, nome string)` para `(email, nome, senhaTemporaria string)`. O único lugar do repositório que chama este método (`SolicitarRecuperacaoSenha`, em `auth_email_handlers.go`) é atualizado na seção 5 deste mesmo documento — aplique as duas seções juntas, não uma sem a outra, ou o projeto não compila.

Nada mais muda neste arquivo — o resto de `email_service.go` (constantes, `SaveToken`, `VerifyToken`, `SendAdminWelcomeEmail`, `SendAcademiaCadastradaEmail*`, `GenerateSecurePassword`, etc.) fica exatamente como está.

---

## 5. `internal/handlers/auth_email_handlers.go` — import novo + reescrever `SolicitarRecuperacaoSenha`

### 5.1 — Import

**Localizar:**
```go
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/middleware"
	"spuri/internal/utils"
```

**Substituir por:**
```go
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/middleware"
	"spuri/internal/services"
	"spuri/internal/utils"
```

### 5.2 — Função `SolicitarRecuperacaoSenha`

**Localizar** (função completa, exatamente como está hoje no arquivo):

```go
// SolicitarRecuperacaoSenha gera o token e envia o email de recuperação diretamente.
// Usado por fluxos onde o backend controla o envio completo.
// Rota: POST /email/recuperar-senha/solicitar  (pública — usa identificador no body)
func SolicitarRecuperacaoSenha(c *gin.Context) {
	var req struct {
		Identificador string `json:"identificador" binding:"required"`
		Tipo          string `json:"tipo" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("identificador e tipo são obrigatórios"))
		return
	}

	client := getDbClient(c)

	var userID uuid.UUID
	var email, nome string
	var emailVerificado bool
	var idStr string
	var err error

	switch req.Tipo {
	case "estudante":
		err = client.DB().QueryRow(
			`SELECT id, COALESCE(email,''), nome, COALESCE(email_verificado, FALSE)
			 FROM projection_estudantes
			 WHERE codigo_estudante = $1 OR email = $1`,
			req.Identificador,
		).Scan(&idStr, &email, &nome, &emailVerificado)
	case "academia":
		err = client.DB().QueryRow(
			`SELECT id, COALESCE(email,''), nome, COALESCE(email_verificado, FALSE)
			 FROM projection_academias
			 WHERE codigo_academia = $1 OR email = $1`,
			req.Identificador,
		).Scan(&idStr, &email, &nome, &emailVerificado)
	case "admin":
		err = client.DB().QueryRow(
			`SELECT id, email, nome, COALESCE(email_verificado, FALSE)
			 FROM projection_admins WHERE email = $1`,
			req.Identificador,
		).Scan(&idStr, &email, &nome, &emailVerificado)
	default:
		utils.RespondWithValidationError(c, fmt.Errorf("tipo deve ser 'estudante', 'academia' ou 'admin'"))
		return
	}

	if err != nil {
		utils.RespondWithNotFoundError(c, "usuário")
		return
	}

	userID, _ = uuid.Parse(idStr)

	if email == "" {
		utils.RespondWithValidationError(c, fmt.Errorf("usuário não possui email cadastrado"))
		return
	}

	if !emailVerificado {
		utils.RespondWithForbiddenError(c, "Por favor, verifique seu email antes de solicitar recuperação de senha")
		return
	}

	emailSvc := getEmailService(c)

	// Gera token e envia o email diretamente — token NÃO retornado ao frontend
	if err := emailSvc.SendPasswordResetEmail(userID, req.Tipo, email, nome); err != nil {
		log.Printf("Erro ao enviar email de recuperação: %v", err)
		utils.RespondWithInternalError(c, err)
		return
	}

	log.Printf("Email de recuperação enviado para: %s", email)

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"message":   "Email de recuperação enviado com sucesso. Verifique sua caixa de entrada.",
		"expira_em": "1 hora",
	})
}
```

**Substituir por:**

```go
// SolicitarRecuperacaoSenha gera o token e envia o email de recuperação diretamente.
// Usado por fluxos onde o backend controla o envio completo.
// Rota: POST /email/recuperar-senha/solicitar  (pública — usa identificador no body)
//
// 'tipo' é OPCIONAL: quando omitido, tenta identificar o usuário como
// estudante, depois academia, depois admin, nessa ordem, parando no
// primeiro em que o identificador exista — replicando aqui a lógica que
// antes só existia no frontend (src/app/api/recuperar-senha/route.ts,
// removido nesta tarefa), usada pela página pública "/esqueci-senha" que
// nunca pede ao usuário para escolher o tipo de conta. Se o identificador
// existir num tipo mas o email não estiver verificado, a busca PARA ali
// (não continua tentando os outros tipos) — mesmo comportamento de antes.
func SolicitarRecuperacaoSenha(c *gin.Context) {
	var req struct {
		Identificador string `json:"identificador" binding:"required"`
		Tipo          string `json:"tipo"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("identificador é obrigatório"))
		return
	}

	tiposParaTentar := []string{"estudante", "academia", "admin"}
	if req.Tipo != "" {
		if req.Tipo != "estudante" && req.Tipo != "academia" && req.Tipo != "admin" {
			utils.RespondWithValidationError(c, fmt.Errorf("tipo deve ser 'estudante', 'academia' ou 'admin'"))
			return
		}
		tiposParaTentar = []string{req.Tipo}
	}

	client := getDbClient(c)

	var userID uuid.UUID
	var email, nome, tipoEncontrado string
	var emailVerificado bool
	encontrado := false

	for _, tipo := range tiposParaTentar {
		var idStr, e, n string
		var verificado bool
		var errBusca error
		switch tipo {
		case "estudante":
			errBusca = client.DB().QueryRow(
				`SELECT id, COALESCE(email,''), nome, COALESCE(email_verificado, FALSE)
				 FROM projection_estudantes
				 WHERE codigo_estudante = $1 OR email = $1`,
				req.Identificador,
			).Scan(&idStr, &e, &n, &verificado)
		case "academia":
			errBusca = client.DB().QueryRow(
				`SELECT id, COALESCE(email,''), nome, COALESCE(email_verificado, FALSE)
				 FROM projection_academias
				 WHERE codigo_academia = $1 OR email = $1`,
				req.Identificador,
			).Scan(&idStr, &e, &n, &verificado)
		case "admin":
			errBusca = client.DB().QueryRow(
				`SELECT id, email, nome, COALESCE(email_verificado, FALSE)
				 FROM projection_admins WHERE email = $1`,
				req.Identificador,
			).Scan(&idStr, &e, &n, &verificado)
		}
		if errBusca != nil {
			continue // não encontrado neste tipo — tenta o próximo
		}
		userID, _ = uuid.Parse(idStr)
		email, nome, emailVerificado, tipoEncontrado = e, n, verificado, tipo
		encontrado = true
		break
	}

	if !encontrado {
		utils.RespondWithNotFoundError(c, "usuário")
		return
	}

	if email == "" {
		utils.RespondWithValidationError(c, fmt.Errorf("usuário não possui email cadastrado"))
		return
	}

	if !emailVerificado {
		utils.RespondWithForbiddenError(c, "Por favor, verifique seu email antes de solicitar recuperação de senha")
		return
	}

	// A partir daqui o fluxo passa a ser inteiramente controlado pelo
	// backend: gera-se uma senha temporária segura, aplica-se-a já à conta
	// (mesmo mecanismo de event sourcing usado em ResetarSenha — carregado
	// diretamente aqui, e não através de ResetarSenha, para não alterar o
	// comportamento já existente e testado desse outro endpoint), e só
	// então o email é enviado. Não existe mais token: a recuperação
	// acontece de forma síncrona nesta única chamada, tal como já
	// acontecia no frontend antes desta tarefa (gerarTokenRecuperacao +
	// resetarSenha + envio do email, agora atômico e do lado do servidor).
	senhaTemporaria, err := services.GenerateSecurePassword()
	if err != nil {
		utils.RespondWithInternalError(c, fmt.Errorf("erro ao gerar senha temporária: %w", err))
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(senhaTemporaria), bcrypt.DefaultCost)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	repository := getRepository(c)
	audit := db.AuditContext{UserID: "sistema", UserType: "sistema", IP: c.ClientIP()}

	switch tipoEncontrado {
	case "admin":
		adminAgg, loadErr := repository.Load(userID, "Admin")
		if loadErr != nil {
			utils.RespondWithNotFoundError(c, "administrador")
			return
		}
		admin, ok := adminAgg.(*aggregates.Admin)
		if !ok {
			utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado para admin"))
			return
		}
		if err := admin.AlterarSenha(string(hashedPassword), uuid.Nil, "reset_senha"); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if err := repository.SaveWithAudit(admin, audit); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
	case "academia":
		academiaAgg, loadErr := repository.Load(userID, "Academia")
		if loadErr != nil {
			utils.RespondWithNotFoundError(c, "academia")
			return
		}
		academia, ok := academiaAgg.(*aggregates.Academia)
		if !ok {
			utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado para academia"))
			return
		}
		if err := academia.AlterarSenha(string(hashedPassword), uuid.Nil, "reset_senha"); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if err := repository.SaveWithAudit(academia, audit); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
	case "estudante":
		estudanteAgg, loadErr := repository.Load(userID, "Estudante")
		if loadErr != nil {
			utils.RespondWithNotFoundError(c, "estudante")
			return
		}
		estudante, ok := estudanteAgg.(*aggregates.Estudante)
		if !ok {
			utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado para estudante"))
			return
		}
		if err := estudante.AlterarSenha(string(hashedPassword)); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if err := repository.SaveWithAudit(estudante, audit); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
	}

	log.Printf("Senha resetada (recuperação, event sourcing) para %s: %s", tipoEncontrado, email)

	emailSvc := getEmailService(c)
	if err := emailSvc.SendPasswordResetEmail(email, nome, senhaTemporaria); err != nil {
		// A senha JÁ foi trocada neste ponto. Falhar em silêncio devolvendo
		// sucesso seria pior (o usuário nunca saberia a senha nova); por
		// isso o erro é reportado ao chamador. SendPasswordResetEmail já
		// regista a senha temporária no log do servidor como salvaguarda
		// antes de devolver este erro.
		log.Printf("Erro ao enviar email de recuperação para %s: %v", email, err)
		utils.RespondWithInternalError(c, fmt.Errorf("senha redefinida, mas houve falha ao enviar o email com a nova senha: %w", err))
		return
	}

	log.Printf("Email de recuperação enviado para: %s", email)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Sua senha foi redefinida com sucesso. Enviamos a nova senha temporária para o seu email.",
	})
}
```

Nada mais muda neste arquivo — `GerarTokenVerificacao`, `SolicitarVerificacaoEmail`, `GerarTokenRecuperacao`, `ResetarSenha` e os dois helpers internos não utilizados (`gerarEEnviarTokenVerificacao`, `gerarEEnviarTokenRecuperacao`) ficam exatamente como estão. Em particular, **não tocar em `ResetarSenha`** — ele continua a funcionar exatamente como antes, para quem quer que ainda o use; o novo fluxo de `SolicitarRecuperacaoSenha` foi escrito para não depender dele, de propósito, para não arriscar alterar um comportamento já testado.

---

## Arquivos a remover

Nenhum. Esta tarefa só cria 3 arquivos novos e edita 2 já existentes.

## Validação (já executada pelo orquestrador — Codex só precisa confirmar)

```bash
gofmt -l internal/services/email_service.go internal/services/email_html_templates.go internal/services/email_html_templates_test.go internal/handlers/auth_email_handlers.go internal/handlers/auth_email_handlers_integration_test.go
# esperado: nenhuma saída

go build ./...
# esperado: nenhuma saída (sucesso)

go vet ./...
# esperado: nenhuma saída
```

Testes que **não** exigem PostgreSQL (rodam em qualquer ambiente, inclusive no do Codex):

```bash
go test ./internal/services/... -run TestRender -v
go test ./internal/services/... -run TestEmailLogoURL -v
# esperado: todos PASS (11 testes de renderização do HTML/texto dos e-mails)
```

Testes que **exigem PostgreSQL real** (ambiente do Codex não tem — já executados com sucesso pelo orquestrador, banco novo, isolado):

```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/<banco_de_teste>?sslmode=disable"
export RUN_POSTGRES_INTEGRATION=1
export JWT_SECRET="<qualquer_valor_para_teste>"

go test ./internal/handlers/... -run TestIntegrationSolicitar -v
# esperado: todos PASS, incluindo a prova de que a senha antiga deixa de
# validar depois de SolicitarRecuperacaoSenha, o gate de email não
# verificado, o 404 para identificador inexistente, a auto-detecção de
# tipo, e a persistência do token de verificação
```

## Conclusão

Depois de aplicar as 5 seções acima e confirmar `gofmt`/`go build`/`go vet` limpos e os testes de renderização (`TestRender*`, `TestEmailLogoURL`) passando, marcar esta tarefa como concluída. Os testes com `RUN_POSTGRES_INTEGRATION=1` podem ser pulados se o ambiente não tiver Postgres — já foram validados previamente pelo orquestrador.

**Não aplicar a tarefa de frontend `Tarefa - Remover Envio de E-mails pelo Frontend e Notificacao Duplicada.md` antes desta aqui estar concluída** — o frontend passa a depender destes dois endpoints (`/email/verificar-email/solicitar` e `/email/recuperar-senha/solicitar`) com o comportamento novo descrito aqui.
