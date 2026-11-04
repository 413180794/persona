package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/prompts"
)

// fakeModel answers every Messages API call with reply.
func fakeModel(t *testing.T, reply string) *ai.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text", "text": reply}}})
	}))
	t.Cleanup(srv.Close)
	key := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(key, []byte("k"), 0o600)
	return ai.New(ai.Config{BaseURL: srv.URL, APIKeyFile: key, Model: "m"})
}

func agentHub(t *testing.T) *Hub {
	h := newTestHub(t)
	h.inbox.init()
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

	h.triageAI = fakeModel(t, `{"handle":true,"task":"查 1013 是否合入","reason":"可查"}`)
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
	h.triageAI = fakeModel(t, "```json\n{\"handle\":false,\"reason\":\"只是通知\"}\n```")
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
	h.triageAI = fakeModel(t, "no json here")
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
	if _, err := h.toolInboxDraft(context.Background(), "session-r1", json.RawMessage(`{"text":"好的"}`)); err != nil {
		t.Fatal(err)
	}
	if it.Agent.State != AgentDrafted || it.Agent.Draft != "好的" {
		t.Fatalf("agent = %+v", it.Agent)
	}
	old := inboxDraftWait
	inboxDraftWait = 200 * time.Millisecond
	t.Cleanup(func() { inboxDraftWait = old })
	other := actionItem("y", time.Now())
	other.Agent = &InboxAgent{State: AgentRunning}
	h.inbox.items[other.ID] = other
	if _, err := h.toolInboxDraft(context.Background(), "session-nobody", json.RawMessage(`{"text":"hi"}`)); err == nil {
		t.Fatal("a conversation no item gets drafted")
	}
}

func TestInboxDraftTool(t *testing.T) {
	h := agentHub(t)
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentRunning, SessionID: "s1", Task: "t"}
	h.inbox.items[it.ID] = it

	if _, err := h.toolInboxDraft(context.Background(), "other", json.RawMessage(`{"text":"hi"}`)); err == nil {
		t.Fatal("drafted from a conversation that is not an inbox agent")
	}
	if _, err := h.toolInboxDraft(context.Background(), "s1", json.RawMessage(`{"text":"  "}`)); err == nil {
		t.Fatal("accepted an empty draft")
	}
	if _, err := h.toolInboxDraft(context.Background(), "s1", json.RawMessage(`{"text":" 合了，MR 已经进主干 ","note":"agent_read 看到已合入"}`)); err != nil {
		t.Fatal(err)
	}
	if a := it.Agent; a.State != AgentDrafted || a.Draft != "合了，MR 已经进主干" || a.Note == "" || a.DraftedAt == nil {
		t.Fatalf("agent = %+v", a)
	}
	if !h.isInboxAgent("s1") || h.isInboxAgent("s2") {
		t.Fatal("isInboxAgent")
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

func TestInboxAgentNoMachineFails(t *testing.T) {
	h := agentHub(t)
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentQueued, Task: "t"}
	h.inbox.items[it.ID] = it
	h.startInboxAgents(context.Background(), InboxAgentConfig{Enabled: true})
	if it.Agent.State != AgentFailed || !strings.Contains(it.Agent.Error, "machine") {
		t.Fatalf("agent = %+v", it.Agent)
	}
}

func TestInboxAgentPrompt(t *testing.T) {
	it := actionItem("x", time.Now())
	it.Title = `群 "A" <b>`
	it.Agent = &InboxAgent{Task: "查 1013 是否合入"}
	p := inboxAgentPrompt(it, "小明", nil)
	for _, want := range []string{`title="飞书 · 群 &quot;A&quot; &lt;b&gt;"`, "飞书会话 id：f1", "查 1013 是否合入",
		"inbox_draft", "张三：1013 合了吗？", "小明"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
}

func TestAIConfigOver(t *testing.T) {
	base := ai.Config{BaseURL: "http://gw", APIKeyFile: "~/k", Model: "base"}
	got := ai.Config{Model: "other"}.Over(base)
	if got.BaseURL != "http://gw" || got.APIKeyFile != "~/k" || got.Model != "other" {
		t.Fatalf("over = %+v", got)
	}
}

// TestInboxModelsLive runs models 1 and 2 on a sample item against a real gateway:
// JARVIS_LIVE_AI='{"base_url":"…","api_key_file":"…","model":"…"}' go test -run InboxModelsLive -v ./internal/hub
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
	h.triageAI, h.triageAI = ai.New(cfg), ai.New(cfg)
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
	h := agentHub(t)
	h.initTodos(ctx)
	connectAssistant(t, h) // the judge sees the tasks it is working on (assistant.tasks)
	todo, err := h.createTodo(ctx, "修登录崩溃", "", "conv1", "plan_1")
	if err != nil {
		t.Fatal(err)
	}
	model, calls := captureModel(t, `{"handle":true,"task":"回复修复进度","reason":"问进度","task_id":"`+todo.ID+`"}`)
	h.triageAI = model
	it := actionItem("lark:f1:12", time.Now())
	it.Title, it.Sender, it.Preview, it.Source, it.AccountName = "客户端发版群", "陈晓", "登录那个崩溃今天能修好吗？", "lark", "飞书"
	h.inbox.items[it.ID] = it
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
	if p := inboxAgentPromptFor(it, "示例用户", nil, linked, 1); !strings.Contains(p, "正在做的任务「修登录崩溃」") || !strings.Contains(p, "测试通过") {
		t.Fatalf("the agent is not told the task:\n%s", p)
	}

	// A task id it was not shown is not taken.
	model2, _ := captureModel(t, `{"handle":true,"task":"x","reason":"r","task_id":"todo_made_up"}`)
	h.triageAI = model2
	other := actionItem("lark:f2:12", time.Now())
	h.inbox.items[other.ID] = other
	h.judgeOne(ctx, other)
	if other.Agent == nil || other.Agent.TodoID != "" {
		t.Fatalf("made-up task linked: %+v", other.Agent)
	}

	// The draft: the card goes into the task's conversation.
	it.Agent.State, it.Agent.SessionID = AgentRunning, "s_agent"
	if _, err := h.toolInboxDraft(ctx, "s_agent", json.RawMessage(`{"text":"测试已经过了，十点前提 PR"}`)); err != nil {
		t.Fatal(err)
	}
	items, _ := feed(h, "conv1")
	if len(items) != 1 || items[0].Kind != "message" || items[0].Ref != it.ID {
		t.Fatalf("feed = %+v", items)
	}
	var mb assistant.MessageCard
	_ = json.Unmarshal(items[0].Body, &mb)
	if mb.TodoID != todo.ID || mb.Draft != "测试已经过了，十点前提 PR" || mb.Sender != "陈晓" || mb.Chat != "客户端发版群" {
		t.Fatalf("card = %+v", mb)
	}
}
