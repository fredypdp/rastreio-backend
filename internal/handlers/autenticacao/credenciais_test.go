package autenticacao

import (
	"bytes"
	"context"
	"log"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"spuri/internal/db"
	"spuri/internal/handlers/apoio"
	"spuri/internal/services"
)

func sp(s string) *string { return &s }

// Tarefa 123 (SEC-04) + Parte 5: o SMS de credenciais vai para o telefone certo,
// no formato 9XXXXXXXX, pela regra única de contato. Para o nível escolar nunca
// cai no telefone do estudante e, sem nível definido, não há destino.
func TestDestinoDasCredenciaisDoEstudantePelaRegraUnicaDeContato(t *testing.T) {
	casos := []struct {
		nome                             string
		fund, medio, sup, telEst, telEnc *string
		quer                             string
		erro                             bool
	}{
		{"escolar fundamental → encarregado", sp("1_ano_fundamental"), nil, nil, sp("911111111"), sp("922222222"), "922222222", false},
		{"escolar médio → encarregado", nil, sp("10_ano_medio"), nil, sp("911111111"), sp("922222222"), "922222222", false},
		{"superior → o próprio estudante", nil, nil, sp("1_ano_superior"), sp("911111111"), sp("922222222"), "911111111", false},
		{"sem nível: não há destino (já não cai no estudante)", nil, nil, nil, sp("911111111"), sp("922222222"), "", true},
		{"normaliza espaços", sp("1_ano_fundamental"), nil, nil, nil, sp("922 222 222"), "922222222", false},
		{"normaliza +244", sp("1_ano_fundamental"), nil, nil, nil, sp("+244922222222"), "922222222", false},
		{"escolar sem telefone do encarregado (o do estudante não serve)", sp("1_ano_fundamental"), nil, nil, sp("911111111"), nil, "", true},
		{"superior sem telefone do estudante (o do encarregado não serve)", nil, nil, sp("1_ano_superior"), nil, sp("922222222"), "", true},
		{"telefone em branco", sp("1_ano_fundamental"), nil, nil, nil, sp("   "), "", true},
		{"fixo não é móvel", sp("1_ano_fundamental"), nil, nil, nil, sp("222123456"), "", true},
		{"curto demais", sp("1_ano_fundamental"), nil, nil, nil, sp("92222"), "", true},
	}
	val := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	for _, c := range casos {
		perfil := services.PerfilNivelEstudante{AnoFundamental: c.fund, AnoMedio: c.medio, AnoSuperior: c.sup}
		est := services.ContatoPessoa{Telefone: val(c.telEst)}
		var enc *services.ContatoPessoa
		if c.telEnc != nil {
			enc = &services.ContatoPessoa{Telefone: val(c.telEnc)}
		}
		got, err := services.ResolverContato(perfil, est, enc)
		if (err != nil) != c.erro || got.Telefone != c.quer {
			t.Errorf("%s: (%q, %v), quer (%q, erro=%v)", c.nome, got.Telefone, err, c.quer, c.erro)
		}
	}
}

func TestTelefoneDestinoCredenciaisAcademia(t *testing.T) {
	if _, err := apoio.TelefoneDestinoCredenciaisAcademia(nil); err == nil {
		t.Error("telefone ausente deve ser recusado")
	}
	if _, err := apoio.TelefoneDestinoCredenciaisAcademia(sp("  ")); err == nil {
		t.Error("telefone em branco deve ser recusado")
	}
	if _, err := apoio.TelefoneDestinoCredenciaisAcademia(sp("222123456")); err == nil {
		t.Error("telefone fixo deve ser recusado")
	}
	if got, err := apoio.TelefoneDestinoCredenciaisAcademia(sp("923 456 789")); err != nil || got != "923456789" {
		t.Errorf("923 456 789 → (%q, %v), quer 923456789", got, err)
	}
}

// Fora de produção NADA é enviado nem registado, e a senha só vai para o log
// com CREDENCIAIS_LOG_DEV=true.
func TestEnviarCredenciaisSMSForaDeProducao(t *testing.T) {
	gin.SetMode(gin.TestMode)
	chamado := false
	restaurar := apoio.SubstituirEnvioSMSParaTeste(func(context.Context, string, string, string) (string, error) {
		chamado = true
		return "x", nil
	})
	defer restaurar()

	var buf bytes.Buffer
	antigo := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(antigo)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	for _, env := range []string{"test", "development", "", "staging"} {
		t.Setenv("ENV", env)
		t.Setenv("CREDENCIAIS_LOG_DEV", "")
		buf.Reset()
		r := apoio.EnviarCredenciaisSMS(context.Background(), c, apoio.AtorComunicacao{}, db.AuditContext{}, "923456789", "Ana", "ABC1234", "Zk3mPq7w", false)
		if r.Enviada || r.Motivo != apoio.MotivoAmbienteNaoProducao || r.Canal != "sms" {
			t.Errorf("ENV=%q: resultado %+v", env, r)
		}
		if strings.Contains(buf.String(), "Zk3mPq7w") {
			t.Errorf("ENV=%q: a senha foi para o log sem CREDENCIAIS_LOG_DEV", env)
		}
	}
	if chamado {
		t.Fatal("fora de produção o provedor de SMS não pode ser chamado")
	}

	t.Setenv("ENV", "development")
	t.Setenv("CREDENCIAIS_LOG_DEV", "true")
	buf.Reset()
	apoio.EnviarCredenciaisSMS(context.Background(), c, apoio.AtorComunicacao{}, db.AuditContext{}, "923456789", "Ana", "ABC1234", "Zk3mPq7w", false)
	if !strings.Contains(buf.String(), "Zk3mPq7w") || !strings.Contains(buf.String(), "ABC1234") {
		t.Error("com CREDENCIAIS_LOG_DEV=true em desenvolvimento as credenciais deveriam ir ao log")
	}
}
