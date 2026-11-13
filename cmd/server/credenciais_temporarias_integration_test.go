package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"spuri/internal/db"
	"spuri/internal/handlers"
	"spuri/internal/middleware"
	"spuri/internal/projections"
)

// Tarefa 123 (SEC-04) — ponta a ponta, com o router real (setupRouter),
// PostgreSQL real e o caminho real de event sourcing: cadastro de estudante →
// senha temporária por SMS → login → bloqueio até trocar a senha → troca.

type smsCapturado struct{ Destinatario, Conteudo string }

type capturaSMS struct {
	mu     sync.Mutex
	itens  []smsCapturado
	falhar error
}

func (c *capturaSMS) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.itens)
}

func (c *capturaSMS) ultimo(t *testing.T) smsCapturado {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.itens) == 0 {
		t.Fatal("nenhum SMS foi enviado")
	}
	return c.itens[len(c.itens)-1]
}

// prepararSMS liga um provedor padrão fictício e captura o que iria para o SMS.
// Restaura a configuração global no fim do teste.
func prepararSMS(t *testing.T, fx *turmaVinculoFixture, producao bool) *capturaSMS {
	t.Helper()
	if producao {
		t.Setenv("ENV", "production")
	} else {
		t.Setenv("ENV", "test")
	}
	t.Setenv("CREDENCIAIS_LOG_DEV", "")
	var anterior *string
	if err := fx.client.DB().QueryRow(`SELECT provedor_padrao FROM projection_comunicacao_config WHERE id = 1`).Scan(&anterior); err != nil {
		t.Fatal(err)
	}
	res, err := fx.client.DB().Exec(`
		INSERT INTO projection_remetentes_comunicacao (id, provedor, identificador, token_api_cifrado, configurado_por, configurado_por_tipo, created_at, updated_at)
		VALUES (gen_random_uuid(), 'GOSMS', 'SPURI', 'token-de-teste', $1, 'admin', now(), now())
		ON CONFLICT (provedor) DO NOTHING`, fx.academia.ID)
	if err != nil {
		t.Fatal(err)
	}
	inseriu, _ := res.RowsAffected()
	if _, err := fx.client.DB().Exec(`UPDATE projection_comunicacao_config SET provedor_padrao = 'GOSMS' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	cap := &capturaSMS{}
	restaurar := handlers.SubstituirEnvioSMSParaTeste(func(_ context.Context, _ string, destinatario, conteudo string) (string, error) {
		cap.mu.Lock()
		defer cap.mu.Unlock()
		if cap.falhar != nil {
			return "", cap.falhar
		}
		cap.itens = append(cap.itens, smsCapturado{destinatario, conteudo})
		return "ext-teste", nil
	})
	t.Cleanup(func() {
		restaurar()
		_, _ = fx.client.DB().Exec(`UPDATE projection_comunicacao_config SET provedor_padrao = $1 WHERE id = 1`, anterior)
		if inseriu > 0 {
			_, _ = fx.client.DB().Exec(`DELETE FROM projection_remetentes_comunicacao WHERE provedor = 'GOSMS' AND identificador = 'SPURI' AND token_api_cifrado = 'token-de-teste'`)
		}
	})
	return cap
}

var reSenhaNoSMS = regexp.MustCompile(`Senha temporaria: (\S+) Altere`)

// aplicarEventos leva à projeção os eventos do agregado com id de ledger maior
// que depoisDe (no runtime real é o gerenciador de projeções que o faz; o
// fixture não o executa). Devolve o maior id aplicado.
func aplicarEventos(t *testing.T, fx *turmaVinculoFixture, agregadoID string, depoisDe int64, handle func(db.Event) error) int64 {
	t.Helper()
	id, err := uuid.Parse(agregadoID)
	if err != nil {
		t.Fatalf("id de agregado inválido %q: %v", agregadoID, err)
	}
	rows, err := fx.client.DB().Query(`
		SELECT id, event_id, aggregate_id, aggregate_type, event_type, event_version, payload, metadata,
		       occurred_at, recorded_at, ledger_hash, previous_hash
		  FROM spuri_ledger WHERE aggregate_id = $1 AND id > $2 ORDER BY id ASC`, id, depoisDe)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ultimo := depoisDe
	for rows.Next() {
		var ev db.Event
		var prev sql.NullString
		if err := rows.Scan(&ev.ID, &ev.EventID, &ev.AggregateID, &ev.AggregateType, &ev.EventType, &ev.EventVersion,
			&ev.Payload, &ev.Metadata, &ev.OccurredAt, &ev.RecordedAt, &ev.LedgerHash, &prev); err != nil {
			t.Fatal(err)
		}
		if prev.Valid {
			ev.PreviousHash = &prev.String
		}
		if err := handle(ev); err != nil {
			t.Fatalf("projeção do evento %s: %v", ev.EventType, err)
		}
		ultimo = ev.ID
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ultimo
}

func aplicarEventosEstudante(t *testing.T, fx *turmaVinculoFixture, id string, depoisDe int64) int64 {
	t.Helper()
	return aplicarEventos(t, fx, id, depoisDe, projections.NewEstudanteProjection(fx.client).Handle)
}

func aplicarEventosAcademia(t *testing.T, fx *turmaVinculoFixture, id string, depoisDe int64) int64 {
	t.Helper()
	return aplicarEventos(t, fx, id, depoisDe, projections.NewAcademiaProjection(fx.client).Handle)
}

func aplicarEventosAdmin(t *testing.T, fx *turmaVinculoFixture, id string, depoisDe int64) int64 {
	t.Helper()
	return aplicarEventos(t, fx, id, depoisDe, projections.NewAdminProjection(fx.client).Handle)
}

func totalMensagensRegistadas(t *testing.T, fx *turmaVinculoFixture) int {
	t.Helper()
	var n int
	if err := fx.client.DB().QueryRow(`SELECT count(*) FROM spuri_ledger WHERE event_type = 'MensagemComunicacaoRegistrada'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func chamarJSON(fx *turmaVinculoFixture, method, path, token string, body map[string]any) (*httptest.ResponseRecorder, map[string]any) {
	var buf bytes.Buffer
	if body != nil {
		raw, _ := json.Marshal(body)
		buf.Write(raw)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestCredenciaisTemporarias01CadastroEnviaSenhaAleatoriaPorSMSEExigeTroca(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)

	campos := camposCadastro("Estudante Credencial Um")
	rec := postCadastro(t, fx, campos)
	if rec.Code != http.StatusCreated {
		t.Fatalf("cadastro: esperava 201, recebi %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec.Body.Bytes())
	codigo := codigoEstudante(resp)
	ultimoEvento := aplicarEventosEstudante(t, fx, dataMap(resp)["id"].(string), 0)
	cred, _ := dataMap(resp)["credenciais"].(map[string]any)
	if cred["canal"] != "sms" || cred["enviada"] != true {
		t.Fatalf("a resposta deveria trazer credenciais enviadas por sms: %v", cred)
	}

	// SMS: foi para o telefone do encarregado, em ASCII, com usuário e senha.
	msg := sms.ultimo(t)
	if msg.Destinatario != campos["telefone_encarregado"] {
		t.Fatalf("SMS enviado para %q, quer o telefone do encarregado %q", msg.Destinatario, campos["telefone_encarregado"])
	}
	m := reSenhaNoSMS.FindStringSubmatch(msg.Conteudo)
	if m == nil || !strings.Contains(msg.Conteudo, "Usuario: "+codigo) {
		t.Fatalf("SMS sem usuário/senha esperados: %q", msg.Conteudo)
	}
	senha := m[1]
	if len(senha) != 8 || senha == codigo {
		t.Fatalf("a senha temporária deve ter 8 caracteres e ser diferente do código: %q", senha)
	}
	// A senha em claro NUNCA volta na resposta HTTP.
	if strings.Contains(rec.Body.String(), senha) {
		t.Fatal("a resposta do cadastro expõe a senha temporária")
	}

	// A antiga "senha = código" deixou de funcionar; a temporária funciona.
	if r, _ := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": codigo, "senha": codigo}); r.Code != http.StatusUnauthorized {
		t.Fatalf("login com senha = código deveria falhar (401), recebi %d: %s", r.Code, r.Body.String())
	}
	r, login := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": codigo, "senha": senha})
	if r.Code != http.StatusOK {
		t.Fatalf("login com a senha temporária: %d %s", r.Code, r.Body.String())
	}
	if login["deve_alterar_senha"] != true {
		t.Fatalf("o login deveria sinalizar deve_alterar_senha=true: %v", login)
	}
	token, _ := login["token"].(string)

	// Bloqueio no servidor: nada além de perfil, trocar senha e logout.
	r, corpo := chamarJSON(fx, http.MethodGet, "/dois-fatores", token, nil)
	if r.Code != http.StatusForbidden || corpo["code"] != middleware.CodigoSenhaTemporaria {
		t.Fatalf("rota fora da allowlist: esperava 403 SENHA_TEMPORARIA, recebi %d: %s", r.Code, r.Body.String())
	}
	if r, _ := chamarJSON(fx, http.MethodGet, "/meu-perfil", token, nil); r.Code != http.StatusOK {
		t.Fatalf("/meu-perfil tem de funcionar com senha temporária: %d %s", r.Code, r.Body.String())
	}

	// Troca obrigatória: igual à atual é recusada; uma nova é aceite.
	if r, _ := chamarJSON(fx, http.MethodPut, "/alterar-senha", token, map[string]any{"senha_atual": senha, "nova_senha": senha}); r.Code != http.StatusUnprocessableEntity && r.Code != http.StatusBadRequest {
		t.Fatalf("nova senha igual à atual deveria ser recusada, recebi %d: %s", r.Code, r.Body.String())
	}
	if r, _ := chamarJSON(fx, http.MethodPut, "/alterar-senha", token, map[string]any{"senha_atual": senha, "nova_senha": "MinhaSenhaNova#9"}); r.Code != http.StatusOK {
		t.Fatalf("troca da senha temporária: %d %s", r.Code, r.Body.String())
	}
	// O evento de troca de senha chega à projeção como no runtime real.
	aplicarEventosEstudante(t, fx, dataMap(resp)["id"].(string), ultimoEvento)
	if r, _ := chamarJSON(fx, http.MethodGet, "/dois-fatores", token, nil); r.Code != http.StatusOK {
		t.Fatalf("depois de trocar a senha o acesso deveria abrir; recebi %d: %s", r.Code, r.Body.String())
	}
	_, login2 := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": codigo, "senha": "MinhaSenhaNova#9"})
	if login2["deve_alterar_senha"] != false {
		t.Fatalf("depois da troca deve_alterar_senha deveria ser false: %v", login2)
	}
}

func TestCredenciaisTemporarias02TelefoneDeDestinoInvalidoNaoCriaConta(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)

	campos := camposCadastro("Estudante Telefone Fixo")
	campos["telefone_encarregado"] = "222123456" // fixo: 9 dígitos, mas não é móvel
	rec := postCadastro(t, fx, campos)
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 4xx para telefone de destino não móvel, recebi %d: %s", rec.Code, rec.Body.String())
	}
	if n := estudanteCount(t, fx, "Estudante Telefone Fixo"); n != 0 {
		t.Fatalf("a conta foi criada (%d) apesar de as credenciais não poderem ser entregues", n)
	}
	if sms.total() != 0 {
		t.Fatal("não deveria haver SMS")
	}
}

func TestCredenciaisTemporarias03FalhaDeSMSNaoDesfazACriacao(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)
	sms.falhar = errors.New("provedor fora do ar")

	rec := postCadastro(t, fx, camposCadastro("Estudante Sms Falha"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("falha de SMS não pode impedir a criação: %d %s", rec.Code, rec.Body.String())
	}
	respFalha := decodeMap(t, rec.Body.Bytes())
	aplicarEventosEstudante(t, fx, dataMap(respFalha)["id"].(string), 0)
	cred, _ := dataMap(respFalha)["credenciais"].(map[string]any)
	if cred["enviada"] != false || cred["motivo"] != "falha_envio" {
		t.Fatalf("esperava credenciais {enviada:false, motivo:falha_envio}, recebi %v", cred)
	}
	if n := estudanteCount(t, fx, "Estudante Sms Falha"); n != 1 {
		t.Fatalf("a conta devia existir apesar da falha de SMS; encontrei %d", n)
	}
}

func TestCredenciaisTemporarias04ForaDeProducaoNaoEnviaNemRegistaMensagem(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, false)
	antes := totalMensagensRegistadas(t, fx)

	rec := postCadastro(t, fx, camposCadastro("Estudante Fora Producao"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("cadastro: %d %s", rec.Code, rec.Body.String())
	}
	cred, _ := dataMap(decodeMap(t, rec.Body.Bytes()))["credenciais"].(map[string]any)
	if cred["enviada"] != false || cred["motivo"] != "ambiente_nao_producao" {
		t.Fatalf("esperava motivo ambiente_nao_producao, recebi %v", cred)
	}
	if sms.total() != 0 {
		t.Fatal("fora de produção nenhum SMS pode ser enviado")
	}
	if depois := totalMensagensRegistadas(t, fx); depois != antes {
		t.Fatalf("fora de produção nenhuma mensagem pode ser registada (%d → %d)", antes, depois)
	}
}
