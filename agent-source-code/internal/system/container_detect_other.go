//go:build !linux

package system

// runningInContainer is Linux-only; the mountinfo/cgroup/environ signals it
// relies on don't exist on other platforms, so getBootTime always falls
// through to gopsutil's host boot time there.
func runningInContainer() bool {
	return false
}
