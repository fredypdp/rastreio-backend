package cadastros

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func novaTurmaHist(t *testing.T) *Turma {
	t.Helper()
	turma, err := NovaTurma("TURMA_HIST", "ACA_HIST", "6_ano_fundamental", nil, "manha", nil)
	if err != nil {
		t.Fatal(err)
	}
	return turma
}

func TestTurmaNovaNormalizaCodigoEIniciaAtiva(t *testing.T) {
	turma, err := NovaTurma("  turma  a ", "ACA", "7_ano_fundamental", nil, "tarde", nil)
	if err != nil {
		t.Fatal(err)
	}
	if turma.CodigoTurma != "turma_a" || turma.Status != StatusAtivo || len(turma.Estudantes) != 0 {
		t.Fatalf("turma nova inesperada: %+v", turma)
	}
	for _, c := range []struct{ cod, aca, nivel, turno string }{
		{"", "ACA", "7", "manha"}, {"T", "", "7", "manha"}, {"T", "ACA", "", "manha"}, {"T", "ACA", "7", "madrugada"}, {"T@", "ACA", "7", "manha"},
	} {
		if _, err := NovaTurma(c.cod, c.aca, c.nivel, nil, c.turno, nil); err == nil {
			t.Errorf("NovaTurma(%+v) deveria falhar", c)
		} else if _, ok := EhErroRegra(err); !ok {
			t.Errorf("erro de regra esperado para %+v, obteve %T", c, err)
		}
	}
}

func TestTurmaStatusAtivarDesativar(t *testing.T) {
	turma := novaTurmaHist(t)
	por := uuid.New()
	if err := turma.Ativar(por); err == nil {
		t.Fatal("ativar turma já ativa deveria falhar")
	}
	if err := turma.Desativar(por); err != nil {
		t.Fatal(err)
	}
	if turma.Status != StatusInativo || turma.StatusAlteradoPor == nil || *turma.StatusAlteradoPor != por || turma.StatusAlteradoEm == nil {
		t.Fatalf("auditoria de status não registrada: %+v", turma)
	}
	if err := turma.Desativar(por); err == nil {
		t.Fatal("desativar turma já inativa deveria falhar")
	}
	if err := turma.Ativar(por); err != nil || turma.Status != StatusAtivo {
		t.Fatalf("ativar: %v %s", err, turma.Status)
	}
}

func TestTurmaEstudantes(t *testing.T) {
	turma := novaTurmaHist(t)
	if err := turma.AdicionarEstudante("", ""); err == nil {
		t.Fatal("codigo vazio deveria falhar")
	}
	if err := turma.AdicionarEstudante("E1", "2025_2026"); err != nil {
		t.Fatal(err)
	}
	if err := turma.AdicionarEstudante("E1", "2025_2026"); err == nil || !strings.Contains(err.Error(), "já pertence") {
		t.Fatalf("duplicado: %v", err)
	}
	if got := turma.HistoricoEstudantesAnoLetivo["2025_2026"]; len(got) != 1 || got[0] != "E1" {
		t.Fatalf("histórico = %v", got)
	}
	if err := turma.RemoverEstudante("E2"); err == nil || !strings.Contains(err.Error(), "não pertence") {
		t.Fatalf("remover quem não está: %v", err)
	}
	if err := turma.RemoverEstudante("E1"); err != nil || len(turma.Estudantes) != 0 {
		t.Fatalf("remover: %v %v", err, turma.Estudantes)
	}
	// o histórico por ano letivo não é apagado ao remover
	if got := turma.HistoricoEstudantesAnoLetivo["2025_2026"]; len(got) != 1 {
		t.Fatalf("histórico deveria permanecer, obteve %v", got)
	}
}

func TestTurmaAtualizarDadosPermiteIdentidadeSemHistorico(t *testing.T) {
	turma := novaTurmaHist(t)
	nivel := "7_ano_fundamental"
	if err := turma.AtualizarDados(&nivel, nil, nil, nil); err != nil {
		t.Fatalf("turma sem histórico deve aceitar correção de nível: %v", err)
	}
}

func TestTurmaAtualizarDadosBloqueiaIdentidadeComHistorico(t *testing.T) {
	turma := novaTurmaHist(t)
	if err := turma.AdicionarEstudante("EST-HIST", "2025_2026"); err != nil {
		t.Fatal(err)
	}
	nivel := "7_ano_fundamental"
	err := turma.AtualizarDados(&nivel, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "histórico") {
		t.Fatalf("alteração de nível com histórico = %v, queria erro claro", err)
	}
	if turma.Nivel != "6_ano_fundamental" {
		t.Fatalf("nível não pode mudar quando a regra recusa: %s", turma.Nivel)
	}
	cursoID := uuid.New()
	err = turma.AtualizarDados(nil, &cursoID, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "histórico") {
		t.Fatalf("alteração de curso com histórico = %v, queria erro claro", err)
	}
	// repetir o mesmo nível/curso não é alteração
	igual := "6_ano_fundamental"
	if err := turma.AtualizarDados(&igual, nil, nil, nil); err != nil {
		t.Fatalf("mesmo nível não é alteração: %v", err)
	}
	turno := "tarde"
	if err := turma.AtualizarDados(nil, nil, &turno, nil); err != nil {
		t.Fatalf("turno com histórico deve continuar editável: %v", err)
	}
}

func TestTurmaAtualizarDadosRecusaNaoAlteraNada(t *testing.T) {
	turma := novaTurmaHist(t)
	nivel := "7_ano_fundamental"
	turno := "madrugada"
	if err := turma.AtualizarDados(&nivel, nil, &turno, nil); err == nil {
		t.Fatal("turno inválido deveria falhar")
	}
	if turma.Nivel != "6_ano_fundamental" || turma.Turno != "manha" {
		t.Fatalf("recusa não pode deixar alteração parcial: %+v", turma)
	}
}

func TestTurmaGrupoDoQuartoAnoMedioAceitaTemaDeTrabalho(t *testing.T) {
	tema := "  Sistema de gestão escolar  "
	turma, err := NovaTurma("GRUPO_A", "ACA_GRUPO", "4_ano_medio", nil, "manha", &tema)
	if err != nil {
		t.Fatalf("criar grupo com tema: %v", err)
	}
	if turma.TemaTrabalho == nil || *turma.TemaTrabalho != "Sistema de gestão escolar" {
		t.Fatalf("tema esperado normalizado, obteve %v", turma.TemaTrabalho)
	}
	vazio := "   "
	turma, err = NovaTurma("GRUPO_B", "ACA_GRUPO", "4_ano_medio", nil, "tarde", &vazio)
	if err != nil || turma.TemaTrabalho != nil {
		t.Fatalf("tema em branco deveria virar nil: %v %v", err, turma)
	}
}

func TestTurmaTemaDeTrabalhoRejeitadoForaDoQuartoAnoMedio(t *testing.T) {
	tema := "Tema qualquer"
	if _, err := NovaTurma("TURMA_X", "ACA_GRUPO", "3_ano_medio", nil, "manha", &tema); err == nil {
		t.Fatal("tema_trabalho em turma do 3º ano médio deveria ser rejeitado")
	}
	turma, err := NovaTurma("TURMA_Y", "ACA_GRUPO", "3_ano_medio", nil, "manha", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := turma.AtualizarDados(nil, nil, nil, &tema); err == nil {
		t.Fatal("atualizar tema_trabalho em turma do 3º ano médio deveria ser rejeitado")
	}
}

func TestTurmaTemaDeTrabalhoRejeitaTextoMuitoLongo(t *testing.T) {
	longo := strings.Repeat("a", 201)
	if _, err := NovaTurma("GRUPO_C", "ACA_GRUPO", "4_ano_medio", nil, "manha", &longo); err == nil {
		t.Fatal("tema com 201 caracteres deveria ser rejeitado")
	}
}

func TestTurmaAtualizarTemaDoGrupo(t *testing.T) {
	inicial := "Tema inicial"
	turma, err := NovaTurma("GRUPO_D", "ACA_GRUPO", "4_ano_medio", nil, "manha", &inicial)
	if err != nil {
		t.Fatal(err)
	}
	novo := "Tema novo"
	if err := turma.AtualizarDados(nil, nil, nil, &novo); err != nil || *turma.TemaTrabalho != "Tema novo" {
		t.Fatalf("tema deveria ser atualizado: %v %v", err, turma.TemaTrabalho)
	}
	turno := "noite"
	if err := turma.AtualizarDados(nil, nil, &turno, nil); err != nil || turma.TemaTrabalho == nil || *turma.TemaTrabalho != "Tema novo" {
		t.Fatalf("tema não pode mudar quando omitido: %v %v", err, turma.TemaTrabalho)
	}
	vazio := ""
	if err := turma.AtualizarDados(nil, nil, nil, &vazio); err != nil || turma.TemaTrabalho != nil {
		t.Fatalf("string vazia deveria remover o tema: %v %v", err, turma.TemaTrabalho)
	}
}

func TestTurmaMudarNivelParaForaDoQuartoAnoLimpaTema(t *testing.T) {
	tema := "Tema do grupo"
	turma, err := NovaTurma("GRUPO_E", "ACA_GRUPO", "4_ano_medio", nil, "manha", &tema)
	if err != nil {
		t.Fatal(err)
	}
	nivel := "3_ano_medio"
	if err := turma.AtualizarDados(&nivel, nil, nil, nil); err != nil || turma.TemaTrabalho != nil {
		t.Fatalf("ao sair do 4º ano médio o tema deve ser removido: %v %v", err, turma.TemaTrabalho)
	}
}

func TestTurmaDeletarExigeInativaESemEstudantes(t *testing.T) {
	turma := novaTurmaHist(t)
	if err := turma.Deletar(); err == nil || !strings.Contains(err.Error(), "desative") {
		t.Fatalf("turma ativa: %v", err)
	}
	_ = turma.AdicionarEstudante("E1", "")
	_ = turma.Desativar(uuid.New())
	if err := turma.Deletar(); err == nil || !strings.Contains(err.Error(), "remova todos os estudantes") {
		t.Fatalf("turma com estudantes: %v", err)
	}
	_ = turma.RemoverEstudante("E1")
	if err := turma.Deletar(); err != nil || turma.Status != StatusDeletado || turma.DeletedAt == nil {
		t.Fatalf("deletar: %v %+v", err, turma)
	}
	if err := turma.Deletar(); err == nil {
		t.Fatal("deletar duas vezes deveria falhar")
	}
	if err := turma.Ativar(uuid.New()); err == nil {
		t.Fatal("turma deletada não pode ser ativada")
	}
}
