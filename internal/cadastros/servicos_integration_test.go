package cadastros

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"spuri/internal/db"
)

func transporteComRota(t *testing.T, client *db.Client, a academiaTeste) (*ServicosRepo, *Transporte, uuid.UUID, uuid.UUID) {
	t.Helper()
	repo := NovoServicosRepo(client)
	ctx := context.Background()
	rota, err := repo.CriarRota(ctx, a.Audit, a.Codigo, func(tr *Transporte) (*Rota, error) {
		return tr.NovaRota("Rota Norte", "06:30", ModoValor, 5000, []int{1, 2, 3})
	})
	if err != nil {
		t.Fatalf("criar rota: %v", err)
	}
	v, err := repo.CriarVeiculo(ctx, a.Audit, a.Codigo, rota.ID, "Autocarro 1")
	if err != nil {
		t.Fatalf("criar veículo: %v", err)
	}
	tr, persistido, err := repo.ObterTransporte(ctx, a.Codigo)
	if err != nil || !persistido {
		t.Fatalf("obter transporte: %v %v", persistido, err)
	}
	return repo, tr, rota.ID, v.ID
}

func solicitacaoNoBanco(t *testing.T, client *db.Client, a academiaTeste, servico uuid.UUID, rota *uuid.UUID, veiculos []uuid.UUID, turma *uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	vs := make([]string, len(veiculos))
	for i, v := range veiculos {
		vs[i] = v.String()
	}
	_, err := client.DB().Exec(`
		INSERT INTO projection_solicitacoes_servico_extra
		  (id, servico_extra_id, tipo_servico, rota_id, veiculo_ids, turma_id, codigo_academia, codigo_estudante, status, created_at, updated_at, version, last_event_id)
		VALUES ($1,$2,$3,$4,$5::uuid[],$6,$7,'EST1','vinculada',now(),now(),1,$8)`,
		id, servico, map[bool]string{true: "transporte", false: "atividade"}[rota != nil], rota, "{"+join(vs)+"}", turma, a.Codigo, uuid.New())
	if err != nil {
		t.Fatalf("inserir solicitação: %v", err)
	}
	return id
}

func join(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}

func TestServicosTransporteCriadoNaPrimeiraEscritaEAuditado(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	repo := NovoServicosRepo(client)
	ctx := context.Background()

	tr, persistido, err := repo.ObterTransporte(ctx, a.Codigo)
	if err != nil || persistido || len(tr.Rotas) != 0 {
		t.Fatalf("antes de configurar: persistido=%v err=%v", persistido, err)
	}
	salvo, err := repo.AtualizarTransporte(ctx, a.Audit, a.Codigo, "TransporteConfiguracaoAtualizada", func(t *Transporte) error {
		if err := t.DefinirPrecoGeral(ModoValor, 4500); err != nil {
			return err
		}
		return t.DefinirPrazoGeral(ModoMeses, []int{3, 1, 2})
	})
	if err != nil {
		t.Fatal(err)
	}
	if salvo.PrecoValor != 4500 || len(salvo.PrazoMeses) != 3 || salvo.PrazoMeses[0] != 1 || salvo.Version != 2 {
		t.Fatalf("salvo: %+v", salvo)
	}
	got := acoesDeAuditoria(t, client, EntidadeServicoTransporte, salvo.ID.String())
	if len(got) != 2 || got[0] != "TransporteCriado" || got[1] != "TransporteConfiguracaoAtualizada" {
		t.Fatalf("auditoria = %v", got)
	}
	// Academia que não existe não ganha transporte.
	if _, err := repo.AtualizarTransporte(ctx, a.Audit, "NAOEXISTE1", "x", func(*Transporte) error { return nil }); func() bool { _, ok := EhErroRegra(err); return !ok }() {
		t.Fatalf("academia inexistente deve ser recusada: %v", err)
	}
}

func TestServicosConflitoGeralComRotaNaoGravaNada(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	repo, tr, _, _ := transporteComRota(t, client, a)
	ctx := context.Background()
	versaoAntes := tr.Version

	_, err := repo.AtualizarTransporte(ctx, a.Audit, a.Codigo, "TransporteConfiguracaoAtualizada", func(t *Transporte) error {
		return t.DefinirPrecoGeral(ModoValor, 3000)
	})
	if _, ok := EhErroConflito(err); !ok {
		t.Fatalf("esperava conflito, veio %v", err)
	}
	depois, _, _ := repo.ObterTransporte(ctx, a.Codigo)
	if depois.PrecoModo != ModoNenhum || depois.Version != versaoAntes {
		t.Fatalf("o conflito não pode gravar: %+v", depois)
	}
	n := len(acoesDeAuditoria(t, client, EntidadeServicoTransporte, tr.ID.String()))
	if n != 1 { // só TransporteCriado
		t.Fatalf("o conflito não pode auditar: %d ações", n)
	}
}

func TestServicosRotaEVeiculoNomesRepetidosEExclusao(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	repo, tr, rotaID, veiculoID := transporteComRota(t, client, a)
	ctx := context.Background()

	if _, err := repo.CriarRota(ctx, a.Audit, a.Codigo, func(t *Transporte) (*Rota, error) {
		return t.NovaRota("rota norte", "07:00", ModoGratuito, 0, []int{1})
	}); func() bool { _, ok := EhErroConflito(err); return !ok }() {
		t.Fatalf("nome de rota repetido (sem diferenciar caixa) deve conflitar: %v", err)
	}
	if _, err := repo.CriarVeiculo(ctx, a.Audit, a.Codigo, rotaID, "AUTOCARRO 1"); func() bool { _, ok := EhErroConflito(err); return !ok }() {
		t.Fatalf("veículo repetido deve conflitar: %v", err)
	}

	// Sem inscrições, dá para apagar; com inscrição, só desativar.
	sol := solicitacaoNoBanco(t, client, a, tr.ID, &rotaID, []uuid.UUID{veiculoID}, nil)
	if err := repo.ExcluirVeiculo(ctx, a.Audit, a.Codigo, veiculoID); func() bool { _, ok := EhErroConflito(err); return !ok }() {
		t.Fatalf("veículo usado não pode ser apagado: %v", err)
	}
	if err := repo.ExcluirRota(ctx, a.Audit, a.Codigo, rotaID); func() bool { _, ok := EhErroConflito(err); return !ok }() {
		t.Fatalf("rota usada não pode ser apagada: %v", err)
	}
	off := false
	if _, err := repo.AtualizarVeiculo(ctx, a.Audit, a.Codigo, veiculoID, nil, &off); err != nil {
		t.Fatalf("desativar veículo: %v", err)
	}
	if _, _, _, err := repo.AtualizarRota(ctx, a.Audit, a.Codigo, rotaID, "TransporteRotaDesativada", func(_ *Transporte, r *Rota) error { r.Ativo = false; return nil }); err != nil {
		t.Fatalf("desativar rota: %v", err)
	}
	if _, err := client.DB().Exec(`DELETE FROM projection_solicitacoes_servico_extra WHERE id=$1`, sol); err != nil {
		t.Fatal(err)
	}
	if err := repo.ExcluirVeiculo(ctx, a.Audit, a.Codigo, veiculoID); err != nil {
		t.Fatalf("apagar veículo sem uso: %v", err)
	}
	if err := repo.ExcluirRota(ctx, a.Audit, a.Codigo, rotaID); err != nil {
		t.Fatalf("apagar rota sem uso: %v", err)
	}
	got := acoesDeAuditoria(t, client, EntidadeTransporteRota, rotaID.String())
	if got[len(got)-1] != "TransporteRotaApagada" {
		t.Fatalf("auditoria da rota = %v", got)
	}
}

func TestServicosRotaDeOutraAcademiaNaoEAcessivel(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	b := novaAcademia(t, client)
	repo, _, rotaID, veiculoID := transporteComRota(t, client, a)
	ctx := context.Background()
	if _, err := repo.CriarRota(ctx, b.Audit, b.Codigo, func(t *Transporte) (*Rota, error) { return t.NovaRota("X", "06:00", "", 0, nil) }); err != nil {
		t.Fatal(err) // b cria o próprio transporte
	}
	if err := repo.ExcluirRota(ctx, b.Audit, b.Codigo, rotaID); !errors.Is(err, ErrNaoEncontrado) {
		t.Fatalf("rota de outra academia: %v", err)
	}
	if _, err := repo.AtualizarVeiculo(ctx, b.Audit, b.Codigo, veiculoID, nil, new(bool)); !errors.Is(err, ErrNaoEncontrado) {
		t.Fatalf("veículo de outra academia: %v", err)
	}
	if _, err := repo.CriarVeiculo(ctx, b.Audit, b.Codigo, rotaID, "Intruso"); !errors.Is(err, ErrNaoEncontrado) {
		t.Fatalf("veículo em rota de outra academia: %v", err)
	}
}

func TestServicosAtividadeComHorariosETurmas(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	repo := NovoServicosRepo(client)
	ctx := context.Background()

	at, err := NovaAtividade(a.Codigo, "Coro", "Ana", true, false, 2000, []int{2, 3}, TaxaInscricao{}, []string{"REF"}, Disponibilidade{},
		[]Horario{{2, "14:00", "15:00"}, {4, "10:00", "11:30"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CriarAtividade(ctx, a.Audit, at); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ObterAtividade(ctx, at.ID)
	if err != nil || len(got.Horarios) != 2 || got.Horarios[0].DiaSemana != 2 || !got.Coletiva {
		t.Fatalf("obter: %+v %v", got, err)
	}
	if got.Resolver().Configurado() {
		t.Fatal("coletiva sem turma não pode estar configurada")
	}

	turma, err := repo.CriarTurma(ctx, a.Audit, a.Codigo, at.ID, "Turma A")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CriarTurma(ctx, a.Audit, a.Codigo, at.ID, "turma a"); func() bool { _, ok := EhErroConflito(err); return !ok }() {
		t.Fatalf("turma repetida deve conflitar: %v", err)
	}
	sol := uuid.New()
	if err := repo.RegistrarMembro(ctx, a.Audit, turma.ID, "EST1", sol); err != nil {
		t.Fatal(err)
	}
	if err := repo.RegistrarMembro(ctx, a.Audit, turma.ID, "EST1", sol); err != nil { // idempotente
		t.Fatal(err)
	}
	membros, _ := repo.ListarMembros(ctx, a.Codigo, turma.ID)
	if len(membros) != 1 || membros[0].CodigoEstudante != "EST1" {
		t.Fatalf("membros: %+v", membros)
	}
	if err := repo.ExcluirTurma(ctx, a.Audit, a.Codigo, turma.ID); func() bool { _, ok := EhErroConflito(err); return !ok }() {
		t.Fatalf("turma com membros não se apaga: %v", err)
	}
	if err := repo.RemoverMembrosDaSolicitacao(ctx, a.Audit, sol); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoverMembrosDaSolicitacao(ctx, a.Audit, sol); err != nil { // idempotente
		t.Fatal(err)
	}
	acoes := acoesDeAuditoria(t, client, EntidadeAtividadeTurma, turma.ID.String())
	if len(acoes) != 3 || acoes[1] != "AtividadeTurmaMembroAdicionado" || acoes[2] != "AtividadeTurmaMembroRemovido" {
		t.Fatalf("auditoria da turma = %v", acoes)
	}

	// Atualizar horários substitui a lista; atividade individual não tem turma.
	if _, _, _, err := repo.AtualizarAtividade(ctx, a.Audit, a.Codigo, at.ID, "AtividadeAtualizada", func(x *Atividade) error {
		return x.Atualizar(x.Nome, x.Instrutor, x.Coletiva, x.Gratuito, x.Preco, x.PrazoMeses, x.Taxa, x.Metodos, x.Disponibilidade, []Horario{{5, "09:00", "10:00"}})
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.ObterAtividade(ctx, at.ID)
	if len(got.Horarios) != 1 || got.Horarios[0].DiaSemana != 5 {
		t.Fatalf("horários após editar: %+v", got.Horarios)
	}
	indiv, _ := NovaAtividade(a.Codigo, "Xadrez", "Rui", false, true, 0, nil, TaxaInscricao{}, nil, Disponibilidade{}, nil)
	if err := repo.CriarAtividade(ctx, a.Audit, indiv); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CriarTurma(ctx, a.Audit, a.Codigo, indiv.ID, "T"); func() bool { _, ok := EhErroRegra(err); return !ok }() {
		t.Fatalf("atividade individual não tem turma: %v", err)
	}
}

func TestServicosAtividadeUsadaNaoApagaNemTrocaDeModalidade(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	repo := NovoServicosRepo(client)
	ctx := context.Background()
	at, _ := NovaAtividade(a.Codigo, "Dança", "Ana", false, true, 0, nil, TaxaInscricao{}, nil, Disponibilidade{}, nil)
	if err := repo.CriarAtividade(ctx, a.Audit, at); err != nil {
		t.Fatal(err)
	}
	solicitacaoNoBanco(t, client, a, at.ID, nil, nil, nil)
	if err := repo.ExcluirAtividade(ctx, a.Audit, a.Codigo, at.ID); func() bool { _, ok := EhErroConflito(err); return !ok }() {
		t.Fatalf("atividade com inscrições não se apaga: %v", err)
	}
	_, _, _, err := repo.AtualizarAtividade(ctx, a.Audit, a.Codigo, at.ID, "AtividadeAtualizada", func(x *Atividade) error {
		return x.Atualizar(x.Nome, x.Instrutor, true, x.Gratuito, x.Preco, x.PrazoMeses, x.Taxa, x.Metodos, x.Disponibilidade, x.Horarios)
	})
	if _, ok := EhErroConflito(err); !ok {
		t.Fatalf("trocar para coletiva com inscrições deve conflitar: %v", err)
	}
	// Outra academia não enxerga nem edita.
	b := novaAcademia(t, client)
	if err := repo.ExcluirAtividade(ctx, b.Audit, b.Codigo, at.ID); !errors.Is(err, ErrNaoEncontrado) {
		t.Fatalf("atividade de outra academia: %v", err)
	}
}

func TestServicosResolverServicoDaSolicitacao(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	repo, tr, rotaID, _ := transporteComRota(t, client, a)
	ctx := context.Background()
	if _, err := repo.AtualizarTransporte(ctx, a.Audit, a.Codigo, "TransporteConfiguracaoAtualizada", func(t *Transporte) error {
		return t.DefinirPagamento(TaxaInscricao{Valor: 800, Metodos: []string{"REF"}}, []string{"REF", "GPO"}, Disponibilidade{})
	}); err != nil {
		t.Fatal(err)
	}
	res, err := repo.ResolverServico(ctx, tr.ID, &rotaID)
	if err != nil || res.Tipo != TipoServicoTransporte || res.Preco != 5000 || !res.Configurado() || res.Taxa.Valor != 800 {
		t.Fatalf("resolver transporte: %+v %v", res, err)
	}
	if _, err := repo.ResolverServico(ctx, tr.ID, new(uuid.UUID)); !errors.Is(err, ErrNaoEncontrado) {
		t.Fatalf("rota inexistente: %v", err)
	}
	if _, err := repo.ResolverServico(ctx, uuid.New(), nil); !errors.Is(err, ErrNaoEncontrado) {
		t.Fatalf("serviço inexistente: %v", err)
	}
}
