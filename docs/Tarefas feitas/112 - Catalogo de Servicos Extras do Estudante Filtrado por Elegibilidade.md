# Tarefa para o Codex — Repositório `rastreio-backend` (backend)

**Repositório:** https://github.com/fredypdp/rastreio-backend
**Branch base:** main
**Execução:** Não é necessário planejar nada. Todo o código já foi escrito e validado pelo orquestrador (ver "O que já foi validado" abaixo) e está pronto no arquivo `112 - Catalogo de Servicos Extras do Estudante Filtrado por Elegibilidade.patch`, nesta mesma pasta. Sua única tarefa é aplicar o patch, rodar as verificações do Passo 2, e mover/renomear os arquivos conforme o Passo 3.

**Ordem de deploy:** este backend precisa ir para produção **antes** da tarefa de frontend correspondente (Tarefa 18, no `rastreio-frontend`), que passa a chamar a rota nova descrita abaixo.

---

## Contexto do problema

Foi reportado que `/servicos-extras/catalogo` (tela do estudante) não estava mostrando os serviços disponíveis. Investigando, encontrei **duas causas**, as duas no backend:

1. **Nenhuma filtragem por elegibilidade.** O catálogo do estudante usava a rota pública `GET /academia/servico/:codigo/servicos-extras` (`ListarServicosExtrasPublico`), que devolve todos os serviços ativos da academia, sem considerar o ano/curso do estudante. A elegibilidade (`estudanteElegivelServicoExtra`) só existia num lugar: dentro de `SolicitarServicoExtra`, checada só no momento de aceitar ou rejeitar uma inscrição — nunca usada para decidir o que aparece no catálogo. Resultado: o estudante podia ver (e tentar solicitar) um serviço que a inscrição rejeitaria de qualquer forma.
2. **Uma segunda chamada que sempre falhava para um estudante.** Para mostrar o nome da categoria de cada serviço, o frontend também chamava `GET /academia/categorias-servico`, que exige `RequireAcademiaOuAdmin` — **rejeita qualquer estudante autenticado com 403**. Como as duas chamadas do frontend eram feitas em paralelo sem tratamento de erro, essa falha silenciosa derrubava a tela inteira (nenhum serviço aparecia, mesmo os elegíveis).

## O que o patch faz

5 arquivos — nenhuma migration, nenhum campo novo no banco.

- **`internal/handlers/servico_extra_handlers.go`**:
  - Nova função `elegivelParaServicoExtra(serv, est) bool`, logo após `estudanteElegivelServicoExtra`: encapsula a regra "sem nenhuma restrição = disponível para todos; havendo qualquer restrição, decide `estudanteElegivelServicoExtra`" — antes essa regra só existia inline dentro de `SolicitarServicoExtra`.
  - Novo handler `ListarServicosExtrasCatalogoEstudante`, logo após `ListarServicosExtrasPublico`: busca o estudante autenticado (`middleware.GetUserID` + `getEstudanteProjection`), filtra os serviços ativos da academia com `elegivelParaServicoExtra`, e busca as categorias da academia **diretamente na projeção** (não pela rota `/academia/categorias-servico`, que rejeitaria o estudante) — devolve `{servicos_extras, categorias_servico, total}` numa resposta só.
- **`internal/handlers/servico_extra_solicitacao_handlers.go`** — `SolicitarServicoExtra` passou a chamar `elegivelParaServicoExtra` em vez de repetir a mesma condição inline. Comportamento idêntico; elimina a duplicação que permitiu o catálogo e a inscrição divergirem no passado.
- **`cmd/server/main.go`** — nova rota: `estudante.GET("/servicos-extras/catalogo", handlers.ListarServicosExtrasCatalogoEstudante)`, no grupo que já exige autenticação de estudante.
- **`internal/handlers/servico_extra_elegibilidade_test.go`** (novo) — 8 casos de teste unitário (sem banco) para `elegivelParaServicoExtra`: sem restrição, ano fundamental certo/errado, curso médio certo/errado, curso superior certo, e um caso de fronteira confirmando que o id do curso já basta para diferenciar entre médio e superior (não há como um mesmo id ser ambos).
- **`internal/handlers/servico_extra_catalogo_estudante_integration_test.go`** (novo) — `TestIntegrationListarServicosExtrasCatalogoEstudanteFiltraPorElegibilidade`, com Postgres real: cria 1 estudante do 7º ano fundamental e 3 serviços (um elegível, um de outro ano, um sem restrição) e confirma que o catálogo devolve exatamente os 2 que deveriam aparecer — mais a categoria resolvida corretamente na mesma resposta.

## O que já foi validado pelo orquestrador

- **Baseline** (sobre o estado atual do repositório, já com as Tarefas 107–111 aplicadas): `go build`, `go vet`, `go test ./...` e `go test -race ./...` — tudo limpo antes de qualquer mudança, numa base de dados recriada do zero.
- `go build`, `go vet`, `gofmt -l .` e `go test ./...` (suíte inteira) com a mudança aplicada, numa base de dados recriada do zero — todos os pacotes `ok`, incluindo os 2 arquivos de teste novos, sem nenhuma regressão.
- **Validação final, independente de tudo isso:** clone novo e limpo de `main` direto do GitHub, apliquei o `.patch` exatamente como você vai aplicar (`git apply`), confirmei que aplica sem conflito, e rodei de novo `gofmt`, `go build`, `go vet` e `go test ./...` inteiro (base de dados recriada do zero) — 100% verde.

## Passo 1 — Aplicar o patch

Na raiz do repositório:

```bash
git apply "docs/Lista de Tarefas/112 - Catalogo de Servicos Extras do Estudante Filtrado por Elegibilidade.patch"
```

Deve alterar 3 arquivos e criar 2 novos. Se `git apply` falhar, PARE — não recrie as mudanças manualmente, reporte o conflito.

## Passo 2 — Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

Os quatro devem terminar sem nenhum erro. Se você rodar `go test ./...` mais de uma vez seguida com Postgres real disponível, recrie a base entre execuções (bases acumuladas de execuções anteriores já causaram falhas de outros testes não relacionados nesta investigação).

## O que NÃO fazer (fora de escopo)

- Não adicione filtro de elegibilidade a `ListarServicosExtrasPublico` — essa rota é pública, sem estudante autenticado, e continua sendo usada assim (contexto institucional/marketing) em outro lugar do app.
- Não adicione o nome da categoria (`categoria_nome` ou similar) direto no struct `ServicoExtraDTO` — isso afetaria todo mundo que usa esse DTO (telas de academia/admin). A resolução de categoria para o catálogo do estudante fica só na resposta de `ListarServicosExtrasCatalogoEstudante`, como o patch já faz.
- Não crie uma rota equivalente para nenhuma outra entidade (matérias, turmas etc.) — só serviços extras foram reportados.
- As correções de frontend que passam a usar esta rota são um documento separado (Tarefa 18, no `rastreio-frontend`) — não implemente nada de frontend aqui.

## Passo 3 — Marcar como feito

Depois que os Passos 1–2 passarem sem problema, mova este arquivo e o `.patch` correspondente de `docs/Lista de Tarefas/` para `docs/Tarefas feitas/`.

## Resumo das mudanças (checklist final)

- [x] Patch aplicado (`git apply`) sem conflitos
- [x] `gofmt -l .` sem nenhuma saída
- [x] `go build ./...` limpo
- [x] `go vet ./...` limpo
- [x] `go test ./...` — todos os pacotes `ok`, incluindo os testes novos
- [x] Arquivos desta tarefa movidos para `docs/Tarefas feitas/`
