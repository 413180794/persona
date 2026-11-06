package node

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// git.show prints a commit's diff, which is the content of every file it touched: the files file.read refuses (a key,
// a home kept in Git with its credentials folders, the data folder's secrets) are left out of it, the rest diff as
// before and every file is still listed.
func TestGitShowLeavesOutGuardedFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Sample Dev")
	t.Setenv("GIT_AUTHOR_EMAIL", "dev@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Sample Dev")
	t.Setenv("GIT_COMMITTER_EMAIL", "dev@example.com")
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
	// A home kept in Git (dotfiles), with a project in it.
	runPullGit(t, home, "init", "-q", "--initial-branch=main")
	guarded := []string{".aws/credentials", ".ssh/config", "code/repo/id_ed25519", ".persona/hub.json", ".netrc"}
	write("notes.md", "plain\n")
	write("code/repo/main.go", "package main\n")
	for _, p := range guarded {
		write(p, "SECRET-"+p+"\n")
	}
	runPullGit(t, home, "add", "-A")
	runPullGit(t, home, "commit", "-q", "-m", "Add sample files")
	first := runPullGit(t, home, "rev-parse", "HEAD")
	write("notes.md", "plain, edited\n")
	write(".aws/credentials", "SECRET-AWS-2\n")
	write("code/repo/id_ed25519", "SECRET-KEY-2\n")
	runPullGit(t, home, "mv", ".netrc", "netrc.txt") // a secret moved out from under its name: its diff is still the secret
	runPullGit(t, home, "commit", "-q", "-am", "Edit sample files")
	ctx := context.Background()

	for _, c := range []struct{ cwd, hash string }{
		{"~", first},
		{"~", "HEAD"},
		{filepath.Join(home, "code"), "HEAD"}, // from a folder inside the repository
		{filepath.Join(home, "code/repo"), first},
		{"~", "HEAD~1..HEAD"},
	} {
		s, err := n.gitShow(ctx, c.cwd, c.hash)
		if err != nil {
			t.Fatalf("git.show %s in %s: %v", c.hash, c.cwd, err)
		}
		if strings.Contains(s.Diff, "SECRET") {
			t.Errorf("git.show %s in %s shows a guarded file:\n%s", c.hash, c.cwd, s.Diff)
		}
		if !strings.Contains(s.Diff, "notes.md") {
			t.Errorf("git.show %s in %s: notes.md's diff is missing:\n%s", c.hash, c.cwd, s.Diff)
		}
		if len(s.Files) == 0 {
			t.Errorf("git.show %s in %s lists no files", c.hash, c.cwd)
		}
	}
	s, err := n.gitShow(ctx, "~", first)
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, f := range s.Files {
		listed[f.Path] = true
	}
	for _, p := range append([]string{"notes.md", "code/repo/main.go"}, guarded...) {
		if !listed[p] {
			t.Errorf("git.show %s does not list %s: %+v", first, p, s.Files)
		}
	}
	if !strings.Contains(s.Diff, "+package main") {
		t.Errorf("git.show %s: a plain file's diff is missing:\n%s", first, s.Diff)
	}
	// A merge's combined diff, a stash, and renames diffed with diff.renames off leave them out too.
	runPullGit(t, home, "checkout", "-q", "-b", "side", first)
	write("notes.md", "side\n")
	write(".aws/credentials", "SECRET-SIDE\n")
	runPullGit(t, home, "commit", "-q", "-am", "Side edit")
	runPullGit(t, home, "checkout", "-q", "main")
	if _, err := git(ctx, home, "merge", "-q", "side"); err == nil {
		t.Fatal("the sample merge was meant to conflict")
	}
	write("notes.md", "merged\n")
	write(".aws/credentials", "SECRET-MERGED\n")
	runPullGit(t, home, "commit", "-q", "-am", "Merge side")
	write("notes.md", "stashed\n")
	write(".aws/credentials", "SECRET-STASHED\n")
	runPullGit(t, home, "stash", "-q")
	runPullGit(t, home, "config", "diff.renames", "false")
	for _, hash := range []string{"HEAD", "stash@{0}", "HEAD~1"} {
		s, err := n.gitShow(ctx, "~", hash)
		if err != nil {
			t.Fatalf("git.show %s: %v", hash, err)
		}
		if strings.Contains(s.Diff, "SECRET") || !strings.Contains(s.Diff, "notes.md") {
			t.Errorf("git.show %s:\n%s", hash, s.Diff)
		}
	}
	// Without a working tree to check the names against: the commit and its files, no diff.
	bare := filepath.Join(t.TempDir(), "dotfiles.git")
	runPullGit(t, home, "clone", "-q", "--bare", home, bare)
	for _, cwd := range []string{filepath.Join(home, ".git"), bare} {
		s, err := n.gitShow(ctx, cwd, first)
		if err != nil || s.Diff != "" || len(s.Files) == 0 || s.Commit.Hash != first {
			t.Errorf("git.show in %s: %+v %v", cwd, s, err)
		}
	}
	// A file's version by name is not a commit.
	if s, err := n.gitShow(ctx, "~", "HEAD:.aws/credentials"); err == nil && strings.Contains(s.Diff, "SECRET") {
		t.Errorf("git.show HEAD:.aws/credentials shows it: %q", s.Diff)
	}
}
