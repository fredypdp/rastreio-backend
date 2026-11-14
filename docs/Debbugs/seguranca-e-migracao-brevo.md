---
modificado: 2026-11-12 21:00
criado: 2026-11-04 0:12
---
# Spuri â€” SeguranÃ§a, credenciais temporÃ¡rias e e-mail 100 % Brevo

**VersÃ£o final Â· 31/10/2026**
**RepositÃ³rios (branch `main`):** `fredypdp/rastreio-backend` @ `4759474` Â· `fredypdp/rastreio-frontend` @ `77ef4cf`

---

## 0. Escopo, limites e o que mudou desde a versÃ£o 1

Esta versÃ£o foi refeita sobre o estado atual dos dois repositÃ³rios, que avanÃ§aram (tarefa 122, verificaÃ§Ã£o em duas etapas por e-mail). Todos os nÃºmeros de linha referem-se a esses commits.

**RevisÃ£o do 2FA (tarefa 122).** Usa apenas Brevo e define `ErrEmailNaoConfigurado`, que este plano reutiliza. Li os handlers por inteiro (login, reenvio, ativaÃ§Ã£o, desativaÃ§Ã£o) e o serviÃ§o nos pontos de seguranÃ§a: cÃ³digo de 6 dÃ­gitos com `crypto/rand`, HMAC-SHA256, `subtle.ConstantTimeCompare`, verificaÃ§Ã£o com `FOR UPDATE`, mÃ¡ximo de 5 tentativas por desafio dentro da transaÃ§Ã£o, reenvio a cada 60 s, 5 envios por hora e uso Ãºnico. **Sem achados.** Dois pontos de integraÃ§Ã£o com este plano: `avaliarStatusLogin` Ã© chamado tambÃ©m no segundo passo do 2FA (6.3.1), e o limitador de senhas em memÃ³ria do 2FA sÃ³ Ã© usado ao confirmar a senha nos fluxos do prÃ³prio 2FA (`dois_fatores_handlers.go:546` e `:608`), nÃ£o no login. O 2FA tambÃ©m introduz dois padrÃµes que este plano copia: a convenÃ§Ã£o `ENV=production` e a flag de desenvolvimento `DOIS_FATORES_LOG_CODIGO` (ignorada em produÃ§Ã£o).

**Limites.** As conclusÃµes vÃªm da **leitura do cÃ³digo**: nÃ£o executei o cÃ³digo nem os testes, nÃ£o vejo a base de dados nem as variÃ¡veis de produÃ§Ã£o, e o frontend foi analisado sÃ³ neste repositÃ³rio (outros clientes da API, se existirem, nÃ£o). O histÃ³rico git dos dois repositÃ³rios foi analisado (SEC-05).

### Cobertura da auditoria

| Ãrea | O que foi verificado e como | Onde |
|---|---|---|
| Envio de e-mail (Brevo, SMTP, EmailJS) | Leitura integral de `services/email_*`, `brevo_mailer.go`, `smtp_mailer.go`, handlers e rotas; busca em todo o cÃ³digo | Partes 1 e 2 |
| 2FA (tarefa 122) | Handlers por inteiro; serviÃ§o nos pontos de seguranÃ§a | SecÃ§Ã£o 0 |
| CriaÃ§Ã£o de contas e senhas | Os 6 pontos de criaÃ§Ã£o/reset, lidos em contexto | Parte 3 |
| MÃ³dulo de SMS | Handlers, clientes Ziett/GoSMS (timeout de 15 s), agregado e esquema da mensagem | 3.4 |
| Estados do estudante (geral e acadÃ©micos) | Todas as atribuiÃ§Ãµes a `Status`, `StatusEscolar*` e `StatusSuperior` no agregado; projeÃ§Ã£o; login; middleware; 2FA | 6.1, SEC-13 |
| Leituras do estudante | 28 handlers `GET`, um a um | 6.2.3, SEC-14 |
| Pagamentos do estudante | 5 handlers financeiros e o serviÃ§o `internal/finance` | 6.2.4 |
| Escritas do estudante | 23 rotas | 6.2.2 |
| AprovaÃ§Ã£o de pedidos de status | Handler e regras no agregado | 6.2.5 |
| Escopo por academia | Varredura automÃ¡tica de 92 rotas com parÃ¢metro e verificaÃ§Ã£o manual das 21 sinalizadas; as restantes 71 por padrÃ£o, **nÃ£o linha a linha** | 6.2.1 |
| Isolamento dos dados entre academias | Os 6 handlers de leitura por estudante, o esquema (tabelas que guardam `codigo_academia`), os eventos de vÃ­nculo, o ledger e o mecanismo de reconstruÃ§Ã£o de projeÃ§Ãµes | 6.5.1, SEC-15 |
| Frontend | Busca em todo `src/` por estados, por e-mail e pelo token restrito; leitura dos ficheiros afetados | 2.2, 3.10, 6.3.5 |
| HistÃ³rico git | Varredura de segredos e das senhas padrÃ£o nos dois repositÃ³rios | SEC-05 |

---

## 1. DecisÃµes adotadas nesta versÃ£o

| #   | DecisÃ£o                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | Onde estÃ¡ detalhada                                                                                             |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| D1  | **Eliminar** os trÃªs rate limiters desativados (`GlobalRateLimit`, `LoginRateLimit`, `EmailRateLimit`)                                                                                                                                                                                                                                                                                                                                                                                                                                | Parte 7                                                                                                         |
| D2  | **Parar de usar senhas padrÃ£o.** Toda conta criada por terceiros recebe uma senha temporÃ¡ria aleatÃ³ria de **8 caracteres alfanumÃ©ricos** (8 Ã© o mÃ­nimo permitido: `ValidateSenha` exige 8 a 128), com **troca obrigatÃ³ria** no primeiro acesso                                                                                                                                                                                                                                                                                        | Parte 3                                                                                                         |
| D3  | **Entrega das credenciais:** estudante e academia â†’ **SMS**, pela funÃ§Ã£o padronizada de envio do mÃ³dulo de comunicaÃ§Ã£o, **sÃ³ em `ENV=production`**. **Administrador â†’ apenas e-mail (Brevo)**                                                                                                                                                                                                                                                                                                                                         | Parte 3                                                                                                         |
| D4  | **Estudante e academia podem fazer login em qualquer estado da conta** (exceto `deletado`, nos dois). Admin continua a exigir `ativo`. O estudante **consulta tudo o que o envolve e paga as suas contas em qualquer estado geral (`ativo`, `pendente_documentos`, `inativo`) e em qualquer estado acadÃ©mico** (`inativo`, `em_andamento`, `finalizado` em fundamental, mÃ©dio e superior). O `inativo` (desvinculado) passa a ter token normal; as demais escritas dele ficam bloqueadas por padrÃ£o. O token restrito Ã© eliminado     | Parte 6 (a verificaÃ§Ã£o pedida **nÃ£o confirma a premissa por completo**, nem para academias nem para estudantes) |
| D5  | **RecuperaÃ§Ã£o de senha por link (opÃ§Ã£o B):** o utilizador define a prÃ³pria senha via link Brevo; nenhuma senha viaja por e-mail                                                                                                                                                                                                                                                                                                                                                                                                       | Parte 4                                                                                                         |
| D6  | **Remover por completo** as senhas padrÃ£o de admin (Ã¡rvore atual e histÃ³rico git)                                                                                                                                                                                                                                                                                                                                                                                                                                                     | Parte 5                                                                                                         |
| D7  | Eliminar tudo o que envia e-mail fora do Brevo (EmailJS, SMTP, `gerar-token/*`)                                                                                                                                                                                                                                                                                                                                                                                                                                                       | Parte 2                                                                                                         |
| D8  | **Acesso por vÃ­nculo:** toda academia que jÃ¡ teve o estudante consulta apenas o que **ela prÃ³pria registou** enquanto ele esteve vinculado a ela, nunca os registos de outras academias; vale tambÃ©m para a academia atual. A academia anterior sÃ³ lÃª                                                                                                                                                                                                                                                                                 | 6.5.1                                                                                                           |
| D9  | **Apagar a conta** continua permitido, com **aviso no frontend** (perda de acesso e dÃ­vidas que ficam)                                                                                                                                                                                                                                                                                                                                                                                                                                | 6.5.2                                                                                                           |
| D10 | **ServiÃ§os ao sair:** o catÃ¡logo mostra sÃ³ o da academia atual (vazio sem vÃ­nculo); ao ser desvinculado ou apagado, o estudante Ã© **desconectado de todos os serviÃ§os extras** dessa academia.<br><br>Nota: para os casos em que ele foi desvinculado de uma academia mas ainda nÃ£o pagou um serviÃ§o e a academia nÃ£o anulou essa cobranÃ§a, deve existir um mecanismo que permita que a cobranÃ§a continue sendo feita (e permitir que o pagamento seja feito corretamente) mesmo o estudante jÃ¡ nÃ£o estando vinculado Ã  essa academia | 6.5.3                                                                                                           |

---

# Parte 1 â€” DiagnÃ³stico de seguranÃ§a (resolvido)

> **Estado desta parte (14/11/2026):** SEC-01 a SEC-15 estÃ£o **resolvidos**. SEC-01 a SEC-07 pela Tarefa 123 do `rastreio-backend` e Tarefa 34 do `rastreio-frontend`. SEC-08 a SEC-15 pela Tarefa 124 do `rastreio-backend` (este repositÃ³rio). O mecanismo `sessoes_validas_desde` de **SEC-10** foi entregue como dependÃªncia do reenvio de credenciais (reset por link e reenvio). **Partes 6 a 8 (implantaÃ§Ã£o completa de D4, vÃ­nculos, serviÃ§os ao sair, etc.) continuam pendentes** â€” esta parte cobre apenas o diagnÃ³stico e as correÃ§Ãµes de backend jÃ¡ aplicadas.
> O tÃ­tulo "(resolvido)" foi aplicado a pedido; leia-o como "SEC-01 a SEC-15 resolvidos no backend".
>
> **Partes 2 e 4 tambÃ©m concluÃ­das (14/11/2026):**
> - **Parte 2 (eliminar EmailJS/SMTP/gerar-token):** âœ… backend (Tarefa 123 `rastreio-backend`) + âœ… frontend (Tarefa 35 `rastreio-frontend`).
> - **Parte 4 (recuperaÃ§Ã£o por link, opÃ§Ã£o B):** âœ… backend (Tarefa 123 `rastreio-backend`) + âœ… frontend (jÃ¡ usa `emailAuthService`).

| ID | Severidade | Problema | DecisÃ£o / plano |
|---|---|---|---|
| SEC-01 | **CrÃ­tica** | Tomada de conta sem autenticaÃ§Ã£o via `gerar-token/recuperacao` | Eliminar rotas (Parte 2) â€” âœ… **resolvido** (Tarefa 123 / 34) |
| SEC-02 | Alta | Rate limiters sÃ£o no-ops | **Eliminar o cÃ³digo morto** (D1, Parte 7), com risco residual declarado â€” âœ… **resolvido** (Tarefa 123 / 34) |
| SEC-03 | Alta | Reset de senha sem prova de posse do e-mail | OpÃ§Ã£o B (D5, Parte 4) â€” âœ… **resolvido** (Tarefa 123 / 34) |
| SEC-04 | Alta | Senha inicial = cÃ³digo do utilizador, sem troca obrigatÃ³ria | Senhas temporÃ¡rias (D2/D3, Parte 3) â€” âœ… **resolvido** (Tarefa 123 / 34) |
| SEC-05 | MÃ©dia | Senhas padrÃ£o de admin em repositÃ³rios pÃºblicos (Ã¡rvore **e histÃ³rico**) | Remover tudo (D6, Parte 5) â€” âœ… **resolvido** (Tarefa 123 / 34) |
| SEC-06 | MÃ©dia | EnumeraÃ§Ã£o de contas (recuperaÃ§Ã£o **e login**) | Respostas genÃ©ricas â€” âœ… **resolvido** (Tarefa 123 / 34) |
| SEC-07 | MÃ©dia | Segredos gravados nos logs | Falhar fechado, sem logs de segredos â€” âœ… **resolvido** (Tarefa 123 / 34) |
| SEC-08 | MÃ©dia | AmplificaÃ§Ã£o de e-mails pelo cadastro pÃºblico de academias | Envio assÃ­ncrono e deduplicado â€” âœ… **resolvido** (Tarefa 124: `SendBrevoEmailWithBCC`, goroutine) |
| SEC-09 | Baixa | Ciclo de vida frÃ¡gil dos tokens (`auth_tokens`) | Consumo atÃ³mico, hash SHA-256 â€” âœ… **resolvido** (Tarefa 124: `SaveToken`/`VerifyToken` atÃ´micos) |
| SEC-10 | MÃ©dia | SessÃµes JWT nÃ£o sÃ£o revogadas apÃ³s reset de senha | `sessoes_validas_desde` â€” âœ… **resolvido** (Tarefa 123, coluna + middleware) |
| SEC-11 | MÃ©dia | `POST /bootstrap` pÃºblico, sem segredo de instalaÃ§Ã£o nem validaÃ§Ã£o de senha | `BOOTSTRAP_TOKEN` â€” âœ… **resolvido** (Tarefa 124: header obrigatÃ³rio) |
| SEC-12 | **Alta** | Permitir login em qualquer estado deixa rotas sem bloqueio prÃ³prio | ReorganizaÃ§Ã£o rotas, regra por mÃ©todo, elimina token restrito â€” âœ… **resolvido** (Tarefa 124: `AuthMiddleware`, `ValidarStatusAcademia`, `rotaPermitidaEstudanteInativo`) |
| SEC-13 | **Alta** | Estudante com conta apagada (`deletado`) continua a conseguir entrar | Bloquear no login, middleware e `GetAuthByIdentificador` â€” âœ… **resolvido** (Tarefa 124: filtro `status <> 'deletado'`) |
| SEC-14 | MÃ©dia | Hashes de senha expostos nos endpoints de eventos | Redigir o payload â€” âœ… **resolvido** (Tarefa 124: `redigirEventoSensivel` em `GetEventosEstudante`/`GetEventoAuditoria`) |
| SEC-15 | MÃ©dia | A academia atual lÃª registos de outras academias; a academia anterior perde o acesso | VÃ­nculos por perÃ­odo + filtro forÃ§ado â€” âœ… **resolvido** (Tarefa 124: `projection_estudante_vinculos`, `acessoAcademiaAoEstudante`, filtro forÃ§ado em 3 handlers) |

---

## SEC-01 â€” Tomada de conta sem autenticaÃ§Ã£o (CrÃ­tica) â€” âœ… resolvido

**Onde:** `cmd/server/main.go:339` e `:344`; `internal/handlers/auth_email_handlers.go` (`GerarTokenRecuperacao`, linhas 247-327).

**O que acontece.** `POST /email/gerar-token/recuperacao` Ã© pÃºblica (a API doc diz "ProteÃ§Ã£o: pÃºblica") e **devolve o token de recuperaÃ§Ã£o no corpo da resposta**. Esse token Ã© aceito por `POST /email/recuperar-senha/:token`, tambÃ©m pÃºblica, que define a nova senha. O e-mail do titular nunca participa.

**Cadeia de ataque (2 pedidos):**
1. `POST /email/gerar-token/recuperacao` com o `codigo_estudante`, o cÃ³digo da academia ou o e-mail de um admin â†’ resposta com `token`.
2. `POST /email/recuperar-senha/<token>` com `nova_senha` â†’ a conta passa a ser do atacante.

**PrÃ©-condiÃ§Ã£o:** conta com e-mail verificado e identificador conhecido. O cÃ³digo de estudante Ã© o login e circula em documentos.

**Impacto:** tomada de qualquer conta elegÃ­vel, incluindo admins `fpp`/`adm`.

**Estado no frontend:** nenhuma pÃ¡gina chama estas rotas; sÃ³ existem *wrappers* mortos em `src/lib/api/services.ts`. SÃ£o superfÃ­cie de ataque sem utilizador legÃ­timo (neste frontend; outros clientes nÃ£o foram verificados).

**CorreÃ§Ã£o:** eliminar `GerarTokenVerificacao`, `GerarTokenRecuperacao` e as duas rotas (Parte 2). `ResetarSenha` fica, mas sÃ³ acessÃ­vel com tokens que chegam por e-mail (Parte 4).

---

## SEC-02 â€” Rate limiters desativados (Alta â†’ decisÃ£o: eliminar) â€” âœ… resolvido

**Onde:** `internal/middleware/rate_limit.go`; `cmd/server/main.go:286, 308, 311, 312, 313, 333`.

**O que acontece.** `GlobalRateLimit`, `LoginRateLimit` e `EmailRateLimit` existem, sÃ£o registados e **nÃ£o limitam nada**. O `.env.example` (linhas 179-182) documenta `RATE_LIMIT_GLOBAL=100`, `RATE_LIMIT_LOGIN=5`, `RATE_LIMIT_EMAIL=2`, mas nenhum cÃ³digo Go lÃª essas variÃ¡veis. A API doc afirma "Rate limit ativo" em dois sÃ­tios (linhas 657 e 1106), o que Ã© falso. O comentÃ¡rio do prÃ³prio cÃ³digo jÃ¡ reconhece que as rotas financeiras pÃºblicas usam instÃ¢ncias dedicadas "em vez de depender do limitador global legado".

**DecisÃ£o (D1):** eliminar as trÃªs funÃ§Ãµes, os seus usos e a configuraÃ§Ã£o associada. Lista exata na Parte 7.

**O que NÃƒO se elimina.** `RateLimiter`, `NewRateLimiter` e `RateLimitMiddleware` sÃ£o funcionais e **jÃ¡ protegem 3 rotas pÃºblicas** de solicitaÃ§Ã£o de matrÃ­cula.

**Risco residual (declarado, nÃ£o mitigado):** remover cÃ³digo que nÃ£o fazia nada **nÃ£o altera o comportamento**. Continuam sem limite na aplicaÃ§Ã£o: o login por forÃ§a bruta, as rotas pÃºblicas de e-mail (`recuperar-senha/solicitar`, `verificar-email/:token`), o cadastro pÃºblico de academias (SEC-08) e o `bootstrap`. O 2FA tem limites prÃ³prios e independentes (5 tentativas por cÃ³digo), mas sÃ³ protege quem o ativou; o seu limitador de senhas em memÃ³ria sÃ³ Ã© usado ao confirmar a senha nos fluxos do prÃ³prio 2FA (`dois_fatores_handlers.go:546` e `:608`), nÃ£o no login. Com a opÃ§Ã£o B (Parte 4) e as respostas genÃ©ricas (SEC-06) o abuso das rotas de e-mail deixa de comprometer contas, mas ainda pode consumir a quota do Brevo. Se mudar de ideias, `RateLimitMiddleware` pode ser aplicado rota a rota sem escrever cÃ³digo novo.

---

## SEC-03 â€” Reset de senha sem prova de posse do e-mail (Alta) â€” âœ… resolvido

**Onde:** `auth_email_handlers.go` â€” `SolicitarRecuperacaoSenha` (403-583); `email_service.go` â€” `SendPasswordResetEmail` (361-385).

**O que acontece.** `POST /email/recuperar-senha/solicitar` Ã© pÃºblica e **aplica imediatamente uma nova senha aleatÃ³ria** e sÃ³ depois a envia por e-mail. O titular nÃ£o precisa de agir para a senha antiga deixar de funcionar.

**Impacto:** qualquer pessoa pode **bloquear a conta de outra** repetidamente, sÃ³ com o identificador. A senha circula em texto claro por e-mail. Se o envio falhar, a senha jÃ¡ foi trocada e o titular fica sem acesso.

**DecisÃ£o (D5):** fluxo por link. O pedido sÃ³ envia, por Brevo, um link de uso Ãºnico; a senha sÃ³ muda quando o titular abre o link e escolhe a nova. Plano na Parte 4.

---

## SEC-04 â€” Senha inicial igual ao cÃ³digo do utilizador (Alta) â€” âœ… resolvido

**Onde:** `email_service.go:560-571` (`GetDefaultPassword`), usado em `estudante_handlers.go:264`, `solicitacao_matricula_handlers.go:394` e `:591`, `academia_handlers.go:147`; `internal/utils/codigo_estudante.go`.

**O que acontece.**
- Estudantes e academias criados por terceiros recebem como senha o prÃ³prio cÃ³digo, que Ã© tambÃ©m o login. **Quem conhece o cÃ³digo entra na conta.**
- NÃ£o existe nenhum indicador de troca obrigatÃ³ria no backend. O frontend apenas diz, em texto, que o utilizador "deverÃ¡ alterÃ¡-la" (`CadastroSingularForm.tsx:152`, `academias/cadastrar/PageContent.tsx:28`).
- O cÃ³digo de estudante (3 letras + 4 dÃ­gitos, â‰ˆ 175,8 milhÃµes de combinaÃ§Ãµes) Ã© gerado com `math/rand` e registado em log a cada geraÃ§Ã£o, ou seja, regista tambÃ©m a senha inicial.

**Impacto:** contas acessÃ­veis a qualquer pessoa que veja o cÃ³digo (colegas, documentos, logs) atÃ© o titular trocar a senha, o que pode nunca acontecer.

**DecisÃ£o (D2/D3):** senha temporÃ¡ria aleatÃ³ria de 8 caracteres, entregue por SMS (estudante, academia) ou e-mail (admin), com troca obrigatÃ³ria. Plano completo na Parte 3.

---

## SEC-05 â€” Senhas padrÃ£o de admin em repositÃ³rios pÃºblicos (MÃ©dia) â€” âœ… resolvido

**Onde:** frontend `src/types/email-auth.ts` (`SenhasPadrao`: `spuriadm`, `spurigerente`, `spurifpp`, `spuri123`; helper `getSenhaPadrao`) e `src/docs/Tarefas feitas/1 - â€¦md` (â‰ˆ linhas 251-285); backend `email_service.go:93` (comentÃ¡rio que repete os valores).

**O que acontece.** Os dois repositÃ³rios sÃ£o pÃºblicos. O backend jÃ¡ nÃ£o gera admins com estas senhas (fix E4-AA-04), mas o frontend ainda as publica e **todas estÃ£o no histÃ³rico git dos dois repositÃ³rios**: no backend, em pelo menos 8 commits (incluindo cÃ³pias da pasta do frontend); no frontend, nos commits `c93c2f4` e `b6c0ef6`. Nada no cÃ³digo usa `SenhasPadrao`/`getSenhaPadrao`.

**Impacto:** qualquer admin criado antes do fix E4-AA-04 que nunca trocou a senha tem uma conta acessÃ­vel a qualquer pessoa. Apagar os ficheiros **nÃ£o basta**: o histÃ³rico continua a expÃ´-las.

**DecisÃ£o (D6):** remover tudo (Ã¡rvore, histÃ³rico) e tratar as senhas como comprometidas. Plano na Parte 5.

**Varredura de segredos reais no histÃ³rico.** Procurei `xkeysib-` (Brevo), `zk_live_` (Ziett), chaves privadas PEM, `ghp_`, `AKIA` e ficheiros `.env` commitados, nos dois repositÃ³rios. SÃ³ encontrei placeholders de teste e texto de documentaÃ§Ã£o. A varredura nÃ£o deteta chaves sem formato reconhecÃ­vel (EmailJS, senhas SMTP, `JWT_SECRET`); estas nÃ£o podem ser excluÃ­das por esse mÃ©todo.

---

## SEC-06 â€” EnumeraÃ§Ã£o de contas (MÃ©dia) â€” âœ… resolvido

**Onde:** `auth_handlers.go:179` e `:184` (**login**); `SolicitarRecuperacaoSenha` e `GerarTokenRecuperacao`.

**O que acontece.**
- O **login** devolve mensagens diferentes: `"usuÃ¡rio nÃ£o encontrado"` (linha 179) e `"senha incorreta"` (linha 184), apesar de o comentÃ¡rio da funÃ§Ã£o falar em resposta "deliberadamente genÃ©rica".
- A recuperaÃ§Ã£o distingue `404` (inexistente), `403` (e-mail nÃ£o verificado) e `200`. `GerarTokenRecuperacao` ainda devolve `email` e `nome` do titular.

**Impacto:** lista de contas vÃ¡lidas, com o estado de verificaÃ§Ã£o, para ataques dirigidos.

**CorreÃ§Ã£o:** o login responde sempre com a mesma mensagem (`"credenciais invÃ¡lidas"`). `recuperar-senha/solicitar` responde sempre `200` com a mesma mensagem e faz o trabalho real (e o log do motivo interno) apenas no servidor, de preferÃªncia em *goroutine*, para o tempo de resposta nÃ£o revelar a existÃªncia da conta. Ajustar os testes `â€¦ExigeEmailVerificado` e `â€¦IdentificadorInexistente` em `auth_email_handlers_integration_test.go` e a UX do frontend que hoje reage ao `403` (`isEmailNaoVerificadoError`).

---

## SEC-07 â€” Segredos gravados nos logs (MÃ©dia) â€” âœ… resolvido

**Onde (`internal/services/email_service.go`):**

| Linha | SituaÃ§Ã£o | O que Ã© registado |
|---|---|---|
| 326 | `SendVerificationEmail` sem Brevo | Link completo (contÃ©m o token) |
| 371 | `SendPasswordResetEmail` sem Brevo | **Senha temporÃ¡ria em texto claro** |
| 381 | `SendPasswordResetEmail` quando o Brevo falha | **Senha temporÃ¡ria em texto claro** |
| 400 | `SendAdminWelcomeEmail` com EmailJS desativado | **Senha do admin em texto claro** |
| `utils/codigo_estudante.go:22` | GeraÃ§Ã£o de cÃ³digo de estudante | CÃ³digo (= senha inicial, SEC-04) |

**Agravante.** Nos casos "sem configuraÃ§Ã£o" a funÃ§Ã£o retorna `nil`; o chamador conclui que enviou. `RegisterAdmin` afirma que a senha "foi enviada por e-mail" quando sÃ³ existe no log.

**CorreÃ§Ã£o:** nunca registar segredos. Sem canal configurado, registar apenas *"credenciais NÃƒO enviadas: canal indisponÃ­vel"* e **devolver erro** (reutilizar `ErrEmailNaoConfigurado`). **Ãšnica exceÃ§Ã£o**, copiada do padrÃ£o do 2FA: em desenvolvimento, com a flag explÃ­cita `CREDENCIAIS_LOG_DEV=true` e **nunca** com `ENV=production`, as credenciais podem ser escritas no log para testes locais.

---

## SEC-08 â€” AmplificaÃ§Ã£o de e-mails pelo cadastro pÃºblico de academias (MÃ©dia) â€” âœ… resolvido

**Onde:** `POST /academia/cadastro` (pÃºblica) â†’ `notificarAdminsSobreNovaAcademiaPendente` (`academia_handlers.go`, linha 458).

**O que acontece.** Cada cadastro disparava, **de forma sÃ­ncrona e sequencial dentro do pedido**, um e-mail Brevo por administrador elegÃ­vel, com timeout de 15 s por envio. Sem limite de taxa (SEC-02), um script pode repetir o cadastro indefinidamente.

**Impacto:** inundaÃ§Ã£o das caixas dos admins; consumo da quota do Brevo (bloqueando verificaÃ§Ãµes e recuperaÃ§Ãµes legÃ­timas); pedidos presos atÃ© *N admins Ã— 15 s*.

**CorreÃ§Ã£o:** envio **assÃ­ncrono em goroutine** fora do ciclo da requisiÃ§Ã£o + **agrupamento com BCC** (um Ãºnico e-mail via Brevo, primeiro admin como `to`, demais em BCC). Implementado em:
- `internal/handlers/academia_handlers.go:499-577` â€” `notificarAdminsSobreNovaAcademiaPendente` regravada para capturar dados, disparar goroutine e usar `services.SendBrevoEmailWithBCC`.
- `internal/services/brevo_mailer.go:118-190` â€” `LoadBrevoConfigForAsync` (lÃª config sem `gin.Context`) e `SendBrevoEmailWithBCC` (envio Ãºnico com lista BCC via API do Brevo).
- `internal/services/email_service.go:43-60` â€” `LoadBrevoConfigForAsync` exposto no serviÃ§o.
O cadastro da academia retorna imediatamente; falhas de e-mail sÃ³ sÃ£o logadas (padrÃ£o nÃ£o bloqueante do projeto).

---

## SEC-09 â€” Ciclo de vida dos tokens (Baixa) â€” âœ… resolvido

**Onde:** `internal/services/email_service.go` (`SaveToken` linhas 70-105, `VerifyToken` linhas 110-145, `hashToken` linhas 43-59).

1. **Consumo atÃ³mico.** `VerifyToken` agora executa um Ãºnico `UPDATE auth_tokens SET usado = TRUE, used_at = NOW() WHERE token_hash = $1 AND tipo = $2 AND NOT usado AND expires_at > NOW() RETURNING user_id, user_type` â€” se nenhuma linha for afetada, o token Ã© invÃ¡lido/usado/expirado.
2. **Tokens em hash SHA-256.** Nova funÃ§Ã£o `hashToken(token string) string` calcula `SHA-256(token)` (hex). `SaveToken` guarda o hash; `VerifyToken` faz hash do token recebido e compara. O token em texto claro nunca Ã© persistido.
3. **InvalidaÃ§Ã£o de tokens antigos.** `SaveToken` executa `UPDATE auth_tokens SET usado = TRUE WHERE user_id = $1 AND user_type = $2 AND tipo = $3 AND NOT usado` antes de inserir o novo â€” sÃ³ um token pendente por utilizador/tipo.
4. **Limpeza agendada.** Pendente (Parte 7) â€” a migration jÃ¡ tem `cleanup_expired_tokens()`; serÃ¡ chamada por job cron.

---

## SEC-10 â€” SessÃµes JWT nÃ£o sÃ£o revogadas apÃ³s reset de senha (MÃ©dia) â€” âœ… resolvido

**Onde:** `internal/middleware/auth.go` (JWT *stateless*, expiraÃ§Ã£o padrÃ£o de 24 h; `IssuedAt` gravado na linha 90); `internal/projections/estudante_projection.go`, `academia_projection.go`, `admin_projection.go` (coluna `sessoes_validas_desde`); `internal/handlers/auth_handlers.go` (`responderLoginComSucesso`, `finalizarVerificacaoStatusUsuario`).

**O que acontece.** Depois de uma recuperaÃ§Ã£o de senha, os tokens emitidos antes continuavam vÃ¡lidos atÃ© expirarem. Quem jÃ¡ tinha entrado na conta mantinha acesso.

**CorreÃ§Ã£o:** coluna `sessoes_validas_desde` (timestamp) nas trÃªs projeÃ§Ãµes (`projection_estudantes`, `projection_academias`, `projection_admins`). O `AuthMiddleware` (linhas 180-195) rejeita tokens com `IssuedAt` anterior a esse valor.
- Atualizada **apenas** por: reset por link (Parte 4, `FinalizarVerificacaoSenha`) e reenvio de credenciais (Parte 3, `ReenviarCredenciaisTemporarias`).
- **NÃ£o alterada** pela troca normal de senha do prÃ³prio utilizador (`AlterarSenha`), para nÃ£o terminar a sessÃ£o atual.
- Entregue como dependÃªncia da Tarefa 123 (backend) + Tarefa 34 (frontend).

---

## SEC-11 â€” `POST /bootstrap` pÃºblico e sem segredo de instalaÃ§Ã£o (MÃ©dia) â€” âœ… resolvido

**Onde:** `cmd/server/main.go:313` (rota registrada); `internal/handlers/bootstrap_handler.go:30-56` (`BootstrapHandler`).

**O que acontece.** A rota cria o primeiro admin `fpp` e sÃ³ funciona enquanto nÃ£o existir nenhum admin. NÃ£o exigia segredo de instalaÃ§Ã£o: num deploy novo com base vazia, **o primeiro a chamar a rota vencia**. A senha enviada nÃ£o passava por `ValidateSenha`.

**CorreÃ§Ã£o:** exigir header `BOOTSTRAP_TOKEN` com o valor da variÃ¡vel de ambiente `BOOTSTRAP_TOKEN` (obrigatÃ³rio em produÃ§Ã£o). Implementado em:
- `internal/handlers/bootstrap_handler.go:30-56` â€” valida header obrigatÃ³rio, compara com `os.Getenv("BOOTSTRAP_TOKEN")`, retorna `401` se ausente ou invÃ¡lido, `403` se nÃ£o configurado no servidor. Valida senha com `ValidateSenha` (mÃ­nimo 8 chars, maiÃºscula, minÃºscula, nÃºmero, sÃ­mbolo). Retorna `409` se jÃ¡ existe admin.
- VariÃ¡vel `BOOTSTRAP_TOKEN` deve ser definida no ambiente de produÃ§Ã£o antes do primeiro deploy.

---

## SEC-12 â€” Permitir login em qualquer estado deixa rotas sem bloqueio prÃ³prio (Alta) â€” âœ… resolvido

**Onde:** `internal/middleware/auth.go` (`AuthMiddleware` linhas 20-448, `ValidarStatusAcademia` linhas 420-448, `rotaPermitidaEstudanteInativo` linhas 450-495); `cmd/server/main.go` (reestruturaÃ§Ã£o de rotas linhas 357-472); `internal/handlers/auth_handlers.go` (`avaliarStatusLogin` linhas 219-277); handlers de defesa em profundidade em `servico_extra_solicitacao_handlers.go`, `notas_handlers.go`, `faltas_handlers.go`, `avaliacao_final_handler.go`.

**O que acontece.** A decisÃ£o D4 permitiu login em qualquer estado, mas as rotas nÃ£o tinham bloqueio prÃ³prio:
- **Academias:** `ValidarStatusAcademia` sÃ³ estava em 4 grupos `/academia/*`. As demais (financeiro, jobs, protected) ficavam abertas a academia `inativa`.
- **Estudantes `inativo`:** o token restrito (SEC-10) bloqueava tudo exceto pagamentos. Ao eliminÃ¡-lo, 4 rotas ficavam sem proteÃ§Ã£o (serviÃ§o extra, cancelamento, interrupÃ§Ã£o, desvinculaÃ§Ã£o). `SolicitarServicoExtra` testava `CodigoAcademia == nil` que nunca Ã© nulo pÃ³s-desvinculaÃ§Ã£o.

**CorreÃ§Ã£o:**
1. **ReestruturaÃ§Ã£o de rotas** (`cmd/server/main.go:357-472`): dois grupos principais â€” `conta` (apenas autenticaÃ§Ã£o, sem bloqueio de estado) e `protected` (exige `ValidarStatusAcademia` para academia, regra por mÃ©todo para estudante).
2. **Middleware** (`internal/middleware/auth.go`):
   - `rotaPermitidaEstudanteInativo`: allowlist por mÃ©todo â€” `GET /estudante/meu-perfil`, `PUT /estudante/alterar-senha`, `POST /estudante/logout`, rotas 2FA, rotas de pagamento, `GET /estudante/bilhete-identidade`, `POST /estudante/revinculacao`, `DELETE /estudante/conta`.
   - `AuthMiddleware` reescrito: aplica regra por mÃ©todo a estudante `inativo`; academia `inativa` sÃ³ passa no grupo `conta`.
   - `finalizarVerificacaoStatusUsuario` atualizada: bloqueia `deletado` para **todos** os tipos (estudante, academia, admin).
   - Token restrito de finanÃ§as **eliminado** (era dependÃªncia do SEC-10).
3. **Login** (`auth_handlers.go:219-277`): `avaliarStatusLogin` bloqueia `deletado` para estudante/academia; permite `inativo`, `pendente_ativacao`, `pendente_validacao`. `responderLoginComSucesso` simplificada (um sÃ³ `GenerateToken`).
4. **Defesa em profundidade nos handlers** (SEC-12/13): `GetNotasEstudante`, `GetFaltasEstudante`, `GetAvaliacoesFinaisEstudante`, `SolicitarServicoExtra` verificam `status != 'deletado'` (estudante) e `academia.Status == 'ativo'` antes de processar.

---

## SEC-13 â€” Estudante com conta apagada continua a conseguir entrar (Alta) â€” âœ… resolvido

**Onde:** `internal/projections/estudante_projection.go:1321-1343` (`GetAuthByIdentificador`); `internal/handlers/auth_handlers.go:219-277` (`avaliarStatusLogin`); `internal/middleware/auth.go:170-195` (verificaÃ§Ã£o no `AuthMiddleware`); handlers de defesa em profundidade.

**O que acontece.** Apagar a conta de um estudante Ã© *soft delete*: a linha fica em `projection_estudantes` com `status = 'deletado'` e o hash da senha intacto. O login procurava por cÃ³digo ou e-mail **sem filtrar `deletado`**, `avaliarStatusLogin` sÃ³ distinguia `inativo` e o middleware sÃ³ bloqueava `inativo` para estudantes. Resultado: um estudante apagado obtinha token completo. NÃ£o acontecia com admin/academia (bloqueados por `!= ativo`) nem no 2FA (`carregarIdentidadeAuth` filtra `deletado`).

**Impacto:** a exclusÃ£o nÃ£o revogava o acesso. O estudante apagado continuava a ler dados, fazer pagamentos e usar todas as rotas.

**CorreÃ§Ã£o:** bloqueio em trÃªs camadas:
1. **ProjeÃ§Ã£o** (`estudante_projection.go:1321-1343`): `GetAuthByIdentificador` adiciona `AND status <> 'deletado'` no `WHERE` â€” estudante `deletado` nunca Ã© retornado para autenticaÃ§Ã£o.
2. **Login** (`auth_handlers.go:219-277`): `avaliarStatusLogin` retorna erro `"conta indisponÃ­vel"` para `status == 'deletado'` (estudante e academia).
3. **Middleware** (`auth.go:170-195`): `AuthMiddleware` verifica `userDTO.Status == "deletado"` para estudante e academia e retorna `401`.
4. **Defesa em profundidade** nos handlers de leitura (`GetNotasEstudante`, `GetFaltasEstudante`, `GetAvaliacoesFinaisEstudante`, `SolicitarServicoExtra`): verificam `estudante.Status == "deletado"` antes de processar e retornam `401`.
A decisÃ£o D4 mantÃ©m `deletado` bloqueado para os trÃªs tipos de conta.

---

## SEC-14 â€” Hashes de senha expostos nos endpoints de eventos (MÃ©dia) â€” âœ… resolvido

**Onde:** `internal/handlers/estudante_handlers.go` â€” `GetEventosEstudante` (linhas 748-805), `GetEventoAuditoria` (linhas 1186-1260); `repository.GetEventHistory` + `db.Event` (`event_store.go:15`).

**O que acontece.** Devolviam os eventos **tal como estÃ£o guardados**, com o `payload` completo. O evento de criaÃ§Ã£o (`SenhaHash`) e o de troca de senha (`SenhaAlteradaEvent.NovaSenhaHash`) levam o **hash bcrypt** da senha. Quem pode chamar: o prÃ³prio estudante, a **academia** (qualquer estudante com o seu `codigo_academia`) e os admins.

**Impacto:** a academia, ou quem comprometer a conta de uma academia, obtinha hashes de senhas para quebra offline.

**CorreÃ§Ã£o:** funÃ§Ãµes de redaÃ§Ã£o adicionadas em `estudante_handlers.go`:
- `redigirEventoSensivel(event *db.Event) *db.Event` â€” cÃ³pia do evento com `payload` limpo (remove chaves `SenhaHash`, `NovaSenhaHash`, `*Hash`).
- `redigirEventosSensiveis(events []*db.Event) []*db.Event` â€” aplica a anterior a uma lista.
Aplicadas em `GetEventosEstudante` (linha 798) e `GetEventoAuditoria` (linha 1255). O evento guardado nÃ£o muda; sÃ³ a resposta Ã© limpa. Teste: nenhuma resposta contÃ©m `$2a$` nem `$2b$`.

---

## SEC-15 â€” Isolamento entre academias: a atual lÃª registos das anteriores e a anterior perde o acesso (MÃ©dia) â€” âœ… resolvido

**Onde:** `internal/handlers/notas_handlers.go:397` (`GetNotasEstudante`), `faltas_handlers.go:371` (`GetFaltasEstudante`), `avaliacao_final_handler.go:1077` (`GetAvaliacoesFinaisEstudante`); nova projeÃ§Ã£o `internal/projections/estudante_vinculos_projection.go`; `internal/handlers/helpers.go:158, 295` (`getEstudanteVinculosProjection`, `acessoAcademiaAoEstudante`); `internal/projections/manager.go:348` (rebuild order); `migrations/136_estudante_vinculos.sql`.

**O que acontece.** Estes handlers autorizavam pela academia do **Ãºltimo vÃ­nculo** (`estudante.CodigoAcademia`) e depois carregavam os registos **de todas as academias** (`GetByEstudante(codigo)`), filtrando apenas se o chamador passasse `codigos_academia` na query string. Num estudante que passou da academia A para a B: **B lia as notas, faltas e avaliaÃ§Ãµes registadas por A**, e **A era barrada logo na autorizaÃ§Ã£o**, perdendo o acesso ao que ela prÃ³pria registou.

**Impacto:** exposiÃ§Ã£o de dados entre instituiÃ§Ãµes e perda de acesso de cada academia Ã  sua prÃ³pria informaÃ§Ã£o.

**CorreÃ§Ã£o:** vÃ­nculos histÃ³ricos + filtro forÃ§ado por academia do chamador.
1. **Nova projeÃ§Ã£o `projection_estudante_vinculos`** (`estudante_vinculos_projection.go`): reconstrÃ³i histÃ³rico a partir dos eventos `EstudanteCriadoComVinculo`, `EstudanteReintegrado`, `EstudanteDesvinculadoDaAcademia`. Cada linha guarda `estudante_id`, `codigo_academia`, `vinculado_em`, `desvinculado_em` (NULL = ativo). Ãndices por academia, por estudante, por par; constraint Ãºnica impede dois vÃ­nculos ativos com a mesma academia.
2. **Rebuild order** (`manager.go:348`): `"estudante_vinculos"` entre `"estudantes"` e `"turmas"`.
3. **Helper `acessoAcademiaAoEstudante`** (`helpers.go:295`): chama `TeveVinculoAlgumVez` na projeÃ§Ã£o â€” retorna `true` se a academia **jÃ¡ teve** vÃ­nculo com o estudante (ativo ou histÃ³rico).
4. **Filtro forÃ§ado nas 3 rotas** (`notas_handlers.go:397`, `faltas_handlers.go:371`, `avaliacao_final_handler.go:1077`): apÃ³s confirmar vÃ­nculo via helper, **ignora** qualquer `codigos_academia` vindo na query e **injeta** `filtros.codigosAcademia = []string{academiaDTO.CodigoAcademia}`. O estudante continua a ver tudo (sem filtro de academia).
**Implementado (Partes 6.5.1/6.5.2/6.5.3):**
- Perfil reduzido para academia anterior: `GetEstudantePorCodigo` em `estudante_handlers.go` retorna apenas nome, cÃ³digo, estado do vÃ­nculo e perÃ­odos de vÃ­nculo para academia anterior.
- Filtro de eventos por perÃ­odo de vÃ­nculo: `filtrarEventosPorPeriodos` e `eventoDentroDoPeriodo` em `helpers.go`; aplicados em `GetEventosEstudante` e `GetEventoAuditoria`.
- ParÃ¢metro `vinculo=` na lista de estudantes: `ListarEstudantes` em `estudante_handlers.go` aceita `vinculo=atual|anterior|todos`.
- Cancelamento de inscriÃ§Ãµes ao desvincular/apagar: `cancelarInscricoesServicoExtraAoSair` em `helpers.go`; chamado em `decidirSolicitacaoStatus` (desvinculacao), `DeletarContaEstudante` e `DeletarContaEstudantePorAcademia`.
- CatÃ¡logo de serviÃ§os extras sÃ³ da academia atual: `ListarServicosExtrasCatalogoEstudante` em `servico_extra_handlers.go` retorna listas vazias para estudante `inativo`/`deletado`.

---

# Parte 2 â€” Eliminar tudo o que nÃ£o Ã© Brevo

**Estado atual (14/11/2026):**

- **Backend (2.1): âœ… concluÃ­do** â€” Tarefa 123 do `rastreio-backend` (eliminaÃ§Ã£o `smtp_mailer.go`, EmailJS, `gerar-token/*`, `brevo_mailer.go` sem fallback `EMAIL_USER`, `SendAdminWelcomeEmail` via Brevo).
- **Frontend (2.2): âœ… concluÃ­do** â€” Tarefa 35 do `rastreio-frontend` (remoÃ§Ã£o `emailService` morto, exportaÃ§Ã£o, variÃ¡veis legadas `.env.example`).
- **Parte 4 (recuperaÃ§Ã£o por link, opÃ§Ã£o B): âœ… concluÃ­do** â€” backend na Tarefa 123 (`SendPasswordResetLinkEmail`, `ResetarSenha`, `sessoes_validas_desde`); frontend jÃ¡ usa `emailAuthService` (fluxo por link).

| Mecanismo | Quem usa | AÃ§Ã£o |
|---|---|---|
| Brevo | VerificaÃ§Ã£o de e-mail, aviso de nova academia, 2FA, boas-vindas admin, link recuperaÃ§Ã£o | **Manter** (Ãºnico canal) |
| EmailJS | Eliminado (era `SendAdminWelcomeEmail`) | âœ… **Eliminado** |
| SMTP (`net/smtp`) | Eliminado (sem chamadores reais) | âœ… **Eliminado** |
| Frontend a enviar e-mail | Eliminado (restos removidos) | âœ… **Eliminado** |
| `gerar-token/*` | Eliminado (SEC-01) | âœ… **Eliminado** |

**O que passa a usar Brevo:** (a) o e-mail de credenciais do administrador (antes EmailJS) e (b) o e-mail de **link** de recuperaÃ§Ã£o de senha (opÃ§Ã£o B, Parte 4). Os e-mails de verificaÃ§Ã£o, de aviso de academia e do 2FA jÃ¡ usam Brevo. **Nenhum outro mecanismo de e-mail permanece.**

## 2.1 Backend â€” a eliminar

### A. Ficheiros a apagar

| Ficheiro | Motivo |
|---|---|
| `internal/services/smtp_mailer.go` | Cliente SMTP, sem chamadores reais |
| `internal/services/smtp_mailer_test.go` | Testa sÃ³ o cÃ³digo acima |
| `internal/services/smtp_mailer_e2e_test.go` | Servidor SMTP falso; os helpers `startMockSMTPServer`/`waitMail` nÃ£o sÃ£o usados fora dele |
| `internal/services/academia_cadastrada_email_test.go` | Testes exclusivos de `SendAcademiaCadastradaEmail` (EmailJS) e de `IsEnabled()` |

### B. `internal/services/email_service.go`

| Linhas | O que remover |
|---|---|
| 5, 9, 12, 15 (imports) | `"bytes"`, `"encoding/json"`, `"io"`, `"net/http"`; e `"math/big"` se ficar sem uso |
| 23-33 | Campos `serviceID`, `templateVerify`, `templateReset`, `publicKey`, `privateKey`, `enabled`, `httpClient` |
| 35-42 | `type EmailJSRequest` |
| 44-75 | `NewEmailService` atual (lÃª `EMAILJS_*`) |
| 77-79 | `IsEnabled()` |
| â‰ˆ 93-140 | `GenerateSecurePassword` (14 caracteres, com sÃ­mbolos): substituÃ­da por `GenerateTempPassword` (Parte 3) |
| 205-298 | `sendEmailViaEmailJS` |
| 361-385 | `SendPasswordResetEmail` (envia senha): substituÃ­da por `SendPasswordResetLinkEmail` (Parte 4) |
| 396-423 | `SendAdminWelcomeEmail` atual: reescrita para Brevo (abaixo) |
| 437-483 | `SendAcademiaCadastradaEmail` (EmailJS) |
| 485-511 | `SendAcademiaCadastradaEmailSMTP` |
| 560-571 | `GetDefaultPassword` (D2) |
| 299-310, 340-360 | ComentÃ¡rios histÃ³ricos sobre EmailJS |

**InÃ­cio do ficheiro depois da limpeza:**

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

(`ErrEmailNaoConfigurado` jÃ¡ existe em `email_dois_fatores.go:24` e Ã© reutilizado em todos os envios.)

### C. `brevo_mailer.go` e `brevo_mailer_test.go`

Remover o *fallback* para `EMAIL_USER` (variÃ¡vel do SMTP) em `loadBrevoConfig`:

```go
cfg := BrevoConfig{
	APIKey:      os.Getenv("BREVO_API_KEY"),
	SenderEmail: os.Getenv("EMAIL_FROM"),
	SenderName:  "Spuri",
}
```

**Antes de aplicar**, confirme que `EMAIL_FROM` estÃ¡ definido em produÃ§Ã£o; senÃ£o todos os e-mails (incluindo 2FA) deixam de sair. Ajustar `brevo_mailer_test.go` (linhas 8-34), que testa o *fallback* com `EMAIL_USER`.

### D. `auth_email_handlers.go`

| Linhas | FunÃ§Ã£o | AÃ§Ã£o |
|---|---|---|
| 188-242 | `GerarTokenVerificacao` | **Eliminar** |
| 247-327 | `GerarTokenRecuperacao` | **Eliminar** (SEC-01) |
| 336 | rota `verificar-email/solicitar` | **Corrigir** (ver E) |
| 403-583 | `SolicitarRecuperacaoSenha` | **Reescrever** (Parte 4) |
| 590-757 | `ResetarSenha` | **Manter** e ajustar (Parte 4) |
| 763-771 | `gerarEEnviarTokenVerificacao`, `gerarEEnviarTokenRecuperacao` | **Eliminar** (cÃ³digo morto) |

### E. `cmd/server/main.go`

Remover as rotas `gerar-token/*` (linhas 339 e 344) e `EmailRateLimit` (linha 333). **Corrigir tambÃ©m um defeito encontrado:** `POST /email/verificar-email/solicitar` (linha 336) estÃ¡ registada **sem** `AuthMiddleware`. O handler lÃª `user_id`/`user_type` do contexto e ignora o erro (`userID, _ := â€¦`); sem o middleware, `user_type` chega vazio e responde `"tipo de usuÃ¡rio invÃ¡lido"`. Pelo cÃ³digo, **o botÃ£o "Verificar e-mail" do frontend (que envia o JWT) nÃ£o consegue funcionar em produÃ§Ã£o**. Os testes nÃ£o o detetam porque injetam `user_id` Ã  mÃ£o.

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

`getEmailService` cria um `EmailService` novo a cada chamada. Passar a uma instÃ¢ncia Ãºnica (ex.: no contexto Gin, como `dbClient`).

### G. `.env.example`

Remover: bloco **EmailJS** (linhas 95-110, incluindo a nota desatualizada da linha 108); bloco **SMTP** (linhas 116-128, mantendo sÃ³ `EMAIL_FROM`); `RATE_LIMIT_*` (179-182); `ENABLE_EMAIL_VERIFICATION` e `ENABLE_PASSWORD_RECOVERY` (190-191, nenhum cÃ³digo Go os lÃª). Bloco final:

```dotenv
ENV=development               # development, test ou production
FRONTEND_URL=https://seu-frontend.vercel.app

# Brevo (Ãºnico canal de e-mail)
BREVO_API_KEY=
EMAIL_FROM=                   # remetente verificado no Brevo

# Credenciais temporÃ¡rias (Parte 3)
# SMS sÃ³ Ã© enviado com ENV=production. Em desenvolvimento, para testar,
# defina true para escrever as credenciais no log (ignorado em produÃ§Ã£o).
# CREDENCIAIS_LOG_DEV=true

# Segredo de instalaÃ§Ã£o do POST /bootstrap (SEC-11)
BOOTSTRAP_TOKEN=
```

### H. `DocumentaÃ§Ã£o da API.md`

Remover `POST /email/gerar-token/verificacao` (linha 990) e `â€¦/recuperacao` (linha 1013). Reescrever `POST /email/recuperar-senha/solicitar` (linha 1049, ainda descreve token e "expira em 1 hora") e `POST /email/recuperar-senha/:token` (linha 1076) para o fluxo por link. Corrigir as frases "Rate limit ativo" (linhas 657 e 1106) e a regra de senha da linha 1463 (diz 6â€“128; o cÃ³digo exige 8â€“128).

### I. Ambiente de produÃ§Ã£o (fora do cÃ³digo)

Remover `EMAILJS_*`, `EMAIL_HOST`, `EMAIL_PORT`, `EMAIL_USER`, `EMAIL_PASS` do serviÃ§o de hospedagem e **revogar** a chave privada do EmailJS e, se existir, a senha de app do Gmail. Definir `BOOTSTRAP_TOKEN`, `CREDENCIAIS_*` conforme a Parte 3.

## 2.2 Frontend â€” a eliminar

> **Estado (14/11/2026): âœ… concluÃ­do** â€” implementado na **Tarefa 35 do `rastreio-frontend`** (commit `7410248`). O frontend jÃ¡ nÃ£o tem cÃ³digo EmailJS/SMTP nem rotas `gerar-token/*`; usa exclusivamente `emailAuthService` (fluxo por link via Brevo) e variÃ¡veis `ENV`, `NEXT_PUBLIC_API_URL`, `API_URL`, `NEXT_PUBLIC_LOGIN_TOKEN_COOKIE_EXPIRATION_HOURS`.

| Ficheiro | O que remover | Estado |
|---|---|---|
| `src/lib/api/services.ts` (linhas 645-663) | O objeto `emailService` inteiro (`verificarEmail`, `solicitarVerificacaoEmail`, `solicitarRecuperacaoSenha`, `resetarSenha`, `gerarTokenVerificacao`, `gerarTokenRecuperacao`). Nada o importa; o serviÃ§o em uso Ã© `emailAuthService` (`services/email.service.ts`) | âœ… **removido** (Tarefa 35) |
| `src/lib/api/index.ts` (linha 11) | O `export { emailService }` | âœ… **removido** (Tarefa 35) |
| `src/types/email-auth.ts` | `SenhasPadrao`, `SenhaPadrao`, `getSenhaPadrao` (Parte 5). **NÃ£o apagar** `GerarTokenRecuperacaoRequest`: era usado por `email.service.ts`; renomeado para `SolicitarRecuperacaoRequest` | `SenhasPadrao`/`getSenhaPadrao` â†’ Parte 5; `SolicitarRecuperacaoRequest` jÃ¡ renomeado |
| `.env.example` | `EMAIL_HOST`, `EMAIL_PORT`, `EMAIL_SECURE`, `EMAIL_USER`, `EMAIL_PASS`, `ADMINS_NOTIFICACAO_ACADEMIA` (nenhum cÃ³digo as lÃª) e `NEXT_PUBLIC_APP_URL` (nÃ£o usada em `src/`) | âœ… **removidas** (Tarefa 35) |

## 2.3 E-mail de credenciais do administrador (EmailJS â†’ Brevo)

Hoje, se `EMAILJS_*` nÃ£o estiver configurado, a senha vai para o log e a API diz que foi enviada (SEC-07); se `EMAILJS_TEMPLATE_ADMIN_WELCOME` faltar, usa o template de reset. O link enviado (`/admin/login`) **nÃ£o existe no frontend** (a Ãºnica pÃ¡gina de login Ã© `/login`).

**Substituir em `email_service.go` (linhas 396-423):**

```go
// SendAdminWelcomeEmail envia ao admin criado por outro admin as credenciais
// temporÃ¡rias, via Brevo. Falha de forma explÃ­cita se o Brevo nÃ£o estiver
// configurado: RegisterAdmin jÃ¡ trata o erro com o aviso "email_nao_enviado".
func (s *EmailService) SendAdminWelcomeEmail(email, nome, senhaTemporaria, role string) error {
	if email == "" {
		return fmt.Errorf("email vazio")
	}
	cfg, ok := loadBrevoConfig()
	if !ok {
		log.Printf("[EMAIL-BREVO] âš ï¸  Brevo nÃ£o configurado â€” credenciais NÃƒO enviadas ao admin %s (%s)", email, role)
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

**Acrescentar em `email_html_templates.go`** (reutiliza os componentes do e-mail de recuperaÃ§Ã£o atual):

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
        OlÃ¡, <strong style="color:%s;">%s</strong>! A sua conta de administrador (<strong>%s</strong>) foi criada.
      </p>
      <p class="spuri-body-text" style="margin:12px 0 0; font-family:%s; font-size:15px; line-height:1.65; color:%s;">Utilizador (login): <strong>%s</strong></p>

      <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" class="spuri-pass-box" style="margin-top:24px; border-radius:16px; background-color:%s; border:1px solid %s;">
        <tr>
          <td style="padding:22px; text-align:center;">
            <p style="margin:0 0 8px; font-family:%s; font-size:12px; font-weight:600; color:%s;">Sua senha temporÃ¡ria</p>
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
		renderEmailAlert("warning", "âš ï¸ Importante", "SerÃ¡ obrigado a trocar esta senha no primeiro acesso. Nunca a partilhe com ninguÃ©m."),
	)

	return renderEmailShell("A sua conta de administrador Spuri foi criada.", "Bem-vindo ao Spuri", bodyHTML, frontendURL)
}

func renderAdminWelcomeEmailText(userName, userEmail, role, tempPassword, loginURL string) string {
	return fmt.Sprintf(`OlÃ¡ %s!

A sua conta de administrador (%s) no Spuri foi criada.

UTILIZADOR (LOGIN): %s
SENHA TEMPORÃRIA: %s

SerÃ¡ obrigado a trocar esta senha no primeiro acesso.

Acesse: %s

---
Â© %d Spuri - Todos os direitos reservados
`, userName, role, userEmail, tempPassword, loginURL, time.Now().Year())
}
```

---

# Parte 3 â€” Credenciais temporÃ¡rias (D2 e D3)

## 3.1 Regras

| Quem Ã© criado | Quem cria | Canal | Identificador enviado | Telefone / e-mail de destino |
|---|---|---|---|---|
| Estudante | Academia (cadastro direto, **lote**, aprovaÃ§Ã£o de matrÃ­cula sem taxa) ou **sistema** (apÃ³s pagamento da matrÃ­cula) | **SMS** | `codigo_estudante` | NÃ­vel **escolar** â†’ `telefone_encarregado`; **superior** â†’ `telefone` do estudante (regra jÃ¡ existente em `ValidarTelefonesMatricula`; sem nÃ­vel definido, o que existir) |
| Academia | Admin `fpp` (`RegisterAcademia`) | **SMS** | `codigo_academia` | `telefone` da academia (**hoje opcional, passa a obrigatÃ³rio**) |
| Administrador | Admin `fpp` (`RegisterAdmin`) | **E-mail (Brevo)** | o prÃ³prio e-mail | e-mail do admin (o `telefone` continua obrigatÃ³rio no cadastro, mas nÃ£o Ã© usado para credenciais) |

**NÃ£o mudam** (o utilizador escolhe a prÃ³pria senha): cadastro pÃºblico de academia (`RegisterAcademiaPublica`) e `POST /bootstrap`.

**Sem senhas padrÃ£o.** Eliminam-se `GetDefaultPassword` e `GenerateSecurePassword`. A Ãºnica fonte de senhas geradas passa a ser `GenerateTempPassword`.

## 3.2 Gerador de senha temporÃ¡ria

8 Ã© o menor tamanho permitido (`ValidateSenha`: 8 a 128, sÃ³ comprimento). A senha tem **exatamente 8** caracteres alfanumÃ©ricos, sem caracteres ambÃ­guos para facilitar a leitura num SMS (`I l O o 0 1`): alfabeto de 54 sÃ­mbolos, â‰ˆ 46 bits de entropia, o que Ã© suficiente para uma senha de uso Ãºnico e troca obrigatÃ³ria.

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
	tempAlphabet       = tempUpper + tempLower + tempDigits // 54 sÃ­mbolos
)

// GenerateTempPassword devolve 8 caracteres alfanumÃ©ricos (crypto/rand),
// com pelo menos uma maiÃºscula, uma minÃºscula e um dÃ­gito.
func GenerateTempPassword() (string, error) {
	max := big.NewInt(int64(len(tempAlphabet)))
	for {
		b := make([]byte, TempPasswordLength)
		for i := range b {
			n, err := rand.Int(rand.Reader, max)
			if err != nil {
				return "", fmt.Errorf("falha ao gerar senha temporÃ¡ria: %w", err)
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

Criar um helper Ãºnico (a regra de nÃ­vel jÃ¡ existe em `ValidarTelefonesMatricula`) e **validar antes de criar a conta**, para nÃ£o criar contas cujas credenciais nÃ£o possam ser entregues:

```go
// TelefoneDestinoCredenciaisEstudante: superior â†’ telefone do estudante;
// escolar â†’ telefone do encarregado; sem nÃ­vel, o que existir.
func TelefoneDestinoCredenciaisEstudante(anoFund, anoMedio, anoSuperior, telEstudante, telEncarregado *string) (string, error)
```

O resultado passa por `normalizarDestinatarioComunicacao` (`comunicacao_handlers.go:70`), que exige o formato `9XXXXXXXX`. O cadastro valida telefones sÃ³ como 9 dÃ­gitos (`phoneRegex = ^[0-9]{9}$`), por isso um fixo como `222123456` seria aceite no cadastro mas rejeitado no envio. Aplicar a mesma regra do SMS (`^9\d{8}$`) ao telefone de destino no momento do cadastro. Em `RegisterAcademia`, tornar `Telefone` obrigatÃ³rio (`academia_handlers.go:40`).

## 3.4 Envio por SMS: reutilizar a funÃ§Ã£o padronizada

**DecisÃ£o: nÃ£o criar um emissor paralelo.** Toda a lÃ³gica de envio (provedor padrÃ£o, *fallback* para o outro provedor, registo da mensagem) vive hoje **dentro do handler** `EnviarMensagemComunicacao`, acoplada ao `*gin.Context`. O plano Ã© **extrair esse nÃºcleo para uma funÃ§Ã£o Ãºnica**, chamada pelo handler e pelo envio de credenciais, sem mudanÃ§a de lÃ³gica:

```go
// internal/handlers/comunicacao_envio.go
type atorComunicacao struct {
	ID             uuid.UUID
	Tipo           string // "admin" | "academia"  (chk_mensagens_comunicacao_enviado_por_tipo)
	CodigoAcademia string // obrigatÃ³rio se Tipo=="academia"; vazio se "admin"
}

// enviarSMSComunicacao Ã© o ÃšNICO caminho de envio de SMS: normaliza o
// destinatÃ¡rio, valida o conteÃºdo, tenta o provedor padrÃ£o e depois o outro,
// e regista a mensagem (sucesso ou falha) com SaveWithAudit.
// Corpo = o das linhas 252-310 de EnviarMensagemComunicacao, sem alteraÃ§Ãµes.
func enviarSMSComunicacao(c *gin.Context, ator atorComunicacao, destinatario, conteudo string, audit db.AuditContext) (*aggregates.MensagemComunicacao, error)
```

`EnviarMensagemComunicacao` passa a ser sÃ³: ler o corpo â†’ `comunicacaoActor` â†’ `enviarSMSComunicacao` â†’ mapear o resultado para HTTP (o `502` com `detalhes_tentativas` se `Status == falhou`).

**RestriÃ§Ãµes do esquema que determinam o ator** (`migrations/124_comunicacao_mensagens.sql`): `enviado_por_tipo` sÃ³ aceita `'admin'` ou `'academia'`, `enviado_por` Ã© `UUID NOT NULL` e `codigo_academia` Ã© obrigatÃ³rio para `academia` e nulo para `admin`. NÃ£o Ã© preciso alterar o esquema:

| Origem da criaÃ§Ã£o | Ator da mensagem |
|---|---|
| Academia cadastra estudante (direto, lote, aprovaÃ§Ã£o) | A **academia** (`ID`, `'academia'`, `codigo_academia`) |
| Pagamento da matrÃ­cula (`efetivarVinculoMatriculaPaga`, ator "sistema") | A **academia dona do vÃ­nculo** (jÃ¡ carregada na funÃ§Ã£o): a mensagem Ã© registada em nome dela |
| Admin cadastra academia | O **admin** (`ID`, `'admin'`, `codigo_academia` nulo) |

**Risco aceite (decisÃ£o do utilizador).** Como o envio passa pela funÃ§Ã£o padronizada, a senha temporÃ¡ria fica gravada no conteÃºdo da mensagem (evento `MensagemComunicacaoRegistrada` e `projection_mensagens_comunicacao`), visÃ­vel no histÃ³rico Ã  academia que enviou e aos admins. Ã‰ aceitÃ¡vel por ser temporÃ¡ria e de troca obrigatÃ³ria; depois de trocada, a mensagem deixa de ser sensÃ­vel. Estes atores jÃ¡ tÃªm poder sobre a conta (a academia cria o estudante).

**PrÃ©-requisito de produÃ§Ã£o:** deve existir um provedor padrÃ£o configurado (`projection_comunicacao_config.provedor_padrao`) e pelo menos um remetente com credenciais; sem isso o envio devolve erro e o resultado Ã© "nÃ£o enviado" (ver 3.6).

## 3.5 SÃ³ em produÃ§Ã£o

O envio de SMS sÃ³ ocorre com `ENV=production` (a mesma convenÃ§Ã£o do 2FA). Noutros ambientes **nÃ£o se envia nem se regista mensagem**; o resultado Ã© `"motivo": "ambiente_nao_producao"`. Para testes locais, com `CREDENCIAIS_LOG_DEV=true` e **nunca** em produÃ§Ã£o, as credenciais sÃ£o escritas no log (padrÃ£o do 2FA).

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
		logCredenciaisDev(identificador, senha) // sÃ³ com CREDENCIAIS_LOG_DEV=true e ENV != production
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

Texto do SMS (ASCII, sem acentos, para caber num Ãºnico segmento GSM-7; o nome Ã© normalizado e truncado):

```go
func MensagemCredenciaisSMS(nomeCurto, identificador, senha string) string {
	return fmt.Sprintf("Spuri: conta de %s criada. Usuario: %s Senha temporaria: %s Altere a senha no primeiro acesso.",
		nomeCurto, identificador, senha)
}
```

## 3.6 IntegraÃ§Ã£o nos pontos de criaÃ§Ã£o

Em todos: **criar o evento primeiro, enviar depois do commit**. Falha de envio **nunca** desfaz a criaÃ§Ã£o; devolve-se `201` com o objeto `credenciais` (`enviada: false` + motivo) e a conta fica elegÃ­vel a **reenvio** (3.8). A senha em claro nunca entra em eventos (sÃ³ o hash), nem em respostas, nem em logs.

```go
senha, err := services.GenerateTempPassword()
hash, _ := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
// ... est.CriarComVinculo(..., string(hash), ...)
est.DefinirSenhaTemporaria()                       // novo evento (3.7)
repo.SaveWithAudit(est, audit)                     // commit primeiro
resultado := enviarCredenciaisSMS(c, ator, audit, telefone, nomeCurto, codigoEstudante, senha)
// resposta: ..., "credenciais": resultado
```

| Ponto | Ficheiro:linha | AlteraÃ§Ã£o |
|---|---|---|
| Estudante (cadastro direto e **lote**) | `estudante_handlers.go:264`, dentro de `registerEstudantePorAcademiaComRequestModo` (tambÃ©m chamada por `job_item_handlers.go:160` e `batch_handlers.go:252`) | Gerar senha temporÃ¡ria; enviar SMS apÃ³s o commit. Em lote, o envio ocorre por item (custo = 1 SMS por estudante) |
| AprovaÃ§Ã£o de matrÃ­cula sem taxa | `solicitacao_matricula_handlers.go:394` | Idem |
| MatrÃ­cula paga | `solicitacao_matricula_handlers.go:591` (`efetivarVinculoMatriculaPaga`) | Idem, ator = academia do vÃ­nculo. NÃ£o hÃ¡ utilizador a quem mostrar aviso: o estado fica visÃ­vel na lista (3.9) |
| Academia criada por admin | `academia_handlers.go:147` | Idem, ator = admin. `Telefone` obrigatÃ³rio |
| Admin criado por admin | `admin_handlers.go:333` | `GenerateTempPassword` em vez de `GenerateSecurePassword`; e-mail Brevo (2.3); `DefinirSenhaTemporaria()`; se o Brevo falhar, mantÃ©m-se o aviso `email_nao_enviado` jÃ¡ existente |

**Lotes (verificado).** O cadastro em lote sÃ­ncrono (`batch_handlers.go`, atÃ© 100 estudantes) percorre os itens dentro do pedido com um `newFakeContext` por item. Esse contexto copia `user_id`, `user_type`, `dbClient`, `repository`, `projManager` e `request_id`, e os *getters* do mÃ³dulo de comunicaÃ§Ã£o assentam em `projManager`, por isso `enviarSMSComunicacao` funciona nele. Mas os clientes SMS tÃªm *timeout* de **15 s** cada: 100 envios em sequÃªncia podem demorar atÃ© 25 minutos no pior caso. Por isso, **nos lotes o SMS nÃ£o Ã© enviado dentro do laÃ§o**: acumula-se a lista de credenciais e envia-se **depois do commit**, com um *pool* de 5 envios em simultÃ¢neo, num contexto copiado (`c.Copy()`), e cada item da resposta traz `credenciais: {"canal":"sms","enviada":false,"motivo":"em_fila"}`. O resultado real (enviada ou falhada) fica no histÃ³rico de mensagens, e as falhas resolvem-se com o reenvio (3.8). As variantes assÃ­ncronas (`â€¦/register/async`, atÃ© 100 itens, executadas como *jobs*) enviam por item, no prÃ³prio *worker*.

## 3.7 Troca obrigatÃ³ria no primeiro acesso (`deve_alterar_senha`)

Segue o padrÃ£o do 2FA (evento no ledger + coluna na projeÃ§Ã£o reconstruÃ­vel) e **evita alterar assinaturas** (hÃ¡ 110 chamadas de teste a `.Criar(`).

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

**DomÃ­nio e projeÃ§Ãµes:**
- Novo evento `SenhaTemporariaDefinida`, partilhado pelos trÃªs agregados, com o mÃ©todo `DefinirSenhaTemporaria()` (levanta o evento; `Apply` apenas o aceita). **Registar o tipo em `validEventTypes`** (`internal/db/safe_queries.go:9`, onde jÃ¡ consta `DoisFatoresAtivados`).
- ProjeÃ§Ã£o: o evento pÃµe `deve_alterar_senha = TRUE` (helper partilhado, igual a `setDoisFatoresAtivo` em `projections/dois_fatores.go`).
- Eventos de senha alterada: `AdminSenhaAlterada` e `AcademiaSenhaAlterada` jÃ¡ tÃªm `motivo`. O motivo `alteracao_usuario` limpa a flag; `reset_senha` limpa a flag **e** atualiza `sessoes_validas_desde`; o novo motivo `credenciais_reenviadas` **liga** a flag e atualiza `sessoes_validas_desde`. O `SenhaAlteradaEvent` do estudante **nÃ£o tem `motivo`**: acrescentar o campo opcional `Motivo` (com `json:",omitempty"`, mantendo o nome por omissÃ£o como nos demais eventos; compatÃ­vel com eventos antigos) e um mÃ©todo novo `AlterarSenhaComMotivo(hash, motivo)`; `AlterarSenha(hash)` passa a delegar com `alteracao_usuario`, sem mudar a assinatura.

**ImposiÃ§Ã£o (servidor, nÃ£o sÃ³ frontend)** no `AuthMiddleware` (`internal/middleware/auth.go`): a consulta que `verificarStatusUsuario` jÃ¡ faz por pedido passa a ler tambÃ©m `deve_alterar_senha` e `sessoes_validas_desde`.
- Se `deve_alterar_senha = TRUE`, bloquear **tudo** exceto `PUT /alterar-senha`, `GET /meu-perfil` e `POST /logout`, com `403` e cÃ³digo `SENHA_TEMPORARIA` (mesmo padrÃ£o do `AcessoRestritoFinanceiro`).
- Se `claims.IssuedAt < sessoes_validas_desde`, responder `401` (SEC-10).
- O token restrito deixa de existir (Parte 6): o estudante `inativo` conclui a troca obrigatÃ³ria normalmente, porque `PUT /alterar-senha` estÃ¡ na *allowlist* de escrita dele.

**Resposta de login** (`responderLoginComSucesso`, `auth_handlers.go:238`) e `GET /meu-perfil` passam a incluir `deve_alterar_senha`.

**`AlterarSenha` (`profile_handlers.go`):** rejeitar `nova_senha == senha_atual`; remover a exigÃªncia de estado `ativo` para estudantes (ver Parte 6).

## 3.8 Reenvio de credenciais

Quando o SMS/e-mail falha, ou o utilizador perdeu a senha e nÃ£o tem e-mail verificado, a entidade que o gere pode **redefinir e reenviar** as credenciais:

| Rota | Quem pode |
|---|---|
| `POST /estudantes/:codigo/credenciais/reenviar` | Academia dona do estudante, ou admin `fpp` |
| `POST /dominis/academias/:codigo/credenciais/reenviar` | Admin `fpp` (mesma regra de `RegisterAcademia`) |
| `POST /dominis/administradores/:id/credenciais/reenviar` | Admin `fpp` (mesma regra de `RegisterAdmin`) |

Os admins `adm` e `gerente` **nÃ£o** tÃªm esta permissÃ£o (um `adm` poderia assim tomar conta de um `fpp`).

Efeito: gera nova senha temporÃ¡ria, grava o evento de senha com motivo `credenciais_reenviadas` (liga `deve_alterar_senha`, invalida sessÃµes), envia pelo canal do tipo de conta (SMS ou e-mail) e devolve `credenciais`. **Intervalo mÃ­nimo de 10 minutos** por conta, medido por `sessoes_validas_desde` (sem tabela nova), para limitar o custo do SMS. A aÃ§Ã£o fica auditada (`SaveWithAudit`). Confirme a matriz de papÃ©is; a tabela acima segue as regras de criaÃ§Ã£o.

## 3.9 Contas existentes (migraÃ§Ã£o)

Um *script* Ãºnico (com *dry-run* que sÃ³ conta) compara, com `bcrypt.CompareHashAndPassword`, o hash de cada estudante e academia com o respetivo cÃ³digo, e o de cada admin com as 4 senhas pÃºblicas (Parte 5).

| Grupo | AÃ§Ã£o recomendada |
|---|---|
| **Admins** com senha pÃºblica | **RedefiniÃ§Ã£o imediata**: senha temporÃ¡ria + e-mail + `sessoes_validas_desde`. Quem conhece a senha antiga nÃ£o pode ter oportunidade de entrar primeiro |
| Estudantes/academias com senha = cÃ³digo | Passo 1 (grÃ¡tis): marcar `deve_alterar_senha` e emitir o evento. Passo 2 (custo SMS): redefinir e enviar em lotes, comeÃ§ando pelas contas ativas e com atividade recente |

O passo 1 sozinho **nÃ£o elimina** a janela de risco (quem sabe o cÃ³digo pode entrar e trocar a senha antes do titular); sÃ³ obriga a troca e dispara o fluxo de reenvio.

## 3.10 Frontend

- **Login:** se a resposta trouxer `deve_alterar_senha`, redirecionar para um ecrÃ£ de troca obrigatÃ³ria (reutilizar a lÃ³gica de `PasswordSettingsCard`) e impedir a navegaÃ§Ã£o para o resto do painel (`LoginForm.tsx`, `concluirLogin`; *guard* no layout do painel). Tratar o `403 SENHA_TEMPORARIA`.
- **FormulÃ¡rios de cadastro:** em `academias/cadastrar` tornar o telefone obrigatÃ³rio; mostrar o resultado `credenciais` (toast "Credenciais enviadas por SMS" ou "SMS nÃ£o enviado â€” use Reenviar credenciais"). Atualizar os textos que ainda falam em senha igual ao cÃ³digo: `academias/cadastrar/PageContent.tsx:26-28`, `estudantes/cadastrar/CadastroSingularForm.tsx:140`, `:152`, `:836`.
- **Criar administrador:** `CriarAdminRequest` (`src/types/api.ts:505`) tem `senha?` (o backend a ignora) e **nÃ£o tem `telefone`**, que o backend exige em `/dominis/register`; por isso a criaÃ§Ã£o de admins pela interface falha hoje. Remover `senha`, acrescentar `telefone`, retirar o campo "Senha inicial" do modal (`administradores/PageContent.tsx`) e atualizar o texto para indicar que as credenciais seguem por e-mail.
- **Listas** (estudantes, academias, administradores): botÃ£o "Reenviar credenciais" e indicador de "credenciais nÃ£o confirmadas" quando `deve_alterar_senha` for verdadeiro.
- `PasswordSettingsCard` habilita o botÃ£o com `novaSenha.length >= 6`, mas a validaÃ§Ã£o exige 8: alinhar para 8.

## 3.11 Testes a acrescentar

`GenerateTempPassword` (tamanho 8, alfabeto, classes obrigatÃ³rias, unicidade); `TelefoneDestinoCredenciaisEstudante` (escolar/superior/sem nÃ­vel); `enviarSMSComunicacao` (provedor padrÃ£o, *fallback*, falha total, ator `admin`/`academia` respeitando o esquema); `ENV != production` nÃ£o envia nem grava mensagem; login com flag devolve `deve_alterar_senha`; middleware bloqueia tudo fora da *allowlist* e libera depois de `AlterarSenha`; estudante `pendente_documentos` consegue trocar a senha; reenvio respeita o intervalo; `RegisterAcademia` rejeita telefone ausente/invÃ¡lido; o evento `SenhaTemporariaDefinida` reconstrÃ³i a projeÃ§Ã£o.

## 3.12 Riscos residuais

SMS pode ser intercetado (troca de SIM) ou lido por terceiros no aparelho; o provedor mantÃ©m o seu prÃ³prio registo da mensagem; a senha temporÃ¡ria tem â‰ˆ 46 bits. MitigaÃ§Ãµes jÃ¡ previstas: troca obrigatÃ³ria, e (opcional, recomendado) **validade de 7 dias** da senha temporÃ¡ria com `senha_temporaria_expira_em`; passado o prazo o login falha e exige reenvio. NÃ£o incluÃ­ a validade no nÃºcleo do plano por nÃ£o ter sido pedida.

---

# Parte 4 â€” RecuperaÃ§Ã£o de senha por link (opÃ§Ã£o B)

> **Estado (14/11/2026): âœ… concluÃ­do** â€” backend na **Tarefa 123 do `rastreio-backend`** (`SendPasswordResetLinkEmail`, `ResetarSenha` atÃ´mica, `sessoes_validas_desde` via SEC-10, rotas `gerar-token/*` removidas); frontend jÃ¡ usa `emailAuthService` (fluxo por link) em `src/lib/api/services/email.service.ts`.

**PrincÃ­pio:** a senha sÃ³ muda quando o titular, com o e-mail na mÃ£o, abre um link de uso Ãºnico e escolhe a nova senha. Resolve SEC-01, SEC-03, SEC-06 e remove a senha do e-mail.

## 4.1 Backend â€” âœ… implementado (Tarefa 123)

1. **`SolicitarRecuperacaoSenha` (403-583) reescrita:**
   - Procura a conta pelo identificador (cÃ³digo ou e-mail) nos trÃªs tipos.
   - **Resposta sempre `200`** com a mesma mensagem ("se existir uma conta elegÃ­vel, enviÃ¡mos as instruÃ§Ãµes para o e-mail cadastrado"), exista ou nÃ£o a conta, esteja ou nÃ£o o e-mail verificado.
   - Se a conta existir, tiver e-mail verificado e e-mail cadastrado, chama `SendPasswordResetLinkEmail` **em *goroutine*** (tempo de resposta constante).
   - **NÃ£o altera a senha.**
   - Ãšnica resposta diferente: `503` se `ErrEmailNaoConfigurado` (independe da conta, nÃ£o revela nada).
2. **Novo `SendPasswordResetLinkEmail`** (substitui `SendPasswordResetEmail`). `SaveToken` jÃ¡ gera o token e devolve-o:

```go
func (s *EmailService) SendPasswordResetLinkEmail(userID uuid.UUID, userType, email, nome string) error {
	cfg, ok := loadBrevoConfig()
	if !ok {
		return ErrEmailNaoConfigurado
	}
	token, err := s.SaveToken(userID, userType, "recuperacao_senha", email, 1*time.Hour)
	if err != nil {
		return fmt.Errorf("falha ao guardar token de recuperaÃ§Ã£o: %w", err)
	}
	resetURL := fmt.Sprintf("%s/recuperar-senha/%s", s.frontendURL, token)
	subject := "Spuri - RedefiniÃ§Ã£o de senha"
	if err := sendBrevoEmail(cfg, email, nome, subject,
		renderPasswordResetLinkEmailText(nome, resetURL),
		renderPasswordResetLinkEmailHTML(nome, resetURL, s.frontendURL)); err != nil {
		return fmt.Errorf("brevo: %w", err)
	}
	return nil
}
```

3. **Template** (em `email_html_templates.go`; o tipo de token `recuperacao_senha` jÃ¡ Ã© aceite pela tabela `auth_tokens`):

```go
func renderPasswordResetLinkEmailHTML(userName, resetURL, frontendURL string) string {
	bodyHTML := fmt.Sprintf(`
      <p style="margin:0 0 6px; font-family:%s; font-size:12px; font-weight:700; letter-spacing:0.08em; text-transform:uppercase; color:%s;">RecuperaÃ§Ã£o de senha</p>
      <h1 class="spuri-h1 spuri-heading" style="margin:0 0 16px; font-family:%s; font-size:24px; line-height:1.3; font-weight:700; color:%s;">Defina uma nova senha</h1>
      <p class="spuri-body-text" style="margin:0; font-family:%s; font-size:15px; line-height:1.65; color:%s;">OlÃ¡, <strong style="color:%s;">%s</strong>. Recebemos um pedido para redefinir a senha da sua conta. Clique no botÃ£o abaixo para escolher uma nova senha. O link Ã© vÃ¡lido por 1 hora e sÃ³ pode ser usado uma vez.</p>
      <div style="text-align:center; margin-top:24px;">%s</div>
      %s
    `,
		emailTplFontStack, emailTplBrandBlue,
		emailTplFontStack, emailTplNavy,
		emailTplFontStack, emailTplGray700, emailTplNavy, html.EscapeString(userName),
		renderEmailButton(resetURL, "Definir nova senha"),
		renderEmailAlert("warning", "NÃ£o foi vocÃª?", "Ignore este e-mail: a sua senha atual continua a funcionar e nada foi alterado."),
	)
	return renderEmailShell("Redefina a sua senha do Spuri.", "RedefiniÃ§Ã£o de senha", bodyHTML, frontendURL)
}

func renderPasswordResetLinkEmailText(userName, resetURL string) string {
	return fmt.Sprintf(`OlÃ¡ %s,

Recebemos um pedido para redefinir a senha da sua conta Spuri.
Abra o link abaixo para escolher uma nova senha (vÃ¡lido por 1 hora, uso Ãºnico):

%s

Se nÃ£o foi vocÃª, ignore este e-mail: a sua senha atual continua a funcionar.
`, userName, resetURL)
}
```

4. **`ResetarSenha` (590-757) mantÃ©m-se**, com estes ajustes:
   - consumo **atÃ³mico** do token (SEC-09);
   - ao concluir, invalidar os restantes tokens pendentes do utilizador e gravar o evento com motivo `reset_senha` (limpa `deve_alterar_senha`, atualiza `sessoes_validas_desde`; SEC-10);
   - mantÃ©m `ValidateSenha` (8 a 128) e a reverificaÃ§Ã£o do e-mail verificado.
5. **Rotas:** ficam `POST /email/recuperar-senha/solicitar` e `POST /email/recuperar-senha/:token`. Desaparecem `gerar-token/*`.

## 4.2 Frontend â€” âœ… implementado (jÃ¡ usa `emailAuthService`)

- `src/lib/api/services/email.service.ts`: `EmailAuthService` com `solicitarRecuperacaoSenha` (POST `/email/recuperar-senha/solicitar`) e `resetarSenha` (POST `/email/recuperar-senha/:token`) â€” fluxo por link jÃ¡ funcional.
- PÃ¡gina `/recuperar-senha/[token]` existente consome `emailAuthService.resetarSenha`.
- Rotas pÃºblicas `/esqueci-senha` e `/dominis/esqueci-senha` mostram texto "enviaremos um link".
- **LimitaÃ§Ã£o a comunicar:** quem nÃ£o tiver e-mail verificado nÃ£o recupera por este caminho; usa o **reenvio de credenciais** (Parte 3.8 / Tarefa 123).

---

# Parte 5 â€” Senhas padrÃ£o de admin: remover tudo (D6)

## 5.1 Ãrvore atual

| RepositÃ³rio | Ficheiro | AÃ§Ã£o |
|---|---|---|
| Frontend | `src/types/email-auth.ts` (â‰ˆ linhas 62-97) | Apagar `SenhasPadrao` (62), `SenhaPadrao` (74) e `getSenhaPadrao` (84), que ainda codifica "senha = cÃ³digo" para estudante e academia (nada os usa) |
| Frontend | `src/docs/Tarefas feitas/1 - â€¦md` (â‰ˆ linhas 251-285) | Apagar o bloco de cÃ³digo com as senhas; reescrever a frase sobre "senhas padrÃ£o por role" |
| Backend | `internal/services/email_service.go:93` | O comentÃ¡rio `FIX E4-AA-04` repete os valores literais: reescrever sem eles (a funÃ§Ã£o e o comentÃ¡rio somem com `GenerateSecurePassword`) |
| Backend | `internal/domain/aggregates/remetente_comunicacao_test.go:21` | `spuri123` Ã© um identificador de remetente num teste (falso positivo); renomear para nÃ£o gerar alertas |

## 5.2 HistÃ³rico git (obrigatÃ³rio para "remover tudo")

As strings estÃ£o no histÃ³rico dos **dois** repositÃ³rios. Como as `main` de ambos jÃ¡ sofreram *force push*, reescrever o histÃ³rico Ã© viÃ¡vel:

```bash
# em cada repositÃ³rio, num clone espelho
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

Depois: todos os colaboradores fazem novo clone; pedir ao suporte do GitHub a remoÃ§Ã£o de vistas em cache e de referÃªncias antigas (PRs); *forks* e clones jÃ¡ feitos **nÃ£o** sÃ£o afetados.

## 5.3 RotaÃ§Ã£o (a parte que realmente protege)

Reescrever o histÃ³rico **nÃ£o desfaz** a exposiÃ§Ã£o. Considere as 4 senhas comprometidas para sempre. Aplicar o passo "Admins" da secÃ§Ã£o 3.9: redefiniÃ§Ã£o imediata de todo admin cujo hash corresponda a alguma delas, com troca obrigatÃ³ria e sessÃµes invalidadas.

---

# Parte 6 â€” Login por estado da conta (D4)

## 6.1 O modelo de estados do estudante e a regra pedida

O estudante tem **dois eixos de estado**, e a regra "consultar tudo o que o envolve e pagar as suas contas" vale para **todas as combinaÃ§Ãµes dos dois**.

**Eixo 1 â€” estado geral da conta** (`projection_estudantes.status`; transiÃ§Ãµes no agregado `estudante.go`):

| Estado | Como se chega lÃ¡ | Significado |
|---|---|---|
| `ativo` | CriaÃ§Ã£o com vÃ­nculo (linha 1097), reintegraÃ§Ã£o (1238), documentos completados (1429) | Vinculado a uma academia |
| `pendente_documentos` | CriaÃ§Ã£o sem todos os documentos (`CriarComVinculoPendenteDocumentos`); passa a `ativo` quando se completam | Vinculado, com documentos em falta |
| `inativo` | **SÃ³** por `EstudanteDesvinculadoDaAcademia` (linha 1210), aprovada pela academia, e sÃ³ a partir de `ativo` | **Desvinculado** de academia (o `codigo_academia` nÃ£o Ã© limpo) |
| `deletado` | `Deletar` (autoexclusÃ£o, **sÃ³ a partir de `inativo`**) ou `DeletarPorAcademia` (sÃ³ `ativo` ou `pendente_documentos`) â€” linha 1223 | Conta apagada (*soft delete*: a linha e o hash da senha ficam) |

**Eixo 2 â€” estados acadÃ©micos** (`status_escolar_fundamental`, `status_escolar_medio`, `status_superior`): cada um Ã© `inativo`, `em_andamento` ou `finalizado`. Mudam com a matrÃ­cula, a interrupÃ§Ã£o e a finalizaÃ§Ã£o de cada nÃ­vel (ensino fundamental, mÃ©dio e superior). Um estudante que terminou todos os cursos continua `ativo` no eixo 1.

**VerificaÃ§Ã£o: o eixo 2 nÃ£o influencia login, consulta nem pagamento.** Procurei todas as utilizaÃ§Ãµes dos estados acadÃ©micos: sÃ³ aparecem no domÃ­nio, na projeÃ§Ã£o e em duas funÃ§Ãµes de handlers: `validarMatriculaEmAndamento` (**lanÃ§ar nota**, escrita da academia) e `inferirTipoEnsinoDoEstudante` (deduz o nÃ­vel, nÃ£o bloqueia). Nenhum handler financeiro nem o serviÃ§o `internal/finance` os consultam; nenhuma rota de leitura os usa como condiÃ§Ã£o.

**Matriz resultante (depois de D4):**

| Estado geral | Login | Consultar tudo o que o envolve | Pagar as suas contas | Outras escritas |
|---|---|---|---|---|
| `ativo` | Sim | Sim | Sim | Conforme as regras de cada funcionalidade (inclui os estados acadÃ©micos) |
| `pendente_documentos` | Sim | Sim | Sim | Idem |
| `inativo` (desvinculado) | Sim, com **token normal** | Sim | Sim | SÃ³ a *allowlist* (6.3.2) |
| `deletado` | **NÃ£o** (hoje consegue; ver SEC-13) | â€” | â€” | â€” |

Qualquer combinaÃ§Ã£o com os estados acadÃ©micos (`inativo`, `em_andamento` ou `finalizado` em fundamental, mÃ©dio ou superior) segue a linha do estado geral. Hoje, para `ativo` e `pendente_documentos` isto jÃ¡ Ã© assim; **sÃ³ o `inativo` estava limitado** (pelo token restrito).

| Tipo | Hoje | Depois (D4) |
|---|---|---|
| Estudante | Login em qualquer estado; o `inativo` recebe um **token restrito** que sÃ³ permite pagar mensalidades (`rotaFinanceiraRestrita`) | Login em qualquer estado **exceto `deletado`**, com token normal. O token restrito Ã© **eliminado**. Consulta tudo (`GET`) e paga tudo (mensalidades, taxa de inscriÃ§Ã£o, obrigaÃ§Ãµes de serviÃ§o extra) |
| Academia | Login e `AuthMiddleware` exigem `ativo` | Login em qualquer estado, **exceto `deletado`** |
| Admin | Exige `ativo` | **Sem mudanÃ§a** |

**Duas rotas hoje inalcanÃ§Ã¡veis.** `PUT /estudante/bilhete-identidade` (o comentÃ¡rio do cÃ³digo diz que atende "exatamente esse estudante") e `POST /estudante/solicitacoes-status/revinculacao/:codigo_academia` foram feitas para o estudante desvinculado. O mesmo vale para `DELETE /estudante/conta`, que o domÃ­nio sÃ³ aceita para `inativo`. O token restrito bloqueia as trÃªs; com D4 passam a funcionar como foram desenhadas. A pÃ¡gina `/configuracoes/personalizar` do frontend jÃ¡ usa o conceito "vinculado" (`ativo` ou `pendente_documentos`) para mostrar o formulÃ¡rio do bilhete ao desvinculado.

## 6.2 VerificaÃ§Ã£o pedida: "as funcionalidades jÃ¡ tÃªm os seus prÃ³prios bloqueios?"

**Resultado: parcialmente, tanto para academias como para estudantes.**

### 6.2.1 Academias

O bloqueio por estado existe como middleware (`ValidarStatusAcademia`, `academia_middleware.go`), mas sÃ³ estÃ¡ aplicado a 4 grupos de rotas `/academia/*` (`main.go:528, 536, 545, 580`). O prÃ³prio comentÃ¡rio `FIX AUTH-01 (E4-AA-02)` em `auth.go` explica que o gate central existe **para fechar o buraco do grupo `protected`**. Ao retirar o gate central das academias, ficam **sem bloqueio por estado**:

| Rotas acessÃ­veis a academia | Onde | Risco se a academia estiver inativa |
|---|---|---|
| `POST /comunicacao/mensagens` | `main.go:379-385` | **Enviar SMS a custo da plataforma** |
| `POST /documentos/academias/:codigo/:campo/upload` | grupo `protected` | Carregar documentos |
| Grupo `/financeiro/*` (31 rotas) | `main.go` (`financeiro := protected.Group`) | Configurar credenciais de pagamento, criar/cancelar cobranÃ§as, gerar QR, **registrar pagamento externo**, anular/reativar obrigaÃ§Ãµes, configurar mensalidades e matrÃ­culas |
| `/jobs`, `POST /jobs/:id/retry-failed` | `main.go:357-364` | Reexecutar lotes (criaÃ§Ã£o de estudantes e, com a Parte 3, SMS) |
| Leituras do grupo `protected` (`/estudantes`, `/notas`, `/faltas`, `/avaliacoes*`, `/consultar-estudante/:codigo`, `/eventos-estudante/:codigo`, `/verificar-integridade/:codigo`, downloads de documentosâ€¦) | `main.go:408-452` | Ler dados |

**O que estÃ¡ correto:** todos os grupos `/academia/*` (4, com `ValidarStatusAcademia`) e `CriarSolicitacaoMatricula` (pÃºblica), que valida `academia.Status` (`solicitacao_matricula_handlers.go:114`).

**Escopo por academia (verificado).** Para saber se cada rota restringe os dados Ã  academia autenticada, fiz uma varredura automÃ¡tica das **92 rotas de academia com parÃ¢metro de caminho** (69 em `/academia/*`; 23 nos grupos `protected`, `financeiro`, `jobs` e `comunicacao`) e verifiquei manualmente as **21 sinalizadas** (sem padrÃ£o de pertenÃ§a visÃ­vel). **Todas estavam corretas**: eram wrappers que delegam em funÃ§Ãµes que verificam a pertenÃ§a (`loadServico`, `loadDocumentoExtra`, `loadCategoriaServico`, `canAccessâ€¦`, `decidirSolicitacaoStatus`) ou, no caso de `AtualizarCredencial`, autorizam o escopo declarado e `ConfigureCredential` confirma com `credentialBelongsToScope` que a credencial `:id` pertence a esse escopo. As restantes 71 rotas tÃªm uma verificaÃ§Ã£o detetÃ¡vel por padrÃ£o; **isto Ã© uma varredura, nÃ£o uma prova linha a linha**. Ela verifica **quem** pode aceder a um estudante; **o que** cada academia vÃª dentro de um estudante que passou por vÃ¡rias academias Ã© outra questÃ£o (SEC-15, 6.5.1).

### 6.2.2 Estudantes: rotas de escrita

Hoje o `inativo` nÃ£o alcanÃ§a nenhuma rota de escrita que nÃ£o seja pagamento porque o token restrito a rejeita (`auth.go:160`). Sem esse token, sÃ³ resta o que cada handler verifica. Auditei as 23 rotas de escrita que um estudante pode alcanÃ§ar (grupo `/estudante` e grupo `protected`):

| Rota | Bloqueio prÃ³prio por estado? | Tratamento para o `inativo` |
|---|---|---|
| `POST /logout`, `PUT /alterar-senha`, `PUT /me/email`, `PUT /me/telefone`, `POST /dois-fatores/*` (4) | n/a (conta) | **Permitido** |
| `POST /financeiro/mensalidades/pagamento`, `POST /financeiro/servicos-extras/taxa-inscricao/pagamento`, `POST /financeiro/servicos-extras/obrigacao/pagamento` | n/a (pagamento) | **Permitido** |
| `PUT /estudante/bilhete-identidade` | **Sim**: rejeita `ativo`/`pendente_documentos`; sÃ³ atende desvinculados | **Permitido** (feita para ele) |
| `POST /estudante/solicitacoes-status/revinculacao/:codigo_academia` | **NÃ£o** no handler; o domÃ­nio (`Reintegrar`) exige `Status == "inativo"` na aprovaÃ§Ã£o | **Permitido**; acrescentar a verificaÃ§Ã£o na criaÃ§Ã£o |
| `DELETE /estudante/conta` | **Sim, no domÃ­nio**: `Deletar` sÃ³ aceita `inativo` | **Permitido** (feita para ele; sem isto nenhum estudante conseguiria apagar a prÃ³pria conta) |
| `PUT /estudante/encarregado/telefone` | NÃ£o (sÃ³ valida o formato) | **Permitido** (dado de contacto, como `/me/telefone`) |
| `POST /estudante/solicitacoes-edicao/{nome, bilhete-identidade, bilhete-identidade-encarregado, data-nascimento}` (4) | **Sim**: exige `ativo` ou `pendente_documentos` | Bloqueado (tambÃ©m pelo handler) |
| `POST /estudante/servicos-extras/:id/solicitacao` | **NÃ£o.** Verifica `CodigoAcademia == nil`, que nunca Ã© nulo: **a verificaÃ§Ã£o existe mas nÃ£o protege** | Bloqueado por padrÃ£o; corrigir o handler |
| `PUT /estudante/servicos-extras/minhas-inscricoes/:id/cancelar` | NÃ£o | Bloqueado por padrÃ£o |
| `POST /estudante/solicitacoes-status/interrupcao` e `/desvinculacao` | **NÃ£o** no handler; o domÃ­nio rejeita na aprovaÃ§Ã£o (`Desvincular` exige `ativo`; a interrupÃ§Ã£o exige uma etapa `em_andamento`) | Bloqueado por padrÃ£o; corrigir o handler |

**ConclusÃ£o.** Quatro rotas de estudante (`servicos-extras/:id/solicitacao`, o cancelamento, `interrupcao` e `desvinculacao`) estÃ£o hoje protegidas **apenas** pelo token restrito. Retirando-o sem substituto, um estudante desvinculado poderia, por exemplo, solicitar serviÃ§os extras ou abrir pedidos de interrupÃ§Ã£o na fila da academia a que jÃ¡ nÃ£o pertence. Por isso a regra para o `inativo` Ã© **por mÃ©todo** (6.3.2), e nÃ£o handler a handler: toda escrita nasce bloqueada, e sÃ³ a *allowlist* a libera.

### 6.2.3 Estudantes: leituras (verificadas)

Verifiquei individualmente os **28 handlers `GET`** que um estudante pode alcanÃ§ar. **Todos restringem o estudante aos prÃ³prios dados e nenhum depende de estado**:

| Grupo | Handlers | Escopo |
|---|---|---|
| Conta | `GetMeuPerfil`, `ObterDoisFatores` | O prÃ³prio |
| HistÃ³rico e auditoria | `GetEventosEstudante`, `GetEventoAuditoria`, `VerificarIntegridade` | `podeAuditarEstudante`: estudante sÃ³ o prÃ³prio, academia sÃ³ os seus, admin todos (**mas ver SEC-14 e SEC-15**) |
| Notas, faltas, turmas | `GetNotasEstudante`, `GetFaltasEstudante`, `GetTurmasEstudante`, `ListarNotas`, `ListarFaltas`, `GetMinhasAvaliacoes`, `ListarCategoriasNota` | O prÃ³prio |
| AvaliaÃ§Ãµes finais | `ListarAvaliacoes`, `ListarAprovacoes`, `ListarReprovacoes` (e `GetAvaliacoesFinaisEstudante`, sÃ³ academia/admin) | Estudante â†’ o prÃ³prio; academia â†’ a prÃ³pria |
| Documentos | `ListarMeusDocumentosEstudante`, `DownloadMeuDocumentoEstudante`, `DownloadDocumentoEstudante`, `DownloadDocumentoSolicitacaoEdicaoEstudante`, â€¦ | `canAccessEstudanteDocument` e afins: estudante sÃ³ os seus |
| ServiÃ§os extras | `ListarServicosExtrasCatalogoEstudante`, `ListarMinhasInscricoesServicoExtra`, `DownloadDocumentoSolicitacaoServicoExtraEstudante`, `MinhasPendenciasServicoExtra` | O prÃ³prio (o catÃ¡logo mostra o da academia anterior ao desvinculado; Ã© sÃ³ leitura e inÃ³cuo) |
| SolicitaÃ§Ãµes | `ListarSolicitacoesEdicaoEstudante`, `ListarMinhasSolicitacoesStatusAcademicoHandler` | O prÃ³prio |
| Financeiro (consulta) | `ConsultarMensalidadesEstudante`, `ConsultarCobrancasEstudante` | SÃ³ as suas |

Fora do alcance do estudante (correto): `GetEstudantePorCodigo` e `ListarEstudantes` (sÃ³ academia/admin; o estudante usa `/meu-perfil`) e as trÃªs rotas de anos letivos (configuraÃ§Ã£o global, sem dados pessoais).

### 6.2.4 Estudantes: pagamento (verificado)

Os 3 handlers de pagamento (`IniciarPagamentoMensalidades`, `IniciarPagamentoTaxaInscricaoServicoExtra`, `IniciarPagamentoServicoExtraObrigacao`) e os 2 de consulta usam o cÃ³digo do **prÃ³prio** estudante autenticado e **nÃ£o verificam nenhum estado** do estudante; o serviÃ§o `internal/finance` tambÃ©m nÃ£o. Pagar uma obrigaÃ§Ã£o de serviÃ§o extra depende do estado **da inscriÃ§Ã£o**, nÃ£o do estudante.

### 6.2.5 AprovaÃ§Ã£o dos pedidos de status acadÃ©mico (verificada)

`decidirSolicitacaoStatus` confirma que a solicitaÃ§Ã£o pertence Ã  academia que decide. As regras de estado vivem no domÃ­nio e **estÃ£o corretas**: `DesvincularDaAcademia` exige `ativo`, `Reintegrar` exige `inativo`, e a interrupÃ§Ã£o exige exatamente uma etapa `em_andamento`. Portanto um pedido invÃ¡lido seria rejeitado na aprovaÃ§Ã£o, mas **jÃ¡ teria entrado na fila da academia**; daÃ­ a validaÃ§Ã£o na criaÃ§Ã£o (6.3.4).

## 6.3 MudanÃ§as

### 6.3.1 Login (backend)

`avaliarStatusLogin` tem **3 chamadores**: `auth_handlers.go:193` (com `responderLoginComSucesso` em `:209`) e, no segundo passo do 2FA, `dois_fatores_handlers.go:369` (com `responderLoginComSucesso` em `:375`) e `:414` (reenvio do cÃ³digo). Todos tÃªm de ser atualizados.

```go
// auth_handlers.go â€” avaliarStatusLogin (linha 219): (mensagemBloqueio, bloqueado)
func avaliarStatusLogin(userType, userStatus string) (string, bool) {
	switch userType {
	case "estudante":
		if userStatus == "deletado" { // SEC-13: conta apagada nÃ£o entra
			return "conta indisponÃ­vel. Entre em contato com a sua academia ou com o suporte.", true
		}
		return "", false // qualquer outro estado entra; a escrita Ã© restringida no middleware (6.3.2)
	case "academia":
		if userStatus == "deletado" {
			return "conta indisponÃ­vel. Entre em contato com o administrador.", true
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

`responderLoginComSucesso` (`auth_handlers.go:238`): remover o parÃ¢metro `acessoRestritoFinanceiro` e o ramo `GenerateRestrictedFinanceToken` (linhas 243-247); emite sempre `middleware.GenerateToken`. Atualizar o comentÃ¡rio de `Login` (`auth_handlers.go:39`).

**SEC-13 (defesa em profundidade):** `GetAuthByIdentificador` (`estudante_projection.go:1323`) passa a filtrar `AND status <> 'deletado'`, como jÃ¡ fazem `GetByEmail` e `GetAll`.

### 6.3.2 Middleware (backend): eliminar o token restrito e aplicar a regra por mÃ©todo

**Eliminar em `internal/middleware/auth.go`:** o campo `AcessoRestritoFinanceiro` (linha 25); `GenerateRestrictedFinanceToken` (77-79); o parÃ¢metro `restrictedFinance` de `generateToken` (82-90); o bloqueio das linhas 160-164 e a exceÃ§Ã£o `&& !(claims.AcessoRestritoFinanceiro && â€¦)` da linha 165; `c.Set("acesso_restrito_financeiro", â€¦)` (176); `rotaFinanceiraRestrita` (184-186); e o ficheiro de teste `auth_financeiro_restrito_test.go`.

**Substituir por:** `verificarStatusUsuario` (a consulta por pedido jÃ¡ existe) passa a devolver o estado da conta, e o `AuthMiddleware` aplica as regras por esta ordem (as regras 2 e 3 vÃªm da Parte 3.7 e do SEC-10):

```go
type estadoConta struct {
	Status              string
	DeveAlterarSenha    bool       // Parte 3.7
	SessoesValidasDesde *time.Time // SEC-10
}

conta, err := carregarEstadoConta(c, claims.UserID, claims.UserType) // SELECT status, deve_alterar_senha, sessoes_validas_desde
// (mantÃ©m-se o comportamento FIX E4-CI-03: erro de BD => deixa passar, com log)

switch {
case contaBloqueada(claims.UserType, conta.Status): // admin != "ativo"; academia == "deletado"; estudante == "deletado"
	// 401 "conta inativa ou nÃ£o encontradaâ€¦"
case conta.SessoesValidasDesde != nil && claims.IssuedAt.Time.Before(*conta.SessoesValidasDesde):
	// 401 (SEC-10)
case conta.DeveAlterarSenha && !rotaPermitidaComSenhaTemporaria(c.Request.Method, c.FullPath()):
	// 403 {"codigo":"SENHA_TEMPORARIA"} (Parte 3.7)
case claims.UserType == "estudante" && conta.Status == "inativo" &&
	!rotaPermitidaEstudanteInativo(c.Request.Method, c.FullPath()):
	// 403 {"codigo":"CONTA_DESVINCULADA","message":"conta desvinculada: apenas consulta e pagamentos estÃ£o disponÃ­veis"}
}
```

```go
// Estudante desvinculado ("inativo"): consulta tudo, paga, usa a prÃ³pria conta e as
// trÃªs aÃ§Ãµes feitas para ele. Qualquer outra escrita Ã© bloqueada por padrÃ£o.
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

`c.FullPath()` devolve o padrÃ£o da rota (com `:codigo_academia`), por isso a *allowlist* nÃ£o depende dos valores dos parÃ¢metros. Para liberar mais alguma aÃ§Ã£o ao `inativo`, basta acrescentar a linha correspondente. **A regra sÃ³ olha o estado geral**: os estados acadÃ©micos nÃ£o entram no middleware, em coerÃªncia com 6.1.

### 6.3.3 Academias: bloqueio prÃ³prio nas rotas

`ValidarStatusAcademia()` jÃ¡ ignora quem nÃ£o Ã© academia, por isso pode ser aplicado a grupos partilhados com admin e estudante. Reorganizar o grupo `protected`:

```go
// rotas de conta: sem gate de estado (qualquer academia pode usÃ¡-las)
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

// restantes rotas: bloqueadas para academia que nÃ£o esteja ativa
protected := router.Group("/")
protected.Use(middleware.AuthMiddleware(), middleware.ValidarStatusAcademia())
// ... (resto do grupo atual)
```

E acrescentar `middleware.ValidarStatusAcademia()` a `jobRoutes` (`/jobs`) e `comunicacaoMensagens` (`/comunicacao/mensagens`). O grupo `financeiro` herda do `protected`.

### 6.3.4 Handlers de estudante (defesa em profundidade)

A *allowlist* do middleware jÃ¡ bloqueia as rotas indevidas para o `inativo`, mas os handlers tÃªm lacunas que convÃ©m fechar:

- Criar o helper `estudanteVinculado(status) bool` (`ativo` ou `pendente_documentos`, a regra que o cÃ³digo jÃ¡ repete em `bilhete_identidade_sem_academia_handlers.go:47` e `solicitacao_edicao_dado_estudante_handlers.go:47`) e usÃ¡-lo nos dois.
- `SolicitarServicoExtra` (`servico_extra_solicitacao_handlers.go:65`): trocar `est.CodigoAcademia == nil` por `!estudanteVinculado(est.Status)`.
- `CriarSolicitacaoStatusAcademicoHandler` (`academia_status_escolar_handlers.go:87`): `interrupcao` e `desvinculacao` exigem estudante vinculado; `revinculacao` exige `inativo`. Assim os pedidos invÃ¡lidos deixam de chegar Ã  fila da academia (6.2.5).
- `AlterarSenha` (`profile_handlers.go:130-131`): remover a exigÃªncia de estado `ativo` para estudantes (hoje responde "conta inativa. NÃ£o Ã© possÃ­vel alterar a senha." e deixaria presos os `pendente_documentos`).

### 6.3.5 Frontend: eliminar a sessÃ£o restrita

O frontend tem uma experiÃªncia inteira construÃ­da em volta do token restrito. Tudo isto passa a ser eliminado, e o estudante `inativo` vÃª o painel completo:

| Ficheiro | O que remover |
|---|---|
| `src/lib/api/client.ts:24` e `:378-382` | O campo `acesso_restrito_financeiro` e `tokenStorage.isRestrictedFinance()` |
| `src/components/auth/LoginForm.tsx:49` e `:55` | O ramo da sessÃ£o restrita (redirecionamento forÃ§ado para `/pagamentos` e a sessÃ£o mÃ­nima com `status: "inativo"`) |
| `src/components/paineis/EstudantePagamentosPainel.tsx:196` e `:548` | A variÃ¡vel `restricted` e a mensagem "HistÃ³rico completo indisponÃ­vel nesta sessÃ£o restrita"; mostrar sempre o histÃ³rico completo |
| `src/components/guards/RouteGuard.tsx:44-45`, `src/hooks/useRoutePermission.ts:19` | A passagem de `isRestrictedFinance` |
| `src/app/(painel)/layout.tsx:54`, `src/layout/AppSidebar.tsx:229` | O tratamento especial da sessÃ£o restrita (layout e menu lateral reduzido) |
| `src/lib/route-guards.ts:356` e `:365` | O parÃ¢metro `isRestrictedFinance` e a regra "sÃ³ `/pagamentos`" |

**Acrescentar:** tratar o `403 CONTA_DESVINCULADA` com uma mensagem clara. O frontend quase nÃ£o ramifica por estado do estudante (sÃ³ `configuracoes/personalizar`, que jÃ¡ usa "vinculado"), por isso as aÃ§Ãµes bloqueadas (solicitar serviÃ§o extra, solicitaÃ§Ãµes de ediÃ§Ã£o, interrupÃ§Ã£o, desvinculaÃ§Ã£o) **continuam visÃ­veis** ao `inativo`. OcultÃ¡-las ou desativÃ¡-las quando `user.estudante.status === "inativo"`, mantendo visÃ­veis as feitas para ele (bilhete, revinculaÃ§Ã£o, apagar conta, pagamentos).

## 6.4 Testes que impedem regressÃµes

- **Academia inativa:** percorre `router.Routes()` com um JWT de academia `inativa` e exige `401/403` em todas as rotas, exceto numa *allowlist* explÃ­cita (as de conta e as pÃºblicas). Como o gate corre antes do handler, parÃ¢metros fictÃ­cios bastam. Qualquer rota nova acessÃ­vel a academias sem bloqueio quebra o teste em vez de ficar aberta sem ninguÃ©m reparar.
- **Estudante inativo:** percorre as rotas com um JWT de estudante `inativo` e exige `403 CONTA_DESVINCULADA` em toda rota **nÃ£o-GET** fora da *allowlist* (6.3.2), e que a *allowlist* nÃ£o seja bloqueada pelo middleware. Cobrir tambÃ©m: o `inativo` consegue pagar (as 3 rotas), trocar a senha, atualizar o bilhete, pedir revinculaÃ§Ã£o, atualizar o telefone do encarregado e **apagar a conta**; nÃ£o consegue solicitar serviÃ§o extra nem abrir `interrupcao`/`desvinculacao`.
- **CombinaÃ§Ãµes de estado:** estudante `ativo`, `pendente_documentos` e `inativo`, cada um com estados acadÃ©micos `inativo`, `em_andamento` e `finalizado`: todos conseguem consultar e pagar.
- **Estudante `deletado` (SEC-13):** o login por cÃ³digo e por e-mail devolve `401`; o segundo passo do 2FA tambÃ©m; um token emitido antes da exclusÃ£o Ã© rejeitado no pedido seguinte.
- **Handlers:** `SolicitarServicoExtra` e `CriarSolicitacaoStatusAcademicoHandler` rejeitam estudante desvinculado mesmo sem o middleware; `AlterarSenha` aceita `pendente_documentos`.
- Remover `auth_financeiro_restrito_test.go` (testa cÃ³digo eliminado).

## 6.5 VÃ­nculos, apagar conta e serviÃ§os ao sair (decisÃµes)

DecisÃµes tomadas sobre as trÃªs observaÃ§Ãµes da versÃ£o anterior, mais uma consequÃªncia financeira que a verificaÃ§Ã£o revelou (6.5.4).

### 6.5.1 Academia anterior: acesso apenas ao que ela prÃ³pria registou

**Regra decidida.** Toda academia que jÃ¡ teve o estudante vinculado pode consultar os dados dele **registados enquanto ele esteve vinculado a essa mesma academia**, e **nunca** os registados na relaÃ§Ã£o dele com outras academias. A regra vale tambÃ©m para a academia atual: ela nÃ£o vÃª o que outra academia registou.

**O que o cÃ³digo faz hoje (verificado), e que viola a regra:**

| Leitura | Como autoriza | O que devolve |
|---|---|---|
| `GetNotasEstudante` (`notas_handlers.go:380-387`) | SÃ³ a academia do **Ãºltimo vÃ­nculo** (`estudante.CodigoAcademia`) | `GetByEstudante(codigo)`: registos de **todas** as academias; sÃ³ filtra se o chamador passar `codigos_academia` na consulta |
| `GetFaltasEstudante` (`faltas_handlers.go:354-361`), `GetAvaliacoesFinaisEstudante` (`avaliacao_final_handler.go:1059-1067`) | Idem | Idem |
| `GetEstudantePorCodigo` (`estudante_handlers.go:847-937`) | SÃ³ academia atual ou admin | Perfil completo: contactos, BI, BI do encarregado, documentos, curso atual |
| `GetEventosEstudante` (`estudante_handlers.go:721`) | `podeAuditarEstudante`: sÃ³ academia atual | Todos os eventos do estudante, de todas as academias |
| `canAccessEstudanteDocument` | SÃ³ academia atual | Documentos atuais do estudante |
| `GetTurmasEstudante` (`turmas_handler.go:178-190`) | SÃ³ academia atual | **JÃ¡ filtra por academia** (`ListByEstudante(codigo, &academia)`) |
| `ConsultarMensalidadesEstudante` (`mensalidade_handlers.go:286-300`), `ConsultarCobrancasEstudante` (`financeiro_handlers.go:332-388`) | `academiaPossuiVinculoMensalidade` (`mensalidade_handlers.go:396`): o `codigo_academia` do estudante **ou** a pertenÃ§a, atual ou histÃ³rica, a uma turma da academia | Filtram as linhas pela academia do chamador (`somenteAcademia`): **jÃ¡ cumprem a regra**, inclusive para academias anteriores |

ConsequÃªncias: se um estudante muda da academia A para a B, **B lÃª notas, faltas, avaliaÃ§Ãµes e eventos registados por A** (exposiÃ§Ã£o entre instituiÃ§Ãµes, SEC-15) e **A deixa de poder consultar o que ela prÃ³pria registou**. A Ãºnica consulta ao histÃ³rico que existe hoje Ã© `estudanteTeveVinculoComAcademia` (`notas_handlers.go:594`, que lÃª o ledger), usada sÃ³ para o **estudante** consultar as categorias de nota de uma academia onde jÃ¡ esteve; nÃ£o serve Ã  academia.

**O que o esquema jÃ¡ permite.** As tabelas ligadas ao estudante guardam o `codigo_academia` em cada registo: `projection_notas`, `projection_faltas`, `projection_avaliacao_final`, `projection_aprovacao_ano`, `projection_reprovacoes`, `projection_materias_pendentes`, `projection_solicitacoes_*` (matrÃ­cula, ediÃ§Ã£o, status acadÃ©mico, serviÃ§o extra), `projection_turmas` e as tabelas `financeiro_*`. **Filtrar por `codigo_academia = A` basta para isolar os registos de cada academia, sem inventar perÃ­odos.** SÃ³ nÃ£o o tÃªm os **documentos** (`Estudante.Documentos`, que guarda a versÃ£o atual e substitui a anterior) e os **eventos** (que tÃªm `occurred_at`).

**DecisÃ£o sobre o `codigo_academia` que fica no estudante: manter, com um novo significado explÃ­cito.** O campo continua a ser "a academia do Ãºltimo vÃ­nculo" e **nÃ£o** Ã© limpo na desvinculaÃ§Ã£o. RazÃµes: tem 41 usos em 17 ficheiros (reintegraÃ§Ã£o, finanÃ§as, histÃ³rico), a reintegraÃ§Ã£o precisa do curso e do ano anteriores, e `ConsultarMensalidadesEstudante` escopa por ele. A alternativa de o anular ao desvincular quebraria tudo isso. Em vez disso:

1. Passa a existir a noÃ§Ã£o explÃ­cita de **academia atual**: `academiaAtual(est)` devolve o cÃ³digo sÃ³ se `estudanteVinculado(est.Status)` (`ativo` ou `pendente_documentos`); caso contrÃ¡rio, nada.
2. Passa a existir um **histÃ³rico de vÃ­nculos** construÃ­do a partir dos eventos.

**Migration `136_estudante_vinculos.sql`:**

```sql
BEGIN;
CREATE TABLE IF NOT EXISTS projection_estudante_vinculos (
    estudante_id     UUID        NOT NULL,
    codigo_estudante VARCHAR(20) NOT NULL,
    codigo_academia  VARCHAR(50) NOT NULL,
    vinculado_em     TIMESTAMPTZ NOT NULL,
    desvinculado_em  TIMESTAMPTZ,            -- NULL = vÃ­nculo em curso
    PRIMARY KEY (estudante_id, codigo_academia, vinculado_em)
);
CREATE INDEX IF NOT EXISTS idx_vinculos_academia_estudante
    ON projection_estudante_vinculos (codigo_academia, codigo_estudante);
CREATE INDEX IF NOT EXISTS idx_vinculos_estudante
    ON projection_estudante_vinculos (estudante_id);
COMMIT;
```

**AlimentaÃ§Ã£o** (na projeÃ§Ã£o do estudante, que jÃ¡ recebe estes eventos; `estudante_projection.go:82-87`): `EstudanteCriadoComVinculo` e `EstudanteReintegrado` abrem um perÃ­odo (`vinculado_em = occurred_at`, `desvinculado_em` nulo); `EstudanteDesvinculadoDaAcademia` e `EstudanteDeletado` fecham o perÃ­odo aberto (`UPDATE â€¦ SET desvinculado_em = occurred_at WHERE estudante_id = $1 AND codigo_academia = $2 AND desvinculado_em IS NULL`). **Preenchimento do passado:** reconstruir a projeÃ§Ã£o de estudantes (`Manager.RebuildProjection`), mecanismo que o projeto jÃ¡ usa; a reconstruÃ§Ã£o parte do ledger, onde os trÃªs eventos trazem `CodigoAcademia`.

**Um Ãºnico helper de acesso**, que substitui as verificaÃ§Ãµes `estudante.CodigoAcademia != academia` e `estudanteTeveVinculoComAcademia`:

```go
type AcessoAcademia int
const (
	SemAcesso      AcessoAcademia = iota
	AcessoAnterior                // jÃ¡ teve vÃ­nculo, hoje nÃ£o tem (ou tem com outra academia)
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

O financeiro jÃ¡ cumpre a regra com o seu critÃ©rio prÃ³prio (`academiaPossuiVinculoMensalidade`) e pode manter-se; convÃ©m convergir para o helper Ãºnico num segundo momento, para nÃ£o haver dois critÃ©rios de vÃ­nculo.

**O que cada tipo de acesso permite (academia):**

| Recurso | `AcessoAtual` | `AcessoAnterior` |
|---|---|---|
| Perfil (`GetEstudantePorCodigo`) | Completo, como hoje | **Reduzido:** nome, cÃ³digo, estado do vÃ­nculo e perÃ­odos de vÃ­nculo (`vinculado_em`/`desvinculado_em`). Sem contactos, BI, documentos, curso atual nem dados de outras academias |
| Notas, faltas, avaliaÃ§Ãµes finais, aprovaÃ§Ãµes, reprovaÃ§Ãµes, matÃ©rias pendentes | SÃ³ registos com `codigo_academia = A` | SÃ³ registos com `codigo_academia = A` |
| Turmas, mensalidades, cobranÃ§as, solicitaÃ§Ãµes, inscriÃ§Ãµes em serviÃ§os | SÃ³ as da academia A (jÃ¡ assim, ou por filtro) | SÃ³ as da academia A |
| Documentos do estudante (`Documentos`) | Sim | **NÃ£o** (sÃ£o a versÃ£o atual e nÃ£o tÃªm perÃ­odo; os documentos de uma solicitaÃ§Ã£o continuam acessÃ­veis Ã  academia que a recebeu, via `canAccessSolicitacaoDocument`) |
| Eventos (`GetEventosEstudante`, `GetEventoAuditoria`) | SÃ³ eventos cujo `occurred_at` cai nos perÃ­odos de A (inclusive os limites), com hashes redigidos (SEC-14) | Idem |
| `VerificarIntegridade` | Sim | NÃ£o (percorre a cadeia inteira do estudante, de todas as academias) |
| **Escrita** (lanÃ§ar nota, alterar dados, apagar contaâ€¦) | Como hoje | **Nenhuma**: a academia anterior sÃ³ lÃª |
| Lista `/estudantes` | `vinculo: "atual"` | `vinculo: "anterior"` |

**Filtro forÃ§ado** nos trÃªs handlers que hoje nÃ£o filtram (exemplo em `GetNotasEstudante`; igual para faltas e avaliaÃ§Ãµes finais). O estudante, que consulta os **seus** dados, continua a ver tudo:

```go
if userType == "academia" {
	acesso, _, err := acessoAcademiaAoEstudante(c, academiaDTO.CodigoAcademia, estudante)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if acesso == SemAcesso {
		utils.RespondWithForbiddenError(c, "Estudante nÃ£o pertence nem pertenceu a esta academia")
		return
	}
	// â€¦ carregar registos e interpretar os filtros da consulta â€¦
	filtros.codigosAcademia = []string{academiaDTO.CodigoAcademia} // FORÃ‡ADO: ignora o que vier na consulta
}
```

**Eventos:** `filtrarEventosPorPeriodos(eventos, periodos)` mantÃ©m os eventos com `occurred_at >= vinculado_em` e (`desvinculado_em` nulo ou `occurred_at <= desvinculado_em`). A lista `/estudantes` ganha o parÃ¢metro `vinculo=atual|anterior|todos` (por omissÃ£o `todos`, que reproduz o comportamento de hoje) e cada linha traz `vinculo`.

**Frontend:** em `/estudantes`, filtro "VÃ­nculo" e identificaÃ§Ã£o do ex-estudante; ao abrir um ex-estudante, a vista reduzida, sem separadores de contactos, documentos nem aÃ§Ãµes de escrita.

**Testes:** estudante que muda de A para B (B nÃ£o vÃª registos de A; A continua a ver os seus; nenhuma vÃª os da outra); `codigos_academia=outra` na consulta Ã© ignorado; academia que nunca teve o estudante recebe `403`; perfil reduzido nÃ£o contÃ©m `email`, `telefone` nem `documentos`; eventos fora do perÃ­odo nÃ£o aparecem; documentos negados Ã  academia anterior; reconstruÃ§Ã£o da projeÃ§Ã£o reproduz os perÃ­odos.

### 6.5.2 Apagar a conta: permitido, com aviso no frontend

**DecisÃ£o:** continua permitido. O estudante sÃ³ pode apagar a prÃ³pria conta quando estÃ¡ `inativo` (regra do domÃ­nio); a academia sÃ³ apaga estudantes `ativo` ou `pendente_documentos`. **Nenhuma regra de backend muda.** O que falta Ã© o aviso, que hoje nÃ£o existe em nenhum dos dois fluxos:

| Fluxo | Onde | Texto atual |
|---|---|---|
| Estudante | `src/app/(painel)/perfil/PageContent.tsx:84-103` (`DangerZoneEstudante`) | "A deleÃ§Ã£o desativa sua conta. O histÃ³rico acadÃªmico de notas e faltas permanece preservadoâ€¦ mas vocÃª nÃ£o conseguirÃ¡ mais entrar no painel." |
| Academia | `src/app/(painel)/estudantes/PageContent.tsx:673-693` | "A conta de {nome} serÃ¡ desativada. Registros acadÃªmicos jÃ¡ lanÃ§ados continuarÃ£o guardados." |

O texto do estudante jÃ¡ promete que ele "nÃ£o conseguirÃ¡ mais entrar no painel", o que sÃ³ passa a ser verdade com a correÃ§Ã£o do SEC-13.

**Aviso a acrescentar (estudante):** antes do botÃ£o de confirmar, um alerta com: *"Ao apagar a conta deixa de poder entrar no painel e, por isso, de consultar ou pagar as suas dÃ­vidas. As dÃ­vidas em aberto continuam registadas na academia {nome}. Os registos acadÃ©micos ficam guardados. Esta aÃ§Ã£o nÃ£o pode ser desfeita."* Se houver dÃ­vidas pendentes, mostrar o nÃºmero e o total e exigir uma caixa de confirmaÃ§Ã£o ("Compreendo que continuo a dever estes valores") para ativar o botÃ£o. Os dados vÃªm dos endpoints de consulta que o painel de pagamentos jÃ¡ usa (`/financeiro/mensalidades/estudante/â€¦` para as mensalidades e, para os serviÃ§os extras, `ListarMinhasInscricoesServicoExtra` e `MinhasPendenciasServicoExtra`); nÃ£o Ã© preciso endpoint novo.

**Aviso a acrescentar (academia):** *"O estudante deixa de poder entrar e de pagar. As inscriÃ§Ãµes ativas em serviÃ§os extras serÃ£o canceladas. As dÃ­vidas em aberto continuam registadas."*, com o nÃºmero e o total de mensalidades pendentes, obtidos pelo mesmo endpoint de consulta (`ConsultarMensalidadesEstudante`, que a academia jÃ¡ usa).

**O que acontece Ã s dÃ­vidas ao apagar:** continuam registadas e visÃ­veis Ã  academia (o estudante apagado mantÃ©m o `codigo_academia`, e a academia mantÃ©m o acesso do 6.5.1). O estudante Ã© que deixa de poder pagÃ¡-las. Ã‰ consequÃªncia direta da decisÃ£o tomada, por isso o aviso Ã© obrigatÃ³rio.

### 6.5.3 ServiÃ§os extras: catÃ¡logo sÃ³ da academia atual e desconexÃ£o ao sair

**CatÃ¡logo.** `ListarServicosExtrasCatalogoEstudante` (`servico_extra_handlers.go:504`) usa `*est.CodigoAcademia`, que nunca Ã© nulo, e por isso mostra o catÃ¡logo da academia anterior a quem estÃ¡ desvinculado. CorreÃ§Ã£o:

```go
if e != nil || est == nil {
	utils.RespondWithForbiddenError(c, "estudante nÃ£o encontrado")
	return
}
if !estudanteVinculado(est.Status) { // desvinculado ou apagado: nÃ£o mostrar nada
	c.JSON(http.StatusOK, gin.H{"servicos_extras": []any{}, "categorias": []any{}})
	return
}
// vinculado: catÃ¡logo da academia atual (*est.CodigoAcademia)
```

Devolve `200` com listas vazias (e nÃ£o `403`), para a interface mostrar "sem serviÃ§os disponÃ­veis" em vez de um erro.

**DesconexÃ£o ao sair.** **Verificado: hoje nada cancela as inscriÃ§Ãµes em serviÃ§os extras** quando o estudante Ã© desvinculado ou apagado. Nem `decidirSolicitacaoStatus` (desvinculaÃ§Ã£o), nem `DeletarPorAcademia`, nem `Deletar` as tocam: ficam `pendente`, `aprovada_pendente_pagamento_taxa_inscricao` ou `vinculada` para sempre. O domÃ­nio jÃ¡ tem os mÃ©todos de cancelamento (`CancelarAntesDaVinculacao`, `Cancelar`); falta chamÃ¡-los.

O helper atual, `cancelarSolicitacaoServicoExtra` (`servico_extra_solicitacao_handlers.go:293`), **escreve a resposta HTTP dentro dele**, por isso Ã© dividido em nÃºcleo reutilizÃ¡vel e wrapper:

```go
func cancelarSolicitacaoServicoExtraCore(ctx context.Context, repo *db.AggregateRepository,
	s *aggregates.SolicitacaoServicoExtra, motivo, por, actor, ip string) error {
	var e error
	switch s.Status {
	case aggregates.StatusInscricaoPendente:
		e = s.CancelarAntesDaVinculacao(motivo, por)
	case aggregates.StatusInscricaoAprovadaPendentePagamentoTaxa:
		// cancela primeiro a cobranÃ§a aberta da taxa de inscriÃ§Ã£o
		if e = FinanceiroService.CancelarCobrancaTaxaInscricaoServicoAberta(ctx, s.GetID().String(), motivo, actor, por, ip); e == nil {
			e = s.CancelarAntesDaVinculacao(motivo, por)
		}
	case aggregates.StatusInscricaoVinculada:
		e = s.Cancelar(motivo, por)
	default:
		return fmt.Errorf("solicitaÃ§Ã£o em estado '%s' nÃ£o pode ser cancelada", s.Status)
	}
	if e != nil {
		return e
	}
	return repo.SaveWithAudit(s, db.AuditContext{UserID: actor, UserType: por, IP: ip})
}
```

E a funÃ§Ã£o de encerramento, idempotente (sÃ³ toca em estados ativos) e que **nunca bloqueia a saÃ­da**:

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

`por` Ã© `"academia"` porque a coluna `cancelada_por` tem uma restriÃ§Ã£o `CHECK` que sÃ³ aceita `'academia'` e `'estudante'`; o ator `sistema:encerramento-vinculo` fica na auditoria. (Alternativa: acrescentar `"sistema"` ao domÃ­nio e a uma migration; nÃ£o necessÃ¡rio.)

| Chamador | Quando | Motivo |
|---|---|---|
| `decidirSolicitacaoStatus` (`academia_status_escolar_handlers.go:286-330`) | Logo depois de gravar a decisÃ£o de `desvinculacao` | `estudante_desvinculado` |
| `DeletarContaEstudantePorAcademia` (`estudante_delecao_academia_handlers.go:71`) | Logo depois de `DeletarPorAcademia` | `conta_deletada` |
| `DeletarContaEstudante` (`estudante_handlers.go`, autoexclusÃ£o) | Defensivo: o estudante jÃ¡ estÃ¡ `inativo`, mas a funÃ§Ã£o Ã© idempotente | `conta_deletada` |

O resumo (`encerradas`, `falhas`) vai na resposta de cada uma. Uma falha numa inscriÃ§Ã£o nÃ£o impede as restantes nem a saÃ­da; como a funÃ§Ã£o sÃ³ toca em estados ativos, pode ser repetida.

**O que NÃƒO se cancela:** as dÃ­vidas jÃ¡ geradas (taxas de inscriÃ§Ã£o e obrigaÃ§Ãµes por pagar) **continuam registadas e pagÃ¡veis** (D4: o estudante `inativo` paga). As inscriÃ§Ãµes canceladas ficam no histÃ³rico do estudante (`cancelada` ou `cancelada_antes_da_vinculacao`), que continua visÃ­vel. A regra do projeto "desvinculaÃ§Ã£o nÃ£o remove notas, faltas, avaliaÃ§Ãµes, documentos, turmas ou eventos" (tarefa 05, linha 250) mantÃ©m-se; passa a existir uma exceÃ§Ã£o documentada: **as inscriÃ§Ãµes em serviÃ§os extras sÃ£o encerradas**.

**Testes:** desvincular encerra as inscriÃ§Ãµes `pendente`, `aprovada_pendente_pagamento_taxa_inscricao` (cancelando a cobranÃ§a aberta) e `vinculada`; apagar pela academia faz o mesmo; as dÃ­vidas continuam pagÃ¡veis; o catÃ¡logo devolve listas vazias a `inativo` e `deletado`; o catÃ¡logo de quem estÃ¡ vinculado mostra sÃ³ a academia atual; uma falha numa inscriÃ§Ã£o nÃ£o impede as outras.

### 6.5.4 Turmas e mensalidades depois da saÃ­da (observaÃ§Ã£o nova, com padrÃ£o assumido)

Ao verificar o que "desconectar" implica, vi que as **mensalidades** nÃ£o sÃ£o geradas por uma lista de estudantes: `ListMensalidades` (`internal/finance/mensalidade.go:403`) e `vinculosMensalidade` (`:681`) derivam-nas da **pertenÃ§a do estudante a turmas** (`projection_turmas.estudantes` e `historico_estudantes_ano_letivo`). A regra do projeto diz que desvincular **nÃ£o remove das turmas**, e nenhum cÃ³digo as remove ao sair. ConsequÃªncia: os meses que faltam do ano letivo em curso **continuam a aparecer como `pendente`** para um estudante que jÃ¡ saiu.

**PadrÃ£o assumido (reversÃ­vel, sem alteraÃ§Ãµes automÃ¡ticas de dinheiro):**
- **NÃ£o** remover o estudante das turmas: como as mensalidades derivam delas, isso faria **desaparecer das listas as dÃ­vidas jÃ¡ existentes**, em conflito com D4 (pagar as suas dÃ­vidas).
- **NÃ£o** anular obrigaÃ§Ãµes automaticamente. Depois de uma desvinculaÃ§Ã£o ou exclusÃ£o, a ficha do estudante na academia mostra um aviso, *"Mensalidades futuras pendentes: N"*, com a aÃ§Ã£o existente de anular (`AnularObrigacoesMensalidade`: sÃ³ meses ainda nÃ£o pagos, e reversÃ­vel com `ReativarObrigacoesMensalidade`).

Se preferir que os meses posteriores Ã  saÃ­da sejam anulados automaticamente, a funÃ§Ã£o de encerramento da 6.5.3 pode chamar `AnularObrigacoesMensalidade` para os meses seguintes ao da saÃ­da; nÃ£o o incluÃ­ por ser uma decisÃ£o financeira, com casos limite (pagamento em curso, mÃªs parcial).

---

# Parte 7 â€” Rate limiters: o que eliminar (D1)

| Local | AÃ§Ã£o |
|---|---|
| `internal/middleware/rate_limit.go` | Apagar as funÃ§Ãµes `GlobalRateLimit`, `LoginRateLimit` e `EmailRateLimit` (e os comentÃ¡rios que as descrevem). **Manter** `RateLimiter`, `NewRateLimiter` e `RateLimitMiddleware` |
| `cmd/server/main.go:286` | Remover `router.Use(middleware.GlobalRateLimit())` |
| `cmd/server/main.go:308, 311, 312, 313` | Remover `middleware.LoginRateLimit()` de `/login`, `/login/dois-fatores/verificar`, `/login/dois-fatores/reenviar` e `/bootstrap` |
| `cmd/server/main.go:333` | Remover `emailGroup.Use(middleware.EmailRateLimit())` |
| `.env.example:179-182` | Remover o comentÃ¡rio e `RATE_LIMIT_GLOBAL`, `RATE_LIMIT_LOGIN`, `RATE_LIMIT_EMAIL` (nenhum cÃ³digo Go as lÃª) |
| `DocumentaÃ§Ã£o da API.md:657, 1106` | Remover as frases "Rate limit ativo" |
| `docs/Tarefas feitas/122 - â€¦md` | Rever as menÃ§Ãµes ao `LoginRateLimit` |

Nenhum teste referencia as trÃªs funÃ§Ãµes. **Efeito:** nenhum na execuÃ§Ã£o (eram no-ops); o risco residual estÃ¡ descrito no SEC-02.

---

# Parte 8 â€” Ordem de execuÃ§Ã£o e verificaÃ§Ã£o

1. **CorreÃ§Ãµes imediatas, independentes do resto:** SEC-13 (bloquear estudante `deletado`), SEC-14 (redigir hashes nos endpoints de eventos) e a metade barata do SEC-15 (forÃ§ar o filtro por `codigo_academia` em notas, faltas e avaliaÃ§Ãµes finais).
2. **PrÃ©-requisitos de produÃ§Ã£o:** confirmar `EMAIL_FROM`, provedor padrÃ£o de SMS e remetente configurados, `FRONTEND_URL` correto.
3. **Base:** migration 135, evento `SenhaTemporariaDefinida` (+ `validEventTypes`), colunas e imposiÃ§Ã£o no `AuthMiddleware`, `GenerateTempPassword`, extraÃ§Ã£o de `enviarSMSComunicacao`.
4. **Estado da conta (6.1 a 6.4):** reorganizar grupos e aplicar `ValidarStatusAcademia`; implementar a regra por mÃ©todo do estudante `inativo` e eliminar o token restrito (backend e frontend); escrever os testes de rotas **antes** de relaxar o login.
5. **VÃ­nculos (6.5.1):** migration 136, alimentaÃ§Ã£o da projeÃ§Ã£o, reconstruÃ§Ã£o da projeÃ§Ã£o de estudantes, helper `acessoAcademiaAoEstudante`, perfil reduzido, filtro de eventos, parÃ¢metro `vinculo` na lista, e o frontend correspondente.
6. **ServiÃ§os ao sair e aviso de apagar conta (6.5.2 e 6.5.3):** dividir o cancelamento em nÃºcleo e wrapper, funÃ§Ã£o de encerramento e os trÃªs chamadores, correÃ§Ã£o do catÃ¡logo, avisos nos dois fluxos de apagar conta.
7. **CriaÃ§Ã£o por terceiros (Parte 3):** os 5 pontos (com o envio por *pool* nos lotes), `RegisterAcademia` com telefone obrigatÃ³rio, admin por Brevo, reenvio de credenciais.
8. **RecuperaÃ§Ã£o por link (Parte 4)** e pÃ¡gina do frontend.
9. **EliminaÃ§Ãµes:** Parte 2 (EmailJS, SMTP, `gerar-token/*`, `GetDefaultPassword`, `GenerateSecurePassword`) e Parte 7 (rate limiters).
10. **Senhas pÃºblicas (Parte 5):** limpar a Ã¡rvore, redefinir admins afetados (3.9), reescrever o histÃ³rico.
11. **Contas existentes (3.9)** e, sÃ³ depois, remover as variÃ¡veis antigas e **revogar** as credenciais do EmailJS/SMTP.

**VerificaÃ§Ã£o final** (os `grep` devem terminar sem resultados, exceto em `docs/`):

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

# histÃ³rico (apÃ³s o filter-repo)
git log --all -S"spuriadm" --oneline        # deve ficar vazio
```

**PadrÃµes assumidos (jÃ¡ refletidos no documento; ajustÃ¡veis sem refazer o plano):**

1. **Reenvio de credenciais (3.8):** academia dona â†’ seus estudantes; `fpp` â†’ estudantes, academias e admins; `adm` e `gerente` sem permissÃ£o.
2. **Estudante `inativo` (6.1 e 6.3.2):** consulta tudo e paga, em qualquer combinaÃ§Ã£o de estado geral e estados acadÃ©micos; escreve apenas na *allowlist* (conta, pagamentos, bilhete, revinculaÃ§Ã£o, telefone do encarregado e **apagar a conta**). Para liberar mais uma aÃ§Ã£o, acrescentar a linha Ã  *allowlist*.
3. **Validade da senha temporÃ¡ria (3.12):** nÃ£o incluÃ­da; Ã© opcional.
4. **Contas existentes (3.9):** o passo 2 (redefinir e enviar SMS em massa) sÃ³ deve correr depois de aprovado o custo, a partir da contagem do *dry-run*.
5. **Academia anterior (6.5.1):** consulta apenas o que ela registou durante o seu vÃ­nculo (decidido). Perfil reduzido, sem documentos e sem escrita; o `codigo_academia` do estudante **nÃ£o Ã© limpo** (significa "academia do Ãºltimo vÃ­nculo").
6. **Apagar conta (6.5.2):** permitido, com os avisos descritos; nenhuma regra de backend muda.
7. **ServiÃ§os ao sair (6.5.3):** catÃ¡logo sÃ³ da academia atual; ao desvincular ou apagar, as inscriÃ§Ãµes ativas em serviÃ§os extras sÃ£o canceladas. As dÃ­vidas geradas continuam pagÃ¡veis.
8. **Turmas e mensalidades depois da saÃ­da (6.5.4):** sem alteraÃ§Ãµes automÃ¡ticas de dinheiro nem remoÃ§Ã£o das turmas; a academia vÃª um aviso e decide anular os meses futuros.

