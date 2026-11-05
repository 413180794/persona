package main

import "testing"

// A count that is not a whole number of 0 or more is refused: -1 divided by zero, junk silently ran as 0.
func TestSysmonCount(t *testing.T) {
	for _, c := range []struct {
		args []string
		n    int
		ok   bool
	}{
		{nil, 3, true}, {[]string{"0"}, 0, true}, {[]string{"30"}, 30, true},
		{[]string{"-1"}, 0, false}, {[]string{"abc"}, 0, false}, {[]string{"1", "2"}, 0, false},
	} {
		n, err := sysmonCount(c.args)
		if (err == nil) != c.ok || (c.ok && n != c.n) {
			t.Errorf("sysmonCount(%q) = %d, %v", c.args, n, err)
		}
	}
}
