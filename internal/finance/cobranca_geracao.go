package finance

import (
	"context"
	"strings"
)

// gerarCobrancaInput agrupa os parâmetros necessários para emitir uma nova
// cobrança em nome de uma academia (contexto sempre ContextoAcademia), a
// partir de uma obrigação já validada por quem chama (mensalidade ou serviço
// extra). CodigoEstudante e Mensalidades são metadados de auditoria
// opcionais: preencha apenas o(s) que fizer(em) sentido para a origem da
// cobrança (CodigoEstudante e Mensalidades para mensalidade) e deixe os
// demais no valor zero —
// ChargeRequest e QRCodeRequest já tratam esses campos como omitempty e
// eles nunca são enviados à AppyPay (ver comentário de ChargeRequest em
// appypay.go).
type gerarCobrancaInput struct {
	CodigoAcademia             string
	MetodoPagamento            string // "REF", "GPO" ou "GPO_QR" — já normalizado (TrimSpace+ToUpper) pelo chamador
	Amount                     float64
	Description                string
	MerchantTransactionID      string
	Telefone                   string // usado apenas quando MetodoPagamento == "GPO"; ignorado nos demais casos
	CodigoEstudante            string
	CodigoInscricaoServico     string
	TipoLancamentoServicoExtra string
	MesReferencia              *int
	AnoReferencia              *int
	Mensalidades               []MensalidadeSelecaoMes
}

// gerarCobranca é a única função do módulo financeiro que decide, a partir
// de um método de pagamento aceite pelo sistema (REF, GPO ou GPO_QR), qual
// das duas funções que efetivamente falam com a AppyPay chamar —
// CreateGPOQRCode (GPO_QR) ou CreateCharge (REF e GPO) — e monta o
// paymentInfo.phoneNumber exigido pela AppyPay quando o método é GPO.
//
// É reutilizada por IniciarPagamentoMensalidades (mensalidade.go) e pelos
// pagamentos de serviço extra (servico_extra.go): as funções do sistema que
// iniciam uma cobrança nova a partir de uma obrigação (mensalidade ou serviço
// extra) e de um método de pagamento escolhido pelo pagador. Nenhum outro lugar do módulo deve reimplementar
// esta decisão — ver o comentário de pacote no topo de appypay.go
// ("Package finance is the only package allowed to call AppyPay's HTTP
// API").
//
// Endpoints administrativos que criam uma cobrança "avulsa" com controlo
// total sobre o payload (CriarCobrancaAppyPay, GerarQRCodeAppyPay em
// internal/handlers/financeiro_handlers.go) continuam, deliberadamente,
// chamando CreateCharge/CreateGPOQRCode diretamente: não partem de uma
// obrigação nem de um método simples REF/GPO/GPO_QR escolhido pelo
// pagador, então esta função não se aplica a eles.
//
// O retorno é sempre QRCodeResult (mesmo para REF/GPO, com QRCodeArr
// vazio), para que o chamador nunca precise tratar dois tipos de retorno
// diferentes — o mesmo motivo pelo qual MensalidadePagamentoView.Charge já é
// declarado como QRCodeResult.
func (s *Service) gerarCobranca(ctx context.Context, in gerarCobrancaInput, actorID, actorType, ip string) (QRCodeResult, error) {
	if in.MetodoPagamento == "GPO_QR" {
		qr, err := s.CreateGPOQRCode(ctx, QRCodeRequest{
			ContextoTipo:               ContextoAcademia,
			CodigoAcademia:             in.CodigoAcademia,
			CodigoEstudante:            in.CodigoEstudante,
			CodigoInscricaoServico:     in.CodigoInscricaoServico,
			TipoLancamentoServicoExtra: in.TipoLancamentoServicoExtra, MesReferencia: in.MesReferencia, AnoReferencia: in.AnoReferencia,
			Amount:                in.Amount,
			Currency:              "AOA",
			Description:           in.Description,
			MerchantTransactionID: in.MerchantTransactionID,
			Mensalidades:          in.Mensalidades,
		}, actorID, actorType, ip)
		if err != nil {
			return QRCodeResult{}, err
		}
		return qr, nil
	}
	info := map[string]any{}
	if in.MetodoPagamento == "GPO" {
		info["phoneNumber"] = strings.TrimSpace(in.Telefone)
	}
	charge, err := s.CreateCharge(ctx, ChargeRequest{
		ContextoTipo:               ContextoAcademia,
		CodigoAcademia:             in.CodigoAcademia,
		CodigoEstudante:            in.CodigoEstudante,
		CodigoInscricaoServico:     in.CodigoInscricaoServico,
		TipoLancamentoServicoExtra: in.TipoLancamentoServicoExtra, MesReferencia: in.MesReferencia, AnoReferencia: in.AnoReferencia,
		Mensalidades:          in.Mensalidades,
		Amount:                in.Amount,
		Currency:              "AOA",
		Description:           in.Description,
		MerchantTransactionID: in.MerchantTransactionID,
		PaymentMethod:         in.MetodoPagamento,
		PaymentInfo:           info,
	}, actorID, actorType, ip)
	if err != nil {
		return QRCodeResult{}, err
	}
	return QRCodeResult{ChargeResult: charge}, nil
}
