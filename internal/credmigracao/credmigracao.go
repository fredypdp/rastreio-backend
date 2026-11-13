// Package credmigracao corrige as contas EXISTENTES afetadas pelas senhas
// previsíveis (Tarefa 123 — plano 3.9 e 5.3):
//
//   - estudantes e academias cuja senha é igual ao próprio código;
//   - administradores cuja senha é uma das senhas públicas antigas.
//
// O comando cmd/migrar-credenciais é um invólucro fino sobre Executar. O
// padrão é DRY-RUN: só conta. Nada é escrito sem as flags explícitas.
package credmigracao

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/services"
)

// Opcoes controla o que o comando faz. O zero value é o dry-run.
type Opcoes struct {
	// MarcarContas (passo 1, grátis): emite SenhaTemporariaDefinida para os
	// estudantes e academias com senha = código, ligando deve_alterar_senha. O
	// titular passa a ser obrigado a trocar a senha no próximo login. NÃO elimina
	// a janela de risco (quem sabe o código ainda pode entrar primeiro): o passo
	// 2 é o reenvio de credenciais pela interface (Reenviar credenciais).
	MarcarContas bool
	// RedefinirAdmins: redefinição IMEDIATA dos administradores com senha
	// pública — senha temporária por e-mail (Brevo) + invalidação de sessões.
	RedefinirAdmins bool
	// SenhasPublicasAdmin são as senhas antigas a procurar. Vêm de fora do
	// código (variável de ambiente), para que o repositório não volte a
	// conter as senhas que esta tarefa eliminou.
	SenhasPublicasAdmin []string
	// Paralelismo das comparações bcrypt (0 = nº de CPUs).
	Paralelismo int
}

// Relatorio resume o que foi encontrado e feito.
type Relatorio struct {
	EstudantesAnalisados, EstudantesComSenhaIgualAoCodigo, EstudantesMarcados int
	AcademiasAnalisadas, AcademiasComSenhaIgualAoCodigo, AcademiasMarcadas    int
	AdminsAnalisados, AdminsComSenhaPublica, AdminsRedefinidos                int
	AdminsEmailNaoEnviado                                                     []string // e-mails a quem as credenciais NÃO chegaram
	Erros                                                                     []string
}

type candidato struct {
	id     uuid.UUID
	chave  string // código (estudante/academia) ou e-mail (admin)
	hash   string
	tipo   string // "Estudante" | "Academia" | "Admin"
	nome   string
	correu bool
}

// Executar analisa as contas e, conforme Opcoes, corrige-as.
func Executar(ctx context.Context, client *db.Client, repo *db.AggregateRepository, op Opcoes) (*Relatorio, error) {
	rel := &Relatorio{}
	audit := db.AuditContext{UserID: "migracao-credenciais", UserType: "sistema", IP: "localhost"}

	// Protege os administradores: sem canal de e-mail não se redefine senha que
	// ninguém conseguiria receber.
	if op.RedefinirAdmins && !services.BrevoConfigurado() {
		return nil, fmt.Errorf("RedefinirAdmins exige BREVO_API_KEY e EMAIL_FROM configurados: sem e-mail os administradores ficariam sem acesso")
	}

	est, err := carregar(client, `SELECT id::text, codigo_estudante, senha_hash, nome FROM projection_estudantes WHERE deve_alterar_senha = FALSE AND senha_hash IS NOT NULL`, "Estudante")
	if err != nil {
		return nil, err
	}
	aca, err := carregar(client, `SELECT id::text, codigo_academia, senha_hash, nome FROM projection_academias WHERE deve_alterar_senha = FALSE AND senha_hash IS NOT NULL`, "Academia")
	if err != nil {
		return nil, err
	}
	rel.EstudantesAnalisados, rel.AcademiasAnalisadas = len(est), len(aca)

	igualAoCodigo := func(c candidato) bool { return bcrypt.CompareHashAndPassword([]byte(c.hash), []byte(c.chave)) == nil }
	est = filtrar(est, igualAoCodigo, op.Paralelismo)
	aca = filtrar(aca, igualAoCodigo, op.Paralelismo)
	rel.EstudantesComSenhaIgualAoCodigo, rel.AcademiasComSenhaIgualAoCodigo = len(est), len(aca)

	var admins []candidato
	if len(op.SenhasPublicasAdmin) > 0 {
		todos, err := carregar(client, `SELECT id::text, email, senha_hash, nome FROM projection_admins WHERE senha_hash IS NOT NULL`, "Admin")
		if err != nil {
			return nil, err
		}
		rel.AdminsAnalisados = len(todos)
		admins = filtrar(todos, func(c candidato) bool {
			for _, s := range op.SenhasPublicasAdmin {
				if bcrypt.CompareHashAndPassword([]byte(c.hash), []byte(s)) == nil {
					return true
				}
			}
			return false
		}, op.Paralelismo)
		rel.AdminsComSenhaPublica = len(admins)
	}

	if op.MarcarContas {
		for _, c := range append(append([]candidato{}, est...), aca...) {
			if err := ctx.Err(); err != nil {
				return rel, err
			}
			if err := marcar(repo, audit, c); err != nil {
				rel.Erros = append(rel.Erros, fmt.Sprintf("%s %s: %v", c.tipo, c.chave, err))
				continue
			}
			if c.tipo == "Estudante" {
				rel.EstudantesMarcados++
			} else {
				rel.AcademiasMarcadas++
			}
		}
	}

	if op.RedefinirAdmins {
		emailSvc := services.NewEmailService(client.DB())
		for _, c := range admins {
			if err := ctx.Err(); err != nil {
				return rel, err
			}
			senha, err := services.GenerateTempPassword()
			if err != nil {
				return rel, err
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
			if err != nil {
				return rel, err
			}
			agg, err := repo.Load(c.id, "Admin")
			if err != nil {
				rel.Erros = append(rel.Erros, fmt.Sprintf("admin %s: %v", c.chave, err))
				continue
			}
			adm, ok := agg.(*aggregates.Admin)
			if !ok {
				rel.Erros = append(rel.Erros, fmt.Sprintf("admin %s: agregado inesperado", c.chave))
				continue
			}
			if err := adm.AlterarSenha(string(hash), c.id, aggregates.MotivoSenhaCredenciaisReenviadas); err != nil {
				rel.Erros = append(rel.Erros, fmt.Sprintf("admin %s: %v", c.chave, err))
				continue
			}
			if err := repo.SaveWithAudit(adm, audit); err != nil {
				rel.Erros = append(rel.Erros, fmt.Sprintf("admin %s: %v", c.chave, err))
				continue
			}
			rel.AdminsRedefinidos++
			if err := emailSvc.SendAdminWelcomeEmail(adm.Email, adm.Nome, senha, adm.Role); err != nil {
				// A senha antiga já não vale; a nova não chegou. Outro admin fpp usa o
				// "Reenviar credenciais" (ou, se não houver, a recuperação por link).
				rel.AdminsEmailNaoEnviado = append(rel.AdminsEmailNaoEnviado, c.chave)
				log.Printf("[migrar-credenciais] e-mail NÃO enviado ao admin %s: %v", c.chave, err)
			}
		}
	}
	return rel, nil
}

func marcar(repo *db.AggregateRepository, audit db.AuditContext, c candidato) error {
	agg, err := repo.Load(c.id, c.tipo)
	if err != nil {
		return err
	}
	switch a := agg.(type) {
	case *aggregates.Estudante:
		if err := a.DefinirSenhaTemporaria(); err != nil {
			return err
		}
	case *aggregates.Academia:
		if err := a.DefinirSenhaTemporaria(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("agregado inesperado %T", agg)
	}
	return repo.SaveWithAudit(agg, audit)
}

func carregar(client *db.Client, query, tipo string) ([]candidato, error) {
	rows, err := client.DB().Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []candidato
	for rows.Next() {
		var idStr, chave, hash, nome string
		if err := rows.Scan(&idStr, &chave, &hash, &nome); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		out = append(out, candidato{id: id, chave: chave, hash: hash, tipo: tipo, nome: nome})
	}
	return out, rows.Err()
}

// filtrar mantém os candidatos para os quais pred é verdadeiro. As comparações
// bcrypt são lentas (por desenho), por isso correm em paralelo.
func filtrar(in []candidato, pred func(candidato) bool, paralelismo int) []candidato {
	if paralelismo <= 0 {
		paralelismo = runtime.NumCPU()
	}
	marca := make([]bool, len(in))
	var wg sync.WaitGroup
	trabalho := make(chan int)
	for w := 0; w < paralelismo; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range trabalho {
				marca[i] = pred(in[i])
			}
		}()
	}
	for i := range in {
		trabalho <- i
	}
	close(trabalho)
	wg.Wait()
	var out []candidato
	for i, c := range in {
		if marca[i] {
			out = append(out, c)
		}
	}
	return out
}
