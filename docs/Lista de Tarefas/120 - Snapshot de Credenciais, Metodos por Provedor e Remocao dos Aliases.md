# Tarefa 120 — Snapshot de credenciais removidas, métodos por provedor e remoção dos aliases deprecados (backend)

**Estado:** pendente
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `758e714` (commit completo `758e7143a67e710bd00a052e9d0898ae74e735cd`, "feat(financeiro): módulo de pagamentos dividido por provedores (PSP) - tarefa 119"). Conferi que o `main` aplicado contém exatamente os arquivos entregues na Tarefa 119 (0 divergências).
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório
**Ordem de deploy:** backend **antes** do frontend da Tarefa 30. O frontend atual (Tarefa 29) continua funcionando com este backend: ele já não usa nenhuma rota `/financeiro/appypay/*`, e a mudança da remoção de credenciais (de `409` para `200` com resumo) é compatível com a chamada que ele faz. A migration 132 roda no arranque do servidor.
**Antes de aplicar, confirme:** que nenhum outro cliente além do painel web (ex.: aplicação móvel, scripts) chama `/financeiro/appypay/*`. As URLs públicas de webhook `/webhooks/appypay/gpo` e `/ref` **não** mudam.

---

## O que foi pedido

1. Remover as rotas deprecadas `/financeiro/appypay/*`.
2. O seletor de métodos de propina e matrícula passa a mostrar só os métodos habilitados do provedor definido.
3. Remover credenciais com cobranças pendentes deixa de ser bloqueado: permitir **com aviso** e guardar um **snapshot** (ou mecanismo semelhante) que permita continuar a executar a retroatividade nas cobranças e eventos pendentes.
4. (Frontend) A configuração do provedor passa para `/financas/configuracoes` — Tarefa 30 do `rastreio-frontend`.
5. Explicação do job de reconciliação — respondida na conversa; **nenhuma implementação** nesta tarefa.

## Decisões de design

- **Snapshot = credenciais arquivadas.** Quando se removem as credenciais de um provedor que ainda tem cobranças pendentes no contexto, elas deixam a tabela ativa e passam para `financeiro_credenciais_arquivadas` (metadados + visão mascarada). Os **segredos continuam no cofre**, cifrados com a mesma chave (`financeiro_segredos_provedor`), em vez de serem copiados: nada fica duplicado e nenhum segredo vai para o ledger. O snapshot **só** serve para cobranças já emitidas (consulta, conferência ao vivo, cancelamento e autenticação dos webhooks); nunca para cobranças novas, para definir provedor ou para a listagem de credenciais.
- **Cada cobrança grava a credencial com que foi emitida** (`financeiro_cobrancas.credencial_id`, e `credencial_id` no payload do evento no ledger). É o vínculo exato entre uma cobrança e o seu snapshot, e deixa o evento auto-descritivo no replay. A resolução das credenciais de uma cobrança existente é, por ordem: (1) a credencial exata que a emitiu — ativa ou arquivada; (2) a credencial ativa do contexto + provedor (cobranças legadas); (3) o snapshot mais recente do contexto + provedor.
- **Descarte automático.** O snapshot é descartado — evento `CredenciaisArquivadasPurgadas` no ledger + limpeza do cofre — assim que a última cobrança que dependia dele é resolvida. O gatilho é único e fica em `record`: depois de gravar um evento que leva uma cobrança a estado terminal, tenta descartar. É de melhor esforço (uma falha nunca desfaz o evento já gravado) e idempotente. Uma cobrança legada sem credencial emitente segura apenas o snapshot mais recente.
- **A remoção nunca é bloqueada** e passa a responder `200` com `{provedor, cobrancas_pendentes, credenciais_arquivadas}` (antes `204`). O frontend usa isso para mostrar o aviso. Sem pendentes, tudo é apagado de imediato, como antes.
- **Trocar de conta num provedor sem perder pendentes** = remover e cadastrar de novo (o snapshot preserva as antigas). Editar uma credencial continua a substituir os segredos usados por todas as cobranças ligadas a ela — o frontend avisa disso.
- **Métodos mostrados ao pagador.** Dois pontos de leitura passam pelo provedor ativo da academia: `MetodosPagamentoMensalidade` (propina do estudante) e o status público da solicitação de matrícula. Sem provedor ativo com credenciais a lista vem vazia (não há como pagar); numa falha de infraestrutura devolve a lista sem filtrar (a geração da cobrança continua a validar). A validação interna de um pagamento continua a usar só a configuração.
- **Aliases removidos:** rotas, handlers legados (`ConfigurarCredencialAppyPay`, `AtualizarCredencialAppyPay`, `ListarCredenciaisAppyPay`, `RemoverCredencialAppyPay`), adaptadores de formato antigo e a seção 19.29 da documentação. A leitura dos **eventos antigos** do ledger (campos planos da AppyPay) **continua**, porque é dado, não rota.

## O que foi atualizado e onde

**20 arquivos alterados + 2 novos.** Nenhum arquivo é apagado, renomeado ou movido de diretório.

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `migrations/132_financeiro_credenciais_arquivadas.sql` (**novo**) | — | Coluna `credencial_id` em `financeiro_cobrancas` (+ índice); backfill só das cobranças **não terminais** cujo contexto + provedor tem **exatamente uma** credencial (as demais ficam `NULL`, e a resolução cai para contexto + provedor + ambiente); tabela `financeiro_credenciais_arquivadas`. Preserva todos os dados. |
| `internal/finance/credenciais.go` | `RemocaoCredencialResultado` (l. 248); `RemoveCredential` (l. 278); `AuthenticateWebhook` (l. 515; candidatas arquivadas na l. 540); `validarCofre` (l. 573); `loadCredentialPorID` (l. 585); `credencialComSegredos` (l. 597); `loadCredentialArquivada` (l. 612); `credencialDaCobranca` (l. 626); `credenciaisArquivadasDoContexto` (l. 645); `pendentesDoSnapshot` (l. 654); `purgarArquivadasSemPendentes` (l. 668) | Remoção sem bloqueio, com arquivo do snapshot e resumo; resolução das credenciais por cobrança (exata → ativa → snapshot); o webhook também autentica com o segredo de credenciais arquivadas do provedor; descarte dos snapshots sem pendentes (evento + cofre). Remove-se o erro `ErrCredenciaisEmUso`. |
| `internal/finance/servico.go` | `CreateCharge` (l. 256) e `CreateGPOQRCode` (l. 343): payloads com `credencial_id`; `record` (l. 957) + `liberarSnapshotsSeCobrancaTerminal` (l. 979); `chargeRow` (l. 1127); `loadChargeWhere` (l. 1150); `provedorDaCobranca` (l. 1174); `comCredencial` (l. 1188) | A cobrança grava a credencial emitente; `loadCharge*` lê-a; consulta, conferência, cancelamento e webhook resolvem as credenciais por `credencialDaCobranca`; o gatilho de descarte depois de eventos terminais. |
| `internal/finance/provedor_config.go` | `CredenciaisArquivadas` (l. 37); preenchimento (l. 77); `FiltrarMetodosPorProvedor` (l. 167) | `GET /financeiro/provedor` passa a informar `credenciais_arquivadas` por provedor; novo filtro dos métodos pelo provedor ativo. |
| `internal/finance/mensalidade.go` | `MetodosPagamentoMensalidade` (l. 896) | A lista que o estudante recebe passa pelo filtro do provedor ativo; a validação interna (`metodosPagamentoMensalidade`) fica sem filtro. |
| `internal/finance/provedor.go` | erros (l. 67 em diante) | Sai `ErrCredenciaisEmUso`. |
| `internal/domain/aggregates/financeiro.go` | constantes (l. 28) | Constante `CredenciaisArquivadasPurgadas`. |
| `internal/db/safe_queries.go` | l. 138 | O novo evento entra no allowlist do ledger. |
| `internal/projections/financeiro_projection.go` | `credencialDoPayload` (l. 65); `CredenciaisProvedorRemovidas` (l. 276); `CredenciaisArquivadasPurgadas` (l. 308); insert de cobranças (l. 337); Rebuild (l. 357) | A remoção com `arquivada: true` cria o snapshot (a sem a marca, dos eventos antigos, só apaga); o descarte apaga o snapshot; a cobrança projeta `credencial_id` (nunca é sobrescrito por eventos seguintes); o Rebuild limpa também os snapshots. |
| `internal/handlers/financeiro_provedores_handlers.go` | `RemoverCredencial` (l. 163); `removerCredencial` (l. 176; resposta na l. 186) | `DELETE` responde `200` com o resumo; **removidos** os handlers e tipos legados (`credencialAppyPayLegadaInput`, `CredencialAppyPayCriada`, as quatro funções `…AppyPay`). |
| `internal/handlers/financeiro_handlers.go` | `financeError` (l. 96) | Sai o mapeamento `409` de `ErrCredenciaisEmUso`. |
| `internal/handlers/mensalidade_handlers.go` | l. 312 | Só comentário: a lista de métodos já vem filtrada do Service. |
| `internal/handlers/solicitacao_matricula_handlers.go` | l. 449 | O status público da matrícula devolve só os métodos que o provedor ativo suporta. |
| `cmd/server/main.go` | rotas neutras (l. 431–440) | **Removido** o bloco de aliases `/financeiro/appypay/*`. Webhooks da AppyPay inalterados. |
| `Documentação da API.md` | versão 2.8.0 (l. 5); 19.23 (l. 8768) reescrita; 19.28 (l. 8915) ganha `credenciais_arquivadas`; 19.29 removida; regras gerais, 9.10 (status público da matrícula) e 19.17 (propina do estudante) atualizadas | Documenta o snapshot, a resposta `200` da remoção, o filtro de métodos do pagador e a remoção dos aliases. |
| `cmd/server/main_test.go` | `TestLegacyPaymentRoutesAreRemoved` (l. 356 e 377) | O teste existente afirmava que `/financeiro/appypay/credenciais` exigia token; agora afirma que as rotas deprecadas dão `404` e as neutras dão `401`. |
| `cmd/server/financeiro_provedores_rotas_test.go` | 1 teste | Afirma que **nenhuma** rota `/financeiro/appypay/*` está registrada (os webhooks `/webhooks/appypay/*` não contam). |
| `internal/finance/provedores_snapshot_integration_test.go` (**novo**) | 5 testes | Snapshot ponta a ponta (remoção, resolução com o snapshot, webhook com o segredo arquivado, convivência com credenciais novas, descarte), remoção sem pendentes, cobrança legada sem credencial emitente, métodos do pagador, replay da projeção. |
| `internal/finance/provedores_integration_test.go` | teste retroativo | Adaptado: remover as credenciais do provedor anterior com pendente passa a ser permitido (arquiva) e o snapshot é descartado ao resolver a pendente. |
| `internal/finance/appypay_remocao_integration_test.go` | duas chamadas | Nova assinatura de `RemoveCredential`. |
| `internal/handlers/financeiro_provedores_handlers_integration_test.go` | 3 testes | Removido o teste das rotas legadas; o do `409` virou "arquiva e avisa" (`200`); novo teste do status público da matrícula filtrado pelo provedor (idempotente no mesmo banco). |
| `internal/handlers/financeiro_remocao_handlers_integration_test.go` | chamadas | Usa o handler neutro `RemoverCredencial` (com `provedor`) e espera `200`. |

### Contrato de API

| Rota | Mudança |
| --- | --- |
| `DELETE /financeiro/credenciais` | `200` com `{"provedor":"APPYPAY","cobrancas_pendentes":2,"credenciais_arquivadas":true}` (antes `204`); nunca `409`; `404` se não houver credencial ativa; `403`; `400` provedor desconhecido. |
| `GET /financeiro/provedor` | Cada provedor ganha `credenciais_arquivadas` (boolean). |
| `GET /financeiro/mensalidades/estudante/:codigo` (documentação 19.17) | `metodos_pagamento_por_academia` só com métodos suportados pelo provedor ativo da academia; vazio sem provedor ativo. |
| `GET /solicitacao-matricula/:codigo/status` (documentação 9.10) | `metodos_pagamento` só com métodos suportados pelo provedor ativo; vazio sem provedor ativo. |
| `/financeiro/appypay/*` | **Removidas** (agora `404`). |

## Validação realizada

**Ambiente.** Go 1.24.13 (apt) e PostgreSQL 16.15 reais; o proxy de módulos do Go é bloqueado no sandbox, então usei um `go.mod` auxiliar (`-modfile`, só no sandbox) com espelhos no GitHub — o `go.mod` e o `go.sum` do repositório não foram tocados e não fazem parte da entrega. Variáveis: `RUN_POSTGRES_INTEGRATION=1`, `SPURI_RUN_DB_INTEGRITY_TESTS=1`, `ENV=test`, `APPYPAY_RESOURCE`, `JWT_SECRET`, `DB_*`.

**Baseline (`main` @ `758e714`), banco novo, `go test -count=1 -p 1 ./...`:** todos os pacotes `ok`, `EXIT=0`; 364 funções de teste em `finance`, `handlers` e `cmd/server`.

**Depois (clone novo de `main` @ `758e714`, pacote copiado por cima — sem nenhum arquivo a apagar —, como você vai aplicar):**

- `gofmt -l .` vazio; `go build ./...` e `go vet ./...` limpos.
- **Execução 1, banco novo** (migrations 001 a 132 do zero): todos os pacotes `ok`, `EXIT=0`; **369** funções de teste (364 + 5 novas; o teste das rotas legadas saiu, o do status da matrícula entrou).
- **Execução 2, no mesmo banco:** `finance`, `handlers`, `db` e demais pacotes passam; `cmd/server` falha com **25 testes — exatamente os mesmos 25** que falham em `main` sem a mudança no mesmo cenário (comparação das duas listas: idêntica). A primeira versão do meu teste do status da matrícula não era repetível no mesmo banco (academia de seed fixa); foi corrigida (limpeza antes e depois) e rodada três vezes seguidas.

**Atualização de uma base real (migration 132 sobre o esquema de `main`).** Num banco novo apliquei as migrations 001–131 de `main` e inseri 4 credenciais (uma de academia única, uma de academia com ambientes `test` e `prod`, uma do `spuri`) e 5 cobranças; apliquei a 132: aplicou sem erro, sem perder nada. Resultado do backfill de `credencial_id`: cobrança pendente da academia de credencial única → ligada; cobrança pendente do `spuri` → ligada; cobrança **terminal** (`Success`) → `NULL`; cobrança pendente de contexto **ambíguo** (duas credenciais) → `NULL`; cobrança `EXTERNO` → `NULL`. A tabela de arquivadas aceita `prod` e recusa ambientes fora de `test`/`prod`.

**Ciclo desfazer a regra → teste falha → restaurar** (sobre o clone verificado; depois de todos os ciclos os arquivos voltaram idênticos ao pacote):

| # | Regra desfeita | Teste que falhou |
| --- | --- | --- |
| N1 | O snapshot deixa de servir a cobrança emitida com ele | `TestIntegrationSnapshotCredenciaisRemovidasComPendentesResolveRetroativamente` |
| N2 | Remover com pendentes deixa de arquivar | `TestIntegrationRetroativoCobrancaPendenteSegueProvedorOriginalAposTroca`, `TestIntegrationSnapshotCredenciaisRemovidasComPendentesResolveRetroativamente`, `TestIntegrationSnapshotServeCobrancaLegadaSemCredencialEmitente` |
| N3 | O snapshot nunca é descartado | `TestIntegrationRetroativoCobrancaPendenteSegueProvedorOriginalAposTroca`, `TestIntegrationSnapshotCredenciaisRemovidasComPendentesResolveRetroativamente` |
| N4 | O snapshot é descartado mesmo com pendente dependendo dele | `TestIntegrationSnapshotCredenciaisRemovidasComPendentesResolveRetroativamente`, `TestIntegrationSnapshotServeCobrancaLegadaSemCredencialEmitente` |
| N5 | O webhook deixa de autenticar com o segredo do snapshot | `TestIntegrationRetroativoCobrancaPendenteSegueProvedorOriginalAposTroca`, `TestIntegrationSnapshotCredenciaisRemovidasComPendentesResolveRetroativamente` |
| N6 | A cobrança deixa de gravar a credencial emitente | `TestIntegrationSnapshotCredenciaisRemovidasComPendentesResolveRetroativamente` |
| N7 | Resolver a cobrança deixa de disparar o descarte | `TestIntegrationRetroativoCobrancaPendenteSegueProvedorOriginalAposTroca`, `TestIntegrationSnapshotCredenciaisRemovidasComPendentesResolveRetroativamente` |
| N8 | A projeção deixa de criar o snapshot na remoção arquivada | `TestIntegrationProjecaoReplaySnapshotDeCredenciais`, `TestIntegrationSnapshotCredenciaisRemovidasComPendentesResolveRetroativamente` |
| N9 | O filtro por provedor deixa de remover métodos não suportados | `TestIntegrationMetodosDoPagadorSoOsDoProvedorAtivo` |
| N10 | A remoção deixa de devolver o aviso (volta a `204` sem corpo) | `TestIntegrationHandlersRemoverCredencialComCobrancaPendenteArquivaEAvisa`, `TestIntegrationHandlersRemocaoFinanceiraRespeitamEscopoDaAcademia` |
| N11 | O status público da matrícula deixa de filtrar pelo provedor | `TestIntegrationHandlersStatusMatriculaFiltraMetodosPeloProvedorAtivo` |
| N12 | A propina do estudante deixa de filtrar pelo provedor | `TestIntegrationMetodosDoPagadorSoOsDoProvedorAtivo` |
| N13 | Um alias `/financeiro/appypay/*` volta a ser registrado | `TestRotasProvedoresPagamentoNeutrasSemAliasesDeprecadosEWebhooks`, `TestLegacyPaymentRoutesAreRemoved` |
| R1 | (Tarefa 119) Consulta segue o provedor ativo em vez do da cobrança | `TestIntegrationRetroativoCobrancaPendenteSegueProvedorOriginalAposTroca` |
| R3 | (119) O núcleo deixa de recusar método não suportado | `TestIntegrationCobrancaNovaUsaProvedorAtivoEMetodoSuportado` |
| R4 | (119) Webhook deixa de filtrar credenciais por provedor | `TestIntegrationWebhookNaoAutenticaEntreProvedores` |
| R5 | (119) Definir provedor deixa de exigir credenciais ativas | `TestIntegrationDefinirProvedorExigeCredenciaisEUnicoAtivo`, `TestIntegrationSnapshotCredenciaisRemovidasComPendentesResolveRetroativamente` |
| R6 | (119) Projeção deixa de aceitar nomes de evento legados | `TestIntegrationProjecaoAceitaEventosLegadosAppyPay` |
| R7 | (119) A cobrança deixa de gravar o provedor emitente | `TestIntegrationCobrancaNovaUsaProvedorAtivoEMetodoSuportado` |
| R9 | (119) As primeiras credenciais deixam de tornar o provedor ativo | `TestIntegrationDefinirProvedorExigeCredenciaisEUnicoAtivo`, `TestIntegrationProjecaoAceitaEventosLegadosAppyPay` |

As regras R2, R8, R10–R12 da Tarefa 119 deixaram de existir (remoção bloqueada, aliases) ou ficaram redundantes e não foram repetidas; o que cobria "remoção com pendente dá `409`" foi reescrito nos testes acima.

## Falha antiga que não é desta tarefa

Em `main`, a **segunda execução de `go test ./...` no mesmo banco** faz falharem 25 testes de `cmd/server` (família `TestTurmaVinculo…` e testes de faltas): o `Rebuild` de turmas reprocessa eventos de uma execução anterior e viola `projection_turmas_codigo_academia_fkey`. Medido com e sem esta tarefa, no mesmo banco reutilizado: as duas listas são idênticas. Sem relação com finanças.

## Limitações conhecidas

- **Editar uma credencial** (mesmo `id`) substitui os segredos usados pelas cobranças pendentes ligadas a ela; **não** gera snapshot. Para trocar de conta no mesmo provedor sem perder as pendentes, remover e cadastrar de novo. O frontend (Tarefa 30) avisa disso quando há pendentes.
- Uma cobrança que **nunca se resolve** (ex.: referência que o provedor não expira e ninguém cancela) mantém o snapshot — e os segredos no cofre — indefinidamente. A academia pode cancelá-la ou registrar o pagamento externo para liberá-lo. Resolver isso de forma automática é o caso de uso do job de reconciliação (fora de escopo, ver abaixo).
- O backfill da migration só liga a `credencial_id` as cobranças pendentes de contextos com **uma** credencial; nas demais, e num `Rebuild` do ledger antigo, a resolução usa o contexto + provedor + ambiente (comportamento anterior), que continua correto.
- Os **serviços extras** continuam a mostrar/aceitar todos os métodos na configuração e no pagamento; a recusa do que o provedor não suporta acontece só na geração da cobrança (`400`). Não foi pedido nesta tarefa.

## Fora de escopo

- **Job de reconciliação** (consulta periódica de cobranças pendentes): não implementado, aguarda a sua decisão depois da explicação.
- **Intelize e PayPay**: continuam só como estrutura (Tarefa 119).
- Filtrar os métodos dos serviços extras pelo provedor ativo.
- Cópia da `Documentação da API.md` dentro do `rastreio-frontend` (`src/Documentação da API.md`, versão 2.3.0, desatualizada): não tocada.

## Como aplicar

1. **Confirme que a base não mudou:** `git fetch && git diff --stat 758e714 origin/main -- cmd internal migrations "Documentação da API.md"`. Vazio = pode substituir; se aparecer algum arquivo, ele mudou depois da base — não substitua e avise. A próxima migration livre deve ser a 132 (`ls migrations | tail -1` mostra a 131).
2. **Copie** a pasta `rastreio-backend/` para a raiz do repositório, mantendo os caminhos. **Não há arquivo a apagar.**
3. **Verifique** (abaixo).
4. **Faça o deploy do backend antes do frontend da Tarefa 30.**
5. **Marque como feito** (abaixo).

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

`gofmt -l .` vazio; `go build` e `go vet` limpos. Os testes `TestIntegration…` só correm de verdade com PostgreSQL de teste descartável: `RUN_POSTGRES_INTEGRATION=1 SPURI_RUN_DB_INTEGRITY_TESTS=1 DATABASE_URL=postgres://… go test ./...` (num banco **novo**; numa segunda execução no mesmo banco, `cmd/server` falha como descrito acima, com ou sem esta tarefa).

## Marcar como feito

1. `**Estado:** feito`, `(feito)` no título.
2. Secção **Resultado** no fim: um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mover para `docs/Tarefas feitas/`, mesmo nome.

## Checklist

- [ ] Base conferida (`758e714`) e migration 132 livre
- [ ] Confirmado que nenhum cliente além do painel usa `/financeiro/appypay/*`
- [ ] Pasta copiada por inteiro (20 alterados, 2 novos); nada a apagar
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test ./...` — todos `ok` (banco novo)
- [ ] Backend em produção antes do frontend da Tarefa 30
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`
