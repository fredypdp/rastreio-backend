package services

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"spuri/internal/security"
)

// ============================================================================
// Verificação em duas etapas por e-mail (tarefa 122)
// ----------------------------------------------------------------------------
// Cada envio de código cria um DESAFIO (tabela dois_fatores_desafios) com id
// próprio. O código de 6 dígitos nunca é chave nem é guardado em texto puro:
// guarda-se um HMAC-SHA256 (chave derivada de JWT_SECRET) amarrado ao id do
// desafio, ao usuário e à finalidade. Assim:
//
//   - dois códigos iguais por coincidência são desafios distintos (id e data
//     de criação diferentes) e um não interfere no outro;
//   - reutilização é impedida pelo ESTADO do desafio (usado_em), não pelo
//     valor do código;
//   - pedir um código novo invalida o anterior (invalidado_em / 'substituido');
//   - o código vale 10 minutos e admite no máximo 5 tentativas erradas;
//   - há intervalo mínimo entre envios e teto de envios por hora, porque o
//     rate limit HTTP do login está desativado neste projeto.
// ============================================================================

const (
	DoisFatoresFinalidadeAtivacao    = "ativacao"
	DoisFatoresFinalidadeDesativacao = "desativacao"
	DoisFatoresFinalidadeLogin       = "login"

	// DoisFatoresValidadeCodigo é o tempo de vida de um código.
	DoisFatoresValidadeCodigo = 10 * time.Minute
	// DoisFatoresMaxTentativas é o nº de códigos errados aceitos por desafio.
	DoisFatoresMaxTentativas = 5
	// DoisFatoresIntervaloReenvio é o intervalo mínimo entre dois envios
	// quando o desafio anterior ainda está vivo.
	DoisFatoresIntervaloReenvio = 60 * time.Second
	// DoisFatoresMaxEnviosPorHora limita os envios NÃO concluídos (códigos que
	// não chegaram a ser usados) por usuário e finalidade, na última hora.
	DoisFatoresMaxEnviosPorHora = 5
	// DoisFatoresJanelaLogin é o tempo, desde a senha correta, em que o login
	// pode pedir reenvio do código sem digitar a senha de novo.
	DoisFatoresJanelaLogin = 30 * time.Minute

	doisFatoresKeyLabel = "dois-fatores-email-v1"

	motivoSubstituido         = "substituido"
	motivoTentativasExcedidas = "tentativas_excedidas"
)

var (
	// ErrDoisFatoresDesafioInvalido — desafio inexistente, de outro usuário ou
	// com finalidade diferente (resposta única para não revelar qual).
	ErrDoisFatoresDesafioInvalido = errors.New("código inválido. Solicite um novo código")
	// ErrDoisFatoresCodigoUsado — o desafio já foi concluído.
	ErrDoisFatoresCodigoUsado = errors.New("este código já foi utilizado. Solicite um novo código")
	// ErrDoisFatoresCodigoSubstituido — um código mais recente foi enviado.
	ErrDoisFatoresCodigoSubstituido = errors.New("um código mais recente foi enviado para o seu e-mail. Use o código mais recente")
	// ErrDoisFatoresCodigoExpirado — passou a validade do código.
	ErrDoisFatoresCodigoExpirado = errors.New("o código expirou. Solicite um novo código")
	// ErrDoisFatoresTentativasExcedidas — limite de tentativas errado atingido.
	ErrDoisFatoresTentativasExcedidas = errors.New("número máximo de tentativas atingido. Solicite um novo código")
	// ErrDoisFatoresFormatoCodigo — o código não tem 6 dígitos (não consome tentativa).
	ErrDoisFatoresFormatoCodigo = errors.New("o código deve ter 6 dígitos numéricos")
	// ErrDoisFatoresSessaoExpirada — reenvio fora da janela do login.
	ErrDoisFatoresSessaoExpirada = errors.New("a sessão de login expirou. Entre novamente com a sua senha")
	// ErrDoisFatoresEnvioFalhou — o e-mail não pôde ser enviado.
	ErrDoisFatoresEnvioFalhou = errors.New("não foi possível enviar o código por e-mail. Tente novamente em instantes")
)

// ErrDoisFatoresCodigoIncorreto — código errado; Restantes é o nº de tentativas
// que ainda sobram neste desafio.
type ErrDoisFatoresCodigoIncorreto struct{ Restantes int }

func (e *ErrDoisFatoresCodigoIncorreto) Error() string {
	if e.Restantes <= 0 {
		return "código incorreto. Número máximo de tentativas atingido; solicite um novo código"
	}
	if e.Restantes == 1 {
		return "código incorreto. Resta 1 tentativa"
	}
	return fmt.Sprintf("código incorreto. Restam %d tentativas", e.Restantes)
}

// ErrDoisFatoresAguardar — um código acabou de ser enviado e ainda está vivo.
// DesafioID é o desafio vivo (uuid.Nil se não houver).
type ErrDoisFatoresAguardar struct {
	Segundos         int
	DesafioID        uuid.UUID
	ExpiraEmSegundos int
}

func (e *ErrDoisFatoresAguardar) Error() string {
	return fmt.Sprintf("um código acabou de ser enviado. Aguarde %d segundos para solicitar outro", e.Segundos)
}

// ErrDoisFatoresLimiteEnvios — teto de envios por hora atingido.
type ErrDoisFatoresLimiteEnvios struct{ Segundos int }

func (e *ErrDoisFatoresLimiteEnvios) Error() string {
	minutos := int(math.Ceil(float64(e.Segundos) / 60))
	if minutos < 1 {
		minutos = 1
	}
	return fmt.Sprintf("muitos códigos solicitados. Tente novamente em %d minuto(s)", minutos)
}

// DoisFatoresSender envia o código ao usuário (implementado por EmailService).
type DoisFatoresSender interface {
	EnviarCodigoDoisFatores(destinatario, nome, finalidade, codigo string, validadeMinutos int) error
}

// DoisFatoresDesafio descreve um desafio criado ou verificado.
type DoisFatoresDesafio struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	UserType          string
	Finalidade        string
	EmailDestino      string
	IniciadoEm        time.Time
	ExpiraEmSegundos  int
	ReenvioEmSegundos int
}

// UsuarioDoisFatores identifica o dono esperado de um desafio.
type UsuarioDoisFatores struct {
	ID   uuid.UUID
	Tipo string
}

// DoisFatoresService implementa o ciclo de vida dos desafios.
type DoisFatoresService struct {
	db     *sqlx.DB
	sender DoisFatoresSender
}

func NewDoisFatoresService(db *sqlx.DB, sender DoisFatoresSender) *DoisFatoresService {
	return &DoisFatoresService{db: db, sender: sender}
}

var regexCodigoSeisDigitos = regexp.MustCompile(`^[0-9]{6}$`)

func finalidadeValida(f string) bool {
	return f == DoisFatoresFinalidadeAtivacao || f == DoisFatoresFinalidadeDesativacao || f == DoisFatoresFinalidadeLogin
}

func tipoUsuarioValido(t string) bool {
	return t == "admin" || t == "academia" || t == "estudante"
}

// gerarCodigoNumerico devolve um código uniforme entre 000000 e 999999 usando
// crypto/rand (sem viés de módulo).
func gerarCodigoNumerico() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("erro ao gerar código: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// hashCodigo calcula HMAC-SHA256(chave derivada, id|usuário|tipo|finalidade|código).
// Amarrar o id do desafio faz com que o MESMO código em dois desafios gere
// hashes diferentes.
func hashCodigoDoisFatores(id, userID uuid.UUID, userType, finalidade, codigo string) (string, error) {
	key, err := security.DeriveKey(doisFatoresKeyLabel)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	fmt.Fprintf(mac, "%s|%s|%s|%s|%s", id, userID, userType, finalidade, codigo)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// Criar cria um desafio novo, invalida o anterior da mesma finalidade e envia o
// código por e-mail.
//
// iniciadoEm só é informado em reenvios de login: preserva o instante da senha
// correta original, para que reenvios sucessivos não estendam a janela
// (DoisFatoresJanelaLogin) para sempre.
func (s *DoisFatoresService) Criar(ctx context.Context, userID uuid.UUID, userType, finalidade, email, nome string, iniciadoEm *time.Time) (*DoisFatoresDesafio, error) {
	if !tipoUsuarioValido(userType) || !finalidadeValida(finalidade) {
		return nil, fmt.Errorf("tipo de usuário ou finalidade inválidos")
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, fmt.Errorf("e-mail de destino vazio")
	}

	codigo, err := gerarCodigoNumerico()
	if err != nil {
		return nil, err
	}
	id := uuid.New()
	hash, err := hashCodigoDoisFatores(id, userID, userType, finalidade, codigo)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Serializa criações concorrentes do mesmo usuário e finalidade, para que
	// limite, intervalo e invalidação do anterior sejam avaliados em sequência.
	chave := fmt.Sprintf("2fa|%s|%s|%s", userType, userID, finalidade)
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, chave); err != nil {
		return nil, err
	}

	// Limpeza oportunista (limitada ao próprio usuário): desafios com mais de 7 dias.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM dois_fatores_desafios
		  WHERE user_id = $1 AND user_type = $2 AND created_at < NOW() - INTERVAL '7 days'`,
		userID, userType); err != nil {
		return nil, err
	}

	// Intervalo mínimo: só vale enquanto o desafio anterior ainda está vivo.
	var (
		ultimoID    uuid.UUID
		desdeSeg    float64
		restaSeg    float64
		ultimoVivo  bool
		temAnterior = true
	)
	err = tx.QueryRowContext(ctx,
		`SELECT id,
		        EXTRACT(EPOCH FROM (NOW() - created_at))::float8,
		        EXTRACT(EPOCH FROM (expira_em - NOW()))::float8,
		        (usado_em IS NULL AND invalidado_em IS NULL)
		   FROM dois_fatores_desafios
		  WHERE user_id = $1 AND user_type = $2 AND finalidade = $3
		  ORDER BY created_at DESC LIMIT 1`,
		userID, userType, finalidade).Scan(&ultimoID, &desdeSeg, &restaSeg, &ultimoVivo)
	if err == sql.ErrNoRows {
		temAnterior = false
	} else if err != nil {
		return nil, err
	}
	if temAnterior && ultimoVivo && desdeSeg < DoisFatoresIntervaloReenvio.Seconds() {
		espera := int(math.Ceil(DoisFatoresIntervaloReenvio.Seconds() - desdeSeg))
		if espera < 1 {
			espera = 1
		}
		ag := &ErrDoisFatoresAguardar{Segundos: espera}
		if restaSeg > 0 {
			ag.DesafioID = ultimoID
			ag.ExpiraEmSegundos = int(math.Ceil(restaSeg))
		}
		return nil, ag
	}

	// Teto de envios por hora: só conta códigos que NÃO chegaram a ser usados
	// (um login bem-sucedido prova que o usuário é legítimo e não consome o teto).
	var (
		enviosHora int
		liberaEm   float64
	)
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COALESCE(EXTRACT(EPOCH FROM (MIN(created_at) + INTERVAL '1 hour' - NOW())), 0)::float8
		   FROM dois_fatores_desafios
		  WHERE user_id = $1 AND user_type = $2 AND finalidade = $3
		    AND usado_em IS NULL AND created_at > NOW() - INTERVAL '1 hour'`,
		userID, userType, finalidade).Scan(&enviosHora, &liberaEm); err != nil {
		return nil, err
	}
	if enviosHora >= DoisFatoresMaxEnviosPorHora {
		seg := int(math.Ceil(liberaEm))
		if seg < 1 {
			seg = 1
		}
		return nil, &ErrDoisFatoresLimiteEnvios{Segundos: seg}
	}

	// Invalida o desafio vivo anterior.
	if _, err := tx.ExecContext(ctx,
		`UPDATE dois_fatores_desafios
		    SET invalidado_em = NOW(), motivo_invalidacao = $4
		  WHERE user_id = $1 AND user_type = $2 AND finalidade = $3
		    AND usado_em IS NULL AND invalidado_em IS NULL`,
		userID, userType, finalidade, motivoSubstituido); err != nil {
		return nil, err
	}

	var inicio sql.NullTime
	if iniciadoEm != nil {
		inicio = sql.NullTime{Time: *iniciadoEm, Valid: true}
	}
	var iniciadoGravado time.Time
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO dois_fatores_desafios
		        (id, user_id, user_type, finalidade, codigo_hash, email_destino, iniciado_em, expira_em)
		 VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7, NOW()), NOW() + make_interval(secs => $8))
		 RETURNING iniciado_em`,
		id, userID, userType, finalidade, hash, email, inicio, DoisFatoresValidadeCodigo.Seconds(),
	).Scan(&iniciadoGravado); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Envio fora da transação (chamada de rede). Se falhar, o desafio é
	// removido: um código que o usuário nunca recebeu não deve consumir limite.
	if err := s.sender.EnviarCodigoDoisFatores(email, nome, finalidade, codigo, int(DoisFatoresValidadeCodigo.Minutes())); err != nil {
		bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, delErr := s.db.ExecContext(bg, `DELETE FROM dois_fatores_desafios WHERE id = $1`, id); delErr != nil {
			log.Printf("[2FA] erro ao remover desafio %s após falha de envio: %v", id, delErr)
		}
		log.Printf("[2FA] falha ao enviar código (%s/%s): %v", userType, finalidade, err)
		return nil, fmt.Errorf("%w", ErrDoisFatoresEnvioFalhou)
	}

	return &DoisFatoresDesafio{
		ID:                id,
		UserID:            userID,
		UserType:          userType,
		Finalidade:        finalidade,
		EmailDestino:      email,
		IniciadoEm:        iniciadoGravado,
		ExpiraEmSegundos:  int(DoisFatoresValidadeCodigo.Seconds()),
		ReenvioEmSegundos: int(DoisFatoresIntervaloReenvio.Seconds()),
	}, nil
}

// Verificar valida o código de um desafio e, se correto, o consome (usado_em).
//
// esperado (opcional) amarra o desafio ao usuário autenticado nos fluxos de
// ativação/desativação: um desafio de outro usuário responde como inexistente.
// No login esperado é nil — o dono vem do próprio desafio.
//
// Tentativas erradas são persistidas (commit) mesmo quando a verificação falha.
func (s *DoisFatoresService) Verificar(ctx context.Context, desafioID uuid.UUID, codigo, finalidade string, esperado *UsuarioDoisFatores) (*DoisFatoresDesafio, error) {
	codigo = strings.TrimSpace(codigo)
	if !regexCodigoSeisDigitos.MatchString(codigo) {
		return nil, ErrDoisFatoresFormatoCodigo
	}
	if !finalidadeValida(finalidade) {
		return nil, ErrDoisFatoresDesafioInvalido
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		d          DoisFatoresDesafio
		hashSalvo  string
		tentativas int
		usado      bool
		motivo     sql.NullString
		invalidado bool
		expirado   bool
	)
	err = tx.QueryRowContext(ctx,
		`SELECT id, user_id, user_type, finalidade, email_destino, codigo_hash, tentativas, iniciado_em,
		        (usado_em IS NOT NULL), (invalidado_em IS NOT NULL), motivo_invalidacao,
		        (expira_em <= NOW())
		   FROM dois_fatores_desafios
		  WHERE id = $1
		  FOR UPDATE`, desafioID,
	).Scan(&d.ID, &d.UserID, &d.UserType, &d.Finalidade, &d.EmailDestino, &hashSalvo, &tentativas, &d.IniciadoEm,
		&usado, &invalidado, &motivo, &expirado)
	if err == sql.ErrNoRows {
		return nil, ErrDoisFatoresDesafioInvalido
	}
	if err != nil {
		return nil, err
	}

	if d.Finalidade != finalidade {
		return nil, ErrDoisFatoresDesafioInvalido
	}
	if esperado != nil && (d.UserID != esperado.ID || d.UserType != esperado.Tipo) {
		return nil, ErrDoisFatoresDesafioInvalido
	}
	if usado {
		return nil, ErrDoisFatoresCodigoUsado
	}
	if invalidado {
		switch motivo.String {
		case motivoSubstituido:
			return nil, ErrDoisFatoresCodigoSubstituido
		case motivoTentativasExcedidas:
			return nil, ErrDoisFatoresTentativasExcedidas
		default:
			return nil, ErrDoisFatoresDesafioInvalido
		}
	}
	if expirado {
		return nil, ErrDoisFatoresCodigoExpirado
	}
	if tentativas >= DoisFatoresMaxTentativas {
		return nil, ErrDoisFatoresTentativasExcedidas
	}

	esperadoHash, err := hashCodigoDoisFatores(d.ID, d.UserID, d.UserType, d.Finalidade, codigo)
	if err != nil {
		return nil, err
	}

	if subtle.ConstantTimeCompare([]byte(esperadoHash), []byte(strings.TrimSpace(hashSalvo))) != 1 {
		novas := tentativas + 1
		if novas >= DoisFatoresMaxTentativas {
			_, err = tx.ExecContext(ctx,
				`UPDATE dois_fatores_desafios
				    SET tentativas = $2, invalidado_em = NOW(), motivo_invalidacao = $3
				  WHERE id = $1`, d.ID, novas, motivoTentativasExcedidas)
		} else {
			_, err = tx.ExecContext(ctx,
				`UPDATE dois_fatores_desafios SET tentativas = $2 WHERE id = $1`, d.ID, novas)
		}
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, &ErrDoisFatoresCodigoIncorreto{Restantes: DoisFatoresMaxTentativas - novas}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE dois_fatores_desafios SET usado_em = NOW() WHERE id = $1`, d.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &d, nil
}

// ObterParaReenvio devolve o desafio de login que pode originar um reenvio:
// não usado e ainda dentro da janela desde a senha correta.
func (s *DoisFatoresService) ObterParaReenvio(ctx context.Context, desafioID uuid.UUID) (*DoisFatoresDesafio, error) {
	var (
		d       DoisFatoresDesafio
		usado   bool
		dentro  bool
		existiu = true
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, user_type, finalidade, email_destino, iniciado_em,
		        (usado_em IS NOT NULL),
		        (iniciado_em > NOW() - make_interval(secs => $2))
		   FROM dois_fatores_desafios
		  WHERE id = $1`, desafioID, DoisFatoresJanelaLogin.Seconds(),
	).Scan(&d.ID, &d.UserID, &d.UserType, &d.Finalidade, &d.EmailDestino, &d.IniciadoEm, &usado, &dentro)
	if err == sql.ErrNoRows {
		existiu = false
	} else if err != nil {
		return nil, err
	}
	if !existiu || d.Finalidade != DoisFatoresFinalidadeLogin {
		return nil, ErrDoisFatoresDesafioInvalido
	}
	if usado {
		return nil, ErrDoisFatoresCodigoUsado
	}
	if !dentro {
		return nil, ErrDoisFatoresSessaoExpirada
	}
	return &d, nil
}
