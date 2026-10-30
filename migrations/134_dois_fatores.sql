-- 134_dois_fatores.sql
-- Tarefa 122: verificação em duas etapas (2FA) por e-mail, com código de 6 dígitos.
--
-- 1) Estado do 2FA nas projeções (fonte de verdade: eventos DoisFatoresAtivados /
--    DoisFatoresDesativados no ledger; a coluna é reconstruída em qualquer rebuild).
-- 2) Tabela de desafios: cada envio de código cria um desafio com id próprio.
--    O código de 6 dígitos NUNCA é chave nem é guardado em texto puro; vale pelo
--    estado do desafio (usado / substituído / expirado / tentativas), não pelo valor.
--    Dois desafios com o mesmo código por coincidência são distintos pelo id e pela
--    data de criação.

ALTER TABLE projection_admins
    ADD COLUMN IF NOT EXISTS dois_fatores_ativo BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE projection_academias
    ADD COLUMN IF NOT EXISTS dois_fatores_ativo BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE projection_estudantes
    ADD COLUMN IF NOT EXISTS dois_fatores_ativo BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS dois_fatores_desafios (
    id                 UUID PRIMARY KEY,
    user_id            UUID         NOT NULL,
    user_type          VARCHAR(20)  NOT NULL,
    finalidade         VARCHAR(20)  NOT NULL,
    codigo_hash        CHAR(64)     NOT NULL,
    email_destino      VARCHAR(255) NOT NULL,
    tentativas         SMALLINT     NOT NULL DEFAULT 0,
    iniciado_em        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    expira_em          TIMESTAMPTZ  NOT NULL,
    usado_em           TIMESTAMPTZ,
    invalidado_em      TIMESTAMPTZ,
    motivo_invalidacao VARCHAR(30),
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_dois_fatores_user_type CHECK (user_type IN ('admin', 'academia', 'estudante')),
    CONSTRAINT chk_dois_fatores_finalidade CHECK (finalidade IN ('ativacao', 'desativacao', 'login')),
    CONSTRAINT chk_dois_fatores_tentativas CHECK (tentativas >= 0)
);

-- No máximo UM desafio vivo por usuário e finalidade: pedir um código novo
-- invalida o anterior (motivo 'substituido') na mesma transação.
CREATE UNIQUE INDEX IF NOT EXISTS uq_dois_fatores_desafio_ativo
    ON dois_fatores_desafios (user_id, user_type, finalidade)
    WHERE usado_em IS NULL AND invalidado_em IS NULL;

-- Contagem de envios na última hora e intervalo mínimo entre envios.
CREATE INDEX IF NOT EXISTS idx_dois_fatores_desafios_envios
    ON dois_fatores_desafios (user_id, user_type, finalidade, created_at DESC);
