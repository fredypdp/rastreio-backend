# (feito) Tarefa 114 — Pagamento marcado como feito fora da plataforma + bloqueio de anular/reativar/cancelar cobrança já paga

**Estado:** feito

**Repositório:** https://github.com/fredypdp/rastreio-backend
**Branch base:** main (validado contra o commit `bf8aa51`)
**Execução:** Não é necessário planejar nada. Todo o código já foi escrito e validado pelo orquestrador (ver "O que já foi validado" abaixo) e está pronto no arquivo `114 - Pagamento Externo e Bloqueio de Anular Reativar Cancelar Cobranca Paga.patch`, nesta mesma pasta. Sua única tarefa é aplicar o patch, rodar as verificações do Passo 2, e mover os arquivos conforme o Passo 3.

**Ordem de deploy:** esta tarefa vai para produção **ANTES** da Tarefa 20 do `rastreio-frontend` (a Tarefa 20 chama as duas rotas novas e lê os campos novos `pagamento_externo`/`referencia_externa`). Sozinha, esta tarefa é segura: não remove nenhum campo JSON, não tem migration, e o frontend atual continua a funcionar (só passa a receber `409` quando tenta anular/reativar/cancelar algo já pago).

---

## 0. Prompt recomendado para executar

> Aplique o patch `114 - Pagamento Externo e Bloqueio de Anular Reativar Cancelar Cobranca Paga.patch` com `git apply`, rode `gofmt -l .`, `go build ./...`, `go vet ./...` e `go test ./...`, confirme o resultado esperado descrito no Passo 2, e depois execute o Passo 3 (marcar a tarefa como feita e movê-la para `docs/Tarefas feitas/`). Não replaneje, não altere o código do patch, não abra PR.

## ⚠️ Limitações do ambiente do Codex (leia antes de verificar)

- Seu ambiente **não tem PostgreSQL, Docker nem `psql`** e bloqueia `apt` (403). **Não tente instalar nada** nem subir um banco.
- Todos os **19 testes novos** desta tarefa são testes de integração (`TestIntegration…`) que exigem `RUN_POSTGRES_INTEGRATION=1` + PostgreSQL. No seu ambiente eles vão aparecer como **SKIP** — isso é o esperado. **Não é falha e não é motivo para parar.** Eles já foram executados de verdade pelo orquestrador contra PostgreSQL 16 real (resultados abaixo).
- Se `go build`/`go vet` não conseguir baixar algum módulo (`golang.org/x/*`), pare e reporte exatamente o erro de rede — não altere `go.mod`/`go.sum`.

---

## Contexto do problema

O pedido tem duas partes:

1. **Poder definir manualmente que um pagamento foi feito fora da plataforma.** Ao marcar a cobrança/pendência como paga, o sistema deve seguir o **mesmo processo** de um pagamento feito e validado dentro do ecossistema (confirmação da mensalidade, efetivação de matrícula, lançamento de serviço extra).
2. **Só pode ser anulada, reativada ou cancelada uma cobrança que nunca foi paga e que não está pendente** (paga e aguardando confirmação).

### O que o código fazia antes (diagnóstico real, lido no código)

- **Pagamento externo não existia.** O evento `MensalidadePaga` está na whitelist e na projeção, mas nunca era emitido por nenhum fluxo.
- **Anular mensalidade** (`AnularObrigacoesMensalidade`, `internal/finance/mensalidade.go`) **gravava a anulação no ledger primeiro** e só depois tentava cancelar as cobranças abertas, **engolindo o erro**. Se o estudante já tinha pago na AppyPay e a confirmação ainda não tinha chegado, a anulação ficava gravada no ledger (append-only, irrecuperável).
- **Anular serviço extra** fazia o mesmo, mas devolvia o erro depois de já ter gravado.
- **Reativar** só conferia se o estado era "anulada". Não olhava para um pagamento que chegou depois do cancelamento local (`CobrancaAppyPayConflitoPosCancelamento`).
- **Cancelar cobrança** (`CancelCharge`) já consultava o provedor ao vivo e recusava cobranças pagas, mas: (a) devolvia um erro genérico (HTTP 400), sem tipo; (b) ao **descobrir** um pagamento, não disparava os efeitos de confirmação (matrícula/serviço extra) que o webhook dispararia.
- **Bug de bónus encontrado pelos testes:** um webhook `Success` tardio, com a consulta ao vivo ainda em `Pending`, **rebaixava** uma cobrança para `aguardando_pagamento` (ver "Defeito real provado" abaixo). Isso destruiria uma cobrança paga externamente.

## Decisões de design já tomadas (não reavalie)

- **Interpretação de "pendente (paga e aguardando confirmação)":** na AppyPay o estado `Pending` significa "aceita, ainda não processada" — **não dá para distinguir** "o pagador ainda não pagou" de "pagou e está processando". Por isso o bloqueio acontece apenas quando há **prova de pagamento**: (i) status local `Success`; (ii) pagamento externo registado; (iii) `provider_status = Success` (pagamento tardio depois do cancelamento local); ou (iv) a **consulta ao vivo** ao provedor devolve `Success` mas o local ainda não confirmou. Cobrança realmente em aberto (`Pending`) continua anulável/cancelável. Decisão acordada com o usuário.
- **Falha de comunicação com o provedor bloqueia** (HTTP 503, nada gravado) — sem consulta não se sabe se o pagador já pagou. Exceção: se o escopo **não tem credencial AppyPay**, não existe provedor com que o pagamento possa conflitar, e a operação segue.
- **Nada novo no banco:** não há migration. O pagamento externo é um **evento novo no ledger** (`CobrancaPagamentoExternoRegistrado`) que reaproveita a tabela de projeção `financeiro_cobrancas`, com `status = "Success"` e `pagamento_externo = true` no payload. Assim todo o pipeline posterior trata a cobrança exatamente como uma paga dentro do ecossistema.
- **Só a academia dona** pode marcar pagamento externo (HTTP 403 para qualquer outro papel). A academia só enxerga/altera as suas próprias cobranças.
- **Idempotência:** repetir "marcar como pago" na mesma cobrança devolve 200 com `ja_registrado: true` e **não** grava outro evento nem duplica confirmações.
- **Anular grava por último:** as cobranças abertas são canceladas **antes** de gravar a anulação; se o cancelamento falhar, nenhuma anulação é gravada.

## Regras de negócio resultantes

**Regra 1 — Marcar como pago fora da plataforma (só academia dona)**

| Situação | Resultado |
|---|---|
| Cobrança real `aguardando_pagamento`, provedor responde `Pending`/`Expired`/etc. | Grava `CobrancaPagamentoExternoRegistrado` (status `Success`, `pagamento_externo=true`), confirma a mensalidade e (camada de handler) efetiva matrícula/serviço extra. **200** |
| Mesma cobrança, repetição | **200**, `ja_registrado:true`, sem novo evento |
| Provedor já diz `Success` (pagou na plataforma, webhook ainda não chegou) | **409**; **reconcilia** (confirma como o webhook faria) e não grava evento de pagamento externo |
| Provedor indisponível | **503**, nada gravado |
| Cobrança já paga / terminal / de outra academia | **409** / **400** / **404** |
| Pendência de mensalidade **sem cobrança** (mês `pendente`) | Cria uma cobrança nova **já paga** (`payment_method = "EXTERNO"`, `merchant_transaction_id = "EXT…"`) para aparecer no histórico, e confirma a mensalidade. **201** |
| Mês já pago / anulado / com cobrança aberta | **409** / **400** ("reative antes") / **400** ("marque essa cobrança como paga") |

**Regra 2 — Anular, reativar e cancelar**

| Operação | Bloqueada com **409** quando… |
|---|---|
| Anular/Reativar **mensalidade** | mês `pago`; OU alguma cobrança do mês tem `Success` local, `provider_status=Success` ou `pagamento_externo`; OU o provedor diz `Success` ao vivo (reconcilia e recusa) |
| Anular/Reativar **serviço extra** (mensalidade/preço único) | as mesmas condições, aplicadas às cobranças do lançamento |
| **Cancelar cobrança** | status local `Success` (inclui pago externamente); OU o provedor diz `Success` ao vivo (reconcilia **e** a camada de handler efetiva matrícula/serviço extra) |
| Qualquer das três com provedor indisponível | **503**, nada gravado |

## Contrato das rotas novas

Ambas dentro do grupo `financeiro` (mesmo prefixo das rotas de cobrança/mensalidade já existentes).

- `POST /financeiro/appypay/cobrancas/:id/pago-externamente` — corpo **opcional**: `{ "referencia_externa"?: string (≤100), "observacao"?: string (≤500) }`. Resposta 200: `{ id, provider_charge_id?, merchant_transaction_id, status: "Success", ja_registrado? }`.
- `POST /financeiro/mensalidades/obrigacoes/pago-externamente` — corpo: `{ "codigo_estudante": string, "meses": [{ "ano_letivo": "2025_2026", "mes": 9 }], "observacao"?, "referencia_externa"? }`. Resposta 201: `{ id, merchant_transaction_id, status: "Success", valor, meses }`.
- Erros: `403` (não é academia dona / estudante de outra academia), `404`, `409` (`ErrPagamentoExistente`, mensagem: "esta cobrança já foi paga ou tem um pagamento aguardando confirmação, por isso não pode ser cancelada, anulada nem reativada"), `503` (provedor indisponível), `400` (validação).
- Campos novos (aditivos, `omitempty`) em cada item de `ListCobrancas`/lista unificada: `pagamento_externo` (bool) e `referencia_externa` (string).

## O que o patch faz

**8 arquivos alterados + 4 arquivos novos** (1325 linhas adicionadas, 18 removidas; 0 migrations):

| Arquivo | Mudança |
|---|---|
| `internal/domain/aggregates/financeiro.go` | Nova constante `CobrancaPagamentoExternoRegistrado` |
| `internal/db/safe_queries.go` | Evento novo na whitelist de `event_type` |
| `internal/projections/financeiro_projection.go` | Evento novo no mesmo `case` de upsert de cobranças dos demais eventos de cobrança (sem isto o Rebuild perde o pagamento externo) |
| `internal/finance/appypay.go` | `CobrancaResumo` ganha `PagamentoExterno`/`ReferenciaExterna` (+ leitura em `scanCobrancaResumo`); `consultCharge` nunca rebaixa cobrança paga externamente; `CancelCharge` devolve `ErrPagamentoExistente` (Success local) e **reconcilia** quando descobre Success ao vivo; `AcceptWebhook` ignora cobrança paga externamente |
| `internal/finance/mensalidade.go` | Guard "nunca paga e sem pagamento pendente" antes de anular/reativar; cancela cobranças abertas **antes** de gravar a anulação; `cancelOpenMensalidadeCharges` deixa de engolir erro; removido o cancelamento best-effort pós-gravação |
| `internal/finance/servico_extra.go` | Mesmo guard em `alterarObrigacaoServicoExtra`; cancelamento antes de gravar; `ConfirmarLancamentoServicoExtraPago` passa a ser **idempotente** |
| `internal/handlers/financeiro_handlers.go` | `financeError` mapeia `ErrPagamentoExistente` → 409; handler de cancelar executa os efeitos de confirmação ao descobrir pagamento |
| `cmd/server/main.go` | Registra as 2 rotas novas |
| **NOVO** `internal/finance/pagamento_externo.go` | Serviço: `RegistrarPagamentoExternoCobranca`, `RegistrarPagamentoExternoMensalidades`, `ErrPagamentoExistente`, guards, reconciliação |
| **NOVO** `internal/handlers/financeiro_pagamento_externo_handlers.go` | Handlers das 2 rotas + `executarEfeitosPagamentoConfirmado` (mesma sequência do webhook/consulta: matrícula, taxa de inscrição, serviço extra) |
| **NOVO** `internal/finance/pagamento_externo_integration_test.go` | 14 testes de integração (PostgreSQL real) |
| **NOVO** `internal/handlers/financeiro_pagamento_externo_integration_test.go` | 5 testes de integração de handler |

## O que já foi validado pelo orquestrador (PostgreSQL 16 real)

Ambiente: Go 1.24, PostgreSQL 16, todas as migrations de `migrations/` (65 arquivos, até a 136) aplicadas do zero, `RUN_POSTGRES_INTEGRATION=1`, `-p 1`, banco recriado antes de cada execução completa.

**1. Baseline (antes do patch)** — `go test -p 1 -count=1 ./internal/finance/ ./internal/handlers/ ./internal/projections/ ./internal/db/`:

```
ok  	spuri/internal/finance	6.370s
ok  	spuri/internal/handlers	3.359s
ok  	spuri/internal/projections	0.014s
ok  	spuri/internal/db	0.084s
```

**2. Com o patch, num clone novo e limpo de `main` (commit `bf8aa51`)**, aplicando o `.patch` exatamente como você vai aplicar — `gofmt -l .` e `go vet ./...` sem nenhuma saída; `go test -p 1 -count=1 ./...`:

```
ok  	spuri/cmd/server	0.026s
ok  	spuri/internal/db	0.059s
ok  	spuri/internal/domain/aggregates	0.016s
ok  	spuri/internal/finance	5.239s
ok  	spuri/internal/handlers	3.463s
ok  	spuri/internal/middleware	0.008s
ok  	spuri/internal/projections	0.010s
ok  	spuri/internal/security	0.002s
ok  	spuri/internal/services	0.015s
ok  	spuri/internal/storage	0.009s
ok  	spuri/internal/utils	0.009s
```

**3. Os 19 testes novos** (todos passam com PostgreSQL real): `TestIntegrationPagamentoExterno{MensalidadeSemCobranca, MensalidadeRecusaMesComCobrancaAberta, CobrancaAbertaSegueMesmoPipeline, CobrancaJaPagaNoProvedorBloqueiaEReconcilia, CobrancaBloqueiaQuandoProvedorIndisponivel, CobrancaRecusaOutraAcademia, MatriculaEfetivaVinculoComoWebhook, ComProvedorJaPagoRespondeConflitoECompletaEfeitos, SoAcademiaDona, MensalidadesExigeVinculoDoEstudante}`, `TestIntegrationAnular{BloqueadoQuandoPagamentoAguardaConfirmacao, BloqueadoQuandoProvedorIndisponivel, CancelaCobrancaAbertaENaoPagaEReativaDepois}`, `TestIntegrationReativarBloqueadoAposPagamentoTardioDoProvedor`, `TestIntegrationCancelarCobranca{DescobreSuccessEReconcilia, ComPagamentoNoProvedorCompletaEfeitos}`, `TestIntegrationServicoExtra{AnularBloqueadoQuandoPagamentoAguardaConfirmacao, AnularCancelaCobrancaAbertaNaoPaga}`, `TestIntegrationWebhookNaoRebaixaCobrancaPagaExternamente`.

Eles cobrem, entre outros: o mês fica `pago` e com exatamente **1** evento `paga`; o estado **sobrevive a `Rebuild()` do ledger**; repetir não duplica `CobrancaPagamentoExternoRegistrado` nem `MensalidadesCobrancaConfirmada`; a consulta ao provedor (ainda `Pending`) **não rebaixa** a cobrança externa; a matrícula é efetivada **uma única vez** (1 estudante) pelo mesmo caminho do webhook.

**4. Ciclos revert → falha → reaplicar** (cada correção crítica foi desfeita, o(s) teste(s) abaixo passaram a **falhar**, e o código foi restaurado — build e testes verdes depois):

| # | Correção desfeita | Teste(s) que falharam |
|---|---|---|
| R1 | `consultCharge` deixa de proteger pagamento externo | `PagamentoExternoCobrancaAbertaSegueMesmoPipeline` |
| R2 | sem guard de anular/reativar mensalidade | `ReativarBloqueadoAposPagamentoTardioDoProvedor` (*) |
| R3 | sem guard de anular/reativar serviço extra | `ServicoExtraAnularBloqueadoQuandoPagamentoAguardaConfirmacao` |
| R4 | `ConfirmarLancamentoServicoExtraPago` não idempotente | `ServicoExtraAnularBloqueadoQuandoPagamentoAguardaConfirmacao` |
| R5 | `CancelCharge` não reconcilia / sem `ErrPagamentoExistente` | `CancelarCobrancaDescobreSuccessEReconcilia` |
| R6 | projeção sem o evento novo | `PagamentoExternoMensalidadeSemCobranca`, `PagamentoExternoCobrancaAbertaSegueMesmoPipeline` |
| R7 | whitelist sem o evento novo | os mesmos dois da R6 |
| R8 | provedor indisponível deixa de bloquear | `PagamentoExternoCobrancaBloqueiaQuandoProvedorIndisponivel` |
| R9 | comportamento **antigo** (sem guard + cancelamento engolindo erro) | `AnularBloqueadoQuandoPagamentoAguardaConfirmacao`, `AnularBloqueadoQuandoProvedorIndisponivel`, `ReativarBloqueadoAposPagamentoTardioDoProvedor` |
| R10 | `AcceptWebhook` sem o guard de pagamento externo | `WebhookNaoRebaixaCobrancaPagaExternamente` |
| H1 | handler de cancelar não executa efeitos | `CancelarCobrancaComPagamentoNoProvedorCompletaEfeitos` |
| H2 | handler de pagamento externo não executa efeitos | `PagamentoExternoMatriculaEfetivaVinculoComoWebhook` |
| H3 | `ErrPagamentoExistente` sem mapeamento 409 | `PagamentoExternoMatriculaEfetivaVinculoComoWebhook`, `PagamentoExternoComProvedorJaPagoRespondeConflitoECompletaEfeitos`, `CancelarCobrancaComPagamentoNoProvedorCompletaEfeitos` |
| H4 | remove o 403 "só academia dona" | `PagamentoExternoSoAcademiaDona` |

(*) Na R2, só o teste de reativar falha porque o anular tem uma **segunda linha de defesa**: o cancelamento agora é estrito e devolve `ErrPagamentoExistente` pelo `CancelCharge`. Na R9 (as duas camadas removidas juntas) os testes de anular também falham.

**5. Defeito real provado:** o teste `WebhookNaoRebaixaCobrancaPagaExternamente` foi escrito **antes** da correção e falhou com `status=aguardando_pagamento externo=true` (o webhook rebaixava a cobrança paga externamente). Depois do guard em `AcceptWebhook`, passa.

### Fragilidades pré-existentes (não são desta tarefa)

- Os testes de integração leem `JWT_SECRET` e precisam de **banco limpo** com `-p 1`. Sem `JWT_SECRET`, vários testes antigos falham por variável de ambiente ausente.
- `TestIntegrationFinanceRejectsNonFPPAdmins` e `TestIntegrationFinanceFPPAdminCannotCancelAcademyCharge` (anteriores a esta tarefa) inserem um admin `fpp` e **falham em qualquer banco reutilizado** (`pq: duplicate key value violates unique constraint "idx_bootstrap_fpp_unique"`). Num banco limpo passam. Não os atribua a este patch.
- Rodar pacotes em paralelo (sem `-p 1`) sobre o mesmo banco contamina os testes de integração.

## Passo 1 — Aplicar o patch

Na raiz do repositório:

```bash
git apply "docs/Lista de Tarefas/114 - Pagamento Externo e Bloqueio de Anular Reativar Cancelar Cobranca Paga.patch"
```

Deve **alterar 8 arquivos e criar 4 novos**. Se `git apply` falhar, PARE — não recrie as mudanças manualmente, reporte o conflito.

## Passo 2 — Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

- Os três primeiros devem terminar **sem nenhuma saída/erro**.
- `go test ./...` no seu ambiente (sem `RUN_POSTGRES_INTEGRATION`): todos os pacotes `ok`; os 19 testes novos aparecem como **SKIP** (esperado, já validados pelo orquestrador acima). Se algum teste **não-integração** falhar, confirme se é pré-existente antes de reportar — este patch não toca em nenhum teste unitário existente.
- **Não** tente rodar com `RUN_POSTGRES_INTEGRATION=1`.

## O que NÃO fazer (fora de escopo)

- **Não** criar migration nem tabela/coluna nova — o pagamento externo vive no ledger + payload da cobrança.
- **Não** alterar o frontend nesta tarefa: a parte visual é a **Tarefa 20 do `rastreio-frontend`**.
- **Não** criar fluxo de "pago fora da plataforma" para **matrícula ou serviço extra sem cobrança gerada**: não existe pendência sintética para eles na lista unificada, só para mensalidade. Só cobranças reais (qualquer origem) e pendências de mensalidade são cobertas.
- **Não** tocar nas cobranças do contexto Spuri (contexto `spuri`) — o pagamento externo é só do contexto academia.
- **Não** refatorar a duplicação de efeitos entre webhook, consulta e `executarEfeitosPagamentoConfirmado` — é dívida técnica conhecida, fora desta tarefa.
- **Não** mexer em `go.mod`/`go.sum`, nem corrigir as fragilidades pré-existentes listadas acima.
- **Não** abrir PR nem fazer merge — deixar o commit pronto para revisão.

## Limitações conhecidas (documentadas, não são bugs do patch)

1. **A AppyPay não tem endpoint de cancelamento.** Cancelar é sempre local; um pagamento tardio depois do cancelamento só é detectado por consulta (evento `CobrancaAppyPayConflitoPosCancelamento`, comportamento que já existia).
2. **Pagamento real tardio numa cobrança já paga externamente não é detectado como conflito.** O guard em `consultCharge`/`AcceptWebhook` mantém a cobrança como paga externamente de propósito; se o pagador também pagar na AppyPay depois, haverá pagamento em duplicidade que exige reembolso manual. Tratar numa tarefa futura, se for relevante.
3. Com credencial AppyPay configurada e a AppyPay fora do ar, marcar como pago/anular/cancelar devolve **503** (decisão de segurança acima).
4. Os efeitos de pagamento descoberto durante **anular/reativar** (mensalidade e serviço extra) são aplicados em melhor esforço na camada de serviço; os efeitos de **matrícula/taxa de inscrição** vivem na camada de handler e por isso só são executados nos endpoints de **cancelar** e **pago-externamente** (que é onde esse tipo de cobrança aparece).

## Passo 3 — Marcar como feito

Depois que os Passos 1–2 passarem sem problema:

1. Neste documento, troque a linha `**Estado:** pendente` por `**Estado:** feito` e coloque `(feito)` no início do título (`# (feito) Tarefa 114 — …`).
2. Acrescente, ao final, uma secção **Resultado** com um parágrafo curto descrevendo o que foi efetivamente feito e qualquer desvio pontual.
3. Mova este `.md` **e** o `.patch` de `docs/Lista de Tarefas/` para `docs/Tarefas feitas/`, **mantendo o mesmo nome** (a numeração 114 não muda).
4. Não renumere nem altere nenhuma outra tarefa.

## Resumo das mudanças (checklist final)

- [ ] Patch aplicado (`git apply`) sem conflitos (8 alterados + 4 novos)
- [ ] `gofmt -l .` sem nenhuma saída
- [ ] `go build ./...` limpo
- [ ] `go vet ./...` limpo
- [ ] `go test ./...` — todos `ok`, com os 19 testes novos como SKIP no ambiente do Codex (já validados pelo orquestrador com PostgreSQL real)
- [ ] Nenhuma migration criada; `go.mod`/`go.sum` intactos
- [ ] Estado trocado para **feito**, título com `(feito)`, secção **Resultado** adicionada
- [ ] `.md` e `.patch` movidos para `docs/Tarefas feitas/` com o mesmo nome


## Resultado

O patch de pagamento externo e dos bloqueios para cobranças pagas foi aplicado conforme documentado. As verificações `gofmt -l .`, `go build ./...`, `go vet ./...` e `go test ./...` terminaram sem saída ou erros neste ambiente; não houve desvios.
