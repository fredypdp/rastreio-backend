package servicosextras

// Gestão do catálogo fixo de serviços pela academia (Parte 7): Transporte
// (preço e prazo gerais, rotas, veículos) e Atividade extracurricular
// (horários, turmas da atividade). A academia só configura o que o catálogo
// oferece; não cria serviço genérico.

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"spuri/internal/cadastros"
	"spuri/internal/db"
	"spuri/internal/finance"
	"spuri/internal/handlers/apoio"
	"spuri/internal/utils"
)

func auditoriaAcademia(c *gin.Context, userID uuid.UUID) db.AuditContext {
	return db.AuditContext{UserID: userID.String(), UserType: "academia", IP: c.ClientIP(), Ator: apoio.AtorDaRequisicao(c)}
}

// validarProvedorServico mantém as regras da Tarefa 121: configurar cobrança
// exige um provedor de pagamento com credenciais, e as listas de métodos
// ENVIADAS precisam ser suportadas pelo provedor ativo da academia (editar outro
// campo não é bloqueado por um método legado).
func validarProvedorServico(c *gin.Context, codigo string, cobra bool, metodos ...[]string) bool {
	if cobra {
		tem, err := apoio.FinanceiroService.PossuiProvedorConfigurado(c.Request.Context(), finance.ContextoAcademia, codigo)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return false
		}
		if !tem {
			utils.RespondWithValidationError(c, errors.New("não é possível configurar cobrança sem um provedor de pagamento configurado (com credenciais) para a academia"))
			return false
		}
	}
	var todos []string
	for _, m := range metodos {
		todos = append(todos, m...)
	}
	if err := apoio.FinanceiroService.ValidarMetodosSeHouverProvedor(c.Request.Context(), codigo, todos); err != nil {
		apoio.FinanceError(c, err)
		return false
	}
	return true
}

func idDoCaminho(c *gin.Context, nome string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(nome))
	if err != nil {
		utils.RespondWithValidationError(c, errors.New("identificador inválido"))
		return uuid.Nil, false
	}
	return id, true
}

// cancelarMesesRemovidos cancela as cobranças não pagas dos meses que saíram do
// prazo, nas inscrições vinculadas ao serviço (e à rota, se informada).
func cancelarMesesRemovidos(c *gin.Context, servicoID uuid.UUID, rotaID *uuid.UUID, removidos []int, actor uuid.UUID) {
	if len(removidos) == 0 {
		return
	}
	rows, err := apoio.GetDBClient(c).DB().QueryContext(c.Request.Context(), `
		SELECT id::text FROM projection_solicitacoes_servico_extra
		WHERE servico_extra_id = $1 AND status = 'vinculada' AND ($2::uuid IS NULL OR rota_id = $2::uuid)`, servicoID, rotaID)
	if err != nil {
		log.Printf("[WARN] meses removidos do serviço %s: %v", servicoID, err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	n, err := apoio.FinanceiroService.CancelarCobrancasServicoMesesRemovidos(c.Request.Context(), ids, removidos, actor.String(), "academia", c.ClientIP())
	if err != nil {
		log.Printf("[WARN] cancelar cobranças dos meses removidos (serviço %s): %v", servicoID, err)
		return
	}
	if n > 0 {
		log.Printf("serviço %s: %d cobrança(s) não paga(s) cancelada(s) por meses removidos do prazo", servicoID, n)
	}
}

// ============================================================================
// Transporte
// ============================================================================

func GetTransporte(c *gin.Context) {
	codigo, _, ok := apoio.Academy(c)
	if !ok {
		return
	}
	t, persistido, err := apoio.GetServicosRepo(c).ObterTransporte(c.Request.Context(), codigo)
	if err != nil {
		apoio.RespondErroCadastro(c, err, "transporte")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": apoio.TransporteJSON(t, persistido)})
}

// AtualizarTransporte define a configuração geral. Campo ausente = não muda.
func AtualizarTransporte(c *gin.Context) {
	codigo, userID, ok := apoio.Academy(c)
	if !ok {
		return
	}
	var in struct {
		PrecoGeralModo     *string   `json:"preco_geral_modo"`
		PrecoGeral         *float64  `json:"preco_geral"`
		PrazoGeralModo     *string   `json:"prazo_geral_modo"`
		PrazoGeralMeses    *[]int    `json:"prazo_geral_meses"`
		ValorTaxaInscricao *float64  `json:"valor_taxa_inscricao"`
		Metodos            *[]string `json:"metodos_pagamento"`
		MetodosTaxa        *[]string `json:"metodos_pagamento_taxa_inscricao"`
		AnosAcademicos     *[]string `json:"anos_academicos_disponiveis"`
		CursosDisponiveis  *[]string `json:"cursos_disponiveis"`
		Ativo              *bool     `json:"ativo"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	if in.CursosDisponiveis != nil {
		if err := cadastros.ValidarDisponibilidade(cadastros.Disponibilidade{Cursos: *in.CursosDisponiveis}); err != nil {
			apoio.RespondErroCadastro(c, err, "transporte")
			return
		}
		if err := apoio.ValidarPosseCursosDisponiveis(c, codigo, *in.CursosDisponiveis); err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
	}
	cobra := (in.PrecoGeralModo != nil && *in.PrecoGeralModo == cadastros.ModoValor) || (in.ValorTaxaInscricao != nil && *in.ValorTaxaInscricao > 0)
	var enviados [][]string
	if in.Metodos != nil {
		enviados = append(enviados, *in.Metodos)
	}
	if in.MetodosTaxa != nil {
		enviados = append(enviados, *in.MetodosTaxa)
	}
	if !validarProvedorServico(c, codigo, cobra, enviados...) {
		return
	}
	type mesesRota struct{ antes, depois []int }
	porRota := map[uuid.UUID]*mesesRota{}
	var servicoID uuid.UUID
	t, err := apoio.GetServicosRepo(c).AtualizarTransporte(c.Request.Context(), auditoriaAcademia(c, userID), codigo, "TransporteConfiguracaoAtualizada", func(t *cadastros.Transporte) error {
		servicoID = t.ID
		for i := range t.Rotas {
			porRota[t.Rotas[i].ID] = &mesesRota{antes: t.Resolver(&t.Rotas[i]).Meses}
		}
		// Limpar o geral e definir o geral só se ajustam as rotas: o conflito
		// (rota com valor próprio) volta como 409 sem gravar nada.
		if in.PrecoGeralModo != nil {
			valor := 0.0
			if in.PrecoGeral != nil {
				valor = *in.PrecoGeral
			}
			if err := t.DefinirPrecoGeral(*in.PrecoGeralModo, valor); err != nil {
				return err
			}
		} else if in.PrecoGeral != nil {
			return &cadastros.ErroRegra{Err: errors.New("informe preco_geral_modo junto com preco_geral")}
		}
		if in.PrazoGeralModo != nil {
			var meses []int
			if in.PrazoGeralMeses != nil {
				meses = *in.PrazoGeralMeses
			}
			if err := t.DefinirPrazoGeral(*in.PrazoGeralModo, meses); err != nil {
				return err
			}
		} else if in.PrazoGeralMeses != nil {
			return &cadastros.ErroRegra{Err: errors.New("informe prazo_geral_modo junto com prazo_geral_meses")}
		}
		taxa, metodos, disp := t.Taxa, t.Metodos, t.Disponibilidade
		if in.ValorTaxaInscricao != nil {
			taxa.Valor = *in.ValorTaxaInscricao
			if taxa.Valor == 0 && in.MetodosTaxa == nil {
				taxa.Metodos = []string{}
			}
		}
		if in.MetodosTaxa != nil {
			taxa.Metodos = *in.MetodosTaxa
		}
		if in.Metodos != nil {
			metodos = *in.Metodos
		}
		if in.AnosAcademicos != nil {
			disp.AnosAcademicos = *in.AnosAcademicos
		}
		if in.CursosDisponiveis != nil {
			disp.Cursos = *in.CursosDisponiveis
		}
		if err := t.DefinirPagamento(taxa, metodos, disp); err != nil {
			return err
		}
		if in.Ativo != nil {
			t.Ativo = *in.Ativo
		}
		for i := range t.Rotas {
			porRota[t.Rotas[i].ID].depois = t.Resolver(&t.Rotas[i]).Meses
		}
		return nil
	})
	if err != nil {
		apoio.RespondErroCadastro(c, err, "transporte")
		return
	}
	for rid, m := range porRota {
		rid := rid
		cancelarMesesRemovidos(c, servicoID, &rid, cadastros.MesesRemovidos(m.antes, m.depois), userID)
	}
	c.JSON(http.StatusOK, gin.H{"data": apoio.TransporteJSON(t, true)})
}

type rotaPayload struct {
	Nome            string  `json:"nome"`
	HorarioEmbarque string  `json:"horario_embarque"`
	PrecoModo       string  `json:"preco_modo"`
	Preco           float64 `json:"preco"`
	PrazoMeses      []int   `json:"prazo_meses"`
}

func CriarRota(c *gin.Context) {
	codigo, userID, ok := apoio.Academy(c)
	if !ok {
		return
	}
	var in rotaPayload
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	if !validarProvedorServico(c, codigo, in.PrecoModo == cadastros.ModoValor) {
		return
	}
	rota, err := apoio.GetServicosRepo(c).CriarRota(c.Request.Context(), auditoriaAcademia(c, userID), codigo, func(t *cadastros.Transporte) (*cadastros.Rota, error) {
		return t.NovaRota(in.Nome, in.HorarioEmbarque, in.PrecoModo, in.Preco, in.PrazoMeses)
	})
	if err != nil {
		apoio.RespondErroCadastro(c, err, "rota")
		return
	}
	devolverTransporte(c, codigo, http.StatusCreated, gin.H{"rota_id": rota.ID})
}

// devolverTransporte responde com o transporte inteiro, já atualizado.
func devolverTransporte(c *gin.Context, codigo string, status int, extra gin.H) {
	t, persistido, err := apoio.GetServicosRepo(c).ObterTransporte(c.Request.Context(), codigo)
	if err != nil {
		apoio.RespondErroCadastro(c, err, "transporte")
		return
	}
	out := gin.H{"data": apoio.TransporteJSON(t, persistido)}
	for k, v := range extra {
		out[k] = v
	}
	c.JSON(status, out)
}

func AtualizarRota(c *gin.Context) {
	codigo, userID, ok := apoio.Academy(c)
	if !ok {
		return
	}
	id, ok := idDoCaminho(c, "id")
	if !ok {
		return
	}
	var in rotaPayload
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	if !validarProvedorServico(c, codigo, in.PrecoModo == cadastros.ModoValor) {
		return
	}
	t, _, err := apoio.GetServicosRepo(c).ObterTransporte(c.Request.Context(), codigo)
	if err != nil {
		apoio.RespondErroCadastro(c, err, "rota")
		return
	}
	_, antes, depois, err := apoio.GetServicosRepo(c).AtualizarRota(c.Request.Context(), auditoriaAcademia(c, userID), codigo, id, "TransporteRotaAtualizada",
		func(t *cadastros.Transporte, r *cadastros.Rota) error {
			return t.AtualizarRota(r, in.Nome, in.HorarioEmbarque, in.PrecoModo, in.Preco, in.PrazoMeses)
		})
	if err != nil {
		apoio.RespondErroCadastro(c, err, "rota")
		return
	}
	cancelarMesesRemovidos(c, t.ID, &id, cadastros.MesesRemovidos(antes, depois), userID)
	devolverTransporte(c, codigo, http.StatusOK, nil)
}

func alternarRota(c *gin.Context, ativo bool) {
	codigo, userID, ok := apoio.Academy(c)
	if !ok {
		return
	}
	id, ok := idDoCaminho(c, "id")
	if !ok {
		return
	}
	acao := "TransporteRotaReativada"
	if !ativo {
		acao = "TransporteRotaDesativada"
	}
	_, _, _, err := apoio.GetServicosRepo(c).AtualizarRota(c.Request.Context(), auditoriaAcademia(c, userID), codigo, id, acao,
		func(_ *cadastros.Transporte, r *cadastros.Rota) error {
			if r.Ativo == ativo {
				return &cadastros.ErroRegra{Err: errors.New("a rota já está nesse estado")}
			}
			r.Ativo = ativo
			return nil
		})
	if err != nil {
		apoio.RespondErroCadastro(c, err, "rota")
		return
	}
	devolverTransporte(c, codigo, http.StatusOK, nil)
}

func DesativarRota(c *gin.Context) { alternarRota(c, false) }
func ReativarRota(c *gin.Context)  { alternarRota(c, true) }

func ExcluirRota(c *gin.Context) {
	codigo, userID, ok := apoio.Academy(c)
	if !ok {
		return
	}
	id, ok := idDoCaminho(c, "id")
	if !ok {
		return
	}
	if err := apoio.GetServicosRepo(c).ExcluirRota(c.Request.Context(), auditoriaAcademia(c, userID), codigo, id); err != nil {
		apoio.RespondErroCadastro(c, err, "rota")
		return
	}
	devolverTransporte(c, codigo, http.StatusOK, nil)
}

func CriarVeiculo(c *gin.Context) {
	codigo, userID, ok := apoio.Academy(c)
	if !ok {
		return
	}
	rotaID, ok := idDoCaminho(c, "id")
	if !ok {
		return
	}
	var in struct {
		Nome string `json:"nome"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	v, err := apoio.GetServicosRepo(c).CriarVeiculo(c.Request.Context(), auditoriaAcademia(c, userID), codigo, rotaID, in.Nome)
	if err != nil {
		apoio.RespondErroCadastro(c, err, "rota")
		return
	}
	devolverTransporte(c, codigo, http.StatusCreated, gin.H{"veiculo_id": v.ID})
}

func AtualizarVeiculo(c *gin.Context) {
	codigo, userID, ok := apoio.Academy(c)
	if !ok {
		return
	}
	id, ok := idDoCaminho(c, "id")
	if !ok {
		return
	}
	var in struct {
		Nome  *string `json:"nome"`
		Ativo *bool   `json:"ativo"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || (in.Nome == nil && in.Ativo == nil) {
		utils.RespondWithValidationError(c, errors.New("informe nome e/ou ativo"))
		return
	}
	if _, err := apoio.GetServicosRepo(c).AtualizarVeiculo(c.Request.Context(), auditoriaAcademia(c, userID), codigo, id, in.Nome, in.Ativo); err != nil {
		apoio.RespondErroCadastro(c, err, "veículo")
		return
	}
	devolverTransporte(c, codigo, http.StatusOK, nil)
}

func ExcluirVeiculo(c *gin.Context) {
	codigo, userID, ok := apoio.Academy(c)
	if !ok {
		return
	}
	id, ok := idDoCaminho(c, "id")
	if !ok {
		return
	}
	if err := apoio.GetServicosRepo(c).ExcluirVeiculo(c.Request.Context(), auditoriaAcademia(c, userID), codigo, id); err != nil {
		apoio.RespondErroCadastro(c, err, "veículo")
		return
	}
	devolverTransporte(c, codigo, http.StatusOK, nil)
}

// ============================================================================
// Atividades
// ============================================================================

type horarioPayload struct {
	DiaSemana  int    `json:"dia_semana"`
	HoraInicio string `json:"hora_inicio"`
	HoraFim    string `json:"hora_fim"`
}

type atividadePayload struct {
	Nome               *string           `json:"nome"`
	Instrutor          *string           `json:"instrutor"`
	Coletiva           *bool             `json:"coletiva"`
	Gratuito           *bool             `json:"gratuito"`
	Preco              *float64          `json:"preco"`
	PrazoMeses         *[]int            `json:"prazo_meses"`
	ValorTaxaInscricao *float64          `json:"valor_taxa_inscricao"`
	Metodos            *[]string         `json:"metodos_pagamento"`
	MetodosTaxa        *[]string         `json:"metodos_pagamento_taxa_inscricao"`
	AnosAcademicos     *[]string         `json:"anos_academicos_disponiveis"`
	CursosDisponiveis  *[]string         `json:"cursos_disponiveis"`
	Horarios           *[]horarioPayload `json:"horarios"`
}

// aplicar mescla o payload sobre a atividade (campo ausente = mantém) e
// valida tudo pelas regras do pacote cadastros.
func (in atividadePayload) aplicar(a *cadastros.Atividade) error {
	nome, instrutor, coletiva, gratuito, preco := a.Nome, a.Instrutor, a.Coletiva, a.Gratuito, a.Preco
	meses, taxa, metodos, disp, horarios := a.PrazoMeses, a.Taxa, a.Metodos, a.Disponibilidade, a.Horarios
	if in.Nome != nil {
		nome = *in.Nome
	}
	if in.Instrutor != nil {
		instrutor = *in.Instrutor
	}
	if in.Coletiva != nil {
		coletiva = *in.Coletiva
	}
	if in.Gratuito != nil {
		gratuito = *in.Gratuito
		if gratuito {
			// Gratuita: ninguém é cobrado, então preço e prazo ficam vazios.
			preco, meses = 0, []int{}
		}
	}
	if in.Preco != nil {
		preco = *in.Preco
	}
	if in.PrazoMeses != nil {
		meses = *in.PrazoMeses
	}
	if in.ValorTaxaInscricao != nil {
		taxa.Valor = *in.ValorTaxaInscricao
		if taxa.Valor == 0 && in.MetodosTaxa == nil {
			taxa.Metodos = []string{}
		}
	}
	if in.MetodosTaxa != nil {
		taxa.Metodos = *in.MetodosTaxa
	}
	if in.Metodos != nil {
		metodos = *in.Metodos
	}
	if in.AnosAcademicos != nil {
		disp.AnosAcademicos = *in.AnosAcademicos
	}
	if in.CursosDisponiveis != nil {
		disp.Cursos = *in.CursosDisponiveis
	}
	if in.Horarios != nil {
		horarios = make([]cadastros.Horario, 0, len(*in.Horarios))
		for _, h := range *in.Horarios {
			horarios = append(horarios, cadastros.Horario{DiaSemana: h.DiaSemana, HoraInicio: h.HoraInicio, HoraFim: h.HoraFim})
		}
	}
	return a.Atualizar(nome, instrutor, coletiva, gratuito, preco, meses, taxa, metodos, disp, horarios)
}

// validarProvedorAtividade aplica validarProvedorServico ao que o payload pede.
func (in atividadePayload) validarProvedor(c *gin.Context, codigo string) bool {
	cobra := (in.Gratuito != nil && !*in.Gratuito) || in.Preco != nil || (in.ValorTaxaInscricao != nil && *in.ValorTaxaInscricao > 0)
	var enviados [][]string
	if in.Metodos != nil {
		enviados = append(enviados, *in.Metodos)
	}
	if in.MetodosTaxa != nil {
		enviados = append(enviados, *in.MetodosTaxa)
	}
	return validarProvedorServico(c, codigo, cobra, enviados...)
}

func ListarAtividades(c *gin.Context) {
	apoio.ListarServicosExtrasAcademia(c)
}

func GetAtividade(c *gin.Context) {
	codigo, _, ok := apoio.Academy(c)
	if !ok {
		return
	}
	id, ok := idDoCaminho(c, "id")
	if !ok {
		return
	}
	a, err := apoio.GetServicosRepo(c).ObterAtividade(c.Request.Context(), id)
	if err != nil || a.CodigoAcademia != codigo {
		utils.RespondWithNotFoundError(c, "atividade")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": apoio.AtividadeJSON(a)})
}

func CriarAtividade(c *gin.Context) {
	codigo, userID, ok := apoio.Academy(c)
	if !ok {
		return
	}
	var in atividadePayload
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	if in.CursosDisponiveis != nil {
		if err := cadastros.ValidarDisponibilidade(cadastros.Disponibilidade{Cursos: *in.CursosDisponiveis}); err != nil {
			apoio.RespondErroCadastro(c, err, "atividade")
			return
		}
		if err := apoio.ValidarPosseCursosDisponiveis(c, codigo, *in.CursosDisponiveis); err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
	}
	if !in.validarProvedor(c, codigo) {
		return
	}
	// Parte de uma atividade vazia, para reaproveitar a mesma mesclagem da edição.
	base := &cadastros.Atividade{CodigoAcademia: codigo, Nome: "-", Instrutor: "-", Gratuito: true}
	if in.Nome == nil || in.Instrutor == nil {
		utils.RespondWithValidationError(c, errors.New("nome e instrutor são obrigatórios"))
		return
	}
	if in.Gratuito == nil && in.Preco == nil {
		utils.RespondWithValidationError(c, errors.New("informe gratuito=true ou o preço e os meses cobrados"))
		return
	}
	if in.Gratuito == nil {
		falso := false
		in.Gratuito = &falso
	}
	if err := in.aplicar(base); err != nil {
		apoio.RespondErroCadastro(c, err, "atividade")
		return
	}
	a, err := cadastros.NovaAtividade(codigo, base.Nome, base.Instrutor, base.Coletiva, base.Gratuito, base.Preco, base.PrazoMeses,
		base.Taxa, base.Metodos, base.Disponibilidade, base.Horarios)
	if err != nil {
		apoio.RespondErroCadastro(c, err, "atividade")
		return
	}
	if err := apoio.GetServicosRepo(c).CriarAtividade(c.Request.Context(), auditoriaAcademia(c, userID), a); err != nil {
		apoio.RespondErroCadastro(c, err, "atividade")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": apoio.AtividadeJSON(a)})
}

func AtualizarAtividade(c *gin.Context) {
	codigo, userID, ok := apoio.Academy(c)
	if !ok {
		return
	}
	id, ok := idDoCaminho(c, "id")
	if !ok {
		return
	}
	var in atividadePayload
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	if in.CursosDisponiveis != nil {
		if err := cadastros.ValidarDisponibilidade(cadastros.Disponibilidade{Cursos: *in.CursosDisponiveis}); err != nil {
			apoio.RespondErroCadastro(c, err, "atividade")
			return
		}
		if err := apoio.ValidarPosseCursosDisponiveis(c, codigo, *in.CursosDisponiveis); err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
	}
	if !in.validarProvedor(c, codigo) {
		return
	}
	a, antes, depois, err := apoio.GetServicosRepo(c).AtualizarAtividade(c.Request.Context(), auditoriaAcademia(c, userID), codigo, id, "AtividadeAtualizada", in.aplicar)
	if err != nil {
		apoio.RespondErroCadastro(c, err, "atividade")
		return
	}
	cancelarMesesRemovidos(c, id, nil, cadastros.MesesRemovidos(antes, depois), userID)
	novo, err := apoio.GetServicosRepo(c).ObterAtividade(c.Request.Context(), a.ID)
	if err != nil {
		apoio.RespondErroCadastro(c, err, "atividade")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": apoio.AtividadeJSON(novo)})
}

func alternarAtividade(c *gin.Context, ativo bool) {
	codigo, userID, ok := apoio.Academy(c)
	if !ok {
		return
	}
	id, ok := idDoCaminho(c, "id")
	if !ok {
		return
	}
	acao := "AtividadeReativada"
	if !ativo {
		acao = "AtividadeDesativada"
	}
	a, _, _, err := apoio.GetServicosRepo(c).AtualizarAtividade(c.Request.Context(), auditoriaAcademia(c, userID), codigo, id, acao, func(a *cadastros.Atividade) error {
		if a.Ativo == ativo {
			return &cadastros.ErroRegra{Err: errors.New("a atividade já está nesse estado")}
		}
		a.Ativo = ativo
		return nil
	})
	if err != nil {
		apoio.RespondErroCadastro(c, err, "atividade")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": apoio.AtividadeJSON(a)})
}

func DesativarAtividade(c *gin.Context) { alternarAtividade(c, false) }
func ReativarAtividade(c *gin.Context)  { alternarAtividade(c, true) }

func ExcluirAtividade(c *gin.Context) {
	codigo, userID, ok := apoio.Academy(c)
	if !ok {
		return
	}
	id, ok := idDoCaminho(c, "id")
	if !ok {
		return
	}
	if err := apoio.GetServicosRepo(c).ExcluirAtividade(c.Request.Context(), auditoriaAcademia(c, userID), codigo, id); err != nil {
		apoio.RespondErroCadastro(c, err, "atividade")
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "atividade apagada"})
}

// ============================================================================
// Turmas da atividade
// ============================================================================

func CriarTurmaAtividade(c *gin.Context) {
	codigo, userID, ok := apoio.Academy(c)
	if !ok {
		return
	}
	id, ok := idDoCaminho(c, "id")
	if !ok {
		return
	}
	var in struct {
		Nome string `json:"nome"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.RespondWithValidationError(c, errors.New("payload inválido"))
		return
	}
	if _, err := apoio.GetServicosRepo(c).CriarTurma(c.Request.Context(), auditoriaAcademia(c, userID), codigo, id, in.Nome); err != nil {
		apoio.RespondErroCadastro(c, err, "atividade")
		return
	}
	a, err := apoio.GetServicosRepo(c).ObterAtividade(c.Request.Context(), id)
	if err != nil {
		apoio.RespondErroCadastro(c, err, "atividade")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": apoio.AtividadeJSON(a)})
}

func AtualizarTurmaAtividade(c *gin.Context) {
	codigo, userID, ok := apoio.Academy(c)
	if !ok {
		return
	}
	id, ok := idDoCaminho(c, "turma_id")
	if !ok {
		return
	}
	var in struct {
		Nome  *string `json:"nome"`
		Ativo *bool   `json:"ativo"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || (in.Nome == nil && in.Ativo == nil) {
		utils.RespondWithValidationError(c, errors.New("informe nome e/ou ativo"))
		return
	}
	t, err := apoio.GetServicosRepo(c).AtualizarTurma(c.Request.Context(), auditoriaAcademia(c, userID), codigo, id, in.Nome, in.Ativo)
	if err != nil {
		apoio.RespondErroCadastro(c, err, "turma da atividade")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": t.ID, "atividade_id": t.AtividadeID, "nome": t.Nome, "ativo": t.Ativo, "membros": t.Membros}})
}

func ExcluirTurmaAtividade(c *gin.Context) {
	codigo, userID, ok := apoio.Academy(c)
	if !ok {
		return
	}
	id, ok := idDoCaminho(c, "turma_id")
	if !ok {
		return
	}
	if err := apoio.GetServicosRepo(c).ExcluirTurma(c.Request.Context(), auditoriaAcademia(c, userID), codigo, id); err != nil {
		apoio.RespondErroCadastro(c, err, "turma da atividade")
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "turma da atividade apagada"})
}

func ListarMembrosTurmaAtividade(c *gin.Context) {
	codigo, _, ok := apoio.Academy(c)
	if !ok {
		return
	}
	id, ok := idDoCaminho(c, "turma_id")
	if !ok {
		return
	}
	membros, err := apoio.GetServicosRepo(c).ListarMembros(c.Request.Context(), codigo, id)
	if err != nil {
		apoio.RespondErroCadastro(c, err, "turma da atividade")
		return
	}
	out := make([]gin.H, 0, len(membros))
	for _, m := range membros {
		out = append(out, gin.H{"codigo_estudante": m.CodigoEstudante, "solicitacao_id": m.SolicitacaoID, "desde": m.Desde})
	}
	c.JSON(http.StatusOK, gin.H{"membros": out, "total": len(out)})
}
