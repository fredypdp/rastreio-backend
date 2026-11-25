-- ============================================================================
-- MIGRATION 145 — Configuração de faltas passa a CRUD (Parte 2B, entrega 2B-1)
-- ============================================================================
--
-- projection_faltas_configuracao deixa de ser projeção do ledger: vira tabela de
-- registro (nada se migra; as linhas atuais continuam). Quem grava é
-- internal/cadastros, com auditoria em auditoria_crud (migration 142).
--
-- Esta tabela não tem chave estrangeira para tabela reconstruível (a chave é o
-- próprio codigo_academia, sem FK), então não há nada a remover aqui.
-- ============================================================================

BEGIN;

DELETE FROM projection_checkpoints WHERE projection_name = 'faltas_configuracao';

COMMENT ON TABLE projection_faltas_configuracao IS
    'Tabela de registro da configuração de faltas por academia (CRUD, Tarefa 135). Gravada por internal/cadastros com auditoria em auditoria_crud. NÃO é reconstruída a partir do ledger.';

COMMIT;
