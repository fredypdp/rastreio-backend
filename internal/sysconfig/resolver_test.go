package sysconfig

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fonteFalsa struct {
	mu       sync.Mutex
	valores  map[string]string
	err      error
	chamadas int
}

func (f *fonteFalsa) Carregar(ctx context.Context) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chamadas++
	if f.err != nil {
		return nil, f.err
	}
	cp := map[string]string{}
	for k, v := range f.valores {
		cp[k] = v
	}
	return cp, nil
}

func (f *fonteFalsa) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.chamadas
}

func relogio(r *Resolver, t0 time.Time) *time.Time {
	agora := t0
	r.agora = func() time.Time { return agora }
	return &agora
}

func TestBancoVenceSobreOAmbienteEORemocaoVoltaAoAmbiente(t *testing.T) {
	t.Setenv("EMAIL_FROM", "ambiente@exemplo.ao")
	f := &fonteFalsa{valores: map[string]string{"EMAIL_FROM": "banco@exemplo.ao"}}
	r := NovoResolver(f, time.Minute)
	if err := r.Recarregar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if v, o := r.Valor("EMAIL_FROM"); v != "banco@exemplo.ao" || o != OrigemBanco {
		t.Fatalf("banco deveria vencer: %q %s", v, o)
	}
	f.valores = map[string]string{}
	_ = r.Recarregar(context.Background())
	if v, o := r.Valor("EMAIL_FROM"); v != "ambiente@exemplo.ao" || o != OrigemAmbiente {
		t.Fatalf("sem valor no banco deveria cair para o ambiente: %q %s", v, o)
	}
	t.Setenv("EMAIL_FROM", "")
	if v, o := r.Valor("EMAIL_FROM"); v != "" || o != OrigemNaoDefinida {
		t.Fatalf("sem banco nem ambiente: %q %s", v, o)
	}
}

func TestCacheSoRenovaDepoisDoVencimentoESemPolling(t *testing.T) {
	f := &fonteFalsa{valores: map[string]string{"LOG_LEVEL": "info"}}
	r := NovoResolver(f, time.Minute)
	agora := relogio(r, time.Now())
	_ = r.Recarregar(context.Background())
	if f.total() != 1 {
		t.Fatalf("carga inicial: %d", f.total())
	}
	for i := 0; i < 50; i++ {
		r.Valor("LOG_LEVEL")
	}
	if f.total() != 1 {
		t.Fatalf("dentro do TTL não pode haver nova consulta ao banco: %d", f.total())
	}
	f.valores = map[string]string{"LOG_LEVEL": "debug"}
	*agora = agora.Add(61 * time.Second)
	if v, _ := r.Valor("LOG_LEVEL"); v != "debug" || f.total() != 2 {
		t.Fatalf("depois do TTL deveria renovar: %q, consultas=%d", v, f.total())
	}
}

// O logger lê LOG_LEVEL em cada chamada: nunca pode acordar o banco.
func TestValorEmCacheNuncaConsultaOBanco(t *testing.T) {
	f := &fonteFalsa{valores: map[string]string{"LOG_LEVEL": "debug"}}
	r := NovoResolver(f, time.Minute)
	agora := relogio(r, time.Now())
	_ = r.Recarregar(context.Background())
	*agora = agora.Add(time.Hour) // cache vencido
	for i := 0; i < 20; i++ {
		if v, _ := r.ValorEmCache("LOG_LEVEL"); v != "debug" {
			t.Fatalf("valor em cache: %q", v)
		}
	}
	if f.total() != 1 {
		t.Fatalf("ValorEmCache consultou o banco: %d consultas", f.total())
	}
}

func TestFalhaNaRenovacaoMantemOCacheEEsperaParaTentarDeNovo(t *testing.T) {
	f := &fonteFalsa{valores: map[string]string{"EMAIL_FROM": "banco@exemplo.ao"}}
	r := NovoResolver(f, time.Minute)
	agora := relogio(r, time.Now())
	_ = r.Recarregar(context.Background())
	f.err = errors.New("banco fora do ar")
	*agora = agora.Add(2 * time.Minute)
	if v, o := r.Valor("EMAIL_FROM"); v != "banco@exemplo.ao" || o != OrigemBanco {
		t.Fatalf("com o banco fora do ar deveria servir o que está em memória: %q %s", v, o)
	}
	antes := f.total()
	for i := 0; i < 10; i++ {
		r.Valor("EMAIL_FROM")
	}
	if f.total() != antes {
		t.Fatalf("depois de falhar, não pode martelar o banco a cada leitura: %d → %d", antes, f.total())
	}
	f.err = nil
	*agora = agora.Add(esperaAposFalha + time.Second)
	r.Valor("EMAIL_FROM")
	if f.total() != antes+1 {
		t.Fatalf("passada a espera deveria tentar de novo: %d", f.total())
	}
}

func TestSemResolverGlobalLeSoOAmbiente(t *testing.T) {
	Parar()
	t.Setenv("BREVO_API_KEY", "do-ambiente")
	if v, o := Valor("BREVO_API_KEY"); v != "do-ambiente" || o != OrigemAmbiente {
		t.Fatalf("%q %s", v, o)
	}
	if StringEmCache("BREVO_API_KEY") != "do-ambiente" {
		t.Fatal("StringEmCache sem resolver deveria ler o ambiente")
	}
	if err := Recarregar(context.Background()); err != nil {
		t.Fatalf("Recarregar sem resolver não deveria falhar: %v", err)
	}
}

func TestCifrarDecifrar(t *testing.T) {
	t.Setenv("JWT_SECRET", "segredo-de-teste-com-tamanho-suficiente-123456")
	c, err := Cifrar("BREVO_API_KEY", "xkeysib-super-secreta")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c, "xkeysib") {
		t.Fatal("o texto cifrado contém o valor em claro")
	}
	c2, _ := Cifrar("BREVO_API_KEY", "xkeysib-super-secreta")
	if c == c2 {
		t.Fatal("duas cifras do mesmo valor deveriam diferir (nonce aleatório)")
	}
	claro, err := Decifrar("BREVO_API_KEY", c)
	if err != nil || claro != "xkeysib-super-secreta" {
		t.Fatalf("ida e volta: %q %v", claro, err)
	}
	// a chave de configuração é dado autenticado: copiar o cifrado para outra chave não decifra
	if _, err := Decifrar("ZIETT_API_KEY", c); err == nil {
		t.Fatal("texto cifrado de uma chave não pode decifrar em outra")
	}
}

func TestDecifrarComOutroJWTSecretFalhaSemVazarNada(t *testing.T) {
	t.Setenv("JWT_SECRET", "segredo-de-teste-com-tamanho-suficiente-123456")
	c, _ := Cifrar("MEGA_PASSWORD", "senha-mega-123")
	t.Setenv("JWT_SECRET", "outro-segredo-completamente-diferente-654321")
	_, err := Decifrar("MEGA_PASSWORD", c)
	if err == nil {
		t.Fatal("esperava erro")
	}
	if strings.Contains(err.Error(), "senha-mega-123") || strings.Contains(err.Error(), c) {
		t.Fatalf("o erro vaza segredo ou texto cifrado: %v", err)
	}
}
