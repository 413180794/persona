package update

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// Commits of a made-up history: a, b, c on master, then d deployed straight from master after the release at b.
const (
	commitA = "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
	commitB = "b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2"
	commitC = "c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3"
	commitD = "d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4"
)

func releaseOf(t *testing.T, commit string, ancestors ...string) *Manifest {
	t.Helper()
	enc, err := EncodeAncestors(ancestors)
	if err != nil {
		t.Fatal(err)
	}
	return &Manifest{Version: "v", Build: 202611030000, Commit: commit, Ancestors: enc}
}

// What git rev-list prints goes in as is: order, duplicates and blank lines do not matter; each commit keeps 6 bytes.
func TestEncodeAncestors(t *testing.T) {
	enc, err := EncodeAncestors([]string{commitC, "", commitA, commitB + "\n", commitA})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(enc)
	if len(raw) != 3*ancestorBytes {
		t.Fatalf("%d bytes for 3 commits", len(raw))
	}
	m := &Manifest{Ancestors: enc}
	for _, c := range []string{commitA, commitB, commitC, strings.ToUpper(commitB)} {
		if ok, err := m.Contains(c); !ok || err != nil {
			t.Errorf("Contains(%.8s) = %v, %v", c, ok, err)
		}
	}
	if ok, err := m.Contains(commitD); ok || err != nil {
		t.Errorf("a commit not in the list: %v, %v", ok, err)
	}
	for _, bad := range [][]string{{"abc"}, {"zz" + commitA[2:]}, {}} {
		if _, err := EncodeAncestors(bad); err == nil {
			t.Errorf("EncodeAncestors(%q) took it", bad)
		}
	}
	if _, err := (&Manifest{Ancestors: "not base64!"}).Contains(commitA); err == nil {
		t.Error("a list that does not read")
	}
	// Older hubs read the manifest as before: the field is optional.
	var back Manifest
	b, _ := json.Marshal(Manifest{Version: "v", Build: 1, Commit: commitC, Ancestors: enc})
	if err := json.Unmarshal(b, &back); err != nil || back.Ancestors != enc {
		t.Fatalf("round trip: %v %q", err, back.Ancestors)
	}
}

// A hub installs a release by itself only when the release's code contains the commit it runs (or is that commit);
// anything it cannot be sure of holds it back, with the reason.
func TestHold(t *testing.T) {
	atB := releaseOf(t, commitB, commitB, commitA) // published from b
	atD := releaseOf(t, commitD, commitD, commitC, commitB, commitA)
	for _, c := range []struct {
		name   string
		commit string
		m      *Manifest
		held   string // a word the reason has; "" when not held
	}{
		{"the release contains the hub's commit", commitB, atD, ""},
		{"the same commit (published again)", commitD, atD, ""},
		{"an older first commit is there too", commitA, atD, ""},
		{"published from code behind the hub's", commitD, atB, "不包含"},
		{"a hub that does not know its commit", "", atD, "没有记下"},
		{"built with changes not committed", commitB + "-dirty", atD, "没提交的改动"},
		{"a short commit cannot be looked up", "b2b2b2", atD, "不完整"},
		{"a manifest from an older publish.sh", commitB, &Manifest{Commit: commitD}, "没有提交记录"},
		{"a list that does not read", commitB, &Manifest{Commit: commitD, Ancestors: "@@"}, "读不出来"},
	} {
		got := Build{Commit: c.commit}.Hold(c.m)
		if c.held == "" && got != "" || c.held != "" && !strings.Contains(got, c.held) {
			t.Errorf("%s: Hold = %q, want %q", c.name, got, c.held)
		}
	}
	if got := (Build{Commit: commitD}).Hold(atB); !strings.Contains(got, commitB[:8]) || !strings.Contains(got, commitD[:8]) {
		t.Errorf("the reason names both commits: %q", got)
	}
}
