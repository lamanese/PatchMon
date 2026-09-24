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

// assertRowsDrawnAsMeasured fails when any logged table row was drawn taller
// than measured or continued on another page (a mid-row auto page break), or
// crosses the bottom margin.
func assertRowsDrawnAsMeasured(t *testing.T, c *fpdfCanvas) {
	t.Helper()
	_, pageH := c.pdf.GetPageSize()
	for i, r := range c.rowLog {
		if r.endPage != r.page {
			t.Fatalf("row %d starts on page %d but drawing ended on page %d", i, r.page, r.endPage)
		}
		if d := r.drawnBottom - (r.y + r.h); d > 0.01 || d < -0.01 {
			t.Fatalf("row %d on page %d: measured bottom %.2f, drawn bottom %.2f", i, r.page, r.y+r.h, r.drawnBottom)
		}
		if r.y+r.h > pageH-pdfBottom+0.01 {
			t.Fatalf("row %d on page %d starts at %.1f with height %.1f: crosses the bottom margin", i, r.page, r.y, r.h)
		}
	}
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
	assertRowsDrawnAsMeasured(t, c)
}

func TestTableRowsWithMissingGlyphsNeverOverlap(t *testing.T) {
	c := newTestCanvas(t)
	// Noto Sans has neither CJK nor Hebrew glyphs: fpdf's SplitText counted
	// them 0 wide (1 line measured) while MultiCell drew ~3 lines.
	cjk := strings.Repeat("セキュリティ 更新プログラム ", 12)
	hebrew := strings.Repeat("עדכון אבטחה חשוב ", 12)
	cols := []pdfCol{{Title: "Host", W: 0.3}, {Title: "Text", W: 0.7}} // 0.7 × 180 = 126 mm, inner 123 mm
	var rows [][]cell
	for i := 0; i < 90; i++ {
		text := cjk
		if i%2 == 1 {
			text = hebrew
		}
		rows = append(rows, []cell{{Text: "host-" + strings.Repeat("ה", i%5)}, {Text: text}})
	}
	c.table(cols, rows)
	if c.err() != nil {
		t.Fatal(c.err())
	}
	if len(c.rowLog) != len(rows) {
		t.Fatalf("logged %d rows, want %d", len(c.rowLog), len(rows))
	}
	if c.rowLog[0].h <= pdfLineH+2*pdfCellPad {
		t.Fatalf("CJK row measured as a single line (h=%.1f)", c.rowLog[0].h)
	}
	if c.pdf.PageCount() < 2 {
		t.Fatalf("expected the rows to span pages, got %d", c.pdf.PageCount())
	}
	assertRowsDrawnAsMeasured(t, c)
	// consecutive rows on one page must not overlap
	for i := 1; i < len(c.rowLog); i++ {
		p, r := c.rowLog[i-1], c.rowLog[i]
		if p.page == r.page && r.y < p.y+p.h-0.01 {
			t.Fatalf("row %d (y=%.2f) overlaps row %d (ends %.2f)", i, r.y, i-1, p.y+p.h)
		}
	}
	if _, err := c.output(); err != nil {
		t.Fatal(err)
	}
}

func TestWrapLinesNeverExceedsWidth(t *testing.T) {
	c := newTestCanvas(t)
	c.font(false, 8, colText)
	in := "prefix " + strings.Repeat("abcdefghij", 30) + " " + strings.Repeat("セキュリティ 更新 ", 20) + "\nzweite Zeile\n\n"
	lines := wrapLines(c.pdf, in, 40)
	for _, l := range lines {
		if w := c.pdf.GetStringWidth(l); w > 40 {
			t.Fatalf("line wider than 40 mm (%.1f): %q", w, l)
		}
	}
	if lines[len(lines)-1] != "zweite Zeile" {
		t.Fatalf("trailing newlines must be dropped, last line %q", lines[len(lines)-1])
	}
	if got := wrapLines(c.pdf, "", 40); len(got) != 1 || got[0] != "" {
		t.Fatalf("empty text: %q", got)
	}
}

// fillNearBottom draws short rows until the cursor is below y=200 mm, so the
// next tall row cannot fit on the current page.
func fillNearBottom(t *testing.T, c *fpdfCanvas) {
	t.Helper()
	for c.pdf.GetY() <= 200 {
		c.table([]pdfCol{{Title: "Fill", W: 1}}, [][]cell{{{Text: "x"}}, {{Text: "y"}}})
	}
	if c.pdf.PageCount() != 1 {
		t.Fatalf("filler spilled onto page %d", c.pdf.PageCount())
	}
}

func TestTableRowNeverOverflowsPage(t *testing.T) {
	c := newTestCanvas(t)
	fillNearBottom(t, c)
	before := len(c.rowLog)
	cols := []pdfCol{{Title: "A", W: 1}}
	rows := [][]cell{{{Text: strings.Repeat("word ", 400)}}} // ~18 lines, ~80 mm
	c.table(cols, rows)
	if c.err() != nil {
		t.Fatal(c.err())
	}
	_, pageH := c.pdf.GetPageSize()
	r := c.rowLog[before]
	if r.page != 2 || r.y > pdfTop+pdfTableHeadH+0.01 {
		t.Fatalf("tall row not moved to the top of a fresh page (page=%d y=%.1f)", r.page, r.y)
	}
	if r.y+r.h > pageH-20+0.01 {
		t.Fatalf("row crosses the bottom margin (y=%.1f h=%.1f)", r.y, r.h)
	}
	if c.truncatedCells != 0 {
		t.Fatalf("row fitting a page must not be truncated")
	}
	assertRowsDrawnAsMeasured(t, c)
}

func TestTableRowTallerThanAPageIsTruncated(t *testing.T) {
	c := newTestCanvas(t)
	fillNearBottom(t, c)
	before := len(c.rowLog)
	var sb strings.Builder
	for i := 0; i < 80; i++ { // 80 lines, more than a page holds (~57)
		sb.WriteString("line\n")
	}
	c.table([]pdfCol{{Title: "A", W: 1}}, [][]cell{{{Text: sb.String()}}})
	if c.err() != nil {
		t.Fatal(c.err())
	}
	if c.truncatedCells != 1 {
		t.Fatalf("want one truncated cell, got %d", c.truncatedCells)
	}
	_, pageH := c.pdf.GetPageSize()
	r := c.rowLog[before]
	if r.page != 2 {
		t.Fatalf("oversized row not moved to a fresh page (page=%d)", r.page)
	}
	if r.y+r.h > pageH-20+0.01 {
		t.Fatalf("truncated row still crosses the bottom margin (y=%.1f h=%.1f)", r.y, r.h)
	}
	assertRowsDrawnAsMeasured(t, c)
	if _, err := c.output(); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasReplacesRunesOutsideBMP(t *testing.T) {
	m := sampleModel("de", false)
	m.ReportName = "Kunde \U0001F680 X"
	c := newFpdfCanvas(context.Background(), m, Branding{})
	c.title(m.ReportName, [][2]string{{"Zeitraum \U0001F600", "30 Tage \U0001F600"}})
	c.heading("Abschnitt \U0001F4C8")
	c.subheading("Teil \U0001F4C8")
	c.kpis([]kpiItem{{Value: "5\U0001F525", Label: "Hosts \U0001F525"}})
	c.table([]pdfCol{{Title: "Host \U0001F5A5", W: 0.4}, {Title: "Text", W: 0.6}},
		[][]cell{{{Text: "web01 \U0001F525"}, {Text: strings.Repeat("\U0001F680", 200)}}})
	c.note("Hinweis \U0001F44D")
	c.nodata("Keine Daten \U0001F44D")
	out, err := c.output()
	if err != nil {
		t.Fatalf("emoji must not fail the PDF: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-1.")) {
		t.Fatal("not a PDF")
	}
	if got := pdfText("a\U0001F680b\u00e4"); got != "a?b\u00e4" {
		t.Fatalf("pdfText: %q", got)
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
