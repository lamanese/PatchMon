package store

import (
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
)

func TestDbHostToModel_BootTime(t *testing.T) {
	boot := time.Date(2026, 9, 18, 6, 30, 0, 0, time.UTC)

	got := dbHostToModel(db.Host{ID: "h1", ForkBootTime: &boot})
	if got.BootTime == nil || !got.BootTime.Equal(boot) {
		t.Fatalf("BootTime = %v, want %v", got.BootTime, boot)
	}

	if got := dbHostToModel(db.Host{ID: "h2"}); got.BootTime != nil {
		t.Fatalf("BootTime = %v for a host without one, want nil", got.BootTime)
	}
}
