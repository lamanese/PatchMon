package reports

import (
	"context"
	"encoding/csv"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRenderCSVShapeAndEscaping(t *testing.T) {
	m := sampleModel("en", true)
	m.OpenAlerts.Rows[0].Title = `Quote " and, comma`
	out := RenderCSV(m)
	rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatalf("csv must parse back: %v\n%s", err, out)
	}
	if strings.Join(rows[0], ",") != "section,metric,value" {
		t.Fatalf("header %v", rows[0])
	}
	want := map[string]bool{
		"report|host_count|2":                                           false,
		"executive_summary|average_score|40.5":                          false,
		"executive_summary|runs_total|3":                                false,
		"compliance|hosts_critical|1":                                   false,
		"recent_patch_run|r1|completed|patch_all|web\" onmouseover=\"x": false,
		"host|h1|inactive":                                              false,
		"open_alert|a1|critical|Quote \" and, comma":                    false,
		"hosts_by_updates|web01|5|2":                                    false,
		"top_security_package|openssl|1|3.0.2":                          false,
		"host_overview|web01|5|2|yes|2026-09-21T10:00:00Z":              false,
		"security_update|web01|openssl|3.0.1|3.0.2":                     false,
		"disk|web01|/|96.0":                                             false,
		"patch_activity|r2|completed|patch_all|web01|dry_run|2":         false,
		"reboot|web01|sent":                                             false,
	}
	for _, r := range rows[1:] {
		if len(r) != 3 {
			t.Fatalf("row %v has %d columns", r, len(r))
		}
		key := strings.Join(r, "|")
		for k := range want {
			if strings.HasPrefix(key, k) {
				want[k] = true
			}
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("missing row %q\n%s", k, out)
		}
	}
}

func TestRenderCSVNilSectionsDoNotPanic(t *testing.T) {
	m := &Model{ReportName: "x", Language: "en", Sections: KnownSections, GeneratedAt: time.Now()}
	if out := RenderCSV(m); !strings.HasPrefix(out, "section,metric,value") {
		t.Fatalf("got %q", out)
	}
}

func TestBuildRejectsInvalidDefinitionBeforeTouchingTheDatabase(t *testing.T) {
	_, err := Build(context.Background(), nil, BuildInput{ReportName: "x", Definition: []byte(`{"language":"fr"}`), Now: time.Now()})
	if !errors.Is(err, ErrDefinition) {
		t.Fatalf("got %v", err)
	}
}

func TestSubjectPerLanguage(t *testing.T) {
	if got := Subject("de", "Kunde X"); got != "amanIT Patch-Bericht: Kunde X" {
		t.Fatalf("de subject %q", got)
	}
	if got := Subject("en", "Fleet"); got != "amanIT Patch Report: Fleet" {
		t.Fatalf("en subject %q", got)
	}
}

func TestResolveLocationFallsBackToUTC(t *testing.T) {
	loc, name := ResolveLocation("Not/AZone")
	if loc != time.UTC || name != "UTC" {
		t.Fatalf("got %v %q", loc, name)
	}
	loc, name = ResolveLocation("")
	if loc != time.UTC || name != "UTC" {
		t.Fatalf("empty: got %v %q", loc, name)
	}
	if loc, name := ResolveLocation("Europe/Zurich"); loc == nil || name != "Europe/Zurich" || loc.String() != "Europe/Zurich" {
		t.Fatalf("zurich: got %v %q", loc, name)
	}
}
