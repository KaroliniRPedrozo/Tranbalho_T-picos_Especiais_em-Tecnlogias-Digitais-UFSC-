// Package migrations embute os arquivos .sql no binario e os aplica no boot.
// Mantem o deploy em um unico artefato: nao e preciso copiar SQL para a imagem
// nem subir um container separado so para migrar.
package migrations

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed *.sql
var files embed.FS

// Executor e satisfeito por *pgxpool.Pool, *pgx.Conn e pgx.Tx.
type Executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Apply roda, em ordem lexicografica, toda migration ainda nao registrada em
// schema_migrations. Rodar duas vezes nao causa efeito nenhum.
func Apply(ctx context.Context, db Executor, log *slog.Logger) error {
	const createTable = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`
	if _, err := db.Exec(ctx, createTable); err != nil {
		return fmt.Errorf("criando schema_migrations: %w", err)
	}

	entries, err := files.ReadDir(".")
	if err != nil {
		return fmt.Errorf("lendo migrations embutidas: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var exists bool
		row := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name)
		if err := row.Scan(&exists); err != nil {
			return fmt.Errorf("verificando migration %s: %w", name, err)
		}
		if exists {
			log.Debug("migration ja aplicada", slog.String("version", name))
			continue
		}

		content, err := files.ReadFile(name)
		if err != nil {
			return fmt.Errorf("lendo migration %s: %w", name, err)
		}

		// QueryExecModeSimpleProtocol permite varios comandos SQL no mesmo
		// arquivo; o modo padrao do pgx prepara a query e aceitaria so um.
		if _, err := db.Exec(ctx, string(content), pgx.QueryExecModeSimpleProtocol); err != nil {
			return fmt.Errorf("aplicando migration %s: %w", name, err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			return fmt.Errorf("registrando migration %s: %w", name, err)
		}

		log.Info("migration aplicada", slog.String("version", name))
	}

	return nil
}
