package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/db"
)

// managersHub is newBusHub with no install codes and no default picked.
func managersHub(t *testing.T) (*Hub, *httptest.Server, *db.DB) {
	t.Helper()
	h, srv, store := newBusHub(t)
	clear := func() {
		list, _ := store.PendingManagerInstalls(context.Background())
		for _, c := range list {
			_, _ = store.DropManagerInstallCodes(context.Background(), c.ManagerID)
		}
		_ = store.PutSetting(context.Background(), managerDefaultKey, nil)
	}
	clear()
	t.Cleanup(clear)
	return h, srv, store
}

// managersDo sends an app's request with the master token (loopback) and decodes the answer into out.
func managersDo(t *testing.T, h *Hub, srv *httptest.Server, method, path string, body, out any) int {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, srv.URL+path, rd)
	if path != bus.JoinPath {
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && len(raw) > 0 {
		if l, ok := out.(*managersReply); ok {
			*l = managersReply{} // json reuses the slice's elements, keeping fields the answer leaves out
		}
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: %v: %s", method, path, err, raw)
		}
	}
	return resp.StatusCode
}

type managersReply struct {
	Managers []managerInfo `json:"managers"`
	Default  string        `json:"default"`
}

func (r managersReply) find(id string) (managerInfo, bool) {
	for _, m := range r.Managers {
		if m.ID == id {
			return m, true
		}
	}
	return managerInfo{}, false
}

// The assistant the hub starts is the first manager and the default; a new one is made with a name, waits as
// installing, trades its code once for a token, connects, and the roster keeps where it ran after it drops.
func TestManagersRosterAndInstall(t *testing.T) {
	h, srv, _ := managersHub(t)
	assistantTok := busToken(t, h, assistantwire.AssistantID, "助手")
	_ = dialRawBus(t, srv, assistantTok)

	var list managersReply
	if code := managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list); code != 200 {
		t.Fatalf("list: %d", code)
	}
	if list.Default != assistantwire.AssistantID || len(list.Managers) != 1 || !list.Managers[0].Default || !list.Managers[0].Builtin {
		t.Fatalf("first roster: %+v", list)
	}

	var made struct {
		Manager   managerInfo `json:"manager"`
		Code      string      `json:"code"`
		ExpiresAt time.Time   `json:"expires_at"`
	}
	if code := managersDo(t, h, srv, "POST", "/api/v1/managers", map[string]any{"name": " "}, nil); code != 400 {
		t.Fatalf("empty name: %d", code)
	}
	if code := managersDo(t, h, srv, "POST", "/api/v1/managers", map[string]any{"name": "工作"}, &made); code != 201 {
		t.Fatalf("make: %d", code)
	}
	id := made.Manager.ID
	if !installCodeShape.MatchString(made.Code) || time.Until(made.ExpiresAt) > installCodeTTL+time.Minute || time.Until(made.ExpiresAt) < 9*time.Minute {
		t.Fatalf("code %q until %v", made.Code, made.ExpiresAt)
	}
	managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list)
	if m, ok := list.find(id); !ok || m.Installing == nil || m.Online || m.Name != "工作" {
		t.Fatalf("installing: %+v", list)
	}

	var joined struct {
		ID, Name, Token string
	}
	if code := managersDo(t, h, srv, "POST", bus.JoinPath, map[string]any{"code": "mi_" + "0123456789abcdef0123456789abcdef"}, nil); code != 401 {
		t.Fatalf("unknown code: %d", code)
	}
	if code := managersDo(t, h, srv, "POST", bus.JoinPath, map[string]any{"code": made.Code}, &joined); code != 200 {
		t.Fatalf("join: %d", code)
	}
	if joined.ID != id || joined.Name != "工作" || joined.Token == "" {
		t.Fatalf("joined: %+v", joined)
	}
	if code := managersDo(t, h, srv, "POST", bus.JoinPath, map[string]any{"code": made.Code}, nil); code != 401 {
		t.Fatalf("a code works once, got %d", code)
	}

	r := dialRawBus(t, srv, joined.Token)
	if err := r.call("manager.hello", map[string]any{"id": id, "host": "box", "os": "linux", "arch": "amd64", "deploy": "service", "version": "v1"}, nil); err != nil {
		t.Fatal(err)
	}
	managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list)
	m, _ := list.find(id)
	if !m.Online || m.Installing != nil || m.Host != "box" || m.Deploy != "service" || m.Default || m.Builtin {
		t.Fatalf("connected: %+v", m)
	}

	_ = r.ws.Close(websocket.StatusNormalClosure, "bye")
	deadline := time.Now().Add(5 * time.Second)
	for {
		managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list)
		if m, _ = list.find(id); !m.Online || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if m.Online || m.LastSeen == nil || m.Host != "box" {
		t.Fatalf("offline: %+v", m)
	}
}

// Rename, make default, revoke: the default and the hub's own cannot be removed; a revoked token is hung up and
// opens nothing.
func TestManagersPatchAndRevoke(t *testing.T) {
	h, srv, store := managersHub(t)
	busToken(t, h, assistantwire.AssistantID, "助手")
	tok := busToken(t, h, "m-abc123", "家")
	r := dialRawBus(t, srv, tok)
	if err := r.call("manager.hello", map[string]any{"id": "m-abc123"}, nil); err != nil {
		t.Fatal(err)
	}

	if code := managersDo(t, h, srv, "PATCH", "/api/v1/managers/m-abc123", map[string]any{"name": "家里", "default": true}, nil); code != 200 {
		t.Fatalf("patch: %d", code)
	}
	var list managersReply
	managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list)
	if list.Default != "m-abc123" || list.Managers[0].ID != "m-abc123" || list.Managers[0].Name != "家里" {
		t.Fatalf("after patch: %+v", list)
	}
	// The hub making the assistant a new token on start keeps the name the user gave it.
	managersDo(t, h, srv, "PATCH", "/api/v1/managers/"+assistantwire.AssistantID, map[string]any{"name": "工作助手"}, nil)
	busToken(t, h, assistantwire.AssistantID, "助手")
	if b, _ := store.BusIdentityByID(context.Background(), assistantwire.AssistantID); b == nil || b.Name != "工作助手" {
		t.Fatalf("assistant renamed: %+v", b)
	}

	if code := managersDo(t, h, srv, "DELETE", "/api/v1/managers/m-abc123", nil, nil); code != 409 {
		t.Fatalf("delete the default: %d", code)
	}
	if code := managersDo(t, h, srv, "DELETE", "/api/v1/managers/"+assistantwire.AssistantID, nil, nil); code != 409 {
		t.Fatalf("delete the builtin: %d", code)
	}
	managersDo(t, h, srv, "PATCH", "/api/v1/managers/"+assistantwire.AssistantID, map[string]any{"default": true}, nil)
	if code := managersDo(t, h, srv, "DELETE", "/api/v1/managers/m-abc123", nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	r.closedWith(t, bus.CloseRevoked)
	if _, ok := h.busIdentity(context.Background(), tok); ok {
		t.Fatal("a revoked token still names its manager")
	}
	if code := managersDo(t, h, srv, "DELETE", "/api/v1/managers/m-abc123", nil, nil); code != 404 {
		t.Fatalf("delete again: %d", code)
	}

	// A pending install is called off: its code no longer trades.
	var made struct {
		Manager managerInfo `json:"manager"`
		Code    string      `json:"code"`
	}
	managersDo(t, h, srv, "POST", "/api/v1/managers", map[string]any{"name": "临时"}, &made)
	if code := managersDo(t, h, srv, "DELETE", "/api/v1/managers/"+made.Manager.ID, nil, nil); code != 200 {
		t.Fatalf("call off: %d", code)
	}
	if code := managersDo(t, h, srv, "POST", bus.JoinPath, map[string]any{"code": made.Code}, nil); code != 401 {
		t.Fatalf("a called-off code traded: %d", code)
	}
}

// Only the apps that asked for "managers" hear that the roster changed (a manager connected).
func TestManagersEventIsOptIn(t *testing.T) {
	h, srv, _ := managersHub(t)
	plain := wsApp(t, h)
	asked := &client{device: "test-device-2", out: make(chan []byte, 64)}
	h.mu.Lock()
	h.clients[asked] = struct{}{}
	h.mu.Unlock()
	h.onClientMessage(context.Background(), asked, []byte(`{"type":"client_hello","features":["managers"]}`))
	<-asked.out // client_hello_ack
	r := dialRawBus(t, srv, busToken(t, h, "m-abc123", "家"))
	if err := r.call("manager.hello", map[string]any{"id": "m-abc123"}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case raw := <-asked.out:
		if string(raw) != `{"type":"managers"}` {
			t.Fatalf("got %s", raw)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the app that asked heard nothing")
	}
	select {
	case raw := <-plain.out:
		t.Fatalf("an app that did not ask got %s", raw)
	case <-time.After(100 * time.Millisecond):
	}
}
