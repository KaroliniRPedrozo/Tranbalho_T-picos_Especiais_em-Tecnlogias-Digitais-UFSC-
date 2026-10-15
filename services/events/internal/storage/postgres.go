package storage

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect abre o pool e espera o Postgres ficar pronto. O retry existe porque
// o healthcheck do compose cobre o caso normal, mas uma reinicializacao do
// banco em runtime ainda pode pegar o servico no meio do caminho.
func Connect(ctx context.Context, dsn string, attempts int, log *slog.Logger) (*pgxpool.Pool, error) {
	// 1. Le a URL de conexao.
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("DSN invalida: %w", err)
	}

	// 2. Limites do pool de conexoes.
	cfg.MaxConns = 10
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute

	// 3. Cria o pool (ainda nao garante que o banco esta de pe).
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("criando pool: %w", err)
	}

	// 4. Tenta falar com o banco ate o numero de tentativas, esperando um
	// pouco mais a cada vez.
	var lastErr error
	for i := 1; i <= attempts; i++ {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		lastErr = pool.Ping(pingCtx)
		cancel()

		if lastErr == nil {
			log.Info("conectado ao postgres", slog.Int("tentativa", i))
			return pool, nil
		}

		log.Warn("postgres indisponivel, tentando de novo",
			slog.Int("tentativa", i),
			slog.Int("max_tentativas", attempts),
			slog.String("erro", lastErr.Error()),
		)

		// Espera antes da proxima tentativa, ou desiste se o servico for parado.
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(time.Duration(i) * time.Second):
		}
	}

	// 5. Esgotou as tentativas: devolve o ultimo erro.
	pool.Close()
	return nil, fmt.Errorf("sem conexao com o postgres apos %d tentativas: %w", attempts, lastErr)
}
