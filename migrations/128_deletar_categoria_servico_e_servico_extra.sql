ALTER TABLE projection_categorias_servico
    ADD COLUMN deleted_at TIMESTAMPTZ NULL;

ALTER TABLE projection_servicos_extras
    ADD COLUMN deleted_at TIMESTAMPTZ NULL;
