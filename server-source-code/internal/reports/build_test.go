package reports

import (
	"context"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
)

func TestBrandingFromSettings(t *testing.T) {
	light := "/api/v1/settings/logos/light"
	ct := "image/png"
	b, stale := BrandingFromSettings(db.Setting{ServerUrl: "https://pm.example.com/", LogoLight: &light, LogoLightData: []byte{1, 2}, LogoLightContentType: &ct, UpdateInterval: 60})
	if b.ServerURL != "https://pm.example.com" || b.LogoURL != "https://pm.example.com/api/v1/settings/logos/light" || string(b.LogoData) != "\x01\x02" || b.LogoContentType != "image/png" || stale != 120*time.Minute {
		t.Fatalf("got %+v stale=%v", b, stale)
	}
	b, stale = BrandingFromSettings(db.Setting{ServerUrl: "https://x"})
	if b.LogoURL != "" || b.LogoData != nil || stale != 0 {
		t.Fatalf("empty settings: %+v stale=%v", b, stale)
	}
}

// TestBuildInvalidDefinitionNeverTouchesDB mirrors the definition_test.go
// pattern: parsing fails before the nil *database.DB is ever used, so PDF:
// true must not change that — the render step is never reached.
func TestBuildInvalidDefinitionNeverTouchesDB(t *testing.T) {
	_, err := Build(context.Background(), nil, BuildInput{
		Definition: []byte(`{"version":3}`),
		PDF:        true,
	})
	if err == nil {
		t.Fatal("expected error for invalid definition")
	}
}
