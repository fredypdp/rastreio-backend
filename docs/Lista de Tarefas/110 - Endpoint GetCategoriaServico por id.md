# Tarefa para o Codex — Repositório `rastreio-backend` (backend)

**Repositório:** https://github.com/fredypdp/rastreio-backend
**Branch base:** main
**Execução:** Não é necessário planejar nada. Todo o código já foi escrito e validado pelo orquestrador (ver "O que já foi validado" abaixo) e está pronto no arquivo `110 - Endpoint GetCategoriaServico por id.patch`, nesta mesma pasta. Sua única tarefa é aplicar o patch, rodar as verificações do Passo 2, e mover/renomear os arquivos conforme o Passo 3.

**Ordem de deploy:** este backend precisa ir para produção **antes** da tarefa de frontend correspondente (que troca a tela de edição de categoria de serviço para chamar esta rota nova). Se o frontend for ao ar primeiro, a tela de edição vai receber 404 da rota inexistente.

---

## Contexto do problema

O usuário notou que a tela de edição de categoria de serviço, no frontend, buscava a lista inteira de categorias e filtrava pelo id no cliente — uma gambiarra para contornar a falta de um endpoint de "buscar uma categoria pelo id". Investiguei e confirmei: **não existia** nenhuma rota `GET /academia/categorias-servico/:id` — só `GET /academia/categorias-servico` (lista completa).

Comparando com o recurso irmão, serviço extra, encontrei que ele **já tinha** esse endpoint (`GET /academia/servicos-extras/:id`, `handlers.GetServicoExtra`) — então a correção aqui é só trazer categoria de serviço para o mesmo padrão que serviço extra já segue, não inventar nada novo.

## O que o patch faz

3 arquivos — nenhuma migration, nenhum campo novo:

1. **`internal/handlers/categoria_servico_handlers.go`** — novo handler `GetCategoriaServico`, espelhando `GetServicoExtra` (`internal/handlers/servico_extra_handlers.go`) linha por linha: busca pela projection (`getCategoriasServicoProjection(c).GetByID(id)` — mais barato que reconstituir o agregado via ledger, que é o que as operações de escrita já fazem via `loadCategoriaServico`), 404 se não existir, 403 se pertencer a outra academia, admin sempre pode ver. Import de `spuri/internal/middleware` adicionado (para `middleware.GetUserType`, já usado por `GetServicoExtra`).
2. **`cmd/server/main.go`** — rota registrada: `academiaRead.GET("/categorias-servico/:id", handlers.GetCategoriaServico)`, ao lado da rota de listagem já existente, no mesmo grupo (`academiaRead`) onde `GET /servicos-extras/:id` já vive.
3. **`internal/handlers/categoria_servico_get_by_id_integration_test.go`** (novo) — `TestIntegrationGetCategoriaServicoRespeitaEscopoDaAcademia`, cobrindo os 3 casos: a academia dona vê a categoria; outra academia recebe 403; um id inexistente recebe 404.

**Detalhe técnico do teste, para quem for mexer nele depois:** o teste grava a categoria direto na tabela da projection (`seedCategoriaServico`), em vez de criar via `CriarCategoriaServico` (o fluxo real). Isso é proposital: `CriarCategoriaServico` só escreve no ledger (event sourcing) e chama `notifyLedgerWritten()`, que acorda o Projection Manager — um worker **assíncrono** que processa esse evento e só então atualiza `projection_categorias_servico`. Um teste isolado do pacote `handlers` (sem subir esse worker) não tem como esperar por isso de forma determinística. Como `GetCategoriaServico` só lê da projection, testar o endpoint não depende de como a linha chegou lá — por isso semear a projection direto é válido e não é um teste mais fraco.

## O que já foi validado pelo orquestrador

Você (Codex) não tem PostgreSQL/Docker neste ambiente — as validações abaixo já foram feitas fora dele, num sandbox com PostgreSQL 16 real.

- **Baseline** (sobre o estado atual do repositório, já com as Tarefas 107–109 aplicadas): `go build ./...`, `go vet ./...` e `go test ./...` — tudo limpo antes de qualquer mudança.
- **Ciclo reverter → falhar → reaplicar**: fiz o handler retornar 404 incondicionalmente e confirmei que o teste falha exatamente no ponto certo (`esperava 200, obteve 404`); reaplicando o handler correto, o teste volta a passar.
- `go test ./...` (suíte inteira) com a mudança aplicada — **todos os pacotes `ok`**, incluindo o teste novo, sem nenhuma regressão em nenhum teste pré-existente.
- **Validação final, independente de tudo isso:** clone novo e limpo de `main` direto do GitHub, apliquei o `.patch` exatamente como você vai aplicar (`git apply`), confirmei que aplica sem conflito, e rodei de novo `go build`, `go vet` e `go test ./...` inteiro contra um banco Postgres recriado do zero — **100% verde**.

## Passo 1 — Aplicar o patch

Na raiz do repositório:

```bash
git apply "docs/Lista de Tarefas/110 - Endpoint GetCategoriaServico por id.patch"
```

Deve alterar 2 arquivos e criar 1 novo. Se `git apply` falhar, PARE — não recrie as mudanças manualmente, reporte o conflito.

## Passo 2 — Verificação de build e testes

```bash
go build ./...
go vet ./...
go test ./...
```

Os três devem terminar sem nenhum erro (`go test` deve mostrar `ok` para todos os pacotes, incluindo `spuri/internal/handlers`, onde está o teste novo).

## O que NÃO fazer (fora de escopo)

- Não crie um endpoint equivalente para nenhum outro recurso além de categoria de serviço — serviço extra já tem o dele (`GetServicoExtra`), e nenhum outro recurso foi mencionado nesta tarefa.
- Não adicione filtro de `deleted_at IS NULL` em `CategoriaServicoProjection.GetByID` — `ServicoExtraProjection.GetByID` (já existente) também não filtra; mudar só um dos dois criaria uma inconsistência nova entre os dois recursos.
- Não mexa em `CriarCategoriaServico`, `AtualizarCategoriaServico`, `DesativarCategoriaServico`/`ReativarCategoriaServico` ou `DeletarCategoriaServico` — nenhum deles precisou mudar.
- A tarefa de frontend que passa a usar esta rota é um documento separado, no `rastreio-frontend` — não implemente nada de frontend aqui.

## Passo 3 — Marcar como feito

Depois que os Passos 1–2 passarem sem problema, mova este arquivo e o `.patch` correspondente de `docs/Lista de Tarefas/` para `docs/Tarefas feitas/`.

## Resumo das mudanças (checklist final)

- [x] Patch aplicado (`git apply`) sem conflitos
- [x] `go build ./...` limpo
- [x] `go vet ./...` limpo
- [x] `go test ./...` — todos os pacotes `ok`, incluindo o teste novo em `spuri/internal/handlers`
- [x] Arquivos desta tarefa movidos para `docs/Tarefas feitas/`
