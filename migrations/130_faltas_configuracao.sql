-- Tarefa 117: limite de faltas por período e reprovação por faltas (por academia).
CREATE TABLE IF NOT EXISTS projection_faltas_configuracao (
    codigo_academia           VARCHAR(50) PRIMARY KEY,
    limite_faltas_por_periodo INTEGER,
    reprovacao_por_faltas     BOOLEAN NOT NULL DEFAULT FALSE,
    atualizado_por            UUID,
    atualizado_em             TIMESTAMPTZ,
    version                   INTEGER NOT NULL DEFAULT 0,
    last_event_id             UUID,
    CONSTRAINT chk_faltas_cfg_limite CHECK (limite_faltas_por_periodo IS NULL OR (limite_faltas_por_periodo BETWEEN 1 AND 500)),
    CONSTRAINT chk_faltas_cfg_reprovacao_exige_limite CHECK (NOT reprovacao_por_faltas OR limite_faltas_por_periodo IS NOT NULL)
);
