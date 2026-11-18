package finance

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Registro em memória das cobranças pendentes.
//
// É o mecanismo que permite ao job de reconciliação trabalhar SEM consultar o
// banco a cada ciclo (o NeonDB cobra por tempo de computação ativo): o
// registro é carregado do banco uma única vez no arranque e depois mantido
// pelos próprios eventos que o Service já grava (criação e resolução de
// cobranças passam por Service.record). Enquanto uma cobrança continua
// pendente, nenhuma verificação lê ou escreve no banco.

// pendenteReconciliacao é o que o job precisa saber de uma cobrança pendente
// para consultá-la no provedor sem tocar no banco.
type pendenteReconciliacao struct {
	ID                 uuid.UUID
	Provedor, Metodo   string
	Contexto, Academia string
	ProviderID         string
	Merchant           string
	// CredencialID é a credencial emitente (uuid.Nil nas cobranças legadas).
	CredencialID uuid.UUID
	EmitidaEm    time.Time
	// Proxima é quando a cobrança deve ser verificada de novo.
	Proxima time.Time
	// Falhas conta erros consecutivos (provedor fora do ar, credencial
	// ausente...) para o recuo exponencial. Um erro nunca muda a cobrança.
	Falhas int
	// EfeitoPendente indica que a cobrança já foi gravada como paga mas os
	// efeitos de negócio (serviço extra) ainda não foram aplicados
	// com sucesso; eles são repetidos (são idempotentes) até darem certo.
	EfeitoPendente bool
}

type registroPendentes struct {
	mu    sync.Mutex
	itens map[uuid.UUID]*pendenteReconciliacao
	emVoo map[uuid.UUID]bool
	// acordar desperta o laço do job quando algo entra ou muda de data. Tem
	// buffer 1 e envio não bloqueante: vários avisos viram um só.
	acordar chan struct{}
}

func novoRegistroPendentes() *registroPendentes {
	return &registroPendentes{
		itens:   make(map[uuid.UUID]*pendenteReconciliacao),
		emVoo:   make(map[uuid.UUID]bool),
		acordar: make(chan struct{}, 1),
	}
}

func (r *registroPendentes) sinalizar() {
	select {
	case r.acordar <- struct{}{}:
	default:
	}
}

// Adicionar insere (ou substitui) uma pendente. Uma cobrança já presente
// mantém o contador de falhas e a próxima data (um evento repetido não deve
// antecipar a consulta).
func (r *registroPendentes) Adicionar(p pendenteReconciliacao) {
	r.mu.Lock()
	if atual, ok := r.itens[p.ID]; ok {
		p.Falhas, p.Proxima, p.EfeitoPendente = atual.Falhas, atual.Proxima, atual.EfeitoPendente
	}
	cp := p
	r.itens[p.ID] = &cp
	r.mu.Unlock()
	r.sinalizar()
}

// Remover tira a cobrança do registro (resolvida ou fora da janela).
func (r *registroPendentes) Remover(id uuid.UUID) {
	r.mu.Lock()
	delete(r.itens, id)
	r.mu.Unlock()
}

// RemoverSeLivre tira a cobrança do registro, exceto se o job a estiver
// processando agora: nesse caso é o próprio job quem decide o destino dela
// (por exemplo, repetir efeitos de negócio que falharam).
func (r *registroPendentes) RemoverSeLivre(id uuid.UUID) {
	r.mu.Lock()
	if !r.emVoo[id] {
		delete(r.itens, id)
	}
	r.mu.Unlock()
}

// Existe informa se a cobrança está no registro.
func (r *registroPendentes) Existe(id uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.itens[id]
	return ok
}

// Len devolve quantas cobranças estão registradas.
func (r *registroPendentes) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.itens)
}

// Vencidos devolve (e marca como em processamento) até max cobranças cuja
// próxima verificação já chegou. A ordem não é garantida (e não importa):
// todas as vencidas serão tratadas.
func (r *registroPendentes) Vencidos(agora time.Time, max int) []pendenteReconciliacao {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []pendenteReconciliacao
	for id, p := range r.itens {
		if r.emVoo[id] || p.Proxima.After(agora) {
			continue
		}
		r.emVoo[id] = true
		out = append(out, *p)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

// Concluir encerra o processamento de uma cobrança. atualizar recebe a versão
// corrente (se ainda estiver registrada) e devolve se ela deve permanecer.
func (r *registroPendentes) Concluir(id uuid.UUID, atualizar func(p *pendenteReconciliacao) bool) {
	r.mu.Lock()
	delete(r.emVoo, id)
	if p, ok := r.itens[id]; ok {
		if atualizar == nil || !atualizar(p) {
			delete(r.itens, id)
		}
	}
	r.mu.Unlock()
	r.sinalizar()
}

// ProximaData devolve a data mais próxima entre as cobranças que não estão em
// processamento; ok=false quando não há nenhuma (o laço então dorme sem timer).
func (r *registroPendentes) ProximaData() (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var menor time.Time
	achou := false
	for id, p := range r.itens {
		if r.emVoo[id] {
			continue
		}
		if !achou || p.Proxima.Before(menor) {
			menor, achou = p.Proxima, true
		}
	}
	return menor, achou
}

// RepontarCredencial troca a credencial emitente das pendentes de um contexto +
// provedor (usado quando uma edição de credenciais cria um snapshot: as
// pendentes passam a usar o snapshot, não a credencial editada).
func (r *registroPendentes) RepontarCredencial(contexto, academia, provedor string, de, para uuid.UUID) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, p := range r.itens {
		if p.Provedor == provedor && p.Contexto == contexto && p.Academia == academia && (p.CredencialID == de || p.CredencialID == uuid.Nil) {
			p.CredencialID = para
			n++
		}
	}
	return n
}
