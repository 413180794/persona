package node

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// probe runs probeScript against testdata/launchers.zsh (zsh, as on the machines).
func probe(t *testing.T, name string) proto.LauncherDetail {
	t.Helper()
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("no zsh")
	}
	defs, err := os.ReadFile("testdata/launchers.zsh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(context.Background(), zsh, "-fc", string(defs)+"\n"+probeScript(name))
	cmd.Env = append(os.Environ(), "ANTHROPIC_MODEL=from-the-shell", "USER=tester")
	out, _ := cmd.Output()
	p, ok := parseProbe(string(out))
	if !ok {
		t.Fatalf("%s: no probe output:\n%s", name, out)
	}
	return LauncherDetail(p)
}

func models(d proto.LauncherDetail) map[string]string {
	m := map[string]string{}
	for _, x := range d.Models {
		m[x.Role] = x.Model
	}
	return m
}

func env(d proto.LauncherDetail, key string) (string, bool) {
	for _, e := range d.Env {
		if e.Key == key {
			return e.Value, true
		}
	}
	return "", false
}

func TestProbeHelperWithWindow(t *testing.T) {
	d := probe(t, "cc_proxy")
	if d.Agent != "claude" || d.BaseURL != "https://gw.example.com" || d.Auth != "ANTHROPIC_AUTH_TOKEN" {
		t.Fatalf("%+v", d)
	}
	m := models(d)
	for _, role := range []string{"主模型", "Opus", "Sonnet", "Haiku", "小模型"} {
		if m[role] != "vendor/model-a" {
			t.Errorf("%s = %q", role, m[role])
		}
	}
	if d.ContextWindow != 700000 || d.CompactWindow != 0 || d.Effort != "max" || d.TimeoutMS != 3000000 {
		t.Errorf("window %d compact %d effort %q timeout %d", d.ContextWindow, d.CompactWindow, d.Effort, d.TimeoutMS)
	}
	if !slices.Equal(d.Args, []string{"--teammate-mode", "in-process", "--dangerously-skip-permissions"}) {
		t.Errorf("args %v", d.Args)
	}
	if v, ok := env(d, "CLAUDE_CODE_FORK_SUBAGENT"); !ok || v != "1" {
		t.Errorf("other settings are listed: %v", d.Env)
	}
	if v, _ := env(d, "ANTHROPIC_AUTH_TOKEN"); v != "" {
		t.Errorf("the token's variable is the auth, not a setting shown with its value: %q", v)
	}
	assertNoSecret(t, d, "sk-test-0123456789abcdef")
}

func TestProbeSmallModelAndWindow(t *testing.T) {
	d := probe(t, "cc_proxy2")
	m := models(d)
	if m["主模型"] != "model-b[1m]" || m["小模型"] != "model-b-flash" || m["Haiku"] != "model-b-flash" || m["子 Agent"] != "model-b-flash" {
		t.Errorf("models %v", m)
	}
	if d.ContextWindow != 786432 || d.BaseURL != "https://gw.example.com/v1" {
		t.Errorf("%+v", d)
	}
	assertNoSecret(t, d, "test-0123456789abcdef0123")
}

func TestProbeVariablesAroundAHelper(t *testing.T) {
	d := probe(t, "cc_headers")
	if m := models(d); m["主模型"] != "model-c" || m["子 Agent"] != "model-c" {
		t.Errorf("models %v", m)
	}
	if d.ContextWindow != 1000000 {
		t.Errorf("window %d", d.ContextWindow)
	}
	if v, _ := env(d, "ANTHROPIC_CUSTOM_HEADERS"); v != "x-source: dogfood" {
		t.Errorf("custom headers %q (%v)", v, d.Env)
	}
}

func TestProbeExportsInASubshell(t *testing.T) {
	d := probe(t, "cc_exports")
	if d.Agent != "claude" || d.BaseURL != "http://127.0.0.1:18000" || d.Auth != "ANTHROPIC_API_KEY" || d.Effort != "high" {
		t.Fatalf("%+v", d)
	}
	m := models(d)
	if m["主模型"] != "vendor/model-d" || m["Fable"] != "vendor/model-d" || m["Haiku"] != "vendor/model-d-mini" ||
		m["小模型"] != "vendor/model-d-mini" || m["子 Agent"] != "vendor/model-d" {
		t.Errorf("models %v", m)
	}
	if d.ContextWindow != 0 {
		t.Errorf("no window set: %d", d.ContextWindow)
	}
	assertNoSecret(t, d, "fake-key-from-file")
}

func TestProbeCodex(t *testing.T) {
	d := probe(t, "cx")
	if d.Agent != "codex" || d.BaseURL != "https://gw.example.com/openai" || d.Auth != "OPENAI_API_KEY" {
		t.Fatalf("%+v", d)
	}
	if m := models(d); m["主模型"] != "gpt-x" {
		t.Errorf("codex's -m is its model: %v", m)
	}
	assertNoSecret(t, d, "sk-fake-codex-0123456789")
}

func TestProbeDSH(t *testing.T) {
	// A launcher running DSH is DSH's, not a Claude one tried as claude (the dsh it runs is stubbed, not started).
	if d := probe(t, "ds"); d.Agent != "dsh" {
		t.Fatalf("%+v", d)
	}
}

func TestProbeOfANonLauncher(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("no zsh")
	}
	out, _ := exec.Command(zsh, "-fc", "not_a_launcher() { echo hi }\n"+probeScript("not_a_launcher")).Output()
	if _, ok := parseProbe(string(out)); ok {
		t.Fatal("a function that starts no agent has no probe")
	}
}

func assertNoSecret(t *testing.T, d proto.LauncherDetail, secret string) {
	t.Helper()
	for _, e := range d.Env {
		if strings.Contains(e.Value, secret) {
			t.Errorf("secret shown in %s", e.Key)
		}
	}
}

func TestRedactSecretsInHelperCalls(t *testing.T) {
	api := "sk-test-" + strings.Repeat("a1", 16)
	bearer := "test-bearer-" + strings.Repeat("b2", 16)
	vendor := "plat_" + strings.Repeat("c3", 24)
	got := redactSecrets("_claude_proxy https://gw.example.com " + api + ` "model-alpha" 786432 "on"
_claude_proxy https://gw.example.com "Bearer ` + bearer + `" model-beta "on"
_claude_proxy https://gw.example.com ` + vendor + ` model-gamma 700000 "on"
unset CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR ANTHROPIC_AUTH_TOKEN`)
	for _, secret := range []string{api, bearer, vendor, api[:12], bearer[len(bearer)-12:], vendor[:12]} {
		if strings.Contains(got, secret) {
			t.Errorf("a synthetic secret or excerpt remains in the redacted helper output")
		}
	}
	for _, kept := range []string{"https://gw.example.com", "model-alpha", "model-beta", "model-gamma", "786432", "700000", "CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR", "ANTHROPIC_AUTH_TOKEN"} {
		if !strings.Contains(got, kept) {
			t.Errorf("a non-secret helper option or environment-variable name was removed: %s", kept)
		}
	}
}

func TestClaudeResultOfACheck(t *testing.T) {
	ok := "banner from zshrc\n" + `{"type":"result","subtype":"success","is_error":false,"duration_ms":2310,"result":"ok",` +
		`"modelUsage":{"vendor/model-a":{"inputTokens":12},"m":{"inputTokens":3}}}`
	r, found := claudeResult(ok)
	if !found || r.isError || r.result != "ok" || r.millis != 2310 || r.model != "vendor/model-a" {
		t.Fatalf("%+v %v", r, found)
	}
	bad := `{"type":"result","subtype":"success","is_error":true,"result":"API Error: 401 invalid API key","duration_ms":800}`
	if r, found := claudeResult(bad); !found || !r.isError || !strings.Contains(r.result, "401") {
		t.Fatalf("%+v %v", r, found)
	}
	if _, found := claudeResult("zsh: command not found: cc_nope"); found {
		t.Fatal("not a result")
	}
}

// A launcher whose gateway and key are in the env of its --settings file (Claude Code reads it after it starts):
// the probe must see them, or quota and the launcher's page find no gateway (#1378).
func TestProbeReadsTheSettingsEnv(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(home+"/.claude", 0o700); err != nil {
		t.Fatal(err)
	}
	settings := `{"env": {"ANTHROPIC_BASE_URL": "https://gw.example.com/v1", "ANTHROPIC_AUTH_TOKEN": "sk-test",
		"ANTHROPIC_MODEL": "from-settings", "CLAUDE_CODE_MAX_CONTEXT_TOKENS": 200000}}`
	if err := os.WriteFile(home+"/.claude/settings.work.json", []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	claude := func(args ...string) Probe {
		return Probe{Agent: "claude", Args: args, Env: map[string]string{"ANTHROPIC_MODEL": "from-shell", "CLAUDE_CODE_EFFORT_LEVEL": "high"}}
	}

	for _, args := range [][]string{
		{"--settings", "~/.claude/settings.work.json", "--dangerously-skip-permissions"},
		{"--settings=" + home + "/.claude/settings.work.json"},
		{"--settings", "$HOME/.claude/settings.work.json"},
		{"--settings", ".claude/settings.work.json"},                                 // relative: from home
		{"--settings", "missing.json", "--settings", "~/.claude/settings.work.json"}, // the last one counts
	} {
		p := withSettingsEnv(claude(args...), home)
		if p.Env["ANTHROPIC_BASE_URL"] != "https://gw.example.com/v1" || p.Env["ANTHROPIC_AUTH_TOKEN"] != "sk-test" {
			t.Errorf("%v: gateway and key not read: %v", args, p.Env)
		}
		if p.Env["ANTHROPIC_MODEL"] != "from-settings" {
			t.Errorf("%v: settings env must win over the shell's: %q", args, p.Env["ANTHROPIC_MODEL"])
		}
		if p.Env["CLAUDE_CODE_EFFORT_LEVEL"] != "high" || p.Env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] != "200000" {
			t.Errorf("%v: env %v", args, p.Env)
		}
		if d := LauncherDetail(p); d.BaseURL != "https://gw.example.com/v1" || d.Auth != "ANTHROPIC_AUTH_TOKEN" || d.ContextWindow != 200000 {
			t.Errorf("%v: detail %+v", args, d)
		}
	}

	inline := withSettingsEnv(claude("--settings", `{"env": {"ANTHROPIC_BASE_URL": "https://inline.example.com"}}`), home)
	if inline.Env["ANTHROPIC_BASE_URL"] != "https://inline.example.com" {
		t.Errorf("inline settings: %v", inline.Env)
	}

	// Left as they were: no --settings, a file that cannot be read or is not JSON, settings without env, not claude.
	if err := os.WriteFile(home+"/bad.json", []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(home+"/noenv.json", []byte(`{"model": "x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []Probe{
		claude("--dangerously-skip-permissions"),
		claude("--settings", "~/nope.json"),
		claude("--settings", "~/bad.json"),
		claude("--settings", "~/noenv.json"),
		claude("--settings"),
		{Agent: "codex", Args: []string{"--settings", "~/.claude/settings.work.json"}, Env: map[string]string{"ANTHROPIC_MODEL": "from-shell"}},
	} {
		got := withSettingsEnv(p, home)
		if _, ok := got.Env["ANTHROPIC_BASE_URL"]; ok || got.Env["ANTHROPIC_MODEL"] != "from-shell" {
			t.Errorf("%s %v: changed: %v", p.Agent, p.Args, got.Env)
		}
	}
}
