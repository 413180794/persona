package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/proto"
)

// The knowledge base is the assistant's (internal/assistant kb*.go, its own tests there); these are the hub's ends:
// the tools, the routes and the hub's features reaching it over the bus, the sources it reads through the hub.

// testKBs are the knowledge bases the tests' assistants keep, by hub (dialAssistant passes them on).
var testKBs sync.Map

// testKBOf is the knowledge base h's assistant keeps; nil: none.
func testKBOf(h *Hub) *assistant.KBOptions {
	v, _ := testKBs.Load(h)
	o, _ := v.(*assistant.KBOptions)
	return o
}

// withKB gives the assistant connected to h from now on a knowledge base in a directory of its own, and returns an
// engine on the same files for the test to write and read them.
func withKB(t *testing.T, h *Hub) *kb.Engine {
	t.Helper()
	dir := t.TempDir()
	testKBs.Store(h, &assistant.KBOptions{Dir: dir})
	t.Cleanup(func() { testKBs.Delete(h) })
	st, err := kb.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &kb.Engine{Store: st, Owner: "我"}
}

// connectAssistantRunning is connectAssistant with the assistant's own loops running, as `personad assistant` runs
// them (the knowledge base's among them).
func connectAssistantRunning(t *testing.T, h *Hub) *testAssistant {
	t.Helper()
	if h.busStore() == nil {
		h.bus.idents = &memIdents{}
	}
	mux := http.NewServeMux()
	h.busRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return dialAssistant(t, h, srv.URL, true)
}

// The kb_* tools, the assistant's, on its knowledge base: written, listed, read (a project by its name), searched;
// the hub's features read it over the bus: the inbox agent's context, the projects of a directory for the triage.
func TestKBToolsAndTheHubsReads(t *testing.T) {
	h := newTestHub(t)
	e := withKB(t, h)
	c := &kb.Chat{ID: kb.ChatID("lark", "f1"), Source: "lark", SourceID: "f1", Kind: "group", Name: "发布群", Created: time.Now()}
	_ = e.Store.PutChat(c)
	_ = e.Store.AppendMessages(c, []kb.Line{{At: time.Now(), Sender: "张三", Text: "周五灰度 Jarvis"}})
	_ = e.Store.PutSummary(kb.KindChat, c.ID, "# 发布群\n\n在推 Jarvis 发布")
	connectAssistant(t, h)
	tool := func(name, args string) string {
		t.Helper()
		r := h.asst().Tool(context.Background(), assistantwire.ToolCall{Name: name, Arguments: json.RawMessage(args)})
		if r.IsError {
			t.Fatalf("%s: %s", name, textOf(r))
		}
		return textOf(r)
	}
	if out := tool("kb_update", `{"kind":"projects","id":"Jarvis 发布","project_kind":"long","importance":"P0","relevance":95,"log":"定了周五灰度","chats":["lark-f1"],"dirs":["server:/code/jarvis"]}`); !strings.Contains(out, "projects/jarvis-发布") {
		t.Fatalf("update: %q", out)
	}
	if p, _ := e.Store.Project("jarvis-发布"); p == nil || p.Kind != "long" || !p.Locked || p.Dirs[0] != "server:/code/jarvis" {
		t.Fatalf("project: %+v", p)
	}
	if out := tool("kb_get", `{"kind":"projects","id":"Jarvis 发布"}`); !strings.Contains(out, "定了周五灰度") {
		t.Fatalf("get project by name: %q", out)
	}
	if out := tool("kb_search", `{"query":"灰度"}`); !strings.Contains(out, "messages/") {
		t.Fatalf("search: %q", out)
	}

	// The inbox agent gets the chat's and its project's context.
	it := &InboxItem{Lark: &InboxLark{FeedID: "f1"}, Agent: &InboxAgent{}}
	prompt := inboxAgentPrompt(it, "我", h.kbContextFor(it))
	if !strings.Contains(prompt, "会话档案 · 发布群") || !strings.Contains(prompt, "项目 · Jarvis 发布") || !strings.Contains(prompt, "kb_update") {
		t.Fatalf("inbox context: %s", prompt)
	}
	if h.kbContextFor(&InboxItem{Lark: &InboxLark{FeedID: "nope"}}) != nil {
		t.Fatal("context for an unknown chat")
	}
	// The triage's projects of a directory: that machine's only.
	if got := h.kbProjectNames(context.Background(), "server", "/code/jarvis/server"); len(got) != 1 || got[0] != "Jarvis 发布" {
		t.Fatalf("dir match: %v", got)
	}
	if got := h.kbProjectNames(context.Background(), "laptop", "/code/jarvis"); len(got) != 0 {
		t.Fatalf("other machine matched: %v", got)
	}
}

// Without a knowledge base the assistant's kb_* tools say it is off.
func TestKBToolsWithoutAKnowledgeBase(t *testing.T) {
	h := newTestHub(t)
	connectAssistant(t, h)
	r := h.asst().Tool(context.Background(), assistantwire.ToolCall{Name: "kb_search", Arguments: json.RawMessage(`{"query":"x"}`)})
	if !r.IsError || textOf(r) != "the knowledge base is off (hub.json kb.enabled)" {
		t.Fatalf("kb_search: %s", textOf(r))
	}
}

// The apps' /api/v1/kb routes are relayed to the assistant with the shapes they had; while it is not connected they
// answer 503 assistant_offline, and the hub's own reads go without.
func TestKBRoutesRelayed(t *testing.T) {
	h := newTestHub(t)
	withKB(t, h)
	a := connectAssistant(t, h)
	mux := h.routes()
	tok := h.cfg.Token
	if code, out := call(t, mux, "PUT", "/api/v1/kb/projects/X", tok, `{"name":"X","tags":["a"]}`); code != 200 || out["id"] != "x" {
		t.Fatalf("put: %d %v", code, out)
	}
	code, out := call(t, mux, "GET", "/api/v1/kb/projects?tag=a", tok, "")
	if items, _ := out["items"].([]any); code != 200 || len(items) != 1 || items[0].(map[string]any)["id"] != "x" || out["tags"] == nil {
		t.Fatalf("list: %d %v", code, out)
	}
	if code, out := call(t, mux, "GET", "/api/v1/kb/projects/x", tok, ""); code != 200 || out["detail"].(map[string]any)["kind"] != "projects" {
		t.Fatalf("get: %d %v", code, out)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/kb/chats/missing", tok, ""); code != 404 {
		t.Fatalf("missing: %d", code)
	}
	if code, out := call(t, mux, "GET", "/api/v1/kb-search?q=x&results=1", tok, ""); code != 200 || out["results"] == nil {
		t.Fatalf("search: %d %v", code, out)
	}

	a.stop()
	<-a.done
	eventually(t, 5*time.Second, "the hub sees the assistant gone", func() bool { return h.busConnOf(assistantwire.AssistantID) == nil })
	if code, out := call(t, mux, "GET", "/api/v1/kb/projects", tok, ""); code != 503 || out["code"] != assistantwire.CodeAssistantOffline {
		t.Fatalf("while the assistant is away: %d %v", code, out)
	}
	if got := h.kbProjectNames(context.Background(), "server", "/code"); got != nil {
		t.Fatalf("project names while away: %v", got)
	}
}

// An agent's finished turn goes into the timeline of the projects of its directory: once per agent and half hour,
// only on that machine; one that ends while the assistant is away waits at the hub and goes when it is back.
func TestKBAgentDone(t *testing.T) {
	h := newTestHub(t)
	e := withKB(t, h)
	if _, err := e.Apply(kb.KindProject, "Jarvis", kb.Update{Dirs: []string{"server:/code/jarvis"}}); err != nil {
		t.Fatal(err)
	}
	a := connectAssistant(t, h)
	s := proto.Session{SessionID: "s1", Cwd: "/code/jarvis/ios", Title: "修排序", LastText: "改好了，\n测试通过"}
	h.kbAgentDone("server", s)
	h.kbAgentDone("server", s) // the same agent again right away: one line
	h.kbAgentDone("laptop", proto.Session{SessionID: "s2", Cwd: "/code/jarvis", LastText: "x"})
	h.kbAgentDone("server", proto.Session{SessionID: "s3", Cwd: "/code/jarvis"}) // nothing said
	eventually(t, 5*time.Second, "the agent's line", func() bool { return len(e.Store.Log("jarvis", 10)) > 0 })
	a.bh.Settle()
	if log := e.Store.Log("jarvis", 10); len(log) != 1 || !strings.Contains(log[0], "Agent · 修排序 · server · 改好了， 测试通过") {
		t.Fatalf("log: %v", log)
	}

	a.stop()
	<-a.done
	eventually(t, 5*time.Second, "the hub sees the assistant gone", func() bool { return h.busConnOf(assistantwire.AssistantID) == nil })
	h.kbAgentDone("server", proto.Session{SessionID: "s4", Cwd: "/code/jarvis", Title: "发版", LastText: "先写一半"})
	h.kbAgentDone("server", proto.Session{SessionID: "s4", Cwd: "/code/jarvis", Title: "发版", LastText: "发好了"})
	connectAssistant(t, h)
	eventually(t, 5*time.Second, "the turn that waited", func() bool { return len(e.Store.Log("jarvis", 10)) == 2 })
	if log := e.Store.Log("jarvis", 10); !strings.Contains(log[len(log)-1], "Agent · 发版 · server · 发好了") {
		t.Fatalf("log after the assistant came back: %v", log)
	}
}

// archiveExample is a connector whose archive hands over one chat (two messages) the first time.
type archiveExample struct{ calls int }

func (*archiveExample) ID() string   { return "example" }
func (*archiveExample) Name() string { return "Example" }
func (*archiveExample) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapArchive}}
}
func (a *archiveExample) Changes(_ context.Context, cursor string, _ int) ([]connector.ChatBatch, string, bool, error) {
	a.calls++
	if cursor != "" {
		return nil, cursor, false, nil
	}
	at := time.Now().Add(-time.Hour)
	return []connector.ChatBatch{{Chat: kb.SourceChat{ID: "g1", Name: "示例群", Kind: "group"},
		Messages: []kb.SourceMessage{{ID: "m1", SenderID: "u1", SenderName: "示例用户", Type: "TEXT", Text: "周五发布", At: at},
			{ID: "m2", SenderID: "u2", SenderName: "另一位", Type: "TEXT", Text: "收到", At: at.Add(time.Minute)}}}}, "1", false, nil
}

// The assistant's knowledge base reads a connector through the hub (connectors.archives, connectors.changes) and
// keeps it in its own files: the chat, its messages, the cursor.
func TestKBArchivesThroughTheHub(t *testing.T) {
	h := newTestHub(t)
	h.connectors = connector.NewRegistry(nil)
	f := &archiveExample{}
	if err := h.addConnector(f); err != nil {
		t.Fatal(err)
	}
	e := withKB(t, h)
	connectAssistantRunning(t, h) // its loop: the first round reads every connector
	eventually(t, 10*time.Second, "the connector's chat in the knowledge base", func() bool {
		c, ok := e.Store.Chat(kb.ChatID("example", "g1"))
		return ok && c.Messages == 2
	})
	if text := e.Store.Since(kb.ChatID("example", "g1"), time.Time{}, 10000); !strings.Contains(text, "周五发布") || !strings.Contains(text, "收到") {
		t.Fatalf("archived:\n%s", text)
	}
	eventually(t, 5*time.Second, "the cursor", func() bool { // written after the batch went in
		b, err := os.ReadFile(filepath.Join(e.Store.Dir, "cursors", "example.json"))
		return err == nil && string(b) == `{"cursor":"1"}`
	})
}

// hub.json kb.enabled false: the assistant is told (hello) and opens none, and the hub hands nothing over itself: no
// sources listed or read, no posts taken, nothing asked to delete.
func TestKBOffAtTheHub(t *testing.T) {
	h := newTestHub(t)
	off := false
	h.cfg.KB.Enabled = &off
	h.cfg.KB.TextDays = 30
	if s := h.kbSettings(); !s.Off || s.TextDays != 30 {
		t.Fatalf("settings %+v", s)
	}
	if hello, _ := (assistantLink{h}).Hello(context.Background()); hello.KB == nil || !hello.KB.Off {
		t.Fatalf("hello %+v", hello.KB)
	}
	h.connectors = connector.NewRegistry(nil)
	f := &archiveExample{}
	if err := h.addConnector(f); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	raw, ok, err := h.kbSourcesCall(ctx, assistantwire.AppCall{AppID: assistantwire.AppConnectors, Method: assistantwire.ConnectorsArchives})
	if !ok || err != nil || string(raw) != `{"archives":[],"items":[]}` {
		t.Fatalf("archives: %s %v", raw, err)
	}
	if _, _, err := h.kbSourcesCall(ctx, assistantwire.AppCall{AppID: assistantwire.AppConnectors, Method: assistantwire.ConnectorsChanges,
		Params: json.RawMessage(`{"id":"example"}`)}); err == nil || f.calls != 0 {
		t.Fatalf("changes read with the knowledge base off: %v", err)
	}
	if _, err := h.kbFor("example").PendingPosts(ctx, 10); err == nil || err.Error() != "这台 hub 没开知识库" {
		t.Fatalf("posts: %v", err)
	}
	if c, p, err := h.forgetArchive(ctx, "example"); c != 0 || p != 0 || err != nil {
		t.Fatalf("forget asked: %d %d %v", c, p, err)
	}
}

// The queue of agents' turns keeps the latest of each agent and at most kbPendingMost.
func TestKBTurnQueue(t *testing.T) {
	var q []assistantwire.KBAgentTurn
	q = queueKBTurn(q, assistantwire.KBAgentTurn{MachineID: "m", SessionID: "a", Line: "1"})
	q = queueKBTurn(q, assistantwire.KBAgentTurn{MachineID: "m", SessionID: "b", Line: "2"})
	q = queueKBTurn(q, assistantwire.KBAgentTurn{MachineID: "m", SessionID: "a", Line: "3"})
	if len(q) != 2 || q[0].SessionID != "b" || q[1].Line != "3" {
		t.Fatalf("queue %+v", q)
	}
	for i := range kbPendingMost + 10 {
		q = queueKBTurn(q, assistantwire.KBAgentTurn{MachineID: "m", SessionID: fmt.Sprint(i)})
	}
	if len(q) != kbPendingMost || q[len(q)-1].SessionID != fmt.Sprint(kbPendingMost+9) {
		t.Fatalf("capped: %d", len(q))
	}
}

// An attachment for an assistant on another machine is at most kbMediaExternal (one bus message must not hold a slow
// link past its keepalive).
func TestKBMediaForAnExternalAssistant(t *testing.T) {
	h := newTestHub(t)
	if h.kbMediaMost() != connector.ArchiveMediaMost {
		t.Fatal("the assistant beside the hub gets less than the whole")
	}
	h.cfg.Assistant.External = true
	if h.kbMediaMost() != kbMediaExternal {
		t.Fatal("an external assistant gets the whole")
	}
}
