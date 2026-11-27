# Plano de Atualizações 2 — Módulo Comunidade

Atualizado em 2026-11-27. Este plano reúne **tudo sobre o módulo Comunidade e seus recursos**. O resto das atualizações está em `Plano_de_Atualizacoes.md` (o "plano principal"). Aqui as partes têm numeração própria; as partes do plano principal são sempre citadas como "Parte N do plano principal".

Só análise e planejamento: nada foi implementado.

---

## Como usar este plano

- Valem as **regras 1 a 17 do plano principal** ("Regras que valem para todas as partes"), em especial: o back-end decide (regra 1), todo recurso declara o módulo que o protege (regra 11), o preço por número de estudantes conta só estudantes ativos vinculados à academia (regra 15) e a persistência segue a regra 17 (os eventos da Comunidade ficam em CRUD, não em event sourcing).
- Tarefas, enumeração, documentos de tarefa, ordem de deploy e atualização deste arquivo seguem o mesmo método do plano principal ("Como acompanhar o andamento").
- **(sugestão)** = regra acrescentada por mim para fechar lacuna; pode ser cortada. **(a confirmar)** = decisão pendente.

---

## Visão geral

| Parte | O que é | Depende de | Estado | Tarefa |
|---|---|---|---|---|
| 1 | Módulo Comunidade: Eventos | Partes 4 e 9 do plano principal | Pendente | |

---

## Módulo Comunidade no catálogo

- **Id:** `comunidade`. É um **módulo independente**, como todo módulo: só depende da própria assinatura e nenhum outro módulo precisa estar ativo.
- **Recursos:** Eventos (Parte 1 deste plano); as demais funcionalidades ainda não estão definidas.
- **Preço e planos:** **a definir** (entrada que preciso de você). Sem plano contratável, o módulo fica inativo para todas as academias.
- **Catálogo:** quando este plano for executado, o módulo entra no catálogo da Parte 9 do plano principal, por código (módulos não são criados por administradores).
- **Bloqueio:** o mesmo dos outros módulos. Com `comunidade` inativo, o middleware do back-end bloqueia só as **ações** e a **consulta continua**; o front permite navegar e mostra o aviso (CTA) com o botão para a página de assinatura do módulo (Parte 9 do plano principal).

---

### Parte 1 — Módulo Comunidade: Eventos

**Estado:** Pendente · **Depende de:** Partes 4 e 9 do plano principal

**Já existe:** nada. Cuidado com o nome: o back-end já tem rotas `/eventos-estudante/:codigo` e `/eventos/:event_id`, que são **eventos de auditoria do ledger**. Os da Comunidade usam **`/comunidade/eventos`**.

**Regras**
- **Eventos** é a primeira funcionalidade do módulo.
- A academia e o secretário criam, editam, cancelam e **agendam** eventos: título, descrição, início e fim, local, **público-alvo** (todos, ou por tipo de usuário, nível, curso, turma) e estado (agendado, cancelado, realizado).
- Só vê quem é vinculado à academia **dentro do público-alvo**. Estudante do nível escolar: visível ao estudante e ao encarregado.
- Evento cancelado continua aparecendo como "cancelado"; mudança de data/hora marca "alterado"; o passado vira histórico só de leitura.
- Aviso por SMS (sugestão) usa a Parte 26 do plano principal e só aparece com o módulo Comunicação ativo (com cota de SMS). Avisos automáticos de evento aguardam o catálogo de notificações.
- Fora do escopo: confirmação de presença.

**Entregas**
- [ ] Módulo Comunidade (`comunidade`; rotas `/comunidade/...`) e permissões, declarando o módulo em todas as rotas
- [ ] Eventos: criar, editar, cancelar, agendar, público-alvo e visibilidade
- [ ] Front-end e Documentação da API

---

## O que ainda preciso receber

- **Preço e planos do módulo Comunidade.**
- **Duas confirmações sobre o provedor PayPay** (Parte 6 do plano principal, já implementada), detalhadas na seção seguinte: (1) se os pagamentos Express e Reference chegam sempre ao estado `TRADE_FINISHED`; (2) se o `seller_actual_amount` é o valor bruto pago ou já descontado das taxas.

## Dúvidas em aberto sobre o PayPay (Parte 6 do plano principal)

Estas dúvidas não têm relação com o módulo Comunidade; ficam aqui por pedido, como pendências a resolver. Nada disto impede o uso do PayPay, só afina duas regras. A resposta vem do PayPay ou de um teste com credenciais de teste.

**Dúvida 1 — Os pagamentos Express e Reference chegam sempre ao estado `TRADE_FINISHED`?**
- **O que está implementado:** só `TRADE_FINISHED` (pagamento liquidado) quita a cobrança. `TRADE_SUCCESS` é tratado como aviso sem efeito, porque a documentação do PayPay recomenda ignorá-lo e só chama de concluído o `TRADE_FINISHED`.
- **Hipótese:** sim, todo pagamento concluído passa por `TRADE_FINISHED`, e o `TRADE_SUCCESS` é só um passo intermédio. É o que a documentação sugere, mas ela não diz se vale para os dois métodos.
- **Hipótese alternativa:** em algum método (por exemplo, Reference pago num caixa) o pedido fica em `TRADE_SUCCESS` e nunca avança. Nesse caso a cobrança **nunca quitaria** e o pagador ficaria com a dívida aberta, apesar de ter pago.
- **Como confirmar:** perguntar ao PayPay, ou, com credenciais de teste, pagar um pedido de cada método e consultar o estado (`trade_query`) logo a seguir e algumas horas depois.
- **O que muda se a alternativa for a verdadeira:** `TRADE_SUCCESS` (confirmado pela consulta ao vivo) passa a quitar, no método afetado. É uma alteração pequena na tradução de estados do provedor e nos testes.

**Dúvida 2 — O `seller_actual_amount` da consulta é o valor bruto pago ou já descontado das taxas?**
- **O que está implementado:** a regra de "pagamento divergente" compara o `amount` (valor do pedido no PayPay) com o valor cobrado. O `seller_actual_amount` não entra na comparação.
- **Hipótese A (bruto):** é o valor que o pagador pagou. Então a comparação passa a usá-lo também, o que apanha o caso de o pagador pagar um valor diferente do pedido, caso que a comparação só do `amount` não apanha.
- **Hipótese B (líquido de taxas):** é o que chegou à conta da escola depois das taxas do PayPay. Então comparar com o cobrado marcaria **todo** pagamento como divergente (por exemplo, 1470 contra 1500) e nenhuma cobrança quitaria. Nesse caso o campo continua de fora.
- **Qual parece mais provável:** não há como afirmar só pela documentação, que descreve o campo apenas como "valor real pago".
- **Como confirmar:** perguntar ao PayPay, ou, com credenciais de teste, pagar 1500 Kz e ver o que a consulta devolve: 1500 indica bruto; menos que 1500, líquido.
- **O que muda:** só se for bruto, acrescenta-se a comparação na consulta do provedor e um teste. Se for líquido, nada muda.

## Fora do escopo deste plano

- Confirmação de presença em eventos.
- Outras funcionalidades da Comunidade (ainda não definidas).

---

# Registro de andamento

Preencher a cada entrega.

| Data | Parte | Tarefa back | Tarefa front | Observação |
|---|---|---|---|---|
| 2026-11-22 | — | — | — | Plano criado: a Parte 28 (Módulo Comunidade: Eventos) do plano principal passou para cá. Só análise e planejamento |
| 2026-11-27 | — | — | — | Registadas aqui, por pedido, duas dúvidas em aberto sobre o PayPay (Parte 6 do plano principal), com hipóteses. Sem relação com o módulo Comunidade; sem alteração de código |
| | | | | |
