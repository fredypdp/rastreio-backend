package cadastros

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"spuri/internal/domain/aggregates"
)

func ptrUUID() *uuid.UUID { x := uuid.New(); return &x }

func TestSumarioNovoValidacoes(t *testing.T) {
	casos := []struct {
		nivel, tipo string
		curso       *uuid.UUID
		ok          bool
	}{
		{"fundamental", aggregates.TipoEscolar, nil, true},
		{"medio", aggregates.TipoEscolar, nil, false},
		{"superior", aggregates.TipoSuperior, nil, false},
		{"fundamental", aggregates.TipoEscolar, ptrUUID(), false},
		{"superior", aggregates.TipoSuperior, ptrUUID(), true},
		{"medio", aggregates.TipoEscolar, ptrUUID(), true},
		{"superior", aggregates.TipoEscolar, ptrUUID(), false}, // type incoerente com nivel
	}
	for _, tt := range casos {
		_, err := NovoSumario("Aula válida", nil, "ACA", tt.tipo, tt.nivel, "1_trimestre", "7_ano_fundamental", tt.curso, uuid.New(), uuid.New())
		if (err == nil) != tt.ok {
			t.Errorf("%+v: %v", tt, err)
		}
	}
	if _, err := NovoSumario("Aula válida", nil, "ACA", aggregates.TipoEscolar, "fundamental", "", "7_ano_fundamental", nil, uuid.New(), uuid.New()); err == nil {
		t.Fatal("periodo vazio deveria falhar")
	}
	if _, err := NovoSumario("ab", nil, "ACA", aggregates.TipoEscolar, "fundamental", "1_trimestre", "7_ano_fundamental", nil, uuid.New(), uuid.New()); err == nil {
		t.Fatal("título curto deveria falhar")
	}
}

func TestSumarioAtualizarEDeletar(t *testing.T) {
	s, err := NovoSumario("  Aula 1  ", nil, "ACA", aggregates.TipoEscolar, "fundamental", "1_trimestre", "7_ano_fundamental", nil, uuid.New(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if s.SumarioTitulo != "Aula 1" || s.Status != StatusAtivo {
		t.Fatalf("sumário novo: %+v", s)
	}
	if err := s.AtualizarDados(nil, nil); err == nil {
		t.Fatal("sem dados deveria falhar")
	}
	curto := "ab"
	if err := s.AtualizarDados(&curto, nil); err == nil {
		t.Fatal("título curto deveria falhar")
	}
	longa := strings.Repeat("x", 2001)
	if err := s.AtualizarDados(nil, &longa); err == nil {
		t.Fatal("descrição longa deveria falhar")
	}
	if s.SumarioTitulo != "Aula 1" {
		t.Fatalf("recusa não pode alterar: %q", s.SumarioTitulo)
	}
	novo, desc := "Aula 2", "  texto  "
	if err := s.AtualizarDados(&novo, &desc); err != nil || s.SumarioTitulo != "Aula 2" || *s.Descricao != "texto" {
		t.Fatalf("atualizar: %v %+v", err, s)
	}
	if err := s.Deletar(); err != nil || s.Status != StatusDeletado || s.DeletedAt == nil {
		t.Fatalf("deletar: %v", err)
	}
	if err := s.Deletar(); err == nil {
		t.Fatal("deletar duas vezes deveria falhar")
	}
	if err := s.AtualizarDados(&novo, nil); err == nil {
		t.Fatal("atualizar sumário deletado deveria falhar")
	}
}
