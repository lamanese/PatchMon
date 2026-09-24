package reports

import (
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
		r.ops = append(r.ops, "kpi:"+k.Label+"="+k.Value)
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
			cells = append(cells, c.Text)
		}
		r.ops = append(r.ops, "tr:"+strings.Join(cells, "|"))
	}
}
func (r *recordingCanvas) note(t string)   { r.ops = append(r.ops, "note:"+t) }
func (r *recordingCanvas) nodata(t string) { r.ops = append(r.ops, "nodata:"+t) }
func (r *recordingCanvas) err() error      { return nil }

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
	checks := []string{
		"title:Kunde <b>X</b>", "meta:" + tx.S("hosts") + "=2",
		"kpi:" + tx.S("kpi.total_hosts") + "=2", "kpi:" + tx.S("kpi.avg_compliance") + "=40.5%",
		"tr:<script>alert(1)</script>|40.5%|CIS|24.09.2026 12:00",
		"|" + tx.Status("completed") + "|" + tx.Status("patch_all") + "|" + tx.S("val.all_packages") + "|",
		"tr:web01|" + tx.Status("inactive") + "|24.09.2026 11:00",
		"tr:critical|Host down & out|web01|",
		"tr:openssl|1|3.0.2",
		tx.S("val.pkg_broken"), tx.F("val.uptime_reported", "3 days, 2 hours"), tx.S("val.none"),
		"h3:web01", "tr:openssl|3.0.1|3.0.2", "note:" + tx.F("val.and_n_more", 4),
		"tr:web01|/dev/sda1|/|50.0 GB|96.0%", "tr:db01|C:|C:\\|-|n/a",
		tx.Status("patch_all") + " – " + tx.S("val.dry_run"), "|" + tx.S("val.not_determined"),
		tx.S("val.reboot_sent"), tx.S("val.reboot_not_delivered") + " – Agent not connected",
		"note:" + tx.S("note.windows_boot"), "note:" + tx.S("note.activity"), "note:" + tx.S("note.reboot"),
	}
	for _, c := range checks {
		if !rc.has(c) {
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
