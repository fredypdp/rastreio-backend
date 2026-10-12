# Tarefa 119 — Módulo de pagamentos dividido por provedores (PSP) (backend)

**Estado:** pendente
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `0e909c8` (commit completo `0e909c85d6b89e1e93108776935c9378d7246214`, "Add files via upload" — só acrescentou `docs/Integrações/PayPay API.md`; o código é o do `3e26af8`)
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório
**Ordem de deploy:** deve ir para produção **antes** da Tarefa 29 do `rastreio-frontend`. O frontend novo chama rotas e campos que só existem depois desta tarefa. O frontend **atual** não é afetado: as rotas `/financeiro/appypay/*` continuam registradas como alias deprecado e as credenciais mantêm o formato antigo nelas. **Depois da migration 131 não há compatibilidade com o binário anterior** (as tabelas de credenciais e segredos foram renomeadas): não faça rollback só do código sem reverter a migration.

---

## Contexto

Todo o módulo financeiro estava acoplado à AppyPay: tabelas `financeiro_credenciais_appypay` e `financeiro_segredos_appypay`, eventos do ledger com prefixo `AppyPay`, rotas `/financeiro/appypay/*`, `appypay.go` com 2154 linhas misturando regras genéricas (idempotência, estados, ledger) com chamadas HTTP da AppyPay, e validações "sem credenciais AppyPay" em mensalidade, matrícula e serviços extras.

Pedido: dividir o módulo por provedores, como o módulo de SMS. O Spuri e cada academia definem **um único provedor** (sem padrão nem secundário) e as credenciais dele; cada provedor tem arquivo/handler exclusivo; a criação de cobrança e o pagamento descobrem o provedor e seguem a rota dele; o estudante paga pelos métodos que o provedor suporta e que a academia habilitou. Mecanismo retroativo: se o provedor for trocado enquanto houver pagamentos pendentes, o sistema continua falando com o provedor original até a resposta.

Provedores planejados (esclarecido na conversa): **AppyPay, Intelize e PayPay**; Ulemo é provedor de faturas e recibos, não PSP. **Só a AppyPay entra com código**; a estrutura fica pronta para os outros.

## Decisões de design

- **Um arquivo por provedor, no mesmo pacote `finance`.** O contrato é a interface `Provedor` (`provedor.go`); a AppyPay é o primeiro handler (`provedor_appypay.go`). Mesmo pacote para não criar ciclo de import com os tipos neutros. Um provedor novo = um arquivo `provedor_<nome>.go` + uma linha em `fabricasProvedores`; tabelas, eventos e rotas de credenciais não mudam. A interface já cobre o que Intelize e PayPay exigem (conferido nos documentos em `docs/Integrações/`): campos de credencial arbitrários (PayPay usa chaves RSA), webhooks **sem** segredo gerado pelo Spuri (PayPay autentica por assinatura), resposta de webhook própria (PayPay espera o texto `success`) e provedor sem webhook (Intelize não documenta webhook — a consulta cobre).
- **Provedor ativo por contexto** na tabela `financeiro_provedores_pagamento` (um por contexto: `spuri` ou academia), registrado no ledger pelo evento `ProvedorPagamentoDefinido`. Configurar credenciais **não** troca o ativo, exceto quando o contexto ainda não tem nenhum (as primeiras credenciais viram o provedor ativo — regra aplicada **na projeção**, para o replay do ledger antigo dar o mesmo estado). Definir o provedor exige credenciais já configuradas.
- **Mecanismo retroativo.** Cada cobrança grava o provedor que a emitiu (coluna `financeiro_cobrancas.provedor` + campo `provedor` no payload do evento; o provedor nunca muda depois). Consulta, conferência ao vivo antes de confirmar, cancelamento, webhook e pagamento externo usam o provedor **da cobrança** e as credenciais dele (`provedorDaCobranca`), não o ativo. Trocar de provedor não toca nas credenciais do anterior, e a resposta diz quantas cobranças pendentes ficam nele. **Remover credenciais de um provedor com cobranças pendentes é recusado (`409`)**; resolvidas as pendentes, a remoção passa. Não foi criado job de reconciliação: webhook e consulta sob demanda já cobrem (ver "Fora de escopo").
- **Credenciais e cofre generalizados.** `financeiro_credenciais_appypay` → `financeiro_credenciais_provedor` (coluna `provedor`; único por contexto + ambiente + provedor) e `financeiro_segredos_appypay` → `financeiro_segredos_provedor`. As chaves dos campos da AppyPay (`client_id`, `client_secret`, `gpo_method`, `ref_method`) coincidem com os tipos de segredo **já cifrados**, então nenhum ciphertext foi migrado.
- **Eventos do ledger com nomes neutros** (`CobrancaCriada`, `CredenciaisProvedorConfiguradas`, `WebhookProvedorRecebido` etc.). O ledger é imutável: os nomes `…AppyPay…` antigos continuam sendo aceitos (allowlist de `safe_queries.go`) e a projeção os traduz por `EventoFinanceiroCanonico`; evento antigo sem o campo `provedor` vale `APPYPAY`.
- **Webhooks.** As URLs da AppyPay (`/webhooks/appypay/gpo` e `/ref`) não mudam — já estão configuradas no painel deles. As rotas passam a ser registradas a partir de `RotasWebhook()` de cada provedor (`/webhooks/<slug>/<caminho>`). A autenticação só considera credenciais **do provedor da rota** (um segredo nunca autentica notificação de outro) e a dedupe passa a ser por `(provedor, event_id)`.
- **Métodos de pagamento canônicos** (`REF`, `GPO`, `GPO_QR`): cada provedor declara os que suporta (`Metodos()`) e traduz para os códigos dele. A configuração de propina e de matrícula valida contra o provedor ativo; a geração de cobrança/QR recusa (`400`, `ErrMetodoIndisponivel`) método que o provedor ativo não suporta.
- **Pagamento registrado fora da plataforma** passa a ter o pseudo-provedor `EXTERNO` (sem credenciais, nunca consultado), para não aparecer como cobrança da AppyPay.
- **Compatibilidade para o deploy em duas etapas.** As rotas `/financeiro/appypay/*` ficam como alias deprecado; as de cobrança usam os mesmos handlers das neutras; as de credenciais usam um adaptador que aceita e devolve o formato antigo (`client_id`, `gpo_payment_method_mask` …). Serão removidas numa tarefa futura.
- **Rótulos de auditoria** `appypay:matricula` e `appypay:servico_extra` viram `pagamento:matricula` e `pagamento:servico_extra` (a confirmação não depende do provedor). Registros antigos mantêm o rótulo antigo.

## O que foi atualizado e onde

**24 arquivos alterados + 10 novos + 1 apagado.**

**Arquivo a apagar (ação manual):** `internal/finance/appypay.go` — o conteúdo foi dividido em `servico.go` (parte neutra), `credenciais.go` (credenciais e provedor ativo) e `provedor_appypay.go` (parte específica da AppyPay). Se não for apagado, o pacote não compila (declarações duplicadas). **Nenhum arquivo foi renomeado ou movido de diretório.** Os arquivos de teste `appypay_*_test.go` mantiveram o nome.

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `migrations/131_financeiro_provedores_pagamento.sql` (**novo**) | — | Renomeia as tabelas de credenciais e segredos e acrescenta `provedor`; `provedor` em `financeiro_cobrancas` e `financeiro_webhooks_recebidos` (DEFAULT `APPYPAY`, de propósito, para linhas antigas e replay); índice único de `provider_charge_id` passa a ser por provedor; PK de webhooks passa a `(provedor, event_id)` e o CHECK de método cai; cria `financeiro_provedores_pagamento` e faz backfill (`APPYPAY` para quem já tinha credenciais). Preserva todos os dados. |
| `internal/finance/provedor.go` (**novo**) | `Provedor` (l. 155); `fabricasProvedores` (l. 199); `ValidarConfiguracaoProvedores` (l. 214); `Provedores` (l. 235); `RotasWebhookDoProvedor` (l. 256); `ProvedorPorID` (l. 265); `suportaMetodo` (l. 274); `provedorDoPayloadCobranca` (l. 290); constantes `ProvedorExterno` (l. 37), `WebhookHeaderName` (l. 56) e erros `ErrCredenciaisEmUso` etc. (l. 70) | Contrato da interface, registro por fábrica (o cliente HTTP é por `Service`, sem estado global), catálogo público, métodos canônicos e erros neutros. |
| `internal/finance/provedor_appypay.go` (**novo**) | `EndpointsAtuais` (l. 27), `ValidateAppyPayResourceConfig` (l. 51), `callJSON` (l. 61), `token` (l. 108), `extractProviderOutcome` (l. 216), `appyPayCodeOutcomes` (l. 307), `appyPayProvider` (l. 324) e métodos da interface (l. 356–520) | Tudo o que é da AppyPay: OAuth e cache de token (agora estado do provedor, não do `Service`), endpoints, tabela de códigos, tradução para os estados da plataforma, corpo das chamadas de cobrança/QR/consulta, regra do código 103 do QR, campos de credencial, máscara, autenticação do webhook por segredo no cabeçalho, resposta HTTP. Código movido, não reescrito. |
| `internal/finance/credenciais.go` (**novo**) | `CredentialInput` (l. 21), `CredentialView` (l. 31), `ConfigureCredential` (l. 98), `legacyMaskKeys` (l. 172), `ListCredentials` (l. 179), `CobrancasPendentes` (l. 239), `RemoveCredential` (l. 262), `loadCredentialAtiva` (l. 311), `loadCredentialDoProvedor` (l. 334), `ProvedorAtivo` (l. 358), `PossuiProvedorConfigurado` (l. 373), `WebhookOwner` (l. 480), `AuthenticateWebhook` (l. 490) | Credenciais neutras por provedor (`credenciais` indexado pelas chaves do catálogo; resposta com `credenciais_mascaradas` e `ativo`); `ListCredentials` lê também o payload antigo (campos planos da AppyPay); `RemoveCredential` recusa com `ErrCredenciaisEmUso` quando há pendentes e limpa o cofre; carga de credenciais separada em "do provedor ativo" (cobranças novas) e "de um provedor" (cobranças existentes); autenticação de webhook restrita ao provedor da rota. |
| `internal/finance/provedor_config.go` (**novo**) | `provedorAggregateID` (l. 18), `ConsultarProvedorContexto` (l. 48), `DefinirProvedor` (l. 95), `validarMetodosDoProvedor` (l. 140) | Provedor ativo do contexto, com credenciais configuradas e cobranças pendentes por provedor; troca idempotente (sem evento repetido) que informa as pendentes do anterior; validação de métodos contra o provedor ativo. |
| `internal/finance/servico.go` (**novo**, substitui `appypay.go`) | `ChargeResult` (l. 118) e `CobrancaResumo` (l. 148) ganham `provedor`; `Service` (l. 228), `NewService` (l. 237), `SetHTTPClient` (l. 250); `CreateCharge` (l. 256); `CreateGPOQRCode` (l. 343); `consultCharge` (l. 895); `liveChargeStatus` (l. 989); `AcceptWebhook` (l. 1005); `chargeRow` (l. 1099); `loadChargeWhere` (l. 1119); `provedorDaCobranca` (l. 1138); `comProvedor` (l. 1151) | Núcleo neutro: criação resolve o provedor ativo, o método e chama o provedor pela interface; o provedor fica no payload e nas respostas; consulta, conferência e webhook usam o provedor da cobrança; `AcceptWebhook` recebe o provedor e deduplica por `(provedor, id)`; eventos com nomes neutros. A regra "REF nunca se confirma na criação" continua no núcleo e vale para qualquer provedor. |
| `internal/finance/mensalidade.go` | linha 494 | Troca "sem credenciais AppyPay" por `validarMetodosDoProvedor` (provedor ativo com credenciais que suporte os métodos habilitados). |
| `internal/finance/matricula.go` | linha 173 | Idem para a taxa de matrícula. |
| `internal/finance/pagamento_externo.go` | `statusAoVivoSeDisponivel` (l. 124); linha 285 | A conferência ao vivo usa o provedor da cobrança (sem provedor/credenciais continua "não verificada", sem erro); o registro fora da plataforma grava `EXTERNO`. |
| `internal/domain/aggregates/financeiro.go` | constantes (l. 26); `eventosFinanceirosLegados` (l. 58); `EventoFinanceiroCanonico` (l. 77) | Constantes neutras dos eventos e tradução dos nomes antigos. |
| `internal/db/safe_queries.go` | linha 134 | Nomes neutros acrescentados ao allowlist de eventos (os antigos ficam). |
| `internal/projections/financeiro_projection.go` | `provedorPadraoLegado`/`provedorDoPayload` (l. 52–56); switch por nome canônico (l. 89); credenciais configuradas (l. 251) e removidas (l. 265); provedor definido (l. 288); cobranças (l. 298); webhook (l. 314); Rebuild (l. 329) | Projeta nos nomes novos de tabela; grava o provedor da cobrança na criação (nunca muda); infere o provedor ativo das primeiras credenciais; remover credenciais do provedor ativo limpa a seleção; `ProvedorPagamentoDefinido` faz upsert; Rebuild limpa também a seleção. |
| `internal/handlers/financeiro_provedores_handlers.go` (**novo**) | `ListarProvedores` (l. 24); `ConsultarProvedorContexto` (l. 30); `DefinirProvedorContexto` (l. 47); `ConfigurarCredencial` (l. 85); `configurarCredencial` (l. 115); `ListarCredenciais` (l. 135); `RemoverCredencial` (l. 161); `removerCredencial` (l. 174); `ConsultarSegredoWebhook` (l. 194); `RotacionarSegredoWebhook` (l. 213); legado `credencialAppyPayLegadaInput` (l. 246) e `ConfigurarCredencialAppyPay` (l. 290) | Handlers de provedor e credenciais, com a mesma autorização de todo o módulo (`authorizeFinanceScope`) e os adaptadores das rotas deprecadas. |
| `internal/handlers/financeiro_handlers.go` | `credentialScopeAuthorized` (l. 83); `financeError` (l. 106); `CriarCobranca` (l. 114), `GerarQRCode` (l. 140), `ConsultarCobranca` (l. 165), `ListarCobrancas` (l. 252), `CancelarCobranca` (l. 416); `ReceberWebhook` (l. 464) | Handlers de credenciais saem (foram para o arquivo novo); handlers de cobrança renomeados sem o sufixo `AppyPay`; `ErrCredenciaisEmUso` vira `409`; `ReceberWebhook(provedor, método)` delega autenticação, leitura do corpo e resposta ao provedor. |
| `internal/handlers/servico_extra_handlers.go` | linha 235 | `PossuiProvedorConfigurado` no lugar de `HasCredential`; mensagem neutra. |
| `internal/handlers/servico_extra_solicitacao_handlers.go` / `solicitacao_matricula_handlers.go` | linhas 246 / 560 | Rótulo de auditoria `pagamento:servico_extra` / `pagamento:matricula`. |
| `cmd/server/main.go` | linha 92; linha 305; linhas 431–460 | Validação de ambiente de **todos** os provedores no arranque (a `APPYPAY_RESOURCE` continua exigida); rotas de webhook registradas a partir de cada provedor; rotas neutras novas e bloco de aliases deprecados (l. 448). |
| `Documentação da API.md` | seção 19 (versão 2.7.0) | Reescreve 19.1–19.3, 19.4–19.6, 19.9–19.13 e 19.23 para as rotas neutras; acrescenta 19.27 (catálogo), 19.28 (provedor ativo) e 19.29 (rotas deprecadas); regras gerais com provedor, mecanismo retroativo e eventos neutros. |
| `internal/finance/provedores_integration_test.go` (**novo**) | 7 testes | Cria um segundo provedor de teste (só `REF`, webhook por token, resposta `success`) e prova: catálogo, definição exige credenciais e é única, cobrança nova usa o provedor ativo e recusa método não suportado, **cobrança antiga segue o provedor original após a troca** (consulta, webhook, remoção bloqueada), webhooks não autenticam entre provedores, validação de métodos na configuração, replay de eventos legados na projeção. |
| `internal/handlers/financeiro_provedores_handlers_integration_test.go` (**novo**) | 3 testes | Catálogo, fluxo da academia, autorização (academia alheia e admin sem `fpp`), formato legado das rotas deprecadas, `409` na remoção com pendente. |
| `cmd/server/financeiro_provedores_rotas_test.go` (**novo**) | 1 teste | As rotas neutras, os aliases e os webhooks da AppyPay estão registrados (sem conflito de rota no gin) e exigem autenticação. |
| Testes existentes: `internal/finance/appypay_test.go`, `appypay_integration_test.go`, `appypay_remocao_integration_test.go`, `matricula_remocao_integration_test.go`, `mensalidade_remocao_integration_test.go`, `pagamento_externo_integration_test.go`; `internal/handlers/financeiro_cobrancas_handlers_test.go`, `financeiro_handlers_integration_test.go`, `financeiro_matricula_consulta_test.go`, `financeiro_pagamento_externo_integration_test.go`, `financeiro_pendencias_handlers_test.go`, `financeiro_remocao_handlers_integration_test.go` | chamadas de `CredentialInput`, `AcceptWebhook`, `AuthenticateWebhook`, `RemoveCredential`, `SetHTTPClient`, handlers renomeados, nomes de evento e de tabela | Adaptação mecânica às assinaturas e nomes novos; nenhuma asserção foi enfraquecida. O teste do método da conta foi reescrito para o provedor (`ResolverMetodo`/`codigoMetodo`). |

### Contrato de API (resumo; detalhes na seção 19 da documentação)

| Rota | Corpo / resposta | Erros |
| --- | --- | --- |
| `GET /financeiro/provedores` | catálogo: `id`, `nome`, `slug`, `metodos`, `campos_credenciais[{chave,rotulo,obrigatorio,secreto,ajuda}]`, `usa_segredo_webhook`, `webhooks` | `401` |
| `GET /financeiro/provedor?contexto_tipo&codigo_academia` | `provedor_ativo` (ou `null`) e, por provedor, `ativo`, `credenciais_configuradas`, `cobrancas_pendentes` | `403` |
| `PUT /financeiro/provedor` `{contexto_tipo, codigo_academia?, provedor}` | estado acima + `provedor_anterior` e `cobrancas_pendentes_no_anterior` | `400` provedor desconhecido ou sem credenciais; `403` (admin FPP fora de `spuri`) |
| `POST` / `PUT :id` `/financeiro/credenciais` `{contexto_tipo, codigo_academia?, provedor, credenciais{…}}` | `id`, `provedor`, `credenciais_mascaradas`, `ativo`, `webhook_header_name?`, `webhook_secret?` (só no `POST`, só na 1ª vez) | `400`, `403`, `404` |
| `GET /financeiro/credenciais?provedor=` | lista com os mesmos campos | `403` |
| `DELETE /financeiro/credenciais` `{contexto_tipo, codigo_academia?, provedor}` | `204` | `404`, `403`, **`409` com cobranças pendentes** |
| `POST /financeiro/cobrancas`, `POST /financeiro/qr-codes`, `GET /financeiro/cobrancas/:id`, `POST …/:id/cancelar`, `POST …/:id/pago-externamente` | iguais às antigas `/appypay/…`, com `provedor` na resposta | `400` método indisponível no provedor ativo |
| `/financeiro/appypay/*` | **deprecadas**; mesmos handlers (cobrança) ou adaptador de formato antigo (credenciais) | — |

## Validação realizada

**Ambiente.** Go 1.24.13 (pacote `golang-1.24-go` do apt) e PostgreSQL 16.15, reais. O proxy de módulos do Go está bloqueado na rede do sandbox, então a compilação usou um `go.mod` auxiliar (`-modfile`, só no sandbox) com os módulos `golang.org/x/*`, `gopkg.in/*` e similares apontados para espelhos no GitHub; o `go.mod` e o `go.sum` do repositório **não foram tocados** e não fazem parte da entrega. Variáveis dos testes: `RUN_POSTGRES_INTEGRATION=1`, `SPURI_RUN_DB_INTEGRITY_TESTS=1`, `ENV=test`, `APPYPAY_RESOURCE`, `JWT_SECRET`, `DB_*`.

**Baseline (`main`, sem a mudança), banco novo, `go test -count=1 -p 1 ./...`:** todos os pacotes `ok` (`cmd/server`, `internal/db`, `internal/domain/aggregates`, `internal/finance`, `internal/handlers`, `internal/middleware`, `internal/projections`, `internal/security`, `internal/services`, `internal/storage`, `internal/utils`), `EXIT=0`. 353 funções de teste em `finance`, `handlers` e `cmd/server`.

**Depois (clone novo de `main` @ `0e909c8`, pacote copiado por cima e `internal/finance/appypay.go` apagado, como você vai aplicar):**

- `gofmt -l .` → vazio; `go build ./...` → ok; `go vet ./...` → ok.
- `go test -count=1 -p 1 ./...` em **banco novo** (migrations de 001 a 131 do zero): todos os pacotes `ok`, `EXIT=0`. 364 funções de teste (353 + 11 novas).
- Os 11 testes novos passam; a tabela abaixo mostra que cada regra crítica tem teste que cai quando ela é desfeita.

**Ciclo desfazer a regra → teste falha → restaurar** (rodado sobre o clone verificado; após todos os ciclos os arquivos voltaram idênticos ao pacote):

| # | Regra desfeita | Teste que falhou |
| --- | --- | --- |
| M1 | Consulta/conferência passam a seguir o provedor **ativo** em vez do da cobrança | `TestIntegrationRetroativoCobrancaPendenteSegueProvedorOriginalAposTroca` |
| M2 | Remover credenciais com cobrança pendente deixa de ser bloqueado | `TestIntegrationRetroativoCobrancaPendenteSegueProvedorOriginalAposTroca` |
| M3 | O núcleo deixa de recusar método que o provedor ativo não suporta | `TestIntegrationCobrancaNovaUsaProvedorAtivoEMetodoSuportado` |
| M4 | A autenticação de webhook deixa de filtrar as credenciais pelo provedor | `TestIntegrationWebhookNaoAutenticaEntreProvedores` |
| M5 | Definir provedor deixa de exigir credenciais configuradas | `TestIntegrationDefinirProvedorExigeCredenciaisEUnicoAtivo` |
| M6 | A projeção deixa de aceitar os nomes de evento legados | `TestIntegrationProjecaoAceitaEventosLegadosAppyPay` |
| M7 | A cobrança deixa de gravar o provedor emitente | `TestIntegrationCobrancaNovaUsaProvedorAtivoEMetodoSuportado` |
| M8 | Remover credenciais do provedor ativo deixa de limpar a seleção | `TestIntegrationProjecaoAceitaEventosLegadosAppyPay` |
| M9 | As primeiras credenciais deixam de tornar o provedor ativo | `TestIntegrationDefinirProvedorExigeCredenciaisEUnicoAtivo` e `TestIntegrationProjecaoAceitaEventosLegadosAppyPay` |
| M10 | A rota deprecada deixa de devolver as máscaras no formato antigo | `TestIntegrationHandlersCredenciaisLegadasAppyPayMantemFormatoAntigo` |
| M11 | O conflito de remoção deixa de virar `409` | `TestIntegrationHandlersRemoverCredencialComCobrancaPendenteDevolve409` |
| M12 | O segredo de webhook deixa de ser gerado só para quem usa segredo | `TestIntegrationWebhookNaoAutenticaEntreProvedores` |

**Atualização de uma base real (migration 131 sobre dados antigos).** Num banco novo apliquei as migrations 001–130 de `main`, inseri dados no formato antigo (3 credenciais — uma do `spuri`, duas da mesma academia em ambientes `test` e `prod` —, 2 segredos cifrados, 2 cobranças, 2 webhooks) e apliquei a 131 do pacote: aplicou sem erro; credenciais, segredos, cobranças e webhooks preservados com `provedor = APPYPAY`; backfill criou **um** provedor ativo por contexto (`spuri` e a academia, mesmo com dois ambientes); as tabelas antigas deixaram de existir; o mesmo `provider_charge_id` passou a ser aceito em provedores diferentes e continua recusado dentro do mesmo provedor.

**Segunda execução no mesmo banco.** `internal/finance`, `internal/handlers`, `internal/db` e demais pacotes passam de novo. **`cmd/server` falha com 25 testes** — exatamente os mesmos 25 que falham em `main` sem a mudança na segunda execução (ver abaixo); a comparação das duas listas deu idêntica.

## Falha antiga que não é desta tarefa

Em `main`, uma **segunda execução de `go test ./...` no mesmo banco** faz falharem 25 testes de `cmd/server` (família `TestTurmaVinculo…` e testes de faltas). Causa observada: o `Rebuild` de turmas reprocessa eventos de uma execução anterior e viola `projection_turmas_codigo_academia_fkey` (`rebuild turmas: erro no evento …`). Medido com e sem esta tarefa, no mesmo banco reutilizado: as duas listas são idênticas. Não tem relação com finanças.

Observei também, uma única vez, um pânico em `mensalidade_integration_test.go:35` (fatiamento `[:10]` de uma string com menos de 10 dígitos extraídos de um UUID aleatório). É instabilidade do próprio teste de `main`, não reapareceu nas execuções seguintes e não foi alterada por esta tarefa.

## Como aplicar

1. **Confirme que a base não mudou:** `git fetch && git diff --stat 0e909c8 origin/main -- cmd internal migrations "Documentação da API.md"`. Vazio = pode substituir; se aparecer algum arquivo, ele mudou depois da base — não substitua e avise. Confirme também que a próxima migration livre continua sendo a 131 (`ls migrations | tail -2` deve mostrar a 130).
2. **Apague** `internal/finance/appypay.go`.
3. **Copie** a pasta `rastreio-backend/` para a raiz do repositório, mantendo os caminhos.
4. **Verifique** (abaixo).
5. **Faça o deploy do backend antes do frontend.** A migration 131 roda no arranque do servidor, como as anteriores; as academias e o Spuri que já tinham credenciais AppyPay passam a ter `APPYPAY` como provedor ativo, sem nenhuma ação manual.
6. **Marque como feito** (abaixo).

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

`gofmt -l .` deve sair vazio; `go build` e `go vet` limpos. Os testes `TestIntegration…` só correm de verdade com PostgreSQL de teste descartável: `RUN_POSTGRES_INTEGRATION=1 SPURI_RUN_DB_INTEGRITY_TESTS=1 DATABASE_URL=postgres://… go test ./...` (num banco **novo**; numa segunda execução no mesmo banco, `cmd/server` falha como descrito acima, com ou sem esta tarefa).

## Fora de escopo

- **Intelize e PayPay não estão implementados**; só a estrutura (interface, registro, catálogo, tabelas e eventos neutros). Cada um é uma tarefa própria: um arquivo `provedor_<nome>.go` + a linha em `fabricasProvedores`.
- **Ulemo** (faturas e recibos) não é PSP e não entra neste módulo.
- **Reconciliação em segundo plano de cobranças pendentes** (job periódico que consulta o provedor): não criado. Hoje a resolução vem por webhook, por consulta sob demanda e pela conferência antes de confirmar. É relevante principalmente para provedores sem webhook (Intelize).
- **Remoção das rotas deprecadas `/financeiro/appypay/*`**: tarefa futura, depois de o frontend da Tarefa 29 estar em produção.
- **`validateCharge`** ainda aceita o identificador bruto do método (`GPO_…`/`REF_…`) porque a API antiga o aceitava; resquício do formato da AppyPay a rever quando houver um segundo provedor.
- **Cópia da `Documentação da API.md` dentro do `rastreio-frontend`** (`src/Documentação da API.md`, versão 2.3.0): está desatualizada em relação à do backend (2.6.0 em `main`) e não foi tocada.
- **Ordem de preferência ou provedor de reserva**: não existem por decisão do pedido (só o provedor definido é usado).
- Frontend (tela de escolha de provedor, credenciais dinâmicas, textos): **Tarefa 29 do `rastreio-frontend`**.

## Marcar como feito

1. `**Estado:** feito`, `(feito)` no título.
2. Secção **Resultado** no fim: um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mover para `docs/Tarefas feitas/`, mesmo nome.

## Checklist

- [ ] Base conferida (`0e909c8`) e migration 131 livre
- [ ] `internal/finance/appypay.go` apagado
- [ ] Arquivos copiados (24 alterados, 10 novos); migrations sem conflito de número
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test ./...` — todos `ok` (banco novo)
- [ ] Backend em produção antes do frontend da Tarefa 29
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`
