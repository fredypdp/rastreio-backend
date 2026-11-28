package apoio

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"spuri/internal/cadastros"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/finance"
	"spuri/internal/middleware"
	"spuri/internal/projections"
	"spuri/internal/utils"
)

func Academy(c *gin.Context) (string, uuid.UUID, bool) {
	id, _ := middleware.GetUserID(c)
	a, e := GetAcademiaProjection(c).GetByID(id)
	if e != nil || a == nil {
		utils.RespondWithNotFoundError(c, "academia")
		return "", id, false
	}
	return a.CodigoAcademia, id, true
}

// validarPosseCursosDisponiveis confirma que cada curso pertence à academia,
// não foi deletado, possui o tipo esperado e oferece o ano informado.
func ValidarPosseCursosDisponiveis(c *gin.Context, codigoAcademia string, cursosDisponiveis []string) error {
	cursosProj := GetCursosProjection(c)
	cache := map[uuid.UUID]*projections.CursoDTO{}
	for _, item := range cursosDisponiveis {
		partes := strings.SplitN(item, "|", 2)
		if len(partes) != 2 {
			continue
		}
		cursoID, err := uuid.Parse(partes[0])
		if err != nil {
			continue
		}
		curso, ok := cache[cursoID]
		if !ok {
			curso, err = cursosProj.GetByID(cursoID)
			if err != nil {
				return fmt.Errorf("erro ao verificar curso %s: %v", cursoID, err)
			}
			cache[cursoID] = curso
		}
		if curso == nil {
			return fmt.Errorf("curso %s não encontrado", cursoID)
		}
		if curso.CodigoAcademia != codigoAcademia {
			return fmt.Errorf("curso %s não pertence a esta academia", cursoID)
		}
		if curso.Status == "deletado" {
			return fmt.Errorf("curso %s foi removido e não pode ser usado em serviços extras", cursoID)
		}
		ano := partes[1]
		tipoEsperado := "medio"
		if strings.HasSuffix(ano, "_ano_superior") {
			tipoEsperado = "superior"
		}
		if curso.Type != tipoEsperado {
			return fmt.Errorf("ano %q não corresponde ao tipo do curso %s (%s)", ano, cursoID, curso.Type)
		}
		anoValido := false
		for _, a := range curso.AnosAcademicos {
			if a == ano {
				anoValido = true
				break
			}
		}
		if !anoValido {
			return fmt.Errorf("ano %q não faz parte dos anos acadêmicos do curso %s", ano, cursoID)
		}
	}
	return nil
}

// estudanteElegivelServicoExtra cruza as restrições do serviço com o ano e
// curso atuais do estudante. anos_academicos_disponiveis só cobre fundamental
// (sem curso); médio e superior são verificados via cursos_disponiveis.
func estudanteElegivelServicoExtra(d cadastros.Disponibilidade, est *projections.EstudanteDTO) bool {
	contains := func(list []string, v string) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	}
	if est.AnoEscolar != nil && contains(d.AnosAcademicos, *est.AnoEscolar) {
		return true
	}
	if est.CursoMedioID != nil && est.AnoEscolarMedio != nil && contains(d.Cursos, *est.CursoMedioID+"|"+*est.AnoEscolarMedio) {
		return true
	}
	if est.CursoSuperiorID != nil && est.AnoSuperior != nil && contains(d.Cursos, *est.CursoSuperiorID+"|"+*est.AnoSuperior) {
		return true
	}
	return false
}

// ElegivelParaServicoExtra: sem nenhuma restrição (nem anos, nem cursos) o
// serviço está disponível para todos os estudantes da academia; havendo
// restrição, só estudanteElegivelServicoExtra decide. É a MESMA regra do
// catálogo do estudante e da aceitação da inscrição.
func ElegivelParaServicoExtra(d cadastros.Disponibilidade, est *projections.EstudanteDTO) bool {
	if len(d.AnosAcademicos) == 0 && len(d.Cursos) == 0 {
		return true
	}
	return estudanteElegivelServicoExtra(d, est)
}

func GetServicosRepo(c *gin.Context) *cadastros.ServicosRepo {
	return cadastros.NovoServicosRepo(GetDbClient(c))
}

// ============================================================================
// Serialização do catálogo
// ============================================================================

func mesesJSON(m []int) []int {
	if m == nil {
		return []int{}
	}
	return m
}

func horariosJSON(hs []cadastros.Horario) []gin.H {
	out := make([]gin.H, 0, len(hs))
	for _, h := range hs {
		out = append(out, gin.H{"dia_semana": h.DiaSemana, "hora_inicio": h.HoraInicio, "hora_fim": h.HoraFim})
	}
	return out
}

// TransporteJSON é a visão da ACADEMIA: toda a configuração, com o que falta
// configurar em cada rota (problemas).
func TransporteJSON(t *cadastros.Transporte, persistido bool) gin.H {
	rotas := make([]gin.H, 0, len(t.Rotas))
	for i := range t.Rotas {
		r := &t.Rotas[i]
		veiculos := make([]gin.H, 0, len(r.Veiculos))
		for _, v := range r.Veiculos {
			veiculos = append(veiculos, gin.H{"id": v.ID, "nome": v.Nome, "ativo": v.Ativo})
		}
		res := t.Resolver(r)
		rotas = append(rotas, gin.H{
			"id": r.ID, "nome": r.Nome, "horario_embarque": r.HorarioEmbarque, "ativo": r.Ativo,
			"preco_modo": r.PrecoModo, "preco": r.PrecoValor, "prazo_meses": mesesJSON(r.PrazoMeses),
			"veiculos": veiculos, "preco_efetivo": res.Preco, "gratuito_efetivo": res.Gratuito,
			"meses_efetivos": mesesJSON(res.Meses), "problemas": res.Problemas, "configurado": res.Configurado(),
		})
	}
	return gin.H{
		"id": t.ID, "codigo_academia": t.CodigoAcademia, "ativo": t.Ativo, "persistido": persistido,
		"preco_geral_modo": t.PrecoModo, "preco_geral": t.PrecoValor,
		"prazo_geral_modo": t.PrazoModo, "prazo_geral_meses": mesesJSON(t.PrazoMeses),
		"tem_taxa_inscricao": t.Taxa.Valor > 0, "valor_taxa_inscricao": t.Taxa.Valor,
		"metodos_pagamento": t.Metodos, "metodos_pagamento_taxa_inscricao": t.Taxa.Metodos,
		"anos_academicos_disponiveis": t.AnosAcademicos, "cursos_disponiveis": t.Cursos,
		"rotas": rotas,
	}
}

// AtividadeJSON é a visão da ACADEMIA de uma atividade.
func AtividadeJSON(a *cadastros.Atividade) gin.H {
	turmas := make([]gin.H, 0, len(a.Turmas))
	for _, t := range a.Turmas {
		turmas = append(turmas, gin.H{"id": t.ID, "nome": t.Nome, "ativo": t.Ativo, "membros": t.Membros})
	}
	res := a.Resolver()
	return gin.H{
		"id": a.ID, "codigo_academia": a.CodigoAcademia, "nome": a.Nome, "instrutor": a.Instrutor,
		"coletiva": a.Coletiva, "gratuito": a.Gratuito, "preco": a.Preco, "prazo_meses": mesesJSON(a.PrazoMeses),
		"tem_taxa_inscricao": a.Taxa.Valor > 0, "valor_taxa_inscricao": a.Taxa.Valor,
		"metodos_pagamento": a.Metodos, "metodos_pagamento_taxa_inscricao": a.Taxa.Metodos,
		"anos_academicos_disponiveis": a.AnosAcademicos, "cursos_disponiveis": a.Cursos,
		"ativo": a.Ativo, "horarios": horariosJSON(a.Horarios), "turmas": turmas,
		"problemas": res.Problemas, "configurado": res.Configurado(),
	}
}

// metodosParaPagador filtra os métodos pelo provedor ativo da academia e
// devolve os avisos quando nenhum sobra (Tarefa 121).
func metodosParaPagador(disp finance.PagamentoDisponivel, res cadastros.ServicoResolvido, h gin.H) {
	if !res.Gratuito {
		m := disp.Filtrar(res.Metodos)
		h["metodos_pagamento"] = m
		if av := disp.Aviso(res.Metodos, m); av != nil {
			h["metodos_pagamento_aviso"] = av
		}
	} else {
		h["metodos_pagamento"] = []string{}
	}
	if res.Taxa.Valor > 0 {
		mt := disp.Filtrar(res.Taxa.Metodos)
		h["metodos_pagamento_taxa_inscricao"] = mt
		if av := disp.Aviso(res.Taxa.Metodos, mt); av != nil {
			h["metodos_pagamento_taxa_inscricao_aviso"] = av
		}
	} else {
		h["metodos_pagamento_taxa_inscricao"] = []string{}
	}
}

// CatalogoParaPagador monta o que o pagador (estudante) vê: só o que está
// ativo e configurado, e (se est != nil) só o que o estudante pode pedir.
func CatalogoParaPagador(c *gin.Context, codigo string, est *projections.EstudanteDTO) (gin.H, int, error) {
	repo := GetServicosRepo(c)
	ctx := c.Request.Context()
	disp := FinanceiroService.PagamentoDisponivel(ctx, codigo)
	var transporte any
	total := 0

	t, persistido, err := repo.ObterTransporte(ctx, codigo)
	if err != nil {
		return nil, 0, err
	}
	if persistido && t.Ativo && (est == nil || ElegivelParaServicoExtra(t.Disponibilidade, est)) {
		rotas := make([]gin.H, 0, len(t.Rotas))
		var base cadastros.ServicoResolvido
		for i := range t.Rotas {
			r := &t.Rotas[i]
			res := t.Resolver(r)
			if !res.Configurado() {
				continue
			}
			base = res
			veiculos := make([]gin.H, 0, len(r.Veiculos))
			for _, v := range r.Veiculos {
				if v.Ativo {
					veiculos = append(veiculos, gin.H{"id": v.ID, "nome": v.Nome})
				}
			}
			rotas = append(rotas, gin.H{
				"id": r.ID, "nome": r.Nome, "horario_embarque": r.HorarioEmbarque, "gratuito": res.Gratuito,
				"preco": res.Preco, "meses": mesesJSON(res.Meses), "veiculos": veiculos,
			})
		}
		if len(rotas) > 0 {
			h := gin.H{
				"id": t.ID, "tipo": cadastros.TipoServicoTransporte, "nome": "Transporte", "rotas": rotas,
				"tem_taxa_inscricao": t.Taxa.Valor > 0, "valor_taxa_inscricao": t.Taxa.Valor,
			}
			metodosParaPagador(disp, base, h)
			transporte = h
			total++
		}
	}

	atividades, err := repo.ListarAtividades(ctx, codigo, true)
	if err != nil {
		return nil, 0, err
	}
	lista := make([]gin.H, 0, len(atividades))
	for _, a := range atividades {
		res := a.Resolver()
		if !res.Configurado() || (est != nil && !ElegivelParaServicoExtra(a.Disponibilidade, est)) {
			continue
		}
		h := gin.H{
			"id": a.ID, "tipo": cadastros.TipoServicoAtividade, "nome": a.Nome, "instrutor": a.Instrutor, "coletiva": a.Coletiva,
			"gratuito": a.Gratuito, "preco": a.Preco, "meses": mesesJSON(a.PrazoMeses), "horarios": horariosJSON(a.Horarios),
			"tem_taxa_inscricao": a.Taxa.Valor > 0, "valor_taxa_inscricao": a.Taxa.Valor,
		}
		metodosParaPagador(disp, res, h)
		lista = append(lista, h)
		total++
	}
	return gin.H{"transporte": transporte, "atividades": lista, "total": total}, total, nil
}

// ListarServicosExtrasAcademia: catálogo e configuração, como a academia os vê.
func ListarServicosExtrasAcademia(c *gin.Context) {
	codigo, _, ok := Academy(c)
	if !ok {
		return
	}
	repo := GetServicosRepo(c)
	t, persistido, err := repo.ObterTransporte(c.Request.Context(), codigo)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	atividades, err := repo.ListarAtividades(c.Request.Context(), codigo, false)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	lista := make([]gin.H, 0, len(atividades))
	for _, a := range atividades {
		lista = append(lista, AtividadeJSON(a))
	}
	c.JSON(http.StatusOK, gin.H{"transporte": TransporteJSON(t, persistido), "atividades": lista, "total_atividades": len(lista)})
}

// ListarServicosExtrasPublico: catálogo público da academia (sem estudante, sem
// filtro de elegibilidade), só o que está ativo e configurado.
func ListarServicosExtrasPublico(c *gin.Context) {
	out, _, err := CatalogoParaPagador(c, c.Param("codigo_academia"), nil)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// ListarServicosExtrasCatalogoEstudante: o catálogo do estudante autenticado,
// já filtrado pela elegibilidade (ano/curso atual).
func ListarServicosExtrasCatalogoEstudante(c *gin.Context) {
	uid, _ := middleware.GetUserID(c)
	est, e := GetEstudanteProjection(c).GetByID(uid)
	if e != nil || est == nil {
		utils.RespondWithForbiddenError(c, "estudante não encontrado")
		return
	}
	// SEC-15 / D10: estudante desvinculado (inativo) ou deletado não vê catálogo
	if !estudanteVinculado(est.Status) {
		c.JSON(http.StatusOK, gin.H{"transporte": nil, "atividades": []any{}, "total": 0})
		return
	}
	if est.CodigoAcademia == nil {
		utils.RespondWithForbiddenError(c, "estudante não está vinculado a uma academia")
		return
	}
	out, _, err := CatalogoParaPagador(c, *est.CodigoAcademia, est)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// ============================================================================
// Solicitação
// ============================================================================

// SolicitacaoServicoExtraToJSON serializa o aggregate com chaves snake_case.
func SolicitacaoServicoExtraToJSON(s *aggregates.SolicitacaoServicoExtra) gin.H {
	var vinculadaEm interface{}
	if !s.VinculadaEm.IsZero() {
		vinculadaEm = s.VinculadaEm
	}
	veiculos := s.VeiculoIDs
	if veiculos == nil {
		veiculos = []uuid.UUID{}
	}
	return gin.H{
		"id":                               s.GetID(),
		"servico_extra_id":                 s.ServicoExtraID,
		"tipo_servico":                     s.TipoServico,
		"rota_id":                          s.RotaID,
		"veiculo_ids":                      veiculos,
		"turma_id":                         s.TurmaID,
		"codigo_academia":                  s.CodigoAcademia,
		"codigo_estudante":                 s.CodigoEstudante,
		"status":                           s.Status,
		"motivo_reprovacao":                s.MotivoReprovacao,
		"motivo_cancelamento":              s.MotivoCancelamento,
		"cancelada_por":                    s.CanceladaPor,
		"documento_path":                   s.DocumentoPath,
		"documento_url":                    s.DocumentoURL,
		"valor_taxa_inscricao":             s.ValorTaxaInscricao,
		"metodos_pagamento_taxa_inscricao": s.MetodosPagamentoTaxaInscricao,
		"aprovada_por":                     s.AprovadaPor,
		"reprovada_por":                    s.ReprovadaPor,
		"vinculada_em":                     vinculadaEm,
		"created_at":                       s.CreatedAt,
		"updated_at":                       s.UpdatedAt,
	}
}

func LoadSolicitacaoServicoExtra(c *gin.Context, id uuid.UUID) (*aggregates.SolicitacaoServicoExtra, bool) {
	a, e := GetRepository(c).WithContext(c.Request.Context()).Load(id, "SolicitacaoServicoExtra")
	if e != nil {
		utils.RespondWithNotFoundError(c, "solicitação de serviço extra")
		return nil, false
	}
	s, ok := a.(*aggregates.SolicitacaoServicoExtra)
	if !ok {
		utils.RespondWithInternalError(c, errors.New("tipo de aggregate inesperado"))
		return nil, false
	}
	return s, true
}

// RegistrarMembroDaAtividade põe o estudante na turma da atividade quando o
// vínculo de uma atividade coletiva é efetivado. Sem turma, não faz nada.
func RegistrarMembroDaAtividade(c *gin.Context, s *aggregates.SolicitacaoServicoExtra, audit db.AuditContext) error {
	if s.TipoServico != aggregates.TipoServicoSolicitadoAtividade || s.TurmaID == nil {
		return nil
	}
	return GetServicosRepo(c).RegistrarMembro(c.Request.Context(), audit, *s.TurmaID, s.CodigoEstudante, s.GetID())
}

// RemoverMembroDaAtividade tira o estudante da turma quando a inscrição acaba.
func RemoverMembroDaAtividade(c *gin.Context, s *aggregates.SolicitacaoServicoExtra, audit db.AuditContext) {
	if s.TipoServico != aggregates.TipoServicoSolicitadoAtividade {
		return
	}
	if err := GetServicosRepo(c).RemoverMembrosDaSolicitacao(c.Request.Context(), audit, s.GetID()); err != nil {
		log.Printf("[WARN] remover membro da turma da atividade (solicitação %s): %v", s.GetID(), err)
	}
}

func EfetivarVinculoServicoExtraPago(c *gin.Context, id string) error {
	x, e := uuid.Parse(id)
	if e != nil {
		return e
	}
	s, ok := LoadSolicitacaoServicoExtra(c, x)
	if !ok {
		return errors.New("solicitação não encontrada")
	}
	if s.Status != aggregates.StatusInscricaoAprovadaPendentePagamentoTaxa {
		return nil
	}
	if e = s.VincularAposPagamento(); e != nil {
		return e
	}
	audit := db.AuditContext{UserID: "pagamento:servico_extra", UserType: "sistema", IP: c.ClientIP()}
	if e = GetRepository(c).SaveWithAudit(s, audit); e != nil {
		return e
	}
	if e = RegistrarMembroDaAtividade(c, s, audit); e != nil {
		return fmt.Errorf("vínculo efetivado, mas a turma da atividade não foi atualizada: %w", e)
	}
	return nil
}

var _ = strings.TrimSpace
