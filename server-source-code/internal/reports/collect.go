package reports

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/pgtime"
	"github.com/jackc/pgx/v5/pgtype"
)

// CollectInput parametrises Collect.
type CollectInput struct {
	ReportName   string
	Def          Definition
	Scope        Scope
	Now          time.Time
	Location     *time.Location
	TimezoneName string
	// StaleAfter: an active host whose last_update is older than this is shown
	// as inactive (dashboard rule: 2 × update interval). Zero → 2 h.
	StaleAfter time.Duration
}

// Compliance thresholds, identical to store/compliance_dashboard.go.
const (
	complianceCompliantMin = 80.0
	complianceWarningMin   = 60.0
)

// Collect reads every selected section through the ForkReport* queries,
// which are filtered by the scope's host ids before any aggregation. Any
// query error aborts the report: a customer must never receive a report
// with a silently missing section.
func Collect(ctx context.Context, d *database.DB, in CollectInput) (*Model, error) {
	if len(in.Scope.HostIDs) == 0 {
		return nil, ErrNoHosts
	}
	if in.Location == nil {
		in.Location = time.UTC
	}
	if in.TimezoneName == "" {
		in.TimezoneName = in.Location.String()
	}
	if in.StaleAfter <= 0 {
		in.StaleAfter = 2 * time.Hour
	}
	now := in.Now.UTC()
	m := &Model{
		ReportName:   in.ReportName,
		Language:     in.Def.Language,
		Location:     in.Location,
		TimezoneName: in.TimezoneName,
		GeneratedAt:  now,
		PeriodDays:   in.Def.PeriodDays,
		PeriodFrom:   now.Add(-time.Duration(in.Def.PeriodDays) * 24 * time.Hour),
		PeriodTo:     now,
		Groups:       in.Scope.Groups,
		FleetWide:    in.Scope.FleetWide,
		CustomerMode: in.Scope.CustomerMode,
		Sections:     append([]string(nil), in.Def.Sections...),
	}
	q := d.Queries
	ignoreDef := d.IgnoreDefinitionUpdates()
	ids := in.Scope.HostIDs
	top := in.Def.Limits.TopHosts
	if top <= 0 {
		top = DefaultTopHosts
	}

	hosts, err := q.ForkReportHosts(ctx, db.ForkReportHostsParams{HostIds: ids, IgnoreDefinitionUpdates: ignoreDef})
	if err != nil {
		return nil, fmt.Errorf("hosts: %w", err)
	}
	m.HostCount = len(hosts)
	staleBefore := now.Add(-in.StaleAfter)

	c := &collector{ctx: ctx, q: q, in: in, m: m, hosts: hosts, ids: ids, ignoreDef: ignoreDef, top: top, staleBefore: staleBefore}
	for _, sec := range in.Def.Sections {
		var err error
		switch sec {
		case SectionExecutiveSummary:
			err = c.executiveSummary()
		case SectionComplianceSummary:
			err = c.complianceSummary()
		case SectionRecentPatchRuns:
			err = c.recentPatchRuns()
		case SectionHostStatus:
			c.hostStatus()
		case SectionOpenAlerts:
			err = c.openAlerts()
		case SectionHostsByUpdates:
			c.hostsByUpdates()
		case SectionTopSecurityPackages:
			err = c.topSecurityPackages()
		case SectionHostOverview:
			c.hostOverview()
		case SectionSecurityUpdatesByHost:
			err = c.securityUpdatesByHost()
		case SectionDisks:
			c.disks()
		case SectionPatchActivity:
			err = c.patchActivity()
		case SectionReboots:
			err = c.reboots()
		default:
			err = fmt.Errorf("%w: unknown section %q", ErrDefinition, sec)
		}
		if err != nil {
			return nil, fmt.Errorf("section %s: %w", sec, err)
		}
	}
	return m, nil
}

type collector struct {
	ctx         context.Context
	q           *db.Queries
	in          CollectInput
	m           *Model
	hosts       []db.ForkReportHostsRow
	ids         []string
	ignoreDef   bool
	top         int
	staleBefore time.Time

	compliance    []db.ForkReportComplianceLatestRow
	complianceOK  bool
	securityRows  []db.ForkReportSecurityUpdatesRow
	securityOK    bool
	hostNameByID  map[string]string
	hostNameReady bool
}

// --- shared loaders (each query runs at most once per report) ---

func (c *collector) complianceRows() ([]db.ForkReportComplianceLatestRow, error) {
	if c.complianceOK {
		return c.compliance, nil
	}
	rows, err := c.q.ForkReportComplianceLatest(c.ctx, c.ids)
	if err != nil {
		return nil, fmt.Errorf("compliance: %w", err)
	}
	c.compliance, c.complianceOK = rows, true
	return rows, nil
}

func (c *collector) securityUpdateRows() ([]db.ForkReportSecurityUpdatesRow, error) {
	if c.securityOK {
		return c.securityRows, nil
	}
	rows, err := c.q.ForkReportSecurityUpdates(c.ctx, db.ForkReportSecurityUpdatesParams{HostIds: c.ids, IgnoreDefinitionUpdates: c.ignoreDef})
	if err != nil {
		return nil, fmt.Errorf("security updates: %w", err)
	}
	c.securityRows, c.securityOK = rows, true
	return rows, nil
}

func (c *collector) hostName(h db.ForkReportHostsRow) string {
	if h.FriendlyName != "" {
		return h.FriendlyName
	}
	if h.Hostname != nil && *h.Hostname != "" {
		return *h.Hostname
	}
	return h.ApiID
}

func (c *collector) effectiveStatus(h db.ForkReportHostsRow) string {
	if h.Status == "active" && h.LastUpdate.Valid && h.LastUpdate.Time.Before(c.staleBefore) {
		return "inactive"
	}
	return h.Status
}

// complianceKPIs computes the per-host worst score and the thresholds used
// by the dashboard: compliant >= 80, critical < 60.
type complianceKPIs struct {
	scanned, critical, compliant, passed, failed int
	average                                      float64
}

func complianceStats(rows []db.ForkReportComplianceLatestRow) complianceKPIs {
	var k complianceKPIs
	worst := map[string]float64{}
	var sum float64
	var n int
	for _, r := range rows {
		k.passed += int(r.Passed)
		k.failed += int(r.Failed)
		score := 0.0
		if r.Score != nil {
			score = *r.Score
			sum += score
			n++
		}
		if w, ok := worst[r.HostID]; !ok || score < w {
			worst[r.HostID] = score
		}
	}
	k.scanned = len(worst)
	for _, w := range worst {
		switch {
		case w >= complianceCompliantMin:
			k.compliant++
		case w < complianceWarningMin:
			k.critical++
		}
	}
	if n > 0 {
		k.average = sum / float64(n)
	}
	return k
}

// --- sections ---

func (c *collector) executiveSummary() error {
	rows, err := c.complianceRows()
	if err != nil {
		return err
	}
	k := complianceStats(rows)
	stats, err := c.q.ForkReportPatchRunStats(c.ctx, db.ForkReportPatchRunStatsParams{
		HostIds: c.ids, PeriodFrom: pgtime.From(c.m.PeriodFrom), PeriodTo: pgtime.From(c.m.PeriodTo),
	})
	if err != nil {
		return fmt.Errorf("patch run stats: %w", err)
	}
	es := &ExecutiveSummary{HostCount: c.m.HostCount, ScannedHosts: k.scanned, AverageScore: k.average, HostsCritical: k.critical, HostsCompliant: k.compliant}
	for _, s := range stats {
		n := int(s.Cnt)
		es.RunsTotal += n
		switch s.Status {
		case "completed":
			es.RunsCompleted += n
		case "failed":
			es.RunsFailed += n
		case "running":
			es.RunsRunning += n
		}
	}
	c.m.ExecutiveSummary = es
	return nil
}

func (c *collector) complianceSummary() error {
	rows, err := c.complianceRows()
	if err != nil {
		return err
	}
	k := complianceStats(rows)
	cs := &ComplianceSummary{PassedRules: k.passed, FailedRules: k.failed, HostsCritical: k.critical, HostsCompliant: k.compliant,
		ScannedHosts: k.scanned, Unscanned: c.m.HostCount - k.scanned, AverageScore: k.average}
	limit := 5
	for i, r := range rows {
		if i >= limit {
			break
		}
		cs.Worst = append(cs.Worst, ComplianceRow{HostID: r.HostID, HostName: r.FriendlyName, Profile: r.ProfileName, Score: r.Score,
			Passed: int(r.Passed), Failed: int(r.Failed), CompletedAt: tsTime(r.CompletedAt)})
	}
	c.m.ComplianceSummary = cs
	return nil
}

func (c *collector) recentPatchRuns() error {
	rows, err := c.q.ForkReportRecentPatchRuns(c.ctx, db.ForkReportRecentPatchRunsParams{HostIds: c.ids, MaxRows: int32(c.top)})
	if err != nil {
		return fmt.Errorf("recent patch runs: %w", err)
	}
	list := &PatchRunList{}
	for _, r := range rows {
		name := r.FriendlyName
		if name == "" && r.Hostname != nil {
			name = *r.Hostname
		}
		list.Rows = append(list.Rows, PatchRunRow{
			ID: r.ID, HostID: r.HostID, HostName: name, Status: r.Status, PatchType: r.PatchType,
			Packages: packagesText(r.PackageName, r.PackageNames), CreatedAt: tsTime(r.CreatedAt),
			StartedAt: tsPtr(r.StartedAt), CompletedAt: tsPtr(r.CompletedAt),
		})
	}
	c.m.RecentPatchRuns = list
	return nil
}

func (c *collector) hostStatus() {
	list := &HostStatusList{}
	for _, h := range c.hosts {
		list.Rows = append(list.Rows, HostStatusRow{HostID: h.ID, HostName: c.hostName(h), Status: c.effectiveStatus(h), LastSeen: tsPtr(h.LastUpdate)})
	}
	c.m.HostStatus = list
}

func (c *collector) openAlerts() error {
	rows, err := c.q.ForkReportOpenAlerts(c.ctx, db.ForkReportOpenAlertsParams{HostIds: c.ids, IncludeGlobal: !c.m.CustomerMode})
	if err != nil {
		return fmt.Errorf("alerts: %w", err)
	}
	inScope := make(map[string]bool, len(c.ids))
	for _, id := range c.ids {
		inScope[id] = true
	}
	oa := &OpenAlerts{}
	for _, r := range rows {
		hostBound := r.HostID != ""
		if hostBound && (!inScope[r.HostID] || r.HostName == nil) {
			continue // deleted or foreign host: never shown
		}
		if c.m.CustomerMode && (!hostBound || !IsCustomerAlertType(r.Type)) {
			continue
		}
		oa.Total++
		switch strings.ToLower(r.Severity) {
		case "critical":
			oa.Critical++
		case "error":
			oa.Error++
		case "warning":
			oa.Warning++
		}
		if len(oa.Rows) < c.top {
			row := AlertRow{ID: r.ID, Type: r.Type, Severity: r.Severity, Title: r.Title, HostID: r.HostID, CreatedAt: tsTime(r.CreatedAt)}
			if r.HostName != nil {
				row.HostName = *r.HostName
			}
			oa.Rows = append(oa.Rows, row)
		}
	}
	c.m.OpenAlerts = oa
	return nil
}

func (c *collector) hostsByUpdates() {
	rows := make([]HostUpdateRow, 0, len(c.hosts))
	for _, h := range c.hosts {
		rows = append(rows, HostUpdateRow{HostID: h.ID, HostName: c.hostName(h), Status: c.effectiveStatus(h),
			Updates: int(h.UpdatesCount), SecurityUpdates: int(h.SecurityUpdatesCount), LastSeen: tsPtr(h.LastUpdate)})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Updates != rows[j].Updates {
			return rows[i].Updates > rows[j].Updates
		}
		return rows[i].HostName < rows[j].HostName
	})
	if len(rows) > c.top {
		rows = rows[:c.top]
	}
	c.m.HostsByUpdates = &HostUpdateList{Rows: rows}
}

func (c *collector) topSecurityPackages() error {
	rows, err := c.securityUpdateRows()
	if err != nil {
		return err
	}
	type agg struct {
		hosts    map[string]bool
		versions map[string]bool
	}
	byName := map[string]*agg{}
	for _, r := range rows {
		a := byName[r.PackageName]
		if a == nil {
			a = &agg{hosts: map[string]bool{}, versions: map[string]bool{}}
			byName[r.PackageName] = a
		}
		a.hosts[r.HostID] = true
		if r.AvailableVersion != nil && strings.TrimSpace(*r.AvailableVersion) != "" {
			a.versions[strings.TrimSpace(*r.AvailableVersion)] = true
		}
	}
	list := &SecurityPackageList{}
	for name, a := range byName {
		versions := make([]string, 0, len(a.versions))
		for v := range a.versions {
			versions = append(versions, v)
		}
		sort.Strings(versions)
		if len(versions) > 3 {
			versions = versions[:3]
		}
		list.Rows = append(list.Rows, SecurityPackageRow{Name: name, AffectedHosts: len(a.hosts), AvailableVersions: versions})
	}
	sort.Slice(list.Rows, func(i, j int) bool {
		if list.Rows[i].AffectedHosts != list.Rows[j].AffectedHosts {
			return list.Rows[i].AffectedHosts > list.Rows[j].AffectedHosts
		}
		return list.Rows[i].Name < list.Rows[j].Name
	})
	if len(list.Rows) > c.top {
		list.Rows = list.Rows[:c.top]
	}
	c.m.TopSecurityPackages = list
	return nil
}

func (c *collector) hostOverview() {
	ho := &HostOverview{}
	for _, h := range c.hosts {
		row := HostOverviewRow{
			HostID: h.ID, HostName: c.hostName(h), OS: strings.TrimSpace(h.OsType + " " + h.OsVersion),
			Status: c.effectiveStatus(h), Updates: int(h.UpdatesCount), SecurityUpdates: int(h.SecurityUpdatesCount),
			NeedsReboot: h.NeedsReboot != nil && *h.NeedsReboot, BootTime: h.ForkBootTime,
			LastRunStatus: h.LastRunStatus, LastSeen: tsPtr(h.LastUpdate), PkgBroken: h.ForkPkgBroken,
		}
		if h.AgentVersion != nil {
			row.AgentVersion = *h.AgentVersion
		}
		if h.SystemUptime != nil {
			row.Uptime = *h.SystemUptime
		}
		if h.LastRunID != "" {
			at := h.LastRunCompletedAt
			if !at.Valid {
				at = h.LastRunCreatedAt
			}
			row.LastRunAt = tsPtr(at)
		}
		ho.Rows = append(ho.Rows, row)
	}
	c.m.HostOverview = ho
}

func (c *collector) securityUpdatesByHost() error {
	rows, err := c.securityUpdateRows()
	if err != nil {
		return err
	}
	byHost := map[string]*HostSecurityUpdates{}
	order := []string{}
	for _, r := range rows { // query order: friendly_name, host_id, package
		h := byHost[r.HostID]
		if h == nil {
			h = &HostSecurityUpdates{HostID: r.HostID, HostName: r.FriendlyName}
			byHost[r.HostID] = h
			order = append(order, r.HostID)
		}
		if len(h.Rows) >= MaxSecurityUpdatesPerHost {
			h.More++
			continue
		}
		row := SecurityUpdateRow{Package: r.PackageName, Installed: r.CurrentVersion}
		if r.AvailableVersion != nil {
			row.Available = *r.AvailableVersion
		}
		h.Rows = append(h.Rows, row)
	}
	su := &SecurityUpdatesByHost{}
	for _, id := range order {
		su.Hosts = append(su.Hosts, *byHost[id])
	}
	c.m.SecurityUpdatesByHost = su
	return nil
}

// diskDetail mirrors the agent's models.DiskInfo JSON.
type diskDetail struct {
	Name       string `json:"name"`
	Size       string `json:"size"`
	MountPoint string `json:"mountpoint"`
}

func (c *collector) disks() {
	dl := &DiskList{}
	for _, h := range c.hosts {
		if len(h.DiskDetails) == 0 {
			continue
		}
		var disks []diskDetail
		if err := json.Unmarshal(h.DiskDetails, &disks); err != nil {
			continue // unexpected shape: skip the host, never fail the report
		}
		for _, dd := range disks {
			row := DiskRow{HostID: h.ID, HostName: c.hostName(h), Name: dd.Name, Mount: dd.MountPoint, Raw: dd.Size}
			if u, ok := ParseDiskSize(dd.Size); ok {
				u := u
				row.Usage = &u
				row.Level = DiskLevel(u.UsedPercent)
			}
			dl.Rows = append(dl.Rows, row)
		}
	}
	c.m.Disks = dl
}

func (c *collector) patchActivity() error {
	rows, err := c.q.ForkReportPatchActivity(c.ctx, db.ForkReportPatchActivityParams{
		HostIds: c.ids, PeriodFrom: pgtime.From(c.m.PeriodFrom), PeriodTo: pgtime.From(c.m.PeriodTo), MaxRows: int32(MaxActivityRows + 1),
	})
	if err != nil {
		return fmt.Errorf("patch activity: %w", err)
	}
	pa := &PatchActivity{}
	if len(rows) > MaxActivityRows {
		rows = rows[:MaxActivityRows]
		pa.Truncated = true
	}
	for _, r := range rows {
		row := PatchRunRow{ID: r.ID, HostID: r.HostID, HostName: r.FriendlyName, Status: r.Status, PatchType: r.PatchType, DryRun: r.DryRun,
			CreatedAt: tsTime(r.CreatedAt), CompletedAt: tsPtr(r.CompletedAt)}
		if len(r.PackagesAffected) > 0 {
			var names []string
			if err := json.Unmarshal(r.PackagesAffected, &names); err == nil {
				n := len(names)
				row.PackageCount = &n
			}
		}
		if !r.DryRun {
			switch r.Status {
			case "completed":
				pa.Completed++
			case "failed":
				pa.Failed++
			default:
				pa.Other++
			}
		}
		pa.Rows = append(pa.Rows, row)
	}
	c.m.PatchActivity = pa
	return nil
}

func (c *collector) reboots() error {
	rows, err := c.q.ForkReportReboots(c.ctx, db.ForkReportRebootsParams{
		HostIds: c.ids, PeriodFrom: pgtime.From(c.m.PeriodFrom), PeriodTo: pgtime.From(c.m.PeriodTo), MaxRows: int32(MaxActivityRows + 1),
	})
	if err != nil {
		return fmt.Errorf("reboots: %w", err)
	}
	rl := &RebootList{}
	if len(rows) > MaxActivityRows {
		rows = rows[:MaxActivityRows]
		rl.Truncated = true
	}
	for _, r := range rows {
		row := RebootRow{CreatedAt: tsTime(r.CreatedAt), CompletedAt: tsPtr(r.CompletedAt)}
		if r.HostID != nil {
			row.HostID = *r.HostID
		}
		if r.FriendlyName != nil {
			row.HostName = *r.FriendlyName
		}
		if r.ErrorMessage != nil {
			row.Error = *r.ErrorMessage
		}
		switch r.Status {
		case "completed":
			row.Status = "sent"
		case "failed":
			row.Status = "not_delivered"
		default:
			row.Status = r.Status
		}
		rl.Rows = append(rl.Rows, row)
	}
	c.m.Reboots = rl
	return nil
}

// --- helpers ---

func packagesText(single *string, namesJSON []byte) string {
	if single != nil && *single != "" {
		return *single
	}
	var names []string
	if len(namesJSON) > 0 {
		_ = json.Unmarshal(namesJSON, &names)
	}
	switch {
	case len(names) == 0:
		return ""
	case len(names) <= 3:
		return strings.Join(names, ", ")
	default:
		return fmt.Sprintf("%s +%d", strings.Join(names[:3], ", "), len(names)-3)
	}
}

func tsTime(ts pgtype.Timestamp) time.Time {
	if !ts.Valid {
		return time.Time{}
	}
	return ts.Time.UTC()
}

func tsPtr(ts pgtype.Timestamp) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time.UTC()
	return &t
}
