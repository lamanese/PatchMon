package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/agentregistry"
	"github.com/PatchMon/PatchMon/server-source-code/internal/store"
	"github.com/hibiken/asynq"
)

// TestRunPatchHandler_OfflineRetryDoesNotBumpUpdatedAtWhenStatusUnchanged is
// the fix-round-1 regression test for the reaper review finding: the offline
// branch of ProcessTask used to call UpdateStatus (and therefore stamp
// updated_at = NOW()) on every single pass, even when the status was already
// "queued"/"pending_validation". While a host stays offline, that pass runs
// every 5 minutes forever (asynq.ProcessIn(5*time.Minute), deterministic
// TaskID), so GREATEST(updated_at, scheduled_at) - the reference time
// ForkCancelStaleWaitingPatchRuns uses - was never more than ~5 minutes old,
// and the 24h "host was not reachable" bucket could never fire for exactly
// the run it exists to catch.
//
// Fires the offline path twice (as production does on consecutive 5-minute
// passes) against a run whose updated_at is already 25h stale, asserts
// updated_at did not move, then runs the real cleanupDB and asserts it
// cancels the run with the 24h message.
func TestRunPatchHandler_OfflineRetryDoesNotBumpUpdatedAtWhenStatusUnchanged(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	ctx := context.Background()
	hostID := insertTestHost(t, d, "offline-retry-host")

	runID := insertTestPatchRun(t, d, hostID, patchRunFixture{status: "queued", updatedAt: time.Now().Add(-25 * time.Hour)})

	reg := agentregistry.New() // agent never registered/connected: IsConnected() is always false here
	patchRuns := store.NewPatchRunsStore(d)

	// Unreachable Redis: Enqueue will fail fast, which ProcessTask already
	// tolerates (logs at Debug and returns nil - see the offline branch).
	// The retry task actually landing in a queue is not what this test is
	// about; only the DB side effect (UpdateStatus / updated_at) is.
	queueClient := asynq.NewClient(asynq.RedisClientOpt{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = queueClient.Close() })

	h := NewRunPatchHandler(reg, patchRuns, nil, queueClient, discardTestLogger())

	payload := RunPatchPayload{ApiID: "offline-api-id", HostID: hostID, PatchRunID: runID, PatchType: "patch_all"}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	task := asynq.NewTask(TypeRunPatch, raw)

	for i := 0; i < 2; i++ {
		if err := h.ProcessTask(ctx, task); err != nil {
			t.Fatalf("ProcessTask pass %d: %v", i, err)
		}
	}

	status, _, _, updatedAt := fetchPatchRun(t, d, runID)
	if status != "queued" {
		t.Fatalf("status = %q, want unchanged queued", status)
	}
	if updatedAt == nil {
		t.Fatal("updated_at is nil")
	}
	if time.Since(*updatedAt) < 24*time.Hour {
		t.Fatalf("updated_at = %v (only %v old); a no-op status write must not bump it - the 24h waiting reaper would never see this row as stale", updatedAt, time.Since(*updatedAt))
	}

	// The reaper must now be able to see and cancel it.
	cleanup := NewPatchRunCleanupHandler(d, nil, discardTestLogger())
	if err := cleanup.cleanupDB(ctx, d); err != nil {
		t.Fatalf("cleanupDB: %v", err)
	}

	status, errMsg, completedAt, _ := fetchPatchRun(t, d, runID)
	if status != "cancelled" {
		t.Fatalf("status = %q, want cancelled after cleanupDB", status)
	}
	if errMsg == nil || *errMsg != patchRunWaitingStaleMessage {
		t.Fatalf("error_message = %v, want exactly %q", errMsg, patchRunWaitingStaleMessage)
	}
	if completedAt == nil {
		t.Fatal("completed_at must be set once the reaper cancels the run")
	}
}
