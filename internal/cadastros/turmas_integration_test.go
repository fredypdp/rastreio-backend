package cadastros

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"spuri/internal/db"
	"spuri/internal/projections"
)

func novaTurmaNoBanco(t *testing.T, client *db.Client, a academiaTeste, codigo, nivel string, curso *uuid.UUID) *Turma {
	t.Helper()
	tr, err := NovaTurma(codigo, a.Codigo, nivel, curso, "manha", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := NovoTurmasRepo(client).Criar(context.Background(), a.Audit, tr); err != nil {
		t.Fatalf("criar turma %s: %v", codigo, err)
	}
	return tr
}

func acoesDeAuditoria(t *testing.T, client *db.Client, entidade, id string) []string {
	t.Helper()
	linhas, err := client.ListarAuditoria(context.Background(), entidade, id, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	acoes := make([]string, 0, len(linhas))
	for i := len(linhas) - 1; i >= 0; i-- { // da mais antiga para a mais recente
		acoes = append(acoes, linhas[i].Acao)
	}
	return acoes
}

// ── Turmas ──────────────────────────────────────────────────────────────────

func TestTurmaCriarGravaRegistroEAuditoria(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	tr := novaTurmaNoBanco(t, client, a, "1A", "1_ano_fundamental", nil)

	var status, codigo string
	var versao int
	if err := client.DB().QueryRow(`SELECT status, codigo_turma, version FROM projection_turmas WHERE id=$1`, tr.ID).Scan(&status, &codigo, &versao); err != nil {
		t.Fatalf("registro: %v", err)
	}
	if status != "ativo" || codigo != "1A" || versao != 1 {
		t.Fatalf("registro: %q %q v%d", status, codigo, versao)
	}
	if got := acoesDeAuditoria(t, client, EntidadeTurma, tr.ID.String()); len(got) != 1 || got[0] != "TurmaCriada" {
		t.Fatalf("auditoria = %v", got)
	}
}

func TestTurmaCriarRecusaAcademiaInexistenteECodigoRepetido(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	ctx := context.Background()

	orfa, _ := NovaTurma("X1", "NAO-EXISTE-"+uuid.NewString()[:6], "1_ano_fundamental", nil, "manha", nil)
	err := NovoTurmasRepo(client).Criar(ctx, a.Audit, orfa)
	if _, ok := EhErroRegra(err); !ok || !strings.Contains(err.Error(), "não encontrada") {
		t.Fatalf("academia inexistente deveria ser erro de regra: %v", err)
	}
	if n := contar(t, client, `SELECT count(*) FROM projection_turmas WHERE id=$1`, orfa.ID); n != 0 {
		t.Fatal("turma de academia inexistente foi gravada")
	}

	novaTurmaNoBanco(t, client, a, "REP", "1_ano_fundamental", nil)
	rep, _ := NovaTurma("rep", a.Codigo, "1_ano_fundamental", nil, "tarde", nil) // normaliza para "rep" != "REP": distinto
	if err := NovoTurmasRepo(client).Criar(ctx, a.Audit, rep); err != nil {
		t.Fatalf("códigos diferentes (caixa) são turmas diferentes: %v", err)
	}
	igual, _ := NovaTurma("REP", a.Codigo, "1_ano_fundamental", nil, "tarde", nil)
	err = NovoTurmasRepo(client).Criar(ctx, a.Audit, igual)
	if _, ok := EhErroRegra(err); !ok || !strings.Contains(err.Error(), "já existe") {
		t.Fatalf("código repetido: %v", err)
	}

	// Outra academia pode usar o mesmo código.
	b := novaAcademia(t, client)
	outra, _ := NovaTurma("REP", b.Codigo, "1_ano_fundamental", nil, "tarde", nil)
	if err := NovoTurmasRepo(client).Criar(ctx, b.Audit, outra); err != nil {
		t.Fatalf("outra academia pode repetir o código: %v", err)
	}

	// Depois de deletada, o código volta a ficar livre.
	tr := novaTurmaNoBanco(t, client, a, "LIVRE", "1_ano_fundamental", nil)
	repo := NovoTurmasRepo(client)
	if _, err := repo.Atualizar(ctx, a.Audit, tr.ID, "TurmaDesativada", "", func(x *Turma) error { return x.Desativar(a.ID) }); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Atualizar(ctx, a.Audit, tr.ID, "TurmaDeletada", "teste", func(x *Turma) error { return x.Deletar() }); err != nil {
		t.Fatal(err)
	}
	novaTurmaNoBanco(t, client, a, "LIVRE", "1_ano_fundamental", nil)
}

func TestTurmaCodigoRepetidoConcorrenteCriaSoUma(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	const n = 8
	var ok, recusadas atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr, _ := NovaTurma("CONC", a.Codigo, "1_ano_fundamental", nil, "manha", nil)
			err := NovoTurmasRepo(client).Criar(context.Background(), a.Audit, tr)
			switch {
			case err == nil:
				ok.Add(1)
			default:
				if _, regraErr := EhErroRegra(err); regraErr {
					recusadas.Add(1)
				} else {
					t.Errorf("erro inesperado: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || recusadas.Load() != n-1 {
		t.Fatalf("esperado 1 criada e %d recusadas, obteve %d e %d", n-1, ok.Load(), recusadas.Load())
	}
	if c := contar(t, client, `SELECT count(*) FROM projection_turmas WHERE codigo_academia=$1 AND codigo_turma='CONC'`, a.Codigo); c != 1 {
		t.Fatalf("existem %d turmas CONC", c)
	}
}

func TestTurmaAtualizarRecusadoNaoGravaNemAudita(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	tr := novaTurmaNoBanco(t, client, a, "ATM", "1_ano_fundamental", nil)
	repo := NovoTurmasRepo(client)

	_, err := repo.Atualizar(context.Background(), a.Audit, tr.ID, "TurmaAtivada", "", func(x *Turma) error { return x.Ativar(a.ID) })
	if _, ok := EhErroRegra(err); !ok {
		t.Fatalf("ativar turma ativa: %v", err)
	}
	if got := acoesDeAuditoria(t, client, EntidadeTurma, tr.ID.String()); len(got) != 1 {
		t.Fatalf("regra recusada não pode auditar: %v", got)
	}
	_, err = repo.Atualizar(context.Background(), a.Audit, uuid.New(), "TurmaAtivada", "", func(x *Turma) error { return nil })
	if err != ErrNaoEncontrado {
		t.Fatalf("turma inexistente: %v", err)
	}
}

func TestTurmaEstudantesConcorrentesNaoSePerdem(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	tr := novaTurmaNoBanco(t, client, a, "EST", "1_ano_fundamental", nil)
	const n = 12
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cod := fmt.Sprintf("E%02d", i)
			if _, err := NovoTurmasRepo(client).Atualizar(context.Background(), a.Audit, tr.ID, "EstudanteAdicionadoATurma", "",
				func(x *Turma) error { return x.AdicionarEstudante(cod, "2025_2026") }); err != nil {
				t.Errorf("adicionar %s: %v", cod, err)
			}
		}(i)
	}
	wg.Wait()
	var estudantes string
	var hist string
	if err := client.DB().QueryRow(`SELECT estudantes::text, historico_estudantes_ano_letivo::text FROM projection_turmas WHERE id=$1`, tr.ID).Scan(&estudantes, &hist); err != nil {
		t.Fatal(err)
	}
	var lista []string
	_ = json.Unmarshal([]byte(estudantes), &lista)
	var historico map[string][]string
	_ = json.Unmarshal([]byte(hist), &historico)
	if len(lista) != n || len(historico["2025_2026"]) != n {
		t.Fatalf("atualização perdida: %d estudantes, %d no histórico (esperado %d)", len(lista), len(historico["2025_2026"]), n)
	}
}

func TestTurmaMesmoEstudanteConcorrenteEntraUmaVez(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	tr := novaTurmaNoBanco(t, client, a, "DUP", "1_ano_fundamental", nil)
	const n = 8
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := NovoTurmasRepo(client).Atualizar(context.Background(), a.Audit, tr.ID, "EstudanteAdicionadoATurma", "",
				func(x *Turma) error { return x.AdicionarEstudante("MESMO", "") })
			if err == nil {
				ok.Add(1)
			} else if _, regraErr := EhErroRegra(err); !regraErr {
				t.Errorf("erro inesperado: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("o mesmo estudante entrou %d vezes", ok.Load())
	}
}

// ── Cascata do curso (turmas + matérias + curso, uma transação) ─────────────

func TestDeletarCursoComTurmas(t *testing.T) {
	client := clienteParaTeste(t)
	ctx := context.Background()
	cursos := NovoCursosRepo(client)
	turmas := NovoTurmasRepo(client)
	desativarCurso := func(a academiaTeste, c *Curso) {
		t.Helper()
		if _, err := cursos.Atualizar(ctx, a.Audit, c.ID, "CursoDesativado", "", func(x *Curso) error { return x.Desativar() }); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("turma ativa bloqueia e nada é apagado", func(t *testing.T) {
		a := novaAcademia(t, client)
		c := novoCursoSuperior(t, client, a)
		desativarCurso(a, c)
		tr := novaTurmaNoBanco(t, client, a, "CA", "1_ano_superior", &c.ID)
		_, _, _, err := cursos.DeletarComMaterias(ctx, a.Audit, c.ID, "x")
		if _, ok := EhErroRegra(err); !ok || !strings.Contains(err.Error(), "turma ativa: CA") {
			t.Fatalf("esperado bloqueio pela turma ativa: %v", err)
		}
		if contar(t, client, `SELECT count(*) FROM projection_cursos WHERE id=$1 AND status<>'deletado'`, c.ID) != 1 ||
			contar(t, client, `SELECT count(*) FROM projection_turmas WHERE id=$1 AND status='ativo'`, tr.ID) != 1 {
			t.Fatal("a recusa não pode alterar nada")
		}
	})

	t.Run("turma inativa com estudantes bloqueia", func(t *testing.T) {
		a := novaAcademia(t, client)
		c := novoCursoSuperior(t, client, a)
		desativarCurso(a, c)
		tr := novaTurmaNoBanco(t, client, a, "CB", "1_ano_superior", &c.ID)
		_, _ = turmas.Atualizar(ctx, a.Audit, tr.ID, "EstudanteAdicionadoATurma", "", func(x *Turma) error { return x.AdicionarEstudante("E1", "") })
		_, _ = turmas.Atualizar(ctx, a.Audit, tr.ID, "TurmaDesativada", "", func(x *Turma) error { return x.Desativar(a.ID) })
		_, _, _, err := cursos.DeletarComMaterias(ctx, a.Audit, c.ID, "x")
		if _, ok := EhErroRegra(err); !ok || !strings.Contains(err.Error(), "ainda possui estudantes") {
			t.Fatalf("esperado bloqueio pelos estudantes: %v", err)
		}
	})

	t.Run("turma inativa e vazia é deletada junto, tudo na mesma transação", func(t *testing.T) {
		a := novaAcademia(t, client)
		c := novoCursoSuperior(t, client, a)
		m := novaMateriaSuperior(t, client, a, c, "Mat cascata")
		_, _ = NovoMateriasRepo(client).Atualizar(ctx, a.Audit, m.ID, "MateriaDesativada", "", func(x *Materia) error { return x.Desativar() })
		desativarCurso(a, c)
		tr := novaTurmaNoBanco(t, client, a, "CC", "1_ano_superior", &c.ID)
		_, _ = turmas.Atualizar(ctx, a.Audit, tr.ID, "TurmaDesativada", "", func(x *Turma) error { return x.Desativar(a.ID) })
		_, nomes, codigos, err := cursos.DeletarComMaterias(ctx, a.Audit, c.ID, "encerrado")
		if err != nil {
			t.Fatalf("deletar curso: %v", err)
		}
		if len(nomes) != 1 || len(codigos) != 1 || codigos[0] != "CC" {
			t.Fatalf("matérias=%v turmas=%v", nomes, codigos)
		}
		if contar(t, client, `SELECT count(*) FROM projection_turmas WHERE id=$1 AND status='deletado' AND deleted_at IS NOT NULL`, tr.ID) != 1 {
			t.Fatal("turma não foi deletada")
		}
		got := acoesDeAuditoria(t, client, EntidadeTurma, tr.ID.String())
		if got[len(got)-1] != "TurmaDeletada" {
			t.Fatalf("auditoria da turma = %v", got)
		}
	})
}

// ── Sumários ────────────────────────────────────────────────────────────────

func TestSumarioCicloCompletoComAuditoria(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	c := novoCursoSuperior(t, client, a)
	m := novaMateriaSuperior(t, client, a, c, "Mat sumário")
	ctx := context.Background()
	repo := NovoSumariosRepo(client)

	s, err := NovoSumario("Aula inaugural", nil, a.Codigo, "superior", "superior", "1_semestre", "1_ano_superior", &c.ID, m.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Criar(ctx, a.Audit, s); err != nil {
		t.Fatalf("criar: %v", err)
	}
	novo := "Aula 1"
	got, err := repo.Atualizar(ctx, a.Audit, s.ID, "SumarioDadosAtualizados", func(x *Sumario) error { return x.AtualizarDados(&novo, nil) })
	if err != nil || got.SumarioTitulo != "Aula 1" || got.Version != 2 {
		t.Fatalf("atualizar: %v %+v", err, got)
	}
	if _, err := repo.Atualizar(ctx, a.Audit, s.ID, "SumarioDeletado", func(x *Sumario) error { return x.Deletar() }); err != nil {
		t.Fatalf("deletar: %v", err)
	}
	if _, err := repo.Atualizar(ctx, a.Audit, s.ID, "SumarioDadosAtualizados", func(x *Sumario) error { return x.AtualizarDados(&novo, nil) }); err == nil {
		t.Fatal("atualizar sumário deletado deveria falhar")
	}
	want := []string{"SumarioCriado", "SumarioDadosAtualizados", "SumarioDeletado"}
	if got := acoesDeAuditoria(t, client, EntidadeSumario, s.ID.String()); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("auditoria = %v, esperado %v", got, want)
	}
	var status string
	if err := client.DB().Get(&status, `SELECT status FROM projection_sumarios WHERE id=$1`, s.ID); err != nil || status != "deletado" {
		t.Fatalf("status = %q %v", status, err)
	}

	orfao, _ := NovoSumario("Aula órfã", nil, "NAO-EXISTE-"+uuid.NewString()[:6], "superior", "superior", "1_semestre", "1_ano_superior", &c.ID, m.ID, a.ID)
	if _, ok := EhErroRegra(repo.Criar(ctx, a.Audit, orfao)); !ok {
		t.Fatal("sumário de academia inexistente deveria ser recusado")
	}
}

// ── Efeito da avaliação final sobre as turmas ───────────────────────────────

func eventoAvaliacaoFinal(t *testing.T, a academiaTeste, estudante, anoLetivo, proximo string, removidas []string) db.Event {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"codigo_estudante": estudante, "codigo_academia": a.Codigo, "ano_lectivo": anoLetivo,
		"proximo_ano_academico": proximo, "codigos_turmas_removidas": removidas, "aprovado": true,
	})
	md, _ := json.Marshal(map[string]any{"user_id": a.ID.String(), "user_type": "academia", "ip": "127.0.0.1"})
	return db.Event{EventID: uuid.New(), AggregateID: uuid.New(), AggregateType: "Estudante",
		EventType: "AvaliacaoFinalEscolar", EventVersion: 1, Payload: payload, Metadata: md}
}

func estudantesDaTurma(t *testing.T, client *db.Client, id uuid.UUID) []string {
	t.Helper()
	var s string
	if err := client.DB().Get(&s, `SELECT estudantes::text FROM projection_turmas WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	var l []string
	_ = json.Unmarshal([]byte(s), &l)
	return l
}

func TestAvaliacaoFinalMoveEstudanteEAudita(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	b := novaAcademia(t, client) // outra academia com turma de MESMO código: não pode ser tocada
	ctx := context.Background()
	repo := NovoTurmasRepo(client)

	origem := novaTurmaNoBanco(t, client, a, "7A", "7_ano_fundamental", nil)
	destino := novaTurmaNoBanco(t, client, a, "8A", "8_ano_fundamental", nil)
	alheia := novaTurmaNoBanco(t, client, b, "7A", "7_ano_fundamental", nil)
	for _, tr := range []*Turma{origem, alheia} {
		acad := a
		if tr == alheia {
			acad = b
		}
		if _, err := repo.Atualizar(ctx, acad.Audit, tr.ID, "EstudanteAdicionadoATurma", "", func(x *Turma) error { return x.AdicionarEstudante("EST-AV", "") }); err != nil {
			t.Fatal(err)
		}
	}

	proj := projections.NewTurmasProjection(client)
	if err := proj.Handle(eventoAvaliacaoFinal(t, a, "EST-AV", "2025_2026", "8_ano_fundamental", []string{"7A"})); err != nil {
		t.Fatalf("efeito da avaliação final: %v", err)
	}

	if l := estudantesDaTurma(t, client, origem.ID); len(l) != 0 {
		t.Fatalf("o estudante deveria ter saído da turma de origem: %v", l)
	}
	if l := estudantesDaTurma(t, client, destino.ID); len(l) != 1 || l[0] != "EST-AV" {
		t.Fatalf("o estudante deveria ter entrado na turma do ano seguinte: %v", l)
	}
	if l := estudantesDaTurma(t, client, alheia.ID); len(l) != 1 {
		t.Fatalf("turma de OUTRA academia com o mesmo código não pode ser alterada: %v", l)
	}
	var hist string
	if err := client.DB().Get(&hist, `SELECT historico_estudantes_ano_letivo::text FROM projection_turmas WHERE id=$1`, origem.ID); err != nil || !strings.Contains(hist, "EST-AV") {
		t.Fatalf("histórico por ano letivo deveria registrar o estudante: %s %v", hist, err)
	}
	if got := acoesDeAuditoria(t, client, EntidadeTurma, origem.ID.String()); got[len(got)-1] != "EstudanteRemovidoDaTurmaPorAvaliacaoFinal" {
		t.Fatalf("auditoria da origem = %v", got)
	}
	if got := acoesDeAuditoria(t, client, EntidadeTurma, destino.ID.String()); got[len(got)-1] != "EstudanteAdicionadoATurmaPorAvaliacaoFinal" {
		t.Fatalf("auditoria do destino = %v", got)
	}

	// Sem turma de destino compatível, nada fica pela metade: o efeito falha
	// inteiro (a remoção da origem é desfeita) e o evento pode ser repetido.
	solto := novaTurmaNoBanco(t, client, a, "9A", "9_ano_fundamental", nil)
	_, _ = repo.Atualizar(ctx, a.Audit, solto.ID, "EstudanteAdicionadoATurma", "", func(x *Turma) error { return x.AdicionarEstudante("EST-SEM-DESTINO", "") })
	err := proj.Handle(eventoAvaliacaoFinal(t, a, "EST-SEM-DESTINO", "2025_2026", "10_ano_fundamental", []string{"9A"}))
	if err == nil || !strings.Contains(err.Error(), "nenhuma turma destino") {
		t.Fatalf("sem destino deveria falhar: %v", err)
	}
	if l := estudantesDaTurma(t, client, solto.ID); len(l) != 1 {
		t.Fatalf("a remoção da origem deveria ter sido desfeita junto: %v", l)
	}
}

func TestReconstruirTurmasNaoApagaNemRepeteEfeitos(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	tr := novaTurmaNoBanco(t, client, a, "RB", "1_ano_fundamental", nil)
	if err := projections.NewTurmasProjection(client).Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if contar(t, client, `SELECT count(*) FROM projection_turmas WHERE id=$1`, tr.ID) != 1 {
		t.Fatal("Rebuild de turmas apagou uma turma CRUD")
	}
}

// Quando o secretário age como a academia, a auditoria CRUD grava quem agiu
// (defeito da Tarefa 133, corrigido na 134: os repositórios ignoravam o ator).
func TestAuditoriaCRUDGravaOAtorEmNomeDe(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	secretario := uuid.NewString()
	audit := a.Audit
	audit.Ator = &db.AtorAuditoria{ID: secretario, Tipo: "secretario"}

	tr, _ := NovaTurma("ATOR", a.Codigo, "1_ano_fundamental", nil, "manha", nil)
	if err := NovoTurmasRepo(client).Criar(context.Background(), audit, tr); err != nil {
		t.Fatal(err)
	}
	c := novoCursoSuperior(t, client, a)
	if _, err := NovoCursosRepo(client).Atualizar(context.Background(), audit, c.ID, "CursoDesativado", "", func(x *Curso) error { return x.Desativar() }); err != nil {
		t.Fatal(err)
	}
	for _, e := range []struct{ tipo, id string }{{EntidadeTurma, tr.ID.String()}, {EntidadeCurso, c.ID.String()}} {
		linhas, err := client.ListarAuditoria(context.Background(), e.tipo, e.id, 10, 0)
		if err != nil || len(linhas) == 0 {
			t.Fatalf("%s sem auditoria: %v", e.tipo, err)
		}
		ultima := linhas[0]
		if ultima.EmNomeDeID != secretario || ultima.EmNomeDeTipo != "secretario" || ultima.AtorID != a.ID.String() {
			t.Fatalf("%s: o secretário deveria constar como quem agiu em nome da academia: %+v", e.tipo, ultima)
		}
	}
}
