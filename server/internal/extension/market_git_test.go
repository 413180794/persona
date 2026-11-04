package extension

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGitMarket(t *testing.T) {
	for in, want := range map[string]string{
		"git:git@git.example.com:team/plugins.git":          "git:git@git.example.com:team/plugins.git",
		"git@git.example.com:team/plugins.git":              "git:git@git.example.com:team/plugins.git",
		"git:ssh://git@git.example.com/team/plugins.git#v2": "git:ssh://git@git.example.com/team/plugins.git#v2",
		"git:https://git.example.com/team/plugins.git":      "git:https://git.example.com/team/plugins.git",
	} {
		s, err := ParseMarket(in)
		if err != nil || s.Kind != "git" || s.String() != want {
			t.Errorf("%q: %+v %v", in, s, err)
		}
	}
	for _, in := range []string{"git:/srv/plugins.git", "git:file:///srv/plugins.git", "git:ext::sh -c x", "git:git@git.example.com:team/plugins.git#--upload-pack=x",
		"git:http://git.example.com/p.git", "git:-oProxyCommand=x", "git:-o@git.example.com:x", "git:git@-host:x"} {
		if _, err := ParseMarket(in); err == nil {
			t.Errorf("%q: parsed", in)
		}
	}
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

// A git market: cloned once, fetched again on the next read, its packages read from the clone (never through a
// path or a link out of it, never .git), checked against the index's sha256.
func TestGitMarket(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	work, bare := t.TempDir(), filepath.Join(t.TempDir(), "plugins.git")
	run(t, work, "init", "-q", "-b", "main")
	pkg := []byte("a package")
	sum := sha256.Sum256(pkg)
	write := func(version string) {
		var m map[string]any
		_ = json.Unmarshal([]byte(exampleManifest), &m)
		m["version"] = version
		ix, _ := json.Marshal(map[string]any{"market_version": 1, "name": "Team plugins", "plugins": []any{
			map[string]any{"manifest": m, "packages": []any{map[string]any{"url": "packages/notes.tar.gz", "sha256": hex.EncodeToString(sum[:])}}}}})
		_ = os.MkdirAll(filepath.Join(work, "packages"), 0o755)
		_ = os.WriteFile(filepath.Join(work, "packages", "notes.tar.gz"), pkg, 0o644)
		_ = os.WriteFile(filepath.Join(work, IndexFile), ix, 0o644)
		_ = os.Symlink("/etc/hosts", filepath.Join(work, "packages", "out"))
		run(t, work, "add", "-A")
		run(t, work, "commit", "-q", "-m", version)
	}
	write("1.0.0")
	run(t, t.TempDir(), "clone", "-q", "--bare", work, bare)
	m := &Market{Spec: MarketSpec{Kind: "git", URL: bare}, Dir: t.TempDir(), allowLocal: true}
	ix, _, err := m.Fetch(context.Background())
	if err != nil || ix.Name != "Team plugins" || ix.Extensions[0].Parsed.Version != "1.0.0" {
		t.Fatalf("%+v %v", ix, err)
	}
	write("1.1.0")
	run(t, work, "push", "-q", bare, "main")
	if ix, _, err = m.Fetch(context.Background()); err != nil || ix.Extensions[0].Parsed.Version != "1.1.0" {
		t.Fatalf("fetched again: %+v %v", ix, err)
	}
	if _, _, err := m.Download(context.Background(), ix.Extensions[0].PackageFor("linux-amd64"), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../plugins.git/HEAD", ".git/config", "packages/out", "/etc/hosts"} {
		var b strings.Builder
		if err := m.gitFile(bad, &b, 1<<20); err == nil {
			t.Errorf("%s: read", bad)
		}
	}
	if (&Market{Spec: MarketSpec{Kind: "git", URL: bare}, Dir: t.TempDir(), Token: "x", allowLocal: true}).fetchErr() == nil {
		t.Fatal("a git market takes no token")
	}
	m.RemoveCheckout()
	if _, err := os.Stat(m.checkout()); err == nil {
		t.Fatal("the clone stays")
	}
}

func (m *Market) fetchErr() error { _, _, err := m.Fetch(context.Background()); return err }
