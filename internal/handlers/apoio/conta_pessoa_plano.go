package apoio

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/projections"
	"spuri/internal/services"
	"spuri/internal/utils"
)

// Plano de Atualizações, Partes 4 e 5 — preparo de contas novas (secretário,
// professor, encarregado) e planejamento do encarregado no cadastro do
// estudante. Vive em apoio porque os pacotes de módulo só podem importar apoio
// (regra da Parte 2, protegida por TestPacotesDeModuloNaoSeImportam) e o
// cadastro do estudante e as rotas do encarregado compartilham esta lógica.

type PedidoConta struct {
	Nome           string  `json:"nome"`
	Genero         string  `json:"genero"`
	DataNascimento string  `json:"data_nascimento"`
	Telefone       string  `json:"telefone"`
	Email          *string `json:"email"`
}

// ContaPreparada é o resultado de PrepararConta: nada foi gravado ainda.
type ContaPreparada struct {
	Dados aggregates.DadosContaPessoa
	Senha string
	SMS   string // telefone normalizado 9XXXXXXXX
}

// PrepararConta valida o pedido e gera código e senha temporária. Nada é gravado.
func PrepararConta(c *gin.Context, tipo, quem string, p PedidoConta) (*ContaPreparada, bool) {
	// Os erros dizem de QUAL pessoa são ("encarregado: ..."): no cadastro do
	// estudante há duas pessoas no mesmo formulário.
	rotulo := map[string]string{"secretario": "secretário", "professor": "professor", "encarregado": "encarregado"}[tipo]
	falhar := func(err error) { utils.RespondWithValidationError(c, fmt.Errorf("%s: %w", rotulo, err)) }
	dn, err := ParseDataNascimento(p.DataNascimento)
	if err != nil {
		falhar(err)
		return nil, false
	}
	sms, err := TelefoneDestinoCredenciaisConta(p.Telefone, quem)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return nil, false
	}
	var email *string
	if p.Email != nil && strings.TrimSpace(*p.Email) != "" {
		e := strings.TrimSpace(*p.Email)
		if err := utils.ValidateEmail(e); err != nil {
			falhar(err)
			return nil, false
		}
		if EmailEmUso(c, e, "") {
			utils.RespondWithConflictError(c, "email já cadastrado no sistema")
			return nil, false
		}
		email = &e
	}
	codigo, err := GerarCodigoConta(c, tipo)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return nil, false
	}
	senha, err := services.GenerateTempPassword()
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return nil, false
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return nil, false
	}
	d := aggregates.DadosContaPessoa{
		Codigo: codigo, Nome: p.Nome, Genero: p.Genero, DataNascimento: dn,
		Email: email, Telefone: sms, SenhaHash: string(hash),
	}
	// Valida já, para devolver 400 antes de qualquer escrita.
	if err := aggregates.ValidarDadosContaPessoa(d); err != nil {
		falhar(err)
		return nil, false
	}
	return &ContaPreparada{Dados: d, Senha: senha, SMS: sms}, true
}

// PlanoEncarregado é o resultado de PlanejarEncarregado: nada foi gravado ainda.
type PlanoEncarregado struct {
	// Existente != nil: o encarregado já tem conta e apenas ganha mais um educando.
	Existente *projections.ContaDTO
	// Novo != nil: conta a criar (com senha temporária em prep).
	Novo *aggregates.Encarregado
	prep *ContaPreparada
	// guard impede duas contas novas para o mesmo telefone ao mesmo tempo
	// (a projeção é assíncrona: a consulta por telefone pode ainda não ver a anterior).
	guard         *db.UniqueGuardReservation
	guardConsumed bool

	relacao, relacaoOutro *string
}

// Telefone devolve o telefone (normalizado) do encarregado do plano.
func (p *PlanoEncarregado) Telefone() string {
	if p.Existente != nil {
		return p.Existente.Telefone
	}
	return p.prep.SMS
}

// Nome devolve o nome do encarregado do plano.
func (p *PlanoEncarregado) Nome() string {
	if p.Existente != nil {
		return p.Existente.Nome
	}
	return p.prep.Dados.Nome
}

// ContaNova diz se o plano cria uma conta (e, portanto, envia credenciais).
func (p *PlanoEncarregado) ContaNova() bool { return p.Novo != nil }

// PlanejarEncarregado valida o bloco e decide entre reutilizar e criar. Em caso
// de erro já respondeu à requisição (ok=false). criadoPor é quem cadastra.
func PlanejarEncarregado(c *gin.Context, b BlocoEncarregado, criadoPor *uuid.UUID) (*PlanoEncarregado, bool) {
	rel, relOutro, err := aggregates.ValidarRelacao(b.Relacao, b.RelacaoOutro)
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return nil, false
	}
	tel, err := TelefoneDestinoCredenciaisConta(b.Telefone, "do encarregado")
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return nil, false
	}
	email := ""
	if b.Email != nil {
		email = strings.TrimSpace(*b.Email)
	}
	proj := GetContasProjection(c)
	porTelefone, err := proj.GetEncarregadoPorContato(tel, "")
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return nil, false
	}
	var porEmail *projections.ContaDTO
	if email != "" {
		if porEmail, err = proj.GetEncarregadoPorContato("", email); err != nil {
			utils.RespondWithInternalError(c, err)
			return nil, false
		}
	}
	if porTelefone != nil && porEmail != nil && porTelefone.ID != porEmail.ID {
		utils.RespondWithConflictError(c, "o telefone e o e-mail informados pertencem a encarregados diferentes")
		return nil, false
	}
	existente := porTelefone
	if existente == nil {
		existente = porEmail
	}
	if existente != nil {
		if existente.Status != aggregates.StatusContaAtivo {
			utils.RespondWithConflictError(c, "o encarregado identificado está desativado")
			return nil, false
		}
		return &PlanoEncarregado{Existente: existente, relacao: rel, relacaoOutro: relOutro}, true
	}
	prep, ok := PrepararConta(c, aggregates.TipoContaEncarregado, "do encarregado", PedidoConta{
		Nome: b.Nome, Genero: b.Genero, DataNascimento: b.DataNascimento, Telefone: tel, Email: b.Email,
	})
	if !ok {
		return nil, false
	}
	novo := aggregates.NewEncarregado()
	if err := novo.Criar(prep.Dados, criadoPor); err != nil {
		utils.RespondWithValidationError(c, err)
		return nil, false
	}
	if err := novo.DefinirSenhaTemporaria(); err != nil {
		utils.RespondWithInternalError(c, err)
		return nil, false
	}
	guardOpts := db.UniqueGuardOptions{UserType: "encarregado"}
	if criadoPor != nil {
		guardOpts.UserID = criadoPor.String()
	}
	guard, err := db.NewUniqueOperationGuard(GetDbClient(c)).WithContext(c.Request.Context()).Reserve("encarregado:telefone", prep.SMS, guardOpts)
	if errors.Is(err, db.ErrUniqueOperationInProgress) {
		// O telefone já foi usado para criar uma conta cuja projeção pode ainda não
		// estar pronta (irmãos cadastrados em sequência, sobretudo em lote): em vez
		// de falhar, recupera o encarregado pelo próprio ledger, via a reserva consumida.
		if dto, achou := encarregadoPelaReservaDoTelefone(c, prep.SMS); achou {
			if dto.Status != aggregates.StatusContaAtivo {
				utils.RespondWithConflictError(c, "o encarregado identificado está desativado")
				return nil, false
			}
			return &PlanoEncarregado{Existente: dto, relacao: rel, relacaoOutro: relOutro}, true
		}
		utils.RespondWithConflictError(c, "já existe um cadastro de encarregado em andamento com este telefone; aguarde alguns instantes e tente de novo")
		return nil, false
	}
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return nil, false
	}
	return &PlanoEncarregado{Novo: novo, prep: prep, guard: guard, relacao: rel, relacaoOutro: relOutro}, true
}

// encarregadoPelaReservaDoTelefone devolve o encarregado criado com este telefone
// lendo o agregado do ledger (não da projeção), para o caso de a projeção ainda
// não ter processado a criação.
func encarregadoPelaReservaDoTelefone(c *gin.Context, telefone string) (*projections.ContaDTO, bool) {
	var idStr string
	err := GetDbClient(c).DB().QueryRow(`
		SELECT aggregate_id::text FROM unique_operation_guards
		 WHERE scope = 'encarregado:telefone' AND key = $1 AND status = 'consumed' AND aggregate_id IS NOT NULL
		 ORDER BY consumed_at DESC LIMIT 1`, telefone).Scan(&idStr)
	if err != nil {
		return nil, false
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, false
	}
	agg, err := GetRepository(c).Load(id, aggregates.AggregateEncarregado)
	if err != nil {
		return nil, false
	}
	enc, ok := agg.(*aggregates.Encarregado)
	if !ok || enc.Status == "" {
		return nil, false
	}
	return &projections.ContaDTO{
		ID: enc.ID, Tipo: aggregates.TipoContaEncarregado, Codigo: enc.Codigo, Nome: enc.Nome, Telefone: enc.Telefone,
		Email: enc.Email, Status: enc.Status, EmailVerificado: enc.EmailVerificado,
	}, true
}

// Concluir encerra o plano: sucesso consome a reserva do telefone; falha a
// libera. Deve ser chamado (com defer) por quem chamou PlanejarEncarregado.
func (p *PlanoEncarregado) Concluir(sucesso bool) {
	if p == nil || p.guard == nil || p.guardConsumed {
		return
	}
	if sucesso {
		if err := p.guard.Consume(p.Novo.ID); err == nil {
			p.guardConsumed = true
		}
		return
	}
	_ = p.guard.Release()
}

// Agregado devolve o agregado do encarregado do plano pronto para receber o
// vínculo (carrega o existente do ledger).
func (p *PlanoEncarregado) Agregado(c *gin.Context) (*aggregates.Encarregado, error) {
	if p.Novo != nil {
		return p.Novo, nil
	}
	agg, err := GetRepository(c).Load(p.Existente.ID, aggregates.AggregateEncarregado)
	if err != nil {
		return nil, err
	}
	enc, ok := agg.(*aggregates.Encarregado)
	if !ok {
		return nil, fmt.Errorf("tipo de aggregate inesperado")
	}
	return enc, nil
}

// Vincular liga o encarregado do plano ao estudante.
func (p *PlanoEncarregado) Vincular(enc *aggregates.Encarregado, estudanteID uuid.UUID, codigoEstudante string, por *uuid.UUID) error {
	return enc.VincularEducando(estudanteID, codigoEstudante, p.relacao, p.relacaoOutro, por)
}

// EnviarCredenciais envia o SMS da conta NOVA (nada para encarregado existente).
func (p *PlanoEncarregado) EnviarCredenciais(c *gin.Context, ator AtorComunicacao, audit db.AuditContext) *ResultadoCredenciais {
	if p.Novo == nil {
		return nil
	}
	r := EnviarCredenciaisSMS(c.Request.Context(), c, ator, audit, p.prep.SMS, p.prep.Dados.Nome, p.prep.Dados.Codigo, p.prep.Senha, false)
	return &r
}

// Codigo devolve o código de login do encarregado (conta nova ou existente).
func (p *PlanoEncarregado) Codigo() string {
	if p.Existente != nil {
		return p.Existente.Codigo
	}
	return p.prep.Dados.Codigo
}

// ID devolve o id do encarregado (conta nova ou existente).
func (p *PlanoEncarregado) ID() uuid.UUID {
	if p.Existente != nil {
		return p.Existente.ID
	}
	return p.Novo.ID
}
