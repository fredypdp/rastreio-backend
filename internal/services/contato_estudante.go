package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"spuri/internal/utils"
)

// Plano de Atualizações, Parte 5 — REGRA ÚNICA DE CONTATO.
//
// Dado um estudante, devolve o telefone e o e-mail certos:
//
//	nível escolar  → o do ENCARREGADO (o estudante é menor de idade);
//	superior       → o do próprio estudante.
//
// É a única função usada pelas credenciais, pelas notificações (Parte 17) e
// pelos SMS (Partes 22 e 23). Nenhum outro código decide para onde vai uma
// mensagem do estudante. Em particular, para o nível escolar NUNCA cai no
// telefone do estudante, mesmo que o do encarregado falte: devolve erro, e
// quem chama mostra o erro à academia (que reenvia ou corrige).

// Origens possíveis do contato.
const (
	OrigemContatoEstudante   = "estudante"
	OrigemContatoEncarregado = "encarregado"
)

// ErrSemContatoDoEncarregado: estudante do nível escolar sem encarregado ativo.
var ErrSemContatoDoEncarregado = errors.New("o estudante do nível escolar não tem encarregado com contato válido: informe ou troque o encarregado")

// ErrEstudanteSemNivel: o estudante não está em nenhum nível.
var ErrEstudanteSemNivel = errors.New("o estudante não tem nível (escolar ou superior) definido: não há contato para enviar a mensagem")

// ContatoPessoa é o contato bruto de uma pessoa.
type ContatoPessoa struct {
	Nome            string
	Telefone        string
	Email           *string
	EmailVerificado bool
}

// ContatoEstudante é o contato resolvido.
type ContatoEstudante struct {
	Origem          string  `json:"origem"`
	Nome            string  `json:"nome"`
	Telefone        string  `json:"telefone"`
	Email           *string `json:"email,omitempty"`
	EmailVerificado bool    `json:"email_verificado"`
}

// PerfilNivelEstudante resume o que a regra precisa saber do estudante.
type PerfilNivelEstudante struct {
	AnoFundamental, AnoMedio, AnoSuperior *string
	StatusFundamental, StatusMedio        string
}

func preenchidoPtr(s *string) bool { return s != nil && strings.TrimSpace(*s) != "" }

// EstudanteEEscolar diz se, para efeito de contato, o estudante está no nível
// escolar: tem ano/status escolar e AINDA não está no superior. Quem está no
// superior responde por si, mesmo que tenha passado pelo escolar.
func EstudanteEEscolar(p PerfilNivelEstudante) bool {
	if preenchidoPtr(p.AnoSuperior) {
		return false
	}
	return utils.TemNivelEscolar(p.AnoFundamental, p.AnoMedio, p.StatusFundamental, p.StatusMedio)
}

func normalizarContato(c ContatoPessoa, quem string) (ContatoEstudante, error) {
	tel, err := utils.NormalizarTelefoneNacional(c.Telefone)
	if err != nil {
		return ContatoEstudante{}, fmt.Errorf("telefone %s inválido: %w", quem, err)
	}
	return ContatoEstudante{Nome: c.Nome, Telefone: tel, Email: c.Email, EmailVerificado: c.EmailVerificado}, nil
}

// ResolverContato aplica a regra. encarregado é o encarregado ATIVO (nil se não
// há). É pura: não toca no banco, por isso serve também no cadastro, antes de
// a conta do estudante existir.
func ResolverContato(perfil PerfilNivelEstudante, estudante ContatoPessoa, encarregado *ContatoPessoa) (ContatoEstudante, error) {
	superior := preenchidoPtr(perfil.AnoSuperior)
	escolar := EstudanteEEscolar(perfil)

	switch {
	case escolar:
		if encarregado == nil {
			// Concluiu o nível escolar e já se desvinculou do encarregado: adulto, responde por si.
			if utils.FinalizouNivelEscolar(perfil.StatusMedio, perfil.AnoSuperior) {
				c, err := normalizarContato(estudante, "do estudante")
				if err != nil {
					return ContatoEstudante{}, err
				}
				c.Origem = OrigemContatoEstudante
				return c, nil
			}
			return ContatoEstudante{}, ErrSemContatoDoEncarregado
		}
		c, err := normalizarContato(*encarregado, "do encarregado")
		if err != nil {
			// Sem queda para o telefone do estudante.
			return ContatoEstudante{}, err
		}
		c.Origem = OrigemContatoEncarregado
		return c, nil
	case superior:
		c, err := normalizarContato(estudante, "do estudante")
		if err != nil {
			return ContatoEstudante{}, err
		}
		c.Origem = OrigemContatoEstudante
		return c, nil
	}
	return ContatoEstudante{}, ErrEstudanteSemNivel
}

// LinhaUnica é o mínimo de *sql.DB / *sqlx.DB de que o resolvedor precisa.
type LinhaUnica interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

// ResolverContatoDoEstudante carrega o estudante e o seu encarregado ativo e
// aplica ResolverContato.
func ResolverContatoDoEstudante(conn LinhaUnica, estudanteID uuid.UUID) (ContatoEstudante, error) {
	var (
		nome                      string
		telefone, email           sql.NullString
		emailVerificado           bool
		anoFund, anoMedio, anoSup sql.NullString
		statusFund, statusMedio   string
	)
	err := conn.QueryRow(`
		SELECT nome, telefone, email, email_verificado,
		       ano_escolar_fundamental, ano_escolar_medio, ano_superior,
		       COALESCE(status_escolar_fundamental,''), COALESCE(status_escolar_medio,'')
		  FROM projection_estudantes WHERE id = $1`, estudanteID).
		Scan(&nome, &telefone, &email, &emailVerificado, &anoFund, &anoMedio, &anoSup, &statusFund, &statusMedio)
	if err != nil {
		if err == sql.ErrNoRows {
			return ContatoEstudante{}, fmt.Errorf("estudante não encontrado")
		}
		return ContatoEstudante{}, err
	}
	ptr := func(n sql.NullString) *string {
		if n.Valid {
			v := n.String
			return &v
		}
		return nil
	}
	perfil := PerfilNivelEstudante{AnoFundamental: ptr(anoFund), AnoMedio: ptr(anoMedio), AnoSuperior: ptr(anoSup), StatusFundamental: statusFund, StatusMedio: statusMedio}
	est := ContatoPessoa{Nome: nome, Telefone: telefone.String, Email: ptr(email), EmailVerificado: emailVerificado}

	var enc *ContatoPessoa
	var encNome, encTel string
	var encEmail sql.NullString
	var encVerificado bool
	err = conn.QueryRow(`
		SELECT c.nome, c.telefone, c.email, c.email_verificado
		  FROM projection_encarregado_educandos x
		  JOIN projection_contas_pessoa c ON c.id = x.encarregado_id
		 WHERE x.estudante_id = $1 AND x.desvinculado_em IS NULL AND c.status = 'ativo'`, estudanteID).
		Scan(&encNome, &encTel, &encEmail, &encVerificado)
	switch {
	case err == nil:
		enc = &ContatoPessoa{Nome: encNome, Telefone: encTel, Email: ptr(encEmail), EmailVerificado: encVerificado}
	case err != sql.ErrNoRows:
		return ContatoEstudante{}, err
	}
	return ResolverContato(perfil, est, enc)
}
