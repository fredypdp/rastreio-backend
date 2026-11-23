package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"spuri/internal/services"
)

// Plano de Atualizações, Partes 4 e 5 — encarregado e credenciais, ponta a
// ponta (router real, PostgreSQL real, event sourcing real).

func chamarComCabecalho(fx *turmaVinculoFixture, metodo, caminho, token string, cabecalhos map[string]string, corpo map[string]any) (*httptest.ResponseRecorder, map[string]any) {
	var buf bytes.Buffer
	if corpo != nil {
		raw, _ := json.Marshal(corpo)
		buf.Write(raw)
	}
	req := httptest.NewRequest(metodo, caminho, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range cabecalhos {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

var reSenhaGenerica = regexp.MustCompile(`Senha temporaria: (\S+) Altere`)

// senhaDoSMSDe devolve a senha temporária do SMS que traz "Usuario: <codigo>".
func senhaDoSMSDe(t *testing.T, sms *capturaSMS, codigo string) string {
	t.Helper()
	sms.mu.Lock()
	defer sms.mu.Unlock()
	for i := len(sms.itens) - 1; i >= 0; i-- {
		if strings.Contains(sms.itens[i].Conteudo, "Usuario: "+codigo+" ") {
			if m := reSenhaGenerica.FindStringSubmatch(sms.itens[i].Conteudo); m != nil {
				return m[1]
			}
		}
	}
	t.Fatalf("nenhum SMS com as credenciais de %s", codigo)
	return ""
}

// cadastrarEscolar cadastra um estudante escolar com um encarregado (novo ou
// reutilizado pelo telefone) e devolve a resposta.
func cadastrarEscolar(t *testing.T, fx *turmaVinculoFixture, nome, telEnc string) map[string]any {
	t.Helper()
	campos := camposCadastro(nome)
	campos["telefone_encarregado"] = telEnc
	campos["encarregado_telefone"] = telEnc
	campos["bilhete_identidade_encarregado"] = "BI" + geraDigitosTurmaVinculo(9) + "LA042"
	rec := postCadastro(t, fx, campos)
	if rec.Code != http.StatusCreated {
		t.Fatalf("cadastro de %s: %d %s", nome, rec.Code, rec.Body.String())
	}
	return decodeMap(t, rec.Body.Bytes())
}

func totalAgregados(t *testing.T, fx *turmaVinculoFixture, tipo string) int {
	t.Helper()
	var n int
	if err := fx.client.DB().QueryRow(`SELECT count(DISTINCT aggregate_id) FROM spuri_ledger WHERE aggregate_type = $1`, tipo).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEncarregado01CadastroEscolarCriaContaEEnviaCredenciaisSoAoTelefoneDele(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)
	telEnc := novoTelefone()
	telEstudante := novoTelefone()

	campos := camposCadastro("Aluno Escolar Um")
	campos["telefone"] = telEstudante // o do estudante existe, mas NUNCA recebe as credenciais
	campos["telefone_encarregado"], campos["encarregado_telefone"] = telEnc, telEnc
	rec := postCadastro(t, fx, campos)
	if rec.Code != http.StatusCreated {
		t.Fatalf("cadastro: %d %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec.Body.Bytes())
	enc, _ := dataMap(resp)["encarregado"].(map[string]any)
	if enc["conta_nova"] != true || enc["codigo"] == nil {
		t.Fatalf("o encarregado deveria ter conta nova: %v", enc)
	}
	// Duas contas nasceram (estudante + encarregado): duas mensagens, as duas para o encarregado.
	if sms.total() != 2 {
		t.Fatalf("esperava 2 SMS (estudante e encarregado novo), houve %d", sms.total())
	}
	for _, m := range sms.itens {
		if m.Destinatario != telEnc {
			t.Errorf("credenciais enviadas para %q; só podem ir para o telefone do encarregado %q", m.Destinatario, telEnc)
		}
		if m.Destinatario == telEstudante {
			t.Errorf("o telefone do estudante recebeu credenciais")
		}
	}
	if totalAgregados(t, fx, "Encarregado") < 1 {
		t.Fatal("a conta do encarregado deveria estar no ledger")
	}

	// Irmão: mesmo telefone → o encarregado é REUTILIZADO, sem novas credenciais.
	reconstruirContas(t, fx)
	antesAgregados := totalAgregados(t, fx, "Encarregado")
	antesSMS := sms.total()
	resp2 := cadastrarEscolar(t, fx, "Aluno Escolar Dois", telEnc)
	enc2, _ := dataMap(resp2)["encarregado"].(map[string]any)
	if enc2["conta_nova"] != false || enc2["id"] != enc["id"] {
		t.Fatalf("o irmão deveria vincular o MESMO encarregado: %v vs %v", enc2, enc)
	}
	if enc2["credenciais"] != nil {
		t.Errorf("encarregado já existente não recebe novas credenciais: %v", enc2)
	}
	if sms.total() != antesSMS+1 {
		t.Errorf("só as credenciais do estudante novo deveriam ser enviadas (+1), houve +%d", sms.total()-antesSMS)
	}
	if totalAgregados(t, fx, "Encarregado") != antesAgregados {
		t.Errorf("não podia nascer um segundo encarregado para o mesmo telefone")
	}
	reconstruirContas(t, fx)
	var educandos int
	if err := fx.client.DB().QueryRow(`SELECT count(*) FROM projection_encarregado_educandos WHERE encarregado_id = $1::uuid AND desvinculado_em IS NULL`, enc["id"]).Scan(&educandos); err != nil || educandos != 2 {
		t.Fatalf("o encarregado deveria ter 2 educandos: %d (%v)", educandos, err)
	}
}

func TestEncarregado02CadastroEscolarSemDadosDoEncarregadoNaoCriaNada(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)
	antesEst, antesEnc := totalAgregados(t, fx, "Estudante"), totalAgregados(t, fx, "Encarregado")

	for nome, ajuste := range map[string]func(map[string]string){
		"sem nome do encarregado":       func(c map[string]string) { delete(c, "encarregado_nome") },
		"sem data de nascimento":        func(c map[string]string) { delete(c, "encarregado_data_nascimento") },
		"relação 'outro' sem descrição": func(c map[string]string) { c["encarregado_relacao"] = "outro" },
		"relação inventada":             func(c map[string]string) { c["encarregado_relacao"] = "vizinho" },
		"encarregado menor de idade":    func(c map[string]string) { c["encarregado_data_nascimento"] = "2012-01-01" },
	} {
		campos := camposCadastro("Aluno Sem Encarregado")
		ajuste(campos)
		rec := postCadastro(t, fx, campos)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: esperava 4xx, recebi %d %s", nome, rec.Code, rec.Body.String())
		}
	}
	if totalAgregados(t, fx, "Estudante") != antesEst || totalAgregados(t, fx, "Encarregado") != antesEnc || sms.total() != 0 {
		t.Fatalf("cadastro inválido não pode criar conta nem enviar SMS (estudantes %d→%d, encarregados %d→%d, sms %d)",
			antesEst, totalAgregados(t, fx, "Estudante"), antesEnc, totalAgregados(t, fx, "Encarregado"), sms.total())
	}
}

func TestEncarregado03AgeEmNomeDoEducandoSoOVinculadoESoNoNivelEscolar(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)
	telEnc := novoTelefone()
	resp := cadastrarEscolar(t, fx, "Aluno Do Encarregado", telEnc)
	estCodigo := codigoEstudante(resp)
	estID := dataMap(resp)["id"].(string)
	encInfo := dataMap(resp)["encarregado"].(map[string]any)
	encCodigo := encInfo["codigo"].(string)
	aplicarEventosEstudante(t, fx, estID, 0) // em produção, o gerenciador de projeções
	tokenEnc := entrarTrocandoSenha(t, fx, encCodigo, senhaDoSMSDe(t, sms, encCodigo))

	// A conta do encarregado lista os seus educandos (fase "acao").
	rec, lista := chamarJSON(fx, http.MethodGet, "/encarregado/educandos", tokenEnc, nil)
	if rec.Code != http.StatusOK || lista["total"] != float64(1) {
		t.Fatalf("educandos: %d %v", rec.Code, lista)
	}
	if primeiro := lista["data"].([]any)[0].(map[string]any); primeiro["fase"] != "acao" || primeiro["codigo_estudante"] != estCodigo {
		t.Fatalf("o educando deveria estar em fase 'acao': %v", primeiro)
	}

	// Sem indicar o educando, as rotas do estudante não abrem.
	rec, resp1 := chamarJSON(fx, http.MethodGet, "/notas-estudante/"+estCodigo, tokenEnc, nil)
	if rec.Code != http.StatusForbidden || codigoDeErro(resp1) != "EDUCANDO_OBRIGATORIO" {
		t.Fatalf("sem X-Educando: esperava 403 EDUCANDO_OBRIGATORIO, recebi %d %v", rec.Code, resp1)
	}
	// Educando que não é dele: negado.
	outro := codigoEstudante(cadastrarEscolar(t, fx, "Aluno De Outro Encarregado", novoTelefone()))
	rec, resp2 := chamarComCabecalho(fx, http.MethodGet, "/notas-estudante/"+outro, tokenEnc, map[string]string{"X-Educando": outro}, nil)
	if rec.Code != http.StatusForbidden || codigoDeErro(resp2) != "EDUCANDO_NAO_VINCULADO" {
		t.Fatalf("educando alheio: esperava 403 EDUCANDO_NAO_VINCULADO, recebi %d %v", rec.Code, resp2)
	}
	reconstruirContas(t, fx)

	cab := map[string]string{"X-Educando": estCodigo}
	// Do educando dele: pode consultar como o estudante consultaria.
	if rec, r := chamarComCabecalho(fx, http.MethodGet, "/notas-estudante/"+estCodigo, tokenEnc, cab, nil); rec.Code != http.StatusOK {
		t.Fatalf("notas do educando: %d %v", rec.Code, r)
	}
	// Ações do próprio estudante continuam só dele.
	rec, resp3 := chamarComCabecalho(fx, http.MethodDelete, "/estudante/conta", tokenEnc, cab, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("excluir a conta do estudante: esperava 403, recebi %d %v", rec.Code, resp3)
	}
	// Rotas da academia nunca.
	if rec, _ := chamarComCabecalho(fx, http.MethodGet, "/academia/turmas", tokenEnc, cab, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("rota da academia como encarregado: esperava 403, recebi %d", rec.Code)
	}
	// /meu-perfil continua sendo o do ENCARREGADO, mesmo com o cabeçalho.
	if rec, perfil := chamarComCabecalho(fx, http.MethodGet, "/meu-perfil", tokenEnc, cab, nil); rec.Code != http.StatusOK || perfil["tipo"] != "encarregado" {
		t.Fatalf("/meu-perfil deveria ser o do encarregado: %d %v", rec.Code, perfil)
	}

	// O educando conclui o nível escolar: o encarregado só lê notas e faltas.
	if _, err := fx.client.DB().Exec(`UPDATE projection_estudantes SET status_escolar_medio = 'finalizado' WHERE id = $1::uuid`, estID); err != nil {
		t.Fatal(err)
	}
	if rec, r := chamarComCabecalho(fx, http.MethodGet, "/notas-estudante/"+estCodigo, tokenEnc, cab, nil); rec.Code != http.StatusOK {
		t.Fatalf("leitura de notas depois do escolar: %d %v", rec.Code, r)
	}
	for _, tc := range []struct{ metodo, caminho string }{
		{http.MethodPost, "/financeiro/mensalidades/pagamento"},
		{http.MethodGet, "/financeiro/mensalidades/estudante/" + estCodigo},
		{http.MethodPost, "/estudante/solicitacoes-status/desvinculacao"},
	} {
		rec, r := chamarComCabecalho(fx, tc.metodo, tc.caminho, tokenEnc, cab, map[string]any{})
		if rec.Code != http.StatusForbidden || codigoDeErro(r) != "ENCARREGADO_SOMENTE_LEITURA" {
			t.Errorf("%s %s depois do escolar: esperava 403 ENCARREGADO_SOMENTE_LEITURA, recebi %d %v", tc.metodo, tc.caminho, rec.Code, r)
		}
	}
}

func TestEncarregado04EstudanteDesvinculaOEncarregadoSoDepoisDoNivelEscolar(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)
	telEnc := novoTelefone()
	resp := cadastrarEscolar(t, fx, "Aluno Que Se Desvincula", telEnc)
	estCodigo, estID := codigoEstudante(resp), dataMap(resp)["id"].(string)
	encCodigo := dataMap(resp)["encarregado"].(map[string]any)["codigo"].(string)
	ultimo := aplicarEventosEstudante(t, fx, estID, 0)
	reconstruirContas(t, fx)

	// O estudante entra (trocando a senha temporária).
	_, login := chamarJSON(fx, http.MethodPost, "/login", "", map[string]any{"usuario": estCodigo, "senha": senhaDoSMSDe(t, sms, estCodigo)})
	tokenEst, _ := login["token"].(string)
	_, trocada := chamarJSON(fx, http.MethodPut, "/alterar-senha", tokenEst, map[string]any{"senha_atual": senhaDoSMSDe(t, sms, estCodigo), "nova_senha": "MinhaSenhaNova#9"})
	tokenEst, _ = trocada["token"].(string)
	aplicarEventosEstudante(t, fx, estID, ultimo)

	// Antes de concluir o nível escolar não pode desvincular o encarregado.
	if rec, r := chamarJSON(fx, http.MethodDelete, "/estudante/encarregado", tokenEst, nil); rec.Code != http.StatusConflict {
		t.Fatalf("antes de concluir o escolar: esperava 409, recebi %d %v", rec.Code, r)
	}
	// O encarregado não o faz por ele.
	tokenEnc := entrarTrocandoSenha(t, fx, encCodigo, senhaDoSMSDe(t, sms, encCodigo))
	if rec, _ := chamarComCabecalho(fx, http.MethodDelete, "/estudante/encarregado", tokenEnc, map[string]string{"X-Educando": estCodigo}, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("o encarregado não desvincula o estudante: %d", rec.Code)
	}

	if _, err := fx.client.DB().Exec(`UPDATE projection_estudantes SET ano_superior = '1_ano_superior' WHERE id = $1::uuid`, estID); err != nil {
		t.Fatal(err)
	}
	if rec, r := chamarJSON(fx, http.MethodDelete, "/estudante/encarregado", tokenEst, nil); rec.Code != http.StatusOK {
		t.Fatalf("depois de concluir o escolar: esperava 200, recebi %d %v", rec.Code, r)
	}
	reconstruirContas(t, fx)
	// O encarregado perde até a consulta do histórico.
	rec, r := chamarComCabecalho(fx, http.MethodGet, "/notas-estudante/"+estCodigo, tokenEnc, map[string]string{"X-Educando": estCodigo}, nil)
	if rec.Code != http.StatusForbidden || codigoDeErro(r) != "EDUCANDO_NAO_VINCULADO" {
		t.Fatalf("depois de desvinculado: esperava 403 EDUCANDO_NAO_VINCULADO, recebi %d %v", rec.Code, r)
	}
}

func TestEncarregado05AcademiaTrocaOEncarregadoNaMesmaTransacao(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)
	resp := cadastrarEscolar(t, fx, "Aluno Troca Encarregado", novoTelefone())
	estCodigo := codigoEstudante(resp)
	aplicarEventosEstudante(t, fx, dataMap(resp)["id"].(string), 0)
	antigo := dataMap(resp)["encarregado"].(map[string]any)["codigo"].(string)
	antigoToken := entrarTrocandoSenha(t, fx, antigo, senhaDoSMSDe(t, sms, antigo))

	telNovo := novoTelefone()
	rec, r := chamarJSON(fx, http.MethodPut, "/academia/estudante/"+estCodigo+"/encarregado", fx.token, map[string]any{
		"encarregado": map[string]any{"nome": "Novo Encarregado", "genero": "masculino", "data_nascimento": "1979-02-02", "telefone": telNovo, "relacao": "pai"},
	})
	if rec.Code != http.StatusOK || dataMap(r)["conta_nova"] != true {
		t.Fatalf("troca: %d %v", rec.Code, r)
	}
	if ultimo := sms.ultimo(t); ultimo.Destinatario != telNovo {
		t.Errorf("as credenciais do novo encarregado vão para o telefone dele (%s), foram para %s", telNovo, ultimo.Destinatario)
	}
	reconstruirContas(t, fx)
	// O antigo perde o acesso ao educando; o mesmo encarregado não pode ser "trocado" por ele mesmo.
	rec, r = chamarComCabecalho(fx, http.MethodGet, "/notas-estudante/"+estCodigo, antigoToken, map[string]string{"X-Educando": estCodigo}, nil)
	if rec.Code != http.StatusForbidden || codigoDeErro(r) != "EDUCANDO_NAO_VINCULADO" {
		t.Fatalf("o encarregado antigo deveria perder o acesso: %d %v", rec.Code, r)
	}
	rec, r = chamarJSON(fx, http.MethodPut, "/academia/estudante/"+estCodigo+"/encarregado", fx.token, map[string]any{
		"encarregado": map[string]any{"telefone": telNovo},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("trocar pelo mesmo encarregado deveria dar 409: %d %v", rec.Code, r)
	}
}

// ── Parte 5: reset de credenciais por perfil ────────────────────────────────

func TestCredenciaisReset01SecretarioSoComPermissaoEComoAAcademia(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)
	resp := cadastrarEscolar(t, fx, "Aluno Reset Secretario", novoTelefone())
	estCodigo, estID := codigoEstudante(resp), dataMap(resp)["id"].(string)
	aplicarEventosEstudante(t, fx, estID, 0)
	reconstruirContas(t, fx)

	_, _, semPerm := criarSecretario(t, fx, sms, []string{"estudantes.ver"})
	rec, r := chamarJSON(fx, http.MethodPost, "/estudantes/"+estCodigo+"/credenciais/reenviar", semPerm, nil)
	if rec.Code != http.StatusForbidden || codigoDeErro(r) != "SECRETARIO_SEM_PERMISSAO" {
		t.Fatalf("secretário sem estudantes.credenciais: esperava 403, recebi %d %v", rec.Code, r)
	}
	_, _, comPerm := criarSecretario(t, fx, sms, []string{"estudantes.credenciais"})
	antes := sms.total()
	rec, r = chamarJSON(fx, http.MethodPost, "/estudantes/"+estCodigo+"/credenciais/reenviar", comPerm, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("secretário com estudantes.credenciais: esperava 200, recebi %d %v", rec.Code, r)
	}
	if sms.total() != antes+1 {
		t.Fatalf("o reenvio deveria enviar 1 SMS")
	}
}

func TestCredenciaisReset02AdminAdmOuFppSoParaEstudanteSemNenhumaAcademia(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	prepararSMS(t, fx, true)
	telEnc := novoTelefone()
	comAcademia := cadastrarEscolar(t, fx, "Aluno Com Academia", telEnc)
	semAcademia := cadastrarEscolar(t, fx, "Aluno Sem Academia", telEnc)
	codCom, codSem := codigoEstudante(comAcademia), codigoEstudante(semAcademia)
	idSem := dataMap(semAcademia)["id"].(string)
	aplicarEventosEstudante(t, fx, dataMap(comAcademia)["id"].(string), 0)
	ultimoSem := aplicarEventosEstudante(t, fx, idSem, 0)
	reconstruirContas(t, fx)
	// O segundo estudante fica sem academia nenhuma (coluna e vínculos).
	if _, err := fx.client.DB().Exec(`UPDATE projection_estudantes SET codigo_academia = NULL WHERE id = $1::uuid`, idSem); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.client.DB().Exec(`UPDATE projection_estudante_vinculos SET desvinculado_em = now() WHERE estudante_id = $1::uuid`, idSem); err != nil {
		t.Fatal(err)
	}

	raiz := criarAdminCriador(t, fx)
	admID, _ := criarAdminParaTeste(t, fx, raiz, "adm", "SenhaAdm#123456")
	fppID, _ := criarAdminParaTeste(t, fx, raiz, "fpp", "SenhaFpp#123456")
	gerID, _ := criarAdminParaTeste(t, fx, raiz, "gerente", "SenhaGer#123456")
	for _, id := range []string{admID, fppID, gerID} {
		aplicarEventosAdmin(t, fx, id, 0)
	}
	tAdm, tFpp, tGer := tokenDe(t, admID, "admin"), tokenDe(t, fppID, "admin"), tokenDe(t, gerID, "admin")

	// Estudante COM academia: nem adm nem fpp resetam (é da academia).
	for nome, tk := range map[string]string{"adm": tAdm, "fpp": tFpp} {
		if rec, r := chamarJSON(fx, http.MethodPost, "/estudantes/"+codCom+"/credenciais/reenviar", tk, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s → estudante com academia: esperava 403, recebi %d %v", nome, rec.Code, r)
		}
	}
	// Estudante SEM academia: gerente não; adm e fpp sim.
	if rec, r := chamarJSON(fx, http.MethodPost, "/estudantes/"+codSem+"/credenciais/reenviar", tGer, nil); rec.Code != http.StatusForbidden {
		t.Errorf("gerente → esperava 403, recebi %d %v", rec.Code, r)
	}
	if rec, r := chamarJSON(fx, http.MethodPost, "/estudantes/"+codSem+"/credenciais/reenviar", tAdm, nil); rec.Code != http.StatusOK {
		t.Fatalf("adm → estudante sem academia: esperava 200, recebi %d %v", rec.Code, r)
	}
	// O intervalo de 10 minutos continua valendo para o fpp logo a seguir
	// (o evento do reenvio chega à projeção como no runtime real).
	aplicarEventosEstudante(t, fx, idSem, ultimoSem)
	if rec, _ := chamarJSON(fx, http.MethodPost, "/estudantes/"+codSem+"/credenciais/reenviar", tFpp, nil); rec.Code != http.StatusTooManyRequests {
		t.Errorf("segundo reenvio imediato deveria dar 429, recebi %d", rec.Code)
	}
}

func TestCredenciaisReset03EstudanteEscolarSemEncarregadoNuncaCaiNoTelefoneDele(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	sms := prepararSMS(t, fx, true)
	telEstudante := novoTelefone()
	campos := camposCadastro("Aluno Sem Encarregado Ativo")
	campos["telefone"] = telEstudante
	rec := postCadastro(t, fx, campos)
	if rec.Code != http.StatusCreated {
		t.Fatalf("cadastro: %d %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec.Body.Bytes())
	estCodigo := codigoEstudante(resp)
	aplicarEventosEstudante(t, fx, dataMap(resp)["id"].(string), 0)
	reconstruirContas(t, fx)
	// Simula "encarregado sem vínculo ativo".
	if _, err := fx.client.DB().Exec(`UPDATE projection_encarregado_educandos SET desvinculado_em = now() WHERE codigo_estudante = $1`, estCodigo); err != nil {
		t.Fatal(err)
	}
	antes := sms.total()
	rec2, r := chamarJSON(fx, http.MethodPost, "/estudantes/"+estCodigo+"/credenciais/reenviar", fx.token, nil)
	if rec2.Code != http.StatusBadRequest && rec2.Code != http.StatusUnprocessableEntity {
		t.Fatalf("sem encarregado ativo o reenvio deveria falhar (4xx), recebi %d %v", rec2.Code, r)
	}
	if sms.total() != antes {
		t.Fatalf("nenhum SMS pode sair (houve %d)", sms.total()-antes)
	}
	// E a regra única de contato, lida direto, devolve o mesmo erro.
	var id string
	_ = fx.client.DB().QueryRow(`SELECT id::text FROM projection_estudantes WHERE codigo_estudante = $1`, estCodigo).Scan(&id)
	if _, err := services.ResolverContatoDoEstudante(fx.client.DB(), mustUUID(t, id)); err == nil {
		t.Fatal("a regra única de contato deveria falhar para escolar sem encarregado")
	}
}

func mustUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	u, err := uuid.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
