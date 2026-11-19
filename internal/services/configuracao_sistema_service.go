package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/projections"
	"spuri/internal/sysconfig"
)

// MascaraSegredo é tudo o que a API mostra de um segredo. Nenhum pedaço do
// valor (nem os últimos caracteres) sai em resposta ou em log.
const MascaraSegredo = "********"

// ErrConfiguracaoSemValorNoBanco: tentativa de remover o que não está gravado.
var ErrConfiguracaoSemValorNoBanco = errors.New("não há valor gravado no banco para esta configuração")

// ConfiguracaoSistemaView é uma configuração como a tela de administração a vê.
// Valor é o valor efetivo e fica nil para segredo (que só tem Mascara).
type ConfiguracaoSistemaView struct {
	Chave             string     `json:"chave"`
	Grupo             string     `json:"grupo"`
	Descricao         string     `json:"descricao"`
	Tipo              string     `json:"tipo"`
	Aplicacao         string     `json:"aplicacao"`
	Observacao        string     `json:"observacao,omitempty"`
	Exemplo           string     `json:"exemplo,omitempty"`
	Opcoes            []string   `json:"opcoes,omitempty"`
	Inteiro           bool       `json:"inteiro,omitempty"`
	Min               *float64   `json:"min,omitempty"`
	Max               *float64   `json:"max,omitempty"`
	Segredo           bool       `json:"segredo"`
	Origem            string     `json:"origem"`
	DefinidaNoBanco   bool       `json:"definida_no_banco"`
	Valor             *string    `json:"valor"`
	Mascara           string     `json:"mascara,omitempty"`
	AtualizadoEm      *time.Time `json:"atualizado_em,omitempty"`
	AtualizadoPorID   *uuid.UUID `json:"atualizado_por_id,omitempty"`
	AtualizadoPorNome *string    `json:"atualizado_por_nome,omitempty"`
}

// ConfiguracaoSistemaService grava e lista as configurações do sistema.
type ConfiguracaoSistemaService struct {
	client *db.Client
	repo   *db.AggregateRepository
	proj   *projections.ConfiguracaoSistemaProjection
}

func NovaConfiguracaoSistemaService(client *db.Client, repo *db.AggregateRepository) *ConfiguracaoSistemaService {
	return &ConfiguracaoSistemaService{client: client, repo: repo, proj: projections.NewConfiguracaoSistemaProjection(client)}
}

// Listar devolve todas as configurações aprovadas, na ordem da lista, com o
// valor efetivo (banco → ambiente) e a origem. Antes de montar, relê o banco
// para refletir alterações feitas por outra instância.
func (s *ConfiguracaoSistemaService) Listar(ctx context.Context) ([]ConfiguracaoSistemaView, error) {
	if err := sysconfig.Recarregar(ctx); err != nil {
		log.Printf("[sysconfig] listar: não foi possível reler o banco: %v", err)
	}
	linhas, err := s.proj.Listar()
	if err != nil {
		return nil, err
	}
	defs := sysconfig.Definicoes()
	out := make([]ConfiguracaoSistemaView, 0, len(defs))
	for _, d := range defs {
		out = append(out, montarView(d, linhas[d.Chave]))
	}
	return out, nil
}

func montarView(d sysconfig.Definicao, l projections.ConfiguracaoSistemaLinha) ConfiguracaoSistemaView {
	efetivo, origem := sysconfig.Valor(d.Chave)
	v := ConfiguracaoSistemaView{
		Chave: d.Chave, Grupo: d.Grupo, Descricao: d.Descricao, Tipo: string(d.Tipo),
		Aplicacao: string(d.Aplicacao), Observacao: d.Observacao, Exemplo: d.Exemplo,
		Opcoes: d.Opcoes, Inteiro: d.Inteiro, Min: d.Min, Max: d.Max,
		Segredo: d.Segredo(), Origem: string(origem), DefinidaNoBanco: origem == sysconfig.OrigemBanco,
	}
	if d.Segredo() {
		if origem != sysconfig.OrigemNaoDefinida {
			v.Mascara = MascaraSegredo
		}
	} else if origem != sysconfig.OrigemNaoDefinida {
		v.Valor = &efetivo
	}
	if l.Chave != "" {
		t := l.AtualizadoEm
		v.AtualizadoEm, v.AtualizadoPorID, v.AtualizadoPorNome = &t, l.PorID, l.PorNome
	}
	return v
}

func (s *ConfiguracaoSistemaService) carregarAgregado(chave string) (*aggregates.ConfiguracaoSistema, error) {
	id := aggregates.ConfiguracaoSistemaAggregateID(chave)
	existe, err := s.repo.Exists(id)
	if err != nil {
		return nil, err
	}
	if !existe {
		a := aggregates.NewConfiguracaoSistema()
		a.SetID(id)
		return a, nil
	}
	loaded, err := s.repo.Load(id, "ConfiguracaoSistema")
	if err != nil {
		return nil, err
	}
	a, ok := loaded.(*aggregates.ConfiguracaoSistema)
	if !ok {
		return nil, fmt.Errorf("tipo inesperado ao carregar ConfiguracaoSistema")
	}
	return a, nil
}

// Definir valida e grava o valor da chave. Devolve a configuração já com o
// novo valor efetivo. Erros esperados: sysconfig.ErrNaoAprovada e
// *sysconfig.ErroValidacao.
func (s *ConfiguracaoSistemaService) Definir(ctx context.Context, chave, valor string, admin uuid.UUID, ip string) (ConfiguracaoSistemaView, error) {
	def, ok := sysconfig.Buscar(chave)
	if !ok {
		return ConfiguracaoSistemaView{}, sysconfig.ErrNaoAprovada
	}
	norm, err := def.Validar(valor)
	if err != nil {
		return ConfiguracaoSistemaView{}, err
	}
	agg, err := s.carregarAgregado(def.Chave)
	if err != nil {
		return ConfiguracaoSistemaView{}, err
	}
	audit := db.AuditContext{UserID: admin.String(), UserType: "admin", IP: ip}

	if def.Segredo() {
		cifrado, err := sysconfig.Cifrar(def.Chave, norm)
		if err != nil {
			return ConfiguracaoSistemaView{}, err
		}
		anterior, tinha, err := s.lerCifrado(ctx, def.Chave)
		if err != nil {
			return ConfiguracaoSistemaView{}, err
		}
		if err := s.gravarCifrado(ctx, def.Chave, cifrado); err != nil {
			return ConfiguracaoSistemaView{}, err
		}
		if err := agg.Definir(def.Chave, string(def.Tipo), true, "", admin); err != nil {
			s.restaurarCifrado(ctx, def.Chave, anterior, tinha)
			return ConfiguracaoSistemaView{}, err
		}
		if err := s.repo.SaveWithAudit(agg, audit); err != nil {
			s.restaurarCifrado(ctx, def.Chave, anterior, tinha)
			return ConfiguracaoSistemaView{}, err
		}
	} else {
		if err := agg.Definir(def.Chave, string(def.Tipo), false, norm, admin); err != nil {
			return ConfiguracaoSistemaView{}, err
		}
		if err := s.repo.SaveWithAudit(agg, audit); err != nil {
			return ConfiguracaoSistemaView{}, err
		}
	}
	return s.depoisDeGravar(ctx, def, agg.ID)
}

// Remover tira o valor do banco; a chave volta a ser lida do ambiente.
func (s *ConfiguracaoSistemaService) Remover(ctx context.Context, chave string, admin uuid.UUID, ip string) (ConfiguracaoSistemaView, error) {
	def, ok := sysconfig.Buscar(chave)
	if !ok {
		return ConfiguracaoSistemaView{}, sysconfig.ErrNaoAprovada
	}
	agg, err := s.carregarAgregado(def.Chave)
	if err != nil {
		return ConfiguracaoSistemaView{}, err
	}
	if !agg.Definida {
		return ConfiguracaoSistemaView{}, ErrConfiguracaoSemValorNoBanco
	}
	if err := agg.Remover(admin); err != nil {
		return ConfiguracaoSistemaView{}, err
	}
	audit := db.AuditContext{UserID: admin.String(), UserType: "admin", IP: ip}
	if err := s.repo.SaveWithAudit(agg, audit); err != nil {
		return ConfiguracaoSistemaView{}, err
	}
	if def.Segredo() {
		// O leitor já ignora segredo cuja projeção está sem definida, então uma
		// falha aqui não expõe nada; só deixa texto cifrado órfão.
		if _, err := s.client.DB().ExecContext(ctx, `DELETE FROM configuracao_sistema_segredos WHERE chave = $1`, def.Chave); err != nil {
			log.Printf("[sysconfig] segredo %s removido mas o cofre não foi limpo: %v", def.Chave, err)
		}
	}
	return s.depoisDeGravar(ctx, def, agg.ID)
}

// depoisDeGravar aplica o último evento à projeção na hora (o gerenciador a
// reaplica depois sem efeito, pela guarda de versão), recarrega o cache desta
// instância e devolve a configuração atualizada.
func (s *ConfiguracaoSistemaService) depoisDeGravar(ctx context.Context, def sysconfig.Definicao, aggID uuid.UUID) (ConfiguracaoSistemaView, error) {
	if hist, err := s.repo.GetEventHistory(aggID); err != nil || len(hist) == 0 {
		log.Printf("[sysconfig] %s: não foi possível ler o evento recém-gravado (a projeção será aplicada pelo gerenciador): %v", def.Chave, err)
	} else if err := s.proj.Handle(hist[len(hist)-1]); err != nil {
		log.Printf("[sysconfig] %s: projeção não aplicada na hora (o gerenciador aplica em seguida): %v", def.Chave, err)
	}
	if err := sysconfig.Recarregar(ctx); err != nil {
		log.Printf("[sysconfig] %s: cache não recarregado (renova no próximo vencimento): %v", def.Chave, err)
	}
	linhas, err := s.proj.Listar()
	if err != nil {
		return ConfiguracaoSistemaView{}, err
	}
	return montarView(def, linhas[def.Chave]), nil
}

func (s *ConfiguracaoSistemaService) lerCifrado(ctx context.Context, chave string) (string, bool, error) {
	var c string
	err := s.client.DB().QueryRowContext(ctx, `SELECT cifrado FROM configuracao_sistema_segredos WHERE chave = $1`, chave).Scan(&c)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return c, err == nil, err
}

func (s *ConfiguracaoSistemaService) gravarCifrado(ctx context.Context, chave, cifrado string) error {
	_, err := s.client.DB().ExecContext(ctx, `
		INSERT INTO configuracao_sistema_segredos (chave, cifrado, atualizado_em) VALUES ($1,$2,CURRENT_TIMESTAMP)
		ON CONFLICT (chave) DO UPDATE SET cifrado = EXCLUDED.cifrado, atualizado_em = CURRENT_TIMESTAMP`, chave, cifrado)
	return err
}

// restaurarCifrado desfaz a gravação do cofre quando o evento não foi salvo.
func (s *ConfiguracaoSistemaService) restaurarCifrado(ctx context.Context, chave, anterior string, tinha bool) {
	var err error
	if tinha {
		err = s.gravarCifrado(ctx, chave, anterior)
	} else {
		_, err = s.client.DB().ExecContext(ctx, `DELETE FROM configuracao_sistema_segredos WHERE chave = $1`, chave)
	}
	if err != nil {
		log.Printf("[sysconfig] não foi possível restaurar o cofre de %s após falha ao gravar o evento: %v", chave, err)
	}
}
