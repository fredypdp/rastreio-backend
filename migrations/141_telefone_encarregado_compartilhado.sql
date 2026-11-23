-- 141_telefone_encarregado_compartilhado.sql
-- Plano de Atualizações, Parte 5 — o encarregado agora tem CONTA própria e pode
-- ter vários educandos (irmãos, inclusive de academias diferentes).
--
-- Até aqui o telefone do encarregado era um campo do estudante e uma restrição
-- única impedia dois estudantes de partilharem o mesmo telefone de encarregado.
-- Com a conta do encarregado (projection_contas_pessoa), o telefone passa a ser
-- identificador de UMA pessoa que responde por vários estudantes: a coluna do
-- estudante vira apenas um retrato do telefone no momento do cadastro (e segue
-- exigida por projection_estudantes_telefone_minimo_check). A fonte da verdade
-- do contato é a conta do encarregado (services.ResolverContatoDoEstudante).
--
-- Os índices únicos saem; ficam índices comuns para consulta.
DROP INDEX IF EXISTS idx_estudante_telefone_encarregado_unico;
DROP INDEX IF EXISTS idx_telefone_resp_verificado_estudante;

CREATE INDEX IF NOT EXISTS idx_estudante_telefone_encarregado
    ON projection_estudantes (telefone_encarregado)
    WHERE telefone_encarregado IS NOT NULL AND status::text <> 'deletado';

-- Um telefone identifica um encarregado (é por ele que a academia reconhece um
-- encarregado já existente no cadastro do estudante).
CREATE UNIQUE INDEX IF NOT EXISTS uq_contas_pessoa_encarregado_telefone
    ON projection_contas_pessoa (telefone)
    WHERE tipo = 'encarregado' AND status <> 'deletado';
