package migrations

import (
	"context"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDB imita o banco: lembra quais migrations foram registradas e quantas
// vezes cada SQL foi executado.
type fakeDB struct {
	applied   map[string]bool
	registros []string // ordem em que as migrations foram registradas
	executed  []string // SQL das migrations rodadas (fora o controle)
}

func newFakeDB() *fakeDB { return &fakeDB{applied: map[string]bool{}} }

func (f *fakeDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.HasPrefix(strings.TrimSpace(sql), "INSERT INTO schema_migrations"):
		name := args[0].(string)
		f.applied[name] = true
		f.registros = append(f.registros, name)
	case strings.Contains(sql, "CREATE TABLE IF NOT EXISTS schema_migrations"):
		// criacao da tabela de controle: nada a registrar
	default:
		f.executed = append(f.executed, sql)
	}
	return pgconn.CommandTag{}, nil
}

func (f *fakeDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	return boolRow{f.applied[args[0].(string)]}
}

type boolRow struct{ v bool }

func (r boolRow) Scan(dest ...any) error {
	*(dest[0].(*bool)) = r.v
	return nil
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestArquivosEmbutidos(t *testing.T) {
	entries, err := files.ReadDir(".")
	require.NoError(t, err)

	nome := regexp.MustCompile(`^\d{4}_[a-z0-9_]+\.sql$`)
	var sqls []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			sqls = append(sqls, e.Name())
			assert.Regexp(t, nome, e.Name(), "migration deve se chamar NNNN_descricao.sql")
		}
	}
	assert.GreaterOrEqual(t, len(sqls), 2)
	assert.Contains(t, sqls, "0001_create_events.sql")
}

func TestApply_AplicaEmOrdem(t *testing.T) {
	db := newFakeDB()
	require.NoError(t, Apply(context.Background(), db, discardLogger()))

	assert.Equal(t, []string{"0001_create_events.sql", "0002_seed_events.sql"}, db.registros)
	assert.Len(t, db.executed, 2)
	assert.Contains(t, db.executed[0], "CREATE TABLE IF NOT EXISTS events")
}

func TestApply_Idempotente(t *testing.T) {
	db := newFakeDB()
	require.NoError(t, Apply(context.Background(), db, discardLogger()))
	require.NoError(t, Apply(context.Background(), db, discardLogger()))

	assert.Len(t, db.registros, 2, "rodar de novo nao deve registrar nada")
	assert.Len(t, db.executed, 2, "rodar de novo nao deve reexecutar o SQL")
}

func TestApply_PulaAsJaAplicadas(t *testing.T) {
	db := newFakeDB()
	db.applied["0001_create_events.sql"] = true

	require.NoError(t, Apply(context.Background(), db, discardLogger()))

	assert.Equal(t, []string{"0002_seed_events.sql"}, db.registros)
}
