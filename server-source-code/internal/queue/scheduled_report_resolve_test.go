package queue

import (
	"context"
	"testing"

	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
)

// Tenant resolution must fail closed: a task that names a tenant host may
// never fall back to the default database (spec §4.2).

func TestScheduledReportRunResolveDBFailsClosed(t *testing.T) {
	def := &database.DB{}
	h := NewScheduledReportRunHandler(def, nil, nil, nil, nil)

	got, err := h.resolveDB(context.Background(), []byte(`{"report_id":"r1"}`))
	if err != nil || got != def {
		t.Fatalf("empty host must resolve to the default DB, got %v %v", got, err)
	}
	got, err = h.resolveDB(context.Background(), []byte(`{"report_id":"r1","host":"tenant.example"}`))
	if err == nil || got != nil {
		t.Fatalf("tenant host without pool cache must be an error, got %v %v", got, err)
	}
	if _, err := h.resolveDB(context.Background(), []byte(`{`)); err == nil {
		t.Fatal("invalid payload must be an error")
	}
}

func TestScheduledReportsDispatchResolveDBFailsClosed(t *testing.T) {
	def := &database.DB{}
	h := NewScheduledReportsDispatchHandler(def, nil, nil, nil)

	got, err := h.resolveDB(context.Background(), []byte(`{}`))
	if err != nil || got != def {
		t.Fatalf("empty host must resolve to the default DB, got %v %v", got, err)
	}
	got, err = h.resolveDB(context.Background(), []byte(`{"host":"tenant.example"}`))
	if err == nil || got != nil {
		t.Fatalf("tenant host without pool cache must be an error, got %v %v", got, err)
	}
}
