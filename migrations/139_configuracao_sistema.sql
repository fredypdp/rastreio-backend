-- 139: configurações do sistema editáveis pela tela de administração (Parte 3 do Plano de Atualizações).
--
-- projection_configuracao_sistema: uma linha por chave que já foi gravada alguma vez.
--   Remover uma configuração não apaga a linha: marca definida = FALSE (lápide). A projeção só
--   aplica um evento se a versão dele for maior que a da linha, então reaplicar eventos fora de
--   ordem (o servidor aplica na hora e o gerenciador de projeções reaplica depois) converge
--   sempre para o último estado e uma remoção nunca ressuscita um valor antigo.
--   Para segredo, valor fica NULL: o texto vive só no cofre abaixo.
-- configuracao_sistema_segredos: cofre dos segredos, cifrados com AES-256-GCM (chave derivada de
--   JWT_SECRET). NÃO é projeção: é escrito pelo comando e o ledger nunca recebe o valor, nem
--   cifrado; por isso uma reconstrução da projeção não o toca.

CREATE TABLE IF NOT EXISTS projection_configuracao_sistema (
    chave          VARCHAR(100) PRIMARY KEY,
    tipo           VARCHAR(20)  NOT NULL,
    segredo        BOOLEAN      NOT NULL DEFAULT FALSE,
    definida       BOOLEAN      NOT NULL DEFAULT FALSE,
    valor          TEXT,
    atualizado_por UUID,
    atualizado_em  TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    version        INTEGER      NOT NULL DEFAULT 0,
    last_event_id  UUID
);

CREATE TABLE IF NOT EXISTS configuracao_sistema_segredos (
    chave         VARCHAR(100) PRIMARY KEY,
    cifrado       TEXT         NOT NULL,
    atualizado_em TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);
