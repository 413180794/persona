package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Guest conversations (the hub's assistant/guest.go, docs/assistant.md「访客对话」): an assistant conversation that
// answers someone else for the user. It is started on Claude Code only (no other agent, runtime or the Agent SDK,
// whose built-in tools could not be taken away), never in the warm spare, and every process it runs in (a resume,
// a compaction, a model switch) has no built-in tools (--tools ""), only Persona's MCP server (--strict-mcp-config:
// none of the user's or the extensions'), and only its own tools allowed. The hub lists and runs only those for it
// and gives it no live snapshot; the tools it has are kept in its sessionState.Guest.

// maxGuestTools is the hub's bound (assistant/guest.go).
const maxGuestTools = 8

var guestToolName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// checkGuestTools: one to maxGuestTools tool names, plain identifiers (they go into --allowedTools).
func checkGuestTools(tools []string) error {
	if len(tools) == 0 || len(tools) > maxGuestTools {
		return fmt.Errorf("a guest conversation takes 1 to %d tools", maxGuestTools)
	}
	for _, t := range tools {
		if !guestToolName.MatchString(t) {
			return fmt.Errorf("bad guest tool %q", t)
		}
	}
	return nil
}

// guestParams is assistant.send's guest: the tools of Persona's it may use.
type guestParams struct {
	Tools []string `json:"tools"`
}

type guestKey struct{}

// withGuest marks ctx's assistant.send as starting a guest conversation with tools.
func withGuest(ctx context.Context, tools []string) context.Context {
	return context.WithValue(ctx, guestKey{}, tools)
}

func guestFrom(ctx context.Context) []string {
	tools, _ := ctx.Value(guestKey{}).([]string)
	return tools
}

// errGuestAgent refuses a guest conversation anything but Claude Code on the CLI.
var errGuestAgent = errors.New("a guest conversation runs on Claude Code only (its built-in tools can be taken away there)")

// assistantGuestPrepare makes a guest conversation's directory and writes only its instructions there.
func (n *Node) assistantGuestPrepare(params json.RawMessage) (any, error) {
	var a struct {
		Dir      string `json:"dir"`
		ClaudeMD string `json:"claude_md"`
	}
	if err := json.Unmarshal(params, &a); err != nil || a.Dir == "" || strings.Contains(a.Dir, "..") || filepath.IsAbs(a.Dir) {
		return nil, errors.New("bad guest dir")
	}
	dir := filepath.Join(n.home, a.Dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(a.ClaudeMD), 0o600); err != nil {
			return nil, err
		}
	}
	return map[string]any{"cwd": dir}, nil
}

// guestOf is conversation id's guest tools (nil: not a guest).
func (n *Node) guestOf(id string) []string {
	if id == "" {
		return nil
	}
	st, _ := n.state.get(id)
	return st.Guest
}

// guestMCPAndTools are a guest conversation's process flags: Persona's MCP server alone, told it is a guest's, no
// built-in tools, and only its tools allowed.
func (n *Node) guestMCPAndTools(id string, tools []string) (mcpJSON string, toolFlags []string) {
	env := map[string]string{"PERSONA_ASSISTANT": "1", "JARVIS_ASSISTANT": "1", "PERSONA_SESSION_ID": id, "JARVIS_SESSION_ID": id,
		"PERSONA_GUEST": "1", "JARVIS_GUEST": "1"}
	mcp, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"jarvis": map[string]any{"command": n.bin, "args": []string{"mcp"}, "env": env}}})
	allowed := make([]string, len(tools))
	for i, t := range tools {
		allowed[i] = "mcp__jarvis__" + t
	}
	return string(mcp), []string{"--mcp-config", string(mcp), "--strict-mcp-config", "--tools", "", "--allowedTools", strings.Join(allowed, ",")}
}
