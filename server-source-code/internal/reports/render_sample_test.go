package reports

import (
	"context"
	"os"
	"strconv"
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

// TestWriteSamplePDF writes a rendered PDF sample to PM_REPORT_SAMPLE_PDF_OUT
// for a visual check; it is a no-op unless the variable is set.
func TestWriteSamplePDF(t *testing.T) {
	out := os.Getenv("PM_REPORT_SAMPLE_PDF_OUT")
	if out == "" {
		t.Skip("PM_REPORT_SAMPLE_PDF_OUT not set")
	}
	pdf, _, err := RenderPDF(context.Background(), sampleModel("de", false), Branding{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, pdf, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestWriteBigSamplePDF writes a rendered bigModel(n) PDF to
// PM_REPORT_SAMPLE_PDF_OUT for page-count measurement (pdfinfo); it is a
// no-op unless both that variable and PM_REPORT_SAMPLE_HOSTS are set.
func TestWriteBigSamplePDF(t *testing.T) {
	out := os.Getenv("PM_REPORT_SAMPLE_PDF_OUT")
	hostsEnv := os.Getenv("PM_REPORT_SAMPLE_HOSTS")
	if out == "" || hostsEnv == "" {
		t.Skip("PM_REPORT_SAMPLE_PDF_OUT / PM_REPORT_SAMPLE_HOSTS not set")
	}
	n, err := strconv.Atoi(hostsEnv)
	if err != nil {
		t.Fatalf("PM_REPORT_SAMPLE_HOSTS: %v", err)
	}
	pdf, _, err := RenderPDF(context.Background(), bigModel(n), Branding{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, pdf, 0o644); err != nil {
		t.Fatal(err)
	}
}
