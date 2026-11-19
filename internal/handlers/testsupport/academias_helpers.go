package testsupport

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/projections"
)

func ExtractFunctionSource(t *testing.T, source, funcSignature string) string {
	t.Helper()
	start := strings.Index(source, funcSignature)
	if start == -1 {
		t.Fatalf("função não encontrada no arquivo: %s", funcSignature)
	}
	rest := source[start:]
	end := strings.Index(rest[1:], "\nfunc ")
	if end == -1 {
		return rest
	}
	return rest[:end+1]
}

func CriarAcademiaEscolarParaTeste(t *testing.T, client *db.Client, codigo, nivelEscolar string, anos []string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	agg := &aggregates.Academia{}
	agg.SetID(id)
	nif := fmt.Sprintf("9%09d", time.Now().UnixNano()%1000000000)
	if err := agg.Criar("escola", "private", "Academia Teste "+codigo, nif, codigo, "hash", "LUA", "Rua Teste", nil, nil, nil, &nivelEscolar, nil, anos, nil); err != nil {
		t.Fatalf("erro ao criar academia de teste: %v", err)
	}
	repository := db.NewAggregateRepository(client)
	if err := repository.SaveWithAudit(agg, db.AuditContext{UserID: "integration-test", UserType: "sistema", IP: "127.0.0.1"}); err != nil {
		t.Fatalf("erro ao salvar academia de teste: %v", err)
	}
	if err := projections.NewAcademiaProjection(client).Rebuild(); err != nil {
		t.Fatalf("erro ao reconstruir projeção de academias: %v", err)
	}
	return id
}

func NivelEscolarTestClient(t *testing.T) *db.Client {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL não definido — pulei teste de integração de nivel_escolar")
	}
	// RunMigrations() procura o diretório migrations/ relativo ao working
	// directory do processo — a partir de internal/handlers ele não é
	// encontrado, por isso o mesmo padrão de chdir usado em
	// integrationFinanceClient (financeiro_handlers_integration_test.go) é
	// replicado aqui.
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(RaizDoRepositorio(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDir) })

	client, err := db.NewClient(db.DefaultConfig())
	if err != nil {
		t.Fatalf("erro ao conectar no banco de teste: %v", err)
	}
	// Sem isto, testes que usam este helper dependiam de outro teste (ex.:
	// integrationFinanceClient) já ter rodado as migrations antes, no MESMO
	// binário de teste — rodando isoladamente (ex.: `go test -run
	// TestAprovarSolicitacaoEdicaoBI...`), falhavam com 'relation
	// "spuri_ledger" does not exist'. RunMigrations() é idempotente (usa
	// IF NOT EXISTS), então é seguro chamar mesmo que outro teste já as
	// tenha aplicado nesta mesma conexão de banco.
	if err := client.RunMigrations(); err != nil {
		t.Fatalf("erro ao rodar migrations: %v", err)
	}
	return client
}
