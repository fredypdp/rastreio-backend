package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Tarefa 127 (SEC-15 / R-01): GET /estudantes com o filtro ?vinculo= e todos os demais
// filtros, para academia e admin. A versão anterior entrava em pânico sem filtros e
// deslocava os parâmetros $n quando havia filtros.
func TestIntegrationListarEstudantesVinculoEFiltros(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	marca := strings.ToUpper(uuid.NewString()[:6])
	academiaA := "LA" + marca
	academiaB := "LB" + marca
	idA := seedAcademiaParaCategoriaServico(t, client, academiaA)
	idB := seedAcademiaParaCategoriaServico(t, client, academiaB)

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := client.DB().Exec(q, args...); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	novo := func(nome, academia, status string) uuid.UUID {
		id := uuid.New()
		cod := "E" + geraDigitos(6)
		exec(`INSERT INTO projection_estudantes (id,nome,codigo_estudante,senha_hash,telefone,email,bilhete_identidade,codigo_academia,status,genero,created_at,updated_at)
		      VALUES ($1,$2,$3,'hash',$4,$5,$6,$7,$8,'feminino',now(),now())`,
			id, nome+" "+marca, cod, "9"+geraDigitos(8), cod+"@example.com", geraDigitos(9)+"LA042", academia, status)
		return id
	}
	vinculo := func(id uuid.UUID, academia string, desvinculado bool) {
		var fim interface{}
		if desvinculado {
			fim = time.Now()
		}
		exec(`INSERT INTO projection_estudante_vinculos (estudante_id,codigo_estudante,codigo_academia,tipo_ensino,ano_academico,evento_tipo,evento_version,vinculado_em,desvinculado_em)
		      SELECT id, codigo_estudante, $2, 'medio', '1_ano_medio', 'EstudanteCriadoComVinculo', 1, now() - interval '1 day', $3 FROM projection_estudantes WHERE id = $1`, id, academia, fim)
	}

	ana := novo("Ana", academiaA, "ativo")
	vinculo(ana, academiaA, false)
	beto := novo("Beto", academiaA, "inativo") // desvinculado, continua registado na A
	vinculo(beto, academiaA, true)
	carla := novo("Carla", academiaB, "ativo") // passou da A para a B
	vinculo(carla, academiaA, true)
	vinculo(carla, academiaB, false)
	dani := novo("Dani", academiaB, "ativo")
	vinculo(dani, academiaB, false)

	chamar := func(tipo string, id uuid.UUID, qs url.Values) (int, []map[string]any) {
		t.Helper()
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		qs.Set("limit", "50")
		ctx.Request = httptest.NewRequest(http.MethodGet, "/estudantes?"+qs.Encode(), nil)
		ctx.Set("dbClient", client)
		ctx.Set("user_type", tipo)
		ctx.Set("user_id", id)
		if tipo == "admin" {
			ctx.Set("admin_role", "fpp")
		}
		ListarEstudantes(ctx)
		var corpo struct {
			Estudantes []map[string]any `json:"estudantes"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &corpo)
		return rec.Code, corpo.Estudantes
	}
	nomes := func(linhas []map[string]any) string {
		var n []string
		for _, l := range linhas {
			n = append(n, strings.TrimSuffix(fmt.Sprint(l["nome"]), " "+marca))
		}
		sort.Strings(n)
		return strings.Join(n, ",")
	}
	esperar := func(quem string, tipo string, id uuid.UUID, qs url.Values, quer string) []map[string]any {
		t.Helper()
		code, linhas := chamar(tipo, id, qs)
		if code != http.StatusOK || nomes(linhas) != quer {
			t.Errorf("%s %v → HTTP %d, estudantes [%s]; esperado [%s]", quem, qs, code, nomes(linhas), quer)
		}
		return linhas
	}

	// ── academia A ────────────────────────────────────────────────────────
	linhas := esperar("A sem filtros", "academia", idA, url.Values{}, "Ana,Beto,Carla")
	porNome := map[string]map[string]any{}
	for _, l := range linhas {
		porNome[strings.TrimSuffix(fmt.Sprint(l["nome"]), " "+marca)] = l
	}
	if porNome["Ana"]["vinculo"] != "atual" || porNome["Beto"]["vinculo"] != "anterior" || porNome["Carla"]["vinculo"] != "anterior" {
		t.Errorf("campo vinculo por linha incorreto: Ana=%v Beto=%v Carla=%v", porNome["Ana"]["vinculo"], porNome["Beto"]["vinculo"], porNome["Carla"]["vinculo"])
	}
	// Carla já segue na B: a A só vê nome, código e vínculo.
	if c := porNome["Carla"]; c["codigo_academia"] != nil || c["email"] != nil || c["telefone"] != nil || c["bilhete_identidade"] != nil || c["status"] != nil || c["acesso"] != "reduzido" {
		t.Errorf("a linha de Carla na A devia ser reduzida: %v", c)
	}
	if b := porNome["Beto"]; b["codigo_academia"] != academiaA || b["email"] == nil {
		t.Errorf("Beto continua registado na A: devia vir completo: %v", b)
	}
	esperar("A vinculo=atual", "academia", idA, url.Values{"vinculo": {"atual"}}, "Ana")
	esperar("A vinculo=anterior", "academia", idA, url.Values{"vinculo": {"anterior"}}, "Beto,Carla")
	esperar("A vinculo=todos", "academia", idA, url.Values{"vinculo": {"todos"}}, "Ana,Beto,Carla")
	esperar("A busca=Ana", "academia", idA, url.Values{"busca": {"Ana"}}, "Ana")
	// Filtros e busca nunca alcançam quem já seguiu para outra academia.
	esperar("A busca=Carla", "academia", idA, url.Values{"busca": {"Carla"}}, "")
	esperar("A status=ativo", "academia", idA, url.Values{"status": {"ativo"}}, "Ana")
	esperar("A genero=feminino", "academia", idA, url.Values{"genero": {"feminino"}}, "Ana,Beto")
	esperar("A vinculo=anterior&busca=Beto", "academia", idA, url.Values{"vinculo": {"anterior"}, "busca": {"Beto"}}, "Beto")
	// ── academia B ────────────────────────────────────────────────────────
	esperar("B sem filtros", "academia", idB, url.Values{}, "Carla,Dani")
	esperar("B vinculo=atual", "academia", idB, url.Values{"vinculo": {"atual"}}, "Carla,Dani")
	esperar("B vinculo=anterior", "academia", idB, url.Values{"vinculo": {"anterior"}}, "")
	// ── vinculo inválido ──────────────────────────────────────────────────
	if code, _ := chamar("academia", idA, url.Values{"vinculo": {"qualquer"}}); code != http.StatusUnprocessableEntity && code != http.StatusBadRequest {
		t.Errorf("vinculo inválido devia dar erro de validação, deu %d", code)
	}

	// ── admin (o marcador no nome isola estes registos dos de outros testes) ──
	admin := uuid.New()
	esperar("admin busca=marca", "admin", admin, url.Values{"busca": {marca}}, "Ana,Beto,Carla,Dani")
	esperar("admin codigo_academia=A", "admin", admin, url.Values{"codigo_academia": {academiaA}}, "Ana,Beto,Carla")
	esperar("admin A vinculo=atual", "admin", admin, url.Values{"codigo_academia": {academiaA}, "vinculo": {"atual"}}, "Ana")
	esperar("admin A vinculo=anterior", "admin", admin, url.Values{"codigo_academia": {academiaA}, "vinculo": {"anterior"}}, "Beto,Carla")
	esperar("admin A vinculo=todos", "admin", admin, url.Values{"codigo_academia": {academiaA}, "vinculo": {"todos"}}, "Ana,Beto,Carla")
	esperar("admin A busca=Ana", "admin", admin, url.Values{"codigo_academia": {academiaA}, "busca": {"Ana"}}, "Ana")
	esperar("admin A+B busca=marca", "admin", admin, url.Values{"codigo_academia": {academiaA, academiaB}, "busca": {marca}}, "Ana,Beto,Carla,Dani")
	esperar("admin vinculo=atual (sem academia)", "admin", admin, url.Values{"busca": {marca}, "vinculo": {"atual"}}, "Ana,Carla,Dani")
	esperar("admin vinculo=anterior (sem academia)", "admin", admin, url.Values{"busca": {marca}, "vinculo": {"anterior"}}, "Beto")
	esperar("admin status=ativo", "admin", admin, url.Values{"busca": {marca}, "status": {"ativo"}}, "Ana,Carla,Dani")
}
