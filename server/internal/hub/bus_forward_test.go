package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The assistant's routes (assistantwire.Routes) are its to answer, unchanged; the hub's own stay the hub's; while the
// assistant is not connected they answer 503 assistant_offline.
func TestBusForwardsTheAssistantRoutes(t *testing.T) {
	h, srv, _ := newBusHub(t)
	got := make(chan assistantwire.HTTPRequest, 4)
	r := helloAssistant(t, h, srv, func(_ context.Context, method string, params json.RawMessage) (any, error) {
		if method == assistantwire.MethodTools {
			return map[string]any{"tools": []any{}}, nil
		}
		var req assistantwire.HTTPRequest
		if method != assistantwire.MethodHTTP || json.Unmarshal(params, &req) != nil {
			return nil, errors.New("unexpected " + method)
		}
		got <- req
		if req.Path == "/api/v1/assistant/runs" {
			return nil, &rpc.Error{Code: "boom", Message: "it failed"}
		}
		return assistantwire.HTTPResponse{Status: 201, Header: map[string][]string{"Content-Type": {"application/json"}, "X-Test": {"1"}},
			Body: []byte(`{"answered":"by the assistant"}`)}, nil
	})
	do := func(method, path, body string) (*http.Response, string) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		req.Header.Set(assistantwire.RoleHeader, "forged")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}
	resp, body := do("POST", "/api/v1/assistant/conversations/c1/route?x=1", `{"text":"你好"}`)
	if resp.StatusCode != 201 || body != `{"answered":"by the assistant"}` || resp.Header.Get("X-Test") != "1" {
		t.Fatalf("forwarded: %d %s %v", resp.StatusCode, body, resp.Header)
	}
	req := <-got
	if req.Method != "POST" || req.Path != "/api/v1/assistant/conversations/c1/route" || req.Query != "x=1" || string(req.Body) != `{"text":"你好"}` ||
		req.Header[assistantwire.RoleHeader][0] != roleFull || req.Header["Authorization"] != nil {
		t.Fatalf("the assistant got %+v", req)
	}
	if resp, body := do("GET", "/api/v1/assistant/runs", ""); resp.StatusCode != http.StatusBadGateway || !strings.Contains(body, "boom") {
		t.Fatalf("the assistant's error: %d %s", resp.StatusCode, body)
	}
	<-got
	for _, c := range [][2]string{{"POST", "/api/v1/task-plans/p1/start"}, {"GET", "/api/v1/task-trust"},
		{"DELETE", "/api/v1/task-trust"}, {"POST", "/api/v1/todos/t1/done-check"}} {
		if resp, _ := do(c[0], c[1], ""); resp.StatusCode != 201 {
			t.Fatalf("%s %s: %d", c[0], c[1], resp.StatusCode)
		}
		if req := <-got; req.Method != c[0] || req.Path != c[1] {
			t.Fatalf("%s %s reached the assistant as %s %s", c[0], c[1], req.Method, req.Path)
		}
	}
	// The hub's own routes stay the hub's, the assistant's settings and conversations among them (follow-ups).
	for _, c := range [][2]string{{"GET", "/api/v1/assistant"}, {"GET", "/api/v1/skills"}, {"GET", "/api/v1/machines"},
		{"GET", "/api/v1/todos"}, {"GET", "/api/v1/assistantx"}} {
		do(c[0], c[1], "")
	}
	select {
	case req := <-got:
		t.Fatalf("forwarded %s %s", req.Method, req.Path)
	default:
	}
	// Not connected: 503 assistant_offline, the hub's own routes as before.
	_ = r.ws.CloseNow()
	eventually(t, 5*time.Second, "the assistant gone", func() bool { return h.busConnOf(busAssistant) == nil })
	resp, body = do("GET", "/api/v1/assistant/tasks", "")
	if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(body, `"code":"assistant_offline"`) {
		t.Fatalf("offline: %d %s", resp.StatusCode, body)
	}
	if resp, _ := do("GET", "/api/v1/assistant", ""); resp.StatusCode != 200 {
		t.Fatalf("the hub's own: %d", resp.StatusCode)
	}
}

// What the assistant sends the apps (ws.broadcast) reaches them unchanged; a message without a type does not.
func TestBusBroadcast(t *testing.T) {
	h, srv, _ := newBusHub(t)
	app := wsApp(t, h)
	r := helloAssistant(t, h, srv, echoService)
	_ = r.peer.Notify("ws.broadcast", map[string]any{"message": map[string]any{"oops": true}})
	msg := json.RawMessage(`{"type":"task_plan","plan":{"id":"p1","steps":["一","二"]}}`)
	_ = r.peer.Notify("ws.broadcast", map[string]any{"message": msg})
	select {
	case raw := <-app.out:
		if !jsonEqual(withoutSeq(raw), withoutSeq(msg)) {
			t.Fatalf("the apps got %s, want %s (and the hub's event_seq)", raw, msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing reached the apps")
	}
}

// wsApp is an app connected to the hub's WebSocket.
func wsApp(t *testing.T, h *Hub) *client {
	t.Helper()
	c := &client{device: "test-device", out: make(chan []byte, 64)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","features":["ws_notify"]}`))
	<-c.out // client_hello_ack
	return c
}

// helloAssistant connects a fake assistant answering the hub's calls with handle, past its handshake.
func helloAssistant(t *testing.T, h *Hub, srv *httptest.Server, handle rpc.Handler) *rawBus {
	t.Helper()
	tok := busToken(t, h, busAssistant, "助手")
	r := dialRawBusWith(t, srv, tok, handle)
	if err := r.call("manager.hello", map[string]any{"id": busAssistant}, nil); err != nil {
		t.Fatal(err)
	}
	return r
}

// withoutSeq is a message the apps got without the event_seq the hub numbers every message with.
func withoutSeq(b []byte) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return b
	}
	delete(m, "event_seq")
	out, _ := json.Marshal(m)
	return out
}
