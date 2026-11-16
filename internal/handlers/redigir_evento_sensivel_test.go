package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	"spuri/internal/db"
)

// Tarefa 127 (SEC-14): a redação não depende do tipo do evento nem do nível do campo.
func TestRedigirEventoSensivelGenerica(t *testing.T) {
	const hash = "$2a$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ01234"
	casos := map[string]string{
		"tipo conhecido":          `{"CodigoEstudante":"E1","SenhaHash":"` + hash + `"}`,
		"tipo novo, outro nome":   `{"Nome":"Ana","PasswordHash":"` + hash + `"}`,
		"snake_case":              `{"nova_senha_hash":"` + hash + `","outro":1}`,
		"aninhado em objeto":      `{"conta":{"credenciais":{"senhaHash":"` + hash + `"}}}`,
		"dentro de lista":         `{"itens":[{"ok":true},{"hash_senha_hash":"` + hash + `"}]}`,
		"senha em claro":          `{"Senha":"abc12345","Nome":"Ana"}`,
		"payload que é uma lista": `[{"SenhaHash":"` + hash + `"}]`,
	}
	for nome, payload := range casos {
		original := db.Event{EventType: "EventoQualquerNovo", Payload: json.RawMessage(payload)}
		redigido := redigirEventoSensivel(&original)
		saida := string(redigido.Payload)
		if strings.Contains(saida, "$2a$") || strings.Contains(saida, "abc12345") {
			t.Errorf("%s: o payload redigido ainda contém o segredo: %s", nome, saida)
		}
		if !strings.Contains(saida, "[REDACTED]") {
			t.Errorf("%s: esperava [REDACTED] no payload: %s", nome, saida)
		}
		if string(original.Payload) != payload {
			t.Errorf("%s: o evento original não pode ser alterado", nome)
		}
	}
}

func TestRedigirEventoSensivelPreservaOsCamposNormais(t *testing.T) {
	original := db.Event{EventType: "EstudanteCriadoComVinculo", Payload: json.RawMessage(`{"CodigoEstudante":"E1","Nome":"Ana","SenhaHash":"$2a$10$x","Notas":[1,2]}`)}
	var saida map[string]any
	if err := json.Unmarshal(redigirEventoSensivel(&original).Payload, &saida); err != nil {
		t.Fatal(err)
	}
	if saida["CodigoEstudante"] != "E1" || saida["Nome"] != "Ana" || saida["SenhaHash"] != "[REDACTED]" {
		t.Fatalf("campos normais devem ficar intactos: %v", saida)
	}
	if lista, ok := saida["Notas"].([]any); !ok || len(lista) != 2 {
		t.Fatalf("a lista devia ficar intacta: %v", saida["Notas"])
	}
}

// Payload ilegível: falha fechada (nada do conteúdo original é devolvido).
func TestRedigirEventoSensivelPayloadIlegivelFalhaFechada(t *testing.T) {
	original := db.Event{EventType: "X", Payload: json.RawMessage(`{"SenhaHash":"$2a$10$x"`)}
	saida := string(redigirEventoSensivel(&original).Payload)
	if strings.Contains(saida, "$2a$") {
		t.Fatalf("payload ilegível não pode ser devolvido: %s", saida)
	}
}

func TestRedigirEventoSensivelNilEVazio(t *testing.T) {
	if redigirEventoSensivel(nil) != nil {
		t.Fatal("nil deve continuar nil")
	}
	vazio := &db.Event{EventType: "X"}
	if redigirEventoSensivel(vazio) != vazio {
		t.Fatal("evento sem payload deve ser devolvido tal como está")
	}
}
