package academias

import (
	"fmt"
	"strings"
	"time"

	"spuri/internal/db"
	"spuri/internal/handlers/apoio"
)

func mesPermiteFinalizacaoAnoLetivo(mesAtual, mesFim, mesInicio int) bool {
	return mesAtual >= mesFim && mesAtual < mesInicio
}

func anoFinalAnoLetivo(anoLetivo string) (int, error) {
	partes, err := apoio.ParseAnoLetivo(anoLetivo)
	if err != nil {
		return 0, err
	}
	return partes.Fim, nil
}

func mesesPermitidosFinalizacaoAnoLetivo(mesFim, mesInicio int) []time.Month {
	meses := []time.Month{}
	for mes := mesFim; mes < mesInicio; mes++ {
		meses = append(meses, time.Month(mes))
	}
	return meses
}

func nomesMesesPortugues(meses []time.Month) string {
	nomes := map[time.Month]string{
		time.January: "janeiro", time.February: "fevereiro", time.March: "março",
		time.April: "abril", time.May: "maio", time.June: "junho",
		time.July: "julho", time.August: "agosto", time.September: "setembro",
		time.October: "outubro", time.November: "novembro", time.December: "dezembro",
	}
	partes := make([]string, 0, len(meses))
	for _, mes := range meses {
		partes = append(partes, nomes[mes])
	}
	if len(partes) <= 1 {
		return strings.Join(partes, "")
	}
	return strings.Join(partes[:len(partes)-1], ", ") + " e " + partes[len(partes)-1]
}

func validarDataAtualPermiteFinalizacaoAnoLetivo(client *db.Client, tipo, anoLetivo string, agora time.Time) error {
	anoFinal, err := anoFinalAnoLetivo(anoLetivo)
	if err != nil {
		return err
	}
	agoraUTC := agora.UTC()
	anoAtual := agoraUTC.Year()
	if anoAtual != anoFinal {
		return fmt.Errorf("não é possível finalizar o ano letivo %s: o ano atual (%d) não é o ano final do período letivo (%d)", anoLetivo, anoAtual, anoFinal)
	}

	periodo, err := apoio.PeriodoConfigurado(client, tipo)
	if err != nil {
		return err
	}
	mesInicio, mesFim, err := apoio.MesesPeriodoLetivo(periodo)
	if err != nil {
		return err
	}
	mesAtual := int(agoraUTC.Month())
	if mesPermiteFinalizacaoAnoLetivo(mesAtual, mesFim, mesInicio) {
		return nil
	}
	mesesPermitidos := mesesPermitidosFinalizacaoAnoLetivo(mesFim, mesInicio)
	return fmt.Errorf(
		"não é possível finalizar o ano letivo %s: fora da janela mensal de finalização; permitido apenas em %s de %d (periodo=%s; mês atual=%02d)",
		anoLetivo, nomesMesesPortugues(mesesPermitidos), anoFinal, periodo, mesAtual,
	)
}

func validarMesAtualPermiteFinalizacaoAnoLetivo(client *db.Client, tipo string, agora time.Time) error {
	periodo, err := apoio.PeriodoConfigurado(client, tipo)
	if err != nil {
		return err
	}
	mesInicio, mesFim, err := apoio.MesesPeriodoLetivo(periodo)
	if err != nil {
		return err
	}
	mesAtual := int(agora.UTC().Month())
	if mesPermiteFinalizacaoAnoLetivo(mesAtual, mesFim, mesInicio) {
		return nil
	}
	return fmt.Errorf(
		"ano letivo %s só pode ser finalizado entre o mês de fim do período letivo e o mês anterior ao início do próximo período (periodo=%s; mês atual=%02d; permitido: mês >= %02d e < %02d)",
		tipo, periodo, mesAtual, mesFim, mesInicio,
	)
}
