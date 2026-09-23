package reports

import (
	"context"
	"fmt"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
)

// BuildInput is everything the worker knows about a scheduled report row.
type BuildInput struct {
	ReportName   string
	Definition   []byte // scheduled_reports.definition
	Timezone     string // scheduled_reports.timezone
	Now          time.Time
	CustomerMode bool
	StaleAfter   time.Duration
	Branding     Branding
}

// Output is what the delivery code sends.
type Output struct {
	Subject string
	HTML    string
	CSV     string
	Model   *Model
}

// Build parses, scopes, collects and renders one report. It fails closed:
// any invalid definition, unknown group, empty scope or query error is an
// error, and nothing is rendered from a partial model.
func Build(ctx context.Context, d *database.DB, in BuildInput) (*Output, error) {
	def, err := ParseDefinition(in.Definition)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, fmt.Errorf("build: no database")
	}
	scope, err := ResolveScope(ctx, d.Queries, def, in.CustomerMode)
	if err != nil {
		return nil, err
	}
	loc, name := ResolveLocation(in.Timezone)
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	m, err := Collect(ctx, d, CollectInput{
		ReportName: in.ReportName, Def: def, Scope: scope, Now: now,
		Location: loc, TimezoneName: name, StaleAfter: in.StaleAfter,
	})
	if err != nil {
		return nil, err
	}
	html, err := RenderHTML(m, in.Branding)
	if err != nil {
		return nil, err
	}
	return &Output{Subject: Subject(def.Language, in.ReportName), HTML: html, CSV: RenderCSV(m), Model: m}, nil
}

// Subject is the mail subject for a report.
func Subject(lang, reportName string) string {
	return T(lang).S("subject_prefix") + ": " + reportName
}

// ResolveLocation loads a timezone name; unknown or empty names mean UTC.
func ResolveLocation(name string) (*time.Location, string) {
	if name == "" {
		return time.UTC, "UTC"
	}
	loc, err := time.LoadLocation(name)
	if err != nil || loc == nil {
		return time.UTC, "UTC"
	}
	return loc, name
}
