package node

import (
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

	"github.com/nwverse/persona/server/internal/nodeclient"
	"github.com/nwverse/persona/server/internal/osproc"
	"github.com/nwverse/persona/server/internal/proto"
)

// Launchers kept by the hub ("Settings › Model", docs/launchers.md): the hub sends them (keys included) on connect and on
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
		// An API provider (kind api) runs claude too, on its messages endpoint (#2455); the hub sends none on another.
		if l.Kind == "api" && l.API != "" && l.API != "messages" {
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
// would otherwise answer every agent with "Not logged in" (#1459).
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

// shellDirQ quotes a directory for a shell command with q, keeping a leading ~/ as "$HOME"/ so it is the home of the
// shell that runs it.
func shellDirQ(p string, q func(string) string) string {
	if p == "~" {
		return `"$HOME"`
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return `"$HOME"/` + q(rest)
	}
	return q(p)
}

func managedEnv(l proto.ManagedLauncher) []string {
	switch l.AgentType() {
	case "codex":
		var env []string
		if l.BaseURL != "" {
			env = append(env, "OPENAI_BASE_URL="+l.BaseURL)
		}
		if l.APIKey != "" {
			env = append(env, "OPENAI_API_KEY="+l.APIKey)
		}
		return env
	case "agy":
		var env []string
		if l.BaseURL != "" {
			env = append(env, "GOOGLE_GEMINI_BASE_URL="+l.BaseURL)
		}
		if l.APIKey != "" {
			env = append(env, "GEMINI_API_KEY="+l.APIKey)
		}
		return env
	}
	return envList(providerEnv(l, "", "", false))
}

// artifactsOff turns Claude Code's Artifact tool off for a claude a managed launcher runs. Artifacts publish to
// claude.ai, which a gateway with an API key is not. Claude Code turns the tool on by default (since 2.1.26x), and an
// interactive session then sends the Artifact skills in its system message; a gateway that cannot carry them refuses
// the first request ("400 request is not representable on this edge: Claude Code Artifacts are enabled"), so every
// Agent started in herdr through it failed on its first turn while `claude -p` went on working. The environment
// variable reaches `jarvisd launch` (headless runs) and the settings env alike; the settings file also says
// enableArtifact: false (mergeManagedSettings): off in --settings wins over the other settings sources.
const artifactsOff = "CLAUDE_CODE_DISABLE_ARTIFACT=1"

// managedSettingsEnv is managedEnv as a settings "env" map (providerEnv). Settings env wins over the process env, so
// the user's ~/.claude/settings.json (another gateway, another token) would otherwise replace a managed launcher's.
func managedSettingsEnv(l proto.ManagedLauncher) map[string]string {
	return pinnedEnv(providerEnv(l, "", "", false))
}

// claudeExecutable finds claude: PATH first, then the usual install spots. A node started by launchd (macOS) has a
// minimal PATH that misses /opt/homebrew/bin, and nothing in `jarvisd launch`'s own path adds it.
func agentExecutable(bin string) (string, error) {
	if p, err := exec.LookPath(bin); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(home, ".local", "bin", bin), "/opt/homebrew/bin/" + bin,
		"/usr/local/bin/" + bin, filepath.Join(home, ".claude", "local", bin)} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s not found in PATH or the usual install locations", bin)
}

func claudeExecutable() (string, error) { return agentExecutable("claude") }

// Launch is `jarvisd launch <name> [claude args…]`: the managed launcher's settings from the local node, then claude.
func Launch(ctx context.Context, name string, args []string) error {
	line, err := nodeclient.Dial(ctx, sockRequest{Kind: "launcher.env", Name: name})
	if err != nil {
		return fmt.Errorf("jarvisd launch %s: %w", name, err)
	}
	var resp struct {
		Agent string   `json:"agent"`
		Model string   `json:"model"`
		Env   []string `json:"env"`
		Clear []string `json:"clear"` // what to drop from the shell's env first (envClearFor)
		Args  []string `json:"args"`
		Error string   `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New("jarvisd launch " + name + ": " + resp.Error)
	}
	agent := resp.Agent
	if agent == "" {
		agent = "claude"
	}
	exe, err := agentExecutable(agent)
	if err != nil {
		return fmt.Errorf("jarvisd launch: %w", err)
	}
	var env []string
	if agent == "claude" {
		if len(resp.Clear) == 0 {
			resp.Clear = legacyEnvClear
		}
		resp.Clear, resp.Env = launchEnvFor(resp.Clear, resp.Env, os.Getenv("PERSONA_ASSISTANT") == "1")
		env = append(withoutEnv(os.Environ(), resp.Clear), resp.Env...)
		if err := approveClaudeAPIKey(env); err != nil { // apikeyapproval.go: else Claude asks whether to use the key, default No
			fmt.Fprintf(os.Stderr, "jarvisd launch %s: could not mark the API key as approved for Claude Code: %v\n", name, err)
		}
	} else {
		env = append(os.Environ(), resp.Env...)
	}
	hasModelArg := false
	hasSettingsArg := false
	for _, arg := range args {
		if arg == "-m" || arg == "--model" || strings.HasPrefix(arg, "--model=") {
			hasModelArg = true
			resp.Args = withoutModelArgs(resp.Args)
		}
		if arg == "--settings" || strings.HasPrefix(arg, "--settings=") {
			hasSettingsArg = true
		}
	}
	for _, arg := range resp.Args {
		if arg == "-m" || arg == "--model" || strings.HasPrefix(arg, "--model=") {
			hasModelArg = true
		}
		if arg == "--settings" || strings.HasPrefix(arg, "--settings=") {
			hasSettingsArg = true
		}
	}
	launchArgs := append([]string{agent}, resp.Args...)
	if agent == "codex" && resp.Model != "" && !hasModelArg {
		launchArgs = append(launchArgs, "-m", resp.Model)
	} else if agent == "agy" {
		if resp.Model != "" && !hasModelArg {
			launchArgs = append(launchArgs, "--model", resp.Model)
		}
		for _, kv := range resp.Env {
			if strings.HasPrefix(kv, "GEMINI_API_KEY=") && len(kv) > len("GEMINI_API_KEY=") && !hasSettingsArg {
				launchArgs = append(launchArgs, "--settings", `{"modelProvider":"gemini"}`)
				break
			}
		}
	}
	return osproc.Exec(exe, append(launchArgs, args...), env)
}

// launcherEnv answers `jarvisd launch` on the local socket.
func (n *Node) launcherEnv(name string) map[string]any {
	l, ok := n.managedLauncher(name)
	if !ok {
		return map[string]any{"error": "no launcher " + name + " in Persona (设置 › 模型), or the hub has not sent it yet"}
	}
	return map[string]any{"env": managedEnv(l), "clear": envClearFor(l), "args": l.Args, "agent": l.AgentType(), "model": l.Model}
}

var secretValue = regexp.MustCompile(`(?i)((?:key|token|secret|password|passwd|auth)[A-Za-z0-9_]*\s*=\s*)("[^"]*"|'[^']*'|\S+)`)

// launcherDefinition is a ~/.zshrc launcher's function body, for its page in "Settings › Model", with anything that looks like
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
