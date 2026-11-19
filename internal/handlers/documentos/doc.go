// Package documentos reúne os handlers HTTP de download de documentos e documentos extra.
//
// Regra da divisão de internal/handlers em pacotes (Parte 2 do Plano de
// Atualizações): um pacote de módulo não importa outro pacote de módulo. O que
// dois ou mais módulos usam vive em spuri/internal/handlers/apoio, e os helpers
// de teste compartilhados, em spuri/internal/handlers/testsupport. O teste
// TestPacotesDeModuloNaoSeImportam (pacote apoio) garante a regra.
package documentos
