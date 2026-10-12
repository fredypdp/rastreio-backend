package finance

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CredentialInput é o pedido de configuração de credenciais de um provedor
// para um contexto (Spuri ou academia). Credenciais é indexado pelas chaves
// que o provedor declara em Campos() — o núcleo não conhece os campos.
type CredentialInput struct {
	ContextoTipo   string            `json:"contexto_tipo"`
	CodigoAcademia string            `json:"codigo_academia,omitempty"`
	Ambiente       string            `json:"ambiente"`
	Provedor       string            `json:"provedor"`
	Credenciais    map[string]string `json:"credenciais"`
}

// CredentialView é a visão pública (sem segredos) das credenciais de um
// provedor num contexto.
type CredentialView struct {
	ID             uuid.UUID `json:"id"`
	ContextoTipo   string    `json:"contexto_tipo"`
	CodigoAcademia string    `json:"codigo_academia,omitempty"`
	Ambiente       string    `json:"ambiente"`
	Provedor       string    `json:"provedor"`
	// CredenciaisMascaradas guarda, por chave de campo, o valor mascarado.
	CredenciaisMascaradas map[string]string `json:"credenciais_mascaradas"`
	// Ativo é true quando este provedor é o definido para o contexto.
	Ativo             bool      `json:"ativo"`
	WebhookHeaderName string    `json:"webhook_header_name,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// mask devolve a versão mascarada de um valor, deixando visíveis só os quatro
// últimos caracteres.
func mask(v string) string {
	if len(v) <= 4 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}

const (
	webhookSecretLength   = 15
	webhookSecretAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

// generateWebhookSecret gera um segredo alfanumérico de 15 caracteres usando
// crypto/rand (seguro criptograficamente) com math/big para evitar viés de
// módulo sobre o alfabeto.
func generateWebhookSecret() (string, error) {
	out := make([]byte, webhookSecretLength)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(webhookSecretAlphabet))))
		if err != nil {
			return "", fmt.Errorf("erro ao gerar segredo de webhook: %w", err)
		}
		out[i] = webhookSecretAlphabet[n.Int64()]
	}
	return string(out), nil
}

// hasWebhookSecret verifica, sem decifrar nada, se uma credencial já possui
// um segredo de webhook gravado no cofre — usado por ConfigureCredential
// para decidir se deve gerar um segredo novo (apenas na primeira vez que uma
// credencial recebe um).
func (s *Service) hasWebhookSecret(ctx context.Context, id uuid.UUID) (bool, error) {
	var exists bool
	err := s.client.DB().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM financeiro_segredos_provedor WHERE credential_id=$1 AND secret_type='webhook_secret')`, id).Scan(&exists)
	return exists, err
}

func validContext(c, academy string) error {
	if c == ContextoSpuri && academy == "" {
		return nil
	}
	if c == ContextoAcademia && strings.TrimSpace(academy) != "" {
		return nil
	}
	return errors.New("contexto financeiro inválido")
}

// ConfigureCredential grava (ou atualiza) as credenciais de um provedor para
// um contexto. Configurar credenciais NÃO muda o provedor ativo, exceto
// quando o contexto ainda não tinha nenhum provedor definido: nesse caso o
// provedor configurado passa a ser o ativo (regra aplicada pela projeção).
func (s *Service) ConfigureCredential(ctx context.Context, id *uuid.UUID, in CredentialInput, userID, userType, ip string) (CredentialView, string, error) {
	if s.client == nil {
		return CredentialView{}, "", errors.New("serviço financeiro não inicializado")
	}
	in.ContextoTipo = strings.ToLower(strings.TrimSpace(in.ContextoTipo))
	in.Ambiente = strings.ToLower(strings.TrimSpace(in.Ambiente))
	if err := validContext(in.ContextoTipo, in.CodigoAcademia); err != nil {
		return CredentialView{}, "", err
	}
	if in.Ambiente == "" {
		in.Ambiente = AmbienteAtual()
	}
	if in.Ambiente != AmbienteAtual() {
		return CredentialView{}, "", errors.New("credenciais devem pertencer ao ambiente ativo")
	}
	if strings.TrimSpace(userID) == "" {
		return CredentialView{}, "", errors.New("usuário não identificado")
	}
	prov, err := s.ProvedorPorID(in.Provedor)
	if err != nil {
		return CredentialView{}, "", err
	}
	valores, err := prov.ValidarCredenciais(in.Credenciais)
	if err != nil {
		return CredentialView{}, "", err
	}
	credentialID := uuid.New()
	if id != nil {
		credentialID = *id
		if err := s.credentialBelongsToScope(ctx, credentialID, in.ContextoTipo, in.CodigoAcademia, in.Ambiente, prov.ID()); err != nil {
			return CredentialView{}, "", err
		}
	} else if found, err := s.findCredentialID(ctx, in.ContextoTipo, in.CodigoAcademia, in.Ambiente, prov.ID()); err == nil {
		credentialID = found
	}
	secretsToSave := make(map[string]string, len(valores)+1)
	for k, v := range valores {
		secretsToSave[k] = v
	}
	var newWebhookSecret string
	if prov.UsaSegredoWebhook() {
		// O segredo de webhook só é gerado na primeira vez que esta credencial
		// recebe um — atualizações subsequentes nunca o tocam.
		hadSecret, err := s.hasWebhookSecret(ctx, credentialID)
		if err != nil {
			return CredentialView{}, "", err
		}
		if !hadSecret {
			newWebhookSecret, err = generateWebhookSecret()
			if err != nil {
				return CredentialView{}, "", err
			}
			secretsToSave["webhook_secret"] = newWebhookSecret
		}
	}
	view := CredentialView{ID: credentialID, ContextoTipo: in.ContextoTipo, CodigoAcademia: in.CodigoAcademia, Ambiente: in.Ambiente, Provedor: prov.ID(), CredenciaisMascaradas: prov.Mascarar(valores), UpdatedAt: time.Now().UTC()}
	if prov.UsaSegredoWebhook() {
		view.WebhookHeaderName = WebhookHeaderName
	}
	payload := map[string]any{"credential_id": credentialID.String(), "contexto_tipo": view.ContextoTipo, "codigo_academia": view.CodigoAcademia, "ambiente": view.Ambiente, "provedor": view.Provedor, "credenciais_mascaradas": view.CredenciaisMascaradas, "updated_at": view.UpdatedAt}
	if err := s.record(ctx, credentialID, "CredenciaisProvedorConfiguradas", payload, userID, userType, ip); err != nil {
		return CredentialView{}, "", err
	}
	if err := s.saveSecrets(ctx, credentialID, secretsToSave); err != nil {
		return CredentialView{}, "", err
	}
	if ativo, err := s.ProvedorAtivo(ctx, in.ContextoTipo, in.CodigoAcademia); err == nil && ativo == view.Provedor {
		view.Ativo = true
	}
	return view, newWebhookSecret, nil
}

// legacyMaskKeys liga as chaves do payload de credenciais gravado antes da
// Tarefa 119 (campos próprios da AppyPay) às chaves neutras de campo.
var legacyMaskKeys = map[string]string{
	"client_id_mask":          "client_id",
	"gpo_payment_method_mask": "gpo_method",
	"ref_payment_method_mask": "ref_method",
}

// ListCredentials lista as credenciais de todos os provedores do contexto.
func (s *Service) ListCredentials(ctx context.Context, contexto, academia string) ([]CredentialView, error) {
	q := `SELECT c.id,c.contexto_tipo,COALESCE(c.codigo_academia,''),c.ambiente,c.provedor,c.payload,c.updated_at,
		(p.provedor IS NOT NULL) AS ativo
		FROM financeiro_credenciais_provedor c
		LEFT JOIN financeiro_provedores_pagamento p ON p.contexto_tipo=c.contexto_tipo AND COALESCE(p.codigo_academia,'')=COALESCE(c.codigo_academia,'') AND p.provedor=c.provedor`
	args := []any{}
	where := []string{}
	if contexto != "" {
		where = append(where, "c.contexto_tipo=$1")
		args = append(args, contexto)
	}
	if academia != "" {
		where = append(where, fmt.Sprintf("c.codigo_academia=$%d", len(args)+1))
		args = append(args, academia)
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY c.updated_at DESC"
	rows, err := s.client.DB().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CredentialView{}
	for rows.Next() {
		var v CredentialView
		var raw []byte
		if err := rows.Scan(&v.ID, &v.ContextoTipo, &v.CodigoAcademia, &v.Ambiente, &v.Provedor, &raw, &v.UpdatedAt, &v.Ativo); err != nil {
			return nil, err
		}
		var meta map[string]any
		_ = json.Unmarshal(raw, &meta)
		v.CredenciaisMascaradas = map[string]string{}
		if masked, ok := meta["credenciais_mascaradas"].(map[string]any); ok {
			for k, val := range masked {
				if str, ok := val.(string); ok {
					v.CredenciaisMascaradas[k] = str
				}
			}
		} else {
			// Evento anterior à Tarefa 119: máscaras nos campos da AppyPay.
			for legacy, chave := range legacyMaskKeys {
				if str, ok := meta[legacy].(string); ok {
					v.CredenciaisMascaradas[chave] = str
				}
			}
		}
		if prov, ok := s.provedores[v.Provedor]; ok && prov.UsaSegredoWebhook() {
			v.WebhookHeaderName = WebhookHeaderName
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// CobrancasPendentes conta as cobranças ainda em andamento (não terminais)
// que um provedor tem num contexto. É a base do mecanismo retroativo: enquanto
// existirem, o provedor continua sendo consultado e as suas credenciais não
// podem ser removidas.
func (s *Service) CobrancasPendentes(ctx context.Context, contexto, academia, provedor string) (int, error) {
	var n int
	err := s.client.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM financeiro_cobrancas
		WHERE provedor=$1 AND contexto_tipo=$2 AND codigo_academia IS NOT DISTINCT FROM NULLIF($3,'')
		AND lower(COALESCE(payload->>'status','')) NOT IN (`+chargeAbertaStatusExcluidos+`)`, provedor, contexto, academia).Scan(&n)
	return n, err
}

// RemoveCredential remove as credenciais de um provedor configuradas para um
// contexto (academia ou spuri) no ambiente ativo. A remoção é registrada
// como um novo evento imutável no ledger (CredenciaisProvedorRemovidas) — o
// evento CredenciaisProvedorConfiguradas original nunca é apagado ou
// reescrito, apenas deixa de ser o fato mais recente.
//
// Mecanismo retroativo: se o provedor ainda tem cobranças pendentes neste
// contexto a remoção é recusada com ErrCredenciaisEmUso — sem as credenciais
// não haveria como consultar, cancelar nem autenticar os webhooks dessas
// cobranças. Se o provedor removido era o ativo, o contexto fica sem provedor
// ativo (regra da projeção) e novas cobranças são bloqueadas exatamente como
// se nunca tivesse existido provedor. O cofre de segredos
// (financeiro_segredos_provedor) é limpo neste mesmo comando, já que essa
// tabela vive fora do replay do ledger (não é reconstruída a partir de
// eventos).
func (s *Service) RemoveCredential(ctx context.Context, contextoTipo, codigoAcademia, provedor, userID, userType, ip string) error {
	if s.client == nil {
		return errors.New("serviço financeiro não inicializado")
	}
	contextoTipo = strings.ToLower(strings.TrimSpace(contextoTipo))
	if err := validContext(contextoTipo, codigoAcademia); err != nil {
		return err
	}
	prov, err := s.ProvedorPorID(provedor)
	if err != nil {
		return err
	}
	ambiente := AmbienteAtual()
	credentialID, err := s.findCredentialID(ctx, contextoTipo, codigoAcademia, ambiente, prov.ID())
	if err != nil {
		return fmt.Errorf("%w: credenciais %s não configuradas para este contexto", ErrNotFound, prov.Nome())
	}
	pendentes, err := s.CobrancasPendentes(ctx, contextoTipo, codigoAcademia, prov.ID())
	if err != nil {
		return err
	}
	if pendentes > 0 {
		return fmt.Errorf("%w: %d cobrança(s) pendente(s) no %s; aguarde a resolução ou cancele-as antes de remover as credenciais", ErrCredenciaisEmUso, pendentes, prov.Nome())
	}
	payload := map[string]any{
		"credential_id":   credentialID.String(),
		"contexto_tipo":   contextoTipo,
		"codigo_academia": codigoAcademia,
		"ambiente":        ambiente,
		"provedor":        prov.ID(),
	}
	if err := s.record(ctx, credentialID, "CredenciaisProvedorRemovidas", payload, userID, userType, ip); err != nil {
		return err
	}
	if _, err := s.client.DB().ExecContext(ctx, `DELETE FROM financeiro_segredos_provedor WHERE credential_id=$1`, credentialID); err != nil {
		return err
	}
	return nil
}

// loadCredential carrega as credenciais do provedor ATIVO do contexto — é o
// que as cobranças NOVAS usam. Falha com ErrNotFound (wrapping
// ErrProvedorNaoConfigurado) quando o contexto não tem provedor ativo ou o
// provedor ativo não tem credenciais no ambiente atual.
func (s *Service) loadCredential(ctx context.Context, contexto, academia string) (credencialProvedor, error) {
	_, cred, err := s.loadCredentialAtiva(ctx, contexto, academia)
	return cred, err
}

func (s *Service) loadCredentialAtiva(ctx context.Context, contexto, academia string) (Provedor, credencialProvedor, error) {
	if err := validContext(contexto, academia); err != nil {
		return nil, credencialProvedor{}, err
	}
	ativo, err := s.ProvedorAtivo(ctx, contexto, academia)
	if err != nil {
		return nil, credencialProvedor{}, err
	}
	prov, err := s.ProvedorPorID(ativo)
	if err != nil {
		return nil, credencialProvedor{}, err
	}
	cred, err := s.loadCredentialDoProvedor(ctx, contexto, academia, prov.ID())
	if err != nil {
		return nil, credencialProvedor{}, err
	}
	return prov, cred, nil
}

// loadCredentialDoProvedor carrega as credenciais de um provedor específico —
// usado pelas operações sobre cobranças JÁ EMITIDAS (consulta, cancelamento,
// confirmação, webhook), que seguem sempre o provedor gravado na cobrança, não
// o provedor ativo (mecanismo retroativo).
func (s *Service) loadCredentialDoProvedor(ctx context.Context, contexto, academia, provedor string) (credencialProvedor, error) {
	if err := validContext(contexto, academia); err != nil {
		return credencialProvedor{}, err
	}
	var id uuid.UUID
	err := s.client.DB().QueryRowContext(ctx, `SELECT id FROM financeiro_credenciais_provedor WHERE contexto_tipo=$1 AND codigo_academia IS NOT DISTINCT FROM NULLIF($2,'') AND ambiente=$3 AND provedor=$4`, contexto, academia, AmbienteAtual(), provedor).Scan(&id)
	if err != nil {
		return credencialProvedor{}, fmt.Errorf("%w: credenciais do provedor %s não configuradas para este contexto", ErrNotFound, provedor)
	}
	values, err := s.loadSecrets(ctx, id)
	if err != nil {
		return credencialProvedor{}, err
	}
	if prov, ok := s.provedores[provedor]; ok {
		for _, campo := range prov.Campos() {
			if campo.Obrigatorio && values[campo.Chave] == "" {
				return credencialProvedor{}, fmt.Errorf("cofre de credenciais %s incompleto", prov.Nome())
			}
		}
	}
	return credencialProvedor{ID: id, Provedor: provedor, Valores: values}, nil
}

// ProvedorAtivo devolve o identificador do provedor definido para o contexto.
func (s *Service) ProvedorAtivo(ctx context.Context, contexto, academia string) (string, error) {
	if err := validContext(contexto, academia); err != nil {
		return "", err
	}
	var provedor string
	err := s.client.DB().QueryRowContext(ctx, `SELECT provedor FROM financeiro_provedores_pagamento WHERE contexto_tipo=$1 AND codigo_academia IS NOT DISTINCT FROM NULLIF($2,'')`, contexto, academia).Scan(&provedor)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: %w", ErrNotFound, ErrProvedorNaoConfigurado)
	}
	return provedor, err
}

// PossuiProvedorConfigurado informa se o contexto tem um provedor ativo com
// credenciais — condição para habilitar cobranças (propina, matrícula, serviço
// pago).
func (s *Service) PossuiProvedorConfigurado(ctx context.Context, contexto, academia string) (bool, error) {
	_, err := s.loadCredential(ctx, contexto, academia)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *Service) findCredentialID(ctx context.Context, contexto, academia, ambiente, provedor string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.client.DB().QueryRowContext(ctx, `SELECT id FROM financeiro_credenciais_provedor WHERE contexto_tipo=$1 AND codigo_academia IS NOT DISTINCT FROM NULLIF($2,'') AND ambiente=$3 AND provedor=$4`, contexto, academia, ambiente, provedor).Scan(&id)
	return id, err
}

func (s *Service) credentialBelongsToScope(ctx context.Context, id uuid.UUID, contexto, academia, ambiente, provedor string) error {
	var found uuid.UUID
	err := s.client.DB().QueryRowContext(ctx, `SELECT id FROM financeiro_credenciais_provedor WHERE id=$1 AND contexto_tipo=$2 AND codigo_academia IS NOT DISTINCT FROM NULLIF($3,'') AND ambiente=$4 AND provedor=$5`, id, contexto, academia, ambiente, provedor).Scan(&found)
	if err != nil {
		return fmt.Errorf("%w: credencial não encontrada no contexto", ErrNotFound)
	}
	return nil
}

// CredentialScope resolve o contexto (spuri/academia) e a academia dona de
// uma credencial pelo seu id — usado pelos handlers de segredo de webhook
// para reaplicar a mesma autorização (authorizeFinanceScope) das demais
// rotas de credenciais.
func (s *Service) CredentialScope(ctx context.Context, id uuid.UUID) (string, string, error) {
	var contexto, academia string
	if err := s.client.DB().QueryRowContext(ctx, `SELECT contexto_tipo, COALESCE(codigo_academia,'') FROM financeiro_credenciais_provedor WHERE id=$1`, id).Scan(&contexto, &academia); err != nil {
		return "", "", fmt.Errorf("%w: credencial não encontrada", ErrNotFound)
	}
	return contexto, academia, nil
}

// WebhookSecret devolve o segredo de webhook atual em texto plano.
func (s *Service) WebhookSecret(ctx context.Context, id uuid.UUID) (string, error) {
	secrets, err := s.loadSecrets(ctx, id)
	if err != nil {
		return "", err
	}
	secret := secrets["webhook_secret"]
	if secret == "" {
		return "", fmt.Errorf("%w: credencial sem segredo de webhook", ErrNotFound)
	}
	return secret, nil
}

// RotateWebhookSecret gera um novo segredo de webhook, substitui o anterior
// no cofre e grava um evento de auditoria no ledger. A rotação é imediata:
// o segredo anterior deixa de autenticar assim que esta função retorna sem
// erro.
func (s *Service) RotateWebhookSecret(ctx context.Context, id uuid.UUID, userID, userType, ip string) (string, error) {
	secret, err := generateWebhookSecret()
	if err != nil {
		return "", err
	}
	if err := s.record(ctx, id, "SegredoWebhookProvedorRotacionado", map[string]any{"credential_id": id.String()}, userID, userType, ip); err != nil {
		return "", err
	}
	if err := s.saveSecrets(ctx, id, map[string]string{"webhook_secret": secret}); err != nil {
		return "", err
	}
	return secret, nil
}

func (s *Service) saveSecrets(ctx context.Context, id uuid.UUID, plain map[string]string) error {
	for typ, value := range plain {
		if value == "" {
			continue
		}
		ciphertext, err := encrypt(value)
		if err != nil {
			return err
		}
		if _, err = s.client.DB().ExecContext(ctx, `INSERT INTO financeiro_segredos_provedor (credential_id,secret_type,ciphertext) VALUES ($1,$2,$3) ON CONFLICT (credential_id,secret_type) DO UPDATE SET ciphertext=EXCLUDED.ciphertext,created_at=CURRENT_TIMESTAMP`, id, typ, ciphertext); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) loadSecrets(ctx context.Context, id uuid.UUID) (map[string]string, error) {
	rows, err := s.client.DB().QueryContext(ctx, `SELECT secret_type,ciphertext FROM financeiro_segredos_provedor WHERE credential_id=$1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var typ, ciphertext string
		if err := rows.Scan(&typ, &ciphertext); err != nil {
			return nil, err
		}
		v, e := decrypt(ciphertext)
		if e != nil {
			return nil, e
		}
		out[typ] = v
	}
	return out, rows.Err()
}

// WebhookOwner identifica o contexto dono de uma notificação autenticada.
type WebhookOwner struct {
	CredentialID                 uuid.UUID
	Provedor                     string
	ContextoTipo, CodigoAcademia string
}

// AuthenticateWebhook autentica uma notificação de webhook de um provedor.
// Apenas as credenciais DESSE provedor são candidatas — um segredo de um
// provedor nunca autentica notificações de outro. O provedor (e não o
// contexto) decide como autenticar: segredo em cabeçalho, assinatura etc.
func (s *Service) AuthenticateWebhook(ctx context.Context, provedor string, headers http.Header) (WebhookOwner, error) {
	prov, err := s.ProvedorPorID(provedor)
	if err != nil {
		return WebhookOwner{}, err
	}
	rows, err := s.client.DB().QueryContext(ctx, `SELECT id,contexto_tipo,COALESCE(codigo_academia,'') FROM financeiro_credenciais_provedor WHERE provedor=$1`, prov.ID())
	if err != nil {
		return WebhookOwner{}, err
	}
	defer rows.Close()
	var owners []WebhookOwner
	for rows.Next() {
		var o WebhookOwner
		if err := rows.Scan(&o.CredentialID, &o.ContextoTipo, &o.CodigoAcademia); err != nil {
			return WebhookOwner{}, err
		}
		o.Provedor = prov.ID()
		owners = append(owners, o)
	}
	if err := rows.Err(); err != nil {
		return WebhookOwner{}, err
	}
	candidatos := make([]credencialProvedor, 0, len(owners))
	validos := make([]WebhookOwner, 0, len(owners))
	for _, o := range owners {
		secrets, err := s.loadSecrets(ctx, o.CredentialID)
		if err != nil {
			continue
		}
		candidatos = append(candidatos, credencialProvedor{ID: o.CredentialID, Provedor: prov.ID(), Valores: secrets})
		validos = append(validos, o)
	}
	if idx, ok := prov.AutenticarWebhook(headers, candidatos); ok && idx >= 0 && idx < len(validos) {
		return validos[idx], nil
	}
	return WebhookOwner{}, errors.New("webhook não autenticado")
}
