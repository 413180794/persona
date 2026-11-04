package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// Closing a finished task (docs/todos.md「自动完成」, #1092). Every test runs without a store (h.store nil: nothing
// reaches a database) and with a stubbed judge model.

// judgeStub answers the judge's calls with reply (status 200) or fails them (status ≠ 200), counting them and keeping
// the last input.
type judgeStub struct {
	calls atomic.Int32
	input atomic.Value
}

func stubJudge(t *testing.T, h *Hub, status int, reply string) *judgeStub {
	t.Helper()
	js := &judgeStub{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		js.input.Store(string(raw))
		js.calls.Add(1)
		if status != http.StatusOK {
			http.Error(w, "upstream down", status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text", "text": reply}}})
	}))
	t.Cleanup(srv.Close)
	key := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(key, []byte("k"), 0o600)
	// inbox.judge inherits 信息筛选 (inbox.triage) when nothing else is set, as in the inbox's own tests.
	h.triageAI = ai.New(ai.Config{BaseURL: srv.URL, APIKeyFile: key, Model: "m"})
	return js
}

// startedTask opens a read-only task from conv1 and waits for its agent (s1 on box) to be linked.
func startedTask(t *testing.T, h *Hub) Todo {
	t.Helper()
	if _, err := h.toolTaskPlan(context.Background(), "conv1", planArgs(true)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the link", func() bool { ts := taskTodos(h); return len(ts) == 1 && len(ts[0].Agents) == 1 })
	return taskTodos(h)[0]
}

// runTurn makes s1 work and then stop with text as its last words.
func runTurn(h *Hub, text string) {
	m := h.byID["box"]
	sess := func(status, text string) proto.Session {
		return proto.Session{SessionID: "s1", Cwd: "/code/app", Agent: "claude", Status: status, LastText: text, UpdatedAt: time.Now()}
	}
	m.setSessions([]proto.Session{sess(proto.StatusWorking, "")})
	time.Sleep(20 * time.Millisecond) // taskWorking runs in its own goroutine
	m.setSessions([]proto.Session{sess(proto.StatusDone, text)})
}

func receipts(h *Hub) int { return strings.Count(strings.Join(feedKinds(h, "conv1"), ","), "receipt") }

func todoNow(h *Hub, id string) Todo {
	h.todos.mu.RLock()
	defer h.todos.mu.RUnlock()
	return copyTodoLocked(h.todos.todos[id])
}

// The judge finds the work finished: the todo is done by the hub, with why, and works like one the user closed (the
// agent no longer counts for it, the snapshot says it was closed by the hub).
func TestTaskAutoDone(t *testing.T) {
	h, _ := newTaskHub(t)
	js := stubJudge(t, h, http.StatusOK, `{"verdict":"done","reason":"已合并 PR 并发了 TestFlight"}`)
	todo := startedTask(t, h)
	runTurn(h, "修好了，PR https://github.com/o/r/pull/901 已合并，TestFlight 构建号 202611041200")
	waitFor(t, "auto done", func() bool { return todoNow(h, todo.ID).DoneAt != nil })
	got := todoNow(h, todo.ID)
	if got.DoneBy != db.DoneByAuto || got.DoneReason != "已合并 PR 并发了 TestFlight" || receipts(h) != 1 {
		t.Fatalf("todo = %+v, %d receipts", got, receipts(h))
	}
	in, _ := js.input.Load().(string)
	for _, want := range []string{"修登录崩溃", "iPad 横屏时登录页崩溃", "pull/901", "已合并的 PR", "TestFlight 构建号"} {
		if !strings.Contains(in, want) {
			t.Errorf("judge input lacks %q: %s", want, in)
		}
	}
	h.todos.mu.RLock()
	_, active := h.todos.activeBySession[[2]string{"box", "s1"}]
	h.todos.mu.RUnlock()
	if active {
		t.Fatal("the agent still counts for a done todo")
	}
	if _, ok := h.taskOf("box", "s1"); ok {
		t.Fatal("a done task is still followed")
	}
	var label string
	for _, st := range h.snapshotTasks() {
		if st.ID == todo.ID {
			label = st.State
		}
	}
	if label != taskStateLabel("auto_done") {
		t.Fatalf("snapshot state = %q", label)
	}
	for _, it := range h.inboxTasks() {
		if it.ID == todo.ID {
			t.Fatal("the inbox still links messages to a done task")
		}
	}
}

// Agents that stopped without finishing, the model failing or missing, and a verdict it made up: the todo stays open.
func TestTaskNotAutoDone(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		reply  string
		last   string
	}{
		{"not finished", http.StatusOK, `{"verdict":"not_done","reason":"还差两处没改"}`, "没有全改完，剩下 iPad 的两处下次再说"},
		{"waiting", http.StatusOK, `{"verdict":"waiting","reason":"等 App 审核"}`, "已提交，等待审核"},
		{"needs the user", http.StatusOK, `{"verdict":"needs_user","reason":"要你选方案"}`, "两个方案你选一个"},
		{"made-up verdict", http.StatusOK, `{"verdict":"probably","reason":"大概好了"}`, "应该好了"},
		{"not JSON", http.StatusOK, `好像做完了`, "好了"},
		{"model down", http.StatusInternalServerError, "", "PR https://github.com/o/r/pull/9 已合并"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newTaskHub(t)
			js := stubJudge(t, h, tc.status, tc.reply)
			todo := startedTask(t, h)
			runTurn(h, tc.last)
			waitFor(t, "the judge asked", func() bool { return js.calls.Load() > 0 })
			time.Sleep(50 * time.Millisecond)
			if got := todoNow(h, todo.ID); got.DoneAt != nil || got.DoneBy != "" {
				t.Fatalf("closed: %+v", got)
			}
			if receipts(h) != 1 {
				t.Fatalf("%d receipts", receipts(h))
			}
		})
	}
	t.Run("no model", func(t *testing.T) {
		h, _ := newTaskHub(t) // no judge configured at all
		todo := startedTask(t, h)
		runTurn(h, "PR https://github.com/o/r/pull/9 已合并")
		waitFor(t, "a receipt", func() bool { return receipts(h) == 1 })
		time.Sleep(50 * time.Millisecond)
		if got := todoNow(h, todo.ID); got.DoneAt != nil {
			t.Fatalf("closed without a model: %+v", got)
		}
	})
}

// A todo the user reopened is not closed again by the same work; after a new turn it is judged again.
func TestTaskReopenedNotAutoDoneAgain(t *testing.T) {
	h, _ := newTaskHub(t)
	js := stubJudge(t, h, http.StatusOK, `{"verdict":"done","reason":"做完了"}`)
	todo := startedTask(t, h)
	runTurn(h, "做完了")
	waitFor(t, "auto done", func() bool { return todoNow(h, todo.ID).DoneAt != nil })

	handler := h.routes()
	patch := func(body string) Todo {
		req := todoRequest("PATCH", "/api/v1/todos/"+todo.ID, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var out struct {
			Todo Todo `json:"todo"`
		}
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("PATCH %s: %d %s", body, w.Code, w.Body.String())
		}
		return out.Todo
	}
	re := patch(`{"done":false}`)
	if re.DoneAt != nil || re.DoneBy != "" || re.DoneReason != "" || re.ReopenedAt == nil {
		t.Fatalf("reopened = %+v", re)
	}
	if _, ok := h.taskOf("box", "s1"); !ok {
		t.Fatal("a reopened task is not followed again")
	}

	// The same stretch of work, judged again (a late turn end, a receipt lost to a restart): left open, model not asked.
	calls := js.calls.Load()
	h.autoFinishTask(todoNow(h, todo.ID), re.ReopenedAt.Add(-time.Second), true)
	h.autoFinishTask(todoNow(h, todo.ID), time.Time{}, false)
	if got := todoNow(h, todo.ID); got.DoneAt != nil || js.calls.Load() != calls {
		t.Fatalf("closed again by the same work: %+v (%d calls)", got, js.calls.Load()-calls)
	}
	// A judgment that began before the reopen and ends after it does not overrule the user either.
	if ok, err := h.autoDoneTodo(context.Background(), todo.ID, "做完了", re.ReopenedAt.Add(-time.Second)); ok || err != nil {
		t.Fatalf("a stale judgment closed it: %v %v", ok, err)
	}

	// The user sends it on; the agent works and stops again: judged again, and closed.
	time.Sleep(5 * time.Millisecond)
	runTurn(h, "又改了一处，全部做完了")
	waitFor(t, "auto done again", func() bool { return todoNow(h, todo.ID).DoneAt != nil })
	if got := todoNow(h, todo.ID); got.DoneBy != db.DoneByAuto {
		t.Fatalf("todo = %+v", got)
	}

	// Done by the user: the hub's mark goes.
	patch(`{"done":false}`)
	if done := patch(`{"done":true}`); done.DoneAt == nil || done.DoneBy != "" || done.DoneReason != "" {
		t.Fatalf("done by the user = %+v", done)
	}
}

// Only tasks opened from a conversation are closed by the hub; done-check judges without writing.
func TestTaskDoneSkipAndCheck(t *testing.T) {
	now := time.Now()
	reopened := now.Add(-time.Minute)
	for _, tc := range []struct {
		t      Todo
		work   time.Time
		worked bool
		want   string
	}{
		{Todo{}, now, true, "not_a_task"},
		{Todo{Origin: "c", DoneAt: &now}, now, true, "done"},
		{Todo{Origin: "c", ReopenedAt: &reopened}, now.Add(-2 * time.Minute), true, "reopened"},
		{Todo{Origin: "c", ReopenedAt: &reopened}, time.Time{}, false, "reopened"},
		{Todo{Origin: "c", ReopenedAt: &reopened}, now, true, ""},
		{Todo{Origin: "c"}, time.Time{}, false, ""},
	} {
		if got := autoDoneSkip(tc.t, tc.work, tc.worked); got != tc.want {
			t.Errorf("autoDoneSkip(%+v, worked %v) = %q, want %q", tc.t, tc.worked, got, tc.want)
		}
	}

	h, _ := newTaskHub(t)
	stubJudge(t, h, http.StatusOK, `{"verdict":"waiting","reason":"等审核"}`)
	todo := startedTask(t, h)
	h.byID["box"].setSessions([]proto.Session{{SessionID: "s1", Cwd: "/code/app", Agent: "claude", Status: proto.StatusIdle,
		LastText: "已提交，等待审核", UpdatedAt: time.Now()}})
	handler := h.routes()
	check := func(id string) (int, taskDoneCheck) {
		req := todoRequest("POST", "/api/v1/todos/"+id+"/done-check", nil)
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var c taskDoneCheck
		_ = json.Unmarshal(w.Body.Bytes(), &c)
		return w.Code, c
	}
	code, c := check(todo.ID)
	if code != http.StatusOK || c.Verdict != verdictWaiting || c.Reason != "等审核" || c.WouldClose || c.Replies != 1 {
		t.Fatalf("done-check = %d %+v", code, c)
	}
	if got := todoNow(h, todo.ID); got.DoneAt != nil {
		t.Fatalf("done-check wrote: %+v", got)
	}
	if code, _ := check("todo_nope"); code != http.StatusNotFound {
		t.Fatalf("missing todo: %d", code)
	}

	// A todo made by hand: never closed by the hub, whatever the model says.
	stubJudge(t, h, http.StatusOK, `{"verdict":"done","reason":"做完了"}`)
	hand, err := h.createTodo(context.Background(), "自己记的事", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.linkTodoAgent(context.Background(), hand.ID, "box", "s2", "", "/code/app", "", ""); err != nil {
		t.Fatal(err)
	}
	h.autoFinishTask(todoNow(h, hand.ID), time.Now(), true)
	if got := todoNow(h, hand.ID); got.DoneAt != nil {
		t.Fatalf("a hand-made todo was closed: %+v", got)
	}
	if _, c := check(hand.ID); c.Skip != "not_a_task" || c.WouldClose {
		t.Fatalf("done-check on a hand-made todo = %+v", c)
	}
}

func TestDoneSignals(t *testing.T) {
	for _, tc := range []struct {
		text      string
		pr, build bool
	}{
		{"PR https://github.com/o/r/pull/12 已合并", true, false},
		{"提了 https://github.com/o/r/pull/12，等 review", false, false},
		{"merged https://github.com/o/r/pull/12; TestFlight build 202611041200 is up", true, true},
		{"发了 TestFlight，构建号 1234", false, true},
		{"没有全改完", false, false},
	} {
		got := doneSignals([]prompts.TaskDoneReply{{Text: tc.text}})
		if got.MergedPR != tc.pr || got.Build != tc.build {
			t.Errorf("doneSignals(%q) = %+v", tc.text, got)
		}
	}
	if s := clipMiddle(strings.Repeat("a", 10)+strings.Repeat("b", 10), 4); s != "aaaa\n…\nbbbb" {
		t.Fatalf("clipMiddle = %q", s)
	}
}
