package reports

import (
	"bytes"
	"embed"
	"fmt"
	"github.com/PatchMon/PatchMon/server-source-code/internal/branding"
	"html/template"
	"strings"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

// Branding carries the server URL (links, internal reports only) and the URL
// of an UPLOADED logo. Customer reports never emit either. Without an uploaded
// logo the header shows the vendor wordmark; the logos endpoint answers 404
// in that case and mail clients would render a broken image.
type Branding struct {
	ServerURL       string
	LogoURL         string
	LogoData        []byte // uploaded light logo bytes (settings.logo_light_data), may be nil
	LogoContentType string // settings.logo_light_content_type, e.g. "image/png"
}

// Vendor identity shown in every report footer (fork operator).
const (
	BrandName = branding.VendorName
	BrandURL  = branding.VendorURL
)

type htmlData struct {
	M         *Model
	T         Texts
	Links     bool
	BaseURL   string
	LogoURL   string
	BrandName string
	BrandURL  string
	TD        template.CSS // trusted cell style; a plain string would be sanitised
}

const cellStyle template.CSS = "padding:8px 10px;font-size:13px;color:#334155;border-bottom:1px solid #f1f5f9;"

type badgeColor struct {
	FG template.CSS
	BG template.CSS
}

var reportTemplate = template.Must(template.New("report.html").Funcs(template.FuncMap{
	"t": func(tx Texts, key string) string { return tx.S(key) },
	"dict": func(kv ...any) (map[string]any, error) {
		if len(kv)%2 != 0 {
			return nil, fmt.Errorf("dict: odd argument count")
		}
		m := make(map[string]any, len(kv)/2)
		for i := 0; i < len(kv); i += 2 {
			k, ok := kv[i].(string)
			if !ok {
				return nil, fmt.Errorf("dict: key %v is not a string", kv[i])
			}
			m[k] = kv[i+1]
		}
		return m, nil
	},
	"tf": func(tx Texts, key string, args ...any) string { return tx.F(key, args...) },
	"dt": func(tx Texts, loc *time.Location, tm time.Time) string {
		if tm.IsZero() {
			return "-"
		}
		return tx.DateTime(tm, loc)
	},
	"dtp": func(tx Texts, loc *time.Location, tm *time.Time) string {
		if tm == nil || tm.IsZero() {
			return "-"
		}
		return tx.DateTime(*tm, loc)
	},
	"status": func(tx Texts, s string) string { return tx.Status(s) },
	"pct":    func(v float64) string { return fmt.Sprintf("%.1f%%", v) },
	"pctp": func(v *float64) string {
		if v == nil {
			return "-"
		}
		return fmt.Sprintf("%.1f%%", *v)
	},
	"gb":          func(v float64) string { return fmt.Sprintf("%.1f GB", v) },
	"join":        func(s []string) string { return strings.Join(s, ", ") },
	"statusColor": statusColor,
	"sevColor":    severityColor,
	"levelColor":  levelColor,
	"scoreColor": func(v float64) template.CSS {
		return levelColor(scoreLevel(v)).FG
	},
	"zeroGreen": func(n int) template.CSS {
		if n == 0 {
			return "#16a34a"
		}
		return "#dc2626"
	},
	"alt": func(i int) template.CSS {
		if i%2 == 1 {
			return "#f8fafc"
		}
		return "#ffffff"
	},
	"yesno": func(tx Texts, b bool) string {
		if b {
			return tx.S("val.yes")
		}
		return tx.S("val.no")
	},
	"orDash": func(s string) string {
		if strings.TrimSpace(s) == "" {
			return "-"
		}
		return s
	},
	"intp": func(tx Texts, v *int) string {
		if v == nil {
			return tx.S("val.not_determined")
		}
		return fmt.Sprintf("%d", *v)
	},
}).ParseFS(templateFS, "templates/report.html"))

// RenderHTML renders the model as a self-contained HTML mail body.
// html/template escapes every value in its context; the template never uses
// template.HTML for data. Links and the server URL are omitted in customer mode.
func RenderHTML(m *Model, b Branding) (string, error) {
	if m == nil {
		return "", fmt.Errorf("render: nil model")
	}
	data := htmlData{M: m, T: T(m.Language), TD: cellStyle, BrandName: BrandName, BrandURL: BrandURL}
	if !m.CustomerMode {
		data.BaseURL = strings.TrimRight(b.ServerURL, "/")
		data.Links = data.BaseURL != ""
		data.LogoURL = b.LogoURL
	}
	var buf bytes.Buffer
	if err := reportTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render html: %w", err)
	}
	return buf.String(), nil
}

func statusColor(status string) badgeColor {
	switch strings.ToLower(status) {
	case "completed", "active", "compliant", "sent":
		return badgeColor{FG: "#16a34a", BG: "#f0fdf4"}
	case "failed", "critical", "not_delivered":
		return badgeColor{FG: "#dc2626", BG: "#fef2f2"}
	case "running", "warning":
		return badgeColor{FG: "#d97706", BG: "#fffbeb"}
	case "queued", "pending", "pending_validation", "pending_approval", "validated", "approved":
		return badgeColor{FG: "#6366f1", BG: "#eef2ff"}
	case "inactive", "offline", "cancelled":
		return badgeColor{FG: "#94a3b8", BG: "#f1f5f9"}
	}
	return badgeColor{FG: "#64748b", BG: "#f1f5f9"}
}

// levelColor maps a scoreLevel/compliance level ("ok"/"warn"/"critical", or
// disk usage's "warn"/"critical") to a badge colour; anything else is "ok".
func levelColor(level string) badgeColor {
	switch level {
	case "critical":
		return badgeColor{FG: "#dc2626", BG: "#fef2f2"}
	case "warn":
		return badgeColor{FG: "#d97706", BG: "#fffbeb"}
	}
	return badgeColor{FG: "#16a34a", BG: "#f0fdf4"}
}

func severityColor(severity string) badgeColor {
	switch strings.ToLower(severity) {
	case "critical", "error":
		return badgeColor{FG: "#dc2626", BG: "#fef2f2"}
	case "warning":
		return badgeColor{FG: "#d97706", BG: "#fffbeb"}
	case "informational", "info":
		return badgeColor{FG: "#2563eb", BG: "#eff6ff"}
	}
	return badgeColor{FG: "#64748b", BG: "#f1f5f9"}
}
