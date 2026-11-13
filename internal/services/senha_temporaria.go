package services

import (
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
)

// Tarefa 123 (SEC-04) — senhas temporárias.
//
// Toda conta criada por terceiros (estudante, academia, administrador) nasce com
// uma senha aleatória de 8 caracteres, de troca obrigatória no primeiro acesso.
// Esta é a ÚNICA fonte de senhas geradas pelo sistema: não existe mais senha
// padrão derivada do código do utilizador nem senha fixa por perfil.

const (
	// TempPasswordLength é o menor tamanho aceite por utils.ValidateSenha (8 a 128).
	TempPasswordLength = 8

	tempUpper    = "ABCDEFGHJKMNPQRSTUVWXYZ" // sem I, L, O (ambíguos)
	tempLower    = "abcdefghjkmnpqrstuvwxyz" // sem i, l, o
	tempDigits   = "23456789"                // sem 0, 1
	tempAlphabet = tempUpper + tempLower + tempDigits
)

// GenerateTempPassword devolve exatamente 8 caracteres alfanuméricos sorteados
// com crypto/rand, com pelo menos uma maiúscula, uma minúscula e um dígito.
// Os caracteres ambíguos (I l O o 0 1) ficam de fora para facilitar a leitura
// da senha num SMS.
func GenerateTempPassword() (string, error) {
	max := big.NewInt(int64(len(tempAlphabet)))
	for {
		b := make([]byte, TempPasswordLength)
		for i := range b {
			n, err := rand.Int(rand.Reader, max)
			if err != nil {
				return "", fmt.Errorf("falha ao gerar senha temporária: %w", err)
			}
			b[i] = tempAlphabet[n.Int64()]
		}
		s := string(b)
		if strings.ContainsAny(s, tempUpper) && strings.ContainsAny(s, tempLower) && strings.ContainsAny(s, tempDigits) {
			return s, nil
		}
	}
}

// nomeCurtoSMSMax limita o nome que entra no SMS para a mensagem caber num
// único segmento GSM-7.
const nomeCurtoSMSMax = 20

// acentosPT transliteram os acentos do português para ASCII. Sem dependência
// externa: o conjunto de letras acentuadas dos nomes angolanos é pequeno.
var acentosPT = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
	"Á", "A", "À", "A", "Â", "A", "Ã", "A", "Ä", "A",
	"É", "E", "È", "E", "Ê", "E", "Ë", "E",
	"Í", "I", "Ì", "I", "Î", "I", "Ï", "I",
	"Ó", "O", "Ò", "O", "Ô", "O", "Õ", "O", "Ö", "O",
	"Ú", "U", "Ù", "U", "Û", "U", "Ü", "U",
	"Ç", "C", "Ñ", "N",
)

// NomeCurtoSMS devolve o primeiro nome em ASCII (sem acentos nem símbolos),
// truncado. Nunca devolve vazio: usa "utilizador" como reserva.
func NomeCurtoSMS(nome string) string {
	semAcentos := acentosPT.Replace(strings.TrimSpace(nome))
	primeiro := ""
	if campos := strings.Fields(semAcentos); len(campos) > 0 {
		primeiro = campos[0]
	}
	var sb strings.Builder
	for _, r := range primeiro {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	curto := sb.String()
	if len(curto) > nomeCurtoSMSMax {
		curto = curto[:nomeCurtoSMSMax]
	}
	if curto == "" {
		return "utilizador"
	}
	return curto
}

// MensagemCredenciaisSMS monta o texto das credenciais. ASCII puro, sem
// acentos, para caber num único segmento GSM-7.
func MensagemCredenciaisSMS(nomeCurto, identificador, senha string) string {
	return fmt.Sprintf("Spuri: conta de %s criada. Usuario: %s Senha temporaria: %s Altere a senha no primeiro acesso.",
		NomeCurtoSMS(nomeCurto), identificador, senha)
}

// MensagemCredenciaisReenviadasSMS é o texto do reenvio de credenciais.
func MensagemCredenciaisReenviadasSMS(nomeCurto, identificador, senha string) string {
	return fmt.Sprintf("Spuri: novas credenciais de %s. Usuario: %s Senha temporaria: %s Altere a senha no primeiro acesso.",
		NomeCurtoSMS(nomeCurto), identificador, senha)
}

// CredenciaisLogDevAtivo diz se o log de credenciais para testes locais está
// ligado: exige CREDENCIAIS_LOG_DEV=true e NUNCA vale com ENV=production
// (mesmo padrão de DOIS_FATORES_LOG_CODIGO no 2FA).
func CredenciaisLogDevAtivo() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("CREDENCIAIS_LOG_DEV")), "true") &&
		!strings.EqualFold(strings.TrimSpace(os.Getenv("ENV")), "production")
}

// LogCredenciaisDev escreve as credenciais no log SOMENTE quando
// CredenciaisLogDevAtivo() é verdadeiro. É a única exceção à regra de nunca
// registar segredos (SEC-07). Devolve true se registou.
func LogCredenciaisDev(identificador, senha string) bool {
	if !CredenciaisLogDevAtivo() {
		return false
	}
	log.Printf("[CREDENCIAIS-DEV] ⚠️  somente desenvolvimento — usuário: %s senha temporária: %s", identificador, senha)
	return true
}
