package finance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// --- regra pura (sem banco) -------------------------------------------------

func TestPagamentoDisponivelSemProvedorNaoTemMetodos(t *testing.T) {
	p := PagamentoDisponivel{}
	if p.Configurado() {
		t.Fatal("sem provedor não está configurado")
	}
	got := p.Filtrar([]string{"REF", "GPO"})
	if got == nil || len(got) != 0 {
		t.Fatalf("sem provedor a lista é vazia e nunca nil (JSON []): %#v", got)
	}
	a := p.Aviso([]string{"REF"}, got)
	if a == nil || a.Codigo != AvisoProvedorNaoConfigurado || a.Mensagem == "" {
		t.Fatalf("aviso = %+v", a)
	}
	// Mesmo sem métodos habilitados o motivo mais útil continua sendo o provedor.
	if a := p.Aviso(nil, nil); a == nil || a.Codigo != AvisoProvedorNaoConfigurado {
		t.Fatalf("aviso sem métodos habilitados = %+v", a)
	}
}

func TestPagamentoDisponivelComProvedorFiltraNaOrdemRecebida(t *testing.T) {
	p := PagamentoDisponivel{provedor: &appyPayProvider{}}
	if !p.Configurado() {
		t.Fatal("deveria estar configurado")
	}
	got := p.Filtrar([]string{"gpo", "XYZ", "REF", "GPO_QR"})
	if strings.Join(got, ",") != "gpo,REF,GPO_QR" {
		t.Fatalf("filtrar = %v", got)
	}
	if a := p.Aviso([]string{"gpo", "XYZ"}, got); a != nil {
		t.Fatalf("com métodos disponíveis não há o que explicar: %+v", a)
	}
}

func TestPagamentoDisponivelAvisoPorMotivo(t *testing.T) {
	p := PagamentoDisponivel{provedor: &appyPayProvider{}}
	casos := []struct {
		nome        string
		habilitados []string
		codigo      string
	}{
		{"só métodos não suportados", []string{"XYZ", "ABC"}, AvisoMetodosNaoSuportados},
		{"nenhum habilitado (nil)", nil, AvisoNenhumMetodoHabilitado},
		{"nenhum habilitado (vazio)", []string{}, AvisoNenhumMetodoHabilitado},
	}
	for _, c := range casos {
		a := p.Aviso(c.habilitados, p.Filtrar(c.habilitados))
		if a == nil || a.Codigo != c.codigo || a.Mensagem == "" {
			t.Errorf("%s: aviso = %+v, queria %s", c.nome, a, c.codigo)
		}
	}
}

func TestPagamentoDisponivelIndeterminadoNaoEscondeNemExplica(t *testing.T) {
	// Falha de infraestrutura: nada some e nenhum aviso falso é dado; a
	// geração da cobrança continua validando o método.
	p := PagamentoDisponivel{indeterminado: true}
	in := []string{"REF", "XYZ"}
	if got := p.Filtrar(in); strings.Join(got, ",") != "REF,XYZ" {
		t.Fatalf("filtrar indeterminado = %v", got)
	}
	if a := p.Aviso(in, nil); a != nil {
		t.Fatalf("indeterminado não gera aviso: %+v", a)
	}
}

// --- com banco --------------------------------------------------------------

func TestIntegrationPagamentoDisponivelExplicaEValidaAoSalvar(t *testing.T) {
	provedorEnv(t)
	client := integrationClient(t)
	registrarProvedorFalso(t)
	service := NewService(client)
	ctx := context.Background()
	academia := "DSP" + uuid.NewString()[:8]

	// Sem provedor: não há como pagar, e salvar não pode ser validado.
	disp := service.PagamentoDisponivel(ctx, academia)
	if disp.Configurado() {
		t.Fatal("academia nova não tem provedor")
	}
	if a := disp.Aviso([]string{"REF"}, disp.Filtrar([]string{"REF"})); a == nil || a.Codigo != AvisoProvedorNaoConfigurado {
		t.Fatalf("aviso sem provedor: %+v", a)
	}
	if err := service.ValidarMetodosSeHouverProvedor(ctx, academia, []string{"QUALQUER"}); err != nil {
		t.Fatalf("sem provedor não há contra o que validar: %v", err)
	}

	configurarAppyPay(t, service, academia)
	if err := service.ValidarMetodosSeHouverProvedor(ctx, academia, []string{"REF", "GPO", "GPO_QR"}); err != nil {
		t.Fatalf("métodos suportados: %v", err)
	}
	err := service.ValidarMetodosSeHouverProvedor(ctx, academia, []string{"REF", "XYZ"})
	if !errors.Is(err, ErrMetodoIndisponivel) || !strings.Contains(err.Error(), "XYZ") {
		t.Fatalf("método não suportado deveria ser recusado citando-o: %v", err)
	}

	// Um provedor que só suporta REF: GPO deixa de ser oferecido e de ser aceito ao salvar.
	configurarFalso(t, service, academia, "tok")
	if _, err := service.DefinirProvedor(ctx, ContextoAcademia, academia, provedorFalsoID, "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	disp = service.PagamentoDisponivel(ctx, academia)
	if got := disp.Filtrar([]string{"GPO", "REF"}); strings.Join(got, ",") != "REF" {
		t.Fatalf("provedor só-REF: %v", got)
	}
	if err := service.ValidarMetodosSeHouverProvedor(ctx, academia, []string{"GPO"}); !errors.Is(err, ErrMetodoIndisponivel) {
		t.Fatalf("GPO não é suportado pelo provedor só-REF: %v", err)
	}
}

func TestIntegrationMetodosDaPropinaExplicamListaVazia(t *testing.T) {
	provedorEnv(t)
	client := integrationClient(t)
	registrarProvedorFalso(t)
	service := NewService(client)
	ctx := context.Background()
	academia := "PRP" + uuid.NewString()[:8]
	seedMensalidadeAcademia(t, client, academia, "private", "fundamental", "2025_2026")
	seedMensalidadeConfiguracao(t, client, academia, NivelFundamental, "6_ano_fundamental", nil, 1000, 7, time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC))
	definir := func(metodos string) {
		if _, err := client.DB().Exec(`UPDATE financeiro_mensalidade_configuracoes SET metodos_pagamento=`+metodos+` WHERE codigo_academia=$1`, academia); err != nil {
			t.Fatal(err)
		}
	}
	definir(`ARRAY['GPO','REF']`)

	// 1. Sem provedor.
	metodos, aviso, err := service.MetodosPagamentoMensalidadeComAviso(ctx, academia)
	if err != nil || len(metodos) != 0 || aviso == nil || aviso.Codigo != AvisoProvedorNaoConfigurado {
		t.Fatalf("sem provedor: %v %+v %v", metodos, aviso, err)
	}
	// 2. Provedor só-REF e propina só com GPO: há provedor, mas nada em comum.
	configurarFalso(t, service, academia, "tok")
	if _, err = service.DefinirProvedor(ctx, ContextoAcademia, academia, provedorFalsoID, "u", "academia", "ip"); err != nil {
		t.Fatal(err)
	}
	definir(`ARRAY['GPO']`)
	if metodos, aviso, err = service.MetodosPagamentoMensalidadeComAviso(ctx, academia); err != nil || len(metodos) != 0 || aviso == nil || aviso.Codigo != AvisoMetodosNaoSuportados {
		t.Fatalf("métodos não suportados: %v %+v %v", metodos, aviso, err)
	}
	// 3. Há método utilizável: sem aviso.
	definir(`ARRAY['GPO','REF']`)
	if metodos, aviso, err = service.MetodosPagamentoMensalidadeComAviso(ctx, academia); err != nil || strings.Join(metodos, ",") != "REF" || aviso != nil {
		t.Fatalf("com método utilizável: %v %+v %v", metodos, aviso, err)
	}
}

// Um Service sem banco (ou nulo) não pode derrubar a leitura pública de
// serviços/estudante: não há como saber o provedor, então nada é escondido nem
// explicado, e salvar não é validado.
func TestPagamentoDisponivelToleraServicoOuBancoNulos(t *testing.T) {
	ctx := context.Background()
	for nome, s := range map[string]*Service{"serviço nulo": nil, "serviço sem banco": NewService(nil)} {
		disp := s.PagamentoDisponivel(ctx, "ACA")
		in := []string{"REF", "XYZ"}
		if got := disp.Filtrar(in); strings.Join(got, ",") != "REF,XYZ" {
			t.Errorf("%s: não deveria esconder nada: %v", nome, got)
		}
		if a := disp.Aviso(in, nil); a != nil {
			t.Errorf("%s: não deveria dar aviso: %+v", nome, a)
		}
		if err := s.ValidarMetodosSeHouverProvedor(ctx, "ACA", in); err != nil {
			t.Errorf("%s: sem provedor conhecido não há o que validar: %v", nome, err)
		}
		if _, _, err := s.MetodosPagamentoMensalidadeComAviso(ctx, "ACA"); err == nil {
			t.Errorf("%s: a propina precisa do banco e deve devolver erro, não pânico", nome)
		}
	}
}
