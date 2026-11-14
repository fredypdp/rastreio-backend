package handlers

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
	"spuri/internal/middleware"
	"spuri/internal/projections"
	"spuri/internal/services"
	"spuri/internal/storage"
	"spuri/internal/utils"
)

// ============================================================================
// POST /academia/estudante/register
// ============================================================================

// CadastroEstudanteAcademiaRequest — genero e data_nascimento são obrigatórios.
type CadastroEstudanteAcademiaRequest struct {
	Nome                string                                   `json:"nome"            binding:"required"`
	Genero              string                                   `json:"genero"          binding:"required"`
	DataNascimento      time.Time                                `json:"data_nascimento" binding:"required"`
	Email               string                                   `json:"email"`
	Telefone            string                                   `json:"telefone"`
	TelefoneEncarregado string                                   `json:"telefone_encarregado"`
	BilheteIdentidade   string                                   `json:"bilhete_identidade"`
	BilheteEncarregado  string                                   `json:"bilhete_identidade_encarregado"`
	AnoEscolar          string                                   `json:"ano_escolar_fundamental"`
	AnoEscolarMedio     string                                   `json:"ano_escolar_medio"`
	AnoSuperior         string                                   `json:"ano_superior"`
	CursoMedioID        string                                   `json:"curso_medio_id"`
	CursoSuperiorID     string                                   `json:"curso_superior_id"`
	Documentos          map[string]aggregates.DocumentoMatricula `json:"documentos,omitempty"`
	CodigoTurma         string                                   `json:"codigo_turma"`
}

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
	if rejectRemovedMultipartFields(c) {
		return
	}
	if err := validarCamposArquivoMatricula(c.Request.MultipartForm); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	get := func(k string) string { return strings.TrimSpace(c.PostForm(k)) }
	dataNascimento, err := time.Parse("2006-01-02", get("data_nascimento"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("data_nascimento deve ser YYYY-MM-DD anterior à data atual"))
		return
	}
	req := CadastroEstudanteAcademiaRequest{
		Nome: get("nome"), Genero: get("genero"), DataNascimento: dataNascimento,
		Email: get("email"), Telefone: get("telefone"), TelefoneEncarregado: get("telefone_encarregado"),
		BilheteIdentidade: get("bilhete_identidade"), BilheteEncarregado: get("bilhete_identidade_encarregado"),
		AnoEscolar: get("ano_escolar_fundamental"), AnoEscolarMedio: get("ano_escolar_medio"), AnoSuperior: get("ano_superior"),
		CursoMedioID: get("curso_medio_id"), CursoSuperiorID: get("curso_superior_id"), CodigoTurma: get("codigo_turma"),
	}

	files := map[string]uploadedPDF{}
	for _, field := range solicitacaoDocFields {
		if fh, err := c.FormFile(field); err == nil {
			pdf, err := readAndValidatePDF(field, fh)
			if err != nil {
				utils.RespondWithValidationError(c, err)
				return
			}
			files[field] = pdf
		}
	}
	registerEstudantePorAcademiaComRequest(c, req, files, get("declaracao_ano_academico"))
}

func registerEstudantePorAcademiaComRequest(c *gin.Context, req CadastroEstudanteAcademiaRequest, files map[string]uploadedPDF, declaracaoAnoAcademico string) {
	registerEstudantePorAcademiaComRequestModo(c, req, files, declaracaoAnoAcademico, false)
}

func permitirPendenciaDocumentosEmFalhaStorage(c *gin.Context, pendenteDocumentos bool) bool {
	if pendenteDocumentos {
		return false
	}
	permitir, ok := c.Get("permitir_pendencia_documentos_em_falha_storage")
	return ok && permitir == true
}

func registerEstudantePorAcademiaComRequestModo(c *gin.Context, req CadastroEstudanteAcademiaRequest, files map[string]uploadedPDF, declaracaoAnoAcademico string, pendenteDocumentos bool) {
	academiaID, ok := middleware.GetUserID(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	academia, err := getAcademiaProjection(c).GetByID(academiaID)
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
		turmaDTO, err := getTurmasProjection(c).GetByCodigoTurma(codigoTurma, academia.CodigoAcademia)
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
		if err := validarCompatibilidadeEstudanteTurma(nil, nil, academia.AnosAcademicos, turmaDTO.Nivel, turmaDTO.CursoID, stringPtrIfNotBlank(req.AnoEscolar), stringPtrIfNotBlank(req.AnoEscolarMedio), stringPtrIfNotBlank(req.AnoSuperior), stringPtrIfNotBlank(req.CursoMedioID), stringPtrIfNotBlank(req.CursoSuperiorID)); err != nil {
			utils.RespondWithValidationError(c, fmt.Errorf("estudante incompatível com esta turma: %w", err))
			return
		}
	}

	cursoMedioUUID, cursoSuperiorUUID, err := validarCursosMatriculaCommon(c, academia.CodigoAcademia, req.CursoMedioID, req.CursoSuperiorID)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	// Documentos extra (catálogo definido pela academia) — pulados por
	// completo no modo "pendente de documentos", mesmo tratamento dado aos
	// documentos fixos (PularValidacaoDocumentos), já que este modo registra
	// o estudante deliberadamente sem nenhum arquivo por enquanto.
	var catalogoDocsExtra []projections.DocumentoExtraDTO
	var docsExtraEnviados map[string]documentoExtraUpload
	var anoAcademicoDocsExtra string
	if !pendenteDocumentos {
		anoAcademicoDocsExtra = resolverAnoAcademicoParaDocumentosExtra(stringPtrIfNotBlank(req.AnoEscolar), stringPtrIfNotBlank(req.AnoEscolarMedio), stringPtrIfNotBlank(req.AnoSuperior))
		if anoAcademicoDocsExtra != "" {
			catalogoDocsExtra, err = getDocumentosExtraProjection(c).GetAtivosPorAnoAcademico(academia.CodigoAcademia, anoAcademicoDocsExtra)
			if err != nil {
				utils.RespondWithInternalError(c, err)
				return
			}
		}
		docsExtraEnviados, err = parseDocumentosExtra(c.Request.MultipartForm, catalogoDocsExtra)
		if err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
		if err := validarObrigatoriedadeDocumentosExtra(catalogoDocsExtra, docsExtraEnviados); err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
	}
	documentosParaValidacao := documentosMatriculaParaValidacao(files, declaracaoAnoAcademico)
	for campo, documento := range req.Documentos {
		key, doc := documentoMatriculaNormalizadoComBase(campo, declaracaoAnoAcademico, "", documento)
		documentosParaValidacao[key] = doc
	}
	validado, err := services.ValidateMatriculaCommon(services.MatriculaCommonInput{
		Contexto: services.MatriculaContextCadastroDireto,
		Nome:     req.Nome, Genero: req.Genero, DataNascimento: req.DataNascimento,
		Email: stringPtrIfNotBlank(req.Email), TelefoneEstudante: stringPtrIfNotBlank(req.Telefone), TelefoneEncarregado: stringPtrIfNotBlank(req.TelefoneEncarregado),
		BilheteIdentidade: stringPtrIfNotBlank(req.BilheteIdentidade), BilheteIdentidadeEncarregado: stringPtrIfNotBlank(req.BilheteEncarregado),
		AnoEscolarFundamental: stringPtrIfNotBlank(req.AnoEscolar), AnoEscolarMedio: stringPtrIfNotBlank(req.AnoEscolarMedio), AnoSuperior: stringPtrIfNotBlank(req.AnoSuperior),
		Documentos:               documentosParaValidacao,
		PularValidacaoDocumentos: pendenteDocumentos,
	})
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := validateBIEncarregadoNaoConflitaComEscolar(c, &req.BilheteEncarregado, nil); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	// SEC-04: as credenciais seguem por SMS. Valida-se o telefone de destino ANTES
	// de criar a conta, para não criar contas cujas credenciais não possam ser entregues.
	telefoneDestinoCredenciais, err := TelefoneDestinoCredenciaisEstudante(validado.AnoEscolarFundamental, validado.AnoEscolarMedio, validado.AnoSuperior, validado.TelefoneEstudante, validado.TelefoneEncarregado)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}

	client := getDbClient(c)
	codigoEstudante, err := utils.GenerateUniqueCodigoEstudante(client.DB())
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	provider := getStorageProvider(c)
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
		key, doc := documentoMatriculaNormalizadoComBase(campo, declaracaoAnoAcademico, "", documento)
		documentos[key] = doc
	}
	for field, f := range files {
		storageTipo, storagePath := storagePathDocumentoEstudante(dir, field, codigoEstudante, declaracaoAnoAcademico)
		stored, err := provider.Upload(storagePath, bytes.NewReader(f.data), f.size)
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
		key, doc := documentoMatriculaNormalizado(field, declaracaoAnoAcademico, estudanteDocumentoDownloadURL(codigoEstudante, storageTipo), stored.Path, stored.FileURL)
		documentos[key] = doc
	}
	if len(docsExtraEnviados) > 0 {
		docsExtraArmazenados, err := armazenarDocumentosExtra(provider, dir, docsExtraEnviados, anoAcademicoDocsExtra, func(campo string) string {
			return estudanteDocumentoDownloadURL(codigoEstudante, campo)
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
		biGuard, err = db.NewUniqueOperationGuard(getDbClient(c)).WithContext(c.Request.Context()).Reserve(
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

		existente, err := getEstudanteProjection(c).GetByBilheteIdentidadePrincipal(*bilhetePtr)
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
	if err := getRepository(c).SaveWithAudit(estudante, audit); err != nil {
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
	atorCredenciais := atorComunicacao{ID: academiaID, Tipo: "academia", CodigoAcademia: academia.CodigoAcademia}
	credenciais := enviarCredenciaisSMS(c.Request.Context(), c, atorCredenciais, audit, telefoneDestinoCredenciais, req.Nome, codigoEstudante, senhaTemporaria, false)

	data := gin.H{"id": estudante.ID, "codigo_estudante": codigoEstudante, "codigo_academia": academia.CodigoAcademia, "documentos": documentos, "status": estudante.Status, "credenciais": credenciais}
	if codigoTurma != "" {
		data["codigo_turma"] = codigoTurma
		if err := vincularEstudanteATurma(c, academia, codigoEstudante, req.AnoEscolar, req.AnoEscolarMedio, req.AnoSuperior, req.CursoMedioID, req.CursoSuperiorID, codigoTurma, true, academiaID); err != nil {
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

	client := getDbClient(c)
	limit, offset := getPaginationParams(c)
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

	if generos := parseMultiValueQueryParam(c, "genero"); len(generos) > 0 {
		args = append(args, pq.Array(generos))
		conditions = append(conditions, fmt.Sprintf("e.genero = ANY($%d)", len(args)))
	}
	if status := parseMultiValueQueryParam(c, "status"); len(status) > 0 {
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
	if anosFund := parseMultiValueQueryParam(c, "ano_escolar_fundamental"); len(anosFund) > 0 {
		args = append(args, pq.Array(anosFund))
		conditions = append(conditions, fmt.Sprintf("e.ano_escolar_fundamental = ANY($%d)", len(args)))
	}
	if anosMedio := parseMultiValueQueryParam(c, "ano_escolar_medio"); len(anosMedio) > 0 {
		args = append(args, pq.Array(anosMedio))
		conditions = append(conditions, fmt.Sprintf("e.ano_escolar_medio = ANY($%d)", len(args)))
	}
	if anosSup := parseMultiValueQueryParam(c, "ano_superior"); len(anosSup) > 0 {
		args = append(args, pq.Array(anosSup))
		conditions = append(conditions, fmt.Sprintf("e.ano_superior = ANY($%d)", len(args)))
	}
	if turno := parseMultiValueQueryParam(c, "turno"); len(turno) > 0 {
		args = append(args, pq.Array(turno))
		conditions = append(conditions, fmt.Sprintf("t.turno = ANY($%d)", len(args)))
	}
	if codigoTurma := parseMultiValueQueryParam(c, "codigo_turma"); len(codigoTurma) > 0 {
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
		if values := parseMultiValueQueryParam(c, item.key); len(values) > 0 {
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
	conditions = append(conditions, "e.status <> 'deletado'")

	// SEC-15: filtro por tipo de vínculo (atual, anterior, todos)
	// Só aplica para academia e admin
	vinculoParam := strings.TrimSpace(c.Query("vinculo"))
	if userType == "academia" || userType == "admin" {
		switch vinculoParam {
		case "atual", "anterior", "todos", "":
			// Valores válidos - processados abaixo
		default:
			utils.RespondWithValidationError(c, fmt.Errorf("vinculo inválido: valores aceitos são atual, anterior ou todos"))
			return
		}
	}

	baseQuery := selectCols + ` e
		LEFT JOIN projection_turmas t
		  ON t.codigo_academia = e.codigo_academia
		 AND EXISTS (
		    SELECT 1 FROM jsonb_array_elements_text(COALESCE(t.estudantes, '[]'::jsonb)) AS cod(codigo)
		    WHERE cod.codigo = e.codigo_estudante
		 )`

	if userType == "academia" {
		academiaProj := getAcademiaProjection(c)
		academiaDTO, err := academiaProj.GetByID(userID)
		if err != nil || academiaDTO == nil {
			utils.RespondWithForbiddenError(c, "academia não encontrada")
			return
		}

		// SEC-15: construir query com filtro de vínculo
		var where []string
		var argsAcademia []interface{}
		baseAcademiaQuery := baseQuery

		switch vinculoParam {
		case "atual":
			// Vínculo atual: estudante com codigo_academia = academia E status ativo/pendente
			where = append([]string{}, conditions...)
			where = append(where, "e.codigo_academia = $1 AND e.status IN ('ativo', 'pendente_documentos')")
			argsAcademia = append(argsAcademia, academiaDTO.CodigoAcademia)
			argsAcademia = append(argsAcademia, args[1:]...) // skip potential first arg if any
		case "anterior":
			// Vínculo anterior: estudante que já teve vínculo com a academia mas não tem atual
			// Usa JOIN com projection_estudante_vinculos
			baseAcademiaQuery = selectCols + ` e
				INNER JOIN projection_estudante_vinculos ev
				  ON ev.estudante_id = e.id
				 AND ev.codigo_academia = $1
				 AND ev.desvinculado_em IS NOT NULL
				LEFT JOIN projection_turmas t
				  ON t.codigo_academia = e.codigo_academia
				 AND EXISTS (
				    SELECT 1 FROM jsonb_array_elements_text(COALESCE(t.estudantes, '[]'::jsonb)) AS cod(codigo)
				    WHERE cod.codigo = e.codigo_estudante
				 )
				WHERE e.id NOT IN (
				    SELECT estudante_id FROM projection_estudante_vinculos
				    WHERE codigo_academia = $1 AND desvinculado_em IS NULL
				)`
			argsAcademia = append(argsAcademia, academiaDTO.CodigoAcademia)
			argsAcademia = append(argsAcademia, args[1:]...)
			where = conditions
		case "todos", "":
			// Todos: estudantes que já tiveram vínculo com a academia (atual ou anterior)
			baseAcademiaQuery = selectCols + ` e
				INNER JOIN projection_estudante_vinculos ev
				  ON ev.estudante_id = e.id
				 AND ev.codigo_academia = $1
				LEFT JOIN projection_turmas t
				  ON t.codigo_academia = e.codigo_academia
				 AND EXISTS (
				    SELECT 1 FROM jsonb_array_elements_text(COALESCE(t.estudantes, '[]'::jsonb)) AS cod(codigo)
				    WHERE cod.codigo = e.codigo_estudante
				 )`
			argsAcademia = append(argsAcademia, academiaDTO.CodigoAcademia)
			argsAcademia = append(argsAcademia, args[1:]...)
			where = conditions
		}

		whereSQL := ``
		if len(where) > 0 {
			whereSQL = ` WHERE ` + strings.Join(where, " AND ")
		}

		// Para queries com JOIN, precisamos de DISTINCT no COUNT
		countQuery := `SELECT COUNT(DISTINCT e.id) FROM projection_estudantes e `
		if vinculoParam == "anterior" {
			countQuery = `SELECT COUNT(DISTINCT e.id) FROM projection_estudantes e
				INNER JOIN projection_estudante_vinculos ev
				  ON ev.estudante_id = e.id
				 AND ev.codigo_academia = $1
				 AND ev.desvinculado_em IS NOT NULL
				WHERE e.id NOT IN (
				    SELECT estudante_id FROM projection_estudante_vinculos
				    WHERE codigo_academia = $1 AND desvinculado_em IS NULL
				)`
		} else if vinculoParam == "todos" || vinculoParam == "" {
			countQuery = `SELECT COUNT(DISTINCT e.id) FROM projection_estudantes e
				INNER JOIN projection_estudante_vinculos ev
				  ON ev.estudante_id = e.id
				 AND ev.codigo_academia = $1`
		} else {
			countQuery = `SELECT COUNT(DISTINCT e.id) FROM projection_estudantes e
				LEFT JOIN projection_turmas t
				  ON t.codigo_academia = e.codigo_academia
				 AND EXISTS (
				    SELECT 1 FROM jsonb_array_elements_text(COALESCE(t.estudantes, '[]'::jsonb)) AS cod(codigo)
				    WHERE cod.codigo = e.codigo_estudante
				 )`
		}
		countQuery += whereSQL

		var totalGeral int
		if err = client.DB().QueryRow(countQuery, argsAcademia...).Scan(&totalGeral); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		argsAcademia = append(argsAcademia, limit, offset)
		rows, err = client.DB().Query(
			baseAcademiaQuery+whereSQL+fmt.Sprintf(` ORDER BY e.created_at DESC LIMIT $%d OFFSET $%d`, len(argsAcademia)-1, len(argsAcademia)),
			argsAcademia...,
		)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		defer rows.Close()
		estudantes := scanEstudantesRows(rows)
		anexarDeveAlterarSenha(client, "projection_estudantes", "codigo_estudante", "codigo_estudante", estudantes)

		// Adicionar info de vínculo a cada estudante
		for _, est := range estudantes {
			est["vinculo"] = vinculoParam
			if vinculoParam == "" {
				est["vinculo"] = "todos"
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"estudantes":      estudantes,
			"total":           len(estudantes),
			"total_geral":     totalGeral,
			"tipo_usuario":    "academia",
			"codigo_academia": academiaDTO.CodigoAcademia,
			"nome_academia":   academiaDTO.Nome,
			"limit":           limit,
			"offset":          offset,
		})
		return
	}

	if userType == "admin" {
		// Admin pode filtrar por codigo_academia (multi-value) e por vinculo
		codigosAcademia := parseMultiValueQueryParam(c, "codigo_academia")
		if len(codigosAcademia) > 0 {
			args = append(args, pq.Array(codigosAcademia))
			conditions = append(conditions, fmt.Sprintf("e.codigo_academia = ANY($%d)", len(args)))
		}

		var adminBaseQuery string
		var adminArgs []interface{}
		var adminWhere []string
		var adminCountQuery string

		switch vinculoParam {
		case "atual":
			adminBaseQuery = baseQuery
			adminArgs = append(adminArgs, args...)
			adminWhere = conditions
			adminCountQuery = `SELECT COUNT(DISTINCT e.id) FROM projection_estudantes e
				LEFT JOIN projection_turmas t
				  ON t.codigo_academia = e.codigo_academia
				 AND EXISTS (
				    SELECT 1 FROM jsonb_array_elements_text(COALESCE(t.estudantes, '[]'::jsonb)) AS cod(codigo)
				    WHERE cod.codigo = e.codigo_estudante
				 )`
		case "anterior":
			if len(codigosAcademia) == 0 {
				utils.RespondWithValidationError(c, fmt.Errorf("filtro vinculo=anterior requer codigo_academia"))
				return
			}
			adminBaseQuery = selectCols + ` e
				INNER JOIN projection_estudante_vinculos ev
				  ON ev.estudante_id = e.id
				 AND ev.codigo_academia = ANY($1)
				 AND ev.desvinculado_em IS NOT NULL
				LEFT JOIN projection_turmas t
				  ON t.codigo_academia = e.codigo_academia
				 AND EXISTS (
				    SELECT 1 FROM jsonb_array_elements_text(COALESCE(t.estudantes, '[]'::jsonb)) AS cod(codigo)
				    WHERE cod.codigo = e.codigo_estudante
				 )
				WHERE e.id NOT IN (
				    SELECT estudante_id FROM projection_estudante_vinculos
				    WHERE codigo_academia = ANY($1) AND desvinculado_em IS NULL
				)`
			adminArgs = append(adminArgs, pq.Array(codigosAcademia))
			if len(args) > 1 {
				adminArgs = append(adminArgs, args[1:]...)
			}
			adminWhere = conditions
			adminCountQuery = `SELECT COUNT(DISTINCT e.id) FROM projection_estudantes e
				INNER JOIN projection_estudante_vinculos ev
				  ON ev.estudante_id = e.id
				 AND ev.codigo_academia = ANY($1)
				 AND ev.desvinculado_em IS NOT NULL
				WHERE e.id NOT IN (
				    SELECT estudante_id FROM projection_estudante_vinculos
				    WHERE codigo_academia = ANY($1) AND desvinculado_em IS NULL
				)`
		case "todos", "":
			if len(codigosAcademia) == 0 {
				// Sem filtro de academia, retorna todos os estudantes (comportamento original)
				adminBaseQuery = baseQuery
				adminArgs = append(adminArgs, args...)
				adminWhere = conditions
				adminCountQuery = `SELECT COUNT(DISTINCT e.id) FROM projection_estudantes e
					LEFT JOIN projection_turmas t
					  ON t.codigo_academia = e.codigo_academia
					 AND EXISTS (
					    SELECT 1 FROM jsonb_array_elements_text(COALESCE(t.estudantes, '[]'::jsonb)) AS cod(codigo)
					    WHERE cod.codigo = e.codigo_estudante
					 )`
			} else {
				adminBaseQuery = selectCols + ` e
					INNER JOIN projection_estudante_vinculos ev
					  ON ev.estudante_id = e.id
					 AND ev.codigo_academia = ANY($1)
					LEFT JOIN projection_turmas t
					  ON t.codigo_academia = e.codigo_academia
					 AND EXISTS (
					    SELECT 1 FROM jsonb_array_elements_text(COALESCE(t.estudantes, '[]'::jsonb)) AS cod(codigo)
					    WHERE cod.codigo = e.codigo_estudante
					 )`
				adminArgs = append(adminArgs, pq.Array(codigosAcademia))
				if len(args) > 1 {
					adminArgs = append(adminArgs, args[1:]...)
				}
				adminWhere = conditions
				adminCountQuery = `SELECT COUNT(DISTINCT e.id) FROM projection_estudantes e
					INNER JOIN projection_estudante_vinculos ev
					  ON ev.estudante_id = e.id
					 AND ev.codigo_academia = ANY($1)`
			}
		}

		adminWhereSQL := ``
		if len(adminWhere) > 0 {
			adminWhereSQL = ` WHERE ` + strings.Join(adminWhere, " AND ")
		}

		var totalGeral int
		if err = client.DB().QueryRow(adminCountQuery+adminWhereSQL, adminArgs...).Scan(&totalGeral); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		adminArgs = append(adminArgs, limit, offset)
		query := adminBaseQuery + adminWhereSQL + fmt.Sprintf(` ORDER BY e.created_at DESC LIMIT $%d OFFSET $%d`, len(adminArgs)-1, len(adminArgs))
		rows, err = client.DB().Query(query, adminArgs...)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		defer rows.Close()
		estudantes := scanEstudantesRows(rows)
		anexarDeveAlterarSenha(client, "projection_estudantes", "codigo_estudante", "codigo_estudante", estudantes)
		c.JSON(http.StatusOK, gin.H{
			"estudantes":   estudantes,
			"total":        len(estudantes),
			"total_geral":  totalGeral,
			"tipo_usuario": "admin",
			"limit":        limit,
			"offset":       offset,
		})
		return
	}

	utils.RespondWithForbiddenError(c, "Acesso negado. Apenas academias e administradores podem listar estudantes.")
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
			"email":                          getNullString(email),
			"telefone":                       getNullString(telefone),
			"email_verificado":               emailVerif,
			"bilhete_identidade":             getNullString(bilhete),
			"bilhete_identidade_encarregado": getNullString(bilheteResp),
			"codigo_academia":                getNullString(codigoAcad),
			"status":                         getNullString(status),
			"status_escolar_fundamental":     getNullString(statusFund),
			"status_escolar_medio":           getNullString(statusMedio),
			"status_superior":                getNullString(statusSuperior),
			"ano_escolar_fundamental":        getNullString(anoEscolar),
			"ano_escolar_medio":              getNullString(anoEscolarMedio),
			"ano_superior":                   getNullString(anoSuperior),
			"semestre_atual":                 getNullInt64(semestreAtual),
			"curso_medio_id":                 getNullString(cursoMedioID),
			"curso_superior_id":              getNullString(cursoSuperiorID),
			"genero":                         getNullString(genero),
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
		for _, value := range parseMultiValueQueryParam(c, key) {
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
	values := parseMultiValueQueryParam(c, key)
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

	estudanteProj := getEstudanteProjection(c)
	estudante, err := estudanteProj.GetByCodigo(codigoEstudante)
	if err != nil || estudante == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}

	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)

	// SEC-15: para academia, filtrar eventos pelo período de vínculo
	var periodos []projections.EstudanteVinculoDTO
	if userType == "academia" {
		academiaProj := getAcademiaProjection(c)
		academiaDTO, err := academiaProj.GetByID(userID)
		if err != nil || academiaDTO == nil {
			utils.RespondWithForbiddenError(c, "academia não encontrada")
			return
		}
		acesso, p, err := acessoAcademiaAoEstudante(c, academiaDTO.CodigoAcademia, estudante)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if acesso == SemAcesso {
			utils.RespondWithForbiddenError(c, "Estudante não pertence nem pertenceu a esta academia")
			return
		}
		if acesso == AcessoAnterior {
			periodos = p
		}
		// AcessoAtual: periodos fica nil (sem filtro)
	}

	repository := getRepository(c)
	eventos, err := repository.GetEventHistory(estudante.ID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	// SEC-15: filtrar eventos pelo período de vínculo (se for academia anterior)
	if len(periodos) > 0 {
		eventos = filtrarEventosPorPeriodos(eventos, periodos)
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
	evento, err := getRepository(c).GetEventByID(eventID)
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

	estudante, err := getEstudanteProjection(c).GetByID(evento.AggregateID)
	if err != nil || estudante == nil {
		utils.RespondWithNotFoundError(c, "evento")
		return
	}

	// SEC-15: para academia, verificar acesso e filtrar por período se for anterior
	if userType == "academia" {
		academiaProj := getAcademiaProjection(c)
		academiaDTO, err := academiaProj.GetByID(userID)
		if err != nil || academiaDTO == nil {
			utils.RespondWithForbiddenError(c, "academia não encontrada")
			return
		}
		acesso, periodos, err := acessoAcademiaAoEstudante(c, academiaDTO.CodigoAcademia, estudante)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if acesso == SemAcesso {
			utils.RespondWithNotFoundError(c, "evento")
			return
		}
		if acesso == AcessoAnterior {
			// Verificar se o evento está dentro do período de vínculo
			if !eventoDentroDoPeriodo(evento, periodos) {
				utils.RespondWithNotFoundError(c, "evento")
				return
			}
		}
		// AcessoAtual: sem filtro adicional
	} else if !podeAuditarEstudante(c, estudante) {
		utils.RespondWithNotFoundError(c, "evento")
		return
	}

	// SEC-14: redigir hashes de senha do evento
	eventoRedigido := redigirEventoSensivel(evento)
	c.JSON(http.StatusOK, gin.H{"evento": eventoRedigido})
}

func podeAuditarEstudante(c *gin.Context, estudante *projections.EstudanteDTO) bool {
	if estudante == nil {
		return false
	}
	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)
	switch userType {
	case "admin":
		return true
	case "estudante":
		return userID == estudante.ID
	case "academia":
		academia, err := getAcademiaProjection(c).GetByID(userID)
		return err == nil && academia != nil && estudantePertenceAAcademia(estudante, academia.CodigoAcademia)
	default:
		return false
	}
}

func estudantePertenceAAcademia(estudante *projections.EstudanteDTO, codigoAcademia string) bool {
	return estudante != nil && estudante.CodigoAcademia != nil && *estudante.CodigoAcademia == codigoAcademia
}

// ============================================================================
// GET /estudante/minhas-avaliacoes
// ============================================================================

func GetMinhasAvaliacoes(c *gin.Context) {
	userID, _ := middleware.GetUserID(c)

	estudanteProj := getEstudanteProjection(c)
	estudante, err := estudanteProj.GetByID(userID)
	if err != nil || estudante == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return
	}

	avaliacaoProj := getAvaliacaoFinalProjection(c)
	avaliacoes, err := avaliacaoProj.GetByEstudante(estudante.CodigoEstudante)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	avaliacoes, totalGeral, limit, offset := paginarAvaliacoesFinais(c, avaliacoes)
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
type EstudantePerfilReduzido struct {
	ID                   uuid.UUID     `json:"id"`
	Nome                 string        `json:"nome"`
	CodigoEstudante      string        `json:"codigo_estudante"`
	CodigoAcademia       *string       `json:"codigo_academia"`
	Status               string        `json:"status"`
	StatusEscolarFund    string        `json:"status_escolar_fundamental"`
	StatusEscolarMedio   string        `json:"status_escolar_medio"`
	StatusSuperior       string        `json:"status_superior"`
	VinculosPeriodos     []PeriodoVinculoDTO `json:"periodos_vinculo"`
}

type PeriodoVinculoDTO struct {
	CodigoAcademia string      `json:"codigo_academia"`
	VinculadoEm    time.Time   `json:"vinculado_em"`
	DesvinculadoEm *time.Time  `json:"desvinculado_em,omitempty"`
	VinculoAtivo   bool        `json:"vinculo_ativo"`
}

func GetEstudantePorCodigo(c *gin.Context) {
	codigoEstudante := c.Param("codigo")

	userType, _ := middleware.GetUserType(c)
	if userType != "academia" && userType != "admin" {
		utils.RespondWithForbiddenError(c, "Apenas academias e administradores podem consultar estudantes.")
		return
	}

	estudanteProj := getEstudanteProjection(c)
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
		academiaProj := getAcademiaProjection(c)
		academiaDTO, err := academiaProj.GetByID(userID)
		if err != nil || academiaDTO == nil {
			utils.RespondWithForbiddenError(c, "academia não encontrada")
			return
		}

		// SEC-15: usar helper de acesso por vínculo (permite academia atual E anterior)
		acesso, periodos, err := acessoAcademiaAoEstudante(c, academiaDTO.CodigoAcademia, estudante)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		if acesso == SemAcesso {
			utils.RespondWithForbiddenError(c, "Estudante não pertence nem pertenceu a esta academia")
			return
		}

		// SEC-12: bloquear se academia inativa
		if academiaDTO.Status != "ativo" {
			utils.RespondWithError(c, http.StatusUnauthorized, "academia inativa. Entre em contato com o administrador.", nil)
			return
		}

		// Se for acesso anterior, devolver perfil reduzido
		if acesso == AcessoAnterior {
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
				ID:                  estudante.ID,
				Nome:                estudante.Nome,
				CodigoEstudante:     estudante.CodigoEstudante,
				CodigoAcademia:      estudante.CodigoAcademia,
				Status:              estudante.Status,
				StatusEscolarFund:   estudante.StatusEscolarFundamental,
				StatusEscolarMedio:  estudante.StatusEscolarMedio,
				StatusSuperior:      estudante.StatusSuperior,
				VinculosPeriodos:    vinculosDTO,
			}
			c.JSON(http.StatusOK, gin.H{"estudante": perfil, "tipo_acesso": "anterior"})
			return
		}
		// Se for acesso atual, continua para o perfil completo abaixo
	}

	var academiaInfo *gin.H
	if estudante.CodigoAcademia != nil {
		academiaProj := getAcademiaProjection(c)
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
	cursosProj := getCursosProjection(c)

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
			"documentos":                     documentosComDownloadEstudante(estudante.CodigoEstudante, estudante.Documentos),
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
	if rejectRemovedMultipartFields(c) {
		return
	}
	if err := validarCamposArquivoMatricula(c.Request.MultipartForm); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	codigo := strings.TrimSpace(c.Param("codigo"))
	academiaID, ok := middleware.GetUserID(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return
	}
	academia, err := getAcademiaProjection(c).GetByID(academiaID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	proj, err := getEstudanteProjection(c).GetByCodigo(codigo)
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
	files := map[string]uploadedPDF{}
	for _, field := range solicitacaoDocFields {
		if fh, err := c.FormFile(field); err == nil {
			pdf, err := readAndValidatePDF(field, fh)
			if err != nil {
				utils.RespondWithValidationError(c, err)
				return
			}
			files[field] = pdf
		}
	}
	docsVal := documentosMatriculaParaValidacao(files, strings.TrimSpace(c.PostForm("declaracao_ano_academico")))
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
	anoAcademicoDocsExtra := resolverAnoAcademicoParaDocumentosExtra(proj.AnoEscolar, proj.AnoEscolarMedio, proj.AnoSuperior)
	var catalogoDocsExtra []projections.DocumentoExtraDTO
	if anoAcademicoDocsExtra != "" {
		catalogoDocsExtra, err = getDocumentosExtraProjection(c).GetAtivosPorAnoAcademico(academia.CodigoAcademia, anoAcademicoDocsExtra)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
	}
	docsExtraEnviados, err := parseDocumentosExtra(c.Request.MultipartForm, catalogoDocsExtra)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	docsExtraJaEnviados := map[string]documentoExtraUpload{}
	for _, cat := range catalogoDocsExtra {
		if doc, ok := docsCompletosVal["documento_extra."+cat.ID.String()]; ok && doc.TemReferenciaArquivo() {
			docsExtraJaEnviados[cat.ID.String()] = documentoExtraUpload{Catalog: cat}
		}
	}
	for id, up := range docsExtraEnviados {
		docsExtraJaEnviados[id] = up
	}
	if err := validarObrigatoriedadeDocumentosExtra(catalogoDocsExtra, docsExtraJaEnviados); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if _, err := services.ValidateMatriculaCommon(services.MatriculaCommonInput{Contexto: services.MatriculaContextCadastroDireto, Nome: proj.Nome, Genero: proj.Genero, DataNascimento: proj.DataNascimento, Email: proj.Email, TelefoneEstudante: proj.Telefone, TelefoneEncarregado: proj.TelefoneEncarregado, BilheteIdentidade: proj.BilheteIdentidade, BilheteIdentidadeEncarregado: proj.BilheteIdentidadeResp, AnoEscolarFundamental: proj.AnoEscolar, AnoEscolarMedio: proj.AnoEscolarMedio, AnoSuperior: proj.AnoSuperior, Documentos: docsCompletosVal}); err != nil {
		faltantes := aggregates.DocumentosMatriculaFaltantes(proj.BilheteIdentidade, proj.BilheteIdentidadeResp, proj.AnoEscolar, proj.AnoEscolarMedio, proj.AnoSuperior, docsCompletosVal)
		utils.RespondWithErrorData(c, http.StatusBadRequest, err.Error(), err, gin.H{"documentos_faltantes": faltantes})
		return
	}
	provider := getStorageProvider(c)
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
		storageTipo, storagePath := storagePathDocumentoEstudante(dir, field, codigo, strings.TrimSpace(c.PostForm("declaracao_ano_academico")))
		stored, err := provider.Upload(storagePath, bytes.NewReader(f.data), f.size)
		if err != nil {
			_ = provider.Delete(dir)
			utils.RespondWithInternalError(c, err)
			return
		}
		key, doc := documentoMatriculaNormalizado(field, strings.TrimSpace(c.PostForm("declaracao_ano_academico")), estudanteDocumentoDownloadURL(codigo, storageTipo), stored.Path, stored.FileURL)
		documentos[key] = doc
	}
	if len(docsExtraEnviados) > 0 {
		docsExtraArmazenados, err := armazenarDocumentosExtra(provider, dir, docsExtraEnviados, anoAcademicoDocsExtra, func(campo string) string {
			return estudanteDocumentoDownloadURL(codigo, campo)
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
	loaded, err := getRepository(c).Load(proj.ID, "Estudante")
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
	if err := getRepository(c).SaveWithAudit(agg, db.AuditContext{UserID: academiaID.String(), UserType: "academia", IP: c.ClientIP()}); err != nil {
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

	repository := getRepository(c)
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
		if err := cancelarInscricoesServicoExtraAoSair(c, estudante.CodigoEstudante, *estudante.CodigoAcademia, "Conta deletada pelo próprio estudante", "estudante"); err != nil {
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

// redigirEventoSensivel redige campos sensíveis (hashes de senha) de um evento.
// Cria uma cópia do evento com o payload sanitizado.
func redigirEventoSensivel(evento *db.Event) *db.Event {
	// Eventos que contêm hashes de senha:
	// - EstudanteCriadoComVinculo: SenhaHash
	// - SenhaAlterada (estudante): NovaSenhaHash
	// - AcademiaCriada: SenhaHash
	// - AcademiaSenhaAlterada: NovaSenhaHash
	// - AdminCriado: SenhaHash
	// - AdminSenhaAlterada: NovaSenhaHash

	// Se o evento ou payload for nulo/vazio, retorna o evento original
	if evento == nil || len(evento.Payload) == 0 {
		return evento
	}

	// Cria uma cópia do evento para não modificar o original
	eventoRedigido := *evento

	// Unmarshal do payload para map genérico
	var payload map[string]interface{}
	if err := json.Unmarshal(evento.Payload, &payload); err != nil {
		// Se falhar ao fazer unmarshal, retorna o evento original (falha segura)
		return evento
	}

	// Redigir campos sensíveis baseado no tipo do evento
	switch evento.EventType {
	case "EstudanteCriadoComVinculo":
		if _, ok := payload["SenhaHash"]; ok {
			payload["SenhaHash"] = "[REDACTED]"
		}
	case "SenhaAlterada": // Estudante
		if _, ok := payload["NovaSenhaHash"]; ok {
			payload["NovaSenhaHash"] = "[REDACTED]"
		}
	case "AcademiaCriada":
		if _, ok := payload["SenhaHash"]; ok {
			payload["SenhaHash"] = "[REDACTED]"
		}
	case "AcademiaSenhaAlterada":
		if _, ok := payload["NovaSenhaHash"]; ok {
			payload["NovaSenhaHash"] = "[REDACTED]"
		}
	case "AdminCriado":
		if _, ok := payload["SenhaHash"]; ok {
			payload["SenhaHash"] = "[REDACTED]"
		}
	case "AdminSenhaAlterada":
		if _, ok := payload["NovaSenhaHash"]; ok {
			payload["NovaSenhaHash"] = "[REDACTED]"
		}
	}

	// Remarshal do payload sanitizado
	redigidoJSON, err := json.Marshal(payload)
	if err != nil {
		// Se falhar ao fazer marshal, retorna o evento original (falha segura)
		return evento
	}

	eventoRedigido.Payload = redigidoJSON
	return &eventoRedigido
}
