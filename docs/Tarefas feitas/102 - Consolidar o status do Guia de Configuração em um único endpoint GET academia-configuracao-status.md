---
criado: 2026-09-14
origem: Claude (orquestrador) — pré-testado com PostgreSQL real (134 migrations aplicadas, queries novas rodadas contra dados reais), comparação campo a campo com a lógica original do front end (`buildSteps`, 8 cenários) e type-check completo (`go build`/`go vet`, zero erros) via stubs fiéis às structs/métodos reais do repositório, antes de virar tarefa para o Codex.
status: feito
---

# Consolidar o status do Guia de Configuração em um único endpoint `GET /academia/configuracao-status`

**Repositório:** https://github.com/fredypdp/rastreio-backend
**Branch base:** main

## Prompt recomendado para executar esta tarefa

Implemente, no repositório `rastreio-backend`, exatamente o código descrito neste documento: um arquivo novo (`internal/handlers/configuracao_status_handlers.go`) e uma linha nova de rota em `cmd/server/main.go`. O desenho já foi decidido e pré-validado por Claude (orquestrador) numa sessão anterior — ver "Nota de validação" abaixo. Não é necessário planejar nada: todo o código está especificado exatamente como deve ficar. Sua responsabilidade é aplicar os Passos 1–5 na ordem, sem improvisar caso algo não bata 100% com o repositório real (nesse caso, **pare e reporte a diferença**).

## Contexto do problema

O painel da academia tem um "Guia de Configuração" (front end, componente `GuiaConfiguracoesSection.tsx` + hook `useAcademiaConfiguracaoStatus.ts`, repositório `rastreio-frontend`) que mostra 9 passos (verificar e-mail, definir ano letivo, criar cursos, criar matérias, etc.) e marca quais já foram concluídos.

Hoje, para saber isso, o front end faz **até 8 chamadas HTTP em paralelo** (`GET /academia/ano-letivo`, `/academia/anos-academicos`, `/academia/cursos`, `/academia/materias`, `/academia/turmas`, `/estudantes`, `/academia/categorias-nota`, `/academia/avaliacao-final/regras`) e só depois calcula no navegador quais passos estão completos. Uma dessas chamadas (`/estudantes`) ainda pagina internamente e pode virar dezenas de chamadas para academias com muitos estudantes.

**Objetivo desta tarefa:** criar **um único endpoint GET**, `GET /academia/configuracao-status`, que já devolve pronto o que está completo e o que falta em cada passo, para o front end (tarefa separada, repositório `rastreio-frontend`) parar de fazer essas 8 chamadas e passar a fazer só esta.

**Esta tarefa mexe SOMENTE no backend.** Não altere nada no repositório `rastreio-frontend`.

---

## Nota de validação (já realizada por Claude, o orquestrador, ANTES desta tarefa)

Todo o código abaixo já foi verificado da seguinte forma, para que você não precise reinvestigar nada disso:

1. **Schema real do banco**: as 134 migrations de `migrations/` foram aplicadas, em ordem, contra um PostgreSQL 16 real. Todas aplicaram sem erro. As colunas usadas nas queries abaixo (`projection_estudantes.ano_escolar_fundamental`, `ano_escolar_medio`, `curso_medio_id`, `ano_superior`, `curso_superior_id`, `status`; `projection_regras_avaliacao_final.status`) foram conferidas contra o schema real resultante (`\d nome_da_tabela`), não contra a migration `001` isolada (que está desatualizada em relação ao schema atual).
2. **As duas queries SQL novas** (agregação de estudantes por nível e contagem de regras ativas, ambas no Passo 1 abaixo) foram executadas contra dados reais inseridos manualmente nesse Postgres de teste (uma academia nível "escola"/"medio" com curso, matéria, turma e estudantes; uma academia "superior" com regras ativa/inativa) e retornaram os valores esperados.
3. **A lógica de negócio** (quais critérios tornam cada passo "completo") foi extraída da função `buildSteps` real do front end (`src/hooks/useAcademiaConfiguracaoStatus.ts`), rodada em Node.js contra 8 cenários (fundamental, médio, misto, superior, 4º ano médio, itens inativos, estado vazio), e comparada campo a campo com a tradução Go abaixo. **Todos os campos bateram exatamente** — incluindo uma particularidade do comportamento atual que o código abaixo replica de propósito, não corrige: o passo "regras-superiores" hoje é considerado completo com **qualquer** regra ativa (não só regras de nível "superior"), porque o campo `type` de uma regra está sempre preenchido.
4. **Compilação/tipagem**: como o ambiente usado para esta validação não teve acesso de rede a alguns domínios do ecossistema Go (não é uma limitação de Docker/psql — é o allowlist de rede do meu sandbox que não cobre `go.opentelemetry.io`, `golang.org/x/*` e alguns módulos transitivos mais raros), não foi possível rodar `go build ./...` **dentro do módulo real**. Para compensar isso, o handler abaixo foi colado, sem nenhuma alteração de lógica, em um módulo Go local com *stubs* que reproduzem exatamente as assinaturas reais de `academiaAutenticada`, `getCursosProjection`, `getMateriasProjection`, `getTurmasProjection`, `getCategoriasNotaProjection`, `getDbClient` e as structs reais `AcademiaDTO`, `CursoDTO`, `MateriaDTO`, `TurmaDTO`, `CategoriaNotaDTO` (mesmos nomes de campo, mesmos tipos ponteiro). Esse módulo local compilou (`go build ./...`) e passou em `go vet ./...` sem nenhum erro.
   → **Por isso o Passo 4 abaixo (rodar `go build`/`go vet` no repositório de verdade) não é opcional**: é a confirmação final, dentro do módulo real, do que já foi checado em isolamento.

---

## Passo 1 — Criar o arquivo `internal/handlers/configuracao_status_handlers.go`

Crie um arquivo novo com exatamente este conteúdo:

```go
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"spuri/internal/projections"
	"spuri/internal/utils"
)

// ============================================================================
// GET /academia/configuracao-status
//
// Consolida em UMA única consulta tudo que o "Guia de Configuração" do painel
// da academia (front end: GuiaConfiguracoesSection.tsx via o hook
// useAcademiaConfiguracaoStatus.ts) precisa saber sobre o que já foi
// configurado e o que ainda falta.
//
// Antes desta rota, o front end fazia até 8 chamadas em paralelo (ano letivo,
// anos acadêmicos, cursos, matérias, turmas, estudantes, categorias de nota e
// regras de avaliação final) e calculava tudo no navegador. Esta rota faz o
// mesmo cálculo aqui no back end e devolve apenas os booleanos (e algumas
// contagens) já prontos, na MESMA semântica usada hoje pelo front end,
// incluindo as mesmas particularidades:
//
//   - o 4º ano médio é ignorado na cobertura de matérias/turmas
//     (replicando `ignoreFourthYearMedio` do front end);
//   - "regras-superiores" é considerado completo com QUALQUER regra ativa
//     (não só regras com nivel="superior"), pois o campo "type" de uma regra
//     é sempre preenchido — isso replica fielmente o comportamento atual do
//     front end, não é uma correção de uma eventual inconsistência dele;
//   - estudantes com status='deletado' nunca contam, igual ao que
//     GET /estudantes já faz hoje.
//
// O passo "email-verificacao" do guia NÃO é incluído aqui de propósito: ele já
// é derivado, no front end, de dados que o usuário logado já tem em memória
// (user.academia.email / email_verificado, vindos do contexto de sessão), sem
// precisar de nenhuma chamada de API — não há nada para mover ao back end
// nesse passo específico.
// ============================================================================
func GetConfiguracaoStatusAcademia(c *gin.Context) {
	academiaDTO, ok := academiaAutenticada(c)
	if !ok {
		return
	}

	nivel := academiaDTO.Nivel
	nivelEscolar := ""
	if academiaDTO.NivelEscolar != nil {
		nivelEscolar = *academiaDTO.NivelEscolar
	}

	isFundamental := nivel == "escola" && (nivelEscolar == "fundamental" || nivelEscolar == "misto")
	needsCourses := nivel == "superior" || (nivel == "escola" && (nivelEscolar == "medio" || nivelEscolar == "misto"))
	isSuperior := nivel == "superior"

	// ano-letivo já está disponível na própria academiaDTO, sem consulta
	// extra (equivalente a GET /academia/ano-letivo).
	anoLetivoCompleto := academiaDTO.AnoLetivo != nil && *academiaDTO.AnoLetivo != ""

	// cursos ativos — só é buscado quando o nível exige cursos, replicando a
	// mesma condição que hoje decide, no front end, se
	// academiaService.listarCursos(token) é chamado ou não.
	var cursosAtivos []projections.CursoDTO
	if needsCourses {
		cursos, err := getCursosProjection(c).GetByAcademia(academiaDTO.CodigoAcademia)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		for _, curso := range cursos {
			if isStatusAtivoOuVazio(curso.Status) {
				cursosAtivos = append(cursosAtivos, curso)
			}
		}
	}

	// matérias ativas (sempre buscadas: usadas no passo "materias" mesmo
	// quando needsCourses é falso, para cobertura do fundamental).
	materiasTodas, err := getMateriasProjection(c).GetByAcademia(academiaDTO.CodigoAcademia)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	var materiasAtivas []projections.MateriaDTO
	for _, m := range materiasTodas {
		if isStatusAtivoOuVazio(m.Status) {
			materiasAtivas = append(materiasAtivas, m)
		}
	}

	// turmas ativas (sempre buscadas: usadas em "turmas" e
	// "estudantes-turmas").
	turmasTodas, err := getTurmasProjection(c).GetByAcademia(academiaDTO.CodigoAcademia)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	var turmasAtivas []projections.TurmaDTO
	for _, t := range turmasTodas {
		if isStatusAtivoOuVazio(t.Status) {
			turmasAtivas = append(turmasAtivas, t)
		}
	}

	fundamentalYears := academiaDTO.AnosAcademicos

	hasFundamentalCoverage := func(pred func(year string) bool) bool {
		if !isFundamental {
			return true
		}
		if len(fundamentalYears) == 0 {
			return false
		}
		for _, y := range fundamentalYears {
			if !pred(y) {
				return false
			}
		}
		return true
	}

	hasCourseCoverage := func(pred func(curso projections.CursoDTO, year string) bool, ignoreFourthYearMedio bool) bool {
		if !needsCourses {
			return true
		}
		if len(cursosAtivos) == 0 {
			return false
		}
		for _, curso := range cursosAtivos {
			years := make([]string, 0, len(curso.AnosAcademicos))
			for _, y := range curso.AnosAcademicos {
				if ignoreFourthYearMedio && curso.Type == "medio" && y == "4_ano_medio" {
					continue
				}
				years = append(years, y)
			}
			if len(years) == 0 {
				return false
			}
			for _, y := range years {
				if !pred(curso, y) {
					return false
				}
			}
		}
		return true
	}

	materiaComplete := hasFundamentalCoverage(func(year string) bool {
		for _, m := range materiasAtivas {
			if m.Type == "fundamental" && contemAno(m.AnosAcademicos, year) {
				return true
			}
		}
		return false
	}) && hasCourseCoverage(func(curso projections.CursoDTO, year string) bool {
		if curso.Type == "superior" {
			if len(curso.Periodos) == 0 {
				return false
			}
			for _, periodo := range curso.Periodos {
				encontrada := false
				for _, m := range materiasAtivas {
					if m.Type == "superior" && m.CursoID != nil && *m.CursoID == curso.ID &&
						m.Periodo != nil && *m.Periodo == periodo && contemAno(m.AnosAcademicos, year) {
						encontrada = true
						break
					}
				}
				if !encontrada {
					return false
				}
			}
			return true
		}
		for _, m := range materiasAtivas {
			if m.Type == "medio" && m.CursoID != nil && *m.CursoID == curso.ID && contemAno(m.AnosAcademicos, year) {
				return true
			}
		}
		return false
	}, true)

	turmaComplete := hasFundamentalCoverage(func(year string) bool {
		for _, t := range turmasAtivas {
			if t.CursoID == nil && t.Nivel == year {
				return true
			}
		}
		return false
	}) && hasCourseCoverage(func(curso projections.CursoDTO, year string) bool {
		for _, t := range turmasAtivas {
			if t.CursoID != nil && *t.CursoID == curso.ID && t.Nivel == year {
				return true
			}
		}
		return false
	}, true)

	estudantesTurmasComplete := hasFundamentalCoverage(func(year string) bool {
		for _, t := range turmasAtivas {
			if t.CursoID == nil && t.Nivel == year && len(t.Estudantes) > 0 {
				return true
			}
		}
		return false
	}) && hasCourseCoverage(func(curso projections.CursoDTO, year string) bool {
		for _, t := range turmasAtivas {
			if t.CursoID != nil && *t.CursoID == curso.ID && t.Nivel == year && len(t.Estudantes) > 0 {
				return true
			}
		}
		return false
	}, true)

	// estudantes: contagem + existência por nível via agregação no banco, em
	// vez de trazer a lista inteira de estudantes (que hoje pode disparar
	// dezenas de chamadas paginadas no front end para academias grandes).
	// Replica exatamente a mesma exclusão de deletados que GET /estudantes já
	// aplica.
	var totalEstudantes, temFundamental, temMedio, temSuperior int
	err = getDbClient(c).DB().QueryRow(`
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE NULLIF(ano_escolar_fundamental, '') IS NOT NULL),
			COUNT(*) FILTER (WHERE NULLIF(ano_escolar_medio, '') IS NOT NULL OR curso_medio_id IS NOT NULL),
			COUNT(*) FILTER (WHERE NULLIF(ano_superior, '') IS NOT NULL OR curso_superior_id IS NOT NULL)
		FROM projection_estudantes
		WHERE codigo_academia = $1 AND status <> 'deletado'
	`, academiaDTO.CodigoAcademia).Scan(&totalEstudantes, &temFundamental, &temMedio, &temSuperior)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}

	var estudantesCompleto bool
	if isSuperior {
		estudantesCompleto = temSuperior > 0
	} else {
		fundamentalOK := (nivelEscolar != "fundamental" && nivelEscolar != "misto") || temFundamental > 0
		medioOK := (nivelEscolar != "medio" && nivelEscolar != "misto") || temMedio > 0
		estudantesCompleto = fundamentalOK && medioOK
	}

	steps := gin.H{
		"ano-letivo": gin.H{"completed": anoLetivoCompleto},
		"materias":   gin.H{"completed": materiaComplete},
		"turmas":     gin.H{"completed": turmaComplete},
		"estudantes": gin.H{
			"completed": estudantesCompleto,
			"total":     totalEstudantes,
		},
		"estudantes-turmas": gin.H{"completed": estudantesTurmasComplete},
	}

	if needsCourses {
		steps["cursos"] = gin.H{
			"completed":    len(cursosAtivos) > 0,
			"total_ativos": len(cursosAtivos),
		}
	}

	if isSuperior {
		categoriasAtivas, err := getCategoriasNotaProjection(c).ListarPorAcademia(academiaDTO.CodigoAcademia)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		categoriasCompleto := hasCourseCoverage(func(_ projections.CursoDTO, year string) bool {
			for _, cat := range categoriasAtivas {
				if contemAno(cat.AnosAcademicos, year) {
					return true
				}
			}
			return false
		}, false)

		var totalRegrasAtivas int
		if err := getDbClient(c).DB().QueryRow(`
			SELECT COUNT(*) FROM projection_regras_avaliacao_final
			WHERE codigo_academia = $1 AND status = 'ativo'
		`, academiaDTO.CodigoAcademia).Scan(&totalRegrasAtivas); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}

		steps["categorias-superiores"] = gin.H{"completed": categoriasCompleto}
		steps["regras-superiores"] = gin.H{
			"completed":    totalRegrasAtivas > 0,
			"total_ativas": totalRegrasAtivas,
		}
	}

	c.JSON(http.StatusOK, gin.H{"steps": steps})
}

func isStatusAtivoOuVazio(status string) bool { return status == "ativo" || status == "" }

func contemAno(anos []string, ano string) bool {
	for _, a := range anos {
		if a == ano {
			return true
		}
	}
	return false
}
```

**Não renomeie nenhuma função, variável ou struct deste arquivo.** Os nomes `academiaAutenticada`, `getCursosProjection`, `getMateriasProjection`, `getTurmasProjection`, `getCategoriasNotaProjection`, `getDbClient`, `projections.CursoDTO`, `projections.MateriaDTO`, `projections.TurmaDTO`, `projections.CategoriaNotaDTO` já existem no repositório (em `internal/handlers/helpers.go`, `internal/handlers/anos_academicos_handlers.go` e `internal/projections/*.go`) e foram conferidos um a um contra o código-fonte real antes de escrever este arquivo.

---

## Passo 2 — Registrar a rota em `cmd/server/main.go`

Localize, em `cmd/server/main.go`, o grupo de rotas `academiaRead` (ele já contém, entre outras, a linha `academiaRead.GET("/avaliacao-final/regras", handlers.ListarRegrasAvaliacaoFinal)`). Adicione a nova rota dentro desse mesmo grupo, por exemplo logo após essa linha:

```go
academiaRead.GET("/configuracao-status", handlers.GetConfiguracaoStatusAcademia)
```

**Importante:** a rota precisa ficar dentro do grupo `academiaRead` (o que já tem `middleware.RequireAcademiaOuAdmin()` e `middleware.ValidarStatusAcademia()` aplicados), não em outro grupo. Se a linha `/avaliacao-final/regras` não existir exatamente assim, procure o grupo correto pelo nome da variável `academiaRead` e pelo middleware aplicado a ela, e insira a nova linha dentro dele mesmo assim.

---

## Passo 3 — Conferir que não há colisão de nomes

Rode, na raiz do repositório:

```bash
grep -rn "func isStatusAtivoOuVazio\|func contemAno\|func GetConfiguracaoStatusAcademia" internal/handlers/*.go
```

O resultado esperado é que cada uma das três funções apareça **exatamente uma vez** (no arquivo novo criado no Passo 1). Se alguma aparecer em mais de um lugar, já existe uma função com esse nome no pacote `handlers` — pare e reporte antes de prosseguir (provavelmente vai precisar renomear só a função nova, mantendo a lógica idêntica).

---

## Passo 4 — Build, vet e testes (a validação final que eu não consegui fazer)

Rode, na raiz do repositório:

```bash
go build ./...
go vet ./...
```

Ambos devem passar sem nenhum erro. Se algum erro aparecer, ele provavelmente é de um destes dois tipos:

- **Nome de campo ou de método diferente do esperado** (por exemplo, se `CategoriaNotaDTO` não tiver um campo `AnosAcademicos` com esse nome exato) — nesse caso, ajuste **apenas o nome** no arquivo novo para bater com o que existe de verdade no repositório, sem mudar a lógica.
- **Import não utilizado ou faltando** — ajuste conforme o erro indicar.

Se o erro for de outra natureza (por exemplo, a função `academiaAutenticada` não existir mais, ou ter uma assinatura diferente), **pare e reporte** — não invente uma solução alternativa sem confirmação.

Depois, rode os testes existentes do pacote de handlers (não deve haver nenhum teste quebrado, já que nenhum código existente foi alterado, só adicionado):

```bash
go test ./internal/handlers/... ./cmd/server/...
```

---

## Passo 5 — Teste manual do endpoint (opcional, mas recomendado)

Se você tiver como subir a aplicação com um banco de dados de teste (o que eu não sei se é possível no seu ambiente), suba o servidor, faça login como uma academia de teste e chame:

```bash
curl -H "Authorization: Bearer SEU_TOKEN_AQUI" http://localhost:PORTA/academia/configuracao-status
```

A resposta esperada tem este formato (os campos `cursos`, `categorias-superiores` e `regras-superiores` só aparecem quando fazem sentido para o nível da academia — veja a tabela abaixo):

```json
{
  "steps": {
    "ano-letivo": { "completed": true },
    "cursos": { "completed": true, "total_ativos": 2 },
    "materias": { "completed": false },
    "turmas": { "completed": false },
    "estudantes": { "completed": true, "total": 1 },
    "estudantes-turmas": { "completed": false }
  }
}
```

| Chave                     | Quando aparece                                              |
|---------------------------|--------------------------------------------------------------|
| `ano-letivo`               | Sempre                                                        |
| `cursos`                   | `nivel="superior"` OU (`nivel="escola"` E `nivel_escolar` em `medio`/`misto`) |
| `materias`                 | Sempre                                                        |
| `categorias-superiores`    | Só quando `nivel="superior"`                                  |
| `regras-superiores`        | Só quando `nivel="superior"`                                  |
| `turmas`                   | Sempre                                                        |
| `estudantes`               | Sempre                                                        |
| `estudantes-turmas`        | Sempre                                                        |

Se você não tiver ambiente para subir o servidor/banco, não se preocupe — o Passo 4 (`go build`/`go vet`/`go test`) já é a validação obrigatória desta tarefa. O teste manual aqui é só um bônus caso seu ambiente permita.

---

## O que NÃO fazer (fora de escopo)

- **Não altere nenhum arquivo do repositório `rastreio-frontend`.** Essa parte é uma tarefa separada.
- **Não altere nenhum handler existente** (`ListarCursos`, `ListarMaterias`, `ListarTurmasAcademia`, `ListarCategoriasNota`, `ListarRegrasAvaliacaoFinal`, `GetAnoLetivoAcademia`, `ListarEstudantes` etc.). O endpoint novo só **lê** dados através dos mesmos métodos de projeção que esses handlers já usam — nenhum deles precisa mudar.
- **Não "corrija" a regra de `regras-superiores`** (qualquer regra ativa conta, não só as de `nivel="superior"`). Isso é intencional: é para replicar o comportamento atual do front end, não uma correção de bug. Se você achar que é um bug, reporte separadamente — não altere nesta tarefa.
- **Não adicione autenticação/autorização diferente** da que já vem do grupo `academiaRead` (não crie um novo middleware, não mude o middleware do grupo).
- **Não crie um endpoint POST/PUT/PATCH.** É estritamente GET, somente leitura.
- **Não remova nem modifique nenhuma das 8 rotas antigas** (`/academia/ano-letivo`, `/academia/anos-academicos`, `/academia/cursos`, `/academia/materias`, `/academia/turmas`, `/estudantes`, `/academia/categorias-nota`, `/academia/avaliacao-final/regras`). Elas continuam existindo e sendo usadas por outras partes do sistema — esta tarefa só **adiciona** uma rota nova, não remove as antigas.

---

## Resumo das mudanças (checklist final)

- [x] Arquivo novo `internal/handlers/configuracao_status_handlers.go` criado com o conteúdo exato do Passo 1
- [x] Rota `academiaRead.GET("/configuracao-status", handlers.GetConfiguracaoStatusAcademia)` adicionada em `cmd/server/main.go`, dentro do grupo `academiaRead`
- [x] `grep` do Passo 3 confirma que não há nomes duplicados
- [x] `go build ./...` passa sem erros
- [x] `go vet ./...` passa sem erros
- [x] `go test ./internal/handlers/... ./cmd/server/...` passa sem quebrar nenhum teste existente
- [x] Nenhum arquivo do repositório `rastreio-frontend` foi tocado
- [x] Nenhuma rota antiga foi removida ou alterada
- [x] Commit com mensagem sugerida: `feat: adiciona GET /academia/configuracao-status para consolidar o status do guia de configuração`
