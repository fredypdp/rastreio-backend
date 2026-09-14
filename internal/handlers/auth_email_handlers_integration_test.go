package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/projections"
)

// applyAdminEvents aplica, de forma incremental, TODOS os eventos já
// persistidos no ledger para um agregado Admin específico à projeção
// (projection_admins) — usando o MESMO dispatcher (AdminProjection.Handle)
// que projections.NewAdminProjection(client).Rebuild() usa internamente.
//
// Deliberadamente NÃO se usa Rebuild() neste arquivo: Rebuild() começa com
// `TRUNCATE TABLE projection_admins CASCADE` (ver admin_projection.go) e
// depois repõe TODOS os admins a partir do zero, incluindo os de outros
// testes deste MESMO pacote que não passam pelo event store (ex.:
// financeiro_handlers_integration_test.go insere admins diretamente via SQL
// para simular papéis — um TRUNCATE apagaria essas linhas criadas antes ou
// depois deste teste, quebrando testes irmãos sem qualquer relação com
// e-mail/recuperação de senha). Aplicar só os eventos do agregado em
// questão evita esse efeito colateral.
func applyAdminEvents(t *testing.T, client *db.Client, aggregateID uuid.UUID) {
	t.Helper()
	projection := projections.NewAdminProjection(client)
	rows, err := client.DB().Query(`
		SELECT id, event_id, aggregate_id, aggregate_type, event_type,
			event_version, payload, metadata, occurred_at, recorded_at,
			ledger_hash, previous_hash
		FROM spuri_ledger
		WHERE aggregate_id = $1
		ORDER BY id ASC
	`, aggregateID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var event db.Event
		var prevHash sql.NullString
		if err := rows.Scan(
			&event.ID, &event.EventID, &event.AggregateID, &event.AggregateType,
			&event.EventType, &event.EventVersion, &event.Payload, &event.Metadata,
			&event.OccurredAt, &event.RecordedAt, &event.LedgerHash, &prevHash,
		); err != nil {
			t.Fatal(err)
		}
		if prevHash.Valid {
			event.PreviousHash = &prevHash.String
		}
		if err := projection.Handle(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

var (
	testCreatorAdminOnce sync.Once
	testCreatorAdminID   uuid.UUID
)

// getOrCreateTestCreatorAdmin devolve o ID de um admin real já persistido em
// projection_admins, para ser usado como created_by (foreign key) dos
// admins seedados nos testes deste arquivo.
//
// A coluna role tem check constraint no banco (fpp/adm/gerente apenas —
// migrations/001_complete_schema.sql), e a constraint de bootstrap
// (idx_bootstrap_fpp_unique, migrations/025_admin_email_unique_index.sql)
// permite no máximo um admin com created_by NULO por role — vaga também
// disputada por testes já existentes neste mesmo pacote (ex.:
// financeiro_handlers_integration_test.go cria admins nas 3 roles com
// created_by implicitamente nulo). Por isso este criador ocupa a vaga
// (created_by nulo) só o tempo mínimo necessário: assim que persistido via
// event sourcing (sobrevive a qualquer Handle() futuro, ao contrário de um
// INSERT solto), a própria linha é atualizada para se autorreferenciar em
// created_by — o que satisfaz a foreign key de quem a referenciar depois E
// tira a linha do escopo do índice parcial (que só vale quando created_by É
// nulo), libertando a vaga para os outros testes deste pacote.
func getOrCreateTestCreatorAdmin(t *testing.T, client *db.Client) uuid.UUID {
	t.Helper()
	testCreatorAdminOnce.Do(func() {
		hash, err := bcrypt.GenerateFromPassword([]byte("CriadorDeTeste#123"), bcrypt.DefaultCost)
		if err != nil {
			t.Fatal(err)
		}
		telefone := "923000111"
		creator := aggregates.NewAdmin()
		if err := creator.Criar("Criador de Teste", "criador-"+uuid.NewString()+"@example.test", &telefone, string(hash), "gerente", nil); err != nil {
			t.Fatal(err)
		}
		repository := db.NewAggregateRepository(client)
		if err := repository.SaveWithAudit(creator, db.AuditContext{UserID: "integration-test", UserType: "sistema", IP: "127.0.0.1"}); err != nil {
			t.Fatal(err)
		}
		applyAdminEvents(t, client, creator.ID)

		if _, err := client.DB().Exec(`UPDATE projection_admins SET created_by = $1 WHERE id = $1`, creator.ID); err != nil {
			t.Fatal(err)
		}
		testCreatorAdminID = creator.ID
	})
	return testCreatorAdminID
}

// seedAdminParaRecuperacao cria um admin real (agregado -> event store ->
// projeção), com senha e email_verificado controlados pelo teste. Usa o
// mesmo caminho de event sourcing de que o handler em teste depende
// (repository.Load(userID, "Admin")), ao contrário de um INSERT direto na
// projeção, que deixaria o event store vazio e faria SolicitarRecuperacaoSenha
// falhar com "administrador não encontrado".
func seedAdminParaRecuperacao(t *testing.T, client *db.Client, senha string, emailVerificado bool) (userID uuid.UUID, email string) {
	t.Helper()
	repository := db.NewAggregateRepository(client)
	audit := db.AuditContext{UserID: "integration-test", UserType: "sistema", IP: "127.0.0.1"}

	hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}

	criadoPor := getOrCreateTestCreatorAdmin(t, client)
	email = "admin-" + uuid.NewString() + "@example.test"
	telefone := "923456789"
	admin := aggregates.NewAdmin()
	if err := admin.Criar("Admin de Teste", email, &telefone, string(hash), "adm", &criadoPor); err != nil {
		t.Fatal(err)
	}
	if emailVerificado {
		if err := admin.VerificarEmail(); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.SaveWithAudit(admin, audit); err != nil {
		t.Fatal(err)
	}
	applyAdminEvents(t, client, admin.ID)
	return admin.ID, email
}

func postJSON(router *gin.Engine, method, path string, body map[string]any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewBuffer(raw))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)
	return recorder
}

// routerParaAuthEmail monta um router mínimo só com a rota pública testada
// aqui, injetando dbClient e repository exatamente como setupRouter faz em
// cmd/server/main.go (mesmas chaves de contexto usadas por getDbClient e
// getRepository em helpers.go).
func routerParaAuthEmail(client *db.Client) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	repository := db.NewAggregateRepository(client)
	router.Use(func(c *gin.Context) {
		c.Set("dbClient", client)
		c.Set("repository", repository)
		c.Next()
	})
	router.POST("/email/recuperar-senha/solicitar", SolicitarRecuperacaoSenha)
	return router
}

func TestIntegrationSolicitarRecuperacaoSenhaAplicanovaSenhaImediatamente(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)

	userID, email := seedAdminParaRecuperacao(t, client, "SenhaAntiga#123", true)

	recorder := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{
		"identificador": email,
		"tipo":          "admin",
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("esperava 200, recebi %d: %s", recorder.Code, recorder.Body.String())
	}

	// O handler gravou um novo evento (AdminSenhaAlterada) no ledger — aplica-o
	// à projeção antes de consultar projection_admins (ver applyAdminEvents).
	applyAdminEvents(t, client, userID)

	// Prova empírica de que a senha REALMENTE mudou: a senha antiga não
	// deve mais bater com o hash persistido. Sem BREVO_API_KEY configurada
	// no ambiente de teste, SendPasswordResetEmail cai no modo degradado
	// (loga a senha temporária e devolve sucesso) — por isso não temos a
	// senha nova aqui para testar positivamente, mas a prova negativa (a
	// senha antiga deixou de funcionar) já demonstra que a troca ocorreu.
	var novoHash string
	if err := client.DB().QueryRow(`SELECT senha_hash FROM projection_admins WHERE id = $1`, userID).Scan(&novoHash); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(novoHash), []byte("SenhaAntiga#123")) == nil {
		t.Fatal("a senha antiga ainda é válida após SolicitarRecuperacaoSenha — a senha não foi trocada")
	}
}

func TestIntegrationSolicitarRecuperacaoSenhaExigeEmailVerificado(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)

	userID, email := seedAdminParaRecuperacao(t, client, "SenhaAntiga#123", false)

	recorder := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{
		"identificador": email,
		"tipo":          "admin",
	})

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("esperava 403 (email não verificado), recebi %d: %s", recorder.Code, recorder.Body.String())
	}

	var hashAtual string
	if err := client.DB().QueryRow(`SELECT senha_hash FROM projection_admins WHERE id = $1`, userID).Scan(&hashAtual); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hashAtual), []byte("SenhaAntiga#123")) != nil {
		t.Fatal("a senha foi alterada mesmo com email não verificado — não deveria")
	}
}

func TestIntegrationSolicitarRecuperacaoSenhaAutoDetectaTipoQuandoOmitido(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)

	userID, email := seedAdminParaRecuperacao(t, client, "SenhaAntiga#123", true)

	// Nenhum "tipo" no corpo — deve auto-detectar como admin (estudante e
	// academia não têm esse email, então a busca cai no admin).
	recorder := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{
		"identificador": email,
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("esperava 200, recebi %d: %s", recorder.Code, recorder.Body.String())
	}

	applyAdminEvents(t, client, userID)

	var novoHash string
	if err := client.DB().QueryRow(`SELECT senha_hash FROM projection_admins WHERE id = $1`, userID).Scan(&novoHash); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(novoHash), []byte("SenhaAntiga#123")) == nil {
		t.Fatal("a senha antiga ainda é válida — auto-detecção de tipo não funcionou")
	}
}

func TestIntegrationSolicitarRecuperacaoSenhaIdentificadorInexistente(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)

	recorder := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{
		"identificador": "ninguem-" + uuid.NewString() + "@example.test",
		"tipo":          "admin",
	})

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("esperava 404, recebi %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestIntegrationSolicitarVerificacaoEmailGeraTokenPersistido(t *testing.T) {
	client := integrationFinanceClient(t)
	repository := db.NewAggregateRepository(client)

	userID, email := seedAdminParaRecuperacao(t, client, "SenhaQualquer#123", false)

	// SolicitarVerificacaoEmail exige AuthMiddleware (lê user_id/user_type
	// do contexto, não do corpo da requisição) — reproduzido aqui como em
	// TestIntegrationFinanceRejectsNonFPPAdmins, via gin.CreateTestContext
	// direto, sem passar pelo router com middleware de auth real.
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/email/verificar-email/solicitar", nil)
	ctx.Set("dbClient", client)
	ctx.Set("repository", repository)
	ctx.Set("user_id", userID)
	ctx.Set("user_type", "admin")

	SolicitarVerificacaoEmail(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("esperava 200, recebi %d: %s", recorder.Code, recorder.Body.String())
	}

	var count int
	if err := client.DB().QueryRow(
		`SELECT COUNT(*) FROM auth_tokens WHERE email = $1 AND tipo = 'verificacao_email' AND usado = FALSE`,
		email,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("esperava exatamente 1 token de verificação não usado para %s, encontrei %d", email, count)
	}
}
