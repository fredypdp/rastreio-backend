package db

import "sort"

// ============================================================================
// Política de event sourcing (regra 17 do Plano de Atualizações 1 — Parte 2B)
// ============================================================================
//
// Regra 17: só o que é sensível, e onde a prova do histórico importa, grava no
// ledger (event sourcing). O resto é CRUD, com auditoria em auditoria_crud
// (auditoria_crud.go). Nenhum agregado nem evento novo entra em event sourcing
// fora da lista aprovada abaixo: o teste politica_eventsourcing_test.go falha
// se um tipo existir na whitelist (safe_queries.go) ou no código sem estar
// classificado aqui.
//
// Cada tipo está em UMA destas situações:
//
//   - aprovado: continua em event sourcing (dinheiro, notas/faltas/resultados,
//     identidade e vínculo do estudante, ciclo de vida das contas e da
//     academia, solicitações, ações administrativas);
//   - pendente de conversão: ainda está no ledger, mas vai para CRUD numa
//     tarefa da Parte 2B (ou da Parte 7, para serviços extras). Cada tarefa de
//     conversão REMOVE seus tipos de ConversaoPendente*, e os tipos deixam de
//     ser gravados — o ledger antigo não é apagado, e os tipos continuam na
//     whitelist enquanto existirem eventos históricos a ler.
//
// Quando ConversaoPendente* ficar vazio, a Parte 2B está concluída.

// Entregas do plano a que uma pendência pertence.
const (
	EntregaConversao2B1  = "2B-1" // dados cadastrais, catálogos e configurações
	EntregaConversao2B2  = "2B-2" // autenticação (senha, 2FA, e-mail)
	EntregaConversao2B3  = "2B-3" // pagamentos, parte operacional (condicional)
	EntregaConversaoP7   = "Parte 7"
	entregaNaoClassifica = ""
)

// AgregadosEventSourcingAprovados são os aggregate types que continuam no
// ledger. Um agregado aprovado pode ter eventos pendentes de conversão (por
// exemplo, Estudante e Academia misturam eventos sensíveis e cadastrais): a
// decisão por evento está em EventosEventSourcingAprovados/ConversaoPendente.
var AgregadosEventSourcingAprovados = setDe(
	"SolicitacaoServicoExtra",
	"Estudante",
	"Academia",
	"Admin",
	"Secretario",
	"Professor",
	"Encarregado",
	"SolicitacaoEdicaoDadoEstudante",
	"SolicitacaoAlteracaoNIFAcademia",
	"Financeiro",
	"System",
)

// ConversaoPendenteAgregados são os aggregate types inteiros que vão para CRUD.
var ConversaoPendenteAgregados = map[string]string{
	"Turma":                EntregaConversao2B1,
	"Sumario":              EntregaConversao2B1,
	"DocumentoExtra":       EntregaConversao2B1,
	"ConfiguracaoFaltas":   EntregaConversao2B1,
	"ConfiguracaoSistema":  EntregaConversao2B1,
	"RemetenteComunicacao": EntregaConversao2B1,
	"MensagemComunicacao":  EntregaConversao2B1,
	// Serviços extras e suas categorias: o catálogo vira CRUD na Parte 7
	// (ver Plano de Atualizações 1), não na 2B.
	"ServicoExtra":     EntregaConversaoP7,
	"CategoriaServico": EntregaConversaoP7,
}

// EventosEventSourcingAprovados são os event types que continuam no ledger.
var EventosEventSourcingAprovados = setDe(
	"SchemaCreated",

	// ── Dinheiro: cobranças, pagamentos, obrigações e serviços extras pagos ──
	"SolicitacaoServicoExtraCriada",
	"SolicitacaoServicoExtraAprovadaPendentePagamento",
	"SolicitacaoServicoExtraVinculada",
	"SolicitacaoServicoExtraReprovada",
	"SolicitacaoServicoExtraCanceladaAntesDaVinculacao",
	"SolicitacaoServicoExtraCancelada",
	"CobrancaSolicitada", "CobrancaCriada", "CobrancaFalhou", "CobrancaCancelada",
	"CobrancaConflitoPosCancelamento", "CobrancaPagamentoExternoRegistrado",
	// Nomes AppyPay: continuam aceitos porque existem para sempre no ledger.
	"CobrancaAppyPaySolicitada", "CobrancaAppyPayCriada", "CobrancaAppyPayFalhou",
	"CobrancaAppyPayCancelada", "CobrancaAppyPayConflitoPosCancelamento",
	"MensalidadePaga", "MensalidadesCobrancaConfirmada", "ServicoExtraLancamentoPago",
	"MensalidadeConfigurada", "MensalidadeConfiguracaoRemovida",
	"MesInicioCobrancaDefinido", "MesInicioCobrancaRemovido",
	"ObrigacaoMensalidadeAnulada", "ObrigacaoMensalidadeReativada",
	"ObrigacaoServicoExtraAnulada", "ObrigacaoServicoExtraReativada",

	// ── Notas, faltas e resultados ──
	"NotasRegistradas", "FaltasRegistradas", "FaltaRegistrada",
	"NotaCorrigida", "FaltaCorrigida",
	"AvaliacaoFinalEscolar", "AvaliacaoFinalSuperior",

	// ── Estudante: identidade, vínculo e trajetória ──
	"EstudanteCriadoComVinculo", "EstudanteDeletado",
	"EstudanteDesvinculadoDaAcademia", "EstudanteReintegrado", "CursoAlterado",
	"FundamentalRetomado", "FundamentalInterrompido", "EquivalenciaFundamentalReconhecida",
	"MedioRetomado", "MedioInterrompido", "EquivalenciaMedioReconhecida",
	"MatriculaSuperiorReativada", "IngressoSuperiorPorEquivalenciaRegistrado",
	"SuperiorInterrompido", "SuperiorAbandonado",
	// Dados críticos (nome, BI, data de nascimento) e o fluxo de solicitação.
	"SolicitacaoEdicaoDadoEstudanteCriada", "SolicitacaoEdicaoDadoEstudanteAprovada",
	"SolicitacaoEdicaoDadoEstudanteReprovada",
	"NomeEstudanteAlteradoPorSolicitacao", "BilheteIdentidadeEstudanteAlteradoPorSolicitacao",
	"BilheteIdentidadeEstudanteAlteradoDiretamente", "BilheteIdentidadeEncarregadoAlteradoPorSolicitacao",
	"DataNascimentoEstudanteAlteradaPorSolicitacao",

	// ── Academia: ciclo de vida, NIF e ano letivo ──
	"AcademiaCriada", "AcademiaAtivada", "AcademiaDesativada", "AcademiaDeletada",
	"AcademiaNIFAlteradoPorSolicitacao",
	"SolicitacaoAlteracaoNIFAcademiaCriada", "SolicitacaoAlteracaoNIFAcademiaAprovada",
	"SolicitacaoAlteracaoNIFAcademiaReprovada",
	"AnoLetivoAcademiaDefinido", "AnoLetivoAcademiaFinalizado",

	// ── Administradores e contas de secretário, professor e encarregado ──
	"AdminCriado", "AdminAtivado", "AdminDesativado", "AdminDeletado", "AdminRoleAtualizado",
	"AcaoAdminRegistrada",
	"ContaPessoaCriada", "ContaPessoaAtivada", "ContaPessoaDesativada", "ContaPessoaDeletada",
	"SecretarioPermissoesDefinidas",
	"EncarregadoEducandoVinculado", "EncarregadoEducandoDesvinculado",
)

// ConversaoPendenteEventos são os event types que ainda estão no ledger mas
// vão para CRUD (ou, no caso da 2B-2 e 2B-3, saem do ledger por outro meio).
//
// Decisões de classificação tomadas aqui (cada tarefa de conversão as confirma
// ao converter e as registra no seu documento):
//   - DadosPessoaisAtualizados e DadosAcademicosAtualizados do estudante: o que
//     é crítico (nome, BI, data de nascimento) tem evento próprio e fica em
//     event sourcing; o restante é cadastral → 2B-1.
//   - TelefoneEncarregadoAlterado: telefone não é dado crítico → 2B-1.
//   - EstudanteDocumentosCompletados: marca de cadastro, não prova → 2B-1.
//   - ProfessorVinculoAtribuido/Removido: vínculo professor ↔ turma/matéria,
//     com início e fim → 2B-1.
//   - ProvedorPagamentoDefinido: provedor escolhido pela academia (operacional)
//     → 2B-3, condicional.
var ConversaoPendenteEventos = map[string]string{
	// ── 2B-1: dados cadastrais, catálogos e configurações ──
	"EstudanteDocumentosCompletados":            EntregaConversao2B1,
	"DadosPessoaisAtualizados":                  EntregaConversao2B1,
	"DadosAcademicosAtualizados":                EntregaConversao2B1,
	"TelefoneEncarregadoAlterado":               EntregaConversao2B1,
	"AcademiaDadosAtualizados":                  EntregaConversao2B1,
	"CursosAtualizados":                         EntregaConversao2B1,
	"CategoriaNotaAdicionada":                   EntregaConversao2B1,
	"CategoriaNotaRemovida":                     EntregaConversao2B1,
	"AcademiaDocumentosObrigatoriosAtualizados": EntregaConversao2B1,
	"AdminDadosAtualizados":                     EntregaConversao2B1,
	"ContaPessoaDadosAtualizados":               EntregaConversao2B1,
	"EncarregadoRelacaoAtualizada":              EntregaConversao2B1,
	"ProfessorVinculoAtribuido":                 EntregaConversao2B1,
	"ProfessorVinculoRemovido":                  EntregaConversao2B1,
	"TurmaCriada":                               EntregaConversao2B1,
	"TurmaAtivada":                              EntregaConversao2B1,
	"TurmaDesativada":                           EntregaConversao2B1,
	"TurmaDadosAtualizados":                     EntregaConversao2B1,
	"TurmaDeletada":                             EntregaConversao2B1,
	"TurmaEncerrada":                            EntregaConversao2B1,
	"EstudanteAdicionadoATurma":                 EntregaConversao2B1,
	"EstudanteRemovidoDaTurma":                  EntregaConversao2B1,
	"SumarioCriado":                             EntregaConversao2B1,
	"SumarioDadosAtualizados":                   EntregaConversao2B1,
	"SumarioDeletado":                           EntregaConversao2B1,
	"DocumentoExtraCriado":                      EntregaConversao2B1,
	"DocumentoExtraAtualizado":                  EntregaConversao2B1,
	"DocumentoExtraDesativado":                  EntregaConversao2B1,
	"DocumentoExtraReativado":                   EntregaConversao2B1,
	"ConfiguracaoFaltasDefinida":                EntregaConversao2B1,
	"ConfiguracaoSistemaDefinida":               EntregaConversao2B1,
	"ConfiguracaoSistemaRemovida":               EntregaConversao2B1,
	"RemetenteComunicacaoConfigurado":           EntregaConversao2B1,
	"MensagemComunicacaoRegistrada":             EntregaConversao2B1,

	// ── Parte 7: catálogo de serviços extras ──
	"CategoriaServicoCriada":     EntregaConversaoP7,
	"CategoriaServicoRenomeada":  EntregaConversaoP7,
	"CategoriaServicoDesativada": EntregaConversaoP7,
	"CategoriaServicoReativada":  EntregaConversaoP7,
	"CategoriaServicoDeletada":   EntregaConversaoP7,
	"ServicoExtraCriado":         EntregaConversaoP7,
	"ServicoExtraAtualizado":     EntregaConversaoP7,
	"ServicoExtraDesativado":     EntregaConversaoP7,
	"ServicoExtraReativado":      EntregaConversaoP7,
	"ServicoExtraDeletado":       EntregaConversaoP7,

	// ── 2B-2: autenticação (senha, verificação em duas etapas, e-mail) ──
	"SenhaAlterada":            EntregaConversao2B2,
	"AcademiaSenhaAlterada":    EntregaConversao2B2,
	"AdminSenhaAlterada":       EntregaConversao2B2,
	"ContaPessoaSenhaAlterada": EntregaConversao2B2,
	"SenhaTemporariaDefinida":  EntregaConversao2B2,
	"DoisFatoresAtivados":      EntregaConversao2B2,
	"DoisFatoresDesativados":   EntregaConversao2B2,
	"EmailVerificado":          EntregaConversao2B2,
	"EmailVerificadoEstudante": EntregaConversao2B2,

	// ── 2B-3 (condicional): pagamentos, parte operacional ──
	"CredenciaisAppyPayConfiguradas":       EntregaConversao2B3,
	"CredenciaisAppyPayRemovidas":          EntregaConversao2B3,
	"SegredoWebhookAppyPayRotacionado":     EntregaConversao2B3,
	"CobrancaAppyPayConsultada":            EntregaConversao2B3,
	"QRCodeAppyPaySolicitado":              EntregaConversao2B3,
	"QRCodeAppyPayGerado":                  EntregaConversao2B3,
	"QRCodeAppyPayFalhou":                  EntregaConversao2B3,
	"WebhookAppyPayRecebido":               EntregaConversao2B3,
	"CredenciaisProvedorConfiguradas":      EntregaConversao2B3,
	"CredenciaisProvedorRemovidas":         EntregaConversao2B3,
	"CredenciaisArquivadasPurgadas":        EntregaConversao2B3,
	"CredenciaisProvedorSnapshotPorEdicao": EntregaConversao2B3,
	"SegredoWebhookProvedorRotacionado":    EntregaConversao2B3,
	"ProvedorPagamentoDefinido":            EntregaConversao2B3,
	"CobrancaConsultada":                   EntregaConversao2B3,
	"QRCodeSolicitado":                     EntregaConversao2B3,
	"QRCodeGerado":                         EntregaConversao2B3,
	"QRCodeFalhou":                         EntregaConversao2B3,
	"WebhookProvedorRecebido":              EntregaConversao2B3,
}

// EventosConvertidosHistoricos / AgregadosConvertidosHistoricos são os tipos
// cuja conversão para CRUD já foi feita: deixaram de ser gravados, mas ficam na
// whitelist porque o ledger antigo ainda os contém e precisa continuar legível
// (replay e verificação de integridade). A tarefa de conversão MOVE o tipo de
// ConversaoPendente* para cá; nada novo pode ser gravado com eles.
var (
	EventosConvertidosHistoricos   = map[string]bool{}
	AgregadosConvertidosHistoricos = map[string]bool{}
)

// Tabelas que já são CRUD (registro oficial, não reconstruídas a partir do
// ledger). Toda tarefa de conversão acrescenta as suas aqui; o teste
// TestTabelasCRUDNaoDependemDeProjecoesReconstruiveis (internal/cadastros)
// garante que nenhuma delas tem chave estrangeira para uma projeção que o
// ledger reconstrói.
const (
	TabelaCursos   = "projection_cursos"
	TabelaMaterias = "projection_materias"
)

// ClassificacaoES diz em que situação um event type ou aggregate type está.
type ClassificacaoES int

const (
	// ESNaoClassificado: o tipo não está em nenhuma das listas. É violação.
	ESNaoClassificado ClassificacaoES = iota
	// ESAprovado: continua em event sourcing.
	ESAprovado
	// ESPendenteConversao: vai para CRUD (ver a entrega em EntregaDeConversao*).
	ESPendenteConversao
	// ESConvertidoHistorico: já convertido; só existe na whitelist para leitura
	// do ledger antigo.
	ESConvertidoHistorico
)

// ClassificarEventType classifica um event type pela política.
func ClassificarEventType(eventType string) ClassificacaoES {
	switch {
	case EventosEventSourcingAprovados[eventType]:
		return ESAprovado
	case ConversaoPendenteEventos[eventType] != entregaNaoClassifica:
		return ESPendenteConversao
	case EventosConvertidosHistoricos[eventType]:
		return ESConvertidoHistorico
	}
	return ESNaoClassificado
}

// ClassificarAggregateType classifica um aggregate type pela política.
func ClassificarAggregateType(aggregateType string) ClassificacaoES {
	switch {
	case AgregadosEventSourcingAprovados[aggregateType]:
		return ESAprovado
	case ConversaoPendenteAgregados[aggregateType] != entregaNaoClassifica:
		return ESPendenteConversao
	case AgregadosConvertidosHistoricos[aggregateType]:
		return ESConvertidoHistorico
	}
	return ESNaoClassificado
}

// EntregaDeConversaoDoEvento devolve a entrega do plano responsável por tirar o
// event type do ledger ("" se não houver pendência).
func EntregaDeConversaoDoEvento(eventType string) string {
	return ConversaoPendenteEventos[eventType]
}

// EventosPendentesDeConversao lista (ordenados) os event types pendentes de uma
// entrega; entrega "" lista todos.
func EventosPendentesDeConversao(entrega string) []string {
	var out []string
	for tipo, e := range ConversaoPendenteEventos {
		if entrega == "" || e == entrega {
			out = append(out, tipo)
		}
	}
	sort.Strings(out)
	return out
}

func setDe(itens ...string) map[string]bool {
	m := make(map[string]bool, len(itens))
	for _, i := range itens {
		m[i] = true
	}
	return m
}

// ViolacoesPoliticaEventSourcing confere a política contra a whitelist e os
// tipos descobertos no código, e devolve uma mensagem por violação (vazio =
// política respeitada). É a lógica do teste-guarda, separada para poder ser
// provada com entradas sintéticas (ver politica_eventsourcing_test.go).
//
// Violações:
//  1. tipo da whitelist ou do código que não está classificado (nem aprovado,
//     nem pendente, nem convertido) — é o caso de um agregado/evento novo fora
//     da lista;
//  2. tipo classificado em mais de uma lista ao mesmo tempo;
//  3. tipo classificado que não existe mais na whitelist (lista desatualizada);
//  4. pendência sem entrega de conversão válida.
func ViolacoesPoliticaEventSourcing(
	whitelistEventos, whitelistAgregados map[string]bool,
	descobertosEventos, descobertosAgregados map[string]bool,
) []string {
	var v []string
	entregasValidas := map[string]bool{
		EntregaConversao2B1: true, EntregaConversao2B2: true,
		EntregaConversao2B3: true, EntregaConversaoP7: true,
	}

	// situacoes devolve em quantas listas o tipo está (deve ser exatamente 1).
	situacoes := func(tipo string, aprovado, historico map[string]bool, pendente map[string]string) int {
		n := 0
		if aprovado[tipo] {
			n++
		}
		if historico[tipo] {
			n++
		}
		if pendente[tipo] != "" {
			n++
		}
		return n
	}

	verifica := func(natureza string, whitelist, descobertos map[string]bool,
		aprovado, historico map[string]bool, pendente map[string]string) {
		for tipo := range whitelist {
			switch n := situacoes(tipo, aprovado, historico, pendente); {
			case n == 0:
				v = append(v, natureza+" "+tipo+": está na whitelist mas não consta na lista aprovada de event sourcing nem na de conversão para CRUD (regra 17)")
			case n > 1:
				v = append(v, natureza+" "+tipo+": consta em mais de uma lista da política")
			}
		}
		for tipo := range descobertos {
			if !whitelist[tipo] {
				v = append(v, natureza+" "+tipo+": usado no código mas ausente da whitelist (safe_queries.go)")
			}
			if situacoes(tipo, aprovado, historico, pendente) == 0 {
				v = append(v, natureza+" "+tipo+": usado no código e não classificado (regra 17)")
			}
		}
		for tipo := range aprovado {
			if !whitelist[tipo] {
				v = append(v, natureza+" "+tipo+": está na lista aprovada mas não existe na whitelist")
			}
		}
		for tipo := range historico {
			if !whitelist[tipo] {
				v = append(v, natureza+" "+tipo+": está como convertido (histórico) mas não existe na whitelist")
			}
		}
		for tipo, entrega := range pendente {
			if !whitelist[tipo] {
				v = append(v, natureza+" "+tipo+": está na lista de conversão mas não existe na whitelist")
			}
			if !entregasValidas[entrega] {
				v = append(v, natureza+" "+tipo+": entrega de conversão inválida "+entrega)
			}
		}
	}

	verifica("evento", whitelistEventos, descobertosEventos,
		EventosEventSourcingAprovados, EventosConvertidosHistoricos, ConversaoPendenteEventos)
	verifica("agregado", whitelistAgregados, descobertosAgregados,
		AgregadosEventSourcingAprovados, AgregadosConvertidosHistoricos, ConversaoPendenteAgregados)

	sort.Strings(v)
	return v
}
