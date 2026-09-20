package queue

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestPatchRunCleanupHandler_cleanupDB is the DB-backed reaper test required
// by the am6 cleanup task: one row per status x fresh/stale (plus NULL
// started_at and future/past scheduled_at variants), asserting exactly which
// rows get cancelled, with which message, and that terminal statuses are
// never touched.
func TestPatchRunCleanupHandler_cleanupDB(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	ctx := context.Background()
	hostID := insertTestHost(t, d, "reaper-host")

	now := time.Now().UTC()

	type tc struct {
		name          string
		fixture       patchRunFixture
		wantCancelled bool
		wantMsg       string
	}

	cases := []tc{
		// running: 6h threshold via COALESCE(started_at, updated_at, created_at)
		{
			name:          "running stale via started_at",
			fixture:       patchRunFixture{status: "running", startedAt: ptr(now.Add(-7 * time.Hour)), updatedAt: now.Add(-7 * time.Hour)},
			wantCancelled: true,
			wantMsg:       "still marked running after more than 6 hours",
		},
		{
			name:    "running fresh via started_at",
			fixture: patchRunFixture{status: "running", startedAt: ptr(now.Add(-1 * time.Hour)), updatedAt: now.Add(-1 * time.Hour)},
		},
		{
			name:          "running stale with NULL started_at falls back to updated_at",
			fixture:       patchRunFixture{status: "running", startedAt: nil, updatedAt: now.Add(-7 * time.Hour)},
			wantCancelled: true,
			wantMsg:       "still marked running after more than 6 hours",
		},
		{
			name:    "running fresh with NULL started_at",
			fixture: patchRunFixture{status: "running", startedAt: nil, updatedAt: now.Add(-1 * time.Hour)},
		},
		{
			name:    "running exactly at the edge (5h59m) stays",
			fixture: patchRunFixture{status: "running", startedAt: ptr(now.Add(-(6*time.Hour - time.Minute))), updatedAt: now.Add(-(6*time.Hour - time.Minute))},
		},

		// queued / pending_validation: 24h threshold, reference is
		// GREATEST(updated_at, COALESCE(scheduled_at, updated_at))
		{
			name:          "queued stale",
			fixture:       patchRunFixture{status: "queued", updatedAt: now.Add(-25 * time.Hour)},
			wantCancelled: true,
			wantMsg:       "host was not reachable within 24 hours",
		},
		{
			name:    "queued fresh",
			fixture: patchRunFixture{status: "queued", updatedAt: now.Add(-1 * time.Hour)},
		},
		{
			name:          "pending_validation stale",
			fixture:       patchRunFixture{status: "pending_validation", updatedAt: now.Add(-25 * time.Hour)},
			wantCancelled: true,
			wantMsg:       "host was not reachable within 24 hours",
		},
		{
			name:    "pending_validation fresh",
			fixture: patchRunFixture{status: "pending_validation", updatedAt: now.Add(-1 * time.Hour)},
		},
		{
			name:    "queued with future scheduled_at is never reaped early",
			fixture: patchRunFixture{status: "queued", updatedAt: now.Add(-25 * time.Hour), scheduledAt: ptr(now.Add(2 * time.Hour))},
		},
		{
			name:          "queued with past scheduled_at older than updated_at uses updated_at",
			fixture:       patchRunFixture{status: "queued", updatedAt: now.Add(-25 * time.Hour), scheduledAt: ptr(now.Add(-30 * time.Hour))},
			wantCancelled: true,
			wantMsg:       "host was not reachable within 24 hours",
		},

		// pending_approval / validated / approved: 7 day threshold, same
		// GREATEST(updated_at, scheduled_at) reference.
		{
			name:          "pending_approval stale",
			fixture:       patchRunFixture{status: "pending_approval", updatedAt: now.Add(-8 * 24 * time.Hour)},
			wantCancelled: true,
			wantMsg:       "not approved or executed within 7 days",
		},
		{
			name:          "validated stale",
			fixture:       patchRunFixture{status: "validated", updatedAt: now.Add(-8 * 24 * time.Hour)},
			wantCancelled: true,
			wantMsg:       "not approved or executed within 7 days",
		},
		{
			name:          "approved stale",
			fixture:       patchRunFixture{status: "approved", updatedAt: now.Add(-8 * 24 * time.Hour)},
			wantCancelled: true,
			wantMsg:       "not approved or executed within 7 days",
		},
		{
			name:    "approved fresh",
			fixture: patchRunFixture{status: "approved", updatedAt: now.Add(-24 * time.Hour)},
		},
		{
			name:    "approved with far-future scheduled_at is never reaped early",
			fixture: patchRunFixture{status: "approved", updatedAt: now.Add(-8 * 24 * time.Hour), scheduledAt: ptr(now.Add(10 * 24 * time.Hour))},
		},

		// terminal statuses: must never be touched, however stale.
		{
			name:    "completed terminal untouched",
			fixture: patchRunFixture{status: "completed", updatedAt: now.Add(-100 * 24 * time.Hour)},
		},
		{
			name:    "failed terminal untouched",
			fixture: patchRunFixture{status: "failed", updatedAt: now.Add(-100 * 24 * time.Hour)},
		},
		{
			name:    "cancelled terminal untouched",
			fixture: patchRunFixture{status: "cancelled", updatedAt: now.Add(-100 * 24 * time.Hour)},
		},
	}

	ids := make(map[string]string, len(cases))
	origStatus := make(map[string]string, len(cases))
	for _, c := range cases {
		ids[c.name] = insertTestPatchRun(t, d, hostID, c.fixture)
		origStatus[c.name] = c.fixture.status
	}

	h := NewPatchRunCleanupHandler(d, nil, discardTestLogger())
	if err := h.cleanupDB(ctx, d); err != nil {
		t.Fatalf("cleanupDB: %v", err)
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			status, errMsg, completedAt := fetchPatchRun(t, d, ids[c.name])
			if c.wantCancelled {
				if status != "cancelled" {
					t.Fatalf("status = %q, want cancelled", status)
				}
				if errMsg == nil || !strings.Contains(*errMsg, c.wantMsg) {
					t.Fatalf("error_message = %v, want containing %q", errMsg, c.wantMsg)
				}
				if completedAt == nil {
					t.Fatal("completed_at must be set when a run is cancelled")
				}
				return
			}
			if status != origStatus[c.name] {
				t.Fatalf("status = %q, want unchanged %q", status, origStatus[c.name])
			}
		})
	}
}
