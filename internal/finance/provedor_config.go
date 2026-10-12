package finance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// provedorNamespace gera o ID determinístico do agregado "provedor do
// contexto" (um agregado por contexto no ledger).
var provedorNamespace = uuid.MustParse("5b1d2f0e-3a9c-4c61-9d57-0f7a4c2e8b11")

func provedorAggregateID(contexto, academia string) uuid.UUID {
	return uuid.NewSHA1(provedorNamespace, []byte("provedor-pagamento:"+contexto+":"+academia))
}

// ProvedorDoContexto descreve, para um contexto, um provedor implementado.
type ProvedorDoContexto struct {
	InfoProvedor
	// Ativo: é o provedor usado para cobranças NOVAS neste contexto.
	Ativo bool `json:"ativo"`
	// CredenciaisConfiguradas: o contexto já informou as credenciais deste
	// provedor no ambiente atual.
	CredenciaisConfiguradas bool       `json:"credenciais_configuradas"`
	CredencialID            *uuid.UUID `json:"credencial_id,omitempty"`
	// CobrancasPendentes: cobranças deste provedor ainda em andamento neste
	// contexto. Enquanto houver, o provedor continua sendo consultado mesmo
	// que deixe de ser o ativo (mecanismo retroativo).
	CobrancasPendentes int `json:"cobrancas_pendentes"`
}

// ProvedorContextoView é o estado completo de provedores de um contexto.
type ProvedorContextoView struct {
	ContextoTipo   string               `json:"contexto_tipo"`
	CodigoAcademia string               `json:"codigo_academia,omitempty"`
	ProvedorAtivo  *string              `json:"provedor_ativo"`
	DefinidoEm     *time.Time           `json:"definido_em,omitempty"`
	Provedores     []ProvedorDoContexto `json:"provedores"`
}

// ConsultarProvedorContexto devolve o provedor ativo do contexto e, para cada
// provedor implementado, se tem credenciais e quantas cobranças pendentes.
func (s *Service) ConsultarProvedorContexto(ctx context.Context, contexto, academia string) (ProvedorContextoView, error) {
	if err := validContext(contexto, academia); err != nil {
		return ProvedorContextoView{}, err
	}
	view := ProvedorContextoView{ContextoTipo: contexto, CodigoAcademia: academia, Provedores: []ProvedorDoContexto{}}
	var ativo string
	var definidoEm time.Time
	err := s.client.DB().QueryRowContext(ctx, `SELECT provedor,definido_em FROM financeiro_provedores_pagamento WHERE contexto_tipo=$1 AND codigo_academia IS NOT DISTINCT FROM NULLIF($2,'')`, contexto, academia).Scan(&ativo, &definidoEm)
	switch {
	case err == nil:
		view.ProvedorAtivo = &ativo
		view.DefinidoEm = &definidoEm
	case !isNoRows(err):
		return ProvedorContextoView{}, err
	}
	for _, info := range s.Provedores() {
		item := ProvedorDoContexto{InfoProvedor: info, Ativo: view.ProvedorAtivo != nil && *view.ProvedorAtivo == info.ID}
		if id, err := s.findCredentialID(ctx, contexto, academia, AmbienteAtual(), info.ID); err == nil {
			item.CredenciaisConfiguradas = true
			item.CredencialID = &id
		}
		pendentes, err := s.CobrancasPendentes(ctx, contexto, academia, info.ID)
		if err != nil {
			return ProvedorContextoView{}, err
		}
		item.CobrancasPendentes = pendentes
		view.Provedores = append(view.Provedores, item)
	}
	return view, nil
}

// DefinirProvedorResultado é o resultado de DefinirProvedor.
type DefinirProvedorResultado struct {
	ProvedorContextoView
	// ProvedorAnterior é o provedor que era o ativo antes (vazio se nenhum).
	ProvedorAnterior string `json:"provedor_anterior,omitempty"`
	// CobrancasPendentesNoAnterior: cobranças ainda em andamento no provedor
	// anterior. Continuam sendo acompanhadas junto a ele até serem resolvidas.
	CobrancasPendentesNoAnterior int `json:"cobrancas_pendentes_no_anterior"`
}

// DefinirProvedor define o provedor de pagamento (único, sem padrão nem
// secundário) usado para as cobranças NOVAS do contexto. As credenciais do
// provedor escolhido têm de já estar configuradas. Trocar de provedor nunca
// toca nas cobranças existentes nem nas credenciais do provedor anterior: as
// cobranças em andamento continuam sendo consultadas e confirmadas junto ao
// provedor que as emitiu.
func (s *Service) DefinirProvedor(ctx context.Context, contexto, academia, provedor, userID, userType, ip string) (DefinirProvedorResultado, error) {
	if s.client == nil {
		return DefinirProvedorResultado{}, errors.New("serviço financeiro não inicializado")
	}
	contexto = strings.ToLower(strings.TrimSpace(contexto))
	if err := validContext(contexto, academia); err != nil {
		return DefinirProvedorResultado{}, err
	}
	prov, err := s.ProvedorPorID(provedor)
	if err != nil {
		return DefinirProvedorResultado{}, err
	}
	if _, err := s.loadCredentialDoProvedor(ctx, contexto, academia, prov.ID()); err != nil {
		if errors.Is(err, ErrNotFound) {
			return DefinirProvedorResultado{}, fmt.Errorf("%w: configure as credenciais do %s antes de o definir como provedor", ErrProvedorNaoConfigurado, prov.Nome())
		}
		return DefinirProvedorResultado{}, err
	}
	anterior, err := s.ProvedorAtivo(ctx, contexto, academia)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return DefinirProvedorResultado{}, err
	}
	resultado := DefinirProvedorResultado{ProvedorAnterior: anterior}
	if anterior != prov.ID() {
		payload := map[string]any{"contexto_tipo": contexto, "codigo_academia": academia, "provedor": prov.ID(), "provedor_anterior": anterior, "definido_por": userID}
		if err := s.record(ctx, provedorAggregateID(contexto, academia), "ProvedorPagamentoDefinido", payload, userID, userType, ip); err != nil {
			return DefinirProvedorResultado{}, err
		}
		if anterior != "" {
			if n, err := s.CobrancasPendentes(ctx, contexto, academia, anterior); err == nil {
				resultado.CobrancasPendentesNoAnterior = n
			}
		}
	}
	view, err := s.ConsultarProvedorContexto(ctx, contexto, academia)
	if err != nil {
		return DefinirProvedorResultado{}, err
	}
	resultado.ProvedorContextoView = view
	return resultado, nil
}

// validarMetodosDoProvedor garante que o contexto tem provedor ativo com
// credenciais e que todos os métodos pedidos são suportados por ele. É usada
// na configuração de mensalidade e matrícula das academias.
func (s *Service) validarMetodosDoProvedor(ctx context.Context, academia, objeto string, metodos []string) error {
	prov, _, err := s.loadCredentialAtiva(ctx, ContextoAcademia, academia)
	if err != nil {
		return fmt.Errorf("não é possível habilitar %s sem um provedor de pagamento configurado (com credenciais) na academia", objeto)
	}
	for _, m := range metodos {
		if !suportaMetodo(prov, m) {
			return fmt.Errorf("%w: %s não é suportado pelo provedor %s", ErrMetodoIndisponivel, m, prov.Nome())
		}
	}
	return nil
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
