package projections

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"spuri/internal/db"
)

func clienteVinculosParaTeste(t *testing.T) *db.Client {
	t.Helper()
	if os.Getenv("SPURI_RUN_DB_INTEGRITY_TESTS") != "1" {
		t.Skip("set SPURI_RUN_DB_INTEGRITY_TESTS=1 with an isolated PostgreSQL database to run")
	}
	prevDir, _ := os.Getwd()
	_ = os.Chdir("../..")
	t.Cleanup(func() { _ = os.Chdir(prevDir) })
	client, err := db.NewClient(db.DefaultConfig())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RunMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	return client
}

// Tarefa 127 (migration 137): a projeção de vínculos não depende de projection_estudantes
// por chave estrangeira — nem a ordem entre projeções nem o rebuild de "estudantes" a
// podem esvaziar.
func TestVinculosSemChaveEstrangeiraParaEstudantes(t *testing.T) {
	client := clienteVinculosParaTeste(t)
	var fks int
	if err := client.DB().QueryRow(`SELECT COUNT(*) FROM pg_constraint
		WHERE conrelid = 'projection_estudante_vinculos'::regclass AND contype = 'f'`).Scan(&fks); err != nil {
		t.Fatal(err)
	}
	if fks != 0 {
		t.Fatalf("projection_estudante_vinculos não deve ter chaves estrangeiras (tem %d)", fks)
	}
}

func eventoEstudante(tipo string, estudante uuid.UUID, ocorreu time.Time, payload any) db.Event {
	corpo, _ := json.Marshal(payload)
	return db.Event{
		ID: time.Now().UnixNano(), EventID: uuid.New(), AggregateID: estudante, AggregateType: "Estudante",
		EventType: tipo, EventVersion: 1, Payload: corpo, OccurredAt: ocorreu, RecordedAt: ocorreu,
	}
}

func TestVinculosCriacaoEDelecaoFechamOPeriodo(t *testing.T) {
	client := clienteVinculosParaTeste(t)
	p := NewEstudanteVinculosProjection(client)
	estudante := uuid.New()
	cod := "V" + estudante.String()[:6]
	academia := "ACAD" + estudante.String()[:4]
	inicio := time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond)

	// Sem linha em projection_estudantes: com a FK isto falharia.
	if err := p.Handle(eventoEstudante("EstudanteCriadoComVinculo", estudante, inicio, map[string]any{
		"CodigoEstudante": cod, "CodigoAcademia": academia, "TipoEnsino": "medio", "AnoEscolarMedio": "1_ano_medio", "CreatedAt": inicio,
	})); err != nil {
		t.Fatalf("criação do vínculo: %v", err)
	}
	var aberto int
	_ = client.DB().QueryRow(`SELECT COUNT(*) FROM projection_estudante_vinculos WHERE estudante_id = $1 AND desvinculado_em IS NULL`, estudante).Scan(&aberto)
	if aberto != 1 {
		t.Fatalf("esperava 1 vínculo aberto, obteve %d", aberto)
	}

	fim := inicio.Add(30 * time.Minute)
	for i := 0; i < 2; i++ { // idempotente: repetir não altera o período já fechado
		if err := p.Handle(eventoEstudante("EstudanteDeletado", estudante, fim.Add(time.Duration(i)*time.Minute), map[string]any{})); err != nil {
			t.Fatalf("deleção %d: %v", i, err)
		}
	}
	var fechadoEm time.Time
	if err := client.DB().QueryRow(`SELECT desvinculado_em FROM projection_estudante_vinculos WHERE estudante_id = $1`, estudante).Scan(&fechadoEm); err != nil {
		t.Fatalf("o vínculo devia estar fechado: %v", err)
	}
	if !fechadoEm.Equal(fim) {
		t.Fatalf("desvinculado_em = %v, esperado %v (a 2.ª deleção não pode reescrever)", fechadoEm, fim)
	}
}
