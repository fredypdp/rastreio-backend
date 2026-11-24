package cadastros

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"spuri/internal/db"
)

// Testes de integração: rodam com RUN_POSTGRES_INTEGRATION=1 e um PostgreSQL
// de teste (DATABASE_URL), sempre com as migrations aplicadas.

func clienteParaTeste(t *testing.T) *db.Client {
	t.Helper()
	if os.Getenv("RUN_POSTGRES_INTEGRATION") != "1" {
		t.Skip("teste de integração requer RUN_POSTGRES_INTEGRATION=1 e PostgreSQL")
	}
	prev, _ := os.Getwd()
	_ = os.Chdir("../..")
	t.Cleanup(func() { _ = os.Chdir(prev) })

	client, err := db.NewClient(db.DefaultConfig())
	if err != nil {
		t.Fatalf("conectar: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RunMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	return client
}

var nifSeq atomic.Int64

type academiaTeste struct {
	ID     uuid.UUID
	Codigo string
	Audit  db.AuditContext
}

// novaAcademia insere uma academia mínima em projection_academias (a academia
// continua sendo event sourcing; aqui só precisa existir).
func novaAcademia(t *testing.T, client *db.Client) academiaTeste {
	t.Helper()
	id := uuid.New()
	codigo := "T" + strings.ToUpper(strings.ReplaceAll(id.String(), "-", ""))[:12]
	nif := fmt.Sprintf("%010d", (time.Now().UnixNano()+nifSeq.Add(1))%10_000_000_000)
	_, err := client.DB().Exec(`
		INSERT INTO projection_academias
			(id, nivel, nome, codigo_academia, senha_hash, provincia, endereco, version, created_at, type, nif, status)
		VALUES ($1, 'superior', $2, $3, 'hash-de-teste', 'LDA', 'Rua de teste', 1, CURRENT_TIMESTAMP, 'private', $4, 'ativo')`,
		id, "Academia "+codigo, codigo, nif)
	if err != nil {
		t.Fatalf("inserir academia de teste: %v", err)
	}
	return academiaTeste{ID: id, Codigo: codigo,
		Audit: db.AuditContext{UserID: id.String(), UserType: "academia", IP: "127.0.0.1"}}
}

func novoCursoSuperior(t *testing.T, client *db.Client, a academiaTeste) *Curso {
	t.Helper()
	c, err := NovoCurso("Engenharia "+uuid.NewString()[:6], "superior", "",
		[]string{"1_ano_superior"}, []string{"1_semestre", "2_semestre"}, a.Codigo)
	if err != nil {
		t.Fatal(err)
	}
	if err := NovoCursosRepo(client).Criar(context.Background(), a.Audit, c); err != nil {
		t.Fatalf("criar curso: %v", err)
	}
	return c
}

func novaMateriaSuperior(t *testing.T, client *db.Client, a academiaTeste, curso *Curso, nome string) *Materia {
	t.Helper()
	m, err := NovaMateria(nome, "superior", []string{"1_ano_superior"}, a.Codigo, &curso.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.DefinirPeriodo("1_semestre"); err != nil {
		t.Fatal(err)
	}
	if err := NovoMateriasRepo(client).Criar(context.Background(), a.Audit, m); err != nil {
		t.Fatalf("criar matéria: %v", err)
	}
	return m
}

func contar(t *testing.T, client *db.Client, q string, args ...any) int {
	t.Helper()
	var n int
	if err := client.DB().Get(&n, q, args...); err != nil {
		t.Fatalf("contar: %v", err)
	}
	return n
}

func TestCursoCriarGravaRegistroEAuditoria(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	c := novoCursoSuperior(t, client, a)

	var nome, status string
	var versao int
	if err := client.DB().QueryRow(`SELECT nome, status, version FROM projection_cursos WHERE id=$1`, c.ID).Scan(&nome, &status, &versao); err != nil {
		t.Fatalf("registro do curso: %v", err)
	}
	if nome != c.Nome || status != "ativo" || versao != 1 {
		t.Fatalf("registro: nome=%q status=%q versao=%d", nome, status, versao)
	}
	linhas, err := client.ListarAuditoria(context.Background(), EntidadeCurso, c.ID.String(), 10, 0)
	if err != nil || len(linhas) != 1 {
		t.Fatalf("auditoria: %d linhas, err %v", len(linhas), err)
	}
	l := linhas[0]
	if l.Acao != "CursoCriado" || l.AtorID != a.ID.String() || l.AtorTipo != "academia" || l.Sequencia != 1 {
		t.Fatalf("linha inesperada: %+v", l)
	}
	if !l.AcademiaID.Valid || l.AcademiaID.String != a.ID.String() {
		t.Fatalf("academia_id = %+v", l.AcademiaID)
	}
	if !strings.Contains(string(l.Depois), c.Nome) {
		t.Fatalf("depois não traz o curso: %s", l.Depois)
	}
}

func TestCursoCriarExigeAcademiaExistente(t *testing.T) {
	client := clienteParaTeste(t)
	c, err := NovoCurso("Sem academia", "superior", "", []string{"1_ano_superior"}, []string{"1_semestre", "2_semestre"}, "INEXISTENTE_"+uuid.NewString()[:6])
	if err != nil {
		t.Fatal(err)
	}
	err = NovoCursosRepo(client).Criar(context.Background(), db.AuditContext{UserID: uuid.NewString(), UserType: "academia"}, c)
	if _, ok := EhErroRegra(err); !ok {
		t.Fatalf("err = %v, want violação de regra (academia inexistente)", err)
	}
	if n := contar(t, client, `SELECT count(*) FROM projection_cursos WHERE id=$1`, c.ID); n != 0 {
		t.Fatalf("curso gravado sem academia: %d", n)
	}
	if n := contar(t, client, `SELECT count(*) FROM auditoria_crud WHERE entidade_id=$1`, c.ID.String()); n != 0 {
		t.Fatalf("auditoria gravada sem curso: %d", n)
	}
}

func TestCursoAtualizarVersaoAuditoriaAntesDepois(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	c := novoCursoSuperior(t, client, a)
	repo := NovoCursosRepo(client)
	ctx := context.Background()

	novo := "Nome Novo"
	atualizado, err := repo.Atualizar(ctx, a.Audit, c.ID, "CursoDadosAtualizados", "", func(cur *Curso) error {
		return cur.AtualizarDados(&novo, nil, nil, nil)
	})
	if err != nil {
		t.Fatalf("Atualizar: %v", err)
	}
	if atualizado.Nome != novo || atualizado.Version != 2 {
		t.Fatalf("atualizado: nome=%q versão=%d", atualizado.Nome, atualizado.Version)
	}
	linhas, _ := client.ListarAuditoria(ctx, EntidadeCurso, c.ID.String(), 10, 0)
	if len(linhas) != 2 || linhas[0].Acao != "CursoDadosAtualizados" {
		t.Fatalf("auditoria: %+v", linhas)
	}
	if !strings.Contains(string(linhas[0].Antes), c.Nome) || !strings.Contains(string(linhas[0].Depois), novo) {
		t.Fatalf("antes/depois: antes=%s depois=%s", linhas[0].Antes, linhas[0].Depois)
	}
	if ok, msg, err := client.VerificarCadeiaAuditoria(ctx, EntidadeCurso, c.ID.String()); err != nil || !ok {
		t.Fatalf("cadeia: %v %q %v", ok, msg, err)
	}
}

func TestCursoRegraRecusadaNaoGravaNemAudita(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	c := novoCursoSuperior(t, client, a)
	ctx := context.Background()

	_, err := NovoCursosRepo(client).Atualizar(ctx, a.Audit, c.ID, "CursoAtivado", "", func(cur *Curso) error { return cur.Ativar() })
	if _, ok := EhErroRegra(err); !ok {
		t.Fatalf("err = %v, want violação de regra (curso já ativo)", err)
	}
	var versao int
	_ = client.DB().Get(&versao, `SELECT version FROM projection_cursos WHERE id=$1`, c.ID)
	if versao != 1 {
		t.Fatalf("versão mudou apesar da recusa: %d", versao)
	}
	if n := contar(t, client, `SELECT count(*) FROM auditoria_crud WHERE entidade_tipo='Curso' AND entidade_id=$1`, c.ID.String()); n != 1 {
		t.Fatalf("auditoria = %d, want só a da criação", n)
	}
}

func TestCursoInexistenteDevolveNaoEncontrado(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	_, err := NovoCursosRepo(client).Atualizar(context.Background(), a.Audit, uuid.New(), "X", "", func(*Curso) error { return nil })
	if !errors.Is(err, ErrNaoEncontrado) {
		t.Fatalf("err = %v, want ErrNaoEncontrado", err)
	}
}

// Duas alterações simultâneas do mesmo curso: cada uma enxerga o resultado da
// anterior (bloqueio de linha), nenhuma se perde, a versão e a cadeia de
// auditoria seguem contíguas.
func TestCursoAtualizacoesConcorrentesNaoSePerdem(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	c := novoCursoSuperior(t, client, a)
	repo := NovoCursosRepo(client)
	const n = 12

	var wg sync.WaitGroup
	erros := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			nome := fmt.Sprintf("Concorrente %02d", i)
			_, err := repo.Atualizar(context.Background(), a.Audit, c.ID, "CursoDadosAtualizados", "", func(cur *Curso) error {
				return cur.AtualizarDados(&nome, nil, nil, nil)
			})
			erros <- err
		}(i)
	}
	wg.Wait()
	close(erros)
	for err := range erros {
		if err != nil {
			t.Fatalf("atualização concorrente falhou: %v", err)
		}
	}
	var versao int
	_ = client.DB().Get(&versao, `SELECT version FROM projection_cursos WHERE id=$1`, c.ID)
	if versao != 1+n {
		t.Fatalf("versão = %d, want %d (uma atualização foi perdida)", versao, 1+n)
	}
	if got := contar(t, client, `SELECT count(*) FROM auditoria_crud WHERE entidade_tipo='Curso' AND entidade_id=$1`, c.ID.String()); got != 1+n {
		t.Fatalf("auditoria = %d, want %d", got, 1+n)
	}
	if ok, msg, err := client.VerificarCadeiaAuditoria(context.Background(), EntidadeCurso, c.ID.String()); err != nil || !ok {
		t.Fatalf("cadeia: %v %q %v", ok, msg, err)
	}
}

func TestMateriaCriarEAtualizarComAuditoria(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	curso := novoCursoSuperior(t, client, a)
	m := novaMateriaSuperior(t, client, a, curso, "Cálculo I")
	ctx := context.Background()

	var periodo, status string
	if err := client.DB().QueryRow(`SELECT periodo, status FROM projection_materias WHERE id=$1`, m.ID).Scan(&periodo, &status); err != nil {
		t.Fatal(err)
	}
	if periodo != "1_semestre" || status != "ativo" {
		t.Fatalf("periodo=%q status=%q", periodo, status)
	}

	novo := "Cálculo I (revisto)"
	got, err := NovoMateriasRepo(client).Atualizar(ctx, a.Audit, m.ID, "MateriaDadosAtualizados", "", func(x *Materia) error {
		return x.AtualizarDados(&novo, nil, nil, nil, nil)
	})
	if err != nil || got.Nome != novo || got.Version != 2 {
		t.Fatalf("atualizar: %+v err %v", got, err)
	}
	linhas, _ := client.ListarAuditoria(ctx, EntidadeMateria, m.ID.String(), 10, 0)
	if len(linhas) != 2 || linhas[0].Acao != "MateriaDadosAtualizados" {
		t.Fatalf("auditoria: %+v", linhas)
	}
}

// Trocar a matéria para um curso de OUTRA academia não é permitido (o código
// antigo não verificava isto na atualização).
func TestMateriaNaoPodeMudarParaCursoDeOutraAcademia(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	b := novaAcademia(t, client)
	cursoA := novoCursoSuperior(t, client, a)
	cursoB := novoCursoSuperior(t, client, b)
	m := novaMateriaSuperior(t, client, a, cursoA, "Álgebra")

	_, err := NovoMateriasRepo(client).Atualizar(context.Background(), a.Audit, m.ID, "MateriaDadosAtualizados", "", func(x *Materia) error {
		return x.AtualizarDados(nil, nil, &cursoB.ID, nil, nil)
	})
	r, ok := EhErroRegra(err)
	if !ok || !strings.Contains(r.Error(), "não pertence a esta academia") {
		t.Fatalf("err = %v, want 'curso não pertence a esta academia'", err)
	}
	var curso uuid.UUID
	_ = client.DB().Get(&curso, `SELECT curso_id FROM projection_materias WHERE id=$1`, m.ID)
	if curso != cursoA.ID {
		t.Fatalf("curso da matéria mudou: %s", curso)
	}
}

func TestMateriaCriarComCursoInexistenteOuDeOutraAcademiaFalha(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	b := novaAcademia(t, client)
	cursoB := novoCursoSuperior(t, client, b)
	repo := NovoMateriasRepo(client)

	for nome, cursoID := range map[string]uuid.UUID{"inexistente": uuid.New(), "de outra academia": cursoB.ID} {
		cid := cursoID
		m, _ := NovaMateria("X", "superior", []string{"1_ano_superior"}, a.Codigo, &cid, nil, nil)
		_ = m.DefinirPeriodo("1_semestre")
		if err := repo.Criar(context.Background(), a.Audit, m); err == nil {
			t.Errorf("%s: deveria falhar", nome)
		}
		if n := contar(t, client, `SELECT count(*) FROM projection_materias WHERE id=$1`, m.ID); n != 0 {
			t.Errorf("%s: matéria gravada", nome)
		}
	}
}

func TestDeletarCursoEmCascataEhAtomico(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	ctx := context.Background()
	cursos := NovoCursosRepo(client)
	materias := NovoMateriasRepo(client)

	t.Run("matéria ativa impede tudo", func(t *testing.T) {
		curso := novoCursoSuperior(t, client, a)
		m := novaMateriaSuperior(t, client, a, curso, "Ativa")
		if _, err := cursos.Atualizar(ctx, a.Audit, curso.ID, "CursoDesativado", "", func(c *Curso) error { return c.Desativar() }); err != nil {
			t.Fatal(err)
		}
		_, _, err := cursos.DeletarComMaterias(ctx, a.Audit, curso.ID, "teste")
		if _, ok := EhErroRegra(err); !ok {
			t.Fatalf("err = %v, want violação de regra", err)
		}
		var sc, sm string
		_ = client.DB().Get(&sc, `SELECT status FROM projection_cursos WHERE id=$1`, curso.ID)
		_ = client.DB().Get(&sm, `SELECT status FROM projection_materias WHERE id=$1`, m.ID)
		if sc != "inativo" || sm != "ativo" {
			t.Fatalf("estado mudou apesar da recusa: curso=%s matéria=%s", sc, sm)
		}
		if n := contar(t, client, `SELECT count(*) FROM auditoria_crud WHERE acao IN ('CursoDeletado','MateriaDeletada') AND entidade_id IN ($1,$2)`, curso.ID.String(), m.ID.String()); n != 0 {
			t.Fatalf("auditoria de deleção gravada sem deleção: %d", n)
		}
	})

	t.Run("deleta matérias inativas e o curso juntos, com uma auditoria cada", func(t *testing.T) {
		curso := novoCursoSuperior(t, client, a)
		m1 := novaMateriaSuperior(t, client, a, curso, "Inativa 1")
		m2 := novaMateriaSuperior(t, client, a, curso, "Já deletada")
		for _, m := range []*Materia{m1, m2} {
			if _, err := materias.Atualizar(ctx, a.Audit, m.ID, "MateriaDesativada", "", func(x *Materia) error { return x.Desativar() }); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := materias.Atualizar(ctx, a.Audit, m2.ID, "MateriaDeletada", "", func(x *Materia) error { return x.Deletar() }); err != nil {
			t.Fatal(err)
		}
		if _, err := cursos.Atualizar(ctx, a.Audit, curso.ID, "CursoDesativado", "", func(c *Curso) error { return c.Desativar() }); err != nil {
			t.Fatal(err)
		}

		deletado, nomes, err := cursos.DeletarComMaterias(ctx, a.Audit, curso.ID, "encerrado")
		if err != nil {
			t.Fatalf("DeletarComMaterias: %v", err)
		}
		if deletado.Status != StatusDeletado || len(nomes) != 1 || nomes[0] != "Inativa 1" {
			t.Fatalf("status=%s nomes=%v (a já deletada não deve ser repetida)", deletado.Status, nomes)
		}
		var d1, d2 string
		_ = client.DB().Get(&d1, `SELECT status FROM projection_materias WHERE id=$1`, m1.ID)
		_ = client.DB().Get(&d2, `SELECT status FROM projection_materias WHERE id=$1`, m2.ID)
		if d1 != "deletado" || d2 != "deletado" {
			t.Fatalf("matérias: %s %s", d1, d2)
		}
		linhas, _ := client.ListarAuditoria(ctx, EntidadeCurso, curso.ID.String(), 10, 0)
		if linhas[0].Acao != "CursoDeletado" || linhas[0].Motivo != "encerrado" {
			t.Fatalf("auditoria do curso: %+v", linhas[0])
		}
		lm, _ := client.ListarAuditoria(ctx, EntidadeMateria, m1.ID.String(), 10, 0)
		if lm[0].Acao != "MateriaDeletada" || !strings.Contains(lm[0].Motivo, "cascata") {
			t.Fatalf("auditoria da matéria: %+v", lm[0])
		}
	})
}

// ── O ponto que a conversão podia quebrar em silêncio ───────────────────────

// A reconstrução da projeção de academias faz TRUNCATE ... CASCADE. Enquanto
// cursos e matérias tinham chave estrangeira para as academias, isso apagava
// todos eles — e, agora que são o registro oficial (não vêm do ledger),
// seria perda irrecuperável. A transação é desfeita no fim: nada é apagado.
func TestReconstruirAcademiasNaoApagaCursosNemMaterias(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	curso := novoCursoSuperior(t, client, a)
	m := novaMateriaSuperior(t, client, a, curso, "Sobrevive")

	tx, err := client.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`TRUNCATE TABLE projection_academias CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	var nc, nm int
	if err := tx.QueryRow(`SELECT count(*) FROM projection_cursos WHERE id=$1`, curso.ID).Scan(&nc); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT count(*) FROM projection_materias WHERE id=$1`, m.ID).Scan(&nm); err != nil {
		t.Fatal(err)
	}
	if nc != 1 || nm != 1 {
		t.Fatalf("TRUNCATE de academias apagou dados CRUD: cursos=%d matérias=%d", nc, nm)
	}
}

// Guarda para as próximas conversões: tabela CRUD nenhuma pode ter chave
// estrangeira para uma tabela de projeção que o ledger reconstrói. (Apontar
// para outra tabela CRUD é permitido.)
func TestTabelasCRUDNaoDependemDeProjecoesReconstruiveis(t *testing.T) {
	client := clienteParaTeste(t)
	crud := map[string]bool{db.TabelaCursos: true, db.TabelaMaterias: true}

	var fks []struct {
		Origem  string `db:"origem"`
		Destino string `db:"destino"`
		Nome    string `db:"nome"`
	}
	if err := client.DB().Select(&fks, `
		SELECT conrelid::regclass::text AS origem, confrelid::regclass::text AS destino, conname AS nome
		FROM pg_constraint WHERE contype = 'f'`); err != nil {
		t.Fatal(err)
	}
	for _, fk := range fks {
		if crud[fk.Origem] && !crud[fk.Destino] {
			t.Errorf("tabela CRUD %s tem chave estrangeira %s para %s, que o ledger reconstrói: "+
				"um TRUNCATE ... CASCADE na reconstrução apagaria dados que não têm como voltar",
				fk.Origem, fk.Nome, fk.Destino)
		}
	}
}

func TestAtualizacaoAtualizaUpdatedAt(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	c := novoCursoSuperior(t, client, a)
	time.Sleep(20 * time.Millisecond)
	novo := "Outro nome"
	got, err := NovoCursosRepo(client).Atualizar(context.Background(), a.Audit, c.ID, "CursoDadosAtualizados", "", func(cur *Curso) error {
		return cur.AtualizarDados(&novo, nil, nil, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpdatedAt.After(c.UpdatedAt) {
		t.Fatalf("updated_at não avançou: %v -> %v", c.UpdatedAt, got.UpdatedAt)
	}
}
