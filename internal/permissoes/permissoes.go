// Package permissoes define o que um secretário pode fazer em nome da academia.
//
// Plano de Atualizações, Parte 4: o secretário tem as mesmas funções da academia,
// EXCETO (1) lançar/corrigir notas e faltas (só de professores) e (2) tudo o que
// é "da própria conta da academia" ou "retroativo" em relação a ele (gerir
// secretários, alterar os dados/NIF/alvará/nível da academia, credenciais do
// provedor de pagamento). Dentro do que sobra, a ACADEMIA escolhe, ao criar cada
// secretário, quais permissões ele terá (catálogo abaixo).
//
// O pacote não depende de gin nem de banco: é só a tabela de decisão. Quem a
// aplica é o middleware de autenticação (default: negar).
package permissoes

import (
	"fmt"
	"sort"
	"strings"
)

// Permissao é uma capacidade que a academia pode conceder a um secretário.
type Permissao struct {
	Chave     string   `json:"chave"`
	Rotulo    string   `json:"rotulo"`
	Descricao string   `json:"descricao"`
	Grupo     string   `json:"grupo"`
	Implica   []string `json:"implica,omitempty"`
}

// Chaves do catálogo. Constantes para os testes e a tabela de rotas não
// dependerem de strings soltas.
const (
	EstudantesVer         = "estudantes.ver"
	EstudantesCadastrar   = "estudantes.cadastrar"
	EstudantesSituacao    = "estudantes.situacao"
	EstudantesDados       = "estudantes.dados"
	EstudantesExcluir     = "estudantes.excluir"
	EstudantesCredenciais = "estudantes.credenciais"

	TurmasVer   = "turmas.ver"
	TurmasGerir = "turmas.gerir"

	CursosGerir = "cursos.gerir"

	MateriasVer   = "materias.ver"
	MateriasGerir = "materias.gerir"

	SumariosVer   = "sumarios.ver"
	SumariosGerir = "sumarios.gerir"

	NotasVer = "notas.ver"

	FaltasVer        = "faltas.ver"
	FaltasConfigurar = "faltas.configurar"

	AvaliacoesVer        = "avaliacoes.ver"
	AvaliacoesConfigurar = "avaliacoes.configurar"

	ServicosExtrasVer          = "servicos_extras.ver"
	ServicosExtrasGerir        = "servicos_extras.gerir"
	ServicosExtrasSolicitacoes = "servicos_extras.solicitacoes"

	FinanceiroVer   = "financeiro.ver"
	FinanceiroGerir = "financeiro.gerir"

	ComunicacaoVer    = "comunicacao.ver"
	ComunicacaoEnviar = "comunicacao.enviar"

	AnoLetivoGerir = "ano_letivo.gerir"

	ProfessoresVer   = "professores.ver"
	ProfessoresGerir = "professores.gerir"
)

// Catalogo é a lista oficial, na ordem em que a tela de criação a apresenta.
var Catalogo = []Permissao{
	{EstudantesVer, "Consultar estudantes", "Ver a lista de estudantes, perfis, documentos, turmas e histórico.", "Estudantes", nil},
	{EstudantesCadastrar, "Cadastrar e matricular estudantes", "Cadastro individual e em massa, completar documentos e documentos extra.", "Estudantes", []string{EstudantesVer}},
	{EstudantesSituacao, "Decidir situação académica", "Aprovar ou reprovar pedidos de interrupção, desvinculação e revinculação.", "Estudantes", []string{EstudantesVer}},
	{EstudantesDados, "Decidir edição de dados do estudante", "Aprovar ou reprovar pedidos de alteração de nome, BI e data de nascimento.", "Estudantes", []string{EstudantesVer}},
	{EstudantesExcluir, "Excluir conta de estudante", "Eliminar a conta de um estudante cadastrado pela academia.", "Estudantes", []string{EstudantesVer}},
	{EstudantesCredenciais, "Reenviar credenciais do estudante", "Gerar nova senha temporária e reenviar as credenciais por SMS.", "Estudantes", []string{EstudantesVer}},

	{TurmasVer, "Consultar turmas", "Ver turmas e os estudantes de cada uma.", "Turmas", nil},
	{TurmasGerir, "Gerir turmas", "Criar, editar, ativar, desativar e eliminar turmas; adicionar e remover estudantes.", "Turmas", []string{TurmasVer}},

	{CursosGerir, "Gerir cursos", "Criar, editar, ativar, desativar e eliminar cursos.", "Cursos", nil},

	{MateriasVer, "Consultar matérias", "Ver as matérias disciplinares.", "Matérias", nil},
	{MateriasGerir, "Gerir matérias", "Criar, editar, ativar, desativar e eliminar matérias.", "Matérias", []string{MateriasVer}},

	{SumariosVer, "Consultar sumários", "Ver os sumários de aula.", "Sumários", nil},
	{SumariosGerir, "Gerir sumários", "Criar, editar e eliminar sumários de aula.", "Sumários", []string{SumariosVer}},

	{NotasVer, "Consultar notas", "Ver as notas lançadas pelos professores. Não permite lançar nem corrigir.", "Notas e faltas", nil},
	{FaltasVer, "Consultar faltas", "Ver as faltas lançadas pelos professores. Não permite lançar nem corrigir.", "Notas e faltas", nil},
	{FaltasConfigurar, "Configurar o limite de faltas", "Definir o limite de faltas e a reprovação por faltas.", "Notas e faltas", []string{FaltasVer}},

	{AvaliacoesVer, "Consultar avaliações finais", "Ver avaliações finais, aprovações, reprovações e as regras.", "Avaliações", nil},
	{AvaliacoesConfigurar, "Configurar avaliações", "Criar, editar e eliminar regras de avaliação final e categorias de nota.", "Avaliações", []string{AvaliacoesVer}},

	{ServicosExtrasVer, "Consultar serviços", "Ver o Transporte, as atividades, as inscrições e as pendências.", "Serviços extras", nil},
	{ServicosExtrasGerir, "Configurar serviços", "Configurar o Transporte (rotas e veículos) e as atividades extracurriculares (horários e turmas).", "Serviços extras", []string{ServicosExtrasVer}},
	{ServicosExtrasSolicitacoes, "Decidir inscrições em serviços", "Aprovar, reprovar e cancelar inscrições.", "Serviços extras", []string{ServicosExtrasVer}},

	{FinanceiroVer, "Consultar o financeiro", "Ver cobranças, mensalidades, configurações e o provedor em uso.", "Financeiro", nil},
	{FinanceiroGerir, "Gerir o financeiro", "Criar e cancelar cobranças, configurar mensalidades, anular, reativar e marcar pago fora da plataforma.", "Financeiro", []string{FinanceiroVer}},

	{ComunicacaoVer, "Consultar mensagens", "Ver as mensagens enviadas pela academia.", "Comunicação", nil},
	{ComunicacaoEnviar, "Enviar mensagens", "Enviar SMS em nome da academia.", "Comunicação", []string{ComunicacaoVer}},

	{AnoLetivoGerir, "Gerir o ano letivo", "Definir o ano letivo, os anos académicos e finalizar o ano letivo.", "Ano letivo", nil},

	{ProfessoresVer, "Consultar professores", "Ver professores e os seus vínculos com turmas e disciplinas.", "Professores", nil},
	{ProfessoresGerir, "Gerir professores", "Criar professores, atribuir e reatribuir vínculos, desativar e eliminar.", "Professores", []string{ProfessoresVer}},
}

var indice = func() map[string]Permissao {
	m := make(map[string]Permissao, len(Catalogo))
	for _, p := range Catalogo {
		m[p.Chave] = p
	}
	return m
}()

// Existe diz se a chave pertence ao catálogo.
func Existe(chave string) bool {
	_, ok := indice[chave]
	return ok
}

// Normalizar valida, remove duplicados, acrescenta as permissões implicadas
// (gerir → ver) e devolve a lista ordenada. É esta lista que é gravada: assim a
// decisão da rota é uma simples pertença ao conjunto. Uma chave fora do
// catálogo (por exemplo, "notas.lancar", que nunca existe para secretário) é
// erro: o servidor não aceita permissões que o catálogo não conhece.
func Normalizar(chaves []string) ([]string, error) {
	conjunto := map[string]struct{}{}
	var invalidas []string
	for _, bruta := range chaves {
		chave := strings.TrimSpace(bruta)
		if chave == "" {
			continue
		}
		p, ok := indice[chave]
		if !ok {
			invalidas = append(invalidas, chave)
			continue
		}
		conjunto[p.Chave] = struct{}{}
		for _, impl := range p.Implica {
			conjunto[impl] = struct{}{}
		}
	}
	if len(invalidas) > 0 {
		sort.Strings(invalidas)
		return nil, fmt.Errorf("permissões inválidas: %s", strings.Join(invalidas, ", "))
	}
	saida := make([]string, 0, len(conjunto))
	for c := range conjunto {
		saida = append(saida, c)
	}
	sort.Strings(saida)
	return saida, nil
}

// Todas devolve todas as chaves do catálogo (atalho "todas as permissões").
func Todas() []string {
	saida := make([]string, 0, len(Catalogo))
	for _, p := range Catalogo {
		saida = append(saida, p.Chave)
	}
	sort.Strings(saida)
	return saida
}

// Tem diz se o conjunto concedido cobre a permissão exigida (incluindo as
// implicadas, por segurança caso o conjunto não tenha sido normalizado).
func Tem(concedidas []string, requerida string) bool {
	for _, c := range concedidas {
		if c == requerida {
			return true
		}
		if p, ok := indice[c]; ok {
			for _, impl := range p.Implica {
				if impl == requerida {
					return true
				}
			}
		}
	}
	return false
}
