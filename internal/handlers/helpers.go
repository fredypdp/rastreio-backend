package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/middleware"
	"spuri/internal/projections"
	"spuri/internal/services"
	"spuri/internal/storage"
)

// ============================================================================
// Repositório e cliente de banco
// ============================================================================

func getRepository(c *gin.Context) *db.AggregateRepository {
	repo, _ := c.Get("repository")
	return repo.(*db.AggregateRepository)
}

// getDbClient retorna o *db.Client injetado pelo middleware de setup.
//
// H4-14: se o client não estiver no contexto (bug de configuração do router),
// a requisição é abortada com 500 e nil NÃO é retornado — prevenindo panic.
// Criar um novo pool de conexões por requisição seria vazamento de recursos;
// o client deve sempre vir do contexto injetado em setupRouter.
func getDbClient(c *gin.Context) *db.Client {
	client, exists := c.Get("dbClient")
	if !exists {
		log.Printf("❌ [getDbClient] dbClient ausente no contexto Gin — abortando requisição. Path: %s", c.Request.URL.Path)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": "erro interno: cliente de banco de dados não disponível",
		})
		return nil
	}
	dbCli, ok := client.(*db.Client)
	if !ok || dbCli == nil {
		log.Printf("❌ [getDbClient] dbClient no contexto não é *db.Client válido. Path: %s", c.Request.URL.Path)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": "erro interno: cliente de banco de dados inválido",
		})
		return nil
	}
	return dbCli
}

// getDbClientFromContext é alias de getDbClient — mantido por compatibilidade.
func getDbClientFromContext(c *gin.Context) *db.Client {
	return getDbClient(c)
}

func getProjManager(c *gin.Context) *projections.Manager {
	raw, _ := c.Get("projManager")
	return raw.(*projections.Manager)
}

// ============================================================================
// Helpers de projeção
//
// Cada função instancia a projeção directamente via New...Projection(dbClient).
// O Manager não expõe GetProjection — as projeções não são recuperadas pelo
// Manager nos handlers; apenas RegisterProjection e StartProcessing são usados.
// ============================================================================

func getAdminProjection(c *gin.Context) *projections.AdminProjection {
	return projections.NewAdminProjection(getDbClient(c))
}

func getAcademiaProjection(c *gin.Context) *projections.AcademiaProjection {
	return projections.NewAcademiaProjection(getDbClient(c))
}

func getEstudanteProjection(c *gin.Context) *projections.EstudanteProjection {
	return projections.NewEstudanteProjection(getDbClient(c))
}

func getServicosExtrasProjection(c *gin.Context) *projections.ServicoExtraProjection {
	return projections.NewServicoExtraProjection(getDbClient(c))
}
func getCategoriasServicoProjection(c *gin.Context) *projections.CategoriaServicoProjection {
	return projections.NewCategoriaServicoProjection(getDbClient(c))
}
func getDocumentosExtraProjection(c *gin.Context) *projections.DocumentoExtraProjection {
	return projections.NewDocumentoExtraProjection(getDbClient(c))
}
func getRemetentesComunicacaoProjection(c *gin.Context) *projections.RemetenteComunicacaoProjection {
	return projections.NewRemetenteComunicacaoProjection(getDbClient(c))
}
func getMensagensComunicacaoProjection(c *gin.Context) *projections.MensagemComunicacaoProjection {
	return projections.NewMensagemComunicacaoProjection(getDbClient(c))
}
func getSolicitacoesServicoExtraProjection(c *gin.Context) *projections.SolicitacaoServicoExtraProjection {
	return projections.NewSolicitacaoServicoExtraProjection(getDbClient(c))
}

func getCursosProjection(c *gin.Context) *projections.CursosProjection {
	return projections.NewCursosProjection(getDbClient(c))
}

func getMateriasProjection(c *gin.Context) *projections.MateriasProjection {
	return projections.NewMateriasProjection(getDbClient(c))
}

func getSumariosProjection(c *gin.Context) *projections.SumariosProjection {
	return projections.NewSumariosProjection(getDbClient(c))
}

func getNotasProjection(c *gin.Context) *projections.NotasProjection {
	return projections.NewNotasProjection(getDbClient(c))
}

func getFaltasProjection(c *gin.Context) *projections.FaltasProjection {
	return projections.NewFaltasProjection(getDbClient(c))
}

func getAvaliacaoFinalProjection(c *gin.Context) *projections.AvaliacaoFinalProjection {
	return projections.NewAvaliacaoFinalProjection(getDbClient(c))
}

func getTurmasProjection(c *gin.Context) *projections.TurmasProjection {
	return projections.NewTurmasProjection(getDbClient(c))
}

func getCategoriasNotaProjection(c *gin.Context) *projections.CategoriasNotaProjection {
	return projections.NewCategoriasNotaProjection(getDbClient(c))
}

func getEmailService(c *gin.Context) *services.EmailService {
	return services.NewEmailService(getDbClient(c).DB())
}

func getStorageProvider(c *gin.Context) storage.StorageProvider {
	raw, exists := c.Get("storageProvider")
	if !exists || raw == nil {
		return nil
	}
	provider, _ := raw.(storage.StorageProvider)
	return provider
}

func getSolicitacaoAlteracaoNIFAcademiaProjection(c *gin.Context) *projections.SolicitacaoAlteracaoNIFAcademiaProjection {
	return projections.NewSolicitacaoAlteracaoNIFAcademiaProjection(getDbClient(c))
}

func getEstudanteVinculosProjection(c *gin.Context) *projections.EstudanteVinculosProjection {
	return projections.NewEstudanteVinculosProjection(getDbClient(c))
}

// ============================================================================
// Helpers de permissão admin
// ============================================================================

// verificarPermissaoAdmin verifica se o admin autenticado tem role >= minRole.
// Usado por handlers que precisam de autorização adicional além do middleware.
func verificarPermissaoAdmin(c *gin.Context, minRole string) error {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		return fmt.Errorf("usuário não autenticado")
	}
	adminProj := getAdminProjection(c)
	admin, err := adminProj.GetByID(userID)
	if err != nil || admin == nil {
		return fmt.Errorf("administrador não encontrado")
	}
	if admin.Status != "ativo" {
		return fmt.Errorf("administrador está inativo")
	}
	hierarchy := map[string]int{"fpp": 3, "adm": 2, "gerente": 1}
	if hierarchy[admin.Role] < hierarchy[minRole] {
		return fmt.Errorf("permissão negada: requer role '%s' ou superior", minRole)
	}
	return nil
}

// ============================================================================
// Helper de auditoria de ações admin
// ============================================================================

// registrarAcaoAdmin persiste uma ação administrativa no aggregate Admin
// via event sourcing. Falhas são apenas logadas — não abortam o handler
// principal, pois a operação primária já foi concluída com sucesso.
//
// Assinatura compatível com os handlers que chamam:
//
//	registrarAcaoAdmin(c, adminUserID, "acao", map[string]interface{}{...})
func registrarAcaoAdmin(c *gin.Context, adminID uuid.UUID, acao string, detalhes map[string]interface{}) {
	repository := getRepository(c)

	agg, err := repository.Load(adminID, "Admin")
	if err != nil {
		log.Printf("[WARN] registrarAcaoAdmin: falha ao carregar admin %s: %v", adminID, err)
		return
	}
	admin, ok := agg.(*aggregates.Admin)
	if !ok {
		log.Printf("[WARN] registrarAcaoAdmin: tipo de aggregate inesperado para admin %s", adminID)
		return
	}

	if err := admin.RegistrarAcao(acao, detalhes); err != nil {
		log.Printf("[WARN] registrarAcaoAdmin: falha ao registar ação '%s' para admin %s: %v", acao, adminID, err)
		return
	}

	audit := db.AuditContext{
		UserID:   adminID.String(),
		UserType: "admin",
		IP:       c.ClientIP(),
	}
	if err := repository.SaveWithAudit(admin, audit); err != nil {
		log.Printf("[WARN] registrarAcaoAdmin: falha ao persistir ação '%s' para admin %s: %v", acao, adminID, err)
	}
}

// ============================================================================
// Helpers de SQL
// ============================================================================

// getNullString converte sql.NullString para interface{} (nil se inválido).
// Usado em scans de queries que retornam colunas nullable.
func getNullString(ns sql.NullString) interface{} {
	if ns.Valid {
		return ns.String
	}
	return nil
}

// getNullUUID converte sql.NullString contendo UUID para interface{}.
func getNullUUID(ns sql.NullString) interface{} {
	if ns.Valid && ns.String != "" {
		if id, err := uuid.Parse(ns.String); err == nil {
			return id.String()
		}
	}
	return nil
}

// jsonToMap faz unmarshal de uma coluna JSON nullable para map.
// Retorna nil se o valor for NULL ou vazio — sem panic.
func jsonToMap(raw *string) map[string]interface{} {
	if raw == nil || *raw == "" {
		return nil
	}
	var m map[string]interface{}
	_ = json.Unmarshal([]byte(*raw), &m)
	return m
}

// ============================================================================
// Helpers de string
// ============================================================================

// maskEmail ofusca um endereço de email para exibição segura.
// Ex: "joao.silva@gmail.com" → "j*******a@gmail.com"
func maskEmail(email string) string {
	for i, ch := range email {
		if ch == '@' {
			local := email[:i]
			domain := email[i:]
			if len(local) <= 2 {
				return "**" + domain
			}
			return string(local[0]) + repeat("*", len(local)-2) + string(local[len(local)-1]) + domain
		}
	}
	return "***@***"
}

func repeat(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}

// ============================================================================
// Helper de isolamento de academia (SEC-15)
// ============================================================================

// AcessoAcademia representa o nível de acesso da academia ao estudante
type AcessoAcademia int

const (
	SemAcesso      AcessoAcademia = iota // Academia nunca teve vínculo
	AcessoAnterior                       // Academia já teve vínculo, hoje não tem (ou tem com outra)
	AcessoAtual                          // Academia tem vínculo ativo agora
)

// acessoAcademiaAoEstudante verifica o nível de acesso da academia ao estudante,
// consultando a projeção de vínculos (projection_estudante_vinculos).
// Retorna (nível de acesso, períodos de vínculo, erro).
// Usado em handlers de consulta (perfil, notas, faltas, avaliações, eventos) para
// forçar o isolamento por academia mesmo quando projection_estudantes.codigo_academia
// foi alterado ou o estudante está inativo/desvinculado.
func acessoAcademiaAoEstudante(c *gin.Context, codigoAcademia string, estudante *projections.EstudanteDTO) (AcessoAcademia, []projections.EstudanteVinculoDTO, error) {
	vinculosProj := getEstudanteVinculosProjection(c)
	periodos, err := vinculosProj.GetByEstudanteAcademia(estudante.ID, codigoAcademia)
	if err != nil {
		return SemAcesso, nil, err
	}
	if len(periodos) == 0 {
		// Rede de segurança: enquanto a projeção de vínculos ainda não alcançou o
		// ledger (logo após o deploy) a academia registada no estudante mantém o
		// acesso ATUAL, que é exatamente a regra anterior ao SEC-15. Sem períodos,
		// os eventos ficam vedados (filtrarEventosPorPeriodos falha fechado).
		if estudante.CodigoAcademia != nil && *estudante.CodigoAcademia == codigoAcademia && estudanteVinculado(estudante.Status) {
			return AcessoAtual, nil, nil
		}
		return SemAcesso, nil, nil
	}

	// Verificar se é vínculo atual (estudante vinculado e codigo_academia corresponde)
	if estudante.CodigoAcademia != nil && *estudante.CodigoAcademia == codigoAcademia && estudanteVinculado(estudante.Status) {
		return AcessoAtual, periodos, nil
	}
	return AcessoAnterior, periodos, nil
}

// estudanteVinculado verifica se o estudante tem vínculo ativo (ativo ou pendente_documentos)
func estudanteVinculado(status string) bool {
	return status == "ativo" || status == "pendente_documentos"
}

// ============================================================================
// Helpers de filtro de eventos por período de vínculo (SEC-15)
// ============================================================================

// filtrarEventosPorPeriodos filtra uma lista de eventos mantendo apenas aqueles
// cujo occurred_at cai dentro de algum dos períodos de vínculo fornecidos.
// Inclui os limites: occurred_at >= vinculado_em e (desvinculado_em == nil ou occurred_at <= desvinculado_em).
//
// Sem períodos conhecidos NENHUM evento é devolvido (falha fechada): devolver
// tudo exporia o histórico de outras academias.
func filtrarEventosPorPeriodos(eventos []db.Event, periodos []projections.EstudanteVinculoDTO) []db.Event {
	if len(periodos) == 0 {
		return []db.Event{}
	}
	filtrados := make([]db.Event, 0, len(eventos))
	for _, e := range eventos {
		if eventoDentroDoPeriodo(&e, periodos) {
			filtrados = append(filtrados, e)
		}
	}
	return filtrados
}

// margemInicioVinculo tolera a diferença entre occurred_at (momento em que o
// agregado gerou o evento) e recorded_at (momento da gravação no ledger).
const margemInicioVinculo = 2 * time.Second

// eventoDentroDoPeriodo verifica se o evento ocorreu dentro de algum dos períodos de vínculo.
func eventoDentroDoPeriodo(evento *db.Event, periodos []projections.EstudanteVinculoDTO) bool {
	for _, p := range periodos {
		// O evento que abre o vínculo é gravado milissegundos depois de ocorrer:
		// a margem evita esconder à academia o próprio evento de criação.
		if evento.OccurredAt.Before(p.VinculadoEm.Add(-margemInicioVinculo)) {
			continue
		}
		if p.DesvinculadoEm != nil && evento.OccurredAt.After(*p.DesvinculadoEm) {
			continue
		}
		return true
	}
	return false
}

// cancelarInscricoesServicoExtraAoSair cancela todas as inscrições ativas em serviços extras
// de um estudante ao ser desvinculado ou deletado de uma academia.
// Retorna erro se houver falha crítica, mas continua cancelando as demais.
func cancelarInscricoesServicoExtraAoSair(c *gin.Context, codigoEstudante, codigoAcademia, motivo, canceladaPor string) error {
	solicitacoesProj := getSolicitacoesServicoExtraProjection(c)
	solicitacaoIDs, err := solicitacoesProj.GetAtivasPorEstudanteAcademia(codigoEstudante, codigoAcademia)
	if err != nil {
		return fmt.Errorf("falha ao buscar inscrições ativas: %w", err)
	}

	repository := getRepository(c)
	for _, solicitacaoID := range solicitacaoIDs {
		agg, err := repository.Load(solicitacaoID, "SolicitacaoServicoExtra")
		if err != nil {
			log.Printf("[WARN] cancelarInscricoesServicoExtraAoSair: falha ao carregar solicitação %s: %v", solicitacaoID, err)
			continue
		}
		solicitacao, ok := agg.(*aggregates.SolicitacaoServicoExtra)
		if !ok {
			log.Printf("[WARN] cancelarInscricoesServicoExtraAoSair: tipo de aggregate inesperado para solicitação %s", solicitacaoID)
			continue
		}

		var cancelErr error
		if solicitacao.Status == aggregates.StatusInscricaoPendente || solicitacao.Status == aggregates.StatusInscricaoAprovadaPendentePagamentoTaxa {
			cancelErr = solicitacao.CancelarAntesDaVinculacao(motivo, canceladaPor)
		} else if solicitacao.Status == aggregates.StatusInscricaoVinculada {
			cancelErr = solicitacao.Cancelar(motivo, canceladaPor)
		}

		if cancelErr != nil {
			log.Printf("[WARN] cancelarInscricoesServicoExtraAoSair: falha ao cancelar solicitação %s: %v", solicitacaoID, cancelErr)
			continue
		}

		audit := db.AuditContext{
			UserID:   codigoAcademia,
			UserType: "academia",
			IP:       c.ClientIP(),
		}
		if err := repository.SaveWithAudit(solicitacao, audit); err != nil {
			log.Printf("[WARN] cancelarInscricoesServicoExtraAoSair: falha ao persistir cancelamento da solicitação %s: %v", solicitacaoID, err)
			continue
		}
	}
	return nil
}
