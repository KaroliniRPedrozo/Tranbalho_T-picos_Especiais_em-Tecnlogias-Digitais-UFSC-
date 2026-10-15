package observability

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// ctxKey e um tipo proprio para as chaves do contexto, evitando colisao com
// chaves de outros pacotes.
type ctxKey string

const (
	loggerKey    ctxKey = "logger"
	requestIDKey ctxKey = "request_id"
)

// NewLogger devolve um *slog.Logger que escreve JSON em stdout. Logs
// estruturados em stdout sao o formato que qualquer coletor (Loki, ELK,
// docker logs) consegue ler sem parser customizado.
func NewLogger(serviceName, env, level string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(level),
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// padroniza o nome do campo de timestamp
			if a.Key == slog.TimeKey {
				a.Key = "timestamp"
			}
			return a
		},
	})

	return slog.New(handler).With(
		slog.String("service", serviceName),
		slog.String("env", env),
	)
}

// parseLevel converte o texto da configuracao (debug, warn, error) em nivel
// do slog; qualquer outro valor vira info.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// WithLogger guarda o logger no contexto para que as camadas internas
// consigam logar ja com o request_id anexado.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// LoggerFrom recupera o logger do contexto; cai no default se nao houver.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// WithRequestID guarda o id da requisicao no contexto.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFrom devolve o correlation id da requisicao. Na fase 2 esse mesmo
// valor vai ser propagado para os outros servicos via metadata gRPC.
func RequestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}
