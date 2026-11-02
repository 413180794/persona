package hub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/lark"
	"github.com/chyroc/jarvis/server/internal/proto"
)

func TestAskUserReturnsAnswers(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
	h.machines, h.byID["box"] = []*Machine{m}, m
	args := json.RawMessage(`{"questions":[{"question":"发吗？","options":[{"label":"发"},{"label":"不发"}]}]}`)
	done := make(chan proto.ToolResult, 1)
	go func() { done <- h.callAssistantTool(context.Background(), m, "conv-1", "ask_user", args) }()
	for len(h.pendingInteractions()) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	ix := h.pendingInteractions()[0]
	if ix.Kind != proto.IxQuestion || ix.SessionID != "conv-1" || ix.MachineID != "box" || len(ix.Questions) != 1 {
		t.Fatalf("unexpected interaction %+v", ix)
	}
	if err := h.resolve(ix.ID, proto.Resolution{Answers: map[string]string{"发吗？": "发"}}); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.IsError || !strings.Contains(textOf(r), "发吗？ → 发") {
		t.Fatalf("unexpected result %+v", r)
	}
}

func TestAgentsListAndFind(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
	h.machines, h.byID["box"] = []*Machine{m}, m
	now := time.Now().UTC()
	m.sessions = []proto.Session{
		{SessionID: "aaaaaaaa-1", Title: "修登录", Cwd: "/code/app", Status: proto.StatusWorking, UpdatedAt: now, LastText: "正在跑测试"},
		{SessionID: "bbbbbbbb-2", Title: "旧的", Cwd: "/code/old", Status: proto.StatusIdle, UpdatedAt: now.Add(-time.Hour)},
	}
	out := h.describeAgents("", "", "")
	if !strings.Contains(out, "修登录") || strings.Contains(out, "旧的") || !strings.Contains(out, "正在跑测试") {
		t.Fatalf("active list:\n%s", out)
	}
	if all := h.describeAgents("all", "", "old"); !strings.Contains(all, "旧的") || strings.Contains(all, "修登录") {
		t.Fatalf("query filter:\n%s", all)
	}
	if _, s, err := h.findAgent("", "aaaaaaaa"); err != nil || s.SessionID != "aaaaaaaa-1" {
		t.Fatalf("prefix lookup: %v %+v", err, s)
	}
	if _, _, err := h.findAgent("box", "cccccccc"); err == nil {
		t.Fatal("unknown id should fail")
	}
}

func TestAssistantToolsOnlyForAssistant(t *testing.T) {
	h := newTestHub(t)
	h.lark = lark.New(lark.Config{})
	h.connectors = connector.NewRegistry(nil)
	h.registerConnectors() // Feishu's read tools come from its connector
	for _, tool := range h.mcpTools(false) {
		if isAssistantTool(tool.Name) || strings.HasPrefix(tool.Name, "lark_") {
			t.Fatalf("%s leaked to ordinary sessions", tool.Name)
		}
	}
	names := map[string]bool{}
	for _, tool := range h.mcpTools(true) {
		names[tool.Name] = true
		var v any
		if err := json.Unmarshal(tool.InputSchema, &v); err != nil {
			t.Fatalf("%s schema: %v", tool.Name, err)
		}
	}
	for _, want := range []string{"ask_user", "notify_user", "schedule_followup", "agents_list", "agent_read", "lark_chats", "lark_search"} {
		if !names[want] {
			t.Fatalf("assistant is missing %s", want)
		}
	}
}

func TestCompactJSONPrunes(t *testing.T) {
	out := compactJSON(json.RawMessage(`{"a":{"avatarUrl":"x","name":"n","empty":"","no":false},"list":[{"thumbnail":"t","v":1}]}`), 1000)
	if strings.Contains(out, "avatar") || strings.Contains(out, "thumbnail") || strings.Contains(out, "empty") || !strings.Contains(out, `"name":"n"`) {
		t.Fatalf("got %s", out)
	}
}
