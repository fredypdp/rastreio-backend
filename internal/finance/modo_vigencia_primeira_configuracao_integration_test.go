package finance

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestIntegrationMensalidadePrimeiraConfiguracaoNaoExigeModoVigencia cobre o
// comportamento pedido: a primeira configuração de mensalidade de um escopo
// não deve exigir modo_vigencia (aplica-se implicitamente a "cobrancas_
// pendentes", o equivalente a "vale para todos"), mas a segunda chamada
// para o MESMO escopo (uma edição, já que a primeira ficou vigente) volta a
// exigir a escolha, exatamente como antes.
func TestIntegrationMensalidadePrimeiraConfiguracaoNaoExigeModoVigencia(t *testing.T) {
	client := integrationClient(t)
	service := NewService(client)
	academia := mensalidadeCodigo()
	seedMensalidadeAcademia(t, client, academia, "private", "fundamental", "2026_2027")
	configureIntegrationCredential(t, service, ContextoAcademia, academia)

	primeira, err := service.ConfigureMensalidade(context.Background(), MensalidadeConfiguracaoInput{
		CodigoAcademia:   academia,
		Nivel:            NivelFundamental,
		AnoAcademico:     "6_ano_fundamental",
		Valor:            100,
		MesFimCobranca:   7,
		MetodosPagamento: []string{"REF"},
	}, uuid.NewString(), "academia", "127.0.0.1")
	if err != nil {
		t.Fatalf("primeira configuração sem modo_vigencia foi rejeitada: %v", err)
	}
	if primeira.Valor != 100 {
		t.Fatalf("valor da primeira configuração = %v, queria 100", primeira.Valor)
	}

	var modoPersistido string
	if err := client.DB().QueryRowContext(context.Background(),
		`SELECT modo_vigencia FROM financeiro_mensalidade_configuracoes WHERE codigo_academia=$1 AND nivel=$2 AND ano_academico=$3 ORDER BY sequencia DESC LIMIT 1`,
		academia, NivelFundamental, "6_ano_fundamental").Scan(&modoPersistido); err != nil {
		t.Fatal(err)
	}
	if modoPersistido != ModoVigenciaCobrancasPendentes {
		t.Fatalf("modo_vigencia persistido = %q, queria %q (default da primeira configuração)", modoPersistido, ModoVigenciaCobrancasPendentes)
	}

	if _, err := service.ConfigureMensalidade(context.Background(), MensalidadeConfiguracaoInput{
		CodigoAcademia:   academia,
		Nivel:            NivelFundamental,
		AnoAcademico:     "6_ano_fundamental",
		Valor:            120,
		MesFimCobranca:   7,
		MetodosPagamento: []string{"REF"},
	}, uuid.NewString(), "academia", "127.0.0.1"); err == nil {
		t.Fatal("edição sem modo_vigencia foi aceita: deveria exigir a escolha, já existe configuração vigente")
	}

	if _, err := service.ConfigureMensalidade(context.Background(), MensalidadeConfiguracaoInput{
		CodigoAcademia:   academia,
		Nivel:            NivelFundamental,
		AnoAcademico:     "6_ano_fundamental",
		Valor:            120,
		MesFimCobranca:   7,
		MetodosPagamento: []string{"REF"},
		ModoVigencia:     ModoVigenciaAPartirDaAtualizacao,
	}, uuid.NewString(), "academia", "127.0.0.1"); err != nil {
		t.Fatalf("edição com modo_vigencia explícito foi rejeitada: %v", err)
	}
}

// TestIntegrationMatriculaPrimeiraConfiguracaoNaoExigeModoVigencia é o
// espelho do teste acima para a taxa de matrícula.
func TestIntegrationMatriculaPrimeiraConfiguracaoNaoExigeModoVigencia(t *testing.T) {
	client := integrationClient(t)
	service := NewService(client)
	academia := mensalidadeCodigo()
	seedMensalidadeAcademia(t, client, academia, "private", "fundamental", "2026_2027")
	configureIntegrationCredential(t, service, ContextoAcademia, academia)

	primeira, err := service.ConfigureMatricula(context.Background(), MatriculaConfiguracaoInput{
		CodigoAcademia:   academia,
		Nivel:            NivelFundamental,
		AnoAcademico:     "6_ano_fundamental",
		Valor:            50,
		MetodosPagamento: []string{"REF"},
	}, uuid.NewString(), "academia", "127.0.0.1")
	if err != nil {
		t.Fatalf("primeira configuração sem modo_vigencia foi rejeitada: %v", err)
	}
	if primeira.Valor != 50 {
		t.Fatalf("valor da primeira configuração = %v, queria 50", primeira.Valor)
	}

	if _, err := service.ConfigureMatricula(context.Background(), MatriculaConfiguracaoInput{
		CodigoAcademia:   academia,
		Nivel:            NivelFundamental,
		AnoAcademico:     "6_ano_fundamental",
		Valor:            80,
		MetodosPagamento: []string{"REF"},
	}, uuid.NewString(), "academia", "127.0.0.1"); err == nil {
		t.Fatal("edição sem modo_vigencia foi aceita: deveria exigir a escolha, já existe configuração vigente")
	}

	if _, err := service.ConfigureMatricula(context.Background(), MatriculaConfiguracaoInput{
		CodigoAcademia:   academia,
		Nivel:            NivelFundamental,
		AnoAcademico:     "6_ano_fundamental",
		Valor:            80,
		MetodosPagamento: []string{"REF"},
		ModoVigencia:     ModoVigenciaAPartirDaAtualizacao,
	}, uuid.NewString(), "academia", "127.0.0.1"); err != nil {
		t.Fatalf("edição com modo_vigencia explícito foi rejeitada: %v", err)
	}
}

// TestIntegrationMesInicioCobrancaRespeitaAnoLetivoSemConfiguracaoDeMensalidade
// prova o bug corrigido: antes, sem nenhuma configuração de mensalidade
// ainda cadastrada para a academia, validateMesInicioCobranca não aplicava
// NENHUM limite — qualquer mês (inclusive agosto, fora do ano letivo) era
// aceito. Agora o limite-padrão (mes_fim_cobranca=7) vale mesmo sem
// nenhuma configuração de preço existir.
func TestIntegrationMesInicioCobrancaRespeitaAnoLetivoSemConfiguracaoDeMensalidade(t *testing.T) {
	client := integrationClient(t)
	service := NewService(client)

	escola := mensalidadeCodigo()
	seedMensalidadeAcademia(t, client, escola, "private", "fundamental", "2026_2027")
	if err := service.validateMesInicioCobranca(context.Background(), &MesInicioCobrancaInput{CodigoAcademia: escola, AnoLetivo: "2026_2027", MesInicio: 8}); err == nil {
		t.Fatal("agosto (fora do ano letivo) foi aceito sem nenhuma configuração de mensalidade existir")
	}
	if err := service.validateMesInicioCobranca(context.Background(), &MesInicioCobrancaInput{CodigoAcademia: escola, AnoLetivo: "2026_2027", MesInicio: 9}); err != nil {
		t.Fatalf("setembro (início natural do ano letivo escolar) foi rejeitado: %v", err)
	}
	if err := service.validateMesInicioCobranca(context.Background(), &MesInicioCobrancaInput{CodigoAcademia: escola, AnoLetivo: "2026_2027", MesInicio: 7}); err != nil {
		t.Fatalf("julho (último mês possível, teto padrão) foi rejeitado: %v", err)
	}

	superior := mensalidadeCodigo()
	seedMensalidadeAcademia(t, client, superior, "private", "superior", "2026_2027")
	if err := service.validateMesInicioCobranca(context.Background(), &MesInicioCobrancaInput{CodigoAcademia: superior, AnoLetivo: "2026_2027", MesInicio: 9}); err == nil {
		t.Fatal("setembro (anterior ao início natural do ensino superior, outubro) foi aceito sem nenhuma configuração de mensalidade existir")
	}
	if err := service.validateMesInicioCobranca(context.Background(), &MesInicioCobrancaInput{CodigoAcademia: superior, AnoLetivo: "2026_2027", MesInicio: 10}); err != nil {
		t.Fatalf("outubro (início natural do ensino superior) foi rejeitado: %v", err)
	}
}
