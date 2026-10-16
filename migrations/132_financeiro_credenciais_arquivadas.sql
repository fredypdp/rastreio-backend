-- Tarefa 120: credenciais removidas com cobranças pendentes deixam de ser
-- bloqueadas; ficam ARQUIVADAS (snapshot) para que o Spuri continue
-- consultando, cancelando e autenticando os webhooks dessas cobranças junto ao
-- provedor que as emitiu (retroatividade), e são descartadas quando a última
-- cobrança pendente é resolvida.
--
-- 1. Cada cobrança passa a guardar a credencial (financeiro_credenciais_provedor.id)
--    com a qual foi emitida — é o vínculo exato entre a cobrança e o seu snapshot.
--    NULL para cobranças anteriores a esta migration que não puderam ser ligadas
--    a uma credencial única: nesse caso a resolução cai para "credencial do
--    contexto + provedor" (ativa e, na falta dela, a arquivada), exatamente o
--    comportamento anterior.
-- 2. Nova tabela de credenciais arquivadas. Só guarda metadados e a visão
--    mascarada; os segredos continuam no cofre (financeiro_segredos_provedor),
--    que é mantido enquanto a credencial estiver arquivada.
BEGIN;

ALTER TABLE financeiro_cobrancas ADD COLUMN credencial_id UUID;
CREATE INDEX ix_financeiro_cobrancas_credencial
    ON financeiro_cobrancas (credencial_id) WHERE credencial_id IS NOT NULL;

-- Backfill: só cobranças ainda em andamento (as terminais nunca mais precisam de
-- credenciais) cujo contexto + provedor tem exatamente UMA credencial. Havendo
-- mais de uma (ex.: ambientes test e prod) a ligação fica NULL e a resolução usa
-- o contexto + provedor + ambiente.
UPDATE financeiro_cobrancas c
SET credencial_id = x.id
FROM (
    SELECT contexto_tipo, COALESCE(codigo_academia, '') AS academia, provedor,
           (array_agg(id))[1] AS id, COUNT(*) AS n
    FROM financeiro_credenciais_provedor
    GROUP BY contexto_tipo, COALESCE(codigo_academia, ''), provedor
) x
WHERE x.n = 1
  AND c.credencial_id IS NULL
  AND c.provedor = x.provedor
  AND c.contexto_tipo = x.contexto_tipo
  AND COALESCE(c.codigo_academia, '') = x.academia
  AND lower(COALESCE(c.payload->>'status', '')) NOT IN ('success','cancelada','falhada','failed','cancelled','expired');

CREATE TABLE financeiro_credenciais_arquivadas (
    credential_id       UUID PRIMARY KEY,
    contexto_tipo       VARCHAR(16) NOT NULL CHECK (contexto_tipo IN ('spuri','academia')),
    codigo_academia     TEXT,
    ambiente            VARCHAR(8) NOT NULL CHECK (ambiente IN ('test','prod')),
    provedor            VARCHAR(32) NOT NULL,
    payload             JSONB NOT NULL DEFAULT '{}'::jsonb,
    cobrancas_pendentes INTEGER NOT NULL DEFAULT 0,
    arquivada_por       TEXT,
    arquivada_em        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ((contexto_tipo = 'spuri' AND codigo_academia IS NULL) OR
           (contexto_tipo = 'academia' AND codigo_academia IS NOT NULL))
);
CREATE INDEX ix_financeiro_credenciais_arquivadas_contexto
    ON financeiro_credenciais_arquivadas (contexto_tipo, COALESCE(codigo_academia, ''), provedor, ambiente);

COMMIT;

COMMENT ON TABLE financeiro_credenciais_arquivadas IS
 'Snapshot das credenciais de um provedor removidas enquanto havia cobranças pendentes. Usadas apenas para concluir essas cobranças (consulta, cancelamento, webhook); nunca para cobranças novas. Descartadas (com os segredos do cofre) quando a última pendente é resolvida.';
COMMENT ON COLUMN financeiro_cobrancas.credencial_id IS
 'Credencial (ativa ou arquivada) com a qual a cobrança foi emitida. NULL em cobranças legadas sem ligação única.';
