package apoio

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/middleware"
	"spuri/internal/utils"
)

func ResolverAnoLetivoAcademia(anoLetivo *string, codigoAcademia string) (string, error) {
	if anoLetivo == nil || strings.TrimSpace(*anoLetivo) == "" {
		return "", fmt.Errorf(
			"a academia '%s' não possui um ano letivo ativo. "+
				"Configure via POST /academia/ano-letivo antes de registrar",
			codigoAcademia,
		)
	}
	return *anoLetivo, nil
}

func CalcularLimiteFinalizacao(client *db.Client, tipo, anoFiltro string) (string, string, int, int, error) {
	var total int
	nivel := "escola"
	if tipo == "superior" {
		nivel = "superior"
	}
	if err := client.DB().QueryRow(`SELECT COUNT(*) FROM projection_academias WHERE status='ativo' AND nivel=$1`, nivel).Scan(&total); err != nil {
		return "", "", 0, 0, err
	}
	if total == 0 {
		return "", "", 0, 0, nil
	}
	rows, err := client.DB().Query(`SELECT ano_letivo, COUNT(DISTINCT academia_id) FROM projection_anos_letivos_academia_finalizacoes WHERE type=$1 AND finalizado=TRUE GROUP BY ano_letivo`, tipo)
	if err != nil {
		return "", "", 0, 0, err
	}
	defer rows.Close()
	marco := ""
	fin := 0
	for rows.Next() {
		var ano string
		var c int
		if err := rows.Scan(&ano, &c); err != nil {
			return "", "", 0, 0, err
		}
		if c >= total {
			if marco == "" {
				marco = ano
				fin = c
			} else if cmp, _ := CompareAnoLetivo(ano, marco); cmp > 0 {
				marco = ano
				fin = c
			}
		}
	}
	minimo := ""
	if marco != "" {
		minimo, _ = ProximoAnoLetivoValidado(marco)
	}
	return marco, minimo, total, fin, nil
}

type estudanteBusinessContext struct {
	AcademiaID     uuid.UUID
	CodigoAcademia string
	Estudante      *aggregates.Estudante
}

func CarregarEstudanteDaAcademia(c *gin.Context) (*estudanteBusinessContext, bool) {
	codigoEstudante := c.Param("codigo")
	academiaID, _ := middleware.GetUserID(c)
	academia, err := GetAcademiaProjection(c).GetByID(academiaID)
	if err != nil || academia == nil {
		utils.RespondWithNotFoundError(c, "academia")
		return nil, false
	}
	estudanteDTO, err := GetEstudanteProjection(c).GetByCodigo(codigoEstudante)
	if err != nil || estudanteDTO == nil {
		utils.RespondWithNotFoundError(c, "estudante")
		return nil, false
	}
	if estudanteDTO.CodigoAcademia == nil || *estudanteDTO.CodigoAcademia != academia.CodigoAcademia {
		utils.RespondWithForbiddenError(c, "estudante não pertence a esta academia")
		return nil, false
	}
	estudanteAgg, err := GetRepository(c).Load(estudanteDTO.ID, "Estudante")
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return nil, false
	}
	estudante, ok := estudanteAgg.(*aggregates.Estudante)
	if !ok {
		utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
		return nil, false
	}
	return &estudanteBusinessContext{academiaID, academia.CodigoAcademia, estudante}, true
}

func SalvarEventoEstudante(c *gin.Context, ctx *estudanteBusinessContext) bool {
	audit := db.AuditContext{UserID: ctx.AcademiaID.String(), UserType: "academia", IP: c.ClientIP()}
	if err := GetRepository(c).SaveWithAudit(ctx.Estudante, audit); err != nil {
		utils.RespondWithInternalError(c, err)
		return false
	}
	return true
}

type anoLetivoPartes struct{ Inicio, Fim int }

const (
	PeriodoLetivoEscolar  = "09_07"
	PeriodoLetivoSuperior = "10_07"
)

func PeriodoFixoPorTipoAnoLetivo(tipo string) (string, error) {
	t, err := NormalizarTipoAnoLetivo(tipo)
	if err != nil {
		return "", err
	}
	switch t {
	case "escolar":
		return PeriodoLetivoEscolar, nil
	case "superior":
		return PeriodoLetivoSuperior, nil
	default:
		return "", fmt.Errorf("type deve ser 'escolar' ou 'superior'")
	}
}

func NormalizarTipoAnoLetivo(tipo string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(tipo))
	if t != "escolar" && t != "superior" {
		return "", fmt.Errorf("type deve ser 'escolar' ou 'superior'")
	}
	return t, nil
}

func ParseAnoLetivo(v string) (anoLetivoPartes, error) {
	v = strings.TrimSpace(v)
	var p anoLetivoPartes
	if len(v) != 9 || v[4] != '_' {
		return p, fmt.Errorf("ano_letivo deve usar formato YYYY_YYYY")
	}
	if _, err := fmt.Sscanf(v, "%4d_%4d", &p.Inicio, &p.Fim); err != nil || p.Fim != p.Inicio+1 {
		return p, fmt.Errorf("ano_letivo deve ter segundo ano igual ao primeiro + 1")
	}
	return p, nil
}

func ProximoAnoLetivoValidado(v string) (string, error) {
	p, err := ParseAnoLetivo(v)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%04d_%04d", p.Fim, p.Fim+1), nil
}

func CompareAnoLetivo(a, b string) (int, error) {
	pa, err := ParseAnoLetivo(a)
	if err != nil {
		return 0, err
	}
	pb, err := ParseAnoLetivo(b)
	if err != nil {
		return 0, err
	}
	if pa.Inicio < pb.Inicio {
		return -1, nil
	}
	if pa.Inicio > pb.Inicio {
		return 1, nil
	}
	return 0, nil
}

func ValidarPeriodoLetivoFixoPayload(tipo, periodo string) (string, error) {
	periodoRecebido, err := NormalizarPeriodoLetivo(periodo)
	if err != nil {
		return "", err
	}
	periodoFixo, err := PeriodoFixoPorTipoAnoLetivo(tipo)
	if err != nil {
		return "", err
	}
	if periodoRecebido != periodoFixo {
		return "", fmt.Errorf("periodo de ano letivo é fixo e imutável para type=%s: use %s", tipo, periodoFixo)
	}
	return periodoFixo, nil
}

func NormalizarPeriodoLetivo(periodo string) (string, error) {
	periodo = strings.TrimSpace(periodo)
	partes := strings.Split(periodo, "_")
	if len(partes) != 2 || strings.TrimSpace(partes[0]) == "" || strings.TrimSpace(partes[1]) == "" {
		return "", fmt.Errorf("periodo deve usar formato MM_MM com meses entre 01 e 12")
	}
	ini, errIni := strconv.Atoi(partes[0])
	fim, errFim := strconv.Atoi(partes[1])
	if errIni != nil || errFim != nil || ini < 1 || ini > 12 || fim < 1 || fim > 12 {
		return "", fmt.Errorf("periodo deve usar formato MM_MM com meses entre 01 e 12")
	}
	return fmt.Sprintf("%02d_%02d", ini, fim), nil
}

func MesesPeriodoLetivo(periodo string) (int, int, error) {
	var mi, mf int
	if _, err := fmt.Sscanf(periodo, "%d_%d", &mi, &mf); err != nil || mi < 1 || mi > 12 || mf < 1 || mf > 12 {
		return 0, 0, fmt.Errorf("periodo inválido")
	}
	return mi, mf, nil
}

func IntervaloAnoLetivo(anoLetivo, periodo string) (time.Time, time.Time, error) {
	a, err := ParseAnoLetivo(anoLetivo)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	mi, mf, err := MesesPeriodoLetivo(periodo)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	inicio := time.Date(a.Inicio, time.Month(mi), 1, 0, 0, 0, 0, time.UTC)
	fim := time.Date(a.Fim, time.Month(mf)+1, 0, 23, 59, 59, 0, time.UTC)
	return inicio, fim, nil
}

func PeriodoConfigurado(_ *db.Client, tipo string) (string, error) {
	return PeriodoFixoPorTipoAnoLetivo(tipo)
}

func ValidarDataNoPeriodoLetivo(client *db.Client, tipo, anoLetivo string, data time.Time) error {
	periodo, err := PeriodoConfigurado(client, tipo)
	if err != nil {
		return err
	}
	ini, fim, err := IntervaloAnoLetivo(anoLetivo, periodo)
	if err != nil {
		return err
	}
	d := data.UTC()
	if d.Before(ini) || d.After(fim) {
		return fmt.Errorf("data da falta fora do período letivo %s %s: permitido de %s até %s", tipo, anoLetivo, ini.Format("2006-01-02"), fim.Format("2006-01-02"))
	}
	return nil
}

func InferirTipoLetivoMateria(materiaType string) (string, error) {
	if strings.TrimSpace(strings.ToLower(materiaType)) == "superior" {
		return "superior", nil
	}
	return "escolar", nil
}

func ValidarSequenciaAnosMedio(anos []string) error {
	if err := utils.ValidateAnosCurso("medio", anos); err != nil {
		return err
	}
	for i, ano := range anos {
		numero, sufixo, ok := strings.Cut(ano, "_")
		if !ok || sufixo != "ano_medio" {
			return fmt.Errorf("ano '%s' inválido para curso médio", ano)
		}
		n, err := strconv.Atoi(numero)
		if err != nil || n <= 0 {
			return fmt.Errorf("ano '%s' inválido para curso médio", ano)
		}
		esperado := i + 1
		if n != esperado {
			return fmt.Errorf("anos do ensino médio devem estar em ordem sequencial crescente começando em 1_ano_medio; esperado %d_ano_medio na posição %d", esperado, i+1)
		}
	}
	return nil
}
