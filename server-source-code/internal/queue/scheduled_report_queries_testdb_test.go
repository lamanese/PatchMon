package queue

import (
	"context"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/pgtime"
	"github.com/google/uuid"
)

func TestForkClaimScheduledReportSlotOnlyOnce(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	ctx := context.Background()
	id := uuid.NewString()
	slot := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	if _, err := d.Exec(ctx, `INSERT INTO scheduled_reports (id, name, cron_expr, enabled, definition, destination_ids, timezone, next_run_at)
		VALUES ($1, 'claim', '0 8 * * *', true, '{}'::jsonb, '[]'::jsonb, 'UTC', $2)`, id, pgtime.From(slot)); err != nil {
		t.Fatal(err)
	}
	next := slot.Add(24 * time.Hour)
	p := db.ForkClaimScheduledReportSlotParams{ID: id, Slot: pgtime.From(slot), Now: pgtime.Now(), Next: pgtime.From(next)}
	n, err := d.Queries.ForkClaimScheduledReportSlot(ctx, p)
	if err != nil || n != 1 {
		t.Fatalf("first claim: rows=%d err=%v", n, err)
	}
	n, err = d.Queries.ForkClaimScheduledReportSlot(ctx, p)
	if err != nil || n != 0 {
		t.Fatalf("second claim must find no row: rows=%d err=%v", n, err)
	}
	var got time.Time
	if err := d.RawQueryRow(ctx, `SELECT next_run_at FROM scheduled_reports WHERE id = $1`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Equal(next) {
		t.Fatalf("next_run_at = %v, want %v", got, next)
	}
}

// TestForkClaimScheduledReportSlotToleratesMillisecondNextRunAt pins the
// date_trunc('second', ...) comparison rule: a legacy row whose next_run_at
// carries sub-second precision must still be claimable with the whole-second
// slot value the worker computed.
func TestForkClaimScheduledReportSlotToleratesMillisecondNextRunAt(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	ctx := context.Background()
	id := uuid.NewString()
	slot := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	slotWithMillis := slot.Add(123 * time.Millisecond)
	if _, err := d.Exec(ctx, `INSERT INTO scheduled_reports (id, name, cron_expr, enabled, definition, destination_ids, timezone, next_run_at)
		VALUES ($1, 'claim-ms', '0 8 * * *', true, '{}'::jsonb, '[]'::jsonb, 'UTC', $2)`, id, pgtime.From(slotWithMillis)); err != nil {
		t.Fatal(err)
	}
	next := slot.Add(24 * time.Hour)
	p := db.ForkClaimScheduledReportSlotParams{ID: id, Slot: pgtime.From(slot), Now: pgtime.Now(), Next: pgtime.From(next)}
	n, err := d.Queries.ForkClaimScheduledReportSlot(ctx, p)
	if err != nil || n != 1 {
		t.Fatalf("claim with second-precision slot against ms-precision next_run_at: rows=%d err=%v", n, err)
	}
}
