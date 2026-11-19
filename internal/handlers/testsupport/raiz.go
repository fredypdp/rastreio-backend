package testsupport

import (
	"os"
	"path/filepath"
	"testing"
)

// RaizDoRepositorio sobe a partir do diretório do teste até achar o go.mod.
// Os testes de integração precisam da raiz (pasta migrations/) e os testes
// estáticos leem arquivos-fonte por caminho; com os handlers divididos em
// pacotes, a profundidade do diretório do teste deixou de ser fixa.
func RaizDoRepositorio(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		pai := filepath.Dir(dir)
		if pai == dir {
			t.Fatal("go.mod não encontrado acima do diretório do teste")
		}
		dir = pai
	}
}
