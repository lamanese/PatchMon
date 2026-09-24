package reports

import (
	"fmt"
	"strings"
	"testing"
)

type recordingCanvas struct {
	ops []string
}

func (r *recordingCanvas) title(name string, meta [][2]string) {
	r.ops = append(r.ops, "title:"+name)
	for _, kv := range meta {
		r.ops = append(r.ops, "meta:"+kv[0]+"="+kv[1])
	}
}
func (r *recordingCanvas) heading(t string)    { r.ops = append(r.ops, "h2:"+t) }
func (r *recordingCanvas) subheading(t string) { r.ops = append(r.ops, "h3:"+t) }
func (r *recordingCanvas) kpis(items []kpiItem) {
	for _, k := range items {
		r.ops = append(r.ops, "kpi:"+k.Label+"="+k.Value+hexOf(k.Color))
	}
}
func (r *recordingCanvas) table(cols []pdfCol, rows [][]cell) {
	var titles []string
	for _, c := range cols {
		titles = append(titles, c.Title)
	}
	r.ops = append(r.ops, "th:"+strings.Join(titles, "|"))
	for _, row := range rows {
		var cells []string
		for _, c := range row {
			cells = append(cells, cellOp(c))
		}
		r.ops = append(r.ops, "tr:"+strings.Join(cells, "|"))
	}
}
func (r *recordingCanvas) note(t string)   { r.ops = append(r.ops, "note:"+t) }
func (r *recordingCanvas) nodata(t string) { r.ops = append(r.ops, "nodata:"+t) }
func (r *recordingCanvas) err() error      { return nil }

// hexOf renders a colour as "#rrggbb"; the zero colour (default text) is "".
func hexOf(c pdfColor) string {
	if c == (pdfColor{}) {
		return ""
	}
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// cellOp records a cell as <text>, <text>#rrggbb or <text>#rrggbb* (bold).
func cellOp(c cell) string {
	s := c.Text + hexOf(c.Color)
	if c.Bold {
		s += "*"
	}
	return s
}

// isFullOp reports whether a check names a whole op (header, row or KPI)
// that must match exactly, so an extra or missing column fails.
func isFullOp(c string) bool {
	return strings.HasPrefix(c, "th:") || strings.HasPrefix(c, "tr:") || strings.HasPrefix(c, "kpi:")
}

// hasSeq reports whether seq occurs as consecutive ops.
func (r *recordingCanvas) hasSeq(seq []string) bool {
	for i := 0; i+len(seq) <= len(r.ops); i++ {
		ok := true
		for j, want := range seq {
			if r.ops[i+j] != want {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func (r *recordingCanvas) hasExact(op string) bool {
	for _, o := range r.ops {
		if o == op {
			return true
		}
	}
	return false
}

func (r *recordingCanvas) has(sub string) bool {
	for _, op := range r.ops {
		if strings.Contains(op, sub) {
			return true
		}
	}
	return false
}

func TestRenderSectionsCoversEverySectionInOrder(t *testing.T) {
	m := sampleModel("de", false)
	m.HostOverview.Rows[1].PkgBroken = true // sampleModel has no host with a broken package state
	rc := &recordingCanvas{}
	renderSections(rc, m)
	var headings []string
	for _, op := range rc.ops {
		if strings.HasPrefix(op, "h2:") {
			headings = append(headings, strings.TrimPrefix(op, "h2:"))
		}
	}
	tx := T("de")
	if len(headings) != len(KnownSections) {
		t.Fatalf("headings %d != sections %d: %v", len(headings), len(KnownSections), headings)
	}
	for i, sec := range KnownSections {
		if headings[i] != tx.S("sec."+sec) {
			t.Fatalf("heading %d: want %q got %q", i, tx.S("sec."+sec), headings[i])
		}
	}
	st := tx.Status
	checks := []string{
		"title:Kunde <b>X</b>", "meta:" + tx.S("hosts") + "=2",
		// one header line per table (eleven sections draw tables)
		"th:" + tx.S("col.host") + "|" + tx.S("col.score") + "|" + tx.S("col.profile") + "|" + tx.S("col.completed"),
		"th:" + tx.S("col.host") + "|" + tx.S("col.status") + "|" + tx.S("col.type") + "|" + tx.S("col.packages") + "|" + tx.S("col.started") + "|" + tx.S("col.completed"),
		"th:" + tx.S("col.host") + "|" + tx.S("col.status") + "|" + tx.S("col.last_seen"),
		"th:" + tx.S("col.severity") + "|" + tx.S("col.title") + "|" + tx.S("col.host") + "|" + tx.S("col.created"),
		"th:" + tx.S("col.host") + "|" + tx.S("col.updates") + "|" + tx.S("col.security_updates") + "|" + tx.S("col.status") + "|" + tx.S("col.last_seen"),
		"th:" + tx.S("col.package") + "|" + tx.S("col.affected_hosts") + "|" + tx.S("col.available"),
		"th:" + tx.S("col.host") + "|" + tx.S("col.os") + "|" + tx.S("col.status") + "|" + tx.S("col.updates") + "|" + tx.S("col.security_updates") + "|" + tx.S("col.reboot_pending") + "|" + tx.S("col.last_boot") + "|" + tx.S("col.last_patch_run") + "|" + tx.S("col.last_seen") + "|" + tx.S("col.agent"),
		"th:" + tx.S("col.package") + "|" + tx.S("col.installed") + "|" + tx.S("col.available"),
		"th:" + tx.S("col.host") + "|" + tx.S("col.disk") + "|" + tx.S("col.mount") + "|" + tx.S("col.size") + "|" + tx.S("col.used"),
		"th:" + tx.S("col.date") + "|" + tx.S("col.host") + "|" + tx.S("col.type") + "|" + tx.S("col.result") + "|" + tx.S("col.package_count"),
		"th:" + tx.S("col.date") + "|" + tx.S("col.host") + "|" + tx.S("col.result"),
		// rows, every column in order
		"tr:<script>alert(1)</script>|40.5%|CIS|24.09.2026 12:00",
		"tr:web\" onmouseover=\"x|" + st("completed") + "#16a34a*|" + st("patch_all") + "|" + tx.S("val.all_packages") + "|24.09.2026 12:00|24.09.2026 12:00",
		"tr:web01|" + st("inactive") + "#94a3b8*|24.09.2026 11:00",
		"tr:critical#dc2626*|Host down & out|web01|24.09.2026 12:00",
		"tr:web01|5|2|" + st("active") + "#16a34a*|24.09.2026 11:00",
		"tr:openssl|1|3.0.2",
		"tr:web01|ubuntu 24.04|" + st("active") + "#16a34a*|5|2|" + tx.S("val.yes") + "|21.09.2026 12:00|" + st("completed") + " (24.09.2026 12:00)|24.09.2026 11:00|2.0.20",
		"tr:db01\n" + tx.S("val.pkg_broken") + "|windows 24H2|" + st("active") + "#16a34a*|0|0|" + tx.S("val.no") + "|" + tx.F("val.uptime_reported", "3 days, 2 hours") + "|" + tx.S("val.none") + "|" + tx.S("val.never") + "|-",
		"h3:web01", "tr:openssl|3.0.1|3.0.2", "note:" + tx.F("val.and_n_more", 4),
		"tr:web01|/dev/sda1|/|50.0 GB|96.0%#dc2626*", "tr:db01|C:|C:\\|-|n/a",
		"tr:24.09.2026 12:00|web01|" + st("patch_all") + " – " + tx.S("val.dry_run") + "|" + st("completed") + "#16a34a*|2",
		"tr:24.09.2026 12:00|web01|" + st("patch_all") + "|" + st("failed") + "#dc2626*|" + tx.S("val.not_determined"),
		"tr:24.09.2026 12:00|web01|" + tx.S("val.reboot_sent") + "#16a34a*",
		"tr:24.09.2026 12:00|web01|" + tx.S("val.reboot_not_delivered") + " – Agent not connected#dc2626*",
		"note:" + tx.S("note.windows_boot"), "note:" + tx.S("note.activity"), "note:" + tx.S("note.reboot"),
	}
	// KPI groups: each one in order, right after its heading, with colours
	k := func(key, val string) string { return "kpi:" + tx.S("kpi."+key) + "=" + val }
	kpiGroups := [][]string{
		{"h2:" + tx.S("sec.executive_summary"),
			k("total_hosts", "2#2563eb"), k("avg_compliance", "40.5%#dc2626"), k("critical_hosts", "1#dc2626"), k("compliant_hosts", "0#16a34a"),
			"h3:" + tx.S("sec.patching_overview") + " – " + tx.PeriodLabel(30),
			k("runs_total", "3#6366f1"), k("runs_completed", "2#16a34a"), k("runs_failed", "1#dc2626"), k("runs_running", "0#d97706")},
		{"h2:" + tx.S("sec.compliance_summary"),
			k("passed_rules", "10#16a34a"), k("failed_rules", "15#dc2626"), k("critical_hosts", "1#dc2626"), k("unscanned", "1#94a3b8")},
		{"h2:" + tx.S("sec.open_alerts"),
			k("alerts_total", "1#6366f1"), k("alerts_critical", "1#dc2626"), k("alerts_error", "0#16a34a"), k("alerts_warning", "0#d97706")},
		{"h2:" + tx.S("sec.patch_activity"), k("runs_completed", "0#16a34a"), k("runs_failed", "1#dc2626")},
	}
	for _, g := range kpiGroups {
		if !rc.hasSeq(g) {
			t.Errorf("missing KPI sequence %q\nops:\n%s", g, strings.Join(rc.ops, "\n"))
		}
	}
	for _, c := range checks {
		if !rc.has(c) || (isFullOp(c) && !rc.hasExact(c)) {
			t.Errorf("missing %q\nops:\n%s", c, strings.Join(rc.ops, "\n"))
		}
	}
}

func TestRenderSectionsEnglishAndEmptySections(t *testing.T) {
	m := sampleModel("en", true)
	m.ComplianceSummary.Worst = nil
	m.RecentPatchRuns.Rows = nil
	m.Disks = nil // nil pointer is skipped like {{with}}
	rc := &recordingCanvas{}
	renderSections(rc, m)
	tx := T("en")
	if !rc.has("meta:"+tx.S("period")+"="+tx.PeriodLabel(30)) || !rc.has("2026-09-24 12:00") {
		t.Fatalf("english formats missing:\n%s", strings.Join(rc.ops, "\n"))
	}
	if n := strings.Count(strings.Join(rc.ops, "\n"), "nodata:"+tx.S("val.no_data")); n < 2 {
		t.Fatalf("expected no-data lines for the emptied sections, got %d", n)
	}
	if rc.has("h2:" + tx.S("sec.disks")) {
		t.Fatal("nil section must be skipped")
	}
	for _, op := range rc.ops {
		if strings.Contains(op, "https://") || strings.Contains(op, "pm.example.com") {
			t.Fatalf("customer PDF must not carry URLs: %s", op)
		}
	}
}
