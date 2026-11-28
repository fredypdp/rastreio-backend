package permissoes

import "strings"

// Tipo de decisão para uma rota quando quem chama é um SECRETÁRIO.
type Tipo int

const (
	// Livre: qualquer secretário ativo (consultas básicas que toda tela precisa,
	// como o ano letivo e o estado dos jobs).
	Livre Tipo = iota + 1
	// ExigePermissao: só com a permissão indicada.
	ExigePermissao
	// Reservada: só a ACADEMIA. Conta da academia, gestão de secretários e
	// credenciais do provedor de pagamento. Nunca delegável.
	Reservada
	// Negada: nem a academia delegaria. Hoje: lançar/corrigir notas e faltas
	// (só de professores) e o pagamento feito pelo estudante.
	Negada
)

// Regra é a decisão para uma rota.
type Regra struct {
	Tipo      Tipo
	Permissao string
}

func chave(metodo, caminho string) string { return metodo + " " + caminho }

var regras = map[string]Regra{}

func livre(rotas ...string)     { registrar(Regra{Tipo: Livre}, rotas) }
func reservada(rotas ...string) { registrar(Regra{Tipo: Reservada}, rotas) }
func negada(rotas ...string)    { registrar(Regra{Tipo: Negada}, rotas) }
func exige(permissao string, rotas ...string) {
	registrar(Regra{Tipo: ExigePermissao, Permissao: permissao}, rotas)
}

func registrar(r Regra, rotas []string) {
	for _, rota := range rotas {
		regras[rota] = r
	}
}

// RegraDaRota devolve a decisão registrada para "MÉTODO /caminho/registrado".
// Rota ausente da tabela = não encontrada = o chamador DEVE negar.
func RegraDaRota(metodo, caminho string) (Regra, bool) {
	r, ok := regras[chave(metodo, caminho)]
	return r, ok
}

// RotasClassificadas lista as chaves registradas (usado pelo teste de varredura).
func RotasClassificadas() []string {
	saida := make([]string, 0, len(regras))
	for k := range regras {
		saida = append(saida, k)
	}
	return saida
}

// PrefixosDeOutrosPerfis são rotas autenticadas que o secretário NUNCA alcança
// por pertencerem a outro perfil (admin, estudante, webhooks, integrações). O
// teste de varredura as aceita sem classificação individual; o middleware nega
// por padrão de qualquer forma, porque não estão na tabela.
var PrefixosDeOutrosPerfis = []string{"/dominis/", "/admin/", "/estudante/", "/integracoes/", "/webhooks/", "/professor/", "/encarregado/"}

// PertenceAOutroPerfil diz se o caminho registrado é de outro perfil.
func PertenceAOutroPerfil(caminho string) bool {
	for _, p := range PrefixosDeOutrosPerfis {
		if strings.HasPrefix(caminho, p) {
			return true
		}
	}
	return false
}

func init() {
	// ── Consultas básicas (qualquer secretário) ─────────────────────────────
	livre(
		"GET /ano-letivo", "GET /anos-letivos-lista", "GET /anos-letivos/configuracoes",
		"GET /academia/ano-letivo", "GET /academia/anos-letivos-lista", "GET /academia/anos-academicos",
		"GET /academia/anos-letivos/finalizacoes", "GET /academia/configuracao-status",
		"GET /academia/categorias-nota",
		"GET /jobs", "GET /jobs/stream", "GET /jobs/:id", "DELETE /jobs/:id/sse", "POST /jobs/:id/retry-failed",
	)

	// ── Estudantes ──────────────────────────────────────────────────────────
	exige(EstudantesVer,
		"GET /estudantes", "GET /consultar-estudante/:codigo", "GET /eventos-estudante/:codigo",
		"GET /eventos/:event_id", "GET /verificar-integridade/:codigo",
		"GET /documentos/estudantes/:codigo/:campo/download",
		"GET /academia/documentos/estudantes/:codigo/:campo/download",
		"GET /turmas-estudante/:codigo",
		"GET /academia/documentos-extra",
	)
	exige(EstudantesCadastrar,
		"POST /academia/estudante/register", "POST /academia/estudante/register/async",
		"POST /academia/estudante/:codigo/documentos",
		"POST /academia/documentos-extra", "PUT /academia/documentos-extra/:id",
		"PUT /academia/documentos-extra/:id/desativar", "PUT /academia/documentos-extra/:id/reativar",
		"GET /academia/encarregados/buscar",
	)
	exige(EstudantesSituacao,
		"GET /academia/solicitacoes",
		"POST /academia/estudante/:codigo/interromper/percurso-academico",
		"POST /academia/estudante/:codigo/interromper/percurso-academico/reprovar",
		"POST /academia/estudante/:codigo/desvincular", "POST /academia/estudante/:codigo/desvincular/reprovar",
		"POST /academia/estudante/:codigo/revincular", "POST /academia/estudante/:codigo/revincular/reprovar",
	)
	exige(EstudantesDados,
		"GET /academia/solicitacoes-edicao-estudante",
		"GET /academia/documentos/solicitacoes-edicao-estudante/:codigo/documento/download",
		"PUT /academia/solicitacoes-edicao-estudante/nome/:codigo/aprovar",
		"PUT /academia/solicitacoes-edicao-estudante/nome/:codigo/reprovar",
		"PUT /academia/solicitacoes-edicao-estudante/bilhete-identidade/:codigo/aprovar",
		"PUT /academia/solicitacoes-edicao-estudante/bilhete-identidade/:codigo/reprovar",
		"PUT /academia/solicitacoes-edicao-estudante/bilhete-identidade-encarregado/:codigo/aprovar",
		"PUT /academia/solicitacoes-edicao-estudante/bilhete-identidade-encarregado/:codigo/reprovar",
		"PUT /academia/solicitacoes-edicao-estudante/data-nascimento/:codigo/aprovar",
		"PUT /academia/solicitacoes-edicao-estudante/data-nascimento/:codigo/reprovar",
		"PUT /academia/estudante/:codigo/encarregado",
	)
	exige(EstudantesExcluir, "DELETE /academia/estudante/:codigo/conta")
	exige(EstudantesCredenciais, "POST /estudantes/:codigo/credenciais/reenviar")

	// ── Turmas, cursos, matérias, sumários ──────────────────────────────────
	exige(TurmasVer, "GET /academia/turmas", "GET /academia/turma/:codigo")
	exige(TurmasGerir,
		"POST /academia/turma", "PUT /academia/turma/:codigo/ativar", "PUT /academia/turma/:codigo/desativar",
		"PUT /academia/turma/:codigo/dados", "DELETE /academia/turma/:codigo",
		"POST /academia/turma/:codigo/estudante", "DELETE /academia/turma/:codigo/estudantes/:codigo_estudante",
		"POST /academia/turma/async", "POST /academia/turma/estudante/async",
		"PUT /academia/turma/ativar/async", "PUT /academia/turma/desativar/async",
		"PUT /academia/turma/dados/async", "DELETE /academia/turma/async", "DELETE /academia/turma/estudante/async",
	)
	exige(CursosGerir,
		"POST /academia/curso", "PUT /academia/curso/:id/ativar", "PUT /academia/curso/:id/desativar",
		"PUT /academia/curso/:id/dados", "DELETE /academia/curso/:id",
		"POST /academia/curso/async", "PUT /academia/curso/ativar/async", "PUT /academia/curso/desativar/async",
		"PUT /academia/curso/dados/async", "DELETE /academia/curso/async",
	)
	exige(MateriasVer, "GET /academia/materias", "GET /academia/materia/:id")
	exige(MateriasGerir,
		"POST /academia/materia", "PUT /academia/materia/:id/ativar", "PUT /academia/materia/:id/desativar",
		"PUT /academia/materia/:id/dados", "DELETE /academia/materia/:id",
		"POST /academia/materia/async", "PUT /academia/materia/ativar/async", "PUT /academia/materia/desativar/async",
		"PUT /academia/materia/dados/async", "DELETE /academia/materia/async",
	)
	exige(SumariosVer, "GET /academia/sumarios", "GET /academia/sumario/:id")
	exige(SumariosGerir, "POST /academia/sumario", "PUT /academia/sumario/:id/dados", "DELETE /academia/sumario/:id")

	// ── Notas, faltas e avaliações: o secretário só CONSULTA ────────────────
	exige(NotasVer, "GET /notas", "GET /notas-estudante/:codigo")
	exige(FaltasVer, "GET /faltas", "GET /faltas-estudante/:codigo", "GET /academia/faltas/configuracao")
	exige(FaltasConfigurar, "PUT /academia/faltas/configuracao")
	exige(AvaliacoesVer,
		"GET /avaliacoes", "GET /aprovacoes", "GET /reprovacoes", "GET /avaliacoes-estudante/:codigo",
		"GET /academia/avaliacao-final/regras",
	)
	exige(AvaliacoesConfigurar,
		"POST /academia/avaliacao-final/regras", "PUT /academia/avaliacao-final/regras/:id",
		"DELETE /academia/avaliacao-final/regras/:id",
		"POST /academia/categorias-nota", "DELETE /academia/categorias-nota/:codigo",
		"POST /academia/categorias-nota/async", "DELETE /academia/categorias-nota/async",
	)
	// Lançar e corrigir notas e faltas é só do professor (matriz do plano).
	negada(
		"POST /academia/notas-aluno", "PATCH /academia/notas-aluno/:id", "POST /academia/notas-aluno/async",
		"POST /academia/faltas-aluno", "PATCH /academia/faltas-aluno/:id", "POST /academia/faltas-aluno/async",
		"PUT /academia/faltas-aluno/:id/desvincular-sumario",
	)

	// ── Serviços extras ─────────────────────────────────────────────────────
	exige(ServicosExtrasVer,
		"GET /academia/servicos-extras", "GET /academia/servicos-extras/transporte",
		"GET /academia/servicos-extras/atividades", "GET /academia/servicos-extras/atividades/:id",
		"GET /academia/servicos-extras/turmas-atividade/:turma_id/membros",
		"GET /academia/servicos-extras/solicitacoes", "GET /academia/servicos-extras/solicitacoes/:id",
		"GET /academia/servicos-extras/solicitacoes/:id/documento/download",
		"GET /academia/servicos-extras/inscricoes/:id/pendencias",
	)
	exige(ServicosExtrasGerir,
		"PUT /academia/servicos-extras/transporte",
		"POST /academia/servicos-extras/transporte/rotas", "PUT /academia/servicos-extras/transporte/rotas/:id",
		"PUT /academia/servicos-extras/transporte/rotas/:id/desativar", "PUT /academia/servicos-extras/transporte/rotas/:id/reativar",
		"DELETE /academia/servicos-extras/transporte/rotas/:id",
		"POST /academia/servicos-extras/transporte/rotas/:id/veiculos",
		"PUT /academia/servicos-extras/transporte/veiculos/:id", "DELETE /academia/servicos-extras/transporte/veiculos/:id",
		"POST /academia/servicos-extras/atividades", "PUT /academia/servicos-extras/atividades/:id",
		"PUT /academia/servicos-extras/atividades/:id/desativar", "PUT /academia/servicos-extras/atividades/:id/reativar",
		"DELETE /academia/servicos-extras/atividades/:id",
		"POST /academia/servicos-extras/atividades/:id/turmas",
		"PUT /academia/servicos-extras/turmas-atividade/:turma_id", "DELETE /academia/servicos-extras/turmas-atividade/:turma_id",
	)
	exige(ServicosExtrasSolicitacoes,
		"PUT /academia/servicos-extras/solicitacoes/:id/aprovar", "PUT /academia/servicos-extras/solicitacoes/:id/reprovar",
		"PUT /academia/servicos-extras/inscricoes/:id/cancelar",
	)

	// ── Financeiro ──────────────────────────────────────────────────────────
	exige(FinanceiroVer,
		"GET /financeiro/provedores", "GET /financeiro/provedor", "GET /financeiro/credenciais",
		"GET /financeiro/cobrancas", "GET /financeiro/cobrancas/:id",
		"GET /financeiro/cobrancas/estudante/:codigo", "GET /financeiro/mensalidades/estudante/:codigo",
		"GET /financeiro/mensalidades/configuracoes", "GET /financeiro/mensalidades/inicio-cobranca",
	)
	exige(FinanceiroGerir,
		"POST /financeiro/cobrancas", "POST /financeiro/qr-codes",
		"POST /financeiro/cobrancas/:id/cancelar", "POST /financeiro/cobrancas/:id/pago-externamente",
		"POST /financeiro/servicos-extras/obrigacao/anular", "POST /financeiro/servicos-extras/obrigacao/reativar",
		"POST /financeiro/mensalidades/configuracoes", "PUT /financeiro/mensalidades/configuracoes",
		"DELETE /financeiro/mensalidades/configuracoes",
		"POST /financeiro/mensalidades/inicio-cobranca", "DELETE /financeiro/mensalidades/inicio-cobranca",
		"POST /financeiro/mensalidades/obrigacoes/anular", "POST /financeiro/mensalidades/obrigacoes/reativar",
		"POST /financeiro/mensalidades/obrigacoes/pago-externamente",
	)
	// Escolher o provedor e mexer em credenciais/segredos é da conta da academia.
	reservada(
		"PUT /financeiro/provedor", "POST /financeiro/credenciais", "PUT /financeiro/credenciais/:id",
		"DELETE /financeiro/credenciais", "GET /financeiro/credenciais/:id/webhook-secret",
		"POST /financeiro/credenciais/:id/webhook-secret/rotacionar",
	)
	// Pagar é do estudante (e do encarregado): a academia e o secretário não pagam.
	negada(
		"POST /financeiro/mensalidades/pagamento",
		"POST /financeiro/servicos-extras/taxa-inscricao/pagamento",
		"POST /financeiro/servicos-extras/obrigacao/pagamento",
	)

	// ── Comunicação ─────────────────────────────────────────────────────────
	exige(ComunicacaoVer, "GET /comunicacao/mensagens")
	exige(ComunicacaoEnviar, "POST /comunicacao/mensagens")

	// ── Ano letivo ──────────────────────────────────────────────────────────
	exige(AnoLetivoGerir,
		"POST /academia/definir-ano-letivo", "POST /academia/anos-academicos",
		"DELETE /academia/anos-academicos", "POST /academia/anos-letivos/finalizar",
	)

	// ── Professores (a academia e o secretário criam) ───────────────────────
	exige(ProfessoresVer, "GET /academia/professores", "GET /academia/professores/:id")
	exige(ProfessoresGerir,
		"POST /academia/professores", "PUT /academia/professores/:id",
		"PUT /academia/professores/:id/ativar", "PUT /academia/professores/:id/desativar",
		"DELETE /academia/professores/:id",
		"POST /academia/professores/:id/vinculos", "DELETE /academia/professores/:id/vinculos/:vinculo_id",
		"PUT /academia/vinculos-professor/:vinculo_id/reatribuir",
	)

	// ── Conta da academia e gestão de secretários: só a academia ────────────
	reservada(
		"PUT /academia/dados", "PUT /academia/dados/async", "PUT /academia/nivel-escolar",
		"POST /academia/solicitacoes-nif", "GET /academia/solicitacoes-nif",
		"GET /academia/documentos", "GET /academia/documentos/academia/:campo/download",
		"GET /documentos/academias/:codigo/:campo/download", "POST /documentos/academias/:codigo/:campo/upload",
		"GET /academia/permissoes",
		"POST /academia/secretarios", "GET /academia/secretarios", "GET /academia/secretarios/:id",
		"PUT /academia/secretarios/:id", "PUT /academia/secretarios/:id/ativar",
		"PUT /academia/secretarios/:id/desativar", "DELETE /academia/secretarios/:id",
	)
}
