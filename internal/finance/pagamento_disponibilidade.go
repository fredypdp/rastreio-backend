package finance

import (
	"context"
	"errors"
	"fmt"
)

// Disponibilidade de pagamento (Tarefa 121).
//
// O pagador (estudante) só pode usar os métodos habilitados pela academia QUE
// o provedor de pagamento ativo dela suporta. Esta é a definição única dessa
// regra, usada por TODOS os módulos que mostram métodos a quem paga (propina,
// matrícula, serviços extras) e pela validação ao salvar a configuração. A
// geração da cobrança (CreateCharge) continua recusando o que o provedor não
// suporta, em qualquer módulo — este arquivo cuida de MOSTRAR certo e de
// EXPLICAR por que não há o que mostrar.

// Códigos de AvisoMetodos. São estáveis: o frontend decide a apresentação por
// eles e usa a mensagem apenas como texto de apoio.
const (
	// AvisoProvedorNaoConfigurado: a academia não tem provedor ativo com credenciais.
	AvisoProvedorNaoConfigurado = "PROVEDOR_NAO_CONFIGURADO"
	// AvisoMetodosNaoSuportados: há provedor, mas nenhum dos métodos habilitados é suportado por ele.
	AvisoMetodosNaoSuportados = "METODOS_NAO_SUPORTADOS_PELO_PROVEDOR"
	// AvisoNenhumMetodoHabilitado: a academia não habilitou método algum.
	AvisoNenhumMetodoHabilitado = "NENHUM_METODO_HABILITADO"
)

// AvisoMetodos explica ao pagador por que a lista de métodos veio vazia. Só é
// devolvido quando há algo a explicar (lista vazia); campo aditivo nas respostas.
type AvisoMetodos struct {
	Codigo   string `json:"codigo"`
	Mensagem string `json:"mensagem"`
}

// PagamentoDisponivel é o retrato do provedor de pagamento ativo de uma
// academia. Obtém-se uma vez por academia (uma leitura) e serve para filtrar
// quantas listas forem necessárias.
type PagamentoDisponivel struct {
	provedor Provedor
	// indeterminado: falha de infraestrutura ao consultar o provedor. Nada é
	// escondido (uma falha transitória não deve fazer sumirem as opções) e
	// nenhum aviso é dado; a geração da cobrança segue validando o método.
	indeterminado bool
}

// PagamentoDisponivel consulta o provedor ativo (com credenciais) da academia.
func (s *Service) PagamentoDisponivel(ctx context.Context, academia string) PagamentoDisponivel {
	// Sem serviço financeiro utilizável (ex.: Service sem banco, como em testes
	// de outros módulos) não há como saber o provedor: nada é escondido e nenhum
	// aviso é dado, em vez de derrubar uma leitura pública com um pânico.
	if s == nil || s.client == nil {
		return PagamentoDisponivel{indeterminado: true}
	}
	prov, _, err := s.loadCredentialAtiva(ctx, ContextoAcademia, academia)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return PagamentoDisponivel{}
		}
		return PagamentoDisponivel{indeterminado: true}
	}
	return PagamentoDisponivel{provedor: prov}
}

// Configurado informa se a academia tem provedor ativo com credenciais.
func (p PagamentoDisponivel) Configurado() bool { return p.provedor != nil }

// Filtrar devolve, na ordem recebida, só os métodos que o provedor suporta.
// Sem provedor devolve lista vazia (não há como pagar); com falha de
// infraestrutura devolve os métodos recebidos.
func (p PagamentoDisponivel) Filtrar(metodos []string) []string {
	if p.indeterminado {
		return metodos
	}
	out := make([]string, 0, len(metodos))
	if p.provedor == nil {
		return out
	}
	for _, m := range metodos {
		if suportaMetodo(p.provedor, m) {
			out = append(out, m)
		}
	}
	return out
}

// Aviso explica por que ficou sem métodos. habilitados é o que a academia
// configurou; disponiveis, o que sobrou depois de Filtrar. Devolve nil quando
// há métodos disponíveis (nada a explicar) ou quando a situação é indeterminada.
func (p PagamentoDisponivel) Aviso(habilitados, disponiveis []string) *AvisoMetodos {
	if p.indeterminado || len(disponiveis) > 0 {
		return nil
	}
	switch {
	case p.provedor == nil:
		return &AvisoMetodos{Codigo: AvisoProvedorNaoConfigurado,
			Mensagem: "A instituição ainda não definiu um provedor de pagamento, por isso não é possível pagar online neste momento. Contacte a secretaria da instituição."}
	case len(habilitados) == 0:
		return &AvisoMetodos{Codigo: AvisoNenhumMetodoHabilitado,
			Mensagem: "A instituição não habilitou nenhum método de pagamento para este item. Contacte a secretaria da instituição."}
	default:
		return &AvisoMetodos{Codigo: AvisoMetodosNaoSuportados,
			Mensagem: "Os métodos de pagamento habilitados pela instituição não são suportados pelo provedor de pagamento atual. Contacte a secretaria da instituição."}
	}
}

// FiltrarMetodosPorProvedor devolve, na ordem recebida, apenas os métodos que o
// provedor ATIVO da academia suporta (ver PagamentoDisponivel.Filtrar).
func (s *Service) FiltrarMetodosPorProvedor(ctx context.Context, academia string, metodos []string) []string {
	return s.PagamentoDisponivel(ctx, academia).Filtrar(metodos)
}

// MetodosPagamentoMensalidadeComAviso é MetodosPagamentoMensalidade com a
// explicação de uma lista vazia (provedor não configurado, métodos não
// suportados ou nenhum método habilitado).
func (s *Service) MetodosPagamentoMensalidadeComAviso(ctx context.Context, academia string) ([]string, *AvisoMetodos, error) {
	if s == nil || s.client == nil {
		return nil, nil, errors.New("serviço financeiro sem banco de dados")
	}
	habilitados, err := s.metodosPagamentoMensalidade(ctx, academia)
	if err != nil {
		return nil, nil, err
	}
	disp := s.PagamentoDisponivel(ctx, academia)
	metodos := disp.Filtrar(habilitados)
	return metodos, disp.Aviso(habilitados, metodos), nil
}

// ValidarMetodosSeHouverProvedor valida, ao SALVAR uma configuração, que os
// métodos são suportados pelo provedor ativo da academia. Sem provedor ativo
// não há contra o que validar e a configuração é aceita: ao contrário de
// mensalidade e matrícula (que exigem o provedor para habilitar pagamento), os
// serviços extras podem ser cadastrados antes de o provedor ser definido; até
// lá o pagador vê o aviso AvisoProvedorNaoConfigurado e a geração da cobrança
// recusa qualquer método.
func (s *Service) ValidarMetodosSeHouverProvedor(ctx context.Context, academia string, metodos []string) error {
	if s == nil || s.client == nil {
		return nil
	}
	prov, _, err := s.loadCredentialAtiva(ctx, ContextoAcademia, academia)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	for _, m := range metodos {
		if !suportaMetodo(prov, m) {
			return fmt.Errorf("%w: %s não é suportado pelo provedor %s", ErrMetodoIndisponivel, m, prov.Nome())
		}
	}
	return nil
}
