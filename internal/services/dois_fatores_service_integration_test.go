package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"spuri/internal/db"
)

// Tarefa 122 — ciclo de vida dos desafios de 2FA contra PostgreSQL real.
// Rodam com RUN_POSTGRES_INTEGRATION=1 (mesmo contrato dos demais testes de
// integração). Cada teste usa um usuário novo (uuid aleatório): são repetíveis
// no mesmo banco, sem limpeza prévia.

type envioFake struct{ Destinatario, Nome, Finalidade, Codigo string }

type senderFake struct {
	mu       sync.Mutex
	enviados []envioFake
	falhar   error
}

func (s *senderFake) EnviarCodigoDoisFatores(destinatario, nome, finalidade, codigo string, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.falhar != nil {
		return s.falhar
	}
	s.enviados = append(s.enviados, envioFake{destinatario, nome, finalidade, codigo})
	return nil
}

func (s *senderFake) ultimo(t *testing.T) envioFake {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.enviados) == 0 {
		t.Fatal("nenhum código foi enviado")
	}
	return s.enviados[len(s.enviados)-1]
}

func (s *senderFake) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.enviados)
}

func servicoDFParaTeste(t *testing.T) (*DoisFatoresService, *senderFake, *sqlx.DB) {
	t.Helper()
	if os.Getenv("RUN_POSTGRES_INTEGRATION") != "1" {
		t.Skip("teste de integração requer RUN_POSTGRES_INTEGRATION=1 e PostgreSQL")
	}
	t.Setenv("JWT_SECRET", "segredo-de-teste-dois-fatores")
	anterior, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(anterior) })
	client, err := db.NewClient(db.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RunMigrations(); err != nil {
		t.Fatal(err)
	}
	sender := &senderFake{}
	return NewDoisFatoresService(client.DB(), sender), sender, client.DB()
}

// envelhecer empurra o created_at dos desafios do usuário para trás, vencendo o
// intervalo mínimo entre envios sem esperar 60 s reais.
func envelhecer(t *testing.T, conn *sqlx.DB, userID uuid.UUID) {
	t.Helper()
	if _, err := conn.Exec(`UPDATE dois_fatores_desafios SET created_at = created_at - INTERVAL '2 minutes' WHERE user_id = $1`, userID); err != nil {
		t.Fatal(err)
	}
}

func inserirDesafio(t *testing.T, conn *sqlx.DB, id, userID uuid.UUID, tipo, finalidade, codigo string, motivoInvalidado string) {
	t.Helper()
	hash, err := hashCodigoDoisFatores(id, userID, tipo, finalidade, codigo)
	if err != nil {
		t.Fatal(err)
	}
	invalidado := "NULL"
	motivo := "NULL"
	if motivoInvalidado != "" {
		invalidado = "NOW()"
		motivo = "'" + motivoInvalidado + "'"
	}
	_, err = conn.Exec(fmt.Sprintf(
		`INSERT INTO dois_fatores_desafios (id, user_id, user_type, finalidade, codigo_hash, email_destino, expira_em, invalidado_em, motivo_invalidacao)
		 VALUES ($1,$2,$3,$4,$5,'u@example.test', NOW() + INTERVAL '10 minutes', %s, %s)`, invalidado, motivo),
		id, userID, tipo, finalidade, hash)
	if err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationDoisFatoresCicloFelizEUsoUnico(t *testing.T) {
	svc, sender, _ := servicoDFParaTeste(t)
	ctx := context.Background()
	user := uuid.New()

	d, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "ana@example.test", "Ana", nil)
	if err != nil {
		t.Fatal(err)
	}
	envio := sender.ultimo(t)
	if envio.Destinatario != "ana@example.test" || envio.Finalidade != DoisFatoresFinalidadeLogin || len(envio.Codigo) != 6 {
		t.Fatalf("envio inesperado: %+v", envio)
	}
	if d.ExpiraEmSegundos != 600 || d.ReenvioEmSegundos != 60 {
		t.Fatalf("tempos inesperados: %+v", d)
	}

	if _, err := svc.Verificar(ctx, d.ID, envio.Codigo, DoisFatoresFinalidadeLogin, nil); err != nil {
		t.Fatalf("código correto deveria passar: %v", err)
	}
	// Uso único: o MESMO código não vale uma segunda vez.
	if _, err := svc.Verificar(ctx, d.ID, envio.Codigo, DoisFatoresFinalidadeLogin, nil); !errors.Is(err, ErrDoisFatoresCodigoUsado) {
		t.Fatalf("reutilizar o código deveria falhar com ErrDoisFatoresCodigoUsado, obteve %v", err)
	}
}

func TestIntegrationDoisFatoresCodigoNaoFicaEmTextoPuro(t *testing.T) {
	svc, sender, conn := servicoDFParaTeste(t)
	user := uuid.New()
	d, err := svc.Criar(context.Background(), user, "estudante", DoisFatoresFinalidadeAtivacao, "e@example.test", "E", nil)
	if err != nil {
		t.Fatal(err)
	}
	var hash string
	if err := conn.Get(&hash, `SELECT codigo_hash FROM dois_fatores_desafios WHERE id = $1`, d.ID); err != nil {
		t.Fatal(err)
	}
	if len(strings.TrimSpace(hash)) != 64 || strings.Contains(hash, sender.ultimo(t).Codigo) {
		t.Fatalf("o código não pode ser guardado em texto puro: %q", hash)
	}
}

// Requisito do pedido: códigos iguais gerados em momentos diferentes têm de ser
// distinguíveis. O código vale pelo ESTADO do desafio (id/data), não pelo valor.
func TestIntegrationDoisFatoresMesmoCodigoEmDesafiosDiferentes(t *testing.T) {
	svc, _, conn := servicoDFParaTeste(t)
	ctx := context.Background()
	user := uuid.New()
	const codigoRepetido = "123456"

	// A: já foi substituído por B; B: o vivo. Mesmo código nos dois.
	a, b := uuid.New(), uuid.New()
	inserirDesafio(t, conn, a, user, "admin", DoisFatoresFinalidadeLogin, codigoRepetido, motivoSubstituido)
	inserirDesafio(t, conn, b, user, "admin", DoisFatoresFinalidadeLogin, codigoRepetido, "")

	if _, err := svc.Verificar(ctx, a, codigoRepetido, DoisFatoresFinalidadeLogin, nil); !errors.Is(err, ErrDoisFatoresCodigoSubstituido) {
		t.Fatalf("o desafio antigo (mesmo código) deveria estar substituído, obteve %v", err)
	}
	if _, err := svc.Verificar(ctx, b, codigoRepetido, DoisFatoresFinalidadeLogin, nil); err != nil {
		t.Fatalf("o desafio novo com o MESMO código deveria passar, obteve %v", err)
	}
	if _, err := svc.Verificar(ctx, b, codigoRepetido, DoisFatoresFinalidadeLogin, nil); !errors.Is(err, ErrDoisFatoresCodigoUsado) {
		t.Fatalf("B depois de usado deveria falhar como usado, obteve %v", err)
	}

	// Código já usado antes e sorteado de novo: o desafio seguinte NÃO é barrado.
	c := uuid.New()
	inserirDesafio(t, conn, c, user, "admin", DoisFatoresFinalidadeLogin, codigoRepetido, "")
	if _, err := svc.Verificar(ctx, c, codigoRepetido, DoisFatoresFinalidadeLogin, nil); err != nil {
		t.Fatalf("código repetido de um uso anterior não pode cair na invalidação: %v", err)
	}
	// E o desafio antigo continua usado (um não interfere no outro).
	if _, err := svc.Verificar(ctx, b, codigoRepetido, DoisFatoresFinalidadeLogin, nil); !errors.Is(err, ErrDoisFatoresCodigoUsado) {
		t.Fatalf("B continua usado, obteve %v", err)
	}
}

func TestIntegrationDoisFatoresCodigoDeOutroDesafioNaoServe(t *testing.T) {
	svc, sender, _ := servicoDFParaTeste(t)
	ctx := context.Background()
	user := uuid.New()
	d1, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
	if err != nil {
		t.Fatal(err)
	}
	codigo1 := sender.ultimo(t).Codigo
	other := uuid.New()
	d2, err := svc.Criar(ctx, other, "admin", DoisFatoresFinalidadeLogin, "v@example.test", "V", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = d2
	// O código do usuário 1 aplicado ao desafio do usuário 2 não pode passar
	// (salvo colisão de 1 em 1.000.000: re-sorteia para evitar flakiness).
	if sender.ultimo(t).Codigo == codigo1 {
		t.Skip("colisão improvável de códigos entre usuários diferentes; ignorar esta execução")
	}
	if _, err := svc.Verificar(ctx, d2.ID, codigo1, DoisFatoresFinalidadeLogin, nil); err == nil {
		t.Fatal("o código de outro desafio não pode ser aceito")
	}
	_ = d1
}

func TestIntegrationDoisFatoresExpiracao(t *testing.T) {
	svc, sender, conn := servicoDFParaTeste(t)
	ctx := context.Background()
	user := uuid.New()
	d, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
	if err != nil {
		t.Fatal(err)
	}
	codigo := sender.ultimo(t).Codigo
	if _, err := conn.Exec(`UPDATE dois_fatores_desafios SET expira_em = NOW() - INTERVAL '1 second' WHERE id = $1`, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verificar(ctx, d.ID, codigo, DoisFatoresFinalidadeLogin, nil); !errors.Is(err, ErrDoisFatoresCodigoExpirado) {
		t.Fatalf("código vencido deveria falhar com ErrDoisFatoresCodigoExpirado, obteve %v", err)
	}
}

func TestIntegrationDoisFatoresLimiteDeTentativas(t *testing.T) {
	svc, sender, conn := servicoDFParaTeste(t)
	ctx := context.Background()
	user := uuid.New()
	d, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
	if err != nil {
		t.Fatal(err)
	}
	correto := sender.ultimo(t).Codigo
	errado := "000000"
	if correto == errado {
		errado = "000001"
	}

	for i := 1; i <= DoisFatoresMaxTentativas; i++ {
		_, err := svc.Verificar(ctx, d.ID, errado, DoisFatoresFinalidadeLogin, nil)
		var inc *ErrDoisFatoresCodigoIncorreto
		if !errors.As(err, &inc) {
			t.Fatalf("tentativa %d: esperava ErrDoisFatoresCodigoIncorreto, obteve %v", i, err)
		}
		if inc.Restantes != DoisFatoresMaxTentativas-i {
			t.Fatalf("tentativa %d: restantes = %d", i, inc.Restantes)
		}
	}
	// Esgotado: nem o código CORRETO passa mais.
	if _, err := svc.Verificar(ctx, d.ID, correto, DoisFatoresFinalidadeLogin, nil); !errors.Is(err, ErrDoisFatoresTentativasExcedidas) {
		t.Fatalf("após 5 erros o desafio deve estar bloqueado, obteve %v", err)
	}
	var tentativas int
	if err := conn.Get(&tentativas, `SELECT tentativas FROM dois_fatores_desafios WHERE id = $1`, d.ID); err != nil || tentativas != DoisFatoresMaxTentativas {
		t.Fatalf("as tentativas erradas precisam ser persistidas: %d (%v)", tentativas, err)
	}

	// Um desafio esgotado não conta como "vivo": o usuário pede e recebe um código
	// novo na hora, sem cair no intervalo mínimo de 60 s.
	novo, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
	if err != nil {
		t.Fatalf("após esgotar as tentativas deveria ser possível pedir outro código: %v", err)
	}
	if _, err := svc.Verificar(ctx, novo.ID, sender.ultimo(t).Codigo, DoisFatoresFinalidadeLogin, nil); err != nil {
		t.Fatalf("o código novo deveria passar: %v", err)
	}
}

// Segunda camada: mesmo que o contador chegue ao máximo sem a invalidação (ex.:
// escrita concorrente), o desafio não aceita mais nenhum código.
func TestIntegrationDoisFatoresContadorNoMaximoBloqueiaMesmoSemInvalidacao(t *testing.T) {
	svc, sender, conn := servicoDFParaTeste(t)
	ctx := context.Background()
	user := uuid.New()
	d, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE dois_fatores_desafios SET tentativas = $2 WHERE id = $1`, d.ID, DoisFatoresMaxTentativas); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verificar(ctx, d.ID, sender.ultimo(t).Codigo, DoisFatoresFinalidadeLogin, nil); !errors.Is(err, ErrDoisFatoresTentativasExcedidas) {
		t.Fatalf("com o contador no máximo o código correto não pode passar, obteve %v", err)
	}
}

func TestIntegrationDoisFatoresFormatoInvalidoNaoConsomeTentativa(t *testing.T) {
	svc, _, conn := servicoDFParaTeste(t)
	ctx := context.Background()
	user := uuid.New()
	d, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, ruim := range []string{"12345", "1234567", "12a456", "", "      "} {
		if _, err := svc.Verificar(ctx, d.ID, ruim, DoisFatoresFinalidadeLogin, nil); !errors.Is(err, ErrDoisFatoresFormatoCodigo) {
			t.Errorf("%q deveria falhar com ErrDoisFatoresFormatoCodigo, obteve %v", ruim, err)
		}
	}
	var tentativas int
	if err := conn.Get(&tentativas, `SELECT tentativas FROM dois_fatores_desafios WHERE id = $1`, d.ID); err != nil || tentativas != 0 {
		t.Fatalf("formato inválido não pode consumir tentativas: %d (%v)", tentativas, err)
	}
}

func TestIntegrationDoisFatoresNovoCodigoInvalidaOAnterior(t *testing.T) {
	svc, sender, conn := servicoDFParaTeste(t)
	ctx := context.Background()
	user := uuid.New()
	d1, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
	if err != nil {
		t.Fatal(err)
	}
	codigo1 := sender.ultimo(t).Codigo
	envelhecer(t, conn, user)
	d2, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
	if err != nil {
		t.Fatal(err)
	}
	codigo2 := sender.ultimo(t).Codigo

	if _, err := svc.Verificar(ctx, d1.ID, codigo1, DoisFatoresFinalidadeLogin, nil); !errors.Is(err, ErrDoisFatoresCodigoSubstituido) {
		t.Fatalf("o código anterior deveria estar substituído, obteve %v", err)
	}
	if _, err := svc.Verificar(ctx, d2.ID, codigo2, DoisFatoresFinalidadeLogin, nil); err != nil {
		t.Fatalf("o código novo deveria passar: %v", err)
	}
	var vivos int
	if err := conn.Get(&vivos, `SELECT COUNT(*) FROM dois_fatores_desafios WHERE user_id = $1 AND usado_em IS NULL AND invalidado_em IS NULL`, user); err != nil || vivos != 0 {
		t.Fatalf("não deveria sobrar desafio vivo: %d (%v)", vivos, err)
	}
}

func TestIntegrationDoisFatoresIntervaloMinimoEntreEnvios(t *testing.T) {
	svc, sender, _ := servicoDFParaTeste(t)
	ctx := context.Background()
	user := uuid.New()
	d1, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
	var ag *ErrDoisFatoresAguardar
	if !errors.As(err, &ag) {
		t.Fatalf("segundo envio imediato deveria falhar com ErrDoisFatoresAguardar, obteve %v", err)
	}
	if ag.DesafioID != d1.ID || ag.Segundos < 1 || ag.Segundos > 60 {
		t.Fatalf("dados de espera inesperados: %+v", ag)
	}
	if sender.total() != 1 {
		t.Fatalf("o envio bloqueado não pode mandar e-mail: %d envios", sender.total())
	}
}

func TestIntegrationDoisFatoresTetoDeEnviosPorHoraNaoContaSucessos(t *testing.T) {
	svc, sender, conn := servicoDFParaTeste(t)
	ctx := context.Background()
	user := uuid.New()

	var primeiro uuid.UUID
	for i := 0; i < DoisFatoresMaxEnviosPorHora; i++ {
		envelhecer(t, conn, user)
		d, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
		if err != nil {
			t.Fatalf("envio %d deveria passar: %v", i+1, err)
		}
		if i == 0 {
			primeiro = d.ID
		}
	}
	envelhecer(t, conn, user)
	_, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
	var lim *ErrDoisFatoresLimiteEnvios
	if !errors.As(err, &lim) || lim.Segundos < 1 || lim.Segundos > 3600 {
		t.Fatalf("o envio acima do teto deveria falhar com ErrDoisFatoresLimiteEnvios, obteve %v", err)
	}

	// Um código USADO prova que o usuário é legítimo e não conta para o teto.
	if _, err := conn.Exec(`UPDATE dois_fatores_desafios SET usado_em = NOW() WHERE id = $1`, primeiro); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil); err != nil {
		t.Fatalf("após um sucesso o teto deveria liberar uma vaga: %v", err)
	}
	_ = sender
}

func TestIntegrationDoisFatoresFinalidadesIndependentes(t *testing.T) {
	svc, sender, _ := servicoDFParaTeste(t)
	ctx := context.Background()
	user := uuid.New()
	dLogin, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
	if err != nil {
		t.Fatal(err)
	}
	codLogin := sender.ultimo(t).Codigo
	dAtiv, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeAtivacao, "u@example.test", "U", nil)
	if err != nil {
		t.Fatalf("pedir outra finalidade não deveria cair no intervalo do login: %v", err)
	}
	codAtiv := sender.ultimo(t).Codigo

	// O desafio de login continua vivo e não aceita a finalidade de ativação.
	if _, err := svc.Verificar(ctx, dLogin.ID, codLogin, DoisFatoresFinalidadeAtivacao, nil); !errors.Is(err, ErrDoisFatoresDesafioInvalido) {
		t.Fatalf("finalidade errada deveria ser inválida, obteve %v", err)
	}
	if _, err := svc.Verificar(ctx, dLogin.ID, codLogin, DoisFatoresFinalidadeLogin, nil); err != nil {
		t.Fatalf("login deveria passar: %v", err)
	}
	if _, err := svc.Verificar(ctx, dAtiv.ID, codAtiv, DoisFatoresFinalidadeAtivacao, nil); err != nil {
		t.Fatalf("ativação deveria passar: %v", err)
	}
}

func TestIntegrationDoisFatoresDesafioAmarradoAoUsuario(t *testing.T) {
	svc, sender, _ := servicoDFParaTeste(t)
	ctx := context.Background()
	dono := uuid.New()
	d, err := svc.Criar(ctx, dono, "admin", DoisFatoresFinalidadeAtivacao, "u@example.test", "U", nil)
	if err != nil {
		t.Fatal(err)
	}
	codigo := sender.ultimo(t).Codigo

	if _, err := svc.Verificar(ctx, d.ID, codigo, DoisFatoresFinalidadeAtivacao, &UsuarioDoisFatores{ID: uuid.New(), Tipo: "admin"}); !errors.Is(err, ErrDoisFatoresDesafioInvalido) {
		t.Fatalf("outro usuário não pode usar o desafio, obteve %v", err)
	}
	if _, err := svc.Verificar(ctx, d.ID, codigo, DoisFatoresFinalidadeAtivacao, &UsuarioDoisFatores{ID: dono, Tipo: "estudante"}); !errors.Is(err, ErrDoisFatoresDesafioInvalido) {
		t.Fatalf("mesmo id com outro tipo não pode usar o desafio, obteve %v", err)
	}
	// As tentativas de quem não é dono não queimam o desafio do dono.
	if _, err := svc.Verificar(ctx, d.ID, codigo, DoisFatoresFinalidadeAtivacao, &UsuarioDoisFatores{ID: dono, Tipo: "admin"}); err != nil {
		t.Fatalf("o dono deveria conseguir usar o desafio: %v", err)
	}
}

func TestIntegrationDoisFatoresFalhaNoEnvioNaoDeixaDesafio(t *testing.T) {
	svc, sender, conn := servicoDFParaTeste(t)
	ctx := context.Background()
	user := uuid.New()
	sender.falhar = errors.New("brevo fora do ar")

	_, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
	if !errors.Is(err, ErrDoisFatoresEnvioFalhou) {
		t.Fatalf("esperava ErrDoisFatoresEnvioFalhou, obteve %v", err)
	}
	var n int
	if err := conn.Get(&n, `SELECT COUNT(*) FROM dois_fatores_desafios WHERE user_id = $1`, user); err != nil || n != 0 {
		t.Fatalf("o desafio de um código nunca entregue deve ser removido: %d (%v)", n, err)
	}

	// Sem o desafio fantasma, a próxima tentativa funciona de imediato.
	sender.falhar = nil
	if _, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil); err != nil {
		t.Fatalf("após falha de envio o usuário deve poder tentar de novo: %v", err)
	}
}

func TestIntegrationDoisFatoresReenvioPreservaJanelaDoLogin(t *testing.T) {
	svc, _, conn := servicoDFParaTeste(t)
	ctx := context.Background()
	user := uuid.New()
	d1, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
	if err != nil {
		t.Fatal(err)
	}

	origem, err := svc.ObterParaReenvio(ctx, d1.ID)
	if err != nil {
		t.Fatalf("dentro da janela o reenvio deveria ser permitido: %v", err)
	}
	envelhecer(t, conn, user)
	d2, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", &origem.IniciadoEm)
	if err != nil {
		t.Fatal(err)
	}
	if d2.IniciadoEm.Sub(origem.IniciadoEm).Abs() > time.Millisecond {
		t.Fatalf("o reenvio deve herdar o instante da senha correta: %v vs %v", d2.IniciadoEm, origem.IniciadoEm)
	}

	// Passada a janela de 30 min desde a senha correta, o reenvio é recusado.
	if _, err := conn.Exec(`UPDATE dois_fatores_desafios SET iniciado_em = NOW() - INTERVAL '31 minutes' WHERE user_id = $1`, user); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ObterParaReenvio(ctx, d2.ID); !errors.Is(err, ErrDoisFatoresSessaoExpirada) {
		t.Fatalf("fora da janela deveria falhar com ErrDoisFatoresSessaoExpirada, obteve %v", err)
	}
	// Só desafios de login servem de origem de reenvio.
	dAtiv, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeAtivacao, "u@example.test", "U", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ObterParaReenvio(ctx, dAtiv.ID); !errors.Is(err, ErrDoisFatoresDesafioInvalido) {
		t.Fatalf("desafio de ativação não pode originar reenvio de login, obteve %v", err)
	}
}

func TestIntegrationDoisFatoresVerificacaoConcorrenteSoUmSucesso(t *testing.T) {
	svc, sender, _ := servicoDFParaTeste(t)
	ctx := context.Background()
	user := uuid.New()
	d, err := svc.Criar(ctx, user, "admin", DoisFatoresFinalidadeLogin, "u@example.test", "U", nil)
	if err != nil {
		t.Fatal(err)
	}
	codigo := sender.ultimo(t).Codigo

	const n = 8
	var wg sync.WaitGroup
	resultados := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Verificar(ctx, d.ID, codigo, DoisFatoresFinalidadeLogin, nil)
			resultados <- err
		}()
	}
	wg.Wait()
	close(resultados)
	sucessos := 0
	for err := range resultados {
		if err == nil {
			sucessos++
		} else if !errors.Is(err, ErrDoisFatoresCodigoUsado) {
			t.Errorf("falha inesperada: %v", err)
		}
	}
	if sucessos != 1 {
		t.Fatalf("exatamente UMA verificação concorrente pode ter sucesso, houve %d", sucessos)
	}
}

func TestIntegrationDoisFatoresDesafioInexistente(t *testing.T) {
	svc, _, _ := servicoDFParaTeste(t)
	if _, err := svc.Verificar(context.Background(), uuid.New(), "123456", DoisFatoresFinalidadeLogin, nil); !errors.Is(err, ErrDoisFatoresDesafioInvalido) {
		t.Fatalf("desafio inexistente deveria ser inválido, obteve %v", err)
	}
	if _, err := svc.ObterParaReenvio(context.Background(), uuid.New()); !errors.Is(err, ErrDoisFatoresDesafioInvalido) {
		t.Fatalf("reenvio de desafio inexistente deveria ser inválido, obteve %v", err)
	}
}
