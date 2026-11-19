package administradores

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/handlers/testsupport"
	"spuri/internal/projections"
	"spuri/internal/services"
	"spuri/internal/sysconfig"
)

type viewConfig struct {
	Chave             string  `json:"chave"`
	Tipo              string  `json:"tipo"`
	Aplicacao         string  `json:"aplicacao"`
	Segredo           bool    `json:"segredo"`
	Origem            string  `json:"origem"`
	DefinidaNoBanco   bool    `json:"definida_no_banco"`
	Valor             *string `json:"valor"`
	Mascara           string  `json:"mascara"`
	AtualizadoPorID   *string `json:"atualizado_por_id"`
	AtualizadoPorNome *string `json:"atualizado_por_nome"`
}

var chavesDoTeste = []string{"EMAIL_FROM", "BREVO_API_KEY", "ZIETT_API_KEY", "JWT_EXPIRY_HOURS", "LOG_LEVEL", "RECONCILIACAO_ATIVA"}

// prepararConfigSistema liga o resolvedor ao banco real e deixa as chaves usadas
// sem valor gravado (os testes são repetíveis no mesmo banco). Ao final, também.
func prepararConfigSistema(t *testing.T) (*db.Client, uuid.UUID) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("JWT_SECRET", "ci-unified-jwt-secret")
	client := testsupport.IntegrationFinanceClient(t)
	admin := uuid.New()
	svc := func() *services.ConfiguracaoSistemaService {
		return services.NovaConfiguracaoSistemaService(client, db.NewAggregateRepository(client))
	}
	limpar := func() {
		for _, k := range chavesDoTeste {
			_, _ = svc().Remover(t.Context(), k, admin, "127.0.0.1")
			_, _ = client.DB().Exec(`DELETE FROM configuracao_sistema_segredos WHERE chave = $1`, k)
		}
		_ = sysconfig.Recarregar(t.Context())
	}
	sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteSQL{DB: client.DB().DB}, sysconfig.TTLPadrao))
	limpar()
	t.Cleanup(func() { limpar(); sysconfig.Parar() })
	return client, admin
}

func chamarConfig(client *db.Client, admin uuid.UUID, metodo, chave string, corpo any, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	var buf bytes.Buffer
	if corpo != nil {
		_ = json.NewEncoder(&buf).Encode(corpo)
	}
	caminho := "/admin/sistema/configuracoes"
	if chave != "" {
		caminho += "/" + chave
		ctx.Params = gin.Params{{Key: "chave", Value: chave}}
	}
	ctx.Request = httptest.NewRequest(metodo, caminho, &buf)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("dbClient", client)
	ctx.Set("repository", db.NewAggregateRepository(client))
	ctx.Set("user_id", admin)
	ctx.Set("user_type", "admin")
	handler(ctx)
	return rec
}

func listarConfig(t *testing.T, client *db.Client, admin uuid.UUID) map[string]viewConfig {
	t.Helper()
	rec := chamarConfig(client, admin, http.MethodGet, "", nil, ListarConfiguracoesSistema)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
	}
	var r struct {
		Data []viewConfig `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	out := map[string]viewConfig{}
	for _, v := range r.Data {
		out[v.Chave] = v
	}
	return out
}

func definirConfig(client *db.Client, admin uuid.UUID, chave, valor string) *httptest.ResponseRecorder {
	return chamarConfig(client, admin, http.MethodPut, chave, map[string]string{"valor": valor}, DefinirConfiguracaoSistema)
}

func TestIntegrationConfiguracaoSistemaListaComOrigemEMascara(t *testing.T) {
	client, admin := prepararConfigSistema(t)
	t.Setenv("EMAIL_FROM", "ambiente@exemplo.ao")
	t.Setenv("BREVO_API_KEY", "valor-secreto-do-ambiente")

	lista := listarConfig(t, client, admin)
	if len(lista) != len(sysconfig.Definicoes()) {
		t.Fatalf("a lista deveria trazer as %d configurações aprovadas, trouxe %d", len(sysconfig.Definicoes()), len(lista))
	}
	email := lista["EMAIL_FROM"]
	if email.Origem != "ambiente" || email.Valor == nil || *email.Valor != "ambiente@exemplo.ao" || email.DefinidaNoBanco {
		t.Fatalf("EMAIL_FROM só no ambiente: %+v", email)
	}
	brevo := lista["BREVO_API_KEY"]
	if !brevo.Segredo || brevo.Valor != nil || brevo.Mascara != services.MascaraSegredo || brevo.Origem != "ambiente" {
		t.Fatalf("segredo vem só com máscara, nunca com o valor: %+v", brevo)
	}
	if lista["MEGA_PASSWORD"].Mascara != "" && lista["MEGA_PASSWORD"].Origem == "nao_definida" {
		t.Fatalf("segredo sem valor em lugar nenhum não tem máscara: %+v", lista["MEGA_PASSWORD"])
	}
	rec := chamarConfig(client, admin, http.MethodGet, "", nil, ListarConfiguracoesSistema)
	if strings.Contains(rec.Body.String(), "valor-secreto-do-ambiente") {
		t.Fatal("o valor do segredo (do ambiente) vazou na listagem")
	}
	if lista["RECONCILIACAO_ATIVA"].Aplicacao != "reinicio" || lista["EMAIL_FROM"].Aplicacao != "imediato" {
		t.Fatal("cada configuração declara se vale na hora ou só no reinício")
	}
}

func TestIntegrationConfiguracaoSistemaDefineEAuditaDeXParaY(t *testing.T) {
	client, admin := prepararConfigSistema(t)
	t.Setenv("EMAIL_FROM", "ambiente@exemplo.ao")
	nome := "Admin Config " + uuid.NewString()[:6]
	if _, err := client.DB().Exec(`INSERT INTO projection_admins (id, nome, email, senha_hash, role) VALUES ($1,$2,$3,'x','fpp')`, admin, nome, admin.String()[:8]+"@config.test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = client.DB().Exec(`DELETE FROM projection_admins WHERE id = $1`, admin) })

	if rec := definirConfig(client, admin, "EMAIL_FROM", " primeiro@exemplo.ao "); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	if got := sysconfig.String("EMAIL_FROM"); got != "primeiro@exemplo.ao" {
		t.Fatalf("o valor gravado (normalizado) deveria valer na hora: %q", got)
	}
	if rec := definirConfig(client, admin, "EMAIL_FROM", "segundo@exemplo.ao"); rec.Code != http.StatusOK {
		t.Fatalf("PUT 2: %d %s", rec.Code, rec.Body.String())
	}
	v := listarConfig(t, client, admin)["EMAIL_FROM"]
	if v.Origem != "banco" || !v.DefinidaNoBanco || v.Valor == nil || *v.Valor != "segundo@exemplo.ao" {
		t.Fatalf("depois do PUT: %+v", v)
	}
	if v.AtualizadoPorID == nil || *v.AtualizadoPorID != admin.String() || v.AtualizadoPorNome == nil || *v.AtualizadoPorNome != nome {
		t.Fatalf("a tela precisa saber quem alterou: %+v", v)
	}

	hist, err := db.NewAggregateRepository(client).GetEventHistory(aggregates.ConfiguracaoSistemaAggregateID("EMAIL_FROM"))
	if err != nil || len(hist) < 2 {
		t.Fatalf("histórico: %v %d", err, len(hist))
	}
	ultimo := hist[len(hist)-1]
	var ev struct {
		Valor              string
		ValorAnterior      string
		TinhaValorAnterior bool
		DefinidoPor        uuid.UUID
	}
	if err := json.Unmarshal(ultimo.Payload, &ev); err != nil {
		t.Fatal(err)
	}
	if ultimo.EventType != "ConfiguracaoSistemaDefinida" || ev.ValorAnterior != "primeiro@exemplo.ao" || !ev.TinhaValorAnterior || ev.Valor != "segundo@exemplo.ao" || ev.DefinidoPor != admin {
		t.Fatalf("o evento deveria registrar de X para Y e quem: %s %+v", ultimo.EventType, ev)
	}
	if !strings.Contains(string(ultimo.Metadata), admin.String()) {
		t.Fatalf("a auditoria do ledger deveria registrar o admin: %s", ultimo.Metadata)
	}
}

func TestIntegrationConfiguracaoSistemaSegredoNuncaApareceEmLugarNenhum(t *testing.T) {
	client, admin := prepararConfigSistema(t)
	segredo := "SEGREDO-" + strings.ToUpper(uuid.NewString())
	outro := "OUTRO-" + strings.ToUpper(uuid.NewString())

	var logs bytes.Buffer
	antigo := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(antigo) })

	rec := definirConfig(client, admin, "BREVO_API_KEY", segredo)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), segredo) {
		t.Fatal("a resposta do PUT devolveu o segredo")
	}
	lista := chamarConfig(client, admin, http.MethodGet, "", nil, ListarConfiguracoesSistema)
	if strings.Contains(lista.Body.String(), segredo) {
		t.Fatal("a listagem devolveu o segredo")
	}
	v := listarConfig(t, client, admin)["BREVO_API_KEY"]
	if v.Valor != nil || v.Mascara != services.MascaraSegredo || v.Origem != "banco" {
		t.Fatalf("segredo gravado: só máscara, origem banco: %+v", v)
	}

	// O sistema usa o valor de verdade.
	if got := sysconfig.String("BREVO_API_KEY"); got != segredo {
		t.Fatalf("o valor efetivo deveria ser o segredo gravado: %q", got)
	}

	// Ledger (payload e metadata de TODOS os eventos), projeção e cofre.
	var noLedger int
	if err := client.DB().QueryRow(`SELECT COUNT(*) FROM spuri_ledger WHERE payload::text LIKE $1 OR metadata::text LIKE $1`, "%"+segredo+"%").Scan(&noLedger); err != nil || noLedger != 0 {
		t.Fatalf("o segredo não pode estar no ledger: %d %v", noLedger, err)
	}
	var valorProj *string
	if err := client.DB().QueryRow(`SELECT valor FROM projection_configuracao_sistema WHERE chave = 'BREVO_API_KEY'`).Scan(&valorProj); err != nil || valorProj != nil {
		t.Fatalf("a projeção não pode guardar o valor do segredo: %v %v", valorProj, err)
	}
	var cifrado string
	if err := client.DB().QueryRow(`SELECT cifrado FROM configuracao_sistema_segredos WHERE chave = 'BREVO_API_KEY'`).Scan(&cifrado); err != nil {
		t.Fatal(err)
	}
	if cifrado == "" || strings.Contains(cifrado, segredo) {
		t.Fatal("o cofre deve guardar só o texto cifrado")
	}
	var noLedgerCifrado int
	_ = client.DB().QueryRow(`SELECT COUNT(*) FROM spuri_ledger WHERE payload::text LIKE $1 OR metadata::text LIKE $1`, "%"+cifrado+"%").Scan(&noLedgerCifrado)
	if noLedgerCifrado != 0 {
		t.Fatal("nem o texto cifrado entra no ledger")
	}

	// Trocar o segredo substitui o anterior e também não vaza.
	if rec := definirConfig(client, admin, "BREVO_API_KEY", outro); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), outro) {
		t.Fatalf("PUT 2: %d %s", rec.Code, rec.Body.String())
	}
	if got := sysconfig.String("BREVO_API_KEY"); got != outro {
		t.Fatalf("o novo segredo deveria valer: %q", got)
	}
	var restos int
	_ = client.DB().QueryRow(`SELECT COUNT(*) FROM spuri_ledger WHERE payload::text LIKE $1 OR metadata::text LIKE $1 OR payload::text LIKE $2 OR metadata::text LIKE $2`, "%"+segredo+"%", "%"+outro+"%").Scan(&restos)
	if restos != 0 {
		t.Fatal("segredo no ledger depois da troca")
	}

	// Remover apaga o cofre e devolve a chave ao ambiente.
	t.Setenv("BREVO_API_KEY", "")
	if rec := chamarConfig(client, admin, http.MethodDelete, "BREVO_API_KEY", nil, RemoverConfiguracaoSistema); rec.Code != http.StatusOK {
		t.Fatalf("DELETE: %d %s", rec.Code, rec.Body.String())
	}
	var sobrou int
	_ = client.DB().QueryRow(`SELECT COUNT(*) FROM configuracao_sistema_segredos WHERE chave = 'BREVO_API_KEY'`).Scan(&sobrou)
	if sobrou != 0 {
		t.Fatal("o segredo removido não pode ficar no cofre")
	}
	if got := sysconfig.String("BREVO_API_KEY"); got != "" {
		t.Fatalf("sem banco nem ambiente: %q", got)
	}

	if strings.Contains(logs.String(), segredo) || strings.Contains(logs.String(), outro) {
		t.Fatalf("o segredo apareceu no log:\n%s", logs.String())
	}
}

func TestIntegrationConfiguracaoSistemaValidaERecusaChavesForaDaLista(t *testing.T) {
	client, admin := prepararConfigSistema(t)
	t.Setenv("JWT_EXPIRY_HOURS", "")

	casos := []struct {
		chave, valor string
		status       int
	}{
		{"EMAIL_FROM", "isto não é e-mail", http.StatusBadRequest},
		{"JWT_EXPIRY_HOURS", "0", http.StatusBadRequest},
		{"JWT_EXPIRY_HOURS", "", http.StatusBadRequest},
		{"LOG_LEVEL", "verboso", http.StatusBadRequest},
		{"DATABASE_URL", "postgres://outro", http.StatusNotFound},
		{"JWT_SECRET", "novo-segredo", http.StatusNotFound},
		{"CHAVE_INEXISTENTE", "x", http.StatusNotFound},
	}
	for _, c := range casos {
		rec := definirConfig(client, admin, c.chave, c.valor)
		if rec.Code != c.status {
			t.Errorf("PUT %s=%q: esperava %d, obteve %d %s", c.chave, c.valor, c.status, rec.Code, rec.Body.String())
		}
	}
	if got := sysconfig.String("JWT_EXPIRY_HOURS"); got != "" {
		t.Fatalf("PUT recusado não pode gravar nada: %q", got)
	}
	if rec := chamarConfig(client, admin, http.MethodDelete, "DATABASE_URL", nil, RemoverConfiguracaoSistema); rec.Code != http.StatusNotFound {
		t.Errorf("DELETE de chave fora da lista: %d", rec.Code)
	}
	if rec := chamarConfig(client, admin, http.MethodDelete, "LOG_LEVEL", nil, RemoverConfiguracaoSistema); rec.Code != http.StatusNotFound {
		t.Errorf("DELETE de chave sem valor no banco: %d", rec.Code)
	}
	if rec := chamarConfig(client, admin, http.MethodPut, "LOG_LEVEL", "corpo-que-nao-e-objeto", DefinirConfiguracaoSistema); rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("corpo inválido: %d", rec.Code)
	}
}

func TestIntegrationConfiguracaoSistemaRemoverVoltaAoAmbiente(t *testing.T) {
	client, admin := prepararConfigSistema(t)
	t.Setenv("LOG_LEVEL", "info")

	definirConfig(client, admin, "LOG_LEVEL", "debug")
	if got := sysconfig.String("LOG_LEVEL"); got != "debug" {
		t.Fatalf("banco: %q", got)
	}
	rec := chamarConfig(client, admin, http.MethodDelete, "LOG_LEVEL", nil, RemoverConfiguracaoSistema)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE: %d %s", rec.Code, rec.Body.String())
	}
	if got := sysconfig.String("LOG_LEVEL"); got != "info" {
		t.Fatalf("depois de remover vale o ambiente: %q", got)
	}
	v := listarConfig(t, client, admin)["LOG_LEVEL"]
	if v.Origem != "ambiente" || v.DefinidaNoBanco {
		t.Fatalf("origem depois de remover: %+v", v)
	}
	if rec := chamarConfig(client, admin, http.MethodDelete, "LOG_LEVEL", nil, RemoverConfiguracaoSistema); rec.Code != http.StatusNotFound {
		t.Fatalf("remover de novo: %d", rec.Code)
	}
}

func TestIntegrationConfiguracaoSistemaAvisaQuandoSoValeNoReinicio(t *testing.T) {
	client, admin := prepararConfigSistema(t)
	rec := definirConfig(client, admin, "RECONCILIACAO_ATIVA", "não")
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	var r struct {
		Message string     `json:"message"`
		Data    viewConfig `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if !strings.Contains(r.Message, "próximo reinício") || r.Data.Aplicacao != "reinicio" || r.Data.Valor == nil || *r.Data.Valor != "false" {
		t.Fatalf("a resposta deveria avisar do reinício e devolver o booleano normalizado: %+v", r)
	}
	rec = definirConfig(client, admin, "LOG_LEVEL", "debug")
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if strings.Contains(r.Message, "reinício") {
		t.Fatalf("configuração imediata não fala de reinício: %q", r.Message)
	}
}

// O gerenciador de projeções reaplica os eventos depois de o serviço já os ter aplicado
// na hora, e pode reaplicar fora de ordem: a guarda de versão tem de convergir sempre
// para o último estado e uma remoção nunca ressuscita um valor antigo.
func TestIntegrationConfiguracaoSistemaProjecaoIdempotenteEForaDeOrdem(t *testing.T) {
	client, admin := prepararConfigSistema(t)
	definirConfig(client, admin, "LOG_LEVEL", "debug")
	definirConfig(client, admin, "LOG_LEVEL", "info")
	chamarConfig(client, admin, http.MethodDelete, "LOG_LEVEL", nil, RemoverConfiguracaoSistema)

	hist, err := db.NewAggregateRepository(client).GetEventHistory(aggregates.ConfiguracaoSistemaAggregateID("LOG_LEVEL"))
	if err != nil || len(hist) < 3 {
		t.Fatalf("histórico: %v %d", err, len(hist))
	}
	proj := projections.NewConfiguracaoSistemaProjection(client)
	estado := func() (definida bool, existe bool) {
		linhas, err := proj.Listar()
		if err != nil {
			t.Fatal(err)
		}
		l, ok := linhas["LOG_LEVEL"]
		return l.Definida, ok
	}
	for i := len(hist) - 1; i >= 0; i-- { // do mais novo para o mais antigo
		if err := proj.Handle(hist[i]); err != nil {
			t.Fatal(err)
		}
	}
	if def, ok := estado(); !ok || def {
		t.Fatalf("reaplicar em ordem inversa não pode ressuscitar o valor removido (definida=%v)", def)
	}
	for _, ev := range hist { // e em ordem, duas vezes
		for i := 0; i < 2; i++ {
			if err := proj.Handle(ev); err != nil {
				t.Fatal(err)
			}
		}
	}
	if def, _ := estado(); def {
		t.Fatal("reaplicar em ordem, duas vezes, termina no último estado: removida")
	}
	// um valor mais novo depois da remoção vale
	definirConfig(client, admin, "LOG_LEVEL", "debug")
	if def, _ := estado(); !def {
		t.Fatal("nova definição depois da remoção deveria valer")
	}
	if got := sysconfig.String("LOG_LEVEL"); got != "debug" {
		t.Fatalf("valor efetivo: %q", got)
	}
}

// Reconstruir a projeção a partir do ledger devolve o mesmo estado e não toca no
// cofre: o segredo continua valendo.
func TestIntegrationConfiguracaoSistemaReconstrucaoDaProjecaoPreservaSegredos(t *testing.T) {
	client, admin := prepararConfigSistema(t)
	segredo := "REBUILD-" + uuid.NewString()
	definirConfig(client, admin, "ZIETT_API_KEY", segredo)
	definirConfig(client, admin, "EMAIL_FROM", "rebuild@exemplo.ao")

	antes := listarConfig(t, client, admin)
	if err := projections.NewConfiguracaoSistemaProjection(client).Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if err := sysconfig.Recarregar(t.Context()); err != nil {
		t.Fatal(err)
	}
	depois := listarConfig(t, client, admin)
	for _, k := range []string{"ZIETT_API_KEY", "EMAIL_FROM"} {
		if antes[k].Origem != depois[k].Origem || antes[k].DefinidaNoBanco != depois[k].DefinidaNoBanco {
			t.Fatalf("%s mudou com a reconstrução: %+v → %+v", k, antes[k], depois[k])
		}
	}
	if got := sysconfig.String("ZIETT_API_KEY"); got != segredo {
		t.Fatalf("o segredo tem de sobreviver à reconstrução da projeção: %q", got)
	}
	if got := sysconfig.String("EMAIL_FROM"); got != "rebuild@exemplo.ao" {
		t.Fatalf("valor comum após reconstrução: %q", got)
	}
}

// Segredo definido sem texto no cofre (ou que não decifra, por JWT_SECRET trocado) não
// derruba o sistema: cai para o ambiente.
func TestIntegrationConfiguracaoSistemaSegredoIlegivelCaiParaOAmbiente(t *testing.T) {
	client, admin := prepararConfigSistema(t)
	t.Setenv("BREVO_API_KEY", "do-ambiente")
	definirConfig(client, admin, "BREVO_API_KEY", "valor-no-cofre")
	if got := sysconfig.String("BREVO_API_KEY"); got != "valor-no-cofre" {
		t.Fatalf("antes de estragar: %q", got)
	}
	if _, err := client.DB().Exec(`UPDATE configuracao_sistema_segredos SET cifrado = 'lixo-que-nao-decifra' WHERE chave = 'BREVO_API_KEY'`); err != nil {
		t.Fatal(err)
	}
	if err := sysconfig.Recarregar(t.Context()); err != nil {
		t.Fatalf("segredo ilegível não pode falhar a carga inteira: %v", err)
	}
	if got := sysconfig.String("BREVO_API_KEY"); got != "do-ambiente" {
		t.Fatalf("segredo ilegível deveria cair para o ambiente: %q", got)
	}
}
