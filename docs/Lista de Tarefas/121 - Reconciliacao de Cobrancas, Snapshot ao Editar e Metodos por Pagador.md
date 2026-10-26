# Tarefa 121 — Job de reconciliação de cobranças, snapshot ao editar credenciais e métodos por pagador (backend)

**Estado:** pendente
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `07f0617` (commit completo `07f06173660d486cef8964d1517c72c90fef2d45`, "feat(financeiro): snapshot de credenciais removidas e métodos por provedor - tarefa 120"). A Tarefa 120 já está aplicada nessa base. Conferido antes de entregar: `origin/main` continuava em `07f0617`.
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório (15 alterados, 16 novos, incluindo este documento). Nenhum arquivo é apagado, renomeado ou movido de diretório.
**Ordem de deploy:** backend **antes** do frontend da Tarefa 31 do `rastreio-frontend`. O frontend atual (Tarefa 30) continua a funcionar com este backend: os campos novos das respostas são aditivos. A única diferença visível para ele é que um serviço extra com método não suportado pelo provedor passa a ser recusado ao salvar (400), em vez de só na geração da cobrança.
**Antes de aplicar, confirme:** (1) a próxima migration livre é a `133`; (2) quantas instâncias do servidor rodam em produção: com mais de uma, defina `RECONCILIACAO_ATIVA=false` nas que não devem executar o job (ver "Limitações conhecidas").

---

## Contexto

Pedido desta tarefa:

1. **Job de reconciliação** de cobranças pendentes, sem Intelize nem PayPay mas com arquitetura para muitos provedores, com frequência e idade máxima a critério de quem implementa (priorizando a segurança) e com um mecanismo que guarde as pendentes para **não consultar o banco com frequência**, porque o NeonDB cobra por tempo de uso.
2. **Filtro de métodos de pagamento** também nos serviços extras e em qualquer outro módulo que tenha pagamentos.
3. **Snapshot também ao editar** credenciais.
4. Quando a lista de métodos do estudante vem vazia por falta de provedor, **dizer isso** na resposta.
5. (Frontend) explicar as regras de troca/atualização de credenciais. O backend entrega o que a tela precisa: `snapshot_criado` e `cobrancas_pendentes` na resposta da edição.

Auditoria dos módulos com pagamento: propina e matrícula (configuração validada e lista do pagador filtrada desde a Tarefa 120), serviços extras (taxa de inscrição e serviço pago: **sem** filtro nem validação, resolvidos aqui) e cobrança avulsa (já validada na geração da cobrança com `ErrMetodoIndisponivel`). Todos os caminhos que geram cobrança (`CreateCharge`, `IniciarPagamento*`) já recusavam método não suportado e continuam a fazê-lo.

## Decisões de design

### Job de reconciliação

- **Registro em memória, sem polling de tabela.** As pendentes ficam em memória, carregadas por **uma** consulta no arranque e mantidas pelos eventos que `Service.record` já grava (criação entra, estado terminal sai), em linha com o `projections/manager.go`, que já acorda por escrita no ledger. O laço dorme **sem temporizador** quando não há pendentes e, havendo, até a próxima data de verificação (nunca em intervalo fixo).
- **"Continua pendente" não toca o banco.** A verificação usa dados do registro e credenciais decifradas em cache; só uma mudança de estado lê a cobrança e grava o evento. Está provado por teste: com as conexões do banco **fechadas**, uma verificação de cobrança ainda pendente funciona. Não foi medido num Neon real; a garantia é essa prova de teste.
- **Recarga diária (uma consulta por dia).** O registro é recarregado do banco uma vez por dia (`RECONCILIACAO_RESYNC_MIN`, padrão 1440; 0 desliga) como rede de segurança contra desvio do registro (cobranças criadas por outra instância, falha ao observar um evento).
- **Mesmo caminho do webhook e da consulta manual.** `consultCharge` foi dividido em "consultar no provedor" e `aplicarConsulta`; o job consulta com credenciais em cache e chama a segunda, sem consultar duas vezes. Os efeitos de negócio (matrícula, serviço extra) estão em `aplicarEfeitosPagamento`, usado agora pelo webhook, pela consulta manual e pelo job, este último por um contexto Gin sintético, o mesmo padrão do worker de jobs em lote.
- **Segurança.** Um erro do provedor **nunca** muda a cobrança. Recuo exponencial por cobrança (1 min até 1 h); disjuntor por provedor (5 erros seguidos pausam 1 a 15 min); limite de taxa (2/s) e de concorrência (3) por provedor; jitter. Efeitos de negócio que falham depois de gravar o pagamento são **repetidos sem reconsultar** o provedor (são idempotentes).
- **Sem trava no banco.** A aplicação é idempotente, então duas instâncias só duplicam consultas; uma trava exigiria ir ao banco a cada ciclo e acordaria o Neon.
- **Política por método** (padrão; um provedor pode declarar a sua pela interface opcional `ProvedorReconciliavel`). A validade real das referências não consta da documentação da AppyPay; por isso a idade máxima é generosa (priorizar não perder um pagamento tardio) e as consultas se espaçam muito com a idade (custa só uma chamada ao provedor, nada ao banco). A última consulta cai **exatamente** na idade máxima; depois a cobrança sai da reconciliação **sem mudar de estado** e continua consultável manualmente.

| Método | Intervalos por idade da cobrança | Idade máxima |
|---|---|---|
| `GPO` | 2 min até 15 min · 10 min até 2 h · 1 h até 24 h | 24 h |
| `GPO_QR` | 5 min até 1 h · 30 min até 12 h · 2 h até 3 dias · 12 h até 30 dias | 30 dias |
| `REF` e outros | 10 min até 1 h · 30 min até 6 h · 2 h até 2 dias · 6 h até 7 dias · 24 h até 30 dias | 30 dias |

- **Idade da cobrança:** `financeiro_cobrancas.emitida_em` (migration 133). `updated_at` não serve: é reescrito a cada evento e em todo rebuild.
- **Cache de credenciais:** TTL de 6 h, porque o intervalo entre consultas chega a horas (com TTL curto cada verificação releria o cofre). É descartado ao editar ou remover credenciais, ao descartar um snapshot (só a entrada dele) e a cada erro do provedor. Resolver uma cobrança **não** o esvazia.
- **Arranque:** a primeira verificação das pendentes acontece em até ~1 minuto, espalhada, porque webhooks podem ter sido perdidos enquanto o servidor estava fora.
- **Como acrescentar um provedor:** implementar `Provedor` (a reconciliação só exige a consulta da cobrança) e, se precisar de política própria, `PoliticaReconciliacao`. Está provado por teste com um segundo provedor (ver tabela de testes).

### Snapshot ao editar credenciais

Editar substitui os segredos da credencial ativa, que mantém o mesmo `id`. Se existem pendentes que dependem dela **e** algum valor mudou, o estado anterior é arquivado com `motivo = 'editada'` (novo evento; a projeção cria a linha arquivada e reaponta as pendentes, inclusive as legadas sem `credencial_id`, mas não as já resolvidas). A ordem é segura: copia o cofre → grava o evento → só então sobrescreve; se o evento falha, a cópia é desfeita e a edição é abortada. A decisão usa as pendentes que **dependem da credencial ativa**, não o total do provedor: uma segunda edição, com pendentes já num snapshot, não cria um snapshot órfão. O snapshot é descartado, com os segredos do cofre, quando a última pendente é resolvida (mecanismo da Tarefa 120). O segredo de webhook e o provedor ativo não mudam.

### Métodos para quem paga

Uma só regra (`PagamentoDisponivel`): só aparecem os métodos habilitados pela academia **que o provedor ativo suporta**. Aplica-se a **todos** os pontos que mostram métodos a quem paga: propina, status público da matrícula, lista pública de serviços extras, catálogo do estudante e "minhas inscrições" (taxa). Quando a lista fica vazia, um aviso com `codigo` e `mensagem` explica o motivo (`PROVEDOR_NAO_CONFIGURADO`, `METODOS_NAO_SUPORTADOS_PELO_PROVEDOR`, `NENHUM_METODO_HABILITADO`); as mensagens usam o vocabulário das telas do estudante ("instituição", "secretaria"). Serviço gratuito e inscrição sem taxa não são tocados e não geram aviso. Com falha de infraestrutura, ou serviço sem banco, nada é escondido e nenhum aviso falso é dado. As visões da **academia** continuam a mostrar o que foi configurado. Ao criar/atualizar um serviço extra os métodos são validados contra o provedor ativo (400 citando o método e o provedor); no `PUT` só as listas enviadas são validadas, para não bloquear a edição de outros campos de um serviço com método legado.

### Correção incluída: cache de token OAuth da AppyPay

O token era guardado só pelo `id` da credencial; depois de editar o `client_id`/`client_secret`, o `id` ativo continuava a usar o token do cliente **antigo** por até ~1 h. A chave passou a incluir um resumo dos valores. Está coberta por teste: uma cobrança nova depois da edição pede token ao cliente novo.

## O que foi atualizado e onde

**15 arquivos alterados + 16 novos (15 de código/testes e este documento).** Nenhum arquivo é apagado, renomeado ou movido.

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `migrations/133_financeiro_reconciliacao.sql` (**novo**, 31 linhas) | — | `financeiro_cobrancas.emitida_em` (momento de emissão; backfill pelo primeiro evento da cobrança em `spuri_ledger`, com `updated_at` como último recurso) e `financeiro_credenciais_arquivadas.motivo` (`removida` \| `editada`, com `CHECK`; as linhas antigas ficam `removida`). |
| `internal/finance/reconciliacao_politica.go` (**novo**, 226 linhas) | FaixaReconciliacao (l. 29); PoliticaReconciliacao (l. 38); ProvedorReconciliavel (l. 49); IntervaloPara (l. 78); ProximaVerificacao (l. 97); politicaPadrao (l. 126); politicaDaCobranca (l. 153); ConfigReconciliacao (l. 165); ConfigReconciliacaoDoAmbiente (l. 207) | Política de reverificação como funções puras: faixas de idade → intervalo, idade máxima por método, última consulta exatamente na idade máxima, extensão opcional por provedor (`ProvedorReconciliavel`) e configuração por ambiente (padrões e variáveis `RECONCILIACAO_*`). |
| `internal/finance/reconciliacao_registro.go` (**novo**, 179 linhas) | pendenteReconciliacao (l. 21); registroPendentes (l. 41); Adicionar (l. 68); Vencidos (l. 115); Concluir (l. 134); ProximaData (l. 148); RemoverSeLivre (l. 89); RepontarCredencial (l. 167) | Registro em memória das cobranças pendentes: o que o job precisa saber de cada uma, marcação de "em processamento", próxima data (sem pendentes não há temporizador) e reaponte da credencial quando uma edição cria snapshot. |
| `internal/finance/reconciliacao_job.go` (**novo**, 659 linhas) | CobrancaConfirmada (l. 49); EfeitosPagamento (l. 60); limitadorProvedor (l. 74); cacheCredenciais (l. 150); reconciliador (l. 194); SetEfeitosPagamento (l. 258); IniciarReconciliacao (l. 272); observarReconciliacao (l. 295); pendenteDoPayload (l. 338); carregar (l. 363); executar (l. 411); processarVencidos (l. 455); processar (l. 492); verificar (l. 550); aplicarEfeitos (l. 617); credenciais (l. 646) | O job: laço que dorme até a próxima data, limite de taxa, concorrência e disjuntor por provedor, cache de credenciais com TTL, carga única no arranque e recarga periódica, verificação sem tocar o banco quando continua pendente, aplicação da resposta por `aplicarConsulta` e repetição dos efeitos que falham sem reconsultar. |
| `internal/finance/credenciais_snapshot.go` (**novo**, 115 linhas) | valoresAlterados (l. 35); pendentesDaCredencial (l. 50); snapshotAntesDeEditar (l. 67) | Snapshot ao editar credenciais: decide pelas pendentes que dependem da credencial ativa e só se algum valor mudou; copia o cofre, grava o evento e só então o chamador sobrescreve (desfaz a cópia se o evento falha). |
| `internal/finance/pagamento_disponibilidade.go` (**novo**, 155 linhas) | códigos de aviso (l. 23); AvisoMetodos (l. 32); PagamentoDisponivel (tipo) (l. 40); PagamentoDisponivel (consulta) (l. 49); Filtrar (l. 72); Aviso (l. 91); FiltrarMetodosPorProvedor (l. 110); MetodosPagamentoMensalidadeComAviso (l. 117); ValidarMetodosSeHouverProvedor (l. 137) | Regra única dos métodos que o pagador pode usar (o provedor ativo da academia), os três avisos de lista vazia e a validação ao salvar. Tolera serviço nulo ou sem banco (não esconde nada e não avisa). `FiltrarMetodosPorProvedor` veio de `provedor_config.go`. |
| `internal/handlers/efeitos_pagamento.go` (**novo**, 65 linhas) | aplicarEfeitosPagamento (l. 24); contextoDeSegundoPlano (l. 47); NovosEfeitosPagamento (l. 59) | Definição única dos efeitos de negócio de um pagamento confirmado (matrícula, serviço extra), antes duplicada no webhook e na consulta manual; contexto Gin sintético e o adaptador usado pelo job. |
| `internal/handlers/metodos_pagamento_pagador.go` (**novo**, 68 linhas) | servicoExtraParaPagador (l. 23); servicosExtrasParaPagador (l. 31); solicitacaoServicoExtraParaPagador (l. 52) | Serviço extra e inscrição como o pagador os vê: métodos filtrados pelo provedor e avisos (do serviço e da taxa), com uma consulta ao provedor por academia. |
| `internal/finance/servico.go` | Service (l. 228); NewService (l. 240); consultCharge (l. 900); aplicarConsulta (l. 925); record (l. 976); observarReconciliacao (chamada) (l. 990); loadChargeByID (l. 1167) | `Service` ganha o reconciliador; `consultCharge` passa a só consultar e `aplicarConsulta` aplica a resposta (a consulta manual não muda de comportamento); `record` alimenta o registro depois da projeção; `loadChargeByID` novo. |
| `internal/finance/credenciais.go` | CredentialView (l. 31); ConfigureCredential (l. 103); snapshotAntesDeEditar (chamada) (l. 170); RemoveCredential (l. 297); purgarArquivadasSemPendentes (l. 688) | `CredentialView` ganha `snapshot_criado` e `cobrancas_pendentes`; `ConfigureCredential` chama o snapshot antes de sobrescrever os segredos e descarta o cache do job; `RemoveCredential` descarta o cache; o descarte de snapshot remove só a entrada dele do cache (esvaziar o cache a cada pagamento anularia o ganho). |
| `internal/finance/provedor_appypay.go` | chaveTokenAppyPay (l. 112); token (l. 117); appyPayProvider (l. 333) | Corrige o cache do token OAuth: a chave passa a incluir um resumo de `client_id`/`client_secret`, para que editar a credencial (que mantém o `id`) não reaproveite o token do cliente anterior. |
| `internal/finance/provedor_config.go` | `FiltrarMetodosPorProvedor` (antes na l. 167; hoje em `pagamento_disponibilidade.go`) | Função removida daqui (movida e reescrita sobre `PagamentoDisponivel`). |
| `internal/projections/financeiro_projection.go` | insert de cobranças (emitida_em) (l. 363); CredenciaisProvedorSnapshotPorEdicao (l. 308) | Grava `emitida_em` com o `occurred_at` do primeiro evento (nunca sobrescreve); trata `CredenciaisProvedorSnapshotPorEdicao`: cria a linha arquivada (`motivo` `editada`) e reaponta as pendentes, incluindo as legadas sem `credencial_id`, mas não as já resolvidas (vale também no replay). |
| `internal/domain/aggregates/financeiro.go`, `internal/db/safe_queries.go` | constante (l. 33); allowlist de eventos (l. 139) | Novo evento `CredenciaisProvedorSnapshotPorEdicao`. O diff é maior que a mudança por realinhamento do `gofmt`. |
| `internal/handlers/financeiro_handlers.go` | ConsultarCobranca (efeitos) (l. 190); ReceberWebhook (efeitos) (l. 476) | O webhook e a consulta manual passam a usar `aplicarEfeitosPagamento` (sem mudar o comportamento). |
| `internal/handlers/mensalidade_handlers.go` | ConsultarMensalidadesEstudante (l. 261); avisos por academia (l. 322) | Propina do estudante: `metodos_pagamento_avisos_por_academia` (sempre presente). |
| `internal/handlers/solicitacao_matricula_handlers.go` | ConsultarStatusSolicitacaoMatricula (l. 434); metodos_pagamento_aviso (l. 449) | Status público da matrícula: `metodos_pagamento_aviso` quando a lista vem vazia. |
| `internal/handlers/servico_extra_handlers.go` | CriarServicoExtra (l. 220); validação (criar) (l. 262); AtualizarServicoExtra (l. 299); validação (atualizar) (l. 338); ListarServicosExtrasPublico (l. 474); lista pública filtrada (l. 483); ListarServicosExtrasCatalogoEstudante (l. 504); catálogo filtrado (l. 527) | Criar e atualizar validam os métodos contra o provedor ativo (no `PUT`, só as listas enviadas); lista pública e catálogo do estudante passam pelo filtro do pagador; as rotas da academia continuam sem filtro. |
| `internal/handlers/servico_extra_solicitacao_handlers.go` | ListarMinhasInscricoesServicoExtra (l. 437); taxa filtrada (l. 460) | "Minhas inscrições": métodos da taxa de inscrição filtrados e com aviso. |
| `cmd/server/main.go` | initReconciliacao (chamada) (l. 71); initReconciliacao (l. 249) | Liga o job depois de `initJobs`; falhar ao iniciar não derruba a API. |
| `.env.example` | seção de reconciliação (l. 70) | Documenta as variáveis opcionais `RECONCILIACAO_*` e a recomendação de ligar o job em uma só instância. |
| `Documentação da API.md` | versão 2.9.0; seções 9.10, 19.2, 19.17, 19.29 (**nova**), 23.1, 23.5, 23.8 | Snapshot ao editar e campos novos; avisos de lista vazia; filtro e validação dos serviços extras; reconciliação (política, variáveis, funcionamento). |
| `internal/finance/reconciliacao_test.go` (**novo**, 410 linhas) | 19 testes | Política, registro, disjuntor, limitador, cache, recuo e extração do payload (sem banco). |
| `internal/finance/reconciliacao_integration_test.go` (**novo**, 543 linhas) | 10 testes | Job com PostgreSQL real e relógio simulado: confirma pagamento, não grava nem lê o banco enquanto continua pendente (prova com as conexões fechadas), erro do provedor, disjuntor, repetição de efeitos, idade máxima, carga no arranque, resolução por outro caminho, laço em tempo real e `emitida_em` (inclusive após `Rebuild`). |
| `internal/finance/reconciliacao_multiprovedor_integration_test.go` (**novo**, 77 linhas) | 1 testes | Um segundo provedor entra no job só por implementar a interface, com política, limitador e credenciais próprios. |
| `internal/finance/credenciais_snapshot_integration_test.go` (**novo**, 249 linhas) | 7 testes | Snapshot ao editar: segredos antigos usados pelas pendentes e novos pelas cobranças novas, sem pendentes e valores iguais, segunda edição sem snapshot órfão, replay, pagamento já feito não reapontado, job depois da edição e cache descartado. |
| `internal/finance/pagamento_disponibilidade_test.go` (**novo**, 184 linhas) | 7 testes | Regra dos métodos do pagador: motivos dos avisos, ordem, indeterminado, tolerância a nulos, validação ao salvar e propina. |
| `internal/handlers/metodos_pagamento_pagador_integration_test.go` (**novo**, 380 linhas) | 5 testes | Handlers: status da matrícula (4 motivos), catálogo, lista pública, visão da academia intacta, criar/atualizar serviço extra, propina e inscrições. |
| `internal/handlers/efeitos_pagamento_integration_test.go` (**novo**, 82 linhas) | 1 testes | O adaptador de efeitos do job efetiva a matrícula fora de uma requisição, e repetir é idempotente. |

### Contrato de API (tudo aditivo)

- `PUT /financeiro/credenciais/:id` (e `POST` sobre credencial existente): `snapshot_criado` e `cobrancas_pendentes`, omitidos quando valem `false`/0.
- `GET /financeiro/mensalidades/estudante/:codigo`: `metodos_pagamento_avisos_por_academia` (sempre presente; só traz a academia com algo a explicar).
- `GET /solicitacao-matricula/:codigo/status`: `metodos_pagamento_aviso`, só quando a lista vem vazia.
- Serviços extras (lista pública e catálogo do estudante): `metodos_pagamento_aviso` e `metodos_pagamento_taxa_inscricao_aviso` por serviço; `minhas-inscricoes`: `metodos_pagamento_taxa_inscricao_aviso`. Em todos, as listas de métodos já vêm filtradas.
- `POST/PUT` de serviço extra: método não suportado pelo provedor ativo → `400`.
- Variáveis de ambiente opcionais: `RECONCILIACAO_ATIVA` (padrão `true`), `RECONCILIACAO_CONCORRENCIA` (3), `RECONCILIACAO_CHAMADAS_POR_SEGUNDO` (2), `RECONCILIACAO_CACHE_CREDENCIAIS_SEG` (21600), `RECONCILIACAO_RESYNC_MIN` (1440; 0 = só no arranque).

## Validação realizada

**Ambiente:** Go 1.24.13 e PostgreSQL 16.15 (instalados por `apt`). O sandbox não alcança o proxy de módulos do Go: usei um `go.mod` auxiliar (via `-modfile`, com espelhos no GitHub) só lá; o `go.mod` e o `go.sum` do repositório não foram tocados. Testes com `RUN_POSTGRES_INTEGRATION=1`, `JWT_SECRET`, `-p 1`.

| | `main` @ `07f0617` | clone novo de `main` + este pacote |
|---|---|---|
| `gofmt -l cmd internal` | vazio | vazio |
| `go build ./...` e `go vet ./...` | limpos | limpos |
| `go test ./...`, execução 1 (banco novo) | 11 pacotes `ok`, 0 falhas | 11 pacotes `ok`, 0 falhas |
| `go test ./...`, **execução 2 (mesmo banco)** | 11 pacotes `ok`, 0 falhas | 11 pacotes `ok`, 0 falhas |
| funções de teste (`func Test…` em todo o repositório) | 594 | 644 (+50) |

- **Migration 133 sobre dados antigos:** apliquei as migrations 001–132 de `main` com cobranças, eventos no ledger e uma credencial arquivada, e depois a 133: a cobrança com dois eventos recebeu a data do **primeiro**, a sem eventos caiu no `updated_at`, a de evento único recebeu o seu; a credencial arquivada antiga ficou `motivo = 'removida'`; o `CHECK` rejeita outros valores; nada foi apagado (3 cobranças, 4 eventos no ledger e 1 credencial arquivada presentes depois da 133).
- **Regra desfeita, teste cai** (35 mutações, uma de cada vez, arquivo restaurado depois; todas caem):

| # | Resultado | Teste que cai |
|---|---|---|
| 1 | politica GPO 24h->25h | `TestPoliticaIntervaloPorFaixaEIdadeMaxima` |
| 2 | job: pendente passa a gravar (sem checar terminal) | `TestIntegrationReconciliacaoContinuaPendenteNaoGravaENaoLeOBanco` |
| 3 | job: nao guarda credenciais em cache | `TestIntegrationReconciliacaoCacheDeCredenciaisNaoSobreviveAEdicao` |
| 4 | job: falha de efeito nao fica pendente | `TestIntegrationReconciliacaoRepeteEfeitosQueFalhamSemReconsultar` |
| 5 | observar remove em voo (Remover em vez de RemoverSeLivre) | `TestIntegrationReconciliacaoRepeteEfeitosQueFalhamSemReconsultar` |
| 6 | disjuntor nunca abre | `TestIntegrationReconciliacaoErroDoProvedorNaoMudaEstadoEAbreDisjuntor` |
| 7 | erro do provedor descarta a cobranca | `TestIntegrationReconciliacaoErroDoProvedorNaoMudaEstadoEAbreDisjuntor` |
| 8 | carregar ignora a janela da politica | `TestIntegrationReconciliacaoCarregaPendentesNoArranque` |
| 9 | carregar nao exclui terminais | `TestIntegrationReconciliacaoCarregaPendentesNoArranque` |
| 10 | projecao: emitida_em sobrescrita por evento posterior | `TestIntegrationReconciliacaoEmitidaEmGravadaPelaProjecaoESobreviveAoRebuild` |
| 11 | projecao: snapshot por edicao nao reaponta pendentes | `TestIntegrationEditarCredenciaisComPendentesCriaSnapshotQueConcluiAsPendentes` |
| 12 | projecao: snapshot por edicao grava motivo removida | `TestIntegrationEditarCredenciaisComPendentesCriaSnapshotQueConcluiAsPendentes` |
| 13 | snapshot: nunca detecta alteracao | `TestIntegrationEditarCredenciaisComPendentesCriaSnapshotQueConcluiAsPendentes` |
| 14 | snapshot: ignora pendentes que dependem da ativa (orfao) | `TestIntegrationSegundaEdicaoNaoCriaSnapshotOrfao` |
| 15 | snapshot: nao reaponta o registro do job | `TestIntegrationReconciliacaoDepoisDeEdicaoUsaSegredosAntigos` |
| 16 | snapshot: nao invalida cache ao editar | `TestIntegrationReconciliacaoCacheDeCredenciaisNaoSobreviveAEdicao` |
| 17 | token cache so por ID da credencial | `TestIntegrationEditarCredenciaisComPendentesCriaSnapshotQueConcluiAsPendentes` |
| 18 | filtro: Filtrar nao filtra | `TestPagamentoDisponivelComProvedorFiltraNaOrdemRecebida` |
| 19 | aviso: codigo trocado (sem provedor) | `TestPagamentoDisponivelSemProvedorNaoTemMetodos` |
| 20 | validar ao salvar nunca recusa | `TestIntegrationPagamentoDisponivelExplicaEValidaAoSalvar` |
| 21 | handler: serviço criado sem validar metodos | `TestIntegrationHandlersCriarEAtualizarServicoExtraValidamMetodosNoProvedor` |
| 22 | handler: catalogo sem filtro | `TestIntegrationHandlersServicosExtrasFiltramMetodosParaOPagadorEMantemAVisaoDaAcademia` |
| 23 | handler: lista publica sem filtro | `TestIntegrationHandlersServicosExtrasFiltramMetodosParaOPagadorEMantemAVisaoDaAcademia` |
| 24 | handler: matricula sem aviso | `TestIntegrationHandlersStatusMatriculaExplicaPorQueNaoHaMetodos` |
| 25 | handler: propina sem avisos | `TestIntegrationHandlersMensalidadesDoEstudanteTrazOCampoDeAvisos` |
| 26 | handler: efeitos pagamento pula matricula | `TestIntegrationEfeitosPagamentoDoJobEfetivamMatriculaForaDeUmaRequisicao` |
| 27 | handler: serviço atualizado sem validar metodos | `TestIntegrationHandlersCriarEAtualizarServicoExtraValidamMetodosNoProvedor` |
| 28 | evento de snapshot fora da lista permitida | `TestIntegrationEditarCredenciaisComPendentesCriaSnapshotQueConcluiAsPendentes` |
| 29 | nulos: PagamentoDisponivel sem tolerancia | `TestPagamentoDisponivelToleraServicoOuBancoNulos` |
| 30 | nulos: ValidarMetodosSeHouverProvedor sem tolerancia | `TestPagamentoDisponivelToleraServicoOuBancoNulos` |
| 31 | projecao: snapshot reaponta tambem cobrancas ja pagas | `TestIntegrationSnapshotPorEdicaoSobreviveAoRebuildDaProjecao` |
| 32 | adaptador de efeitos do job nao faz nada | `TestIntegrationEfeitosPagamentoDoJobEfetivamMatriculaForaDeUmaRequisicao` |
| 33 | politica do provedor ignorada (multi-provedor) | `TestIntegrationReconciliacaoSuportaVariosProvedoresCadaUmComSuaPolitica` |
| 34 | recarga diaria desligada por padrao | `TestConfigReconciliacaoDoAmbiente` |
| 35 | REF desiste em 7 dias (menos seguro) | `TestIntegrationReconciliacaoDesisteNaIdadeMaximaSemMudarEstado` |

**Achados da própria validação, corrigidos antes de entregar:** (a) a mutação 16 mostrou que o descarte do cache de credenciais acontecia (por engano) a cada pagamento resolvido, anulando o ganho para o Neon, e que a invalidação ao editar não tinha teste; (b) a mutação 21 mostrou que o teste da validação ao salvar não isolava a regra (o agregado também recusa um método inventado); passou a exigir a mensagem do provedor; (c) na primeira comparação da 2ª execução, um teste meu reaproveitou a academia fixa de seed de um teste da Tarefa 120 e deixava no ledger uma segunda credencial ativa, o que quebrava o `Rebuild` nas execuções seguintes; passou a usar uma academia única por execução; (d) a janela de 7 dias para REF foi ampliada para 30 dias (mais seguro: não perder pagamento tardio).

## Falha antiga que não é desta tarefa

Nenhuma. As suítes de `main` passam inteiras em banco novo e numa segunda execução no mesmo banco.

## Limitações conhecidas

- **Várias instâncias.** O registro é por instância: cobranças criadas noutra instância só entram na recarga diária (ou ao reiniciar). Todas as instâncias ativas consultariam o provedor (idempotente, mas multiplica as chamadas): ligue o job em uma só. O cache de credenciais de outra instância pode ficar até 6 h com valores anteriores à edição, o que é inofensivo para as cobranças já emitidas (elas precisam justamente dos valores antigos).
- **Validade por método** é uma escolha conservadora, não um dado do provedor (a documentação da AppyPay não informa a validade das referências). Ajuste em `politicaPadrao`.
- Cobranças sem `provider_charge_id` (gravadas e interrompidas antes da resposta do provedor) não são reconciliadas: não há o que consultar.
- Os efeitos de **matrícula** pelo job estão provados de ponta a ponta (adaptador + efeito real). Os de **serviço extra** compartilham o código do webhook, mas não há teste dedicado de ponta a ponta nem seed existente para ele. `ConfirmarLancamentoServicoExtraPago` continua a ter o erro ignorado, como no webhook e na consulta manual (paridade).
- A garantia de "não acordar o banco" é por teste (conexões fechadas); não foi medida num Neon real. A recarga diária e qualquer mudança de estado acordam o banco normalmente.

## Fora de escopo

Intelize e PayPay (a arquitetura está pronta e provada com um segundo provedor); filtro de métodos fora dos módulos de pagamento; qualquer mudança de frontend (Tarefa 31 do `rastreio-frontend`); um endpoint administrativo para disparar a reconciliação à mão.

## Como aplicar

1. **Confirme que a base não mudou:** `git fetch && git diff --stat 07f0617 origin/main -- cmd internal migrations "Documentação da API.md" .env.example`. Vazio = pode substituir; se aparecer algum arquivo, ele mudou depois da base e deve ser mesclado, não substituído.
2. **Copie** a pasta `rastreio-backend/` para a raiz do repositório, mantendo os caminhos.
3. **Verifique** (abaixo).
4. **Faça o deploy do backend antes do frontend da Tarefa 31.** A migration 133 corre no arranque.
5. **Marque como feito** (abaixo).

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

`gofmt -l .` vazio; `go build` e `go vet` limpos. Os testes `TestIntegration…` só correm de verdade com PostgreSQL de teste **descartável**: `RUN_POSTGRES_INTEGRATION=1 SPURI_RUN_DB_INTEGRITY_TESTS=1 DATABASE_URL=postgres://… JWT_SECRET=… go test ./... -p 1` (sem `JWT_SECRET` aparecem falhas falsas: "JWT_SECRET é obrigatória"). Esperado: todos os pacotes `ok`, também numa segunda execução no mesmo banco.

Depois do deploy, nos logs do arranque: `[RECONCILIACAO] N cobrança(s) pendente(s) carregada(s)…` e `Reconciliação de cobranças iniciada`.

## Marcar como feito

1. `**Estado:** feito`, `(feito)` no título.
2. Secção **Resultado** no fim: um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mover para `docs/Tarefas feitas/`, mesmo nome.

## Checklist

- [ ] Base conferida (`07f0617`) e migration 133 livre
- [ ] Decidido em que instância o job corre (`RECONCILIACAO_ATIVA`)
- [ ] Pasta copiada por inteiro (15 alterados, 16 novos); nada a apagar
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test ./...` — todos `ok` (também na 2ª execução no mesmo banco)
- [ ] Backend em produção antes do frontend da Tarefa 31
- [ ] Logs do arranque mostram a reconciliação iniciada
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`
