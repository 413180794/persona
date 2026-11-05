package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// The message cards of the AI 分身's drafts about a task (docs/unified-entry.md「消息卡关联任务」): the card is in the
// task's conversation, and its 发送 is the inbox's reply, once per card.

// cardHub has a task in conv1 and an inbox item about it (a GitHub-like review thread) whose agent is drafting.
func cardHub(t *testing.T) (*inboxHub, *replierConnector, *InboxItem) {
	t.Helper()
	s, f := newTaskService(t)
	in := newFakeInbox()
	f.inbox, in.svc = in, s
	h := &inboxHub{Service: s, f: f, in: in}
	h.inboxReady(context.Background())
	rc := &replierConnector{}
	h.addConnector(rc, false)
	todo := startedTask(t, s, f)
	it := &InboxItem{ID: "example:1", Source: "example", Last: "m1", UpdatedAt: time.Now(),
		Connector: &InboxConnector{Thread: "example-org/example-repo#12", CanReply: true},
		Agent:     &InboxAgent{State: AgentRunning, SessionID: "s1", TodoID: todo.ID}}
	h.inbox.mu.Lock()
	h.inbox.items[it.ID] = it
	h.inbox.mu.Unlock()
	return h, rc, it
}

// cards are the message cards of conv1's feed after since, as the app reads it (not its task's plan and card).
func cards(t *testing.T, h *inboxHub, since time.Time) []FeedItem {
	t.Helper()
	path := "/api/v1/assistant/conversations/conv1/feed"
	if !since.IsZero() {
		path += "?since=" + url.QueryEscape(since.Format(time.RFC3339Nano))
	}
	code, out := inboxCall(t, h.Service, "GET", path, "")
	if code != http.StatusOK {
		t.Fatalf("feed: %d", code)
	}
	raw, _ := json.Marshal(out["items"])
	var items []FeedItem
	_ = json.Unmarshal(raw, &items)
	var cards []FeedItem
	for _, it := range items {
		if it.Kind == feedMessage {
			cards = append(cards, it)
		}
	}
	return cards
}

func cardBody(t *testing.T, it FeedItem) MessageCard {
	t.Helper()
	var b MessageCard
	if err := json.Unmarshal(it.Body, &b); err != nil {
		t.Fatal(err)
	}
	return b
}

// 发送 on a card: the reply goes out once, the card says so for good — on a reload, after the item opens again and
// the AI 分身 drafts a new card, and to the clients at once.
func TestMessageCardStaysSent(t *testing.T) {
	h, rc, it := cardHub(t)
	mux := h.routes()
	draft(t, h, "确实是 bug，谢谢指出，修好了告诉你")
	list := cards(t, h, time.Time{})
	if len(list) != 1 || cardSentAt(list[0]) != nil {
		t.Fatalf("feed = %+v", list)
	}
	card := list[0]
	loaded := card.At

	eventsBefore := len(strings.Fields(h.f.eventTypes()))
	body, _ := json.Marshal(map[string]string{"text": "确实是 bug，谢谢指出（改过）", "card": card.ID})
	if code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/example:1/reply", string(body)); code != 200 || out["done_by"] != "replied" {
		t.Fatalf("reply: %d %v", code, out)
	}
	if len(rc.got) != 1 || rc.got[0].Text != "确实是 bug，谢谢指出（改过）" {
		t.Fatalf("sent = %+v", rc.got)
	}
	// The clients hear the card changed; a reload draws it sent, with what went out; so does a client that has it
	// already and asks for what came since.
	if !slices.Contains(strings.Fields(h.f.eventTypes())[eventsBefore:], "assistant_feed") {
		t.Fatal("the clients were not told the card was sent")
	}
	for _, since := range []time.Time{{}, loaded} {
		got := cards(t, h, since)
		if len(got) != 1 || cardSentAt(got[0]) == nil || cardBody(t, got[0]).Sent != "确实是 bug，谢谢指出（改过）" {
			t.Fatalf("since %v: feed = %+v", since, got)
		}
	}

	// Tapped again, from another device or after a reload with the old page: nothing more goes out.
	if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/example:1/reply", string(body)); code != 200 || len(rc.got) != 1 {
		t.Fatalf("sent again: %d %+v", code, rc.got)
	}
	other, _ := json.Marshal(map[string]string{"text": "再说一遍", "card": card.ID})
	if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/example:1/reply", string(other)); code != 200 || len(rc.got) != 1 {
		t.Fatalf("sent again with other text: %d %+v", code, rc.got)
	}

	// A draft that arrives after the reply went out puts no new card in.
	draft(t, h, "晚到的草稿")
	if got := cards(t, h, time.Time{}); len(got) != 1 {
		t.Fatalf("a card for a reply already out: %+v", got)
	}

	// A new message opens the item again and the AI 分身 drafts again: a new card to send; the old one stays sent.
	h.inbox.mu.Lock()
	it.DoneAt, it.DoneBy, it.Last = nil, "", "m2"
	h.inbox.mu.Unlock()
	time.Sleep(2 * time.Millisecond)
	draft(t, h, "已经合了")
	got := cards(t, h, time.Time{})
	if len(got) != 2 || got[0].ID != card.ID || cardSentAt(got[0]) == nil || cardSentAt(got[1]) != nil ||
		cardBody(t, got[0]).Sent != "确实是 bug，谢谢指出（改过）" || cardBody(t, got[1]).Draft != "已经合了" {
		t.Fatalf("after a new draft: %+v", got)
	}
}

// Taps that arrive together send one reply.
func TestMessageCardSendsOnce(t *testing.T) {
	h, rc, _ := cardHub(t)
	mux := h.routes()
	draft(t, h, "在修了")
	card := cards(t, h, time.Time{})[0]
	body, _ := json.Marshal(map[string]string{"text": "在修了", "card": card.ID})
	var wg sync.WaitGroup
	codes := make([]int, 5)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = inboxCall(t, mux, "POST", "/api/v1/inbox/example:1/reply", string(body))
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
	h, _, it := cardHub(t)
	draft(t, h, "在修了")
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
	draft(t, h2, "在修了")
	h2.inbox.mu.Lock()
	it2.done(time.Now(), "user")
	h2.inbox.mu.Unlock()
	if got := cards(t, h2, time.Time{}); len(got) != 1 || cardSentAt(got[0]) != nil {
		t.Fatalf("filed, not replied: %+v", got)
	}
}

// The AI 分身's draft goes out with one tap: one that leaves a blank to fill in is sent back to it (the assistant's
// rule, assistant.TestInboxDraftRules), and nothing changes on the item.
func TestInboxDraftRefusesPlaceholders(t *testing.T) {
	h, _, it := cardHub(t)
	if _, err := runTool(h.Service, "s1", "inbox_draft", map[string]any{"text": "修好了，【预计今晚 / 几点前】合进去"}); err == nil || !strings.Contains(err.Error(), "fill in") {
		t.Fatalf("a draft with a blank: %v", err)
	}
	if it.Agent.State != AgentRunning || it.Agent.Draft != "" || len(cards(t, h, time.Time{})) != 0 {
		t.Fatalf("a draft with a blank was taken: %+v", it.Agent)
	}
	draft(t, h, "修好了，合进去了")
}

// draft is the AI 分身 in conversation s1 handing in its draft (inbox_draft).
func draft(t *testing.T, h *inboxHub, text string) {
	t.Helper()
	if _, err := runTool(h.Service, "s1", "inbox_draft", map[string]any{"text": text}); err != nil {
		t.Fatalf("inbox_draft: %v", err)
	}
}

// replierConnector records the replies it gets.
type replierConnector struct {
	mu  sync.Mutex
	got []connector.InboxReply
}

func (*replierConnector) ID() string                                              { return "example" }
func (*replierConnector) Name() string                                            { return "Example" }
func (*replierConnector) Collect(context.Context) ([]connector.InboxThing, error) { return nil, nil }
func (*replierConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapInbox}}
}
func (c *replierConnector) Reply(_ context.Context, r connector.InboxReply) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, r)
	return nil
}

// An item the user filed while its AI 分身 worked (已处理, 忽略, read and filed elsewhere) needs nothing more: the
// draft that comes in after puts no card with 发送 in the task's conversation.
func TestMessageCardNotForAFiledItem(t *testing.T) {
	for _, by := range []string{"user", "dismissed", "read"} {
		h, rc, it := cardHub(t)
		h.inbox.mu.Lock()
		it.done(time.Now(), by)
		h.inbox.mu.Unlock()
		draft(t, h, "在修了")
		if got := cards(t, h, time.Time{}); len(got) != 0 {
			t.Fatalf("filed by %s: a card to send: %+v", by, got)
		}
		if len(rc.got) != 0 {
			t.Fatalf("filed by %s: sent %+v", by, rc.got)
		}
	}
}
