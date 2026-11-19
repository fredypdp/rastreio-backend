package apoio

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	prefixoHandlers = "spuri/internal/handlers"
	pacoteApoio     = prefixoHandlers + "/apoio"
	pacoteTeste     = prefixoHandlers + "/testsupport"
)

// TestPacotesDeModuloNaoSeImportam protege a regra da Parte 2: pacotes de módulo
// de internal/handlers só importam apoio (e testsupport, nos _test.go); apoio e
// testsupport não importam módulo nenhum; ninguém importa o pacote handlers
// (que guarda o HTTP do financeiro enquanto ele não migra).
func TestPacotesDeModuloNaoSeImportam(t *testing.T) {
	raiz := filepath.Join("..") // internal/handlers
	ents, err := os.ReadDir(raiz)
	if err != nil {
		t.Fatal(err)
	}
	var verificados int
	pastas := []string{"."} // "." é o pacote handlers (financeiro)
	for _, e := range ents {
		if e.IsDir() {
			pastas = append(pastas, e.Name())
		}
	}
	for _, modulo := range pastas {
		arquivos, _ := filepath.Glob(filepath.Join(raiz, modulo, "*.go"))
		for _, arq := range arquivos {
			f, err := parser.ParseFile(token.NewFileSet(), arq, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("%s: %v", arq, err)
			}
			ehTeste := strings.HasSuffix(arq, "_test.go")
			verificados++
			for _, imp := range f.Imports {
				caminho := strings.Trim(imp.Path.Value, `"`)
				if caminho != prefixoHandlers && !strings.HasPrefix(caminho, prefixoHandlers+"/") {
					continue
				}
				permitido := false
				switch modulo {
				case "apoio":
					permitido = false
				case "testsupport":
					permitido = caminho == pacoteApoio
				default:
					permitido = caminho == pacoteApoio || (ehTeste && caminho == pacoteTeste)
				}
				if !permitido {
					t.Errorf("%s importa %s: pacote de módulo só pode importar %s (e %s nos testes)", arq, caminho, pacoteApoio, pacoteTeste)
				}
			}
		}
	}
	if verificados < 50 {
		t.Fatalf("varredura incompleta: só %d arquivos verificados", verificados)
	}
}
