package queue

import (
	"testing"
	"time"

	"github.com/PatchMon/PatchMon/server-source-code/internal/db"
	"github.com/PatchMon/PatchMon/server-source-code/internal/pgtime"
)

func ptr[T any](v T) *T { return &v }

func mustTime(t *testing.T, layout, value string, loc *time.Location) time.Time {
	t.Helper()
	tm, err := time.ParseInLocation(layout, value, loc)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return tm
}

func TestRebootScheduleSlotOnce(t *testing.T) {
	runAt := time.Date(2026, 6, 28, 13, 0, 0, 0, time.UTC)
	s := db.RebootSchedule{ScheduleType: "once", RunAt: pgtime.From(runAt)}

	cases := []struct {
		name   string
		now    time.Time
		due    bool
		missed bool
	}{
		{"before slot", runAt.Add(-time.Minute), false, false},
		{"at slot", runAt, true, false},
		{"within tolerance", runAt.Add(rebootScheduleTolerance), true, false},
		{"past tolerance", runAt.Add(rebootScheduleTolerance + time.Second), false, true},
		{"hours late", runAt.Add(6 * time.Hour), false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			slot, due, missed, err := rebootScheduleSlot(s, c.now)
			if err != nil {
				t.Fatal(err)
			}
			if due != c.due || missed != c.missed {
				t.Errorf("due=%v missed=%v, want due=%v missed=%v", due, missed, c.due, c.missed)
			}
			if !slot.Equal(runAt) {
				t.Errorf("slot=%v, want %v", slot, runAt)
			}
		})
	}
}

func TestRebootScheduleSlotOnceAlreadyRunNotMissed(t *testing.T) {
	runAt := time.Date(2026, 6, 28, 13, 0, 0, 0, time.UTC)
	s := db.RebootSchedule{
		ScheduleType: "once",
		RunAt:        pgtime.From(runAt),
		LastRunAt:    pgtime.From(runAt.Add(time.Minute)),
	}
	_, due, missed, err := rebootScheduleSlot(s, runAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if due || missed {
		t.Errorf("consumed slot reported due=%v missed=%v, want neither", due, missed)
	}
}

func TestRebootScheduleSlotWeekly(t *testing.T) {
	zurich, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	// Sunday 13:00 Europe/Zurich. 2026-06-28 is a Sunday (CEST, UTC+2).
	s := db.RebootSchedule{
		ScheduleType: "weekly",
		Weekday:      ptr(int32(0)),
		TimeOfDay:    ptr("13:00"),
		Timezone:     "Europe/Zurich",
	}
	wantSlot := mustTime(t, "2006-01-02 15:04", "2026-06-28 13:00", zurich)

	cases := []struct {
		name string
		now  time.Time
		due  bool
	}{
		{"just before", wantSlot.Add(-time.Second), false}, // resolves to previous week's slot
		{"at slot", wantSlot, true},
		{"within tolerance", wantSlot.Add(rebootScheduleTolerance), true},
		{"past tolerance", wantSlot.Add(rebootScheduleTolerance + time.Second), false},
		{"next day", wantSlot.Add(24 * time.Hour), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			slot, due, missed, err := rebootScheduleSlot(s, c.now)
			if err != nil {
				t.Fatal(err)
			}
			if missed {
				t.Error("weekly schedule must never be marked missed")
			}
			if due != c.due {
				t.Errorf("due=%v, want %v (slot=%v)", due, c.due, slot)
			}
			if c.due && !slot.Equal(wantSlot.UTC()) {
				t.Errorf("slot=%v, want %v", slot, wantSlot.UTC())
			}
		})
	}
}

func TestRebootScheduleSlotWeeklyLocalTimeAcrossDST(t *testing.T) {
	zurich, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	s := db.RebootSchedule{
		ScheduleType: "weekly",
		Weekday:      ptr(int32(0)),
		TimeOfDay:    ptr("13:00"),
		Timezone:     "Europe/Zurich",
	}
	// 2026-01-04 is a Sunday in CET (UTC+1); the slot must stay 13:00 local,
	// i.e. 12:00 UTC in winter vs 11:00 UTC in summer.
	winterSlot := mustTime(t, "2006-01-02 15:04", "2026-01-04 13:00", zurich)
	slot, due, _, err := rebootScheduleSlot(s, winterSlot.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !due {
		t.Fatal("expected due at winter slot")
	}
	if got := slot.UTC().Hour(); got != 12 {
		t.Errorf("winter slot UTC hour = %d, want 12", got)
	}
}

func TestRebootScheduleSlotInvalid(t *testing.T) {
	cases := []db.RebootSchedule{
		{ScheduleType: "once"}, // no run_at
		{ScheduleType: "weekly", Weekday: ptr(int32(7)), TimeOfDay: ptr("13:00"), Timezone: "UTC"},
		{ScheduleType: "weekly", Weekday: ptr(int32(0)), Timezone: "UTC"},
		{ScheduleType: "weekly", Weekday: ptr(int32(0)), TimeOfDay: ptr("25:99"), Timezone: "UTC"},
		{ScheduleType: "weekly", Weekday: ptr(int32(0)), TimeOfDay: ptr("13:00"), Timezone: "Mars/Olympus"},
		{ScheduleType: "monthly"},
	}
	now := time.Now()
	for _, s := range cases {
		if _, _, _, err := rebootScheduleSlot(s, now); err == nil {
			t.Errorf("schedule %+v: expected error", s)
		}
	}
}
