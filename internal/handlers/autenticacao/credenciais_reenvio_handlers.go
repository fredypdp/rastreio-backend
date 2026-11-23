package autenticacao

import (
	"database/sql"
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
	"spuri/internal/middleware"
	"spuri/internal/services"
	"spuri/internal/utils"
)

// Tarefa 123 (SEC-04) — reenvio de credenciais (plano 3.8).
//
// Quando o SMS/e-mail com as credenciais falha, ou o titular perdeu a senha e
// não tem e-mail verificado, a entidade que gere a conta redefine e reenvia as
// credenciais: gera NOVA senha temporária, grava o evento de senha com motivo
// credenciais_reenviadas (liga deve_alterar_senha e invalida as sessões
// anteriores), envia pelo canal do tipo de conta e devolve "credenciais".
//
// Intervalo mínimo de 10 minutos por conta, medido por sessoes_validas_desde
// (sem tabela nova), para limitar o custo do SMS. A ação fica auditada
// (SaveWithAudit) com o executor real.

// intervaloMinimoReenvioCredenciais é o intervalo mínimo entre dois reenvios da mesma conta.
const intervaloMinimoReenvioCredenciais = 10 * time.Minute

// agora é o relógio usado pelo intervalo; variável para os testes.
var agora = time.Now

// verificarIntervaloReenvio devolve erro (429) se a conta teve as sessões
// invalidadas (reenvio ou reset) há menos de 10 minutos.
//
// Tarefa 127: o intervalo só vale enquanto a senha temporária do último reenvio
// continua por trocar (pendente = deve_alterar_senha). Quando o titular já trocou
// a senha, sessoes_validas_desde passa a marcar essa troca e não pode travar um
// reenvio legítimo logo a seguir.
func verificarIntervaloReenvio(desde sql.NullTime, pendente bool) (bool, time.Duration) {
	if !desde.Valid || !pendente {
		return false, 0
	}
	decorrido := agora().Sub(desde.Time)
	if decorrido < intervaloMinimoReenvioCredenciais {
		return true, intervaloMinimoReenvioCredenciais - decorrido
	}
	return false, 0
}

func responderIntervaloReenvio(c *gin.Context, restante time.Duration) {
	minutos := int(restante.Minutes()) + 1
	utils.RespondWithErrorData(c, http.StatusTooManyRequests,
		fmt.Sprintf("as credenciais desta conta foram redefinidas há pouco: aguarde cerca de %d minuto(s) para reenviar de novo", minutos),
		nil, gin.H{"retry_after_segundos": int(restante.Seconds()) + 1})
}

func novaSenhaTemporariaComHash() (senha, hash string, err error) {
	senha, err = services.GenerateTempPassword()
	if err != nil {
		return "", "", err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		return "", "", err
	}
	return senha, string(h), nil
}

// ReenviarCredenciaisEstudante — POST /estudantes/:codigo/credenciais/reenviar
// Quem pode (Plano, Parte 5):
//   - a academia dona do estudante e o SECRETÁRIO dela (com estudantes.credenciais);
//     o secretário chega aqui com a identidade efetiva da academia;
//   - admin 'adm' ou 'fpp', mas SÓ para estudante sem nenhuma academia.
//
// O telefone de destino vem da regra única de contato (services.ResolverContatoDoEstudante):
// nível escolar → encarregado; superior → o próprio estudante. Nunca cai no
// telefone do estudante quando ele é do nível escolar.
func ReenviarCredenciaisEstudante(c *gin.Context) {
	codigo := strings.TrimSpace(c.Param("codigo"))
	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)

	var (
		idStr                string
		nome, codigoAcademia string
		desde                sql.NullTime
		pendente             bool
	)
	err := apoio.GetDbClient(c).DB().QueryRow(`
		SELECT id::text, nome, COALESCE(codigo_academia,''), sessoes_validas_desde,
		       COALESCE(deve_alterar_senha, FALSE)
		  FROM projection_estudantes WHERE codigo_estudante = $1 AND status <> 'deletado'`, codigo,
	).Scan(&idStr, &nome, &codigoAcademia, &desde, &pendente)
	if err == sql.ErrNoRows {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	// Permissão: academia (ou secretário) dona do estudante; admin adm/fpp só sem academia.
	var ator apoio.AtorComunicacao
	switch userType {
	case "academia":
		cod, _ := c.Get("codigo_academia")
		codAtor, _ := cod.(string)
		if codAtor == "" || codAtor != codigoAcademia {
			utils.RespondWithForbiddenError(c, "este estudante não pertence à sua academia")
			return
		}
		ator = apoio.AtorComunicacao{ID: userID, Tipo: "academia", CodigoAcademia: codAtor}
	case "admin":
		if err := apoio.VerificarPermissaoAdmin(c, "adm"); err != nil {
			utils.RespondWithForbiddenError(c, "apenas admin com role 'adm' ou 'fpp' pode reenviar credenciais")
			return
		}
		semAcademia, errSem := estudanteSemNenhumaAcademia(c, idStr, codigoAcademia)
		if errSem != nil {
			utils.RespondWithInternalError(c, errSem)
			return
		}
		if !semAcademia {
			utils.RespondWithForbiddenError(c, "este estudante pertence a uma academia: apenas a academia (ou o seu secretário) pode reenviar as credenciais")
			return
		}
		ator = apoio.AtorComunicacao{ID: userID, Tipo: "admin"}
	default:
		utils.RespondWithForbiddenError(c, "acesso negado")
		return
	}

	if bloqueado, restante := verificarIntervaloReenvio(desde, pendente); bloqueado {
		responderIntervaloReenvio(c, restante)
		return
	}

	uid, err := uuid.Parse(idStr)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	// Regra única de contato (Parte 5). Sem queda para o telefone do estudante no nível escolar.
	contato, err := services.ResolverContatoDoEstudante(apoio.GetDbClient(c).DB(), uid)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	telefone := contato.Telefone

	senha, hash, err := novaSenhaTemporariaComHash()
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	repo := apoio.GetRepository(c)
	agg, err := repo.Load(uid, "Estudante")
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	est, ok := agg.(*aggregates.Estudante)
	if !ok {
		utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
		return
	}
	if err := est.AlterarSenhaComMotivo(hash, aggregates.MotivoSenhaCredenciaisReenviadas); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	audit := db.AuditContext{UserID: userID.String(), UserType: userType, IP: c.ClientIP()}
	if err := repo.SaveWithAudit(est, audit); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	log.Printf("[credenciais] reenvio solicitado por %s para estudante %s", userType, codigo)
	credenciais := apoio.EnviarCredenciaisSMS(c.Request.Context(), c, ator, audit, telefone, nome, codigo, senha, true)
	c.JSON(http.StatusOK, gin.H{"message": "credenciais redefinidas", "credenciais": credenciais})
}

// ReenviarCredenciaisAcademia — POST /dominis/academias/:codigo/credenciais/reenviar
// Quem pode: admin 'fpp' (mesma regra de RegisterAcademia).
func ReenviarCredenciaisAcademia(c *gin.Context) {
	if err := apoio.VerificarPermissaoAdmin(c, "fpp"); err != nil {
		utils.RespondWithForbiddenError(c, "apenas admin com role 'fpp' pode reenviar credenciais")
		return
	}
	userID, _ := middleware.GetUserID(c)
	codigo := strings.TrimSpace(c.Param("codigo"))

	var (
		idStr, nome string
		telefone    sql.NullString
		desde       sql.NullTime
		pendente    bool
	)
	err := apoio.GetDbClient(c).DB().QueryRow(`
		SELECT id::text, nome, telefone, sessoes_validas_desde, COALESCE(deve_alterar_senha, FALSE)
		  FROM projection_academias WHERE codigo_academia = $1`, codigo,
	).Scan(&idStr, &nome, &telefone, &desde, &pendente)
	if err == sql.ErrNoRows {
		utils.RespondWithNotFoundError(c, "academia")
		return
	}
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if bloqueado, restante := verificarIntervaloReenvio(desde, pendente); bloqueado {
		responderIntervaloReenvio(c, restante)
		return
	}
	var telPtr *string
	if telefone.Valid {
		v := telefone.String
		telPtr = &v
	}
	destino, err := apoio.TelefoneDestinoCredenciaisAcademia(telPtr)
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("%v — atualize o telefone da academia antes de reenviar", err))
		return
	}

	uid, err := uuid.Parse(idStr)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	senha, hash, err := novaSenhaTemporariaComHash()
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	repo := apoio.GetRepository(c)
	agg, err := repo.Load(uid, "Academia")
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	academia, ok := agg.(*aggregates.Academia)
	if !ok {
		utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
		return
	}
	if err := academia.AlterarSenha(hash, userID, aggregates.MotivoSenhaCredenciaisReenviadas); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	audit := db.AuditContext{UserID: userID.String(), UserType: "admin", IP: c.ClientIP()}
	if err := repo.SaveWithAudit(academia, audit); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	log.Printf("[credenciais] reenvio solicitado por admin para academia %s", codigo)
	credenciais := apoio.EnviarCredenciaisSMS(c.Request.Context(), c, apoio.AtorComunicacao{ID: userID, Tipo: "admin"}, audit, destino, nome, codigo, senha, true)
	c.JSON(http.StatusOK, gin.H{"message": "credenciais redefinidas", "credenciais": credenciais})
}

// ReenviarCredenciaisAdmin — POST /dominis/administradores/:id/credenciais/reenviar
// Quem pode: admin 'fpp' (mesma regra de RegisterAdmin). As credenciais seguem
// por e-mail (Brevo).
func ReenviarCredenciaisAdmin(c *gin.Context) {
	if err := apoio.VerificarPermissaoAdmin(c, "fpp"); err != nil {
		utils.RespondWithForbiddenError(c, "apenas admin com role 'fpp' pode reenviar credenciais")
		return
	}
	userID, _ := middleware.GetUserID(c)
	alvoID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("id de administrador inválido"))
		return
	}
	if alvoID == userID {
		utils.RespondWithValidationError(c, fmt.Errorf("para trocar a sua própria senha use a opção de alterar senha"))
		return
	}

	var (
		nome, email, role string
		desde             sql.NullTime
		pendente          bool
	)
	err = apoio.GetDbClient(c).DB().QueryRow(`
		SELECT nome, email, role, sessoes_validas_desde, COALESCE(deve_alterar_senha, FALSE)
		  FROM projection_admins WHERE id::text = $1`, alvoID.String(),
	).Scan(&nome, &email, &role, &desde, &pendente)
	if err == sql.ErrNoRows {
		utils.RespondWithNotFoundError(c, "administrador")
		return
	}
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if bloqueado, restante := verificarIntervaloReenvio(desde, pendente); bloqueado {
		responderIntervaloReenvio(c, restante)
		return
	}
	// Antes de mudar a senha, garante que há canal: sem Brevo não se redefine uma
	// senha que ninguém conseguiria receber.
	if !services.BrevoConfigurado() && !services.CredenciaisLogDevAtivo() {
		utils.RespondWithServiceUnavailable(c, fmt.Errorf("o envio de e-mail está temporariamente indisponível"))
		return
	}

	senha, hash, err := novaSenhaTemporariaComHash()
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	repo := apoio.GetRepository(c)
	agg, err := repo.Load(alvoID, "Admin")
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	admin, ok := agg.(*aggregates.Admin)
	if !ok {
		utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
		return
	}
	if err := admin.AlterarSenha(hash, userID, aggregates.MotivoSenhaCredenciaisReenviadas); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	audit := db.AuditContext{UserID: userID.String(), UserType: "admin", IP: c.ClientIP()}
	if err := repo.SaveWithAudit(admin, audit); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	log.Printf("[credenciais] reenvio solicitado por admin para administrador %s", alvoID)
	credenciais := apoio.ResultadoCredenciais{Canal: "email", Enviada: true}
	if err := apoio.GetEmailService(c).SendAdminWelcomeEmail(email, nome, senha, role); err != nil {
		log.Printf("[credenciais] e-mail de credenciais NÃO enviado ao administrador %s: %v", alvoID, err)
		credenciais = apoio.ResultadoCredenciais{Canal: "email", Motivo: apoio.MotivoFalhaEnvio}
	}
	c.JSON(http.StatusOK, gin.H{"message": "credenciais redefinidas", "credenciais": credenciais})
}

// estudanteSemNenhumaAcademia diz se o estudante não tem academia: nem a coluna
// principal nem qualquer vínculo ativo. É o único caso em que o admin pode
// resetar as credenciais de um estudante.
func estudanteSemNenhumaAcademia(c *gin.Context, estudanteID, codigoAcademia string) (bool, error) {
	if strings.TrimSpace(codigoAcademia) != "" {
		return false, nil
	}
	var existe bool
	err := apoio.GetDbClient(c).DB().QueryRow(`
		SELECT EXISTS (SELECT 1 FROM projection_estudante_vinculos
		                WHERE estudante_id = $1::uuid AND desvinculado_em IS NULL AND deleted_at IS NULL)`, estudanteID).Scan(&existe)
	return !existe, err
}
