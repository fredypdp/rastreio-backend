# Tarefa 126 — Verificação Segurança SEC-01 a SEC-15: Backend Já Completo (backend)

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `02c6737`
**Entrega:** nenhum arquivo alterado (verificação apenas)
**Ordem de deploy:** nenhuma dependência — backend já contém todas as 15 SEC implementadas nas Tarefas 123, 124, 125.

---

## Contexto

A pedido do usuário ("Nova tarefa: Faça uma depuração nos dois repositórios para saber quais partes e pontos já foram corrigidos/implementados (docs/Debbugs/seguranca-e-migracao-brevo.md)"), foi feita a verificação completa das 15 SEC (SEC-01 a SEC-15) no backend. O backend já estava 100% completo — todas as SEC implementadas nas Tarefas 123, 124, 125 anteriores. Nenhuma alteração foi necessária.

---

## O que foi verificado e onde

**0 arquivos alterados + 0 novos.**

Todas as 15 SEC já implementadas no backend:

| SEC | Item | Onde implementado (Tarefa backend) |
| --- | --- | --- |
| SEC-01 | Eliminação EmailJS/SMTP → Brevo único | Tarefa 123 |
| SEC-02 | Rate limiters (GlobalRateLimit, LoginRateLimit, EmailRateLimit) removidos | Tarefa 123 |
| SEC-03 | Rotas reorganizadas: `conta` (auth) vs `protected` (validação estado) | Tarefa 123 |
| SEC-04 | Senha temporária 8 chars (crypto/rand), troca obrigatória 1º login | Tarefa 123 |
| SEC-05 | Recuperação de senha por link (opção B), não envio de senha por e-mail | Tarefa 123 |
| SEC-06 | `sessoes_validas_desde` para invalidação de sessões após reset/reenvio credenciais | Tarefa 123 |
| SEC-07 | BOOTSTRAP_TOKEN header para criação segura do 1º admin | Tarefa 123 |
| SEC-08 | Eliminação token `AcessoRestritoFinanceiro` (restrito a finanças) | Tarefa 124 |
| SEC-09 | Middleware `rotaPermitidaEstudanteInativo` para bloquear inativos | Tarefa 124 |
| SEC-10 | Projeção `estudante_vinculos` para rastrear vínculos ao longo do tempo | Tarefa 125 |
| SEC-11 | Migração `136_estudante_vinculos.sql` cria tabela `estudante_vinculos` | Tarefa 125 |
| SEC-12 | `GetEstudantePorCodigo` devolve perfil reduzido (`tipo_acesso: "anterior"`) para vínculo anterior | Tarefa 125 |
| SEC-13 | `ListarEstudantes` aceita filtro `vinculo: 'atual' \| 'anterior' \| 'todos'` | Tarefa 125 |
| SEC-14 | Forçado filtro por `codigo_academia` da academia autenticada | Tarefa 125 |
| SEC-15 | Cancelamento de serviços extras e catálogo respeita vínculo atual | Tarefa 125 |

Arquivos-chave já existentes e completos:
- `internal/handlers/estudante_handlers.go` — `ListarEstudantes` (filtro `vinculo`), `GetEstudantePorCodigo` (perfil reduzido + `periodos_vinculo`), `acessoAcademiaAoEstudante`
- `internal/projections/estudante_vinculos_projection.go` — projeção de vínculos
- `internal/middleware/auth.go` — `rotaPermitidaEstudanteInativo`, eliminação token restrito
- `migrations/136_estudante_vinculos.sql` — tabela de vínculos

---

## Validação realizada

**Baseline (`main` @ `02c6737`):**
- `gofmt -l .`: limpo
- `go build ./...`: sucesso
- `go vet ./...`: limpo
- `go test ./...`: todos `ok` (testes de integração contra PostgreSQL local descartável)

**Sem alterações** — mesma saída esperada.

---

## Como aplicar

Nenhuma ação necessária — backend já contém tudo. Apenas documentação de verificação.

---

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

Todos passam.

---

## Fora de escopo

- Frontend: implementação das partes pendentes do SEC-15 (filtro de vínculo, perfil reduzido UI, ações desabilitadas) e remoção do código de sessão financeira restrita — endereçado na Tarefa 36 do `rastreio-frontend`.

---

## Marcar como feito

- [x] **Estado:** feito, `(feito)` no título
- [x] Secção **Resultado** adicionada
- [x] Documento movido para `docs/Tarefas feitas/`

---

## Resultado

Verificação confirmada: backend já continha todas as 15 SEC (SEC-01 a SEC-15) implementadas integralmente nas Tarefas 123, 124, 125. Nenhum gap encontrado. Nenhuma alteração de código necessária. Validação completa (`gofmt`, `go build`, `go vet`, `go test`) passa sem falhas.