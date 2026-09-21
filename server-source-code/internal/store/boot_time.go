package store

import "time"

// bootTimeFutureTolerance absorbs clock skew between agent and server.
const bootTimeFutureTolerance = 5 * time.Minute

// bootTimeFloor rejects sentinels such as the Unix epoch, which a host with a
// dead RTC battery or a failed probe can report.
var bootTimeFloor = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// plausibleBootTime decides whether a reported boot instant may be stored.
// ok is false for agents that do not report one (nil) and for values that
// cannot be real; the caller then leaves the stored value alone.
func plausibleBootTime(reported *time.Time, now time.Time) (time.Time, bool) {
	if reported == nil {
		return time.Time{}, false
	}
	t := reported.UTC()
	if t.Before(bootTimeFloor) || t.After(now.Add(bootTimeFutureTolerance)) {
		return time.Time{}, false
	}
	return t, true
}
