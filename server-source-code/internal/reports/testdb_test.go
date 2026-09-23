package reports

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/config"
	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/migrate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The harness mirrors internal/queue/patch_run_cleanup_testdb_test.go: one
// fresh, fully migrated database per test, dropped on cleanup. Tests skip
// without PM_TEST_DATABASE_URL (CI sets it, see build-lamanese.yml).

func reportsAdminURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("PM_TEST_DATABASE_URL")
	if u == "" {
		t.Skip("PM_TEST_DATABASE_URL not set; skipping database-backed report tests")
	}
	return u
}

func discardTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newReportsTestDB(t *testing.T) *database.DB {
	t.Helper()
	admin := reportsAdminURL(t)
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("connect admin db: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	name := fmt.Sprintf("pm_reportstest_%d", time.Now().UnixNano())
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), admin)
		if err != nil {
			return
		}
		defer func() { _ = c.Close(context.Background()) }()
		_, _ = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	u, err := url.Parse(admin)
	if err != nil {
		t.Fatalf("parse admin url: %v", err)
	}
	u.Path = "/" + name
	dbURL := u.String()

	if err := migrate.Run(dbURL, discardTestLogger()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	d, err := database.NewFromURL(ctx, dbURL, 0, 0, &config.Config{})
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// --- fixtures (raw SQL so that created_at is under test control) ---

func mustExec(t *testing.T, d *database.DB, sql string, args ...any) {
	t.Helper()
	if _, err := d.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func insertGroup(t *testing.T, d *database.DB, name string) string {
	id := uuid.NewString()
	mustExec(t, d, `INSERT INTO host_groups (id, name, created_at, updated_at) VALUES ($1, $2, NOW(), NOW())`, id, name)
	return id
}

func insertHost(t *testing.T, d *database.DB, name string, groupIDs ...string) string {
	id := uuid.NewString()
	mustExec(t, d, `INSERT INTO hosts (id, friendly_name, hostname, os_type, os_version, status, last_update, created_at, updated_at, api_id, api_key, agent_version)
		VALUES ($1, $2, $2, 'ubuntu', '24.04', 'active', NOW(), NOW(), NOW(), $3, $4, '2.0.20')`, id, name, "api-"+id, "key-"+id)
	for _, g := range groupIDs {
		mustExec(t, d, `INSERT INTO host_group_memberships (id, host_id, host_group_id, created_at) VALUES ($1, $2, $3, NOW())`, uuid.NewString(), id, g)
	}
	return id
}
