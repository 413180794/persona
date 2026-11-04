package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// assistantRuntimeHello is a runtime whose launcher can run the assistant (caps.assistant).
func assistantRuntimeHello() map[string]any {
	return map[string]any{"runtime": "dsh", "name": "DSH", "launcher": "dsh",
		"caps": map[string]any{"start": true, "send": true, "interrupt": true, "assistant": true}}
}

type hubCalls struct {
	mu    sync.Mutex
	tools []json.RawMessage
	calls []json.RawMessage
	asked []proto.Interaction
}

// assistantHub answers what the hub answers an assistant on a runtime: the snapshot, its tools, their calls.
func assistantHub(hub *runtimeHub) *hubCalls {
	c := &hubCalls{}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	hub.answer = func(method string, params json.RawMessage) (any, error, bool) {
		c.mu.Lock()
		defer c.mu.Unlock()
		switch method {
		case "assistant.context":
			return map[string]any{"text": "- 现在：测试时间\n- 在线电脑：测试机"}, nil, true
		case "assistant.lark":
			return map[string]any{}, nil, true
		case "mcp.tools":
			c.tools = append(c.tools, params)
			var p struct {
				Assistant bool `json:"assistant"`
			}
			_ = json.Unmarshal(params, &p)
			tools := []proto.Tool{{Name: "phone_contacts", InputSchema: json.RawMessage(`{"type":"object"}`)}}
			if p.Assistant {
				tools = append(tools, proto.Tool{Name: "ask_user", InputSchema: json.RawMessage(`{"type":"object"}`)})
			}
			return map[string]any{"tools": tools}, nil, true
		case "mcp.call":
			c.calls = append(c.calls, params)
			return proto.TextResult("ok", false), nil, true
		}
		return nil, nil, false
	}
	return c
}

func startedAssistant(t *testing.T, n *Node, host *fakeRuntime) string {
	t.Helper()
	host.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.start" {
			return map[string]any{"session": map[string]any{"session_id": "session-a1", "cwd": filepath.Join(n.home, ".jarvis", "assistant"), "status": "idle"}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	raw, err := nodeCall(t, n, "assistant.send", map[string]any{"launcher": "dsh", "cwd": filepath.Join(n.home, ".jarvis", "assistant"), "text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		SessionID string `json:"session_id"`
	}
	_ = json.Unmarshal(raw, &res)
	if res.SessionID != "session-a1" {
		t.Fatalf("assistant.send %s", raw)
	}
	return res.SessionID
}

func TestAssistantOnARuntimeStartsAndSendsWithTheSnapshot(t *testing.T) {
	n, hub := runtimeNode(t)
	assistantHub(hub)
	host := dialRuntime(t, n, assistantRuntimeHello())
	if a, err := n.assistantAgent(context.Background(), "dsh"); err != nil || a != assistantOnRuntime {
		t.Fatalf("assistantAgent: %q %v", a, err)
	}
	sid := startedAssistant(t, n, host)

	starts := host.got("session.start")
	if len(starts) != 1 || !strings.Contains(string(starts[0]), `"assistant":true`) {
		t.Fatalf("session.start %s", starts)
	}
	sends := host.got("session.send")
	if len(sends) != 1 {
		t.Fatalf("%d sends", len(sends))
	}
	var p struct {
		SessionID string `json:"session_id"`
		Text      string `json:"text"`
		Context   string `json:"context"`
		Assistant bool   `json:"assistant"`
		MemoryDir string `json:"memory_dir"`
	}
	_ = json.Unmarshal(sends[0], &p)
	if p.SessionID != sid || p.Text != "hello" || !p.Assistant || p.MemoryDir == "" {
		t.Fatalf("send %s", sends[0])
	}
	if !strings.Contains(p.Context, "测试时间") || !strings.Contains(p.Context, "MEMORY.md") {
		t.Fatalf("the first message's context: %q", p.Context)
	}
	if st, ok := n.state.get(sid); !ok || st.Launcher != "dsh" || !strings.HasSuffix(st.Cwd, "/.jarvis/assistant") {
		t.Fatalf("state %+v", st)
	}
	if !n.isAssistantSession(sid) || !n.runtimeAssistant(sid) {
		t.Fatal("not known as an assistant conversation")
	}

	// The phone's message to it is the assistant's: the snapshot again (only what changed) and the mode.
	if _, err := nodeCall(t, n, "session.modes", map[string]any{"session_id": sid, "permission_mode": "plan"}); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": sid, "text": "next", "client_msg_id": "m-2"}); err != nil {
		t.Fatal(err)
	}
	sends = host.got("session.send")
	if len(sends) != 2 {
		t.Fatalf("%d sends", len(sends))
	}
	var q struct {
		Text      string `json:"text"`
		Mode      string `json:"mode"`
		Assistant bool   `json:"assistant"`
		Context   string `json:"context"`
	}
	_ = json.Unmarshal(sends[1], &q)
	if q.Text != "next" || q.Mode != "plan" || !q.Assistant || strings.Contains(q.Context, "MEMORY.md") || !strings.Contains(q.Context, "测试时间") {
		t.Fatalf("second send %s", sends[1])
	}
	if s := listed(t, n)[sid]; s.PermissionMode != "plan" {
		t.Fatalf("listed mode %q", s.PermissionMode)
	}

	// A message without a launcher (the node forgot the conversation's state: a restart) still reaches its runtime.
	n.state.put(sid, sessionState{})
	if _, err := nodeCall(t, n, "assistant.send", map[string]any{"session_id": sid, "text": "after a restart"}); err != nil {
		t.Fatalf("without a launcher: %v", err)
	}
	if got := host.got("session.send"); len(got) != 3 || !strings.Contains(string(got[2]), "after a restart") {
		t.Fatalf("sends %d", len(got))
	}

	// Stop from the phone: the runtime's interrupt.
	if _, err := nodeCall(t, n, "session.interrupt", map[string]any{"session_id": sid}); err != nil || len(host.got("session.interrupt")) != 1 {
		t.Fatalf("interrupt: %v", err)
	}
	// Another kind of agent in the same conversation: refused, not sent.
	if _, err := nodeCall(t, n, "assistant.send", map[string]any{"session_id": sid, "launcher": "claude", "text": "x"}); err == nil {
		t.Fatal("switching a runtime conversation to claude was accepted")
	}
}

func TestAssistantLauncherNeedsTheCapability(t *testing.T) {
	n, _ := runtimeNode(t)
	_ = dialRuntime(t, n, dshHello()) // a runtime without caps.assistant
	if n.assistantRuntime("dsh") != nil {
		t.Fatal("a runtime that did not declare assistant runs the assistant")
	}
	raw, _ := nodeCall(t, n, "launchers.list", map[string]any{})
	if strings.Contains(string(raw), `"assistant":true`) {
		t.Fatalf("launchers %s", raw)
	}
}

func TestAssistantToolsOnlyForTheAssistantsConversations(t *testing.T) {
	n, hub := runtimeNode(t)
	calls := assistantHub(hub)
	host := dialRuntime(t, n, assistantRuntimeHello())
	plain := dialRuntime(t, n, map[string]any{"runtime": "other", "caps": map[string]any{"send": true}})
	sid := startedAssistant(t, n, host)
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{
		{"session_id": sid, "cwd": filepath.Join(n.home, ".jarvis", "assistant"), "status": "idle"},
		{"session_id": "session-w", "cwd": "/w/app", "status": "idle"},
	}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)["session-w"]; return ok })

	toolNames := func(f *fakeRuntime, assistant bool) []string {
		var res struct {
			Tools []proto.Tool `json:"tools"`
		}
		if err := f.peer.Call(context.Background(), "mcp.tools", map[string]any{"assistant": assistant}, &res); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, x := range res.Tools {
			out = append(out, x.Name)
		}
		return out
	}
	if got := toolNames(plain, true); strings.Contains(strings.Join(got, ","), "ask_user") {
		t.Fatalf("a runtime that does not run the assistant got %v", got)
	}
	if got := strings.Join(toolNames(host, true), ","); !strings.Contains(got, "ask_user") {
		t.Fatalf("the assistant's runtime got %v", got)
	}

	call := func(f *fakeRuntime, session string) map[string]any {
		var res proto.ToolResult
		if err := f.peer.Call(context.Background(), "mcp.call", map[string]any{"name": "ask_user", "args": map[string]any{}, "session_id": session, "assistant": true}, &res); err != nil {
			t.Fatal(err)
		}
		calls.mu.Lock()
		defer calls.mu.Unlock()
		var p map[string]any
		_ = json.Unmarshal(calls.calls[len(calls.calls)-1], &p)
		return p
	}
	if p := call(host, sid); p["assistant"] != true || p["session_id"] != sid {
		t.Fatalf("the assistant's call %v", p)
	}
	if p := call(host, "session-w"); p["assistant"] == true {
		t.Fatalf("a call from another conversation passed as the assistant's: %v", p)
	}
	if p := call(plain, sid); p["assistant"] == true {
		t.Fatalf("another runtime's call passed as the assistant's: %v", p)
	}
}

func TestGatedToolGoesThroughTheGate(t *testing.T) {
	n, hub := runtimeNode(t)
	assistantHub(hub)
	skills := filepath.Join(n.home, ".jarvis", "skills")
	t.Setenv("JARVIS_SKILLS_DIR", skills)
	n.dir = filepath.Join(n.home, ".jarvis")
	script := filepath.Join(skills, "example", "scripts", "ex.sh")
	_ = os.MkdirAll(filepath.Dir(script), 0o755)
	_ = os.WriteFile(filepath.Join(skills, "example", "SKILL.md"), []byte("---\nname: example\n---\n"), 0o644)
	ran := filepath.Join(n.home, "ran")
	_ = os.WriteFile(script, []byte("#!/bin/bash\necho \"$*\" >> "+shellQuote(ran)+"\necho done \"$*\"\n"), 0o755)
	g := exampleGate("")
	g.Tool = &proto.GateTool{Name: "example_api", Description: "Run ex."}
	n.mu.Lock()
	n.larkCfg, n.larkCfgAt = proto.AssistantLark{Gates: []proto.CommandGate{g}}, time.Now().Add(time.Hour)
	n.mu.Unlock()
	const lark-connectorTool = "example_api"
	host := dialRuntime(t, n, assistantRuntimeHello())
	sid := startedAssistant(t, n, host)

	var tools struct {
		Tools []proto.Tool `json:"tools"`
	}
	if err := host.peer.Call(context.Background(), "mcp.tools", map[string]any{"assistant": true}, &tools); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolList(tools.Tools), lark-connectorTool) {
		t.Fatalf("tools %v", toolList(tools.Tools))
	}
	run := func(args ...string) proto.ToolResult {
		var res proto.ToolResult
		if err := host.peer.Call(context.Background(), "mcp.call", map[string]any{"name": lark-connectorTool, "args": map[string]any{"args": args}, "session_id": sid, "assistant": true}, &res); err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := run("GET", "/me"); res.IsError || !strings.Contains(res.Content[0].Text, "done GET /me") {
		t.Fatalf("a read: %+v", res)
	}
	hub.mu.Lock()
	hub.resolve = func(ix proto.Interaction) proto.Resolution {
		no := false
		return proto.Resolution{Allow: &no, Message: "not now"}
	}
	hub.mu.Unlock()
	if res := run("POST", "/conversations/oc_test/messages", `{"text":"hi"}`); !res.IsError || !strings.Contains(res.Content[0].Text, "not now") {
		t.Fatalf("a refused write: %+v", res)
	}
	hub.mu.Lock()
	asked := len(hub.asked)
	hub.mu.Unlock()
	if asked != 1 {
		t.Fatalf("%d cards", asked)
	}
	b, _ := os.ReadFile(ran)
	if got := strings.TrimSpace(string(b)); got != "GET /me" {
		t.Fatalf("ol ran %q", got)
	}
	// Not the assistant's conversation: the gated tool is not run here (it goes to the hub, which has no such tool).
	var res proto.ToolResult
	if err := host.peer.Call(context.Background(), "mcp.call", map[string]any{"name": lark-connectorTool, "args": map[string]any{"args": []string{"GET", "/other"}}, "session_id": "session-other", "assistant": true}, &res); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(ran); strings.Contains(string(b), "/other") {
		t.Fatal("the gated tool ran for a conversation that is not the assistant's")
	}
}

func toolList(ts []proto.Tool) string {
	var out []string
	for _, x := range ts {
		out = append(out, x.Name)
	}
	return strings.Join(out, ",")
}
