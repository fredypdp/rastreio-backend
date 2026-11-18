-- 138_remove_matricula_publica.sql
-- Plano de Atualizações, Parte 1: a matrícula pública (solicitação de matrícula
-- feita pelo próprio estudante) e a taxa de matrícula deixam de existir.
--
-- Remove apenas tabelas/visão de LEITURA (projeções) que nenhum código usa mais:
--   * projection_solicitacoes_matricula
--   * financeiro_matricula_configuracoes (+ _remocoes e a visão _atual)
--
-- O ledger (spuri_ledger) NÃO é tocado: é append-only e continua com os eventos
-- antigos, se existirem. A projeção financeira ignora, sem efeito, os eventos
-- MatriculaConfigurada/MatriculaConfiguracaoRemovida num replay.
--
-- Sem CASCADE de propósito: se algum objeto desconhecido depender destas
-- tabelas, a migration falha de forma visível em vez de apagá-lo em silêncio.

DROP VIEW IF EXISTS financeiro_matricula_configuracoes_atual;
DROP TABLE IF EXISTS financeiro_matricula_configuracoes_remocoes;
DROP TABLE IF EXISTS financeiro_matricula_configuracoes;
DROP TABLE IF EXISTS projection_solicitacoes_matricula;
