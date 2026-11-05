package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/github"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/mail"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The hub's half of the inbox (inbox.go): reading the sources into a round, handing it to the manager, answering what
// the manager asks of the sources, relaying the routes. The inbox itself (items, triage, events, mutes…) is the
// manager's and tested there (internal/assistant's inbox*_test.go).

// inboxConnector is a connector with an inbox, on made-up data.
type inboxConnector struct {
	mu      sync.Mutex
	things  []connector.InboxThing
	replies []connector.InboxReply
	collect atomic.Int32
	failing error
}

func (*inboxConnector) ID() string   { return "example" }
func (*inboxConnector) Name() string { return "Example" }
func (*inboxConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapInbox}, Kinds: []string{"review_requested"}}
}
func (c *inboxConnector) Collect(context.Context) ([]connector.InboxThing, error) {
	c.collect.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.things), c.failing
}
func (c *inboxConnector) Reply(_ context.Context, r connector.InboxReply) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.replies = append(c.replies, r)
	return nil
}

func thing(last string, at time.Time) connector.InboxThing {
	return connector.InboxThing{Thread: "example-org/example-repo#12", Kind: "review_requested", Label: "请你 review",
		Title: "Add the example", Sender: "bob", Preview: "请看一下", Context: "上下文", At: at, Last: last,
		URL: "https://github.com/example-org/example-repo/pull/12", ReplyTo: "评论到 example-org/example-repo#12", CanReply: true,
		Tier: connector.TierAction}
}

// groupingConnector relates batches and writes events (group, meta) and settles threads; it records what it was
// asked.
type groupingConnector struct {
	inboxConnector
	tools     []connector.GroupToolCall
	appends   []connector.GroupAppend
	fallbacks []connector.GroupFallback
}

func (*groupingConnector) ID() string { return "examplechat" }
func (*groupingConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapInbox, connector.CapGroup, connector.CapMeta, connector.CapSettle},
		Kinds: []string{"group"}, MetaDescription: "  示例 meta 说明  "}
}
func (g *groupingConnector) GroupTool(_ context.Context, c connector.GroupToolCall) (json.RawMessage, error) {
	g.tools = append(g.tools, c)
	if c.Name == connector.GroupSearch {
		return nil, &rpc.Error{Code: "unknown_method", Message: "no search"}
	}
	return json.RawMessage(`{"messages":[{"id":"m1","text":"示例","avatar":"avatar:1"}]}`), nil
}
func (g *groupingConnector) GroupFallback(_ context.Context, f connector.GroupFallback) (connector.GroupFallbackResult, error) {
	g.fallbacks = append(g.fallbacks, f)
	return connector.GroupFallbackResult{New: []connector.GroupNew{{Key: "k", Messages: []string{"m1"}, RuleTier: connector.TierFYI}}}, nil
}
func (g *groupingConnector) GroupAppend(_ context.Context, a connector.GroupAppend) (connector.InboxThing, error) {
	g.appends = append(g.appends, a)
	return connector.InboxThing{ID: "examplechat:e1", Thread: a.Thread, Last: "m1"}, nil
}
func (*groupingConnector) Settle(_ context.Context, threads []string) map[string]string {
	out := map[string]string{}
	for _, t := range threads {
		if strings.HasPrefix(t, "old:") {
			out[t] = "示例：按新规则合并"
		}
	}
	return out
}

// fakeManager is the manager on the bus for the hub's tests: it answers the inbox's calls from what it keeps and
// records what it was asked.
type fakeManager struct {
	mu        sync.Mutex
	http      func(ctx context.Context, r assistantwire.HTTPRequest) (any, error) // http.request, when set
	rounds    []assistantwire.InboxRound
	items     map[string]json.RawMessage
	forgotten []string
	repos     map[string][]string
	asked     []string
}

// connectFakeManager connects a fake manager to h's bus (identities in memory without a database).
func connectFakeManager(t *testing.T, h *Hub) (*fakeManager, *rawBus) {
	t.Helper()
	if h.busStore() == nil {
		h.bus.idents = &memIdents{}
	}
	mux := http.NewServeMux()
	h.busRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	m := &fakeManager{items: map[string]json.RawMessage{}, repos: map[string][]string{}}
	r := helloAssistant(t, h, srv, m.handle)
	eventually(t, 5*time.Second, "the manager connected", func() bool { return h.busConnOf(assistantwire.AssistantID) != nil })
	t.Cleanup(func() {
		for _, c := range h.stopBus() {
			_ = c.ws.CloseNow()
		}
	})
	return m, r
}

func (m *fakeManager) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	m.mu.Lock()
	if serve := m.http; method == assistantwire.MethodHTTP && serve != nil {
		m.mu.Unlock()
		var r assistantwire.HTTPRequest
		if err := json.Unmarshal(params, &r); err != nil {
			return nil, err
		}
		return serve(ctx, r)
	}
	defer m.mu.Unlock()
	m.asked = append(m.asked, method)
	switch method {
	case assistantwire.MethodTools:
		return map[string]any{"tools": []any{}}, nil
	case assistantwire.MethodInboxCollected:
		var r assistantwire.InboxRound
		if err := json.Unmarshal(params, &r); err != nil {
			return nil, err
		}
		m.rounds = append(m.rounds, r)
		return map[string]any{}, nil
	case assistantwire.MethodInboxItem:
		var a struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(params, &a)
		raw, ok := m.items[a.ID]
		return assistantwire.InboxItemResult{Item: raw, OK: ok}, nil
	case assistantwire.MethodInboxForget:
		var a struct {
			Source string `json:"source"`
		}
		_ = json.Unmarshal(params, &a)
		m.forgotten = append(m.forgotten, a.Source)
		return map[string]any{"deleted": 1}, nil
	case assistantwire.MethodInboxRepoMutes:
		var a assistantwire.InboxRepoMutes
		_ = json.Unmarshal(params, &a)
		if a.Set {
			if slices.ContainsFunc(a.Repos, func(r string) bool { return !strings.Contains(r, "/") }) {
				return nil, errors.New("not owner/name")
			}
			if a.Add {
				m.repos[a.Source] = append(m.repos[a.Source], a.Repos...)
			} else {
				m.repos[a.Source] = a.Repos
			}
		}
		return map[string]any{"repos": m.repos[a.Source]}, nil
	}
	return map[string]any{}, nil
}

func (m *fakeManager) lastRound(t *testing.T) assistantwire.InboxRound {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.rounds) == 0 {
		t.Fatal("no round reached the manager")
	}
	return m.rounds[len(m.rounds)-1]
}

func sourceIn(r assistantwire.InboxRound, id string) (assistantwire.InboxSource, bool) {
	for _, s := range r.Sources {
		if s.ID == id {
			return s, true
		}
	}
	return assistantwire.InboxSource{}, false
}

// inboxMail is the hub's mail on made-up data: one account, its latest messages, their full texts, what it sent.
type inboxMail struct {
	Mail
	mu   sync.Mutex
	msgs []mail.MailSummary
	full map[uint32]*mail.MailMessage
	sent []mail.SendRequest
}

func (*inboxMail) Tools() []proto.Tool   { return nil } // registering the mail connector lists them
func (*inboxMail) Handler() http.Handler { return http.NotFoundHandler() }
func (*inboxMail) ListAccounts() ([]mail.MailAccount, error) {
	return []mail.MailAccount{{ID: "acc1", Email: "me@example.com"}}, nil
}
func (m *inboxMail) ListMessages(context.Context, mail.ListOptions) (*mail.ListResult, error) {
	return &mail.ListResult{Messages: m.msgs}, nil
}
func (m *inboxMail) GetMessage(_ context.Context, _ string, uid uint32, _ string) (*mail.MailMessage, error) {
	if f := m.full[uid]; f != nil {
		return f, nil
	}
	return nil, fmt.Errorf("no mail %d", uid)
}
func (m *inboxMail) Send(_ context.Context, r mail.SendRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, r)
	return nil
}

// A round reads every connector with an inbox the way the manager needs it: its things (vetted: an unknown tier
// dropped, rules trimmed and cut), what it can do (group, meta, settle) and how a plugin describes its kinds; one
// switched off for the inbox or not signed in is read with nothing; one that failed is left out; mail only on a full
// round. The round goes to the manager, which answers before the next one.
func TestInboxRoundReadsTheSources(t *testing.T) {
	h := newTestHub(t)
	m := &inboxMail{msgs: []mail.MailSummary{{UID: 9, Mailbox: "INBOX", Subject: "示例周报", From: mail.Address{Name: "李四", Email: "li@example.com"},
		To: []mail.Address{{Email: "me@example.com"}}, Date: time.Now().UTC().Format(time.RFC3339), Snippet: "这周"}}}
	h.mail = m
	h.connectors = connector.NewRegistry(nil)
	h.registerConnectors()
	plain := &inboxConnector{}
	odd := thing("a", time.Now())
	odd.Tier, odd.Rules = "urgent", "  "+strings.Repeat("规", thingRulesMost+10)+"  "
	plain.things = []connector.InboxThing{odd}
	g := &groupingConnector{}
	g.things = []connector.InboxThing{{Thread: "chat:C1", Kind: "group", Last: "m1", Group: &connector.ThingGroup{Batch: "b1", New: []string{"m1"}}}}
	for _, c := range []connector.Connector{plain, g} {
		if err := h.addConnector(c); err != nil {
			t.Fatal(err)
		}
	}
	extensionKinds.Store("examplechat/group", "示例群里的新消息")
	t.Cleanup(func() { extensionKinds.Delete("examplechat/group") })
	fm, _ := connectFakeManager(t, h)
	ctx := context.Background()

	if err := h.collectInbox(ctx, roundPoll); err != nil {
		t.Fatal(err)
	}
	r := fm.lastRound(t)
	if r.Round != assistantwire.RoundPoll || r.Mail != nil || r.Names["example"] != "Example" || r.Names["mail"] != "邮件" {
		t.Fatalf("a poll round: %+v", r)
	}
	src, ok := sourceIn(r, "example")
	if !ok || len(src.Things) != 1 || src.Things[0].Tier != "" || len([]rune(src.Things[0].Rules)) != thingRulesMost+1 || src.Group || src.Settles {
		t.Fatalf("the plain connector: %+v", src)
	}
	src, ok = sourceIn(r, "examplechat")
	if !ok || !src.Group || !src.Meta || !src.Settles || src.MetaDescription != "示例 meta 说明" || src.Kinds["group"] != "示例群里的新消息" ||
		len(src.Things) != 1 || src.Things[0].Group == nil {
		t.Fatalf("the grouping connector: %+v", src)
	}

	// Switched off for the inbox: read with nothing; failing: left out; a full round reads mail too.
	_ = h.connectors.SetFeed("example", connector.FeedInbox, false)
	g.failing = errors.New("示例：读不了")
	if err := h.collectInbox(ctx, roundFull); err != nil {
		t.Fatal(err)
	}
	r = fm.lastRound(t)
	if src, ok := sourceIn(r, "example"); !ok || len(src.Things) != 0 || src.Settles {
		t.Fatalf("switched off: %+v", src)
	}
	if _, ok := sourceIn(r, "examplechat"); ok {
		t.Fatal("a source that could not be read was handed over")
	}
	if r.Mail == nil || len(r.Mail.Accounts) != 1 || r.Mail.Accounts[0].Email != "me@example.com" || len(r.Mail.Accounts[0].Messages) != 1 ||
		r.Mail.Accounts[0].Messages[0].UID != 9 || r.Mail.Accounts[0].Messages[0].From.Name != "李四" {
		t.Fatalf("a full round's mail: %+v", r.Mail)
	}
	// A Feishu round reads no connector.
	before := plain.collect.Load()
	if err := h.collectInbox(ctx, roundChats); err != nil || plain.collect.Load() != before || len(fm.lastRound(t).Sources) != 0 {
		t.Fatalf("a round of the chats read the connectors: %v", err)
	}
}

// While the manager is not connected the sources are still read and what they had is kept for it: a thing that left
// the source's window meanwhile, and a mail that left the newest page, reach the manager with the first round once
// it is back (a thread as last read); the routes answer assistant_offline meanwhile.
func TestInboxKeepsWhatWasReadForTheManager(t *testing.T) {
	h := newTestHub(t)
	now := time.Now()
	mailAt := now.UTC().Format(time.RFC3339)
	m := &inboxMail{msgs: []mail.MailSummary{{UID: 1, Mailbox: "INBOX", Subject: "第一封", Date: mailAt}}}
	h.mail = m
	h.connectors = connector.NewRegistry(nil)
	h.registerConnectors()
	c := &inboxConnector{things: []connector.InboxThing{thing("a", now)}}
	_ = h.addConnector(c)
	h.inbox.init()
	if err := h.collectInbox(context.Background(), roundFull); !errors.Is(err, errAssistantOffline) || c.collect.Load() != 1 || !h.inbox.owed.Load() {
		t.Fatalf("without the manager: %v, %d collect(s)", err, c.collect.Load())
	}
	// Meanwhile the first thing and the first mail left the window; the second thread moved.
	second := thing("b", now)
	second.Thread = "example-org/example-repo#13"
	c.mu.Lock()
	c.things = []connector.InboxThing{second}
	c.mu.Unlock()
	m.msgs = []mail.MailSummary{{UID: 2, Mailbox: "INBOX", Subject: "第二封", Date: mailAt}}
	if err := h.collectInbox(context.Background(), roundFull); !errors.Is(err, errAssistantOffline) {
		t.Fatal(err)
	}
	mux := h.routes()
	for _, path := range []string{"/api/v1/inbox", "/api/v1/inbox/x", "/api/v1/inbox/mutes"} {
		if code, out := call(t, mux, "GET", path, h.cfg.Token, ""); code != http.StatusServiceUnavailable || out["code"] != "assistant_offline" {
			t.Fatalf("%s without the manager: %d %v", path, code, out)
		}
	}
	for _, path := range []string{"/api/v1/inbox/refresh", "/api/v1/inbox/x/reply", "/api/v1/inbox/done", "/api/v1/translate"} {
		if code, out := call(t, mux, "POST", path, h.cfg.Token, `{"text":"好的"}`); code != http.StatusServiceUnavailable || out["code"] != "assistant_offline" {
			t.Fatalf("%s without the manager: %d %v", path, code, out)
		}
	}
	fm, _ := connectFakeManager(t, h)
	// Connected: a full round is owed (the loop runs it after the debounce; here by hand), and it clears the debt.
	eventually(t, 5*time.Second, "the kick", func() bool { return len(h.inbox.kick) == 1 })
	if r := h.inbox.kickedRound(); r != roundPoll {
		t.Fatalf("the kick asks for round %d", r)
	}
	if err := h.collectInbox(context.Background(), roundPoll); err != nil || h.inbox.owed.Load() || h.inbox.held != nil {
		t.Fatalf("the round when it is back: %v", err)
	}
	r := fm.lastRound(t)
	src, ok := sourceIn(r, "example")
	if r.Round != assistantwire.RoundFull || r.Seq == 0 || r.Epoch == "" || !ok || len(src.Things) != 2 ||
		src.Things[0].Last != "a" || src.Things[1].Last != "b" {
		t.Fatalf("what was kept: %+v", r)
	}
	if r.Mail == nil || len(r.Mail.Accounts) != 1 || len(r.Mail.Accounts[0].Messages) != 2 {
		t.Fatalf("the mail kept: %+v", r.Mail)
	}
	// Handed over: nothing is kept any more, and the rounds count on.
	seq := r.Seq
	if err := h.collectInbox(context.Background(), roundPoll); err != nil {
		t.Fatal(err)
	}
	r = fm.lastRound(t)
	if src, _ := sourceIn(r, "example"); len(src.Things) != 1 || r.Mail != nil || r.Seq != seq+1 {
		t.Fatalf("the next round: %+v", r)
	}
}

// What is kept is bounded, and a source read as switched off keeps nothing.
func TestMergeRoundsBounds(t *testing.T) {
	var old, n assistantwire.InboxRound
	n.Names = map[string]string{"example": "Example"}
	src := assistantwire.InboxSource{ID: "example"}
	for i := range heldThingsMost {
		src.Things = append(src.Things, connector.InboxThing{Thread: fmt.Sprint("t", i), Last: "1"})
	}
	old.Sources = []assistantwire.InboxSource{src}
	n.Sources = []assistantwire.InboxSource{{ID: "example", Things: []connector.InboxThing{{Thread: "new", Last: "1"}, {Thread: "t5", Last: "2"}}}}
	got := mergeRounds(old, n)
	things := got.Sources[0].Things
	if len(things) != heldThingsMost || things[len(things)-1].Thread != "t5" || things[len(things)-1].Last != "2" || things[0].Thread != "t1" {
		t.Fatalf("merged: %d things, first %+v, last %+v", len(things), things[0], things[len(things)-1])
	}
	n.Sources = []assistantwire.InboxSource{{ID: "example", Off: true, Things: []connector.InboxThing{}}}
	if got := mergeRounds(old, n); len(got.Sources[0].Things) != 0 {
		t.Fatal("a source switched off kept its things")
	}
	n.Sources, n.Names = nil, map[string]string{}
	if got := mergeRounds(old, n); len(got.Sources) != 0 {
		t.Fatal("a source the hub no longer has was kept")
	}
}

// What the manager asks of the sources: a reply through the item's connector, the threads a connector settles, its
// group tools (an answer cut for the model, a refusal with its code), its fallback and an event's append (a new one's
// meta null), a mail's text and a reply to it, the hub's inbox settings.
func TestInboxSourcesAnswerTheManager(t *testing.T) {
	h := newTestHub(t)
	ctx := context.Background()
	m := &inboxMail{full: map[uint32]*mail.MailMessage{9: {MailSummary: mail.MailSummary{Subject: "季度汇报", MessageID: "<m9@example.com>",
		From: mail.Address{Name: "HR", Email: "hr@example.com"}, Date: "2026-10-27T03:00:00Z"}, HTML: "<p>请周五前提交</p>"}}}
	h.mail = m
	h.connectors = connector.NewRegistry(nil)
	g := &groupingConnector{}
	_ = h.addConnector(g)
	h.cfg.Inbox.Rules = "  示例规则  "
	h.cfg.Inbox.Agent = InboxAgentConfig{Enabled: true, Concurrency: 2}
	h.cfg.Lark.Account = 3
	app := func(app, method string, params any) (json.RawMessage, error) {
		raw, _ := json.Marshal(params)
		return assistantLink{h}.AppCall(ctx, assistantwire.AppCall{AppID: app, Method: method, Params: raw})
	}
	inbox := func(method string, params any) (json.RawMessage, error) {
		return app(assistantwire.AppInbox, method, params)
	}

	if _, err := inbox(assistantwire.InboxReply, map[string]any{"source": "examplechat", "reply": connector.InboxReply{Thread: "chat:C1", Text: "好的"}}); err != nil ||
		len(g.replies) != 1 || g.replies[0].Text != "好的" {
		t.Fatalf("reply: %v %+v", err, g.replies)
	}
	if _, err := inbox(assistantwire.InboxReply, map[string]any{"source": "lark", "reply": connector.InboxReply{Thread: "chat:1"}}); !errors.Is(err, errCannotReply) {
		t.Fatalf("a reply through a connector the hub has not: %v", err)
	}
	if out, err := inbox(assistantwire.InboxSettle, map[string]any{"source": "examplechat", "threads": []string{"old:1", "new:2"}}); err != nil ||
		string(out) != `{"over":{"old:1":"示例：按新规则合并"}}` {
		t.Fatalf("settle: %s %v", out, err)
	}
	out, err := inbox(assistantwire.InboxGroupTool, map[string]any{"source": "examplechat", "call": connector.GroupToolCall{Thread: "chat:C1", Name: connector.GroupMessagesBefore}})
	if err != nil || strings.Contains(string(out), "avatar") || !strings.Contains(string(out), `示例`) {
		t.Fatalf("a group tool: %s %v", out, err)
	}
	var re *rpc.Error
	if _, err := inbox(assistantwire.InboxGroupTool, map[string]any{"source": "examplechat", "call": connector.GroupToolCall{Name: connector.GroupSearch}}); !errors.As(busCodedError(err), &re) || re.Code != "unknown_method" {
		t.Fatalf("a tool the connector has not: %v", err)
	}
	if out, err := inbox(assistantwire.InboxGroupFallback, map[string]any{"source": "examplechat", "fallback": connector.GroupFallback{Thread: "chat:C1"}}); err != nil ||
		!strings.Contains(string(out), `"rule_tier":"fyi"`) || len(g.fallbacks) != 1 {
		t.Fatalf("fallback: %s %v", out, err)
	}
	if out, err := inbox(assistantwire.InboxGroupAppend, json.RawMessage(`{"source":"examplechat","append":{"thread":"chat:C1","event":"","meta":null,"messages":["m1"]}}`)); err != nil ||
		!strings.Contains(string(out), `"id":"examplechat:e1"`) || len(g.appends) != 1 || g.appends[0].Meta != nil {
		t.Fatalf("append: %s %v, %+v", out, err, g.appends)
	}
	if out, err := inbox(assistantwire.InboxMailBody, assistantwire.InboxMailRef{AccountID: "acc1", UID: 9, Mailbox: "INBOX"}); err != nil ||
		!strings.Contains(string(out), `"text":"请周五前提交"`) || !strings.Contains(string(out), `"name":"HR"`) {
		t.Fatalf("a mail's text: %s %v", out, err)
	}
	if _, err := inbox(assistantwire.InboxReplyMail, map[string]any{"account_id": "acc1", "uid": 9, "mailbox": "INBOX", "text": "收到"}); err != nil ||
		len(m.sent) != 1 || m.sent[0].Subject != "Re: 季度汇报" || m.sent[0].InReplyTo != "<m9@example.com>" || !strings.HasPrefix(m.sent[0].Text, "收到") {
		t.Fatalf("a mail reply: %v %+v", err, m.sent)
	}
	extensionConnectors.Store("examplechat", "dir:example")
	t.Cleanup(func() { extensionConnectors.Delete("examplechat") })
	var cfg assistantwire.InboxConfig
	out, err = inbox(assistantwire.InboxConfigMethod, map[string]any{})
	if err != nil || json.Unmarshal(out, &cfg) != nil || cfg.Rules != "示例规则" || !cfg.Agent.Enabled || cfg.Agent.Concurrency != 2 || cfg.LarkAccount != 3 ||
		!slices.Contains(cfg.Extensions, "examplechat") {
		t.Fatalf("the settings: %s %v", out, err)
	}
	// A mail's text for translation keeps the HTML's paragraphs and links.
	if out, err := inbox(assistantwire.InboxMailBody, assistantwire.InboxMailRef{AccountID: "acc1", UID: 9, Mailbox: "INBOX", Readable: true}); err != nil ||
		!strings.Contains(string(out), "请周五前提交") {
		t.Fatalf("a mail's readable text: %s %v", out, err)
	}

	// Asked again under the same send id (the answer was lost on the way), a reply is not sent again; one cut off
	// before is in doubt and not sent either; an event's append asked again under its id is not written again.
	sent := newSentStore()
	h.store = sent
	for i := range 2 {
		out, err := inbox(assistantwire.InboxReply, map[string]any{"source": "examplechat", "reply": connector.InboxReply{Thread: "chat:C1", Text: "再说一次"},
			"send_id": "send-1"})
		if err != nil || (i == 1) != strings.Contains(string(out), `"already":true`) {
			t.Fatalf("reply %d under one send id: %s %v", i, out, err)
		}
		out, err = inbox(assistantwire.InboxReplyMail, map[string]any{"account_id": "acc1", "uid": 9, "mailbox": "INBOX", "text": "再确认", "send_id": "send-2"})
		if err != nil || (i == 1) != strings.Contains(string(out), `"already":true`) {
			t.Fatalf("mail reply %d under one send id: %s %v", i, out, err)
		}
		out, err = inbox(assistantwire.InboxGroupAppend, json.RawMessage(`{"source":"examplechat","append":{"thread":"chat:C1","event":"examplechat:e1","meta":{},"messages":["m2"]},"append_id":"append-1"}`))
		if err != nil || !strings.Contains(string(out), `"id":"examplechat:e1"`) {
			t.Fatalf("append %d under one id: %s %v", i, out, err)
		}
	}
	if len(g.replies) != 2 || len(m.sent) != 2 || len(g.appends) != 2 {
		t.Fatalf("sent again: %d replies, %d mails, %d appends", len(g.replies), len(m.sent), len(g.appends))
	}
	sent.rows["inbox:send-3"] = sendUnknown
	if _, err := inbox(assistantwire.InboxReplyMail, map[string]any{"account_id": "acc1", "uid": 9, "mailbox": "INBOX", "text": "又一次", "send_id": "send-3"}); !errors.As(busCodedError(err), &re) ||
		re.Code != assistantwire.CodeAssistantInDoubt || len(m.sent) != 2 {
		t.Fatalf("a send cut off before: %v, %d mails", err, len(m.sent))
	}
}

// An answer-only device (the watch) replies with text only, the hub holding it to that before the manager sees the
// request: no documents' status moves, no "/agent …".
func TestInboxReplyFromTheWatchIsTextOnly(t *testing.T) {
	h := newTestHub(t)
	fm, _ := connectFakeManager(t, h)
	var relayed atomic.Int32
	fm.mu.Lock()
	fm.http = func(context.Context, assistantwire.HTTPRequest) (any, error) {
		relayed.Add(1)
		return assistantwire.HTTPResponse{Status: http.StatusOK, Body: []byte(`{"id":"x"}`)}, nil
	}
	fm.mu.Unlock()
	api := http.NewServeMux()
	h.inboxRoutesRelayed(api)
	reply := func(body string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/inbox/x/reply", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{Role: roleAnswer}))
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		return w.Code
	}
	for _, body := range []string{`{"text":"/agent 看看构建"}`, `{"text":"好","changes":[{"ref":"r1","field":"状态","value":"完成"}]}`} {
		if code := reply(body); code != http.StatusForbidden || relayed.Load() != 0 {
			t.Fatalf("%s from the watch: %d, %d relayed", body, code, relayed.Load())
		}
	}
	if code := reply(`{"text":"好的"}`); code != http.StatusOK || relayed.Load() != 1 {
		t.Fatalf("a text reply from the watch: %d, %d relayed", code, relayed.Load())
	}
}

// receiptStore keeps the apps' operation receipts (client_ops) and drops one when asked (ForgetClientOp).
type receiptStore struct {
	*memStore
	ops *clientOpsStore
}

func (s *receiptStore) BeginClientOp(ctx context.Context, op db.ClientOp) (*db.ClientOp, bool, error) {
	return s.ops.BeginClientOp(ctx, op)
}
func (s *receiptStore) ClientOp(ctx context.Context, id string) (*db.ClientOp, error) {
	return s.ops.ClientOp(ctx, id)
}
func (s *receiptStore) FinishClientOp(ctx context.Context, id, state string, result json.RawMessage) error {
	return s.ops.FinishClientOp(ctx, id, state, result)
}
func (s *receiptStore) ForgetClientOp(_ context.Context, id string) error {
	s.ops.mu.Lock()
	defer s.ops.mu.Unlock()
	delete(s.ops.ops, id)
	return nil
}

// The link to the manager dropping while a reply is with it: the reply may have gone out, so the app is told so
// (assistant_in_doubt, delivered unknown), not that the assistant is offline; the receipt is not kept, so the same
// reply asked again reaches the manager (whose send id has the hub send it once) instead of the stored answer.
func TestInboxReplyCutOffIsInDoubt(t *testing.T) {
	h := newTestHub(t)
	store := &receiptStore{memStore: newMemStore(), ops: &clientOpsStore{ops: map[string]db.ClientOp{}}}
	h.store = store
	fm, link := connectFakeManager(t, h)
	got := make(chan struct{}, 1)
	fm.mu.Lock()
	fm.http = func(ctx context.Context, r assistantwire.HTTPRequest) (any, error) {
		got <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	fm.mu.Unlock()
	go func() {
		<-got
		_ = link.ws.CloseNow()
	}()
	mux := h.routes()
	body := `{"text":"好的","client_id":"c1"}`
	code, out := call(t, mux, "POST", "/api/v1/inbox/x/reply", h.cfg.Token, body)
	if code != http.StatusBadGateway || out["code"] != assistantwire.CodeAssistantInDoubt || out["delivered"] != "unknown" {
		t.Fatalf("a reply cut off: %d %v", code, out)
	}
	store.ops.mu.Lock()
	kept := len(store.ops.ops)
	store.ops.mu.Unlock()
	if kept != 0 {
		t.Fatal("the receipt of a reply in doubt was kept")
	}
	fm2, _ := connectFakeManager(t, h)
	fm2.mu.Lock()
	fm2.http = func(context.Context, assistantwire.HTTPRequest) (any, error) {
		return assistantwire.HTTPResponse{Status: http.StatusOK, Body: []byte(`{"id":"x","done_by":"replied"}`)}, nil
	}
	fm2.mu.Unlock()
	if code, out := call(t, mux, "POST", "/api/v1/inbox/x/reply", h.cfg.Token, body); code != http.StatusOK || out["done_by"] != "replied" {
		t.Fatalf("the same reply asked again: %d %v", code, out)
	}
	// Answered: the receipt is kept now, and a repeat gets the answer without the manager.
	fm2.mu.Lock()
	fm2.http = func(context.Context, assistantwire.HTTPRequest) (any, error) { return nil, errors.New("asked twice") }
	fm2.mu.Unlock()
	if code, out := call(t, mux, "POST", "/api/v1/inbox/x/reply", h.cfg.Token, body); code != http.StatusOK || out["done_by"] != "replied" {
		t.Fatalf("a repeat after the answer: %d %v", code, out)
	}
}

// inbox.changed: the extensions hear of the items, the apps get {"type":"inbox"} (also when no item changed: the
// triage status moved).
func TestInboxChangedTellsTheApps(t *testing.T) {
	h := newTestHub(t)
	app := wsApp(t, h)
	_, r := connectFakeManager(t, h)
	for _, items := range [][]assistantwire.InboxChangedItem{{{ID: "example:1", Source: "example", Kind: "dm", Title: "示例"}}, {}} {
		if err := r.peer.Notify(assistantwire.EventInboxChanged, assistantwire.InboxChanged{Items: items}); err != nil {
			t.Fatal(err)
		}
		select {
		case raw := <-app.out:
			if !strings.Contains(string(raw), `"type":"inbox"`) {
				t.Fatalf("the apps got %s", raw)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the apps were not told")
		}
	}
}

// GitHub's muted repositories are the inbox's (the manager's) mutes: the ones its options kept move there when the
// manager connects; its settings list them from there and set them there; a repository without its owner is refused.
func TestGitHubMutedReposAreTheInboxMutes(t *testing.T) {
	h := newTestHub(t)
	h.store = newKeepStore()
	h.connectors = connector.NewRegistry(nil)
	ctx := context.Background()
	gh := newGitHubForTest(t, []string{"Example-Org/Old-Site"})
	if err := h.addConnector(gh); err != nil {
		t.Fatal(err)
	}
	fm, link := connectFakeManager(t, h)
	eventually(t, 5*time.Second, "the old ones moved", func() bool {
		fm.mu.Lock()
		defer fm.mu.Unlock()
		return slices.Equal(fm.repos["github"], []string{"Example-Org/Old-Site"})
	})
	eventually(t, 5*time.Second, "GitHub's options emptied", func() bool {
		var kept struct {
			MutedRepos []string `json:"muted_repos"`
		}
		_ = json.Unmarshal(gh.Options(), &kept)
		return len(kept.MutedRepos) == 0
	})
	listed := func() []string {
		for _, info := range h.listedConnectors(ctx, true) {
			if info.ID == "github" {
				var o struct {
					MutedRepos []string `json:"muted_repos"`
				}
				_ = json.Unmarshal(info.Options, &o)
				return o.MutedRepos
			}
		}
		return nil
	}
	if got := listed(); !slices.Equal(got, []string{"Example-Org/Old-Site"}) {
		t.Fatalf("GitHub's page lists %v", got)
	}
	if err := h.setConnectorOptions(ctx, "github", json.RawMessage(`{"ci":"all","muted_repos":["example-org/new"],"ignore_branches":["train/*"]}`)); err != nil {
		t.Fatal(err)
	}
	if got := listed(); !slices.Equal(got, []string{"example-org/new"}) {
		t.Fatalf("after setting: %v", got)
	}
	var o struct {
		CI string `json:"ci"`
	}
	_ = json.Unmarshal(gh.Options(), &o)
	if o.CI != "all" {
		t.Fatalf("the CI choice did not reach GitHub: %s", gh.Options())
	}
	if err := h.setConnectorOptions(ctx, "github", json.RawMessage(`{"muted_repos":["nope"]}`)); err == nil {
		t.Fatal("a repository without its owner was taken")
	}
	// Options that do not list them (no muted_repos, or null) leave the mutes as they are.
	for _, opts := range []string{`{"ci":"off"}`, `{"ci":"off","muted_repos":null}`} {
		if err := h.setConnectorOptions(ctx, "github", json.RawMessage(opts)); err != nil {
			t.Fatal(err)
		}
		if got := listed(); !slices.Equal(got, []string{"example-org/new"}) {
			t.Fatalf("after saving %s: %v", opts, got)
		}
	}
	// The manager away: GitHub lists no options (the apps draw no inbox section for it): showing none muted would be
	// untrue, and saving that would unmute every repository.
	_ = link.ws.CloseNow()
	eventually(t, 5*time.Second, "the manager gone", func() bool { return h.busConnOf(assistantwire.AssistantID) == nil })
	for _, info := range h.listedConnectors(ctx, true) {
		if info.ID == "github" && info.Options != nil {
			t.Fatalf("GitHub's options without the manager: %s", info.Options)
		}
	}
}

// An extension's inbox_item slot draws the item the manager keeps (inbox.item); none while it can't say.
func TestUIItemAsksTheManager(t *testing.T) {
	h := newTestHub(t)
	if _, _, ok := h.uiItem("examplechat:1"); ok {
		t.Fatal("an item without the manager")
	}
	fm, _ := connectFakeManager(t, h)
	fm.mu.Lock()
	fm.items["examplechat:1"] = json.RawMessage(`{"id":"examplechat:1","source":"examplechat","title":"周会","tier":"action","meta":{"messages":["m1"]}}`)
	fm.mu.Unlock()
	it, source, ok := h.uiItem("examplechat:1")
	if !ok || source != "examplechat" || it.Title != "周会" || string(it.Meta) != `{"messages":["m1"]}` {
		t.Fatalf("the item: %+v %q %v", it, source, ok)
	}
	if _, _, ok := h.uiItem("nothing:1"); ok {
		t.Fatal("an item the manager has not")
	}
}

func TestInboxMailReply(t *testing.T) {
	orig := &mail.MailMessage{MailSummary: mail.MailSummary{Subject: "周报", MessageID: "<m2@x>", From: mail.Address{Name: "HR", Email: "hr@x"},
		Date: "2026-10-27T03:00:00Z"}, Text: "请确认\n谢谢", References: []string{"<m1@x>"}}
	r := mailReply(i18n.Default, "a1", orig, "收到")
	if r.Subject != "Re: 周报" || r.InReplyTo != "<m2@x>" || r.References != "<m1@x> <m2@x>" || len(r.To) != 1 || r.To[0].Email != "hr@x" {
		t.Fatalf("reply = %+v", r)
	}
	if !strings.HasPrefix(r.Text, "收到\n\n") || !strings.Contains(r.Text, "> 请确认\n> 谢谢") {
		t.Fatalf("body = %q", r.Text)
	}
	if mailReply(i18n.Default, "a1", &mail.MailMessage{MailSummary: mail.MailSummary{Subject: "RE: x"}}, "y").Subject != "RE: x" {
		t.Fatal("kept an existing Re:")
	}
}

func TestAIConfigOver(t *testing.T) {
	base := ai.Config{BaseURL: "http://gw", APIKeyFile: "~/k", Model: "base"}
	got := ai.Config{Model: "other"}.Over(base)
	if got.BaseURL != "http://gw" || got.APIKeyFile != "~/k" || got.Model != "other" {
		t.Fatalf("over = %+v", got)
	}
}

// newGitHubForTest is the GitHub connector with repositories its options still mute (as before they moved).
func newGitHubForTest(t *testing.T, muted []string) *github.Connector {
	t.Helper()
	return github.New(github.Config{Inbox: github.InboxOptions{MutedRepos: muted}}, "Mac", nil)
}

// inboxTasks are the tasks going on as the inbox's judge sees them (the connected assistant's).
func inboxTasks(t *testing.T, h *Hub) []prompts.InboxTask {
	t.Helper()
	return assistantOf(t, h).svc.ActiveTasks(context.Background())
}

// replyMail records what it was asked to send.
type replyMail struct {
	Mail
	sent []mail.SendRequest
}

func (*replyMail) GetMessage(_ context.Context, account string, uid uint32, mailbox string) (*mail.MailMessage, error) {
	return &mail.MailMessage{MailSummary: mail.MailSummary{AccountID: account, UID: uid, Mailbox: mailbox, Subject: "季度汇报",
		MessageID: "<m9@example.com>", From: mail.Address{Email: "sender@example.com"}}, Text: "请周五前提交"}, nil
}

func (m *replyMail) Send(_ context.Context, r mail.SendRequest) error {
	m.sent = append(m.sent, r)
	return nil
}

func (*replyMail) Tools() []proto.Tool { return (*mail.Service)(nil).Tools() }

// The inbox end to end on the bus, as `personad hub` runs it: the hub reads a connector and hands the round to the
// manager (the real assistant, its items in the hub's Postgres), the apps list and file items through the hub; with
// the manager gone the routes answer 503 assistant_offline and the sources are not read; the manager started again
// has everything, and the next round goes on from it.
func TestInboxOverTheBus(t *testing.T) {
	h, srv, store := newBusHub(t)
	ctx := context.Background()
	c := &inboxConnector{things: []connector.InboxThing{thing("a", time.Now().Add(-time.Minute))}}
	h.connectors = connector.NewRegistry(nil)
	if err := h.addConnector(c); err != nil {
		t.Fatal(err)
	}
	var ids []string
	t.Cleanup(func() { _ = store.DeleteInboxItems(context.Background(), ids) })
	do := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out := map[string]any{}
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	listed := func() []map[string]any {
		t.Helper()
		code, out := do("GET", "/api/v1/inbox?sources=all", "")
		if code != 200 {
			t.Fatalf("list: %d %v", code, out)
		}
		var items []map[string]any
		for _, v := range out["items"].([]any) {
			if it := v.(map[string]any); it["source"] == "example" {
				items = append(items, it)
			}
		}
		return items
	}

	dialAssistant(t, h, srv.URL, false)
	if err := h.collectInbox(ctx, roundFull); err != nil {
		t.Fatal(err)
	}
	items := listed()
	if len(items) != 1 || items[0]["title"] != "Add the example" || items[0]["tier"] != "action" || items[0]["context"] != nil ||
		items[0]["connector"].(map[string]any)["thread"] != "example-org/example-repo#12" || items[0]["version"] == "" {
		t.Fatalf("listed: %v", items)
	}
	id := items[0]["id"].(string)
	ids = append(ids, id)
	if code, out := do("GET", "/api/v1/inbox/"+id, ""); code != 200 || out["context"] != "上下文" {
		t.Fatalf("the item: %d %v", code, out)
	}
	if code, out := do("POST", "/api/v1/inbox/"+id+"/done", `{"done":true}`); code != 200 || out["done_by"] != "user" {
		t.Fatalf("done: %d %v", code, out)
	}

	// The manager gone: 503; the hub still reads the connector and keeps what it read for the manager.
	a := assistantOf(t, h)
	a.stop()
	<-a.done
	eventually(t, 5*time.Second, "the manager gone", func() bool { return h.busConnOf(assistantwire.AssistantID) == nil })
	if code, out := do("GET", "/api/v1/inbox", ""); code != http.StatusServiceUnavailable || out["code"] != "assistant_offline" {
		t.Fatalf("offline: %d %v", code, out)
	}
	c.mu.Lock()
	c.things = []connector.InboxThing{thing("b", time.Now())}
	c.mu.Unlock()
	read := c.collect.Load()
	if err := h.collectInbox(ctx, roundPoll); !errors.Is(err, errAssistantOffline) || c.collect.Load() != read+1 || h.inbox.held == nil {
		t.Fatalf("read without the manager: %v", err)
	}
	c.mu.Lock()
	c.things = nil // the move left the connector's window meanwhile
	c.mu.Unlock()

	// Back (a new process: it reads its store again): the item is there, done; the move read meanwhile starts a new one.
	dialAssistant(t, h, srv.URL, false)
	if items := listed(); len(items) != 1 || items[0]["id"] != id || items[0]["done_by"] != "user" {
		t.Fatalf("after the restart: %v", items)
	}
	if err := h.collectInbox(ctx, roundFull); err != nil {
		t.Fatal(err)
	}
	items = listed()
	if len(items) != 2 {
		t.Fatalf("after a new move: %v", items)
	}
	for _, it := range items {
		if it["id"] != id {
			ids = append(ids, it["id"].(string))
		}
	}
}

// seedInbox gives the connected assistant's inbox one item of each source (a round with a thing of each, read as
// the hub would hand it over).
func seedInbox(t *testing.T, h *Hub, sources ...string) {
	t.Helper()
	r := assistantwire.InboxRound{Round: assistantwire.RoundPoll, Names: map[string]string{}}
	for _, s := range sources {
		th := thing("a", time.Now().Add(-time.Minute))
		th.Thread = s + "-thread"
		r.Names[s] = s
		r.Sources = append(r.Sources, assistantwire.InboxSource{ID: s, Name: s, Things: []connector.InboxThing{th}})
	}
	assistantOf(t, h).svc.InboxCollected(context.Background(), r)
}

// inboxSourcesListed are the sources of the items GET /api/v1/inbox?sources=all lists (sorted, one per item).
func inboxSourcesListed(t *testing.T, h *Hub) []string {
	t.Helper()
	code, out := call(t, h.routes(), "GET", "/api/v1/inbox?sources=all", h.cfg.Token, "")
	if code != 200 {
		t.Fatalf("GET /api/v1/inbox: %d %v", code, out)
	}
	var got []string
	for _, v := range out["items"].([]any) {
		got = append(got, v.(map[string]any)["source"].(string))
	}
	slices.Sort(got)
	return got
}

// Clearing the inbox in one go is a full device's or an extension's with inbox.write: a watch (answer-only) files one
// item at a time, an extension that may only read the inbox may not.
func TestInboxBulkPermissions(t *testing.T) {
	h := newTestHub(t)
	_ = h.routes()
	if allowed(principal{ID: "dev_watch", Role: roleAnswer}, "POST", "/api/v1/inbox/bulk") {
		t.Fatal("an answer-only device may clear the inbox")
	}
	req := httptest.NewRequest("POST", "/api/v1/inbox/bulk", strings.NewReader(`{}`))
	ext := func(scopes ...string) principal {
		return principal{ID: "extension:x", Role: roleExtension, Extension: "x", Scopes: scopes}
	}
	if ok, scope := h.extensionMay(ext(extension.ScopeInboxRead), req); ok || scope != extension.ScopeInboxWrite {
		t.Fatalf("inbox.read clears the inbox: %v %q", ok, scope)
	}
	if ok, _ := h.extensionMay(ext(extension.ScopeInboxWrite), req); !ok {
		t.Fatal("inbox.write may not clear the inbox")
	}
}
