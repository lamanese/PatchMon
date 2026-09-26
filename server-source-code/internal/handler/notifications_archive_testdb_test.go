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

// insertHandlerDestination stores a destination with a TLS SMTP config
// (enc is nil in these tests, so the config is plain JSON).
func insertHandlerDestination(t *testing.T, d *database.DB, channel string, enabled bool) string {
	t.Helper()
	return insertHandlerDestinationConfig(t, d, channel, enabled, `{"smtp_host":"mail.example","smtp_port":587,"use_tls":true,"from":"reports@example.com","to":"x@example.com","url":"https://hook.example"}`)
}

func insertHandlerDestinationConfig(t *testing.T, d *database.DB, channel string, enabled bool, config string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := d.Exec(context.Background(), `INSERT INTO notification_destinations (id, display_name, channel_type, config_encrypted, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $5, $4, NOW(), NOW())`, id, channel+"-dest", channel, enabled, config); err != nil {
		t.Fatal(err)
	}
	return id
}

// insertHandlerGroup creates an (empty) host group; validation only needs it to exist.
func insertHandlerGroup(t *testing.T, d *database.DB) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := d.Exec(context.Background(), `INSERT INTO host_groups (id, name, created_at, updated_at) VALUES ($1, $1, NOW(), NOW())`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

// groupDef is a report definition scoped to one host group.
func groupDef(group string) string {
	return `"definition":{"version":2,"sections":["executive_summary"],"host_group_ids":["` + group + `"]}`
}

// createReport posts body and returns the created report's id.
func createReport(t *testing.T, h *NotificationsHandler, body string) string {
	t.Helper()
	w := httptest.NewRecorder()
	h.CreateScheduledReport(w, routedRequest(http.MethodPost, "/", body, nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	id, _ := created["id"].(string)
	return id
}

func putReport(h *NotificationsHandler, id, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.UpdateScheduledReport(w, routedRequest(http.MethodPut, "/", body, map[string]string{"id": id}))
	return w
}

func TestCreateReportRejectsBadRecipients(t *testing.T) {
	d := newHandlerTestDB(t)
	h := handlerWithDB(d)
	email := insertHandlerDestination(t, d, "email", true)
	webhook := insertHandlerDestination(t, d, "webhook", true)
	plain := insertHandlerDestinationConfig(t, d, "email", true, `{"smtp_host":"mail.example","smtp_port":587,"use_tls":false,"from":"reports@example.com"}`)
	smtps := insertHandlerDestinationConfig(t, d, "email", true, `{"smtp_host":"mail.example","smtp_port":465,"use_tls":false,"from":"reports@example.com"}`)
	smtpsTLS := insertHandlerDestinationConfig(t, d, "email", true, `{"smtp_host":"mail.example","smtp_port":465,"use_tls":true,"from":"reports@example.com"}`)
	noFrom := insertHandlerDestinationConfig(t, d, "email", true, `{"smtp_host":"mail.example","smtp_port":587,"use_tls":true,"from":"not an address"}`)
	g := groupDef(insertHandlerGroup(t, d))
	cases := []struct {
		name, body string
		want       int
	}{
		{"empty list", `{"name":"r",` + g + `,"email_recipients":[],"destination_ids":["` + email + `"]}`, 400},
		{"injection", `{"name":"r",` + g + `,"email_recipients":["a@example.com\r\nBcc: x@y.example"],"destination_ids":["` + email + `"]}`, 400},
		{"webhook in customer mode", `{"name":"r",` + g + `,"email_recipients":["a@example.com"],"destination_ids":["` + webhook + `"]}`, 400},
		{"two destinations in customer mode", `{"name":"r",` + g + `,"email_recipients":["a@example.com"],"destination_ids":["` + email + `","` + webhook + `"]}`, 400},
		{"half-hourly customer report", `{"name":"r",` + g + `,"cron_expr":"*/30 * * * *","email_recipients":["a@example.com"],"destination_ids":["` + email + `"]}`, 400},
		{"irregular sub-hourly customer report", `{"name":"r",` + g + `,"cron_expr":"0,30 9 * * *","email_recipients":["a@example.com"],"destination_ids":["` + email + `"]}`, 400},
		{"customer report without host groups", `{"name":"r","cron_expr":"0 6 * * 1","email_recipients":["a@example.com"],"destination_ids":["` + email + `"]}`, 400},
		{"customer report over plaintext SMTP", `{"name":"r",` + g + `,"cron_expr":"0 6 * * 1","email_recipients":["a@example.com"],"destination_ids":["` + plain + `"]}`, 400},
		{"customer report with invalid sender", `{"name":"r",` + g + `,"cron_expr":"0 6 * * 1","email_recipients":["a@example.com"],"destination_ids":["` + noFrom + `"]}`, 400},
		{"name too long", `{"name":"` + strings.Repeat("x", 201) + `","destination_ids":["` + webhook + `"]}`, 400},
		{"unknown destination in internal mode is dropped", `{"name":"r","destination_ids":["nope"]}`, 201},
		{"valid customer report", `{"name":"r",` + g + `,"cron_expr":"0 6 * * 1","email_recipients":["A@Example.com","b@example.com"],"destination_ids":["` + email + `"]}`, 201},
		{"customer report over port 465 without Use TLS (plaintext rule)", `{"name":"r",` + g + `,"cron_expr":"0 6 * * 1","email_recipients":["a@example.com"],"destination_ids":["` + smtps + `"]}`, 400},
		{"valid customer report over port 465", `{"name":"r",` + g + `,"cron_expr":"0 6 * * 1","email_recipients":["A@Example.com","b@example.com"],"destination_ids":["` + smtpsTLS + `"]}`, 201},
		{"valid internal report", `{"name":"i","destination_ids":["` + webhook + `"]}`, 201},
		{"name of 200 characters", `{"name":"` + strings.Repeat("x", 200) + `","destination_ids":["` + webhook + `"]}`, 201},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		h.CreateScheduledReport(w, routedRequest(http.MethodPost, "/api/v1/notifications/scheduled-reports", c.body, nil))
		if w.Code != c.want {
			t.Errorf("%s: want %d got %d %s", c.name, c.want, w.Code, w.Body.String())
		}
		if c.want == 400 {
			var resp map[string]interface{}
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			msg, _ := resp["error"].(string)
			for sub, want := range map[string]string{
				"plaintext":           "customer reports require an SMTP destination with 'Use TLS' enabled",
				"without host groups": "customer reports need at least one host group",
				"invalid sender":      "the SMTP destination has no valid sender address",
				"too long":            "name too long (max 200)",
			} {
				if strings.Contains(c.name, sub) && msg != want {
					t.Errorf("%s: error text %q, want %q", c.name, msg, want)
				}
			}
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
	id := createReport(t, h, `{"name":"r",`+groupDef(insertHandlerGroup(t, d))+`,"cron_expr":"0 6 * * 1","email_recipients":["a@example.com"],"destination_ids":["`+email+`"]}`)
	if _, err := d.Exec(context.Background(), `UPDATE scheduled_reports SET next_run_at = '2020-01-01 00:00:00' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	h.UpdateScheduledReport(w, routedRequest(http.MethodPut, "/", `{"email_recipients":["x@example.com\r\nBcc: y@example.com"],"destination_ids":["`+email+`"]}`, map[string]string{"id": id}))
	if w.Code != 400 || strings.Contains(w.Body.String(), "example.com") {
		t.Fatalf("bad recipient: %d %s (must not echo addresses)", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.UpdateScheduledReport(w, routedRequest(http.MethodPut, "/", `{"destination_ids":["`+internal+`"]}`, map[string]string{"id": id}))
	if w.Code != 400 {
		t.Fatalf("internal destination: %d %s", w.Code, w.Body.String())
	}

	storedNext := func() string {
		t.Helper()
		var v string
		if err := d.RawQueryRow(context.Background(), `SELECT next_run_at::text FROM scheduled_reports WHERE id = $1`, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	// Enabled report, past slot, name-only PUT: the slot stays as stored so
	// the worker claims it late, once.
	if w := putReport(h, id, `{"name":"renamed"}`); w.Code != 200 {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	if got := storedNext(); got != "2020-01-01 00:00:00" {
		t.Fatalf("a name-only PUT must keep the past slot, got %s", got)
	}
	// Disabled report with a past slot, enabled via PUT: the slot moves forward.
	if w := putReport(h, id, `{"enabled":false}`); w.Code != 200 {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}
	if got := storedNext(); got != "2020-01-01 00:00:00" {
		t.Fatalf("disabling must keep the slot, got %s", got)
	}

	w = httptest.NewRecorder()
	h.UpdateScheduledReport(w, routedRequest(http.MethodPut, "/", `{"enabled":true,"email_recipients":null,"destination_ids":["`+email+`"]}`, map[string]string{"id": id}))
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
		t.Fatalf("enabling with a stale next_run_at must move it to the next cron slot: %v", resp["next_run_at"])
	}
}

// A customer report must stay disable-able after its SMTP destination is
// gone; enabling it again is strict.
func TestUpdateCustomerReportCanBeDisabledWithoutDestination(t *testing.T) {
	d := newHandlerTestDB(t)
	h := handlerWithDB(d)
	email := insertHandlerDestination(t, d, "email", true)
	id := createReport(t, h, `{"name":"r",`+groupDef(insertHandlerGroup(t, d))+`,"cron_expr":"0 6 * * 1","email_recipients":["a@example.com"],"destination_ids":["`+email+`"]}`)
	if _, err := d.Exec(context.Background(), `DELETE FROM notification_destinations WHERE id = $1`, email); err != nil {
		t.Fatal(err)
	}
	if w := putReport(h, id, `{"enabled":false}`); w.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}
	if w := putReport(h, id, `{"enabled":true}`); w.Code != http.StatusBadRequest {
		t.Fatalf("enable without destination: %d %s", w.Code, w.Body.String())
	}
}

// Absent email_recipients keeps the stored list, null switches to internal,
// [] is rejected.
func TestUpdateReportRecipientsAbsentNullEmpty(t *testing.T) {
	d := newHandlerTestDB(t)
	h := handlerWithDB(d)
	email := insertHandlerDestination(t, d, "email", true)
	id := createReport(t, h, `{"name":"r",`+groupDef(insertHandlerGroup(t, d))+`,"cron_expr":"0 6 * * 1","email_recipients":["a@example.com","b@example.com"],"destination_ids":["`+email+`"]}`)

	w := putReport(h, id, `{"name":"x"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if rc, _ := resp["email_recipients"].([]interface{}); len(rc) != 2 || resp["customer_mode"] != true || resp["name"] != "x" {
		t.Fatalf("absent recipients must be kept: %v", resp)
	}
	if w := putReport(h, id, `{"email_recipients":[]}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty list: %d %s", w.Code, w.Body.String())
	}
	w = putReport(h, id, `{"email_recipients":null}`)
	if w.Code != http.StatusOK {
		t.Fatalf("null: %d %s", w.Code, w.Body.String())
	}
	resp = nil
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["email_recipients"] != nil || resp["customer_mode"] != false {
		t.Fatalf("null makes the report internal: %v", resp)
	}
}

// A PUT that only renames must write next_run_at/last_run_at back exactly as
// the locked row holds them (no stale overwrite of a worker claim).
func TestUpdateReportNameKeepsSchedule(t *testing.T) {
	d := newHandlerTestDB(t)
	h := handlerWithDB(d)
	webhook := insertHandlerDestination(t, d, "webhook", true)
	id := createReport(t, h, `{"name":"r","cron_expr":"0 6 * * 1","destination_ids":["`+webhook+`"]}`)
	ctx := context.Background()
	if _, err := d.Exec(ctx, `UPDATE scheduled_reports SET next_run_at = NOW() + INTERVAL '3 days 1.234 seconds', last_run_at = '2026-09-20 06:00:00.567' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	var next0, last0 string
	if err := d.RawQueryRow(ctx, `SELECT next_run_at::text, last_run_at::text FROM scheduled_reports WHERE id = $1`, id).Scan(&next0, &last0); err != nil {
		t.Fatal(err)
	}
	if w := putReport(h, id, `{"name":"renamed","cron_expr":"0 6 * * 1"}`); w.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	var next1, last1, name string
	if err := d.RawQueryRow(ctx, `SELECT next_run_at::text, last_run_at::text, name FROM scheduled_reports WHERE id = $1`, id).Scan(&next1, &last1, &name); err != nil {
		t.Fatal(err)
	}
	if next1 != next0 || last1 != last0 || name != "renamed" {
		t.Fatalf("schedule changed: next %s -> %s, last %s -> %s, name %s", next0, next1, last0, last1, name)
	}
}

// Stale destination ids (deleted destinations never leave destination_ids)
// must not block saving an internal report; they are dropped.
func TestUpdateInternalReportDropsStaleDestinations(t *testing.T) {
	d := newHandlerTestDB(t)
	h := handlerWithDB(d)
	webhook := insertHandlerDestination(t, d, "webhook", true)
	internal := insertHandlerDestination(t, d, "internal", true)
	id := createReport(t, h, `{"name":"r","destination_ids":["`+webhook+`"]}`)
	ctx := context.Background()
	if _, err := d.Exec(ctx, `UPDATE scheduled_reports SET destination_ids = to_jsonb(ARRAY['deleted-id', $2::text, $3::text]) WHERE id = $1`, id, webhook, internal); err != nil {
		t.Fatal(err)
	}
	if w := putReport(h, id, `{"name":"r2"}`); w.Code != http.StatusOK {
		t.Fatalf("save with stale ids: %d %s", w.Code, w.Body.String())
	}
	var stored []string
	var raw []byte
	if err := d.RawQueryRow(ctx, `SELECT destination_ids FROM scheduled_reports WHERE id = $1`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &stored)
	if len(stored) != 1 || stored[0] != webhook {
		t.Fatalf("stored destination_ids %v, want only %s", stored, webhook)
	}
}
