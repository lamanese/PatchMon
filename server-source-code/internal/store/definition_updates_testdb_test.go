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
// test run, migrated with the real embedded migration sets (so fork migration
// 000007's fork_is_definition_update function exists), dropped on cleanup.
// Skips (rather than fails) when PM_TEST_DATABASE_URL is unset.

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
	wuaCategoriesJSON *string // nil => NULL column; else raw jsonb text (array, string, or object)
	wuaGUID           *string // nil => NULL column
	wuaKB             *string // nil => NULL column, e.g. "KB2267602" or "KB1111, KB2267602"
}

// insertDefUpdatesHostPackage inserts a host_packages row. wuaCategoriesJSON
// nil exercises the NULL-safe COALESCE path fork_is_definition_update relies on.
func insertDefUpdatesHostPackage(t *testing.T, d *database.DB, f hostPackageFixture) {
	t.Helper()
	_, err := d.Exec(context.Background(), `
		INSERT INTO host_packages (id, host_id, package_id, current_version, needs_update, is_security_update, wua_categories, wua_guid, wua_kb)
		VALUES ($1, $2, $3, '1.0', $4, $5, $6::jsonb, $7, $8)
	`, uuid.NewString(), f.hostID, f.packageID, f.needsUpdate, f.isSecurity, f.wuaCategoriesJSON, f.wuaGUID, f.wuaKB)
	if err != nil {
		t.Fatalf("insert host_package: %v", err)
	}
}

// defUpdatesFixture is shared by all the counter assertions below.
//
// Windows host (6 host_packages rows, all with wua_guid set so they are
// visible to the Windows-Update-specific queries too):
//
//	a: normal pending security update (not a definition update)
//	b: pending Defender definition update, English category name
//	c: already-installed package (needs_update=false)
//	d: pending Defender definition update, German category name ("Definitionsupdates")
//	e: pending Defender definition update matched by KB2267602 alone (wua_categories NULL)
//	f: pending, non-security, wua_categories is a JSON STRING (not an array) - must not
//	   error and must NOT be treated as a definition update
//
// Linux host (1 host_packages row, no WUA metadata at all):
//
//	g: pending package, wua_categories and wua_kb both NULL - proves the
//	   exclusion is NULL-safe and does not touch non-Windows hosts.
//
// Definition-flagged rows (b, d, e) are all needs_update=true, is_security_update=true,
// so with the flag on: outdated drops by 3, security drops by 3, on the Windows host.
type defUpdatesFixture struct {
	winHostID   string
	linuxHostID string
}

func seedDefUpdatesFixture(t *testing.T, d *database.DB) defUpdatesFixture {
	t.Helper()
	winHostID := insertDefUpdatesTestHost(t, d, "win-host", "windows")
	linuxHostID := insertDefUpdatesTestHost(t, d, "linux-host", "ubuntu")

	pkgA := insertDefUpdatesTestPackage(t, d, "windows-cumulative-update")
	pkgB := insertDefUpdatesTestPackage(t, d, "microsoft-defender-antivirus-definition-update-en")
	pkgC := insertDefUpdatesTestPackage(t, d, "already-installed-package")
	pkgD := insertDefUpdatesTestPackage(t, d, "microsoft-defender-antivirus-definition-update-de")
	pkgE := insertDefUpdatesTestPackage(t, d, "microsoft-defender-antivirus-definition-update-kb-only")
	pkgF := insertDefUpdatesTestPackage(t, d, "package-with-non-array-categories")
	pkgG := insertDefUpdatesTestPackage(t, d, "openssl")

	enCategories := `["Definition Updates", "Microsoft Defender Antivirus"]`
	deCategories := `["Definitionsupdates", "Microsoft Defender Antivirus"]`
	scalarCategories := `"not-an-array"`

	// (a) normal pending security update
	insertDefUpdatesHostPackage(t, d, hostPackageFixture{hostID: winHostID, packageID: pkgA, needsUpdate: true, isSecurity: true, wuaGUID: strPtr("guid-a"), wuaKB: strPtr("KB1111111")})
	// (b) pending Definition Update, English category
	insertDefUpdatesHostPackage(t, d, hostPackageFixture{hostID: winHostID, packageID: pkgB, needsUpdate: true, isSecurity: true, wuaCategoriesJSON: &enCategories, wuaGUID: strPtr("guid-b"), wuaKB: strPtr("KB2267602")})
	// (c) installed package (needs_update=false either way)
	insertDefUpdatesHostPackage(t, d, hostPackageFixture{hostID: winHostID, packageID: pkgC, needsUpdate: false, isSecurity: false, wuaGUID: strPtr("guid-c")})
	// (d) pending Definition Update, German category ("Definitionsupdates") - localisation coverage
	insertDefUpdatesHostPackage(t, d, hostPackageFixture{hostID: winHostID, packageID: pkgD, needsUpdate: true, isSecurity: true, wuaCategoriesJSON: &deCategories, wuaGUID: strPtr("guid-d")})
	// (e) pending Definition Update matched by KB2267602 alone, categories NULL
	insertDefUpdatesHostPackage(t, d, hostPackageFixture{hostID: winHostID, packageID: pkgE, needsUpdate: true, isSecurity: true, wuaGUID: strPtr("guid-e"), wuaKB: strPtr("KB2267602")})
	// (f) wua_categories is a JSON scalar string, not an array - must not error, must not match
	insertDefUpdatesHostPackage(t, d, hostPackageFixture{hostID: winHostID, packageID: pkgF, needsUpdate: true, isSecurity: false, wuaCategoriesJSON: &scalarCategories, wuaGUID: strPtr("guid-f")})
	// Linux host: pending package, wua_categories and wua_kb both NULL - must count identically with the flag on or off.
	insertDefUpdatesHostPackage(t, d, hostPackageFixture{hostID: linuxHostID, packageID: pkgG, needsUpdate: true, isSecurity: false})

	return defUpdatesFixture{winHostID: winHostID, linuxHostID: linuxHostID}
}

// TestForkIsDefinitionUpdateFunction directly table-tests the
// fork_is_definition_update(categories, kb) SQL function added by fork
// migration 000007, independent of any host_packages fixture.
func TestForkIsDefinitionUpdateFunction(t *testing.T) {
	d := newDefinitionUpdatesTestDB(t, false) // flag value is irrelevant here - calling the function directly
	ctx := context.Background()

	tests := []struct {
		name       string
		categories *string // nil => SQL NULL
		kb         *string // nil => SQL NULL
		want       bool
	}{
		{name: "both nil", categories: nil, kb: nil, want: false},
		{name: "english category", categories: strPtr(`["Definition Updates","Microsoft Defender Antivirus"]`), want: true},
		{name: "german category", categories: strPtr(`["Definitionsupdates"]`), want: true},
		{name: "french category", categories: strPtr(`["Mises à jour de définitions"]`), want: true},
		{name: "italian category", categories: strPtr(`["Aggiornamenti delle definizioni"]`), want: true},
		{name: "unrelated category", categories: strPtr(`["Security Updates"]`), want: false},
		{name: "empty array", categories: strPtr(`[]`), want: false},
		{name: "kb exact with prefix", kb: strPtr("KB2267602"), want: true},
		{name: "kb without prefix", kb: strPtr("2267602"), want: true},
		{name: "kb lowercase prefix", kb: strPtr("kb2267602"), want: true},
		{name: "kb in comma list", kb: strPtr("KB1111111, KB2267602"), want: true},
		{name: "unrelated kb", kb: strPtr("KB1111111"), want: false},
		{name: "non-array json string", categories: strPtr(`"Definition Updates"`), want: false},
		{name: "non-array json object", categories: strPtr(`{"a":1}`), want: false},
		{name: "json null literal", categories: strPtr(`null`), want: false},
		{name: "category miss, kb hit", categories: strPtr(`["Security Updates"]`), kb: strPtr("KB2267602"), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			row := d.RawQueryRow(ctx, `SELECT fork_is_definition_update($1::jsonb, $2)`, tt.categories, tt.kb)
			if err := row.Scan(&got); err != nil {
				t.Fatalf("fork_is_definition_update: %v", err)
			}
			if got != tt.want {
				t.Errorf("fork_is_definition_update(%v, %v) = %v, want %v",
					strDeref(tt.categories), strDeref(tt.kb), got, tt.want)
			}
		})
	}
}

func strDeref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// TestDefinitionUpdates_GetHostsWithCounts asserts the host-list counters
// (GetHostsWithCounts, backing GET /dashboard/hosts) exclude the pending
// Definition Updates only when PM_IGNORE_DEFINITION_UPDATES is on, and never
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

			wantWinUpdates, wantWinSecurity := int32(5), int32(4)
			if flag {
				wantWinUpdates, wantWinSecurity = int32(2), int32(1)
			}
			if got := win["updatesCount"]; got != wantWinUpdates {
				t.Errorf("windows updatesCount = %v, want %d", got, wantWinUpdates)
			}
			if got := win["securityUpdatesCount"]; got != wantWinSecurity {
				t.Errorf("windows securityUpdatesCount = %v, want %d", got, wantWinSecurity)
			}
			if got := win["totalPackagesCount"]; got != int32(6) {
				t.Errorf("windows totalPackagesCount = %v, want 6 (unaffected by the flag)", got)
			}

			// Linux host: NULL wua_categories/wua_kb must count the same regardless of the flag.
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
// cards (GetDashboardStats) drop the Definition Update packages from
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

			// Distinct packages needing an update: a,b,d,e,f (windows) + g (linux) = 6.
			// With the flag on, b/d/e (the three definition updates) drop out -> 3.
			wantOutdated, wantSecurity := int32(6), int32(4)
			if flag {
				wantOutdated, wantSecurity = int32(3), int32(1)
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
// (GetHostPackageStats, backing the Host Detail package stat cards and the
// scoped external API) exclude the Definition Updates only for the Windows
// host, only when the flag is on, and never change the total-installed count.
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
			wantOutdated, wantSecurity := int32(5), int32(4)
			if flag {
				wantOutdated, wantSecurity = int32(2), int32(1)
			}
			if winStats.Column1 != 6 {
				t.Errorf("windows total installed = %d, want 6 (unaffected by the flag)", winStats.Column1)
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

// TestDefinitionUpdates_GetHostPackageStatsByHostIDs asserts the admin
// hosts-list stats (GetHostPackageStatsByHostIDs, /hosts/admin/list?include=stats)
// agree with GetHostPackageStats on the same host - same metric, same answer.
func TestDefinitionUpdates_GetHostPackageStatsByHostIDs(t *testing.T) {
	for _, flag := range []bool{false, true} {
		flag := flag
		t.Run(fmt.Sprintf("flag=%v", flag), func(t *testing.T) {
			d := newDefinitionUpdatesTestDB(t, flag)
			fx := seedDefUpdatesFixture(t, d)

			rows, err := d.Queries.GetHostPackageStatsByHostIDs(context.Background(), db.GetHostPackageStatsByHostIDsParams{
				HostIds:                 []string{fx.winHostID, fx.linuxHostID},
				IgnoreDefinitionUpdates: d.IgnoreDefinitionUpdates(),
			})
			if err != nil {
				t.Fatalf("GetHostPackageStatsByHostIDs: %v", err)
			}
			byHost := make(map[string]db.GetHostPackageStatsByHostIDsRow, len(rows))
			for _, r := range rows {
				byHost[r.HostID] = r
			}

			wantOutdated, wantSecurity := int32(5), int32(4)
			if flag {
				wantOutdated, wantSecurity = int32(2), int32(1)
			}
			win := byHost[fx.winHostID]
			if win.Total != 6 || win.Outdated != wantOutdated || win.Security != wantSecurity {
				t.Errorf("windows (total,outdated,security) = (%d,%d,%d), want (6,%d,%d)", win.Total, win.Outdated, win.Security, wantOutdated, wantSecurity)
			}
			linux := byHost[fx.linuxHostID]
			if linux.Total != 1 || linux.Outdated != 1 || linux.Security != 0 {
				t.Errorf("linux (total,outdated,security) = (%d,%d,%d), want (1,1,0)", linux.Total, linux.Outdated, linux.Security)
			}
		})
	}
}

// TestDefinitionUpdates_GetPendingUpdateCountsPerHost asserts the
// update-threshold alert monitor's per-host counts (GetPendingUpdateCountsPerHost)
// exclude the Definition Updates only when the flag is on.
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

			wantPending, wantSecurity := int32(5), int32(4)
			if flag {
				wantPending, wantSecurity = int32(2), int32(1)
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

// TestDefinitionUpdates_GetSystemStatsForInsert asserts the system-statistics
// history job's needs_update subselects exclude Definition Updates only when
// the flag is on, while total_packages and total_hosts stay untouched.
func TestDefinitionUpdates_GetSystemStatsForInsert(t *testing.T) {
	for _, flag := range []bool{false, true} {
		flag := flag
		t.Run(fmt.Sprintf("flag=%v", flag), func(t *testing.T) {
			d := newDefinitionUpdatesTestDB(t, flag)
			seedDefUpdatesFixture(t, d)

			stats, err := d.Queries.GetSystemStatsForInsert(context.Background(), d.IgnoreDefinitionUpdates())
			if err != nil {
				t.Fatalf("GetSystemStatsForInsert: %v", err)
			}

			wantUniquePackages, wantUniqueSecurity := int32(6), int32(4)
			if flag {
				wantUniquePackages, wantUniqueSecurity = int32(3), int32(1)
			}
			if stats.Column1 != wantUniquePackages {
				t.Errorf("unique_packages_count = %d, want %d", stats.Column1, wantUniquePackages)
			}
			if stats.Column2 != wantUniqueSecurity {
				t.Errorf("unique_security_count = %d, want %d", stats.Column2, wantUniqueSecurity)
			}
			// total_packages: every distinct package with any host_packages row
			// (a,b,c,d,e,f,g = 7), regardless of needs_update - never filtered.
			if stats.Column3 != 7 {
				t.Errorf("total_packages = %d, want 7 (unaffected by the flag)", stats.Column3)
			}
			// total_hosts: active hosts (win+linux) - never filtered.
			if stats.Column4 != 2 {
				t.Errorf("total_hosts = %d, want 2 (unaffected by the flag)", stats.Column4)
			}
			if stats.Column5 != 2 {
				t.Errorf("hosts_needing_updates = %d, want 2 (both hosts keep a non-definition pending package)", stats.Column5)
			}
		})
	}
}

// TestDefinitionUpdates_CountWindowsUpdatesByHostID asserts the Windows-updates
// tab counters (CountWindowsUpdatesByHostID) exclude the Definition Updates
// only when the flag is on, and never change installed_count.
func TestDefinitionUpdates_CountWindowsUpdatesByHostID(t *testing.T) {
	for _, flag := range []bool{false, true} {
		flag := flag
		t.Run(fmt.Sprintf("flag=%v", flag), func(t *testing.T) {
			d := newDefinitionUpdatesTestDB(t, flag)
			fx := seedDefUpdatesFixture(t, d)

			stats, err := d.Queries.CountWindowsUpdatesByHostID(context.Background(), db.CountWindowsUpdatesByHostIDParams{
				HostID:                  fx.winHostID,
				IgnoreDefinitionUpdates: d.IgnoreDefinitionUpdates(),
			})
			if err != nil {
				t.Fatalf("CountWindowsUpdatesByHostID: %v", err)
			}
			wantPending, wantSecurity := int32(5), int32(4)
			if flag {
				wantPending, wantSecurity = int32(2), int32(1)
			}
			if stats.PendingCount != wantPending {
				t.Errorf("PendingCount = %d, want %d", stats.PendingCount, wantPending)
			}
			if stats.SecurityCount != wantSecurity {
				t.Errorf("SecurityCount = %d, want %d", stats.SecurityCount, wantSecurity)
			}
			if stats.InstalledCount != 1 {
				t.Errorf("InstalledCount = %d, want 1 (unaffected by the flag)", stats.InstalledCount)
			}
		})
	}
}

// TestDefinitionUpdates_GetHostWindowsUpdatesIsDefinitionUpdate asserts the
// Windows-updates list (GetHostWindowsUpdates) stays complete (every row with
// a wua_guid, regardless of the flag) and flags exactly the three definition
// updates via is_definition_update, independent of the flag.
func TestDefinitionUpdates_GetHostWindowsUpdatesIsDefinitionUpdate(t *testing.T) {
	d := newDefinitionUpdatesTestDB(t, true)
	fx := seedDefUpdatesFixture(t, d)

	rows, err := d.Queries.GetHostWindowsUpdates(context.Background(), fx.winHostID)
	if err != nil {
		t.Fatalf("GetHostWindowsUpdates: %v", err)
	}
	if len(rows) != 6 {
		t.Fatalf("got %d Windows update rows, want 6 (list must stay complete even with the flag on)", len(rows))
	}
	definitionRows := 0
	for _, r := range rows {
		if r.IsDefinitionUpdate {
			definitionRows++
		}
	}
	if definitionRows != 3 {
		t.Errorf("rows flagged is_definition_update = %d, want exactly 3 (b, d, e)", definitionRows)
	}
}

// TestDefinitionUpdates_IsDefinitionUpdateField asserts the host-packages
// list (GetHostPackagesWithPackages, used by GET /dashboard/hosts/{id}) keeps
// every row - including the Definition Updates - visible, and flags them via
// is_definition_update independent of the flag.
func TestDefinitionUpdates_IsDefinitionUpdateField(t *testing.T) {
	d := newDefinitionUpdatesTestDB(t, true)
	fx := seedDefUpdatesFixture(t, d)
	store := NewDashboardStore(d)

	rows, err := store.getHostPackagesWithPackages(context.Background(), fx.winHostID)
	if err != nil {
		t.Fatalf("getHostPackagesWithPackages: %v", err)
	}
	if len(rows) != 6 {
		t.Fatalf("got %d host_packages rows for the windows host, want 6 (list must stay complete even with the flag on)", len(rows))
	}
	definitionRows := 0
	for _, r := range rows {
		if r["is_definition_update"] == true {
			definitionRows++
		}
	}
	if definitionRows != 3 {
		t.Errorf("rows flagged is_definition_update = %d, want exactly 3 (b, d, e)", definitionRows)
	}
}

// TestDefinitionUpdates_ListPackagesIsDefinitionUpdate asserts the Packages
// page list (ListPackages, backing /packages) flags the Definition Update
// packages via is_definition_update without hiding any row or changing
// CountPackages.
func TestDefinitionUpdates_ListPackagesIsDefinitionUpdate(t *testing.T) {
	d := newDefinitionUpdatesTestDB(t, true)
	seedDefUpdatesFixture(t, d)
	pkgStore := NewPackagesStore(d)

	pkgs, total, err := pkgStore.List(context.Background(), ListParams{Limit: 100})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 7 {
		t.Fatalf("CountPackages = %d, want 7 (all packages stay visible/counted regardless of the flag)", total)
	}
	if len(pkgs) != 7 {
		t.Fatalf("got %d packages, want 7", len(pkgs))
	}
	definitionCount := 0
	for _, p := range pkgs {
		if p.IsDefinitionUpdate {
			definitionCount++
		}
	}
	if definitionCount != 3 {
		t.Errorf("packages flagged is_definition_update = %d, want exactly 3 (b, d, e)", definitionCount)
	}
}
