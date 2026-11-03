package proto

import (
	"slices"
	"testing"
)

func TestClaudeDirBlocked(t *testing.T) {
	home := "/home/me"
	pats := []string{"~/.automation/*", "/opt/bots/claude-x"}
	for _, c := range []struct {
		dir  string
		want bool
	}{
		{"~/.automation/claude-a", true},
		{"/home/me/.automation/claude-b", true},        // absolute, the pattern written with ~
		{"/home/me/.automation/claude-b/", true},       // trailing slash
		{"/home/me/.automation/claude-b/nested", true}, // under a blocked directory
		{"/home/me/.automation", false},                // the parent itself is not matched by its children's glob
		{"/home/me/.automation-other/claude-c", false}, // a prefix of the name is not the directory
		{"/opt/bots/claude-x", true},                   // an exact path
		{"/opt/bots/claude-xy", false},
		{"~/.claude", false},
		{"", false},
	} {
		if got := ClaudeDirBlocked(c.dir, home, pats); got != c.want {
			t.Errorf("%q: got %v, want %v", c.dir, got, c.want)
		}
	}
	// Home unknown: only the same spelling matches.
	if !ClaudeDirBlocked("~/.automation/claude-a", "", pats) || ClaudeDirBlocked("/home/me/.automation/claude-a", "", pats) {
		t.Fatal("without home, ~ matches ~ only")
	}
	if ClaudeDirBlocked("/home/me/x", home, nil) {
		t.Fatal("no patterns block nothing")
	}
}

func TestCleanClaudeDirPatterns(t *testing.T) {
	got := CleanClaudeDirPatterns([]string{" ~/.a/* ", "", "~/.a/*", "~", "/", "/opt/b/"})
	if want := []string{"~/.a/*", "/opt/b"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for p, want := range map[string]bool{"~/.a/*": true, "/opt/b": true, "relative/x": false, "~/[": false} {
		if ValidClaudeDirPattern(p) != want {
			t.Errorf("valid %q: want %v", p, want)
		}
	}
}
