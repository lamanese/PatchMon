// Package serverident identifies the machine the PatchMon server itself runs
// on, for the reboot self-exclusion check. It is shared by the HTTP handlers
// (bulk reboot) and the queue workers (scheduled reboots), which must apply
// the exact same exclusion logic.
package serverident

import (
	"os"
	"slices"
	"strings"
	"sync"
)

// isContainerized reports whether the server runs inside a container
// (Docker creates /.dockerenv, Podman /run/.containerenv).
var isContainerized = sync.OnceValue(func() bool {
	for _, p := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
})

// NormalizeMachineID canonicalizes a machine identifier for comparison:
// agents report gopsutil's host.HostID() (on Linux the dashed DMI product
// UUID), while operators may configure the undashed /etc/machine-id form.
func NormalizeMachineID(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "-", "")
}

// MachineIDs caches the normalized candidate identifiers of the machine
// running the PatchMon server itself, for the reboot self-exclusion check.
//
// Agents report gopsutil's host.HostID(), which on Linux prefers the DMI
// product UUID and falls back to /etc/machine-id (e.g. in LXC or VMs without
// DMI). The server therefore collects every identifier its own host may be
// known by: the PM_SERVER_MACHINE_ID override, the DMI product UUID (sysfs is
// host-wide even inside containers), the host's machine-id bind-mounted to
// /run/host-machine-id (see docker-compose), and the local machine-id files
// outside containers. An empty result means self-exclusion cannot work;
// callers must fail closed in that case.
var MachineIDs = sync.OnceValue(func() []string {
	var ids []string
	add := func(s string) {
		if n := NormalizeMachineID(s); n != "" && !slices.Contains(ids, n) {
			ids = append(ids, n)
		}
	}
	add(os.Getenv("PM_SERVER_MACHINE_ID"))
	if b, err := os.ReadFile("/sys/class/dmi/id/product_uuid"); err == nil {
		add(string(b))
	}
	// The Docker host's machine-id, bind-mounted read-only into the container.
	if b, err := os.ReadFile("/run/host-machine-id"); err == nil {
		add(string(b))
	}
	if !isContainerized() {
		for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			if b, err := os.ReadFile(p); err == nil {
				add(string(b))
			}
		}
	}
	return ids
})

// IsSelf reports whether the given agent-reported machine ID identifies the
// machine the PatchMon server itself runs on. Matches on machine_id only: a
// hostname fallback would let a host that merely shares the server's (short)
// hostname dodge reboots, and hostnames are agent-reported and freely
// choosable.
func IsSelf(machineID *string) bool {
	if machineID == nil {
		return false
	}
	id := NormalizeMachineID(*machineID)
	return id != "" && slices.Contains(MachineIDs(), id)
}
