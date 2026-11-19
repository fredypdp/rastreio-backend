package apoio

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/jobs"
	"spuri/internal/middleware"
	"spuri/internal/utils"
)

// enqueueAsyncBatch é o helper central para todos os endpoints batch assíncronos.
// Valida o tamanho do array, cria o job no store e o enfileira no worker.
// Retorna 202 Accepted com o job_id para polling.
func EnqueueAsyncBatch(c *gin.Context, jobType jobs.JobType, maxItems int) {
	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)

	// Ler body bruto e validar que é um array JSON sem re-serializar.
	// Isso reduz CPU/memória em batches grandes (ex.: notas/faltas com 2000 itens)
	// e evita brechas de timeout no endpoint de enqueue.
	payloadBytes, totalItems, err := ReadAndValidateJSONArrayBody(c, maxItems)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if totalItems == 0 {
		utils.RespondWithValidationError(c, fmt.Errorf("array não pode ser vazio"))
		return
	}

	store := GetJobStore(c)
	if store == nil {
		return
	}

	j, err := store.Enqueue(jobType, userID, userType, json.RawMessage(payloadBytes), totalItems)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	publishAndReturnEnqueuedJob(c, j, userID)
}

func EnqueueAsyncBatchPayload(c *gin.Context, jobType jobs.JobType, payload interface{}, totalItems int) {
	if totalItems == 0 {
		utils.RespondWithValidationError(c, fmt.Errorf("array não pode ser vazio"))
		return
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	store := GetJobStore(c)
	if store == nil {
		return
	}

	userID, _ := middleware.GetUserID(c)
	userType, _ := middleware.GetUserType(c)
	j, err := store.Enqueue(jobType, userID, userType, json.RawMessage(payloadBytes), totalItems)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	publishAndReturnEnqueuedJob(c, j, userID)
}

func publishAndReturnEnqueuedJob(c *gin.Context, j *jobs.Job, userID uuid.UUID) {
	if w := GetJobWorker(c); w != nil {
		w.Enqueue(j)
	}
	if n := GetJobNotifier(c); n != nil {
		n.Publish(userID, jobs.Event{
			Type:       jobs.EventJobEnqueued,
			JobID:      j.ID,
			JobType:    j.Type,
			Status:     j.Status,
			Progress:   j.Progress(),
			DoneItems:  j.DoneItems,
			FailItems:  j.FailItems,
			TotalItems: j.TotalItems,
		})
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":     "job criado com sucesso. acompanhe o progresso pelo painel.",
		"job_id":      j.ID,
		"total_items": j.TotalItems,
		"status":      j.Status,
		"poll_url":    fmt.Sprintf("/jobs/%s", j.ID),
		"sse_url":     "/jobs/stream",
	})
}

func ReadAndValidateJSONArrayBody(c *gin.Context, maxItems int) ([]byte, int, error) {
	if c.Request == nil || c.Request.Body == nil {
		return nil, 0, fmt.Errorf("body deve ser um array JSON")
	}

	payloadBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("erro ao ler body")
	}
	if len(bytes.TrimSpace(payloadBytes)) == 0 {
		return nil, 0, fmt.Errorf("body deve ser um array JSON")
	}

	decoder := json.NewDecoder(bytes.NewReader(payloadBytes))
	tok, err := decoder.Token()
	if err != nil {
		return nil, 0, fmt.Errorf("body deve ser um array JSON")
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '[' {
		return nil, 0, fmt.Errorf("body deve ser um array JSON")
	}

	count := 0
	for decoder.More() {
		var item json.RawMessage
		if err := decoder.Decode(&item); err != nil {
			return nil, 0, fmt.Errorf("body deve ser um array JSON válido")
		}
		count++
		if count > maxItems {
			return nil, 0, fmt.Errorf("máximo de %d itens por batch assíncrono", maxItems)
		}
	}

	endTok, err := decoder.Token()
	if err != nil {
		return nil, 0, fmt.Errorf("body deve ser um array JSON")
	}
	endDelim, ok := endTok.(json.Delim)
	if !ok || endDelim != ']' {
		return nil, 0, fmt.Errorf("body deve ser um array JSON")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, 0, fmt.Errorf("body deve conter apenas um array JSON")
	}

	return payloadBytes, count, nil
}

type batchResponseWriter struct {
	*httptest.ResponseRecorder
	statusWritten bool
}

func newBatchResponseWriter() *batchResponseWriter {
	rec := httptest.NewRecorder()
	rec.Code = http.StatusOK
	return &batchResponseWriter{ResponseRecorder: rec}
}

func (w *batchResponseWriter) WriteHeader(code int) {
	if !w.statusWritten {
		w.Code = code
		w.statusWritten = true
	}
}

func (w *batchResponseWriter) WriteHeaderNow() {}

func (w *batchResponseWriter) Written() bool {
	return w.statusWritten
}

func (w *batchResponseWriter) Status() int {
	return w.Code
}

func (w *batchResponseWriter) Size() int {
	return w.Body.Len()
}

func (w *batchResponseWriter) Pusher() http.Pusher {
	return nil
}

// CloseNotify implementa gin.ResponseWriter — retorna canal que nunca fecha
// (contexto sintético de batch não tem conexão real).
func (w *batchResponseWriter) CloseNotify() <-chan bool {
	return make(chan bool)
}

// Hijack implementa http.Hijacker — necessário para satisfazer gin.ResponseWriter.
// Contextos sintéticos de batch não têm conexão real; retorna erro explícito
// em vez de panic caso seja chamado inesperadamente.
func (w *batchResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, fmt.Errorf("batch context: Hijack não suportado")
}

func NewFakeContext(parent *gin.Context) *gin.Context {
	w := newBatchResponseWriter()

	req := parent.Request.Clone(parent.Request.Context())
	req.Body = http.NoBody
	req.ContentLength = 0

	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("_batch_writer", w)

	for _, key := range []string{
		"user_id", "user_type", "admin_role",
		"dbClient", "repository", "projManager",
		"request_id",
	} {
		if v, exists := parent.Get(key); exists {
			c.Set(key, v)
		}
	}

	return c
}

func SetJSONBody(c *gin.Context, v interface{}) {
	b, _ := json.Marshal(v)
	c.Request.Body = io.NopCloser(bytes.NewReader(b))
	if c.Request.Header == nil {
		c.Request.Header = make(http.Header)
	}
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.ContentLength = int64(len(b))
}

func ExtractResult(c *gin.Context, index int) BatchItemResult {
	w, ok := c.Get("_batch_writer")
	if !ok {
		return BatchItemResult{
			Index:   index,
			Sucesso: false,
			Erro:    "erro interno: writer do contexto batch não encontrado",
		}
	}
	bw, ok := w.(*batchResponseWriter)
	if !ok {
		return BatchItemResult{
			Index:   index,
			Sucesso: false,
			Erro:    "erro interno: tipo de writer inválido no contexto batch",
		}
	}

	code := bw.Code
	body := bw.Body.Bytes()

	if code >= 200 && code < 300 {
		var dados interface{}
		if len(body) > 0 {
			_ = json.Unmarshal(body, &dados)
		}
		return BatchItemResult{Index: index, Sucesso: true, Dados: dados}
	}

	var errResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &errResp)
	}
	msg := errResp.Message
	if msg == "" {
		msg = errResp.Error
	}
	if msg == "" {
		msg = http.StatusText(code)
	}

	return BatchItemResult{Index: index, Sucesso: false, Erro: msg}
}

// =============================================================================
// Resposta padrão de batch
// =============================================================================

type BatchItemResult struct {
	Index   int         `json:"index"`
	Sucesso bool        `json:"sucesso"`
	Dados   interface{} `json:"dados,omitempty"`
	Erro    string      `json:"erro,omitempty"`
}

type BatchResponse struct {
	Total   int               `json:"total"`
	Sucesso int               `json:"sucesso"`
	Falhas  int               `json:"falhas"`
	Items   []BatchItemResult `json:"items"`
}

func NewBatchResponse(items []BatchItemResult) BatchResponse {
	s := 0
	for _, it := range items {
		if it.Sucesso {
			s++
		}
	}
	return BatchResponse{
		Total:   len(items),
		Sucesso: s,
		Falhas:  len(items) - s,
		Items:   items,
	}
}

func BatchHTTPStatus(results []BatchItemResult) int {
	falhas := 0
	for _, r := range results {
		if !r.Sucesso {
			falhas++
		}
	}
	switch {
	case falhas == 0:
		return http.StatusOK
	case falhas == len(results):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusMultiStatus
	}
}

func BatchErr(index int, err error) BatchItemResult {
	return BatchItemResult{Index: index, Sucesso: false, Erro: err.Error()}
}

// =============================================================================
// Helpers internos
// =============================================================================

func ValidarTamanhoBatch(n, max int) error {
	if n == 0 {
		return fmt.Errorf("array não pode ser vazio")
	}
	if n > max {
		return fmt.Errorf("máximo de %d itens por batch", max)
	}
	return nil
}

// runIDParamBatch executa fn para cada item com param "id" extraído do body.
// Body esperado: [{ "id": "uuid" }, ...]
func RunIDParamBatch(c *gin.Context, max int, fn func(*gin.Context)) {
	type ReqID struct {
		ID string `json:"id"`
	}
	var reqs []ReqID
	if err := c.ShouldBindJSON(&reqs); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve ser um array de {id}", nil)
		return
	}
	if err := ValidarTamanhoBatch(len(reqs), max); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, err.Error(), nil)
		return
	}

	results := make([]BatchItemResult, 0, len(reqs))
	for i, req := range reqs {
		rc := NewFakeContext(c)
		rc.Params = gin.Params{gin.Param{Key: "id", Value: req.ID}}
		fn(rc)
		results = append(results, ExtractResult(rc, i))
	}

	c.JSON(BatchHTTPStatus(results), NewBatchResponse(results))
}

// runCodigoParamBatch executa fn para cada item com param "codigo" extraído do body.
// Body esperado: [{ "codigo": "string" }, ...]
func RunCodigoParamBatch(c *gin.Context, max int, fn func(*gin.Context)) {
	type ReqCodigo struct {
		Codigo         string `json:"codigo"`
		CodigoAcademia string `json:"codigo_academia"`
	}
	var reqs []ReqCodigo
	if err := c.ShouldBindJSON(&reqs); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, "body deve ser um array de {codigo}", nil)
		return
	}
	if err := ValidarTamanhoBatch(len(reqs), max); err != nil {
		utils.RespondWithError(c, http.StatusBadRequest, err.Error(), nil)
		return
	}

	results := make([]BatchItemResult, 0, len(reqs))
	for i, req := range reqs {
		codigo := FirstNonEmptyTrimmed(req.Codigo, req.CodigoAcademia)
		if codigo == "" {
			results = append(results, BatchErr(i, fmt.Errorf("codigo é obrigatório")))
			continue
		}
		rc := NewFakeContext(c)
		rc.Params = gin.Params{gin.Param{Key: "codigo", Value: codigo}}
		fn(rc)
		results = append(results, ExtractResult(rc, i))
	}

	c.JSON(BatchHTTPStatus(results), NewBatchResponse(results))
}

func FirstNonEmptyTrimmed(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// getJobStore retorna o *jobs.Store injetado no contexto.
func GetJobStore(c *gin.Context) *jobs.Store {
	raw, exists := c.Get("jobStore")
	if !exists {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": "erro interno: job store não disponível",
		})
		return nil
	}
	s, ok := raw.(*jobs.Store)
	if !ok || s == nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": "erro interno: job store inválido",
		})
		return nil
	}
	return s
}

// getJobWorker retorna o *jobs.Worker injetado no contexto.
func GetJobWorker(c *gin.Context) *jobs.Worker {
	raw, exists := c.Get("jobWorker")
	if !exists {
		return nil
	}
	w, _ := raw.(*jobs.Worker)
	return w
}

func GetJobNotifier(c *gin.Context) *jobs.Notifier {
	raw, exists := c.Get("jobNotifier")
	if !exists {
		return nil
	}
	n, _ := raw.(*jobs.Notifier)
	return n
}

// ============================================================================
// Adapters para jobs assíncronos (1 item por execução)
//
// Objetivo: todo processamento batch/async reaproveita os handlers normais
// (mesma validação, mesmas regras de domínio e mesma resposta por item).
// ============================================================================

// restoreBody repõe o body original no request sem re-serializar.
//
// BUG ORIGINAL: a versão anterior usava setJSONBody(c, rawBody) que internamente
// chama json.Marshal(rawBody). Quando rawBody é []byte, json.Marshal produz uma
// string base64 em vez do JSON original — double-encoding que corrompe o payload
// para qualquer handler que chame ShouldBindJSON depois.
func restoreBody(c *gin.Context, raw []byte) {
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if c.Request.Header == nil {
		c.Request.Header = make(http.Header)
	}
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.ContentLength = int64(len(raw))
}

func BindJobItemWithoutLosingBody(c *gin.Context, target interface{}) error {
	if c.Request == nil || c.Request.Body == nil {
		return fmt.Errorf("body ausente")
	}

	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}

	if len(rawBody) == 0 {
		return fmt.Errorf("body ausente")
	}

	// Restaura ANTES de ShouldBindJSON — o reader precisa estar disponível.
	restoreBody(c, rawBody)

	if err := c.ShouldBindJSON(target); err != nil {
		return err
	}

	// ShouldBindJSON consome o stream; restaura novamente para o handler principal.
	restoreBody(c, rawBody)
	return nil
}
