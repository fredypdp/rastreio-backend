package academias

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"spuri/internal/handlers/testsupport"
)

func TestRegisterAcademiaPublicaPassesContactFieldsToAggregate(t *testing.T) {
	source, err := os.ReadFile("academia_handlers.go")
	if err != nil {
		t.Fatalf("read academia handler source: %v", err)
	}
	fn := testsupport.ExtractFunctionSource(t, string(source), "func RegisterAcademiaPublica(")

	const expectedCall = "\t\treq.Endereco,\n\t\treq.Telefone,\n\t\treq.Email,\n\t\treq.Website,"
	if !strings.Contains(fn, expectedCall) {
		t.Fatal("RegisterAcademiaPublica deve repassar req.Telefone e req.Email para Academia.Criar, igual ao RegisterAcademia")
	}
}

func TestRegisterAcademiaPublicaDoesNotRequireAdminAuth(t *testing.T) {
	source, err := os.ReadFile("academia_handlers.go")
	if err != nil {
		t.Fatalf("read academia handler source: %v", err)
	}
	fn := testsupport.ExtractFunctionSource(t, string(source), "func RegisterAcademiaPublica(")

	forbidden := []string{"getAdminProjection(", "middleware.GetUserID(", "executorAdmin"}
	for _, term := range forbidden {
		if strings.Contains(fn, term) {
			t.Fatalf("RegisterAcademiaPublica não deve depender de autenticação/admin — encontrado %q", term)
		}
	}
}

func TestRegisterAcademiaPublicaForcesNilCriadoPor(t *testing.T) {
	source, err := os.ReadFile("academia_handlers.go")
	if err != nil {
		t.Fatalf("read academia handler source: %v", err)
	}
	fn := testsupport.ExtractFunctionSource(t, string(source), "func RegisterAcademiaPublica(")

	if !strings.Contains(fn, "\t\treq.AnosAcademicos,\n\t\tnil,") {
		t.Fatal("RegisterAcademiaPublica deve chamar academia.Criar com criadoPor=nil (cadastro público sem admin executor)")
	}
}

func TestRegisterAcademiaPublicaRequiresCustomPassword(t *testing.T) {
	source, err := os.ReadFile("academia_handlers.go")
	if err != nil {
		t.Fatalf("read academia handler source: %v", err)
	}
	fn := testsupport.ExtractFunctionSource(t, string(source), "func RegisterAcademiaPublica(")

	mustContain := []string{
		`c.PostForm("senha")`,
		`fmt.Errorf("senha é obrigatória")`,
		"utils.ValidateSenha(senha)",
	}
	for _, term := range mustContain {
		if !strings.Contains(fn, term) {
			t.Fatalf("RegisterAcademiaPublica deve exigir senha customizada — não encontrado: %q", term)
		}
	}
	// Tarefa 123: não existe mais senha padrão; a pública usa a senha do formulário.
	if strings.Contains(fn, "GenerateTempPassword") || strings.Contains(fn, "DefinirSenhaTemporaria") {
		t.Fatal("RegisterAcademiaPublica não deve gerar senha temporária; a senha é a escolhida pela academia")
	}
}

func TestRegisterAcademiaDoesNotAcceptCustomPassword(t *testing.T) {
	source, err := os.ReadFile("academia_handlers.go")
	if err != nil {
		t.Fatalf("read academia handler source: %v", err)
	}
	fn := testsupport.ExtractFunctionSource(t, string(source), "func RegisterAcademia(")

	if strings.Contains(fn, `c.PostForm("senha")`) {
		t.Fatal("RegisterAcademia (fluxo admin) não deve aceitar senha customizada — comportamento deve continuar exclusivo do cadastro público")
	}
}

func TestAcademiaCadastroPublicoRouteIsPublic(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(testsupport.RaizDoRepositorio(t), "cmd/server/main.go"))
	if err != nil {
		t.Fatalf("read main.go source: %v", err)
	}

	var routeLine string
	for _, line := range strings.Split(string(source), "\n") {
		if strings.Contains(line, "/academia/cadastro") {
			routeLine = line
			break
		}
	}
	if routeLine == "" {
		t.Fatal("rota POST /academia/cadastro não encontrada em cmd/server/main.go")
	}
	// A rota é pública (sem autenticação); o limite de taxa por IP (SEC-08,
	// Tarefa 127) é o único middleware permitido.
	for _, proibido := range []string{"AuthMiddleware", "RequireAcademia", "RequireAdmin", "RequireEstudante", "RequireFPP"} {
		if strings.Contains(routeLine, proibido) {
			t.Fatalf("rota /academia/cadastro deve ser pública, sem middleware de autenticação (%s): %q", proibido, routeLine)
		}
	}
	if !strings.Contains(routeLine, "router.POST(") {
		t.Fatalf("rota /academia/cadastro deve ser registrada diretamente em router.POST, fora de grupos autenticados: %q", routeLine)
	}
}
