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

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/proto"
)

func testKBHub(t *testing.T) *Hub {
	st, err := kb.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := &Hub{}
	h.kb.eng = &kb.Engine{Store: st, Owner: "我"}
	return h
}

// The kb_* tools, the assistant's, on the hub's knowledge base (app.call kb.*): written, listed, read (a project by
// its name), searched.
func TestKBTools(t *testing.T) {
	h := newTestHub(t)
	st, err := kb.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.kb.eng = &kb.Engine{Store: st, Owner: "我"}
	e := h.kb.eng
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
	p, _ := e.Store.Project("jarvis-发布")
	if p.Kind != "long" || !p.Locked || p.Importance != "P0" || len(p.Chats) != 1 || p.Dirs[0] != "server:/code/jarvis" {
		t.Fatalf("project: %+v", p)
	}
	if c2, _ := e.Store.Chat("lark-f1"); len(c2.Projects) != 1 {
		t.Fatalf("chat not linked back: %+v", c2)
	}
	if out := tool("kb_list", `{"kind":"projects"}`); !strings.Contains(out, "Jarvis 发布 [P0 相关95]") {
		t.Fatalf("list: %q", out)
	}
	if out := tool("kb_get", `{"kind":"projects","id":"Jarvis 发布"}`); !strings.Contains(out, "定了周五灰度") {
		t.Fatalf("get project by name: %q", out)
	}
	// meta.json reads as the file has it.
	meta, _ := os.ReadFile(filepath.Join(e.Store.Dir, "chats", "lark-f1", "meta.json"))
	if out := tool("kb_get", `{"kind":"chats","id":"lark-f1"}`); !strings.Contains(out, "周五灰度 Jarvis") || !strings.Contains(out, "在推 Jarvis 发布") ||
		len(meta) == 0 || !strings.Contains(out, "meta.json:\n"+string(meta)+"\n") {
		t.Fatalf("get chat: %q", out)
	}
	if out := tool("kb_search", `{"query":"灰度"}`); !strings.Contains(out, "messages/") {
		t.Fatalf("search: %q", out)
	}
	if out := tool("kb_search", `{"query":"没有这个词"}`); out != "No match in "+e.Store.Dir+"." {
		t.Fatalf("search, nothing found: %q", out)
	}
	if got := e.ProjectsForDir("server", "/code/jarvis/server"); len(got) != 1 {
		t.Fatalf("dir match: %v", got)
	}
	if got := e.ProjectsForDir("laptop", "/code/jarvis"); len(got) != 0 {
		t.Fatalf("other machine matched: %v", got)
	}

	// The inbox agent gets the chat's and its project's context.
	it := &InboxItem{Lark: &InboxLark{FeedID: "f1"}, Agent: &InboxAgent{}}
	ctx := inboxAgentPrompt(it, "我", h.kbContextFor(it))
	if !strings.Contains(ctx, "会话档案 · 发布群") || !strings.Contains(ctx, "项目 · Jarvis 发布") || !strings.Contains(ctx, "kb_update") {
		t.Fatalf("inbox context: %s", ctx)
	}
	if h.kbContextFor(&InboxItem{Lark: &InboxLark{FeedID: "nope"}}) != nil {
		t.Fatal("context for an unknown chat")
	}
}

// Without a knowledge base the hub's kb methods fail and the assistant's kb_* tools say it is off.
func TestKBToolsWithoutAKnowledgeBase(t *testing.T) {
	h := newTestHub(t)
	connectAssistant(t, h)
	r := h.asst().Tool(context.Background(), assistantwire.ToolCall{Name: "kb_search", Arguments: json.RawMessage(`{"query":"x"}`)})
	if !r.IsError || textOf(r) != "the knowledge base is off (hub.json kb.enabled)" {
		t.Fatalf("kb_search: %s", textOf(r))
	}
}

func TestKBRoutes(t *testing.T) {
	h := testKBHub(t)
	mux := http.NewServeMux()
	h.kbRoutes(mux)
	put := httptest.NewRequest("PUT", "/api/v1/kb/projects/X", strings.NewReader(`{"name":"X","tags":["a"]}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, put)
	if rec.Code != 200 {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/kb/projects?tag=a", nil))
	var list struct {
		Items []kb.Entry `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].ID != "x" {
		t.Fatalf("list: %s", rec.Body)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/kb/projects/x", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"kind":"projects"`) {
		t.Fatalf("get: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/kb/chats/missing", nil))
	if rec.Code != 404 {
		t.Fatalf("missing: %d", rec.Code)
	}
}

func TestKBAgentDone(t *testing.T) {
	h := testKBHub(t)
	e := h.kb.eng
	if _, err := e.Apply(kb.KindProject, "Jarvis", kb.Update{Dirs: []string{"server:/code/jarvis"}}); err != nil {
		t.Fatal(err)
	}
	s := proto.Session{SessionID: "s1", Cwd: "/code/jarvis/ios", Title: "修排序", LastText: "改好了，\n测试通过"}
	h.kbAgentDone("server", s)
	h.kbAgentDone("server", s) // the same agent again right away: one line
	h.kbAgentDone("laptop", proto.Session{SessionID: "s2", Cwd: "/code/jarvis", LastText: "x"})
	log := e.Store.Log("jarvis", 10)
	if len(log) != 1 || !strings.Contains(log[0], "Agent · 修排序 · server · 改好了， 测试通过") {
		t.Fatalf("log: %v", log)
	}
}
