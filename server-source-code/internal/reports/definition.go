// Package reports builds host-group-scoped scheduled reports: a typed
// definition, a fail-closed scope, a collector that only reads fork queries
// filtered by host id, and renderers (HTML, CSV) that never touch the database.
package reports

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Section identifiers stored in scheduled_reports.definition.sections.
const (
	SectionExecutiveSummary      = "executive_summary"
	SectionComplianceSummary     = "compliance_summary"
	SectionRecentPatchRuns       = "recent_patch_runs"
	SectionHostStatus            = "hosts_offline"
	SectionOpenAlerts            = "open_alerts"
	SectionHostsByUpdates        = "hosts_by_updates"
	SectionTopSecurityPackages   = "top_security_packages"
	SectionHostOverview          = "host_overview"
	SectionSecurityUpdatesByHost = "security_updates_by_host"
	SectionDisks                 = "disks"
	SectionPatchActivity         = "patch_activity"
	SectionReboots               = "reboots"
)

// KnownSections lists every section id the collector understands, in the
// order the UI offers them.
var KnownSections = []string{
	SectionExecutiveSummary, SectionComplianceSummary, SectionRecentPatchRuns, SectionHostStatus,
	SectionOpenAlerts, SectionHostsByUpdates, SectionTopSecurityPackages, SectionHostOverview,
	SectionSecurityUpdatesByHost, SectionDisks, SectionPatchActivity, SectionReboots,
}

// DefaultSections is used when a definition names no sections.
var DefaultSections = []string{SectionExecutiveSummary, SectionComplianceSummary, SectionRecentPatchRuns}

// Limits shared by collector, renderers and the API validation.
const (
	DefaultTopHosts           = 20
	MaxTopHosts               = 200
	MaxHosts                  = 500
	MaxSecurityUpdatesPerHost = 15
	MaxActivityRows           = 500
	MaxGroupIDs               = 50
	DefaultPeriodDays         = 30
	DefaultLanguage           = "en"
)

// Definition is the JSON stored on scheduled_reports.definition (version 2).
// Version 1 rows (no language / period) parse with the defaults en / 30.
type Definition struct {
	Version      int      `json:"version"`
	Sections     []string `json:"sections"`
	HostGroupIDs []string `json:"host_group_ids"`
	Language     string   `json:"language"`
	PeriodDays   int      `json:"period_days"`
	Limits       struct {
		TopHosts int `json:"top_hosts"`
	} `json:"limits"`
}

// ErrDefinition marks every validation error of ParseDefinition.
var ErrDefinition = errors.New("definition")

// ParseDefinition parses and validates a stored or submitted definition.
// It is strict: invalid JSON, unknown sections, unsupported languages,
// periods or limits are errors, never silent defaults. Missing fields get
// the documented defaults. The returned Version is always 2.
func ParseDefinition(raw []byte) (Definition, error) {
	var def Definition
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		if err := json.Unmarshal(trimmed, &def); err != nil {
			return Definition{}, fmt.Errorf("%w: invalid JSON: %v", ErrDefinition, err)
		}
	}
	switch def.Version {
	case 0, 1, 2:
	default:
		return Definition{}, fmt.Errorf("%w: unsupported version %d", ErrDefinition, def.Version)
	}
	def.Version = 2

	if len(def.Sections) == 0 {
		def.Sections = append([]string(nil), DefaultSections...)
	} else {
		seen := make(map[string]bool, len(def.Sections))
		out := make([]string, 0, len(def.Sections))
		for _, s := range def.Sections {
			s = strings.TrimSpace(s)
			if !isKnownSection(s) {
				return Definition{}, fmt.Errorf("%w: unknown section %q", ErrDefinition, s)
			}
			if seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
		def.Sections = out
	}

	if len(def.HostGroupIDs) > 0 {
		seen := make(map[string]bool, len(def.HostGroupIDs))
		out := make([]string, 0, len(def.HostGroupIDs))
		for _, id := range def.HostGroupIDs {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
		if len(out) > MaxGroupIDs {
			return Definition{}, fmt.Errorf("%w: at most %d host groups", ErrDefinition, MaxGroupIDs)
		}
		def.HostGroupIDs = out
	}

	def.Language = strings.ToLower(strings.TrimSpace(def.Language))
	switch def.Language {
	case "":
		def.Language = DefaultLanguage
	case "de", "en":
	default:
		return Definition{}, fmt.Errorf("%w: unsupported language %q", ErrDefinition, def.Language)
	}

	switch def.PeriodDays {
	case 0:
		def.PeriodDays = DefaultPeriodDays
	case 7, 30, 90:
	default:
		return Definition{}, fmt.Errorf("%w: period_days must be 7, 30 or 90", ErrDefinition)
	}

	// top_hosts is clamped, not rejected: older UIs accepted any number and
	// stored rows must keep rendering.
	switch {
	case def.Limits.TopHosts == 0:
		def.Limits.TopHosts = DefaultTopHosts
	case def.Limits.TopHosts < 1:
		def.Limits.TopHosts = 1
	case def.Limits.TopHosts > MaxTopHosts:
		def.Limits.TopHosts = MaxTopHosts
	}
	return def, nil
}

func isKnownSection(s string) bool {
	for _, k := range KnownSections {
		if k == s {
			return true
		}
	}
	return false
}

// HasSection reports whether the definition selects the given section.
func (d Definition) HasSection(id string) bool {
	for _, s := range d.Sections {
		if s == id {
			return true
		}
	}
	return false
}
