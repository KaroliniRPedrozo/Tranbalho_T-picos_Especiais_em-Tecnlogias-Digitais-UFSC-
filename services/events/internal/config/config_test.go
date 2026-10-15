package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_DatabaseURLObrigatoria(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	_, err := Load()
	assert.Error(t, err)
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("HTTP_PORT", "")
	t.Setenv("VIACEP_TIMEOUT", "")
	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "8080", cfg.HTTPPort)
	assert.Equal(t, 5*time.Second, cfg.ViaCEPTimeout)
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("HTTP_PORT", "9000")
	t.Setenv("VIACEP_URL", "http://localhost:1")
	t.Setenv("VIACEP_TIMEOUT", "2s")
	t.Setenv("DB_CONNECT_RETRY", "abc") // invalido -> default
	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "9000", cfg.HTTPPort)
	assert.Equal(t, "http://localhost:1", cfg.ViaCEPURL)
	assert.Equal(t, 2*time.Second, cfg.ViaCEPTimeout)
	assert.Equal(t, 10, cfg.DBConnectRetry)
}
