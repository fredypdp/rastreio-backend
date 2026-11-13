package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/middleware"
	"spuri/internal/services"
)

// Tarefa 123 (SEC-04 / SEC-10) — reenvio de credenciais, invalidação de
// sessões e login genérico, no router real com PostgreSQL real.

// criarAdminCriador cria o admin "raiz" que satisfaz a FK created_by dos demais
// (mesmo padrão de getOrCreateTestCreatorAdmin em internal/handlers).
func criarAdminCriador(t *testing.T, fx *turmaVinculoFixture) uuid.UUID {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("CriadorDeTeste#123"), bcrypt.MinCost)
	tel := "923000111"
	raiz := aggregates.NewAdmin()
	if err := raiz.Criar("Criador de Teste", "criador-"+uuid.NewString()+"@spuri.test", &tel, string(hash), "gerente", nil); err != nil {
		t.Fatal(err)
	}
	if err := fx.repository.SaveWithAudit(raiz, db.AuditContext{UserID: "integration-test", UserType: "sistema", IP: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	aplicarEventosAdmin(t, fx, raiz.ID.String(), 0)
	if _, err := fx.client.DB().Exec(`UPDATE projection_admins SET created_by = $1 WHERE id = $1`, raiz.ID); err != nil {
		t.Fatal(err)
	}
	return raiz.ID
}

func criarAdminParaTeste(t *testing.T, fx *turmaVinculoFixture, criador uuid.UUID, role, senha string) (id, email string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	email = "admin-" + uuid.NewString() + "@spuri.test"
	tel := "9" + geraDigitosTurmaVinculo(8)
	adm := aggregates.NewAdmin()
	if err := adm.Criar("Admin "+role, email, &tel, string(hash), role, &criador); err != nil {
		t.Fatal(err)
	}
	if err := fx.repository.SaveWithAudit(adm, db.AuditContext{UserID: criador.String(), UserType: "admin", IP: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	aplicarEventosAdmin(t, fx, adm.ID.String(), 0)
	// RequireAdmin exige e-mail verificado para o painel administrativo.
	if _, err := fx.client.DB().Exec(`UPDATE projection_admins SET email_verificado = TRUE WHERE id = $1`, adm.ID); err != nil {
		t.Fatal(err)
	}
	return adm.ID.String(), email
}

func tokenDe(t *testing.T, id, tipo string) string {
	t.Helper()
	uid, err := uuid.Parse(id)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := middleware.GenerateToken(uid, tipo)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// cadastrarEstudanteEntregue cadastra um estudante, aplica os eventos e devolve
// id, código e a senha temporária lida do SMS.
func cadastrarEstudanteEntregue(t *testing.T, fx *turmaVinculoFixture, sms *capturaSMS, nome string) (id, codigo, senha string, ultimoEvento int64) {
	t.Helper()
	rec := postCadastro(t, fx, camposCadastro(nome))
	if rec.Code != http.StatusCreated {
		t.Fatalf("cadastro: %d %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec.Body.Bytes())
	id = dataMap(resp)["id"].(string)
	codigo = codigoEstudante(resp)
	ultimoEvento = aplicarEventosEstudante(t, fx, id, 0)
	m := reSenhaNoSMS.FindStringSubmatch(sms.ultimo(t).Conteudo)
	if m == nil {
		t.Fatalf("SMS sem senha: %q", sms.ultimo(t).Conteudo)
	}
	return id, codigo, m[1], ultimoEvento
}

func TestCredenciaisReenvio01EstudantePelaAcademiaInvalidaSessoesEExigeNovaTroca(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)

	id, codigo, senha1, ultimo := cadastrarEstudanteEntregue(t, fx, sms, "Estudante Reenvio Um")
	_, login := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": codigo, "senha": senha1})
	tokenAntigo, _ := login["token"].(string)
	if tokenAntigo == "" {
		t.Fatalf("login inicial falhou: %v", login)
	}
	// O titular troca a senha temporária: a conta passa a uso normal.
	if r, _ := chamarJSON(fx, http.MethodPut, "/alterar-senha", tokenAntigo, map[string]any{"senha_atual": senha1, "nova_senha": "SenhaDoTitular#1"}); r.Code != http.StatusOK {
		t.Fatalf("troca: %d %s", r.Code, r.Body.String())
	}
	ultimo = aplicarEventosEstudante(t, fx, id, ultimo)

	// Outra academia não pode reenviar credenciais de um estudante alheio.
	tokenOutra := tokenDe(t, fx.outraAcademia.ID.String(), "academia")
	if r, _ := chamarJSON(fx, http.MethodPost, "/estudantes/"+codigo+"/credenciais/reenviar", tokenOutra, nil); r.Code != http.StatusForbidden {
		t.Fatalf("academia alheia: esperava 403, recebi %d: %s", r.Code, r.Body.String())
	}
	// Um estudante também não.
	tokenTitular := ""
	if _, l := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": codigo, "senha": "SenhaDoTitular#1"}); l["token"] != nil {
		tokenTitular, _ = l["token"].(string)
	}
	if r, _ := chamarJSON(fx, http.MethodPost, "/estudantes/"+codigo+"/credenciais/reenviar", tokenTitular, nil); r.Code != http.StatusForbidden {
		t.Fatalf("estudante: esperava 403, recebi %d: %s", r.Code, r.Body.String())
	}

	// O token do titular foi emitido antes do reenvio: precisa ser anterior ao
	// segundo do evento (o iat do JWT tem precisão de segundos).
	time.Sleep(1100 * time.Millisecond)

	// A academia dona reenvia.
	antes := sms.total()
	r, corpo := chamarJSON(fx, http.MethodPost, "/estudantes/"+codigo+"/credenciais/reenviar", fx.token, nil)
	if r.Code != http.StatusOK {
		t.Fatalf("reenvio: esperava 200, recebi %d: %s", r.Code, r.Body.String())
	}
	if cred, _ := corpo["credenciais"].(map[string]any); cred["enviada"] != true {
		t.Fatalf("credenciais não enviadas: %v", corpo)
	}
	if sms.total() != antes+1 {
		t.Fatalf("esperava 1 SMS novo, houve %d", sms.total()-antes)
	}
	msg := sms.ultimo(t)
	if !strings.Contains(msg.Conteudo, "novas credenciais") {
		t.Fatalf("texto do reenvio inesperado: %q", msg.Conteudo)
	}
	senha2 := reSenhaNoSMS.FindStringSubmatch(msg.Conteudo)[1]
	if senha2 == senha1 || strings.Contains(r.Body.String(), senha2) {
		t.Fatal("a nova senha tem de ser diferente e nunca voltar na resposta HTTP")
	}
	aplicarEventosEstudante(t, fx, id, ultimo)

	// SEC-10: a sessão anterior deixou de valer; a senha do titular também.
	if r, _ := chamarJSON(fx, http.MethodGet, "/meu-perfil", tokenTitular, nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("sessão anterior ao reenvio deveria ser recusada (401), recebi %d: %s", r.Code, r.Body.String())
	}
	if r, _ := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": codigo, "senha": "SenhaDoTitular#1"}); r.Code != http.StatusUnauthorized {
		t.Fatalf("a senha anterior deveria ter deixado de funcionar, recebi %d", r.Code)
	}
	// SEC-04: a nova senha é temporária de novo.
	_, l2 := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": codigo, "senha": senha2})
	if l2["deve_alterar_senha"] != true {
		t.Fatalf("após o reenvio a conta deve exigir troca de novo: %v", l2)
	}

	// Intervalo mínimo de 10 minutos por conta.
	r, corpo = chamarJSON(fx, http.MethodPost, "/estudantes/"+codigo+"/credenciais/reenviar", fx.token, nil)
	if r.Code != http.StatusTooManyRequests {
		t.Fatalf("segundo reenvio imediato: esperava 429, recebi %d: %s", r.Code, r.Body.String())
	}
	if corpo["retry_after_segundos"] == nil {
		t.Fatalf("a resposta 429 devia indicar retry_after_segundos: %v", corpo)
	}
}

func TestCredenciaisReenvio02EstudanteInativoCompleteATrocaComSessaoRestrita(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)

	id, codigo, senha, _ := cadastrarEstudanteEntregue(t, fx, sms, "Estudante Inativo Temp")
	if _, err := fx.client.DB().Exec(`UPDATE projection_estudantes SET status = 'inativo' WHERE id::text = $1`, id); err != nil {
		t.Fatal(err)
	}
	// Estudante inativo recebe sessão restrita ao financeiro.
	_, login := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": codigo, "senha": senha})
	token, _ := login["token"].(string)
	if token == "" || login["deve_alterar_senha"] != true {
		t.Fatalf("login do inativo: %v", login)
	}
	// Fora da allowlist a sessão restrita continua bloqueada...
	if r, _ := chamarJSON(fx, http.MethodGet, "/dois-fatores", token, nil); r.Code != http.StatusForbidden {
		t.Fatalf("rota fora da allowlist: esperava 403, recebi %d", r.Code)
	}
	// ...mas a troca obrigatória está disponível, para não ficar sem saída.
	if r, _ := chamarJSON(fx, http.MethodPut, "/alterar-senha", token, map[string]any{"senha_atual": senha, "nova_senha": "SenhaDoInativo#1"}); r.Code != http.StatusOK {
		t.Fatalf("troca da senha temporária pelo estudante inativo: esperava 200, recebi %d: %s", r.Code, r.Body.String())
	}
}

func TestCredenciaisReenvio03AcademiaEAdministradorSoParaFPP(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)

	// Canal de e-mail falso para o reenvio do administrador.
	var mu sync.Mutex
	var corpos []string
	brevo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		corpos = append(corpos, string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"messageId":"x"}`))
	}))
	defer brevo.Close()
	anterior := services.BrevoAPIURL
	services.BrevoAPIURL = brevo.URL
	defer func() { services.BrevoAPIURL = anterior }()
	t.Setenv("BREVO_API_KEY", "chave-teste")
	t.Setenv("EMAIL_FROM", "avisos@spuri.test")

	raiz := criarAdminCriador(t, fx)
	fppID, _ := criarAdminParaTeste(t, fx, raiz, "fpp", "SenhaFpp#12345")
	admID, admEmail := criarAdminParaTeste(t, fx, raiz, "adm", "SenhaAdm#12345")
	tokenFPP := tokenDe(t, fppID, "admin")
	tokenADM := tokenDe(t, admID, "admin")

	// ── Academia ────────────────────────────────────────────────────────────
	if _, err := fx.client.DB().Exec(`UPDATE projection_academias SET telefone = '923456789' WHERE id = $1`, fx.academia.ID); err != nil {
		t.Fatal(err)
	}
	codAcademia := fx.academia.CodigoAcademia
	if r, _ := chamarJSON(fx, http.MethodPost, "/dominis/academias/"+codAcademia+"/credenciais/reenviar", tokenADM, nil); r.Code != http.StatusForbidden {
		t.Fatalf("admin não-fpp: esperava 403, recebi %d: %s", r.Code, r.Body.String())
	}
	if r, _ := chamarJSON(fx, http.MethodPost, "/dominis/academias/"+codAcademia+"/credenciais/reenviar", fx.token, nil); r.Code != http.StatusForbidden && r.Code != http.StatusUnauthorized {
		t.Fatalf("a própria academia não pode reenviar as suas credenciais: %d", r.Code)
	}
	r, corpo := chamarJSON(fx, http.MethodPost, "/dominis/academias/"+codAcademia+"/credenciais/reenviar", tokenFPP, nil)
	if r.Code != http.StatusOK {
		t.Fatalf("fpp → academia: esperava 200, recebi %d: %s", r.Code, r.Body.String())
	}
	if cred, _ := corpo["credenciais"].(map[string]any); cred["enviada"] != true {
		t.Fatalf("credenciais da academia não enviadas: %v", corpo)
	}
	if got := sms.ultimo(t).Destinatario; got != "923456789" {
		t.Fatalf("SMS para %q, quer o telefone da academia", got)
	}
	senhaAcademia := reSenhaNoSMS.FindStringSubmatch(sms.ultimo(t).Conteudo)[1]
	aplicarEventosAcademia(t, fx, fx.academia.ID.String(), 0)
	_, la := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": codAcademia, "senha": senhaAcademia})
	if la["token"] == nil || la["deve_alterar_senha"] != true {
		t.Fatalf("login da academia com a senha temporária: %v", la)
	}

	// ── SEC-06 no login da academia por e-mail não verificado ───────────────
	emailAcademia := "academia-" + uuid.NewString() + "@spuri.test"
	if _, err := fx.client.DB().Exec(`UPDATE projection_academias SET email = $1, email_verificado = FALSE WHERE id = $2`, emailAcademia, fx.academia.ID); err != nil {
		t.Fatal(err)
	}
	rErr, errSenha := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": emailAcademia, "senha": "errada-errada"})
	rNao, errNao := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": "nao-existe-" + uuid.NewString() + "@x.test", "senha": "errada-errada"})
	if rErr.Code != http.StatusUnauthorized || rNao.Code != http.StatusUnauthorized || errSenha["message"] != errNao["message"] || errSenha["error"] != errNao["error"] {
		t.Fatalf("senha errada de conta com e-mail não verificado e conta inexistente têm de ser idênticas:\n%s\n%s", rErr.Body.String(), rNao.Body.String())
	}
	if m, _ := errSenha["message"].(string); !strings.Contains(m, "credenciais inválidas") {
		t.Fatalf("mensagem genérica esperada, recebi %q", m)
	}
	rOk, okCorpo := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": emailAcademia, "senha": senhaAcademia})
	if rOk.Code != http.StatusUnauthorized || !strings.Contains(rOk.Body.String(), "e-mail não verificado") {
		t.Fatalf("com a senha certa, o e-mail não verificado deve ser dito: %d %v", rOk.Code, okCorpo)
	}

	// ── Administrador ───────────────────────────────────────────────────────
	if r, _ := chamarJSON(fx, http.MethodPost, "/dominis/administradores/"+admID+"/credenciais/reenviar", tokenADM, nil); r.Code != http.StatusForbidden {
		t.Fatalf("admin não-fpp: esperava 403, recebi %d: %s", r.Code, r.Body.String())
	}
	if r, _ := chamarJSON(fx, http.MethodPost, "/dominis/administradores/"+fppID+"/credenciais/reenviar", tokenFPP, nil); r.Code != http.StatusUnprocessableEntity && r.Code != http.StatusBadRequest {
		t.Fatalf("reenviar para si próprio: esperava 4xx, recebi %d: %s", r.Code, r.Body.String())
	}
	r, corpo = chamarJSON(fx, http.MethodPost, "/dominis/administradores/"+admID+"/credenciais/reenviar", tokenFPP, nil)
	if r.Code != http.StatusOK {
		t.Fatalf("fpp → admin: esperava 200, recebi %d: %s", r.Code, r.Body.String())
	}
	if cred, _ := corpo["credenciais"].(map[string]any); cred["canal"] != "email" || cred["enviada"] != true {
		t.Fatalf("credenciais do admin não enviadas por e-mail: %v", corpo)
	}
	mu.Lock()
	ultimoEmail := corpos[len(corpos)-1]
	mu.Unlock()
	var emailBrevo struct {
		To []struct {
			Email string `json:"email"`
		} `json:"to"`
		Texto string `json:"textContent"`
	}
	if err := json.Unmarshal([]byte(ultimoEmail), &emailBrevo); err != nil || len(emailBrevo.To) != 1 {
		t.Fatalf("payload do Brevo ilegível: %v", err)
	}
	if emailBrevo.To[0].Email != admEmail {
		t.Fatalf("o e-mail devia ir para %s, foi para %s", admEmail, emailBrevo.To[0].Email)
	}
	m := regexp.MustCompile(`SENHA TEMPORÁRIA: (\S+)`).FindStringSubmatch(emailBrevo.Texto)
	if m == nil {
		t.Fatalf("não achei a senha temporária no texto do e-mail: %s", emailBrevo.Texto)
	}
	aplicarEventosAdmin(t, fx, admID, 0)
	_, ladm := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": admEmail, "senha": m[1]})
	if ladm["token"] == nil || ladm["deve_alterar_senha"] != true {
		t.Fatalf("login do admin com a senha do e-mail: %v", ladm)
	}
	if r, _ := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": admEmail, "senha": "SenhaAdm#12345"}); r.Code != http.StatusUnauthorized {
		t.Fatalf("a senha anterior do admin deveria ter deixado de funcionar, recebi %d", r.Code)
	}
}

// As listagens indicam quem ainda não confirmou as credenciais (deve_alterar_senha).
func TestCredenciaisListasIndicamContasComSenhaTemporaria(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	prepararSMS(t, fx, false)

	id, codigo, _, _ := cadastrarEstudanteEntregue(t, fx, &capturaSMS{itens: []smsCapturado{{Conteudo: "Senha temporaria: x Altere"}}}, "Estudante Lista Flag")
	buscar := func() map[string]any {
		r, corpo := chamarJSON(fx, http.MethodGet, "/estudantes?limit=100", fx.token, nil)
		if r.Code != http.StatusOK {
			t.Fatalf("listar estudantes: %d %s", r.Code, r.Body.String())
		}
		itens, _ := corpo["estudantes"].([]any)
		for _, it := range itens {
			m, _ := it.(map[string]any)
			if m["codigo_estudante"] == codigo {
				return m
			}
		}
		t.Fatalf("estudante %s não está na lista", codigo)
		return nil
	}
	if buscar()["deve_alterar_senha"] != true {
		t.Fatal("recém-criado com senha temporária: deve_alterar_senha deveria ser true na lista")
	}
	if _, err := fx.client.DB().Exec(`UPDATE projection_estudantes SET deve_alterar_senha = FALSE WHERE id::text = $1`, id); err != nil {
		t.Fatal(err)
	}
	if buscar()["deve_alterar_senha"] != false {
		t.Fatal("depois de trocada a senha o indicador deveria ser false")
	}

	// Lista de administradores.
	raiz := criarAdminCriador(t, fx)
	fppID, _ := criarAdminParaTeste(t, fx, raiz, "fpp", "SenhaFpp#12345")
	admID, _ := criarAdminParaTeste(t, fx, raiz, "adm", "SenhaAdm#12345")
	if _, err := fx.client.DB().Exec(`UPDATE projection_admins SET deve_alterar_senha = TRUE WHERE id::text = $1`, admID); err != nil {
		t.Fatal(err)
	}
	r, corpo := chamarJSON(fx, http.MethodGet, "/dominis/admin-lista", tokenDe(t, fppID, "admin"), nil)
	if r.Code != http.StatusOK {
		t.Fatalf("admin-lista: %d %s", r.Code, r.Body.String())
	}
	achou := false
	for _, it := range corpo["admins"].([]any) {
		m := it.(map[string]any)
		if m["id"] == admID {
			achou = true
			if m["deve_alterar_senha"] != true {
				t.Fatalf("o admin com senha temporária devia vir marcado: %v", m)
			}
		}
		if m["id"] == fppID && m["deve_alterar_senha"] != false {
			t.Fatalf("o fpp com senha própria não devia vir marcado: %v", m)
		}
	}
	if !achou {
		t.Fatal("admin alvo não apareceu na lista")
	}
}
