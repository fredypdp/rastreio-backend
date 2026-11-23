package services

import (
	"errors"
	"testing"
)

func p(s string) *string { return &s }

func TestResolverContatoPorNivel(t *testing.T) {
	est := ContatoPessoa{Nome: "Estudante", Telefone: "911111111", Email: p("est@x.ao"), EmailVerificado: true}
	enc := &ContatoPessoa{Nome: "Encarregado", Telefone: "+244 922 222 222", Email: p("enc@x.ao")}

	casos := []struct {
		nome        string
		perfil      PerfilNivelEstudante
		encarregado *ContatoPessoa
		origem      string
		telefone    string
		erro        error
	}{
		{"escolar fundamental usa o encarregado", PerfilNivelEstudante{AnoFundamental: p("3_ano_fundamental"), StatusFundamental: "em_andamento"}, enc, OrigemContatoEncarregado, "922222222", nil},
		{"escolar médio usa o encarregado", PerfilNivelEstudante{AnoMedio: p("2_ano_medio"), StatusMedio: "em_andamento"}, enc, OrigemContatoEncarregado, "922222222", nil},
		{"superior usa o próprio estudante", PerfilNivelEstudante{AnoSuperior: p("1_ano_superior")}, nil, OrigemContatoEstudante, "911111111", nil},
		{"superior ignora o encarregado que ainda esteja vinculado", PerfilNivelEstudante{AnoSuperior: p("1_ano_superior"), StatusMedio: "finalizado"}, enc, OrigemContatoEstudante, "911111111", nil},
		{"escolar SEM encarregado nunca cai no telefone do estudante", PerfilNivelEstudante{AnoMedio: p("2_ano_medio"), StatusMedio: "em_andamento"}, nil, "", "", ErrSemContatoDoEncarregado},
		{"médio concluído e desvinculado do encarregado responde por si", PerfilNivelEstudante{AnoMedio: p("4_ano_medio"), StatusMedio: "finalizado"}, nil, OrigemContatoEstudante, "911111111", nil},
		{"sem nível nenhum não tem contato", PerfilNivelEstudante{}, enc, "", "", ErrEstudanteSemNivel},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got, err := ResolverContato(c.perfil, est, c.encarregado)
			if c.erro != nil {
				if !errors.Is(err, c.erro) {
					t.Fatalf("esperava erro %v, recebi %v (%+v)", c.erro, err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if got.Origem != c.origem || got.Telefone != c.telefone {
				t.Fatalf("esperava %s/%s, recebi %s/%s", c.origem, c.telefone, got.Origem, got.Telefone)
			}
		})
	}
}

// O e-mail acompanha a mesma regra do telefone: escolar → do encarregado.
func TestResolverContatoEmailSegueOTelefone(t *testing.T) {
	est := ContatoPessoa{Nome: "E", Telefone: "911111111", Email: p("est@x.ao"), EmailVerificado: true}
	enc := &ContatoPessoa{Nome: "Enc", Telefone: "922222222", Email: p("enc@x.ao"), EmailVerificado: false}
	got, err := ResolverContato(PerfilNivelEstudante{AnoFundamental: p("1_ano_fundamental"), StatusFundamental: "em_andamento"}, est, enc)
	if err != nil || got.Email == nil || *got.Email != "enc@x.ao" || got.EmailVerificado {
		t.Fatalf("o e-mail deveria ser o do encarregado (não verificado): %+v %v", got, err)
	}
}

// Telefone inválido do encarregado é erro; jamais substituído pelo do estudante.
func TestResolverContatoTelefoneInvalidoDoEncarregadoNaoCaiNoDoEstudante(t *testing.T) {
	est := ContatoPessoa{Telefone: "911111111"}
	enc := &ContatoPessoa{Telefone: "123"}
	if got, err := ResolverContato(PerfilNivelEstudante{AnoMedio: p("1_ano_medio"), StatusMedio: "em_andamento"}, est, enc); err == nil {
		t.Fatalf("telefone inválido do encarregado deveria falhar, recebi %+v", got)
	}
}
