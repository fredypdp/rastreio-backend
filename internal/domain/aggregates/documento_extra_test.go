package aggregates

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestDocumentoExtraLifecycle(t *testing.T) {
	d := NewDocumentoExtra()

	// rotulo vazio
	if err := d.Criar("ACA", "", "pdf", true, []string{"6_ano_fundamental"}, uuid.New()); err == nil {
		t.Fatal("rotulo vazio aceito")
	}
	// tipo invalido
	if err := d.Criar("ACA", "Foto 3x4", "png", true, []string{"6_ano_fundamental"}, uuid.New()); err == nil {
		t.Fatal("tipo invalido aceito")
	}
	// anos_academicos vazio
	if err := d.Criar("ACA", "Foto 3x4", "jpg", true, nil, uuid.New()); err == nil {
		t.Fatal("anos_academicos vazio aceito")
	}
	if err := d.Criar("ACA", "Foto 3x4", "jpg", true, []string{}, uuid.New()); err == nil {
		t.Fatal("anos_academicos vazio (slice vazio) aceito")
	}
	// ano_academico invalido
	if err := d.Criar("ACA", "Foto 3x4", "jpg", true, []string{"99_ano_fundamental"}, uuid.New()); err == nil {
		t.Fatal("ano_academico invalido aceito")
	}
	// um ano válido misturado com um inválido: o conjunto inteiro é rejeitado
	if err := d.Criar("ACA", "Foto 3x4", "jpg", true, []string{"6_ano_fundamental", "99_ano_fundamental"}, uuid.New()); err == nil {
		t.Fatal("conjunto com ano invalido aceito")
	}
	// ano_academico em formato desconhecido
	if err := d.Criar("ACA", "Foto 3x4", "jpg", true, []string{"fundamental"}, uuid.New()); err == nil {
		t.Fatal("ano_academico sem sufixo reconhecido aceito")
	}

	if err := d.Criar("ACA", " Foto 3x4 ", "JPG", true, []string{"6_ano_fundamental"}, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if !d.Ativo || d.Rotulo != "Foto 3x4" || d.Tipo != "jpg" || !reflect.DeepEqual(d.AnosAcademicos, []string{"6_ano_fundamental"}) || !d.Obrigatorio {
		t.Fatalf("estado inesperado após Criar: %+v", d)
	}

	// múltiplos anos, inclusive cruzando níveis (fundamental + médio) — a
	// tela de configurações permite isso explicitamente (duas secções de
	// botões, seleção livre em ambas)
	dMulti := NewDocumentoExtra()
	if err := dMulti.Criar("ACA", "Atestado médico", "pdf", true, []string{"1_ano_medio", "9_ano_fundamental"}, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dMulti.AnosAcademicos, []string{"1_ano_medio", "9_ano_fundamental"}) {
		t.Fatalf("anos_academicos deveriam vir ordenados e sem duplicados, obtido %+v", dMulti.AnosAcademicos)
	}

	// duplicados no payload são removidos silenciosamente
	dDup := NewDocumentoExtra()
	if err := dDup.Criar("ACA", "Ficha médica", "pdf", false, []string{"2_ano_medio", "2_ano_medio"}, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dDup.AnosAcademicos, []string{"2_ano_medio"}) {
		t.Fatalf("duplicados deveriam ter sido removidos, obtido %+v", dDup.AnosAcademicos)
	}

	dSuperior := NewDocumentoExtra()
	if err := dSuperior.Criar("ACA", "Comprovativo", "pdf", false, []string{"1_ano_superior"}, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dSuperior.AnosAcademicos, []string{"1_ano_superior"}) {
		t.Fatalf("anos_academicos inesperado: %+v", dSuperior.AnosAcademicos)
	}

	// Atualizar
	if err := d.Atualizar("", "pdf", false, []string{"6_ano_fundamental"}, uuid.New()); err == nil {
		t.Fatal("rotulo vazio aceito em Atualizar")
	}
	if err := d.Atualizar("Foto tipo passe", "pdf", false, []string{"7_ano_fundamental"}, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if d.Rotulo != "Foto tipo passe" || d.Tipo != "pdf" || d.Obrigatorio || !reflect.DeepEqual(d.AnosAcademicos, []string{"7_ano_fundamental"}) {
		t.Fatalf("estado inesperado após Atualizar: %+v", d)
	}

	// Desativar / Reativar
	if err := d.Desativar(uuid.New()); err != nil {
		t.Fatal(err)
	}
	if d.Ativo {
		t.Fatal("deveria estar inativo")
	}
	if err := d.Desativar(uuid.New()); err == nil {
		t.Fatal("dupla desativação aceita")
	}
	if err := d.Reativar(uuid.New()); err != nil {
		t.Fatal(err)
	}
	if !d.Ativo {
		t.Fatal("deveria estar ativo")
	}
	if err := d.Reativar(uuid.New()); err == nil {
		t.Fatal("dupla reativação aceita")
	}
}
