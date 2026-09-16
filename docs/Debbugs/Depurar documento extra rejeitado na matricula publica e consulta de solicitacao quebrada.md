---
data: 2026-09-16
status: corrigido_via_106_corrigir_upload_de_documento_extra_e_consulta_publica_de_solicitacoes_de_matricula
auditor: Claude (orquestrador) — depuração com PostgreSQL 16 e Go 1.22 reais em sandbox (go.mod ajustado só localmente, via replace directives, para contornar a exigência de Go 1.24 do módulo — nenhuma dessas replace directives faz parte da correção), testes ao vivo do binário compilado, e simulação completa do fluxo público de matrícula de ponta a ponta com uma academia e um documento extra reais
tarefa_correcao: docs/Tarefas feitas/106 - Corrigir upload de documento extra e consulta publica de solicitacoes de matricula.md
---

# Depuração — upload de documento extra rejeitado na matrícula pública + consulta pública de solicitação quebrada

## Como o problema foi relatado

Fredy relatou uma tentativa real de solicitação de matrícula que falhou em produção:

```
2026/09/15 21:46:42.582277 ❌ [ERROR] RequestID=1789508802284229644 Method=POST Path=/solicitacao-matricula IP=102.218.151.26 User=anonymous Error=campo de arquivo não suportado para matrícula: documento_extra_4f356c77-9397-4751-9377-de41fef371cc.
```

Junto, enviou um HAR/JSON de uma sessão de teste anterior mostrando uma consulta de status (`GET /solicitacao-matricula/L9XD4UCQ8BP/status`) devolvendo 404 com a mensagem `"solicitação não disponível para pagamento de matrícula"`, e pediu para investigar e corrigir (1) a criação de solicitação de matrícula e (2) a consulta de uma solicitação — e, numa mensagem seguinte, para não esquecer de **melhorar** a consulta pública, não só corrigir o que já estava quebrado.

## Investigação

### Bug 1 — upload de documento extra sempre rejeitado

`internal/handlers/solicitacao_matricula_handlers.go`, função `CriarSolicitacaoMatricula`: a primeira validação que roda sobre o multipart form, antes de qualquer outra, é `validarCamposArquivoMatricula`. Essa função aceita apenas uma lista fixa de nomes de campo (`solicitacaoDocFieldSet`: `bi_estudante`, `bi_encarregado`, `cedula_estudante`, `declaracao`, `certificado_6_ano_fundamental`, `certificado_9_ano_fundamental`, `certificado_ensino_medio`) e rejeita qualquer outro nome com exatamente a mensagem do log de produção.

O frontend (`MatriculaPublicPage.tsx`) envia documentos extra como campos `documento_extra_<id_do_catalogo>` desde que essa funcionalidade existe. Mais abaixo, no mesmo arquivo do handler, já existe uma função pronta e correta (`parseDocumentosExtra`, em `internal/handlers/documento_extra_upload.go`) que reconhece exatamente esse padrão de campo e valida o ID contra o catálogo real da academia para o ano acadêmico do estudante — inclusive com uma mensagem de erro própria e mais precisa (`"documento extra não aplicável a este cadastro: <id>"`) para quando o ID não corresponde a nada. `validarCamposArquivoMatricula` nunca foi atualizada para deixar esses campos passarem até essa validação real — toda submissão com documento extra morre no primeiro check, mesmo com um documento extra ativo, corretamente configurado, para o ano certo.

Rastreei quando essa funcionalidade passou a ser efetivamente alcançável por um usuário real: a tarefa `105 - Documentos Extra multi-ano e consulta publica.md` (já concluída) documenta, no seu próprio contexto, que a listagem pública de documentos extra na tela `/matricula` estava morta desde que foi implementada — a tela pública chamava um endpoint que exigia sessão de academia autenticada, então nunca listava nada para um visitante anônimo, e o frontend engolia esse erro silenciosamente. A tarefa 105 corrigiu exatamente isso, expondo uma rota pública dedicada. Ou seja: **antes da tarefa 105, nenhum usuário real conseguia nem ver que documentos extra existiam na matrícula pública — então o bug 1 nunca tinha sido exercitado.** Depois da tarefa 105, os documentos extra passaram a aparecer normalmente para qualquer visitante, e a primeira tentativa real de enviar um produziu o erro que Fredy reportou. Os dois bugs são independentes (105 não tocou `validarCamposArquivoMatricula`), mas um só ficou visível depois do outro ser corrigido.

### Bug 2 — mensagem errada ao consultar um código inexistente

`ConsultarStatusSolicitacaoMatricula`, no mesmo arquivo: quando a consulta ao banco não encontra a solicitação (ou falha por qualquer outro motivo), a resposta 404 usa a constante `solicitacaoPagamentoIndisponivel` = `"solicitação não disponível para pagamento de matrícula"` — a mesma mensagem usada, corretamente, em `IniciarPagamentoMatricula` para um cenário completamente diferente (tentativa de pagar uma matrícula que não está no estado certo para isso). Confirmei contra o binário compilado: consultar um código inexistente devolve exatamente essa mensagem, palavra por palavra igual ao que aparece no HAR enviado por Fredy. O resto do código já usa, consistentemente, `"solicitação não encontrada"` para esse mesmo cenário em outro handler (`loadSolicitacaoByCodigo`, usado pelas rotas autenticadas da academia) — só `ConsultarStatusSolicitacaoMatricula` (a rota pública) usava a mensagem errada.

### Bug 3 — a consulta de status nunca funcionava, mesmo para uma solicitação real (encontrado durante a validação da própria correção)

Depois de corrigir o Bug 2 e tentar confirmar que consultar uma solicitação **existente** agora devolvia 200 com o status correto, a consulta continuava devolvendo 404 — mesmo com a linha comprovadamente presente na projeção (`SELECT codigo_solicitacao, status FROM projection_solicitacoes_matricula` mostrando a linha certa via `psql` direto).

Causa: a mesma função lê a coluna `metodos_pagamento_matricula` — um `TEXT[]` nativo do Postgres, criado pela migration `106_financeiro_matricula.sql` — direto para uma variável `[]string`, sem o wrapper `pq.Array(...)`:

```go
var metodos []string
...Scan(&status, &academia, &valor, &metodos)
```

O driver `lib/pq` (usado por todo o projeto) não sabe decodificar um array nativo do Postgres para um `*[]string` sem esse wrapper — precisa de `pq.Array(&metodos)`. Sem ele, o `Scan` retorna um erro sempre que a linha existe (a query só chega a tentar decodificar as colunas quando encontra uma linha; se não encontra, o erro é `sql.ErrNoRows`, decodificado antes de chegar nas colunas). Esse erro de scan cai no mesmo `if err != nil` que trata "não encontrado" — e por isso qualquer consulta a uma solicitação **real e existente** também devolvia 404, indistinguível de "não existe".

Confirmei que isso não é uma escolha peculiar deste ponto do código: **todos os outros lugares** do projeto que leem essa mesma coluna (`internal/finance/matricula.go`, `internal/finance/servico_extra.go`, `internal/projections/solicitacao_matricula_projection.go`, entre outros) usam `pq.Array(&metodos)` corretamente. Só este handler específico não usa — claramente um descuido isolado, não um padrão intencional diferente.

**Isto significa que a funcionalidade de consulta pública de status nunca funcionou, para nenhuma solicitação, desde que a coluna `metodos_pagamento_matricula` foi criada** — é o bug mais sério dos três, e não estava no relato original de Fredy (só apareceu ao tentar confirmar, ao vivo, que a correção do Bug 2 realmente devolvia os dados certos para o caso de sucesso).

### Melhoria 4 — busca pública (por telefone/email/BI) engana o usuário sobre quantos campos preencher

Pedido explícito de Fredy: "não esqueça de melhorar a consulta pública". Investigando o segundo mecanismo de consulta pública (`BuscarSolicitacoesMatricula`, usado quando o usuário não tem o código da solicitação em mãos e busca por telefone/email/BI): a função exige pelo menos 2 dos 5 campos identificadores (`telefone`, `telefone_encarregado`, `email`, `bilhete_identidade`, `bilhete_identidade_encarregado`) preenchidos para rodar qualquer busca — mas quando recebe só 1, devolve `200 {"solicitacoes": []}`, uma resposta **idêntica** à de "busquei certo e não encontrei nada".

O teste de integração existente (`TestIntegrationBuscaPublicaMatriculaExigeDoisCamposENaoExibePagamento`) confirma que essa exigência de 2 campos é deliberada — não é o bug. O problema real está no frontend: `MatriculaPublicPage.tsx` mostra três campos de busca lado a lado (Telefone / Email / BI) com uma única mensagem de validação, "Informe telefone, email ou BI para buscar solicitações", que **implica que qualquer um dos três, isoladamente, é suficiente** — quando nunca foi. Resultado prático: um usuário que só sabe o telefone (o caso mais comum) preenche só o telefone, busca, e recebe uma lista vazia silenciosa — exatamente como se a solicitação não existisse — mesmo quando ela existe.

Duas correções, uma em cada ponta:

- **Backend**: quando faltam identificadores (`n < 2`), devolver `400` com uma mensagem clara em vez do `200` vazio — para que o chamador (frontend ou qualquer outro consumidor da API) consiga diferenciar "faltam dados para buscar" de "busquei e não achei". Isso **não** cria um canal de informação sobre quais valores específicos existem no banco (a checagem é só de contagem de campos preenchidos, roda antes de qualquer consulta ao banco, e é totalmente independente dos valores) — não compromete a proteção contra enumeração que o teste existente cobre.
- **Frontend**: a mensagem e o texto de ajuda passam a refletir a regra real. E, como o campo "BI" desta tela já é enviado à API duas vezes (como `bilhete_identidade` e como `bilhete_identidade_encarregado`, mesmo valor — porque o usuário pode não saber qual dos dois foi usado no cadastro), ele já satisfaz a exigência de 2 campos por conta própria; a regra client-side correta é "BI preenchido, OU telefone e email preenchidos juntos" — não simplesmente "dois quaisquer dos três campos visíveis".

### Uma pegadinha descoberta ao escrever a correção da melhoria 4

A primeira tentativa de implementar a mensagem de erro do `n < 2` usou `utils.RespondWithValidationError(c, fmt.Errorf("informe pelo menos dois identificadores (... bilhete_identidade ...)"))` — o padrão mais comum no resto do arquivo para erros de validação. A mensagem que voltou, ao vivo, não foi a que eu escrevi: foi `"Bilhete de identidade inválido (deve conter 12 números e 2 letras)"`.

Causa: `RespondWithValidationError` passa a mensagem por `utils.SafeErrorMessage` (`internal/utils/errors.go`), uma função que sanitiza mensagens de erro fazendo correspondência por substring contra uma tabela de palavras-chave (`"bilhete"`, `"email"`, `"senha"`, `"status"`, `"role"`, `"type"`, `"provincia"`, entre outras) e **substitui a mensagem inteira** pelo texto canônico daquela palavra-chave — pensada para casos em que o erro original vem de uma constraint de banco ou de uma validação de formato, não para desambiguar. Minha mensagem continha a palavra "bilhete_identidade" (um dos nomes de campo que eu estava listando para o usuário), e isso bastou para disparar a substituição errada. A correção final usa `utils.RespondWithError` diretamente (o mesmo padrão já usado no Bug 2, acima), que não passa pela sanitização — a mensagem literal é devolvida como está. Documentei isso explicitamente no documento de tarefa para que o Codex não reintroduza o mesmo engano.

## Validação executada

- **Ambiente**: PostgreSQL 16 real instalado no sandbox; backend clonado do zero de `github.com/fredypdp/rastreio-backend` (branch `main`) e compilado com Go 1.22 (o `go.mod` do projeto exige Go ≥ 1.24; usei `replace` directives locais, só no meu sandbox, para dependências transitivas que exigiam toolchains recentes — nenhuma dessas mudanças de `go.mod`/`go.sum` faz parte da correção real nem está no documento de tarefa).
- **Baseline**: `go build ./...`, `go vet ./...` e `go test ./...` completo (com `RUN_POSTGRES_INTEGRATION=1`, Postgres real, banco recriado do zero) rodados no código original, sem nenhuma alteração — 100% verde, para isolar que qualquer falha depois seria das minhas mudanças, não pré-existente.
- **Reprodução ao vivo dos três bugs**, contra o binário original, com uma academia real (criada via `POST /dominis/academia/cadastro`, ativada, com login) e um documento extra real cadastrado no catálogo dela (`POST /academia/documentos-extra`, ativo, para `ano_academico=1_ano_fundamental`):
  - Bug 1: `POST /solicitacao-matricula` com o campo `documento_extra_<id-real-do-catálogo>` devolveu exatamente `campo de arquivo não suportado para matrícula: documento_extra_<id>` — reprodução fiel, com um catálogo real e válido, não um caso hipotético.
  - Bug 2: `GET /solicitacao-matricula/L9XD4UCQ8BP/status` (código inexistente) devolveu `solicitação não disponível para pagamento de matrícula` — igual ao HAR enviado.
  - Bug 3: depois de criar uma solicitação real (contornando o Bug 1 corrigido), `GET /solicitacao-matricula/<código-real>/status` também devolvia 404 — só percebido nesta validação, não estava no relato original.
- **Depois de cada correção**: `go build`, `go vet`, `go test ./...` completo com Postgres real — 100% verde em todas as rodadas, incluindo o teste de integração atualizado (`TestIntegrationBuscaPublicaMatriculaExigeDoisCamposENaoExibePagamento`).
- **Reprodução ao vivo das quatro correções juntas, no binário corrigido**, com a mesma academia e o mesmo documento extra:
  - `POST /solicitacao-matricula` com o documento extra válido → `201`, solicitação criada com sucesso.
  - Regressão — documento extra com ID que não existe no catálogo → continua rejeitado, agora com a mensagem correta do `parseDocumentosExtra` (`documento extra não aplicável a este cadastro: <id>`), não mais a mensagem genérica de campo não suportado.
  - Regressão — campo de arquivo totalmente desconhecido (nada a ver com documento extra) → continua rejeitado, mensagem idêntica à original.
  - `GET /solicitacao-matricula/<código-real>/status` → agora `200`, com `status` e `codigo_academia` corretos.
  - `GET /solicitacao-matricula/L9XD4UCQ8BP/status` (inexistente) → `404`, agora com `"solicitação não encontrada"`.
  - `GET /solicitacao-matricula/busca?telefone_encarregado=...` (1 campo só) → `400`, mensagem clara.
  - `GET /solicitacao-matricula/busca?telefone_encarregado=...&bilhete_identidade_encarregado=...` (2 campos, combinando com a solicitação real) → `200`, encontra a solicitação criada.
- **Frontend**: clonado do zero de `github.com/fredypdp/rastreio-frontend`, `npm install`, `npx tsc --noEmit` e `npx eslint` no arquivo alterado — ambos limpos, sem erros, depois da alteração.
- **Simulação da própria tarefa de correção**: os blocos "localizar/substituir" do documento de tarefa (`docs/Tarefas feitas/106 - ...md`) foram os mesmos efetivamente aplicados nesta investigação — não é uma transcrição a posteriori, é o diff real já testado.

## O que o Codex precisa fazer

Tudo já implementado, testado e validado com PostgreSQL e Go reais, incluindo reprodução ao vivo dos três bugs contra o binário original e das quatro correções juntas contra o binário corrigido. Seguir `docs/Tarefas feitas/106 - Corrigir upload de documento extra e consulta publica de solicitacoes de matricula.md` mecanicamente.
