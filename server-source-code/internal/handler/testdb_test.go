package handler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/config"
	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/migrate"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// The harness mirrors internal/reports/testdb_test.go: one fresh, fully
// migrated database per test, dropped on cleanup. Tests skip without
// PM_TEST_DATABASE_URL (CI sets it, see build-lamanese.yml).

func handlerAdminURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("PM_TEST_DATABASE_URL")
	if u == "" {
		t.Skip("PM_TEST_DATABASE_URL not set; skipping database-backed handler tests")
	}
	return u
}

func newHandlerTestDB(t *testing.T) *database.DB {
	t.Helper()
	admin := handlerAdminURL(t)
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("connect admin db: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	name := fmt.Sprintf("pm_handlertest_%d", time.Now().UnixNano())
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

	if err := migrate.Run(dbURL, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	d, err := database.NewFromURL(ctx, dbURL, 0, 0, &config.Config{})
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

func handlerWithDB(d *database.DB) *NotificationsHandler {
	return &NotificationsHandler{db: fakeProvider{d}}
}

// routedRequest builds a JSON request with chi URL params set.
func routedRequest(method, path string, body string, params map[string]string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rc := chi.NewRouteContext()
	for k, v := range params {
		rc.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
}
