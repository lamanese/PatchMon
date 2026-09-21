package store

import (
	"testing"
	"time"
)

func TestPlausibleBootTime(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	ptr := func(v time.Time) *time.Time { return &v }
	zurich := time.FixedZone("CEST", 2*3600)

	cases := []struct {
		name     string
		reported *time.Time
		want     time.Time
		ok       bool
	}{
		{"nil from old agent", nil, time.Time{}, false},
		{"normal value", ptr(now.Add(-72 * time.Hour)), now.Add(-72 * time.Hour), true},
		{"non-UTC input is normalised", ptr(time.Date(2026, 9, 20, 10, 0, 0, 0, zurich)), time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC), true},
		{"clock skew up to 5 min is tolerated", ptr(now.Add(4 * time.Minute)), now.Add(4 * time.Minute), true},
		{"future beyond tolerance is rejected", ptr(now.Add(6 * time.Minute)), time.Time{}, false},
		{"exactly the 5 min tolerance is accepted", ptr(now.Add(5 * time.Minute)), now.Add(5 * time.Minute), true},
		{"5 min tolerance plus one second is rejected", ptr(now.Add(5*time.Minute + time.Second)), time.Time{}, false},
		{"before year 2000 is rejected", ptr(time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC)), time.Time{}, false},
		{"unix epoch sentinel is rejected", ptr(time.Unix(0, 0)), time.Time{}, false},
		{"exactly the year 2000 floor is accepted", ptr(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)), time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"one nanosecond before the floor is rejected", ptr(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Nanosecond)), time.Time{}, false},
		{"one second before the floor is rejected", ptr(time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC)), time.Time{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := plausibleBootTime(c.reported, now)
			if ok != c.ok || !got.Equal(c.want) {
				t.Fatalf("plausibleBootTime() = %v, %v; want %v, %v", got, ok, c.want, c.ok)
			}
			if ok && got.Location() != time.UTC {
				t.Fatalf("result not in UTC: %v", got.Location())
			}
		})
	}
}
