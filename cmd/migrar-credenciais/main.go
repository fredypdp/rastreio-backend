// Comando migrar-credenciais — corrige as contas existentes com senha
// previsível (Tarefa 123, plano 3.9 / 5.3). Veja o pacote internal/credmigracao.
//
// Uso (a partir da raiz do backend, com DATABASE_URL apontando para o banco):
//
//	go run ./cmd/migrar-credenciais                       # dry-run: só conta
//	go run ./cmd/migrar-credenciais -marcar-contas        # passo 1: liga deve_alterar_senha
//	SENHAS_PUBLICAS_ADMIN='a,b,c,d' \
//	  go run ./cmd/migrar-credenciais -redefinir-admins   # redefinição imediata dos admins
//
// SENHAS_PUBLICAS_ADMIN (separadas por vírgula) fica fora do código de propósito:
// são as senhas que a Tarefa 123 eliminou do repositório.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"

	"spuri/internal/credmigracao"
	"spuri/internal/db"
)

func main() {
	marcar := flag.Bool("marcar-contas", false, "passo 1: marcar estudantes/academias com senha = código (deve_alterar_senha)")
	admins := flag.Bool("redefinir-admins", false, "redefinir já os admins com senha pública e enviar a nova por e-mail")
	flag.Parse()

	if strings.ToLower(strings.TrimSpace(os.Getenv("ENV"))) != "production" {
		_ = godotenv.Load()
	}
	client, err := db.NewClient(db.DefaultConfig())
	if err != nil {
		log.Fatalf("erro ao conectar ao banco: %v", err)
	}
	defer client.Close()

	var senhas []string
	for _, s := range strings.Split(os.Getenv("SENHAS_PUBLICAS_ADMIN"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			senhas = append(senhas, s)
		}
	}
	if *admins && len(senhas) == 0 {
		log.Fatal("-redefinir-admins exige SENHAS_PUBLICAS_ADMIN")
	}

	rel, err := credmigracao.Executar(context.Background(), client, db.NewAggregateRepository(client),
		credmigracao.Opcoes{MarcarContas: *marcar, RedefinirAdmins: *admins, SenhasPublicasAdmin: senhas})
	if rel != nil {
		modo := "DRY-RUN (nada foi alterado)"
		if *marcar || *admins {
			modo = "APLICADO"
		}
		fmt.Printf("== migrar-credenciais — %s ==\n", modo)
		fmt.Printf("estudantes: %d analisados, %d com senha = código, %d marcados\n", rel.EstudantesAnalisados, rel.EstudantesComSenhaIgualAoCodigo, rel.EstudantesMarcados)
		fmt.Printf("academias:  %d analisadas, %d com senha = código, %d marcadas\n", rel.AcademiasAnalisadas, rel.AcademiasComSenhaIgualAoCodigo, rel.AcademiasMarcadas)
		fmt.Printf("admins:     %d analisados, %d com senha pública, %d redefinidos\n", rel.AdminsAnalisados, rel.AdminsComSenhaPublica, rel.AdminsRedefinidos)
		for _, e := range rel.AdminsEmailNaoEnviado {
			fmt.Printf("ATENÇÃO: e-mail NÃO enviado ao admin %s — use \"Reenviar credenciais\"\n", e)
		}
		for _, e := range rel.Erros {
			fmt.Printf("erro: %s\n", e)
		}
	}
	if err != nil {
		log.Fatalf("falha: %v", err)
	}
}
