package autenticacao

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/handlers/testsupport"
	"spuri/internal/projections"
)

// Tarefa 123 (SEC-04 / SEC-10) — as três projeções (admin, academia, estudante)
// aplicam os mesmos eventos às colunas deve_alterar_senha e
// sessoes_validas_desde. O instante vem do OccurredAt do evento (nunca de
// CURRENT_TIMESTAMP), para que um rebuild não invalide sessões em massa.

type alvoProjecao struct {
	nome      string
	tabela    string
	tipoAgg   string
	tipoSenha string // tipo do evento de troca de senha
	id        uuid.UUID
	handle    func(db.Event) error
}

func eventoArtesanal(a alvoProjecao, tipo string, payload any, versao int, quando time.Time) db.Event {
	raw, _ := json.Marshal(payload)
	return db.Event{
		EventID: uuid.New(), AggregateID: a.id, AggregateType: a.tipoAgg, EventType: tipo,
		EventVersion: versao, Payload: raw, Metadata: []byte(`{}`), OccurredAt: quando, RecordedAt: quando,
	}
}

func estadoSenha(t *testing.T, client *db.Client, a alvoProjecao) (deve bool, desde sql.NullTime, hash string) {
	t.Helper()
	if err := client.DB().QueryRow(`SELECT deve_alterar_senha, sessoes_validas_desde, senha_hash FROM `+a.tabela+` WHERE id = $1`, a.id).Scan(&deve, &desde, &hash); err != nil {
		t.Fatal(err)
	}
	return
}

func TestIntegrationProjecoesAplicamSenhaTemporariaEMotivos(t *testing.T) {
	client := testsupport.IntegrationFinanceClient(t)

	estID, _, _ := seedEstudanteDoisFatores(t, client, "Qualquer#123", "ativo", true, false)
	acaID, _, _ := seedAcademiaDoisFatores(t, client, "Qualquer#123", false)
	admID, _ := seedAdminParaRecuperacao(t, client, "Qualquer#123", true)

	alvos := []alvoProjecao{
		{"estudante", "projection_estudantes", "Estudante", "SenhaAlterada", estID, projections.NewEstudanteProjection(client).Handle},
		{"academia", "projection_academias", "Academia", "AcademiaSenhaAlterada", acaID, projections.NewAcademiaProjection(client).Handle},
		{"admin", "projection_admins", "Admin", "AdminSenhaAlterada", admID, projections.NewAdminProjection(client).Handle},
	}

	for _, a := range alvos {
		a := a
		t.Run(a.nome, func(t *testing.T) {
			base := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Hour)
			hashNovo := func(s string) map[string]any {
				return map[string]any{"NovaSenhaHash": "$2a$10$" + s + "............................................", "AlteradaAt": base}
			}
			comMotivo := func(s, motivo string) map[string]any {
				m := hashNovo(s)
				m["Motivo"] = motivo
				return m
			}
			aplicar := func(tipo string, payload any, versao int, quando time.Time) {
				t.Helper()
				if err := a.handle(eventoArtesanal(a, tipo, payload, versao, quando)); err != nil {
					t.Fatalf("%s: %v", tipo, err)
				}
			}

			// 1) conta nasce com senha temporária.
			aplicar("SenhaTemporariaDefinida", map[string]any{"DefinidaEm": base}, 100, base)
			if deve, desde, _ := estadoSenha(t, client, a); !deve || desde.Valid {
				t.Fatalf("SenhaTemporariaDefinida: deve=%v desde=%v (quer true e NULL)", deve, desde)
			}

			// 2) evento antigo, sem motivo, equivale a alteracao_usuario: desliga a flag.
			aplicar(a.tipoSenha, hashNovo("a"), 101, base.Add(time.Minute))
			deve, desde, hash := estadoSenha(t, client, a)
			if deve || desde.Valid || hash == "" {
				t.Fatalf("troca sem motivo: deve=%v desde=%v", deve, desde)
			}

			// 3) credenciais_reenviadas: liga de novo e invalida sessões AO INSTANTE DO EVENTO.
			t3 := base.Add(10 * time.Minute)
			aplicar(a.tipoSenha, comMotivo("b", "credenciais_reenviadas"), 102, t3)
			deve, desde, _ = estadoSenha(t, client, a)
			if !deve || !desde.Valid || !desde.Time.Equal(t3) {
				t.Fatalf("credenciais_reenviadas: deve=%v desde=%v (quer true e %v)", deve, desde, t3)
			}

			// 4) reset_senha: desliga a flag e avança o instante de invalidação.
			t4 := base.Add(20 * time.Minute)
			aplicar(a.tipoSenha, comMotivo("c", "reset_senha"), 103, t4)
			deve, desde, _ = estadoSenha(t, client, a)
			if deve || !desde.Valid || !desde.Time.Equal(t4) {
				t.Fatalf("reset_senha: deve=%v desde=%v (quer false e %v)", deve, desde, t4)
			}

			// 5) alteracao_usuario: desliga a flag e NÃO mexe nas sessões.
			aplicar(a.tipoSenha, comMotivo("d", "alteracao_usuario"), 104, base.Add(30*time.Minute))
			deve, desde, _ = estadoSenha(t, client, a)
			if deve || !desde.Valid || !desde.Time.Equal(t4) {
				t.Fatalf("alteracao_usuario: deve=%v desde=%v (as sessões não podem ser tocadas; quer %v)", deve, desde, t4)
			}
		})
	}
}
