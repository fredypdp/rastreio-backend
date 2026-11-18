# Tarefa 128 — Remover a matrícula pública e a taxa de matrícula (Parte 1 do Plano de Atualizações) (backend)

**Estado:** pendente
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `127006a`
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório. **9 arquivos precisam ser apagados** (lista em "Como aplicar").
**Ordem de deploy:** esta tarefa vai para produção **depois** da Tarefa 38 do `rastreio-frontend`. O frontend atual (antes da Tarefa 38) ainda tem a página pública `/matricula`, a tela de solicitações e as telas de taxa de matrícula; com este backend em produção elas passam a receber `404`. Com a Tarefa 38 já no ar, nenhuma tela chama as rotas removidas.

---

## Contexto

A Parte 1 do `docs/Plano_de_Atualizacoes.md` pede a remoção da matrícula pública: a solicitação de matrícula feita pelo próprio estudante (criar, buscar, consultar estado, pagar a matrícula), a gestão dessa solicitação pela academia e pelo admin, o download dos documentos da solicitação, o agregado e a projeção `SolicitacaoMatricula`, e a taxa de matrícula do módulo financeiro. O estudante passa a ser cadastrado só pela academia (`POST /academia/estudante/register` e cadastro em lote). Não há academias cadastradas, então não há dados de produção a preservar.

Havia código compartilhado dentro dos arquivos da solicitação (validação de telefones, bilhetes e documentos; leitura de PDF; geração de código de solicitação; quota de armazenamento). Ele foi preservado em arquivos próprios antes de apagar o resto.

## Decisões de design

1. **Ledger imutável:** nenhum evento é apagado. Os eventos antigos `SolicitacaoMatricula*`, `MatriculaConfigurada` e `MatriculaConfiguracaoRemovida` deixam de poder ser **gravados** (saem da lista de eventos permitidos e o tipo de agregado `SolicitacaoMatricula` sai da factory e da lista de agregados).
2. **Replay financeiro tolerante:** se algum ambiente tiver `MatriculaConfigurada`/`MatriculaConfiguracaoRemovida` no ledger, a projeção financeira passa por esses dois eventos sem efeito (retorna `nil`), para um rebuild não parar num evento antigo. É o único "código de compatibilidade" que sobra, com teste próprio. Antes do deploy, confira se o ledger tem esses eventos (consulta em "Como aplicar").
3. **Migration 138 só apaga tabelas de leitura (projeções)**, com `DROP … IF EXISTS` e **sem `CASCADE`**: se algo desconhecido depender delas, a migration falha de forma visível em vez de apagar em silêncio.
4. **Cobrança de matrícula deixa de existir como tipo:** o filtro `tipo=matricula` passa a ser recusado (`400`), a origem derivada de uma cobrança é `mensalidade`, `servico_extra` ou `avulsa`, e o campo `codigo_solicitacao` sai do resumo de cobrança.
5. **Rota pública de documentos extra removida:** `GET /academia/documento/:codigo_academia/documentos-extra` só servia a tela pública de matrícula. A academia autenticada continua usando `GET /academia/documentos-extra`, e o upload de documentos extra no cadastro feito pela academia não foi tocado.
6. **Gerador de código de solicitação** (`gerarCodigoSolicitacao`) passa a ser puramente aleatório: antes consultava a tabela da solicitação de matrícula (que sai). As solicitações de edição de dados e de NIF já conferem a unicidade na própria tabela.
7. **Testes que usavam a cobrança de matrícula só como exemplo** (provedor, webhook, reconciliação, listagens) passaram a emitir a cobrança pelo mesmo `gerarCobranca` usado por mensalidade e serviço extra, via helpers de teste. Os testes cuja regra era a própria taxa de matrícula foram apagados.

## O que foi atualizado e onde

**61 arquivos alterados + 7 novos (5 criados e 2 renomeados; 68 entregues) + 9 a apagar.** Números de linha são os do arquivo entregue.

### Novos e movidos

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `internal/domain/aggregates/documentos_matricula.go` (**novo**) | linhas 1-287 | Recebe, sem mudança de comportamento, o que o cadastro da academia usa e vivia em `solicitacao_matricula.go`: status `pendente/aprovada/reprovada` (linha 15, compartilhados com edição de dados e NIF), `ValidarTelefonesMatricula`, `DocumentoMatricula` e a política de documentos (`ValidarDocumentosMatricula`, linha 143). Sem os status `cancelada` e `aprovada_pendente_pagamento_matricula`. |
| `internal/handlers/matricula_cadastro_helpers.go` (**novo**) | linhas 1-260 | Recebe os auxiliares do cadastro da academia que viviam no handler da solicitação: `readAndValidatePDF` (68), `uploadedPDF`, `MaxPDFUploadBytes`, normalização de documentos, `validarCursosMatriculaCommon`, `validateBIEncarregadoNaoConflitaComEscolar`, `parseBoundedInt`, `storagePathDocumentoEstudante` e o novo `gerarCodigoSolicitacao` (249). |
| `internal/handlers/storage_quota_handlers.go` (**novo**) | `GetStorageQuota` (12) | A rota de admin de quota de armazenamento, que não tem relação com matrícula, sai do handler apagado para o seu arquivo. |
| `internal/handlers/matricula_cadastro_helpers_test.go` (**renomeado**) | — | Antes `solicitacao_matricula_handlers_test.go`; conteúdo idêntico (testes de leitura de PDF). |
| `internal/handlers/financeiro_consulta_cobranca_test.go` (**renomeado + alterado**) | `TestIntegrationConsultarCobrancaAppyPayPersisteSuccess` | Antes `financeiro_matricula_consulta_test.go`. Passa a consultar uma cobrança avulsa e a conferir que o `Success` fica persistido. |
| `internal/projections/financeiro_projection_descontinuados_test.go` (**novo**) | linha 16 | Prova que os dois eventos financeiros descontinuados são atravessados sem erro e sem tocar o banco. |
| `migrations/138_remove_matricula_publica.sql` (**novo**) | — | Apaga `financeiro_matricula_configuracoes_atual` (visão), `financeiro_matricula_configuracoes_remocoes`, `financeiro_matricula_configuracoes` e `projection_solicitacoes_matricula`. |

### Alterados — produção

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `cmd/server/main.go` | linhas 180, 312, 316, 349, 448, 451, 497, 571, 576, 609, 717 (remoções) | Saem o registro da projeção, as rotas públicas `/solicitacao-matricula*`, a listagem pública de documentos extra, as rotas de academia e de admin da solicitação, os downloads de documentos da solicitação e `/financeiro/matriculas/configuracoes` (POST, PUT, GET, DELETE). |
| `internal/domain/aggregates/aggregate.go` | factory de agregados (linha 150) | Sai o `case "SolicitacaoMatricula"`. |
| `internal/domain/aggregates/financeiro.go` | constantes de evento (linha 55) | Saem `MatriculaConfigurada` e `MatriculaConfiguracaoRemovida`. |
| `internal/domain/aggregates/documento_extra.go` | linhas 21-22 e 130 | Só comentários: deixam de citar a solicitação de matrícula. |
| `internal/db/safe_queries.go` | linhas 109, 164, 196-197, 225 | Saem da lista de eventos permitidos os 9 eventos de matrícula e o tipo de agregado `SolicitacaoMatricula`. |
| `internal/projections/financeiro_projection.go` | `case` das linhas 101-105 e `Rebuild` (356) | Os dois eventos descontinuados passam a ser ignorados no replay; o `Rebuild` deixa de limpar as tabelas de matrícula. |
| `internal/projections/manager.go` | linha 356 | Sai `solicitacoes_matricula` da lista de projeções reconstruíveis. |
| `internal/projections/projection.go` | linhas 8 e 61 | Sai o auxiliar `nullOrUUID`, que só a projeção apagada usava. |
| `internal/finance/cobranca_geracao.go` | linhas 10-14, 39-42, 56-57 e remoções | Sai `CodigoSolicitacao` do pedido de geração de cobrança e do payload; comentários atualizados. |
| `internal/finance/servico.go` | `origensClause` (632), derivação de origem (693), pedido de cobrança e QR Code, consulta por estudante | Sai a origem `matricula`, `codigo_solicitacao` do resumo e dos payloads, e o cruzamento SQL com `projection_solicitacoes_matricula`. A listagem por estudante passa a filtrar só por `codigo_estudante`. |
| `internal/finance/servico_extra.go`, `mensalidade.go`, `mensalidade_pendencias.go`, `pagamento_externo.go`, `pagamento_disponibilidade.go`, `pagamentos_unificado.go`, `provedor.go`, `provedor_appypay.go`, `provedor_config.go`, `credenciais.go`, `reconciliacao_job.go`, `reconciliacao_registro.go` | só comentários | Deixam de citar `IniciarPagamentoMatricula`, `CancelarCobrancaMatriculaAberta` e a taxa de matrícula. |
| `internal/handlers/efeitos_pagamento.go` | `NovosEfeitosPagamento` (54) | Sai a efetivação da matrícula paga; ficam só os efeitos de serviço extra. |
| `internal/handlers/financeiro_pagamento_externo_handlers.go` | linhas 16-17 e 25 | Idem, no pagamento marcado como feito fora da plataforma. |
| `internal/handlers/financeiro_handlers.go` | linha 179 e 192 | Sai a consulta de cobrança de matrícula no polling; comentários atualizados. |
| `internal/handlers/mensalidade_handlers.go` | linha 117 | Saem os handlers de configuração da taxa de matrícula. |
| `internal/handlers/documento_download_handlers.go` | `ListarDocumentosAcademia` (77) e remoções | Saem os downloads de documento de solicitação e as chaves `solicitacoes_matricula`, `total_solicitacoes`, `limit_solicitacoes` e `offset_solicitacoes` da resposta da academia. |
| `internal/handlers/documento_extra_handlers.go` | linha 202 | Sai `ListarDocumentosExtraPublico`. |
| `internal/handlers/documento_extra_upload.go` | linha 150 | Só comentário. |
| `internal/handlers/anos_academicos_handlers.go` | `dependenciasAnoFundamental` (140) | Sai a contagem de "solicitações de matrícula pendentes" das dependências que bloqueiam mudanças no ano. |
| `internal/handlers/nivel_escolar_handlers.go` | dependências (173) e mensagem (53) | Idem para a mudança do nível escolar. |
| `internal/handlers/helpers.go` | linha 150 | Sai `getSolicitacaoMatriculaProjection`. |
| `internal/handlers/solicitacao_edicao_dado_estudante_handlers.go` (527) e `solicitacao_alteracao_nif_academia_handlers.go` (280) | geração do código | Passam a chamar `gerarCodigoSolicitacao()`. |
| `internal/services/matricula_validation.go` | linha 14 | Sai a constante de contexto `solicitacao_matricula`. |
| `Documentação da API.md` | seção 9 (4166), 19.21-19.22 (8445), 19.26 (8525) e menções espalhadas | Seção 9 e as rotas de taxa de matrícula viram notas de "removida" (com a lista das rotas que passam a responder `404`); tabelas de rotas, filtros `tipo`/`origem`, DTOs e regras de documentos atualizados. |

### Alterados — testes

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `internal/db/safe_queries_test.go` | 291, 317 | Dois testes novos: os 9 eventos e o tipo `SolicitacaoMatricula` são rejeitados. |
| `cmd/server/main_test.go` | `TestPublicEnrollmentRoutesAreRemoved` (396) | Novo: as 18 rotas removidas respondem `404` (e não `401/403`). Saem duas URLs de download antigas de outro teste. |
| `internal/domain/aggregates/bilhetes_validation_test.go` | linhas 54 e 312 | Saem os dois testes da solicitação e o auxiliar que só eles usavam; a regra de bilhetes iguais continua coberta pelo teste do cadastro do estudante. |
| `internal/projections/manager_rebuild_test.go` | 53, 78, 83 | Sai `solicitacoes_matricula` da ordem de rebuild. |
| `internal/finance/appypay_integration_test.go` | `obrigacaoTeste` (108) e usos | Helpers de teste (`novaObrigacaoTeste`, `iniciarCobrancaTeste`, `cancelarCobrancaTeste`) emitem a cobrança pelo `gerarCobranca`; sai o teste exclusivo da regra de valor fixo da matrícula; o teste de webhook tardio ganha nome genérico. |
| `internal/finance/cobrancas_estudante_integration_test.go` (15), `cobrancas_integration_test.go` (16) | listagens | Passam a cobrir mensalidade e serviço extra; verificam que `tipo=matricula` é recusado. |
| `internal/finance/cobranca_geracao_integration_test.go`, `qrcode_regression_integration_test.go`, `credenciais_snapshot_integration_test.go`, `reconciliacao_integration_test.go`, `pagamentos_unificado_test.go`, `financeiro_ledger_integrity_test.go`, `modo_vigencia_primeira_configuracao_integration_test.go`, `qrcode_pagamento_view_test.go` | vários | Trocam a cobrança de matrícula pelo helper; o teste de rebuild do ledger passa a usar a configuração de mensalidade; saem os testes que só existiam para a taxa de matrícula. |
| `internal/handlers/financeiro_handlers_integration_test.go` | `novaCobrancaAvulsaREF` (109), webhook (237) | Helper novo; o teste de webhook passa a confirmar uma cobrança avulsa e a provar a idempotência (1 evento no ledger). Sai o teste da busca pública. |
| `internal/handlers/efeitos_pagamento_integration_test.go` | linha 21 | Passa a provar que o adaptador do job roda fora de uma requisição, é repetível e não grava eventos fora do financeiro para uma cobrança avulsa. |
| `internal/handlers/financeiro_pagamento_externo_integration_test.go` | fixture e testes | Fixture com cobrança avulsa; verificações trocam "estudante criado" por "status da cobrança = Success". |
| `internal/handlers/documento_download_handlers_test.go`, `financeiro_pendencias_handlers_test.go`, `financeiro_provedores_handlers_integration_test.go`, `financeiro_remocao_handlers_integration_test.go`, `metodos_pagamento_pagador_integration_test.go`, `seguranca_codigo_test.go`, `unique_operation_guard_integration_test.go` | vários | Saem as partes da solicitação/taxa de matrícula; o filtro `tipo=matricula` dos testes de pendências passa a `tipo=servico_extra`. |

### Contrato de API

- **Passam a responder `404`:** `POST /solicitacao-matricula`, `GET /solicitacao-matricula/busca`, `GET /solicitacao-matricula/:codigo/status`, `POST /solicitacao-matricula/:codigo/pagamento-matricula`, `GET /solicitacoes-matricula`, `GET /dominis/solicitacoes-matricula`, `GET /academia/solicitacoes-matricula`, `GET /academia/solicitacao-matricula/:codigo`, `PUT /academia/solicitacao-matricula/:codigo/{aprovar,reprovar,cancelar}`, os dois downloads `…/solicitacoes-matricula/:codigo/:campo/download`, `GET /academia/documento/:codigo_academia/documentos-extra` e `POST/PUT/GET/DELETE /financeiro/matriculas/configuracoes`.
- **Resposta que mudou:** a lista de documentos da academia deixa de trazer `solicitacoes_matricula`, `total_solicitacoes`, `limit_solicitacoes` e `offset_solicitacoes`. O resumo de cobrança deixa de trazer `codigo_solicitacao`. `tipo=matricula` é recusado com `400`.
- O frontend atual não lê nenhum desses campos.

## Validação realizada

**Ambiente:** Go 1.24.13 (o `go.mod` pede o toolchain 1.24.12; o sandbox só tem a 1.24.13 via apt) e PostgreSQL 16. O proxy de módulos do Go está bloqueado no sandbox, então as dependências foram resolvidas por espelhos do GitHub num `go.mod` **alternativo, fora do repositório** (`-modfile`). O `go.mod` e o `go.sum` reais não foram tocados e não fazem parte da entrega.

**Baseline de `main` @ `127006a`** (sem a mudança, banco novo): `go build`, `go vet` e `go test -p 1 ./...` com `RUN_POSTGRES_INTEGRATION=1` — todos os pacotes `ok`.

**Clone novo de `main` @ `127006a`, com a pasta `rastreio-backend/` copiada por cima e os 9 arquivos apagados:**

```
gofmt -l .        -> (vazio)
go build ./...    -> rc=0
go vet ./...      -> rc=0
go test -p 1 -count=1 ./...   (1ª execução, banco novo)
ok  spuri/cmd/server  ok  spuri/internal/db  ok  spuri/internal/domain/aggregates
ok  spuri/internal/finance  ok  spuri/internal/handlers  ok  spuri/internal/middleware
ok  spuri/internal/projections  ok  spuri/internal/security  ok  spuri/internal/services
ok  spuri/internal/storage  ok  spuri/internal/utils
go test -p 1 -count=1 ./...   (2ª execução, MESMO banco) -> todos ok
```

- **Migrations do zero:** as migrations, incluindo a `138`, aplicam num banco novo. Depois delas, as três tabelas antigas e a visão não existem (consulta ao `information_schema` devolveu `0`).
- **Integração rodou de verdade** (não foi pulada): `-v` dos testes de pagamento externo, webhook e efeitos mostra `--- PASS` (nenhum `SKIP`).

**Regras novas: desfazer → falhar → restaurar** (cada regra foi desfeita no código, o teste foi executado e caiu, e depois a regra foi restaurada e o teste voltou a passar):

| Regra desfeita | Teste que falhou |
| --- | --- |
| Voltar `SolicitacaoMatriculaCriada` à lista de eventos permitidos | `TestValidateEventTypeRejectsRemovedPublicEnrollmentEvents` |
| Voltar o tipo de agregado `SolicitacaoMatricula` à lista | `TestValidateAggregateTypeRejectsRemovedSolicitacaoMatricula` (`retornou nil, want erro`) |
| Registrar de novo uma rota `/solicitacao-matricula` | `TestPublicEnrollmentRoutesAreRemoved` |
| Tirar o `case` que ignora os eventos financeiros descontinuados | `TestFinanceiroProjectionAtravessaEventosDescontinuadosDaTaxaDeMatricula` |
| Voltar a aceitar `matricula` em `origensClause` | `TestIntegrationListCobrancasFiltraOrigemEstadoEIsolaPorAcademia` / `…EstudanteIncluiMensalidadeEServicoExtra` |

Uma varredura de declarações sem nenhuma referência, antes e depois, achou um único órfão novo (`nullOrUUID`), que foi removido.

**Não foi validado:** o deploy real (o ledger de produção/homologação não estava acessível); a consulta SQL abaixo precisa ser corrida por você antes do deploy.

## Falha antiga que não é desta tarefa

Rodar **sozinho** o pacote `internal/handlers` duas vezes seguidas no mesmo banco (ou só `TestIntegrationConsultarCobrancasEstudante*`) faz 5 testes falharem na segunda vez, porque contam cobranças que ficaram da execução anterior. Acontece igual em `main` sem a mudança. A suíte inteira (`./...`, onde o pacote `finance` roda antes) passa duas vezes no mesmo banco, como mostrado acima.

## Como aplicar

1. **Confirme que a base não mudou:** `git diff --stat 127006a HEAD -- cmd internal migrations "Documentação da API.md"`. Vazio = pode substituir; se aparecer algum arquivo, ele mudou depois da base — não substitua e avise. Confirme também que `migrations/` ainda termina em `137_…` (esta tarefa cria a `138`).
2. **Apague** estes 9 arquivos (dois deles foram renomeados e voltam com o nome novo na pasta entregue):
   - `internal/domain/aggregates/solicitacao_matricula.go`
   - `internal/domain/aggregates/solicitacao_matricula_pagamento_test.go`
   - `internal/finance/matricula.go`
   - `internal/finance/matricula_remocao_integration_test.go`
   - `internal/handlers/solicitacao_matricula_handlers.go`
   - `internal/handlers/solicitacao_matricula_handlers_test.go` (renomeado para `matricula_cadastro_helpers_test.go`)
   - `internal/handlers/financeiro_matricula_consulta_test.go` (renomeado para `financeiro_consulta_cobranca_test.go`)
   - `internal/projections/solicitacao_matricula_projection.go`
   - `internal/projections/solicitacao_matricula_projection_test.go`
3. **Copie** a pasta `rastreio-backend/` para a raiz do repositório, mantendo os caminhos.
4. **Antes do deploy**, no banco do ambiente, confira se o ledger tem eventos da matrícula pública (esperado: nenhuma linha):

```sql
SELECT aggregate_type, event_type, count(*)
FROM spuri_ledger
WHERE aggregate_type = 'SolicitacaoMatricula'
   OR event_type LIKE 'SolicitacaoMatricula%'
   OR event_type IN ('MatriculaConfigurada', 'MatriculaConfiguracaoRemovida')
GROUP BY 1, 2;
```

Se aparecerem linhas, o replay financeiro já está tolerante aos dois eventos de taxa; os eventos `SolicitacaoMatricula*` ficam no ledger sem projeção. Avise antes de continuar.
5. **Verifique** (abaixo) e **marque como feito** (abaixo).

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
RUN_POSTGRES_INTEGRATION=1 DATABASE_URL=postgres://… go test -p 1 ./...
```

Esperado: `gofmt` sem saída; `build` e `vet` limpos; todos os pacotes `ok`. A migration `138` é aplicada na subida do servidor. Os testes `TestIntegration…` só rodam de verdade com um PostgreSQL de teste descartável.

## Fora de escopo

- Frontend: coberto pela Tarefa 38 do `rastreio-frontend`.
- A listagem pública de serviços extras (`ListarServicosExtrasPublico`) e o cadastro público de academia (`POST /academia/cadastro`) não fazem parte da matrícula pública e ficam como estão.
- Renomear os nomes que ainda dizem "matrícula" mas pertencem ao cadastro feito pela academia (`DocumentoMatricula`, `ValidarDocumentosMatricula`, `ValidateMatriculaCommon`) e o helper de teste `seedAcademiaParaMatriculaWebhook`.
- Apagar eventos do ledger (imutável) ou arquivos antigos do storage sob `matriculas/` (não há academias cadastradas).
- Teste de integração dos efeitos de pagamento do serviço extra (taxa de inscrição e lançamentos): não existia antes e continua sem existir. O teste de efetivação da matrícula que cobria o adaptador de efeitos foi substituído pelo equivalente com cobrança avulsa.
- Marcar os itens da Parte 1 em `docs/Plano_de_Atualizacoes.md` (você marca depois de aplicar) e as Partes seguintes do plano.

## Marcar como feito

1. `**Estado:** feito`, `(feito)` no título.
2. Secção **Resultado** no fim: um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mover para `docs/Tarefas feitas/`, mesmo nome.

## Checklist

- [ ] Base conferida (`127006a`; migrations terminam em `137`)
- [ ] 9 arquivos apagados; pasta copiada (61 alterados, 7 novos — 2 deles renomeados)
- [ ] Consulta SQL do ledger executada (sem linhas, ou avisado)
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test -p 1 ./...` — todos `ok`
- [ ] Tarefa 38 do `rastreio-frontend` já em produção antes deste deploy
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`
