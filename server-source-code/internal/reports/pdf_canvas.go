package reports

import (
	"html/template"
	"strconv"
	"strings"
)

// pdfColor is an RGB colour for the PDF renderer (0-255 per channel).
type pdfColor struct{ R, G, B int }

// Palette of the PDF report, aligned with the HTML report.
var (
	colText   = pdfColor{51, 65, 85}    // #334155
	colMuted  = pdfColor{100, 116, 139} // #64748b
	colHead   = pdfColor{15, 23, 42}    // #0f172a
	colStripe = pdfColor{248, 250, 252} // #f8fafc
	colRule   = pdfColor{226, 232, 240} // #e2e8f0
	colTHBg   = pdfColor{241, 245, 249} // #f1f5f9
)

// hexRGB converts a "#rrggbb" CSS colour (as used by statusColor and
// severityColor) to a pdfColor. Anything else yields colText.
func hexRGB(css template.CSS) pdfColor {
	s := strings.TrimSpace(string(css))
	if len(s) != 7 || s[0] != '#' {
		return colText
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return colText
	}
	return pdfColor{int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff)}
}

// cell is one table cell. A zero Color means colText.
type cell struct {
	Text  string
	Color pdfColor
	Bold  bool
}

// kpiItem is one KPI box (big value, small uppercase label).
type kpiItem struct {
	Value, Label string
	Color        pdfColor
}

// pdfCol describes a table column. W is the fraction of the usable width (all
// columns sum to 1.0); Align is "L", "R" or "C".
type pdfCol struct {
	Title string
	W     float64
	Align string
}

// canvas is the drawing surface the PDF section renderers write to. It keeps
// the renderers independent of the PDF library.
type canvas interface {
	// title draws the report name and meta rows (label, value).
	title(name string, meta [][2]string)
	// heading draws a section h2 and keeps the next 20 mm together.
	heading(text string)
	// subheading draws an h3 with the same keep-together rule.
	subheading(text string)
	// kpis draws up to four boxes per row; more wrap to a new row.
	kpis(items []kpiItem)
	// table draws a table; the header repeats per page and rows never split.
	table(cols []pdfCol, rows [][]cell)
	// note draws a small grey line.
	note(text string)
	// nodata draws a "no data" line.
	nodata(text string)
	// err returns the first error (context or PDF library).
	err() error
}
