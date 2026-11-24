package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"spuri/internal/middleware"
)

// Tarefa 133 (Parte 2B): cursos e matérias são CRUD. Estes testes percorrem a
// API de ponta a ponta — criar, consultar, atualizar, ativar/desativar,
// deletar em cascata — e conferem o registro e a auditoria no banco.

// academiaSuperiorParaCursos insere uma academia de nível superior direto na
// projeção de academias (a academia continua sendo event sourcing; aqui só
// precisa existir) e devolve o id e o token dela.
func academiaSuperiorParaCursos(t *testing.T, fx *turmaVinculoFixture, sufixo string) (uuid.UUID, string, string) {
	t.Helper()
	id := uuid.New()
	codigo := fmt.Sprintf("C%s%05d", sufixo, time.Now().UnixNano()%100_000)
	nif := fmt.Sprintf("%010d", (time.Now().UnixNano()+int64(sufixo[0]))%10_000_000_000)
	_, err := fx.client.DB().Exec(`
		INSERT INTO projection_academias
			(id, nivel, nome, codigo_academia, senha_hash, provincia, endereco, version, created_at, type, nif, status)
		VALUES ($1, 'superior', $2, $3, 'hash-de-teste', 'LDA', 'Rua de teste', 1, CURRENT_TIMESTAMP, 'private', $4, 'ativo')`,
		id, "Universidade "+codigo, codigo, nif)
	if err != nil {
		t.Fatalf("inserir academia superior: %v", err)
	}
	token, err := middleware.GenerateToken(id, "academia")
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	return id, codigo, token
}

func idDaResposta(t *testing.T, resp map[string]any) string {
	t.Helper()
	d, _ := resp["data"].(map[string]any)
	id, _ := d["id"].(string)
	if id == "" {
		t.Fatalf("resposta sem data.id: %v", resp)
	}
	return id
}

func acoesDeAuditoria(t *testing.T, fx *turmaVinculoFixture, tipo, id string) []string {
	t.Helper()
	var acoes []string
	if err := fx.client.DB().Select(&acoes,
		`SELECT acao FROM auditoria_crud WHERE entidade_tipo=$1 AND entidade_id=$2 ORDER BY sequencia`, tipo, id); err != nil {
		t.Fatalf("auditoria: %v", err)
	}
	return acoes
}

func TestCursosMaterias133FluxoCompletoPorHTTP(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	_, _, token := academiaSuperiorParaCursos(t, fx, "A")

	// ── Criar curso (superior: 4 semestres → 2 anos) ──
	code, resp := statusDe(t, fx, http.MethodPost, "/academia/curso", token, map[string]any{"nome": "Engenharia Informática", "periodos": 4})
	if code != http.StatusCreated {
		t.Fatalf("criar curso: %d %v", code, resp)
	}
	cursoID := idDaResposta(t, resp)
	d := resp["data"].(map[string]any)
	if d["type"] != "superior" || len(d["periodos"].([]any)) != 4 || len(d["anos_academicos"].([]any)) != 2 {
		t.Fatalf("curso criado inesperado: %v", d)
	}

	// Leitura imediata (sem esperar projeção).
	code, resp = statusDe(t, fx, http.MethodGet, "/academia/curso/"+cursoID, token, nil)
	if code != http.StatusOK {
		t.Fatalf("ler curso: %d %v", code, resp)
	}

	// ── Atualizar dados ──
	code, resp = statusDe(t, fx, http.MethodPut, "/academia/curso/"+cursoID+"/dados", token, map[string]any{"nome": "Engenharia Informática e de Sistemas"})
	if code != http.StatusOK || resp["nome"] != "Engenharia Informática e de Sistemas" {
		t.Fatalf("atualizar curso: %d %v", code, resp)
	}
	_, resp = statusDe(t, fx, http.MethodGet, "/academia/curso/"+cursoID, token, nil)
	if v, _ := resp["version"].(float64); v != 2 {
		t.Fatalf("version = %v, want 2 depois de uma atualização (%v)", resp["version"], resp)
	}

	// ── Regras: curso já ativo; deletar curso ativo ──
	if code, resp = statusDe(t, fx, http.MethodPut, "/academia/curso/"+cursoID+"/ativar", token, nil); code != http.StatusUnprocessableEntity && code != http.StatusBadRequest {
		t.Fatalf("ativar curso ativo deveria ser recusado: %d %v", code, resp)
	}
	code, resp = statusDe(t, fx, http.MethodDelete, "/academia/curso/"+cursoID, token, map[string]any{"motivo": "x"})
	if code < 400 || code >= 500 {
		t.Fatalf("deletar curso ativo deveria ser recusado: %d %v", code, resp)
	}

	// ── Matéria superior (período obrigatório e do curso) ──
	code, resp = statusDe(t, fx, http.MethodPost, "/academia/materia", token, map[string]any{
		"nome": "Cálculo I", "curso_id": cursoID, "anos_academicos": []string{"1_ano_superior"}, "periodo": "1_semestre",
	})
	if code != http.StatusCreated {
		t.Fatalf("criar matéria: %d %v", code, resp)
	}
	materiaID := idDaResposta(t, resp)
	if m := resp["data"].(map[string]any); m["status"] != "ativo" || m["periodo"] != "1_semestre" {
		t.Fatalf("matéria criada: %v", m)
	}
	// Período fora do curso é recusado e nada é gravado.
	code, _ = statusDe(t, fx, http.MethodPost, "/academia/materia", token, map[string]any{
		"nome": "Fora do curso", "curso_id": cursoID, "anos_academicos": []string{"1_ano_superior"}, "periodo": "9_semestre",
	})
	if code < 400 || code >= 500 {
		t.Fatalf("período fora do curso deveria ser recusado: %d", code)
	}

	// ── Atualizar matéria ──
	code, resp = statusDe(t, fx, http.MethodPut, "/academia/materia/"+materiaID+"/dados", token, map[string]any{"nome": "Cálculo I (revisto)"})
	if code != http.StatusOK {
		t.Fatalf("atualizar matéria: %d %v", code, resp)
	}

	// ── Deletar curso com matéria ativa → recusado, tudo intacto ──
	statusDe(t, fx, http.MethodPut, "/academia/curso/"+cursoID+"/desativar", token, nil)
	code, resp = statusDe(t, fx, http.MethodDelete, "/academia/curso/"+cursoID, token, map[string]any{"motivo": "encerrado"})
	if code < 400 || code >= 500 {
		t.Fatalf("deletar curso com matéria ativa deveria ser recusado: %d %v", code, resp)
	}
	var sm string
	_ = fx.client.DB().Get(&sm, `SELECT status FROM projection_materias WHERE id=$1`, materiaID)
	if sm != "ativo" {
		t.Fatalf("matéria mudou (%s) apesar da recusa", sm)
	}

	// ── Desativar a matéria e deletar o curso em cascata ──
	if code, resp = statusDe(t, fx, http.MethodPut, "/academia/materia/"+materiaID+"/desativar", token, nil); code != http.StatusOK {
		t.Fatalf("desativar matéria: %d %v", code, resp)
	}
	code, resp = statusDe(t, fx, http.MethodDelete, "/academia/curso/"+cursoID, token, map[string]any{"motivo": "encerrado"})
	if code != http.StatusOK {
		t.Fatalf("deletar curso: %d %v", code, resp)
	}
	nomes, _ := resp["materias_deletadas"].([]any)
	if len(nomes) != 1 || !strings.HasPrefix(fmt.Sprint(nomes[0]), "Cálculo I") {
		t.Fatalf("materias_deletadas = %v", resp["materias_deletadas"])
	}
	var sc, sm2 string
	_ = fx.client.DB().Get(&sc, `SELECT status FROM projection_cursos WHERE id=$1`, cursoID)
	_ = fx.client.DB().Get(&sm2, `SELECT status FROM projection_materias WHERE id=$1`, materiaID)
	if sc != "deletado" || sm2 != "deletado" {
		t.Fatalf("status final: curso=%s matéria=%s", sc, sm2)
	}

	// ── Auditoria completa, na ordem, sem passar pelo ledger ──
	want := []string{"CursoCriado", "CursoDadosAtualizados", "CursoDesativado", "CursoDeletado"}
	if got := acoesDeAuditoria(t, fx, "Curso", cursoID); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("auditoria do curso = %v, want %v", got, want)
	}
	wantM := []string{"MateriaCriada", "MateriaDadosAtualizados", "MateriaDesativada", "MateriaDeletada"}
	if got := acoesDeAuditoria(t, fx, "MateriaDisciplinar", materiaID); strings.Join(got, ",") != strings.Join(wantM, ",") {
		t.Fatalf("auditoria da matéria = %v, want %v", got, wantM)
	}
	var noLedger int
	_ = fx.client.DB().Get(&noLedger, `SELECT count(*) FROM spuri_ledger WHERE aggregate_id IN ($1::uuid, $2::uuid)`, cursoID, materiaID)
	if noLedger != 0 {
		t.Fatalf("curso/matéria deixaram %d evento(s) no ledger: devem ser só CRUD", noLedger)
	}
	for _, e := range [][2]string{{"Curso", cursoID}, {"MateriaDisciplinar", materiaID}} {
		var ok bool
		_ = fx.client.DB().Get(&ok, `SELECT is_valid FROM verificar_cadeia_auditoria_crud($1, $2)`, e[0], e[1])
		if !ok {
			t.Fatalf("cadeia de auditoria quebrada para %s %s", e[0], e[1])
		}
	}
}

func TestCursosMaterias133OutraAcademiaNaoMexe(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	_, _, tokenA := academiaSuperiorParaCursos(t, fx, "B")
	_, _, tokenB := academiaSuperiorParaCursos(t, fx, "C")

	code, resp := statusDe(t, fx, http.MethodPost, "/academia/curso", tokenA, map[string]any{"nome": "Direito", "periodos": 2})
	if code != http.StatusCreated {
		t.Fatalf("criar curso: %d %v", code, resp)
	}
	cursoID := idDaResposta(t, resp)

	for _, c := range []struct{ metodo, caminho string }{
		{http.MethodPut, "/academia/curso/" + cursoID + "/desativar"},
		{http.MethodPut, "/academia/curso/" + cursoID + "/dados"},
		{http.MethodDelete, "/academia/curso/" + cursoID},
	} {
		code, _ := statusDe(t, fx, c.metodo, c.caminho, tokenB, map[string]any{"nome": "invadido"})
		if code != http.StatusForbidden {
			t.Errorf("%s %s por outra academia: %d, want 403", c.metodo, c.caminho, code)
		}
	}
	var nome, status string
	_ = fx.client.DB().QueryRow(`SELECT nome, status FROM projection_cursos WHERE id=$1`, cursoID).Scan(&nome, &status)
	if nome != "Direito" || status != "ativo" {
		t.Fatalf("curso alterado por outra academia: %q %q", nome, status)
	}
	// Matéria de um curso de outra academia também é recusada.
	code, _ = statusDe(t, fx, http.MethodPost, "/academia/materia", tokenB, map[string]any{
		"nome": "Intrusa", "curso_id": cursoID, "anos_academicos": []string{"1_ano_superior"}, "periodo": "1_semestre",
	})
	if code != http.StatusForbidden && code != http.StatusNotFound {
		t.Fatalf("matéria em curso alheio: %d, want 403/404", code)
	}
}
