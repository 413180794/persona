package node

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// An alias's own --settings is found as the shell would pass it: either form, the last of several, ~ and $HOME
// left for the node to expand. One without, or a function, has none.
func TestAliasOwnSettings(t *testing.T) {
	defs := `alias work="claude --settings ~/.claude/settings.work.json --dangerously-skip-permissions"
alias eqs='claude --settings=$HOME/s.json'
alias two="claude --settings a.json --settings b.json --model m"
alias plain="claude --model m"
alias other="ls --settings x"
`
	_, _, settings := mergeLaunchers(asNode(t, defs, launcherScript))
	want := map[string]string{"work": "~/.claude/settings.work.json", "eqs": "$HOME/s.json", "two": "b.json"}
	if len(settings) != len(want) {
		t.Fatalf("settings %v, want %v", settings, want)
	}
	for k, v := range want {
		if settings[k] != v {
			t.Errorf("%s: %q, want %q", k, settings[k], v)
		}
	}
}

// withLauncherSettings is the launcher list's --settings for the test, put back after it.
func withLauncherSettings(t *testing.T, settings map[string]string) {
	t.Helper()
	launcherCache.Lock()
	old, oldAt := launcherCache.settings, launcherCache.at
	launcherCache.settings, launcherCache.at = settings, time.Now()
	launcherCache.Unlock()
	t.Cleanup(func() {
		launcherCache.Lock()
		launcherCache.settings, launcherCache.at = old, oldAt
		launcherCache.Unlock()
	})
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func hookCommands(t *testing.T, s map[string]any, event string) []string {
	t.Helper()
	hooks, _ := s["hooks"].(map[string]any)
	list, _ := hooks[event].([]any)
	var out []string
	for _, m := range list {
		for _, h := range m.(map[string]any)["hooks"].([]any) {
			out = append(out, h.(map[string]any)["command"].(string))
		}
	}
	return out
}

// A claude start of a launcher with its own settings passes one file that is both: the launcher's env (its token)
// and keys, and Persona's hooks after its own. Claude Code keeps only the last --settings: before, the launcher's
// was dropped and claude was "Not logged in".
func TestClaudeSettingsMergesTheLaunchers(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	n := &Node{home: home, dir: filepath.Join(home, ".jarvis"), logger: log.New(io.Discard, "", 0)}
	ours := filepath.Join(n.dir, "claude-settings.json")
	writeFile(t, ours, `{"hooks":{"PreToolUse":[{"matcher":"AskUserQuestion","hooks":[{"type":"command","command":"jarvisd hook"}]}],
		"Stop":[{"hooks":[{"type":"command","command":"jarvisd hook"}]}]}}`)
	writeFile(t, filepath.Join(home, ".claude/settings.work.json"), `{"env":{"ANTHROPIC_AUTH_TOKEN":"tok","ANTHROPIC_BASE_URL":"https://gw.example.com"},
		"model":"m1","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"their-hook"}]}]}}`)
	writeFile(t, filepath.Join(cwd, "rel.json"), `{"model":"rel"}`)
	withLauncherSettings(t, map[string]string{
		"work": "~/.claude/settings.work.json", "home": "$HOME/.claude/settings.work.json", "rel": "rel.json",
		"inline": `{"model":"inline"}`, "gone": "~/missing.json", "broken": "{not json",
	})
	ctx := context.Background()

	got := n.claudeSettings(ctx, "work", ours, cwd)
	if got != filepath.Join(n.dir, "launcher-settings", "work-claude-settings.json") {
		t.Fatalf("path %s", got)
	}
	s := readSettings(t, got)
	env, _ := s["env"].(map[string]any)
	if env["ANTHROPIC_AUTH_TOKEN"] != "tok" || env["ANTHROPIC_BASE_URL"] != "https://gw.example.com" || s["model"] != "m1" {
		t.Errorf("the launcher's settings are not all there: %v", s)
	}
	if c := hookCommands(t, s, "PreToolUse"); len(c) != 2 || c[0] != "their-hook" || c[1] != "jarvisd hook" {
		t.Errorf("PreToolUse %v, want its own then Persona's", c)
	}
	if c := hookCommands(t, s, "Stop"); len(c) != 1 || c[0] != "jarvisd hook" {
		t.Errorf("Stop %v", c)
	}
	if fi, err := os.Stat(got); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v (%v): it holds the launcher's token", fi.Mode(), err)
	}

	for launcher, model := range map[string]string{"home": "m1", "rel": "rel", "inline": "inline"} {
		if s := readSettings(t, n.claudeSettings(ctx, launcher, ours, cwd)); s["model"] != model || len(hookCommands(t, s, "Stop")) != 1 {
			t.Errorf("%s: %v", launcher, s)
		}
	}
	// Nothing to merge, or nothing that can be: Persona's file, as before.
	for _, launcher := range []string{"claude", "plain", "gone", "broken"} {
		if got := n.claudeSettings(ctx, launcher, ours, cwd); got != ours {
			t.Errorf("%s: %s, want %s", launcher, got, ours)
		}
	}
}
