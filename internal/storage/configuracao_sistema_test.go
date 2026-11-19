package storage

import (
	"testing"

	"spuri/internal/sysconfig"
)

// Parte 3: MEGA_ROOT_FOLDER (e as credenciais Mega) leem do banco e, se faltar, do
// ambiente; o storage é criado uma vez no arranque.
func TestMegaRootFolderLeDoBancoDepoisDoAmbiente(t *testing.T) {
	t.Cleanup(sysconfig.Parar)
	t.Setenv("MEGA_ROOT_FOLDER", "pasta-do-ambiente")
	t.Setenv("MEGA_LOCAL_ROOT", t.TempDir())

	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{"MEGA_ROOT_FOLDER": "/pasta-do-banco/"}, 0))
	if got := NewLocalProvider().(*MegaProvider).rootFolder; got != "pasta-do-banco" {
		t.Fatalf("o banco deveria vencer o ambiente: %q", got)
	}
	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{}, 0))
	if got := NewLocalProvider().(*MegaProvider).rootFolder; got != "pasta-do-ambiente" {
		t.Fatalf("sem valor no banco deveria usar o ambiente: %q", got)
	}
}

func TestMegaExigeCredenciaisEmQualquerOrigem(t *testing.T) {
	t.Cleanup(sysconfig.Parar)
	t.Setenv("STORAGE_PROVIDER", "mega")
	t.Setenv("MEGA_EMAIL", "")
	t.Setenv("MEGA_PASSWORD", "")
	t.Setenv("MEGA_USE_LOCAL_FALLBACK", "")

	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{"MEGA_EMAIL": "conta@exemplo.ao"}, 0))
	if _, err := NewMegaProvider(); err == nil {
		t.Fatal("só o e-mail definido: a senha continua obrigatória")
	}
}

// O erro do Mega nunca pode devolver a senha nem o e-mail, venham do banco ou do ambiente.
func TestSanitizeMegaErrorRedigeCredenciaisDoBanco(t *testing.T) {
	t.Cleanup(sysconfig.Parar)
	t.Setenv("MEGA_EMAIL", "")
	t.Setenv("MEGA_PASSWORD", "")
	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{"MEGA_EMAIL": "conta@exemplo.ao", "MEGA_PASSWORD": "senha-super-secreta"}, 0))
	err := sanitizeMegaError(errString("login falhou para conta@exemplo.ao com senha-super-secreta"))
	if err == nil {
		t.Fatal("esperava erro")
	}
	if msg := err.Error(); containsAny(msg, "conta@exemplo.ao", "senha-super-secreta") {
		t.Fatalf("o erro vaza credenciais: %q", msg)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if x != "" && len(s) >= len(x) {
			for i := 0; i+len(x) <= len(s); i++ {
				if s[i:i+len(x)] == x {
					return true
				}
			}
		}
	}
	return false
}
