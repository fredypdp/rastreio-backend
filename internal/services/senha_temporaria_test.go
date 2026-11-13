package services

import (
	"strings"
	"testing"
)

func TestGenerateTempPasswordFormato(t *testing.T) {
	vistas := map[string]bool{}
	for i := 0; i < 3000; i++ {
		s, err := GenerateTempPassword()
		if err != nil {
			t.Fatal(err)
		}
		if len(s) != 8 {
			t.Fatalf("tamanho %d, quer exatamente 8: %q", len(s), s)
		}
		for _, r := range s {
			if !strings.ContainsRune(tempAlphabet, r) {
				t.Fatalf("caractere %q fora do alfabeto em %q", r, s)
			}
		}
		if !strings.ContainsAny(s, tempUpper) || !strings.ContainsAny(s, tempLower) || !strings.ContainsAny(s, tempDigits) {
			t.Fatalf("faltam classes obrigatórias (maiúscula, minúscula, dígito) em %q", s)
		}
		if strings.ContainsAny(s, "IlOo01") {
			t.Fatalf("a senha %q contém caractere ambíguo", s)
		}
		vistas[s] = true
	}
	if len(vistas) < 2990 {
		t.Fatalf("senhas pouco aleatórias: %d únicas em 3000", len(vistas))
	}
}

func TestGenerateTempPasswordPassaNaValidacaoDeSenha(t *testing.T) {
	// 8 é o mínimo aceite por utils.ValidateSenha (8 a 128): a senha gerada
	// tem de poder ser trocada/validada pelo mesmo critério.
	if TempPasswordLength != 8 {
		t.Fatalf("TempPasswordLength = %d, quer 8", TempPasswordLength)
	}
}

func TestNomeCurtoSMS(t *testing.T) {
	casos := map[string]string{
		"João Paulo da Silva":             "Joao",
		"  Conceição Mendes ":             "Conceicao",
		"Ñandú-Ça":                        "NanduCa",
		"":                                "utilizador",
		"***":                             "utilizador",
		"Anastasiaaaaaaaaaaaaaaaaaaaaaaa": "Anastasiaaaaaaaaaaaa",
	}
	for in, want := range casos {
		if got := NomeCurtoSMS(in); got != want {
			t.Errorf("NomeCurtoSMS(%q) = %q, quer %q", in, got, want)
		}
	}
}

func TestMensagemCredenciaisSMSAsciiEUmSegmento(t *testing.T) {
	msg := MensagemCredenciaisSMS("João Ç", "ABC1234", "Ab3dEf7h")
	for _, r := range msg {
		if r > 127 {
			t.Fatalf("o SMS tem caractere não ASCII %q: %s", r, msg)
		}
	}
	if len(msg) > 160 {
		t.Fatalf("o SMS tem %d caracteres (> 160, mais de um segmento GSM-7): %s", len(msg), msg)
	}
	for _, parte := range []string{"Joao", "ABC1234", "Ab3dEf7h", "Altere a senha"} {
		if !strings.Contains(msg, parte) {
			t.Errorf("o SMS não contém %q: %s", parte, msg)
		}
	}
	if re := MensagemCredenciaisReenviadasSMS("Ana", "LDA2026A001", "Ab3dEf7h"); len(re) > 160 || !strings.Contains(re, "novas credenciais") {
		t.Errorf("SMS de reenvio inesperado (%d): %s", len(re), re)
	}
}

func TestCredenciaisLogDevNuncaEmProducao(t *testing.T) {
	casos := []struct {
		env, flag string
		quer      bool
	}{
		{"development", "true", true},
		{"test", "TRUE", true},
		{"development", "", false},
		{"development", "false", false},
		{"production", "true", false}, // a flag é ignorada em produção
		{"Production", "true", false},
		{"", "true", true}, // sem ENV não é produção
	}
	for _, c := range casos {
		t.Setenv("ENV", c.env)
		t.Setenv("CREDENCIAIS_LOG_DEV", c.flag)
		if got := CredenciaisLogDevAtivo(); got != c.quer {
			t.Errorf("ENV=%q CREDENCIAIS_LOG_DEV=%q → %v, quer %v", c.env, c.flag, got, c.quer)
		}
	}
}
