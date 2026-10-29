package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/kb"
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

func TestKBTools(t *testing.T) {
	h := testKBHub(t)
	e := h.kb.eng
	c := &kb.Chat{ID: kb.ChatID("lark", "f1"), Source: "lark", SourceID: "f1", Kind: "group", Name: "发布群", Created: time.Now()}
	_ = e.Store.PutChat(c)
	_ = e.Store.AppendMessages(c, []kb.Line{{At: time.Now(), Sender: "张三", Text: "周五灰度 Jarvis"}})
	_ = e.Store.PutSummary(kb.KindChat, c.ID, "# 发布群\n\n在推 Jarvis 发布")

	out, err := h.runKBTool("kb_update", json.RawMessage(`{"kind":"projects","id":"Jarvis 发布","project_kind":"long","importance":"P0","relevance":95,"log":"定了周五灰度","chats":["lark-f1"],"dirs":["example-machine:/code/jarvis"]}`))
	if err != nil || !strings.Contains(out, "projects/jarvis-发布") {
		t.Fatalf("update: %q %v", out, err)
	}
	p, _ := e.Store.Project("jarvis-发布")
	if p.Kind != "long" || !p.Locked || p.Importance != "P0" || len(p.Chats) != 1 || p.Dirs[0] != "example-machine:/code/jarvis" {
		t.Fatalf("project: %+v", p)
	}
	if c2, _ := e.Store.Chat("lark-f1"); len(c2.Projects) != 1 {
		t.Fatalf("chat not linked back: %+v", c2)
	}
	if out, _ := h.runKBTool("kb_list", json.RawMessage(`{"kind":"projects"}`)); !strings.Contains(out, "Jarvis 发布 [P0 相关95]") {
		t.Fatalf("list: %q", out)
	}
	if out, _ := h.runKBTool("kb_get", json.RawMessage(`{"kind":"projects","id":"Jarvis 发布"}`)); !strings.Contains(out, "定了周五灰度") {
		t.Fatalf("get project by name: %q", out)
	}
	if out, _ := h.runKBTool("kb_get", json.RawMessage(`{"kind":"chats","id":"lark-f1"}`)); !strings.Contains(out, "周五灰度 Jarvis") || !strings.Contains(out, "在推 Jarvis 发布") {
		t.Fatalf("get chat: %q", out)
	}
	if out, _ := h.runKBTool("kb_search", json.RawMessage(`{"query":"灰度"}`)); !strings.Contains(out, "messages/") {
		t.Fatalf("search: %q", out)
	}
	if got := e.ProjectsForDir("example-machine", "/code/jarvis/server"); len(got) != 1 {
		t.Fatalf("dir match: %v", got)
	}
	if got := e.ProjectsForDir("laptop", "/code/jarvis"); len(got) != 0 {
		t.Fatalf("other machine matched: %v", got)
	}

	// The inbox agent gets the chat's and its project's context.
	ctx := h.kbContextFor(&InboxItem{Lark: &InboxLark{FeedID: "f1"}})
	if !strings.Contains(ctx, "会话档案 · 发布群") || !strings.Contains(ctx, "项目 · Jarvis 发布") || !strings.Contains(ctx, "kb_update") {
		t.Fatalf("inbox context: %s", ctx)
	}
	if h.kbContextFor(&InboxItem{Lark: &InboxLark{FeedID: "nope"}}) != "" {
		t.Fatal("context for an unknown chat")
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
