package migrate

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// pool_cache.go may call Run for one database from several goroutines.
func TestBridge_ConcurrentRunsBridgeOnce(t *testing.T) {
	dbURL := newTestDB(t)
	makeLegacyForkDB(t, dbURL, 5)

	const callers = 4
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() { errs <- Run(dbURL, discardLogger()) }()
	}
	for i := 0; i < callers; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Run %d: %v", i, err)
		}
	}
	up, fork, _ := dbVersions(t, dbURL)
	if want := highestVersion(t, migrationsFS, "migrations"); up != want {
		t.Errorf("upstream version = %d, want %d", up, want)
	}
	if want := highestVersion(t, forkMigrationsFS, "migrations_fork"); fork != want {
		t.Errorf("fork version = %d, want %d", fork, want)
	}
}

// schemaFingerprint lists columns, indexes and constraints in a stable order.
func schemaFingerprint(t *testing.T, dbURL string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	const q = `
SELECT 'col|' || table_name || '|' || column_name || '|' || data_type || '|' || is_nullable || '|' || COALESCE(column_default, '')
  FROM information_schema.columns WHERE table_schema = current_schema()
UNION ALL
SELECT 'idx|' || tablename || '|' || indexname || '|' || indexdef
  FROM pg_indexes WHERE schemaname = current_schema()
UNION ALL
SELECT 'con|' || conrelid::regclass::text || '|' || conname || '|' || pg_get_constraintdef(oid)
  FROM pg_constraint WHERE connamespace = current_schema()::regnamespace
ORDER BY 1`
	rows, err := conn.Query(ctx, q)
	if err != nil {
		t.Fatalf("fingerprint query: %v", err)
	}
	defer rows.Close()
	var sb strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan: %v", err)
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// A bridged legacy database must end up with the same schema as a fresh install.
func TestBridge_SchemaMatchesFreshInstall(t *testing.T) {
	fresh := newTestDB(t)
	if err := Run(fresh, discardLogger()); err != nil {
		t.Fatalf("Run fresh: %v", err)
	}
	legacy := newTestDB(t)
	makeLegacyForkDB(t, legacy, 5)
	if err := Run(legacy, discardLogger()); err != nil {
		t.Fatalf("Run legacy: %v", err)
	}
	a, b := schemaFingerprint(t, fresh), schemaFingerprint(t, legacy)
	if a != b {
		t.Errorf("schemas differ.\n--- fresh\n%s\n--- bridged\n%s", a, b)
	}
}

func TestBridge_LegacyDatabaseAtEveryLevel(t *testing.T) {
	// The set of legacy levels a pre-split image could have produced is closed
	// at len(forkMarkers) forever (fork 1..6 = old 41..46); it must never track
	// the growing fork migration set. forkMax is only the post-bridge target:
	// migrations added after the split (000007+) must still run after bridging.
	forkMax := highestVersion(t, forkMigrationsFS, "migrations_fork")
	for n := 1; n <= len(forkMarkers); n++ {
		n := n
		t.Run(fmt.Sprintf("legacy_version_%d", forkBaseVersion+n), func(t *testing.T) {
			dbURL := newTestDB(t)
			makeLegacyForkDB(t, dbURL, n)

			var buf bytes.Buffer
			log := slog.New(slog.NewTextHandler(&buf, nil))
			if err := Run(dbURL, log); err != nil {
				t.Fatalf("Run: %v", err)
			}

			up, fork, forkTable := dbVersions(t, dbURL)
			if !forkTable {
				t.Fatal("fork table missing after bridge")
			}
			if want := highestVersion(t, migrationsFS, "migrations"); up != want {
				t.Errorf("upstream version = %d, want %d", up, want)
			}
			if fork != forkMax {
				t.Errorf("fork version = %d, want %d (missing fork migrations must run after the bridge)", fork, forkMax)
			}
			if !strings.Contains(buf.String(), "legacy fork database bridged") {
				t.Errorf("bridge was not logged; log:\n%s", buf.String())
			}
		})
	}
}

// 000001_fork re-grants can_reboot_hosts to admin. The bridge must seed the fork
// table so that migration never runs again on a database that already had it.
func TestBridge_DoesNotRegrantRebootPermission(t *testing.T) {
	dbURL := newTestDB(t)
	makeLegacyForkDB(t, dbURL, len(forkMarkers))
	execSQL(t, dbURL, "UPDATE role_permissions SET can_reboot_hosts = false WHERE role = 'admin'")

	if err := Run(dbURL, discardLogger()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var granted bool
	if err := conn.QueryRow(ctx, "SELECT can_reboot_hosts FROM role_permissions WHERE role = 'admin'").Scan(&granted); err != nil {
		t.Fatalf("read permission: %v", err)
	}
	if granted {
		t.Error("can_reboot_hosts was re-granted to admin; the bridge re-ran fork migration 1")
	}
}

func TestBridge_AbortsAndLeavesDatabaseUntouched(t *testing.T) {
	cases := []struct {
		name    string
		break_  string // SQL that makes the legacy database inconsistent
		wantErr string
	}{
		{"dirty", "UPDATE schema_migrations SET dirty = true", "dirty"},
		{"version below range", "UPDATE schema_migrations SET version = 39", "outside"},
		{"version above range", "UPDATE schema_migrations SET version = 47", "outside"},
		{"marker missing for claimed level", "DROP TABLE patch_schedules", "patch_schedules"},
		// Version 44 claims fork level 4, but markers 5 and 6 exist. Markers are
		// checked in level order, so the first one reported is level 5.
		{"marker present above claimed level", "UPDATE schema_migrations SET version = 44", "theme_preference"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dbURL := newTestDB(t)
			makeLegacyForkDB(t, dbURL, len(forkMarkers))
			execSQL(t, dbURL, tc.break_)
			upBefore, _, _ := dbVersions(t, dbURL)

			err := Run(dbURL, discardLogger())
			if err == nil {
				t.Fatal("Run succeeded on an inconsistent legacy database, want error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), tc.wantErr)
			}
			up, _, forkTable := dbVersions(t, dbURL)
			if forkTable {
				t.Error("fork table was created although the bridge aborted")
			}
			if up != upBefore {
				t.Errorf("schema_migrations changed: %d -> %d", upBefore, up)
			}
		})
	}
}

// Once a legacy database has been bridged, hasForkTable short-circuits the
// bridge on every later Run. It must stay a silent no-op: no error, no
// version change, and no repeat of the "legacy fork database bridged" log.
func TestBridge_SecondRunOnBridgedDatabaseIsNoOp(t *testing.T) {
	dbURL := newTestDB(t)
	makeLegacyForkDB(t, dbURL, len(forkMarkers))

	if err := Run(dbURL, discardLogger()); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	upBefore, forkBefore, _ := dbVersions(t, dbURL)

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	if err := Run(dbURL, log); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	up, fork, forkTable := dbVersions(t, dbURL)
	if !forkTable {
		t.Fatal("fork table missing after second Run")
	}
	if up != upBefore || fork != forkBefore {
		t.Errorf("versions changed on second run: %d/%d -> %d/%d", upBefore, forkBefore, up, fork)
	}
	if strings.Contains(buf.String(), "legacy fork database bridged") {
		t.Errorf("bridge ran again on an already-bridged database; log:\n%s", buf.String())
	}
}
