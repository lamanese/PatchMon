package store

import (
	"context"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
)

// TestForkUpdateHostBootTime_Hysteresis asserts the write-side jitter guard on
// ForkUpdateHostBootTime: the first write always stores, a difference under
// 10s from the stored value (in either direction, as a container's own
// uptime-derived value jitters by about a second on every report) is
// ignored, and a difference at or above 10s always gets through — including
// two real boots close together, which can never be less than 10s apart.
// Reuses the throwaway-database harness from
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

	plus9s := first.Add(9 * time.Second)
	if err := d.Queries.ForkUpdateHostBootTime(ctx, db.ForkUpdateHostBootTimeParams{ID: hostID, BootTime: plus9s}); err != nil {
		t.Fatalf("+9s write: %v", err)
	}
	assertStoredBootTime(t, d, hostID, first) // unchanged: 9s ahead is jitter, below the 10s threshold

	minus9s := first.Add(-9 * time.Second)
	if err := d.Queries.ForkUpdateHostBootTime(ctx, db.ForkUpdateHostBootTimeParams{ID: hostID, BootTime: minus9s}); err != nil {
		t.Fatalf("-9s write: %v", err)
	}
	assertStoredBootTime(t, d, hostID, first) // unchanged: 9s behind is still jitter (ABS applies both ways)

	plus10s := first.Add(10 * time.Second)
	if err := d.Queries.ForkUpdateHostBootTime(ctx, db.ForkUpdateHostBootTimeParams{ID: hostID, BootTime: plus10s}); err != nil {
		t.Fatalf("+10s write: %v", err)
	}
	assertStoredBootTime(t, d, hostID, plus10s) // updated: exactly at the threshold

	newStoredPlus11s := plus10s.Add(11 * time.Second)
	if err := d.Queries.ForkUpdateHostBootTime(ctx, db.ForkUpdateHostBootTimeParams{ID: hostID, BootTime: newStoredPlus11s}); err != nil {
		t.Fatalf("+11s from new stored value write: %v", err)
	}
	assertStoredBootTime(t, d, hostID, newStoredPlus11s) // updated: past the threshold, measured from the new stored value

	// Reviewer's scenario: boot 10:00:00, report, reboot, boot 10:01:30 — two
	// real boots 90s apart must never be swallowed by the guard.
	reviewersScenarioReboot := newStoredPlus11s.Add(90 * time.Second)
	if err := d.Queries.ForkUpdateHostBootTime(ctx, db.ForkUpdateHostBootTimeParams{ID: hostID, BootTime: reviewersScenarioReboot}); err != nil {
		t.Fatalf("real reboot 90s later write: %v", err)
	}
	assertStoredBootTime(t, d, hostID, reviewersScenarioReboot) // updated: a genuine reboot is never suppressed
}

func assertStoredBootTime(t *testing.T, d *database.DB, hostID string, want time.Time) {
	t.Helper()
	h, err := NewHostsStore(d).GetByID(context.Background(), hostID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	want = want.Truncate(time.Microsecond) // Postgres timestamptz precision
	if h.BootTime == nil || !h.BootTime.Truncate(time.Microsecond).Equal(want) {
		t.Fatalf("stored fork_boot_time = %v, want %v", h.BootTime, want)
	}
}
