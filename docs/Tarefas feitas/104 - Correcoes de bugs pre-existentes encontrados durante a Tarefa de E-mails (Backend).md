# Tarefa — Correções de bugs pré-existentes (backend)

**Repositório:** `rastreio-backend`
**Execução:** Codex só executa — todo o código abaixo já foi escrito, compilado (`go build ./...`), validado (`gofmt`, `go vet ./...`) e testado (`go test`, com PostgreSQL real) pelo orquestrador. Não há nada para planejar, só aplicar exatamente como está aqui e confirmar com os comandos da seção "Validação".

**Contexto:** estes 3 bugs foram encontrados incidentalmente durante a tarefa de e-mails (ver `Tarefa - E-mails de Verificacao e Recuperacao de Senha via Backend (Brevo).md`), mas **não têm nenhuma relação** com e-mail, matrícula, estudantes ou solicitações — são falhas pré-existentes e independentes na suíte de testes. Por isso viraram um documento à parte, em vez de serem misturados com a tarefa de e-mails.

Esta tarefa pode ser aplicada **antes, depois, ou independentemente** da tarefa de e-mails — não há dependência entre elas.

---

## 1. `internal/finance/appypay_test.go` — import de `"os"` faltando

**Sintoma:** `go vet ./...` falhava no módulo inteiro com `internal/finance/appypay_test.go:291:12: undefined: os`, porque o arquivo usa `os.Unsetenv` sem importar o pacote `"os"`.

**Localizar:**
```go
import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"spuri/internal/db"
)
```

**Substituir por:**
```go
import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"spuri/internal/db"
)
```

---

## 2. `internal/handlers/nivel_escolar_handlers_integration_test.go` — helper de teste não rodava as migrations

**Sintoma:** os testes que usam `nivelEscolarTestClient` (entre eles, os de edição de BI do estudante) falhavam com `relation "spuri_ledger" does not exist` quando rodados isoladamente (`go test -run TestAprovarSolicitacaoEdicaoBI...`), porque este helper conecta ao banco mas nunca chama `RunMigrations()` — ao contrário do helper equivalente usado noutros arquivos (`integrationFinanceClient`, em `financeiro_handlers_integration_test.go`), que já roda as migrations corretamente. Rodando a suíte inteira, os testes "funcionavam por sorte" apenas quando outro teste já tinha aplicado as migrations antes, na mesma conexão.

**Localizar:**
```go
func nivelEscolarTestClient(t *testing.T) *db.Client {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL não definido — pulei teste de integração de nivel_escolar")
	}
	client, err := db.NewClient(db.DefaultConfig())
	if err != nil {
		t.Fatalf("erro ao conectar no banco de teste: %v", err)
	}
	return client
}
```

**Substituir por:**
```go
func nivelEscolarTestClient(t *testing.T) *db.Client {
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
	if err := os.Chdir("../.."); err != nil {
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
```

Nada mais muda neste arquivo — o resto do arquivo (os testes que usam este helper) fica exatamente como está.

---

## 3. `internal/handlers/financeiro_handlers_integration_test.go` — a causa raiz real da intermitência dos testes de BI

Este é o bug mais sério dos três. Os outros dois (1 e 2) resolvem sintomas; este resolve a causa raiz de uma falha intermitente que afeta testes **completamente sem relação** com pagamentos/webhooks (os de edição de BI do estudante).

**O que estava acontecendo:** `seedAcademiaParaMatriculaWebhook` inseria a academia de teste **direto via SQL** em `projection_academias`, sem nenhum evento correspondente no event store (`spuri_ledger`). Isso funcionava para o próprio teste do webhook (que nunca chama `AcademiaProjection.Rebuild()`), mas o estudante criado logo a seguir — esse sim, através de um evento real (`EstudanteCriadoComVinculo`, gerado pelo próprio fluxo de webhook em produção) — referenciava essa academia pelo código.

Qualquer **outro** teste do mesmo pacote que chamasse depois `AcademiaProjection.Rebuild()` (que faz `TRUNCATE` na tabela e repõe só a partir dos eventos reais) apagava essa academia "fantasma" **para sempre** — ela não tinha evento para ser reconstruída. A partir daí, qualquer `EstudanteProjection.Rebuild()` posterior, em qualquer teste (inclusive os de edição de BI, que não têm nada a ver com isso), esbarrava nesse estudante órfão e falhava com `academia ainda não projetada`. A intermitência dependia só da ordem em que os testes do pacote rodavam.

**Correção:** criar a academia pelo caminho normal de event sourcing (agregado → `SaveWithAudit` → `Rebuild`), como qualquer outra academia real do sistema — assim ela sobrevive a qualquer `Rebuild()` futuro.

**Localizar:**
```go
func seedAcademiaParaMatriculaWebhook(t *testing.T, client *db.Client, codigo string) {
	t.Helper()
	_, err := client.DB().Exec(`INSERT INTO projection_academias
		(id,nivel,nome,nif,codigo_academia,senha_hash,provincia,endereco,nivel_escolar,status,cursos,anos_academicos,type,ano_letivo,created_at)
		VALUES ($1,'escola','Academia webhook',$2,$3,'hash','LUA','endereco','fundamental','ativo','[]'::jsonb,'["1_ano_fundamental"]'::jsonb,'private','2026_2027',CURRENT_TIMESTAMP)`,
		uuid.New(), strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, uuid.NewString())[:10], codigo)
	if err != nil {
		t.Fatal(err)
	}
}
```

**Substituir por:**
```go
// seedAcademiaParaMatriculaWebhook cria uma academia real (agregado ->
// event store -> projeção) para os testes de webhook AppyPay.
//
// ATENÇÃO — bug pré-existente corrigido nesta tarefa: esta função inseria a
// academia DIRETO via SQL em projection_academias, sem nenhum evento
// correspondente no event store (spuri_ledger). Isso funcionava para o
// PRÓPRIO teste (que nunca chama AcademiaProjection.Rebuild()), mas o
// estudante criado logo a seguir, via webhook, É gerado por um evento REAL
// (EstudanteCriadoComVinculo, referenciando esta academia pelo código). Se
// QUALQUER outro teste do pacote chamasse depois AcademiaProjection.Rebuild()
// (TRUNCATE + replay só dos eventos reais — ver admin_projection.go /
// academia_projection.go), a linha desta academia "fantasma" era apagada
// para sempre (sem evento para repô-la), deixando o estudante do webhook
// com uma academia órfã permanentemente. Qualquer EstudanteProjection.Rebuild()
// posterior (em QUALQUER outro teste, ex.: os de edição de BI) passava a
// falhar com "academia ainda não projetada" de forma aparentemente
// intermitente (na verdade dependia só da ordem de execução dos testes).
// Criar a academia pelo caminho normal de event sourcing resolve isso: ela
// passa a ser reconstruível por qualquer Rebuild() futuro, como qualquer
// outra academia real.
func seedAcademiaParaMatriculaWebhook(t *testing.T, client *db.Client, codigo string) {
	t.Helper()
	id := uuid.New()
	agg := &aggregates.Academia{}
	agg.SetID(id)
	nif := fmt.Sprintf("9%09d", time.Now().UnixNano()%1000000000)
	if err := agg.Criar("escola", "private", "Academia webhook", nif, codigo, "hash", "LUA", "endereco", nil, nil, nil, ptrString("fundamental"), nil, []string{"1_ano_fundamental"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := agg.DefinirAnoLetivo("2026_2027", "escolar", uuid.New()); err != nil {
		t.Fatal(err)
	}
	repository := db.NewAggregateRepository(client)
	if err := repository.SaveWithAudit(agg, db.AuditContext{UserID: "integration-test", UserType: "sistema", IP: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if err := projections.NewAcademiaProjection(client).Rebuild(); err != nil {
		t.Fatal(err)
	}
}

func ptrString(s string) *string { return &s }
```

### Import adicional necessário

Este arquivo passa a usar `fmt.Sprintf` (para gerar o NIF), que não estava importado. O bloco de import completo do arquivo já deve ficar assim:

**Localizar:**
```go
import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/finance"
	"spuri/internal/projections"
)
```

**Substituir por:**
```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/finance"
	"spuri/internal/projections"
)
```

Nenhum outro ponto deste arquivo muda — `aggregates`, `projections` e `time` já estavam importados antes (usados por outras partes do arquivo) e continuam sendo usados exatamente da mesma forma.

⚠️ **Atenção ao aplicar:** `strings` continua sendo necessário neste arquivo (usado por `geraDigitos`, mais abaixo, que não muda) — não remover esse import.

---

## Validação (já executada pelo orquestrador — Codex só precisa confirmar)

```bash
gofmt -l internal/finance/appypay_test.go internal/handlers/nivel_escolar_handlers_integration_test.go internal/handlers/financeiro_handlers_integration_test.go
# esperado: nenhuma saída

go build ./...
# esperado: nenhuma saída (sucesso)

go vet ./...
# esperado: nenhuma saída (módulo inteiro limpo — antes desta tarefa, o item 1 sozinho já fazia isto falhar)
```

Os comandos abaixo **exigem PostgreSQL real** (ambiente do Codex não tem — ver nota geral do orquestrador). Já foram executados com sucesso por mim, com banco novo antes de cada pacote:

```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/<banco_de_teste>?sslmode=disable"
export RUN_POSTGRES_INTEGRATION=1
export JWT_SECRET="<qualquer_valor_para_teste>"

go test ./internal/handlers/... -count=1   # ok — 100% (antes desta tarefa, 4 testes de edição de BI falhavam de forma intermitente aqui)
go test ./internal/finance/... -count=1    # ok — 100%
```

**Nota sobre paralelismo entre pacotes:** `go test ./...` sem restrição paraleliza pacotes diferentes como processos separados, todos batendo no mesmo Postgres — isso pode gerar falhas não relacionadas a este código (uma característica pré-existente da suíte, não um bug desta tarefa). Para validação confiável, rode um pacote por vez com banco recriado antes de cada um, como acima, ou use `go test -p 1 ./...`.

## Arquivos a remover

Nenhum. Esta tarefa só edita 2 arquivos de teste já existentes.

## Conclusão

Depois de aplicar e confirmar os 3 comandos de `gofmt`/`go build`/`go vet` acima (os únicos que não exigem Postgres), marcar esta tarefa como concluída. Se o ambiente do Codex não tiver Postgres disponível (situação esperada, conforme nota do orquestrador), os comandos `go test` com `RUN_POSTGRES_INTEGRATION=1` podem ser pulados — já foram validados previamente.
