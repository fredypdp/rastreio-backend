package cadastros

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func ptrInt(v int) *int { return &v }

func TestConfiguracaoFaltasRegras(t *testing.T) {
	por := uuid.New()
	ok := []struct {
		limite *int
		reprov bool
	}{{ptrInt(5), true}, {ptrInt(3), false}, {nil, false}, {ptrInt(1), true}, {ptrInt(MaxLimiteFaltasPorPeriodo), false}}
	for _, c := range ok {
		if _, err := NovaConfiguracaoFaltas("ACA1", c.limite, c.reprov, por); err != nil {
			t.Errorf("%v/%v deveria ser válido: %v", c.limite, c.reprov, err)
		}
	}
	invalidos := []struct {
		cod    string
		limite *int
		reprov bool
		msg    string
	}{
		{"", ptrInt(3), false, "codigo_academia"},
		{"ACA1", ptrInt(0), false, "entre 1 e"},
		{"ACA1", ptrInt(MaxLimiteFaltasPorPeriodo + 1), false, "entre 1 e"},
		{"ACA1", ptrInt(-3), true, "entre 1 e"},
		{"ACA1", nil, true, "exige limite"},
	}
	for _, c := range invalidos {
		_, err := NovaConfiguracaoFaltas(c.cod, c.limite, c.reprov, por)
		if err == nil || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%+v: %v", c, err)
		} else if _, regraErr := EhErroRegra(err); !regraErr {
			t.Errorf("deveria ser erro de regra: %T", err)
		}
	}
	// o limite é copiado: alterar o original não muda a configuração
	v := 4
	cfg, _ := NovaConfiguracaoFaltas("ACA1", &v, false, por)
	v = 99
	if *cfg.LimiteFaltasPorPeriodo != 4 {
		t.Fatal("o limite deveria ser copiado")
	}
}

func TestConfiguracaoFaltasDefinirSubstituiEAudita(t *testing.T) {
	client := clienteParaTeste(t)
	a := novaAcademia(t, client)
	repo := NovoFaltasConfiguracaoRepo(client)
	ctx := context.Background()

	c1, _ := NovaConfiguracaoFaltas(a.Codigo, ptrInt(5), true, a.ID)
	if err := repo.Definir(ctx, a.Audit, c1); err != nil || c1.Version != 1 {
		t.Fatalf("primeira definição: %v v%d", err, c1.Version)
	}
	c2, _ := NovaConfiguracaoFaltas(a.Codigo, ptrInt(8), false, a.ID)
	if err := repo.Definir(ctx, a.Audit, c2); err != nil || c2.Version != 2 {
		t.Fatalf("segunda definição: %v v%d", err, c2.Version)
	}
	var limite int
	var reprov bool
	if err := client.DB().QueryRow(`SELECT limite_faltas_por_periodo, reprovacao_por_faltas FROM projection_faltas_configuracao WHERE codigo_academia=$1`, a.Codigo).Scan(&limite, &reprov); err != nil || limite != 8 || reprov {
		t.Fatalf("registro = %d/%v %v", limite, reprov, err)
	}
	if got := acoesDeAuditoria(t, client, EntidadeConfiguracaoFaltas, a.Codigo); len(got) != 2 {
		t.Fatalf("auditoria = %v", got)
	}

	orfa, _ := NovaConfiguracaoFaltas("NAO-EXISTE-"+uuid.NewString()[:6], ptrInt(2), false, a.ID)
	if _, ok := EhErroRegra(repo.Definir(ctx, a.Audit, orfa)); !ok {
		t.Fatal("academia inexistente deveria ser recusada")
	}
}
