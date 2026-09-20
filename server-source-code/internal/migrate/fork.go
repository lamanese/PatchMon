package migrate

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Fork migrations live apart from upstream's so the two never compete for the
// same version numbers. golang-migrate tracks ONE number per table; with a
// shared table, a fork database at 46 would silently skip upstream's own
// 41-46 after a sync. See KIMigrationsstrategie.md.
//
//go:embed migrations_fork/*.sql
var forkMigrationsFS embed.FS

// ForkMigrationsTable is the version table of the fork migration set.
const ForkMigrationsTable = "schema_migrations_fork"

// Set names one of the two migration sets.
type Set string

const (
	SetUpstream Set = "upstream"
	SetFork     Set = "fork"
)

// Run is the startup entry point: bridge a legacy fork database once, then
// apply upstream migrations, then fork migrations. Fork migrations run last
// because they reference upstream tables (host_groups, users, settings).
func Run(databaseURL string, log *slog.Logger) error {
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required for migrations")
	}
	if err := bridgeLegacyFork(context.Background(), databaseURL, log); err != nil {
		fmt.Fprintf(os.Stderr, "[migrate] legacy fork bridge failed: %v\n", err)
		return fmt.Errorf("bridge legacy fork database: %w", err)
	}
	if err := runUpstream(databaseURL, log); err != nil {
		return err
	}
	return runFork(databaseURL, log)
}

func runFork(databaseURL string, log *slog.Logger) error {
	m, err := OpenSet(databaseURL, SetFork)
	if err != nil {
		return fmt.Errorf("create fork migrate instance: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	upErr := m.Up()
	if upErr != nil && !errors.Is(upErr, migrate.ErrNoChange) {
		fmt.Fprintf(os.Stderr, "[migrate] fork set failed: %v\n", upErr)
		return fmt.Errorf("fork migration up: %w", upErr)
	}
	if errors.Is(upErr, migrate.ErrNoChange) {
		_, _ = fmt.Fprintln(os.Stdout, "[migrate] fork set already up to date")
		log.Info("fork migrations: already up to date")
		return nil
	}
	version, _, _ := m.Version()
	_, _ = fmt.Fprintf(os.Stdout, "[migrate] fork set applied successfully (version %d)\n", version)
	log.Info("fork migrations applied successfully", "version", version)
	return nil
}

// OpenSet returns a migrate instance for one set. Caller must Close it.
func OpenSet(databaseURL string, set Set) (*migrate.Migrate, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required for migrations")
	}
	switch set {
	case SetUpstream:
		return Open(databaseURL)
	case SetFork:
		source, err := iofs.New(forkMigrationsFS, "migrations_fork")
		if err != nil {
			return nil, fmt.Errorf("create embedded fork migrate source: %w", err)
		}
		return migrate.NewWithSourceInstance("iofs", source, forkURL(databaseURL))
	default:
		return nil, fmt.Errorf("unknown migration set %q (want %q or %q)", set, SetUpstream, SetFork)
	}
}

// forkURL points golang-migrate at the fork version table. It always sets (or
// replaces) x-migrations-table: leaving a caller-supplied value in place
// would let the fork and upstream migrate.Migrate instances share one
// version table, and each would then silently skip the other's migrations.
func forkURL(databaseURL string) string {
	u, err := url.Parse(databaseURL)
	if err != nil {
		// Cannot parse: fall back to the old string-based behaviour rather
		// than fail startup over a URL shape net/url does not accept.
		s := ensureSSLMode(databaseURL) // guarantees a query string
		if strings.Contains(s, "x-migrations-table=") {
			return s
		}
		return s + "&x-migrations-table=" + ForkMigrationsTable
	}

	q := u.Query()
	if q.Get("sslmode") == "" {
		q.Set("sslmode", "disable")
	}
	q.Set("x-migrations-table", ForkMigrationsTable)
	u.RawQuery = q.Encode()
	return u.String()
}
