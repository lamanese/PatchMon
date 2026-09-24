package reports

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
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
	PDF          bool // true renders Output.PDF (and LogoSource) under the caller's ctx
}

// Output is what the delivery code sends.
type Output struct {
	Subject    string
	HTML       string
	CSV        string
	Model      *Model
	PDF        []byte
	LogoSource string
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
	out := &Output{Subject: Subject(def.Language, in.ReportName), HTML: html, CSV: RenderCSV(m), Model: m}
	if in.PDF {
		pdf, src, err := RenderPDF(ctx, m, in.Branding)
		if err != nil {
			return nil, err
		}
		out.PDF, out.LogoSource = pdf, src
	}
	return out, nil
}

// BrandingFromSettings maps a settings row to Branding and the stale
// threshold (2 × update_interval minutes, 0 when unset). Shared by the
// scheduled-report worker and the preview API so both stay in sync.
func BrandingFromSettings(s db.Setting) (Branding, time.Duration) {
	b := Branding{ServerURL: strings.TrimRight(s.ServerUrl, "/")}
	if s.LogoLight != nil && *s.LogoLight != "" {
		b.LogoURL = b.ServerURL + *s.LogoLight
	}
	b.LogoData = s.LogoLightData
	if s.LogoLightContentType != nil {
		b.LogoContentType = *s.LogoLightContentType
	}
	var stale time.Duration
	if s.UpdateInterval > 0 {
		stale = 2 * time.Duration(s.UpdateInterval) * time.Minute
	}
	return b, stale
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
