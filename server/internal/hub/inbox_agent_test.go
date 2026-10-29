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

	"github.com/chyroc/jarvis/server/internal/ai"
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

	h.judgeAI = fakeModel(t, `{"handle":true,"task":"查 1013 是否合入","reason":"可查"}`)
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
	h.judgeAI = fakeModel(t, "```json\n{\"handle\":false,\"reason\":\"只是通知\"}\n```")
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
	h.judgeAI = fakeModel(t, "no json here")
	if h.judgeOne(context.Background(), it) || it.JudgedAt != "" || it.Agent != nil {
		t.Fatalf("judged on a failed call: %+v", it.Agent)
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
	if _, err := h.toolInboxDraft(context.Background(), "s1", json.RawMessage(`{"text":" 合了，MR 在【链接】 ","note":"agent_read 看到已合入"}`)); err != nil {
		t.Fatal(err)
	}
	if a := it.Agent; a.State != AgentDrafted || a.Draft != "合了，MR 在【链接】" || a.Note == "" || a.DraftedAt == nil {
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
	p := inboxAgentPrompt(it, "小明")
	for _, want := range []string{`title="飞书 · 群 &quot;A&quot; &lt;b&gt;"`, "飞书会话 id（lark_messages 用）：f1", "查 1013 是否合入",
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
	h.triageAI, h.judgeAI = ai.New(cfg), ai.New(cfg)
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
