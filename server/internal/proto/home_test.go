package proto

import "testing"

func TestExpandHome(t *testing.T) {
	for _, c := range []struct{ in, home, want string }{
		{"~", "/home/user", "/home/user"},
		{"~/", "/home/user", "/home/user"},
		{"~/src/app", "/home/user", "/home/user/src/app"},
		{"~/src/../app", "/home/user", "/home/user/app"},
		{"/srv/app", "/home/user", "/srv/app"},
		{"", "/home/user", ""},
		{"~other/app", "/home/user", "~other/app"},
		{"src/~", "/home/user", "src/~"},
		// An unknown home leaves the path for whoever knows it (the node itself).
		{"~", "", "~"},
		{"~/src", "", "~/src"},
	} {
		if got := ExpandHome(c.in, c.home); got != c.want {
			t.Errorf("ExpandHome(%q, %q) = %q, want %q", c.in, c.home, got, c.want)
		}
	}
}
