package reports

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MaxPDFBytes is the hard cap for one rendered report (spec §4.4).
const MaxPDFBytes = 10 << 20

var maxPDFBytes = MaxPDFBytes

// ErrPDFTooLarge is returned instead of a truncated document.
var ErrPDFTooLarge = errors.New("pdf exceeds 10 MB")

// newCanvas builds the drawing surface; a variable so a test can inject a
// broken canvas and prove that RenderPDF recovers from a library panic.
var newCanvas = newFpdfCanvas

// RenderPDF draws the model as an A4 document. It is deterministic for equal
// input, stops when ctx ends and never embeds a server URL. A panic inside
// the PDF library is turned into an error (defence in depth: the inputs are
// sanitised, but one bad document must never take the server down).
func RenderPDF(ctx context.Context, m *Model, b Branding) (pdf []byte, logoSrc string, err error) {
	if m == nil {
		return nil, "", fmt.Errorf("render pdf: nil model")
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	defer func() {
		if r := recover(); r != nil {
			pdf, logoSrc, err = nil, "", fmt.Errorf("render pdf: panic: %v", r)
		}
	}()
	c := newCanvas(ctx, m, b)
	renderSections(c, m)
	out, err := c.output()
	if err != nil {
		return nil, "", fmt.Errorf("render pdf: %w", err)
	}
	if len(out) > maxPDFBytes {
		return nil, "", fmt.Errorf("render pdf: %d bytes: %w", len(out), ErrPDFTooLarge)
	}
	return out, c.logoSource(), nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// PDFFileName builds the download name: report-<slug>-<yyyymmdd>.pdf.
func PDFFileName(reportName string, generatedAt time.Time) string {
	slug := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(reportName), "-"), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	if slug == "" {
		slug = "report"
	}
	return "report-" + slug + "-" + generatedAt.UTC().Format("20060102") + ".pdf"
}

// PDF palette, identical to the HTML FuncMap colours.
var (
	pdfBlue   = hexRGB("#2563eb")
	pdfIndigo = hexRGB("#6366f1")
	pdfGreen  = hexRGB("#16a34a")
	pdfAmber  = hexRGB("#d97706")
	pdfRed    = hexRGB("#dc2626")
	pdfGrey   = hexRGB("#94a3b8")
)

// pdfSections maps a section id to its renderer. Each renderer skips a nil
// section (like {{with}} in the HTML template).
var pdfSections = map[string]func(canvas, Texts, *Model){
	SectionExecutiveSummary:      pdfExecutiveSummary,
	SectionComplianceSummary:     pdfComplianceSummary,
	SectionRecentPatchRuns:       pdfRecentPatchRuns,
	SectionHostStatus:            pdfHostsOffline,
	SectionOpenAlerts:            pdfOpenAlerts,
	SectionHostsByUpdates:        pdfHostsByUpdates,
	SectionTopSecurityPackages:   pdfTopSecurityPackages,
	SectionHostOverview:          pdfHostOverview,
	SectionSecurityUpdatesByHost: pdfSecurityUpdatesByHost,
	SectionDisks:                 pdfDisks,
	SectionPatchActivity:         pdfPatchActivity,
	SectionReboots:               pdfReboots,
}

// renderSections draws the title block and every section in m.Sections order.
func renderSections(c canvas, m *Model) {
	tx := T(m.Language)
	groups := tx.S("fleet_wide")
	if !m.FleetWide {
		names := make([]string, 0, len(m.Groups))
		for _, g := range m.Groups {
			names = append(names, g.Name)
		}
		groups = strings.Join(names, ", ")
	}
	c.title(m.ReportName, [][2]string{
		{tx.S("period"), tx.PeriodLabel(m.PeriodDays) + " (" + pdfDT(tx, m, m.PeriodFrom) + " – " + pdfDT(tx, m, m.PeriodTo) + ")"},
		{tx.S("timezone"), m.TimezoneName},
		{tx.S("groups"), groups},
		{tx.S("hosts"), strconv.Itoa(m.HostCount)},
	})
	for _, sec := range m.Sections {
		if c.err() != nil {
			return
		}
		if fn, ok := pdfSections[sec]; ok {
			fn(c, tx, m)
		}
	}
}

// --- value helpers (mirror the HTML FuncMap) ---

func pdfDT(tx Texts, m *Model, t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return tx.DateTime(t, m.Location)
}

func pdfDTP(tx Texts, m *Model, t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return tx.DateTime(*t, m.Location)
}

func pdfNever(tx Texts, m *Model, t *time.Time) string {
	if t == nil {
		return tx.S("val.never")
	}
	return pdfDTP(tx, m, t)
}

func pdfPct(v float64) string { return fmt.Sprintf("%.1f%%", v) }

func pdfOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func pdfZeroGreen(n int) pdfColor {
	if n == 0 {
		return pdfGreen
	}
	return pdfRed
}

func pdfScoreColor(v float64) pdfColor {
	switch {
	case v >= complianceCompliantMin:
		return pdfGreen
	case v >= complianceWarningMin:
		return pdfAmber
	}
	return pdfRed
}

func pdfLevelColor(level string) pdfColor {
	switch level {
	case "critical":
		return pdfRed
	case "warn":
		return pdfAmber
	}
	return pdfGreen
}

func pdfBadge(tx Texts, status string) cell {
	return cell{Text: tx.Status(status), Color: hexRGB(statusColor(status).FG), Bold: true}
}

func txt(s string) cell { return cell{Text: s} }

func num(n int) cell { return cell{Text: strconv.Itoa(n)} }

func kpi(n int, label string, col pdfColor) kpiItem {
	return kpiItem{Value: strconv.Itoa(n), Label: label, Color: col}
}

func col(tx Texts, key string, w float64, align string) pdfCol {
	return pdfCol{Title: tx.S("col." + key), W: w, Align: align}
}

// --- sections ---

func pdfExecutiveSummary(c canvas, tx Texts, m *Model) {
	s := m.ExecutiveSummary
	if s == nil {
		return
	}
	c.heading(tx.S("sec.executive_summary"))
	c.kpis([]kpiItem{
		kpi(s.HostCount, tx.S("kpi.total_hosts"), pdfBlue),
		{Value: pdfPct(s.AverageScore), Label: tx.S("kpi.avg_compliance"), Color: pdfScoreColor(s.AverageScore)},
		kpi(s.HostsCritical, tx.S("kpi.critical_hosts"), pdfZeroGreen(s.HostsCritical)),
		kpi(s.HostsCompliant, tx.S("kpi.compliant_hosts"), pdfGreen),
	})
	c.subheading(tx.S("sec.patching_overview") + " – " + tx.PeriodLabel(m.PeriodDays))
	c.kpis([]kpiItem{
		kpi(s.RunsTotal, tx.S("kpi.runs_total"), pdfIndigo),
		kpi(s.RunsCompleted, tx.S("kpi.runs_completed"), pdfGreen),
		kpi(s.RunsFailed, tx.S("kpi.runs_failed"), pdfZeroGreen(s.RunsFailed)),
		kpi(s.RunsRunning, tx.S("kpi.runs_running"), pdfAmber),
	})
}

func pdfComplianceSummary(c canvas, tx Texts, m *Model) {
	s := m.ComplianceSummary
	if s == nil {
		return
	}
	c.heading(tx.S("sec.compliance_summary"))
	c.kpis([]kpiItem{
		kpi(s.PassedRules, tx.S("kpi.passed_rules"), pdfGreen),
		kpi(s.FailedRules, tx.S("kpi.failed_rules"), pdfZeroGreen(s.FailedRules)),
		kpi(s.HostsCritical, tx.S("kpi.critical_hosts"), pdfZeroGreen(s.HostsCritical)),
		kpi(s.Unscanned, tx.S("kpi.unscanned"), pdfGrey),
	})
	if len(s.Worst) == 0 {
		c.nodata(tx.S("val.no_data"))
		return
	}
	c.subheading(tx.S("sec.worst_hosts"))
	rows := make([][]cell, 0, len(s.Worst))
	for _, r := range s.Worst {
		score := "-"
		if r.Score != nil {
			score = pdfPct(*r.Score)
		}
		rows = append(rows, []cell{txt(r.HostName), txt(score), txt(r.Profile), txt(pdfDT(tx, m, r.CompletedAt))})
	}
	c.table([]pdfCol{col(tx, "host", 0.4, "L"), col(tx, "score", 0.15, "R"), col(tx, "profile", 0.25, "L"), col(tx, "completed", 0.2, "L")}, rows)
}

func pdfRecentPatchRuns(c canvas, tx Texts, m *Model) {
	s := m.RecentPatchRuns
	if s == nil {
		return
	}
	c.heading(tx.S("sec.recent_patch_runs"))
	if len(s.Rows) == 0 {
		c.nodata(tx.S("val.no_data"))
		return
	}
	rows := make([][]cell, 0, len(s.Rows))
	for _, r := range s.Rows {
		pkgs := r.Packages
		if pkgs == "" {
			pkgs = tx.S("val.all_packages")
		}
		started := pdfDT(tx, m, r.CreatedAt)
		if r.StartedAt != nil {
			started = pdfDTP(tx, m, r.StartedAt)
		}
		rows = append(rows, []cell{txt(r.HostName), pdfBadge(tx, r.Status), txt(tx.Status(r.PatchType)), txt(pkgs), txt(started), txt(pdfDTP(tx, m, r.CompletedAt))})
	}
	c.table([]pdfCol{
		// status is wider than the brief's 0.13: bold "Abgeschlossen" broke mid-word
		col(tx, "host", 0.2, "L"), col(tx, "status", 0.15, "L"), col(tx, "type", 0.13, "L"),
		col(tx, "packages", 0.25, "L"), col(tx, "started", 0.135, "L"), col(tx, "completed", 0.135, "L"),
	}, rows)
}

func pdfHostsOffline(c canvas, tx Texts, m *Model) {
	s := m.HostStatus
	if s == nil {
		return
	}
	c.heading(tx.S("sec.hosts_offline"))
	if len(s.Rows) == 0 {
		c.nodata(tx.S("val.no_data"))
		return
	}
	rows := make([][]cell, 0, len(s.Rows))
	for _, r := range s.Rows {
		rows = append(rows, []cell{txt(r.HostName), pdfBadge(tx, r.Status), txt(pdfNever(tx, m, r.LastSeen))})
	}
	c.table([]pdfCol{col(tx, "host", 0.5, "L"), col(tx, "status", 0.2, "L"), col(tx, "last_seen", 0.3, "L")}, rows)
}

func pdfOpenAlerts(c canvas, tx Texts, m *Model) {
	s := m.OpenAlerts
	if s == nil {
		return
	}
	c.heading(tx.S("sec.open_alerts"))
	c.kpis([]kpiItem{
		kpi(s.Total, tx.S("kpi.alerts_total"), pdfIndigo),
		kpi(s.Critical, tx.S("kpi.alerts_critical"), pdfZeroGreen(s.Critical)),
		kpi(s.Error, tx.S("kpi.alerts_error"), pdfZeroGreen(s.Error)),
		kpi(s.Warning, tx.S("kpi.alerts_warning"), pdfAmber),
	})
	if len(s.Rows) == 0 {
		c.nodata(tx.S("val.no_data"))
		return
	}
	rows := make([][]cell, 0, len(s.Rows))
	for _, r := range s.Rows {
		sev := cell{Text: r.Severity, Color: hexRGB(severityColor(r.Severity).FG), Bold: true}
		rows = append(rows, []cell{sev, txt(r.Title), txt(pdfOrDash(r.HostName)), txt(pdfDT(tx, m, r.CreatedAt))})
	}
	c.table([]pdfCol{col(tx, "severity", 0.14, "L"), col(tx, "title", 0.46, "L"), col(tx, "host", 0.2, "L"), col(tx, "created", 0.2, "L")}, rows)
}

func pdfHostsByUpdates(c canvas, tx Texts, m *Model) {
	s := m.HostsByUpdates
	if s == nil {
		return
	}
	c.heading(tx.S("sec.hosts_by_updates"))
	if len(s.Rows) == 0 {
		c.nodata(tx.S("val.no_data"))
		return
	}
	rows := make([][]cell, 0, len(s.Rows))
	for _, r := range s.Rows {
		rows = append(rows, []cell{txt(r.HostName), num(r.Updates), num(r.SecurityUpdates), pdfBadge(tx, r.Status), txt(pdfNever(tx, m, r.LastSeen))})
	}
	c.table([]pdfCol{col(tx, "host", 0.34, "L"), col(tx, "updates", 0.13, "R"), col(tx, "security_updates", 0.15, "R"), col(tx, "status", 0.15, "L"), col(tx, "last_seen", 0.23, "L")}, rows)
}

func pdfTopSecurityPackages(c canvas, tx Texts, m *Model) {
	s := m.TopSecurityPackages
	if s == nil {
		return
	}
	c.heading(tx.S("sec.top_security_packages"))
	if len(s.Rows) == 0 {
		c.nodata(tx.S("val.no_data"))
		return
	}
	rows := make([][]cell, 0, len(s.Rows))
	for _, r := range s.Rows {
		rows = append(rows, []cell{txt(r.Name), num(r.AffectedHosts), txt(pdfOrDash(strings.Join(r.AvailableVersions, ", ")))})
	}
	c.table([]pdfCol{col(tx, "package", 0.5, "L"), col(tx, "affected_hosts", 0.2, "R"), col(tx, "available", 0.3, "L")}, rows)
}

func pdfHostOverview(c canvas, tx Texts, m *Model) {
	s := m.HostOverview
	if s == nil {
		return
	}
	c.heading(tx.S("sec.host_overview"))
	if len(s.Rows) == 0 {
		c.nodata(tx.S("val.no_data"))
		return
	}
	rows := make([][]cell, 0, len(s.Rows))
	for _, r := range s.Rows {
		host := r.HostName
		if r.PkgBroken {
			host += "\n" + tx.S("val.pkg_broken")
		}
		reboot := tx.S("val.no")
		if r.NeedsReboot {
			reboot = tx.S("val.yes")
		}
		boot := "-"
		switch {
		case r.BootTime != nil:
			boot = pdfDTP(tx, m, r.BootTime)
		case r.Uptime != "":
			boot = tx.F("val.uptime_reported", r.Uptime)
		}
		lastRun := tx.S("val.none")
		if r.LastRunStatus != "" {
			lastRun = tx.Status(r.LastRunStatus) + " (" + pdfDTP(tx, m, r.LastRunAt) + ")"
		}
		rows = append(rows, []cell{
			txt(host), txt(r.OS), pdfBadge(tx, r.Status), num(r.Updates), num(r.SecurityUpdates),
			txt(reboot), txt(boot), txt(lastRun), txt(pdfNever(tx, m, r.LastSeen)), txt(pdfOrDash(r.AgentVersion)),
		})
	}
	c.table([]pdfCol{
		// ten columns on A4 portrait: widths are balanced so that dates and
		// "Abgeschlossen" never break mid-word (the brief's split did)
		col(tx, "host", 0.14, "L"), col(tx, "os", 0.13, "L"), col(tx, "status", 0.08, "L"),
		col(tx, "updates", 0.06, "R"), col(tx, "security_updates", 0.06, "R"), col(tx, "reboot_pending", 0.07, "L"),
		col(tx, "last_boot", 0.115, "L"), col(tx, "last_patch_run", 0.14, "L"), col(tx, "last_seen", 0.115, "L"),
		col(tx, "agent", 0.09, "L"),
	}, rows)
	c.note(tx.S("note.windows_boot"))
}

func pdfSecurityUpdatesByHost(c canvas, tx Texts, m *Model) {
	s := m.SecurityUpdatesByHost
	if s == nil {
		return
	}
	c.heading(tx.S("sec.security_updates_by_host"))
	if len(s.Hosts) == 0 {
		c.nodata(tx.S("val.no_data"))
		return
	}
	for _, h := range s.Hosts {
		c.subheading(h.HostName)
		rows := make([][]cell, 0, len(h.Rows))
		for _, r := range h.Rows {
			rows = append(rows, []cell{txt(r.Package), txt(r.Installed), txt(pdfOrDash(r.Available))})
		}
		if len(rows) == 0 {
			c.nodata(tx.S("val.no_data"))
		} else {
			c.table([]pdfCol{col(tx, "package", 0.4, "L"), col(tx, "installed", 0.3, "L"), col(tx, "available", 0.3, "L")}, rows)
		}
		if h.More > 0 {
			c.note(tx.F("val.and_n_more", h.More))
		}
	}
}

func pdfDisks(c canvas, tx Texts, m *Model) {
	s := m.Disks
	if s == nil {
		return
	}
	c.heading(tx.S("sec.disks"))
	if len(s.Rows) == 0 {
		c.nodata(tx.S("val.no_data"))
		return
	}
	rows := make([][]cell, 0, len(s.Rows))
	for _, r := range s.Rows {
		size, used := txt("-"), txt(pdfOrDash(r.Raw))
		if r.Usage != nil {
			size = txt(fmt.Sprintf("%.1f GB", r.Usage.TotalGB))
			used = cell{Text: pdfPct(r.Usage.UsedPercent), Color: pdfLevelColor(r.Level), Bold: true}
		}
		rows = append(rows, []cell{txt(r.HostName), txt(r.Name), txt(r.Mount), size, used})
	}
	c.table([]pdfCol{col(tx, "host", 0.25, "L"), col(tx, "disk", 0.2, "L"), col(tx, "mount", 0.25, "L"), col(tx, "size", 0.15, "R"), col(tx, "used", 0.15, "R")}, rows)
}

func pdfPatchActivity(c canvas, tx Texts, m *Model) {
	s := m.PatchActivity
	if s == nil {
		return
	}
	c.heading(tx.S("sec.patch_activity"))
	c.kpis([]kpiItem{
		kpi(s.Completed, tx.S("kpi.runs_completed"), pdfGreen),
		kpi(s.Failed, tx.S("kpi.runs_failed"), pdfZeroGreen(s.Failed)),
	})
	if len(s.Rows) == 0 {
		c.nodata(tx.S("val.no_data"))
		return
	}
	rows := make([][]cell, 0, len(s.Rows))
	for _, r := range s.Rows {
		typ := tx.Status(r.PatchType)
		if r.DryRun {
			typ += " – " + tx.S("val.dry_run")
		}
		count := tx.S("val.not_determined")
		if r.PackageCount != nil {
			count = strconv.Itoa(*r.PackageCount)
		}
		rows = append(rows, []cell{txt(pdfDT(tx, m, r.CreatedAt)), txt(r.HostName), txt(typ), pdfBadge(tx, r.Status), txt(count)})
	}
	c.table([]pdfCol{col(tx, "date", 0.17, "L"), col(tx, "host", 0.28, "L"), col(tx, "type", 0.2, "L"), col(tx, "result", 0.15, "L"), col(tx, "package_count", 0.2, "R")}, rows)
	if s.Truncated {
		c.note(tx.F("val.more_rows", len(s.Rows)))
	}
	c.note(tx.S("note.activity"))
}

func pdfReboots(c canvas, tx Texts, m *Model) {
	s := m.Reboots
	if s == nil {
		return
	}
	c.heading(tx.S("sec.reboots"))
	if len(s.Rows) == 0 {
		c.nodata(tx.S("val.no_data"))
		return
	}
	rows := make([][]cell, 0, len(s.Rows))
	for _, r := range s.Rows {
		var res cell
		switch r.Status {
		case "sent":
			res = cell{Text: tx.S("val.reboot_sent"), Color: hexRGB(statusColor("sent").FG), Bold: true}
		case "not_delivered":
			res = cell{Text: tx.S("val.reboot_not_delivered"), Color: hexRGB(statusColor("not_delivered").FG), Bold: true}
			if r.Error != "" {
				res.Text += " – " + r.Error
			}
		default:
			res = txt(r.Status)
		}
		rows = append(rows, []cell{txt(pdfDT(tx, m, r.CreatedAt)), txt(pdfOrDash(r.HostName)), res})
	}
	c.table([]pdfCol{col(tx, "date", 0.25, "L"), col(tx, "host", 0.35, "L"), col(tx, "result", 0.4, "L")}, rows)
	if s.Truncated {
		c.note(tx.F("val.more_rows", len(s.Rows)))
	}
	c.note(tx.S("note.reboot"))
}
