package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// taskNode connects a fake node named id whose session.start answers a new session and records what it was asked.
type taskNode struct {
	mu     sync.Mutex
	starts []map[string]any
	sends  []map[string]any
	n      int
	// failStart: session.start answers this error instead of a session.
	failStart error
	// items: what session.items answers (the agent's latest turn).
	items []proto.Item
}

func (tn *taskNode) calls() (starts, sends []map[string]any) {
	tn.mu.Lock()
	defer tn.mu.Unlock()
	return append([]map[string]any(nil), tn.starts...), append([]map[string]any(nil), tn.sends...)
}

func connectTaskNode(t *testing.T, h *Hub, id string) *taskNode {
	t.Helper()
	tn := &taskNode{}
	m := newMachine(h, MachineConfig{ID: id, Name: id})
	h.byID[id] = m
	h.machines = append(h.machines, m)
	hubIn, nodeOut := io.Pipe()
	nodeIn, hubOut := io.Pipe()
	hubPeer, nodePeer := rpc.New("h", hubOut), rpc.New("n", nodeOut)
	nodePeer.OnRequest = func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		var p map[string]any
		_ = json.Unmarshal(raw, &p)
		tn.mu.Lock()
		defer tn.mu.Unlock()
		switch method {
		case "session.start":
			tn.starts = append(tn.starts, p)
			if tn.failStart != nil {
				return nil, tn.failStart
			}
			tn.n++
			sid := "s" + string(rune('0'+tn.n))
			return map[string]any{"session": proto.Session{SessionID: sid, Cwd: p["cwd"].(string), Agent: "claude"}}, nil
		case "session.send":
			tn.sends = append(tn.sends, p)
		case "session.items":
			return map[string]any{"items": tn.items}, nil
		}
		return map[string]any{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = hubPeer.Serve(ctx, hubIn) }()
	go func() { _ = nodePeer.Serve(ctx, nodeIn) }()
	m.mu.Lock()
	m.peer, m.online = hubPeer, true
	m.mu.Unlock()
	return tn
}

func newTaskHub(t *testing.T) (*Hub, *taskNode) {
	t.Helper()
	h := newTestHub(t)
	h.initTodos(context.Background())
	tn := connectTaskNode(t, h, "box")
	old := taskSettleDelay
	taskSettleDelay = func(NotifyConfig) time.Duration { return 0 }
	t.Cleanup(func() { taskSettleDelay = old })
	return h, tn
}

func planArgs(readonly bool) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"tasks": []map[string]any{{
		"title": "修登录崩溃", "prompt": "iPad 横屏时登录页崩溃，修好并补测试", "machine": "box", "cwd": "/code/app",
		"readonly": readonly, "why": "这个仓库最近在 box 上", "permission": "acceptEdits",
	}}})
	return raw
}

func feedKinds(h *Hub, conv string) []string {
	items, _, _ := h.feed(context.Background(), conv, time.Time{})
	var out []string
	for _, it := range items {
		out = append(out, it.Kind)
	}
	return out
}

func taskTodos(h *Hub) []Todo {
	h.todos.mu.RLock()
	defer h.todos.mu.RUnlock()
	var out []Todo
	for _, t := range h.todos.todos {
		out = append(out, copyTodoLocked(t))
	}
	return out
}

// A read-only plan starts at once: a todo that remembers the conversation, its agent started with the prompt, and
// the plan and the task in the conversation's feed.
func TestTaskPlanReadOnlyStartsAtOnce(t *testing.T) {
	h, tn := newTaskHub(t)
	res, err := h.toolTaskPlan(context.Background(), "conv1", planArgs(true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "Started 1 task") {
		t.Fatalf("result = %q", res)
	}
	waitFor(t, "the agent to start and link", func() bool {
		ts := taskTodos(h)
		return len(ts) == 1 && len(ts[0].Agents) == 1
	})
	todo := taskTodos(h)[0]
	if todo.Origin != "conv1" || todo.PlanID == "" || todo.Title != "修登录崩溃" || todo.Agents[0].SessionID != "s1" {
		t.Fatalf("todo = %+v", todo)
	}
	starts, _ := tn.calls()
	if len(starts) != 1 || starts[0]["prompt"] != "iPad 横屏时登录页崩溃，修好并补测试" || starts[0]["cwd"] != "/code/app" ||
		starts[0]["permission_mode"] != "acceptEdits" {
		t.Fatalf("session.start = %+v", starts)
	}
	if got := strings.Join(feedKinds(h, "conv1"), ","); got != "plan,task" {
		t.Fatalf("feed = %s", got)
	}
	p, _ := h.loadPlan(context.Background(), todo.PlanID)
	if p.State != planStarted || p.Tasks[0].TodoID != todo.ID {
		t.Fatalf("plan = %+v", p)
	}
}

// failedStart checks a task whose agent did not start: its todo is done with why and no agent, the snapshot and the
// conversation's live counts see it as ended (#1077), the plan keeps the error and the conversation has a failed receipt.
func failedStart(t *testing.T, h *Hub, want string) Todo {
	t.Helper()
	waitFor(t, "the todo to end", func() bool { ts := taskTodos(h); return len(ts) == 1 && ts[0].DoneAt != nil })
	todo := taskTodos(h)[0]
	if !strings.Contains(todo.StartError, want) || len(todo.Agents) != 0 {
		t.Fatalf("todo = %+v", todo)
	}
	if st := taskState(todo, h.taskAgents(todo)); st != "failed" {
		t.Fatalf("taskState = %q", st)
	}
	snap := h.snapshotTasks()
	if len(snap) != 1 || snap[0].State != "没开起来" {
		t.Fatalf("snapshot = %+v", snap)
	}
	if got := h.inboxTasks(); len(got) != 0 {
		t.Fatalf("the inbox still sees a failed task: %+v", got)
	}
	waitFor(t, "the receipt", func() bool { return strings.Contains(strings.Join(feedKinds(h, "conv1"), ","), "receipt") })
	items, _, _ := h.feed(context.Background(), "conv1", time.Time{})
	var rb receiptBody
	_ = json.Unmarshal(items[len(items)-1].Body, &rb)
	if rb.State != "failed" || !strings.Contains(rb.Summary, want) {
		t.Fatalf("receipt = %+v", rb)
	}
	waitFor(t, "the plan's error", func() bool {
		p, _ := h.loadPlan(context.Background(), todo.PlanID)
		return p != nil && strings.Contains(p.Tasks[0].Error, want)
	})
	return todo
}

// A task whose agent the node refuses to start ends at once (no retry): no todo stays open without an agent.
func TestTaskStartFailedEndsTheTodo(t *testing.T) {
	h, tn := newTaskHub(t)
	tn.failStart = errors.New("launcher not found")
	if _, err := h.toolTaskPlan(context.Background(), "conv1", planArgs(true)); err != nil {
		t.Fatal(err)
	}
	todo := failedStart(t, h, "launcher not found")
	if starts, _ := tn.calls(); len(starts) != 1 {
		t.Fatalf("a refused start was retried: %d starts", len(starts))
	}
	if _, ok := h.taskOf("box", "s1"); ok {
		t.Fatal("a failed task still follows an agent")
	}
	// Reopening it by hand clears the failure.
	mux := h.routes()
	r := todoRequest("PATCH", "/api/v1/todos/"+todo.ID, strings.NewReader(`{"done":false}`))
	r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("reopen: %d %s", w.Code, w.Body)
	}
	if got := taskTodos(h)[0]; got.DoneAt != nil || got.StartError != "" {
		t.Fatalf("reopened todo = %+v", got)
	}
}

// A dropped link is retried once: the machine back by then, the task starts as usual; still down, the task ends.
func TestTaskStartRetriesADroppedLink(t *testing.T) {
	old := taskStartRetryDelay
	taskStartRetryDelay = 300 * time.Millisecond
	t.Cleanup(func() { taskStartRetryDelay = old })

	h, tn := newTaskHub(t)
	m := h.byID["box"]
	m.mu.Lock()
	peer := m.peer
	m.peer = nil // offline when the task starts
	m.mu.Unlock()
	go func() {
		time.Sleep(100 * time.Millisecond)
		m.mu.Lock()
		m.peer = peer
		m.mu.Unlock()
	}()
	if _, err := h.toolTaskPlan(context.Background(), "conv1", planArgs(true)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the retried start to link", func() bool { ts := taskTodos(h); return len(ts) == 1 && len(ts[0].Agents) == 1 })
	if todo := taskTodos(h)[0]; todo.DoneAt != nil || todo.StartError != "" {
		t.Fatalf("todo = %+v", todo)
	}
	if starts, _ := tn.calls(); len(starts) != 1 {
		t.Fatalf("%d starts", len(starts))
	}

	h2, tn2 := newTaskHub(t)
	m2 := h2.byID["box"]
	m2.mu.Lock()
	m2.peer = nil // and it stays down
	m2.mu.Unlock()
	if _, err := h2.toolTaskPlan(context.Background(), "conv1", planArgs(true)); err != nil {
		t.Fatal(err)
	}
	failedStart(t, h2, "offline")
	if starts, _ := tn2.calls(); len(starts) != 0 {
		t.Fatalf("%d starts reached a machine that was down", len(starts))
	}
}

func TestTransientStartErr(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{rpc.ErrClosed, true},
		{fmt.Errorf("box: %w", errOffline), true},
		{fmt.Errorf("wrapped: %w", rpc.ErrClosed), true},
		{&rpc.Error{Message: "rpc: connection closed"}, true}, // a link further on dropped
		{&rpc.Error{Message: "session.start: no such launcher"}, false},
		{errors.New("launcher not found"), false},
	} {
		if got := transientStartErr(c.err); got != c.want {
			t.Errorf("transientStartErr(%v) = %v", c.err, got)
		}
	}
}

// A plan that edits waits for the user; starting it takes their edits once, a second start or a dismiss is refused.
func TestTaskPlanWaitsForTheUser(t *testing.T) {
	h, tn := newTaskHub(t)
	res, err := h.toolTaskPlan(context.Background(), "conv1", planArgs(false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "nothing has started") {
		t.Fatalf("result = %q", res)
	}
	if len(taskTodos(h)) != 0 {
		t.Fatal("a plan that edits started without the user")
	}
	items, plans, _ := h.feed(context.Background(), "conv1", time.Time{})
	if len(items) != 1 || items[0].Kind != feedPlan || plans[items[0].Ref] == nil || plans[items[0].Ref].State != planOpen {
		t.Fatalf("feed = %+v plans = %+v", items, plans)
	}
	id := items[0].Ref

	mux := h.routes()
	start := func(body string) *httptest.ResponseRecorder {
		r := todoRequest("POST", "/api/v1/task-plans/"+id+"/start", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := start(`{"tasks":[{"title":"修 iPad 横屏崩溃","model":"opus"}]}`); w.Code != http.StatusOK {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	waitFor(t, "the agent", func() bool { s, _ := tn.calls(); return len(s) == 1 })
	starts, _ := tn.calls()
	if starts[0]["model"] != "opus" {
		t.Fatalf("the edit did not reach the start: %+v", starts[0])
	}
	if ts := taskTodos(h); len(ts) != 1 || ts[0].Title != "修 iPad 横屏崩溃" {
		t.Fatalf("todos = %+v", ts)
	}
	if w := start(""); w.Code != http.StatusConflict {
		t.Fatalf("second start: %d", w.Code)
	}
	r := todoRequest("POST", "/api/v1/task-plans/"+id+"/dismiss", nil)
	r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("dismiss after start: %d", w.Code)
	}
}

// When the task's agent ends a turn and waits for nothing, the conversation gets one receipt with the agent's last
// words and its PR; the next turn's end after more work gets another.
func TestTaskReceipt(t *testing.T) {
	h, _ := newTaskHub(t)
	if _, err := h.toolTaskPlan(context.Background(), "conv1", planArgs(true)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the link", func() bool { ts := taskTodos(h); return len(ts) == 1 && len(ts[0].Agents) == 1 })
	m := h.byID["box"]
	sess := func(status, text string) proto.Session {
		return proto.Session{SessionID: "s1", Cwd: "/code/app", Agent: "claude", Status: status, LastText: text, UpdatedAt: time.Now()}
	}
	m.setSessions([]proto.Session{sess(proto.StatusWorking, "")})
	m.setSessions([]proto.Session{sess(proto.StatusDone, "修好了，提了 https://github.com/o/r/pull/901")})
	waitFor(t, "a receipt", func() bool { return strings.Contains(strings.Join(feedKinds(h, "conv1"), ","), "receipt") })
	items, _, _ := h.feed(context.Background(), "conv1", time.Time{})
	var rb receiptBody
	_ = json.Unmarshal(items[len(items)-1].Body, &rb)
	if rb.State != "done" || rb.PR != "https://github.com/o/r/pull/901" || rb.Title != "修登录崩溃" {
		t.Fatalf("receipt = %+v", rb)
	}
	// The same stillness again is no new receipt.
	m.setSessions([]proto.Session{sess(proto.StatusIdle, "修好了")})
	time.Sleep(100 * time.Millisecond)
	if n := strings.Count(strings.Join(feedKinds(h, "conv1"), ","), "receipt"); n != 1 {
		t.Fatalf("%d receipts", n)
	}
	time.Sleep(5 * time.Millisecond)
	m.setSessions([]proto.Session{sess(proto.StatusWorking, "")})
	waitFor(t, "work noted", func() bool {
		h.tasks.mu.Lock()
		defer h.tasks.mu.Unlock()
		return h.tasks.working[taskTodos(h)[0].ID].After(h.tasks.receipted[taskTodos(h)[0].ID])
	})
	m.setSessions([]proto.Session{sess(proto.StatusDone, "又改了一处")})
	waitFor(t, "a second receipt", func() bool { return strings.Count(strings.Join(feedKinds(h, "conv1"), ","), "receipt") == 2 })
}

// Answering a task's card leaves a line in the conversation; a message to a task goes to its agent.
func TestTaskAnsweredAndRouted(t *testing.T) {
	h, tn := newTaskHub(t)
	if _, err := h.toolTaskPlan(context.Background(), "conv1", planArgs(true)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the link", func() bool { ts := taskTodos(h); return len(ts) == 1 && len(ts[0].Agents) == 1 })
	todo := taskTodos(h)[0]
	h.byID["box"].setSessions([]proto.Session{{SessionID: "s1", Cwd: "/code/app", Agent: "claude", Status: proto.StatusWorking, UpdatedAt: time.Now()}})

	yes := true
	h.taskAnswered(proto.Interaction{MachineID: "box", SessionID: "s1", Kind: proto.IxPermission, ToolName: "Bash"},
		proto.Resolution{Allow: &yes, Always: true}, "resolved")
	items, _, _ := h.feed(context.Background(), "conv1", time.Time{})
	var ab answeredBody
	_ = json.Unmarshal(items[len(items)-1].Body, &ab)
	if items[len(items)-1].Kind != feedAnswered || ab.Question != "使用 Bash" || ab.Answer != "批准了 · 以后同类不再问" {
		t.Fatalf("answered = %+v %+v", items[len(items)-1], ab)
	}

	mux := h.routes()
	body, _ := json.Marshal(map[string]string{"todo_id": todo.ID, "text": "第 5 条也改了"})
	r := todoRequest("POST", "/api/v1/assistant/conversations/conv1/route", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("route: %d %s", w.Code, w.Body)
	}
	_, sends := tn.calls()
	if len(sends) != 1 || sends[0]["session_id"] != "s1" || sends[0]["text"] != "第 5 条也改了" {
		t.Fatalf("session.send = %+v", sends)
	}
	// Another conversation cannot reach this task.
	r = todoRequest("POST", "/api/v1/assistant/conversations/other/route", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("route from another conversation: %d", w.Code)
	}

	r = todoRequest("GET", "/api/v1/assistant/conversations/conv1/feed", nil)
	r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var got struct {
		Items []FeedItem
		Plans map[string]*TaskPlan
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	kinds := []string{}
	for _, it := range got.Items {
		kinds = append(kinds, it.Kind)
	}
	if strings.Join(kinds, ",") != "plan,task,answered,routed" || len(got.Plans) != 1 {
		t.Fatalf("feed = %v plans = %d", kinds, len(got.Plans))
	}

	snap := h.snapshotTasks()
	if len(snap) != 1 || snap[0].ID != todo.ID || snap[0].State != "在做" || snap[0].Session != "s1" {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestResolutionAnswer(t *testing.T) {
	yes, no := true, false
	q := proto.Interaction{Kind: proto.IxQuestion, Questions: []proto.Question{{Question: "怎么改？"}}}
	for _, c := range []struct {
		ix   proto.Interaction
		r    proto.Resolution
		want string
	}{
		{q, proto.Resolution{Answers: map[string]string{"怎么改？": "B"}}, "B"},
		{q, proto.Resolution{Dismiss: true}, "忽略了"},
		{proto.Interaction{Kind: proto.IxPlan}, proto.Resolution{Approve: &yes}, "批准了"},
		{proto.Interaction{Kind: proto.IxPermission}, proto.Resolution{Allow: &no, Message: "先别跑"}, "拒绝了：先别跑"},
	} {
		if got := resolutionAnswer(c.ix, c.r); got != c.want {
			t.Errorf("resolutionAnswer(%+v) = %q, want %q", c.r, got, c.want)
		}
	}
	if got := interactionQuestion(proto.Interaction{Kind: proto.IxPlan}); got != "批准计划" {
		t.Errorf("plan question = %q", got)
	}
}

// The 今天 card: one a day per conversation, at the start of the day, with what finished and what waits.
func TestTodayCard(t *testing.T) {
	h, _ := newTaskHub(t)
	if _, err := h.toolTaskPlan(context.Background(), "conv1", planArgs(true)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the link", func() bool { ts := taskTodos(h); return len(ts) == 1 && len(ts[0].Agents) == 1 })
	todo := taskTodos(h)[0]
	h.byID["box"].setSessions([]proto.Session{{SessionID: "s1", Cwd: "/code/app", Agent: "claude", Status: proto.StatusBlocked,
		State: proto.StateNeedsYou, PendingInteractions: 1, UpdatedAt: time.Now()}})
	if _, err := h.appendFeed(context.Background(), "conv1", feedReceipt, "todo_old", receiptBody{Title: "夜里的活", State: "done"}); err != nil {
		t.Fatal(err)
	}
	it, made, err := h.ensureToday(context.Background(), "conv1")
	if err != nil || !made || it.Kind != feedToday {
		t.Fatalf("today = %+v made=%v err=%v", it, made, err)
	}
	var body todayBody
	_ = json.Unmarshal(it.Body, &body)
	if len(body.Done) != 1 || body.Done[0].Title != "夜里的活" || body.Date != it.Ref {
		t.Fatalf("body = %+v", body)
	}
	if len(body.Waiting) != 1 || body.Waiting[0].ID != todo.ID {
		t.Fatalf("waiting = %+v (state %s)", body.Waiting, taskState(todo, h.taskAgents(todo)))
	}
	loc := h.userLocation()
	now := time.Now().In(loc)
	if want := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc); !it.At.Equal(want) {
		t.Fatalf("at %v, want the start of the day %v", it.At, want)
	}
	again, made, err := h.ensureToday(context.Background(), "conv1")
	if err != nil || made || again.ID != it.ID {
		t.Fatalf("second call made another: %+v made=%v err=%v", again, made, err)
	}
}

// Kinds remembered with 这类以后直接开 are listed and can be forgotten (Postgres: on the hub machine).
func TestTaskTrust(t *testing.T) {
	database := setupTestDB(t)
	h := newTestHub(t)
	h.store = database
	h.trustKinds(context.Background(), []PlanTask{{Machine: "box", Cwd: "/code/app"}, {Machine: "box", Cwd: "/code/docs", Readonly: true}})
	list := h.trustList(context.Background())
	if len(list) != 2 || list[0].Key != "box|/code/app|edit" || list[1].Readonly != true {
		t.Fatalf("list = %+v", list)
	}
	if !h.planStartsAtOnce(context.Background(), []PlanTask{{Machine: "box", Cwd: "/code/app"}}) {
		t.Fatal("a trusted kind should start at once")
	}
	if ok, err := h.forgetTrust(context.Background(), "box|/code/app|edit"); !ok || err != nil {
		t.Fatalf("forget: %v %v", ok, err)
	}
	if h.planStartsAtOnce(context.Background(), []PlanTask{{Machine: "box", Cwd: "/code/app"}}) {
		t.Fatal("a forgotten kind still starts at once")
	}
	if ok, _ := h.forgetTrust(context.Background(), "nope"); ok {
		t.Fatal("forgot a kind that was never remembered")
	}
}

// An interaction of a task's agent names the task when the hub sends it (the watch shows it).
func TestInteractionNamesItsTask(t *testing.T) {
	h, _ := newTaskHub(t)
	if _, err := h.toolTaskPlan(context.Background(), "conv1", planArgs(true)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the link", func() bool { ts := taskTodos(h); return len(ts) == 1 && len(ts[0].Agents) == 1 })
	h.mu.Lock()
	h.interactions["ix_t"] = &pending{ix: proto.Interaction{ID: "ix_t", MachineID: "box", SessionID: "s1", Kind: proto.IxPermission}}
	h.interactions["ix_o"] = &pending{ix: proto.Interaction{ID: "ix_o", MachineID: "box", SessionID: "other", Kind: proto.IxPermission}}
	h.mu.Unlock()
	got := map[string]string{}
	for _, ix := range h.pendingInteractions() {
		got[ix.ID] = ix.TaskTitle
	}
	if got["ix_t"] != "修登录崩溃" || got["ix_o"] != "" {
		t.Fatalf("task titles = %v", got)
	}
}
