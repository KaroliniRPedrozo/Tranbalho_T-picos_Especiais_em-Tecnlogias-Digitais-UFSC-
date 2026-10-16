package observability

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLevel(t *testing.T) {
	casos := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"DEBUG":   slog.LevelDebug,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"info":    slog.LevelInfo,
		"":        slog.LevelInfo,
		"xyz":     slog.LevelInfo,
	}
	for texto, esperado := range casos {
		assert.Equal(t, esperado, parseLevel(texto), "nivel %q", texto)
	}
}

func TestRequestID_Contexto(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "", RequestIDFrom(ctx), "sem id no contexto devolve vazio")

	ctx = WithRequestID(ctx, "req-1")
	assert.Equal(t, "req-1", RequestIDFrom(ctx))
}

func TestLogger_Contexto(t *testing.T) {
	assert.Same(t, slog.Default(), LoggerFrom(context.Background()),
		"sem logger no contexto cai no default")

	l := NewLogger("svc", "test", "info")
	ctx := WithLogger(context.Background(), l)
	assert.Same(t, l, LoggerFrom(ctx))
}

func TestNewLogger_RespeitaONivel(t *testing.T) {
	l := NewLogger("svc", "test", "warn")
	assert.False(t, l.Enabled(context.Background(), slog.LevelInfo))
	assert.True(t, l.Enabled(context.Background(), slog.LevelWarn))
}
