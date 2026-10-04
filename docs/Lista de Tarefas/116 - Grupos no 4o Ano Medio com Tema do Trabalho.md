# Tarefa 116 — Grupos no 4º ano médio, com tema do trabalho (backend)

**Estado:** pendente

**Repositório:** https://github.com/fredypdp/rastreio-backend
**Branch base:** main (validado contra o commit `519a956`)
**Execução:** Não é necessário planejar nada. Todo o código já foi escrito e validado pelo orquestrador (ver "O que já foi validado" abaixo) e está pronto no arquivo `116 - Grupos no 4o Ano Medio com Tema do Trabalho.patch`, nesta mesma pasta. Sua única tarefa é aplicar o patch, rodar as verificações do Passo 2, e mover os arquivos conforme o Passo 3.

**Ordem de deploy:** esta tarefa só **acrescenta** campos ao JSON (`tipo_agrupamento`, `tema_trabalho`) e uma coluna anulável — não remove nem renomeia nada, então o frontend atual continua a funcionar. Deve ir para produção **antes** da Tarefa 23 do `rastreio-frontend` (que passa a enviar/ler `tema_trabalho`). Aplicar **antes** da Tarefa 117 deste repositório (a migration 129 desta tarefa precede a 130 da 117).

---

## 0. Prompt recomendado para executar

> Aplique o patch `116 - Grupos no 4o Ano Medio com Tema do Trabalho.patch` com `git apply`, rode `gofmt -l .`, `go build ./...`, `go vet ./...` e `go test ./...`, confirme o resultado esperado descrito no Passo 2, e depois execute o Passo 3 (marcar a tarefa como feita e movê-la para `docs/Tarefas feitas/`). Não replaneje, não altere o código do patch, não abra PR.

## ⚠️ Limitações do ambiente do Codex (leia antes de verificar)

- Seu ambiente **não tem PostgreSQL, Docker nem `psql`** e bloqueia `apt` (403). **Não tente instalar nada** nem subir um banco.
- Os testes de integração (`TestIntegration…`) exigem `RUN_POSTGRES_INTEGRATION=1` + PostgreSQL. No seu ambiente eles vão aparecer como **SKIP** — isso é o esperado. **Não é falha e não é motivo para parar.** Eles já foram executados de verdade pelo orquestrador contra PostgreSQL 16 real (resultados abaixo), com a migration aplicada do zero.
- Se `go build`/`go vet` não conseguir baixar algum módulo (`golang.org/x/*`), pare e reporte exatamente o erro de rede — não altere `go.mod`/`go.sum`. Esta tarefa **não adiciona nenhuma dependência**.

---

## Contexto do pedido

No **4º ano do ensino médio** (ano da PAP, curso técnico) já não existem turmas: os estudantes são separados em **grupos**, e cada grupo tem um trabalho com um **tema** diferente. Hoje o sistema só conhece "turma", sem tema.

### Decisão de arquitetura (já tomada)

**Não foi criado um agregado novo.** O agregado `Turma` continua a ser a fonte de verdade, com as mesmas rotas, o mesmo `codigo_turma`, o mesmo stream no ledger. Motivo: vínculo de estudantes, histórico por ano letivo, faltas, notas, avaliações e matrículas já dependem de `Turma`/`codigo_turma`; um agregado paralelo duplicaria tudo isso e quebraria esses fluxos. O que muda é só:

1. **`tipo_agrupamento`** — campo **derivado** do nível (`"grupo"` quando `nivel == "4_ano_medio"`, `"turma"` nos outros). Não é gravado em lado nenhum, logo nunca pode divergir do nível.
2. **`tema_trabalho`** — texto opcional, **só permitido** quando o nível é `4_ano_medio`.
3. Mensagens da API passam a dizer "grupo" nesse nível.

### Regras do tema (já decididas)

- Entra em `POST /academia/turma` (criar) e `PUT /academia/turma/:codigo/dados` (atualizar; atendido por `AtualizarDadosTurma` → `AtualizarTurma`) como `tema_trabalho`. Os fluxos em lote (`…/async`) reutilizam os mesmos handlers e passam a aceitar o campo.
- Espaços nas pontas são removidos; texto vazio/em branco = sem tema; máximo **200 caracteres**.
- Fora do 4º ano médio, enviar `tema_trabalho` com texto → **400** (`tema_trabalho só é permitido em grupos do 4º ano médio`).
- Na atualização: campo **omitido** = não altera; **string vazia** = remove o tema.
- Se o nível de um grupo mudar para outro que não seja `4_ano_medio`, o tema existente é removido automaticamente (o evento grava `""`).
- Eventos antigos (sem o campo) continuam a reproduzir normalmente: o campo fica `nil`.

## O que o patch faz

**5 arquivos alterados + 2 novos** (7 no total):

| Arquivo | Mudança |
| --- | --- |
| `migrations/129_turmas_tema_trabalho.sql` (**novo**) | `ALTER TABLE projection_turmas ADD COLUMN IF NOT EXISTS tema_trabalho TEXT;` (anulável; linhas existentes ficam `NULL`) |
| `internal/domain/aggregates/turma.go` | constante `NivelGruposTrabalho`; `TipoAgrupamentoDoNivel`; `normalizarTemaTrabalho`; campo `TemaTrabalho` na struct e nos eventos `TurmaCriada`/`TurmaDadosAtualizados`; **assinaturas novas**: `Criar(..., turno, temaTrabalho *string, criadoPor)` e `AtualizarDados(nivel, cursoID, turno, temaTrabalho *string, atualizadoPor)`; `Apply` atualizado |
| `internal/domain/aggregates/turma_test.go` | chamadas existentes ajustadas à nova assinatura + **8 testes unitários novos** |
| `internal/projections/turmas_projection.go` | `TurmaDTO` ganha `tipo_agrupamento` e `tema_trabalho` (`omitempty`); todos os `SELECT`/`Scan` incluem `tema_trabalho`; `handleTurmaCriada` grava; `handleTurmaAtualizada` atualiza (`NULLIF(BTRIM($1),'')`) |
| `internal/handlers/turmas_handler.go` | requests de criar/atualizar aceitam `tema_trabalho`; mensagens "grupo criado/atualizado com sucesso" no 4º ano médio; resposta de criação inclui `tipo_agrupamento` |
| `internal/handlers/turmas_grupo_integration_test.go` (**novo**) | 3 testes de integração com PostgreSQL real: 2 de projeção + 1 que percorre os **handlers HTTP reais** (criar grupo com tema → ledger → projeção → atualizar tema → omitir tema não apaga → `""` remove → tema no 3º ano = 400 → turma comum continua "turma") |
| `cmd/server/turma_vinculo_estudante_integration_test.go` | 2 chamadas a `Turma.Criar` ajustadas à nova assinatura (`nil` no tema) |

## O que já foi validado pelo orquestrador

Ambiente: Go 1.24 + PostgreSQL 16 reais, `RUN_POSTGRES_INTEGRATION=1`, todas as migrations aplicadas do zero.

**Baseline** (`main` @ `519a956`, sem o patch): `gofmt -l` vazio, `go build`, `go vet` limpos, `go test ./...` tudo `ok` (banco novo e 2ª execução no mesmo banco).

**Com o patch** (clone novo e limpo de `main`, patch aplicado com `git apply` exatamente como o Codex fará, junto com o 117):

```
ok  	spuri/cmd/server	0.022s
ok  	spuri/internal/db	0.058s
ok  	spuri/internal/domain/aggregates	0.018s
ok  	spuri/internal/finance	4.043s
ok  	spuri/internal/handlers	5.265s
ok  	spuri/internal/middleware	0.009s
ok  	spuri/internal/projections	0.009s
ok  	spuri/internal/security	0.002s
ok  	spuri/internal/services	0.015s
ok  	spuri/internal/storage	0.007s
ok  	spuri/internal/utils	0.009s
```

**Ciclo reverter → falhar → reaplicar** (cada regra foi desfeita à mão e o teste correspondente caiu; depois restaurada e tudo voltou a passar):

| Mutação (regra desfeita) | Teste que falhou |
| --- | --- |
| G1 — projeção ignora a atualização do tema | `TestIntegrationProjecaoDeGrupoDoQuartoAnoMedio` |
| G2 — agregado não aplica o tema ao criar | `TestTurmaGrupoDoQuartoAnoMedioAceitaTemaDeTrabalho` |
| G3 — tema aceito fora do 4º ano médio | `TestTurmaTemaDeTrabalhoRejeitadoForaDoQuartoAnoMedio` |
| H1 — handler de atualização não repassa `tema_trabalho` | `TestIntegrationHandlersDeGrupoDoQuartoAnoMedioComTema` ("projeção após atualizar tema") |
| H2 — handler de criação não repassa `tema_trabalho` | `TestIntegrationHandlersDeGrupoDoQuartoAnoMedioComTema` ("tema no evento TurmaCriada = nil") |

## Falhas pré-existentes (não são desta tarefa)

Medido pelo orquestrador em `main` **sem** nenhum patch: `TestIntegrationConsultarCobrancasEstudanteEstudanteVeTodosOsEstados` e `TestIntegrationConsultarCobrancasEstudanteFiltroEstadoFailedIncluiFalhadaLocal` falham quando o pacote `internal/handlers` é executado **sozinho** (`go test ./internal/handlers/`) contra o banco de teste, e passam dentro de `go test ./...`. Acontece igual em `main` puro. Se algum dia vir esses dois nomes falharem, **não os atribua a esta tarefa**. (No ambiente do Codex, sem PostgreSQL, eles aparecem como SKIP.)

## Passo 1 — Aplicar o patch

Na raiz do repositório:

```bash
git apply "docs/Lista de Tarefas/116 - Grupos no 4o Ano Medio com Tema do Trabalho.patch"
```

Deve alterar 5 arquivo(s) e criar 2 novo(s). Se `git apply` falhar (inclusive porque `migrations/129_…` já exista), **PARE** — não recrie as mudanças manualmente, reporte o conflito ao orquestrador, que renumera.

## Passo 2 — Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

Os três primeiros devem terminar sem nenhuma saída/erro. `go test ./...` deve mostrar todos os pacotes `ok` (os `TestIntegration…` como SKIP no seu ambiente). Os 8 testes unitários novos correm de verdade (os 3 `TestIntegrationProjecaoDe…`/`TestIntegrationHandlersDeGrupo…` aparecem como SKIP):
`TestTipoAgrupamentoDoNivel`, `TestTurmaGrupoDoQuartoAnoMedioAceitaTemaDeTrabalho`, `TestTurmaGrupoDoQuartoAnoMedioPermiteCriarSemTema`, `TestTurmaTemaDeTrabalhoRejeitadoForaDoQuartoAnoMedio`, `TestTurmaTemaDeTrabalhoRejeitaTextoMuitoLongo`, `TestTurmaAtualizarTemaDoGrupo`, `TestTurmaMudarNivelParaForaDoQuartoAnoLimpaTema`, `TestTurmaTemaDeTrabalhoSobreviveAoReplayDoLedger`.

## O que NÃO fazer (fora de escopo)

- **Não** criar agregado/rotas novos para grupos; **não** renomear `codigo_turma`, `/academia/turma…` nem nenhum campo JSON existente.
- **Não** mexer em validações de vínculo/matrícula (`validarCompatibilidadeEstudanteTurma` e afins) nem em `configuracao_status_handlers.go` (cobertura de matérias do 4º ano).
- **Não** tocar em faltas, notas ou avaliação final — o limite de faltas é a Tarefa 117.
- **Não** corrigir os dois testes de cobranças citados em "Falhas pré-existentes".
- Frontend: coberto pela Tarefa 23 do `rastreio-frontend` (só o painel de Turmas). As outras telas que ainda dizem "turma" no 4º ano ficam para tarefa própria.
- Entidades professor / secretário / encarregado: adiadas pelo dono do produto para outra atualização.

## Passo 3 — Marcar como feito

Depois que os Passos 1–2 passarem sem problema:

1. Neste documento, troque a linha `**Estado:** pendente` por `**Estado:** feito` e coloque `(feito)` no início do título (`# (feito) Tarefa 116 — …`).
2. Acrescente, ao final, uma secção **Resultado** com um parágrafo curto descrevendo o que foi efetivamente feito e qualquer desvio pontual.
3. Mova este `.md` **e** o `.patch` de `docs/Lista de Tarefas/` para `docs/Tarefas feitas/`, **mantendo o mesmo nome** (a numeração 116 não muda).
4. Não renumere nem altere nenhuma outra tarefa. Não abra PR nem faça merge.

## Resumo das mudanças (checklist final)

- [ ] Patch aplicado (`git apply`) sem conflitos (5 arquivos alterados, 2 novos)
- [ ] `gofmt -l .` sem nenhuma saída
- [ ] `go build ./...` limpo
- [ ] `go vet ./...` limpo
- [ ] `go test ./...` — todos `ok`, `TestIntegration…` como SKIP no ambiente do Codex (já validados com PostgreSQL real)
- [ ] `go.mod`/`go.sum` intactos
- [ ] Estado trocado para **feito**, título com `(feito)`, secção **Resultado** adicionada
- [ ] `.md` e `.patch` movidos para `docs/Tarefas feitas/` com o mesmo nome
