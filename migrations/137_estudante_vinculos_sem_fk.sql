-- 137_estudante_vinculos_sem_fk.sql
-- SEC-15 (Tarefa 127): projection_estudante_vinculos deixa de ter chave estrangeira
-- para projection_estudantes.
--
-- Motivos:
--  1. As projeções são processadas de forma independente (checkpoints próprios) e o
--     Manager não garante a ordem entre elas: um evento de vínculo podia chegar antes
--     de a projeção de estudantes inserir o estudante e falhar por violação da FK.
--  2. A reconstrução da projeção "estudantes" apagava (ON DELETE CASCADE) todos os
--     vínculos sem os reconstruir, deixando todas as academias sem acesso até alguém
--     reconstruir "estudante_vinculos" à mão.
--
-- O estudante_id continua obrigatório (NOT NULL) e indexado; a integridade vem do
-- ledger, não de uma FK entre tabelas de leitura.

DO $$
DECLARE
    fk RECORD;
BEGIN
    FOR fk IN
        SELECT conname
          FROM pg_constraint
         WHERE conrelid = 'projection_estudante_vinculos'::regclass
           AND contype = 'f'
    LOOP
        EXECUTE format('ALTER TABLE projection_estudante_vinculos DROP CONSTRAINT %I', fk.conname);
    END LOOP;
END $$;
