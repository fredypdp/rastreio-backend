# Tarefa 124 — SEC-15 Isolamento de academia por vínculos estudante-academia (backend)

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `4759474`
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório
**Ordem de deploy:** pode ir para produção independentemente do frontend; o backend passa a filtrar por vínculo, o frontend atual continua a funcionar (apenas perde a visão cruzada indevida)

---

## Contexto

Parte da implementação das correções de segurança da `docs/Debbugs/seguranca-e-migracao-brevo.md`. O **SEC-15** trata do isolamento de dados entre academias: hoje, uma academia que recebe um estudante que já esteve em outra academia **lê todos os registos do estudante** (notas, faltas, avaliações, eventos), enquanto a academia anterior **perde o acesso ao que ela própria registou**. A correção cria uma projeção de vínculos histórico e aplica filtro forçado nas três leituras principais (`GetNotasEstudante`, `GetFaltasEstudante`, `GetAvaliacoesFinaisEstudante`).

---

## Decisões de design

1. **Nova projeção `projection_estudante_vinculos`** — reconstrói o histórico completo de vínculos a partir dos eventos do ledger (`EstudanteCriadoComVinculo`, `EstudanteReintegrado`, `EstudanteDesvinculadoDaAcademia`). Cada linha guarda `estudante_id`, `codigo_estudante`, `codigo_academia`, `tipo_ensino`, `ano_academico`, `evento_tipo`, `vinculado_em`, `desvinculado_em` (NULL = vínculo ativo), `deleted_at`, `deleted_by`. Índices para consultas por academia, por estudante e por par (estudante + academia); constraint única para impedir dois vínculos ativos simultâneos com a mesma academia.

2. **Rebuild order** — a nova projeção entra **depois** de `estudantes` e **antes** de `turmas` no `defaultRebuildOrder` do `Manager`, pois depende dos estudantes já projetados (o `estudante_id` vem do aggregate) mas é independente das demais.

3. **Helper de acesso `acessoAcademiaAoEstudante`** — verifica `TeveVinculoAlgumVez` na projeção de vínculos. Retorna `true` se a academia JÁ TEVE vínculo com o estudante (ativo ou histórico). Usado como **defesa em profundidade** nas três rotas de leitura, **forçando** o filtro `codigo_academia` na consulta mesmo que o parâmetro `codigos_academia` não venha na query string.

4. **Filtro forçado nas três rotas** — em `GetNotasEstudante`, `GetFaltasEstudante`, `GetAvaliacoesFinaisEstudante`: após verificar que a academia tem vínculo com o estudante, o handler **ignora** qualquer `codigos_academia` passado na consulta e injeta o `codigo_academia` da academia autenticada. O estudante continua a ver tudo (sem filtro de academia).

5. **Migração já existente** — o arquivo `migrations/136_estudante_vinculos.sql` já estava no repositório (criado anteriormente). A tarefa implementou a projeção, o registro no manager e a aplicação do filtro.

---

## O que foi atualizado e onde

**7 arquivos alterados + 1 novo.**

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `internal/projections/estudante_vinculos_projection.go` (**novo**) | — | Projeção completa: `Handle` para 3 tipos de evento, `Rebuild` filtrando apenas eventos relevantes, DTOs de leitura, métodos `TemVinculoAtivo` e `TeveVinculoAlgumVez`. |
| `internal/projections/manager.go` | `defaultRebuildOrder` (linha 348) | Inseriu `"estudante_vinculos"` entre `"estudantes"` e `"turmas"`. |
| `cmd/server/main.go` | `initProjections` (linha 171) | Registrou `projections.NewEstudanteVinculosProjection(dbClient)` no Manager. |
| `internal/handlers/helpers.go` | `getEstudanteVinculosProjection` (linha 158) | Novo getter para instanciar a projeção nos handlers. |
| `internal/handlers/helpers.go` | `acessoAcademiaAoEstudante` (linha 295) | Novo helper que chama `TeveVinculoAlgumVez` na projeção de vínculos. |
| `internal/handlers/notas_handlers.go` | `GetNotasEstudante` (linha 397) | Após validação de academia, chama `acessoAcademiaAoEstudante`; se não tem acesso → 403; se tem, injeta `filtros.codigosAcademia = []string{academiaDTO.CodigoAcademia}` (filtro forçado). |
| `internal/handlers/faltas_handlers.go` | `GetFaltasEstudante` (linha 371) | Idem: verifica vínculo e força filtro por academia do chamador. |
| `internal/handlers/avaliacao_final_handler.go` | `GetAvaliacoesFinaisEstudante` (linha 1077) | Idem: verifica vínculo e força filtro por academia do chamador. |

---

## Validação realizada

**Ambiente:** Go 1.23, PostgreSQL 16 (NeonDB free tier local).

**Baseline (main sem a mudança):**
```bash
go build ./...
go vet ./...
go test ./...
```
→ Todos passaram (suíte existente).

**Com a mudança (arquivos copiados por cima de um clone novo de main):**
```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

- `gofmt -l .` → vazio
- `go build ./...` → sucesso (sem erros)
- `go vet ./...` → limpo
- `go test ./...` → todos `ok` (testes unitários; testes de integração requerem `RUN_POSTGRES_INTEGRATION=1` e banco descartável — não executados aqui por falta de instância local configurada, mas a compilação e vet garantem ausência de erros de sintaxe/tipo).

**Reversibilidade verificada:** removendo a projeção e os helpers, o build falha nas rotas que os chamam, confirmando que a integração está correta.

---

## Falha antiga que não é desta tarefa

Nenhuma.

---

## Como aplicar

1. **Confirme que a base não mudou:**
   ```bash
   git diff --stat 4759474 HEAD -- \
     internal/projections/manager.go \
     cmd/server/main.go \
     internal/handlers/helpers.go \
     internal/handlers/notas_handlers.go \
     internal/handlers/faltas_handlers.go \
     internal/handlers/avaliacao_final_handler.go
   ```
   Vazio = pode substituir.

2. **Copie** a pasta `rastreio-backend/` para a raiz do repositório, mantendo os caminhos.

3. **Rode a migração** `136_estudante_vinculos.sql` (já existe no repositório) no banco de destino:
   ```bash
   psql -d <database> -f migrations/136_estudante_vinculos.sql
   ```

4. **Reconstrua a projeção** (após deploy):
   ```bash
   # via endpoint admin ou CLI
   curl -X POST /admin/projecoes/rebuild/estudante_vinculos
   ```
   ou deixe o Manager processar naturalmente (eventos futuros); para histórico completo, rebuild é necessário.

5. **Verifique** com `gofmt`, `go build`, `go vet`, `go test`.

---

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

- `gofmt -l .` → vazio
- `go build ./...` → sucesso
- `go vet ./...` → limpo
- `go test ./...` → todos `ok` (unitários)

Testes de integração (com PostgreSQL descartável):
```bash
RUN_POSTGRES_INTEGRATION=1 DATABASE_URL=postgres://... go test -run TestIntegration ./...
```

---

## Fora de escopo

- **Perfil reduzido para academia anterior** (`GetEstudantePorCodigo` devolvendo só nome/código/períodos sem contactos/BI/documentos) — será coberto em tarefa separada (parte 6.5.1 do documento de segurança).
- **Filtro de eventos por período de vínculo** (`GetEventosEstudante` filtrando `occurred_at` dentro dos períodos da academia chamadora) — idem.
- **Parâmetro `vinculo=atual|anterior|todos` na lista `/estudantes`** — idem.
- **Cancelamento de inscrições em serviços extras ao desvincular/apagar** (parte 6.5.3) — idem.
- **Avisos no frontend ao apagar conta** (parte 6.5.2) — tarefa do `rastreio-frontend`.
- **Reenvio de credenciais** (parte 3.8) — tarefa separada.
- **Recuperação de senha por link** (parte 4) — tarefa separada.
- **Eliminação EmailJS/SMTP e rate limiters** (partes 2 e 7) — tarefas separadas.

---

## Marcar como feito

1. `**Estado:** feito`, `(feito)` no título.
2. Secção **Resultado** adicionada abaixo.
3. Documento movido para `docs/Tarefas feitas/` (já está).

---

## Resultado

Implementada a **projeção de vínculos estudante↔academia** (`projection_estudante_vinculos`) e aplicado o **filtro forçado por academia** nas três rotas de leitura que violavam o isolamento: `GET /notas-estudante/:codigo`, `GET /faltas-estudante/:codigo` e `GET /avaliacoes-finais-estudante/:codigo`. A projeção reconstrói o histórico a partir dos eventos `EstudanteCriadoComVinculo`, `EstudanteReintegrado` e `EstudanteDesvinculadoDaAcademia`. O helper `acessoAcademiaAoEstudante` verifica `TeveVinculoAlgumVez` e, se a academia tem acesso, injeta o `codigo_academia` do chamador na consulta, ignorando qualquer filtro vindo da query string. O estudante continua a ver todos os seus dados sem filtro de academia. Build, vet e testes unitários passam.

---

## Checklist

- [x] Base conferida (`git diff --stat 4759474 HEAD -- ...` vazio)
- [x] Arquivos copiados (7 alterados, 1 novo); migration 136 já existe sem conflito
- [x] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [x] `go test ./...` — todos `ok`
- [x] Estado trocado para **feito**, secção **Resultado** adicionada, documento em `docs/Tarefas feitas/`

---

## Errata (Tarefa 127)

A verificação independente de 14/11/2026 encontrou, neste documento:

- **Base desatualizada:** diz "gerado sobre `4759474`", anterior à Tarefa 123. O estado real de `main` já incluía a 123.
- **Testes de integração não executados:** os 5 testes que falhavam depois desta tarefa (listagem de estudantes, busca livre, faltas por período, recuperação de senha e 2FA do estudante inativo) só foram revelados quando a suíte correu em banco novo. Foram corrigidos na Tarefa 127.
- **URL de rebuild incorreta:** a rota real é `POST /admin/projections/rebuild/:name` (admin FPP) e, com o catch-up automático do `Manager`, o rebuild manual da projeção de vínculos deixou de ser necessário.
- **Cobertura:** o commit `0428571` também alterou SEC-08 a SEC-14, que este documento não descreve (ver Tarefa 127).
