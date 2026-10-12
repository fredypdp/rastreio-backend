package projections

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
)

func (p *FinanceiroProjection) upsertMensalidadeCobrancas(chargeID uuid.UUID, academia string, payload map[string]any) error {
	raw := payload["mensalidades"]
	estudante, _ := payload["codigo_estudante"].(string)
	if raw == nil {
		if info, ok := payload["payment_info"].(map[string]any); ok {
			raw = info["mensalidades"]
			estudante, _ = info["codigo_estudante"].(string)
		}
	}
	if raw == nil || estudante == "" || academia == "" {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	var meses []struct {
		AnoLetivo string `json:"ano_letivo"`
		Mes       int    `json:"mes"`
	}
	if err := json.Unmarshal(b, &meses); err != nil {
		return err
	}
	for _, mes := range meses {
		if mes.AnoLetivo == "" || mes.Mes < 1 || mes.Mes > 12 {
			return fmt.Errorf("mês associado à cobrança inválido")
		}
		if _, err := p.client.DB().Exec(`INSERT INTO financeiro_mensalidade_cobrancas (charge_id,codigo_estudante,codigo_academia,ano_letivo,mes) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, chargeID, estudante, academia, mes.AnoLetivo, mes.Mes); err != nil {
			return err
		}
	}
	return nil
}

// provedorPadraoLegado e o provedor assumido para eventos gravados antes da
// Tarefa 119, quando a AppyPay era o único provedor e o payload não trazia o
// campo "provedor".
const provedorPadraoLegado = "APPYPAY"

// provedorDoPayload le o provedor de pagamento de um payload de evento
// financeiro, assumindo AppyPay para eventos anteriores ao campo existir.
func provedorDoPayload(v map[string]any) string {
	if p, _ := v["provedor"].(string); p != "" {
		return p
	}
	return provedorPadraoLegado
}

// FinanceiroProjection is the sole writer of financial read models. Its event
// payloads are deliberately secret-free, allowing deterministic replay.
type FinanceiroProjection struct{ client *db.Client }

func NewFinanceiroProjection(client *db.Client) *FinanceiroProjection {
	return &FinanceiroProjection{client: client}
}
func (p *FinanceiroProjection) Name() string { return "financeiro" }
func (p *FinanceiroProjection) GetLastProcessedEventID() (int64, error) {
	return NewBaseProjection(p.client).GetLastProcessedEventIDByName(p.Name())
}
func (p *FinanceiroProjection) UpdateCheckpoint(id int64) error {
	return NewBaseProjection(p.client).UpdateCheckpointByName(p.Name(), id)
}

func (p *FinanceiroProjection) Handle(e db.Event) error {
	if e.AggregateType != "Financeiro" {
		return nil
	}
	var v map[string]any
	if err := json.Unmarshal(e.Payload, &v); err != nil {
		return err
	}
	// Os nomes AppyPay legados (imutáveis no ledger) são traduzidos para o nome
	// neutro; assim o replay de eventos antigos e a leitura de eventos novos
	// passam pelo mesmo caminho.
	switch aggregates.EventoFinanceiroCanonico(e.EventType) {
	case "MatriculaConfigurada":
		var in struct {
			CodigoAcademia   string   `json:"codigo_academia"`
			Nivel            string   `json:"nivel"`
			AnoAcademico     string   `json:"ano_academico"`
			CursoID          *string  `json:"curso_id"`
			Valor            float64  `json:"valor"`
			MetodosPagamento []string `json:"metodos_pagamento"`
		}
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return err
		}
		if in.CodigoAcademia == "" || in.Nivel == "" || in.AnoAcademico == "" || in.Valor <= 0 || len(in.MetodosPagamento) == 0 {
			return fmt.Errorf("evento MatriculaConfigurada inválido")
		}
		_, err := p.client.DB().Exec(`INSERT INTO financeiro_matricula_configuracoes (event_id,aggregate_id,codigo_academia,nivel,ano_academico,curso_id,valor,metodos_pagamento,vigente_em) VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,$7,$8,$9) ON CONFLICT (event_id) DO NOTHING`, e.EventID, e.AggregateID, in.CodigoAcademia, in.Nivel, in.AnoAcademico, stringValue(in.CursoID), in.Valor, pq.Array(in.MetodosPagamento), e.OccurredAt)
		return err
	case "MatriculaConfiguracaoRemovida":
		var in struct {
			CodigoAcademia string  `json:"codigo_academia"`
			Nivel          string  `json:"nivel"`
			AnoAcademico   string  `json:"ano_academico"`
			CursoID        *string `json:"curso_id"`
		}
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return err
		}
		if in.CodigoAcademia == "" || in.Nivel == "" || in.AnoAcademico == "" {
			return fmt.Errorf("evento MatriculaConfiguracaoRemovida inválido")
		}
		_, err := p.client.DB().Exec(`INSERT INTO financeiro_matricula_configuracoes_remocoes (event_id,aggregate_id,codigo_academia,nivel,ano_academico,curso_id,removido_em) VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,$7) ON CONFLICT (event_id) DO NOTHING`, e.EventID, e.AggregateID, in.CodigoAcademia, in.Nivel, in.AnoAcademico, stringValue(in.CursoID), e.OccurredAt)
		return err
	case "MensalidadeConfigurada":
		var in struct {
			CodigoAcademia   string   `json:"codigo_academia"`
			Nivel            string   `json:"nivel"`
			AnoAcademico     string   `json:"ano_academico"`
			CursoID          *string  `json:"curso_id"`
			Valor            float64  `json:"valor"`
			MesFimCobranca   int      `json:"mes_fim_cobranca"`
			MetodosPagamento []string `json:"metodos_pagamento"`
			ModoVigencia     string   `json:"modo_vigencia"`
		}
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return err
		}
		if in.CodigoAcademia == "" || in.Nivel == "" || in.AnoAcademico == "" || in.Valor <= 0 {
			return fmt.Errorf("evento MensalidadeConfigurada inválido")
		}
		modoVigencia := in.ModoVigencia
		if modoVigencia == "" {
			modoVigencia = "a_partir_da_atualizacao"
		}
		_, err := p.client.DB().Exec(`INSERT INTO financeiro_mensalidade_configuracoes (event_id,aggregate_id,codigo_academia,nivel,ano_academico,curso_id,valor,mes_fim_cobranca,metodos_pagamento,vigente_em,sequencia,modo_vigencia) VALUES ($1,$2,$3,$4,$5,NULLIF($6,'' )::uuid,$7,$8,$9,$10,$11,$12) ON CONFLICT (event_id) DO NOTHING`, e.EventID, e.AggregateID, in.CodigoAcademia, in.Nivel, in.AnoAcademico, stringValue(in.CursoID), in.Valor, in.MesFimCobranca, pq.Array(in.MetodosPagamento), e.OccurredAt, e.ID, modoVigencia)
		return err
	case "MensalidadeConfiguracaoRemovida":
		var in struct {
			CodigoAcademia string  `json:"codigo_academia"`
			Nivel          string  `json:"nivel"`
			AnoAcademico   string  `json:"ano_academico"`
			CursoID        *string `json:"curso_id"`
		}
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return err
		}
		if in.CodigoAcademia == "" || in.Nivel == "" || in.AnoAcademico == "" {
			return fmt.Errorf("evento MensalidadeConfiguracaoRemovida inválido")
		}
		_, err := p.client.DB().Exec(`INSERT INTO financeiro_mensalidade_configuracoes_remocoes (event_id,aggregate_id,codigo_academia,nivel,ano_academico,curso_id,removido_em) VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,$7) ON CONFLICT (event_id) DO NOTHING`, e.EventID, e.AggregateID, in.CodigoAcademia, in.Nivel, in.AnoAcademico, stringValue(in.CursoID), e.OccurredAt)
		return err
	case "MesInicioCobrancaDefinido":
		var in struct {
			CodigoAcademia string `json:"codigo_academia"`
			AnoLetivo      string `json:"ano_letivo"`
			MesInicio      int    `json:"mes_inicio"`
		}
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return err
		}
		if in.CodigoAcademia == "" || in.AnoLetivo == "" || in.MesInicio < 1 || in.MesInicio > 12 {
			return fmt.Errorf("evento MesInicioCobrancaDefinido invÃ¡lido")
		}
		_, err := p.client.DB().Exec(`INSERT INTO financeiro_mensalidade_inicio_cobranca (event_id,aggregate_id,codigo_academia,ano_letivo,mes_inicio,definido_em) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (event_id) DO NOTHING`, e.EventID, e.AggregateID, in.CodigoAcademia, in.AnoLetivo, in.MesInicio, e.OccurredAt)
		return err
	case "MesInicioCobrancaRemovido":
		var in struct {
			CodigoAcademia string `json:"codigo_academia"`
			AnoLetivo      string `json:"ano_letivo"`
		}
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return err
		}
		if in.CodigoAcademia == "" || in.AnoLetivo == "" {
			return fmt.Errorf("evento MesInicioCobrancaRemovido inválido")
		}
		_, err := p.client.DB().Exec(`INSERT INTO financeiro_mensalidade_inicio_cobranca_remocoes (event_id,aggregate_id,codigo_academia,ano_letivo,removido_em) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (event_id) DO NOTHING`, e.EventID, e.AggregateID, in.CodigoAcademia, in.AnoLetivo, e.OccurredAt)
		return err
	case "ObrigacaoMensalidadeAnulada", "ObrigacaoMensalidadeReativada", "MensalidadePaga":
		var in struct {
			CodigoEstudante string `json:"codigo_estudante"`
			CodigoAcademia  string `json:"codigo_academia"`
			AnoLetivo       string `json:"ano_letivo"`
			Mes             int    `json:"mes"`
			Motivo          string `json:"motivo"`
		}
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return err
		}
		if in.CodigoEstudante == "" || in.CodigoAcademia == "" || in.AnoLetivo == "" || in.Mes < 1 || in.Mes > 12 {
			return fmt.Errorf("evento de obrigaÃ§Ã£o mensal invÃ¡lido")
		}
		tipo := map[string]string{"ObrigacaoMensalidadeAnulada": "anulada", "ObrigacaoMensalidadeReativada": "reativada", "MensalidadePaga": "paga"}[e.EventType]
		_, err := p.client.DB().Exec(`INSERT INTO financeiro_mensalidade_obrigacoes_eventos (event_id,aggregate_id,codigo_estudante,codigo_academia,ano_letivo,mes,tipo,motivo,ocorrido_em) VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9) ON CONFLICT (event_id, codigo_estudante, codigo_academia, ano_letivo, mes) DO NOTHING`, e.EventID, e.AggregateID, in.CodigoEstudante, in.CodigoAcademia, in.AnoLetivo, in.Mes, tipo, in.Motivo, e.OccurredAt)
		return err
	case "ObrigacaoServicoExtraAnulada", "ObrigacaoServicoExtraReativada", "ServicoExtraLancamentoPago":
		var in struct {
			SolicitacaoID  string `json:"solicitacao_id"`
			TipoLancamento string `json:"tipo_lancamento"`
			Ano            *int   `json:"ano"`
			Mes            *int   `json:"mes"`
		}
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return err
		}
		if in.SolicitacaoID == "" || (in.TipoLancamento != "mensalidade" && in.TipoLancamento != "preco_unico") {
			return fmt.Errorf("evento de obrigação de serviço extra inválido")
		}
		tipo := map[string]string{"ObrigacaoServicoExtraAnulada": "anulada", "ObrigacaoServicoExtraReativada": "reativada", "ServicoExtraLancamentoPago": "paga"}[e.EventType]
		_, err := p.client.DB().Exec(`INSERT INTO financeiro_servico_extra_obrigacoes_eventos (event_id,solicitacao_id,tipo_lancamento,ano,mes,tipo,ocorrido_em) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (event_id) DO NOTHING`, e.EventID, in.SolicitacaoID, in.TipoLancamento, in.Ano, in.Mes, tipo, e.OccurredAt)
		return err
	case "MensalidadesCobrancaConfirmada":
		var in struct {
			ChargeID        string `json:"charge_id"`
			CodigoEstudante string `json:"codigo_estudante"`
			CodigoAcademia  string `json:"codigo_academia"`
			Meses           []struct {
				AnoLetivo string `json:"ano_letivo"`
				Mes       int    `json:"mes"`
			} `json:"meses"`
		}
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return err
		}
		chargeID, err := uuid.Parse(in.ChargeID)
		if err != nil || in.CodigoEstudante == "" || in.CodigoAcademia == "" || len(in.Meses) == 0 {
			return fmt.Errorf("evento de confirmação de mensalidade inválido")
		}
		tx, err := p.client.BeginTx(context.Background())
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, mes := range in.Meses {
			if mes.AnoLetivo == "" || mes.Mes < 1 || mes.Mes > 12 {
				return fmt.Errorf("mês de confirmação inválido")
			}
			if _, err = tx.Exec(`INSERT INTO financeiro_mensalidade_obrigacoes_eventos (event_id,aggregate_id,codigo_estudante,codigo_academia,ano_letivo,mes,tipo,charge_id,ocorrido_em) VALUES ($1,$2,$3,$4,$5,$6,'paga',$7,$8) ON CONFLICT (charge_id, codigo_estudante, codigo_academia, ano_letivo, mes) WHERE charge_id IS NOT NULL DO NOTHING`, e.EventID, e.AggregateID, in.CodigoEstudante, in.CodigoAcademia, mes.AnoLetivo, mes.Mes, chargeID, e.OccurredAt); err != nil {
				return err
			}
		}
		return tx.Commit()
	case aggregates.CredenciaisProvedorConfiguradas:
		contexto, _ := v["contexto_tipo"].(string)
		academia, _ := v["codigo_academia"].(string)
		ambiente, _ := v["ambiente"].(string)
		provedor := provedorDoPayload(v)
		if _, err := p.client.DB().Exec(`INSERT INTO financeiro_credenciais_provedor (id,contexto_tipo,codigo_academia,ambiente,provedor,payload,updated_at) VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,CURRENT_TIMESTAMP) ON CONFLICT (id) DO UPDATE SET contexto_tipo=EXCLUDED.contexto_tipo,codigo_academia=EXCLUDED.codigo_academia,ambiente=EXCLUDED.ambiente,provedor=EXCLUDED.provedor,payload=EXCLUDED.payload,updated_at=CURRENT_TIMESTAMP`, e.AggregateID, contexto, academia, ambiente, provedor, e.Payload); err != nil {
			return err
		}
		// Primeiras credenciais de um contexto sem provedor definido tornam
		// esse provedor o ativo. Regra aplicada na projeção (e não gravada
		// como evento) para que o replay do ledger antigo, anterior à
		// seleção explícita de provedor, produza o mesmo estado.
		_, err := p.client.DB().Exec(`INSERT INTO financeiro_provedores_pagamento (contexto_tipo,codigo_academia,provedor,definido_em,updated_at) VALUES ($1,NULLIF($2,''),$3,$4,CURRENT_TIMESTAMP) ON CONFLICT DO NOTHING`, contexto, academia, provedor, e.OccurredAt)
		return err
	case aggregates.CredenciaisProvedorRemovidas:
		// Remove apenas a linha da PROJEÇÃO (estado de leitura atual). O
		// evento em si permanece para sempre no ledger, imutável. O cofre
		// de segredos (financeiro_segredos_provedor) NÃO é tocado aqui:
		// ele vive fora do replay do ledger e é limpo diretamente pelo
		// Service, no mesmo comando que grava este evento (ver
		// Service.RemoveCredential em finance/credenciais.go), exatamente
		// como ConfigureCredential grava segredos fora da projeção.
		var contexto, provedor string
		var academia sql.NullString
		err := p.client.DB().QueryRow(`DELETE FROM financeiro_credenciais_provedor WHERE id=$1 RETURNING contexto_tipo,codigo_academia,provedor`, e.AggregateID).Scan(&contexto, &academia, &provedor)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		// Um provedor sem credenciais não pode continuar ativo.
		_, err = p.client.DB().Exec(`DELETE FROM financeiro_provedores_pagamento WHERE contexto_tipo=$1 AND COALESCE(codigo_academia,'')=$2 AND provedor=$3`, contexto, academia.String, provedor)
		return err
	case aggregates.SegredoWebhookProvedorRotacionado:
		_, err := p.client.DB().Exec(`UPDATE financeiro_credenciais_provedor SET updated_at=CURRENT_TIMESTAMP WHERE id=$1`, e.AggregateID)
		return err
	case aggregates.ProvedorPagamentoDefinido:
		contexto, _ := v["contexto_tipo"].(string)
		academia, _ := v["codigo_academia"].(string)
		provedor, _ := v["provedor"].(string)
		definidoPor, _ := v["definido_por"].(string)
		if provedor == "" || (contexto != "spuri" && contexto != "academia") {
			return fmt.Errorf("evento de provedor de pagamento inválido")
		}
		_, err := p.client.DB().Exec(`INSERT INTO financeiro_provedores_pagamento (contexto_tipo,codigo_academia,provedor,definido_por,definido_em,updated_at) VALUES ($1,NULLIF($2,''),$3,NULLIF($4,''),$5,CURRENT_TIMESTAMP) ON CONFLICT (contexto_tipo, COALESCE(codigo_academia,'')) DO UPDATE SET provedor=EXCLUDED.provedor,definido_por=EXCLUDED.definido_por,definido_em=EXCLUDED.definido_em,updated_at=CURRENT_TIMESTAMP`, contexto, academia, provedor, definidoPor, e.OccurredAt)
		return err
	case aggregates.CobrancaSolicitada, aggregates.CobrancaCriada, aggregates.CobrancaFalhou, aggregates.CobrancaConsultada, aggregates.CobrancaCancelada, aggregates.CobrancaConflitoPosCancelamento, "CobrancaPagamentoExternoRegistrado", aggregates.QRCodeSolicitado, aggregates.QRCodeGerado, aggregates.QRCodeFalhou:
		merchant, _ := v["merchant_transaction_id"].(string)
		provider, _ := v["provider_charge_id"].(string)
		contexto, _ := v["contexto_tipo"].(string)
		academia, _ := v["codigo_academia"].(string)
		if merchant == "" {
			return fmt.Errorf("evento financeiro sem merchant_transaction_id")
		}
		// O provedor é gravado na criação e nunca muda: é ele que garante que
		// a cobrança continue sendo consultada/cancelada/confirmada pelo
		// provedor que a emitiu mesmo depois de o contexto trocar de provedor.
		_, err := p.client.DB().Exec(`INSERT INTO financeiro_cobrancas (id,provider_charge_id,merchant_transaction_id,contexto_tipo,codigo_academia,provedor,payload,updated_at) VALUES ($1,NULLIF($2,''),$3,$4,NULLIF($5,''),$6,$7,CURRENT_TIMESTAMP) ON CONFLICT (id) DO UPDATE SET provider_charge_id=COALESCE(EXCLUDED.provider_charge_id,financeiro_cobrancas.provider_charge_id),payload=EXCLUDED.payload,updated_at=CURRENT_TIMESTAMP`, e.AggregateID, provider, merchant, contexto, academia, provedorDoPayload(v), e.Payload)
		if err != nil {
			return err
		}
		return p.upsertMensalidadeCobrancas(e.AggregateID, academia, v)
	case aggregates.WebhookProvedorRecebido:
		id, _ := v["event_id"].(string)
		metodo, _ := v["metodo"].(string)
		if id == "" || metodo == "" {
			return fmt.Errorf("webhook financeiro inválido")
		}
		_, err := p.client.DB().Exec(`INSERT INTO financeiro_webhooks_recebidos (provedor,event_id,metodo,received_at) VALUES ($1,$2,$3,$4) ON CONFLICT (provedor, event_id) DO NOTHING`, provedorDoPayload(v), id, metodo, e.OccurredAt)
		return err
	default:
		return fmt.Errorf("evento financeiro desconhecido: %s", e.EventType)
	}
}

func (p *FinanceiroProjection) Rebuild() error {
	// Secrets are operational material and intentionally survive a ledger replay.
	if _, err := p.client.DB().Exec(`DELETE FROM financeiro_mensalidade_obrigacoes_eventos; DELETE FROM financeiro_mensalidade_inicio_cobranca; DELETE FROM financeiro_mensalidade_inicio_cobranca_remocoes; DELETE FROM financeiro_mensalidade_configuracoes; DELETE FROM financeiro_mensalidade_configuracoes_remocoes; DELETE FROM financeiro_mensalidade_cobrancas; DELETE FROM financeiro_matricula_configuracoes; DELETE FROM financeiro_matricula_configuracoes_remocoes; DELETE FROM financeiro_webhooks_recebidos; DELETE FROM financeiro_cobrancas; DELETE FROM financeiro_credenciais_provedor; DELETE FROM financeiro_provedores_pagamento;`); err != nil {
		return err
	}
	rows, err := p.client.DB().Query(`SELECT id,event_id,aggregate_id,aggregate_type,event_type,event_version,payload,metadata,occurred_at,recorded_at,ledger_hash,previous_hash FROM spuri_ledger WHERE aggregate_type='Financeiro' ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var e db.Event
		var previous sql.NullString
		if err := rows.Scan(&e.ID, &e.EventID, &e.AggregateID, &e.AggregateType, &e.EventType, &e.EventVersion, &e.Payload, &e.Metadata, &e.OccurredAt, &e.RecordedAt, &e.LedgerHash, &previous); err != nil {
			return err
		}
		if previous.Valid {
			e.PreviousHash = &previous.String
		}
		if err := p.Handle(e); err != nil {
			return err
		}
	}
	return rows.Err()
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// ApplyNow is used immediately after appending a ledger event. It invokes the
// same projection implementation as the asynchronous manager, never a second
// write path, while preserving a responsive API for newly configured accounts.
func (p *FinanceiroProjection) ApplyNow(id uuid.UUID) error {
	var e db.Event
	var previous sql.NullString
	err := p.client.DB().QueryRow(`SELECT id,event_id,aggregate_id,aggregate_type,event_type,event_version,payload,metadata,occurred_at,recorded_at,ledger_hash,previous_hash FROM spuri_ledger WHERE event_id=$1`, id).Scan(&e.ID, &e.EventID, &e.AggregateID, &e.AggregateType, &e.EventType, &e.EventVersion, &e.Payload, &e.Metadata, &e.OccurredAt, &e.RecordedAt, &e.LedgerHash, &previous)
	if err != nil {
		return err
	}
	if previous.Valid {
		e.PreviousHash = &previous.String
	}
	return p.Handle(e)
}

func (p *FinanceiroProjection) ApplyLatestForAggregate(aggregateID uuid.UUID) error {
	var eventID uuid.UUID
	if err := p.client.DB().QueryRow(`SELECT event_id FROM spuri_ledger WHERE aggregate_id=$1 AND aggregate_type='Financeiro' ORDER BY id DESC LIMIT 1`, aggregateID).Scan(&eventID); err != nil {
		return err
	}
	return p.ApplyNow(eventID)
}
