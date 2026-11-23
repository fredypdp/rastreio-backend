package main

import (
	"sort"
	"strings"
	"testing"

	"spuri/internal/middleware"
	"spuri/internal/permissoes"
)

// Varredura: toda rota registrada em setupRouter() precisa ter uma decisão
// explícita sobre o secretário. A porta é "negar por padrão", então uma rota
// nova não classificada simplesmente fica inacessível ao secretário — este
// teste obriga a decidir de propósito (e a registrar a decisão em
// internal/permissoes/rotas.go) em vez de descobrir em produção.
func TestPermissoesTodaRotaTemDecisaoParaOSecretario(t *testing.T) {
	router := setupRouter()
	var semDecisao []string
	for _, r := range router.Routes() {
		if r.Method == "OPTIONS" || r.Method == "HEAD" {
			continue
		}
		if _, ok := permissoes.RegraDaRota(r.Method, r.Path); ok {
			continue
		}
		if middleware.RotaDeContaPropria(r.Method, r.Path) || permissoes.PertenceAOutroPerfil(r.Path) {
			continue
		}
		if rotaPublicaConhecida(r.Method, r.Path) {
			continue
		}
		semDecisao = append(semDecisao, r.Method+" "+r.Path)
	}
	sort.Strings(semDecisao)
	if len(semDecisao) > 0 {
		t.Fatalf("%d rota(s) sem decisão para o secretário (classifique em internal/permissoes/rotas.go):\n  %s",
			len(semDecisao), strings.Join(semDecisao, "\n  "))
	}
}

// Toda rota classificada precisa existir de fato no router (evita regras órfãs
// depois de renomear/remover rotas) e toda permissão citada precisa estar no catálogo.
func TestPermissoesRegrasApontamParaRotasEPermissoesExistentes(t *testing.T) {
	router := setupRouter()
	registradas := map[string]bool{}
	for _, r := range router.Routes() {
		registradas[r.Method+" "+r.Path] = true
	}
	for _, chave := range permissoes.RotasClassificadas() {
		if !registradas[chave] {
			t.Errorf("regra para rota que não existe no router: %q", chave)
			continue
		}
		parts := strings.SplitN(chave, " ", 2)
		regra, _ := permissoes.RegraDaRota(parts[0], parts[1])
		if regra.Tipo == permissoes.ExigePermissao && !permissoes.Existe(regra.Permissao) {
			t.Errorf("%s exige permissão fora do catálogo: %q", chave, regra.Permissao)
		}
	}
}

// Rotas públicas ou de autenticação opcional: o secretário nunca passa por
// elas na porta (não usam AuthMiddleware), então não precisam de decisão.
func rotaPublicaConhecida(method, path string) bool {
	return publicasConhecidas[method+" "+path]
}

var publicasConhecidas = map[string]bool{
	// Autenticação opcional (visão ampliada; o secretário vê o que a academia vê).
	"GET /academia/curso/:id": true, "GET /academia/cursos": true,
	"GET /academia/servico/:codigo_academia/servicos-extras": true,
	"GET /academias": true, "GET /consultar-academia/:codigo": true,
	// Do administrador (RequireAdmin): o secretário nunca alcança.
	"GET /comunicacao/remetentes": true, "POST /comunicacao/remetentes": true,
	// Públicas.
	"GET /health": true, "POST /academia/cadastro": true, "POST /bootstrap": true,
	"POST /email/recuperar-senha/:token": true, "POST /email/recuperar-senha/solicitar": true,
	"POST /email/verificar-email/:token": true, "POST /login": true,
	"POST /login/dois-fatores/reenviar": true, "POST /login/dois-fatores/verificar": true,
}
