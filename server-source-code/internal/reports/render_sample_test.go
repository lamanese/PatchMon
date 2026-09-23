package reports

import (
	"os"
	"testing"
)

// TestWriteSampleHTML writes a rendered sample to PM_REPORT_SAMPLE_OUT for a
// visual check; it is a no-op unless the variable is set.
func TestWriteSampleHTML(t *testing.T) {
	out := os.Getenv("PM_REPORT_SAMPLE_OUT")
	if out == "" {
		t.Skip("PM_REPORT_SAMPLE_OUT not set")
	}
	html, err := RenderHTML(sampleModel("de", false), Branding{ServerURL: "https://pm.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
}
