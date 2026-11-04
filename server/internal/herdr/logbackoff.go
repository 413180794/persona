package herdr

import "time"

// LogBackoff spaces out the log lines about a failure that goes on: the first at once, the next 30s later, then twice
// the last gap each time, up to an hour. A computer that does not run herdr fails every poll, and one line per poll
// (or per 30s) filled the hub's journal with thousands of lines a day (#1765).
type LogBackoff struct {
	next time.Time
	gap  time.Duration
}

const (
	logGapFirst = 30 * time.Second
	logGapMax   = time.Hour
)

// Due reports whether a failure at now should be logged, and when it should moves the next one on.
func (b *LogBackoff) Due(now time.Time) bool {
	if now.Before(b.next) {
		return false
	}
	if b.gap == 0 {
		b.gap = logGapFirst
	} else {
		b.gap = min(b.gap*2, logGapMax)
	}
	b.next = now.Add(b.gap)
	return true
}

// Reset is for when the failure has ended: the next one is logged at once.
func (b *LogBackoff) Reset() { *b = LogBackoff{} }
