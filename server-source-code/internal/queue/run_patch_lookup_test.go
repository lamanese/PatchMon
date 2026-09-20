package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/agentregistry"
	"github.com/PatchMon/PatchMon/server-source-code/internal/config"
	"github.com/PatchMon/PatchMon/server-source-code/internal/database"
	"github.com/PatchMon/PatchMon/server-source-code/internal/store"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// TestRunPatchHandler_DropsTaskWhenRunNotFound covers the "not found" half of
// PatchRunsStore.GetByID's two nil-returning shapes: (nil, nil) when the run
// was deleted (or never existed), which ProcessTask must drop quietly - no
// error, no dispatch.
func TestRunPatchHandler_DropsTaskWhenRunNotFound(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	ctx := context.Background()

	patchRuns := store.NewPatchRunsStore(d)
	reg := agentregistry.New()
	h := NewRunPatchHandler(reg, patchRuns, nil, nil, discardTestLogger())

	payload := RunPatchPayload{ApiID: "whoever", HostID: "no-such-host", PatchRunID: uuid.NewString(), PatchType: "patch_all"}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	task := asynq.NewTask(TypeRunPatch, raw)

	if err := h.ProcessTask(ctx, task); err != nil {
		t.Fatalf("ProcessTask returned %v for a not-found run, want nil (dropped)", err)
	}
}

// TestRunPatchHandler_RetriesOnLookupError covers the other half: a real
// GetByID error (DB blip, connection drop - anything other than "no rows")
// must be returned from ProcessTask so asynq retries the task, not silently
// dropped as if the run had been deleted. Simulated with a second connection
// to the same throwaway database, closed before use, so the sqlc query fails
// with a real driver error rather than pgx.ErrNoRows.
func TestRunPatchHandler_RetriesOnLookupError(t *testing.T) {
	dbURL := newPatchRunCleanupTestDBURL(t)
	ctx := context.Background()

	d, err := database.NewFromURL(ctx, dbURL, 0, 0, &config.Config{})
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(d.Close)

	hostID := insertTestHost(t, d, "lookup-error-host")
	runID := insertTestPatchRun(t, d, hostID, patchRunFixture{status: "queued", updatedAt: time.Now()})

	brokenDB, err := database.NewFromURL(ctx, dbURL, 1, 1, &config.Config{})
	if err != nil {
		t.Fatalf("connect second test db handle: %v", err)
	}
	brokenDB.Close() // closed before any query - GetByID must now see a real error, not ErrNoRows

	patchRuns := store.NewPatchRunsStore(brokenDB)
	reg := agentregistry.New()
	h := NewRunPatchHandler(reg, patchRuns, nil, nil, discardTestLogger())

	payload := RunPatchPayload{ApiID: "whoever", HostID: hostID, PatchRunID: runID, PatchType: "patch_all"}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	task := asynq.NewTask(TypeRunPatch, raw)

	if err := h.ProcessTask(ctx, task); err == nil {
		t.Fatal("ProcessTask returned nil for a real lookup error; want the error returned so asynq retries instead of silently dropping the dispatch")
	}

	// The row itself must be untouched - a dropped-vs-retried decision must
	// not also mutate state.
	status, _, _, _ := fetchPatchRun(t, d, runID)
	if status != "queued" {
		t.Fatalf("status = %q, want unchanged queued", status)
	}
}
