package node

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// SocketPath is where hook and mcp subprocesses reach the node.
func SocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".jarvis", "node.sock")
}

// HookTimeout is how long a hook waits for the phone before handing the dialog back to the terminal.
var HookTimeout = 30 * time.Minute

type sockRequest struct {
	Kind      string          `json:"kind"`
	Input     json.RawMessage `json:"input,omitempty"`
	Pane      string          `json:"pane,omitempty"`
	Name      string          `json:"name,omitempty"`
	Args      json.RawMessage `json:"args,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
}

func (n *Node) listenHookSocket(ctx context.Context) (net.Listener, error) {
	path := SocketPath()
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go n.serveSock(ctx, c)
		}
	}()
	return ln, nil
}

func (n *Node) serveSock(ctx context.Context, c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	line, err := r.ReadBytes('\n')
	if err != nil {
		return
	}
	var req sockRequest
	if json.Unmarshal(line, &req) != nil {
		return
	}
	// The caller going away (hook killed because the user answered in the terminal) cancels the request.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_, _ = r.ReadByte()
		cancel()
	}()

	var resp any
	switch req.Kind {
	case "hook":
		out := n.handleHook(ctx, req.Input, req.Pane)
		resp = map[string]any{"output": out}
	case "mcp.tools":
		var res struct {
			Tools []proto.Tool `json:"tools"`
		}
		if err := n.peer.Call(ctx, "mcp.tools", map[string]any{}, &res); err != nil {
			resp = map[string]any{"tools": []proto.Tool{}, "error": err.Error()}
		} else {
			resp = res
		}
	case "mcp.call":
		var res proto.ToolResult
		if err := n.peer.Call(ctx, "mcp.call", map[string]any{"name": req.Name, "args": req.Args, "session_id": req.SessionID}, &res); err != nil {
			res = proto.TextResult("Jarvis hub error: "+err.Error(), true)
		}
		resp = res
	default:
		resp = map[string]any{"error": "unknown kind"}
	}
	b, _ := json.Marshal(resp)
	_, _ = c.Write(append(b, '\n'))
}

type hookInput struct {
	HookEventName        string          `json:"hook_event_name"`
	SessionID            string          `json:"session_id"`
	TranscriptPath       string          `json:"transcript_path"`
	Cwd                  string          `json:"cwd"`
	PermissionMode       string          `json:"permission_mode"`
	ToolName             string          `json:"tool_name"`
	ToolInput            json.RawMessage `json:"tool_input"`
	ToolUseID            string          `json:"tool_use_id"`
	Message              string          `json:"message"`
	LastAssistantMessage string          `json:"last_assistant_message"`
	Prompt               string          `json:"prompt"`
}

// handleHook returns the JSON Claude Code should read from the hook's stdout, or nil to let the TUI decide.
func (n *Node) handleHook(ctx context.Context, raw json.RawMessage, pane string) any {
	var in hookInput
	if json.Unmarshal(raw, &in) != nil || n.peer == nil {
		return nil
	}
	if in.TranscriptPath != "" && validID(in.SessionID) {
		n.mu.Lock()
		n.paths[in.SessionID] = in.TranscriptPath
		n.mu.Unlock()
	}
	_ = n.peer.Notify("hook.event", map[string]any{
		"session_id": in.SessionID, "event": in.HookEventName, "tool_name": in.ToolName,
		"message": in.Message, "last_assistant_message": snippet(in.LastAssistantMessage), "cwd": in.Cwd,
	})

	ix := proto.Interaction{
		ID: "ix_" + newUUID()[:12], SessionID: in.SessionID, Cwd: in.Cwd, CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().Add(HookTimeout).UTC(), ToolName: in.ToolName, ToolUseID: in.ToolUseID, ToolInput: in.ToolInput,
	}
	if p := n.transcriptPath(in.SessionID); p != "" {
		if st, err := os.Stat(p); err == nil {
			ix.SessionTitle = n.meta(p, st).title
		}
	}
	if ix.SessionTitle == "" {
		ix.SessionTitle = filepath.Base(in.Cwd)
	}

	switch {
	case in.HookEventName == "PreToolUse" && in.ToolName == "AskUserQuestion":
		var q struct {
			Questions []struct {
				Question    string                 `json:"question"`
				Header      string                 `json:"header"`
				MultiSelect bool                   `json:"multiSelect"`
				Options     []proto.QuestionOption `json:"options"`
			} `json:"questions"`
		}
		if json.Unmarshal(in.ToolInput, &q) != nil || len(q.Questions) == 0 {
			return nil
		}
		ix.Kind = proto.IxQuestion
		for _, qq := range q.Questions {
			ix.Questions = append(ix.Questions, proto.Question{Question: qq.Question, Header: qq.Header, MultiSelect: qq.MultiSelect, Options: qq.Options})
		}
		res, ok := n.ask(ctx, ix)
		if !ok || len(res.Answers) == 0 {
			return nil
		}
		var input map[string]any
		_ = json.Unmarshal(in.ToolInput, &input)
		input["answers"] = res.Answers
		return map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "allow", "updatedInput": input,
		}}

	case in.HookEventName == "PermissionRequest" && in.ToolName == "ExitPlanMode":
		var p struct {
			Plan string `json:"plan"`
		}
		_ = json.Unmarshal(in.ToolInput, &p)
		ix.Kind, ix.Plan = proto.IxPlan, p.Plan
		res, ok := n.ask(ctx, ix)
		if !ok || res.Approve == nil {
			return nil
		}
		if !*res.Approve {
			msg := "用户在手机上打回了这个计划。"
			if strings.TrimSpace(res.Feedback) != "" {
				msg += "修改意见：" + res.Feedback
			}
			return permissionDecision(map[string]any{"behavior": "deny", "message": msg})
		}
		mode := res.Mode
		if mode == "" {
			mode = "bypassPermissions"
		}
		var input map[string]any
		_ = json.Unmarshal(in.ToolInput, &input)
		// A bare allow does not close the plan dialog; it needs updatedInput plus a setMode update (2.1.310).
		return permissionDecision(map[string]any{
			"behavior": "allow", "updatedInput": input,
			"updatedPermissions": []any{map[string]any{"type": "setMode", "mode": mode, "destination": "session"}},
		})

	case in.HookEventName == "PermissionRequest":
		ix.Kind, ix.Summary = proto.IxPermission, toolSummary(in.ToolName, in.ToolInput)
		res, ok := n.ask(ctx, ix)
		if !ok || res.Allow == nil {
			return nil
		}
		if !*res.Allow {
			msg := res.Message
			if msg == "" {
				msg = "用户在手机上拒绝了这次调用。"
			}
			return permissionDecision(map[string]any{"behavior": "deny", "message": msg})
		}
		var input map[string]any
		_ = json.Unmarshal(in.ToolInput, &input)
		d := map[string]any{"behavior": "allow", "updatedInput": input}
		if res.Always {
			d["updatedPermissions"] = []any{map[string]any{
				"type": "addRules", "rules": []any{map[string]any{"toolName": in.ToolName}},
				"behavior": "allow", "destination": "session",
			}}
		}
		return permissionDecision(d)
	}
	return nil
}

func permissionDecision(d map[string]any) any {
	return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PermissionRequest", "decision": d}}
}

// ask forwards an interaction to the hub and blocks until the phone answers, it expires, or the hook is cancelled.
func (n *Node) ask(ctx context.Context, ix proto.Interaction) (proto.Resolution, bool) {
	ctx, cancel := context.WithDeadline(ctx, ix.ExpiresAt)
	defer cancel()
	var res proto.Resolution
	err := n.peer.Call(ctx, "interaction.create", map[string]any{"interaction": ix}, &res)
	if err != nil {
		_ = n.peer.Notify("interaction.cancel", map[string]any{"id": ix.ID})
		return res, false
	}
	if res.Dismiss {
		return res, false
	}
	return res, true
}

func toolSummary(name string, input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	pick := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := m[k].(string); ok && s != "" {
				return s
			}
		}
		return ""
	}
	switch name {
	case "Bash":
		return pick("command")
	case "Edit", "Write", "Read", "MultiEdit", "NotebookEdit":
		return pick("file_path", "notebook_path")
	case "WebFetch":
		return pick("url")
	case "WebSearch":
		return pick("query")
	}
	if s := pick("description", "command", "path", "url", "query", "prompt"); s != "" {
		return s
	}
	b, _ := json.Marshal(m)
	return snippet(string(b))
}

// DialSocket sends one request to the local node and returns the raw response line.
func DialSocket(ctx context.Context, req any) ([]byte, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, "unix", SocketPath())
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(dl)
	}
	b, _ := json.Marshal(req)
	if _, err := c.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("node closed connection: %w", err)
	}
	return line, nil
}
