# Tarefa 117 — Limite de faltas por período e reprovação por faltas

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `519a956`
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/` do pacote, com os mesmos caminhos do repositório
**Ordem de deploy:** só acrescenta rotas, uma tabela e um campo `omitempty`; o frontend atual não é afetado. Colocar em produção **antes** da Tarefa 25 do `rastreio-frontend` (a página `/faltas/configuracoes` chama as rotas novas; sem esta tarefa ela recebe 404). Sem arquivos em comum com a Tarefa 116; só confirme que a migration `130` está livre.

---

## Contexto

A academia passa a poder definir **duas configurações separadas** (que se complementam):

1. **Limite de faltas por período** (opcional): quantas faltas um estudante pode ter **numa matéria, em cada período** (trimestre no ensino escolar, semestre no superior).
2. **Reprovação por faltas** (liga/desliga): só pode estar ligada se houver limite.

**Regra de cálculo, só quando as duas estão definidas:** se, numa matéria e período, o total de faltas do estudante **ultrapassar** o limite (`total > limite`; igual ao limite **não** ultrapassa), então, na **avaliação final automática**, uma nota dessa matéria nesse período é **lida como 0**:

| Ensino | Nota lida como 0 | Como é identificada |
| --- | --- | --- |
| Escolar (fundamental e médio) | a **nota do professor** do período excedido | categoria `nota_professor` + período (ex.: `2_trimestre`) |
| Superior | o **exame final** | categoria com o código `exame_final` |

Nenhuma outra nota é afetada (prova trimestral, exame de recurso, nota da PAP, nota contínua do superior…).

## Quando a regra é aplicada

A avaliação final automática é **tentada** quando uma nota é lançada ou corrigida (a nota "despertadora" da regra), mas **só é calculada se ainda não existir avaliação gravada** para aquele estudante, ano letivo e tipo de regra. **Registar ou corrigir faltas não dispara cálculo nenhum.** Depois de gravada, uma avaliação **nunca é recalculada**: nem por novas faltas, nem por corrigir uma nota, nem por mudar a configuração (o backend ignora a regra quando já existe resultado para aquele ano e tipo, e o agregado também bloqueia a duplicidade). Logo, a regra de faltas só entra em ação no momento em que a avaliação é calculada pela primeira vez:

- *Funciona:* limite 5; o estudante tem 6 faltas em Matemática no 2º trimestre; depois a nota despertadora é lançada → o cálculo vê as 6 faltas e lê a nota do professor desse trimestre como 0.
- *Não recalcula:* a avaliação já foi calculada e gravada; só depois lançam mais faltas, a academia liga a regra hoje ou uma nota é corrigida → o resultado já gravado **não muda**.

Cada etapa da cadeia de avaliação (por exemplo, final, com exame e com recurso) é calculada uma vez, e a regra de faltas atua em cada etapa quando ela é calculada.

Recalcular avaliações já gravadas, ao registar/corrigir faltas ou por um botão "recalcular", **não está** nesta tarefa e seria uma decisão futura.

## Decisões de design

- **Agregado novo `ConfiguracaoFaltas`, um por academia**, com ID determinístico (`uuid.NewSHA1(NameSpaceOID, "spuri.faltas.configuracao."+codigo_academia)`); cada `PUT` é um novo evento `ConfiguracaoFaltasDefinida` **no mesmo agregado**, substituindo a configuração inteira. Mesmo padrão já usado por `RemetenteComunicacao`.
- **Projeção** `projection_faltas_configuracao` (migration 130, com `CHECK`s: limite entre 1 e 500; reprovação exige limite), registada no gestor de projeções como `faltas_configuracao`.
- **Onde atua:** em `calcularResultadoMateriasAvaliacaoFinal`, **depois** de carregar as notas e **antes** de `substituirNotasAusentesPorZero`. Percorre as referências `[categoria,periodo]` da **fórmula** usada; só zera referências da categoria indicada na tabela acima cujo período excedeu o limite. Se a nota não existia, passa a existir como `0` (por isso nunca aparece em `notas_substituidas_zero`).
- **Rastreabilidade:** cada nota zerada vai em `notas_zeradas_por_faltas` (`categoria`, `periodo`, `total_faltas`, `limite_faltas`) no resultado por matéria — campo novo `omitempty`; resultados antigos e academias sem a funcionalidade não mudam.
- **Contagem de faltas:** soma de `quantidade` em `projection_faltas` do estudante, na matéria, por período, **no ano letivo do cálculo**. Faltas de outra matéria, de outro ano letivo ou sem período (registos antigos) **não contam**.
- **"Reprovação" = nota 0 na entrada do cálculo**, não reprovação direta: o estudante só reprova se a média resultante ficar abaixo do mínimo da regra. Ex.: nota do professor do 2º trimestre zerada e o resto 10 → média 8,33 → reprovado com mínimo 10; mas zerar um trimestre nem sempre reprova.
- **Limite sozinho** (sem reprovação por faltas) é apenas gravado; não altera nenhum cálculo.
- **As duas categorias ficam em constantes** (`categoriaZeroPorFaltasEscolar`, `categoriaZeroPorFaltasSuperior`) — único ponto a alterar se a escolha mudar.

## API nova

`GET /academia/faltas/configuracao` e `PUT /academia/faltas/configuracao` — somente academia autenticada (mesmo grupo de `/academia/faltas-aluno`; sem token → 401/403).

```json
// GET → 200 (academia sem configuração: limite null, reprovação false)
{ "data": { "codigo_academia": "ABC123", "limite_faltas_por_periodo": 5, "reprovacao_por_faltas": true, "atualizado_em": "2026-10-03T10:00:00Z" } }

// PUT, corpo (limite null = sem limite)
{ "limite_faltas_por_periodo": 5, "reprovacao_por_faltas": true }

// PUT → 200 (devolve os valores gravados; a projeção é assíncrona)
{ "message": "configuração de faltas salva com sucesso", "data": { … } }
```

Erros `400`: reprovação ligada sem limite; limite fora de **1 a 500**.

## O que foi atualizado e onde

**6 arquivos alterados + 9 novos.**

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `migrations/130_faltas_configuracao.sql` (**novo**) | — | Tabela `projection_faltas_configuracao` com os `CHECK`s |
| `internal/domain/aggregates/configuracao_faltas.go` (**novo**) | `ConfiguracaoFaltasAggregateID` (linha 18), `ConfiguracaoFaltas` (27), `Definir` (79) | Agregado, ID determinístico, evento e validações |
| `internal/domain/aggregates/aggregate.go` | `case "ConfiguracaoFaltas"` (linha 149) | A fábrica passa a criar o agregado |
| `internal/db/safe_queries.go` | `event_type` (linha 201), `aggregate_type` (233) | Whitelists: `ConfiguracaoFaltasDefinida` e `ConfiguracaoFaltas` |
| `internal/domain/aggregates/estudante_avaliacao.go` | `NotaZeradaPorFaltas` (linha 49), campo `NotasZeradasPorFaltas` (66) | Registo de nota zerada no resultado por matéria |
| `internal/projections/faltas_configuracao_projection.go` (**novo**) | `ConfiguracaoFaltasDTO` (linha 71), `GetByAcademia` (105) | Projeção da configuração |
| `internal/projections/faltas_projection.go` | `SomarPorPeriodo` (linha 344) | Soma de faltas por período para estudante + matéria + ano letivo |
| `internal/handlers/faltas_configuracao_handlers.go` (**novo**) | `GetConfiguracaoFaltas` (linha 22), `DefinirConfiguracaoFaltas` (45) | Os dois endpoints |
| `internal/handlers/avaliacao_final_faltas.go` (**novo**) | constantes das categorias (linha 18), `aplicarZeroPorFaltas` (33) | Lógica de zerar |
| `internal/handlers/avaliacao_final_handler.go` | leitura da configuração (linha 801) e chamada a `aplicarZeroPorFaltas` (838), dentro de `calcularResultadoMateriasAvaliacaoFinal` | Aplica o zero antes de `substituirNotasAusentesPorZero` |
| `cmd/server/main.go` | registo da projeção (linha 175), rotas (582) | `RegisterProjection("faltas_configuracao", …)` e as 2 rotas no grupo `academia` |
| `internal/domain/aggregates/configuracao_faltas_test.go` (**novo**) | — | 9 testes unitários |
| `internal/handlers/avaliacao_final_faltas_test.go` (**novo**) | — | 7 testes unitários |
| `internal/handlers/faltas_configuracao_integration_test.go` (**novo**) | — | 3 testes de integração (PostgreSQL real) |
| `cmd/server/faltas_configuracao_rotas_test.go` (**novo**) | `TestRotasConfiguracaoFaltas…` (linha 12) | Rotas registadas e protegidas |

## Validação realizada

Mesmo ambiente e mesmo clone da Tarefa 116 (os arquivos das duas tarefas foram copiados juntos): `gofmt`, `build`, `vet` limpos; `go test ./...` todos `ok` (banco novo e 2ª execução no mesmo banco):

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

**Cenário de ponta a ponta (PostgreSQL real)** — `TestIntegrationAvaliacaoFinalZeraNotaDoProfessorPorFaltas`: 7º ano, 3 trimestres com todas as notas 10, limite 5:

| Situação | Resultado |
| --- | --- |
| Sem configuração, 6 faltas no 2º trimestre | média 10, aprovado |
| Só o limite (reprovação desligada) | média 10, aprovado |
| Limite 5 + reprovação, 6 faltas (4+2) no 2º trimestre | `nota_professor`/`2_trimestre` zerada → média 8,33, reprovado; registo `{total_faltas:6, limite_faltas:5}` |
| Exatamente 5 faltas | média 10, aprovado |
| Faltas de outra matéria / outro ano letivo | não contam |
| Faltas no 3º trimestre | só o 3º trimestre é zerado |
| Reprovação desligada de novo | volta a média 10 |

**Ciclo reverter → falhar → restaurar:**

| Regra desfeita | Teste que falhou |
| --- | --- |
| F1 — cálculo ignora o zero por faltas | `TestIntegrationAvaliacaoFinalZeraNotaDoProfessorPorFaltas` |
| F2 — limite exato passa a zerar (`<` em vez de `<=`) | `TestAplicarZeroPorFaltasNoLimiteExatoNaoZera` |
| F3 — reprovação sem limite passa a ser aceita | `TestConfiguracaoFaltasReprovacaoExigeLimite` |
| F4 — soma de faltas ignora o ano letivo | `TestIntegrationAvaliacaoFinalZeraNotaDoProfessorPorFaltas` |
| F5 — categoria escolar errada (`prova_trimestral`) | `TestCategoriaZeroPorFaltas` |
| F6 — zero aplicado com a reprovação desligada | `TestIntegrationAvaliacaoFinalZeraNotaDoProfessorPorFaltas` |
| F7 — rota `PUT` não registada | `TestRotasConfiguracaoFaltasExistemEExigemAutenticacao` |

## Falha antiga que não é desta tarefa

Em `main`, **sem nenhuma alteração**, os testes `TestIntegrationConsultarCobrancasEstudanteEstudanteVeTodosOsEstados` e `TestIntegrationConsultarCobrancasEstudanteFiltroEstadoFailedIncluiFalhadaLocal` falham quando o pacote `internal/handlers` é executado **sozinho** (`go test ./internal/handlers/`) e passam dentro de `go test ./...`. Se aparecerem a falhar numa execução isolada, não têm relação com esta tarefa.

## Como aplicar

1. **Confirme que a base não mudou.** Os arquivos entregues foram gerados sobre o código de `main` @ `519a956` (o `main` atual, `2602902`, só acrescentou documentos de tarefa). Antes de substituir, rode na raiz do repositório:
   ```bash
   git diff --stat 519a956 HEAD -- cmd/server/main.go internal/db/safe_queries.go internal/domain/aggregates/aggregate.go internal/domain/aggregates/estudante_avaliacao.go internal/handlers/avaliacao_final_handler.go internal/projections/faltas_projection.go
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

- **Não** recalcular avaliações finais já gravadas, nem disparar recálculo ao registar faltas ou ao mudar a configuração.
- **Não** bloquear nem alertar no lançamento de faltas quando o limite é ultrapassado (sem indicador de "limite excedido").
- **Não** alterar `substituirNotasAusentesPorZero`, a fórmula, o mínimo de aprovação nem as regras de avaliação final.
- **Não** mudar `nota_professor`/`exame_final` para outras categorias, nem criar configuração por matéria, curso ou tipo de ensino — o limite é **um único valor por academia**.
- **Não** corrigir os dois testes de cobranças citados acima.
- Frontend: Tarefas 24 e 25 do `rastreio-frontend`.

## Marcar como feito

1. Troque `**Estado:** pendente` por `**Estado:** feito` e coloque `(feito)` no início do título.
2. Acrescente no fim uma secção **Resultado** com um parágrafo curto do que foi efetivamente feito e qualquer desvio.
3. Mova este documento de `docs/Lista de Tarefas/` para `docs/Tarefas feitas/`, com o mesmo nome (a numeração não muda).

## Checklist

- [ ] Base conferida (`git diff --stat 519a956 HEAD -- …` vazio)
- [ ] Arquivos copiados (6 alterados, 9 novos) e migration `130` sem conflito de número
- [ ] `gofmt -l .` vazio; `go build` e `go vet` limpos
- [ ] `go test ./...` — todos `ok`
- [ ] `go.mod` e `go.sum` intactos
- [ ] Estado trocado para **feito**, secção **Resultado** adicionada, documento movido para `docs/Tarefas feitas/`
