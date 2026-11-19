// Package handlers contém, por enquanto, só o HTTP do financeiro (cobranças,
// mensalidades, provedores e pagamento externo). É a última etapa da Parte 2 do
// Plano de Atualizações e migra para um pacote próprio depois da Parte 6.
//
// Regra da divisão de internal/handlers em pacotes (Parte 2 do Plano de
// Atualizações): um pacote de módulo não importa outro pacote de módulo. O que
// dois ou mais módulos usam vive em spuri/internal/handlers/apoio, e os helpers
// de teste compartilhados, em spuri/internal/handlers/testsupport. O teste
// TestPacotesDeModuloNaoSeImportam (pacote apoio) garante a regra.
package handlers
