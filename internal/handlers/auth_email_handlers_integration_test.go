package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/projections"
	"spuri/internal/services"
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

// routerParaAuthEmail monta um router mínimo só com as rotas públicas testadas
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
	router.POST("/email/recuperar-senha/:token", ResetarSenha)
	return router
}

// emailBrevoCapturado é um e-mail recebido pelo Brevo falso.
type emailBrevoCapturado struct {
	Para    string
	Assunto string
	HTML    string
	Texto   string
}

// brevoFalso liga o canal Brevo apontando para um servidor local que só
// regista o que recebe, e torna síncrono o envio em segundo plano da
// recuperação. Devolve a função que lista os e-mails capturados.
func brevoFalso(t *testing.T) func() []emailBrevoCapturado {
	t.Helper()
	var mu sync.Mutex
	var recebidos []emailBrevoCapturado
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corpo, _ := io.ReadAll(r.Body)
		var req struct {
			To []struct {
				Email string `json:"email"`
			} `json:"to"`
			Subject     string `json:"subject"`
			HTMLContent string `json:"htmlContent"`
			TextContent string `json:"textContent"`
		}
		_ = json.Unmarshal(corpo, &req)
		mu.Lock()
		para := ""
		if len(req.To) > 0 {
			para = req.To[0].Email
		}
		recebidos = append(recebidos, emailBrevoCapturado{Para: para, Assunto: req.Subject, HTML: req.HTMLContent, Texto: req.TextContent})
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"messageId":"teste"}`))
	}))
	t.Cleanup(srv.Close)

	anteriorURL := services.BrevoAPIURL
	services.BrevoAPIURL = srv.URL
	anteriorExec := executarEnvioRecuperacao
	executarEnvioRecuperacao = func(f func()) { f() }
	t.Cleanup(func() {
		services.BrevoAPIURL = anteriorURL
		executarEnvioRecuperacao = anteriorExec
	})
	t.Setenv("BREVO_API_KEY", "chave-de-teste")
	t.Setenv("EMAIL_FROM", "avisos@spuri.test")
	t.Setenv("FRONTEND_URL", "https://app.spuri.test")

	return func() []emailBrevoCapturado {
		mu.Lock()
		defer mu.Unlock()
		return append([]emailBrevoCapturado(nil), recebidos...)
	}
}

var reTokenRecuperacao = regexp.MustCompile(`/recuperar-senha/([0-9a-f]{64})`)

func contarEventosDoAgregado(t *testing.T, client *db.Client, id uuid.UUID) int {
	t.Helper()
	var n int
	if err := client.DB().QueryRow(`SELECT COUNT(*) FROM spuri_ledger WHERE aggregate_id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// SEC-03: pedir a recuperação NÃO altera a senha (antes, qualquer pessoa com o
// identificador conseguia trocar a senha de outra conta). Envia um LINK.
func TestIntegrationSolicitarRecuperacaoSenhaNaoAlteraSenhaEEnviaLink(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)
	capturados := brevoFalso(t)

	userID, email := seedAdminParaRecuperacao(t, client, "SenhaAntiga#123", true)
	eventosAntes := contarEventosDoAgregado(t, client, userID)

	recorder := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{
		"identificador": email,
		"tipo":          "admin",
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("esperava 200, recebi %d: %s", recorder.Code, recorder.Body.String())
	}

	// Nenhum evento novo no ledger e hash intacto.
	if depois := contarEventosDoAgregado(t, client, userID); depois != eventosAntes {
		t.Fatalf("o pedido de recuperação gravou %d evento(s) novo(s); a senha só pode mudar no passo do link", depois-eventosAntes)
	}
	applyAdminEvents(t, client, userID)
	var hash string
	if err := client.DB().QueryRow(`SELECT senha_hash FROM projection_admins WHERE id = $1`, userID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("SenhaAntiga#123")) != nil {
		t.Fatal("a senha foi alterada ao pedir a recuperação — só o link pode alterá-la")
	}

	// Exatamente um e-mail, ao titular, com link de redefinição e SEM senha.
	enviados := capturados()
	if len(enviados) != 1 {
		t.Fatalf("esperava 1 e-mail, o Brevo recebeu %d", len(enviados))
	}
	msg := enviados[0]
	if msg.Para != email {
		t.Fatalf("e-mail enviado para %q, quer %q", msg.Para, email)
	}
	m := reTokenRecuperacao.FindStringSubmatch(msg.HTML)
	if m == nil || !strings.Contains(msg.HTML, "https://app.spuri.test/recuperar-senha/") {
		t.Fatalf("o e-mail não contém o link https://app.spuri.test/recuperar-senha/<token>: %s", msg.HTML)
	}
	if strings.Contains(strings.ToLower(msg.HTML), "senha temporária") || strings.Contains(strings.ToLower(msg.Texto), "senha temporária") {
		t.Fatal("o e-mail de recuperação não pode conter senha")
	}
	var usado bool
	if err := client.DB().QueryRow(`SELECT usado FROM auth_tokens WHERE token = $1 AND tipo = 'recuperacao_senha' AND user_id = $2`, m[1], userID).Scan(&usado); err != nil {
		t.Fatalf("o token do link não está persistido: %v", err)
	}
	if usado {
		t.Fatal("o token acabou de ser emitido e não pode estar usado")
	}
}

// SEC-06: a mesma resposta (código e corpo) para conta elegível, conta com
// e-mail não verificado e conta inexistente — e só a elegível recebe e-mail.
func TestIntegrationSolicitarRecuperacaoSenhaRespostaIdenticaParaQualquerConta(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)
	capturados := brevoFalso(t)

	_, emailVerificado := seedAdminParaRecuperacao(t, client, "SenhaAntiga#123", true)
	_, emailNaoVerificado := seedAdminParaRecuperacao(t, client, "SenhaAntiga#123", false)
	inexistente := "ninguem-" + uuid.NewString() + "@example.test"

	var corpos []string
	for _, id := range []string{emailVerificado, emailNaoVerificado, inexistente} {
		rec := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{"identificador": id, "tipo": "admin"})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: esperava 200, recebi %d: %s", id, rec.Code, rec.Body.String())
		}
		corpos = append(corpos, rec.Body.String())
	}
	if corpos[0] != corpos[1] || corpos[1] != corpos[2] {
		t.Fatalf("as respostas diferem (permite enumerar contas):\n%s\n%s\n%s", corpos[0], corpos[1], corpos[2])
	}
	if !strings.Contains(corpos[0], mensagemRecuperacaoSenha) {
		t.Fatalf("mensagem inesperada: %s", corpos[0])
	}
	enviados := capturados()
	if len(enviados) != 1 || enviados[0].Para != emailVerificado {
		t.Fatalf("só a conta elegível (e-mail verificado) deve receber e-mail; recebidos: %+v", enviados)
	}
}

func TestIntegrationSolicitarRecuperacaoSenhaAutoDetectaTipoQuandoOmitido(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)
	capturados := brevoFalso(t)

	_, email := seedAdminParaRecuperacao(t, client, "SenhaAntiga#123", true)

	// Nenhum "tipo" no corpo — auto-detecta como admin (estudante e academia
	// não têm esse e-mail, então a busca cai no admin).
	rec := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{"identificador": email})
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, recebi %d: %s", rec.Code, rec.Body.String())
	}
	if enviados := capturados(); len(enviados) != 1 || enviados[0].Para != email {
		t.Fatalf("a auto-detecção de tipo não enviou o link ao admin: %+v", enviados)
	}
}

// Sem canal de e-mail a resposta é 503 para QUALQUER identificador (não revela
// nada sobre a conta) e nada é gravado.
func TestIntegrationSolicitarRecuperacaoSenhaSemBrevoDevolve503(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)
	t.Setenv("BREVO_API_KEY", "")
	t.Setenv("EMAIL_FROM", "")

	userID, email := seedAdminParaRecuperacao(t, client, "SenhaAntiga#123", true)
	for _, id := range []string{email, "ninguem-" + uuid.NewString() + "@example.test"} {
		rec := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{"identificador": id, "tipo": "admin"})
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: esperava 503, recebi %d: %s", id, rec.Code, rec.Body.String())
		}
	}
	var n int
	if err := client.DB().QueryRow(`SELECT COUNT(*) FROM auth_tokens WHERE user_id = $1 AND tipo = 'recuperacao_senha'`, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("sem canal de e-mail não pode haver token emitido; há %d", n)
	}
}

func TestIntegrationSolicitarRecuperacaoSenhaRejeitaCorpoInvalido(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)
	brevoFalso(t)

	if rec := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{}); rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("corpo sem identificador: esperava 4xx, recebi %d", rec.Code)
	}
	if rec := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{"identificador": "x@y.z", "tipo": "outro"}); rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("tipo inválido: esperava 4xx, recebi %d", rec.Code)
	}
}

// Fluxo completo: pedir → abrir o link → escolher a nova senha. Só aqui a senha
// muda, o token é de uso único e o evento leva o motivo reset_senha (que
// invalida as sessões anteriores — SEC-10).
func TestIntegrationRecuperacaoSenhaPorLinkFluxoCompleto(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)
	capturados := brevoFalso(t)

	userID, email := seedAdminParaRecuperacao(t, client, "SenhaAntiga#123", true)
	if rec := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{"identificador": email, "tipo": "admin"}); rec.Code != http.StatusOK {
		t.Fatalf("solicitar: %d %s", rec.Code, rec.Body.String())
	}
	enviados := capturados()
	if len(enviados) != 1 {
		t.Fatalf("esperava 1 e-mail, recebi %d", len(enviados))
	}
	token := reTokenRecuperacao.FindStringSubmatch(enviados[0].HTML)[1]

	// Senha fraca é recusada e o token continua válido.
	if rec := postJSON(router, http.MethodPost, "/email/recuperar-senha/"+token, map[string]any{"nova_senha": "curta"}); rec.Code == http.StatusOK {
		t.Fatalf("senha curta aceite: %s", rec.Body.String())
	}

	rec := postJSON(router, http.MethodPost, "/email/recuperar-senha/"+token, map[string]any{"nova_senha": "NovaSenhaForte#456"})
	if rec.Code != http.StatusOK {
		t.Fatalf("redefinir: esperava 200, recebi %d: %s", rec.Code, rec.Body.String())
	}
	applyAdminEvents(t, client, userID)

	var hash string
	var deve bool
	var desde sql.NullTime
	if err := client.DB().QueryRow(`SELECT senha_hash, deve_alterar_senha, sessoes_validas_desde FROM projection_admins WHERE id = $1`, userID).Scan(&hash, &deve, &desde); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("NovaSenhaForte#456")) != nil {
		t.Fatal("a nova senha escolhida no link não foi gravada")
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("SenhaAntiga#123")) == nil {
		t.Fatal("a senha antiga continua válida depois da redefinição")
	}
	if deve {
		t.Fatal("deve_alterar_senha tem de ficar FALSE depois de o titular escolher a própria senha")
	}
	if !desde.Valid {
		t.Fatal("reset por link tem de preencher sessoes_validas_desde (invalida sessões anteriores)")
	}

	// Uso único: o mesmo link não funciona uma segunda vez.
	if rec2 := postJSON(router, http.MethodPost, "/email/recuperar-senha/"+token, map[string]any{"nova_senha": "OutraSenhaForte#789"}); rec2.Code == http.StatusOK {
		t.Fatal("o link de recuperação foi aceite uma segunda vez")
	}
}

// Sem Brevo, o pedido de verificação responde 503 (antes devolvia 200 e
// escrevia o link com o token no log) e não persiste nenhum token.
func TestIntegrationSolicitarVerificacaoEmailSemBrevoDevolve503(t *testing.T) {
	client := integrationFinanceClient(t)
	repository := db.NewAggregateRepository(client)
	t.Setenv("BREVO_API_KEY", "")
	t.Setenv("EMAIL_FROM", "")

	userID, email := seedAdminParaRecuperacao(t, client, "SenhaQualquer#123", false)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/email/verificar-email/solicitar", nil)
	ctx.Set("dbClient", client)
	ctx.Set("repository", repository)
	ctx.Set("user_id", userID)
	ctx.Set("user_type", "admin")
	SolicitarVerificacaoEmail(ctx)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("esperava 503, recebi %d: %s", recorder.Code, recorder.Body.String())
	}
	var n int
	if err := client.DB().QueryRow(`SELECT COUNT(*) FROM auth_tokens WHERE email = $1 AND tipo = 'verificacao_email'`, email).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("sem canal de e-mail não pode ficar token de verificação persistido; há %d", n)
	}
}

func TestIntegrationSolicitarVerificacaoEmailGeraTokenPersistido(t *testing.T) {
	client := integrationFinanceClient(t)
	repository := db.NewAggregateRepository(client)
	capturados := brevoFalso(t)

	userID, email := seedAdminParaRecuperacao(t, client, "SenhaQualquer#123", false)

	// SolicitarVerificacaoEmail exige AuthMiddleware (lê user_id/user_type do
	// contexto, não do corpo) — reproduzido via gin.CreateTestContext.
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
	if enviados := capturados(); len(enviados) != 1 || enviados[0].Para != email {
		t.Fatalf("o e-mail de verificação deveria ter ido para %s: %+v", email, enviados)
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
