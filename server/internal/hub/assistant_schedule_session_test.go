package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// assistantNode is the assistant's machine as runTask sees it: assistant.send with no session starts one (s1, s2…),
// with a session continues it, and with resume_only refuses one it doesn't have (cleaned up, never written).
type assistantNode struct {
	m     *Machine
	mu    sync.Mutex
	known map[string]bool
	made  int // conversations started, for the next id
	sends []map[string]any
	kept  []string     // the last node.configure keep_sessions
	items []proto.Item // session.items answers this (assistant_titles_test.go)
}

func newAssistantNode(t *testing.T, h *Hub) *assistantNode {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide := rpc.New("h", h2nW)
	nodeSide := rpc.New("n", n2hW)
	n := &assistantNode{known: map[string]bool{}}
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		var p map[string]any
		_ = json.Unmarshal(params, &p)
		n.mu.Lock()
		defer n.mu.Unlock()
		switch method {
		case "assistant.prepare":
			return map[string]any{"cwd": "/home/me/.jarvis/assistant"}, nil
		case "node.configure":
			n.kept = nil
			for _, k := range p["keep_sessions"].([]any) {
				n.kept = append(n.kept, k.(string))
			}
			return map[string]any{}, nil
		case "session.items":
			return map[string]any{"items": n.items}, nil
		case "session.rename":
			n.sends = append(n.sends, map[string]any{"rename": p})
			return map[string]any{"ok": true}, nil
		case "assistant.send":
			n.sends = append(n.sends, p)
			id, _ := p["session_id"].(string)
			if id == "" {
				n.made++
				id = fmt.Sprintf("s%d", n.made)
				n.known[id] = true
			} else if p["resume_only"] == true && !n.known[id] {
				return nil, fmt.Errorf("conversation %s is gone", id)
			}
			return map[string]any{"session_id": id}, nil
		}
		return nil, fmt.Errorf("unexpected %s", method)
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	n.m = newMachine(h, MachineConfig{ID: "server", Name: "server", Kind: "local"})
	n.m.peer = hubSide
	h.machines = append(h.machines, n.m)
	h.byID["server"] = n.m
	return n
}

func (n *assistantNode) lastSend(t *testing.T) map[string]any {
	t.Helper()
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.sends) == 0 {
		t.Fatal("nothing was sent")
	}
	return n.sends[len(n.sends)-1]
}

func (n *assistantNode) sent() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.sends)
}

func (n *assistantNode) forget(id string) {
	n.mu.Lock()
	delete(n.known, id)
	n.mu.Unlock()
}

// run is what runDue does for one due task: read it as it is now, then run it.
func runNow(t *testing.T, h *Hub, id string) (proto.Session, error) {
	t.Helper()
	task, ok := taskByID(h, id)
	if !ok {
		t.Fatalf("no task %s", id)
	}
	now := time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC)
	return h.runTask(task, now, now, false, false)
}

// The user's ask (2026-11-01): 「同一个定时任务，用同一个 session 跑」. The first run starts the task's own
// conversation; later runs go into it, told the earlier runs are above instead of being given the background again.
func TestATaskRunsInItsOwnConversation(t *testing.T) {
	h := newTestHub(t)
	n := newAssistantNode(t, h)
	setTasks(h, Task{ID: "r", Title: "早报", Prompt: "给我今天的早报", Repeat: repeatDaily, Time: "09:00", Enabled: true})

	first, err := runNow(t, h, "r")
	if err != nil {
		t.Fatal(err)
	}
	if p := n.lastSend(t); p["session_id"] != nil && p["session_id"] != "" || strings.Contains(p["text"].(string), "还是这个任务自己的对话") {
		t.Fatalf("the first run starts a conversation with the whole message: %v", p)
	}
	task, _ := taskByID(h, "r")
	if task.RunSessionID != first.SessionID || task.RunMachineID != "server" || task.RunLauncher != "claude" {
		t.Fatalf("the new conversation is the task's own: %+v", task)
	}

	second, err := runNow(t, h, "r")
	if err != nil {
		t.Fatal(err)
	}
	p := n.lastSend(t)
	if second.MachineID != "server" || second.SessionID != first.SessionID || p["session_id"] != first.SessionID || p["resume_only"] != true {
		t.Fatalf("the second run goes into the same conversation: %+v %v", second, p)
	}
	if text := p["text"].(string); !strings.Contains(text, "还是这个任务自己的对话") || !strings.Contains(text, "给我今天的早报") {
		t.Fatalf("a continued run says so and still has the task's prompt: %q", text)
	}
	runs := waitRuns(t, h, 2)
	if runs[0].SessionID != first.SessionID || runs[1].SessionID != first.SessionID || runs[1].Status != runOK {
		t.Fatalf("both runs are recorded in that conversation: %+v", runs)
	}
	if kept := h.keptSessions(context.Background(), "server"); !slices.Contains(kept, first.SessionID) {
		t.Fatalf("the node keeps listing the task's conversation however quiet: %v", kept)
	}
}

// A conversation that is gone (Claude Code cleans transcripts up after 30 days), one on another launcher, or one the
// user reset (下次开新对话) can't be continued: the run starts a new one and that becomes the task's.
func TestATaskStartsANewConversationWhenItsOwnCannotGoOn(t *testing.T) {
	h, _ := modelTestHub(t)
	n := newAssistantNode(t, h)
	setTasks(h, Task{ID: "p", Title: "巡检", Prompt: "巡检", Repeat: repeatInterval, EveryMinutes: 30, Enabled: true})
	first, _ := runNow(t, h, "p")

	n.forget(first.SessionID)
	gone, err := runNow(t, h, "p")
	if err != nil {
		t.Fatal(err)
	}
	if gone.SessionID == first.SessionID || gone.SessionID == "" {
		t.Fatalf("a gone conversation is replaced: %+v", gone)
	}
	if task, _ := taskByID(h, "p"); task.RunSessionID != gone.SessionID {
		t.Fatalf("and the new one is kept: %+v", task)
	}
	if text := n.lastSend(t)["text"].(string); !strings.Contains(text, "不用再用 schedule_followup") {
		t.Fatalf("a new conversation gets the whole message again: %q", text)
	}

	if err := h.saveModelRecord(context.Background(), "agent.new", 0, ModelBinding{Provider: "launcher.codex"}); err != nil {
		t.Fatal(err)
	}
	switched, _ := runNow(t, h, "p")
	if switched.SessionID == gone.SessionID {
		t.Fatal("a conversation started with claude isn't continued with codex")
	}
	if p := n.lastSend(t); p["session_id"] != nil && p["session_id"] != "" || p["launcher"] != "codex" {
		t.Fatalf("a new conversation with the new launcher: %v", p)
	}
	if task, _ := taskByID(h, "p"); task.RunSessionID != switched.SessionID || task.RunLauncher != "codex" {
		t.Fatalf("%+v", task)
	}

	// 下次开新对话 in the app.
	srv := httptest.NewServer(scheduleMux(h))
	defer srv.Close()
	res, err := http.Post(srv.URL+"/api/v1/assistant/tasks/p/fresh", "application/json", nil)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("fresh: %v %v", res, err)
	}
	var body struct{ Task Task }
	_ = json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	if body.Task.RunSessionID != "" {
		t.Fatalf("the task forgets its conversation: %+v", body.Task)
	}
	fresh, _ := runNow(t, h, "p")
	if fresh.SessionID == switched.SessionID {
		t.Fatal("the next run starts a new conversation")
	}
	if res, _ := http.Post(srv.URL+"/api/v1/assistant/tasks/nope/fresh", "application/json", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("an unknown task: %d", res.StatusCode)
	}
}

// A repeating task due while its last run is still going (a patrol stuck on an approval) is skipped and recorded, not
// stacked into the same conversation; a one-off still goes in, and 立即运行 says why it didn't.
func TestATaskDueWhileItsLastRunIsGoingIsSkipped(t *testing.T) {
	h := newTestHub(t)
	n := newAssistantNode(t, h)
	setTasks(h, Task{ID: "p", Title: "巡检", Prompt: "巡检", Repeat: repeatInterval, EveryMinutes: 30, Enabled: true},
		Task{ID: "f", Prompt: "看看部署", Repeat: repeatOnce, Enabled: true})
	first, _ := runNow(t, h, "p")
	n.m.mu.Lock()
	n.m.sessions = []proto.Session{{SessionID: first.SessionID, Status: proto.StatusWorking, State: proto.StateNeedsYou}}
	n.m.mu.Unlock()
	sends := n.sent()

	if _, err := runNow(t, h, "p"); !errors.Is(err, errTaskBusy) {
		t.Fatalf("err = %v", err)
	}
	if n.sent() != sends {
		t.Fatal("nothing is sent into a busy conversation")
	}
	runs := waitRuns(t, h, 2)
	if r := runs[1]; r.Status != runSkipped || r.Error != errTaskBusy.Error() || r.SessionID != first.SessionID {
		t.Fatalf("the skip is in the history: %+v", r)
	}
	if task, _ := taskByID(h, "p"); task.RunSessionID != first.SessionID || task.LastStatus != runSkipped {
		t.Fatalf("the task keeps its conversation: %+v", task)
	}

	srv := httptest.NewServer(scheduleMux(h))
	defer srv.Close()
	res, _ := http.Post(srv.URL+"/api/v1/assistant/tasks/p/run", "application/json", nil)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("立即运行 on a busy task: %d", res.StatusCode)
	}

	// A one-off that shares the conversation (rescheduled by its own run) still goes in, after the turn.
	s := h.loadSchedule(context.Background())
	s.mu.Lock()
	f := &s.tasks[s.indexLocked("f")]
	f.RunMachineID, f.RunSessionID, f.RunLauncher = "server", first.SessionID, "claude"
	s.mu.Unlock()
	if sess, err := runNow(t, h, "f"); err != nil || sess.SessionID != first.SessionID {
		t.Fatalf("a one-off is not skipped: %+v %v", sess, err)
	}

	// Two runs of one task at the same moment: the second one waits for nothing and is skipped.
	if !h.sched.begin("p") {
		t.Fatal("begin")
	}
	n.m.mu.Lock()
	n.m.sessions = nil
	n.m.mu.Unlock()
	if _, err := runNow(t, h, "p"); !errors.Is(err, errTaskBusy) {
		t.Fatalf("a run while the same task is being sent: %v", err)
	}
	h.sched.end("p")
	if _, err := runNow(t, h, "p"); err != nil {
		t.Fatal(err)
	}
}

// A task from a conversation (SessionID) whose conversation fails keeps the new one as its own: the next failure
// continues there instead of starting yet another one.
func TestATaskFromAConversationFallsBackToItsOwn(t *testing.T) {
	h := newTestHub(t)
	n := newAssistantNode(t, h)
	setTasks(h, Task{ID: "f", Prompt: "接着看", Repeat: repeatInterval, EveryMinutes: 60, Enabled: true, MachineID: "gone", SessionID: "origin"})
	first, _ := runNow(t, h, "f")
	if first.SessionID == "" || first.SessionID == "origin" {
		t.Fatalf("%+v", first)
	}
	again, _ := runNow(t, h, "f")
	if again.SessionID != first.SessionID || n.lastSend(t)["resume_only"] != true {
		t.Fatalf("the fallback conversation is continued: %+v", again)
	}
	if task, _ := taskByID(h, "f"); task.SessionID != "origin" {
		t.Fatalf("the conversation it came from is still tried first: %+v", task)
	}
}

func scheduleMux(h *Hub) *http.ServeMux {
	mux := http.NewServeMux()
	h.scheduleRoutes(mux)
	return mux
}

// A run the node accepts and whose turn then ends in an API error (503 / no such model) is failed, pushed and retried
// once — the node reports the send as accepted, so it used to stay "ok" and the user was never told.
func TestTaskTurnEndingInAPIErrorFailsAndRetriesOnce(t *testing.T) {
	h := newTestHub(t)
	n := newAssistantNode(t, h)
	old := runRetryDelay
	runRetryDelay = 50 * time.Millisecond
	t.Cleanup(func() { runRetryDelay = old })
	n.items = []proto.Item{{Role: "assistant", Kind: "text", Text: "API Error: 503 upstream attempt budget is exhausted"}}
	setTasks(h, Task{ID: "p", Title: "值守", Prompt: "go", Repeat: repeatInterval, EveryMinutes: 30, At: time.Now().Add(-time.Minute), Enabled: true, MachineID: "server", CreatedBy: "assistant"})

	// The turn runs, then ends: the watcher sees working and then idle.
	n.m.mu.Lock()
	n.m.sessions = []proto.Session{{SessionID: "s1", Status: proto.StatusWorking, State: proto.StateWorking, Assistant: true,
		Cwd: "/home/me/.jarvis/assistant"}}
	n.m.mu.Unlock()
	go func() {
		time.Sleep(3 * time.Second)
		n.m.mu.Lock()
		for i := range n.m.sessions {
			n.m.sessions[i].Status, n.m.sessions[i].State = proto.StatusOffline, proto.StateIdle
		}
		n.m.mu.Unlock()
	}()

	if _, err := runNow(t, h, "p"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		s := h.loadSchedule(context.Background())
		s.mu.Lock()
		failed := len(s.runs) > 0 && slices.ContainsFunc(s.runs, func(r ScheduleRun) bool {
			return r.RefID == "p" && r.Status == runFailed && strings.Contains(r.Error, "503")
		})
		s.mu.Unlock()
		if failed {
			// and the retry went, marked as such
			for time.Now().Before(deadline) {
				if n.sent() >= 2 {
					s.mu.Lock()
					retried := slices.ContainsFunc(s.runs, func(r ScheduleRun) bool { return r.RefID == "p" && r.Retried })
					s.mu.Unlock()
					if retried {
						return
					}
				}
				time.Sleep(50 * time.Millisecond)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the failed run was not recorded")
}

// A model error can end the turn before the first poll sees it working (a vanished model fails immediately): the
// changed last item alone must still end the watch, or the run stays "ok" for the whole timeout.
func TestTaskFailingBeforeTheWatcherSeesItWorking(t *testing.T) {
	h := newTestHub(t)
	n := newAssistantNode(t, h)
	old := runRetryDelay
	runRetryDelay = 50 * time.Millisecond
	t.Cleanup(func() { runRetryDelay = old })
	setTasks(h, Task{ID: "p", Title: "值守", Prompt: "go", Repeat: repeatInterval, EveryMinutes: 30, At: time.Now().Add(-time.Minute), Enabled: true, MachineID: "server", CreatedBy: "assistant"})
	n.items = []proto.Item{{ID: "before", Role: "assistant", Kind: "text", Text: "old"}}
	n.m.mu.Lock()
	n.m.sessions = []proto.Session{{SessionID: "s1", Status: proto.StatusOffline, State: proto.StateIdle, Assistant: true,
		Cwd: "/home/me/.jarvis/assistant"}}
	n.m.mu.Unlock()
	go func() {
		time.Sleep(1500 * time.Millisecond)
		n.mu.Lock()
		n.items = append(n.items, proto.Item{ID: "err", Role: "assistant", Kind: "text", Text: "There's an issue with the selected model (gone)."})
		n.mu.Unlock()
	}()
	if _, err := runNow(t, h, "p"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		s := h.loadSchedule(context.Background())
		s.mu.Lock()
		failed := slices.ContainsFunc(s.runs, func(r ScheduleRun) bool { return r.RefID == "p" && r.Status == runFailed })
		s.mu.Unlock()
		if failed {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the failure before the first working poll was not recorded")
}

// A follow-up the assistant scheduled for itself goes into its conversation on that conversation's launcher, not the
// Agents one (#1518): the main conversation on 模型配置 › 主对话 (even when it last ran on another launcher), another
// of its conversations on what it runs. On the Agents launcher (plain claude by default) a follow-up moved the main
// conversation off its launcher, and every message after stayed there.
func TestAFollowUpKeepsItsConversationsLauncher(t *testing.T) {
	h, _ := modelTestHub(t)
	n := newAssistantNode(t, h)
	ctx := context.Background()
	if err := h.store.PutSetting(ctx, "assistant", AssistantConfig{MachineID: "server", Main: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := h.saveModelRecord(ctx, "assistant", 0, ModelBinding{Provider: "launcher.conv", Effort: "high"}); err != nil {
		t.Fatal(err)
	}
	if err := h.saveModelRecord(ctx, "agent.new", 0, ModelBinding{Provider: "launcher.agents"}); err != nil {
		t.Fatal(err)
	}
	n.m.mu.Lock()
	n.m.sessions = []proto.Session{
		{SessionID: "main", Launcher: "claude", Model: "stale", Assistant: true, Status: proto.StatusOffline},
		{SessionID: "side", Launcher: "side-l", Model: "side-m", Assistant: true, Status: proto.StatusOffline},
	}
	n.m.mu.Unlock()
	setTasks(h,
		Task{ID: "fu_main", Prompt: "会前准备", Repeat: repeatOnce, Enabled: true, MachineID: "server", SessionID: "main", CreatedBy: "assistant"},
		Task{ID: "fu_side", Prompt: "再看一眼", Repeat: repeatOnce, Enabled: true, MachineID: "server", SessionID: "side", CreatedBy: "assistant"},
		Task{ID: "own", Prompt: "早报", Repeat: repeatDaily, Time: "09:00", Enabled: true})
	for _, c := range []struct{ task, session, launcher, model, effort string }{
		{"fu_main", "main", "conv", "", "high"},
		{"fu_side", "side", "side-l", "side-m", ""},
	} {
		got, err := runNow(t, h, c.task)
		if err != nil {
			t.Fatal(err)
		}
		p := n.lastSend(t)
		model, _ := p["model"].(string)
		effort, _ := p["effort"].(string)
		if got.SessionID != c.session || p["session_id"] != c.session || p["launcher"] != c.launcher || model != c.model || effort != c.effort {
			t.Errorf("%s: into %s with %v, want %s on %s %q %q", c.task, got.SessionID, p, c.session, c.launcher, c.model, c.effort)
		}
		if fp, _ := p["model_fingerprint"].(string); fp == "" {
			t.Errorf("%s: no model fingerprint: %v", c.task, p)
		}
	}
	// A task with no conversation of its own to go back to is an Agents task (#697): its own conversation, on that launcher.
	if _, err := runNow(t, h, "own"); err != nil {
		t.Fatal(err)
	}
	if p := n.lastSend(t); p["launcher"] != "agents" {
		t.Fatalf("a task's own conversation runs on the Agents launcher: %v", p)
	}
}

// What the node asks before it tells a conversation by itself that a restart lost its background tasks (#1554): the
// launcher and model a message from the phone runs on. The main conversation follows 模型配置 › 主对话, not the model it
// last ran (which left the phone's next message on another model than the running process, refused while it was busy).
func TestAssistantModelIsWhatThePhonesMessageRunsOn(t *testing.T) {
	h, _ := modelTestHub(t)
	n := newAssistantNode(t, h)
	ctx := context.Background()
	if err := h.store.PutSetting(ctx, "assistant", AssistantConfig{MachineID: "server", Main: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := h.saveModelRecord(ctx, "assistant", 0, ModelBinding{Provider: "launcher.conv", Effort: "high"}); err != nil {
		t.Fatal(err)
	}
	n.m.mu.Lock()
	n.m.sessions = []proto.Session{{SessionID: "main", Launcher: "conv", Model: "picked-once", Assistant: true, Status: proto.StatusIdle}}
	n.m.mu.Unlock()
	params, _ := json.Marshal(map[string]any{"session_id": "main"})
	got, err := n.m.onRequest(ctx, "assistant.model", params)
	if err != nil {
		t.Fatal(err)
	}
	phone := map[string]any{}
	if err := h.assistantSendModel(ctx, n.m, "main", phone); err != nil {
		t.Fatal(err)
	}
	p := got.(map[string]any)
	if p["launcher"] != "conv" || p["model"] != nil || p["effort"] != "high" || p["model_fingerprint"] == nil || p["model_fingerprint"] != phone["model_fingerprint"] {
		t.Fatalf("assistant.model = %v, the phone's message runs on %v", p, phone)
	}
	// Another conversation the hub doesn't list yet (the node just started) keeps its own: nothing to say.
	params, _ = json.Marshal(map[string]any{"session_id": "side"})
	if got, err := n.m.onRequest(ctx, "assistant.model", params); err != nil || len(got.(map[string]any)) != 0 {
		t.Fatalf("an unlisted conversation: %v %v", got, err)
	}
}
