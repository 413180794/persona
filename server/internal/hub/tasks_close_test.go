package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Closing the agents of finished tasks (tasks_close.go).

// idleAgent is s1 as a finished agent in a pane the hub started, last active at.
func idleAgent(at time.Time) proto.Session {
	return proto.Session{SessionID: "s1", Cwd: "/code/app", Agent: "claude", Status: proto.StatusIdle, PaneID: "w1:p1",
		StartedByJarvis: true, UpdatedAt: at, LastText: "做完了"}
}

func retires(tn *taskNode) []string {
	tn.mu.Lock()
	defer tn.mu.Unlock()
	return append([]string(nil), tn.retires...)
}

// A done task's agent closes once it has been still long enough; one working, waiting for the user, running
// something in the background, or not running is left as it is.
func TestRetirable(t *testing.T) {
	now := time.Now()
	done := now.Add(-time.Hour)
	old := now.Add(-time.Hour)
	cases := []struct {
		name string
		edit func(*proto.Session)
		done time.Time
		want bool
	}{
		{"idle long enough", func(*proto.Session) {}, done, true},
		{"done, unread", func(s *proto.Session) { s.State = proto.StateUnread }, done, true},
		{"codex", func(s *proto.Session) { s.Agent = "codex" }, done, true},
		{"task done just now", func(*proto.Session) {}, now.Add(-time.Second), false},
		{"active just now", func(s *proto.Session) { s.UpdatedAt = now.Add(-time.Second) }, done, false},
		{"working", func(s *proto.Session) { s.Status = proto.StatusWorking }, done, false},
		{"blocked", func(s *proto.Session) { s.Status = proto.StatusBlocked }, done, false},
		{"unknown status", func(s *proto.Session) { s.Status = proto.StatusUnknown }, done, false},
		{"needs you", func(s *proto.Session) { s.State = proto.StateNeedsYou }, done, false},
		{"pending question", func(s *proto.Session) { s.PendingInteractions = 1 }, done, false},
		{"queued", func(s *proto.Session) { s.Queued = []string{"next"} }, done, false},
		{"background shell", func(s *proto.Session) { s.Background = []proto.BackgroundTask{{}} }, done, false},
		{"subagent running", func(s *proto.Session) { s.Subagents = []proto.Subagent{{Running: true}} }, done, false},
		{"subagent finished", func(s *proto.Session) { s.Subagents = []proto.Subagent{{}} }, done, true},
		{"loop", func(s *proto.Session) { s.Loop = &proto.LoopInfo{} }, done, false},
		{"serving a port", func(s *proto.Session) { s.Ports = []proto.Port{{}} }, done, false},
		{"offline", func(s *proto.Session) { s.Status, s.PaneID = proto.StatusOffline, "" }, done, false},
		{"starting", func(s *proto.Session) { s.Starting = true }, done, false},
		{"assistant", func(s *proto.Session) { s.Assistant = true }, done, false},
		{"another tool's", func(s *proto.Session) { s.SourceKind = "codex_app_server" }, done, false},
		{"not started by the hub", func(s *proto.Session) { s.StartedByJarvis = false }, done, false},
		{"a runtime's agent", func(s *proto.Session) { s.Agent = "dsh" }, done, false},
	}
	for _, c := range cases {
		s := idleAgent(old)
		c.edit(&s)
		if got := retirable(s, c.done, now); got != c.want {
			t.Errorf("%s: retirable = %v, want %v", c.name, got, c.want)
		}
	}
}

// A task the hub found done: its agent, still for long enough, is retired on its machine; the todo keeps it.
func TestDoneTaskAgentIsClosed(t *testing.T) {
	h, tn := newTaskHub(t)
	todo := startedTask(t, h)
	m := h.byID["box"]
	m.setSessions([]proto.Session{idleAgent(time.Now().Add(-time.Hour))})
	refused := taskCloseRefused{}

	h.closeDoneTaskAgents(context.Background(), refused)
	if got := retires(tn); len(got) != 0 {
		t.Fatalf("closed the agent of an open task: %v", got)
	}

	if ok, err := h.autoDoneTodo(context.Background(), todo.ID, "做完了", time.Time{}); err != nil || !ok {
		t.Fatalf("auto done: %v %v", ok, err)
	}
	h.closeDoneTaskAgents(context.Background(), refused) // done just now: not yet
	if got := retires(tn); len(got) != 0 {
		t.Fatalf("closed at once: %v", got)
	}
	old := taskCloseIdle
	taskCloseIdle = 0
	t.Cleanup(func() { taskCloseIdle = old })
	h.closeDoneTaskAgents(context.Background(), refused)
	if got := retires(tn); len(got) != 1 || got[0] != "s1" {
		t.Fatalf("retired %v", got)
	}
	if a := todoNow(h, todo.ID).Agents; len(a) != 1 || a[0].SessionID != "s1" || a[0].ReleasedAt != nil {
		t.Fatalf("the todo lost its agent: %+v", a)
	}

	// Closed, it is offline: not asked again.
	s := idleAgent(time.Now().Add(-time.Hour))
	s.Status, s.PaneID = proto.StatusOffline, ""
	m.setSessions([]proto.Session{s})
	h.closeDoneTaskAgents(context.Background(), refused)
	if got := retires(tn); len(got) != 1 {
		t.Fatalf("asked again: %v", got)
	}
}

// A node that will not close an agent (it cannot resume it, or is too old to know how) is not asked again every
// pass, only once the agent changed.
func TestRefusedRetireIsNotRepeated(t *testing.T) {
	h, tn := newTaskHub(t)
	todo := startedTask(t, h)
	old := taskCloseIdle
	taskCloseIdle = 0
	t.Cleanup(func() { taskCloseIdle = old })
	tn.mu.Lock()
	tn.retireErr = &rpc.Error{Code: "not_retirable", Message: "this engine cannot open it again"}
	tn.mu.Unlock()
	m := h.byID["box"]
	at := time.Now().Add(-time.Hour)
	m.setSessions([]proto.Session{idleAgent(at)})
	if ok, err := h.autoDoneTodo(context.Background(), todo.ID, "做完了", time.Time{}); err != nil || !ok {
		t.Fatalf("auto done: %v %v", ok, err)
	}
	refused := taskCloseRefused{}
	h.closeDoneTaskAgents(context.Background(), refused)
	h.closeDoneTaskAgents(context.Background(), refused)
	if got := retires(tn); len(got) != 1 {
		t.Fatalf("asked %d times", len(got))
	}
	m.setSessions([]proto.Session{idleAgent(at.Add(time.Minute))}) // it did something since
	h.closeDoneTaskAgents(context.Background(), refused)
	if got := retires(tn); len(got) != 2 {
		t.Fatalf("not asked again after a change: %d", len(got))
	}
}

// A message to a done task whose agent was closed goes to that agent (the node resumes it before typing).
func TestRouteToClosedTaskAgent(t *testing.T) {
	h, tn := newTaskHub(t)
	todo := startedTask(t, h)
	s := idleAgent(time.Now().Add(-time.Hour))
	s.Status, s.PaneID = proto.StatusOffline, ""
	h.byID["box"].setSessions([]proto.Session{s})
	if ok, err := h.autoDoneTodo(context.Background(), todo.ID, "做完了", time.Time{}); err != nil || !ok {
		t.Fatalf("auto done: %v %v", ok, err)
	}
	body, _ := json.Marshal(map[string]string{"todo_id": todo.ID, "text": "再补一个测试"})
	r := todoRequest("POST", "/api/v1/assistant/conversations/conv1/route", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	h.routes().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("route: %d %s", w.Code, w.Body)
	}
	_, sends := tn.calls()
	if len(sends) != 1 || sends[0]["session_id"] != "s1" || sends[0]["text"] != "再补一个测试" {
		t.Fatalf("sends = %v", sends)
	}
}
