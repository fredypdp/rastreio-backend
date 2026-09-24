# Modelo de Tarefa para o Codex — Backend (`rastreio-backend`)

> Este documento descreve a estrutura e as convenções que **todo** documento de tarefa para o Codex, neste repositório, deve seguir. Não é uma tarefa real — é o gabarito a copiar e preencher ao criar uma nova.

## Princípio central

- Quem escreve o documento de tarefa (o orquestrador) já **planeou, escreveu e validou** a solução antes de entregar — inclusive tudo o que depende de PostgreSQL real, que o Codex normalmente não tem no seu ambiente.
- O Codex (quem executa) **nunca planeia nem repensa a abordagem** — só aplica exatamente o que o documento descreve, corre as verificações pedidas, e confirma o resultado.
- Sempre que o ambiente do Codex tiver uma limitação conhecida (sem Postgres/Docker, sem acesso a `golang.org/x/*` para baixar dependências novas, etc.), o documento diz isso explicitamente perto do topo e explica o que fazer com essa limitação — normalmente "pule só esta verificação específica, já validada por quem escreveu este documento".

## Onde guardar e como nomear

- Pendente: `docs/Lista de Tarefas/<número> - <Título da tarefa>.md` (e o `.patch` correspondente, se houver, no mesmo sítio).
- Concluída: mover para `docs/Tarefas feitas/`, mantendo o mesmo nome.
- A numeração é própria deste repositório, independente da numeração do `rastreio-frontend`. Ao referenciar uma tarefa do outro repositório, citar sempre número + nome do repositório (ex.: "Tarefa 19 do `rastreio-frontend`").

## Duas variantes — qual usar

| | Variante A — patch pronto | Variante B — arquivo completo / diffs no documento |
|---|---|---|
| Quando usar | Mudança pequena e isolada (1–3 arquivos) | Mudança grande, que toca muitos arquivos, ou onde o próprio código faz parte da explicação |
| Onde está o código | Num ficheiro `.patch` ao lado do `.md` | Dentro do próprio `.md`, em blocos de código |
| O que o Codex faz | `git apply` do patch + verificação + mover ficheiros | Copiar cada bloco exatamente como descrito (substituição total do arquivo OU o par "Localizar → Substituir") + verificação + mover ficheiros |

As duas seguem o mesmo princípio: o Codex só executa.

---

## Variante A — patch pronto

```markdown
# Tarefa para o Codex — Repositório `rastreio-backend` (backend)

**Repositório:** https://github.com/fredypdp/rastreio-backend
**Branch base:** main
**Execução:** Não é necessário planejar nada. Todo o código já foi escrito e validado pelo orquestrador (ver "O que já foi validado" abaixo) e está pronto no arquivo `<número> - <Título>.patch`, nesta mesma pasta. Sua única tarefa é aplicar o patch, rodar as verificações do Passo 2, e mover/renomear os arquivos conforme o Passo 3.

**Ordem de deploy:** [ex.: nenhuma dependência com a Tarefa N do `rastreio-frontend` — pode ir para produção em qualquer ordem / OU: depende da Tarefa N já estar em produção]

---

## Contexto do problema

[O que foi reportado (bug) ou pedido (feature). Se for bug: log/erro real, causa raiz no código (arquivo + função) e como foi reproduzido — de preferência empiricamente, não só por leitura de código.]

## O que o patch faz

[Lista arquivo a arquivo do que muda e por quê. Separar claramente "N arquivo(s) alterado(s) + N arquivo(s) novo(s)".]

## O que já foi validado pelo orquestrador

[O que já correu no sandbox do orquestrador: gofmt/go build/go vet/go test antes e depois do patch — baseline vs com o patch, confirmando as mesmas falhas pré-existentes e nenhuma nova —, os testes novos passando, `go test -race` se relevante, e idealmente uma validação final num clone novo e limpo de `main` aplicando o `.patch` exatamente como o Codex vai aplicar.]

## Passo 1 — Aplicar o patch

Na raiz do repositório:

\`\`\`bash
git apply "docs/Lista de Tarefas/<número> - <Título>.patch"
\`\`\`

Deve alterar N arquivo(s) [e criar N novo(s)]. Se `git apply` falhar, PARE — não recrie as mudanças manualmente, reporte o conflito.

## Passo 2 — Verificação

\`\`\`bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
\`\`\`

Os três primeiros devem terminar sem nenhuma saída/erro. `go test ./...` deve mostrar [os N testes novos] passando; se aparecerem falhas fora do escopo desta tarefa, confirme que são as mesmas falhas pré-existentes do ambiente antes de reportar como problema — não foram causadas por este patch.

## O que NÃO fazer (fora de escopo)

[Lista explícita de pontos próximos ao código tocado que NÃO devem ser alterados, e por quê — inclusive referências a tarefas irmãs (deste ou do outro repositório) que cobrem a parte que fica de fora daqui.]

## Passo 3 — Marcar como feito

Depois que os Passos 1–2 passarem sem problema, mova este arquivo e o `.patch` correspondente de `docs/Lista de Tarefas/` para `docs/Tarefas feitas/`.

## Resumo das mudanças (checklist final)

- [ ] Patch aplicado (`git apply`) sem conflitos
- [ ] `gofmt -l .` sem nenhuma saída
- [ ] `go build ./...` limpo
- [ ] `go vet ./...` limpo
- [ ] `go test ./...` — mesmas falhas pré-existentes de sempre (nenhuma nova), com os testes novos passando
- [ ] Arquivos desta tarefa movidos para `docs/Tarefas feitas/`
```

---

## Variante B — arquivo completo / diffs "Localizar → Substituir"

```markdown
# [Título da tarefa]

## Antes de começar (leia isto primeiro)

Esta tarefa já foi **inteiramente planejada, implementada e validada** por quem escreveu este documento[: detalhar o que foi validado e como — Postgres real com as migrations existentes aplicadas, gofmt em cada arquivo novo, etc.]. Sua tarefa é só aplicar as mudanças abaixo exatamente como estão descritas — não há necessidade de repensar a abordagem. Onde o documento diz "substitua o arquivo inteiro por", é para substituir o arquivo inteiro (não fazer merge parcial). Onde diz "Localizar este bloco exato" / "Substituir por", é uma mudança cirúrgica pequena — localize o trecho pelo contexto ao redor e aplique só o que muda.

[Se aplicável, nota permanente sobre a limitação do ambiente do Codex — ex.: "Você não tem acesso a PostgreSQL/Docker neste ambiente — não tente subir um banco para validar a migration, isso já foi validado por quem escreveu este documento." ou "O seu ambiente bloqueia `golang.org/x/*` — não tente `go get` nada novo; se algo não compilar, é provável que seja um detalhe pontual de assinatura que mudou desde que este documento foi escrito — corrija localmente sem alterar a arquitetura descrita."]

## ⚠️ Dependência de outra tarefa (se houver)

[Nome + repositório da tarefa da qual esta depende, o que quebra em runtime se for aplicada fora de ordem, e o que continua funcionando mesmo sem a outra.]

## Contexto — o que está sendo resolvido e por quê

1. [Primeiro problema/objetivo, com detalhe suficiente para alguém sem contexto entender o "porquê", não só o "o quê".]
2. [Segundo, se houver.]
3. [Terceiro, se houver — inclua aqui qualquer bug real encontrado "de bónus" durante a investigação, mesmo que fora do pedido original.]

## Decisões de design já tomadas (não precisa reavaliar)

- [Cada decisão de arquitetura/modelagem já fechada, com o "porquê" e, quando útil, referência a um padrão já existente no projeto que está a ser replicado.]

## Resumo executivo (opcional — útil em tarefas com várias partes)

| Área | O que muda |
| --- | --- |
| [Migrations] | [...] |
| [Agregados] | [...] |
| [Rotas] | [...] |
| [Arquivos que **não** mudam] | [...] |

---

## Passo 1 — [ex.: Nova migration]

Crie o arquivo `migrations/<próximo-número>_<nome>.sql` (confirme qual é o próximo número disponível) com este conteúdo exato:

\`\`\`sql
-- <número>_<nome>.sql
[SQL completo, com comentário explicando o motivo de cada decisão não óbvia]
\`\`\`

## Passo 2 — Substituir `<caminho/do/arquivo.go>`

Substitua o arquivo **inteiro** por este conteúdo:

\`\`\`go
[conteúdo completo do arquivo]
\`\`\`

## Passo 3 — Editar `<caminho/do/outro_arquivo.go>`

Esta é uma mudança **cirúrgica**, não uma substituição de arquivo inteiro.

### Localizar este bloco exato

\`\`\`go
[bloco exatamente como está hoje no arquivo, com contexto suficiente ao redor para ser único]
\`\`\`

### Substituir por

\`\`\`go
[bloco novo]
\`\`\`

[Repita "Passo N" quantas vezes for preciso — um por arquivo ou por unidade lógica de mudança. Numere sempre na ordem de aplicação (ex.: a migration antes do código que a usa; o registo da rota depois do handler já existir).]

---

## Validação (rode nesta ordem)

\`\`\`bash
go build ./...
go vet ./...
go test ./...
\`\`\`

[Diga exatamente o que esperar de cada comando — inclusive quais falhas são conhecidas/pré-existentes e não indicam problema nesta tarefa (ex.: testes que dependem de `DATABASE_URL`/credenciais reais não configuradas neste ambiente). Se a suíte completa for lenta ou tiver partes irrelevantes, prefira um filtro, ex. `go test ./internal/domain/aggregates/... ./internal/projections/...`.]

## O que NÃO fazer

- [Lista explícita — inclusive "não toque em X, que parece relacionado mas é um caso genuinamente diferente" quando for o caso.]

## Testes obrigatórios (quando a tarefa introduz lógica nova)

[Lista dos cenários que precisam de teste automatizado, com detalhe suficiente (valores de entrada, resultado esperado) para não deixar ambíguo.]

## Critérios de aceite

- [ ] [Cada critério observável e verificável — evitar "funciona bem", preferir "responde 200 com X quando Y".]

## Procedimento de conclusão

1. Confirme que todos os critérios de aceite acima estão satisfeitos.
2. Mova este documento de `docs/Lista de Tarefas/` para `docs/Tarefas feitas/`, e adicione "(feito)" no início do título dentro do arquivo.
3. Ao final do documento, acrescente uma secção **Resultado** com um parágrafo curto descrevendo o que foi efetivamente feito e qualquer desvio pontual em relação ao que foi pedido.
4. Não abra pull request nem faça merge — deixe o commit pronto para revisão.
```

---

## Convenções transversais (valem para as duas variantes)

- **Ordem fixa de validação**: `gofmt -l .` → `go build ./...` → `go vet ./...` → `go test ./...` (ou um subconjunto filtrado do último, quando a suíte completa for lenta ou irrelevante para a tarefa).
- **Baseline vs com a mudança**: sempre que possível, quem escreve o documento roda a suíte sem a mudança primeiro, anota as falhas pré-existentes, depois roda com a mudança e confirma que são exatamente as mesmas (nenhuma nova).
- **Fora de escopo nunca fica implícito**: toda tarefa diz explicitamente o que fica de fora, principalmente quando há uma tarefa irmã (no `rastreio-frontend` ou no mesmo repositório) cobrindo a parte complementar.
- **Nunca abrir PR/merge sozinho** — o commit fica pronto para revisão humana.
- **"Prompt recomendado para executar a atualização"** (opcional): um parágrafo único, logo após o título, resumindo a tarefa de ponta a ponta — útil quando o documento vai ser entregue junto de um prompt curto em vez de lido por extenso primeiro.
