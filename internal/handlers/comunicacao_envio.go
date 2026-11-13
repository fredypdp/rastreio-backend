package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"spuri/internal/db"
	"spuri/internal/domain/aggregates"
	"spuri/internal/projections"
)

// Tarefa 123 (SEC-04) — envio padronizado de SMS.
//
// Toda a lógica de envio (provedor padrão, fallback automático para o outro
// provedor e registo da mensagem) vivia dentro do handler EnviarMensagemComunicacao,
// acoplada ao *gin.Context. Foi extraída, sem mudança de lógica, para esta função
// única, chamada pelo handler e pelo envio de credenciais. Não existe emissor paralelo.

// atorComunicacao identifica quem figura como remetente da mensagem. O esquema
// (migrations/124_comunicacao_mensagens.sql) só aceita 'admin' ou 'academia'.
type atorComunicacao struct {
	ID             uuid.UUID
	Tipo           string // "admin" | "academia"
	CodigoAcademia string // obrigatório se Tipo == "academia"; vazio se "admin"
}

// enviarViaProvedor é o envio efetivo ao provedor. É uma variável apenas para
// os testes substituírem a chamada de rede; em produção é sempre
// enviarViaProvedorComunicacao.
var enviarViaProvedor = enviarViaProvedorComunicacao

// erroEntradaComunicacao marca os erros causados pela entrada (destinatário ou
// conteúdo inválidos), para o handler os devolver como 422 e não como 500.
type erroEntradaComunicacao struct{ error }

func (e erroEntradaComunicacao) Unwrap() error { return e.error }

// errProvedorPadraoNaoConfigurado — nenhum provedor padrão foi definido ainda.
var errProvedorPadraoNaoConfigurado = errors.New("nenhum provedor padrão foi configurado ainda — um administrador FPP precisa de definir o provedor padrão antes de enviar mensagens")

// enviarSMSComunicacao é o ÚNICO caminho de envio de SMS: normaliza o
// destinatário, valida o conteúdo, tenta o provedor padrão e depois o outro, e
// regista a mensagem (sucesso ou falha) com SaveWithAudit.
//
// ctx governa os pedidos HTTP aos provedores (o handler passa o contexto da
// requisição; os envios em segundo plano passam um contexto desligado dela).
// c só serve para resolver as dependências (repositório, projeções, banco).
//
// Erros devolvidos (a mensagem NÃO é registada neles): destinatário/conteúdo
// inválidos, errProvedorPadraoNaoConfigurado, falhas de banco. Quando todos os
// provedores falham, devolve-se a mensagem registada com Status == falhou e erro nil.
func enviarSMSComunicacao(ctx context.Context, c *gin.Context, ator atorComunicacao, destinatarioBruto, conteudoBruto string, audit db.AuditContext) (*aggregates.MensagemComunicacao, error) {
	destinatario, err := normalizarDestinatarioComunicacao(destinatarioBruto)
	if err != nil {
		return nil, erroEntradaComunicacao{err}
	}
	conteudo := strings.TrimSpace(conteudoBruto)
	if conteudo == "" {
		return nil, erroEntradaComunicacao{fmt.Errorf("conteudo é obrigatório")}
	}
	if len(conteudo) > conteudoMensagemComunicacaoMaxLen {
		return nil, erroEntradaComunicacao{fmt.Errorf("conteudo excede o limite de %d caracteres", conteudoMensagemComunicacaoMaxLen)}
	}

	var provedorPadrao sql.NullString
	if err := getDbClient(c).DB().QueryRow(`SELECT provedor_padrao FROM projection_comunicacao_config WHERE id = 1`).Scan(&provedorPadrao); err != nil {
		return nil, err
	}
	if !provedorPadrao.Valid || provedorPadrao.String == "" {
		return nil, errProvedorPadraoNaoConfigurado
	}

	ordemProvedores := []string{provedorPadrao.String, outroProvedorComunicacao(provedorPadrao.String)}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	var provedorTentado2, provedorUtilizado, mensagemExternaID string
	detalhes := []aggregates.TentativaEnvioComunicacao{}

	for i, provedor := range ordemProvedores {
		if i == 1 {
			provedorTentado2 = provedor
		}
		credenciais, err := getRemetentesComunicacaoProjection(c).GetCredenciaisByProvedor(provedor)
		if err != nil {
			detalhes = append(detalhes, aggregates.TentativaEnvioComunicacao{Provedor: provedor, Sucesso: false, ErroMensagem: "falha ao consultar remetente configurado"})
			continue
		}
		if credenciais == nil {
			detalhes = append(detalhes, aggregates.TentativaEnvioComunicacao{Provedor: provedor, Sucesso: false, ErroMensagem: "nenhum remetente configurado para este provedor"})
			continue
		}
		externalID, sendErr := enviarViaProvedor(ctx, provedor, credenciais, destinatario, conteudo)
		if sendErr != nil {
			detalhes = append(detalhes, aggregates.TentativaEnvioComunicacao{Provedor: provedor, Sucesso: false, ErroMensagem: sendErr.Error()})
			continue
		}
		detalhes = append(detalhes, aggregates.TentativaEnvioComunicacao{Provedor: provedor, Sucesso: true, MensagemExternaID: externalID})
		provedorUtilizado = provedor
		mensagemExternaID = externalID
		break
	}

	status := aggregates.StatusMensagemComunicacaoFalhou
	if provedorUtilizado != "" {
		status = aggregates.StatusMensagemComunicacaoEnviada
	}

	msg := aggregates.NewMensagemComunicacao()
	if err := msg.Registrar(destinatario, conteudo, provedorPadrao.String, provedorTentado2, provedorUtilizado, status, mensagemExternaID, detalhes, ator.ID, ator.Tipo, ator.CodigoAcademia); err != nil {
		return nil, err
	}
	if err := getRepository(c).SaveWithAudit(msg, audit); err != nil {
		return nil, err
	}
	return msg, nil
}

// SubstituirEnvioSMSParaTeste troca o envio efetivo ao provedor de SMS por fn e
// devolve a função que restaura o original. Existe apenas para os testes de
// integração (inclusive os do pacote main), que não podem alcançar a rede.
func SubstituirEnvioSMSParaTeste(fn func(ctx context.Context, provedor, destinatario, conteudo string) (string, error)) (restaurar func()) {
	anterior := enviarViaProvedor
	enviarViaProvedor = func(ctx context.Context, provedor string, _ *projections.RemetenteComunicacaoCredenciais, destinatario, conteudo string) (string, error) {
		return fn(ctx, provedor, destinatario, conteudo)
	}
	return func() { enviarViaProvedor = anterior }
}

// EnvioRecuperacaoSincronoParaTeste faz o e-mail de recuperação de senha sair
// dentro da própria requisição (em produção sai numa goroutine). Devolve a
// função que restaura o comportamento original. Somente para testes.
func EnvioRecuperacaoSincronoParaTeste() (restaurar func()) {
	anterior := executarEnvioRecuperacao
	executarEnvioRecuperacao = func(f func()) { f() }
	return func() { executarEnvioRecuperacao = anterior }
}
