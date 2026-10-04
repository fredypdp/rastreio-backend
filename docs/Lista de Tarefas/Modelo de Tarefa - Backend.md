# Modelo de Tarefa — Backend (`rastreio-backend`)

> Estrutura e convenções que **todo** documento de tarefa deste repositório segue. Não é uma tarefa real — é o gabarito a copiar e preencher ao criar uma nova.

## Princípio central

- O documento de tarefa **explica o que foi atualizado e onde**. Ele **não carrega o código**: o código é entregue à parte, em **arquivos completos e atualizados**, numa pasta com a **mesma estrutura de caminhos do repositório** (`cmd/…`, `internal/…`, `migrations/…`, `docs/…`).
- Quem escreve o documento já **planeou, implementou e validou** tudo antes de entregar — inclusive o que depende de PostgreSQL real (migrations aplicadas do zero, projeções, testes de integração). O documento traz a **evidência real** (saída dos comandos), não só a intenção.
- Cada mudança é localizada com precisão: **arquivo** (alterado ou novo) → **onde** (função, tipo ou trecho, com o número da linha no arquivo entregue) → **o que mudou**.

## Onde guardar e como nomear

- Pendente: `docs/Lista de Tarefas/<número> - <Título da tarefa>.md`.
- Concluída: mover para `docs/Tarefas feitas/`, **mantendo o mesmo nome**; dentro do arquivo, `**Estado:** feito`, `(feito)` no início do título e uma secção **Resultado** no fim.
- A numeração é própria deste repositório, independente da do `rastreio-frontend`. Ao referenciar uma tarefa do outro repositório, citar sempre número + nome do repositório (ex.: "Tarefa 25 do `rastreio-frontend`").
- Os arquivos de código acompanham o documento numa pasta `rastreio-backend/` que **espelha a raiz do repositório**; copiá-la por cima da raiz coloca cada arquivo no sítio certo. Os arquivos entregues são **completos** e substituem os existentes por inteiro (nunca é uma mesclagem parcial).

## Estrutura do documento (copiar e preencher)

```markdown
# Tarefa <número> — <Título> (backend)

**Estado:** pendente
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `<hash curto>`
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório
**Ordem de deploy:** [nenhuma dependência com a Tarefa N do `rastreio-frontend` — qualquer ordem / OU: deve ir para produção antes da Tarefa N do `rastreio-frontend`, porque …]

---

## Contexto

[O que foi pedido (feature) ou reportado (bug). Se for bug: erro/log real, causa raiz (arquivo + função) e como foi reproduzido — de preferência de forma empírica, não só por leitura de código.]

## Decisões de design

[Cada decisão de arquitetura/modelagem já fechada, com o "porquê" e, quando útil, o padrão já existente no projeto que foi replicado.]

## O que foi atualizado e onde

**N arquivos alterados + N novos.**

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `caminho/do/arquivo.go` | `NomeDaFuncao` (linha N) | [descrição curta] |
| `migrations/<número>_<nome>.sql` (**novo**) | — | [descrição curta] |

[Se a tarefa tem API nova ou alterada: contrato resumido (método, rota, corpo, resposta, erros).]

## Validação realizada

[Ambiente (versões de Go e PostgreSQL). Baseline de `main` sem a mudança; depois, num clone novo de `main` com a pasta de arquivos copiada por cima: `gofmt`, `go build`, `go vet`, `go test ./...` com a **saída real**. Ciclo reverter → falhar → restaurar para cada regra crítica (tabela "regra desfeita → teste que falhou").]

## Falha antiga que não é desta tarefa

[Qualquer falha ou instabilidade que já existia em `main`, para não ser atribuída a esta tarefa. Se não houver, escrever "Nenhuma".]

## Como aplicar

1. **Confirme que a base não mudou:** `git diff --stat <hash> HEAD -- <arquivos que serão substituídos>`. Vazio = pode substituir; se aparecer algum arquivo, ele mudou depois da base — não substitua e avise.
2. **Copie** a pasta `rastreio-backend/` para a raiz do repositório, mantendo os caminhos.
3. **Verifique** (abaixo).
4. **Marque como feito** (abaixo).

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

[O que esperar de cada comando. Os testes `TestIntegration…` só correm de verdade com PostgreSQL de teste descartável: `RUN_POSTGRES_INTEGRATION=1 DATABASE_URL=postgres://… go test ./...`.]

## Fora de escopo

[Lista explícita do que fica de fora, com referência às tarefas irmãs (deste ou do outro repositório) que cobrem a parte complementar.]

## Marcar como feito

1. `**Estado:** feito`, `(feito)` no título.
2. Secção **Resultado** no fim: um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mover para `docs/Tarefas feitas/`, mesmo nome.

## Checklist

- [ ] Base conferida
- [ ] Arquivos copiados (N alterados, N novos); migrations sem conflito de número
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test ./...` — todos `ok`
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`
```

## Convenções transversais

- **O documento nunca carrega código de implementação.** Trechos curtos só quando fazem parte de um contrato (corpo/resposta de API) ou de um comando de verificação.
- **Ordem fixa de validação:** `gofmt -l .` → `go build ./...` → `go vet ./...` → `go test ./...`.
- **Baseline vs com a mudança:** roda-se a suíte sem a mudança primeiro, anotam-se as falhas antigas, depois com a mudança, e confirma-se que não surgiu nenhuma nova.
- **Tudo que depende de PostgreSQL é validado de verdade** (migrations do zero, testes de integração, 2ª execução no mesmo banco) e o resultado entra no documento.
- **Regras críticas têm teste que falha sem elas:** desfaz-se a regra, o teste tem de cair, restaura-se.
- **Base explícita:** o documento diz sobre que commit de `main` os arquivos foram gerados e dá o comando para detectar divergência antes de substituir.
- **Ordem de deploy sempre explícita:** quando uma mudança remove ou renomeia um campo JSON de que o frontend depende, backend e frontend têm de ir juntos e o documento traz um aviso claro. Mudanças que só acrescentam campos ou rotas dizem que o frontend atual não é afetado.
- **Numeração de migrations:** confirmar o próximo número livre antes de entregar; se outro trabalho tiver entrado em `main`, renumerar antes da entrega.
- **Fora de escopo nunca fica implícito:** toda tarefa lista o que não faz, principalmente quando há tarefa irmã cobrindo a parte complementar.
- **Sem segredos** (chaves, tokens, senhas, URLs de banco reais) em documentos nem em arquivos entregues.
