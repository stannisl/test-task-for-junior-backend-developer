package task

import "time"

// DateOnlyUTC обрезает t в полуночь UTC, оставляя только дату без времени.
func DateOnlyUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
