# Corrigir upload de documento extra na matrícula pública + consulta pública de solicitações

## Antes de começar (leia isto primeiro)

Esta tarefa já foi **inteiramente investigada, implementada e validada** por quem escreveu este documento — não em teoria: PostgreSQL 16 real, backend compilado (`go build`), `go vet` limpo, a suíte de testes inteira (`go test ./...`, com `RUN_POSTGRES_INTEGRATION=1` e Postgres real) 100% verde antes e depois, e reprodução ao vivo contra o binário compilado (academia real criada e ativada, documento extra real cadastrado no catálogo, solicitação de matrícula criada de ponta a ponta, casos de regressão testados). O frontend foi validado com `tsc --noEmit` e `eslint` limpos.

**A sua tarefa é só aplicar as mudanças abaixo exatamente como estão descritas.** Não há necessidade de repensar a abordagem, buscar alternativas, ou avaliar se "faz sentido" — isso já foi feito. Todos os blocos abaixo são "localizar X, substituir por Y" cirúrgicos — nenhum arquivo inteiro precisa ser reescrito. **Nenhum arquivo precisa ser removido** nesta tarefa.

Você **não tem acesso a PostgreSQL nem Docker neste ambiente** (isso já é sabido). Não tente subir um banco. Rode apenas os comandos da secção "Validação que o Codex deve rodar" no final — os pacotes que dependem de `DATABASE_URL`/`RUN_POSTGRES_INTEGRATION=1` vão pular (`SKIP`) ou falhar por falta de conexão quando essa variável não estiver definida, o que é esperado e **não é um problema desta mudança**.

Numerei esta tarefa como **106** (o próximo número livre depois de `105 - Documentos Extra multi-ano e consulta publica.md` em `docs/Tarefas feitas/`). Se esse número já estiver reservado para outra tarefa em andamento, apenas renomeie o arquivo — o número não afeta nada no código.

---

## Contexto — o que está sendo corrigido e por quê

Depois da tarefa 105 (que expôs publicamente a listagem de documentos extra na tela `/matricula`), usuários reais passaram a conseguir *ver* e tentar enviar documentos extra pela primeira vez — e toda tentativa falhava com:

```
Error=campo de arquivo não suportado para matrícula: documento_extra_4f356c77-9397-4751-9377-de41fef371cc
```

A investigação encontrou **três bugs distintos** na mesma vizinhança de código (`internal/handlers/solicitacao_matricula_handlers.go`) e uma melhoria pedida na consulta pública. Os três bugs e a melhoria são independentes entre si — cada um tem sua própria causa raiz e seu próprio bloco de correção abaixo — mas foram agrupados neste documento porque tocam a mesma superfície de código (o fluxo público de solicitação de matrícula) e devem ser aplicados juntos, no mesmo deploy.

1. **Upload de documento extra sempre rejeitado.** `validarCamposArquivoMatricula` roda antes de qualquer outra validação e só aceita uma lista fixa de nomes de campo (`bi_estudante`, `cedula_estudante`, etc.). Nunca foi atualizada para reconhecer o padrão `documento_extra_<id>`, usado pelo frontend desde que a funcionalidade de documentos extra foi criada — apesar de já existir, mais abaixo no mesmo arquivo, uma função (`parseDocumentosExtra`, em `documento_extra_upload.go`) que valida esse padrão corretamente contra o catálogo real da academia. Resultado: toda submissão com documento extra é rejeitada antes de chegar na validação correta, mesmo com um documento extra válido, ativo e configurado certo.

2. **Mensagem errada ao consultar uma solicitação inexistente.** `ConsultarStatusSolicitacaoMatricula` usa, para "código não encontrado", a mesma mensagem que deveria ser exclusiva do fluxo de pagamento (`"solicitação não disponível para pagamento de matrícula"`) — confunde quem só está checando o estado da própria submissão, sem ter chegado perto de pagar nada.

3. **A consulta de status nunca funcionava para uma solicitação que existe** (bug mais sério, encontrado durante a validação ao vivo desta própria correção — não fazia parte do relato original). A mesma função lê a coluna `metodos_pagamento_matricula` (um `TEXT[]` do Postgres) direto para um `[]string`, sem o wrapper `pq.Array(...)` que **todos os outros pontos do código** usam para ler essa mesma coluna (`internal/finance/matricula.go`, `internal/finance/servico_extra.go`, `internal/projections/solicitacao_matricula_projection.go`, etc.). Sem `pq.Array`, o `Scan` falha sempre que a linha existe — e esse erro de scan cai no mesmo `if err != nil` que trata "não encontrado", devolvendo 404 mesmo para um código real e válido. Ou seja: a consulta pública de status **nunca funcionou** para nenhuma solicitação, desde que essa coluna foi criada (migration `106_financeiro_matricula.sql`).

4. **Melhoria pedida na consulta pública (busca por telefone/email/BI).** `BuscarSolicitacoesMatricula` exige pelo menos 2 dos 5 identificadores (`telefone`, `telefone_encarregado`, `email`, `bilhete_identidade`, `bilhete_identidade_encarregado`) para rodar qualquer busca — mas quando recebe só 1, devolve `200 {"solicitacoes": []}`, **indistinguível** de "busquei certo e não achei nada". O frontend, por sua vez, convida o usuário a buscar por "telefone, email OU BI" (implicando que 1 basta), quando no fundo sempre precisou de 2. Resultado: buscar só por telefone, ou só por email, sempre "não encontra nada" silenciosamente, mesmo quando a solicitação existe. Corrigido para devolver `400` com mensagem clara nesse caso, e o frontend corrigido para refletir a regra real.

---

## Passo 1 — Editar `internal/handlers/solicitacao_matricula_handlers.go`

### 1.1 — Adicionar o import de `lib/pq` (necessário para o Passo 1.2)

Localizar:

```go
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
```

Substituir por:

```go
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
```

*(`github.com/lib/pq` já é uma dependência direta do módulo — está em `go.mod`/`go.sum` e já é usado em outros arquivos do mesmo pacote `handlers`, como `registros_handlers.go` e `estudante_handlers.go`. Nenhuma mudança em `go.mod`/`go.sum` é necessária.)*

### 1.2 — Corrigir `ConsultarStatusSolicitacaoMatricula` (bugs 2 e 3 do contexto)

Localizar:

```go
func ConsultarStatusSolicitacaoMatricula(c *gin.Context) {
	var status, academia string
	var valor sql.NullFloat64
	var metodos []string
	err := getDbClient(c).DB().QueryRowContext(c.Request.Context(), `SELECT status,codigo_academia,valor_matricula::float8,metodos_pagamento_matricula FROM projection_solicitacoes_matricula WHERE codigo_solicitacao=$1`, c.Param("codigo")).Scan(&status, &academia, &valor, &metodos)
	if err != nil {
		utils.RespondWithError(c, http.StatusNotFound, solicitacaoPagamentoIndisponivel, nil)
		return
	}
```

Substituir por:

```go
func ConsultarStatusSolicitacaoMatricula(c *gin.Context) {
	var status, academia string
	var valor sql.NullFloat64
	var metodos []string
	err := getDbClient(c).DB().QueryRowContext(c.Request.Context(), `SELECT status,codigo_academia,valor_matricula::float8,metodos_pagamento_matricula FROM projection_solicitacoes_matricula WHERE codigo_solicitacao=$1`, c.Param("codigo")).Scan(&status, &academia, &valor, pq.Array(&metodos))
	if err != nil {
		utils.RespondWithError(c, http.StatusNotFound, "solicitação não encontrada", nil)
		return
	}
```

Duas mudanças nesse bloco, ambas na mesma função: (a) `&metodos` → `pq.Array(&metodos)` no `Scan` — corrige o bug 3; (b) `solicitacaoPagamentoIndisponivel` → `"solicitação não encontrada"` — corrige o bug 2. A constante `solicitacaoPagamentoIndisponivel` **não é removida** — continua em uso correto em `IniciarPagamentoMatricula`, mais abaixo no mesmo arquivo. Não toque nela.

### 1.3 — Melhorar `BuscarSolicitacoesMatricula` (item 4 do contexto)

Localizar:

```go
	if n < 2 {
		c.JSON(http.StatusOK, gin.H{"solicitacoes": []gin.H{}})
		return
	}
```

Substituir por:

```go
	if n < 2 {
		utils.RespondWithError(c, http.StatusBadRequest, "informe pelo menos dois identificadores (telefone, telefone_encarregado, email, bilhete_identidade ou bilhete_identidade_encarregado) para buscar solicitações", nil)
		return
	}
```

**Atenção, não use `utils.RespondWithValidationError` aqui** (seria o padrão "óbvio", mas está errado neste caso específico): essa função passa a mensagem por `utils.SafeErrorMessage`, que faz correspondência por substring e **reescreve qualquer mensagem contendo a palavra "bilhete" para o texto fixo "Bilhete de identidade inválido (deve conter 12 números e 2 letras)"** — un texto completamente errado para este contexto (não é um bilhete que está inválido, é a busca que não tem identificadores suficientes). Isso foi confirmado ao vivo durante a validação: a primeira tentativa desta correção usou `RespondWithValidationError` e a mensagem saiu errada exatamente por esse motivo. `utils.RespondWithError` (usado acima) não passa pelo `SafeErrorMessage` — usa a `userMessage` literal, que é o que queremos aqui.

### 1.4 — Corrigir `validarCamposArquivoMatricula` (bug 1 do contexto)

Localizar:

```go
func validarCamposArquivoMatricula(form *multipart.Form) error {
	if form == nil {
		return nil
	}
	for field := range form.File {
		if _, ok := solicitacaoDocFieldSet[field]; !ok {
			return fmt.Errorf("campo de arquivo não suportado para matrícula: %s", field)
		}
	}
	return nil
}
```

Substituir por:

```go
func validarCamposArquivoMatricula(form *multipart.Form) error {
	if form == nil {
		return nil
	}
	for field := range form.File {
		if _, ok := solicitacaoDocFieldSet[field]; ok {
			continue
		}
		if strings.HasPrefix(field, extraDocFieldPrefix) {
			continue
		}
		return fmt.Errorf("campo de arquivo não suportado para matrícula: %s", field)
	}
	return nil
}
```

`extraDocFieldPrefix` (valor `"documento_extra_"`) já existe — está definida em `internal/handlers/documento_extra_upload.go`, mesmo pacote `handlers`, nenhum import novo necessário. A validação real de que o ID depois do prefixo corresponde a um documento extra ativo no catálogo da academia, para o ano acadêmico certo, continua acontecendo — sem nenhuma mudança — em `parseDocumentosExtra`, chamada mais abaixo na mesma função `CriarSolicitacaoMatricula`. Esta mudança só faz `validarCamposArquivoMatricula` parar de bloquear esses campos *antes* dessa validação real acontecer; não afrouxa nenhuma regra de segurança.

Esta mesma função (`validarCamposArquivoMatricula`) é chamada também de dois outros pontos, em `internal/handlers/estudante_handlers.go` (cadastro singular de estudante e outro fluxo de cadastro direto pela academia) — **não precisa tocar nesses dois pontos**, a correção na função compartilhada já resolve os três lugares automaticamente.

---

## Passo 2 — Editar `internal/handlers/financeiro_handlers_integration_test.go`

O teste existente assumia (corretamente, até esta tarefa) que buscar com 1 único campo devolve `200` com lista vazia — o Passo 1.3 muda esse contrato para `400`. Atualizar o teste para refletir o novo contrato, sem perder nenhuma das verificações originais.

Localizar:

```go
	for _, query := range []string{"telefone=" + telefone, "telefone=" + telefone + "&email=outro@example.test"} {
		recorder := buscar(query)
		if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), codigo) {
			t.Fatalf("busca indevida para %q: %d %s", query, recorder.Code, recorder.Body.String())
		}
	}
```

Substituir por:

```go
	umCampo := buscar("telefone=" + telefone)
	if umCampo.Code != http.StatusBadRequest || strings.Contains(umCampo.Body.String(), codigo) {
		t.Fatalf("busca com um único campo deveria exigir mais identificadores (400): %d %s", umCampo.Code, umCampo.Body.String())
	}
	doisCamposSemMatch := buscar("telefone=" + telefone + "&email=outro@example.test")
	if doisCamposSemMatch.Code != http.StatusOK || strings.Contains(doisCamposSemMatch.Body.String(), codigo) {
		t.Fatalf("busca com dois campos que não combinam não deveria retornar a solicitação: %d %s", doisCamposSemMatch.Code, doisCamposSemMatch.Body.String())
	}
```

O caso de "1 campo apenas" passa a esperar `400` (o novo contrato). O caso de "2 campos, mas que não combinam entre si" continua esperando `200` com lista vazia, exatamente como antes — essa parte do comportamento não muda, só foi separada num bloco próprio para não ficar misturada com o caso que mudou.

---

## Passo 3 — Editar `frontend/src/app/(full-width-pages)/(auth)/matricula/MatriculaPublicPage.tsx`

*(ajuste o caminho do repositório `rastreio-frontend` se o clone local usar outra raiz — o caminho relativo a partir da raiz do repositório é `src/app/(full-width-pages)/(auth)/matricula/MatriculaPublicPage.tsx`.)*

### 3.1 — Corrigir a validação e a mensagem da busca pública

Localizar:

```tsx
  async function buscarSolicitacoes() {
    setErro(""); setSolicitacoes([]);
    const params = { telefone: busca.telefone, email: busca.email, bilhete_identidade: busca.bi, bilhete_identidade_encarregado: busca.bi };
    if (!params.telefone && !params.email && !params.bilhete_identidade) { setErro("Informe telefone, email ou BI para buscar solicitações."); return; }
    setLoadingStatus(true);
    try { const res = await solicitacaoMatriculaService.buscar(params); setSolicitacoes(res.solicitacoes ?? []); }
    catch (err: any) { setErro(err?.message ?? "Não foi possível buscar solicitações."); }
    finally { setLoadingStatus(false); }
  }
```

Substituir por:

```tsx
  async function buscarSolicitacoes() {
    setErro(""); setSolicitacoes([]);
    const params = { telefone: busca.telefone, email: busca.email, bilhete_identidade: busca.bi, bilhete_identidade_encarregado: busca.bi };
    const identificadoresSuficientes = Boolean(busca.bi) || (Boolean(busca.telefone) && Boolean(busca.email));
    if (!identificadoresSuficientes) { setErro("Informe o BI, ou telefone e email juntos, para buscar solicitações."); return; }
    setLoadingStatus(true);
    try { const res = await solicitacaoMatriculaService.buscar(params); setSolicitacoes(res.solicitacoes ?? []); }
    catch (err: any) { setErro(err?.message ?? "Não foi possível buscar solicitações."); }
    finally { setLoadingStatus(false); }
  }
```

Por que a regra é "BI sozinho OU telefone+email juntos", e não simplesmente "2 de 3 campos preenchidos": o campo "BI" desta tela já é enviado para a API **duas vezes** — como `bilhete_identidade` (do estudante) e como `bilhete_identidade_encarregado` (do encarregado), com o mesmo valor digitado (linha `params` acima, que não muda) — porque quem está buscando pode não saber qual dos dois BIs foi usado no cadastro. Isso já satisfaz, por si só, a exigência de 2 identificadores do backend (Passo 1.3). Exigir também um segundo campo (telefone ou email) quando o BI já foi informado seria mais restritivo do que o backend realmente exige, sem necessidade.

### 3.2 — Corrigir o texto de ajuda acima dos campos de busca

Localizar:

```tsx
            <p className="text-sm text-gray-500 dark:text-gray-400">Informe o código recebido ou busque por telefone, email ou BI para consultar o estado da sua matrícula e pagar a taxa quando ela existir.</p>
```

Substituir por:

```tsx
            <p className="text-sm text-gray-500 dark:text-gray-400">Informe o código recebido, ou busque pelo BI (ou por telefone e email juntos) para consultar o estado da sua matrícula e pagar a taxa quando ela existir.</p>
```

---

## Checklist — arquivos tocados nesta tarefa

- [ ] `internal/handlers/solicitacao_matricula_handlers.go` — 4 mudanças cirúrgicas (Passos 1.1 a 1.4). Nenhuma outra parte do arquivo muda.
- [ ] `internal/handlers/financeiro_handlers_integration_test.go` — 1 mudança cirúrgica (Passo 2).
- [ ] `frontend/.../matricula/MatriculaPublicPage.tsx` — 2 mudanças cirúrgicas (Passos 3.1 e 3.2).
- [ ] **Nenhum arquivo é removido ou criado.** `go.mod`/`go.sum` **não mudam** — `github.com/lib/pq` já é dependência direta existente.
- [ ] Nenhuma outra função, arquivo ou lógica não relacionada a solicitação/consulta de matrícula foi tocada.

---

## Validação que o Codex deve rodar

Rode, na raiz de `rastreio-backend`:

```bash
go build ./...
go vet ./...
gofmt -l internal/handlers/solicitacao_matricula_handlers.go internal/handlers/financeiro_handlers_integration_test.go
go test ./...
```

Esperado: `go build`/`go vet` sem saída; `gofmt -l` sem saída (nenhum arquivo listado — se listar algo, o arquivo não está formatado, rode `gofmt -w` nesse arquivo); `go test ./...` verde em todos os pacotes que não dependem de banco. Os testes de integração deste mesmo arquivo (`TestIntegrationBuscaPublicaMatriculaExigeDoisCamposENaoExibePagamento` e os demais no arquivo) **só rodam de verdade com `RUN_POSTGRES_INTEGRATION=1` e `DATABASE_URL` configurados** — sem isso, eles são pulados (`SKIP`), o que é esperado neste ambiente e não indica problema. Isto já foi confirmado 100% verde, com Postgres 16 real e `RUN_POSTGRES_INTEGRATION=1`, por quem escreveu este documento — não precisa repetir essa parte.

Rode, na raiz de `rastreio-frontend`:

```bash
npx tsc --noEmit
npx eslint "src/app/(full-width-pages)/(auth)/matricula/MatriculaPublicPage.tsx"
```

Esperado: ambos sem erros. Já confirmado limpo por quem escreveu este documento.

Não tente rodar `npm run build` completo para validar isto — nesse ambiente ele pode falhar por não conseguir buscar fontes do Google Fonts (`fonts.googleapis.com`), um problema de rede do ambiente sem relação com esta mudança; `tsc --noEmit` e `eslint` já são suficientes para validar o arquivo alterado.

## Conclusão

Tudo já implementado, testado e validado com PostgreSQL e Go reais, incluindo reprodução ao vivo dos três bugs contra o binário original e confirmação ao vivo de que as quatro correções funcionam juntas sem regressão (documento extra válido → matrícula criada com sucesso; documento extra inexistente no catálogo → continua rejeitado; campo de arquivo totalmente desconhecido → continua rejeitado; consulta de status de uma solicitação real → agora funciona; busca pública com 1 campo → 400 claro; busca pública com 2 campos corretos → encontra a solicitação). Siga os Passos 1 a 3 mecanicamente.
