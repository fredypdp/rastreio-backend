// Package testsupport reúne helpers de teste usados por mais de um módulo de
// handlers (fixtures de academia, estudante e financeiro, leitura de fontes).
// Só é importado por arquivos _test.go e não importa nenhum pacote de módulo.
//
// Regra da divisão de internal/handlers em pacotes (Parte 2 do Plano de
// Atualizações): um pacote de módulo não importa outro pacote de módulo. O que
// dois ou mais módulos usam vive em spuri/internal/handlers/apoio, e os helpers
// de teste compartilhados, em spuri/internal/handlers/testsupport. O teste
// TestPacotesDeModuloNaoSeImportam (pacote apoio) garante a regra.
package testsupport
