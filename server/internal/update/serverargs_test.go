package update

import (
	"slices"
	"testing"
)

// A hub run as persona-hub comes back as `personad hub …` when personad is the target; anything else keeps its
// arguments as they were.
func TestTargetArgs(t *testing.T) {
	for _, c := range []struct {
		argv   []string
		target string
		want   []string
	}{
		{[]string{"/x/personad", "hub", "run"}, "/x/personad", []string{"hub", "run"}},
		{[]string{"/x/personad", "hub"}, "/x/personad", []string{"hub"}},
		{[]string{"/x/persona-hub"}, "/x/personad", []string{"hub"}},
		{[]string{"/x/persona-hub", "run"}, "/x/personad", []string{"hub", "run"}},
		{[]string{"/x/persona-hub", "hub", "run"}, "/x/personad", []string{"hub", "run"}},
		{[]string{"/x/persona-hub.exe", "run"}, "/x/personad.exe", []string{"hub", "run"}},
		{[]string{"/x/persona-hub", "run"}, "/x/persona-hub", []string{"run"}},
		{nil, "/x/personad", []string{"hub"}},
		{[]string{"/x/personad"}, "/x/personad", []string{"hub"}}, // as the watchdog did before
		{[]string{"/x/persona-hub"}, "/x/persona-hub", []string{"hub"}},
	} {
		if got := TargetArgs(c.argv, c.target); !slices.Equal(got, c.want) {
			t.Errorf("%q -> %s: %q, want %q", c.argv, c.target, got, c.want)
		}
	}
}
