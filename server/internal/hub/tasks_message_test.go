package hub

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// cardHub has a task in conv1 and an inbox item about it (a GitHub-like review thread) whose agent is drafting.
func cardHub(t *testing.T) (*Hub, *replierConnector, *InboxItem) {
	t.Helper()
	ctx := context.Background()
	h := agentHub(t)
	h.initTodos(ctx)
	h.connectors = connector.NewRegistry(nil)
	h.registerConnectors()
	rc := &replierConnector{}
	if err := h.addConnector(rc); err != nil {
		t.Fatal(err)
	}
	todo, err := h.createTodo(ctx, "修角标", "", "conv1", "plan_1")
	if err != nil {
		t.Fatal(err)
	}
	it := &InboxItem{ID: "example:1", Source: "example", Last: "m1", UpdatedAt: time.Now(),
		Connector: &InboxConnector{Thread: "example-org/example-repo#12", CanReply: true},
		Agent:     &InboxAgent{State: AgentRunning, SessionID: "s1", TodoID: todo.ID}}
	h.inbox.items[it.ID] = it
	return h, rc, it
}

func cards(t *testing.T, h *Hub, since time.Time) []FeedItem {
	t.Helper()
	items, _, err := h.feed(context.Background(), "conv1", since)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func cardBody(t *testing.T, it FeedItem) messageBody {
	t.Helper()
	var b messageBody
	if err := json.Unmarshal(it.Body, &b); err != nil {
		t.Fatal(err)
	}
	return b
}

// 发送 on a card: the reply goes out once, the card says so for good — on a reload, after the item opens again and
// the AI 分身 drafts a new card, and to the clients at once.
func TestMessageCardStaysSent(t *testing.T) {
	ctx := context.Background()
	h, rc, it := cardHub(t)
	mux := h.routes()
	if _, err := h.toolInboxDraft(ctx, "s1", json.RawMessage(`{"text":"确实是 bug，谢谢指出，修好了告诉你"}`)); err != nil {
		t.Fatal(err)
	}
	list := cards(t, h, time.Time{})
	if len(list) != 1 || cardSentAt(list[0]) != nil {
		t.Fatalf("feed = %+v", list)
	}
	card := list[0]
	loaded := card.At

	app := &client{out: make(chan []byte, 64)}
	h.mu.Lock()
	h.clients[app] = struct{}{}
	h.mu.Unlock()
	body, _ := json.Marshal(map[string]string{"text": "确实是 bug，谢谢指出（改过）", "card": card.ID})
	if code, out := call(t, mux, "POST", "/api/v1/inbox/example:1/reply", h.cfg.Token, string(body)); code != 200 || out["done_by"] != "replied" {
		t.Fatalf("reply: %d %v", code, out)
	}
	if len(rc.got) != 1 || rc.got[0].Text != "确实是 bug，谢谢指出（改过）" {
		t.Fatalf("sent = %+v", rc.got)
	}
	// The clients hear the card changed; a reload draws it sent, with what went out; so does a client that has it
	// already and asks for what came since.
	heard := false
	for len(app.out) > 0 {
		ev := string(<-app.out)
		heard = heard || strings.Contains(ev, `"assistant_feed"`) && strings.Contains(ev, card.ID) && strings.Contains(ev, "sent_at")
	}
	if !heard {
		t.Fatal("the clients were not told the card was sent")
	}
	for _, since := range []time.Time{{}, loaded} {
		got := cards(t, h, since)
		if len(got) != 1 || cardSentAt(got[0]) == nil || cardBody(t, got[0]).Sent != "确实是 bug，谢谢指出（改过）" {
			t.Fatalf("since %v: feed = %+v", since, got)
		}
	}

	// Tapped again, from another device or after a reload with the old page: nothing more goes out.
	if code, _ := call(t, mux, "POST", "/api/v1/inbox/example:1/reply", h.cfg.Token, string(body)); code != 200 || len(rc.got) != 1 {
		t.Fatalf("sent again: %d %+v", code, rc.got)
	}
	other, _ := json.Marshal(map[string]string{"text": "再说一遍", "card": card.ID})
	if code, _ := call(t, mux, "POST", "/api/v1/inbox/example:1/reply", h.cfg.Token, string(other)); code != 200 || len(rc.got) != 1 {
		t.Fatalf("sent again with other text: %d %+v", code, rc.got)
	}

	// A draft that arrives after the reply went out puts no new card in.
	if _, err := h.toolInboxDraft(ctx, "s1", json.RawMessage(`{"text":"晚到的草稿"}`)); err != nil {
		t.Fatal(err)
	}
	if got := cards(t, h, time.Time{}); len(got) != 1 {
		t.Fatalf("a card for a reply already out: %+v", got)
	}

	// A new message opens the item again and the AI 分身 drafts again: a new card to send; the old one stays sent.
	h.inbox.mu.Lock()
	it.DoneAt, it.DoneBy, it.Last = nil, "", "m2"
	h.inbox.mu.Unlock()
	time.Sleep(2 * time.Millisecond)
	if _, err := h.toolInboxDraft(ctx, "s1", json.RawMessage(`{"text":"已经合了"}`)); err != nil {
		t.Fatal(err)
	}
	got := cards(t, h, time.Time{})
	if len(got) != 2 || got[0].ID != card.ID || cardSentAt(got[0]) == nil || cardSentAt(got[1]) != nil ||
		cardBody(t, got[0]).Sent != "确实是 bug，谢谢指出（改过）" || cardBody(t, got[1]).Draft != "已经合了" {
		t.Fatalf("after a new draft: %+v", got)
	}
}

// Taps that arrive together send one reply.
func TestMessageCardSendsOnce(t *testing.T) {
	ctx := context.Background()
	h, rc, _ := cardHub(t)
	mux := h.routes()
	if _, err := h.toolInboxDraft(ctx, "s1", json.RawMessage(`{"text":"在修了"}`)); err != nil {
		t.Fatal(err)
	}
	card := cards(t, h, time.Time{})[0]
	body, _ := json.Marshal(map[string]string{"text": "在修了", "card": card.ID})
	var wg sync.WaitGroup
	codes := make([]int, 5)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = call(t, mux, "POST", "/api/v1/inbox/example:1/reply", h.cfg.Token, string(body))
		}()
	}
	wg.Wait()
	for _, c := range codes {
		if c != 200 {
			t.Fatalf("codes = %v", codes)
		}
	}
	if len(rc.got) != 1 {
		t.Fatalf("sent %d times", len(rc.got))
	}
}

// A card from before the hub recorded sends: its item was replied to after it came, so it reads as sent (with the
// draft), and stays so when the item opens again.
func TestMessageCardSentBeforeTheRecord(t *testing.T) {
	ctx := context.Background()
	h, _, it := cardHub(t)
	if _, err := h.toolInboxDraft(ctx, "s1", json.RawMessage(`{"text":"在修了"}`)); err != nil {
		t.Fatal(err)
	}
	h.inbox.mu.Lock()
	it.done(time.Now().Add(time.Second), "replied")
	h.inbox.mu.Unlock()
	got := cards(t, h, time.Time{})
	if len(got) != 1 || cardSentAt(got[0]) == nil || cardBody(t, got[0]).Sent != "" || cardBody(t, got[0]).Draft != "在修了" {
		t.Fatalf("feed = %+v", got)
	}
	h.inbox.mu.Lock()
	it.DoneAt, it.DoneBy = nil, ""
	h.inbox.mu.Unlock()
	if got := cards(t, h, time.Time{}); cardSentAt(got[0]) == nil {
		t.Fatal("the card forgot it was sent")
	}

	// Filed without a reply, or replied to before the card came: the card still offers 发送.
	h2, _, it2 := cardHub(t)
	h2.inbox.mu.Lock()
	it2.done(time.Now().Add(-time.Minute), "replied")
	h2.inbox.mu.Unlock()
	it2.DoneBy = ""
	it2.DoneAt = nil
	if _, err := h2.toolInboxDraft(ctx, "s1", json.RawMessage(`{"text":"在修了"}`)); err != nil {
		t.Fatal(err)
	}
	h2.inbox.mu.Lock()
	it2.done(time.Now(), "user")
	h2.inbox.mu.Unlock()
	if got := cards(t, h2, time.Time{}); len(got) != 1 || cardSentAt(got[0]) != nil {
		t.Fatalf("filed, not replied: %+v", got)
	}
}

// The AI 分身's draft goes out with one tap: one that leaves a blank to fill in is sent back to it.
func TestInboxDraftRefusesPlaceholders(t *testing.T) {
	ctx := context.Background()
	h, _, it := cardHub(t)
	for _, text := range []string{"修好了，【预计今晚 / 几点前】合进去", "MR 在 {{链接}}"} {
		_, err := h.toolInboxDraft(ctx, "s1", json.RawMessage(`{"text":`+string(mustJSON(text))+`}`))
		if err == nil || !strings.Contains(err.Error(), "fill in") {
			t.Fatalf("%q: %v", text, err)
		}
	}
	if it.Agent.State != AgentRunning || it.Agent.Draft != "" || len(cards(t, h, time.Time{})) != 0 {
		t.Fatalf("a draft with a blank was taken: %+v", it.Agent)
	}
	if _, err := h.toolInboxDraft(ctx, "s1", json.RawMessage(`{"text":"修好了，合进去了"}`)); err != nil {
		t.Fatal(err)
	}
	if draftPlaceholder("【通知】 不算吗") == "" || draftPlaceholder("周五前给你") != "" {
		t.Fatal("draftPlaceholder")
	}
}
