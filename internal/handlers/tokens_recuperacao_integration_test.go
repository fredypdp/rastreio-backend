package handlers

import (
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// Tarefa 127 (SEC-03/SEC-09): ciclo de vida dos links de recuperação de senha.

// Um novo pedido invalida o link anterior: só o mais recente funciona.
func TestIntegrationRecuperacaoNovoPedidoInvalidaOLinkAnterior(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)
	capturados := brevoFalso(t)
	userID, email := seedAdminParaRecuperacao(t, client, "SenhaAntiga#123", true)

	for i := 0; i < 2; i++ {
		if rec := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{"identificador": email, "tipo": "admin"}); rec.Code != http.StatusOK {
			t.Fatalf("solicitar %d: %d", i, rec.Code)
		}
	}
	envios := capturados()
	if len(envios) != 2 {
		t.Fatalf("esperava 2 e-mails, obteve %d", len(envios))
	}
	antigo := reTokenRecuperacao.FindStringSubmatch(envios[0].HTML)[1]
	recente := reTokenRecuperacao.FindStringSubmatch(envios[1].HTML)[1]

	var pendentes int
	_ = client.DB().QueryRow(`SELECT COUNT(*) FROM auth_tokens WHERE user_id = $1 AND tipo = 'recuperacao_senha' AND usado = FALSE`, userID).Scan(&pendentes)
	if pendentes != 1 {
		t.Fatalf("só deve existir 1 token pendente por utilizador e tipo, existem %d", pendentes)
	}
	if rec := postJSON(router, http.MethodPost, "/email/recuperar-senha/"+antigo, map[string]any{"nova_senha": "SenhaDoAtacante#99"}); rec.Code == http.StatusOK {
		t.Fatal("o link anterior ao último pedido não pode redefinir a senha")
	}
	if rec := postJSON(router, http.MethodPost, "/email/recuperar-senha/"+recente, map[string]any{"nova_senha": "NovaSenhaLegitima#1"}); rec.Code != http.StatusOK {
		t.Fatalf("o link mais recente devia funcionar: %d %s", rec.Code, rec.Body.String())
	}
}

// Depois de um reset concluído nenhum outro token de recuperação do utilizador fica válido,
// mesmo um que tenha sobrevivido por outra via (aqui: inserido à mão).
func TestIntegrationRecuperacaoResetInvalidaTokensPendentes(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)
	capturados := brevoFalso(t)
	userID, email := seedAdminParaRecuperacao(t, client, "SenhaAntiga#123", true)

	if rec := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{"identificador": email, "tipo": "admin"}); rec.Code != http.StatusOK {
		t.Fatalf("solicitar: %d", rec.Code)
	}
	tok := reTokenRecuperacao.FindStringSubmatch(capturados()[0].HTML)[1]
	if _, err := client.DB().Exec(`INSERT INTO auth_tokens (token, user_id, user_type, tipo, email, expires_at, usado)
		VALUES ($1, $2, 'admin', 'recuperacao_senha', $3, now() + interval '1 hour', FALSE)`, uuid.NewString()+"legado", userID, email); err != nil {
		t.Fatal(err)
	}
	if rec := postJSON(router, http.MethodPost, "/email/recuperar-senha/"+tok, map[string]any{"nova_senha": "NovaSenhaLegitima#1"}); rec.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body.String())
	}
	var pendentes int
	_ = client.DB().QueryRow(`SELECT COUNT(*) FROM auth_tokens WHERE user_id = $1 AND tipo = 'recuperacao_senha' AND usado = FALSE`, userID).Scan(&pendentes)
	if pendentes != 0 {
		t.Fatalf("depois do reset não pode restar token pendente, restam %d", pendentes)
	}
}

// Uso único sob concorrência: vários pedidos simultâneos com o MESMO token → um só sucesso.
func TestIntegrationRecuperacaoTokenDeUsoUnicoSobConcorrencia(t *testing.T) {
	client := integrationFinanceClient(t)
	router := routerParaAuthEmail(client)
	capturados := brevoFalso(t)
	_, email := seedAdminParaRecuperacao(t, client, "SenhaAntiga#123", true)
	if rec := postJSON(router, http.MethodPost, "/email/recuperar-senha/solicitar", map[string]any{"identificador": email, "tipo": "admin"}); rec.Code != http.StatusOK {
		t.Fatalf("solicitar: %d", rec.Code)
	}
	tok := reTokenRecuperacao.FindStringSubmatch(capturados()[0].HTML)[1]

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		sucessos int
	)
	inicio := make(chan struct{})
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-inicio
			rec := postJSON(router, http.MethodPost, "/email/recuperar-senha/"+tok, map[string]any{"nova_senha": "SenhaConcorrente#" + string(rune('A'+i)) + "1"})
			if rec.Code == http.StatusOK {
				mu.Lock()
				sucessos++
				mu.Unlock()
			}
		}(i)
	}
	close(inicio)
	wg.Wait()
	if sucessos != 1 {
		t.Fatalf("o token é de uso único: esperava 1 sucesso, obteve %d", sucessos)
	}
}
