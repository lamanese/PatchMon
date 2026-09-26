package reports

import "sort"

// CustomerAlertTypes is the closed list of host-bound alert types a customer
// report may show. Everything else (user logins, SSH/RDP sessions, container
// events, enrolment, agent/server updates) is internal and never leaves the
// scope. Keep sorted; alerts_test.go checks it.
var CustomerAlertTypes = []string{
	"compliance_scan_completed",
	"compliance_scan_failed",
	"host_down",
	"host_pending_updates_exceeded",
	"host_pending_updates_resolved",
	"host_recovered",
	"host_security_updates_exceeded",
	"host_security_updates_resolved",
	"patch_reboot_required",
	"patch_run_approved",
	"patch_run_cancelled",
	"patch_run_completed",
	"patch_run_failed",
	"patch_run_started",
}

// IsCustomerAlertType reports whether an alert type may appear in a customer report.
func IsCustomerAlertType(t string) bool {
	i := sort.SearchStrings(CustomerAlertTypes, t)
	return i < len(CustomerAlertTypes) && CustomerAlertTypes[i] == t
}
