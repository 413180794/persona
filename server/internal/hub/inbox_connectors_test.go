package hub

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// inboxConnector is a connector with an inbox, on made-up data.
type inboxConnector struct {
	things  []connector.InboxThing
	replies []string
}

func (*inboxConnector) ID() string   { return "example" }
func (*inboxConnector) Name() string { return "Example" }
func (*inboxConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapInbox}, Kinds: []string{"review_requested"}}
}
func (c *inboxConnector) Collect(context.Context) ([]connector.InboxThing, error) {
	return c.things, nil
}
func (c *inboxConnector) Reply(_ context.Context, r connector.InboxReply) error {
	c.replies = append(c.replies, r.Thread+": "+r.Text)
	return nil
}

func thing(last string, at time.Time) connector.InboxThing {
	return connector.InboxThing{Thread: "example-org/example-repo#12", Kind: "review_requested", Label: "请你 review",
		Title: "Add the example", Sender: "bob", Preview: "请看一下", Context: "上下文", At: at, Last: last,
		URL: "https://github.com/example-org/example-repo/pull/12", ReplyTo: "评论到 example-org/example-repo#12", CanReply: true, Tier: TierAction}
}

func openItems(h *Hub) []*InboxItem {
	var out []*InboxItem
	for _, it := range h.inbox.items {
		if it.open() {
			out = append(out, it)
		}
	}
	return out
}

// One open item per thread gathers its moves; done, a later move starts a new item, the same one does not; a thing
// that is over closes the open item.
func TestConnectorThingsBecomeItems(t *testing.T) {
	h := newTestHub(t)
	h.inbox.init()
	now := time.Now()
	apply := func(things ...connector.InboxThing) []*InboxItem {
		h.inbox.mu.Lock()
		defer h.inbox.mu.Unlock()
		return h.inbox.applyConnector("example", "Example", things, now)
	}

	if ch := apply(thing("a", now.Add(-time.Hour))); len(ch) != 1 {
		t.Fatalf("changed %d", len(ch))
	}
	it := openItems(h)[0]
	if it.Source != "example" || it.AccountName != "Example" || it.Connector.Label != "请你 review" || !it.Connector.CanReply || it.Count != 1 {
		t.Fatalf("%+v %+v", it, it.Connector)
	}
	if ruleTier(it) != TierAction {
		t.Fatalf("rule tier %s", ruleTier(it))
	}
	if ch := apply(thing("a", now.Add(-time.Hour))); len(ch) != 0 {
		t.Fatal("the same move changed the item")
	}
	apply(thing("b", now.Add(-time.Minute)))
	if len(openItems(h)) != 1 || it.Count != 2 || it.Last != "b" {
		t.Fatalf("a later move did not join the open item: %d open, count %d", len(openItems(h)), it.Count)
	}

	it.done(now, "user")
	apply(thing("b", now.Add(-time.Minute)))
	if len(openItems(h)) != 0 {
		t.Fatal("a move already dealt with came back")
	}
	apply(thing("c", now))
	if len(openItems(h)) != 1 || openItems(h)[0] == it {
		t.Fatal("a move after done did not start a new item")
	}

	over := thing("d", now)
	over.Done = connector.DoneRead
	apply(over)
	if len(openItems(h)) != 0 {
		t.Fatal("a thread that is over left its item open")
	}

	// Old or already read: never an item.
	old := thing("e", now.Add(-inboxWindow-time.Hour))
	old.Thread = "example-org/example-repo#1"
	read := thing("f", now)
	read.Thread, read.Read = "example-org/example-repo#2", true
	if ch := apply(old, read); len(ch) != 0 {
		t.Fatalf("old or read things became items: %d", len(ch))
	}
}

// Read elsewhere: an item that does not need the user is done; one that does stays.
func TestConnectorThingReadElsewhere(t *testing.T) {
	h := newTestHub(t)
	h.inbox.init()
	now := time.Now()
	h.inbox.mu.Lock()
	fyi := thing("a", now)
	fyi.Tier = TierFYI // the rules make it FYI
	h.inbox.applyConnector("example", "Example", []connector.InboxThing{fyi}, now)
	it := openItems(h)[0]
	read := fyi
	read.Read = true
	h.inbox.applyConnector("example", "Example", []connector.InboxThing{read}, now)
	h.inbox.mu.Unlock()
	if it.open() || it.DoneBy != "read" {
		t.Fatalf("an FYI read on GitHub is still open: %+v", it)
	}
}

// Old apps never see a connector's items (they would draw a PR as a mail); new ones ask for them. Reply goes to the
// connector once, and files the item as replied.
func TestConnectorItemsListAndReply(t *testing.T) {
	h := newTestHub(t)
	h.inbox.init()
	h.connectors = connector.NewRegistry(nil)
	c := &inboxConnector{things: []connector.InboxThing{thing("a", time.Now())}}
	if err := h.addConnector(c); err != nil {
		t.Fatal(err)
	}
	h.inbox.mu.Lock()
	h.inbox.items["mail:1"] = &InboxItem{ID: "mail:1", Source: "mail", Kind: KindMail, UpdatedAt: time.Now()}
	h.inbox.mu.Unlock()
	if ch := h.collectConnectors(context.Background()); len(ch) != 1 {
		t.Fatalf("collected %d", len(ch))
	}
	mux := h.routes()
	_, old := call(t, mux, "GET", "/api/v1/inbox", h.cfg.Token, "")
	_, all := call(t, mux, "GET", "/api/v1/inbox?sources=all", h.cfg.Token, "")
	if len(old["items"].([]any)) != 1 || len(all["items"].([]any)) != 2 {
		t.Fatalf("old %v\nall %v", old, all)
	}
	id := ""
	for _, it := range openItems(h) { // a map's order: read it once
		if it.Connector != nil {
			id = it.ID
		}
	}
	for range 2 { // pressed twice (phone and watch): sent once
		code, out := call(t, mux, "POST", "/api/v1/inbox/"+id+"/reply", h.cfg.Token, `{"text":"看过了"}`)
		if code != 200 || out["done_by"] != "replied" {
			t.Fatalf("%d %v", code, out)
		}
	}
	if len(c.replies) != 1 || c.replies[0] != "example-org/example-repo#12: 看过了" {
		t.Fatalf("replies %v", c.replies)
	}
}

// settlingConnector settles every "old:" thread and keeps options.
type settlingConnector struct {
	inboxConnector
	opts json.RawMessage
}

func (*settlingConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapInbox, connector.CapSettle, connector.CapOptions},
		Kinds: []string{"review_requested"}}
}

func (*settlingConnector) Settle(_ context.Context, threads []string) map[string]string {
	out := map[string]string{}
	for _, t := range threads {
		if strings.HasPrefix(t, "old:") {
			out[t] = "示例：按新规则合并"
		}
	}
	return out
}
func (c *settlingConnector) Options() json.RawMessage { return c.opts }
func (c *settlingConnector) SetOptions(raw json.RawMessage) error {
	if !json.Valid(raw) || strings.Contains(string(raw), "bad") {
		return errors.New("bad options")
	}
	c.opts = raw
	return nil
}

// Items made under older rules are filed as done with why (not deleted); a rule's tier wins over the model's, also
// for an item already open; options go to the connector and come back in the list.
func TestConnectorRulesSettleAndTier(t *testing.T) {
	h := newTestHub(t)
	h.inbox.init()
	h.connectors = connector.NewRegistry(nil)
	now := time.Now()
	c := &settlingConnector{opts: json.RawMessage(`{"ci":"mine"}`)}
	if err := h.addConnector(c); err != nil {
		t.Fatal(err)
	}
	old := thing("a", now)
	old.Thread, old.Tier = "old:1", ""
	h.inbox.mu.Lock()
	h.inbox.applyConnector("example", "Example", []connector.InboxThing{old}, now)
	h.inbox.mu.Unlock()
	modelTiered := openItems(h)[0]
	modelTiered.Tier, modelTiered.TriagedAt = TierAction, "a" // what the model said before

	c.things = []connector.InboxThing{thing("b", now)}
	c.things[0].Tier, c.things[0].Why = TierFYI, "示例规则：知悉即可"
	h.collectConnectors(context.Background())
	if modelTiered.open() || modelTiered.DoneBy != "read" || modelTiered.Reason != "示例：按新规则合并" {
		t.Fatalf("old item: %+v", modelTiered)
	}
	if _, ok := h.inbox.items[modelTiered.ID]; !ok {
		t.Fatal("the old item was deleted")
	}
	var fresh *InboxItem
	for _, it := range openItems(h) {
		fresh = it
	}
	if fresh == nil || fresh.Tier != TierFYI || fresh.Reason != "示例规则：知悉即可" || fresh.TriagedAt != fresh.Last {
		t.Fatalf("rule tier: %+v", fresh)
	}
	// Same move, the rule now says otherwise (other options): the open item follows.
	c.things[0].Tier, c.things[0].Why = TierAction, "示例规则：需要你"
	h.collectConnectors(context.Background())
	if fresh.Tier != TierAction || fresh.Reason != "示例规则：需要你" {
		t.Fatalf("re-tiered: %+v", fresh)
	}

	mux := h.routes()
	if code, _ := call(t, mux, "PUT", "/api/v1/connectors/example/settings", h.cfg.Token, `{"options":{"ci":"bad"}}`); code != 400 {
		t.Fatalf("bad options: %d", code)
	}
	code, out := call(t, mux, "PUT", "/api/v1/connectors/example/settings", h.cfg.Token, `{"options":{"ci":"all"}}`)
	if code != 200 || out["options"].(map[string]any)["ci"] != "all" {
		t.Fatalf("%d %v", code, out)
	}
}
