package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
)

func seedEstudanteParaBusca(t *testing.T, client *db.Client, codigoAcademia, codigoEstudante, nome, bilhete, telefone, email string) {
	t.Helper()
	_, err := client.DB().Exec(`
		INSERT INTO projection_estudantes (id, nome, codigo_estudante, senha_hash, telefone, email, bilhete_identidade, codigo_academia, status, created_at, updated_at)
		VALUES ($1, $2, $3, 'hash', $4, $5, $6, $7, 'ativo', now(), now())
	`, uuid.New(), nome, codigoEstudante, telefone, email, bilhete, codigoAcademia)
	if err != nil {
		t.Fatal(err)
	}
}

// Tarefa 111: ?busca= é novo em GET /estudantes — antes a única forma de
// localizar um estudante específico era paginar a lista inteira (ou
// filtrar por categorias fixas como turma/curso/status), nunca por
// identidade (código, nome, BI, telefone ou e-mail). Cobre que o termo
// casa com as 5 colunas e que a busca continua isolada por academia.
func TestIntegrationListarEstudantesBuscaLivre(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := integrationFinanceClient(t)
	academia := "BUS" + uuid.NewString()[:6]
	outraAcademia := "OUT" + uuid.NewString()[:6]
	// ListarEstudantes (ramo "academia") resolve a academia pelo user_id
	// autenticado (busca em projection_academias por id, não por
	// codigo_academia do contexto) — por isso precisamos do UUID real.
	idAcademia := seedAcademiaParaCategoriaServico(t, client, academia)
	seedAcademiaParaCategoriaServico(t, client, outraAcademia)

	// BI e telefone únicos por execução (índices únicos globais na tabela)
	// para o teste poder ser reexecutado sem colidir com dados de uma
	// execução anterior.
	codigoAna := "E" + geraDigitos(6)
	codigoBeto := "E" + geraDigitos(6)
	biAna := geraDigitos(9) + "LA042"
	biBeto := geraDigitos(9) + "LA042"
	biOutraAna := geraDigitos(9) + "LA042"
	telAna, telBeto, telOutraAna := "9"+geraDigitos(8), "9"+geraDigitos(8), "9"+geraDigitos(8)
	seedEstudanteParaBusca(t, client, academia, codigoAna, "Ana Paula Silva", biAna, telAna, "ana.silva."+geraDigitos(6)+"@example.com")
	seedEstudanteParaBusca(t, client, academia, codigoBeto, "Beto Manuel", biBeto, telBeto, "beto."+geraDigitos(6)+"@example.com")
	// Mesmo nome em OUTRA academia — não deve aparecer na busca da primeira.
	seedEstudanteParaBusca(t, client, outraAcademia, "E"+geraDigitos(6), "Ana Paula Silva", biOutraAna, telOutraAna, "ana.outra."+geraDigitos(6)+"@example.com")

	type resposta struct {
		Estudantes []struct {
			Nome            string `json:"nome"`
			CodigoEstudante string `json:"codigo_estudante"`
		} `json:"estudantes"`
	}
	buscar := func(t *testing.T, termo string) []string {
		t.Helper()
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		qs := url.Values{"busca": {termo}, "limit": {"50"}, "offset": {"0"}}
		ctx.Request = httptest.NewRequest(http.MethodGet, "/estudantes?"+qs.Encode(), nil)
		ctx.Set("dbClient", client)
		ctx.Set("user_id", idAcademia)
		ctx.Set("user_type", "academia")
		ListarEstudantes(ctx)
		if recorder.Code != http.StatusOK {
			t.Fatalf("busca=%q: esperava 200, obteve %d: %s", termo, recorder.Code, recorder.Body.String())
		}
		var body resposta
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		var codigos []string
		for _, e := range body.Estudantes {
			codigos = append(codigos, e.CodigoEstudante)
		}
		return codigos
	}

	// Por nome (parcial, case-insensitive).
	if codigos := buscar(t, "ana paula"); len(codigos) != 1 || codigos[0] != codigoAna {
		t.Fatalf("busca por nome: esperava só %q, obteve %v", codigoAna, codigos)
	}
	// Por código.
	if codigos := buscar(t, codigoBeto); len(codigos) != 1 || codigos[0] != codigoBeto {
		t.Fatalf("busca por código: esperava só %q, obteve %v", codigoBeto, codigos)
	}
	// Por BI (parcial).
	if codigos := buscar(t, biAna[:9]); len(codigos) != 1 || codigos[0] != codigoAna {
		t.Fatalf("busca por BI: esperava só %q, obteve %v", codigoAna, codigos)
	}
	// Por telefone.
	if codigos := buscar(t, telBeto); len(codigos) != 1 || codigos[0] != codigoBeto {
		t.Fatalf("busca por telefone: esperava só %q, obteve %v", codigoBeto, codigos)
	}
	// Por e-mail (parcial).
	if codigos := buscar(t, "ana.silva."); len(codigos) != 1 || codigos[0] != codigoAna {
		t.Fatalf("busca por e-mail: esperava só %q, obteve %v", codigoAna, codigos)
	}
	// "Ana Paula Silva" existe em DUAS academias, mas a busca desta academia
	// só encontra a própria (isolamento por academia preservado).
	if codigos := buscar(t, "Ana Paula"); len(codigos) != 1 {
		t.Fatalf("busca isolada por academia: esperava 1 resultado, obteve %v", codigos)
	}
	// Sem correspondência.
	if codigos := buscar(t, "NomeQueNaoExisteDeJeitoNenhum"); len(codigos) != 0 {
		t.Fatalf("busca sem correspondência: esperava 0 resultados, obteve %v", codigos)
	}
}
