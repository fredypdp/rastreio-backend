# Tarefa para o Codex — Repositório `rastreio-backend` (backend)

**Repositório:** https://github.com/fredypdp/rastreio-backend
**Branch base:** main
**Execução:** Não é necessário planejar nada. Todo o código desta tarefa já foi escrito e validado pelo orquestrador (ver "O que já foi validado" abaixo) e está pronto no arquivo `107 - Deletar Categoria de Servico e Servico Extra.patch`, nesta mesma pasta. Sua única tarefa é aplicar o patch, rodar as verificações do Passo 2, e mover/renomear o arquivo conforme o Passo 4. Se o patch não aplicar de primeira (`git apply` reclamar de conflito), PARE e reporte a diferença em vez de tentar recriar as mudanças manualmente.

---

## Contexto do problema

No painel de "Serviços Extras" da academia, **Categoria de Serviço** e **Serviço Extra** só podiam ser criados, editados e ativados/desativados — nunca deletados, nem pelo front end nem pelo back end. Confirmei isso diretamente nas rotas (`cmd/server/main.go`): não existia nenhum `DELETE` registado para `/academia/categorias-servico/:id` nem para `/academia/servicos-extras/:id`.

O padrão já estabelecido neste repositório para deleção lógica e auditável (nunca remove a linha do banco, só marca como deletada e mantém o evento no ledger) é o `Curso.Deletar()` em `internal/domain/aggregates/curso.go`, criado na Tarefa 74 ("Mecanismo de deleção auditável"). Este patch replica exatamente esse padrão para `CategoriaServico` e `ServicoExtra`.

**Regra de negócio (igual à de Curso):** só é possível deletar uma categoria/serviço que já esteja **inativo** (desative antes de deletar). Além disso:
- Uma **Categoria de Serviço** só pode ser deletada se **nenhum serviço** (ativo ou inativo, mas não deletado) ainda estiver vinculado a ela — deletar a categoria não reatribui nem limpa esse vínculo automaticamente, então o vínculo precisa ser desfeito primeiro (editando o serviço para trocar/remover a categoria, ou deletando o serviço).
- Um **Serviço Extra** só pode ser deletado se não houver nenhuma solicitação de estudante com status `pendente`, `aprovada_pendente_pagamento_taxa_inscricao` ou `vinculada` nele.

## O que o patch faz

Um único evento novo por aggregate (`CategoriaServicoDeletada` e `ServicoExtraDeletado`), seguindo o fluxo padrão aggregate → projeção → handler → rota:

1. **`internal/domain/aggregates/categoria_servico.go`** — campos `Deletado bool`/`DeletedAt *time.Time`, evento `CategoriaServicoDeletadaEvent`, método `Deletar(deletadoPor uuid.UUID, motivo string) error` (rejeita se já deletada ou ainda ativa), `applyDeletada`.
2. **`internal/domain/aggregates/servico_extra.go`** — mesma coisa para `ServicoExtra` (`ServicoExtraDeletadoEvent`, `Deletar`, `applyDeletado`).
3. **`internal/domain/aggregates/categoria_servico_test.go`** e **`servico_extra_test.go`** — testes novos (`TestCategoriaServicoDeletar`, `TestServicoExtraDeletar`) cobrindo: rejeição se ativo, sucesso após desativar, rejeição se já deletado, `DeletedAt` preenchido.
4. **`internal/projections/categoria_servico_projection.go`** — trata o novo evento (`deleted()`), e `GetByAcademia` passa a filtrar `deleted_at IS NULL` (categorias deletadas somem da listagem, mas `GetByID` continua enxergando, igual ao padrão de Curso).
5. **`internal/projections/servico_extra_projection.go`** — mesma coisa, mais o novo método `CountByCategoria(categoriaID uuid.UUID) (int, error)`, usado para bloquear a deleção de uma categoria com serviços ainda vinculados.
6. **`internal/projections/solicitacao_servico_extra_projection.go`** — novo método `CountAtivasPorServico(servicoID uuid.UUID) (int, error)`, usado para bloquear a deleção de um serviço com inscrições ativas.
7. **`internal/handlers/categoria_servico_handlers.go`** — handler `DeletarCategoriaServico` (checa vínculos via `CountByCategoria` antes de chamar `cat.Deletar`).
8. **`internal/handlers/servico_extra_handlers.go`** — handler `DeletarServicoExtra` (checa inscrições via `CountAtivasPorServico` antes de chamar `s.Deletar`).
9. **`cmd/server/main.go`** — novas rotas `DELETE /academia/categorias-servico/:id` e `DELETE /academia/servicos-extras/:id`. Ambas aceitam um corpo JSON opcional `{"motivo": "..."}` (texto livre, para auditoria) e respondem `{"message": "..."}` em caso de sucesso.
10. **`internal/db/safe_queries.go`** — os dois novos tipos de evento (`CategoriaServicoDeletada`, `ServicoExtraDeletado`) adicionados à allow-list de eventos válidos do ledger.
11. **`migrations/128_deletar_categoria_servico_e_servico_extra.sql`** — adiciona a coluna `deleted_at TIMESTAMPTZ NULL` em `projection_categorias_servico` e `projection_servicos_extras`.

## O que já foi validado pelo orquestrador

Você (Codex) **não tem acesso a `apt`, Docker nem `psql`** neste ambiente — por isso as validações abaixo, que exigem um PostgreSQL de verdade, já foram feitas fora do seu ambiente, num sandbox com PostgreSQL 16 real instalado. Resultado: **positivo em tudo**, nenhum ajuste foi necessário depois desses testes.

- `go build ./...` e `go vet ./...` — limpos, sem erros.
- `go test ./...` (suite inteira do projeto, não só os pacotes tocados) — todos os pacotes passam, incluindo os dois testes novos.
- Rodei as 136 migrations (as 127 já existentes + a `128` nova) do zero contra um Postgres real — todas aplicam sem erro, e confirmei por `\d` que as colunas `deleted_at` foram criadas nas duas tabelas.
- Escrevi e rodei um teste de integração ponta-a-ponta contra esse Postgres real (não incluído no patch — era só uma ferramenta de validação, já removida): criei uma categoria e um serviço vinculado a ela de verdade (aggregate → `SaveWithAudit` → ledger → projeção assíncrona via `Manager.StartProcessing`/`Wake`, o mesmo caminho que a API usa), e confirmei que:
  - `CountByCategoria` detecta o serviço vinculado (bloquearia a deleção da categoria).
  - Depois de desativar e deletar o serviço, `CountByCategoria` volta a 0 e o serviço some de `GetByAcademia`.
  - A categoria só then consegue ser deletada, e some de `GetByAcademia`.
  - Inserindo uma solicitação com status `vinculada` para um segundo serviço, `CountAtivasPorServico` a detecta corretamente (bloquearia a deleção do serviço).
  - `Deletar()` chamado num serviço ainda ativo é rejeitado, mesmo com tudo isso rodando contra o banco real.

Ou seja: o caminho aggregate → ledger → projeção → allow-list de eventos → filtro de listagem foi exercitado de ponta a ponta com dados reais, não só testado em memória.

## Passo 1 — Aplicar o patch

Na raiz do repositório:

```bash
git apply "docs/Lista de Tarefas/107 - Deletar Categoria de Servico e Servico Extra.patch"
```

Isso deve alterar exatamente 11 arquivos e criar a migration nova, conforme listado acima. Se o `git apply` falhar, PARE — não recrie as mudanças manualmente, reporte o conflito.

## Passo 2 — Verificação de build e testes

Rode, na raiz do repositório:

```bash
go build ./...
go vet ./...
go test ./...
```

Os três devem terminar sem nenhum erro (o `go test` deve mostrar `ok` para todos os pacotes, incluindo `spuri/internal/domain/aggregates`, onde estão os dois testes novos). Como você não tem PostgreSQL neste ambiente, não é preciso (nem possível) repetir a validação com banco real — ela já foi feita, ver seção anterior.

## Passo 3 — Conferir a allow-list de eventos

Rode:

```bash
grep -n "CategoriaServicoDeletada\|ServicoExtraDeletado" internal/db/safe_queries.go
```

Deve mostrar as duas linhas novas (`"CategoriaServicoDeletada": true` e `"ServicoExtraDeletado": true`) dentro do mesmo mapa onde já estão `CategoriaServicoCriada`, `ServicoExtraCriado`, etc.

## O que NÃO fazer (fora de escopo)

- Não alterar a regra de negócio: deleção continua exigindo que a categoria/serviço já esteja **inativo** — não remova essa checagem para "facilitar".
- Não fazer a deleção de uma categoria cascatear para os serviços vinculados a ela (nem automaticamente reatribuir a categoria desses serviços) — a checagem de `CountByCategoria` deve continuar **bloqueando**, não contornando.
- Não alterar o comportamento de `GetByID` das duas projeções (ele continua enxergando registos deletados, de propósito — só `GetByAcademia` filtra).
- Não tocar em nada relacionado à Tarefa 106 (upload de documento extra / consulta pública de matrícula) nem em qualquer outro item de `docs/Tarefas feitas/` — este patch não depende de nada além do estado atual do `main`.

## Passo 4 — Marcar como feito

Depois que os Passos 1–3 passarem sem problema:

1. Mova este arquivo (`107 - Deletar Categoria de Servico e Servico Extra.md`) e o `.patch` correspondente de `docs/Lista de Tarefas/` para `docs/Tarefas feitas/`.
2. Marque os itens do checklist abaixo como feitos.

## Resumo das mudanças (checklist final)

- [x] Patch aplicado (`git apply`) sem conflitos
- [x] `go build ./...` limpo
- [x] `go vet ./...` limpo
- [x] `go test ./...` — todos os pacotes `ok`, incluindo `TestCategoriaServicoDeletar` e `TestServicoExtraDeletar`
- [x] `internal/db/safe_queries.go` contém `CategoriaServicoDeletada` e `ServicoExtraDeletado`
- [x] Migration `128_deletar_categoria_servico_e_servico_extra.sql` presente em `migrations/`
- [x] Arquivos desta tarefa movidos para `docs/Tarefas feitas/`
