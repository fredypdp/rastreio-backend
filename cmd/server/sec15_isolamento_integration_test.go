package main

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"spuri/internal/middleware"
	"spuri/internal/projections"
)

// Tarefa 127 (SEC-15): isolamento entre academias depois de o estudante mudar de A para B.
//
//	A (anterior): vê o que ela própria registou, o seu período de eventos e um perfil reduzido;
//	B (atual):    vê só o que B registou e só os eventos do período de B;
//	C (nunca):    403 em tudo.
func TestSeguranca15IsolamentoEntreAcademiasAposTransferencia(t *testing.T) {
	fx := setupTurmaVinculoIntegration(t)
	prepararSMS(t, fx, true)

	terceira := criarAcademiaCorrecao(t, fx.repository, time.Now().UnixNano(), "Z")
	if err := projections.NewAcademiaProjection(fx.client).Rebuild(); err != nil {
		t.Fatal(err)
	}
	tokA := fx.token
	tokB, _ := middleware.GenerateToken(fx.outraAcademia.ID, "academia")
	tokC, _ := middleware.GenerateToken(terceira.ID, "academia")

	rec := postCadastro(t, fx, camposCadastro("Estudante Isolamento Quinze"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("cadastro: %d %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec.Body.Bytes())
	cod := codigoEstudante(resp)
	estID := dataMap(resp)["id"].(string)
	aplicarEventosEstudante(t, fx, estID, 0)
	aplicarEventos(t, fx, estID, 0, projections.NewEstudanteVinculosProjection(fx.client).Handle)

	sql := func(q string, a ...any) {
		t.Helper()
		if _, err := fx.client.DB().Exec(q, a...); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	// Linha do tempo determinística: A vinculada de 1 h atrás até daqui a 1 h
	// (os eventos de agora caem dentro); B só começa daqui a 2 h.
	sql(`UPDATE projection_estudante_vinculos SET vinculado_em = now() - interval '1 hour', desvinculado_em = now() + interval '1 hour'
	      WHERE codigo_estudante = $1 AND codigo_academia = $2`, cod, fx.academia.CodigoAcademia)
	sql(`UPDATE projection_estudantes SET codigo_academia = $2, status = 'ativo' WHERE codigo_estudante = $1`, cod, fx.outraAcademia.CodigoAcademia)
	sql(`INSERT INTO projection_estudante_vinculos (estudante_id,codigo_estudante,codigo_academia,tipo_ensino,ano_academico,evento_tipo,evento_version,vinculado_em)
	     VALUES ($1,$2,$3,'fundamental','1_ano_fundamental','EstudanteReintegrado',1, now() + interval '2 hours')`, estID, cod, fx.outraAcademia.CodigoAcademia)

	// Uma falta registada por cada academia (a matéria é só âncora da chave estrangeira).
	materiaID := uuid.NewString()
	sql(`INSERT INTO projection_materias (id, nome, type, codigo_academia) VALUES ($1, 'Matematica Isolamento', 'fundamental', $2)`, materiaID, fx.academia.CodigoAcademia)
	for i, academia := range []string{fx.academia.CodigoAcademia, fx.outraAcademia.CodigoAcademia} {
		sql(`INSERT INTO projection_faltas (id,codigo_estudante,codigo_academia,ano_lectivo,ano_academico,periodo,data,materia_disciplinar_id,quantidade,registered_at,event_id,version)
		     VALUES ($1,$2,$3,'2026','1_ano_fundamental','1_trimestre',$4::date,$5,1,CURRENT_TIMESTAMP,$6,1)`,
			uuid.NewString(), cod, academia, fmt.Sprintf("2026-02-1%d", i+1), materiaID, uuid.NewString())
	}

	faltasDaAcademia := func(tok string) (int, []string) {
		rr, corpo := chamarJSON(fx, http.MethodGet, "/faltas-estudante/"+cod, tok, nil)
		var academias []string
		if arr, ok := corpo["faltas"].([]any); ok {
			for _, f := range arr {
				if m, ok := f.(map[string]any); ok {
					academias = append(academias, fmt.Sprint(m["codigo_academia"]))
				}
			}
		}
		return rr.Code, academias
	}

	t.Run("anterior_ve_o_que_registou", func(t *testing.T) {
		code, academias := faltasDaAcademia(tokA)
		if code != http.StatusOK || len(academias) != 1 || academias[0] != fx.academia.CodigoAcademia {
			t.Fatalf("A (anterior) devia ver só a sua falta: HTTP %d %v", code, academias)
		}
		for _, rota := range []string{"/notas-estudante/", "/avaliacoes-estudante/"} {
			if rr, _ := chamarJSON(fx, http.MethodGet, rota+cod, tokA, nil); rr.Code != http.StatusOK {
				t.Fatalf("A (anterior) em %s: HTTP %d %s", rota, rr.Code, rr.Body.String())
			}
		}
	})

	t.Run("atual_nao_ve_o_que_outra_registou", func(t *testing.T) {
		code, academias := faltasDaAcademia(tokB)
		if code != http.StatusOK || len(academias) != 1 || academias[0] != fx.outraAcademia.CodigoAcademia {
			t.Fatalf("B (atual) devia ver só a sua falta: HTTP %d %v", code, academias)
		}
	})

	t.Run("nunca_vinculada_recebe_403", func(t *testing.T) {
		for _, rota := range []string{"/consultar-estudante/", "/notas-estudante/", "/faltas-estudante/", "/avaliacoes-estudante/", "/eventos-estudante/", "/verificar-integridade/"} {
			if rr, _ := chamarJSON(fx, http.MethodGet, rota+cod, tokC, nil); rr.Code != http.StatusForbidden {
				t.Fatalf("C em %s: esperava 403, obteve %d %s", rota, rr.Code, rr.Body.String())
			}
		}
	})

	t.Run("perfil_reduzido_da_anterior_nao_revela_a_academia_atual", func(t *testing.T) {
		rr, corpo := chamarJSON(fx, http.MethodGet, "/consultar-estudante/"+cod, tokA, nil)
		if rr.Code != http.StatusOK || corpo["tipo_acesso"] != "anterior" {
			t.Fatalf("perfil da anterior: %d %s", rr.Code, rr.Body.String())
		}
		est, _ := corpo["estudante"].(map[string]any)
		for _, proibido := range []string{"codigo_academia", "status_escolar_fundamental", "status_escolar_medio", "status_superior", "email", "telefone", "bilhete_identidade"} {
			if _, existe := est[proibido]; existe {
				t.Fatalf("o perfil reduzido não pode conter %q: %v", proibido, est)
			}
		}
		if est["status"] != "desvinculado" || est["nome"] == nil || est["codigo_estudante"] != cod {
			t.Fatalf("perfil reduzido incompleto: %v", est)
		}
	})

	t.Run("eventos_so_do_periodo_de_cada_academia", func(t *testing.T) {
		tiposVisiveis := func(tok string) []string {
			_, corpo := chamarJSON(fx, http.MethodGet, "/eventos-estudante/"+cod, tok, nil)
			var tipos []string
			if arr, ok := corpo["eventos"].([]any); ok {
				for _, e := range arr {
					if m, ok := e.(map[string]any); ok {
						tipos = append(tipos, fmt.Sprint(m["event_type"]))
					}
				}
			}
			return tipos
		}
		if tipos := tiposVisiveis(tokA); len(tipos) == 0 {
			t.Fatal("A devia ver os eventos do seu próprio período")
		}
		if tipos := tiposVisiveis(tokB); len(tipos) != 0 {
			t.Fatalf("B (atual) não pode ver eventos anteriores ao seu vínculo: %v", tipos)
		}
	})

	t.Run("evento_individual_fora_do_periodo_da_atual_e_404", func(t *testing.T) {
		var evID string
		_ = fx.client.DB().QueryRow(`SELECT event_id::text FROM spuri_ledger WHERE aggregate_id = $1 ORDER BY id LIMIT 1`, estID).Scan(&evID)
		if rr, _ := chamarJSON(fx, http.MethodGet, "/eventos/"+evID, tokA, nil); rr.Code != http.StatusOK {
			t.Fatalf("A devia ver o evento do seu período: %d", rr.Code)
		}
		if rr, _ := chamarJSON(fx, http.MethodGet, "/eventos/"+evID, tokB, nil); rr.Code != http.StatusNotFound {
			t.Fatalf("B não pode ver evento anterior ao seu vínculo: %d", rr.Code)
		}
	})
}
