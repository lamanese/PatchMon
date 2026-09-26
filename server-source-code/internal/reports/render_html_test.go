package reports

import (
	"strings"
	"testing"
	"time"
)

func sampleModel(lang string, customer bool) *Model {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	loc, _ := time.LoadLocation("Europe/Zurich")
	if loc == nil {
		loc = time.UTC
	}
	score := 40.5
	seen := now.Add(-time.Hour)
	boot := now.Add(-72 * time.Hour)
	n := 2
	m := &Model{
		ReportName: "Kunde <b>X</b>", Language: lang, Location: loc, TimezoneName: "Europe/Zurich",
		GeneratedAt: now, PeriodDays: 30, PeriodFrom: now.Add(-30 * 24 * time.Hour), PeriodTo: now,
		Groups: []GroupRef{{ID: "g1", Name: "Group \"A\""}}, CustomerMode: customer, HostCount: 2,
		Sections:         append([]string(nil), KnownSections...),
		ExecutiveSummary: &ExecutiveSummary{HostCount: 2, ScannedHosts: 1, AverageScore: 40.5, HostsCritical: 1, RunsTotal: 3, RunsCompleted: 2, RunsFailed: 1},
		ComplianceSummary: &ComplianceSummary{PassedRules: 10, FailedRules: 15, HostsCritical: 1, Unscanned: 1, ScannedHosts: 1, AverageScore: 40.5,
			Worst: []ComplianceRow{{HostID: "h1", HostName: "<script>alert(1)</script>", Profile: "CIS", Score: &score, Passed: 10, Failed: 15, CompletedAt: now}}},
		RecentPatchRuns:     &PatchRunList{Rows: []PatchRunRow{{ID: "r1", HostID: "h1", HostName: "web\" onmouseover=\"x", Status: "completed", PatchType: "patch_all", CreatedAt: now, CompletedAt: &now}}},
		HostStatus:          &HostStatusList{Rows: []HostStatusRow{{HostID: "h1", HostName: "web01", Status: "inactive", LastSeen: &seen}}},
		OpenAlerts:          &OpenAlerts{Total: 1, Critical: 1, Rows: []AlertRow{{ID: "a1", Type: "host_down", Severity: "critical", Title: "Host down & out", HostID: "h1", HostName: "web01", CreatedAt: now}}},
		HostsByUpdates:      &HostUpdateList{Rows: []HostUpdateRow{{HostID: "h1", HostName: "web01", Status: "active", Updates: 5, SecurityUpdates: 2, LastSeen: &seen}}},
		TopSecurityPackages: &SecurityPackageList{Rows: []SecurityPackageRow{{Name: "openssl", AffectedHosts: 1, AvailableVersions: []string{"3.0.2"}}}},
		HostOverview: &HostOverview{Rows: []HostOverviewRow{{HostID: "h1", HostName: "web01", OS: "ubuntu 24.04", AgentVersion: "2.0.20", Status: "active", Updates: 5, SecurityUpdates: 2, NeedsReboot: true, BootTime: &boot, LastRunStatus: "completed", LastRunAt: &now, LastSeen: &seen},
			{HostID: "h2", HostName: "db01", OS: "windows 24H2", Status: "active", Uptime: "3 days, 2 hours"}}},
		SecurityUpdatesByHost: &SecurityUpdatesByHost{Hosts: []HostSecurityUpdates{{HostID: "h1", HostName: "web01", Rows: []SecurityUpdateRow{{Package: "openssl", Installed: "3.0.1", Available: "3.0.2"}}, More: 4}}},
		Disks:                 &DiskList{Rows: []DiskRow{{HostID: "h1", HostName: "web01", Name: "/dev/sda1", Mount: "/", Raw: "x", Usage: &DiskUsage{TotalGB: 50, UsedGB: 48, FreeGB: 2, UsedPercent: 96}, Level: "critical"}, {HostID: "h2", HostName: "db01", Name: "C:", Mount: "C:\\", Raw: "n/a"}}},
		PatchActivity:         &PatchActivity{Rows: []PatchRunRow{{ID: "r2", HostID: "h1", HostName: "web01", Status: "completed", PatchType: "patch_all", DryRun: true, PackageCount: &n, CreatedAt: now}, {ID: "r3", HostID: "h1", HostName: "web01", Status: "failed", PatchType: "patch_all", CreatedAt: now}}, Completed: 0, Failed: 1},
		Reboots:               &RebootList{Rows: []RebootRow{{HostID: "h1", HostName: "web01", Status: "sent", CreatedAt: now}, {HostID: "h1", HostName: "web01", Status: "not_delivered", Error: "Agent not connected", CreatedAt: now}}},
	}
	return m
}

func TestRenderHTMLEscapesHostileValues(t *testing.T) {
	out, err := RenderHTML(sampleModel("en", false), Branding{ServerURL: "https://pm.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"<script>", `onmouseover="x`, "<b>X</b>"} {
		if strings.Contains(out, bad) {
			t.Errorf("unescaped %q in output", bad)
		}
	}
	for _, good := range []string{"&lt;script&gt;", "Host down &amp; out", "Group &#34;A&#34;"} {
		if !strings.Contains(out, good) {
			t.Errorf("expected escaped %q in output", good)
		}
	}
	if !strings.Contains(out, `href="https://pm.example.com/hosts/h1"`) {
		t.Error("internal report must link the host")
	}
	if strings.Contains(out, "ZgotmplZ") {
		t.Error("html/template sanitised a trusted style; mark it template.CSS")
	}
}

func TestRenderHTMLCustomerModeHasNoLinksAndNoServerURL(t *testing.T) {
	out, err := RenderHTML(sampleModel("de", true), Branding{ServerURL: "https://pm.example.com", LogoURL: "https://pm.example.com/logo.png"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "pm.example.com") {
		t.Error("customer report must not contain the server URL")
	}
	// The only link a customer report may carry is the vendor link in the footer.
	if strings.Count(out, "href=") != 1 || !strings.Contains(out, `href="`+BrandURL+`"`) {
		t.Errorf("customer report must contain exactly the vendor link, got %d href(s)", strings.Count(out, "href="))
	}
}

func TestRenderHTMLFooterNamesTheVendorNotTheUI(t *testing.T) {
	for _, lang := range []string{"de", "en"} {
		out, err := RenderHTML(sampleModel(lang, false), Branding{ServerURL: "https://pm.example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, `<a href="`+BrandURL+`"`) || !strings.Contains(out, BrandName) {
			t.Errorf("%s: footer must link %s as %s", lang, BrandURL, BrandName)
		}
		if strings.Contains(out, "/reporting") {
			t.Errorf("%s: footer must not link the PatchMon UI", lang)
		}
	}
}

func TestRenderHTMLLogoOnlyWhenUploaded(t *testing.T) {
	// No uploaded logo: never point at the logos endpoint (it answers 404
	// and mail clients show a broken image); show the wordmark instead.
	out, err := RenderHTML(sampleModel("de", false), Branding{ServerURL: "https://pm.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "/api/v1/settings/logos") || strings.Contains(out, "<img") {
		t.Error("no image without an uploaded logo")
	}
	if !strings.Contains(out, ">"+BrandName+"<") {
		t.Error("wordmark expected in the header")
	}
	out, err = RenderHTML(sampleModel("de", false), Branding{ServerURL: "https://pm.example.com", LogoURL: "https://pm.example.com/api/v1/settings/logos/light"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `<img src="https://pm.example.com/api/v1/settings/logos/light"`) {
		t.Error("uploaded logo must be referenced")
	}
}

func TestRenderHTMLLanguageAndFormats(t *testing.T) {
	de, err := RenderHTML(sampleModel("de", false), Branding{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Letzte 30 Tage", "24.09.2026 12:00", "Testlauf", "Neustartbefehl gesendet", "Nicht zugestellt", "… und 4 weitere", "alle Pakete", "Uptime laut letzter Meldung: 3 days, 2 hours", "Compliance-Übersicht", "Europe/Zurich"} {
		if !strings.Contains(de, want) {
			t.Errorf("de output lacks %q", want)
		}
	}
	en, err := RenderHTML(sampleModel("en", false), Branding{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Last 30 days", "2026-09-24 12:00", "Dry run", "Reboot command sent", "… and 4 more", "96.0%"} {
		if !strings.Contains(en, want) {
			t.Errorf("en output lacks %q", want)
		}
	}
	if strings.Contains(en, "[[") {
		t.Errorf("missing text key marker in output")
	}
}

func TestRenderHTMLEmptySections(t *testing.T) {
	m := &Model{ReportName: "Empty", Language: "en", Location: time.UTC, TimezoneName: "UTC", GeneratedAt: time.Now(), PeriodDays: 7,
		Sections: append([]string(nil), KnownSections...), FleetWide: true,
		ExecutiveSummary: &ExecutiveSummary{}, ComplianceSummary: &ComplianceSummary{}, RecentPatchRuns: &PatchRunList{}, HostStatus: &HostStatusList{},
		OpenAlerts: &OpenAlerts{}, HostsByUpdates: &HostUpdateList{}, TopSecurityPackages: &SecurityPackageList{}, HostOverview: &HostOverview{},
		SecurityUpdatesByHost: &SecurityUpdatesByHost{}, Disks: &DiskList{}, PatchActivity: &PatchActivity{}, Reboots: &RebootList{}}
	out, err := RenderHTML(m, Branding{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "No data for this period.") < 5 || !strings.Contains(out, "All hosts") {
		t.Errorf("empty sections should render the no-data text; got %d occurrences", strings.Count(out, "No data for this period."))
	}
	// a model with a section listed but nil data must not panic
	m.Disks = nil
	if _, err := RenderHTML(m, Branding{}); err != nil {
		t.Fatalf("nil section data: %v", err)
	}
}
