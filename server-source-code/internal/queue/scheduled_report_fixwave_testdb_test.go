package queue

import (
	"bytes"
	"context"
	"errors"
	"net/textproto"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/reports"
	"github.com/google/uuid"
)

func deliveryState(t *testing.T, d *database.DB, recipient string) (status, code string, attempts int32) {
	t.Helper()
	if err := d.RawQueryRow(context.Background(), `SELECT status, COALESCE(error_code,''), attempts FROM fork_report_deliveries WHERE recipient = $1`, recipient).Scan(&status, &code, &attempts); err != nil {
		t.Fatal(err)
	}
	return
}

// A retry never mails a recipient the operator removed after the snapshot.
func TestReportRetrySkipsRemovedRecipient(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	rec := installMailRecorder(t)
	insertTestHost(t, d, "h1")
	slot := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	rep := insertReport(t, d, "Weekly", slot, insertEmailDestination(t, d, "SMTP"), []string{"a@example.com", "b@example.com"})
	rec.failFor["b@example.com"] = errors.New("dial tcp: connection refused")
	reportRetryState = func(context.Context) (int, int) { return 0, 3 }
	h := NewScheduledReportRunHandler(d, nil, nil, nil, discardTestLogger())
	if err := h.ProcessTask(context.Background(), scheduledTask(rep, slot)); err == nil {
		t.Fatal("first attempt must ask for a retry")
	}
	if _, err := d.Exec(context.Background(), `UPDATE scheduled_reports SET fork_email_recipients = ARRAY['a@example.com'] WHERE id = $1`, rep); err != nil {
		t.Fatal(err)
	}
	delete(rec.failFor, "b@example.com")
	reportRetryState = func(context.Context) (int, int) { return 1, 3 }
	if err := h.ProcessTask(context.Background(), scheduledTask(rep, slot)); err != nil {
		t.Fatal(err)
	}
	if rec.count("b@example.com") != 0 {
		t.Fatal("removed recipient must not be mailed on retry")
	}
	if _, code, _ := deliveryState(t, d, "b@example.com"); code != reports.CodeDestinationInvalid {
		t.Fatalf("code %q", code)
	}
	if status, _, _ := archiveRow(t, d, rep); status != "partial" {
		t.Fatalf("archive %q", status)
	}
}

// Non-retryable failures stay failed: a retry triggered by another delivery
// does not attempt them again.
func TestReportRetrySkipsNonRetryableFailures(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	rec := installMailRecorder(t)
	insertTestHost(t, d, "h1")
	slot := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	rep := insertReport(t, d, "Weekly", slot, insertEmailDestination(t, d, "SMTP"), []string{"a@example.com", "b@example.com", "c@example.com"})
	rec.failFor["a@example.com"] = &textproto.Error{Code: 550, Msg: "mailbox unavailable"}
	rec.failFor["b@example.com"] = errors.New("dial tcp: connection refused")
	reportRetryState = func(context.Context) (int, int) { return 0, 3 }
	h := NewScheduledReportRunHandler(d, nil, nil, nil, discardTestLogger())
	if err := h.ProcessTask(context.Background(), scheduledTask(rep, slot)); err == nil {
		t.Fatal("first attempt must ask for a retry")
	}
	delete(rec.failFor, "a@example.com")
	delete(rec.failFor, "b@example.com")
	reportRetryState = func(context.Context) (int, int) { return 1, 3 }
	if err := h.ProcessTask(context.Background(), scheduledTask(rep, slot)); err != nil {
		t.Fatal(err)
	}
	if st, code, n := deliveryState(t, d, "a@example.com"); st != "failed" || code != reports.CodeSMTPRejected || n != 1 || rec.count("a@example.com") != 0 {
		t.Fatalf("a: %s %s attempts=%d mails=%d", st, code, n, rec.count("a@example.com"))
	}
	if st, _, n := deliveryState(t, d, "b@example.com"); st != "sent" || n != 2 {
		t.Fatalf("b: %s attempts=%d", st, n)
	}
	if rec.count("c@example.com") != 1 {
		t.Fatal("c must be sent exactly once")
	}
	if status, _, _ := archiveRow(t, d, rep); status != "partial" {
		t.Fatalf("archive %q", status)
	}
}

// After a transient render failure the retry re-renders; the archive row
// must describe that render (name), not the one from openArchive.
func TestReportRetryAfterRenderFailureSnapshotsCurrentName(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	rec := installMailRecorder(t)
	insertTestHost(t, d, "h1")
	slot := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	rep := insertReport(t, d, "Weekly", slot, insertEmailDestination(t, d, "SMTP"), []string{"a@example.com"})
	// Waiting on a busy render gate until the task deadline is a transient
	// render failure.
	release, err := reports.DefaultGate.Acquire(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	reportRetryState = func(context.Context) (int, int) { return 0, 3 }
	h := NewScheduledReportRunHandler(d, nil, nil, nil, discardTestLogger())
	short, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	err = h.ProcessTask(short, scheduledTask(rep, slot))
	cancel()
	release()
	if err == nil {
		t.Fatal("transient render failure must ask for a retry")
	}
	if status, hasPDF, _ := archiveRow(t, d, rep); status != "pending" || hasPDF {
		t.Fatalf("archive after render failure: %s pdf=%v", status, hasPDF)
	}
	if _, err := d.Exec(context.Background(), `UPDATE scheduled_reports SET name = 'Renamed' WHERE id = $1`, rep); err != nil {
		t.Fatal(err)
	}
	reportRetryState = func(context.Context) (int, int) { return 1, 3 }
	if err := h.ProcessTask(context.Background(), scheduledTask(rep, slot)); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := d.RawQueryRow(context.Background(), `SELECT report_name FROM fork_report_archive WHERE scheduled_report_id = $1`, rep).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Renamed" || rec.count("a@example.com") != 1 || !bytes.Contains(rec.sent["a@example.com"][0], []byte("Renamed")) {
		t.Fatalf("archive name %q must equal the mailed report's name", name)
	}
}

// Finalizing the same run twice (a retry after a failed status write) records
// exactly one run row.
func TestReportFinalizeIsIdempotent(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	ctx := context.Background()
	rep := insertReport(t, d, "Weekly", time.Now().Add(time.Hour), insertEmailDestination(t, d, "SMTP"), nil)
	arch := uuid.NewString()
	if _, err := d.Exec(ctx, `INSERT INTO fork_report_archive (id, scheduled_report_id, run_key, trigger_kind, status, report_name) VALUES ($1, $2, $1, 'manual', 'pending', 'Weekly')`, arch, rep); err != nil {
		t.Fatal(err)
	}
	h := NewScheduledReportRunHandler(d, nil, nil, nil, discardTestLogger())
	for i := 0; i < 2; i++ {
		if err := h.finalizeRun(ctx, d, rep, arch, "completed", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	var runs int
	if err := d.RawQueryRow(ctx, `SELECT COUNT(*) FROM scheduled_report_runs WHERE scheduled_report_id = $1`, rep).Scan(&runs); err != nil || runs != 1 {
		t.Fatalf("runs=%d err=%v", runs, err)
	}
}

// On the last attempt a cancelled context still ends the run: failed on the
// render path, partial on the delivery path.
func TestReportLastAttemptCancelledContextFinalizes(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	rec := installMailRecorder(t)
	insertTestHost(t, d, "h1")
	reportRetryState = func(context.Context) (int, int) { return 3, 3 }
	h := NewScheduledReportRunHandler(d, nil, nil, nil, discardTestLogger())

	// Render path.
	rep := insertReport(t, d, "Weekly", time.Now().Add(time.Hour), insertEmailDestination(t, d, "SMTP"), nil)
	arch := uuid.NewString()
	if _, err := d.Exec(context.Background(), `INSERT INTO fork_report_archive (id, scheduled_report_id, run_key, trigger_kind, status, report_name) VALUES ($1, $2, $1, 'manual', 'pending', 'Weekly')`, arch, rep); err != nil {
		t.Fatal(err)
	}
	row, err := d.Queries.GetScheduledReportByID(context.Background(), rep)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.handleRenderError(cancelled, d, row, arch, context.Canceled); err != nil {
		t.Fatalf("last attempt must finalize: %v", err)
	}
	var st string
	if err := d.RawQueryRow(context.Background(), `SELECT status FROM fork_report_archive WHERE id = $1`, arch).Scan(&st); err != nil || st != "failed" {
		t.Fatalf("render path archive %q %v", st, err)
	}

	// Delivery path: the context ends right after the first mail went out.
	slot := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	rep2 := insertReport(t, d, "Monthly", slot, insertEmailDestination(t, d, "SMTP2"), []string{"a@example.com", "b@example.com"})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	sendReportEmail = func(_ context.Context, _ scheduledEmailConfig, to string, msg []byte) error {
		rec.sent[to] = append(rec.sent[to], msg)
		stop()
		return nil
	}
	if err := h.ProcessTask(ctx, scheduledTask(rep2, slot)); err != nil {
		t.Fatalf("last attempt must finalize: %v", err)
	}
	if status, _, _ := archiveRow(t, d, rep2); status != "partial" {
		t.Fatalf("delivery path archive %q", status)
	}
	if st, code, _ := deliveryState(t, d, "b@example.com"); st != "failed" || code != reports.CodeAbandoned || rec.count("b@example.com") != 0 {
		t.Fatalf("b: %s %s", st, code)
	}

	// A row that already failed keeps its own code; only pending rows become
	// abandoned. x (pending) is sent and ends the context, y failed earlier
	// with smtp_timeout, z is pending.
	sendReportEmail = func(_ context.Context, _ scheduledEmailConfig, to string, _ []byte) error {
		switch to {
		case "y@example.com":
			return timeoutErr{}
		default:
			return errors.New("dial tcp: connection refused")
		}
	}
	reportRetryState = func(context.Context) (int, int) { return 0, 3 }
	rep3 := insertReport(t, d, "Quarterly", slot, insertEmailDestination(t, d, "SMTP3"), []string{"x@example.com", "y@example.com", "z@example.com"})
	if err := h.ProcessTask(context.Background(), scheduledTask(rep3, slot)); err == nil {
		t.Fatal("first attempt must ask for a retry")
	}
	if _, err := d.Exec(context.Background(), `UPDATE fork_report_deliveries SET status = 'pending', error_code = NULL, error_message = NULL WHERE recipient IN ('x@example.com', 'z@example.com')`); err != nil {
		t.Fatal(err)
	}
	ctx3, stop3 := context.WithCancel(context.Background())
	defer stop3()
	sendReportEmail = func(_ context.Context, _ scheduledEmailConfig, to string, msg []byte) error {
		rec.sent[to] = append(rec.sent[to], msg)
		stop3()
		return nil
	}
	reportRetryState = func(context.Context) (int, int) { return 3, 3 }
	if err := h.ProcessTask(ctx3, scheduledTask(rep3, slot)); err != nil {
		t.Fatalf("last attempt must finalize: %v", err)
	}
	if st, code, n := deliveryState(t, d, "y@example.com"); st != "failed" || code != reports.CodeSMTPTimeout || n != 1 || rec.count("y@example.com") != 0 {
		t.Fatalf("pre-failed y must keep smtp_timeout: %s %s attempts=%d", st, code, n)
	}
	if st, code, _ := deliveryState(t, d, "z@example.com"); st != "failed" || code != reports.CodeAbandoned {
		t.Fatalf("pending z: %s %s", st, code)
	}
	if st, _, _ := deliveryState(t, d, "x@example.com"); st != "sent" {
		t.Fatalf("x: %s", st)
	}
	if status, _, _ := archiveRow(t, d, rep3); status != "partial" {
		t.Fatalf("archive %q", status)
	}
}

// A report disabled between attempts ends its pending run as failed/abandoned.
func TestReportDisabledBetweenAttemptsAbandonsPendingRun(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	rec := installMailRecorder(t)
	insertTestHost(t, d, "h1")
	slot := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	rep := insertReport(t, d, "Weekly", slot, insertEmailDestination(t, d, "SMTP"), []string{"a@example.com"})
	rec.failFor["a@example.com"] = errors.New("dial tcp: connection refused")
	reportRetryState = func(context.Context) (int, int) { return 0, 3 }
	h := NewScheduledReportRunHandler(d, nil, nil, nil, discardTestLogger())
	if err := h.ProcessTask(context.Background(), scheduledTask(rep, slot)); err == nil {
		t.Fatal("first attempt must ask for a retry")
	}
	if _, err := d.Exec(context.Background(), `UPDATE scheduled_reports SET enabled = false WHERE id = $1`, rep); err != nil {
		t.Fatal(err)
	}
	delete(rec.failFor, "a@example.com")
	reportRetryState = func(context.Context) (int, int) { return 1, 3 }
	if err := h.ProcessTask(context.Background(), scheduledTask(rep, slot)); err != nil {
		t.Fatal(err)
	}
	assertArchiveFailed(t, d, rep, reports.CodeAbandoned)
	if rec.count("a@example.com") != 0 {
		t.Fatal("a disabled report sends nothing")
	}
	// The delivery keeps its real failure; only never-attempted rows become abandoned.
	if st, code, n := deliveryState(t, d, "a@example.com"); st != "failed" || code != reports.CodeSMTPConnect || n != 1 {
		t.Fatalf("delivery %s %s attempts=%d", st, code, n)
	}
}

// Customer mail never goes over a plaintext SMTP session.
func TestReportCustomerRunRefusesPlaintextSMTP(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	rec := installMailRecorder(t)
	insertTestHost(t, d, "h1")
	dest := insertEmailDestination(t, d, "SMTP")
	if _, err := d.Exec(context.Background(), `UPDATE notification_destinations SET config_encrypted = replace(config_encrypted, '"use_tls":true', '"use_tls":false') WHERE id = $1`, dest); err != nil {
		t.Fatal(err)
	}
	slot := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	rep := insertReport(t, d, "Weekly", slot, dest, []string{"a@example.com"})
	h := NewScheduledReportRunHandler(d, nil, nil, nil, discardTestLogger())
	if err := h.ProcessTask(context.Background(), scheduledTask(rep, slot)); err != nil {
		t.Fatal(err)
	}
	assertArchiveFailed(t, d, rep, reports.CodeDestinationInvalid)
	if rec.count("a@example.com") != 0 {
		t.Fatal("no mail over plaintext SMTP")
	}
}

// A sent delivery can never be flipped back to failed.
func TestForkMarkReportDeliveryNeverDowngradesSent(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	ctx := context.Background()
	rep := insertReport(t, d, "Weekly", time.Now().Add(time.Hour), insertEmailDestination(t, d, "SMTP"), nil)
	arch, del := uuid.NewString(), uuid.NewString()
	if _, err := d.Exec(ctx, `INSERT INTO fork_report_archive (id, scheduled_report_id, run_key, trigger_kind, status, report_name) VALUES ($1, $2, $1, 'manual', 'pending', 'Weekly')`, arch, rep); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `INSERT INTO fork_report_deliveries (id, archive_id, destination_id, destination_name, channel, recipient, status, attempts, sent_at)
		VALUES ($1, $2, 'dest', 'SMTP', 'email', 'a@example.com', 'sent', 1, NOW())`, del, arch); err != nil {
		t.Fatal(err)
	}
	code, msg := reports.CodeSMTPConnect, "x"
	if err := d.Queries.ForkMarkReportDelivery(ctx, db.ForkMarkReportDeliveryParams{ID: del, Status: "failed", ErrorCode: &code, ErrorMessage: &msg}); err != nil {
		t.Fatal(err)
	}
	if st, _, n := deliveryState(t, d, "a@example.com"); st != "sent" || n != 1 {
		t.Fatalf("sent row changed: %s attempts=%d", st, n)
	}
}
