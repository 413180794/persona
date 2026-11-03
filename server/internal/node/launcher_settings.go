package node

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// A launcher alias can bring its own settings (`alias work="claude --settings ~/.claude/settings.work.json …"`, with
// the gateway, its token and the model in that file's env). Every claude start Persona makes adds its own --settings
// after the launcher's words (the hooks that bring questions, permission requests and turn ends to the phone), and
// Claude Code keeps only the last --settings it is given: the launcher's file was dropped, and the claude it started
// answered every prompt with "Not logged in". So the start passes one file that is both: the launcher's settings
// with Persona's hooks added to its own, as the last --settings.

// claudeSettings is the --settings a claude start of launcher passes: ours (a file in n.dir), or, when the launcher
// is an alias with its own --settings, the two merged into launcher-settings/<launcher>-<ours>. cwd is where claude
// runs (a relative path in the alias is from there). Anything wrong with the launcher's file: ours, as before.
func (n *Node) claudeSettings(ctx context.Context, launcher, ours, cwd string) string {
	own := n.launcherOwnSettings(ctx, launcher)
	if own == "" {
		return ours
	}
	path, err := n.mergeLauncherSettings(launcher, own, ours, cwd)
	if err != nil {
		// Not the value: an inline --settings '{…}' can hold a token.
		n.logger.Printf("launcher %s: its own --settings not merged with Persona's, it will be ignored: %v", launcher, err)
		return ours
	}
	return path
}

// launcherOwnSettings is the --settings the launcher's alias gives claude, "" for none (plain claude, a managed
// launcher, a function, an alias without one). The list is read first when it has not been since the node started.
func (n *Node) launcherOwnSettings(ctx context.Context, launcher string) string {
	if launcher == "" || launcher == "claude" {
		return ""
	}
	if _, ok := n.managedLauncher(launcher); ok {
		return "" // jarvisd launch sets its environment and execs claude: no settings of its own
	}
	launcherCache.Lock()
	loaded := !launcherCache.at.IsZero()
	launcherCache.Unlock()
	if !loaded {
		_, _ = n.launchers(ctx)
	}
	launcherCache.Lock()
	defer launcherCache.Unlock()
	return launcherCache.settings[launcher]
}

// mergeLauncherSettings writes the launcher's settings (own: what its alias passes) merged with ours, and says where.
// Written again on every start: the launcher's file may have changed. 0600: it holds the launcher's token.
func (n *Node) mergeLauncherSettings(launcher, own, ours, cwd string) (string, error) {
	theirs, err := readLauncherSettings(own, cwd, n.home)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(ours)
	if err != nil {
		return "", err
	}
	var mine map[string]any
	if err := json.Unmarshal(b, &mine); err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(mergeClaudeSettings(theirs, mine), "", "  ")
	if err != nil {
		return "", err
	}
	dir := filepath.Join(n.dir, "launcher-settings")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, launcher+"-"+filepath.Base(ours))
	// A temporary file of its own: two starts of the same launcher at once must not rename each other's away.
	tmp, err := os.CreateTemp(dir, "."+launcher+"-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(tmp.Name(), path)
}

// readLauncherSettings reads what an alias passes as --settings: a file (~ and $HOME are the home, a relative path
// is from cwd) or, as Claude Code also takes it, the JSON itself.
func readLauncherSettings(arg, cwd, home string) (map[string]any, error) {
	s := strings.TrimSpace(arg)
	var b []byte
	if strings.HasPrefix(s, "{") {
		b = []byte(s)
	} else {
		path := shellPath(s, home)
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		var err error
		if b, err = os.ReadFile(path); err != nil {
			return nil, err
		}
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, errors.New("not a JSON object")
	}
	if v == nil {
		v = map[string]any{}
	}
	return v, nil
}

// shellPath is a path from an alias's words (they come unexpanded) as the shell would read ~ and $HOME in it.
func shellPath(p, home string) string {
	for _, h := range []string{"${HOME}", "$HOME"} {
		if p == h || strings.HasPrefix(p, h+"/") {
			p = "~" + p[len(h):]
			break
		}
	}
	return expandHome(p, home) // claudedirs.go
}

// mergeClaudeSettings is the launcher's settings (theirs) with ours on top: for each hook event our hooks after its
// own, so both run; any other key of ours (there is none today) in place of its.
func mergeClaudeSettings(theirs, mine map[string]any) map[string]any {
	out := make(map[string]any, len(theirs)+len(mine))
	for k, v := range theirs {
		out[k] = v
	}
	for k, v := range mine {
		ourHooks, ok := v.(map[string]any)
		if k != "hooks" || !ok {
			out[k] = v
			continue
		}
		hooks := map[string]any{}
		if theirHooks, ok := theirs["hooks"].(map[string]any); ok {
			for event, list := range theirHooks {
				hooks[event] = list
			}
		}
		for event, list := range ourHooks {
			prev, _ := hooks[event].([]any)
			add, _ := list.([]any)
			hooks[event] = append(append([]any{}, prev...), add...)
		}
		out["hooks"] = hooks
	}
	return out
}
