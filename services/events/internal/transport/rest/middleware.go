package rest

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/observability"
)

const headerRequestID = "X-Request-ID"

// statusRecorder captura o status code para o log de acesso.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// RequestID aceita um correlation id vindo do cliente ou gera um novo. Esse
// mesmo id sera repassado aos outros microsservicos na fase gRPC.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(headerRequestID)
		if id == "" {
			id = newID()
		}

		w.Header().Set(headerRequestID, id)
		ctx := observability.WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// StructuredLogger coloca um logger ja enriquecido no contexto e emite uma
// linha JSON por requisicao concluida.
func StructuredLogger(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			reqLog := base.With(
				slog.String("request_id", observability.RequestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)
			ctx := observability.WithLogger(r.Context(), reqLog)

			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r.WithContext(ctx))

			if rec.status == 0 {
				rec.status = http.StatusOK
			}

			attrs := []any{
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("user_agent", r.UserAgent()),
			}

			switch {
			case rec.status >= 500:
				reqLog.Error("requisicao concluida", attrs...)
			case rec.status >= 400:
				reqLog.Warn("requisicao concluida", attrs...)
			default:
				reqLog.Info("requisicao concluida", attrs...)
			}
		})
	}
}

// Recoverer evita que um panic derrube o processo inteiro e devolve 500 no
// formato de erro padrao da API.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log := observability.LoggerFrom(r.Context())
				log.Error("panic recuperado", slog.Any("panic", rec))
				writeError(w, r, http.StatusInternalServerError, "internal_error", "erro interno inesperado", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "req-fallback"
	}
	return hex.EncodeToString(b)
}
