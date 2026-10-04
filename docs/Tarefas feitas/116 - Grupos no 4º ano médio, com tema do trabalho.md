# Tarefa 116 — Grupos no 4º ano médio, com tema do trabalho

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `519a956`
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/` do pacote, com os mesmos caminhos do repositório
**Ordem de deploy:** só **acrescenta** campos ao JSON (`tipo_agrupamento`, `tema_trabalho`) e uma coluna anulável; não remove nem renomeia nada, então o frontend atual continua a funcionar. Colocar em produção **antes** da Tarefa 23 do `rastreio-frontend`. Sem arquivos em comum com a Tarefa 117 deste repositório; só confirme que as migrations `129` e `130` estão livres.

---

## Contexto

No **4º ano do ensino médio** (ano da PAP, curso técnico) não existem turmas: os estudantes são separados em **grupos**, e cada grupo faz um trabalho com **tema** diferente. Até aqui o sistema só conhecia "turma", sem tema.

## Decisões de design

- **Não há agregado novo.** O agregado `Turma` continua a ser a fonte de verdade (mesmas rotas, mesmo `codigo_turma`, mesmo stream no ledger). Vínculo de estudantes, histórico por ano letivo, faltas, notas e matrículas já dependem de `Turma`; um agregado paralelo duplicaria tudo isso.
- **`tipo_agrupamento` é derivado**, nunca gravado: `"grupo"` quando `nivel == "4_ano_medio"`, `"turma"` nos outros. Não pode divergir do nível.
- **`tema_trabalho`** é opcional e só permitido no 4º ano médio. Espaços nas pontas são removidos; texto vazio ou em branco = sem tema; máximo **200 caracteres**.
- Fora do 4º ano médio, enviar `tema_trabalho` com texto → **400** (`tema_trabalho só é permitido em grupos do 4º ano médio`).
- Na atualização: campo **omitido** = não altera; **string vazia** = remove o tema.
- Se o nível de um grupo mudar para outro que não seja `4_ano_medio`, o tema existente é removido (o evento grava `""`).
- Eventos antigos (sem o campo) continuam a reproduzir normalmente: o tema fica `nil`.

## O que foi atualizado e onde

**5 arquivos alterados + 2 novos.**

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `migrations/129_turmas_tema_trabalho.sql` (**novo**) | — | `ALTER TABLE projection_turmas ADD COLUMN IF NOT EXISTS tema_trabalho TEXT` (anulável; linhas existentes ficam `NULL`) |
| `internal/domain/aggregates/turma.go` | `NivelGruposTrabalho` (linha 117), `TipoAgrupamentoDoNivel` (122), `normalizarTemaTrabalho` (131), eventos `TurmaCriadaEvent` (469) e `TurmaDadosAtualizadosEvent` (502), `Criar` (148), `AtualizarDados` (265), e os `case` do `Apply` | Campo `TemaTrabalho` na struct e nos dois eventos; regras do tema; **assinaturas novas**: `Criar(…, turno, temaTrabalho *string, criadoPor)` e `AtualizarDados(nivel, cursoID, turno, temaTrabalho *string, atualizadoPor)` |
| `internal/domain/aggregates/turma_test.go` | testes novos a partir da linha 59; chamadas existentes ajustadas | 8 testes unitários: tipo por nível, criar com/sem tema, tema fora do 4º ano, texto longo, atualizar/remover, mudar de nível limpa o tema, replay do ledger |
| `internal/projections/turmas_projection.go` | `TurmaDTO` (532), `handleTurmaCriada` (119), `handleTurmaAtualizada` (447) e todos os `SELECT`/`Scan` | DTO ganha `tipo_agrupamento` e `tema_trabalho` (`omitempty`); a criação grava o tema; a atualização aplica `NULLIF(BTRIM($1),'')` |
| `internal/handlers/turmas_handler.go` | `CriarTurma` (37), `AtualizarTurma` (746), `artigoAgrupamento` (19), `mensagemAgrupamentoCriado` (26) | Os dois requests aceitam `tema_trabalho`; mensagens dizem "grupo" no 4º ano médio; a resposta de criação inclui `tipo_agrupamento` |
| `internal/handlers/turmas_grupo_integration_test.go` (**novo**) | `TestIntegrationProjecaoDeGrupo…` (47), `TestIntegrationHandlersDeGrupo…` (159) | 3 testes com PostgreSQL real; um deles percorre os handlers HTTP: criar grupo com tema → ledger → projeção → atualizar → omitir não apaga → `""` remove → tema no 3º ano = 400 → turma comum continua "turma" |
| `cmd/server/turma_vinculo_estudante_integration_test.go` | 2 chamadas a `Turma.Criar` | Ajustadas à nova assinatura (novo parâmetro `nil` para o tema) |

Rotas afetadas (sem mudar o caminho): `POST /academia/turma` e `PUT /academia/turma/:codigo/dados`. Os fluxos em lote (`…/async`) reutilizam os mesmos handlers e passam a aceitar o campo.

## Validação realizada

Ambiente: Go 1.24 + PostgreSQL 16 reais, migrations aplicadas do zero. Clone novo de `main` (`2602902`), arquivos da pasta `rastreio-backend/` copiados por cima (11 alterados + 11 novos entre as Tarefas 116 e 117, 0 apagados): `gofmt -l .` vazio, `go build` e `go vet` limpos, e `go test ./...` (banco novo e 2ª execução no mesmo banco, resultados iguais):

```
ok  	spuri/cmd/server	0.027s
ok  	spuri/internal/db	0.065s
ok  	spuri/internal/domain/aggregates	0.017s
ok  	spuri/internal/finance	4.480s
ok  	spuri/internal/handlers	5.250s
ok  	spuri/internal/middleware	0.008s
ok  	spuri/internal/projections	0.010s
ok  	spuri/internal/security	0.002s
ok  	spuri/internal/services	0.048s
ok  	spuri/internal/storage	0.008s
ok  	spuri/internal/utils	0.009s
```

Com `SPURI_RUN_DB_INTEGRITY_TESTS=1` em banco isolado, `TestTurmaVinculo01…11` (arquivo que esta tarefa tocou) passam: **11 PASS**, e o pacote `cmd/server` inteiro dá 43 PASS (os 2 SKIP restantes já existiam).

**Ciclo reverter → falhar → restaurar** (cada regra desfeita à mão; o teste correspondente caiu; depois restaurada):

| Regra desfeita | Teste que falhou |
| --- | --- |
| G1 — projeção ignora a atualização do tema | `TestIntegrationProjecaoDeGrupoDoQuartoAnoMedio` |
| G2 — agregado não aplica o tema ao criar | `TestTurmaGrupoDoQuartoAnoMedioAceitaTemaDeTrabalho` |
| G3 — tema aceito fora do 4º ano médio | `TestTurmaTemaDeTrabalhoRejeitadoForaDoQuartoAnoMedio` |
| H1 — handler de atualização não repassa `tema_trabalho` | `TestIntegrationHandlersDeGrupoDoQuartoAnoMedioComTema` |
| H2 — handler de criação não repassa `tema_trabalho` | `TestIntegrationHandlersDeGrupoDoQuartoAnoMedioComTema` |

## Falha antiga que não é desta tarefa

Em `main`, **sem nenhuma alteração**, os testes `TestIntegrationConsultarCobrancasEstudanteEstudanteVeTodosOsEstados` e `TestIntegrationConsultarCobrancasEstudanteFiltroEstadoFailedIncluiFalhadaLocal` falham quando o pacote `internal/handlers` é executado **sozinho** (`go test ./internal/handlers/`) e passam dentro de `go test ./...`. Se aparecerem a falhar numa execução isolada, não têm relação com esta tarefa.

## Como aplicar

1. **Confirme que a base não mudou.** Os arquivos entregues foram gerados sobre o código de `main` @ `519a956` (o `main` atual, `2602902`, só acrescentou documentos de tarefa). Antes de substituir, rode na raiz do repositório:
   ```bash
   git diff --stat 519a956 HEAD -- cmd/server/turma_vinculo_estudante_integration_test.go internal/domain/aggregates/turma.go internal/domain/aggregates/turma_test.go internal/handlers/turmas_handler.go internal/projections/turmas_projection.go
   ```
   Saída **vazia** = pode substituir. Se aparecer algum arquivo, ele mudou depois da base: **não substitua** — avise para eu reconciliar.
2. **Copie** os arquivos da pasta `rastreio-backend/` do pacote para a raiz do repositório, **mantendo os caminhos**. Os arquivos são **completos**: substituem os existentes por inteiro (não é para mesclar).
3. **Verifique** (secção abaixo).
4. **Marque como feito** (secção "Marcar como feito").

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

Os três primeiros não devem imprimir nada. `go test ./...` deve mostrar todos os pacotes `ok`. Os testes `TestIntegration…` só correm de verdade com PostgreSQL de **teste descartável** (eles inserem dados): `RUN_POSTGRES_INTEGRATION=1 DATABASE_URL=postgres://… go test ./...`. Sem essas variáveis aparecem como SKIP.

## Fora de escopo

- **Não** criar agregado/rotas novos para grupos; **não** renomear `codigo_turma`, `/academia/turma…` nem nenhum campo JSON existente.
- **Não** mexer nas validações de vínculo/matrícula (`validarCompatibilidadeEstudanteTurma` e afins) nem em `configuracao_status_handlers.go` (cobertura de matérias do 4º ano).
- **Não** tocar em faltas, notas ou avaliação final — o limite de faltas é a Tarefa 117.
- **Não** corrigir os dois testes de cobranças citados acima.
- Frontend: Tarefa 23 do `rastreio-frontend` (só o painel de Turmas). As outras telas que ainda dizem "turma" no 4º ano ficam para tarefa própria.
- Entidades professor, secretário e encarregado: adiadas para outra atualização.

## Marcar como feito

1. Troque `**Estado:** pendente` por `**Estado:** feito` e coloque `(feito)` no início do título.
2. Acrescente no fim uma secção **Resultado** com um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mova este documento de `docs/Lista de Tarefas/` para `docs/Tarefas feitas/`, com o mesmo nome (a numeração não muda).

## Checklist

- [ ] Base conferida (`git diff --stat 519a956 HEAD -- …` vazio)
- [ ] Arquivos copiados (5 alterados, 2 novos) e migration `129` sem conflito de número
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test ./...` — todos `ok`
- [ ] `go.mod` e `go.sum` intactos
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`
