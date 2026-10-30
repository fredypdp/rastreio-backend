# Tarefa 122 — Verificação em duas etapas (2FA) por e-mail (backend)

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `2084f87` (`2084f87dec9e09a284732b5afece9ab8834b94c1`, tarefa 121 já aplicada)
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório
**Ordem de deploy:** **backend (esta tarefa) primeiro, depois a Tarefa 32 do `rastreio-frontend`.** Esta tarefa só acrescenta rotas, uma tabela e colunas; o frontend atual continua funcionando para quem não usa 2FA. Atenção à janela entre os dois deploys: quem ativar o 2FA por chamada direta à API antes de a Tarefa 32 estar no ar não conseguirá concluir o login pela tela antiga, porque a resposta de `POST /login` passa a vir **sem token** (pede o código). Como a única tela para ativar o 2FA é a da Tarefa 32, na prática ninguém ativa antes dela.

---

## Contexto

Pedido: verificação de duas etapas, inicialmente só por e-mail, com código numérico de 6 dígitos.

- **Ativação:** o e-mail já deve estar verificado; o usuário solicita, recebe o código por e-mail e, ao digitar o correto, o 2FA fica ativo.
- **Desativação:** o 2FA já deve estar ativo; lógica semelhante à da ativação. **Decisão posterior do dono do projeto: a desativação também exige a senha atual.**
- **Funcionamento:** a cada login com credenciais corretas, um código é enviado ao e-mail do usuário.
- **Expiração:** impedir a reutilização de códigos já usados e/ou dar prazo de validade; códigos aleatórios iguais a um gerado antes têm de poder ser diferenciados, sem cair na invalidação do código antigo.
- Regra geral dada pelo dono do projeto: para tudo o que não foi instruído, escolher sempre a opção mais segura.

**Diagnóstico do código existente (o que moldou o desenho):**

- Existe um único `POST /login` (`auth_handlers.go`) que emite o JWT direto, na ordem admin → academia → estudante. Estudante inativo recebe o token restrito ao financeiro.
- A tabela `auth_tokens` não serve para o código de 6 dígitos: tem `token UNIQUE` (6 dígitos colidiriam) e um CHECK que só aceita `verificacao_email` e `recuperacao_senha`.
- `LoginRateLimit` está desativado em `main`, então não há limite HTTP de tentativas no login: o limite precisa estar no próprio mecanismo.
- `email_verificado` é um evento do ledger e as projeções são assíncronas; o estado do 2FA segue o mesmo padrão.
- `PUT /me/email` não pede senha e zera `email_verificado`: com 2FA ativo, quem tivesse uma sessão aberta poderia redirecionar os códigos para um endereço seu.

## Interpretações adotadas (onde o pedido era aberto)

1. **"Código semelhante a um gerado anteriormente"** → cada envio cria um **desafio** com `desafio_id` próprio e data de criação. O código vale pelo *estado do desafio* (usado, substituído, expirado, tentativas), nunca pelo valor. Dois códigos iguais em momentos diferentes são desafios diferentes e um não afeta o outro.
2. **2FA vale para os três tipos** (admin, academia, estudante), inclusive estudante inativo: o token restrito ao financeiro também só sai depois do código.
3. **Troca de e-mail com 2FA ativo é bloqueada** (escolha de segurança, não pedida): o e-mail é o canal do segundo fator. Para trocar, desativa-se o 2FA antes (exige senha + código no e-mail atual). A regra está nos agregados, então cobre qualquer caminho de edição de e-mail.
4. **Falha fechada:** erro ao ler o estado do 2FA, falha no envio ou ausência de e-mail verificado nunca resultam em token.
5. **Limites escolhidos:** validade do código 10 min; 5 tentativas erradas por desafio; 60 s entre envios; no máximo 5 envios *não concluídos* por hora por usuário e finalidade; reenvio no login só até 30 min depois da senha correta; 5 senhas erradas em 15 min bloqueiam a desativação (por usuário, em memória do processo).
6. **Canal:** Brevo (o mesmo do e-mail de verificação). Sem Brevo configurado o envio falha (503) — só em desenvolvimento, `DOIS_FATORES_LOG_CODIGO=true` registra o código no log, e é ignorado com `ENV=production`.
7. **Leitura do estado:** o estado do 2FA não foi acrescentado ao `GET /meu-perfil` (evita mexer nas consultas das três projeções); há uma rota própria `GET /dois-fatores`.

## Decisões de design

- **Event sourcing:** ativar/desativar são os eventos `DoisFatoresAtivados` e `DoisFatoresDesativados` (nomes compartilhados pelos três agregados, como já ocorre com `EmailVerificado`; as projeções filtram por `aggregate_type`). A coluna `dois_fatores_ativo` das projeções é consequência dos eventos e é reconstruída em qualquer rebuild. Há auditoria (`SaveWithAudit`).
- **Código nunca em texto puro:** guarda-se HMAC-SHA256 do conjunto `desafio_id | usuário | tipo | finalidade | código`, com chave derivada de `JWT_SECRET` por `security.DeriveKey` (rótulo próprio). Comparação em tempo constante.
- **Tabela própria `dois_fatores_desafios`** com índice único parcial: no máximo um desafio vivo por usuário e finalidade. Pedir outro código invalida o anterior na mesma transação (serializada por trava consultiva), mantendo as regras de intervalo e teto livres de corrida.
- **Verificação transacional** com `SELECT … FOR UPDATE`: sob concorrência só uma verificação consome o código; tentativas erradas são gravadas mesmo quando a verificação falha.
- **Ordem na desativação:** senha conferida *antes* de consumir o código (senha errada não queima o código); código consumido *antes* de gravar o evento (se a gravação falhar, o usuário só pede outro código).
- **Login em duas etapas:** `POST /login` devolve `requer_dois_fatores` + `desafio_id` sem token; `POST /login/dois-fatores/verificar` emite o JWT pelo **mesmo** ponto de emissão do login normal (`responderLoginComSucesso`), então a resposta é idêntica; o status da conta é reavaliado nessa segunda etapa.
- **Repetir o login com um código ainda vivo** (duplo clique, voltou à tela) reaproveita o desafio sem novo e-mail (`reutilizado: true`).
- **Sem alterar** `models.go` (os `LoginRequest`/`LoginResponse` de lá são duplicatas sem uso; os handlers usam os próprios tipos) nem `go.mod`/`go.sum`.

## O que foi atualizado e onde

**11 arquivos alterados + 10 novos** (mais este documento).

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `migrations/134_dois_fatores.sql` (**novo**) | — | Coluna `dois_fatores_ativo BOOLEAN NOT NULL DEFAULT FALSE` em `projection_admins`, `projection_academias` e `projection_estudantes`; tabela `dois_fatores_desafios` (id, usuário, tipo, finalidade, hash do código, e-mail de destino, tentativas, `iniciado_em`, `expira_em`, `usado_em`, `invalidado_em`, motivo, `created_at`) com CHECKs; índice único parcial de um desafio vivo por usuário+finalidade (`uq_dois_fatores_desafio_ativo`) e índice de consulta de envios. Idempotente (`IF NOT EXISTS`). |
| `internal/domain/aggregates/dois_fatores.go` (**novo**) | eventos (linhas 39 e 49), erros (linha 22), regras (linhas 59 e 70) | Eventos `DoisFatoresAtivados`/`DoisFatoresDesativados`; erros de domínio (já ativo, não ativo, e-mail não verificado, e-mail bloqueado por 2FA); regras de ativação e desativação compartilhadas pelos três agregados. |
| `internal/domain/aggregates/admin.go` | campo `DoisFatoresAtivo` (38); `Apply` (79); `AtivarDoisFatores` (163); `DesativarDoisFatores` (177); `AtualizarDados` (298) | Estado reconstruído pelos eventos; comandos de ativar/desativar; troca de e-mail recusada com 2FA ativo (reenviar o mesmo e-mail continua permitido). |
| `internal/domain/aggregates/academia.go` | campo (34); `Apply` (112); `AtivarDoisFatores` (229); `DesativarDoisFatores` (243); `AtualizarDados` (400) | Idem para Academia. |
| `internal/domain/aggregates/estudante.go` | campo (47); `Apply` (134); `AtivarDoisFatores` (593); `DesativarDoisFatores` (607); `AtualizarDadosPessoais` (714) | Idem para Estudante. |
| `internal/db/safe_queries.go` | whitelist de eventos (linhas 60–61) | Os dois tipos de evento novos entram na whitelist (o teste estático de `safe_queries` exige). |
| `internal/projections/dois_fatores.go` (**novo**) | `setDoisFatoresAtivo` (32) | UPDATE idempotente de `dois_fatores_ativo` (com `version` e `last_event_id`), com whitelist fechada das três tabelas. |
| `internal/projections/admin_projection.go` | mapa de handlers (65–66); `handleDoisFatoresAtivados/Desativados` (330, 334) | Projeção do estado do 2FA no admin. |
| `internal/projections/academia_projection.go` | mapas `Handle` (49–50) e `HandleTx` (84–85); handlers (729–741) | Idem para academia, nas duas variantes (normal e transacional). |
| `internal/projections/estudante_projection.go` | `Handle` (94–97) | Idem para estudante. |
| `internal/services/dois_fatores_service.go` (**novo**) | constantes (44); erros (69); `Criar` (198); `Verificar` (355); `ObterParaReenvio` (458) | Ciclo de vida dos desafios: geração do código (6 dígitos, `crypto/rand`), HMAC, intervalo mínimo, teto por hora, invalidação do anterior, envio fora da transação (remove o desafio se o e-mail falhar), verificação transacional com tentativas, expiração, uso único, amarração ao usuário e à finalidade. |
| `internal/services/email_dois_fatores.go` (**novo**) | `EnviarCodigoDoisFatores` (27); templates (80, 122) | Envio do código via Brevo com o invólucro de marca já existente (nome escapado); falha fechada sem Brevo; opção de log só em desenvolvimento. |
| `internal/handlers/dois_fatores_handlers.go` (**novo**) | `iniciarDoisFatoresNoLogin` (285); `VerificarLoginDoisFatores` (339); `ReenviarLoginDoisFatores` (385); `ObterDoisFatores` (447); `SolicitarAtivacaoDoisFatores` (475); `confirmarMudancaDoisFatores` (577); `SolicitarDesativacaoDoisFatores` (524); `exigirSenhaAtual` (258); `respondDoisFatoresError` (154) | Rotas e regras da API (contrato abaixo); leitor de identidade por id nas projeções; limite de senhas erradas na desativação; tradução de erros do serviço para HTTP. |
| `internal/handlers/auth_handlers.go` | `Login` (42, bloco 2FA na linha 205); `avaliarStatusLogin` (219); `responderLoginComSucesso` (238) | A regra de status e a emissão do token saíram do corpo do `Login` para funções compartilhadas com a segunda etapa (comportamento e mensagens do login normal inalterados); chamada do 2FA depois de senha e status corretos e **antes** de qualquer token. |
| `cmd/server/main.go` | rotas públicas (311–312); rotas autenticadas (417–421) | Registro das 7 rotas novas. |
| `Documentação da API.md` | `POST /login` (681); nova seção "Verificação em duas etapas" (776–969); `PUT /me/email` (1123) | Contrato completo das rotas novas, regras de negócio e limites; resposta e erros novos do login; regra de bloqueio da troca de e-mail. |
| `.env.example` | linhas 149–154 | Documenta `DOIS_FATORES_LOG_CODIGO` (somente desenvolvimento). |
| `internal/domain/aggregates/dois_fatores_test.go` (**novo**) | 8 testes | Regras nos três agregados, reconstrução por eventos, bloqueio de troca de e-mail. |
| `internal/services/dois_fatores_unit_test.go` (**novo**) | 8 testes | Formato do código, HMAC amarrado a desafio/usuário/tipo/finalidade, falha sem `JWT_SECRET`, e-mail (escape, falha fechada, opção de log não vale em produção). |
| `internal/services/dois_fatores_service_integration_test.go` (**novo**) | 17 testes (PostgreSQL) | Uso único, mesmo código em desafios diferentes, expiração, tentativas (duas camadas), substituição, intervalo, teto por hora, finalidades, amarração ao usuário, falha de envio, janela do login, concorrência. |
| `internal/handlers/dois_fatores_integration_test.go` (**novo**) | 16 testes (PostgreSQL) | Fluxos HTTP de ponta a ponta com eventos reais: admin completo, estudante inativo, academia, falhas fechadas, reenvio, limite de senha, projeções dos três tipos. |

### Contrato da API (resumo; detalhe em `Documentação da API.md`)

| Rota | Proteção | Resumo |
| --- | --- | --- |
| `POST /login` | pública | Sem 2FA: igual a antes. Com 2FA: `200 {requer_dois_fatores, desafio_id, metodo, email_mascarado, expira_em_segundos, reenvio_em_segundos, message}` **sem token**. Novos erros: `403` (2FA ativo sem e-mail verificado), `429`, `503`. |
| `POST /login/dois-fatores/verificar` | pública | `{desafio_id, codigo}` → mesma resposta do login com token. `401` para código/desafio inválido, expirado, usado, substituído, tentativas esgotadas ou conta inativa. |
| `POST /login/dois-fatores/reenviar` | pública | `{desafio_id}` → novo desafio; o anterior deixa de valer. `429` com `retry_after_segundos`. |
| `GET /dois-fatores` | autenticada | `{ativo, metodo, email_verificado, pode_ativar, email_mascarado}`. |
| `POST /dois-fatores/ativacao/solicitar` e `/confirmar` | autenticada | Exige e-mail verificado; `409` se já ativo. |
| `POST /dois-fatores/desativacao/solicitar` e `/confirmar` | autenticada | Exige **senha atual** (e o código na confirmação); `401` senha incorreta (o código não é consumido); `409` se não está ativo. |

Erros específicos: código incorreto devolve `tentativas_restantes`; `429` devolve `retry_after_segundos` (e cabeçalho `Retry-After`).

## Validação realizada

**Ambiente:** Go 1.24.13 e PostgreSQL 16.15 (pacotes `golang-1.24-go` e `postgresql-16` do Ubuntu 24.04), instalados de verdade no sandbox. Limitação de rede do sandbox: `proxy.golang.org`, `golang.org` e `gopkg.in` não são acessíveis; por isso os módulos `golang.org/x/*`, `google.golang.org/protobuf` e `gopkg.in/*` (nas mesmas versões do `go.mod`) foram obtidos de espelhos no GitHub por um `go.mod` alternativo usado só na validação (`-modfile`). O `go.mod` e o `go.sum` do repositório não foram tocados (`git status` limpo para eles).

**Baseline (`main` @ `2084f87`, clone novo, banco novo, migrations do zero):**

```
go vet ./...                       → sem saída (exit 0); gofmt -l . → vazio
go test ./... -count=1   (1ª exec) → ok em 11 pacotes, exit 0
go test ./... -count=1 -v (2ª exec, mesmo banco) → PASS 613 · FAIL 0 · SKIP 30
```

**Com a mudança (clone novo de `main` @ `2084f87` + a pasta `rastreio-backend/` copiada por cima, banco novo):**

```
git status --short                 → 11 arquivos alterados + 10 novos (+ data/, ver abaixo)
gofmt -l .                         → vazio
go build ./...                     → ok
go vet ./...                       → ok
go test ./... -count=1 -v (1ª exec, migrations do zero) → PASS 662 · FAIL 0 · SKIP 30
go test ./... -count=1 -v (2ª exec, mesmo banco)        → PASS 662 · FAIL 0 · SKIP 30

ok  spuri/cmd/server · ok spuri/internal/db · ok spuri/internal/domain/aggregates
ok  spuri/internal/finance · ok spuri/internal/handlers · ok spuri/internal/middleware
ok  spuri/internal/projections · ok spuri/internal/security · ok spuri/internal/services
ok  spuri/internal/storage · ok spuri/internal/utils
```

- 662 − 613 = **49 testes novos**, todos passando (8 + 8 + 17 + 16); os 30 SKIPs são exatamente os mesmos do baseline (testes que `main` já ignora; não são desta tarefa).
- Após as migrations: `schema_migrations` com 142 registros (141 + a `134_dois_fatores.sql`); a coluna `dois_fatores_ativo` existe nas 3 projeções; a tabela `dois_fatores_desafios` tem a chave primária, o índice único parcial e o índice de envios.
- Segunda execução no mesmo banco passa: os testes novos usam usuários e códigos aleatórios (nenhum depende de dados fixos nem de limpeza).

**Regra desfeita → teste que falhou → restaurada** (29 mutações; em todas a árvore foi restaurada e `gofmt` ficou limpo; todas foram detectadas):

| Regra desfeita | Teste(s) que falharam |
| --- | --- |
| Aceitar código já usado | `CicloFelizEUsoUnico`, `MesmoCodigoEmDesafiosDiferentes`, `VerificacaoConcorrenteSoUmSucesso` |
| Sem limite de tentativas (camada do contador) | `ContadorNoMaximoBloqueiaMesmoSemInvalidacao` |
| Esgotar tentativas não invalida o desafio (camada da invalidação) | `LimiteDeTentativas` |
| Tentativa errada não persistida | `LimiteDeTentativas` |
| Código substituído/invalidado continua válido | `MesmoCodigoEmDesafiosDiferentes`, `NovoCodigoInvalidaOAnterior` |
| Código expirado continua válido | `Expiracao` |
| Ignorar a finalidade do desafio | `FinalidadesIndependentes` |
| Não amarrar o desafio ao usuário | `DesafioAmarradoAoUsuario` |
| Sem teto de envios por hora | `TetoDeEnviosPorHoraNaoContaSucessos` |
| Sem intervalo mínimo entre envios | `IntervaloMinimoEntreEnvios` |
| Aceitar qualquer código (sem comparar o hash) | `CodigoDeOutroDesafioNaoServe`, `LimiteDeTentativas` |
| Manter desafio de código nunca enviado | `FalhaNoEnvioNaoDeixaDesafio` |
| Não validar o formato de 6 dígitos | `FormatoInvalidoNaoConsomeTentativa` |
| Reenvio sem janela de 30 min | `ReenvioPreservaJanelaDoLogin` |
| Login pula a segunda etapa (token direto) | `FluxoCompletoAdmin`, `LoginEstudanteInativoSoRecebeTokenRestritoDepoisDoCodigo`, `LoginAcademia` (+5) |
| Falha de envio ainda emite token | `FalhaDeEnvioNoLoginNaoEmiteToken` |
| 2FA ativo sem e-mail verificado passa direto | `LoginSemEmailVerificadoFalhaFechado` |
| 2ª etapa não reavalia o status da conta | `ContaDesativadaEntreAsEtapasNaoRecebeToken` |
| Desativar sem exigir a senha (confirmar) | `FluxoCompletoAdmin` |
| Desativar sem exigir a senha (solicitar) | `FluxoCompletoAdmin`, `DesativacaoLimitaTentativasDeSenha` |
| Confirmar sem amarrar o desafio ao usuário autenticado | `ConfirmacaoDeOutroUsuarioRecusada` |
| Ativar enviando código sem e-mail verificado | `AtivacaoExigeEmailVerificado` |
| Senha errada não conta para o limite | `DesativacaoLimitaTentativasDeSenha` |
| Admin: trocar e-mail com 2FA ativo | `TestAdminNaoAlteraEmailComDoisFatoresAtivo`, `FluxoCompletoAdmin` |
| Academia: trocar e-mail com 2FA ativo | `TestAcademiaNaoAlteraEmailComDoisFatoresAtivo` |
| Estudante: trocar e-mail com 2FA ativo | `TestEstudanteNaoAlteraEmailComDoisFatoresAtivo` |
| Ativar sem e-mail verificado (agregados) | `TestAdminDoisFatoresExigeEmailVerificado`, `TestAcademiaEEstudanteDoisFatoresExigemEmailVerificado` |
| Ativar/desativar duas vezes (agregados) | `TestDoisFatoresCicloAtivarDesativarNosTresAgregados` |
| Projeção grava "inativo" ao ativar | `FluxoCompletoAdmin`, `ConflitosDeEstado`, `DesativacaoLimitaTentativasDeSenha` (+1) |

**O que NÃO foi validado:**

- **Entrega real de e-mail:** não há chave do Brevo nem saída para a internet no sandbox. Validei o conteúdo/escape do e-mail e todo o fluxo com um remetente falso; o envio real pelo Brevo depende da configuração de produção (`BREVO_API_KEY` e `EMAIL_FROM`/`EMAIL_USER`).
- **Render/NeonDB reais:** testado em PostgreSQL 16 local, não no NeonDB.
- **Limite de senhas erradas** é em memória do processo (como o rate limiter existente): com várias instâncias, cada uma conta separado.

## Falha antiga que não é desta tarefa

- Os testes de `main` deixam uma pasta `data/` (`data/mega_storage/…`) não ignorada pelo `.gitignore` na raiz depois de rodar; ela aparece em `git status` como não rastreada, também no baseline. **Não a inclua no commit** (o texto de commit sugerido adiciona só os arquivos da tarefa).
- 30 testes são ignorados (SKIP) por `main`, idênticos antes e depois.

## Como aplicar

1. **Confirme que a base não mudou** (vazio = pode substituir; se aparecer algum arquivo, ele mudou depois da base — não substitua e avise):
   ```bash
   git diff --stat 2084f87 HEAD -- ".env.example" "Documentação da API.md" cmd/server/main.go internal/db/safe_queries.go internal/domain/aggregates/academia.go internal/domain/aggregates/admin.go internal/domain/aggregates/estudante.go internal/handlers/auth_handlers.go internal/projections/academia_projection.go internal/projections/admin_projection.go internal/projections/estudante_projection.go
   git ls-remote https://github.com/fredypdp/rastreio-backend.git refs/heads/main   # deve ser 2084f87dec9e…
   ls migrations | sort -V | tail -2                                                 # deve terminar em 133_…; 134 livre
   ```
2. **Copie** a pasta `rastreio-backend/` para a raiz do repositório, mantendo os caminhos. Nenhum arquivo é apagado, movido ou renomeado.
3. **Verifique** (abaixo).
4. **Configure em produção** (se ainda não estiver): `JWT_SECRET` (já obrigatório) e o Brevo (`BREVO_API_KEY` + `EMAIL_FROM`/`EMAIL_USER`). Sem Brevo, ativar o 2FA e entrar com 2FA devolvem `503`. A migration 134 é aplicada sozinha na subida do servidor.
5. **Marque como feito** (abaixo).

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

Esperado: `gofmt -l .` vazio; `build` e `vet` sem saída; `go test` com `ok` em todos os pacotes. Os testes `TestIntegration…` só correm de verdade com PostgreSQL de teste descartável: `RUN_POSTGRES_INTEGRATION=1 DATABASE_URL=postgres://… go test ./...` (sem isso são ignorados; os 8 testes de agregados e os 8 unitários de serviço rodam sempre).

## Fora de escopo

- Outros métodos de segundo fator (SMS, WhatsApp, aplicativo autenticador) e códigos de recuperação.
- **Recuperação de conta quando o usuário perde o acesso ao e-mail** com 2FA ativo (hoje só o suporte, direto no banco/ledger); desativação do 2FA de outro usuário por um admin.
- Tornar o 2FA obrigatório por perfil ou academia; "lembrar este dispositivo".
- Expor `dois_fatores_ativo` em `GET /meu-perfil` ou em listagens.
- Reativar o rate limit HTTP do login (continua desativado em `main`; o 2FA tem limites próprios).
- Rotina periódica global de limpeza da tabela de desafios (só há limpeza oportunista, por usuário, de desafios com mais de 7 dias).
- Limite de senhas erradas compartilhado entre instâncias.
- Tela de gerenciamento e etapa do código no login: **Tarefa 32 do `rastreio-frontend`**.
- Mover as tarefas 119–121 para `docs/Tarefas feitas/` (continuam em `Lista de Tarefas` como estão em `main`).

## Marcar como feito

1. `**Estado:** feito`, `(feito)` no título.
2. Secção **Resultado** no fim: um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mover para `docs/Tarefas feitas/`, mesmo nome.

## Checklist

- [ ] Base conferida
- [ ] Arquivos copiados (11 alterados, 10 novos); migration 134 sem conflito de número
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test ./...` — todos `ok`
- [ ] `JWT_SECRET` e Brevo configurados em produção
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`
