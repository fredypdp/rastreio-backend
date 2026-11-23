package autenticacao

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/handlers/apoio"
	"spuri/internal/services"
	"spuri/internal/utils"
)

// Plano de Atualizações, Parte 4 — o secretário, o professor e o encarregado
// passam pelos MESMOS fluxos de conta que admin, academia e estudante (login,
// perfil, troca de senha, e-mail, telefone, 2FA, recuperação de senha). Como
// têm o mesmo ciclo de vida (aggregates.ContaPessoa), um único caminho genérico
// serve aos três.

// ehContaPessoa diz se o tipo é um dos três novos.
func ehContaPessoa(tipo string) bool {
	return tipo == aggregates.TipoContaSecretario || tipo == aggregates.TipoContaProfessor || tipo == aggregates.TipoContaEncarregado
}

// aggregateTypeDaConta devolve o aggregate_type do ledger para o tipo de conta.
func aggregateTypeDaConta(tipo string) string {
	switch tipo {
	case aggregates.TipoContaSecretario:
		return aggregates.AggregateSecretario
	case aggregates.TipoContaProfessor:
		return aggregates.AggregateProfessor
	case aggregates.TipoContaEncarregado:
		return aggregates.AggregateEncarregado
	}
	return ""
}

// contaPessoaAggregate é o que os fluxos genéricos precisam do agregado.
type contaPessoaAggregate interface {
	aggregates.Aggregate
	AlterarSenhaComMotivo(novaSenhaHash, motivo string) error
	VerificarEmail() error
	AtualizarDados(nome, genero *string, dataNascimento *time.Time, email, telefone *string) error
}

// carregarContaPessoa carrega o agregado do tipo certo.
func carregarContaPessoa(c *gin.Context, tipo string, id uuid.UUID) (contaPessoaAggregate, error) {
	agg, err := apoio.GetRepository(c).Load(id, aggregateTypeDaConta(tipo))
	if err != nil {
		return nil, err
	}
	conta, ok := agg.(contaPessoaAggregate)
	if !ok {
		return nil, fmt.Errorf("tipo de aggregate inesperado")
	}
	return conta, nil
}

// verificarEmailContaPessoa conclui a verificação de e-mail por token.
func verificarEmailContaPessoa(c *gin.Context, info *services.TokenInfo) {
	repo := apoio.GetRepository(c)
	conta, err := carregarContaPessoa(c, info.UserType, info.UserID)
	if err != nil {
		utils.RespondWithNotFoundError(c, info.UserType)
		return
	}
	jaVerificado := false
	if err := conta.VerificarEmail(); err != nil {
		if err.Error() != "email já verificado" {
			utils.RespondWithInternalError(c, err)
			return
		}
		jaVerificado = true
	}
	if !jaVerificado {
		audit := db.AuditContext{UserID: info.UserID.String(), UserType: info.UserType, IP: c.ClientIP()}
		if err := repo.SaveWithAudit(conta, audit); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
	}
	msg := "Email verificado com sucesso!"
	if jaVerificado {
		msg = "Email já estava verificado."
	}
	c.JSON(http.StatusOK, gin.H{"message": msg, "email": info.Email})
}

// emailEnomeDaConta devolve e-mail e nome para o envio da verificação.
func emailEnomeDaConta(c *gin.Context, tipo string, id uuid.UUID) (email, nome string, err error) {
	err = apoio.GetDbClient(c).DB().QueryRow(
		`SELECT COALESCE(email,''), nome FROM projection_contas_pessoa WHERE id = $1 AND tipo = $2 AND status <> 'deletado'`,
		id, tipo).Scan(&email, &nome)
	return
}

// buscarContaParaRecuperacao procura a conta (por código ou e-mail) para a
// recuperação de senha. Devolve ok=false se não existe.
func buscarContaParaRecuperacao(c *gin.Context, tipo, identificador string) (id uuid.UUID, email, nome string, verificado, ok bool) {
	var idStr string
	err := apoio.GetDbClient(c).DB().QueryRow(`
		SELECT id::text, COALESCE(email,''), nome, email_verificado
		  FROM projection_contas_pessoa
		 WHERE tipo = $1 AND status <> 'deletado' AND (UPPER(codigo) = UPPER($2) OR LOWER(email) = LOWER($2))
		 LIMIT 1`, tipo, strings.TrimSpace(identificador)).Scan(&idStr, &email, &nome, &verificado)
	if err != nil {
		return uuid.Nil, "", "", false, false
	}
	id, _ = uuid.Parse(idStr)
	return id, email, nome, verificado, true
}

// resetarSenhaContaPessoa conclui o reset de senha por link para os tipos novos.
func resetarSenhaContaPessoa(c *gin.Context, info *services.TokenInfo, hash string, invalidarPendentes func()) {
	var verificado bool
	if err := apoio.GetDbClient(c).DB().QueryRow(
		`SELECT email_verificado FROM projection_contas_pessoa WHERE id = $1 AND tipo = $2`, info.UserID, info.UserType).Scan(&verificado); err != nil {
		utils.RespondWithNotFoundError(c, info.UserType)
		return
	}
	if !verificado {
		utils.RespondWithForbiddenError(c, "Por favor, verifique seu email antes de resetar a senha")
		return
	}
	repo := apoio.GetRepository(c)
	conta, err := carregarContaPessoa(c, info.UserType, info.UserID)
	if err != nil {
		utils.RespondWithNotFoundError(c, info.UserType)
		return
	}
	if err := conta.AlterarSenhaComMotivo(hash, aggregates.MotivoSenhaResetSenha); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	audit := db.AuditContext{UserID: "sistema", UserType: "sistema", IP: c.ClientIP()}
	if err := repo.SaveWithAudit(conta, audit); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	invalidarPendentes()
	log.Printf("Senha resetada (event sourcing) para %s", info.UserType)
	c.JSON(http.StatusOK, gin.H{"message": "Senha redefinida com sucesso!", "proximos_passos": "Faça login com sua nova senha."})
}

// loginContaPessoa é o passo 4 do login em cascata (admin → academia →
// estudante → secretário/professor/encarregado). O código (SC/PF/EN + 6
// dígitos) tem formato próprio, então nunca colide com os dos outros tipos; o
// e-mail é único entre todos os tipos (apoio.EmailEmUso).
type loginContaPessoa struct {
	ID                 uuid.UUID
	Tipo               string
	Nome               string
	Hash               string
	Codigo             string
	Status             string
	EmailNaoVerificado bool
}

func buscarLoginContaPessoa(c *gin.Context, usuario string) (*loginContaPessoa, error) {
	conta, err := apoio.GetContasProjection(c).GetByCodigoOrEmailParaLogin(usuario)
	if err != nil || conta == nil {
		return nil, err
	}
	return &loginContaPessoa{
		ID: conta.ID, Tipo: conta.Tipo, Nome: conta.Nome, Hash: conta.SenhaHash, Codigo: conta.Codigo, Status: conta.Status,
		// Login pelo código é sempre permitido; pelo e-mail só depois de verificado.
		EmailNaoVerificado: conta.Email != nil && strings.EqualFold(*conta.Email, strings.TrimSpace(usuario)) && !conta.EmailVerificado,
	}, nil
}

// perfilContaPessoa responde GET /meu-perfil para os tipos novos.
func perfilContaPessoa(c *gin.Context, tipo string, id uuid.UUID) {
	conta, err := apoio.GetContasProjection(c).GetByID(id, tipo)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if conta == nil || conta.Status == aggregates.StatusContaDeletado {
		utils.RespondWithNotFoundError(c, tipo)
		return
	}
	resp := gin.H{
		"id": conta.ID, "tipo": conta.Tipo, "codigo": conta.Codigo, "nome": conta.Nome, "genero": conta.Genero,
		"data_nascimento": conta.DataNascimento, "email": conta.Email, "email_verificado": conta.EmailVerificado,
		"telefone": conta.Telefone, "status": conta.Status, "dois_fatores_ativo": conta.DoisFatoresAtivo,
		"deve_alterar_senha": conta.DeveAlterarSenha, "created_at": conta.CreatedAt,
	}
	if tipo == aggregates.TipoContaSecretario || tipo == aggregates.TipoContaProfessor {
		resp["codigo_academia"] = conta.CodigoAcademia
		// Dados públicos da academia: a interface depende do nível, do tipo e do
		// nível escolar para decidir o que mostrar (mesmos nomes do perfil da academia).
		if conta.AcademiaID != nil {
			if ac, err := apoio.GetAcademiaProjection(c).GetByID(*conta.AcademiaID); err == nil && ac != nil {
				resp["nome_academia"] = ac.Nome
				resp["academia"] = gin.H{
					"id": ac.ID, "nivel": ac.Nivel, "type": ac.Type, "nome": ac.Nome, "codigo_academia": ac.CodigoAcademia,
					"nivel_escolar": ac.NivelEscolar, "anos_academicos": ac.AnosAcademicos, "status": ac.Status,
				}
			}
		}
	}
	if tipo == aggregates.TipoContaSecretario {
		resp["permissoes"] = conta.Permissoes
	}
	if tipo == aggregates.TipoContaEncarregado {
		educandos, _ := apoio.GetContasProjection(c).EducandosDoEncarregado(conta.ID)
		resp["educandos"] = educandos
	}
	c.JSON(http.StatusOK, resp)
}

// alterarSenhaContaPessoa conclui PUT /alterar-senha para os tipos novos. A
// senha atual já foi validada pelo chamador contra conta.SenhaHash.
func alterarSenhaContaPessoa(c *gin.Context, tipo string, id uuid.UUID, novaSenha, motivo string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(novaSenha), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	conta, err := carregarContaPessoa(c, tipo, id)
	if err != nil {
		return err
	}
	if err := conta.AlterarSenhaComMotivo(string(hash), motivo); err != nil {
		return err
	}
	audit := db.AuditContext{UserID: id.String(), UserType: tipo, IP: c.ClientIP()}
	return apoio.GetRepository(c).SaveWithAudit(conta, audit)
}

// atualizarContatoContaPessoa grava e-mail/telefone próprios dos tipos novos.
func atualizarContatoContaPessoa(c *gin.Context, tipo string, id uuid.UUID, email, telefone *string) error {
	conta, err := carregarContaPessoa(c, tipo, id)
	if err != nil {
		return err
	}
	if err := conta.AtualizarDados(nil, nil, nil, email, telefone); err != nil {
		return err
	}
	audit := db.AuditContext{UserID: id.String(), UserType: tipo, IP: c.ClientIP()}
	return apoio.GetRepository(c).SaveWithAudit(conta, audit)
}

var errContaPessoaNaoEncontrada = errors.New("conta não encontrada")
