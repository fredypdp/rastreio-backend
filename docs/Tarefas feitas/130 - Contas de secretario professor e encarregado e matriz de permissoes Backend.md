# Tarefa 130 — Contas de secretário, professor e encarregado e matriz de permissões  (backend)

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `ffc13e2`
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório
**Ordem de deploy:** **Backend e frontend vão juntos.** As Tarefas 130 e 131 (`rastreio-backend`) entram no **mesmo pacote** (arquivos em comum) e devem ir para produção **na mesma janela** das Tarefas 40 e 41 (`rastreio-frontend`). A Tarefa 131 é uma mudança **incompatível** (API 4.0.0): sem o frontend novo, o cadastro de estudante do nível escolar com encarregado novo e a edição do telefone do encarregado do frontend atual deixam de funcionar. Ordem sugerida: **backend primeiro, frontend logo a seguir.** As migrations 140 e 141 rodam sozinhas na subida.
**Versão da API:** `3.0.1` → `4.0.0` (as Tarefas 130 e 131 juntas; ver o histórico na seção 1.2 da Documentação da API)

---

## Contexto

Plano de Atualizações, **Parte 4** (usuários e permissões), mais duas notas do pedido:

- **Nota 1 — permissões do secretário.** O plano listava só "mesmas permissões da academia, exceto lançar notas e faltas". O secretário deve poder ter todas as funções da academia (menos as dos professores), mas **a academia escolhe, ao criar cada secretário, quais permissões ele terá**; e, por estar abaixo da conta da academia, ele não pode executar ações retroativas (eliminar outro secretário, alterar a própria conta da academia). Faltava ainda o **gerenciamento de sumários**.
- **Nota 2 — documentação.** A entrega anterior não devolveu o plano atualizado nem a versão da `Documentação da API.md` em SemVer. Esta devolve os dois, e o plano ganhou o **passo 9** em "Como acompanhar" com a regra de escolha do número (a orientação existia só como menção na regra 13).

Antes desta tarefa só existiam admin, academia e estudante; o encarregado era apenas um telefone e um BI no cadastro do estudante.

## Decisões de design

- **Identidade efetiva em vez de reescrever handlers.** As centenas de handlers tratam `user_id` como a academia (143 pontos) ou o estudante. O `AuthMiddleware`, depois de validar a conta **real** (estado, sessão, senha temporária), troca no contexto: secretário → academia; encarregado → educando (`X-Educando`). Os handlers e o isolamento entre academias ficam intactos. As rotas de conta (senha, perfil, e-mail, telefone, 2FA, logout) nunca são trocadas.
- **Negar por padrão.** O secretário só alcança rotas listadas em `internal/permissoes/rotas.go` (livre, exige permissão, reservada ou negada). Rota nova esquecida fica **inacessível** em vez de aberta. O teste `TestPermissoesTodaRotaTemDecisaoParaOSecretario` percorre o router e falha se aparecer rota sem decisão.
- **Catálogo fechado e normalizado.** As permissões gravadas já incluem as implicadas (`gerir` → `ver`); o servidor recusa chave fora do catálogo, então `notas.lancar` e `secretarios.gerir` não existem para ninguém.
- **Reservado à academia:** gerir secretários; `PUT /academia/dados`, nível escolar, NIF, alvará; escolha do provedor e credenciais/segredos de pagamento. **Negado também à academia delegante:** lançar/corrigir notas e faltas e pagar.
- **Auditoria do ator sem tocar nos 97 pontos de `AuditContext`:** o repositório devolvido por `GetRepository(c)` já leva o ator e o grava no `metadata`. `user_id`/`user_type` continuam sendo a conta em nome de quem se agiu.
- **Uma tabela para os três tipos** (`projection_contas_pessoa`): mesmo ciclo de conta, então login, e-mail único e 2FA consultam um só lugar. Códigos `SC`/`PF`/`EN` + 6 dígitos (não colidem com estudante nem academia). E-mail único entre **todos** os tipos.
- **Professor com rotas próprias** (`/professor/*`) em vez de reaproveitar as da academia com filtro: cada consulta nasce filtrada pelos vínculos, não há filtro a esquecer.
- **Encarregado:** fase de **ação** enquanto o educando está no nível escolar; fase de **leitura** (só `GET` de notas e faltas, com `ano_academico` forçado ao período escolar) depois de `status_escolar_medio = finalizado` ou matrícula no superior. Um encarregado ativo por estudante; vários educandos por encarregado.
- **Atomicidade:** `SaveManyWithAudit` grava estudante e encarregado novos na mesma transação.
- **Reaproveita:** a cadeia de credenciais temporárias, os motivos de senha, o 2FA por e-mail, o guard de operação única e o padrão de agregado/projeção do `Admin`.

## O que foi atualizado e onde

**15 arquivos alterados + 18 novos** (nesta tarefa; a outra tarefa do pacote lista os seus).

Arquivos compartilhados com a Tarefa 131 (`main.go`, `repository.go` ↔ `SaveManyWithAudit`, `middleware/auth.go`, `encarregado_integration_test.go`, a documentação da API e o plano): cada linha da tabela diz o que é desta tarefa.

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `migrations/140_contas_secretario_professor_encarregado.sql` (**novo**) | — | Tabela `projection_contas_pessoa` (secretário, professor e encarregado numa só tabela, coluna `tipo`), `projection_professor_vinculos` (um professor ativo por par turma+matéria, índice único parcial) e `projection_encarregado_educandos` (um encarregado ativo por estudante). Amplia os CHECK de `auth_tokens.user_type` e `dois_fatores_desafios.user_type` para os três tipos novos. |
| `internal/permissoes/permissoes.go` (**novo**) | `Catalogo` (linha 75) | Catálogo fechado das permissões do secretário (inclui `sumarios.ver`/`sumarios.gerir`, que faltavam no plano), com as implicações (`gerir` traz `ver`). |
| `internal/permissoes/permissoes.go` (**novo**) | `Normalizar` (linha 136) | Valida as chaves (fora do catálogo é erro, ex.: `notas.lancar`), completa as implicadas, remove duplicadas e ordena. É o conjunto gravado. |
| `internal/permissoes/rotas.go` (**novo**) | `init` (linha 77) | Tabela de decisão `MÉTODO /rota` → livre, exige permissão, **reservada** à academia ou **negada** (lançar notas/faltas, pagar). Cobre todas as rotas da academia e as novas. Rota ausente = negada. |
| `internal/permissoes/permissoes_test.go` (**novo**) | — | Testes do catálogo, da normalização, da coerência da tabela de rotas e das decisões que não podem mudar sem querer. |
| `internal/domain/aggregates/conta_pessoa.go` (**novo**) | `ContaPessoa` (linha 41) | Núcleo comum dos três agregados: criar, dados, senha (mesmos motivos das outras contas), e-mail, 2FA, ativar/desativar/eliminar. Eventos genéricos `ContaPessoa*`. |
| `internal/domain/aggregates/conta_pessoa.go` (**novo**) | `ValidarDadosContaPessoa` (linha 91) | Regras de campo: nome, gênero, nascimento (mínimo 18 anos), telefone móvel nacional, e-mail, hash. |
| `internal/domain/aggregates/contas_especificas.go` (**novo**) | `Secretario` (linha 19) | Agregado do secretário: criar com permissões normalizadas, `DefinirPermissoes`. |
| `internal/domain/aggregates/contas_especificas.go` (**novo**) | `Professor` (linha 113) | Agregado do professor: vínculos turma+matéria (`AtribuirVinculo`, `RemoverVinculo`). |
| `internal/domain/aggregates/contas_especificas.go` (**novo**) | `ValidarRelacao` (linha 238) | Relação opcional do encarregado (`pai`, `mae`, `irmao`, `irma`, `outro` com texto obrigatório). |
| `internal/domain/aggregates/contas_especificas.go` (**novo**) | `Encarregado` (linha 273) | Agregado do encarregado: vincular, desvincular e atualizar a relação de cada educando. |
| `internal/domain/aggregates/conta_pessoa_test.go` (**novo**) | — | Testes unitários dos três agregados (criação, recarga pelo ledger, ciclo de vida, vínculos únicos, relação). |
| `internal/domain/aggregates/aggregate.go` | fábrica de agregados (linha 133) | Registra os tipos `Secretario`, `Professor` e `Encarregado` na fábrica. |
| `internal/db/safe_queries.go` | tipos de agregado permitidos (linha 236) | Libera os três `aggregate_type` e os eventos novos (`ContaPessoa*`, vínculos, permissões, educandos). |
| `internal/db/repository.go` | `AtorAuditoria` (linha 31) | Ator da auditoria: o repositório da requisição grava `ator_id` e `ator_tipo` no `metadata` de cada evento quando alguém age em nome de outra conta. |
| `internal/db/repository.go` | `SaveManyWithAudit` (linha 272) | Grava eventos de vários agregados **na mesma transação** (usado pelo cadastro do estudante + encarregado e pela troca de encarregado). |
| `internal/projections/contas_projection.go` (**novo**) | `ContasProjection` (linha 20) | Projeção `contas`: as três tabelas, rebuild, consultas (login, e-mail, encarregado por telefone, vínculos, educandos). Eliminar encerra vínculos; a exclusão da conta do estudante encerra o vínculo com o encarregado. |
| `internal/projections/manager.go` | ordem de rebuild (linha 351) | `contas` entra na ordem de reconstrução (depois de academias). |
| `internal/projections/dois_fatores.go` | lista de tabelas permitidas (linha 23) | Permite a tabela nova nos helpers de 2FA e senha temporária. |
| `internal/middleware/identidade_efetiva.go` (**novo**) | `aplicarIdentidade` (linha 111) | **Identidade efetiva**: secretário → academia (depois da porta de permissões); encarregado → educando indicado em `X-Educando`; professor → só `/professor/*`. Rotas de conta nunca são substituídas. |
| `internal/middleware/identidade_efetiva.go` (**novo**) | `aplicarSecretario` (linha 210) | Porta do secretário: academia ativa, rota classificada, reservada/negada, permissão exigida. Códigos `SECRETARIO_SEM_PERMISSAO`, `ACAO_RESERVADA_A_ACADEMIA`, `ACAO_NAO_PERMITIDA_AO_SECRETARIO`, `ROTA_FORA_DO_PERFIL`. |
| `internal/middleware/identidade_efetiva.go` (**novo**) | `aplicarEncarregado` (linha 327) | Vínculo ativo, educando existente, fase de **ação** (faz o que o estudante faz) ou de **leitura** (só notas e faltas, com `ano_academico` forçado ao período escolar). |
| `internal/middleware/auth.go` | `consultarEstadoUsuario` (linha 404) | Estado (status, sessões, senha temporária) dos três tipos novos; `finalizarVerificacaoStatusUsuario` bloqueia conta não ativa; `AuthMiddleware` e `OptionalAuthMiddleware` chamam a identidade efetiva; novos `RequireProfessor` e `RequireEncarregado`. |
| `internal/handlers/apoio/helpers.go` | `GetRepository` (linha 25) | Devolve o repositório já com o ator da requisição: a auditoria dos ~97 pontos que montam `AuditContext` passa a registrar o secretário/encarregado sem tocar neles. |
| `internal/handlers/apoio/contas.go` (**novo**) | `EmailEmUso` (linha 26) | E-mail único entre **todos** os tipos de conta; gerador de código `SC/PF/EN + 6 dígitos`; telefone de destino das credenciais; data de nascimento. |
| `internal/handlers/apoio/conta_pessoa_plano.go` (**novo**) | `PrepararConta` (linha 41) | Valida e prepara uma conta nova (código, senha temporária, hash); erros prefixados com a pessoa (`encarregado: …`). Em `apoio` porque pacotes de módulo só importam `apoio`. |
| `internal/handlers/apoio/conta_pessoa_plano.go` (**novo**) | `PlanejarEncarregado` (linha 132) | Decide entre **reutilizar** o encarregado (telefone ou e-mail) e **criar**; reserva o telefone com o guard de operação única e, se a projeção ainda não viu a conta recém-criada, a recupera do ledger. |
| `internal/handlers/contas/contas_handlers.go` (**novo**) | `CriarSecretario` (linha 106) | Secretários (reservados à academia): criar com permissões ou `todas_permissoes`, listar, obter, editar dados e permissões, ativar, desativar, eliminar. Professores: criar com vínculos, listar, obter, editar, ciclo de vida, atribuir/remover vínculo, **reatribuir** par. |
| `internal/handlers/contas/contas_handlers.go` (**novo**) | `validarParTurmaMateria` (linha 324) | Vínculo só entre turma e matéria **da academia, ativas e do mesmo nível/curso**; um professor ativo por par (409). |
| `internal/handlers/contas/professor_area_handlers.go` (**novo**) | `ListarMeusVinculos` (linha 61) | Área do professor: vínculos, turmas, estudantes, notas e faltas **já filtrados pelos vínculos ativos** (turma sem vínculo = 404). |
| `internal/handlers/contas/encarregados_handlers.go` (**novo**) | `TrocarEncarregado` (linha 72) | Troca de encarregado na mesma transação; busca por telefone/e-mail; rotas do encarregado (`/encarregado/educandos`, relação); `DELETE /estudante/encarregado` (só depois de concluir o nível escolar). |
| `internal/handlers/autenticacao/auth_handlers.go` | login passo 4 (linha 163) | Login em cascata ganha o passo 4 (código `SC/PF/EN` ou e-mail verificado); mensagens de status e senha temporária para os tipos novos. |
| `internal/handlers/autenticacao/contas_pessoa_auth.go` (**novo**) | `perfilContaPessoa` (linha 180) | Caminho genérico dos três tipos para perfil (com a academia, as permissões ou os educandos), senha, contato, verificação de e-mail e reset por link. |
| `internal/handlers/autenticacao/auth_email_handlers.go` | — | Verificação de e-mail, solicitação e reset de senha por link aceitam os três tipos novos. |
| `internal/handlers/autenticacao/contact_handlers.go` | `emailEmUsoPorOutro` (linha 113) | `PUT /me/email` e `/me/telefone` para os tipos novos; unicidade de e-mail passa a incluí-los. |
| `internal/handlers/autenticacao/dois_fatores_handlers.go` | — | 2FA por e-mail aceita os tipos novos (identidade, mapa de agregados, tipos permitidos). |
| `internal/handlers/autenticacao/profile_handlers.go` | — | `GET /meu-perfil` e `PUT /alterar-senha` para os tipos novos. |
| `cmd/server/main.go` | rotas novas (linha 640) | Rotas de secretários, professores, encarregados, `/professor/*`, `/encarregado/*`, `DELETE /estudante/encarregado`; registra a projeção `contas`. |
| `cmd/server/permissoes_rotas_test.go` (**novo**) | — | **Varredura do router:** toda rota registrada precisa de decisão para o secretário; regras órfãs e permissões fora do catálogo falham. |
| `cmd/server/contas_integration_test.go` (**novo**) | — | Integração (router, PostgreSQL e ledger reais): secretário trabalha só com o que lhe foi concedido, catálogo, auditoria do ator, desativação, isolamento entre academias, professor e vínculos. |
| `cmd/server/encarregado_integration_test.go` (**novo**) | Encarregado03 (linha 167) | Integração: encarregado age só no educando vinculado e só no nível escolar; desvinculação pelo estudante; troca pela academia. (Os demais testes deste arquivo são da Tarefa 131.) |
| `Documentação da API.md` | seção 3.6 (linha 637) | Seções 3.6 (perfis, identidade efetiva, erros, matriz), 26 (secretários), 27 (professores), 28 (encarregados), versionamento (1.2) e atores. **Versão 4.0.0** (junto com a Tarefa 131). |
| `docs/Plano_de_Atualizacoes.md` | Parte 4 (linha 202) | Parte 4 ✅ e itens marcados; permissões do secretário (nota 1); **passo 9 novo** em "Como acompanhar" (versão da API em SemVer); interpretações 29 a 37; registro de andamento. |

**API** (contrato completo nas seções 3.6, 26, 27 e 28 da Documentação da API): `POST/GET/PUT/DELETE /academia/secretarios[/:id]`, `GET /academia/permissoes`, `POST/GET/PUT/DELETE /academia/professores[/:id]`, `POST|DELETE /academia/professores/:id/vinculos`, `PUT /academia/vinculos-professor/:vinculo_id/reatribuir`, `GET /professor/{vinculos,turmas,turmas/:codigo/{estudantes,notas,faltas}}`, `GET /encarregado/educandos`, `PUT /encarregado/educandos/:codigo/relacao`, `GET /academia/encarregados/buscar`, `PUT /academia/estudante/:codigo/encarregado`, `DELETE /estudante/encarregado`; `POST /login` aceita os três tipos novos. Erros da porta de identidade (`403` com `code`): `SECRETARIO_SEM_PERMISSAO`, `ACAO_RESERVADA_A_ACADEMIA`, `ACAO_NAO_PERMITIDA_AO_SECRETARIO`, `ROTA_FORA_DO_PERFIL`, `ACADEMIA_INATIVA`, `EDUCANDO_OBRIGATORIO`, `EDUCANDO_NAO_VINCULADO`, `ENCARREGADO_SOMENTE_LEITURA`.

## Validação realizada

**Método.** Clone novo de `main` @ `ffc13e2`, a pasta `rastreio-backend/` do zip copiada por cima e só então as verificações abaixo. Go 1.24.13 e PostgreSQL 16.16; o banco foi **recriado do zero antes de cada execução** (os testes de integridade não aceitam banco reaproveitado, ver "Falha antiga"). A mesma sequência foi corrida antes no `main` original, para comparar.

| Verificação | `main` original | Com o pacote |
| --- | --- | --- |
| `gofmt -l cmd internal` | vazio | vazio |
| `go build ./...` · `go vet ./...` | limpos | limpos |
| `go test ./...` (modo do CI, `RUN_POSTGRES_INTEGRATION=1`) | 27 pacotes `ok` | **28 pacotes `ok`** (o novo é `internal/permissoes`) |
| `SPURI_RUN_DB_INTEGRITY_TESTS=1 go test ./cmd/server -p 1` (banco novo) | `ok` (16 s) | **`ok` (46 s)** |

**Testes novos (33 funções, mais subtestes):** `internal/permissoes` (6), agregados de conta (8), regra única de contato (3), varredura do router (2), contas — secretário, catálogo, auditoria, desativação, isolamento, professor (6) e encarregado e reset por perfil (8). **Quatro arquivos de teste existentes foram ajustados** ao novo contrato: o helper de cadastro (agora envia o encarregado), o teste de telefone inválido do encarregado, o helper do reenvio e o teste do destino das credenciais. O teste estrutural `TestSegurancaPontosDeCriacaoUsamSenhaTemporaria` foi atualizado e passou a cobrir os pontos de criação novos; `TestPacotesDeModuloNaoSeImportam` obrigou a mover a lógica compartilhada para `apoio` (ficou como estava).

**Regras críticas: desfeita a regra, um teste cai** (cada linha foi desfeita numa cópia, o teste corrido e a regra restaurada; sem desfazer nada, tudo passa):

| Regra desfeita | Teste que falhou |
| --- | --- |
| Rota não classificada deixa de ser negada ao secretário | `TestContas01…` (esperava `ROTA_FORA_DO_PERFIL`) |
| Permissão exigida do secretário deixa de ser conferida | `TestContas01…` |
| Ações reservadas à academia liberadas ao secretário | `TestContas01…` |
| Lançar notas e faltas liberado ao secretário | `TestContas01…` |
| Auditoria deixa de gravar o ator | `TestContas03AuditoriaGravaOSecretarioComoAtor…` |
| Admin reseta credenciais de estudante que tem academia | `TestCredenciaisReset02AdminAdmOuFpp…` |
| Nível escolar cai no telefone do estudante | `TestResolverContatoPorNivel/escolar_SEM_encarregado…` |
| Fase de leitura do encarregado ignorada | `TestEncarregado03AgeEmNomeDoEducando…` |
| Professor vê turma sem vínculo | `TestContas06ProfessorTemVinculos…` |
| Dois professores no mesmo par turma+matéria | `TestContas06ProfessorTemVinculos…` |
| Estudante e encarregado gravados sem transação única | `TestEncarregado01CadastroEscolar…` |
| Irmão cadastrado em sequência recusado (sem recuperar o encarregado pelo ledger) | `TestCredenciaisReset02…` |

**Não verificado:** pagamentos reais pelo encarregado (só a negação na fase de leitura está testada) e envio de SMS por um provedor real (os testes capturam a mensagem).


## Falha antiga que não é desta tarefa

Duas falhas já existem em `main` @ `ffc13e2` e **não** vêm desta tarefa; foram reproduzidas no `main` original, sem nenhum arquivo deste pacote:

1. **2ª execução no mesmo banco:** `TestIntegrationConsultarCobrancasEstudanteEstudanteVeTodosOsEstados` e `TestIntegrationConsultarCobrancasEstudanteFiltroEstadoFailedIncluiFalhadaLocal` (`internal/handlers`) acumulam cobranças do código fixo `ESTCOB1` entre execuções (esperam 2 e obtêm 4). Com banco novo passam.
2. **Testes de integridade depois da suíte do CI no mesmo banco:** `go test ./cmd/server` com `SPURI_RUN_DB_INTEGRITY_TESTS=1` falha em `rebuild turmas: … violates foreign key constraint "projection_turmas_codigo_academia_fkey"` se o banco já foi usado pela suíte completa (eventos de outros pacotes ficam no ledger). Com banco novo passa.

## Como aplicar

1. **Confirme que a base não mudou:** `git fetch origin && git diff --stat ffc13e2 origin/main -- "Documentação da API.md" "cmd/server/credenciais_reenvio_integration_test.go" "cmd/server/credenciais_temporarias_integration_test.go" "cmd/server/main.go" "cmd/server/turma_vinculo_estudante_integration_test.go" "internal/db/repository.go" "internal/db/safe_queries.go" "internal/domain/aggregates/aggregate.go" "internal/handlers/administradores/seguranca_codigo_test.go" "internal/handlers/apoio/autenticacao.go" "internal/handlers/apoio/comunicacao.go" "internal/handlers/apoio/estudantes.go" "internal/handlers/apoio/helpers.go" "internal/handlers/autenticacao/auth_email_handlers.go" "internal/handlers/autenticacao/auth_handlers.go" "internal/handlers/autenticacao/contact_handlers.go" "internal/handlers/autenticacao/credenciais_reenvio_handlers.go" "internal/handlers/autenticacao/credenciais_test.go" "internal/handlers/autenticacao/dois_fatores_handlers.go" "internal/handlers/autenticacao/profile_handlers.go" "internal/handlers/estudantes/estudante_handlers.go" "internal/handlers/estudantes/solicitacao_edicao_dado_estudante_handlers.go" "internal/middleware/auth.go" "internal/projections/dois_fatores.go" "internal/projections/manager.go"`. Vazio = pode substituir; se aparecer algum arquivo, ele mudou depois da base — não substitua e avise.
2. **Copie** a pasta `rastreio-backend/` para a raiz do repositório, mantendo os caminhos. O pacote traz as Tarefas 130 e 131 juntas.
3. **Verifique** (abaixo).
4. **Marque como feito** (abaixo).

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

`gofmt -l .` não imprime nada; `go build` e `go vet` sem saída; `go test ./...` com todos os pacotes `ok`. Os testes de integração só correm de verdade com PostgreSQL descartável:

```bash
RUN_POSTGRES_INTEGRATION=1 DATABASE_URL=postgres://… go test ./...
SPURI_RUN_DB_INTEGRITY_TESTS=1 DATABASE_URL=postgres://… go test ./cmd/server -p 1   # banco NOVO
```

O segundo comando (cadastro, credenciais, turmas, contas, encarregados) **não está no CI**; rode-o num banco recém-criado (ver "Falha antiga").

## Fora de escopo

- **Lançar e corrigir notas e faltas pelo professor** e retirar essa escrita da academia: Parte 11 (hoje a academia ainda lança; o secretário **não**).
- **Reenvio de credenciais para secretário, professor e encarregado**: fora do plano (só o do estudante foi pedido). Se o SMS de uma conta nova falhar, a academia elimina a conta e a recria; a resposta e o ecrã dizem isso.
- **Proteção por assinatura** dos recursos novos (Parte 9) e **notificações** (Parte 17).
- **Dados do período escolar na leitura do encarregado** limitam-se a notas e faltas (as únicas rotas liberadas nessa fase).
- **Interface:** Tarefa 40 do `rastreio-frontend`.

## Marcar como feito

1. `**Estado:** feito`, `(feito)` no título.
2. Secção **Resultado** no fim: um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mover para `docs/Tarefas feitas/`, mesmo nome.

## Checklist

- [ ] Base conferida
- [ ] Arquivos copiados (15 alterados, 18 novos); migrations 140 e 141 sem conflito de número
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test ./...` — todos `ok`
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`
