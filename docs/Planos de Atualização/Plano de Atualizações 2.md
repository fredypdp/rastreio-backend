# Plano de Atualizações 2 — Módulo Comunidade

Atualizado em 2026-11-23. Este plano reúne **tudo sobre o módulo Comunidade e seus recursos**. O resto das atualizações está em `Plano_de_Atualizacoes.md` (o "plano principal"). Aqui as partes têm numeração própria; as partes do plano principal são sempre citadas como "Parte N do plano principal".

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

## Fora do escopo deste plano

- Confirmação de presença em eventos.
- Outras funcionalidades da Comunidade (ainda não definidas).

---

# Registro de andamento

Preencher a cada entrega.

| Data | Parte | Tarefa back | Tarefa front | Observação |
|---|---|---|---|---|
| 2026-11-22 | — | — | — | Plano criado: a Parte 28 (Módulo Comunidade: Eventos) do plano principal passou para cá. Só análise e planejamento |
| | | | | |
