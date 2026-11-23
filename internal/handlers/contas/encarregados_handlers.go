package contas

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"spuri/internal/domain/aggregates"
	"spuri/internal/handlers/apoio"
	"spuri/internal/middleware"
	"spuri/internal/utils"
)

// ============================================================================
// Planejamento do encarregado (usado no cadastro e na troca de encarregado)
// ============================================================================

// ============================================================================
// Rotas da academia / secretário
// ============================================================================

func mascararTelefone(t string) string {
	if len(t) < 6 {
		return "******"
	}
	return t[:3] + "***" + t[len(t)-3:]
}

// BuscarEncarregado — GET /academia/encarregados/buscar?telefone=&email=
// Ajuda a academia a confirmar se o encarregado já existe antes de cadastrar o
// estudante. Devolve só o necessário para confirmar a identidade (nome e
// telefone mascarado).
func BuscarEncarregado(c *gin.Context) {
	tel := strings.TrimSpace(c.Query("telefone"))
	email := strings.TrimSpace(c.Query("email"))
	if tel == "" && email == "" {
		utils.RespondWithValidationError(c, fmt.Errorf("informe telefone ou email"))
		return
	}
	if tel != "" {
		n, err := apoio.TelefoneDestinoCredenciaisConta(tel, "do encarregado")
		if err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
		tel = n
	}
	enc, err := apoio.GetContasProjection(c).GetEncarregadoPorContato(tel, email)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if enc == nil {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"encontrado": false}})
		return
	}
	educandos, _ := apoio.GetContasProjection(c).EducandosDoEncarregado(enc.ID)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"encontrado": true,
		"encarregado": gin.H{
			"id": enc.ID, "nome": enc.Nome, "telefone_mascarado": mascararTelefone(enc.Telefone),
			"total_educandos": len(educandos), "status": enc.Status,
		},
	}})
}

// TrocarEncarregado — PUT /academia/estudante/:codigo/encarregado
// A academia (ou o secretário com estudantes.dados) troca o encarregado do
// estudante: o vínculo atual é encerrado e o novo criado, na mesma transação.
func TrocarEncarregado(c *gin.Context) {
	ac, ok := academiaDaRequisicao(c)
	if !ok {
		return
	}
	var req struct {
		Encarregado apoio.BlocoEncarregado `json:"encarregado"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("body inválido"))
		return
	}
	est, err := apoio.GetEstudanteProjection(c).GetByCodigo(strings.ToUpper(strings.TrimSpace(c.Param("codigo"))))
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if est == nil || est.Status == "deletado" || est.CodigoAcademia == nil || *est.CodigoAcademia != ac.Codigo {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}
	if !utils.TemNivelEscolar(est.AnoEscolar, est.AnoEscolarMedio, est.StatusEscolarFundamental, est.StatusEscolarMedio) {
		utils.RespondWithConflictError(c, "somente estudantes do nível escolar têm encarregado")
		return
	}
	if utils.FinalizouNivelEscolar(est.StatusEscolarMedio, est.AnoSuperior) {
		utils.RespondWithConflictError(c, "o estudante concluiu o nível escolar: o encarregado já não pode ser trocado")
		return
	}
	por := atorProfessores(c, ac)
	plano, ok := apoio.PlanejarEncarregado(c, req.Encarregado, &por)
	if !ok {
		return
	}
	sucesso := false
	defer func() { plano.Concluir(sucesso) }()
	proj := apoio.GetContasProjection(c)
	atual, err := proj.EncarregadoDoEstudante(est.ID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if atual != nil && atual.ID == plano.ID() {
		utils.RespondWithConflictError(c, "este já é o encarregado do estudante")
		return
	}
	novoEnc, err := plano.Agregado(c)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	lote := []aggregates.Aggregate{}
	if atual != nil {
		agg, err := apoio.GetRepository(c).Load(atual.ID, aggregates.AggregateEncarregado)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		antigo := agg.(*aggregates.Encarregado)
		if err := antigo.DesvincularEducando(est.ID, aggregates.MotivoDesvinculoTrocaAcademia, &por); err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
		lote = append(lote, antigo)
	}
	if err := plano.Vincular(novoEnc, est.ID, est.CodigoEstudante, &por); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	lote = append(lote, novoEnc)
	audit := apoio.AuditoriaDaRequisicao(c, ac.ID, "academia")
	if err := apoio.GetRepository(c).SaveManyWithAudit(audit, lote...); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	sucesso = true
	resp := gin.H{"encarregado_id": plano.ID(), "codigo": plano.Codigo(), "conta_nova": plano.ContaNova()}
	if cred := plano.EnviarCredenciais(c, apoio.AtorComunicacao{ID: ac.ID, Tipo: "academia", CodigoAcademia: ac.Codigo}, audit); cred != nil {
		resp["credenciais"] = cred
	}
	c.JSON(http.StatusOK, gin.H{"message": "encarregado atualizado com sucesso", "data": resp})
}

// ============================================================================
// Rotas do próprio encarregado
// ============================================================================

// ListarMeusEducandos — GET /encarregado/educandos
func ListarMeusEducandos(c *gin.Context) {
	encID, ok := middleware.GetUserID(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	rows, err := apoio.GetDbClient(c).DB().Query(`
		SELECT x.codigo_estudante, e.nome, e.status, COALESCE(e.codigo_academia,''), COALESCE(a.nome,''),
		       COALESCE(e.status_escolar_medio,''), e.ano_superior, x.relacao, x.relacao_outro
		  FROM projection_encarregado_educandos x
		  JOIN projection_estudantes e ON e.id = x.estudante_id
		  LEFT JOIN projection_academias a ON a.codigo_academia = e.codigo_academia
		 WHERE x.encarregado_id = $1 AND x.desvinculado_em IS NULL AND e.status <> 'deletado'
		 ORDER BY e.nome`, encID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	defer rows.Close()
	saida := []gin.H{}
	for rows.Next() {
		var cod, nome, status, codAcad, nomeAcad, statusMedio string
		var anoSup, rel, relOutro *string
		if err := rows.Scan(&cod, &nome, &status, &codAcad, &nomeAcad, &statusMedio, &anoSup, &rel, &relOutro); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		fase := "acao"
		if utils.FinalizouNivelEscolar(statusMedio, anoSup) {
			fase = "leitura"
		}
		saida = append(saida, gin.H{
			"codigo_estudante": cod, "nome": nome, "status": status,
			"codigo_academia": codAcad, "nome_academia": nomeAcad,
			"relacao": rel, "relacao_outro": relOutro,
			// "acao": o encarregado faz tudo o que o estudante faz;
			// "leitura": o estudante concluiu o nível escolar (só consulta).
			"fase": fase,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": saida, "total": len(saida)})
}

// AtualizarRelacaoEducando — PUT /encarregado/educandos/:codigo/relacao
func AtualizarRelacaoEducando(c *gin.Context) {
	encID, ok := middleware.GetUserID(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	var req struct {
		Relacao      *string `json:"relacao"`
		RelacaoOutro *string `json:"relacao_outro"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("body inválido"))
		return
	}
	ed, err := apoio.GetContasProjection(c).EducandoAtivo(encID, c.Param("codigo"))
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if ed == nil {
		utils.RespondWithNotFoundError(c, "educando")
		return
	}
	repo := apoio.GetRepository(c)
	agg, err := repo.Load(encID, aggregates.AggregateEncarregado)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	enc := agg.(*aggregates.Encarregado)
	if err := enc.AtualizarRelacao(ed.EstudanteID, req.Relacao, req.RelacaoOutro); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := repo.SaveWithAudit(enc, apoio.AuditoriaDaRequisicao(c, encID, "encarregado")); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "relação atualizada com sucesso"})
}

// ============================================================================
// Rota do estudante: desvincular-se do encarregado
// ============================================================================

// DesvincularEncarregado — DELETE /estudante/encarregado
// O estudante só pode desvincular-se do encarregado depois de concluir o nível
// escolar. Antes disso o encarregado é quem responde por ele.
func DesvincularEncarregado(c *gin.Context) {
	estID, ok := middleware.GetUserID(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	est, err := apoio.GetEstudanteProjection(c).GetByID(estID)
	if err != nil || est == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}
	if !utils.FinalizouNivelEscolar(est.StatusEscolarMedio, est.AnoSuperior) {
		utils.RespondWithConflictError(c, "só é possível desvincular o encarregado depois de concluir o nível escolar")
		return
	}
	proj := apoio.GetContasProjection(c)
	atual, err := proj.EncarregadoDoEstudante(estID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if atual == nil {
		utils.RespondWithNotFoundError(c, "encarregado")
		return
	}
	repo := apoio.GetRepository(c)
	agg, err := repo.Load(atual.ID, aggregates.AggregateEncarregado)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	enc := agg.(*aggregates.Encarregado)
	if err := enc.DesvincularEducando(estID, aggregates.MotivoDesvinculoEstudante, &estID); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := repo.SaveWithAudit(enc, apoio.AuditoriaDaRequisicao(c, estID, "estudante")); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "encarregado desvinculado com sucesso"})
}
