-- 146_servicos_catalogo_fixo.sql
--
-- Parte 7 do Plano de Atualizações 1 — Serviços: catálogo fixo no código
-- (Transporte e Atividade extracurricular).
--
-- A academia deixa de criar e personalizar serviços genéricos. O catálogo e a
-- configuração de cada serviço nascem em tabelas CRUD (regra 17: a auditoria
-- vai para auditoria_crud, na mesma transação). A solicitação, a aprovação e
-- o vínculo continuam em event sourcing (projection_solicitacoes_servico_extra).
--
-- Não há academias cadastradas, então não há migração de dados: as tabelas
-- antigas (serviço genérico e categoria) saem.
--
-- Regra 18: estas tabelas são CRUD e NÃO apontam por chave estrangeira para
-- tabelas reconstruídas a partir do ledger (academia, estudante). Só há chaves
-- estrangeiras entre as próprias tabelas CRUD.

BEGIN;

DROP TABLE IF EXISTS projection_servicos_extras;
DROP TABLE IF EXISTS projection_categorias_servico;

-- ---------------------------------------------------------------------------
-- Transporte: um por academia.
-- preco_modo / prazo_modo = 'nenhum' quer dizer "não definido no geral": cada
-- rota define o seu. Definido no geral, nenhuma rota pode definir o campo.
-- ---------------------------------------------------------------------------
CREATE TABLE servico_transporte (
    id                                UUID PRIMARY KEY,
    codigo_academia                   VARCHAR(50) NOT NULL UNIQUE,
    ativo                             BOOLEAN NOT NULL DEFAULT true,
    preco_modo                        VARCHAR(10) NOT NULL DEFAULT 'nenhum'
                                      CHECK (preco_modo IN ('nenhum', 'gratuito', 'valor')),
    preco_valor                       NUMERIC(14,2),
    prazo_modo                        VARCHAR(10) NOT NULL DEFAULT 'nenhum'
                                      CHECK (prazo_modo IN ('nenhum', 'gratuito', 'meses')),
    prazo_meses                       SMALLINT[] NOT NULL DEFAULT ARRAY[]::SMALLINT[],
    valor_taxa_inscricao              NUMERIC(14,2),
    metodos_pagamento                 TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    metodos_pagamento_taxa_inscricao  TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    anos_academicos_disponiveis       TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    cursos_disponiveis                TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    created_at                        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                        TIMESTAMPTZ NOT NULL DEFAULT now(),
    version                           INTEGER NOT NULL DEFAULT 1,
    CONSTRAINT chk_transporte_preco CHECK (
        (preco_modo = 'valor' AND preco_valor IS NOT NULL AND preco_valor > 0)
        OR (preco_modo <> 'valor' AND preco_valor IS NULL)),
    CONSTRAINT chk_transporte_prazo CHECK (
        (prazo_modo = 'meses' AND cardinality(prazo_meses) BETWEEN 1 AND 12)
        OR (prazo_modo <> 'meses' AND cardinality(prazo_meses) = 0)),
    CONSTRAINT chk_transporte_taxa CHECK (valor_taxa_inscricao IS NULL OR valor_taxa_inscricao > 0)
);

CREATE TABLE servico_transporte_rotas (
    id                UUID PRIMARY KEY,
    transporte_id     UUID NOT NULL REFERENCES servico_transporte(id) ON DELETE CASCADE,
    nome              VARCHAR(100) NOT NULL,
    horario_embarque  CHAR(5) NOT NULL CHECK (horario_embarque ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    preco_modo        VARCHAR(10) NOT NULL DEFAULT 'nenhum'
                      CHECK (preco_modo IN ('nenhum', 'gratuito', 'valor')),
    preco_valor       NUMERIC(14,2),
    prazo_meses       SMALLINT[] NOT NULL DEFAULT ARRAY[]::SMALLINT[],
    ativo             BOOLEAN NOT NULL DEFAULT true,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    version           INTEGER NOT NULL DEFAULT 1,
    CONSTRAINT chk_rota_preco CHECK (
        (preco_modo = 'valor' AND preco_valor IS NOT NULL AND preco_valor > 0)
        OR (preco_modo <> 'valor' AND preco_valor IS NULL)),
    CONSTRAINT chk_rota_prazo CHECK (cardinality(prazo_meses) BETWEEN 0 AND 12)
);
CREATE UNIQUE INDEX ux_transporte_rota_nome ON servico_transporte_rotas (transporte_id, lower(nome)) WHERE ativo;
CREATE INDEX idx_transporte_rotas_transporte ON servico_transporte_rotas (transporte_id);

CREATE TABLE servico_transporte_veiculos (
    id          UUID PRIMARY KEY,
    rota_id     UUID NOT NULL REFERENCES servico_transporte_rotas(id) ON DELETE CASCADE,
    nome        VARCHAR(100) NOT NULL,
    ativo       BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    version     INTEGER NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX ux_transporte_veiculo_nome ON servico_transporte_veiculos (rota_id, lower(nome)) WHERE ativo;
CREATE INDEX idx_transporte_veiculos_rota ON servico_transporte_veiculos (rota_id);

-- ---------------------------------------------------------------------------
-- Atividade extracurricular: várias por academia.
-- gratuito = true: nenhum mês é cobrado (preco e prazo ficam vazios).
-- ---------------------------------------------------------------------------
CREATE TABLE servico_atividades (
    id                                UUID PRIMARY KEY,
    codigo_academia                   VARCHAR(50) NOT NULL,
    nome                              VARCHAR(150) NOT NULL,
    instrutor                         VARCHAR(150) NOT NULL,
    coletiva                          BOOLEAN NOT NULL DEFAULT false,
    gratuito                          BOOLEAN NOT NULL DEFAULT false,
    preco                             NUMERIC(14,2),
    prazo_meses                       SMALLINT[] NOT NULL DEFAULT ARRAY[]::SMALLINT[],
    valor_taxa_inscricao              NUMERIC(14,2),
    metodos_pagamento                 TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    metodos_pagamento_taxa_inscricao  TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    anos_academicos_disponiveis       TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    cursos_disponiveis                TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    ativo                             BOOLEAN NOT NULL DEFAULT true,
    created_at                        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                        TIMESTAMPTZ NOT NULL DEFAULT now(),
    version                           INTEGER NOT NULL DEFAULT 1,
    CONSTRAINT chk_atividade_preco CHECK (
        (gratuito AND preco IS NULL AND cardinality(prazo_meses) = 0)
        OR (NOT gratuito AND preco IS NOT NULL AND preco > 0 AND cardinality(prazo_meses) BETWEEN 1 AND 12)),
    CONSTRAINT chk_atividade_taxa CHECK (valor_taxa_inscricao IS NULL OR valor_taxa_inscricao > 0)
);
CREATE UNIQUE INDEX ux_atividade_nome ON servico_atividades (codigo_academia, lower(nome)) WHERE ativo;
CREATE INDEX idx_atividades_academia ON servico_atividades (codigo_academia, ativo);

-- Horários: dia_semana 1 (segunda) a 7 (domingo), hora de início e de fim.
CREATE TABLE servico_atividade_horarios (
    id            UUID PRIMARY KEY,
    atividade_id  UUID NOT NULL REFERENCES servico_atividades(id) ON DELETE CASCADE,
    dia_semana    SMALLINT NOT NULL CHECK (dia_semana BETWEEN 1 AND 7),
    hora_inicio   CHAR(5) NOT NULL CHECK (hora_inicio ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    hora_fim      CHAR(5) NOT NULL CHECK (hora_fim ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    CONSTRAINT chk_horario_ordem CHECK (hora_fim > hora_inicio)
);
CREATE INDEX idx_atividade_horarios_atividade ON servico_atividade_horarios (atividade_id);

-- Turma da atividade (só atividade coletiva). Não é a Turma acadêmica.
CREATE TABLE servico_atividade_turmas (
    id            UUID PRIMARY KEY,
    atividade_id  UUID NOT NULL REFERENCES servico_atividades(id) ON DELETE CASCADE,
    nome          VARCHAR(100) NOT NULL,
    ativo         BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    version       INTEGER NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX ux_atividade_turma_nome ON servico_atividade_turmas (atividade_id, lower(nome)) WHERE ativo;

-- Membros da turma da atividade. codigo_estudante é referência por código
-- (sem chave estrangeira: o estudante é reconstruído a partir do ledger).
CREATE TABLE servico_atividade_turma_membros (
    turma_id          UUID NOT NULL REFERENCES servico_atividade_turmas(id) ON DELETE CASCADE,
    codigo_estudante  VARCHAR(20) NOT NULL,
    solicitacao_id    UUID NOT NULL,
    desde             TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (turma_id, codigo_estudante)
);
CREATE INDEX idx_atividade_turma_membros_estudante ON servico_atividade_turma_membros (codigo_estudante);

-- ---------------------------------------------------------------------------
-- A solicitação (event sourcing) passa a dizer o que foi pedido: o tipo do
-- serviço e, no transporte, a rota e os veículos; na atividade coletiva, a
-- turma escolhida na aprovação. servico_extra_id continua apontando para o
-- serviço (o id do transporte da academia ou o da atividade).
-- ---------------------------------------------------------------------------
ALTER TABLE projection_solicitacoes_servico_extra
    ADD COLUMN IF NOT EXISTS tipo_servico VARCHAR(12) NOT NULL DEFAULT 'atividade'
        CHECK (tipo_servico IN ('transporte', 'atividade')),
    ADD COLUMN IF NOT EXISTS rota_id UUID,
    ADD COLUMN IF NOT EXISTS veiculo_ids UUID[] NOT NULL DEFAULT ARRAY[]::UUID[],
    ADD COLUMN IF NOT EXISTS turma_id UUID;

COMMIT;

DO $$ BEGIN
    RAISE NOTICE 'MIGRATION 146 — catálogo fixo de serviços (Transporte e Atividade extracurricular)';
END $$;
