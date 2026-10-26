-- Tarefa 121: job de reconciliação de cobranças pendentes e snapshot ao editar
-- credenciais.
--
-- 1. financeiro_cobrancas.emitida_em: momento em que a cobrança foi emitida. É
--    a "idade" que a política de reconciliação usa para decidir quando
--    reconsultar o provedor e quando desistir. updated_at NÃO serve: é
--    reescrito a cada evento e em todo rebuild da projeção. A projeção passa a
--    gravar o occurred_at do primeiro evento da cobrança; aqui o backfill usa o
--    primeiro evento do ledger de cada cobrança já existente.
-- 2. financeiro_credenciais_arquivadas.motivo: por que o snapshot existe
--    ('removida' = credenciais removidas com pendentes, Tarefa 120; 'editada' =
--    credenciais editadas com pendentes, Tarefa 121). Só informativo (a tela
--    explica o que aconteceu); a resolução e o descarte não dependem dele.
BEGIN;

ALTER TABLE financeiro_cobrancas ADD COLUMN emitida_em TIMESTAMP;

UPDATE financeiro_cobrancas c
SET emitida_em = (SELECT MIN(l.occurred_at) FROM spuri_ledger l WHERE l.aggregate_id = c.id);
UPDATE financeiro_cobrancas SET emitida_em = updated_at WHERE emitida_em IS NULL;

ALTER TABLE financeiro_credenciais_arquivadas
    ADD COLUMN motivo VARCHAR(16) NOT NULL DEFAULT 'removida'
        CHECK (motivo IN ('removida','editada'));

COMMIT;

COMMENT ON COLUMN financeiro_cobrancas.emitida_em IS
 'Momento de emissão da cobrança (occurred_at do primeiro evento). Base da idade usada pela reconciliação automática.';
COMMENT ON COLUMN financeiro_credenciais_arquivadas.motivo IS
 'Origem do snapshot: removida (credenciais removidas com pendentes) ou editada (credenciais editadas com pendentes).';
