package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// newQuietService is newTaskService with the main conversation conv1 (idle, on machine server), a short notice
// delay and 督办 messages (supervise.go) that come at once and often.
func newQuietService(t *testing.T) (*Service, *fakeHub) {
	t.Helper()
	old := quietNoticeDelay
	quietNoticeDelay = 30 * time.Millisecond
	t.Cleanup(func() { quietNoticeDelay = old })
	shortSupervise(t)
	s, f := newTaskService(t)
	f.hello.Assistant.MachineID = "server"
	setSettings(t, s, func(v *Settings) { v.Main = "conv1" })
	f.known["conv1"] = true
	f.setSessions("server", proto.Session{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle})
	return s, f
}

// notices are the texts sent into the main conversation.
func (f *fakeHub) notices() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, a := range f.sends {
		if a["session_id"] == "conv1" {
			text, _ := a["text"].(string)
			out = append(out, text)
		}
	}
	return out
}

// agentTurn sets the task agent s1 on box and tells the assistant its turn ended (or started).
func agentTurn(s *Service, f *fakeHub, status, text string, pending int) {
	ctx := context.Background()
	st := proto.StateUnread
	if pending > 0 {
		st = proto.StateNeedsYou
	}
	if status == proto.StatusWorking {
		st = proto.StateWorking
	}
	f.setSessions("box", proto.Session{SessionID: "s1", MachineID: "box", Cwd: "/code/app", Agent: "claude", Status: status, State: st,
		LastText: text, PendingInteractions: pending, UpdatedAt: time.Now()})
	if status == proto.StatusWorking {
		s.taskWorking(ctx, "box", "s1")
	} else {
		s.taskTurnEnded(ctx, "box", "s1")
	}
}

// A task the main conversation opens is quiet and supervised: when it is done, the main conversation gets one 督办
// message with what its agent said, as is (not wrapped like a message from another conversation), and the task waits
// for review (not closed); the receipt is there as always.
func TestQuietTaskTellsTheMainConversation(t *testing.T) {
	s, f := newQuietService(t)
	f.judge = `{"verdict":"done","reason":"修好了"}`
	todo := startedTask(t, s, f)
	if !todo.Quiet || todo.Supervision == nil || todo.Supervision.Supervisor != "conv1" {
		t.Fatalf("a task from the main conversation is not quiet and supervised: %+v", todo)
	}
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, "修好了，提了 https://github.com/o/r/pull/901", 0)
	waitFor(t, "the notice", func() bool { return len(f.notices()) == 1 })
	n := f.notices()[0]
	for _, want := range []string{"📋 督办", "任务「修登录崩溃」（" + todo.ID + "）：hub 判断做完了，等你验收（修好了）；在 box", "修好了，提了 https://github.com/o/r/pull/901", "不是用户发的"} {
		if !strings.Contains(n, want) {
			t.Errorf("notice lacks %q:\n%s", want, n)
		}
	}
	if strings.Contains(n, "来自对话") {
		t.Errorf("the notice went in as a relay:\n%s", n)
	}
	if !strings.Contains(feedKinds(s, "conv1"), "receipt") {
		t.Error("no receipt")
	}
	if now := todoNow(s, todo.ID); now.DoneAt != nil || now.Supervision.State != reviewPending {
		t.Fatalf("todo = %+v, %+v", now, now.Supervision)
	}
	time.Sleep(3 * quietNoticeDelay)
	if got := len(f.notices()); got != 1 {
		t.Fatalf("%d notices", got)
	}
}

// A question and the end of the work within the delay are one message: the question answered meanwhile is left out.
// A question answered before the message goes out is no message.
func TestQuietNoticesMerge(t *testing.T) {
	s, f := newQuietService(t)
	f.judge = `{"verdict":"done","reason":"做完了"}`
	startedTask(t, s, f)
	ix := proto.Interaction{ID: "ix1", MachineID: "box", SessionID: "s1", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "用哪个分支？", Options: []proto.QuestionOption{{Label: "main"}, {Label: "dev"}}}}}
	agentTurn(s, f, proto.StatusBlocked, "", 1)
	s.InteractionRaised(context.Background(), InteractionRaised{Interaction: ix})
	agentTurn(s, f, proto.StatusDone, "用 main 做完了", 0)
	waitFor(t, "the notice", func() bool { return len(f.notices()) == 1 })
	n := f.notices()[0]
	if !strings.Contains(n, "做完了，等你验收") || strings.Contains(n, "用哪个分支") || strings.Count(n, "任务「") != 1 {
		t.Fatalf("notice:\n%s", n)
	}
	time.Sleep(3 * quietNoticeDelay)
	if got := len(f.notices()); got != 1 {
		t.Fatalf("%d notices", got)
	}

	s2, f2 := newQuietService(t)
	startedTask(t, s2, f2)
	agentTurn(s2, f2, proto.StatusBlocked, "", 1)
	s2.InteractionRaised(context.Background(), InteractionRaised{Interaction: ix})
	agentTurn(s2, f2, proto.StatusWorking, "", 0) // answered on the watch: working again
	time.Sleep(4 * quietNoticeDelay)
	if got := f2.notices(); len(got) != 0 {
		t.Fatalf("a question answered meanwhile was told: %q", got)
	}

	s3, f3 := newQuietService(t)
	startedTask(t, s3, f3)
	agentTurn(s3, f3, proto.StatusBlocked, "", 1)
	s3.InteractionRaised(context.Background(), InteractionRaised{Interaction: ix})
	waitFor(t, "the question's notice", func() bool { return len(f3.notices()) == 1 })
	if n := f3.notices()[0]; !strings.Contains(n, "Agent 在等回答") || !strings.Contains(n, "用户没收到推送") ||
		!strings.Contains(n, "用哪个分支？（选项：main / dev）") {
		t.Fatalf("notice:\n%s", n)
	}
}

// While the main conversation runs, the notice waits behind its turn; a newer one about the same task takes its
// place in the queue instead of following it.
func TestQuietNoticeWaitsForTheTurn(t *testing.T) {
	s, f := newQuietService(t)
	f.judge = `{"verdict":"done","reason":"做完了"}`
	startedTask(t, s, f)
	busy := proto.Session{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusWorking}
	f.setSessions("server", busy)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, "第一次做完", 0)
	waitFor(t, "the notice to queue", func() bool { return s.queuedFor("conv1") == 1 })
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, "第二次做完", 0)
	time.Sleep(4 * superviseEvery) // the next message is due: it takes the queued one's place
	if n := s.queuedFor("conv1"); n != 1 {
		t.Fatalf("%d queued", n)
	}
	if q, _ := s.queuedRelays(); len(q["conv1"]) != 1 || q["conv1"][0] != "督办消息" {
		t.Fatalf("shown waiting: %q", q["conv1"])
	}
	if len(f.notices()) != 0 {
		t.Fatal("sent while the main conversation runs")
	}
	idle := busy
	idle.Status = proto.StatusIdle
	f.setSessions("server", idle)
	s.drainRelays("server", []proto.Session{idle})
	waitFor(t, "the queued notice", func() bool { return len(f.notices()) == 1 })
	if n := f.notices()[0]; !strings.Contains(n, "第二次做完") || strings.Contains(n, "第一次做完") || strings.Contains(n, "来自对话") {
		t.Fatalf("notice:\n%s", n)
	}
	if !s.supervising("conv1") {
		t.Fatal("the turn the message started is pushed")
	}
}

// Tasks from another conversation are neither quiet nor supervised: no messages (the hub pushes as always). With
// the setting off a task from the main conversation is not quiet (the hub pushes) but is still supervised.
func TestNotQuietTasksAreNotTold(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(*Settings)
		told bool
	}{
		{"side conversation", func(v *Settings) { v.Main = "conv-main" }, false},
		{"setting off", func(v *Settings) { v.MainTasksQuiet = new(false) }, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, f := newQuietService(t)
			f.judge = `{"verdict":"done","reason":"好了"}`
			setSettings(t, s, c.set)
			if todo := startedTask(t, s, f); todo.Quiet || (todo.Supervision != nil) != c.told {
				t.Fatalf("todo = %+v", todo)
			}
			agentTurn(s, f, proto.StatusWorking, "", 0)
			agentTurn(s, f, proto.StatusDone, "好了", 0)
			time.Sleep(4 * quietNoticeDelay)
			if told := len(f.notices()) > 0; told != c.told {
				t.Fatalf("told = %v: %q", told, f.notices())
			}
		})
	}
}

// A task whose agent does not start is told too.
func TestQuietTaskFailedStart(t *testing.T) {
	s, f := newQuietService(t)
	f.failStart = errLauncherGone
	if _, err := s.toolTaskPlan(context.Background(), "conv1", planArgs(true)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the notice", func() bool { return len(f.notices()) == 1 })
	if n := f.notices()[0]; !strings.Contains(n, "没开起来") || !strings.Contains(n, "no such launcher") {
		t.Fatalf("notice:\n%s", n)
	}
}

var errLauncherGone = errors.New("session.start: no such launcher")

// agent_start from the main conversation is a quiet task (card, link); from another conversation or with the setting
// off it stays an agent of its own.
func TestAgentStartFromTheMainConversation(t *testing.T) {
	args := json.RawMessage(`{"machine":"box","cwd":"/code/app","prompt":"看看 CI 为什么挂了"}`)
	s, _ := newQuietService(t)
	res, err := s.runTool(context.Background(), "server", "conv1", "agent_start", args)
	if err != nil {
		t.Fatal(err)
	}
	todo := onlyTodo(s)
	if !todo.Quiet || todo.Origin != "conv1" || len(todo.Agents) != 1 || todo.Agents[0].SessionID != "s1" || todo.Title != "看看 CI 为什么挂了" ||
		todo.Supervision == nil || todo.Supervision.Supervisor != "conv1" {
		t.Fatalf("todo = %+v", todo)
	}
	if !strings.Contains(res, "a task of this conversation") || feedKinds(s, "conv1") != "task" {
		t.Fatalf("result %q, feed %q", res, feedKinds(s, "conv1"))
	}

	for _, c := range []struct {
		conv string
		off  bool
	}{{"conv2", false}, {"conv1", true}} {
		s, _ := newQuietService(t)
		if c.off {
			setSettings(t, s, func(v *Settings) { v.MainTasksQuiet = new(false) })
		}
		if _, err := s.runTool(context.Background(), "server", c.conv, "agent_start", args); err != nil {
			t.Fatal(err)
		}
		if todoCount(s) != 0 || feedKinds(s, c.conv) != "" {
			t.Fatalf("%s (off %v): todos %d, feed %q", c.conv, c.off, todoCount(s), feedKinds(s, c.conv))
		}
	}
}

func TestGoldenTaskNotice(t *testing.T) {
	for _, c := range []struct {
		name string
		n    quietNotice
	}{
		{"done", quietNotice{todo: Todo{Title: "修登录崩溃"}, machine: "box", state: "done", text: "修好了，PR：https://github.com/o/r/pull/1"}},
		{"needs", quietNotice{todo: Todo{Title: "修登录崩溃"}, machine: "box", state: "needs", ask: "- 用哪个分支？（选项：main / dev）"}},
		{"interrupted_after_needs", quietNotice{todo: Todo{Title: "整理材料"}, state: "interrupted", earlier: []string{"等过用户"}, text: "API Error: 529 overloaded"}},
		{"failed", quietNotice{todo: Todo{Title: "整理材料"}, machine: "box", state: "failed", text: "box 现在离线"}},
	} {
		golden(t, "task_notice."+c.name, renderNotice(&c.n, false))
	}
}

// A conversation started with report_tasks (a plugin's /agent, #3192) is told about the tasks it opens, as the main
// conversation is about its quiet ones: the task is quiet, its notice goes into that conversation (not the main one)
// and says to hand the result on as that conversation was told to; agent_start from it is a task too.
func TestReportingConversationIsToldItsTasks(t *testing.T) {
	s, f := newQuietService(t)
	ctx := context.Background()
	f.known["lark1"] = true
	f.setSessions("server", proto.Session{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle},
		proto.Session{SessionID: "lark1", MachineID: "server", Assistant: true, Status: proto.StatusIdle})
	s.markReporter(ctx, "server", "lark1")
	if _, err := s.toolTaskPlan(ctx, "lark1", planArgs(true)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the link", func() bool { return len(onlyTodo(s).Agents) == 1 })
	if todo := onlyTodo(s); !todo.Quiet || todo.Origin != "lark1" {
		t.Fatalf("todo = %+v", todo)
	}
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, "MR 提好了：https://example.com/mr/7", 0)
	sentTo := func(id string) []string {
		f.mu.Lock()
		defer f.mu.Unlock()
		var out []string
		for _, a := range f.sends {
			if a["session_id"] == id {
				text, _ := a["text"].(string)
				out = append(out, text)
			}
		}
		return out
	}
	waitFor(t, "the notice", func() bool { return len(sentTo("lark1")) == 1 })
	n := sentTo("lark1")[0]
	for _, want := range []string{"🔔 任务「修登录崩溃」做完了（box）", "https://example.com/mr/7", "从这段对话派出去的", "照这段对话一开始说的方式交代"} {
		if !strings.Contains(n, want) {
			t.Errorf("notice lacks %q:\n%s", want, n)
		}
	}
	if strings.Contains(n, "主对话") {
		t.Errorf("the notice talks of the main conversation:\n%s", n)
	}
	if got := f.notices(); len(got) != 0 {
		t.Fatalf("the main conversation was told too: %q", got)
	}

	s2, _ := newQuietService(t)
	setSettings(t, s2, func(v *Settings) { v.MainTasksQuiet = new(false) }) // the main conversation's setting: not this one's
	s2.markReporter(ctx, "server", "lark1")
	args := json.RawMessage(`{"machine":"box","cwd":"/code/app","prompt":"提个 MR 修掉它"}`)
	if _, err := s2.runTool(ctx, "server", "lark1", "agent_start", args); err != nil {
		t.Fatal(err)
	}
	if todo := onlyTodo(s2); !todo.Quiet || todo.Origin != "lark1" || len(todo.Agents) != 1 {
		t.Fatalf("agent_start from a reporting conversation: todo = %+v", todo)
	}
}

// POST /api/v1/assistant/conversations with report_tasks marks the new conversation; without it, none is.
func TestStartWithReportTasks(t *testing.T) {
	s, _ := newQuietService(t)
	if w := serve(s, "POST", "/api/v1/assistant/conversations", `{"prompt":"/agent 看看","report_tasks":true}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if m, ok := s.reporterOf(context.Background(), "s1"); !ok || m != "server" {
		t.Fatalf("reporter = %q, %v", m, ok)
	}
	if w := serve(s, "POST", "/api/v1/assistant/conversations", `{"prompt":"随便聊聊"}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if _, ok := s.reporterOf(context.Background(), "s2"); ok {
		t.Fatal("a conversation started without report_tasks reports")
	}
}

func TestGoldenTaskNoticeReporting(t *testing.T) {
	golden(t, "task_notice.reporting_done", renderNotice(&quietNotice{todo: Todo{Title: "提 MR"}, machine: "box", state: "done", text: "MR：https://example.com/mr/7"}, true))
	golden(t, "task_notice.reporting_needs", renderNotice(&quietNotice{todo: Todo{Title: "提 MR"}, machine: "box", state: "needs", ask: "- 合到哪个分支？"}, true))
}

// An agent a reporting conversation opens with agent_start is its task as one from task_plan is (#4101): not
// supervised by the main conversation, so when it is done the reporting conversation gets the notice and the main
// conversation hears nothing of it.
func TestReportingConversationAgentStartIsToldThere(t *testing.T) {
	s, f := newQuietService(t)
	ctx := context.Background()
	f.judge = `{"verdict":"done","reason":"MR 提好了"}`
	f.known["lark1"] = true
	f.setSessions("server", proto.Session{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle},
		proto.Session{SessionID: "lark1", MachineID: "server", Assistant: true, Status: proto.StatusIdle})
	s.markReporter(ctx, "server", "lark1")
	args := json.RawMessage(`{"machine":"box","cwd":"/code/app","prompt":"提个 MR 修掉它"}`)
	if _, err := s.runTool(ctx, "server", "lark1", "agent_start", args); err != nil {
		t.Fatal(err)
	}
	todo := onlyTodo(s)
	if !todo.Quiet || todo.Origin != "lark1" || todo.Supervision != nil {
		t.Fatalf("todo = %+v, supervision %+v", todo, todo.Supervision)
	}
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, "MR 提好了：https://example.com/mr/8", 0)
	sentTo := func(id string) []string {
		f.mu.Lock()
		defer f.mu.Unlock()
		var out []string
		for _, a := range f.sends {
			if a["session_id"] == id {
				text, _ := a["text"].(string)
				out = append(out, text)
			}
		}
		return out
	}
	waitFor(t, "the notice", func() bool { return len(sentTo("lark1")) == 1 })
	if n := sentTo("lark1")[0]; !strings.Contains(n, "做完了（box）") || !strings.Contains(n, "https://example.com/mr/8") {
		t.Fatalf("notice:\n%s", n)
	}
	time.Sleep(4 * superviseEvery)
	if got := f.notices(); len(got) != 0 {
		t.Fatalf("the main conversation was told: %q", got)
	}
}

// A reporting conversation's task_review on its own unsupervised task closes it without making a ledger, so the task
// never joins the main conversation's 督办 messages; rework and needs_user are refused (#4262).
func TestReportingConversationTaskReviewMakesNoLedger(t *testing.T) {
	s, f := newQuietService(t)
	ctx := context.Background()
	f.known["lark1"] = true
	f.setSessions("server", proto.Session{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle},
		proto.Session{SessionID: "lark1", MachineID: "server", Assistant: true, Status: proto.StatusIdle})
	s.markReporter(ctx, "server", "lark1")
	if _, err := s.runTool(ctx, "server", "lark1", "agent_start", json.RawMessage(`{"machine":"box","cwd":"/code/app","prompt":"提个 MR 修掉它"}`)); err != nil {
		t.Fatal(err)
	}
	todo := onlyTodo(s)
	review := func(verdict string) error {
		raw, _ := json.Marshal(map[string]any{"todo_id": todo.ID, "verdict": verdict, "note": "重复了"})
		_, err := s.runTool(ctx, "server", "lark1", "task_review", raw)
		return err
	}
	for _, v := range []string{"rework", "needs_user"} {
		if err := review(v); err == nil || !strings.Contains(err.Error(), "not supervised") {
			t.Fatalf("%s: %v", v, err)
		}
	}
	if err := review("cancelled"); err != nil {
		t.Fatal(err)
	}
	now := todoNow(s, todo.ID)
	if now.DoneAt == nil || now.DoneBy != db.DoneByReview || now.Supervision != nil {
		t.Fatalf("after cancelled: %+v, supervision %+v", now, now.Supervision)
	}
}
