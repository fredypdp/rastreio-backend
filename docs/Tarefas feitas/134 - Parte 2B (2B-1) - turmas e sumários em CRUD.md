# Tarefa 134 — Parte 2B (2B-1): turmas e sumários em CRUD (backend)

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `52e137a` (a Tarefa 133 já aplicada). Conferido contra `origin/main` antes de entregar: sem divergência.
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório, **mais 6 arquivos a apagar** (lista abaixo). O mesmo pacote leva a Tarefa 135.
**Plano:** Parte 2B, entrega 2B-1: turmas e sumários (destravam as Partes 16 e 19)
**Versão da API:** 4.0.1 → **4.0.2** (PATCH, junto com a 135)
**Ordem de deploy:** só backend; sem dependência com o `rastreio-frontend` (o contrato HTTP é o mesmo e o frontend não referencia nenhum tipo de evento de turma ou sumário). A migration 144 roda sozinha na subida. **O deploy não tem volta automática** (ver "Antes de aplicar em produção").

---

## Contexto

Pela regra 17, `Turma` e `Sumario` deixam de ser event sourcing: `projection_turmas` e `projection_sumarios` viram tabelas de registro e cada alteração grava a auditoria na mesma transação (`auditoria_crud`). Saem os dois agregados e 11 tipos de evento. **Nada se migra.** Rotas, campos, permissões e respostas são os mesmos.

Esta tarefa também fecha três pontos que a 133 deixou em aberto, e que o pedido de "testar tudo o que o sandbox permitir" levou a resolver aqui: a prova de reconstrução de ponta a ponta, a guarda automática contra chave estrangeira em tabela CRUD, e o ator "em nome de" na auditoria CRUD.

## Decisões de design

1. **Mesmo padrão da 133** (`internal/cadastros`): regras puras (`Turma`, `Sumario`), `TurmasRepo` e `SumariosRepo` com carga `FOR UPDATE` → regra → gravação → auditoria numa transação. Regra recusada não grava nem audita. Ações de auditoria mantêm os nomes dos eventos antigos (`TurmaCriada`, `TurmaAtivada`, `TurmaDesativada`, `TurmaDadosAtualizados`, `TurmaDeletada`, `EstudanteAdicionadoATurma`, `EstudanteRemovidoDaTurma`, `SumarioCriado`, `SumarioDadosAtualizados`, `SumarioDeletado`).
2. **Código de turma repetido é conferido dentro da transação** (isolamento SERIALIZABLE da 132). Antes, o handler conferia e o agregado gravava depois, sem proteção contra duas criações simultâneas. Teste: 8 criações simultâneas do mesmo código resultam em 1 criada e 7 recusadas.
3. **Estudante adicionado/removido da turma** passa por `Atualizar` com bloqueio de linha: 12 adições simultâneas de estudantes diferentes não se perdem; 8 adições simultâneas do mesmo estudante entram uma vez. (No event sourcing isso dependia de repetir por conflito de versão.)
4. **A projeção de turmas não some: fica só com um EFEITO.** O evento `AvaliacaoFinalEscolar` continua no ledger (nota/resultado é sensível) e a projeção de turmas o usava para tirar o estudante aprovado da turma do ano que terminou e pô-lo numa turma do ano seguinte. Isso altera uma tabela CRUD a partir de um evento. Decisão: a projeção `turmas` continua registrada **somente** para esse efeito; agora aplica o efeito e a auditoria **na mesma transação** (`EstudanteRemovidoDaTurmaPorAvaliacaoFinal`, `EstudanteAdicionadoATurmaPorAvaliacaoFinal`, ator = quem consta no metadata do evento, ou `sistema`); e o seu `Rebuild()` **não faz nada** (repetir o efeito desfaria correções feitas depois pela academia). O checkpoint `turmas` é mantido; o de `sumarios` é apagado (a projeção de sumários deixa de ser registrada).
5. **Falha corrigida no efeito (a favor da correção):** o `UPDATE` que remove o estudante das turmas de origem filtrava só por `codigo_turma`, sem a academia, e podia mexer em turma de OUTRA academia com o mesmo código. Agora filtra por academia (teste com duas academias, mesma turma `7A`). Também ganhou atomicidade: se não existe turma de destino, a remoção da origem é desfeita junto e o evento pode ser repetido.
6. **`DELETE /academia/curso/:id` agora apaga turmas, matérias e curso numa só transação** (`CursosRepo.DeletarComMaterias`, que passa a devolver também os códigos das turmas). O handler continua validando antes, para mensagens claras; a mesma regra (turma ativa ou com estudantes bloqueia) é conferida de novo dentro da transação. Antes, as turmas eram deletadas uma a uma, fora da transação das matérias.
7. **Migration 144** remove as chaves de `projection_turmas` e `projection_sumarios` para `projection_academias` (a academia passa a ser exigida pelo código, na mesma transação), pela regra 17 descoberta na 133: um `TRUNCATE … CASCADE` na reconstrução das academias apagaria as duas tabelas. Ficam as chaves entre tabelas CRUD (turmas → cursos; sumários → cursos e matérias) e as chaves de projeções para elas (`projection_faltas.sumario_id`).
8. **Funções puras que não são agregado** (`NormalizarCodigoTurma`, `TipoAgrupamentoDoNivel`, `NivelGruposTrabalho`, `NormalizarTemaTrabalho`) ficam em `aggregates/agrupamento.go`, para `projections` e os handlers usarem sem importar `cadastros` (evita ciclo de importação).
9. **Guarda automática em tempo de execução (proposta da 133, entregue aqui).** `db.TabelasCRUD` é a lista das tabelas CRUD. `Client.FKsCRUDPerigosas` consulta o banco real e acha qualquer chave estrangeira de tabela CRUD para tabela não-CRUD (cobre cadeias). O gestor de projeções chama `VerificarTabelasCRUDProtegidas` no início de **toda** reconstrução (`executeRebuild`) e a recusa, com mensagem clara, antes de apagar qualquer coisa. O teste `TestTabelasCRUDNaoDependemDeProjecoesReconstruiveis` passou a usar a mesma consulta (sem lista paralela). Limite honesto: a lista `TabelasCRUD` continua manual; o que se automatizou foi a verificação das chaves, e cada tarefa de conversão precisa acrescentar as suas tabelas.
10. **Defeito da 133 corrigido: o ator "em nome de" não chegava à auditoria CRUD.** Os handlers de cursos e matérias construíam `AuditContext` sem o secretário que agiu como a academia (Parte 4), então a auditoria diria que foi a academia. `db.AuditContext` ganhou o campo `Ator`; `ComAuditoria` o usa; `apoio.AtorDaRequisicao(c)` o preenche; todos os `AuditContext` dos handlers CRUD (cursos, matérias, turmas, sumários, configuração de faltas) passam `Ator`. Atenção à nomenclatura herdada da 132: na tabela, `ator_id/ator_tipo` = a conta em nome de quem se agiu (a academia) e `em_nome_de_id/em_nome_de_tipo` = quem agiu de fato (o secretário). Não renomeei para não mexer no que a 132 entregou.
11. **Tipos retirados** da whitelist e da política, como na 133: agregados `Turma` e `Sumario`; eventos `TurmaCriada`, `TurmaAtivada`, `TurmaDesativada`, `TurmaDadosAtualizados`, `TurmaDeletada`, `TurmaEncerrada`, `EstudanteAdicionadoATurma`, `EstudanteRemovidoDaTurma`, `SumarioCriado`, `SumarioDadosAtualizados`, `SumarioDeletado`. O ledger antigo não é apagado.

## O que foi atualizado e onde

**42 arquivos no pacote (das Tarefas 134 e 135 juntas): 28 alterados e 14 novos (incluindo os dois documentos de tarefa) e, fora do pacote, 6 a apagar.**

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `migrations/144_turmas_sumarios_crud.sql` (**novo**) | — | `DROP CONSTRAINT` das duas chaves para a academia; apaga o checkpoint `sumarios`; `COMMENT` nas tabelas. Idempotente. |
| `internal/cadastros/turmas.go` (**novo**) | `Turma` (25), `NovaTurma` (54), `Ativar` (92), `Desativar` (105), `AdicionarEstudante` (128), `RemoverEstudante` (158), `AtualizarDados` (178), `Deletar` (219), `TurmasRepo` (241), `Criar` (325), `carregarTurmaTx` (373), `gravarTurmaTx` (385), `Atualizar` (420), `idsTurmasDoCursoTx` (447) | Regras portadas do agregado e persistência com auditoria. |
| `internal/cadastros/sumarios.go` (**novo**) | `Sumario` (23), `NovoSumario` (66), `AtualizarDados` (112), `Deletar` (142), `SumariosRepo` (158), `Criar` (187), `carregarSumarioTx` (215), `Atualizar` (252) | Idem. |
| `internal/cadastros/cursos.go` | `DeletarComMaterias` (460) | Cascata inclui as turmas do curso, na mesma transação; devolve também os códigos das turmas. |
| `internal/cadastros/erros.go` | `isNoRows` (fim) | Auxiliar (usado pela 135). |
| `internal/cadastros/turmas_test.go`, `sumarios_test.go` (**novos**) | 12 + 2 testes de regra | Testes do agregado antigo portados, mais ciclo de status, estudantes, recusa sem alteração parcial, `Deletar`. |
| `internal/cadastros/turmas_integration_test.go` (**novo**) | 11 testes | Registro e auditoria, academia inexistente, código repetido (sequencial e concorrente), regra recusada não audita, estudantes concorrentes, cascata do curso (3 cenários), ciclo do sumário, **efeito da avaliação final** (movimento, histórico, outra academia, atomicidade), reconstrução de turmas, **ator "em nome de"**. |
| `internal/cadastros/repo_integration_test.go` | `TestTabelasCRUDNaoDependemDeProjecoesReconstruiveis`, chamadas de `DeletarComMaterias` | Usa `FKsCRUDPerigosas`; nova assinatura. |
| `cmd/server/rebuild_crud_integration_test.go` (**novo**) | `TestReconstrucaoRealPreservaTabelasCRUD` (27), `bancoIsolado` (191) | **Reconstrução real de ponta a ponta** (ver "Validação"). |
| `cmd/server/main.go` | `registrarProjecoes` (180); `initProjections` | A lista de projeções virou função, para o teste usar exatamente a da produção; sai o registro de `sumarios`. |
| `internal/projections/turmas_projection.go` | `Handle` (72), `Rebuild` (82), `atorDoEvento` (89), `handleAvaliacaoFinalAnoAcademico` (113) | Só o efeito da avaliação final, com auditoria na mesma transação e filtro por academia; sem handlers de turma; `Rebuild` não faz nada. Leituras e DTO intactos. |
| `internal/projections/sumarios_projection.go` | — | Sem `Name`, checkpoint, `Handle` nem `Rebuild`; ficam as leituras. |
| `internal/projections/manager.go` | `executeRebuild` (442) | Chama a guarda antes de reconstruir. |
| `internal/db/politica_eventsourcing.go` | `TabelasCRUD` (236), `FKsCRUDPerigosas` (265), `VerificarTabelasCRUDProtegidas` (292) | Lista e guarda das tabelas CRUD; pendências de turma e sumário removidas. |
| `internal/db/politica_eventsourcing_test.go` | `TestPoliticaGuardaReprovaTipoNovoForaDaLista` | Usa um tipo fictício pendente, não depende mais de tipo real. |
| `internal/db/safe_queries.go` | whitelist | Saem 2 agregados e 11 eventos (e comentários velhos). |
| `internal/db/repository.go`, `auditoria_crud.go` | `AuditContext.Ator` (55); `comAuditoriaUmaVez` (216) | Ator "em nome de" na auditoria CRUD. |
| `internal/domain/aggregates/agrupamento.go` (**renomeado de `turma.go`**), `agrupamento_test.go` (**novo**) | funções puras | Só `NormalizarCodigoTurma`, `NivelGruposTrabalho`, `TipoAgrupamentoDoNivel`, `NormalizarTemaTrabalho`. |
| `internal/domain/aggregates/aggregate.go` | fábrica | Saem `Turma` e `Sumario`. |
| `internal/handlers/turmas/turmas_handler.go` | `CriarTurma` (37), `AtivarTurma` (249), `DesativarTurma` (290), `RemoverEstudanteDaTurma` (428), `DeletarTurma` (579), `AtualizarDadosTurma` (625) | Chamam `cadastros.TurmasRepo`; validações do handler intactas. |
| `internal/handlers/apoio/turmas.go` | `VincularEstudanteATurma` (163) | Idem; o erro de regra continua devolvendo a mensagem de antes. |
| `internal/handlers/apoio/helpers.go` | `AtorDaRequisicao` (39) | Novo. |
| `internal/handlers/sumarios/sumario_handlers.go` | `CriarSumario` (25), `AtualizarDadosSumario` (197), `DeletarSumario` (256) | Idem. |
| `internal/handlers/cursos/cursos_handlers.go`, `materias/materia_disciplinar_handlers.go` | todos os `AuditContext` | Passam `Ator` (decisão 10); `DeletarCurso` deixa de apagar turmas uma a uma. |
| `internal/handlers/turmas/turmas_grupo_integration_test.go` | 3 testes | Reescritos sem "projetar eventos": gravam pelo repositório, leem na hora, conferem a auditoria. Ganhou o caso de código repetido (400). |
| `cmd/server/turma_vinculo_estudante_integration_test.go`, `cmd/server/contas_integration_test.go` | fixture de turmas; `TestContas03…` | Turmas criadas pela camada CRUD; a auditoria do secretário é conferida em `auditoria_crud`. |
| `Documentação da API.md`, `Dockerfile` | versão, tabela de versões, regra de aprovação automática | 4.0.2. |
| `docs/Planos de Atualização/Plano de Atualizações 1.md` | Parte 2B, Visão geral, próxima tarefa livre (136), Registro | Passos do plano. |
| `docs/Lista de Tarefas/134 - …md`, `135 - …md` (**novos**) | — | Estes documentos. |

### Arquivos a APAGAR

```
internal/domain/aggregates/turma.go
internal/domain/aggregates/turma_test.go
internal/domain/aggregates/sumario.go
internal/domain/aggregates/sumario_test.go
internal/domain/aggregates/configuracao_faltas.go
internal/domain/aggregates/configuracao_faltas_test.go
```

(`turma.go` foi **renomeado** para `agrupamento.go`: apague o antigo e copie o novo. Os dois últimos são da Tarefa 135.) Nada mais precisa ser movido ou renomeado.

## Antes de aplicar em produção

**Sem volta automática.** Depois do deploy, turmas e sumários só mudam nas tabelas. Faça backup e confira:

```sql
-- 1) Há eventos de turma ou sumário no ledger? (esperado: nenhuma linha, ou só dados de teste)
SELECT aggregate_type, event_type, count(*) FROM spuri_ledger
WHERE aggregate_type IN ('Turma', 'Sumario') GROUP BY 1, 2;

-- 2) Contagens de hoje (devem ser iguais depois do deploy)
SELECT (SELECT count(*) FROM projection_turmas) AS turmas, (SELECT count(*) FROM projection_sumarios) AS sumarios;

-- 3) As chaves que a migration 144 remove (a migration usa IF EXISTS)
SELECT conrelid::regclass AS tabela, conname FROM pg_constraint
WHERE contype = 'f' AND conrelid IN ('projection_turmas'::regclass, 'projection_sumarios'::regclass)
  AND confrelid = 'projection_academias'::regclass;
```

**Se a consulta 1 mostrar dados reais** (turmas criadas no ledger e já alteradas só pelo ledger depois de a projeção ter sido reconstruída), não aplique às cegas: avise antes, porque os tipos estão saindo da whitelist. Como o ledger antigo não é apagado, a leitura histórica por `aggregate_id` continua funcionando; só a leitura *por tipo* (`GetEventsByType`) deixa de aceitar os tipos retirados.

## Validação realizada

**Ambiente:** Go 1.24.13 (pacote `golang-1.24-go`), PostgreSQL 16 (cluster local descartável), `main` @ `52e137a`. Dependências resolvidas por espelhos do GitHub, porque o proxy oficial de módulos não é acessível do sandbox (só no ambiente de teste, sem alterar `go.mod`). `go vet ./...` e `gofmt -l .` limpos; `go build ./...` limpo.

**Baseline de `main`** (banco novo, `RUN_POSTGRES_INTEGRATION=1`, `SPURI_RUN_DB_INTEGRITY_TESTS=1`, `-p 1`): 29 pacotes `ok`, **842 testes passam, 2 pulados** (`TestHTTPIntegrationCorrigirNotaRecalculaAvaliacaoFinal` e `TestFaltasPeriodo15…`, que se pulam por conta própria), 0 falhas.

**Com a 134 e a 135:** 29 pacotes `ok`, **850 testes passam, os mesmos 2 pulados, 0 falhas, 0 regressões.**

**Reconstrução real de ponta a ponta** (o ponto que a 133 deixou sem prova). `TestReconstrucaoRealPreservaTabelasCRUD`, num **banco criado só para o teste** (a reconstrução completa reprocessa o ledger inteiro e o banco compartilhado dos outros testes tem linhas inseridas à mão que não são reproduzíveis; o teste cria o banco, roda as migrations e o apaga, e se pula se o usuário não puder criar bancos):

1. cria uma academia de verdade no ledger e roda `RebuildAllProjections()` (a mesma lista da produção, com a verificação de integridade do ledger): a academia é materializada;
2. cria curso, matéria, turma (com um estudante) e sumário pela camada CRUD;
3. roda **outra** `RebuildAllProjections()` completa: as 4 linhas, o conteúdo da turma e a contagem de auditoria são idênticos;
4. roda `RebuildProjection("academias")` (o `TRUNCATE … CASCADE` que apagaria tudo): idem;
5. cria de propósito uma chave estrangeira perigosa e confirma que o gestor **recusa** a reconstrução (a parcial e a completa) e nada muda;
6. **contraprova:** com essa chave presente, o `Rebuild()` direto da projeção de academias (sem passar pelo gestor) **apaga** a turma. Isso mostra que o teste enxerga o perigo de verdade.

**Ciclo "desfazer a regra → o teste cai → restaurar"** (8 mutações, todas detectadas):

| Regra desfeita | Teste que falhou |
| --- | --- |
| sem checagem de código repetido na turma | `TestTurmaCriarRecusaAcademiaInexistenteECodigoRepetido`, `TestTurmaCodigoRepetidoConcorrenteCriaSoUma` |
| efeito da avaliação final sem filtro de academia | `TestAvaliacaoFinalMoveEstudanteEAudita` |
| guarda de chave estrangeira do gestor desligada | `TestReconstrucaoRealPreservaTabelasCRUD` |
| ator "em nome de" ignorado na auditoria CRUD | `TestAuditoriaCRUDGravaOAtorEmNomeDe` |
| turma com histórico aceita mudar nível | `TestTurmaAtualizarDadosBloqueiaIdentidadeComHistorico` |
| curso deletado com turma ativa | `TestDeletarCursoComTurmas` |
| remoção da origem não desfeita quando falta destino | `TestAvaliacaoFinalMoveEstudanteEAudita` |
| (135) reprovação por faltas sem exigir limite | `TestConfiguracaoFaltasRegras` |

**O que ainda NÃO está provado:** (a) a validação foi em PostgreSQL 16 local, não no Neon de produção; (b) o efeito da avaliação final foi testado chamando `Handle` com um evento montado, não pelo fluxo completo de uma avaliação final real (que continua coberta pelos testes existentes de avaliação, todos verdes); (c) não executei o `frontend`, só confirmei por busca que ele não referencia tipos de evento de turma ou sumário.

## Falha antiga que não é desta tarefa

Rodar a suíte uma **segunda vez no mesmo banco** já falha em `main` (fixtures com códigos fixos). Comparei a 2ª execução de `main` com a desta entrega: **nenhuma falha nova**. Use sempre banco novo.

## Como aplicar

1. **Confirme que a base não mudou:** `git fetch origin && git diff --stat 52e137a origin/main`. Vazio = pode substituir; se aparecer algum arquivo, avise antes.
2. **Confirme o número da migration:** `ls migrations | grep -E "^14[45]"` não deve listar nada.
3. **Apague** os 6 arquivos acima e **copie** `rastreio-backend/` por cima da raiz (o bloco git do final já faz isso na ordem certa).
4. **Verifique** (abaixo) e **marque como feito**.

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
RUN_POSTGRES_INTEGRATION=1 SPURI_RUN_DB_INTEGRITY_TESTS=1 DATABASE_URL=postgres://… go test ./... -p 1
```

Esperado: `gofmt` vazio; build e vet limpos; todos `ok`. Sem as duas variáveis, os testes de integração são pulados. O teste de reconstrução precisa de usuário com `CREATEDB`; sem isso ele se pula e mostra o motivo.

## Fora de escopo

- Os demais grupos da 2B-1: documentos extra, categorias de nota, `ConfiguracaoSistema`, comunicação, dados cadastrais não críticos e dados das contas de pessoa. E toda a **2B-2** (senhas, 2FA, e-mail). A **2B-3** é condicional e só vem depois da Parte 8.
- `CursosProjection`, `MateriasProjection`, `TurmasProjection` e `SumariosProjection` mantêm o nome "projeção" (leitores); renomear fica para limpeza futura.
- Renomear as colunas `ator_*` / `em_nome_de_*` da auditoria (decisão 10).
- Nenhuma mudança no `rastreio-frontend`.

## Marcar como feito

1. `**Estado:** feito`, `(feito)` no título.
2. Secção **Resultado** no fim.
3. Mover para `docs/Tarefas feitas/`, mesmo nome.

## Checklist

- [ ] Base conferida (`52e137a`)
- [ ] 6 arquivos apagados; pasta copiada; migrations 144 e 145 sem conflito
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test ./...` — todos `ok` (850 passam, 2 pulados)
- [ ] Backup feito e as 3 consultas de produção rodadas antes do deploy
- [ ] Estado trocado para **feito**, **Resultado** adicionado, documento movido
