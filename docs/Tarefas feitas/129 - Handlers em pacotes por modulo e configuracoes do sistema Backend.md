# Tarefa 129 — Handlers em pacotes por módulo e configurações do sistema editáveis (Partes 2 e 3 do Plano de Atualizações) (backend)

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `673be19` (a Tarefa 128, Parte 1, já aplicada)
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório. **102 arquivos precisam ser apagados** (lista em "Como aplicar"). A pasta inclui o `docs/Plano_de_Atualizacoes.md` já atualizado.
**Ordem de deploy:** esta tarefa vai para produção **antes** da Tarefa 39 do `rastreio-frontend`. Sozinha ela não quebra nada: a Parte 2 é refatoração pura (mesmas 273 rotas, mesmos handlers) e as rotas novas da Parte 3 só são chamadas pela tela da Tarefa 39. Se o frontend (Tarefa 39) subir primeiro, o item "Sistema" aparece em Configurações e recebe `404` até este deploy. A migration `139` (duas tabelas novas, nenhum dado alterado) roda sozinha na subida do servidor.

---

## Contexto

Pedido: implementar as Partes 2 e 3 do `docs/Plano_de_Atualizacoes.md`, e devolver o próprio plano atualizado (na entrega da Parte 1 isso ficou esquecido; a regra agora está escrita no plano, "Como acompanhar", item 8).

- **Parte 2 — Back-end em pacotes.** `internal/handlers` era um único pacote com 62 arquivos de produção e 56 de teste. Agora há um pacote por módulo, um pacote de apoio para o que é comum e um pacote de helpers de teste. O HTTP do financeiro **continua** em `internal/handlers`: o plano permite deixá-lo por último, depois da Parte 6, sem bloquear nada.
- **Parte 3 — Configurações do sistema.** 15 variáveis de ambiente (as da proposta do plano) passam a poder ser gravadas no banco por administradores, com validação por tipo, auditoria, segredos cifrados e mascarados, e leitura **banco → ambiente**. Quem não gravar nada continua exatamente como antes.

## Decisões de design

1. **Uma tarefa para as duas Partes**, porque o pedido foi "Parte 2 e 3" e a Parte 3 depende da 2 (a rota nova já nasce em `handlers/administradores`). O plano é atualizado de uma vez.
2. **Financeiro (HTTP) fora da Parte 2**, como o plano prevê. Ele continua em `internal/handlers`, com 5 arquivos de produção (`efeitos_pagamento`, `financeiro_handlers`, `financeiro_pagamento_externo_handlers`, `financeiro_provedores_handlers`, `mensalidade_handlers`); `metodos_pagamento_pagador.go` ficou vazio porque tudo nele era usado por outros módulos, e foi inteiro para `apoio/financeiro.go`. Só o que outros módulos reaproveitavam dele foi para `apoio/financeiro.go`.
3. **A divisão foi feita por ferramenta, não à mão.** Um programa baseado em tipos (`go/packages`) classificou cada declaração: o que só o próprio módulo usa ficou no módulo; o que dois ou mais módulos usam foi para `apoio` (código) ou `testsupport` (helpers de teste), até não restar nenhum import entre módulos. Os nomes que mudaram de pacote e precisavam ser visíveis passaram a exportados (214 nomes: funções, tipos, campos e métodos). Onze campos que ficaram exportados ganharam `json:"-"` para não aparecerem em resposta nenhuma.
4. **Cada módulo ficou com os seus próprios wrappers de lote e itens de job** (por exemplo `CriarCursoBatch`, `AtivarAcademiaJobItem`, `RegistrarNotaBatchAsync`); só a infraestrutura genérica de lote (`BatchResponse`, contexto falso, enfileiramento) é compartilhada. Sem isso, os handlers inteiros de cursos, matérias, turmas, estudantes e academias teriam ido para `apoio` só porque o lote os chama.
5. **A regra "módulos não se importam" tem teste:** `TestPacotesDeModuloNaoSeImportam` (pacote `apoio`). Ele também impede que alguém importe o pacote `handlers` (financeiro) de dentro de um módulo.
6. **Testes que leem código-fonte por caminho** (`seguranca_codigo_test.go`, `unique_operation_guard_integration_test.go`, os de `academia_*`) foram apontados para os novos caminhos, e os helpers que faziam `os.Chdir("../..")` passaram a achar a raiz pelo `go.mod` (`testsupport.RaizDoRepositorio`), porque a profundidade do diretório do teste deixou de ser fixa. A **intenção de cada teste não mudou**; um deles (`TestSegurancaPontosDeCriacaoUsamSenhaTemporaria`) agora procura `TelefoneDestinoCredenciaisAcademia(` com a inicial maiúscula, porque a função foi exportada.
7. **Quem lê e quem altera (Parte 3):** `adm` e `fpp` consultam (`GET`); só `fpp` altera e remove (`PUT`, `DELETE`). O plano dizia apenas "administradores autorizados"; escolhi a leitura mais ampla e a escrita mais restrita.
8. **Lista aprovada = a proposta do plano, com um ajuste:** `MEGA_ROOT_FOLDER` ficou como **texto visível**, não segredo (é só o nome de uma pasta; escondê-lo impediria conferir o valor). `BREVO_API_KEY`, `ZIETT_API_KEY`, `MEGA_EMAIL` e `MEGA_PASSWORD` são segredos.
9. **Quando vale:** `imediato` (as 7 chaves lidas a cada uso: `ALLOWED_ORIGINS`, `JWT_EXPIRY_HOURS`, `LOG_LEVEL`, `EMAIL_FROM`, `BREVO_API_KEY`, `ZIETT_API_KEY`, `APPYPAY_RESOURCE`) ou `reinicio` (as cinco `RECONCILIACAO_*` e as três `MEGA_*`, lidas uma vez no arranque). `JWT_EXPIRY_HOURS` é `imediato`, mas só para tokens emitidos depois (sessões abertas mantêm a validade com que nasceram) e a tela diz isso.
10. **Cache sem polling.** O resolvedor carrega tudo uma vez no arranque e renova **de forma preguiçosa**, só quando alguém lê depois de 60 s. Nenhuma consulta periódica: um servidor ocioso não acorda o banco por causa disto (preocupação das Tarefas 19 e 21). `LOG_LEVEL` é lido sempre do cache, **sem nunca** consultar o banco, porque o logger o lê a cada chamada. Se a renovação falhar, o sistema segue com o que já estava em memória e só tenta de novo 30 s depois.
11. **Segredos:** o texto fica cifrado (AES-256-GCM, chave derivada de `JWT_SECRET`, com o nome da chave como dado autenticado) numa tabela própria, `configuracao_sistema_segredos`. **Nunca** vai para o ledger (nem cifrado), para a projeção, para resposta ou para log; o evento só registra que o segredo foi trocado, por quem e quando. É o mesmo desenho do cofre financeiro (a tabela não é projeção, então reconstruir a projeção não a toca). Se `JWT_SECRET` mudar e o segredo deixar de decifrar, a chave cai para o ambiente em vez de derrubar o servidor.
12. **Projeção com lápide e versão.** `projection_configuracao_sistema` mantém uma linha por chave; remover marca `definida = FALSE` em vez de apagar, e cada evento só vale se a versão dele for maior que a da linha. O serviço aplica o evento **na hora** (para a resposta já refletir o valor) e o gerenciador de projeções o reaplica depois sem efeito; reaplicar fora de ordem nunca ressuscita um valor removido.
13. **Ordem de arranque corrigida.** `finance.ValidarConfiguracaoProvedores()` (que exige `APPYPAY_RESOURCE`) rodava **antes** de existir banco; passou a rodar logo depois de `sysconfig.Iniciar`. Sem isso, um `APPYPAY_RESOURCE` gravado só no banco impediria o servidor de subir. Há teste estático desta ordem.
14. **A tabela `projection_sistema_config` (anos letivos) não foi reaproveitada:** tem colunas e semântica próprias dos anos letivos.

## O que foi atualizado e onde

**37 arquivos alterados + 174 novos (incluindo este documento) + 102 a apagar.** Números de linha são os do arquivo entregue.

### Parte 3 — novos

| Arquivo | O que é |
| --- | --- |
| `migrations/139_configuracao_sistema.sql` | Cria `projection_configuracao_sistema` (uma linha por chave, com `definida` e `version`) e `configuracao_sistema_segredos` (cofre cifrado). |
| `internal/sysconfig/definicoes.go` | Lista aprovada (`definicoes`), tipos, `Definicao.Validar` por tipo (inteiro/decimal, faixa, booleano, UUID, e-mail, origens CORS, pasta). |
| `internal/sysconfig/resolver.go` | `Resolver` com cache (banco → ambiente), renovação preguiçosa de 60 s, `ValorEmCache`, `FonteSQL`, `FonteEstatica` (testes), funções globais `String`, `Valor`, `Recarregar`. |
| `internal/sysconfig/cifra.go` | `Cifrar`/`Decifrar` (AES-256-GCM). |
| `internal/domain/aggregates/configuracao_sistema.go` | Agregado `ConfiguracaoSistema` (ID determinístico por chave), eventos `ConfiguracaoSistemaDefinida` e `ConfiguracaoSistemaRemovida`. |
| `internal/projections/configuracao_sistema_projection.go` | Projeção com guarda de versão e lápide; `Listar` com o nome de quem alterou. |
| `internal/services/configuracao_sistema_service.go` | `Listar`, `Definir`, `Remover`: valida, grava o cofre, salva o evento com auditoria, aplica a projeção na hora, recarrega o cache. |
| `internal/handlers/administradores/configuracao_sistema_handlers.go` | `ListarConfiguracoesSistema`, `DefinirConfiguracaoSistema`, `RemoverConfiguracaoSistema`. |

### Parte 3 — alterados

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `cmd/server/main.go` | `initDB` (linha 133) | `sysconfig.Iniciar(...)` logo depois das migrations. |
| `cmd/server/main.go` | `initDB` (linha 134) | `finance.ValidarConfiguracaoProvedores()` movida para depois do `Iniciar` (decisão 13). |
| `cmd/server/main.go` | `initProjections` (linha 202) | registra a projeção `configuracao_sistema`. |
| `cmd/server/main.go` | `initStorage` (linha 159) | `MEGA_ROOT_FOLDER` via `sysconfig.String`. |
| `cmd/server/main.go` | `setupRouter` (linhas 770–772) | `GET /admin/sistema/configuracoes` (`RequireAdm`), `PUT` e `DELETE /admin/sistema/configuracoes/:chave` (`RequireFPP`). |
| `cmd/server/main.go` | `corsMiddleware` (linha 781) | `ALLOWED_ORIGINS` lido a cada requisição via `sysconfig.String`. |
| `internal/middleware/auth.go` | `getJWTExpiryHours` (linhas 62 e 68) | `JWT_EXPIRY_HOURS` via `sysconfig.String`. |
| `internal/utils/logging.go` | `Debugf` (linha 12) | `LOG_LEVEL` via `sysconfig.StringEmCache` (nunca consulta o banco). |
| `internal/services/brevo_mailer.go` | `loadBrevoConfig` (linhas 39–40) | `BREVO_API_KEY` e `EMAIL_FROM` via `sysconfig.String`. |
| `internal/services/ziett_sms_test_client.go` | `NewZiettClientFromEnv` (linha 70) | `ZIETT_API_KEY` via `sysconfig.String`. |
| `internal/finance/provedor_appypay.go` | `appyPayResource` (linha 41) | `APPYPAY_RESOURCE` via `sysconfig.String`. |
| `internal/finance/reconciliacao_politica.go` | `ConfigReconciliacaoDoAmbiente` (linhas 209, 212, 215, 218, 221) | as cinco `RECONCILIACAO_*` via `sysconfig.String`. |
| `internal/storage/storage.go` | linhas 125, 129 (e-mail e senha do Mega), 144, 205 (pasta raiz), 302–303 (`sanitizeMegaError`) | credenciais e pasta do Mega via `sysconfig.String`. |
| `internal/db/safe_queries.go` | linhas 207–208 e 240 | permite os dois eventos e o tipo de agregado `ConfiguracaoSistema`. |
| `internal/domain/aggregates/aggregate.go` | linhas 151–152 | fábrica `ConfiguracaoSistema`. |
| `Documentação da API.md` | nova seção **16.7** (linha 7263); linhas 576, 1124, 8785, 8804 e 9051 | documenta as três rotas, a lista aprovada, segredos e auditoria; as menções antigas às variáveis passam a dizer "banco ou ambiente". |
| `.env.example` | cabeçalho (linhas 6–14) | explica quais variáveis também podem ser gravadas pela tela. |

### Parte 3 — testes novos

| Arquivo | O que cobre |
| --- | --- |
| `internal/sysconfig/definicoes_test.go` | lista fechada (nada de `DATABASE_URL`, `JWT_SECRET`...), segredos, `Validar` com 30 casos, erro nunca repete o segredo. |
| `internal/sysconfig/resolver_test.go` | banco vence ambiente, TTL e ausência de polling, `ValorEmCache` nunca consulta, falha mantém cache e espera 30 s, cifra ida e volta, cifra presa à chave, `JWT_SECRET` trocado não vaza. |
| `internal/handlers/administradores/configuracao_sistema_integration_test.go` | 9 testes com PostgreSQL real: listagem e máscara, "de X para Y" com nome de quem alterou, segredo ausente de resposta, log, ledger, projeção e cofre, validação e chaves fora da lista, remover volta ao ambiente, aviso de reinício, projeção idempotente e fora de ordem, reconstrução da projeção preserva segredos, segredo ilegível cai para o ambiente. |
| `cmd/server/configuracao_sistema_test.go` | CORS lê o banco, rotas existem e exigem autenticação, papéis (`RequireAdm` leitura, `RequireFPP` escrita), ordem de arranque (migrations → `Iniciar` → validação). |
| `internal/middleware/auth_sysconfig_test.go`, `internal/utils/logging_sysconfig_test.go`, `internal/services/configuracao_sistema_consumidores_test.go`, `internal/finance/configuracao_sistema_test.go`, `internal/storage/configuracao_sistema_test.go` | cada consumidor lê banco → ambiente: JWT, log, Brevo, Ziett, AppyPay, reconciliação, Mega (inclui credenciais redigidas no erro). |

### Parte 2 — pacotes novos em `internal/handlers`

| Pacote | O que cobre | Arquivos | Linhas | Testes |
| --- | --- | --- | --- | --- |
| `internal/handlers/academias/` | cadastro e gestão de academias, status escolar, anos letivos, nível escolar, alteração de NIF | 10 | 3400 | 6 |
| `internal/handlers/administradores/` | admins, bootstrap, auditoria de deleções, métricas, quota e migração de storage, **configurações do sistema (novo)** | 11 | 2113 | 4 |
| `internal/handlers/autenticacao/` | login, e-mails de verificação e recuperação, dois fatores, perfil, contato, credenciais | 6 | 2418 | 6 |
| `internal/handlers/avaliacoes/` | avaliação final: listagens, regras, modelo avaliativo escolar | 5 | 1906 | 4 |
| `internal/handlers/comunicacao/` | remetentes e envio de comunicação por SMS | 3 | 379 | 0 |
| `internal/handlers/cursos/` | cursos (e seus lotes e itens de job) | 4 | 1112 | 1 |
| `internal/handlers/documentos/` | download de documentos e documentos extra | 2 | 659 | 2 |
| `internal/handlers/estudantes/` | cadastro, listagens, edição de dados sensíveis, deleção, BI, lotes de cadastro | 7 | 2574 | 7 |
| `internal/handlers/faltas/` | lançamento, correção e configuração de faltas | 3 | 615 | 2 |
| `internal/handlers/lotes/` | consulta, stream e nova tentativa de jobs assíncronos | 1 | 344 | 2 |
| `internal/handlers/materias/` | matérias disciplinares (e seus lotes e itens de job) | 4 | 770 | 1 |
| `internal/handlers/notas/` | notas e categorias de nota | 3 | 772 | 1 |
| `internal/handlers/registros/` | listagem de registros de notas e faltas | 1 | 415 | 1 |
| `internal/handlers/servicosextras/` | serviços extras, categorias e solicitações | 3 | 944 | 4 |
| `internal/handlers/sumarios/` | sumários de aula | 1 | 349 | 0 |
| `internal/handlers/turmas/` | turmas, grupos e vínculo de estudantes | 4 | 1028 | 2 |

Arquivos de produção por pacote não contam `doc.go`. Cada pacote ganhou um `doc.go` com a regra. O pacote `internal/handlers` (sem subpasta) ficou com o HTTP do financeiro: 5 arquivos de produção e 12 de teste, **no mesmo caminho de antes** (por isso aparecem como alterados, não movidos: só os imports e as referências a outros pacotes mudaram).

**`internal/handlers/apoio/`** (código que mais de um módulo usa; cada arquivo leva o nome do módulo de onde o trecho veio):

| Arquivo | Linhas |
| --- | --- |
| `internal/handlers/apoio/academias.go` | 280 |
| `internal/handlers/apoio/administradores.go` | 65 |
| `internal/handlers/apoio/autenticacao.go` | 210 |
| `internal/handlers/apoio/avaliacoes.go` | 1744 |
| `internal/handlers/apoio/comunicacao.go` | 183 |
| `internal/handlers/apoio/cursos.go` | 16 |
| `internal/handlers/apoio/documentos.go` | 321 |
| `internal/handlers/apoio/estudantes.go` | 161 |
| `internal/handlers/apoio/faltas.go` | 10 |
| `internal/handlers/apoio/financeiro.go` | 106 |
| `internal/handlers/apoio/helpers.go` | 428 |
| `internal/handlers/apoio/lotes.go` | 510 |
| `internal/handlers/apoio/materias.go` | 30 |
| `internal/handlers/apoio/matricula_cadastro_helpers.go` | 259 |
| `internal/handlers/apoio/notas.go` | 140 |
| `internal/handlers/apoio/registros.go` | 109 |
| `internal/handlers/apoio/removed_fields.go` | 135 |
| `internal/handlers/apoio/servicosextras.go` | 547 |
| `internal/handlers/apoio/turmas.go` | 238 |

**`internal/handlers/testsupport/`** (helpers de teste usados por mais de um módulo; só `_test.go` os importam; sem `Test…`): `raiz.go` (novo, `RaizDoRepositorio`) e os helpers vindos dos testes de academia, estudante, categoria de serviço, autenticação e financeiro.

**Como ler o movimento:** cada arquivo de produção que existia em `internal/handlers/` agora existe com o **mesmo nome** em `internal/handlers/<módulo>/` (a coluna "Arquivos" acima), exceto os do financeiro, que ficaram. Os trechos que outros módulos usavam saíram do arquivo e foram para `apoio/<módulo>.go`. Nada foi reescrito: o corpo das funções é o mesmo; mudam só o `package`, os imports e os nomes qualificados (`apoio.GetRepository`).

### Parte 2 — alterados fora de `handlers`

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `cmd/server/main.go` | imports (linhas 3–45) e as **306** referências `handlers.X` | as do financeiro continuam `handlers.X`; todas as outras passam a `<pacote>.X` (ex.: `notas.ListarNotas`); o import antigo de `handlers` foi trocado pelos pacotes usados. |
| `cmd/server/credenciais_temporarias_integration_test.go`, `cmd/server/turma_vinculo_estudante_integration_test.go` | referências a `handlers.` | idem. |
| `internal/finance/mensalidade_pendencias.go`, `internal/services/email_html_templates.go`, `internal/services/comunicacao_ziett_client.go`, `internal/services/comunicacao_gosms_client.go`, `internal/domain/aggregates/remetente_comunicacao.go` | comentários | citam o novo caminho do arquivo. Nenhum código mudou. |
| `docs/Manual avaliacao final ensino superior.md` | tabela de arquivos | aponta o motor e o parser da fórmula para `apoio/avaliacoes.go` e as regras para `avaliacoes/`. |
| `docs/Plano_de_Atualizacoes.md` | tabela da Visão geral, Partes 1, 2 e 3, "Como acompanhar", Registro | Parte 1 ✅, Parte 2 Parcial, Parte 3 ✅, regra 8 nova (o plano vai em toda entrega), próxima tarefa livre 130, Registro de andamento. |

### Parte 2 — testes

| Arquivo | O que mudou |
| --- | --- |
| `internal/handlers/apoio/arquitetura_test.go` (**novo**) | `TestPacotesDeModuloNaoSeImportam`. |
| `internal/handlers/administradores/seguranca_codigo_test.go` | raiz achada pelo `go.mod`; caminhos dos arquivos verificados e `TelefoneDestinoCredenciaisAcademia(`. |
| `internal/handlers/estudantes/unique_operation_guard_integration_test.go`, `internal/handlers/academias/academia_notificacao_admins_test.go`, `internal/handlers/academias/academia_register_publica_test.go` | caminhos novos; `main.go` achado pela raiz do repositório. |
| `internal/handlers/testsupport/*.go` | `os.Chdir` e `ReadFile` passaram a usar a raiz do repositório. |
| demais testes | movidos para o pacote do módulo que testam, sem alteração de lógica. |

## Validação realizada

**Ambiente:** Go 1.24.11 (o `go.mod` pede o toolchain 1.24.12; o sandbox só tem a 1.24.11 via pip) e PostgreSQL 16. O proxy de módulos do Go está bloqueado, então as dependências foram resolvidas por espelhos do GitHub num `go.mod` **alternativo, fora do repositório** (`-modfile`). O `go.mod` e o `go.sum` reais não foram tocados e não fazem parte da entrega.

**Baseline de `main` @ `673be19`** (banco novo, variáveis do CI, `go test -p 1 -count=1 -json ./...`): todos os pacotes `ok`; **1057 testes passam** (contando subtestes) e 48 são pulados por desenho.

**Árvore entregue** (clone de `main` com a pasta copiada por cima e os arquivos apagados):

```
gofmt -l .        -> (vazio)
go build ./...    -> rc=0
go vet ./...      -> rc=0
go test -p 1 -count=1 ./...  -> todos ok: 1095 passam, 48 pulados, 0 falham
```

- **Parte 2 — mesmo resultado antes e depois:** os **1057 testes do baseline passam todos, com os mesmos nomes**; os 38 a mais são os testes novos desta tarefa. Nenhum teste foi removido ou enfraquecido.
- **Mesmas rotas e mesmos handlers:** extraí de `setupRouter()` a lista `método caminho -> nome do handler` antes e depois: **273 rotas, diff vazio**.
- **Regra de arquitetura:** com um `import` proibido de `notas` dentro de `cursos` o teste cai; sem ele passa.
- **Parte 3 — desfazer → falhar → restaurar:** (1) tirei a guarda de versão da projeção → `…ProjecaoIdempotenteEForaDeOrdem` caiu; (2) fiz o serviço registrar o segredo no log → `…SegredoNuncaApareceEmLugarNenhum` caiu; (3) fiz o ambiente vencer o banco → `TestBancoVenceSobreOAmbiente…` e `TestGetJWTExpiryHoursLeDoBanco…` caíram. Restaurado, tudo voltou a passar.
- **Repetível:** os testes de integração novos passam duas vezes seguidas no mesmo banco.
- **Segredos:** nos testes, um segredo único por execução não aparece em nenhum `payload` ou `metadata` do ledger (nem o texto cifrado), na coluna `valor` da projeção, em resposta, nem no log capturado.

**Não foi validado:** deploy real; o comportamento multi-instância (a renovação em 60 s foi testada com relógio falso, não com duas instâncias); a tela (ver Tarefa 39).

## Como aplicar

1. **Confirme que a base não mudou:** `git diff --stat 673be19 HEAD -- cmd internal migrations "Documentação da API.md" docs .env.example`. Vazio = pode substituir; se aparecer algum arquivo, ele mudou depois da base — não substitua e avise. Confirme também que `migrations/` ainda termina em `138_…` (esta tarefa cria a `139`).
2. **Apague** os 102 arquivos antigos (todos foram movidos para os pacotes novos; nada se perde). Na raiz do repositório:

```bash
git rm -q -- internal/handlers/academia_handlers.go
git rm -q -- internal/handlers/academia_notificacao_admins_test.go
git rm -q -- internal/handlers/academia_register_contact_test.go
git rm -q -- internal/handlers/academia_register_errors_test.go
git rm -q -- internal/handlers/academia_register_publica_test.go
git rm -q -- internal/handlers/academia_status_escolar_handlers.go
git rm -q -- internal/handlers/admin_handlers.go
git rm -q -- internal/handlers/admin_registros_handlers.go
git rm -q -- internal/handlers/ano_letivo_helpers.go
git rm -q -- internal/handlers/ano_letivo_helpers_test.go
git rm -q -- internal/handlers/anos_academicos_handlers.go
git rm -q -- internal/handlers/async_batch_handlers.go
git rm -q -- internal/handlers/async_batch_handlers_test.go
git rm -q -- internal/handlers/auditoria_autorizacao_test.go
git rm -q -- internal/handlers/auditoria_delecoes_handler.go
git rm -q -- internal/handlers/auth_email_handlers.go
git rm -q -- internal/handlers/auth_email_handlers_integration_test.go
git rm -q -- internal/handlers/auth_handlers.go
git rm -q -- internal/handlers/avaliacao_final_faltas.go
git rm -q -- internal/handlers/avaliacao_final_faltas_test.go
git rm -q -- internal/handlers/avaliacao_final_formula_test.go
git rm -q -- internal/handlers/avaliacao_final_handler.go
git rm -q -- internal/handlers/avaliacao_final_regras.go
git rm -q -- internal/handlers/avaliacao_final_regras_test.go
git rm -q -- internal/handlers/batch_context.go
git rm -q -- internal/handlers/batch_handlers.go
git rm -q -- internal/handlers/bilhete_identidade_sem_academia_handlers.go
git rm -q -- internal/handlers/bilhete_identidade_sem_academia_integration_test.go
git rm -q -- internal/handlers/bootstrap_handler.go
git rm -q -- internal/handlers/bootstrap_seguranca_test.go
git rm -q -- internal/handlers/categoria_servico_get_by_id_integration_test.go
git rm -q -- internal/handlers/categoria_servico_handlers.go
git rm -q -- internal/handlers/comunicacao_envio.go
git rm -q -- internal/handlers/comunicacao_handlers.go
git rm -q -- internal/handlers/configuracao_status_handlers.go
git rm -q -- internal/handlers/contact_handlers.go
git rm -q -- internal/handlers/contact_handlers_test.go
git rm -q -- internal/handlers/credenciais.go
git rm -q -- internal/handlers/credenciais_reenvio_handlers.go
git rm -q -- internal/handlers/credenciais_test.go
git rm -q -- internal/handlers/cursos_handlers.go
git rm -q -- internal/handlers/cursos_handlers_test.go
git rm -q -- internal/handlers/documento_download_handlers.go
git rm -q -- internal/handlers/documento_download_handlers_test.go
git rm -q -- internal/handlers/documento_extra_handlers.go
git rm -q -- internal/handlers/documento_extra_upload.go
git rm -q -- internal/handlers/documentos_academicos_normalizacao_test.go
git rm -q -- internal/handlers/dois_fatores_handlers.go
git rm -q -- internal/handlers/dois_fatores_integration_test.go
git rm -q -- internal/handlers/estudante_delecao_academia_handlers.go
git rm -q -- internal/handlers/estudante_delecao_academia_integration_test.go
git rm -q -- internal/handlers/estudante_handlers.go
git rm -q -- internal/handlers/estudantes_busca_livre_integration_test.go
git rm -q -- internal/handlers/estudantes_listagem_vinculo_integration_test.go
git rm -q -- internal/handlers/faltas_configuracao_handlers.go
git rm -q -- internal/handlers/faltas_configuracao_integration_test.go
git rm -q -- internal/handlers/faltas_handlers.go
git rm -q -- internal/handlers/faltas_sumario_test.go
git rm -q -- internal/handlers/helpers.go
git rm -q -- internal/handlers/job_handlers.go
git rm -q -- internal/handlers/job_handlers_test.go
git rm -q -- internal/handlers/job_item_handlers.go
git rm -q -- internal/handlers/materia_disciplinar_handlers.go
git rm -q -- internal/handlers/materia_disciplinar_handlers_test.go
git rm -q -- internal/handlers/matricula_cadastro_helpers.go
git rm -q -- internal/handlers/matricula_cadastro_helpers_test.go
git rm -q -- internal/handlers/metodos_pagamento_pagador.go
git rm -q -- internal/handlers/metrics_handler.go
git rm -q -- internal/handlers/modelo_avaliativo_escolar.go
git rm -q -- internal/handlers/modelo_avaliativo_escolar_test.go
git rm -q -- internal/handlers/nivel_escolar_handlers.go
git rm -q -- internal/handlers/nivel_escolar_handlers_integration_test.go
git rm -q -- internal/handlers/notas_handlers.go
git rm -q -- internal/handlers/notas_handlers_test.go
git rm -q -- internal/handlers/profile_handlers.go
git rm -q -- internal/handlers/query_handlers.go
git rm -q -- internal/handlers/redigir_evento_sensivel_test.go
git rm -q -- internal/handlers/registros_estudante_filters.go
git rm -q -- internal/handlers/registros_handlers.go
git rm -q -- internal/handlers/registros_handlers_test.go
git rm -q -- internal/handlers/removed_fields.go
git rm -q -- internal/handlers/removed_fields_test.go
git rm -q -- internal/handlers/seguranca_codigo_test.go
git rm -q -- internal/handlers/senha_temporaria_projecao_integration_test.go
git rm -q -- internal/handlers/servico_extra_catalogo_estudante_integration_test.go
git rm -q -- internal/handlers/servico_extra_elegibilidade_test.go
git rm -q -- internal/handlers/servico_extra_handlers.go
git rm -q -- internal/handlers/servico_extra_pendencias_gratuito_integration_test.go
git rm -q -- internal/handlers/servico_extra_solicitacao_handlers.go
git rm -q -- internal/handlers/sistema_handler.go
git rm -q -- internal/handlers/solicitacao_alteracao_nif_academia_handlers.go
git rm -q -- internal/handlers/solicitacao_edicao_bi_documento_integration_test.go
git rm -q -- internal/handlers/solicitacao_edicao_dado_estudante_handlers.go
git rm -q -- internal/handlers/storage_migration_handlers.go
git rm -q -- internal/handlers/storage_quota_handlers.go
git rm -q -- internal/handlers/sumario_handlers.go
git rm -q -- internal/handlers/tokens_recuperacao_integration_test.go
git rm -q -- internal/handlers/turmas_grupo_integration_test.go
git rm -q -- internal/handlers/turmas_handler.go
git rm -q -- internal/handlers/turmas_handler_test.go
git rm -q -- internal/handlers/unique_operation_guard_integration_test.go
git rm -q -- internal/handlers/ziett_sms_test_handler.go
```

3. **Copie** a pasta `rastreio-backend/` para a raiz do repositório, mantendo os caminhos.
4. **Verifique** (abaixo).
5. Commit: o texto está na resposta da entrega.

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
RUN_POSTGRES_INTEGRATION=1 JWT_SECRET=… DATABASE_URL=postgres://… go test -p 1 ./...
```

Esperado: `gofmt` sem saída; `build` e `vet` limpos; todos os pacotes `ok`. A migration `139` é aplicada na subida do servidor. Os testes `TestIntegration…` só rodam de verdade com um PostgreSQL de teste descartável.

## Fora de escopo

- **HTTP do financeiro** (Parte 2): continua em `internal/handlers`; entra por último.
- **Reconstrução pela tela:** a projeção `configuracao_sistema` está registrada no gerenciador, mas a lista de "Rebuild de projeções" do frontend (Admin) não foi estendida: ela já não lista as projeções mais recentes (por exemplo `faltas_configuracao`).
- **Histórico de alterações por chave na tela:** o ledger já guarda tudo (quem, IP, de X para Y); consultar esse histórico por uma rota fica para quando for pedido.
- **Novas variáveis** das próximas partes (limites de SMS, tarifa, tamanho de arquivo): entram com uma linha em `sysconfig.definicoes` quando suas Partes forem feitas.
- **Tela:** Tarefa 39 do `rastreio-frontend`.

## Perguntas

Só estas mudam o resultado (cada uma é uma linha na lista de `internal/sysconfig/definicoes.go`):

1. `MEGA_ROOT_FOLDER` deve ser **segredo** (mascarado), como a proposta do plano dizia, em vez de texto visível?
2. O papel `adm` deve poder **consultar** as configurações, ou só o `fpp`? (Implementei: `adm` e `fpp` consultam, só `fpp` altera.)
3. A lista de 15 chaves está aprovada, ou alguma deve sair?

## Marcar como feito

1. `**Estado:** feito`, `(feito)` no título.
2. Secção **Resultado** no fim: um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mover para `docs/Tarefas feitas/`, mesmo nome.

## Checklist

- [ ] Base conferida (`673be19`; migrations terminam em `138`)
- [ ] 102 arquivos apagados; pasta copiada
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test -p 1 ./...` — todos `ok`
- [ ] Tarefa 39 do `rastreio-frontend` só entra em produção depois deste deploy
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`

## Resultado

As Partes 2 e 3 do plano foram implementadas e validadas. `internal/handlers` foi dividido em 16 pacotes de módulo, `apoio` e `testsupport`, com as mesmas 273 rotas e os mesmos 1057 testes passando; o HTTP do financeiro ficou como estava, por previsão do plano. As 15 configurações aprovadas passaram a ler banco → ambiente, com API para `adm`/`fpp`, auditoria, segredos cifrados e mascarados e indicação "na hora / no reinício". Desvios: `MEGA_ROOT_FOLDER` ficou como texto visível; a lista de reconstrução de projeções do frontend não foi estendida.
