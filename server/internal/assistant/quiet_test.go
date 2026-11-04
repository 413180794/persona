package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// newQuietService is newTaskService with the main conversation conv1 (idle, on machine server) and a short notice
// delay.
func newQuietService(t *testing.T) (*Service, *fakeHub) {
	t.Helper()
	old := quietNoticeDelay
	quietNoticeDelay = 30 * time.Millisecond
	t.Cleanup(func() { quietNoticeDelay = old })
	s, f := newTaskService(t)
	f.hello.Assistant = Placement{MachineID: "server", Main: "conv1"}
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

// A task the main conversation opens is quiet: when it is done, the main conversation gets one notice with what its
// agent said, as is (not wrapped like a message from another conversation); the receipt is there as always.
func TestQuietTaskTellsTheMainConversation(t *testing.T) {
	s, f := newQuietService(t)
	todo := startedTask(t, s, f)
	if !todo.Quiet {
		t.Fatalf("a task from the main conversation is not quiet: %+v", todo)
	}
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, "修好了，提了 https://github.com/o/r/pull/901", 0)
	waitFor(t, "the notice", func() bool { return len(f.notices()) == 1 })
	n := f.notices()[0]
	for _, want := range []string{"🔔 任务「修登录崩溃」做完了（box）", "修好了，提了 https://github.com/o/r/pull/901", "不是用户发的消息"} {
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
	time.Sleep(3 * quietNoticeDelay)
	if got := len(f.notices()); got != 1 {
		t.Fatalf("%d notices", got)
	}
}

// A question and the end of the work within the delay are one notice: the latest state, saying the earlier one.
// A question answered before the notice goes out is no notice.
func TestQuietNoticesMerge(t *testing.T) {
	s, f := newQuietService(t)
	startedTask(t, s, f)
	ix := proto.Interaction{ID: "ix1", MachineID: "box", SessionID: "s1", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "用哪个分支？", Options: []proto.QuestionOption{{Label: "main"}, {Label: "dev"}}}}}
	agentTurn(s, f, proto.StatusBlocked, "", 1)
	s.InteractionRaised(context.Background(), InteractionRaised{Interaction: ix})
	agentTurn(s, f, proto.StatusDone, "用 main 做完了", 0)
	waitFor(t, "the notice", func() bool { return len(f.notices()) == 1 })
	n := f.notices()[0]
	if !strings.Contains(n, "做完了") || !strings.Contains(n, "等过用户") || strings.Contains(n, "用哪个分支") {
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
	if n := f3.notices()[0]; !strings.Contains(n, "在等用户") || !strings.Contains(n, "用哪个分支？（选项：main / dev）") {
		t.Fatalf("notice:\n%s", n)
	}
}

// While the main conversation runs, the notice waits behind its turn; a newer one about the same task takes its
// place in the queue instead of following it.
func TestQuietNoticeWaitsForTheTurn(t *testing.T) {
	s, f := newQuietService(t)
	startedTask(t, s, f)
	busy := proto.Session{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusWorking}
	f.setSessions("server", busy)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, "第一次做完", 0)
	waitFor(t, "the notice to queue", func() bool { return s.queuedFor("conv1") == 1 })
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, "第二次做完", 0)
	time.Sleep(4 * quietNoticeDelay)
	if n := s.queuedFor("conv1"); n != 1 {
		t.Fatalf("%d queued", n)
	}
	if q, _ := s.queuedRelays(); len(q["conv1"]) != 1 || q["conv1"][0] != "任务「修登录崩溃」的通知" {
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
	if n := f.notices()[0]; !strings.Contains(n, "第二次做完") || strings.Contains(n, "来自对话") {
		t.Fatalf("notice:\n%s", n)
	}
}

// Tasks from another conversation, or with the setting off, are not quiet: no notices (the hub pushes as always).
func TestNotQuietTasksAreNotTold(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(*fakeHub)
	}{
		{"side conversation", func(f *fakeHub) { f.hello.Assistant.Main = "conv-main" }},
		{"setting off", func(f *fakeHub) { f.quietOff = true }},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, f := newQuietService(t)
			c.set(f)
			if todo := startedTask(t, s, f); todo.Quiet {
				t.Fatal("quiet")
			}
			agentTurn(s, f, proto.StatusWorking, "", 0)
			agentTurn(s, f, proto.StatusDone, "好了", 0)
			time.Sleep(4 * quietNoticeDelay)
			if len(f.notices()) != 0 {
				t.Fatal("told")
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
	s, f := newQuietService(t)
	res, err := s.runTool(context.Background(), "server", "conv1", "agent_start", args)
	if err != nil {
		t.Fatal(err)
	}
	todo := f.onlyTodo()
	if !todo.Quiet || todo.Origin != "conv1" || len(todo.Agents) != 1 || todo.Agents[0].SessionID != "s1" || todo.Title != "看看 CI 为什么挂了" {
		t.Fatalf("todo = %+v", todo)
	}
	if !strings.Contains(res, "a task of this conversation") || feedKinds(s, "conv1") != "task" {
		t.Fatalf("result %q, feed %q", res, feedKinds(s, "conv1"))
	}

	for _, c := range []struct {
		conv string
		off  bool
	}{{"conv2", false}, {"conv1", true}} {
		s, f := newQuietService(t)
		f.quietOff = c.off
		if _, err := s.runTool(context.Background(), "server", c.conv, "agent_start", args); err != nil {
			t.Fatal(err)
		}
		if len(f.todos) != 0 || feedKinds(s, c.conv) != "" {
			t.Fatalf("%s (off %v): todos %d, feed %q", c.conv, c.off, len(f.todos), feedKinds(s, c.conv))
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
		golden(t, "task_notice."+c.name, renderNotice(&c.n))
	}
}
