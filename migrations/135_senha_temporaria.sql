-- 135_senha_temporaria.sql
-- Tarefa 123 (SEC-04 / SEC-10): senha temporária de troca obrigatória e
-- invalidação de sessões anteriores.
--
-- deve_alterar_senha: TRUE enquanto a conta usa a senha temporária que o sistema
--   gerou. Consequência dos eventos do ledger (SenhaTemporariaDefinida liga;
--   o evento de troca de senha desliga conforme o motivo), logo é reconstruída
--   em qualquer rebuild. Contas antigas ficam FALSE por padrão.
-- sessoes_validas_desde: tokens emitidos ANTES deste instante deixam de valer.
--   Só é atualizada pelos motivos reset_senha e credenciais_reenviadas.
--   NULL = nenhuma sessão foi invalidada.

ALTER TABLE projection_admins
    ADD COLUMN IF NOT EXISTS deve_alterar_senha    BOOLEAN     NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS sessoes_validas_desde TIMESTAMPTZ;

ALTER TABLE projection_academias
    ADD COLUMN IF NOT EXISTS deve_alterar_senha    BOOLEAN     NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS sessoes_validas_desde TIMESTAMPTZ;

ALTER TABLE projection_estudantes
    ADD COLUMN IF NOT EXISTS deve_alterar_senha    BOOLEAN     NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS sessoes_validas_desde TIMESTAMPTZ;
