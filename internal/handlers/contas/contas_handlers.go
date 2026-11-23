// Package contas reúne os handlers das contas de secretário, professor e
// encarregado (Plano de Atualizações, Parte 4).
package contas

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/handlers/apoio"
	"spuri/internal/middleware"
	"spuri/internal/permissoes"
	"spuri/internal/projections"
	"spuri/internal/utils"
)

// ── Contexto da academia ────────────────────────────────────────────────────

type academiaCtx struct {
	ID     uuid.UUID
	Codigo string
}

// academiaDaRequisicao devolve a academia EFETIVA (a própria academia, ou a do
// secretário que age em nome dela).
func academiaDaRequisicao(c *gin.Context) (*academiaCtx, bool) {
	id, ok := middleware.GetUserID(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return nil, false
	}
	cod, _ := c.Get("codigo_academia")
	codigo, _ := cod.(string)
	if codigo == "" {
		a, err := apoio.GetAcademiaProjection(c).GetByID(id)
		if err != nil || a == nil {
			utils.RespondWithNotFoundError(c, "academia")
			return nil, false
		}
		codigo = a.CodigoAcademia
	}
	return &academiaCtx{ID: id, Codigo: codigo}, true
}

// contaDaAcademia carrega a conta do tipo indicado e garante que pertence à academia.
func contaDaAcademia(c *gin.Context, ac *academiaCtx, tipo string) (*projections.ContaDTO, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("id inválido"))
		return nil, false
	}
	conta, err := apoio.GetContasProjection(c).GetByID(id, tipo)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return nil, false
	}
	if conta == nil || conta.Status == aggregates.StatusContaDeletado ||
		conta.CodigoAcademia == nil || *conta.CodigoAcademia != ac.Codigo {
		// Mesma resposta para "não existe" e "é de outra academia": não revela existência.
		utils.RespondWithNotFoundError(c, tipo)
		return nil, false
	}
	return conta, true
}

// ── Criação comum ───────────────────────────────────────────────────────────

// enviarCredenciais envia o SMS DEPOIS do commit (falha nunca desfaz a criação).
func enviarCredenciais(c *gin.Context, ac *academiaCtx, audit db.AuditContext, p *apoio.ContaPreparada) apoio.ResultadoCredenciais {
	ator := apoio.AtorComunicacao{ID: ac.ID, Tipo: "academia", CodigoAcademia: ac.Codigo}
	return apoio.EnviarCredenciaisSMS(c.Request.Context(), c, ator, audit, p.SMS, p.Dados.Nome, p.Dados.Codigo, p.Senha, false)
}

func dtoDaConta(a *aggregates.ContaPessoa) gin.H {
	h := gin.H{
		"id": a.ID, "tipo": a.Tipo, "codigo": a.Codigo, "nome": a.Nome, "genero": a.Genero,
		"data_nascimento": a.DataNascimento.Format("2006-01-02"), "telefone": a.Telefone,
		"email": a.Email, "status": a.Status, "email_verificado": a.EmailVerificado,
		"codigo_academia": a.CodigoAcademia,
	}
	return h
}

// ============================================================================
// Secretários — ações RESERVADAS à academia (internal/permissoes/rotas.go)
// ============================================================================

type pedidoSecretario struct {
	apoio.PedidoConta
	Permissoes      []string `json:"permissoes"`
	TodasPermissoes bool     `json:"todas_permissoes"`
}

// ListarPermissoes — GET /academia/permissoes: catálogo para a tela de criação.
func ListarPermissoes(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": permissoes.Catalogo})
}

// CriarSecretario — POST /academia/secretarios
func CriarSecretario(c *gin.Context) {
	ac, ok := academiaDaRequisicao(c)
	if !ok {
		return
	}
	var req pedidoSecretario
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("body inválido"))
		return
	}
	perms := req.Permissoes
	if req.TodasPermissoes {
		perms = permissoes.Todas()
	}
	prep, ok := apoio.PrepararConta(c, aggregates.TipoContaSecretario, "do secretário", req.PedidoConta)
	if !ok {
		return
	}
	sec := aggregates.NewSecretario()
	if err := sec.Criar(prep.Dados, ac.ID, ac.Codigo, perms, &ac.ID); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := sec.DefinirSenhaTemporaria(); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	audit := apoio.AuditoriaDaRequisicao(c, ac.ID, "academia")
	if err := apoio.GetRepository(c).SaveWithAudit(sec, audit); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	cred := enviarCredenciais(c, ac, audit, prep)
	h := dtoDaConta(&sec.ContaPessoa)
	h["permissoes"] = sec.Permissoes
	h["credenciais"] = cred
	c.JSON(http.StatusCreated, gin.H{"message": "secretário criado com sucesso", "data": h})
}

// ListarSecretarios — GET /academia/secretarios
func ListarSecretarios(c *gin.Context) {
	ac, ok := academiaDaRequisicao(c)
	if !ok {
		return
	}
	lista, err := apoio.GetContasProjection(c).ListarDaAcademia(aggregates.TipoContaSecretario, ac.Codigo)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": lista, "total": len(lista)})
}

// ObterSecretario — GET /academia/secretarios/:id
func ObterSecretario(c *gin.Context) {
	ac, ok := academiaDaRequisicao(c)
	if !ok {
		return
	}
	conta, ok := contaDaAcademia(c, ac, aggregates.TipoContaSecretario)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": conta})
}

type pedidoEdicao struct {
	Nome           *string  `json:"nome"`
	Genero         *string  `json:"genero"`
	DataNascimento *string  `json:"data_nascimento"`
	Telefone       *string  `json:"telefone"`
	Email          *string  `json:"email"`
	Permissoes     []string `json:"permissoes"`
}

// aplicarEdicaoDados valida unicidade de e-mail e aplica os dados comuns.
func aplicarEdicaoDados(c *gin.Context, conta *aggregates.ContaPessoa, req pedidoEdicao) bool {
	var dn *time.Time
	if req.DataNascimento != nil {
		t, err := apoio.ParseDataNascimento(*req.DataNascimento)
		if err != nil {
			utils.RespondWithValidationError(c, err)
			return false
		}
		dn = &t
	}
	if req.Email != nil {
		if apoio.EmailEmUso(c, *req.Email, conta.ID.String()) {
			utils.RespondWithConflictError(c, "email já cadastrado no sistema")
			return false
		}
	}
	if err := conta.AtualizarDados(req.Nome, req.Genero, dn, req.Email, req.Telefone); err != nil {
		utils.RespondWithValidationError(c, err)
		return false
	}
	return true
}

// EditarSecretario — PUT /academia/secretarios/:id (dados e/ou permissões)
func EditarSecretario(c *gin.Context) {
	ac, ok := academiaDaRequisicao(c)
	if !ok {
		return
	}
	dto, ok := contaDaAcademia(c, ac, aggregates.TipoContaSecretario)
	if !ok {
		return
	}
	var req pedidoEdicao
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("body inválido"))
		return
	}
	repo := apoio.GetRepository(c)
	agg, err := repo.Load(dto.ID, aggregates.AggregateSecretario)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	sec, ok2 := agg.(*aggregates.Secretario)
	if !ok2 {
		utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
		return
	}
	if !aplicarEdicaoDados(c, &sec.ContaPessoa, req) {
		return
	}
	if req.Permissoes != nil {
		if err := sec.DefinirPermissoes(req.Permissoes, &ac.ID); err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
	}
	if err := repo.SaveWithAudit(sec, apoio.AuditoriaDaRequisicao(c, ac.ID, "academia")); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	h := dtoDaConta(&sec.ContaPessoa)
	h["permissoes"] = sec.Permissoes
	c.JSON(http.StatusOK, gin.H{"message": "secretário atualizado com sucesso", "data": h})
}

// Ciclo de vida comum (ativar, desativar, eliminar) para secretário e professor.
func mudarEstado(c *gin.Context, tipo, aggType, acao string) {
	ac, ok := academiaDaRequisicao(c)
	if !ok {
		return
	}
	dto, ok := contaDaAcademia(c, ac, tipo)
	if !ok {
		return
	}
	repo := apoio.GetRepository(c)
	agg, err := repo.Load(dto.ID, aggType)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	var base *aggregates.ContaPessoa
	switch a := agg.(type) {
	case *aggregates.Secretario:
		base = &a.ContaPessoa
	case *aggregates.Professor:
		base = &a.ContaPessoa
	default:
		utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
		return
	}
	var motivo struct {
		Motivo string `json:"motivo"`
	}
	_ = c.ShouldBindJSON(&motivo) // corpo opcional
	switch acao {
	case "ativar":
		err = base.Ativar(&ac.ID)
	case "desativar":
		err = base.Desativar(&ac.ID, motivo.Motivo)
	default:
		err = base.Deletar(&ac.ID, motivo.Motivo)
	}
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := repo.SaveWithAudit(agg, apoio.AuditoriaDaRequisicao(c, ac.ID, "academia")); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("conta %s com sucesso", map[string]string{"ativar": "ativada", "desativar": "desativada", "eliminar": "eliminada"}[acao]), "data": gin.H{"id": dto.ID, "status": base.Status}})
}

func AtivarSecretario(c *gin.Context) {
	mudarEstado(c, aggregates.TipoContaSecretario, aggregates.AggregateSecretario, "ativar")
}
func DesativarSecretario(c *gin.Context) {
	mudarEstado(c, aggregates.TipoContaSecretario, aggregates.AggregateSecretario, "desativar")
}
func EliminarSecretario(c *gin.Context) {
	mudarEstado(c, aggregates.TipoContaSecretario, aggregates.AggregateSecretario, "eliminar")
}

// ============================================================================
// Professores — a academia E o secretário (com professores.gerir) criam
// ============================================================================

type pedidoVinculo struct {
	CodigoTurma string    `json:"codigo_turma"`
	MateriaID   uuid.UUID `json:"materia_id"`
}

type pedidoProfessor struct {
	apoio.PedidoConta
	Vinculos []pedidoVinculo `json:"vinculos"`
}

// validarParTurmaMateria garante que turma e matéria existem na academia, estão
// ativas e são compatíveis (mesmo nível e, quando ambas têm, mesmo curso).
func validarParTurmaMateria(c *gin.Context, ac *academiaCtx, v pedidoVinculo) error {
	if strings.TrimSpace(v.CodigoTurma) == "" || v.MateriaID == uuid.Nil {
		return fmt.Errorf("cada vínculo precisa de codigo_turma e materia_id")
	}
	turma, err := apoio.GetTurmasProjection(c).GetByCodigoTurma(v.CodigoTurma, ac.Codigo)
	if err != nil {
		return err
	}
	if turma == nil {
		return fmt.Errorf("turma '%s' não encontrada nesta academia", v.CodigoTurma)
	}
	if turma.Status != "ativo" {
		return fmt.Errorf("turma '%s' está %s", v.CodigoTurma, turma.Status)
	}
	materia, err := apoio.GetMateriasProjection(c).GetByID(v.MateriaID)
	if err != nil {
		return err
	}
	if materia == nil || materia.CodigoAcademia != ac.Codigo {
		return fmt.Errorf("matéria não encontrada nesta academia")
	}
	if materia.Status != "ativo" {
		return fmt.Errorf("a matéria '%s' está %s", materia.Nome, materia.Status)
	}
	if (materia.Type == "superior") != (turma.Nivel == "superior") {
		return fmt.Errorf("a matéria '%s' não é do mesmo nível da turma '%s'", materia.Nome, v.CodigoTurma)
	}
	if materia.CursoID != nil && turma.CursoID != nil && *materia.CursoID != *turma.CursoID {
		return fmt.Errorf("a matéria '%s' pertence a outro curso que não o da turma '%s'", materia.Nome, v.CodigoTurma)
	}
	return nil
}

// parLivre garante que ninguém mais (ativo) tem o par turma+matéria.
func parLivre(c *gin.Context, ac *academiaCtx, v pedidoVinculo, ignorarProfessor uuid.UUID) error {
	atual, err := apoio.GetContasProjection(c).ProfessorDoPar(ac.Codigo, v.CodigoTurma, v.MateriaID)
	if err != nil {
		return err
	}
	if atual != uuid.Nil && atual != ignorarProfessor {
		return fmt.Errorf("esta turma e matéria já têm um professor atribuído: use a reatribuição")
	}
	return nil
}

// CriarProfessor — POST /academia/professores
func CriarProfessor(c *gin.Context) {
	ac, ok := academiaDaRequisicao(c)
	if !ok {
		return
	}
	var req pedidoProfessor
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("body inválido"))
		return
	}
	vistos := map[string]bool{}
	for _, v := range req.Vinculos {
		chave := v.CodigoTurma + "|" + v.MateriaID.String()
		if vistos[chave] {
			utils.RespondWithValidationError(c, fmt.Errorf("vínculo repetido: turma '%s' e matéria %s", v.CodigoTurma, v.MateriaID))
			return
		}
		vistos[chave] = true
		if err := validarParTurmaMateria(c, ac, v); err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
		if err := parLivre(c, ac, v, uuid.Nil); err != nil {
			utils.RespondWithConflictError(c, err.Error())
			return
		}
	}
	prep, ok := apoio.PrepararConta(c, aggregates.TipoContaProfessor, "do professor", req.PedidoConta)
	if !ok {
		return
	}
	criador := ac.ID
	if id, tipo, atua := middleware.GetAtor(c); atua && tipo == "secretario" {
		criador = id
	}
	prof := aggregates.NewProfessor()
	if err := prof.Criar(prep.Dados, ac.ID, ac.Codigo, &criador); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := prof.DefinirSenhaTemporaria(); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	vinculosCriados := []gin.H{}
	for _, v := range req.Vinculos {
		id, err := prof.AtribuirVinculo(v.CodigoTurma, v.MateriaID, &criador)
		if err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
		vinculosCriados = append(vinculosCriados, gin.H{"id": id, "codigo_turma": v.CodigoTurma, "materia_id": v.MateriaID})
	}
	audit := apoio.AuditoriaDaRequisicao(c, ac.ID, "academia")
	if err := apoio.GetRepository(c).SaveWithAudit(prof, audit); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	cred := enviarCredenciais(c, ac, audit, prep)
	h := dtoDaConta(&prof.ContaPessoa)
	h["vinculos"] = vinculosCriados
	h["credenciais"] = cred
	c.JSON(http.StatusCreated, gin.H{"message": "professor criado com sucesso", "data": h})
}

// ListarProfessores — GET /academia/professores
func ListarProfessores(c *gin.Context) {
	ac, ok := academiaDaRequisicao(c)
	if !ok {
		return
	}
	proj := apoio.GetContasProjection(c)
	lista, err := proj.ListarDaAcademia(aggregates.TipoContaProfessor, ac.Codigo)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	saida := make([]gin.H, 0, len(lista))
	for _, p := range lista {
		vs, err := proj.VinculosDoProfessor(p.ID)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		saida = append(saida, gin.H{"professor": p, "vinculos": vs})
	}
	c.JSON(http.StatusOK, gin.H{"data": saida, "total": len(saida)})
}

// ObterProfessor — GET /academia/professores/:id
func ObterProfessor(c *gin.Context) {
	ac, ok := academiaDaRequisicao(c)
	if !ok {
		return
	}
	conta, ok := contaDaAcademia(c, ac, aggregates.TipoContaProfessor)
	if !ok {
		return
	}
	vs, err := apoio.GetContasProjection(c).VinculosDoProfessor(conta.ID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"professor": conta, "vinculos": vs}})
}

func carregarProfessor(c *gin.Context, id uuid.UUID) (*aggregates.Professor, bool) {
	agg, err := apoio.GetRepository(c).Load(id, aggregates.AggregateProfessor)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return nil, false
	}
	p, ok := agg.(*aggregates.Professor)
	if !ok {
		utils.RespondWithInternalError(c, fmt.Errorf("tipo de aggregate inesperado"))
		return nil, false
	}
	return p, true
}

func atorProfessores(c *gin.Context, ac *academiaCtx) uuid.UUID {
	if id, tipo, ok := middleware.GetAtor(c); ok && tipo == "secretario" {
		return id
	}
	return ac.ID
}

// EditarProfessor — PUT /academia/professores/:id (dados pessoais)
func EditarProfessor(c *gin.Context) {
	ac, ok := academiaDaRequisicao(c)
	if !ok {
		return
	}
	dto, ok := contaDaAcademia(c, ac, aggregates.TipoContaProfessor)
	if !ok {
		return
	}
	var req pedidoEdicao
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("body inválido"))
		return
	}
	prof, ok := carregarProfessor(c, dto.ID)
	if !ok {
		return
	}
	if !aplicarEdicaoDados(c, &prof.ContaPessoa, req) {
		return
	}
	if err := apoio.GetRepository(c).SaveWithAudit(prof, apoio.AuditoriaDaRequisicao(c, ac.ID, "academia")); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "professor atualizado com sucesso", "data": dtoDaConta(&prof.ContaPessoa)})
}

func AtivarProfessor(c *gin.Context) {
	mudarEstado(c, aggregates.TipoContaProfessor, aggregates.AggregateProfessor, "ativar")
}
func DesativarProfessor(c *gin.Context) {
	mudarEstado(c, aggregates.TipoContaProfessor, aggregates.AggregateProfessor, "desativar")
}
func EliminarProfessor(c *gin.Context) {
	mudarEstado(c, aggregates.TipoContaProfessor, aggregates.AggregateProfessor, "eliminar")
}

// AtribuirVinculoProfessor — POST /academia/professores/:id/vinculos
func AtribuirVinculoProfessor(c *gin.Context) {
	ac, ok := academiaDaRequisicao(c)
	if !ok {
		return
	}
	dto, ok := contaDaAcademia(c, ac, aggregates.TipoContaProfessor)
	if !ok {
		return
	}
	var req pedidoVinculo
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("body inválido"))
		return
	}
	if err := validarParTurmaMateria(c, ac, req); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := parLivre(c, ac, req, dto.ID); err != nil {
		utils.RespondWithConflictError(c, err.Error())
		return
	}
	prof, ok := carregarProfessor(c, dto.ID)
	if !ok {
		return
	}
	por := atorProfessores(c, ac)
	id, err := prof.AtribuirVinculo(req.CodigoTurma, req.MateriaID, &por)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := apoio.GetRepository(c).SaveWithAudit(prof, apoio.AuditoriaDaRequisicao(c, ac.ID, "academia")); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "vínculo atribuído com sucesso", "data": gin.H{"id": id, "professor_id": dto.ID, "codigo_turma": req.CodigoTurma, "materia_id": req.MateriaID}})
}

// RemoverVinculoProfessor — DELETE /academia/professores/:id/vinculos/:vinculo_id
func RemoverVinculoProfessor(c *gin.Context) {
	ac, ok := academiaDaRequisicao(c)
	if !ok {
		return
	}
	dto, ok := contaDaAcademia(c, ac, aggregates.TipoContaProfessor)
	if !ok {
		return
	}
	vid, err := uuid.Parse(c.Param("vinculo_id"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("vinculo_id inválido"))
		return
	}
	prof, ok := carregarProfessor(c, dto.ID)
	if !ok {
		return
	}
	por := atorProfessores(c, ac)
	if err := prof.RemoverVinculo(vid, &por, "removido_pela_academia"); err != nil {
		utils.RespondWithNotFoundError(c, "vínculo")
		return
	}
	if err := apoio.GetRepository(c).SaveWithAudit(prof, apoio.AuditoriaDaRequisicao(c, ac.ID, "academia")); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "vínculo removido com sucesso"})
}

// ReatribuirVinculo — PUT /academia/vinculos-professor/:vinculo_id/reatribuir
// Passa o par turma+matéria para outro professor. São dois agregados: o
// vínculo sai do professor atual e entra no novo, nessa ordem (o ledger é
// processado em ordem, logo a projeção nunca vê dois professores no mesmo par).
// Se a segunda gravação falhar, o vínculo é devolvido ao professor de origem.
func ReatribuirVinculo(c *gin.Context) {
	ac, ok := academiaDaRequisicao(c)
	if !ok {
		return
	}
	vid, err := uuid.Parse(c.Param("vinculo_id"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("vinculo_id inválido"))
		return
	}
	var req struct {
		ProfessorID uuid.UUID `json:"professor_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ProfessorID == uuid.Nil {
		utils.RespondWithValidationError(c, fmt.Errorf("professor_id é obrigatório"))
		return
	}
	proj := apoio.GetContasProjection(c)
	vinculo, err := proj.VinculoPorID(vid)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if vinculo == nil || vinculo.CodigoAcademia != ac.Codigo {
		utils.RespondWithNotFoundError(c, "vínculo")
		return
	}
	if vinculo.ProfessorID == req.ProfessorID {
		utils.RespondWithValidationError(c, fmt.Errorf("o vínculo já pertence a este professor"))
		return
	}
	destino, err := proj.GetByID(req.ProfessorID, aggregates.TipoContaProfessor)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if destino == nil || destino.Status != aggregates.StatusContaAtivo || destino.CodigoAcademia == nil || *destino.CodigoAcademia != ac.Codigo {
		utils.RespondWithNotFoundError(c, "professor de destino ativo")
		return
	}
	origem, ok := carregarProfessor(c, vinculo.ProfessorID)
	if !ok {
		return
	}
	novo, ok := carregarProfessor(c, destino.ID)
	if !ok {
		return
	}
	por := atorProfessores(c, ac)
	if err := origem.RemoverVinculo(vid, &por, "reatribuido"); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	novoID, err := novo.AtribuirVinculo(vinculo.CodigoTurma, vinculo.MateriaID, &por)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	repo := apoio.GetRepository(c)
	audit := apoio.AuditoriaDaRequisicao(c, ac.ID, "academia")
	if err := repo.SaveWithAudit(origem, audit); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	if err := repo.SaveWithAudit(novo, audit); err != nil {
		// Devolve o vínculo ao professor de origem.
		if volta, ok := recarregarSilencioso(c, vinculo.ProfessorID); ok {
			if _, e := volta.AtribuirVinculo(vinculo.CodigoTurma, vinculo.MateriaID, &por); e == nil {
				_ = repo.SaveWithAudit(volta, audit)
			}
		}
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "vínculo reatribuído com sucesso", "data": gin.H{"id": novoID, "professor_id": destino.ID, "codigo_turma": vinculo.CodigoTurma, "materia_id": vinculo.MateriaID}})
}

func recarregarSilencioso(c *gin.Context, id uuid.UUID) (*aggregates.Professor, bool) {
	agg, err := apoio.GetRepository(c).Load(id, aggregates.AggregateProfessor)
	if err != nil {
		return nil, false
	}
	p, ok := agg.(*aggregates.Professor)
	return p, ok
}
