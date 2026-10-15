package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/clients/viacep"
	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/config"
	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/observability"
	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/storage"
	"github.com/KaroliniRPedrozo/ticketing/services/events/internal/transport/rest"
	"github.com/KaroliniRPedrozo/ticketing/services/events/migrations"
)

// version e sobrescrito no build via -ldflags.
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("servico encerrado com erro", slog.String("erro", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := observability.NewLogger(cfg.ServiceName, cfg.Env, cfg.LogLevel)
	slog.SetDefault(log)

	log.Info("iniciando servico",
		slog.String("version", version),
		slog.String("http_port", cfg.HTTPPort),
	)

	// Contexto cancelado por SIGINT/SIGTERM: o Docker manda SIGTERM no stop.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := storage.Connect(ctx, cfg.DatabaseURL, cfg.DBConnectRetry, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	migCtx, cancelMig := context.WithTimeout(ctx, 30*time.Second)
	defer cancelMig()
	if err := migrations.Apply(migCtx, pool, log); err != nil {
		return err
	}

	repo := storage.NewEventRepository(pool)

	handler := rest.NewRouter(rest.RouterDeps{
		Logger:      log,
		EventStore:  repo,
		Addresses:   viacep.New(cfg.ViaCEPURL, cfg.ViaCEPTimeout),
		Pinger:      repo,
		ServiceName: cfg.ServiceName,
		Version:     version,
	})

	server := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info("servidor http ouvindo", slog.String("addr", server.Addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		log.Info("sinal de encerramento recebido, drenando conexoes")
	}

	// Graceful shutdown: para de aceitar conexoes novas e espera as em voo.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}

	log.Info("servico encerrado com sucesso")
	return nil
}
