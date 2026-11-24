package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"golang.org/x/time/rate"

	"spuri/internal/db"
	"spuri/internal/finance"
	"spuri/internal/handlers"
	"spuri/internal/handlers/academias"
	"spuri/internal/handlers/administradores"
	"spuri/internal/handlers/apoio"
	"spuri/internal/handlers/autenticacao"
	"spuri/internal/handlers/avaliacoes"
	"spuri/internal/handlers/comunicacao"
	"spuri/internal/handlers/contas"
	"spuri/internal/handlers/cursos"
	"spuri/internal/handlers/documentos"
	"spuri/internal/handlers/estudantes"
	"spuri/internal/handlers/faltas"
	"spuri/internal/handlers/lotes"
	"spuri/internal/handlers/materias"
	"spuri/internal/handlers/notas"
	"spuri/internal/handlers/registros"
	"spuri/internal/handlers/servicosextras"
	"spuri/internal/handlers/sumarios"
	"spuri/internal/handlers/turmas"
	"spuri/internal/jobs"
	"spuri/internal/middleware"
	"spuri/internal/projections"
	"spuri/internal/services"
	"spuri/internal/storage"
	"spuri/internal/sysconfig"
	"spuri/internal/utils"
)

var (
	dbClient        *db.Client
	repository      *db.AggregateRepository
	projManager     *projections.Manager
	jobStore        *jobs.Store
	jobWorker       *jobs.Worker
	jobNotifier     *jobs.Notifier
	storageProvider storage.StorageProvider
)

// requestIDPattern define os caracteres permitidos num X-Request-ID externo.
var requestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9\-]{1,64}$`)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	env := strings.ToLower(strings.TrimSpace(os.Getenv("ENV")))
	if env != "production" {
		if err := godotenv.Load(); err != nil {
			log.Println("[WARN] Arquivo .env não encontrado")
		}
	}
	if err := middleware.ValidateJWTConfig(); err != nil {
		log.Fatalf("[FATAL] configuração JWT inválida: %v", err)
	}

	if err := initDB(); err != nil {
		log.Fatalf("[ERROR] Erro ao conectar ao banco de dados: %v", err)
	}
	defer dbClient.Close()

	if err := initStorage(); err != nil {
		log.Fatalf("[ERROR] Erro ao inicializar armazenamento: %v", err)
	}

	if err := initProjections(); err != nil {
		log.Fatalf("[ERROR] Erro ao inicializar projeções: %v", err)
	}

	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()
	initJobs(ctx)
	initReconciliacao(ctx)

	if env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := setupRouter()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if err := router.Run("0.0.0.0:" + port); err != nil {
		log.Fatalf("[ERROR] Erro ao iniciar servidor: %v", err)
	}
}

func initDB() error {
	if err := finance.ValidateEncryptionConfig(); err != nil {
		return fmt.Errorf("configuração de criptografia financeira inválida: %w", err)
	}
	if err := services.ValidateComunicacaoEncryptionConfig(); err != nil {
		return fmt.Errorf("configuração de criptografia de comunicação inválida: %w", err)
	}
	config := db.DefaultConfig()
	var err error
	dbClient, err = db.NewClient(config)
	if err != nil {
		return err
	}
	repository = db.NewAggregateRepository(dbClient)
	apoio.FinanceiroService = finance.NewService(dbClient)
	if err := dbClient.Health(); err != nil {
		return err
	}
	if err := dbClient.RunMigrations(); err != nil {
		return fmt.Errorf("erro ao rodar migrations: %w", err)
	}

	// Configurações editáveis pela tela de administração (Parte 3): daqui em
	// diante sysconfig.String lê o banco e, se não houver, a variável de ambiente.
	// Tem de vir depois das migrations (a tabela nasce na 139) e antes de tudo
	// que lê configuração: a validação dos provedores abaixo, o storage e os jobs.
	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteSQL{DB: dbClient.DB().DB}, sysconfig.TTLPadrao))
	if err := finance.ValidarConfiguracaoProvedores(); err != nil {
		return fmt.Errorf("configuração de provedor de pagamento inválida: %w", err)
	}
	log.Println("[INFO] Banco de dados inicializado com Event Sourcing")
	return nil
}

func initStorage() error {
	provider, err := storage.NewStorageProvider()
	if err != nil {
		return err
	}
	storageProvider = provider
	logStorageProviderStartup(provider)
	return nil
}

// logStorageProviderStartup registra, de forma bem visível, qual backend de
// armazenamento de arquivos está realmente ativo nesta instância. Existe
// porque um provider em modo de fallback local historicamente não deixava
// nenhum rastro nos logs de inicialização — ver
// docs/Debbugs/Depurar arquivos de alvara nao chegando ao Mega real.md.
func logStorageProviderStartup(provider storage.StorageProvider) {
	name := provider.ProviderName()
	if name != "mega-local" {
		rootFolder := strings.Trim(strings.TrimSpace(sysconfig.String("MEGA_ROOT_FOLDER")), "/")
		log.Printf("[INFO] armazenamento de arquivos: Mega remoto ativo (provider=%q, root_folder=%q)", name, rootFolder)
		return
	}

	localRoot := strings.TrimSpace(os.Getenv("MEGA_LOCAL_ROOT"))
	if localRoot == "" {
		localRoot = "data/mega_storage"
	}
	env := strings.ToLower(strings.TrimSpace(os.Getenv("ENV")))
	if env == "production" {
		log.Printf("[ALERTA] armazenamento de arquivos rodando em MODO LOCAL (provider=%q) com ENV=production — alvarás e outros documentos NÃO estão sendo enviados ao Mega real, ficam em %q e podem ser perdidos em redeploys. Verifique a variável STORAGE_PROVIDER (deve ser \"mega\", nunca \"local\", em produção).", name, localRoot)
		return
	}
	log.Printf("[INFO] armazenamento de arquivos: modo local (provider=%q), diretório=%q, ambiente=%q", name, localRoot, env)
}

func initProjections() error {
	projManager = projections.NewManager(dbClient)

	// ── Tier 1 — sem dependências externas ───────────────────────────────
	projManager.RegisterProjection("admins", projections.NewAdminProjection(dbClient))
	projManager.RegisterProjection("contas", projections.NewContasProjection(dbClient))
	projManager.RegisterProjection("academias", projections.NewAcademiaProjection(dbClient))
	projManager.RegisterProjection("servicos_extras", projections.NewServicoExtraProjection(dbClient))
	projManager.RegisterProjection("categorias_servico", projections.NewCategoriaServicoProjection(dbClient))
	projManager.RegisterProjection("documentos_extra", projections.NewDocumentoExtraProjection(dbClient))
	projManager.RegisterProjection("remetentes_comunicacao", projections.NewRemetenteComunicacaoProjection(dbClient))
	projManager.RegisterProjection("mensagens_comunicacao", projections.NewMensagemComunicacaoProjection(dbClient))
	projManager.RegisterProjection("solicitacoes_servico_extra", projections.NewSolicitacaoServicoExtraProjection(dbClient))
	projManager.RegisterProjection("sumarios", projections.NewSumariosProjection(dbClient))
	projManager.RegisterProjection("categorias_nota", projections.NewCategoriasNotaProjection(dbClient))

	// ── Tier 2 — dependem de academias/cursos ────────────────────────────
	projManager.RegisterProjection("estudantes", projections.NewEstudanteProjection(dbClient))
	projManager.RegisterProjection("estudante_vinculos", projections.NewEstudanteVinculosProjection(dbClient))
	projManager.RegisterProjection("turmas", projections.NewTurmasProjection(dbClient))

	// ── Tier 3 — dependem de estudantes e materias ───────────────────────
	projManager.RegisterProjection("notas", projections.NewNotasProjection(dbClient))
	projManager.RegisterProjection("faltas", projections.NewFaltasProjection(dbClient))
	projManager.RegisterProjection("faltas_configuracao", projections.NewFaltasConfiguracaoProjection(dbClient))
	projManager.RegisterProjection("configuracao_sistema", projections.NewConfiguracaoSistemaProjection(dbClient))

	// ── Tier 4 — avaliação final ──────────────────────────────────────────
	projManager.RegisterProjection("avaliacao_final", projections.NewAvaliacaoFinalProjection(dbClient))
	projManager.RegisterProjection("solicitacoes_edicao_dados_estudante", projections.NewSolicitacaoEdicaoDadoEstudanteProjection(dbClient))
	projManager.RegisterProjection("solicitacoes_alteracao_nif_academia", projections.NewSolicitacaoAlteracaoNIFAcademiaProjection(dbClient))
	projManager.RegisterProjection("financeiro", projections.NewFinanceiroProjection(dbClient))

	db.SetLedgerWriteHook(projManager.Wake)

	go projManager.StartProcessing()
	return nil
}

// initJobs inicializa o sistema de jobs assíncronos.
// setupCtx injeta todas as dependências que os handlers individuais precisam.
func initJobs(ctx context.Context) {
	jobStore = jobs.NewStore(dbClient.DB())
	jobNotifier = jobs.NewNotifier()

	setupCtx := func(c *gin.Context, userID uuid.UUID, userType string) {
		c.Set("user_id", userID)
		c.Set("user_type", userType)
		c.Set("dbClient", dbClient)
		c.Set("repository", repository)
		c.Set("projManager", projManager)
		c.Set("jobStore", jobStore)
		c.Set("jobNotifier", jobNotifier)
		c.Set("storageProvider", storageProvider)
	}

	// 4 goroutines paralelas — ajustar conforme recursos do servidor
	jobWorker = jobs.NewWorker(jobStore, jobNotifier, setupCtx, 4)

	// ── Handlers de job por tipo ──────────────────────────────────────────
	jobWorker.RegisterHandler(jobs.JobTypeRegisterAcademiaBatch, academias.RegisterAcademia)
	jobWorker.RegisterHandler(jobs.JobTypeAtivarAcademiaBatch, academias.AtivarAcademiaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeDesativarAcademiaBatch, academias.DesativarAcademiaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeRegisterEstudanteBatch, estudantes.RegisterEstudantePorAcademiaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeRegistrarNotaBatch, notas.RegistrarNota)
	jobWorker.RegisterHandler(jobs.JobTypeRegistrarFaltasBatch, faltas.RegistrarFaltas)
	jobWorker.RegisterHandler(jobs.JobTypeCriarCursoBatch, cursos.CriarCurso)
	jobWorker.RegisterHandler(jobs.JobTypeCriarMateriaBatch, materias.CriarMateria)
	jobWorker.RegisterHandler(jobs.JobTypeCriarTurmaBatch, turmas.CriarTurma)
	jobWorker.RegisterHandler(jobs.JobTypeAdicionarEstudanteBatch, turmas.AdicionarEstudanteATurmaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeAtualizarDadosAcademiaBatch, academias.AtualizarDadosAcademiaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeCriarCategoriaNotaBatch, notas.CriarCategoriaNotaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeDeletarCategoriaNotaBatch, notas.DeletarCategoriaNotaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeAtivarCursoBatch, cursos.AtivarCursoJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeDesativarCursoBatch, cursos.DesativarCursoJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeAtualizarDadosCursoBatch, cursos.AtualizarDadosCursoJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeDeletarCursoBatch, cursos.DeletarCursoJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeAtivarMateriaBatch, materias.AtivarMateriaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeDesativarMateriaBatch, materias.DesativarMateriaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeAtualizarDadosMateriaBatch, materias.AtualizarDadosMateriaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeDeletarMateriaBatch, materias.DeletarMateriaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeAtivarTurmaBatch, turmas.AtivarTurmaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeDesativarTurmaBatch, turmas.DesativarTurmaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeAtualizarDadosTurmaBatch, turmas.AtualizarDadosTurmaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeDeletarTurmaBatch, turmas.DeletarTurmaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeRemoverEstudanteTurmaBatch, turmas.RemoverEstudanteTurmaJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeAtivarAdminBatch, administradores.AtivarAdminJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeDesativarAdminBatch, administradores.DesativarAdminJobItem)
	jobWorker.RegisterHandler(jobs.JobTypeRebuildProjection, administradores.RebuildProjectionJobItem)

	jobWorker.Start(ctx)
	log.Println("[INFO] Job worker iniciado com 4 goroutines")
}

// initReconciliacao inicia o job que reconcilia cobranças pendentes com o
// provedor (rede de segurança dos webhooks). Uma falha aqui não impede a API de
// subir: o webhook e a consulta manual continuam funcionando.
func initReconciliacao(ctx context.Context) {
	apoio.FinanceiroService.SetEfeitosPagamento(handlers.NovosEfeitosPagamento(dbClient, repository))
	cfg := finance.ConfigReconciliacaoDoAmbiente()
	if !cfg.Ativa {
		log.Println("[INFO] Reconciliação de cobranças desativada (RECONCILIACAO_ATIVA=false)")
		return
	}
	if err := apoio.FinanceiroService.IniciarReconciliacao(ctx, cfg); err != nil {
		log.Printf("[ERROR] Reconciliação de cobranças não iniciou: %v", err)
		return
	}
	log.Println("[INFO] Reconciliação de cobranças iniciada")
}

func setupRouter() *gin.Engine {
	router := gin.New()

	router.Use(gin.RecoveryWithWriter(gin.DefaultErrorWriter, func(c *gin.Context, recovered interface{}) {
		log.Printf("[PANIC] Recuperado: %v — IP: %s — Path: %s", recovered, c.ClientIP(), c.Request.URL.Path)
		utils.RespondWithInternalError(c, fmt.Errorf("panic recuperado"))
	}))

	router.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Content-Security-Policy", "default-src 'none'")
		if strings.EqualFold(strings.TrimSpace(os.Getenv("ENV")), "production") {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	})

	router.Use(corsMiddleware())
	router.Use(requestIDMiddleware())
	router.Use(middleware.MonitoringMiddleware())

	// Injetar dependências no contexto de cada requisição
	router.Use(func(c *gin.Context) {
		c.Set("dbClient", dbClient)
		c.Set("repository", repository)
		c.Set("projManager", projManager)
		c.Set("jobStore", jobStore)
		c.Set("jobWorker", jobWorker)
		c.Set("jobNotifier", jobNotifier)
		c.Set("storageProvider", storageProvider)
		c.Next()
	})

	// ── Rotas públicas ─────────────────────────────────────────────────────
	router.GET("/health", func(c *gin.Context) {
		if err := dbClient.Health(); err != nil {
			utils.RespondWithServiceUnavailable(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.POST("/login", autenticacao.Login)
	// Segunda etapa do login com verificação em duas etapas (tarefa 122). Públicas:
	// o desafio_id devolvido por POST /login é a prova de que a senha estava correta.
	router.POST("/login/dois-fatores/verificar", autenticacao.VerificarLoginDoisFatores)
	router.POST("/login/dois-fatores/reenviar", autenticacao.ReenviarLoginDoisFatores)
	router.POST("/bootstrap", administradores.BootstrapAdminFPP)
	// SEC-08 (Tarefa 127): cada cadastro público dispara um e-mail aos admins; o
	// limite por IP (rajada de 10, reposição de 1 a cada 3 min) trava o envio em
	// massa sem afetar o uso normal (cadastros de academias são raros).
	router.POST("/academia/cadastro", middleware.RateLimitMiddleware(middleware.NewRateLimiter(rate.Every(3*time.Minute), 10, time.Hour)), academias.RegisterAcademiaPublica)
	// Webhooks são públicos por necessidade do gateway, mas autenticados pelo
	// próprio módulo antes de qualquer evento ser aceite.
	// As rotas de cada provedor vêm do próprio provedor (RotasWebhook), no
	// formato /webhooks/<slug>/<caminho>. Para a AppyPay continuam exatamente
	// as URLs já configuradas no painel deles (/webhooks/appypay/gpo e /ref).
	for _, p := range apoio.FinanceiroService.Provedores() {
		for _, rota := range apoio.FinanceiroService.RotasWebhookDoProvedor(p.ID) {
			router.POST("/webhooks/"+p.Slug+"/"+rota.Caminho, handlers.ReceberWebhook(p.ID, rota.Metodo))
		}
	}

	// E-mail: verificação e recuperação de senha. Tarefa 123: desapareceram as
	// rotas gerar-token/* (devolviam o token em claro ao chamador) e o
	// "limitador de e-mail" desativado. A recuperação é por LINK enviado ao e-mail
	// (SEC-03); "/verificar-email/solicitar" passou a exigir autenticação (antes
	// estava fora do grupo autenticado e falhava sempre no handler).
	emailGroup := router.Group("/email")
	{
		emailGroup.POST("/verificar-email/:token", autenticacao.VerificarEmail)
		emailGroup.POST("/recuperar-senha/solicitar", autenticacao.SolicitarRecuperacaoSenha)
		emailGroup.POST("/recuperar-senha/:token", autenticacao.ResetarSenha)

		emailAuthGroup := emailGroup.Group("/")
		emailAuthGroup.Use(middleware.AuthMiddleware())
		{
			emailAuthGroup.POST("/verificar-email/solicitar", autenticacao.SolicitarVerificacaoEmail)
		}
	}

	// ── Rotas públicas com autenticação opcional ─────────────────────────
	router.GET("/academias", middleware.OptionalAuthMiddleware(), academias.ListarTodasAcademias)
	router.GET("/academia/cursos", middleware.OptionalAuthMiddleware(), cursos.ListarCursos)
	router.GET("/academia/servico/:codigo_academia/servicos-extras", middleware.OptionalAuthMiddleware(), apoio.ListarServicosExtrasPublico)
	router.GET("/academia/curso/:id", middleware.OptionalAuthMiddleware(), cursos.GetCurso)
	router.GET("/consultar-academia/:codigo", middleware.OptionalAuthMiddleware(), academias.GetAcademiaPorCodigo)

	// ── Rotas de jobs assíncronos (qualquer usuário autenticado) ──────────
	// SEC-12: academia inativa não pode acessar jobs (envio de SMS, etc.)
	jobRoutes := router.Group("/jobs")
	jobRoutes.Use(middleware.AuthMiddleware())
	jobRoutes.Use(middleware.ValidarStatusAcademia())
	{
		jobRoutes.GET("", lotes.ListJobs)
		jobRoutes.GET("/stream", lotes.StreamJobs)
		jobRoutes.DELETE("/:id/sse", lotes.HideJobFromSSE)
		jobRoutes.POST("/:id/retry-failed", lotes.RetryFailedJob)
		jobRoutes.GET("/:id", lotes.GetJob)
	}

	// ── Rota isolada de integração Ziett (teste de SMS) ──────────────────
	integracoes := router.Group("/integracoes")
	integracoes.Use(middleware.AuthMiddleware())
	integracoes.Use(middleware.RequireFPP())
	{
		integracoes.POST("/ziett/mensagens/teste", comunicacao.EnviarMensagemTesteZiettSMS)
	}

	// ── Módulo de comunicação (base): enviar/listar mensagens ────────────
	// Disponível para administradores (qualquer role) e academias. Uma
	// academia só vê/envia em nome de si própria; um admin vê tudo, com
	// filtro opcional por codigo_academia na listagem.
	// SEC-12: academia inativa não pode enviar mensagens (custo da plataforma)
	comunicacaoMensagens := router.Group("/comunicacao")
	comunicacaoMensagens.Use(middleware.AuthMiddleware())
	comunicacaoMensagens.Use(middleware.RequireAcademiaOuAdmin())
	comunicacaoMensagens.Use(middleware.ValidarStatusAcademia())
	{
		comunicacaoMensagens.POST("/mensagens", comunicacao.EnviarMensagemComunicacao)
		comunicacaoMensagens.GET("/mensagens", comunicacao.ListarMensagensComunicacao)
	}

	// ── Módulo de comunicação (base): remetentes (apenas administradores) ─
	// Cadastrar exige role FPP (checado dentro do handler); listar aceita
	// qualquer role de administrador.
	comunicacaoAdmin := router.Group("/comunicacao")
	comunicacaoAdmin.Use(middleware.AuthMiddleware())
	comunicacaoAdmin.Use(middleware.RequireAdmin())
	{
		comunicacaoAdmin.POST("/remetentes", comunicacao.CriarRemetenteComunicacao)
		comunicacaoAdmin.GET("/remetentes", comunicacao.ListarRemetentesComunicacao)
	}

	// ── Módulo de comunicação (base): provedor padrão (apenas FPP) ───────
	adminComunicacao := router.Group("/admin/comunicacao")
	adminComunicacao.Use(middleware.AuthMiddleware())
	adminComunicacao.Use(middleware.RequireAdmin())
	{
		adminComunicacao.GET("/provedor-padrao", comunicacao.ConsultarProvedorPadraoComunicacao)
		adminComunicacao.PUT("/provedor-padrao", comunicacao.DefinirProvedorPadraoComunicacao)
	}

	// ── Rotas de conta (qualquer tipo, sem gate de estado da academia) ──────
	// Permitem que academia inativa acesse sua própria conta (logout, senha, perfil, 2FA).
	conta := router.Group("/")
	conta.Use(middleware.AuthMiddleware())
	{
		conta.POST("/logout", autenticacao.Logout)
		conta.PUT("/alterar-senha", autenticacao.AlterarSenha)
		conta.GET("/meu-perfil", autenticacao.GetMeuPerfil)
		conta.PUT("/me/email", autenticacao.AtualizarMeuEmail)
		conta.PUT("/me/telefone", autenticacao.AtualizarMeuTelefone)
		// Verificação em duas etapas por e-mail (tarefa 122)
		conta.GET("/dois-fatores", autenticacao.ObterDoisFatores)
		conta.POST("/dois-fatores/ativacao/solicitar", autenticacao.SolicitarAtivacaoDoisFatores)
		conta.POST("/dois-fatores/ativacao/confirmar", autenticacao.ConfirmarAtivacaoDoisFatores)
		conta.POST("/dois-fatores/desativacao/solicitar", autenticacao.SolicitarDesativacaoDoisFatores)
		conta.POST("/dois-fatores/desativacao/confirmar", autenticacao.ConfirmarDesativacaoDoisFatores)
	}

	// ── Rotas autenticadas restantes (bloqueadas para academia inativa) ───
	// SEC-12: ValidarStatusAcademia aplica-se a tudo que não for rota de conta.
	protected := router.Group("/")
	protected.Use(middleware.AuthMiddleware())
	protected.Use(middleware.ValidarStatusAcademia())
	{
		protected.GET("/eventos-estudante/:codigo", estudantes.GetEventosEstudante)
		protected.GET("/eventos/:event_id", estudantes.GetEventoAuditoria)
		protected.GET("/verificar-integridade/:codigo", estudantes.VerificarIntegridade)
		protected.GET("/consultar-estudante/:codigo", estudantes.GetEstudantePorCodigo)
		protected.GET("/estudantes", middleware.RequireAcademiaOuAdmin(), estudantes.ListarEstudantes)
		// Tarefa 123 (SEC-04): reenvio de credenciais temporárias (academia dona ou admin fpp).
		protected.POST("/estudantes/:codigo/credenciais/reenviar", middleware.RequireAcademiaOuAdmin(), autenticacao.ReenviarCredenciaisEstudante)
		protected.GET("/avaliacoes", avaliacoes.ListarAvaliacoes)
		protected.GET("/aprovacoes", avaliacoes.ListarAprovacoes)
		protected.GET("/reprovacoes", avaliacoes.ListarReprovacoes)
		protected.GET("/notas", registros.ListarNotas)
		protected.GET("/faltas", registros.ListarFaltas)
		protected.GET("/notas-estudante/:codigo", notas.GetNotasEstudante)
		protected.GET("/faltas-estudante/:codigo", faltas.GetFaltasEstudante)
		protected.GET("/ano-letivo", administradores.GetAnoLetivoGlobalSistemaAtual)
		protected.GET("/anos-letivos-lista", administradores.GetAnosLetivosGlobaisLista)
		protected.GET("/anos-letivos/configuracoes", administradores.ListarConfiguracoesAnosLetivos)
		protected.GET("/documentos/academias/:codigo/:campo/download", documentos.DownloadDocumentoAcademia)
		protected.POST("/documentos/academias/:codigo/:campo/upload", documentos.UploadDocumentoAcademia)
		protected.GET("/documentos/estudantes/:codigo/:campo/download", documentos.DownloadDocumentoEstudante)
		protected.GET("/avaliacoes-estudante/:codigo", middleware.RequireAcademiaOuAdmin(), avaliacoes.GetAvaliacoesFinaisEstudante)
		protected.GET("/turmas-estudante/:codigo", turmas.GetTurmasEstudante)
		// Consulta de mensalidades também é acessível ao próprio estudante; as
		// demais ações financeiras ficam no grupo academia/admin abaixo.
		protected.GET("/financeiro/mensalidades/estudante/:codigo", handlers.ConsultarMensalidadesEstudante)
		protected.POST("/financeiro/mensalidades/pagamento", handlers.IniciarPagamentoMensalidades)
		protected.POST("/financeiro/servicos-extras/taxa-inscricao/pagamento", servicosextras.IniciarPagamentoTaxaInscricaoServicoExtra)
		protected.POST("/financeiro/servicos-extras/obrigacao/pagamento", servicosextras.IniciarPagamentoServicoExtraObrigacao)
		// Idem para o histórico de cobranças do estudante (mensalidade, matrícula
		// e avulsas, em qualquer estado) — ver tarefa 48.
		protected.GET("/financeiro/cobrancas/estudante/:codigo", handlers.ConsultarCobrancasEstudante)

		financeiro := protected.Group("/financeiro")
		financeiro.Use(middleware.RequireAcademiaOuAdmin())
		{
			// Provedores de pagamento (PSP): catálogo, provedor ativo do
			// contexto e credenciais por provedor.
			financeiro.GET("/provedores", handlers.ListarProvedores)
			financeiro.GET("/provedor", handlers.ConsultarProvedorContexto)
			financeiro.PUT("/provedor", handlers.DefinirProvedorContexto)
			financeiro.POST("/credenciais", handlers.ConfigurarCredencial)
			financeiro.PUT("/credenciais/:id", handlers.AtualizarCredencial)
			financeiro.GET("/credenciais", handlers.ListarCredenciais)
			financeiro.DELETE("/credenciais", handlers.RemoverCredencial)
			financeiro.GET("/credenciais/:id/webhook-secret", handlers.ConsultarSegredoWebhook)
			financeiro.POST("/credenciais/:id/webhook-secret/rotacionar", handlers.RotacionarSegredoWebhook)
			// Cobranças (independentes de provedor: o provedor de cada cobrança
			// é o que a emitiu).
			financeiro.POST("/cobrancas", handlers.CriarCobranca)
			financeiro.POST("/qr-codes", handlers.GerarQRCode)
			financeiro.GET("/cobrancas/:id", handlers.ConsultarCobranca)
			financeiro.POST("/cobrancas/:id/cancelar", handlers.CancelarCobranca)
			financeiro.POST("/cobrancas/:id/pago-externamente", handlers.RegistrarPagamentoExternoCobranca)
			financeiro.GET("/cobrancas", handlers.ListarCobrancas)
			financeiro.POST("/servicos-extras/obrigacao/anular", servicosextras.AnularObrigacaoServicoExtra)
			financeiro.POST("/servicos-extras/obrigacao/reativar", servicosextras.ReativarObrigacaoServicoExtra)
			financeiro.POST("/mensalidades/configuracoes", handlers.ConfigurarMensalidade)
			financeiro.PUT("/mensalidades/configuracoes", handlers.ConfigurarMensalidade)
			financeiro.GET("/mensalidades/configuracoes", handlers.ListarConfiguracoesMensalidade)
			financeiro.DELETE("/mensalidades/configuracoes", handlers.RemoverConfiguracaoMensalidade)
			financeiro.POST("/mensalidades/inicio-cobranca", handlers.DefinirMesInicioCobranca)
			financeiro.GET("/mensalidades/inicio-cobranca", handlers.ConsultarMesInicioCobranca)
			financeiro.DELETE("/mensalidades/inicio-cobranca", handlers.RemoverMesInicioCobranca)
			financeiro.POST("/mensalidades/obrigacoes/anular", handlers.AnularObrigacoesMensalidade)
			financeiro.POST("/mensalidades/obrigacoes/reativar", handlers.ReativarObrigacoesMensalidade)
			financeiro.POST("/mensalidades/obrigacoes/pago-externamente", handlers.RegistrarPagamentoExternoMensalidades)
		}
	}

	// ── Rotas exclusivas do estudante ─────────────────────────────────────
	estudante := router.Group("/estudante")
	estudante.Use(middleware.AuthMiddleware())
	estudante.Use(middleware.RequireEstudante())
	{
		estudante.GET("/servicos-extras/catalogo", apoio.ListarServicosExtrasCatalogoEstudante)
		estudante.POST("/servicos-extras/:id/solicitacao", servicosextras.SolicitarServicoExtra)
		estudante.GET("/servicos-extras/minhas-inscricoes", servicosextras.ListarMinhasInscricoesServicoExtra)
		estudante.GET("/servicos-extras/minhas-inscricoes/:id/documento/download", servicosextras.DownloadDocumentoSolicitacaoServicoExtraEstudante)
		estudante.GET("/servicos-extras/minhas-inscricoes/:id/pendencias", servicosextras.MinhasPendenciasServicoExtra)
		estudante.PUT("/servicos-extras/minhas-inscricoes/:id/cancelar", servicosextras.CancelarSolicitacaoServicoExtraEstudante)
		estudante.GET("/solicitacoes-edicao", estudantes.ListarSolicitacoesEdicaoEstudante)
		estudante.GET("/solicitacoes-edicao/:codigo/documento/download", documentos.DownloadDocumentoSolicitacaoEdicaoEstudante)
		estudante.PUT("/bilhete-identidade", estudantes.AtualizarBilheteIdentidadeSemAcademia)
		estudante.POST("/solicitacoes-edicao/nome", estudantes.CriarSolicitacaoEdicaoDadoEstudanteHandler("nome"))
		estudante.POST("/solicitacoes-edicao/bilhete-identidade", estudantes.CriarSolicitacaoEdicaoDadoEstudanteHandler("bilhete_identidade"))
		estudante.POST("/solicitacoes-edicao/bilhete-identidade-encarregado", estudantes.CriarSolicitacaoEdicaoDadoEstudanteHandler("bilhete_identidade_encarregado"))
		estudante.POST("/solicitacoes-edicao/data-nascimento", estudantes.CriarSolicitacaoEdicaoDadoEstudanteHandler("data_nascimento"))
		estudante.GET("/minhas-avaliacoes", estudantes.GetMinhasAvaliacoes)
		estudante.GET("/documentos", documentos.ListarMeusDocumentosEstudante)
		estudante.GET("/documentos/:campo/download", documentos.DownloadMeuDocumentoEstudante)
		estudante.GET("/categorias-nota", notas.ListarCategoriasNota)
		estudante.GET("/solicitacoes", academias.ListarMinhasSolicitacoesStatusAcademicoHandler)
		estudante.POST("/solicitacoes-status/interrupcao", academias.CriarSolicitacaoStatusAcademicoHandler("interrupcao"))
		estudante.POST("/solicitacoes-status/desvinculacao", academias.CriarSolicitacaoStatusAcademicoHandler("desvinculacao"))
		estudante.POST("/solicitacoes-status/revinculacao/:codigo_academia", academias.CriarSolicitacaoStatusAcademicoHandler("revinculacao"))
		estudante.DELETE("/conta", estudantes.DeletarContaEstudante) // Tarefa 73
		// Parte 4: depois de concluir o nível escolar o estudante pode desvincular-se do encarregado.
		estudante.DELETE("/encarregado", contas.DesvincularEncarregado)
	}

	// ── Área do professor (Parte 4): consulta restrita aos seus vínculos ──
	professor := router.Group("/professor")
	professor.Use(middleware.AuthMiddleware())
	professor.Use(middleware.RequireProfessor())
	{
		professor.GET("/vinculos", contas.ListarMeusVinculos)
		professor.GET("/turmas", contas.ListarMinhasTurmas)
		professor.GET("/turmas/:codigo/estudantes", contas.ListarEstudantesDaTurma)
		professor.GET("/turmas/:codigo/notas", contas.ConsultarNotasDaTurma)
		professor.GET("/turmas/:codigo/faltas", contas.ConsultarFaltasDaTurma)
	}

	// ── Área do encarregado (Parte 4). Para agir em nome de um educando nas
	// rotas do estudante, o cabeçalho X-Educando indica o código do estudante.
	encarregado := router.Group("/encarregado")
	encarregado.Use(middleware.AuthMiddleware())
	encarregado.Use(middleware.RequireEncarregado())
	{
		encarregado.GET("/educandos", contas.ListarMeusEducandos)
		encarregado.PUT("/educandos/:codigo/relacao", contas.AtualizarRelacaoEducando)
	}

	// ── Rotas de leitura compartilhadas de academia ────────────────────────
	academiaShared := router.Group("/academia")
	academiaShared.Use(middleware.AuthMiddleware())
	academiaShared.Use(middleware.ValidarStatusAcademia())
	{
		academiaShared.GET("/categorias-nota", notas.ListarCategoriasNota)
	}

	// ── Rotas de academia ─────────────────────────────────────────────────
	academiaAnoLetivoRead := router.Group("/academia")
	academiaAnoLetivoRead.Use(middleware.AuthMiddleware())
	academiaAnoLetivoRead.Use(middleware.ValidarStatusAcademia())
	{
		academiaAnoLetivoRead.GET("/ano-letivo", academias.GetAnoLetivoAcademia)
		academiaAnoLetivoRead.GET("/anos-letivos-lista", academias.GetAnosLetivosListaAcademia)
	}

	academiaRead := router.Group("/academia")
	academiaRead.Use(middleware.AuthMiddleware())
	academiaRead.Use(middleware.RequireAcademiaOuAdmin())
	academiaRead.Use(middleware.ValidarStatusAcademia())
	{
		academiaRead.GET("/materias", materias.ListarMaterias)
		academiaRead.GET("/servicos-extras", apoio.ListarServicosExtrasAcademia)
		academiaRead.GET("/categorias-servico", servicosextras.ListarCategoriasServico)
		academiaRead.GET("/categorias-servico/:id", servicosextras.GetCategoriaServico)
		academiaRead.GET("/documentos-extra", documentos.ListarDocumentosExtraAcademia)
		academiaRead.GET("/servicos-extras/:id", servicosextras.GetServicoExtra)
		academiaRead.GET("/servicos-extras/solicitacoes", servicosextras.ListarSolicitacoesServicoExtraAcademia)
		academiaRead.GET("/servicos-extras/solicitacoes/:id/documento/download", servicosextras.DownloadDocumentoSolicitacaoServicoExtraAcademia)
		academiaRead.GET("/servicos-extras/solicitacoes/:id", servicosextras.GetSolicitacaoServicoExtraAcademia)
		academiaRead.GET("/servicos-extras/inscricoes/:id/pendencias", servicosextras.PendenciasServicoExtraAcademia)
		academiaRead.GET("/sumarios", sumarios.ListarSumarios)
		academiaRead.GET("/sumario/:id", sumarios.GetSumario)
		academiaRead.GET("/materia/:id", materias.GetMateria)
		academiaRead.GET("/turmas", turmas.ListarTurmasAcademia)
		academiaRead.GET("/turma/:codigo", turmas.GetTurma)
		academiaRead.GET("/anos-academicos", academias.ListarAnosAcademicos)
		academiaRead.GET("/anos-letivos/finalizacoes", academias.ListarFinalizacoesAnoLetivoAcademia)
		academiaRead.GET("/avaliacao-final/regras", avaliacoes.ListarRegrasAvaliacaoFinal)
		academiaRead.GET("/configuracao-status", academias.GetConfiguracaoStatusAcademia)
		academiaRead.GET("/solicitacoes", academias.ListarSolicitacoesStatusAcademicoHandler)
		academiaRead.GET("/solicitacoes-edicao-estudante", estudantes.ListarSolicitacoesEdicaoAcademia)
		academiaRead.GET("/documentos", documentos.ListarDocumentosAcademia)
		academiaRead.GET("/documentos/academia/:campo/download", documentos.DownloadDocumentoAcademiaPropria)
		academiaRead.GET("/documentos/estudantes/:codigo/:campo/download", documentos.DownloadDocumentoEstudanteAcademia)
		academiaRead.GET("/documentos/solicitacoes-edicao-estudante/:codigo/documento/download", documentos.DownloadDocumentoSolicitacaoEdicaoAcademia)
	}

	academia := router.Group("/academia")
	academia.Use(middleware.AuthMiddleware())
	academia.Use(middleware.RequireAcademia())
	academia.Use(middleware.ValidarStatusAcademia())
	{
		academia.PUT("/dados", academias.AtualizarDadosAcademia)
		academia.PUT("/nivel-escolar", academias.AtualizarNivelEscolarAcademia)

		// Parte 4 — secretários (RESERVADO à academia), professores e encarregados.
		academia.GET("/permissoes", contas.ListarPermissoes)
		academia.POST("/secretarios", contas.CriarSecretario)
		academia.GET("/secretarios", contas.ListarSecretarios)
		academia.GET("/secretarios/:id", contas.ObterSecretario)
		academia.PUT("/secretarios/:id", contas.EditarSecretario)
		academia.PUT("/secretarios/:id/ativar", contas.AtivarSecretario)
		academia.PUT("/secretarios/:id/desativar", contas.DesativarSecretario)
		academia.DELETE("/secretarios/:id", contas.EliminarSecretario)
		academia.POST("/professores", contas.CriarProfessor)
		academia.GET("/professores", contas.ListarProfessores)
		academia.GET("/professores/:id", contas.ObterProfessor)
		academia.PUT("/professores/:id", contas.EditarProfessor)
		academia.PUT("/professores/:id/ativar", contas.AtivarProfessor)
		academia.PUT("/professores/:id/desativar", contas.DesativarProfessor)
		academia.DELETE("/professores/:id", contas.EliminarProfessor)
		academia.POST("/professores/:id/vinculos", contas.AtribuirVinculoProfessor)
		academia.DELETE("/professores/:id/vinculos/:vinculo_id", contas.RemoverVinculoProfessor)
		academia.PUT("/vinculos-professor/:vinculo_id/reatribuir", contas.ReatribuirVinculo)
		academia.GET("/encarregados/buscar", contas.BuscarEncarregado)
		academia.PUT("/estudante/:codigo/encarregado", contas.TrocarEncarregado)
		academia.POST("/servicos-extras", apoio.CriarServicoExtra)
		academia.POST("/categorias-servico", servicosextras.CriarCategoriaServico)
		academia.PUT("/categorias-servico/:id", servicosextras.AtualizarCategoriaServico)
		academia.PUT("/categorias-servico/:id/desativar", servicosextras.DesativarCategoriaServico)
		academia.PUT("/categorias-servico/:id/reativar", servicosextras.ReativarCategoriaServico)
		academia.DELETE("/categorias-servico/:id", servicosextras.DeletarCategoriaServico)
		academia.POST("/documentos-extra", documentos.CriarDocumentoExtra)
		academia.PUT("/documentos-extra/:id", documentos.AtualizarDocumentoExtra)
		academia.PUT("/documentos-extra/:id/desativar", documentos.DesativarDocumentoExtra)
		academia.PUT("/documentos-extra/:id/reativar", documentos.ReativarDocumentoExtra)
		academia.PUT("/servicos-extras/:id", apoio.AtualizarServicoExtra)
		academia.PUT("/servicos-extras/:id/desativar", servicosextras.DesativarServicoExtra)
		academia.PUT("/servicos-extras/:id/reativar", servicosextras.ReativarServicoExtra)
		academia.DELETE("/servicos-extras/:id", servicosextras.DeletarServicoExtra)
		academia.PUT("/servicos-extras/solicitacoes/:id/aprovar", servicosextras.AprovarSolicitacaoServicoExtra)
		academia.PUT("/servicos-extras/solicitacoes/:id/reprovar", servicosextras.ReprovarSolicitacaoServicoExtra)
		academia.PUT("/servicos-extras/inscricoes/:id/cancelar", servicosextras.CancelarSolicitacaoServicoExtraAcademia)
		academia.POST("/solicitacoes-nif", academias.CriarSolicitacaoAlteracaoNIFAcademiaHandler)
		academia.GET("/solicitacoes-nif", academias.ListarSolicitacoesNIFAcademia)
		academia.POST("/definir-ano-letivo", academias.DefinirAnoLetivoAcademia)
		academia.POST("/anos-academicos", academias.AdicionarAnosAcademicos)
		academia.DELETE("/anos-academicos", academias.RemoverAnosAcademicos)
		academia.POST("/anos-letivos/finalizar", academias.FinalizarAnoLetivoAcademia)
		academia.PUT("/solicitacoes-edicao-estudante/nome/:codigo/aprovar", estudantes.DecidirSolicitacaoEdicaoDadoEstudanteHandler("nome", true))
		academia.PUT("/solicitacoes-edicao-estudante/nome/:codigo/reprovar", estudantes.DecidirSolicitacaoEdicaoDadoEstudanteHandler("nome", false))
		academia.PUT("/solicitacoes-edicao-estudante/bilhete-identidade/:codigo/aprovar", estudantes.DecidirSolicitacaoEdicaoDadoEstudanteHandler("bilhete_identidade", true))
		academia.PUT("/solicitacoes-edicao-estudante/bilhete-identidade/:codigo/reprovar", estudantes.DecidirSolicitacaoEdicaoDadoEstudanteHandler("bilhete_identidade", false))
		academia.PUT("/solicitacoes-edicao-estudante/bilhete-identidade-encarregado/:codigo/aprovar", estudantes.DecidirSolicitacaoEdicaoDadoEstudanteHandler("bilhete_identidade_encarregado", true))
		academia.PUT("/solicitacoes-edicao-estudante/bilhete-identidade-encarregado/:codigo/reprovar", estudantes.DecidirSolicitacaoEdicaoDadoEstudanteHandler("bilhete_identidade_encarregado", false))
		academia.PUT("/solicitacoes-edicao-estudante/data-nascimento/:codigo/aprovar", estudantes.DecidirSolicitacaoEdicaoDadoEstudanteHandler("data_nascimento", true))
		academia.PUT("/solicitacoes-edicao-estudante/data-nascimento/:codigo/reprovar", estudantes.DecidirSolicitacaoEdicaoDadoEstudanteHandler("data_nascimento", false))

		academia.POST("/estudante/register", estudantes.RegisterEstudantePorAcademia)
		academia.POST("/estudante/:codigo/documentos", estudantes.CompletarDocumentosEstudantePendente)
		academia.POST("/notas-aluno", notas.RegistrarNota)
		academia.POST("/faltas-aluno", faltas.RegistrarFaltas)
		academia.GET("/faltas/configuracao", faltas.GetConfiguracaoFaltas)
		academia.PUT("/faltas/configuracao", faltas.DefinirConfiguracaoFaltas)
		academia.PATCH("/notas-aluno/:id", notas.CorrigirNota)
		academia.PATCH("/faltas-aluno/:id", faltas.CorrigirFalta)
		// Avaliação final é acionada automaticamente pelo registro de notas
		academia.POST("/avaliacao-final/regras", avaliacoes.CriarRegraAvaliacaoFinal)
		academia.PUT("/avaliacao-final/regras/:id", avaliacoes.EditarRegraAvaliacaoFinal)
		academia.DELETE("/avaliacao-final/regras/:id", avaliacoes.DeletarRegraAvaliacaoFinal)
		academia.POST("/categorias-nota", notas.CriarCategoriaNota)
		academia.DELETE("/categorias-nota/:codigo", notas.DeletarCategoriaNota)
		academia.POST("/estudante/:codigo/interromper/percurso-academico", academias.AprovarSolicitacaoStatusAcademicoHandler("interrupcao"))
		academia.POST("/estudante/:codigo/interromper/percurso-academico/reprovar", academias.ReprovarSolicitacaoStatusAcademicoHandler("interrupcao"))
		academia.POST("/estudante/:codigo/desvincular", academias.AprovarSolicitacaoStatusAcademicoHandler("desvinculacao"))
		academia.POST("/estudante/:codigo/desvincular/reprovar", academias.ReprovarSolicitacaoStatusAcademicoHandler("desvinculacao"))
		academia.POST("/estudante/:codigo/revincular", academias.AprovarSolicitacaoStatusAcademicoHandler("revinculacao"))
		academia.POST("/estudante/:codigo/revincular/reprovar", academias.ReprovarSolicitacaoStatusAcademicoHandler("revinculacao"))
		academia.DELETE("/estudante/:codigo/conta", estudantes.DeletarContaEstudantePorAcademia) // Tarefa 98

		// ── Cursos ────────────────────────────────────────────────────────
		academia.POST("/curso", cursos.CriarCurso)
		academia.PUT("/curso/:id/ativar", cursos.AtivarCurso)
		academia.PUT("/curso/:id/desativar", cursos.DesativarCurso)
		academia.PUT("/curso/:id/dados", cursos.AtualizarDadosCurso)
		academia.DELETE("/curso/:id", cursos.DeletarCurso)

		// ── Matérias ──────────────────────────────────────────────────────
		academia.POST("/materia", materias.CriarMateria)
		academia.POST("/sumario", sumarios.CriarSumario)
		academia.PUT("/sumario/:id/dados", sumarios.AtualizarDadosSumario)
		academia.DELETE("/sumario/:id", sumarios.DeletarSumario)
		academia.PUT("/faltas-aluno/:id/desvincular-sumario", faltas.DesvincularSumarioFalta)
		academia.PUT("/materia/:id/ativar", materias.AtivarMateria)
		academia.PUT("/materia/:id/desativar", materias.DesativarMateria)
		academia.PUT("/materia/:id/dados", materias.AtualizarDadosMateria)
		academia.DELETE("/materia/:id", materias.DeletarMateria)

		// ── Turmas ────────────────────────────────────────────────────────
		academia.POST("/turma", turmas.CriarTurma)
		academia.PUT("/turma/:codigo/ativar", turmas.AtivarTurma)
		academia.PUT("/turma/:codigo/desativar", turmas.DesativarTurma)
		academia.PUT("/turma/:codigo/dados", turmas.AtualizarDadosTurma)
		academia.DELETE("/turma/:codigo", turmas.DeletarTurma)
		academia.POST("/turma/:codigo/estudante", turmas.AdicionarEstudanteATurma)
		academia.DELETE("/turma/:codigo/estudantes/:codigo_estudante", turmas.RemoverEstudanteDaTurma)

		// ── Batch/async: estudantes usam resposta de lote; demais criam jobs ──
		academia.POST("/estudante/register/async", estudantes.RegisterEstudanteBatch)
		academia.POST("/notas-aluno/async", notas.RegistrarNotaBatchAsync)
		academia.POST("/faltas-aluno/async", faltas.RegistrarFaltasBatchAsync)
		academia.POST("/curso/async", cursos.CriarCursoBatchAsync)
		academia.POST("/materia/async", materias.CriarMateriaBatchAsync)
		academia.POST("/turma/async", turmas.CriarTurmaBatchAsync)
		academia.POST("/turma/estudante/async", turmas.AdicionarEstudanteBatchAsync)
		academia.PUT("/dados/async", academias.AtualizarDadosAcademiaBatchAsync)
		academia.POST("/categorias-nota/async", notas.CriarCategoriaNotaBatchAsync)
		academia.DELETE("/categorias-nota/async", notas.DeletarCategoriaNotaBatchAsync)
		academia.PUT("/curso/ativar/async", cursos.AtivarCursoBatchAsync)
		academia.PUT("/curso/desativar/async", cursos.DesativarCursoBatchAsync)
		academia.PUT("/curso/dados/async", cursos.AtualizarDadosCursoBatchAsync)
		academia.DELETE("/curso/async", cursos.DeletarCursoBatchAsync)
		academia.PUT("/materia/ativar/async", materias.AtivarMateriaBatchAsync)
		academia.PUT("/materia/desativar/async", materias.DesativarMateriaBatchAsync)
		academia.PUT("/materia/dados/async", materias.AtualizarDadosMateriaBatchAsync)
		academia.DELETE("/materia/async", materias.DeletarMateriaBatchAsync)
		academia.PUT("/turma/ativar/async", turmas.AtivarTurmaBatchAsync)
		academia.PUT("/turma/desativar/async", turmas.DesativarTurmaBatchAsync)
		academia.PUT("/turma/dados/async", turmas.AtualizarDadosTurmaBatchAsync)
		academia.DELETE("/turma/async", turmas.DeletarTurmaBatchAsync)
		academia.DELETE("/turma/estudante/async", turmas.RemoverEstudanteTurmaBatchAsync)
	}

	// ── Rotas de admin ────────────────────────────────────────────────────
	admin := router.Group("/dominis")
	admin.Use(middleware.AuthMiddleware())
	admin.Use(middleware.RequireAdmin())
	{
		admin.POST("/register", middleware.RequireFPP(), administradores.RegisterAdmin)
		admin.POST("/academia/cadastro", middleware.RequireFPP(), academias.RegisterAcademia)
		// Tarefa 123 (SEC-04): reenvio de credenciais temporárias (somente fpp).
		admin.POST("/academias/:codigo/credenciais/reenviar", middleware.RequireFPP(), autenticacao.ReenviarCredenciaisAcademia)
		admin.POST("/administradores/:id/credenciais/reenviar", middleware.RequireFPP(), autenticacao.ReenviarCredenciaisAdmin)
		admin.PUT("/academia/:codigo/ativar", middleware.RequireAdm(), academias.AtivarAcademia)
		admin.PUT("/academia/:codigo/desativar", middleware.RequireAdm(), academias.DesativarAcademia)
		admin.DELETE("/academia/:codigo", middleware.RequireFPP(), academias.DeletarAcademia)
		admin.GET("/solicitacoes-nif-academia", academias.ListarSolicitacoesNIFAdmin)
		admin.PUT("/solicitacoes-nif-academia/:codigo/aprovar", middleware.RequireAdm(), academias.DecidirSolicitacaoAlteracaoNIFAcademiaHandler(true))
		admin.PUT("/solicitacoes-nif-academia/:codigo/reprovar", middleware.RequireAdm(), academias.DecidirSolicitacaoAlteracaoNIFAcademiaHandler(false))
		admin.PUT("/admin/:id/ativar", middleware.RequireAdm(), administradores.AtivarAdmin)
		admin.PUT("/admin/:id/desativar", middleware.RequireAdm(), administradores.DesativarAdmin)
		admin.DELETE("/admin/:id", middleware.RequireAdm(), administradores.DeletarAdmin)                  // Tarefa 73
		admin.GET("/auditoria/delecoes", middleware.RequireAdm(), administradores.ListarAuditoriaDelecoes) // Tarefa 73/2
		admin.GET("/admin-lista", administradores.ListarTodosAdmins)
		admin.GET("/metrics", administradores.GetSystemMetrics)
		admin.GET("/storage/quota", administradores.GetStorageQuota)
		admin.POST("/storage/migrar-local-para-mega", middleware.RequireFPP(), administradores.MigrarStorageLocalParaMega)
		admin.POST("/projections/rebuild/:name", middleware.RequireFPP(), administradores.RebuildProjection)
		admin.POST("/projections/rebuild/:name/async", middleware.RequireFPP(), administradores.RebuildProjectionAsync)
		admin.GET("/consultar-admin/:email", administradores.GetAdminPorEmail)
		admin.PUT("/admin/:id/role", middleware.RequireFPP(), administradores.AtualizarRoleAdmin)
		admin.PUT("/admin/:id/dados", administradores.AtualizarDadosAdmin)

		// ── Batch assíncronos (admin) ─────────────────────────────────────
		admin.POST("/academia/register/async", middleware.RequireFPP(), academias.RegisterAcademiaBatchAsync)
		admin.PUT("/academia/ativar/async", middleware.RequireAdm(), academias.AtivarAcademiaBatchAsync)
		admin.PUT("/academia/desativar/async", middleware.RequireAdm(), academias.DesativarAcademiaBatchAsync)
		admin.PUT("/admin/ativar/async", middleware.RequireAdm(), administradores.AtivarAdminBatchAsync)
		admin.PUT("/admin/desativar/async", middleware.RequireAdm(), administradores.DesativarAdminBatchAsync)
	}

	// Configurações globais do painel administrativo.
	adminSistema := router.Group("/admin")
	adminSistema.Use(middleware.AuthMiddleware())
	adminSistema.Use(middleware.RequireAdmin())
	{
		adminSistema.POST("/definir-ano-letivo-geral", middleware.RequireFPP(), administradores.DefinirAnoLetivoGlobalSistema)
		adminSistema.GET("/sistema/anos-letivos/configuracoes", middleware.RequireFPP(), administradores.ListarConfiguracoesAnosLetivos)
		adminSistema.PUT("/sistema/anos-letivos/configuracoes/:type", middleware.RequireFPP(), administradores.AtualizarConfiguracaoAnoLetivo)
		adminSistema.GET("/sistema/anos-letivos/finalizacao-limites", middleware.RequireFPP(), administradores.GetLimitesFinalizacaoAnosLetivos)
		adminSistema.GET("/academias/anos-letivos/finalizacoes", middleware.RequireFPP(), academias.ListarFinalizacoesAnoLetivoAdmin)

		// Configurações operacionais editáveis (Parte 3): leitura para adm e fpp,
		// alteração só fpp. Segredos nunca saem em claro.
		adminSistema.GET("/sistema/configuracoes", middleware.RequireAdm(), administradores.ListarConfiguracoesSistema)
		adminSistema.PUT("/sistema/configuracoes/:chave", middleware.RequireFPP(), administradores.DefinirConfiguracaoSistema)
		adminSistema.DELETE("/sistema/configuracoes/:chave", middleware.RequireFPP(), administradores.RemoverConfiguracaoSistema)
	}

	return router
}

// corsMiddleware — whitelist configurável via ALLOWED_ORIGINS.
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		allowedOriginsEnv := sysconfig.String("ALLOWED_ORIGINS")
		var allowedOrigins []string
		if allowedOriginsEnv != "" {
			for _, o := range strings.Split(allowedOriginsEnv, ",") {
				trimmed := strings.TrimSpace(o)
				if trimmed != "" {
					allowedOrigins = append(allowedOrigins, trimmed)
				}
			}
		}

		origin := c.Request.Header.Get("Origin")
		allowed := false

		if len(allowedOrigins) == 0 {
			allowed = true
		} else {
			for _, o := range allowedOrigins {
				if o == origin {
					allowed = true
					break
				}
			}
		}

		if allowed && origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Request-ID")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// requestIDMiddleware propaga ou gera um X-Request-ID para rastreamento.
func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")

		if requestID != "" && !requestIDPattern.MatchString(requestID) {
			requestID = ""
		}

		if requestID == "" {
			requestID = fmt.Sprintf("%d", time.Now().UnixNano())
		}

		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}
