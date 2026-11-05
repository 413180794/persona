package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

const deployedReply = "已合入 https://github.com/o/r/pull/9 并已部署到 n37。打开 https://hub.example.com/settings?tab=feishu 能看到新开关"

// acceptService is reportService with the small model answering the 请验收 lines, the cards' updates and notes kept.
func acceptService(t *testing.T) (*Service, *fakeHub, func() []string) {
	t.Helper()
	s, f, posts := reportService(t, true)
	f.judge = `{"changed":"飞书机器人多了任务进展开关","link":"https://hub.example.com/settings?tab=feishu","how":"打开设置 › 飞书机器人，应看到「任务进展发到飞书」"}`
	f.answerApp(AppFeishu, FeishuUpdateCard, func(json.RawMessage) (any, error) { return map[string]any{}, nil })
	f.answerApp(AppFeishu, FeishuText, func(json.RawMessage) (any, error) { return map[string]any{}, nil })
	return s, f, posts
}

// askOf is task id's 请验收 ask as it is now (nil: none), read under the lock the hub's goroutines write it under.
func askOf(s *Service, id string) *acceptAsk {
	s.accept.mu.Lock()
	defer s.accept.mu.Unlock()
	a, ok := s.accept.asks[id]
	if !ok {
		return nil
	}
	cp := *a
	return &cp
}

func acceptCardOf(posts []string) string {
	for _, p := range posts {
		if strings.Contains(p, "请验收") {
			return p
		}
	}
	return ""
}

// A turn that says the work is deployed gets a 请验收 card: what changed, a link straight to the page (one from the
// reply, never made up), how to check, and 通过. 通过 marks the task verified.
func TestDeployedTaskAsksForAcceptance(t *testing.T) {
	s, f, posts := acceptService(t)
	todo := startedTask(t, s, f)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, deployedReply, 0)
	waitFor(t, "the 请验收 card", func() bool { return acceptCardOf(posts()) != "" })
	card := acceptCardOf(posts())
	for _, want := range []string{"改了什么", "飞书机器人多了任务进展开关", "去哪看", "https://hub.example.com/settings?tab=feishu", "怎么验", "通过", `"a":"accept"`} {
		if !strings.Contains(card, want) {
			t.Errorf("card lacks %q:\n%s", want, card)
		}
	}
	mid := askOf(s, todo.ID).MessageID
	if !s.acceptReply(context.Background(), "om_reply", "通过", mid, "") {
		t.Fatal("a reply to the card was not taken as one")
	}
	if got := onlyTodo(s); got.Stage != "verify" {
		t.Fatalf("stage %q", got.Stage)
	}
	// Another turn that says deployed again does not ask twice.
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, deployedReply, 0)
	time.Sleep(6 * testReportDelay)
	n := 0
	for _, p := range posts() {
		if strings.Contains(p, "请验收") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d 请验收 cards", n)
	}
}

// Anything else said to the card goes to the task's agent (and the todo's decisions with a store); the next deploy
// asks again. A model that makes up a link is not believed.
func TestAcceptanceFeedbackGoesToTheAgent(t *testing.T) {
	s, f, posts := acceptService(t)
	f.judge = `{"changed":"改了","link":"https://evil.example.com/x","how":"看看"}`
	todo := startedTask(t, s, f)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, deployedReply, 0)
	waitFor(t, "the 请验收 card", func() bool { return acceptCardOf(posts()) != "" })
	if card := acceptCardOf(posts()); strings.Contains(card, "evil.example.com") || !strings.Contains(card, "/todos?id="+todo.ID) {
		t.Fatalf("link:\n%s", card)
	}
	mid := askOf(s, todo.ID).MessageID
	before := len(f.agentSent)
	if !s.acceptReply(context.Background(), "om_r2", "开关名字改成「任务进展」", "", mid) {
		t.Fatal("not taken")
	}
	f.mu.Lock()
	sent := f.agentSent[before:]
	f.mu.Unlock()
	if len(sent) != 1 || !strings.Contains(sent[0]["text"].(string), "开关名字改成「任务进展」") {
		t.Fatalf("to the agent: %v", sent)
	}
	if askOf(s, todo.ID) != nil {
		t.Fatal("a change asked for leaves the ask in place: the next deploy would not ask again")
	}
	if s.acceptReply(context.Background(), "om_r3", "随便聊聊", "om_other", "") {
		t.Fatal("a message about something else was taken")
	}
}

// One not accepted by the next morning is asked again, once.
func TestAcceptanceReminderNextMorning(t *testing.T) {
	s, f, posts := acceptService(t)
	todo := startedTask(t, s, f)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, deployedReply, 0)
	waitFor(t, "the 请验收 card", func() bool { return acceptCardOf(posts()) != "" })
	loc := s.location(context.Background())
	s.accept.mu.Lock()
	s.accept.asks[todo.ID].AskedAt = time.Now().Add(-30 * time.Hour)
	s.accept.mu.Unlock()
	now := time.Now().In(loc)
	morning := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, loc)
	n := len(posts())
	s.remindAcceptance(morning)
	s.remindAcceptance(morning.Add(time.Hour))
	if got := posts()[n:]; len(got) != 1 || !strings.Contains(got[0], "还没验收") {
		t.Fatalf("reminders %q", got)
	}
}

// A card not passed yet is in the snapshot the morning briefing reads, with what changed and where to look; the
// reminder waits for the briefing's own time (08:30 here), not a fixed 9:00.
func TestUnacceptedInTheMorningBriefing(t *testing.T) {
	s, f, posts := acceptService(t)
	todo := startedTask(t, s, f)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, deployedReply, 0)
	waitFor(t, "the 请验收 card", func() bool { return acceptCardOf(posts()) != "" })
	ctx := context.Background()
	loc := s.location(ctx)
	got, total := s.snapshotUnaccepted(ctx, loc)
	if len(got) != 1 || total != 1 || got[0].Changed != "飞书机器人多了任务进展开关" || got[0].Link != "https://hub.example.com/settings?tab=feishu" {
		t.Fatalf("unaccepted %+v", got)
	}
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range sc.tasks {
		if sc.tasks[i].ID == "morning" {
			sc.tasks[i].Time, sc.tasks[i].Enabled = "08:30", true
		}
	}
	sc.mu.Unlock()
	if hh, mm := s.briefingClock(ctx); hh != 8 || mm != 30 {
		t.Fatalf("briefing at %d:%02d", hh, mm)
	}
	s.accept.mu.Lock()
	s.accept.asks[todo.ID].AskedAt = time.Now().Add(-30 * time.Hour)
	s.accept.mu.Unlock()
	now := time.Now().In(loc)
	n := len(posts())
	s.remindAcceptance(time.Date(now.Year(), now.Month(), now.Day(), 8, 20, 0, 0, loc))
	if len(posts()) != n {
		t.Fatalf("reminded before the briefing")
	}
	s.remindAcceptance(time.Date(now.Year(), now.Month(), now.Day(), 8, 40, 0, 0, loc))
	if got := posts()[n:]; len(got) != 1 || !strings.Contains(got[0], "还没验收") {
		t.Fatalf("reminders %q", got)
	}
	s.accepted(ctx, todo.ID)
	if got, _ := s.snapshotUnaccepted(ctx, loc); len(got) != 0 {
		t.Fatalf("still listed after 通过: %+v", got)
	}
}

// A change asked for on the card of a task the hub closed meanwhile reopens it: while its agent works on the change
// the task is open, and the agent's next turn is followed again — the next deploy gets a new 请验收 card (#3457).
func TestAcceptanceFeedbackReopensTheClosedTask(t *testing.T) {
	s, f, posts := acceptService(t)
	todo := startedTask(t, s, f)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, deployedReply, 0)
	waitFor(t, "the 请验收 card", func() bool { return acceptCardOf(posts()) != "" })
	ctx := context.Background()
	if ok, err := s.autoDoneTodo(ctx, todo.ID, "做完了", time.Now().Add(-time.Minute)); err != nil || !ok {
		t.Fatalf("auto done: %v %v", ok, err)
	}
	mid := askOf(s, todo.ID).MessageID
	// The agent may take the change up at once: by the time it is sent, the task must follow the agent again.
	followedAtSend := false
	f.mu.Lock()
	f.onSend = func(*fakeHub, map[string]any) { _, followedAtSend = s.taskOf(ctx, "box", "s1") }
	f.mu.Unlock()
	if !s.acceptReply(ctx, "om_r2", "按钮改成蓝色", mid, "") {
		t.Fatal("not taken")
	}
	f.mu.Lock()
	f.onSend = nil
	f.mu.Unlock()
	if !followedAtSend {
		t.Fatal("the change was sent while the task was still closed: the agent's turn would not be followed")
	}
	got := onlyTodo(s)
	if got.DoneAt != nil || got.ReopenedAt == nil || got.DoneBy != "" {
		t.Fatalf("still closed after a change was asked for: %+v", got)
	}
	if _, ok := s.taskOf(ctx, "box", "s1"); !ok {
		t.Fatal("the agent no longer counts for the task")
	}
	n := 0
	for _, p := range posts() {
		if strings.Contains(p, "请验收") {
			n++
		}
	}
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, deployedReply, 0)
	waitFor(t, "a new 请验收 card", func() bool {
		m := 0
		for _, p := range posts() {
			if strings.Contains(p, "请验收") {
				m++
			}
		}
		return m == n+1
	})
}

// Approval with a completion particle (通过了, 可以啊, 没问题了呀, ok了, 验收通过了) still marks the task
// verified. Questions (可以了吗, 通过了？/?) and negations (不通过, 通过不了, 不行) stay feedback for the agent.
func TestPassWordsParticles(t *testing.T) {
	pass := []string{
		"通过", "通过。", "通过了。", "通过啦！", "验收通过了",
		"可以了", "没问题了", "ok了", "OK啦",
		"通过呀", "通过咯", "通过喽", "可以呗", "好哒", "可以啊", "没问题啊",
		"通过了呀",    // two completion particles, still an approval
		"好", "好的", // bare 好 already matched before this change; the behavior is kept
	}
	for _, in := range pass {
		if !passWords.MatchString(in) {
			t.Errorf("%q must count as approval", in)
		}
	}
	notPass := []string{
		"可以了吗", "通过了？", "通过了?", "这能通过？",
		"不通过", "没通过", "不行", "还不行", "通过不了", "不可以了",
		"开关名字改成「任务进展」",
	}
	for _, in := range notPass {
		if passWords.MatchString(in) {
			t.Errorf("%q must not count as approval", in)
		}
	}
}

// A 请验收 card whose task ends without the hub's auto-judge — the owner closes it in the todos, or a supervision
// review does — can never be passed: it is neither reminded nor counted in the morning briefing, and is dropped.
func TestAcceptanceAskEndedElsewhere(t *testing.T) {
	cases := []struct {
		name string
		end  func(ctx context.Context, s *Service, id string)
		kept bool
	}{
		{"closed manually", func(_ context.Context, s *Service, id string) {
			done := time.Now()
			s.todos.mu.Lock()
			s.todos.todos[id].DoneAt, s.todos.todos[id].DoneBy, s.todos.todos[id].DoneReason = &done, "", ""
			s.todos.mu.Unlock()
		}, false},
		{"closed by a review", func(ctx context.Context, s *Service, id string) {
			if ok, err := s.reviewDoneTodo(ctx, id, "验收通过", true); err != nil || !ok {
				t.Fatalf("review done: %v %v", ok, err)
			}
		}, false},
		{"closed by the auto judge", func(ctx context.Context, s *Service, id string) {
			if ok, err := s.autoDoneTodo(ctx, id, "做完了", time.Now().Add(-time.Minute)); err != nil || !ok {
				t.Fatalf("auto done: %v %v", ok, err)
			}
		}, true},
		{"still open", func(context.Context, *Service, string) {}, true},
	}
	// setup deploys a task until its 请验收 card is out; the case then ends it.
	setup := func(t *testing.T) (*Service, *fakeHub, func() []string, string) {
		s, f, posts := acceptService(t)
		todo := startedTask(t, s, f)
		agentTurn(s, f, proto.StatusWorking, "", 0)
		agentTurn(s, f, proto.StatusDone, deployedReply, 0)
		// Wait for the final ask (MessageID is set only after botPost returns): backdating AskedAt must not race the
		// post-botPost replacement of the claimed placeholder.
		waitFor(t, "the 请验收 card", func() bool {
			a := askOf(s, todo.ID)
			return acceptCardOf(posts()) != "" && a != nil && a.MessageID != ""
		})
		return s, f, posts, todo.ID
	}
	for _, c := range cases {
		t.Run(c.name+" remind", func(t *testing.T) {
			ctx := context.Background()
			s, _, posts, id := setup(t)
			c.end(ctx, s, id)
			s.accept.mu.Lock()
			s.accept.asks[id].AskedAt = time.Now().Add(-30 * time.Hour)
			s.accept.mu.Unlock()
			now := time.Now().In(s.location(ctx))
			n := len(posts())
			s.remindAcceptance(time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, s.location(ctx)))
			got := posts()[n:]
			if c.kept {
				if len(got) != 1 || !strings.Contains(got[0], "还没验收") || askOf(s, id) == nil {
					t.Fatalf("reminders %q, ask kept=%v", got, askOf(s, id) != nil)
				}
			} else if td := onlyTodo(s); td.DoneAt == nil || td.DoneBy == db.DoneByAuto ||
				len(got) != 0 || askOf(s, id) != nil || s.accepted(ctx, id) {
				t.Fatalf("ended task was not cleaned: task=%+v reminded=%q ask=%v", td, got, askOf(s, id) != nil)
			}
		})
		t.Run(c.name+" snapshot", func(t *testing.T) {
			ctx := context.Background()
			s, _, _, id := setup(t)
			c.end(ctx, s, id)
			_, total := s.snapshotUnaccepted(ctx, s.location(ctx))
			if (total == 1) != c.kept || (askOf(s, id) != nil) != c.kept {
				t.Fatalf("total=%d kept=%v ask=%v", total, c.kept, askOf(s, id) != nil)
			}
		})
	}
}
