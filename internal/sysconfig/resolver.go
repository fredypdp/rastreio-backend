package sysconfig

import (
	"context"
	"database/sql"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Origem diz de onde veio o valor efetivo de uma chave.
type Origem string

const (
	OrigemBanco       Origem = "banco"
	OrigemAmbiente    Origem = "ambiente"
	OrigemNaoDefinida Origem = "nao_definida"
)

// TTLPadrao é o intervalo máximo, entre instâncias, para uma alteração
// "imediata" ser vista: o cache é renovado de forma preguiçosa, só quando
// alguém lê depois do vencimento. Não há polling, então um servidor ocioso não
// acorda o banco por causa disto.
const TTLPadrao = 60 * time.Second

const (
	timeoutCarga    = 3 * time.Second
	esperaAposFalha = 30 * time.Second
)

// Fonte carrega do armazenamento todas as configurações gravadas, já com os
// segredos decifrados.
type Fonte interface {
	Carregar(ctx context.Context) (map[string]string, error)
}

// Resolver mantém as configurações do banco em memória.
type Resolver struct {
	fonte Fonte
	ttl   time.Duration
	agora func() time.Time

	mu         sync.RWMutex
	valores    map[string]string
	vencimento time.Time
}

// NovoResolver cria um resolvedor sem carregar nada: a primeira leitura (ou
// Recarregar) busca no banco.
func NovoResolver(f Fonte, ttl time.Duration) *Resolver {
	if ttl <= 0 {
		ttl = TTLPadrao
	}
	return &Resolver{fonte: f, ttl: ttl, agora: time.Now, valores: map[string]string{}}
}

// Recarregar busca tudo no banco agora. Em caso de erro mantém o que já estava
// em memória.
func (r *Resolver) Recarregar(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, timeoutCarga)
	defer cancel()
	v, err := r.fonte.Carregar(cctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.vencimento = r.agora().Add(esperaAposFalha)
		return err
	}
	r.valores = v
	r.vencimento = r.agora().Add(r.ttl)
	return nil
}

func (r *Resolver) vencido() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return !r.agora().Before(r.vencimento)
}

func (r *Resolver) lerCache(chave string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.valores[chave]
	return v, ok
}

// Valor devolve o valor efetivo e a origem. Se o cache venceu, renova antes.
func (r *Resolver) Valor(chave string) (string, Origem) {
	if r.vencido() {
		if err := r.Recarregar(context.Background()); err != nil {
			// Só a chave e o tipo do erro vão para o log; nunca valores.
			log.Printf("[sysconfig] não foi possível renovar as configurações do banco (usando o que já está em memória): %v", err)
		}
	}
	return r.resolverSemRenovar(chave)
}

// ValorEmCache devolve o valor sem nunca ir ao banco. É para quem lê com
// altíssima frequência (o logger) e não pode nem disparar a renovação.
func (r *Resolver) ValorEmCache(chave string) (string, Origem) {
	return r.resolverSemRenovar(chave)
}

func (r *Resolver) resolverSemRenovar(chave string) (string, Origem) {
	if v, ok := r.lerCache(chave); ok && v != "" {
		return v, OrigemBanco
	}
	if v := os.Getenv(chave); v != "" {
		return v, OrigemAmbiente
	}
	return "", OrigemNaoDefinida
}

var padrao atomic.Pointer[Resolver]

// Iniciar instala o resolvedor global e faz a primeira carga. Falha na carga
// não impede o arranque: o sistema segue só com o ambiente e tenta de novo na
// próxima leitura depois de esperaAposFalha.
func Iniciar(r *Resolver) {
	padrao.Store(r)
	if err := r.Recarregar(context.Background()); err != nil {
		log.Printf("[sysconfig] carga inicial falhou; seguindo só com variáveis de ambiente: %v", err)
	}
}

// Parar remove o resolvedor global (testes).
func Parar() { padrao.Store(nil) }

// Valor lê a chave: banco → ambiente.
func Valor(chave string) (string, Origem) {
	if r := padrao.Load(); r != nil {
		return r.Valor(chave)
	}
	if v := os.Getenv(chave); v != "" {
		return v, OrigemAmbiente
	}
	return "", OrigemNaoDefinida
}

// String lê a chave: banco → ambiente. Devolve "" se não houver em nenhum.
func String(chave string) string {
	v, _ := Valor(chave)
	return v
}

// StringEmCache é como String mas nunca consulta o banco (uso: logger).
func StringEmCache(chave string) string {
	if r := padrao.Load(); r != nil {
		v, _ := r.ValorEmCache(chave)
		return v
	}
	return os.Getenv(chave)
}

// Recarregar força a releitura do banco no resolvedor global.
func Recarregar(ctx context.Context) error {
	if r := padrao.Load(); r != nil {
		return r.Recarregar(ctx)
	}
	return nil
}

// FonteSQL lê as configurações da projeção e do cofre de segredos.
type FonteSQL struct{ DB *sql.DB }

const consultaCarga = `
SELECT c.chave, c.segredo, COALESCE(c.valor, ''), COALESCE(s.cifrado, '')
FROM projection_configuracao_sistema c
LEFT JOIN configuracao_sistema_segredos s ON s.chave = c.chave
WHERE c.definida`

// Carregar implementa Fonte. Segredo definido sem texto cifrado, ou que não
// decifra, é ignorado (a chave cai para o ambiente) e só o nome da chave vai
// para o log.
func (f FonteSQL) Carregar(ctx context.Context) (map[string]string, error) {
	rows, err := f.DB.QueryContext(ctx, consultaCarga)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var chave, valor, cifrado string
		var segredo bool
		if err := rows.Scan(&chave, &segredo, &valor, &cifrado); err != nil {
			return nil, err
		}
		if _, ok := Buscar(chave); !ok {
			continue // chave que saiu da lista aprovada: ignorada
		}
		if segredo {
			if cifrado == "" {
				log.Printf("[sysconfig] segredo %s está definido mas sem valor no cofre; usando o ambiente", chave)
				continue
			}
			claro, err := Decifrar(chave, cifrado)
			if err != nil {
				log.Printf("[sysconfig] %v; usando o ambiente", err)
				continue
			}
			valor = claro
		}
		if valor != "" {
			out[chave] = valor
		}
	}
	return out, rows.Err()
}

// FonteEstatica é uma Fonte em memória, para os testes dos pacotes que leem
// configuração: sysconfig.Iniciar(sysconfig.NovoResolver(sysconfig.FonteEstatica{...}, 0)).
type FonteEstatica map[string]string

func (f FonteEstatica) Carregar(context.Context) (map[string]string, error) {
	out := make(map[string]string, len(f))
	for k, v := range f {
		out[k] = v
	}
	return out, nil
}
