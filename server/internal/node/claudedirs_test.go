package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestParsePSEnv(t *testing.T) {
	out := parsePSEnv("" +
		"  101 /home/me/.local/bin/claude --settings /p/a/settings.json TERM=xterm CLAUDE_CONFIG_DIR=/p/a SHELL=/bin/zsh\n" +
		"  102 claude --resume x HOME=/home/me\n" + // the default directory: left out
		"  103 node /usr/lib/node_modules/@anthropic-ai/claude-code/cli.js CLAUDE_CONFIG_DIR=/p/b\n" +
		"  104 /bin/zsh -c run CLAUDE_CONFIG_DIR=/p/c\n" + // a shell a claude started: not claude
		"garbage\n")
	if len(out) != 2 || out[101] != "/p/a" || out[103] != "/p/b" {
		t.Fatalf("got %v", out)
	}
}

// claudeHome lays out a home with the default ~/.claude and a profile directory, each with a transcript.
func claudeHome(t *testing.T) (*Node, string) {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := t.TempDir()
	profile := filepath.Join(home, ".config", "cc", "work")
	for dir, id := range map[string]string{filepath.Join(home, ".claude"): "aaaaaaaa-0000-0000-0000-000000000001", profile: "bbbbbbbb-0000-0000-0000-000000000002"} {
		p := filepath.Join(dir, "projects", "-src-app")
		_ = os.MkdirAll(p, 0o700)
		_ = os.WriteFile(filepath.Join(p, id+".jsonl"), []byte("{}\n"), 0o600)
		_ = os.MkdirAll(filepath.Join(dir, "sessions"), 0o700)
	}
	n := &Node{home: home, paths: map[string]string{}, cdirs: newClaudeDirs()}
	n.cdirs.scan = func() map[int]string { return map[int]string{4242: profile} }
	return n, profile
}

func TestTranscriptsInProfileDirs(t *testing.T) {
	n, profile := claudeHome(t)
	var reported []string
	n.cdirs.report = func(d []string) { reported = d }
	if p := n.transcriptPath("bbbbbbbb-0000-0000-0000-000000000002"); p != filepath.Join(profile, "projects", "-src-app", "bbbbbbbb-0000-0000-0000-000000000002.jsonl") {
		t.Fatalf("profile transcript at %q", p)
	}
	if p := n.transcriptPath("aaaaaaaa-0000-0000-0000-000000000001"); p == "" {
		t.Fatal("default transcript not found")
	}
	if len(n.globProjects("*", "*.jsonl")) != 2 {
		t.Fatalf("offline scan sees %v", n.globProjects("*", "*.jsonl"))
	}
	if !slices.Equal(reported, []string{profile}) {
		t.Fatalf("reported to the hub: %v", reported)
	}
	// The pid's own directory decides where its sessions file is.
	if f := n.claudeSessionFile(4242); f != filepath.Join(profile, "sessions", "4242.json") {
		t.Fatalf("sessions file %q", f)
	}
}

func TestKnownDirsWithoutRunningClaude(t *testing.T) {
	n, profile := claudeHome(t)
	n.cdirs.scan = func() map[int]string { return nil }
	if n.transcriptPath("bbbbbbbb-0000-0000-0000-000000000002") != "" {
		t.Fatal("an unknown profile cannot be found")
	}
	n.paths = map[string]string{}
	n.setKnownClaudeDirs([]string{"~/" + mustRel(t, n.home, profile)})
	if n.transcriptPath("bbbbbbbb-0000-0000-0000-000000000002") == "" {
		t.Fatal("a directory from the hub (config or remembered) is searched")
	}
}

func TestSymlinkedProjectsCountOnce(t *testing.T) {
	n, profile := claudeHome(t)
	// The old workaround: ~/.claude/projects is a symlink to the profile's.
	def := filepath.Join(n.home, ".claude", "projects")
	_ = os.RemoveAll(def)
	if err := os.Symlink(filepath.Join(profile, "projects"), def); err != nil {
		t.Fatal(err)
	}
	if got := n.projectDirs(); len(got) != 1 {
		t.Fatalf("projects dirs %v", got)
	}
	if got := n.globProjects("*", "*.jsonl"); len(got) != 1 {
		t.Fatalf("one transcript listed twice: %v", got)
	}
}

func TestSettingsWrittenToEveryDir(t *testing.T) {
	n, profile := claudeHome(t)
	_ = os.WriteFile(filepath.Join(profile, "settings.json"), []byte(`{"model":"opus"}`), 0o600)
	if err := n.setClaudeSetting("language", "中文"); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(n.home, ".claude"), profile} {
		var raw map[string]any
		b, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
		_ = json.Unmarshal(b, &raw)
		if raw["language"] != "中文" {
			t.Fatalf("%s: %v", dir, raw)
		}
	}
	// The page shows the directory the running claudes use.
	if p := n.claudeSettingsPath(); p != filepath.Join(profile, "settings.json") {
		real, _ := filepath.EvalSymlinks(filepath.Join(profile, "settings.json"))
		if p != real {
			t.Fatalf("settings shown from %q", p)
		}
	}
}

func mustRel(t *testing.T, base, p string) string {
	t.Helper()
	r, err := filepath.Rel(base, p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
