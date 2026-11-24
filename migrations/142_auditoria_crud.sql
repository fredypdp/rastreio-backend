-- ============================================================================
-- MIGRATION 142 — Auditoria do que fica em CRUD (Parte 2B, entrega 2B-1)
-- ============================================================================
--
-- Contexto: pela regra 17 do Plano de Atualizações 1, só o que é sensível
-- continua em event sourcing. O resto passa a ser CRUD e, em vez do ledger,
-- a auditoria ("quem, quando, o quê") fica nesta tabela, gravada NA MESMA
-- transação da alteração (internal/db/auditoria_crud.go).
--
-- Garantias:
--   * Só inserção: triggers BEFORE UPDATE/DELETE/TRUNCATE bloqueiam qualquer
--     role. (Como na migration 091, a aplicação e as migrations compartilham a
--     mesma role, então o controle preventivo é o trigger, não só GRANT/REVOKE.)
--   * Hash encadeado POR ENTIDADE, como o ledger: cada linha guarda o hash da
--     anterior da mesma entidade e uma sequência contígua (1, 2, 3...). Um
--     advisory lock por entidade serializa as gravações concorrentes, e o
--     índice único (entidade_tipo, entidade_id, sequencia) impede bifurcação.
--   * O hash da senha e o código de verificação NUNCA entram: um trigger
--     rejeita `antes`/`depois` que contenham chave proibida, em qualquer nível.
--     A lista tem de ser igual à de ChavesProibidasAuditoria (Go); há teste.
--
-- Nada é migrado: não há academias cadastradas e nenhum agregado foi
-- convertido ainda. As conversões vêm nas tarefas seguintes da Parte 2B.
-- ============================================================================

BEGIN;

CREATE TABLE IF NOT EXISTS auditoria_crud (
    id              BIGSERIAL PRIMARY KEY,
    audit_id        UUID         NOT NULL DEFAULT gen_random_uuid(),
    entidade_tipo   VARCHAR(80)  NOT NULL CHECK (btrim(entidade_tipo) <> ''),
    entidade_id     VARCHAR(80)  NOT NULL CHECK (btrim(entidade_id) <> ''),
    academia_id     UUID,
    acao            VARCHAR(120) NOT NULL CHECK (btrim(acao) <> ''),
    ator_id         VARCHAR(80)  NOT NULL DEFAULT '',
    ator_tipo       VARCHAR(40)  NOT NULL DEFAULT '',
    -- Quem REALMENTE agiu quando a ação foi feita em nome de outra conta
    -- (secretário como academia, encarregado como educando). Vazio = o próprio.
    em_nome_de_id   VARCHAR(80)  NOT NULL DEFAULT '',
    em_nome_de_tipo VARCHAR(40)  NOT NULL DEFAULT '',
    ip              VARCHAR(64)  NOT NULL DEFAULT '',
    antes           JSONB,
    depois          JSONB,
    motivo          TEXT         NOT NULL DEFAULT '',
    ocorrido_em     TIMESTAMPTZ  NOT NULL DEFAULT clock_timestamp(),
    sequencia       INTEGER      NOT NULL,
    hash_anterior   VARCHAR(64),
    hash            VARCHAR(64)  NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_auditoria_crud_audit_id
    ON auditoria_crud (audit_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_auditoria_crud_entidade_sequencia
    ON auditoria_crud (entidade_tipo, entidade_id, sequencia);
CREATE INDEX IF NOT EXISTS idx_auditoria_crud_academia
    ON auditoria_crud (academia_id, ocorrido_em DESC)
    WHERE academia_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_auditoria_crud_ator
    ON auditoria_crud (ator_tipo, ator_id, ocorrido_em DESC);

COMMENT ON TABLE auditoria_crud IS
    'Auditoria do que fica em CRUD (Parte 2B). Só inserção, hash encadeado por '
    'entidade, gravada na mesma transação da alteração. Nunca guarda hash de '
    'senha nem código de verificação.';

-- Chaves que nunca podem aparecer em antes/depois (em qualquer nível, sem
-- diferenciar maiúsculas de minúsculas). Mesma lista de ChavesProibidasAuditoria.
CREATE OR REPLACE FUNCTION auditoria_crud_chave_proibida(p_valor JSONB)
RETURNS TEXT AS $$
DECLARE
    v_proibidas CONSTANT TEXT[] := ARRAY[
        'senha', 'senha_hash', 'password', 'password_hash', 'hash_senha',
        'codigo', 'codigo_verificacao', 'codigo_2fa',
        'token', 'segredo', 'secret'
    ];
    v_par   RECORD;
    v_item  JSONB;
    v_achou TEXT;
BEGIN
    IF p_valor IS NULL THEN
        RETURN NULL;
    END IF;

    IF jsonb_typeof(p_valor) = 'object' THEN
        FOR v_par IN SELECT key, value FROM jsonb_each(p_valor) LOOP
            IF lower(v_par.key) = ANY (v_proibidas) THEN
                RETURN v_par.key;
            END IF;
            v_achou := auditoria_crud_chave_proibida(v_par.value);
            IF v_achou IS NOT NULL THEN
                RETURN v_achou;
            END IF;
        END LOOP;
    ELSIF jsonb_typeof(p_valor) = 'array' THEN
        FOR v_item IN SELECT value FROM jsonb_array_elements(p_valor) LOOP
            v_achou := auditoria_crud_chave_proibida(v_item);
            IF v_achou IS NOT NULL THEN
                RETURN v_achou;
            END IF;
        END LOOP;
    END IF;

    RETURN NULL;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- Hash da linha: protege tudo o que a auditoria afirma (quem, quando, o quê,
-- antes e depois, motivo), a sequência e o hash anterior.
CREATE OR REPLACE FUNCTION auditoria_crud_calcular_hash(
    p_audit_id        UUID,
    p_entidade_tipo   VARCHAR,
    p_entidade_id     VARCHAR,
    p_academia_id     UUID,
    p_acao            VARCHAR,
    p_ator_id         VARCHAR,
    p_ator_tipo       VARCHAR,
    p_em_nome_de_id   VARCHAR,
    p_em_nome_de_tipo VARCHAR,
    p_ip              VARCHAR,
    p_antes           JSONB,
    p_depois          JSONB,
    p_motivo          TEXT,
    p_ocorrido_em     TIMESTAMPTZ,
    p_sequencia       INTEGER,
    p_hash_anterior   VARCHAR
) RETURNS VARCHAR AS $$
BEGIN
    RETURN encode(
        digest(
            concat_ws('|',
                p_audit_id::text,
                p_entidade_tipo,
                p_entidade_id,
                COALESCE(p_academia_id::text, ''),
                p_acao,
                p_ator_id,
                p_ator_tipo,
                p_em_nome_de_id,
                p_em_nome_de_tipo,
                p_ip,
                COALESCE(p_antes::text, 'null'),
                COALESCE(p_depois::text, 'null'),
                p_motivo,
                (extract(epoch FROM p_ocorrido_em) * 1000000)::bigint::text,
                p_sequencia::text,
                COALESCE(p_hash_anterior, '')
            ),
            'sha256'
        ),
        'hex'
    );
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION auditoria_crud_antes_de_inserir()
RETURNS TRIGGER AS $$
DECLARE
    v_chave    TEXT;
    v_ultimo   RECORD;
BEGIN
    v_chave := auditoria_crud_chave_proibida(NEW.antes);
    IF v_chave IS NULL THEN
        v_chave := auditoria_crud_chave_proibida(NEW.depois);
    END IF;
    IF v_chave IS NOT NULL THEN
        RAISE EXCEPTION
            'auditoria_crud não aceita a chave "%" (senha, hash de senha e código de verificação nunca entram na auditoria).', v_chave
            USING ERRCODE = '45001';
    END IF;

    -- Serializa as gravações da MESMA entidade até o fim da transação, para
    -- que duas concorrentes não leiam o mesmo "último" e bifurquem a cadeia.
    PERFORM pg_advisory_xact_lock(
        hashtextextended('auditoria_crud:' || NEW.entidade_tipo || ':' || NEW.entidade_id, 0)
    );

    SELECT sequencia, hash INTO v_ultimo
    FROM auditoria_crud
    WHERE entidade_tipo = NEW.entidade_tipo
      AND entidade_id   = NEW.entidade_id
    ORDER BY sequencia DESC
    LIMIT 1;

    NEW.sequencia     := COALESCE(v_ultimo.sequencia, 0) + 1;
    NEW.hash_anterior := v_ultimo.hash;
    NEW.hash := auditoria_crud_calcular_hash(
        NEW.audit_id, NEW.entidade_tipo, NEW.entidade_id, NEW.academia_id,
        NEW.acao, NEW.ator_id, NEW.ator_tipo, NEW.em_nome_de_id,
        NEW.em_nome_de_tipo, NEW.ip, NEW.antes, NEW.depois, NEW.motivo,
        NEW.ocorrido_em, NEW.sequencia, NEW.hash_anterior
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS auditoria_crud_inserir ON auditoria_crud;
CREATE TRIGGER auditoria_crud_inserir
    BEFORE INSERT ON auditoria_crud
    FOR EACH ROW
    EXECUTE FUNCTION auditoria_crud_antes_de_inserir();

CREATE OR REPLACE FUNCTION auditoria_crud_somente_insercao()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION
        'auditoria_crud é append-only. Operação % não permitida.', TG_OP
        USING ERRCODE = '45000';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS auditoria_crud_bloquear_update ON auditoria_crud;
CREATE TRIGGER auditoria_crud_bloquear_update
    BEFORE UPDATE ON auditoria_crud
    FOR EACH ROW
    EXECUTE FUNCTION auditoria_crud_somente_insercao();

DROP TRIGGER IF EXISTS auditoria_crud_bloquear_delete ON auditoria_crud;
CREATE TRIGGER auditoria_crud_bloquear_delete
    BEFORE DELETE ON auditoria_crud
    FOR EACH ROW
    EXECUTE FUNCTION auditoria_crud_somente_insercao();

DROP TRIGGER IF EXISTS auditoria_crud_bloquear_truncate ON auditoria_crud;
CREATE TRIGGER auditoria_crud_bloquear_truncate
    BEFORE TRUNCATE ON auditoria_crud
    FOR EACH STATEMENT
    EXECUTE FUNCTION auditoria_crud_somente_insercao();

-- Verificação da cadeia de UMA entidade (equivalente a verify_hash_chain).
CREATE OR REPLACE FUNCTION verificar_cadeia_auditoria_crud(
    p_entidade_tipo VARCHAR,
    p_entidade_id   VARCHAR
)
RETURNS TABLE(
    is_valid            BOOLEAN,
    quebrada_na_sequencia INTEGER,
    message             TEXT
) AS $$
DECLARE
    v_linha            RECORD;
    v_hash_atual       VARCHAR(64);
    v_sequencia_esperada INTEGER := 1;
    v_hash_esperado    VARCHAR(64);
BEGIN
    FOR v_linha IN
        SELECT *
        FROM auditoria_crud
        WHERE entidade_tipo = p_entidade_tipo
          AND entidade_id   = p_entidade_id
        ORDER BY sequencia ASC
    LOOP
        IF v_linha.sequencia <> v_sequencia_esperada THEN
            is_valid := FALSE;
            quebrada_na_sequencia := v_linha.sequencia;
            message := format('Sequência inválida: esperado %s, encontrado %s (audit_id=%s)',
                v_sequencia_esperada, v_linha.sequencia, v_linha.audit_id);
            RETURN NEXT;
            RETURN;
        END IF;

        IF v_sequencia_esperada = 1 AND v_linha.hash_anterior IS NOT NULL THEN
            is_valid := FALSE;
            quebrada_na_sequencia := v_linha.sequencia;
            message := format('A primeira linha deve ter hash_anterior NULL (audit_id=%s)', v_linha.audit_id);
            RETURN NEXT;
            RETURN;
        END IF;

        IF v_sequencia_esperada > 1 AND v_linha.hash_anterior IS DISTINCT FROM v_hash_atual THEN
            is_valid := FALSE;
            quebrada_na_sequencia := v_linha.sequencia;
            message := format('Cadeia quebrada na sequência %s (audit_id=%s)', v_linha.sequencia, v_linha.audit_id);
            RETURN NEXT;
            RETURN;
        END IF;

        v_hash_esperado := auditoria_crud_calcular_hash(
            v_linha.audit_id, v_linha.entidade_tipo, v_linha.entidade_id, v_linha.academia_id,
            v_linha.acao, v_linha.ator_id, v_linha.ator_tipo, v_linha.em_nome_de_id,
            v_linha.em_nome_de_tipo, v_linha.ip, v_linha.antes, v_linha.depois, v_linha.motivo,
            v_linha.ocorrido_em, v_linha.sequencia, v_linha.hash_anterior
        );
        IF v_linha.hash <> v_hash_esperado THEN
            is_valid := FALSE;
            quebrada_na_sequencia := v_linha.sequencia;
            message := format('Hash inválido na sequência %s (audit_id=%s)', v_linha.sequencia, v_linha.audit_id);
            RETURN NEXT;
            RETURN;
        END IF;

        v_hash_atual := v_linha.hash;
        v_sequencia_esperada := v_sequencia_esperada + 1;
    END LOOP;

    is_valid := TRUE;
    quebrada_na_sequencia := NULL;
    message := 'Cadeia de auditoria íntegra';
    RETURN NEXT;
END;
$$ LANGUAGE plpgsql STABLE;

COMMIT;
