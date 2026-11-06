package nodecmd

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A count that is not a whole number of 0 or more is refused: -1 divided by zero, junk silently ran as 0.
func TestSysmonCount(t *testing.T) {
	for _, c := range []struct {
		args []string
		n    int
		ok   bool
	}{
		{nil, 3, true}, {[]string{"0"}, 0, true}, {[]string{"30"}, 30, true},
		{[]string{"-1"}, 0, false}, {[]string{"abc"}, 0, false}, {[]string{"1", "2"}, 0, false},
		{[]string{strconv.Itoa(sysmonMaxCount)}, sysmonMaxCount, true},
		{[]string{strconv.Itoa(sysmonMaxCount + 1)}, 0, false}, {[]string{"9223372036854775807"}, 0, false},
	} {
		n, err := sysmonCount(c.args)
		if (err == nil) != c.ok || (c.ok && n != c.n) {
			t.Errorf("sysmonCount(%q) = %d, %v", c.args, n, err)
		}
	}
}

// Too big a count says what the most is, so the next try is the right one.
func TestSysmonCountTooBigSaysTheMost(t *testing.T) {
	_, err := sysmonCount([]string{"9223372036854775807"})
	if err == nil || !strings.Contains(err.Error(), strconv.Itoa(sysmonMaxCount)) {
		t.Fatalf("err = %v, want it to name %d", err, sysmonMaxCount)
	}
}

// 1 + n samples when left alone; when ctx ends in a wait it stops at once and says how many it took.
func TestSysmonSamples(t *testing.T) {
	calls := 0
	if got := sysmonSamples(context.Background(), 2, time.Millisecond, func() { calls++ }); got != 3 || calls != 3 {
		t.Errorf("n=2: took %d, called %d, want 3", got, calls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	calls = 0
	start := time.Now()
	// Ctrl-C while waiting an hour for the second sample: it must stop then, not after the hour.
	stop := time.AfterFunc(20*time.Millisecond, cancel)
	defer stop.Stop()
	got := sysmonSamples(ctx, 5, time.Hour, func() { calls++ })
	if got != 1 || calls != 1 {
		t.Errorf("stopped after the first: took %d, called %d, want 1", got, calls)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("stopping took %v", d)
	}

	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	calls = 0
	if got := sysmonSamples(ctx, 5, time.Hour, func() { calls++ }); got != 0 || calls != 0 {
		t.Errorf("stopped before the first: took %d, called %d, want 0", got, calls)
	}
}
