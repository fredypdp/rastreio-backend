package apoio

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/projections"
)

// Plano de Atualizações, Partes 4 e 5 — utilitários das contas de secretário,
// professor e encarregado.

// GetContasProjection devolve a projeção das contas de secretário, professor e encarregado.
func GetContasProjection(c *gin.Context) *projections.ContasProjection {
	return projections.NewContasProjection(GetDbClient(c))
}

// EmailEmUso diz se o e-mail já pertence a outra conta do sistema (qualquer
// tipo). ignorarID é a conta que está a alterar o próprio e-mail. O e-mail é
// identificador de login, por isso é único entre TODOS os tipos de conta.
func EmailEmUso(c *gin.Context, email, ignorarID string) bool {
	email = strings.TrimSpace(email)
	if email == "" {
		return false
	}
	if e, _ := GetEstudanteProjection(c).GetByEmail(email); e != nil && e.ID.String() != ignorarID {
		return true
	}
	if a, _ := GetAcademiaProjection(c).GetByEmail(email); a != nil && a.ID.String() != ignorarID {
		return true
	}
	if a, _ := GetAdminProjection(c).GetByEmail(email); a != nil && a.ID.String() != ignorarID {
		return true
	}
	if p, _ := GetContasProjection(c).GetByEmail(email); p != nil && p.ID.String() != ignorarID {
		return true
	}
	return false
}

// TelefoneDestinoCredenciaisConta valida e normaliza (9XXXXXXXX) o telefone
// que recebe as credenciais de uma conta nova. quem é o texto da mensagem
// ("do secretário", "do professor", "do encarregado").
func TelefoneDestinoCredenciaisConta(telefone, quem string) (string, error) {
	if strings.TrimSpace(telefone) == "" {
		return "", fmt.Errorf("telefone %s é obrigatório: é para ele que as credenciais são enviadas", quem)
	}
	destino, err := normalizarDestinatarioComunicacao(telefone)
	if err != nil {
		return "", fmt.Errorf("telefone %s deve ser um número móvel angolano de 9 dígitos iniciado por 9 (ex.: 923456789), pois recebe as credenciais por SMS", quem)
	}
	return destino, nil
}

// prefixosCodigoConta: duas letras + seis dígitos. Não colide com o código do
// estudante (3 letras + 4 dígitos) nem com o da academia (província + ano + seq).
var prefixosCodigoConta = map[string]string{
	"secretario":  "SC",
	"professor":   "PF",
	"encarregado": "EN",
}

// GerarCodigoConta gera um código de login único para o tipo de conta. O índice
// único de projection_contas_pessoa é a guarda definitiva; aqui só se reduz a
// chance de colisão.
func GerarCodigoConta(c *gin.Context, tipo string) (string, error) {
	prefixo, ok := prefixosCodigoConta[tipo]
	if !ok {
		return "", fmt.Errorf("tipo de conta inválido: %s", tipo)
	}
	proj := GetContasProjection(c)
	for i := 0; i < 50; i++ {
		codigo := fmt.Sprintf("%s%06d", prefixo, rand.Intn(1000000))
		existe, err := proj.CodigoExiste(codigo)
		if err != nil {
			return "", err
		}
		if !existe {
			return codigo, nil
		}
	}
	return "", fmt.Errorf("não foi possível gerar um código único")
}

// ParseDataNascimento aceita "YYYY-MM-DD" (formato dos formulários) ou RFC3339.
func ParseDataNascimento(valor string) (time.Time, error) {
	valor = strings.TrimSpace(valor)
	if t, err := time.Parse("2006-01-02", valor); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, valor); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("data_nascimento inválida: use o formato AAAA-MM-DD")
}

// AuditoriaDaRequisicao monta o AuditContext da conta em nome de quem se age
// (a efetiva). O ator real é gravado pelo repositório (GetRepository).
func AuditoriaDaRequisicao(c *gin.Context, userID uuid.UUID, userType string) db.AuditContext {
	return db.AuditContext{UserID: userID.String(), UserType: userType, IP: c.ClientIP()}
}
