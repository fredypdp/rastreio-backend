package aggregates

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func novaTurmaParaHistorico(t *testing.T) *Turma {
	t.Helper()
	turma := NewTurma()
	if err := turma.Criar("TURMA_HIST", "ACA_HIST", "6_ano_fundamental", nil, "manha", nil, uuid.New()); err != nil {
		t.Fatal(err)
	}
	return turma
}

func TestTurmaAtualizarDadosPermiteIdentidadeSemHistorico(t *testing.T) {
	turma := novaTurmaParaHistorico(t)
	nivel := "7_ano_fundamental"
	if err := turma.AtualizarDados(&nivel, nil, nil, nil, uuid.New()); err != nil {
		t.Fatalf("turma sem histórico deve aceitar correção de nível: %v", err)
	}
}

func TestTurmaAtualizarDadosBloqueiaIdentidadeComHistorico(t *testing.T) {
	turma := novaTurmaParaHistorico(t)
	if err := turma.AdicionarEstudanteNoAnoLectivo("EST-HIST", "2025_2026", uuid.New()); err != nil {
		t.Fatal(err)
	}

	// Antes da proteção da tarefa 31 este comando era aceite e permitia que a
	// projeção resolvesse meses antigos com a identidade atual da turma.
	nivel := "7_ano_fundamental"
	err := turma.AtualizarDados(&nivel, nil, nil, nil, uuid.New())
	if err == nil || !strings.Contains(err.Error(), "histórico") {
		t.Fatalf("alteração de nível com histórico = %v, queria erro claro", err)
	}

	cursoID := uuid.New()
	err = turma.AtualizarDados(nil, &cursoID, nil, nil, uuid.New())
	if err == nil || !strings.Contains(err.Error(), "histórico") {
		t.Fatalf("alteração de curso com histórico = %v, queria erro claro", err)
	}
}

func TestTurmaAtualizarDadosPermiteTurnoComHistorico(t *testing.T) {
	turma := novaTurmaParaHistorico(t)
	if err := turma.AdicionarEstudanteNoAnoLectivo("EST-HIST", "2025_2026", uuid.New()); err != nil {
		t.Fatal(err)
	}
	turno := "tarde"
	if err := turma.AtualizarDados(nil, nil, &turno, nil, uuid.New()); err != nil {
		t.Fatalf("turno com histórico deve continuar editável: %v", err)
	}
}

func TestTipoAgrupamentoDoNivel(t *testing.T) {
	casos := map[string]string{
		"4_ano_medio":       "grupo",
		" 4_ano_medio ":     "grupo",
		"3_ano_medio":       "turma",
		"1_ano_medio":       "turma",
		"7_ano_fundamental": "turma",
		"1_semestre":        "turma",
	}
	for nivel, esperado := range casos {
		if got := TipoAgrupamentoDoNivel(nivel); got != esperado {
			t.Fatalf("TipoAgrupamentoDoNivel(%q) = %q, esperado %q", nivel, got, esperado)
		}
	}
}

func TestTurmaGrupoDoQuartoAnoMedioAceitaTemaDeTrabalho(t *testing.T) {
	tema := "  Sistema de gestão escolar  "
	turma := NewTurma()
	if err := turma.Criar("GRUPO_A", "ACA_GRUPO", "4_ano_medio", nil, "manha", &tema, uuid.New()); err != nil {
		t.Fatalf("criar grupo com tema: %v", err)
	}
	if turma.TemaTrabalho == nil || *turma.TemaTrabalho != "Sistema de gestão escolar" {
		t.Fatalf("tema esperado normalizado (sem espaços nas pontas), obteve %v", turma.TemaTrabalho)
	}
}

func TestTurmaGrupoDoQuartoAnoMedioPermiteCriarSemTema(t *testing.T) {
	vazio := "   "
	turma := NewTurma()
	if err := turma.Criar("GRUPO_B", "ACA_GRUPO", "4_ano_medio", nil, "tarde", &vazio, uuid.New()); err != nil {
		t.Fatalf("tema em branco deve ser tratado como ausente: %v", err)
	}
	if turma.TemaTrabalho != nil {
		t.Fatalf("tema em branco deveria virar nil, obteve %q", *turma.TemaTrabalho)
	}
}

func TestTurmaTemaDeTrabalhoRejeitadoForaDoQuartoAnoMedio(t *testing.T) {
	tema := "Tema qualquer"
	turma := NewTurma()
	if err := turma.Criar("TURMA_X", "ACA_GRUPO", "3_ano_medio", nil, "manha", &tema, uuid.New()); err == nil {
		t.Fatal("tema_trabalho em turma do 3º ano médio deveria ser rejeitado")
	}
	turma = NewTurma()
	if err := turma.Criar("TURMA_Y", "ACA_GRUPO", "3_ano_medio", nil, "manha", nil, uuid.New()); err != nil {
		t.Fatalf("criar turma sem tema: %v", err)
	}
	if err := turma.AtualizarDados(nil, nil, nil, &tema, uuid.New()); err == nil {
		t.Fatal("atualizar tema_trabalho em turma do 3º ano médio deveria ser rejeitado")
	}
}

func TestTurmaTemaDeTrabalhoRejeitaTextoMuitoLongo(t *testing.T) {
	longo := ""
	for i := 0; i < 201; i++ {
		longo += "a"
	}
	turma := NewTurma()
	if err := turma.Criar("GRUPO_C", "ACA_GRUPO", "4_ano_medio", nil, "manha", &longo, uuid.New()); err == nil {
		t.Fatal("tema com 201 caracteres deveria ser rejeitado")
	}
}

func TestTurmaAtualizarTemaDoGrupo(t *testing.T) {
	inicial := "Tema inicial"
	turma := NewTurma()
	if err := turma.Criar("GRUPO_D", "ACA_GRUPO", "4_ano_medio", nil, "manha", &inicial, uuid.New()); err != nil {
		t.Fatal(err)
	}
	novo := "Tema novo"
	if err := turma.AtualizarDados(nil, nil, nil, &novo, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if turma.TemaTrabalho == nil || *turma.TemaTrabalho != "Tema novo" {
		t.Fatalf("tema deveria ser atualizado, obteve %v", turma.TemaTrabalho)
	}
	// nil = não altera
	turno := "noite"
	if err := turma.AtualizarDados(nil, nil, &turno, nil, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if turma.TemaTrabalho == nil || *turma.TemaTrabalho != "Tema novo" {
		t.Fatalf("tema não pode mudar quando tema_trabalho é omitido, obteve %v", turma.TemaTrabalho)
	}
	// "" = remove
	vazio := ""
	if err := turma.AtualizarDados(nil, nil, nil, &vazio, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if turma.TemaTrabalho != nil {
		t.Fatalf("string vazia deveria remover o tema, obteve %q", *turma.TemaTrabalho)
	}
}

func TestTurmaMudarNivelParaForaDoQuartoAnoLimpaTema(t *testing.T) {
	tema := "Tema do grupo"
	turma := NewTurma()
	if err := turma.Criar("GRUPO_E", "ACA_GRUPO", "4_ano_medio", nil, "manha", &tema, uuid.New()); err != nil {
		t.Fatal(err)
	}
	nivel := "3_ano_medio"
	if err := turma.AtualizarDados(&nivel, nil, nil, nil, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if turma.TemaTrabalho != nil {
		t.Fatalf("ao sair do 4º ano médio o tema deve ser removido, obteve %q", *turma.TemaTrabalho)
	}
}

func TestTurmaTemaDeTrabalhoSobreviveAoReplayDoLedger(t *testing.T) {
	tema := "Tema persistido"
	origem := NewTurma()
	if err := origem.Criar("GRUPO_F", "ACA_GRUPO", "4_ano_medio", nil, "manha", &tema, uuid.New()); err != nil {
		t.Fatal(err)
	}
	novo := "Tema depois do replay"
	if err := origem.AtualizarDados(nil, nil, nil, &novo, uuid.New()); err != nil {
		t.Fatal(err)
	}
	reconstruida := NewTurma()
	for _, ev := range origem.GetUncommittedEvents() {
		if err := reconstruida.Apply(ev); err != nil {
			t.Fatalf("replay de %s: %v", ev.GetEventType(), err)
		}
	}
	if reconstruida.TemaTrabalho == nil || *reconstruida.TemaTrabalho != "Tema depois do replay" {
		t.Fatalf("replay deveria reconstruir o tema final, obteve %v", reconstruida.TemaTrabalho)
	}
}
