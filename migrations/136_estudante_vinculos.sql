-- 136_estudante_vinculos.sql
-- SEC-15: Projeção de vínculos estudante↔academia para isolamento de academia.
-- Permite consultar histórico completo de vínculos (incluindo deleções) para
-- garantir que uma academia só acesse estudantes que já estiveram vinculados a ela.

CREATE TABLE IF NOT EXISTS projection_estudante_vinculos (
    id                          BIGSERIAL PRIMARY KEY,
    estudante_id                UUID        NOT NULL REFERENCES projection_estudantes(id) ON DELETE CASCADE,
    codigo_estudante            VARCHAR     NOT NULL,
    codigo_academia             VARCHAR     NOT NULL,
    tipo_ensino                 VARCHAR     NOT NULL,           -- 'fundamental' | 'medio' | 'superior'
    ano_academico               VARCHAR     NOT NULL,           -- ex: '1_ano_fundamental', '2_ano_medio', '1_semestre'
    evento_tipo                 VARCHAR     NOT NULL,           -- 'EstudanteCriadoComVinculo', 'EstudanteReintegrado', 'EstudanteDesvinculadoDaAcademia'
    evento_version              INT         NOT NULL,
    vinculado_em                TIMESTAMPTZ NOT NULL,           -- recorded_at do evento
    desvinculado_em             TIMESTAMPTZ,                    -- recorded_at do evento de desvinculação (NULL = vínculo ativo)
    deleted_at                  TIMESTAMPTZ,                    -- preenchido quando evento_tipo = 'EstudanteDesvinculadoDaAcademia'
    deleted_by                  UUID                            -- quem desvinculou/deletou
);

-- Índice para consultas por academia (padrão de acesso mais comum)
CREATE INDEX IF NOT EXISTS idx_projection_estudante_vinculos_academia
    ON projection_estudante_vinculos (codigo_academia);

-- Índice para consultas por estudante
CREATE INDEX IF NOT EXISTS idx_projection_estudante_vinculos_estudante
    ON projection_estudante_vinculos (estudante_id);

-- Índice para consultas por estudante + academia
CREATE INDEX IF NOT EXISTS idx_projection_estudante_vinculos_estudante_academia
    ON projection_estudante_vinculos (codigo_estudante, codigo_academia);

-- Constraint para evitar duplicatas do mesmo vínculo ativo
-- (um estudante não pode ter dois vínculos ativos com a mesma academia simultaneamente)
CREATE UNIQUE INDEX IF NOT EXISTS uq_projection_estudante_vinculos_ativo
    ON projection_estudante_vinculos (codigo_estudante, codigo_academia)
    WHERE desvinculado_em IS NULL;

-- Comentários para documentação
COMMENT ON TABLE projection_estudante_vinculos IS 'Histórico completo de vínculos estudante↔academia para isolamento de academia (SEC-15). Inclui vínculos ativos e históricos (desvinculados). Reconstruída a partir do ledger via eventos EstudanteCriadoComVinculo, EstudanteReintegrado, EstudanteDesvinculadoDaAcademia.';
COMMENT ON COLUMN projection_estudante_vinculos.evento_tipo IS 'Tipo do evento do ledger que originou este registro: EstudanteCriadoComVinculo (vínculo inicial), EstudanteReintegrado (reativação), EstudanteDesvinculadoDaAcademia (desvinculação).';
COMMENT ON COLUMN projection_estudante_vinculos.desvinculado_em IS 'Timestamp da desvinculação (NULL = vínculo ativo no momento).';
COMMENT ON COLUMN projection_estudante_vinculos.deleted_at IS 'Mesmo valor de desvinculado_em quando evento_tipo = EstudanteDesvinculadoDaAcademia; NULL caso contrário.';
COMMENT ON COLUMN projection_estudante_vinculos.deleted_by IS 'UUID de quem executou a desvinculação (estudante ou academia).';