# Tarefa para o Codex — Repositório `rastreio-backend` (backend)

**Repositório:** https://github.com/fredypdp/rastreio-backend
**Branch base:** main
**Execução:** Não é necessário planejar nada. Todo o código já foi escrito e validado pelo orquestrador (ver "O que já foi validado" abaixo) e está pronto no arquivo `113 - Corrigir Pendencias de Servico Extra Gratuito.patch`, nesta mesma pasta. Sua única tarefa é aplicar o patch, rodar as verificações do Passo 2, e mover/renomear os arquivos conforme o Passo 3.

**Ordem de deploy:** nenhuma dependência com a Tarefa 19 do `rastreio-frontend` — cada uma corrige um sintoma independente do mesmo bug relatado e pode ir para produção separadamente, em qualquer ordem.

---

## Contexto do problema

Reportado: `GET /estudante/servicos-extras/minhas-inscricoes/:id/pendencias` devolvendo 500 para um estudante, com este log no servidor:

```
❌ [ERROR] ... Path=/estudante/servicos-extras/minhas-inscricoes/56b7e55c-c318-41a1-84c9-398b0350f057/pendencias ... Error=serviço extra sem preço configurado
```

...enquanto o frontend mostrava só a mensagem genérica "Erro interno do servidor. Tente novamente mais tarde." — confirmei pelo HAR da requisição real que o `request_id` da resposta (`1790200218890154701`) é exatamente o mesmo da linha de log acima; não há nenhuma transformação estranha no frontend, é só o comportamento normal de `RespondWithInternalError` (`internal/utils/errors.go`), que nunca expõe a mensagem real do erro ao cliente, só ao log do servidor.

**Causa raiz**, em `pendenciasServicoExtra` (`internal/handlers/servico_extra_solicitacao_handlers.go`): a função trata `serv.Preco == nil || serv.TipoCobranca == nil` como erro interno. Mas essa condição é exatamente o estado normal de um serviço extra **gratuito** (`pago=false`) — garantido pelo `CHECK chk_servico_extra_pago_campos` da migration 118 (`pago=false ⇔ preco IS NULL AND tipo_cobranca IS NULL`). Uma inscrição pode ficar `vinculada` ou `cancelada` num serviço gratuito normalmente (aprovação sem taxa de inscrição vincula direto — ver `Aprovar` em `internal/domain/aggregates/solicitacao_servico_extra.go`), e nesse caso não existe cobrança nenhuma — logo, nunca há pendências. Isso nunca deveria ter sido tratado como erro.

Reproduzi o bug empiricamente (não só por leitura de código): com Postgres real, uma inscrição vinculada a um serviço gratuito, chamando o handler diretamente, devolveu o mesmíssimo 500 com "serviço extra sem preço configurado" no log — idêntico ao reportado.

## O que o patch faz

1 arquivo alterado + 1 arquivo novo:

- **`internal/handlers/servico_extra_solicitacao_handlers.go`** — em `pendenciasServicoExtra`, o branch que tratava `serv.Preco == nil || serv.TipoCobranca == nil` como erro interno passa a devolver `200` com `pendencias: []`, igual ao early-return que já existe alguns passos acima para outros status. Só o comentário e o corpo desse `if` mudam — nenhuma outra linha da função é tocada.
- **`internal/handlers/servico_extra_pendencias_gratuito_integration_test.go`** (novo) — 2 testes de integração com Postgres real:
  - `TestIntegrationPendenciasServicoExtraGratuitoVinculadaDevolveVazia`: reproduz o cenário do bug (serviço gratuito, inscrição vinculada) e confirma `200` com `pendencias` vazia.
  - `TestIntegrationPendenciasServicoExtraPagoVinculadaContinuaFuncionando`: checagem de regressão — um serviço pago (`tipo_cobranca="unico"`) continua devolvendo a pendência real; a correção não pode silenciar pendências de serviços que de fato cobram algo.

## O que já foi validado pelo orquestrador

- Reproduzi o 500 original contra o código sem a correção, com Postgres real — log idêntico ao reportado, byte a byte na mensagem de erro.
- Com a correção aplicada, o mesmo cenário passa a devolver `200` com `pendencias: []`; o cenário de serviço pago (regressão) continua devolvendo a pendência real corretamente.
- **Baseline** (estado atual do repositório, sem esta mudança, banco recriado do zero): `gofmt`, `go build`, `go vet` limpos; `go test ./...` com **16 falhas pré-existentes**, todas em `internal/finance` e `internal/handlers`, nenhuma relacionada a serviços extras (são testes que dependem de credenciais reais de e-mail/AppyPay não configuradas neste ambiente de validação — não são causadas por nenhuma mudança de código).
- **Com o patch aplicado** (banco recriado do zero): `gofmt`, `go build`, `go vet` limpos; `go test ./...` com **exatamente as mesmas 16 falhas pré-existentes** (nenhuma nova, nenhuma regressão) e os 2 testes novos passando.
- `go test -race` nos 2 testes novos — limpo.
- **Validação final, independente de tudo isso:** clone novo e limpo de `main` direto do GitHub, apliquei o `.patch` exatamente como você vai aplicar (`git apply`), banco recriado do zero, `gofmt`/`go build`/`go vet`/`go test ./...` de novo — mesmíssimo resultado (mesmas 16 falhas pré-existentes, 2 testes novos passando).

## Passo 1 — Aplicar o patch

Na raiz do repositório:

```bash
git apply "docs/Lista de Tarefas/113 - Corrigir Pendencias de Servico Extra Gratuito.patch"
```

Deve alterar 1 arquivo e criar 1 novo. Se `git apply` falhar, PARE — não recrie as mudanças manualmente, reporte o conflito.

## Passo 2 — Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

Os três primeiros devem terminar sem nenhuma saída/erro. `go test ./...` deve mostrar os 2 testes novos passando; se aparecerem falhas fora do escopo de serviços extras (mensalidade, matrícula, AppyPay, recuperação de senha, remoção de credencial), confirme que são as mesmas ~16 falhas pré-existentes do ambiente antes de reportar como problema — não foram causadas por este patch. Se rodar a suíte mais de uma vez seguida com Postgres real, recrie o banco entre execuções (bases acumuladas de execuções anteriores já causaram falhas de outros testes não relacionados).

## O que NÃO fazer (fora de escopo)

- Não altere `IniciarPagamentoServicoExtraObrigacao` (mesmo arquivo, checagem `if serv.Preco == nil` perto da linha 560, mesma mensagem de erro). Ali é um caso genuinamente diferente: tentar pagar uma obrigação de um serviço sem preço é uma inconsistência real (nunca deveria existir uma obrigação/pendência gerada para um serviço gratuito), não um estado normal como em `pendenciasServicoExtra`. Deixe esse branch como está.
- Não mude `RespondWithInternalError` nem o comportamento genérico de mensagens de erro interno (`internal/utils/errors.go`) — isso afeta todos os 500 do sistema, não só este endpoint.
- Não toque em `PendenciasServicoExtraAcademia` (rota da academia) — ela chama a mesma função `pendenciasServicoExtra` corrigida aqui, então já se beneficia da correção automaticamente, sem precisar de mudança própria.
- A correção de frontend que oculta o botão "Ver pendências" quando o serviço é gratuito é um documento separado (Tarefa 19, no `rastreio-frontend`) — não implemente nada de frontend aqui.

## Passo 3 — Marcar como feito

Depois que os Passos 1–2 passarem sem problema, mova este arquivo e o `.patch` correspondente de `docs/Lista de Tarefas/` para `docs/Tarefas feitas/`.

## Resumo das mudanças (checklist final)

- [ ] Patch aplicado (`git apply`) sem conflitos
- [ ] `gofmt -l .` sem nenhuma saída
- [ ] `go build ./...` limpo
- [ ] `go vet ./...` limpo
- [ ] `go test ./...` — mesmas ~16 falhas pré-existentes de sempre (nenhuma nova), com os 2 testes novos passando
- [ ] Arquivos desta tarefa movidos para `docs/Tarefas feitas/`
