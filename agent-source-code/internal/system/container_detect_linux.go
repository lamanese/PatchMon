//go:build linux

package system

// runningInContainer reports whether the agent runs inside a container, whether
// or not the container's own uptime could be established. containerUptime
// returns false for both "not a container" and "probe failed"; the boot time
// must tell them apart, because gopsutil's fallback is the hypervisor's.
func runningInContainer() bool {
	_, lxcfsProcMount := defaultProcSources.mountinfoSignals()
	return defaultProcSources.inContainer(lxcfsProcMount)
}
