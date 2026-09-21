package commands

import "fmt"

// windowsUpdateStepHeader is the first line of the Windows Update step of a
// patch_all run. Without it a run with nothing to install showed no Windows
// Update section at all, which read as if the step had been skipped.
func windowsUpdateStepHeader(approved int, fetchFailed bool) string {
	switch {
	case fetchFailed:
		return ""
	case approved == 0:
		return "[Windows Update] No pending updates\n"
	default:
		return fmt.Sprintf("[Windows Update] Installing %d approved update(s)...\n", approved)
	}
}
