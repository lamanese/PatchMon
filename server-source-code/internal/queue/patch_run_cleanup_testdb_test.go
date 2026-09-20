package queue

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

// Small Postgres test harness for the patch-run reaper, modelled on
// internal/migrate/testdb_test.go: a throwaway database per test run,
// migrated with the real embedded migration sets, dropped on cleanup.
// Skips (rather than fails) when PM_TEST_DATABASE_URL is unset, same as the
// migrate package tests.

func patchRunCleanupAdminURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("PM_TEST_DATABASE_URL")
	if u == "" {
		t.Skip("PM_TEST_DATABASE_URL not set; skipping database-backed patch run cleanup tests")
	}
	return u
}

func discardTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newPatchRunCleanupTestDB creates a throwaway, fully migrated database and
// returns a *database.DB connected to it. The database is dropped and the
// pool closed via t.Cleanup.
func newPatchRunCleanupTestDB(t *testing.T) *database.DB {
	t.Helper()
	admin := patchRunCleanupAdminURL(t)
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("connect admin db: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	name := fmt.Sprintf("pm_reapertest_%d", time.Now().UnixNano())
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

// insertTestHost inserts the minimal hosts row patch_runs' FK needs.
func insertTestHost(t *testing.T, d *database.DB, friendlyName string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := d.Exec(context.Background(), `
		INSERT INTO hosts (id, friendly_name, os_type, os_version, updated_at, api_id, api_key)
		VALUES ($1, $2, 'ubuntu', '22.04', NOW(), $3, $4)
	`, id, friendlyName, "api-"+id, "key-"+id)
	if err != nil {
		t.Fatalf("insert host: %v", err)
	}
	return id
}

// patchRunFixture describes one patch_runs row to insert for a reaper test case.
type patchRunFixture struct {
	status      string
	startedAt   *time.Time
	scheduledAt *time.Time
	updatedAt   time.Time
}

// insertTestPatchRun inserts a patch_runs row with explicit timestamps so
// staleness thresholds can be tested deterministically (CreatePatchRun always
// stamps NOW(), which is useless for testing "N hours ago").
func insertTestPatchRun(t *testing.T, d *database.DB, hostID string, f patchRunFixture) string {
	t.Helper()
	id := uuid.NewString()
	createdAt := f.updatedAt
	_, err := d.Exec(context.Background(), `
		INSERT INTO patch_runs (id, host_id, job_id, patch_type, status, shell_output, dry_run, started_at, scheduled_at, created_at, updated_at)
		VALUES ($1, $2, 'test-job', 'patch_all', $3, '', false, $4, $5, $6, $7)
	`, id, hostID, f.status, f.startedAt, f.scheduledAt, createdAt, f.updatedAt)
	if err != nil {
		t.Fatalf("insert patch_run: %v", err)
	}
	return id
}

// fetchPatchRun reads back the fields the reaper is responsible for.
func fetchPatchRun(t *testing.T, d *database.DB, id string) (status string, errMsg *string, completedAt *time.Time) {
	t.Helper()
	row := d.RawQueryRow(context.Background(), `SELECT status, error_message, completed_at FROM patch_runs WHERE id = $1`, id)
	if err := row.Scan(&status, &errMsg, &completedAt); err != nil {
		t.Fatalf("fetch patch_run %s: %v", id, err)
	}
	return status, errMsg, completedAt
}
