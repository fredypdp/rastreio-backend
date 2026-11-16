// internal/services/email_service.go
package services

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// EmailService guarda tokens de autenticação (verificação de e-mail e
// recuperação de senha) e envia os e-mails transacionais.
//
// Tarefa 123 (D7): o Brevo é o ÚNICO canal de e-mail. EmailJS e SMTP foram
// eliminados. Sem Brevo configurado nenhum envio "finge" que correu bem:
// devolve-se ErrEmailNaoConfigurado (falha fechada) e nenhum segredo (token,
// link ou senha) é escrito no log (SEC-07).
type EmailService struct {
	db          *sqlx.DB
	frontendURL string
}

func NewEmailService(db *sqlx.DB) *EmailService {
	return &EmailService{
		db:          db,
		frontendURL: getEnvOrDefault("FRONTEND_URL", "http://localhost:3000"),
	}
}

// GenerateToken gera um token aleatório seguro de 32 bytes (64 chars hex).
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashToken calcula o SHA-256 do token (para armazenamento seguro).
// SEC-09: Armazenamos apenas o hash, nunca o token em claro.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// SaveToken persiste um token de autenticação (verificação ou recuperação).
//
// FIX SVC-01: prepared statement — sem interpolação de string.
// SEC-09: Armazena SHA-256(token) em vez do token em claro.
// SEC-09 (Tarefa 127): só existe UM token pendente por utilizador e tipo — os
// anteriores são invalidados na mesma transação que grava o novo, para que um
// e-mail antigo não continue a funcionar depois de um pedido mais recente.
func (s *EmailService) SaveToken(userID uuid.UUID, userType, tipo, email string, expiresIn time.Duration) (string, error) {
	token, err := GenerateToken()
	if err != nil {
		return "", fmt.Errorf("erro ao gerar token: %w", err)
	}

	expiresAt := time.Now().Add(expiresIn)
	tokenHash := hashToken(token)

	tx, err := s.db.Begin()
	if err != nil {
		return "", fmt.Errorf("erro ao salvar token: %w", err)
	}
	defer tx.Rollback()

	if _, err = tx.Exec(`
		UPDATE auth_tokens
		SET usado = TRUE, usado_em = CURRENT_TIMESTAMP
		WHERE user_id = $1 AND user_type = $2 AND tipo = $3 AND usado = FALSE
	`, userID, userType, tipo); err != nil {
		return "", fmt.Errorf("erro ao invalidar tokens anteriores: %w", err)
	}

	if _, err = tx.Exec(`
		INSERT INTO auth_tokens (token, user_id, user_type, tipo, email, expires_at, usado)
		VALUES ($1, $2, $3, $4, $5, $6, FALSE)
	`, tokenHash, userID, userType, tipo, email, expiresAt); err != nil {
		return "", fmt.Errorf("erro ao salvar token: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return "", fmt.Errorf("erro ao salvar token: %w", err)
	}

	return token, nil
}

// InvalidarTokensPendentes marca como usados todos os tokens ainda pendentes do
// utilizador para o tipo indicado. Chamado depois de um reset de senha concluído
// para que nenhum outro link de recuperação continue válido (SEC-03/SEC-09).
func (s *EmailService) InvalidarTokensPendentes(userID uuid.UUID, userType, tipo string) error {
	_, err := s.db.Exec(`
		UPDATE auth_tokens
		SET usado = TRUE, usado_em = CURRENT_TIMESTAMP
		WHERE user_id = $1 AND user_type = $2 AND tipo = $3 AND usado = FALSE
	`, userID, userType, tipo)
	if err != nil {
		return fmt.Errorf("erro ao invalidar tokens pendentes: %w", err)
	}
	return nil
}

// VerifyToken valida e consome um token de autenticação de forma atômica.
//
// FIX SVC-03: erro do UPDATE não é silenciado — token não reutilizável.
// SEC-09: Consumo atômico com UPDATE ... WHERE usado=FALSE (evita race condition).
//
//	Compara SHA-256(token) com o hash armazenado.
func (s *EmailService) VerifyToken(token, tipo string) (*TokenInfo, error) {
	tokenHash := hashToken(token)

	var info TokenInfo
	var idStr string

	// Atomic UPDATE: consome o token apenas se não usado e não expirado
	// Retorna os dados do token se a atualização afetar exatamente 1 linha
	err := s.db.QueryRow(`
		UPDATE auth_tokens
		SET usado = TRUE, usado_em = CURRENT_TIMESTAMP
		WHERE token = $1
		  AND tipo = $2
		  AND usado = FALSE
		  AND expires_at > CURRENT_TIMESTAMP
		RETURNING user_id, user_type, email, usado, expires_at
	`, tokenHash, tipo).Scan(&idStr, &info.UserType, &info.Email, &info.Usado, &info.ExpiresAt)

	if err == sql.ErrNoRows {
		// Token não encontrado, já usado, expirado ou tipo incorreto
		// Não distinguimos entre esses casos para não vazar informação
		return nil, fmt.Errorf("token inválido, expirado ou já utilizado")
	}
	if err != nil {
		return nil, fmt.Errorf("erro ao verificar token: %w", err)
	}

	info.UserID, _ = uuid.Parse(idStr)
	return &info, nil
}

// SendVerificationEmail envia o e-mail de verificação de endereço (Brevo).
//
// Falha fechada: sem Brevo configurado devolve ErrEmailNaoConfigurado e NÃO
// grava token nem escreve o link no log (o link contém o token).
func (s *EmailService) SendVerificationEmail(userID uuid.UUID, userType, email, nome string) error {
	if email == "" {
		return fmt.Errorf("email vazio")
	}

	cfg, ok := loadBrevoConfig()
	if !ok {
		log.Printf("[EMAIL-BREVO] ⚠️  Brevo não configurado — verificação de e-mail NÃO enviada para %s", email)
		return ErrEmailNaoConfigurado
	}

	token, err := s.SaveToken(userID, userType, "verificacao_email", email, 24*time.Hour)
	if err != nil {
		return fmt.Errorf("erro ao gerar token: %w", err)
	}

	verifyURL := fmt.Sprintf("%s/verificar-email/%s", s.frontendURL, token)
	subject := "Verificação de E-mail - Spuri"
	htmlBody := renderVerificationEmailHTML(nome, verifyURL, s.frontendURL)
	textBody := renderVerificationEmailText(nome, verifyURL)

	if err := sendBrevoEmail(cfg, email, nome, subject, textBody, htmlBody); err != nil {
		return fmt.Errorf("brevo: %w", err)
	}
	return nil
}

// SendPasswordResetLinkEmail (SEC-03) envia o link de recuperação de senha.
//
// A senha NÃO muda aqui nem viaja por e-mail: o titular abre o link (token de
// uso único, 1 hora), escolhe a nova senha e só então ela é gravada
// (POST /email/recuperar-senha/:token).
func (s *EmailService) SendPasswordResetLinkEmail(userID uuid.UUID, userType, email, nome string) error {
	if email == "" {
		return fmt.Errorf("email vazio")
	}
	cfg, ok := loadBrevoConfig()
	if !ok {
		return ErrEmailNaoConfigurado
	}
	token, err := s.SaveToken(userID, userType, "recuperacao_senha", email, 1*time.Hour)
	if err != nil {
		return fmt.Errorf("falha ao guardar token de recuperação: %w", err)
	}
	resetURL := fmt.Sprintf("%s/recuperar-senha/%s", s.frontendURL, token)
	subject := "Spuri - Redefinição de senha"
	if err := sendBrevoEmail(cfg, email, nome, subject,
		renderPasswordResetLinkEmailText(nome, resetURL),
		renderPasswordResetLinkEmailHTML(nome, resetURL, s.frontendURL)); err != nil {
		return fmt.Errorf("brevo: %w", err)
	}
	return nil
}

// SendAdminWelcomeEmail envia ao admin criado por outro admin as credenciais
// temporárias, via Brevo. É o único canal pelo qual a senha chega ao admin: a
// resposta HTTP da criação nunca a inclui.
//
// Falha de forma explícita se o Brevo não estiver configurado
// (ErrEmailNaoConfigurado): RegisterAdmin trata o erro com o aviso
// "email_nao_enviado". Única exceção, só para testes locais:
// CREDENCIAIS_LOG_DEV=true com ENV != production escreve as credenciais no log.
func (s *EmailService) SendAdminWelcomeEmail(email, nome, senhaTemporaria, role string) error {
	if email == "" {
		return fmt.Errorf("email vazio")
	}
	cfg, ok := loadBrevoConfig()
	if !ok {
		if LogCredenciaisDev(email, senhaTemporaria) {
			return nil
		}
		log.Printf("[EMAIL-BREVO] ⚠️  Brevo não configurado — credenciais NÃO enviadas ao admin %s (%s): canal indisponível", email, role)
		return ErrEmailNaoConfigurado
	}

	loginURL := fmt.Sprintf("%s/login", s.frontendURL)
	subject := "Bem-vindo ao Spuri - Acesso de Administrador"
	htmlBody := renderAdminWelcomeEmailHTML(nome, email, role, senhaTemporaria, loginURL, s.frontendURL)
	textBody := renderAdminWelcomeEmailText(nome, email, role, senhaTemporaria, loginURL)

	if err := sendBrevoEmail(cfg, email, nome, subject, textBody, htmlBody); err != nil {
		return fmt.Errorf("brevo: %w", err)
	}
	return nil
}

// AcademiaCadastradaInfo agrupa os dados (não sensíveis) da academia recém
// autocadastrada usados no email de aviso enviado aos administradores com
// permissão de ativação. Nenhum campo aqui inclui dados de acesso (senha,
// hash) — o admin apenas revisa e decide ativar ou não pelo painel.
type AcademiaCadastradaInfo struct {
	Nome           string
	CodigoAcademia string
	NIF            string
	Nivel          string // "escola" ou "superior"
	Type           string // "public" ou "private"
	Provincia      string // código de 3 letras, ex: "LDA"
}

// SendAcademiaCadastradaEmailBrevo avisa um administrador que uma nova
// academia se autocadastrou e está pendente de análise/ativação no painel.
//
// Envia via a API HTTP do Brevo, em vez de SMTP puro, reaproveitando o mesmo
// HTML do caminho SMTP. Falhas nunca devem bloquear o fluxo de cadastro.
func (s *EmailService) SendAcademiaCadastradaEmailBrevo(adminEmail, adminNome string, info AcademiaCadastradaInfo) error {
	cfg, ok := loadBrevoConfig()
	if !ok {
		log.Printf("[EMAIL-BREVO] ⚠️  BREVO_API_KEY/EMAIL_FROM não configurados — aviso de nova academia %s (%s) não enviado para admin %s",
			info.Nome, info.CodigoAcademia, adminEmail)
		return nil
	}
	if adminEmail == "" {
		return fmt.Errorf("email do admin vazio")
	}

	painelURL := fmt.Sprintf("%s/academias", s.frontendURL)
	subject := fmt.Sprintf("Nova instituição cadastrada: %s", info.Nome)
	textBody := fmt.Sprintf(
		"Olá %s!\n\nA instituição %s (código %s, NIF %s, %s, %s, província %s) concluiu o autocadastro no Spuri e está inativa, aguardando análise.\n\nAcesse o painel para revisar: %s\n",
		adminNome, info.Nome, info.CodigoAcademia, info.NIF, info.Type, info.Nivel, info.Provincia, painelURL,
	)
	htmlBody := renderAcademiaCadastradaHTML(adminNome, info, painelURL)
	if err := sendBrevoEmail(cfg, adminEmail, adminNome, subject, textBody, htmlBody); err != nil {
		return fmt.Errorf("brevo: %w", err)
	}
	return nil
}

// renderAcademiaCadastradaHTML monta e escapa o HTML do aviso de cadastro.
func renderAcademiaCadastradaHTML(adminNome string, info AcademiaCadastradaInfo, painelURL string) string {
	esc := html.EscapeString
	const tpl = `<!DOCTYPE html>
<html lang="pt-AO"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"></head>
<body style="margin:0;padding:0;background-color:#F9FAFB;font-family:'Segoe UI',Arial,sans-serif;">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0"><tr><td align="center" style="padding:40px 16px;"><table role="presentation" width="600" cellpadding="0" cellspacing="0" style="width:100%%;max-width:600px;background:#FFFFFF;border-radius:24px;border:1px solid #E4E7EC;"><tr><td style="border-radius:24px 24px 0 0;height:5px;background:#465FFF;"></td></tr><tr><td style="padding:30px 40px 22px;text-align:center;border-bottom:1px solid #F2F4F7;"><span style="font-size:18px;font-weight:700;color:#172741;">Spuri</span></td></tr><tr><td style="padding:40px;"><p style="margin:0 0 6px;font-size:12px;font-weight:700;letter-spacing:0.08em;text-transform:uppercase;color:#465FFF;">Novo cadastro pendente</p><h1 style="margin:0 0 16px;font-size:24px;font-weight:700;color:#172741;">Uma instituição acabou de se cadastrar</h1><p style="margin:0;font-size:15px;line-height:1.65;color:#344054;">Olá, <strong>%s</strong>! A instituição <strong>%s</strong> concluiu o autocadastro no Spuri e está com a conta <strong>inativa</strong>, aguardando a sua análise.</p><table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="margin-top:24px;border-radius:16px;background:#F9FAFB;border:1px solid #F2F4F7;"><tr><td style="padding:20px 22px;"><p style="margin:0 0 6px;font-size:13px;color:#344054;"><strong>Código:</strong> %s</p><p style="margin:0 0 6px;font-size:13px;color:#344054;"><strong>NIF:</strong> %s</p><p style="margin:0 0 6px;font-size:13px;color:#344054;"><strong>Tipo:</strong> %s</p><p style="margin:0 0 6px;font-size:13px;color:#344054;"><strong>Nível:</strong> %s</p><p style="margin:0;font-size:13px;color:#344054;"><strong>Província:</strong> %s</p></td></tr></table><div style="text-align:center;margin-top:28px;"><a href="%s" style="display:inline-block;padding:14px 34px;background:#465FFF;color:#FFFFFF;text-decoration:none;border-radius:12px;font-size:15px;font-weight:600;">Analisar no Painel</a></div></td></tr><tr><td style="background:#F9FAFB;padding:26px 40px;text-align:center;border-top:1px solid #F2F4F7;border-radius:0 0 24px 24px;"><p style="margin:0;font-size:13px;font-weight:600;color:#172741;">Spuri</p><p style="margin:4px 0 0;font-size:12px;color:#667085;">Confiança e eficiência na gestão académica.</p></td></tr></table></td></tr></table>
</body></html>`
	return fmt.Sprintf(tpl, esc(adminNome), esc(info.Nome), esc(info.CodigoAcademia), esc(info.NIF), esc(info.Type), esc(info.Nivel), esc(info.Provincia), painelURL)
}

func getEnvOrDefault(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// TokenInfo representa as informações de um token de autenticação.
type TokenInfo struct {
	UserID    uuid.UUID `db:"user_id"`
	UserType  string    `db:"user_type"`
	Email     string    `db:"email"`
	Usado     bool      `db:"usado"`
	ExpiresAt time.Time `db:"expires_at"`
}
