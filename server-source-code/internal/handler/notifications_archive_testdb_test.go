package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/google/uuid"
)

func insertHandlerDestination(t *testing.T, d *database.DB, channel string, enabled bool) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := d.Exec(context.Background(), `INSERT INTO notification_destinations (id, display_name, channel_type, config_encrypted, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, '{"smtp_host":"mail.example","from":"reports@example.com","to":"x@example.com","url":"https://hook.example"}', $4, NOW(), NOW())`, id, channel+"-dest", channel, enabled); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCreateReportRejectsBadRecipients(t *testing.T) {
	d := newHandlerTestDB(t)
	h := handlerWithDB(d)
	email := insertHandlerDestination(t, d, "email", true)
	webhook := insertHandlerDestination(t, d, "webhook", true)
	cases := []struct {
		name, body string
		want       int
	}{
		{"empty list", `{"name":"r","email_recipients":[],"destination_ids":["` + email + `"]}`, 400},
		{"injection", `{"name":"r","email_recipients":["a@example.com\r\nBcc: x@y.example"],"destination_ids":["` + email + `"]}`, 400},
		{"webhook in customer mode", `{"name":"r","email_recipients":["a@example.com"],"destination_ids":["` + webhook + `"]}`, 400},
		{"two destinations in customer mode", `{"name":"r","email_recipients":["a@example.com"],"destination_ids":["` + email + `","` + webhook + `"]}`, 400},
		{"half-hourly customer report", `{"name":"r","cron_expr":"*/30 * * * *","email_recipients":["a@example.com"],"destination_ids":["` + email + `"]}`, 400},
		{"unknown destination", `{"name":"r","destination_ids":["nope"]}`, 400},
		{"valid customer report", `{"name":"r","cron_expr":"0 6 * * 1","email_recipients":["A@Example.com","b@example.com"],"destination_ids":["` + email + `"]}`, 201},
		{"valid internal report", `{"name":"i","destination_ids":["` + webhook + `"]}`, 201},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		h.CreateScheduledReport(w, routedRequest(http.MethodPost, "/api/v1/notifications/scheduled-reports", c.body, nil))
		if w.Code != c.want {
			t.Errorf("%s: want %d got %d %s", c.name, c.want, w.Code, w.Body.String())
		}
		if c.want == 201 && strings.Contains(c.name, "customer") {
			var resp map[string]interface{}
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			if rc, _ := resp["email_recipients"].([]interface{}); len(rc) != 2 || rc[0] != "a@example.com" || resp["customer_mode"] != true {
				t.Errorf("response must echo normalised recipients: %v", resp)
			}
		}
	}
}

func TestRunNowCooldownAnswers429(t *testing.T) {
	d := newHandlerTestDB(t)
	h := handlerWithDB(d) // qc nil: enqueue fails → 500 on the first call, but the limiter must be released then
	email := insertHandlerDestination(t, d, "email", true)
	w := httptest.NewRecorder()
	h.CreateScheduledReport(w, routedRequest(http.MethodPost, "/", `{"name":"r","destination_ids":["`+email+`"]}`, nil))
	var created map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	id, _ := created["id"].(string)
	old := runNowLimit
	runNowLimit = &runNowLimiter{last: map[string]time.Time{}, now: time.Now}
	defer func() { runNowLimit = old }()
	w1 := httptest.NewRecorder()
	h.RunScheduledReportNow(w1, routedRequest(http.MethodPost, "/", "", map[string]string{"id": id}))
	if w1.Code != http.StatusInternalServerError {
		t.Fatalf("without a queue the enqueue fails: %d", w1.Code)
	}
	w2 := httptest.NewRecorder()
	h.RunScheduledReportNow(w2, routedRequest(http.MethodPost, "/", "", map[string]string{"id": id}))
	if w2.Code != http.StatusInternalServerError {
		t.Fatalf("a failed enqueue must not consume the cooldown: %d", w2.Code)
	}
	runNowLimit.last[id] = time.Now()
	w3 := httptest.NewRecorder()
	h.RunScheduledReportNow(w3, routedRequest(http.MethodPost, "/", "", map[string]string{"id": id}))
	if w3.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429 got %d %s", w3.Code, w3.Body.String())
	}
}

func TestArchiveListAndPDFDownload(t *testing.T) {
	d := newHandlerTestDB(t)
	h := handlerWithDB(d)
	ctx := context.Background()
	rep := uuid.NewString()
	if _, err := d.Exec(ctx, `INSERT INTO scheduled_reports (id, name, cron_expr, enabled, definition, destination_ids, timezone) VALUES ($1, 'Weekly', '0 6 * * 1', true, '{}', '[]', 'Europe/Zurich')`, rep); err != nil {
		t.Fatal(err)
	}
	arch := uuid.NewString()
	if _, err := d.Exec(ctx, `INSERT INTO fork_report_archive (id, scheduled_report_id, run_key, trigger_kind, status, report_name, pdf, pdf_size, created_at)
		VALUES ($1, $2, $1, 'manual', 'completed', 'Weekly', '%PDF-1.4 x', 10, '2026-09-28 04:00:00+00')`, arch, rep); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `INSERT INTO fork_report_deliveries (id, archive_id, destination_id, destination_name, channel, recipient, status, attempts, sent_at)
		VALUES ($1, $2, 'dest', 'SMTP', 'email', 'a@example.com', 'sent', 1, NOW())`, uuid.NewString(), arch); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ListReportArchive(w, routedRequest(http.MethodGet, "/", "", map[string]string{"id": rep}))
	if w.Code != 200 {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var rows []map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0]["has_pdf"] != true || rows[0]["status"] != "completed" {
		t.Fatalf("rows %v", rows)
	}
	if dl, _ := rows[0]["deliveries"].([]interface{}); len(dl) != 1 || dl[0].(map[string]interface{})["recipient"] != "a@example.com" {
		t.Fatalf("deliveries %v", rows[0]["deliveries"])
	}
	if strings.Contains(w.Body.String(), "%PDF") {
		t.Fatal("list must not include pdf bytes")
	}
	w = httptest.NewRecorder()
	h.ListReportArchive(w, routedRequest(http.MethodGet, "/", "", map[string]string{"id": "missing"}))
	if w.Code != 404 {
		t.Fatalf("unknown report: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.DownloadReportArchivePDF(w, routedRequest(http.MethodGet, "/", "", map[string]string{"archiveId": arch}))
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/pdf" || w.Header().Get("Cache-Control") != "private, no-store" ||
		w.Header().Get("Content-Disposition") != `attachment; filename="report-weekly-20260928.pdf"` || w.Body.String() != "%PDF-1.4 x" {
		t.Fatalf("download: %d %v %q", w.Code, w.Header(), w.Body.String())
	}
	w = httptest.NewRecorder()
	h.DownloadReportArchivePDF(w, routedRequest(http.MethodGet, "/", "", map[string]string{"archiveId": "missing"}))
	if w.Code != 404 {
		t.Fatalf("unknown archive: %d", w.Code)
	}
}

func TestUpdateReportRecipientsAndStaleSlot(t *testing.T) {
	d := newHandlerTestDB(t)
	h := handlerWithDB(d)
	email := insertHandlerDestination(t, d, "email", true)
	internal := insertHandlerDestination(t, d, "internal", true)
	w := httptest.NewRecorder()
	h.CreateScheduledReport(w, routedRequest(http.MethodPost, "/", `{"name":"r","cron_expr":"0 6 * * 1","email_recipients":["a@example.com"],"destination_ids":["`+email+`"]}`, nil))
	var created map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	id, _ := created["id"].(string)
	if _, err := d.Exec(context.Background(), `UPDATE scheduled_reports SET next_run_at = '2020-01-01 00:00:00' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}

	w = httptest.NewRecorder()
	h.UpdateScheduledReport(w, routedRequest(http.MethodPut, "/", `{"email_recipients":["x@example.com\r\nBcc: y@example.com"],"destination_ids":["`+email+`"]}`, map[string]string{"id": id}))
	if w.Code != 400 || strings.Contains(w.Body.String(), "example.com") {
		t.Fatalf("bad recipient: %d %s (must not echo addresses)", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.UpdateScheduledReport(w, routedRequest(http.MethodPut, "/", `{"destination_ids":["`+internal+`"]}`, map[string]string{"id": id}))
	if w.Code != 400 {
		t.Fatalf("internal destination: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	h.UpdateScheduledReport(w, routedRequest(http.MethodPut, "/", `{"email_recipients":null,"destination_ids":["`+email+`"]}`, map[string]string{"id": id}))
	if w.Code != 200 {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["email_recipients"] != nil || resp["customer_mode"] != false {
		t.Fatalf("null recipients make the report internal: %v", resp)
	}
	next, _ := time.Parse(time.RFC3339, resp["next_run_at"].(string))
	if !next.After(time.Now()) {
		t.Fatalf("a stale next_run_at must move to the next cron slot: %v", resp["next_run_at"])
	}
}
