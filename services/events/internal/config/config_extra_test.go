package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_TodosOsPadroes(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	for _, k := range []string{"SERVICE_NAME", "APP_ENV", "LOG_LEVEL", "SHUTDOWN_TIMEOUT", "DB_CONNECT_RETRY", "VIACEP_URL"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "events-service", cfg.ServiceName)
	assert.Equal(t, "development", cfg.Env)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, 10*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, 10, cfg.DBConnectRetry)
	assert.Equal(t, "https://viacep.com.br", cfg.ViaCEPURL)
	assert.Equal(t, "postgres://x", cfg.DatabaseURL)
}

func TestLoad_ValoresValidosSaoUsados(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("SHUTDOWN_TIMEOUT", "3s")
	t.Setenv("DB_CONNECT_RETRY", "5")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 3*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, 5, cfg.DBConnectRetry)
	assert.Equal(t, "debug", cfg.LogLevel)
}

func TestLoad_DuracaoInvalidaUsaPadrao(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("SHUTDOWN_TIMEOUT", "dez segundos")
	t.Setenv("VIACEP_TIMEOUT", "5") // sem unidade, o Go recusa

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 10*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, 5*time.Second, cfg.ViaCEPTimeout)
}
