package reports

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"codeberg.org/go-pdf/fpdf"
)

const (
	pdfLeft, pdfRight, pdfTop, pdfBottom = 15.0, 15.0, 28.0, 20.0
	pdfUsable                            = 210 - pdfLeft - pdfRight
	pdfLineH                             = 4.2
	pdfCellPad                           = 1.5
	pdfFont                              = "NotoSans"
	pdfKeepTogether                      = 20.0 // mm a heading needs below it
	pdfTableHeadH                        = 6.0
	pdfKPIGap                            = 3.0
	pdfKPIH                              = 16.0
	pdfCtxEvery                          = 25 // table rows between context checks
	pdfEllipsis                          = "…"
)

// fpdfCanvas implements canvas on top of codeberg.org/go-pdf/fpdf: A4
// portrait, embedded Noto Sans, logo header and page footer on every page.
type fpdfCanvas struct {
	ctx      context.Context
	pdf      *fpdf.Fpdf
	tx       Texts
	loc      *time.Location
	m        *Model
	firstErr error
	logoSrc  string
	logoW    float64
	logoH    float64
	logoOK   bool

	// test-visible bookkeeping
	rowLog         []rowLogEntry
	headerDraws    int
	truncatedCells int // table cells cut to fit one page
}

// fpdfCanvas must satisfy canvas (compile-time check).
var _ canvas = (*fpdfCanvas)(nil)

type rowLogEntry struct {
	page int
	y, h float64
}

// newFpdfCanvas sets up the document (fonts, metadata, logo, header/footer)
// and opens the first page. Errors are kept and surface through err().
func newFpdfCanvas(ctx context.Context, m *Model, b Branding) *fpdfCanvas {
	pdf := fpdf.New("P", "mm", "A4", "")
	c := &fpdfCanvas{ctx: ctx, pdf: pdf, tx: T(m.Language), loc: m.Location, m: m}
	pdf.SetMargins(pdfLeft, pdfTop, pdfRight)
	pdf.SetAutoPageBreak(true, pdfBottom)
	pdf.AliasNbPages("")
	pdf.SetCompression(true)
	pdf.SetCatalogSort(true) // deterministic object order
	pdf.AddUTF8FontFromBytes(pdfFont, "", fontRegular)
	pdf.AddUTF8FontFromBytes(pdfFont, "B", fontBold)
	pdf.SetTitle(Subject(m.Language, m.ReportName), true)
	pdf.SetAuthor(BrandName, true)
	// Producer and creator are plain ASCII: written as Latin-1 so the info
	// dictionary reads "(PatchMon)" instead of a UTF-16 byte string.
	pdf.SetCreator("PatchMon", false)
	pdf.SetProducer("PatchMon", false)
	pdf.SetCreationDate(m.GeneratedAt)
	pdf.SetModificationDate(m.GeneratedAt)

	img, typ, src := selectLogo(b)
	c.logoSrc = src
	if info := pdf.RegisterImageOptionsReader("logo", fpdf.ImageOptions{ImageType: typ}, bytes.NewReader(img)); info != nil && info.Width() > 0 && info.Height() > 0 {
		c.logoH = 10
		c.logoW = c.logoH * info.Width() / info.Height()
		if c.logoW > 45 {
			c.logoW = 45
			c.logoH = 45 * info.Height() / info.Width()
		}
		c.logoOK = true
	}

	pdf.SetHeaderFunc(c.drawPageHeader)
	pdf.SetFooterFunc(c.drawPageFooter)
	if !c.checkCtx() {
		return c
	}
	pdf.AddPage()
	c.font(false, 9, colText)
	return c
}

func (c *fpdfCanvas) drawPageHeader() {
	pdf := c.pdf
	if c.logoOK {
		pdf.ImageOptions("logo", pdfLeft, 10, c.logoW, c.logoH, false, fpdf.ImageOptions{}, 0, "")
	}
	c.font(true, 10, colHead)
	pdf.SetXY(110, 10)
	pdf.CellFormat(85, 5, fitText(pdf, c.m.ReportName, 85-2*pdf.GetCellMargin()), "", 0, "R", false, 0, "")
	c.font(false, 8, colMuted)
	pdf.SetXY(110, 15.5)
	gen := fmt.Sprintf("%s: %s (%s)", c.tx.S("pdf.generated"), c.tx.DateTime(c.m.GeneratedAt, c.loc), c.m.TimezoneName)
	pdf.CellFormat(85, 4, fitText(pdf, gen, 85-2*pdf.GetCellMargin()), "", 0, "R", false, 0, "")
	c.rule(24)
	pdf.SetY(pdfTop)
}

func (c *fpdfCanvas) drawPageFooter() {
	pdf := c.pdf
	pdf.SetY(-15)
	y := pdf.GetY()
	c.font(false, 8, colMuted)
	pdf.SetX(pdfLeft)
	pdf.CellFormat(pdfUsable/2, 5, pdfText(BrandName), "", 0, "L", false, 0, "")
	pdf.SetXY(pdfLeft+pdfUsable/2, y)
	pdf.CellFormat(pdfUsable/2, 5, pdfText(c.tx.F("pdf.page_of", pdf.PageNo(), "{nb}")), "", 0, "R", false, 0, "")
}

// font selects Noto Sans (bold or regular) at size pt in colour col.
func (c *fpdfCanvas) font(bold bool, size float64, col pdfColor) {
	style := ""
	if bold {
		style = "B"
	}
	c.pdf.SetFont(pdfFont, style, size)
	c.pdf.SetTextColor(col.R, col.G, col.B)
}

// rule draws a full-width hairline at y.
func (c *fpdfCanvas) rule(y float64) {
	c.pdf.SetDrawColor(colRule.R, colRule.G, colRule.B)
	c.pdf.SetLineWidth(0.2)
	c.pdf.Line(pdfLeft, y, pdfLeft+pdfUsable, y)
}

func (c *fpdfCanvas) pageH() float64 {
	_, h := c.pdf.GetPageSize()
	return h
}

// keepTogether starts a new page when less than need mm remain.
func (c *fpdfCanvas) keepTogether(need float64) {
	if c.pdf.GetY() > c.pageH()-pdfBottom-need {
		c.pdf.AddPage()
	}
}

func (c *fpdfCanvas) checkCtx() bool {
	if c.firstErr != nil {
		return false
	}
	if err := c.ctx.Err(); err != nil {
		c.firstErr = err
		return false
	}
	return true
}

func (c *fpdfCanvas) err() error {
	if c.firstErr != nil {
		return c.firstErr
	}
	return c.pdf.Error()
}

func (c *fpdfCanvas) logoSource() string { return c.logoSrc }

func (c *fpdfCanvas) title(name string, meta [][2]string) {
	if !c.checkCtx() {
		return
	}
	pdf := c.pdf
	c.font(true, 16, colHead)
	pdf.SetX(pdfLeft)
	pdf.MultiCell(pdfUsable, 7.5, hardWrap(pdf, name, pdfUsable-2*pdf.GetCellMargin()), "", "L", false)
	pdf.Ln(1)
	for _, row := range meta {
		y := pdf.GetY()
		c.font(true, 8, colText)
		pdf.SetXY(pdfLeft, y)
		pdf.CellFormat(35, pdfLineH, fitText(pdf, row[0], 35-2*pdf.GetCellMargin()), "", 0, "L", false, 0, "")
		c.font(false, 8, colText)
		pdf.SetXY(pdfLeft+35, y)
		pdf.MultiCell(pdfUsable-35, pdfLineH, hardWrap(pdf, row[1], pdfUsable-35-2*pdf.GetCellMargin()), "", "L", false)
	}
	pdf.Ln(4)
}

func (c *fpdfCanvas) heading(text string) {
	if !c.checkCtx() {
		return
	}
	pdf := c.pdf
	c.keepTogether(pdfKeepTogether)
	c.font(true, 12, colHead)
	pdf.SetX(pdfLeft)
	pdf.MultiCell(pdfUsable, 6, hardWrap(pdf, text, pdfUsable-2*pdf.GetCellMargin()), "", "L", false)
	pdf.Ln(2)
	c.rule(pdf.GetY())
	pdf.Ln(3)
}

func (c *fpdfCanvas) subheading(text string) {
	if !c.checkCtx() {
		return
	}
	pdf := c.pdf
	c.keepTogether(pdfKeepTogether)
	c.font(true, 10, colHead)
	pdf.SetX(pdfLeft)
	pdf.MultiCell(pdfUsable, 5, hardWrap(pdf, text, pdfUsable-2*pdf.GetCellMargin()), "", "L", false)
	pdf.Ln(1.5)
}

func (c *fpdfCanvas) kpis(items []kpiItem) {
	if !c.checkCtx() {
		return
	}
	pdf := c.pdf
	w := (pdfUsable - 3*pdfKPIGap) / 4
	for start := 0; start < len(items); start += 4 {
		if pdf.GetY()+pdfKPIH > c.pageH()-pdfBottom {
			pdf.AddPage()
		}
		y := pdf.GetY()
		end := min(start+4, len(items))
		for i, it := range items[start:end] {
			x := pdfLeft + float64(i)*(w+pdfKPIGap)
			pdf.SetFillColor(colStripe.R, colStripe.G, colStripe.B)
			pdf.Rect(x, y, w, pdfKPIH, "F")
			c.font(true, 13, orText(it.Color))
			pdf.SetXY(x+1, y+1.5)
			pdf.CellFormat(w-2, 7, fitText(pdf, it.Value, w-2-2*pdf.GetCellMargin()), "", 0, "L", false, 0, "")
			c.font(false, 7, colMuted)
			pdf.SetXY(x+1, y+10)
			pdf.CellFormat(w-2, 4, fitText(pdf, strings.ToUpper(it.Label), w-2-2*pdf.GetCellMargin()), "", 0, "L", false, 0, "")
		}
		pdf.SetXY(pdfLeft, y+pdfKPIH+4)
	}
	c.font(false, 9, colText)
}

func (c *fpdfCanvas) table(cols []pdfCol, rows [][]cell) {
	if !c.checkCtx() || len(cols) == 0 {
		return
	}
	pdf := c.pdf
	cw := make([]float64, len(cols))
	for i, col := range cols {
		cw[i] = pdfUsable * col.W
	}
	drawHeader := func() {
		y := pdf.GetY()
		pdf.SetFillColor(colTHBg.R, colTHBg.G, colTHBg.B)
		pdf.Rect(pdfLeft, y, pdfUsable, pdfTableHeadH, "F")
		c.font(true, 7.5, colMuted)
		x := pdfLeft
		for i, col := range cols {
			pdf.SetXY(x+pdfCellPad-pdf.GetCellMargin(), y)
			inner := cw[i] - 2*pdfCellPad
			pdf.CellFormat(inner+2*pdf.GetCellMargin(), pdfTableHeadH, fitText(pdf, strings.ToUpper(col.Title), inner), "", 0, alignOf(col.Align), false, 0, "")
			x += cw[i]
		}
		c.rule(y + pdfTableHeadH)
		pdf.SetXY(pdfLeft, y+pdfTableHeadH)
		c.headerDraws++
	}

	pageH := c.pageH()
	// the tallest row that still fits a fresh page below the table header
	maxLines := int((pageH - pdfTop - pdfBottom - pdfTableHeadH - 2*pdfCellPad) / pdfLineH)

	if pdf.GetY()+pdfTableHeadH+pdfLineH+2*pdfCellPad > pageH-pdfBottom {
		pdf.AddPage()
	}
	drawHeader()

	lines := make([][]string, len(cols))
	for r, row := range rows {
		if r > 0 && r%pdfCtxEvery == 0 && !c.checkCtx() {
			return
		}
		n := 1
		for i := range cols {
			lines[i] = nil
			if i >= len(row) {
				continue
			}
			inner := cw[i] - 2*pdfCellPad
			c.font(row[i].Bold, 8, colText)
			ls := pdf.SplitText(hardWrap(pdf, row[i].Text, inner-2*pdf.GetCellMargin()), inner)
			if len(ls) > maxLines {
				ls = ls[:maxLines]
				c.truncatedCells++
				ls[maxLines-1] = fitText(pdf, ls[maxLines-1]+pdfEllipsis, inner-2*pdf.GetCellMargin())
			}
			lines[i] = ls
			n = max(n, len(ls))
		}
		rowH := float64(n)*pdfLineH + 2*pdfCellPad
		y := pdf.GetY()
		if y+rowH > pageH-pdfBottom {
			pdf.AddPage()
			drawHeader()
			y = pdf.GetY()
		}
		c.rowLog = append(c.rowLog, rowLogEntry{page: pdf.PageNo(), y: y, h: rowH})
		if r%2 == 1 {
			pdf.SetFillColor(colStripe.R, colStripe.G, colStripe.B)
			pdf.Rect(pdfLeft, y, pdfUsable, rowH, "F")
		}
		x := pdfLeft
		for i, col := range cols {
			if i < len(row) && len(lines[i]) > 0 {
				c.font(row[i].Bold, 8, orText(row[i].Color))
				pdf.SetXY(x+pdfCellPad, y+pdfCellPad)
				pdf.MultiCell(cw[i]-2*pdfCellPad, pdfLineH, strings.Join(lines[i], "\n"), "", alignOf(col.Align), false)
			}
			x += cw[i]
		}
		c.rule(y + rowH)
		pdf.SetXY(pdfLeft, y+rowH)
	}
	pdf.Ln(4)
	c.font(false, 9, colText)
}

func (c *fpdfCanvas) note(text string) {
	if !c.checkCtx() {
		return
	}
	pdf := c.pdf
	c.font(false, 7.5, colMuted)
	pdf.SetX(pdfLeft)
	pdf.MultiCell(pdfUsable, 3.8, hardWrap(pdf, text, pdfUsable-2*pdf.GetCellMargin()), "", "L", false)
	pdf.Ln(1)
	c.font(false, 9, colText)
}

func (c *fpdfCanvas) nodata(text string) {
	if !c.checkCtx() {
		return
	}
	pdf := c.pdf
	c.font(false, 9, colMuted)
	pdf.SetX(pdfLeft)
	pdf.MultiCell(pdfUsable, pdfLineH, hardWrap(pdf, text, pdfUsable-2*pdf.GetCellMargin()), "", "L", false)
	pdf.Ln(2)
	c.font(false, 9, colText)
}

// output closes the document and returns the PDF bytes, or the first error.
func (c *fpdfCanvas) output() ([]byte, error) {
	if err := c.err(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := c.pdf.Output(&buf); err != nil {
		return nil, err
	}
	if c.pdf.Err() {
		return nil, c.pdf.Error()
	}
	return buf.Bytes(), nil
}

// pdfReplacement stands in for runes fpdf cannot encode.
const pdfReplacement = '?'

// pdfText makes s safe for fpdf's UTF-8 fonts: their width table covers the
// Basic Multilingual Plane only, and fpdf fails the whole document on any rune
// above U+FFFF (emoji and the like). Such runes become '?'. Every string that
// reaches fpdf passes through here (directly or via hardWrap/fitText).
func pdfText(s string) string {
	for _, r := range s {
		if r > 0xFFFF {
			return strings.Map(func(r rune) rune {
				if r > 0xFFFF {
					return pdfReplacement
				}
				return r
			}, s)
		}
	}
	return s
}

// hardWrap inserts '\n' into tokens wider than w (in the current font) so
// that no single token overflows a cell. '\r' is dropped, '\t' becomes a
// space, runes above U+FFFF become '?' (pdfText); short text is returned
// unchanged.
func hardWrap(pdf *fpdf.Fpdf, s string, w float64) string {
	s = pdfText(s)
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", " ")
	lines := strings.Split(s, "\n")
	for li, line := range lines {
		toks := strings.Split(line, " ")
		for ti, tok := range toks {
			if tok == "" || pdf.GetStringWidth(tok) <= w {
				continue
			}
			var chunks []string
			cur := ""
			for _, r := range tok {
				cand := cur + string(r)
				if cur != "" && pdf.GetStringWidth(cand) > w {
					chunks = append(chunks, cur)
					cur = string(r)
					continue
				}
				cur = cand
			}
			if cur != "" {
				chunks = append(chunks, cur)
			}
			toks[ti] = strings.Join(chunks, "\n")
		}
		lines[li] = strings.Join(toks, " ")
	}
	return strings.Join(lines, "\n")
}

// fitText truncates s with "…" so that it is at most w wide.
func fitText(pdf *fpdf.Fpdf, s string, w float64) string {
	s = pdfText(s)
	if pdf.GetStringWidth(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && pdf.GetStringWidth(string(r)+pdfEllipsis) > w {
		r = r[:len(r)-1]
	}
	return string(r) + pdfEllipsis
}

func orText(col pdfColor) pdfColor {
	if col == (pdfColor{}) {
		return colText
	}
	return col
}

func alignOf(a string) string {
	switch a {
	case "R", "C":
		return a
	}
	return "L"
}
