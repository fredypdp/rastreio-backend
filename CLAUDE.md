# SpuriBackend

As regras gerais (gatilho "Nova tarefa", documento de tarefa, segurança e relatório) estão em `../CLAUDE.md`.

## Leitura obrigatória

Antes de editar, leia por inteiro, sempre, mesmo que a mudança pareça tocar só parte deles:

- `db/` (migrations e queries)
- aggregates
- `models.go`
- handlers
- projections
- `Documentação da API.md`

## Validação

- Crie um banco PostgreSQL local novo para a tarefa, rode tudo contra ele e apague-o no fim (ver Segurança em `../CLAUDE.md`). Se faltar Go ou PostgreSQL, diga o que falta antes de continuar.
- Baseline: rode a suíte inteira antes de editar.
- Depois da mudança: migrations do zero, testes de integração e uma segunda execução no mesmo banco.
- Para cada regra importante, desfaça a regra, confirme que um teste falha e restaure.
