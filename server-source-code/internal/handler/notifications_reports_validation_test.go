package handler

import (
	"testing"
	"time"
)

func TestValidateCustomerCronRequiresOneHour(t *testing.T) {
	now := time.Date(2026, 9, 28, 5, 0, 0, 0, time.UTC)
	if err := validateCustomerCron("*/30 * * * *", "UTC", now); err == nil {
		t.Fatal("every 30 minutes must be rejected")
	}
	if err := validateCustomerCron("0,30 9 * * *", "UTC", now); err == nil {
		t.Fatal("two runs 30 minutes apart inside one day must be rejected")
	}
	if err := validateCustomerCron("0 * * * *", "UTC", now); err != nil {
		t.Fatalf("hourly is the minimum: %v", err)
	}
	if err := validateCustomerCron("0 6 * * 1", "UTC", now); err != nil {
		t.Fatalf("weekly: %v", err)
	}
	if err := validateCustomerCron("0 6 * * 1", "Europe/Zurich", now); err != nil {
		t.Fatalf("weekly: %v", err)
	}
	if err := validateCustomerCron("not a cron", "UTC", now); err == nil {
		t.Fatal("invalid expressions are errors")
	}
}

func TestRunNowLimiterAllowsOncePerCooldown(t *testing.T) {
	clock := time.Date(2026, 9, 28, 5, 0, 0, 0, time.UTC)
	l := &runNowLimiter{last: map[string]time.Time{}, now: func() time.Time { return clock }}
	if _, ok := l.allow("r1"); !ok {
		t.Fatal("first call allowed")
	}
	if wait, ok := l.allow("r1"); ok || wait <= 0 || wait > RunNowCooldown {
		t.Fatalf("second call within cooldown must be refused with the remaining wait, got %v %v", wait, ok)
	}
	if _, ok := l.allow("r2"); !ok {
		t.Fatal("other reports are independent")
	}
	clock = clock.Add(RunNowCooldown)
	if _, ok := l.allow("r1"); !ok {
		t.Fatal("allowed again after the cooldown")
	}
	l.forget("r1")
	if _, ok := l.allow("r1"); !ok {
		t.Fatal("forget releases the slot (enqueue failure)")
	}
}
