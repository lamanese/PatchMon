package reports

import (
	"sort"
	"strings"
	"testing"
	"time"
)

func TestTextsHaveIdenticalKeySets(t *testing.T) {
	de := texts["de"]
	en := texts["en"]
	if len(de) == 0 || len(en) == 0 {
		t.Fatal("empty text tables")
	}
	keys := func(m map[string]string) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	dk, ek := keys(de), keys(en)
	if strings.Join(dk, "\n") != strings.Join(ek, "\n") {
		t.Fatalf("key sets differ\nde: %v\nen: %v", dk, ek)
	}
	for k, v := range de {
		if strings.TrimSpace(v) == "" {
			t.Errorf("de[%q] empty", k)
		}
		if strings.Contains(v, "ß") {
			t.Errorf("de[%q] uses ß, Swiss spelling wants ss", k)
		}
	}
	for _, k := range []string{"sec." + SectionExecutiveSummary, "sec." + SectionReboots, "col.host", "val.dry_run", "note.reboot"} {
		if _, ok := en[k]; !ok {
			t.Errorf("missing required key %q", k)
		}
	}
}

func TestTextsLookupAndFallback(t *testing.T) {
	if got := T("de").S("val.yes"); got != "Ja" {
		t.Fatalf("de val.yes = %q", got)
	}
	if got := T("xx").S("val.yes"); got != "Yes" {
		t.Fatalf("unknown language should fall back to en, got %q", got)
	}
	if got := T("en").S("does.not.exist"); got != "[[does.not.exist]]" {
		t.Fatalf("missing key marker = %q", got)
	}
}

func TestDateFormatsPerLanguage(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		t.Skip("tzdata not available")
	}
	tm := time.Date(2026, 9, 24, 13, 5, 0, 0, time.UTC) // 15:05 CEST
	if got := T("de").DateTime(tm, loc); got != "24.09.2026 15:05" {
		t.Fatalf("de datetime = %q", got)
	}
	if got := T("en").DateTime(tm, loc); got != "2026-09-24 15:05" {
		t.Fatalf("en datetime = %q", got)
	}
	if got := T("de").Date(tm, loc); got != "24.09.2026" {
		t.Fatalf("de date = %q", got)
	}
	if got := T("en").Date(tm, nil); got != "2026-09-24" {
		t.Fatalf("nil location must mean UTC, got %q", got)
	}
	if got := T("de").PeriodLabel(30); got != "Letzte 30 Tage" {
		t.Fatalf("de period = %q", got)
	}
	if got := T("en").PeriodLabel(7); got != "Last 7 days" {
		t.Fatalf("en period = %q", got)
	}
}

func TestParseDiskSize(t *testing.T) {
	u, ok := ParseDiskSize("49.10GB (12.30GB used, 36.80GB free, 26.5% used)")
	if !ok {
		t.Fatal("expected parse ok")
	}
	if u.TotalGB != 49.10 || u.UsedGB != 12.30 || u.FreeGB != 36.80 || u.UsedPercent != 26.5 {
		t.Fatalf("%+v", u)
	}
	for _, raw := range []string{"", "n/a", "49GB", "49.10GB (12.30GB used)"} {
		if _, ok := ParseDiskSize(raw); ok {
			t.Errorf("%q should not parse", raw)
		}
	}
}

func TestDiskLevel(t *testing.T) {
	cases := map[float64]string{0: "", 84.9: "", 85: "warn", 94.9: "warn", 95: "critical", 100: "critical"}
	for pct, want := range cases {
		if got := DiskLevel(pct); got != want {
			t.Errorf("DiskLevel(%v) = %q, want %q", pct, got, want)
		}
	}
}
