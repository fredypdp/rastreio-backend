# Tarefa para o Codex — Repositório `rastreio-backend` (backend)

**Repositório:** https://github.com/fredypdp/rastreio-backend
**Branch base:** main
**Execução:** Não é necessário planejar nada. Todo o código desta tarefa já foi escrito e validado pelo orquestrador (ver "O que já foi validado" abaixo) e está pronto no arquivo `109 - Modo de vigencia opcional na primeira configuracao e correcao do mes_inicio_cobranca.patch`, nesta mesma pasta. Sua única tarefa é aplicar o patch, rodar as verificações do Passo 2, e mover/renomear os arquivos conforme o Passo 4. Se o patch não aplicar de primeira (`git apply` reclamar de conflito), PARE e reporte a diferença em vez de tentar recriar as mudanças manualmente.

Esta tarefa cobre **dois problemas de backend**, ambos dentro do módulo financeiro (`internal/finance`), encontrados a partir de uma lista maior de pedidos do usuário que também incluía itens de frontend puro (ver "Fora do escopo desta tarefa" no final — nenhum deles precisa de mudança neste repositório).

---

## Parte 1 — `modo_vigencia` só deveria ser obrigatório na edição, não na criação

### Contexto do problema

Tanto a taxa de matrícula (`ConfigureMatricula`) quanto a mensalidade (`ConfigureMensalidade`) já suportam um campo `modo_vigencia`, com dois valores possíveis:

- `"cobrancas_pendentes"` — o novo valor passa a valer também para o que já está pendente (retroativo).
- `"a_partir_da_atualizacao"` — o novo valor só vale a partir de agora; o que já está pendente mantém o preço antigo.

O bug: `validateConfiguracaoMensalidade` e `validateConfiguracaoMatricula` exigiam esse campo em **toda** chamada, inclusive na primeiríssima configuração de um escopo (academia + nível + ano acadêmico + curso) — quando ainda não existe preço nenhum configurado.

O usuário apontou que isso não faz sentido: "quando se trata da criação o valor deve valer para tudo já que é primeira configuração da taxa, e quando é editado aí sim pode escolher se é retroativo ou vale a partir do momento da edição". Confirmei essa intuição lendo o próprio código, não só concordando por bom senso:

- **Mensalidade** — `resolveConfiguracao` (a função que resolve o preço de um mês, histórico ou pendente) já tem esse comentário no código: "The first configuration is the best information available for every earlier month of that academic year." Ou seja, a primeira configuração de um escopo já vale para qualquer mês, passado ou futuro, **independente** de `modo_vigencia` — esse campo só passa a ter efeito real a partir da *segunda* configuração do mesmo escopo (`resolveConfiguracaoEfetiva`, usado só para meses `pendente`, olha o `modo_vigencia` da configuração mais recente).
- **Matrícula** — `ConfigureMatricula` só dispara `reprecificarSolicitacoesMatriculaPendentes` (que reprecifica solicitações já aprovadas aguardando pagamento) quando `modo_vigencia == "cobrancas_pendentes"`. Confirmei em `internal/handlers/solicitacao_matricula_handlers.go` que uma solicitação só entra no status `aprovada_pendente_pagamento_matricula` quando `ResolveMatriculaConfiguracao` **já encontra uma configuração existente** no momento da aprovação. Ou seja: na primeira configuração de um escopo, é **impossível** já existir alguma solicitação pendente sob um preço antigo daquele escopo — `reprecificarSolicitacoesMatriculaPendentes` sempre encontraria zero linhas.

Conclusão: na primeira configuração de um escopo, `modo_vigencia` literalmente não muda nada no comportamento do sistema. Exigir essa escolha do usuário nesse momento é desnecessário e confuso — exatamente o que ele reportou.

**Um detalhe que vale registrar (comportamento intencional, não uma sobra do bug):** se uma configuração for removida (`RemoveMensalidadeConfiguracao` / `RemoveMatriculaConfiguracao`) e depois uma nova for criada para o mesmo escopo, essa nova configuração também é tratada como "primeira" (porque não há mais nenhuma configuração *vigente* agora) — mesmo que, tecnicamente, já tenha existido uma configuração ali antes. Isso é deliberado: sem configuração vigente, nada está "devido" no momento, então a mesma lógica de "a primeira vale pra tudo" se aplica. Não tente "consertar" isso adicionando uma checagem de histórico — não é um bug.

### O que o patch faz — Parte 1

Em `internal/finance/mensalidade.go` e `internal/finance/matricula.go`:

1. Remove a checagem antiga, incondicional, de `modo_vigencia` obrigatório (era a primeira coisa validada, antes até do `valor`).
2. No fim de `validateConfiguracaoMensalidade` e `validateConfiguracaoMatricula` (nos dois pontos de saída: ramo do ensino fundamental, e ramo médio/superior), a função agora termina chamando uma nova função auxiliar:
   - `resolverModoVigenciaMensalidade` (nova, em `mensalidade.go`)
   - `resolverModoVigenciaMatricula` (nova, em `matricula.go`)
3. Essas funções auxiliares:
   - Consultam se já existe uma configuração **vigente agora** para o escopo (`resolveConfiguracao` para mensalidade, `ResolveMatriculaConfiguracao` para matrícula — as mesmas funções que o resto do código já usa para resolver preço).
   - Se **não existir** (primeira configuração) e `modo_vigencia` vier vazio, aplica o default `"cobrancas_pendentes"` (o mais próximo semanticamente de "vale para todos") e retorna sem erro. Se vier preenchido mesmo assim, valida normalmente (não aceita lixo).
   - Se **já existir** uma configuração vigente (isto é uma edição), exige `modo_vigencia` exatamente como antes — nenhuma mudança de comportamento aqui.

Isso é **retrocompatível**: se o frontend continuar mandando `modo_vigencia` sempre (inclusive na criação), nada muda — o valor enviado é usado normalmente. A mudança só passa a valer quando o campo vem vazio/ausente na primeira configuração de um escopo.

**Nota para uma futura tarefa de frontend (fora do escopo daqui):** como o backend agora aceita `modo_vigencia` vazio na criação, a tela `/financas/configuracoes/taxa-matricula/criar` e `/financas/configuracoes/mensalidade/criar` pode parar de perguntar "o que acontece com quem já está pendente?" e simplesmente não enviar o campo (ou enviar string vazia). A pergunta deve aparecer só na edição.

---

## Parte 2 — `mes_inicio_cobranca` aceitava qualquer mês quando nenhuma mensalidade ainda estava configurada

### Contexto do problema

A tela de "mês de início de cobrança" (`/financas/configuracoes/mensalidade/inicio-cobranca`) define, por academia + ano letivo, a partir de que mês as cobranças de mensalidade começam a valer — usado quando uma academia é integrada à plataforma com o ano letivo já em andamento.

`validateMesInicioCobranca` (em `internal/finance/mensalidade.go`) calcula a posição do mês escolhido dentro do ano letivo (usando `mesNaturalInicioAnoLetivo`, que retorna 9/setembro para escolas e 10/outubro para ensino superior, e `posicaoNoAnoLetivo`, que lida com a virada do ano) e rejeita `mes_inicio` se ele vier **depois** do menor `mes_fim_cobranca` já configurado para a academia (buscado com `MIN(mes_fim_cobranca) FROM financeiro_mensalidade_configuracoes_atual`).

**O bug:** essa checagem só rodava `if menor.Valid` — ou seja, só quando **já existe pelo menos uma configuração de preço de mensalidade** para a academia. Se a academia ainda não configurou nenhum preço de mensalidade (o que é perfeitamente possível: nada impede a academia de acessar `/inicio-cobranca` antes de `/mensalidade`), a consulta não retorna nada e a checagem inteira era **pulada** — `mes_inicio` aceitava literalmente qualquer valor de 1 a 12, inclusive agosto, que nunca é um mês letivo (nem para escola, nem para ensino superior).

Escrevi um teste de integração que reproduz exatamente isso (`TestIntegrationMesInicioCobrancaRespeitaAnoLetivoSemConfiguracaoDeMensalidade`, ver Parte 2 do patch) e confirmei o bug: sem nenhuma configuração de mensalidade, `MesInicio: 8` (agosto) era aceito sem erro.

### O que o patch faz — Parte 2

Em `validateMesInicioCobranca`: quando não existe nenhuma configuração de mensalidade ainda (`!menor.Valid`), em vez de pular a checagem, o limite passa a usar **7** como teto-padrão — o maior valor que `mes_fim_cobranca` pode assumir (`validateConfiguracaoMensalidade` já só aceita 6 ou 7). Isso garante que `mes_inicio` continue restrito ao período do ano letivo (setembro/outubro até julho, dependendo do tipo de academia) mesmo antes de qualquer preço ser configurado — sem mudar em nada o comportamento para quem já tem configuração (o `menor.Int64` real continua sendo usado quando existe).

A mensagem de erro também mudou de `"mes_inicio não pode ser posterior ao mes_fim_cobranca configurado"` para `"mes_inicio deve estar dentro do período do ano letivo"` (mais precisa, já que agora a checagem roda sempre, não só quando há um `mes_fim_cobranca` configurado). Confirmei que nenhum outro lugar do código (handlers, outros testes) depende do texto exato da mensagem antiga.

**Nota para uma futura tarefa de frontend (fora do escopo daqui):** a ordenação do seletor de mês pedida pelo usuário ("do mês que o ano letivo começa até quando termina") pode ser replicada no frontend com a mesma lógica de `mesNaturalInicioAnoLetivo`/`posicaoNoAnoLetivo` — início natural em setembro para escola, outubro para ensino superior, teto padrão em julho quando ainda não há configuração de mensalidade. Isso não exige nenhum endpoint novo; é só espelhar a mesma regra que o backend agora aplica.

---

## Resumo dos arquivos alterados

O patch altera exatamente **2 arquivos existentes** e cria **1 arquivo novo** — nenhuma migration, nenhum campo novo de banco, nenhum evento novo no ledger:

1. **`internal/finance/mensalidade.go`** — Parte 1 (mensalidade) + Parte 2 (mes_inicio_cobranca).
2. **`internal/finance/matricula.go`** — Parte 1 (matrícula).
3. **`internal/finance/modo_vigencia_primeira_configuracao_integration_test.go`** (novo) — 3 testes de integração novos:
   - `TestIntegrationMensalidadePrimeiraConfiguracaoNaoExigeModoVigencia`
   - `TestIntegrationMatriculaPrimeiraConfiguracaoNaoExigeModoVigencia`
   - `TestIntegrationMesInicioCobrancaRespeitaAnoLetivoSemConfiguracaoDeMensalidade`

**Não há aviso de ordem de deploy**: a mudança é aditiva e retrocompatível — o frontend atual (que já sempre envia `modo_vigencia`) continua funcionando sem nenhuma alteração. Pode fazer o deploy do backend a qualquer momento, independente do frontend.

## O que já foi validado pelo orquestrador

Você (Codex) **não tem acesso a `apt`, Docker nem `psql`** neste ambiente — por isso as validações abaixo, que exigem um PostgreSQL de verdade, já foram feitas fora do seu ambiente, num sandbox com PostgreSQL 16 real instalado. Resultado: **positivo em tudo**, nenhum ajuste foi necessário depois desses testes.

- **Baseline antes de tocar em qualquer código:** `go build ./...`, `go vet ./...` e `go test ./...` (as 136 migrations existentes, do zero, contra um Postgres real) — tudo limpo, 100% dos pacotes `ok`.
- Implementei as duas correções e escrevi os 3 testes novos.
- **Ciclo reverter → falhar → reaplicar**, feito para os três testes, um de cada vez: revertendo temporariamente cada correção, confirmei que o teste correspondente **falha** com a mensagem exata que o bug produziria (ex.: `"primeira configuração sem modo_vigencia foi rejeitada: modo_vigencia é obrigatório..."`, `"agosto (fora do ano letivo) foi aceito sem nenhuma configuração de mensalidade existir"`) — ou seja, os testes realmente pegam os bugs, não passam por acaso. Depois reapliquei cada correção e confirmei que os testes voltam a passar.
- `go test ./...` (suíte inteira, não só os pacotes tocados) com as correções aplicadas — **todos os pacotes `ok`**, incluindo os 3 testes novos, sem nenhuma regressão em nenhum teste pré-existente.
- **Validação final, independente de tudo isso:** cloney novo e limpo de `main` direto do GitHub, apliquei o `.patch` exatamente como você vai aplicar (`git apply`), confirmei que aplica sem conflito, e rodei de novo `go build`, `go vet` e `go test ./...` inteiro contra um banco Postgres recriado do zero — **100% verde**, incluindo os 3 testes novos passando individualmente com `-v`:
  ```
  --- PASS: TestIntegrationMensalidadePrimeiraConfiguracaoNaoExigeModoVigencia (0.04s)
  --- PASS: TestIntegrationMatriculaPrimeiraConfiguracaoNaoExigeModoVigencia (0.05s)
  --- PASS: TestIntegrationMesInicioCobrancaRespeitaAnoLetivoSemConfiguracaoDeMensalidade (0.03s)
  ```
- Também confirmei, por `grep`, que nenhum outro arquivo (`internal/handlers/*.go`) duplica a validação de `modo_vigencia` — toda a validação vive só em `internal/finance`, então o patch é suficiente sozinho, não precisa mexer na camada de handlers.

## Passo 1 — Aplicar o patch

Na raiz do repositório:

```bash
git apply "docs/Lista de Tarefas/108 - Modo de vigencia opcional na primeira configuracao e correcao do mes_inicio_cobranca.patch"
```

Isso deve alterar exatamente 2 arquivos (`internal/finance/mensalidade.go`, `internal/finance/matricula.go`) e criar 1 arquivo novo (`internal/finance/modo_vigencia_primeira_configuracao_integration_test.go`). Se o `git apply` falhar, PARE — não recrie as mudanças manualmente, reporte o conflito.

## Passo 2 — Verificação de build e testes

Rode, na raiz do repositório:

```bash
go build ./...
go vet ./...
go test ./...
```

Os três devem terminar sem nenhum erro (o `go test` deve mostrar `ok` para todos os pacotes, incluindo `spuri/internal/finance`, onde estão os três testes novos). Como você não tem PostgreSQL neste ambiente, não é preciso (nem possível) repetir a validação com banco real — ela já foi feita, ver seção anterior.

## Passo 3 — Conferir que a validação ficou só em `internal/finance`

Rode:

```bash
grep -rn "ModoVigencia\|modo_vigencia" internal/handlers/*.go
```

Não deve retornar nenhuma linha (fora de arquivos `_test.go`, se algum teste de handler citar o campo apenas para montar um payload de requisição — isso é esperado e não é um problema). Se aparecer alguma validação de "obrigatório" duplicada num handler, PARE e reporte, porque significa que existe uma segunda cópia da regra antiga que o patch não cobriu.

## O que NÃO fazer (fora de escopo)

- **Nenhuma mudança de frontend faz parte desta tarefa.** O documento original do usuário pedia uma lista grande de mudanças, quase todas de frontend puro (botões "Voltar", renomeação de rótulos, reorganização de listas em cartões, rotas aninhadas de criar/editar, colunas "Criado em"/"Editado em"/"Ver mais", UI de "Personalização adicional"). Já confirmei lendo `internal/projections/categoria_servico_projection.go` e `internal/projections/servico_extra_projection.go` que `created_at`/`updated_at` **já são persistidos e já retornam no JSON** das listagens — nenhuma mudança de backend é necessária para as colunas "Criado em"/"Editado em". E `detalhes_personalizados` (o campo por trás de "Personalização adicional") já é um mapa que naturalmente representa "vazio = não, preenchido = sim" — também não precisa de mudança de backend. Nenhum desses itens deve ser tocado aqui; eles pertencem a uma tarefa de frontend separada.
- Não altere a ordenação do seletor de mês no frontend nem crie nenhum endpoint novo para isso — a correção desta tarefa é só a validação no backend (Parte 2). Ver a nota de frontend na Parte 2 acima.
- Não exija `modo_vigencia` em nenhum outro fluxo (ex.: não adicione a exigência em `RemoveMensalidadeConfiguracao`/`RemoveMatriculaConfiguracao` — remoção nunca usou esse campo e deve continuar assim).
- Não mude o valor default aplicado quando `modo_vigencia` vem vazio na primeira configuração — deve continuar sendo `"cobrancas_pendentes"` (é o que os testes verificam).
- Não toque no teto de `mes_fim_cobranca` (continua fixo em 6 ou 7) nem no cálculo de `mesNaturalInicioAnoLetivo`/`posicaoNoAnoLetivo` — a Parte 2 só muda o que acontece quando `menor.Valid` é `false`.
- Não toque em nenhum outro item de `docs/Tarefas feitas/` — este patch não depende de nada além do estado atual do `main`.

## Passo 4 — Marcar como feito

Depois que os Passos 1–3 passarem sem problema:

1. Mova este arquivo (`108 - Modo de vigencia opcional na primeira configuracao e correcao do mes_inicio_cobranca.md`) e o `.patch` correspondente de `docs/Lista de Tarefas/` para `docs/Tarefas feitas/`.
2. Marque os itens do checklist abaixo como feitos.

## Resumo das mudanças (checklist final)

- [x] Patch aplicado (`git apply`) sem conflitos
- [x] `go build ./...` limpo
- [x] `go vet ./...` limpo
- [x] `go test ./...` — todos os pacotes `ok`, incluindo os 3 testes novos em `spuri/internal/finance`
- [x] `grep -rn "ModoVigencia\|modo_vigencia" internal/handlers/*.go` não mostra validação duplicada
- [x] Arquivos desta tarefa movidos para `docs/Tarefas feitas/`
