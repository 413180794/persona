package node

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// asBash runs script the way the node does where the login shell is bash (`bash -ic`), with defs as the ~/.bashrc,
// in asNode's minimal environment (without fixedBins).
func asBash(t *testing.T, defs, script string) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".bashrc"), []byte(defs), 0o600); err != nil {
		t.Fatal(err)
	}
	script = strings.ReplaceAll(script, fixedBins, "")
	cmd := exec.CommandContext(context.Background(), bash, "-ic", script)
	cmd.Env = []string{"HOME=" + dir, "PATH=/usr/bin:/bin", "USER=tester", "TERM=dumb"}
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		t.Fatalf("bash: %v", err)
	}
	return string(out)
}

// A machine whose login shell is bash lists the launchers in its ~/.bashrc: the same aliases as in a ~/.zshrc give
// the same launchers, with the same gateways and models.
func TestBashrcAliasLaunchers(t *testing.T) {
	list, _, _, _ := mergeLaunchers(asBash(t, fixture(t, "aliases.zsh"), launcherScript))
	var names []string
	for _, l := range list {
		names = append(names, l.Name)
	}
	want := []string{"dmcc", "envcc", "expcc", "kcc", "mycc", "ncc", "pathcc", "spaced", "ncodex"}
	if !slices.Equal(names, want) {
		t.Fatalf("launchers %v, want %v", names, want)
	}
	m := byName(list)
	for name, w := range map[string][2]string{
		"kcc":    {"https://gw-a.example.com/anthropic", "model-a[1m]"},
		"spaced": {"https://gw-c.example.com", "model with space"},
		"envcc":  {"https://gw-d.example.com", ""},
		"expcc":  {"https://gw-f.example.com", "eq-model"},
	} {
		if l := m[name]; l.BaseURL != w[0] || l.Model != w[1] {
			t.Errorf("%s: base %q model %q, want %q %q", name, l.BaseURL, l.Model, w[0], w[1])
		}
	}
	if m["ncodex"].Agent != "codex" {
		t.Errorf("ncodex: %+v", m["ncodex"])
	}
}

// No alias that starts claude in ~/.bashrc: plain claude, as with zsh.
func TestBashrcWithoutLaunchers(t *testing.T) {
	list, _, _, _ := mergeLaunchers(asBash(t, `alias ll="ls -l"`, launcherScript))
	if len(list) != 1 || list[0].Name != "claude" {
		t.Fatalf("got %v", list)
	}
}

func TestShellWords(t *testing.T) {
	for in, want := range map[string][]string{
		`a b  c`:                       {"a", "b", "c"},
		`'x y' "p \"q\" \$r" z\ w`:     {"x y", `p "q" $r`, "z w"},
		`'it'\''s'`:                    {"it's"},
		`export A=1; claude --model=m`: {"export", "A=1", ";", "claude", "--model=m"},
		`cd /x&&claude|tee f &`:        {"cd", "/x", "&&", "claude", "|", "tee", "f", "&"},
		`''`:                           {""},
	} {
		if got := shellWords(in); !slices.Equal(got, want) {
			t.Errorf("shellWords(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBashAliases(t *testing.T) {
	got := bashAliases("alias kcc='ANTHROPIC_MODEL=\"m 1\" claude'\nalias -- odd='claude'\nnot an alias\n")
	want := "kcc\x1fANTHROPIC_MODEL=m 1\x1fclaude\x1f\x1eodd\x1fclaude\x1f\x1e"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPasswdShell(t *testing.T) {
	passwd := "root:x:0:0:root:/root:/bin/bash\nme:x:501:20::/Users/me:/bin/zsh\n# comment\n"
	if s := passwdShell(passwd, 0); s != "bash" {
		t.Errorf("uid 0: %q", s)
	}
	if s := passwdShell(passwd, 501); s != "zsh" {
		t.Errorf("uid 501: %q", s)
	}
	if s := passwdShell(passwd, 7); s != "" {
		t.Errorf("uid 7: %q", s)
	}
}
