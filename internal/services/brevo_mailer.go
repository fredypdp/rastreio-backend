// Envio de email via a API HTTP do Brevo (https://api.brevo.com/v3/smtp/email).
package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

var brevoHTTPClient = &http.Client{Timeout: 15 * time.Second}

// BrevoAPIURL é o endpoint de envio. É uma variável apenas para que os testes
// (inclusive os de handlers, noutro pacote) apontem para um servidor falso;
// em produção nunca é alterada.
var BrevoAPIURL = "https://api.brevo.com/v3/smtp/email"

// BrevoConfig agrupa a configuração de envio via API do Brevo.
type BrevoConfig struct {
	APIKey      string
	SenderEmail string // precisa estar verificado em app.brevo.com/settings/senders
	SenderName  string
}

// BrevoConfigurado diz se o canal de e-mail (Brevo) está utilizável. Os
// handlers usam-no para responder 503 em vez de fingir que enviaram.
func BrevoConfigurado() bool {
	_, ok := loadBrevoConfig()
	return ok
}

// loadBrevoConfig lê a configuração do ambiente: BREVO_API_KEY e o remetente
// verificado em EMAIL_FROM. Não existe mais fallback para EMAIL_USER (variável
// do SMTP, eliminado na Tarefa 123).
func loadBrevoConfig() (BrevoConfig, bool) {
	cfg := BrevoConfig{
		APIKey:      os.Getenv("BREVO_API_KEY"),
		SenderEmail: os.Getenv("EMAIL_FROM"),
		SenderName:  "Spuri",
	}
	if cfg.APIKey == "" || cfg.SenderEmail == "" {
		return cfg, false
	}
	return cfg, true
}

type brevoEmailAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type brevoSendRequest struct {
	Sender      brevoEmailAddress   `json:"sender"`
	To          []brevoEmailAddress `json:"to"`
	Subject     string              `json:"subject"`
	HTMLContent string              `json:"htmlContent"`
	TextContent string              `json:"textContent,omitempty"`
}

type brevoSendResponse struct {
	MessageID string `json:"messageId"`
}

type brevoErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// sendBrevoEmail envia um email via a API REST do Brevo.
func sendBrevoEmail(cfg BrevoConfig, toEmail, toName, subject, textBody, htmlBody string) error {
	return sendBrevoEmailToURL(cfg, BrevoAPIURL, toEmail, toName, subject, textBody, htmlBody)
}

// sendBrevoEmailToURL permite apontar para um servidor mock nos testes.
func sendBrevoEmailToURL(cfg BrevoConfig, url, toEmail, toName, subject, textBody, htmlBody string) error {
	reqBody := brevoSendRequest{
		Sender:      brevoEmailAddress{Email: cfg.SenderEmail, Name: cfg.SenderName},
		To:          []brevoEmailAddress{{Email: toEmail, Name: toName}},
		Subject:     subject,
		HTMLContent: htmlBody,
		TextContent: textBody,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("erro ao serializar email: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("erro ao montar requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("api-key", cfg.APIKey)

	resp, err := brevoHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("erro na requisição ao Brevo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var ok brevoSendResponse
		_ = json.NewDecoder(resp.Body).Decode(&ok)
		return nil
	}

	var apiErr brevoErrorResponse
	_ = json.NewDecoder(resp.Body).Decode(&apiErr)
	if apiErr.Message != "" {
		return fmt.Errorf("brevo respondeu %d: %s (%s)", resp.StatusCode, apiErr.Message, apiErr.Code)
	}
	return fmt.Errorf("brevo respondeu status %d", resp.StatusCode)
}

// LoadBrevoConfigForAsync carrega a configuração do Brevo sem depender de DB.
// Usado em goroutines assíncronas (ex: SEC-08 notificação de nova academia).
func LoadBrevoConfigForAsync() (BrevoConfig, bool) {
	return loadBrevoConfig()
}

// brevoSendRequestWithBCC estende brevoSendRequest com BCC.
type brevoSendRequestWithBCC struct {
	Sender      brevoEmailAddress   `json:"sender"`
	To          []brevoEmailAddress `json:"to"`
	Bcc         []brevoEmailAddress `json:"bcc,omitempty"`
	Subject     string              `json:"subject"`
	HTMLContent string              `json:"htmlContent"`
	TextContent string              `json:"textContent,omitempty"`
}

// SendBrevoEmailWithBCC envia email via Brevo com destinatários em BCC.
// toEmail/toName: destinatário principal (To)
// bccEmails: lista de emails em cópia oculta (BCC)
func SendBrevoEmailWithBCC(cfg BrevoConfig, toEmail, toName, subject, textBody, htmlBody string, bccEmails []string) error {
	if len(bccEmails) == 0 {
		return sendBrevoEmail(cfg, toEmail, toName, subject, textBody, htmlBody)
	}

	bcc := make([]brevoEmailAddress, 0, len(bccEmails))
	for _, e := range bccEmails {
		if e != "" {
			bcc = append(bcc, brevoEmailAddress{Email: e})
		}
	}

	reqBody := brevoSendRequestWithBCC{
		Sender:      brevoEmailAddress{Email: cfg.SenderEmail, Name: cfg.SenderName},
		To:          []brevoEmailAddress{{Email: toEmail, Name: toName}},
		Bcc:         bcc,
		Subject:     subject,
		HTMLContent: htmlBody,
		TextContent: textBody,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("erro ao serializar email: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, BrevoAPIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("erro ao montar requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("api-key", cfg.APIKey)

	resp, err := brevoHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("erro na requisição ao Brevo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var ok brevoSendResponse
		_ = json.NewDecoder(resp.Body).Decode(&ok)
		return nil
	}

	var apiErr brevoErrorResponse
	_ = json.NewDecoder(resp.Body).Decode(&apiErr)
	if apiErr.Message != "" {
		return fmt.Errorf("brevo respondeu %d: %s (%s)", resp.StatusCode, apiErr.Message, apiErr.Code)
	}
	return fmt.Errorf("brevo respondeu status %d", resp.StatusCode)
}
