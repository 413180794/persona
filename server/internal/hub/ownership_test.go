package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// fakeManager is a manager on the bus that answers every http.request with who it is and a list of one item of its
// own under each list key, and every devices.call with who it is.
type fakeManager struct {
	id  string
	bus *rawBus
	got chan string // "METHOD path" of each http.request
}

func connectFakeManager(t *testing.T, h *Hub, srv *httptest.Server, id string) *fakeManager {
	t.Helper()
	f := &fakeManager{id: id, got: make(chan string, 64)}
	f.bus = dialRawBusWith(t, srv, busToken(t, h, id, id), func(_ context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case assistantwire.MethodHTTP:
			var req assistantwire.HTTPRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, err
			}
			select {
			case f.got <- req.Method + " " + req.Path:
			default:
			}
			item := []map[string]any{{"id": "item-" + id}}
			body, _ := json.Marshal(map[string]any{"who": id, "items": item, "todos": item, "as_of": "t"})
			return assistantwire.HTTPResponse{Status: http.StatusOK, Header: map[string][]string{"Content-Type": {"application/json"}}, Body: body}, nil
		case devicewire.MethodCall:
			return map[string]any{"who": id}, nil
		}
		return nil, &rpc.Error{Code: "no_such_thing", Message: "the fake manager has no " + method}
	})
	if err := f.bus.call("manager.hello", map[string]any{"id": id}, nil); err != nil {
		t.Fatal(err)
	}
	return f
}

// report says the manager carries machine mid (devices.state), connected.
func (f *fakeManager) report(t *testing.T, mids ...string) {
	t.Helper()
	st := devicewire.States{}
	for _, mid := range mids {
		st.Machines = append(st.Machines, devicewire.State{Machine: proto.Machine{ID: mid, Name: mid, Online: true}, Connected: true,
			ConnectedAt: time.Now().UTC()})
	}
	if err := f.bus.peer.Notify(devicewire.EventState, st); err != nil {
		t.Fatal(err)
	}
}

func ownershipGet(t *testing.T, h *Hub, srv *httptest.Server, method, path string, header map[string]string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return resp.StatusCode, out
}

// With two managers on the bus, a request goes to the manager its thing belongs to, or the one it names, else the
// default; lists merge both, each item stamped; an offline owner is 503, an unknown one 404.
func TestOwnershipRoutesByManager(t *testing.T) {
	h, srv, _ := newBusHub(t)
	a := connectFakeManager(t, h, srv, assistantwire.AssistantID)
	b := connectFakeManager(t, h, srv, "m-0a0b0c")
	a.report(t, "ma")
	b.report(t, "mb")
	eventually(t, 5*time.Second, "both machines reported", func() bool {
		return h.machineOwner("ma") == assistantwire.AssistantID && h.machineOwner("mb") == "m-0a0b0c" &&
			h.localMachine("mb") != nil && h.localMachine("ma") != nil
	})

	// What names no thing: the default, or the manager it names (header or query).
	for _, tc := range []struct {
		path   string
		header map[string]string
		want   string
	}{
		{"/api/v1/assistant", nil, assistantwire.AssistantID},
		{"/api/v1/assistant", map[string]string{ManagerHeader: "m-0a0b0c"}, "m-0a0b0c"},
		{"/api/v1/assistant?manager=m-0a0b0c", nil, "m-0a0b0c"},
		{"/api/v1/model-config", map[string]string{ManagerHeader: "m-0a0b0c"}, "m-0a0b0c"},
		{"/api/v1/inbox/x?manager=m-0a0b0c", nil, "m-0a0b0c"},
		// A machine's route goes to its manager, whatever the request names.
		{"/api/v1/machines/mb/herdr", nil, "m-0a0b0c"},
		{"/api/v1/machines/ma/herdr", map[string]string{ManagerHeader: "m-0a0b0c"}, assistantwire.AssistantID},
	} {
		code, out := ownershipGet(t, h, srv, http.MethodGet, tc.path, tc.header)
		if code != http.StatusOK || out["who"] != tc.want {
			t.Fatalf("%s %v: %d %v, want %s's", tc.path, tc.header, code, out, tc.want)
		}
	}
	if code, out := ownershipGet(t, h, srv, http.MethodGet, "/api/v1/assistant", map[string]string{ManagerHeader: "m-ffffff"}); code != http.StatusNotFound || out["code"] != "no_manager" {
		t.Fatalf("unknown manager: %d %v", code, out)
	}

	// A list merges every manager's, the default's first, each item with its manager.
	code, out := ownershipGet(t, h, srv, http.MethodGet, "/api/v1/inbox", nil)
	items, _ := out["items"].([]any)
	if code != http.StatusOK || len(items) != 2 || out["as_of"] != "t" {
		t.Fatalf("merged inbox: %d %v", code, out)
	}
	for i, want := range []string{assistantwire.AssistantID, "m-0a0b0c"} {
		it := items[i].(map[string]any)
		if it["manager"] != want || it["id"] != "item-"+want {
			t.Fatalf("item %d: %v, want %s's", i, it, want)
		}
	}
	// Naming a manager lists only its own.
	if _, out := ownershipGet(t, h, srv, http.MethodGet, "/api/v1/todos?manager=m-0a0b0c", nil); len(out["todos"].([]any)) != 1 ||
		out["todos"].([]any)[0].(map[string]any)["manager"] != "m-0a0b0c" {
		t.Fatalf("one manager's todos: %v", out)
	}

	// The machines and their sessions carry their manager.
	machines := map[string]string{}
	for _, m := range h.machineList() {
		machines[m.ID] = m.Manager
	}
	if machines["ma"] != assistantwire.AssistantID || machines["mb"] != "m-0a0b0c" {
		t.Fatalf("machines' managers: %v", machines)
	}
	h.takeFleetSessions(assistantwire.FleetSessions{MachineID: "mb", Sessions: []proto.Session{{MachineID: "mb", SessionID: "s1"}}})
	if list := h.decorated(h.localMachine("mb")); len(list) != 1 || list[0].Manager != "m-0a0b0c" {
		t.Fatalf("sessions' manager: %+v", list)
	}
	// A node call goes through the machine's manager.
	var who map[string]any
	if err := h.localMachine("mb").call(context.Background(), "panes.list", map[string]any{}, &who); err != nil || who["who"] != "m-0a0b0c" {
		t.Fatalf("node call through mb's manager: %v %v", who, err)
	}

	// An interaction is the manager's that reported it: its resolve goes there.
	if err := b.bus.peer.Notify(assistantwire.EventFleetInteraction, assistantwire.FleetInteraction{Interaction: proto.Interaction{
		ID: "ix-b", MachineID: "mb", SessionID: "s1", Kind: "question", CreatedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "the interaction", func() bool { return h.interactionOwner("ix-b") == "m-0a0b0c" })
	if code, out := ownershipGet(t, h, srv, http.MethodPost, "/api/v1/interactions/ix-b/resolve", nil); code != http.StatusOK || out["who"] != "m-0a0b0c" {
		t.Fatalf("resolve: %d %v", code, out)
	}
	// The assistant's own list replacing its interactions leaves the other manager's.
	h.takeFleetInteractions(nil)
	if h.interactionOwner("ix-b") != "m-0a0b0c" {
		t.Fatal("the assistant's report dropped another manager's interaction")
	}

	// Offline: its machine stays listed (offline, still its), its routes are 503, the lists go on without it.
	_ = b.bus.ws.Close(websocket.StatusNormalClosure, "test")
	eventually(t, 5*time.Second, "m-0a0b0c offline", func() bool { return h.busConnOf("m-0a0b0c") == nil })
	if code, out := ownershipGet(t, h, srv, http.MethodGet, "/api/v1/machines/mb/herdr", nil); code != http.StatusServiceUnavailable || out["code"] != assistantwire.CodeAssistantOffline {
		t.Fatalf("offline owner's machine: %d %v", code, out)
	}
	if code, out := ownershipGet(t, h, srv, http.MethodGet, "/api/v1/assistant", map[string]string{ManagerHeader: "m-0a0b0c"}); code != http.StatusServiceUnavailable || out["code"] != assistantwire.CodeAssistantOffline {
		t.Fatalf("offline manager named: %d %v", code, out)
	}
	if _, out := ownershipGet(t, h, srv, http.MethodGet, "/api/v1/inbox", nil); len(out["items"].([]any)) != 1 {
		t.Fatalf("inbox without the offline manager: %v", out)
	}
	eventually(t, 5*time.Second, "mb shown offline, still m-0a0b0c's", func() bool {
		for _, m := range h.machineList() {
			if m.ID == "mb" {
				return !m.Online && m.Manager == "m-0a0b0c"
			}
		}
		return false
	})
	if m := h.localMachine("ma"); m == nil || !m.snapshot().Online {
		t.Fatal("the other manager's machine went offline too")
	}
}

// One manager: a list's answer is the manager's own with each item stamped, nothing else changed.
func TestMergeListOneManager(t *testing.T) {
	rec := httptest.NewRecorder()
	writeMerged(rec, "items", []listAnswer{{id: "assistant", status: 200, header: http.Header{"X-Kept": {"1"}},
		body: []byte(`{"items":[{"id":"a","n":1},{}],"as_of":"t"}`)}})
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	items := out["items"].([]any)
	if rec.Header().Get("X-Kept") != "1" || out["as_of"] != "t" || len(items) != 2 ||
		items[0].(map[string]any)["manager"] != "assistant" || items[0].(map[string]any)["n"] != 1.0 || items[1].(map[string]any)["manager"] != "assistant" {
		t.Fatalf("merged: %s", rec.Body.String())
	}
	// Nobody answered: the default's answer as it is.
	rec = httptest.NewRecorder()
	writeMerged(rec, "items", []listAnswer{{id: "assistant", status: 503, body: []byte(`{"code":"assistant_offline"}`)}})
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "assistant_offline") {
		t.Fatalf("offline: %d %s", rec.Code, rec.Body.String())
	}
}
