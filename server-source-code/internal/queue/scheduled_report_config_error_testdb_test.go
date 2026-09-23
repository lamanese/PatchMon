package queue

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/pgtime"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// A report whose definition can never render (unknown language, deleted
// group, empty group) must fail ONCE per slot: record a failed run, move
// next_run_at forward and not be retried by asynq or the hourly fallback.
func TestScheduledReportRunConfigErrorAdvancesScheduleWithoutRetry(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	ctx := context.Background()
	id := uuid.NewString()
	past := time.Now().Add(-time.Hour)
	if _, err := d.Exec(ctx, `INSERT INTO scheduled_reports (id, name, cron_expr, enabled, definition, destination_ids, timezone, next_run_at)
		VALUES ($1, 'broken', '0 8 * * *', true, '{"language":"fr"}'::jsonb, '["dest"]'::jsonb, 'UTC', $2)`, id, pgtime.From(past)); err != nil {
		t.Fatal(err)
	}
	h := NewScheduledReportRunHandler(d, nil, nil, nil, discardTestLogger())
	task := asynq.NewTask(TypeScheduledReportRun, []byte(`{"report_id":"`+id+`"}`))
	if err := h.ProcessTask(ctx, task); err != nil {
		t.Fatalf("config errors must not be retried, got %v", err)
	}
	var status, msg string
	if err := d.RawQueryRow(ctx, `SELECT status, COALESCE(error_message,'') FROM scheduled_report_runs WHERE scheduled_report_id = $1`, id).Scan(&status, &msg); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || !strings.Contains(msg, "unsupported language") {
		t.Fatalf("run row %q %q", status, msg)
	}
	var next time.Time
	if err := d.RawQueryRow(ctx, `SELECT next_run_at FROM scheduled_reports WHERE id = $1`, id).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if !next.After(time.Now()) {
		t.Fatalf("next_run_at must move to the next slot, got %v", next)
	}
}
