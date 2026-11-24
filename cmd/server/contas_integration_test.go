package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"spuri/internal/cadastros"
	"spuri/internal/db"
	"spuri/internal/middleware"
	"spuri/internal/projections"
)

// Plano de Atualizações, Parte 4 — secretário e professor, ponta a ponta:
// router real, PostgreSQL real, event sourcing real. Cada teste cria a sua
// academia (fixture), então não dependem uns dos outros.

func reconstruirContas(t *testing.T, fx *turmaVinculoFixture) {
	t.Helper()
	if err := projections.NewContasProjection(fx.client).Rebuild(); err != nil {
		t.Fatalf("rebuild contas: %v", err)
	}
}

func novoTelefone() string { return "9" + geraDigitosTurmaVinculo(8) }

// criarContaViaAPI cria secretário/professor pela academia e devolve id, código e senha temporária (do SMS).
func criarContaViaAPI(t *testing.T, fx *turmaVinculoFixture, sms *capturaSMS, caminho string, corpo map[string]any) (id, codigo, senha string) {
	t.Helper()
	antes := sms.total()
	rec, resp := chamarJSON(fx, http.MethodPost, caminho, fx.token, corpo)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %s: esperava 201, recebi %d: %s", caminho, rec.Code, rec.Body.String())
	}
	d := dataMap(resp)
	id, _ = d["id"].(string)
	codigo, _ = d["codigo"].(string)
	cred, _ := d["credenciais"].(map[string]any)
	if cred["enviada"] != true {
		t.Fatalf("as credenciais deveriam ter sido enviadas por SMS: %v", cred)
	}
	if sms.total() != antes+1 {
		t.Fatalf("esperava exatamente 1 SMS novo, houve %d", sms.total()-antes)
	}
	msg := sms.ultimo(t)
	if corpo["telefone"] != nil && msg.Destinatario != corpo["telefone"] {
		t.Fatalf("SMS foi para %q, esperava o telefone da conta %q", msg.Destinatario, corpo["telefone"])
	}
	m := reSenhaNoSMS.FindStringSubmatch(msg.Conteudo)
	if m == nil {
		t.Fatalf("SMS sem senha temporária: %q", msg.Conteudo)
	}
	if len(codigo) != 8 {
		t.Fatalf("código de login deveria ter 8 caracteres (2 letras + 6 dígitos): %q", codigo)
	}
	return id, codigo, m[1]
}

// entrarTrocandoSenha faz login com a senha temporária e troca-a; devolve o token da sessão definitiva.
func entrarTrocandoSenha(t *testing.T, fx *turmaVinculoFixture, codigo, senhaTemp string) string {
	t.Helper()
	reconstruirContas(t, fx)
	rec, login := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": codigo, "senha": senhaTemp})
	if rec.Code != http.StatusOK {
		t.Fatalf("login de %s: %d %s", codigo, rec.Code, rec.Body.String())
	}
	if login["deve_alterar_senha"] != true {
		t.Fatalf("a conta nova deveria exigir a troca da senha: %v", login)
	}
	token, _ := login["token"].(string)
	rec, trocada := chamarJSON(fx, http.MethodPut, "/alterar-senha", token, map[string]any{"senha_atual": senhaTemp, "nova_senha": "MinhaSenhaNova#9"})
	if rec.Code != http.StatusOK {
		t.Fatalf("troca da senha de %s: %d %s", codigo, rec.Code, rec.Body.String())
	}
	reconstruirContas(t, fx)
	token, _ = trocada["token"].(string)
	if token == "" {
		t.Fatalf("a troca de senha deveria devolver token novo: %v", trocada)
	}
	return token
}

func criarSecretario(t *testing.T, fx *turmaVinculoFixture, sms *capturaSMS, permissoes []string) (id, codigo, token string) {
	t.Helper()
	id, codigo, senha := criarContaViaAPI(t, fx, sms, "/academia/secretarios", map[string]any{
		"nome": "Secretario Teste", "genero": "feminino", "data_nascimento": "1990-05-10",
		"telefone": novoTelefone(), "permissoes": permissoes,
	})
	return id, codigo, entrarTrocandoSenha(t, fx, codigo, senha)
}

func statusDe(t *testing.T, fx *turmaVinculoFixture, metodo, caminho, token string, corpo map[string]any) (int, map[string]any) {
	t.Helper()
	rec, resp := chamarJSON(fx, metodo, caminho, token, corpo)
	return rec.Code, resp
}

func codigoDeErro(resp map[string]any) string {
	if d, ok := resp["data"].(map[string]any); ok {
		if c, ok := d["code"].(string); ok {
			return c
		}
	}
	c, _ := resp["code"].(string)
	return c
}

func TestContas01SecretarioTrabalhaEmNomeDaAcademiaSoComAsPermissoesConcedidas(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)
	_, _, tokenSec := criarSecretario(t, fx, sms, []string{"turmas.ver", "sumarios.gerir"})

	// Com turmas.ver vê as turmas DA ACADEMIA dele (identidade efetiva).
	if code, resp := statusDe(t, fx, http.MethodGet, "/academia/turmas", tokenSec, nil); code != http.StatusOK {
		t.Fatalf("secretário com turmas.ver deveria listar turmas: %d %v", code, resp)
	}
	// sumarios.gerir implica sumarios.ver (a permissão que faltava no plano).
	if code, resp := statusDe(t, fx, http.MethodGet, "/academia/sumarios", tokenSec, nil); code != http.StatusOK {
		t.Fatalf("sumarios.gerir deveria permitir consultar sumários: %d %v", code, resp)
	}
	// Sem turmas.gerir não cria turma.
	code, resp := statusDe(t, fx, http.MethodPost, "/academia/turma", tokenSec, map[string]any{})
	if code != http.StatusForbidden || codigoDeErro(resp) != "SECRETARIO_SEM_PERMISSAO" {
		t.Fatalf("criar turma sem permissão: esperava 403 SECRETARIO_SEM_PERMISSAO, recebi %d %v", code, resp)
	}
	// Nunca: gerir secretários, conta da academia, lançar notas/faltas.
	for _, tc := range []struct {
		metodo, caminho, quer string
	}{
		{http.MethodGet, "/academia/secretarios", "ACAO_RESERVADA_A_ACADEMIA"},
		{http.MethodPost, "/academia/secretarios", "ACAO_RESERVADA_A_ACADEMIA"},
		{http.MethodPut, "/academia/dados", "ACAO_RESERVADA_A_ACADEMIA"},
		{http.MethodPut, "/academia/nivel-escolar", "ACAO_RESERVADA_A_ACADEMIA"},
		{http.MethodGet, "/academia/permissoes", "ACAO_RESERVADA_A_ACADEMIA"},
		{http.MethodPost, "/academia/notas-aluno", "ACAO_NAO_PERMITIDA_AO_SECRETARIO"},
		{http.MethodPost, "/academia/faltas-aluno", "ACAO_NAO_PERMITIDA_AO_SECRETARIO"},
		{http.MethodGet, "/admin/sistema/configuracoes", "ROTA_FORA_DO_PERFIL"},
	} {
		code, resp := statusDe(t, fx, tc.metodo, tc.caminho, tokenSec, map[string]any{})
		if code != http.StatusForbidden || codigoDeErro(resp) != tc.quer {
			t.Errorf("%s %s como secretário: esperava 403 %s, recebi %d %v", tc.metodo, tc.caminho, tc.quer, code, resp)
		}
	}
	// A conta é a dele: /meu-perfil devolve o SECRETÁRIO (não a academia) e as permissões.
	rec, perfil := chamarJSON(fx, http.MethodGet, "/meu-perfil", tokenSec, nil)
	if rec.Code != http.StatusOK || perfil["tipo"] != "secretario" {
		t.Fatalf("/meu-perfil do secretário: %d %v", rec.Code, perfil)
	}
	// O perfil traz os dados da academia (a interface decide o que mostrar por nível).
	if ac, _ := perfil["academia"].(map[string]any); ac == nil || ac["codigo_academia"] != fx.academia.CodigoAcademia {
		t.Fatalf("o perfil do secretário deveria trazer a academia dele: %v", perfil["academia"])
	}
	// sumarios.gerir implica sumarios.ver: o conjunto gravado é o normalizado.
	if perms, _ := perfil["permissoes"].([]any); len(perms) != 3 {
		t.Fatalf("o perfil deveria trazer 3 permissões (sumarios.gerir, sumarios.ver, turmas.ver): %v", perfil["permissoes"])
	}
}

func TestContas02PermissoesDoSecretarioSaoValidadasPeloCatalogo(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)
	base := func(perms any) map[string]any {
		return map[string]any{"nome": "Secretario Catalogo", "genero": "masculino", "data_nascimento": "1988-01-02", "telefone": novoTelefone(), "permissoes": perms}
	}
	for nome, perms := range map[string]any{
		"lancar notas nunca é delegável": []string{"notas.lancar"},
		"gerir secretários é reservado":  []string{"secretarios.gerir"},
		"conjunto vazio":                 []string{},
	} {
		if code, resp := statusDe(t, fx, http.MethodPost, "/academia/secretarios", fx.token, base(perms)); code != http.StatusBadRequest && code != http.StatusUnprocessableEntity {
			t.Errorf("%s: esperava 4xx de validação, recebi %d %v", nome, code, resp)
		}
	}
	if sms.total() != 0 {
		t.Fatalf("pedido inválido não pode enviar SMS (houve %d)", sms.total())
	}
	// "todas_permissoes" concede o catálogo inteiro, incluindo os sumários.
	corpo := base(nil)
	delete(corpo, "permissoes")
	corpo["todas_permissoes"] = true
	_, resp := chamarJSON(fx, http.MethodPost, "/academia/secretarios", fx.token, corpo)
	perms, _ := dataMap(resp)["permissoes"].([]any)
	vistos := map[string]bool{}
	for _, p := range perms {
		vistos[p.(string)] = true
	}
	for _, p := range []string{"sumarios.ver", "sumarios.gerir", "professores.gerir", "estudantes.cadastrar", "financeiro.gerir"} {
		if !vistos[p] {
			t.Errorf("todas_permissoes deveria incluir %s: %v", p, perms)
		}
	}
	for _, proibida := range []string{"notas.lancar", "faltas.lancar", "secretarios.gerir"} {
		if vistos[proibida] {
			t.Errorf("o catálogo não pode conter %s", proibida)
		}
	}
	// Permissão implicada é acrescentada: turmas.gerir traz turmas.ver.
	corpo = base([]string{"turmas.gerir"})
	_, resp = chamarJSON(fx, http.MethodPost, "/academia/secretarios", fx.token, corpo)
	perms, _ = dataMap(resp)["permissoes"].([]any)
	if len(perms) != 2 {
		t.Fatalf("turmas.gerir deveria vir com turmas.ver: %v", perms)
	}
}

func TestContas03AuditoriaGravaOSecretarioComoAtorEAAcademiaComoDonaDaAcao(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)
	secID, _, tokenSec := criarSecretario(t, fx, sms, []string{"turmas.gerir"})

	code, resp := statusDe(t, fx, http.MethodPut, "/academia/turma/"+fx.turmaAtiva.CodigoTurma+"/desativar", tokenSec, nil)
	if code != http.StatusOK {
		t.Fatalf("secretário com turmas.gerir deveria desativar a turma: %d %v", code, resp)
	}
	var atorID, atorTipo, userID, userType string
	err := fx.client.DB().QueryRow(`
		SELECT COALESCE(metadata->>'ator_id',''), COALESCE(metadata->>'ator_tipo',''),
		       COALESCE(metadata->>'user_id',''), COALESCE(metadata->>'user_type','')
		  FROM spuri_ledger WHERE aggregate_id = $1 AND event_type = 'TurmaDesativada'
		 ORDER BY id DESC LIMIT 1`, fx.turmaAtiva.ID).Scan(&atorID, &atorTipo, &userID, &userType)
	if err != nil {
		t.Fatalf("evento TurmaDesativada não encontrado: %v", err)
	}
	if atorID != secID || atorTipo != "secretario" {
		t.Errorf("o ator deveria ser o secretário %s, está %q/%q", secID, atorID, atorTipo)
	}
	if userID != fx.academia.ID.String() || userType != "academia" {
		t.Errorf("a ação é em nome da academia %s, está %q/%q", fx.academia.ID, userID, userType)
	}
}

func TestContas04SecretarioDesativadoOuAcademiaInativaPerdeOAcessoNaHora(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)
	secID, _, tokenSec := criarSecretario(t, fx, sms, []string{"turmas.ver"})

	if code, _ := statusDe(t, fx, http.MethodGet, "/academia/turmas", tokenSec, nil); code != http.StatusOK {
		t.Fatalf("antes de desativar deveria acessar: %d", code)
	}
	if code, resp := statusDe(t, fx, http.MethodPut, "/academia/secretarios/"+secID+"/desativar", fx.token, nil); code != http.StatusOK {
		t.Fatalf("desativar: %d %v", code, resp)
	}
	reconstruirContas(t, fx)
	if code, _ := statusDe(t, fx, http.MethodGet, "/academia/turmas", tokenSec, nil); code != http.StatusForbidden && code != http.StatusUnauthorized {
		t.Fatalf("secretário desativado deveria ser barrado, recebi %d", code)
	}
	if code, resp := statusDe(t, fx, http.MethodPut, "/academia/secretarios/"+secID+"/ativar", fx.token, nil); code != http.StatusOK {
		t.Fatalf("ativar: %d %v", code, resp)
	}
	reconstruirContas(t, fx)
	if code, _ := statusDe(t, fx, http.MethodGet, "/academia/turmas", tokenSec, nil); code != http.StatusOK {
		t.Fatalf("depois de reativar deveria acessar: %d", code)
	}
	// Eliminado: nem consegue mais entrar.
	if code, resp := statusDe(t, fx, http.MethodDelete, "/academia/secretarios/"+secID, fx.token, nil); code != http.StatusOK {
		t.Fatalf("eliminar: %d %v", code, resp)
	}
	reconstruirContas(t, fx)
	if code, _ := statusDe(t, fx, http.MethodGet, "/academia/turmas", tokenSec, nil); code == http.StatusOK {
		t.Fatalf("secretário eliminado não pode mais acessar")
	}
}

func TestContas05SecretarioNaoAlcancaOutraAcademiaNemGereOutrosSecretarios(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)
	secID, _, _ := criarSecretario(t, fx, sms, []string{"turmas.ver"})

	// Uma turma de OUTRA academia não aparece para o secretário desta.
	_, _, tokenSec := func() (string, string, string) {
		id, cod, senha := criarContaViaAPI(t, fx, sms, "/academia/secretarios", map[string]any{
			"nome": "Secretario Dois", "genero": "masculino", "data_nascimento": "1991-03-04",
			"telefone": novoTelefone(), "permissoes": []string{"turmas.ver"},
		})
		return id, cod, entrarTrocandoSenha(t, fx, cod, senha)
	}()
	code, resp := statusDe(t, fx, http.MethodGet, "/academia/turma/"+fx.turmaOutra.CodigoTurma, tokenSec, nil)
	if code != http.StatusNotFound {
		t.Fatalf("turma de outra academia deveria dar 404 ao secretário, recebi %d %v", code, resp)
	}
	// A OUTRA academia não enxerga o secretário desta (404, não revela existência).
	outraToken := tokenDaAcademia(t, fx.outraAcademia.ID.String())
	if code, _ := statusDe(t, fx, http.MethodGet, "/academia/secretarios/"+secID, outraToken, nil); code != http.StatusNotFound {
		t.Fatalf("secretário de outra academia deveria dar 404, recebi %d", code)
	}
	if code, _ := statusDe(t, fx, http.MethodDelete, "/academia/secretarios/"+secID, outraToken, nil); code != http.StatusNotFound {
		t.Fatalf("eliminar secretário de outra academia deveria dar 404, recebi %d", code)
	}
}

func TestContas06ProfessorTemVinculosEVeSoAsSuasTurmas(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)
	materia := criarMateriaTeste(t, fx, "Matemática")

	profA, codA, senhaA := criarContaViaAPI(t, fx, sms, "/academia/professores", map[string]any{
		"nome": "Professor Alfa", "genero": "masculino", "data_nascimento": "1985-02-03", "telefone": novoTelefone(),
		"vinculos": []map[string]any{{"codigo_turma": fx.turmaAtiva.CodigoTurma, "materia_id": materia}},
	})
	reconstruirContas(t, fx)

	// O par (turma, matéria) só tem um professor: o segundo é recusado.
	code, resp := statusDe(t, fx, http.MethodPost, "/academia/professores", fx.token, map[string]any{
		"nome": "Professor Beta", "genero": "feminino", "data_nascimento": "1986-02-03", "telefone": novoTelefone(),
		"vinculos": []map[string]any{{"codigo_turma": fx.turmaAtiva.CodigoTurma, "materia_id": materia}},
	})
	if code != http.StatusConflict {
		t.Fatalf("par já atribuído deveria dar 409, recebi %d %v", code, resp)
	}
	// Turma inativa e turma de outra academia não aceitam vínculo.
	for _, turma := range []string{fx.turmaInativa.CodigoTurma, fx.turmaOutra.CodigoTurma} {
		code, _ := statusDe(t, fx, http.MethodPost, "/academia/professores", fx.token, map[string]any{
			"nome": "Professor Gama", "genero": "feminino", "data_nascimento": "1986-02-03", "telefone": novoTelefone(),
			"vinculos": []map[string]any{{"codigo_turma": turma, "materia_id": materia}},
		})
		if code != http.StatusBadRequest && code != http.StatusUnprocessableEntity {
			t.Errorf("turma %s não deveria aceitar vínculo: recebi %d", turma, code)
		}
	}

	tokenA := entrarTrocandoSenha(t, fx, codA, senhaA)
	rec, vincs := chamarJSON(fx, http.MethodGet, "/professor/vinculos", tokenA, nil)
	if rec.Code != http.StatusOK || vincs["total"] != float64(1) {
		t.Fatalf("o professor deveria ter 1 vínculo: %d %v", rec.Code, vincs)
	}
	if code, _ := statusDe(t, fx, http.MethodGet, "/professor/turmas/"+fx.turmaAtiva.CodigoTurma+"/estudantes", tokenA, nil); code != http.StatusOK {
		t.Fatalf("turma vinculada deveria abrir: %d", code)
	}
	// Turma da MESMA academia sem vínculo: 404 (não revela que existe).
	if code, _ := statusDe(t, fx, http.MethodGet, "/professor/turmas/"+fx.turmaAtiva2.CodigoTurma+"/notas", tokenA, nil); code != http.StatusNotFound {
		t.Fatalf("turma sem vínculo deveria dar 404, recebi %d", code)
	}
	// Fora da área do professor: nada das rotas da academia.
	for _, caminho := range []string{"/academia/turmas", "/estudantes", "/notas", "/financeiro/cobrancas"} {
		code, resp := statusDe(t, fx, http.MethodGet, caminho, tokenA, nil)
		if code != http.StatusForbidden || codigoDeErro(resp) != "ROTA_FORA_DO_PERFIL" {
			t.Errorf("GET %s como professor: esperava 403 ROTA_FORA_DO_PERFIL, recebi %d %v", caminho, code, resp)
		}
	}
	// Lançar nota/falta continua fora (Parte 11).
	if code, _ := statusDe(t, fx, http.MethodPost, "/academia/notas-aluno", tokenA, map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("professor não lança nota pela rota da academia: %d", code)
	}

	// Reatribuir: o par passa para o professor Beta e o Alfa perde o acesso à turma.
	profB, _, _ := criarContaViaAPI(t, fx, sms, "/academia/professores", map[string]any{
		"nome": "Professor Beta", "genero": "feminino", "data_nascimento": "1986-02-03", "telefone": novoTelefone(),
	})
	reconstruirContas(t, fx)
	vinculoID := primeiroVinculo(t, fx, profA)
	if code, resp := statusDe(t, fx, http.MethodPut, "/academia/vinculos-professor/"+vinculoID+"/reatribuir", fx.token, map[string]any{"professor_id": profB}); code != http.StatusOK {
		t.Fatalf("reatribuir: %d %v", code, resp)
	}
	reconstruirContas(t, fx)
	if code, _ := statusDe(t, fx, http.MethodGet, "/professor/turmas/"+fx.turmaAtiva.CodigoTurma+"/estudantes", tokenA, nil); code != http.StatusNotFound {
		t.Fatalf("depois de reatribuir o professor antigo perde o acesso: %d", code)
	}
}

func primeiroVinculo(t *testing.T, fx *turmaVinculoFixture, professorID string) string {
	t.Helper()
	var id string
	if err := fx.client.DB().QueryRow(`SELECT id::text FROM projection_professor_vinculos WHERE professor_id = $1 AND removido_em IS NULL`, professorID).Scan(&id); err != nil {
		t.Fatalf("vínculo do professor %s: %v", professorID, err)
	}
	return id
}

func criarMateriaTeste(t *testing.T, fx *turmaVinculoFixture, nome string) string {
	t.Helper()
	m, err := cadastros.NovaMateria(nome, "fundamental", []string{"1_ano_fundamental"}, fx.academia.CodigoAcademia, nil, nil, nil)
	if err != nil {
		t.Fatalf("criar matéria: %v", err)
	}
	audit := db.AuditContext{UserID: fx.academia.ID.String(), UserType: "academia", IP: "127.0.0.1"}
	if err := cadastros.NovoMateriasRepo(fx.client).Criar(context.Background(), audit, m); err != nil {
		t.Fatalf("salvar matéria: %v", err)
	}
	return m.ID.String()
}

func tokenDaAcademia(t *testing.T, id string) string {
	t.Helper()
	uid, err := uuid.Parse(id)
	if err != nil {
		t.Fatal(err)
	}
	token, err := middleware.GenerateToken(uid, "academia")
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	return token
}
