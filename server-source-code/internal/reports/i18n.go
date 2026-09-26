package reports

import (
	"fmt"
	"time"
)

// Texts resolves UI strings for one report language.
type Texts struct {
	Lang string
}

// T returns the text table for lang; unknown languages fall back to English.
func T(lang string) Texts {
	if _, ok := texts[lang]; !ok {
		lang = DefaultLanguage
	}
	return Texts{Lang: lang}
}

// S looks up a key. A missing key renders as "[[key]]" so it is visible in
// output; TestTextsHaveIdenticalKeySets keeps both tables complete.
func (t Texts) S(key string) string {
	if v, ok := texts[t.Lang][key]; ok {
		return v
	}
	if v, ok := texts[DefaultLanguage][key]; ok {
		return v
	}
	return "[[" + key + "]]"
}

// F formats a text with fmt verbs.
func (t Texts) F(key string, args ...any) string {
	return fmt.Sprintf(t.S(key), args...)
}

// DateTime formats an instant in the report timezone (nil = UTC).
func (t Texts) DateTime(tm time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	if t.Lang == "de" {
		return tm.In(loc).Format("02.01.2006 15:04")
	}
	return tm.In(loc).Format("2006-01-02 15:04")
}

// Date formats a calendar day in the report timezone (nil = UTC).
func (t Texts) Date(tm time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	if t.Lang == "de" {
		return tm.In(loc).Format("02.01.2006")
	}
	return tm.In(loc).Format("2006-01-02")
}

// PeriodLabel names the activity window.
func (t Texts) PeriodLabel(days int) string {
	return t.F("period_label", days)
}

// Status translates a stored status word; unknown values pass through.
func (t Texts) Status(status string) string {
	if v, ok := texts[t.Lang]["status."+status]; ok {
		return v
	}
	return status
}

var texts = map[string]map[string]string{
	"en": {
		"title":          "Report",
		"subject_prefix": "AutoMan Report",
		"generated_at":   "Generated",
		"period":         "Period",
		"period_label":   "Last %d days",
		"timezone":       "Timezone",
		"groups":         "Host groups",
		"fleet_wide":     "All hosts",
		"hosts":          "Hosts",
		"footer":         "This report was generated automatically.",

		"sec.executive_summary":        "Executive summary",
		"sec.compliance_summary":       "Compliance summary",
		"sec.recent_patch_runs":        "Recent patch runs",
		"sec.hosts_offline":            "Host status",
		"sec.open_alerts":              "Open alerts",
		"sec.hosts_by_updates":         "Hosts by outstanding updates",
		"sec.top_security_packages":    "Top outstanding security packages",
		"sec.host_overview":            "Host overview",
		"sec.security_updates_by_host": "Open security updates per host",
		"sec.disks":                    "Disks",
		"sec.patch_activity":           "Patch activity",
		"sec.reboots":                  "Reboots",
		"sec.patching_overview":        "Patching overview",
		"sec.worst_hosts":              "Lowest scoring hosts",

		"kpi.total_hosts":     "Hosts",
		"kpi.scanned_hosts":   "Scanned hosts",
		"kpi.avg_compliance":  "Avg compliance",
		"kpi.critical_hosts":  "Critical hosts",
		"kpi.compliant_hosts": "Compliant hosts",
		"kpi.runs_total":      "Patch runs",
		"kpi.runs_completed":  "Completed",
		"kpi.runs_failed":     "Failed",
		"kpi.runs_running":    "Running",
		"kpi.passed_rules":    "Passed rules",
		"kpi.failed_rules":    "Failed rules",
		"kpi.unscanned":       "Unscanned",
		"kpi.alerts_total":    "Open alerts",
		"kpi.alerts_critical": "Critical",
		"kpi.alerts_error":    "Error",
		"kpi.alerts_warning":  "Warning",

		"col.host":             "Host",
		"col.status":           "Status",
		"col.type":             "Type",
		"col.packages":         "Packages",
		"col.started":          "Started",
		"col.completed":        "Completed",
		"col.last_seen":        "Last seen",
		"col.score":            "Score",
		"col.profile":          "Profile",
		"col.severity":         "Severity",
		"col.title":            "Title",
		"col.created":          "Created",
		"col.updates":          "Updates",
		"col.security_updates": "Security updates",
		"col.package":          "Package",
		"col.affected_hosts":   "Affected hosts",
		"col.available":        "Available",
		"col.installed":        "Installed",
		"col.os":               "Operating system",
		"col.reboot_pending":   "Reboot pending",
		"col.last_boot":        "Last boot",
		"col.last_patch_run":   "Last patch run",
		"col.agent":            "Agent",
		"col.disk":             "Disk",
		"col.mount":            "Mount point",
		"col.size":             "Size",
		"col.used":             "Used",
		"col.result":           "Result",
		"col.date":             "Date",
		"col.package_count":    "Packages",

		// Short heads for the ten-column host overview table; the full
		// col.* names above truncate to "…" at that table's column widths
		// (os, last_boot and last_seen only truncate in German - "OS" /
		// "Boot" / "Kontakt" fit both languages, so the English value is
		// left equal to the full name).
		"col.short.updates":          "Upd.",
		"col.short.security_updates": "Sec.",
		"col.short.reboot_pending":   "Reboot",
		"col.short.agent":            "Agent",
		"col.short.last_patch_run":   "Last run",
		"col.short.os":               "OS",
		"col.short.last_boot":        "Last boot",
		"col.short.last_seen":        "Last seen",

		"val.yes":                  "Yes",
		"val.no":                   "No",
		"val.never":                "Never",
		"val.none":                 "None",
		"val.dry_run":              "Dry run",
		"val.not_determined":       "not determined",
		"val.uptime_reported":      "Uptime as last reported: %s",
		"val.and_n_more":           "… and %d more",
		"val.reboot_sent":          "Reboot command sent",
		"val.reboot_not_delivered": "Not delivered",
		"val.all_packages":         "all packages",
		"val.no_data":              "No data for this period.",
		"val.more_rows":            "The list was cut at %d rows.",
		"val.pkg_broken":           "Package installation incomplete",

		"note.reboot":       "Reboot entries show whether the server delivered the reboot command to the agent, not whether the host actually restarted.",
		"note.activity":     "Activity is listed as far as it is still stored. Runs of hosts that were removed are no longer available.",
		"note.windows_boot": "On Windows, Fast Startup and hibernation do not count as a reboot; the last boot time may be older than expected. FreeBSD values are estimates.",

		"status.completed":          "Completed",
		"status.failed":             "Failed",
		"status.running":            "Running",
		"status.queued":             "Queued",
		"status.pending":            "Pending",
		"status.cancelled":          "Cancelled",
		"status.active":             "Active",
		"status.inactive":           "Inactive",
		"status.pending_validation": "Pending validation",
		"status.pending_approval":   "Pending approval",
		"status.validated":          "Validated",
		"status.approved":           "Approved",
		"status.compliant":          "Compliant",
		"status.warning":            "Warning",
		"status.critical":           "Critical",
		"status.patch_all":          "All packages",
		"status.patch_package":      "Selected packages",
		"pdf.page_of":               "Page %d of %s",
		"pdf.generated":             "Generated",
	},
	"de": {
		"title":          "Bericht",
		"subject_prefix": "AutoMan-Bericht",
		"generated_at":   "Erstellt",
		"period":         "Zeitraum",
		"period_label":   "Letzte %d Tage",
		"timezone":       "Zeitzone",
		"groups":         "Host-Gruppen",
		"fleet_wide":     "Alle Hosts",
		"hosts":          "Hosts",
		"footer":         "Dieser Bericht wurde automatisch erstellt.",

		"sec.executive_summary":        "Zusammenfassung",
		"sec.compliance_summary":       "Compliance-Übersicht",
		"sec.recent_patch_runs":        "Letzte Patch-Läufe",
		"sec.hosts_offline":            "Host-Status",
		"sec.open_alerts":              "Offene Meldungen",
		"sec.hosts_by_updates":         "Hosts nach offenen Updates",
		"sec.top_security_packages":    "Häufigste offene Sicherheitsupdates",
		"sec.host_overview":            "Host-Übersicht",
		"sec.security_updates_by_host": "Offene Sicherheitsupdates pro Host",
		"sec.disks":                    "Datenträger",
		"sec.patch_activity":           "Patch-Aktivität",
		"sec.reboots":                  "Neustarts",
		"sec.patching_overview":        "Patching-Übersicht",
		"sec.worst_hosts":              "Hosts mit der tiefsten Bewertung",

		"kpi.total_hosts":     "Hosts",
		"kpi.scanned_hosts":   "Gescannte Hosts",
		"kpi.avg_compliance":  "Compliance im Mittel",
		"kpi.critical_hosts":  "Kritische Hosts",
		"kpi.compliant_hosts": "Konforme Hosts",
		"kpi.runs_total":      "Patch-Läufe",
		"kpi.runs_completed":  "Abgeschlossen",
		"kpi.runs_failed":     "Fehlgeschlagen",
		"kpi.runs_running":    "Laufend",
		"kpi.passed_rules":    "Bestandene Regeln",
		"kpi.failed_rules":    "Verletzte Regeln",
		"kpi.unscanned":       "Ohne Scan",
		"kpi.alerts_total":    "Offene Meldungen",
		"kpi.alerts_critical": "Kritisch",
		"kpi.alerts_error":    "Fehler",
		"kpi.alerts_warning":  "Warnung",

		"col.host":             "Host",
		"col.status":           "Status",
		"col.type":             "Typ",
		"col.packages":         "Pakete",
		"col.started":          "Gestartet",
		"col.completed":        "Abgeschlossen",
		"col.last_seen":        "Letzter Kontakt",
		"col.score":            "Bewertung",
		"col.profile":          "Profil",
		"col.severity":         "Schweregrad",
		"col.title":            "Titel",
		"col.created":          "Erstellt",
		"col.updates":          "Updates",
		"col.security_updates": "Sicherheitsupdates",
		"col.package":          "Paket",
		"col.affected_hosts":   "Betroffene Hosts",
		"col.available":        "Verfügbar",
		"col.installed":        "Installiert",
		"col.os":               "Betriebssystem",
		"col.reboot_pending":   "Neustart ausstehend",
		"col.last_boot":        "Letzter Start",
		"col.last_patch_run":   "Letzter Patch-Lauf",
		"col.agent":            "Agent",
		"col.disk":             "Datenträger",
		"col.mount":            "Einhängepunkt",
		"col.size":             "Grösse",
		"col.used":             "Belegt",
		"col.result":           "Ergebnis",
		"col.date":             "Datum",
		"col.package_count":    "Pakete",

		// Short heads for the ten-column host overview table; the full
		// col.* names above truncate to "…" at that table's column widths.
		"col.short.updates":          "Upd.",
		"col.short.security_updates": "Sich.",
		"col.short.reboot_pending":   "Reboot",
		"col.short.agent":            "Agent",
		"col.short.last_patch_run":   "Letzter Lauf",
		"col.short.os":               "OS",
		"col.short.last_boot":        "Boot",
		"col.short.last_seen":        "Kontakt",

		"val.yes":                  "Ja",
		"val.no":                   "Nein",
		"val.never":                "Nie",
		"val.none":                 "Keine",
		"val.dry_run":              "Testlauf",
		"val.not_determined":       "nicht ermittelbar",
		"val.uptime_reported":      "Uptime laut letzter Meldung: %s",
		"val.and_n_more":           "… und %d weitere",
		"val.reboot_sent":          "Neustartbefehl gesendet",
		"val.reboot_not_delivered": "Nicht zugestellt",
		"val.all_packages":         "alle Pakete",
		"val.no_data":              "Keine Daten für diesen Zeitraum.",
		"val.more_rows":            "Die Liste wurde nach %d Zeilen gekürzt.",
		"val.pkg_broken":           "Paketinstallation unvollständig",

		"note.reboot":       "Neustart-Einträge zeigen, ob der Server den Neustartbefehl an den Agenten zugestellt hat, nicht ob der Host tatsächlich neu gestartet ist.",
		"note.activity":     "Die Aktivität wird aufgeführt, soweit sie noch gespeichert ist. Läufe entfernter Hosts sind nicht mehr verfügbar.",
		"note.windows_boot": "Unter Windows zählen Schnellstart und Ruhezustand nicht als Neustart; der letzte Start kann älter sein als erwartet. FreeBSD-Werte sind Schätzungen.",

		"status.completed":          "Abgeschlossen",
		"status.failed":             "Fehlgeschlagen",
		"status.running":            "Laufend",
		"status.queued":             "Eingereiht",
		"status.pending":            "Ausstehend",
		"status.cancelled":          "Abgebrochen",
		"status.active":             "Aktiv",
		"status.inactive":           "Inaktiv",
		"status.pending_validation": "Prüfung ausstehend",
		"status.pending_approval":   "Freigabe ausstehend",
		"status.validated":          "Geprüft",
		"status.approved":           "Freigegeben",
		"status.compliant":          "Konform",
		"status.warning":            "Warnung",
		"status.critical":           "Kritisch",
		"status.patch_all":          "Alle Pakete",
		"status.patch_package":      "Ausgewählte Pakete",
		"pdf.page_of":               "Seite %d von %s",
		"pdf.generated":             "Erstellt",
	},
}
