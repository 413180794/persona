package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// The settings are the assistant's: hub.json's (the hello) are their defaults, a saved machine the hub no longer has
// is not used, and the hub gets what its features need in every snapshot (the profile, with the conversations its
// nodes keep listing).
func TestSettingsAndTheProfile(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub().withMachine()
	f.hello.Assistant.Launcher, f.hello.OwnerNotes = "hub-l", "  hub.json 的备注 "
	s := newTestService(t, f)
	if v := s.settings(ctx); v.MachineID != "server" || v.Launcher != "hub-l" || s.notes(ctx) != "hub.json 的备注" || !s.mainTasksQuiet(ctx) {
		t.Fatalf("defaults: %+v %q", v, s.notes(ctx))
	}
	notes := "我自己写的"
	setSettings(t, s, func(v *Settings) {
		v.MachineID, v.Main, v.Notes, v.Timezone = "gone", "main-1", &notes, "Asia/Shanghai"
		v.Former = []FormerMain{{MachineID: "box", SessionID: "old-1"}}
	})
	if v := s.settings(ctx); v.MachineID != "server" || s.notes(ctx) != notes || s.location(ctx).String() != "Asia/Shanghai" {
		t.Fatalf("saved: %+v", v)
	}
	s.publish(ctx)
	p := f.lastSnapshot().Profile
	want := []SessionRef{{MachineID: "server", SessionID: "main-1"}, {MachineID: "box", SessionID: "old-1"}}
	if p == nil || p.MachineID != "server" || p.Main != "main-1" || p.Launcher != "hub-l" || p.Timezone != "Asia/Shanghai" ||
		!slices.Equal(p.Keep, want) {
		t.Fatalf("profile: %+v", p)
	}
	// The main conversation is 「Persona」 on the lists the apps get.
	if i := slices.IndexFunc(f.lastSnapshot().Sessions, func(v SessionView) bool { return v.SessionID == "main-1" }); i < 0 ||
		f.lastSnapshot().Sessions[i].Title != mainTitle {
		t.Fatalf("sessions: %+v", f.lastSnapshot().Sessions)
	}
}

// PUT /api/v1/assistant checks what it is given (a machine the hub has, a time zone, a launcher that runs Claude
// Code, Codex or an assistant runtime), puts a launcher into 模型配置 › 主对话 too, and answers the view.
func TestPutSettings(t *testing.T) {
	f := newFakeHub().withMachine()
	f.devices = []Device{{ID: "server", Name: "server", Kind: "local", Home: "/home/me"}}
	f.hello.Assistant.Machines = []string{"server"}
	s := newTestService(t, f)
	put := func(body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/assistant", strings.NewReader(body)))
		out := map[string]any{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	for _, bad := range []string{`{"machine_id":"nowhere"}`, `{"timezone":"Mars/Base"}`, `{"launcher":"gemini-l"}`} {
		if code, out := put(bad); code != http.StatusBadRequest {
			t.Fatalf("%s: %d %v", bad, code, out)
		}
	}
	code, out := put(`{"launcher":"codex-l","timezone":"Asia/Shanghai","main":"m1","main_tasks_quiet":false,"notes":"关于我"}`)
	if code != http.StatusOK || out["launcher"] != "codex-l" || out["timezone"] != "Asia/Shanghai" || out["main"] != "m1" ||
		out["main_tasks_quiet"] != false || out["notes_effective"] != "关于我" || out["cwd"] != "/home/me/.jarvis/assistant" {
		t.Fatalf("PUT: %d %v", code, out)
	}
	if f.record.Binding.Provider != "launcher.codex-l" {
		t.Fatalf("模型配置 › 主对话: %+v", f.record)
	}
	if p := f.lastSnapshot().Profile; p == nil || p.Main != "m1" || p.Timezone != "Asia/Shanghai" {
		t.Fatalf("the hub is told: %+v", p)
	}
	if code, out := put(`{"main":"-"}`); code != http.StatusOK || out["main"] != nil {
		t.Fatalf("clear main: %d %v", code, out)
	}
}

// A message for one of its conversations: one the machine does not list (and not the main one) keeps what it runs
// on; the main one runs on 主对话's, a gone launcher falls back to the assistant's, a dropped model to the launcher's
// default; the first message to a new main conversation started empty carries what the old one left, once.
func TestSendParams(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub().withMachine()
	f.launcher = "conv"
	f.setSessions("server", proto.Session{SessionID: "side", Launcher: "gone-l", Model: "m", Assistant: true},
		proto.Session{SessionID: "side2", Launcher: "side-l", Model: "dropped", Assistant: true})
	f.gone["gone-l"], f.gone["side-l/dropped"] = true, true
	s := newTestService(t, f)
	if p, err := s.SendParams(ctx, SendParams{MachineID: "server", SessionID: "unlisted", Params: map[string]any{"text": "hi"}}); err != nil ||
		len(p) != 1 {
		t.Fatalf("unlisted: %v %v", p, err)
	}
	p, err := s.SendParams(ctx, SendParams{MachineID: "server", SessionID: "side", Params: map[string]any{"text": "hi"}})
	if err != nil || p["launcher"] != "claude" || p["model"] != nil || p["model_fingerprint"] != "picked" {
		t.Fatalf("a gone launcher: %v %v", p, err)
	}
	p, err = s.SendParams(ctx, SendParams{MachineID: "server", SessionID: "side2", Params: map[string]any{"text": "hi"}})
	if err != nil || p["launcher"] != "side-l" || p["model"] != nil {
		t.Fatalf("a dropped model: %v %v", p, err)
	}

	// The main conversation put away, the next one started empty: its first message carries the old one's summary.
	setSettings(t, s, func(v *Settings) { v.Main = "main-0" })
	f.setSessions("server", proto.Session{SessionID: "main-0", Assistant: true, Status: proto.StatusIdle})
	if old, carried, err := s.newMain(ctx, false, "main-0"); err != nil || old != "main-0" || carried {
		t.Fatalf("new main: %q %v %v", old, carried, err)
	}
	sess, err := s.start(ctx, StartParams{Main: true})
	if err != nil || s.settings(ctx).Main != sess.SessionID || !slices.Contains(s.settings(ctx).Former, FormerMain{MachineID: "server", SessionID: "main-0",
		Until: s.settings(ctx).Former[0].Until}) {
		t.Fatalf("started: %+v %v %+v", sess, err, s.settings(ctx))
	}
	first, err := s.SendParams(ctx, SendParams{MachineID: "server", SessionID: sess.SessionID, Params: map[string]any{"text": "你好"}})
	if text, _ := first["text"].(string); err != nil || !strings.Contains(text, "main-0") || !strings.HasSuffix(text, "你好") || first["launcher"] != "conv" {
		t.Fatalf("the first message: %v %v", first, err)
	}
	if again, _ := s.SendParams(ctx, SendParams{MachineID: "server", SessionID: sess.SessionID, Params: map[string]any{"text": "再来"}}); again["text"] != "再来" {
		t.Fatalf("taken once: %v", again)
	}
}

// Renaming one of its conversations: not the main one (400), a Codex one only in the assistant's titles.
func TestRename(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub().withMachine()
	f.setSessions("server", proto.Session{SessionID: "cx", Agent: "codex", Assistant: true}, proto.Session{SessionID: "cl", Assistant: true})
	s := newTestService(t, f)
	setSettings(t, s, func(v *Settings) { v.Main = "main-1" })
	if _, err := s.Rename(ctx, RenameParams{MachineID: "server", SessionID: "main-1", Params: map[string]any{"title": "x"}}); err == nil ||
		!strings.Contains(err.Error(), "主对话") {
		t.Fatalf("main: %v", err)
	}
	if _, err := s.Rename(ctx, RenameParams{MachineID: "server", SessionID: "cx", Params: map[string]any{"title": "codex 的"}}); err != nil || f.sent() != 0 {
		t.Fatalf("codex: %v, %d node calls", err, f.sent())
	}
	if _, err := s.Rename(ctx, RenameParams{MachineID: "server", SessionID: "cl", Params: map[string]any{"title": "claude 的"}}); err != nil || f.sent() != 1 {
		t.Fatalf("claude: %v, %d node calls", err, f.sent())
	}
	s.publish(ctx)
	titles := map[string]string{}
	for _, v := range f.lastSnapshot().Sessions {
		titles[v.SessionID] = v.Title
	}
	if titles["cx"] != "codex 的" || titles["cl"] != "claude 的" {
		t.Fatalf("titles: %v", titles)
	}
}

// jarvis_settings: the list has the assistant's settings and the hub's; the assistant's own are read, and changed
// once the user approves on the phone (its own card); the hub's go to the hub.
func TestSettingsTool(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub().withMachine()
	f.launcherList = []proto.Launcher{{Name: "claude"}, {Name: "fast", Model: "m-fast"}}
	f.record = modelRecord{Revision: 1, Binding: ModelBinding{Provider: "launcher.claude", Model: "chosen", Effort: "high"}}
	s := newTestService(t, f)
	tool := func(args string) string {
		r := s.Tool(ctx, ToolCall{Name: "jarvis_settings", Arguments: json.RawMessage(args), MachineID: "server", SessionID: "c1"})
		return r.Content[0].Text
	}
	if out := tool(`{}`); !strings.Contains(out, "- assistant.launcher (Persona 的启动器)") || !strings.Contains(out, "- inbox.rules (收件箱分拣规则)") ||
		strings.Index(out, "assistant.notes") > strings.Index(out, "inbox.rules") {
		t.Fatalf("list:\n%s", out)
	}
	if out := tool(`{"action":"get","key":"inbox.rules"}`); !strings.HasPrefix(out, "the hub's: ") {
		t.Fatalf("a hub setting: %s", out)
	}
	answer := func(allow bool) {
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			if qs := s.pendingQuestions(); len(qs) == 1 {
				raw, _ := json.Marshal(proto.Resolution{Allow: &allow})
				if qs[0].Kind != proto.IxPermission || qs[0].ToolName != "jarvis_settings" || qs[0].SessionID != "c1" {
					t.Errorf("card: %+v", qs[0])
				}
				if _, err := s.Action(ctx, Action{Op: "answer", InteractionID: qs[0].ID, Answer: raw}); err != nil {
					t.Error(err)
				}
				return
			}
		}
		t.Error("no card on the phone")
	}
	go answer(false)
	if out := tool(`{"action":"set","key":"assistant.notes","value":"示例备注"}`); !strings.Contains(out, "declined") || s.notes(ctx) != "" {
		t.Fatalf("declined: %s", out)
	}
	go answer(true)
	if out := tool(`{"action":"set","key":"assistant.notes","value":"示例备注"}`); !strings.Contains(out, "updated") || s.notes(ctx) != "示例备注" {
		t.Fatalf("approved: %s", out)
	}
	if saved, _ := s.savedSettings(ctx); saved.Launcher != "" {
		t.Fatal("notes changed legacy model inheritance")
	}
	go answer(true)
	if out := tool(`{"action":"set","key":"assistant.launcher","value":"fast"}`); !strings.Contains(out, "fast") ||
		f.record.Binding != (ModelBinding{Provider: "launcher.fast", Model: "chosen", Effort: "high"}) {
		t.Fatalf("launcher: %s %+v", out, f.record)
	}
	if out := tool(`{"action":"set","key":"assistant.launcher","value":"nope"}`); !strings.Contains(out, "no launcher") {
		t.Fatalf("unknown launcher: %s", out)
	}
	if out := tool(`{"action":"get","key":"assistant.main_tasks_quiet"}`); out != "true" {
		t.Fatalf("quiet: %s", out)
	}
}
