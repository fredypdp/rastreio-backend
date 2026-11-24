-- ============================================================================
-- MIGRATION 143 — Cursos e matérias passam a CRUD (Parte 2B, entrega 2B-1)
-- ============================================================================
--
-- projection_cursos e projection_materias deixam de ser projeções do ledger:
-- viram a tabela de registro (nada se migra: as linhas atuais continuam). Quem
-- grava é internal/cadastros, com auditoria em auditoria_crud (migration 142).
--
-- Efeito colateral que precisa ser desfeito: as duas tabelas tinham chave
-- estrangeira para projection_academias com ON DELETE CASCADE, e a reconstrução
-- da projeção de academias faz TRUNCATE ... CASCADE. Com a chave, reconstruir
-- as academias APAGARIA todos os cursos e matérias — e, agora que eles não vêm
-- mais do ledger, não haveria como recuperá-los. Sem a chave, a reconstrução
-- das academias não toca nelas. A existência da academia passa a ser exigida
-- pelo código, na gravação (cadastros.exigirAcademia), na mesma transação.
--
-- Fica a chave projection_materias.curso_id -> projection_cursos (as duas são
-- CRUD) e as chaves das projeções de estudantes, turmas, sumários, notas e
-- faltas PARA cursos/matérias (a reconstrução dessas projeções não afeta cursos
-- e matérias; o inverso, o TRUNCATE de cursos/matérias, deixou de existir).
-- ============================================================================

BEGIN;

ALTER TABLE projection_cursos
    DROP CONSTRAINT IF EXISTS projection_cursos_codigo_academia_fkey;

ALTER TABLE projection_materias
    DROP CONSTRAINT IF EXISTS projection_materias_codigo_academia_fkey;

-- Não são mais projeções: o gestor de projeções não deve listá-las.
DELETE FROM projection_checkpoints WHERE projection_name IN ('cursos', 'materias');

COMMENT ON TABLE projection_cursos IS
    'Tabela de registro de cursos (CRUD, Tarefa 133). Gravada por internal/cadastros com auditoria em auditoria_crud. NÃO é reconstruída a partir do ledger.';
COMMENT ON TABLE projection_materias IS
    'Tabela de registro de matérias disciplinares (CRUD, Tarefa 133). Gravada por internal/cadastros com auditoria em auditoria_crud. NÃO é reconstruída a partir do ledger.';

COMMIT;
