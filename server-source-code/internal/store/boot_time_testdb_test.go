package store

import (
	"context"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
)

// TestForkUpdateHostBootTime_Hysteresis asserts the write-side jitter guard on
// ForkUpdateHostBootTime: the first write always stores, a small drift well
// under the 120s threshold (as a container's own uptime-derived value jitters
// by on every report) is ignored, and a real change well past it still gets
// through. Reuses the throwaway-database harness from
// definition_updates_testdb_test.go; skips without PM_TEST_DATABASE_URL.
func TestForkUpdateHostBootTime_Hysteresis(t *testing.T) {
	d := newDefinitionUpdatesTestDB(t, false) // flag value is irrelevant here
	ctx := context.Background()
	hostID := insertDefUpdatesTestHost(t, d, "boot-time-host", "ubuntu")

	first := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	if err := d.Queries.ForkUpdateHostBootTime(ctx, db.ForkUpdateHostBootTimeParams{ID: hostID, BootTime: first}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	assertStoredBootTime(t, d, hostID, first)

	small := first.Add(30 * time.Second)
	if err := d.Queries.ForkUpdateHostBootTime(ctx, db.ForkUpdateHostBootTimeParams{ID: hostID, BootTime: small}); err != nil {
		t.Fatalf("small-drift write: %v", err)
	}
	assertStoredBootTime(t, d, hostID, first) // unchanged: 30s is jitter, not a reboot

	real := first.Add(10 * time.Minute)
	if err := d.Queries.ForkUpdateHostBootTime(ctx, db.ForkUpdateHostBootTimeParams{ID: hostID, BootTime: real}); err != nil {
		t.Fatalf("real-reboot write: %v", err)
	}
	assertStoredBootTime(t, d, hostID, real)
}

func assertStoredBootTime(t *testing.T, d *database.DB, hostID string, want time.Time) {
	t.Helper()
	h, err := NewHostsStore(d).GetByID(context.Background(), hostID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if h.BootTime == nil || !h.BootTime.Equal(want) {
		t.Fatalf("stored fork_boot_time = %v, want %v", h.BootTime, want)
	}
}
