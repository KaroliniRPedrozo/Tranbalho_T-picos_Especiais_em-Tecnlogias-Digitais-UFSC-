package rest

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/observability"
)

func TestRequestID(t *testing.T) {
	// O handler devolve no corpo o id que enxergou no contexto.
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(observability.RequestIDFrom(r.Context())))
	}))

	t.Run("gera um id quando o cliente nao envia", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		id := rec.Header().Get("X-Request-ID")
		assert.Len(t, id, 24)
		assert.Equal(t, id, rec.Body.String(), "o contexto deve ter o mesmo id do cabecalho")
	})

	t.Run("preserva o id enviado pelo cliente", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-ID", "abc-123")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		assert.Equal(t, "abc-123", rec.Header().Get("X-Request-ID"))
		assert.Equal(t, "abc-123", rec.Body.String())
	})

	t.Run("ids gerados sao diferentes entre requisicoes", func(t *testing.T) {
		a, b := httptest.NewRecorder(), httptest.NewRecorder()
		h.ServeHTTP(a, httptest.NewRequest(http.MethodGet, "/", nil))
		h.ServeHTTP(b, httptest.NewRequest(http.MethodGet, "/", nil))
		assert.NotEqual(t, a.Header().Get("X-Request-ID"), b.Header().Get("X-Request-ID"))
	})
}

func TestRecoverer_PanicViraErro500(t *testing.T) {
	h := Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	require.NotPanics(t, func() {
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "internal_error", errorCode(t, rec))
	assert.NotContains(t, rec.Body.String(), "boom", "o detalhe do panic nao pode vazar")
}

// accessLog roda uma requisicao pelo StructuredLogger e devolve a linha de log.
func accessLog(t *testing.T, handler http.HandlerFunc) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))

	h := RequestID(StructuredLogger(base)(handler))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/algo", nil))

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	return line
}

func TestStructuredLogger_NivelDependeDoStatus(t *testing.T) {
	casos := []struct {
		nome   string
		status int
		nivel  string
	}{
		{"200 vira INFO", http.StatusOK, "INFO"},
		{"404 vira WARN", http.StatusNotFound, "WARN"},
		{"500 vira ERROR", http.StatusInternalServerError, "ERROR"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			line := accessLog(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(c.status) })

			assert.Equal(t, c.nivel, line["level"])
			assert.EqualValues(t, c.status, line["status"])
			assert.Equal(t, "GET", line["method"])
			assert.Equal(t, "/algo", line["path"])
			assert.NotEmpty(t, line["request_id"])
		})
	}
}

func TestStructuredLogger_SemWriteHeaderAssume200(t *testing.T) {
	line := accessLog(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ola")) })

	assert.EqualValues(t, 200, line["status"])
	assert.EqualValues(t, 3, line["bytes"])
}
