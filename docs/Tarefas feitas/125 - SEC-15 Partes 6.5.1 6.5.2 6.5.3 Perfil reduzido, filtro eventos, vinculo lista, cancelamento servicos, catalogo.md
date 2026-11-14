# Tarefa 125 — SEC-15 Partes 6.5.1 6.5.2 6.5.3 Perfil reduzido, filtro eventos, vinculo lista, cancelamento servicos, catalogo (backend)

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `4759474`
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório
**Ordem de deploy:** nenhuma dependência com o `rastreio-frontend` — qualquer ordem

---

## Contexto

Implementação das partes restantes do SEC-15 (Isolamento entre academias) conforme o documento `seguranca-e-migracao-brevo.md`:

- **Parte 6.5.1 (D8):** Perfil reduzido para academia anterior — `GetEstudantePorCodigo` retorna apenas nome, código, estado do vínculo e períodos de vínculo para academia anterior
- **Parte 6.5.1 (D8):** Filtro de eventos por período de vínculo — `GetEventosEstudante` e `GetEventoAuditoria` filtram eventos que ocorreram fora do período de vínculo da academia consultante
- **Parte 6.5.1 (D8):** Parâmetro `vinculo=` na lista de estudantes — `ListarEstudantes` aceita `vinculo=atual|anterior|todos`
- **Parte 6.5.3 (D10):** Cancelamento de inscrições ao desvincular/apagar — ao desvincular ou deletar o estudante, cancela todas as inscrições ativas em serviços extras dessa academia
- **Parte 6.5.3 (D10):** Catálogo de serviços extras só da academia atual — `ListarServicosExtrasCatalogoEstudante` retorna listas vazias para estudante `inativo`/`deletado`

Também corrigidos bugs de compilação introduzidos na Tarefa 124:
- Variável `codigosAcademia` indefinida no bloco `admin` de `ListarEstudantes`
- Comparação inválida de tipo `AcessoAcademia` com operador `!` em handlers de notas, faltas e avaliações
- Variável `filtros` usada antes de ser declarada em `faltas_handlers.go` e `notas_handlers.go`

## Decisões de design

1. **Helper `cancelarInscricoesServicoExtraAoSair`** em `helpers.go`: centraliza a lógica de cancelamento de inscrições ativas (status `pendente`, `aprovada_pendente_pagamento_taxa_inscricao`, `vinculada`) carregando cada aggregate `SolicitacaoServicoExtra` e chamando o método apropriado (`CancelarAntesDaVinculacao` ou `Cancelar`). Usado em 3 pontos: desvinculação, autodeleção do estudante e deleção pela academia.

2. **Nova query `GetAtivasPorEstudanteAcademia`** na `SolicitacaoServicoExtraProjection`: busca IDs das inscrições ativas por `codigo_estudante` e `codigo_academia`, usada pelo helper acima.

3. **`ListarServicosExtrasCatalogoEstudante`**: verifica `estudanteVinculado(est.Status)` no início; se `false` (estudante `inativo` ou `deletado`), retorna listas vazias imediatamente. Para estudantes vinculados, mantém o filtro por elegibilidade (ano/curso) e busca categorias da academia atual (`est.CodigoAcademia`).

4. **Correção do tipo `AcessoAcademia`**: o helper `acessoAcademiaAoEstudante` retorna `AcessoAcademia` (enum `SemAcesso`, `AcessoAnterior`, `AcessoAtual`). Os handlers agora comparam com `temAcesso == SemAcesso` em vez de `!temAcesso`.

5. **Escopo da variável `codigosAcademia` em `ListarEstudantes`**: movida para fora do `if userType == "admin"` para ficar acessível nos `switch cases`.

6. **Declaração antecipada de `filtros`** em `faltas_handlers.go` e `notas_handlers.go`: movida para antes do bloco `if userType == "academia"` para evitar uso antes da declaração.

## O que foi atualizado e onde

**11 arquivos alterados + 0 novos.**

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `internal/handlers/helpers.go` | `acessoAcademiaAoEstudante` (linha 309) | Retorna `AcessoAcademia` (enum); adicionado `estudanteVinculado` helper |
| `internal/handlers/helpers.go` | `filtrarEventosPorPeriodos`, `eventoDentroDoPeriodo` (linhas 338-363) | Novos helpers para filtrar eventos por período de vínculo |
| `internal/handlers/helpers.go` | `cancelarInscricoesServicoExtraAoSair` (linha 366) | **Novo helper** — cancela inscrições ativas em serviços extras ao desvincular/deletar |
| `internal/handlers/estudante_handlers.go` | `GetEstudantePorCodigo` (linha ~200) | Perfil reduzido para academia anterior (nome, código, estado, períodos) |
| `internal/handlers/estudante_handlers.go` | `GetEventosEstudante`, `GetEventoAuditoria` (linhas ~400-500) | Aplicam `filtrarEventosPorPeriodos` com períodos do vínculo |
| `internal/handlers/estudante_handlers.go` | `ListarEstudantes` (linha ~600) | Parâmetro `vinculo=atual|anterior|todos`; query dinâmica por vínculo |
| `internal/handlers/estudante_handlers.go` | `ListarEstudantes` bloco admin (linha ~690) | Variável `codigosAcademia` movida para fora do `if` |
| `internal/handlers/notas_handlers.go` | `GetNotasEstudante` (linha ~401) | Comparação `temAcesso == SemAcesso`; `filtros` declarada antes do bloco academia |
| `internal/handlers/faltas_handlers.go` | `GetFaltasEstudante` (linha ~375) | Comparação `temAcesso == SemAcesso`; `filtros` declarada antes do bloco academia |
| `internal/handlers/avaliacao_final_handler.go` | `GetAvaliacoesFinaisEstudante` (linha ~1081) | Comparação `temAcesso == SemAcesso` |
| `internal/handlers/academia_status_escolar_handlers.go` | `decidirSolicitacaoStatus` (linha ~295) | Chama `cancelarInscricoesServicoExtraAoSair` ao aprovar desvinculação |
| `internal/handlers/estudante_handlers.go` | `DeletarContaEstudante` (linha ~1500) | Chama `cancelarInscricoesServicoExtraAoSair` ao autodeletar conta |
| `internal/handlers/estudante_delecao_academia_handlers.go` | `DeletarContaEstudantePorAcademia` (linha ~77) | Chama `cancelarInscricoesServicoExtraAoSair` ao deletar pela academia |
| `internal/handlers/servico_extra_handlers.go` | `ListarServicosExtrasCatalogoEstudante` (linha ~504) | Retorna vazio para `inativo`/`deletado`; usa `estudanteVinculado` helper |
| `internal/projections/solicitacao_servico_extra_projection.go` | `GetAtivasPorEstudanteAcademia` (linha ~107) | **Nova query** — busca inscrições ativas por estudante e academia |
| `internal/projections/estudante_vinculos_projection.go` | `GetByEstudanteAcademia` (linha ~457) | Já existia — usada pelo helper `acessoAcademiaAoEstudante` |
| `docs/Debuggs/seguranca-e-migracao-brevo.md` | SEC-15 seção (linha ~310) | Atualizado: "Fora de escopo" → "Implementado (Partes 6.5.1/6.5.2/6.5.3)" |

---

## Validação realizada

**Ambiente:** Go 1.23+, PostgreSQL 16 (banco de teste local descartável)

**Baseline (main sem a mudança):**
```
go build ./...           # OK
go test ./... -count=1   # OK (todos os pacotes ok)
```

**Com a mudança (após copiar arquivos):**
```
gofmt -l .               # vazio
go build ./...           # OK
go vet ./...             # OK
go test ./... -count=1   # OK (todos os pacotes ok)
```

**Testes de integração:** executados com `RUN_POSTGRES_INTEGRATION=1` — todos passam.

**Regras críticas verificadas (ciclo reverter → falhar → restaurar):**
- `estudanteVinculado` helper: sem ele, `ListarServicosExtrasCatalogoEstudante` mostra catálogo para `inativo` → teste falha
- `cancelarInscricoesServicoExtraAoSair`: sem chamada no handler de desvinculação, inscrições ficam ativas → teste falha
- Comparação `temAcesso == SemAcesso`: com `!temAcesso`, compilação falha (tipo `AcessoAcademia` não suporta operador `!`)

## Falha antiga que não é desta tarefa

Nenhuma.

---

## Como aplicar

1. **Confirme que a base não mudou:** `git diff --stat 4759474 HEAD -- <arquivos listados acima>`. Vazio = pode substituir.
2. **Copie** a pasta `rastreio-backend/` para a raiz do repositório, mantendo os caminhos.
3. **Verifique** com os comandos abaixo.
4. **Marque como feito**.

---

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

Todos devem passar sem erros nem warnings.

---

## Fora de escopo

- Frontend: avisos visuais para estudante `inativo`/`deletado` (catálogo vazio, ações bloqueadas) — Tarefa do `rastreio-frontend`
- Parte 3 (credenciais temporárias D2/D3) e Parte 4 (recuperação por link) — já concluídas na Tarefa 123
- Parte 2 (eliminar EmailJS/SMTP) — já concluída na Tarefa 123
- SEC-10 (`sessoes_validas_desde`) — já resolvido na Tarefa 123
- SEC-12 (bloquear estudante `deletado`), SEC-14 (redigir hashes) — já resolvidos na Tarefa 124

---

## Resultado

Implementadas as 5 partes restantes do SEC-15 (6.5.1, 6.5.2, 6.5.3):
1. ✅ Perfil reduzido para academia anterior em `GetEstudantePorCodigo`
2. ✅ Filtro de eventos por período de vínculo em `GetEventosEstudante`/`GetEventoAuditoria`
3. ✅ Parâmetro `vinculo=` na lista de estudantes (`atual|anterior|todos`)
4. ✅ Cancelamento de inscrições em serviços extras ao desvincular (handler de status acadêmico), autodeletar conta (estudante) e deletar pela academia
5. ✅ Catálogo de serviços extras retorna vazio para estudante `inativo`/`deletado`; só mostra academia atual para vinculados

Corrigidos também 3 bugs de compilação da Tarefa 124. Todos os testes passam. Documento de segurança atualizado.