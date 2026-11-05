package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The hub's end-to-end tests of tasks (real todos, a real node) are hub/tasks_test.go; these need the assistant's own
// state or timing.

// newTaskService is an assistant on a fake hub with machine box, whose task agents are followed at once.
func newTaskService(t *testing.T) (*Service, *fakeHub) {
	t.Helper()
	f := newFakeHub()
	f.sessions["box"] = nil
	f.settings = &Settings{Timezone: "Asia/Shanghai"}
	return newStorelessService(t, f), f
}

func planArgs(readonly bool) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"tasks": []map[string]any{{
		"title": "修登录崩溃", "prompt": "iPad 横屏时登录页崩溃，修好并补测试", "machine": "box", "cwd": "/code/app",
		"readonly": readonly, "why": "这个仓库最近在 box 上", "permission": "acceptEdits",
	}}})
	return raw
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func feedKinds(s *Service, conv string) string {
	items, _, _ := s.feed(context.Background(), conv, time.Time{})
	var out []string
	for _, it := range items {
		out = append(out, it.Kind)
	}
	return strings.Join(out, ",")
}

// startedTask opens a read-only task from conv1 and waits for its agent (s1 on box) to be linked.
func startedTask(t *testing.T, s *Service, f *fakeHub) Todo {
	t.Helper()
	if _, err := s.toolTaskPlan(context.Background(), "conv1", planArgs(true)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the link", func() bool { return len(onlyTodo(s).Agents) == 1 })
	return onlyTodo(s)
}

// A dropped link is retried once: the machine back by then, the task starts as usual; still down, the task ends.
func TestTaskStartRetriesADroppedLink(t *testing.T) {
	old := taskStartRetryDelay
	taskStartRetryDelay = 300 * time.Millisecond
	t.Cleanup(func() { taskStartRetryDelay = old })

	s, f := newTaskService(t)
	f.offline["box"] = true // offline when the task starts
	go func() {
		time.Sleep(100 * time.Millisecond)
		f.mu.Lock()
		f.offline["box"] = false
		f.mu.Unlock()
	}()
	if _, err := s.toolTaskPlan(context.Background(), "conv1", planArgs(true)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the retried start to link", func() bool { return len(onlyTodo(s).Agents) == 1 })
	if todo := onlyTodo(s); todo.DoneAt != nil || todo.StartError != "" {
		t.Fatalf("todo = %+v", todo)
	}
	if len(f.starts) != 1 {
		t.Fatalf("%d starts", len(f.starts))
	}

	s2, f2 := newTaskService(t)
	f2.offline["box"] = true // and it stays down
	if _, err := s2.toolTaskPlan(context.Background(), "conv1", planArgs(true)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the todo to end", func() bool { return onlyTodo(s2).DoneAt != nil })
	if todo := onlyTodo(s2); !strings.Contains(todo.StartError, "offline") || len(todo.Agents) != 0 {
		t.Fatalf("todo = %+v", todo)
	}
	waitFor(t, "the receipt", func() bool { return strings.Contains(feedKinds(s2, "conv1"), "receipt") })
	if len(f2.starts) != 0 {
		t.Fatalf("%d starts reached a machine that was down", len(f2.starts))
	}
}

func TestTransientStartErr(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{rpc.ErrClosed, true},
		{fmt.Errorf("box: %w", apierr.ErrOffline), true},
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

// When the task's agent ends a turn and waits for nothing, the conversation gets one receipt with the agent's last
// words and its PR; the next turn's end after more work gets another.
func TestTaskReceipt(t *testing.T) {
	s, f := newTaskService(t)
	todo := startedTask(t, s, f)
	ctx := context.Background()
	turn := func(status, text string) {
		f.setSessions("box", proto.Session{SessionID: "s1", MachineID: "box", Cwd: "/code/app", Agent: "claude", Status: status, LastText: text,
			UpdatedAt: time.Now()})
		if status == proto.StatusWorking {
			s.taskWorking(ctx, "box", "s1")
		} else {
			s.taskTurnEnded(ctx, "box", "s1")
		}
	}
	turn(proto.StatusWorking, "")
	turn(proto.StatusDone, "修好了，提了 https://github.com/o/r/pull/901")
	items, _, _ := s.feed(ctx, "conv1", time.Time{})
	var rb receiptBody
	_ = json.Unmarshal(items[len(items)-1].Body, &rb)
	if items[len(items)-1].Kind != feedReceipt || rb.State != "done" || rb.PR != "https://github.com/o/r/pull/901" || rb.Title != "修登录崩溃" {
		t.Fatalf("receipt = %+v", rb)
	}
	// The same stillness again is no new receipt.
	turn(proto.StatusIdle, "修好了")
	if n := strings.Count(feedKinds(s, "conv1"), "receipt"); n != 1 {
		t.Fatalf("%d receipts", n)
	}
	time.Sleep(5 * time.Millisecond)
	turn(proto.StatusWorking, "")
	s.tasks.mu.Lock()
	noted := s.tasks.working[todo.ID].After(s.tasks.receipted[todo.ID])
	s.tasks.mu.Unlock()
	if !noted {
		t.Fatal("the new work is not noted")
	}
	turn(proto.StatusDone, "又改了一处")
	if n := strings.Count(feedKinds(s, "conv1"), "receipt"); n != 2 {
		t.Fatalf("%d receipts after more work", n)
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
	s, f := newTaskService(t)
	todo := startedTask(t, s, f)
	ctx := context.Background()
	f.setSessions("box", proto.Session{SessionID: "s1", MachineID: "box", Cwd: "/code/app", Agent: "claude", Status: proto.StatusBlocked,
		State: proto.StateNeedsYou, PendingInteractions: 1, UpdatedAt: time.Now()})
	if _, err := s.appendFeed(ctx, "conv1", feedReceipt, "todo_old", receiptBody{Title: "夜里的活", State: "done"}); err != nil {
		t.Fatal(err)
	}
	it, made, err := s.ensureToday(ctx, "conv1")
	if err != nil || !made || it.Kind != feedToday {
		t.Fatalf("today = %+v made=%v err=%v", it, made, err)
	}
	var body todayBody
	_ = json.Unmarshal(it.Body, &body)
	if len(body.Done) != 1 || body.Done[0].Title != "夜里的活" || body.Date != it.Ref {
		t.Fatalf("body = %+v", body)
	}
	if len(body.Waiting) != 1 || body.Waiting[0].ID != todo.ID {
		t.Fatalf("waiting = %+v (state %s)", body.Waiting, taskState(todo, s.taskAgents(ctx, todo)))
	}
	loc := s.location(ctx)
	now := time.Now().In(loc)
	if want := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc); !it.At.Equal(want) {
		t.Fatalf("at %v, want the start of the day %v", it.At, want)
	}
	again, made, err := s.ensureToday(ctx, "conv1")
	if err != nil || made || again.ID != it.ID {
		t.Fatalf("second call made another: %+v made=%v err=%v", again, made, err)
	}
}

// Kinds remembered with 这类以后直接开 are listed and can be forgotten (Postgres: on the hub machine).
func TestTaskTrust(t *testing.T) {
	url := os.Getenv("JARVIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("JARVIS_TEST_DATABASE_URL not set (scripts/test/test-server.sh runs this on the hub machine)")
	}
	database, err := db.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.PutSetting(context.Background(), trustSetting, nil); err != nil {
		t.Fatal(err)
	}
	h := newServiceOn(t, newFakeHub(), database)
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

// A todo the user reopened is not closed again by the work before it, nor when that is not known (a receipt lost to
// a restart); a todo made by hand is never closed by the judge: in neither is the model asked.
func TestAutoFinishLeavesAReopenedTask(t *testing.T) {
	s, f := newTaskService(t)
	f.judge = `{"verdict":"done","reason":"做完了"}`
	reopened := time.Now().Add(-time.Minute)
	putTodo(s, Todo{ID: "todo_r", Title: "修", Origin: "conv1", ReopenedAt: &reopened, CreatedAt: reopened.Add(-time.Hour),
		Agents: []TodoAgent{{MachineID: "box", SessionID: "s9"}}})
	f.setItems(proto.Item{Kind: proto.KindText, Role: "assistant", Text: "全部做完了，PR 已合并"})
	putTodo(s, Todo{ID: "todo_h", Title: "自己记的事", CreatedAt: reopened})
	s.autoFinishTask(todoNow(s, "todo_r"), reopened.Add(-time.Second), true)
	s.autoFinishTask(todoNow(s, "todo_r"), time.Time{}, false)
	s.autoFinishTask(todoNow(s, "todo_h"), time.Now(), true)
	if todoNow(s, "todo_r").DoneAt != nil || todoNow(s, "todo_h").DoneAt != nil || f.judged != 0 {
		t.Fatalf("closed: %+v %+v, %d judgments", todoNow(s, "todo_r"), todoNow(s, "todo_h"), f.judged)
	}
	// Work after the reopen is judged, and closes it.
	s.autoFinishTask(todoNow(s, "todo_r"), time.Now(), true)
	if got := todoNow(s, "todo_r"); got.DoneAt == nil || got.DoneBy != db.DoneByAuto || f.judged != 1 {
		t.Fatalf("not closed after new work: %+v, %d judgments", got, f.judged)
	}
}

// A task's line in the snapshot follows its agent as the cards do (#1583): the step of one that has only run tools
// and said nothing, its words and step while it works, its words once it stopped, and its running subagents on top
// of its own progress, never instead of it.
func TestSnapshotTaskFollowsTheAgent(t *testing.T) {
	now := time.Date(2026, 11, 4, 11, 52, 0, 0, time.UTC)
	todo := Todo{ID: "todo_1", Title: "hub 重启无感"}
	started := now.Add(-3*time.Minute - 10*time.Second)
	lead := proto.Session{MachineID: "mac-a", SessionID: "b00502b8-82a3", State: proto.StateWorking, UpdatedAt: now,
		Activity: "在跑测试", ActivityAt: &started}

	got := snapshotTask(todo, "working", []proto.Session{lead}, now)
	if got.Last != "" || got.Doing != "在跑测试" || got.DoingFor != "3 分钟" || got.Session != "b00502b8" {
		t.Fatalf("tools only, no words: %+v", got)
	}

	lead.LastText = "已建 issue 并认领，\n两个只读子任务在调查"
	lead.Subagents = []proto.Subagent{
		{ID: "a1", Description: "Hub children lifecycle survey", Running: true, LastText: "子任务的话"},
		{ID: "a2", Description: "Client reconnect survey", Running: true},
		{ID: "a3", Description: "早就做完的", Running: false},
	}
	got = snapshotTask(todo, "working", []proto.Session{lead}, now)
	if got.Last != "已建 issue 并认领， 两个只读子任务在调查" || got.Doing != "在跑测试" ||
		!reflect.DeepEqual(got.Subagents, []string{"Hub children lifecycle survey", "Client reconnect survey"}) {
		t.Fatalf("with subagents: %+v", got)
	}
	golden(t, "assistant_snapshot.tasks", prompts.RenderSnapshot(prompts.Snapshot{Now: now, Zone: "UTC",
		Tasks: []prompts.SnapshotTask{
			snapshotTask(Todo{ID: "todo_2", Title: "只跑工具"}, "working", []proto.Session{{MachineID: "mac-a",
				SessionID: "c1", State: proto.StateWorking, UpdatedAt: now, Activity: "在读 tasks.go", ActivityAt: &started}}, now),
			got,
		}}))
	linked := got
	linked.Link = todoLink("https://hub.example.com", "todo_1")
	golden(t, "assistant_snapshot.links", prompts.RenderSnapshot(prompts.Snapshot{Now: now, Zone: "UTC",
		Needs: []prompts.SnapshotAgent{{Label: "修 CI", Machine: "mac-a", Session: "b00502b8", Line: "要你批准推送",
			Link: sessionLink("https://hub.example.com", "mac-a", "b00502b8-82a3", false)}},
		Tasks: []prompts.SnapshotTask{linked}}))

	lead.State, lead.Subagents = proto.StateUnread, nil
	if got = snapshotTask(todo, "unread", []proto.Session{lead}, now); got.Doing != "" || got.DoingFor != "" || got.Last == "" {
		t.Fatalf("stopped: its words, not the step it stopped on: %+v", got)
	}
}

// The snapshot's links are the web app's own routes (web/src/lib/router.ts), on the hub's public_url; none without
// one (#2194).
func TestSnapshotLinks(t *testing.T) {
	web := "https://hub.example.com"
	for _, c := range []struct{ got, want string }{
		{sessionLink(web, "mac-a", "b00502b8-82a3", false), web + "/s/mac-a/b00502b8-82a3"},
		{sessionLink(web, "mac a", "x/y", false), web + "/s/mac%20a/x%2Fy"},
		{sessionLink(web, "mac-a", "c1", true), web + "/j/c1"},
		{todoLink(web, "todo_1"), web + "/todos?id=todo_1"},
		{todoLink(web, "a&b"), web + "/todos?id=a%26b"},
		{sessionLink("", "mac-a", "c1", false), ""},
		{todoLink("", "todo_1"), ""},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
