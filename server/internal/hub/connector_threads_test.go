package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// threadsConnector is a chat app with one made-up conversation of 40 messages, a picture, a reply action, and a
// state the test sets.
type threadsConnector struct {
	state   atomic.Value // string
	fetched atomic.Int32 // Media calls
	mu      sync.Mutex
	sent    []string
}

func newThreadsConnector() *threadsConnector {
	c := &threadsConnector{}
	c.state.Store(connector.StateOK)
	return c
}

func (*threadsConnector) ID() string   { return "example" }
func (*threadsConnector) Name() string { return "Example" }
func (*threadsConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapActions, connector.CapThreads}}
}
func (c *threadsConnector) Status(context.Context) connector.Status {
	st := c.state.Load().(string)
	if st == connector.StateRateLimited {
		return connector.Status{State: st, Error: "限流：还要 30 秒"}
	}
	return connector.Status{State: st}
}

func (c *threadsConnector) Thread(_ context.Context, thread, before string, limit int) (connector.ThreadPage, error) {
	if thread != "u1" {
		return connector.ThreadPage{}, errors.New("没有这个会话")
	}
	end := 40
	if before != "" {
		fmt.Sscan(before, &end)
	}
	start := max(end-limit, 0)
	page := connector.ThreadPage{Chat: connector.ThreadChat{ID: "u1", Name: "示例用户", Kind: "dm", Reply: "example_send"}}
	at := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	for i := start; i < end; i++ {
		m := connector.ThreadMessage{ID: fmt.Sprint(i + 1), At: at.Add(time.Duration(i) * time.Minute), Text: fmt.Sprintf("示例消息 %d", i+1),
			Sender: connector.ThreadPerson{ID: "u1", Name: "示例用户", Avatar: "avatar:u1:1"}}
		if i == 39 {
			m.Media = []connector.ThreadMedia{{Kind: "image", Ref: "photo:40", Width: 4, Height: 3}}
		}
		page.Messages = append(page.Messages, m)
	}
	if start > 0 {
		page.Before = fmt.Sprint(start)
	}
	return page, nil
}

func (c *threadsConnector) Media(_ context.Context, ref string) (connector.Media, error) {
	c.fetched.Add(1)
	time.Sleep(20 * time.Millisecond) // the connector's own limits: concurrent asks wait for the one fetch
	if ref == "photo:missing" {
		return connector.Media{}, errors.New("没有这张图")
	}
	return connector.Media{Mime: "image/png", Data: []byte("made-up png " + ref)}, nil
}

func (*threadsConnector) Actions() []proto.Tool { return []proto.Tool{{Name: "example_send"}} }
func (*threadsConnector) Describe(_ context.Context, _ string, args json.RawMessage) (connector.Action, string, error) {
	return connector.Action{Visible: "示例用户"}, "发给示例用户：" + string(args), nil
}
func (c *threadsConnector) Do(_ context.Context, _ string, args json.RawMessage) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, string(args))
	return `{"sent":{"id":41}}`, nil
}

func threadsHub(t *testing.T) (*Hub, *threadsConnector, http.Handler) {
	t.Helper()
	return threadsHubWith(t, nil)
}

// threadsHubWith is threadsHub with hub.json's connector_gate rules.
func threadsHubWith(t *testing.T, rules []connector.Rule) (*Hub, *threadsConnector, http.Handler) {
	t.Helper()
	h := newTestHub(t)
	store := newPairStore()
	store.memStore = *newMemStore()
	h.store = store
	h.connectors = connector.NewRegistry(rules)
	c := newThreadsConnector()
	if err := h.addConnector(c); err != nil {
		t.Fatal(err)
	}
	return h, c, h.routes()
}

// A thread reads a page at a time, newest first, each page going back from the one before; the app is told why it
// cannot read one (no such connector, no threads, signed out, rate limited).
func TestConnectorThreadPages(t *testing.T) {
	h, c, mux := threadsHub(t)
	code, out := call(t, mux, "GET", "/api/v1/connectors/example/thread?thread=u1&limit=30", h.cfg.Token, "")
	msgs, _ := out["messages"].([]any)
	if code != 200 || len(msgs) != 30 || out["before"] != "10" || msgs[0].(map[string]any)["id"] != "11" || msgs[29].(map[string]any)["id"] != "40" {
		t.Fatalf("first page: %d %v", code, out)
	}
	if chat := out["chat"].(map[string]any); chat["reply"] != "example_send" || chat["kind"] != "dm" {
		t.Fatalf("chat %v", chat)
	}
	code, out = call(t, mux, "GET", "/api/v1/connectors/example/thread?thread=u1&before=10&limit=500", h.cfg.Token, "")
	if msgs, _ := out["messages"].([]any); code != 200 || len(msgs) != 10 || out["before"] != nil {
		t.Fatalf("the last page: %d %v", code, out)
	}
	for _, c := range []struct {
		path string
		code int
	}{
		{"/api/v1/connectors/nope/thread?thread=u1", 404},
		{"/api/v1/connectors/example/thread", 400},
		{"/api/v1/connectors/example/thread?thread=u9", 502},
	} {
		if code, _ := call(t, mux, "GET", c.path, h.cfg.Token, ""); code != c.code {
			t.Errorf("%s: %d, want %d", c.path, code, c.code)
		}
	}
	c.state.Store(connector.StateRateLimited)
	if code, out := call(t, mux, "GET", "/api/v1/connectors/example/thread?thread=u1", h.cfg.Token, ""); code != 429 || out["code"] != "rate_limited" ||
		!strings.Contains(fmt.Sprint(out["error"]), "还要 30 秒") {
		t.Fatalf("rate limited: %d %v", code, out)
	}
	c.state.Store(connector.StateNotSignedIn)
	if code, _ := call(t, mux, "GET", "/api/v1/connectors/example/thread?thread=u1", h.cfg.Token, ""); code != 409 {
		t.Fatalf("signed out: %d", code)
	}
	_ = h.addConnector(&exampleConnector{})
	if code, _ := call(t, h.routes(), "GET", "/api/v1/connectors/example/thread?thread=u1", h.cfg.Token, ""); code != 409 && code != 404 {
		t.Fatalf("a connector without threads: %d", code)
	}
}

// A media is fetched once however many ask for it at once, kept on disk, and served from there afterwards (even with
// the connector signed out); one that cannot be fetched is not kept.
func TestConnectorMedia(t *testing.T) {
	h, c, mux := threadsHub(t)
	get := func(ref string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/v1/connectors/example/media?ref="+ref, nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := get("avatar:u1:1"); w.Code != 200 || w.Body.String() != "made-up png avatar:u1:1" || w.Header().Get("Content-Type") != "image/png" ||
				!strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
				t.Errorf("%d %q %v", w.Code, w.Body.String(), w.Header())
			}
		}()
	}
	wg.Wait()
	if n := c.fetched.Load(); n != 1 {
		t.Fatalf("fetched %d times", n)
	}
	c.state.Store(connector.StateNotSignedIn)
	if w := get("avatar:u1:1"); w.Code != 200 || c.fetched.Load() != 1 {
		t.Fatalf("not served from disk: %d, %d fetches", w.Code, c.fetched.Load())
	}
	c.state.Store(connector.StateOK)
	if w := get("photo:missing"); w.Code != 502 {
		t.Fatalf("missing: %d", w.Code)
	}
	if _, ok := h.media.get(h.mediaDir(), "example", "photo:missing"); ok {
		t.Fatal("a failed fetch was kept")
	}
	if w := get(""); w.Code != 400 {
		t.Fatalf("no ref: %d", w.Code)
	}
}

// The oldest media go once more than the most is kept.
func TestPruneMedia(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-time.Hour)
	for i, name := range []string{"a", "b", "c"} {
		p := filepath.Join(dir, "example", name)
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, make([]byte, 100), 0o600)
		_ = os.Chtimes(p, old.Add(time.Duration(i)*time.Minute), old.Add(time.Duration(i)*time.Minute))
	}
	pruneMedia(dir, 250)
	for name, want := range map[string]bool{"a": false, "b": true, "c": true} {
		if _, err := os.Stat(filepath.Join(dir, "example", name)); (err == nil) != want {
			t.Errorf("%s kept %v, want %v", name, err == nil, want)
		}
	}
}

// A reply that does not come from one of the user's devices (here the pairing token) goes through the action the page
// named and the gate: declined, nothing is sent; allowed, the action gets {chat, text, reply_to}. Another connector's
// action, or none, is refused.
func TestConnectorThreadReply(t *testing.T) {
	h, c, mux := threadsHub(t)
	answer := func(allow bool) {
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			for _, ix := range h.pendingInteractions() {
				if ix.ToolName == "example_send" {
					_ = h.resolve(ix.ID, proto.Resolution{Allow: &allow})
					return
				}
			}
		}
		t.Error("no card on the phone")
	}
	post := func(body string, allow bool) (int, map[string]any) {
		done := make(chan struct{})
		go func() { defer close(done); answer(allow) }()
		code, out := call(t, mux, "POST", "/api/v1/connectors/example/thread/reply", h.cfg.Token, body)
		<-done
		return code, out
	}
	if code, out := post(`{"thread":"u1","action":"example_send","text":"示例回复","reply_to":"39"}`, false); code != 403 || out["code"] != "declined" {
		t.Fatalf("declined: %d %v", code, out)
	}
	if len(c.sent) != 0 {
		t.Fatalf("declined, yet sent %v", c.sent)
	}
	code, out := post(`{"thread":"u1","action":"example_send","text":"示例回复","reply_to":"39"}`, true)
	if code != 200 || out["ok"] != true || len(c.sent) != 1 || c.sent[0] != `{"chat":"u1","reply_to":39,"text":"示例回复"}` {
		t.Fatalf("allowed: %d %v, sent %v", code, out, c.sent)
	}
	for _, body := range []string{`{"thread":"u1","action":"other_send","text":"x"}`, `{"thread":"u1","action":"example_nope","text":"x"}`,
		`{"thread":"u1","action":"example_send","text":"  "}`, `{"action":"example_send","text":"x"}`} {
		if code, _ := call(t, mux, "POST", "/api/v1/connectors/example/thread/reply", h.cfg.Token, body); code != 400 {
			t.Errorf("%s: %d", body, code)
		}
	}
}

// A reply the user sends from their own app (a phone's or the web's device token) runs as they sent it: no card, and
// not even a deny rule in connector_gate stops it, the gate being for the AI's writes. With the same rule the pairing
// token, a pairing code and the assistant's mcp.call are refused; with none they would be asked about.
func TestConnectorThreadReplyFromTheUsersDevice(t *testing.T) {
	device := func(h *Hub, id, kind, role string) string {
		tok := "jd_" + randomHex(24)
		_ = h.store.CreateDeviceToken(context.Background(), db.DeviceToken{ID: id, Name: kind, Kind: kind, Role: role}, hashToken(tok))
		return tok
	}
	const body = `{"thread":"u1","action":"example_send","text":"示例回复"}`

	h, c, mux := threadsHub(t)
	// A card that shows up anyway is declined at once, so the reply fails here instead of waiting for it.
	carded := make(chan string, 4)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		no := false
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
			for _, ix := range h.pendingInteractions() {
				carded <- ix.ToolName
				_ = h.resolve(ix.ID, proto.Resolution{Allow: &no})
			}
		}
	}()
	for _, tok := range []string{device(h, "dev_phone", "phone", roleFull), device(h, "dev_web", "other", roleFull)} {
		if code, out := call(t, mux, "POST", "/api/v1/connectors/example/thread/reply", tok, body); code != 200 || out["ok"] != true {
			t.Fatalf("from the device: %d %v", code, out)
		}
	}
	if len(carded) != 0 {
		t.Fatalf("a card for the user's own reply: %s", <-carded)
	}
	if len(c.sent) != 2 || c.sent[0] != `{"chat":"u1","text":"示例回复"}` {
		t.Fatalf("sent %v", c.sent)
	}

	h, c, mux = threadsHubWith(t, []connector.Rule{{Connector: "example", Action: "example_send", Decision: connector.Deny}})
	if code, out := call(t, mux, "POST", "/api/v1/connectors/example/thread/reply", device(h, "dev_phone", "phone", roleFull), body); code != 200 || out["ok"] != true || len(c.sent) != 1 {
		t.Fatalf("a deny rule stopped the user's own reply: %d %v, sent %v", code, out, c.sent)
	}
	for name, tok := range map[string]string{"the pairing token": h.cfg.Token, "a pairing code": newPairCode(t, h).Token} {
		if status, out := call(t, mux, "POST", "/api/v1/connectors/example/thread/reply", tok, body); status != 403 || out["code"] != "declined" {
			t.Errorf("%s: %d %v", name, status, out)
		}
	}
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
	params, _ := json.Marshal(map[string]any{"name": "example_send", "args": map[string]any{"chat": "u1", "text": "示例回复"}, "assistant": true})
	if out, err := nodeAsks(context.Background(), m, "mcp.call", params); err != nil || !out.(proto.ToolResult).IsError {
		t.Fatalf("the assistant got past the gate: %v %+v", err, out)
	}
	if len(c.sent) != 1 {
		t.Fatalf("sent %v", c.sent)
	}
}
