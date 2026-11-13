package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"spuri/internal/credmigracao"
	"spuri/internal/services"
)

// Tarefa 123 (plano 3.9 / 5.3) — migração das contas existentes com senha previsível.
func TestMigracaoCredenciaisCorrigeContasComSenhaPrevisivel(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, false)
	_ = sms

	// Estudante "legado": senha = código (como era antes da Tarefa 123).
	rec := postCadastro(t, fx, camposCadastro("Estudante Legado Mig"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("cadastro: %d %s", rec.Code, rec.Body.String())
	}
	legado := decodeMap(t, rec.Body.Bytes())
	legadoID := dataMap(legado)["id"].(string)
	legadoCodigo := codigoEstudante(legado)
	ult := aplicarEventosEstudante(t, fx, legadoID, 0)
	hashLegado, _ := bcrypt.GenerateFromPassword([]byte(legadoCodigo), bcrypt.MinCost)
	if _, err := fx.client.DB().Exec(`UPDATE projection_estudantes SET senha_hash = $1, deve_alterar_senha = FALSE WHERE id::text = $2`, string(hashLegado), legadoID); err != nil {
		t.Fatal(err)
	}
	// Estudante "controle": senha temporária aleatória que NÃO é o código.
	rec2 := postCadastro(t, fx, camposCadastro("Estudante Controle Mig"))
	controle := decodeMap(t, rec2.Body.Bytes())
	controleID := dataMap(controle)["id"].(string)
	aplicarEventosEstudante(t, fx, controleID, 0)
	if _, err := fx.client.DB().Exec(`UPDATE projection_estudantes SET deve_alterar_senha = FALSE WHERE id::text = $1`, controleID); err != nil {
		t.Fatal(err)
	}
	// Academia legado: senha = código da academia.
	hashAca, _ := bcrypt.GenerateFromPassword([]byte(fx.academia.CodigoAcademia), bcrypt.MinCost)
	if _, err := fx.client.DB().Exec(`UPDATE projection_academias SET senha_hash = $1, deve_alterar_senha = FALSE WHERE id = $2`, string(hashAca), fx.academia.ID); err != nil {
		t.Fatal(err)
	}
	// Admin com uma "senha pública".
	raiz := criarAdminCriador(t, fx)
	admID, admEmail := criarAdminParaTeste(t, fx, raiz, "adm", "SenhaPublicaTeste1")

	flag := func(tabela, id string) bool {
		var v bool
		if err := fx.client.DB().QueryRow(`SELECT deve_alterar_senha FROM `+tabela+` WHERE id::text = $1`, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	ledger := func() int {
		var n int
		_ = fx.client.DB().QueryRow(`SELECT count(*) FROM spuri_ledger`).Scan(&n)
		return n
	}
	senhasPublicas := []string{"SenhaPublicaTeste1", "OutraPublica2"}

	// ── Dry-run: conta e NÃO escreve nada ───────────────────────────────────
	antes := ledger()
	rel, err := credmigracao.Executar(context.Background(), fx.client, fx.repository, credmigracao.Opcoes{SenhasPublicasAdmin: senhasPublicas})
	if err != nil {
		t.Fatal(err)
	}
	if rel.EstudantesComSenhaIgualAoCodigo < 1 || rel.AcademiasComSenhaIgualAoCodigo < 1 || rel.AdminsComSenhaPublica < 1 {
		t.Fatalf("o dry-run devia encontrar o estudante, a academia e o admin legados: %+v", rel)
	}
	if rel.EstudantesMarcados+rel.AcademiasMarcadas+rel.AdminsRedefinidos != 0 {
		t.Fatalf("dry-run não pode alterar nada: %+v", rel)
	}
	if ledger() != antes {
		t.Fatalf("dry-run escreveu no ledger (%d → %d)", antes, ledger())
	}

	// ── Passo 1: marcar contas ──────────────────────────────────────────────
	rel, err = credmigracao.Executar(context.Background(), fx.client, fx.repository, credmigracao.Opcoes{MarcarContas: true})
	if err != nil || len(rel.Erros) != 0 {
		t.Fatalf("marcar: %v %v", err, rel)
	}
	if rel.EstudantesMarcados < 1 || rel.AcademiasMarcadas < 1 {
		t.Fatalf("esperava marcar estudante e academia: %+v", rel)
	}
	aplicarEventosEstudante(t, fx, legadoID, ult)
	aplicarEventosAcademia(t, fx, fx.academia.ID.String(), 0)
	if !flag("projection_estudantes", legadoID) || !flag("projection_academias", fx.academia.ID.String()) {
		t.Fatal("as contas com senha = código deviam ficar com deve_alterar_senha = true")
	}
	if flag("projection_estudantes", controleID) {
		t.Fatal("a conta de controle (senha aleatória) não pode ser marcada")
	}
	// Segunda execução: idempotente (já marcadas, nada a fazer).
	rel2, _ := credmigracao.Executar(context.Background(), fx.client, fx.repository, credmigracao.Opcoes{MarcarContas: true})
	if rel2.EstudantesMarcados != 0 || rel2.AcademiasMarcadas != 0 {
		t.Fatalf("a segunda execução deveria ser idempotente: %+v", rel2)
	}

	// ── Admins: sem canal de e-mail não se redefine ─────────────────────────
	t.Setenv("BREVO_API_KEY", "")
	t.Setenv("EMAIL_FROM", "")
	if _, err := credmigracao.Executar(context.Background(), fx.client, fx.repository, credmigracao.Opcoes{RedefinirAdmins: true, SenhasPublicasAdmin: senhasPublicas}); err == nil {
		t.Fatal("sem Brevo a redefinição de admins devia ser recusada")
	}

	// ── Admins: redefinição imediata com e-mail ─────────────────────────────
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
	t.Setenv("BREVO_API_KEY", "chave")
	t.Setenv("EMAIL_FROM", "avisos@spuri.test")

	rel, err = credmigracao.Executar(context.Background(), fx.client, fx.repository, credmigracao.Opcoes{RedefinirAdmins: true, SenhasPublicasAdmin: senhasPublicas})
	if err != nil || rel.AdminsRedefinidos < 1 || len(rel.Erros) != 0 {
		t.Fatalf("redefinir admins: %v %+v", err, rel)
	}
	aplicarEventosAdmin(t, fx, admID, 0)
	if r, _ := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": admEmail, "senha": "SenhaPublicaTeste1"}); r.Code != http.StatusUnauthorized {
		t.Fatalf("a senha pública deveria ter deixado de funcionar, recebi %d", r.Code)
	}
	var novaSenha string
	mu.Lock()
	for _, c := range corpos {
		var e struct {
			To []struct {
				Email string `json:"email"`
			} `json:"to"`
			Texto string `json:"textContent"`
		}
		if json.Unmarshal([]byte(c), &e) == nil && len(e.To) == 1 && e.To[0].Email == admEmail {
			if m := regexp.MustCompile(`SENHA TEMPORÁRIA: (\S+)`).FindStringSubmatch(e.Texto); m != nil {
				novaSenha = m[1]
			}
		}
	}
	mu.Unlock()
	if novaSenha == "" || strings.EqualFold(novaSenha, "SenhaPublicaTeste1") {
		t.Fatalf("o admin devia ter recebido uma senha temporária nova por e-mail (%q)", novaSenha)
	}
	if _, l := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": admEmail, "senha": novaSenha}); l["deve_alterar_senha"] != true {
		t.Fatalf("login do admin com a senha nova: %v", l)
	}
}
