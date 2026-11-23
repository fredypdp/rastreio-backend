# Plano de Atualizações — Regras de Negócio e Partes (v2)

> Base: `Lista de tarefas.md` (2026-11-08) + `notificacoes_eventos_spuri.md` · Atualizado em 2026-11-23
> **Back-end verificado:** `rastreio-backend`, `main` @ `d9c47fd`, por leitura do código, das rotas e dos documentos de tarefa. A suíte de testes **não foi executada**.
> **Verificação de 2026-11-21 (Partes 8 e 13):** `rastreio-backend`, `main` @ `ffc13e2`. `gofmt`, `go vet` e `go test ./...` limpos, com os testes de integração executados em PostgreSQL 16.15 (Go 1.27.4; o CI usa 1.24.12). O front-end não foi reanalisado nessa verificação.
> **Front-end:** `rastreio-frontend`, `main` @ `740ce90` (2026-11-18), lido e validado com `tsc` e `eslint` na Tarefa 39. Os trechos do plano sobre telas que ainda não foram implementadas continuam vindo das regras do plano; cada tarefa de front começa pela análise do código.
> **Atualização de 2026-11-21 (módulos, serviços, facturação, passaporte, sumários):** só plano, sem código. `rastreio-backend`, `main` @ `ffc13e2` (o mesmo commit da verificação anterior): conferidos por leitura a estrutura de `internal/`, quem importa `internal/finance`, os status por nível do estudante, os campos de BI, o agregado `Sumario` e o agregado `ServicoExtra`. Nada foi executado. O front-end não foi reanalisado.
> **Verificação de 2026-11-23:** `rastreio-backend`, `main` @ `7f9b70e`: lidos o commit das Tarefas 130 e 131, os documentos dessas tarefas, o plano que o commit alterou, `internal/permissoes` e o reenvio de credenciais. O front-end é privado e não foi lido; o que consta dele vem do plano e dos documentos de tarefa.
> **Partes 4 e 5** estão implementadas (back-end `7f9b70e`, Tarefas 130 e 131; front-end, Tarefas 40 e 41), e os detalhes da implementação estão integrados nesta versão.

## O que mudou nesta versão

1. Plano conferido com o back-end: cada Parte tem **Estado** e o que **já existe** no código. Várias partes ficaram menores.
2. **Eventos** passa a ser funcionalidade do módulo **Comunidade**, tratado no `Plano de Atualizações 2.md`.
3. **Não há academias cadastradas**: removidas as migrações de dados e a "assinatura de migração".
4. **Pesquisa e Estatística** saiu do plano.
5. **Envios automáticos de SMS** redefinidos (Parte 27): academia → encarregados/estudantes; Spuri (FPP) → academias.
6. **Notificações** do arquivo anexo incorporadas: Partes 21 a 27 e Anexo A.
7. Nova seção **Como acompanhar o andamento** (logo abaixo) para marcar o que foi implementado.
8. **Atualização de 2026-11-21:** (a) Parte 8 ganha a **Garantia 4** (valor monetário com tipo e validação certos); (b) Parte 13 registra que **zerar por faltas já é só leitura em memória** (com teste que prova isso), que o recálculo **nunca grava nota**, e que o **código morto da avaliação final manual é eliminado**, sem suporte a legado; (c) a regra 3 passa a conter a **convenção de dinheiro**; (d) pontos 29 e 30 em "Interpretações adotadas".
9. **Atualização de 2026-11-21 (módulos, serviços, facturação, passaporte, sumários):** (a) **módulos, recursos e planos passam a ser definidos no código, e os preços são editáveis pelo FPP** (Gestão Académica, Finanças, Facturação, Comunicação e Serviços); administradores deixam de criar planos (Parte 9); o bloqueio por módulo é feito em **middleware no back-end** e a **consulta continua**; (b) **novo modelo de cobrança por número de estudantes**: a academia define o **limite** ao contratar e paga **excedente** por módulo para ultrapassá-lo; **demonstração gratuita** de 15 dias (Partes 9 e 10); preços editáveis pelo FPP, valendo só para quem não tem o módulo contratado, e Finanças e Serviços a 15 Kz por estudante; (c) **toda cobrança pode ser marcada como paga manualmente**, com a mesma sequência de efeitos de um pagamento por provedor (Parte 11, nova); (d) **Serviços** vira catálogo nosso, com Transporte e Atividade extracurricular (Parte 7 reescrita, Parte 8 ajustada); (e) **Facturação** sobe para a Parte 12, logo depois das Partes 8 a 11, ganha cópia de segurança no Mega e envio do recibo por e-mail; (f) novas Partes **19** (sumários sequenciais) e **20** (passaporte no cadastro); (g) o pacote `finance` vai para `internal/handlers/finance` (Parte 2); (h) a Parte 23 passa a tratar os pacotes de SMS como **cota de envio**, complemento das Partes 26 e 27; (i) **renumeração** das partes depois da 10; (j) revisões do mesmo dia: o front permite navegar e bloqueia a ação com aviso (CTA) para a página de assinatura do módulo; a Comunidade e seus recursos vão para o `Plano de Atualizações 2.md`; o reset de credenciais de encarregado, professor e secretário vira a Parte 28; o evento 34 foi retirado.
10. **Atualização de 2026-11-23 (versão final):** as Partes 4 e 5 foram implementadas (Tarefas 130 e 131 no back-end; 40 e 41 no front-end), com os detalhes integrados ao plano; entram o passo 9 (versão da API em SemVer) e as interpretações 56 a 64; a seção de renumeração foi removida.

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
8. **Este arquivo (`docs/Planos de Atualização/Plano de Atualizações 1.md`) atualizado vai em TODA entrega de back-end**, dentro do zip, ao lado do documento da tarefa e da `Documentação da API.md` com a versão atualizada (passo 9), com os passos 1 a 7 já aplicados (itens marcados, Estado, tabela da Visão geral, ✅ e Registro de andamento). Uma entrega sem o plano atualizado está incompleta. Se a entrega for só de front-end, o plano vai mesmo assim no zip do back-end (ou em um zip avulso de `docs/`).
9. **A `Documentação da API.md` vai em toda entrega que mexe na API, com a linha `Versão atual:` (no topo) atualizada no formato de Versionamento Semântico `MAJOR.MINOR.PATCH`.** Escolha o número assim:
   - **MAJOR** — algo que funcionava deixa de funcionar para quem já consome a API: rota removida ou renomeada, campo JSON removido ou com outro significado, campo novo **obrigatório** num pedido, regra que passa a negar o que antes era permitido.
   - **MINOR** — acréscimo compatível: rota nova, campo novo opcional, permissão nova, código de erro novo em fluxo novo.
   - **PATCH** — correção de comportamento ou de texto sem mudar o contrato.
9.2 Atualizar também a versão no Dockerfile

   Atualize junto: `modificado:` no cabeçalho e a tabela **Versionamento da API** (seção 1.2 da documentação), com a versão, a(s) tarefa(s) e um resumo de uma linha. O documento da tarefa informa a versão anterior e a nova. Uma entrega que altera a API sem atualizar a versão está incompleta.

**Outras orientações**
- Cada Parte vira uma ou mais tarefas (modelo de criação de tarefa). Backend: a próxima tarefa livre é a **132** (a 130 fechou a Parte 4 e a 131, a Parte 5). Front-end (numeração própria): a próxima é a **42** (a 40 fechou a Parte 4 e a 41, a Parte 5).
- **(sugestão)** = regra acrescentada por mim para fechar lacuna; pode ser cortada. **(a confirmar)** = decisão pendente, com padrão na seção final.
- Ordem de deploy padrão: back-end primeiro, depois front-end. Exceção: partes que removem funcionalidade (1 e 14, e a remoção da criação genérica de serviços na Parte 7): front-end primeiro.
- As partes estão na ordem de execução; as que não dependem uma da outra podem andar em paralelo (ver "Ordem de execução").

---

## Visão geral

| Parte | O que é | Depende de | Estado | Tarefa |
|---|---|---|---|---|
| **Fase 1 — Fundações** | | | | |
| 1 ✅ | Remover a matrícula pública | — | Implementado | 128 (back) · 38 (front) |
| 2 | Back-end em pacotes (refatoração) | 1 | Parcial (falta o pacote `finance`) | 129 (back) |
| 3 ✅ | Configurações do sistema (variáveis de ambiente → banco) | 2 | Implementado | 129 (back) · 39 (front) |
| 4 ✅ | Usuários e permissões (encarregado, secretário, professor) | 2, 3 | Implementado | 130 (back) · 40 (front) |
| **Fase 2 — Credenciais** | | | | |
| 5 ✅ | Credenciais temporárias: ajustes do cadastro e do reset | 4 | Implementado | 131 (back) · 41 (front) |
| **Fase 3 — Finanças** | | | | |
| 6 | Provedor PayPay e troca de provedor | 2 (inclui `finance`), 4 | Parcial | |
| 7 | Serviços: catálogo fixo (Transporte e Atividade extracurricular) | 2 (inclui `finance`) | Pendente | |
| 8 | Validação do módulo financeiro | 6, 7 | Pendente | |
| **Fase 4 — Assinaturas e pagamento manual** | | | | |
| 9 | Módulos e assinaturas: catálogo no código, preço por limite de estudantes, demonstração, bloqueio e ativação | 4 | Pendente | |
| 10 | Contratação paga de módulos e de excedente de estudantes | 8, 9 | Pendente | |
| 11 | Pagamento manual (fora do sistema) | 4, 8, 10 | Pendente | |
| **Fase 5 — Facturação** | | | | |
| 12 | Facturação com Ulemo | 8, 9, 10, 11 | Pendente | |
| **Fase 6 — Gestão acadêmica** | | | | |
| 13 | Notas e faltas só por professores + recálculo da avaliação final | 4 | Pendente | |
| 14 | Avaliação final do ensino superior fixa no código | 2, 13 | Pendente | |
| 15 | Horários das turmas | 4 | Pendente | |
| 16 | Grupos do 4º ano de cursos técnicos | 4 | Parcial | |
| 17 | Material de apoio | 4, 9 | Pendente | |
| 18 | Refazer matrícula | 8, 13 | Pendente | |
| 19 | Sumários sequenciais | 4 | Pendente | |
| 20 | Documento de identificação alternativo (passaporte) | 4, 5 | Pendente | |
| **Fase 7 — Comunicação e notificações** | | | | |
| 21 | Núcleo de notificações (e-mail e SMS) | 3, 5 | Parcial | |
| 22 | Notificações de segurança e conta | 21 | Pendente | |
| 23 | Pacotes de SMS: cota de envio da academia | 8, 9, 10, 11, 21 | Pendente | |
| 24 | Notificações acadêmicas | 4, 13, 21, 23 | Pendente | |
| 25 | Notificações financeiras e de serviços | 7, 8, 21, 23 | Pendente | |
| 26 | SMS manual da academia (individual, coletivo, citação) | 9, 21, 23 | Parcial | |
| 27 | SMS agendado e automático (academia e Spuri) | 26 | Pendente | |
| **Fase 8 — Credenciais complementares** | | | | |
| 28 | Reset de credenciais de encarregado, professor e secretário | 4, 5 | Pendente | |

### Ordem de execução

**1. Sequência obrigatória.** Cada passo só começa quando o anterior fecha. As partes entre colchetes precisam estar prontas juntas, e `+` marca uma segunda parte de que o passo também depende:

`2 → [6 e 7] → 8 → (+ 9) 10 → 11 → (+ 21) 23 → 26 → 27`

- A **2** (pacote `finance`) vem antes de **6** e **7**, porque as duas mexem nesse pacote. A **8** precisa das duas.
- A **10** precisa da **8** e da **9**. A **11** precisa da **10**.
- A **23** precisa da **11** e da **21**. A **26** precisa da **23**, e a **27**, da **26**.
- Dependem das partes da sequência, mas ficam fora dela: **12** (depois da 11), **14** (depois da 13), **18** (depois da 8 e da 13), **22** (depois da 21), **24** (depois da 13, 21 e 23) e **25** (depois da 7, 8, 21 e 23).
- As Partes **1, 3, 4 e 5** já foram feitas.

**2. Etapas em ordem, com o que pode andar em paralelo.** Em cada etapa, a coluna "Em paralelo" pode ser feita em qualquer combinação, desde que a parte de que ela depende já tenha fechado.

| Etapa | Obrigatória | Em paralelo | Observação |
|---|---|---|---|
| 0 | 1, 3, 4, 5 (feitas) | — | Já aplicadas (commit `7f9b70e`; Tarefas 130, 131, 40 e 41) |
| 1 | 2 (pacote `finance`) | 13, 15, 16, 19, 20, 21, 28 | Nenhuma dessas mexe em `finance` |
| 2 | 6 e 7 (andam juntas) | 9; 14 (se a 13 já fechou); 22 (se a 21 já fechou) | A 9 está no caminho crítico e só depende da 4 |
| 3 | 8 | 17 (precisa da 9) | |
| 4 | 10 | 18 (precisa da 8 e da 13) | |
| 5 | 11 | O que sobrou das etapas anteriores | |
| 6 | 12 e 23 | — | As duas só esperam a 11 (a 23 também espera a 21) |
| 7 | 24, 25 e 26 | — | Esperam a 23 (e a 21) |
| 8 | 27 | — | Espera a 26 |

**Cuidados com o paralelo**
- A **Parte 9** declara o módulo em **todas** as rotas. Se rodar ao mesmo tempo que partes que criam rotas (13, 15, 16, 19, 28...), aplique a 9 por último ou reconfira as rotas novas depois, pela regra 11.
- Cada parte em paralelo é uma tarefa separada, com seu pacote de arquivos e seu documento de tarefa. Como cada entrega substitui arquivos inteiros, duas tarefas que mexam no **mesmo arquivo** se sobrescrevem: aplique uma de cada vez e rode o comando de divergência (`git fetch origin && git diff <commit-base> origin/main`) antes de substituir.
- Se preferir uma parte de cada vez, a ordem das etapas acima já serve como ordem única.

---

## Termos usados

- **Spuri**: a plataforma. **Administradores**: papéis `gerente`, `adm` e `fpp` (hierarquia existente). Atribuição e pagamento manual de módulos do Spuri: só `fpp`.
- **Academia**: a instituição. Cada academia só vê os próprios dados.
- **Nível escolar**: Ensino Primário e Iº Ciclo (1º–9º ano) e Ensino Médio. **Ensino superior** é separado.
- **Educando**: estudante do nível escolar. **Pagador**: estudante ou encarregado quando paga a academia; a academia, quando paga o Spuri.
- **Eventos (Comunidade)**: tratados no `Plano de Atualizações 2.md`.
- **Módulo**: conjunto de recursos vendido à academia (Gestão Académica, Finanças, Facturação, Comunicação, Serviços). Definido no código (Parte 9).
- **Estudante ativo**: estudante **vinculado à academia** (vínculo atual; desvinculado ou de outra academia não conta) e com pelo menos um dos status por nível (`status_escolar_fundamental`, `status_escolar_medio`, `status_superior`) em `em_andamento`. `inativo` e `finalizado` não contam. Nomes e valores conferidos em `main` @ `ffc13e2`; a Parte 9 confirma a regra no código.
- **Limite de estudantes**: número que a academia define ao contratar um módulo que cobra por estudante; é o máximo de estudantes ativos que ela pode cadastrar no período. **Excedente** = quantos a mais ela quer cadastrar, pago por módulo (Parte 9).
- **Pagamento manual**: cobrança paga **fora do sistema** e marcada como paga por quem tem permissão (Parte 11). Gera os mesmos efeitos de um pagamento confirmado pelo provedor.

## Regras que valem para todas as partes

1. **O back-end decide.** Permissão, assinatura e preço são validados no servidor; o front só esconde ou mostra.
2. **Isolamento.** Nenhuma academia acessa dados de outra.
3. **Dinheiro** em Kwanza (AOA/Kz), calculado no servidor, nunca recebido do cliente. **Convenção de valor monetário** (Tarefa 26): `NUMERIC(14,2)` no banco; `float64` no Go por causa do contrato do provedor (a AppyPay define `amount` como `number<double>`), sempre com `roundAmount` (2 casas, arredondando meio para longe do zero), somas arredondadas depois de acumular e **nunca `==` em valores monetários** (usar `amountsEqual`). Todo campo monetário novo passa pelo **validador único** da Parte 8 (finito, maior que zero, no máximo 2 casas, dentro do limite do `NUMERIC(14,2)`); entrada inválida é **rejeitada**, nunca arredondada em silêncio. Não se migra o tipo no Go (ver "Fora do escopo").
4. **Datas** no fuso de Luanda (UTC+1). Plano: 1 mês = 30 dias, calendário civil.
5. **Segredos** criptografados, nunca devolvidos em texto claro, nunca em log.
6. **Auditoria** em ação sensível: quem, quando, o quê (o ledger e `SaveWithAudit` já fazem isso).
7. **Nada de histórico apagado:** pagamentos, notas, documentos fiscais e matrículas são arquivados ou anulados.
8. **Sem duplicidade:** repetir uma operação não gera cobrança, SMS, e-mail ou recibo em dobro.
9. **Menor de idade:** contato de estudante do nível escolar é sempre o do encarregado (regra única, Parte 5).
10. **Mensagens sensíveis são genéricas** quando o assunto é assinatura (Parte 9). Exceção: o aviso (CTA) de contratação do módulo, mostrado à academia.
11. **Todo recurso novo declara o módulo que o protege** (Parte 9). Depois da Parte 9, nada nasce sem proteção.
12. **Notificação de SMS paga pela academia nasce desligada.** O que o sistema "recomenda" não vem ligado por padrão (evita gasto indesejado).
13. **Cada parte atualiza a Documentação da API.md** (e a sua versão, no formato de Versionamento Semântico) e entrega migrations que rodam do zero.
14. **Cada parte atualiza este plano** (ver "Como acompanhar o andamento").
15. **Módulos que cobram por número de estudantes cobram pelo limite contratado**: o número que a academia define ao contratar, que é o **máximo de estudantes ativos** que ela pode cadastrar durante o período do plano. Passar dele exige pagar o **excedente**, por módulo (Parte 9). A contagem de estudantes ativos serve só para conferir o limite.
16. **Toda cobrança pode ser marcada como paga manualmente** (estudante → academia e academia → Spuri), com a mesma sequência de efeitos de um pagamento por provedor (Parte 11).

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

**Estado:** Parcial (Tarefa 129: tudo, menos o pacote `finance`) · **Depende de:** 1 (liberada)

**Já existe:** `internal/handlers` é **um único pacote com 61 arquivos** (fora testes). `finance`, `projections`, `domain/aggregates`, `services`, `middleware`, `storage` já são pacotes. `finance` é a camada de serviço do financeiro e fica **fora** da pasta dos módulos (`internal/finance`); o HTTP do financeiro (`financeiro_*.go`, `mensalidade_handlers.go`, `efeitos_pagamento.go`) ainda está solto em `internal/handlers`. `internal/finance` é importado por `cmd/server`, por `internal/handlers`, por `apoio` e por `servicosextras` (conferido em `main` @ `7f9b70e`).

**Regras**
- Refatoração pura: mesmas rotas, respostas, regras e tabelas.
- Um pacote por módulo: Notas, Faltas, Estudantes, Turmas, Avaliações, Comunicação e os demais que o código mostrar (cursos, matérias, serviços extras, sumários...).
- Pacotes de módulo **não importam uns aos outros**. O que é comum vai para um pacote de apoio.
- Uma entrega por pacote; a suíte completa dá o mesmo resultado antes e depois.
- **Pacote `finance` no mesmo diretório dos outros (2026-11-21):** `internal/finance` (serviço) e o HTTP do financeiro viram **um só pacote**, `internal/handlers/finance/`, ao lado de `notas/`, `turmas/` etc. Interpretação adotada: nome do pacote `finance`, um pacote só (ver ponto 31 de "Interpretações").
- **Cuidado com os imports:** `apoio` e `servicosextras` já importam `finance`, e a regra "pacotes de módulo não se importam" tem teste (`TestPacotesDeModuloNaoSeImportam`). A tarefa lê esses imports e decide: mover para `apoio` o que for comum, ou tratar `finance` como pacote de base permitido no teste, com o motivo escrito.
- **Ordem:** o financeiro agora entra **antes das Partes 6 e 7** (PayPay e serviços mexem em `finance`), para não escrever código novo no caminho antigo e mover depois. As Partes 4 e 5 já foram aplicadas, então a mudança pode começar já.

**Entregas**
- [x] Pacote de apoio (helpers comuns de `handlers/helpers.go`) (Tarefa 129: `internal/handlers/apoio` e, para helpers de teste, `internal/handlers/testsupport`)
- [x] Notas · [x] Faltas · [x] Estudantes · [x] Turmas · [x] Avaliações · [x] Comunicação (Tarefa 129)
- [x] Demais módulos (cursos, matérias, sumários, serviços extras, documentos) (Tarefa 129; também academias, administradores, autenticação, lotes e registros)
- [ ] Financeiro: `internal/finance` e o HTTP do financeiro viram `internal/handlers/finance/` (antes das Partes 6 e 7; mesmas rotas, respostas, regras e tabelas; suíte idêntica antes e depois)

**Como ficou (Tarefa 129)**
- `internal/handlers/<módulo>/` para academias, administradores, autenticacao, avaliacoes, comunicacao, cursos, documentos, estudantes, faltas, lotes, materias, notas, registros, servicosextras, sumarios e turmas. `internal/handlers` guarda só o HTTP do financeiro.
- `apoio` guarda o que mais de um módulo usa (helpers HTTP, o motor da avaliação final, a infraestrutura de lotes, o cadastro de estudante reaproveitado pelo cadastro em lote). Cada módulo ficou com os seus próprios wrappers de lote e itens de job (`CriarCursoBatch`, `AtivarAcademiaJobItem`...); só a infraestrutura genérica é compartilhada.
- A regra "módulos não se importam" tem teste: `TestPacotesDeModuloNaoSeImportam` (pacote `apoio`).
- Para efeito da regra 4 de "Como acompanhar", as Partes 3 e 4 estão liberadas. O que falta da Parte 2 (pacote `finance`) não bloqueia as Partes 3 a 5, mas **bloqueia as Partes 6 e 7**.

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
- Novos valores das próximas partes (limites de SMS, tamanho de arquivo...) já nascem nesta tela. Preços de módulos e pacotes de SMS **não** ficam aqui: têm tela própria do FPP, com versões e auditoria (Parte 9).

**Entregas**
- [x] Lista aprovada de variáveis (Tarefa 129: a proposta acima, em `internal/sysconfig`; lista confirmada por você em 2026-11-22)
- [x] API de leitura e escrita (só administradores) com validação por tipo (Tarefa 129: `GET/PUT/DELETE /admin/sistema/configuracoes`)
- [x] Leitura banco → ambiente, com cache e indicação "na hora / no reinício" (Tarefa 129)
- [x] Segredos mascarados e fora dos logs (Tarefa 129: cifrados em tabela própria; nunca no ledger, na projeção, em resposta ou em log)
- [x] Tela de administração (front) (Tarefa 39: `/configuracoes/sistema`)

**Como ficou (Tarefa 129 e Tarefa 39)**
- Quem pode: `adm` e `fpp` consultam; só `fpp` altera ou remove. Cada chave declara `aplicacao`: `imediato` (vale na próxima leitura nesta instância e em até 60 s nas outras) ou `reinicio` (lida uma vez no arranque: as cinco `RECONCILIACAO_*` e as três `MEGA_*`).
- `MEGA_ROOT_FOLDER` ficou como **texto visível**, não como segredo: é só o nome de uma pasta e esconder o valor impediria o administrador de conferi-lo. Confirmado por você.
- A tabela `projection_sistema_config` dos anos letivos **não** foi reaproveitada: a nova `projection_configuracao_sistema` (migration 139) tem lápide e guarda de versão, e os segredos vão para `configuracao_sistema_segredos`.
- Para entrar nesta tela, um novo valor das próximas partes só precisa de uma linha em `sysconfig.definicoes` e de ler a chave com `sysconfig.String`.

---

### Parte 4 — Usuários e permissões ✅

**Estado:** Implementado (Tarefas 130 e 40) · **Depende de:** 2, 3

**Já existia (antes da Tarefa 130):** só três tipos de login: **admin** (`gerente`/`adm`/`fpp`), **academia** e **estudante**. O **encarregado** era apenas o telefone e o BI dentro do cadastro do estudante (sem nome, sem login). Não existiam professor nem secretário. **Agora:** os três tipos existem (`secretario`, `professor`, `encarregado`); o BI do encarregado segue nos documentos de matrícula do estudante e o telefone passou a ser da conta do encarregado.

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
| Gerir turmas, cursos, matérias, sumários, serviços extras, finanças, comunicação, ano letivo | ✔ | ✔ (**as que a academia conceder**) | ✘ | ✘ | ✘ | — |
| Gerir secretários; alterar dados, NIF, alvará e nível da academia; credenciais do provedor de pagamento | ✔ | ✘ (reservado à academia) | ✘ | ✘ | ✘ | — |
| Registrar pagamento manual de cobrança do Spuri (módulos e pacotes) | ✘ | ✘ | ✘ | ✘ | ✘ | ✔ (só FPP) |

_Nota (2026-11-21): a última linha mudou porque os planos agora são fixos no código (Parte 9). A regra é implementada na Parte 11, não aqui._

**Regras comuns:** criação segue a cadeia de credenciais temporárias que já existe (enviadas ao telefone **do próprio usuário**, troca obrigatória no primeiro acesso); telefone obrigatório (sugestão). Desativar não apaga o que a pessoa fez.

**Secretário**
- Criado pela academia; pertence a uma só academia.
- Mesmas funções da academia, **exceto lançar notas e faltas** e **o que é da própria conta da academia ou retroativo em relação a ele**: gerir secretários (criar, editar permissões, desativar, eliminar), alterar os dados/NIF/alvará/nível escolar da academia e as credenciais do provedor de pagamento. Estas ações são **reservadas à academia** e nenhuma permissão as concede.
- **Permissões escolhidas por secretário:** ao criar cada secretário, a academia marca quais permissões ele terá, num catálogo (estudantes, turmas, cursos, matérias, **sumários**, notas e faltas só para consulta, avaliações, serviços extras, financeiro, comunicação, ano letivo, professores). "Gerir" traz "ver" junto. O servidor recusa permissão fora do catálogo (por exemplo, lançar notas). Detalhe na seção 26 da Documentação da API.
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
- [x] Tipos de login `secretario`, `professor`, `encarregado` e middlewares de permissão (Tarefa 130)
- [x] Cadastro de secretário, com permissões escolhidas pela academia (Tarefa 130) · [x] Cadastro de professor com vínculos turma+disciplina (Tarefa 130)
- [x] Cadastro e vínculo de encarregado (migrando o telefone/BI hoje no estudante) (Tarefas 130 e 131)
- [x] Regra de acesso do encarregado (nível escolar, fim do período, desvinculação) (Tarefa 130)
- [x] Matriz de permissões aplicada às rotas existentes (Tarefa 130; negar por padrão, com teste de varredura do router)
- [x] Front-end: telas de cadastro e áreas de cada tipo (Tarefa 40)
- [x] Documentação da API atualizada (Tarefa 130; versão 4.0.0)

**Como ficou a implementação (resumo; o detalhe está na Documentação da API, seções 3.6 e 26 a 28)**
- O secretário age **como a academia** e o encarregado **como o educando** (cabeçalho `X-Educando`), depois de uma porta com *negar por padrão*: rota não classificada em `internal/permissoes/rotas.go` fica inacessível. Um teste percorre o router e falha se aparecer rota sem decisão.
- A auditoria grava **quem agiu** (`ator_id`, `ator_tipo`) e a conta em nome de quem se agiu.
- O professor tem **rotas próprias de consulta** (`/professor/*`), já filtradas pelos vínculos; lançar e corrigir notas e faltas é a Parte 13.
- O estudante e o encarregado novo são gravados **na mesma transação**.
- Uma só tabela (`projection_contas_pessoa`, migration 140) guarda os três tipos de conta. Códigos de login `SC`, `PF` e `EN` + 6 dígitos; e-mail único entre **todos** os tipos de conta.
- A Tarefa 130 e a 131 (e as 40 e 41 do front-end) entram no **mesmo pacote** e vão para produção **na mesma janela**, porque a Parte 5 é uma mudança incompatível (API 4.0.0).

---

# Fase 2 — Credenciais

### Parte 5 — Credenciais temporárias: ajustes do cadastro e do reset ✅

**Estado:** Implementado (Tarefas 131 e 41) · **Depende de:** 4

**Já existe (Tarefa 123 / SEC-04)**
- [x] Cadastro do estudante envia as credenciais por SMS ao telefone certo: **superior → estudante; escolar → encarregado** (`TelefoneDestinoCredenciaisEstudante`).
- [x] **Reset/reenvio** `POST /estudantes/:codigo/credenciais/reenviar`: gera nova senha temporária, invalida as sessões, exige troca no primeiro acesso, intervalo mínimo de 10 minutos, auditado.

**O que falta**
- O admin hoje é só `fpp` e pode resetar **qualquer** estudante. A regra é: **`adm` ou `fpp`, só estudante sem nenhuma academia**. A academia continua só com os seus; **secretário** entra com a mesma regra da academia.
- Cadastro do nível escolar: com a conta do encarregado (Parte 4), as credenciais do estudante **e do encarregado novo** vão só para o telefone do encarregado. Encarregado já existente é vinculado sem novas credenciais (identificado por telefone ou e-mail, como foi implementado; a identificação pelo bilhete de identidade é corrigida na Parte 20). Se o envio falhar, **nunca** cai para o telefone do estudante; a academia vê o erro e reenvia.
- A função de destino tem um caminho em que, sem nível definido, usa o telefone do estudante. Para estudante do nível escolar isso não pode acontecer.
- Transformar na **regra única de contato**: dado um estudante, devolve telefone **e e-mail** certos (escolar → encarregado; superior → o próprio). É a única função usada pelas credenciais, pelas notificações (Parte 21) e pelos SMS (Partes 26 e 27).

**Entregas**
- [x] Destino das credenciais por nível (Tarefa 123)
- [x] Reset/reenvio com intervalo e auditoria (Tarefa 123)
- [x] Reset: admin `adm`/`fpp` só para estudante sem academia; secretário como a academia (Tarefa 131)
- [x] Cadastro escolar com conta do encarregado e credenciais só ao telefone dele (Tarefas 131 e 41)
- [x] Remover o caminho que cai no telefone do estudante no nível escolar (Tarefa 131)
- [x] Regra única de contato (telefone e e-mail) em função reutilizável: `services.ResolverContato` / `ResolverContatoDoEstudante` (Tarefa 131)
- [x] Testes por perfil e por nível (Tarefa 131)

**Como ficou a implementação**
- O encarregado novo e o estudante nascem juntos; as credenciais dos dois vão para o telefone do encarregado (as dele primeiro). Encarregado já existente (mesmo telefone ou e-mail) é só vinculado, sem novas credenciais. Dois estudantes cadastrados em sequência com o mesmo encarregado novo reaproveitam a mesma conta, mesmo antes de a projeção processar o primeiro.
- `PUT /estudante/encarregado/telefone` foi **removida** (o telefone é da conta do encarregado). A restrição única do telefone do encarregado no estudante saiu (migration 141): um encarregado pode ter vários educandos.
- Cadastro em massa: o modelo Excel ganhou as colunas I a N do encarregado.

---

# Fase 3 — Finanças

### Parte 6 — Provedor PayPay e troca de provedor

**Estado:** Parcial · **Depende de:** 2 (inclusive o pacote `finance` na pasta nova), 4

**Já existe (Tarefas 119 a 121)**
- [x] Módulo financeiro dividido por provedores: interface `Provedor` (`finance/provedor.go`), AppyPay em `provedor_appypay.go`, **provedor novo = um arquivo `provedor_<nome>.go` + uma linha em `fabricasProvedores`**.
- [x] **Provedor ativo único por contexto** (`spuri` ou cada academia). Definir o provedor exige credenciais; as primeiras credenciais viram o ativo.
- [x] **Mecanismo retroativo:** cada cobrança guarda o provedor que a emitiu; consulta, cancelamento, webhook e pagamento externo usam o dele. **Remover credenciais com cobrança pendente é recusado (409).**
- [x] Credenciais cifradas por contexto/ambiente/provedor, snapshot ao editar ou remover, **job de reconciliação**, **métodos de pagamento filtrados por provedor e por pagador**.
- A interface já cobre o PayPay (chaves RSA, webhook sem segredo gerado pelo Spuri, resposta `success`). A documentação está em `docs/Integrações/PayPay API.md`.
- O "pacote Finanças" já existe como `internal/finance` (a Parte 2 o move para `internal/handlers/finance`). O PayPay segue o padrão atual: **`provedor_paypay.go`**, sem criar pacote novo; funções comuns já ficam em arquivos neutros do pacote.

**O que falta**
- Do PayPay entra só o necessário: **MULTICAIXA Express (§3.2 → método interno `GPO`)**, **MULTICAIXA Reference (§3.3 → `REF`)**, **consultar pedido (§3.6)**, **fechar pedido (§3.5)** e **notificações (§4)**. Fora: reembolso, transferências, Kwik, agente.
- **Tela de credenciais** (módulo de finanças): no topo, a escolha do provedor; cada um (PayPay, AppyPay) tem uma **subtela** com os campos de credenciais e o **manual**. Mostra qual está ativo. O PayPay ainda **não tem manual** (a subtela fica sem ele por enquanto); o AppyPay **mantém os textos atuais**.
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
- [ ] Front-end: seletor de provedor e subtela do PayPay (campos; sem manual por enquanto)
- [ ] Documentação da API atualizada

**Decisões (2026-11-22):** sem credenciais de teste do PayPay, ele é construído **a partir da documentação dele** (a tarefa a localiza em `docs/Integrações/`), com testes contra um servidor simulado do contrato (assinatura RSA, webhook). A validação no PayPay real só acontece quando houver credenciais. Manuais: o AppyPay mantém os textos atuais; o PayPay fica sem manual até existir um.

---

### Parte 7 — Serviços: catálogo fixo (Transporte e Atividade extracurricular)

**Estado:** Pendente · **Depende de:** 2 (inclusive o pacote `finance` na pasta nova)

**Substitui** a antiga Parte 7 ("mês de início da cobrança dos serviços extras"), que deixa de existir: o prazo agora é a lista de meses de cada serviço.

**Já existe:** serviço extra **criado e personalizado pela própria academia**: agregado `ServicoExtra` (`internal/domain/aggregates/servico_extra.go`: nome, preço, tipo de cobrança `unico`/`mensal`, `valor_taxa_inscricao`), cobrança em `finance/servico_extra.go` (a partir da data do vínculo, `vinculada_em`), handlers em `internal/handlers/servicosextras/` e o fluxo de solicitação, aprovação e vínculo (eventos 38 a 40 do Anexo A).

**Decisão (2026-11-21):** a academia **deixa de criar e personalizar serviços**. Nós criamos os serviços conforme a demanda e mantemos o controle de tudo: o catálogo fica no código. O módulo passa a se chamar **Serviços** (era "Serviços extras"). Por enquanto são dois: **Transporte** e **Atividade extracurricular**. A academia só configura e disponibiliza o que o catálogo oferece.

**Regras gerais**
- As rotas e telas em que a academia cria, edita e apaga serviço genérico são **removidas**, sem flag, sem código morto e sem legado (mesmo critério da Parte 1; não há academias). O nome visível passa a ser "Serviços"; se o pacote e as rotas de `servicosextras` serão renomeados, a tarefa decide e documenta (sugestão: manter o pacote e documentar as rotas novas).
- **Gratuito** pode ser definido em todo campo em que o pedido o prevê: preço geral, preço da rota, preço da atividade e prazo geral do Transporte. O que é gratuito não gera cobrança.
- **Prazo mensal:** a academia escolhe quais meses do ano civil (janeiro a dezembro) o serviço está disponível. **Esses meses são os cobrados** do estudante, um por vez, a preço × 1 por mês.
- **Adesão e taxa de inscrição permanecem** (confirmado por você): o fluxo atual (solicitação, aprovação, vínculo) continua, e a academia continua podendo cobrar uma **taxa de inscrição**, opcional (pode ser gratuita), cobrada uma vez na adesão aprovada; uma para o Transporte e uma por atividade (sugestão).
- **Adesão tardia:** cobra-se a partir do mês da adesão, só nos meses selecionados que faltam, sem meses retroativos (sugestão, herdada da antiga Parte 7).
- **Alterar preço ou meses** só afeta o que ainda não foi gerado: cobranças **não pagas** de meses removidos são canceladas; as **pagas** nunca são tocadas.
- **Validação de valor:** os campos de preço já nascem rejeitando mais de 2 casas e valor acima do teto do `NUMERIC(14,2)`, mesmo antes do validador único da Parte 8, que depois só consolida.
- Cada recurso declara o módulo `servicos` (Parte 9). Sem academias, não há migração.

**Transporte**
- **Preço geral** (pode ser gratuito) e **prazo geral** (meses; pode ser definido como gratuito, e então nenhum mês é cobrado, em todas as rotas): opcionais. Se definidos, valem para **todas** as rotas e **nenhuma rota pode definir** o campo (a API recusa).
- Só se define o geral quando nenhuma rota tem valor próprio naquele campo; senão, erro 409 pedindo para limpar as rotas (sugestão).
- **Rotas:** nome (ex.: "Rota 1", "Talatona - Kilamba"); preço da rota (pode ser gratuito; só se não houver preço geral); prazo em meses (só se não houver prazo geral); **veículos** da rota (nome, ex.: "Autocarro 1"); **horário de embarque** (`hh:mm`) de cada rota.
- **Vínculo do estudante:** a **uma só rota** e a **pelo menos um veículo** dessa rota.

**Atividade extracurricular**
- Campos: nome ("Dança", "Jiu Jitsu"...); preço (pode ser gratuito); prazo (meses do ano civil, cobrados); instrutor ou responsável (nome); **horários**: lista de horários, cada um com **hora de início e de fim** (`hh:mm` a `hh:mm`), para cada um dos 7 dias da semana; **coletiva** (true/false).
- **Individual** (`false`): só o estudante vinculado.
- **Coletiva** (`true`): o estudante passa a fazer parte de uma **turma da atividade** (nome e lista de membros). Não é a `Turma` acadêmica e não se cruza com o horário acadêmico (Parte 15) (sugestão).

**Entregas**
- [ ] Catálogo fixo no código (Transporte, Atividade extracurricular) e remoção das rotas e telas de criação genérica
- [ ] Transporte: preço e prazo gerais, rotas (com horário de embarque), veículos e vínculo (uma rota, pelo menos um veículo)
- [ ] Atividade: campos, horários (início e fim), individual e coletiva com turma da atividade
- [ ] Cobrança mensal nos meses selecionados e taxa de inscrição em `finance` (adesão tardia, alteração, cancelamento das não pagas)
- [ ] Testes: gratuito sem cobrança; geral bloqueia a rota; uma rota por estudante; meses; adesão tardia; valor com mais de 2 casas rejeitado
- [ ] Front-end: gestão do Transporte e da Atividade; remoção das telas de serviço genérico (nessa remoção, front-end primeiro)
- [ ] Documentação da API atualizada

---

### Parte 8 — Validação do módulo financeiro

**Estado:** Pendente · **Depende de:** 6, 7

**Já existe (a auditar, não refazer):** guarda de operação única (`unique_operation_guard.go`), idempotência em `aplicarEfeitosPagamento`, conferência ao vivo no provedor antes de confirmar (Tarefa 79), bloqueio de anular/reativar/cancelar cobrança paga (Tarefa 114), reconciliação (Tarefa 121), auditorias anteriores (Tarefas 40 a 46), convenção do valor monetário (Tarefa 26: `roundAmount`, rejeição de mais de 2 casas na cobrança, no QR Code e na mensalidade; coluna `NUMERIC(14,2)`).

**Objetivo:** provar com testes e corrigir o que faltar nas **quatro** garantias, valendo para os **dois provedores**, para estudante **e encarregado**, e para mensalidades **e serviços** (Transporte e Atividade extracurricular). Não muda regra de preço: só corrige desvio. A Garantia 4 endurece a validação de entrada: valor inválido passa a receber **erro 400** em vez de ser arredondado em silêncio.

**Garantia 1 — O estudante paga o que deve e a academia sabe:** só o provedor confirma; callback repetido não duplica; valor confirmado = valor cobrado; o "pago" aparece para academia, estudante e encarregado e não volta a pendente; pagamento sem dívida correspondente fica "não identificado".

**Garantia 2 — Valor certo:** o preço sai da configuração (tipo de estudante, características, ano/nível, curso), calculado só no servidor; fica **congelado na cobrança**; um teste por combinação de tipo/nível/serviço (preço geral ou da rota; atividade).

**Garantia 3 — Nunca cobrar o que já foi pago:** conferir "já pago" e "já pendente" antes de criar cobrança; restrição única por (estudante, item, período) no banco; criações simultâneas (duplo clique, dois aparelhos, estudante + encarregado) geram **uma** cobrança; pagar dívida paga é recusado.

**Garantia 4 — Dinheiro com o tipo e a validação certos** (verificado em `main` @ `ffc13e2`)
- **O que está certo:** o banco usa `NUMERIC(14,2)` (`valor` de mensalidade e de matrícula, `preco` e `valor_taxa_inscricao` de serviço extra). No Go, `float64` com `roundAmount` é decisão documentada (Tarefa 26) e as somas são arredondadas depois de acumular (`finance/mensalidade.go`, `finance/pagamento_externo.go`). Não há `==` direto em dinheiro em código de produção. **Manter `float64` no Go; não migrar o tipo.**
- **Falha 1:** `preco` e `valor_taxa_inscricao` do serviço extra (`validarCamposPagamentoServico` e `validarCamposTaxaInscricao`, em `internal/domain/aggregates/servico_extra.go`) só validam `> 0`. Aceitam 15,999 e o banco arredonda em silêncio, contra a regra da Tarefa 26. **Atualização (2026-11-21):** a Parte 7 substitui esse agregado (a taxa de inscrição permanece e continua validada). Os campos novos (preço geral e da rota do Transporte, preço da atividade) já nascem com 2 casas e teto na Parte 7; aqui o validador único passa a servir a todos.
- **Falha 2:** nenhuma validação de valor tem teto. Um valor a partir de 10¹² estoura o `NUMERIC(14,2)` na projeção, depois de o evento já estar no ledger. Vale também para o `valor` da mensalidade (`finance/mensalidade.go`).
- **Falha 3:** `amountsEqual` (`finance/servico.go`) só é usada em testes. Usá-la onde o valor esperado é comparado com o confirmado pelo provedor, ou removê-la.
- As falhas 1 e 2 foram identificadas por leitura do código; o teste da entrega abaixo deve reproduzi-las antes da correção.

**Entregas**
- [ ] Relatório do que já cumpre e do que foi corrigido
- [ ] Testes das quatro garantias (um que falha quando a regra é desfeita, por regra)
- [ ] Cobertura com AppyPay e PayPay, estudante e encarregado, mensalidade e serviço
- [ ] Validador único de valor monetário, usado por toda entrada de dinheiro: finito, maior que zero, no máximo 2 casas (rejeitar com erro claro, sem arredondar) e no máximo o limite do `NUMERIC(14,2)`
- [ ] Aplicar o validador no preço geral e no preço da rota do Transporte, no preço da atividade, na taxa de inscrição e em `valor` de mensalidade (e em qualquer validador de serviço antigo que sobreviva à Parte 7)
- [ ] Testes: 15,999 rejeitado; valor acima do teto rejeitado **sem gravar evento no ledger**; 19,99 e 0,10 aceitos
- [ ] Usar `amountsEqual` na comparação com o valor confirmado pelo provedor, ou removê-la
- [ ] Front-end: conferir se os formulários de preço validam 2 casas (a confirmar; front não reanalisado)
- [ ] Documentação da API: limites e mensagens de erro desses campos

---

# Fase 4 — Assinaturas e pagamento manual

### Parte 9 — Módulos e assinaturas: catálogo no código, preço por limite de estudantes, demonstração, bloqueio e ativação

**Estado:** Pendente · **Depende de:** 4

**Já existe:** nada de assinatura. Reaproveitar: papéis de admin (`RequireFPP`), o cadastro público de academia com **ativação por admin** (`AtivarAcademia`) e os **status por nível** do estudante (`status_escolar_fundamental`, `status_escolar_medio`, `status_superior`, com os valores `em_andamento`, `finalizado` e `inativo`) para contar estudantes ativos. O código por e-mail do 2FA (`dois_fatores_service`, Tarefa 122) é reaproveitado na Parte 11. Da Parte 4 vêm a **classificação de rotas** (`internal/permissoes/rotas.go`: livre, exige permissão, reservada, negada), com *negar por padrão* e um teste que percorre o router, e a **identidade efetiva** (`internal/middleware/identidade_efetiva.go`): o secretário age como a academia e o encarregado como o educando.

**Mudança de modelo (2026-11-21):** administradores **não criam** mais planos nem módulos. **Módulos, recursos e planos (prazos) são definidos no código**, num catálogo versionado. Os **preços** são dados que o administrador **FPP** pode alterar (ver "Preços editáveis pelo FPP"); os valores iniciais vêm do catálogo. Saem do plano: o CRUD de módulos e de planos e a tela de gestão de planos; fica só a edição de preços pelo FPP. **Gratuito** continua possível onde o pedido o descreve (preço geral, preço da rota e preço da atividade, e prazo geral do Transporte; Parte 7). O **único plano gratuito dos módulos** é a **demonstração** (ver abaixo).

**Catálogo** (1 mês = 30 dias, mês comercial)

| Módulo (`id`) | Recursos | Preço do módulo | Planos |
|---|---|---|---|
| Gestão Académica (`gestao_academica`) | Gestão Académica, Matrículas e Inscrições, Avaliação, Notas e Assiduidade, Sumários e tudo que envolve diretamente os processos acadêmicos (horários, grupos, material de apoio, refazer matrícula) | **30.000 Kz/mês** fixos se o limite contratado for de até **1.000 estudantes**; acima de 1.000, **30 Kz/mês por estudante** do limite (30 × limite) | Mensal (1 mês): preço × 1 · Semestral (6 meses): preço × 6 · Anual (12 meses): preço × 12 |
| Finanças (`financas`) | Receber pagamentos, configurar cobranças, gestão de propinas, credenciais dos provedores, pagamento manual etc. | **15 Kz/mês por estudante** do limite contratado (15 × limite) | Mensal × 1 · Semestral × 6 · Anual × 12 |
| Facturação (`facturacao`) | Todas as funcionalidades de facturação (Parte 12) | Valor fixo por plano | Mensal (1 mês): 5.000 Kz · Trimestral (3 meses): 10.000 Kz · Semestral (6 meses): 19.000 Kz · Anual (12 meses): 35.000 Kz |
| Comunicação (`comunicacao`) | Comunicação e Notificações (SMS manual, agendado e automático, configuração de notificações da academia; Partes 23 a 27) | Pacotes de SMS (não é por prazo) | Pacotes na tabela abaixo |
| Serviços (`servicos`) | Transporte e Atividade extracurricular (Parte 7) | **15 Kz/mês por estudante** do limite contratado (15 × limite) | Mensal × 1 · Semestral × 6 · Anual × 12 |

O módulo **Comunidade** e seus recursos ficam no `Plano de Atualizações 2.md`; ele entra neste catálogo, por código, quando aquele plano for executado.

**Pacotes do módulo Comunicação** (os planos do provedor de SMS; só definem quantas mensagens a academia pode enviar, Parte 23; valores iniciais)

| Pacote | Quantidade | Preço/SMS | Total do pacote |
|---|---|---|---|
| SMS Básico | 1.000 | 8,80 Kz | 8.800 Kz |
| SMS Start | 10.000 | 8,80 Kz | 88.000 Kz |
| SMS Pro | 30.000 | 7,98 Kz | 239.400 Kz |
| SMS Pro Max | 50.000 | 7,98 Kz | 399.000 Kz |
| SMS Ultra | 500.000 | 7,80 Kz | 3.900.000 Kz |

**Preços editáveis pelo FPP** (2026-11-22)
- Um administrador **FPP** pode alterar os **preços dos módulos**: os **fixos** (30.000 Kz da Gestão Académica até 1.000 estudantes; os quatro planos da Facturação) e os **por estudante** (30 Kz da Gestão Académica acima de 1.000; 15 Kz de Finanças e de Serviços). Nos pacotes de SMS, o preço por SMS (a quantidade de cada pacote é do código) (sugestão). Continuam no código: módulos, recursos, prazos dos planos e as regras (o corte de 1.000 estudantes, por exemplo).
- Os **valores iniciais** são os do catálogo acima, carregados no banco; depois disso, o banco é a fonte dos preços vigentes.
- **A alteração vale só para academia que não tem o módulo contratado.** Quem já contratou e pagou não é afetado: cada período guarda os **preços vigentes na contratação** (valor fixo e preço unitário). O **excedente** daquele período usa esses preços congelados, **nunca o preço novo**. Quando o período acaba e a academia contrata de novo, vale o preço então vigente.
- Cobrança já criada e ainda não paga mantém o preço da criação; se expirar, a nova nasce com o preço vigente (sugestão).
- **Renovação antecipada** (novo período comprado antes de o atual acabar): o período atual mantém o seu preço e o novo usa o preço vigente (confirmado por você; ponto 54).
- **Registro:** cada alteração cria uma **versão** do preço (valor anterior, valor novo, quem, quando e motivo), sem apagar as anteriores. Só o `fpp` altera, com o validador monetário da Parte 8 (2 casas e teto) e **confirmação por código por e-mail**, como na Parte 11 (mantida por você). A demonstração não tem preço.
- **Avisos de alteração de preço** (confirmado por você), enviados só depois que a alteração é confirmada pelo código e salva, um por alteração e por destinatário:
  - Os **outros administradores FPP** (todos, menos o que alterou) recebem um aviso informando que o administrador **[nome]** alterou o preço do plano de um módulo (módulo, plano, valor anterior e novo, data). Evento 42 do Anexo A.
  - As **academias que têm o módulo contratado** (período em curso) recebem um aviso sobre a atualização dos preços, **sem dizer quem alterou**; o texto deixa claro que o período em curso não muda e que o novo preço vale na próxima contratação (sugestão). Evento 43 do Anexo A. Academia sem o módulo contratado não recebe aviso (sugestão).
  - Por e-mail (e-mail verificado do administrador e da academia), pelo envio existente (`brevo_mailer`) até o núcleo de notificações (Parte 21) existir (sugestão).

**Preço por limite de estudantes** (novo modelo, 2026-11-22; vale para Gestão Académica, Finanças e Serviços)
- Ao contratar, a academia **define quantos estudantes tem** (o limite, N). **N é o máximo de estudantes que ela pode cadastrar durante o período do plano.**
- **Gestão Académica:** valor mensal = **30.000 Kz** fixos se N ≤ 1.000; se N > 1.000, **30 × N**. **Finanças e Serviços:** valor mensal = **15 × N**. Total do plano = valor mensal × meses do plano.
- O limite, o valor e os **preços unitários vigentes na contratação** ficam **congelados** no período contratado (e na cobrança).
- Cada módulo guarda o limite do seu período; o front sugere, ao contratar, o limite vigente da Gestão Académica (sugestão). O limite de cadastro, porém, é um só: o da Gestão Académica.

**Limite de cadastro e histórico (Gestão Académica)**
- O recurso **cadastro de estudantes** só deixa cadastrar enquanto os estudantes **ativos vinculados à academia** (ver "Termos usados") forem menos que o limite vigente. A checagem é **atômica**: dois cadastros ao mesmo tempo não passam do limite, e um lote que ultrapassaria é recusado inteiro, informando quantas vagas restam (sugestão).
- Ao atingir o limite, o cadastro é recusado com mensagem clara e um aviso (CTA) para comprar o excedente. Estudante finalizado, inativo ou desvinculado libera vaga (sugestão).
- **Mecanismo de histórico:** cada período da Gestão Académica guarda seu **limite** (o contratado mais os excedentes pagos). Assim o módulo sabe **quantos estudantes poderiam ser cadastrados em um contrato anterior**: o limite do contrato anterior vem como valor sugerido na renovação e fica visível à academia e ao FPP. Um limite novo não pode ser menor que o número de estudantes ativos já cadastrados (sugestão).

**Excedente: cadastrar mais estudantes com contrato em curso**
- A academia define o **excedente** (E: quantos estudantes a mais quer cadastrar) e paga por **cada módulo que cobra por estudante e tem período em curso**, **separadamente**: preço unitário **congelado no período contratado** daquele módulo (valores iniciais: 30 Kz na Gestão Académica; 15 Kz em Finanças e Serviços) × E × **meses que faltam** para o período daquele módulo acabar.
- **Meses que faltam** = dias restantes do período ÷ 30, arredondando para cima (mês comercial; sugestão).
- O cálculo é por módulo porque cada um tem seu valor, prazo e plano: nem sempre quando um esgotou o outro também esgotou. **Módulo cujo período já terminou não cobra excedente.**
- Como os módulos que cobram por estudante dependem do **cadastro de estudantes** da Gestão Académica, **acrescentar excedente na Gestão Académica obriga a pagar também os outros módulos com período em curso**. Isso mantém o funcionamento dos recursos que têm custo unitário por estudante (como SMS). Uma **única cobrança**, com uma linha por módulo; o limite só sobe depois de **todo o pagamento** confirmado (pelo provedor ou marcado como pago manualmente, Parte 11) (sugestão).
- Depois de pago, E soma ao limite da Gestão Académica e ao limite de cada módulo cobrado, até o fim do período de cada um.
- Com a Gestão Académica sem período em curso não há excedente: é preciso contratar um plano novo (Parte 10).

**Exemplos**

| Caso | Contratação inicial | Excedente |
|---|---|---|
| Gestão Académica e Finanças, anuais, em janeiro, N = 500 | Gestão: 30.000 × 12 = **360.000 Kz**; Finanças: 15 × 500 × 12 = **90.000 Kz** | Em junho, E = 100: Gestão 30 × 100 × 6 = **18.000 Kz**; Finanças 15 × 100 × 6 = **9.000 Kz** |
| Gestão Académica anual e Finanças mensal, em janeiro, N = 1.200 | Gestão: 30 × 1.200 × 12 = **432.000 Kz**; Finanças: 15 × 1.200 × 1 = **18.000 Kz** | Em fevereiro, E = 100: Gestão 30 × 100 × 11 = **33.000 Kz**; Finanças **não paga**, o plano já terminou |

Nos exemplos, os meses que faltam (6 e 11) são os dados de cada cenário; no sistema vale a regra dos dias restantes ÷ 30, arredondando para cima. O excedente é sempre cobrado pelo preço unitário, mesmo com o limite abaixo de 1.000, como nos exemplos; por isso definir 1.000 de início sai mais barato que 500 mais excedente (o front pode avisar disso; sugestão).

**Plano de demonstração gratuita**
- Plano `demonstracao`: **15 dias** (dias corridos, não mês comercial), **sem preço**, para os módulos **Gestão Académica e Finanças**.
- **Só configuração**: a academia não terá nenhum estudante cadastrado (limite 0). Fica **de fora o recurso de cadastro de estudantes** (individual e em lote), porque ele emite SMS sistemáticos, como as credenciais, por conta do Spuri, e nada entrou no caixa para pagá-los. Em Finanças, só a configuração (provedores e cobranças).
- Regra geral: **nenhum recurso que emita SMS por conta do Spuri** entra na demonstração. Além do cadastro de estudantes, fica de fora o **cadastro de encarregado** e o reset das credenciais dele (tratamento semelhante ao do estudante). **Secretário e professor** podem ser criados e ter as credenciais resetadas, e as credenciais vão por **e-mail** em vez de SMS (confirmado por você). Fora da demonstração, voltam a ir por SMS. É uma exceção à regra de nunca enviar senha por e-mail (Partes 4 e 28 passam a consultar se a academia está em demonstração).
- Quem ativa: a própria academia, **uma única vez**, depois de aprovada (sugestão). Origem `demonstracao`; sem cobrança e sem recibo.
- Fim automático em 15 dias; depois os módulos ficam inativos (a consulta continua). Não renova: para continuar, contrata plano pago (Parte 10). Contratar um plano pago durante a demonstração substitui a demonstração daquele módulo (o período pago começa na confirmação do pagamento) (sugestão).
- No catálogo, cada plano lista os **recursos excluídos**. As rotas declaram o **módulo e o recurso** (ex.: `cadastro_estudantes`) e o middleware recusa a ação de recurso excluído do plano ativo, com um código estável (`recurso_indisponivel`) e o id do módulo, para o front mostrar o aviso de contratação.

**Assinatura da academia (por módulo)**
- **Módulo ativado/desativado para a academia** indica se ela pode usar os recursos do módulo. Estado **calculado pelas datas**: fim = início + meses × 30 dias. Sem botão manual de "inativar"; cancelar manualmente só pelo FPP, com a validação por código da Parte 11 (sugestão).
- **Comunicação** é exceção: não tem prazo; o módulo está **ativo enquanto houver cota de SMS**; esgotada, ele desativa (Parte 23).
- Um período ativo por módulo; renovar com período ativo **soma** o prazo ao fim atual (sugestão).
- Cada período guarda a **origem** (`plataforma`: pago pelo provedor; `manual`: pago por fora, marcado pelo FPP; `demonstracao`: demonstração gratuita, sem cobrança) e a cobrança que o gerou.
- **Academia nova nasce sem assinatura.** Quando o cadastro é aprovado, ela mesma paga os módulos escolhidos ou ativa a demonstração gratuita; o administrador só entra para definir que foi pago por fora (Parte 11). Sem academias hoje, **não há migração**.
- **Todo módulo é independente:** cada recurso depende só do próprio módulo, e nenhum módulo exige outro ativo.

**Ativação e desativação automáticas**
- **Ativar:** só depois de **pagamento confirmado**, seja pelo provedor (Parte 10) seja marcado como pago manualmente por um administrador FPP (Parte 11). As duas entradas chamam a **mesma função de ativação, idempotente**: repetir não soma o prazo duas vezes.
- **Desativar:** automático no fim do período, por data, sem ação de ninguém. Avisos de vencimento ficam para o catálogo de notificações (a definir).

**Bloqueio (back-end, em middleware)**
- O bloqueio é feito **no back-end, na camada de middleware**, antes de o handler rodar; o front só reflete (regra 1). Um middleware `RequireModulo(<id>)` é aplicado por grupo de rotas (ou por rota) e confere se o módulo está ativo para a academia do usuário (cache curto, invalidado quando o módulo é ativado ou desativado; sugestão). Roda **depois da identidade efetiva**, para consultar o módulo da academia certa quando quem age é o secretário ou o encarregado.
- **Módulo inativo: a consulta continua e a ação é bloqueada.** Leitura (GET e HEAD) passa. Criar, editar, remover, pagar, enviar e qualquer ação que mude dado ou mexa em dinheiro (POST, PUT, PATCH, DELETE, inclusive as rotas `/async`) é recusada. Rota POST que é só consulta (ex.: uma busca) se declara como consulta, explicitamente (sugestão).
- Cada rota **declara o seu módulo** e se é consulta ou ação. Um **teste de cobertura**, no mesmo padrão do teste de `internal/permissoes` (negar por padrão), percorre as rotas registradas e falha se alguma não declarar módulo ou isenção (regra 11). Isentas: autenticação, "Minha assinatura / Módulos", pagamento de módulos, webhooks e consulta de pagamento, rotas de admin e rotas do sistema.
- Vale para **todos os perfis** da academia (secretário, professor, estudante, encarregado), pelo módulo da academia onde atuam. Na Gestão Académica inativa, todos só consultam; estudantes e encarregados não pagam mensalidades.
- Ação recusada responde com o texto genérico **"Serviço indisponível no momento"** (regra 10), mas traz um **código estável (`modulo_inativo`) e o id do módulo**, para o front montar o aviso de contratação (sugestão).
- A academia **sempre** entra, vê "Minha assinatura / Módulos" (mostra o estado real, porque é onde ela renova; a confirmar) e **paga módulos**. Confirmações de pagamentos já iniciados continuam sendo registradas.
- SMS e e-mail do próprio sistema (credenciais, segurança) não dependem de módulo.
- O back-end expõe a **lista de módulos ativos** da academia do usuário, para o front.

**Navegação (front-end)**
- O front **permite a navegação** por todas as rotas, inclusive as de módulos inativos, e **bloqueia a ação**: botões e formulários de ação ficam desativados (o middleware continua sendo a garantia).
- Em toda tela de um módulo inativo aparece **sempre** um **aviso (CTA)** informando que o módulo precisa ser contratado, com um **botão que leva à página de assinatura daquele módulo** (planos, preços e contratação, Parte 10).
- O botão aparece para quem contrata, a **academia**. Para secretário, professor, estudante e encarregado, que não contratam, o aviso é o genérico "Serviço indisponível no momento", sem botão (sugestão; regra 10).
- A barra lateral mostra os módulos conforme o perfil e as permissões, já que a navegação é permitida; marcar visualmente os inativos é sugestão.
- Quem decide continua sendo o back-end; o front só esconde, mostra e avisa.

**Entregas**
- [ ] Catálogo de módulos, recursos e planos no código e tabela de preços (valores iniciais do catálogo), com um teste por preço
- [ ] Tela e API do FPP para alterar preços: versões auditadas, validador da Parte 8 e código por e-mail; o período contratado guarda os preços vigentes na contratação
- [ ] Avisos de alteração de preço: aos outros FPP (com o nome de quem alterou) e às academias com o módulo contratado (sem o nome)
- [ ] Cálculo de preço por limite (Gestão: 30.000 fixos se N ≤ 1.000, senão 30 × N; Finanças e Serviços: 15 × N) e limite congelado no período
- [ ] Limite de cadastro de estudantes (atômico, individual e em lote) e histórico do limite por período da Gestão Académica
- [ ] Cálculo do excedente por módulo, com os meses que faltam de cada período, e testes com os dois exemplos
- [ ] Plano de demonstração (15 dias, Gestão Académica e Finanças, sem cadastro de estudantes nem de encarregado) e exclusão de recursos por plano no middleware
- [ ] Credenciais de secretário e professor por e-mail, só enquanto a academia estiver em demonstração
- [ ] Assinatura por módulo (períodos, estado por datas, origem, renovação soma prazo) e função de ativação idempotente
- [ ] Middleware `RequireModulo` em **todas** as rotas existentes (consulta passa, ação é bloqueada), com teste de cobertura das rotas
- [ ] Endpoint de módulos ativos da academia
- [ ] Front-end: navegação livre com ação bloqueada e aviso (CTA) com botão para a página de assinatura do módulo; tela "Minha assinatura / Módulos" e página de assinatura de cada módulo
- [ ] Documentação da API atualizada

---

### Parte 10 — Contratação paga de módulos e de excedente de estudantes

**Estado:** Pendente · **Depende de:** 8, 9

**Regras**
- A academia escolhe **módulo + plano** (ou um pacote de SMS, Parte 23). Nos módulos que cobram por estudante, **define também o limite de estudantes** (Parte 9). A cobrança sai no **provedor ativo do Spuri** (Parte 6), com o preço **calculado naquele momento**: preço vigente × limite × meses. O período contratado **congela** esses preços (Parte 9).
- O módulo só ativa **depois do pagamento confirmado pelo provedor** (regras da Parte 8), pela função idempotente da Parte 9.
- Pagamento confirmado depois de a cobrança expirar: ativa mesmo assim, o dinheiro entrou.
- Renovação soma ao fim atual; o limite novo vale para o período novo. Mudar o preço no catálogo depois não afeta cobrança já criada.
- **Excedente de estudantes** (Parte 9): a academia define quantos a mais quer; o sistema calcula, por módulo com período em curso, preço unitário × excedente × meses que faltam, numa **única cobrança**. O limite só sobe depois do pagamento. Paga pelo mesmo caminho.
- A **demonstração gratuita** (Parte 9) se ativa sem cobrança e sem passar por aqui.
- Uma cobrança pendente por (academia, módulo) de cada vez, para não duplicar (sugestão).
- Pagamento de módulo (Academia → Spuri) nunca se mistura com pagamentos de estudantes (Estudante → Academia).
- Origem do período: `plataforma`. Se o pagamento for feito por fora, vale a Parte 11.

**Entregas**
- [ ] Cobrança do módulo, com o limite informado, no provedor do Spuri e ativação após confirmação
- [ ] Cobrança única de excedente (uma linha por módulo) e aumento do limite só após o pagamento
- [ ] Renovação e idempotência
- [ ] Front-end: informar o limite, contratar, comprar excedente e acompanhar pagamento na página de assinatura de cada módulo (destino do aviso CTA da Parte 9)
- [ ] Documentação da API atualizada

---

### Parte 11 — Pagamento manual (fora do sistema)

**Estado:** Pendente · **Depende de:** 4, 8, 10

**Já existe:** o **pagamento externo** das cobranças do estudante: a academia marca pendências como pagas fora da plataforma (`finance/pagamento_externo.go`, método `EXTERNO`, com observação e referência; handlers em `financeiro_pagamento_externo_handlers.go`; evento 31 do Anexo A). Não existe nada do lado do Spuri. A tarefa começa lendo o que o mecanismo atual cobre (mensalidades, serviços) e o **generaliza**.

**Regra:** **toda cobrança** pode ser marcada como **paga manualmente**, nas duas direções.

| Cobrança | Quem marca | Validação |
|---|---|---|
| Estudante/encarregado → academia (mensalidade, serviços) | Academia e secretário, só da própria academia | Motivo obrigatório e auditoria |
| Academia → Spuri (módulos, excedente de estudantes, pacotes de SMS) | Só administrador `fpp` | Motivo obrigatório, auditoria e **código por e-mail** |

**Efeito: a mesma sequência de um pagamento por provedor.** A marcação chama o **mesmo gancho idempotente** (`aplicarEfeitosPagamento`, hoje usado pelo webhook, pela consulta manual e pela reconciliação), com origem `manual`. Resultado: a cobrança fica "paga" e não volta a pendente; a dívida é abatida; o **módulo é ativado** (Parte 9) ou a **cota de SMS é atualizada** (Parte 23); o **recibo** é emitido (Parte 12); as notificações financeiras saem (Parte 25).

**Dados da marcação:** motivo (obrigatório); forma do pagamento (dinheiro, depósito, transferência, outro) e referência ou número do comprovativo (opcionais, como o pagamento externo de hoje); data do pagamento (não futura, fuso de Luanda); valor igual ao cobrado, ou seja, **pagamento total** (parcial fora do escopo) (sugestão).

**Atribuição manual de módulo (negociação fora do sistema)** — substitui a antiga "atribuição manual sem cobrança nem recibo". No caminho normal a academia já gerou a cobrança (Parte 10) e o administrador **só a marca como paga por fora**. Os passos abaixo valem quando a academia pagou por fora **sem ter gerado a cobrança**: o administrador a gera em nome dela e marca:
1. O administrador FPP escolhe academia, módulo e plano (ou pacote de SMS), data de início e escreve o motivo.
2. O sistema **calcula e cria a cobrança** (catálogo, plano e o limite de estudantes informados pelo administrador, onde o módulo cobra por estudante), sem emitir no provedor.
3. Envia um código ao **e-mail do próprio administrador**: válido por 10 min, uso único, até 5 tentativas, vale só para essa operação (valores sugeridos).
4. Com o código válido, a cobrança é marcada como paga manualmente: o módulo ativa com origem `manual` e a auditoria é gravada.
- Módulo concedido **sem cobrança** (cortesia) fica fora do escopo (a confirmar).

**Regras de segurança**
- Só cobrança em aberto (pendente, expirada ou que falhou); cobrança já paga ou anulada é recusada.
- Se houver cobrança **pendente no provedor**, ela é cancelada quando o provedor permitir. Se o provedor confirmar depois, vale a regra de duplicidade da Parte 6 (b): "pago em duplicidade", com aviso, **sem abater outra dívida**.
- Repetir a marcação não duplica efeitos (regra 8).
- **Não há "desfazer"** (regra 7: nada é apagado). Correção de marcação errada é ajuste de administração e fica fora deste plano; devolução automática também.
- Auditoria: quem, quando, motivo, IP. Para as cobranças do estudante, a marcação é um recurso do módulo Finanças (`financas`); a do Spuri é de administrador e não depende de módulo.

**Quem pode o quê**

| Ação | Academia | Secretário | Professor | Estudante | Encarregado | Admin |
|---|---|---|---|---|---|---|
| Marcar cobrança de estudante como paga manualmente | ✔ (suas) | ✔ (suas) | ✘ | ✘ | ✘ | ✘ |
| Marcar cobrança do Spuri (academia) como paga manualmente | ✘ | ✘ | ✘ | ✘ | ✘ | ✔ (só FPP, com código) |

**Entregas**
- [ ] Generalizar o pagamento externo para toda cobrança de estudante (mensalidade e serviços), com motivo e auditoria
- [ ] Marcação manual das cobranças do Spuri (módulos, excedente de estudantes, SMS), só FPP, com código por e-mail
- [ ] Atribuição manual de módulo (cria a cobrança e marca como paga)
- [ ] Mesmo gancho `aplicarEfeitosPagamento` com origem `manual`; testes de que os efeitos são idênticos aos do provedor (ativação do módulo, créditos, recibo)
- [ ] Regras de duplicidade e idempotência
- [ ] Front-end: botão "Marcar como paga" (academia e secretário) e tela do FPP
- [ ] Documentação da API atualizada

---

# Fase 5 — Facturação

### Parte 12 — Facturação com Ulemo

**Estado:** Pendente · **Depende de:** 8, 9, 10, 11

**Posição (2026-11-21):** subiu para logo depois das Partes 8 a 11, de que depende (estava no fim do plano). A Parte 11 entra porque o recibo também vale para pagamento manual.

**Já existe:** só a documentação (`docs/Integrações/Ulemo IBA API - Documentação.md`). A Tarefa 119 definiu: **Ulemo é provedor de faturas e recibos, não de pagamento**. Sem código de facturação. Para a cópia de segurança já existem a camada de armazenamento (`internal/storage`, Mega ou local; ver `docs/Configuracao_Storage_Mega.md`) e as credenciais `MEGA_*` da Parte 3.

**Regras**
- Documentos: **factura, recibo e nota de crédito**, emitidos pelo Ulemo; o sistema guarda número, data, estado e o arquivo (PDF).
- **Quem emite:** o Spuri (para academias, nos pagamentos de módulo e de pacote de SMS) e cada academia (para estudantes/encarregados). Cada um com **suas credenciais Ulemo**, por contexto, no cofre existente (mesmo padrão das credenciais de pagamento).
- Configuração na página **`/facturacao`**, que também lista os documentos emitidos.
- **Recibo:** emitido **automaticamente** quando o pagamento está **comprovadamente concluído** (confirmado pelo provedor, regras da Parte 8, ou marcado como pago manualmente, Parte 11). Um por pagamento, nunca em dobro. Se o Ulemo estiver indisponível, o pagamento continua pago e o recibo fica "pendente de emissão" com nova tentativa automática; **o pagamento nunca falha por causa do recibo.**
- **Visualizar e baixar** o recibo na tela de visualização de cada pagamento (estudante, encarregado, academia, secretário).
- **Envio do recibo por e-mail:** depois de emitido, o recibo é **enviado ao e-mail verificado** do pagador. O destinatário vem da regra única de contato (Parte 5): estudante do nível escolar → e-mail verificado do **encarregado**; academia pagando o Spuri → e-mail verificado da academia. Sem e-mail verificado, **não envia** (fica registrado; nunca cai num e-mail não verificado). Um envio por recibo (chave de idempotência: recibo + destinatário). Falha no e-mail não afeta o recibo; há nova tentativa. É o e-mail do **evento 30** do Anexo A: fica na Facturação e não se duplica na Parte 25. Usa o envio de e-mail existente (`brevo_mailer`) até o núcleo de notificações (Parte 21) existir.
- **Cópia de segurança no Mega:** o Ulemo já permite baixar os documentos emitidos; por segurança, **em segundo plano**, cada documento emitido (PDF) também é guardado no **Mega**, pela camada `internal/storage` e com as credenciais `MEGA_*` da Parte 3. Caminho montado só por identificadores internos: `/Facturacao/{identificador do emissor}/{ano}/{tipo do documento}/{identificador do documento}.pdf` (sugestão). Tarefa em segundo plano com nova tentativa, **uma cópia por documento** (idempotente), com hash e estado (pendente, guardado, falhou). **Nunca atrasa nem derruba a emissão.** Sem credenciais do Mega, a cópia fica "pendente" e os administradores são alertados (sugestão). Acesso só pela API autenticada, sem link público.
- **Factura:** emitida pelo emissor a partir de uma cobrança, por ação dele (sugestão).
- **Nota de crédito:** só sobre documento emitido, referencia o original, exige motivo, e a soma das notas nunca passa do valor original.
- Documento emitido **nunca é editado nem apagado**; correção = nota de crédito + novo documento.
- Cliente sem NIF → "consumidor final" (sugestão). Estudante do nível escolar: documento **em nome do encarregado**, com o estudante na descrição (a confirmar).
- Com o módulo Facturação inativo a academia não emite (consultar e baixar o que já foi emitido continua); pagamentos seguem registrados e **não** se emitem recibos retroativos ao reativar (a confirmar). A emissão do Spuri não depende de módulo.

**Entregas**
- [ ] Credenciais Ulemo por contexto no cofre
- [ ] Recibo automático após pagamento confirmado, com reemissão segura
- [ ] Factura e nota de crédito
- [ ] Página `/facturacao` e visualização/baixa no pagamento
- [ ] Recibo também para pagamento marcado como manual (Parte 11)
- [ ] Envio do recibo por e-mail ao e-mail verificado, idempotente
- [ ] Cópia de segurança dos documentos no Mega, em segundo plano (tarefa com repetição, hash e estado)
- [ ] Documentação da API atualizada

**Decisões (2026-11-22):** o Ulemo **não tem ambiente de testes**: a construção usa um **cliente simulado do contrato** documentado, e toda emissão real em produção é fiscal (a primeira deve ser acompanhada). **Regras fiscais** (NIF, séries, IVA ou isenção por tipo de serviço): a tarefa lê a documentação do Ulemo para ver o que ele já resolve (por exemplo, séries e numeração) e devolve a lista do que sobrar para você decidir.

---

# Fase 6 — Gestão acadêmica

### Parte 13 — Notas e faltas só por professores + recálculo da avaliação final

**Estado:** Pendente · **Depende de:** 4

**Já existe**
- Hoje **só a academia** lança e corrige (`/academia/notas-aluno`, `/faltas-aluno`, `PATCH` e rotas `/async`). O **secretário já está impedido** de lançar (rota `Negada` em `internal/permissoes/rotas.go`, Tarefa 130). O professor já tem **rotas próprias de consulta** (`/professor/*`), filtradas pelos vínculos turma + matéria, mas ainda **não lança** nada.
- [x] Configuração de limite de faltas e **reprovação por faltas** por academia (Tarefa 117).
- **A regra de recálculo NÃO está em vigor** (você pediu para verificar). Corrigir uma nota só dispara a avaliação final se **ainda não existir** avaliação gravada; registrar ou corrigir **faltas não dispara nada**; depois de gravada, a avaliação **nunca é recalculada** (documento da Tarefa 117 diz isso expressamente).
- [x] **Zerar por faltas só lê como 0; não altera a nota registrada** (verificado em `main` @ `ffc13e2`). O zero é aplicado num mapa em memória (`AplicarZeroPorFaltas`, em `internal/handlers/apoio/avaliacoes.go`, chamado por `CalcularResultadoMateriasAvaliacaoFinal`), montado a cada cálculo por um `SELECT` em `projection_notas`. A soma de faltas é só leitura. Só `Estudante.RegistrarNota` e `Estudante.CorrigirNota` escrevem em `projection_notas`, e só por ação explícita. O zero fica registrado no resultado (`notas_zeradas_por_faltas`), nunca na nota. **Falta um teste que prove isso:** hoje nenhum teste afirma que a nota registrada continua intacta.
- **Código morto da avaliação final manual:** `RegistrarAvaliacaoFinal`, `RegistrarAvaliacaoFinalBatch`, `RegistrarAvaliacaoFinalBatchAsync` e o tipo de job `registrar_avaliacao_final_batch` não têm rota nem worker (já apontado no item 3.1 do `Manual avaliacao final ensino superior.md`). O `RegistrarAvaliacaoFinal` calcula a nota sem aplicar a regra de faltas. **Decisão: eliminar, sem suporte a legado.**

**Registro de notas e faltas**
- Lançar, corrigir e remover: **só o professor vinculado** à turma+disciplina. Academia e secretário apenas consultam. Os caminhos de escrita da academia (inclusive `/async`) são removidos ou fechados.
- Cada nota/falta guarda **quem lançou**. Se o professor sai, a academia/secretário reatribui o vínculo e o novo professor passa a poder corrigir.

**Recálculo da avaliação final (a implementar)**
- Nota lançada ou corrigida → o resultado final é **recalculado**.
- Falta adicionada, alterada ou removida → recalcula **somente se a academia ativou a reprovação por faltas**.
- Só recalcula **enquanto o ano letivo do registro está em andamento** para a academia (`FinalizarAnoLetivoAcademia` ainda não aconteceu). Depois, o resultado fica fechado.
- Como o ledger é imutável, o recálculo grava um **novo evento** com valor anterior, novo valor e motivo. Dois professores ao mesmo tempo: o resultado reflete sempre os dados atuais.
- O recálculo **nunca grava nota**. A nota zerada por faltas continua sendo só leitura em memória: o evento de recálculo guarda o resultado novo e o motivo, e as notas registradas ficam como o professor lançou. Se as faltas deixarem de ultrapassar o limite, o recálculo volta a usar a nota original.

**Entregas**
- [ ] Rotas de professor para lançar/corrigir/remover notas e faltas (inclusive em lote)
- [ ] Escrita da academia/secretário nessas rotas removida
- [ ] Autor registrado em cada nota/falta
- [ ] Recálculo ao corrigir nota e ao mudar falta, dentro do ano letivo, com evento de histórico
- [ ] Testes: dentro e fora do período; falta com e sem reprovação por faltas
- [ ] Teste de integração: com o limite de faltas excedido, o cálculo usa 0, mas `projection_notas` (nota, version, event_id, valor_anterior) e a contagem de eventos `NotasRegistradas`/`NotaCorrigida` no `spuri_ledger` ficam idênticos antes e depois; ao subir o limite, o cálculo volta a usar a nota original. Provar o teste desfazendo a regra (fazer o cálculo gravar 0 e ver o teste cair)
- [ ] Mesmo teste depois do recálculo (faltas adicionadas, alteradas e removidas)
- [ ] Eliminar o código morto da avaliação final manual, sem substituto (sem rota 410, sem stub, sem alias): `RegistrarAvaliacaoFinal` e `validarNotasParaAprovacao` (`internal/handlers/avaliacoes/avaliacao_final_handler.go`); `RegistrarAvaliacaoFinalBatch` (`batch_handlers.go`, arquivo inteiro); `RegistrarAvaliacaoFinalBatchAsync` (`async_batch_handlers.go`, arquivo inteiro); `JobTypeRegistrarAvaliacaoFinalBatch` (`internal/jobs/job.go`); `getRegraAvaliacaoFinal` e `carregarNotasFormula` (`avaliacao_final_regras.go`). **Manter** o resto de `avaliacao_final_handler.go` (`ListarAvaliacoes`, `GetAvaliacoesFinaisEstudante`, `ListarAprovacoes`, `ListarReprovacoes` e os filtros), o método `Estudante.RegistrarAvaliacaoFinal` do agregado (o cálculo automático o usa) e os eventos antigos do ledger
- [ ] Prova de que nada mudou para quem usa a API depois da eliminação: lista de rotas registrada idêntica antes e depois; `go build`, `go vet` e `go test ./...` limpos
- [ ] Atualizar o `Manual avaliacao final ensino superior.md` (nota de auditoria no início, achado do código morto e item 3.1): marcar como removido
- [ ] Front-end: telas do professor; academia só consulta
- [ ] Documentação da API atualizada

---

### Parte 14 — Avaliação final do ensino superior fixa no código

**Estado:** Pendente · **Depende de:** 2, 11

**Já existe:** o modelo do **escolar já é fixo** (`regraAvaliacaoFinalEscolarFixa`). O **superior é configurável** pela academia (`POST/PUT/DELETE /academia/avaliacao-final/regras` com fórmula, e `/academia/categorias-nota`); há um manual em `docs/`. O código morto do registro manual da avaliação final (`RegistrarAvaliacaoFinal` e afins) **sai na Parte 13**; aqui só sobram as rotas e a lógica de regras configuráveis (`avaliacao_final_regras.go`, incluindo `getRegraAvaliacaoFinalPorID`).

**Regras**
- A estrutura atual vira **só a base**: o contrato do que um mecanismo recebe e devolve. Deixa de ser configurável.
- Cada academia de ensino superior tem **seu mecanismo no código**, ligado à academia por identificador estável. Removem-se as rotas e telas de configuração do superior.
- Academia superior **sem mecanismo**: o recurso fica **desabilitado** e a avaliação final **não é calculada** (nunca um padrão silencioso). Enquanto uma academia de ensino superior estiver com o recurso desabilitado: (a) os **administradores** recebem **aviso** (alerta que persiste, não só uma vez, e a lista de academias superiores sem mecanismo fica visível a eles); (b) a **própria academia** é avisada de que o recurso está desabilitado, com texto genérico e **sem detalhes da tecnologia do Spuri** (ex.: "Avaliação final indisponível no momento"; regra 10).
- Como não há academias, **nada a reproduzir hoje**. Academia superior nova só tem avaliação final depois que o mecanismo dela for implementado e publicado (consequência de fixar tudo no código).
- O nível escolar não muda.

**Entregas**
- [ ] Contrato do mecanismo e registro por academia
- [ ] Rotas de configuração do superior removidas
- [ ] Aviso aos administradores e à academia (texto genérico) enquanto o recurso estiver desabilitado
- [ ] Front-end: telas de configuração removidas
- [ ] Documentação da API atualizada

**Como entra cada academia:** sempre que uma instituição de ensino superior for integrada, **você entrega as regras de avaliação dela** e isso habilita o recurso (cada mecanismo é uma tarefa pequena). Não há entrada pendente agora.

---

### Parte 15 — Horários das turmas

**Estado:** Pendente · **Depende de:** 4

**Já existe:** nada (os "sumários de aula" da Tarefa 75 são outra coisa).

**Regras**
- O horário é uma lista de aulas: turma, disciplina, professor, dia da semana, hora de início e fim (sala opcional).
- O professor vem do vínculo turma+disciplina (Parte 4); não se agenda aula sem professor vinculado.
- **Conflito bloqueia salvar:** mesma turma com duas aulas ao mesmo tempo; mesmo professor em duas turmas ao mesmo tempo; mesma sala, se usada.
- Pertence ao período letivo da turma. Se o professor do vínculo muda, as aulas passam ao novo e os conflitos são revalidados.
- **Gerencia:** academia e secretário. **Vê:** professor (o seu), estudante e encarregado (o da turma), academia e secretário (todos). Módulo Gestão Académica (`gestao_academica`).

**Entregas**
- [ ] Modelo, regras de conflito e rotas
- [ ] Visões por perfil
- [ ] Front-end: montagem e consulta do horário
- [ ] Documentação da API atualizada

---

### Parte 16 — Grupos do 4º ano de cursos técnicos

**Estado:** Parcial · **Depende de:** 4

**Já existe (Tarefa 116):** no `4_ano_medio` não há turmas, há **grupos**. O agregado `Turma` continua sendo a fonte (`tipo_agrupamento` derivado do nível) e já tem **`tema_trabalho`** (opcional, até 200 caracteres). Vínculo de estudantes, histórico por ano letivo, faltas e notas já dependem de `Turma`; **não criar agregado paralelo**.

**O que falta**
- Dados: **mentor** (professor ativo da mesma academia), descrição, estado (em formação, em andamento, concluído) e data de defesa (sugestão). Um mentor pode orientar vários grupos (limite a definir na tarefa).
- Um estudante em **um só grupo por ano letivo**, matriculado no 4º ano técnico do mesmo curso. Grupo sem integrantes ou sem mentor não vai para "em andamento".
- Mudanças de mentor, tema e integrantes ficam em histórico; "concluído" é só leitura.
- **Cria/edita:** academia e secretário. **Vê:** o mentor (seus grupos), os integrantes e seus encarregados.
- Se o estudante deixar o 4º ano técnico (ex.: refazer matrícula, Parte 18), sai do grupo.

**Entregas**
- [x] Grupo = turma do 4º ano médio, com tema (Tarefa 116)
- [ ] Mentor, descrição, estado e histórico
- [ ] Regra de um grupo por estudante/ano e validação do curso
- [ ] Visões por perfil
- [ ] Front-end e Documentação da API

---

### Parte 17 — Material de apoio

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
- Download sempre pela API com verificação de acesso; sem link público permanente. Com o módulo Gestão Académica inativo: só consultar e baixar (Parte 9).

**Entregas**
- [ ] Upload com validação de tipo e conteúdo
- [ ] Estrutura de pastas por nível e controle de acesso
- [ ] Download autenticado
- [ ] Front-end e Documentação da API

---

### Parte 18 — Refazer matrícula

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

### Parte 19 — Sumários sequenciais

**Estado:** Pendente · **Depende de:** 4

**Já existe (Tarefa 75; `internal/handlers/sumarios`, agregado `Sumario`):** o sumário pertence a uma academia e tem título, descrição, período, ano acadêmico, nível, curso (opcional) e **matéria**. **Não tem número de ordem**; os eventos são criado, dados atualizados e deletado. A tarefa confere como a listagem ordena hoje.

**Regras**
- Os sumários de uma **disciplina** (matéria) passam a poder ser **sequenciais**: cada um tem um **número de ordem** (1, 2, 3...) que define a ordem em que a matéria é dada. A sequência vale por (academia, matéria, curso quando houver, nível, ano acadêmico, período), a mesma chave que o sumário já usa (interpretação adotada).
- A sequência é **opcional** por disciplina; sem ela, o comportamento atual continua (a confirmar).
- Quem define e reordena: **só academia e secretário** (o secretário, com a permissão `sumarios.gerir` do catálogo da Parte 4, que a academia concede). **O professor não gerencia sumários**: só os visualiza, na ordem.
- Numeração **sem lacunas e sem repetição**. O sumário novo nasce no fim (próximo número) ou numa posição escolhida, empurrando os seguintes.
- **Reordenar** = mover um sumário para outra posição, renumerando os afetados **numa só operação**. Duas pessoas ao mesmo tempo nunca geram número repetido (guarda de versão).
- Remover um sumário faz os seguintes subirem uma posição (a confirmar). O removido continua no histórico (regra 7).
- O ledger é imutável: cada criação com posição, reordenação e remoção grava um evento com a ordem anterior, a nova e o autor. O número de ordem é **projeção**: o `rebuild` reproduz o mesmo resultado.
- Estudantes, encarregados e professores veem os sumários **na ordem**. Módulo Gestão Académica (`gestao_academica`).

**Entregas**
- [ ] Número de ordem no agregado e na projeção, com unicidade por escopo (migration que roda do zero)
- [ ] Criar com posição, reordenar e remover renumerando, com evento de histórico
- [ ] Listagens ordenadas pela sequência
- [ ] Testes: sem lacuna e sem repetição, concorrência, `rebuild`
- [ ] Front-end: ordenar (arrastar) e exibir a sequência
- [ ] Documentação da API atualizada

---

### Parte 20 — Documento de identificação alternativo: passaporte

**Estado:** Pendente · **Depende de:** 4, 5

Fica **fora das Partes 4 e 5 de propósito**: elas estão em implementação em outro chat; esta só começa depois de ambas.

**Já existe:** o cadastro do estudante guarda o **BI do estudante** (`bilhete_identidade`) e o **BI do encarregado** (`bilhete_identidade_encarregado`), cada um com o arquivo do documento (`bi_estudante`, `bi_encarregado`). As regras estão em `internal/domain/aggregates/documentos_matricula.go`: o BI é obrigatório no ensino superior e o número exige o arquivo. A Parte 4 criou a conta do encarregado (código `EN`, identificado por telefone), mas o **BI do encarregado continuou nos documentos de matrícula do estudante**, não na conta dele.

**Regras**
- O cadastro passa a aceitar **passaporte** no lugar do BI, para o **estudante** e para o **encarregado**. São dois dados, como no BI: um campo para o **número do passaporte** e o armazenamento do **documento em PDF**. Número sem documento, ou o contrário, é recusado, como no BI.
- O passaporte é **o único documento equivalente** ao BI: nenhum outro (cartão de residente etc.) o substitui.
- **A academia ativa ou desativa** a aceitação do passaporte, numa configuração da própria academia. **Padrão: ativada.** Desativada, cadastros novos e edições que passem a usar passaporte são recusados com mensagem clara; os **cadastros já feitos com passaporte continuam válidos** (nada é invalidado, regra 7).
- **Achar encarregado já existente (correção da Parte 5):** a Parte 5 foi implementada identificando por telefone ou e-mail. Aqui passa a ser pelo **bilhete de identidade do encarregado** e, com o passaporte, também pelo **código do passaporte** (confirmado por você). A tarefa confere o que muda no cadastro do nível escolar e decide onde guardar o BI do encarregado para essa busca (sugestão: na conta do encarregado).
- Cada pessoa tem **BI ou passaporte**, nunca os dois no mesmo papel (sugestão). Onde hoje se exige BI (ex.: ensino superior), o passaporte cumpre a exigência.
- Validação de formato do código: mínima, para não recusar passaporte estrangeiro válido (sugestão). A unicidade segue o que o BI já faz hoje; a tarefa confere.
- Vale em todo lugar onde o BI aparece: cadastro individual e em lote (`/academia/estudante/register`), edição, lista de documentos faltantes, consulta e detalhes.

**Entregas**
- [ ] Número e documento em PDF do passaporte para estudante e encarregado (migration, agregado, eventos, projeção)
- [ ] Achar o encarregado já existente pelo BI e pelo passaporte (correção da Parte 5)
- [ ] `documentos_matricula` aceitando BI ou passaporte
- [ ] Configuração "aceitar passaporte" por academia (padrão ativada), com auditoria
- [ ] Cadastro individual e em lote, edição e consulta
- [ ] Front-end: escolha BI/passaporte nos formulários e chave na configuração da academia
- [ ] Testes (academia com a opção ligada e desligada; cadastro antigo continua válido) e Documentação da API atualizada

---

# Fase 7 — Comunicação e notificações

**Já existe no módulo de comunicação (Tarefa 93):** envio de **um SMS por vez** para um número (`POST /comunicacao/mensagens`) por **admin ou academia**, com provedores **GoSMS e Ziett** e fallback entre eles; remetentes (admin); provedor padrão (admin); listagem. **Não existem:** canal e-mail no histórico, créditos/saldo, envio em lote ou por usuário/filtro, citação, agendamento, regras automáticas, configuração por evento. O e-mail do sistema já funciona por Brevo (`services/brevo_mailer.go`).

**Política de canais** (do arquivo de notificações)

| Canal | Quem paga | Para quê |
|---|---|---|
| E-mail (Brevo) | Spuri | Canal padrão de tudo. O plano Free cobre 300 e-mails/dia; por isso notas e faltas vão em **resumo**, nunca uma mensagem por lançamento |
| SMS Spuri (sistêmico) | Spuri | Só segurança da conta e comunicação da Spuri com a academia. **Lista fechada** (Parte 22) |
| SMS Academia (configurável) | A academia, com créditos | Todo o resto. A academia liga ou desliga cada evento |

O catálogo completo de eventos está no **Anexo A**.

### Parte 21 — Núcleo de notificações (e-mail e SMS)

**Estado:** Parcial · **Depende de:** 3, 5

**Regras**
- **Disparo pós-commit com chave de idempotência** (evento + canal + destinatário). **Não** usar Projection: um `rebuild` reenviaria tudo.
- **Disparar no handler, não só pelo ledger:** algumas decisões não geram evento (ex.: reprovação de solicitação de status acadêmico faz `UPDATE` direto na projeção).
- **Migration de comunicação:** a tabela só aceita `enviado_por_tipo` `admin`/`academia`, `status` `enviada`/`falhou` e `destinatario` de 20 caracteres. Faltam o tipo `sistema`, o **canal**, o destinatário de e-mail, o **número de segmentos**, o **custo** e estados de fila.
- **Contato:** sempre pela regra única da Parte 5 (escolar → encarregado).
- **SMS Spuri = lista fechada** em código (Parte 22); todo o resto é **SMS Academia**.
- **SMS Academia, configurado por academia:** liga/desliga **cada evento** (**nasce desligado**; os marcados ✱ no anexo são só recomendação); destinatário (estudante ou encarregado; no escolar o telefone do encarregado é obrigatório e o do estudante é opcional); modo **imediato ou resumo** (notas e faltas); **teto diário** e **limite por destinatário**; mensagens de até **160 caracteres**.
- **Módulo:** a configuração de notificações da academia e o **SMS Academia** só funcionam com o módulo **Comunicação** ativo (ativo = com cota de SMS, Parte 23). As notificações do sistema (segurança, credenciais) e os e-mails automáticos pagos pelo Spuri **não** dependem de módulo (interpretação adotada).
- **Sem cota de SMS**, o sistema usa o e-mail como alternativa e avisa a academia por e-mail (evento 14, Parte 23).
- **Resumos** (diário/semanal) exigem um **executor de tarefas agendadas** que nunca roda a mesma tarefa duas vezes (mesmo com mais de uma instância ou reinício). Esse executor é reaproveitado na Parte 27.
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

### Parte 22 — Notificações de segurança e conta

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
- **Troca de e-mail/telefone** (eventos 7 e 8): o sistema envia um **código de confirmação ao contato antigo** e a troca só vale depois de o código ser informado (padrão de grandes empresas; código de uso único, validade curta e número limitado de tentativas, valores sugeridos como os da Parte 11). Depois da troca, o contato antigo também é avisado.
- O evento 14 (cota de SMS baixa ou esgotada) fica na Parte 23.

**Entregas**
- [ ] Notificações dos eventos 3, 4, 6, 10, 11, 12, 13 (e-mail e SMS conforme a tabela)
- [ ] Verificação de telefone (código de 6 dígitos) e marcação de `telefone_verificado`
- [ ] Recuperação de senha com canal SMS (telefone verificado)
- [ ] Avisos de troca de e-mail e de telefone ao contato antigo
- [ ] Login suspeito
- [ ] SMS do código de 2FA (opcional, só se for criado)
- [ ] Testes e Documentação da API

---

### Parte 23 — Pacotes de SMS: cota de envio da academia

**Estado:** Pendente · **Depende de:** 8, 9, 10, 11, 21

**Complementa as Partes 26 (SMS manual) e 27 (SMS agendado e automático).** Não cria envio novo: os pacotes dizem apenas **quantas mensagens a academia pode enviar**, conforme o pacote que pagou. Substitui a antiga Parte 19 ("Créditos de SMS da academia").

**Já existe:** nada.

**Regras**
- **Pacotes** no catálogo do código (Parte 9): SMS Básico (1.000), SMS Start (10.000), SMS Pro (30.000), SMS Pro Max (50.000) e SMS Ultra (500.000). Preço do pacote = quantidade × preço por SMS (Parte 9).
- A academia tem uma **cota de SMS**: quantas mensagens ainda pode enviar. **Pagar um pacote soma a quantidade do pacote à cota**, depois de o pagamento ser confirmado pelo provedor (Parte 10) ou marcado como pago manualmente (Parte 11), de forma idempotente.
- **Cada SMS enviado pela academia consome 1 da cota**: SMS manual (Parte 26), agendado e automático (Parte 27) e o SMS Academia das notificações (Parte 21). SMS que falha no envio não consome (sugestão). Dois envios simultâneos não gastam a mesma cota (decremento atômico).
- **Cota esgotada:** nenhuma mensagem é enviada pela academia e o módulo Comunicação fica **desativado** (a consulta do histórico continua, Parte 9). Ao pagar um novo pacote, a cota é atualizada e o módulo volta a ficar ativo.
- Mensagem agendada ou automática que chega sem cota não é enviada e fica "não enviada" (Parte 27).
- **Evento 14, cota baixa ou esgotada:** só e-mail, um aviso ao cruzar o limite (para não gastar SMS avisando que acabou o SMS). **Cota baixa = restam 10% do que foi pago** (10% da quantidade do pacote pago; com várias compras, 10% do total creditado desde a última vez que a cota zerou, sugestão).
- SMS do Spuri (lista fechada) **não** consome a cota da academia.
- **Dinheiro:** os preços dos pacotes são os do **Intelize**. A academia paga ao Spuri o preço do pacote; o Spuri paga ao Intelize depois, fora do sistema.

**Entregas**
- [ ] Cota de SMS por academia, com decremento atômico a cada envio
- [ ] Compra de pacote (provedor ou pagamento manual) somando à cota, de forma idempotente
- [ ] Módulo Comunicação desativa ao zerar a cota e reativa ao pagar novo pacote
- [ ] Evento 14 por e-mail
- [ ] Front-end: ver a cota, comprar pacote e extrato de uso
- [ ] Documentação da API atualizada

---

### Parte 24 — Notificações acadêmicas

**Estado:** Pendente · **Depende de:** 4, 11, 17, 19

Eventos **21 a 29** do Anexo A. Destinatário de estudante do nível escolar é **o encarregado**.

**Regras**
- **E-mail:** notas em resumo diário; faltas em resumo semanal; correção de nota, avaliação final e os demais por evento. O lançamento em lote (assíncrono) agrupa **por estudante**.
- **SMS Academia (opcional, desligado por padrão):** notas e faltas em resumo; o SMS diário de faltas é a opção mais cara de todas.
- **Evento 24, limite de faltas** (†): hoje o limite só é conferido na avaliação final. Precisa de verificação **ao lançar a falta**; no máximo **1 SMS por matéria e período**.
- Eventos 26, 28 e 29 (ano letivo finalizado, solicitações e decisões) são só por e-mail.
- Como só professores lançam notas e faltas (Parte 13), o gatilho nasce nas rotas do professor.

**Entregas**
- [ ] Resumos diário (notas) e semanal (faltas) por e-mail
- [ ] Avisos por evento (21 a 29) por e-mail
- [ ] SMS Academia configurável para 21 a 25 e 27, com resumo
- [ ] Gatilho do limite de faltas ao lançar a falta (1 SMS por matéria e período)
- [ ] Testes e Documentação da API

---

### Parte 25 — Notificações financeiras e de serviços

**Estado:** Pendente · **Depende de:** 7, 8, 17, 19

Eventos **30 a 40** do Anexo A.

**Regras**
- **Gancho único:** `aplicarEfeitosPagamento` (idempotente; usado pelo webhook, pela consulta manual, pela reconciliação e pelo pagamento manual da Parte 11). O evento 30 (pagamento confirmado) é o mais pesado: um por estudante por mês. O **e-mail do recibo** é enviado pela Facturação (Parte 12), para não duplicar; aqui fica o resto do evento 30 e o SMS Academia.
- Cobrança com referência REF (evento 33): só e-mail e só para REF; no GPO o provedor já envia push.
- Evento 36 (pagamento recebido depois do cancelamento local, `CobrancaConflitoPosCancelamento`): raro e urgente, avisa academia e administradores.
- **Evento 34 (lembrete de mensalidade em aberto): retirado do plano.** Não existe data de vencimento no modelo e, por decisão sua, o evento não deve existir.
- Serviços (38 a 40): solicitação criada vai em resumo para a academia; o recibo vem do pagamento confirmado (evento 30). O fluxo de adesão da Parte 7 permanece.

**Entregas**
- [ ] Notificações 30 a 33 e 35 a 37 no gancho de pagamento
- [ ] Notificações de serviços (38 a 40)
- [ ] SMS Academia configurável para 30 a 32 e 39
- [ ] Lembrete 34 (depende da regra de vencimento)
- [ ] Testes e Documentação da API

---

### Parte 26 — SMS manual da academia (individual, coletivo e citação)

**Estado:** Parcial · **Depende de:** 9, 17, 19

**Já existe:** envio de um SMS para um número (evento 41 do anexo), sem usuário, filtro, citação, créditos nem módulo.

**Regras**
- **Quem envia:** academia e secretário (professor não, a confirmar). Exige o módulo Comunicação ativo (com cota de SMS, Parte 23); cada SMS enviado consome a cota.
- **Destinatários:** só usuários **vinculados à academia** (encarregados, estudantes, professores, secretários).
- **Individual:** um usuário. **Coletivo:** vários, por **filtros combináveis**: tipo de usuário, nível, curso, ano, turma, disciplina, situação financeira (em dia/em atraso), serviço, gênero, situação (ativo/inativo), grupo do 4º ano (sugestão). Novos filtros devem ser fáceis de acrescentar.
- **Contato:** regra única da Parte 5. Estudante do nível escolar → SMS vai ao **encarregado**.
- **Sem duplicar:** o mesmo telefone recebendo o mesmo texto final é enviado uma vez; textos finais diferentes são separados. Usuário sem telefone válido é pulado e listado no relatório.
- **Citação de usuário:** o remetente insere no texto uma citação ("Bom dia [usuario.nome]...") e o back-end troca pelo **primeiro nome**, sempre. Duas formas: o **próprio destinatário** (padrão no coletivo) ou **um usuário específico** escolhido na tela. O citado precisa ser vinculado à mesma academia. A troca ocorre **no envio**; se não der para resolver, aquele SMS **não sai** e fica como falha com motivo (nunca sai "[usuario.nome]" cru). O limite de tamanho conta **depois** da troca.
- **Controle:** **prévia** (destinatários, SMS, texto resolvido) e confirmação explícita; limite diário; fila; histórico completo.

**Entregas**
- [x] Envio de um SMS por número (Tarefa 93)
- [ ] Envio individual por usuário e coletivo por filtros
- [ ] Regra de contato aplicada e deduplicação
- [ ] Citação de usuário
- [ ] Prévia, créditos, módulo e limites
- [ ] Front-end: envio, filtros, editor com citação, prévia e histórico
- [ ] Documentação da API atualizada

---

### Parte 27 — SMS agendado e automático (academia e Spuri)

**Estado:** Pendente · **Depende de:** 22

Usa o executor de tarefas agendadas da Parte 21. **Nunca envia duas vezes.**

**Agendado**
- Data e hora no fuso de Luanda, sempre no futuro. A academia **edita ou cancela até a hora do envio**.
- Estados: agendado → enviando → enviado | cancelado | falhou | expirado.
- Filtros e citações resolvidos **no envio**; a prévia do agendamento é indicativa.
- No envio confere o módulo Comunicação (ativo = com cota de SMS) e os limites; se inativo ou sem cota de SMS, não envia e marca "não enviado" (mensagem genérica).
- Se o sistema ficou fora e o envio passou de uma tolerância (sugestão: 1 hora), **não envia**, marca "expirado" e avisa o autor.

**Automático da academia**
- A academia define **mensagens automáticas** enviadas aos **encarregados** (estudantes do nível escolar) ou ao **próprio estudante** (ensino superior), sempre pela regra única de contato. Não vai a professores nem secretários.
- Uma regra tem: **gatilho**, **público** (mesmos filtros da Parte 26), **texto** (com citação) e ativa/inativa; a academia pode pausar e apagar.
- Gatilhos: por **data/recorrência** (diária, semanal, mensal, data fixa, ex.: aniversário) e por **evento do sistema**. Primeira entrega só com data/recorrência; os eventos do sistema vêm depois.
- Nunca envia duas vezes pelo mesmo gatilho para o mesmo destinatário (registro de execuções).
- Com o módulo Comunicação inativo (sem cota de SMS), as regras param; ao reativar, retomam **sem reenviar** o que foi perdido (sugestão).

**Automático do Spuri (administrador FPP)**
- O FPP define mensagens automáticas **para as academias** (destino: o contato da academia, telefone cadastrado, de preferência verificado) **e para qualquer estudante que o administrador FPP definir** (destino pela regra única de contato, Parte 5). Só o `fpp` define (confirmado por você).
- Público por filtros (estado ativa/desativada, nível; módulo ativo/inativo, sugestão) ou por estudantes escolhidos pelo administrador. Mesmas regras de agendamento e de idempotência.
- É **pago pelo Spuri**: não consome créditos de academia nem depende de módulo. Tem teto diário próprio (Parte 3). Só `fpp` cria e edita; fica na auditoria.

**Entregas**
- [ ] Agendamento: criar, editar, cancelar, estados e tolerância
- [ ] Regras automáticas da academia (recorrência) e registro de execuções
- [ ] Regras automáticas do Spuri para academias (FPP)
- [ ] Gatilhos por evento do sistema
- [ ] Front-end: agendamento e gestão de regras (academia e FPP)
- [ ] Documentação da API atualizada

---

# Fase 8 — Credenciais complementares

### Parte 28 — Reset de credenciais de encarregado, professor e secretário

**Estado:** Pendente · **Depende de:** 4, 5

Vem depois da Parte 5 (implementada), que trata só do reset do **estudante**. Esta parte estende o mesmo mecanismo aos outros três perfis.

**Já existe:** o reset/reenvio do estudante (`POST /estudantes/:codigo/credenciais/reenviar`, Tarefa 123) e, com a Parte 4, as contas de encarregado, professor e secretário, criadas com credenciais temporárias. Não há reset para esses perfis: a Tarefa 131 os deixou fora do escopo (se o SMS falhar, a academia elimina a conta e a recria; ponto 64). O reenvio do estudante (`internal/handlers/autenticacao/credenciais_reenvio_handlers.go`) e a regra única de contato (`services.ResolverContato`) são o ponto de partida.

**Regras** (as mesmas do reset do estudante)
- Gera **nova senha temporária**, invalida as sessões, exige troca no primeiro acesso, tem **intervalo mínimo de 10 minutos** e é auditado. Nunca envia a senha por e-mail, **exceto** para secretário e professor de academia em demonstração (Parte 9).
- A senha vai por **SMS ao telefone da própria pessoa** (encarregado, professor ou secretário); para secretário e professor de academia em demonstração, vai por **e-mail**. Se o envio falhar, **nunca** cai para outro telefone; quem resetou vê o erro e reenvia.
- **Quem reseta:** a **academia** reseta os seus encarregados, professores e secretários; o **secretário** reseta professores e encarregados da academia, mas não outros secretários, já que gerir secretários é reservado à academia (Parte 4); `adm` ou `fpp` resetam só contas **sem nenhuma academia**, como na Parte 5.
- **Encarregado com educandos em mais de uma academia:** o reset invalidaria as sessões dele nas outras. A academia só pode resetar o encarregado se **todos os educandos dele estiverem vinculados a ela** (confirmado por você); senão, ele segue o caminho padrão de recuperação de senha.
- Não depende de módulo: SMS de credenciais é do sistema (Parte 9).

**Entregas**
- [ ] Reset/reenvio para encarregado, professor e secretário, reaproveitando o mecanismo do estudante
- [ ] Regras de quem pode resetar cada perfil (inclusive o encarregado de várias academias)
- [ ] Testes por perfil, por papel de quem reseta e por falha de envio
- [ ] Front-end: botão de reset nas telas desses perfis
- [ ] Documentação da API atualizada

---

# Anexo A — Catálogo de notificações

Fonte: `notificacoes_eventos_spuri.md` (base `4759474`, ancestral do `main` atual). **† = gatilho que ainda não existe.** **Config. ✱** = a academia decide; o ✱ é só recomendação, **não vem ligado**. O arquivo de origem **não tem os itens 15 a 20** (seção 2 ausente).

| # | Evento | Para | E-mail | SMS Spuri | SMS Academia | Parte |
|---|---|---|---|---|---|---|
| 1 | Recuperação de senha † (hoje por link; falta SMS) | Usuário | Sim | Sim (tel. verificado) | — | 22 |
| 2 | Código de 2FA no login | Usuário | Sim (existe) | Opcional | — | 22 |
| 3 | 2FA ativado | Usuário | Sim | — | — | 22 |
| 4 | 2FA desativado | Usuário | Sim | Sim | — | 22 |
| 5 | Verificação de telefone (6 dígitos) † | Usuário | — | Sim | — | 22 |
| 6 | Senha alterada (usuário, academia, admin) | Usuário | Sim | Fallback | — | 22 |
| 7 | E-mail alterado | Endereço antigo e telefone | Sim | Sim | — | 22 |
| 8 | Telefone alterado | Número antigo e e-mail | Sim | Sim (número antigo) | — | 22 |
| 9 | Login suspeito † | Usuário | Sim | — | — | 22 |
| 10 | Verificação de e-mail e boas-vindas de admin | Usuário | Sim (existem) | — | — | 22 |
| 11 | Admin criado, role alterada, desativado ou deletado | Admin | Sim | — | — | 22 |
| 12 | Academia ativada | Academia | Sim | Sim | — | 22 |
| 13 | Academia desativada (com motivo) | Academia | Sim | Sim | — | 22 |
| 14 | Saldo de SMS baixo ou esgotado † | Academia | Sim | — | — | 23 |
| 21 | Notas registradas | Estudante/encarregado | Sim (resumo diário) | — | Config. (resumo) | 24 |
| 22 | Nota corrigida | Estudante/encarregado | Sim | — | Config. | 24 |
| 23 | Faltas registradas ou corrigidas | Estudante/encarregado | Sim (resumo semanal) | — | Config. (resumo) | 24 |
| 24 | Limite de faltas próximo ou ultrapassado † | Estudante/encarregado | Sim | — | Config. ✱ | 24 |
| 25 | Avaliação final (escolar e superior) | Estudante/encarregado | Sim | — | Config. ✱ | 24 |
| 26 | Ano letivo da academia finalizado | Academia | Sim | — | — | 24 |
| 27 | Decisão de interrupção, desvinculação ou revinculação | Estudante | Sim | — | Config. | 24 |
| 28 | Solicitação de status, edição de dados ou NIF criada | Academia (NIF: admins) | Sim | — | — | 24 |
| 29 | Decisão sobre edição de dados ou NIF | Solicitante | Sim | — | — | 24 |
| 30 | Pagamento confirmado (mensalidade, serviço) | Pagador | Sim (recibo, enviado pela Facturação: Parte 12) | — | Config. ✱ | 25 |
| 31 | Pagamento manual registrado (fora do sistema) | Pagador (estudante, encarregado ou academia) | Sim | — | Config. | 25 |
| 32 | Cobrança falhou, expirou ou foi cancelada | Pagador | Sim | — | Config. | 25 |
| 33 | Cobrança criada com referência REF | Pagador | Sim (só REF) | — | — | 25 |
| 34 | **Retirado:** lembrete de mensalidade em aberto (não existe data de vencimento; o evento não deve existir) | — | — | — | — | 25 |
| 35 | Obrigação de mensalidade anulada ou reativada | Estudante | Sim | — | — | 25 |
| 36 | Conflito de pagamento pós-cancelamento | Academia e admins | Sim | — | — | 25 |
| 37 | Credenciais do provedor alteradas ou webhook rotacionado | Academia | Sim | — | — | 25 |
| 38 | Solicitação de serviço criada | Academia | Sim (resumo) | — | — | 25 |
| 39 | Serviço aprovado (pendente de pagamento), reprovado ou cancelado | Estudante | Sim | — | Config. | 25 |
| 40 | Serviço vinculado | Estudante | Sim | — | — | 25 |
| 41 | Comunicado escrito pela academia (já existe) | Escolhido pela academia | — | — | Sim (debita créditos) | 26 |
| 42 | Preço de módulo alterado, com o nome de quem alterou † (novo) | Demais administradores FPP | Sim | — | — | 9 |
| 43 | Preço de módulo atualizado, sem dizer quem alterou † (novo) | Academias com o módulo contratado | Sim | — | — | 9 |

Itens 15–20: removidos por você do arquivo de origem; fora do plano.

---

# Interpretações adotadas e pontos a confirmar

Onde a lista original ou o arquivo de notificações deixava dúvida, usei o padrão abaixo. Se algum estiver errado, é aqui que se corrige.

| # | Ponto | Padrão adotado | Parte |
|---|---|---|---|
| 1 | "Um canal" no cadastro de estudante | O canal único é o cadastro feito pela academia (e secretário) | 1 |
| 2 | Secretário "menos a dos professores" | Menos **lançar notas e faltas**, que passa a ser só dos professores (vale também para a academia) | 4, 13 |
| 3 | Relação com o educando "opcional" | A relação é opcional; se "outro", o texto é obrigatório | 4 |
| 4 | Encarregado: "não pode consultar" x "consultas limitadas ao período escolar" | Sem ações e sem dados do período posterior; leitura do período escolar | 4 |
| 5 | Marco de "finalizar o nível escolar" | Aprovado no último ano do médio ou matriculado no superior, o que vier primeiro | 4 |
| 6 | Encarregados por estudante | Um por vez (trocável pela academia); um encarregado pode ter vários educandos | 4, 5 |
| 7 | Como achar encarregado já existente | Hoje (Parte 5 implementada): telefone ou e-mail. **Correção na Parte 20:** pelo **bilhete de identidade do encarregado** e, com o passaporte, pelo **código do passaporte** | 5, 20 |
| 8 | Papéis de admin | "ADMIN ou FPP" = papéis `adm` e `fpp` da hierarquia existente | 5, 9 |
| 9 | "Pacote Finanças + paypay.go" | O pacote já existe (`internal/finance`; a Parte 2 o move para `internal/handlers/finance`); o PayPay entra como `provedor_paypay.go`, padrão do AppyPay | 6 |
| 10 | Módulo inativo | **Confirmado por você:** o **bloqueio é no middleware do back-end** e vale só para **ações**; a **consulta continua**. O **front permite navegar** e bloqueia a ação, sempre com um **aviso (CTA)** que informa a contratação do módulo e leva, por um botão, à **página de assinatura daquele módulo**. Estudantes e encarregados só consultam e não pagam; na Gestão Académica, academia, secretário e professores só consultam. A academia sempre entra e paga; pagamentos já iniciados continuam sendo registrados. Para quem não contrata (secretário, professor, estudante, encarregado), o aviso é o genérico, sem botão (sugestão) | 9 |
| 11 | Academias novas | Nascem **sem assinatura**. Quando o cadastro é aprovado, **ela mesma paga os módulos escolhidos** (Parte 10). O administrador só entra no processo para definir que foi **pago por fora** do sistema (Parte 11). Sem migração | 9, 10, 11 |
| 12 | Plano gratuito | **Mudou em 2026-11-22:** fora a **demonstração gratuita** (15 dias, só Gestão Académica e Finanças, sem cadastro de estudantes), não existem módulos gratuitos do Spuri → academias. O gratuito também existe nos preços que a academia define em Serviços e no prazo geral do Transporte (Parte 7) | 7, 9 |
| 13 | Tela "Minha assinatura" | Mostra o estado real; só as mensagens operacionais são genéricas | 9 |
| 14 | Recálculo da avaliação final | Notas: sempre. Faltas: só com reprovação por faltas ativa. Só dentro do ano letivo do registro. **Hoje não existe**, é implementação | 13 |
| 15 | Refazer matrícula com pagamentos e notas | Pagas ficam sem devolução automática; notas/faltas ficam na matrícula antiga; avaliação antiga anulada | 18 |
| 16 | Assinatura `comunicacao_sms` **e** créditos | **Mudou:** os pacotes de SMS são só a **cota de envio**, complemento das Partes 26 e 27. Pagar o pacote soma à cota; esgotada, nada é enviado e o módulo Comunicação desativa | 9, 23, 26, 27 |
| 17 | Quem envia SMS manual | Academia e secretário; professor não | 26 |
| 18 | "Envios automáticos de SMS" | Academia → encarregados/estudantes (confirmado antes). **Spuri (FPP) → academias e qualquer estudante que o administrador FPP definir**; só o `fpp` (confirmado por você) | 27 |
| 19 | Facturação inativa | Sem emissão e sem recibos retroativos | 12 |
| 20 | Documento de estudante do nível escolar | Em nome do encarregado | 12 |
| 21 | Módulo da Comunidade | **Módulo independente**, como todo módulo (Parte 9). Tudo sobre ele e seus recursos fica no `Plano de Atualizações 2.md` | — |
| 22 | **Contradição no arquivo de notificações:** a legenda diz que os ✱ **não** vêm ligados; o fim do arquivo diz que vêm "já ligados por padrão" | Adotei a legenda (você a escreveu depois): **nascem desligados**, só recomendados | 21, 24, 25 |
| 23 | **Frase incompleta** no início da seção 1 ("Quando o telefone tá verificado e 2FA tá ativada, em vez de") | **Ignorar** (decisão sua): não muda nenhum evento, inclusive o 2 | 22 |
| 24 | **Itens 15 a 20** (seção 2) | **Removidos por você** do arquivo; fora do plano | 22 a 25 |
| 25 | Evento 7 ("confirmação para o e-mail recente...") | **Esclarecido por você:** envia-se um **código de confirmação ao contato antigo** e a troca só vale depois de ele ser informado (padrão de grandes empresas). O contato antigo também é avisado da troca. Adotei o mesmo para o telefone (evento 8) | 22 |
| 26 | Evento 34 (lembrete de mensalidade) | **Confirmado por você:** não deveria existir; **retirado do plano** (não há data de vencimento) | 25 |
| 27 | Item 1 do arquivo (recuperação "troca a senha na hora") | **Desatualizado:** a Tarefa 123 já trocou por link. Resta o canal SMS | 22 |
| 28 | Item 5 do arquivo (limitadores "no-op") | **Desatualizado:** foram removidos; login e e-mail seguem sem limite. Virou pré-requisito | 21 |
| 29 | Tipo do valor monetário | Banco `NUMERIC(14,2)` e Go `float64` com `roundAmount`, como na Tarefa 26. **Não migrar** para inteiro em cêntimos nem para decimal; corrigir só a validação de entrada (erro 400 para mais de 2 casas ou acima do teto, em vez de arredondar em silêncio) | 8 |
| 30 | Código morto da avaliação final manual | **Confirmado por você:** eliminar de vez, sem suporte a legado (sem rota 410, sem stub). Ficam o método do agregado e os eventos antigos | 13 |
| 31 | Onde mora o pacote `finance` | Um só pacote, `internal/handlers/finance/` (serviço + HTTP), antes das Partes 6 e 7; os imports de `apoio` e `servicosextras` a tarefa resolve | 2 |
| 32 | "Estudante ativo" | **Vinculado à academia** (vínculo atual; desvinculado ou de outra academia não conta) e com pelo menos um dos status por nível (`status_escolar_fundamental`, `status_escolar_medio`, `status_superior`) em `em_andamento`; `inativo` e `finalizado` não contam | 9 |
| 33 | Preço da Gestão Académica | **Confirmado por você (2026-11-22):** 30.000 Kz/mês fixos para limite de até 1.000 estudantes; acima de 1.000, 30 Kz por estudante do limite (30 × limite). São valores iniciais: o FPP pode alterá-los | 9 |
| 34 | Estudantes além do limite | **Novo modelo (2026-11-22):** a academia define o limite ao contratar; para cadastrar mais, paga **excedente** por módulo com período em curso (preço unitário × excedente × meses que faltam), e pagar o excedente da Gestão Académica obriga a pagar os outros módulos em curso | 9, 10 |
| 35 | Preços dos pacotes de SMS | **Confirmado por você:** são os preços do **Intelize**; o Spuri recebe da academia e depois paga ao Intelize. A cota conta SMS enviados | 9, 23 |
| 36 | Pagamento manual | Total (sem parcial), motivo obrigatório, sem "desfazer"; academia e secretário nas cobranças do estudante, só FPP com código nas do Spuri | 11 |
| 37 | Atribuição manual de módulo | Cria a cobrança e a marca como paga (recibo emitido como em qualquer pagamento); substitui a "atribuição sem cobrança nem recibo" | 11, 12 |
| 38 | Recibo por e-mail | Só para e-mail **verificado**; estudante do nível escolar → encarregado; sem e-mail verificado não envia | 12 |
| 39 | Cópia no Mega | Tarefa em segundo plano; falha ou ausência do Mega nunca afeta a emissão | 12 |
| 40 | Passaporte | **Confirmado por você:** mesma coisa que o BI: campo para o número e **documento em PDF**; único equivalente; BI **ou** passaporte; a academia liga/desliga (padrão ligada); cadastros antigos continuam válidos | 20 |
| 41 | Sumários sequenciais | Sequência opcional por (academia, matéria, curso, nível, ano, período), com histórico de reordenação | 19 |
| 42 | "Prazo geral (pode ser definido como gratuito)" do Transporte | Como você escreveu: gratuito no prazo geral = nenhum mês é cobrado, em todas as rotas, e nenhuma rota define prazo | 7 |
| 43 | Meses do ano civil x ano letivo (Serviços) | Cobra-se nos meses selecionados dentro do ano letivo corrente; a tarefa confirma como os dois calendários se cruzam | 7 |
| 44 | Taxa de inscrição e fluxo de adesão dos Serviços | **Confirmado por você:** os dois permanecem (taxa opcional; solicitação → aprovação → vínculo) | 7, 25 |
| 45 | Independência dos módulos | Todo módulo é independente: cada recurso depende só do próprio módulo. Pagar mensalidades e serviços pelo sistema é recurso de Finanças; configurar e vincular serviços é recurso de Serviços | 9 |
| 46 | Notificações e módulos | Segurança, credenciais e e-mails automáticos: sem módulo; SMS Academia e configuração de notificações: Comunicação | 21 |
| 47 | Reset de credenciais dos outros perfis | Entra na Parte 28, depois da Parte 5; mesmo mecanismo do estudante | 28 |
| 48 | Horário de embarque do Transporte | Um horário `hh:mm` por rota (se variar por dia da semana, é só dizer) | 7 |
| 49 | Demonstração gratuita | 15 dias corridos, Gestão Académica e Finanças, sem cadastro de estudantes nem de encarregado; credenciais de secretário e professor por e-mail durante a demonstração (confirmado por você); a academia ativa uma vez; não renova | 9 |
| 50 | Meses que faltam no excedente | **Confirmado por você:** dias restantes ÷ 30, arredondando para cima; os 6 e 11 dos exemplos são os dados daqueles cenários | 9 |
| 51 | Preço da Gestão Académica e a faixa de 501 a 1.000 | **Confirmado por você:** o fixo de 30.000 vale até 1.000, para uma escola de 501 não pagar menos que uma de 500 | 9 |
| 52 | Preços editáveis pelo FPP | **Confirmado por você:** preços fixos e por estudante; a alteração vale só para academia sem o módulo contratado; o excedente usa os preços congelados do período. Módulos, recursos e planos seguem no código | 9, 10 |
| 53 | Finanças e Serviços | **Confirmado por você:** 15 Kz por estudante por mês (era 20) | 9 |
| 54 | Renovação antecipada | **Confirmado por você:** o período atual mantém o preço; o período novo usa o preço vigente | 9 |
| 55 | Avisos de alteração de preço | **Confirmado por você:** o código por e-mail é mantido; aviso aos outros administradores FPP (com o nome de quem alterou) e às academias com o módulo contratado (sem o nome) | 9 |
| 56 | Permissões do secretário | A academia escolhe, ao criar cada secretário, entre um catálogo fechado (`GET /academia/permissoes`). Reservado à academia: gerir secretários, dados/NIF/alvará/nível da academia, credenciais do provedor de pagamento. Negado a todos os secretários: lançar notas/faltas e pagar | 4 |
| 57 | E-mail e idade dos novos tipos | E-mail opcional e único entre **todos** os tipos de conta; idade mínima de 18 anos para secretário, professor e encarregado | 4 |
| 58 | Código de login dos novos tipos | `SC`, `PF` e `EN` + 6 dígitos (não colide com o do estudante nem o da academia) | 4 |
| 59 | Relação do encarregado | `pai`, `mae`, `irmao`, `irma`, `outro` (texto obrigatório se `outro`) | 4 |
| 60 | Vínculo de professor | A turma e a matéria têm de existir na academia, estar ativas e ser do mesmo nível (e do mesmo curso, quando ambas têm) | 4 |
| 61 | Encarregado em "leitura" | Só `GET` de notas e faltas, com o ano académico forçado ao período escolar; todo o resto devolve `ENCARREGADO_SOMENTE_LEITURA` | 4 |
| 62 | Telefone do encarregado | É o identificador do encarregado (único entre encarregados). O estudante guarda um retrato dele no cadastro (exigido por uma restrição da tabela), mas a fonte do contacto é a conta do encarregado | 5 |
| 63 | Estudante que concluiu o escolar e se desvinculou do encarregado | Passa a responder por si na regra única de contato (adulto) | 5 |
| 64 | Credenciais de secretário, professor e encarregado | A Tarefa 131 não fez reenvio para esses tipos: se o SMS falhar, a academia elimina a conta e a recria. O reenvio entra na Parte 28 | 4, 5, 28 |

## O que ainda preciso receber

- **Partes 4 e 5:** confirmar ou corrigir os pontos 56 a 64 da tabela acima. O que mais pesa: o ponto 56 (que ações são reservadas à academia) e o ponto 64 (reenvio de credenciais dos novos tipos, tratado agora na Parte 28).
- **Parte 12:** as regras fiscais (NIF, séries, IVA ou isenção por tipo de serviço) que o Ulemo não resolver sozinho. A tarefa lê a documentação dele e lista só o que faltar.
- **Parte 14:** as regras de avaliação de cada instituição de ensino superior, **sempre que uma for integrada** (você as entrega no momento; nada pendente agora).
- **`Plano de Atualizações 2.md`:** preço e planos do módulo Comunidade.

## Fora do escopo deste plano

- **Pesquisa e Estatística** (retirada do plano por enquanto).
- Módulo Comunidade e seus recursos (tratados no `Plano de Atualizações 2.md`).
- Devolução automática de pagamentos.
- Migrar os valores monetários do Go de `float64` para inteiro em cêntimos ou decimal (decisão: manter `float64`, ver ponto 29).
- Criação e personalização de serviços pela academia (removida); serviços além de Transporte e Atividade extracurricular (criados por nós, sob demanda).
- Criação de módulos e planos por administradores (o catálogo é código).
- Módulo concedido sem cobrança (cortesia), que o pedido não descreve.
- Pagamento manual parcial e "desfazer" pagamento manual.

---

# Registro de andamento

Preencher a cada entrega (ver "Como acompanhar o andamento").

| Data | Parte / item | Tarefa | Commit | Observação |
|---|---|---|---|---|
| 2026-11-17 | Plano v2: itens já existentes marcados `[x]` | — | — | Verificado em `main` @ `d9c47fd` |
| 2026-11-18 | Parte 1 (todos os itens) | 128 (back) · 38 (front) | `673be19` (back) · `740ce90` (front) | Marcada só em 2026-11-19, junto com a Tarefa 129 |
| 2026-11-19 | Parte 2 (menos o HTTP do financeiro) | 129 (back) | ver o texto do commit da Tarefa 129 | 273 rotas e handlers idênticos antes e depois; suíte: mesmos 1057 testes passam + 38 novos |
| 2026-11-19 | Parte 3 (todos os itens) | 129 (back) · 39 (front) | ver o texto do commit da Tarefa 129 | Migration 139. Regra nova em "Como acompanhar": o plano atualizado vai em toda entrega |
| 2026-11-21 | Plano atualizado: Parte 8 (Garantia 4) e Parte 13 (faltas sem alterar nota; recálculo sem gravar nota; eliminação do código morto) | — | — | Só análise e planejamento, nada implementado. Verificado em `main` @ `ffc13e2` com a suíte executada |
| 2026-11-21 | Plano atualizado: módulos no código e bloqueio por módulo (Parte 9), contratação (10), pagamento manual (11, nova), Serviços (7 reescrita, 8 ajustada), Facturação subiu para a Parte 12, sumários (19) e passaporte (20) novos, pacote `finance` (2), Comunicação (23); renumeração das partes depois da 10 | — | — | Só análise e planejamento, nada implementado. Verificado por leitura em `main` @ `ffc13e2`; suíte não executada; front-end não reanalisado. Partes 4 e 5 não alteradas (em andamento em outro chat). Revisado no mesmo dia: bloqueio em middleware com consulta mantida, gratuito onde descrito, estudantes vinculados, professor fora dos sumários, pacotes de SMS como cota de envio, front que permite navegar com aviso (CTA), Comunidade como módulo, Parte 28 (reset de credenciais), evento 34 retirado e respostas às interpretações |
| 2026-11-22 | Plano atualizado com as respostas às pendências: Parte 5 sem alteração (correção do encarregado na Parte 20), Parte 3 aceita, PayPay sem credenciais e sem manual, Parte 14 com aviso enquanto desabilitada, cota baixa = 10%, Ulemo sem ambiente de testes, taxa de inscrição e adesão mantidas, embarque e horários com início e fim, complementar para todo módulo por estudante, reset do encarregado, Comunidade movida para o `Plano de Atualizações 2.md`; e, no mesmo dia, demonstração gratuita e cobrança por limite de estudantes com excedente por módulo (Partes 9, 10 e 11), com o preço fixo da Gestão Académica até 1.000 estudantes e e-mail para secretário e professor na demonstração; preços editáveis pelo FPP (só para quem não tem o módulo contratado) e Finanças e Serviços a 15 Kz por estudante, com avisos de alteração de preço | — | — | Só análise e planejamento, nada implementado. Sem nova verificação do repositório |
| 2026-11-23 | Parte 4 (todos os itens) | 130 (back) · 40 (front) | `7f9b70e` (back) | Migration 140. API 4.0.0. Back-end validado com PostgreSQL real; front-end com `tsc`, `eslint` e `next build` (sem teste em navegador) |
| 2026-11-23 | Parte 5 (todos os itens) | 131 (back) · 41 (front) | `7f9b70e` (back) | Migration 141. Mudança incompatível: cadastro escolar exige o encarregado e `PUT /estudante/encarregado/telefone` saiu. Versão da API da Tarefa 129 corrigida para 3.1.0 |
| 2026-11-23 | Plano final: detalhes das Partes 4 e 5 integrados (Como ficou, matriz, passo 9, interpretações 56 a 64); seção de renumeração removida | — | — | Só análise e planejamento. Lido o repositório em `main` @ `7f9b70e`. Achado: no repositório, a linha `Versão atual` do topo da `Documentação da API.md` diz 3.0.2, mas a tabela de versões e as Tarefas 130 e 131 dizem 4.0.0; corrigir na próxima entrega que mexer na API |
| | | | | |
