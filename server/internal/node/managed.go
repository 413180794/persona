package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"github.com/chyroc/jarvis/server/internal/proto"
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
// claude), or `jarvisd launch <name>` for a managed one.
func (n *Node) launcherCommand(name string) string {
	if _, ok := n.managedLauncher(name); ok {
		return shellQuote(n.bin) + " launch " + name
	}
	return name
}

// managedEnv is the environment a managed launcher sets: the gateway, its key, the model for every tier.
func managedEnv(l proto.ManagedLauncher) []string {
	small := l.SmallModel
	if small == "" {
		small = l.Model
	}
	env := []string{"ANTHROPIC_BASE_URL=" + l.BaseURL}
	if l.APIKey != "" {
		env = append(env, "ANTHROPIC_API_KEY="+l.APIKey)
	}
	if l.Model != "" {
		for _, k := range []string{"ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_FABLE_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL",
			"ANTHROPIC_DEFAULT_SONNET_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL"} {
			env = append(env, k+"="+l.Model)
		}
	}
	if small != "" {
		env = append(env, "ANTHROPIC_DEFAULT_HAIKU_MODEL="+small, "ANTHROPIC_SMALL_FAST_MODEL="+small)
	}
	return env
}

// launchEnvRemoved: what the shell may carry from another launcher or account, dropped before a managed one runs.
var launchEnvRemoved = []string{"ANTHROPIC_", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
	"CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_SUBAGENT_MODEL"}

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
	claude, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("jarvisd launch: claude not found in PATH: %w", err)
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
	return syscall.Exec(claude, append(append([]string{"claude"}, resp.Args...), args...), env)
}

// launcherEnv answers `jarvisd launch` on the local socket.
func (n *Node) launcherEnv(name string) map[string]any {
	l, ok := n.managedLauncher(name)
	if !ok {
		return map[string]any{"error": "no launcher " + name + " in Jarvis (设置 › 模型), or the hub has not sent it yet"}
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

func redactSecrets(s string) string {
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
