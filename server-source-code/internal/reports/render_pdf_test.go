package reports

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRenderPDFIsDeterministic(t *testing.T) {
	m := sampleModel("de", false)
	a, srcA, err := RenderPDF(context.Background(), m, Branding{})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := RenderPDF(context.Background(), m, Branding{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two renderings of the same model differ")
	}
	if srcA != LogoSourceDefault || !bytes.HasPrefix(a, []byte("%PDF-")) || len(a) < 10_000 {
		t.Fatalf("src=%s len=%d", srcA, len(a))
	}
}

func TestRenderPDFUsesUploadedLogo(t *testing.T) {
	_, src, err := RenderPDF(context.Background(), sampleModel("en", true), Branding{LogoData: pngBytes(t, 200, 60), LogoContentType: "image/png"})
	if err != nil || src != LogoSourceUploaded {
		t.Fatalf("src=%s err=%v", src, err)
	}
}

func TestRenderPDFStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := RenderPDF(ctx, sampleModel("de", false), Branding{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestRenderPDFRejectsNilModelAndOversize(t *testing.T) {
	if _, _, err := RenderPDF(context.Background(), nil, Branding{}); err == nil {
		t.Fatal("nil model must fail")
	}
	old := maxPDFBytes
	maxPDFBytes = 1000
	defer func() { maxPDFBytes = old }()
	if _, _, err := RenderPDF(context.Background(), sampleModel("de", false), Branding{}); !errors.Is(err, ErrPDFTooLarge) {
		t.Fatalf("want ErrPDFTooLarge, got %v", err)
	}
}

func TestPDFFileName(t *testing.T) {
	at := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	cases := map[string]string{
		"Kunde X / Monatsbericht": "report-kunde-x-monatsbericht-20260924.pdf",
		"":                        "report-report-20260924.pdf",
		"<script>":                "report-script-20260924.pdf",
		strings.Repeat("a", 60):   "report-" + strings.Repeat("a", 40) + "-20260924.pdf",
	}
	for in, want := range cases {
		if got := PDFFileName(in, at); got != want {
			t.Errorf("%q: want %q got %q", in, want, got)
		}
	}
}
