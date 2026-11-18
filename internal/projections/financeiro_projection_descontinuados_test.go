package projections

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"spuri/internal/db"
)

// Plano de Atualizações, Parte 1: a taxa de matrícula foi removida, mas os
// eventos MatriculaConfigurada/MatriculaConfiguracaoRemovida já gravados no
// ledger (imutável) continuam existindo. O replay tem de atravessá-los sem
// erro e sem efeito, senão um rebuild da projeção financeira pararia neles.
func TestFinanceiroProjectionAtravessaEventosDescontinuadosDaTaxaDeMatricula(t *testing.T) {
	t.Parallel()

	// Cliente nulo de propósito: o tratamento dos eventos descontinuados não
	// pode tocar o banco (se tocar, o teste falha com pânico).
	p := NewFinanceiroProjection(nil)

	for _, eventType := range []string{"MatriculaConfigurada", "MatriculaConfiguracaoRemovida"} {
		eventType := eventType
		t.Run(eventType, func(t *testing.T) {
			t.Parallel()
			payload, err := json.Marshal(map[string]any{"codigo_academia": "ACA001", "nivel": "fundamental"})
			if err != nil {
				t.Fatal(err)
			}
			err = p.Handle(db.Event{
				EventID:       uuid.New(),
				AggregateID:   uuid.New(),
				AggregateType: "Financeiro",
				EventType:     eventType,
				Payload:       payload,
			})
			if err != nil {
				t.Fatalf("Handle(%s) retornou erro %v, esperado nil (evento descontinuado é ignorado)", eventType, err)
			}
		})
	}
}
