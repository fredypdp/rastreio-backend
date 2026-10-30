package services

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Tarefa 122 — partes do 2FA que não precisam de banco.

func TestGerarCodigoNumericoTemSeisDigitosEPreservaZerosAEsquerda(t *testing.T) {
	re := regexp.MustCompile(`^[0-9]{6}$`)
	vistos := map[string]bool{}
	for i := 0; i < 2000; i++ {
		c, err := gerarCodigoNumerico()
		if err != nil {
			t.Fatal(err)
		}
		if !re.MatchString(c) {
			t.Fatalf("código fora do formato: %q", c)
		}
		vistos[c] = true
	}
	// 2000 sorteios de 1.000.000 possibilidades: um gerador quebrado (constante)
	// ficaria muito abaixo disto.
	if len(vistos) < 1900 {
		t.Fatalf("variedade suspeita de códigos: %d distintos em 2000", len(vistos))
	}
}

func TestHashCodigoDoisFatoresAmarraDesafioUsuarioETipo(t *testing.T) {
	t.Setenv("JWT_SECRET", "segredo-de-teste")
	user := uuid.New()
	d1, d2 := uuid.New(), uuid.New()

	h1, err := hashCodigoDoisFatores(d1, user, "admin", DoisFatoresFinalidadeLogin, "123456")
	if err != nil {
		t.Fatal(err)
	}
	if len(h1) != 64 || strings.Contains(h1, "123456") {
		t.Fatalf("hash inesperado: %q", h1)
	}
	igual, _ := hashCodigoDoisFatores(d1, user, "admin", DoisFatoresFinalidadeLogin, "123456")
	if igual != h1 {
		t.Fatal("o hash deve ser determinístico para os mesmos dados")
	}
	// Mesmo código, outro desafio: hash diferente (é isto que distingue dois
	// códigos iguais gerados em momentos diferentes).
	outroDesafio, _ := hashCodigoDoisFatores(d2, user, "admin", DoisFatoresFinalidadeLogin, "123456")
	if outroDesafio == h1 {
		t.Fatal("o mesmo código em desafios diferentes deve ter hashes diferentes")
	}
	outroUsuario, _ := hashCodigoDoisFatores(d1, uuid.New(), "admin", DoisFatoresFinalidadeLogin, "123456")
	outroTipo, _ := hashCodigoDoisFatores(d1, user, "estudante", DoisFatoresFinalidadeLogin, "123456")
	outraFinalidade, _ := hashCodigoDoisFatores(d1, user, "admin", DoisFatoresFinalidadeAtivacao, "123456")
	for nome, h := range map[string]string{"usuário": outroUsuario, "tipo": outroTipo, "finalidade": outraFinalidade} {
		if h == h1 {
			t.Errorf("trocar %s deveria mudar o hash", nome)
		}
	}
}

func TestHashCodigoDoisFatoresExigeJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	if _, err := hashCodigoDoisFatores(uuid.New(), uuid.New(), "admin", DoisFatoresFinalidadeLogin, "123456"); err == nil {
		t.Fatal("sem JWT_SECRET o hash não pode ser calculado (falha fechada)")
	}
}

func TestMensagensDeErroDoisFatores(t *testing.T) {
	if got := (&ErrDoisFatoresCodigoIncorreto{Restantes: 4}).Error(); !strings.Contains(got, "4 tentativas") {
		t.Errorf("mensagem: %q", got)
	}
	if got := (&ErrDoisFatoresCodigoIncorreto{Restantes: 1}).Error(); !strings.Contains(got, "1 tentativa") {
		t.Errorf("mensagem: %q", got)
	}
	if got := (&ErrDoisFatoresCodigoIncorreto{Restantes: 0}).Error(); !strings.Contains(got, "máximo de tentativas") {
		t.Errorf("mensagem: %q", got)
	}
	if got := (&ErrDoisFatoresLimiteEnvios{Segundos: 90}).Error(); !strings.Contains(got, "2 minuto") {
		t.Errorf("mensagem: %q", got)
	}
}

// ── E-mail ──────────────────────────────────────────────────────────────────

func TestRenderDoisFatoresEmailEscapaNomeEIncluiCodigo(t *testing.T) {
	htmlOut := renderDoisFatoresEmailHTML(`<script>alert(1)</script> Ana`, DoisFatoresFinalidadeLogin, "042917", 10, "https://painel.example.test")
	if strings.Contains(htmlOut, "<script>") {
		t.Fatal("o nome do usuário deve ser escapado no HTML")
	}
	for _, esperado := range []string{"042917", "10 minutos", "Nunca partilhe este código", "Seu código de acesso"} {
		if !strings.Contains(htmlOut, esperado) {
			t.Errorf("HTML sem %q", esperado)
		}
	}
	texto := renderDoisFatoresEmailText("Ana", DoisFatoresFinalidadeDesativacao, "042917", 10)
	if !strings.Contains(texto, "042917") || !strings.Contains(texto, "desativar") {
		t.Errorf("texto inesperado: %q", texto)
	}
	if !strings.Contains(assuntoCodigoDoisFatores(DoisFatoresFinalidadeAtivacao), "Ative") ||
		!strings.Contains(assuntoCodigoDoisFatores(DoisFatoresFinalidadeDesativacao), "Desative") {
		t.Error("assuntos de ativação/desativação incorretos")
	}
}

func limparEnvEmail(t *testing.T) {
	t.Helper()
	for _, k := range []string{"BREVO_API_KEY", "EMAIL_FROM", "EMAIL_USER", "DOIS_FATORES_LOG_CODIGO"} {
		t.Setenv(k, "")
	}
}

func TestEnviarCodigoDoisFatoresSemBrevoFalhaFechado(t *testing.T) {
	limparEnvEmail(t)
	t.Setenv("ENV", "development")
	svc := &EmailService{}
	err := svc.EnviarCodigoDoisFatores("a@example.test", "Ana", DoisFatoresFinalidadeLogin, "123456", 10)
	if !errors.Is(err, ErrEmailNaoConfigurado) {
		t.Fatalf("sem Brevo deveria falhar com ErrEmailNaoConfigurado, obteve %v", err)
	}
}

func TestEnviarCodigoDoisFatoresLogDeDesenvolvimentoSoForaDeProducao(t *testing.T) {
	limparEnvEmail(t)
	t.Setenv("DOIS_FATORES_LOG_CODIGO", "true")

	t.Setenv("ENV", "development")
	svc := &EmailService{}
	if err := svc.EnviarCodigoDoisFatores("a@example.test", "Ana", DoisFatoresFinalidadeLogin, "123456", 10); err != nil {
		t.Fatalf("em desenvolvimento com a opção explícita o código vai para o log: %v", err)
	}

	t.Setenv("ENV", "production")
	if err := svc.EnviarCodigoDoisFatores("a@example.test", "Ana", DoisFatoresFinalidadeLogin, "123456", 10); !errors.Is(err, ErrEmailNaoConfigurado) {
		t.Fatalf("em produção a opção de log deve ser ignorada, obteve %v", err)
	}
}

func TestEnviarCodigoDoisFatoresRejeitaDestinatarioVazio(t *testing.T) {
	limparEnvEmail(t)
	svc := &EmailService{}
	if err := svc.EnviarCodigoDoisFatores("  ", "Ana", DoisFatoresFinalidadeLogin, "123456", 10); err == nil {
		t.Fatal("destinatário vazio deve falhar")
	}
}
