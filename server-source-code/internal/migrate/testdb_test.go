package migrate

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// adminURL returns the server the tests may create databases on, or skips.
func adminURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("PM_TEST_DATABASE_URL")
	if u == "" {
		t.Skip("PM_TEST_DATABASE_URL not set; skipping database migration tests")
	}
	return u
}

// newTestDB creates an empty throwaway database and returns its URL.
func newTestDB(t *testing.T) string {
	t.Helper()
	admin := adminURL(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("connect admin db: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	name := fmt.Sprintf("pm_migtest_%d", time.Now().UnixNano())
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(ctx, admin)
		if err != nil {
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	u, err := url.Parse(admin)
	if err != nil {
		t.Fatalf("parse admin url: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

var versionPrefix = regexp.MustCompile(`^(\d+)_.*\.up\.sql$`)

// highestVersion returns the largest migration number in an embedded directory.
// Tests use it instead of a literal so they survive the upstream sync (40 -> 47).
func highestVersion(t *testing.T, fsys fs.FS, dir string) int64 {
	t.Helper()
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var max int64
	for _, e := range entries {
		m := versionPrefix.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v, _ := strconv.ParseInt(m[1], 10, 64)
		if v > max {
			max = v
		}
	}
	if max == 0 {
		t.Fatalf("no migrations found in %s", dir)
	}
	return max
}

// dbVersions reads both version tables. forkTable reports whether the fork table exists.
func dbVersions(t *testing.T, dbURL string) (upstream, fork int64, forkTable bool) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	if err := conn.QueryRow(ctx, "SELECT version FROM schema_migrations").Scan(&upstream); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	if err := conn.QueryRow(ctx, "SELECT to_regclass('schema_migrations_fork') IS NOT NULL").Scan(&forkTable); err != nil {
		t.Fatalf("check fork table: %v", err)
	}
	if forkTable {
		if err := conn.QueryRow(ctx, "SELECT version FROM schema_migrations_fork").Scan(&fork); err != nil {
			t.Fatalf("read schema_migrations_fork: %v", err)
		}
	}
	return upstream, fork, forkTable
}

//nolint:unused // consumed by makeLegacyForkDB and the bridge tests added in task 2
func execSQL(t *testing.T, dbURL, sql string, args ...any) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// makeLegacyForkDB builds a database exactly as a pre-split fork image left it:
// upstream migrations up to forkBaseVersion, the first n fork migrations applied
// by hand, ONE version table holding forkBaseVersion+n, no fork table.
//
//nolint:unused // consumed by the bridge tests added in task 2
func makeLegacyForkDB(t *testing.T, dbURL string, n int) {
	t.Helper()
	m, err := OpenSet(dbURL, SetUpstream)
	if err != nil {
		t.Fatalf("open upstream set: %v", err)
	}
	if err := m.Migrate(forkBaseVersion); err != nil {
		t.Fatalf("migrate to base: %v", err)
	}
	_, _ = m.Close()

	entries, err := fs.ReadDir(forkMigrationsFS, "migrations_fork")
	if err != nil {
		t.Fatalf("read fork migrations: %v", err)
	}
	for _, e := range entries {
		mm := versionPrefix.FindStringSubmatch(e.Name())
		if mm == nil {
			continue
		}
		v, _ := strconv.Atoi(mm[1])
		if v > n {
			continue
		}
		body, err := fs.ReadFile(forkMigrationsFS, "migrations_fork/"+e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		execSQL(t, dbURL, string(body))
	}
	execSQL(t, dbURL, "UPDATE schema_migrations SET version = $1, dirty = false", int64(forkBaseVersion+n))
}
