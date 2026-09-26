package reports

import (
	"encoding/csv"
	"fmt"
	"strings"
	"time"
)

// RenderCSV renders the model as the flat "section,metric,value" table the
// webhook/ntfy deliveries have always received. Instants are RFC3339 UTC.
func RenderCSV(m *Model) string {
	var sb strings.Builder
	w := csv.NewWriter(&sb)
	row := func(section, metric string, value ...string) {
		_ = w.Write([]string{section, metric, strings.Join(value, "|")})
	}
	row("section", "metric", "value")
	if m == nil {
		w.Flush()
		return sb.String()
	}
	ts := func(t time.Time) string { return t.UTC().Format(time.RFC3339) }
	tsp := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return ts(*t)
	}
	yesno := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}

	row("report", "name", m.ReportName)
	row("report", "generated_at", ts(m.GeneratedAt))
	row("report", "period_days", fmt.Sprintf("%d", m.PeriodDays))
	row("report", "host_count", fmt.Sprintf("%d", m.HostCount))
	for _, g := range m.Groups {
		row("report", "host_group", g.Name)
	}

	for _, sec := range m.Sections {
		switch sec {
		case SectionExecutiveSummary:
			if s := m.ExecutiveSummary; s != nil {
				row("executive_summary", "total_hosts", fmt.Sprintf("%d", s.HostCount))
				row("executive_summary", "scanned_hosts", fmt.Sprintf("%d", s.ScannedHosts))
				row("executive_summary", "average_score", fmt.Sprintf("%.1f", s.AverageScore))
				row("executive_summary", "hosts_critical", fmt.Sprintf("%d", s.HostsCritical))
				row("executive_summary", "hosts_compliant", fmt.Sprintf("%d", s.HostsCompliant))
				row("executive_summary", "runs_total", fmt.Sprintf("%d", s.RunsTotal))
				row("executive_summary", "runs_completed", fmt.Sprintf("%d", s.RunsCompleted))
				row("executive_summary", "runs_failed", fmt.Sprintf("%d", s.RunsFailed))
				row("executive_summary", "runs_running", fmt.Sprintf("%d", s.RunsRunning))
			}
		case SectionComplianceSummary:
			if s := m.ComplianceSummary; s != nil {
				row("compliance", "total_passed_rules", fmt.Sprintf("%d", s.PassedRules))
				row("compliance", "total_failed_rules", fmt.Sprintf("%d", s.FailedRules))
				row("compliance", "hosts_critical", fmt.Sprintf("%d", s.HostsCritical))
				row("compliance", "hosts_compliant", fmt.Sprintf("%d", s.HostsCompliant))
				row("compliance", "unscanned", fmt.Sprintf("%d", s.Unscanned))
				for _, r := range s.Worst {
					score := ""
					if r.Score != nil {
						score = fmt.Sprintf("%.1f", *r.Score)
					}
					row("compliance_worst", r.HostName, score, r.Profile)
				}
			}
		case SectionRecentPatchRuns:
			if s := m.RecentPatchRuns; s != nil {
				for _, r := range s.Rows {
					row("recent_patch_run", r.ID, r.Status, r.PatchType, r.HostName)
				}
			}
		case SectionHostStatus:
			if s := m.HostStatus; s != nil {
				for _, r := range s.Rows {
					row("host", r.HostID, r.Status, tsp(r.LastSeen))
				}
			}
		case SectionOpenAlerts:
			if s := m.OpenAlerts; s != nil {
				row("open_alerts", "total", fmt.Sprintf("%d", s.Total))
				row("open_alerts", "critical", fmt.Sprintf("%d", s.Critical))
				row("open_alerts", "error", fmt.Sprintf("%d", s.Error))
				row("open_alerts", "warning", fmt.Sprintf("%d", s.Warning))
				for _, r := range s.Rows {
					row("open_alert", r.ID, r.Severity, r.Title)
				}
			}
		case SectionHostsByUpdates:
			if s := m.HostsByUpdates; s != nil {
				for _, r := range s.Rows {
					row("hosts_by_updates", r.HostName, fmt.Sprintf("%d", r.Updates), fmt.Sprintf("%d", r.SecurityUpdates))
				}
			}
		case SectionTopSecurityPackages:
			if s := m.TopSecurityPackages; s != nil {
				for _, r := range s.Rows {
					row("top_security_package", r.Name, fmt.Sprintf("%d", r.AffectedHosts), strings.Join(r.AvailableVersions, " "))
				}
			}
		case SectionHostOverview:
			if s := m.HostOverview; s != nil {
				for _, r := range s.Rows {
					row("host_overview", r.HostName, fmt.Sprintf("%d", r.Updates), fmt.Sprintf("%d", r.SecurityUpdates), yesno(r.NeedsReboot), tsp(r.BootTime))
				}
			}
		case SectionSecurityUpdatesByHost:
			if s := m.SecurityUpdatesByHost; s != nil {
				for _, h := range s.Hosts {
					for _, r := range h.Rows {
						row("security_update", h.HostName, r.Package, r.Installed, r.Available)
					}
					if h.More > 0 {
						row("security_update_more", h.HostName, fmt.Sprintf("%d", h.More))
					}
				}
			}
		case SectionDisks:
			if s := m.Disks; s != nil {
				for _, r := range s.Rows {
					pct := ""
					if r.Usage != nil {
						pct = fmt.Sprintf("%.1f", r.Usage.UsedPercent)
					}
					row("disk", r.HostName, r.Mount, pct)
				}
			}
		case SectionPatchActivity:
			if s := m.PatchActivity; s != nil {
				row("patch_activity", "completed", fmt.Sprintf("%d", s.Completed))
				row("patch_activity", "failed", fmt.Sprintf("%d", s.Failed))
				for _, r := range s.Rows {
					kind := "run"
					if r.DryRun {
						kind = "dry_run"
					}
					count := ""
					if r.PackageCount != nil {
						count = fmt.Sprintf("%d", *r.PackageCount)
					}
					row("patch_activity", r.ID, r.Status, r.PatchType, r.HostName, kind, count)
				}
			}
		case SectionReboots:
			if s := m.Reboots; s != nil {
				for _, r := range s.Rows {
					row("reboot", r.HostName, r.Status, ts(r.CreatedAt))
				}
			}
		}
	}
	w.Flush()
	return sb.String()
}
