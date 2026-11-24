# Tarefa 133 — Parte 2B (2B-1): cursos e matérias em CRUD (backend)

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `13e89af` ("feat(db): regra 17 com teste-guarda e auditoria transacional do CRUD", a Tarefa 132 já aplicada)
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório, **mais 4 arquivos a apagar** (lista abaixo)
**Plano:** Parte 2B, entrega 2B-1, primeiro grupo de conversão: cursos e matérias
**Versão da API:** 4.0.0 → **4.0.1** (PATCH; ver "Decisões", item 8). `Documentação da API.md` e `Dockerfile` vão no pacote.
**Ordem de deploy:** só backend; sem dependência com o `rastreio-frontend`. A migration 143 roda sozinha na subida. **O deploy não tem volta automática** (ver "Antes de aplicar em produção").

---

## Contexto

Pela regra 17, cursos e matérias deixam de ser event sourcing: a projeção atual (`projection_cursos`, `projection_materias`) vira a tabela de registro e cada alteração grava a auditoria na mesma transação, em `auditoria_crud` (base entregue na Tarefa 132). Saem o agregado, os eventos e o handler de projeção. **Nada se migra**: as linhas que já existem nas tabelas continuam sendo o dado.

As rotas, os campos, as permissões e as respostas são os mesmos.

## Decisões de design

1. **Pacote novo `internal/cadastros`** com as regras de negócio em funções puras sobre structs simples (sem banco, testáveis sozinhas) e a persistência em `CursosRepo` e `MateriasRepo`. Mensagens de erro iguais às do agregado antigo. Uma violação de regra volta como `ErroRegra` (o handler responde com erro de validação, como antes); registro inexistente volta como `ErrNaoEncontrado` (404).
2. **Cada alteração é uma transação só:** carrega a linha com bloqueio (`FOR UPDATE`), aplica a regra, grava, audita. Se a regra recusar, nada é gravado nem auditado. Ações de auditoria mantêm os nomes dos eventos antigos (`CursoCriado`, `CursoAtivado`, `CursoDesativado`, `CursoDadosAtualizados`, `CursoDeletado`, `MateriaCriada`, `MateriaAtivada`, `MateriaDesativada`, `MateriaDadosAtualizados`, `MateriaDeletada`), para a leitura da auditoria continuar familiar. `MateriaPeriodoDefinido` deixou de existir: o período é gravado na criação da matéria, junto das demais colunas.
3. **`version` continua existindo** e continua sendo devolvido pela API: começa em 1 e sobe 1 a cada alteração, como no event sourcing.
4. **Problema encontrado e resolvido: reconstruir academias apagaria todos os cursos e matérias.** A reconstrução da projeção de academias faz `TRUNCATE ... CASCADE`, e `projection_cursos` e `projection_materias` tinham chave estrangeira para `projection_academias` com `ON DELETE CASCADE`. Como agora o dado deixou de vir do ledger, não haveria como recuperá-lo. Foi demonstrado em teste: sem a correção, depois do `TRUNCATE` ficam 0 cursos e 0 matérias. A migration 143 remove essas duas chaves; a existência da academia passa a ser exigida pelo código, **na mesma transação** da gravação. Ficam as chaves entre as duas tabelas CRUD (`materias.curso_id → cursos`) e as chaves das projeções que apontam *para* cursos e matérias (estudantes, turmas, sumários, notas, faltas): essas são seguras, porque reconstruí-las não toca em cursos e matérias. **Regra nova para toda conversão** (já escrita no plano): tabela CRUD nenhuma pode ter chave estrangeira para tabela que o ledger reconstrói; um teste confere isso e cada tarefa de conversão acrescenta as suas tabelas.
5. **Cursos e matérias saem da reconstrução de projeções:** o gestor deixa de registrá-los (`main.go`), de listá-los na ordem de reconstrução (`manager.go`) e a migration apaga os dois checkpoints. Um teste garante que não voltam para essa lista. Os tipos `Curso` e `MateriaDisciplinar` e os 11 eventos saíram da whitelist e da política (o ledger antigo não é apagado).
6. **Leituras não mudam:** `CursosProjection` e `MateriasProjection` continuam existindo como leitores (mesmos DTOs e métodos, usados por ~20 arquivos), mas sem consumir eventos nem reconstruir. O nome "projeção" ficou por não valer o risco de mexer em tantos arquivos; fica como limpeza futura.
7. **Três ajustes de comportamento, todos a favor da consistência** (cobertos por teste):
   - **`DELETE /academia/curso/:id`** passou a validar as turmas (ativa, com estudantes) **antes de gravar qualquer coisa**. Antes, as matérias já tinham sido deletadas quando uma turma ativa fazia a operação falhar. Agora as matérias inativas e o curso saem **numa só transação**, cada um com a sua auditoria. As turmas continuam em event sourcing (convertidas em tarefa própria) e são deletadas antes; uma nova tentativa depois de falha parcial continua de onde parou, porque turma já deletada é ignorada.
   - **`PUT /academia/materia/:id/dados`** com `curso_id` passou a recusar de fato curso inexistente ou de **outra academia** (`422`). O código antigo aceitava; a documentação já dizia que o curso tinha de ser da mesma academia.
   - **Leitura logo depois de gravar** já devolve o dado novo: não há mais espera pela projeção.
8. **Versão 4.0.1, não 4.0.0 nem 5.0.0.** A regra do plano pede MAJOR quando uma regra de acesso passa a negar o que antes era permitido. Aqui a única coisa que passa a ser negada (matéria ligada a curso de outra academia) já era declarada como proibida na documentação: é correção do comportamento ao contrato, não mudança do contrato. Se preferir tratar como MAJOR, é só trocar a versão (arquivos `Documentação da API.md`, `Dockerfile`).
9. **Ator da auditoria:** a academia (`UserID` = id da academia). `academia_id` guarda o UUID da academia; o código legível (`ACA_01`) está no snapshot de antes/depois.
10. **Fixtures de teste** que criavam curso e matéria por agregado e reconstrução passaram a gravar direto pela camada CRUD.

## O que foi atualizado e onde

**29 arquivos no pacote: 3 de documentação/infra alterados (API, Dockerfile, plano), 17 de código alterados, 9 novos (incluindo este documento) e, fora do pacote, 4 a apagar.**

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `migrations/143_cursos_materias_crud.sql` (**novo**) | `DROP CONSTRAINT` das duas chaves para a academia; `DELETE` dos checkpoints; `COMMENT` nas tabelas | Passa as tabelas a registro oficial e desfaz o risco do `TRUNCATE ... CASCADE`. Idempotente (`IF EXISTS`). |
| `internal/cadastros/erros.go` (**novo**) | `ErrNaoEncontrado` (19), `ErroRegra` (24), `EhErroRegra` (34) | Erros do pacote. |
| `internal/cadastros/cursos.go` (**novo**) | `Curso` (33), `NovoCurso` (57), `Ativar` (103), `Desativar` (112), `AtualizarDados` (122), `Deletar` (178), validadores de período e modelo (191–293), `CursosRepo` (300), `Criar` (340), `exigirAcademia` (363), `carregarCursoTx` (375), `gravarCursoTx` (408), `Atualizar` (428), `DeletarComMaterias` (458) | Regras puras (portadas do agregado) e persistência com auditoria na mesma transação. |
| `internal/cadastros/materias.go` (**novo**) | `Materia` (21), `NovaMateria` (44), `Ativar` (91), `Desativar` (103), `AtualizarDados` (113), `DefinirPeriodo` (146), `Deletar` (158), `MateriasRepo` (177), `exigirCursoDaAcademia` (200), `Criar` (216), `carregarMateriaTx` (249), `gravarMateriaTx` (290), `Atualizar` (323) | Idem para matérias; curso da matéria precisa existir e ser da mesma academia. |
| `internal/cadastros/regras_test.go` (**novo**) | 22 testes de regra | Os testes do antigo agregado, portados, mais os de ciclo de status e campos obrigatórios. |
| `internal/cadastros/repo_integration_test.go` (**novo**) | 13 testes (106–452) | Registro e auditoria, regra recusada não grava nem audita, concorrência sem perda, matéria × curso de outra academia, cascata atômica, **`TestReconstruirAcademiasNaoApagaCursosNemMaterias` (400)** e **`TestTabelasCRUDNaoDependemDeProjecoesReconstruiveis` (429)**. |
| `internal/handlers/apoio/cadastros_resposta.go` (**novo**) | `RespondErroCadastro` | Traduz `ErroRegra` → validação, `ErrNaoEncontrado` → 404, resto → 500. |
| `internal/handlers/cursos/cursos_handlers.go` | `CriarCurso` (186), `AtualizarDadosCurso` (323), `AtivarCurso` (467), `DesativarCurso` (509), `DeletarCurso` (559); usos de `cadastros` nas linhas 107, 116, 226–238, 378–384, 495–499, 537–541, 693–696 | Deixam de carregar e salvar agregado; chamam o repositório CRUD. `DeletarCurso` reordenado (decisão 7). Validações do handler intactas. |
| `internal/handlers/materias/materia_disciplinar_handlers.go` | `CriarMateria` (22), `AtivarMateria` (220), `DesativarMateria` (262), `AtualizarDadosMateria` (307), `DeletarMateria` (486) | Idem. |
| `internal/handlers/apoio/avaliacoes.go` | 2 referências | `aggregates.ModeloCursoMedio*` → `cadastros.ModeloCursoMedio*` (as constantes e a derivação dos anos do curso médio moraram no agregado apagado). |
| `internal/projections/cursos_projection.go`, `materias_projection.go` | removidos `Name`, checkpoint, `Handle`, `Rebuild`, handlers de evento e `modeloCursoParaProjecao` | Ficam só os DTOs e as leituras. |
| `internal/projections/manager.go` | `defaultRebuildOrder` | `cursos` e `materias` saem da ordem de reconstrução. |
| `internal/projections/manager_rebuild_test.go` | lista de projeções registradas; `TestCursosEMateriasNaoSaoReconstruiveis` (novo) | Atualizado e protegido. |
| `internal/projections/cursos_projection_test.go` | teste do backfill de evento legado removido | O backfill só existia para reprocessar eventos. |
| `internal/domain/aggregates/aggregate.go` | fábrica de agregados | Saem os casos `Curso` e `MateriaDisciplinar`. |
| `internal/db/safe_queries.go` | whitelist | Saem 2 tipos de agregado e 11 de evento. |
| `internal/db/politica_eventsourcing.go` | pendências; constantes `TabelaCursos`, `TabelaMaterias` | Curso e Matéria saem de "pendentes"; novo registro das tabelas CRUD. |
| `internal/db/politica_eventsourcing_test.go` | amostras e mapa de prefixos | Usa `TurmaCriada` como amostra de pendente. |
| `cmd/server/main.go` | registro de projeções | Saem os dois `RegisterProjection`. |
| `cmd/server/cursos_materias_crud_integration_test.go` (**novo**) | `TestCursosMaterias133FluxoCompletoPorHTTP` (62), `…OutraAcademiaNaoMexe` (181) | Fluxo completo pela API: criar, ler, atualizar, ativar/desativar, recusas, cascata, auditoria na ordem, **nenhum evento no ledger**, cadeia íntegra, outra academia recebe 403. |
| `cmd/server/contas_integration_test.go`, `notas_faltas_correcao_integration_test.go`, `internal/handlers/materias/materia_disciplinar_handlers_test.go`, `internal/handlers/avaliacoes/modelo_avaliativo_escolar_test.go` | fixtures e constantes | Passam a usar `cadastros`. |
| `Documentação da API.md` | versão (linha 5), tabela de versões (nova 4.0.1), `POST /academia/curso`, `DELETE /academia/curso/:id`, `PUT /academia/materia/:id/dados`, `DELETE /academia/materias/:id` | Texto de "grava evento no ledger" corrigido; regras de cascata e de curso da mesma academia explícitas. |
| `Dockerfile` | `LABEL version` | 4.0.0 → 4.0.1. |
| `docs/Planos de Atualização/Plano de Atualizações 1.md` | Parte 2B: Estado, subitens de conversão, regra nova das tabelas CRUD; visão geral; próxima tarefa livre → 134; Registro de andamento | Passos 1 a 5 e 8 do plano. |
| `docs/Lista de Tarefas/133 - …md` (**novo**) | — | Este documento. |

### Arquivos a APAGAR

```
internal/domain/aggregates/curso.go
internal/domain/aggregates/curso_test.go
internal/domain/aggregates/materia_disciplinar.go
internal/domain/aggregates/materia_disciplinar_test.go
```

Nada precisa ser movido ou renomeado. **Contrato HTTP:** igual (ver decisão 7 para os três ajustes de comportamento).

## Antes de aplicar em produção

**Sem volta automática.** Depois do deploy, cursos e matérias passam a ser alterados só nas tabelas. Se alguém voltasse ao código antigo e reconstruísse as projeções a partir do ledger, perderia tudo o que foi alterado depois. Faça backup do banco antes e confira:

```sql
-- 1) Há eventos de curso ou matéria no ledger? (esperado: nenhuma linha, ou só dados de teste)
SELECT aggregate_type, event_type, count(*) FROM spuri_ledger
WHERE aggregate_type IN ('Curso', 'MateriaDisciplinar') GROUP BY 1, 2;

-- 2) Quantos registros existem hoje? (anote; devem ser iguais depois do deploy)
SELECT (SELECT count(*) FROM projection_cursos) AS cursos, (SELECT count(*) FROM projection_materias) AS materias;

-- 3) As chaves que a migration 143 remove existem? (a migration usa IF EXISTS, então não falha de qualquer forma)
SELECT conname FROM pg_constraint
WHERE confrelid = 'projection_academias'::regclass
  AND conrelid IN ('projection_cursos'::regclass, 'projection_materias'::regclass);
```

Se a consulta 1 mostrar dado real, **nada se perde** (as tabelas já têm o estado atual) e os eventos antigos ficam no ledger sem serem lidos por ninguém; avise para decidirmos se vale congelar os tipos em vez de os tirar da whitelist.

## Validação realizada

**Ambiente:** Go 1.27.4 e PostgreSQL 16.15 (sandbox; as dependências Go vieram de clones do GitHub nas versões do `go.sum`, porque o proxy de módulos não é acessível aqui). Bancos de teste novos, com as 143 migrations aplicadas do zero. O PostgreSQL do sandbox caiu algumas vezes durante o trabalho; as execuções finais foram feitas com ele mantido ativo.

**Base:** clone novo de `main` @ `13e89af` + a pasta `rastreio-backend/` copiada por cima + os 4 arquivos apagados.

| Comando | Resultado |
| --- | --- |
| `gofmt -l .` | lista vazia |
| `go build ./...` | ok |
| `go vet ./...` | ok |
| `RUN_POSTGRES_INTEGRATION=1 SPURI_RUN_DB_INTEGRITY_TESTS=1 go test ./... -p 1` (banco novo) | 29 pacotes `ok`, 0 `FAIL` |

**Comparação por teste (`-v`, banco novo, 1.ª execução):**

| | `--- PASS` | `--- FAIL` | `--- SKIP` |
| --- | --- | --- | --- |
| `main` @ `13e89af` puro | 817 | 0 | 2 |
| com a Tarefa 133 | 842 | 0 | 2 |

Quatro nomes de teste deixaram de existir, e nenhum é regressão: `TestCursoMedioRejeitaAnosAcademicosManuaisNoAggregate` (portado como `TestCursoMedioRejeitaAnosAcademicosManuais`), `TestMateriaMedioPermiteMaisDeUmAnoAcademico` e `TestMateriaMedioPermiteQuartoAnoParaPAPValidadaNoHandler` (fundidos em `TestMateriaMedioPermiteMaisDeUmAnoAcademicoEPAP`) e `TestModeloCursoParaProjecaoBackfillEventoLegadoMedio` (testava o reprocessamento de eventos antigos, que deixou de existir). 817 − 4 + 29 novos = 842.

**Ciclo reverter → falhar → restaurar** (cada arquivo restaurado conferido com `cmp`):

| Regra desfeita | Teste que falhou |
| --- | --- |
| Migration 143 sem remover as chaves para a academia | `TestReconstruirAcademiasNaoApagaCursosNemMaterias` ("TRUNCATE de academias apagou dados CRUD: cursos=0 matérias=0") e `TestTabelasCRUDNaoDependemDeProjecoesReconstruiveis` |
| Criar curso sem exigir que a academia exista | `TestCursoCriarExigeAcademiaExistente` |
| Atualizar matéria sem conferir que o curso é da mesma academia | `TestMateriaNaoPodeMudarParaCursoDeOutraAcademia` |
| Atualização de curso sem gravar a auditoria | `TestCursoAtualizarVersaoAuditoriaAntesDepois` |
| Handler deixa de traduzir violação de regra em erro de validação | `TestCursosMaterias133FluxoCompletoPorHTTP` (500 em vez de 4xx) |

Observação honesta: o bloqueio de linha (`FOR UPDATE`) **não** tem ciclo de reversão próprio. `TestCursoAtualizacoesConcorrentesNaoSePerdem` (12 alterações simultâneas, nenhuma se perde) passa também porque a transação serializable da Tarefa 132 repete as colisões; o bloqueio de linha reduz as repetições, mas o teste não o isola.

**Não validado:** o front-end (não foi lido nem alterado); o Neon de produção (só PostgreSQL local); `Documentação da API.md` foi revista só nas passagens que citam cursos e matérias, não lida inteira.

## Falha antiga que não é desta tarefa

Rodar a suíte **uma segunda vez no mesmo banco** faz falhar dezenas de testes em `main` puro (`spuri/cmd/server` e `spuri/internal/handlers/administradores`), todos por `rebuild turmas: ... violates foreign key constraint "projection_turmas_codigo_academia_fkey"`: o `rebuild` reprocessa um ledger com eventos de testes anteriores. Com a Tarefa 133 a 2.ª execução falha nos mesmos testes mais **os 2 testes HTTP novos** (`TestCursosMaterias133…`), pelo mesmo motivo: eles usam a mesma fixture (`setupTurmaVinculoIntegration`) que falha antes de chegar ao código novo. Os testes novos de `internal/cadastros` e de `internal/db` passam na 2.ª execução. Recomendação: rodar a suíte completa sempre em banco novo. A causa não foi corrigida aqui.

## Como aplicar

1. **Confirme que a base não mudou:** `git fetch origin && git diff --stat 13e89af origin/main`. Vazio = pode substituir. Se aparecer qualquer arquivo da tabela acima, ele mudou depois da base: não substitua, avise. Confirme também que o número 143 continua livre (`ls migrations | grep ^143`).
2. **Copie** a pasta `rastreio-backend/` para a raiz do repositório, mantendo os caminhos.
3. **Apague** os 4 arquivos da lista acima.
4. **Verifique** (abaixo) e faça a conferência da seção "Antes de aplicar em produção".
5. **Marque como feito** (abaixo).

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./internal/cadastros/ ./internal/db/ ./internal/projections/ ./internal/domain/... -v
RUN_POSTGRES_INTEGRATION=1 DATABASE_URL='postgres://…banco-de-teste-NOVO-e-descartavel…' go test ./... -p 1
```

Esperado: `gofmt -l` vazio; build e vet limpos; sem `RUN_POSTGRES_INTEGRATION=1` os testes de banco são ignorados; com ele, todos passam. Use sempre um **banco novo e descartável**.

## Fora de escopo

- **Demais conversões da 2B-1:** turmas e sumários (próximos, por causa das Partes 16 e 19), documentos extra, categorias de nota, configurações, comunicação, dados cadastrais não críticos e contas de pessoa. Também a **2B-2**, a **2B-3** (condicional), a **Parte 7** e a **Parte 2** (pacote `finance`).
- **Renomear** `projection_cursos`/`projection_materias` e os tipos `CursosProjection`/`MateriasProjection` (limpeza futura).
- **Rota HTTP de leitura da auditoria** e o destino dos endpoints `/eventos-estudante/:codigo` e `/eventos/:event_id` (decisão adiada para a conversão que os tocar).
- **Front-end:** nenhuma mudança.
- **Correção da fragilidade da 2.ª execução da suíte** no mesmo banco.

## Marcar como feito

1. `**Estado:** feito`, `(feito)` no título.
2. Secção **Resultado** no fim: um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mover para `docs/Tarefas feitas/`, mesmo nome.

No plano, o subitem "cursos e matérias" já vai marcado `[x]`; o Estado da Parte 2B continua **Parcial**.

## Checklist

- [ ] Base conferida (`13e89af`) e número 143 livre
- [ ] Os 29 arquivos copiados e os 4 apagados
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test ./...` em banco novo — todos `ok`
- [ ] Conferência do ledger e backup feitos antes do deploy em produção
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`
