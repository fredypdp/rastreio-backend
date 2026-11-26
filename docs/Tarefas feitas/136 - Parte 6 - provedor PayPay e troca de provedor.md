# Tarefa 136 — Parte 6: provedor PayPay e troca de provedor (backend)

**Estado:** feito
**Repositório:** https://github.com/fredypdp/rastreio-backend
**Gerado sobre:** `main` @ `b5d7e5a` ("Add files via upload", a atualização do Plano feita pelo chat da Parte 2B; não toca em código)
**Entrega:** arquivos completos e atualizados na pasta `rastreio-backend/`, com os mesmos caminhos do repositório
**Plano:** `docs/Planos de Atualização/Plano de Atualizações 1.md`, Parte 6
**Versão da API:** 4.0.2 → **4.1.0** (MINOR: provedor novo, rota nova e campos opcionais novos; nada existente muda de contrato)
**Migration:** nenhuma (não há mudança de esquema)
**Ordem de deploy:** **backend primeiro, depois o front-end** (Tarefa 42 do `rastreio-frontend`). A mudança é aditiva: o front-end atual continua a funcionar com o backend novo. Antes de uma academia configurar o PayPay em produção, o admin FPP deve preencher a chave de sistema **`PAYPAY_API_URL`** (Configurações do sistema) com o endereço que o PayPay entregou para o ambiente. Sem ela, o PayPay aparece no catálogo, mas qualquer cobrança nele falha com "PAYPAY_API_URL não configurada".

---

## Contexto

A Parte 6 do plano pede o provedor PayPay (Express e Reference), a troca de provedor sem perder dívidas em aberto e o front-end correspondente. Antes desta tarefa o pacote `finance` já falava com o provedor só pela interface `Provedor` (Parte 4), mas o PayPay não existia. A leitura do código mostrou ainda que três coisas que o plano dá como previstas **não existiam**: fechar a cobrança no provedor ao cancelá-la, marcar o segundo pagamento da mesma dívida como duplicado e rejeitar pagamento de valor diferente do cobrado. Foram implementadas aqui.

Fonte do contrato do PayPay: `docs/Integrações/PayPay API.md`.

## O que foi atualizado e onde

**14 arquivos alterados, 6 novos e este documento.** Linhas referem-se aos arquivos entregues.

### Provedor novo

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `internal/finance/provedor_paypay.go` (**novo**) | `paypayProvider` (64) | O provedor PayPay inteiro. Métodos `REF` (banco `REF`) e `GPO` (MULTICAIXA Express, banco `MUL`); `GPO_QR` não é suportado. |
| idem | `Campos` (86), `ValidarCredenciais` (95) | Credenciais `partner_id` (só dígitos), `sale_product_code` (9 caracteres), `chave_privada` (secreta) e `chave_publica_paypay`. Aceita base64 ou PEM (com ou sem quebras de linha), PKCS#8 ou PKCS#1, mínimo 1024 bits; recusa a chave pública que for a pública da própria chave privada (erro comum). Guarda tudo normalizado. |
| idem | `paypayURLAPI` (151) | O endereço da API vem da chave de sistema `PAYPAY_API_URL` (https; http só fora de produção). Opcional no arranque. |
| idem | `ValidarPedido` (178) | `REF` só acima de 1000 Kz; `GPO` exige telefone de 9 dígitos (aceita `+244`, `244` e espaços). |
| idem | `PoliticaReconciliacao` (193) | Política própria por método: `GPO` 3 min → 15 min → 1 h, até 24 h; `REF` 15 min → 1 h → 6 h → 24 h, até 30 dias. O webhook é o caminho principal; a reconciliação é a rede de segurança. |
| idem | `CriarCobranca` (216), `ConsultarCobranca` (266), `CancelarCobranca` (290) | Serviços `instant_trade`, `trade_query` e `trade_close`. A criação recusada pelo PayPay vira cobrança `Failed` com o motivo dele; uma **consulta** recusada nunca muda o estado (é erro). O `trade_token` e o `dynamic_link` nunca são guardados nem devolvidos; a referência (`reference_id`) e a entidade (`entity_id`) vão em `response`. |
| idem | `chamar` (356), `paypayMontarPedido` (415), `paypayCifrarComChavePrivada` (505) | Pedido assinado: objeto de negócio cifrado em blocos com a chave privada (RSA PKCS#1 tipo 1), assinatura SHA1withRSA do texto ordenado, todos os valores com urlencode uma vez, fuso de Angola (GMT+1). |
| idem | `paypayOutcomeDeEstado` (608) | Só `TRADE_FINISHED` quita. `TRADE_SUCCESS` é aviso sem efeito (o PayPay recomenda); `WAIT_BUYER_PAY` e `P` são pendente; `TRADE_CLOSED` vira `Expired` (ou `Failed` com motivo, se vier `failCode`/`failReason`). |
| idem | `AutenticarWebhook` (639), `InterpretarWebhook` (686), `ChaveEventoWebhook` (709), `ResultadoWebhook` (718), `RespostaWebhook` (740) | Webhook autenticado pela **assinatura RSA** do PayPay (chave pública da credencial); idempotência por `notify_id`; notificações de reembolso e de outro tipo são registadas sem efeito; resposta exata `success`. Rota única `/webhooks/paypay/notificacoes`. |

### Contrato do provedor e núcleo

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `internal/finance/provedor.go` | `AutenticarWebhook` (192) | Passou a receber também o **corpo** da notificação (o PayPay assina o conteúdo). Quem já implementava (AppyPay, provedor falso dos testes) só ganhou o parâmetro. |
| idem | `ProvedorCancelavel` (210), `ProvedorComChaveDeEvento` (218), `ProvedorComPedidoNoWebhook` (227), `ProvedorValidaPedido` (236) | Quatro **extensões opcionais** do contrato: fechar a cobrança no provedor; chave de idempotência por notificação; pedido presente no corpo do webhook; validação do pedido antes de registar. A AppyPay não implementa nenhuma e continua com o comportamento de sempre. |
| idem | `Multilinha` (83), `IPPagador` (127), `novoProvedorPayPay` (247) | Dica de apresentação nos campos de credencial; IP de quem pediu o pagamento no pedido ao provedor; fábrica do PayPay registada no catálogo. |
| `internal/finance/credenciais.go` | `AuthenticateWebhook` (534), `restringirDonosDoWebhook` (600) | Recebe o corpo. Quando o provedor diz qual é o pedido do webhook, só a credencial que **emitiu** essa cobrança (ativa ou arquivada) é candidata a dona: a chave pública do PayPay pode ser a mesma em várias contas e, sem isto, a notificação de uma academia podia ser atribuída a outra. |
| `internal/finance/servico.go` | `CreateCharge` (287, 318) | Valida o pedido antes de registar qualquer coisa (um pedido impossível não deixa cobrança `Failed` fantasma) e passa o IP ao provedor. |
| idem | `cancelarCobranca` (487), `fecharNoProvedor` (530), `provedorPermiteFechar` (543) | O cancelamento passou a fechar o pedido no provedor **antes** de cancelar no Spuri, quando ele sabe fechar. Se o fecho falhar, nada é cancelado (erro 502). `CancelCharge` mantém a autorização e delega. |
| idem | `AcceptWebhook` (1143, 1148) | Idempotência por chave de notificação (e não pelo id da cobrança, que descartaria a notificação que confirma o pagamento como repetida da anterior). |
| idem | `aplicarConsulta` (986, 1031) | Uma consulta a uma cobrança **cancelada** deixou de poder reabri-la ou reescrever o estado (com o fecho do PayPay, `TRADE_CLOSED` a transformaria em `Expired`; na AppyPay um `Pending` a reabriria). Sucesso tardio continua a gerar o conflito de sempre. |
| idem | `providerOutcome` (1535), `liveChargeStatus` (1121), `aplicarConsulta` (986), `AcceptWebhook` (1143) | O resultado do provedor pode trazer o valor do pedido; pagamento com valor diferente do cobrado **não quita** (ver abaixo). |
| idem | `CobrancaResumo` (221, 227) | Campos opcionais `pago_em_duplicidade`, `duplicidade_com`, `pagamento_divergente`, `valor_pago_provedor`. |
| idem | `record` (1074) | Depois de um evento que leva a cobrança a `Success`, verifica duplicidade. |

### Troca de provedor, duplicidade e divergência

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `internal/finance/troca_provedor.go` (**novo**) | `liberarCobrancasDeOutroProvedor` (32) | Ao gerar cobrança nova para uma dívida que tem cobrança em aberto **noutro** provedor: se esse provedor sabe fechar (PayPay), a antiga é fechada e cancelada (motivo "substituída por nova cobrança após a troca de provedor") e a nova segue; se não sabe (AppyPay), a antiga continua pagável e **continua a bloquear** a nova (cancelá-la só no Spuri deixaria o pagador a poder pagar uma dívida "cancelada"); se o fecho for recusado, a nova não é criada. |
| idem | `verificarDuplicidade` (82), `cobrancasPagasDaMesmaDivida` (107) | Se outra cobrança já paga quita a mesma dívida (mensalidade ou serviço extra), a segunda fica `pago_em_duplicidade`: continua `Success` (o dinheiro entrou) mas não é aplicada a dívida nenhuma. |
| `internal/finance/mensalidade.go` | `IniciarPagamentoMensalidades` (373); confirmação (972) | Chama a liberação antes do bloqueio por cobrança em aberto; a confirmação ignora cobrança marcada como duplicada. |
| `internal/finance/servico_extra.go` | taxa de inscrição (49), obrigações (214) | Mesma liberação nos dois pagamentos de serviço extra. |
| `internal/finance/divergencia_valor.go` (**novo**) | `divergenciaDeValor` (25), `divergenciaMudou` (73) | Compara o valor do pedido segundo o provedor com o cobrado (tolerância de meio cêntimo). Se difere, a cobrança fica no estado em que estava, marcada `pagamento_divergente` com `valor_pago_provedor`; se o provedor corrigir, quita e a marca some. Só vale para provedores que informam o valor (PayPay, campo `amount`). |

### Entrada pública, configuração e documentação

| Arquivo | Onde | O que mudou |
| --- | --- | --- |
| `internal/handlers/financeiro_handlers.go` | `ReceberWebhook` (425, 430) | O corpo é lido **antes** de autenticar (limite de 1 MB mantido), porque o PayPay só se autentica com ele. |
| `internal/sysconfig/definicoes.go` | `PAYPAY_API_URL` (121), `validarURLHTTPS` (300) | Nova chave de sistema (grupo "Pagamentos (PayPay)"), imediata, só aceita https completo sem utilizador nem `#`. |
| `internal/finance/provedor_appypay.go` | `AutenticarWebhook` | Só o parâmetro novo (ignorado). Comportamento igual. |
| `Documentação da API.md` | versão (topo e linha 116), 7333, 7803, 7891, 8331, 8739, 8829, 8867, 8884 | Versão 4.1.0; chave `PAYPAY_API_URL` (16.7); regras de ambiente, webhook, credenciais do PayPay, cancelamento (19.9), catálogo (19.27), política de reconciliação (19.29); novas seções **19.30** (webhook do PayPay) e **19.31** (troca de provedor, duplicidade e divergência). |
| `Dockerfile` | linha 50 | `LABEL version="4.1.0"`. |
| `docs/Planos de Atualização/Plano de Atualizações 1.md` | Parte 6, visão geral, Registro de andamento | Parte 6 marcada como implementada, com "como ficou" e as interpretações adotadas. Alteradas só a Parte 6, a linha dela na visão geral e o Registro, como o plano manda quando há mais de um chat. |

### Testes (novos e ajustados)

| Arquivo | O que cobre |
| --- | --- |
| `internal/finance/provedor_paypay_test.go` (**novo**, 11 testes) | Servidor PayPay simulado que, com código **independente** do que está em teste, verifica a assinatura de cada pedido, decifra o `biz_content` (operação pública do RSA, remoção do preenchimento PKCS#1) e confere os campos. Cobre: cifra em vários blocos com acentos e símbolos, telefone do Express, regras de pedido, tradução dos estados, consulta, fecho e erros, endereço da API, validação de credenciais, autenticação e adulteração de webhooks, interpretação de webhooks, políticas de reconciliação, catálogo. |
| `internal/finance/provedor_paypay_integration_test.go` (**novo**, 11 testes, PostgreSQL real) | Ciclo completo por webhook; consulta e falha terminal; criação recusada; dono do webhook com a mesma chave pública em duas academias; cancelar com fecho no PayPay; pagamento divergente; troca PayPay → AppyPay (fecha a antiga e não bloqueia a nova); fecho recusado não cria cobrança nova; troca AppyPay → PayPay (a antiga continua a bloquear); métodos do provedor ativo; pagamento em duplicidade. |
| `internal/handlers/financeiro_paypay_webhook_integration_test.go` (**novo**, 1 teste) | O endpoint público de ponta a ponta: 401 para assinatura falsa ou ausente; resposta exata `success` em `text/plain`; `TRADE_SUCCESS` não quita; `TRADE_FINISHED` quita; repetições respondem `success` e gravam uma só. |
| `internal/finance/provedores_integration_test.go`, `appypay_integration_test.go`, `provedores_snapshot_integration_test.go` | Ajustes mecânicos: o novo parâmetro do corpo e o catálogo agora trazer o PayPay (que ainda não tem credenciais). Nenhuma expectativa de comportamento da AppyPay mudou. |

## Validação realizada

Ambiente do sandbox: Go 1.27.4 (o repositório declara 1.24 com toolchain 1.24.12; não havia como instalar essa versão), PostgreSQL 16. O proxy de módulos do Go não é acessível no sandbox; as dependências foram obtidas dos espelhos do GitHub por um `go.mod` alternativo **fora do repositório** (`-modfile`). O `go.mod` e o `go.sum` do repositório não foram tocados.

**Pacote aplicado por cima de um clone novo de `main` @ `b5d7e5a`** (só os arquivos entregues copiados):

- `gofmt -l .`: sem arquivos listados.
- `go build ./...`: ok.
- `go vet ./...`: ok.
- `go test ./... -count=1 -p 1 -v` com `RUN_POSTGRES_INTEGRATION=1` e banco real (as migrations já aplicadas): **29 pacotes `ok`**, 9 sem testes, **810 testes passaram, 0 falharam**, 65 ignorados — nenhum em `finance` nem em `handlers` (finance: 192 passaram, 0 ignorados). Os 65 ignorados são de outros pacotes e já ficavam ignorados antes desta tarefa.
- A mesma suíte, antes da tarefa, no mesmo banco: `finance` e `handlers` `ok`. Nenhuma fragilidade antiga apareceu; o banco de teste foi reutilizado em dezenas de execuções (equivale a "segunda execução no mesmo banco") sem falhar.

**Prova de que os testes detetam o defeito (revert → falha → reaplicar).** Desativei, uma a uma, 16 proteções novas e confirmei que o teste correspondente falha; depois reaplicei e a suíte voltou a passar:

| # | Proteção desativada | Teste que falhou |
| --- | --- | --- |
| 1 | Atribuir o webhook à credencial que emitiu a cobrança | `TestIntegrationPayPayWebhookEAtribuidoAContaQueEmitiuACobranca` |
| 2 | Idempotência por notificação | `TestIntegrationPayPayCicloDeVidaDaCobrancaREFePorWebhook` |
| 3 | Fechar no provedor ao cancelar | `TestIntegrationPayPayCancelarFechaNoProvedorEAConsultaNaoReabre` |
| 4 | Liberar cobranças de outro provedor | `TestIntegrationTrocaPayPayParaAppyPayFechaACobrancaAbertaENaoBloqueiaANova` |
| 5 | Marcar duplicidade | `TestIntegrationPagamentoEmDuplicidadeNaoAplicaASegundaCobranca` |
| 6 | Cancelada não reabre na consulta | `TestIntegrationPayPayCancelarFechaNoProvedorEAConsultaNaoReabre` |
| 7 | Validar o pedido antes de registar | `TestIntegrationPayPayCriacaoRecusadaGravaFalhaComMotivoDoProvedor` |
| 8 | A confirmação ignora a cobrança duplicada | `TestIntegrationPagamentoEmDuplicidadeNaoAplicaASegundaCobranca` |
| 9 | Pagamento divergente não quita (consulta) | `TestIntegrationPayPayPagamentoDivergenteNaoQuita` |
| 10 | Webhook confere o valor do pedido sem consulta ao vivo | idem |
| 11 | Fecho recusado impede a cobrança nova | `TestIntegrationTrocaFechoRecusadoNaoCriaCobrancaNova` |
| 12 | Respeitar o provedor que não sabe fechar | `TestIntegrationTrocaAppyPayParaPayPayMantemACobrancaAbertaPorqueNaoSeFecha` |
| 13 | REF só acima de 1000 Kz | `TestPayPayRegrasDePedido` |
| 14 | Só `TRADE_FINISHED` quita | `TestPayPayTraducaoDeEstados` |
| 15 | Verificar a assinatura do webhook | `TestPayPayWebhookAutenticacaoPorAssinatura` |
| 16 | Ler o corpo antes de autenticar (handler) | `TestIntegrationReceberWebhookPayPayAutenticaPorAssinaturaEConfirma` |

A mutação 11 **não** era detetada na primeira versão do teste (o erro de "já possui cobrança em aberto" escondia o do fecho); o teste foi reforçado para exigir o erro do fecho (`ErrUpstream`) e passou a detetá-la.

**O que NÃO provei:**
- **Nada foi chamado contra o PayPay real.** Sem credenciais nem endereço de API, tudo foi validado contra um servidor simulado do contrato descrito em `PayPay API.md`. Se a documentação divergir da realidade (formato exato da assinatura, campos, códigos de erro), só a primeira chamada de verdade mostra.
- A **assinatura da resposta síncrona** do PayPay não é verificada (o texto a assinar de um `biz_content` aninhado não está definido na documentação); a das notificações é.
- Não executei `AcademiaProjection.Rebuild()` nem reconstruções de projeção de ponta a ponta: esta tarefa não altera tabelas nem projeções (sem migration), apenas o payload de eventos já existentes (`CobrancaConsultada`).
- A reconciliação automática por tempo (job) não foi exercitada ao longo de horas; só a validade das políticas do PayPay e o seu uso pelo núcleo.

## Como aplicar

1. **Confirme que a base não mudou** (ficheiros que esta tarefa substitui):
   `git fetch origin && git diff --stat b5d7e5a origin/main -- Dockerfile "Documentação da API.md" internal/finance internal/handlers/financeiro_handlers.go internal/sysconfig/definicoes.go "docs/Planos de Atualização"`
   Saída vazia = pode substituir. Se aparecer um arquivo, ele mudou depois da base: **não substitua**; avise para eu integrar. (O plano pode ter sido tocado pelo chat da Parte 2B; nesse caso só a Parte 6, a sua linha na visão geral e o Registro devem ser refeitos por cima da versão nova.)
2. **Copie** a pasta `rastreio-backend/` por cima da raiz do repositório. Nenhum arquivo precisa ser apagado, movido ou renomeado.
3. **Verifique** (abaixo).
4. **Deploy** na ordem indicada no topo.

## Verificação

```bash
gofmt -l .
go build ./...
go vet ./...
RUN_POSTGRES_INTEGRATION=1 go test ./internal/finance/ ./internal/handlers/ ./internal/sysconfig/ -count=1
RUN_POSTGRES_INTEGRATION=1 go test ./... -count=1 -p 1
```

Esperado: `gofmt` sem saída; tudo `ok`. Depois do deploy, com o PayPay configurado: a rota `POST /webhooks/paypay/notificacoes` responde `401` a um corpo sem assinatura e `GET /financeiro/provedores` lista `AppyPay` e `PayPay`.

## Fora de escopo

- **Teste de conexão** antes de ativar um provedor (sugestão do plano): não implementado; sem credenciais reais não há como provar o que o PayPay responderia.
- Reembolso, transferências, Kwik e agente do PayPay; `GPO_QR` no PayPay.
- Verificar a assinatura da resposta síncrona do PayPay (ver "O que NÃO provei").
- Verificar valor pago em provedores que não o informam (AppyPay): fica como estava.
- Ações sobre duplicidade/divergência além do **aviso** (devolver, forçar quitação, aviso por e-mail ou SMS): Partes 8 e 25.
- As quatro garantias contra cobrança duplicada no banco (restrição única, reserva atómica, etc.): Parte 8. A marca de duplicidade desta tarefa é a rede de segurança, não a prevenção.
- Migrar o pacote `finance` para a pasta nova da Parte 2 e qualquer outra Parte do plano.

## Interpretações adotadas (corrija se discordar)

1. O **Endereço da API** vem da configuração do sistema, não da credencial: um endereço escolhido pela academia faria o servidor enviar pedidos assinados para onde ela quisesse.
2. Só **`TRADE_FINISHED`** quita; `TRADE_SUCCESS` é tratado como aviso, como o PayPay recomenda.
3. Na comparação de valores usa-se o `amount` (valor do pedido); o `seller_actual_amount` fica de fora porque pode vir líquido de taxas e daria divergências falsas.
4. A rota de notificações é uma só (`notificacoes`), com "método" `QUALQUER`, porque o portal do PayPay aceita um único endereço.
5. Cobrança pendente na AppyPay **continua a bloquear** a nova ao trocar para o PayPay (não se cancela só localmente uma coisa que o pagador ainda pode pagar).
6. `TRADE_CLOSED` sem motivo vira `Expired`; com `failCode`/`failReason` vira `Failed`.

## Marcar como feito

Já feito: `**Estado:** feito` e documento em `docs/Tarefas feitas/`.

## Checklist

- [ ] Base conferida (`b5d7e5a`)
- [ ] Pasta copiada por inteiro (14 alterados, 6 novos, este documento)
- [ ] `gofmt`, `build`, `vet` limpos
- [ ] `go test ./...` com PostgreSQL: tudo `ok`
- [ ] `PAYPAY_API_URL` preenchida antes de usar o PayPay
- [ ] `go.mod` e `go.sum` intactos

## Resultado

Provedor PayPay (Express e Reference) com pedido assinado, webhook por assinatura RSA, fecho de pedido e política de reconciliação própria; troca de provedor que fecha a cobrança pendente do provedor que sabe fechar e protege a que não sabe; pagamento em duplicidade e pagamento divergente deixam de quitar e ficam sinalizados; uma consulta já não reabre cobrança cancelada. API 4.1.0, sem migration. Validado com PostgreSQL real e servidor PayPay simulado; 16 mutações detetadas; nada testado contra o PayPay real.
