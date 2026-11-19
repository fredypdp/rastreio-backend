package utils

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"spuri/internal/sysconfig"
)

func capturarLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	antigo := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(antigo) })
	return &buf
}

// Parte 3: LOG_LEVEL vem do banco (via cache, sem nunca consultar o banco) e, se
// faltar, do ambiente.
func TestDebugfSegueOLogLevelDoBancoOuDoAmbiente(t *testing.T) {
	t.Cleanup(sysconfig.Parar)
	buf := capturarLog(t)

	t.Setenv("LOG_LEVEL", "info")
	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{"LOG_LEVEL": "debug"}, 0))
	Debugf("diagnostico-%d", 1)
	if !strings.Contains(buf.String(), "diagnostico-1") {
		t.Fatalf("LOG_LEVEL=debug no banco deveria ligar o Debugf: %q", buf.String())
	}

	buf.Reset()
	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{}, 0))
	Debugf("diagnostico-%d", 2)
	if buf.Len() != 0 {
		t.Fatalf("sem valor no banco vale o ambiente (info): %q", buf.String())
	}

	t.Setenv("LOG_LEVEL", "debug")
	Debugf("diagnostico-%d", 3)
	if !strings.Contains(buf.String(), "diagnostico-3") {
		t.Fatalf("LOG_LEVEL=debug no ambiente continua funcionando: %q", buf.String())
	}
}
