package sysconfig

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"spuri/internal/security"
)

// cifraLabel separa a chave de cifra dos segredos de configuração das demais
// chaves derivadas de JWT_SECRET (comunicação, financeiro, dois fatores).
const cifraLabel = "spuri/sistema-config/v1"

func chaveCifra() ([]byte, error) { return security.DeriveKey(cifraLabel) }

// Cifrar cifra o segredo com AES-256-GCM. A chave de configuração entra como
// dado autenticado: um texto cifrado copiado para outra chave não decifra.
func Cifrar(chave, valor string) (string, error) {
	k, err := chaveCifra()
	if err != nil {
		return "", err
	}
	bloco, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(bloco)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	selado := gcm.Seal(nonce, nonce, []byte(valor), []byte(chave))
	return base64.StdEncoding.EncodeToString(selado), nil
}

// Decifrar é o inverso de Cifrar. O erro nunca inclui o texto cifrado.
func Decifrar(chave, cifrado string) (string, error) {
	k, err := chaveCifra()
	if err != nil {
		return "", err
	}
	bruto, err := base64.StdEncoding.DecodeString(cifrado)
	if err != nil {
		return "", errors.New("segredo de configuração corrompido")
	}
	bloco, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(bloco)
	if err != nil {
		return "", err
	}
	if len(bruto) < gcm.NonceSize() {
		return "", errors.New("segredo de configuração corrompido")
	}
	nonce, corpo := bruto[:gcm.NonceSize()], bruto[gcm.NonceSize():]
	claro, err := gcm.Open(nil, nonce, corpo, []byte(chave))
	if err != nil {
		return "", fmt.Errorf("não foi possível decifrar o segredo %s (JWT_SECRET mudou?)", chave)
	}
	return string(claro), nil
}
