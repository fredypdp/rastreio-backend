package cadastros

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ── Curso: regras portadas dos testes do antigo agregado ────────────────────

func TestCursoSuperiorAtualizarPeriodosRecalculaAnosAntesDeValidar(t *testing.T) {
	curso, err := NovoCurso("Engenharia Informática", "superior", "",
		[]string{"1_ano_superior", "2_ano_superior", "3_ano_superior"},
		[]string{"1_semestre", "2_semestre", "3_semestre", "4_semestre", "5_semestre", "6_semestre"}, "ACA_01")
	if err != nil {
		t.Fatalf("NovoCurso() erro inesperado: %v", err)
	}
	novosAnos := []string{"1_ano_superior", "2_ano_superior", "3_ano_superior", "4_ano_superior"}
	novosPeriodos := []string{"1_semestre", "2_semestre", "3_semestre", "4_semestre", "5_semestre", "6_semestre", "7_semestre", "8_semestre"}
	if err := curso.AtualizarDados(nil, novosAnos, &novosPeriodos, nil); err != nil {
		t.Fatalf("AtualizarDados() deve aceitar anos derivados dos novos períodos, erro: %v", err)
	}
	if !reflect.DeepEqual(curso.AnosAcademicos, novosAnos) || !reflect.DeepEqual(curso.Periodos, novosPeriodos) {
		t.Fatalf("anos=%v periodos=%v", curso.AnosAcademicos, curso.Periodos)
	}
}

func TestCursoSuperiorAtualizarPeriodosRejeitaAnosNaoDerivados(t *testing.T) {
	curso, err := NovoCurso("Engenharia Informática", "superior", "",
		[]string{"1_ano_superior", "2_ano_superior"},
		[]string{"1_semestre", "2_semestre", "3_semestre", "4_semestre"}, "ACA_01")
	if err != nil {
		t.Fatal(err)
	}
	antesAnos := append([]string(nil), curso.AnosAcademicos...)
	novosAnos := []string{"1_ano_superior", "2_ano_superior", "3_ano_superior"}
	novosPeriodos := []string{"1_semestre", "2_semestre", "3_semestre", "4_semestre"}
	if err := curso.AtualizarDados(nil, novosAnos, &novosPeriodos, nil); err == nil {
		t.Fatal("AtualizarDados() deve rejeitar anos superiores não derivados dos períodos")
	}
	if !reflect.DeepEqual(curso.AnosAcademicos, antesAnos) {
		t.Fatalf("estado mudou apesar da recusa: %v", curso.AnosAcademicos)
	}
}

func TestCursoMedioModeloObrigatorioEValido(t *testing.T) {
	for _, modelo := range []string{"liceu", "tecnico"} {
		curso, err := NovoCurso("Curso Médio", "medio", modelo, nil, nil, "ACA_01")
		if err != nil {
			t.Fatalf("modelo %s deveria ser aceito: %v", modelo, err)
		}
		if curso.Modelo != modelo {
			t.Fatalf("Modelo = %q, want %q", curso.Modelo, modelo)
		}
		esperado, _ := DerivarAnosAcademicosCursoMedio(modelo)
		if !reflect.DeepEqual(curso.AnosAcademicos, esperado) {
			t.Fatalf("modelo %s derivou anos %v, want %v", modelo, curso.AnosAcademicos, esperado)
		}
		if curso.Status != StatusAtivo {
			t.Fatalf("curso novo deve nascer ativo, got %q", curso.Status)
		}
	}
	for _, modelo := range []string{"", "profissional", "LICEU"} {
		if _, err := NovoCurso("Curso Médio", "medio", modelo, nil, nil, "ACA_01"); err == nil {
			t.Fatalf("modelo %q deveria ser rejeitado", modelo)
		}
	}
}

func TestCursoSuperiorRejeitaModelo(t *testing.T) {
	if _, err := NovoCurso("Engenharia", "superior", "tecnico", []string{"1_ano_superior"}, []string{"1_semestre"}, "ACA_01"); err == nil {
		t.Fatal("curso superior com modelo deveria ser rejeitado")
	}
}

func TestCursoMedioRejeitaAtualizacaoDeModelo(t *testing.T) {
	curso, err := NovoCurso("Curso Médio", "medio", "liceu", nil, nil, "ACA_01")
	if err != nil {
		t.Fatal(err)
	}
	modelo := "tecnico"
	if err := curso.AtualizarDados(nil, nil, nil, &modelo); err == nil {
		t.Fatal("AtualizarDados() deveria rejeitar troca de modelo de curso médio")
	}
	if curso.Modelo != "liceu" {
		t.Fatalf("modelo mudou apesar da recusa: %q", curso.Modelo)
	}
}

func TestCursoMedioRejeitaAnosAcademicosManuais(t *testing.T) {
	if _, err := NovoCurso("Curso Médio", "medio", "tecnico",
		[]string{"1_ano_medio", "2_ano_medio", "3_ano_medio", "4_ano_medio"}, nil, "ACA_01"); err == nil {
		t.Fatal("NovoCurso() deveria rejeitar anos_academicos manuais em curso médio")
	}
}

func TestCursoMedioNuncaTemPeriodos(t *testing.T) {
	if _, err := NovoCurso("Curso Médio", "medio", "liceu", nil, []string{"1_semestre"}, "ACA_01"); err == nil {
		t.Fatal("curso médio com períodos deveria ser rejeitado")
	}
}

func TestCursoSuperiorPeriodosPrecisamSerSequenciais(t *testing.T) {
	casos := map[string][]string{
		"vazio":      nil,
		"pulando":    {"1_semestre", "3_semestre"},
		"duplicado":  {"1_semestre", "1_semestre"},
		"formato":    {"semestre_1"},
		"fora de 1º": {"2_semestre", "3_semestre"},
	}
	for nome, periodos := range casos {
		t.Run(nome, func(t *testing.T) {
			if _, err := NovoCurso("Eng", "superior", "", []string{"1_ano_superior"}, periodos, "ACA_01"); err == nil {
				t.Fatalf("períodos %v deveriam ser rejeitados", periodos)
			}
		})
	}
}

func TestCursoCamposObrigatorios(t *testing.T) {
	if _, err := NovoCurso("", "superior", "", []string{"1_ano_superior"}, []string{"1_semestre"}, "ACA_01"); err == nil {
		t.Fatal("nome vazio")
	}
	if _, err := NovoCurso("X", "basico", "", nil, nil, "ACA_01"); err == nil {
		t.Fatal("tipo inválido")
	}
	if _, err := NovoCurso("X", "superior", "", []string{"1_ano_superior"}, []string{"1_semestre"}, ""); err == nil {
		t.Fatal("código da academia vazio")
	}
}

func TestCursoCicloDeStatus(t *testing.T) {
	c, _ := NovoCurso("Eng", "superior", "", []string{"1_ano_superior"}, []string{"1_semestre"}, "ACA_01")
	if err := c.Ativar(); err == nil {
		t.Fatal("ativar curso ativo deve falhar")
	}
	if err := c.Deletar(); err == nil || !strings.Contains(err.Error(), "desative o curso") {
		t.Fatalf("deletar curso ativo deve exigir desativação: %v", err)
	}
	if err := c.Desativar(); err != nil {
		t.Fatal(err)
	}
	if err := c.Desativar(); err == nil {
		t.Fatal("desativar curso inativo deve falhar")
	}
	if err := c.Deletar(); err != nil {
		t.Fatalf("deletar curso inativo: %v", err)
	}
	if c.Status != StatusDeletado || c.DeletedAt == nil {
		t.Fatalf("status=%q deleted_at=%v", c.Status, c.DeletedAt)
	}
	if err := c.Deletar(); err == nil {
		t.Fatal("deletar curso já deletado deve falhar")
	}
}

func TestCursoAtualizarSemCamposFalha(t *testing.T) {
	c, _ := NovoCurso("Eng", "superior", "", []string{"1_ano_superior"}, []string{"1_semestre"}, "ACA_01")
	if err := c.AtualizarDados(nil, nil, nil, nil); err == nil {
		t.Fatal("nenhum campo para atualizar deve falhar")
	}
}

func TestErroRegraEhDetectavelMesmoEmbrulhado(t *testing.T) {
	_, err := NovoCurso("", "superior", "", nil, nil, "ACA_01")
	if _, ok := EhErroRegra(err); !ok {
		t.Fatalf("erro de regra não detectado: %T %v", err, err)
	}
	if _, ok := EhErroRegra(ErrNaoEncontrado); ok {
		t.Fatal("ErrNaoEncontrado não é violação de regra")
	}
}

// ── Matéria: regras portadas dos testes do antigo agregado ──────────────────

func TestMateriaSuperiorCriadaComPendenciaPermitidaNasceAtiva(t *testing.T) {
	cursoID := uuid.New()
	m, err := NovaMateria("Cálculo I", "superior", []string{"1_semestre"}, "ACA001", &cursoID, nil, nil)
	if err != nil {
		t.Fatalf("erro inesperado ao criar matéria superior: %v", err)
	}
	if !m.PendenciaPermitida {
		t.Fatal("esperava pendencia_permitida=true para matéria superior")
	}
	if m.Status != StatusAtivo {
		t.Fatalf("esperava status ativo, obtido %q", m.Status)
	}
}

func TestMateriaEscolarNaoPermiteAtualizarPendenciaMesmoFalse(t *testing.T) {
	m, err := NovaMateria("Matemática", "medio", []string{"10_classe"}, "ACA001", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	falso := false
	if err := m.AtualizarDados(nil, nil, nil, &falso, nil); err == nil {
		t.Fatal("esperava erro ao atualizar pendencia_permitida em matéria escolar")
	}
}

func TestMateriaSuperiorMantemPendenciaPermitidaFalseExplicita(t *testing.T) {
	cursoID := uuid.New()
	falso := false
	m, err := NovaMateria("Cálculo II", "superior", []string{"2_semestre"}, "ACA001", &cursoID, &falso, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.PendenciaPermitida {
		t.Fatal("esperava manter pendencia_permitida=false explícito para matéria superior")
	}
}

func TestMateriaEscolarNaoPermiteCriarPendenciaMesmoFalse(t *testing.T) {
	falso := false
	if _, err := NovaMateria("Matemática", "medio", []string{"10_classe"}, "ACA001", nil, &falso, nil); err == nil {
		t.Fatal("esperava erro ao criar matéria escolar com pendencia_permitida explícita")
	}
	nivel := "2_semestre"
	if _, err := NovaMateria("Matemática", "fundamental", []string{"1_ano_fundamental"}, "ACA001", nil, nil, &nivel); err == nil {
		t.Fatal("esperava erro ao criar matéria escolar com pendencia_nivel_conclusao")
	}
}

func TestMateriaMedioPermiteMaisDeUmAnoAcademicoEPAP(t *testing.T) {
	m, err := NovaMateria("Ciências", "medio", []string{"1_ano_medio", "2_ano_medio"}, "ACA001", nil, nil, nil)
	if err != nil || len(m.AnosAcademicos) != 2 {
		t.Fatalf("múltiplos anos: err=%v anos=%v", err, m)
	}
	// A restrição do 4º ano técnico (PAP) fica no handler, como antes.
	if _, err := NovaMateria("PAP", "medio", []string{"4_ano_medio"}, "ACA001", nil, nil, nil); err != nil {
		t.Fatalf("4_ano_medio deve ser aceito pela regra de domínio: %v", err)
	}
}

func TestMateriaCamposObrigatorios(t *testing.T) {
	curso := uuid.New()
	if _, err := NovaMateria("", "medio", []string{"1_ano_medio"}, "ACA", nil, nil, nil); err == nil {
		t.Fatal("nome vazio")
	}
	if _, err := NovaMateria("X", "escolar", []string{"1"}, "ACA", nil, nil, nil); err == nil {
		t.Fatal("tipo inválido")
	}
	if _, err := NovaMateria("X", "medio", []string{"1_ano_medio"}, "", nil, nil, nil); err == nil {
		t.Fatal("academia vazia")
	}
	if _, err := NovaMateria("X", "medio", nil, "ACA", nil, nil, nil); err == nil {
		t.Fatal("anos vazios")
	}
	if _, err := NovaMateria("X", "superior", []string{"1_ano_superior"}, "ACA", nil, nil, nil); err == nil {
		t.Fatal("superior sem curso")
	}
	if _, err := NovaMateria("X", "superior", []string{"1_ano_superior"}, "ACA", &curso, nil, nil); err != nil {
		t.Fatalf("superior com curso: %v", err)
	}
}

func TestMateriaSuperiorSoAtivaComPeriodo(t *testing.T) {
	curso := uuid.New()
	m, _ := NovaMateria("Cálculo", "superior", []string{"1_ano_superior"}, "ACA", &curso, nil, nil)
	if err := m.Desativar(); err != nil {
		t.Fatal(err)
	}
	if err := m.Ativar(); err == nil || !strings.Contains(err.Error(), "periodo") {
		t.Fatalf("ativar superior sem período deve falhar: %v", err)
	}
	if err := m.DefinirPeriodo("1_semestre"); err != nil {
		t.Fatal(err)
	}
	if err := m.Ativar(); err != nil {
		t.Fatalf("ativar com período: %v", err)
	}
}

func TestMateriaPeriodoSoParaSuperior(t *testing.T) {
	m, _ := NovaMateria("Mat", "medio", []string{"1_ano_medio"}, "ACA", nil, nil, nil)
	if err := m.DefinirPeriodo("1_semestre"); err == nil {
		t.Fatal("período em matéria média deve falhar")
	}
	curso := uuid.New()
	s, _ := NovaMateria("Cálc", "superior", []string{"1_ano_superior"}, "ACA", &curso, nil, nil)
	if err := s.DefinirPeriodo(""); err == nil {
		t.Fatal("período vazio deve falhar")
	}
}

func TestMateriaCicloDeStatus(t *testing.T) {
	m, _ := NovaMateria("Mat", "medio", []string{"1_ano_medio"}, "ACA", nil, nil, nil)
	if err := m.Ativar(); err == nil {
		t.Fatal("ativar matéria ativa deve falhar")
	}
	if err := m.Deletar(); err == nil || !strings.Contains(err.Error(), "desative a matéria") {
		t.Fatalf("deletar matéria ativa: %v", err)
	}
	if err := m.Desativar(); err != nil {
		t.Fatal(err)
	}
	if err := m.Desativar(); err == nil {
		t.Fatal("desativar matéria inativa deve falhar")
	}
	if err := m.Deletar(); err != nil {
		t.Fatal(err)
	}
	if err := m.Deletar(); err == nil {
		t.Fatal("deletar duas vezes deve falhar")
	}
}

func TestMateriaAtualizarDados(t *testing.T) {
	m, _ := NovaMateria("Mat", "medio", []string{"1_ano_medio"}, "ACA", nil, nil, nil)
	if err := m.AtualizarDados(nil, nil, nil, nil, nil); err == nil {
		t.Fatal("nenhum campo para atualizar")
	}
	if err := m.AtualizarDados(nil, []string{}, nil, nil, nil); err == nil {
		t.Fatal("anos vazios devem falhar")
	}
	novo := "Matemática"
	if err := m.AtualizarDados(&novo, []string{"2_ano_medio"}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if m.Nome != novo || !reflect.DeepEqual(m.AnosAcademicos, []string{"2_ano_medio"}) {
		t.Fatalf("estado: %+v", m)
	}
}
