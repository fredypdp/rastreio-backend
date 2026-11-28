package servicosextras

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/cadastros"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/finance"
	"spuri/internal/handlers/apoio"
	"spuri/internal/middleware"
	"spuri/internal/utils"
)

func SolicitarServicoExtra(c *gin.Context) {
	sid, e := uuid.Parse(c.Param("id"))
	if e != nil {
		utils.RespondWithValidationError(c, e)
		return
	}
	uid, _ := middleware.GetUserID(c)
	est, e := apoio.GetEstudanteProjection(c).GetByID(uid)
	if e != nil || est == nil || est.CodigoAcademia == nil {
		utils.RespondWithForbiddenError(c, "estudante não está vinculado a uma academia")
		return
	}
	// SEC-12/13 handler defense in depth: bloquear estudante deletado e academia inativa
	if est.Status == "deletado" {
		utils.RespondWithError(c, http.StatusUnauthorized, "conta indisponível. Entre em contato com a sua academia ou com o suporte.", nil)
		return
	}
	academiaProj := apoio.GetAcademiaProjection(c)
	academiaDTO, err := academiaProj.GetByCodigo(*est.CodigoAcademia)
	if err != nil || academiaDTO == nil || academiaDTO.Status != "ativo" {
		utils.RespondWithError(c, http.StatusUnauthorized, "academia inativa ou indisponível", nil)
		return
	}
	pedido, ok := lerPedidoSolicitacao(c)
	if !ok {
		return
	}
	serv, rotaID, veiculoIDs, ok := resolverPedido(c, sid, *est.CodigoAcademia, pedido)
	if !ok {
		return
	}
	if !apoio.ElegivelParaServicoExtra(serv.Disponibilidade, est) {
		utils.RespondWithForbiddenError(c, "este serviço não está disponível para o seu ano/curso atual")
		return
	}
	active, e := apoio.GetSolicitacoesServicoExtraProjection(c).ExisteAtiva(sid, est.CodigoEstudante)
	if e != nil {
		utils.RespondWithInternalError(c, e)
		return
	}
	if active {
		utils.RespondWithConflictError(c, "já existe solicitação ativa para este serviço")
		return
	}
	guard, e := db.NewUniqueOperationGuard(apoio.GetDbClient(c)).WithContext(c.Request.Context()).Reserve(
		"solicitacao_servico_extra:ativa",
		db.CanonicalGuardKey(sid.String(), est.CodigoEstudante),
		db.UniqueGuardOptions{UserID: uid.String(), UserType: "estudante"},
	)
	if e != nil {
		utils.RespondWithConflictError(c, "já existe solicitação ativa para este serviço")
		return
	}
	guardConsumed := false
	defer func() {
		if !guardConsumed {
			_ = guard.Release()
		}
	}()

	var documentoPath, documentoURL string
	// A solicitação pode não ter documento; nesse caso, uma requisição sem
	// multipart/form-data continua válida quando o serviço não o exige.
	_ = c.Request.ParseMultipartForm(apoio.MaxPDFUploadBytes + 1024)
	if fh, err := c.FormFile("documento"); err == nil {
		pdf, err := apoio.ReadAndValidatePDF("documento", fh)
		if err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
		provider := apoio.GetStorageProvider(c)
		if provider == nil {
			utils.RespondWithInternalError(c, errors.New("storage não configurado"))
			return
		}
		path := fmt.Sprintf("%s/estudantes/%s/servicos_extras/%s.pdf", serv.CodigoAcademia, est.CodigoEstudante, sid)
		stored, err := provider.Upload(path, bytes.NewReader(pdf.Data), pdf.Size)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		documentoPath, documentoURL = stored.Path, stored.FileURL
	}
	s := aggregates.NewSolicitacaoServicoExtra()
	if e = s.Criar(sid, serv.Tipo, rotaID, veiculoIDs, serv.CodigoAcademia, est.CodigoEstudante, documentoPath, documentoURL); e != nil {
		if documentoPath != "" {
			_ = apoio.GetStorageProvider(c).Delete(documentoPath)
		}
		utils.RespondWithValidationError(c, e)
		return
	}
	if e = apoio.GetRepository(c).SaveWithAudit(s, db.AuditContext{UserID: uid.String(), UserType: "estudante", IP: c.ClientIP()}); e != nil {
		if documentoPath != "" {
			_ = apoio.GetStorageProvider(c).Delete(documentoPath)
		}
		utils.RespondWithInternalError(c, e)
		return
	}
	if e = guard.Consume(s.GetID()); e != nil {
		utils.RespondWithInternalError(c, e)
		return
	}
	guardConsumed = true
	c.JSON(http.StatusCreated, gin.H{"data": apoio.SolicitacaoServicoExtraToJSON(s)})
}

func solicFromParam(c *gin.Context) (*aggregates.SolicitacaoServicoExtra, bool) {
	id, e := uuid.Parse(c.Param("id"))
	if e != nil {
		utils.RespondWithValidationError(c, e)
		return nil, false
	}
	return apoio.LoadSolicitacaoServicoExtra(c, id)
}

func AprovarSolicitacaoServicoExtra(c *gin.Context) {
	s, ok := solicFromParam(c)
	if !ok {
		return
	}
	codigo, user, ok := apoio.Academy(c)
	if !ok {
		return
	}
	if s.CodigoAcademia != codigo {
		utils.RespondWithForbiddenError(c, "solicitação não pertence à academia")
		return
	}
	serv, e := apoio.GetServicosRepo(c).ResolverServico(c.Request.Context(), s.ServicoExtraID, s.RotaID)
	if e != nil {
		utils.RespondWithNotFoundError(c, "serviço extra")
		return
	}
	var in struct {
		TurmaID string `json:"turma_id"`
	}
	_ = c.ShouldBindJSON(&in) // corpo opcional: só a atividade coletiva usa
	turmaID, ok := turmaDaAprovacao(c, s, serv, in.TurmaID)
	if !ok {
		return
	}
	if e = s.Aprovar(serv.Taxa.Valor > 0, serv.Taxa.Valor, serv.Taxa.Metodos, turmaID, user); e != nil {
		utils.RespondWithConflictError(c, e.Error())
		return
	}
	audit := db.AuditContext{UserID: user.String(), UserType: "academia", IP: c.ClientIP()}
	if e = apoio.GetRepository(c).SaveWithAudit(s, audit); e != nil {
		utils.RespondWithInternalError(c, e)
		return
	}
	if s.Status == aggregates.StatusInscricaoVinculada {
		if e = apoio.RegistrarMembroDaAtividade(c, s, audit); e != nil {
			utils.RespondWithInternalError(c, e)
			return
		}
	}
	c.JSON(200, gin.H{"status": s.Status})
}

func ReprovarSolicitacaoServicoExtra(c *gin.Context) {
	s, ok := solicFromParam(c)
	if !ok {
		return
	}
	codigo, user, ok := apoio.Academy(c)
	if !ok {
		return
	}
	if s.CodigoAcademia != codigo {
		utils.RespondWithForbiddenError(c, "solicitação não pertence à academia")
		return
	}
	var in struct {
		Motivo string `json:"motivo_reprovacao"`
	}
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Motivo) == "" {
		utils.RespondWithValidationError(c, errors.New("motivo_reprovacao é obrigatório"))
		return
	}
	if e := s.Reprovar(in.Motivo, user); e != nil {
		utils.RespondWithConflictError(c, e.Error())
		return
	}
	if e := apoio.GetRepository(c).SaveWithAudit(s, db.AuditContext{UserID: user.String(), UserType: "academia", IP: c.ClientIP()}); e != nil {
		utils.RespondWithInternalError(c, e)
		return
	}
	c.JSON(200, gin.H{"status": s.Status})
}

func IniciarPagamentoTaxaInscricaoServicoExtra(c *gin.Context) {
	var in finance.TaxaInscricaoServicoExtraPagamentoInput
	if c.ShouldBindJSON(&in) != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	id, typ, _, ok := apoio.FinanceActor(c)
	if !ok || typ != "estudante" {
		utils.RespondWithForbiddenError(c, "somente o estudante pode iniciar pagamento")
		return
	}
	sid := c.Query("solicitacao_id")
	suid, e := uuid.Parse(sid)
	if e != nil {
		utils.RespondWithValidationError(c, errors.New("solicitacao_id inválido"))
		return
	}
	s, ok := apoio.LoadSolicitacaoServicoExtra(c, suid)
	if !ok {
		return
	}
	var code string
	if e = apoio.GetDBClient(c).DB().QueryRowContext(c.Request.Context(), `SELECT codigo_estudante FROM projection_estudantes WHERE id=$1`, id).Scan(&code); e != nil || s.CodigoEstudante != code {
		utils.RespondWithForbiddenError(c, "esta solicitação não pertence ao estudante autenticado")
		return
	}
	in.SolicitacaoID = sid
	out, e := apoio.FinanceiroService.IniciarPagamentoTaxaInscricaoServicoExtra(c.Request.Context(), in, c.ClientIP())
	if e != nil {
		apoio.FinanceError(c, e)
		return
	}
	if strings.EqualFold(out.Charge.Status, "success") {
		codigo, tipo, mes, ano, err := apoio.FinanceiroService.DadosServicoExtraDaCobranca(c.Request.Context(), out.Charge.ID.String())
		if err == nil && codigo != "" {
			switch tipo {
			case "taxa_inscricao":
				_ = apoio.EfetivarVinculoServicoExtraPago(c, codigo)
			case "mensalidade", "preco_unico":
				_ = apoio.FinanceiroService.ConfirmarLancamentoServicoExtraPago(c.Request.Context(), codigo, tipo, ano, mes, id.String(), typ, c.ClientIP())
			}
		}
	}
	c.JSON(http.StatusCreated, out)
}

func cancelarSolicitacaoServicoExtra(c *gin.Context, s *aggregates.SolicitacaoServicoExtra, motivo, por, actor string) {
	var e error
	if s.Status == aggregates.StatusInscricaoPendente {
		// Ainda nem foi decidida pela academia — não há cobrança nenhuma
		// criada para esta solicitação neste estágio, então não há nada a
		// estornar no financeiro antes de cancelar.
		e = s.CancelarAntesDaVinculacao(motivo, por)
	} else if s.Status == aggregates.StatusInscricaoAprovadaPendentePagamentoTaxa {
		e = apoio.FinanceiroService.CancelarCobrancaTaxaInscricaoServicoAberta(c.Request.Context(), s.GetID().String(), motivo, actor, por, c.ClientIP())
		if e == nil {
			e = s.CancelarAntesDaVinculacao(motivo, por)
		}
	} else if s.Status == aggregates.StatusInscricaoVinculada {
		e = s.Cancelar(motivo, por)
	} else {
		utils.RespondWithConflictError(c, fmt.Sprintf("solicitação em estado '%s' não pode ser cancelada", s.Status))
		return
	}
	if e != nil {
		utils.RespondWithConflictError(c, e.Error())
		return
	}
	audit := db.AuditContext{UserID: actor, UserType: por, IP: c.ClientIP()}
	if e = apoio.GetRepository(c).SaveWithAudit(s, audit); e != nil {
		utils.RespondWithInternalError(c, e)
		return
	}
	apoio.RemoverMembroDaAtividade(c, s, audit)
	c.JSON(200, gin.H{"status": s.Status})
}

func CancelarSolicitacaoServicoExtraAcademia(c *gin.Context) {
	s, ok := solicFromParam(c)
	if !ok {
		return
	}
	code, id, ok := apoio.Academy(c)
	if !ok {
		return
	}
	if s.CodigoAcademia != code {
		utils.RespondWithForbiddenError(c, "solicitação não pertence à academia")
		return
	}
	var x struct {
		Motivo string `json:"motivo"`
	}
	_ = c.ShouldBindJSON(&x)
	cancelarSolicitacaoServicoExtra(c, s, x.Motivo, "academia", id.String())
}

func CancelarSolicitacaoServicoExtraEstudante(c *gin.Context) {
	s, ok := solicFromParam(c)
	if !ok {
		return
	}
	id, _ := middleware.GetUserID(c)
	est, e := apoio.GetEstudanteProjection(c).GetByID(id)
	if e != nil || est == nil || s.CodigoEstudante != est.CodigoEstudante {
		utils.RespondWithForbiddenError(c, "solicitação não pertence ao estudante")
		return
	}
	var x struct {
		Motivo string `json:"motivo"`
	}
	_ = c.ShouldBindJSON(&x)
	cancelarSolicitacaoServicoExtra(c, s, x.Motivo, "estudante", id.String())
}

// ListarSolicitacoesServicoExtraAcademia retorna as solicitações da academia autenticada.
func ListarSolicitacoesServicoExtraAcademia(c *gin.Context) {
	codigo, _, ok := apoio.Academy(c)
	if !ok {
		return
	}
	rows, err := apoio.GetDBClient(c).DB().QueryContext(c.Request.Context(), `SELECT id,servico_extra_id,tipo_servico,rota_id,turma_id,codigo_estudante,status,motivo_reprovacao,motivo_cancelamento,created_at,updated_at FROM projection_solicitacoes_servico_extra WHERE codigo_academia=$1 AND (NULLIF($2,'') IS NULL OR status=$2) AND (NULLIF($3,'') IS NULL OR servico_extra_id=$3::uuid) ORDER BY created_at DESC`, codigo, c.Query("status"), c.Query("servico_extra_id"))
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, service uuid.UUID
		var rota, turma *uuid.UUID
		var student, status, tipo string
		var repro, cancel *string
		var created, updated interface{}
		if err := rows.Scan(&id, &service, &tipo, &rota, &turma, &student, &status, &repro, &cancel, &created, &updated); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		out = append(out, gin.H{"id": id, "servico_extra_id": service, "tipo_servico": tipo, "rota_id": rota, "turma_id": turma, "codigo_estudante": student, "status": status, "motivo_reprovacao": repro, "motivo_cancelamento": cancel, "created_at": created, "updated_at": updated})
	}
	c.JSON(200, gin.H{"solicitacoes": out, "total": len(out)})
}

func GetSolicitacaoServicoExtraAcademia(c *gin.Context) {
	s, ok := solicFromParam(c)
	if !ok {
		return
	}
	code, _, ok := apoio.Academy(c)
	if !ok {
		return
	}
	if s.CodigoAcademia != code {
		utils.RespondWithForbiddenError(c, "solicitação não pertence à academia")
		return
	}
	c.JSON(200, gin.H{"data": apoio.SolicitacaoServicoExtraToJSON(s)})
}

func DownloadDocumentoSolicitacaoServicoExtraAcademia(c *gin.Context) {
	s, ok := solicFromParam(c)
	if !ok {
		return
	}
	codigo, _, ok := apoio.Academy(c)
	if !ok {
		return
	}
	if s.CodigoAcademia != codigo {
		utils.RespondWithForbiddenError(c, "solicitação não pertence à academia")
		return
	}
	downloadDocumentoServicoExtra(c, s)
}

func DownloadDocumentoSolicitacaoServicoExtraEstudante(c *gin.Context) {
	s, ok := solicFromParam(c)
	if !ok {
		return
	}
	id, _ := middleware.GetUserID(c)
	est, e := apoio.GetEstudanteProjection(c).GetByID(id)
	if e != nil || est == nil || s.CodigoEstudante != est.CodigoEstudante {
		utils.RespondWithForbiddenError(c, "solicitação não pertence ao estudante")
		return
	}
	downloadDocumentoServicoExtra(c, s)
}

func downloadDocumentoServicoExtra(c *gin.Context, s *aggregates.SolicitacaoServicoExtra) {
	if s.DocumentoPath == "" {
		utils.RespondWithNotFoundError(c, "documento")
		return
	}
	apoio.StreamDocumento(c, "documento_servico_extra", aggregates.DocumentoMatricula{Path: s.DocumentoPath, FileURL: s.DocumentoURL})
}

func ListarMinhasInscricoesServicoExtra(c *gin.Context) {
	id, _ := middleware.GetUserID(c)
	est, e := apoio.GetEstudanteProjection(c).GetByID(id)
	if e != nil || est == nil {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	rows, e := apoio.GetDBClient(c).DB().QueryContext(c.Request.Context(), `SELECT id FROM projection_solicitacoes_servico_extra WHERE codigo_estudante=$1 AND (NULLIF($2,'') IS NULL OR status=$2) ORDER BY created_at DESC`, est.CodigoEstudante, c.Query("status"))
	if e != nil {
		utils.RespondWithInternalError(c, e)
		return
	}
	defer rows.Close()
	out := []gin.H{}
	disps := map[string]finance.PagamentoDisponivel{}
	for rows.Next() {
		var sid uuid.UUID
		if rows.Scan(&sid) != nil {
			continue
		}
		if s, ok := apoio.LoadSolicitacaoServicoExtra(c, sid); ok {
			// Os métodos da taxa de inscrição que o estudante pode usar
			// dependem do provedor ativo da academia (Tarefa 121).
			out = append(out, apoio.SolicitacaoServicoExtraParaPagador(c.Request.Context(), disps, s, apoio.SolicitacaoServicoExtraToJSON(s)))
		}
	}
	c.JSON(200, gin.H{"inscricoes": out, "total": len(out)})
}

func pendenciasServicoExtra(c *gin.Context, s *aggregates.SolicitacaoServicoExtra) {
	if s.Status != aggregates.StatusInscricaoVinculada && s.Status != aggregates.StatusInscricaoCancelada {
		c.JSON(http.StatusOK, gin.H{"pendencias": []finance.ServicoExtraPendenciaView{}})
		return
	}
	serv, err := apoio.GetServicosRepo(c).ResolverServico(c.Request.Context(), s.ServicoExtraID, s.RotaID)
	if err != nil {
		utils.RespondWithNotFoundError(c, "serviço extra")
		return
	}
	// Serviço gratuito (ou sem meses cobrados): nunca há mensalidade a pagar.
	if serv.Gratuito || len(serv.Meses) == 0 {
		c.JSON(http.StatusOK, gin.H{"pendencias": []finance.ServicoExtraPendenciaView{}})
		return
	}
	fim := time.Now()
	if s.Status == aggregates.StatusInscricaoCancelada {
		fim = s.UpdatedAt
	}
	out, err := apoio.FinanceiroService.PendenciasServicoExtra(c.Request.Context(), s.GetID().String(), serv.Meses, serv.Preco, s.VinculadaEm, fim)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"pendencias": out})
}

func MinhasPendenciasServicoExtra(c *gin.Context) {
	s, ok := solicFromParam(c)
	if !ok {
		return
	}
	id, _ := middleware.GetUserID(c)
	est, err := apoio.GetEstudanteProjection(c).GetByID(id)
	if err != nil || est == nil || s.CodigoEstudante != est.CodigoEstudante {
		utils.RespondWithForbiddenError(c, "solicitação não pertence ao estudante")
		return
	}
	pendenciasServicoExtra(c, s)
}

func PendenciasServicoExtraAcademia(c *gin.Context) {
	s, ok := solicFromParam(c)
	if !ok {
		return
	}
	codigo, _, ok := apoio.Academy(c)
	if !ok {
		return
	}
	if s.CodigoAcademia != codigo {
		utils.RespondWithForbiddenError(c, "solicitação não pertence à academia")
		return
	}
	pendenciasServicoExtra(c, s)
}

func IniciarPagamentoServicoExtraObrigacao(c *gin.Context) {
	// O solicitacao_id vem no corpo: no input do finance ele é json:"-" (nunca
	// lido do cliente por engano em outros fluxos), então é lido aqui à parte.
	var corpo struct {
		SolicitacaoID string `json:"solicitacao_id"`
		finance.ServicoExtraObrigacaoPagamentoInput
	}
	if c.ShouldBindJSON(&corpo) != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	in := corpo.ServicoExtraObrigacaoPagamentoInput
	in.SolicitacaoID = corpo.SolicitacaoID
	id, typ, _, ok := apoio.FinanceActor(c)
	if !ok || typ != "estudante" {
		utils.RespondWithForbiddenError(c, "somente o estudante pode iniciar pagamento")
		return
	}
	sid, err := uuid.Parse(in.SolicitacaoID)
	if err != nil {
		utils.RespondWithValidationError(c, errors.New("solicitacao_id inválido"))
		return
	}
	s, ok := apoio.LoadSolicitacaoServicoExtra(c, sid)
	if !ok {
		return
	}
	var estudante string
	if err = apoio.GetDBClient(c).DB().QueryRowContext(c.Request.Context(), `SELECT codigo_estudante FROM projection_estudantes WHERE id=$1`, id).Scan(&estudante); err != nil || s.CodigoEstudante != estudante {
		utils.RespondWithForbiddenError(c, "esta solicitação não pertence ao estudante autenticado")
		return
	}
	if s.Status != aggregates.StatusInscricaoVinculada {
		utils.RespondWithConflictError(c, "solicitação não possui obrigações pagáveis")
		return
	}
	serv, err := apoio.GetServicosRepo(c).ResolverServico(c.Request.Context(), s.ServicoExtraID, s.RotaID)
	if err != nil {
		utils.RespondWithNotFoundError(c, "serviço extra")
		return
	}
	if serv.Gratuito {
		utils.RespondWithConflictError(c, "este serviço é gratuito: não há mensalidade a pagar")
		return
	}
	// Só se paga um mês cobrado do serviço, a partir do mês do vínculo (sem
	// meses retroativos) e até o mês atual.
	inicio := time.Date(s.VinculadaEm.Year(), s.VinculadaEm.Month(), 1, 0, 0, 0, 0, time.UTC)
	alvo := time.Date(in.Ano, time.Month(in.Mes), 1, 0, 0, 0, 0, time.UTC)
	if !serv.MesCobrado(in.Mes) || alvo.Before(inicio) || alvo.After(time.Now()) {
		utils.RespondWithConflictError(c, "este mês não é cobrado neste serviço para esta inscrição")
		return
	}
	out, err := apoio.FinanceiroService.IniciarPagamentoServicoExtraObrigacao(c.Request.Context(), in, serv.CodigoAcademia, serv.Preco, serv.Metodos, c.ClientIP())
	if err != nil {
		apoio.FinanceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func alterarObrigacaoServicoExtra(c *gin.Context, reativar bool) {
	var in struct {
		SolicitacaoID  string `json:"solicitacao_id"`
		TipoLancamento string `json:"tipo_lancamento"`
		Ano            int    `json:"ano"`
		Mes            int    `json:"mes"`
		Motivo         string `json:"motivo"`
	}
	if c.ShouldBindJSON(&in) != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	sid, err := uuid.Parse(in.SolicitacaoID)
	if err != nil {
		utils.RespondWithValidationError(c, errors.New("solicitacao_id inválido"))
		return
	}
	s, ok := apoio.LoadSolicitacaoServicoExtra(c, sid)
	if !ok {
		return
	}
	codigo, actor, ok := apoio.Academy(c)
	if !ok {
		return
	}
	if s.CodigoAcademia != codigo {
		utils.RespondWithForbiddenError(c, "solicitação não pertence à academia")
		return
	}
	if reativar {
		err = apoio.FinanceiroService.ReativarObrigacaoServicoExtra(c.Request.Context(), in.SolicitacaoID, in.TipoLancamento, in.Ano, in.Mes, in.Motivo, actor.String(), "academia", c.ClientIP())
	} else {
		err = apoio.FinanceiroService.AnularObrigacaoServicoExtra(c.Request.Context(), in.SolicitacaoID, in.TipoLancamento, in.Ano, in.Mes, in.Motivo, actor.String(), "academia", c.ClientIP())
	}
	if err != nil {
		apoio.FinanceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func AnularObrigacaoServicoExtra(c *gin.Context) { alterarObrigacaoServicoExtra(c, false) }

func ReativarObrigacaoServicoExtra(c *gin.Context) { alterarObrigacaoServicoExtra(c, true) }

// pedidoSolicitacao é o que o estudante envia além do serviço: no transporte,
// a rota e os veículos dela.
type pedidoSolicitacao struct {
	RotaID     string
	VeiculoIDs []string
}

// lerPedidoSolicitacao lê rota_id e veiculo_ids do JSON ou do formulário
// (multipart, que também carrega o documento opcional). Em formulário,
// veiculo_ids pode vir repetido ou separado por vírgula.
func lerPedidoSolicitacao(c *gin.Context) (pedidoSolicitacao, bool) {
	var p pedidoSolicitacao
	if strings.HasPrefix(c.GetHeader("Content-Type"), "application/json") {
		var in struct {
			RotaID     string   `json:"rota_id"`
			VeiculoIDs []string `json:"veiculo_ids"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			utils.RespondWithValidationError(c, errors.New("payload inválido"))
			return p, false
		}
		p.RotaID, p.VeiculoIDs = in.RotaID, in.VeiculoIDs
		return p, true
	}
	_ = c.Request.ParseMultipartForm(apoio.MaxPDFUploadBytes + 1024)
	p.RotaID = strings.TrimSpace(c.PostForm("rota_id"))
	for _, v := range c.PostFormArray("veiculo_ids") {
		for _, parte := range strings.Split(v, ",") {
			if parte = strings.TrimSpace(parte); parte != "" {
				p.VeiculoIDs = append(p.VeiculoIDs, parte)
			}
		}
	}
	return p, true
}

// resolverPedido confere o serviço pedido (da academia do estudante, ativo,
// configurado) e, no transporte, a rota e os veículos: uma só rota e ao menos
// um veículo ATIVO dessa rota.
func resolverPedido(c *gin.Context, servicoID uuid.UUID, codigoAcademia string, p pedidoSolicitacao) (*cadastros.ServicoResolvido, *uuid.UUID, []uuid.UUID, bool) {
	repo := apoio.GetServicosRepo(c)
	ctx := c.Request.Context()
	base, err := repo.ResolverServico(ctx, servicoID, nil)
	if err != nil || base.CodigoAcademia != codigoAcademia || !base.Ativo {
		utils.RespondWithForbiddenError(c, "serviço extra indisponível")
		return nil, nil, nil, false
	}
	if base.Tipo == cadastros.TipoServicoAtividade {
		if p.RotaID != "" || len(p.VeiculoIDs) > 0 {
			utils.RespondWithValidationError(c, errors.New("rota e veículo só valem para o transporte"))
			return nil, nil, nil, false
		}
		if !base.Configurado() {
			utils.RespondWithConflictError(c, "serviço ainda não configurado pela academia: "+strings.Join(base.Problemas, "; "))
			return nil, nil, nil, false
		}
		return base, nil, nil, true
	}
	rid, err := uuid.Parse(p.RotaID)
	if err != nil || len(p.VeiculoIDs) == 0 {
		utils.RespondWithValidationError(c, errors.New("o transporte exige rota_id e ao menos um veiculo_ids"))
		return nil, nil, nil, false
	}
	serv, err := repo.ResolverServico(ctx, servicoID, &rid)
	if err != nil {
		utils.RespondWithValidationError(c, errors.New("rota não encontrada neste transporte"))
		return nil, nil, nil, false
	}
	if !serv.Configurado() {
		utils.RespondWithConflictError(c, "rota ainda não configurada pela academia: "+strings.Join(serv.Problemas, "; "))
		return nil, nil, nil, false
	}
	t, _, err := repo.ObterTransporte(ctx, codigoAcademia)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return nil, nil, nil, false
	}
	ativos := map[uuid.UUID]bool{}
	for _, r := range t.Rotas {
		if r.ID == rid {
			for _, v := range r.Veiculos {
				if v.Ativo {
					ativos[v.ID] = true
				}
			}
		}
	}
	veiculos := make([]uuid.UUID, 0, len(p.VeiculoIDs))
	for _, vs := range p.VeiculoIDs {
		v, err := uuid.Parse(vs)
		if err != nil || !ativos[v] {
			utils.RespondWithValidationError(c, errors.New("veículo inválido: escolha veículos ativos da rota escolhida"))
			return nil, nil, nil, false
		}
		veiculos = append(veiculos, v)
	}
	return serv, &rid, veiculos, true
}

// turmaDaAprovacao valida a turma da atividade escolhida ao aprovar: obrigatória
// na atividade coletiva (turma ativa da própria atividade) e recusada nos demais.
func turmaDaAprovacao(c *gin.Context, s *aggregates.SolicitacaoServicoExtra, serv *cadastros.ServicoResolvido, texto string) (*uuid.UUID, bool) {
	texto = strings.TrimSpace(texto)
	if !serv.Coletiva {
		if texto != "" {
			utils.RespondWithValidationError(c, errors.New("turma_id só vale para atividade coletiva"))
			return nil, false
		}
		return nil, true
	}
	id, err := uuid.Parse(texto)
	if err != nil {
		utils.RespondWithValidationError(c, errors.New("turma_id é obrigatório para aprovar uma atividade coletiva"))
		return nil, false
	}
	t, err := apoio.GetServicosRepo(c).TurmaDaAtividade(c.Request.Context(), s.ServicoExtraID, id)
	if err != nil || !t.Ativo {
		utils.RespondWithValidationError(c, errors.New("turma da atividade inválida ou desativada"))
		return nil, false
	}
	return &id, true
}
