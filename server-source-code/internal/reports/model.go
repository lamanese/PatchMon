package reports

import "time"

// Model is the fully collected report. Renderers read it and nothing else.
// All instants are UTC; renderers convert to Location.
type Model struct {
	ReportName   string
	Language     string
	Location     *time.Location
	TimezoneName string
	GeneratedAt  time.Time
	PeriodDays   int
	PeriodFrom   time.Time
	PeriodTo     time.Time
	Groups       []GroupRef
	FleetWide    bool
	CustomerMode bool
	HostCount    int
	Sections     []string // definition order

	ExecutiveSummary      *ExecutiveSummary
	ComplianceSummary     *ComplianceSummary
	RecentPatchRuns       *PatchRunList
	HostStatus            *HostStatusList
	OpenAlerts            *OpenAlerts
	HostsByUpdates        *HostUpdateList
	TopSecurityPackages   *SecurityPackageList
	HostOverview          *HostOverview
	SecurityUpdatesByHost *SecurityUpdatesByHost
	Disks                 *DiskList
	PatchActivity         *PatchActivity
	Reboots               *RebootList
}

// ExecutiveSummary combines compliance state and patching over the period.
type ExecutiveSummary struct {
	HostCount      int
	ScannedHosts   int
	AverageScore   float64
	HostsCritical  int
	HostsCompliant int
	RunsTotal      int
	RunsCompleted  int
	RunsFailed     int
	RunsRunning    int
}

// ComplianceRow is the latest completed scan of one host and profile.
type ComplianceRow struct {
	HostID      string
	HostName    string
	Profile     string
	Score       *float64
	Passed      int
	Failed      int
	CompletedAt time.Time
}

// ComplianceSummary aggregates ComplianceRows of the scope.
type ComplianceSummary struct {
	PassedRules    int
	FailedRules    int
	HostsCritical  int
	HostsCompliant int
	Unscanned      int
	ScannedHosts   int
	AverageScore   float64
	Worst          []ComplianceRow
}

// PatchRunRow is one patch run in a list.
type PatchRunRow struct {
	ID           string
	HostID       string
	HostName     string
	Status       string
	PatchType    string
	Packages     string // human text for the recent list
	DryRun       bool
	PackageCount *int // nil = not determined
	CreatedAt    time.Time
	StartedAt    *time.Time
	CompletedAt  *time.Time
}

// PatchRunList is the "recent patch runs" section.
type PatchRunList struct {
	Rows []PatchRunRow
}

// HostStatusRow is one host with its effective status.
type HostStatusRow struct {
	HostID   string
	HostName string
	Status   string
	LastSeen *time.Time
}

// HostStatusList is the "hosts_offline" section.
type HostStatusList struct {
	Rows []HostStatusRow
}

// AlertRow is one open alert.
type AlertRow struct {
	ID        string
	Type      string
	Severity  string
	Title     string
	HostID    string
	HostName  string
	CreatedAt time.Time
}

// OpenAlerts is the "open_alerts" section.
type OpenAlerts struct {
	Total    int
	Critical int
	Error    int
	Warning  int
	Rows     []AlertRow
}

// HostUpdateRow is one host in the "hosts by outstanding updates" list.
type HostUpdateRow struct {
	HostID          string
	HostName        string
	Status          string
	Updates         int
	SecurityUpdates int
	LastSeen        *time.Time
}

// HostUpdateList is the "hosts_by_updates" section.
type HostUpdateList struct {
	Rows []HostUpdateRow
}

// SecurityPackageRow is one package aggregated over the scope hosts.
type SecurityPackageRow struct {
	Name              string
	AffectedHosts     int
	AvailableVersions []string
}

// SecurityPackageList is the "top_security_packages" section.
type SecurityPackageList struct {
	Rows []SecurityPackageRow
}

// HostOverviewRow is one host in the customer overview table.
type HostOverviewRow struct {
	HostID          string
	HostName        string
	OS              string
	AgentVersion    string
	Status          string
	Updates         int
	SecurityUpdates int
	NeedsReboot     bool
	BootTime        *time.Time
	Uptime          string
	LastRunStatus   string
	LastRunAt       *time.Time
	LastSeen        *time.Time
	PkgBroken       bool
}

// HostOverview is the "host_overview" section.
type HostOverview struct {
	Rows []HostOverviewRow
}

// SecurityUpdateRow is one open security update of a host.
type SecurityUpdateRow struct {
	Package   string
	Installed string
	Available string
}

// HostSecurityUpdates lists a host's open security updates, capped.
type HostSecurityUpdates struct {
	HostID   string
	HostName string
	Rows     []SecurityUpdateRow
	More     int
}

// SecurityUpdatesByHost is the "security_updates_by_host" section.
type SecurityUpdatesByHost struct {
	Hosts []HostSecurityUpdates
}

// DiskRow is one disk of one host.
type DiskRow struct {
	HostID   string
	HostName string
	Name     string
	Mount    string
	Raw      string
	Usage    *DiskUsage
	Level    string
}

// DiskList is the "disks" section.
type DiskList struct {
	Rows []DiskRow
}

// PatchActivity is the "patch_activity" section over the period.
type PatchActivity struct {
	Rows      []PatchRunRow
	Completed int
	Failed    int
	Other     int
	Truncated bool
}

// RebootRow is one reboot command over the period.
type RebootRow struct {
	HostID      string
	HostName    string
	Status      string // "sent" | "not_delivered" | raw
	Error       string
	CreatedAt   time.Time
	CompletedAt *time.Time
}

// RebootList is the "reboots" section.
type RebootList struct {
	Rows      []RebootRow
	Truncated bool
}
