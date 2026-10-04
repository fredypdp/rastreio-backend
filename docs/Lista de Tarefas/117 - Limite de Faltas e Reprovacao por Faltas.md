# Tarefa 117 — Limite de faltas por período e reprovação por faltas (backend)

**Estado:** pendente

**Repositório:** https://github.com/fredypdp/rastreio-backend
**Branch base:** main (validado contra o commit `519a956`)
**Execução:** Não é necessário planejar nada. Todo o código já foi escrito e validado pelo orquestrador (ver "O que já foi validado" abaixo) e está pronto no arquivo `117 - Limite de Faltas e Reprovacao por Faltas.patch`, nesta mesma pasta. Sua única tarefa é aplicar o patch, rodar as verificações do Passo 2, e mover os arquivos conforme o Passo 3.

**Ordem de deploy:** só acrescenta rotas, uma tabela e um campo `omitempty` — o frontend atual não é afetado. Deve ir para produção **antes** da Tarefa 25 do `rastreio-frontend` (a página `/faltas/configuracoes` chama as rotas novas; sem esta tarefa ela recebe 404). Aplicar **depois** da Tarefa 116 deste repositório (migration 130 segue a 129).

---

## 0. Prompt recomendado para executar

> Aplique o patch `117 - Limite de Faltas e Reprovacao por Faltas.patch` com `git apply`, rode `gofmt -l .`, `go build ./...`, `go vet ./...` e `go test ./...`, confirme o resultado esperado descrito no Passo 2, e depois execute o Passo 3 (marcar a tarefa como feita e movê-la para `docs/Tarefas feitas/`). Não replaneje, não altere o código do patch, não abra PR.

## ⚠️ Limitações do ambiente do Codex (leia antes de verificar)

- Seu ambiente **não tem PostgreSQL, Docker nem `psql`** e bloqueia `apt` (403). **Não tente instalar nada** nem subir um banco.
- Os testes de integração (`TestIntegration…`) exigem `RUN_POSTGRES_INTEGRATION=1` + PostgreSQL. No seu ambiente eles vão aparecer como **SKIP** — isso é o esperado. **Não é falha e não é motivo para parar.** Eles já foram executados de verdade pelo orquestrador contra PostgreSQL 16 real (resultados abaixo), com a migration aplicada do zero.
- Se `go build`/`go vet` não conseguir baixar algum módulo (`golang.org/x/*`), pare e reporte exatamente o erro de rede — não altere `go.mod`/`go.sum`. Esta tarefa **não adiciona nenhuma dependência**.

---

## Contexto do pedido

A academia passa a poder definir **duas configurações separadas** (que se complementam):

1. **Limite de faltas por período** — quantas faltas um estudante pode ter **numa matéria, em cada período** (trimestre no ensino escolar, semestre no superior). Opcional.
2. **Reprovação por faltas** — liga/desliga. Só pode estar ligada se houver limite.

**Regra de cálculo (só quando as duas estão definidas):** se, numa matéria e período, o total de faltas do estudante **ultrapassar** o limite (`total > limite`; igual ao limite **não** ultrapassa), então, na **avaliação final automática**, uma nota específica dessa matéria nesse período é **lida como 0**:

| Ensino | Nota lida como 0 | Como é identificada |
| --- | --- | --- |
| Escolar (fundamental e médio) | a **nota do professor** do período excedido | categoria `nota_professor` + período (ex.: `2_trimestre`) |
| Superior | o **exame final** | categoria com código `exame_final` |

**Nenhuma outra nota é afetada** (prova trimestral, exame de recurso, nota da PAP, nota contínua do superior etc. ficam como estão).

## Decisões de design já tomadas (não precisa reavaliar)

- **Agregado novo `ConfiguracaoFaltas`, um por academia**, com ID determinístico `uuid.NewSHA1(NameSpaceOID, "spuri.faltas.configuracao."+codigo_academia)`; cada `PUT` é um novo evento `ConfiguracaoFaltasDefinida` **no mesmo agregado** (substitui a configuração inteira). Mesmo padrão já usado por `RemetenteComunicacao`.
- **Projeção** `projection_faltas_configuracao` (migration 130, com `CHECK`s: limite entre 1 e 500; reprovação exige limite), registada no `projManager` como `faltas_configuracao`.
- **Onde atua:** em `calcularResultadoMateriasAvaliacaoFinal`, **depois** de carregar as notas e **antes** de `substituirNotasAusentesPorZero`. Percorre as referências `[categoria,periodo]` da **fórmula** efetivamente usada; só zera referências cuja categoria seja a indicada na tabela acima e cujo período tenha excedido o limite. Se a nota não existia, passa a existir como `0`. Por isso a nota zerada **nunca** aparece em `notas_substituidas_zero`.
- **Rastreabilidade:** cada nota zerada é registada em `notas_zeradas_por_faltas` (`categoria`, `periodo`, `total_faltas`, `limite_faltas`) dentro do resultado por matéria (campo novo, `omitempty` — resultados antigos e academias sem a funcionalidade não mudam).
- **Contagem de faltas:** soma de `quantidade` em `projection_faltas` do estudante, na matéria, por período, **no ano letivo do cálculo**. Faltas de outra matéria, de outro ano letivo ou sem período (registos antigos) **não contam**.
- **"Reprovação" = nota 0 na entrada do cálculo**, não reprovação direta: o estudante só reprova se a média/fórmula resultante ficar abaixo do mínimo da regra. Ex.: nota do professor do 2º trimestre zerada com tudo o resto 10 → média 8,33 → reprovado com mínimo 10; mas zerar um trimestre nem sempre reprova.
- **Limite sozinho** (sem reprovação por faltas) é apenas gravado; **não altera nenhum cálculo** nesta fase.
- **As duas categorias ficam em constantes** em `internal/handlers/avaliacao_final_faltas.go` (`categoriaZeroPorFaltasEscolar = "nota_professor"`, `categoriaZeroPorFaltasSuperior = "exame_final"`). É o único ponto a alterar se o dono do produto mudar a escolha.
- **Momento do efeito:** a regra só é aplicada **quando a avaliação final é calculada**. Resultados já gravados não são recalculados quando a configuração muda nem quando novas faltas são lançadas depois.

## Quando a regra é aplicada (exemplo simples)

A avaliação final automática é calculada **quando uma nota é lançada ou corrigida** (a nota "despertadora" da regra; hoje em `RegistrarNota` e `CorrigirNota`). **Registar ou corrigir faltas não dispara cálculo nenhum.** Logo, a regra de faltas só entra em ação nesse momento de cálculo:

- *Funciona:* o limite é 5; o estudante tem 6 faltas em Matemática no 2º trimestre; depois a nota despertadora é lançada → o cálculo vê as 6 faltas e lê a nota do professor desse trimestre como 0.
- *Não recalcula sozinho:* a avaliação do estudante já foi calculada e gravada; só depois lançam-se mais faltas (ou a academia liga a regra hoje) → o resultado já gravado **não muda**. Só muda se uma nota for lançada/corrigida de novo e o cálculo correr outra vez.

Disparar recálculo ao registar/corrigir faltas (ou um botão "recalcular") **não está** nesta tarefa — é uma decisão futura do dono do produto.

## API nova

`GET /academia/faltas/configuracao` e `PUT /academia/faltas/configuracao` — somente academia autenticada (mesmo grupo de `/academia/faltas-aluno`; sem token → 401/403).

```json
// GET → 200   (academia sem configuração: limite null, reprovação false, sem atualizado_em)
{ "data": { "codigo_academia": "ABC123", "limite_faltas_por_periodo": 5, "reprovacao_por_faltas": true, "atualizado_em": "2026-10-03T10:00:00Z" } }

// PUT body
{ "limite_faltas_por_periodo": 5, "reprovacao_por_faltas": true }      // limite null = sem limite

// PUT → 200  (devolve os valores gravados; a projeção é assíncrona)
{ "message": "configuração de faltas salva com sucesso", "data": { ... } }
```

Erros `400`: `reprovacao_por_faltas` ligada sem limite; limite fora de **1 a 500**.

## O que o patch faz

**6 arquivos alterados + 9 novos** (15 no total):

| Arquivo | Mudança |
| --- | --- |
| `migrations/130_faltas_configuracao.sql` (**novo**) | tabela `projection_faltas_configuracao` + `CHECK`s |
| `internal/domain/aggregates/configuracao_faltas.go` (**novo**) | agregado, ID determinístico, evento e validações |
| `internal/domain/aggregates/aggregate.go` | `DefaultAggregateFactory` cria `ConfiguracaoFaltas` |
| `internal/db/safe_queries.go` | whitelist: `aggregate_type` `ConfiguracaoFaltas` e `event_type` `ConfiguracaoFaltasDefinida` |
| `internal/domain/aggregates/estudante_avaliacao.go` | `NotaZeradaPorFaltas` + campo `NotasZeradasPorFaltas` em `ResultadoMateriaAvaliacaoFinal` |
| `internal/projections/faltas_configuracao_projection.go` (**novo**) | projeção + `GetByAcademia` |
| `internal/projections/faltas_projection.go` | `SomarPorPeriodo(estudante, academia, anoLectivo, materiaID)` |
| `internal/handlers/faltas_configuracao_handlers.go` (**novo**) | `GetConfiguracaoFaltas`, `DefinirConfiguracaoFaltas` |
| `internal/handlers/avaliacao_final_faltas.go` (**novo**) | constantes das categorias + `aplicarZeroPorFaltas` |
| `internal/handlers/avaliacao_final_handler.go` | chamada em `calcularResultadoMateriasAvaliacaoFinal` |
| `cmd/server/main.go` | `RegisterProjection("faltas_configuracao", …)` + 2 rotas |
| `internal/domain/aggregates/configuracao_faltas_test.go` (**novo**) | 9 testes unitários |
| `internal/handlers/avaliacao_final_faltas_test.go` (**novo**) | 7 testes unitários |
| `internal/handlers/faltas_configuracao_integration_test.go` (**novo**) | 3 testes de integração (PostgreSQL real) |
| `cmd/server/faltas_configuracao_rotas_test.go` (**novo**) | 1 teste: rotas registadas e protegidas |

## O que já foi validado pelo orquestrador

Ambiente: Go 1.24 + PostgreSQL 16 reais, `RUN_POSTGRES_INTEGRATION=1`, migrations aplicadas do zero.

**Baseline** (`main` @ `519a956`): `gofmt`, `build`, `vet` limpos; `go test ./...` tudo `ok`.

**Com os patches 116 + 117** aplicados em clone novo e limpo de `main`, exatamente como o Codex fará: `gofmt -l internal cmd` vazio, `go build` e `go vet` limpos, e (banco novo, depois 2ª execução no mesmo banco — ambas iguais):

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

**Cenário de ponta a ponta (PostgreSQL real)** — `TestIntegrationAvaliacaoFinalZeraNotaDoProfessorPorFaltas`, 7° ano, 3 trimestres com todas as notas 10, limite 5:

| Situação | Resultado |
| --- | --- |
| Sem configuração, 6 faltas no 2º trimestre | média 10, aprovado |
| Só o limite (reprovação desligada) | média 10, aprovado |
| Limite 5 + reprovação, 6 faltas (4+2) no 2º trimestre | `nota_professor`/`2_trimestre` zerada → média 8,33, reprovado; registo `{total_faltas:6, limite_faltas:5}` |
| Exatamente 5 faltas | média 10, aprovado (igual ao limite não ultrapassa) |
| Faltas de outra matéria / outro ano letivo | não contam |
| Faltas no 3º trimestre | só o 3º trimestre é zerado |
| Reprovação desligada de novo | volta a média 10 |

**Ciclo reverter → falhar → reaplicar** (cada regra desfeita à mão; o teste correspondente caiu; depois restaurada e tudo voltou a passar):

| Mutação (regra desfeita) | Teste que falhou |
| --- | --- |
| F1 — cálculo ignora o zero por faltas | `TestIntegrationAvaliacaoFinalZeraNotaDoProfessorPorFaltas` |
| F2 — limite exato passa a zerar (`<` em vez de `<=`) | `TestAplicarZeroPorFaltasNoLimiteExatoNaoZera` (+ integração) |
| F3 — reprovação sem limite passa a ser aceita | `TestConfiguracaoFaltasReprovacaoExigeLimite`, `TestIntegrationConfiguracaoFaltasPutEGet` |
| F4 — soma de faltas ignora o ano letivo | `TestIntegrationAvaliacaoFinalZeraNotaDoProfessorPorFaltas` |
| F5 — categoria escolar errada (`prova_trimestral`) | `TestCategoriaZeroPorFaltas` (+ integração) |
| F6 — zero aplicado com a reprovação desligada | `TestIntegrationAvaliacaoFinalZeraNotaDoProfessorPorFaltas` |
| F7 — rota `PUT` não registada | `TestRotasConfiguracaoFaltasExistemEExigemAutenticacao` |

## Falhas pré-existentes (não são desta tarefa)

Medido pelo orquestrador em `main` **sem** nenhum patch: `TestIntegrationConsultarCobrancasEstudanteEstudanteVeTodosOsEstados` e `TestIntegrationConsultarCobrancasEstudanteFiltroEstadoFailedIncluiFalhadaLocal` falham quando o pacote `internal/handlers` é executado **sozinho** (`go test ./internal/handlers/`) contra o banco de teste, e passam dentro de `go test ./...`. Acontece igual em `main` puro. Se algum dia vir esses dois nomes falharem, **não os atribua a esta tarefa**. (No ambiente do Codex, sem PostgreSQL, eles aparecem como SKIP.)

## Passo 1 — Aplicar o patch

**Pré-requisito:** a Tarefa 116 já aplicada (existe `migrations/129_turmas_tema_trabalho.sql`). Na raiz do repositório:

```bash
git apply "docs/Lista de Tarefas/117 - Limite de Faltas e Reprovacao por Faltas.patch"
```

Deve alterar 6 arquivo(s) e criar 9 novo(s). Se `git apply` falhar (inclusive porque `migrations/130_…` já exista), **PARE** — não recrie as mudanças manualmente, reporte o conflito ao orquestrador, que renumera.

## Passo 2 — Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

Os três primeiros devem terminar sem nenhuma saída/erro. `go test ./...` deve mostrar todos os pacotes `ok`. Correm de verdade no seu ambiente (não precisam de PostgreSQL): os 9 testes de `configuracao_faltas_test.go`, os 7 de `avaliacao_final_faltas_test.go` e `TestRotasConfiguracaoFaltasExistemEExigemAutenticacao`. Os 3 `TestIntegrationConfiguracaoFaltas…`/`TestIntegrationAvaliacaoFinalZeraNota…` aparecem como SKIP.

## O que NÃO fazer (fora de escopo)

- **Não** recalcular avaliações finais já gravadas, **nem** disparar recálculo ao registar faltas ou ao mudar a configuração.
- **Não** bloquear nem alertar no lançamento de faltas quando o limite é ultrapassado (sem resumo/indicador de "limite excedido" nesta tarefa).
- **Não** alterar `substituirNotasAusentesPorZero`, a fórmula, o mínimo de aprovação nem as regras de avaliação final.
- **Não** mudar `nota_professor`/`exame_final` para outras categorias, nem criar configuração por matéria, por curso ou por tipo de ensino — o limite é **um único valor por academia**.
- **Não** corrigir os dois testes de cobranças citados em "Falhas pré-existentes".
- Frontend: coberto pelas Tarefas 24 e 25 do `rastreio-frontend`.

## Passo 3 — Marcar como feito

Depois que os Passos 1–2 passarem sem problema:

1. Neste documento, troque a linha `**Estado:** pendente` por `**Estado:** feito` e coloque `(feito)` no início do título (`# (feito) Tarefa 117 — …`).
2. Acrescente, ao final, uma secção **Resultado** com um parágrafo curto descrevendo o que foi efetivamente feito e qualquer desvio pontual.
3. Mova este `.md` **e** o `.patch` de `docs/Lista de Tarefas/` para `docs/Tarefas feitas/`, **mantendo o mesmo nome** (a numeração 117 não muda).
4. Não renumere nem altere nenhuma outra tarefa. Não abra PR nem faça merge.

## Resumo das mudanças (checklist final)

- [ ] Tarefa 116 já aplicada antes desta
- [ ] Patch aplicado (`git apply`) sem conflitos (6 arquivos alterados, 9 novos)
- [ ] `gofmt -l .` sem nenhuma saída
- [ ] `go build ./...` limpo
- [ ] `go vet ./...` limpo
- [ ] `go test ./...` — todos `ok`, `TestIntegration…` como SKIP no ambiente do Codex (já validados com PostgreSQL real)
- [ ] `go.mod`/`go.sum` intactos
- [ ] Estado trocado para **feito**, título com `(feito)`, secção **Resultado** adicionada
- [ ] `.md` e `.patch` movidos para `docs/Tarefas feitas/` com o mesmo nome
