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

// launcherCheck answers 设置 › 模型's 测试可用性: runs the launcher once, headless, with a one-word prompt in an empty
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
	if m, managed := n.managedLauncher(name); managed {
		// Without --settings the check reads the user's global settings, whose env (another gateway) would win over
		// the launcher's. Real starts always pass a settings file; this one carries the launcher's env too.
		_ = m
		if settings := n.claudeSettings(ctx, name, filepath.Join(n.dir, "claude-settings.json"), dir); settings != "" {
			args += ` --settings ` + shellQuote(settings)
		}
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

// claudeResult reads `claude -p --output-format json`'s last JSON object (an interactive zsh may print banners first).
func claudeResult(out string) (claudeOut, bool) {
	i := strings.LastIndex(out, "\n{")
	body := out
	if i >= 0 {
		body = out[i+1:]
	} else if !strings.HasPrefix(out, "{") {
		return claudeOut{}, false
	}
	var r struct {
		Result     string                     `json:"result"`
		IsError    bool                       `json:"is_error"`
		Subtype    string                     `json:"subtype"`
		DurationMS int                        `json:"duration_ms"`
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	}
	if json.Unmarshal([]byte(body), &r) != nil {
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
