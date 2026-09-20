package store

import (
	"context"
	"testing"
)

// TestProcessReport_PackageStateHint covers the fork "package installation
// incomplete" hint (dpkg --audit, agents 2.0.15+): a reporting agent sets and
// clears it, an agent that does not know the field leaves it alone.
func TestProcessReport_PackageStateHint(t *testing.T) {
	d := newDefinitionUpdatesTestDB(t, false)
	ctx := context.Background()
	hostID := insertDefUpdatesTestHost(t, d, "pkg-state-host", "Ubuntu")
	reports := NewReportStore(d)

	read := func() (bool, string) {
		t.Helper()
		var broken bool
		var detail *string
		if err := d.RawQueryRow(ctx, `SELECT fork_pkg_broken, fork_pkg_broken_detail FROM hosts WHERE id = $1`, hostID).Scan(&broken, &detail); err != nil {
			t.Fatalf("read host: %v", err)
		}
		if detail == nil {
			return broken, ""
		}
		return broken, *detail
	}
	send := func(broken *bool, detail string) {
		t.Helper()
		p := &ReportPayload{Packages: []ReportPackage{}, PackageStateBroken: broken, PackageStateDetail: detail}
		if _, err := reports.ProcessReport(ctx, hostID, p); err != nil {
			t.Fatalf("ProcessReport: %v", err)
		}
	}
	yes, no := true, false

	send(&yes, "fwupd, php8.0-fpm")
	if broken, detail := read(); !broken || detail != "fwupd, php8.0-fpm" {
		t.Fatalf("after broken report: got (%v, %q)", broken, detail)
	}

	// An agent older than 2.0.15 does not send the field: keep the last value.
	send(nil, "")
	if broken, detail := read(); !broken || detail != "fwupd, php8.0-fpm" {
		t.Fatalf("report without the field must not clear the hint: got (%v, %q)", broken, detail)
	}

	// Repaired on the host: the next report clears flag and detail.
	send(&no, "stale detail must be dropped")
	if broken, detail := read(); broken || detail != "" {
		t.Fatalf("after clean report: got (%v, %q)", broken, detail)
	}
}
