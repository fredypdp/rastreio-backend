-- Tarefa 116: grupos do 4º ano médio.
-- O 4º ano médio não tem turmas: os estudantes são separados em grupos, cada um
-- com um trabalho de tema próprio. O agregado continua sendo Turma; o tema é
-- opcional e só preenchido quando nivel = '4_ano_medio' (regra no agregado).
ALTER TABLE projection_turmas ADD COLUMN IF NOT EXISTS tema_trabalho TEXT;
