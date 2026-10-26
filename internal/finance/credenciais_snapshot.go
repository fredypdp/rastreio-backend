package finance

import (
	"context"
	"errors"
	"fmt"

	"spuri/internal/domain/aggregates"

	"github.com/google/uuid"
)

// Snapshot ao editar credenciais (Tarefa 121).
//
// Editar as credenciais de um provedor substitui os segredos no cofre. As
// cobranças pendentes foram emitidas com os segredos ANTERIORES e precisam
// deles para serem consultadas, canceladas e autenticadas até serem resolvidas.
// Por isso, quando a edição altera algum valor E existem cobranças pendentes do
// provedor no contexto, o estado anterior é arquivado como snapshot (mesma
// tabela e mesmas regras do snapshot por remoção): as pendentes passam a
// apontar para ele e o snapshot é descartado quando a última é resolvida.
//
// A credencial ativa mantém o mesmo ID; só ela recebe os segredos novos.

// resultadoSnapshotEdicao descreve o que a edição fez às cobranças pendentes.
type resultadoSnapshotEdicao struct {
	// SnapshotID é o ID do snapshot criado (uuid.Nil se nada foi arquivado).
	SnapshotID uuid.UUID
	// CobrancasPendentes é quantas pendentes o provedor tinha no contexto.
	CobrancasPendentes int
}

// valoresAlterados informa se algum dos novos valores difere do que está no
// cofre. Um valor novo que não existia antes também conta como alteração.
func valoresAlterados(antigos, novos map[string]string) bool {
	for k, v := range novos {
		if antigos[k] != v {
			return true
		}
	}
	return false
}

// pendentesDaCredencial conta as cobranças pendentes que dependem DA CREDENCIAL
// ATIVA: as emitidas com ela ou legadas sem vínculo. As que já apontam para um
// snapshot (de uma edição ou remoção anterior) não dependem dela. É esta
// contagem, e não o total do provedor, que decide se um novo snapshot tem a
// quem servir: sem ela, uma segunda edição criaria um snapshot órfão, com
// segredos no cofre e nenhuma cobrança para descartá-lo.
func (s *Service) pendentesDaCredencial(ctx context.Context, contexto, academia, provedor string, credentialID uuid.UUID) (int, error) {
	var n int
	err := s.client.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM financeiro_cobrancas
		WHERE provedor=$1 AND contexto_tipo=$2 AND codigo_academia IS NOT DISTINCT FROM NULLIF($3,'')
		AND (credencial_id=$4 OR credencial_id IS NULL)
		AND lower(COALESCE(payload->>'status','')) NOT IN (`+chargeAbertaStatusExcluidos+`)`, provedor, contexto, academia, credentialID).Scan(&n)
	return n, err
}

// snapshotAntesDeEditar arquiva as credenciais atuais quando necessário. Deve
// ser chamada ANTES de os segredos novos serem gravados. Ordem segura:
//  1. copia os segredos antigos para o ID do snapshot (cofre, fora do replay);
//  2. grava o evento que cria a linha arquivada e reaponta as pendentes;
//  3. só então o chamador sobrescreve os segredos da credencial ativa.
//
// Se o passo 2 falha, a cópia do passo 1 é desfeita e a edição é abortada:
// nunca se perde o acesso às credenciais antigas.
func (s *Service) snapshotAntesDeEditar(ctx context.Context, prov Provedor, credentialID uuid.UUID, in CredentialInput, novos map[string]string, userID, userType, ip string) (resultadoSnapshotEdicao, error) {
	pendentes, err := s.CobrancasPendentes(ctx, in.ContextoTipo, in.CodigoAcademia, prov.ID())
	if err != nil {
		return resultadoSnapshotEdicao{}, err
	}
	res := resultadoSnapshotEdicao{CobrancasPendentes: pendentes}
	if pendentes == 0 {
		return res, nil
	}
	dependem, err := s.pendentesDaCredencial(ctx, in.ContextoTipo, in.CodigoAcademia, prov.ID(), credentialID)
	if err != nil {
		return res, err
	}
	if dependem == 0 {
		return res, nil // as pendentes já usam um snapshot anterior
	}
	antigos, err := s.loadSecrets(ctx, credentialID)
	if err != nil {
		return res, fmt.Errorf("não foi possível preservar as credenciais anteriores para as cobranças pendentes: %w", err)
	}
	if len(antigos) == 0 || !valoresAlterados(antigos, novos) {
		return res, nil // nada a preservar ou nada mudou
	}

	snapshotID := uuid.New()
	if err := s.saveSecrets(ctx, snapshotID, antigos); err != nil {
		return res, err
	}
	payload := map[string]any{
		"credential_id_origem":   credentialID.String(),
		"contexto_tipo":          in.ContextoTipo,
		"codigo_academia":        in.CodigoAcademia,
		"ambiente":               in.Ambiente,
		"provedor":               prov.ID(),
		"credenciais_mascaradas": prov.Mascarar(antigos),
		"editada_por":            userID,
		"cobrancas_pendentes":    dependem,
	}
	if err := s.record(ctx, snapshotID, aggregates.CredenciaisProvedorSnapshotPorEdicao, payload, userID, userType, ip); err != nil {
		if _, delErr := s.client.DB().ExecContext(ctx, `DELETE FROM financeiro_segredos_provedor WHERE credential_id=$1`, snapshotID); delErr != nil {
			err = errors.Join(err, delErr)
		}
		return res, err
	}
	s.reconc.reg.RepontarCredencial(in.ContextoTipo, in.CodigoAcademia, prov.ID(), credentialID, snapshotID)
	res.SnapshotID = snapshotID
	return res, nil
}
