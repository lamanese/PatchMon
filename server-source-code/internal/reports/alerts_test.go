package reports

import (
	"sort"
	"testing"
)

func TestCustomerAlertTypesAreSortedUniqueHostBound(t *testing.T) {
	if !sort.StringsAreSorted(CustomerAlertTypes) {
		t.Fatalf("CustomerAlertTypes must be sorted: %v", CustomerAlertTypes)
	}
	seen := map[string]bool{}
	for _, a := range CustomerAlertTypes {
		if seen[a] {
			t.Fatalf("duplicate %q", a)
		}
		seen[a] = true
	}
	for _, want := range []string{"host_down", "host_recovered", "patch_run_failed", "patch_reboot_required", "compliance_scan_failed"} {
		if !IsCustomerAlertType(want) {
			t.Errorf("%q should be a customer alert type", want)
		}
	}
	for _, no := range []string{"user_login", "ssh_session_started", "rdp_session_started", "agent_update", "server_update", "host_enrolled", "host_deleted", "container_stopped", "test", ""} {
		if IsCustomerAlertType(no) {
			t.Errorf("%q must never reach a customer report", no)
		}
	}
}
