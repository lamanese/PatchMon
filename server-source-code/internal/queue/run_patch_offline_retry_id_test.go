package queue

import "testing"

// TestNextOfflineRetryTaskID guards the offline-retry chain. The retry used to
// be re-enqueued under the single fixed ID "patch-run-<id>-retry". While the
// retry task is being processed it still holds that ID, so its own re-enqueue
// failed with asynq.ErrTaskIDConflict (swallowed at Debug level) and the chain
// ended after exactly one retry: a host offline for more than ~5 minutes never
// got its run. The chain now alternates between two IDs, so the ID it enqueues
// is never the one it is currently running under.
func TestNextOfflineRetryTaskID(t *testing.T) {
	ids := OfflineRetryTaskIDs("abc")
	if ids[0] == ids[1] {
		t.Fatalf("retry IDs must differ, got %q twice", ids[0])
	}
	if ids[0] != "patch-run-abc-retry" {
		t.Fatalf("first retry ID must stay %q for tasks enqueued by older servers, got %q", "patch-run-abc-retry", ids[0])
	}

	cases := []struct {
		name    string
		current string
		want    string
	}{
		{"original task", "patch-run-abc", ids[0]},
		{"no task id in context", "", ids[0]},
		{"first retry", ids[0], ids[1]},
		{"second retry", ids[1], ids[0]},
	}
	for _, tc := range cases {
		got := nextOfflineRetryTaskID("abc", tc.current)
		if got != tc.want {
			t.Errorf("%s: nextOfflineRetryTaskID(%q) = %q, want %q", tc.name, tc.current, got, tc.want)
		}
		if got == tc.current {
			t.Errorf("%s: next ID equals the ID of the running task (%q): re-enqueue would conflict", tc.name, got)
		}
	}
}
