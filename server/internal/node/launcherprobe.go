package node

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// What a ~/.zshrc launcher really sets, for its page in 设置 › 模型: reading the function's text misses everything a
// helper (`_claude_proxy URL TOKEN MODEL WINDOW`) or a local variable does, so the node runs the launcher in a
// subshell with `claude`, `codex`, `traex`, `command` and `env` stubbed out: instead of starting anything they print
// the environment and arguments the agent would have got. probeScript is that harness; parseProbe reads its output.

const probeMarker = "__JARVIS_LAUNCHER_PROBE__"

// probeScript runs launcher `name` with the agents stubbed. Output: "<marker> base" and the shell's environment before
// (NUL-separated KEY=VALUE on one line), "<marker> agent <name>", "<marker> arg <arg>" lines, then "<marker> env" and
// the environment the agent got. Only what the launcher changed counts: the node's own environment (a claude it runs
// in, a gateway exported in ~/.zshrc for everything) is not the launcher's.
func probeScript(name string) string {
	return fmt.Sprintf(`
__jp_report() {
  print -r -- "%[1]s agent $1"; shift
  local a; for a in "$@"; do print -r -- "%[1]s arg $a"; done
  print -rn -- "%[1]s env "; print -rn -N -- ${(f)"$(/usr/bin/env)"}; print
  exit 0
}
claude() { __jp_report claude "$@" }
codex() { __jp_report codex "$@" }
traex() { __jp_report traex "$@" }
command() { case "$1" in claude|codex|traex) local w=$1; shift; __jp_report $w "$@" ;; *) builtin command "$@" ;; esac }
env() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -u) unset "$2"; shift 2 ;;
      -i|--) shift ;;
      *=*) export "$1"; shift ;;
      *) break ;;
    esac
  done
  "$@"
}
print -rn -- "%[1]s base "; print -rn -N -- ${(f)"$(/usr/bin/env)"}; print
( %[2]s ) </dev/null
`, probeMarker, name)
}

// Probe is what a launcher would run: the agent, its arguments, the environment it sets.
type Probe struct {
	Agent string
	Args  []string
	Env   map[string]string
}

func parseProbe(out string) (Probe, bool) {
	p := Probe{Env: map[string]string{}}
	base := map[string]string{}
	found := false
	pairs := func(s string, into map[string]string) {
		for _, kv := range strings.Split(s, "\x00") {
			if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
				into[k] = v
			}
		}
	}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		line := sc.Text()
		rest, ok := strings.CutPrefix(line, probeMarker+" ")
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(rest, "agent "):
			p.Agent, found = strings.TrimPrefix(rest, "agent "), true
		case strings.HasPrefix(rest, "arg "):
			p.Args = append(p.Args, strings.TrimPrefix(rest, "arg "))
		case strings.HasPrefix(rest, "base "):
			pairs(strings.TrimPrefix(rest, "base "), base)
		case strings.HasPrefix(rest, "env "):
			pairs(strings.TrimPrefix(rest, "env "), p.Env)
		}
	}
	for k, v := range p.Env {
		if old, ok := base[k]; ok && old == v {
			delete(p.Env, k) // the shell had it already
		}
	}
	return p, found
}

var probeCache = struct {
	sync.Mutex
	at  map[string]time.Time
	got map[string]proto.LauncherDetail
}{at: map[string]time.Time{}, got: map[string]proto.LauncherDetail{}}

// launcherProbeCached probes at most every 10 minutes per launcher: running one may run what it runs first (a script
// that fetches a token), so opening its page again should not repeat that.
func (n *Node) launcherProbeCached(ctx context.Context, name string) proto.LauncherDetail {
	probeCache.Lock()
	if at, ok := probeCache.at[name]; ok && time.Since(at) < 10*time.Minute {
		d := probeCache.got[name]
		probeCache.Unlock()
		return d
	}
	probeCache.Unlock()
	var d proto.LauncherDetail
	if p, err := n.launcherProbe(ctx, name); err == nil {
		d = LauncherDetail(p)
	} else {
		d = proto.LauncherDetail{Error: err.Error()}
	}
	probeCache.Lock()
	probeCache.at[name], probeCache.got[name] = time.Now(), d
	probeCache.Unlock()
	return d
}

// launcherProbe runs the probe on this machine (a login shell, so ~/.zshrc defines the launcher).
func (n *Node) launcherProbe(ctx context.Context, name string) (Probe, error) {
	if !nameRe.MatchString(name) {
		return Probe{}, fmt.Errorf("invalid launcher %q", name)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := loginShell(ctx, probeScript(name))
	killGroupOnCancel(cmd)
	out, _ := cmd.Output()
	p, ok := parseProbe(string(out))
	if !ok {
		return Probe{}, fmt.Errorf("%s did not start an agent when tried", name)
	}
	return p, nil
}

// --- what the probe means ---

// secretEnv: variables whose values are never shown.
var secretEnv = regexp.MustCompile(`(?i)(KEY|TOKEN|SECRET|PASSWORD|AUTH|COOKIE|CREDENTIAL)`)

// launcherEnvKeys: the variables that configure the agent (the rest of a shell's environment is noise).
var launcherEnvKeys = regexp.MustCompile(`^(ANTHROPIC_|CLAUDE_|CLAUDE_CODE_|API_TIMEOUT_MS$|DISABLE_|MAX_THINKING_TOKENS$|MAX_MCP_OUTPUT_TOKENS$|BASH_|MCP_|OPENAI_|CODEX_|HTTPS?_PROXY$|https?_proxy$|NO_PROXY$|no_proxy$)`)

// LauncherDetail turns a probe into the page's fields: agent, gateway, how it authenticates, every model tier, the
// context window, effort, flags, and the other settings — secrets masked.
func LauncherDetail(p Probe) proto.LauncherDetail {
	d := proto.LauncherDetail{Agent: p.Agent, Args: p.Args}
	get := func(k string) string { return p.Env[k] }
	d.BaseURL = firstNonEmpty(get("ANTHROPIC_BASE_URL"), get("OPENAI_BASE_URL"))
	switch {
	case get("ANTHROPIC_AUTH_TOKEN") != "":
		d.Auth = "ANTHROPIC_AUTH_TOKEN"
	case get("ANTHROPIC_API_KEY") != "":
		d.Auth = "ANTHROPIC_API_KEY"
	case get("OPENAI_API_KEY") != "":
		d.Auth = "OPENAI_API_KEY"
	case get("CLAUDE_CODE_OAUTH_TOKEN") != "":
		d.Auth = "CLAUDE_CODE_OAUTH_TOKEN"
	}
	for _, m := range []struct{ label, key string }{
		{"主模型", "ANTHROPIC_MODEL"}, {"Fable", "ANTHROPIC_DEFAULT_FABLE_MODEL"}, {"Opus", "ANTHROPIC_DEFAULT_OPUS_MODEL"},
		{"Sonnet", "ANTHROPIC_DEFAULT_SONNET_MODEL"}, {"Haiku", "ANTHROPIC_DEFAULT_HAIKU_MODEL"},
		{"小模型", "ANTHROPIC_SMALL_FAST_MODEL"}, {"子 Agent", "CLAUDE_CODE_SUBAGENT_MODEL"},
	} {
		if v := get(m.key); v != "" {
			d.Models = append(d.Models, proto.LauncherModel{Role: m.label, Env: m.key, Model: v})
		}
	}
	if d.Agent != "claude" {
		if i := indexOf(p.Args, "-m", "--model"); i >= 0 && i+1 < len(p.Args) {
			d.Models = append(d.Models, proto.LauncherModel{Role: "主模型", Env: "-m", Model: p.Args[i+1]})
		}
	}
	if w := firstNonEmpty(get("CLAUDE_CODE_MAX_CONTEXT_TOKENS"), get("CLAUDE_CODE_AUTO_COMPACT_WINDOW")); w != "" {
		d.ContextWindow, _ = strconv.Atoi(w)
	}
	if a := get("CLAUDE_CODE_AUTO_COMPACT_WINDOW"); a != "" && a != get("CLAUDE_CODE_MAX_CONTEXT_TOKENS") {
		d.CompactWindow, _ = strconv.Atoi(a)
	}
	d.Effort = get("CLAUDE_CODE_EFFORT_LEVEL")
	if t := get("API_TIMEOUT_MS"); t != "" {
		d.TimeoutMS, _ = strconv.Atoi(t)
	}
	shown := map[string]bool{"ANTHROPIC_BASE_URL": true, "OPENAI_BASE_URL": true, "CLAUDE_CODE_MAX_CONTEXT_TOKENS": true,
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW": true, "CLAUDE_CODE_EFFORT_LEVEL": true, "API_TIMEOUT_MS": true, d.Auth: true}
	for _, m := range d.Models {
		shown[m.Env] = true
	}
	var keys []string
	for k := range p.Env {
		if launcherEnvKeys.MatchString(k) && !shown[k] && p.Env[k] != "" { // set empty: cleared, nothing to show
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := p.Env[k]
		if secretEnv.MatchString(k) && v != "" {
			v = "•••"
		}
		d.Env = append(d.Env, proto.LauncherEnv{Key: k, Value: v})
	}
	return d
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func indexOf(list []string, want ...string) int {
	for i, s := range list {
		for _, w := range want {
			if s == w {
				return i
			}
		}
	}
	return -1
}

// killGroupOnCancel: on timeout kill everything the shell started (a token script, a hung network call) and stop
// waiting for its output soon after; killing only the shell left Output() waiting on the children's open pipe.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second
}
