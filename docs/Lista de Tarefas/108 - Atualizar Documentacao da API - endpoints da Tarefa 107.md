---
criado: 2026-09-18
status: pronto_para_execucao
tipo: documentacao
patch: tarefa_doc_api_delecao_servicos_extras.patch
depende_de: "Tarefa 107 (já aplicada) — este documento só atualiza a Documentação da API.md para refletir o que já está no ar"
---

# Atualizar `Documentação da API.md` com os endpoints da Tarefa 107 (deleção de Categoria de Serviço e Serviço Extra)

## 0. O que aconteceu

A Tarefa 107 (deleção auditável de Categoria de Serviço e Serviço Extra) foi aplicada e mesclada (`DELETE /academia/categorias-servico/:id` e `DELETE /academia/servicos-extras/:id`), mas a atualização da `Documentação da API.md` — prática padrão neste repositório para mudanças de contrato de API (ver a Tarefa 74, que fez exatamente isso para a deleção de Academia/Administrador/Estudante) — ficou pendente. Este documento cobre só isso: é puro markdown, sem migration, sem Go, sem risco de regressão de código.

Claude conferiu, antes de escrever qualquer linha, o código realmente mesclado da Tarefa 107 (não só o que foi planejado) para garantir que a documentação abaixo descreve o comportamento real: o status HTTP de erro (`400`, verificado em `utils.RespondWithValidationError`, não `422`), e que `GET /academia/servicos-extras/:id` continua retornando um serviço deletado normalmente por ID direto (só as listagens filtram `deleted_at` — `GetByID` não filtra, de propósito, mesmo padrão do `Curso`). Também validou que nenhuma entidade nova precisa ser adicionada ao `GET /dominis/auditoria/delecoes` (seção 16.6) — esse endpoint é escopado deliberadamente só a Academia/Administrador/Estudante desde a Tarefa 74, e nem `Curso` (que já tinha deleção auditável antes da 107) está nele.

## 1. Prompt recomendado

> Aplique `tarefa_doc_api_delecao_servicos_extras.patch` na raiz do repositório (`git apply tarefa_doc_api_delecao_servicos_extras.patch`) e confirme visualmente que o markdown renderiza corretamente (sem blocos de código quebrados, sem headers duplicados) nas seções 23.6 e 23.10. Não precisa rodar `go build`/testes — este patch toca só `Documentação da API.md`. Depois, mova este documento para `docs/Tarefas feitas/` seguindo a convenção normal.

## 2. O que o patch muda em `Documentação da API.md`

| Seção | Mudança |
|---|---|
| Frontmatter | `modificado: 18-09-2026`, versão `2.5.0 → 2.6.0` |
| `### 23.6 Categorias de serviço` | novo bullet `DELETE /academia/categorias-servico/:id`; "Proteção" e "Regras de negócio" atualizadas com a pré-condição de deleção (categoria inativa + sem serviços vinculados) e os erros `400` correspondentes; nota de que categorias deletadas somem da listagem |
| **`### 23.10 Deletar serviço extra`** (nova, ao final da seção 23 — mesma convenção de sempre neste arquivo: novos endpoints são anexados ao final da seção, nunca inseridos no meio com renumeração; ver `8.3 Autodeleção de conta`, adicionada da mesma forma) | documenta `DELETE /academia/servicos-extras/:id`: proteção, request body opcional (`motivo`), response 200, regras de negócio (inativo + sem inscrições ativas/pendentes/vinculadas) e erros |

Todas as entradas novas seguem exatamente o formato já usado no resto do arquivo (bullets com **Request body:**/**Response 200:** inline para a seção 23.6, e o formato mais completo com `#### `/**Proteção:**/**Request body:**/**Response 200:**/**Regras de negócio:**/**Erros:** para a subseção nova 23.10 — o mesmo formato já usado, por exemplo, em `### 22.x` Deletar sumário).

## 3. Como aplicar

```bash
git apply tarefa_doc_api_delecao_servicos_extras.patch
```

Já testado por Claude: `git apply --check` limpo num clone independente e recente de `main` (depois da Tarefa 107 já mesclada).

## 4. Nota — o `rastreio-frontend` mantém cópias deste mesmo arquivo

`rastreio-frontend` tem **duas** cópias deste arquivo: `src/Documentação da API.md` (desatualizada, versão `2.3.0`) e `src/docs/Documentação da API.md` (sincronizada até a versão `2.5.0`, igual à deste repositório antes deste patch). Se o fluxo de trabalho é copiar o arquivo atualizado para lá depois de mexer aqui, isso não está incluído neste patch (é um repositório diferente) — e a cópia desatualizada (`src/Documentação da API.md`) parece já estar obsoleta independentemente desta tarefa, o que vale reportar separadamente a quem mantém o frontend.

## 5. Checklist

1. `git apply tarefa_doc_api_delecao_servicos_extras.patch`.
2. Abrir o arquivo renderizado (preview do editor/GitHub) e conferir visualmente as seções 23.6 e 23.10.
3. Mover este arquivo e o patch para `docs/Tarefas feitas/`.
