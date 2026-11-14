---
modificado: 2026-11-12 21:00
criado: 2026-11-04 0:12
---
# Spuri — Segurança, credenciais temporárias e e-mail 100 % Brevo

**Versão final · 31/10/2026**
**Repositórios (branch `main`):** `fredypdp/rastreio-backend` @ `4759474` · `fredypdp/rastreio-frontend` @ `77ef4cf`

---

## 0. Escopo, limites e o que mudou desde a versão 1

Esta versão foi refeita sobre o estado atual dos dois repositórios, que avançaram (tarefa 122, verificação em duas etapas por e-mail). Todos os números de linha referem-se a esses commits.

**Revisão do 2FA (tarefa 122).** Usa apenas Brevo e define `ErrEmailNaoConfigurado`, que este plano reutiliza. Li os handlers por inteiro (login, reenvio, ativação, desativação) e o serviço nos pontos de segurança: código de 6 dígitos com `crypto/rand`, HMAC-SHA256, `subtle.ConstantTimeCompare`, verificação com `FOR UPDATE`, máximo de 5 tentativas por desafio dentro da transação, reenvio a cada 60 s, 5 envios por hora e uso único. **Sem achados.** Dois pontos de integração com este plano: `avaliarStatusLogin` é chamado também no segundo passo do 2FA (6.3.1), e o limitador de senhas em memória do 2FA só é usado ao confirmar a senha nos fluxos do próprio 2FA (`dois_fatores_handlers.go:546` e `:608`), não no login. O 2FA também introduz dois padrões que este plano copia: a convenção `ENV=production` e a flag de desenvolvimento `DOIS_FATORES_LOG_CODIGO` (ignorada em produção).

**Limites.** As conclusões vêm da **leitura do código**: não executei o código nem os testes, não vejo a base de dados nem as variáveis de produção, e o frontend foi analisado só neste repositório (outros clientes da API, se existirem, não). O histórico git dos dois repositórios foi analisado (SEC-05).

### Cobertura da auditoria

| Área | O que foi verificado e como | Onde |
|---|---|---|
| Envio de e-mail (Brevo, SMTP, EmailJS) | Leitura integral de `services/email_*`, `brevo_mailer.go`, `smtp_mailer.go`, handlers e rotas; busca em todo o código | Partes 1 e 2 |
| 2FA (tarefa 122) | Handlers por inteiro; serviço nos pontos de segurança | Secção 0 |
| Criação de contas e senhas | Os 6 pontos de criação/reset, lidos em contexto | Parte 3 |
| Módulo de SMS | Handlers, clientes Ziett/GoSMS (timeout de 15 s), agregado e esquema da mensagem | 3.4 |
| Estados do estudante (geral e académicos) | Todas as atribuições a `Status`, `StatusEscolar*` e `StatusSuperior` no agregado; projeção; login; middleware; 2FA | 6.1, SEC-13 |
| Leituras do estudante | 28 handlers `GET`, um a um | 6.2.3, SEC-14 |
| Pagamentos do estudante | 5 handlers financeiros e o serviço `internal/finance` | 6.2.4 |
| Escritas do estudante | 23 rotas | 6.2.2 |
| Aprovação de pedidos de status | Handler e regras no agregado | 6.2.5 |
| Escopo por academia | Varredura automática de 92 rotas com parâmetro e verificação manual das 21 sinalizadas; as restantes 71 por padrão, **não linha a linha** | 6.2.1 |
| Isolamento dos dados entre academias | Os 6 handlers de leitura por estudante, o esquema (tabelas que guardam `codigo_academia`), os eventos de vínculo, o ledger e o mecanismo de reconstrução de projeções | 6.5.1, SEC-15 |
| Frontend | Busca em todo `src/` por estados, por e-mail e pelo token restrito; leitura dos ficheiros afetados | 2.2, 3.10, 6.3.5 |
| Histórico git | Varredura de segredos e das senhas padrão nos dois repositórios | SEC-05 |

---

## 1. Decisões adotadas nesta versão

| #   | Decisão                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | Onde está detalhada                                                                                             |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| D1  | **Eliminar** os três rate limiters desativados (`GlobalRateLimit`, `LoginRateLimit`, `EmailRateLimit`)                                                                                                                                                                                                                                                                                                                                                                                                                                | Parte 7                                                                                                         |
| D2  | **Parar de usar senhas padrão.** Toda conta criada por terceiros recebe uma senha temporária aleatória de **8 caracteres alfanuméricos** (8 é o mínimo permitido: `ValidateSenha` exige 8 a 128), com **troca obrigatória** no primeiro acesso                                                                                                                                                                                                                                                                                        | Parte 3                                                                                                         |
| D3  | **Entrega das credenciais:** estudante e academia → **SMS**, pela função padronizada de envio do módulo de comunicação, **só em `ENV=production`**. **Administrador → apenas e-mail (Brevo)**                                                                                                                                                                                                                                                                                                                                         | Parte 3                                                                                                         |
| D4  | **Estudante e academia podem fazer login em qualquer estado da conta** (exceto `deletado`, nos dois). Admin continua a exigir `ativo`. O estudante **consulta tudo o que o envolve e paga as suas contas em qualquer estado geral (`ativo`, `pendente_documentos`, `inativo`) e em qualquer estado académico** (`inativo`, `em_andamento`, `finalizado` em fundamental, médio e superior). O `inativo` (desvinculado) passa a ter token normal; as demais escritas dele ficam bloqueadas por padrão. O token restrito é eliminado     | Parte 6 (a verificação pedida **não confirma a premissa por completo**, nem para academias nem para estudantes) |
| D5  | **Recuperação de senha por link (opção B):** o utilizador define a própria senha via link Brevo; nenhuma senha viaja por e-mail                                                                                                                                                                                                                                                                                                                                                                                                       | Parte 4                                                                                                         |
| D6  | **Remover por completo** as senhas padrão de admin (árvore atual e histórico git)                                                                                                                                                                                                                                                                                                                                                                                                                                                     | Parte 5                                                                                                         |
| D7  | Eliminar tudo o que envia e-mail fora do Brevo (EmailJS, SMTP, `gerar-token/*`)                                                                                                                                                                                                                                                                                                                                                                                                                                                       | Parte 2                                                                                                         |
| D8  | **Acesso por vínculo:** toda academia que já teve o estudante consulta apenas o que **ela própria registou** enquanto ele esteve vinculado a ela, nunca os registos de outras academias; vale também para a academia atual. A academia anterior só lê                                                                                                                                                                                                                                                                                 | 6.5.1                                                                                                           |
| D9  | **Apagar a conta** continua permitido, com **aviso no frontend** (perda de acesso e dívidas que ficam)                                                                                                                                                                                                                                                                                                                                                                                                                                | 6.5.2                                                                                                           |
| D10 | **Serviços ao sair:** o catálogo mostra só o da academia atual (vazio sem vínculo); ao ser desvinculado ou apagado, o estudante é **desconectado de todos os serviços extras** dessa academia.<br><br>Nota: para os casos em que ele foi desvinculado de uma academia mas ainda não pagou um serviço e a academia não anulou essa cobrança, deve existir um mecanismo que permita que a cobrança continue sendo feita (e permitir que o pagamento seja feito corretamente) mesmo o estudante já não estando vinculado à essa academia | 6.5.3                                                                                                           |

---

# Parte 1 — Diagnóstico de segurança (resolvido)

> **Estado desta parte (14/11/2026):** SEC-01 a SEC-15 estão **resolvidos**. SEC-01 a SEC-07 pela Tarefa 123 do `rastreio-backend` e Tarefa 34 do `rastreio-frontend`. SEC-08 a SEC-15 pela Tarefa 124 do `rastreio-backend` (este repositório). O mecanismo `sessoes_validas_desde` de **SEC-10** foi entregue como dependência do reenvio de credenciais (reset por link e reenvio). **Partes 6 a 8 (implantação completa de D4, vínculos, serviços ao sair, etc.) continuam pendentes** — esta parte cobre apenas o diagnóstico e as correções de backend já aplicadas.
> O título "(resolvido)" foi aplicado a pedido; leia-o como "SEC-01 a SEC-15 resolvidos no backend".

| ID | Severidade | Problema | Decisão / plano |
|---|---|---|---|
| SEC-01 | **Crítica** | Tomada de conta sem autenticação via `gerar-token/recuperacao` | Eliminar rotas (Parte 2) — ✅ **resolvido** (Tarefa 123 / 34) |
| SEC-02 | Alta | Rate limiters são no-ops | **Eliminar o código morto** (D1, Parte 7), com risco residual declarado — ✅ **resolvido** (Tarefa 123 / 34) |
| SEC-03 | Alta | Reset de senha sem prova de posse do e-mail | Opção B (D5, Parte 4) — ✅ **resolvido** (Tarefa 123 / 34) |
| SEC-04 | Alta | Senha inicial = código do utilizador, sem troca obrigatória | Senhas temporárias (D2/D3, Parte 3) — ✅ **resolvido** (Tarefa 123 / 34) |
| SEC-05 | Média | Senhas padrão de admin em repositórios públicos (árvore **e histórico**) | Remover tudo (D6, Parte 5) — ✅ **resolvido** (Tarefa 123 / 34) |
| SEC-06 | Média | Enumeração de contas (recuperação **e login**) | Respostas genéricas — ✅ **resolvido** (Tarefa 123 / 34) |
| SEC-07 | Média | Segredos gravados nos logs | Falhar fechado, sem logs de segredos — ✅ **resolvido** (Tarefa 123 / 34) |
| SEC-08 | Média | Amplificação de e-mails pelo cadastro público de academias | Envio assíncrono e deduplicado — ✅ **resolvido** (Tarefa 124: `SendBrevoEmailWithBCC`, goroutine) |
| SEC-09 | Baixa | Ciclo de vida frágil dos tokens (`auth_tokens`) | Consumo atómico, hash SHA-256 — ✅ **resolvido** (Tarefa 124: `SaveToken`/`VerifyToken` atômicos) |
| SEC-10 | Média | Sessões JWT não são revogadas após reset de senha | `sessoes_validas_desde` — ✅ **resolvido** (Tarefa 123, coluna + middleware) |
| SEC-11 | Média | `POST /bootstrap` público, sem segredo de instalação nem validação de senha | `BOOTSTRAP_TOKEN` — ✅ **resolvido** (Tarefa 124: header obrigatório) |
| SEC-12 | **Alta** | Permitir login em qualquer estado deixa rotas sem bloqueio próprio | Reorganização rotas, regra por método, elimina token restrito — ✅ **resolvido** (Tarefa 124: `AuthMiddleware`, `ValidarStatusAcademia`, `rotaPermitidaEstudanteInativo`) |
| SEC-13 | **Alta** | Estudante com conta apagada (`deletado`) continua a conseguir entrar | Bloquear no login, middleware e `GetAuthByIdentificador` — ✅ **resolvido** (Tarefa 124: filtro `status <> 'deletado'`) |
| SEC-14 | Média | Hashes de senha expostos nos endpoints de eventos | Redigir o payload — ✅ **resolvido** (Tarefa 124: `redigirEventoSensivel` em `GetEventosEstudante`/`GetEventoAuditoria`) |
| SEC-15 | Média | A academia atual lê registos de outras academias; a academia anterior perde o acesso | Vínculos por período + filtro forçado — ✅ **resolvido** (Tarefa 124: `projection_estudante_vinculos`, `acessoAcademiaAoEstudante`, filtro forçado em 3 handlers) |

---

## SEC-01 — Tomada de conta sem autenticação (Crítica) — ✅ resolvido

**Onde:** `cmd/server/main.go:339` e `:344`; `internal/handlers/auth_email_handlers.go` (`GerarTokenRecuperacao`, linhas 247-327).

**O que acontece.** `POST /email/gerar-token/recuperacao` é pública (a API doc diz "Proteção: pública") e **devolve o token de recuperação no corpo da resposta**. Esse token é aceito por `POST /email/recuperar-senha/:token`, também pública, que define a nova senha. O e-mail do titular nunca participa.

**Cadeia de ataque (2 pedidos):**
1. `POST /email/gerar-token/recuperacao` com o `codigo_estudante`, o código da academia ou o e-mail de um admin → resposta com `token`.
2. `POST /email/recuperar-senha/<token>` com `nova_senha` → a conta passa a ser do atacante.

**Pré-condição:** conta com e-mail verificado e identificador conhecido. O código de estudante é o login e circula em documentos.

**Impacto:** tomada de qualquer conta elegível, incluindo admins `fpp`/`adm`.

**Estado no frontend:** nenhuma página chama estas rotas; só existem *wrappers* mortos em `src/lib/api/services.ts`. São superfície de ataque sem utilizador legítimo (neste frontend; outros clientes não foram verificados).

**Correção:** eliminar `GerarTokenVerificacao`, `GerarTokenRecuperacao` e as duas rotas (Parte 2). `ResetarSenha` fica, mas só acessível com tokens que chegam por e-mail (Parte 4).

---

## SEC-02 — Rate limiters desativados (Alta → decisão: eliminar) — ✅ resolvido

**Onde:** `internal/middleware/rate_limit.go`; `cmd/server/main.go:286, 308, 311, 312, 313, 333`.

**O que acontece.** `GlobalRateLimit`, `LoginRateLimit` e `EmailRateLimit` existem, são registados e **não limitam nada**. O `.env.example` (linhas 179-182) documenta `RATE_LIMIT_GLOBAL=100`, `RATE_LIMIT_LOGIN=5`, `RATE_LIMIT_EMAIL=2`, mas nenhum código Go lê essas variáveis. A API doc afirma "Rate limit ativo" em dois sítios (linhas 657 e 1106), o que é falso. O comentário do próprio código já reconhece que as rotas financeiras públicas usam instâncias dedicadas "em vez de depender do limitador global legado".

**Decisão (D1):** eliminar as três funções, os seus usos e a configuração associada. Lista exata na Parte 7.

**O que NÃO se elimina.** `RateLimiter`, `NewRateLimiter` e `RateLimitMiddleware` são funcionais e **já protegem 3 rotas públicas** de solicitação de matrícula.

**Risco residual (declarado, não mitigado):** remover código que não fazia nada **não altera o comportamento**. Continuam sem limite na aplicação: o login por força bruta, as rotas públicas de e-mail (`recuperar-senha/solicitar`, `verificar-email/:token`), o cadastro público de academias (SEC-08) e o `bootstrap`. O 2FA tem limites próprios e independentes (5 tentativas por código), mas só protege quem o ativou; o seu limitador de senhas em memória só é usado ao confirmar a senha nos fluxos do próprio 2FA (`dois_fatores_handlers.go:546` e `:608`), não no login. Com a opção B (Parte 4) e as respostas genéricas (SEC-06) o abuso das rotas de e-mail deixa de comprometer contas, mas ainda pode consumir a quota do Brevo. Se mudar de ideias, `RateLimitMiddleware` pode ser aplicado rota a rota sem escrever código novo.

---

## SEC-03 — Reset de senha sem prova de posse do e-mail (Alta) — ✅ resolvido

**Onde:** `auth_email_handlers.go` — `SolicitarRecuperacaoSenha` (403-583); `email_service.go` — `SendPasswordResetEmail` (361-385).

**O que acontece.** `POST /email/recuperar-senha/solicitar` é pública e **aplica imediatamente uma nova senha aleatória** e só depois a envia por e-mail. O titular não precisa de agir para a senha antiga deixar de funcionar.

**Impacto:** qualquer pessoa pode **bloquear a conta de outra** repetidamente, só com o identificador. A senha circula em texto claro por e-mail. Se o envio falhar, a senha já foi trocada e o titular fica sem acesso.

**Decisão (D5):** fluxo por link. O pedido só envia, por Brevo, um link de uso único; a senha só muda quando o titular abre o link e escolhe a nova. Plano na Parte 4.

---

## SEC-04 — Senha inicial igual ao código do utilizador (Alta) — ✅ resolvido

**Onde:** `email_service.go:560-571` (`GetDefaultPassword`), usado em `estudante_handlers.go:264`, `solicitacao_matricula_handlers.go:394` e `:591`, `academia_handlers.go:147`; `internal/utils/codigo_estudante.go`.

**O que acontece.**
- Estudantes e academias criados por terceiros recebem como senha o próprio código, que é também o login. **Quem conhece o código entra na conta.**
- Não existe nenhum indicador de troca obrigatória no backend. O frontend apenas diz, em texto, que o utilizador "deverá alterá-la" (`CadastroSingularForm.tsx:152`, `academias/cadastrar/PageContent.tsx:28`).
- O código de estudante (3 letras + 4 dígitos, ≈ 175,8 milhões de combinações) é gerado com `math/rand` e registado em log a cada geração, ou seja, regista também a senha inicial.

**Impacto:** contas acessíveis a qualquer pessoa que veja o código (colegas, documentos, logs) até o titular trocar a senha, o que pode nunca acontecer.

**Decisão (D2/D3):** senha temporária aleatória de 8 caracteres, entregue por SMS (estudante, academia) ou e-mail (admin), com troca obrigatória. Plano completo na Parte 3.

---

## SEC-05 — Senhas padrão de admin em repositórios públicos (Média) — ✅ resolvido

**Onde:** frontend `src/types/email-auth.ts` (`SenhasPadrao`: `spuriadm`, `spurigerente`, `spurifpp`, `spuri123`; helper `getSenhaPadrao`) e `src/docs/Tarefas feitas/1 - …md` (≈ linhas 251-285); backend `email_service.go:93` (comentário que repete os valores).

**O que acontece.** Os dois repositórios são públicos. O backend já não gera admins com estas senhas (fix E4-AA-04), mas o frontend ainda as publica e **todas estão no histórico git dos dois repositórios**: no backend, em pelo menos 8 commits (incluindo cópias da pasta do frontend); no frontend, nos commits `c93c2f4` e `b6c0ef6`. Nada no código usa `SenhasPadrao`/`getSenhaPadrao`.

**Impacto:** qualquer admin criado antes do fix E4-AA-04 que nunca trocou a senha tem uma conta acessível a qualquer pessoa. Apagar os ficheiros **não basta**: o histórico continua a expô-las.

**Decisão (D6):** remover tudo (árvore, histórico) e tratar as senhas como comprometidas. Plano na Parte 5.

**Varredura de segredos reais no histórico.** Procurei `xkeysib-` (Brevo), `zk_live_` (Ziett), chaves privadas PEM, `ghp_`, `AKIA` e ficheiros `.env` commitados, nos dois repositórios. Só encontrei placeholders de teste e texto de documentação. A varredura não deteta chaves sem formato reconhecível (EmailJS, senhas SMTP, `JWT_SECRET`); estas não podem ser excluídas por esse método.

---

## SEC-06 — Enumeração de contas (Média) — ✅ resolvido

**Onde:** `auth_handlers.go:179` e `:184` (**login**); `SolicitarRecuperacaoSenha` e `GerarTokenRecuperacao`.

**O que acontece.**
- O **login** devolve mensagens diferentes: `"usuário não encontrado"` (linha 179) e `"senha incorreta"` (linha 184), apesar de o comentário da função falar em resposta "deliberadamente genérica".
- A recuperação distingue `404` (inexistente), `403` (e-mail não verificado) e `200`. `GerarTokenRecuperacao` ainda devolve `email` e `nome` do titular.

**Impacto:** lista de contas válidas, com o estado de verificação, para ataques dirigidos.

**Correção:** o login responde sempre com a mesma mensagem (`"credenciais inválidas"`). `recuperar-senha/solicitar` responde sempre `200` com a mesma mensagem e faz o trabalho real (e o log do motivo interno) apenas no servidor, de preferência em *goroutine*, para o tempo de resposta não revelar a existência da conta. Ajustar os testes `…ExigeEmailVerificado` e `…IdentificadorInexistente` em `auth_email_handlers_integration_test.go` e a UX do frontend que hoje reage ao `403` (`isEmailNaoVerificadoError`).

---

## SEC-07 — Segredos gravados nos logs (Média) — ✅ resolvido

**Onde (`internal/services/email_service.go`):**

| Linha | Situação | O que é registado |
|---|---|---|
| 326 | `SendVerificationEmail` sem Brevo | Link completo (contém o token) |
| 371 | `SendPasswordResetEmail` sem Brevo | **Senha temporária em texto claro** |
| 381 | `SendPasswordResetEmail` quando o Brevo falha | **Senha temporária em texto claro** |
| 400 | `SendAdminWelcomeEmail` com EmailJS desativado | **Senha do admin em texto claro** |
| `utils/codigo_estudante.go:22` | Geração de código de estudante | Código (= senha inicial, SEC-04) |

**Agravante.** Nos casos "sem configuração" a função retorna `nil`; o chamador conclui que enviou. `RegisterAdmin` afirma que a senha "foi enviada por e-mail" quando só existe no log.

**Correção:** nunca registar segredos. Sem canal configurado, registar apenas *"credenciais NÃO enviadas: canal indisponível"* e **devolver erro** (reutilizar `ErrEmailNaoConfigurado`). **Única exceção**, copiada do padrão do 2FA: em desenvolvimento, com a flag explícita `CREDENCIAIS_LOG_DEV=true` e **nunca** com `ENV=production`, as credenciais podem ser escritas no log para testes locais.

---

## SEC-08 — Amplificação de e-mails pelo cadastro público de academias (Média) — ✅ resolvido

**Onde:** `POST /academia/cadastro` (pública) → `notificarAdminsSobreNovaAcademiaPendente` (`academia_handlers.go`, linha 458).

**O que acontece.** Cada cadastro disparava, **de forma síncrona e sequencial dentro do pedido**, um e-mail Brevo por administrador elegível, com timeout de 15 s por envio. Sem limite de taxa (SEC-02), um script pode repetir o cadastro indefinidamente.

**Impacto:** inundação das caixas dos admins; consumo da quota do Brevo (bloqueando verificações e recuperações legítimas); pedidos presos até *N admins × 15 s*.

**Correção:** envio **assíncrono em goroutine** fora do ciclo da requisição + **agrupamento com BCC** (um único e-mail via Brevo, primeiro admin como `to`, demais em BCC). Implementado em:
- `internal/handlers/academia_handlers.go:499-577` — `notificarAdminsSobreNovaAcademiaPendente` regravada para capturar dados, disparar goroutine e usar `services.SendBrevoEmailWithBCC`.
- `internal/services/brevo_mailer.go:118-190` — `LoadBrevoConfigForAsync` (lê config sem `gin.Context`) e `SendBrevoEmailWithBCC` (envio único com lista BCC via API do Brevo).
- `internal/services/email_service.go:43-60` — `LoadBrevoConfigForAsync` exposto no serviço.
O cadastro da academia retorna imediatamente; falhas de e-mail só são logadas (padrão não bloqueante do projeto).

---

## SEC-09 — Ciclo de vida dos tokens (Baixa) — ✅ resolvido

**Onde:** `internal/services/email_service.go` (`SaveToken` linhas 70-105, `VerifyToken` linhas 110-145, `hashToken` linhas 43-59).

1. **Consumo atómico.** `VerifyToken` agora executa um único `UPDATE auth_tokens SET usado = TRUE, used_at = NOW() WHERE token_hash = $1 AND tipo = $2 AND NOT usado AND expires_at > NOW() RETURNING user_id, user_type` — se nenhuma linha for afetada, o token é inválido/usado/expirado.
2. **Tokens em hash SHA-256.** Nova função `hashToken(token string) string` calcula `SHA-256(token)` (hex). `SaveToken` guarda o hash; `VerifyToken` faz hash do token recebido e compara. O token em texto claro nunca é persistido.
3. **Invalidação de tokens antigos.** `SaveToken` executa `UPDATE auth_tokens SET usado = TRUE WHERE user_id = $1 AND user_type = $2 AND tipo = $3 AND NOT usado` antes de inserir o novo — só um token pendente por utilizador/tipo.
4. **Limpeza agendada.** Pendente (Parte 7) — a migration já tem `cleanup_expired_tokens()`; será chamada por job cron.

---

## SEC-10 — Sessões JWT não são revogadas após reset de senha (Média) — ✅ resolvido

**Onde:** `internal/middleware/auth.go` (JWT *stateless*, expiração padrão de 24 h; `IssuedAt` gravado na linha 90); `internal/projections/estudante_projection.go`, `academia_projection.go`, `admin_projection.go` (coluna `sessoes_validas_desde`); `internal/handlers/auth_handlers.go` (`responderLoginComSucesso`, `finalizarVerificacaoStatusUsuario`).

**O que acontece.** Depois de uma recuperação de senha, os tokens emitidos antes continuavam válidos até expirarem. Quem já tinha entrado na conta mantinha acesso.

**Correção:** coluna `sessoes_validas_desde` (timestamp) nas três projeções (`projection_estudantes`, `projection_academias`, `projection_admins`). O `AuthMiddleware` (linhas 180-195) rejeita tokens com `IssuedAt` anterior a esse valor.
- Atualizada **apenas** por: reset por link (Parte 4, `FinalizarVerificacaoSenha`) e reenvio de credenciais (Parte 3, `ReenviarCredenciaisTemporarias`).
- **Não alterada** pela troca normal de senha do próprio utilizador (`AlterarSenha`), para não terminar a sessão atual.
- Entregue como dependência da Tarefa 123 (backend) + Tarefa 34 (frontend).

---

## SEC-11 — `POST /bootstrap` público e sem segredo de instalação (Média) — ✅ resolvido

**Onde:** `cmd/server/main.go:313` (rota registrada); `internal/handlers/bootstrap_handler.go:30-56` (`BootstrapHandler`).

**O que acontece.** A rota cria o primeiro admin `fpp` e só funciona enquanto não existir nenhum admin. Não exigia segredo de instalação: num deploy novo com base vazia, **o primeiro a chamar a rota vencia**. A senha enviada não passava por `ValidateSenha`.

**Correção:** exigir header `BOOTSTRAP_TOKEN` com o valor da variável de ambiente `BOOTSTRAP_TOKEN` (obrigatório em produção). Implementado em:
- `internal/handlers/bootstrap_handler.go:30-56` — valida header obrigatório, compara com `os.Getenv("BOOTSTRAP_TOKEN")`, retorna `401` se ausente ou inválido, `403` se não configurado no servidor. Valida senha com `ValidateSenha` (mínimo 8 chars, maiúscula, minúscula, número, símbolo). Retorna `409` se já existe admin.
- Variável `BOOTSTRAP_TOKEN` deve ser definida no ambiente de produção antes do primeiro deploy.

---

## SEC-12 — Permitir login em qualquer estado deixa rotas sem bloqueio próprio (Alta) — ✅ resolvido

**Onde:** `internal/middleware/auth.go` (`AuthMiddleware` linhas 20-448, `ValidarStatusAcademia` linhas 420-448, `rotaPermitidaEstudanteInativo` linhas 450-495); `cmd/server/main.go` (reestruturação de rotas linhas 357-472); `internal/handlers/auth_handlers.go` (`avaliarStatusLogin` linhas 219-277); handlers de defesa em profundidade em `servico_extra_solicitacao_handlers.go`, `notas_handlers.go`, `faltas_handlers.go`, `avaliacao_final_handler.go`.

**O que acontece.** A decisão D4 permitiu login em qualquer estado, mas as rotas não tinham bloqueio próprio:
- **Academias:** `ValidarStatusAcademia` só estava em 4 grupos `/academia/*`. As demais (financeiro, jobs, protected) ficavam abertas a academia `inativa`.
- **Estudantes `inativo`:** o token restrito (SEC-10) bloqueava tudo exceto pagamentos. Ao eliminá-lo, 4 rotas ficavam sem proteção (serviço extra, cancelamento, interrupção, desvinculação). `SolicitarServicoExtra` testava `CodigoAcademia == nil` que nunca é nulo pós-desvinculação.

**Correção:**
1. **Reestruturação de rotas** (`cmd/server/main.go:357-472`): dois grupos principais — `conta` (apenas autenticação, sem bloqueio de estado) e `protected` (exige `ValidarStatusAcademia` para academia, regra por método para estudante).
2. **Middleware** (`internal/middleware/auth.go`):
   - `rotaPermitidaEstudanteInativo`: allowlist por método — `GET /estudante/meu-perfil`, `PUT /estudante/alterar-senha`, `POST /estudante/logout`, rotas 2FA, rotas de pagamento, `GET /estudante/bilhete-identidade`, `POST /estudante/revinculacao`, `DELETE /estudante/conta`.
   - `AuthMiddleware` reescrito: aplica regra por método a estudante `inativo`; academia `inativa` só passa no grupo `conta`.
   - `finalizarVerificacaoStatusUsuario` atualizada: bloqueia `deletado` para **todos** os tipos (estudante, academia, admin).
   - Token restrito de finanças **eliminado** (era dependência do SEC-10).
3. **Login** (`auth_handlers.go:219-277`): `avaliarStatusLogin` bloqueia `deletado` para estudante/academia; permite `inativo`, `pendente_ativacao`, `pendente_validacao`. `responderLoginComSucesso` simplificada (um só `GenerateToken`).
4. **Defesa em profundidade nos handlers** (SEC-12/13): `GetNotasEstudante`, `GetFaltasEstudante`, `GetAvaliacoesFinaisEstudante`, `SolicitarServicoExtra` verificam `status != 'deletado'` (estudante) e `academia.Status == 'ativo'` antes de processar.

---

## SEC-13 — Estudante com conta apagada continua a conseguir entrar (Alta) — ✅ resolvido

**Onde:** `internal/projections/estudante_projection.go:1321-1343` (`GetAuthByIdentificador`); `internal/handlers/auth_handlers.go:219-277` (`avaliarStatusLogin`); `internal/middleware/auth.go:170-195` (verificação no `AuthMiddleware`); handlers de defesa em profundidade.

**O que acontece.** Apagar a conta de um estudante é *soft delete*: a linha fica em `projection_estudantes` com `status = 'deletado'` e o hash da senha intacto. O login procurava por código ou e-mail **sem filtrar `deletado`**, `avaliarStatusLogin` só distinguia `inativo` e o middleware só bloqueava `inativo` para estudantes. Resultado: um estudante apagado obtinha token completo. Não acontecia com admin/academia (bloqueados por `!= ativo`) nem no 2FA (`carregarIdentidadeAuth` filtra `deletado`).

**Impacto:** a exclusão não revogava o acesso. O estudante apagado continuava a ler dados, fazer pagamentos e usar todas as rotas.

**Correção:** bloqueio em três camadas:
1. **Projeção** (`estudante_projection.go:1321-1343`): `GetAuthByIdentificador` adiciona `AND status <> 'deletado'` no `WHERE` — estudante `deletado` nunca é retornado para autenticação.
2. **Login** (`auth_handlers.go:219-277`): `avaliarStatusLogin` retorna erro `"conta indisponível"` para `status == 'deletado'` (estudante e academia).
3. **Middleware** (`auth.go:170-195`): `AuthMiddleware` verifica `userDTO.Status == "deletado"` para estudante e academia e retorna `401`.
4. **Defesa em profundidade** nos handlers de leitura (`GetNotasEstudante`, `GetFaltasEstudante`, `GetAvaliacoesFinaisEstudante`, `SolicitarServicoExtra`): verificam `estudante.Status == "deletado"` antes de processar e retornam `401`.
A decisão D4 mantém `deletado` bloqueado para os três tipos de conta.

---

## SEC-14 — Hashes de senha expostos nos endpoints de eventos (Média) — ✅ resolvido

**Onde:** `internal/handlers/estudante_handlers.go` — `GetEventosEstudante` (linhas 748-805), `GetEventoAuditoria` (linhas 1186-1260); `repository.GetEventHistory` + `db.Event` (`event_store.go:15`).

**O que acontece.** Devolviam os eventos **tal como estão guardados**, com o `payload` completo. O evento de criação (`SenhaHash`) e o de troca de senha (`SenhaAlteradaEvent.NovaSenhaHash`) levam o **hash bcrypt** da senha. Quem pode chamar: o próprio estudante, a **academia** (qualquer estudante com o seu `codigo_academia`) e os admins.

**Impacto:** a academia, ou quem comprometer a conta de uma academia, obtinha hashes de senhas para quebra offline.

**Correção:** funções de redação adicionadas em `estudante_handlers.go`:
- `redigirEventoSensivel(event *db.Event) *db.Event` — cópia do evento com `payload` limpo (remove chaves `SenhaHash`, `NovaSenhaHash`, `*Hash`).
- `redigirEventosSensiveis(events []*db.Event) []*db.Event` — aplica a anterior a uma lista.
Aplicadas em `GetEventosEstudante` (linha 798) e `GetEventoAuditoria` (linha 1255). O evento guardado não muda; só a resposta é limpa. Teste: nenhuma resposta contém `$2a$` nem `$2b$`.

---

## SEC-15 — Isolamento entre academias: a atual lê registos das anteriores e a anterior perde o acesso (Média) — ✅ resolvido

**Onde:** `internal/handlers/notas_handlers.go:397` (`GetNotasEstudante`), `faltas_handlers.go:371` (`GetFaltasEstudante`), `avaliacao_final_handler.go:1077` (`GetAvaliacoesFinaisEstudante`); nova projeção `internal/projections/estudante_vinculos_projection.go`; `internal/handlers/helpers.go:158, 295` (`getEstudanteVinculosProjection`, `acessoAcademiaAoEstudante`); `internal/projections/manager.go:348` (rebuild order); `migrations/136_estudante_vinculos.sql`.

**O que acontece.** Estes handlers autorizavam pela academia do **último vínculo** (`estudante.CodigoAcademia`) e depois carregavam os registos **de todas as academias** (`GetByEstudante(codigo)`), filtrando apenas se o chamador passasse `codigos_academia` na query string. Num estudante que passou da academia A para a B: **B lia as notas, faltas e avaliações registadas por A**, e **A era barrada logo na autorização**, perdendo o acesso ao que ela própria registou.

**Impacto:** exposição de dados entre instituições e perda de acesso de cada academia à sua própria informação.

**Correção:** vínculos históricos + filtro forçado por academia do chamador.
1. **Nova projeção `projection_estudante_vinculos`** (`estudante_vinculos_projection.go`): reconstrói histórico a partir dos eventos `EstudanteCriadoComVinculo`, `EstudanteReintegrado`, `EstudanteDesvinculadoDaAcademia`. Cada linha guarda `estudante_id`, `codigo_academia`, `vinculado_em`, `desvinculado_em` (NULL = ativo). Índices por academia, por estudante, por par; constraint única impede dois vínculos ativos com a mesma academia.
2. **Rebuild order** (`manager.go:348`): `"estudante_vinculos"` entre `"estudantes"` e `"turmas"`.
3. **Helper `acessoAcademiaAoEstudante`** (`helpers.go:295`): chama `TeveVinculoAlgumVez` na projeção — retorna `true` se a academia **já teve** vínculo com o estudante (ativo ou histórico).
4. **Filtro forçado nas 3 rotas** (`notas_handlers.go:397`, `faltas_handlers.go:371`, `avaliacao_final_handler.go:1077`): após confirmar vínculo via helper, **ignora** qualquer `codigos_academia` vindo na query e **injeta** `filtros.codigosAcademia = []string{academiaDTO.CodigoAcademia}`. O estudante continua a ver tudo (sem filtro de academia).
Fora de escopo desta tarefa (Partes 6.5.1/6.5.2/6.5.3): perfil reduzido para academia anterior, filtro de eventos por período de vínculo, parâmetro `vinculo=` na lista de estudantes, cancelamento de inscrições ao desvincular/apagar, avisos no frontend.

---

# Parte 2 — Eliminar tudo o que não é Brevo

**Estado atual (verificado):**

| Mecanismo | Quem usa | Ação |
|---|---|---|
| Brevo | Verificação de e-mail, aviso de nova academia, 2FA | **Manter** |
| EmailJS | Apenas `SendAdminWelcomeEmail`. `SendAcademiaCadastradaEmail` não tem chamadores | **Eliminar tudo**; boas-vindas do admin passam para Brevo |
| SMTP (`net/smtp`) | Ninguém (`SendAcademiaCadastradaEmailSMTP` só tem testes) | **Eliminar tudo** |
| Frontend a enviar e-mail | Ninguém | Remover restos |
| `gerar-token/*` | Ninguém | **Eliminar** (SEC-01) |

**O que passa a usar Brevo:** (a) o e-mail de credenciais do administrador (hoje EmailJS) e (b) o novo e-mail de **link** de recuperação de senha (substitui o e-mail com senha). Os e-mails de verificação, de aviso de academia e do 2FA já usam Brevo.

## 2.1 Backend — a eliminar

### A. Ficheiros a apagar

| Ficheiro | Motivo |
|---|---|
| `internal/services/smtp_mailer.go` | Cliente SMTP, sem chamadores reais |
| `internal/services/smtp_mailer_test.go` | Testa só o código acima |
| `internal/services/smtp_mailer_e2e_test.go` | Servidor SMTP falso; os helpers `startMockSMTPServer`/`waitMail` não são usados fora dele |
| `internal/services/academia_cadastrada_email_test.go` | Testes exclusivos de `SendAcademiaCadastradaEmail` (EmailJS) e de `IsEnabled()` |

### B. `internal/services/email_service.go`

| Linhas | O que remover |
|---|---|
| 5, 9, 12, 15 (imports) | `"bytes"`, `"encoding/json"`, `"io"`, `"net/http"`; e `"math/big"` se ficar sem uso |
| 23-33 | Campos `serviceID`, `templateVerify`, `templateReset`, `publicKey`, `privateKey`, `enabled`, `httpClient` |
| 35-42 | `type EmailJSRequest` |
| 44-75 | `NewEmailService` atual (lê `EMAILJS_*`) |
| 77-79 | `IsEnabled()` |
| ≈ 93-140 | `GenerateSecurePassword` (14 caracteres, com símbolos): substituída por `GenerateTempPassword` (Parte 3) |
| 205-298 | `sendEmailViaEmailJS` |
| 361-385 | `SendPasswordResetEmail` (envia senha): substituída por `SendPasswordResetLinkEmail` (Parte 4) |
| 396-423 | `SendAdminWelcomeEmail` atual: reescrita para Brevo (abaixo) |
| 437-483 | `SendAcademiaCadastradaEmail` (EmailJS) |
| 485-511 | `SendAcademiaCadastradaEmailSMTP` |
| 560-571 | `GetDefaultPassword` (D2) |
| 299-310, 340-360 | Comentários históricos sobre EmailJS |

**Início do ficheiro depois da limpeza:**

```go
import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type EmailService struct {
	db          *sqlx.DB
	frontendURL string
}

func NewEmailService(db *sqlx.DB) *EmailService {
	return &EmailService{
		db:          db,
		frontendURL: getEnvOrDefault("FRONTEND_URL", "http://localhost:3000"),
	}
}
```

(`ErrEmailNaoConfigurado` já existe em `email_dois_fatores.go:24` e é reutilizado em todos os envios.)

### C. `brevo_mailer.go` e `brevo_mailer_test.go`

Remover o *fallback* para `EMAIL_USER` (variável do SMTP) em `loadBrevoConfig`:

```go
cfg := BrevoConfig{
	APIKey:      os.Getenv("BREVO_API_KEY"),
	SenderEmail: os.Getenv("EMAIL_FROM"),
	SenderName:  "Spuri",
}
```

**Antes de aplicar**, confirme que `EMAIL_FROM` está definido em produção; senão todos os e-mails (incluindo 2FA) deixam de sair. Ajustar `brevo_mailer_test.go` (linhas 8-34), que testa o *fallback* com `EMAIL_USER`.

### D. `auth_email_handlers.go`

| Linhas | Função | Ação |
|---|---|---|
| 188-242 | `GerarTokenVerificacao` | **Eliminar** |
| 247-327 | `GerarTokenRecuperacao` | **Eliminar** (SEC-01) |
| 336 | rota `verificar-email/solicitar` | **Corrigir** (ver E) |
| 403-583 | `SolicitarRecuperacaoSenha` | **Reescrever** (Parte 4) |
| 590-757 | `ResetarSenha` | **Manter** e ajustar (Parte 4) |
| 763-771 | `gerarEEnviarTokenVerificacao`, `gerarEEnviarTokenRecuperacao` | **Eliminar** (código morto) |

### E. `cmd/server/main.go`

Remover as rotas `gerar-token/*` (linhas 339 e 344) e `EmailRateLimit` (linha 333). **Corrigir também um defeito encontrado:** `POST /email/verificar-email/solicitar` (linha 336) está registada **sem** `AuthMiddleware`. O handler lê `user_id`/`user_type` do contexto e ignora o erro (`userID, _ := …`); sem o middleware, `user_type` chega vazio e responde `"tipo de usuário inválido"`. Pelo código, **o botão "Verificar e-mail" do frontend (que envia o JWT) não consegue funcionar em produção**. Os testes não o detetam porque injetam `user_id` à mão.

```go
emailGroup := router.Group("/email")
{
	emailGroup.POST("/verificar-email/:token", handlers.VerificarEmail)
	emailGroup.POST("/recuperar-senha/solicitar", handlers.SolicitarRecuperacaoSenha)
	emailGroup.POST("/recuperar-senha/:token", handlers.ResetarSenha)

	emailAuthGroup := emailGroup.Group("/")
	emailAuthGroup.Use(middleware.AuthMiddleware())
	{
		emailAuthGroup.POST("/verificar-email/solicitar", handlers.SolicitarVerificacaoEmail)
	}
}
```

### F. `internal/handlers/helpers.go`

`getEmailService` cria um `EmailService` novo a cada chamada. Passar a uma instância única (ex.: no contexto Gin, como `dbClient`).

### G. `.env.example`

Remover: bloco **EmailJS** (linhas 95-110, incluindo a nota desatualizada da linha 108); bloco **SMTP** (linhas 116-128, mantendo só `EMAIL_FROM`); `RATE_LIMIT_*` (179-182); `ENABLE_EMAIL_VERIFICATION` e `ENABLE_PASSWORD_RECOVERY` (190-191, nenhum código Go os lê). Bloco final:

```dotenv
ENV=development               # development, test ou production
FRONTEND_URL=https://seu-frontend.vercel.app

# Brevo (único canal de e-mail)
BREVO_API_KEY=
EMAIL_FROM=                   # remetente verificado no Brevo

# Credenciais temporárias (Parte 3)
# SMS só é enviado com ENV=production. Em desenvolvimento, para testar,
# defina true para escrever as credenciais no log (ignorado em produção).
# CREDENCIAIS_LOG_DEV=true

# Segredo de instalação do POST /bootstrap (SEC-11)
BOOTSTRAP_TOKEN=
```

### H. `Documentação da API.md`

Remover `POST /email/gerar-token/verificacao` (linha 990) e `…/recuperacao` (linha 1013). Reescrever `POST /email/recuperar-senha/solicitar` (linha 1049, ainda descreve token e "expira em 1 hora") e `POST /email/recuperar-senha/:token` (linha 1076) para o fluxo por link. Corrigir as frases "Rate limit ativo" (linhas 657 e 1106) e a regra de senha da linha 1463 (diz 6–128; o código exige 8–128).

### I. Ambiente de produção (fora do código)

Remover `EMAILJS_*`, `EMAIL_HOST`, `EMAIL_PORT`, `EMAIL_USER`, `EMAIL_PASS` do serviço de hospedagem e **revogar** a chave privada do EmailJS e, se existir, a senha de app do Gmail. Definir `BOOTSTRAP_TOKEN`, `CREDENCIAIS_*` conforme a Parte 3.

## 2.2 Frontend — a eliminar

| Ficheiro | O que remover |
|---|---|
| `src/lib/api/services.ts` (linhas 645-663) | O objeto `emailService` inteiro (`verificarEmail`, `solicitarVerificacaoEmail`, `solicitarRecuperacaoSenha`, `resetarSenha`, `gerarTokenVerificacao`, `gerarTokenRecuperacao`). Nada o importa; o serviço em uso é `emailAuthService` (`services/email.service.ts`) |
| `src/lib/api/index.ts` (linha 11) | O `export { emailService }` |
| `src/types/email-auth.ts` | `SenhasPadrao`, `SenhaPadrao`, `getSenhaPadrao` (Parte 5). **Não apagar** `GerarTokenRecuperacaoRequest`: é usado por `email.service.ts`; renomear para `SolicitarRecuperacaoRequest` |
| `.env.example` | `EMAIL_HOST`, `EMAIL_PORT`, `EMAIL_SECURE`, `EMAIL_USER`, `EMAIL_PASS`, `ADMINS_NOTIFICACAO_ACADEMIA` (nenhum código as lê) e `NEXT_PUBLIC_APP_URL` (não usada em `src/`) |

## 2.3 E-mail de credenciais do administrador (EmailJS → Brevo)

Hoje, se `EMAILJS_*` não estiver configurado, a senha vai para o log e a API diz que foi enviada (SEC-07); se `EMAILJS_TEMPLATE_ADMIN_WELCOME` faltar, usa o template de reset. O link enviado (`/admin/login`) **não existe no frontend** (a única página de login é `/login`).

**Substituir em `email_service.go` (linhas 396-423):**

```go
// SendAdminWelcomeEmail envia ao admin criado por outro admin as credenciais
// temporárias, via Brevo. Falha de forma explícita se o Brevo não estiver
// configurado: RegisterAdmin já trata o erro com o aviso "email_nao_enviado".
func (s *EmailService) SendAdminWelcomeEmail(email, nome, senhaTemporaria, role string) error {
	if email == "" {
		return fmt.Errorf("email vazio")
	}
	cfg, ok := loadBrevoConfig()
	if !ok {
		log.Printf("[EMAIL-BREVO] ⚠️  Brevo não configurado — credenciais NÃO enviadas ao admin %s (%s)", email, role)
		return ErrEmailNaoConfigurado
	}

	loginURL := fmt.Sprintf("%s/login", s.frontendURL)
	subject := "Bem-vindo ao Spuri - Acesso de Administrador"
	htmlBody := renderAdminWelcomeEmailHTML(nome, email, role, senhaTemporaria, loginURL, s.frontendURL)
	textBody := renderAdminWelcomeEmailText(nome, email, role, senhaTemporaria, loginURL)

	if err := sendBrevoEmail(cfg, email, nome, subject, textBody, htmlBody); err != nil {
		return fmt.Errorf("brevo: %w", err)
	}
	return nil
}
```

**Acrescentar em `email_html_templates.go`** (reutiliza os componentes do e-mail de recuperação atual):

```go
func renderAdminWelcomeEmailHTML(userName, userEmail, role, tempPassword, loginURL, frontendURL string) string {
	safeName := html.EscapeString(userName)
	safeEmail := html.EscapeString(userEmail)
	safeRole := html.EscapeString(role)
	safePassword := html.EscapeString(tempPassword)

	bodyHTML := fmt.Sprintf(`
      <p style="margin:0 0 6px; font-family:%s; font-size:12px; font-weight:700; letter-spacing:0.08em; text-transform:uppercase; color:%s;">Acesso de administrador</p>
      <h1 class="spuri-h1 spuri-heading" style="margin:0 0 16px; font-family:%s; font-size:24px; line-height:1.3; font-weight:700; color:%s;">Bem-vindo ao Spuri</h1>
      <p class="spuri-body-text" style="margin:0; font-family:%s; font-size:15px; line-height:1.65; color:%s;">
        Olá, <strong style="color:%s;">%s</strong>! A sua conta de administrador (<strong>%s</strong>) foi criada.
      </p>
      <p class="spuri-body-text" style="margin:12px 0 0; font-family:%s; font-size:15px; line-height:1.65; color:%s;">Utilizador (login): <strong>%s</strong></p>

      <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" class="spuri-pass-box" style="margin-top:24px; border-radius:16px; background-color:%s; border:1px solid %s;">
        <tr>
          <td style="padding:22px; text-align:center;">
            <p style="margin:0 0 8px; font-family:%s; font-size:12px; font-weight:600; color:%s;">Sua senha temporária</p>
            <p class="spuri-pass" style="margin:0; font-family:%s; font-size:26px; font-weight:700; letter-spacing:0.05em; color:%s; word-break:break-all;">%s</p>
          </td>
        </tr>
      </table>

      <div style="text-align:center;">%s</div>

      %s
    `,
		emailTplFontStack, emailTplBrandBlue,
		emailTplFontStack, emailTplNavy,
		emailTplFontStack, emailTplGray700, emailTplNavy, safeName, safeRole,
		emailTplFontStack, emailTplGray700, safeEmail,
		emailTplBrand50, emailTplBrand100,
		emailTplFontStack, emailTplBrandBlueDark,
		emailTplMonoStack, emailTplNavy, safePassword,
		renderEmailButton(loginURL, "Ir para o Login"),
		renderEmailAlert("warning", "⚠️ Importante", "Será obrigado a trocar esta senha no primeiro acesso. Nunca a partilhe com ninguém."),
	)

	return renderEmailShell("A sua conta de administrador Spuri foi criada.", "Bem-vindo ao Spuri", bodyHTML, frontendURL)
}

func renderAdminWelcomeEmailText(userName, userEmail, role, tempPassword, loginURL string) string {
	return fmt.Sprintf(`Olá %s!

A sua conta de administrador (%s) no Spuri foi criada.

UTILIZADOR (LOGIN): %s
SENHA TEMPORÁRIA: %s

Será obrigado a trocar esta senha no primeiro acesso.

Acesse: %s

---
© %d Spuri - Todos os direitos reservados
`, userName, role, userEmail, tempPassword, loginURL, time.Now().Year())
}
```

---

# Parte 3 — Credenciais temporárias (D2 e D3)

## 3.1 Regras

| Quem é criado | Quem cria | Canal | Identificador enviado | Telefone / e-mail de destino |
|---|---|---|---|---|
| Estudante | Academia (cadastro direto, **lote**, aprovação de matrícula sem taxa) ou **sistema** (após pagamento da matrícula) | **SMS** | `codigo_estudante` | Nível **escolar** → `telefone_encarregado`; **superior** → `telefone` do estudante (regra já existente em `ValidarTelefonesMatricula`; sem nível definido, o que existir) |
| Academia | Admin `fpp` (`RegisterAcademia`) | **SMS** | `codigo_academia` | `telefone` da academia (**hoje opcional, passa a obrigatório**) |
| Administrador | Admin `fpp` (`RegisterAdmin`) | **E-mail (Brevo)** | o próprio e-mail | e-mail do admin (o `telefone` continua obrigatório no cadastro, mas não é usado para credenciais) |

**Não mudam** (o utilizador escolhe a própria senha): cadastro público de academia (`RegisterAcademiaPublica`) e `POST /bootstrap`.

**Sem senhas padrão.** Eliminam-se `GetDefaultPassword` e `GenerateSecurePassword`. A única fonte de senhas geradas passa a ser `GenerateTempPassword`.

## 3.2 Gerador de senha temporária

8 é o menor tamanho permitido (`ValidateSenha`: 8 a 128, só comprimento). A senha tem **exatamente 8** caracteres alfanuméricos, sem caracteres ambíguos para facilitar a leitura num SMS (`I l O o 0 1`): alfabeto de 54 símbolos, ≈ 46 bits de entropia, o que é suficiente para uma senha de uso único e troca obrigatória.

```go
// internal/services/senha_temporaria.go
package services

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

const (
	TempPasswordLength = 8
	tempUpper          = "ABCDEFGHJKMNPQRSTUVWXYZ"
	tempLower          = "abcdefghjkmnpqrstuvwxyz"
	tempDigits         = "23456789"
	tempAlphabet       = tempUpper + tempLower + tempDigits // 54 símbolos
)

// GenerateTempPassword devolve 8 caracteres alfanuméricos (crypto/rand),
// com pelo menos uma maiúscula, uma minúscula e um dígito.
func GenerateTempPassword() (string, error) {
	max := big.NewInt(int64(len(tempAlphabet)))
	for {
		b := make([]byte, TempPasswordLength)
		for i := range b {
			n, err := rand.Int(rand.Reader, max)
			if err != nil {
				return "", fmt.Errorf("falha ao gerar senha temporária: %w", err)
			}
			b[i] = tempAlphabet[n.Int64()]
		}
		s := string(b)
		if strings.ContainsAny(s, tempUpper) && strings.ContainsAny(s, tempLower) && strings.ContainsAny(s, tempDigits) {
			return s, nil
		}
	}
}
```

## 3.3 Telefone de destino

Criar um helper único (a regra de nível já existe em `ValidarTelefonesMatricula`) e **validar antes de criar a conta**, para não criar contas cujas credenciais não possam ser entregues:

```go
// TelefoneDestinoCredenciaisEstudante: superior → telefone do estudante;
// escolar → telefone do encarregado; sem nível, o que existir.
func TelefoneDestinoCredenciaisEstudante(anoFund, anoMedio, anoSuperior, telEstudante, telEncarregado *string) (string, error)
```

O resultado passa por `normalizarDestinatarioComunicacao` (`comunicacao_handlers.go:70`), que exige o formato `9XXXXXXXX`. O cadastro valida telefones só como 9 dígitos (`phoneRegex = ^[0-9]{9}$`), por isso um fixo como `222123456` seria aceite no cadastro mas rejeitado no envio. Aplicar a mesma regra do SMS (`^9\d{8}$`) ao telefone de destino no momento do cadastro. Em `RegisterAcademia`, tornar `Telefone` obrigatório (`academia_handlers.go:40`).

## 3.4 Envio por SMS: reutilizar a função padronizada

**Decisão: não criar um emissor paralelo.** Toda a lógica de envio (provedor padrão, *fallback* para o outro provedor, registo da mensagem) vive hoje **dentro do handler** `EnviarMensagemComunicacao`, acoplada ao `*gin.Context`. O plano é **extrair esse núcleo para uma função única**, chamada pelo handler e pelo envio de credenciais, sem mudança de lógica:

```go
// internal/handlers/comunicacao_envio.go
type atorComunicacao struct {
	ID             uuid.UUID
	Tipo           string // "admin" | "academia"  (chk_mensagens_comunicacao_enviado_por_tipo)
	CodigoAcademia string // obrigatório se Tipo=="academia"; vazio se "admin"
}

// enviarSMSComunicacao é o ÚNICO caminho de envio de SMS: normaliza o
// destinatário, valida o conteúdo, tenta o provedor padrão e depois o outro,
// e regista a mensagem (sucesso ou falha) com SaveWithAudit.
// Corpo = o das linhas 252-310 de EnviarMensagemComunicacao, sem alterações.
func enviarSMSComunicacao(c *gin.Context, ator atorComunicacao, destinatario, conteudo string, audit db.AuditContext) (*aggregates.MensagemComunicacao, error)
```

`EnviarMensagemComunicacao` passa a ser só: ler o corpo → `comunicacaoActor` → `enviarSMSComunicacao` → mapear o resultado para HTTP (o `502` com `detalhes_tentativas` se `Status == falhou`).

**Restrições do esquema que determinam o ator** (`migrations/124_comunicacao_mensagens.sql`): `enviado_por_tipo` só aceita `'admin'` ou `'academia'`, `enviado_por` é `UUID NOT NULL` e `codigo_academia` é obrigatório para `academia` e nulo para `admin`. Não é preciso alterar o esquema:

| Origem da criação | Ator da mensagem |
|---|---|
| Academia cadastra estudante (direto, lote, aprovação) | A **academia** (`ID`, `'academia'`, `codigo_academia`) |
| Pagamento da matrícula (`efetivarVinculoMatriculaPaga`, ator "sistema") | A **academia dona do vínculo** (já carregada na função): a mensagem é registada em nome dela |
| Admin cadastra academia | O **admin** (`ID`, `'admin'`, `codigo_academia` nulo) |

**Risco aceite (decisão do utilizador).** Como o envio passa pela função padronizada, a senha temporária fica gravada no conteúdo da mensagem (evento `MensagemComunicacaoRegistrada` e `projection_mensagens_comunicacao`), visível no histórico à academia que enviou e aos admins. É aceitável por ser temporária e de troca obrigatória; depois de trocada, a mensagem deixa de ser sensível. Estes atores já têm poder sobre a conta (a academia cria o estudante).

**Pré-requisito de produção:** deve existir um provedor padrão configurado (`projection_comunicacao_config.provedor_padrao`) e pelo menos um remetente com credenciais; sem isso o envio devolve erro e o resultado é "não enviado" (ver 3.6).

## 3.5 Só em produção

O envio de SMS só ocorre com `ENV=production` (a mesma convenção do 2FA). Noutros ambientes **não se envia nem se regista mensagem**; o resultado é `"motivo": "ambiente_nao_producao"`. Para testes locais, com `CREDENCIAIS_LOG_DEV=true` e **nunca** em produção, as credenciais são escritas no log (padrão do 2FA).

```go
// internal/handlers/credenciais.go
type resultadoCredenciais struct {
	Canal   string `json:"canal"`            // "sms" | "email"
	Enviada bool   `json:"enviada"`
	Motivo  string `json:"motivo,omitempty"` // ambiente_nao_producao | provedor_nao_configurado | falha_envio
}

func enviarCredenciaisSMS(c *gin.Context, ator atorComunicacao, audit db.AuditContext,
	telefone, nomeCurto, identificador, senha string) resultadoCredenciais {

	if !strings.EqualFold(os.Getenv("ENV"), "production") {
		logCredenciaisDev(identificador, senha) // só com CREDENCIAIS_LOG_DEV=true e ENV != production
		return resultadoCredenciais{Canal: "sms", Motivo: "ambiente_nao_producao"}
	}
	msg, err := enviarSMSComunicacao(c, ator, telefone,
		services.MensagemCredenciaisSMS(nomeCurto, identificador, senha), audit)
	switch {
	case errors.Is(err, errProvedorPadraoNaoConfigurado):
		return resultadoCredenciais{Canal: "sms", Motivo: "provedor_nao_configurado"}
	case err != nil || msg == nil || msg.Status != aggregates.StatusMensagemComunicacaoEnviada:
		return resultadoCredenciais{Canal: "sms", Motivo: "falha_envio"}
	}
	return resultadoCredenciais{Canal: "sms", Enviada: true}
}
```

Texto do SMS (ASCII, sem acentos, para caber num único segmento GSM-7; o nome é normalizado e truncado):

```go
func MensagemCredenciaisSMS(nomeCurto, identificador, senha string) string {
	return fmt.Sprintf("Spuri: conta de %s criada. Usuario: %s Senha temporaria: %s Altere a senha no primeiro acesso.",
		nomeCurto, identificador, senha)
}
```

## 3.6 Integração nos pontos de criação

Em todos: **criar o evento primeiro, enviar depois do commit**. Falha de envio **nunca** desfaz a criação; devolve-se `201` com o objeto `credenciais` (`enviada: false` + motivo) e a conta fica elegível a **reenvio** (3.8). A senha em claro nunca entra em eventos (só o hash), nem em respostas, nem em logs.

```go
senha, err := services.GenerateTempPassword()
hash, _ := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
// ... est.CriarComVinculo(..., string(hash), ...)
est.DefinirSenhaTemporaria()                       // novo evento (3.7)
repo.SaveWithAudit(est, audit)                     // commit primeiro
resultado := enviarCredenciaisSMS(c, ator, audit, telefone, nomeCurto, codigoEstudante, senha)
// resposta: ..., "credenciais": resultado
```

| Ponto | Ficheiro:linha | Alteração |
|---|---|---|
| Estudante (cadastro direto e **lote**) | `estudante_handlers.go:264`, dentro de `registerEstudantePorAcademiaComRequestModo` (também chamada por `job_item_handlers.go:160` e `batch_handlers.go:252`) | Gerar senha temporária; enviar SMS após o commit. Em lote, o envio ocorre por item (custo = 1 SMS por estudante) |
| Aprovação de matrícula sem taxa | `solicitacao_matricula_handlers.go:394` | Idem |
| Matrícula paga | `solicitacao_matricula_handlers.go:591` (`efetivarVinculoMatriculaPaga`) | Idem, ator = academia do vínculo. Não há utilizador a quem mostrar aviso: o estado fica visível na lista (3.9) |
| Academia criada por admin | `academia_handlers.go:147` | Idem, ator = admin. `Telefone` obrigatório |
| Admin criado por admin | `admin_handlers.go:333` | `GenerateTempPassword` em vez de `GenerateSecurePassword`; e-mail Brevo (2.3); `DefinirSenhaTemporaria()`; se o Brevo falhar, mantém-se o aviso `email_nao_enviado` já existente |

**Lotes (verificado).** O cadastro em lote síncrono (`batch_handlers.go`, até 100 estudantes) percorre os itens dentro do pedido com um `newFakeContext` por item. Esse contexto copia `user_id`, `user_type`, `dbClient`, `repository`, `projManager` e `request_id`, e os *getters* do módulo de comunicação assentam em `projManager`, por isso `enviarSMSComunicacao` funciona nele. Mas os clientes SMS têm *timeout* de **15 s** cada: 100 envios em sequência podem demorar até 25 minutos no pior caso. Por isso, **nos lotes o SMS não é enviado dentro do laço**: acumula-se a lista de credenciais e envia-se **depois do commit**, com um *pool* de 5 envios em simultâneo, num contexto copiado (`c.Copy()`), e cada item da resposta traz `credenciais: {"canal":"sms","enviada":false,"motivo":"em_fila"}`. O resultado real (enviada ou falhada) fica no histórico de mensagens, e as falhas resolvem-se com o reenvio (3.8). As variantes assíncronas (`…/register/async`, até 100 itens, executadas como *jobs*) enviam por item, no próprio *worker*.

## 3.7 Troca obrigatória no primeiro acesso (`deve_alterar_senha`)

Segue o padrão do 2FA (evento no ledger + coluna na projeção reconstruível) e **evita alterar assinaturas** (há 110 chamadas de teste a `.Criar(`).

**Migration `135_senha_temporaria.sql`:**

```sql
BEGIN;
ALTER TABLE projection_admins     ADD COLUMN IF NOT EXISTS deve_alterar_senha    BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE projection_academias   ADD COLUMN IF NOT EXISTS deve_alterar_senha    BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE projection_estudantes  ADD COLUMN IF NOT EXISTS deve_alterar_senha    BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE projection_admins     ADD COLUMN IF NOT EXISTS sessoes_validas_desde TIMESTAMPTZ;
ALTER TABLE projection_academias   ADD COLUMN IF NOT EXISTS sessoes_validas_desde TIMESTAMPTZ;
ALTER TABLE projection_estudantes  ADD COLUMN IF NOT EXISTS sessoes_validas_desde TIMESTAMPTZ;
COMMIT;
```

**Domínio e projeções:**
- Novo evento `SenhaTemporariaDefinida`, partilhado pelos três agregados, com o método `DefinirSenhaTemporaria()` (levanta o evento; `Apply` apenas o aceita). **Registar o tipo em `validEventTypes`** (`internal/db/safe_queries.go:9`, onde já consta `DoisFatoresAtivados`).
- Projeção: o evento põe `deve_alterar_senha = TRUE` (helper partilhado, igual a `setDoisFatoresAtivo` em `projections/dois_fatores.go`).
- Eventos de senha alterada: `AdminSenhaAlterada` e `AcademiaSenhaAlterada` já têm `motivo`. O motivo `alteracao_usuario` limpa a flag; `reset_senha` limpa a flag **e** atualiza `sessoes_validas_desde`; o novo motivo `credenciais_reenviadas` **liga** a flag e atualiza `sessoes_validas_desde`. O `SenhaAlteradaEvent` do estudante **não tem `motivo`**: acrescentar o campo opcional `Motivo` (com `json:",omitempty"`, mantendo o nome por omissão como nos demais eventos; compatível com eventos antigos) e um método novo `AlterarSenhaComMotivo(hash, motivo)`; `AlterarSenha(hash)` passa a delegar com `alteracao_usuario`, sem mudar a assinatura.

**Imposição (servidor, não só frontend)** no `AuthMiddleware` (`internal/middleware/auth.go`): a consulta que `verificarStatusUsuario` já faz por pedido passa a ler também `deve_alterar_senha` e `sessoes_validas_desde`.
- Se `deve_alterar_senha = TRUE`, bloquear **tudo** exceto `PUT /alterar-senha`, `GET /meu-perfil` e `POST /logout`, com `403` e código `SENHA_TEMPORARIA` (mesmo padrão do `AcessoRestritoFinanceiro`).
- Se `claims.IssuedAt < sessoes_validas_desde`, responder `401` (SEC-10).
- O token restrito deixa de existir (Parte 6): o estudante `inativo` conclui a troca obrigatória normalmente, porque `PUT /alterar-senha` está na *allowlist* de escrita dele.

**Resposta de login** (`responderLoginComSucesso`, `auth_handlers.go:238`) e `GET /meu-perfil` passam a incluir `deve_alterar_senha`.

**`AlterarSenha` (`profile_handlers.go`):** rejeitar `nova_senha == senha_atual`; remover a exigência de estado `ativo` para estudantes (ver Parte 6).

## 3.8 Reenvio de credenciais

Quando o SMS/e-mail falha, ou o utilizador perdeu a senha e não tem e-mail verificado, a entidade que o gere pode **redefinir e reenviar** as credenciais:

| Rota | Quem pode |
|---|---|
| `POST /estudantes/:codigo/credenciais/reenviar` | Academia dona do estudante, ou admin `fpp` |
| `POST /dominis/academias/:codigo/credenciais/reenviar` | Admin `fpp` (mesma regra de `RegisterAcademia`) |
| `POST /dominis/administradores/:id/credenciais/reenviar` | Admin `fpp` (mesma regra de `RegisterAdmin`) |

Os admins `adm` e `gerente` **não** têm esta permissão (um `adm` poderia assim tomar conta de um `fpp`).

Efeito: gera nova senha temporária, grava o evento de senha com motivo `credenciais_reenviadas` (liga `deve_alterar_senha`, invalida sessões), envia pelo canal do tipo de conta (SMS ou e-mail) e devolve `credenciais`. **Intervalo mínimo de 10 minutos** por conta, medido por `sessoes_validas_desde` (sem tabela nova), para limitar o custo do SMS. A ação fica auditada (`SaveWithAudit`). Confirme a matriz de papéis; a tabela acima segue as regras de criação.

## 3.9 Contas existentes (migração)

Um *script* único (com *dry-run* que só conta) compara, com `bcrypt.CompareHashAndPassword`, o hash de cada estudante e academia com o respetivo código, e o de cada admin com as 4 senhas públicas (Parte 5).

| Grupo | Ação recomendada |
|---|---|
| **Admins** com senha pública | **Redefinição imediata**: senha temporária + e-mail + `sessoes_validas_desde`. Quem conhece a senha antiga não pode ter oportunidade de entrar primeiro |
| Estudantes/academias com senha = código | Passo 1 (grátis): marcar `deve_alterar_senha` e emitir o evento. Passo 2 (custo SMS): redefinir e enviar em lotes, começando pelas contas ativas e com atividade recente |

O passo 1 sozinho **não elimina** a janela de risco (quem sabe o código pode entrar e trocar a senha antes do titular); só obriga a troca e dispara o fluxo de reenvio.

## 3.10 Frontend

- **Login:** se a resposta trouxer `deve_alterar_senha`, redirecionar para um ecrã de troca obrigatória (reutilizar a lógica de `PasswordSettingsCard`) e impedir a navegação para o resto do painel (`LoginForm.tsx`, `concluirLogin`; *guard* no layout do painel). Tratar o `403 SENHA_TEMPORARIA`.
- **Formulários de cadastro:** em `academias/cadastrar` tornar o telefone obrigatório; mostrar o resultado `credenciais` (toast "Credenciais enviadas por SMS" ou "SMS não enviado — use Reenviar credenciais"). Atualizar os textos que ainda falam em senha igual ao código: `academias/cadastrar/PageContent.tsx:26-28`, `estudantes/cadastrar/CadastroSingularForm.tsx:140`, `:152`, `:836`.
- **Criar administrador:** `CriarAdminRequest` (`src/types/api.ts:505`) tem `senha?` (o backend a ignora) e **não tem `telefone`**, que o backend exige em `/dominis/register`; por isso a criação de admins pela interface falha hoje. Remover `senha`, acrescentar `telefone`, retirar o campo "Senha inicial" do modal (`administradores/PageContent.tsx`) e atualizar o texto para indicar que as credenciais seguem por e-mail.
- **Listas** (estudantes, academias, administradores): botão "Reenviar credenciais" e indicador de "credenciais não confirmadas" quando `deve_alterar_senha` for verdadeiro.
- `PasswordSettingsCard` habilita o botão com `novaSenha.length >= 6`, mas a validação exige 8: alinhar para 8.

## 3.11 Testes a acrescentar

`GenerateTempPassword` (tamanho 8, alfabeto, classes obrigatórias, unicidade); `TelefoneDestinoCredenciaisEstudante` (escolar/superior/sem nível); `enviarSMSComunicacao` (provedor padrão, *fallback*, falha total, ator `admin`/`academia` respeitando o esquema); `ENV != production` não envia nem grava mensagem; login com flag devolve `deve_alterar_senha`; middleware bloqueia tudo fora da *allowlist* e libera depois de `AlterarSenha`; estudante `pendente_documentos` consegue trocar a senha; reenvio respeita o intervalo; `RegisterAcademia` rejeita telefone ausente/inválido; o evento `SenhaTemporariaDefinida` reconstrói a projeção.

## 3.12 Riscos residuais

SMS pode ser intercetado (troca de SIM) ou lido por terceiros no aparelho; o provedor mantém o seu próprio registo da mensagem; a senha temporária tem ≈ 46 bits. Mitigações já previstas: troca obrigatória, e (opcional, recomendado) **validade de 7 dias** da senha temporária com `senha_temporaria_expira_em`; passado o prazo o login falha e exige reenvio. Não incluí a validade no núcleo do plano por não ter sido pedida.

---

# Parte 4 — Recuperação de senha por link (opção B)

**Princípio:** a senha só muda quando o titular, com o e-mail na mão, abre um link de uso único e escolhe a nova senha. Resolve SEC-01, SEC-03, SEC-06 e remove a senha do e-mail.

## 4.1 Backend

1. **`SolicitarRecuperacaoSenha` (403-583) reescrita:**
   - Procura a conta pelo identificador (código ou e-mail) nos três tipos.
   - **Resposta sempre `200`** com a mesma mensagem ("se existir uma conta elegível, enviámos as instruções para o e-mail cadastrado"), exista ou não a conta, esteja ou não o e-mail verificado.
   - Se a conta existir, tiver e-mail verificado e e-mail cadastrado, chama `SendPasswordResetLinkEmail` **em *goroutine*** (tempo de resposta constante).
   - **Não altera a senha.**
   - Única resposta diferente: `503` se `ErrEmailNaoConfigurado` (independe da conta, não revela nada).
2. **Novo `SendPasswordResetLinkEmail`** (substitui `SendPasswordResetEmail`). `SaveToken` já gera o token e devolve-o:

```go
func (s *EmailService) SendPasswordResetLinkEmail(userID uuid.UUID, userType, email, nome string) error {
	cfg, ok := loadBrevoConfig()
	if !ok {
		return ErrEmailNaoConfigurado
	}
	token, err := s.SaveToken(userID, userType, "recuperacao_senha", email, 1*time.Hour)
	if err != nil {
		return fmt.Errorf("falha ao guardar token de recuperação: %w", err)
	}
	resetURL := fmt.Sprintf("%s/recuperar-senha/%s", s.frontendURL, token)
	subject := "Spuri - Redefinição de senha"
	if err := sendBrevoEmail(cfg, email, nome, subject,
		renderPasswordResetLinkEmailText(nome, resetURL),
		renderPasswordResetLinkEmailHTML(nome, resetURL, s.frontendURL)); err != nil {
		return fmt.Errorf("brevo: %w", err)
	}
	return nil
}
```

3. **Template** (em `email_html_templates.go`; o tipo de token `recuperacao_senha` já é aceite pela tabela `auth_tokens`):

```go
func renderPasswordResetLinkEmailHTML(userName, resetURL, frontendURL string) string {
	bodyHTML := fmt.Sprintf(`
      <p style="margin:0 0 6px; font-family:%s; font-size:12px; font-weight:700; letter-spacing:0.08em; text-transform:uppercase; color:%s;">Recuperação de senha</p>
      <h1 class="spuri-h1 spuri-heading" style="margin:0 0 16px; font-family:%s; font-size:24px; line-height:1.3; font-weight:700; color:%s;">Defina uma nova senha</h1>
      <p class="spuri-body-text" style="margin:0; font-family:%s; font-size:15px; line-height:1.65; color:%s;">Olá, <strong style="color:%s;">%s</strong>. Recebemos um pedido para redefinir a senha da sua conta. Clique no botão abaixo para escolher uma nova senha. O link é válido por 1 hora e só pode ser usado uma vez.</p>
      <div style="text-align:center; margin-top:24px;">%s</div>
      %s
    `,
		emailTplFontStack, emailTplBrandBlue,
		emailTplFontStack, emailTplNavy,
		emailTplFontStack, emailTplGray700, emailTplNavy, html.EscapeString(userName),
		renderEmailButton(resetURL, "Definir nova senha"),
		renderEmailAlert("warning", "Não foi você?", "Ignore este e-mail: a sua senha atual continua a funcionar e nada foi alterado."),
	)
	return renderEmailShell("Redefina a sua senha do Spuri.", "Redefinição de senha", bodyHTML, frontendURL)
}

func renderPasswordResetLinkEmailText(userName, resetURL string) string {
	return fmt.Sprintf(`Olá %s,

Recebemos um pedido para redefinir a senha da sua conta Spuri.
Abra o link abaixo para escolher uma nova senha (válido por 1 hora, uso único):

%s

Se não foi você, ignore este e-mail: a sua senha atual continua a funcionar.
`, userName, resetURL)
}
```

4. **`ResetarSenha` (590-757) mantém-se**, com estes ajustes:
   - consumo **atómico** do token (SEC-09);
   - ao concluir, invalidar os restantes tokens pendentes do utilizador e gravar o evento com motivo `reset_senha` (limpa `deve_alterar_senha`, atualiza `sessoes_validas_desde`; SEC-10);
   - mantém `ValidateSenha` (8 a 128) e a reverificação do e-mail verificado.
5. **Rotas:** ficam `POST /email/recuperar-senha/solicitar` e `POST /email/recuperar-senha/:token`. Desaparecem `gerar-token/*`.

## 4.2 Frontend

- **Nova página** `src/app/…/recuperar-senha/[token]/page.tsx` com formulário "nova senha + confirmação" (mínimo 8) que chama `POST /email/recuperar-senha/:token` (usar `emailAuthService`; o método `resetarSenha` do serviço antigo estava morto e é removido).
- `src/lib/route-guards.ts`: acrescentar `/recuperar-senha` às rotas públicas, como `/verificar-email`.
- `/esqueci-senha` e `/dominis/esqueci-senha`: trocar o texto para "enviaremos um link para o seu e-mail" e mostrar sempre a mesma confirmação (SEC-06).
- **Limitação a comunicar:** quem não tiver e-mail verificado não consegue recuperar a senha por este caminho; para esses casos serve o **reenvio de credenciais** (3.8).

---

# Parte 5 — Senhas padrão de admin: remover tudo (D6)

## 5.1 Árvore atual

| Repositório | Ficheiro | Ação |
|---|---|---|
| Frontend | `src/types/email-auth.ts` (≈ linhas 62-97) | Apagar `SenhasPadrao` (62), `SenhaPadrao` (74) e `getSenhaPadrao` (84), que ainda codifica "senha = código" para estudante e academia (nada os usa) |
| Frontend | `src/docs/Tarefas feitas/1 - …md` (≈ linhas 251-285) | Apagar o bloco de código com as senhas; reescrever a frase sobre "senhas padrão por role" |
| Backend | `internal/services/email_service.go:93` | O comentário `FIX E4-AA-04` repete os valores literais: reescrever sem eles (a função e o comentário somem com `GenerateSecurePassword`) |
| Backend | `internal/domain/aggregates/remetente_comunicacao_test.go:21` | `spuri123` é um identificador de remetente num teste (falso positivo); renomear para não gerar alertas |

## 5.2 Histórico git (obrigatório para "remover tudo")

As strings estão no histórico dos **dois** repositórios. Como as `main` de ambos já sofreram *force push*, reescrever o histórico é viável:

```bash
# em cada repositório, num clone espelho
git clone --mirror git@github.com:fredypdp/rastreio-backend.git
cd rastreio-backend.git
cat > /tmp/expressions.txt <<'EOF'
spuriadm==>REMOVIDO
spurigerente==>REMOVIDO
spurifpp==>REMOVIDO
spuri123==>REMOVIDO
EOF
git filter-repo --replace-text /tmp/expressions.txt
git push --force --mirror
# repetir para rastreio-frontend
```

Depois: todos os colaboradores fazem novo clone; pedir ao suporte do GitHub a remoção de vistas em cache e de referências antigas (PRs); *forks* e clones já feitos **não** são afetados.

## 5.3 Rotação (a parte que realmente protege)

Reescrever o histórico **não desfaz** a exposição. Considere as 4 senhas comprometidas para sempre. Aplicar o passo "Admins" da secção 3.9: redefinição imediata de todo admin cujo hash corresponda a alguma delas, com troca obrigatória e sessões invalidadas.

---

# Parte 6 — Login por estado da conta (D4)

## 6.1 O modelo de estados do estudante e a regra pedida

O estudante tem **dois eixos de estado**, e a regra "consultar tudo o que o envolve e pagar as suas contas" vale para **todas as combinações dos dois**.

**Eixo 1 — estado geral da conta** (`projection_estudantes.status`; transições no agregado `estudante.go`):

| Estado | Como se chega lá | Significado |
|---|---|---|
| `ativo` | Criação com vínculo (linha 1097), reintegração (1238), documentos completados (1429) | Vinculado a uma academia |
| `pendente_documentos` | Criação sem todos os documentos (`CriarComVinculoPendenteDocumentos`); passa a `ativo` quando se completam | Vinculado, com documentos em falta |
| `inativo` | **Só** por `EstudanteDesvinculadoDaAcademia` (linha 1210), aprovada pela academia, e só a partir de `ativo` | **Desvinculado** de academia (o `codigo_academia` não é limpo) |
| `deletado` | `Deletar` (autoexclusão, **só a partir de `inativo`**) ou `DeletarPorAcademia` (só `ativo` ou `pendente_documentos`) — linha 1223 | Conta apagada (*soft delete*: a linha e o hash da senha ficam) |

**Eixo 2 — estados académicos** (`status_escolar_fundamental`, `status_escolar_medio`, `status_superior`): cada um é `inativo`, `em_andamento` ou `finalizado`. Mudam com a matrícula, a interrupção e a finalização de cada nível (ensino fundamental, médio e superior). Um estudante que terminou todos os cursos continua `ativo` no eixo 1.

**Verificação: o eixo 2 não influencia login, consulta nem pagamento.** Procurei todas as utilizações dos estados académicos: só aparecem no domínio, na projeção e em duas funções de handlers: `validarMatriculaEmAndamento` (**lançar nota**, escrita da academia) e `inferirTipoEnsinoDoEstudante` (deduz o nível, não bloqueia). Nenhum handler financeiro nem o serviço `internal/finance` os consultam; nenhuma rota de leitura os usa como condição.

**Matriz resultante (depois de D4):**

| Estado geral | Login | Consultar tudo o que o envolve | Pagar as suas contas | Outras escritas |
|---|---|---|---|---|
| `ativo` | Sim | Sim | Sim | Conforme as regras de cada funcionalidade (inclui os estados académicos) |
| `pendente_documentos` | Sim | Sim | Sim | Idem |
| `inativo` (desvinculado) | Sim, com **token normal** | Sim | Sim | Só a *allowlist* (6.3.2) |
| `deletado` | **Não** (hoje consegue; ver SEC-13) | — | — | — |

Qualquer combinação com os estados académicos (`inativo`, `em_andamento` ou `finalizado` em fundamental, médio ou superior) segue a linha do estado geral. Hoje, para `ativo` e `pendente_documentos` isto já é assim; **só o `inativo` estava limitado** (pelo token restrito).

| Tipo | Hoje | Depois (D4) |
|---|---|---|
| Estudante | Login em qualquer estado; o `inativo` recebe um **token restrito** que só permite pagar mensalidades (`rotaFinanceiraRestrita`) | Login em qualquer estado **exceto `deletado`**, com token normal. O token restrito é **eliminado**. Consulta tudo (`GET`) e paga tudo (mensalidades, taxa de inscrição, obrigações de serviço extra) |
| Academia | Login e `AuthMiddleware` exigem `ativo` | Login em qualquer estado, **exceto `deletado`** |
| Admin | Exige `ativo` | **Sem mudança** |

**Duas rotas hoje inalcançáveis.** `PUT /estudante/bilhete-identidade` (o comentário do código diz que atende "exatamente esse estudante") e `POST /estudante/solicitacoes-status/revinculacao/:codigo_academia` foram feitas para o estudante desvinculado. O mesmo vale para `DELETE /estudante/conta`, que o domínio só aceita para `inativo`. O token restrito bloqueia as três; com D4 passam a funcionar como foram desenhadas. A página `/configuracoes/personalizar` do frontend já usa o conceito "vinculado" (`ativo` ou `pendente_documentos`) para mostrar o formulário do bilhete ao desvinculado.

## 6.2 Verificação pedida: "as funcionalidades já têm os seus próprios bloqueios?"

**Resultado: parcialmente, tanto para academias como para estudantes.**

### 6.2.1 Academias

O bloqueio por estado existe como middleware (`ValidarStatusAcademia`, `academia_middleware.go`), mas só está aplicado a 4 grupos de rotas `/academia/*` (`main.go:528, 536, 545, 580`). O próprio comentário `FIX AUTH-01 (E4-AA-02)` em `auth.go` explica que o gate central existe **para fechar o buraco do grupo `protected`**. Ao retirar o gate central das academias, ficam **sem bloqueio por estado**:

| Rotas acessíveis a academia | Onde | Risco se a academia estiver inativa |
|---|---|---|
| `POST /comunicacao/mensagens` | `main.go:379-385` | **Enviar SMS a custo da plataforma** |
| `POST /documentos/academias/:codigo/:campo/upload` | grupo `protected` | Carregar documentos |
| Grupo `/financeiro/*` (31 rotas) | `main.go` (`financeiro := protected.Group`) | Configurar credenciais de pagamento, criar/cancelar cobranças, gerar QR, **registrar pagamento externo**, anular/reativar obrigações, configurar mensalidades e matrículas |
| `/jobs`, `POST /jobs/:id/retry-failed` | `main.go:357-364` | Reexecutar lotes (criação de estudantes e, com a Parte 3, SMS) |
| Leituras do grupo `protected` (`/estudantes`, `/notas`, `/faltas`, `/avaliacoes*`, `/consultar-estudante/:codigo`, `/eventos-estudante/:codigo`, `/verificar-integridade/:codigo`, downloads de documentos…) | `main.go:408-452` | Ler dados |

**O que está correto:** todos os grupos `/academia/*` (4, com `ValidarStatusAcademia`) e `CriarSolicitacaoMatricula` (pública), que valida `academia.Status` (`solicitacao_matricula_handlers.go:114`).

**Escopo por academia (verificado).** Para saber se cada rota restringe os dados à academia autenticada, fiz uma varredura automática das **92 rotas de academia com parâmetro de caminho** (69 em `/academia/*`; 23 nos grupos `protected`, `financeiro`, `jobs` e `comunicacao`) e verifiquei manualmente as **21 sinalizadas** (sem padrão de pertença visível). **Todas estavam corretas**: eram wrappers que delegam em funções que verificam a pertença (`loadServico`, `loadDocumentoExtra`, `loadCategoriaServico`, `canAccess…`, `decidirSolicitacaoStatus`) ou, no caso de `AtualizarCredencial`, autorizam o escopo declarado e `ConfigureCredential` confirma com `credentialBelongsToScope` que a credencial `:id` pertence a esse escopo. As restantes 71 rotas têm uma verificação detetável por padrão; **isto é uma varredura, não uma prova linha a linha**. Ela verifica **quem** pode aceder a um estudante; **o que** cada academia vê dentro de um estudante que passou por várias academias é outra questão (SEC-15, 6.5.1).

### 6.2.2 Estudantes: rotas de escrita

Hoje o `inativo` não alcança nenhuma rota de escrita que não seja pagamento porque o token restrito a rejeita (`auth.go:160`). Sem esse token, só resta o que cada handler verifica. Auditei as 23 rotas de escrita que um estudante pode alcançar (grupo `/estudante` e grupo `protected`):

| Rota | Bloqueio próprio por estado? | Tratamento para o `inativo` |
|---|---|---|
| `POST /logout`, `PUT /alterar-senha`, `PUT /me/email`, `PUT /me/telefone`, `POST /dois-fatores/*` (4) | n/a (conta) | **Permitido** |
| `POST /financeiro/mensalidades/pagamento`, `POST /financeiro/servicos-extras/taxa-inscricao/pagamento`, `POST /financeiro/servicos-extras/obrigacao/pagamento` | n/a (pagamento) | **Permitido** |
| `PUT /estudante/bilhete-identidade` | **Sim**: rejeita `ativo`/`pendente_documentos`; só atende desvinculados | **Permitido** (feita para ele) |
| `POST /estudante/solicitacoes-status/revinculacao/:codigo_academia` | **Não** no handler; o domínio (`Reintegrar`) exige `Status == "inativo"` na aprovação | **Permitido**; acrescentar a verificação na criação |
| `DELETE /estudante/conta` | **Sim, no domínio**: `Deletar` só aceita `inativo` | **Permitido** (feita para ele; sem isto nenhum estudante conseguiria apagar a própria conta) |
| `PUT /estudante/encarregado/telefone` | Não (só valida o formato) | **Permitido** (dado de contacto, como `/me/telefone`) |
| `POST /estudante/solicitacoes-edicao/{nome, bilhete-identidade, bilhete-identidade-encarregado, data-nascimento}` (4) | **Sim**: exige `ativo` ou `pendente_documentos` | Bloqueado (também pelo handler) |
| `POST /estudante/servicos-extras/:id/solicitacao` | **Não.** Verifica `CodigoAcademia == nil`, que nunca é nulo: **a verificação existe mas não protege** | Bloqueado por padrão; corrigir o handler |
| `PUT /estudante/servicos-extras/minhas-inscricoes/:id/cancelar` | Não | Bloqueado por padrão |
| `POST /estudante/solicitacoes-status/interrupcao` e `/desvinculacao` | **Não** no handler; o domínio rejeita na aprovação (`Desvincular` exige `ativo`; a interrupção exige uma etapa `em_andamento`) | Bloqueado por padrão; corrigir o handler |

**Conclusão.** Quatro rotas de estudante (`servicos-extras/:id/solicitacao`, o cancelamento, `interrupcao` e `desvinculacao`) estão hoje protegidas **apenas** pelo token restrito. Retirando-o sem substituto, um estudante desvinculado poderia, por exemplo, solicitar serviços extras ou abrir pedidos de interrupção na fila da academia a que já não pertence. Por isso a regra para o `inativo` é **por método** (6.3.2), e não handler a handler: toda escrita nasce bloqueada, e só a *allowlist* a libera.

### 6.2.3 Estudantes: leituras (verificadas)

Verifiquei individualmente os **28 handlers `GET`** que um estudante pode alcançar. **Todos restringem o estudante aos próprios dados e nenhum depende de estado**:

| Grupo | Handlers | Escopo |
|---|---|---|
| Conta | `GetMeuPerfil`, `ObterDoisFatores` | O próprio |
| Histórico e auditoria | `GetEventosEstudante`, `GetEventoAuditoria`, `VerificarIntegridade` | `podeAuditarEstudante`: estudante só o próprio, academia só os seus, admin todos (**mas ver SEC-14 e SEC-15**) |
| Notas, faltas, turmas | `GetNotasEstudante`, `GetFaltasEstudante`, `GetTurmasEstudante`, `ListarNotas`, `ListarFaltas`, `GetMinhasAvaliacoes`, `ListarCategoriasNota` | O próprio |
| Avaliações finais | `ListarAvaliacoes`, `ListarAprovacoes`, `ListarReprovacoes` (e `GetAvaliacoesFinaisEstudante`, só academia/admin) | Estudante → o próprio; academia → a própria |
| Documentos | `ListarMeusDocumentosEstudante`, `DownloadMeuDocumentoEstudante`, `DownloadDocumentoEstudante`, `DownloadDocumentoSolicitacaoEdicaoEstudante`, … | `canAccessEstudanteDocument` e afins: estudante só os seus |
| Serviços extras | `ListarServicosExtrasCatalogoEstudante`, `ListarMinhasInscricoesServicoExtra`, `DownloadDocumentoSolicitacaoServicoExtraEstudante`, `MinhasPendenciasServicoExtra` | O próprio (o catálogo mostra o da academia anterior ao desvinculado; é só leitura e inócuo) |
| Solicitações | `ListarSolicitacoesEdicaoEstudante`, `ListarMinhasSolicitacoesStatusAcademicoHandler` | O próprio |
| Financeiro (consulta) | `ConsultarMensalidadesEstudante`, `ConsultarCobrancasEstudante` | Só as suas |

Fora do alcance do estudante (correto): `GetEstudantePorCodigo` e `ListarEstudantes` (só academia/admin; o estudante usa `/meu-perfil`) e as três rotas de anos letivos (configuração global, sem dados pessoais).

### 6.2.4 Estudantes: pagamento (verificado)

Os 3 handlers de pagamento (`IniciarPagamentoMensalidades`, `IniciarPagamentoTaxaInscricaoServicoExtra`, `IniciarPagamentoServicoExtraObrigacao`) e os 2 de consulta usam o código do **próprio** estudante autenticado e **não verificam nenhum estado** do estudante; o serviço `internal/finance` também não. Pagar uma obrigação de serviço extra depende do estado **da inscrição**, não do estudante.

### 6.2.5 Aprovação dos pedidos de status académico (verificada)

`decidirSolicitacaoStatus` confirma que a solicitação pertence à academia que decide. As regras de estado vivem no domínio e **estão corretas**: `DesvincularDaAcademia` exige `ativo`, `Reintegrar` exige `inativo`, e a interrupção exige exatamente uma etapa `em_andamento`. Portanto um pedido inválido seria rejeitado na aprovação, mas **já teria entrado na fila da academia**; daí a validação na criação (6.3.4).

## 6.3 Mudanças

### 6.3.1 Login (backend)

`avaliarStatusLogin` tem **3 chamadores**: `auth_handlers.go:193` (com `responderLoginComSucesso` em `:209`) e, no segundo passo do 2FA, `dois_fatores_handlers.go:369` (com `responderLoginComSucesso` em `:375`) e `:414` (reenvio do código). Todos têm de ser atualizados.

```go
// auth_handlers.go — avaliarStatusLogin (linha 219): (mensagemBloqueio, bloqueado)
func avaliarStatusLogin(userType, userStatus string) (string, bool) {
	switch userType {
	case "estudante":
		if userStatus == "deletado" { // SEC-13: conta apagada não entra
			return "conta indisponível. Entre em contato com a sua academia ou com o suporte.", true
		}
		return "", false // qualquer outro estado entra; a escrita é restringida no middleware (6.3.2)
	case "academia":
		if userStatus == "deletado" {
			return "conta indisponível. Entre em contato com o administrador.", true
		}
		return "", false
	default: // admin
		if userStatus == "ativo" {
			return "", false
		}
		return "conta inativa. Entre em contato com o suporte.", true
	}
}
```

`responderLoginComSucesso` (`auth_handlers.go:238`): remover o parâmetro `acessoRestritoFinanceiro` e o ramo `GenerateRestrictedFinanceToken` (linhas 243-247); emite sempre `middleware.GenerateToken`. Atualizar o comentário de `Login` (`auth_handlers.go:39`).

**SEC-13 (defesa em profundidade):** `GetAuthByIdentificador` (`estudante_projection.go:1323`) passa a filtrar `AND status <> 'deletado'`, como já fazem `GetByEmail` e `GetAll`.

### 6.3.2 Middleware (backend): eliminar o token restrito e aplicar a regra por método

**Eliminar em `internal/middleware/auth.go`:** o campo `AcessoRestritoFinanceiro` (linha 25); `GenerateRestrictedFinanceToken` (77-79); o parâmetro `restrictedFinance` de `generateToken` (82-90); o bloqueio das linhas 160-164 e a exceção `&& !(claims.AcessoRestritoFinanceiro && …)` da linha 165; `c.Set("acesso_restrito_financeiro", …)` (176); `rotaFinanceiraRestrita` (184-186); e o ficheiro de teste `auth_financeiro_restrito_test.go`.

**Substituir por:** `verificarStatusUsuario` (a consulta por pedido já existe) passa a devolver o estado da conta, e o `AuthMiddleware` aplica as regras por esta ordem (as regras 2 e 3 vêm da Parte 3.7 e do SEC-10):

```go
type estadoConta struct {
	Status              string
	DeveAlterarSenha    bool       // Parte 3.7
	SessoesValidasDesde *time.Time // SEC-10
}

conta, err := carregarEstadoConta(c, claims.UserID, claims.UserType) // SELECT status, deve_alterar_senha, sessoes_validas_desde
// (mantém-se o comportamento FIX E4-CI-03: erro de BD => deixa passar, com log)

switch {
case contaBloqueada(claims.UserType, conta.Status): // admin != "ativo"; academia == "deletado"; estudante == "deletado"
	// 401 "conta inativa ou não encontrada…"
case conta.SessoesValidasDesde != nil && claims.IssuedAt.Time.Before(*conta.SessoesValidasDesde):
	// 401 (SEC-10)
case conta.DeveAlterarSenha && !rotaPermitidaComSenhaTemporaria(c.Request.Method, c.FullPath()):
	// 403 {"codigo":"SENHA_TEMPORARIA"} (Parte 3.7)
case claims.UserType == "estudante" && conta.Status == "inativo" &&
	!rotaPermitidaEstudanteInativo(c.Request.Method, c.FullPath()):
	// 403 {"codigo":"CONTA_DESVINCULADA","message":"conta desvinculada: apenas consulta e pagamentos estão disponíveis"}
}
```

```go
// Estudante desvinculado ("inativo"): consulta tudo, paga, usa a própria conta e as
// três ações feitas para ele. Qualquer outra escrita é bloqueada por padrão.
func rotaPermitidaEstudanteInativo(method, fullPath string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true // consultar tudo o que o envolve
	}
	switch method + " " + fullPath {
	case
		// conta
		"POST /logout", "PUT /alterar-senha", "PUT /me/email", "PUT /me/telefone",
		"POST /dois-fatores/ativacao/solicitar", "POST /dois-fatores/ativacao/confirmar",
		"POST /dois-fatores/desativacao/solicitar", "POST /dois-fatores/desativacao/confirmar",
		"PUT /estudante/encarregado/telefone",
		// pagamentos
		"POST /financeiro/mensalidades/pagamento",
		"POST /financeiro/servicos-extras/taxa-inscricao/pagamento",
		"POST /financeiro/servicos-extras/obrigacao/pagamento",
		// feitas para o estudante desvinculado
		"PUT /estudante/bilhete-identidade",
		"POST /estudante/solicitacoes-status/revinculacao/:codigo_academia",
		"DELETE /estudante/conta":
		return true
	}
	return false
}
```

`c.FullPath()` devolve o padrão da rota (com `:codigo_academia`), por isso a *allowlist* não depende dos valores dos parâmetros. Para liberar mais alguma ação ao `inativo`, basta acrescentar a linha correspondente. **A regra só olha o estado geral**: os estados académicos não entram no middleware, em coerência com 6.1.

### 6.3.3 Academias: bloqueio próprio nas rotas

`ValidarStatusAcademia()` já ignora quem não é academia, por isso pode ser aplicado a grupos partilhados com admin e estudante. Reorganizar o grupo `protected`:

```go
// rotas de conta: sem gate de estado (qualquer academia pode usá-las)
conta := router.Group("/")
conta.Use(middleware.AuthMiddleware())
{
	conta.POST("/logout", handlers.Logout)
	conta.PUT("/alterar-senha", handlers.AlterarSenha)
	conta.GET("/meu-perfil", handlers.GetMeuPerfil)
	conta.PUT("/me/email", handlers.AtualizarMeuEmail)
	conta.PUT("/me/telefone", handlers.AtualizarMeuTelefone)
	// /dois-fatores/*
}

// restantes rotas: bloqueadas para academia que não esteja ativa
protected := router.Group("/")
protected.Use(middleware.AuthMiddleware(), middleware.ValidarStatusAcademia())
// ... (resto do grupo atual)
```

E acrescentar `middleware.ValidarStatusAcademia()` a `jobRoutes` (`/jobs`) e `comunicacaoMensagens` (`/comunicacao/mensagens`). O grupo `financeiro` herda do `protected`.

### 6.3.4 Handlers de estudante (defesa em profundidade)

A *allowlist* do middleware já bloqueia as rotas indevidas para o `inativo`, mas os handlers têm lacunas que convém fechar:

- Criar o helper `estudanteVinculado(status) bool` (`ativo` ou `pendente_documentos`, a regra que o código já repete em `bilhete_identidade_sem_academia_handlers.go:47` e `solicitacao_edicao_dado_estudante_handlers.go:47`) e usá-lo nos dois.
- `SolicitarServicoExtra` (`servico_extra_solicitacao_handlers.go:65`): trocar `est.CodigoAcademia == nil` por `!estudanteVinculado(est.Status)`.
- `CriarSolicitacaoStatusAcademicoHandler` (`academia_status_escolar_handlers.go:87`): `interrupcao` e `desvinculacao` exigem estudante vinculado; `revinculacao` exige `inativo`. Assim os pedidos inválidos deixam de chegar à fila da academia (6.2.5).
- `AlterarSenha` (`profile_handlers.go:130-131`): remover a exigência de estado `ativo` para estudantes (hoje responde "conta inativa. Não é possível alterar a senha." e deixaria presos os `pendente_documentos`).

### 6.3.5 Frontend: eliminar a sessão restrita

O frontend tem uma experiência inteira construída em volta do token restrito. Tudo isto passa a ser eliminado, e o estudante `inativo` vê o painel completo:

| Ficheiro | O que remover |
|---|---|
| `src/lib/api/client.ts:24` e `:378-382` | O campo `acesso_restrito_financeiro` e `tokenStorage.isRestrictedFinance()` |
| `src/components/auth/LoginForm.tsx:49` e `:55` | O ramo da sessão restrita (redirecionamento forçado para `/pagamentos` e a sessão mínima com `status: "inativo"`) |
| `src/components/paineis/EstudantePagamentosPainel.tsx:196` e `:548` | A variável `restricted` e a mensagem "Histórico completo indisponível nesta sessão restrita"; mostrar sempre o histórico completo |
| `src/components/guards/RouteGuard.tsx:44-45`, `src/hooks/useRoutePermission.ts:19` | A passagem de `isRestrictedFinance` |
| `src/app/(painel)/layout.tsx:54`, `src/layout/AppSidebar.tsx:229` | O tratamento especial da sessão restrita (layout e menu lateral reduzido) |
| `src/lib/route-guards.ts:356` e `:365` | O parâmetro `isRestrictedFinance` e a regra "só `/pagamentos`" |

**Acrescentar:** tratar o `403 CONTA_DESVINCULADA` com uma mensagem clara. O frontend quase não ramifica por estado do estudante (só `configuracoes/personalizar`, que já usa "vinculado"), por isso as ações bloqueadas (solicitar serviço extra, solicitações de edição, interrupção, desvinculação) **continuam visíveis** ao `inativo`. Ocultá-las ou desativá-las quando `user.estudante.status === "inativo"`, mantendo visíveis as feitas para ele (bilhete, revinculação, apagar conta, pagamentos).

## 6.4 Testes que impedem regressões

- **Academia inativa:** percorre `router.Routes()` com um JWT de academia `inativa` e exige `401/403` em todas as rotas, exceto numa *allowlist* explícita (as de conta e as públicas). Como o gate corre antes do handler, parâmetros fictícios bastam. Qualquer rota nova acessível a academias sem bloqueio quebra o teste em vez de ficar aberta sem ninguém reparar.
- **Estudante inativo:** percorre as rotas com um JWT de estudante `inativo` e exige `403 CONTA_DESVINCULADA` em toda rota **não-GET** fora da *allowlist* (6.3.2), e que a *allowlist* não seja bloqueada pelo middleware. Cobrir também: o `inativo` consegue pagar (as 3 rotas), trocar a senha, atualizar o bilhete, pedir revinculação, atualizar o telefone do encarregado e **apagar a conta**; não consegue solicitar serviço extra nem abrir `interrupcao`/`desvinculacao`.
- **Combinações de estado:** estudante `ativo`, `pendente_documentos` e `inativo`, cada um com estados académicos `inativo`, `em_andamento` e `finalizado`: todos conseguem consultar e pagar.
- **Estudante `deletado` (SEC-13):** o login por código e por e-mail devolve `401`; o segundo passo do 2FA também; um token emitido antes da exclusão é rejeitado no pedido seguinte.
- **Handlers:** `SolicitarServicoExtra` e `CriarSolicitacaoStatusAcademicoHandler` rejeitam estudante desvinculado mesmo sem o middleware; `AlterarSenha` aceita `pendente_documentos`.
- Remover `auth_financeiro_restrito_test.go` (testa código eliminado).

## 6.5 Vínculos, apagar conta e serviços ao sair (decisões)

Decisões tomadas sobre as três observações da versão anterior, mais uma consequência financeira que a verificação revelou (6.5.4).

### 6.5.1 Academia anterior: acesso apenas ao que ela própria registou

**Regra decidida.** Toda academia que já teve o estudante vinculado pode consultar os dados dele **registados enquanto ele esteve vinculado a essa mesma academia**, e **nunca** os registados na relação dele com outras academias. A regra vale também para a academia atual: ela não vê o que outra academia registou.

**O que o código faz hoje (verificado), e que viola a regra:**

| Leitura | Como autoriza | O que devolve |
|---|---|---|
| `GetNotasEstudante` (`notas_handlers.go:380-387`) | Só a academia do **último vínculo** (`estudante.CodigoAcademia`) | `GetByEstudante(codigo)`: registos de **todas** as academias; só filtra se o chamador passar `codigos_academia` na consulta |
| `GetFaltasEstudante` (`faltas_handlers.go:354-361`), `GetAvaliacoesFinaisEstudante` (`avaliacao_final_handler.go:1059-1067`) | Idem | Idem |
| `GetEstudantePorCodigo` (`estudante_handlers.go:847-937`) | Só academia atual ou admin | Perfil completo: contactos, BI, BI do encarregado, documentos, curso atual |
| `GetEventosEstudante` (`estudante_handlers.go:721`) | `podeAuditarEstudante`: só academia atual | Todos os eventos do estudante, de todas as academias |
| `canAccessEstudanteDocument` | Só academia atual | Documentos atuais do estudante |
| `GetTurmasEstudante` (`turmas_handler.go:178-190`) | Só academia atual | **Já filtra por academia** (`ListByEstudante(codigo, &academia)`) |
| `ConsultarMensalidadesEstudante` (`mensalidade_handlers.go:286-300`), `ConsultarCobrancasEstudante` (`financeiro_handlers.go:332-388`) | `academiaPossuiVinculoMensalidade` (`mensalidade_handlers.go:396`): o `codigo_academia` do estudante **ou** a pertença, atual ou histórica, a uma turma da academia | Filtram as linhas pela academia do chamador (`somenteAcademia`): **já cumprem a regra**, inclusive para academias anteriores |

Consequências: se um estudante muda da academia A para a B, **B lê notas, faltas, avaliações e eventos registados por A** (exposição entre instituições, SEC-15) e **A deixa de poder consultar o que ela própria registou**. A única consulta ao histórico que existe hoje é `estudanteTeveVinculoComAcademia` (`notas_handlers.go:594`, que lê o ledger), usada só para o **estudante** consultar as categorias de nota de uma academia onde já esteve; não serve à academia.

**O que o esquema já permite.** As tabelas ligadas ao estudante guardam o `codigo_academia` em cada registo: `projection_notas`, `projection_faltas`, `projection_avaliacao_final`, `projection_aprovacao_ano`, `projection_reprovacoes`, `projection_materias_pendentes`, `projection_solicitacoes_*` (matrícula, edição, status académico, serviço extra), `projection_turmas` e as tabelas `financeiro_*`. **Filtrar por `codigo_academia = A` basta para isolar os registos de cada academia, sem inventar períodos.** Só não o têm os **documentos** (`Estudante.Documentos`, que guarda a versão atual e substitui a anterior) e os **eventos** (que têm `occurred_at`).

**Decisão sobre o `codigo_academia` que fica no estudante: manter, com um novo significado explícito.** O campo continua a ser "a academia do último vínculo" e **não** é limpo na desvinculação. Razões: tem 41 usos em 17 ficheiros (reintegração, finanças, histórico), a reintegração precisa do curso e do ano anteriores, e `ConsultarMensalidadesEstudante` escopa por ele. A alternativa de o anular ao desvincular quebraria tudo isso. Em vez disso:

1. Passa a existir a noção explícita de **academia atual**: `academiaAtual(est)` devolve o código só se `estudanteVinculado(est.Status)` (`ativo` ou `pendente_documentos`); caso contrário, nada.
2. Passa a existir um **histórico de vínculos** construído a partir dos eventos.

**Migration `136_estudante_vinculos.sql`:**

```sql
BEGIN;
CREATE TABLE IF NOT EXISTS projection_estudante_vinculos (
    estudante_id     UUID        NOT NULL,
    codigo_estudante VARCHAR(20) NOT NULL,
    codigo_academia  VARCHAR(50) NOT NULL,
    vinculado_em     TIMESTAMPTZ NOT NULL,
    desvinculado_em  TIMESTAMPTZ,            -- NULL = vínculo em curso
    PRIMARY KEY (estudante_id, codigo_academia, vinculado_em)
);
CREATE INDEX IF NOT EXISTS idx_vinculos_academia_estudante
    ON projection_estudante_vinculos (codigo_academia, codigo_estudante);
CREATE INDEX IF NOT EXISTS idx_vinculos_estudante
    ON projection_estudante_vinculos (estudante_id);
COMMIT;
```

**Alimentação** (na projeção do estudante, que já recebe estes eventos; `estudante_projection.go:82-87`): `EstudanteCriadoComVinculo` e `EstudanteReintegrado` abrem um período (`vinculado_em = occurred_at`, `desvinculado_em` nulo); `EstudanteDesvinculadoDaAcademia` e `EstudanteDeletado` fecham o período aberto (`UPDATE … SET desvinculado_em = occurred_at WHERE estudante_id = $1 AND codigo_academia = $2 AND desvinculado_em IS NULL`). **Preenchimento do passado:** reconstruir a projeção de estudantes (`Manager.RebuildProjection`), mecanismo que o projeto já usa; a reconstrução parte do ledger, onde os três eventos trazem `CodigoAcademia`.

**Um único helper de acesso**, que substitui as verificações `estudante.CodigoAcademia != academia` e `estudanteTeveVinculoComAcademia`:

```go
type AcessoAcademia int
const (
	SemAcesso      AcessoAcademia = iota
	AcessoAnterior                // já teve vínculo, hoje não tem (ou tem com outra academia)
	AcessoAtual                   // vinculado agora a esta academia
)

func acessoAcademiaAoEstudante(c *gin.Context, codigoAcademia string, est *projections.EstudanteDTO) (AcessoAcademia, []PeriodoVinculo, error) {
	periodos, err := getEstudanteVinculosProjection(c).Listar(est.ID, codigoAcademia)
	if err != nil {
		return SemAcesso, nil, err
	}
	if len(periodos) == 0 {
		return SemAcesso, nil, nil
	}
	if est.CodigoAcademia != nil && *est.CodigoAcademia == codigoAcademia && estudanteVinculado(est.Status) {
		return AcessoAtual, periodos, nil
	}
	return AcessoAnterior, periodos, nil
}
```

O financeiro já cumpre a regra com o seu critério próprio (`academiaPossuiVinculoMensalidade`) e pode manter-se; convém convergir para o helper único num segundo momento, para não haver dois critérios de vínculo.

**O que cada tipo de acesso permite (academia):**

| Recurso | `AcessoAtual` | `AcessoAnterior` |
|---|---|---|
| Perfil (`GetEstudantePorCodigo`) | Completo, como hoje | **Reduzido:** nome, código, estado do vínculo e períodos de vínculo (`vinculado_em`/`desvinculado_em`). Sem contactos, BI, documentos, curso atual nem dados de outras academias |
| Notas, faltas, avaliações finais, aprovações, reprovações, matérias pendentes | Só registos com `codigo_academia = A` | Só registos com `codigo_academia = A` |
| Turmas, mensalidades, cobranças, solicitações, inscrições em serviços | Só as da academia A (já assim, ou por filtro) | Só as da academia A |
| Documentos do estudante (`Documentos`) | Sim | **Não** (são a versão atual e não têm período; os documentos de uma solicitação continuam acessíveis à academia que a recebeu, via `canAccessSolicitacaoDocument`) |
| Eventos (`GetEventosEstudante`, `GetEventoAuditoria`) | Só eventos cujo `occurred_at` cai nos períodos de A (inclusive os limites), com hashes redigidos (SEC-14) | Idem |
| `VerificarIntegridade` | Sim | Não (percorre a cadeia inteira do estudante, de todas as academias) |
| **Escrita** (lançar nota, alterar dados, apagar conta…) | Como hoje | **Nenhuma**: a academia anterior só lê |
| Lista `/estudantes` | `vinculo: "atual"` | `vinculo: "anterior"` |

**Filtro forçado** nos três handlers que hoje não filtram (exemplo em `GetNotasEstudante`; igual para faltas e avaliações finais). O estudante, que consulta os **seus** dados, continua a ver tudo:

```go
if userType == "academia" {
	acesso, _, err := acessoAcademiaAoEstudante(c, academiaDTO.CodigoAcademia, estudante)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if acesso == SemAcesso {
		utils.RespondWithForbiddenError(c, "Estudante não pertence nem pertenceu a esta academia")
		return
	}
	// … carregar registos e interpretar os filtros da consulta …
	filtros.codigosAcademia = []string{academiaDTO.CodigoAcademia} // FORÇADO: ignora o que vier na consulta
}
```

**Eventos:** `filtrarEventosPorPeriodos(eventos, periodos)` mantém os eventos com `occurred_at >= vinculado_em` e (`desvinculado_em` nulo ou `occurred_at <= desvinculado_em`). A lista `/estudantes` ganha o parâmetro `vinculo=atual|anterior|todos` (por omissão `todos`, que reproduz o comportamento de hoje) e cada linha traz `vinculo`.

**Frontend:** em `/estudantes`, filtro "Vínculo" e identificação do ex-estudante; ao abrir um ex-estudante, a vista reduzida, sem separadores de contactos, documentos nem ações de escrita.

**Testes:** estudante que muda de A para B (B não vê registos de A; A continua a ver os seus; nenhuma vê os da outra); `codigos_academia=outra` na consulta é ignorado; academia que nunca teve o estudante recebe `403`; perfil reduzido não contém `email`, `telefone` nem `documentos`; eventos fora do período não aparecem; documentos negados à academia anterior; reconstrução da projeção reproduz os períodos.

### 6.5.2 Apagar a conta: permitido, com aviso no frontend

**Decisão:** continua permitido. O estudante só pode apagar a própria conta quando está `inativo` (regra do domínio); a academia só apaga estudantes `ativo` ou `pendente_documentos`. **Nenhuma regra de backend muda.** O que falta é o aviso, que hoje não existe em nenhum dos dois fluxos:

| Fluxo | Onde | Texto atual |
|---|---|---|
| Estudante | `src/app/(painel)/perfil/PageContent.tsx:84-103` (`DangerZoneEstudante`) | "A deleção desativa sua conta. O histórico acadêmico de notas e faltas permanece preservado… mas você não conseguirá mais entrar no painel." |
| Academia | `src/app/(painel)/estudantes/PageContent.tsx:673-693` | "A conta de {nome} será desativada. Registros acadêmicos já lançados continuarão guardados." |

O texto do estudante já promete que ele "não conseguirá mais entrar no painel", o que só passa a ser verdade com a correção do SEC-13.

**Aviso a acrescentar (estudante):** antes do botão de confirmar, um alerta com: *"Ao apagar a conta deixa de poder entrar no painel e, por isso, de consultar ou pagar as suas dívidas. As dívidas em aberto continuam registadas na academia {nome}. Os registos académicos ficam guardados. Esta ação não pode ser desfeita."* Se houver dívidas pendentes, mostrar o número e o total e exigir uma caixa de confirmação ("Compreendo que continuo a dever estes valores") para ativar o botão. Os dados vêm dos endpoints de consulta que o painel de pagamentos já usa (`/financeiro/mensalidades/estudante/…` para as mensalidades e, para os serviços extras, `ListarMinhasInscricoesServicoExtra` e `MinhasPendenciasServicoExtra`); não é preciso endpoint novo.

**Aviso a acrescentar (academia):** *"O estudante deixa de poder entrar e de pagar. As inscrições ativas em serviços extras serão canceladas. As dívidas em aberto continuam registadas."*, com o número e o total de mensalidades pendentes, obtidos pelo mesmo endpoint de consulta (`ConsultarMensalidadesEstudante`, que a academia já usa).

**O que acontece às dívidas ao apagar:** continuam registadas e visíveis à academia (o estudante apagado mantém o `codigo_academia`, e a academia mantém o acesso do 6.5.1). O estudante é que deixa de poder pagá-las. É consequência direta da decisão tomada, por isso o aviso é obrigatório.

### 6.5.3 Serviços extras: catálogo só da academia atual e desconexão ao sair

**Catálogo.** `ListarServicosExtrasCatalogoEstudante` (`servico_extra_handlers.go:504`) usa `*est.CodigoAcademia`, que nunca é nulo, e por isso mostra o catálogo da academia anterior a quem está desvinculado. Correção:

```go
if e != nil || est == nil {
	utils.RespondWithForbiddenError(c, "estudante não encontrado")
	return
}
if !estudanteVinculado(est.Status) { // desvinculado ou apagado: não mostrar nada
	c.JSON(http.StatusOK, gin.H{"servicos_extras": []any{}, "categorias": []any{}})
	return
}
// vinculado: catálogo da academia atual (*est.CodigoAcademia)
```

Devolve `200` com listas vazias (e não `403`), para a interface mostrar "sem serviços disponíveis" em vez de um erro.

**Desconexão ao sair.** **Verificado: hoje nada cancela as inscrições em serviços extras** quando o estudante é desvinculado ou apagado. Nem `decidirSolicitacaoStatus` (desvinculação), nem `DeletarPorAcademia`, nem `Deletar` as tocam: ficam `pendente`, `aprovada_pendente_pagamento_taxa_inscricao` ou `vinculada` para sempre. O domínio já tem os métodos de cancelamento (`CancelarAntesDaVinculacao`, `Cancelar`); falta chamá-los.

O helper atual, `cancelarSolicitacaoServicoExtra` (`servico_extra_solicitacao_handlers.go:293`), **escreve a resposta HTTP dentro dele**, por isso é dividido em núcleo reutilizável e wrapper:

```go
func cancelarSolicitacaoServicoExtraCore(ctx context.Context, repo *db.AggregateRepository,
	s *aggregates.SolicitacaoServicoExtra, motivo, por, actor, ip string) error {
	var e error
	switch s.Status {
	case aggregates.StatusInscricaoPendente:
		e = s.CancelarAntesDaVinculacao(motivo, por)
	case aggregates.StatusInscricaoAprovadaPendentePagamentoTaxa:
		// cancela primeiro a cobrança aberta da taxa de inscrição
		if e = FinanceiroService.CancelarCobrancaTaxaInscricaoServicoAberta(ctx, s.GetID().String(), motivo, actor, por, ip); e == nil {
			e = s.CancelarAntesDaVinculacao(motivo, por)
		}
	case aggregates.StatusInscricaoVinculada:
		e = s.Cancelar(motivo, por)
	default:
		return fmt.Errorf("solicitação em estado '%s' não pode ser cancelada", s.Status)
	}
	if e != nil {
		return e
	}
	return repo.SaveWithAudit(s, db.AuditContext{UserID: actor, UserType: por, IP: ip})
}
```

E a função de encerramento, idempotente (só toca em estados ativos) e que **nunca bloqueia a saída**:

```go
type resumoEncerramento struct {
	Encerradas int      `json:"encerradas"`
	Falhas     []string `json:"falhas,omitempty"`
}

func encerrarServicosExtrasDoEstudante(c *gin.Context, codigoEstudante, codigoAcademia, motivo string) resumoEncerramento {
	// SELECT id FROM projection_solicitacoes_servico_extra
	//  WHERE codigo_estudante=$1 AND codigo_academia=$2
	//    AND status IN ('pendente','aprovada_pendente_pagamento_taxa_inscricao','vinculada')
	// Para cada id: Load("SolicitacaoServicoExtra") -> cancelarSolicitacaoServicoExtraCore(
	//     ctx, repo, s, motivo, "academia", "sistema:encerramento-vinculo", c.ClientIP())
}
```

`por` é `"academia"` porque a coluna `cancelada_por` tem uma restrição `CHECK` que só aceita `'academia'` e `'estudante'`; o ator `sistema:encerramento-vinculo` fica na auditoria. (Alternativa: acrescentar `"sistema"` ao domínio e a uma migration; não necessário.)

| Chamador | Quando | Motivo |
|---|---|---|
| `decidirSolicitacaoStatus` (`academia_status_escolar_handlers.go:286-330`) | Logo depois de gravar a decisão de `desvinculacao` | `estudante_desvinculado` |
| `DeletarContaEstudantePorAcademia` (`estudante_delecao_academia_handlers.go:71`) | Logo depois de `DeletarPorAcademia` | `conta_deletada` |
| `DeletarContaEstudante` (`estudante_handlers.go`, autoexclusão) | Defensivo: o estudante já está `inativo`, mas a função é idempotente | `conta_deletada` |

O resumo (`encerradas`, `falhas`) vai na resposta de cada uma. Uma falha numa inscrição não impede as restantes nem a saída; como a função só toca em estados ativos, pode ser repetida.

**O que NÃO se cancela:** as dívidas já geradas (taxas de inscrição e obrigações por pagar) **continuam registadas e pagáveis** (D4: o estudante `inativo` paga). As inscrições canceladas ficam no histórico do estudante (`cancelada` ou `cancelada_antes_da_vinculacao`), que continua visível. A regra do projeto "desvinculação não remove notas, faltas, avaliações, documentos, turmas ou eventos" (tarefa 05, linha 250) mantém-se; passa a existir uma exceção documentada: **as inscrições em serviços extras são encerradas**.

**Testes:** desvincular encerra as inscrições `pendente`, `aprovada_pendente_pagamento_taxa_inscricao` (cancelando a cobrança aberta) e `vinculada`; apagar pela academia faz o mesmo; as dívidas continuam pagáveis; o catálogo devolve listas vazias a `inativo` e `deletado`; o catálogo de quem está vinculado mostra só a academia atual; uma falha numa inscrição não impede as outras.

### 6.5.4 Turmas e mensalidades depois da saída (observação nova, com padrão assumido)

Ao verificar o que "desconectar" implica, vi que as **mensalidades** não são geradas por uma lista de estudantes: `ListMensalidades` (`internal/finance/mensalidade.go:403`) e `vinculosMensalidade` (`:681`) derivam-nas da **pertença do estudante a turmas** (`projection_turmas.estudantes` e `historico_estudantes_ano_letivo`). A regra do projeto diz que desvincular **não remove das turmas**, e nenhum código as remove ao sair. Consequência: os meses que faltam do ano letivo em curso **continuam a aparecer como `pendente`** para um estudante que já saiu.

**Padrão assumido (reversível, sem alterações automáticas de dinheiro):**
- **Não** remover o estudante das turmas: como as mensalidades derivam delas, isso faria **desaparecer das listas as dívidas já existentes**, em conflito com D4 (pagar as suas dívidas).
- **Não** anular obrigações automaticamente. Depois de uma desvinculação ou exclusão, a ficha do estudante na academia mostra um aviso, *"Mensalidades futuras pendentes: N"*, com a ação existente de anular (`AnularObrigacoesMensalidade`: só meses ainda não pagos, e reversível com `ReativarObrigacoesMensalidade`).

Se preferir que os meses posteriores à saída sejam anulados automaticamente, a função de encerramento da 6.5.3 pode chamar `AnularObrigacoesMensalidade` para os meses seguintes ao da saída; não o incluí por ser uma decisão financeira, com casos limite (pagamento em curso, mês parcial).

---

# Parte 7 — Rate limiters: o que eliminar (D1)

| Local | Ação |
|---|---|
| `internal/middleware/rate_limit.go` | Apagar as funções `GlobalRateLimit`, `LoginRateLimit` e `EmailRateLimit` (e os comentários que as descrevem). **Manter** `RateLimiter`, `NewRateLimiter` e `RateLimitMiddleware` |
| `cmd/server/main.go:286` | Remover `router.Use(middleware.GlobalRateLimit())` |
| `cmd/server/main.go:308, 311, 312, 313` | Remover `middleware.LoginRateLimit()` de `/login`, `/login/dois-fatores/verificar`, `/login/dois-fatores/reenviar` e `/bootstrap` |
| `cmd/server/main.go:333` | Remover `emailGroup.Use(middleware.EmailRateLimit())` |
| `.env.example:179-182` | Remover o comentário e `RATE_LIMIT_GLOBAL`, `RATE_LIMIT_LOGIN`, `RATE_LIMIT_EMAIL` (nenhum código Go as lê) |
| `Documentação da API.md:657, 1106` | Remover as frases "Rate limit ativo" |
| `docs/Tarefas feitas/122 - …md` | Rever as menções ao `LoginRateLimit` |

Nenhum teste referencia as três funções. **Efeito:** nenhum na execução (eram no-ops); o risco residual está descrito no SEC-02.

---

# Parte 8 — Ordem de execução e verificação

1. **Correções imediatas, independentes do resto:** SEC-13 (bloquear estudante `deletado`), SEC-14 (redigir hashes nos endpoints de eventos) e a metade barata do SEC-15 (forçar o filtro por `codigo_academia` em notas, faltas e avaliações finais).
2. **Pré-requisitos de produção:** confirmar `EMAIL_FROM`, provedor padrão de SMS e remetente configurados, `FRONTEND_URL` correto.
3. **Base:** migration 135, evento `SenhaTemporariaDefinida` (+ `validEventTypes`), colunas e imposição no `AuthMiddleware`, `GenerateTempPassword`, extração de `enviarSMSComunicacao`.
4. **Estado da conta (6.1 a 6.4):** reorganizar grupos e aplicar `ValidarStatusAcademia`; implementar a regra por método do estudante `inativo` e eliminar o token restrito (backend e frontend); escrever os testes de rotas **antes** de relaxar o login.
5. **Vínculos (6.5.1):** migration 136, alimentação da projeção, reconstrução da projeção de estudantes, helper `acessoAcademiaAoEstudante`, perfil reduzido, filtro de eventos, parâmetro `vinculo` na lista, e o frontend correspondente.
6. **Serviços ao sair e aviso de apagar conta (6.5.2 e 6.5.3):** dividir o cancelamento em núcleo e wrapper, função de encerramento e os três chamadores, correção do catálogo, avisos nos dois fluxos de apagar conta.
7. **Criação por terceiros (Parte 3):** os 5 pontos (com o envio por *pool* nos lotes), `RegisterAcademia` com telefone obrigatório, admin por Brevo, reenvio de credenciais.
8. **Recuperação por link (Parte 4)** e página do frontend.
9. **Eliminações:** Parte 2 (EmailJS, SMTP, `gerar-token/*`, `GetDefaultPassword`, `GenerateSecurePassword`) e Parte 7 (rate limiters).
10. **Senhas públicas (Parte 5):** limpar a árvore, redefinir admins afetados (3.9), reescrever o histórico.
11. **Contas existentes (3.9)** e, só depois, remover as variáveis antigas e **revogar** as credenciais do EmailJS/SMTP.

**Verificação final** (os `grep` devem terminar sem resultados, exceto em `docs/`):

```bash
# backend
go build ./... && go vet ./... && go test ./...
grep -rniE "emailjs|net/smtp|EMAIL_HOST|EMAIL_PASS|EMAIL_PORT|sendSMTP|loadSMTP|gerar-token|GetDefaultPassword|GenerateSecurePassword" \
  --include=*.go --include=.env.example --include=Dockerfile .
grep -rnE "GlobalRateLimit|LoginRateLimit|EmailRateLimit|RATE_LIMIT_(GLOBAL|LOGIN|EMAIL)" \
  --include=*.go --include=.env.example .
grep -rnE "AcessoRestritoFinanceiro|GenerateRestrictedFinanceToken|rotaFinanceiraRestrita|acesso_restrito" --include=*.go .
grep -rnE "spuriadm|spurifpp|spurigerente|spuri123" . --exclude-dir=.git

# frontend
npx tsc --noEmit && npm run build
grep -rniE "emailjs|nodemailer|EMAIL_HOST|EMAIL_PASS|gerar-token|spuriadm|spurifpp|spurigerente|spuri123|isRestrictedFinance|acesso_restrito_financeiro" src --include=*.ts --include=*.tsx

# histórico (após o filter-repo)
git log --all -S"spuriadm" --oneline        # deve ficar vazio
```

**Padrões assumidos (já refletidos no documento; ajustáveis sem refazer o plano):**

1. **Reenvio de credenciais (3.8):** academia dona → seus estudantes; `fpp` → estudantes, academias e admins; `adm` e `gerente` sem permissão.
2. **Estudante `inativo` (6.1 e 6.3.2):** consulta tudo e paga, em qualquer combinação de estado geral e estados académicos; escreve apenas na *allowlist* (conta, pagamentos, bilhete, revinculação, telefone do encarregado e **apagar a conta**). Para liberar mais uma ação, acrescentar a linha à *allowlist*.
3. **Validade da senha temporária (3.12):** não incluída; é opcional.
4. **Contas existentes (3.9):** o passo 2 (redefinir e enviar SMS em massa) só deve correr depois de aprovado o custo, a partir da contagem do *dry-run*.
5. **Academia anterior (6.5.1):** consulta apenas o que ela registou durante o seu vínculo (decidido). Perfil reduzido, sem documentos e sem escrita; o `codigo_academia` do estudante **não é limpo** (significa "academia do último vínculo").
6. **Apagar conta (6.5.2):** permitido, com os avisos descritos; nenhuma regra de backend muda.
7. **Serviços ao sair (6.5.3):** catálogo só da academia atual; ao desvincular ou apagar, as inscrições ativas em serviços extras são canceladas. As dívidas geradas continuam pagáveis.
8. **Turmas e mensalidades depois da saída (6.5.4):** sem alterações automáticas de dinheiro nem remoção das turmas; a academia vê um aviso e decide anular os meses futuros.
