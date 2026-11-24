package db

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// ── Unitários (sem banco) ────────────────────────────────────────────────────

func TestSerializarAuditoriaRecusaChavesProibidas(t *testing.T) {
	casos := map[string]any{
		"nivel raiz":      map[string]any{"senha": "x"},
		"hash de senha":   map[string]any{"senha_hash": "$2a$10$abc"},
		"maiusculas":      map[string]any{"Password": "x"},
		"aninhado":        map[string]any{"conta": map[string]any{"codigo_verificacao": "123456"}},
		"dentro de lista": map[string]any{"itens": []any{map[string]any{"token": "t"}}},
		"struct com tag": struct {
			Segredo string `json:"segredo"`
		}{"s"},
		"codigo 2fa":       map[string]any{"codigo_2fa": "123456"},
		"secret em ingles": map[string]any{"secret": "s"},
		"hash_senha":       map[string]any{"hash_senha": "h"},
		"password_hash":    map[string]any{"password_hash": "h"},
		"codigo simples":   map[string]any{"codigo": "123456"},
		"lista no topo":    []any{map[string]any{"senha": "x"}},
		"valor struct aninhado": struct {
			Conta struct {
				Senha string `json:"senha"`
			} `json:"conta"`
		}{},
	}
	for nome, valor := range casos {
		t.Run(nome, func(t *testing.T) {
			if _, err := serializarAuditoria(valor); !errors.Is(err, ErrAuditoriaChaveProibida) {
				t.Fatalf("err = %v, want ErrAuditoriaChaveProibida", err)
			}
		})
	}
}

func TestSerializarAuditoriaAceitaDadosNormais(t *testing.T) {
	aceitos := map[string]any{
		"nulo":                   nil,
		"mapa":                   map[string]any{"nome": "Curso A", "ativo": true},
		"data da troca de senha": map[string]any{"senha_alterada_em": "2026-11-23T10:00:00Z"},
		"chave que contém token": map[string]any{"token_expira_em": 10},
		"lista de objetos":       []any{map[string]any{"id": 1}},
		"slice nulo":             []any(nil),
	}
	for nome, valor := range aceitos {
		t.Run(nome, func(t *testing.T) {
			if _, err := serializarAuditoria(valor); err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
	if got, _ := serializarAuditoria(nil); got != nil {
		t.Fatalf("nil deve virar NULL, got %v", got)
	}
}

// A lista de chaves proibidas do Go e a da migration 142 não podem divergir.
func TestChavesProibidasAuditoriaIguaisAsDaMigration(t *testing.T) {
	bruto, err := os.ReadFile("../../migrations/142_auditoria_crud.sql")
	if err != nil {
		t.Fatalf("ler migration: %v", err)
	}
	bloco := regexp.MustCompile(`(?s)v_proibidas CONSTANT TEXT\[\] := ARRAY\[(.*?)\];`).FindSubmatch(bruto)
	if bloco == nil {
		t.Fatal("bloco v_proibidas não encontrado na migration 142")
	}
	var noSQL []string
	for _, m := range regexp.MustCompile(`'([a-z0-9_]+)'`).FindAllSubmatch(bloco[1], -1) {
		noSQL = append(noSQL, string(m[1]))
	}
	if len(noSQL) != len(ChavesProibidasAuditoria) {
		t.Fatalf("SQL tem %d chaves (%v), Go tem %d (%v)", len(noSQL), noSQL, len(ChavesProibidasAuditoria), ChavesProibidasAuditoria)
	}
	emGo := map[string]bool{}
	for _, k := range ChavesProibidasAuditoria {
		emGo[k] = true
	}
	for _, k := range noSQL {
		if !emGo[k] {
			t.Errorf("chave %q está no SQL e não no Go", k)
		}
	}
}

func TestRegistrarAuditoriaExigeCamposObrigatorios(t *testing.T) {
	aud := NovoAuditor(nil, AuditContext{}, nil)
	if err := aud.Registrar(context.Background(), EntradaAuditoria{}); err == nil {
		t.Fatal("sem transação deveria falhar")
	}
}

// ── Integração (PostgreSQL) ──────────────────────────────────────────────────

func clienteAuditoriaParaTeste(t *testing.T) *Client {
	t.Helper()
	if os.Getenv("RUN_POSTGRES_INTEGRATION") != "1" {
		t.Skip("teste de integração requer RUN_POSTGRES_INTEGRATION=1 e PostgreSQL")
	}
	prev, _ := os.Getwd()
	_ = os.Chdir("../..")
	t.Cleanup(func() { _ = os.Chdir(prev) })

	client, err := NewClient(DefaultConfig())
	if err != nil {
		t.Fatalf("conectar: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RunMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	return client
}

// criaTabelaDeRegistro cria uma tabela de "registro" descartável para provar
// que alteração e auditoria entram/saem juntas.
func criaTabelaDeRegistro(t *testing.T, client *Client) {
	t.Helper()
	if _, err := client.DB().Exec(`CREATE TABLE IF NOT EXISTS teste_crud_auditoria (id UUID PRIMARY KEY, nome TEXT NOT NULL)`); err != nil {
		t.Fatalf("criar tabela de teste: %v", err)
	}
	t.Cleanup(func() { _, _ = client.DB().Exec(`DROP TABLE IF EXISTS teste_crud_auditoria`) })
}

func contaLinhas(t *testing.T, client *Client, query string, args ...any) int {
	t.Helper()
	var n int
	if err := client.DB().Get(&n, query, args...); err != nil {
		t.Fatalf("contar: %v", err)
	}
	return n
}

func TestComAuditoriaAlteracaoEAuditoriaEntramJuntas(t *testing.T) {
	client := clienteAuditoriaParaTeste(t)
	criaTabelaDeRegistro(t, client)
	ctx := context.Background()
	id := uuid.NewString()

	err := client.ComAuditoria(ctx, AuditContext{UserID: "u-1", UserType: "academia", IP: "10.0.0.1"},
		func(tx *sqlx.Tx, aud *Auditor) error {
			if _, err := tx.ExecContext(ctx, `INSERT INTO teste_crud_auditoria (id, nome) VALUES ($1, 'Curso A')`, id); err != nil {
				return err
			}
			return aud.Registrar(ctx, EntradaAuditoria{
				EntidadeTipo: "TesteCrud", EntidadeID: id, Acao: "TesteCriado",
				Depois: map[string]any{"nome": "Curso A"},
			})
		})
	if err != nil {
		t.Fatalf("ComAuditoria: %v", err)
	}
	if n := contaLinhas(t, client, `SELECT count(*) FROM teste_crud_auditoria WHERE id=$1`, id); n != 1 {
		t.Fatalf("registro: %d linhas, want 1", n)
	}
	linhas, err := client.ListarAuditoria(ctx, "TesteCrud", id, 10, 0)
	if err != nil || len(linhas) != 1 {
		t.Fatalf("auditoria: %v linhas, err %v; want 1", len(linhas), err)
	}
	l := linhas[0]
	if l.AtorID != "u-1" || l.AtorTipo != "academia" || l.IP != "10.0.0.1" || l.Acao != "TesteCriado" || l.Sequencia != 1 {
		t.Fatalf("linha de auditoria inesperada: %+v", l)
	}
	if !strings.Contains(string(l.Depois), "Curso A") || string(l.Antes) != "null" {
		t.Fatalf("antes/depois inesperados: antes=%s depois=%s", l.Antes, l.Depois)
	}
}

func TestComAuditoriaFalhaDepoisDaAlteracaoDesfazTudo(t *testing.T) {
	client := clienteAuditoriaParaTeste(t)
	criaTabelaDeRegistro(t, client)
	ctx := context.Background()

	t.Run("fn devolve erro depois de alterar e auditar", func(t *testing.T) {
		id := uuid.NewString()
		boom := errors.New("falha no meio")
		err := client.ComAuditoria(ctx, AuditContext{UserID: "u", UserType: "academia"}, func(tx *sqlx.Tx, aud *Auditor) error {
			if _, err := tx.ExecContext(ctx, `INSERT INTO teste_crud_auditoria (id, nome) VALUES ($1, 'x')`, id); err != nil {
				return err
			}
			if err := aud.Registrar(ctx, EntradaAuditoria{EntidadeTipo: "TesteCrud", EntidadeID: id, Acao: "A"}); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want boom", err)
		}
		if n := contaLinhas(t, client, `SELECT count(*) FROM teste_crud_auditoria WHERE id=$1`, id); n != 0 {
			t.Fatalf("registro ficou (%d) apesar do erro", n)
		}
		if n := contaLinhas(t, client, `SELECT count(*) FROM auditoria_crud WHERE entidade_id=$1`, id); n != 0 {
			t.Fatalf("auditoria ficou (%d) apesar do erro", n)
		}
	})

	t.Run("a auditoria falha (chave proibida): a alteração também não fica", func(t *testing.T) {
		id := uuid.NewString()
		err := client.ComAuditoria(ctx, AuditContext{UserID: "u", UserType: "academia"}, func(tx *sqlx.Tx, aud *Auditor) error {
			if _, err := tx.ExecContext(ctx, `INSERT INTO teste_crud_auditoria (id, nome) VALUES ($1, 'x')`, id); err != nil {
				return err
			}
			return aud.Registrar(ctx, EntradaAuditoria{
				EntidadeTipo: "TesteCrud", EntidadeID: id, Acao: "SenhaAlterada",
				Depois: map[string]any{"senha_hash": "$2a$10$naoPodeEntrar"},
			})
		})
		if !errors.Is(err, ErrAuditoriaChaveProibida) {
			t.Fatalf("err = %v, want ErrAuditoriaChaveProibida", err)
		}
		if n := contaLinhas(t, client, `SELECT count(*) FROM teste_crud_auditoria WHERE id=$1`, id); n != 0 {
			t.Fatalf("a alteração ficou (%d) mesmo sem auditoria", n)
		}
	})

	t.Run("a alteração falha: não sobra auditoria", func(t *testing.T) {
		id := uuid.NewString()
		err := client.ComAuditoria(ctx, AuditContext{UserID: "u", UserType: "academia"}, func(tx *sqlx.Tx, aud *Auditor) error {
			if err := aud.Registrar(ctx, EntradaAuditoria{EntidadeTipo: "TesteCrud", EntidadeID: id, Acao: "A"}); err != nil {
				return err
			}
			// nome NOT NULL violado
			_, err := tx.ExecContext(ctx, `INSERT INTO teste_crud_auditoria (id, nome) VALUES ($1, NULL)`, id)
			return err
		})
		if err == nil {
			t.Fatal("esperava erro de NOT NULL")
		}
		if n := contaLinhas(t, client, `SELECT count(*) FROM auditoria_crud WHERE entidade_id=$1`, id); n != 0 {
			t.Fatalf("auditoria ficou (%d) sem a alteração", n)
		}
	})

	t.Run("campos obrigatórios", func(t *testing.T) {
		err := client.ComAuditoria(ctx, AuditContext{}, func(tx *sqlx.Tx, aud *Auditor) error {
			return aud.Registrar(ctx, EntradaAuditoria{EntidadeTipo: "TesteCrud", EntidadeID: " ", Acao: "A"})
		})
		if err == nil {
			t.Fatal("entidade_id vazio deveria falhar")
		}
	})
}

func TestAuditoriaCrudCadeiaDeHashEOrdem(t *testing.T) {
	client := clienteAuditoriaParaTeste(t)
	ctx := context.Background()
	id := uuid.NewString()
	academia := uuid.NewString()

	for i := 0; i < 3; i++ {
		antes := map[string]any{"nome": i}
		depois := map[string]any{"nome": i + 1}
		if i == 0 {
			antes = nil
		}
		err := client.ComAuditoriaComAtor(ctx, AuditContext{UserID: "academia-1", UserType: "academia"},
			&AtorAuditoria{ID: "sec-9", Tipo: "secretario"},
			func(tx *sqlx.Tx, aud *Auditor) error {
				return aud.Registrar(ctx, EntradaAuditoria{
					EntidadeTipo: "TesteCadeia", EntidadeID: id, AcademiaID: academia,
					Acao: "Alterado", Antes: antes, Depois: depois, Motivo: "teste",
				})
			})
		if err != nil {
			t.Fatalf("registro %d: %v", i, err)
		}
	}

	linhas, err := client.ListarAuditoria(ctx, "TesteCadeia", id, 10, 0)
	if err != nil || len(linhas) != 3 {
		t.Fatalf("listar: %d linhas, err %v", len(linhas), err)
	}
	if linhas[0].Sequencia != 3 || linhas[2].Sequencia != 1 {
		t.Fatalf("ordem deve ser da mais recente para a mais antiga: %d..%d", linhas[0].Sequencia, linhas[2].Sequencia)
	}
	if linhas[0].EmNomeDeID != "sec-9" || linhas[0].EmNomeDeTipo != "secretario" || linhas[0].AtorID != "academia-1" {
		t.Fatalf("ator real não registrado: %+v", linhas[0])
	}
	if !linhas[0].AcademiaID.Valid || linhas[0].AcademiaID.String != academia {
		t.Fatalf("academia_id = %+v", linhas[0].AcademiaID)
	}

	var encadeamento []struct {
		Sequencia    int     `db:"sequencia"`
		HashAnterior *string `db:"hash_anterior"`
		Hash         string  `db:"hash"`
	}
	if err := client.DB().Select(&encadeamento,
		`SELECT sequencia, hash_anterior, hash FROM auditoria_crud WHERE entidade_tipo='TesteCadeia' AND entidade_id=$1 ORDER BY sequencia`, id); err != nil {
		t.Fatal(err)
	}
	if encadeamento[0].HashAnterior != nil {
		t.Fatal("primeira linha deve ter hash_anterior NULL")
	}
	for i := 1; i < len(encadeamento); i++ {
		if encadeamento[i].HashAnterior == nil || *encadeamento[i].HashAnterior != encadeamento[i-1].Hash {
			t.Fatalf("linha %d não aponta para o hash da anterior", i+1)
		}
	}
	if ok, msg, err := client.VerificarCadeiaAuditoria(ctx, "TesteCadeia", id); err != nil || !ok {
		t.Fatalf("cadeia deveria estar íntegra: %v %q %v", ok, msg, err)
	}

	// Paginação.
	pag, _ := client.ListarAuditoria(ctx, "TesteCadeia", id, 1, 1)
	if len(pag) != 1 || pag[0].Sequencia != 2 {
		t.Fatalf("paginação: %+v", pag)
	}
}

func TestAuditoriaCrudEhSomenteInsercao(t *testing.T) {
	client := clienteAuditoriaParaTeste(t)
	ctx := context.Background()
	id := uuid.NewString()
	if err := client.ComAuditoria(ctx, AuditContext{UserID: "u", UserType: "admin"}, func(tx *sqlx.Tx, aud *Auditor) error {
		return aud.Registrar(ctx, EntradaAuditoria{EntidadeTipo: "TesteImutavel", EntidadeID: id, Acao: "A"})
	}); err != nil {
		t.Fatal(err)
	}
	for nome, sql := range map[string]string{
		"UPDATE":   `UPDATE auditoria_crud SET acao='X' WHERE entidade_id='` + id + `'`,
		"DELETE":   `DELETE FROM auditoria_crud WHERE entidade_id='` + id + `'`,
		"TRUNCATE": `TRUNCATE auditoria_crud`,
	} {
		if _, err := client.DB().ExecContext(ctx, sql); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Errorf("%s: err = %v, want append-only", nome, err)
		}
	}
	if n := contaLinhas(t, client, `SELECT count(*) FROM auditoria_crud WHERE entidade_id=$1`, id); n != 1 {
		t.Fatalf("a linha deveria continuar lá, got %d", n)
	}
}

func TestAuditoriaCrudDetectaAdulteracao(t *testing.T) {
	client := clienteAuditoriaParaTeste(t)
	ctx := context.Background()
	id := uuid.NewString()
	for i := 0; i < 3; i++ {
		if err := client.ComAuditoria(ctx, AuditContext{UserID: "u", UserType: "admin"}, func(tx *sqlx.Tx, aud *Auditor) error {
			return aud.Registrar(ctx, EntradaAuditoria{EntidadeTipo: "TesteAdulterado", EntidadeID: id, Acao: "A", Depois: map[string]any{"i": i}})
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Um superuser que desliga o trigger consegue adulterar — a verificação
	// de hash existe para esse caso.
	conn := client.DB()
	if _, err := conn.Exec(`ALTER TABLE auditoria_crud DISABLE TRIGGER auditoria_crud_bloquear_update`); err != nil {
		t.Skipf("sem permissão para desligar o trigger neste banco: %v", err)
	}
	defer func() { _, _ = conn.Exec(`ALTER TABLE auditoria_crud ENABLE TRIGGER auditoria_crud_bloquear_update`) }()

	if _, err := conn.Exec(`UPDATE auditoria_crud SET depois='{"i": 99}' WHERE entidade_id=$1 AND sequencia=2`, id); err != nil {
		t.Fatal(err)
	}
	ok, msg, err := client.VerificarCadeiaAuditoria(ctx, "TesteAdulterado", id)
	if err != nil || ok || !strings.Contains(msg, "sequência 2") {
		t.Fatalf("adulteração não detectada na sequência 2: ok=%v msg=%q err=%v", ok, msg, err)
	}
}

func TestAuditoriaCrudBancoRecusaChaveProibida(t *testing.T) {
	client := clienteAuditoriaParaTeste(t)
	for nome, depois := range map[string]string{
		"raiz":          `{"senha_hash":"x"}`,
		"aninhado":      `{"a":{"b":{"Codigo_Verificacao":"1"}}}`,
		"lista":         `{"l":[{"token":"t"}]}`,
		"lista no topo": `[{"secret":"s"}]`,
	} {
		_, err := client.DB().Exec(`INSERT INTO auditoria_crud (entidade_tipo, entidade_id, acao, depois, sequencia, hash)
			VALUES ('TesteProibida', $1, 'A', $2::jsonb, 0, '')`, uuid.NewString(), depois)
		if err == nil || !strings.Contains(err.Error(), "não aceita a chave") {
			t.Errorf("%s: err = %v, want recusa pelo trigger", nome, err)
		}
	}
	// "antes" também é verificado.
	_, err := client.DB().Exec(`INSERT INTO auditoria_crud (entidade_tipo, entidade_id, acao, antes, sequencia, hash)
		VALUES ('TesteProibida', $1, 'A', '{"password":"x"}'::jsonb, 0, '')`, uuid.NewString())
	if err == nil || !strings.Contains(err.Error(), "não aceita a chave") {
		t.Errorf("antes: err = %v", err)
	}
	// Nome parecido, mas não igual, é aceito.
	_, err = client.DB().Exec(`INSERT INTO auditoria_crud (entidade_tipo, entidade_id, acao, depois, sequencia, hash)
		VALUES ('TesteProibida', $1, 'A', '{"senha_alterada_em":"2026-11-23"}'::jsonb, 0, '')`, uuid.NewString())
	if err != nil {
		t.Errorf("senha_alterada_em deveria ser aceito: %v", err)
	}
}

func TestAuditoriaCrudConcorrenciaNaMesmaEntidadeNaoBifurca(t *testing.T) {
	client := clienteAuditoriaParaTeste(t)
	ctx := context.Background()
	id := uuid.NewString()
	const workers = 20

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- client.ComAuditoria(ctx, AuditContext{UserID: "u", UserType: "academia"}, func(tx *sqlx.Tx, aud *Auditor) error {
				return aud.Registrar(ctx, EntradaAuditoria{EntidadeTipo: "TesteConcorrencia", EntidadeID: id, Acao: "A", Depois: map[string]any{"i": i}})
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("gravação concorrente falhou: %v", err)
		}
	}

	var seqs []int
	if err := client.DB().Select(&seqs, `SELECT sequencia FROM auditoria_crud WHERE entidade_tipo='TesteConcorrencia' AND entidade_id=$1 ORDER BY sequencia`, id); err != nil {
		t.Fatal(err)
	}
	if len(seqs) != workers {
		t.Fatalf("linhas = %d, want %d", len(seqs), workers)
	}
	for i, s := range seqs {
		if s != i+1 {
			t.Fatalf("sequência não contígua: %v", seqs)
		}
	}
	if ok, msg, err := client.VerificarCadeiaAuditoria(ctx, "TesteConcorrencia", id); err != nil || !ok {
		t.Fatalf("cadeia quebrada sob concorrência: %v %q %v", ok, msg, err)
	}
}

// Quem usa NovoAuditor numa transação própria (isolamento padrão, read
// committed) não tem a retentativa de ComAuditoria: aqui só o advisory lock por
// entidade, dentro do trigger, impede duas gravações de lerem o mesmo "último"
// e bifurcarem a cadeia.
func TestAuditoriaCrudAdvisoryLockProtegeTransacaoPropriaReadCommitted(t *testing.T) {
	client := clienteAuditoriaParaTeste(t)
	ctx := context.Background()
	id := uuid.NewString()
	const workers = 20

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tx, err := client.DB().BeginTxx(ctx, nil) // read committed
			if err != nil {
				errs <- err
				return
			}
			defer tx.Rollback()
			aud := NovoAuditor(tx, AuditContext{UserID: "u", UserType: "academia"}, nil)
			if err := aud.Registrar(ctx, EntradaAuditoria{EntidadeTipo: "TesteReadCommitted", EntidadeID: id, Acao: "A", Depois: map[string]any{"i": i}}); err != nil {
				errs <- err
				return
			}
			errs <- tx.Commit()
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("gravação concorrente (read committed) falhou: %v", err)
		}
	}
	if n := contaLinhas(t, client, `SELECT count(*) FROM auditoria_crud WHERE entidade_tipo='TesteReadCommitted' AND entidade_id=$1`, id); n != workers {
		t.Fatalf("linhas = %d, want %d", n, workers)
	}
	if ok, msg, err := client.VerificarCadeiaAuditoria(ctx, "TesteReadCommitted", id); err != nil || !ok {
		t.Fatalf("cadeia quebrada: %v %q %v", ok, msg, err)
	}
}
