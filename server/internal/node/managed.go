package node

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"github.com/nwverse/persona/server/internal/proto"
)

// Launchers kept by the hub (设置 › 模型, docs/launchers.md): the hub sends them (keys included) on connect and on
// every change; they live only in this process's memory. A managed launcher runs as `jarvisd launch <name> …`, which
// asks this node over the local socket for its settings and execs claude with them: the key is never written to a
// file, typed into a terminal or put on a command line.

type managedLaunchers struct {
	mu   sync.Mutex
	byID map[string]proto.ManagedLauncher
}

func (n *Node) setManaged(list []proto.ManagedLauncher) {
	m := map[string]proto.ManagedLauncher{}
	for _, l := range list {
		if l.Kind == "api" {
			continue
		}
		m[l.Name] = l
	}
	n.managed.mu.Lock()
	n.managed.byID = m
	n.managed.mu.Unlock()
}

func (n *Node) managedLauncher(name string) (proto.ManagedLauncher, bool) {
	n.managed.mu.Lock()
	defer n.managed.mu.Unlock()
	l, ok := n.managed.byID[name]
	return l, ok
}

// launcherCommand is what starts claude for a launcher name in a shell: the name itself (a ~/.zshrc function, or
// claude), or `jarvisd launch <name>` for a managed one. Plain claude gets the machine's claude_start_dir as its
// CLAUDE_CONFIG_DIR when one is set: a computer whose claude is logged in under a profile directory, not ~/.claude,
// would otherwise answer every agent with 「Not logged in」 (#1459).
func (n *Node) launcherCommand(name string) string { return n.launcherCommandQ(name, shellQuote) }

// launcherCommandQ is launcherCommand quoting with q (herdrresume.go quotes without apostrophes).
func (n *Node) launcherCommandQ(name string, q func(string) string) string {
	if _, ok := n.managedLauncher(name); ok {
		return q(n.bin) + " launch " + name
	}
	if name == "claude" {
		if d := n.claudeStartDir.Load(); d != nil && *d != "" {
			return "CLAUDE_CONFIG_DIR=" + shellDirQ(*d, q) + " " + name
		}
	}
	return name
}

// shellDir quotes a directory for a shell command, keeping a leading ~/ as "$HOME"/ so it is the home of the shell that
// runs it.
func shellDir(p string) string { return shellDirQ(p, shellQuote) }

func shellDirQ(p string, q func(string) string) string {
	if p == "~" {
		return `"$HOME"`
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return `"$HOME"/` + q(rest)
	}
	return q(p)
}

// managedEnv is the environment a managed launcher sets: the gateway, its key, the model for every tier, the
// subagent / custom model entries when it has them, and Artifacts off (artifactsOff). The key goes in
// ANTHROPIC_AUTH_TOKEN (sent as Bearer) for a gateway that takes only that, else in ANTHROPIC_API_KEY (x-api-key), and
// the other one is set empty. Claude Code fills a variable the launcher does not set from the env of
// ~/.claude/settings.json, so a token kept there for another gateway would otherwise go out as a second credential
// next to this key, and a gateway that sees two (relay-history) answers 401. An empty one stops that. Without a key (none
// stored, or the hub could not open it) neither is set, as before: Claude uses whatever settings or sign-in it has
// rather than nothing.
func managedEnv(l proto.ManagedLauncher) []string {
	small := l.SmallModel
	if small == "" {
		small = l.Model
	}
	env := []string{"ANTHROPIC_BASE_URL=" + l.BaseURL}
	switch {
	case l.APIKey == "":
	case l.BearerAuth():
		env = append(env, "ANTHROPIC_AUTH_TOKEN="+l.APIKey, "ANTHROPIC_API_KEY=")
	default:
		env = append(env, "ANTHROPIC_API_KEY="+l.APIKey, "ANTHROPIC_AUTH_TOKEN=")
	}
	if l.Model != "" {
		for _, k := range []string{"ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_FABLE_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL",
			"ANTHROPIC_DEFAULT_SONNET_MODEL"} {
			env = append(env, k+"="+l.Model)
		}
	}
	// Subagents follow the model unless the launcher says otherwise (workflows, multi-agent runs).
	if subagent := cmp.Or(l.SubagentModel, l.Model); subagent != "" {
		env = append(env, "CLAUDE_CODE_SUBAGENT_MODEL="+subagent)
	}
	if l.CustomModel != "" {
		env = append(env, "ANTHROPIC_CUSTOM_MODEL_OPTION="+l.CustomModel)
		if l.CustomModelName != "" {
			env = append(env, "ANTHROPIC_CUSTOM_MODEL_OPTION_NAME="+l.CustomModelName)
		}
		if l.CustomModelDescription != "" {
			env = append(env, "ANTHROPIC_CUSTOM_MODEL_OPTION_DESCRIPTION="+l.CustomModelDescription)
		}
	}
	if small != "" {
		env = append(env, "ANTHROPIC_DEFAULT_HAIKU_MODEL="+small, "ANTHROPIC_SMALL_FAST_MODEL="+small)
	}
	return append(env, artifactsOff)
}

// artifactsOff turns Claude Code's Artifact tool off for a claude a managed launcher runs. Artifacts publish to
// claude.ai, which a gateway with an API key is not. Claude Code turns the tool on by default (since 2.1.26x), and an
// interactive session then sends the Artifact skills in its system message; a gateway that cannot carry them refuses
// the first request ("400 request is not representable on this edge: Claude Code Artifacts are enabled"), so every
// Agent started in herdr through it failed on its first turn while `claude -p` went on working. The environment
// variable reaches `jarvisd launch` (headless runs) and the settings env alike; the settings file also says
// enableArtifact: false (mergeManagedSettings): off in --settings wins over the other settings sources.
const artifactsOff = "CLAUDE_CODE_DISABLE_ARTIFACT=1"

// managedSettingsEnv is managedEnv as a settings "env" map. Settings env wins over the process env, so the user's
// ~/.claude/settings.json (another gateway, another token) would otherwise replace a managed launcher's.
func managedSettingsEnv(l proto.ManagedLauncher) map[string]string {
	out := map[string]string{}
	for _, kv := range managedEnv(l) {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[k] = v
		}
	}
	return out
}

// launchEnvRemoved: what the shell may carry from another launcher or account, dropped before a managed one runs.
var launchEnvRemoved = []string{"ANTHROPIC_", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
	"CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_SUBAGENT_MODEL"}

// claudeExecutable finds claude: PATH first, then the usual install spots. A node started by launchd (macOS) has a
// minimal PATH that misses /opt/homebrew/bin, and nothing in `jarvisd launch`'s own path adds it.
func claudeExecutable() (string, error) {
	if p, err := exec.LookPath("claude"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(home, ".local", "bin", "claude"), "/opt/homebrew/bin/claude",
		"/usr/local/bin/claude", filepath.Join(home, ".claude", "local", "claude")} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("claude not found in PATH or the usual install locations")
}

// Launch is `jarvisd launch <name> [claude args…]`: the managed launcher's settings from the local node, then claude.
func Launch(ctx context.Context, name string, args []string) error {
	line, err := DialSocket(ctx, sockRequest{Kind: "launcher.env", Name: name})
	if err != nil {
		return fmt.Errorf("jarvisd launch %s: %w", name, err)
	}
	var resp struct {
		Env   []string `json:"env"`
		Args  []string `json:"args"`
		Error string   `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New("jarvisd launch " + name + ": " + resp.Error)
	}
	claude, err := claudeExecutable()
	if err != nil {
		return fmt.Errorf("jarvisd launch: %w", err)
	}
	var env []string
	for _, kv := range os.Environ() {
		drop := false
		for _, p := range launchEnvRemoved {
			if strings.HasPrefix(kv, p) {
				drop = true
				break
			}
		}
		if !drop {
			env = append(env, kv)
		}
	}
	env = append(env, resp.Env...)
	if err := approveClaudeAPIKey(env); err != nil { // apikeyapproval.go: else Claude asks whether to use the key, default No
		fmt.Fprintf(os.Stderr, "jarvisd launch %s: could not mark the API key as approved for Claude Code: %v\n", name, err)
	}
	for _, arg := range args {
		if arg == "--model" || strings.HasPrefix(arg, "--model=") {
			resp.Args = withoutModelArgs(resp.Args)
			break
		}
	}
	return syscall.Exec(claude, append(append([]string{"claude"}, resp.Args...), args...), env)
}

// launcherEnv answers `jarvisd launch` on the local socket.
func (n *Node) launcherEnv(name string) map[string]any {
	l, ok := n.managedLauncher(name)
	if !ok {
		return map[string]any{"error": "no launcher " + name + " in Persona (设置 › 模型), or the hub has not sent it yet"}
	}
	return map[string]any{"env": managedEnv(l), "args": l.Args}
}

var secretValue = regexp.MustCompile(`(?i)((?:key|token|secret|password|passwd|auth)[A-Za-z0-9_]*\s*=\s*)("[^"]*"|'[^']*'|\S+)`)

// launcherDefinition is a ~/.zshrc launcher's function body, for its page in 设置 › 模型, with anything that looks like
// a key or token replaced by •••.
func (n *Node) launcherDefinition(ctx context.Context, name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("invalid launcher %q", name)
	}
	out, err := loginShell(ctx, "whence -f "+name+" 2>/dev/null || type "+name).Output()
	if err != nil && len(out) == 0 {
		return "", fmt.Errorf("no launcher %s on this machine", name)
	}
	return redactSecrets(strings.TrimSpace(string(out))), nil
}

// tokenLike: a literal that looks like a key wherever it sits (an argument to _claude_proxy, "Bearer …"): a known
// prefix, or a long run of letters and digits with no path or URL in it.
var tokenLike = regexp.MustCompile(`Bearer\s+[^\s"']+|\b(?:sk|vk|at|ak|pk|rk|plat|trxp_ak|ghp|gho|xox[bp])[-_][A-Za-z0-9._+/=-]{8,}|[A-Za-z0-9_+=-]{28,}`)

func redactSecrets(s string) string {
	s = tokenLike.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasPrefix(m, "Bearer") {
			return "Bearer •••"
		}
		hasDigit, hasLetter := strings.ContainsAny(m, "0123456789"), strings.ContainsAny(strings.ToLower(m), "abcdefghijklmnopqrstuvwxyz")
		if len(m) >= 28 && !(hasDigit && hasLetter) {
			return m // a long word, not a key
		}
		if len(m) >= 28 && strings.Count(m, "_") > 3 && !strings.ContainsAny(m, "0123456789") {
			return m
		}
		return "•••"
	})
	return secretValue.ReplaceAllStringFunc(s, func(m string) string {
		sub := secretValue.FindStringSubmatch(m)
		v := sub[2]
		// A value read from a file or another variable shows where it comes from, not a secret.
		if strings.Contains(v, "$") || strings.Contains(v, "<") {
			return m
		}
		return sub[1] + "•••"
	})
}
