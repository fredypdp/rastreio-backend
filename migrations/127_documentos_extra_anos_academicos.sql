-- 127_documentos_extra_anos_academicos.sql
--
-- Permite que um DocumentoExtra se aplique a MAIS DE UM ano_academico
-- simultaneamente (ex.: o mesmo "Atestado médico" exigido tanto na 9ª
-- Classe quanto no 1º Ano Médio), em vez de exigir uma definição separada
-- por ano. Mesmo padrão já usado em projection_cursos.anos_academicos e
-- projection_materias.anos_academicos (ver migrations 011/014): coluna
-- JSONB (array de strings) + índice GIN para containment (@>).
--
-- A coluna "nivel" é removida: com múltiplos anos por definição, um único
-- documento pode abranger mais de um nível (ex.: fundamental + médio), o
-- que torna uma coluna "nivel" (singular) inconsistente. O nível de cada
-- ano específico continua disponível sob demanda via
-- aggregates.NivelDoAnoAcademico(ano) sempre que necessário — nunca foi
-- persistido como fonte de verdade (já era derivado de ano_academico).
BEGIN;

ALTER TABLE projection_documentos_extra ADD COLUMN anos_academicos JSONB;

-- Backfill: cada linha existente vira um array de um único elemento,
-- preservando 100% dos dados já cadastrados.
UPDATE projection_documentos_extra
SET anos_academicos = jsonb_build_array(ano_academico)
WHERE anos_academicos IS NULL;

ALTER TABLE projection_documentos_extra ALTER COLUMN anos_academicos SET NOT NULL;
ALTER TABLE projection_documentos_extra ADD CONSTRAINT chk_documentos_extra_anos_academicos_nao_vazio
    CHECK (jsonb_typeof(anos_academicos) = 'array' AND jsonb_array_length(anos_academicos) > 0);

CREATE INDEX IF NOT EXISTS idx_documentos_extra_anos_academicos
    ON projection_documentos_extra USING GIN (anos_academicos);

-- A unicidade antiga era por (academia, ano_academico, rótulo) — fazia
-- sentido quando cada linha cobria só um ano. Com anos_academicos array,
-- checar sobreposição de arrays via constraint de banco exigiria um
-- mecanismo que este projeto não usa em nenhum outro lugar (EXCLUDE com
-- operador de overlap não é suportado nativamente para JSONB). Decisão:
-- simplificar a unicidade para (academia, rótulo) enquanto ativo — um
-- mesmo rótulo não pode ter duas definições ATIVAS ao mesmo tempo para a
-- mesma academia, independentemente dos anos. Quem precisar do mesmo
-- rótulo para conjuntos de anos diferentes com regras diferentes deve usar
-- um rótulo distinto (ex.: "Atestado médico (Ensino Médio)").
DROP INDEX IF EXISTS ux_documentos_extra_rotulo_ano_ativo;
CREATE UNIQUE INDEX IF NOT EXISTS ux_documentos_extra_rotulo_ativo
    ON projection_documentos_extra (codigo_academia, lower(rotulo))
    WHERE ativo = true;

ALTER TABLE projection_documentos_extra DROP COLUMN ano_academico;
ALTER TABLE projection_documentos_extra DROP COLUMN nivel;

COMMIT;
