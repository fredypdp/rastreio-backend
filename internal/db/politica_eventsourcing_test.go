package db

import (
	"strings"
	"testing"
)

// Teste-guarda da regra 17 (Parte 2B): nenhum agregado nem evento entra em
// event sourcing fora da lista aprovada. A lógica está em
// ViolacoesPoliticaEventSourcing; aqui ela é provada com os dados reais do
// repositório e com entradas sintéticas (para provar que o guarda realmente
// reprova o que deve reprovar).

func copiaSet(m map[string]bool) map[string]bool {
	c := make(map[string]bool, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// TestPoliticaEventSourcingRespeitadaPeloCodigo é o guarda de verdade: toda
// whitelist e todo tipo usado no código estão classificados.
func TestPoliticaEventSourcingRespeitadaPeloCodigo(t *testing.T) {
	descEventos, descAgregados := discoverEventAndAggregateTypes(t, aggregatesSourceDir, financeSourceDir)

	violacoes := ViolacoesPoliticaEventSourcing(
		copiaSet(validEventTypes), copiaSet(validAggregateTypes),
		descEventos, descAgregados,
	)
	if len(violacoes) > 0 {
		t.Fatalf("política de event sourcing violada (regra 17 — só o aprovado fica no ledger; "+
			"o resto é CRUD com auditoria_crud):\n  - %s", strings.Join(violacoes, "\n  - "))
	}
}

// TestPoliticaClassificaTodaWhitelistExatamenteUmaVez confere a partição: todo
// tipo da whitelist cai em exatamente uma situação.
func TestPoliticaClassificaTodaWhitelistExatamenteUmaVez(t *testing.T) {
	for tipo := range validEventTypes {
		if ClassificarEventType(tipo) == ESNaoClassificado {
			t.Errorf("evento %s não classificado", tipo)
		}
	}
	for tipo := range validAggregateTypes {
		if ClassificarAggregateType(tipo) == ESNaoClassificado {
			t.Errorf("agregado %s não classificado", tipo)
		}
	}
	if got := len(EventosEventSourcingAprovados) + len(ConversaoPendenteEventos) + len(EventosConvertidosHistoricos); got != len(validEventTypes) {
		t.Errorf("aprovados+pendentes+convertidos = %d eventos, whitelist tem %d", got, len(validEventTypes))
	}
	if got := len(AgregadosEventSourcingAprovados) + len(ConversaoPendenteAgregados) + len(AgregadosConvertidosHistoricos); got != len(validAggregateTypes) {
		t.Errorf("aprovados+pendentes+convertidos = %d agregados, whitelist tem %d", got, len(validAggregateTypes))
	}
}

// Cada agregado pendente de conversão só pode ter eventos pendentes ou
// convertidos — nunca um evento aprovado. (O inverso é permitido: Estudante e
// Academia são aprovados e têm eventos pendentes.)
func TestPoliticaAgregadoPendenteNaoTemEventoAprovado(t *testing.T) {
	prefixos := map[string][]string{
		"DocumentoExtra":       {"DocumentoExtra"},
		"ConfiguracaoFaltas":   {"ConfiguracaoFaltas"},
		"ConfiguracaoSistema":  {"ConfiguracaoSistema"},
		"RemetenteComunicacao": {"RemetenteComunicacao"},
		"MensagemComunicacao":  {"MensagemComunicacao"},
		"ServicoExtra":         {"ServicoExtra"},
		"CategoriaServico":     {"CategoriaServico"},
	}
	for agregado := range ConversaoPendenteAgregados {
		pfx, ok := prefixos[agregado]
		if !ok {
			t.Errorf("agregado pendente %s sem prefixo de eventos no teste", agregado)
			continue
		}
		for evento := range EventosEventSourcingAprovados {
			for _, p := range pfx {
				// Só compartilha o prefixo, mas é de outro agregado:
				// ServicoExtraLancamentoPago é dinheiro (event sourcing).
				if strings.HasPrefix(evento, p) && evento != "ServicoExtraLancamentoPago" {
					t.Errorf("agregado %s é pendente de conversão mas o evento %s está aprovado", agregado, evento)
				}
			}
		}
	}
}

// Dinheiro, notas/faltas e identidade nunca podem aparecer como pendentes.
func TestPoliticaSensivelContinuaAprovado(t *testing.T) {
	devemFicar := []string{
		"CobrancaCriada", "CobrancaPagamentoExternoRegistrado", "MensalidadePaga",
		"ObrigacaoMensalidadeAnulada", "NotasRegistradas", "NotaCorrigida", "FaltaCorrigida",
		"AvaliacaoFinalEscolar", "AvaliacaoFinalSuperior", "EstudanteCriadoComVinculo",
		"NomeEstudanteAlteradoPorSolicitacao", "DataNascimentoEstudanteAlteradaPorSolicitacao",
		"AdminCriado", "AcaoAdminRegistrada", "AcademiaCriada", "ContaPessoaCriada",
	}
	for _, e := range devemFicar {
		if ClassificarEventType(e) != ESAprovado {
			t.Errorf("evento sensível %s deveria continuar em event sourcing", e)
		}
	}
}

func TestPoliticaGuardaReprovaTipoNovoForaDaLista(t *testing.T) {
	// Tipo fictício "pendente de conversão": os testes não dependem de tipos
	// reais, que saem da lista conforme as conversões da Parte 2B avançam.
	const pendente = "XptoConvertivel"
	ConversaoPendenteEventos[pendente] = EntregaConversao2B1
	t.Cleanup(func() { delete(ConversaoPendenteEventos, pendente) })
	base := func() (map[string]bool, map[string]bool) {
		ev := copiaSet(validEventTypes)
		ev[pendente] = true
		return ev, copiaSet(validAggregateTypes)
	}

	t.Run("evento novo na whitelist sem classificar", func(t *testing.T) {
		ev, ag := base()
		ev["XptoCriado"] = true
		v := ViolacoesPoliticaEventSourcing(ev, ag, nil, nil)
		if len(v) != 1 || !strings.Contains(v[0], "XptoCriado") || !strings.Contains(v[0], "regra 17") {
			t.Fatalf("violações = %v; queria exatamente a de XptoCriado", v)
		}
	})

	t.Run("agregado novo na whitelist sem classificar", func(t *testing.T) {
		ev, ag := base()
		ag["Xpto"] = true
		v := ViolacoesPoliticaEventSourcing(ev, ag, nil, nil)
		if len(v) != 1 || !strings.Contains(v[0], "agregado Xpto") {
			t.Fatalf("violações = %v; queria exatamente a de Xpto", v)
		}
	})

	t.Run("tipo usado no código mas fora da whitelist e da política", func(t *testing.T) {
		ev, ag := base()
		v := ViolacoesPoliticaEventSourcing(ev, ag,
			map[string]bool{"XptoCriado": true}, map[string]bool{"Xpto": true})
		if len(v) != 4 {
			t.Fatalf("violações = %v; queria 4 (whitelist e política, para evento e agregado)", v)
		}
	})

	t.Run("tipo em duas listas", func(t *testing.T) {
		ev, ag := base()
		// O tipo já é pendente; marcá-lo também como histórico duplica.
		EventosConvertidosHistoricos[pendente] = true
		defer delete(EventosConvertidosHistoricos, pendente)
		v := ViolacoesPoliticaEventSourcing(ev, ag, nil, nil)
		if len(v) != 1 || !strings.Contains(v[0], pendente) || !strings.Contains(v[0], "mais de uma lista") {
			t.Fatalf("violações = %v", v)
		}
	})

	t.Run("lista com tipo que não existe mais na whitelist", func(t *testing.T) {
		ev, ag := base()
		delete(ev, pendente)
		v := ViolacoesPoliticaEventSourcing(ev, ag, nil, nil)
		if len(v) != 1 || !strings.Contains(v[0], pendente) || !strings.Contains(v[0], "não existe na whitelist") {
			t.Fatalf("violações = %v", v)
		}
	})

	t.Run("entrega de conversão inválida", func(t *testing.T) {
		ev, ag := base()
		ConversaoPendenteEventos[pendente] = "entrega-inventada"
		defer func() { ConversaoPendenteEventos[pendente] = EntregaConversao2B1 }()
		v := ViolacoesPoliticaEventSourcing(ev, ag, nil, nil)
		if len(v) != 1 || !strings.Contains(v[0], "entrega de conversão inválida") {
			t.Fatalf("violações = %v", v)
		}
	})

	t.Run("convertido histórico continua aceito", func(t *testing.T) {
		ev, ag := base()
		// Simula a conclusão da conversão do tipo pendente.
		delete(ConversaoPendenteEventos, pendente)
		EventosConvertidosHistoricos[pendente] = true
		defer func() {
			delete(EventosConvertidosHistoricos, pendente)
			ConversaoPendenteEventos[pendente] = EntregaConversao2B1
		}()
		if v := ViolacoesPoliticaEventSourcing(ev, ag, nil, nil); len(v) != 0 {
			t.Fatalf("violações = %v; convertido histórico deveria ser aceito", v)
		}
	})
}

func TestEventosPendentesDeConversaoOrdenadosEPorEntrega(t *testing.T) {
	todos := EventosPendentesDeConversao("")
	if len(todos) != len(ConversaoPendenteEventos) {
		t.Fatalf("len = %d, want %d", len(todos), len(ConversaoPendenteEventos))
	}
	for i := 1; i < len(todos); i++ {
		if todos[i-1] > todos[i] {
			t.Fatalf("lista não ordenada em %q > %q", todos[i-1], todos[i])
		}
	}
	for _, e := range EventosPendentesDeConversao(EntregaConversao2B2) {
		if EntregaDeConversaoDoEvento(e) != EntregaConversao2B2 {
			t.Fatalf("%s não é da 2B-2", e)
		}
	}
	if len(EventosPendentesDeConversao(EntregaConversao2B2)) == 0 {
		t.Fatal("2B-2 sem pendências: lista de autenticação vazia")
	}
}
