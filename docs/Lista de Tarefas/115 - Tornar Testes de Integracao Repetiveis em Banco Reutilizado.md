# Tarefa 115 — Tornar os testes de integração repetíveis em banco reutilizado

**Estado:** pendente

**Repositório:** https://github.com/fredypdp/rastreio-backend
**Branch base:** main (validado contra o commit `9345dc3`, já com a Tarefa 114 integrada)
**Execução:** Não é necessário planejar nada. Todo o código já foi escrito e validado pelo orquestrador (ver "O que já foi validado" abaixo) e está pronto no arquivo `115 - Tornar Testes de Integracao Repetiveis em Banco Reutilizado.patch`, nesta mesma pasta. Sua única tarefa é aplicar o patch, rodar as verificações do Passo 2, e mover os arquivos conforme o Passo 3.

**Ordem de deploy:** não se aplica. A tarefa altera **somente arquivos de teste** (3 arquivos `_test.go`): nenhum código de produção, nenhuma migration, nenhum JSON de API. Não depende nem bloqueia nenhuma outra tarefa.

---

## 0. Prompt recomendado para executar

> Aplique o patch `115 - Tornar Testes de Integracao Repetiveis em Banco Reutilizado.patch` com `git apply`, rode `gofmt -l .`, `go build ./...`, `go vet ./...` e `go test ./...`, confirme o resultado esperado descrito no Passo 2, e depois execute o Passo 3 (marcar a tarefa como feita e movê-la para `docs/Tarefas feitas/`). Não replaneje, não altere o código do patch, não abra PR.

## ⚠️ Limitações do ambiente do Codex (leia antes de verificar)

- Seu ambiente **não tem PostgreSQL, Docker nem `psql`** e bloqueia `apt` (403). **Não tente instalar nada** nem subir um banco.
- Todos os testes alterados por esta tarefa são testes de integração (`TestIntegration…`) que exigem `RUN_POSTGRES_INTEGRATION=1` + PostgreSQL. No seu ambiente eles vão aparecer como **SKIP** — isso é o esperado. **Não é falha e não é motivo para parar.** Eles já foram executados de verdade pelo orquestrador contra PostgreSQL 16 real (resultados abaixo), inclusive repetidos várias vezes no mesmo banco, que é exatamente o cenário que esta tarefa corrige.
- Se `go build`/`go vet` não conseguir baixar algum módulo (`golang.org/x/*`), pare e reporte exatamente o erro de rede — não altere `go.mod`/`go.sum`.

---

## Contexto do problema

A Tarefa 114 documentou como "fragilidade pré-existente" que dois testes de FPP falhavam em banco reutilizado (`idx_bootstrap_fpp_unique`). Ao investigar para corrigir, o orquestrador mediu a extensão real: **não são 2 testes, são 11**, com 3 causas diferentes, e as falhas **pioram a cada execução** no mesmo banco (porque o ledger é append-only).

Medição na `main` **sem** o patch (`go test -p 1 -count=1 ./...`, PostgreSQL 16, **o mesmo banco** nas três execuções, `JWT_SECRET` definido):

| Execução | Resultado |
|---|---|
| 1 (banco recém-criado) | 11 pacotes ok, **0 falhas** |
| 2 (mesmo banco) | **11 testes falham** |
| 3 (mesmo banco) | **16 testes falham** |

### As 3 causas (todas em código de teste)

**Causa A — admins de teste ocupam para sempre a vaga do índice de bootstrap (6 testes).**
O índice `idx_bootstrap_fpp_unique` (`migrations/114_liberar_dados_unicos_apos_delecao.sql`) é `UNIQUE (role) WHERE created_by IS NULL AND status <> 'deletado'`: **só pode existir 1 admin por papel com `created_by` nulo**. Dois testes inseriam admins direto em `projection_admins` **sem `created_by`** e **nunca os apagavam**:
`TestIntegrationFinanceRejectsNonFPPAdmins` (papéis `gerente` e `adm`) e `TestIntegrationFinanceFPPAdminCannotCancelAcademyCharge` (papel `fpp`). Na 2ª execução o insert colide (`duplicate key … idx_bootstrap_fpp_unique`). Além disso, o helper `getOrCreateTestCreatorAdmin` (`auth_email_handlers_integration_test.go`) cria um admin `gerente` que precisa da mesma vaga: ele falha por causa dos restos dos testes acima e os outros 3 testes de `auth_email` falham em cascata com `projection_admins_created_by_fkey`. Resultado: `SolicitarRecuperacaoSenhaAplicanovaSenhaImediatamente`, `SolicitarRecuperacaoSenhaExigeEmailVerificado`, `SolicitarRecuperacaoSenhaAutoDetectaTipoQuandoOmitido`, `SolicitarVerificacaoEmailGeraTokenPersistido`, `FinanceRejectsNonFPPAdmins`, `FinanceFPPAdminCannotCancelAcademyCharge`.
**Já havia o padrão correto no próprio repositório:** `financeiro_cobrancas_handlers_test.go` insere o admin com `created_by = $1` (autorreferência), o que tira a linha do escopo do índice parcial. Os 2 testes simplesmente não seguiam esse padrão.

**Causa B — 3 testes de ledger contam eventos globalmente por tipo (3 testes).**
`TestIntegrationConfigureMensalidadeGravaNoLedgerEProjectaCorretamente`, `…ConfigureMatriculaGravaNoLedgerEProjectaCorretamente` e `…PagamentoMensalidadeConfirmadoPelaAppyPayMarcaComoPago` faziam `SELECT COUNT(*) FROM spuri_ledger WHERE aggregate_type='Financeiro' AND event_type='…'` **sem filtrar pelo que o próprio teste criou**, e exigiam exatamente 1. Com o ledger já povoado dá 7, 8, 9… (e quebrariam também numa execução limpa se algum teste que gera o mesmo evento rodasse antes deles).

**Causa C — `provider_id` fixo nos mocks da AppyPay (2 testes + envenenamento do ledger).**
Os mocks `handlerAppyPayMockTransport` e `matriculaConsultaMockTransport` devolviam sempre o mesmo `provider_id` (`provider-charge-handler` / `provider-charge-consulta`). `financeiro_cobrancas` tem o índice único `ux_financeiro_cobrancas_provider_id`, então a 2ª execução falha em `TestIntegrationReceberWebhookAppyPayEfetivaVinculoMatricula` e `TestIntegrationConsultarCobrancaAppyPayNaoEfetivaMatriculaAposSuccess`. Pior: o ledger é append-only, então o evento duplicado **já foi gravado** antes de a projeção falhar, e passa a quebrar todo `Rebuild()` futuro. É por isso que a execução 3 tem 5 falhas a mais (`RemoveCredentialRespeitaEventSourcing`, `RebuildFinanceiroReconstroiConfiguracoesEcobrancasMensalidade`, `RemoveMatriculaConfiguracaoFluxoDeComando`, `RemoveMensalidadeConfiguracaoFluxoDeComando` e `PagamentoExternoMensalidadeSemCobranca` — este último é um teste da Tarefa 114 que está correto; ele só sofre o `Rebuild()` envenenado).

## Decisões de design já tomadas (não reavalie)

- **Só testes.** Não alterar código de produção, migrations nem o índice `idx_bootstrap_fpp_unique` — o índice está certo; os testes é que ocupavam a vaga.
- **Admins de teste autorreferenciados** (`created_by = id`), seguindo o padrão já existente no repositório, **mais** `t.Cleanup` que apaga a linha. A autorreferência garante que o teste não colide nem com restos nem com um admin FPP de bootstrap real que exista no banco; o cleanup evita acumular linhas.
- **Contagens de ledger escopadas pelo que o teste criou:** os eventos de configuração por `payload->>'codigo_academia'` (o `aggregate_id` desses eventos é fixo e compartilhado entre academias, então não serve de filtro) e a confirmação de cobrança por `aggregate_id` = id da cobrança.
- **Mocks com `provider_id` único:** a criação devolve `provider-charge-…-<uuid>` e a consulta (GET `/charges/{id}`) devolve o id que veio na própria URL, mantendo criação e consulta coerentes sem estado no transporte.
- **Não** criar infraestrutura nova de isolamento (schema por teste, banco por pacote, helpers globais de limpeza). Fora de escopo.

## O que o patch faz

**3 arquivos alterados, 0 novos** (17 linhas adicionadas, 9 removidas; só `_test.go`):

| Arquivo | Mudança |
|---|---|
| `internal/finance/financeiro_ledger_integrity_test.go` | As 3 contagens de ledger passam a filtrar por `payload->>'codigo_academia'=$1` (configuração de mensalidade e de matrícula) e por `aggregate_id=$1` com `view.Charge.ID` (confirmação de cobrança) |
| `internal/handlers/financeiro_handlers_integration_test.go` | Os inserts de admin dos 2 testes de FPP/gerente/adm passam a gravar `created_by = $1` e ganham `t.Cleanup` com `DELETE FROM projection_admins WHERE id=$1`; `handlerAppyPayMockTransport` gera `provider_id` único na criação e devolve o id da URL na consulta |
| `internal/handlers/financeiro_matricula_consulta_test.go` | `matriculaConsultaMockTransport` idem (`provider_id` único na criação, id da URL na consulta) |

## O que já foi validado pelo orquestrador (PostgreSQL 16 real)

Ambiente: Go 1.24, PostgreSQL 16, todas as migrations de `migrations/` aplicadas do zero, `RUN_POSTGRES_INTEGRATION=1`, `JWT_SECRET` definido, `-p 1`.

**1. Depois do patch, num clone novo e limpo de `main` (commit `9345dc3`)**, aplicando o `.patch` exatamente como você vai aplicar: `gofmt -l .` sem saída; `go build ./...` e `go vet ./...` limpos; e **três execuções seguidas de `go test -p 1 -count=1 ./...` no MESMO banco, sem recriá-lo**:

```
execução 1: 11 pacotes ok, 0 falhas
execução 2: 11 pacotes ok, 0 falhas
execução 3: 11 pacotes ok, 0 falhas
```

(Numa cópia de trabalho foram 4 execuções seguidas no mesmo banco, também todas com 11 pacotes ok e 0 falhas.)

**2. Ciclos revert → falha → reaplicar.** Cada grupo de correção foi desfeito sozinho, num banco novo, e a suíte (`./internal/finance/ ./internal/handlers/`) rodou duas vezes. Em todos os casos a execução 1 passa e a execução 2 **falha exatamente nos testes esperados** — prova de que o teste detecta o problema; depois o código foi restaurado (arquivos idênticos ao patch):

| Correção desfeita | Execução 2 passa a falhar em |
|---|---|
| A — admins autorreferenciados + cleanup | 6 testes: `SolicitarRecuperacaoSenhaAplicanovaSenhaImediatamente`, `…ExigeEmailVerificado`, `…AutoDetectaTipoQuandoOmitido`, `SolicitarVerificacaoEmailGeraTokenPersistido`, `FinanceRejectsNonFPPAdmins`, `FinanceFPPAdminCannotCancelAcademyCharge` |
| B — contagens de ledger escopadas | 3 testes: `ConfigureMensalidadeGravaNoLedger…`, `ConfigureMatriculaGravaNoLedger…`, `PagamentoMensalidadeConfirmadoPelaAppyPayMarcaComoPago` |
| C — `provider_id` único nos mocks | 2 testes: `ReceberWebhookAppyPayEfetivaVinculoMatricula`, `ConsultarCobrancaAppyPayNaoEfetivaMatriculaAposSuccess` |

**3. Verificação extra (informativa):** `go test -count=1 ./...` **sem** `-p 1` (pacotes em paralelo no mesmo banco) também passou 3 de 3 no sandbox do orquestrador. Não é garantia — os pacotes compartilham um único banco, por isso `-p 1` continua recomendado.

## Passo 1 — Aplicar o patch

Na raiz do repositório:

```bash
git apply "docs/Lista de Tarefas/115 - Tornar Testes de Integracao Repetiveis em Banco Reutilizado.patch"
```

Deve **alterar 3 arquivos** (nenhum novo). Se `git apply` falhar, PARE — não recrie as mudanças manualmente, reporte o conflito.

## Passo 2 — Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

- Os três primeiros devem terminar **sem nenhuma saída/erro**.
- `go test ./...` no seu ambiente (sem `RUN_POSTGRES_INTEGRATION`): todos os pacotes `ok`; os testes de integração aparecem como **SKIP** (esperado, já validados pelo orquestrador acima). Se algum teste **não-integração** falhar, confirme se é pré-existente antes de reportar — este patch só altera testes de integração.
- **Não** tente rodar com `RUN_POSTGRES_INTEGRATION=1`.

## O que NÃO fazer (fora de escopo)

- **Não** alterar código de produção (`internal/**` fora dos 3 `_test.go`), migrations, `go.mod`/`go.sum` nem o índice `idx_bootstrap_fpp_unique`.
- **Não** apagar, renomear nem refatorar `getOrCreateTestCreatorAdmin`, `seedAdminParaRecuperacao` ou outros helpers de teste.
- **Não** corrigir outros testes além dos 3 arquivos do patch: o orquestrador mediu a suíte inteira e não há outra falha de repetibilidade.
- **Não** criar infraestrutura de isolamento de banco (schema/banco por teste ou por pacote) nem scripts de limpeza.
- **Não** mexer no frontend (a Tarefa 21 do `rastreio-frontend` é independente).
- **Não** abrir PR nem fazer merge — deixar o commit pronto para revisão.

## Limitações conhecidas (documentadas, não são bugs do patch)

1. **O patch não repara um banco já contaminado.** Linhas de admin sem `created_by` e eventos de cobrança com `provider_id` duplicado que **já foram gravados** por execuções antigas continuam lá (o ledger é append-only). Qualquer banco de teste usado antes deste patch deve ser **recriado uma vez** (`DROP DATABASE` / `CREATE DATABASE` e reaplicar as migrations); a partir daí as execuções repetidas passam.
2. Os testes de integração continuam exigindo `JWT_SECRET` definido; sem ele vários testes falham por variável de ambiente ausente (comportamento antigo, fora de escopo).
3. Os testes continuam exigindo `RUN_POSTGRES_INTEGRATION=1` e PostgreSQL — nada muda para o ambiente do Codex.

## Passo 3 — Marcar como feito

Depois que os Passos 1–2 passarem sem problema:

1. Neste documento, troque a linha `**Estado:** pendente` por `**Estado:** feito` e coloque `(feito)` no início do título (`# (feito) Tarefa 115 — …`).
2. Acrescente, ao final, uma secção **Resultado** com um parágrafo curto descrevendo o que foi efetivamente feito e qualquer desvio pontual.
3. Mova este `.md` **e** o `.patch` de `docs/Lista de Tarefas/` para `docs/Tarefas feitas/`, **mantendo o mesmo nome** (a numeração 115 não muda).
4. Não renumere nem altere nenhuma outra tarefa.

## Resumo das mudanças (checklist final)

- [ ] Patch aplicado (`git apply`) sem conflitos (3 arquivos alterados, 0 novos)
- [ ] `gofmt -l .` sem nenhuma saída
- [ ] `go build ./...` limpo
- [ ] `go vet ./...` limpo
- [ ] `go test ./...` — todos `ok`, com os testes de integração como SKIP no ambiente do Codex (já validados pelo orquestrador com PostgreSQL real, 3 execuções seguidas no mesmo banco)
- [ ] Apenas arquivos `_test.go` alterados; `go.mod`/`go.sum` e migrations intactos
- [ ] Estado trocado para **feito**, título com `(feito)`, secção **Resultado** adicionada
- [ ] `.md` e `.patch` movidos para `docs/Tarefas feitas/` com o mesmo nome
