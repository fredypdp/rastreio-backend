-- 140_contas_secretario_professor_encarregado.sql
-- Plano de Atualizações, Parte 4 — três novos tipos de login.
--
-- projection_contas_pessoa guarda os três tipos numa só tabela (coluna "tipo"):
-- têm exatamente o mesmo ciclo de conta (código, senha, e-mail, 2FA, status).
-- Assim o login, o middleware e a verificação de e-mail único consultam um
-- lugar só. A tabela é consequência do ledger e é reconstruída por rebuild.
--
-- Códigos de login: SC123456 (secretário), PF123456 (professor) e EN123456
-- (encarregado) — duas letras e seis dígitos, formato que não colide com o do
-- estudante (3 letras + 4 dígitos) nem com o da academia (província + ano + seq).

CREATE TABLE IF NOT EXISTS projection_contas_pessoa (
    id                    UUID         PRIMARY KEY,
    tipo                  VARCHAR(20)  NOT NULL CHECK (tipo IN ('secretario', 'professor', 'encarregado')),
    codigo                VARCHAR(20)  NOT NULL,
    nome                  VARCHAR(255) NOT NULL,
    genero                VARCHAR(10)  NOT NULL,
    data_nascimento       DATE         NOT NULL,
    email                 VARCHAR(255),
    email_verificado      BOOLEAN      NOT NULL DEFAULT FALSE,
    telefone              VARCHAR(20)  NOT NULL,
    telefone_verificado   BOOLEAN      NOT NULL DEFAULT FALSE,
    senha_hash            VARCHAR(255) NOT NULL,
    status                VARCHAR(20)  NOT NULL DEFAULT 'ativo' CHECK (status IN ('ativo', 'inativo', 'deletado')),
    -- Só secretário e professor pertencem a uma academia.
    academia_id           UUID,
    codigo_academia       VARCHAR(50),
    -- Só secretário: conjunto normalizado de permissões (ver internal/permissoes).
    permissoes            TEXT[]       NOT NULL DEFAULT '{}',
    deve_alterar_senha    BOOLEAN      NOT NULL DEFAULT FALSE,
    sessoes_validas_desde TIMESTAMPTZ,
    dois_fatores_ativo    BOOLEAN      NOT NULL DEFAULT FALSE,
    criado_por            UUID,
    created_at            TIMESTAMPTZ  NOT NULL,
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at            TIMESTAMPTZ,
    deletado_por          UUID,
    version               INTEGER      NOT NULL DEFAULT 0,
    last_event_id         UUID
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_contas_pessoa_codigo ON projection_contas_pessoa (codigo);
-- E-mail único entre contas não eliminadas do mesmo tipo; a unicidade entre
-- tipos (e contra admin/academia/estudante) é garantida no handler, igual à
-- regra que já existe para PUT /me/email.
CREATE UNIQUE INDEX IF NOT EXISTS uq_contas_pessoa_email
    ON projection_contas_pessoa (LOWER(email)) WHERE email IS NOT NULL AND status <> 'deletado';
CREATE INDEX IF NOT EXISTS idx_contas_pessoa_tipo_academia ON projection_contas_pessoa (tipo, codigo_academia);
CREATE INDEX IF NOT EXISTS idx_contas_pessoa_telefone ON projection_contas_pessoa (tipo, telefone) WHERE status <> 'deletado';

-- Vínculos do professor: turma + matéria. Um par (turma, matéria) tem no máximo
-- UM professor ativo. Sem FK de propósito (mesma razão da migration 137): o
-- rebuild das projeções não garante a ordem entre tabelas.
CREATE TABLE IF NOT EXISTS projection_professor_vinculos (
    id              UUID         PRIMARY KEY,
    professor_id    UUID         NOT NULL,
    codigo_academia VARCHAR(50)  NOT NULL,
    codigo_turma    VARCHAR(50)  NOT NULL,
    materia_id      UUID         NOT NULL,
    atribuido_por   UUID,
    atribuido_em    TIMESTAMPTZ  NOT NULL,
    removido_em     TIMESTAMPTZ,
    removido_por    UUID,
    motivo_remocao  TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_professor_vinculo_par_ativo
    ON projection_professor_vinculos (codigo_academia, codigo_turma, materia_id) WHERE removido_em IS NULL;
CREATE INDEX IF NOT EXISTS idx_professor_vinculos_professor
    ON projection_professor_vinculos (professor_id) WHERE removido_em IS NULL;

-- Educandos do encarregado. Um estudante tem UM encarregado ativo; um
-- encarregado pode ter vários educandos, inclusive de academias diferentes.
CREATE TABLE IF NOT EXISTS projection_encarregado_educandos (
    id                 BIGSERIAL    PRIMARY KEY,
    encarregado_id     UUID         NOT NULL,
    estudante_id       UUID         NOT NULL,
    codigo_estudante   VARCHAR(20)  NOT NULL,
    relacao            VARCHAR(10),
    relacao_outro      VARCHAR(60),
    vinculado_em       TIMESTAMPTZ  NOT NULL,
    vinculado_por      UUID,
    desvinculado_em    TIMESTAMPTZ,
    desvinculado_por   UUID,
    motivo_desvinculo  VARCHAR(40)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_encarregado_educando_estudante_ativo
    ON projection_encarregado_educandos (estudante_id) WHERE desvinculado_em IS NULL;
CREATE INDEX IF NOT EXISTS idx_encarregado_educandos_encarregado
    ON projection_encarregado_educandos (encarregado_id) WHERE desvinculado_em IS NULL;

-- Os tokens de e-mail (verificação e recuperação de senha) e os desafios do 2FA
-- passam a aceitar os três tipos novos.
ALTER TABLE auth_tokens DROP CONSTRAINT IF EXISTS auth_tokens_user_type_check;
ALTER TABLE auth_tokens ADD CONSTRAINT auth_tokens_user_type_check
    CHECK (user_type IN ('estudante', 'academia', 'admin', 'secretario', 'professor', 'encarregado'));

ALTER TABLE dois_fatores_desafios DROP CONSTRAINT IF EXISTS chk_dois_fatores_user_type;
ALTER TABLE dois_fatores_desafios ADD CONSTRAINT chk_dois_fatores_user_type
    CHECK (user_type IN ('admin', 'academia', 'estudante', 'secretario', 'professor', 'encarregado'));

COMMENT ON TABLE projection_contas_pessoa IS 'Projeção de secretários, professores e encarregados (Parte 4 do plano). Reconstruída a partir do ledger.';
COMMENT ON TABLE projection_professor_vinculos IS 'Vínculos professor ↔ (turma, matéria). Um professor ativo por par.';
COMMENT ON TABLE projection_encarregado_educandos IS 'Vínculos encarregado ↔ estudante. Um encarregado ativo por estudante.';
