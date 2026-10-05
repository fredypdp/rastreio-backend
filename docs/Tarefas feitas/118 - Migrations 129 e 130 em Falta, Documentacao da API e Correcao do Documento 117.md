# Tarefa 118 — Migrations 129 e 130 em falta, Documentação da API e correção do documento 117 (backend)

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `a1d46b1`
**Entrega:** apenas os arquivos novos ou atualizados, na pasta `rastreio-backend/`, com os mesmos caminhos do repositório
**Ordem de deploy:** **urgente.** As duas migrations têm de estar no commit implantado (ou ser aplicadas à mão no Neon, ver "Correção em produção"); enquanto faltarem, `GET /academia/turmas` e `GET /academia/faltas/configuracao` respondem 500. A parte de documentação não afeta o deploy. Sem dependência da Tarefa 26 do `rastreio-frontend`, mas a página `/faltas/configuracoes` só funciona depois desta correção.

---

## Contexto

Depois do deploy das Tarefas 116 e 117 apareceram dois erros em produção:

- `GET /academia/turmas` → `pq: column "tema_trabalho" does not exist`
- `GET /academia/faltas/configuracao` → `pq: relation "projection_faltas_configuracao" does not exist`

**Causa:** os arquivos `migrations/129_turmas_tema_trabalho.sql` e `migrations/130_faltas_configuracao.sql` **não estão no `main`**. O commit `a1d46b1` tem o código das Tarefas 116 e 117, mas a pasta `migrations/` termina em `128_…`. O backend só aplica migrations no arranque: `RunMigrations` executa todo `.sql` da pasta que ainda não consta em `schema_migrations`. Sem os arquivos, o código novo consulta uma coluna e uma tabela que não existem.

**Reproduzido:** no `main` atual, os testes de integração `TestIntegrationProjecaoDeGrupoDoQuartoAnoMedio`, `TestIntegrationHandlersDeGrupoDoQuartoAnoMedioComTema`, `TestIntegrationConfiguracaoFaltasPutEGet` e `TestIntegrationAvaliacaoFinalZeraNotaDoProfessorPorFaltas` falham com exatamente essas duas mensagens. Com os dois arquivos acrescentados, passam.

Esta tarefa também resolve duas pendências das Tarefas 116 e 117: a **Documentação da API** (`Documentação da API.md`, na raiz) não tinha sido atualizada, e o documento 117 trazia uma frase imprecisa sobre recálculo.

## Decisões de design

- **Nenhuma alteração de código Go.** As migrations são as mesmas das Tarefas 116 e 117 (`ADD COLUMN IF NOT EXISTS` e `CREATE TABLE IF NOT EXISTS`), por isso podem ser aplicadas à mão antes do deploy sem conflito (verificado).
- **A Documentação da API foi escrita a partir de respostas reais**, capturadas dos handlers em testes de integração com PostgreSQL, e não só da leitura do código. Dois detalhes antigos dessas secções foram corrigidos por estarem diferentes do comportamento real: o `status` de exemplo em `GET /academia/turmas` é `ativo` (não `ativa`), e a mensagem do `PUT /academia/turma/:codigo/dados` é `turma atualizada com sucesso` (a do documento antigo não existia no código).
- **Correção do documento 117:** ele dizia que uma avaliação final já gravada podia mudar se uma nota fosse lançada ou corrigida de novo. Isso está errado: o backend ignora a regra quando já existe resultado para o mesmo estudante, ano letivo e tipo de regra (`regraPodeExecutarAutomaticamente` devolve "encerrar" e o laço segue adiante), e o agregado também bloqueia a duplicidade. Uma avaliação gravada **nunca** é recalculada. O que o documento passa a dizer: a regra de faltas só atua quando a avaliação é calculada pela primeira vez (em cada etapa da cadeia: final, com exame, com recurso).

## O que foi atualizado e onde

**3 arquivos alterados ou novos no código/documentação + 2 migrations novas.**

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `migrations/129_turmas_tema_trabalho.sql` (**novo no repositório**) | — | `ALTER TABLE projection_turmas ADD COLUMN IF NOT EXISTS tema_trabalho TEXT` (da Tarefa 116) |
| `migrations/130_faltas_configuracao.sql` (**novo no repositório**) | — | Tabela `projection_faltas_configuracao` com os `CHECK`s (da Tarefa 117) |
| `Documentação da API.md` | 2.8 `TurmaDTO` (linha 369) | Campos `tipo_agrupamento` e `tema_trabalho`, e parágrafo sobre grupos do 4º ano médio |
| | 12. Turmas: processo (linha 5139), `GET /academia/turmas` (5141), `POST /academia/turma` (5231), `PUT /academia/turma/:codigo/dados` (5319) | Grupos, `tema_trabalho` (regras, limites, remover com `""`), `tipo_agrupamento` nas respostas, mensagens "grupo …", exemplo de grupo com tema |
| | 14. Faltas: processo (5808), `GET /academia/faltas/configuracao` (6027), `PUT /academia/faltas/configuracao` (6060) | Rotas novas com requests, respostas reais, validações (`400`), regra de cálculo e tabela das notas zeradas |
| | 15. Avaliações Finais: `resultados_materias` (6322) e regras de execução automática (6432) | Campo `notas_zeradas_por_faltas` e a regra da reprovação por faltas, incluindo que uma avaliação gravada não é recalculada |
| `docs/Tarefas feitas/117 - Limite de Faltas e Reprovacao por Faltas.md` | secção "Quando a regra é aplicada" (linha 27) | Explicação corrigida (ver Decisões de design) |

## Correção em produção

**Opção A (recomendada):** coloque os dois `.sql` em `migrations/` no repositório, faça o commit e o deploy. No log do arranque devem aparecer `📄 Aplicando: 129_turmas_tema_trabalho.sql`, `📄 Aplicando: 130_faltas_configuracao.sql` e `🎉 2 migration(s) aplicada(s) com sucesso!`.

**Opção B (imediata, sem esperar o deploy):** no SQL Editor do Neon, execute o conteúdo dos dois arquivos, por ordem (129 e depois 130). É seguro porque os dois são idempotentes; no próximo arranque com os arquivos na pasta, o backend os executa de novo sem erro e os regista em `schema_migrations` (testado). **Mesmo usando a Opção B, os arquivos precisam entrar no repositório**, senão qualquer outro ambiente (local, teste, novo banco) volta a falhar.

**Como confirmar:** depois do deploy, `select filename from schema_migrations where filename like '129%' or filename like '130%';` devolve 2 linhas, e `GET /academia/turmas` e `GET /academia/faltas/configuracao` respondem 200.

## Validação realizada

Ambiente: Go 1.24 + PostgreSQL 16 reais.

- **Banco em 128, deploy com as duas migrations:** o arranque aplicou `129` e `130` (2 migrations) e criou a coluna e a tabela; um segundo arranque disse que tudo já estava aplicado.
- **SQL aplicado à mão e depois o arranque por cima:** sem erro, e as duas linhas ficaram em `schema_migrations`.
- **Clone novo de `main` @ `a1d46b1` com os dois `.sql` acrescentados:** `gofmt -l` vazio, `go build` e `go vet` limpos, e `go test ./...` com todos os pacotes `ok`.
- **Documentação da API:** os 8 blocos JSON novos são JSON válido e vêm das respostas reais dos handlers.

## Como aplicar

1. **Confirme que a base não mudou.** Na raiz do repositório:
   ```bash
   git diff --stat a1d46b1 HEAD -- "Documentação da API.md" "docs/Tarefas feitas/117 - Limite de Faltas e Reprovacao por Faltas.md"
   ```
   Saída **vazia** = pode substituir. Se aparecer algum arquivo, ele mudou depois da base: não substitua e avise.
2. **Copie** a pasta `rastreio-backend/` para a raiz do repositório, mantendo os caminhos. Os arquivos são completos e substituem os existentes por inteiro.
3. **Verifique** e **marque como feito** (abaixo).

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

Os três primeiros não devem imprimir nada e `go test ./...` deve mostrar todos os pacotes `ok`. Os testes `TestIntegration…` só correm de verdade com PostgreSQL de teste descartável (`RUN_POSTGRES_INTEGRATION=1 DATABASE_URL=postgres://… go test ./...`); eles aplicam as migrations do repositório, por isso são o teste de que os dois `.sql` estão no lugar certo.

## Fora de escopo

- **Não** alterar código Go, rotas ou projeções.
- **Não** recalcular avaliações finais já gravadas, nem criar um botão ou gatilho de recálculo ao registar faltas.
- **Não** acrescentar uma verificação automática do esquema no arranque.
- Frontend: Tarefa 26 do `rastreio-frontend` (textos da página de configurações de faltas).

## Marcar como feito

1. Troque `**Estado:** pendente` por `**Estado:** feito` e coloque `(feito)` no início do título.
2. Acrescente no fim uma secção **Resultado** com um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mova este documento de `docs/Lista de Tarefas/` para `docs/Tarefas feitas/`, com o mesmo nome.

## Checklist

- [ ] Base conferida (`git diff --stat a1d46b1 HEAD -- …` vazio)
- [ ] `migrations/129_…` e `migrations/130_…` no repositório e no commit implantado
- [ ] Log do arranque mostra as duas migrations aplicadas (ou SQL aplicado à mão no Neon)
- [ ] `GET /academia/turmas` e `GET /academia/faltas/configuracao` respondem 200
- [ ] `Documentação da API.md` e documento 117 substituídos
- [ ] `go test ./...` — todos `ok`
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`
