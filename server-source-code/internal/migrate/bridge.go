package migrate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

// forkBaseVersion is the last migration the fork shares with upstream. All 80
// files up to 000040 are byte-identical between the fork and upstream v2.1.3.
const forkBaseVersion = 40

// bridgeLockKey serialises concurrent bridge attempts; pool_cache.go can call
// Run for the same database from several goroutines. Arbitrary constant ("PMFK").
const bridgeLockKey int64 = 0x504D464B

// forkMarker is the schema object that proves a fork migration level was applied.
// Detection goes by objects, never by the version number alone: a bare "46"
// means fork licensing on a fork database and an upstream index elsewhere.
type forkMarker struct {
	level int
	name  string
	query string // returns exactly one boolean
}

func columnExists(table, column string) string {
	return fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = '%s' AND column_name = '%s')`, table, column)
}

func tableExists(table string) string {
	return fmt.Sprintf(`SELECT to_regclass('%s') IS NOT NULL`, table)
}

// forkMarkers is CLOSED: it describes only the six pre-split fork migrations
// (fork level N = legacy version forkBaseVersion+N, for N in 1..6). New fork
// migrations (000007 and beyond) run through the normal fork migration set
// after the bridge and must never get an entry here — a pre-split image could
// never have produced them, and adding one would silently widen the accepted
// legacy version range.
var forkMarkers = []forkMarker{
	{1, "column role_permissions.can_reboot_hosts", columnExists("role_permissions", "can_reboot_hosts")},
	{2, "column hosts.allow_reboot", columnExists("hosts", "allow_reboot")},
	{3, "table reboot_schedules", tableExists("reboot_schedules")},
	{4, "table patch_schedules", tableExists("patch_schedules")},
	{5, "default 'light' on users.theme_preference", `SELECT COALESCE((SELECT column_default LIKE '%light%'
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'users' AND column_name = 'theme_preference'), false)`},
	{6, "column settings.license_max_hosts", columnExists("settings", "license_max_hosts")},
}

// bridgeLegacyFork moves a database written by a pre-split fork image onto the
// two-table layout: fork level N = old version - 40 goes into the fork table,
// schema_migrations is set back to 40 so upstream's own 41+ run for real.
// It runs once per database, in one transaction, and refuses anything it does
// not fully understand without changing the database.
func bridgeLegacyFork(ctx context.Context, databaseURL string, log *slog.Logger) error {
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", bridgeLockKey); err != nil {
		return fmt.Errorf("acquire bridge lock: %w", err)
	}

	// Everything below runs under the lock, so a second caller sees the result
	// of the first instead of bridging twice.
	var hasUpstreamTable, hasForkTable bool
	if err := tx.QueryRow(ctx,
		"SELECT to_regclass('schema_migrations') IS NOT NULL, to_regclass($1) IS NOT NULL",
		ForkMigrationsTable).Scan(&hasUpstreamTable, &hasForkTable); err != nil {
		return fmt.Errorf("inspect version tables: %w", err)
	}
	if !hasUpstreamTable || hasForkTable {
		return nil // fresh database, or already on the new layout
	}

	present := make([]bool, len(forkMarkers))
	for i, mk := range forkMarkers {
		if err := tx.QueryRow(ctx, mk.query).Scan(&present[i]); err != nil {
			return fmt.Errorf("check marker %q: %w", mk.name, err)
		}
	}
	if !present[0] {
		return nil // pure upstream database: no fork objects, nothing to bridge
	}

	var version int64
	var dirty bool
	err = tx.QueryRow(ctx, "SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&version, &dirty)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("fork objects exist but schema_migrations is empty; refusing to guess the migration level")
	}
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	if dirty {
		return fmt.Errorf("schema_migrations is dirty at version %d; clear the dirty state before upgrading", version)
	}

	level := int(version) - forkBaseVersion
	if level < 1 || level > len(forkMarkers) {
		return fmt.Errorf("legacy fork database reports version %d, outside the expected range %d-%d",
			version, forkBaseVersion+1, forkBaseVersion+len(forkMarkers))
	}
	for i, mk := range forkMarkers {
		want := mk.level <= level
		if present[i] != want {
			return fmt.Errorf("version %d implies fork level %d, but marker %d (%s) present=%v, want %v; database left untouched",
				version, level, mk.level, mk.name, present[i], want)
		}
	}

	// Same shape golang-migrate creates, so its postgres driver adopts the table.
	if _, err := tx.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE %s (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL)`, ForkMigrationsTable)); err != nil {
		return fmt.Errorf("create fork version table: %w", err)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (version, dirty) VALUES ($1, false)`, ForkMigrationsTable), int64(level)); err != nil {
		return fmt.Errorf("seed fork version table: %w", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE schema_migrations SET version = $1, dirty = false", int64(forkBaseVersion)); err != nil {
		return fmt.Errorf("reset upstream version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit bridge: %w", err)
	}

	log.Info("legacy fork database bridged", "old_version", version, "fork_version", level, "upstream_version", forkBaseVersion)
	return nil
}

// NeedsBridge reports whether the database still has the pre-split layout.
// Callers that only inspect state (the CLI) must check this before opening the
// fork set: golang-migrate creates its version table on open, and an empty fork
// table would make bridgeLegacyFork treat the database as already converted.
func NeedsBridge(ctx context.Context, databaseURL string) (bool, error) {
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return false, fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var hasUpstreamTable, hasForkTable, hasForkObjects bool
	if err := conn.QueryRow(ctx,
		"SELECT to_regclass('schema_migrations') IS NOT NULL, to_regclass($1) IS NOT NULL",
		ForkMigrationsTable).Scan(&hasUpstreamTable, &hasForkTable); err != nil {
		return false, fmt.Errorf("inspect version tables: %w", err)
	}
	if !hasUpstreamTable || hasForkTable {
		return false, nil
	}
	if err := conn.QueryRow(ctx, forkMarkers[0].query).Scan(&hasForkObjects); err != nil {
		return false, fmt.Errorf("check fork marker: %w", err)
	}
	return hasForkObjects, nil
}
