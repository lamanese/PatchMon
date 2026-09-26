package queue

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRunKeyDistinguishesTriggersAndTenants(t *testing.T) {
	slot := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	s := ScheduledReportRunPayload{ReportID: "r1", Trigger: ReportTriggerScheduled, SlotAt: &slot}
	m := ScheduledReportRunPayload{ReportID: "r1", Trigger: ReportTriggerManual, RunID: "u-1"}
	tenant := ScheduledReportRunPayload{ReportID: "r1", Host: "acme", Trigger: ReportTriggerScheduled, SlotAt: &slot}
	ks, _ := s.RunKey()
	km, _ := m.RunKey()
	kt, _ := tenant.RunKey()
	if ks != "sched::r1:1790575200" || km != "manual::r1:u-1" || kt != "sched:acme:r1:1790575200" {
		t.Fatalf("keys %q %q %q", ks, km, kt)
	}
}

func TestRunKeyRejectsIncompletePayloads(t *testing.T) {
	for _, p := range []ScheduledReportRunPayload{
		{ReportID: "r1"},
		{ReportID: "r1", Trigger: ReportTriggerScheduled},
		{ReportID: "r1", Trigger: ReportTriggerManual},
		{ReportID: "r1", Trigger: "cron"},
	} {
		if _, err := p.RunKey(); err == nil {
			t.Errorf("%+v must be rejected", p)
		}
	}
}

func TestScheduledReportTaskIDIsDerivedFromRunKey(t *testing.T) {
	slot := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	sched := ScheduledReportRunPayload{ReportID: "r1", Trigger: ReportTriggerScheduled, SlotAt: &slot}
	manual := ScheduledReportRunPayload{ReportID: "r1", Trigger: ReportTriggerManual, RunID: "u-1"}
	ks, _ := sched.RunKey()
	km, _ := manual.RunKey()
	if scheduledReportTaskID(ks) != "srr:sched::r1:1790575200" {
		t.Fatalf("task id %q", scheduledReportTaskID(ks))
	}
	if scheduledReportTaskID(ks) == scheduledReportTaskID(km) {
		t.Fatal("manual and scheduled runs in the same minute must not collide")
	}
	// asynq exposes no option getter on Task, so the id is pinned through the
	// helper above and the task is checked for type, payload round trip and
	// construction success (NewScheduledReportRunTask fails on a bad payload).
	task, err := NewScheduledReportRunTask(sched)
	if err != nil {
		t.Fatal(err)
	}
	if task.Type() != TypeScheduledReportRun {
		t.Fatalf("type %q", task.Type())
	}
	var p ScheduledReportRunPayload
	if err := json.Unmarshal(task.Payload(), &p); err != nil || p.SlotAt == nil || !p.SlotAt.Equal(slot) || p.Trigger != ReportTriggerScheduled {
		t.Fatalf("payload round trip %+v %v", p, err)
	}
	if _, err := NewScheduledReportRunTask(ScheduledReportRunPayload{ReportID: "r1", Trigger: ReportTriggerScheduled}); err == nil {
		t.Fatal("a scheduled task without slot must not be constructible")
	}
}
