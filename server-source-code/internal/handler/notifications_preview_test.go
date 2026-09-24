package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PatchMon/PatchMon/server-source-code/internal/reports"
	"github.com/go-chi/chi/v5"
)

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
	}
	for _, c := range cases {
		if code, _ := previewErrorStatus(c.err); code != c.code {
			t.Errorf("%v: want %d got %d", c.err, c.code, code)
		}
	}
}
