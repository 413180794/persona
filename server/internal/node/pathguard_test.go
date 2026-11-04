package node

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A path into iCloud Drive or onto a secret is never looked at: asked directly, through a link, or through a link
// in the middle of the path. Everything else resolves through its links.
func TestPathGuard(t *testing.T) {
	home := t.TempDir()
	n := &Node{home: home}
	write := func(rel, body string) {
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, rel string) {
		if err := os.Symlink(target, filepath.Join(home, rel)); err != nil {
			t.Skip("symlinks:", err)
		}
	}
	write("Library/Mobile Documents/com~apple~CloudDocs/a.md", "cloud")
	write(".ssh/id_ed25519", "key")
	write(".ssh/config", "Host x")
	write(".persona/hub.json", `{"token":"x"}`)
	write(".persona/hub.json.bak-20261031", `{"token":"x"}`)
	write(".jarvis/apns_key.p8", "key")
	write(".persona/secret.key", "key")
	write(".persona/data/people/a.md", "# a")
	write("code/repo/id_rsa", "key")
	write("code/repo/main.go", "package main\n")
	link(filepath.Join(home, "Library/Mobile Documents"), "cloud")
	link("../.ssh", "code/keys")
	link("repo/main.go", "code/main.go")
	link(filepath.Join(home, "code/repo"), "repo")

	stat := n.statPaths([]string{
		"~/Library/Mobile Documents/com~apple~CloudDocs/a.md",
		"~/library/mobile documents/com~apple~CloudDocs/a.md", // macOS: case-insensitive
		"~/cloud/com~apple~CloudDocs/a.md",                    // a link into iCloud Drive
		"~/.ssh/config",
		"~/code/keys/id_ed25519", // a link to ~/.ssh
		"~/.persona/hub.json",
		"~/.persona/hub.json.bak-20261031",
		"~/.jarvis/apns_key.p8",
		"~/.persona/secret.key",
		"~/code/repo/id_rsa",
		"~/.persona/data/people/a.md",
		"~/code/main.go", // a link to a file
		"~/repo/main.go", // a link in the middle
		"~/repo/../repo/main.go",
	}, "")
	want := []string{"missing", "missing", "missing", "missing", "missing", "missing", "missing", "missing", "missing", "missing", "file", "file", "file", "file"}
	for i, s := range stat {
		if s.Kind != want[i] {
			t.Errorf("%s: %s, want %s", s.Path, s.Kind, want[i])
		}
	}

	if _, err := n.readFile("~/cloud/com~apple~CloudDocs/a.md", "", 0, false); !errors.Is(err, errICloud) {
		t.Errorf("read through a link into iCloud Drive: %v", err)
	}
	if _, err := n.readFile("keys/id_ed25519", filepath.Join(home, "code"), 0, false); !errors.Is(err, errOffLimits) {
		t.Errorf("read a key through a link: %v", err)
	}
	if _, err := n.listDir("~/code/keys", ""); !errors.Is(err, errOffLimits) {
		t.Errorf("list ~/.ssh through a link: %v", err)
	}
	if _, _, _, err := n.openRegular("~/.persona/hub.json", ""); !errors.Is(err, errOffLimits) {
		t.Errorf("download hub.json: %v", err)
	}
	fc, err := n.readFile("main.go", filepath.Join(home, "repo"), 0, false)
	if err != nil || fc.Text != "package main\n" || fc.Path != filepath.Join(home, "repo/main.go") {
		t.Fatalf("read through links: %+v %v", fc, err)
	}
	// A listing marks a link to a directory as one, except a link into iCloud Drive (not looked at).
	ls, err := n.listDir("~", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ls["entries"].([]DirEntry) {
		switch e.Name {
		case "repo":
			if !e.Dir || !e.Symlink {
				t.Errorf("repo: %+v", e)
			}
		case "cloud":
			if e.Dir {
				t.Errorf("a link into iCloud Drive was followed: %+v", e)
			}
		}
	}
}

func TestRealPathLoop(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("b", filepath.Join(dir, "a")); err != nil {
		t.Skip("symlinks:", err)
	}
	_ = os.Symlink("a", filepath.Join(dir, "b"))
	if _, err := (&Node{}).realPath(filepath.Join(dir, "a")); err == nil || !strings.Contains(err.Error(), "too many links") {
		t.Fatalf("a loop of links: %v", err)
	}
	// A missing component ends the walk with the error looking at it gave.
	if _, err := (&Node{}).realPath(filepath.Join(dir, "nope", "x")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
}

// One stat that does not answer leaves its path missing and does not hold up the others.
func TestStatPathsTimeout(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.go"), nil, 0o600)
	oldTimeout, oldStat := statTimeout, statOne
	defer func() { statTimeout, statOne = oldTimeout, oldStat }()
	hang := make(chan struct{})
	defer close(hang)
	statTimeout = 100 * time.Millisecond
	statOne = func(n *Node, asked, cwd string) PathStat {
		if asked == "~/stuck" {
			<-hang
		}
		return oldStat(n, asked, cwd)
	}
	start := time.Now()
	got := (&Node{home: dir}).statPaths([]string{"~/stuck", "~/a.go"}, "")
	if took := time.Since(start); took > time.Second {
		t.Fatalf("took %s", took)
	}
	if len(got) != 2 || got[0].Kind != "missing" || got[0].Path != "~/stuck" || got[1].Kind != "file" {
		t.Fatalf("%+v", got)
	}
}
