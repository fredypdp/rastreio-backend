package main

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"spuri/internal/cadastros"
	"spuri/internal/db"
	"spuri/internal/projections"
)

// Prova de ponta a ponta da regra 17: a reconstrução REAL das projeções (a
// mesma que a produção executa, com a mesma lista de projeções e a verificação
// de integridade do ledger) não apaga nenhum dado das tabelas CRUD.
//
// Isto vai além de provar que o TRUNCATE não chega às tabelas: aqui
// RebuildAllProjections e RebuildProjection("academias") rodam inteiros, sobre
// um ledger com uma academia de verdade.
func TestReconstrucaoRealPreservaTabelasCRUD(t *testing.T) {
	if os.Getenv("SPURI_RUN_DB_INTEGRITY_TESTS") != "1" {
		t.Skip("set SPURI_RUN_DB_INTEGRITY_TESTS=1 with an isolated PostgreSQL database to run")
	}
	t.Setenv("ENV", "test")
	prev, _ := os.Getwd()
	_ = os.Chdir("../..")
	t.Cleanup(func() { _ = os.Chdir(prev) })
	// Banco PRÓPRIO: uma reconstrução completa reprocessa o ledger inteiro, e o
	// banco compartilhado dos outros testes tem eventos e linhas inseridas à mão
	// que não são reproduzíveis. Num banco só deste teste, o ledger é íntegro.
	bancoIsolado(t)
	client, err := db.NewClient(db.DefaultConfig())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RunMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	ctx := context.Background()
	repo := db.NewAggregateRepository(client)

	// Uma academia de verdade, no ledger.
	academia := criarAcademiaCorrecao(t, repo, time.Now().UnixNano(), "R")
	pm := projections.NewManager(client)
	registrarProjecoes(pm, client)
	if err := pm.RebuildAllProjections(); err != nil {
		t.Fatalf("primeira reconstrução completa: %v", err)
	}
	var existe int
	if err := client.DB().Get(&existe, `SELECT count(*) FROM projection_academias WHERE codigo_academia = $1`, academia.CodigoAcademia); err != nil || existe != 1 {
		t.Fatalf("a reconstrução deveria materializar a academia do ledger: %d %v", existe, err)
	}

	// Dados CRUD dessa academia: curso, matéria, turma e sumário.
	audit := db.AuditContext{UserID: academia.ID.String(), UserType: "academia", IP: "127.0.0.1"}
	curso, err := cadastros.NovoCurso("Curso rebuild "+uuid.NewString()[:6], "superior", "",
		[]string{"1_ano_superior"}, []string{"1_semestre", "2_semestre"}, academia.CodigoAcademia)
	if err != nil {
		t.Fatal(err)
	}
	if err := cadastros.NovoCursosRepo(client).Criar(ctx, audit, curso); err != nil {
		t.Fatalf("criar curso: %v", err)
	}
	materia, err := cadastros.NovaMateria("Matéria rebuild", "superior", []string{"1_ano_superior"}, academia.CodigoAcademia, &curso.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := materia.DefinirPeriodo("1_semestre"); err != nil {
		t.Fatal(err)
	}
	if err := cadastros.NovoMateriasRepo(client).Criar(ctx, audit, materia); err != nil {
		t.Fatalf("criar matéria: %v", err)
	}
	turma, err := cadastros.NovaTurma("RB1", academia.CodigoAcademia, "1_ano_superior", &curso.ID, "manha", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := cadastros.NovoTurmasRepo(client).Criar(ctx, audit, turma); err != nil {
		t.Fatalf("criar turma: %v", err)
	}
	if _, err := cadastros.NovoTurmasRepo(client).Atualizar(ctx, audit, turma.ID, "EstudanteAdicionadoATurma", "",
		func(x *cadastros.Turma) error { return x.AdicionarEstudante("EST-RB", "2025_2026") }); err != nil {
		t.Fatalf("adicionar estudante: %v", err)
	}
	sumario, err := cadastros.NovoSumario("Aula rebuild", nil, academia.CodigoAcademia, "superior", "superior",
		"1_semestre", "1_ano_superior", &curso.ID, materia.ID, academia.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := cadastros.NovoSumariosRepo(client).Criar(ctx, audit, sumario); err != nil {
		t.Fatalf("criar sumário: %v", err)
	}

	type contagens struct {
		cursos, materias, turmas, sumarios, auditoria int
		estudantesNaTurma                             string
	}
	ler := func() contagens {
		var c contagens
		get := func(q string, arg any) int {
			var n int
			if err := client.DB().Get(&n, q, arg); err != nil {
				t.Fatalf("contar: %v", err)
			}
			return n
		}
		c.cursos = get(`SELECT count(*) FROM projection_cursos WHERE id = $1`, curso.ID)
		c.materias = get(`SELECT count(*) FROM projection_materias WHERE id = $1`, materia.ID)
		c.turmas = get(`SELECT count(*) FROM projection_turmas WHERE id = $1`, turma.ID)
		c.sumarios = get(`SELECT count(*) FROM projection_sumarios WHERE id = $1`, sumario.ID)
		c.auditoria = get(`SELECT count(*) FROM auditoria_crud WHERE academia_id = $1`, academia.ID)
		if err := client.DB().Get(&c.estudantesNaTurma, `SELECT estudantes::text FROM projection_turmas WHERE id = $1`, turma.ID); err != nil {
			t.Fatal(err)
		}
		return c
	}
	antes := ler()
	if antes.cursos != 1 || antes.materias != 1 || antes.turmas != 1 || antes.sumarios != 1 || antes.auditoria < 5 {
		t.Fatalf("estado inicial inesperado: %+v", antes)
	}

	// 1) Reconstrução COMPLETA, de ponta a ponta.
	if err := pm.RebuildAllProjections(); err != nil {
		t.Fatalf("reconstrução completa: %v", err)
	}
	if depois := ler(); depois != antes {
		t.Fatalf("a reconstrução completa alterou dados CRUD:\n antes=%+v\ndepois=%+v", antes, depois)
	}
	if err := client.DB().Get(&existe, `SELECT count(*) FROM projection_academias WHERE codigo_academia = $1`, academia.CodigoAcademia); err != nil || existe != 1 {
		t.Fatalf("a academia deveria ter sido reconstruída do ledger: %d %v", existe, err)
	}

	// 2) Reconstrução só das academias (o TRUNCATE ... CASCADE que apagaria
	// cursos e matérias antes da Tarefa 133).
	if err := pm.RebuildProjection("academias"); err != nil {
		t.Fatalf("reconstrução das academias: %v", err)
	}
	if depois := ler(); depois != antes {
		t.Fatalf("reconstruir academias alterou dados CRUD:\n antes=%+v\ndepois=%+v", antes, depois)
	}

	// 3) A guarda recusa a reconstrução se uma migration futura der a uma
	// tabela CRUD uma chave estrangeira para uma projeção reconstruível — e
	// nada é apagado.
	if _, err := client.DB().Exec(`ALTER TABLE projection_turmas
		ADD CONSTRAINT zz_teste_fk_perigosa FOREIGN KEY (codigo_academia)
		REFERENCES projection_academias(codigo_academia) ON DELETE CASCADE`); err != nil {
		t.Fatalf("criar FK de teste: %v", err)
	}
	t.Cleanup(func() {
		_, _ = client.DB().Exec(`ALTER TABLE projection_turmas DROP CONSTRAINT IF EXISTS zz_teste_fk_perigosa`)
	})
	err = pm.RebuildProjection("academias")
	if err == nil || !strings.Contains(err.Error(), "reconstrução recusada") || !strings.Contains(err.Error(), "projection_turmas") {
		t.Fatalf("a guarda deveria recusar a reconstrução, obteve: %v", err)
	}
	if err := pm.RebuildAllProjections(); err == nil {
		t.Fatal("a guarda deveria recusar também a reconstrução completa")
	}
	if depois := ler(); depois != antes {
		t.Fatalf("a reconstrução recusada não pode alterar nada:\n antes=%+v\ndepois=%+v", antes, depois)
	}

	// 4) Contraprova: sem a guarda (Rebuild() direto da projeção, que não passa
	// pelo gestor), a mesma chave estrangeira faria o TRUNCATE ... CASCADE
	// apagar a turma. Isto mostra que o teste acima detecta o perigo de
	// verdade, e que é a guarda do gestor que o impede.
	if err := projections.NewAcademiaProjection(client).Rebuild(); err != nil {
		t.Fatalf("Rebuild direto da projeção de academias: %v", err)
	}
	var restantes int
	if err := client.DB().Get(&restantes, `SELECT count(*) FROM projection_turmas WHERE id = $1`, turma.ID); err != nil {
		t.Fatal(err)
	}
	if restantes != 0 {
		t.Fatalf("contraprova: com a FK perigosa a turma deveria ter sido apagada pelo CASCADE (restantes=%d)", restantes)
	}
}

// bancoIsolado cria um banco novo no mesmo servidor de DATABASE_URL, aponta
// DATABASE_URL para ele durante o teste e o apaga no fim. Pula o teste se o
// usuário não puder criar bancos.
func bancoIsolado(t *testing.T) {
	t.Helper()
	original := os.Getenv("DATABASE_URL")
	if original == "" {
		t.Skip("DATABASE_URL não definida: o teste cria um banco próprio")
	}
	u, err := url.Parse(original)
	if err != nil {
		t.Fatalf("DATABASE_URL inválida: %v", err)
	}
	nome := "rebuild_e2e_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	admin := *u
	admin.Path = "/postgres"
	conn, err := sql.Open("postgres", admin.String())
	if err != nil {
		t.Skipf("não foi possível abrir conexão administrativa: %v", err)
	}
	if _, err := conn.Exec(`CREATE DATABASE ` + nome); err != nil {
		_ = conn.Close()
		t.Skipf("sem permissão para criar banco de teste isolado: %v", err)
	}
	novo := *u
	novo.Path = "/" + nome
	t.Setenv("DATABASE_URL", novo.String())
	t.Cleanup(func() {
		_, _ = conn.Exec(`DROP DATABASE IF EXISTS ` + nome + ` WITH (FORCE)`)
		_ = conn.Close()
	})
}
