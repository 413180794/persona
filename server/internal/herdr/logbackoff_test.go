package herdr

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// Polled every 30s for a day, a failure that never ends is logged 30 times, not 2880 (#1765): at once, 30s later, then
// twice the last gap each time, up to an hour apart.
func TestLogBackoffSpacesOutLines(t *testing.T) {
	var b LogBackoff
	start := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	var at []time.Duration
	for d := time.Duration(0); d < 24*time.Hour; d += 30 * time.Second {
		if b.Due(start.Add(d)) {
			at = append(at, d)
		}
	}
	s := time.Second
	first := []time.Duration{0, 30 * s, 90 * s, 210 * s, 450 * s, 930 * s, 1890 * s, 3810 * s}
	if len(at) < len(first) || !slices.Equal(at[:len(first)], first) {
		t.Fatalf("first lines at %v, want %v", at, first)
	}
	if len(at) != 30 {
		t.Fatalf("%d lines in a day, want 30", len(at))
	}
	if gap := at[len(at)-1] - at[len(at)-2]; gap != time.Hour {
		t.Fatalf("last gap %s, want an hour", gap)
	}
	b.Reset()
	if !b.Due(start.Add(24 * time.Hour)) {
		t.Fatal("after Reset the next failure is not logged at once")
	}
}

// The client's transport failures follow it: an hour of failing polls is 7 lines, herdr answering again is one, and the
// next failure is logged at once.
func TestNoteLogsAFailureThatGoesOnLessAndLess(t *testing.T) {
	clock := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	var lines []string
	c := &Client{Log: func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }, now: func() time.Time { return clock }}
	gone := fmt.Errorf("%w: dial unix herdr.sock: connect: no such file or directory", errNoSocket)
	for range 120 { // an hour of polls, every 30s
		c.note("session.snapshot", gone)
		clock = clock.Add(30 * time.Second)
	}
	if len(lines) != 7 || !strings.Contains(lines[0], "session.snapshot failed") {
		t.Fatalf("%d lines for an hour of failures: %q", len(lines), lines)
	}
	c.note("session.snapshot", nil)
	if last := lines[len(lines)-1]; !strings.Contains(last, "reachable again after 1h0m0s (120 failed calls)") {
		t.Fatalf("recovery line %q", last)
	}
	n := len(lines)
	c.note("agent.list", gone)
	if len(lines) != n+1 || !strings.Contains(lines[n], "agent.list failed") {
		t.Fatalf("a new failure after recovery is not logged at once: %q", lines[n:])
	}
}
