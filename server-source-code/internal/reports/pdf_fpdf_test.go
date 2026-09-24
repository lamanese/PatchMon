package reports

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func newTestCanvas(t *testing.T) *fpdfCanvas {
	t.Helper()
	c := newFpdfCanvas(context.Background(), sampleModel("de", false), Branding{})
	if c.err() != nil {
		t.Fatal(c.err())
	}
	return c
}

func TestHexRGB(t *testing.T) {
	if got := hexRGB("#dc2626"); got != (pdfColor{220, 38, 38}) {
		t.Fatalf("got %+v", got)
	}
	if got := hexRGB("red"); got != colText {
		t.Fatalf("fallback: got %+v", got)
	}
}

func TestHardWrapBreaksLongTokens(t *testing.T) {
	c := newTestCanvas(t)
	long := strings.Repeat("abcdefghij", 30) // 300 chars, no spaces
	out := hardWrap(c.pdf, "prefix "+long+" suffix", 40)
	for _, line := range strings.Split(out, "\n") {
		for _, tok := range strings.Split(line, " ") {
			if w := c.pdf.GetStringWidth(tok); w > 40 {
				t.Fatalf("token wider than cell: %.1f mm %q", w, tok)
			}
		}
	}
	if !strings.HasPrefix(out, "prefix ") || !strings.HasSuffix(out, " suffix") {
		t.Fatalf("surrounding words lost: %q", out)
	}
	if hardWrap(c.pdf, "short words only", 40) != "short words only" {
		t.Fatal("short text must be untouched")
	}
}

func TestTableRepeatsHeaderAndKeepsRowsWhole(t *testing.T) {
	c := newTestCanvas(t)
	cols := []pdfCol{{Title: "Host", W: 0.3}, {Title: "Text", W: 0.7}}
	var rows [][]cell
	for i := 0; i < 120; i++ {
		rows = append(rows, []cell{{Text: "host"}, {Text: strings.Repeat("line ", 40)}}) // 3–4 lines each
	}
	c.table(cols, rows)
	if c.err() != nil {
		t.Fatal(c.err())
	}
	if c.pdf.PageCount() < 3 {
		t.Fatalf("expected several pages, got %d", c.pdf.PageCount())
	}
	// every row start recorded by the canvas must leave room for the whole row on its page
	_, pageH := c.pdf.GetPageSize()
	for _, r := range c.rowLog {
		if r.y+r.h > pageH-20+0.01 {
			t.Fatalf("row on page %d starts at %.1f with height %.1f: crosses the bottom margin", r.page, r.y, r.h)
		}
	}
	if c.headerDraws < c.pdf.PageCount() {
		t.Fatalf("table header drawn %d times on %d pages", c.headerDraws, c.pdf.PageCount())
	}
}

func TestTableRowNeverOverflowsPage(t *testing.T) {
	c := newTestCanvas(t)
	cols := []pdfCol{{Title: "A", W: 1}}
	rows := [][]cell{{{Text: strings.Repeat("word ", 400)}}} // taller than half a page
	c.table(cols, rows)
	if c.err() != nil {
		t.Fatal(c.err())
	}
	for _, r := range c.rowLog {
		_, pageH := c.pdf.GetPageSize()
		if r.y+r.h > pageH-20+0.01 {
			t.Fatalf("oversized row not moved to a fresh page (y=%.1f h=%.1f)", r.y, r.h)
		}
	}
}

func TestCanvasStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := newFpdfCanvas(ctx, sampleModel("en", false), Branding{})
	cancel()
	rows := make([][]cell, 200)
	for i := range rows {
		rows[i] = []cell{{Text: "x"}}
	}
	c.table([]pdfCol{{Title: "A", W: 1}}, rows)
	if !errors.Is(c.err(), context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", c.err())
	}
	if _, err := c.output(); !errors.Is(err, context.Canceled) {
		t.Fatalf("output must surface the context error, got %v", err)
	}
}

func TestCanvasOutputIsAPDFWithMetadata(t *testing.T) {
	c := newTestCanvas(t)
	c.title("Kunde X", [][2]string{{"Zeitraum", "Letzte 30 Tage"}})
	c.heading("Abschnitt")
	c.kpis([]kpiItem{{Value: "5", Label: "Hosts", Color: colHead}})
	c.note("Hinweis")
	c.nodata("Keine Daten")
	out, err := c.output()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-1.")) {
		t.Fatalf("not a PDF: %q", out[:8])
	}
	if !bytes.Contains(out, []byte("/Producer (PatchMon)")) || !bytes.Contains(out, []byte("/CreationDate (D:20260924100000")) {
		t.Fatal("metadata missing or not deterministic")
	}
	if c.logoSource() != LogoSourceDefault {
		t.Fatalf("logo source: %s", c.logoSource())
	}
}
