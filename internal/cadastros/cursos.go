package cadastros

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"spuri/internal/db"
	"spuri/internal/utils"
)

const (
	ModeloCursoMedioLiceu   = "liceu"
	ModeloCursoMedioTecnico = "tecnico"

	StatusAtivo    = "ativo"
	StatusInativo  = "inativo"
	StatusDeletado = "deletado"

	EntidadeCurso = "Curso"
)

// Curso é o registro de um curso da academia. O tipo ("medio" ou "superior") é
// imutável depois da criação.
type Curso struct {
	ID             uuid.UUID
	Nome           string
	Type           string   // "medio" ou "superior"
	Modelo         string   // só curso médio: "liceu" ou "tecnico"
	AnosAcademicos []string // derivados do modelo (médio) ou dos períodos (superior)
	Periodos       []string // só curso superior ([n]_semestre, sequenciais)
	CodigoAcademia string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Version        int
	DeletedAt      *time.Time
}

// ============================================================================
// Regras (funções puras, sem banco)
// ============================================================================

// NovoCurso valida e monta um curso novo, ativo.
//
// Médio: periodos deve ser vazio (os trimestres são fixos do sistema) e
// anosAcademicos não pode ser informado (deriva do modelo).
// Superior: periodos é obrigatório, no formato [n]_semestre, sequenciais.
func NovoCurso(nome, tipo, modelo string, anosAcademicos, periodos []string, codigoAcademia string) (*Curso, error) {
	if nome == "" {
		return nil, regra("nome é obrigatório")
	}
	if tipo != "medio" && tipo != "superior" {
		return nil, regra("tipo deve ser 'medio' ou 'superior'")
	}
	if codigoAcademia == "" {
		return nil, regra("código da academia é obrigatório")
	}
	if err := validarModeloCurso(tipo, modelo); err != nil {
		return nil, err
	}
	if tipo == "medio" {
		if len(anosAcademicos) > 0 {
			return nil, regra("anos_academicos de cursos médios são fixos e derivados de modelo; não podem ser informados manualmente")
		}
		var err error
		anosAcademicos, err = DerivarAnosAcademicosCursoMedio(modelo)
		if err != nil {
			return nil, &ErroRegra{Err: err}
		}
	}
	if len(anosAcademicos) == 0 {
		return nil, regra("anos_academicos é obrigatório")
	}
	if err := utils.ValidateAnosCurso(tipo, anosAcademicos); err != nil {
		return nil, &ErroRegra{Err: err}
	}
	if err := validarPeriodosCurso(tipo, periodos, len(anosAcademicos)); err != nil {
		return nil, err
	}

	return &Curso{
		ID:             uuid.New(),
		Nome:           nome,
		Type:           tipo,
		Modelo:         normalizarModeloCurso(tipo, modelo),
		AnosAcademicos: anosAcademicos,
		Periodos:       normalizarPeriodos(tipo, periodos),
		CodigoAcademia: codigoAcademia,
		Status:         StatusAtivo,
	}, nil
}

// Ativar passa o curso para ativo.
func (c *Curso) Ativar() error {
	if c.Status == StatusAtivo {
		return regra("curso já está ativo")
	}
	c.Status = StatusAtivo
	return nil
}

// Desativar passa o curso para inativo.
func (c *Curso) Desativar() error {
	if c.Status == StatusInativo {
		return regra("curso já está inativo")
	}
	c.Status = StatusInativo
	return nil
}

// AtualizarDados atualiza nome, anos_academicos, periodos e/ou modelo. nil não
// altera o campo. O tipo é imutável.
func (c *Curso) AtualizarDados(nome *string, anosAcademicos []string, periodos *[]string, modelo *string) error {
	if nome == nil && anosAcademicos == nil && periodos == nil && modelo == nil {
		return regra("nenhum campo para atualizar")
	}

	var modeloNovo string
	if modelo != nil && c.Type == "medio" {
		return regra("modelo de curso médio é imutável; crie outro curso para trocar a duração acadêmica")
	}
	if modelo != nil {
		if err := validarModeloCurso(c.Type, *modelo); err != nil {
			return err
		}
		modeloNovo = normalizarModeloCurso(c.Type, *modelo)
	}

	if anosAcademicos != nil && c.Type == "medio" {
		return regra("anos_academicos de cursos médios são fixos e derivados de modelo; não podem ser alterados manualmente")
	}
	if anosAcademicos != nil {
		if err := utils.ValidateAnosCurso(c.Type, anosAcademicos); err != nil {
			return &ErroRegra{Err: err}
		}
	}

	var periodosNovos []string
	if periodos != nil {
		totalAnos := len(c.AnosAcademicos)
		if anosAcademicos != nil {
			totalAnos = len(anosAcademicos)
		}
		if err := validarPeriodosCurso(c.Type, *periodos, totalAnos); err != nil {
			return err
		}
		periodosNovos = normalizarPeriodos(c.Type, *periodos)
	}

	// Tudo validado: só agora o estado muda.
	if nome != nil {
		c.Nome = *nome
	}
	if anosAcademicos != nil {
		c.AnosAcademicos = anosAcademicos
	}
	if periodos != nil {
		c.Periodos = periodosNovos
	}
	if modelo != nil {
		c.Modelo = modeloNovo
	}
	return nil
}

// Deletar marca o curso como deletado (exclusão lógica). A validação de
// estudantes matriculados e matérias ativas é feita pelo handler; aqui só o
// estado do próprio curso: precisa estar inativo.
func (c *Curso) Deletar() error {
	if c.Status == StatusDeletado {
		return regra("curso já está deletado")
	}
	if c.Status == StatusAtivo {
		return regra("desative o curso antes de deletá-lo")
	}
	agora := time.Now().UTC()
	c.Status = StatusDeletado
	c.DeletedAt = &agora
	return nil
}

func isSemestreValido(p string) bool {
	const sufixo = "_semestre"
	if !strings.HasSuffix(p, sufixo) {
		return false
	}
	numStr := strings.TrimSuffix(p, sufixo)
	if len(numStr) == 0 {
		return false
	}
	n, err := strconv.Atoi(numStr)
	return err == nil && n >= 1
}

// validarPeriodosCurso valida os períodos de acordo com o tipo do curso.
//
//   - "medio":    não deve ter períodos — são fixos no sistema
//     (1_trimestre, 2_trimestre, 3_trimestre).
//   - "superior": obrigatório; cada item deve seguir [número]_semestre;
//     sem duplicatas; sequenciais a partir de 1_semestre.
func validarPeriodosCurso(tipo string, periodos []string, totalAnos int) error {
	switch tipo {
	case "superior":
		if len(periodos) == 0 {
			return regra("periodos é obrigatório para cursos do tipo 'superior' (formato: 1_semestre, 2_semestre, ...)")
		}
		// Cada ano acadêmico tem entre 1 e 2 semestres:
		// total_semestres >= total_anos e total_semestres <= total_anos*2.
		if totalAnos > 0 {
			if len(periodos) < totalAnos || len(periodos) > totalAnos*2 {
				return regra("cursos superiores devem respeitar: total_semestres >= total_anos e total_semestres <= total_anos*2")
			}
		}
		seen := make(map[string]bool, len(periodos))
		for i, p := range periodos {
			if !isSemestreValido(p) {
				return regra("período '%s' inválido para curso superior. Use o formato [número]_semestre (ex.: 1_semestre, 2_semestre)", p)
			}
			if seen[p] {
				return regra("período duplicado: '%s'", p)
			}
			seen[p] = true
			if p != fmt.Sprintf("%d_semestre", i+1) {
				return regra("periodos de curso superior devem ser sequenciais de 1_semestre até N_semestre")
			}
		}
		totalAnosEsperado := (len(periodos) + 1) / 2
		if totalAnos > 0 && totalAnos != totalAnosEsperado {
			return regra("anos_academicos de curso superior devem ser derivados de periodos: esperado %d ano(s)", totalAnosEsperado)
		}
	case "medio":
		if len(periodos) > 0 {
			return regra("cursos do tipo 'medio' não devem ter periodos definidos — são fixos no sistema: 1_trimestre, 2_trimestre, 3_trimestre")
		}
	}
	return nil
}

// normalizarPeriodos garante que médio nunca persista períodos (os trimestres
// são constantes do sistema, não dados do curso).
func normalizarPeriodos(tipo string, periodos []string) []string {
	if tipo == "medio" {
		return []string{}
	}
	return periodos
}

// DerivarAnosAcademicosCursoMedio retorna a duração acadêmica fixa de um curso médio.
func DerivarAnosAcademicosCursoMedio(modelo string) ([]string, error) {
	switch strings.TrimSpace(modelo) {
	case ModeloCursoMedioLiceu:
		return []string{"1_ano_medio", "2_ano_medio", "3_ano_medio"}, nil
	case ModeloCursoMedioTecnico:
		return []string{"1_ano_medio", "2_ano_medio", "3_ano_medio", "4_ano_medio"}, nil
	default:
		return nil, fmt.Errorf("modelo inválido para curso médio: %q. Valores permitidos: 'liceu' ou 'tecnico'", strings.TrimSpace(modelo))
	}
}

func validarModeloCurso(tipo, modelo string) error {
	modelo = strings.TrimSpace(modelo)
	switch tipo {
	case "medio":
		if modelo == "" {
			return regra("modelo é obrigatório para cursos do tipo medio e deve ser 'liceu' ou 'tecnico'")
		}
		if modelo != ModeloCursoMedioLiceu && modelo != ModeloCursoMedioTecnico {
			return regra("modelo inválido para curso médio: %q. Valores permitidos: 'liceu' ou 'tecnico'", modelo)
		}
	case "superior":
		if modelo != "" {
			return regra("modelo é exclusivo para cursos do tipo medio")
		}
	}
	return nil
}

func normalizarModeloCurso(tipo, modelo string) string {
	if tipo != "medio" {
		return ""
	}
	return strings.TrimSpace(modelo)
}

// ============================================================================
// Persistência
// ============================================================================

// CursosRepo grava cursos na tabela de registro, com auditoria na mesma
// transação.
type CursosRepo struct{ client *db.Client }

func NovoCursosRepo(client *db.Client) *CursosRepo { return &CursosRepo{client: client} }

// snapshot do curso para antes/depois da auditoria (nunca traz dado sensível).
func (c *Curso) snapshot() map[string]any {
	return map[string]any{
		"nome":            c.Nome,
		"type":            c.Type,
		"modelo":          c.Modelo,
		"anos_academicos": c.AnosAcademicos,
		"periodos":        c.Periodos,
		"codigo_academia": c.CodigoAcademia,
		"status":          c.Status,
		"version":         c.Version,
	}
}

// academiaDoAtor devolve o id da academia (UUID) quando quem age é a própria
// academia; a coluna academia_id da auditoria é UUID e o código da academia
// (ex.: ACA_01) já consta no snapshot de antes/depois.
func academiaDoAtor(a db.AuditContext) string {
	if a.UserType == "academia" {
		if _, err := uuid.Parse(a.UserID); err == nil {
			return a.UserID
		}
	}
	return ""
}

func jsonOuNulo(itens []string) any {
	if len(itens) == 0 {
		return nil
	}
	b, _ := json.Marshal(itens)
	return string(b)
}

// Criar insere o curso e audita ("CursoCriado"). A academia precisa existir
// (não há mais chave estrangeira para a projeção da academia: ver migration 143).
func (r *CursosRepo) Criar(ctx context.Context, audit db.AuditContext, c *Curso) error {
	return r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		if err := exigirAcademia(ctx, tx, c.CodigoAcademia); err != nil {
			return err
		}
		anos, _ := json.Marshal(c.AnosAcademicos)
		err := tx.QueryRowxContext(ctx, `
			INSERT INTO projection_cursos
				(id, nome, type, modelo, anos_academicos, periodos, codigo_academia, status, version)
			VALUES ($1, $2, $3, NULLIF($4, ''), $5::jsonb, $6::jsonb, $7, $8, 1)
			RETURNING created_at, updated_at, version`,
			c.ID, c.Nome, c.Type, c.Modelo, string(anos), jsonOuNulo(c.Periodos), c.CodigoAcademia, c.Status,
		).Scan(&c.CreatedAt, &c.UpdatedAt, &c.Version)
		if err != nil {
			return fmt.Errorf("inserir curso: %w", err)
		}
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeCurso, EntidadeID: c.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "CursoCriado", Depois: c.snapshot(),
		})
	})
}

func exigirAcademia(ctx context.Context, tx *sqlx.Tx, codigo string) error {
	var existe bool
	if err := tx.QueryRowxContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM projection_academias WHERE codigo_academia = $1)`, codigo).Scan(&existe); err != nil {
		return fmt.Errorf("verificar academia: %w", err)
	}
	if !existe {
		return regra("academia %s não encontrada", codigo)
	}
	return nil
}

func carregarCursoTx(ctx context.Context, tx *sqlx.Tx, id uuid.UUID) (*Curso, error) {
	var (
		c       Curso
		anos    []byte
		per     []byte
		deleted sql.NullTime
	)
	err := tx.QueryRowxContext(ctx, `
		SELECT id, nome, type, COALESCE(modelo, ''), anos_academicos, periodos, codigo_academia,
		       status, created_at, updated_at, version, deleted_at
		FROM projection_cursos WHERE id = $1 FOR UPDATE`, id).Scan(
		&c.ID, &c.Nome, &c.Type, &c.Modelo, &anos, &per, &c.CodigoAcademia,
		&c.Status, &c.CreatedAt, &c.UpdatedAt, &c.Version, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNaoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("carregar curso: %w", err)
	}
	_ = json.Unmarshal(anos, &c.AnosAcademicos)
	if per != nil {
		_ = json.Unmarshal(per, &c.Periodos)
	}
	if c.Periodos == nil {
		c.Periodos = []string{}
	}
	if deleted.Valid {
		t := deleted.Time
		c.DeletedAt = &t
	}
	return &c, nil
}

func gravarCursoTx(ctx context.Context, tx *sqlx.Tx, c *Curso) error {
	anos, _ := json.Marshal(c.AnosAcademicos)
	var deletedAt any
	if c.DeletedAt != nil {
		deletedAt = c.DeletedAt.UTC()
	}
	return tx.QueryRowxContext(ctx, `
		UPDATE projection_cursos
		SET nome = $2, modelo = NULLIF($3, ''), anos_academicos = $4::jsonb, periodos = $5::jsonb,
		    status = $6, deleted_at = $7, version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING updated_at, version`,
		c.ID, c.Nome, c.Modelo, string(anos), jsonOuNulo(c.Periodos), c.Status, deletedAt,
	).Scan(&c.UpdatedAt, &c.Version)
}

// Atualizar carrega o curso com bloqueio de linha, aplica fn (uma regra do
// domínio: Ativar, Desativar, AtualizarDados...), grava e audita — tudo na
// mesma transação. Se fn recusar, nada é gravado e o erro (um *ErroRegra)
// volta como está. fn pode ser repetida em caso de conflito de serialização.
func (r *CursosRepo) Atualizar(ctx context.Context, audit db.AuditContext, id uuid.UUID, acao, motivo string, fn func(*Curso) error) (*Curso, error) {
	var resultado *Curso
	err := r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		c, err := carregarCursoTx(ctx, tx, id)
		if err != nil {
			return err
		}
		antes := c.snapshot()
		if err := fn(c); err != nil {
			return err
		}
		if err := gravarCursoTx(ctx, tx, c); err != nil {
			return fmt.Errorf("atualizar curso: %w", err)
		}
		resultado = c
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeCurso, EntidadeID: c.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: acao, Antes: antes, Depois: c.snapshot(), Motivo: motivo,
		})
	})
	if err != nil {
		return nil, err
	}
	return resultado, nil
}

// DeletarComMaterias deleta o curso e, na MESMA transação, as turmas e as
// matérias dele que ainda não foram deletadas (cascata). Cada turma, cada
// matéria e o curso geram a sua linha de auditoria. Se alguma turma ainda
// estiver ativa ou com estudantes, se alguma matéria ainda estiver ativa, ou
// se o curso não puder ser deletado, nada é gravado. Devolve o curso, os nomes
// das matérias deletadas e os códigos das turmas deletadas.
func (r *CursosRepo) DeletarComMaterias(ctx context.Context, audit db.AuditContext, id uuid.UUID, motivo string) (*Curso, []string, []string, error) {
	var (
		curso   *Curso
		nomesMs []string
		turmas  []string
	)
	err := r.client.ComAuditoria(ctx, audit, func(tx *sqlx.Tx, aud *db.Auditor) error {
		nomesMs = nil
		turmas = nil
		c, err := carregarCursoTx(ctx, tx, id)
		if err != nil {
			return err
		}
		antes := c.snapshot()
		nomeCurso := c.Nome

		// Turmas primeiro: precisam estar inativas e sem estudantes.
		turmaIDs, err := idsTurmasDoCursoTx(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, tid := range turmaIDs {
			t, err := carregarTurmaTx(ctx, tx, tid)
			if err != nil {
				return err
			}
			if t.Status == StatusDeletado {
				continue
			}
			if t.Status == StatusAtivo {
				return regra("desative todas as turmas vinculadas antes de deletar o curso (turma ativa: %s)", t.CodigoTurma)
			}
			if len(t.Estudantes) > 0 {
				return regra("turma %s ainda possui estudantes vinculados", t.CodigoTurma)
			}
			tAntes := t.snapshot()
			if err := t.Deletar(); err != nil {
				return &ErroRegra{Err: fmt.Errorf("turma '%s': %s", t.CodigoTurma, err.Error())}
			}
			if err := gravarTurmaTx(ctx, tx, t); err != nil {
				return fmt.Errorf("deletar turma '%s': %w", t.CodigoTurma, err)
			}
			if err := aud.Registrar(ctx, db.EntradaAuditoria{
				EntidadeTipo: EntidadeTurma, EntidadeID: t.ID.String(), AcademiaID: academiaDoAtor(audit),
				Acao: "TurmaDeletada", Antes: tAntes, Depois: t.snapshot(),
				Motivo: fmt.Sprintf("cascata: curso %s deletado", nomeCurso),
			}); err != nil {
				return err
			}
			turmas = append(turmas, t.CodigoTurma)
		}

		ids, err := idsMateriasDoCursoTx(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, mid := range ids {
			m, err := carregarMateriaTx(ctx, tx, mid)
			if err != nil {
				return err
			}
			if m.Status == StatusDeletado {
				continue
			}
			mAntes := m.snapshot()
			if err := m.Deletar(); err != nil {
				return &ErroRegra{Err: fmt.Errorf("matéria '%s': %s", m.Nome, err.Error())}
			}
			if err := gravarMateriaTx(ctx, tx, m); err != nil {
				return fmt.Errorf("deletar matéria '%s': %w", m.Nome, err)
			}
			if err := aud.Registrar(ctx, db.EntradaAuditoria{
				EntidadeTipo: EntidadeMateria, EntidadeID: m.ID.String(), AcademiaID: academiaDoAtor(audit),
				Acao: "MateriaDeletada", Antes: mAntes, Depois: m.snapshot(),
				Motivo: fmt.Sprintf("cascata: curso %s deletado", nomeCurso),
			}); err != nil {
				return err
			}
			nomesMs = append(nomesMs, m.Nome)
		}

		if err := c.Deletar(); err != nil {
			return err
		}
		if err := gravarCursoTx(ctx, tx, c); err != nil {
			return fmt.Errorf("deletar curso: %w", err)
		}
		curso = c
		return aud.Registrar(ctx, db.EntradaAuditoria{
			EntidadeTipo: EntidadeCurso, EntidadeID: c.ID.String(), AcademiaID: academiaDoAtor(audit),
			Acao: "CursoDeletado", Antes: antes, Depois: c.snapshot(), Motivo: motivo,
		})
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return curso, nomesMs, turmas, nil
}
