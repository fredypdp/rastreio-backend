package cadastros

// Catálogo fixo de serviços (Parte 7 do Plano de Atualizações 1): Transporte e
// Atividade extracurricular. As regras deste arquivo são funções puras sobre
// structs simples, sem banco; a persistência (CRUD com auditoria na mesma
// transação) está em servicos_repo.go.

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"

	"spuri/internal/utils"
)

const (
	TipoServicoTransporte = "transporte"
	TipoServicoAtividade  = "atividade"

	// Modos de preço (e de prazo no geral do Transporte).
	ModoNenhum   = "nenhum"   // não definido neste nível (a rota define)
	ModoGratuito = "gratuito" // definido como gratuito: não gera cobrança
	ModoValor    = "valor"    // preço definido
	ModoMeses    = "meses"    // prazo definido por uma lista de meses

	EntidadeServicoTransporte = "ServicoTransporte"
	EntidadeTransporteRota    = "TransporteRota"
	EntidadeTransporteVeiculo = "TransporteVeiculo"
	EntidadeServicoAtividade  = "ServicoAtividade"
	EntidadeAtividadeTurma    = "AtividadeTurma"

	// Maior valor que cabe em NUMERIC(14,2).
	MaxValorServico = 999999999999.99
)

// ErroConflito indica um pedido válido em si mas incompatível com o estado
// atual (a API responde 409): por exemplo, definir o preço geral do Transporte
// enquanto uma rota tem preço próprio.
type ErroConflito struct{ Err error }

func (e *ErroConflito) Error() string { return e.Err.Error() }
func (e *ErroConflito) Unwrap() error { return e.Err }

func conflito(format string, args ...any) error {
	return &ErroConflito{Err: fmt.Errorf(format, args...)}
}

// EhErroConflito devolve o conflito contido em err, se houver.
func EhErroConflito(err error) (*ErroConflito, bool) {
	var c *ErroConflito
	if errors.As(err, &c) {
		return c, true
	}
	return nil, false
}

// ============================================================================
// Estruturas
// ============================================================================

// Disponibilidade restringe quem pode pedir o serviço. As duas listas vazias
// querem dizer "todos os estudantes da academia".
type Disponibilidade struct {
	AnosAcademicos []string // só ensino fundamental (sem curso)
	Cursos         []string // "<curso_id>|<ano_academico>" (médio e superior)
}

// Taxa de inscrição: opcional. Valor 0 = sem taxa.
type TaxaInscricao struct {
	Valor   float64
	Metodos []string
}

type Transporte struct {
	ID             uuid.UUID
	CodigoAcademia string
	Ativo          bool
	PrecoModo      string // nenhum | gratuito | valor
	PrecoValor     float64
	PrazoModo      string // nenhum | gratuito | meses
	PrazoMeses     []int
	Taxa           TaxaInscricao
	Metodos        []string
	Disponibilidade
	Rotas     []Rota
	Version   int
	CreatedAt string
	UpdatedAt string
}

type Rota struct {
	ID              uuid.UUID
	TransporteID    uuid.UUID
	Nome            string
	HorarioEmbarque string // hh:mm
	PrecoModo       string // nenhum | gratuito | valor
	PrecoValor      float64
	PrazoMeses      []int
	Ativo           bool
	Veiculos        []Veiculo
	Version         int
}

type Veiculo struct {
	ID      uuid.UUID
	RotaID  uuid.UUID
	Nome    string
	Ativo   bool
	Version int
}

type Horario struct {
	DiaSemana  int    // 1 (segunda) a 7 (domingo)
	HoraInicio string // hh:mm
	HoraFim    string // hh:mm
}

type Atividade struct {
	ID             uuid.UUID
	CodigoAcademia string
	Nome           string
	Instrutor      string
	Coletiva       bool
	Gratuito       bool
	Preco          float64
	PrazoMeses     []int
	Taxa           TaxaInscricao
	Metodos        []string
	Disponibilidade
	Ativo    bool
	Horarios []Horario
	Turmas   []TurmaAtividade
	Version  int
}

type TurmaAtividade struct {
	ID          uuid.UUID
	AtividadeID uuid.UUID
	Nome        string
	Ativo       bool
	Membros     int
	Version     int
}

// ServicoResolvido é o que a cobrança e a solicitação precisam saber de um
// serviço: já com a regra "preço/prazo geral do Transporte ou da rota" aplicada.
type ServicoResolvido struct {
	Tipo           string
	ServicoID      uuid.UUID // id do transporte ou da atividade
	CodigoAcademia string
	Nome           string
	Ativo          bool
	Gratuito       bool    // nenhuma mensalidade é cobrada
	Preco          float64 // valor de cada mês (0 se gratuito)
	Meses          []int   // meses do ano civil cobrados (1 a 12)
	Metodos        []string
	Taxa           TaxaInscricao
	Disponibilidade
	Coletiva bool
	RotaID   *uuid.UUID
	RotaNome string
	// Problemas lista o que falta configurar para o estudante poder pedir.
	Problemas []string
}

// Configurado diz se o serviço pode ser pedido por um estudante.
func (r ServicoResolvido) Configurado() bool { return len(r.Problemas) == 0 }

// ============================================================================
// Validações de campo
// ============================================================================

// ValidarValorServico é a validação de dinheiro dos serviços: finito, maior
// que zero, no máximo 2 casas decimais e dentro do limite do NUMERIC(14,2).
// Entrada inválida é recusada, nunca arredondada (regra 3 do plano; a Parte 8
// passa a usar o mesmo critério em todas as entradas de dinheiro).
func ValidarValorServico(campo string, v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return regra("%s deve ser um número válido", campo)
	}
	if v <= 0 {
		return regra("%s deve ser maior que zero (para gratuito, use o modo gratuito)", campo)
	}
	if v > MaxValorServico {
		return regra("%s excede o valor máximo permitido (%.2f)", campo, MaxValorServico)
	}
	if math.Abs(v*100-math.Round(v*100)) > 1e-6 {
		return regra("%s aceita no máximo 2 casas decimais", campo)
	}
	return nil
}

// NormalizarMeses valida e ordena os meses do ano civil (1 a 12), sem repetir.
func NormalizarMeses(campo string, meses []int) ([]int, error) {
	seen := map[int]bool{}
	out := make([]int, 0, len(meses))
	for _, m := range meses {
		if m < 1 || m > 12 {
			return nil, regra("%s: mês %d inválido (use 1 a 12)", campo, m)
		}
		if seen[m] {
			return nil, regra("%s: mês %d repetido", campo, m)
		}
		seen[m] = true
		out = append(out, m)
	}
	sort.Ints(out)
	return out, nil
}

var horaRE = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// ValidarHora confere o formato hh:mm (24 horas).
func ValidarHora(campo, v string) error {
	if !horaRE.MatchString(v) {
		return regra("%s deve estar no formato hh:mm (00:00 a 23:59)", campo)
	}
	return nil
}

// ValidarHorarios confere os horários de uma atividade: dia de 1 a 7, início
// antes do fim, e nenhum cruzamento entre horários do mesmo dia.
func ValidarHorarios(hs []Horario) error {
	porDia := map[int][]Horario{}
	for _, h := range hs {
		if h.DiaSemana < 1 || h.DiaSemana > 7 {
			return regra("dia_semana %d inválido (1 = segunda a 7 = domingo)", h.DiaSemana)
		}
		if err := ValidarHora("hora_inicio", h.HoraInicio); err != nil {
			return err
		}
		if err := ValidarHora("hora_fim", h.HoraFim); err != nil {
			return err
		}
		if h.HoraFim <= h.HoraInicio {
			return regra("o horário %s–%s termina antes de começar", h.HoraInicio, h.HoraFim)
		}
		porDia[h.DiaSemana] = append(porDia[h.DiaSemana], h)
	}
	for dia, lista := range porDia {
		sort.Slice(lista, func(i, j int) bool { return lista[i].HoraInicio < lista[j].HoraInicio })
		for i := 1; i < len(lista); i++ {
			if lista[i].HoraInicio < lista[i-1].HoraFim {
				return regra("horários do dia %d se cruzam (%s–%s e %s–%s)", dia,
					lista[i-1].HoraInicio, lista[i-1].HoraFim, lista[i].HoraInicio, lista[i].HoraFim)
			}
		}
	}
	return nil
}

var metodosValidos = map[string]bool{"GPO": true, "REF": true, "GPO_QR": true}

// NormalizarMetodos valida os métodos de pagamento (GPO, REF, GPO_QR), sem repetir.
func NormalizarMetodos(campo string, metodos []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(metodos))
	for _, m := range metodos {
		m = strings.ToUpper(strings.TrimSpace(m))
		if !metodosValidos[m] {
			return nil, regra("%s aceita apenas GPO, REF ou GPO_QR", campo)
		}
		if seen[m] {
			return nil, regra("%s não pode conter duplicados", campo)
		}
		seen[m] = true
		out = append(out, m)
	}
	return out, nil
}

func nomeValido(campo, v string, max int) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", regra("%s é obrigatório", campo)
	}
	if len([]rune(v)) > max {
		return "", regra("%s deve ter no máximo %d caracteres", campo, max)
	}
	return v, nil
}

// ValidarDisponibilidade confere só o FORMATO das listas (a posse dos cursos é
// conferida pelo handler, que conhece a academia).
func ValidarDisponibilidade(d Disponibilidade) error {
	for _, ano := range d.AnosAcademicos {
		if !strings.HasSuffix(ano, "_ano_fundamental") {
			return regra("anos_academicos_disponiveis só aceita anos do ensino fundamental (%q inválido); para médio/superior use cursos_disponiveis", ano)
		}
		if err := utils.ValidateAnoFundamental(ano); err != nil {
			return &ErroRegra{Err: err}
		}
	}
	for _, item := range d.Cursos {
		partes := strings.SplitN(item, "|", 2)
		if len(partes) != 2 || partes[0] == "" || partes[1] == "" {
			return regra("formato de curso disponível inválido: %q (esperado \"<curso_id>|<ano_academico>\")", item)
		}
		if _, err := uuid.Parse(partes[0]); err != nil {
			return regra("curso_id inválido em %q", item)
		}
		ano := partes[1]
		switch {
		case strings.HasSuffix(ano, "_ano_medio"):
			if err := utils.ValidateAnoMedio(ano); err != nil {
				return &ErroRegra{Err: err}
			}
		case strings.HasSuffix(ano, "_ano_superior"):
			if err := utils.ValidateAnoSuperior(ano); err != nil {
				return &ErroRegra{Err: err}
			}
		default:
			return regra("ano_academico inválido em %q: deve terminar em _ano_medio ou _ano_superior", item)
		}
	}
	return nil
}

func validarTaxa(t TaxaInscricao) (TaxaInscricao, error) {
	metodos, err := NormalizarMetodos("metodos_pagamento_taxa_inscricao", t.Metodos)
	if err != nil {
		return t, err
	}
	t.Metodos = metodos
	if t.Valor == 0 {
		if len(metodos) > 0 {
			return t, regra("metodos_pagamento_taxa_inscricao só vale com valor_taxa_inscricao")
		}
		return t, nil
	}
	if err := ValidarValorServico("valor_taxa_inscricao", t.Valor); err != nil {
		return t, err
	}
	if len(metodos) == 0 {
		return t, regra("metodos_pagamento_taxa_inscricao é obrigatório quando há taxa de inscrição")
	}
	return t, nil
}

// ============================================================================
// Transporte
// ============================================================================

// NovoTransporte devolve o Transporte vazio e inativo-de-configuração de uma
// academia (nada definido). É criado na primeira vez que a academia o abre.
func NovoTransporte(codigoAcademia string) *Transporte {
	return &Transporte{
		ID: uuid.New(), CodigoAcademia: codigoAcademia, Ativo: true,
		PrecoModo: ModoNenhum, PrazoModo: ModoNenhum,
		PrazoMeses: []int{}, Metodos: []string{}, Rotas: []Rota{},
		Taxa:            TaxaInscricao{Metodos: []string{}},
		Disponibilidade: Disponibilidade{AnosAcademicos: []string{}, Cursos: []string{}},
		Version:         1,
	}
}

// DefinirPrecoGeral define (ou limpa) o preço geral. Definido, vale para todas
// as rotas e nenhuma rota pode ter preço próprio (409).
func (t *Transporte) DefinirPrecoGeral(modo string, valor float64) error {
	switch modo {
	case ModoNenhum:
		valor = 0
	case ModoGratuito:
		valor = 0
	case ModoValor:
		if err := ValidarValorServico("preco_geral", valor); err != nil {
			return err
		}
	default:
		return regra("preco_geral_modo deve ser 'nenhum', 'gratuito' ou 'valor'")
	}
	if modo != ModoNenhum {
		for _, r := range t.Rotas {
			if r.PrecoModo != ModoNenhum {
				return conflito("a rota %q tem preço próprio: limpe o preço das rotas antes de definir o preço geral", r.Nome)
			}
		}
	}
	t.PrecoModo, t.PrecoValor = modo, valor
	return nil
}

// DefinirPrazoGeral define (ou limpa) o prazo geral. "gratuito" quer dizer que
// nenhum mês é cobrado em nenhuma rota.
func (t *Transporte) DefinirPrazoGeral(modo string, meses []int) error {
	switch modo {
	case ModoNenhum, ModoGratuito:
		meses = []int{}
	case ModoMeses:
		m, err := NormalizarMeses("prazo_geral_meses", meses)
		if err != nil {
			return err
		}
		if len(m) == 0 {
			return regra("prazo_geral_meses deve ter ao menos um mês")
		}
		meses = m
	default:
		return regra("prazo_geral_modo deve ser 'nenhum', 'gratuito' ou 'meses'")
	}
	if modo != ModoNenhum {
		for _, r := range t.Rotas {
			if len(r.PrazoMeses) > 0 {
				return conflito("a rota %q tem prazo próprio: limpe o prazo das rotas antes de definir o prazo geral", r.Nome)
			}
		}
	}
	t.PrazoModo, t.PrazoMeses = modo, meses
	return nil
}

// DefinirPagamento define a taxa de inscrição, os métodos e a disponibilidade.
func (t *Transporte) DefinirPagamento(taxa TaxaInscricao, metodos []string, disp Disponibilidade) error {
	taxa, err := validarTaxa(taxa)
	if err != nil {
		return err
	}
	m, err := NormalizarMetodos("metodos_pagamento", metodos)
	if err != nil {
		return err
	}
	if err := ValidarDisponibilidade(disp); err != nil {
		return err
	}
	t.Taxa, t.Metodos, t.Disponibilidade = taxa, m, disp
	return nil
}

// NovaRota valida e monta uma rota nova. O preço e o prazo da rota só podem
// ser definidos quando o geral não está definido.
func (t *Transporte) NovaRota(nome, horario, precoModo string, precoValor float64, meses []int) (*Rota, error) {
	r := &Rota{ID: uuid.New(), TransporteID: t.ID, Ativo: true, PrazoMeses: []int{}, Veiculos: []Veiculo{}, Version: 1,
		PrecoModo: ModoNenhum}
	if err := t.aplicarRota(r, nome, horario, precoModo, precoValor, meses); err != nil {
		return nil, err
	}
	return r, nil
}

// AtualizarRota troca os dados de uma rota existente (mesmas regras de NovaRota).
func (t *Transporte) AtualizarRota(r *Rota, nome, horario, precoModo string, precoValor float64, meses []int) error {
	return t.aplicarRota(r, nome, horario, precoModo, precoValor, meses)
}

func (t *Transporte) aplicarRota(r *Rota, nome, horario, precoModo string, precoValor float64, meses []int) error {
	nome, err := nomeValido("nome", nome, 100)
	if err != nil {
		return err
	}
	if err := ValidarHora("horario_embarque", horario); err != nil {
		return err
	}
	switch precoModo {
	case "", ModoNenhum:
		precoModo, precoValor = ModoNenhum, 0
	case ModoGratuito:
		precoValor = 0
	case ModoValor:
		if err := ValidarValorServico("preco", precoValor); err != nil {
			return err
		}
	default:
		return regra("preco_modo deve ser 'nenhum', 'gratuito' ou 'valor'")
	}
	m, err := NormalizarMeses("prazo_meses", meses)
	if err != nil {
		return err
	}
	if precoModo != ModoNenhum && t.PrecoModo != ModoNenhum {
		return regra("o preço geral do transporte está definido: a rota não pode ter preço próprio")
	}
	if len(m) > 0 && t.PrazoModo != ModoNenhum {
		return regra("o prazo geral do transporte está definido: a rota não pode ter prazo próprio")
	}
	r.Nome, r.HorarioEmbarque, r.PrecoModo, r.PrecoValor, r.PrazoMeses = nome, horario, precoModo, precoValor, m
	return nil
}

// ValidarNomeVeiculo devolve o nome do veículo já aparado.
func ValidarNomeVeiculo(nome string) (string, error) { return nomeValido("nome", nome, 100) }

// Resolver aplica a regra do geral ou da rota. rota pode ser nil (então só o
// que o geral define conta, e o que depende da rota fica em Problemas).
func (t *Transporte) Resolver(rota *Rota) ServicoResolvido {
	r := ServicoResolvido{
		Tipo: TipoServicoTransporte, ServicoID: t.ID, CodigoAcademia: t.CodigoAcademia,
		Nome: "Transporte", Ativo: t.Ativo, Metodos: t.Metodos, Taxa: t.Taxa,
		Disponibilidade: t.Disponibilidade, Meses: []int{},
	}
	precoModo, preco := t.PrecoModo, t.PrecoValor
	if rota != nil {
		rid := rota.ID
		r.RotaID, r.RotaNome = &rid, rota.Nome
		if precoModo == ModoNenhum {
			precoModo, preco = rota.PrecoModo, rota.PrecoValor
		}
	}
	prazoGratuito := t.PrazoModo == ModoGratuito
	switch {
	case t.PrazoModo == ModoMeses:
		r.Meses = append([]int{}, t.PrazoMeses...)
	case rota != nil && !prazoGratuito:
		r.Meses = append([]int{}, rota.PrazoMeses...)
	}
	r.Gratuito = precoModo == ModoGratuito || prazoGratuito
	if precoModo == ModoValor {
		r.Preco = preco
	}
	if !r.Ativo {
		r.Problemas = append(r.Problemas, "o transporte está desativado")
	}
	if rota != nil && !rota.Ativo {
		r.Problemas = append(r.Problemas, "a rota está desativada")
	}
	if rota != nil {
		ativos := 0
		for _, v := range rota.Veiculos {
			if v.Ativo {
				ativos++
			}
		}
		if ativos == 0 {
			r.Problemas = append(r.Problemas, "a rota não tem veículo ativo")
		}
	}
	if precoModo == ModoNenhum && !prazoGratuito {
		r.Problemas = append(r.Problemas, "preço não definido (geral ou da rota)")
	}
	if !r.Gratuito {
		if len(r.Meses) == 0 {
			r.Problemas = append(r.Problemas, "prazo (meses) não definido (geral ou da rota)")
		}
		if len(r.Metodos) == 0 {
			r.Problemas = append(r.Problemas, "métodos de pagamento não definidos")
		}
	}
	if t.Taxa.Valor > 0 && len(t.Taxa.Metodos) == 0 {
		r.Problemas = append(r.Problemas, "métodos de pagamento da taxa de inscrição não definidos")
	}
	return r
}

// ============================================================================
// Atividade extracurricular
// ============================================================================

// NovaAtividade valida e monta uma atividade nova, ativa.
func NovaAtividade(codigoAcademia, nome, instrutor string, coletiva, gratuito bool, preco float64, meses []int,
	taxa TaxaInscricao, metodos []string, disp Disponibilidade, horarios []Horario) (*Atividade, error) {
	a := &Atividade{ID: uuid.New(), CodigoAcademia: codigoAcademia, Ativo: true, Version: 1}
	if strings.TrimSpace(codigoAcademia) == "" {
		return nil, regra("código da academia é obrigatório")
	}
	if err := a.Atualizar(nome, instrutor, coletiva, gratuito, preco, meses, taxa, metodos, disp, horarios); err != nil {
		return nil, err
	}
	return a, nil
}

// Atualizar troca todos os dados editáveis da atividade (a edição manda o
// registro inteiro; o handler preenche o que não veio com o valor atual).
func (a *Atividade) Atualizar(nome, instrutor string, coletiva, gratuito bool, preco float64, meses []int,
	taxa TaxaInscricao, metodos []string, disp Disponibilidade, horarios []Horario) error {
	nome, err := nomeValido("nome", nome, 150)
	if err != nil {
		return err
	}
	instrutor, err = nomeValido("instrutor", instrutor, 150)
	if err != nil {
		return err
	}
	m, err := NormalizarMeses("prazo_meses", meses)
	if err != nil {
		return err
	}
	if gratuito {
		if preco != 0 || len(m) > 0 {
			return regra("atividade gratuita não tem preço nem prazo: ninguém é cobrado")
		}
	} else {
		if err := ValidarValorServico("preco", preco); err != nil {
			return err
		}
		if len(m) == 0 {
			return regra("prazo_meses deve ter ao menos um mês (os meses cobrados)")
		}
	}
	taxa, err = validarTaxa(taxa)
	if err != nil {
		return err
	}
	mp, err := NormalizarMetodos("metodos_pagamento", metodos)
	if err != nil {
		return err
	}
	if !gratuito && len(mp) == 0 {
		return regra("metodos_pagamento é obrigatório quando a atividade é paga")
	}
	if err := ValidarDisponibilidade(disp); err != nil {
		return err
	}
	if err := ValidarHorarios(horarios); err != nil {
		return err
	}
	if horarios == nil {
		horarios = []Horario{}
	}
	a.Nome, a.Instrutor, a.Coletiva, a.Gratuito, a.Preco, a.PrazoMeses = nome, instrutor, coletiva, gratuito, preco, m
	a.Taxa, a.Metodos, a.Disponibilidade, a.Horarios = taxa, mp, disp, horarios
	return nil
}

// Resolver devolve a visão de cobrança da atividade.
func (a *Atividade) Resolver() ServicoResolvido {
	r := ServicoResolvido{
		Tipo: TipoServicoAtividade, ServicoID: a.ID, CodigoAcademia: a.CodigoAcademia, Nome: a.Nome,
		Ativo: a.Ativo, Gratuito: a.Gratuito, Preco: a.Preco, Meses: append([]int{}, a.PrazoMeses...),
		Metodos: a.Metodos, Taxa: a.Taxa, Disponibilidade: a.Disponibilidade, Coletiva: a.Coletiva,
	}
	if !a.Ativo {
		r.Problemas = append(r.Problemas, "a atividade está desativada")
	}
	if a.Coletiva {
		ativas := 0
		for _, t := range a.Turmas {
			if t.Ativo {
				ativas++
			}
		}
		if ativas == 0 {
			r.Problemas = append(r.Problemas, "atividade coletiva sem turma da atividade")
		}
	}
	if a.Taxa.Valor > 0 && len(a.Taxa.Metodos) == 0 {
		r.Problemas = append(r.Problemas, "métodos de pagamento da taxa de inscrição não definidos")
	}
	return r
}

// ValidarNomeTurma devolve o nome da turma da atividade já aparado.
func ValidarNomeTurma(nome string) (string, error) { return nomeValido("nome", nome, 100) }

// MesesRemovidos devolve os meses que estavam em antes e não estão em depois.
func MesesRemovidos(antes, depois []int) []int {
	ficou := map[int]bool{}
	for _, m := range depois {
		ficou[m] = true
	}
	var out []int
	for _, m := range antes {
		if !ficou[m] {
			out = append(out, m)
		}
	}
	return out
}

// MesCobrado diz se o mês (1 a 12) está entre os cobrados do serviço.
func (r ServicoResolvido) MesCobrado(mes int) bool {
	if r.Gratuito {
		return false
	}
	for _, m := range r.Meses {
		if m == mes {
			return true
		}
	}
	return false
}
