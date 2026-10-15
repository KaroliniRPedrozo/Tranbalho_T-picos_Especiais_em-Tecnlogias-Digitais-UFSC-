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

// main so chama run() e transforma o erro em codigo de saida 1.
func main() {
	if err := run(); err != nil {
		slog.Error("servico encerrado com erro", slog.String("erro", err.Error()))
		os.Exit(1)
	}
}

// run monta todas as pecas do servico (config, log, banco, rotas) e sobe o
// servidor HTTP. Devolve erro em vez de chamar os.Exit, o que facilita testar.
func run() error {
	// 1. Configuracao via variaveis de ambiente.
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// 2. Logger estruturado (JSON) como padrao do processo.
	log := observability.NewLogger(cfg.ServiceName, cfg.Env, cfg.LogLevel)
	slog.SetDefault(log)

	log.Info("iniciando servico",
		slog.String("version", version),
		slog.String("http_port", cfg.HTTPPort),
	)

	// Contexto cancelado por SIGINT/SIGTERM: o Docker manda SIGTERM no stop.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 3. Conexao com o Postgres (com tentativas).
	pool, err := storage.Connect(ctx, cfg.DatabaseURL, cfg.DBConnectRetry, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	// 4. Cria/atualiza as tabelas antes de aceitar requisicoes.
	migCtx, cancelMig := context.WithTimeout(ctx, 30*time.Second)
	defer cancelMig()
	if err := migrations.Apply(migCtx, pool, log); err != nil {
		return err
	}

	// 5. Injeta as dependencias: repositorio (banco) e client ViaCEP (API
	// externa) entram no roteador.
	repo := storage.NewEventRepository(pool)

	handler := rest.NewRouter(rest.RouterDeps{
		Logger:      log,
		EventStore:  repo,
		Addresses:   viacep.New(cfg.ViaCEPURL, cfg.ViaCEPTimeout),
		Pinger:      repo,
		ServiceName: cfg.ServiceName,
		Version:     version,
	})

	// 6. Servidor HTTP com timeouts, para nao ficar preso a clientes lentos.
	server := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// 7. Sobe o servidor em outra goroutine para o main continuar escutando
	// o sinal de encerramento.
	serverErr := make(chan error, 1)
	go func() {
		log.Info("servidor http ouvindo", slog.String("addr", server.Addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// 8. Espera o servidor falhar ou o sinal de parada chegar.
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
