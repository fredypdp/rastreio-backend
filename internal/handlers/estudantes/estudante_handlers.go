package estudantes

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/handlers/apoio"
	"spuri/internal/middleware"
	"spuri/internal/projections"
	"spuri/internal/services"
	"spuri/internal/storage"
	"spuri/internal/utils"
)

func RegisterEstudantePorAcademia(c *gin.Context) {
	if strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		registerEstudantePorAcademiaMultipart(c)
		return
	}
	utils.RespondWithValidationError(c, fmt.Errorf("cadastro direto de estudante exige multipart/form-data; documentos são opcionais"))
}

func registerEstudantePorAcademiaMultipart(c *gin.Context) {
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("multipart/form-data inválido"))
		return
	}
	if apoio.RejectRemovedMultipartFields(c) {
		return
	}
	if err := apoio.ValidarCamposArquivoMatricula(c.Request.MultipartForm); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	get := func(k string) string { return strings.TrimSpace(c.PostForm(k)) }
	dataNascimento, err := time.Parse("2006-01-02", get("data_nascimento"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("data_nascimento deve ser YYYY-MM-DD anterior à data atual"))
		return
	}
	req := apoio.CadastroEstudanteAcademiaRequest{
		Nome: get("nome"), Genero: get("genero"), DataNascimento: dataNascimento,
		Email: get("email"), Telefone: get("telefone"), TelefoneEncarregado: get("telefone_encarregado"),
		BilheteIdentidade: get("bilhete_identidade"), BilheteEncarregado: get("bilhete_identidade_encarregado"),
		AnoEscolar: get("ano_escolar_fundamental"), AnoEscolarMedio: get("ano_escolar_medio"), AnoSuperior: get("ano_superior"),
		CursoMedioID: get("curso_medio_id"), CursoSuperiorID: get("curso_superior_id"), CodigoTurma: get("codigo_turma"),
	}

	files := map[string]apoio.UploadedPDF{}
	for _, field := range apoio.SolicitacaoDocFields {
		if fh, err := c.FormFile(field); err == nil {
			pdf, err := apoio.ReadAndValidatePDF(field, fh)
			if err != nil {
				utils.RespondWithValidationError(c, err)
				return
			}
			files[field] = pdf
		}
	}
	registerEstudantePorAcademiaComRequest(c, req, files, get("declaracao_ano_academico"))
}

func registerEstudantePorAcademiaComRequest(c *gin.Context, req apoio.CadastroEstudanteAcademiaRequest, files map[string]apoio.UploadedPDF, declaracaoAnoAcademico string) {
	registerEstudantePorAcademiaComRequestModo(c, req, files, declaracaoAnoAcademico, false)
}

func permitirPendenciaDocumentosEmFalhaStorage(c *gin.Context, pendenteDocumentos bool) bool {
	if pendenteDocumentos {
		return false
	}
	permitir, ok := c.Get("permitir_pendencia_documentos_em_falha_storage")
	return ok && permitir == true
}

func registerEstudantePorAcademiaComRequestModo(c *gin.Context, req apoio.CadastroEstudanteAcademiaRequest, files map[string]apoio.UploadedPDF, declaracaoAnoAcademico string, pendenteDocumentos bool) {
	academiaID, ok := middleware.GetUserID(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	academia, err := apoio.GetAcademiaProjection(c).GetByID(academiaID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if academia == nil {
		utils.RespondWithError(c, http.StatusNotFound, "academia não encontrada", nil)
		return
	}

	codigoTurma := strings.TrimSpace(req.CodigoTurma)
	if codigoTurma != "" {
		turmaDTO, err := apoio.GetTurmasProjection(c).GetByCodigoTurma(codigoTurma, academia.CodigoAcademia)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if turmaDTO == nil {
			utils.RespondWithNotFoundError(c, "turma não encontrada ou não pertence a esta academia")
			return
		}
		if turmaDTO.Status != "ativo" {
			utils.RespondWithValidationError(c, fmt.Errorf("turma '%s' está %s e não pode receber estudantes", codigoTurma, turmaDTO.Status))
			return
		}
		if err := apoio.ValidarCompatibilidadeEstudanteTurma(nil, nil, academia.AnosAcademicos, turmaDTO.Nivel, turmaDTO.CursoID, apoio.StringPtrIfNotBlank(req.AnoEscolar), apoio.StringPtrIfNotBlank(req.AnoEscolarMedio), apoio.StringPtrIfNotBlank(req.AnoSuperior), apoio.StringPtrIfNotBlank(req.CursoMedioID), apoio.StringPtrIfNotBlank(req.CursoSuperiorID)); err != nil {
			utils.RespondWithValidationError(c, fmt.Errorf("estudante incompatível com esta turma: %w", err))
			return
		}
	}

	cursoMedioUUID, cursoSuperiorUUID, err := apoio.ValidarCursosMatriculaCommon(c, academia.CodigoAcademia, req.CursoMedioID, req.CursoSuperiorID)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	// Documentos extra (catálogo definido pela academia) — pulados por
	// completo no modo "pendente de documentos", mesmo tratamento dado aos
	// documentos fixos (PularValidacaoDocumentos), já que este modo registra
	// o estudante deliberadamente sem nenhum arquivo por enquanto.
	var catalogoDocsExtra []projections.DocumentoExtraDTO
	var docsExtraEnviados map[string]apoio.DocumentoExtraUpload
	var anoAcademicoDocsExtra string
	if !pendenteDocumentos {
		anoAcademicoDocsExtra = apoio.ResolverAnoAcademicoParaDocumentosExtra(apoio.StringPtrIfNotBlank(req.AnoEscolar), apoio.StringPtrIfNotBlank(req.AnoEscolarMedio), apoio.StringPtrIfNotBlank(req.AnoSuperior))
		if anoAcademicoDocsExtra != "" {
			catalogoDocsExtra, err = apoio.GetDocumentosExtraProjection(c).GetAtivosPorAnoAcademico(academia.CodigoAcademia, anoAcademicoDocsExtra)
			if err != nil {
				utils.RespondWithInternalError(c, err)
				return
			}
		}
		docsExtraEnviados, err = apoio.ParseDocumentosExtra(c.Request.MultipartForm, catalogoDocsExtra)
		if err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
		if err := apoio.ValidarObrigatoriedadeDocumentosExtra(catalogoDocsExtra, docsExtraEnviados); err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
	}
	documentosParaValidacao := apoio.DocumentosMatriculaParaValidacao(files, declaracaoAnoAcademico)
	for campo, documento := range req.Documentos {
		key, doc := apoio.DocumentoMatriculaNormalizadoComBase(campo, declaracaoAnoAcademico, "", documento)
		documentosParaValidacao[key] = doc
	}
	validado, err := services.ValidateMatriculaCommon(services.MatriculaCommonInput{
		Contexto: services.MatriculaContextCadastroDireto,
		Nome:     req.Nome, Genero: req.Genero, DataNascimento: req.DataNascimento,
		Email: apoio.StringPtrIfNotBlank(req.Email), TelefoneEstudante: apoio.StringPtrIfNotBlank(req.Telefone), TelefoneEncarregado: apoio.StringPtrIfNotBlank(req.TelefoneEncarregado),
		BilheteIdentidade: apoio.StringPtrIfNotBlank(req.BilheteIdentidade), BilheteIdentidadeEncarregado: apoio.StringPtrIfNotBlank(req.BilheteEncarregado),
		AnoEscolarFundamental: apoio.StringPtrIfNotBlank(req.AnoEscolar), AnoEscolarMedio: apoio.StringPtrIfNotBlank(req.AnoEscolarMedio), AnoSuperior: apoio.StringPtrIfNotBlank(req.AnoSuperior),
		Documentos:               documentosParaValidacao,
		PularValidacaoDocumentos: pendenteDocumentos,
	})
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := apoio.ValidateBIEncarregadoNaoConflitaComEscolar(c, &req.BilheteEncarregado, nil); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	// SEC-04: as credenciais seguem por SMS. Valida-se o telefone de destino ANTES
	// de criar a conta, para não criar contas cujas credenciais não possam ser entregues.
	telefoneDestinoCredenciais, err := apoio.TelefoneDestinoCredenciaisEstudante(validado.AnoEscolarFundamental, validado.AnoEscolarMedio, validado.AnoSuperior, validado.TelefoneEstudante, validado.TelefoneEncarregado)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	client := apoio.GetDbClient(c)
	codigoEstudante, err := utils.GenerateUniqueCodigoEstudante(client.DB())
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	provider := apoio.GetStorageProvider(c)
	if provider == nil {
		p, err := storage.NewStorageProvider()
		if err != nil {
			utils.RespondWithError(c, http.StatusServiceUnavailable, err.Error(), err)
			return
		}
		provider = p
	}
	dir := fmt.Sprintf("%s/estudantes/%s/documentos", academia.CodigoAcademia, codigoEstudante)
	if len(files) > 0 || len(docsExtraEnviados) > 0 {
		if err := provider.EnsureDir(dir); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
	}
	documentos := map[string]aggregates.DocumentoMatricula{}
	for campo, documento := range req.Documentos {
		key, doc := apoio.DocumentoMatriculaNormalizadoComBase(campo, declaracaoAnoAcademico, "", documento)
		documentos[key] = doc
	}
	for field, f := range files {
		storageTipo, storagePath := apoio.StoragePathDocumentoEstudante(dir, field, codigoEstudante, declaracaoAnoAcademico)
		stored, err := provider.Upload(storagePath, bytes.NewReader(f.Data), f.Size)
		if err != nil {
			_ = provider.Delete(dir)
			if permitirPendenciaDocumentosEmFalhaStorage(c, pendenteDocumentos) {
				log.Printf("[WARN] falha no upload dos documentos; cadastrando estudante %s em pendência documental para repescagem: %v", req.Nome, err)
				registerEstudantePorAcademiaComRequestModo(c, req, nil, declaracaoAnoAcademico, true)
				return
			}
			utils.RespondWithInternalError(c, fmt.Errorf("falha no upload dos documentos: %w", err))
			return
		}
		key, doc := apoio.DocumentoMatriculaNormalizado(field, declaracaoAnoAcademico, apoio.EstudanteDocumentoDownloadURL(codigoEstudante, storageTipo), stored.Path, stored.FileURL)
		documentos[key] = doc
	}
	if len(docsExtraEnviados) > 0 {
		docsExtraArmazenados, err := apoio.ArmazenarDocumentosExtra(provider, dir, docsExtraEnviados, anoAcademicoDocsExtra, func(campo string) string {
			return apoio.EstudanteDocumentoDownloadURL(codigoEstudante, campo)
		})
		if err != nil {
			_ = provider.Delete(dir)
			if permitirPendenciaDocumentosEmFalhaStorage(c, pendenteDocumentos) {
				log.Printf("[WARN] falha no upload dos documentos extra; cadastrando estudante %s em pendência documental para repescagem: %v", req.Nome, err)
				registerEstudantePorAcademiaComRequestModo(c, req, nil, declaracaoAnoAcademico, true)
				return
			}
			utils.RespondWithInternalError(c, fmt.Errorf("falha no upload dos documentos extra: %w", err))
			return
		}
		for key, doc := range docsExtraArmazenados {
			documentos[key] = doc
		}
	}

	// SEC-04: senha aleatória de 8 caracteres, de troca obrigatória no primeiro acesso.
	senhaTemporaria, err := services.GenerateTempPassword()
	if err != nil {
		_ = provider.Delete(dir)
		utils.RespondWithInternalError(c, err)
		return
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(senhaTemporaria), bcrypt.DefaultCost)
	if err != nil {
		_ = provider.Delete(dir)
		utils.RespondWithInternalError(c, err)
		return
	}
	emailPtr := validado.Email
	telefonePtr := validado.TelefoneEstudante
	telefoneRespPtr := validado.TelefoneEncarregado
	bilhetePtr := validado.BilheteIdentidade
	bilheteRespPtr := validado.BilheteIdentidadeEncarregado
	var biGuard *db.UniqueGuardReservation
	biGuardConsumed := false
	if bilhetePtr != nil {
		guardKey := db.CanonicalGuardKey(*bilhetePtr)
		biGuard, err = db.NewUniqueOperationGuard(apoio.GetDbClient(c)).WithContext(c.Request.Context()).Reserve(
			"estudante:bilhete_identidade",
			guardKey,
			db.UniqueGuardOptions{UserID: academiaID.String(), UserType: "academia"},
		)
		if errors.Is(err, db.ErrUniqueOperationInProgress) {
			_ = provider.Delete(dir)
			utils.RespondWithConflictError(c, "bilhete de identidade já está em uso ou em cadastro")
			return
		}
		if err != nil {
			_ = provider.Delete(dir)
			utils.RespondWithInternalError(c, err)
			return
		}
		defer func() {
			if biGuard != nil && !biGuardConsumed {
				_ = biGuard.Release()
			}
		}()

		existente, err := apoio.GetEstudanteProjection(c).GetByBilheteIdentidadePrincipal(*bilhetePtr)
		if err != nil {
			_ = provider.Delete(dir)
			utils.RespondWithInternalError(c, err)
			return
		}
		if existente != nil {
			_ = provider.Delete(dir)
			utils.RespondWithValidationError(c, fmt.Errorf("bilhete de identidade já cadastrado"))
			return
		}
	}
	anoEscolarPtr := validado.AnoEscolarFundamental
	anoEscolarMedioPtr := validado.AnoEscolarMedio
	anoSuperiorPtr := validado.AnoSuperior

	estudante := aggregates.NewEstudante()
	var criarErr error
	if pendenteDocumentos {
		criarErr = estudante.CriarComVinculoPendenteDocumentos(req.Nome, codigoEstudante, string(hashedPassword), emailPtr, telefonePtr, telefoneRespPtr, bilhetePtr, bilheteRespPtr, req.Genero, req.DataNascimento, anoEscolarPtr, anoEscolarMedioPtr, anoSuperiorPtr, cursoMedioUUID, cursoSuperiorUUID, &academiaID, academia.CodigoAcademia, documentos)
	} else {
		criarErr = estudante.CriarComVinculo(req.Nome, codigoEstudante, string(hashedPassword), emailPtr, telefonePtr, telefoneRespPtr, bilhetePtr, bilheteRespPtr, req.Genero, req.DataNascimento, anoEscolarPtr, anoEscolarMedioPtr, anoSuperiorPtr, cursoMedioUUID, cursoSuperiorUUID, &academiaID, academia.CodigoAcademia, documentos)
	}
	if criarErr != nil {
		_ = provider.Delete(dir)
		utils.RespondWithValidationError(c, criarErr)
		return
	}
	if err := estudante.DefinirSenhaTemporaria(); err != nil {
		_ = provider.Delete(dir)
		utils.RespondWithInternalError(c, err)
		return
	}
	audit := db.AuditContext{UserID: academiaID.String(), UserType: "academia", IP: c.ClientIP()}
	if err := apoio.GetRepository(c).SaveWithAudit(estudante, audit); err != nil {
		_ = provider.Delete(dir)
		utils.RespondWithInternalError(c, err)
		return
	}
	if biGuard != nil {
		if err := biGuard.Consume(estudante.GetID()); err != nil {
			_ = provider.Delete(dir)
			utils.RespondWithInternalError(c, err)
			return
		}
		biGuardConsumed = true
	}
	log.Printf("Estudante criado por academia %s: %s", academia.CodigoAcademia, codigoEstudante)

	// SEC-04: credenciais por SMS DEPOIS do commit. Falha de envio nunca desfaz a
	// criação. No cadastro em lote assíncrono (jobs) cada item chega aqui pelo
	// worker, por isso o envio é por item, dentro do próprio worker.
	atorCredenciais := apoio.AtorComunicacao{ID: academiaID, Tipo: "academia", CodigoAcademia: academia.CodigoAcademia}
	credenciais := apoio.EnviarCredenciaisSMS(c.Request.Context(), c, atorCredenciais, audit, telefoneDestinoCredenciais, req.Nome, codigoEstudante, senhaTemporaria, false)

	data := gin.H{"id": estudante.ID, "codigo_estudante": codigoEstudante, "codigo_academia": academia.CodigoAcademia, "documentos": documentos, "status": estudante.Status, "credenciais": credenciais}
	if codigoTurma != "" {
		data["codigo_turma"] = codigoTurma
		if err := apoio.VincularEstudanteATurma(c, academia, codigoEstudante, req.AnoEscolar, req.AnoEscolarMedio, req.AnoSuperior, req.CursoMedioID, req.CursoSuperiorID, codigoTurma, true, academiaID); err != nil {
			log.Printf("[WARN] falha ao vincular estudante recém-criado à turma: codigo_estudante=%s codigo_turma=%s erro=%v", codigoEstudante, codigoTurma, err)
			data["turma_vinculada"] = false
			data["turma_aviso"] = fmt.Sprintf("o estudante foi cadastrado com sucesso, mas não foi possível vinculá-lo automaticamente à turma '%s'. Vincule manualmente na tela de Turmas.", codigoTurma)
		} else {
			data["turma_vinculada"] = true
		}
	}
	if estudante.Status == "pendente_documentos" {
		data["documentos_faltantes"] = aggregates.DocumentosMatriculaFaltantes(bilhetePtr, bilheteRespPtr, anoEscolarPtr, anoEscolarMedioPtr, anoSuperiorPtr, documentos)
	}
	c.JSON(http.StatusCreated, gin.H{"message": "estudante registrado com sucesso", "data": data})
}

// ============================================================================
// GET /estudantes
// ============================================================================

func ListarEstudantes(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)

	client := apoio.GetDbClient(c)
	limit, offset := apoio.GetPaginationParams(c)
	limit = db.ValidateLimit(limit)
	offset = db.ValidateOffset(offset)

	// data_nascimento é NOT NULL após a migration — scan direto como time.Time.
	const selectCols = `
		SELECT e.id, e.nome, e.codigo_estudante, e.email, e.telefone, e.email_verificado,
			e.bilhete_identidade, e.bilhete_identidade_encarregado, e.codigo_academia,
			e.status, e.status_escolar_fundamental, e.status_escolar_medio, e.status_superior,
			e.ano_escolar_fundamental, e.ano_escolar_medio, e.ano_superior, e.semestre_atual,
			e.curso_medio_id, e.curso_superior_id,
			e.genero, e.data_nascimento, e.created_at, e.updated_at,
			COALESCE(e.total_notas, 0), COALESCE(e.total_faltas, 0), e.version
		FROM projection_estudantes`

	var (
		rows       *sql.Rows
		err        error
		conditions []string
		args       []interface{}
	)

	if generos := apoio.ParseMultiValueQueryParam(c, "genero"); len(generos) > 0 {
		args = append(args, pq.Array(generos))
		conditions = append(conditions, fmt.Sprintf("e.genero = ANY($%d)", len(args)))
	}
	if status := apoio.ParseMultiValueQueryParam(c, "status"); len(status) > 0 {
		for _, value := range status {
			switch value {
			case "ativo", "inativo", "pendente_documentos":
			default:
				utils.RespondWithValidationError(c, fmt.Errorf("status inválido: valores aceitos são ativo, inativo ou pendente_documentos"))
				return
			}
		}
		args = append(args, pq.Array(status))
		conditions = append(conditions, fmt.Sprintf("e.status = ANY($%d)", len(args)))
	}
	if anosFund := apoio.ParseMultiValueQueryParam(c, "ano_escolar_fundamental"); len(anosFund) > 0 {
		args = append(args, pq.Array(anosFund))
		conditions = append(conditions, fmt.Sprintf("e.ano_escolar_fundamental = ANY($%d)", len(args)))
	}
	if anosMedio := apoio.ParseMultiValueQueryParam(c, "ano_escolar_medio"); len(anosMedio) > 0 {
		args = append(args, pq.Array(anosMedio))
		conditions = append(conditions, fmt.Sprintf("e.ano_escolar_medio = ANY($%d)", len(args)))
	}
	if anosSup := apoio.ParseMultiValueQueryParam(c, "ano_superior"); len(anosSup) > 0 {
		args = append(args, pq.Array(anosSup))
		conditions = append(conditions, fmt.Sprintf("e.ano_superior = ANY($%d)", len(args)))
	}
	if turno := apoio.ParseMultiValueQueryParam(c, "turno"); len(turno) > 0 {
		args = append(args, pq.Array(turno))
		conditions = append(conditions, fmt.Sprintf("t.turno = ANY($%d)", len(args)))
	}
	if codigoTurma := apoio.ParseMultiValueQueryParam(c, "codigo_turma"); len(codigoTurma) > 0 {
		args = append(args, pq.Array(codigoTurma))
		conditions = append(conditions, fmt.Sprintf("t.codigo_turma = ANY($%d)", len(args)))
	}
	if semestresAtuais, parseErr := parseMultiPositiveInt64QueryParam(c, "semestre_atual"); parseErr != nil {
		utils.RespondWithValidationError(c, parseErr)
		return
	} else if len(semestresAtuais) > 0 {
		args = append(args, pq.Array(semestresAtuais))
		conditions = append(conditions, fmt.Sprintf("e.semestre_atual = ANY($%d)", len(args)))
	}
	if cursos := parseMultiValueQueryParams(c, "curso_id", "curso"); len(cursos) > 0 {
		for _, curso := range cursos {
			if _, parseErr := uuid.Parse(curso); parseErr != nil {
				utils.RespondWithValidationError(c, fmt.Errorf("curso_id inválido"))
				return
			}
		}
		args = append(args, pq.Array(cursos))
		conditions = append(conditions, fmt.Sprintf("(e.curso_medio_id::text = ANY($%d) OR e.curso_superior_id::text = ANY($%d))", len(args), len(args)))
	}

	if withClass := strings.TrimSpace(c.Query("com_turma")); withClass != "" {
		v, parseErr := strconv.ParseBool(withClass)
		if parseErr != nil {
			utils.RespondWithValidationError(c, fmt.Errorf("com_turma deve ser booleano (true/false)"))
			return
		}
		if v {
			conditions = append(conditions, "t.codigo_turma IS NOT NULL")
		} else {
			conditions = append(conditions, "t.codigo_turma IS NULL")
		}
	}

	if idadeMinStr := strings.TrimSpace(c.Query("idade_min")); idadeMinStr != "" {
		idadeMin, parseErr := strconv.Atoi(idadeMinStr)
		if parseErr != nil || idadeMin < 0 {
			utils.RespondWithValidationError(c, fmt.Errorf("idade_min inválida"))
			return
		}
		args = append(args, time.Now().AddDate(-idadeMin, 0, 0))
		conditions = append(conditions, fmt.Sprintf("e.data_nascimento <= $%d", len(args)))
	}
	if idadeMaxStr := strings.TrimSpace(c.Query("idade_max")); idadeMaxStr != "" {
		idadeMax, parseErr := strconv.Atoi(idadeMaxStr)
		if parseErr != nil || idadeMax < 0 {
			utils.RespondWithValidationError(c, fmt.Errorf("idade_max inválida"))
			return
		}
		args = append(args, time.Now().AddDate(-(idadeMax+1), 0, 1))
		conditions = append(conditions, fmt.Sprintf("e.data_nascimento >= $%d", len(args)))
	}

	for _, item := range []struct{ key, col string }{
		{"status_escolar_fundamental", "e.status_escolar_fundamental"},
		{"status_escolar_medio", "e.status_escolar_medio"},
		{"status_superior", "e.status_superior"},
	} {
		if values := apoio.ParseMultiValueQueryParam(c, item.key); len(values) > 0 {
			args = append(args, pq.Array(values))
			conditions = append(conditions, fmt.Sprintf("%s = ANY($%d)", item.col, len(args)))
		}
	}

	// Tarefa 111: busca livre por código, nome, BI ou telefone/e-mail (do
	// estudante) — usada pelo seletor de estudante em telas que antes
	// carregavam uma lista inteira (até um limite fixo) para um <select> no
	// cliente, sem nenhuma forma de busca real. Um único termo é comparado
	// contra as 5 colunas (ILIKE, case-insensitive, correspondência
	// parcial); não substitui os filtros categóricos acima, que continuam
	// funcionando normalmente combinados com busca.
	if busca := strings.TrimSpace(c.Query("busca")); busca != "" {
		termo := "%" + busca + "%"
		args = append(args, termo)
		idx := len(args)
		conditions = append(conditions, fmt.Sprintf(
			"(e.nome ILIKE $%d OR e.codigo_estudante ILIKE $%d OR e.bilhete_identidade ILIKE $%d OR e.telefone ILIKE $%d OR e.email ILIKE $%d)",
			idx, idx, idx, idx, idx,
		))
	}

	// Tarefa 73/2: nunca retornar estudantes deletados nesta listagem geral,
	// independentemente de quais outros filtros de status foram combinados
	// acima ("deletado" nem é um valor aceito no filtro ?status=). Quem
	// precisa consultar deleções usa o endpoint dedicado de auditoria.
	filtrosDoUtilizador := len(conditions) > 0
	conditions = append(conditions, "e.status <> 'deletado'")

	// SEC-15 (Tarefa 127): filtro por tipo de vínculo (atual, anterior, todos).
	// Os filtros acima numeram os placeholders ($n) a partir de len(args); o
	// código da academia entra sempre DEPOIS deles, com o mesmo contador, para
	// que nenhum parâmetro fique deslocado.
	if userType != "academia" && userType != "admin" {
		utils.RespondWithForbiddenError(c, "Acesso negado. Apenas academias e administradores podem listar estudantes.")
		return
	}
	vinculoParam := strings.TrimSpace(c.Query("vinculo"))
	switch vinculoParam {
	case "atual", "anterior", "todos", "":
	default:
		utils.RespondWithValidationError(c, fmt.Errorf("vinculo inválido: valores aceitos são atual, anterior ou todos"))
		return
	}
	vinculoEfetivo := vinculoParam
	if vinculoEfetivo == "" {
		vinculoEfetivo = "todos"
	}

	var (
		codigosAcademia []string
		nomeAcademia    string
		joinTurmasRef   = "e.codigo_academia"
	)
	if userType == "academia" {
		academiaDTO, err := apoio.GetAcademiaProjection(c).GetByID(userID)
		if err != nil || academiaDTO == nil {
			utils.RespondWithForbiddenError(c, "academia não encontrada")
			return
		}
		codigosAcademia = []string{academiaDTO.CodigoAcademia}
		nomeAcademia = academiaDTO.Nome
	} else {
		codigosAcademia = apoio.ParseMultiValueQueryParam(c, "codigo_academia")
	}

	idxAcademias := 0
	if len(codigosAcademia) > 0 {
		args = append(args, pq.Array(codigosAcademia))
		idxAcademias = len(args)
		if userType == "academia" {
			// A turma mostrada/filtrada é sempre a da academia que consulta,
			// mesmo para estudantes que já seguiram para outra academia.
			joinTurmasRef = fmt.Sprintf("ANY($%d)", idxAcademias)
		}
	}
	// Para a academia, estudantes que já seguiram para OUTRA academia só
	// aparecem na listagem simples (sem filtros nem busca) e só com nome e
	// código: filtrar por atributos (status, ano, BI, telefone…) permitiria
	// descobrir dados que a academia anterior não pode ver (SEC-15).
	soDaAcademia := userType == "academia" && filtrosDoUtilizador
	conditions = append(conditions, condicoesVinculoListagem(vinculoEfetivo, idxAcademias, soDaAcademia)...)

	joinTurmas := fmt.Sprintf(`
		LEFT JOIN projection_turmas t
		  ON t.codigo_academia = %s
		 AND EXISTS (
		    SELECT 1 FROM jsonb_array_elements_text(COALESCE(t.estudantes, '[]'::jsonb)) AS cod(codigo)
		    WHERE cod.codigo = e.codigo_estudante
		 )`, joinTurmasRef)
	whereSQL := " WHERE " + strings.Join(conditions, " AND ")

	var totalGeral int
	if err = client.DB().QueryRow(
		"SELECT COUNT(DISTINCT e.id) FROM projection_estudantes e"+joinTurmas+whereSQL, args...,
	).Scan(&totalGeral); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	args = append(args, limit, offset)
	rows, err = client.DB().Query(
		selectCols+" e"+joinTurmas+whereSQL+fmt.Sprintf(" ORDER BY e.created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)),
		args...,
	)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	defer rows.Close()
	estudantes := scanEstudantesRows(rows)
	apoio.AnexarDeveAlterarSenha(client, "projection_estudantes", "codigo_estudante", "codigo_estudante", estudantes)
	anexarVinculoListagem(estudantes, codigosAcademia)
	if userType == "academia" {
		reduzirLinhasDeOutrasAcademias(estudantes, codigosAcademia)
	}

	if userType == "academia" {
		c.JSON(http.StatusOK, gin.H{
			"estudantes":      estudantes,
			"total":           len(estudantes),
			"total_geral":     totalGeral,
			"tipo_usuario":    "academia",
			"codigo_academia": codigosAcademia[0],
			"nome_academia":   nomeAcademia,
			"vinculo":         vinculoEfetivo,
			"limit":           limit,
			"offset":          offset,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"estudantes":   estudantes,
		"total":        len(estudantes),
		"total_geral":  totalGeral,
		"tipo_usuario": "admin",
		"vinculo":      vinculoEfetivo,
		"limit":        limit,
		"offset":       offset,
	})
}

// statusEstudanteVinculadoSQL lista os status em que o estudante está vinculado
// à academia indicada em codigo_academia (os restantes são "desvinculado").
const statusEstudanteVinculadoSQL = "e.status IN ('ativo', 'pendente_documentos')"

// condicoesVinculoListagem devolve as condições SQL do filtro ?vinculo=.
// idxAcademias é o índice ($n) do array de códigos de academia já presente em
// args (0 = sem filtro de academia, só possível para admin). Usa subconsultas
// EXISTS, nunca JOIN, para não duplicar linhas nem repetir cláusulas WHERE.
func condicoesVinculoListagem(vinculo string, idxAcademias int, soDaAcademia bool) []string {
	if idxAcademias == 0 {
		switch vinculo {
		case "atual":
			return []string{statusEstudanteVinculadoSQL}
		case "anterior":
			return []string{"NOT (" + statusEstudanteVinculadoSQL + ")"}
		}
		return nil
	}
	ref := fmt.Sprintf("ANY($%d)", idxAcademias)
	atual := fmt.Sprintf("(e.codigo_academia = %s AND %s)", ref, statusEstudanteVinculadoSQL)
	switch vinculo {
	case "atual":
		return []string{atual}
	case "anterior":
		if soDaAcademia {
			// só ex-estudantes que continuam registados na própria academia
			return []string{fmt.Sprintf("e.codigo_academia = %s", ref), "NOT " + atual}
		}
		return []string{
			fmt.Sprintf("EXISTS (SELECT 1 FROM projection_estudante_vinculos ev WHERE ev.estudante_id = e.id AND ev.codigo_academia = %s AND ev.desvinculado_em IS NOT NULL)", ref),
			"NOT " + atual,
		}
	}
	if soDaAcademia {
		return []string{fmt.Sprintf("e.codigo_academia = %s", ref)}
	}
	// "todos": quem está vinculado agora (regra antiga, funciona mesmo antes de a
	// projeção de vínculos alcançar o ledger) ou já esteve vinculado.
	return []string{fmt.Sprintf("(e.codigo_academia = %s OR EXISTS (SELECT 1 FROM projection_estudante_vinculos ev WHERE ev.estudante_id = e.id AND ev.codigo_academia = %s))", ref, ref)}
}

// reduzirLinhasDeOutrasAcademias mantém apenas nome, código e vínculo nas
// linhas de estudantes que já seguiram para outra academia; os restantes
// campos ficam nulos para não revelar a academia atual nem dados escolares.
func reduzirLinhasDeOutrasAcademias(estudantes []map[string]interface{}, codigosAcademia []string) {
	propria := make(map[string]struct{}, len(codigosAcademia))
	for _, codigo := range codigosAcademia {
		propria[codigo] = struct{}{}
	}
	for _, est := range estudantes {
		codigo, _ := est["codigo_academia"].(string)
		if codigo == "" {
			continue
		}
		if _, daAcademia := propria[codigo]; daAcademia {
			continue
		}
		for chave := range est {
			switch chave {
			case "nome", "codigo_estudante", "vinculo":
			case "total_notas", "total_faltas", "version":
				est[chave] = 0
			case "email_verificado":
				est[chave] = false
			default:
				est[chave] = nil
			}
		}
		est["vinculo"] = "anterior"
		est["acesso"] = "reduzido"
	}
}

// anexarVinculoListagem marca cada linha com a relação REAL do estudante com a
// academia consultada: "atual" ou "anterior" (e não o eco do filtro pedido).
func anexarVinculoListagem(estudantes []map[string]interface{}, codigosAcademia []string) {
	academias := make(map[string]struct{}, len(codigosAcademia))
	for _, codigo := range codigosAcademia {
		academias[codigo] = struct{}{}
	}
	for _, est := range estudantes {
		status, _ := est["status"].(string)
		vinculado := status == "ativo" || status == "pendente_documentos"
		if len(academias) > 0 {
			codigo, _ := est["codigo_academia"].(string)
			_, daAcademia := academias[codigo]
			vinculado = vinculado && daAcademia
		}
		if vinculado {
			est["vinculo"] = "atual"
		} else {
			est["vinculo"] = "anterior"
		}
	}
}

// scanEstudantesRows faz scan das linhas retornadas por ListarEstudantes.
// data_nascimento é NOT NULL no banco após a migration 043.
func scanEstudantesRows(rows *sql.Rows) []map[string]interface{} {
	var estudantes []map[string]interface{}
	for rows.Next() {
		var id, cursoMedioID, cursoSuperiorID sql.NullString
		var semestreAtual sql.NullInt64
		var nome, codigoEstudante string
		var status, statusFund, statusMedio, statusSuperior sql.NullString
		var email, telefone, bilhete, bilheteResp, codigoAcad sql.NullString
		var anoEscolar, anoEscolarMedio, anoSuperior sql.NullString
		var emailVerif bool
		var genero sql.NullString
		var dataNascimento, createdAt, updatedAt sql.NullTime
		var totalNotas, totalFaltas, version int

		if err := rows.Scan(
			&id, &nome, &codigoEstudante,
			&email, &telefone, &emailVerif, &bilhete, &bilheteResp, &codigoAcad,
			&status, &statusFund, &statusMedio, &statusSuperior,
			&anoEscolar, &anoEscolarMedio, &anoSuperior, &semestreAtual,
			&cursoMedioID, &cursoSuperiorID,
			&genero, &dataNascimento, &createdAt, &updatedAt,
			&totalNotas, &totalFaltas, &version,
		); err != nil {
			log.Printf("[ERROR] ListarEstudantes scan: %v", err)
			continue
		}

		estudantes = append(estudantes, map[string]interface{}{
			"nome":                           nome,
			"codigo_estudante":               codigoEstudante,
			"email":                          apoio.GetNullString(email),
			"telefone":                       apoio.GetNullString(telefone),
			"email_verificado":               emailVerif,
			"bilhete_identidade":             apoio.GetNullString(bilhete),
			"bilhete_identidade_encarregado": apoio.GetNullString(bilheteResp),
			"codigo_academia":                apoio.GetNullString(codigoAcad),
			"status":                         apoio.GetNullString(status),
			"status_escolar_fundamental":     apoio.GetNullString(statusFund),
			"status_escolar_medio":           apoio.GetNullString(statusMedio),
			"status_superior":                apoio.GetNullString(statusSuperior),
			"ano_escolar_fundamental":        apoio.GetNullString(anoEscolar),
			"ano_escolar_medio":              apoio.GetNullString(anoEscolarMedio),
			"ano_superior":                   apoio.GetNullString(anoSuperior),
			"semestre_atual":                 getNullInt64(semestreAtual),
			"curso_medio_id":                 apoio.GetNullString(cursoMedioID),
			"curso_superior_id":              apoio.GetNullString(cursoSuperiorID),
			"genero":                         apoio.GetNullString(genero),
			"data_nascimento":                formatNullDate(dataNascimento),
			"created_at":                     formatNullRFC3339(createdAt),
			"updated_at":                     formatNullRFC3339(updatedAt),
			"total_notas":                    totalNotas,
			"total_faltas":                   totalFaltas,
			"version":                        version,
		})
	}
	return estudantes
}

func parseMultiValueQueryParams(c *gin.Context, keys ...string) []string {
	parsedValues := make([]string, 0)
	seen := make(map[string]struct{})

	for _, key := range keys {
		for _, value := range apoio.ParseMultiValueQueryParam(c, key) {
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			parsedValues = append(parsedValues, value)
		}
	}

	return parsedValues
}

func parseMultiPositiveInt64QueryParam(c *gin.Context, key string) ([]int64, error) {
	values := apoio.ParseMultiValueQueryParam(c, key)
	if len(values) == 0 {
		return nil, nil
	}

	parsedValues := make([]int64, 0, len(values))
	for _, value := range values {
		parsedValue, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsedValue < 1 {
			return nil, fmt.Errorf("%s inválido", key)
		}
		parsedValues = append(parsedValues, parsedValue)
	}

	return parsedValues, nil
}

func getNullInt64(ns sql.NullInt64) interface{} {
	if ns.Valid {
		return ns.Int64
	}
	return nil
}

func formatNullDate(nt sql.NullTime) interface{} {
	if nt.Valid {
		return nt.Time.Format("2006-01-02")
	}
	return nil
}

func formatNullRFC3339(nt sql.NullTime) interface{} {
	if nt.Valid {
		return nt.Time.Format(time.RFC3339)
	}
	return nil
}

// ============================================================================
// GET /eventos-estudante/:codigo
// ============================================================================

func GetEventosEstudante(c *gin.Context) {
	codigoEstudante := c.Param("codigo")

	estudanteProj := apoio.GetEstudanteProjection(c)
	estudante, err := estudanteProj.GetByCodigo(codigoEstudante)
	if err != nil || estudante == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}

	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)

	// SEC-15: para academia, filtrar eventos pelo período de vínculo
	var periodos []projections.EstudanteVinculoDTO
	filtrarPorPeriodos := false
	if userType == "academia" {
		academiaProj := apoio.GetAcademiaProjection(c)
		academiaDTO, err := academiaProj.GetByID(userID)
		if err != nil || academiaDTO == nil {
			utils.RespondWithForbiddenError(c, "academia não encontrada")
			return
		}
		acesso, p, err := apoio.AcessoAcademiaAoEstudante(c, academiaDTO.CodigoAcademia, estudante)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if acesso == apoio.SemAcesso {
			utils.RespondWithForbiddenError(c, "Estudante não pertence nem pertenceu a esta academia")
			return
		}
		// Tanto a academia anterior como a atual só veem os eventos ocorridos
		// nos seus próprios períodos de vínculo (D8).
		periodos = p
		filtrarPorPeriodos = true
	}

	repository := apoio.GetRepository(c)
	eventos, err := repository.GetEventHistory(estudante.ID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	// SEC-15: filtrar eventos pelo período de vínculo (academia atual e anterior)
	if filtrarPorPeriodos {
		eventos = apoio.FiltrarEventosPorPeriodos(eventos, periodos)
	}

	// SEC-14: redigir hashes de senha dos eventos antes de retornar
	eventosRedigidos := redigirEventosSensiveis(eventos)

	c.JSON(http.StatusOK, gin.H{
		"codigo_estudante": codigoEstudante,
		"eventos":          eventosRedigidos,
		"total":            len(eventosRedigidos),
	})
}

// GetEventoAuditoria returns a ledger event only to an owner of the associated
// student aggregate (or to an administrator). Unauthorized lookups are 404 to
// avoid revealing event existence.
func GetEventoAuditoria(c *gin.Context) {
	eventID, err := uuid.Parse(c.Param("event_id"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("event_id inválido"))
		return
	}
	evento, err := apoio.GetRepository(c).GetEventByID(eventID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if evento == nil {
		utils.RespondWithNotFoundError(c, "evento")
		return
	}
	userType, _ := middleware.GetUserType(c)
	userID, _ := middleware.GetUserID(c)

	if userType == "admin" {
		// SEC-14: redigir hashes de senha mesmo para admins
		eventoRedigido := redigirEventoSensivel(evento)
		c.JSON(http.StatusOK, gin.H{"evento": eventoRedigido})
		return
	}

	estudante, err := apoio.GetEstudanteProjection(c).GetByID(evento.AggregateID)
	if err != nil || estudante == nil {
		utils.RespondWithNotFoundError(c, "evento")
		return
	}

	// SEC-15: para academia, verificar acesso e filtrar por período se for anterior
	if userType == "academia" {
		academiaProj := apoio.GetAcademiaProjection(c)
		academiaDTO, err := academiaProj.GetByID(userID)
		if err != nil || academiaDTO == nil {
			utils.RespondWithForbiddenError(c, "academia não encontrada")
			return
		}
		acesso, periodos, err := apoio.AcessoAcademiaAoEstudante(c, academiaDTO.CodigoAcademia, estudante)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if acesso == apoio.SemAcesso {
			utils.RespondWithNotFoundError(c, "evento")
			return
		}
		// Academia atual e anterior: só eventos dentro dos seus períodos de vínculo
		if !apoio.EventoDentroDoPeriodo(evento, periodos) {
			utils.RespondWithNotFoundError(c, "evento")
			return
		}
	} else if !apoio.PodeAuditarEstudante(c, estudante) {
		utils.RespondWithNotFoundError(c, "evento")
		return
	}

	// SEC-14: redigir hashes de senha do evento
	eventoRedigido := redigirEventoSensivel(evento)
	c.JSON(http.StatusOK, gin.H{"evento": eventoRedigido})
}

// ============================================================================
// GET /estudante/minhas-avaliacoes
// ============================================================================

func GetMinhasAvaliacoes(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)

	estudanteProj := apoio.GetEstudanteProjection(c)
	estudante, err := estudanteProj.GetByID(userID)
	if err != nil || estudante == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}

	avaliacaoProj := apoio.GetAvaliacaoFinalProjection(c)
	avaliacoes, err := avaliacaoProj.GetByEstudante(estudante.CodigoEstudante)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	avaliacoes, totalGeral, limit, offset := apoio.PaginarAvaliacoesFinais(c, avaliacoes)
	c.JSON(http.StatusOK, gin.H{
		"avaliacoes":  avaliacoes,
		"total":       len(avaliacoes),
		"total_geral": totalGeral,
		"limit":       limit,
		"offset":      offset,
	})
}

// ============================================================================
// GET /consultar-estudante/:codigo
// ============================================================================

// Perfil reduzido devolvido para academia anterior (SEC-15)
// Contém APENAS dados registrados por ESTA academia: identificação básica,
// status geral do estudante (sem detalhes de curso/ano/semestre atuais de outras academias)
// e períodos de vínculo EXCLUSIVAMENTE com a academia consultante.
type EstudantePerfilReduzido struct {
	ID              uuid.UUID `json:"id"`
	Nome            string    `json:"nome"`
	CodigoEstudante string    `json:"codigo_estudante"`
	// Status é sempre "desvinculado": a academia anterior não vê o estado atual
	// do estudante nem a academia onde ele está agora (SEC-15).
	Status string `json:"status"`
	// SEC-15: APENAS períodos de vínculo com a academia que está consultando.
	// O helper acessoAcademiaAoEstudante já filtra por codigo_academia via
	// projection_estudante_vinculos.GetByEstudanteAcademia.
	VinculosPeriodos []PeriodoVinculoDTO `json:"periodos_vinculo"`
}

type PeriodoVinculoDTO struct {
	CodigoAcademia string     `json:"codigo_academia"`
	VinculadoEm    time.Time  `json:"vinculado_em"`
	DesvinculadoEm *time.Time `json:"desvinculado_em,omitempty"`
	VinculoAtivo   bool       `json:"vinculo_ativo"`
}

func GetEstudantePorCodigo(c *gin.Context) {
	codigoEstudante := c.Param("codigo")

	userType, _ := middleware.GetUserType(c)
	if userType != "academia" && userType != "admin" {
		utils.RespondWithForbiddenError(c, "Apenas academias e administradores podem consultar estudantes.")
		return
	}

	estudanteProj := apoio.GetEstudanteProjection(c)
	estudante, err := estudanteProj.GetByCodigo(codigoEstudante)
	if err != nil || estudante == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}

	// SEC-13: bloquear estudante deletado
	if estudante.Status == "deletado" {
		utils.RespondWithError(c, http.StatusUnauthorized, "conta indisponível. Entre em contato com a sua academia ou com o suporte.", nil)
		return
	}

	if userType == "academia" {
		userID, _ := middleware.GetUserID(c)
		academiaProj := apoio.GetAcademiaProjection(c)
		academiaDTO, err := academiaProj.GetByID(userID)
		if err != nil || academiaDTO == nil {
			utils.RespondWithForbiddenError(c, "academia não encontrada")
			return
		}

		// SEC-15: usar helper de acesso por vínculo (permite academia atual E anterior)
		acesso, periodos, err := apoio.AcessoAcademiaAoEstudante(c, academiaDTO.CodigoAcademia, estudante)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if acesso == apoio.SemAcesso {
			utils.RespondWithForbiddenError(c, "Estudante não pertence nem pertenceu a esta academia")
			return
		}

		// SEC-12: bloquear se academia inativa
		if academiaDTO.Status != "ativo" {
			utils.RespondWithError(c, http.StatusUnauthorized, "academia inativa. Entre em contato com o administrador.", nil)
			return
		}

		// Se for acesso anterior, devolver perfil reduzido
		if acesso == apoio.AcessoAnterior {
			vinculosDTO := make([]PeriodoVinculoDTO, len(periodos))
			for i, p := range periodos {
				vinculosDTO[i] = PeriodoVinculoDTO{
					CodigoAcademia: p.CodigoAcademia,
					VinculadoEm:    p.VinculadoEm,
					DesvinculadoEm: p.DesvinculadoEm,
					VinculoAtivo:   p.DesvinculadoEm == nil,
				}
			}
			perfil := EstudantePerfilReduzido{
				ID:               estudante.ID,
				Nome:             estudante.Nome,
				CodigoEstudante:  estudante.CodigoEstudante,
				Status:           "desvinculado",
				VinculosPeriodos: vinculosDTO,
			}
			c.JSON(http.StatusOK, gin.H{"estudante": perfil, "tipo_acesso": "anterior"})
			return
		}
		// Se for acesso atual, continua para o perfil completo abaixo
	}

	var academiaInfo *gin.H
	if estudante.CodigoAcademia != nil {
		academiaProj := apoio.GetAcademiaProjection(c)
		academia, _ := academiaProj.GetByCodigo(*estudante.CodigoAcademia)
		if academia != nil {
			academiaInfo = &gin.H{
				"codigo": academia.CodigoAcademia,
				"nome":   academia.Nome,
				"nivel":  academia.Nivel,
				"type":   academia.Type,
			}
		}
	}

	var cursoMedioInfo, cursoSuperiorInfo *gin.H
	cursosProj := apoio.GetCursosProjection(c)

	if estudante.CursoMedioID != nil {
		cursoMedioUUID, err := uuid.Parse(*estudante.CursoMedioID)
		if err == nil {
			cursoMedio, _ := cursosProj.GetByID(cursoMedioUUID)
			if cursoMedio != nil {
				cursoMedioInfo = &gin.H{
					"id":     cursoMedio.ID,
					"nome":   cursoMedio.Nome,
					"type":   cursoMedio.Type,
					"status": cursoMedio.Status,
				}
			}
		}
	}

	if estudante.CursoSuperiorID != nil {
		cursoSuperiorUUID, err := uuid.Parse(*estudante.CursoSuperiorID)
		if err == nil {
			cursoSuperior, _ := cursosProj.GetByID(cursoSuperiorUUID)
			if cursoSuperior != nil {
				cursoSuperiorInfo = &gin.H{
					"id":     cursoSuperior.ID,
					"nome":   cursoSuperior.Nome,
					"type":   cursoSuperior.Type,
					"status": cursoSuperior.Status,
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"estudante": gin.H{
			"id":                             estudante.ID,
			"nome":                           estudante.Nome,
			"codigo_estudante":               estudante.CodigoEstudante,
			"email":                          estudante.Email,
			"telefone":                       estudante.Telefone,
			"email_verificado":               estudante.EmailVerificado,
			"bilhete_identidade":             estudante.BilheteIdentidade,
			"bilhete_identidade_encarregado": estudante.BilheteIdentidadeResp,
			"genero":                         estudante.Genero,
			"data_nascimento":                estudante.DataNascimento.Format("2006-01-02"),
			"codigo_academia":                estudante.CodigoAcademia,
			"academia":                       academiaInfo,
			"status":                         estudante.Status,
			"status_escolar_fundamental":     estudante.StatusEscolarFundamental,
			"status_escolar_medio":           estudante.StatusEscolarMedio,
			"status_superior":                estudante.StatusSuperior,
			"ano_escolar_fundamental":        estudante.AnoEscolar,
			"ano_escolar_medio":              estudante.AnoEscolarMedio,
			"ano_superior":                   estudante.AnoSuperior,
			"curso_medio":                    cursoMedioInfo,
			"curso_superior":                 cursoSuperiorInfo,
			"created_at":                     estudante.CreatedAt,
			"updated_at":                     estudante.UpdatedAt,
			"documentos":                     apoio.DocumentosComDownloadEstudante(estudante.CodigoEstudante, estudante.Documentos),
			"version":                        estudante.Version,
		},
		"tipo_acesso": "atual",
	})
}

func CompletarDocumentosEstudantePendente(c *gin.Context) {
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("multipart/form-data inválido"))
		return
	}
	if apoio.RejectRemovedMultipartFields(c) {
		return
	}
	if err := apoio.ValidarCamposArquivoMatricula(c.Request.MultipartForm); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	codigo := strings.TrimSpace(c.Param("codigo"))
	academiaID, ok := middleware.GetUserID(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	academia, err := apoio.GetAcademiaProjection(c).GetByID(academiaID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	proj, err := apoio.GetEstudanteProjection(c).GetByCodigo(codigo)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if academia == nil || proj == nil || proj.CodigoAcademia == nil || *proj.CodigoAcademia != academia.CodigoAcademia {
		utils.RespondWithError(c, http.StatusNotFound, "estudante pendente não encontrado", nil)
		return
	}
	if proj.Status != "pendente_documentos" {
		utils.RespondWithValidationError(c, fmt.Errorf("estudante não está pendente de documentos"))
		return
	}
	files := map[string]apoio.UploadedPDF{}
	for _, field := range apoio.SolicitacaoDocFields {
		if fh, err := c.FormFile(field); err == nil {
			pdf, err := apoio.ReadAndValidatePDF(field, fh)
			if err != nil {
				utils.RespondWithValidationError(c, err)
				return
			}
			files[field] = pdf
		}
	}
	docsVal := apoio.DocumentosMatriculaParaValidacao(files, strings.TrimSpace(c.PostForm("declaracao_ano_academico")))
	docsCompletosVal := map[string]aggregates.DocumentoMatricula{}
	for k, v := range proj.Documentos {
		docsCompletosVal[k] = v
	}
	for k, v := range docsVal {
		docsCompletosVal[k] = v
	}
	// Documentos extra: mesma lógica do cadastro direto, usando o ano
	// acadêmico já registrado para este estudante (proj.AnoEscolar/
	// AnoEscolarMedio/AnoSuperior — imutável neste fluxo de repescagem).
	anoAcademicoDocsExtra := apoio.ResolverAnoAcademicoParaDocumentosExtra(proj.AnoEscolar, proj.AnoEscolarMedio, proj.AnoSuperior)
	var catalogoDocsExtra []projections.DocumentoExtraDTO
	if anoAcademicoDocsExtra != "" {
		catalogoDocsExtra, err = apoio.GetDocumentosExtraProjection(c).GetAtivosPorAnoAcademico(academia.CodigoAcademia, anoAcademicoDocsExtra)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
	}
	docsExtraEnviados, err := apoio.ParseDocumentosExtra(c.Request.MultipartForm, catalogoDocsExtra)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	docsExtraJaEnviados := map[string]apoio.DocumentoExtraUpload{}
	for _, cat := range catalogoDocsExtra {
		if doc, ok := docsCompletosVal["documento_extra."+cat.ID.String()]; ok && doc.TemReferenciaArquivo() {
			docsExtraJaEnviados[cat.ID.String()] = apoio.DocumentoExtraUpload{Catalog: cat}
		}
	}
	for id, up := range docsExtraEnviados {
		docsExtraJaEnviados[id] = up
	}
	if err := apoio.ValidarObrigatoriedadeDocumentosExtra(catalogoDocsExtra, docsExtraJaEnviados); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if _, err := services.ValidateMatriculaCommon(services.MatriculaCommonInput{Contexto: services.MatriculaContextCadastroDireto, Nome: proj.Nome, Genero: proj.Genero, DataNascimento: proj.DataNascimento, Email: proj.Email, TelefoneEstudante: proj.Telefone, TelefoneEncarregado: proj.TelefoneEncarregado, BilheteIdentidade: proj.BilheteIdentidade, BilheteIdentidadeEncarregado: proj.BilheteIdentidadeResp, AnoEscolarFundamental: proj.AnoEscolar, AnoEscolarMedio: proj.AnoEscolarMedio, AnoSuperior: proj.AnoSuperior, Documentos: docsCompletosVal}); err != nil {
		faltantes := aggregates.DocumentosMatriculaFaltantes(proj.BilheteIdentidade, proj.BilheteIdentidadeResp, proj.AnoEscolar, proj.AnoEscolarMedio, proj.AnoSuperior, docsCompletosVal)
		utils.RespondWithErrorData(c, http.StatusBadRequest, err.Error(), err, gin.H{"documentos_faltantes": faltantes})
		return
	}
	provider := apoio.GetStorageProvider(c)
	if provider == nil {
		var err error
		provider, err = storage.NewStorageProvider()
		if err != nil {
			utils.RespondWithError(c, http.StatusServiceUnavailable, err.Error(), err)
			return
		}
	}
	dir := fmt.Sprintf("%s/estudantes/%s/documentos", academia.CodigoAcademia, codigo)
	if err := provider.EnsureDir(dir); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	documentos := map[string]aggregates.DocumentoMatricula{}
	for field, f := range files {
		storageTipo, storagePath := apoio.StoragePathDocumentoEstudante(dir, field, codigo, strings.TrimSpace(c.PostForm("declaracao_ano_academico")))
		stored, err := provider.Upload(storagePath, bytes.NewReader(f.Data), f.Size)
		if err != nil {
			_ = provider.Delete(dir)
			utils.RespondWithInternalError(c, err)
			return
		}
		key, doc := apoio.DocumentoMatriculaNormalizado(field, strings.TrimSpace(c.PostForm("declaracao_ano_academico")), apoio.EstudanteDocumentoDownloadURL(codigo, storageTipo), stored.Path, stored.FileURL)
		documentos[key] = doc
	}
	if len(docsExtraEnviados) > 0 {
		docsExtraArmazenados, err := apoio.ArmazenarDocumentosExtra(provider, dir, docsExtraEnviados, anoAcademicoDocsExtra, func(campo string) string {
			return apoio.EstudanteDocumentoDownloadURL(codigo, campo)
		})
		if err != nil {
			_ = provider.Delete(dir)
			utils.RespondWithInternalError(c, fmt.Errorf("falha no upload dos documentos extra: %w", err))
			return
		}
		for key, doc := range docsExtraArmazenados {
			documentos[key] = doc
		}
	}
	agg := aggregates.NewEstudante()
	loaded, err := apoio.GetRepository(c).Load(proj.ID, "Estudante")
	if err != nil {
		_ = provider.Delete(dir)
		utils.RespondWithInternalError(c, err)
		return
	}
	agg = loaded.(*aggregates.Estudante)
	if err := agg.CompletarDocumentosPendentes(documentos, academiaID); err != nil {
		_ = provider.Delete(dir)
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := apoio.GetRepository(c).SaveWithAudit(agg, db.AuditContext{UserID: academiaID.String(), UserType: "academia", IP: c.ClientIP()}); err != nil {
		_ = provider.Delete(dir)
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "documentos carregados com sucesso", "data": gin.H{"codigo_estudante": codigo, "status": "ativo", "documentos": documentos}})
}

// ============================================================================
// DELETE /estudante/conta — autodeleção (Tarefa 73)
// ============================================================================

// DeletarContaEstudante executa a autodeleção lógica e auditável da própria
// conta do estudante autenticado.
//
// Regra de negócio: só é permitida quando o estudante NÃO está vinculado a
// nenhuma academia no momento (Estudante.Deletar valida e.Status == "inativo"
// — NUNCA CodigoAcademia == nil, ver comentário no aggregate). Se o estudante
// ainda estiver vinculado, ele precisa primeiro solicitar e ter aprovada a
// desvinculação (fluxo já existente: POST /estudante/solicitacoes-status/desvinculacao)
// antes de poder deletar a conta.
//
// Notas, faltas e avaliações já lançadas permanecem intactas e consultáveis.
func DeletarContaEstudante(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)

	var req struct {
		Motivo string `json:"motivo" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Motivo) == "" {
		utils.RespondWithValidationError(c, fmt.Errorf("motivo é obrigatório"))
		return
	}

	repository := apoio.GetRepository(c)
	agg, err := repository.Load(userID, "Estudante")
	if err != nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}

	estudante, ok := agg.(*aggregates.Estudante)
	if !ok {
		utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
		return
	}

	if err := estudante.Deletar(req.Motivo, userID); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	audit := db.AuditContext{
		UserID:   userID.String(),
		UserType: "estudante",
		IP:       c.ClientIP(),
	}
	if err := repository.SaveWithAudit(estudante, audit); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	// SEC-15 / D10: ao deletar a conta, cancelar todas as inscrições ativas em serviços extras
	// da academia anterior (o estudante já está desvinculado, então usamos CodigoAcademia)
	if estudante.CodigoAcademia != nil {
		if err := apoio.CancelarInscricoesServicoExtraAoSair(c, estudante.CodigoEstudante, *estudante.CodigoAcademia, "Conta deletada pelo próprio estudante", "estudante"); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
	}

	log.Printf("Estudante autodeletado: %s - Motivo: %s", estudante.CodigoEstudante, req.Motivo)
	c.JSON(http.StatusOK, gin.H{
		"message":          "conta deletada com sucesso",
		"codigo_estudante": estudante.CodigoEstudante,
	})
}

// ============================================================================
// SEC-14: Redação de hashes de senha em endpoints de auditoria
// ============================================================================

// redigirEventosSensiveis redige campos sensíveis (hashes de senha) de uma lista de eventos.
func redigirEventosSensiveis(eventos []db.Event) []db.Event {
	redigidos := make([]db.Event, len(eventos))
	for i, e := range eventos {
		redigidos[i] = *redigirEventoSensivel(&e)
	}
	return redigidos
}

// redigirEventoSensivel redige campos sensíveis de um evento antes de o devolver pela API.
// Cria uma cópia do evento com o payload sanitizado.
//
// SEC-14 (Tarefa 127): a redação é GENÉRICA — qualquer chave (em qualquer nível do
// payload) cujo nome termine em "hash" ou seja uma senha em claro vira "[REDACTED]",
// seja qual for o tipo do evento. Assim, um evento novo que carregue um hash não volta a
// vazar por não constar de uma lista fixa. Se o payload não for JSON legível, ele é
// substituído por completo (falha fechada).
func redigirEventoSensivel(evento *db.Event) *db.Event {
	// Se o evento ou payload for nulo/vazio, não há o que redigir
	if evento == nil || len(evento.Payload) == 0 {
		return evento
	}

	// Cria uma cópia do evento para não modificar o original
	eventoRedigido := *evento

	var payload interface{}
	if err := json.Unmarshal(evento.Payload, &payload); err != nil {
		eventoRedigido.Payload = json.RawMessage(`{"payload":"[REDACTED]"}`)
		return &eventoRedigido
	}

	redigido, err := json.Marshal(redigirCamposSensiveis(payload))
	if err != nil {
		eventoRedigido.Payload = json.RawMessage(`{"payload":"[REDACTED]"}`)
		return &eventoRedigido
	}
	eventoRedigido.Payload = redigido
	return &eventoRedigido
}

// chaveSensivelDeEvento diz se o nome de um campo do payload guarda um segredo.
func chaveSensivelDeEvento(chave string) bool {
	c := strings.ToLower(strings.ReplaceAll(chave, "_", ""))
	if strings.HasSuffix(c, "hash") {
		return true
	}
	switch c {
	case "senha", "novasenha", "senhaatual", "password":
		return true
	}
	return false
}

// redigirCamposSensiveis percorre mapas e listas e substitui o valor de cada campo sensível.
func redigirCamposSensiveis(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for chave, valor := range t {
			if chaveSensivelDeEvento(chave) {
				t[chave] = "[REDACTED]"
				continue
			}
			t[chave] = redigirCamposSensiveis(valor)
		}
		return t
	case []interface{}:
		for i := range t {
			t[i] = redigirCamposSensiveis(t[i])
		}
		return t
	}
	return v
}
