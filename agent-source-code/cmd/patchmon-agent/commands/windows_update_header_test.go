package commands

import "testing"

func TestWindowsUpdateStepHeader(t *testing.T) {
	cases := []struct {
		name        string
		approved    int
		fetchFailed bool
		want        string
	}{
		{"updates to install", 3, false, "[Windows Update] Installing 3 approved update(s)...\n"},
		{"nothing pending", 0, false, "[Windows Update] No pending updates\n"},
		{"fetch failed: the error line already explains it", 0, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := windowsUpdateStepHeader(c.approved, c.fetchFailed); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
