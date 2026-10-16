package rest

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/domain"
	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/observability"
)

// TestReadyz mostra a diferenca entre liveness e readiness: o /healthz diz
// "o processo esta vivo", o /readyz diz "posso receber trafego" (o banco
// responde?). Aqui o Pinger falso simula o banco no ar e fora do ar.
func TestReadyz(t *testing.T) {
	novo := func(p Pinger) http.Handler {
		return NewRouter(RouterDeps{
			Logger:     observability.NewLogger("test", "test", "error"),
			EventStore: &fakeStore{},
			Addresses:  fakeLookup{},
			Pinger:     p,
		})
	}

	t.Run("200 quando o banco responde", func(t *testing.T) {
		rec := httptest.NewRecorder()
		novo(fakePinger{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("503 quando o banco cai", func(t *testing.T) {
		rec := httptest.NewRecorder()
		novo(fakePinger{err: assert.AnError}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "database_unavailable", errorCode(t, rec))
	})
}

func TestMetodoNaoPermitido(t *testing.T) {
	rec := do(t, &fakeStore{}, nil, http.MethodPost, "/healthz", "")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "method_not_allowed", errorCode(t, rec))
}

func TestRotaInexistente(t *testing.T) {
	rec := do(t, &fakeStore{}, nil, http.MethodGet, "/nada", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "route_not_found", errorCode(t, rec))
}

func TestRespostaTemRequestID(t *testing.T) {
	rec := do(t, &fakeStore{}, nil, http.MethodGet, "/healthz", "")
	assert.NotEmpty(t, rec.Header().Get("X-Request-ID"))
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
}

func TestUpdate(t *testing.T) {
	url := "/api/v1/events/" + validID

	t.Run("200", func(t *testing.T) {
		rec := do(t, &fakeStore{event: domain.Event{ID: validID}}, nil, http.MethodPut, url, validBody())
		assert.Equal(t, http.StatusOK, rec.Code)
	})
	t.Run("404 evento inexistente", func(t *testing.T) {
		rec := do(t, &fakeStore{err: domain.ErrNotFound}, nil, http.MethodPut, url, validBody())
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
	t.Run("400 id invalido", func(t *testing.T) {
		rec := do(t, &fakeStore{}, nil, http.MethodPut, "/api/v1/events/abc", validBody())
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_id", errorCode(t, rec))
	})
	t.Run("422 corpo reprovado na validacao", func(t *testing.T) {
		rec := do(t, &fakeStore{}, nil, http.MethodPut, url, `{"name":""}`)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	})
}

func TestValidacao_DevolveTodosOsCampos(t *testing.T) {
	rec := do(t, &fakeStore{}, nil, http.MethodPost, "/api/v1/events", `{"name":""}`)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	body := rec.Body.String()
	for _, campo := range []string{"name", "venue", "city", "starts_at", "capacity"} {
		assert.Contains(t, body, `"`+campo+`"`, "o erro deveria citar o campo "+campo)
	}
}

func TestTransitions_Erros(t *testing.T) {
	t.Run("404 ao publicar evento inexistente", func(t *testing.T) {
		rec := do(t, &fakeStore{err: domain.ErrNotFound}, nil, http.MethodPost, "/api/v1/events/"+validID+"/publish", "")
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
	t.Run("400 id invalido ao cancelar", func(t *testing.T) {
		rec := do(t, &fakeStore{}, nil, http.MethodPost, "/api/v1/events/xyz/cancel", "")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("409 publicar evento com data no passado", func(t *testing.T) {
		rec := do(t, &fakeStore{event: domain.Event{Status: domain.StatusDraft}}, nil,
			http.MethodPost, "/api/v1/events/"+validID+"/publish", "")
		assert.Equal(t, http.StatusConflict, rec.Code)
	})
}

func TestList_Sucesso(t *testing.T) {
	rec := do(t, &fakeStore{}, nil, http.MethodGet, "/api/v1/events", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"items"`)
}

func TestDocs(t *testing.T) {
	t.Run("swagger ui em /docs", func(t *testing.T) {
		rec := do(t, &fakeStore{}, nil, http.MethodGet, "/docs", "")
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
		assert.Contains(t, rec.Body.String(), "swagger-ui")
	})
	t.Run("especificacao em /openapi.yaml", func(t *testing.T) {
		rec := do(t, &fakeStore{}, nil, http.MethodGet, "/openapi.yaml", "")
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.True(t, strings.HasPrefix(rec.Body.String(), "openapi: 3"))
	})
}

// Garante que o Swagger nao fica desatualizado: toda rota do roteador precisa
// estar no openapi.yaml e toda rota do openapi.yaml precisa existir no roteador.
//
// Como funciona: chi.Walk percorre o roteador e lista as rotas reais; uma
// expressao regular le o openapi.yaml e lista as rotas documentadas; depois o
// teste compara os dois conjuntos nos dois sentidos.
func TestOpenAPI_BateComAsRotas(t *testing.T) {
	spec, err := os.ReadFile("../apidocs/openapi.yaml")
	require.NoError(t, err)

	// Rotas reais, vistas pelo chi.
	router := NewRouter(RouterDeps{
		Logger:     observability.NewLogger("test", "test", "error"),
		EventStore: &fakeStore{},
		Addresses:  fakeLookup{},
		Pinger:     fakePinger{},
	})
	real := map[string]bool{}
	err = chi.Walk(router.(chi.Routes), func(_ string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(route, "/")
		if route == "" || route == "/docs" || route == "/openapi.yaml" {
			return nil // a propria documentacao nao se descreve
		}
		real[route] = true
		return nil
	})
	require.NoError(t, err)

	// Rotas documentadas: linhas "  /caminho:" dentro de paths.
	documented := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^  (/\S+):\s*$`).FindAllStringSubmatch(string(spec), -1) {
		documented[m[1]] = true
	}

	for route := range real {
		assert.True(t, documented[route], "rota %s existe mas nao esta no openapi.yaml", route)
	}
	for route := range documented {
		assert.True(t, real[route], "rota %s esta no openapi.yaml mas nao existe no roteador", route)
	}
}
