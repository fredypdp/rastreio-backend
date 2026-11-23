package permissoes

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizarAcrescentaImplicadasOrdenaERemoveDuplicadas(t *testing.T) {
	got, err := Normalizar([]string{"turmas.gerir", " turmas.ver ", "sumarios.gerir", "turmas.gerir", ""})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"sumarios.gerir", "sumarios.ver", "turmas.gerir", "turmas.ver"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// O secretário NUNCA recebe permissão fora do catálogo: nem lançar notas/faltas
// (só professor), nem gerir secretários (reservado à academia).
func TestNormalizarRecusaChavesForaDoCatalogo(t *testing.T) {
	for _, chave := range []string{"notas.lancar", "faltas.lancar", "secretarios.gerir", "academia.dados", "*", "TURMAS.VER"} {
		if _, err := Normalizar([]string{"turmas.ver", chave}); err == nil {
			t.Errorf("%q deveria ser recusada", chave)
		}
	}
}

func TestCatalogoCobreSumariosEExcluiOQueEReservado(t *testing.T) {
	for _, chave := range []string{"sumarios.ver", "sumarios.gerir"} {
		if !Existe(chave) {
			t.Errorf("o catálogo precisa da permissão %s (faltava no plano original)", chave)
		}
	}
	for _, p := range Catalogo {
		if strings.HasPrefix(p.Chave, "secretarios.") || strings.Contains(p.Chave, "lancar") {
			t.Errorf("permissão %s não pode ser delegável", p.Chave)
		}
		for _, impl := range p.Implica {
			if !Existe(impl) {
				t.Errorf("%s implica %s, que não existe", p.Chave, impl)
			}
		}
	}
}

func TestTemConsideraImplicacao(t *testing.T) {
	if !Tem([]string{"turmas.gerir"}, "turmas.ver") {
		t.Error("turmas.gerir deveria cobrir turmas.ver")
	}
	if Tem([]string{"turmas.ver"}, "turmas.gerir") {
		t.Error("turmas.ver não cobre turmas.gerir")
	}
	if Tem(nil, "turmas.ver") {
		t.Error("conjunto vazio não cobre nada")
	}
}

func TestTodasPermissoesNormalizadasSaoOCatalogoInteiro(t *testing.T) {
	norm, err := Normalizar(Todas())
	if err != nil || len(norm) != len(Catalogo) {
		t.Fatalf("Todas() deveria ser estável: %d vs %d (%v)", len(norm), len(Catalogo), err)
	}
}

func TestRegrasDeRotaSaoCoerentes(t *testing.T) {
	for _, chave := range RotasClassificadas() {
		parts := strings.SplitN(chave, " ", 2)
		r, ok := RegraDaRota(parts[0], parts[1])
		if !ok {
			t.Fatalf("%s listada mas sem regra", chave)
		}
		switch r.Tipo {
		case ExigePermissao:
			if !Existe(r.Permissao) {
				t.Errorf("%s exige permissão inexistente %q", chave, r.Permissao)
			}
		case Livre, Reservada, Negada:
			if r.Permissao != "" {
				t.Errorf("%s não deveria ter permissão", chave)
			}
		default:
			t.Errorf("%s com tipo inválido", chave)
		}
	}
	// Decisões que não podem mudar sem querer.
	casos := map[string]Tipo{
		"POST /academia/secretarios":              Reservada,
		"DELETE /academia/secretarios/:id":        Reservada,
		"PUT /academia/dados":                     Reservada,
		"POST /academia/notas-aluno":              Negada,
		"POST /academia/faltas-aluno":             Negada,
		"POST /financeiro/mensalidades/pagamento": Negada,
		"GET /academia/sumarios":                  ExigePermissao,
	}
	for chave, quer := range casos {
		parts := strings.SplitN(chave, " ", 2)
		if r, ok := RegraDaRota(parts[0], parts[1]); !ok || r.Tipo != quer {
			t.Errorf("%s: esperava tipo %d, recebi %+v (ok=%v)", chave, quer, r, ok)
		}
	}
	if _, ok := RegraDaRota("GET", "/rota/que/nao/existe"); ok {
		t.Error("rota desconhecida não pode ter regra (negar por padrão)")
	}
}
