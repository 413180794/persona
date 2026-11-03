package hub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/lark"
	"github.com/nwverse/persona/server/internal/proto"
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

// The assistant's instructions said 「就是你所在的这台」 only of the hub's own machine and gave everything else an
// `ssh <alias>` command, but the aliases are the hub machine's and a link machine has no ssh: an assistant on a laptop
// was told to ssh into itself through a name only the hub knows (2026-10-31).
func TestAssistantPromptUsesReportedStorageRoots(t *testing.T) {
	h := newTestHub(t)
	h.cfg.dir = "/var/lib/hub state"
	h.cfg.Assistant.Machine = "server"
	m := newMachine(h, MachineConfig{ID: "server", Name: "服务器", Kind: "local"})
	m.info = proto.NodeInfo{Home: "/home/example", DataDir: "/var/lib/assistant-state"}
	h.machines = append(h.machines, m)
	h.byID["server"] = m
	text := h.assistantClaudeMD(context.Background(), false)
	for _, want := range []string{"/var/lib/assistant-state/assistant", "/var/lib/assistant-state/skills/lark-connector", "/var/lib/hub state/data"} {
		if !strings.Contains(text, want) {
			t.Fatalf("reported storage root missing: %s", want)
		}
	}
	if strings.Contains(text, "~/.jarvis/") || strings.Contains(text, "~/.persona/") {
		t.Fatal("hardcoded default root overrides reported storage roots")
	}
	if !strings.Contains(text, "也可以用文件工具直接读取此目录") {
		t.Fatal("a local assistant was not allowed to read its local knowledge directory")
	}
	m.cfg.Kind = "link"
	text = h.assistantClaudeMD(context.Background(), false)
	if strings.Contains(text, "也可以用文件工具直接读取此目录") || !strings.Contains(text, "通过 `kb_*` 工具读取") {
		t.Fatal("a remote assistant was told to read the hub's local files directly")
	}
}

func TestAssistantMachinesFromWhereItRuns(t *testing.T) {
	machines := []MachineConfig{
		{ID: "server", Name: "服务器", Kind: "local"},
		{ID: "box", Name: "台式机", Kind: "ssh", SSHHost: "box-alias"},
		{ID: "laptop", Name: "笔记本", Kind: "link"},
	}
	lines := func(at string) map[string]string {
		h := newTestHub(t)
		h.cfg.Assistant.Machine = at
		for _, mc := range machines {
			m := newMachine(h, mc)
			h.machines = append(h.machines, m)
			h.byID[mc.ID] = m
		}
		out := map[string]string{}
		for _, line := range strings.Split(h.assistantClaudeMD(context.Background(), false), "\n") {
			for _, mc := range machines {
				if strings.HasPrefix(line, "  - "+mc.Name) {
					out[mc.ID] = line
				}
			}
		}
		if len(out) != len(machines) {
			t.Fatalf("the instructions leave out machines: %v", out)
		}
		return out
	}
	here, ssh, noCommand := "就是你所在的这台机器", "`ssh box-alias '<命令>'`", "不能直接在它上面执行命令"
	for _, tc := range []struct {
		at   string
		want map[string]string
	}{
		{"server", map[string]string{"server": here, "box": ssh, "laptop": noCommand}},
		{"box", map[string]string{"server": noCommand, "box": here, "laptop": noCommand}},
		{"laptop", map[string]string{"server": noCommand, "box": noCommand, "laptop": here}},
	} {
		got := lines(tc.at)
		for id, want := range tc.want {
			if !strings.Contains(got[id], want) {
				t.Errorf("assistant on %s, line for %s = %q, want %q", tc.at, id, got[id], want)
			}
			if tc.at != "server" && strings.Contains(got[id], "ssh ") {
				t.Errorf("assistant on %s is told to ssh: %q", tc.at, got[id])
			}
		}
	}
}
