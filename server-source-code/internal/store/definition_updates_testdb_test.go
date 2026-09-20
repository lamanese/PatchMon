package store

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
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/migrate"
	"github.com/PatchMon/PatchMon/server-source-code/internal/pgtime"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Small Postgres test harness for PM_IGNORE_DEFINITION_UPDATES, modelled on
// internal/queue/patch_run_cleanup_testdb_test.go: a throwaway database per
// test run, migrated with the real embedded migration sets, dropped on
// cleanup. Skips (rather than fails) when PM_TEST_DATABASE_URL is unset.

func definitionUpdatesAdminURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("PM_TEST_DATABASE_URL")
	if u == "" {
		t.Skip("PM_TEST_DATABASE_URL not set; skipping database-backed definition-update counter tests")
	}
	return u
}

func definitionUpdatesDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newDefinitionUpdatesTestDB creates a throwaway, fully migrated database
// connected with ignoreDefinitionUpdates baked into its *config.Config, so
// d.IgnoreDefinitionUpdates() (and every store method that reads it) reflects
// the flag under test. The database is dropped and the pool closed via
// t.Cleanup.
func newDefinitionUpdatesTestDB(t *testing.T, ignoreDefinitionUpdates bool) *database.DB {
	t.Helper()
	admin := definitionUpdatesAdminURL(t)
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("connect admin db: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	name := fmt.Sprintf("pm_defupdatestest_%d", time.Now().UnixNano())
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

	if err := migrate.Run(dbURL, definitionUpdatesDiscardLogger()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	d, err := database.NewFromURL(ctx, dbURL, 0, 0, &config.Config{IgnoreDefinitionUpdates: ignoreDefinitionUpdates})
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// insertDefUpdatesTestHost inserts the minimal hosts row host_packages' FK needs.
// osType/osVersion only matter for readability of the fixture; counting logic
// does not branch on OS.
func insertDefUpdatesTestHost(t *testing.T, d *database.DB, friendlyName, osType string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := d.Exec(context.Background(), `
		INSERT INTO hosts (id, friendly_name, os_type, os_version, updated_at, api_id, api_key)
		VALUES ($1, $2, $3, '1.0', NOW(), $4, $5)
	`, id, friendlyName, osType, "api-"+id, "key-"+id)
	if err != nil {
		t.Fatalf("insert host: %v", err)
	}
	return id
}

// insertDefUpdatesTestPackage inserts a row into the global packages catalog.
func insertDefUpdatesTestPackage(t *testing.T, d *database.DB, name string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := d.Exec(context.Background(), `
		INSERT INTO packages (id, name, updated_at) VALUES ($1, $2, NOW())
	`, id, name)
	if err != nil {
		t.Fatalf("insert package: %v", err)
	}
	return id
}

// hostPackageFixture describes one host_packages row to insert.
type hostPackageFixture struct {
	hostID            string
	packageID         string
	needsUpdate       bool
	isSecurity        bool
	wuaCategoriesJSON *string // nil => NULL column; else raw JSON array text, e.g. `["Definition Updates"]`
}

// insertDefUpdatesHostPackage inserts a host_packages row. wuaCategoriesJSON
// nil exercises the NULL-safe COALESCE path the SQL predicate relies on.
func insertDefUpdatesHostPackage(t *testing.T, d *database.DB, f hostPackageFixture) {
	t.Helper()
	_, err := d.Exec(context.Background(), `
		INSERT INTO host_packages (id, host_id, package_id, current_version, needs_update, is_security_update, wua_categories)
		VALUES ($1, $2, $3, '1.0', $4, $5, $6::jsonb)
	`, uuid.NewString(), f.hostID, f.packageID, f.needsUpdate, f.isSecurity, f.wuaCategoriesJSON)
	if err != nil {
		t.Fatalf("insert host_package: %v", err)
	}
}

// defUpdatesFixture is shared by all the counter assertions below: one
// Windows host with a normal pending security update, a pending Windows
// Defender definition update, and an already-installed package; one Linux
// host with a pending package whose wua_categories is NULL (proves the
// exclusion is NULL-safe and does not touch non-Windows hosts).
type defUpdatesFixture struct {
	winHostID   string
	linuxHostID string
}

func seedDefUpdatesFixture(t *testing.T, d *database.DB) defUpdatesFixture {
	t.Helper()
	winHostID := insertDefUpdatesTestHost(t, d, "win-host", "windows")
	linuxHostID := insertDefUpdatesTestHost(t, d, "linux-host", "ubuntu")

	pkgSecurity := insertDefUpdatesTestPackage(t, d, "windows-cumulative-update")
	pkgDefinition := insertDefUpdatesTestPackage(t, d, "microsoft-defender-antivirus-definition-update")
	pkgInstalled := insertDefUpdatesTestPackage(t, d, "already-installed-package")
	pkgLinuxPending := insertDefUpdatesTestPackage(t, d, "openssl")

	definitionCategories := `["Definition Updates", "Microsoft Defender Antivirus"]`

	// (a) normal pending security update
	insertDefUpdatesHostPackage(t, d, hostPackageFixture{hostID: winHostID, packageID: pkgSecurity, needsUpdate: true, isSecurity: true})
	// (b) pending Definition Update
	insertDefUpdatesHostPackage(t, d, hostPackageFixture{hostID: winHostID, packageID: pkgDefinition, needsUpdate: true, isSecurity: true, wuaCategoriesJSON: &definitionCategories})
	// (c) installed package (needs_update=false either way)
	insertDefUpdatesHostPackage(t, d, hostPackageFixture{hostID: winHostID, packageID: pkgInstalled, needsUpdate: false, isSecurity: false})
	// Linux host: pending package, wua_categories NULL - must count identically with the flag on or off.
	insertDefUpdatesHostPackage(t, d, hostPackageFixture{hostID: linuxHostID, packageID: pkgLinuxPending, needsUpdate: true, isSecurity: false})

	return defUpdatesFixture{winHostID: winHostID, linuxHostID: linuxHostID}
}

// TestDefinitionUpdates_GetHostsWithCounts asserts the host-list counters
// (GetHostsWithCounts, backing GET /dashboard/hosts) exclude the pending
// Definition Update only when PM_IGNORE_DEFINITION_UPDATES is on, and never
// touch the Linux host's NULL-category row or the total-installed count.
func TestDefinitionUpdates_GetHostsWithCounts(t *testing.T) {
	for _, flag := range []bool{false, true} {
		flag := flag
		t.Run(fmt.Sprintf("flag=%v", flag), func(t *testing.T) {
			d := newDefinitionUpdatesTestDB(t, flag)
			fx := seedDefUpdatesFixture(t, d)
			store := NewDashboardStore(d)

			rows, err := store.GetHostsWithCounts(context.Background(), HostsListParams{})
			if err != nil {
				t.Fatalf("GetHostsWithCounts: %v", err)
			}
			byID := make(map[string]map[string]interface{}, len(rows))
			for _, r := range rows {
				byID[r["id"].(string)] = r
			}

			win, ok := byID[fx.winHostID]
			if !ok {
				t.Fatalf("windows host missing from GetHostsWithCounts result")
			}
			linux, ok := byID[fx.linuxHostID]
			if !ok {
				t.Fatalf("linux host missing from GetHostsWithCounts result")
			}

			wantWinUpdates, wantWinSecurity := int32(2), int32(2)
			if flag {
				wantWinUpdates, wantWinSecurity = int32(1), int32(1)
			}
			if got := win["updatesCount"]; got != wantWinUpdates {
				t.Errorf("windows updatesCount = %v, want %d", got, wantWinUpdates)
			}
			if got := win["securityUpdatesCount"]; got != wantWinSecurity {
				t.Errorf("windows securityUpdatesCount = %v, want %d", got, wantWinSecurity)
			}
			if got := win["totalPackagesCount"]; got != int32(3) {
				t.Errorf("windows totalPackagesCount = %v, want 3 (unaffected by the flag)", got)
			}

			// Linux host: NULL wua_categories must count the same regardless of the flag.
			if got := linux["updatesCount"]; got != int32(1) {
				t.Errorf("linux updatesCount = %v, want 1 (NULL-safe, unaffected by the flag)", got)
			}
			if got := linux["securityUpdatesCount"]; got != int32(0) {
				t.Errorf("linux securityUpdatesCount = %v, want 0", got)
			}
		})
	}
}

// TestDefinitionUpdates_GetDashboardStats asserts the fleet-wide dashboard
// cards (GetDashboardStats) drop the Definition Update package from
// total_outdated_packages and security_updates only when the flag is on.
func TestDefinitionUpdates_GetDashboardStats(t *testing.T) {
	for _, flag := range []bool{false, true} {
		flag := flag
		t.Run(fmt.Sprintf("flag=%v", flag), func(t *testing.T) {
			d := newDefinitionUpdatesTestDB(t, flag)
			seedDefUpdatesFixture(t, d)

			farPast := pgtime.From(time.Now().AddDate(-10, 0, 0))
			stats, err := d.Queries.GetDashboardStats(context.Background(), db.GetDashboardStatsParams{
				LastUpdate:              farPast,
				LastUpdate_2:            farPast,
				IgnoreDefinitionUpdates: d.IgnoreDefinitionUpdates(),
			})
			if err != nil {
				t.Fatalf("GetDashboardStats: %v", err)
			}

			wantOutdated, wantSecurity := int32(3), int32(2)
			if flag {
				wantOutdated, wantSecurity = int32(2), int32(1)
			}
			if stats.TotalOutdatedPackages != wantOutdated {
				t.Errorf("TotalOutdatedPackages = %d, want %d", stats.TotalOutdatedPackages, wantOutdated)
			}
			if stats.SecurityUpdates != wantSecurity {
				t.Errorf("SecurityUpdates = %d, want %d", stats.SecurityUpdates, wantSecurity)
			}
			// Both hosts still have at least one non-definition pending package in
			// this fixture, so hosts_needing_updates does not change - only the
			// package-level counts do.
			if stats.HostsNeedingUpdates != 2 {
				t.Errorf("HostsNeedingUpdates = %d, want 2", stats.HostsNeedingUpdates)
			}
		})
	}
}

// TestDefinitionUpdates_GetHostPackageStats asserts the host-detail counters
// (GetHostPackageStats, backing the Host Detail package stat cards) exclude
// the Definition Update only for the Windows host, only when the flag is on,
// and never change the total-installed count.
func TestDefinitionUpdates_GetHostPackageStats(t *testing.T) {
	for _, flag := range []bool{false, true} {
		flag := flag
		t.Run(fmt.Sprintf("flag=%v", flag), func(t *testing.T) {
			d := newDefinitionUpdatesTestDB(t, flag)
			fx := seedDefUpdatesFixture(t, d)

			winStats, err := d.Queries.GetHostPackageStats(context.Background(), db.GetHostPackageStatsParams{HostID: fx.winHostID, IgnoreDefinitionUpdates: d.IgnoreDefinitionUpdates()})
			if err != nil {
				t.Fatalf("GetHostPackageStats(windows): %v", err)
			}
			wantOutdated, wantSecurity := int32(2), int32(2)
			if flag {
				wantOutdated, wantSecurity = int32(1), int32(1)
			}
			if winStats.Column1 != 3 {
				t.Errorf("windows total installed = %d, want 3 (unaffected by the flag)", winStats.Column1)
			}
			if winStats.Column2 != wantOutdated {
				t.Errorf("windows outdated = %d, want %d", winStats.Column2, wantOutdated)
			}
			if winStats.Column3 != wantSecurity {
				t.Errorf("windows security = %d, want %d", winStats.Column3, wantSecurity)
			}

			linuxStats, err := d.Queries.GetHostPackageStats(context.Background(), db.GetHostPackageStatsParams{HostID: fx.linuxHostID, IgnoreDefinitionUpdates: d.IgnoreDefinitionUpdates()})
			if err != nil {
				t.Fatalf("GetHostPackageStats(linux): %v", err)
			}
			if linuxStats.Column1 != 1 || linuxStats.Column2 != 1 || linuxStats.Column3 != 0 {
				t.Errorf("linux stats = (%d,%d,%d), want (1,1,0) regardless of the flag (NULL-safe)", linuxStats.Column1, linuxStats.Column2, linuxStats.Column3)
			}
		})
	}
}

// TestDefinitionUpdates_GetPendingUpdateCountsPerHost asserts the
// update-threshold alert monitor's per-host counts (GetPendingUpdateCountsPerHost)
// exclude the Definition Update only when the flag is on.
func TestDefinitionUpdates_GetPendingUpdateCountsPerHost(t *testing.T) {
	for _, flag := range []bool{false, true} {
		flag := flag
		t.Run(fmt.Sprintf("flag=%v", flag), func(t *testing.T) {
			d := newDefinitionUpdatesTestDB(t, flag)
			fx := seedDefUpdatesFixture(t, d)

			rows, err := d.Queries.GetPendingUpdateCountsPerHost(context.Background(), d.IgnoreDefinitionUpdates())
			if err != nil {
				t.Fatalf("GetPendingUpdateCountsPerHost: %v", err)
			}
			byHost := make(map[string]db.GetPendingUpdateCountsPerHostRow, len(rows))
			for _, r := range rows {
				byHost[r.HostID] = r
			}

			win, ok := byHost[fx.winHostID]
			if !ok {
				t.Fatalf("windows host missing from GetPendingUpdateCountsPerHost result")
			}
			linux, ok := byHost[fx.linuxHostID]
			if !ok {
				t.Fatalf("linux host missing from GetPendingUpdateCountsPerHost result")
			}

			wantPending, wantSecurity := int32(2), int32(2)
			if flag {
				wantPending, wantSecurity = int32(1), int32(1)
			}
			if win.PendingCount != wantPending {
				t.Errorf("windows PendingCount = %d, want %d", win.PendingCount, wantPending)
			}
			if win.SecurityCount != wantSecurity {
				t.Errorf("windows SecurityCount = %d, want %d", win.SecurityCount, wantSecurity)
			}
			if linux.PendingCount != 1 || linux.SecurityCount != 0 {
				t.Errorf("linux counts = (%d,%d), want (1,0) regardless of the flag (NULL-safe)", linux.PendingCount, linux.SecurityCount)
			}
		})
	}
}

// TestDefinitionUpdates_IsDefinitionUpdateField asserts the host-packages
// list (GetHostPackagesWithPackages, used by GET /dashboard/hosts/{id}) keeps
// every row - including the Definition Update - visible, and flags it via
// is_definition_update independent of the flag.
func TestDefinitionUpdates_IsDefinitionUpdateField(t *testing.T) {
	d := newDefinitionUpdatesTestDB(t, true)
	fx := seedDefUpdatesFixture(t, d)
	store := NewDashboardStore(d)

	rows, err := store.getHostPackagesWithPackages(context.Background(), fx.winHostID)
	if err != nil {
		t.Fatalf("getHostPackagesWithPackages: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d host_packages rows for the windows host, want 3 (list must stay complete even with the flag on)", len(rows))
	}
	definitionRows := 0
	for _, r := range rows {
		if r["is_definition_update"] == true {
			definitionRows++
		}
	}
	if definitionRows != 1 {
		t.Errorf("rows flagged is_definition_update = %d, want exactly 1", definitionRows)
	}
}
