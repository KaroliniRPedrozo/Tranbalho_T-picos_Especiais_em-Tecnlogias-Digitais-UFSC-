package rest

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/transport/apidocs"
)

// Pinger e implementado pelo repositorio; usado no /readyz.
type Pinger interface {
	Ping(ctx context.Context) error
}

// RouterDeps reune tudo que o roteador precisa, injetado pelo main.
type RouterDeps struct {
	Logger      *slog.Logger
	EventStore  EventStore
	Addresses   AddressLookup
	Pinger      Pinger
	ServiceName string
	Version     string
}

// NewRouter monta a superficie REST publica do servico. Esta e a unica porta
// exposta para fora do cluster; a conversa entre servicos sera gRPC.
func NewRouter(deps RouterDeps) http.Handler {
	// chi e o roteador: liga cada endereco (URL + metodo) a uma funcao.
	r := chi.NewRouter()

	// Middlewares sao funcoes que rodam em TODA requisicao, antes do handler.
	// Ordem importa: o id nasce primeiro para o log e o panic ja o usarem.
	//   1. RequestID: cria/le o X-Request-ID
	//   2. StructuredLogger: escreve uma linha de log por requisicao
	//   3. Recoverer: se algo der panic, responde 500 em vez de derrubar o servico
	r.Use(RequestID)
	r.Use(StructuredLogger(deps.Logger))
	r.Use(Recoverer)

	// Liveness: o processo esta de pe.
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, map[string]string{
			"status":  "ok",
			"service": deps.ServiceName,
			"version": deps.Version,
		})
	})

	// Readiness: o processo esta de pe E o banco responde.
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := deps.Pinger.Ping(ctx); err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "database_unavailable",
				"banco de dados indisponivel", nil)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]string{"status": "ready"})
	})

	// Documentacao: Swagger UI em /docs e a spec OpenAPI em /openapi.yaml.
	r.Get("/openapi.yaml", apidocs.OpenAPISpec)
	r.Get("/docs", apidocs.SwaggerUI)

	// Rotas REST de eventos (CRUD + publicar/cancelar).
	// O handler recebe o repositorio (EventStore) pronto: o router nao sabe
	// se por tras ha Postgres ou um fake de teste.
	h := NewEventHandler(deps.EventStore)

	// "/api/v1" -> o v1 permite lancar uma v2 no futuro sem quebrar quem usa a v1.
	// {id} e um parametro de URL, lido no handler com chi.URLParam.
	r.Route("/api/v1/events", func(r chi.Router) {
		r.Post("/", h.Create)              // C - cria (201)
		r.Get("/", h.List)                 // R - lista com filtros e paginacao
		r.Get("/{id}", h.Get)              // R - busca um
		r.Put("/{id}", h.Update)           // U - atualiza
		r.Delete("/{id}", h.Delete)        // D - remove (204)
		r.Post("/{id}/publish", h.Publish) // transicao: draft -> published
		r.Post("/{id}/cancel", h.Cancel)   // transicao: qualquer -> cancelled
	})

	// Rota de consulta de CEP (usa a API externa ViaCEP).
	ah := NewAddressHandler(deps.Addresses)
	r.Get("/api/v1/addresses/{cep}", ah.Get)

	// Respostas padrao para rota inexistente (404) e metodo errado (405, ex:
	// POST em /healthz). Sem isso o chi devolveria texto puro, fora do nosso
	// formato JSON de erro.
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "route_not_found", "rota nao existe", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "metodo nao permitido nesta rota", nil)
	})

	return r
}
