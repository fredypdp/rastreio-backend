package sysconfig

import (
	"errors"
	"strings"
	"testing"
)

func TestListaAprovadaTemChavesUnicasEDescricao(t *testing.T) {
	vistas := map[string]bool{}
	for _, d := range Definicoes() {
		if d.Chave == "" || d.Grupo == "" || d.Descricao == "" {
			t.Errorf("definição incompleta: %+v", d)
		}
		if vistas[d.Chave] {
			t.Errorf("chave duplicada: %s", d.Chave)
		}
		vistas[d.Chave] = true
		if d.Aplicacao != AplicaImediato && d.Aplicacao != AplicaReinicio {
			t.Errorf("%s: aplicação inválida %q", d.Chave, d.Aplicacao)
		}
	}
}

// A lista é fechada: o que o sistema precisa para chegar ao banco ou para proteger o
// cofre nunca pode ser editado pela tela.
func TestChavesDeBootstrapNaoSaoEditaveis(t *testing.T) {
	for _, proibida := range []string{"DATABASE_URL", "JWT_SECRET", "ENV", "PORT", "STORAGE_PROVIDER", "BOOTSTRAP_TOKEN", "FINANCE_ENCRYPTION_KEY", "MEGA_LOCAL_ROOT"} {
		if _, ok := Buscar(proibida); ok {
			t.Errorf("%s não pode estar na lista de configurações editáveis", proibida)
		}
	}
}

func TestSegredosDaListaAprovada(t *testing.T) {
	for _, k := range []string{"BREVO_API_KEY", "ZIETT_API_KEY", "MEGA_EMAIL", "MEGA_PASSWORD"} {
		d, ok := Buscar(k)
		if !ok || !d.Segredo() {
			t.Errorf("%s deveria ser segredo", k)
		}
	}
	if d, _ := Buscar("MEGA_ROOT_FOLDER"); d.Segredo() {
		t.Error("MEGA_ROOT_FOLDER é só o nome de uma pasta: fica visível")
	}
}

func TestValidar(t *testing.T) {
	casos := []struct {
		chave, entrada, esperado string
		erro                     bool
	}{
		{"JWT_EXPIRY_HOURS", " 12 ", "12", false},
		{"JWT_EXPIRY_HOURS", "0", "", true},
		{"JWT_EXPIRY_HOURS", "721", "", true},
		{"JWT_EXPIRY_HOURS", "1.5", "", true},
		{"JWT_EXPIRY_HOURS", "abc", "", true},
		{"JWT_EXPIRY_HOURS", "", "", true},
		{"LOG_LEVEL", "DEBUG", "debug", false},
		{"LOG_LEVEL", "trace", "", true},
		{"RECONCILIACAO_ATIVA", "Sim", "true", false},
		{"RECONCILIACAO_ATIVA", "não", "false", false},
		{"RECONCILIACAO_ATIVA", "talvez", "", true},
		{"RECONCILIACAO_CHAMADAS_POR_SEGUNDO", "0.5", "0.5", false},
		{"RECONCILIACAO_CHAMADAS_POR_SEGUNDO", "0", "", true},
		{"RECONCILIACAO_RESYNC_MIN", "0", "0", false},
		{"RECONCILIACAO_RESYNC_MIN", "-1", "", true},
		{"RECONCILIACAO_CONCORRENCIA", "21", "", true},
		{"EMAIL_FROM", "nao-responder@exemplo.ao", "nao-responder@exemplo.ao", false},
		{"EMAIL_FROM", "Spuri <nao-responder@exemplo.ao>", "", true},
		{"EMAIL_FROM", "sem-arroba", "", true},
		{"APPYPAY_RESOURCE", "ABCDEF12-3456-7890-ABCD-EF1234567890", "abcdef12-3456-7890-abcd-ef1234567890", false},
		{"APPYPAY_RESOURCE", "nao-e-uuid", "", true},
		{"ALLOWED_ORIGINS", "https://app.exemplo.ao/, http://localhost:3000", "https://app.exemplo.ao,http://localhost:3000", false},
		{"ALLOWED_ORIGINS", "app.exemplo.ao", "", true},
		{"ALLOWED_ORIGINS", "https://app.exemplo.ao/painel", "", true},
		{"ALLOWED_ORIGINS", "*", "", true},
		{"ALLOWED_ORIGINS", " , ", "", true},
		{"MEGA_ROOT_FOLDER", "spuri/producao", "spuri/producao", false},
		{"MEGA_ROOT_FOLDER", "../fora", "", true},
		{"BREVO_API_KEY", "xkeysib-abc123", "xkeysib-abc123", false},
		{"BREVO_API_KEY", "linha1\nlinha2", "", true},
		{"BREVO_API_KEY", strings.Repeat("a", TamanhoMaximoValor+1), "", true},
	}
	for _, c := range casos {
		d, ok := Buscar(c.chave)
		if !ok {
			t.Fatalf("chave %s não existe", c.chave)
		}
		got, err := d.Validar(c.entrada)
		if c.erro {
			var ev *ErroValidacao
			if err == nil || !errors.As(err, &ev) {
				t.Errorf("%s(%q): esperava ErroValidacao, obteve %v / %q", c.chave, c.entrada, err, got)
			}
			continue
		}
		if err != nil || got != c.esperado {
			t.Errorf("%s(%q): esperava %q, obteve %q / %v", c.chave, c.entrada, c.esperado, got, err)
		}
	}
}

// A mensagem de erro de um segredo nunca pode repetir o valor recebido.
func TestErroDeValidacaoNaoRepeteOValorDoSegredo(t *testing.T) {
	d, _ := Buscar("MEGA_PASSWORD")
	segredo := "senha\nsecretissima-123"
	_, err := d.Validar(segredo)
	if err == nil {
		t.Fatal("esperava erro")
	}
	if strings.Contains(err.Error(), "secretissima") {
		t.Fatalf("o erro repete o valor do segredo: %v", err)
	}
}
