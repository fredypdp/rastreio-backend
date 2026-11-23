# Tarefa 131 — Credenciais do cadastro escolar com encarregado e regra única de contato  (backend)

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `ffc13e2`
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório
**Ordem de deploy:** **Backend e frontend vão juntos.** As Tarefas 130 e 131 (`rastreio-backend`) entram no **mesmo pacote** (arquivos em comum) e devem ir para produção **na mesma janela** das Tarefas 40 e 41 (`rastreio-frontend`). A Tarefa 131 é uma mudança **incompatível** (API 4.0.0): sem o frontend novo, o cadastro de estudante do nível escolar com encarregado novo e a edição do telefone do encarregado do frontend atual deixam de funcionar. Ordem sugerida: **backend primeiro, frontend logo a seguir.** As migrations 140 e 141 rodam sozinhas na subida.
**Versão da API:** `3.0.1` → `4.0.0` (as Tarefas 130 e 131 juntas; ver o histórico na seção 1.2 da Documentação da API)

---

## Contexto

Plano de Atualizações, **Parte 5** (credenciais temporárias: ajustes do cadastro e do reset). Com a conta do encarregado (Tarefa 130):

- o admin era só `fpp` e resetava **qualquer** estudante; passa a ser `adm` ou `fpp` e **só estudante sem nenhuma academia**; o secretário entra com a mesma regra da academia;
- no cadastro do nível escolar, as credenciais do estudante **e do encarregado novo** vão só para o telefone do encarregado; encarregado já existente é vinculado sem novas credenciais; se o envio falhar, **nunca** cai no telefone do estudante;
- `TelefoneDestinoCredenciaisEstudante` tinha um caminho em que, **sem nível definido**, usava o telefone do estudante; para o nível escolar isso não pode acontecer;
- tudo vira uma **regra única de contato** (telefone **e e-mail**), reutilizável.

Descoberta ao implementar: existia uma restrição **única** em `projection_estudantes.telefone_encarregado`, ou seja, um telefone de encarregado só podia ter **um** estudante. Isso contradiz "um encarregado com vários educandos" (irmãos) e foi removido (migration 141).

## Decisões de design

- **Uma só função decide o contato.** `services.ResolverContato` (pura) serve ao cadastro, antes de a conta do estudante existir; `ResolverContatoDoEstudante` carrega do banco para o reenvio e para as partes futuras. Escolar → encarregado; superior → o próprio; sem nível → erro (fim do fallback). Exceção declarada: quem **concluiu o escolar e se desvinculou** do encarregado responde por si.
- **Identificação do encarregado por telefone ou e-mail** (ponto 7 do plano). O telefone é único entre encarregados. Telefone e e-mail de encarregados **diferentes** → `409`.
- **Irmãos em sequência (inclusive em lote).** A projeção é assíncrona: ao cadastrar o 2º irmão logo depois do 1º, o encarregado novo pode ainda não estar nela. O telefone é reservado com o guard de operação única e, se a reserva já foi consumida, o encarregado é recuperado **do ledger** em vez de recusar ou duplicar.
- **O retrato `telefone_encarregado` no estudante fica** (a tabela exige telefone do estudante **ou** do encarregado), mas deixa de ser fonte do contacto; `telefone_encarregado` continua aceito como alias de `encarregado.telefone`.
- **Remoção de `PUT /estudante/encarregado/telefone`:** o telefone é de uma pessoa com conta própria; quem o altera é ela (`/me/telefone`) ou a academia (troca de encarregado). É a mudança **incompatível** que leva a API a 4.0.0.
- **Cadastro em massa** aceita o bloco do encarregado (JSON aninhado ou campos planos).
- **Ordem dos SMS:** primeiro as credenciais do encarregado novo, por último as do estudante; as duas para o mesmo telefone.

## O que foi atualizado e onde

**15 arquivos alterados + 6 novos** (nesta tarefa; a outra tarefa do pacote lista os seus).

Depende da Tarefa 130 do pacote (contas, projeção e `PlanejarEncarregado`).

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `migrations/141_telefone_encarregado_compartilhado.sql` (**novo**) | — | Remove os índices **únicos** do telefone do encarregado no estudante (`idx_estudante_telefone_encarregado_unico`, `idx_telefone_resp_verificado_estudante`): um encarregado pode ter vários educandos. Cria índice comum para consulta e o índice único do telefone entre **encarregados** (é o identificador dele). |
| `internal/utils/telefone_nacional.go` (**novo**) | `NormalizarTelefoneNacional` (linha 14) | Normaliza o móvel angolano para `9XXXXXXXX` (aceita `+244`, espaços, hífens). Era privado de `apoio`; agora é compartilhado. |
| `internal/utils/nivel_escolar.go` (**novo**) | `FinalizouNivelEscolar` (linha 16) | Regras de nível: concluiu o escolar (`status_escolar_medio = finalizado` ou matriculado no superior) e tem nível escolar. |
| `internal/services/contato_estudante.go` (**novo**) | `ResolverContato` (linha 85) | **Regra única de contato** (função pura): escolar → encarregado; superior → o estudante; **nunca** cai no telefone do estudante no nível escolar; sem nível = erro. |
| `internal/services/contato_estudante.go` (**novo**) | `ResolverContatoDoEstudante` (linha 128) | Versão sobre o banco: carrega o estudante e o encarregado ativo (telefone **e e-mail**). Usada pelo reenvio de credenciais e pelas partes futuras. |
| `internal/services/contato_estudante_test.go` (**novo**) | — | Testes por nível: fundamental, médio, superior, escolar sem encarregado, médio concluído e desvinculado, sem nível, e-mail acompanha o telefone, telefone inválido do encarregado. |
| `internal/handlers/apoio/estudantes.go` | `BlocoEncarregado` (linha 25) | Bloco do encarregado nos pedidos de cadastro (JSON aninhado ou campos planos `encarregado_*`); `telefone_encarregado` continua como alias do telefone do bloco. |
| `internal/handlers/apoio/autenticacao.go` | — | Remove `TelefoneDestinoCredenciaisEstudante` (o caminho que, sem nível, usava o telefone do estudante). Mensagem de campo recusado aponta para `/me/telefone` e para a troca pela academia. |
| `internal/handlers/apoio/comunicacao.go` | `normalizarDestinatarioComunicacao` (linha 152) | Passa a usar `utils.NormalizarTelefoneNacional` (mesmo comportamento e mensagens). |
| `internal/handlers/estudantes/estudante_handlers.go` | `encarregadoDoFormulario` (linha 82) | Lê o bloco do encarregado do multipart (`encarregado_*`). |
| `internal/handlers/estudantes/estudante_handlers.go` | nível escolar (linha 176) | No nível escolar: planeja o encarregado (reutiliza ou cria), resolve o telefone pela regra única, grava **estudante e encarregado na mesma transação**, envia as credenciais do encarregado novo e depois as do estudante **só ao telefone do encarregado**; a resposta traz `data.encarregado`. |
| `internal/handlers/estudantes/solicitacao_edicao_dado_estudante_handlers.go` | — | Remove `AtualizarTelefoneEncarregado` (`PUT /estudante/encarregado/telefone`). |
| `internal/handlers/autenticacao/credenciais_reenvio_handlers.go` | `estudanteSemNenhumaAcademia` (linha 355) | Reenvio: academia e secretário (com `estudantes.credenciais`) nos seus; **admin `adm` ou `fpp` só para estudante sem nenhuma academia**; destino pela regra única de contato (sem SMS e sem troca de senha se não houver contato válido). |
| `internal/handlers/autenticacao/credenciais_test.go` | — | Teste do destino das credenciais reescrito sobre a regra única (inclui o caso "sem nível", que agora é erro). |
| `internal/handlers/administradores/seguranca_codigo_test.go` | — | Teste estrutural dos pontos de criação de conta: estudante exige `ResolverContato`; os pontos novos (secretário, professor, encarregado) também exigem senha temporária. |
| `internal/middleware/auth.go` | rotaPermitidaEstudanteInativo (linha 160) | Retira `PUT /estudante/encarregado/telefone` da lista de escritas permitidas ao estudante inativo (a rota não existe mais). |
| `cmd/server/main.go` | remoção da rota (linha 556) | Remove a rota `PUT /estudante/encarregado/telefone`. |
| `cmd/server/encarregado_integration_test.go` (**novo**) | Encarregado01 (linha 83) | Integração por perfil e por nível: cadastro escolar (conta nova, credenciais só ao encarregado, irmão reaproveita o encarregado, dados em falta não criam nada), reset por secretário, por admin `adm`/`fpp`/`gerente` e escolar sem encarregado ativo. (Os testes de acesso do encarregado são da Tarefa 130.) |
| `cmd/server/turma_vinculo_estudante_integration_test.go` | `camposCadastro` (linha 156) | Helper de cadastro dos testes passa a enviar o bloco do encarregado (o nível escolar agora o exige). |
| `cmd/server/credenciais_temporarias_integration_test.go` | — | Teste de telefone inválido do encarregado ajustado ao novo campo. |
| `cmd/server/credenciais_reenvio_integration_test.go` | — | Helper de cadastro projeta também as contas (o gerenciador faz isso em produção). |
| `Documentação da API.md` | versionamento (linha 106) | Versionamento (seção 1.2) e histórico: **4.0.0**, com a **3.1.0 da Tarefa 129 corrigida**; reenvio de credenciais; bloco do encarregado no cadastro; remoção da rota do telefone do encarregado. |
| `docs/Plano_de_Atualizacoes.md` | Parte 5 (linha 263) | Parte 5 ✅ e itens marcados; tabela da Visão geral; registro de andamento. |

**API** (ver seções 3.6, 8, 4 e 28 da Documentação da API): `POST /academia/estudante/register` (individual, lote e assíncrono) aceita o bloco `encarregado` (JSON) ou os campos `encarregado_*` (multipart) e devolve `data.encarregado: { id, codigo, conta_nova, credenciais? }`; `POST /estudantes/:codigo/credenciais/reenviar` com as regras por perfil; **removida** `PUT /estudante/encarregado/telefone`.

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

- **Interface do cadastro, da troca de encarregado e do reenvio por perfil:** Tarefa 41 do `rastreio-frontend`.
- **Reenvio de credenciais para o encarregado novo** quando o SMS falha (não existe, como para secretário e professor).
- **Uso da regra única de contato** pelas notificações e pelos SMS: Partes 17, 22 e 23 (a função já existe e devolve também o e-mail).
- **Migração de dados** de encarregados antigos: não há academias cadastradas, então não há o que migrar (o BI do encarregado continua nos documentos de matrícula do estudante).

## Marcar como feito

1. `**Estado:** feito`, `(feito)` no título.
2. Secção **Resultado** no fim: um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mover para `docs/Tarefas feitas/`, mesmo nome.

## Checklist

- [ ] Base conferida
- [ ] Arquivos copiados (15 alterados, 6 novos); migrations 140 e 141 sem conflito de número
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test ./...` — todos `ok`
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`
