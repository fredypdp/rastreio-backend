-- ============================================================================
-- MIGRATION 144 — Turmas e sumários passam a CRUD (Parte 2B, entrega 2B-1)
-- ============================================================================
--
-- projection_turmas e projection_sumarios deixam de ser projeções do ledger:
-- viram tabelas de registro (nada se migra: as linhas atuais continuam). Quem
-- grava é internal/cadastros, com auditoria em auditoria_crud (migration 142).
--
-- Regra 17 (descoberta na Tarefa 133): tabela CRUD não pode ter chave
-- estrangeira para uma tabela que o ledger reconstrói. As duas tabelas tinham
-- chave para projection_academias com ON DELETE CASCADE, e a reconstrução das
-- academias faz TRUNCATE ... CASCADE — apagaria todas as turmas e sumários, que
-- agora não vêm do ledger. A existência da academia passa a ser exigida pelo
-- código, na mesma transação da gravação.
--
-- Ficam as chaves entre tabelas CRUD (turmas -> cursos, sumários -> cursos e
-- matérias) e as chaves de projeções PARA tabelas CRUD (projection_faltas ->
-- projection_sumarios): o inverso é seguro.
-- ============================================================================

BEGIN;

ALTER TABLE projection_turmas
    DROP CONSTRAINT IF EXISTS projection_turmas_codigo_academia_fkey;

ALTER TABLE projection_sumarios
    DROP CONSTRAINT IF EXISTS projection_sumarios_codigo_academia_fkey;

-- A projeção de sumários deixa de existir. A de turmas continua registrada só
-- para aplicar o efeito da avaliação final (estudante aprovado muda de turma),
-- por isso o checkpoint 'turmas' é mantido.
DELETE FROM projection_checkpoints WHERE projection_name = 'sumarios';

COMMENT ON TABLE projection_turmas IS
    'Tabela de registro de turmas e grupos (CRUD, Tarefa 134). Gravada por internal/cadastros com auditoria em auditoria_crud. NÃO é reconstruída a partir do ledger.';
COMMENT ON TABLE projection_sumarios IS
    'Tabela de registro de sumários de aula (CRUD, Tarefa 134). Gravada por internal/cadastros com auditoria em auditoria_crud. NÃO é reconstruída a partir do ledger.';

COMMIT;
