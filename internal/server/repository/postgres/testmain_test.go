package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/dmitriy/gophkeeper/internal/server/repository/postgres"
)

const (
	testDBName     = "gophkeeper"
	testDBUser     = "gophkeeper"
	testDBPassword = "gophkeeper"
	postgresImage  = "postgres:17-alpine"
)

var (
	testStorage *postgres.Storage
	testPool    *pgxpool.Pool
)

// TestMain provides a real PostgreSQL for the whole test package: either
// an ephemeral testcontainers-go instance or, when the TEST_DATABASE_DSN
// environment variable is set, an external database (useful for CI without
// Docker-in-Docker). Migrations from the repository root are applied with
// golang-migrate before the tests run.
func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()

	dsn, cleanup, err := acquireDatabase(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres_test: acquire database: %v\n", err)
		return 1
	}
	defer cleanup()

	testPool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres_test: connect: %v\n", err)
		return 1
	}
	defer testPool.Close()
	if err := testPool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "postgres_test: ping: %v\n", err)
		return 1
	}

	if err := migrateUp(dsn); err != nil {
		fmt.Fprintf(os.Stderr, "postgres_test: migrate: %v\n", err)
		return 1
	}

	testStorage, err = postgres.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres_test: create storage: %v\n", err)
		return 1
	}
	defer testStorage.Close()

	return m.Run()
}

// acquireDatabase returns a DSN to test against and a cleanup function.
// An external database (TEST_DATABASE_DSN) takes precedence over an
// ephemeral container.
func acquireDatabase(ctx context.Context) (dsn string, cleanup func(), err error) {
	if env := os.Getenv("TEST_DATABASE_DSN"); env != "" {
		return env, func() {}, nil
	}

	container, err := tcpg.Run(ctx, postgresImage,
		tcpg.WithDatabase(testDBName),
		tcpg.WithUsername(testDBUser),
		tcpg.WithPassword(testDBPassword),
		tcpg.BasicWaitStrategies(),
	)
	if err != nil {
		return "", nil, fmt.Errorf("start %s container: %w", postgresImage, err)
	}
	dsn, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		return "", nil, fmt.Errorf("container connection string: %w", err)
	}
	return dsn, func() { _ = container.Terminate(ctx) }, nil
}

// migrateUp applies all migrations from the repository root migrations
// directory using golang-migrate.
func migrateUp(dsn string) error {
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "migrations"))
	if err != nil {
		return fmt.Errorf("resolve migrations dir: %w", err)
	}
	source, err := iofs.New(os.DirFS(dir), ".")
	if err != nil {
		return fmt.Errorf("open migrations: %w", err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("parse test database DSN: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("unsupported test database DSN scheme %q", u.Scheme)
	}
	// golang-migrate's pgx/v5 driver is registered under the pgx5 scheme.
	u.Scheme = "pgx5"
	migrations, err := migrate.NewWithSourceInstance("iofs", source, u.String())
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer func() {
		if _, err := migrations.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "postgres_test: close migrator: %v\n", err)
		}
	}()
	if err := migrations.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// missingUUID returns a random UUID that is guaranteed not to be
// referenced by any existing row.
func missingUUID(t *testing.T) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(t.Context(), "SELECT gen_random_uuid()::text").Scan(&id)
	require.NoError(t, err)
	return id
}

// assertValidUUID asserts that id is a canonical lowercase UUID string.
func assertValidUUID(t *testing.T, id string) {
	t.Helper()
	assert.Regexp(t,
		`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, id)
}

// resetTables wipes all users and entries so every test starts from a
// clean, isolated state.
func resetTables(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		_, err := testPool.Exec(context.Background(), "TRUNCATE users, entries CASCADE")
		if err != nil {
			t.Errorf("truncate tables: %v", err)
		}
	})
}
