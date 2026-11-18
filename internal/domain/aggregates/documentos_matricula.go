package aggregates

import (
	"encoding/json"
	"fmt"
	"strings"

	"spuri/internal/utils"
)

// Status compartilhados pelas solicitações que ainda existem (edição de dados
// do estudante, alteração de NIF da academia). A solicitação pública de
// matrícula e os seus status próprios foram removidos (Plano, Parte 1).
const (
	StatusSolicitacaoPendente  = "pendente"
	StatusSolicitacaoAprovada  = "aprovada"
	StatusSolicitacaoReprovada = "reprovada"
)

// Validação de telefones, bilhetes e documentos do cadastro de estudante.
// Estas regras são usadas pelo cadastro feito pela academia
// (Estudante.CriarComVinculo, Estudante.CompletarDocumentos) e pelo
// serviço ValidateMatriculaCommon; antes viviam em solicitacao_matricula.go.

func ValidarTelefonesMatricula(telefone, telefoneEncarregado, anoEscolar, anoEscolarMedio, anoSuperior *string) error {
	escolar := !isNilOrBlank(anoEscolar) || !isNilOrBlank(anoEscolarMedio)
	superior := !isNilOrBlank(anoSuperior)
	if escolar && isNilOrBlank(telefoneEncarregado) {
		return fmt.Errorf("telefone_encarregado é obrigatório para estudante escolar")
	}
	if superior && isNilOrBlank(telefone) {
		return fmt.Errorf("telefone é obrigatório para estudante do ensino superior")
	}
	if !escolar && !superior && isNilOrBlank(telefone) && isNilOrBlank(telefoneEncarregado) {
		return fmt.Errorf("telefone ou telefone_encarregado deve ser informado")
	}
	if !isNilOrBlank(telefone) {
		if err := utils.ValidatePhone(*telefone); err != nil {
			return err
		}
	}
	if !isNilOrBlank(telefoneEncarregado) {
		if err := utils.ValidatePhone(*telefoneEncarregado); err != nil {
			return err
		}
	}
	if !isNilOrBlank(telefone) && !isNilOrBlank(telefoneEncarregado) && *telefone == *telefoneEncarregado {
		return fmt.Errorf("telefone e telefone_encarregado não podem ser iguais")
	}
	return nil
}

func isNilOrBlank(v *string) bool { return v == nil || strings.TrimSpace(*v) == "" }

type DocumentoMatricula struct {
	DocumentoID  string `json:"documento_id,omitempty"`
	Tipo         string `json:"tipo,omitempty"`
	Nivel        string `json:"nivel,omitempty"`
	AnoAcademico string `json:"ano_academico,omitempty"`
	CursoID      string `json:"curso_id,omitempty"`
	AnoLetivo    string `json:"ano_letivo,omitempty"`
	Versao       int    `json:"versao,omitempty"`
	Path         string `json:"path"`
	FileURL      string `json:"file_url"`
	DownloadURL  string `json:"download_url"`
	// DocumentoExtraID referencia o id da definição em
	// projection_documentos_extra (catálogo mantido pela academia) quando
	// este documento é um "documento extra" — vazio para os documentos
	// fixos do sistema (BI, cédula, declaração, certificados).
	DocumentoExtraID string `json:"documento_extra_id,omitempty"`
}

func (d DocumentoMatricula) TemReferenciaArquivo() bool {
	return strings.TrimSpace(d.Path) != "" || strings.TrimSpace(d.FileURL) != "" || strings.TrimSpace(d.DownloadURL) != ""
}

func (d *DocumentoMatricula) UnmarshalJSON(data []byte) error {
	var legacyPath string
	if err := json.Unmarshal(data, &legacyPath); err == nil {
		d.Path = legacyPath
		return nil
	}
	type alias DocumentoMatricula
	var v alias
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*d = DocumentoMatricula(v)
	return nil
}

func DocumentosMatriculaFaltantes(bi, biResp *string, anoFund, anoMedio, anoSuperior *string, documentos map[string]DocumentoMatricula) []string {
	if documentos == nil {
		documentos = map[string]DocumentoMatricula{}
	}
	faltantes := []string{}
	add := func(campo string) {
		for _, existente := range faltantes {
			if existente == campo {
				return
			}
		}
		faltantes = append(faltantes, campo)
	}
	if !isNilOrBlank(bi) && !hasDocumentoComArquivo(documentos, "bi_estudante") {
		add("bi_estudante")
	}
	if !isNilOrBlank(biResp) && !hasDocumentoComArquivo(documentos, "bi_encarregado") {
		add("bi_encarregado")
	}
	if isNilOrBlank(anoSuperior) && (!isNilOrBlank(anoFund) || !isNilOrBlank(anoMedio)) && isNilOrBlank(bi) {
		if doc, ok := documentos["cedula_estudante"]; !ok || !doc.TemReferenciaArquivo() {
			add("cedula_estudante")
		}
	}
	ano := ""
	if !isNilOrBlank(anoSuperior) {
		ano = *anoSuperior
	} else if !isNilOrBlank(anoFund) {
		ano = *anoFund
	} else if !isNilOrBlank(anoMedio) {
		ano = *anoMedio
	}
	if ano != "" && ano != "1_ano_fundamental" {
		esperado, ok := anoAcademicoAnterior(ano)
		if ok {
			if requiredCert, _ := certificadoObrigatorioParaAno(ano); requiredCert != "" {
				if doc, ok := documentoPorTipo(documentos, requiredCert); ok && doc.TemReferenciaArquivo() {
					return faltantes
				}
				if err := validarDeclaracaoAnoAnterior(documentos, esperado); err == nil {
					return faltantes
				}
				add(requiredCert + " ou " + TipoDeclaracaoParaAno(esperado))
			} else if err := validarDeclaracaoAnoAnterior(documentos, esperado); err != nil {
				add(TipoDeclaracaoParaAno(esperado))
			}
		}
	}
	return faltantes
}

func ValidarDocumentosMatricula(bi, biResp *string, anoFund, anoMedio, anoSuperior *string, documentos map[string]DocumentoMatricula) error {
	if documentos == nil {
		documentos = map[string]DocumentoMatricula{}
	}
	if !isNilOrBlank(anoSuperior) && isNilOrBlank(bi) {
		return fmt.Errorf("bilhete_identidade é obrigatório para estudante do ensino superior")
	}
	if !isNilOrBlank(bi) && !hasDocumentoComArquivo(documentos, "bi_estudante") {
		return fmt.Errorf("bi_estudante é obrigatório quando bilhete_identidade do estudante é informado")
	}
	if !isNilOrBlank(biResp) && !hasDocumentoComArquivo(documentos, "bi_encarregado") {
		return fmt.Errorf("bi_encarregado é obrigatório quando bilhete_identidade_encarregado é informado")
	}
	if !isNilOrBlank(anoSuperior) {
		return validarComprovativoAcademico(*anoSuperior, documentos)
	}
	if isNilOrBlank(anoFund) && isNilOrBlank(anoMedio) {
		return nil
	}
	if isNilOrBlank(biResp) {
		return fmt.Errorf("bilhete_identidade_encarregado é obrigatório para estudante escolar")
	}
	if isNilOrBlank(bi) {
		if doc, ok := documentos["cedula_estudante"]; !ok || !doc.TemReferenciaArquivo() {
			return fmt.Errorf("cedula_estudante é obrigatória quando bilhete_identidade do estudante não é informado")
		}
	}
	ano := ""
	if !isNilOrBlank(anoFund) {
		ano = *anoFund
	} else if !isNilOrBlank(anoMedio) {
		ano = *anoMedio
	}
	return validarComprovativoAcademico(ano, documentos)
}

func validarComprovativoAcademico(ano string, documentos map[string]DocumentoMatricula) error {
	ano = strings.TrimSpace(ano)
	if ano == "1_ano_fundamental" {
		return nil
	}
	esperado, ok := anoAcademicoAnterior(ano)
	if !ok {
		return nil
	}
	if requiredCert, requiredLabel := certificadoObrigatorioParaAno(ano); requiredCert != "" {
		if doc, ok := documentoPorTipo(documentos, requiredCert); ok && doc.TemReferenciaArquivo() {
			return nil
		}
		if err := validarDeclaracaoAnoAnterior(documentos, esperado); err == nil {
			return nil
		} else if hasDocumentoComArquivo(documentos, "declaracao") {
			return err
		}
		return fmt.Errorf("%s ou declaracao do ano académico anterior (%s) é obrigatório para %s", requiredLabel, esperado, ano)
	}
	return validarDeclaracaoAnoAnterior(documentos, esperado)
}

func TipoDeclaracaoParaAno(ano string) string {
	ano = strings.TrimSpace(ano)
	if ano == "" {
		return ""
	}
	return "declaracao_" + ano
}

func NivelDoAnoAcademico(ano string) string {
	switch {
	case strings.HasSuffix(ano, "_ano_fundamental"):
		return "fundamental"
	case strings.HasSuffix(ano, "_ano_medio"):
		return "medio"
	case strings.HasSuffix(ano, "_ano_superior"):
		return "superior"
	default:
		return "escopo_desconhecido"
	}
}

func documentoPorTipo(documentos map[string]DocumentoMatricula, tipo string) (DocumentoMatricula, bool) {
	if doc, ok := documentos[tipo]; ok {
		return doc, true
	}
	for campo, doc := range documentos {
		if doc.Tipo == tipo {
			return doc, true
		}
		if strings.HasPrefix(tipo, "declaracao_") && (campo == "declaracao" || doc.Tipo == "declaracao") && doc.AnoAcademico != "" && tipo == TipoDeclaracaoParaAno(doc.AnoAcademico) {
			return doc, true
		}
	}
	return DocumentoMatricula{}, false
}

func hasDocumentoComArquivo(documentos map[string]DocumentoMatricula, campo string) bool {
	doc, ok := documentoPorTipo(documentos, campo)
	return ok && doc.TemReferenciaArquivo()
}

func validarDeclaracaoAnoAnterior(documentos map[string]DocumentoMatricula, esperado string) error {
	tipo := TipoDeclaracaoParaAno(esperado)
	doc, ok := documentoPorTipo(documentos, tipo)
	if !ok || !doc.TemReferenciaArquivo() {
		return fmt.Errorf("declaracao do ano académico anterior (%s) é obrigatória", esperado)
	}
	anoDeclaracao := strings.TrimSpace(doc.AnoAcademico)
	if anoDeclaracao == "" {
		return fmt.Errorf("%s deve informar ano_academico esperado %s", tipo, esperado)
	}
	if anoDeclaracao != esperado {
		return fmt.Errorf("%s deve ser do ano académico anterior %s; recebido %s", tipo, esperado, anoDeclaracao)
	}
	return nil
}

func certificadoObrigatorioParaAno(ano string) (string, string) {
	switch ano {
	case "7_ano_fundamental":
		return "certificado_6_ano_fundamental", "certificado da " + utils.RotuloClasseFundamental("6_ano_fundamental")
	case "1_ano_medio":
		return "certificado_9_ano_fundamental", "certificado da " + utils.RotuloClasseFundamental("9_ano_fundamental")
	case "1_ano_superior":
		return "certificado_ensino_medio", "certificado do ensino médio"
	default:
		return "", ""
	}
}

func anoAcademicoAnterior(ano string) (string, bool) {
	ordem := []string{
		"1_ano_fundamental", "2_ano_fundamental", "3_ano_fundamental", "4_ano_fundamental", "5_ano_fundamental", "6_ano_fundamental", "7_ano_fundamental", "8_ano_fundamental", "9_ano_fundamental",
		"1_ano_medio", "2_ano_medio", "3_ano_medio",
		"1_ano_superior",
	}
	for i, atual := range ordem {
		if atual == ano {
			if i == 0 {
				return "", false
			}
			return ordem[i-1], true
		}
	}
	return "", false
}
