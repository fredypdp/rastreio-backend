package finance

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// TestIntegrationListCobrancasEstudanteIncluiMensalidadeEServicoExtra cobre o
// requisito de que um estudante deve conseguir consultar TODOS os pagamentos
// que já teve, em qualquer estado (não só "pago"), de mensalidade e de
// serviço extra, e que o tipo "matricula" deixou de existir (Plano, Parte 1).
func TestIntegrationListCobrancasEstudanteIncluiMensalidadeEServicoExtra(t *testing.T) {
	client := integrationClient(t)
	service := NewService(client)
	ctx := context.Background()

	academia := mensalidadeCodigo()
	codigoEstudante := "EST" + uuid.NewString()[:4]
	codigoInscricao := uuid.NewString()

	insert := func(status, estudante, inscricao string) {
		payload := map[string]any{"status": status, "amount": 500.0, "currency": "AOA", "description": "teste", "payment_method": "REF"}
		if estudante != "" {
			payload["codigo_estudante"] = estudante
		}
		if inscricao != "" {
			payload["codigo_inscricao_servico"] = inscricao
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.DB().Exec(`INSERT INTO financeiro_cobrancas (id,merchant_transaction_id,contexto_tipo,codigo_academia,payload) VALUES ($1,$2,'academia',$3,$4)`,
			uuid.New(), integrationMerchant("EST"), academia, raw); err != nil {
			t.Fatal(err)
		}
	}
	insert("Success", codigoEstudante, codigoInscricao) // taxa de serviço extra paga
	insert("Success", codigoEstudante, "")              // mensalidade paga
	insert("Failed", codigoEstudante, "")               // mensalidade falhada — deve aparecer também (todos os estados)
	insert("Success", "OUTRO12", "")                    // de outro estudante — não deve aparecer

	res, err := service.ListCobrancasEstudante(ctx, codigoEstudante, nil, nil, nil, nil, nil, "", "", nil, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 3 {
		t.Fatalf("esperava 3 cobranças do estudante (1 serviço extra + 2 mensalidade), obteve %d: %#v", res.Total, res.Cobrancas)
	}
	var temServicoExtra, temFalhada bool
	for _, cobranca := range res.Cobrancas {
		if cobranca.Origem == "servico_extra" && cobranca.CodigoInscricaoServico == codigoInscricao {
			temServicoExtra = true
		}
		if cobranca.Status == "Failed" {
			temFalhada = true
		}
	}
	if !temServicoExtra {
		t.Fatalf("cobrança de serviço extra não apareceu na listagem: %#v", res.Cobrancas)
	}
	if !temFalhada {
		t.Fatalf("cobrança falhada não apareceu (listagem deveria incluir todos os estados por padrão): %#v", res.Cobrancas)
	}

	pagas, err := service.ListCobrancasEstudante(ctx, codigoEstudante, nil, []string{"Success"}, nil, nil, nil, "", "", nil, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if pagas.Total != 2 {
		t.Fatalf("filtro por estado=Success deveria devolver 2 cobranças (serviço extra + mensalidade paga), obteve %d", pagas.Total)
	}

	// tarefa 49: o próprio estudante também precisa conseguir filtrar por
	// tipo de cobrança (mensalidade/serviço extra/avulsa), mesmo mecanismo que
	// ListCobrancas já oferece à academia/admin.
	somenteMensalidade, err := service.ListCobrancasEstudante(ctx, codigoEstudante, nil, nil, []string{"mensalidade"}, nil, nil, "", "", nil, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if somenteMensalidade.Total != 2 {
		t.Fatalf("filtro por tipo=mensalidade deveria devolver 2 cobranças, obteve %d: %#v", somenteMensalidade.Total, somenteMensalidade.Cobrancas)
	}
	for _, cobranca := range somenteMensalidade.Cobrancas {
		if cobranca.Origem != "mensalidade" {
			t.Fatalf("filtro por tipo=mensalidade devolveu cobrança de origem %q: %#v", cobranca.Origem, cobranca)
		}
	}

	somenteServicoExtra, err := service.ListCobrancasEstudante(ctx, codigoEstudante, nil, nil, []string{"servico_extra"}, nil, nil, "", "", nil, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if somenteServicoExtra.Total != 1 || somenteServicoExtra.Cobrancas[0].CodigoInscricaoServico != codigoInscricao {
		t.Fatalf("filtro por tipo=servico_extra deveria devolver só a cobrança do serviço extra, obteve %#v", somenteServicoExtra.Cobrancas)
	}

	for _, invalido := range []string{"invalido", "matricula"} {
		if _, err := service.ListCobrancasEstudante(ctx, codigoEstudante, nil, nil, []string{invalido}, nil, nil, "", "", nil, 50, 0); err == nil {
			t.Fatalf("esperava erro para tipo de cobrança inválido %q", invalido)
		}
	}
}

// TestIntegrationListCobrancasEstudanteSomenteAcademiaIsolaOutraAcademia
// cobre o isolamento por academia: quando somenteAcademia é passado (caso da
// academia autenticada, via ConsultarCobrancasEstudante), cobranças que o
// mesmo estudante fez a OUTRA academia não devem aparecer.
func TestIntegrationListCobrancasEstudanteSomenteAcademiaIsolaOutraAcademia(t *testing.T) {
	client := integrationClient(t)
	service := NewService(client)
	ctx := context.Background()

	academiaA := mensalidadeCodigo()
	academiaB := mensalidadeCodigo()
	estudante := "EST" + uuid.NewString()[:4]

	insert := func(academia string) {
		payload := map[string]any{"status": "Success", "amount": 500.0, "currency": "AOA", "description": "teste", "payment_method": "REF", "codigo_estudante": estudante}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.DB().Exec(`INSERT INTO financeiro_cobrancas (id,merchant_transaction_id,contexto_tipo,codigo_academia,payload) VALUES ($1,$2,'academia',$3,$4)`,
			uuid.New(), integrationMerchant("ISO"), academia, raw); err != nil {
			t.Fatal(err)
		}
	}
	insert(academiaA)
	insert(academiaB)

	semRestricao, err := service.ListCobrancasEstudante(ctx, estudante, nil, nil, nil, nil, nil, "", "", nil, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if semRestricao.Total != 2 {
		t.Fatalf("sem somenteAcademia deveria ver as 2 cobranças (histórico completo), obteve %d", semRestricao.Total)
	}

	comRestricao, err := service.ListCobrancasEstudante(ctx, estudante, &academiaA, nil, nil, nil, nil, "", "", nil, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if comRestricao.Total != 1 || comRestricao.Cobrancas[0].CodigoAcademia != academiaA {
		t.Fatalf("com somenteAcademia=%s deveria ver só a cobrança dessa academia, obteve %#v", academiaA, comRestricao.Cobrancas)
	}
}
