package administradores

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"spuri/internal/handlers/testsupport"
)

// Tarefa 123 — guardas estáticas do código (SEC-04, SEC-05, SEC-07, D7).
// Reprovam se alguém reintroduzir senha padrão, canais de e-mail fora do Brevo
// ou segredos em logs.

func arquivosGoDeProducao(t *testing.T) map[string]string {
	t.Helper()
	raiz := testsupport.RaizDoRepositorio(t)
	out := map[string]string{}
	for _, pasta := range []string{"internal", "cmd"} {
		_ = filepath.Walk(filepath.Join(raiz, pasta), func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				t.Fatal(rerr)
			}
			out[p] = string(b)
			return nil
		})
	}
	if len(out) < 50 {
		t.Fatalf("varredura incompleta: só %d arquivos", len(out))
	}
	return out
}

// semComentarios descarta as linhas de comentário: a documentação pode (e deve)
// explicar o que foi eliminado; o que não pode é o código voltar a usar.
func semComentarios(src string) string {
	var sb strings.Builder
	for _, l := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "//") {
			continue
		}
		sb.WriteString(l)
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestSegurancaNaoExisteSenhaPadraoNemCanalDeEmailForaDoBrevo(t *testing.T) {
	proibidos := []string{
		"GetDefaultPassword", "GenerateSecurePassword", "SendPasswordResetEmail(",
		"api.emailjs.com", "EMAILJS_", "EMAIL_USER", "SMTP_HOST", "sendSMTPEmail", "loadSMTPConfig",
		"GerarTokenVerificacao", "GerarTokenRecuperacao", "gerar-token",
		"LoginRateLimit", "EmailRateLimit", "GlobalRateLimit",
	}
	for p, src := range arquivosGoDeProducao(t) {
		codigo := semComentarios(src)
		for _, termo := range proibidos {
			if strings.Contains(codigo, termo) {
				t.Errorf("%s contém %q, que foi eliminado na Tarefa 123", p, termo)
			}
		}
	}
}

// SEC-07: nenhuma linha de log escreve um segredo (link com token, senha).
func TestSegurancaLogsNaoEscrevemSegredos(t *testing.T) {
	linhaDeLog := regexp.MustCompile(`log\.(Printf|Println|Print)\(`)
	segredos := regexp.MustCompile(`(?i)(verifyURL|resetURL|plainPassword|senhaTemporaria|tempPassword|defaultPassword|novaSenha|req\.Senha|req\.NovaSenha|senha_hash|SenhaHash\b)`)
	// Exceção única e declarada: LogCredenciaisDev (CREDENCIAIS_LOG_DEV, nunca em produção).
	excecoes := map[string]bool{filepath.Join("..", "..", "internal", "services", "senha_temporaria.go"): true}
	for p, src := range arquivosGoDeProducao(t) {
		if excecoes[p] {
			continue
		}
		for n, linha := range strings.Split(src, "\n") {
			if linhaDeLog.MatchString(linha) && segredos.MatchString(linha) {
				t.Errorf("%s:%d escreve possível segredo no log: %s", p, n+1, strings.TrimSpace(linha))
			}
		}
	}
}

func fonte(t *testing.T, arquivos map[string]string, sufixo string) string {
	t.Helper()
	for p, src := range arquivos {
		if strings.HasSuffix(filepath.ToSlash(p), sufixo) {
			return src
		}
	}
	t.Fatalf("arquivo %s não encontrado na varredura", sufixo)
	return ""
}

// SEC-04: todos os pontos de criação de conta por terceiros usam a senha
// temporária e registam o evento que liga a troca obrigatória.
func TestSegurancaPontosDeCriacaoUsamSenhaTemporaria(t *testing.T) {
	arquivos := arquivosGoDeProducao(t)
	exigem := map[string][]string{
		// Parte 5: o destino das credenciais do estudante vem da regra única de contato.
		"internal/handlers/estudantes/estudante_handlers.go": {"GenerateTempPassword()", "DefinirSenhaTemporaria()", "ResolverContato("},
		// Parte 4: secretário, professor e encarregado também nascem com senha temporária.
		"internal/handlers/apoio/conta_pessoa_plano.go":       {"GenerateTempPassword()", "DefinirSenhaTemporaria()"},
		"internal/handlers/contas/contas_handlers.go":         {"DefinirSenhaTemporaria()"},
		"internal/handlers/academias/academia_handlers.go":    {"GenerateTempPassword()", "DefinirSenhaTemporaria()", "TelefoneDestinoCredenciaisAcademia("},
		"internal/handlers/administradores/admin_handlers.go": {"GenerateTempPassword()", "DefinirSenhaTemporaria()"},
	}
	for sufixo, termos := range exigem {
		src := fonte(t, arquivos, sufixo)
		for _, termo := range termos {
			if !strings.Contains(src, termo) {
				t.Errorf("%s deveria conter %q", sufixo, termo)
			}
		}
	}
	// RegisterAcademiaPublica: a senha é a escolhida pela própria academia.
	academia := fonte(t, arquivos, "internal/handlers/academias/academia_handlers.go")
	if fn := testsupport.ExtractFunctionSource(t, academia, "func RegisterAcademiaPublica("); strings.Contains(fn, "GenerateTempPassword") {
		t.Error("RegisterAcademiaPublica não deve gerar senha temporária")
	}
}
