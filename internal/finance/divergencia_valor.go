package finance

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// Pagamento divergente: o provedor confirma o pagamento de um pedido cujo valor
// NÃO é o que o Spuri cobrou. Esse pagamento não quita nada — a cobrança fica
// no estado em que estava (aguardando) e marcada como divergente, para a
// academia e o FPP a verem. Só vale para provedores que informam o valor do
// pedido (providerOutcome.ValorProvedor); os restantes não são verificados.

// toleranciaValor absorve só o arredondamento de centimos (valores em Kz com 2
// casas decimais).
const toleranciaValor = 0.005

type divergenciaValor struct{ Provedor, Cobrado float64 }

// divergenciaDeValor devolve a divergência entre o valor que o provedor diz ter
// e o valor cobrado (payload["amount"]); nil se o provedor não informou valor
// ou se coincidem.
func divergenciaDeValor(payload map[string]any, valorProvedor *float64) *divergenciaValor {
	if valorProvedor == nil {
		return nil
	}
	cobrado, ok := valorDoPayload(payload["amount"])
	if !ok {
		return nil
	}
	if math.Abs(*valorProvedor-cobrado) <= toleranciaValor {
		return nil
	}
	return &divergenciaValor{Provedor: *valorProvedor, Cobrado: cobrado}
}

func marcarDivergencia(payload map[string]any, d *divergenciaValor) {
	payload["pagamento_divergente"] = true
	payload["valor_pago_provedor"] = roundAmount(d.Provedor)
	payload["valor_cobrado"] = roundAmount(d.Cobrado)
}

func limparDivergencia(payload map[string]any) {
	delete(payload, "pagamento_divergente")
	delete(payload, "valor_pago_provedor")
	delete(payload, "valor_cobrado")
}

// valorDoPayload lê um valor monetário do payload (float64 do JSON, int ou
// texto como "1500.00").
func valorDoPayload(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	}
	return 0, false
}

func ptrFloat(v float64) *float64 { return &v }

// divergenciaMudou diz se a marcação de divergência do payload novo difere da
// do anterior (para só gravar evento quando há novidade).
func divergenciaMudou(antes, depois map[string]any) bool {
	a, _ := antes["pagamento_divergente"].(bool)
	b, _ := depois["pagamento_divergente"].(bool)
	if a != b {
		return true
	}
	if !b {
		return false
	}
	va, _ := valorDoPayload(antes["valor_pago_provedor"])
	vb, _ := valorDoPayload(depois["valor_pago_provedor"])
	return math.Abs(va-vb) > toleranciaValor
}
