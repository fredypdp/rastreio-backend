# Plano de Atualizações — Regras de Negócio e Partes (v2)

> Base: `Lista de tarefas.md` (2026-11-08) + `notificacoes_eventos_spuri.md` · Atualizado em 2026-11-19
> **Back-end verificado:** `rastreio-backend`, `main` @ `d9c47fd`, por leitura do código, das rotas e dos documentos de tarefa. A suíte de testes **não foi executada**.
> **Front-end:** `rastreio-frontend`, `main` @ `740ce90` (2026-11-18), lido e validado com `tsc` e `eslint` na Tarefa 39. Os trechos do plano sobre telas que ainda não foram implementadas continuam vindo das regras do plano; cada tarefa de front começa pela análise do código.

## O que mudou nesta versão

1. Plano conferido com o back-end: cada Parte tem **Estado** e o que **já existe** no código. Várias partes ficaram menores.
2. **Eventos** passa a ser funcionalidade do módulo **Comunidade** (Parte 25).
3. **Não há academias cadastradas**: removidas as migrações de dados e a "assinatura de migração".
4. **Pesquisa e Estatística** saiu do plano.
5. **Envios automáticos de SMS** redefinidos (Parte 23): academia → encarregados/estudantes; Spuri (FPP) → academias.
6. **Notificações** do arquivo anexo incorporadas: Partes 17 a 23 e Anexo A.
7. Nova seção **Como acompanhar o andamento** (logo abaixo) para marcar o que foi implementado.

---

## Como acompanhar o andamento (marcar como implementado)

Este documento é a fonte do que falta fazer. **Sempre que uma atualização ou uma Parte for implementada, atualize-o na mesma entrega da tarefa:**

1. Na Parte, em **Entregas**, troque `[ ]` por `[x]` no item concluído e acrescente `(Tarefa N)` ao lado.
2. Quando **todos** os itens da Parte estiverem `[x]` (back-end e front-end aplicados), mude o **Estado** da Parte para **Implementado** e atualize a linha dela na tabela da Visão geral (Estado, Tarefa).
3. Estados: **Pendente** (nada feito) · **Parcial** (algo já existe ou só alguns itens marcados) · **Implementado** (todos os itens marcados).
4. Uma dependência só está liberada quando a Parte de que depende está **Implementada** (ou quando o item específico de que se depende está `[x]`).
5. Se a implementação mudar uma regra, corrija a regra no texto e anote em **Registro de andamento** (última seção).
6. O documento da tarefa e o texto do commit citam a Parte e os itens fechados (ex.: "Plano: Parte 5, itens 3 e 4").
7. Ou de preferência (prefiro assim), marcar com ✅, principalmente no fim do título da parte implementada
8. **Este arquivo (`docs/Plano_de_Atualizacoes.md`) atualizado vai em TODA entrega de back-end**, dentro do zip, ao lado do documento da tarefa, com os passos 1 a 7 já aplicados (itens marcados, Estado, tabela da Visão geral, ✅ e Registro de andamento). Uma entrega sem o plano atualizado está incompleta. Se a entrega for só de front-end, o plano vai mesmo assim no zip do back-end (ou em um zip avulso de `docs/`).

**Outras orientações**
- Cada Parte vira uma ou mais tarefas (modelo de criação de tarefa). Backend: a próxima tarefa livre é a **130** (a 129 fechou as Partes 2 e 3). Front-end (numeração própria): a próxima é a **40**.
- **(sugestão)** = regra acrescentada por mim para fechar lacuna; pode ser cortada. **(a confirmar)** = decisão pendente, com padrão na seção final.
- Ordem de deploy padrão: back-end primeiro, depois front-end. Exceção: partes que removem funcionalidade (1 e 12): front-end primeiro.
- As partes estão na ordem de execução; as que não dependem uma da outra podem andar em paralelo (ver "Caminho crítico").

---

## Visão geral

| Parte | O que é | Depende de | Estado | Tarefa |
|---|---|---|---|---|
| **Fase 1 — Fundações** | | | | |
| 1 ✅ | Remover a matrícula pública | — | Implementado | 128 (back) · 38 (front) |
| 2 | Back-end em pacotes (refatoração) | 1 | Parcial (falta só o HTTP do financeiro) | 129 (back) |
| 3 ✅ | Configurações do sistema (variáveis de ambiente → banco) | 2 | Implementado | 129 (back) · 39 (front) |
| 4 | Usuários e permissões (encarregado, secretário, professor) | 2, 3 | Pendente | |
| **Fase 2 — Credenciais** | | | | |
| 5 | Credenciais temporárias: ajustes do cadastro e do reset | 4 | Parcial | |
| **Fase 3 — Finanças** | | | | |
| 6 | Provedor PayPay e troca de provedor | 4 | Parcial | |
| 7 | Serviços extras: mês de início da cobrança | 2 | Pendente | |
| 8 | Validação do módulo financeiro | 6, 7 | Pendente | |
| **Fase 4 — Assinaturas** | | | | |
| 9 | Assinatura: planos, bloqueio e atribuição manual | 4 | Pendente | |
| 10 | Contratação paga de planos | 8, 9 | Pendente | |
| **Fase 5 — Gestão acadêmica** | | | | |
| 11 | Notas e faltas só por professores + recálculo da avaliação final | 4 | Pendente | |
| 12 | Avaliação final do ensino superior fixa no código | 2, 11 | Pendente | |
| 13 | Horários das turmas | 4 | Pendente | |
| 14 | Grupos do 4º ano de cursos técnicos | 4 | Parcial | |
| 15 | Material de apoio | 4, 9 | Pendente | |
| 16 | Refazer matrícula | 8, 11 | Pendente | |
| **Fase 6 — Comunicação e notificações** | | | | |
| 17 | Núcleo de notificações (e-mail e SMS) | 3, 5 | Parcial | |
| 18 | Notificações de segurança e conta | 17 | Pendente | |
| 19 | Créditos de SMS da academia | 8, 9, 10, 17 | Pendente | |
| 20 | Notificações acadêmicas | 4, 11, 17, 19 | Pendente | |
| 21 | Notificações financeiras e de serviços extras | 7, 8, 17, 19 | Pendente | |
| 22 | SMS manual da academia (individual, coletivo, citação) | 9, 17, 19 | Parcial | |
| 23 | SMS agendado e automático (academia e Spuri) | 22 | Pendente | |
| **Fase 7 — Facturação** | | | | |
| 24 | Facturação com Ulemo | 8, 9, 10 | Pendente | |
| **Fase 8 — Comunidade** | | | | |
| 25 | Módulo Comunidade: Eventos | 4, 9 | Pendente | |

**Caminho crítico:** 1 → 2 → 3 → 4 → 6 → 8 → 9 → 10 → 19 → 22 → 23.
**Em paralelo depois da Parte 4:** 5, 7, 11, 13, 14. **Depois da Parte 5:** 17 e 18 podem começar sem esperar as Fases 3 a 5.

---

## Termos usados

- **Spuri**: a plataforma. **Administradores**: papéis `gerente`, `adm` e `fpp` (hierarquia existente). Planos de assinatura: só `fpp`.
- **Academia**: a instituição. Cada academia só vê os próprios dados.
- **Nível escolar**: Ensino Primário e Iº Ciclo (1º–9º ano) e Ensino Médio. **Ensino superior** é separado.
- **Educando**: estudante do nível escolar. **Pagador**: estudante ou encarregado quando paga a academia; a academia, quando paga o Spuri.
- **Eventos (Comunidade)** não são os "eventos" de auditoria que já existem nas rotas `/eventos/...` do back-end; os da Comunidade usam `/comunidade/eventos`.

## Regras que valem para todas as partes

1. **O back-end decide.** Permissão, assinatura e preço são validados no servidor; o front só esconde ou mostra.
2. **Isolamento.** Nenhuma academia acessa dados de outra.
3. **Dinheiro** em Kwanza (AOA/Kz), calculado no servidor, nunca recebido do cliente.
4. **Datas** no fuso de Luanda (UTC+1). Plano: 1 mês = 30 dias, calendário civil.
5. **Segredos** criptografados, nunca devolvidos em texto claro, nunca em log.
6. **Auditoria** em ação sensível: quem, quando, o quê (o ledger e `SaveWithAudit` já fazem isso).
7. **Nada de histórico apagado:** pagamentos, notas, documentos fiscais e matrículas são arquivados ou anulados.
8. **Sem duplicidade:** repetir uma operação não gera cobrança, SMS, e-mail ou recibo em dobro.
9. **Menor de idade:** contato de estudante do nível escolar é sempre o do encarregado (regra única, Parte 5).
10. **Mensagens sensíveis são genéricas** quando o assunto é assinatura (Parte 9).
11. **Todo recurso novo declara a categoria de assinatura que o protege** (Parte 9). Depois da Parte 9, nada nasce sem proteção.
12. **Notificação de SMS paga pela academia nasce desligada.** O que o sistema "recomenda" não vem ligado por padrão (evita gasto indesejado).
13. **Cada parte atualiza a Documentação da API.md** (e a sua versão, no formato de Versionamento Semântico) e entrega migrations que rodam do zero.
14. **Cada parte atualiza este plano** (ver "Como acompanhar o andamento").

---

# Fase 1 — Fundações

### Parte 1 — Remover a matrícula pública ✅

**Estado:** Implementado (Tarefa 128 no back-end, Tarefa 38 no front-end) · **Depende de:** —

**Já existe (a remover):** rotas públicas `/solicitacao-matricula` (criar, busca, status, pagamento da matrícula); rotas de academia (aprovar, reprovar, cancelar, listar) e de admin; download de documentos da solicitação; agregado e projeção `SolicitacaoMatricula`; taxa de matrícula no módulo financeiro (`finance/matricula.go`, configurações em `/financeiro/matriculas/configuracoes`, efeitos de pagamento). Cerca de 2,5 mil linhas, fora os testes.

**Regras**
- Remover tudo isso, **sem flag, sem "desativado", sem código morto**. A partir daqui, estudante só é cadastrado pela academia (`/academia/estudante/register`) e, depois da Parte 4, pelo secretário.
- **Não confundir:** o cadastro público da **academia** (`/academia/cadastro`) continua. Só sai a matrícula pública de **estudante**.
- **Cuidado:** os documentos extra (`/academia/documentos-extra`) e parte das validações (`services/matricula_validation.go`) também servem ao cadastro feito pela academia. Separar o que é só da solicitação pública do que o cadastro reaproveita; o que o cadastro usa fica.
- Sem academias cadastradas, não há dados de produção a preservar. A tarefa deve conferir se o ledger tem eventos de teste da solicitação antes de tirar os tipos de evento da lista permitida (`safe_queries.go`) e do replay.

**Entregas**
- [x] Rotas públicas, de academia e de admin da solicitação removidas (Tarefa 128)
- [x] Agregado, projeção e eventos da solicitação removidos (Tarefa 128)
- [x] Taxa de matrícula removida do módulo financeiro (Tarefa 128)
- [x] Documentos extra e validações usadas pelo cadastro da academia preservados e testados (Tarefa 128)
- [x] Front-end: telas públicas e de gestão da solicitação removidas (Tarefa 38)
- [x] Documentação da API atualizada (Tarefa 128)

---

### Parte 2 — Back-end em pacotes

**Estado:** Parcial (Tarefa 129: tudo, menos o HTTP do financeiro) · **Depende de:** 1 (liberada)

**Já existe:** `internal/handlers` é **um único pacote com 61 arquivos** (fora testes). `finance`, `projections`, `domain/aggregates`, `services`, `middleware`, `storage` já são pacotes. `finance` é a camada de serviço do financeiro; o HTTP do financeiro ainda está em `handlers`.

**Regras**
- Refatoração pura: mesmas rotas, respostas, regras e tabelas.
- Um pacote por módulo: Notas, Faltas, Estudantes, Turmas, Avaliações, Comunicação e os demais que o código mostrar (cursos, matérias, serviços extras, sumários...).
- Pacotes de módulo **não importam uns aos outros**. O que é comum vai para um pacote de apoio.
- Uma entrega por pacote; a suíte completa dá o mesmo resultado antes e depois.
- O HTTP do financeiro entra **por último** e pode ficar para depois da Parte 6 sem bloquear nada.

**Entregas**
- [x] Pacote de apoio (helpers comuns de `handlers/helpers.go`) (Tarefa 129: `internal/handlers/apoio` e, para helpers de teste, `internal/handlers/testsupport`)
- [x] Notas · [x] Faltas · [x] Estudantes · [x] Turmas · [x] Avaliações · [x] Comunicação (Tarefa 129)
- [x] Demais módulos (cursos, matérias, sumários, serviços extras, documentos) (Tarefa 129; também academias, administradores, autenticação, lotes e registros)
- [ ] Financeiro (camada HTTP): continua em `internal/handlers`; entra por último, como previsto

**Como ficou (Tarefa 129)**
- `internal/handlers/<módulo>/` para academias, administradores, autenticacao, avaliacoes, comunicacao, cursos, documentos, estudantes, faltas, lotes, materias, notas, registros, servicosextras, sumarios e turmas. `internal/handlers` guarda só o HTTP do financeiro.
- `apoio` guarda o que mais de um módulo usa (helpers HTTP, o motor da avaliação final, a infraestrutura de lotes, o cadastro de estudante reaproveitado pelo cadastro em lote). Cada módulo ficou com os seus próprios wrappers de lote e itens de job (`CriarCursoBatch`, `AtivarAcademiaJobItem`...); só a infraestrutura genérica é compartilhada.
- A regra "módulos não se importam" tem teste: `TestPacotesDeModuloNaoSeImportam` (pacote `apoio`).
- Para efeito da regra 4 de "Como acompanhar", as Partes 3 e 4 estão liberadas: o que falta da Parte 2 (HTTP do financeiro) não bloqueia nada.

---

### Parte 3 — Configurações do sistema (variáveis de ambiente → banco) ✅

**Estado:** Implementado (Tarefa 129 no back-end, Tarefa 39 no front-end) · **Depende de:** 2 (liberada)

**Já existe**
- [x] **Cofre de segredos** cifrado para credenciais de provedores (chave derivada do `JWT_SECRET`; Tarefas 101 e 120).
- [x] **Auditoria:** ledger append-only e `SaveWithAudit` (quem, IP, tipo de usuário).
- Tabela `projection_sistema_config` (chave/valor) já usada pelos anos letivos globais. O back-end lê hoje cerca de 36 variáveis de ambiente.

**Regras**
- Só variáveis de uma **lista aprovada** são editáveis pela tela; administradores autorizados alteram. Cada uma tem nome, tipo (texto, número, sim/não, segredo), valor, descrição, quem alterou e quando. O valor é validado pelo tipo antes de salvar.
- **Segredos:** mascarados na tela (só se pode substituir), nunca em log ou resposta.
- Leitura: banco → se vazio, variável de ambiente (compatibilidade). Cada configuração declara se vale **na hora** ou **só no próximo reinício** (ex.: duração do token só vale para novos logins).
- Toda alteração entra na auditoria; para segredos, o valor nunca é gravado.

**Proposta de lista (a aprovar)**
- *Podem ir para o banco:* `ALLOWED_ORIGINS`, `EMAIL_FROM`, `JWT_EXPIRY_HOURS`, `LOG_LEVEL`, `APPYPAY_RESOURCE`, `RECONCILIACAO_*` (5), e como segredos `BREVO_API_KEY`, `ZIETT_API_KEY`, `MEGA_EMAIL`, `MEGA_PASSWORD`, `MEGA_ROOT_FOLDER`.
- *Ficam no ambiente:* `DATABASE_URL` e `DB_*`, `JWT_SECRET`, `BOOTSTRAP_TOKEN*`, `PORT`, `ENV`, `STORAGE_PROVIDER`, `DB_FORCE_SINGLE_CONN`, e os sinalizadores de desenvolvimento e teste.
- Novos valores das próximas partes (limites de SMS, tarifa, tamanho de arquivo...) já nascem nesta tela.

**Entregas**
- [x] Lista aprovada de variáveis (Tarefa 129: a proposta acima, em `internal/sysconfig`; a lista final depende da sua confirmação)
- [x] API de leitura e escrita (só administradores) com validação por tipo (Tarefa 129: `GET/PUT/DELETE /admin/sistema/configuracoes`)
- [x] Leitura banco → ambiente, com cache e indicação "na hora / no reinício" (Tarefa 129)
- [x] Segredos mascarados e fora dos logs (Tarefa 129: cifrados em tabela própria; nunca no ledger, na projeção, em resposta ou em log)
- [x] Tela de administração (front) (Tarefa 39: `/configuracoes/sistema`)

**Como ficou (Tarefa 129 e Tarefa 39)**
- Quem pode: `adm` e `fpp` consultam; só `fpp` altera ou remove. Cada chave declara `aplicacao`: `imediato` (vale na próxima leitura nesta instância e em até 60 s nas outras) ou `reinicio` (lida uma vez no arranque: as cinco `RECONCILIACAO_*` e as três `MEGA_*`).
- `MEGA_ROOT_FOLDER` ficou como **texto visível**, não como segredo: é só o nome de uma pasta e esconder o valor impediria o administrador de conferi-lo. Se preferir segredo, é uma linha na lista.
- A tabela `projection_sistema_config` dos anos letivos **não** foi reaproveitada: a nova `projection_configuracao_sistema` (migration 139) tem lápide e guarda de versão, e os segredos vão para `configuracao_sistema_segredos`.
- Para entrar nesta tela, um novo valor das próximas partes só precisa de uma linha em `sysconfig.definicoes` e de ler a chave com `sysconfig.String`.

---

### Parte 4 — Usuários e permissões

**Estado:** Pendente · **Depende de:** 2, 3

**Já existe:** só três tipos de login: **admin** (`gerente`/`adm`/`fpp`), **academia** e **estudante**. O **encarregado** é apenas o telefone e o BI dentro do cadastro do estudante (sem nome, sem login). Não existem professor nem secretário.

**Quem pode o quê** (— = o plano não altera)

| Ação | Academia | Secretário | Professor | Estudante | Encarregado | Admin |
|---|---|---|---|---|---|---|
| Cadastrar e matricular estudante | ✔ | ✔ | ✘ | ✘ | ✘ | — |
| Criar secretário | ✔ | ✘ | ✘ | ✘ | ✘ | — |
| Criar professor | ✔ | ✔ | ✘ | ✘ | ✘ | — |
| Lançar e corrigir notas e faltas | ✘ | ✘ | ✔ (só turmas/disciplinas vinculadas) | ✘ | ✘ | — |
| Consultar notas e faltas | ✔ | ✔ | ✔ (só as suas turmas) | ✔ (as próprias) | ✔ (educandos, nível escolar) | — |
| Pagar cobranças do estudante | ✘ | ✘ | ✘ | ✔ | ✔ (educandos, nível escolar) | ✘ |
| Resetar credenciais de estudante | ✔ (seus) | ✔ (seus) | ✘ | ✘ | ✘ | ✔ (só sem academia) |
| Gerir planos de assinatura | ✘ | ✘ | ✘ | ✘ | ✘ | ✔ (só FPP) |

**Regras comuns:** criação segue a cadeia de credenciais temporárias que já existe (enviadas ao telefone **do próprio usuário**, troca obrigatória no primeiro acesso); telefone obrigatório (sugestão). Desativar não apaga o que a pessoa fez.

**Secretário**
- Criado pela academia; pertence a uma só academia.
- Mesmas permissões da academia, **exceto lançar notas e faltas**. Não cria outro secretário (sugestão).
- Campos: nome, data de nascimento, gênero, telefone, e-mail.

**Professor**
- Criado pela academia ou pelo secretário. Tem **um ou mais vínculos** (turma + disciplina); cada par tem **um professor por vez** (sugestão).
- Só lança/corrige notas e faltas dos pares a que está vinculado e vê os estudantes dessas turmas.
- A academia/secretário reatribui um par a outro professor; a permissão acompanha o vínculo, não a pessoa.
- Campos: nome, data de nascimento, gênero, telefone, e-mail + vínculos.

**Encarregado de educação**
- Pode tudo que o estudante pode, **só pelos seus educandos** e enquanto estiverem no nível escolar. Pode ter vários educandos, inclusive de academias diferentes.
- Campos: nome, data de nascimento, gênero, telefone, e-mail, educandos e a **relação** com cada um (pai, mãe, irmão/irmã, outro). A relação é opcional; **se for "outro", o texto é obrigatório**.
- Quando o educando **finaliza o nível escolar** (aprovado no último ano do médio ou matriculado no superior, o que vier primeiro), o encarregado deixa de pagar ou agir por ele; só consulta, em leitura, os dados **do período escolar**.
- O estudante do superior pode **se desvincular do encarregado**, só depois de finalizar o nível escolar; ao desvincular, o encarregado perde também a consulta do histórico. (Não confundir com a desvinculação estudante↔academia, que já existe.)
- Um educando tem **um encarregado por vez**; a academia pode trocá-lo (sugestão).

**Entregas**
- [ ] Tipos de login `secretario`, `professor`, `encarregado` e middlewares de permissão
- [ ] Cadastro de secretário · [ ] Cadastro de professor com vínculos turma+disciplina
- [ ] Cadastro e vínculo de encarregado (migrando o telefone/BI hoje no estudante)
- [ ] Regra de acesso do encarregado (nível escolar, fim do período, desvinculação)
- [ ] Matriz de permissões aplicada às rotas existentes
- [ ] Front-end: telas de cadastro e áreas de cada tipo
- [ ] Documentação da API atualizada

---

# Fase 2 — Credenciais

### Parte 5 — Credenciais temporárias: ajustes do cadastro e do reset

**Estado:** Parcial · **Depende de:** 4

**Já existe (Tarefa 123 / SEC-04)**
- [x] Cadastro do estudante envia as credenciais por SMS ao telefone certo: **superior → estudante; escolar → encarregado** (`TelefoneDestinoCredenciaisEstudante`).
- [x] **Reset/reenvio** `POST /estudantes/:codigo/credenciais/reenviar`: gera nova senha temporária, invalida as sessões, exige troca no primeiro acesso, intervalo mínimo de 10 minutos, auditado.

**O que falta**
- O admin hoje é só `fpp` e pode resetar **qualquer** estudante. A regra é: **`adm` ou `fpp`, só estudante sem nenhuma academia**. A academia continua só com os seus; **secretário** entra com a mesma regra da academia.
- Cadastro do nível escolar: com a conta do encarregado (Parte 4), as credenciais do estudante **e do encarregado novo** vão só para o telefone do encarregado. Encarregado já existente é vinculado sem novas credenciais (identificado por telefone ou e-mail, a confirmar). Se o envio falhar, **nunca** cai para o telefone do estudante; a academia vê o erro e reenvia.
- A função de destino tem um caminho em que, sem nível definido, usa o telefone do estudante. Para estudante do nível escolar isso não pode acontecer.
- Transformar na **regra única de contato**: dado um estudante, devolve telefone **e e-mail** certos (escolar → encarregado; superior → o próprio). É a única função usada pelas credenciais, pelas notificações (Parte 17) e pelos SMS (Partes 22 e 23).

**Entregas**
- [x] Destino das credenciais por nível (Tarefa 123)
- [x] Reset/reenvio com intervalo e auditoria (Tarefa 123)
- [ ] Reset: admin `adm`/`fpp` só para estudante sem academia; secretário como a academia
- [ ] Cadastro escolar com conta do encarregado e credenciais só ao telefone dele
- [ ] Remover o caminho que cai no telefone do estudante no nível escolar
- [ ] Regra única de contato (telefone e e-mail) em função reutilizável
- [ ] Testes por perfil e por nível

---

# Fase 3 — Finanças

### Parte 6 — Provedor PayPay e troca de provedor

**Estado:** Parcial · **Depende de:** 4

**Já existe (Tarefas 119 a 121)**
- [x] Módulo financeiro dividido por provedores: interface `Provedor` (`finance/provedor.go`), AppyPay em `provedor_appypay.go`, **provedor novo = um arquivo `provedor_<nome>.go` + uma linha em `fabricasProvedores`**.
- [x] **Provedor ativo único por contexto** (`spuri` ou cada academia). Definir o provedor exige credenciais; as primeiras credenciais viram o ativo.
- [x] **Mecanismo retroativo:** cada cobrança guarda o provedor que a emitiu; consulta, cancelamento, webhook e pagamento externo usam o dele. **Remover credenciais com cobrança pendente é recusado (409).**
- [x] Credenciais cifradas por contexto/ambiente/provedor, snapshot ao editar ou remover, **job de reconciliação**, **métodos de pagamento filtrados por provedor e por pagador**.
- A interface já cobre o PayPay (chaves RSA, webhook sem segredo gerado pelo Spuri, resposta `success`). A documentação está em `docs/Integrações/PayPay API.md`.
- O "pacote Finanças" já existe como `internal/finance`. O PayPay segue o padrão atual: **`provedor_paypay.go`**, sem criar pacote novo; funções comuns já ficam em arquivos neutros do pacote.

**O que falta**
- Do PayPay entra só o necessário: **MULTICAIXA Express (§3.2 → método interno `GPO`)**, **MULTICAIXA Reference (§3.3 → `REF`)**, **consultar pedido (§3.6)**, **fechar pedido (§3.5)** e **notificações (§4)**. Fora: reembolso, transferências, Kwik, agente.
- **Tela de credenciais** (módulo de finanças): no topo, a escolha do provedor; cada um (PayPay, AppyPay) tem uma **subtela** com os campos de credenciais e o **manual**. Mostra qual está ativo.
- **Validar a troca de provedor** (o que você pediu verificar), por teste: pagador passa a ver só os métodos do novo provedor (academia: provedor da academia; academia pagando o Spuri: provedor do Spuri); cobranças novas nascem no novo; as pendentes seguem no antigo.
- Dois pontos que a leitura do código **não confirmou** e a tarefa deve testar: (a) gerar nova cobrança para uma dívida que já tem pendente em outro provedor **cancela a antiga** quando o provedor permite; (b) dois pagamentos confirmados para a mesma dívida: o primeiro quita e o segundo fica "pago em duplicidade", com aviso à academia, **sem abater em outra dívida**.
- Teste de conexão antes de ativar um provedor (sugestão).
- Estados comuns: pendente → pago | expirado | cancelado | falhou. **Só o provedor confirma pagamento**; valor diferente do cobrado fica "divergente" e não quita.

**Entregas**
- [x] Arquitetura multiprovedor, retroativo, cofre, snapshot, reconciliação, métodos por pagador (Tarefas 119–121)
- [ ] `provedor_paypay.go`: Express, Reference, consultar, fechar pedido
- [ ] Fábrica registrada em `fabricasProvedores`; credenciais RSA; webhook por assinatura
- [ ] Reconciliação com política por método do PayPay
- [ ] Testes de troca AppyPay ↔ PayPay (métodos, cobrança nova, pendentes, duplicidade)
- [ ] Front-end: seletor de provedor e subtela do PayPay (campos + manual)
- [ ] Documentação da API atualizada

**Entradas que preciso de você:** credenciais de teste do PayPay e os textos dos manuais do PayPay e do AppyPay para as subtelas.

---

### Parte 7 — Serviços extras: mês de início da cobrança

**Estado:** Pendente · **Depende de:** 2

**Já existe:** a mensalidade tem mês de início por academia e ano letivo (`DefinirMesInicioCobranca`; Tarefas 109 e 111). O **serviço extra mensal não tem**: hoje cobra a partir da data em que o estudante foi vinculado ao serviço (`vinculada_em`).

**Regras**
- Cada serviço extra passa a ter **mês de início da cobrança**, dentro do ano letivo, no mesmo padrão da mensalidade.
- Antes desse mês não há cobrança. A partir dele, cobra-se normalmente.
- Quem adere depois do mês de início paga **a partir do mês da adesão**, sem meses retroativos (sugestão).
- Alterar o mês só afeta o que ainda não foi gerado: cobranças **não pagas** anteriores ao novo mês são canceladas; as **pagas** nunca são tocadas.
- Serviço sem mês definido mantém o comportamento atual. Sem academias, não há migração de dados.

**Entregas**
- [ ] Campo e regras no agregado, projeção e cálculo de pendências (`finance/servico_extra.go`)
- [ ] Rotas de definir/consultar/remover (padrão da mensalidade)
- [ ] Testes: antes/depois do mês, adesão tardia, alteração do mês
- [ ] Front-end: campo no cadastro e na edição do serviço
- [ ] Documentação da API atualizada

---

### Parte 8 — Validação do módulo financeiro

**Estado:** Pendente · **Depende de:** 6, 7

**Já existe (a auditar, não refazer):** guarda de operação única (`unique_operation_guard.go`), idempotência em `aplicarEfeitosPagamento`, conferência ao vivo no provedor antes de confirmar (Tarefa 79), bloqueio de anular/reativar/cancelar cobrança paga (Tarefa 114), reconciliação (Tarefa 121), auditorias anteriores (Tarefas 40 a 46).

**Objetivo:** provar com testes e corrigir o que faltar nas três garantias, valendo para os **dois provedores**, para estudante **e encarregado**, e para mensalidades **e serviços extras**. Não muda regra de preço: só corrige desvio.

**Garantia 1 — O estudante paga o que deve e a academia sabe:** só o provedor confirma; callback repetido não duplica; valor confirmado = valor cobrado; o "pago" aparece para academia, estudante e encarregado e não volta a pendente; pagamento sem dívida correspondente fica "não identificado".

**Garantia 2 — Valor certo:** o preço sai da configuração (tipo de estudante, características, ano/nível, curso), calculado só no servidor; fica **congelado na cobrança**; um teste por combinação de tipo/nível/serviço.

**Garantia 3 — Nunca cobrar o que já foi pago:** conferir "já pago" e "já pendente" antes de criar cobrança; restrição única por (estudante, item, período) no banco; criações simultâneas (duplo clique, dois aparelhos, estudante + encarregado) geram **uma** cobrança; pagar dívida paga é recusado.

**Entregas**
- [ ] Relatório do que já cumpre e do que foi corrigido
- [ ] Testes das três garantias (um que falha quando a regra é desfeita, por regra)
- [ ] Cobertura com AppyPay e PayPay, estudante e encarregado, mensalidade e serviço extra

---

# Fase 4 — Assinaturas

### Parte 9 — Assinatura: planos, bloqueio e atribuição manual

**Estado:** Pendente · **Depende de:** 4

**Já existe:** nada de assinatura. Reaproveitar: papéis de admin (`RequireFPP`), o **código por e-mail do 2FA** (`dois_fatores_service`, Tarefa 122) para a validação da atribuição manual, e o cadastro público de academia com **ativação por admin** (`AtivarAcademia`).

**Plano** (só administradores FPP)
- Campos: nome (obrigatório); pago ou gratuito (obrigatório); preço em Kz (obrigatório se pago); prazo em meses inteiros (obrigatório); descrição; categoria (obrigatório, uma só).
- Prazo: 1 mês = 30 dias, calendário civil. Ex.: 3 meses = 90 dias.
- Plano com assinaturas não é apagado; fica indisponível (sugestão). Mudar preço ou prazo não afeta assinaturas feitas.

**Categorias**

| Categoria | O que habilita | Quando inativa |
|---|---|---|
| `gestao_academia` | Gestão completa: estudantes, turmas, cursos, matrículas, financeiro, notas e faltas, atividades extracurriculares, serviços extras, horários, material de apoio, grupos, Comunidade | Academia, secretário, professores, estudantes e encarregados só **consultam** |
| `comunicacao_sms` | Academia enviar SMS pelo módulo de comunicação (Partes 19 a 23) | Academia não envia SMS |
| `facturacao` | Academia emitir documentos de facturação (Parte 24) | Academia não emite documentos |

- A avaliação final **não tem categoria própria**: segue a Gestão Acadêmica.
- SMS e e-mail do próprio sistema (credenciais, segurança) não dependem de assinatura.
- Mesmo com tudo inativo, a academia **sempre** entra, consulta, vê planos e **paga planos** de qualquer categoria. Confirmações de pagamentos já iniciados continuam sendo registradas.

**Assinatura da academia**
- Estado **ativo/inativo** calculado pelas datas (fim = início + prazo × 30 dias). Sem botão manual de "inativar".
- Uma assinatura ativa por categoria; renovar com assinatura ativa **soma** o prazo ao fim atual (sugestão). Plano gratuito: a academia ativa sozinha (sugestão).
- Cada registro guarda a **origem**: gratuita, paga ou manual.
- **Academia nova nasce sem assinatura** e não opera até ter plano. Como não há academias hoje, **não há migração**. O administrador pode atribuir o plano ao ativar a academia (sugestão).

**Bloqueio**
- Verificação central no back-end, por categoria, em toda ação protegida.
- Mensagem sempre **"Serviço indisponível no momento"**. Nunca diz que o plano expirou nem nada do tipo.
- A tela "Minha assinatura" mostra o estado real, porque é onde a academia renova (a confirmar).

**Atribuição manual (negociação fora do sistema)**
1. Administrador FPP escolhe academia, plano, início e escreve o motivo.
2. O sistema envia um código ao **e-mail do próprio administrador**.
3. O administrador digita o código: válido por 10 min, uso único, até 5 tentativas, vale só para essa operação (valores sugeridos).
4. Cria a assinatura com origem "manual" (sem cobrança nem recibo) e registra auditoria. Cancelar manualmente exige a mesma validação (sugestão).

**Entregas**
- [ ] Planos (CRUD só FPP) e categorias
- [ ] Assinaturas da academia (estado por datas, uma ativa por categoria, renovação soma prazo)
- [ ] Bloqueio central por categoria em **todas** as rotas existentes
- [ ] Atribuição manual com código por e-mail e auditoria
- [ ] Plano gratuito ativado pela própria academia
- [ ] Front-end: gestão de planos (FPP) e "Minha assinatura" (academia)
- [ ] Documentação da API atualizada

---

### Parte 10 — Contratação paga de planos

**Estado:** Pendente · **Depende de:** 8, 9

**Regras**
- A academia escolhe um plano pago → cobrança no **provedor ativo do Spuri** (Parte 6), com o preço do plano naquele momento.
- A assinatura só ativa **depois do pagamento confirmado pelo provedor** (regras da Parte 8); ativar é idempotente.
- Pagamento confirmado depois de a cobrança expirar: ativa mesmo assim, o dinheiro entrou.
- Renovação soma ao fim atual. Mudar o preço do plano depois não afeta cobrança já criada.
- Pagamento de plano (Academia → Spuri) nunca se mistura com pagamentos de estudantes (Estudante → Academia).
- Origem da assinatura: "paga".

**Entregas**
- [ ] Cobrança do plano no provedor do Spuri e ativação após confirmação
- [ ] Renovação e idempotência
- [ ] Front-end: contratar e acompanhar pagamento
- [ ] Documentação da API atualizada

---

# Fase 5 — Gestão acadêmica

### Parte 11 — Notas e faltas só por professores + recálculo da avaliação final

**Estado:** Pendente · **Depende de:** 4

**Já existe**
- Hoje **só a academia** lança e corrige (`/academia/notas-aluno`, `/faltas-aluno`, `PATCH` e rotas `/async`). Não há rota de professor.
- [x] Configuração de limite de faltas e **reprovação por faltas** por academia (Tarefa 117).
- **A regra de recálculo NÃO está em vigor** (você pediu para verificar). Corrigir uma nota só dispara a avaliação final se **ainda não existir** avaliação gravada; registrar ou corrigir **faltas não dispara nada**; depois de gravada, a avaliação **nunca é recalculada** (documento da Tarefa 117 diz isso expressamente).

**Registro de notas e faltas**
- Lançar, corrigir e remover: **só o professor vinculado** à turma+disciplina. Academia e secretário apenas consultam. Os caminhos de escrita da academia (inclusive `/async`) são removidos ou fechados.
- Cada nota/falta guarda **quem lançou**. Se o professor sai, a academia/secretário reatribui o vínculo e o novo professor passa a poder corrigir.

**Recálculo da avaliação final (a implementar)**
- Nota lançada ou corrigida → o resultado final é **recalculado**.
- Falta adicionada, alterada ou removida → recalcula **somente se a academia ativou a reprovação por faltas**.
- Só recalcula **enquanto o ano letivo do registro está em andamento** para a academia (`FinalizarAnoLetivoAcademia` ainda não aconteceu). Depois, o resultado fica fechado.
- Como o ledger é imutável, o recálculo grava um **novo evento** com valor anterior, novo valor e motivo. Dois professores ao mesmo tempo: o resultado reflete sempre os dados atuais.

**Entregas**
- [ ] Rotas de professor para lançar/corrigir/remover notas e faltas (inclusive em lote)
- [ ] Escrita da academia/secretário nessas rotas removida
- [ ] Autor registrado em cada nota/falta
- [ ] Recálculo ao corrigir nota e ao mudar falta, dentro do ano letivo, com evento de histórico
- [ ] Testes: dentro e fora do período; falta com e sem reprovação por faltas
- [ ] Front-end: telas do professor; academia só consulta
- [ ] Documentação da API atualizada

---

### Parte 12 — Avaliação final do ensino superior fixa no código

**Estado:** Pendente · **Depende de:** 2, 11

**Já existe:** o modelo do **escolar já é fixo** (`regraAvaliacaoFinalEscolarFixa`). O **superior é configurável** pela academia (`POST/PUT/DELETE /academia/avaliacao-final/regras` com fórmula, e `/academia/categorias-nota`); há um manual em `docs/`.

**Regras**
- A estrutura atual vira **só a base**: o contrato do que um mecanismo recebe e devolve. Deixa de ser configurável.
- Cada academia de ensino superior tem **seu mecanismo no código**, ligado à academia por identificador estável. Removem-se as rotas e telas de configuração do superior.
- Academia superior **sem mecanismo**: a avaliação final **não é calculada** (nunca um padrão silencioso) e os administradores são alertados.
- Como não há academias, **nada a reproduzir hoje**. Academia superior nova só tem avaliação final depois que o mecanismo dela for implementado e publicado (consequência de fixar tudo no código).
- O nível escolar não muda.

**Entregas**
- [ ] Contrato do mecanismo e registro por academia
- [ ] Rotas de configuração do superior removidas
- [ ] Alerta aos administradores para academia sem mecanismo
- [ ] Front-end: telas de configuração removidas
- [ ] Documentação da API atualizada

**Entrada que preciso de você:** as regras de cada academia superior, **no momento em que cada uma entrar** (cada mecanismo é uma tarefa pequena).

---

### Parte 13 — Horários das turmas

**Estado:** Pendente · **Depende de:** 4

**Já existe:** nada (os "sumários de aula" da Tarefa 75 são outra coisa).

**Regras**
- O horário é uma lista de aulas: turma, disciplina, professor, dia da semana, hora de início e fim (sala opcional).
- O professor vem do vínculo turma+disciplina (Parte 4); não se agenda aula sem professor vinculado.
- **Conflito bloqueia salvar:** mesma turma com duas aulas ao mesmo tempo; mesmo professor em duas turmas ao mesmo tempo; mesma sala, se usada.
- Pertence ao período letivo da turma. Se o professor do vínculo muda, as aulas passam ao novo e os conflitos são revalidados.
- **Gerencia:** academia e secretário. **Vê:** professor (o seu), estudante e encarregado (o da turma), academia e secretário (todos). Categoria `gestao_academia`.

**Entregas**
- [ ] Modelo, regras de conflito e rotas
- [ ] Visões por perfil
- [ ] Front-end: montagem e consulta do horário
- [ ] Documentação da API atualizada

---

### Parte 14 — Grupos do 4º ano de cursos técnicos

**Estado:** Parcial · **Depende de:** 4

**Já existe (Tarefa 116):** no `4_ano_medio` não há turmas, há **grupos**. O agregado `Turma` continua sendo a fonte (`tipo_agrupamento` derivado do nível) e já tem **`tema_trabalho`** (opcional, até 200 caracteres). Vínculo de estudantes, histórico por ano letivo, faltas e notas já dependem de `Turma`; **não criar agregado paralelo**.

**O que falta**
- Dados: **mentor** (professor ativo da mesma academia), descrição, estado (em formação, em andamento, concluído) e data de defesa (sugestão). Um mentor pode orientar vários grupos (limite a definir na tarefa).
- Um estudante em **um só grupo por ano letivo**, matriculado no 4º ano técnico do mesmo curso. Grupo sem integrantes ou sem mentor não vai para "em andamento".
- Mudanças de mentor, tema e integrantes ficam em histórico; "concluído" é só leitura.
- **Cria/edita:** academia e secretário. **Vê:** o mentor (seus grupos), os integrantes e seus encarregados.
- Se o estudante deixar o 4º ano técnico (ex.: refazer matrícula, Parte 16), sai do grupo.

**Entregas**
- [x] Grupo = turma do 4º ano médio, com tema (Tarefa 116)
- [ ] Mentor, descrição, estado e histórico
- [ ] Regra de um grupo por estudante/ano e validação do curso
- [ ] Visões por perfil
- [ ] Front-end e Documentação da API

---

### Parte 15 — Material de apoio

**Estado:** Pendente · **Depende de:** 4, 9

**Já existe:** nada de material de apoio. Há camada de armazenamento (`internal/storage`, Mega ou local) e padrões de upload/download autenticado para reaproveitar.

**Regras**
- Tipos: **PDF, JPG e documentos do pacote Office** (Word, Excel, PowerPoint). Confere a extensão **e** o conteúdo real; outros são recusados.
- Limite por arquivo e cota por academia, ajustáveis na Parte 3 (sugestão).
- Caminho montado **só por identificadores internos**, nome do arquivo higienizado (impede acessar outras pastas). Cada academia só enxerga a sua pasta.

| Nível | Caminho (dentro do diretório da academia) |
|---|---|
| Primário e Iº Ciclo | `/Material de Apoio/{N_ano_fundamental}/{identificador da matéria}/` |
| Ensino Médio | `/Material de Apoio/{identificador do curso}/{N_ano_medio}/{identificador da matéria}/` |
| Ensino Superior | `/Material de Apoio/{identificador do curso}/{identificador da matéria}/` |

- **Envia e remove:** academia, secretário e professor da disciplina, só na sua matéria (sugestão). **Baixa:** estudantes matriculados naquele ano/curso/matéria (o ano atual), seus encarregados, o professor da disciplina, academia e secretário.
- Download sempre pela API com verificação de acesso; sem link público permanente. Com `gestao_academia` inativa: só consultar e baixar.

**Entregas**
- [ ] Upload com validação de tipo e conteúdo
- [ ] Estrutura de pastas por nível e controle de acesso
- [ ] Download autenticado
- [ ] Front-end e Documentação da API

---

### Parte 16 — Refazer matrícula

**Estado:** Pendente · **Depende de:** 8, 11

**Já existe:** não há rota dedicada para corrigir nível, ano ou curso da matrícula de um estudante (a Tarefa 94 só valida dependências ao trocar o nível escolar da academia).

**Regras**
- **Quem:** academia e secretário, só estudantes da própria academia; exige **motivo** e fica na auditoria.
- Mesma pessoa e mesma conta (credenciais não mudam). Muda a matrícula: nível, ano, curso e turma. Refazer para a mesma situação é recusado.
- A matrícula antiga fica no histórico como **"refeita"**, nunca apagada.
- **Cobranças não pagas** da antiga são canceladas e **recalculadas** pelo preço da nova (Parte 8). **Cobranças pagas** não são apagadas nem devolvidas pelo sistema; a academia é avisada da diferença (a confirmar).
- **Notas e faltas** da matrícula errada ficam no histórico dela; a avaliação final antiga fica "anulada" e a nova começa do zero (a confirmar).
- Sai da turma e do grupo antigos. Horário e material de apoio seguem sozinhos a nova matrícula. O acesso do encarregado segue o novo nível (Parte 4).

**Entregas**
- [ ] Rota e agregado de refazer matrícula, com motivo e histórico
- [ ] Efeitos em cobranças, notas/faltas, turma e grupo
- [ ] Front-end e Documentação da API

---

# Fase 6 — Comunicação e notificações

**Já existe no módulo de comunicação (Tarefa 93):** envio de **um SMS por vez** para um número (`POST /comunicacao/mensagens`) por **admin ou academia**, com provedores **GoSMS e Ziett** e fallback entre eles; remetentes (admin); provedor padrão (admin); listagem. **Não existem:** canal e-mail no histórico, créditos/saldo, envio em lote ou por usuário/filtro, citação, agendamento, regras automáticas, configuração por evento. O e-mail do sistema já funciona por Brevo (`services/brevo_mailer.go`).

**Política de canais** (do arquivo de notificações)

| Canal | Quem paga | Para quê |
|---|---|---|
| E-mail (Brevo) | Spuri | Canal padrão de tudo. O plano Free cobre 300 e-mails/dia; por isso notas e faltas vão em **resumo**, nunca uma mensagem por lançamento |
| SMS Spuri (sistêmico) | Spuri | Só segurança da conta e comunicação da Spuri com a academia. **Lista fechada** (Parte 18) |
| SMS Academia (configurável) | A academia, com créditos | Todo o resto. A academia liga ou desliga cada evento |

O catálogo completo de eventos está no **Anexo A**.

---

### Parte 17 — Núcleo de notificações (e-mail e SMS)

**Estado:** Parcial · **Depende de:** 3, 5

**Regras**
- **Disparo pós-commit com chave de idempotência** (evento + canal + destinatário). **Não** usar Projection: um `rebuild` reenviaria tudo.
- **Disparar no handler, não só pelo ledger:** algumas decisões não geram evento (ex.: reprovação de solicitação de status acadêmico faz `UPDATE` direto na projeção).
- **Migration de comunicação:** a tabela só aceita `enviado_por_tipo` `admin`/`academia`, `status` `enviada`/`falhou` e `destinatario` de 20 caracteres. Faltam o tipo `sistema`, o **canal**, o destinatário de e-mail, o **número de segmentos**, o **custo** e estados de fila.
- **Contato:** sempre pela regra única da Parte 5 (escolar → encarregado).
- **SMS Spuri = lista fechada** em código (Parte 18); todo o resto é **SMS Academia**.
- **SMS Academia, configurado por academia:** liga/desliga **cada evento** (**nasce desligado**; os marcados ✱ no anexo são só recomendação); destinatário (estudante ou encarregado; no escolar o telefone do encarregado é obrigatório e o do estudante é opcional); modo **imediato ou resumo** (notas e faltas); **teto diário** e **limite por destinatário**; mensagens de até **160 caracteres**.
- **Sem saldo**, o sistema usa o e-mail como alternativa e avisa a academia por e-mail (evento 14, Parte 19).
- **Resumos** (diário/semanal) exigem um **executor de tarefas agendadas** que nunca roda a mesma tarefa duas vezes (mesmo com mais de uma instância ou reinício). Esse executor é reaproveitado na Parte 23.
- **Limitadores de requisição:** os "no-op" antigos já foram removidos (Tarefa 123), mas **login e e-mail seguem sem limite** (risco D1 declarado). **Antes de ligar qualquer SMS por rota pública** (recuperação de senha, verificação de telefone, reenvio de 2FA), implementar limitadores reais por rota e por destinatário.
- **Histórico** de toda notificação: quem/o quê, canal, destinatário, estado, segmentos, custo. A academia vê só as suas.

**Entregas**
- [x] Envio de SMS com GoSMS/Ziett e fallback; histórico básico (Tarefa 93)
- [ ] Migration de comunicação (tipo `sistema`, canal, e-mail, segmentos, custo, estados)
- [ ] Disparo pós-commit com chave de idempotência e fila
- [ ] Lista fechada de SMS do Spuri em código
- [ ] Configuração por academia (liga/desliga por evento, destinatário, modo, tetos, 160 caracteres, alternativa por e-mail)
- [ ] Executor de tarefas agendadas e resumos
- [ ] Limitadores reais nas rotas públicas que disparam e-mail/SMS
- [ ] Front-end: tela de configuração de notificações da academia
- [ ] Documentação da API atualizada

---

### Parte 18 — Notificações de segurança e conta

**Estado:** Pendente · **Depende de:** 17

Eventos **1 a 14** do Anexo A. Rodam por e-mail; o SMS é pago pelo Spuri **só** nesta lista fechada:

| Evento | Quando o SMS sai |
|---|---|
| 1 Código de recuperação de senha | Só com telefone verificado |
| 2 Código de 2FA por SMS | Só se for implementado e o usuário escolher SMS |
| 4 Desativação do 2FA | Sempre |
| 5 Código de verificação de telefone | Uma vez por número |
| 6 Senha alterada | Só sem e-mail verificado |
| 7 e 8 Troca de e-mail ou de telefone | Para o contato **antigo** |
| 12 e 13 Academia ativada ou desativada | Sempre (comunicação da Spuri com a sua cliente) |

**Já existe:** eventos `SenhaAlterada`, `AcademiaSenhaAlterada`, `AdminSenhaAlterada`, `DoisFatoresAtivados`, `DoisFatoresDesativados`; e-mail de verificação, boas-vindas de admin, 2FA por e-mail; **recuperação de senha por link** (Tarefa 123).

**Gatilhos novos (†) a criar**
- **Verificação de telefone** (evento 5): o campo `telefone_verificado` existe, mas nada o marca como verdadeiro. Sem isso qualquer SMS vai a número não confirmado.
- **Login suspeito** (evento 9): novo dispositivo ou N falhas seguidas. Não notificar todo login.
- **Recuperação de senha por SMS** (evento 1): o link/código já existe por e-mail; falta o canal SMS com telefone verificado. **Nunca enviar a senha por SMS.**
- **Troca de e-mail/telefone** (eventos 7 e 8): avisar o contato antigo. O arquivo sugere ainda uma **confirmação antes da troca** (a confirmar o desenho; ver seção final).
- O evento 14 (saldo de SMS) fica na Parte 19.

**Entregas**
- [ ] Notificações dos eventos 3, 4, 6, 10, 11, 12, 13 (e-mail e SMS conforme a tabela)
- [ ] Verificação de telefone (código de 6 dígitos) e marcação de `telefone_verificado`
- [ ] Recuperação de senha com canal SMS (telefone verificado)
- [ ] Avisos de troca de e-mail e de telefone ao contato antigo
- [ ] Login suspeito
- [ ] SMS do código de 2FA (opcional, só se for criado)
- [ ] Testes e Documentação da API

---

### Parte 19 — Créditos de SMS da academia

**Estado:** Pendente · **Depende de:** 8, 9, 10, 17

**Já existe:** nada. O arquivo de notificações pressupõe "créditos recarregados no módulo de SMS".

**Regras**
- Cada academia tem **saldo de créditos** (razão de movimentos que só recebe lançamentos, sem editar). O saldo nunca fica negativo; **dois envios simultâneos não gastam o mesmo saldo**.
- **Recarga** paga à Spuri pelo provedor ativo do Spuri (mesmo mecanismo da Parte 10); os créditos entram **só após o pagamento confirmado** (regras da Parte 8).
- **Débito** por SMS (segmentos × tarifa) ao enfileirar; **estorno** se o envio falhar (sugestão). A tarifa é do Spuri, definida na Parte 3.
- Para a academia enviar SMS: assinatura `comunicacao_sms` **ativa** **e** saldo suficiente (a confirmar).
- **Evento 14, saldo baixo ou esgotado:** só e-mail, um aviso por vez ao cruzar o limite (para não gastar SMS avisando que acabou o SMS).
- SMS do Spuri (lista fechada) **não** consome créditos da academia.

**Entregas**
- [ ] Razão de créditos e débito atômico com estorno
- [ ] Recarga paga e confirmação
- [ ] Evento 14 por e-mail
- [ ] Front-end: saldo, recarga e extrato
- [ ] Documentação da API atualizada

**Entradas que preciso de você:** tarifa por SMS (ou por segmento), pacotes de crédito e o limite de "saldo baixo".

---

### Parte 20 — Notificações acadêmicas

**Estado:** Pendente · **Depende de:** 4, 11, 17, 19

Eventos **21 a 29** do Anexo A. Destinatário de estudante do nível escolar é **o encarregado**.

**Regras**
- **E-mail:** notas em resumo diário; faltas em resumo semanal; correção de nota, avaliação final e os demais por evento. O lançamento em lote (assíncrono) agrupa **por estudante**.
- **SMS Academia (opcional, desligado por padrão):** notas e faltas em resumo; o SMS diário de faltas é a opção mais cara de todas.
- **Evento 24, limite de faltas** (†): hoje o limite só é conferido na avaliação final. Precisa de verificação **ao lançar a falta**; no máximo **1 SMS por matéria e período**.
- Eventos 26, 28 e 29 (ano letivo finalizado, solicitações e decisões) são só por e-mail.
- Como só professores lançam notas e faltas (Parte 11), o gatilho nasce nas rotas do professor.

**Entregas**
- [ ] Resumos diário (notas) e semanal (faltas) por e-mail
- [ ] Avisos por evento (21 a 29) por e-mail
- [ ] SMS Academia configurável para 21 a 25 e 27, com resumo
- [ ] Gatilho do limite de faltas ao lançar a falta (1 SMS por matéria e período)
- [ ] Testes e Documentação da API

---

### Parte 21 — Notificações financeiras e de serviços extras

**Estado:** Pendente · **Depende de:** 7, 8, 17, 19

Eventos **30 a 40** do Anexo A.

**Regras**
- **Gancho único:** `aplicarEfeitosPagamento` (idempotente; usado pelo webhook, pela consulta manual e pela reconciliação). O evento 30 (pagamento confirmado, com recibo por e-mail) é o mais pesado: um por estudante por mês.
- Cobrança com referência REF (evento 33): só e-mail e só para REF; no GPO o provedor já envia push.
- Evento 36 (pagamento recebido depois do cancelamento local, `CobrancaConflitoPosCancelamento`): raro e urgente, avisa academia e administradores.
- **Evento 34, lembrete de mensalidade em aberto** (†): **bloqueado.** Não existe **data de vencimento** no modelo. Depende de definir a regra (ex.: dia do mês configurado pela academia) e de um job agendado. No máximo 1 SMS por mês.
- Serviços extras (38 a 40): solicitação criada vai em resumo para a academia; o recibo vem do pagamento confirmado (evento 30).

**Entregas**
- [ ] Notificações 30 a 33 e 35 a 37 no gancho de pagamento
- [ ] Notificações de serviços extras (38 a 40)
- [ ] SMS Academia configurável para 30 a 32 e 39
- [ ] Lembrete 34 (depende da regra de vencimento)
- [ ] Testes e Documentação da API

---

### Parte 22 — SMS manual da academia (individual, coletivo e citação)

**Estado:** Parcial · **Depende de:** 9, 17, 19

**Já existe:** envio de um SMS para um número (evento 41 do anexo), sem usuário, filtro, citação, créditos nem assinatura.

**Regras**
- **Quem envia:** academia e secretário (professor não, a confirmar). Exige `comunicacao_sms` ativa e saldo.
- **Destinatários:** só usuários **vinculados à academia** (encarregados, estudantes, professores, secretários).
- **Individual:** um usuário. **Coletivo:** vários, por **filtros combináveis**: tipo de usuário, nível, curso, ano, turma, disciplina, situação financeira (em dia/em atraso), serviço extra, gênero, situação (ativo/inativo), grupo do 4º ano (sugestão). Novos filtros devem ser fáceis de acrescentar.
- **Contato:** regra única da Parte 5. Estudante do nível escolar → SMS vai ao **encarregado**.
- **Sem duplicar:** o mesmo telefone recebendo o mesmo texto final é enviado uma vez; textos finais diferentes são separados. Usuário sem telefone válido é pulado e listado no relatório.
- **Citação de usuário:** o remetente insere no texto uma citação ("Bom dia [usuario.nome]...") e o back-end troca pelo **primeiro nome**, sempre. Duas formas: o **próprio destinatário** (padrão no coletivo) ou **um usuário específico** escolhido na tela. O citado precisa ser vinculado à mesma academia. A troca ocorre **no envio**; se não der para resolver, aquele SMS **não sai** e fica como falha com motivo (nunca sai "[usuario.nome]" cru). O limite de tamanho conta **depois** da troca.
- **Controle:** **prévia** (destinatários, SMS, texto resolvido) e confirmação explícita; limite diário; fila; histórico completo.

**Entregas**
- [x] Envio de um SMS por número (Tarefa 93)
- [ ] Envio individual por usuário e coletivo por filtros
- [ ] Regra de contato aplicada e deduplicação
- [ ] Citação de usuário
- [ ] Prévia, créditos, assinatura e limites
- [ ] Front-end: envio, filtros, editor com citação, prévia e histórico
- [ ] Documentação da API atualizada

---

### Parte 23 — SMS agendado e automático (academia e Spuri)

**Estado:** Pendente · **Depende de:** 22

Usa o executor de tarefas agendadas da Parte 17. **Nunca envia duas vezes.**

**Agendado**
- Data e hora no fuso de Luanda, sempre no futuro. A academia **edita ou cancela até a hora do envio**.
- Estados: agendado → enviando → enviado | cancelado | falhou | expirado.
- Filtros e citações resolvidos **no envio**; a prévia do agendamento é indicativa.
- No envio confere assinatura, saldo e limites; se inativa ou sem saldo, não envia e marca "não enviado" (mensagem genérica).
- Se o sistema ficou fora e o envio passou de uma tolerância (sugestão: 1 hora), **não envia**, marca "expirado" e avisa o autor.

**Automático da academia**
- A academia define **mensagens automáticas** enviadas aos **encarregados** (estudantes do nível escolar) ou ao **próprio estudante** (ensino superior), sempre pela regra única de contato. Não vai a professores nem secretários.
- Uma regra tem: **gatilho**, **público** (mesmos filtros da Parte 22), **texto** (com citação) e ativa/inativa; a academia pode pausar e apagar.
- Gatilhos: por **data/recorrência** (diária, semanal, mensal, data fixa, ex.: aniversário) e por **evento do sistema**. Primeira entrega só com data/recorrência; os eventos do sistema vêm depois.
- Nunca envia duas vezes pelo mesmo gatilho para o mesmo destinatário (registro de execuções).
- Com assinatura inativa ou sem saldo, as regras param; ao reativar, retomam **sem reenviar** o que foi perdido (sugestão).

**Automático do Spuri (administrador FPP)**
- O FPP define mensagens automáticas **para as academias**: destino é o contato da academia (telefone cadastrado, de preferência verificado).
- Público por filtros (estado ativa/desativada, nível; categoria de assinatura ativa/inativa, sugestão). Mesmas regras de agendamento e de idempotência.
- É **pago pelo Spuri**: não consome créditos de academia nem depende de assinatura. Tem teto diário próprio (Parte 3). Só `fpp` cria e edita; fica na auditoria.

**Entregas**
- [ ] Agendamento: criar, editar, cancelar, estados e tolerância
- [ ] Regras automáticas da academia (recorrência) e registro de execuções
- [ ] Regras automáticas do Spuri para academias (FPP)
- [ ] Gatilhos por evento do sistema
- [ ] Front-end: agendamento e gestão de regras (academia e FPP)
- [ ] Documentação da API atualizada

---

# Fase 7 — Facturação

### Parte 24 — Facturação com Ulemo

**Estado:** Pendente · **Depende de:** 8, 9, 10

**Já existe:** só a documentação (`docs/Integrações/Ulemo IBA API - Documentação.md`). A Tarefa 119 definiu: **Ulemo é provedor de faturas e recibos, não de pagamento**. Sem código.

**Regras**
- Documentos: **factura, recibo e nota de crédito**, emitidos pelo Ulemo; o sistema guarda número, data, estado e o arquivo (PDF).
- **Quem emite:** o Spuri (para academias, nos pagamentos de plano e recarga) e cada academia (para estudantes/encarregados). Cada um com **suas credenciais Ulemo**, por contexto, no cofre existente (mesmo padrão das credenciais de pagamento).
- Configuração na página **`/facturacao`**, que também lista os documentos emitidos.
- **Recibo:** emitido **automaticamente** quando o pagamento está **comprovadamente concluído** (confirmado pelo provedor, regras da Parte 8). Um por pagamento, nunca em dobro. Se o Ulemo estiver indisponível, o pagamento continua pago e o recibo fica "pendente de emissão" com nova tentativa automática; **o pagamento nunca falha por causa do recibo.**
- **Visualizar e baixar** o recibo na tela de visualização de cada pagamento (estudante, encarregado, academia, secretário).
- **Factura:** emitida pelo emissor a partir de uma cobrança, por ação dele (sugestão).
- **Nota de crédito:** só sobre documento emitido, referencia o original, exige motivo, e a soma das notas nunca passa do valor original.
- Documento emitido **nunca é editado nem apagado**; correção = nota de crédito + novo documento.
- Cliente sem NIF → "consumidor final" (sugestão). Estudante do nível escolar: documento **em nome do encarregado**, com o estudante na descrição (a confirmar).
- Com `facturacao` inativa a academia não emite; pagamentos seguem registrados e **não** se emitem recibos retroativos ao reativar (a confirmar). A emissão do Spuri não depende de assinatura.

**Entregas**
- [ ] Credenciais Ulemo por contexto no cofre
- [ ] Recibo automático após pagamento confirmado, com reemissão segura
- [ ] Factura e nota de crédito
- [ ] Página `/facturacao` e visualização/baixa no pagamento
- [ ] Documentação da API atualizada

**Entrada que preciso de você:** ambiente de testes do Ulemo e regras fiscais (NIF, séries, IVA/isenção por tipo de serviço).

---

# Fase 8 — Comunidade

### Parte 25 — Módulo Comunidade: Eventos

**Estado:** Pendente · **Depende de:** 4, 9

**Já existe:** nada. Cuidado com o nome: o back-end já tem rotas `/eventos-estudante/:codigo` e `/eventos/:event_id`, que são **eventos de auditoria do ledger**. Os da Comunidade usam **`/comunidade/eventos`**.

**Regras**
- **Comunidade** é o módulo; **Eventos** é a primeira funcionalidade (as demais não estão definidas). Fica sob a categoria `gestao_academia` até você decidir outra (a confirmar).
- A academia e o secretário criam, editam, cancelam e **agendam** eventos: título, descrição, início e fim, local, **público-alvo** (todos, ou por tipo de usuário, nível, curso, turma) e estado (agendado, cancelado, realizado).
- Só vê quem é vinculado à academia **dentro do público-alvo**. Estudante do nível escolar: visível ao estudante e ao encarregado.
- Evento cancelado continua aparecendo como "cancelado"; mudança de data/hora marca "alterado"; o passado vira histórico só de leitura.
- Aviso por SMS (sugestão) usa a Parte 22 e só aparece com `comunicacao_sms` ativa e saldo. Avisos automáticos de evento aguardam o catálogo de notificações.
- Fora do escopo: confirmação de presença.

**Entregas**
- [ ] Módulo Comunidade (rotas `/comunidade/...`) e permissões
- [ ] Eventos: criar, editar, cancelar, agendar, público-alvo e visibilidade
- [ ] Front-end e Documentação da API

---

# Anexo A — Catálogo de notificações

Fonte: `notificacoes_eventos_spuri.md` (base `4759474`, ancestral do `main` atual). **† = gatilho que ainda não existe.** **Config. ✱** = a academia decide; o ✱ é só recomendação, **não vem ligado**. O arquivo de origem **não tem os itens 15 a 20** (seção 2 ausente).

| # | Evento | Para | E-mail | SMS Spuri | SMS Academia | Parte |
|---|---|---|---|---|---|---|
| 1 | Recuperação de senha † (hoje por link; falta SMS) | Usuário | Sim | Sim (tel. verificado) | — | 18 |
| 2 | Código de 2FA no login | Usuário | Sim (existe) | Opcional | — | 18 |
| 3 | 2FA ativado | Usuário | Sim | — | — | 18 |
| 4 | 2FA desativado | Usuário | Sim | Sim | — | 18 |
| 5 | Verificação de telefone (6 dígitos) † | Usuário | — | Sim | — | 18 |
| 6 | Senha alterada (usuário, academia, admin) | Usuário | Sim | Fallback | — | 18 |
| 7 | E-mail alterado | Endereço antigo e telefone | Sim | Sim | — | 18 |
| 8 | Telefone alterado | Número antigo e e-mail | Sim | Sim (número antigo) | — | 18 |
| 9 | Login suspeito † | Usuário | Sim | — | — | 18 |
| 10 | Verificação de e-mail e boas-vindas de admin | Usuário | Sim (existem) | — | — | 18 |
| 11 | Admin criado, role alterada, desativado ou deletado | Admin | Sim | — | — | 18 |
| 12 | Academia ativada | Academia | Sim | Sim | — | 18 |
| 13 | Academia desativada (com motivo) | Academia | Sim | Sim | — | 18 |
| 14 | Saldo de SMS baixo ou esgotado † | Academia | Sim | — | — | 19 |
| 21 | Notas registradas | Estudante/encarregado | Sim (resumo diário) | — | Config. (resumo) | 20 |
| 22 | Nota corrigida | Estudante/encarregado | Sim | — | Config. | 20 |
| 23 | Faltas registradas ou corrigidas | Estudante/encarregado | Sim (resumo semanal) | — | Config. (resumo) | 20 |
| 24 | Limite de faltas próximo ou ultrapassado † | Estudante/encarregado | Sim | — | Config. ✱ | 20 |
| 25 | Avaliação final (escolar e superior) | Estudante/encarregado | Sim | — | Config. ✱ | 20 |
| 26 | Ano letivo da academia finalizado | Academia | Sim | — | — | 20 |
| 27 | Decisão de interrupção, desvinculação ou revinculação | Estudante | Sim | — | Config. | 20 |
| 28 | Solicitação de status, edição de dados ou NIF criada | Academia (NIF: admins) | Sim | — | — | 20 |
| 29 | Decisão sobre edição de dados ou NIF | Solicitante | Sim | — | — | 20 |
| 30 | Pagamento confirmado (mensalidade, serviço extra) | Pagador | Sim (recibo) | — | Config. ✱ | 21 |
| 31 | Pagamento externo registrado | Estudante | Sim | — | Config. | 21 |
| 32 | Cobrança falhou, expirou ou foi cancelada | Pagador | Sim | — | Config. | 21 |
| 33 | Cobrança criada com referência REF | Pagador | Sim (só REF) | — | — | 21 |
| 34 | Lembrete de mensalidade em aberto † | Estudante/encarregado | Sim (mensal) | — | Config. | 21 |
| 35 | Obrigação de mensalidade anulada ou reativada | Estudante | Sim | — | — | 21 |
| 36 | Conflito de pagamento pós-cancelamento | Academia e admins | Sim | — | — | 21 |
| 37 | Credenciais do provedor alteradas ou webhook rotacionado | Academia | Sim | — | — | 21 |
| 38 | Solicitação de serviço extra criada | Academia | Sim (resumo) | — | — | 21 |
| 39 | Serviço extra aprovado (pendente de pagamento), reprovado ou cancelado | Estudante | Sim | — | Config. | 21 |
| 40 | Serviço extra vinculado | Estudante | Sim | — | — | 21 |
| 41 | Comunicado escrito pela academia (já existe) | Escolhido pela academia | — | — | Sim (debita créditos) | 22 |

Itens 15–20 ausentes no arquivo de origem: ver seção seguinte.

---

# Interpretações adotadas e pontos a confirmar

Onde a lista original ou o arquivo de notificações deixava dúvida, usei o padrão abaixo. Se algum estiver errado, é aqui que se corrige.

| # | Ponto | Padrão adotado | Parte |
|---|---|---|---|
| 1 | "Um canal" no cadastro de estudante | O canal único é o cadastro feito pela academia (e secretário) | 1 |
| 2 | Secretário "menos a dos professores" | Menos **lançar notas e faltas**, que passa a ser só dos professores (vale também para a academia) | 4, 11 |
| 3 | Relação com o educando "opcional" | A relação é opcional; se "outro", o texto é obrigatório | 4 |
| 4 | Encarregado: "não pode consultar" x "consultas limitadas ao período escolar" | Sem ações e sem dados do período posterior; leitura do período escolar | 4 |
| 5 | Marco de "finalizar o nível escolar" | Aprovado no último ano do médio ou matriculado no superior, o que vier primeiro | 4 |
| 6 | Encarregados por estudante | Um por vez (trocável pela academia); um encarregado pode ter vários educandos | 4, 5 |
| 7 | Como achar encarregado já existente | Por telefone ou e-mail | 5 |
| 8 | Papéis de admin | "ADMIN ou FPP" = papéis `adm` e `fpp` da hierarquia existente | 5, 9 |
| 9 | "Pacote Finanças + paypay.go" | O pacote já existe (`internal/finance`); o PayPay entra como `provedor_paypay.go`, padrão do AppyPay | 6 |
| 10 | Gestão Acadêmica inativa | Como no texto: estudantes e encarregados só consultam (**não pagam** mensalidades). Pagamentos já iniciados continuam sendo registrados | 9 |
| 11 | Academias novas | Nascem sem assinatura; administrador atribui plano ao ativar (sugestão). Sem migração | 9 |
| 12 | Plano gratuito | A academia ativa sozinha | 9 |
| 13 | Tela "Minha assinatura" | Mostra o estado real; só as mensagens operacionais são genéricas | 9 |
| 14 | Recálculo da avaliação final | Notas: sempre. Faltas: só com reprovação por faltas ativa. Só dentro do ano letivo do registro. **Hoje não existe**, é implementação | 11 |
| 15 | Refazer matrícula com pagamentos e notas | Pagas ficam sem devolução automática; notas/faltas ficam na matrícula antiga; avaliação antiga anulada | 16 |
| 16 | Assinatura `comunicacao_sms` **e** créditos | Convivem: a assinatura dá o direito de enviar; os créditos pagam cada SMS | 19, 22 |
| 17 | Quem envia SMS manual | Academia e secretário; professor não | 22 |
| 18 | "Envios automáticos de SMS" | **Confirmado por você:** academia → encarregados/estudantes; Spuri (FPP) → academias | 23 |
| 19 | Facturação inativa | Sem emissão e sem recibos retroativos | 24 |
| 20 | Documento de estudante do nível escolar | Em nome do encarregado | 24 |
| 21 | Categoria da Comunidade | `gestao_academia` | 25 |
| 22 | **Contradição no arquivo de notificações:** a legenda diz que os ✱ **não** vêm ligados; o fim do arquivo diz que vêm "já ligados por padrão" | Adotei a legenda (você a escreveu depois): **nascem desligados**, só recomendados | 17, 20, 21 |
| 23 | **Frase incompleta** no início da seção 1 ("Quando o telefone tá verificado e 2FA tá ativada, em vez de") | Ignorada até você completar. Pode mudar o evento 2 (2FA por SMS em vez de e-mail) | 18 |
| 24 | **Itens 15 a 20 ausentes** (seção 2) | Fora do plano até você enviar | 18 a 21 |
| 25 | Evento 7 ("confirmação para o e-mail recente...") | Desenho ambíguo. Adotei só: avisar o contato antigo; a confirmação antes da troca fica para você esclarecer | 18 |
| 26 | Evento 34 (lembrete de mensalidade) | Bloqueado: não existe data de vencimento. Preciso da regra | 21 |
| 27 | Item 1 do arquivo (recuperação "troca a senha na hora") | **Desatualizado:** a Tarefa 123 já trocou por link. Resta o canal SMS | 18 |
| 28 | Item 5 do arquivo (limitadores "no-op") | **Desatualizado:** foram removidos; login e e-mail seguem sem limite. Virou pré-requisito | 17 |

## O que ainda preciso receber

- **Itens 15 a 20** das notificações e a **frase incompleta** da seção 1 (pontos 23 e 24 da tabela acima).
- **Parte 3:** confirmar ou corrigir a lista de variáveis implementada (é a proposta da Parte 3, com `MEGA_ROOT_FOLDER` como texto visível).
- **Parte 6:** credenciais de teste do PayPay e textos dos manuais (PayPay e AppyPay).
- **Parte 12:** regras de cada academia superior, quando cada uma entrar.
- **Parte 19:** tarifa por SMS, pacotes de crédito e limite de saldo baixo.
- **Parte 21:** regra de data de vencimento da mensalidade.
- **Parte 24:** ambiente de testes do Ulemo e regras fiscais.

## Fora do escopo deste plano

- **Pesquisa e Estatística** (retirada do plano por enquanto).
- Provedor **Intelize** (documentado em `docs/Integrações/`, mas só o PayPay foi pedido).
- Reset de credenciais para encarregado, professor e secretário (só o do estudante foi pedido).
- Confirmação de presença em eventos; outras funcionalidades da Comunidade.
- Devolução automática de pagamentos.

---

# Registro de andamento

Preencher a cada entrega (ver "Como acompanhar o andamento").

| Data | Parte / item | Tarefa | Commit | Observação |
|---|---|---|---|---|
| 2026-11-17 | Plano v2: itens já existentes marcados `[x]` | — | — | Verificado em `main` @ `d9c47fd` |
| 2026-11-18 | Parte 1 (todos os itens) | 128 (back) · 38 (front) | `673be19` (back) · `740ce90` (front) | Marcada só em 2026-11-19, junto com a Tarefa 129 |
| 2026-11-19 | Parte 2 (menos o HTTP do financeiro) | 129 (back) | ver o texto do commit da Tarefa 129 | 273 rotas e handlers idênticos antes e depois; suíte: mesmos 1057 testes passam + 38 novos |
| 2026-11-19 | Parte 3 (todos os itens) | 129 (back) · 39 (front) | ver o texto do commit da Tarefa 129 | Migration 139. Regra nova em "Como acompanhar": o plano atualizado vai em toda entrega |
| | | | | |
