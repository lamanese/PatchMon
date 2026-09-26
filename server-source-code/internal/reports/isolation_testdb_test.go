package reports

import (
	"context"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/google/uuid"
)

// fixture builds two customer groups whose data must never mix.
type fixture struct {
	gA, gB           string
	a1, a2, b1, u1   string
	now              time.Time
	runA1, runA1Dry  string
	runA2Old, runB1  string
	rebootA1         string
	deletedHostAlert string
}

func insertRun(t *testing.T, d *database.DB, hostID, status string, dryRun bool, createdAt time.Time, affected *string) string {
	id := uuid.NewString()
	mustExec(t, d, `INSERT INTO patch_runs (id, host_id, job_id, patch_type, status, dry_run, packages_affected, created_at, updated_at, completed_at)
		VALUES ($1, $2, $3, 'patch_all', $4, $5, $6::jsonb, $7, $7, $7)`, id, hostID, "job-"+id, status, dryRun, affected, createdAt)
	return id
}

func insertAlert(t *testing.T, d *database.DB, typ, severity, title string, metadata string, createdAt time.Time) string {
	id := uuid.NewString()
	mustExec(t, d, `INSERT INTO alerts (id, type, severity, title, message, metadata, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $4, $5::jsonb, true, $6, $6)`, id, typ, severity, title, metadata, createdAt)
	return id
}

func insertPackage(t *testing.T, d *database.DB, name, latest string) string {
	id := uuid.NewString()
	mustExec(t, d, `INSERT INTO packages (id, name, latest_version, created_at, updated_at) VALUES ($1, $2, $3, NOW(), NOW())`, id, name, latest)
	return id
}

func insertHostPackage(t *testing.T, d *database.DB, hostID, pkgID, current, available string, security bool) {
	mustExec(t, d, `INSERT INTO host_packages (id, host_id, package_id, current_version, available_version, needs_update, is_security_update, last_checked)
		VALUES ($1, $2, $3, $4, $5, true, $6, NOW())`, uuid.NewString(), hostID, pkgID, current, available, security)
}

func insertScan(t *testing.T, d *database.DB, hostID, profileID string, score float64, passed, failed int, completedAt time.Time) {
	mustExec(t, d, `INSERT INTO compliance_scans (id, host_id, profile_id, started_at, completed_at, status, score, passed, failed, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $4, 'completed', $5, $6, $7, $4, $4)`, uuid.NewString(), hostID, profileID, completedAt, score, passed, failed)
}

func insertReboot(t *testing.T, d *database.DB, hostID, status string, createdAt time.Time) string {
	id := uuid.NewString()
	mustExec(t, d, `INSERT INTO job_history (id, job_id, queue_name, job_name, host_id, status, created_at, updated_at, completed_at)
		VALUES ($1, $2, 'agent-commands', 'reboot', $3, $4, $5, $5, $5)`, id, "task-"+id, hostID, status, createdAt)
	return id
}

func strPtr(s string) *string { return &s }

func buildFixture(t *testing.T, d *database.DB) fixture {
	f := fixture{now: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)}
	f.gA = insertGroup(t, d, "Customer A")
	f.gB = insertGroup(t, d, "Customer B")
	f.a1 = insertHost(t, d, "a1", f.gA)
	f.a2 = insertHost(t, d, "a2", f.gA)
	f.b1 = insertHost(t, d, "b1", f.gB)
	f.u1 = insertHost(t, d, "u1")

	// patch runs
	f.runA1 = insertRun(t, d, f.a1, "completed", false, f.now.Add(-2*24*time.Hour), strPtr(`["openssl","curl"]`))
	f.runA1Dry = insertRun(t, d, f.a1, "completed", true, f.now.Add(-1*24*time.Hour), strPtr(`["openssl"]`))
	f.runA2Old = insertRun(t, d, f.a2, "failed", false, f.now.Add(-40*24*time.Hour), nil)
	f.runB1 = insertRun(t, d, f.b1, "failed", false, f.now.Add(-3*24*time.Hour), nil)

	// alerts
	insertAlert(t, d, "host_down", "critical", "Host a1 down", `{"host_id":"`+f.a1+`"}`, f.now.Add(-time.Hour))
	insertAlert(t, d, "host_down", "critical", "Host b1 down", `{"host_id":"`+f.b1+`"}`, f.now.Add(-time.Hour))
	insertAlert(t, d, "agent_update", "info", "Agent update available", `{"current_version":"2.0.19"}`, f.now.Add(-time.Hour))
	f.deletedHostAlert = insertAlert(t, d, "host_down", "critical", "Ghost host down", `{"host_id":"`+uuid.NewString()+`"}`, f.now.Add(-time.Hour))
	insertAlert(t, d, "ssh_session_started", "info", "SSH on a1", `{"host_id":"`+f.a1+`"}`, f.now.Add(-time.Hour))
	insertAlert(t, d, "host_down", "warning", "Odd metadata", `{"host_id":42}`, f.now.Add(-time.Hour))

	// packages: openssl shared, catalog latest_version poisoned by b1
	openssl := insertPackage(t, d, "openssl", "9.9.9")
	insertHostPackage(t, d, f.a1, openssl, "3.0.1", "3.0.2", true)
	insertHostPackage(t, d, f.b1, openssl, "3.0.1", "9.9.9", true)
	curl := insertPackage(t, d, "curl", "8.0")
	insertHostPackage(t, d, f.a1, curl, "7.9", "8.0", false) // not security → not in security lists
	for i := 0; i < 20; i++ {
		p := insertPackage(t, d, "lib"+string(rune('a'+i)), "2")
		insertHostPackage(t, d, f.a2, p, "1", "2", true)
	}

	// compliance
	profile := uuid.NewString()
	mustExec(t, d, `INSERT INTO compliance_profiles (id, name, type, created_at, updated_at) VALUES ($1, 'CIS Level 1', 'openscap', NOW(), NOW())`, profile)
	insertScan(t, d, f.a1, profile, 40, 10, 15, f.now.Add(-5*24*time.Hour))
	insertScan(t, d, f.b1, profile, 95, 30, 1, f.now.Add(-5*24*time.Hour))

	// reboots
	f.rebootA1 = insertReboot(t, d, f.a1, "completed", f.now.Add(-6*24*time.Hour))
	insertReboot(t, d, f.b1, "failed", f.now.Add(-6*24*time.Hour))

	// disks: a1 valid, a2 object instead of array, b1 unparsable size
	mustExec(t, d, `UPDATE hosts SET disk_details = $2::jsonb, needs_reboot = true, fork_boot_time = $3 WHERE id = $1`,
		f.a1, `[{"name":"/dev/sda1","size":"49.10GB (46.80GB used, 2.30GB free, 95.3% used)","mountpoint":"/"}]`, f.now.Add(-8*24*time.Hour))
	mustExec(t, d, `UPDATE hosts SET disk_details = $2::jsonb WHERE id = $1`, f.a2, `{"unexpected":"object"}`)
	mustExec(t, d, `UPDATE hosts SET disk_details = $2::jsonb WHERE id = $1`, f.b1, `[{"name":"C:","size":"n/a","mountpoint":"C:\\"}]`)
	return f
}

func allSectionsDef(groups ...string) Definition {
	def, _ := ParseDefinition([]byte(`{"version":2,"period_days":30,"limits":{"top_hosts":50}}`))
	def.Sections = append([]string(nil), KnownSections...)
	def.HostGroupIDs = groups
	return def
}

func collectFor(t *testing.T, d *database.DB, f fixture, def Definition, customer bool) *Model {
	t.Helper()
	ctx := context.Background()
	sc, err := ResolveScope(ctx, d.Queries, def, customer)
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	m, err := Collect(ctx, d, CollectInput{ReportName: "Test", Def: def, Scope: sc, Now: f.now, Location: time.UTC, TimezoneName: "UTC", StaleAfter: 2 * time.Hour})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	return m
}

func TestCollectCustomerReportIsIsolatedToGroupA(t *testing.T) {
	d := newReportsTestDB(t)
	f := buildFixture(t, d)
	m := collectFor(t, d, f, allSectionsDef(f.gA), true)

	inA := map[string]bool{f.a1: true, f.a2: true}
	assertHost := func(section, id string) {
		t.Helper()
		if !inA[id] {
			t.Errorf("%s: host %s is not in group A", section, id)
		}
	}
	if m.HostCount != 2 || !m.CustomerMode || m.FleetWide || len(m.Groups) != 1 || m.Groups[0].Name != "Customer A" {
		t.Fatalf("header %+v", m)
	}
	if m.PeriodFrom != f.now.Add(-30*24*time.Hour) || m.PeriodTo != f.now {
		t.Fatalf("period %v..%v", m.PeriodFrom, m.PeriodTo)
	}

	// executive summary: 1 scanned host (a1, 40) → critical 1, compliant 0; runs in window: a1 completed only
	es := m.ExecutiveSummary
	if es == nil || es.HostCount != 2 || es.ScannedHosts != 1 || es.AverageScore != 40 || es.HostsCritical != 1 || es.HostsCompliant != 0 {
		t.Errorf("executive summary %+v", es)
	}
	if es != nil && (es.RunsTotal != 1 || es.RunsCompleted != 1 || es.RunsFailed != 0) {
		t.Errorf("executive runs %+v (dry run must not count, a2's old run is outside the window, b1 is foreign)", es)
	}

	// compliance
	cs := m.ComplianceSummary
	if cs == nil || cs.ScannedHosts != 1 || cs.Unscanned != 1 || cs.PassedRules != 10 || cs.FailedRules != 15 || len(cs.Worst) != 1 {
		t.Fatalf("compliance %+v", cs)
	}
	assertHost("compliance.worst", cs.Worst[0].HostID)
	if cs.Worst[0].Profile != "CIS Level 1" {
		t.Errorf("profile %q", cs.Worst[0].Profile)
	}

	// recent patch runs: only a1's real run (a2's old one is real too but outside window → still listed, recent list is not windowed)
	rp := m.RecentPatchRuns
	if rp == nil || len(rp.Rows) != 2 {
		t.Fatalf("recent runs %+v", rp)
	}
	for _, r := range rp.Rows {
		assertHost("recent_patch_runs", r.HostID)
		if r.DryRun {
			t.Errorf("recent runs must exclude dry runs")
		}
	}
	if rp.Rows[0].ID != f.runA1 || rp.Rows[0].Packages != "" {
		t.Errorf("first recent run %+v (patch_all without package_names renders as 'all packages')", rp.Rows[0])
	}

	// host status
	if m.HostStatus == nil || len(m.HostStatus.Rows) != 2 {
		t.Fatalf("host status %+v", m.HostStatus)
	}
	for _, r := range m.HostStatus.Rows {
		assertHost("hosts_offline", r.HostID)
	}

	// open alerts: exactly host_down a1 — no global, no ssh, no ghost host, no odd metadata, nothing of b1
	oa := m.OpenAlerts
	if oa == nil || len(oa.Rows) != 1 || oa.Total != 1 || oa.Critical != 1 {
		t.Fatalf("open alerts %+v", oa)
	}
	if oa.Rows[0].HostID != f.a1 || oa.Rows[0].Type != "host_down" {
		t.Errorf("alert row %+v", oa.Rows[0])
	}

	// hosts by updates: a2 (20) before a1 (2)
	hu := m.HostsByUpdates
	if hu == nil || len(hu.Rows) != 2 || hu.Rows[0].HostID != f.a2 || hu.Rows[0].Updates != 20 || hu.Rows[0].SecurityUpdates != 20 || hu.Rows[1].Updates != 2 || hu.Rows[1].SecurityUpdates != 1 {
		t.Fatalf("hosts by updates %+v", hu)
	}

	// top security packages: openssl available 3.0.2 from a1, never 9.9.9 from b1 or the catalog
	tp := m.TopSecurityPackages
	if tp == nil || len(tp.Rows) != 21 {
		t.Fatalf("top packages %+v", tp)
	}
	var openssl *SecurityPackageRow
	for i := range tp.Rows {
		if tp.Rows[i].Name == "openssl" {
			openssl = &tp.Rows[i]
		}
		if tp.Rows[i].Name == "curl" {
			t.Errorf("curl is not a security update")
		}
	}
	if openssl == nil || openssl.AffectedHosts != 1 || len(openssl.AvailableVersions) != 1 || openssl.AvailableVersions[0] != "3.0.2" {
		t.Errorf("openssl %+v", openssl)
	}

	// host overview
	ho := m.HostOverview
	if ho == nil || len(ho.Rows) != 2 {
		t.Fatalf("host overview %+v", ho)
	}
	for _, r := range ho.Rows {
		assertHost("host_overview", r.HostID)
	}
	if r := ho.Rows[0]; r.HostName != "a1" || !r.NeedsReboot || r.BootTime == nil || r.LastRunStatus != "completed" || r.OS != "ubuntu 24.04" {
		t.Errorf("a1 overview %+v", r)
	}

	// security updates by host: a2 capped at 15 with 5 more
	su := m.SecurityUpdatesByHost
	if su == nil || len(su.Hosts) != 2 {
		t.Fatalf("security by host %+v", su)
	}
	if su.Hosts[0].HostName != "a1" || len(su.Hosts[0].Rows) != 1 || su.Hosts[0].Rows[0].Available != "3.0.2" || su.Hosts[0].More != 0 {
		t.Errorf("a1 security %+v", su.Hosts[0])
	}
	if su.Hosts[1].HostName != "a2" || len(su.Hosts[1].Rows) != MaxSecurityUpdatesPerHost || su.Hosts[1].More != 5 {
		t.Errorf("a2 security cap %d more %d", len(su.Hosts[1].Rows), su.Hosts[1].More)
	}

	// disks: a1 parsed critical; a2 (object) skipped
	dl := m.Disks
	if dl == nil || len(dl.Rows) != 1 || dl.Rows[0].HostID != f.a1 || dl.Rows[0].Usage == nil || dl.Rows[0].Level != "critical" || dl.Rows[0].Mount != "/" {
		t.Fatalf("disks %+v", dl)
	}

	// patch activity: a1 real + a1 dry run; a2's run is 40 days old
	pa := m.PatchActivity
	if pa == nil || len(pa.Rows) != 2 || pa.Completed != 1 || pa.Failed != 0 || pa.Truncated {
		t.Fatalf("patch activity %+v", pa)
	}
	dry := 0
	for _, r := range pa.Rows {
		assertHost("patch_activity", r.HostID)
		if r.DryRun {
			dry++
			if r.PackageCount == nil || *r.PackageCount != 1 {
				t.Errorf("dry run package count %+v", r.PackageCount)
			}
		}
	}
	if dry != 1 {
		t.Errorf("expected exactly one dry run, got %d", dry)
	}

	// reboots: only a1
	rb := m.Reboots
	if rb == nil || len(rb.Rows) != 1 || rb.Rows[0].HostID != f.a1 || rb.Rows[0].Status != "sent" {
		t.Fatalf("reboots %+v", rb)
	}
}

func TestCollectInternalFleetWideSeesEverythingIncludingGlobalAlerts(t *testing.T) {
	d := newReportsTestDB(t)
	f := buildFixture(t, d)
	m := collectFor(t, d, f, allSectionsDef(), false)
	if m.HostCount != 4 || !m.FleetWide || m.CustomerMode {
		t.Fatalf("header %+v", m)
	}
	types := map[string]int{}
	for _, r := range m.OpenAlerts.Rows {
		types[r.Type]++
	}
	// host_down a1, host_down b1, agent_update (global), ssh_session_started a1; ghost + odd metadata never resolve to a host
	if types["agent_update"] != 1 || types["host_down"] != 2 || types["ssh_session_started"] != 1 || len(m.OpenAlerts.Rows) != 4 {
		t.Fatalf("fleet-wide alerts %v", types)
	}
	if m.ComplianceSummary.ScannedHosts != 2 || m.ComplianceSummary.Unscanned != 2 {
		t.Fatalf("compliance %+v", m.ComplianceSummary)
	}
	if m.PatchActivity.Completed != 1 || m.PatchActivity.Failed != 1 {
		t.Fatalf("activity %+v", m.PatchActivity)
	}
}

func TestCollectInternalGroupKeepsGlobalAlertsButNotOtherHosts(t *testing.T) {
	d := newReportsTestDB(t)
	f := buildFixture(t, d)
	m := collectFor(t, d, f, allSectionsDef(f.gA), false)
	if m.HostCount != 2 {
		t.Fatalf("host count %d", m.HostCount)
	}
	types := map[string]int{}
	for _, r := range m.OpenAlerts.Rows {
		types[r.Type]++
		if r.HostID == f.b1 {
			t.Errorf("b1 alert leaked into an A report: %+v", r)
		}
	}
	if types["agent_update"] != 1 || types["host_down"] != 1 || types["ssh_session_started"] != 1 {
		t.Fatalf("internal group alerts %v", types)
	}
	if m.ExecutiveSummary.RunsTotal != 1 {
		t.Fatalf("runs %+v", m.ExecutiveSummary)
	}
}

func TestCollectMovedHostBringsItsActivity(t *testing.T) {
	d := newReportsTestDB(t)
	f := buildFixture(t, d)
	// a1 moves from A to B
	mustExec(t, d, `DELETE FROM host_group_memberships WHERE host_id = $1`, f.a1)
	mustExec(t, d, `INSERT INTO host_group_memberships (id, host_id, host_group_id, created_at) VALUES ($1, $2, $3, NOW())`, uuid.NewString(), f.a1, f.gB)
	m := collectFor(t, d, f, allSectionsDef(f.gB), true)
	if m.HostCount != 2 {
		t.Fatalf("host count %d", m.HostCount)
	}
	found := false
	for _, r := range m.PatchActivity.Rows {
		if r.ID == f.runA1 {
			found = true
		}
	}
	if !found {
		t.Fatal("a1's run from before the move must appear in B's report")
	}
}

func TestCollectLimitsApplyAfterTheGroupFilter(t *testing.T) {
	d := newReportsTestDB(t)
	f := buildFixture(t, d)
	// b1 gets the newest run of the whole fleet; with top_hosts=1 the A
	// report must still show an A run, never nothing.
	insertRun(t, d, f.b1, "completed", false, f.now.Add(-time.Hour), nil)
	def := allSectionsDef(f.gA)
	def.Limits.TopHosts = 1
	m := collectFor(t, d, f, def, true)
	if len(m.RecentPatchRuns.Rows) != 1 || m.RecentPatchRuns.Rows[0].HostID != f.a1 {
		t.Fatalf("recent runs %+v", m.RecentPatchRuns.Rows)
	}
	if len(m.HostStatus.Rows) != 1 || len(m.HostsByUpdates.Rows) != 1 || m.HostsByUpdates.Rows[0].HostID != f.a2 {
		t.Fatalf("host lists must be capped after filtering: status %d, by updates %+v", len(m.HostStatus.Rows), m.HostsByUpdates.Rows)
	}
	if len(m.HostOverview.Rows) != 2 || len(m.SecurityUpdatesByHost.Hosts) != 2 {
		t.Fatalf("customer sections are never capped by top_hosts")
	}
	if len(m.OpenAlerts.Rows) != 1 || m.OpenAlerts.Rows[0].HostID != f.a1 {
		t.Fatalf("alerts %+v", m.OpenAlerts.Rows)
	}
}

func TestCollectPatchActivityTruncationKeepsExactCounters(t *testing.T) {
	d := newReportsTestDB(t)
	f := buildFixture(t, d)
	mustExec(t, d, `INSERT INTO patch_runs (id, host_id, job_id, patch_type, status, dry_run, created_at, updated_at)
		SELECT gen_random_uuid()::text, $1, 'job-' || g, 'patch_all', 'completed', false, $2::timestamp - (g || ' minutes')::interval, $2::timestamp
		FROM generate_series(1, 600) g`, f.a1, f.now.Add(-3*24*time.Hour))
	m := collectFor(t, d, f, allSectionsDef(f.gA), true)
	pa := m.PatchActivity
	if !pa.Truncated || len(pa.Rows) != MaxActivityRows {
		t.Fatalf("expected truncation at %d rows, got %d truncated=%v", MaxActivityRows, len(pa.Rows), pa.Truncated)
	}
	if pa.Completed != 601 || pa.Failed != 0 {
		t.Fatalf("counters must cover the whole period, got completed=%d failed=%d", pa.Completed, pa.Failed)
	}
	if m.ExecutiveSummary.RunsCompleted != 601 {
		t.Fatalf("executive summary %+v", m.ExecutiveSummary)
	}
}
