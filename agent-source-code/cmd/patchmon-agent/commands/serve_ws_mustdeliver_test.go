package commands

import (
	"testing"
	"time"
)

// TestDispatchWSMessage_PatchControlIsDeliveredLate is the fork-side companion
// of TestDispatchWSMessage. A dropped run_patch leaves the run "running" on the
// server until the reaper cancels it hours later, and a dropped patch_run_stop
// lets a patch run on after the operator was told it is stopping. Both must
// reach the service loop once it is free again, without ever blocking the
// reader.
func TestDispatchWSMessage_PatchControlIsDeliveredLate(t *testing.T) {
	for _, kind := range []string{"run_patch", "patch_run_stop"} {
		t.Run(kind, func(t *testing.T) {
			restore := wsDispatchWait
			wsDispatchWait = 50 * time.Millisecond
			defer func() { wsDispatchWait = restore }()

			out := make(chan wsMsg, 1)
			out <- wsMsg{kind: "prefill"} // service loop busy, buffer full

			returned := make(chan struct{})
			go func() {
				dispatchWSMessage(out, wsMsg{kind: kind, patchRunID: "run-1"})
				close(returned)
			}()
			select {
			case <-returned:
			case <-time.After(2 * time.Second):
				t.Fatal("dispatch blocked: the read loop is parked and the connection is unwatched")
			}

			// The service loop becomes free again and drains the channel.
			if got := (<-out).kind; got != "prefill" {
				t.Fatalf("first message = %q, want the prefill", got)
			}
			select {
			case m := <-out:
				if m.kind != kind || m.patchRunID != "run-1" {
					t.Fatalf("late message = %+v, want kind %q for run-1", m, kind)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("%s was dropped; it must be delivered once the service loop is free", kind)
			}
		})
	}
}
