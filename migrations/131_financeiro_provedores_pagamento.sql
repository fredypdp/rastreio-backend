-- Tarefa 119: módulo de pagamentos dividido por provedores (PSP).
--
-- Até aqui as tabelas financeiras eram nomeadas e modeladas para a AppyPay.
-- Esta migration generaliza o modelo SEM perder dados: tudo o que já existe é
-- marcado como pertencente ao provedor APPYPAY e as academias/Spuri que já
-- possuem credenciais passam a ter a AppyPay como provedor ativo, de modo que
-- nada muda de comportamento no deploy.
--
-- 1. Credenciais e cofre de segredos passam a ser por (contexto, provedor).
-- 2. Cada cobrança grava o provedor que a emitiu (mecanismo retroativo: depois
--    de emitida, a cobrança continua sendo consultada/cancelada/confirmada
--    pelo provedor que a criou, mesmo que o provedor ativo seja trocado).
-- 3. A deduplicação de webhooks passa a ser por (provedor, event_id).
-- 4. Nova tabela com o provedor ativo de cada contexto (um único provedor,
--    sem padrão/secundário).
BEGIN;

-- 1. Credenciais -----------------------------------------------------------
ALTER TABLE financeiro_credenciais_appypay RENAME TO financeiro_credenciais_provedor;
ALTER TABLE financeiro_credenciais_provedor
    ADD COLUMN provedor VARCHAR(32) NOT NULL DEFAULT 'APPYPAY';
-- Sem DEFAULT: credenciais só são gravadas pela projeção, que informa o provedor.
ALTER TABLE financeiro_credenciais_provedor ALTER COLUMN provedor DROP DEFAULT;

DROP INDEX IF EXISTS ux_financeiro_credenciais_contexto;
CREATE UNIQUE INDEX ux_financeiro_credenciais_provedor_contexto
    ON financeiro_credenciais_provedor (contexto_tipo, COALESCE(codigo_academia, ''), ambiente, provedor);

ALTER TABLE financeiro_segredos_appypay RENAME TO financeiro_segredos_provedor;

-- 2. Cobranças -------------------------------------------------------------
-- DEFAULT 'APPYPAY' mantido de propósito: até esta tarefa a AppyPay era o único
-- provedor, então qualquer linha inserida sem o campo (inclusive o replay de
-- eventos antigos do ledger, que não trazem "provedor") é da AppyPay. A
-- projeção sempre informa o provedor explicitamente nas cobranças novas.
ALTER TABLE financeiro_cobrancas
    ADD COLUMN provedor VARCHAR(32) NOT NULL DEFAULT 'APPYPAY';

-- O identificador do provedor só é único dentro do próprio provedor.
DROP INDEX IF EXISTS ux_financeiro_cobrancas_provider_id;
CREATE UNIQUE INDEX ux_financeiro_cobrancas_provedor_provider_id
    ON financeiro_cobrancas (provedor, provider_charge_id) WHERE provider_charge_id IS NOT NULL;
CREATE INDEX ix_financeiro_cobrancas_provedor_contexto
    ON financeiro_cobrancas (provedor, contexto_tipo, COALESCE(codigo_academia, ''));

-- 3. Webhooks recebidos ----------------------------------------------------
ALTER TABLE financeiro_webhooks_recebidos
    ADD COLUMN provedor VARCHAR(32) NOT NULL DEFAULT 'APPYPAY';
ALTER TABLE financeiro_webhooks_recebidos DROP CONSTRAINT IF EXISTS financeiro_webhooks_recebidos_pkey;
ALTER TABLE financeiro_webhooks_recebidos ADD PRIMARY KEY (provedor, event_id);
-- Cada provedor tem os seus próprios métodos de notificação.
ALTER TABLE financeiro_webhooks_recebidos DROP CONSTRAINT IF EXISTS financeiro_webhooks_recebidos_metodo_check;
ALTER TABLE financeiro_webhooks_recebidos ALTER COLUMN metodo TYPE VARCHAR(32);

-- 4. Provedor ativo por contexto ------------------------------------------
CREATE TABLE financeiro_provedores_pagamento (
    contexto_tipo   VARCHAR(16) NOT NULL CHECK (contexto_tipo IN ('spuri','academia')),
    codigo_academia TEXT,
    provedor        VARCHAR(32) NOT NULL,
    definido_por    TEXT,
    definido_em     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ((contexto_tipo = 'spuri' AND codigo_academia IS NULL) OR
           (contexto_tipo = 'academia' AND codigo_academia IS NOT NULL))
);
CREATE UNIQUE INDEX ux_financeiro_provedores_pagamento_contexto
    ON financeiro_provedores_pagamento (contexto_tipo, COALESCE(codigo_academia, ''));

-- Backfill: quem já tem credenciais AppyPay continua usando a AppyPay.
INSERT INTO financeiro_provedores_pagamento (contexto_tipo, codigo_academia, provedor)
SELECT DISTINCT contexto_tipo, codigo_academia, provedor
FROM financeiro_credenciais_provedor
ON CONFLICT DO NOTHING;

COMMIT;

COMMENT ON TABLE financeiro_segredos_provedor IS
 'Cofre operacional AES-256-GCM, por credencial de provedor. Segredos e ciphertexts não pertencem ao ledger.';
COMMENT ON TABLE financeiro_provedores_pagamento IS
 'Provedor de pagamento (PSP) ativo de cada contexto financeiro. Usado apenas para cobranças NOVAS; cobranças existentes seguem o provedor gravado em financeiro_cobrancas.provedor.';
