package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// healthHub is a fake hub with one machine: the main conversation and another of the assistant's own, and two of the
// health space's (docs/health.md).
func healthHub() *fakeHub {
	f := claudeMDHub(Device{ID: "server", Name: "服务器", Kind: "local", Home: "/home/me"})
	f.sessions["server"] = []proto.Session{
		{SessionID: "main-0000-0000", Cwd: "/home/me/.persona/assistant", Assistant: true, Title: "Persona", LastText: "今天的会"},
		{SessionID: "work-0000-0000", Cwd: "/home/me/.persona/assistant", Assistant: true, Title: "1105 | 研究 | 工作的事", LastText: "工作"},
		{SessionID: "sleep-000-0000", Cwd: "/home/me/.persona/health", Assistant: true, Space: SpaceHealth, Title: "睡眠", LastText: "睡了 6 小时"},
		{SessionID: "steps-000-0000", Cwd: "/home/me/.persona/health", Assistant: true, Title: "步数", LastText: "8000 步"}, // Space from its directory
	}
	return f
}

func spaceTool(t *testing.T, s *Service, self, name, args string) (string, bool) {
	t.Helper()
	res := s.Tool(context.Background(), ToolCall{MachineID: "server", SessionID: self, Name: name, Arguments: json.RawMessage(args)})
	b, _ := json.Marshal(res)
	return string(b), res.IsError
}

// Each space lists only its own conversations, in the app's list and to the conversation tools.
func TestHealthSpaceIsolation(t *testing.T) {
	ctx := context.Background()
	f := healthHub()
	s := newTestService(t, f)
	setSettings(t, s, func(v *Settings) { v.MachineID, v.Main = "server", "main-0000-0000" })

	ids := func(v assistantView) []string {
		var out []string
		for _, c := range v.Conversations {
			out = append(out, c.SessionID)
		}
		return out
	}
	if got := strings.Join(ids(s.view(ctx, false)), ","); got != "main-0000-0000,work-0000-0000" {
		t.Fatalf("assistant's list: %s", got)
	}
	v := s.view(ctx, false, SpaceHealth)
	if got := strings.Join(ids(v), ","); got != "sleep-000-0000,steps-000-0000" {
		t.Fatalf("health list: %s", got)
	}
	if v.Cwd != "/home/me/.jarvis/health" {
		t.Fatalf("health cwd %q", v.Cwd)
	}

	// From the main conversation: the health ones are not there.
	out, _ := spaceTool(t, s, "main-0000-0000", "conversations_list", `{}`)
	if strings.Contains(out, "sleep") || strings.Contains(out, "steps") || !strings.Contains(out, "work-0000-0000") {
		t.Fatalf("main's conversations_list: %s", out)
	}
	if out, isErr := spaceTool(t, s, "main-0000-0000", "conversation_read", `{"id":"sleep-000-0000"}`); !isErr {
		t.Fatalf("main read a health conversation: %s", out)
	}
	if _, isErr := spaceTool(t, s, "main-0000-0000", "conversation_send", `{"id":"steps-000-0000","text":"hi"}`); !isErr {
		t.Fatal("main sent to a health conversation")
	}
	// From the health space: only its own.
	out, _ = spaceTool(t, s, "sleep-000-0000", "conversations_list", `{}`)
	if strings.Contains(out, "main-0000") || strings.Contains(out, "work-0000") || !strings.Contains(out, "steps-000-0000") {
		t.Fatalf("health's conversations_list: %s", out)
	}
	if _, isErr := spaceTool(t, s, "sleep-000-0000", "conversation_read", `{"id":"work-0000-0000"}`); !isErr {
		t.Fatal("health read the assistant's conversation")
	}
}

// A health conversation starts in the health space's directory with its own CLAUDE.md; the main conversation can't
// be one, and an unknown space is refused.
func TestHealthSpaceStart(t *testing.T) {
	ctx := context.Background()
	f := healthHub()
	s := newTestService(t, f)
	setSettings(t, s, func(v *Settings) { v.MachineID = "server" })
	sess, err := s.Start(ctx, StartParams{Text: "这周睡得怎么样", Space: SpaceHealth})
	if err != nil {
		t.Fatal(err)
	}
	if sess.Space != SpaceHealth {
		t.Fatalf("space %q", sess.Space)
	}
	p := f.prepared[len(f.prepared)-1]
	md, _ := p["claude_md"].(string)
	if p["dir"] != ".jarvis/health" || !strings.Contains(md, "# Persona 健康") || strings.Contains(md, "kb_search") {
		t.Fatalf("assistant.prepare: dir %v, CLAUDE.md %q", p["dir"], md)
	}
	if _, err := s.Start(ctx, StartParams{Text: "x", Space: SpaceHealth, Main: true}); err == nil {
		t.Fatal("a main conversation in the health space")
	}
	if _, err := s.Start(ctx, StartParams{Text: "x", Space: "work"}); err == nil {
		t.Fatal("an unknown space")
	}
	// The assistant's own still get theirs.
	if _, err := s.Start(ctx, StartParams{Text: "今天的会"}); err != nil {
		t.Fatal(err)
	}
	if p := f.prepared[len(f.prepared)-1]; p["dir"] != ".jarvis/assistant" {
		t.Fatalf("assistant's dir %v", p["dir"])
	}
}

func TestGoldenHealthClaudeMD(t *testing.T) {
	f := healthHub()
	f.hello.Owner = "示例用户"
	f.hello.OwnerNotes = "我每天跑步。"
	s := newTestService(t, f)
	golden(t, "health_claude_md", s.healthClaudeMD(context.Background(), "server"))
}

// A health conversation's follow-up and routine keep the health space, so a run that starts a conversation starts it
// there; kb_update is refused; and a conversation the machine doesn't list can't be taken for the assistant's own.
func TestHealthSpaceTasksAndRefusals(t *testing.T) {
	ctx := context.Background()
	f := healthHub()
	s := newTestService(t, f)
	setSettings(t, s, func(v *Settings) { v.MachineID, v.Main = "server", "main-0000-0000" })

	if out, isErr := spaceTool(t, s, "sleep-000-0000", "schedule_followup", `{"at":"1h","prompt":"看看昨晚睡得怎么样","new_conversation":true}`); isErr {
		t.Fatalf("schedule_followup: %s", out)
	}
	if out, isErr := spaceTool(t, s, "sleep-000-0000", "schedule_routine", `{"title":"睡眠周报","prompt":"总结这周睡眠","time":"09:00"}`); isErr {
		t.Fatalf("schedule_routine: %s", out)
	}
	if out, isErr := spaceTool(t, s, "work-0000-0000", "schedule_followup", `{"at":"1h","prompt":"看看 CI","new_conversation":true}`); isErr {
		t.Fatalf("schedule_followup from the assistant's own: %s", out)
	}
	health, own := 0, 0
	for _, task := range s.loadSchedule(ctx).tasks {
		switch {
		case task.Space == SpaceHealth:
			health++
		case task.Space == "" && task.CreatedBy == "assistant":
			own++
		}
	}
	if health != 2 || own != 1 {
		t.Fatalf("tasks in the health space %d, in the assistant's own %d", health, own)
	}

	if out, isErr := spaceTool(t, s, "sleep-000-0000", "kb_update", `{}`); !isErr || !strings.Contains(out, "knowledge base") {
		t.Fatalf("kb_update from the health space: %s", out)
	}
	// A caller the machine doesn't list is taken for the assistant's own: it never sees the health conversations.
	if out, _ := spaceTool(t, s, "gone-0000-0000", "conversations_list", `{}`); strings.Contains(out, "sleep-000") || strings.Contains(out, "steps-000") {
		t.Fatalf("an unlisted caller saw the health conversations: %s", out)
	}
}
