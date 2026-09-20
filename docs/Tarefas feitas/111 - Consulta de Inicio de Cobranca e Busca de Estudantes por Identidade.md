# Tarefa para o Codex — Repositório `rastreio-backend` (backend)

**Repositório:** https://github.com/fredypdp/rastreio-backend
**Branch base:** main
**Execução:** Não é necessário planejar nada. Todo o código já foi escrito e validado pelo orquestrador (ver "O que já foi validado" abaixo) e está pronto no arquivo `111 - Consulta de Inicio de Cobranca e Busca de Estudantes por Identidade.patch`, nesta mesma pasta. Sua única tarefa é aplicar o patch, rodar as verificações do Passo 2, e mover/renomear os arquivos conforme o Passo 3.

**Ordem de deploy:** este backend precisa ir para produção **antes** da tarefa de frontend correspondente (Tarefa 16, no `rastreio-frontend`), que passa a chamar as duas rotas novas descritas abaixo. Se o frontend for ao ar primeiro, as duas telas que dependem delas vão falhar com 404.

---

## Contexto do problema

Esta tarefa nasceu de dois pontos, dentro de um pedido maior sobre a inscrição de estudantes em serviços/cobranças de uma academia, que precisavam de suporte novo no backend antes de qualquer mudança de frontend:

1. **`/financas/configuracoes/mensalidade/inicio-cobranca`** — a tela só deveria permitir remover uma exceção de início de cobrança quando já existe uma configurada para o ano letivo. O próprio código do frontend já documentava, em comentário, por que isso não era feito: "não existe uma consulta dedicada para isso" — só existiam `POST` (definir) e `DELETE` (remover), nenhum jeito de perguntar antes se algo já estava definido.
2. **Busca de estudante em várias telas** — várias telas (ex.: `/financas/gestao-cobrancas/anular-mensalidade`, `/reativar-mensalidade`, `/cancelar-cobranca`) usavam um `<select>` alimentado por uma lista de até 300 estudantes da academia, sem nenhuma busca real por identidade (código, nome, BI, telefone ou e-mail). Investiguei `GET /estudantes` (`ListarEstudantes`) e confirmei: só existiam filtros categóricos (gênero, idade, ano escolar, curso, turma, status) — nenhum campo de busca livre, embora as 5 colunas relevantes (`nome`, `codigo_estudante`, `bilhete_identidade`, `telefone`, `email`) já estivessem disponíveis na própria consulta.

## O que o patch faz

7 arquivos — nenhuma migration, nenhum campo novo no banco:

### 1. Consulta de início de cobrança (`GET /financeiro/mensalidades/inicio-cobranca`)

- **`internal/finance/mensalidade.go`** — novo método `Service.ConsultarMesInicioCobranca(ctx, codigoAcademia, anoLetivo)`, inserido entre `DefinirMesInicioCobranca` e `RemoveMensalidadeConfiguracao`. Reaproveita a view de projeção que já existe (`financeiro_mensalidade_inicio_cobranca_atual`, criada na migration 109) — a mesma que `RemoveMesInicioCobranca` já consulta internamente antes de remover. Devolve `ErrNotFound` quando não há nenhuma exceção definida para o ano letivo; não toca em `mesInicioEfetivo` (a função privada que resolve o mês natural como *fallback* — usada só internamente para calcular cobranças, com semântica diferente: essa nunca "não encontra", sempre devolve um mês).
- **`internal/handlers/mensalidade_handlers.go`** — novo handler `ConsultarMesInicioCobranca`, inserido entre `DefinirMesInicioCobranca` e `RemoverMesInicioCobrancaInput`/`RemoverMesInicioCobranca`. Mesmo padrão de autorização já usado pelos handlers vizinhos (`authorizeMensalidadeAcademia`, academia só vê o próprio escopo, admin FPP pode consultar qualquer uma); erro mapeado por `financeError` (o mesmo helper que os outros handlers financeiros já usam), que já converte `ErrNotFound` em HTTP 404.
- **`cmd/server/main.go`** — rota registrada: `financeiro.GET("/mensalidades/inicio-cobranca", handlers.ConsultarMesInicioCobranca)`, entre o `POST` e o `DELETE` já existentes para o mesmo caminho.
- **`internal/finance/mensalidade_inicio_cobranca_consulta_integration_test.go`** (novo) — 4 testes ao nível de serviço: sem exceção definida (`ErrNotFound`), com exceção definida (valor certo devolvido, e isolado por ano letivo — outro ano letivo da mesma academia continua sem exceção), depois de remover (volta a `ErrNotFound`), e validação de parâmetros obrigatórios.
- **`internal/handlers/mensalidade_inicio_cobranca_consulta_integration_test.go`** (novo) — 1 teste ao nível de handler, cobrindo o contrato HTTP completo: 404 sem exceção, 200 com o `mes_inicio` certo no corpo depois de definir, 403 quando uma academia tenta consultar o escopo de outra, 400 quando falta `ano_letivo`.

### 2. Busca livre de estudante (`GET /estudantes?busca=`)

- **`internal/handlers/estudante_handlers.go`** — novo parâmetro de query `busca` em `ListarEstudantes`, inserido junto aos outros filtros (antes do filtro fixo `status <> 'deletado'`, que já é aplicado por último). Quando presente, adiciona uma condição `OR` comparando o termo (via `ILIKE`, case-insensitive, correspondência parcial) contra as 5 colunas — `nome`, `codigo_estudante`, `bilhete_identidade`, `telefone`, `email` — usando o mesmo padrão de `args`/`conditions` que os filtros vizinhos já seguem. Continua funcionando junto com os filtros categóricos existentes (turma, curso, status etc.) e mantém o isolamento por academia inalterado — não é uma rota nova, nem muda o comportamento de quem não passa `busca`.
- **`internal/handlers/estudantes_busca_livre_integration_test.go`** (novo) — `TestIntegrationListarEstudantesBuscaLivre`, cobrindo busca por nome (parcial), código (exato), BI (parcial), telefone (exato) e e-mail (parcial), confirmando isolamento por academia (o mesmo nome existe em duas academias — a busca de uma só encontra a própria) e o caso sem correspondência.

## O que já foi validado pelo orquestrador

Você (Codex) não tem PostgreSQL/Docker neste ambiente — as validações abaixo já foram feitas fora dele, num sandbox com PostgreSQL 16 real, Go 1.24 real.

- **Baseline** (sobre o estado atual do repositório, já com as Tarefas 107–110 aplicadas): `gofmt -l .`, `go build ./...`, `go vet ./...`, `go test ./...` e `go test -race ./...` — tudo limpo antes de qualquer mudança, numa base de dados recriada do zero.
- `go test ./...` e `go test -race ./...` (suíte inteira) com a mudança aplicada, numa base de dados recriada do zero — **todos os pacotes `ok`**, incluindo os 3 arquivos de teste novos, sem nenhuma regressão em nenhum teste pré-existente.
- **Validação final, independente de tudo isso:** clone novo e limpo de `main` direto do GitHub, apliquei o `.patch` exatamente como você vai aplicar (`git apply`), confirmei que aplica sem conflito, e rodei de novo `gofmt`, `go build`, `go vet` e `go test ./...` inteiro (base de dados recriada do zero) — **100% verde**.
- **Nota sobre um teste pré-existente:** `TestIntegrationHandlersRemocaoFinanceiraRespeitamEscopoDaAcademia` (não relacionado a esta tarefa) falha quando a suíte inteira roda contra uma base de dados que já acumulou dados de execuções anteriores (não é isolado por transação); passa sempre quando a base é recriada antes da execução, como o comando abaixo já faz. Não é um bug introduzido por esta tarefa — é um lembrete para quem rodar a suíte localmente várias vezes seguidas: recrie a base entre execuções completas.

## Passo 1 — Aplicar o patch

Na raiz do repositório:

```bash
git apply "docs/Lista de Tarefas/111 - Consulta de Inicio de Cobranca e Busca de Estudantes por Identidade.patch"
```

Deve alterar 4 arquivos e criar 3 novos. Se `git apply` falhar, PARE — não recrie as mudanças manualmente, reporte o conflito.

## Passo 2 — Verificação de build e testes

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

Os quatro devem terminar sem nenhum erro (`gofmt -l .` sem nenhuma linha de saída; `go test` deve mostrar `ok` para todos os pacotes, incluindo `spuri/internal/finance` e `spuri/internal/handlers`, onde estão os testes novos). Se você rodar `go test ./...` mais de uma vez seguida seu ambiente permitir Postgres real, recrie a base entre execuções (ver nota acima).

## O que NÃO fazer (fora de escopo)

- Não crie uma consulta equivalente para taxa de matrícula nem para nenhum outro tipo de exceção financeira — só início de cobrança de mensalidade foi pedido.
- Não mude `mesInicioEfetivo` (a função privada que já existia) nem qualquer lógica de cálculo de cobrança real — `ConsultarMesInicioCobranca` é só leitura, para a UI decidir se mostra o botão de remover.
- Não adicione o parâmetro `busca` a nenhum outro endpoint de listagem (turmas, cursos, matérias etc.) — só `GET /estudantes`, que é o único mencionado.
- Não mude o comportamento de `GET /estudantes` para quem não passa `busca` — os filtros e o comportamento existentes continuam exatamente iguais.
- As tarefas de frontend que passam a usar estas duas rotas são um documento separado (Tarefa 16, no `rastreio-frontend`) — não implemente nada de frontend aqui.

## Passo 3 — Marcar como feito

Depois que os Passos 1–2 passarem sem problema, mova este arquivo e o `.patch` correspondente de `docs/Lista de Tarefas/` para `docs/Tarefas feitas/`.

## Resumo das mudanças (checklist final)

- [x] Patch aplicado (`git apply`) sem conflitos
- [x] `gofmt -l .` sem nenhuma saída
- [x] `go build ./...` limpo
- [x] `go vet ./...` limpo
- [x] `go test ./...` — todos os pacotes `ok`, incluindo os testes novos em `spuri/internal/finance` e `spuri/internal/handlers`
- [x] Arquivos desta tarefa movidos para `docs/Tarefas feitas/`
