# Tarefa 132 — Parte 2B (2B-1): regra 17, teste-guarda e auditoria do CRUD (backend)

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `ceba7e3` (hash completo `ceba7e3e8d75161090abe7e36b957e0ed117aea2`, "docs(plano): adiciona Parte 2B")
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório
**Plano:** Parte 2B, entrega 2B-1, itens 1 e 2 (os dois primeiros itens de **Entregas**)
**Versão da API:** não muda (4.0.0 → 4.0.0). Nenhuma rota, campo, permissão ou regra de negócio foi alterada; por isso a `Documentação da API.md` e o `Dockerfile` não vão no pacote.
**Ordem de deploy:** sem dependência com o `rastreio-frontend` (não há tarefa de front-end nesta entrega). Pode ir para produção a qualquer momento: a migration 142 só cria uma tabela e funções novas e nada no código em uso passa a depender delas.

---

## Contexto

O pedido foi "implementar a Parte 2B" do `Plano de Atualizações 1.md`: passar o backend para o modelo híbrido (event sourcing só no que é sensível; o resto em CRUD com auditoria, regra 17).

A Parte 2B inclui converter para CRUD cerca de 20 agregados e 45 tipos de evento (2B-1), tirar senha, 2FA e e-mail do ledger (2B-2) e, se a medição pedir, o financeiro operacional (2B-3). Isso não cabe numa entrega só que possa ser validada de verdade; o próprio plano manda converter "uma entrega interna por agregado ou grupo". Por isso a Parte 2B foi dividida em várias tarefas, e **esta, a 132, entrega a base de que todas as outras precisam**: os dois primeiros itens da 2B-1.

1. **Regra 17 e lista aprovada em código, com teste-guarda.** Hoje a whitelist do ledger tem 22 tipos de agregado e 165 tipos de evento. Cada um foi classificado: continua em event sourcing (aprovado) ou vai para CRUD (pendente de conversão, com a entrega responsável). Um teste falha se surgir qualquer tipo fora da lista.
2. **Tabela de auditoria e auxiliar transacional para o CRUD.** `auditoria_crud` (só inserção, hash encadeado) e `ComAuditoria`, que grava a alteração e a auditoria na mesma transação.

**Nada do que já funciona muda.** Nenhum handler, agregado, projeção, rota, `safe_queries.go` ou `main.go` foi tocado; nenhum evento deixa de ser gravado.

## Decisões de design

1. **Divisão da Parte 2B em tarefas.** Esta (132) = itens 1 e 2 da 2B-1. As seguintes (133 em diante) convertem os agregados por grupo, uma tarefa cada, usando a base desta. Sugestão de ordem no fim do documento.
2. **Lista aprovada em código, negar por padrão.** `politica_eventsourcing.go` tem quatro situações, e todo tipo da whitelist e do código tem de estar em exatamente uma: *aprovado*, *pendente de conversão* (com a entrega: 2B-1, 2B-2, 2B-3 ou Parte 7), ou *convertido histórico* (congelado: fora do que é gravado, mas aceito para ler o ledger antigo; hoje vazio). Cada tarefa de conversão tira seus tipos de "pendente"; quando a lista de pendentes ficar vazia, a 2B terminou.
3. **Contagem real da classificação** (sobre `main` @ `ceba7e3`): eventos 82 aprovados + 45 pendentes da 2B-1 + 9 da 2B-2 + 19 da 2B-3 + 10 da Parte 7 = 165; agregados 11 aprovados + 11 pendentes = 22. `ServicoExtra` e `CategoriaServico` ficam como pendentes da **Parte 7**, não da 2B-1 (o plano manda a Parte 7 já os substituir em CRUD).
4. **Eventos cuja classificação foi uma interpretação minha** (cada tarefa de conversão os confirma ao converter): `DadosPessoaisAtualizados` e `DadosAcademicosAtualizados` do estudante (o crítico — nome, BI, data de nascimento — tem evento próprio e fica em event sourcing; o resto é cadastral → 2B-1); `TelefoneEncarregadoAlterado` (telefone não é dado crítico → 2B-1); `EstudanteDocumentosCompletados` (marca de cadastro → 2B-1); `ProfessorVinculoAtribuido`/`Removido` (vínculo com início e fim → 2B-1); `ProvedorPagamentoDefinido` (operacional → 2B-3). Na dúvida, o critério de segurança foi: o que é prova (dinheiro, nota, falta, identidade, permissões) fica em event sourcing.
5. **A proteção da `auditoria_crud` é por trigger, não por permissão.** O plano dizia "o papel da aplicação não tem UPDATE nem DELETE nela". Mas a aplicação e as migrations usam a mesma role (a migration 091 do ledger já parte desse fato), então revogar permissão não impediria nada. A regra foi corrigida no plano: triggers `BEFORE UPDATE/DELETE/TRUNCATE` bloqueiam, como no ledger.
6. **Hash encadeado por entidade** (como o ledger por agregado): cada linha guarda `sequencia`, `hash_anterior` e `hash`. Um advisory lock por entidade, dentro do trigger, serializa gravações concorrentes da mesma entidade; o índice único `(entidade_tipo, entidade_id, sequencia)` impede bifurcação. O hash cobre quem, quando, o quê, antes, depois, motivo, sequência e hash anterior. `verificar_cadeia_auditoria_crud` confere uma entidade.
7. **Senha e código fora da auditoria, com falha fechada.** Se `antes`/`depois` trouxer uma chave proibida (`senha`, `senha_hash`, `password`, `password_hash`, `hash_senha`, `codigo`, `codigo_verificacao`, `codigo_2fa`, `token`, `segredo`, `secret`, em qualquer nível, sem diferenciar maiúsculas), a gravação **falha** e a transação inteira volta atrás — em vez de apagar o campo em silêncio, o que esconderia o erro do programador. A checagem existe duas vezes (Go e trigger do banco) e um teste garante que as duas listas são iguais. Só a chave exata é proibida: `senha_alterada_em` passa.
8. **`ComAuditoria` usa transação serializable com repetição**, o mesmo padrão e o mesmo recuo limitado de `SaveWithAudit`, porque handlers em CRUD tipicamente leem e depois alteram. Por isso a função passada tem de poder ser repetida (sem e-mail, SMS ou arquivo dentro dela); está documentado no código. Quem precisar de transação própria usa `NovoAuditor` (o advisory lock continua protegendo a cadeia).
9. **Ação em nome de outra conta** (secretário como academia): `ComAuditoriaComAtor` grava quem realmente agiu em `em_nome_de_id`/`em_nome_de_tipo`, igual a `AtorAuditoria` no ledger.
10. **Endpoints de auditoria atuais** (`/eventos-estudante/:codigo`, `/eventos/:event_id`): **decisão adiada para a primeira tarefa de conversão**, porque hoje nada está em CRUD e portanto nada ainda precisa aparecer neles. Esta tarefa entrega só a leitura em Go (`ListarAuditoria`), sem rota HTTP, para não alterar a API sem um agregado que a use.
11. **Parte 2 ainda "Parcial" (falta o pacote `finance`).** A regra 4 do plano diz que uma dependência só está liberada quando a Parte de que se depende está Implementada. A 2B-1 depende da Parte 2, mas o conflito que o plano quer evitar é nos arquivos `handlers`, `safe_queries.go` e `main.go`, e esta tarefa não toca em nenhum deles. As **conversões** (133 em diante) tocam e, por isso, **devem esperar a Parte 2 ser concluída**, ou ser aplicadas com atenção ao movimento do pacote `finance`; veja a pergunta no fim.
12. **Nada se migra.** Sem academias cadastradas, não há dado a mover; a migration só cria a tabela, as funções e os triggers.

## O que foi atualizado e onde

**1 arquivo alterado + 6 novos.**

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `migrations/142_auditoria_crud.sql` (**novo**) | tabela `auditoria_crud` (linha 28); índices (51–58); `auditoria_crud_chave_proibida` (68); `auditoria_crud_calcular_hash` (109); `auditoria_crud_antes_de_inserir` + trigger (155/197); `auditoria_crud_somente_insercao` + 3 triggers (202–224); `verificar_cadeia_auditoria_crud` (230) | Tabela só de inserção, com hash encadeado por entidade, recusa de chaves proibidas e função de verificação da cadeia. Usa o `digest` do pgcrypto, que o ledger já usa. |
| `internal/db/auditoria_crud.go` (**novo**) | `ChavesProibidasAuditoria` (55), `EntradaAuditoria` (66), `Auditor` (78), `NovoAuditor` (86), `Registrar` (92), `serializarAuditoria` (136), `chaveProibida` (157), `ComAuditoria` (186), `ComAuditoriaComAtor` (192), `comAuditoriaUmaVez` (214), `LinhaAuditoria` (231), `ListarAuditoria` (251), `VerificarCadeiaAuditoria` (271) | Auxiliar transacional: alteração e auditoria entram ou saem juntas; recusa chave proibida; leitura e verificação da cadeia. |
| `internal/db/politica_eventsourcing.go` (**novo**) | `AgregadosEventSourcingAprovados` (42), `ConversaoPendenteAgregados` (57), `EventosEventSourcingAprovados` (74), `ConversaoPendenteEventos` (143), `EventosConvertidosHistoricos` (241), `ClassificarEventType` (262), `ClassificarAggregateType` (275), `EventosPendentesDeConversao` (295), `ViolacoesPoliticaEventSourcing` (326) | Regra 17 em código: lista aprovada, pendências por entrega e a lógica do teste-guarda. |
| `internal/db/politica_eventsourcing_test.go` (**novo**) | `TestPoliticaEventSourcingRespeitadaPeloCodigo` (24), `…ExatamenteUmaVez` (39), `…AgregadoPendenteNaoTemEventoAprovado` (61), `…SensivelContinuaAprovado` (95), `…GuardaReprovaTipoNovoForaDaLista` (110), `TestEventosPendentesDeConversao…` (187) | O guarda com os dados reais e com entradas sintéticas (prova que ele reprova o que deve). Reaproveita a varredura de tipos de `safe_queries_test.go`. |
| `internal/db/auditoria_crud_test.go` (**novo**) | `TestSerializarAuditoria…` (18, 49), `TestChavesProibidasAuditoriaIguaisAsDaMigration` (71), `TestComAuditoriaAlteracaoEAuditoriaEntramJuntas` (146), `…FalhaDepoisDaAlteracaoDesfazTudo` (181), `TestAuditoriaCrudCadeiaDeHashEOrdem` (256), `…EhSomenteInsercao` (323), `…DetectaAdulteracao` (346), `…BancoRecusaChaveProibida` (374), `…ConcorrenciaNaMesmaEntidadeNaoBifurca` (402), `…AdvisoryLockProtegeTransacaoPropriaReadCommitted` (448) | Testes unitários e de integração com PostgreSQL (rodam com `RUN_POSTGRES_INTEGRATION=1`; sem ele, os de banco são ignorados). |
| `docs/Lista de Tarefas/132 - Parte 2B (2B-1) - regra 17, teste-guarda e auditoria do CRUD.md` (**novo**) | — | Este documento. |
| `docs/Planos de Atualização/Plano de Atualizações 1.md` (**alterado**) | Parte 2B: Estado → Parcial; dois itens de **Entregas** marcados `[x]` (Tarefa 132); regra da auditoria corrigida (trigger em vez de permissão); regra do teste-guarda com o que foi feito; linha 2B da Visão geral; próxima tarefa livre → 133; nova linha no Registro de andamento | Passos 1 a 5 e 8 do plano aplicados. |

**Nenhum arquivo precisa ser apagado, movido ou renomeado.**

**Contrato HTTP:** sem alteração.

## Validação realizada

**Ambiente:** Go 1.27.4 e PostgreSQL 16.15 (sandbox; as dependências Go vieram de clones dos repositórios no GitHub, versões idênticas às do `go.sum`, porque o proxy de módulos não é acessível aqui). Todos os bancos de teste foram criados vazios e as 142 migrations aplicadas do zero.

**Base:** clone novo de `main` @ `ceba7e3` + a pasta `rastreio-backend/` copiada por cima.

| Comando | Resultado |
| --- | --- |
| `gofmt -l .` | lista vazia |
| `go build ./...` | ok |
| `go vet ./...` | ok |
| `RUN_POSTGRES_INTEGRATION=1 SPURI_RUN_DB_INTEGRITY_TESTS=1 go test ./... -p 1` (banco novo) | 28 pacotes `ok`, 0 `FAIL`, 9 sem testes |

**Comparação por teste (`-v`, banco novo, 1.ª execução):**

| | `--- PASS` | `--- FAIL` | `--- SKIP` |
| --- | --- | --- | --- |
| `main` @ `ceba7e3` puro | 799 | 0 | 2 |
| com a Tarefa 132 | 817 | 0 | 2 |

Testes que passavam em `main` e deixaram de passar: **0**. Testes novos: **18** (todos em `internal/db`: 6 da política, 12 da auditoria, contando os 4 unitários).

**Segunda execução no mesmo banco:** ver a seção "Falha antiga" abaixo; as falhas são exatamente as mesmas de `main` puro.

**Ciclo reverter → falhar → restaurar** (cada arquivo restaurado foi conferido com `cmp` contra o entregue):

| Regra desfeita | Teste que falhou |
| --- | --- |
| Tipo novo (`XptoCriado`) na whitelist sem estar na política | `TestPoliticaEventSourcingRespeitadaPeloCodigo`: "evento XptoCriado: está na whitelist mas não consta na lista aprovada … (regra 17)"; restaurado, volta a `ok` |
| Advisory lock por entidade removido do trigger | `TestAuditoriaCrudAdvisoryLockProtegeTransacaoPropriaReadCommitted`: "duplicate key value violates unique constraint idx_auditoria_crud_entidade_sequencia"; com o lock, passa 3 vezes seguidas |
| Go deixa de recusar chave proibida | `TestSerializarAuditoriaRecusaChavesProibidas` e `TestComAuditoriaFalhaDepoisDaAlteracaoDesfazTudo` |
| Banco (trigger) deixa de recusar chave proibida | `TestAuditoriaCrudBancoRecusaChaveProibida` |
| Trigger que bloqueia DELETE desligado | `TestAuditoriaCrudEhSomenteInsercao`: "DELETE: err = <nil>, want append-only" |

Observação honesta sobre a concorrência: `TestAuditoriaCrudConcorrenciaNaMesmaEntidadeNaoBifurca` (que usa `ComAuditoria`) **continua passando sem o advisory lock**, porque a transação serializable e a repetição já resolvem a colisão. Por isso foi acrescentado o teste em read committed, que é o que de fato prova o lock.

**Não validado:** nenhuma conversão de agregado (não faz parte desta tarefa); a integração com o Neon de produção (só PostgreSQL local); o front-end (não foi lido nem alterado).

## Falha antiga que não é desta tarefa

Rodar a suíte **uma segunda vez no mesmo banco** faz **56 testes falharem em `main` puro** (`spuri/cmd/server` e `spuri/internal/handlers/administradores`), por exemplo `rebuild turmas: erro no evento …: violates foreign key constraint "projection_turmas_codigo_academia_fkey"`. A causa é o ledger acumular eventos de testes anteriores que o `rebuild` tenta reprocessar contra projeções já limpas. Com a Tarefa 132, a 2.ª execução falha nos **mesmos 56 testes** (a comparação por nome deu diferença vazia) e nenhum teste novo falha; os testes novos desta tarefa passam na 2.ª execução. Recomendação: rodar a suíte completa sempre em banco novo. A causa não foi corrigida aqui (fora de escopo).

## Como aplicar

1. **Confirme que a base não mudou:** `git fetch origin && git diff --stat ceba7e3 origin/main -- "docs/Planos de Atualização/Plano de Atualizações 1.md" migrations internal/db`. Vazio = pode substituir. Se aparecer o plano, ele mudou depois da base: não substitua, avise. Se aparecer `migrations`, confirme que o número 142 continua livre (`ls migrations | grep ^142`).
2. **Copie** a pasta `rastreio-backend/` para a raiz do repositório, mantendo os caminhos. Só o plano é substituído; o resto são arquivos novos.
3. **Verifique** (abaixo).
4. **Marque como feito** (abaixo).

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./internal/db/ -run 'Politica|Auditoria|Serializar|ChavesProibidas|Registrar' -v
RUN_POSTGRES_INTEGRATION=1 DATABASE_URL='postgres://…banco-de-teste-descartavel…' go test ./internal/db/ -run 'Auditoria|Politica' -v
RUN_POSTGRES_INTEGRATION=1 DATABASE_URL='postgres://…banco-de-teste-descartavel…' go test ./... -p 1
```

Esperado: `gofmt -l` vazio; build e vet limpos; sem `RUN_POSTGRES_INTEGRATION=1` os testes de banco são ignorados e os unitários e os da política passam; com ele, todos passam. Use sempre um **banco novo e descartável** (nunca o de produção) para a suíte completa.

## Fora de escopo

- **A conversão dos agregados para CRUD** (item 3 da 2B-1): cursos, matérias, turmas, sumários, documentos extra, categorias de nota, configurações, comunicação, dados cadastrais não críticos e dados das contas de pessoa. Tarefas seguintes (133 em diante).
- **2B-2** (senha, 2FA, e-mail e senha temporária fora do ledger) e **2B-3** (financeiro operacional, condicional) e a remoção dos tipos antigos da whitelist e do replay.
- **Rota HTTP de auditoria** e a mudança dos endpoints `/eventos-estudante/:codigo` e `/eventos/:event_id` (decisão adiada para a primeira conversão).
- **Parte 7** (`ServicoExtra` e `CategoriaServico` em CRUD) e **Parte 2** (pacote `finance`).
- **Front-end:** nenhuma mudança; não há tarefa correspondente no `rastreio-frontend`.
- **Correção da fragilidade da 2.ª execução da suíte** no mesmo banco.
- `Documentação da API.md` e `Dockerfile`: sem alteração (API intacta).

## Marcar como feito

1. `**Estado:** feito`, `(feito)` no título.
2. Secção **Resultado** no fim: um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mover para `docs/Tarefas feitas/`, mesmo nome.

No plano, o Estado da Parte 2B continua **Parcial** até as conversões e a 2B-2 estarem marcadas.

## Checklist

- [ ] Base conferida (`ceba7e3`)
- [ ] Arquivos copiados (1 alterado, 6 novos); migration 142 sem conflito de número
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test ./...` em banco novo — todos `ok`
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`
