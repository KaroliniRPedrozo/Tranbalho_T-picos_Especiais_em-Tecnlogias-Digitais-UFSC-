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
	r := chi.NewRouter()

	// Middlewares, na ordem: id da requisicao, log de acesso, recuperacao de panic.
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
	h := NewEventHandler(deps.EventStore)

	r.Route("/api/v1/events", func(r chi.Router) {
		r.Post("/", h.Create)
		r.Get("/", h.List)
		r.Get("/{id}", h.Get)
		r.Put("/{id}", h.Update)
		r.Delete("/{id}", h.Delete)
		r.Post("/{id}/publish", h.Publish)
		r.Post("/{id}/cancel", h.Cancel)
	})

	// Rota de consulta de CEP (usa a API externa ViaCEP).
	ah := NewAddressHandler(deps.Addresses)
	r.Get("/api/v1/addresses/{cep}", ah.Get)

	// Respostas padrao para rota inexistente e metodo nao permitido.
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "route_not_found", "rota nao existe", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "metodo nao permitido nesta rota", nil)
	})

	return r
}
