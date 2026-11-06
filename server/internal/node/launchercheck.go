package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// launcherCheck answers Settings › Models' "Test": runs the launcher once, headless, with a one-word prompt in an empty
// directory, and says whether an answer came back, how long it took and which model answered. It is a real request
// (a few tokens).
func (n *Node) launcherCheck(ctx context.Context, name string) proto.LauncherCheck {
	if name != "claude" && !nameRe.MatchString(name) {
		return proto.LauncherCheck{Error: fmt.Sprintf("invalid launcher %q", name)}
	}
	agent := "claude"
	if _, managed := n.managedLauncher(name); !managed && name != "claude" {
		if d := n.launcherProbeCached(ctx, name); d.Agent != "" {
			agent = d.Agent
		}
	}
	dir, err := os.MkdirTemp("", "jarvis-check-")
	if err != nil {
		return proto.LauncherCheck{Error: err.Error()}
	}
	defer os.RemoveAll(dir)
	prompt := prompts.RenderLauncherCheck()
	args := ` -p ` + shellQuote(prompt) + ` --output-format json --max-turns 1`
	if agent != "claude" { // Codex / TRAEX: one non-interactive turn
		args = ` exec --skip-git-repo-check ` + shellQuote(prompt)
	}
	if agent == "claude" {
		// No hooks: the check is no session of the user's, and Persona's own hooks can hold it past the timeout (#3412;
		// the model list does the same, launcher_models.go). claudeSettings merges this with what the launcher needs from
		// --settings: a managed launcher's env (else the user's global settings env, another gateway, would win), an
		// alias's own --settings (Claude Code reads only the last one).
		ours := filepath.Join(dir, "check-settings.json")
		if err := os.WriteFile(ours, []byte(`{"disableAllHooks":true}`), 0o600); err != nil {
			return proto.LauncherCheck{Error: err.Error()}
		}
		args += ` --settings ` + shellQuote(n.claudeSettings(ctx, name, ours, dir))
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := loginShell(ctx, "cd "+shellQuote(dir)+" && "+n.launcherCommand(name)+args+" </dev/null")
	killGroupOnCancel(cmd)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	res := proto.LauncherCheck{Agent: agent, Millis: int(time.Since(start).Milliseconds())}
	if ctx.Err() != nil {
		res.Error = "90 秒内没有回答"
		return res
	}
	text := strings.TrimSpace(string(out))
	if agent == "claude" {
		if r, ok := claudeResult(text); ok {
			res.Model, res.Answer = r.model, clipText(r.result, 200)
			if r.isError {
				res.Error = clipText(redactSecrets(r.result), 400)
			} else {
				res.OK = true
			}
			if r.millis > 0 {
				res.Millis = r.millis
			}
			return res
		}
	} else if err == nil && text != "" {
		lines := strings.Split(text, "\n")
		res.OK, res.Answer = true, clipText(lines[len(lines)-1], 200)
		return res
	}
	res.Error = clipText(redactSecrets(lastLines(text, 6)), 400)
	if res.Error == "" && err != nil {
		res.Error = err.Error()
	}
	return res
}

type claudeOut struct {
	result  string
	isError bool
	model   string
	millis  int
}

// claudeResult reads `claude -p --output-format json`'s answer: the last line that holds a JSON object, from its first
// "{" that starts one (an interactive zsh may print banners first, and a shell integration's escape codes on the same
// line, #3412).
func claudeResult(out string) (claudeOut, bool) {
	lines := strings.Split(out, "\n")
	for l := len(lines) - 1; l >= 0; l-- {
		line := lines[l]
		for i := strings.IndexByte(line, '{'); i >= 0; {
			if r, ok := claudeResultJSON(line[i:]); ok {
				return r, true
			}
			next := strings.IndexByte(line[i+1:], '{')
			if next < 0 {
				break
			}
			i += 1 + next
		}
	}
	return claudeOut{}, false
}

func claudeResultJSON(body string) (claudeOut, bool) {
	var r struct {
		Result     string                     `json:"result"`
		IsError    bool                       `json:"is_error"`
		Subtype    string                     `json:"subtype"`
		DurationMS int                        `json:"duration_ms"`
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(body)), &r) != nil || (r.Result == "" && r.Subtype == "" && r.ModelUsage == nil) {
		return claudeOut{}, false
	}
	c := claudeOut{result: r.Result, isError: r.IsError || (r.Subtype != "" && r.Subtype != "success"), millis: r.DurationMS}
	if c.isError && c.result == "" {
		c.result = r.Subtype
	}
	for m := range r.ModelUsage {
		if c.model == "" || len(m) > len(c.model) { // the main model; the small one is usually a prefix-free other name
			c.model = m
		}
	}
	return c, true
}

func clipText(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}
