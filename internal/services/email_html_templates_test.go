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
