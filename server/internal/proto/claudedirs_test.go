package proto

import (
	"slices"
	"testing"
)

func TestClaudeDirBlocked(t *testing.T) {
	home := "/home/me"
	pats := []string{"~/.automation/*", "/opt/bots/claude-x", "*has_workspaces*"}
	for _, c := range []struct {
		dir  string
		want bool
	}{
		{"~/.automation/claude-a", true},
		{"/home/me/.automation/claude-b", true},               // absolute, the pattern written with ~
		{"/home/me/.automation/claude-b/", true},              // trailing slash
		{"/home/me/.automation/claude-b/nested", true},        // under a blocked directory
		{"/data00/home/me/has_workspaces/uuid/workdir", true}, // matching directory name segment
		{"/home/me/.automation", false},                       // the parent itself is not matched by its children's glob
		{"/home/me/.automation-other/claude-c", false},        // a prefix of the name is not the directory
		{"/opt/bots/claude-x", true},                          // an exact path
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
	// Spellings of home or root that only differ by slashes or "." also block everything, so they are dropped too.
	everything := []string{"~/", "~//", "//", "/./", "~/.", "~/./", ".", "./", "~/..", "~/../.."}
	if got := CleanClaudeDirPatterns(everything); len(got) != 0 {
		t.Fatalf("patterns that block everything survived: %q", got)
	}
	for _, dir := range []string{"/home/me/work/x", "~/.claude", "/opt/bots/claude-x"} {
		if by := ClaudeDirBlockedBy(dir, "/home/me", CleanClaudeDirPatterns(everything)); by != "" {
			t.Errorf("%q blocked by %q", dir, by)
		}
	}
	// Trailing slashes go; what is left is kept as written.
	if got, want := CleanClaudeDirPatterns([]string{"/opt/b//", "~/.a//", "/opt/b"}), []string{"/opt/b", "~/.a"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for p, want := range map[string]bool{"~/.a/*": true, "/opt/b": true, "*has_workspaces*": true, "relative/x": false, "~/[": false} {
		if ValidClaudeDirPattern(p) != want {
			t.Errorf("valid %q: want %v", p, want)
		}
	}
}
