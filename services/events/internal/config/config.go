package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config concentra toda a configuracao do servico. Tudo vem de variavel de
// ambiente, que e o que o docker-compose injeta.
type Config struct {
	ServiceName     string
	Env             string
	HTTPPort        string
	DatabaseURL     string
	LogLevel        string
	ShutdownTimeout time.Duration
	DBConnectRetry  int
	ViaCEPURL       string
	ViaCEPTimeout   time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		ServiceName:     getenv("SERVICE_NAME", "events-service"),
		Env:             getenv("APP_ENV", "development"),
		HTTPPort:        getenv("HTTP_PORT", "8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		LogLevel:        getenv("LOG_LEVEL", "info"),
		ShutdownTimeout: getduration("SHUTDOWN_TIMEOUT", 10*time.Second),
		DBConnectRetry:  getint("DB_CONNECT_RETRY", 10),
		ViaCEPURL:       getenv("VIACEP_URL", "https://viacep.com.br"),
		ViaCEPTimeout:   getduration("VIACEP_TIMEOUT", 5*time.Second),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("variavel de ambiente obrigatoria nao definida: DATABASE_URL")
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getint(key string, fallback int) int {
	v, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return v
}

func getduration(key string, fallback time.Duration) time.Duration {
	v, err := time.ParseDuration(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return v
}
