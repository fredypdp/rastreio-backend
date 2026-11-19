package testsupport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ReadHandlerSource(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(RaizDoRepositorio(t), path))
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return string(data)
}

func MustContain(t *testing.T, source, want string) {
	t.Helper()
	if !strings.Contains(source, want) {
		t.Fatalf("source does not contain %q", want)
	}
}
