# Tarefa 137 — (feito) Parte 7: catálogo fixo de serviços (Transporte e Atividade extracurricular) (backend)

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `452b3e3` (Tarefa 136). `main` já avançou para `be988ce` ("Add files via upload"), que mexe **só** em `docs/Planos de Atualização/Plano de Atualizações 2.md`; nenhum arquivo desta entrega foi alterado por ele.
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório, **mais 12 arquivos a apagar** (lista em "Arquivos a apagar")
**Plano:** Parte 7 (todos os itens do back-end; falta só o front-end, Tarefa 43 do `rastreio-frontend`) e as três correções registradas hoje (Partes 8, 20 e 28, só no plano).
**Versão da API:** 4.1.0 → **5.0.0 (MAJOR)**. Migration **146**.
**Ordem de deploy:** **o front-end primeiro.** Deve ir para produção **depois** da Tarefa 43 do `rastreio-frontend`: esta tarefa **remove** rotas (`/academia/servicos-extras` genérico e `/academia/categorias-servico`) e campos que o front-end antigo usa; o front-end novo não chama nenhuma rota removida e funciona com o back-end antigo só até o ponto em que as rotas novas faltam (as telas de Transporte e Atividades dão erro até o back-end subir). Ordem: **1) Tarefa 43 (front), 2) Tarefa 137 (back)**, com uma janela curta em que Serviços ficam indisponíveis para a academia (sem academias em produção, sem impacto).

---

## Contexto

A academia deixa de criar e personalizar serviços (Plano, Parte 7, decisão de 2026-11-21): o catálogo fica no código, com dois serviços, **Transporte** e **Atividade extracurricular**. O pedido desta tarefa: terminar a Parte 7 (na `main` não havia nada dela, só o plano) e acrescentar ao plano três correções (itens 1 a 3, ver o plano).

**Sobre "terminar":** o repositório em `452b3e3` não tem nenhum arquivo da Parte 7; a Parte inteira foi implementada aqui. Se existia um pacote parcial de um chat anterior que **não** chegou ao `main`, ele não foi usado (ver a pergunta no fim).

## Decisões de design

- **Persistência (regra 17):** catálogo e configuração em tabelas CRUD com auditoria na mesma transação (`internal/cadastros`, mesmo padrão das Tarefas 133 a 135); a solicitação, a aprovação e o vínculo continuam em event sourcing. Sem chave estrangeira de tabela CRUD para tabela reconstruível (regra 18): `codigo_academia` e `codigo_estudante` ficam por código; o teste-guarda passou.
- **Um Transporte por academia**, criado na primeira escrita (a leitura devolve um Transporte vazio com `persistido: false`). Atividades: várias.
- **Preço e prazo gerais do Transporte** (`nenhum` / `gratuito` / valor ou meses) bloqueiam o valor próprio da rota **nos dois sentidos**: definir o geral com rota que já tem valor próprio é **409** e não grava nada; definir valor próprio na rota com geral definido é **400**.
- **Só mensalidade:** cobra-se o valor de cada mês nos meses do ano civil marcados (1 a 12), do mês do vínculo em diante, sem meses retroativos. O tipo `unico` / lançamento `preco_unico` deixou de existir.
- **Mês removido do prazo:** cancela as cobranças não pagas desse mês nas inscrições vinculadas daquele serviço (ou rota). Cobrança paga ou com pagamento em andamento no provedor **nunca é tocada**, em duas camadas: a guarda desta tarefa e a que já existia ao cancelar (a mutação que remove só a primeira não faz o teste cair: a segunda protege).
- **Rota** só é oferecida ao estudante com preço, prazo, métodos (se paga) e ao menos um veículo ativo; **atividade coletiva** só com turma ativa. O que falta aparece em `problemas` na visão da academia.
- **Vínculo do transporte:** uma rota e ao menos um veículo **ativo dessa rota**; o índice único (serviço × estudante) da inscrição ativa já impede uma segunda inscrição no Transporte.
- **Turma da atividade coletiva:** escolhida pela **academia ao aprovar** (`turma_id`, obrigatório na coletiva, recusado nas demais); o estudante entra na turma no vínculo (inclusive depois de pagar a taxa) e sai quando a inscrição é cancelada ou ele sai da academia.
- **Apagar** rota, veículo, atividade e turma só se nenhuma inscrição os usou; senão `409` e desativar. Atividade com inscrições não troca entre individual e coletiva.
- **Mantido do modelo antigo:** disponibilidade por ano/curso (Tarefas 87 e 112), validação dos métodos contra o provedor ativo (Tarefa 121) e a lista de métodos filtrada para o pagador com os avisos. **Removido:** `documento_obrigatorio`, `documento_instrucoes`, `detalhes_personalizados`, descrição, categoria. O documento da inscrição continua aceito, mas é sempre opcional.
- **Valores:** `ValidarValorServico` já recusa (sem arredondar) zero, negativo, mais de 2 casas e acima do teto do `NUMERIC(14,2)`; a Parte 8 só consolida em um validador único.
- **Interpretação:** dia da semana `1` = segunda … `7` = domingo; `horario_embarque` e horários em `hh:mm`; meses do ano civil valem para **todos os anos** (não há lista por ano).

## O que foi atualizado e onde

**20 arquivos alterados + 8 novos + 12 a apagar** (já contando o plano, a documentação da API e o Dockerfile; o próprio documento da tarefa vai junto na pasta).

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `migrations/146_servicos_catalogo_fixo.sql` (**novo**) | — | Apaga `projection_servicos_extras` e `projection_categorias_servico`; cria `servico_transporte`, `servico_transporte_rotas`, `servico_transporte_veiculos`, `servico_atividades`, `servico_atividade_horarios`, `servico_atividade_turmas`, `servico_atividade_turma_membros` (CHECKs de preço, prazo, taxa, hora, dia; índices únicos de nome entre ativos); acrescenta `tipo_servico`, `rota_id`, `veiculo_ids`, `turma_id` à projeção da solicitação. |
| `internal/cadastros/servicos.go` (**novo**) | `ValidarValorServico` (182), `ValidarHorarios` (228), `DefinirPrecoGeral` (364), `DefinirPrazoGeral` (390), `aplicarRota` (450), `Transporte.Resolver` (489), `Atividade.Atualizar` (567) | Regras puras: valor, meses, horas, horários sem cruzamento, métodos, disponibilidade (formato), exclusão mútua geral × rota, resolução do preço/prazo efetivos e dos `problemas`. |
| `internal/cadastros/servicos_repo.go` (**novo**) | `resolverServico` (277), `transporteParaEscrita` (370), `AtualizarRota` (473), `ExcluirRota` (528), `RegistrarMembro` (932) | CRUD com auditoria na mesma transação de Transporte, rotas, veículos, atividades, horários, turmas e membros; bloqueio de apagar o que já foi usado; membros idempotentes. |
| `internal/domain/aggregates/solicitacao_servico_extra.go` | `Criar` (186), `Aprovar` (239), eventos e `Apply` | Solicitação guarda `TipoServico`, `RotaID`, `VeiculoIDs`; `Aprovar` recebe `turmaID`. `Criar` exige rota e veículo no transporte e os recusa na atividade. |
| `internal/projections/solicitacao_servico_extra_projection.go` | `SolicitacaoServicoExtraCriada` (34) e as de aprovação/vínculo | Grava `tipo_servico`, `rota_id`, `veiculo_ids`, `turma_id`. |
| `internal/finance/servico_extra.go` | `PendenciasServicoExtra` (166), `CancelarCobrancasServicoMesesRemovidos` (192), validação do tipo no pagamento | Pendência só nos meses cobrados, sem retroativos; novo cancelamento das cobranças não pagas de meses removidos; pagamento só de `mensalidade`. |
| `internal/handlers/apoio/servicosextras.go` | `ElegivelParaServicoExtra` (113), `TransporteJSON` (145), `CatalogoParaPagador` (215), `RegistrarMembroDaAtividade` (396), `EfetivarVinculoServicoExtraPago` (413) | Reescrito para o catálogo fixo: elegibilidade sobre `Disponibilidade`, serialização da academia e do pagador, vínculo com turma. Saem a criação/atualização genérica e a serialização do serviço antigo. |
| `internal/handlers/apoio/helpers.go` | `cancelarInscricoesServicoExtraAoSair` (434) | Tira o estudante da turma da atividade ao cancelar por saída; saem as fábricas das projeções antigas. |
| `internal/handlers/apoio/financeiro.go` | — | Sai o adaptador do serviço antigo para o pagador. |
| `internal/handlers/apoio/cadastros_resposta.go` | `RespondErroCadastro` (21) | `409` para `ErroConflito`. |
| `internal/handlers/servicosextras/catalogo_handlers.go` (**novo**) | `validarProvedorServico` (31), `cancelarMesesRemovidos` (65), `AtualizarTransporte` (112), `CriarAtividade` (530) e demais | Gestão do catálogo pela academia (23.2 e 23.3 da API); regras de provedor da Tarefa 121 mantidas; cancelamento das cobranças dos meses removidos. |
| `internal/handlers/servicosextras/servico_extra_solicitacao_handlers.go` | `SolicitarServicoExtra` (22), `AprovarSolicitacaoServicoExtra` (137), `MinhasPendenciasServicoExtra` (465), `IniciarPagamentoServicoExtraObrigacao` (495), `resolverPedido` (640), `turmaDaAprovacao` (702) | Pedido com rota e veículos; aprovação com turma; pendências e pagamento pelos meses cobrados; **correção** da leitura do `solicitacao_id` no pagamento da mensalidade (o campo é `json:"-"` no input do `finance` e o corpo era ignorado: a rota sempre respondia 400). |
| `cmd/server/main.go` | bloco "Catálogo fixo de serviços" (665) e leitura | Registra as rotas novas; remove as genéricas, as de categoria e o registro das duas projeções. |
| `internal/permissoes/rotas.go`, `permissoes.go` | `servicos-extras/transporte` (175) | Mapa de rotas ↔ `servicos_extras.ver/gerir` das rotas novas; textos das permissões. |
| `internal/db/politica_eventsourcing.go`, `politica_eventsourcing_test.go`, `safe_queries.go` | — | Saem os agregados `ServicoExtra` e `CategoriaServico` e os 10 tipos de evento (e a entrega "Parte 7" da lista de conversão). |
| `internal/domain/aggregates/aggregate.go` | `Load` | Sai o carregamento dos dois agregados removidos. |
| `internal/domain/aggregates/solicitacao_servico_extra_test.go` | — | Assinaturas novas de `Criar` e `Aprovar`. |
| `internal/cadastros/servicos_test.go`, `servicos_integration_test.go` (**novos**) | — | testes de regra e de integração (PostgreSQL): geral × rota, valor, meses, horários, nomes repetidos, apagar o que foi usado, isolamento entre academias, turma e membros, auditoria. |
| `internal/handlers/servicosextras/catalogo_integration_test.go`, `elegibilidade_test.go` (**novos**) | — | 6 testes de ponta a ponta pelos handlers e a elegibilidade (adaptada do teste antigo). |
| `internal/handlers/metodos_pagamento_pagador_integration_test.go` | — | Saem os 2 testes do serviço genérico (reescritos em `catalogo_integration_test.go`); o resto fica. |
| `Documentação da API.md` | 1.2 (tabela de versões), 23.1 a 23.6 | **5.0.0**: seção 23 reescrita. |
| `Dockerfile` | `LABEL version` | 4.1.0 → 5.0.0. |
| `docs/Planos de Atualização/Plano de Atualizações 1.md` | Partes 7, 8, 20, 28, registro | Parte 7 marcada (back-end) com "Como ficou"; **Garantia 5** na Parte 8; **correções urgentes** nas Partes 20 e 28; registro e numeração. |

### Arquivos a apagar (12)

Copiar a pasta por cima **não** apaga: apague à mão (ou `git rm`). Todos são do serviço genérico e das categorias.

```
internal/domain/aggregates/servico_extra.go
internal/domain/aggregates/servico_extra_test.go
internal/domain/aggregates/categoria_servico.go
internal/domain/aggregates/categoria_servico_test.go
internal/projections/servico_extra_projection.go
internal/projections/categoria_servico_projection.go
internal/handlers/servicosextras/servico_extra_handlers.go
internal/handlers/servicosextras/categoria_servico_handlers.go
internal/handlers/servicosextras/servico_extra_elegibilidade_test.go
internal/handlers/servicosextras/servico_extra_catalogo_estudante_integration_test.go
internal/handlers/servicosextras/servico_extra_pendencias_gratuito_integration_test.go
internal/handlers/servicosextras/categoria_servico_get_by_id_integration_test.go
```

Nada foi renomeado nem movido de diretório.

### Contrato da API (resumo; completo na seção 23)

- **Saem:** `POST/PUT/DELETE /academia/servicos-extras[/:id[/desativar|/reativar]]`, `GET /academia/servicos-extras/:id`, todas as de `/academia/categorias-servico`.
- **Entram (academia):** `GET /academia/servicos-extras` (muda de formato: `{transporte, atividades}`), `GET|PUT .../transporte`, `POST|PUT|DELETE .../transporte/rotas[/:id[/desativar|/reativar]]`, `POST .../transporte/rotas/:id/veiculos`, `PUT|DELETE .../transporte/veiculos/:id`, `GET|POST .../atividades`, `GET|PUT|DELETE .../atividades/:id`, `PUT .../atividades/:id/desativar|reativar`, `POST .../atividades/:id/turmas`, `PUT|DELETE .../turmas-atividade/:turma_id`, `GET .../turmas-atividade/:turma_id/membros`.
- **Mudam:** `GET /academia/servico/:codigo/servicos-extras` e `GET /estudante/servicos-extras/catalogo` passam a `{transporte, atividades, total}`; `POST /estudante/servicos-extras/:id/solicitacao` ganha `rota_id` e `veiculo_ids`; `PUT .../solicitacoes/:id/aprovar` ganha `turma_id`; as inscrições ganham `tipo_servico`, `rota_id`, `veiculo_ids`, `turma_id`; `POST /financeiro/servicos-extras/obrigacao/pagamento` só aceita `mensalidade`.
- **Erros:** `400` regra/formato, `404`, `409` conflito (geral × rota, nome repetido, apagar o que foi usado).

## Validação realizada

**Ambiente:** Go 1.27.4 (`go-bin`), PostgreSQL 16.2 local descartável (as extensões `uuid-ossp` e `pgcrypto` foram **substituídas por equivalentes mínimos só neste ambiente de validação**: o PostgreSQL de teste não as traz; o CI real usa as originais), `RUN_POSTGRES_INTEGRATION=1`. As dependências Go vieram do GitHub (o proxy padrão é bloqueado no ambiente): um `replace` temporário no `go.mod` **só no ambiente de validação**, **não entregue**.

- **Baseline** (`main` @ `be988ce`, banco novo): `go test -p 1 ./...` → **29 pacotes `ok`, 0 falhas**; 875 funções de teste de primeiro nível.
- **Com a mudança** (banco novo): `gofmt -l .` vazio; `go build ./...` e `go vet ./...` limpos; `go test -p 1 ./...` → **29 pacotes `ok`, 0 falhas**; 882 funções de teste (saem 17 do modelo antigo, entram 24).
- **Migration 146** aplicada do zero (pelo próprio teste de migrations) e em cima das anteriores; o teste-guarda de tabelas CRUD (regras 17 e 18) e o de arquitetura dos pacotes passaram.

**Regra desfeita → teste que falhou** (cada uma restaurada depois):

| Regra desfeita | Testes que caíram |
| --- | --- |
| Definir preço geral deixa de conflitar com rota de preço próprio | `TestTransporteGeralBloqueiaRotaComValorProprio`, `TestServicosConflitoGeralComRotaNaoGravaNada`, `TestIntegrationServicosTransporteDoCadastroAAprovacaoEPendencias` |
| Pendência deixa de filtrar pelos meses cobrados | `TestIntegrationServicosTransporteDoCadastroAAprovacaoEPendencias` |
| Veículo de outra rota aceito na inscrição | `TestIntegrationServicosTransporteDoCadastroAAprovacaoEPendencias` |
| Aprovar atividade coletiva sem turma | `TestIntegrationServicosAtividadeColetivaExigeTurmaEGuardaMembro` |
| Rota usada por inscrição pode ser apagada | `TestServicosRotaEVeiculoNomesRepetidosEExclusao` |
| Cancelamento por mês removido sem a guarda própria de pagamento em andamento | **nenhum** (a guarda que já existe ao cancelar cobrança também protege; a regra tem duas camadas, e o teste `...MesRemovidoNaoToCaCobrancaComPagamento` cobre o resultado) |

**Evidência do ponto novo mais delicado:** `TestIntegrationServicosTransporteDoCadastroAAprovacaoEPendencias` cria rota e veículo, o estudante pede, a academia aprova, a pendência do mês atual aparece (5000), o pagamento do mês inicia, a academia tira esse mês do prazo e a pendência some **e a cobrança aberta desse mês fica `cancelada`**; pagar um mês fora do prazo dá `409`.

## Falha antiga que não é desta tarefa

1. Numa **segunda** execução da suíte no **mesmo** banco, `TestIntegrationConsultarCobrancasEstudanteEstudanteVeTodosOsEstados` e `TestIntegrationConsultarCobrancasEstudanteFiltroEstadoFailedIncluiFalhadaLocal` falham (o código de estudante dos testes acumula cobranças entre execuções). Reproduzido em `main` @ `be988ce` sem a mudança. Em banco novo (como no CI) passam.
2. **Defeito de `main` corrigido de passagem:** o pagamento da mensalidade de serviço (`POST /financeiro/servicos-extras/obrigacao/pagamento`) ignorava o `solicitacao_id` do corpo e sempre respondia `400`. Nenhum teste o cobria; agora há.

## Como aplicar

1. **Confirme que a base não mudou** (e que o único avanço de `main` é o do Plano 2):
   ```bash
   git fetch origin && git diff --stat 452b3e3 origin/main
   git diff --stat 452b3e3 origin/main -- cmd internal migrations Dockerfile "Documentação da API.md" "docs/Planos de Atualização/Plano de Atualizações 1.md"
   ```
   O primeiro deve listar só `Plano de Atualizações 2.md`; o segundo, vazio. Se listar mais, não substitua e avise.
2. **Apague** os 12 arquivos de "Arquivos a apagar" (`git rm <arquivo>`).
3. **Copie** a pasta `rastreio-backend/` por cima da raiz.
4. **Verifique** (abaixo) e **só então** faça o deploy, depois da Tarefa 43 do `rastreio-frontend`.
5. Antes de aplicar em produção: a migration `146` apaga as duas tabelas antigas; não há academias, mas confirme com `SELECT (SELECT count(*) FROM projection_servicos_extras) AS servicos, (SELECT count(*) FROM projection_categorias_servico) AS categorias;` que as duas contagens são 0.

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
RUN_POSTGRES_INTEGRATION=1 DATABASE_URL=postgres://… go test ./...
```

Esperado: `gofmt` vazio; `build` e `vet` limpos; `go test` com todos os pacotes `ok` em **banco novo** (ver a falha antiga acima para execuções repetidas no mesmo banco).

## Fora de escopo

- **Front-end** das telas de Transporte e Atividades, do catálogo, das inscrições e a remoção das telas genéricas: Tarefa 43 do `rastreio-frontend`.
- **Módulo `servicos`** por recurso e bloqueio por módulo (Parte 9).
- **Validador único de dinheiro** em toda entrada (Parte 8); aqui só os campos de serviço.
- **Garantia 5** (cobrança pendente × pagamento pendente) e a troca de provedor: só no plano; nenhuma linha de código mudou nisso.
- **Correções urgentes das Partes 20 e 28** (documentos do encarregado na conta dele; reenvio de credenciais para encarregado, professor e secretário; conta nunca eliminada por falha de SMS): só registradas no plano, **não implementadas**.
- **Notificações** de inscrição em serviço (Parte 21), **horário acadêmico** (Parte 15, a turma da atividade não se cruza com ele).
- Migração de dados: não há academias.

## Marcar como feito

Já está: `**Estado:** feito`, `(feito)` no título e a secção **Resultado** abaixo; o documento nasce em `docs/Tarefas feitas/`.

## Checklist

- [ ] Base conferida
- [ ] 12 arquivos apagados; pasta copiada (20 alterados, 8 novos); migration 146 sem conflito de número (a última em `main` é a 145)
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test ./...` — todos `ok` (banco novo)
- [ ] Tarefa 43 do `rastreio-frontend` já em produção antes deste back-end

## Resultado

O back-end da Parte 7 está completo: catálogo fixo (Transporte com preço e prazo gerais, rotas, veículos e vínculo por rota e veículo; Atividade com horários, individual e coletiva com turma da atividade), cobrança só mensal nos meses marcados do ano civil com adesão tardia e cancelamento das cobranças não pagas de meses removidos, taxa de inscrição mantida, API 5.0.0 e plano atualizado. Desvios do plano: o `unico`/`preco_unico` foi removido por inteiro; a turma é escolhida na aprovação; foi corrigido de passagem o pagamento da mensalidade de serviço, que ignorava o `solicitacao_id`. O plano também ganhou, sem código, a Garantia 5 da Parte 8 e as correções urgentes das Partes 20 e 28.
