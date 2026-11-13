# Tarefa 123 — Segurança SEC-01 a SEC-07: credenciais temporárias, recuperação por link e Brevo como único canal (backend)

**Estado:** pendente
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `4759474` (`4759474ed960f1db46683c7a5ecf88345aa318f0`, tarefa 122 já aplicada)
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório (mais a lista de 4 arquivos a apagar)
**Ordem de deploy:** **configurar `EMAIL_FROM` e `BREVO_API_KEY`; depois backend (esta tarefa); logo em seguida a Tarefa 34 do `rastreio-frontend`, na mesma janela.** Detalhes na secção "Ordem de deploy".

---

## Contexto

Pedido: implementar metade da "Parte 1 — Diagnóstico de segurança" do documento `seguranca-e-migracao-brevo.md` — **SEC-01 a SEC-07** — com tudo o que as Partes 2 a 5 e 7 do plano definem para esses itens.

| ID | Problema (resumo) | O que esta tarefa faz |
| --- | --- | --- |
| SEC-01 | `gerar-token/*` devolvia o token ao chamador sem autenticação (tomada de conta) | Rotas e handlers eliminados; `verificar-email/solicitar` passou a exigir login |
| SEC-02 | `GlobalRateLimit`, `LoginRateLimit` e `EmailRateLimit` eram no-ops | Código morto eliminado (risco residual declarado: não há limite de taxa no login) |
| SEC-03 | Recuperação trocava a senha no pedido, sem prova de posse do e-mail | Recuperação por **link** de uso único (1 hora); a senha só muda no passo do link |
| SEC-04 | Senha inicial = código do utilizador, sem troca obrigatória | Senha temporária aleatória de 8 caracteres, entrega por SMS/e-mail, **troca obrigatória** imposta no servidor |
| SEC-05 | Senhas padrão de admin em repositórios públicos | Removidas do código, testes, `.env.example` e doc; rotação das contas via comando novo; reescrita do histórico validada (ver "Histórico do Git") |
| SEC-06 | Enumeração de contas (recuperação e login) | Resposta única e genérica nos dois fluxos |
| SEC-07 | Segredos (links com token, senhas) gravados em logs | Falha fechada; nenhum segredo em log (guardas automáticas); única exceção explícita: `CREDENCIAIS_LOG_DEV` |

**Descobertas durante a análise do código (moldaram o desenho):**

- O envio de SMS vivia **dentro** do handler `EnviarMensagemComunicacao`, acoplado ao `*gin.Context`; foi preciso extraí-lo (sem mudar a lógica) para ser reutilizável.
- `processarCadastroEstudanteBatch` (o "lote síncrono" que o plano 3.6 manda diferir) **não tem nenhum chamador**: em produção só existe o cadastro em lote assíncrono (`/register/async`), em que cada item passa pelo worker. Por isso o envio é por item, dentro do worker, e o arquivo `batch_handlers.go` **não foi alterado**. O plano dá a entender o contrário; está registado aqui para não haver surpresa.
- `AlterarSenha` exigia estudante `ativo`: um estudante recém-criado (pendente de documentos) não conseguiria trocar a senha temporária. A exigência foi removida (o plano 3.7 já a previa).
- Estudante inativo recebe **token restrito ao financeiro**, que bloqueia `PUT /alterar-senha`: sem tratamento, ficaria sem saída. A allowlist da troca obrigatória também vale para o token restrito, só enquanto a flag estiver ligada.
- `GET /academias` é público (autenticação opcional): o indicador `deve_alterar_senha` só é incluído quando o chamador é admin.
- `EMAIL_USER` era fallback do remetente do Brevo (`loadBrevoConfig`). Foi eliminado (D7): **sem `EMAIL_FROM` o Brevo fica desabilitado** — ver "Ordem de deploy".

## Interpretações adotadas (onde o pedido era aberto)

1. **"Metade da Parte 1 (SEC-01–SEC-07)"** = esses 7 itens completos. Entram também, por serem condição para o SEC-04 funcionar com segurança: o reenvio de credenciais (3.8), o comando para contas existentes (3.9/5.3), a eliminação de EmailJS/SMTP (D7, que SEC-01 e SEC-07 exigem) e o mecanismo `sessoes_validas_desde` (parte do SEC-10, indispensável ao reenvio e ao reset por link).
2. **SEC-09** (consumo atómico e hash dos tokens de `auth_tokens`) **não** foi feito: `ResetarSenha` mantém a lógica de consumo atual, só passa a gravar o motivo `reset_senha`.
3. **Rotas novas de reenvio** seguem os caminhos do plano: `POST /estudantes/:codigo/credenciais/reenviar`, `POST /dominis/academias/:codigo/credenciais/reenviar`, `POST /dominis/administradores/:id/credenciais/reenviar`.
4. **Estudante sem nível definido:** o telefone de destino é o do estudante se existir; senão, o do encarregado (o plano diz "o que existir").
5. **E-mail de verificação sem Brevo:** devolve 503 e não escreve o link no log (antes devolvia 200). Em desenvolvimento a exceção `CREDENCIAIS_LOG_DEV` cobre credenciais, **não** links de verificação.
6. **Login por e-mail ainda não verificado:** só é comunicado depois de a senha estar correta; com senha errada a resposta é a genérica.
7. **Senha temporária no log:** `CREDENCIAIS_LOG_DEV=true` só escreve em `ENV` diferente de `production`; em produção a flag é ignorada.
8. **Risco aceite pelo dono do projeto (plano 3.6):** como o SMS passa pelo envio padronizado, a senha temporária fica no conteúdo da mensagem (evento `MensagemComunicacaoRegistrada` e histórico de mensagens), visível à academia e aos admins. Mantido como decidido.

## Decisões de design

- **Event sourcing.** Novo evento `SenhaTemporariaDefinida`, com o mesmo nome nos três agregados (como `EmailVerificado` e os eventos do 2FA; as projeções filtram por `aggregate_type`). O estado `deve_alterar_senha` é consequência do ledger: é ligado por esse evento e desligado/religado pelo **motivo** do evento de troca de senha, então um rebuild o reconstrói.
- **Motivos do evento de troca de senha:** `alteracao_usuario` (também assumido para eventos antigos sem motivo; desliga a flag), `reset_senha` (desliga a flag e invalida sessões), `credenciais_reenviadas` (liga a flag e invalida sessões). O evento de senha do estudante ganhou o campo opcional `Motivo` (`omitempty`), como já tinham os de Admin e Academia; eventos antigos continuam legíveis.
- **`sessoes_validas_desde`** vem do `OccurredAt` do evento, nunca de `CURRENT_TIMESTAMP` (um rebuild não pode invalidar todas as sessões). A comparação com o `iat` do JWT é feita em **segundos**: um token emitido no mesmo segundo do evento não é descartado, para o titular que redefine a senha e entra de imediato não ser expulso; o inverso (token emitido no mesmo segundo, antes do evento) sobrevive por no máximo 1 s.
- **Troca obrigatória imposta no servidor** (`AuthMiddleware`, uma única consulta traz status, flag e `sessoes_validas_desde`): com a flag ligada só passam `GET /meu-perfil`, `PUT /alterar-senha` e `POST /logout` (método **e** caminho exatos); o resto devolve `403` com `"code": "SENHA_TEMPORARIA"`. `OptionalAuthMiddleware` trata a sessão com flag como anónima.
- **Senha temporária:** 8 caracteres de `crypto/rand`, ao menos uma maiúscula, uma minúscula e um dígito, sem os ambíguos `I l O o 0 1` (cabe em `ValidateSenha`, 8 a 128). Só o hash vai para o ledger.
- **Entrega após o commit:** o evento é gravado primeiro e o SMS sai depois; falha de envio nunca desfaz a criação — a resposta traz `credenciais: { canal, enviada, motivo? }` e a conta fica elegível ao reenvio. O SMS só sai com `ENV=production` (mesma convenção do 2FA): fora disso não envia nem regista mensagem.
- **Telefone validado antes de criar a conta** (formato móvel `9XXXXXXXX`, aceita `+244` e espaços): superior → telefone do estudante; escolar → telefone do encarregado. Na matrícula paga (webhook) o telefone já foi validado na aprovação; se mesmo assim falhar, a conta nasce e o aviso vai para o log (sem telefone nem senha).
- **Reenvio:** gera nova senha temporária, grava o evento com motivo `credenciais_reenviadas`, envia pelo canal do tipo de conta e exige intervalo de 10 minutos por conta, medido por `sessoes_validas_desde`. A ação é auditada com o executor real.
- **Recuperação por link:** resposta sempre idêntica (200, mesma mensagem), e-mail enviado em segundo plano só a contas com e-mail cadastrado **e verificado**; `503` apenas quando o canal de e-mail inteiro está indisponível (independe da conta, logo não revela nada).
- **Brevo único:** `EmailJS` e `SMTP` eliminados (arquivos, variáveis, testes). `BrevoAPIURL` é uma variável exportada para os testes apontarem a um servidor falso.
- **Defesa em profundidade:** as rotas de reenvio de academia e administrador têm `RequireFPP()` na rota **e** a verificação no handler (os dois foram desfeitos juntos no teste de mutação).

## O que foi atualizado e onde

**29 arquivos alterados + 20 novos + 4 a apagar** (mais este documento).

**Arquivos a APAGAR** (listados também em `backend_apagar.txt`; apagar antes de rodar os testes):
`internal/services/smtp_mailer.go`, `internal/services/smtp_mailer_test.go`, `internal/services/smtp_mailer_e2e_test.go`, `internal/services/academia_cadastrada_email_test.go`.
Nenhum arquivo foi renomeado ou movido de pasta. `go.mod`/`go.sum` **não** mudaram (nenhuma dependência nova).

### Banco, domínio e projeções

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `migrations/135_senha_temporaria.sql` (**novo**) | — | Colunas `deve_alterar_senha BOOLEAN NOT NULL DEFAULT FALSE` e `sessoes_validas_desde TIMESTAMPTZ` em `projection_admins`, `projection_academias` e `projection_estudantes` (`IF NOT EXISTS`; contas antigas ficam `FALSE`/`NULL`). Número 135 livre em `main`. |
| `internal/domain/aggregates/senha_temporaria.go` (**novo**) | constantes de motivo (24); `SenhaTemporariaDefinidaEvent` (33) | Motivos `alteracao_usuario`, `reset_senha`, `credenciais_reenviadas` e o evento compartilhado. |
| `internal/domain/aggregates/admin.go` | `Apply` (85); `DefinirSenhaTemporaria` (354) | Aceita o novo evento; comando que o gera. |
| `internal/domain/aggregates/academia.go` | `Apply` (118); `DefinirSenhaTemporaria` (464) | Idem para Academia. |
| `internal/domain/aggregates/estudante.go` | `Apply` (132); `SenhaAlteradaEvent.Motivo` (368); `AlterarSenhaComMotivo` (648); `DefinirSenhaTemporaria` (664) | Idem; `AlterarSenha` mantém a assinatura e delega com motivo `alteracao_usuario`. |
| `internal/db/safe_queries.go` | lista de tipos válidos (63) | `SenhaTemporariaDefinida` registado. |
| `internal/projections/senha_temporaria.go` (**novo**) | `setSenhaTemporaria` (19); `aplicarSenhaAlterada` (38) | Aplicam flag e `sessoes_validas_desde` conforme o motivo, usando o `OccurredAt` do evento. |
| `internal/projections/admin_projection.go` | tabela de handlers (65); `handleAdminSenhaAlterada` (341); `handleSenhaTemporariaDefinida` (359) | Usam os helpers acima. |
| `internal/projections/academia_projection.go` | tabelas de handlers normal e transacional (49, 85); `handleAcademiaSenhaAlterada` (748); handlers do novo evento (765, 769) | Idem. |
| `internal/projections/estudante_projection.go` | `Handle` (102); `handleSenhaAlterada` (894) | Idem. |

### Serviços (e-mail e senha temporária)

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `internal/services/senha_temporaria.go` (**novo**) | `GenerateTempPassword` (33); `NomeCurtoSMS` (74); `MensagemCredenciaisSMS` (98); `MensagemCredenciaisReenviadasSMS` (104); `CredenciaisLogDevAtivo` (112); `LogCredenciaisDev` (120) | Gerador único de senhas; textos ASCII de um segmento SMS; flag de log de desenvolvimento (nunca em produção). Sem dependência nova (transliteração própria). |
| `internal/services/email_service.go` | `NewEmailService` (30); `SendVerificationEmail` (115); `SendPasswordResetLinkEmail` (147); `SendAdminWelcomeEmail` (177); `SendAcademiaCadastradaEmailBrevo` (219) | Reescrito só com Brevo. Saíram: EmailJS, SMTP, `GenerateSecurePassword`, `GetDefaultPassword`, `SendPasswordResetEmail`. Sem Brevo: `ErrEmailNaoConfigurado` e **nenhum** segredo no log; verificação não grava token se não houver canal. |
| `internal/services/brevo_mailer.go` | `BrevoAPIURL` (18); `BrevoConfigurado` (29); `loadBrevoConfig` (37) | Fallback `EMAIL_USER` removido; helper para os handlers responderem 503; URL sobrescrevível em teste. |
| `internal/services/email_html_templates.go` | `renderPasswordResetLinkEmailHTML/Text` (323, 353); `renderAdminWelcomeEmailHTML/Text` (372, 414) | Template de recuperação **por link** (sem senha) e de boas-vindas do admin (link `{FRONTEND_URL}/login`, texto de troca obrigatória); template antigo "reset com senha" removido. |
| `internal/services/smtp_mailer*.go`, `academia_cadastrada_email_test.go` | — | **Apagados** (D7). |

### Middleware e handlers

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `internal/middleware/auth.go` | `CodigoSenhaTemporaria` (108); `rotaTrocaSenhaObrigatoria` (113); `sessaoInvalidadaPorReset` (130); `AuthMiddleware` (160); `OptionalAuthMiddleware` (256); `estadoUsuario` (318); `consultarEstadoUsuario` (338) | Uma consulta traz status, flag e `sessoes_validas_desde`; 403 `SENHA_TEMPORARIA` fora da allowlist; 401 para sessão anterior ao reset/reenvio; allowlist também vale para o token restrito do estudante inativo. |
| `internal/middleware/rate_limit.go` | comentário do `RateLimiter` (11) | Removidas `GlobalRateLimit`, `LoginRateLimit`, `EmailRateLimit` (SEC-02). O limitador usado nas rotas financeiras públicas permanece. |
| `cmd/server/main.go` | `/login` e rotas de 2FA/bootstrap (307); grupo `/email` (336); rotas de reenvio (429, 705, 706) | Sem limitadores; rotas `gerar-token/*` removidas; `verificar-email/solicitar` movida para grupo com `AuthMiddleware`; três rotas de reenvio registadas. |
| `internal/handlers/auth_email_handlers.go` | `SolicitarVerificacaoEmail` (188); `mensagemRecuperacaoSenha` (251); `SolicitarRecuperacaoSenha` (274); `ResetarSenha` (512) | `GerarTokenVerificacao/Recuperacao` e helpers mortos eliminados; recuperação por link com resposta única; 503 sem canal de e-mail; reset do estudante grava motivo `reset_senha`. |
| `internal/handlers/auth_handlers.go` | `mensagemCredenciaisInvalidas` (16); `emailNaoVerificado` (70); ramo de senha incorreta (181); `deve_alterar_senha` no login (269); `deveAlterarSenhaDe` (283) | Mensagem única para usuário inexistente e senha errada; "e-mail não verificado" só após senha correta; login (inclusive o passo do 2FA) devolve `deve_alterar_senha`. |
| `internal/handlers/profile_handlers.go` | `AlterarSenha` (36); perfis (260, 301, 339) | Recusa nova senha igual à atual; não exige mais estudante `ativo`; `GET /meu-perfil` devolve `deve_alterar_senha` nos três tipos. |
| `internal/handlers/comunicacao_envio.go` (**novo**) | `atorComunicacao` (28); `enviarViaProvedor` (37); `enviarSMSComunicacao` (59); ganchos de teste (129, 140) | Envio de SMS extraído do handler, sem mudança de lógica (provedor padrão, fallback para o outro, registo da mensagem). |
| `internal/handlers/comunicacao_handlers.go` | `EnviarMensagemComunicacao` (227) | Passa a chamar `enviarSMSComunicacao`; respostas e códigos HTTP iguais. |
| `internal/handlers/credenciais.go` (**novo**) | `resultadoCredenciais` (38); `enviarCredenciaisSMS` (48); `TelefoneDestinoCredenciaisEstudante` (76); `telefoneDestinoCredenciaisAcademia` (101) | Objeto `credenciais` das respostas; envio só em produção; escolha e validação do telefone de destino. |
| `internal/handlers/credenciais_reenvio_handlers.go` (**novo**) | intervalo (36); `verificarIntervaloReenvio` (43); `ReenviarCredenciaisEstudante` (75); `ReenviarCredenciaisAcademia` (178); `ReenviarCredenciaisAdmin` (257); `anexarDeveAlterarSenha` (340) | Reenvio de credenciais (3 rotas) e indicador `deve_alterar_senha` das listas. |
| `internal/handlers/estudante_handlers.go` | telefone de destino (203); senha (272); evento (342); envio (367); resposta (369); lista (573, 613) | Cadastro de estudante: telefone validado antes de criar, senha temporária, evento, SMS depois do commit, `data.credenciais`, indicador na lista. |
| `internal/handlers/academia_handlers.go` | `RegisterAcademia`: telefone (94); senha (156); evento (188); envio (235); `ListarTodasAcademias` (980) | `telefone` **obrigatório**; senha temporária por SMS; indicador só para admin. `RegisterAcademiaPublica` não mudou (a academia escolhe a senha). |
| `internal/handlers/admin_handlers.go` | `RegisterAdmin`: senha (334); evento (352); resposta (410); lista (481) | Senha temporária por e-mail (Brevo); aviso `email_nao_enviado` agora também com objeto `credenciais`; indicador na lista. |
| `internal/handlers/solicitacao_matricula_handlers.go` | aprovação (369, 402, 417, 444); matrícula paga (610, 622, 647, 650) | Telefone validado antes de aprovar; senha temporária nos dois caminhos; resposta da aprovação traz `credenciais`. |
| `internal/utils/codigo_estudante.go` | `GenerateCodigoEstudante` (13); `GenerateUniqueCodigoEstudante` (32) | Código deixou de ir para os logs (era a senha inicial). |

### Comando para contas existentes (plano 3.9 / 5.3)

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `internal/credmigracao/credmigracao.go` (**novo**) | `Opcoes` (27); `Executar` (64) | Analisa estudantes e academias com senha = código e administradores com senha pública. **Dry-run por padrão.** Passo 1 (`-marcar-contas`): emite `SenhaTemporariaDefinida` (grátis; o titular é obrigado a trocar no próximo login). `-redefinir-admins`: redefinição imediata com senha nova por e-mail e sessões invalidadas; recusa-se a correr sem Brevo configurado (não deixa admin sem acesso). Idempotente. |
| `cmd/migrar-credenciais/main.go` (**novo**) | `main` (29) | Invólucro de linha de comando. As senhas públicas a procurar vêm da variável `SENHAS_PUBLICAS_ADMIN` (fora do código, para o repositório não voltar a contê-las). |

### Configuração, documentação e testes

| Arquivo | O que mudou |
| --- | --- |
| `.env.example` | Blocos EmailJS e SMTP e `RATE_LIMIT_*` removidos; bloco do Brevo reescrito (único canal; `EMAIL_FROM` sem fallback); `CREDENCIAIS_LOG_DEV` documentada. |
| `Documentação da API.md` | Versão 2.10.0; secção de senhas e allowlist da troca obrigatória; login com `deve_alterar_senha` e erros genéricos; `alterar-senha` (8 a 128, diferente da atual); secção de e-mail (Brevo único, sem `gerar-token`, recuperação por link, 503); cadastros com `telefone` obrigatório (academia) e objeto `credenciais`; três rotas de reenvio; indicador nas listas. |
| `internal/domain/aggregates/remetente_comunicacao_test.go` | `spuri123` (falso positivo de SEC-05) trocado por identificador neutro. |
| `internal/handlers/academia_register_publica_test.go` | Guarda atualizada: a pública não usa senha temporária. |
| `internal/services/brevo_mailer_test.go`, `email_html_templates_test.go` | Ajustados ao novo comportamento (sem fallback `EMAIL_USER`; templates de link e de admin). |
| `internal/handlers/auth_email_handlers_integration_test.go` | Reescrito para a recuperação por link (ver validação). |
| Testes **novos** | `internal/services/senha_temporaria_test.go`, `email_service_seguranca_test.go`; `internal/domain/aggregates/senha_temporaria_test.go`; `internal/middleware/senha_temporaria_test.go`; `internal/handlers/credenciais_test.go`, `seguranca_codigo_test.go`, `senha_temporaria_projecao_integration_test.go`; `cmd/server/credenciais_temporarias_integration_test.go`, `credenciais_reenvio_integration_test.go`, `credenciais_migracao_integration_test.go`, `rotas_seguranca_integration_test.go`. |

### Contrato da API (resumo)

- `POST /login` (e o passo do 2FA) → além do que já devolvia: `"deve_alterar_senha": boolean`. Falha: `401` `"credenciais inválidas"` para usuário inexistente **e** senha errada.
- Qualquer rota autenticada com a flag ligada, fora de `GET /meu-perfil`, `PUT /alterar-senha`, `POST /logout` → `403` `{ "error": "FORBIDDEN", "message": "...", "code": "SENHA_TEMPORARIA" }`. Sessão anterior a reset/reenvio → `401` `"sessão encerrada: faça login novamente"`.
- `POST /email/recuperar-senha/solicitar` → `200` sempre `{ "success": true, "message": "Se existir uma conta elegível, enviámos as instruções para o e-mail cadastrado." }`; `422` corpo inválido; `503` Brevo indisponível. `POST /email/recuperar-senha/:token` com `{ "nova_senha" }` (8 a 128).
- `POST /email/verificar-email/solicitar` exige `Authorization`; `503` sem Brevo. `POST /email/gerar-token/*` deixa de existir (404).
- Cadastros (estudante, academia, aprovação de matrícula): resposta passa a trazer `credenciais: { "canal": "sms", "enviada": bool, "motivo"?: "ambiente_nao_producao" | "provedor_nao_configurado" | "falha_envio" }`; admin: `"canal": "email"` e `"motivo": "canal_indisponivel"`. `POST /dominis/academia/cadastro` exige `telefone` móvel (`422` se faltar/inválido); cadastro de estudante/aprovação devolvem `422` sem telefone móvel de destino.
- Reenvio: `POST /estudantes/:codigo/credenciais/reenviar` (academia dona ou fpp), `POST /dominis/academias/:codigo/credenciais/reenviar` e `POST /dominis/administradores/:id/credenciais/reenviar` (fpp) → `200 { message, credenciais }`; `403`, `404`, `422`, `429` (com `retry_after_segundos`), `503` (admin sem Brevo).
- Listas de estudantes, academias (só admin) e administradores: cada item traz `deve_alterar_senha`.

## Validação realizada

**Ambiente (sandbox):** Go 1.24.13 e PostgreSQL 16.15 reais (apt), Node 22 no frontend. As dependências do Go foram resolvidas direto do GitHub por um arquivo `go.mod` **só do sandbox** (o proxy do Go é inalcançável aqui); o `go.mod`/`go.sum` do repositório não foram tocados.

**Baseline de `main` (sem a mudança), `ENV=test RUN_POSTGRES_INTEGRATION=1`, `-p 1`:**

| Execução | Resultado |
| --- | --- |
| 1ª, banco novo | todos os pacotes `ok`; 1065 `PASS`, 0 `FAIL`, 2 `SKIP` (`TestHTTPIntegrationCorrigirNotaRecalculaAvaliacaoFinal`, `TestFaltasPeriodo15HistoricaSemPeriodoListavelECorrigivel`, sempre pulados) |
| 2ª, **mesmo** banco | `FAIL` em `cmd/server`: **25 `FAIL`** (1032 `PASS`, 2 `SKIP`) — ver "Falha antiga" |

**Clone novo de `main` (`4759474`) com a pasta copiada por cima e os 4 arquivos apagados:**

```
gofmt -l .      → (vazio)
go build ./...  → ok
go vet ./...    → ok
go test ./... (1ª execução, banco novo)
ok  spuri/cmd/server  19.6s     ok  spuri/internal/db            0.3s
ok  spuri/internal/domain/aggregates  0.0s     ok  spuri/internal/finance  8.1s
ok  spuri/internal/handlers  19.5s              ok  spuri/internal/middleware 0.0s
ok  spuri/internal/projections  0.1s            ok  spuri/internal/security  0.0s
ok  spuri/internal/services  0.6s               ok  spuri/internal/storage   0.0s
ok  spuri/internal/utils  0.0s
→ 1097 PASS, 0 FAIL, 2 SKIP (os mesmos 2 de main)
```

- As migrations rodam do zero (143 arquivos, incluindo a 135) e a 2ª execução as encontra como "Já aplicada".
- **2ª execução no mesmo banco:** `go build`/`vet` iguais; os pacotes `handlers`, `services`, `middleware`, `domain/aggregates`, `projections`, `db`, `finance`, `security`, `storage` e `utils` ficam `ok`. Em `cmd/server` há **36 `FAIL` = 25 de `main` + 11 dos testes novos**; as 11 falham todas pela **mesma causa raiz antiga** (fixture compartilhado `setupTurmaVinculoIntegration`, "rebuild turmas … viola `projection_turmas_codigo_academia_fkey`") e nenhuma por regra de negócio — mesma assinatura que já derruba os `TestTurmaVinculo*` em `main`. Ou seja: **na 2ª execução esses 11 testes não chegam a avaliar a regra.** Os demais testes novos (handlers, serviços, middleware, agregados, projeções) passam na 2ª execução.

**Fluxo ponta a ponta exercitado com o router real (`setupRouter`), PostgreSQL real e o caminho real de event sourcing:** cadastro de estudante → SMS capturado → login com a senha temporária (`deve_alterar_senha: true`) → a antiga "senha = código" falha (401) → rota fora da allowlist `403 SENHA_TEMPORARIA` → `/meu-perfil` ok → nova senha igual à atual recusada → troca aceite → acesso liberado → flag `false`. Também: reenvio pela academia dona (outra academia e o próprio estudante recebem 403; sessão anterior recebe 401; 2º reenvio imediato recebe 429), estudante inativo (token restrito) conclui a troca, reenvio de academia e de administrador só por `fpp`, comando de contas existentes (dry-run, marcação idempotente, redefinição de admins com e-mail capturado), listas com o indicador, e rotas `gerar-token` inexistentes.

**Ciclo "desfazer a regra → teste falha → restaurar"** (32 reversões; cada uma aplicada, o teste executado e o arquivo restaurado):

| Regra desfeita | Teste que falhou |
| --- | --- |
| Senha temporária com 6 caracteres | `TestGenerateTempPasswordFormato` |
| Bloqueio `SENHA_TEMPORARIA` desligado | `TestCredenciaisTemporarias01CadastroEnviaSenhaAleatoriaPorSMSEExigeTroca` |
| Allowlist aceita qualquer método | `TestRotaTrocaSenhaObrigatoriaAllowlistExata` |
| Estudante criado sem evento `SenhaTemporariaDefinida` | `TestCredenciaisTemporarias01…` |
| Nova senha igual à atual aceita | `TestCredenciaisTemporarias01…` |
| Telefone de destino não validado | `TestCredenciaisTemporarias02TelefoneDeDestinoInvalidoNaoCriaConta` |
| SMS enviado fora de produção | `TestCredenciaisTemporarias04ForaDeProducaoNaoEnviaNemRegistaMensagem` |
| Sessões anteriores não invalidadas (SEC-10) | `TestCredenciaisReenvio01EstudantePelaAcademiaInvalidaSessoesEExigeNovaTroca` |
| Reenvio sem intervalo de 10 minutos | `TestCredenciaisReenvio01…` |
| Reenvio ignora a academia dona | `TestCredenciaisReenvio01…` |
| Allowlist não vale para o token restrito | `TestCredenciaisReenvio02EstudanteInativoCompleteATrocaComSessaoRestrita` |
| `AlterarSenha` volta a exigir estudante ativo | `TestCredenciaisReenvio02…` |
| Reenvio de academia sem `fpp` (rota **e** handler) | `TestCredenciaisReenvio03AcademiaEAdministradorSoParaFPP` |
| Login revela usuário inexistente | `TestCredenciaisReenvio03…` |
| Senha errada com mensagem própria | `TestCredenciaisReenvio03…` |
| "E-mail não verificado" dito antes de conferir a senha | `TestCredenciaisReenvio03…` |
| "E-mail não verificado" nunca dito | `TestCredenciaisReenvio03…` |
| Listas sem o indicador | `TestCredenciaisListasIndicamContasComSenhaTemporaria` |
| Projeção: `reset_senha` não invalida sessões | `TestIntegrationProjecoesAplicamSenhaTemporariaEMotivos` |
| Projeção: `credenciais_reenviadas` não liga a flag | `TestIntegrationProjecoesAplicamSenhaTemporariaEMotivos` |
| Recuperação envia e-mail a conta não verificada | `TestIntegrationSolicitarRecuperacaoSenhaRespostaIdenticaParaQualquerConta` |
| Recuperação sem Brevo não devolve 503 | `TestIntegrationSolicitarRecuperacaoSenhaSemBrevoDevolve503` |
| Verificação sem Brevo volta a logar o link | `TestSendVerificationEmailSemBrevoFalhaFechadoSemLogarLink` |
| Admin sem Brevo volta a logar a senha | `TestSendAdminWelcomeEmailSemBrevoNaoLogaSenha` |
| `CREDENCIAIS_LOG_DEV` vale em produção | `TestCredenciaisLogDevNuncaEmProducao` |
| `EMAIL_USER` volta como remetente | `TestLoadBrevoConfigIgnoresEmailUser` |
| Rota `gerar-token` reintroduzida | `TestSeguranca01RotasGerarTokenNaoExistem` |
| `verificar-email/solicitar` sem autenticação | `TestSeguranca01SolicitarVerificacaoEmailExigeAutenticacao` |
| Limitador desativado reintroduzido | `TestSeguranca02LimitadoresDesativadosForamRemovidos` |
| Referência a `GetDefaultPassword` volta ao código | `TestSegurancaNaoExisteSenhaPadraoNemCanalDeEmailForaDoBrevo` |
| Log de senha volta ao código | `TestSegurancaLogsNaoEscrevemSegredos` |
| Comando de migração marca contas sem filtrar a senha | `TestMigracaoCredenciaisCorrigeContasComSenhaPrevisivel` |

Nota honesta sobre o ciclo: a primeira tentativa de desfazer "reenvio de academia sem fpp" **sobreviveu** quando só o handler foi alterado — porque o `RequireFPP()` da rota já barra antes (defesa em profundidade). Só desfazendo os dois ao mesmo tempo o teste cai, como mostra a linha acima. Outras 5 mutações iniciais não compilavam e foram reescritas para compilar antes de contar.

**Histórico do Git (SEC-05):** o comando do plano (`git filter-repo --replace-text`) foi **validado em clones espelho descartáveis dos dois repositórios, sem push**: antes, 11 commits do backend e 2 do frontend continham `spuriadm`; depois, 0 commits contêm qualquer das quatro senhas, nos dois. Nada foi enviado ao GitHub.

**Não foi validado:** SMS real e Brevo real (a rede do sandbox só alcança servidores falsos locais; o conteúdo dos e-mails e SMS foi verificado nos servidores falsos); comportamento sob `ENV=production` completo (os testes ligam só `ENV=production` na parte do envio); desempenho do comando de migração com milhares de contas (as comparações bcrypt correm em paralelo, mas não foram medidas em volume).

## Falha antiga que não é desta tarefa

Em `main`, rodar a suíte uma **segunda vez no mesmo banco** já falha em `cmd/server`: 25 testes — 11 `TestTurmaVinculo*`, 12 `TestFaltasPeriodo*`, `TestHTTPIntegrationCorrigirNotaEFalta` e `TestIntegrationRebuildNotasEFaltasMantemRegistrosCorrigidos`. Causa: os fixtures reconstroem as projeções reproduzindo **todos** os eventos já gravados no banco; eventos deixados por execuções anteriores (de academias que o próprio teste apaga) violam uma FK ao serem reproduzidos. Os testes novos de `cmd/server` reutilizam o mesmo fixture e herdam a falha na 2ª execução (ver acima). Em banco descartável novo (o uso normal) tudo passa. Não foi corrigido aqui por estar fora do escopo.

Também não relacionado: o repositório tem uma pasta `data/` criada pelos testes que não está no `.gitignore` — não a commitar.

## Ordem de deploy

1. **Antes de tudo, no ambiente de produção (Render):** definir `EMAIL_FROM` (remetente **verificado no Brevo**) e `BREVO_API_KEY`. Como o fallback para `EMAIL_USER` foi eliminado, **se hoje o remetente só vem de `EMAIL_USER`, o Brevo ficará desabilitado depois do deploy** (2FA, verificação de e-mail e recuperação passam a falhar com 503). Confirmar também `ENV=production` (SMS só sai em produção) e o provedor de SMS padrão em `/comunicacao`.
2. **Backend (esta tarefa).** A migration 135 é aplicada no arranque; só acrescenta colunas.
3. **Logo em seguida, a Tarefa 34 do `rastreio-frontend`**, na mesma janela. O frontend atual **não quebra** com o novo backend para contas existentes (nenhuma está marcada), mas até o deploy do frontend: (a) contas **criadas** nesse intervalo receberão senha temporária e, com o frontend antigo, o titular entraria e receberia 403 sem tela de troca; (b) os links do e-mail de recuperação apontam para `/recuperar-senha/<token>`, que só existe no frontend novo. Por isso: **não criar contas e não rodar o comando de migração entre os dois deploys.**
4. Só depois dos dois deploys: rodar o comando de contas existentes (abaixo).

Comando de contas existentes (a partir da raiz do backend, com `DATABASE_URL` do banco desejado; **dry-run por padrão**):

```bash
go run ./cmd/migrar-credenciais                          # só conta
go run ./cmd/migrar-credenciais -marcar-contas           # passo 1: obriga a troca em estudantes/academias com senha = código
SENHAS_PUBLICAS_ADMIN='<as 4 senhas, separadas por vírgula>' \
  go run ./cmd/migrar-credenciais -redefinir-admins      # rotação imediata dos admins (exige Brevo configurado)
```

O passo 2 do plano 3.9 (redefinir e reenviar por SMS, em lotes e com custo aprovado) **não** é feito pelo comando: usa-se o botão "Reenviar credenciais" da interface, e o indicador `deve_alterar_senha` das listas mostra quem ainda falta.

## Como aplicar

1. **Confirme que a base não mudou:** `git fetch && git diff --stat 4759474 origin/main -- .` (ou, com a base já local, `git diff --stat 4759474 HEAD -- <arquivos que serão substituídos>`). Vazio = pode substituir; se aparecer algum arquivo, ele mudou depois da base — não substitua e avise. Para ver só a divergência dos arquivos que esta tarefa toca: `git diff --stat 4759474 HEAD -- cmd internal migrations .env.example "Documentação da API.md"`.
2. **Apague** os 4 arquivos listados em "Arquivos a APAGAR".
3. **Copie** a pasta `rastreio-backend/` para a raiz do repositório, mantendo os caminhos.
4. **Verifique** (abaixo).
5. **Marque como feito** (abaixo).

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

Esperado: `gofmt -l .` vazio; build e vet limpos; todos os pacotes `ok`. Os testes `TestIntegration…` e os de `cmd/server` só correm de verdade com PostgreSQL de teste **descartável e novo**: `RUN_POSTGRES_INTEGRATION=1 SPURI_RUN_DB_INTEGRITY_TESTS=1 DATABASE_URL='postgres://…?sslmode=disable' go test -p 1 ./...`. Numa 2ª execução no mesmo banco, esperar as falhas antigas descritas acima (acrescidas dos 11 testes novos de `cmd/server` pelo mesmo motivo).

## Fora de escopo

- **SEC-08** (envio assíncrono e deduplicado de e-mails no cadastro público de academias), **SEC-09** (consumo atómico e hash dos tokens de `auth_tokens`), **SEC-11** a **SEC-15** e as Partes 6 a 8 do plano (incluindo a eliminação do token restrito ao financeiro, SEC-12: aqui só foi acrescentada a exceção da troca obrigatória).
- **Limite de taxa real** no login e nos e-mails (decisão D1: só se elimina o código morto; risco residual declarado).
- **Passo 2 do plano 3.9** (reenvio em lote por SMS com custo aprovado): feito pela interface, conta a conta.
- **Reescrita do histórico do Git e rotação efetiva das 4 senhas públicas em produção:** o comando e o procedimento estão validados, mas executá-los (force push, novos clones, pedido ao suporte do GitHub) é uma decisão e ação sua; reescrever o histórico muda todos os hashes, inclusive o `4759474` desta base — faça-o **depois** de aplicar e commitar esta tarefa e a Tarefa 34.
- **`processarCadastroEstudanteBatch`** (sem chamadores) e a instância única de `getEmailService` (plano 2.1 F): não alterados.
- **Frontend:** Tarefa 34 do `rastreio-frontend`.
- **`Documentação da API.md` do frontend** (`src/docs/…`) é uma cópia própria que já difere da do backend; não foi sincronizada.

## Marcar como feito

1. `**Estado:** feito`, `(feito)` no título.
2. Secção **Resultado** no fim: um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mover para `docs/Tarefas feitas/`, mesmo nome.

## Checklist

- [ ] `EMAIL_FROM`, `BREVO_API_KEY`, `ENV=production` e provedor de SMS padrão conferidos no ambiente
- [ ] Base conferida (`4759474`)
- [ ] 4 arquivos apagados; 49 arquivos copiados (29 alterados, 20 novos); migration `135` sem conflito de número
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test ./...` — todos `ok` (banco novo)
- [ ] Tarefa 34 do `rastreio-frontend` no ar logo depois
- [ ] Comando `migrar-credenciais` executado (dry-run → marcar → admins) depois dos dois deploys
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`
