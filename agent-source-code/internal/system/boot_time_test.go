package system

import (
	"testing"
	"time"
)

func TestBootTimeFrom(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	t.Run("host uses the kernel boot time", func(t *testing.T) {
		got := bootTimeFrom(0, false, 1789900000, now)
		want := time.Unix(1789900000, 0).UTC()
		if got == nil || !got.Equal(want) || got.Location() != time.UTC {
			t.Fatalf("got %v, want %v in UTC", got, want)
		}
	})

	t.Run("container derives it from its own uptime, not the host's", func(t *testing.T) {
		got := bootTimeFrom(3*time.Hour, true, 1700000000, now)
		want := now.Add(-3 * time.Hour)
		if got == nil || !got.Equal(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("zero boot time means unknown", func(t *testing.T) {
		if got := bootTimeFrom(0, false, 0, now); got != nil {
			t.Fatalf("got %v, want nil so the server keeps its stored value", got)
		}
	})
}
