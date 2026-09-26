package queue

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/pgtime"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// mailRecorder replaces the SMTP sender: it records every (to, message) and
// fails the recipients listed in failFor.
type mailRecorder struct {
	mu      sync.Mutex
	sent    map[string][][]byte
	failFor map[string]error
}

func installMailRecorder(t *testing.T) *mailRecorder {
	t.Helper()
	rec := &mailRecorder{sent: map[string][][]byte{}, failFor: map[string]error{}}
	old, oldState := sendReportEmail, reportRetryState
	sendReportEmail = func(_ context.Context, _ scheduledEmailConfig, to string, msg []byte) error {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		if err, ok := rec.failFor[to]; ok && err != nil {
			return err
		}
		rec.sent[to] = append(rec.sent[to], msg)
		return nil
	}
	reportRetryState = func(context.Context) (int, int) { return 0, 0 }
	t.Cleanup(func() { sendReportEmail, reportRetryState = old, oldState })
	return rec
}

func (r *mailRecorder) count(to string) int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.sent[to]) }

func insertEmailDestination(t *testing.T, d *database.DB, name string) string {
	t.Helper()
	id := uuid.NewString()
	// enc is nil in these tests, so decryptNotifConfig returns the plain JSON.
	if _, err := d.Exec(context.Background(), `INSERT INTO notification_destinations (id, display_name, channel_type, config_encrypted, enabled, created_at, updated_at)
		VALUES ($1, $2, 'email', '{"smtp_host":"mail.example","smtp_port":587,"use_tls":true,"from":"reports@example.com","to":"intern@example.com"}', true, NOW(), NOW())`, id, name); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertReport(t *testing.T, d *database.DB, name string, slot time.Time, destID string, recipients []string) string {
	t.Helper()
	id := uuid.NewString()
	ctx := context.Background()
	var rcpt interface{}
	def := `{"version":2,"sections":["executive_summary","host_overview"]}`
	if recipients != nil {
		rcpt = recipients
		// A customer report needs a host group: put every existing host into one.
		group := uuid.NewString()
		if _, err := d.Exec(ctx, `INSERT INTO host_groups (id, name, created_at, updated_at) VALUES ($1, $1, NOW(), NOW())`, group); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Exec(ctx, `INSERT INTO host_group_memberships (id, host_id, host_group_id, created_at) SELECT gen_random_uuid()::text, id, $1, NOW() FROM hosts`, group); err != nil {
			t.Fatal(err)
		}
		def = `{"version":2,"sections":["executive_summary","host_overview"],"host_group_ids":["` + group + `"]}`
	}
	if _, err := d.Exec(ctx, `INSERT INTO scheduled_reports (id, name, cron_expr, enabled, definition, destination_ids, timezone, next_run_at, fork_email_recipients)
		VALUES ($1, $2, '0 6 * * *', true, $6::jsonb, to_jsonb(ARRAY[$3::text]), 'UTC', $4, $5)`, id, name, destID, pgtime.From(slot), rcpt, def); err != nil {
		t.Fatal(err)
	}
	return id
}

func scheduledTask(reportID string, slot time.Time) *asynq.Task {
	task, err := NewScheduledReportRunTask(ScheduledReportRunPayload{ReportID: reportID, Trigger: ReportTriggerScheduled, SlotAt: &slot})
	if err != nil {
		panic(err)
	}
	return task
}

func archiveRow(t *testing.T, d *database.DB, reportID string) (status string, hasPDF bool, n int) {
	t.Helper()
	if err := d.RawQueryRow(context.Background(), `SELECT COALESCE(MAX(status),''), COALESCE(BOOL_OR(pdf IS NOT NULL), false), COUNT(*) FROM fork_report_archive WHERE scheduled_report_id = $1`, reportID).Scan(&status, &hasPDF, &n); err != nil {
		t.Fatal(err)
	}
	return
}

func TestReportRunClaimsSlotArchivesPDFAndDelivers(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	rec := installMailRecorder(t)
	insertTestHost(t, d, "h1")
	slot := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	dest := insertEmailDestination(t, d, "SMTP")
	rep := insertReport(t, d, "Weekly", slot, dest, []string{"kunde@example.com", "ops@example.com"})
	h := NewScheduledReportRunHandler(d, nil, nil, nil, discardTestLogger())
	if err := h.ProcessTask(context.Background(), scheduledTask(rep, slot)); err != nil {
		t.Fatal(err)
	}
	status, hasPDF, n := archiveRow(t, d, rep)
	if n != 1 || status != "completed" || !hasPDF {
		t.Fatalf("archive n=%d status=%s pdf=%v", n, status, hasPDF)
	}
	if rec.count("kunde@example.com") != 1 || rec.count("ops@example.com") != 1 || rec.count("intern@example.com") != 0 {
		t.Fatalf("customer mode sends one mail per recipient and ignores the destination's to: %v", rec.sent)
	}
	if !bytes.Contains(rec.sent["kunde@example.com"][0], []byte("application/pdf")) {
		t.Fatal("mail must carry the PDF attachment")
	}
	var sent int
	if err := d.RawQueryRow(context.Background(), `SELECT COUNT(*) FROM fork_report_deliveries WHERE status = 'sent' AND attempts = 1 AND sent_at IS NOT NULL`).Scan(&sent); err != nil || sent != 2 {
		t.Fatalf("deliveries sent=%d err=%v", sent, err)
	}
	var next time.Time
	var runStatus string
	if err := d.RawQueryRow(context.Background(), `SELECT s.next_run_at, r.status FROM scheduled_reports s JOIN scheduled_report_runs r ON r.scheduled_report_id = s.id WHERE s.id = $1`, rep).Scan(&next, &runStatus); err != nil {
		t.Fatal(err)
	}
	if !next.After(time.Now()) || runStatus != "completed" {
		t.Fatalf("next=%v run=%s", next, runStatus)
	}
}

func TestReportRunSecondTaskForSameSlotIsDiscarded(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	rec := installMailRecorder(t)
	insertTestHost(t, d, "h1")
	slot := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	rep := insertReport(t, d, "Weekly", slot, insertEmailDestination(t, d, "SMTP"), []string{"kunde@example.com"})
	h := NewScheduledReportRunHandler(d, nil, nil, nil, discardTestLogger())
	for i := 0; i < 2; i++ {
		if err := h.ProcessTask(context.Background(), scheduledTask(rep, slot)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, n := archiveRow(t, d, rep); n != 1 || rec.count("kunde@example.com") != 1 {
		t.Fatalf("archive rows=%d mails=%d", n, rec.count("kunde@example.com"))
	}
}

func TestReportRunManualAndScheduledSameMinuteAreTwoRuns(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	installMailRecorder(t)
	insertTestHost(t, d, "h1")
	slot := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	rep := insertReport(t, d, "Weekly", slot, insertEmailDestination(t, d, "SMTP"), nil)
	h := NewScheduledReportRunHandler(d, nil, nil, nil, discardTestLogger())
	manual, _ := NewScheduledReportRunTask(ScheduledReportRunPayload{ReportID: rep, Trigger: ReportTriggerManual, RunID: uuid.NewString()})
	if err := h.ProcessTask(context.Background(), scheduledTask(rep, slot)); err != nil {
		t.Fatal(err)
	}
	if err := h.ProcessTask(context.Background(), manual); err != nil {
		t.Fatal(err)
	}
	if _, _, n := archiveRow(t, d, rep); n != 2 {
		t.Fatalf("want two archive rows, got %d", n)
	}
}

func TestReportRunRetryResendsOnlyUnsentDeliveries(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	rec := installMailRecorder(t)
	insertTestHost(t, d, "h1")
	slot := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	rep := insertReport(t, d, "Weekly", slot, insertEmailDestination(t, d, "SMTP"), []string{"a@example.com", "b@example.com"})
	rec.failFor["b@example.com"] = errors.New("dial tcp: connection refused")
	reportRetryState = func(context.Context) (int, int) { return 0, 3 }
	h := NewScheduledReportRunHandler(d, nil, nil, nil, discardTestLogger())
	if err := h.ProcessTask(context.Background(), scheduledTask(rep, slot)); err == nil {
		t.Fatal("a retryable failure with retries left must return an error so asynq retries")
	}
	if status, _, _ := archiveRow(t, d, rep); status != "pending" {
		t.Fatalf("archive must stay pending while retries remain, got %s", status)
	}
	// The report is renamed between attempts: the retry must use the snapshot.
	if _, err := d.Exec(context.Background(), `UPDATE scheduled_reports SET name = 'Renamed' WHERE id = $1`, rep); err != nil {
		t.Fatal(err)
	}
	// The destination's sender changes too: the retry must use the snapshotted one.
	if _, err := d.Exec(context.Background(), `UPDATE notification_destinations SET config_encrypted = replace(config_encrypted, 'reports@example.com', 'changed@example.com')`); err != nil {
		t.Fatal(err)
	}
	delete(rec.failFor, "b@example.com")
	reportRetryState = func(context.Context) (int, int) { return 1, 3 }
	if err := h.ProcessTask(context.Background(), scheduledTask(rep, slot)); err != nil {
		t.Fatal(err)
	}
	if rec.count("a@example.com") != 1 || rec.count("b@example.com") != 1 {
		t.Fatalf("a must not be re-sent, b once: %d %d", rec.count("a@example.com"), rec.count("b@example.com"))
	}
	if !bytes.Contains(rec.sent["b@example.com"][0], []byte("Weekly")) || bytes.Contains(rec.sent["b@example.com"][0], []byte("Renamed")) {
		t.Fatal("retry must send the snapshot, not a re-render")
	}
	if !bytes.Contains(rec.sent["b@example.com"][0], []byte("From: <reports@example.com>")) {
		t.Fatal("retry must use the snapshotted sender, not the changed destination config")
	}
	status, _, _ := archiveRow(t, d, rep)
	var attemptsB int32
	if err := d.RawQueryRow(context.Background(), `SELECT attempts FROM fork_report_deliveries WHERE recipient = 'b@example.com'`).Scan(&attemptsB); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || attemptsB != 2 {
		t.Fatalf("status=%s attempts(b)=%d", status, attemptsB)
	}
}

func TestReportRunPartialAfterFinalRetry(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	rec := installMailRecorder(t)
	insertTestHost(t, d, "h1")
	slot := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	rep := insertReport(t, d, "Weekly", slot, insertEmailDestination(t, d, "SMTP"), []string{"a@example.com", "b@example.com"})
	rec.failFor["b@example.com"] = errors.New("dial tcp: connection refused")
	reportRetryState = func(context.Context) (int, int) { return 3, 3 }
	h := NewScheduledReportRunHandler(d, nil, nil, nil, discardTestLogger())
	if err := h.ProcessTask(context.Background(), scheduledTask(rep, slot)); err != nil {
		t.Fatalf("final attempt must finalize, not retry: %v", err)
	}
	status, _, _ := archiveRow(t, d, rep)
	var runStatus, code string
	if err := d.RawQueryRow(context.Background(), `SELECT r.status, d.error_code FROM scheduled_report_runs r, fork_report_deliveries d WHERE r.scheduled_report_id = $1 AND d.recipient = 'b@example.com'`, rep).Scan(&runStatus, &code); err != nil {
		t.Fatal(err)
	}
	if status != "partial" || runStatus != "partial" || code != "smtp_connect" {
		t.Fatalf("status=%s run=%s code=%s", status, runStatus, code)
	}
}

func TestReportRunDeletedDestinationFailsDelivery(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	rec := installMailRecorder(t)
	insertTestHost(t, d, "h1")
	slot := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	dest := insertEmailDestination(t, d, "SMTP")
	rep := insertReport(t, d, "Weekly", slot, dest, []string{"a@example.com"})
	// Snapshot first (fail the send so the archive stays pending), then delete the destination.
	rec.failFor["a@example.com"] = errors.New("dial tcp: connection refused")
	reportRetryState = func(context.Context) (int, int) { return 0, 3 }
	h := NewScheduledReportRunHandler(d, nil, nil, nil, discardTestLogger())
	_ = h.ProcessTask(context.Background(), scheduledTask(rep, slot))
	if _, err := d.Exec(context.Background(), `DELETE FROM notification_destinations WHERE id = $1`, dest); err != nil {
		t.Fatal(err)
	}
	delete(rec.failFor, "a@example.com")
	reportRetryState = func(context.Context) (int, int) { return 3, 3 }
	if err := h.ProcessTask(context.Background(), scheduledTask(rep, slot)); err != nil {
		t.Fatal(err)
	}
	var status, code string
	if err := d.RawQueryRow(context.Background(), `SELECT a.status, d.error_code FROM fork_report_archive a JOIN fork_report_deliveries d ON d.archive_id = a.id WHERE a.scheduled_report_id = $1`, rep).Scan(&status, &code); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || code != "destination_invalid" || rec.count("a@example.com") != 0 {
		t.Fatalf("status=%s code=%s mails=%d", status, code, rec.count("a@example.com"))
	}
}

func TestReportRunLegacyPayloadIsDiscarded(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	installMailRecorder(t)
	insertTestHost(t, d, "h1")
	slot := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	rep := insertReport(t, d, "Weekly", slot, insertEmailDestination(t, d, "SMTP"), nil)
	h := NewScheduledReportRunHandler(d, nil, nil, nil, discardTestLogger())
	if err := h.ProcessTask(context.Background(), asynq.NewTask(TypeScheduledReportRun, []byte(`{"report_id":"`+rep+`"}`))); err != nil {
		t.Fatal(err)
	}
	if _, _, n := archiveRow(t, d, rep); n != 0 {
		t.Fatal("legacy payloads must not run")
	}
}

func TestReportArchiveRetentionAbandonsStalePendingAndKeeps24(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	ctx := context.Background()
	rep := insertReport(t, d, "Weekly", time.Now().Add(time.Hour), insertEmailDestination(t, d, "SMTP"), nil)
	for i := 0; i < 30; i++ {
		if _, err := d.Exec(ctx, `INSERT INTO fork_report_archive (id, scheduled_report_id, run_key, trigger_kind, status, report_name, created_at)
			VALUES ($1, $2, $1, 'manual', 'completed', 'Weekly', NOW() - ($3::int * INTERVAL '1 hour'))`, uuid.NewString(), rep, i+48); err != nil {
			t.Fatal(err)
		}
	}
	stale, fresh := uuid.NewString(), uuid.NewString()
	for _, r := range []struct{ id, age string }{{stale, "25 hours"}, {fresh, "1 hour"}} {
		if _, err := d.Exec(ctx, `INSERT INTO fork_report_archive (id, scheduled_report_id, run_key, trigger_kind, status, report_name, created_at)
			VALUES ($1, $2, $1, 'manual', 'pending', 'Weekly', NOW() - $3::interval)`, r.id, rep, r.age); err != nil {
			t.Fatal(err)
		}
	}
	applyReportRetention(ctx, d, rep, discardTestLogger())
	var n int
	if err := d.RawQueryRow(ctx, `SELECT COUNT(*) FROM fork_report_archive WHERE scheduled_report_id = $1`, rep).Scan(&n); err != nil || n != ReportArchiveKeep {
		t.Fatalf("kept %d rows, want %d (%v)", n, ReportArchiveKeep, err)
	}
	var st, code string
	if err := d.RawQueryRow(ctx, `SELECT status, COALESCE(error_code,'') FROM fork_report_archive WHERE id = $1`, stale).Scan(&st, &code); err != nil || st != "failed" || code != "abandoned" {
		t.Fatalf("stale pending row: %s %s %v", st, code, err)
	}
	var freshStatus string
	if err := d.RawQueryRow(ctx, `SELECT status FROM fork_report_archive WHERE id = $1`, fresh).Scan(&freshStatus); err != nil || freshStatus != "pending" {
		t.Fatalf("fresh pending row must survive: %s %v", freshStatus, err)
	}
}
