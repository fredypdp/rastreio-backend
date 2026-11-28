package cadastros

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"spuri/internal/db"
)

// ServicosRepo grava o catálogo de serviços (Transporte e Atividade
// extracurricular) nas tabelas de registro, com auditoria na mesma transação.
type ServicosRepo struct{ client *db.Client }

func NovoServicosRepo(client *db.Client) *ServicosRepo { return &ServicosRepo{client: client} }

// ErrServicoEmUso indica que o registro já foi usado por uma solicitação de
// inscrição e por isso não pode ser apagado (só desativado).
var ErrServicoEmUso = errors.New("registro já usado por inscrições: desative-o em vez de apagar")

func intsDe(a []int64) []int {
	out := make([]int, len(a))
	for i, v := range a {
		out[i] = int(v)
	}
	return out
}

func int64sDe(a []int) []int64 {
	out := make([]int64, len(a))
	for i, v := range a {
		out[i] = int64(v)
	}
	return out
}

func vazio(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ============================================================================
// Leitura
// ============================================================================

const colunasTransporte = `id, codigo_academia, ativo, preco_modo, COALESCE(preco_valor,0)::float8, prazo_modo, prazo_meses,
	COALESCE(valor_taxa_inscricao,0)::float8, metodos_pagamento, metodos_pagamento_taxa_inscricao,
	anos_academicos_disponiveis, cursos_disponiveis, created_at, updated_at, version`

func scanTransporte(row interface{ Scan(...any) error }) (*Transporte, error) {
	var (
		t              Transporte
		prazo          []int64
		metodos, mTaxa []string
		anos, cursos   []string
		criado, atual  time.Time
	)
	err := row.Scan(&t.ID, &t.CodigoAcademia, &t.Ativo, &t.PrecoModo, &t.PrecoValor, &t.PrazoModo, pq.Array(&prazo),
		&t.Taxa.Valor, pq.Array(&metodos), pq.Array(&mTaxa), pq.Array(&anos), pq.Array(&cursos), &criado, &atual, &t.Version)
	if err != nil {
		return nil, err
	}
	t.PrazoMeses = intsDe(prazo)
	t.Metodos, t.Taxa.Metodos = vazio(metodos), vazio(mTaxa)
	t.AnosAcademicos, t.Cursos = vazio(anos), vazio(cursos)
	t.CreatedAt, t.UpdatedAt = criado.Format(time.RFC3339), atual.Format(time.RFC3339)
	t.Rotas = []Rota{}
	return &t, nil
}

func carregarRotas(ctx context.Context, q sqlx.QueryerContext, transporteID uuid.UUID) ([]Rota, error) {
	rows, err := q.QueryxContext(ctx, `
		SELECT id, transporte_id, nome, horario_embarque, preco_modo, COALESCE(preco_valor,0)::float8, prazo_meses, ativo, version
		FROM servico_transporte_rotas WHERE transporte_id = $1 ORDER BY lower(nome), id`, transporteID)
	if err != nil {
		return nil, fmt.Errorf("listar rotas: %w", err)
	}
	defer rows.Close()
	rotas := []Rota{}
	idx := map[uuid.UUID]int{}
	for rows.Next() {
		var r Rota
		var prazo []int64
		if err := rows.Scan(&r.ID, &r.TransporteID, &r.Nome, &r.HorarioEmbarque, &r.PrecoModo, &r.PrecoValor, pq.Array(&prazo), &r.Ativo, &r.Version); err != nil {
			return nil, err
		}
		r.PrazoMeses, r.Veiculos = intsDe(prazo), []Veiculo{}
		idx[r.ID] = len(rotas)
		rotas = append(rotas, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	vr, err := q.QueryxContext(ctx, `
		SELECT v.id, v.rota_id, v.nome, v.ativo, v.version FROM servico_transporte_veiculos v
		JOIN servico_transporte_rotas r ON r.id = v.rota_id WHERE r.transporte_id = $1 ORDER BY lower(v.nome), v.id`, transporteID)
	if err != nil {
		return nil, fmt.Errorf("listar veículos: %w", err)
	}
	defer vr.Close()
	for vr.Next() {
		var v Veiculo
		if err := vr.Scan(&v.ID, &v.RotaID, &v.Nome, &v.Ativo, &v.Version); err != nil {
			return nil, err
		}
		if i, ok := idx[v.RotaID]; ok {
			rotas[i].Veiculos = append(rotas[i].Veiculos, v)
		}
	}
	return rotas, vr.Err()
}

func carregarTransporte(ctx context.Context, q sqlx.QueryerContext, codigo string, bloquear bool) (*Transporte, error) {
	sufixo := ""
	if bloquear {
		sufixo = " FOR UPDATE"
	}
	t, err := scanTransporte(q.QueryRowxContext(ctx, `SELECT `+colunasTransporte+` FROM servico_transporte WHERE codigo_academia = $1`+sufixo, codigo))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNaoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("carregar transporte: %w", err)
	}
	if t.Rotas, err = carregarRotas(ctx, q, t.ID); err != nil {
		return nil, err
	}
	return t, nil
}

// ObterTransporte devolve o transporte da academia. Se a academia ainda não o
// configurou, devolve um transporte vazio (persistido = false).
func (r *ServicosRepo) ObterTransporte(ctx context.Context, codigo string) (*Transporte, bool, error) {
	t, err := carregarTransporte(ctx, r.client.DB(), codigo, false)
	if errors.Is(err, ErrNaoEncontrado) {
		return NovoTransporte(codigo), false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return t, true, nil
}

func carregarAtividade(ctx context.Context, q sqlx.QueryerContext, id uuid.UUID, bloquear bool) (*Atividade, error) {
	sufixo := ""
	if bloquear {
		sufixo = " FOR UPDATE"
	}
	a, err := scanAtividade(q.QueryRowxContext(ctx, `SELECT `+colunasAtividade+` FROM servico_atividades WHERE id = $1`+sufixo, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNaoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("carregar atividade: %w", err)
	}
	if err := completarAtividade(ctx, q, []*Atividade{a}); err != nil {
		return nil, err
	}
	return a, nil
}

const colunasAtividade = `id, codigo_academia, nome, instrutor, coletiva, gratuito, COALESCE(preco,0)::float8, prazo_meses,
	COALESCE(valor_taxa_inscricao,0)::float8, metodos_pagamento, metodos_pagamento_taxa_inscricao,
	anos_academicos_disponiveis, cursos_disponiveis, ativo, version`

func scanAtividade(row interface{ Scan(...any) error }) (*Atividade, error) {
	var (
		a              Atividade
		prazo          []int64
		metodos, mTaxa []string
		anos, cursos   []string
	)
	err := row.Scan(&a.ID, &a.CodigoAcademia, &a.Nome, &a.Instrutor, &a.Coletiva, &a.Gratuito, &a.Preco, pq.Array(&prazo),
		&a.Taxa.Valor, pq.Array(&metodos), pq.Array(&mTaxa), pq.Array(&anos), pq.Array(&cursos), &a.Ativo, &a.Version)
	if err != nil {
		return nil, err
	}
	a.PrazoMeses = intsDe(prazo)
	a.Metodos, a.Taxa.Metodos = vazio(metodos), vazio(mTaxa)
	a.AnosAcademicos, a.Cursos = vazio(anos), vazio(cursos)
	a.Horarios, a.Turmas = []Horario{}, []TurmaAtividade{}
	return &a, nil
}

// completarAtividade carrega horários e turmas (com contagem de membros).
func completarAtividade(ctx context.Context, q sqlx.QueryerContext, as []*Atividade) error {
	if len(as) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(as))
	idx := map[uuid.UUID]*Atividade{}
	for i, a := range as {
		ids[i] = a.ID
		idx[a.ID] = a
	}
	hr, err := q.QueryxContext(ctx, `
		SELECT atividade_id, dia_semana, hora_inicio, hora_fim FROM servico_atividade_horarios
		WHERE atividade_id = ANY($1) ORDER BY dia_semana, hora_inicio`, pq.Array(ids))
	if err != nil {
		return fmt.Errorf("listar horários: %w", err)
	}
	defer hr.Close()
	for hr.Next() {
		var id uuid.UUID
		var h Horario
		if err := hr.Scan(&id, &h.DiaSemana, &h.HoraInicio, &h.HoraFim); err != nil {
			return err
		}
		idx[id].Horarios = append(idx[id].Horarios, h)
	}
	if err := hr.Err(); err != nil {
		return err
	}
	tr, err := q.QueryxContext(ctx, `
		SELECT t.id, t.atividade_id, t.nome, t.ativo, t.version,
		       (SELECT count(*) FROM servico_atividade_turma_membros m WHERE m.turma_id = t.id)
		FROM servico_atividade_turmas t WHERE t.atividade_id = ANY($1) ORDER BY lower(t.nome), t.id`, pq.Array(ids))
	if err != nil {
		return fmt.Errorf("listar turmas da atividade: %w", err)
	}
	defer tr.Close()
	for tr.Next() {
		var t TurmaAtividade
		if err := tr.Scan(&t.ID, &t.AtividadeID, &t.Nome, &t.Ativo, &t.Version, &t.Membros); err != nil {
			return err
		}
		idx[t.AtividadeID].Turmas = append(idx[t.AtividadeID].Turmas, t)
	}
	return tr.Err()
}

// ObterAtividade devolve a atividade (com horários e turmas).
func (r *ServicosRepo) ObterAtividade(ctx context.Context, id uuid.UUID) (*Atividade, error) {
	return carregarAtividade(ctx, r.client.DB(), id, false)
}

// ListarAtividades devolve as atividades da academia.
func (r *ServicosRepo) ListarAtividades(ctx context.Context, codigo string, somenteAtivas bool) ([]*Atividade, error) {
	q := `SELECT ` + colunasAtividade + ` FROM servico_atividades WHERE codigo_academia = $1`
	if somenteAtivas {
		q += ` AND ativo`
	}
	rows, err := r.client.DB().QueryxContext(ctx, q+` ORDER BY lower(nome), id`, codigo)
	if err != nil {
		return nil, fmt.Errorf("listar atividades: %w", err)
	}
	defer rows.Close()
	out := []*Atividade{}
	for rows.Next() {
		a, err := scanAtividade(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, completarAtividade(ctx, r.client.DB(), out)
}

// ResolverServico devolve a visão de cobrança do serviço que a solicitação
// pede: a atividade com esse id, ou o transporte com esse id (e a rota).
func (r *ServicosRepo) ResolverServico(ctx context.Context, servicoID uuid.UUID, rotaID *uuid.UUID) (*ServicoResolvido, error) {
	return resolverServico(ctx, r.client.DB(), servicoID, rotaID)
}

func resolverServico(ctx context.Context, q sqlx.QueryerContext, servicoID uuid.UUID, rotaID *uuid.UUID) (*ServicoResolvido, error) {
	a, err := carregarAtividade(ctx, q, servicoID, false)
	if err == nil {
		v := a.Resolver()
		return &v, nil
	}
	if !errors.Is(err, ErrNaoEncontrado) {
		return nil, err
	}
	var codigo string
	if err := q.QueryRowxContext(ctx, `SELECT codigo_academia FROM servico_transporte WHERE id = $1`, servicoID).Scan(&codigo); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNaoEncontrado
		}
		return nil, err
	}
	t, err := carregarTransporte(ctx, q, codigo, false)
	if err != nil {
		return nil, err
	}
	var rota *Rota
	if rotaID != nil {
		for i := range t.Rotas {
			if t.Rotas[i].ID == *rotaID {
				rota = &t.Rotas[i]
			}
		}
		if rota == nil {
			return nil, ErrNaoEncontrado
		}
	}
	v := t.Resolver(rota)
	return &v, nil
}

// ============================================================================
// Auditoria: snapshots
// ============================================================================

func (t *Transporte) snapshot() map[string]any {
	return map[string]any{
		"codigo_academia": t.CodigoAcademia, "ativo": t.Ativo,
		"preco_modo": t.PrecoModo, "preco_valor": t.PrecoValor, "prazo_modo": t.PrazoModo, "prazo_meses": t.PrazoMeses,
		"valor_taxa_inscricao": t.Taxa.Valor, "metodos_pagamento": t.Metodos, "metodos_pagamento_taxa_inscricao": t.Taxa.Metodos,
		"anos_academicos_disponiveis": t.AnosAcademicos, "cursos_disponiveis": t.Cursos, "version": t.Version,
	}
}

func (r *Rota) snapshot() map[string]any {
	return map[string]any{
		"nome": r.Nome, "horario_embarque": r.HorarioEmbarque, "preco_modo": r.PrecoModo, "preco_valor": r.PrecoValor,
		"prazo_meses": r.PrazoMeses, "ativo": r.Ativo, "version": r.Version,
	}
}

func (a *Atividade) snapshot() map[string]any {
	return map[string]any{
		"codigo_academia": a.CodigoAcademia, "nome": a.Nome, "instrutor": a.Instrutor, "coletiva": a.Coletiva,
		"gratuito": a.Gratuito, "preco": a.Preco, "prazo_meses": a.PrazoMeses, "valor_taxa_inscricao": a.Taxa.Valor,
		"metodos_pagamento": a.Metodos, "metodos_pagamento_taxa_inscricao": a.Taxa.Metodos,
		"anos_academicos_disponiveis": a.AnosAcademicos, "cursos_disponiveis": a.Cursos,
		"ativo": a.Ativo, "horarios": a.Horarios, "version": a.Version,
	}
}

// ============================================================================
// Transporte (configuração geral)
// ============================================================================

func gravarTransporteTx(ctx context.Context, tx *sqlx.Tx, t *Transporte) error {
	var preco any
	if t.PrecoModo == ModoValor {
		preco = t.PrecoValor
	}
	var taxa any
	if t.Taxa.Valor > 0 {
		taxa = t.Taxa.Valor
	}
	err := tx.QueryRowxContext(ctx, `
		UPDATE servico_transporte SET ativo=$2, preco_modo=$3, preco_valor=$4, prazo_modo=$5, prazo_meses=$6,
		    valor_taxa_inscricao=$7, metodos_pagamento=$8, metodos_pagamento_taxa_inscricao=$9,
		    anos_academicos_disponiveis=$10, cursos_disponiveis=$11, version=version+1, updated_at=now()
		WHERE id=$1 RETURNING version`,
		t.ID, t.Ativo, t.PrecoModo, preco, t.PrazoModo, pq.Array(int64sDe(t.PrazoMeses)), taxa,
		pq.Array(vazio(t.Metodos)), pq.Array(vazio(t.Taxa.Metodos)), pq.Array(vazio(t.AnosAcademicos)), pq.Array(vazio(t.Cursos))).Scan(&t.Version)
	if err != nil {
		return fmt.Errorf("gravar transporte: %w", err)
	}
	return nil
}

// transporteParaEscrita devolve o transporte da academia travado para escrita,
// criando-o (vazio) quando é a primeira vez. A criação é auditada.
func transporteParaEscrita(ctx context.Context, tx *sqlx.Tx, aud *db.Auditor, audit db.AuditContext, codigo string) (*Transporte, error) {
	if err := exigirAcademia(ctx, tx, codigo); err != nil {
		return nil, err
	}
	novo := NovoTransporte(codigo)
	res, err := tx.ExecContext(ctx, `
		INSERT INTO servico_transporte (id, codigo_academia) VALUES ($1, $2) ON CONFLICT (codigo_academia) DO NOTHING`, novo.ID, codigo)
	if err != nil {
		return nil, fmt.Errorf("criar transporte: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		if err := aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeServicoTransporte, EntidadeID: novo.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "TransporteCriado", Depois: novo.snapshot(),
		}); err != nil {
			return nil, err
		}
	}
	return carregarTransporte(ctx, tx, codigo, true)
}

// AtualizarTransporte aplica fn à configuração geral do transporte (criando-a
// se preciso), grava e audita. fn devolve o erro de regra/conflito.
func (r *ServicosRepo) AtualizarTransporte(ctx context.Context, audit db.AuditContext, codigo, acao string, fn func(*Transporte) error) (*Transporte, error) {
	var out *Transporte
	err := r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		t, err := transporteParaEscrita(ctx, tx, aud, audit, codigo)
		if err != nil {
			return err
		}
		antes := t.snapshot()
		if err := fn(t); err != nil {
			return err
		}
		if err := gravarTransporteTx(ctx, tx, t); err != nil {
			return err
		}
		out = t
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeServicoTransporte, EntidadeID: t.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: acao, Antes: antes, Depois: t.snapshot(),
		})
	})
	return out, err
}

// ============================================================================
// Rotas e veículos
// ============================================================================

func encontrarRota(t *Transporte, id uuid.UUID) (*Rota, error) {
	for i := range t.Rotas {
		if t.Rotas[i].ID == id {
			return &t.Rotas[i], nil
		}
	}
	return nil, ErrNaoEncontrado
}

func gravarRotaTx(ctx context.Context, tx *sqlx.Tx, r *Rota, inserir bool) error {
	var preco any
	if r.PrecoModo == ModoValor {
		preco = r.PrecoValor
	}
	if inserir {
		return tx.QueryRowxContext(ctx, `
			INSERT INTO servico_transporte_rotas (id, transporte_id, nome, horario_embarque, preco_modo, preco_valor, prazo_meses, ativo)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING version`,
			r.ID, r.TransporteID, r.Nome, r.HorarioEmbarque, r.PrecoModo, preco, pq.Array(int64sDe(r.PrazoMeses)), r.Ativo).Scan(&r.Version)
	}
	return tx.QueryRowxContext(ctx, `
		UPDATE servico_transporte_rotas SET nome=$2, horario_embarque=$3, preco_modo=$4, preco_valor=$5, prazo_meses=$6, ativo=$7,
		    version=version+1, updated_at=now() WHERE id=$1 RETURNING version`,
		r.ID, r.Nome, r.HorarioEmbarque, r.PrecoModo, preco, pq.Array(int64sDe(r.PrazoMeses)), r.Ativo).Scan(&r.Version)
}

// CriarRota cria uma rota. fn monta a rota a partir do transporte (t.NovaRota).
func (r *ServicosRepo) CriarRota(ctx context.Context, audit db.AuditContext, codigo string, fn func(*Transporte) (*Rota, error)) (*Rota, error) {
	var out *Rota
	err := r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		t, err := transporteParaEscrita(ctx, tx, aud, audit, codigo)
		if err != nil {
			return err
		}
		rota, err := fn(t)
		if err != nil {
			return err
		}
		if err := gravarRotaTx(ctx, tx, rota, true); err != nil {
			return mapearUnico(err, "já existe uma rota ativa com esse nome")
		}
		out = rota
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeTransporteRota, EntidadeID: rota.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "TransporteRotaCriada", Depois: rota.snapshot(),
		})
	})
	return out, err
}

// AtualizarRota aplica fn à rota (dados, ativar/desativar), grava e audita.
// Devolve também os meses efetivos (resolvidos) antes e depois, para o handler
// cancelar as cobranças não pagas dos meses removidos.
func (r *ServicosRepo) AtualizarRota(ctx context.Context, audit db.AuditContext, codigo string, rotaID uuid.UUID, acao string,
	fn func(*Transporte, *Rota) error) (*Rota, []int, []int, error) {
	var (
		out           *Rota
		antes, depois []int
	)
	err := r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		t, err := carregarTransporte(ctx, tx, codigo, true)
		if err != nil {
			return err
		}
		rota, err := encontrarRota(t, rotaID)
		if err != nil {
			return err
		}
		snapAntes := rota.snapshot()
		antes = t.Resolver(rota).Meses
		if err := fn(t, rota); err != nil {
			return err
		}
		if err := gravarRotaTx(ctx, tx, rota, false); err != nil {
			return mapearUnico(err, "já existe uma rota ativa com esse nome")
		}
		depois = t.Resolver(rota).Meses
		out = rota
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeTransporteRota, EntidadeID: rota.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: acao, Antes: snapAntes, Depois: rota.snapshot(),
		})
	})
	return out, antes, depois, err
}

func existeSolicitacaoQue(ctx context.Context, q sqlx.QueryerContext, coluna string, id uuid.UUID) (bool, error) {
	var existe bool
	var consulta string
	switch coluna {
	case "servico":
		consulta = `SELECT EXISTS (SELECT 1 FROM projection_solicitacoes_servico_extra WHERE servico_extra_id = $1)`
	case "rota":
		consulta = `SELECT EXISTS (SELECT 1 FROM projection_solicitacoes_servico_extra WHERE rota_id = $1)`
	case "veiculo":
		consulta = `SELECT EXISTS (SELECT 1 FROM projection_solicitacoes_servico_extra WHERE $1 = ANY(veiculo_ids))`
	case "turma":
		consulta = `SELECT EXISTS (SELECT 1 FROM projection_solicitacoes_servico_extra WHERE turma_id = $1)`
	default:
		return false, fmt.Errorf("referência desconhecida: %s", coluna)
	}
	if err := q.QueryRowxContext(ctx, consulta, id).Scan(&existe); err != nil {
		return false, err
	}
	return existe, nil
}

// ExcluirRota apaga a rota (e os veículos dela) se nenhuma inscrição a usou.
func (r *ServicosRepo) ExcluirRota(ctx context.Context, audit db.AuditContext, codigo string, rotaID uuid.UUID) error {
	return r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		t, err := carregarTransporte(ctx, tx, codigo, true)
		if err != nil {
			return err
		}
		rota, err := encontrarRota(t, rotaID)
		if err != nil {
			return err
		}
		if usada, err := existeSolicitacaoQue(ctx, tx, "rota", rotaID); err != nil {
			return err
		} else if usada {
			return conflito("a rota %q já foi usada por inscrições: desative-a em vez de apagar", rota.Nome)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM servico_transporte_rotas WHERE id = $1`, rotaID); err != nil {
			return fmt.Errorf("apagar rota: %w", err)
		}
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeTransporteRota, EntidadeID: rotaID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "TransporteRotaApagada", Antes: rota.snapshot(),
		})
	})
}

func (r *ServicosRepo) CriarVeiculo(ctx context.Context, audit db.AuditContext, codigo string, rotaID uuid.UUID, nome string) (*Veiculo, error) {
	nome, err := ValidarNomeVeiculo(nome)
	if err != nil {
		return nil, err
	}
	v := &Veiculo{ID: uuid.New(), RotaID: rotaID, Nome: nome, Ativo: true, Version: 1}
	err = r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		t, err := carregarTransporte(ctx, tx, codigo, true)
		if err != nil {
			return err
		}
		if _, err := encontrarRota(t, rotaID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO servico_transporte_veiculos (id, rota_id, nome) VALUES ($1,$2,$3)`, v.ID, rotaID, nome); err != nil {
			return mapearUnico(err, "já existe um veículo ativo com esse nome nesta rota")
		}
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeTransporteVeiculo, EntidadeID: v.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "TransporteVeiculoCriado", Depois: map[string]any{"rota_id": rotaID, "nome": nome, "ativo": true},
		})
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}

func encontrarVeiculo(t *Transporte, id uuid.UUID) (*Rota, *Veiculo, error) {
	for i := range t.Rotas {
		for j := range t.Rotas[i].Veiculos {
			if t.Rotas[i].Veiculos[j].ID == id {
				return &t.Rotas[i], &t.Rotas[i].Veiculos[j], nil
			}
		}
	}
	return nil, nil, ErrNaoEncontrado
}

// AtualizarVeiculo renomeia, ativa ou desativa o veículo (nome nil = mantém).
func (r *ServicosRepo) AtualizarVeiculo(ctx context.Context, audit db.AuditContext, codigo string, veiculoID uuid.UUID, nome *string, ativo *bool) (*Veiculo, error) {
	var out *Veiculo
	err := r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		t, err := carregarTransporte(ctx, tx, codigo, true)
		if err != nil {
			return err
		}
		_, v, err := encontrarVeiculo(t, veiculoID)
		if err != nil {
			return err
		}
		antes := map[string]any{"nome": v.Nome, "ativo": v.Ativo}
		if nome != nil {
			n, err := ValidarNomeVeiculo(*nome)
			if err != nil {
				return err
			}
			v.Nome = n
		}
		if ativo != nil {
			v.Ativo = *ativo
		}
		if err := tx.QueryRowxContext(ctx, `UPDATE servico_transporte_veiculos SET nome=$2, ativo=$3, version=version+1, updated_at=now() WHERE id=$1 RETURNING version`,
			v.ID, v.Nome, v.Ativo).Scan(&v.Version); err != nil {
			return mapearUnico(err, "já existe um veículo ativo com esse nome nesta rota")
		}
		out = v
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeTransporteVeiculo, EntidadeID: v.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "TransporteVeiculoAtualizado", Antes: antes, Depois: map[string]any{"nome": v.Nome, "ativo": v.Ativo},
		})
	})
	return out, err
}

func (r *ServicosRepo) ExcluirVeiculo(ctx context.Context, audit db.AuditContext, codigo string, veiculoID uuid.UUID) error {
	return r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		t, err := carregarTransporte(ctx, tx, codigo, true)
		if err != nil {
			return err
		}
		_, v, err := encontrarVeiculo(t, veiculoID)
		if err != nil {
			return err
		}
		if usado, err := existeSolicitacaoQue(ctx, tx, "veiculo", veiculoID); err != nil {
			return err
		} else if usado {
			return conflito("o veículo %q já foi usado por inscrições: desative-o em vez de apagar", v.Nome)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM servico_transporte_veiculos WHERE id = $1`, veiculoID); err != nil {
			return fmt.Errorf("apagar veículo: %w", err)
		}
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeTransporteVeiculo, EntidadeID: veiculoID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "TransporteVeiculoApagado", Antes: map[string]any{"nome": v.Nome},
		})
	})
}

// ============================================================================
// Atividades
// ============================================================================

func gravarAtividadeTx(ctx context.Context, tx *sqlx.Tx, a *Atividade, inserir bool) error {
	var preco, taxa any
	if !a.Gratuito {
		preco = a.Preco
	}
	if a.Taxa.Valor > 0 {
		taxa = a.Taxa.Valor
	}
	args := []any{a.ID, a.CodigoAcademia, a.Nome, a.Instrutor, a.Coletiva, a.Gratuito, preco, pq.Array(int64sDe(a.PrazoMeses)),
		taxa, pq.Array(vazio(a.Metodos)), pq.Array(vazio(a.Taxa.Metodos)), pq.Array(vazio(a.AnosAcademicos)), pq.Array(vazio(a.Cursos)), a.Ativo}
	if inserir {
		if err := tx.QueryRowxContext(ctx, `
			INSERT INTO servico_atividades (id, codigo_academia, nome, instrutor, coletiva, gratuito, preco, prazo_meses,
			    valor_taxa_inscricao, metodos_pagamento, metodos_pagamento_taxa_inscricao, anos_academicos_disponiveis, cursos_disponiveis, ativo)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING version`, args...).Scan(&a.Version); err != nil {
			return mapearUnico(err, "já existe uma atividade ativa com esse nome")
		}
	} else {
		if err := tx.QueryRowxContext(ctx, `
			UPDATE servico_atividades SET nome=$3, instrutor=$4, coletiva=$5, gratuito=$6, preco=$7, prazo_meses=$8,
			    valor_taxa_inscricao=$9, metodos_pagamento=$10, metodos_pagamento_taxa_inscricao=$11,
			    anos_academicos_disponiveis=$12, cursos_disponiveis=$13, ativo=$14, version=version+1, updated_at=now()
			WHERE id=$1 AND codigo_academia=$2 RETURNING version`, args...).Scan(&a.Version); err != nil {
			return mapearUnico(err, "já existe uma atividade ativa com esse nome")
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM servico_atividade_horarios WHERE atividade_id = $1`, a.ID); err != nil {
		return fmt.Errorf("limpar horários: %w", err)
	}
	for _, h := range a.Horarios {
		if _, err := tx.ExecContext(ctx, `INSERT INTO servico_atividade_horarios (id, atividade_id, dia_semana, hora_inicio, hora_fim) VALUES ($1,$2,$3,$4,$5)`,
			uuid.New(), a.ID, h.DiaSemana, h.HoraInicio, h.HoraFim); err != nil {
			return fmt.Errorf("gravar horário: %w", err)
		}
	}
	return nil
}

func (r *ServicosRepo) CriarAtividade(ctx context.Context, audit db.AuditContext, a *Atividade) error {
	return r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		if err := exigirAcademia(ctx, tx, a.CodigoAcademia); err != nil {
			return err
		}
		if err := gravarAtividadeTx(ctx, tx, a, true); err != nil {
			return err
		}
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeServicoAtividade, EntidadeID: a.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "AtividadeCriada", Depois: a.snapshot(),
		})
	})
}

// AtualizarAtividade aplica fn à atividade, grava e audita. Devolve os meses
// efetivos antes e depois (para cancelar cobranças dos meses removidos).
// A atividade deixa de poder passar de coletiva para individual (ou o
// contrário) depois de ter turmas com membros.
func (r *ServicosRepo) AtualizarAtividade(ctx context.Context, audit db.AuditContext, codigo string, id uuid.UUID, acao string,
	fn func(*Atividade) error) (*Atividade, []int, []int, error) {
	var (
		out           *Atividade
		antes, depois []int
	)
	err := r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		a, err := carregarAtividade(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if a.CodigoAcademia != codigo {
			return ErrNaoEncontrado
		}
		snapAntes := a.snapshot()
		antes = a.Resolver().Meses
		coletivaAntes := a.Coletiva
		if err := fn(a); err != nil {
			return err
		}
		if coletivaAntes != a.Coletiva {
			if usada, err := existeSolicitacaoQue(ctx, tx, "servico", a.ID); err != nil {
				return err
			} else if usada {
				return conflito("a atividade já tem inscrições: não dá para trocar entre individual e coletiva")
			}
		}
		if err := gravarAtividadeTx(ctx, tx, a, false); err != nil {
			return err
		}
		depois = a.Resolver().Meses
		out = a
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeServicoAtividade, EntidadeID: a.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: acao, Antes: snapAntes, Depois: a.snapshot(),
		})
	})
	return out, antes, depois, err
}

func (r *ServicosRepo) ExcluirAtividade(ctx context.Context, audit db.AuditContext, codigo string, id uuid.UUID) error {
	return r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		a, err := carregarAtividade(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if a.CodigoAcademia != codigo {
			return ErrNaoEncontrado
		}
		if usada, err := existeSolicitacaoQue(ctx, tx, "servico", id); err != nil {
			return err
		} else if usada {
			return conflito("a atividade %q já teve inscrições: desative-a em vez de apagar", a.Nome)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM servico_atividades WHERE id = $1`, id); err != nil {
			return fmt.Errorf("apagar atividade: %w", err)
		}
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeServicoAtividade, EntidadeID: id.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "AtividadeApagada", Antes: a.snapshot(),
		})
	})
}

// ============================================================================
// Turmas da atividade (só atividade coletiva) e membros
// ============================================================================

func (r *ServicosRepo) CriarTurma(ctx context.Context, audit db.AuditContext, codigo string, atividadeID uuid.UUID, nome string) (*TurmaAtividade, error) {
	nome, err := ValidarNomeTurma(nome)
	if err != nil {
		return nil, err
	}
	t := &TurmaAtividade{ID: uuid.New(), AtividadeID: atividadeID, Nome: nome, Ativo: true, Version: 1}
	err = r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		a, err := carregarAtividade(ctx, tx, atividadeID, true)
		if err != nil {
			return err
		}
		if a.CodigoAcademia != codigo {
			return ErrNaoEncontrado
		}
		if !a.Coletiva {
			return regra("só atividade coletiva tem turma da atividade")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO servico_atividade_turmas (id, atividade_id, nome) VALUES ($1,$2,$3)`, t.ID, atividadeID, nome); err != nil {
			return mapearUnico(err, "já existe uma turma ativa com esse nome nesta atividade")
		}
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeAtividadeTurma, EntidadeID: t.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "AtividadeTurmaCriada", Depois: map[string]any{"atividade_id": atividadeID, "nome": nome},
		})
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

func turmaDaAcademiaTx(ctx context.Context, tx *sqlx.Tx, codigo string, turmaID uuid.UUID) (*TurmaAtividade, error) {
	var t TurmaAtividade
	err := tx.QueryRowxContext(ctx, `
		SELECT t.id, t.atividade_id, t.nome, t.ativo, t.version,
		       (SELECT count(*) FROM servico_atividade_turma_membros m WHERE m.turma_id = t.id)
		FROM servico_atividade_turmas t JOIN servico_atividades a ON a.id = t.atividade_id
		WHERE t.id = $1 AND a.codigo_academia = $2 FOR UPDATE OF t`, turmaID, codigo).
		Scan(&t.ID, &t.AtividadeID, &t.Nome, &t.Ativo, &t.Version, &t.Membros)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNaoEncontrado
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// AtualizarTurma renomeia, ativa ou desativa a turma (nil = mantém).
func (r *ServicosRepo) AtualizarTurma(ctx context.Context, audit db.AuditContext, codigo string, turmaID uuid.UUID, nome *string, ativo *bool) (*TurmaAtividade, error) {
	var out *TurmaAtividade
	err := r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		t, err := turmaDaAcademiaTx(ctx, tx, codigo, turmaID)
		if err != nil {
			return err
		}
		antes := map[string]any{"nome": t.Nome, "ativo": t.Ativo}
		if nome != nil {
			n, err := ValidarNomeTurma(*nome)
			if err != nil {
				return err
			}
			t.Nome = n
		}
		if ativo != nil {
			t.Ativo = *ativo
		}
		if err := tx.QueryRowxContext(ctx, `UPDATE servico_atividade_turmas SET nome=$2, ativo=$3, version=version+1, updated_at=now() WHERE id=$1 RETURNING version`,
			t.ID, t.Nome, t.Ativo).Scan(&t.Version); err != nil {
			return mapearUnico(err, "já existe uma turma ativa com esse nome nesta atividade")
		}
		out = t
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeAtividadeTurma, EntidadeID: t.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "AtividadeTurmaAtualizada", Antes: antes, Depois: map[string]any{"nome": t.Nome, "ativo": t.Ativo},
		})
	})
	return out, err
}

// ExcluirTurma apaga a turma se ela não tem membros e nenhuma inscrição a usou.
func (r *ServicosRepo) ExcluirTurma(ctx context.Context, audit db.AuditContext, codigo string, turmaID uuid.UUID) error {
	return r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		t, err := turmaDaAcademiaTx(ctx, tx, codigo, turmaID)
		if err != nil {
			return err
		}
		if t.Membros > 0 {
			return conflito("a turma %q tem membros: tire-os antes ou desative a turma", t.Nome)
		}
		if usada, err := existeSolicitacaoQue(ctx, tx, "turma", turmaID); err != nil {
			return err
		} else if usada {
			return conflito("a turma %q já foi usada por inscrições: desative-a em vez de apagar", t.Nome)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM servico_atividade_turmas WHERE id = $1`, turmaID); err != nil {
			return fmt.Errorf("apagar turma da atividade: %w", err)
		}
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeAtividadeTurma, EntidadeID: turmaID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "AtividadeTurmaApagada", Antes: map[string]any{"nome": t.Nome},
		})
	})
}

// MembroTurma é um estudante numa turma da atividade.
type MembroTurma struct {
	CodigoEstudante string
	SolicitacaoID   uuid.UUID
	Desde           time.Time
}

// ListarMembros devolve os membros da turma da atividade.
func (r *ServicosRepo) ListarMembros(ctx context.Context, codigo string, turmaID uuid.UUID) ([]MembroTurma, error) {
	rows, err := r.client.DB().QueryxContext(ctx, `
		SELECT m.codigo_estudante, m.solicitacao_id, m.desde
		FROM servico_atividade_turma_membros m
		JOIN servico_atividade_turmas t ON t.id = m.turma_id
		JOIN servico_atividades a ON a.id = t.atividade_id
		WHERE m.turma_id = $1 AND a.codigo_academia = $2 ORDER BY m.desde, m.codigo_estudante`, turmaID, codigo)
	if err != nil {
		return nil, fmt.Errorf("listar membros: %w", err)
	}
	defer rows.Close()
	out := []MembroTurma{}
	for rows.Next() {
		var m MembroTurma
		if err := rows.Scan(&m.CodigoEstudante, &m.SolicitacaoID, &m.Desde); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// TurmaDaAtividade confere que a turma existe, está ativa e pertence à
// atividade; devolve o nome.
func (r *ServicosRepo) TurmaDaAtividade(ctx context.Context, atividadeID, turmaID uuid.UUID) (*TurmaAtividade, error) {
	var t TurmaAtividade
	err := r.client.DB().QueryRowxContext(ctx, `
		SELECT id, atividade_id, nome, ativo, version, 0 FROM servico_atividade_turmas WHERE id = $1 AND atividade_id = $2`, turmaID, atividadeID).
		Scan(&t.ID, &t.AtividadeID, &t.Nome, &t.Ativo, &t.Version, &t.Membros)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNaoEncontrado
	}
	return &t, err
}

// RegistrarMembro coloca o estudante na turma da atividade quando o vínculo é
// efetivado. Repetir é inofensivo (idempotente). Auditado.
func (r *ServicosRepo) RegistrarMembro(ctx context.Context, audit db.AuditContext, turmaID uuid.UUID, codigoEstudante string, solicitacaoID uuid.UUID) error {
	return r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO servico_atividade_turma_membros (turma_id, codigo_estudante, solicitacao_id) VALUES ($1,$2,$3)
			ON CONFLICT (turma_id, codigo_estudante) DO NOTHING`, turmaID, codigoEstudante, solicitacaoID)
		if err != nil {
			return fmt.Errorf("registrar membro: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeAtividadeTurma, EntidadeID: turmaID.String(),
			Acao: "AtividadeTurmaMembroAdicionado", Depois: map[string]any{"codigo_estudante": codigoEstudante, "solicitacao_id": solicitacaoID},
		})
	})
}

// RemoverMembrosDaSolicitacao tira da(s) turma(s) o estudante da inscrição
// cancelada. Repetir é inofensivo. Auditado.
func (r *ServicosRepo) RemoverMembrosDaSolicitacao(ctx context.Context, audit db.AuditContext, solicitacaoID uuid.UUID) error {
	return r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		rows, err := tx.QueryxContext(ctx, `DELETE FROM servico_atividade_turma_membros WHERE solicitacao_id = $1 RETURNING turma_id, codigo_estudante`, solicitacaoID)
		if err != nil {
			return fmt.Errorf("remover membros: %w", err)
		}
		type par struct {
			turma  uuid.UUID
			codigo string
		}
		var removidos []par
		for rows.Next() {
			var p par
			if err := rows.Scan(&p.turma, &p.codigo); err != nil {
				rows.Close()
				return err
			}
			removidos = append(removidos, p)
		}
		rows.Close()
		for _, p := range removidos {
			if err := aud.Registrar(ctx, db.EntradaAuditoria{
				EntidadeTipo: EntidadeAtividadeTurma, EntidadeID: p.turma.String(),
				Acao: "AtividadeTurmaMembroRemovido", Antes: map[string]any{"codigo_estudante": p.codigo, "solicitacao_id": solicitacaoID},
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// mapearUnico traduz a violação de índice único (nome repetido) em conflito.
func mapearUnico(err error, msg string) error {
	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return conflito("%s", msg)
	}
	return err
}
