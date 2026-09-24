package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/reports"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// scriptedDB is a db.DBTX whose QueryRow calls return rows that fail (or
// succeed with zero values) in the scripted order; Exec/Query are unused.
type scriptedDB struct{ scanErrs []error }

type scriptedRow struct{ err error }

func (r scriptedRow) Scan(...any) error { return r.err }

func (s *scriptedDB) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	if len(s.scanErrs) == 0 {
		return scriptedRow{err: errors.New("unexpected query")}
	}
	err := s.scanErrs[0]
	s.scanErrs = s.scanErrs[1:]
	return scriptedRow{err: err}
}

func (s *scriptedDB) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (s *scriptedDB) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected exec")
}

type fakeProvider struct{ d *database.DB }

func (f fakeProvider) DB(context.Context) *database.DB { return f.d }

func previewHandler(scanErrs ...error) *NotificationsHandler {
	return &NotificationsHandler{db: fakeProvider{&database.DB{Queries: db.New(&scriptedDB{scanErrs: scanErrs})}}}
}

// freshGate gives the test its own render gate and restores the globals.
func freshGate(t *testing.T) {
	t.Helper()
	oldGate, oldWait, oldDeadline, oldBuild := previewGate, previewWait, previewDeadline, previewBuild
	previewGate = reports.NewRenderGate()
	t.Cleanup(func() {
		previewGate, previewWait, previewDeadline, previewBuild = oldGate, oldWait, oldDeadline, oldBuild
	})
}

func previewRequest(id string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/scheduled-reports/"+id+"/preview", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", id)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
}

func TestPreviewRejectsMissingID(t *testing.T) {
	h := &NotificationsHandler{}
	w := httptest.NewRecorder()
	h.PreviewScheduledReport(w, previewRequest(""))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestPreviewBusyGateAnswers503(t *testing.T) {
	old := previewGate
	previewGate = reports.NewRenderGate()
	defer func() { previewGate = old }()
	oldWait := previewWait
	previewWait = 20e6 // 20 ms
	defer func() { previewWait = oldWait }()
	release, err := previewGate.Acquire(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	h := &NotificationsHandler{} // db is never touched when the gate is busy
	w := httptest.NewRecorder()
	h.PreviewScheduledReport(w, previewRequest("abc"))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPreviewErrorStatus(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{context.DeadlineExceeded, http.StatusServiceUnavailable},
		{context.Canceled, http.StatusServiceUnavailable},
		{errors.Join(reports.ErrScopeInvalid), http.StatusBadRequest},
		{errors.New("connection refused"), http.StatusInternalServerError},
		{fmt.Errorf("render pdf: 11000000 bytes: %w", reports.ErrPDFTooLarge), http.StatusInternalServerError},
	}
	for _, c := range cases {
		if code, _ := previewErrorStatus(c.err); code != c.code {
			t.Errorf("%v: want %d got %d", c.err, c.code, code)
		}
	}
	if _, msg := previewErrorStatus(fmt.Errorf("x: %w", reports.ErrPDFTooLarge)); msg != "Report exceeds the 10 MB PDF limit" {
		t.Errorf("too large message: %q", msg)
	}
}

func TestPreviewMissingReportIs404OtherErrorsAre500(t *testing.T) {
	freshGate(t)
	w := httptest.NewRecorder()
	previewHandler(pgx.ErrNoRows).PreviewScheduledReport(w, previewRequest("gone"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing row: want 404, got %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	previewHandler(errors.New("connection reset")).PreviewScheduledReport(w, previewRequest("abc"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("db error: want 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPreviewDeadlineCoversGateWait(t *testing.T) {
	freshGate(t)
	previewWait = 10 * time.Second // longer than the deadline below
	previewDeadline = 30 * time.Millisecond
	release, err := previewGate.Acquire(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	start := time.Now()
	w := httptest.NewRecorder()
	previewHandler().PreviewScheduledReport(w, previewRequest("abc"))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", w.Code)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("gate wait ignored the request deadline: %v", el)
	}
}

func TestPreviewRenderFinishingAtDeadlineIsNot200(t *testing.T) {
	freshGate(t)
	previewDeadline = 20 * time.Millisecond
	previewBuild = func(ctx context.Context, _ *database.DB, _ reports.BuildInput) (*reports.Output, error) {
		<-ctx.Done() // "finishes" just as the budget lapses, without an error
		return &reports.Output{Model: &reports.Model{GeneratedAt: time.Now()}, PDF: []byte("%PDF-1.7")}, nil
	}
	w := httptest.NewRecorder()
	// report row found (zero values), settings lookup fails (default branding)
	previewHandler(nil, errors.New("no settings")).PreviewScheduledReport(w, previewRequest("abc"))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", w.Code)
	}
}

func TestPreviewSuccessUsesReportTimezoneForFileName(t *testing.T) {
	freshGate(t)
	zurich, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		t.Skip("tzdata missing:", err)
	}
	previewBuild = func(context.Context, *database.DB, reports.BuildInput) (*reports.Output, error) {
		return &reports.Output{
			Model: &reports.Model{GeneratedAt: time.Date(2026, 9, 24, 22, 30, 0, 0, time.UTC), Location: zurich},
			PDF:   []byte("%PDF-1.7"), LogoSource: reports.LogoSourceDefault,
		}, nil
	}
	w := httptest.NewRecorder()
	previewHandler(nil, errors.New("no settings")).PreviewScheduledReport(w, previewRequest("abc"))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="report-report-20260925.pdf"` {
		t.Fatalf("Content-Disposition: %q", cd)
	}
}
