package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/agentregistry"
	"github.com/PatchMon/PatchMon/server-source-code/internal/store"
	"github.com/gorilla/websocket"
	"github.com/hibiken/asynq"
)

// TestRunPatchHandler_DropsTaskForTerminalStatus proves the safety property
// required by the am6 reaper task: a run_patch task that fires for a patch
// run already in a terminal state (e.g. cancelled by the reaper, or already
// completed/failed) must be dropped before anything is sent to the agent.
//
// Without this guard, the ONLY status check in ProcessTask lives inside the
// "agent not connected" branch. If the agent is connected when a stale task
// finally fires - e.g. the deterministic 5-minute offline-retry task for a
// run the reaper has since cancelled, firing right after the host
// reconnects - the handler falls straight through to building the payload
// and calling registry.SendMessage, regardless of the run's current DB
// status. That would re-execute (or re-arm) a patch run the operator or the
// reaper already cancelled.
//
// The registry here reports the agent as connected via a zero-value
// *websocket.Conn: IsConnected() only checks presence in the connection map,
// but any attempted write (SendMessage) on a zero-value conn panics. That
// makes this test a strong witness - if the terminal-status guard is ever
// removed or bypassed, this test panics instead of just failing quietly.
func TestRunPatchHandler_DropsTaskForTerminalStatus(t *testing.T) {
	d := newPatchRunCleanupTestDB(t)
	ctx := context.Background()
	hostID := insertTestHost(t, d, "guard-host")

	for _, status := range []string{"completed", "failed", "cancelled"} {
		status := status
		t.Run(status, func(t *testing.T) {
			runID := insertTestPatchRun(t, d, hostID, patchRunFixture{status: status, updatedAt: time.Now()})

			const apiID = "guard-api-id"
			reg := agentregistry.New()
			reg.Register(apiID, false)
			reg.SetConnection(apiID, &websocket.Conn{}) // "connected"; any write panics

			patchRuns := store.NewPatchRunsStore(d)
			h := NewRunPatchHandler(reg, patchRuns, nil, nil, discardTestLogger())

			payload := RunPatchPayload{ApiID: apiID, HostID: hostID, PatchRunID: runID, PatchType: "patch_all"}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			task := asynq.NewTask(TypeRunPatch, raw)

			if err := h.ProcessTask(ctx, task); err != nil {
				t.Fatalf("ProcessTask returned error: %v", err)
			}

			gotStatus, _, _, _ := fetchPatchRun(t, d, runID)
			if gotStatus != status {
				t.Fatalf("status changed from %q to %q; a terminal run must never be touched", status, gotStatus)
			}
		})
	}
}
