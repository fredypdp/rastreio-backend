package comunicacao

import (
	"spuri/internal/handlers/apoio"
)

// EnvioRecuperacaoSincronoParaTeste faz o e-mail de recuperação de senha sair
// dentro da própria requisição (em produção sai numa goroutine). Devolve a
// função que restaura o comportamento original. Somente para testes.
func EnvioRecuperacaoSincronoParaTeste() (restaurar func()) {
	anterior := apoio.ExecutarEnvioRecuperacao
	apoio.ExecutarEnvioRecuperacao = func(f func()) { f() }
	return func() { apoio.ExecutarEnvioRecuperacao = anterior }
}
