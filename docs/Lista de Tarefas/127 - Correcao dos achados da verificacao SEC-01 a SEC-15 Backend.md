# Tarefa 127 — Correção dos achados da verificação de segurança SEC-01 a SEC-15 (backend)

**Estado:** pendente
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `e397295`
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório (mais 2 arquivos a apagar à mão, listados abaixo)
**Ordem de deploy:** **depois** da Tarefa 37 do `rastreio-frontend`. Esta tarefa passa a encerrar as sessões antigas na primeira troca da senha temporária e a devolver um token novo na resposta de `PUT /alterar-senha`; só o frontend da Tarefa 37 guarda esse token e trata o `401 SESSAO_INVALIDA`. O frontend novo funciona com o backend antigo (sem o token novo mantém o atual), portanto o inverso é o único que quebra: backend primeiro deixaria o estudante recém-criado preso numa sessão encerrada depois de trocar a senha.

---

## Contexto

A verificação independente de 14/11/2026 (relatório "Relatório de verificação — Segurança SEC-01 a SEC-15") mostrou que o plano marcava os 15 pontos como resolvidos, mas só 5 estavam completos. Esta tarefa fecha todos os achados que são código, documentação ou teste do backend (R-01 a R-14 do relatório):

- **R-01 (alta):** `GET /estudantes` entrava em pânico (HTTP 500) para academias sem filtro de busca e devolvia lista vazia ou 500 com filtros. Causa: em `ListarEstudantes` o código da academia entrava como `$1` e juntava `args[1:]`, mas as condições de filtro já tinham sido numeradas sem ele; em `vinculo=anterior` a consulta ainda tinha dois `WHERE`. Reproduzido com sondas no router real, para academia e admin.
- **R-02 (alta):** SEC-15 incompleto: a academia atual lia os eventos de outras academias, a anterior continuava barrada (403) em notas, faltas e avaliações que ela própria registou, e o perfil reduzido revelava a academia atual e o estado escolar.
- **R-03 (alta):** o link de recuperação de senha antigo continuava válido depois de um reset (provado: dois pedidos, os dois links redefiniam a senha).
- **R-05:** a suíte estava vermelha em banco novo (5 testes) e `gofmt` apontava 5 arquivos.
- **R-06, R-07, R-08, R-09, R-11, R-12, R-13, R-14:** ver "O que foi atualizado e onde".
- **R-04 (parcial):** as 4 senhas padrão deixaram de estar escritas em qualquer arquivo da árvore. A reescrita do histórico git e a rotação das senhas **não são código e continuam com você** (secção "Ações que ficam com você").

## Decisões de design

1. **Academia só entra `ativa` (R-06).** A decisão D4 do plano ("qualquer estado") fica aplicada só ao estudante. Para a academia mantém-se o bloqueio no login e em todas as rotas, que é o mais seguro, e o plano, os comentários e a documentação foram corrigidos para dizer isso. Estender D4 às academias exige também gate por rota e tela de conta restrita no frontend; se quiser, é uma tarefa à parte.
2. **Falha fechada no middleware (R-08).** Erro de banco ao consultar o estado da conta passa a recusar o pedido com `503` (rotas protegidas) ou a seguir como anónimo (rotas públicas com token opcional), em vez de deixar passar. O custo é indisponibilidade durante instabilidade do banco (por exemplo, arranque a frio do Neon).
3. **Troca da senha temporária encerra sessões antigas (R-07).** Novo motivo `troca_senha_temporaria`: desliga `deve_alterar_senha` e atualiza `sessoes_validas_desde`. A troca normal (`alteracao_usuario`) continua a não tocar nas sessões. A resposta da troca temporária traz um `token` novo. O intervalo mínimo de 10 minutos do reenvio de credenciais passa a valer só enquanto a senha temporária continua por trocar, para a troca do próprio titular não travar um reenvio legítimo.
4. **Rede de segurança para a projeção de vínculos (R-11).** Se a projeção ainda não tem períodos de um estudante (logo após o deploy, enquanto o `Manager` faz o catch-up a partir do checkpoint 0), a academia registada no estudante mantém o acesso atual, que é exatamente a regra anterior ao SEC-15. Nesse caso os eventos ficam vedados (falha fechada). Sem períodos conhecidos, nenhum evento é devolvido.
5. **Estudantes que já seguiram para outra academia na listagem (SEC-15).** Aparecem só sem filtros nem busca e só com nome, código e vínculo, para filtros e busca não servirem de sonda a dados da academia atual.
6. **Rate limit só no cadastro público de academias (R-12).** Rajada de 10 por IP e reposição de 1 a cada 3 minutos. Login e recuperação de senha ficam sem limite: muitos utilizadores partilham o mesmo IP (CGNAT) e um limite por IP bloquearia utilizadores legítimos. O limite certo para eles seria por conta.
7. **Sem backfill SQL de vínculos.** O `Manager` já reconstrói a projeção a partir do ledger no arranque; o que se evitou foi o rebuild manual e a dependência de ordem (migration 137).

## O que foi atualizado e onde

**30 arquivos alterados + 9 novos + este documento. 2 arquivos a apagar.** Números de linha referem-se aos arquivos entregues.

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `internal/handlers/estudante_handlers.go` | `ListarEstudantes` (391), `condicoesVinculoListagem` (659), `reduzirLinhasDeOutrasAcademias` (695), `anexarVinculoListagem` (726) | **R-01.** Reescrito o bloco de academia/admin: o array de academias entra depois dos filtros do utilizador com um contador único de `$n`; filtro de vínculo por `EXISTS` (sem `JOIN` nem segundo `WHERE`); `vinculo` por linha passa a ser a relação real (`atual`/`anterior`); linhas de estudantes que seguiram para outra academia vêm reduzidas (`acesso: "reduzido"`); `vinculo` inválido dá erro de validação; admin com `vinculo=anterior` já não exige `codigo_academia`. |
| `internal/handlers/estudante_handlers.go` | `GetEventosEstudante` (867), `GetEventoAuditoria` (930) | **R-02.** O filtro por período vale também para a academia atual. |
| `internal/handlers/estudante_handlers.go` | `EstudantePerfilReduzido` (1055) e montagem do perfil | **R-02.** O perfil reduzido passa a ter só `id`, `nome`, `codigo_estudante`, `status: "desvinculado"` e `periodos_vinculo`; sai `codigo_academia`, estado escolar. |
| `internal/handlers/helpers.go` | `acessoAcademiaAoEstudante` (310), `filtrarEventosPorPeriodos` (349), `margemInicioVinculo` (364) | **R-02/R-11.** Rede de segurança sem períodos (acesso atual pela academia registada), falha fechada nos eventos sem períodos e margem de 2 s no início do vínculo. |
| `internal/handlers/notas_handlers.go` · `faltas_handlers.go` · `avaliacao_final_handler.go` | `GetNotasEstudante` (359) · `GetFaltasEstudante` (334) · `GetAvaliacoesFinaisEstudante` (1044) | **R-02.** Removida a verificação antiga `CodigoAcademia != academia → 403` que barrava a academia anterior antes do helper novo; decide só `acessoAcademiaAoEstudante`. |
| `internal/services/email_service.go` | `SaveToken` (61), `InvalidarTokensPendentes` (100) | **R-03.** `SaveToken` invalida, na mesma transação, os tokens pendentes do mesmo utilizador/tipo; novo método que invalida os pendentes. |
| `internal/handlers/auth_email_handlers.go` | `ResetarSenha` (403) | **R-03.** Depois de um reset concluído invalida os restantes tokens de recuperação do utilizador. |
| `internal/domain/aggregates/senha_temporaria.go` | `MotivoSenhaTrocaTemporaria` (30) | **R-07.** Novo motivo de troca de senha. |
| `internal/projections/senha_temporaria.go` | tratamento do motivo (52) | **R-07.** O motivo novo desliga a flag e atualiza `sessoes_validas_desde`, como o reset. |
| `internal/handlers/profile_handlers.go` | `AlterarSenha` (18), `contaComSenhaTemporaria` (407), `responderSenhaAlterada` (429) | **R-07.** Escolhe o motivo conforme a conta ainda tenha senha temporária e devolve `token` novo nesse caso. |
| `internal/handlers/credenciais_reenvio_handlers.go` | `verificarIntervaloReenvio` (48) e as 3 consultas | **R-07.** O intervalo de 10 min só vale enquanto `deve_alterar_senha` continua ligada. |
| `internal/middleware/auth.go` | `CodigoSessaoInvalida` (105), `responderSessaoInvalida` (114), `RotaPermitidaEstudanteInativo` (155), `AuthMiddleware` (210), `OptionalAuthMiddleware` (306), `consultarEstadoUsuario` (392) | **R-08/R-10/R-13.** Falha fechada em erro de banco (503 / anónimo); todo 401 de autenticação traz `code: "SESSAO_INVALIDA"`; removidos `statusInativo` e `rotaFinanceiraRestrita`; comentário de D4 corrigido (academia só ativa); wrapper exportado para a varredura de rotas. |
| `internal/handlers/estudante_handlers.go` | `redigirEventoSensivel` (1467) e `redigirCamposSensiveis` | **SEC-14.** Redação genérica: qualquer campo `*hash` ou senha em claro, em qualquer nível, em qualquer tipo de evento; payload ilegível é substituído por inteiro (falha fechada). |
| `internal/handlers/bootstrap_handler.go` | `BootstrapAdminFPP` (37) | **R-09.** Token comparado em tempo constante; header `X-Bootstrap-Token` (o `BOOTSTRAP_TOKEN` antigo continua aceito); `401` se ausente/errado e `403` se não configurado; senha validada por `ValidateSenha` antes de tocar no banco. |
| `internal/handlers/academia_handlers.go` | goroutine de notificação (539) | **R-12.** `recover` na goroutine; corrigido o índice do nome do admin. |
| `cmd/server/main.go` | `POST /academia/cadastro` (318) | **R-12.** Limite de taxa por IP. |
| `internal/projections/estudante_vinculos_projection.go` | `handleEstudanteDeletado` (316) | **R-11.** A deleção do estudante fecha o período de vínculo aberto. |
| `migrations/137_estudante_vinculos_sem_fk.sql` (**novo**) | — | **R-11.** Remove a chave estrangeira de `projection_estudante_vinculos` para `projection_estudantes` (ordem entre projeções e rebuild de estudantes deixavam de esvaziar/atrasar os vínculos). |
| `.env.example` | secções finais | **R-13/R-09.** Removidas `ENABLE_EMAIL_VERIFICATION` e `ENABLE_PASSWORD_RECOVERY` (nenhum código as lê); documentadas `BOOTSTRAP_TOKEN` e `BOOTSTRAP_TOKEN_DEV`. |
| `go.mod` · `go.sum` | — | **R-13.** Removidos `gopkg.in/gomail.v2` e a sua dependência `quotedprintable.v3` (sem nenhum import). |
| `.gitignore` | fim | **R-13.** `/data/` (pasta criada pelos testes). |
| `Documentação da API.md` | autenticação, `PUT /alterar-senha`, `POST /bootstrap`, `POST /academia/cadastro`, `GET /estudantes`, `GET /consultar-estudante`, eventos | **R-13.** Documenta `vinculo`, `acesso`, `tipo_acesso`/perfil reduzido, filtro de eventos, `SESSAO_INVALIDA`, `CONTA_DESVINCULADA`, `503`, token na troca temporária, bootstrap; senha 8–128 (dizia 6–128); `429` no cadastro público. |
| `docs/Debbugs/seguranca-e-migracao-brevo.md` | cabeçalho e tabela-resumo SEC-01 a SEC-15, D4, SEC-05, SEC-08 a SEC-12, SEC-14, SEC-15, Parte 5 e Parte 7, comandos de verificação | **R-04/R-14.** Estado de cada SEC atualizado com o resultado verificado (nenhum ponto fica como "resolvido" sem o que o código faz); D4 só para o estudante; sem senhas literais (comandos de verificação e do `filter-repo` com variável/marcadores); corrigidas as afirmações que o código não cumpria (coluna `token` e não `token_hash`, `ValidarStatusAcademia` em `academia_middleware.go`, funções que não existem, validação do bootstrap, invalidação de tokens); notas da Tarefa 127 em SEC-08, SEC-14, SEC-15 e Parte 7. |
| `docs/Tarefas feitas/123 …` · `124 …` · `126 …` | texto / fim | **R-04/R-05.** 123 sem a senha; 124 e 126 com secção **Errata (Tarefa 127)** (base desatualizada, testes não executados, afirmações de `gofmt`/`go test` que não se confirmaram). |
| `internal/handlers/dois_fatores_handlers.go` | — | Só `gofmt`. |
| `internal/handlers/academia_register_publica_test.go` · `auth_email_handlers_integration_test.go` · `dois_fatores_integration_test.go` · `cmd/server/credenciais_temporarias_integration_test.go` | testes existentes | **R-05.** Atualizados para o que mudou de propósito (limite de taxa na rota pública; token guardado em hash; estudante inativo recebe token normal; a troca da senha temporária devolve o token novo e a sessão antiga deixa de valer — este último dependia do segundo em que o teste corria). |
| 8 testes **novos** | `internal/handlers/redigir_evento_sensivel_test.go`, `cmd/server/sec15_isolamento_integration_test.go`, `cmd/server/sessoes_e_estados_integration_test.go`, `internal/handlers/estudantes_listagem_vinculo_integration_test.go`, `internal/handlers/tokens_recuperacao_integration_test.go`, `internal/handlers/bootstrap_seguranca_test.go`, `internal/projections/estudante_vinculos_projection_test.go`, `internal/services/brevo_mailer_bcc_test.go` | isolamento A→B (faltas, notas, eventos, perfil), troca temporária, código de sessão, falha fechada e **varredura das 270 rotas autenticadas** por estado de conta; listagem (academia e admin, todos os filtros); tokens; bootstrap; vínculos; envio com BCC; redação genérica de eventos. |

**Arquivos a apagar à mão (o zip não apaga):** `fix_doc.ps1` (script de edição do plano, com caminho local do Windows) e `internal/middleware/auth_financeiro_restrito_test.go` (testava `rotaFinanceiraRestrita`, removida com a sessão restrita).

**Contrato alterado:**
- `PUT /alterar-senha` → `200 {"message": "...", "token": "<jwt>"}` quando a conta ainda tinha senha temporária (`token` ausente na troca normal).
- `POST /bootstrap` → header `X-Bootstrap-Token` (ou `BOOTSTRAP_TOKEN`); `401` token ausente/errado, `403` não configurado ou já existe admin, `422` senha fora da regra.
- `GET /estudantes` → parâmetro `vinculo=atual|anterior|todos` (padrão `todos`); cada item traz `vinculo` real; envelope traz `vinculo`.
- Todo `401` de autenticação → `"code": "SESSAO_INVALIDA"`; `503` quando o estado da conta não pode ser consultado.
- `POST /academia/cadastro` → `429` ao exceder o limite por IP.

## Validação realizada

Ambiente: Go 1.24.13 e PostgreSQL 16 reais (as dependências do Go vieram do GitHub por um `go.mod` só do ambiente de validação, porque o proxy de módulos é inacessível; os arquivos do repositório não foram alterados por isso). Banco de teste descartável, recriado do zero antes de cada execução.

**Baseline de `main` (`e397295`), clone limpo, banco novo:** `go test ./...` → 1092 passam, **5 falham**, 2 saltam (`TestCredenciaisListasIndicamContasComSenhaTemporaria`, `TestIntegrationListarEstudantesBuscaLivre`, `TestFaltasPeriodo09FaltasEstudanteFiltraPorPeriodo`, `TestIntegrationSolicitarRecuperacaoSenhaNaoAlteraSenhaEEnviaLink`, `TestIntegrationDoisFatoresLoginEstudanteInativoSoRecebeTokenRestritoDepoisDoCodigo`); `gofmt -l .` listava 5 arquivos.

**Com a mudança (clone novo de `main` + pasta copiada por cima + 2 arquivos apagados), banco novo:**

```
gofmt -l .            → (vazio)
go build ./...        → ok
go vet ./...          → ok
go test -p 1 ./...    → 1124 PASS, 0 FAIL, 2 SKIP (os 2 SKIP já existiam)
```

Todos os pacotes `ok`: `cmd/server`, `internal/db`, `internal/domain/aggregates`, `internal/finance`, `internal/handlers`, `internal/middleware`, `internal/projections`, `internal/security`, `internal/services`, `internal/storage`, `internal/utils`.

**Regra desfeita → teste que falhou → restaurada:**

| Regra desfeita | Teste que falhou |
| --- | --- |
| `SaveToken` deixa de invalidar tokens anteriores | `TestIntegrationRecuperacaoNovoPedidoInvalidaOLinkAnterior` |
| `ResetarSenha` deixa de invalidar tokens pendentes | `TestIntegrationRecuperacaoResetInvalidaTokensPendentes` |
| consumo do token deixa de ser atómico | `TestIntegrationRecuperacaoTokenDeUsoUnicoSobConcorrencia` |
| troca temporária usa o motivo que não encerra sessões | `TestTrocaSenhaTemporariaEncerraSessoesAnterioresEDevolveNovoToken` |
| middleware volta a falhar aberto | `TestAuthMiddlewareFalhaFechadoQuandoOBancoFalha` |
| eventos da academia atual sem filtro por período | `TestSeguranca15IsolamentoEntreAcademiasAposTransferencia` |
| academia anterior volta a ser barrada em faltas | `TestSeguranca15IsolamentoEntreAcademiasAposTransferencia` |
| `vinculo=atual` deixa de filtrar | `TestIntegrationListarEstudantesVinculoEFiltros` |
| bootstrap sem `ValidateSenha` | `TestBootstrapSenhaFracaERecusadaComTokenCorreto` |
| redação de eventos volta à lista fixa de tipos | `TestRedigirEventoSensivelGenerica` |
| deleção do estudante deixa de fechar o vínculo | `TestVinculosCriacaoEDelecaoFechamOPeriodo` |

Uma 11.ª mutação (retirar a verificação `token == ""` do bootstrap) **não** faz falhar nenhum teste, e não deve: com o token configurado, a comparação em tempo constante já recusa o token vazio (mutante equivalente).

**Regressão fechada por varredura:** `TestVarreduraDeRotasPorEstadoDaConta` percorre as 270 rotas autenticadas do router real e exige 401/403 para: senha temporária pendente (exceto perfil, trocar senha e logout), estudante desvinculado (toda escrita fora da lista permitida), estudante apagado e academia inativa.

## Falha antiga que não é desta tarefa

A **2.ª execução da suíte no mesmo banco** continua a falhar em dezenas de testes (fixtures partilhados: `rebuild turmas: erro no evento … projection_turmas_codigo_academia_fkey`). É a falha antiga já documentada na Tarefa 123 e **repete-se nos 5 testes novos que usam o mesmo fixture** (`TestSeguranca15…`, `TestTrocaSenhaTemporaria…`, `TestCodigoSessaoInvalida…`, `TestAuthMiddlewareFalhaFechado…`, `TestVarreduraDeRotas…`). Medido: 39 falhas na 2.ª execução do baseline e 41 com esta tarefa (a diferença são os testes novos que usam o fixture partilhado). Rode sempre num banco novo.

Também antigos e fora de escopo: o *advisory lock* do bootstrap é de sessão sobre um *pool* de conexões (o desbloqueio pode cair noutra conexão) e o `go test` apaga tabelas com `TRUNCATE ... CASCADE` em alguns testes de projeção (exigem banco isolado).

## Como aplicar

1. **Confirme que a base não mudou:** `git diff --stat e397295 HEAD -- .` (de preferência com os arquivos listados). Vazio = pode substituir; se aparecer algum arquivo, ele mudou depois da base — não substitua e avise.
2. **Copie** a pasta `rastreio-backend/` para a raiz do repositório, mantendo os caminhos.
3. **Apague** `fix_doc.ps1` e `internal/middleware/auth_financeiro_restrito_test.go`.
4. **Verifique** (abaixo) num banco de teste novo.
5. **Marque como feito** (abaixo).

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
RUN_POSTGRES_INTEGRATION=1 SPURI_RUN_DB_INTEGRITY_TESTS=1 \
  DATABASE_URL='postgres://USUARIO:SENHA@localhost:5432/BANCO_NOVO?sslmode=disable' \
  JWT_SECRET='um-segredo-de-teste-com-mais-de-32-caracteres' ENV=test \
  go test -p 1 -count=1 ./...
```

Esperado: `gofmt` vazio, `build` e `vet` limpos, todos os pacotes `ok`. Sem PostgreSQL os testes de integração saltam. A migration 137 aplica-se sozinha na subida do servidor.

## Ações que ficam com você (não são código)

1. **Histórico git (SEC-05):** hoje o backend tem 12 commits e o frontend 3 que contêm as senhas padrão. Crie fora do repositório o arquivo de expressões com uma linha por senha (`<senha>==>REMOVIDO`), rode `git filter-repo --replace-text` num clone espelho, confira com `git log --all -S"<senha>"` (0 commits), faça o *force-push* e peça aos colaboradores para reclonar. A tarefa já deixou a árvore limpa, então a reescrita não vai mexer em documentos atuais.
2. **Rotação das senhas em produção:** `cmd/migrar-credenciais` continua disponível (modo simulação por defeito). A secção 3.9 do plano foi por você dispensada; o comando só é preciso se existir alguma conta com senha padrão.
3. **Variáveis de produção:** `BOOTSTRAP_TOKEN` só enquanto precisar criar o primeiro admin (remova depois); confirme `EMAIL_FROM`, `BREVO_API_KEY` e `ENV=production`.
4. Após o deploy, os links de recuperação/verificação enviados antes (tokens ainda em claro na base) deixam de funcionar; peça novos links.

## Fora de escopo

- **Rate limit** em login e recuperação de senha (decisão 6); limite por conta fica para outra tarefa.
- **D4 para academias** (decisão 1).
- **Limpeza agendada de tokens expirados** (`cleanup_expired_tokens()`): continua pendente (Parte 7).
- **Efeitos de deploy no frontend:** tratamento do `401 SESSAO_INVALIDA`, token novo na troca temporária, filtro de vínculo para a academia, aviso de exclusão de conta e aviso de conta desvinculada: **Tarefa 37 do `rastreio-frontend`**.
- Sincronizar a cópia da API no frontend (`src/docs/Documentação da API.md`): tarefa à parte.
- Comportamento atual mantido: academia e admin não leem notas/faltas/avaliações de estudante **apagado** (os handlers recusam `deletado`); mude só se quiser que a academia consulte o histórico de apagados.

## Marcar como feito

1. `**Estado:** feito`, `(feito)` no título.
2. Secção **Resultado** no fim: um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mover para `docs/Tarefas feitas/`, mesmo nome.

## Checklist

- [ ] Base conferida (`e397295`)
- [ ] Pasta copiada (30 alterados, 9 novos); `fix_doc.ps1` e `auth_financeiro_restrito_test.go` apagados; migration `137` sem conflito de número
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test ./...` num banco novo — todos `ok`
- [ ] Tarefa 37 do `rastreio-frontend` já em produção antes deste deploy
- [ ] Histórico git reescrito e senhas rodadas (ações suas)
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`
