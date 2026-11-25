# Tarefa 135 — Parte 2B (2B-1): configuração de faltas em CRUD (backend)

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `52e137a`, **depois da Tarefa 134** (as duas vão no mesmo pacote, nesta ordem)
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/` (o mesmo pacote da 134); os dois arquivos a apagar desta tarefa estão na lista da 134
**Plano:** Parte 2B, entrega 2B-1: configurações (faltas)
**Versão da API:** 4.0.1 → **4.0.2** (a mesma versão da 134)
**Ordem de deploy:** só backend; sem dependência com o `rastreio-frontend`. A migration 145 roda sozinha na subida.

---

## Contexto

`ConfiguracaoFaltas` (limite de faltas por período e reprovação por faltas, uma por academia) deixa de ser event sourcing. `projection_faltas_configuracao` vira a tabela de registro e cada `PUT /academia/faltas/configuracao` grava e audita na mesma transação. Sai o agregado e o evento `ConfiguracaoFaltasDefinida`. Nada se migra. Rota, corpo, resposta e permissões iguais.

## Decisões de design

1. **Uma linha por academia, com `UPSERT`** (`INSERT … ON CONFLICT (codigo_academia)`), a linha antiga lida com `FOR UPDATE` para guardar o estado anterior na auditoria (`antes`). `version` começa em 1 e sobe a cada definição. A auditoria usa o `codigo_academia` como identificador da entidade (a coluna é `VARCHAR(80)`).
2. **Regras iguais às do agregado:** limite entre 1 e 500 (ou nulo), e reprovação por faltas só com limite definido. Também validadas pelos `CHECK` que já existiam na tabela.
3. **A academia precisa existir** (conferido dentro da transação). A tabela não tinha chave estrangeira para a academia, então a migration não remove nada.
4. **O ID determinístico do agregado (`ConfiguracaoFaltasAggregateID`) deixa de existir.**
5. **Quem lê a configuração não muda:** `FaltasConfiguracaoProjection.GetByAcademia` continua igual, só sem consumir eventos.

## O que foi atualizado e onde

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `migrations/145_faltas_configuracao_crud.sql` (**novo**) | — | Apaga o checkpoint `faltas_configuracao`; `COMMENT` na tabela. |
| `internal/cadastros/faltas_configuracao.go` (**novo**) | `ConfiguracaoFaltas` (29), `NovaConfiguracaoFaltas` (39), `FaltasConfiguracaoRepo` (76), `Definir` (85) | Regras puras e persistência com auditoria. |
| `internal/cadastros/faltas_configuracao_test.go` (**novo**) | 2 testes | Regras (válidos, inválidos, cópia do limite) e integração (substituição, versão, auditoria, academia inexistente). |
| `internal/handlers/faltas/faltas_configuracao_handlers.go` | `DefinirConfiguracaoFaltas` (41) | Chama o repositório CRUD; resposta igual. |
| `internal/handlers/faltas/faltas_configuracao_integration_test.go` | `TestIntegrationConfiguracaoFaltasPutEGet` | Sem "levar o ledger para a projeção"; confere 2 linhas de auditoria com o estado anterior e a versão 2. |
| `internal/projections/faltas_configuracao_projection.go` | — | Sem `Name`, checkpoint, `Handle`, `Rebuild`; ficam o DTO e a leitura. |
| `cmd/server/main.go` | `registrarProjecoes` | Sai o registro de `faltas_configuracao`. |
| `internal/db/politica_eventsourcing.go`, `safe_queries.go`, `internal/domain/aggregates/aggregate.go` | `TabelasCRUD`, whitelist, fábrica | Sai o tipo; entra `projection_faltas_configuracao` em `TabelasCRUD` (a guarda da 134 passa a proteger a tabela). |

**Contrato HTTP:** igual.

## Antes de aplicar em produção

```sql
SELECT aggregate_type, event_type, count(*) FROM spuri_ledger WHERE aggregate_type = 'ConfiguracaoFaltas' GROUP BY 1, 2;
SELECT count(*) FROM projection_faltas_configuracao;  -- deve ser igual depois do deploy
```

Se a primeira consulta mostrar eventos reais, a linha atual da tabela já os reflete (nada se migra); o ledger antigo não é apagado. Sem volta automática, como na 134.

## Validação realizada

Mesmo ambiente, base e execução da 134 (850 testes passam, 2 pulados, 0 falhas). Mutação: tirar a regra "reprovação exige limite" derruba `TestConfiguracaoFaltasRegras`. Além disso, a reconstrução real da 134 passa a cobrir também esta tabela, porque ela entrou em `TabelasCRUD`. **Não provado:** o uso da configuração pela avaliação final automática foi coberto apenas pelos testes que já existiam (verdes), sem teste novo.

## Falha antiga que não é desta tarefa

A mesma da 134 (segunda execução no mesmo banco). Nenhuma falha nova.

## Como aplicar / Verificação

Como na 134 (mesmo pacote, mesmos comandos). Migration 145: `ls migrations | grep ^145` não deve listar nada antes da cópia.

## Fora de escopo

`ConfiguracaoSistema`, a lista de anos letivos e os demais grupos da 2B-1; toda a 2B-2. Nota sobre "anos letivos": `AnoLetivoAcademiaDefinido` e `AnoLetivoAcademiaFinalizado` estão na lista **aprovada** de event sourcing pela política da 132, então não são convertidos; o plano diz "configurações (faltas, sistema, anos letivos)" e isso precisa da sua confirmação (ver a pergunta final da entrega).

## Marcar como feito

`**Estado:** feito`, `(feito)` no título, secção **Resultado**, mover para `docs/Tarefas feitas/`.

## Checklist

- [ ] Entregue e aplicada junto com a 134
- [ ] `go test ./...` — todos `ok`
- [ ] Estado trocado para **feito** e documento movido
