package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/proto"
)

// lockedBuf is the hub's log in a test, read while handlers write to it.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func runtimeCLIServer(t *testing.T) (*Hub, *httptest.Server, *lockedBuf) {
	t.Helper()
	h := newTestHub(t)
	h.cfg.Token = "test-token"
	logs := &lockedBuf{}
	h.logger = log.New(logs, "", log.LstdFlags)
	srv := httptest.NewServer(h.routes())
	t.Cleanup(srv.Close)
	return h, srv, logs
}

// fakePhone is an app on the hub's WebSocket: it registers as a phone with `tools` and answers tool calls with
// `answer` (nil: never answers).
type fakePhone struct {
	conn  *websocket.Conn
	calls chan map[string]any
}

func dialPhone(t *testing.T, h *Hub, srv *httptest.Server, sim bool, tools []string, answer func(args map[string]any) (string, bool)) *fakePhone {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/v1/ws?token=test-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	conn.SetReadLimit(64 << 20)
	var list []proto.Tool
	for _, n := range tools {
		list = append(list, proto.Tool{Name: n, Description: n})
	}
	reg, _ := json.Marshal(map[string]any{"type": "register_phone", "device_name": "test", "simulator": sim, "tools": list})
	before := registered(h)
	if err := conn.Write(ctx, websocket.MessageText, reg); err != nil {
		t.Fatal(err)
	}
	p := &fakePhone{conn: conn, calls: make(chan map[string]any, 8)}
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var m map[string]any
			if json.Unmarshal(data, &m) != nil || m["type"] != "tool_call" {
				continue
			}
			p.calls <- m
			if answer == nil {
				continue
			}
			args, _ := m["args"].(map[string]any)
			text, isErr := answer(args)
			res, _ := json.Marshal(map[string]any{"type": "tool_result", "call_id": m["call_id"],
				"content": []map[string]any{{"type": "text", "text": text}}, "is_error": isErr})
			_ = conn.Write(ctx, websocket.MessageText, res)
		}
	}()
	// Registered once the hub has one more client that is the phone or offers the runtime CLI.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if registered(h) > before {
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the fake phone did not register")
	return nil
}

func registered(h *Hub) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for c := range h.clients {
		if c.phone || c.runtimeCLI {
			n++
		}
	}
	return n
}

func postCLI(t *testing.T, srv *httptest.Server, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/phone/runtime-cli", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not JSON (%d): %s", resp.StatusCode, b)
	}
	return resp.StatusCode, m
}

// A phone that offers the runtime CLI runs the command and its answer comes back as is; the call is logged by
// command and caller, never with its arguments.
func TestRuntimeCLIGoesToThePhone(t *testing.T) {
	h, srv, logs := runtimeCLIServer(t)
	phone := dialPhone(t, h, srv, false, []string{"phone_contacts", runtimeCLITool}, func(args map[string]any) (string, bool) {
		b, _ := json.Marshal(map[string]any{"ok": true, "cmd": args["cmd"], "args": args["args"], "tab": "chats"})
		return string(b), false
	})
	code, got := postCLI(t, srv, `{"cmd":"type","args":{"text":"secret words"},"timeout":5}`)
	if code != http.StatusOK || got["ok"] != true || got["cmd"] != "type" || got["tab"] != "chats" {
		t.Fatalf("%d %v", code, got)
	}
	if a, _ := got["args"].(map[string]any); a["text"] != "secret words" {
		t.Fatalf("args did not reach the phone: %v", got)
	}
	call := <-phone.calls
	if call["name"] != runtimeCLITool {
		t.Fatalf("tool_call %v", call)
	}
	l := logs.String()
	if !strings.Contains(l, "runtime-cli: cmd=type device=pairing-token status=200") {
		t.Fatalf("no audit line: %q", l)
	}
	if strings.Contains(l, "secret words") {
		t.Fatalf("the audit line holds the arguments: %q", l)
	}
}

// The runtime CLI is not one of the phone's tools: agents never see it, and cannot call it.
func TestRuntimeCLIIsNotAnAgentTool(t *testing.T) {
	h, srv, _ := runtimeCLIServer(t)
	called := make(chan bool, 1)
	dialPhone(t, h, srv, false, []string{"phone_contacts", runtimeCLITool}, func(map[string]any) (string, bool) {
		called <- true
		return `{"ok":true}`, false
	})
	for _, tool := range h.mcpTools(true) {
		if tool.Name == runtimeCLITool {
			t.Fatal("phone_runtime_cli is in the agents' tool list")
		}
	}
	if !hasTool(h.mcpTools(false), "phone_contacts") {
		t.Fatal("the phone's other tools are still there")
	}
	res := h.callTool(context.Background(), "server", runtimeCLITool, json.RawMessage(`{"cmd":"state"}`))
	if !res.IsError {
		t.Fatalf("an agent's call went through: %+v", res)
	}
	select {
	case <-called:
		t.Fatal("the phone got an agent's runtime CLI call")
	case <-time.After(100 * time.Millisecond):
	}
}

func hasTool(tools []proto.Tool, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

// No phone, or a phone without the runtime CLI (a Release build, or the switch off): 503 phone_offline.
func TestRuntimeCLIPhoneOffline(t *testing.T) {
	h, srv, _ := runtimeCLIServer(t)
	if code, got := postCLI(t, srv, `{"cmd":"state"}`); code != http.StatusServiceUnavailable || got["error"] != "phone_offline" {
		t.Fatalf("no phone: %d %v", code, got)
	}
	dialPhone(t, h, srv, false, []string{"phone_contacts"}, nil)
	if code, got := postCLI(t, srv, `{"cmd":"state"}`); code != http.StatusServiceUnavailable || got["error"] != "phone_offline" {
		t.Fatalf("a phone without the runtime CLI: %d %v", code, got)
	}
}

// Only a full device credential: an answer-only one (a watch) is refused, by the role check and by the hub's
// general rule for answer-only writes.
func TestRuntimeCLIAnswerOnlyRefused(t *testing.T) {
	h, srv, _ := runtimeCLIServer(t)
	dialPhone(t, h, srv, false, []string{runtimeCLITool}, func(map[string]any) (string, bool) { return `{"ok":true}`, false })
	if allowed(principal{ID: "dev_watch", Role: roleAnswer}, http.MethodPost, "/api/v1/phone/runtime-cli") {
		t.Fatal("an answer-only device may post to the runtime CLI")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/phone/runtime-cli", strings.NewReader(`{"cmd":"state"}`))
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, principal{ID: "dev_watch", Role: roleAnswer}))
	w := httptest.NewRecorder()
	h.runtimeCLI(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("answer-only: %d %s", w.Code, w.Body)
	}
}

// A phone that does not answer in time: 504; one whose app says the call failed: 502.
func TestRuntimeCLITimeoutAndFailure(t *testing.T) {
	h, srv, _ := runtimeCLIServer(t)
	dialPhone(t, h, srv, false, []string{runtimeCLITool}, nil)
	start := time.Now()
	code, got := postCLI(t, srv, `{"cmd":"settle","timeout":0.3}`)
	if code != http.StatusGatewayTimeout || got["ok"] != false || time.Since(start) > 3*time.Second {
		t.Fatalf("no answer: %d %v after %v", code, got, time.Since(start))
	}

	h2, srv2, _ := runtimeCLIServer(t)
	dialPhone(t, h2, srv2, false, []string{runtimeCLITool}, func(map[string]any) (string, bool) { return "the CLI is off", true })
	if code, got := postCLI(t, srv2, `{"cmd":"state"}`); code != http.StatusBadGateway || got["error"] != "the CLI is off" {
		t.Fatalf("app error: %d %v", code, got)
	}
}

// With a real iPhone and a simulator both offering it, the real iPhone takes the call.
func TestRuntimeCLIPrefersTheRealPhone(t *testing.T) {
	h, srv, _ := runtimeCLIServer(t)
	dialPhone(t, h, srv, true, []string{runtimeCLITool}, func(map[string]any) (string, bool) { return `{"ok":true,"who":"sim"}`, false })
	if _, got := postCLI(t, srv, `{"cmd":"state"}`); got["who"] != "sim" {
		t.Fatalf("only a simulator: %v", got)
	}
	dialPhone(t, h, srv, false, []string{runtimeCLITool}, func(map[string]any) (string, bool) { return `{"ok":true,"who":"phone"}`, false })
	if _, got := postCLI(t, srv, `{"cmd":"state"}`); got["who"] != "phone" {
		t.Fatalf("with a real iPhone: %v", got)
	}
}

// A simulator driven by a script registers the runtime CLI alone: it answers runtime CLI calls but is not the phone
// (no agent's phone_* call goes to it, as before, when the app is launched for automation).
func TestRuntimeCLIAloneIsNotThePhone(t *testing.T) {
	h, srv, _ := runtimeCLIServer(t)
	dialPhone(t, h, srv, true, []string{runtimeCLITool}, func(map[string]any) (string, bool) { return `{"ok":true}`, false })
	if h.phoneClient() != nil {
		t.Fatal("a runtime-CLI-only app became the phone")
	}
	if code, _ := postCLI(t, srv, `{"cmd":"state"}`); code != http.StatusOK {
		t.Fatalf("runtime CLI on it: %d", code)
	}
}
