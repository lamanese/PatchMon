package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestReportPayload_BootTimeJSON(t *testing.T) {
	boot := time.Date(2026, 9, 18, 6, 30, 0, 0, time.UTC)

	with, err := json.Marshal(ReportPayload{BootTime: &boot})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(with), `"bootTime":"2026-09-18T06:30:00Z"`) {
		t.Fatalf("bootTime missing or not RFC3339 UTC: %s", with)
	}

	without, err := json.Marshal(ReportPayload{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(without), "bootTime") {
		t.Fatalf("bootTime must be omitted when unknown: %s", without)
	}
}
