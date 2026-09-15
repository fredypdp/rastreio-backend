# Documentos extra: suportar múltiplos anos acadêmicos + expor consulta pública para a matrícula

## Antes de começar (leia isto primeiro)

Esta tarefa já foi **inteiramente planejada, implementada e validada** por quem escreveu este documento — não em teoria, mas rodando de verdade: PostgreSQL real com as 134 migrations existentes aplicadas do zero, mais esta nova migration 127 testada com dados simulando produção (insert, backfill, constraints, containment query), e o backend inteiro compilado com `go build ./...`, `go vet ./...` e a suíte de testes relevante (`internal/domain/aggregates`, `internal/projections`, testes de `documento_extra`/`documento_download` em `internal/handlers`) — tudo passando.

**Sua tarefa é só aplicar as mudanças abaixo exatamente como estão descritas.** Não há necessidade de repensar a abordagem, buscar alternativas, ou validar se "faz sentido" — isso já foi feito. Onde o documento diz "substitua o arquivo inteiro por", é para substituir o arquivo inteiro (não fazer merge parcial). Onde diz "aplique este diff", é uma mudança cirúrgica pequena.

Você **não tem acesso a PostgreSQL nem Docker neste ambiente** (isso já é sabido) — não tente subir um banco para validar a migration. Rode apenas `go build ./...`, `go vet ./...` e `go test ./...` (os pacotes que não dependem de banco vão passar; os que dependem de `DATABASE_URL` vão falhar por falta de conexão, o que é esperado e **não é um problema desta mudança** — não tente contornar isso).

## Contexto — o que está sendo resolvido e por quê

1. **Multi-select de "Ano acadêmico" em Documentos Extra.** Hoje `DocumentoExtra` guarda um único `ano_academico` (string). O frontend precisa permitir selecionar **vários** anos acadêmicos para a mesma definição de documento (ex.: o mesmo "Atestado médico" aplicável à 9ª Classe E ao 1º Ano Médio ao mesmo tempo). Isso exige mudar o campo para um array — `anos_academicos` — seguindo exatamente o mesmo padrão que este projeto já usa em `projection_cursos.anos_academicos` e `projection_materias.anos_academicos` (JSONB + índice GIN, ver migrations `011_cursos_nivel_to_anos_academicos.sql` e `014_materias_nivel_to_anos_academicos.sql`).

2. **Bug real encontrado durante a investigação: documentos extra nunca aparecem na tela pública de matrícula (`/matricula`), para nenhuma academia.** A tela pública tenta buscar os documentos extra da academia escolhida via `ListarDocumentosExtraAcademia`, mas esse handler exige uma sessão de academia autenticada (`academy(c)` → `middleware.GetUserID(c)`). Um visitante anônimo preenchendo a matrícula pública nunca tem essa sessão, então a chamada sempre falha (401/403) e o frontend engole o erro silenciosamente (`.catch(() => setDocumentosExtra([]))`). Resultado: a funcionalidade de documentos extra na matrícula pública está morta desde que foi implementada. A correção é expor uma rota pública dedicada, **exatamente no mesmo padrão** já usado para o mesmo problema em Serviços Extras (`ListarServicosExtrasPublico`, rota `/academia/servico/:codigo_academia/servicos-extras`).

Estas duas mudanças estão no mesmo documento porque são a mesma superfície de código (mesmo aggregate, mesma projection, mesmos handlers) e devem ser aplicadas juntas, no mesmo deploy.

---

## Decisões de design já tomadas (não precisa reavaliar)

- **`anos_academicos` é `JSONB` (array de strings), não um novo tipo de coluna.** Mesma escolha de `projection_cursos`/`projection_materias`. Índice `GIN` para permitir a query de containment (`@>`).
- **O campo `nivel` (antes `VARCHAR(20)` singular) é removido**, não substituído por um array. Motivo: com múltiplos anos por definição, um único documento pode abranger mais de um nível (ex.: fundamental + médio simultaneamente — a própria tela de configurações permite isso, com duas secções de botões). Um campo "nivel" (singular) deixaria de fazer sentido. O nível de cada ano específico é derivado sob demanda via `aggregates.NivelDoAnoAcademico(ano)` sempre que necessário (nunca foi persistido como fonte de verdade independente — já era derivado de `ano_academico` antes desta mudança).
- **A constraint de unicidade muda de `(academia, ano_academico, rótulo)` para `(academia, rótulo)`.** Antes, cada linha cobria um único ano, então dava para ter "Atestado médico" duas vezes (uma por ano). Agora que uma linha pode cobrir vários anos, checar sobreposição de arrays via constraint de banco exigiria um mecanismo que este projeto não usa em nenhum outro lugar (EXCLUDE com operador de overlap não é suportado nativamente para JSONB no Postgres sem extensões adicionais). Decisão: simplificar para unicidade por rótulo apenas, enquanto ativo. Quem precisar do mesmo rótulo para conjuntos de anos diferentes com regras diferentes usa um rótulo distinto (ex.: "Atestado médico (Ensino Médio)").
- **A nova rota pública fica em `/academia/documento/:codigo_academia/documentos-extra`** (não `/academia/:codigo_academia/documentos-extra`). O segmento literal `documento` antes do parâmetro dinâmico é necessário porque o Gin (radix tree do httprouter) não permite misturar um segmento estático e um segmento wildcard/parâmetro na mesma posição da árvore — e já existem vários outros `router.GET("/academia/<algo-estático>", ...)` registrados (`/academia/documentos-extra`, `/academia/cursos`, `/academia/curso/:id`, etc.). É exatamente o mesmo motivo pelo qual a rota pública de Serviços Extras já existente usa `/academia/servico/:codigo_academia/servicos-extras` (segmento `servico` antes do parâmetro). Confirmado lendo o registro de rotas em `cmd/server/main.go` — não altere este path.
- **`DocumentoMatricula.Nivel`/`.AnoAcademico` (o documento efetivamente enviado por UM estudante) continuam singulares.** Não confundir com `DocumentoExtra.AnosAcademicos` (a definição do catálogo, que agora é plural). Um estudante está matriculado em exatamente um ano; o catálogo é que pode se aplicar a vários anos. Por isso `armazenarDocumentosExtra` passa a receber o ano do ALUNO como parâmetro explícito, em vez de ler de `up.Catalog`.

---

## Passo 1 — Nova migration

Crie o arquivo `migrations/127_documentos_extra_anos_academicos.sql` (o próximo número disponível — a última migration existente é `126_documentos_extra.sql`) com este conteúdo exato:

```sql
-- 127_documentos_extra_anos_academicos.sql
--
-- Permite que um DocumentoExtra se aplique a MAIS DE UM ano_academico
-- simultaneamente (ex.: o mesmo "Atestado médico" exigido tanto na 9ª
-- Classe quanto no 1º Ano Médio), em vez de exigir uma definição separada
-- por ano. Mesmo padrão já usado em projection_cursos.anos_academicos e
-- projection_materias.anos_academicos (ver migrations 011/014): coluna
-- JSONB (array de strings) + índice GIN para containment (@>).
--
-- A coluna "nivel" é removida: com múltiplos anos por definição, um único
-- documento pode abranger mais de um nível (ex.: fundamental + médio), o
-- que torna uma coluna "nivel" (singular) inconsistente. O nível de cada
-- ano específico continua disponível sob demanda via
-- aggregates.NivelDoAnoAcademico(ano) sempre que necessário — nunca foi
-- persistido como fonte de verdade (já era derivado de ano_academico).
BEGIN;

ALTER TABLE projection_documentos_extra ADD COLUMN anos_academicos JSONB;

-- Backfill: cada linha existente vira um array de um único elemento,
-- preservando 100% dos dados já cadastrados.
UPDATE projection_documentos_extra
SET anos_academicos = jsonb_build_array(ano_academico)
WHERE anos_academicos IS NULL;

ALTER TABLE projection_documentos_extra ALTER COLUMN anos_academicos SET NOT NULL;
ALTER TABLE projection_documentos_extra ADD CONSTRAINT chk_documentos_extra_anos_academicos_nao_vazio
    CHECK (jsonb_typeof(anos_academicos) = 'array' AND jsonb_array_length(anos_academicos) > 0);

CREATE INDEX IF NOT EXISTS idx_documentos_extra_anos_academicos
    ON projection_documentos_extra USING GIN (anos_academicos);

-- A unicidade antiga era por (academia, ano_academico, rótulo) — fazia
-- sentido quando cada linha cobria só um ano. Com anos_academicos array,
-- checar sobreposição de arrays via constraint de banco exigiria um
-- mecanismo que este projeto não usa em nenhum outro lugar (EXCLUDE com
-- operador de overlap não é suportado nativamente para JSONB). Decisão:
-- simplificar a unicidade para (academia, rótulo) enquanto ativo — um
-- mesmo rótulo não pode ter duas definições ATIVAS ao mesmo tempo para a
-- mesma academia, independentemente dos anos. Quem precisar do mesmo
-- rótulo para conjuntos de anos diferentes com regras diferentes deve usar
-- um rótulo distinto (ex.: "Atestado médico (Ensino Médio)").
DROP INDEX IF EXISTS ux_documentos_extra_rotulo_ano_ativo;
CREATE UNIQUE INDEX IF NOT EXISTS ux_documentos_extra_rotulo_ativo
    ON projection_documentos_extra (codigo_academia, lower(rotulo))
    WHERE ativo = true;

ALTER TABLE projection_documentos_extra DROP COLUMN ano_academico;
ALTER TABLE projection_documentos_extra DROP COLUMN nivel;

COMMIT;
```

Esta migration já foi executada do zero (junto com as 126 anteriores, em um banco limpo) e testada com dados simulando produção: backfill preservou 100% dos dados, array vazio é rejeitado pela CHECK constraint, rótulo duplicado ativo é rejeitado pelo índice único, e a query de containment (`@>`) retorna corretamente os documentos aplicáveis a um ano específico mesmo quando a definição cobre vários anos.

---

## Passo 2 — Substituir `internal/domain/aggregates/documento_extra.go`

Substitua o arquivo **inteiro** por este conteúdo:

```go
package aggregates

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"spuri/internal/utils"
)

// TiposDocumentoExtraPermitidos define os únicos valores aceitos para o
// campo Tipo de um DocumentoExtra — mesma ideia de conjunto fechado usada em
// TipoDetalhePersonalizado (ver Módulo de Serviços Extras).
var TiposDocumentoExtraPermitidos = map[string]bool{"pdf": true, "jpg": true}

// DocumentoExtra é a definição, mantida pela academia, de um documento
// adicional (além dos fixos do sistema: BI, cédula, declaração, certificados)
// exigido — ou apenas oferecido — no cadastro direto e na solicitação de
// matrícula do estudante, para um ou mais anos_academicos.
//
// Cada estudante que efetivamente envia o arquivo correspondente tem esse
// envio registrado em projection_estudantes.documentos, na chave
// "documento_extra.<DocumentoExtra.ID>" — este aggregate guarda apenas a
// DEFINIÇÃO (rótulo, tipo exigido, obrigatoriedade, escopo), não os arquivos.
type DocumentoExtra struct {
	BaseAggregate

	CodigoAcademia string
	Rotulo         string
	Tipo           string // "pdf" | "jpg"
	Obrigatorio    bool
	// AnosAcademicos: uma mesma definição pode se aplicar a mais de um ano
	// (ex.: "9_ano_fundamental" e "1_ano_medio" ao mesmo tempo). Não existe
	// mais um campo "Nivel" (singular): como uma definição pode abranger
	// anos de níveis diferentes, o nível de CADA ano específico é derivado
	// sob demanda via NivelDoAnoAcademico(ano) sempre que necessário —
	// nunca foi persistido como fonte de verdade independente.
	AnosAcademicos []string // ex.: []string{"6_ano_fundamental", "1_ano_medio"}
	Ativo          bool
	CriadoPor      uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func NewDocumentoExtra() *DocumentoExtra {
	return &DocumentoExtra{BaseAggregate: BaseAggregate{ID: uuid.New(), Version: 0, UncommittedEvents: []DomainEvent{}}, Ativo: true}
}

func (d *DocumentoExtra) GetType() string { return "DocumentoExtra" }

// ============================================================================
// Eventos
// ============================================================================

type DocumentoExtraCriadoEvent struct {
	BaseEvent
	CodigoAcademia string
	Rotulo         string
	Tipo           string
	Obrigatorio    bool
	AnosAcademicos []string
	CriadoPor      uuid.UUID
	CreatedAt      time.Time
}

func (e *DocumentoExtraCriadoEvent) GetPayload() interface{} { return e }
func (e *DocumentoExtraCriadoEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

type DocumentoExtraAtualizadoEvent struct {
	BaseEvent
	Rotulo         string
	Tipo           string
	Obrigatorio    bool
	AnosAcademicos []string
	AtualizadoPor  uuid.UUID
	UpdatedAt      time.Time
}

func (e *DocumentoExtraAtualizadoEvent) GetPayload() interface{} { return e }
func (e *DocumentoExtraAtualizadoEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

type DocumentoExtraDesativadoEvent struct {
	BaseEvent
	DesativadoPor uuid.UUID
	UpdatedAt     time.Time
}

func (e *DocumentoExtraDesativadoEvent) GetPayload() interface{} { return e }
func (e *DocumentoExtraDesativadoEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

type DocumentoExtraReativadoEvent struct {
	BaseEvent
	ReativadoPor uuid.UUID
	UpdatedAt    time.Time
}

func (e *DocumentoExtraReativadoEvent) GetPayload() interface{} { return e }
func (e *DocumentoExtraReativadoEvent) ToJSON() ([]byte, error) { return json.Marshal(e) }

// ============================================================================
// Apply
// ============================================================================

func (d *DocumentoExtra) Apply(event DomainEvent) error {
	switch event.GetEventType() {
	case "DocumentoExtraCriado":
		return d.applyCriado(event)
	case "DocumentoExtraAtualizado":
		return d.applyAtualizado(event)
	case "DocumentoExtraDesativado":
		d.Ativo = false
		return nil
	case "DocumentoExtraReativado":
		d.Ativo = true
		return nil
	default:
		return fmt.Errorf("tipo de evento desconhecido para DocumentoExtra: %s", event.GetEventType())
	}
}

// ============================================================================
// Validação interna compartilhada
// ============================================================================

// validarAnoAcademicoDocumentoExtra valida um único valor de ano_academico
// usando os mesmos validadores por sufixo já usados para os documentos fixos
// de matrícula (solicitacao_matricula.go).
func validarAnoAcademicoDocumentoExtra(anoAcademico string) error {
	switch {
	case strings.HasSuffix(anoAcademico, "_ano_fundamental"):
		return utils.ValidateAnoFundamental(anoAcademico)
	case strings.HasSuffix(anoAcademico, "_ano_medio"):
		return utils.ValidateAnoMedio(anoAcademico)
	case strings.HasSuffix(anoAcademico, "_ano_superior"):
		return utils.ValidateAnoSuperior(anoAcademico)
	default:
		return fmt.Errorf("use o formato N_ano_fundamental, N_ano_medio ou N_ano_superior")
	}
}

// validarCamposDocumentoExtra normaliza e valida rotulo/tipo/anos_academicos.
// anos_academicos precisa ter pelo menos um valor, cada valor é validado
// individualmente e duplicados são removidos; o resultado é ordenado para
// que a igualdade de dois slices (ex.: comparação em testes) não dependa da
// ordem em que o cliente enviou os valores.
func validarCamposDocumentoExtra(rotulo, tipo string, anosAcademicos []string) (string, string, []string, error) {
	rotulo = strings.TrimSpace(rotulo)
	if rotulo == "" {
		return "", "", nil, fmt.Errorf("rotulo é obrigatório")
	}
	if len(rotulo) > 150 {
		return "", "", nil, fmt.Errorf("rotulo deve ter no máximo 150 caracteres")
	}
	tipo = strings.TrimSpace(strings.ToLower(tipo))
	if !TiposDocumentoExtraPermitidos[tipo] {
		return "", "", nil, fmt.Errorf("tipo deve ser 'pdf' ou 'jpg'")
	}
	if len(anosAcademicos) == 0 {
		return "", "", nil, fmt.Errorf("selecione ao menos um ano_academico")
	}
	vistos := make(map[string]bool, len(anosAcademicos))
	normalizados := make([]string, 0, len(anosAcademicos))
	for _, ano := range anosAcademicos {
		ano = strings.TrimSpace(ano)
		if ano == "" {
			continue
		}
		if err := validarAnoAcademicoDocumentoExtra(ano); err != nil {
			return "", "", nil, fmt.Errorf("ano_academico inválido (%s): %w", ano, err)
		}
		if vistos[ano] {
			continue
		}
		vistos[ano] = true
		normalizados = append(normalizados, ano)
	}
	if len(normalizados) == 0 {
		return "", "", nil, fmt.Errorf("selecione ao menos um ano_academico")
	}
	sort.Strings(normalizados)
	return rotulo, tipo, normalizados, nil
}

// ============================================================================
// Comandos
// ============================================================================

func (d *DocumentoExtra) Criar(codigoAcademia, rotulo, tipo string, obrigatorio bool, anosAcademicos []string, criadoPor uuid.UUID) error {
	if strings.TrimSpace(codigoAcademia) == "" {
		return fmt.Errorf("codigo_academia é obrigatório")
	}
	rotulo, tipo, anosAcademicos, err := validarCamposDocumentoExtra(rotulo, tipo, anosAcademicos)
	if err != nil {
		return err
	}
	e := &DocumentoExtraCriadoEvent{
		BaseEvent:      BaseEvent{EventType: "DocumentoExtraCriado", AggregateID: d.ID},
		CodigoAcademia: codigoAcademia,
		Rotulo:         rotulo,
		Tipo:           tipo,
		Obrigatorio:    obrigatorio,
		AnosAcademicos: anosAcademicos,
		CriadoPor:      criadoPor,
		CreatedAt:      time.Now(),
	}
	d.RaiseEvent(e)
	return d.Apply(e)
}

// Atualizar edita rotulo/tipo/obrigatorio/anos_academicos de uma definição já
// existente. A mudança vale apenas prospectivamente: documentos já enviados
// por estudantes mantêm o path/tipo com que foram gravados — trocar o tipo
// aqui não invalida nem re-exige arquivos antigos, apenas passa a valer para
// os próximos cadastros/matrículas.
func (d *DocumentoExtra) Atualizar(rotulo, tipo string, obrigatorio bool, anosAcademicos []string, atualizadoPor uuid.UUID) error {
	rotulo, tipo, anosAcademicos, err := validarCamposDocumentoExtra(rotulo, tipo, anosAcademicos)
	if err != nil {
		return err
	}
	e := &DocumentoExtraAtualizadoEvent{
		BaseEvent:      BaseEvent{EventType: "DocumentoExtraAtualizado", AggregateID: d.ID},
		Rotulo:         rotulo,
		Tipo:           tipo,
		Obrigatorio:    obrigatorio,
		AnosAcademicos: anosAcademicos,
		AtualizadoPor:  atualizadoPor,
		UpdatedAt:      time.Now(),
	}
	d.RaiseEvent(e)
	return d.Apply(e)
}

// Desativar é uma remoção lógica: preserva o histórico (definição continua
// existindo e continua referenciada por documentos já enviados), apenas para
// de ser exigida/oferecida em novos cadastros e matrículas a partir de agora.
func (d *DocumentoExtra) Desativar(p uuid.UUID) error {
	if !d.Ativo {
		return fmt.Errorf("documento extra já está inativo")
	}
	e := &DocumentoExtraDesativadoEvent{BaseEvent: BaseEvent{EventType: "DocumentoExtraDesativado", AggregateID: d.ID}, DesativadoPor: p, UpdatedAt: time.Now()}
	d.RaiseEvent(e)
	return d.Apply(e)
}

func (d *DocumentoExtra) Reativar(p uuid.UUID) error {
	if d.Ativo {
		return fmt.Errorf("documento extra já está ativo")
	}
	e := &DocumentoExtraReativadoEvent{BaseEvent: BaseEvent{EventType: "DocumentoExtraReativado", AggregateID: d.ID}, ReativadoPor: p, UpdatedAt: time.Now()}
	d.RaiseEvent(e)
	return d.Apply(e)
}

// ============================================================================
// Apply handlers
// ============================================================================

func (d *DocumentoExtra) applyCriado(e DomainEvent) error {
	b, err := json.Marshal(e.GetPayload())
	if err != nil {
		return err
	}
	var p DocumentoExtraCriadoEvent
	if err = json.Unmarshal(b, &p); err != nil {
		return err
	}
	d.CodigoAcademia = p.CodigoAcademia
	d.Rotulo = p.Rotulo
	d.Tipo = p.Tipo
	d.Obrigatorio = p.Obrigatorio
	d.AnosAcademicos = p.AnosAcademicos
	d.Ativo = true
	d.CriadoPor = p.CriadoPor
	d.CreatedAt = p.CreatedAt
	d.UpdatedAt = p.CreatedAt
	return nil
}

func (d *DocumentoExtra) applyAtualizado(e DomainEvent) error {
	b, err := json.Marshal(e.GetPayload())
	if err != nil {
		return err
	}
	var p DocumentoExtraAtualizadoEvent
	if err = json.Unmarshal(b, &p); err != nil {
		return err
	}
	d.Rotulo = p.Rotulo
	d.Tipo = p.Tipo
	d.Obrigatorio = p.Obrigatorio
	d.AnosAcademicos = p.AnosAcademicos
	d.UpdatedAt = p.UpdatedAt
	return nil
}
```

---

## Passo 3 — Substituir `internal/domain/aggregates/documento_extra_test.go`

Substitua o arquivo **inteiro** por este conteúdo (cobre: rótulo vazio, tipo inválido, `anos_academicos` vazio/nulo, ano inválido dentro do conjunto, múltiplos anos cruzando fundamental+médio, deduplicação de anos repetidos, ensino superior, e o ciclo Atualizar/Desativar/Reativar):

```go
package aggregates

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestDocumentoExtraLifecycle(t *testing.T) {
	d := NewDocumentoExtra()

	// rotulo vazio
	if err := d.Criar("ACA", "", "pdf", true, []string{"6_ano_fundamental"}, uuid.New()); err == nil {
		t.Fatal("rotulo vazio aceito")
	}
	// tipo invalido
	if err := d.Criar("ACA", "Foto 3x4", "png", true, []string{"6_ano_fundamental"}, uuid.New()); err == nil {
		t.Fatal("tipo invalido aceito")
	}
	// anos_academicos vazio
	if err := d.Criar("ACA", "Foto 3x4", "jpg", true, nil, uuid.New()); err == nil {
		t.Fatal("anos_academicos vazio aceito")
	}
	if err := d.Criar("ACA", "Foto 3x4", "jpg", true, []string{}, uuid.New()); err == nil {
		t.Fatal("anos_academicos vazio (slice vazio) aceito")
	}
	// ano_academico invalido
	if err := d.Criar("ACA", "Foto 3x4", "jpg", true, []string{"99_ano_fundamental"}, uuid.New()); err == nil {
		t.Fatal("ano_academico invalido aceito")
	}
	// um ano válido misturado com um inválido: o conjunto inteiro é rejeitado
	if err := d.Criar("ACA", "Foto 3x4", "jpg", true, []string{"6_ano_fundamental", "99_ano_fundamental"}, uuid.New()); err == nil {
		t.Fatal("conjunto com ano invalido aceito")
	}
	// ano_academico em formato desconhecido
	if err := d.Criar("ACA", "Foto 3x4", "jpg", true, []string{"fundamental"}, uuid.New()); err == nil {
		t.Fatal("ano_academico sem sufixo reconhecido aceito")
	}

	if err := d.Criar("ACA", " Foto 3x4 ", "JPG", true, []string{"6_ano_fundamental"}, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if !d.Ativo || d.Rotulo != "Foto 3x4" || d.Tipo != "jpg" || !reflect.DeepEqual(d.AnosAcademicos, []string{"6_ano_fundamental"}) || !d.Obrigatorio {
		t.Fatalf("estado inesperado após Criar: %+v", d)
	}

	// múltiplos anos, inclusive cruzando níveis (fundamental + médio) — a
	// tela de configurações permite isso explicitamente (duas secções de
	// botões, seleção livre em ambas)
	dMulti := NewDocumentoExtra()
	if err := dMulti.Criar("ACA", "Atestado médico", "pdf", true, []string{"1_ano_medio", "9_ano_fundamental"}, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dMulti.AnosAcademicos, []string{"1_ano_medio", "9_ano_fundamental"}) {
		t.Fatalf("anos_academicos deveriam vir ordenados e sem duplicados, obtido %+v", dMulti.AnosAcademicos)
	}

	// duplicados no payload são removidos silenciosamente
	dDup := NewDocumentoExtra()
	if err := dDup.Criar("ACA", "Ficha médica", "pdf", false, []string{"2_ano_medio", "2_ano_medio"}, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dDup.AnosAcademicos, []string{"2_ano_medio"}) {
		t.Fatalf("duplicados deveriam ter sido removidos, obtido %+v", dDup.AnosAcademicos)
	}

	dSuperior := NewDocumentoExtra()
	if err := dSuperior.Criar("ACA", "Comprovativo", "pdf", false, []string{"1_ano_superior"}, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dSuperior.AnosAcademicos, []string{"1_ano_superior"}) {
		t.Fatalf("anos_academicos inesperado: %+v", dSuperior.AnosAcademicos)
	}

	// Atualizar
	if err := d.Atualizar("", "pdf", false, []string{"6_ano_fundamental"}, uuid.New()); err == nil {
		t.Fatal("rotulo vazio aceito em Atualizar")
	}
	if err := d.Atualizar("Foto tipo passe", "pdf", false, []string{"7_ano_fundamental"}, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if d.Rotulo != "Foto tipo passe" || d.Tipo != "pdf" || d.Obrigatorio || !reflect.DeepEqual(d.AnosAcademicos, []string{"7_ano_fundamental"}) {
		t.Fatalf("estado inesperado após Atualizar: %+v", d)
	}

	// Desativar / Reativar
	if err := d.Desativar(uuid.New()); err != nil {
		t.Fatal(err)
	}
	if d.Ativo {
		t.Fatal("deveria estar inativo")
	}
	if err := d.Desativar(uuid.New()); err == nil {
		t.Fatal("dupla desativação aceita")
	}
	if err := d.Reativar(uuid.New()); err != nil {
		t.Fatal(err)
	}
	if !d.Ativo {
		t.Fatal("deveria estar ativo")
	}
	if err := d.Reativar(uuid.New()); err == nil {
		t.Fatal("dupla reativação aceita")
	}
}
```

---

## Passo 4 — Substituir `internal/projections/documento_extra_projection.go`

Substitua o arquivo **inteiro** por este conteúdo:

```go
package projections

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"spuri/internal/db"
)

type DocumentoExtraProjection struct{ client *db.Client }

func NewDocumentoExtraProjection(c *db.Client) *DocumentoExtraProjection {
	return &DocumentoExtraProjection{c}
}
func (p *DocumentoExtraProjection) Name() string { return "documentos_extra" }
func (p *DocumentoExtraProjection) GetLastProcessedEventID() (int64, error) {
	var v int64
	err := p.client.DB().QueryRow(`SELECT last_processed_event_id FROM projection_checkpoints WHERE projection_name=$1`, p.Name()).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}
func (p *DocumentoExtraProjection) UpdateCheckpoint(id int64) error {
	_, err := p.client.DB().Exec(`INSERT INTO projection_checkpoints (projection_name,last_processed_event_id,last_processed_at,events_processed) VALUES($1,$2,CURRENT_TIMESTAMP,1) ON CONFLICT(projection_name) DO UPDATE SET last_processed_event_id=$2,last_processed_at=CURRENT_TIMESTAMP,events_processed=projection_checkpoints.events_processed+1`, p.Name(), id)
	return err
}
func (p *DocumentoExtraProjection) Handle(e db.Event) error {
	if e.AggregateType != "DocumentoExtra" {
		return nil
	}
	switch e.EventType {
	case "DocumentoExtraCriado":
		return p.created(e)
	case "DocumentoExtraAtualizado":
		return p.updated(e)
	case "DocumentoExtraDesativado":
		return p.active(e, false)
	case "DocumentoExtraReativado":
		return p.active(e, true)
	}
	return nil
}
func (p *DocumentoExtraProjection) Rebuild() error {
	if _, err := p.client.DB().Exec(`TRUNCATE projection_documentos_extra CASCADE`); err != nil {
		return err
	}
	rows, err := p.client.DB().Query(`SELECT id,event_id,aggregate_id,aggregate_type,event_type,event_version,payload,metadata,occurred_at,recorded_at,ledger_hash,previous_hash FROM spuri_ledger WHERE aggregate_type='DocumentoExtra' ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var e db.Event
		var prev sql.NullString
		if err = rows.Scan(&e.ID, &e.EventID, &e.AggregateID, &e.AggregateType, &e.EventType, &e.EventVersion, &e.Payload, &e.Metadata, &e.OccurredAt, &e.RecordedAt, &e.LedgerHash, &prev); err != nil {
			return err
		}
		if prev.Valid {
			e.PreviousHash = &prev.String
		}
		if err = p.Handle(e); err != nil {
			return err
		}
	}
	return rows.Err()
}

type DocumentoExtraDTO struct {
	ID             uuid.UUID `json:"id"`
	CodigoAcademia string    `json:"codigo_academia"`
	Rotulo         string    `json:"rotulo"`
	Tipo           string    `json:"tipo"`
	Obrigatorio    bool      `json:"obrigatorio"`
	AnosAcademicos []string  `json:"anos_academicos"`
	Ativo          bool      `json:"ativo"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (p *DocumentoExtraProjection) created(e db.Event) error {
	var x struct {
		CodigoAcademia string
		Rotulo         string
		Tipo           string
		Obrigatorio    bool
		AnosAcademicos []string
		CriadoPor      uuid.UUID
		CreatedAt      time.Time
	}
	if err := json.Unmarshal(e.Payload, &x); err != nil {
		return err
	}
	anosJSON, err := json.Marshal(x.AnosAcademicos)
	if err != nil {
		return err
	}
	_, err = p.client.DB().Exec(
		`INSERT INTO projection_documentos_extra(id,codigo_academia,rotulo,tipo,obrigatorio,anos_academicos,ativo,criado_por,created_at,updated_at,version,last_event_id)
		 VALUES($1,$2,$3,$4,$5,$6,true,$7,$8,$8,$9,$10) ON CONFLICT(id) DO NOTHING`,
		e.AggregateID, x.CodigoAcademia, x.Rotulo, x.Tipo, x.Obrigatorio, anosJSON, x.CriadoPor, x.CreatedAt, e.EventVersion, e.EventID)
	return err
}
func (p *DocumentoExtraProjection) updated(e db.Event) error {
	var x struct {
		Rotulo         string
		Tipo           string
		Obrigatorio    bool
		AnosAcademicos []string
	}
	if err := json.Unmarshal(e.Payload, &x); err != nil {
		return err
	}
	anosJSON, err := json.Marshal(x.AnosAcademicos)
	if err != nil {
		return err
	}
	_, err = p.client.DB().Exec(
		`UPDATE projection_documentos_extra SET rotulo=$1,tipo=$2,obrigatorio=$3,anos_academicos=$4,version=$5,last_event_id=$6,updated_at=CURRENT_TIMESTAMP WHERE id=$7`,
		x.Rotulo, x.Tipo, x.Obrigatorio, anosJSON, e.EventVersion, e.EventID, e.AggregateID)
	return err
}
func (p *DocumentoExtraProjection) active(e db.Event, ativo bool) error {
	_, err := p.client.DB().Exec(`UPDATE projection_documentos_extra SET ativo=$1,version=$2,last_event_id=$3,updated_at=CURRENT_TIMESTAMP WHERE id=$4`, ativo, e.EventVersion, e.EventID, e.AggregateID)
	return err
}
func (p *DocumentoExtraProjection) scan(row interface{ Scan(...interface{}) error }) (*DocumentoExtraDTO, error) {
	var d DocumentoExtraDTO
	var anosJSON []byte
	err := row.Scan(&d.ID, &d.CodigoAcademia, &d.Rotulo, &d.Tipo, &d.Obrigatorio, &anosJSON, &d.Ativo, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if len(anosJSON) > 0 {
		if err := json.Unmarshal(anosJSON, &d.AnosAcademicos); err != nil {
			return nil, fmt.Errorf("decodificar anos_academicos: %w", err)
		}
	}
	return &d, nil
}

const documentoExtraCols = `id,codigo_academia,rotulo,tipo,obrigatorio,anos_academicos,ativo,created_at,updated_at`

func (p *DocumentoExtraProjection) GetByID(id uuid.UUID) (*DocumentoExtraDTO, error) {
	d, err := p.scan(p.client.DB().QueryRow(`SELECT `+documentoExtraCols+` FROM projection_documentos_extra WHERE id=$1`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}
func (p *DocumentoExtraProjection) GetByAcademia(codigo string, ativosOnly bool) ([]DocumentoExtraDTO, error) {
	q := `SELECT ` + documentoExtraCols + ` FROM projection_documentos_extra WHERE codigo_academia=$1`
	if ativosOnly {
		q += ` AND ativo=true`
	}
	q += ` ORDER BY rotulo`
	rows, err := p.client.DB().Query(q, codigo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocumentoExtraDTO{}
	for rows.Next() {
		d, err := p.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listar documentos extra: %w", err)
	}
	return out, nil
}

// GetAtivosPorAnoAcademico retorna as definições ATIVAS desta academia cujo
// anos_academicos CONTÉM o ano_academico informado — usado no cadastro
// direto e na solicitação de matrícula para saber quais documentos extra
// validar/exigir para o ano específico do estudante. Uma mesma definição
// pode aparecer para mais de um ano_academico distinto (ex.: uma definição
// com anos_academicos=["9_ano_fundamental","1_ano_medio"] aparece tanto na
// consulta para "9_ano_fundamental" quanto para "1_ano_medio").
//
// O operador @> (containment) usa o índice GIN idx_documentos_extra_anos_academicos
// — ver migration 127_documentos_extra_anos_academicos.sql.
func (p *DocumentoExtraProjection) GetAtivosPorAnoAcademico(codigo, anoAcademico string) ([]DocumentoExtraDTO, error) {
	anoJSON, err := json.Marshal([]string{anoAcademico})
	if err != nil {
		return nil, err
	}
	rows, err := p.client.DB().Query(
		`SELECT `+documentoExtraCols+` FROM projection_documentos_extra WHERE codigo_academia=$1 AND anos_academicos @> $2::jsonb AND ativo=true ORDER BY rotulo`,
		codigo, anoJSON)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocumentoExtraDTO{}
	for rows.Next() {
		d, err := p.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listar documentos extra por ano acadêmico: %w", err)
	}
	return out, nil
}
```

Note o comentário em `GetAtivosPorAnoAcademico` explicando o uso do operador `@>` (containment) sobre o índice GIN — essa função já foi testada isoladamente contra Postgres real (insert com array de 2 anos, query por cada um dos dois anos separadamente, confirma que o mesmo registro aparece para ambos; query por um terceiro ano não relacionado não retorna nada).

---

## Passo 5 — Substituir `internal/handlers/documento_extra_handlers.go`

Substitua o arquivo **inteiro** por este conteúdo (inclui a nova função pública `ListarDocumentosExtraPublico` no final do arquivo):

```go
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/utils"
)

// validarRotuloDocumentoExtraDisponivel verifica, ANTES de gerar o evento
// (DocumentoExtraCriado/Atualizado/Reativado), se já existe outro documento
// extra ATIVO com o mesmo rótulo (ignorando maiúsculas/minúsculas) para esta
// academia.
//
// Pré-checagem obrigatória pelo mesmo motivo documentado em
// validarNomeCategoriaServicoDisponivel (categoria_servico_handlers.go): a
// unicidade (ux_documentos_extra_rotulo_ativo) é garantida pela PROJEÇÃO, não
// pelo ledger. Sem esta checagem, dois eventos colidentes são aceitos pelo
// ledger e o SEGUNDO trava o checkpoint da projeção "documentos_extra"
// permanentemente ao tentar aplicar o INSERT/UPDATE que viola o índice único
// — confirmado neste projeto via teste manual de ponta a ponta antes desta
// pré-checagem existir.
//
// Desde a migration 127_documentos_extra_anos_academicos.sql, uma definição
// pode se aplicar a VÁRIOS anos_academicos ao mesmo tempo; a unicidade
// deixou de ser por (academia, ano_academico, rótulo) e passou a ser apenas
// por (academia, rótulo) enquanto ativo=true — ver decisão registrada na
// própria migration.
func validarRotuloDocumentoExtraDisponivel(c *gin.Context, codigoAcademia, rotulo string, excluirID *uuid.UUID) error {
	rotulo = strings.TrimSpace(rotulo)
	docs, err := getDocumentosExtraProjection(c).GetByAcademia(codigoAcademia, true)
	if err != nil {
		return fmt.Errorf("erro ao verificar rótulo de documento extra: %v", err)
	}
	for _, existente := range docs {
		if excluirID != nil && existente.ID == *excluirID {
			continue
		}
		if strings.EqualFold(existente.Rotulo, rotulo) {
			return fmt.Errorf("já existe um documento extra ativo com este rótulo")
		}
	}
	return nil
}

type documentoExtraPayload struct {
	Rotulo         string   `json:"rotulo"`
	Tipo           string   `json:"tipo"`
	Obrigatorio    bool     `json:"obrigatorio"`
	AnosAcademicos []string `json:"anos_academicos"`
}

func bindDocumentoExtraPayload(c *gin.Context, r *documentoExtraPayload) error {
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(r); err != nil {
		return fmt.Errorf("dados invalidos")
	}
	return nil
}

func documentoExtraToJSON(doc *aggregates.DocumentoExtra) gin.H {
	return gin.H{
		"id":              doc.GetID(),
		"codigo_academia": doc.CodigoAcademia,
		"rotulo":          doc.Rotulo,
		"tipo":            doc.Tipo,
		"obrigatorio":     doc.Obrigatorio,
		"anos_academicos": doc.AnosAcademicos,
		"ativo":           doc.Ativo,
		"created_at":      doc.CreatedAt,
		"updated_at":      doc.UpdatedAt,
	}
}

func CriarDocumentoExtra(c *gin.Context) {
	var r documentoExtraPayload
	if err := bindDocumentoExtraPayload(c, &r); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	codigo, id, ok := academy(c)
	if !ok {
		return
	}
	// Pré-checagem de unicidade ANTES de gerar o evento — ver comentário em
	// validarRotuloDocumentoExtraDisponivel sobre por que isto é
	// obrigatório (checkpoint da projeção trava permanentemente, para TODAS
	// as academias, se um duplicado chegar a ser aceito no ledger).
	if err := validarRotuloDocumentoExtraDisponivel(c, codigo, r.Rotulo, nil); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	doc := aggregates.NewDocumentoExtra()
	if err := doc.Criar(codigo, r.Rotulo, r.Tipo, r.Obrigatorio, r.AnosAcademicos, id); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := getRepository(c).SaveWithAudit(doc, db.AuditContext{UserID: id.String(), UserType: "academia", IP: c.ClientIP()}); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "documento extra criado com sucesso", "data": documentoExtraToJSON(doc)})
}

func loadDocumentoExtra(c *gin.Context) (*aggregates.DocumentoExtra, uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("ID de documento extra inválido"))
		return nil, uuid.Nil, false
	}
	codigo, user, ok := academy(c)
	if !ok {
		return nil, user, false
	}
	x, err := getRepository(c).Load(id, "DocumentoExtra")
	if err != nil {
		utils.RespondWithNotFoundError(c, "documento extra")
		return nil, user, false
	}
	doc, ok := x.(*aggregates.DocumentoExtra)
	if !ok || doc.CodigoAcademia != codigo {
		utils.RespondWithForbiddenError(c, "documento extra não pertence a esta academia")
		return nil, user, false
	}
	return doc, user, true
}

func AtualizarDocumentoExtra(c *gin.Context) {
	var r documentoExtraPayload
	if err := bindDocumentoExtraPayload(c, &r); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	doc, id, ok := loadDocumentoExtra(c)
	if !ok {
		return
	}
	docID := doc.GetID()
	if err := validarRotuloDocumentoExtraDisponivel(c, doc.CodigoAcademia, r.Rotulo, &docID); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := doc.Atualizar(r.Rotulo, r.Tipo, r.Obrigatorio, r.AnosAcademicos, id); err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err := getRepository(c).SaveWithAudit(doc, db.AuditContext{UserID: id.String(), UserType: "academia", IP: c.ClientIP()}); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "documento extra atualizado com sucesso", "data": documentoExtraToJSON(doc)})
}

func toggleDocumentoExtra(c *gin.Context, ativar bool) {
	doc, id, ok := loadDocumentoExtra(c)
	if !ok {
		return
	}
	var err error
	if ativar {
		docID := doc.GetID()
		if err = validarRotuloDocumentoExtraDisponivel(c, doc.CodigoAcademia, doc.Rotulo, &docID); err != nil {
			utils.RespondWithValidationError(c, err)
			return
		}
		err = doc.Reativar(id)
	} else {
		err = doc.Desativar(id)
	}
	if err != nil {
		utils.RespondWithValidationError(c, err)
		return
	}
	if err = getRepository(c).SaveWithAudit(doc, db.AuditContext{UserID: id.String(), UserType: "academia", IP: c.ClientIP()}); err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": documentoExtraToJSON(doc)})
}
func DesativarDocumentoExtra(c *gin.Context) { toggleDocumentoExtra(c, false) }
func ReativarDocumentoExtra(c *gin.Context)  { toggleDocumentoExtra(c, true) }

func ListarDocumentosExtraAcademia(c *gin.Context) {
	codigo, _, ok := academy(c)
	if !ok {
		return
	}
	ativosOnly := c.Query("ativos") == "true"
	docs, err := getDocumentosExtraProjection(c).GetByAcademia(codigo, ativosOnly)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"documentos_extra": docs, "total": len(docs)})
}

// ListarDocumentosExtraPublico expõe, sem autenticação, o catálogo de
// documentos extra ATIVOS de uma academia pelo codigo_academia na URL —
// mesmo padrão já usado por ListarServicosExtrasPublico
// (servico_extra_handlers.go) para o mesmo tipo de necessidade: a tela
// pública de matrícula (/matricula) precisa saber, em segundo plano e sem
// exigir login, se a academia escolhida tem documentos extra configurados
// para o ano acadêmico que o candidato está a escolher.
//
// Antes desta rota existir, a tela pública chamava
// ListarDocumentosExtraAcademia (que exige sessão de academia via academy(c))
// e recebia sempre 401/403 — silenciosamente ignorado pelo frontend — então
// documentos extra nunca apareciam na matrícula pública, independentemente
// da academia. Ver documento de tarefa para detalhes desta descoberta.
func ListarDocumentosExtraPublico(c *gin.Context) {
	docs, err := getDocumentosExtraProjection(c).GetByAcademia(c.Param("codigo_academia"), true)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"documentos_extra": docs, "total": len(docs)})
}
```

---

## Passo 6 — Editar `internal/handlers/documento_extra_upload.go`

Esta é uma mudança **cirúrgica**, não uma substituição de arquivo inteiro. Aplique este diff (contexto suficiente para localizar exatamente onde):

```diff
diff --git a/internal/handlers/documento_extra_upload.go b/internal/handlers/documento_extra_upload.go
index bb89554..69885ce 100644
--- a/internal/handlers/documento_extra_upload.go
+++ b/internal/handlers/documento_extra_upload.go
@@ -152,7 +152,16 @@ func storagePathDocumentoExtra(baseDir, catalogID, tipo string) (string, string)
 // Documentos (ver documentoEstudantePorCampoEscopo) — chaves com ponto,
 // como a nossa, já são um padrão existente (ex.: "nivel.ano.campo" para
 // declarações), então nenhuma rota nova precisou ser criada.
-func armazenarDocumentosExtra(provider storage.StorageProvider, dir string, enviados map[string]documentoExtraUpload, downloadURL func(campo string) string) (map[string]aggregates.DocumentoMatricula, error) {
+//
+// anoAcademicoAluno é o ano_academico REAL do estudante sendo
+// cadastrado/matriculado (o mesmo valor já usado para resolver `enviados`
+// via GetAtivosPorAnoAcademico) — não confundir com
+// up.Catalog.AnosAcademicos, que desde a migration
+// 127_documentos_extra_anos_academicos.sql é a lista de TODOS os anos aos
+// quais aquela definição de catálogo se aplica, podendo ter mais de um
+// valor. O documento efetivamente enviado por ESTE estudante pertence a
+// exatamente um ano/nível — o do aluno, não o do catálogo.
+func armazenarDocumentosExtra(provider storage.StorageProvider, dir string, enviados map[string]documentoExtraUpload, anoAcademicoAluno string, downloadURL func(campo string) string) (map[string]aggregates.DocumentoMatricula, error) {
 	out := map[string]aggregates.DocumentoMatricula{}
 	for catalogID, up := range enviados {
 		documentoID, storagePath := storagePathDocumentoExtra(dir, catalogID, up.Catalog.Tipo)
@@ -164,8 +173,8 @@ func armazenarDocumentosExtra(provider storage.StorageProvider, dir string, envi
 		out[campo] = aggregates.DocumentoMatricula{
 			DocumentoID:      documentoID,
 			Tipo:             up.Catalog.Tipo,
-			Nivel:            up.Catalog.Nivel,
-			AnoAcademico:     up.Catalog.AnoAcademico,
+			Nivel:            aggregates.NivelDoAnoAcademico(anoAcademicoAluno),
+			AnoAcademico:     anoAcademicoAluno,
 			DocumentoExtraID: catalogID,
 			Path:             stored.Path,
 			FileURL:          stored.FileURL,
```

---

## Passo 7 — Editar `internal/handlers/estudante_handlers.go`

Duas mudanças neste arquivo:

1. Em `registerEstudantePorAcademiaComRequestModo`, a variável `anoAcademicoDocsExtra` era declarada com `:=` **dentro** do bloco `if !pendenteDocumentos { ... }`, o que limitava seu escopo a esse bloco — mas ela precisa ser lida mais abaixo, fora dele, na chamada de `armazenarDocumentosExtra`. Isso é um bug de escopo pré-existente que só se manifesta agora porque `armazenarDocumentosExtra` passa a precisar desse valor explicitamente (antes lia de `up.Catalog`, que não tinha esse problema). A correção: declarar a variável **antes** do `if`, com `var`, e usar `=` (não `:=`) dentro dele.
2. As duas chamadas de `armazenarDocumentosExtra` (uma em `registerEstudantePorAcademiaComRequestModo`, outra em `CompletarDocumentosEstudantePendente`) passam a receber `anoAcademicoDocsExtra` como novo argumento.

Aplique este diff:

```diff
diff --git a/internal/handlers/estudante_handlers.go b/internal/handlers/estudante_handlers.go
index dfde3e8..0581e9c 100644
--- a/internal/handlers/estudante_handlers.go
+++ b/internal/handlers/estudante_handlers.go
@@ -156,8 +156,9 @@ func registerEstudantePorAcademiaComRequestModo(c *gin.Context, req CadastroEstu
 	// o estudante deliberadamente sem nenhum arquivo por enquanto.
 	var catalogoDocsExtra []projections.DocumentoExtraDTO
 	var docsExtraEnviados map[string]documentoExtraUpload
+	var anoAcademicoDocsExtra string
 	if !pendenteDocumentos {
-		anoAcademicoDocsExtra := resolverAnoAcademicoParaDocumentosExtra(stringPtrIfNotBlank(req.AnoEscolar), stringPtrIfNotBlank(req.AnoEscolarMedio), stringPtrIfNotBlank(req.AnoSuperior))
+		anoAcademicoDocsExtra = resolverAnoAcademicoParaDocumentosExtra(stringPtrIfNotBlank(req.AnoEscolar), stringPtrIfNotBlank(req.AnoEscolarMedio), stringPtrIfNotBlank(req.AnoSuperior))
 		if anoAcademicoDocsExtra != "" {
 			catalogoDocsExtra, err = getDocumentosExtraProjection(c).GetAtivosPorAnoAcademico(academia.CodigoAcademia, anoAcademicoDocsExtra)
 			if err != nil {
@@ -242,7 +243,7 @@ func registerEstudantePorAcademiaComRequestModo(c *gin.Context, req CadastroEstu
 		documentos[key] = doc
 	}
 	if len(docsExtraEnviados) > 0 {
-		docsExtraArmazenados, err := armazenarDocumentosExtra(provider, dir, docsExtraEnviados, func(campo string) string {
+		docsExtraArmazenados, err := armazenarDocumentosExtra(provider, dir, docsExtraEnviados, anoAcademicoDocsExtra, func(campo string) string {
 			return estudanteDocumentoDownloadURL(codigoEstudante, campo)
 		})
 		if err != nil {
@@ -1038,7 +1039,7 @@ func CompletarDocumentosEstudantePendente(c *gin.Context) {
 		documentos[key] = doc
 	}
 	if len(docsExtraEnviados) > 0 {
-		docsExtraArmazenados, err := armazenarDocumentosExtra(provider, dir, docsExtraEnviados, func(campo string) string {
+		docsExtraArmazenados, err := armazenarDocumentosExtra(provider, dir, docsExtraEnviados, anoAcademicoDocsExtra, func(campo string) string {
 			return estudanteDocumentoDownloadURL(codigo, campo)
 		})
 		if err != nil {
```

---

## Passo 8 — Editar `internal/handlers/solicitacao_matricula_handlers.go`

Mesma mudança de assinatura da chamada de `armazenarDocumentosExtra` (aqui a variável `anoAcademicoDocsExtra` já está corretamente no escopo da função, não precisa do fix de escopo do passo anterior):

```diff
diff --git a/internal/handlers/solicitacao_matricula_handlers.go b/internal/handlers/solicitacao_matricula_handlers.go
index 91689db..a3c623f 100644
--- a/internal/handlers/solicitacao_matricula_handlers.go
+++ b/internal/handlers/solicitacao_matricula_handlers.go
@@ -221,7 +221,7 @@ func CriarSolicitacaoMatricula(c *gin.Context) {
 		documentos[key] = doc
 	}
 	if len(docsExtraEnviados) > 0 {
-		docsExtraArmazenados, err := armazenarDocumentosExtra(provider, dir, docsExtraEnviados, func(campo string) string {
+		docsExtraArmazenados, err := armazenarDocumentosExtra(provider, dir, docsExtraEnviados, anoAcademicoDocsExtra, func(campo string) string {
 			return solicitacaoDocumentoDownloadURL(codigo, campo)
 		})
 		if err != nil {
```

---

## Passo 9 — Registrar a nova rota pública em `cmd/server/main.go`

Adicione a nova rota logo abaixo da rota pública de serviços extras (mesmo grupo "Rotas públicas com autenticação opcional"):

```diff
diff --git a/cmd/server/main.go b/cmd/server/main.go
index abd7fb1..2244d9b 100644
--- a/cmd/server/main.go
+++ b/cmd/server/main.go
@@ -320,6 +320,7 @@ func setupRouter() *gin.Engine {
 	router.GET("/academias", middleware.OptionalAuthMiddleware(), handlers.ListarTodasAcademias)
 	router.GET("/academia/cursos", middleware.OptionalAuthMiddleware(), handlers.ListarCursos)
 	router.GET("/academia/servico/:codigo_academia/servicos-extras", middleware.OptionalAuthMiddleware(), handlers.ListarServicosExtrasPublico)
+	router.GET("/academia/documento/:codigo_academia/documentos-extra", middleware.OptionalAuthMiddleware(), handlers.ListarDocumentosExtraPublico)
 	router.GET("/academia/curso/:id", middleware.OptionalAuthMiddleware(), handlers.GetCurso)
 	router.GET("/consultar-academia/:codigo", middleware.OptionalAuthMiddleware(), handlers.GetAcademiaPorCodigo)
 
```

---

## Validação (rode nesta ordem)

```bash
go build ./...
go vet ./...
go test ./internal/domain/aggregates/... ./internal/projections/...
go test -run "DocumentoExtra|DocumentoDownload" ./internal/handlers/...
```

Os dois primeiros comandos não precisam de banco de dados e devem terminar **sem nenhuma saída** (sucesso silencioso do `go build`/`go vet`). Os dois últimos, se você não tiver um `DATABASE_URL` configurado apontando para um Postgres real, vão falhar com erro de conexão (`connect: connection refused` ou similar) nos testes que dependem de banco — isso é esperado neste ambiente e não indica um problema do código; quem escreveu este documento já rodou estes mesmos comandos com um Postgres real e todos passaram.

Se `go build` ou `go vet` reportarem qualquer erro, **pare e revise o passo correspondente** — como o código já foi validado, um erro nesta etapa quase sempre indica que um dos blocos acima foi colado parcialmente ou com uma variável de contexto renomeada por engano (ex.: um merge automático de editor mexeu em algo).

## O que NÃO fazer

- Não mexa em `go.mod` / `go.sum`.
- Não tente rodar `go test ./...` no repositório inteiro sem filtro — a suíte completa inclui testes de integração lentos e não relacionados a esta tarefa (financeiro, notas, faltas, etc.) que não precisam rodar aqui.
- Não adicione uma constraint de banco tentando impedir sobreposição de arrays entre `anos_academicos` de rótulos diferentes — não é isso que foi pedido, e a decisão de design (documentada acima) foi deliberadamente simplificar a unicidade para `(academia, rótulo)`.
- Não restaure o campo `nivel` nem tente popular algo equivalente — foi removido de propósito.
- Não altere `armazenarDocumentosExtra` para ler `up.Catalog.AnosAcademicos[0]` como atalho para obter "o" ano — isso reintroduziria o bug de usar o ano do catálogo em vez do ano real do aluno.

## Checklist de aceitação

- [ ] `migrations/127_documentos_extra_anos_academicos.sql` criado com o conteúdo exato do Passo 1.
- [ ] `internal/domain/aggregates/documento_extra.go` substituído; `Nivel` não existe mais no struct; `AnosAcademicos []string` existe.
- [ ] `internal/domain/aggregates/documento_extra_test.go` substituído; `go test ./internal/domain/aggregates/...` passa.
- [ ] `internal/projections/documento_extra_projection.go` substituído; `DocumentoExtraDTO.AnosAcademicos []string` existe; `GetAtivosPorAnoAcademico` usa `@>`.
- [ ] `internal/handlers/documento_extra_handlers.go` substituído; `documentoExtraPayload.AnosAcademicos []string` existe; `ListarDocumentosExtraPublico` existe.
- [ ] `internal/handlers/documento_extra_upload.go`: `armazenarDocumentosExtra` recebe `anoAcademicoAluno string` como novo parâmetro; usa `aggregates.NivelDoAnoAcademico(anoAcademicoAluno)`.
- [ ] `internal/handlers/estudante_handlers.go`: bug de escopo de `anoAcademicoDocsExtra` corrigido (declarado com `var` antes do `if`); as duas chamadas de `armazenarDocumentosExtra` passam o novo argumento.
- [ ] `internal/handlers/solicitacao_matricula_handlers.go`: chamada de `armazenarDocumentosExtra` passa o novo argumento.
- [ ] `cmd/server/main.go`: rota `GET /academia/documento/:codigo_academia/documentos-extra` registrada, apontando para `handlers.ListarDocumentosExtraPublico`.
- [ ] `go build ./...` e `go vet ./...` sem erros.
- [ ] `go test ./internal/domain/aggregates/... ./internal/projections/...` passa.
