package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
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
// Quem pode: a academia dona do estudante ou admin 'fpp'.
func ReenviarCredenciaisEstudante(c *gin.Context) {
	codigo := strings.TrimSpace(c.Param("codigo"))
	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)

	var (
		idStr                                     string
		nome, codigoAcademia                      string
		telEst, telEnc, anoFund, anoMedio, anoSup sql.NullString
		desde                                     sql.NullTime
		pendente                                  bool
	)
	err := getDbClient(c).DB().QueryRow(`
		SELECT id::text, nome, COALESCE(codigo_academia,''), telefone, telefone_encarregado,
		       ano_escolar_fundamental, ano_escolar_medio, ano_superior, sessoes_validas_desde,
		       COALESCE(deve_alterar_senha, FALSE)
		  FROM projection_estudantes WHERE codigo_estudante = $1`, codigo,
	).Scan(&idStr, &nome, &codigoAcademia, &telEst, &telEnc, &anoFund, &anoMedio, &anoSup, &desde, &pendente)
	if err == sql.ErrNoRows {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	// Permissão: academia dona do estudante, ou admin fpp.
	var ator atorComunicacao
	switch userType {
	case "academia":
		cod, _ := c.Get("codigo_academia")
		codAtor, _ := cod.(string)
		if codAtor == "" || codAtor != codigoAcademia {
			utils.RespondWithForbiddenError(c, "este estudante não pertence à sua academia")
			return
		}
		ator = atorComunicacao{ID: userID, Tipo: "academia", CodigoAcademia: codAtor}
	case "admin":
		if err := verificarPermissaoAdmin(c, "fpp"); err != nil {
			utils.RespondWithForbiddenError(c, "apenas admin com role 'fpp' pode reenviar credenciais")
			return
		}
		ator = atorComunicacao{ID: userID, Tipo: "admin"}
	default:
		utils.RespondWithForbiddenError(c, "acesso negado")
		return
	}

	if bloqueado, restante := verificarIntervaloReenvio(desde, pendente); bloqueado {
		responderIntervaloReenvio(c, restante)
		return
	}

	ptr := func(n sql.NullString) *string {
		if n.Valid {
			v := n.String
			return &v
		}
		return nil
	}
	telefone, err := TelefoneDestinoCredenciaisEstudante(ptr(anoFund), ptr(anoMedio), ptr(anoSup), ptr(telEst), ptr(telEnc))
	if err != nil {
		utils.RespondWithValidationError(c, err)
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
	repo := getRepository(c)
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
	credenciais := enviarCredenciaisSMS(c.Request.Context(), c, ator, audit, telefone, nome, codigo, senha, true)
	c.JSON(http.StatusOK, gin.H{"message": "credenciais redefinidas", "credenciais": credenciais})
}

// ReenviarCredenciaisAcademia — POST /dominis/academias/:codigo/credenciais/reenviar
// Quem pode: admin 'fpp' (mesma regra de RegisterAcademia).
func ReenviarCredenciaisAcademia(c *gin.Context) {
	if err := verificarPermissaoAdmin(c, "fpp"); err != nil {
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
	err := getDbClient(c).DB().QueryRow(`
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
	destino, err := telefoneDestinoCredenciaisAcademia(telPtr)
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
	repo := getRepository(c)
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
	credenciais := enviarCredenciaisSMS(c.Request.Context(), c, atorComunicacao{ID: userID, Tipo: "admin"}, audit, destino, nome, codigo, senha, true)
	c.JSON(http.StatusOK, gin.H{"message": "credenciais redefinidas", "credenciais": credenciais})
}

// ReenviarCredenciaisAdmin — POST /dominis/administradores/:id/credenciais/reenviar
// Quem pode: admin 'fpp' (mesma regra de RegisterAdmin). As credenciais seguem
// por e-mail (Brevo).
func ReenviarCredenciaisAdmin(c *gin.Context) {
	if err := verificarPermissaoAdmin(c, "fpp"); err != nil {
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
	err = getDbClient(c).DB().QueryRow(`
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
	repo := getRepository(c)
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
	credenciais := resultadoCredenciais{Canal: "email", Enviada: true}
	if err := getEmailService(c).SendAdminWelcomeEmail(email, nome, senha, role); err != nil {
		log.Printf("[credenciais] e-mail de credenciais NÃO enviado ao administrador %s: %v", alvoID, err)
		credenciais = resultadoCredenciais{Canal: "email", Motivo: motivoFalhaEnvio}
	}
	c.JSON(http.StatusOK, gin.H{"message": "credenciais redefinidas", "credenciais": credenciais})
}

// anexarDeveAlterarSenha acrescenta "deve_alterar_senha" a cada item de uma
// listagem (uma consulta por página, não uma por linha). O frontend usa-o para
// o indicador "credenciais não confirmadas" e para o botão "Reenviar
// credenciais". chave é o campo que identifica o item na listagem ("id",
// "codigo_estudante" ou "codigo_academia") e coluna a coluna SQL equivalente.
// Falha silenciosa: a lista nunca deixa de ser devolvida por causa deste indicador.
func anexarDeveAlterarSenha(client *db.Client, tabela, coluna, chave string, itens []map[string]interface{}) {
	switch tabela + "/" + coluna {
	case "projection_estudantes/codigo_estudante", "projection_academias/codigo_academia", "projection_admins/id::text":
	default:
		return
	}
	ids := make([]string, 0, len(itens))
	for _, it := range itens {
		if id := fmt.Sprint(it[chave]); id != "" && id != "<nil>" {
			ids = append(ids, id)
		}
	}
	pendentes := map[string]bool{}
	if len(ids) > 0 {
		rows, err := client.DB().Query(`SELECT `+coluna+` FROM `+tabela+` WHERE deve_alterar_senha = TRUE AND `+coluna+` = ANY($1)`, pq.Array(ids))
		if err != nil {
			log.Printf("[WARN] anexarDeveAlterarSenha(%s): %v", tabela, err)
		} else {
			defer rows.Close()
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					pendentes[id] = true
				}
			}
		}
	}
	for _, it := range itens {
		it["deve_alterar_senha"] = pendentes[fmt.Sprint(it[chave])]
	}
}
