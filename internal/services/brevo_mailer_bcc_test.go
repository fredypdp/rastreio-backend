package services

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Tarefa 127 (SEC-08): o aviso de nova academia vai num ÚNICO e-mail Brevo, com o
// primeiro admin em "to" e os restantes em "bcc" (ninguém vê o endereço dos outros).
func TestSendBrevoEmailWithBCCEnviaUmSoPedidoComBcc(t *testing.T) {
	var pedidos int
	var corpo map[string]any
	var apiKey string
	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pedidos++
		apiKey = r.Header.Get("api-key")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &corpo)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"messageId":"<x@brevo>"}`))
	}))
	defer servidor.Close()

	anterior := BrevoAPIURL
	BrevoAPIURL = servidor.URL
	defer func() { BrevoAPIURL = anterior }()

	cfg := BrevoConfig{APIKey: "xkeysib-teste", SenderEmail: "spuri@example.com", SenderName: "Spuri"}
	err := SendBrevoEmailWithBCC(cfg, "um@example.com", "Admin Um", "Assunto", "texto", "<p>html</p>",
		[]string{"dois@example.com", "", "tres@example.com"})
	if err != nil {
		t.Fatalf("envio falhou: %v", err)
	}
	if pedidos != 1 {
		t.Fatalf("esperava 1 pedido ao Brevo, obteve %d", pedidos)
	}
	if apiKey != "xkeysib-teste" {
		t.Fatalf("header api-key = %q", apiKey)
	}
	to, _ := corpo["to"].([]any)
	if len(to) != 1 || to[0].(map[string]any)["email"] != "um@example.com" {
		t.Fatalf("destinatário principal incorreto: %v", corpo["to"])
	}
	bcc, _ := corpo["bcc"].([]any)
	if len(bcc) != 2 {
		t.Fatalf("esperava 2 endereços em bcc (o vazio é ignorado), obteve %v", corpo["bcc"])
	}
	if strings.Contains(corpo["htmlContent"].(string), "dois@example.com") {
		t.Fatal("o corpo não pode revelar os endereços em cópia oculta")
	}
}

func TestSendBrevoEmailWithBCCDevolveErroDaAPI(t *testing.T) {
	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"invalid_parameter","message":"sender inválido"}`))
	}))
	defer servidor.Close()

	anterior := BrevoAPIURL
	BrevoAPIURL = servidor.URL
	defer func() { BrevoAPIURL = anterior }()

	cfg := BrevoConfig{APIKey: "k", SenderEmail: "spuri@example.com", SenderName: "Spuri"}
	err := SendBrevoEmailWithBCC(cfg, "um@example.com", "Admin", "A", "t", "<p>h</p>", []string{"dois@example.com"})
	if err == nil || !strings.Contains(err.Error(), "sender inválido") {
		t.Fatalf("esperava erro da API com a mensagem do Brevo, obteve %v", err)
	}
}

// Sem cópias ocultas o envio cai no caminho simples (1 destinatário).
func TestSendBrevoEmailWithBCCSemCopiasUsaEnvioSimples(t *testing.T) {
	var corpo map[string]any
	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &corpo)
		w.WriteHeader(http.StatusCreated)
	}))
	defer servidor.Close()

	anterior := BrevoAPIURL
	BrevoAPIURL = servidor.URL
	defer func() { BrevoAPIURL = anterior }()

	cfg := BrevoConfig{APIKey: "k", SenderEmail: "spuri@example.com", SenderName: "Spuri"}
	if err := SendBrevoEmailWithBCC(cfg, "um@example.com", "Admin", "A", "t", "<p>h</p>", nil); err != nil {
		t.Fatal(err)
	}
	if _, temBcc := corpo["bcc"]; temBcc {
		t.Fatalf("não devia haver bcc: %v", corpo["bcc"])
	}
}
