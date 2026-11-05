package assistant

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

func agentHub(t *testing.T) *inboxHub {
	h := newInboxHub(t)
	h.inboxReady(context.Background())
	return h
}

func actionItem(id string, at time.Time) *InboxItem {
	return &InboxItem{ID: id, Kind: KindDM, Tier: TierAction, Last: "12", TriagedAt: "12", Context: "张三：1013 合了吗？",
		UpdatedAt: at, Lark: &InboxLark{FeedID: "f1"}}
}

func TestInboxJudgeQueuesOrSkips(t *testing.T) {
	now := time.Now()
	h := agentHub(t)
	it := actionItem("lark:f1:11", now)
	h.inbox.items[it.ID] = it

	h.in.model = fakeModel(t, `{"handle":true,"task":"查 1013 是否合入","reason":"可查"}`)
	if got := h.inbox.toJudge(now.Add(-time.Hour)); len(got) != 1 {
		t.Fatalf("toJudge = %d", len(got))
	}
	if !h.judgeOne(context.Background(), it) || it.Agent == nil || it.Agent.State != AgentQueued ||
		it.Agent.Task != "查 1013 是否合入" || it.JudgedAt != "12" || it.Agent.Of != "12" {
		t.Fatalf("agent = %+v judged %q", it.Agent, it.JudgedAt)
	}
	if got := h.inbox.toJudge(now.Add(-time.Hour)); len(got) != 0 {
		t.Fatalf("judged twice: %d", len(got))
	}

	// New messages: judged again, and the old work is replaced.
	it.Last, it.TriagedAt = "13", "13"
	h.in.model = fakeModel(t, "```json\n{\"handle\":false,\"reason\":\"只是通知\"}\n```")
	if !h.judgeOne(context.Background(), it) || it.Agent.State != AgentSkipped || it.Agent.Reason != "只是通知" {
		t.Fatalf("agent = %+v", it.Agent)
	}

	// Not 需要你, too old, not triaged on the current content, done: never judged.
	fyi := actionItem("a", now)
	fyi.Tier = TierFYI
	old := actionItem("b", now.Add(-48*time.Hour))
	stale := actionItem("c", now)
	stale.Last = "14"
	done := actionItem("d", now)
	done.done(now, "user")
	h.inbox.items = map[string]*InboxItem{"a": fyi, "b": old, "c": stale, "d": done}
	if got := h.inbox.toJudge(now.Add(-24 * time.Hour)); len(got) != 0 {
		t.Fatalf("toJudge = %+v", got)
	}
}

func TestInboxJudgeModelFailureRetries(t *testing.T) {
	h := agentHub(t)
	it := actionItem("x", time.Now())
	h.inbox.items[it.ID] = it
	h.in.model = fakeModel(t, "no json here")
	if h.judgeOne(context.Background(), it) || it.JudgedAt != "" || it.Agent != nil {
		t.Fatalf("judged on a failed call: %+v", it.Agent)
	}
}

// An agent runtime that is already running answers before startInboxAgent has recorded the conversation: the draft
// waits for it instead of failing.
func TestInboxDraftBeforeTheStartReturned(t *testing.T) {
	h := agentHub(t)
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentRunning, Task: "t"} // starting: no conversation yet
	h.inbox.items[it.ID] = it
	go func() {
		time.Sleep(300 * time.Millisecond)
		h.inbox.mu.Lock()
		it.Agent.SessionID = "session-r1"
		h.inbox.mu.Unlock()
	}()
	if id, refused := h.inboxDraft(context.Background(), "session-r1", "好的", "", nil); id != it.ID || refused != nil {
		t.Fatalf("drafted on %q, refused %+v", id, refused)
	}
	if it.Agent.State != AgentDrafted || it.Agent.Draft != "好的" {
		t.Fatalf("agent = %+v", it.Agent)
	}
	old := inboxDraftWait
	inboxDraftWait = 200 * time.Millisecond
	t.Cleanup(func() { inboxDraftWait = old })
	other := actionItem("y", time.Now())
	other.Agent = &InboxAgent{State: AgentRunning}
	h.inbox.mu.Lock()
	h.inbox.items[other.ID] = other
	h.inbox.mu.Unlock()
	if id, _ := h.inboxDraft(context.Background(), "session-nobody", "hi", "", nil); id != "" {
		t.Fatal("a conversation no item gets drafted")
	}
}

// The draft (inbox_draft) goes on the item whose agent runs in the conversation, with the moves of a document's rows
// the item offers; the hub is told which conversations are the AI 分身's (no finished push for them).
func TestInboxDraft(t *testing.T) {
	h := agentHub(t)
	ctx := context.Background()
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentRunning, SessionID: "s1", MachineID: "server", Task: "t"}
	h.inbox.items[it.ID] = it

	if id, _ := h.inboxDraft(ctx, "other", "hi", "", nil); id != "" {
		t.Fatalf("drafted from a conversation that is not an inbox agent: %q", id)
	}
	// A change the item does not list is refused, and nothing is kept.
	change := DocChange{Ref: "r1", Field: "状态", Value: "完成"}
	if id, refused := h.inboxDraft(ctx, "s1", "好了", "", []DocChange{change}); id != it.ID || refused == nil || *refused != change ||
		it.Agent.State != AgentRunning || it.Agent.Draft != "" {
		t.Fatalf("a change the item does not offer: %q %v, agent %+v", id, refused, it.Agent)
	}
	if id, refused := h.inboxDraft(ctx, "s1", "合了，MR 已经进主干", "agent_read 看到已合入", nil); id != it.ID || refused != nil {
		t.Fatalf("draft: %q %v", id, refused)
	}
	if a := it.Agent; a.State != AgentDrafted || a.Draft != "合了，MR 已经进主干" || a.Note != "agent_read 看到已合入" || a.DraftedAt == nil {
		t.Fatalf("agent = %+v", a)
	}
	// A document item's own rows and values are taken with the draft.
	doc := actionItem("doc", time.Now())
	doc.Doc = &InboxDoc{Rows: []DocRow{{Ref: "r1", Status: []DocStatus{{Field: "状态", Options: []DocOption{{Name: "完成"}}}}}}}
	doc.Agent = &InboxAgent{State: AgentRunning, SessionID: "s2", Task: "t"}
	h.inbox.mu.Lock()
	h.inbox.items[doc.ID] = doc
	h.inbox.mu.Unlock()
	if id, refused := h.inboxDraft(ctx, "s2", "好了", "", []DocChange{change}); id != doc.ID || refused != nil {
		t.Fatalf("a change the item offers: %q %v", id, refused)
	}
	if a := doc.Agent; a.State != AgentDrafted || len(a.Changes) != 1 || a.Changes[0] != change {
		t.Fatalf("doc agent = %+v", a)
	}
	r := h.inboxReport(ctx)
	if r == nil || len(r.Agents) != 2 || r.Agents[0] != (SessionRef{MachineID: "server", SessionID: "s1"}) || r.Agents[1].SessionID != "s2" {
		t.Fatalf("the agents the hub is told: %+v", r)
	}
	// The list carries the draft but none of the bookkeeping.
	it.JudgedAt = "12"
	l := it.listed()
	if l.Agent == it.Agent || l.Agent.Draft != it.Agent.Draft || l.JudgedAt != "" || l.Context != "" {
		t.Fatalf("listed = %+v", l)
	}
}

func TestInboxAgentTimesOut(t *testing.T) {
	h := agentHub(t)
	started := time.Now().Add(-20 * time.Minute)
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentRunning, SessionID: "s1", MachineID: "nowhere", StartedAt: &started}
	h.inbox.items[it.ID] = it
	h.sweepInboxAgents(context.Background(), InboxAgentConfig{}, time.Now())
	if it.Agent.State != AgentFailed || it.Agent.Error == "" {
		t.Fatalf("agent = %+v", it.Agent)
	}
}

// An agent that ended its turn without a draft fails (after its first minute).
func TestInboxAgentDoneWithoutADraft(t *testing.T) {
	h := agentHub(t)
	started := time.Now().Add(-2 * time.Minute)
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentRunning, SessionID: "s1", MachineID: "server", StartedAt: &started}
	h.inbox.items[it.ID] = it
	h.f.setSessions("server", proto.Session{SessionID: "s1", Status: proto.StatusIdle})
	h.sweepInboxAgents(context.Background(), InboxAgentConfig{}, time.Now())
	if it.Agent.State != AgentFailed || it.Agent.Error != "做完了但没有给出草稿" {
		t.Fatalf("agent = %+v", it.Agent)
	}
}

func TestInboxAgentNoMachineFails(t *testing.T) {
	h := agentHub(t)
	h.f.mu.Lock()
	h.f.hello.Assistant.MachineID = ""
	delete(h.f.sessions, "server")
	h.f.mu.Unlock()
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentQueued, Task: "t"}
	h.inbox.items[it.ID] = it
	h.startInboxAgents(context.Background(), InboxAgentConfig{Enabled: true})
	if it.Agent.State != AgentFailed || !strings.Contains(it.Agent.Error, "machine") {
		t.Fatalf("agent = %+v", it.Agent)
	}
}

// A queued item's agent is the assistant's own conversation, untitled, on the inbox agent's model and the
// assistant's machine; the hub learns it is the AI 分身's from the next snapshot.
func TestInboxAgentStarts(t *testing.T) {
	h := agentHub(t)
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentQueued, Task: "查 1013 是否合入"}
	h.inbox.items[it.ID] = it
	h.startInboxAgents(context.Background(), InboxAgentConfig{Enabled: true})
	if it.Agent.State != AgentRunning || it.Agent.MachineID != "server" || it.Agent.SessionID == "" {
		t.Fatalf("agent = %+v", it.Agent)
	}
	send := h.f.lastSend(t)
	if !strings.Contains(send["text"].(string), "查 1013 是否合入") {
		t.Fatalf("the first message: %v", send)
	}
	snap := h.f.lastSnapshot()
	if snap.Inbox == nil || len(snap.Inbox.Agents) != 1 || snap.Inbox.Agents[0].SessionID != it.Agent.SessionID {
		t.Fatalf("the snapshot's inbox: %+v", snap.Inbox)
	}
	h.titles.mu.Lock()
	_, titled := h.titles.m[it.Agent.SessionID]
	h.titles.mu.Unlock()
	if titled {
		t.Fatal("the AI 分身's conversation is named like the assistant's")
	}
}

func TestInboxAgentPrompt(t *testing.T) {
	it := actionItem("x", time.Now())
	it.Title = `群 "A" <b>`
	it.Agent = &InboxAgent{Task: "查 1013 是否合入"}
	p := newInbox().inboxAgentPrompt(it, "小明", nil)
	for _, want := range []string{`title="飞书 · 群 &quot;A&quot; &lt;b&gt;"`, "飞书会话 id：f1", "查 1013 是否合入",
		"inbox_draft", "张三：1013 合了吗？", "小明"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
}

// TestInboxModelsLive runs models 1 and 2 on a sample item against a real gateway:
// JARVIS_LIVE_AI='{"base_url":"…","api_key_file":"…","model":"…"}' go test -run InboxModelsLive -v ./internal/assistant
func TestInboxModelsLive(t *testing.T) {
	raw := os.Getenv("JARVIS_LIVE_AI")
	if raw == "" {
		t.Skip("JARVIS_LIVE_AI not set")
	}
	var cfg ai.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	h := agentHub(t)
	h.in.model = ai.New(cfg)
	it := &InboxItem{ID: "x", Kind: KindDM, Last: "12", UpdatedAt: time.Now(), Lark: &InboxLark{FeedID: "f1"},
		Context: "张三（10:02）：你那边 codex 的改动合进 1013 分支了吗？今天下午要封版，合了的话把 MR 发我一下"}
	h.inbox.items[it.ID] = it
	start := time.Now()
	if !h.triageOne(context.Background(), it) || it.Reason == "按规则" {
		t.Fatalf("triage failed: %+v", it)
	}
	t.Logf("model 1 (%s): tier=%s summary=%q reason=%q", time.Since(start).Round(time.Millisecond), it.Tier, it.Summary, it.Reason)
	if it.Tier != TierAction {
		t.Fatalf("tier = %s", it.Tier)
	}
	start = time.Now()
	if !h.judgeOne(context.Background(), it) {
		t.Fatal("judge failed")
	}
	t.Logf("model 2 (%s): %+v", time.Since(start).Round(time.Millisecond), *it.Agent)
	if it.Agent.State != AgentQueued {
		t.Fatalf("not handed over: %+v", it.Agent)
	}
}

// An item about one of the user's tasks (docs/unified-entry.md): the judge sees the tasks and links the item; the AI
// 分身 is told the task's progress; its draft puts a card into the task's conversation.
func TestInboxItemAboutATask(t *testing.T) {
	ctx := context.Background()
	s, f := newTaskService(t)
	in := newFakeInbox()
	f.inbox, in.svc = in, s
	h := &inboxHub{Service: s, f: f, in: in}
	h.inboxReady(ctx)
	todo := startedTask(t, s, f)
	model, calls := captureModel(t, `{"handle":true,"task":"回复修复进度","reason":"问进度","task_id":"`+todo.ID+`"}`)
	in.setModel(model)
	it := actionItem("lark:f1:12", time.Now())
	it.Title, it.Sender, it.Preview, it.Source, it.AccountName = "客户端发版群", "陈晓", "登录那个崩溃今天能修好吗？", "lark", "飞书"
	h.inbox.mu.Lock() // the started task's snapshot reads the inbox meanwhile
	h.inbox.items[it.ID] = it
	h.inbox.mu.Unlock()
	if !h.judgeOne(ctx, it) {
		t.Fatal("not judged")
	}
	if c := onlyCall(t, calls()); !strings.Contains(c.User, todo.ID+"：修登录崩溃") {
		t.Fatalf("the judge was not shown the task:\n%s", c.User)
	}
	if it.Agent == nil || it.Agent.TodoID != todo.ID {
		t.Fatalf("agent = %+v", it.Agent)
	}
	linked := &prompts.InboxTask{ID: todo.ID, Title: "修登录崩溃", State: "在做", Last: "测试通过"}
	if p := h.inbox.inboxAgentPromptFor(it, "示例用户", nil, linked, 1); !strings.Contains(p, "正在做的任务「修登录崩溃」") || !strings.Contains(p, "测试通过") {
		t.Fatalf("the agent is not told the task:\n%s", p)
	}

	// A task id it was not shown is not taken.
	model2, _ := captureModel(t, `{"handle":true,"task":"x","reason":"r","task_id":"todo_made_up"}`)
	in.setModel(model2)
	other := actionItem("lark:f2:12", time.Now())
	h.inbox.mu.Lock()
	h.inbox.items[other.ID] = other
	h.inbox.mu.Unlock()
	h.judgeOne(ctx, other)
	if other.Agent == nil || other.Agent.TodoID != "" {
		t.Fatalf("made-up task linked: %+v", other.Agent)
	}

	// The draft: the card goes into the task's conversation.
	it.Agent.State, it.Agent.SessionID = AgentRunning, "s_agent"
	if out, err := runTool(s, "s_agent", "inbox_draft", map[string]any{"text": "测试已经过了，十点前提 PR"}); err != nil || !strings.Contains(out, "in the user's inbox") {
		t.Fatalf("inbox_draft: %s %v", out, err)
	}
	var cards []FeedItem // its messages (the task's plan and card are there too)
	s.tasks.mu.Lock()
	for _, fi := range s.tasks.feed["conv1"] {
		if fi.Kind == feedMessage {
			cards = append(cards, fi)
		}
	}
	s.tasks.mu.Unlock()
	if len(cards) != 1 || cards[0].Ref != it.ID {
		t.Fatalf("cards = %+v", cards)
	}
	var mb MessageCard
	_ = json.Unmarshal(cards[0].Body, &mb)
	if mb.TodoID != todo.ID || mb.Draft != "测试已经过了，十点前提 PR" || mb.Sender != "陈晓" || mb.Chat != "客户端发版群" {
		t.Fatalf("card = %+v", mb)
	}
}
