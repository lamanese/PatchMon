package reports

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// bigModel builds a fleet of n hosts with every section populated the way
// Collect would for a busy customer: 15 security updates and 3 disks per host,
// 500 activity rows, 100 reboots.
func bigModel(n int) *Model {
	m := sampleModel("de", true)
	now := m.GeneratedAt
	m.HostCount = n
	m.HostOverview = &HostOverview{}
	m.SecurityUpdatesByHost = &SecurityUpdatesByHost{}
	m.Disks = &DiskList{}
	m.HostsByUpdates = &HostUpdateList{}
	m.HostStatus = &HostStatusList{}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("srv-%03d.customer.example", i)
		seen := now.Add(-time.Duration(i) * time.Minute)
		m.HostOverview.Rows = append(m.HostOverview.Rows, HostOverviewRow{HostID: name, HostName: name, OS: "ubuntu 24.04.3 LTS", AgentVersion: "2.0.20", Status: "active", Updates: i % 40, SecurityUpdates: i % 7, NeedsReboot: i%3 == 0, BootTime: &seen, LastRunStatus: "completed", LastRunAt: &seen, LastSeen: &seen})
		h := HostSecurityUpdates{HostID: name, HostName: name}
		for j := 0; j < 15; j++ {
			h.Rows = append(h.Rows, SecurityUpdateRow{Package: fmt.Sprintf("libexample%d-dev", j), Installed: "1.2.3-4ubuntu0.1", Available: "1.2.3-4ubuntu0.2"})
		}
		m.SecurityUpdatesByHost.Hosts = append(m.SecurityUpdatesByHost.Hosts, h)
		for _, mnt := range []string{"/", "/var", "/home"} {
			m.Disks.Rows = append(m.Disks.Rows, DiskRow{HostID: name, HostName: name, Name: "/dev/sda1", Mount: mnt, Usage: &DiskUsage{TotalGB: 100, UsedGB: 50, FreeGB: 50, UsedPercent: 50}, Level: "ok"})
		}
		m.HostsByUpdates.Rows = append(m.HostsByUpdates.Rows, HostUpdateRow{HostID: name, HostName: name, Status: "active", Updates: i % 40, SecurityUpdates: i % 7, LastSeen: &seen})
		m.HostStatus.Rows = append(m.HostStatus.Rows, HostStatusRow{HostID: name, HostName: name, Status: "active", LastSeen: &seen})
	}
	m.PatchActivity = &PatchActivity{}
	for i := 0; i < 500; i++ {
		m.PatchActivity.Rows = append(m.PatchActivity.Rows, PatchRunRow{ID: fmt.Sprint(i), HostName: fmt.Sprintf("srv-%03d.customer.example", i%n), Status: "completed", PatchType: "patch_all", CreatedAt: now.Add(-time.Duration(i) * time.Hour)})
	}
	m.Reboots = &RebootList{}
	for i := 0; i < 100; i++ {
		m.Reboots.Rows = append(m.Reboots.Rows, RebootRow{HostName: fmt.Sprintf("srv-%03d.customer.example", i%n), Status: "sent", CreatedAt: now})
	}
	return m
}

func benchRender(b *testing.B, n int) {
	m := bigModel(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, _, err := RenderPDF(context.Background(), m, Branding{})
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(len(out)), "bytes")
	}
}

func BenchmarkRenderPDF50(b *testing.B)  { benchRender(b, 50) }
func BenchmarkRenderPDF500(b *testing.B) { benchRender(b, 500) }

func TestRenderPDF500HostsStaysUnderLimitAndDeadline(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	ctx, cancel := context.WithTimeout(context.Background(), RenderDeadline)
	defer cancel()
	start := time.Now()
	out, _, err := RenderPDF(ctx, bigModel(500), Branding{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("500 hosts: %d bytes in %s", len(out), time.Since(start))
	if len(out) > MaxPDFBytes {
		t.Fatalf("%d bytes exceed the limit", len(out))
	}
}
