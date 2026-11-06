package hub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/rpc"
)

// fullNode is a node as the hub sees it for full herdr views: herdr.client.open (answering as told) and the events the
// hub sends, recorded in order.
type fullNode struct {
	mu      sync.Mutex
	log     []string
	openErr error
	hub     *Hub
}

func newFullNode(t *testing.T, openErr error) *fullNode {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide := rpc.New("h", h2nW)
	nodeSide := rpc.New("n", n2hW)
	fn := &fullNode{openErr: openErr}
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		fn.mu.Lock()
		defer fn.mu.Unlock()
		fn.log = append(fn.log, method+" "+string(params))
		if method == "herdr.client.open" && fn.openErr != nil {
			return nil, fn.openErr
		}
		return map[string]any{}, nil
	}
	nodeSide.OnEvent = func(event string, params json.RawMessage) {
		fn.mu.Lock()
		defer fn.mu.Unlock()
		fn.log = append(fn.log, event+" "+string(params))
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	h := &Hub{byID: map[string]*Machine{}, logger: log.New(io.Discard, "", 0)}
	m := newMachine(h, MachineConfig{ID: "m1", Name: "m1"})
	m.peer = hubSide
	h.byID["m1"] = m
	fn.hub = h
	return fn
}

func (fn *fullNode) await(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		fn.mu.Lock()
		got := append([]string(nil), fn.log...)
		fn.mu.Unlock()
		if len(got) >= n {
			time.Sleep(30 * time.Millisecond)
			fn.mu.Lock()
			got = append([]string(nil), fn.log...)
			fn.mu.Unlock()
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("node log %q, want %d entries", got, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// nothingMore checks the node heard nothing past n entries.
func (fn *fullNode) nothingMore(t *testing.T, n int) {
	t.Helper()
	time.Sleep(60 * time.Millisecond)
	fn.mu.Lock()
	defer fn.mu.Unlock()
	if len(fn.log) != n {
		t.Fatalf("node log %q, want %d entries", fn.log, n)
	}
}

// told drains what a client was sent and returns the herdr_* messages.
func told(c *client) []map[string]any {
	var out []map[string]any
	for {
		select {
		case b := <-c.out:
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			if s, _ := m["type"].(string); strings.HasPrefix(s, "herdr_") {
				out = append(out, m)
			}
		default:
			return out
		}
	}
}

func awaitTold(t *testing.T, c *client, n int) []map[string]any {
	t.Helper()
	var got []map[string]any
	deadline := time.Now().Add(3 * time.Second)
	for len(got) < n && time.Now().Before(deadline) {
		got = append(got, told(c)...)
		time.Sleep(5 * time.Millisecond)
	}
	if len(got) < n {
		t.Fatalf("told %v, want %d messages", got, n)
	}
	return got
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// nodeID is the id the node knows the only view by.
func nodeID(t *testing.T, h *Hub) string {
	t.Helper()
	h.fulls.mu.Lock()
	defer h.fulls.mu.Unlock()
	if len(h.fulls.byNode) != 1 {
		t.Fatalf("%d views, want 1", len(h.fulls.byNode))
	}
	for k := range h.fulls.byNode {
		return k
	}
	return ""
}

func views(h *Hub) int {
	h.fulls.mu.Lock()
	defer h.fulls.mu.Unlock()
	return len(h.fulls.byNode)
}

func dataMsg(id string, data string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"client_id": id, "data": data})
	return b
}

func TestHerdrViewNeedsAFullDevice(t *testing.T) {
	fn := newFullNode(t, nil)
	h := fn.hub
	answer, full := termClient(h, roleAnswer), termClient(h, roleFull)

	say(h, answer, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "v1", "cols": 100, "rows": 30})
	got := awaitTold(t, answer, 1)
	if got[0]["type"] != "herdr_end" || got[0]["code"] != "forbidden" || !strings.Contains(fmt.Sprint(got[0]["error"]), "full device") {
		t.Fatalf("an answer-only device was told %v", got)
	}
	if views(h) != 0 {
		t.Fatal("a view was made for a device that may not open one")
	}
	fn.nothingMore(t, 0) // the node was never asked

	say(h, full, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "v1", "session_id": "pane:w1:p2", "cols": 100, "rows": 30, "focus": true})
	log := fn.await(t, 1)
	if !strings.HasPrefix(log[0], "herdr.client.open ") || !strings.Contains(log[0], `"cols":100`) ||
		!strings.Contains(log[0], `"session_id":"pane:w1:p2"`) || !strings.Contains(log[0], `"focus":true`) {
		t.Fatalf("node log = %q", log)
	}
	if m := awaitTold(t, full, 1)[0]; m["type"] != "herdr_state" || m["state"] != "open" || m["id"] != "v1" {
		t.Fatalf("told %v", m)
	}
}

func TestHerdrViewRejectsBadRequests(t *testing.T) {
	fn := newFullNode(t, nil)
	h := fn.hub
	full := termClient(h, roleFull)
	for _, bad := range []map[string]any{
		{"type": "herdr_open", "machine_id": "m1", "id": "", "cols": 80, "rows": 24},
		{"type": "herdr_open", "machine_id": "m1", "id": strings.Repeat("x", 65), "cols": 80, "rows": 24},
		{"type": "herdr_open", "machine_id": "m1", "id": "v", "cols": 0, "rows": 24},
		{"type": "herdr_open", "machine_id": "m1", "id": "v", "cols": 80, "rows": fullMaxRows + 1},
		{"type": "herdr_open", "machine_id": "nowhere", "id": "v", "cols": 80, "rows": 24},
	} {
		say(h, full, bad)
		if got := awaitTold(t, full, 1); got[0]["type"] != "herdr_end" {
			t.Fatalf("%v was told %v", bad, got)
		}
	}
	if views(h) != 0 {
		t.Fatal("a bad request made a view")
	}
	fn.nothingMore(t, 0)
}

func TestHerdrViewRelaysBothWays(t *testing.T) {
	fn := newFullNode(t, nil)
	h := fn.hub
	a := termClient(h, roleFull)
	say(h, a, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "v1", "cols": 100, "rows": 30})
	fn.await(t, 1)
	awaitTold(t, a, 1) // herdr_state
	id := nodeID(t, h)

	// The node's bytes go to the app under the app's own id.
	h.onHerdrClientData("m1", dataMsg(id, b64("hello")))
	h.onHerdrClientData("m9", dataMsg(id, b64("from another machine's node"))) // a node reaches only its own machine's views
	h.onHerdrClientData("m1", dataMsg("hc_nobody", b64("nobody's")))
	got := awaitTold(t, a, 1)
	if len(got) != 1 || got[0]["type"] != "herdr_data" || got[0]["id"] != "v1" || got[0]["data"] != b64("hello") {
		t.Fatalf("told %v", got)
	}

	// The app's keys, size and acks go to the node in order, under the node's id.
	say(h, a, map[string]any{"type": "herdr_input", "id": "v1", "data": "ls\r"})
	say(h, a, map[string]any{"type": "herdr_input", "id": "v1", "b64": b64("\x1b[M")})
	say(h, a, map[string]any{"type": "herdr_resize", "id": "v1", "cols": 90, "rows": 25})
	say(h, a, map[string]any{"type": "herdr_resize", "id": "v1", "cols": 0, "rows": 25}) // not a size
	say(h, a, map[string]any{"type": "herdr_ack", "id": "v1", "bytes": 5})
	say(h, a, map[string]any{"type": "herdr_ack", "id": "v1", "bytes": 999}) // more than was given: held at what the app acked before
	say(h, a, map[string]any{"type": "herdr_close", "id": "v1"})
	log := fn.await(t, 7)
	want := []string{
		fmt.Sprintf(`herdr.client.input {"client_id":%q,"data":"ls\r"}`, id),
		fmt.Sprintf(`herdr.client.input {"b64":%q,"client_id":%q}`, b64("\x1b[M"), id),
		fmt.Sprintf(`herdr.client.resize {"client_id":%q,"cols":90,"rows":25}`, id),
		fmt.Sprintf(`herdr.client.ack {"bytes":5,"client_id":%q}`, id),
		fmt.Sprintf(`herdr.client.ack {"bytes":5,"client_id":%q}`, id),
		fmt.Sprintf(`herdr.client.close {"client_id":%q}`, id),
	}
	if fmt.Sprint(log[1:]) != fmt.Sprint(want) {
		t.Fatalf("node log\n %q\nwant\n %q", log[1:], want)
	}
	if views(h) != 0 {
		t.Fatal("the view outlived its close")
	}
	// Closed: nothing more goes either way.
	say(h, a, map[string]any{"type": "herdr_input", "id": "v1", "data": "x"})
	h.onHerdrClientData("m1", dataMsg(id, b64("late")))
	fn.nothingMore(t, 7)
	if got := told(a); len(got) != 0 {
		t.Fatalf("told after close: %v", got)
	}
}

func TestHerdrViewIsOnlyReachableByItsOwner(t *testing.T) {
	fn := newFullNode(t, nil)
	h := fn.hub
	a, b := termClient(h, roleFull), termClient(h, roleFull)
	say(h, a, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "v1", "cols": 100, "rows": 30})
	fn.await(t, 1)
	// b guesses a's view id: its messages find no view of b's.
	say(h, b, map[string]any{"type": "herdr_input", "id": "v1", "data": "rm -rf"})
	say(h, b, map[string]any{"type": "herdr_resize", "id": "v1", "cols": 10, "rows": 10})
	say(h, b, map[string]any{"type": "herdr_ack", "id": "v1", "bytes": 1})
	say(h, b, map[string]any{"type": "herdr_close", "id": "v1"})
	fn.nothingMore(t, 1)
	if views(h) != 1 {
		t.Fatal("another connection closed the view")
	}
	// The same view id from b is b's own view.
	say(h, b, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "v1", "cols": 80, "rows": 24})
	fn.await(t, 2)
	if views(h) != 2 {
		t.Fatalf("%d views, want 2", views(h))
	}
}

func TestHerdrViewCapsPerConnectionAndRefusesDuplicates(t *testing.T) {
	fn := newFullNode(t, nil)
	h := fn.hub
	a := termClient(h, roleFull)
	for i := 0; i < fullPerClient; i++ {
		say(h, a, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": fmt.Sprint("v", i), "cols": 80, "rows": 24})
	}
	fn.await(t, fullPerClient)
	say(h, a, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "v0", "cols": 80, "rows": 24})
	say(h, a, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "extra", "cols": 80, "rows": 24})
	var ends []map[string]any
	deadline := time.Now().Add(3 * time.Second)
	for len(ends) < 2 && time.Now().Before(deadline) {
		for _, m := range told(a) {
			if m["type"] == "herdr_end" {
				ends = append(ends, m)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(ends) != 2 || ends[0]["code"] != "bad_request" || ends[1]["code"] != "too_many" {
		t.Fatalf("ends = %v", ends)
	}
	fn.nothingMore(t, fullPerClient)
}

func TestHerdrViewEndsWhenTheNodeCannot(t *testing.T) {
	for name, tc := range map[string]struct {
		err         error
		unsupported bool
		code        string
	}{
		"old node":        {&rpc.Error{Code: "", Message: "unknown method herdr.client.open"}, true, ""},
		"no herdr":        {&rpc.Error{Code: "", Message: "unsupported: this machine has no host that can run a full client"}, true, ""},
		"machine is full": {&rpc.Error{Code: "too_many", Message: "this computer already runs 4 full herdr views"}, false, "too_many"},
	} {
		t.Run(name, func(t *testing.T) {
			fn := newFullNode(t, tc.err)
			h := fn.hub
			a := termClient(h, roleFull)
			say(h, a, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "v1", "cols": 80, "rows": 24})
			got := awaitTold(t, a, 1)
			if got[0]["type"] != "herdr_end" || (got[0]["unsupported"] == true) != tc.unsupported || fmt.Sprint(got[0]["code"]) != fmt.Sprint(orNil(tc.code)) {
				t.Fatalf("told %v", got)
			}
			if views(h) != 0 {
				t.Fatal("a refused view stays registered")
			}
		})
	}
}

func orNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func TestHerdrViewBackpressure(t *testing.T) {
	fn := newFullNode(t, nil)
	h := fn.hub
	a := termClient(h, roleFull)
	a.out = make(chan []byte, 4096)
	say(h, a, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "v1", "cols": 80, "rows": 24})
	fn.await(t, 1)
	id := nodeID(t, h)
	chunk := strings.Repeat("a", 32<<10)

	// A browser that acks keeps up however much comes.
	sent := 0
	for i := 0; i < 64; i++ { // 2 MiB in all
		h.onHerdrClientData("m1", dataMsg(id, b64(chunk)))
		sent += len(chunk)
		say(h, a, map[string]any{"type": "herdr_ack", "id": "v1", "bytes": sent})
	}
	if views(h) != 1 {
		t.Fatal("the view ended though the browser kept acknowledging")
	}

	// One that does not is cut off at the hub's bound, and the node's client is stopped.
	for i := 0; i < fullHubWindow/len(chunk)+2 && views(h) == 1; i++ {
		h.onHerdrClientData("m1", dataMsg(id, b64(chunk)))
	}
	if views(h) != 0 {
		t.Fatal("a browser that never acks was not cut off")
	}
	var end map[string]any
	for _, m := range told(a) {
		if m["type"] == "herdr_end" {
			end = m
		}
	}
	if end == nil || !strings.Contains(fmt.Sprint(end["error"]), "behind") {
		t.Fatalf("end = %v", end)
	}
	log := fn.await(t, 1+64+1)
	if last := log[len(log)-1]; !strings.HasPrefix(last, "herdr.client.close ") {
		t.Fatalf("node's last message = %q", last)
	}
}

func TestHerdrViewEndsWhenTheBrowsersQueueIsFull(t *testing.T) {
	fn := newFullNode(t, nil)
	h := fn.hub
	a := termClient(h, roleFull)
	a.out = make(chan []byte, 2)
	say(h, a, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "v1", "cols": 80, "rows": 24})
	fn.await(t, 1)
	id := nodeID(t, h)
	for i := 0; i < 4; i++ { // the queue holds the state message and one chunk
		h.onHerdrClientData("m1", dataMsg(id, b64("x")))
	}
	if views(h) != 0 {
		t.Fatal("the view outlived a full queue")
	}
	fn.await(t, 2) // and the node was told to stop the client
}

func TestHerdrViewGoesWithItsConnectionItsMachineAndItsClient(t *testing.T) {
	fn := newFullNode(t, nil)
	h := fn.hub
	a, b := termClient(h, roleFull), termClient(h, roleFull)
	say(h, a, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "v1", "cols": 80, "rows": 24})
	say(h, b, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "v1", "cols": 80, "rows": 24})
	fn.await(t, 2)
	told(a)
	told(b)

	// a's connection drops: its client is told to stop; b's view stays.
	h.fullDrop(a)
	log := fn.await(t, 3)
	if !strings.HasPrefix(log[2], "herdr.client.close ") || views(h) != 1 {
		t.Fatalf("node log %q, %d views", log, views(h))
	}
	// The node's client quits on its own: the app is told why.
	id := nodeID(t, h)
	h.onHerdrClientEnd("m1", json.RawMessage(fmt.Sprintf(`{"client_id":%q,"error":"herdr client exited: 1"}`, id)))
	if m := awaitTold(t, b, 1)[0]; m["type"] != "herdr_end" || m["id"] != "v1" || m["error"] != "herdr client exited: 1" {
		t.Fatalf("told %v", m)
	}
	if views(h) != 0 {
		t.Fatal("an ended view stays registered")
	}
	// A machine going offline ends the views on it.
	say(h, a, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "v2", "cols": 80, "rows": 24})
	fn.await(t, 4)
	told(a)
	h.fullsOffline("m1")
	if m := awaitTold(t, a, 1)[0]; m["type"] != "herdr_end" || m["code"] != "offline" {
		t.Fatalf("told %v", m)
	}
	if views(h) != 0 {
		t.Fatal("a view stays on an offline machine")
	}
}

func TestHerdrViewKeepaliveNamesTheViewsItHas(t *testing.T) {
	fn := newFullNode(t, nil)
	h := fn.hub
	a := termClient(h, roleFull)
	say(h, a, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "v1", "cols": 80, "rows": 24})
	fn.await(t, 1)
	id := nodeID(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	old := keepaliveEvery
	keepaliveEvery = 20 * time.Millisecond
	defer func() { keepaliveEvery = old }()
	go h.fullKeepalive(ctx)
	log := fn.await(t, 2)
	if log[1] != fmt.Sprintf(`herdr.client.keepalive {"client_ids":[%q]}`, id) {
		t.Fatalf("node log = %q", log)
	}
}

func TestHerdrViewEndsWithItsNodesConnection(t *testing.T) {
	fn := newFullNode(t, nil)
	h := fn.hub
	a := termClient(h, roleFull)
	say(h, a, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "v1", "cols": 80, "rows": 24})
	fn.await(t, 1)
	told(a)
	h.connectionEnded(h.byID["m1"], "电脑离线") // what the hub does when a node's connection is gone
	if m := awaitTold(t, a, 1)[0]; m["type"] != "herdr_end" || m["code"] != "offline" {
		t.Fatalf("told %v", m)
	}
	if views(h) != 0 {
		t.Fatal("a view stays on a machine whose connection ended")
	}
}

func TestHerdrFocusIsForTheViewsOwnerOnAFullDevice(t *testing.T) {
	fn := newFullNode(t, nil)
	h := fn.hub
	a, b := termClient(h, roleFull), termClient(h, roleFull)
	say(h, a, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "v1", "cols": 100, "rows": 30})
	fn.await(t, 1)
	id := nodeID(t, h)

	// b guesses a's view id: it has no such view, so the computer's focus is not moved.
	say(h, b, map[string]any{"type": "herdr_focus", "id": "v1", "session_id": "pane:w1:p2"})
	// a, but nothing to show / far too long: not forwarded.
	say(h, a, map[string]any{"type": "herdr_focus", "id": "v1", "session_id": ""})
	say(h, a, map[string]any{"type": "herdr_focus", "id": "v1", "session_id": strings.Repeat("s", 300)})
	say(h, a, map[string]any{"type": "herdr_focus", "id": "nope", "session_id": "pane:w1:p2"})
	fn.nothingMore(t, 1)

	say(h, a, map[string]any{"type": "herdr_focus", "id": "v1", "session_id": "pane:w1:p2"})
	log := fn.await(t, 2)
	if want := fmt.Sprintf(`herdr.client.focus {"client_id":%q,"session_id":"pane:w1:p2"}`, id); log[1] != want {
		t.Fatalf("node log = %q, want %q", log[1], want)
	}

	// A device that is no longer a full one (its view stays until it ends) cannot move the focus either.
	a.role = roleAnswer
	say(h, a, map[string]any{"type": "herdr_focus", "id": "v1", "session_id": "pane:w1:p3"})
	fn.nothingMore(t, 2)
}

func TestHerdrFocusEventsTellEachViewWhetherTheChangeIsItsOwn(t *testing.T) {
	fn := newFullNode(t, nil)
	h := fn.hub
	a, b := termClient(h, roleFull), termClient(h, roleFull)
	say(h, a, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "va", "cols": 100, "rows": 30})
	say(h, b, map[string]any{"type": "herdr_open", "machine_id": "m1", "id": "vb", "cols": 100, "rows": 30})
	fn.await(t, 2)
	awaitTold(t, a, 1) // herdr_state
	awaitTold(t, b, 1)
	h.fulls.mu.Lock()
	var idA string
	for k, r := range h.fulls.byNode {
		if r.id == "va" {
			idA = k
		}
	}
	h.fulls.mu.Unlock()

	event := func(p map[string]any) {
		raw, _ := json.Marshal(p)
		h.onHerdrFocus("m1", raw)
	}
	only := func(c *client) map[string]any {
		got := awaitTold(t, c, 1)
		if len(got) != 1 || got[0]["type"] != "herdr_focus" {
			t.Fatalf("told %v", got)
		}
		return got[0]
	}

	// A change a's client made: a's view follows it, b's only hears of it.
	event(map[string]any{"workspace_id": "w1", "tab_id": "w1:t2", "pane_id": "w1:p2", "session_id": "s1", "source": "client", "client_id": idA})
	ma, mb := only(a), only(b)
	for _, m := range []map[string]any{ma, mb} {
		if m["machine_id"] != "m1" || m["pane_id"] != "w1:p2" || m["tab_id"] != "w1:t2" || m["workspace_id"] != "w1" || m["session_id"] != "s1" || m["source"] != "client" {
			t.Fatalf("told %v", m)
		}
	}
	if ma["id"] != "va" || ma["mine"] != true || mb["id"] != "vb" || mb["mine"] != false {
		t.Fatalf("mine: a %v, b %v", ma, mb)
	}

	// Our own focus coming back: a's echo, and for b a change made elsewhere.
	event(map[string]any{"workspace_id": "w1", "tab_id": "w1:t1", "pane_id": "w1:p1", "source": "persona", "client_id": idA})
	ma, mb = only(a), only(b)
	if ma["source"] != "persona" || ma["mine"] != true || mb["source"] != "persona" || mb["mine"] != false || ma["session_id"] != nil {
		t.Fatalf("echo: a %v, b %v", ma, mb)
	}

	// Anything else (a source the hub does not know, a client id with it) is "other" and nobody's.
	event(map[string]any{"workspace_id": "w1", "tab_id": "w1:t1", "pane_id": "w1:p1", "source": "whatever", "client_id": idA})
	ma, mb = only(a), only(b)
	if ma["source"] != "other" || ma["mine"] != false || mb["mine"] != false {
		t.Fatalf("other: a %v, b %v", ma, mb)
	}

	// A node tells only the views of its own machine; a change with no pane says nothing.
	raw, _ := json.Marshal(map[string]any{"pane_id": "w1:p1", "source": "other"})
	h.onHerdrFocus("m9", raw)
	event(map[string]any{"workspace_id": "w1", "source": "other"})
	if got := told(a); len(got) != 0 {
		t.Fatalf("told %v", got)
	}
}

// TestHerdrDataPushMatchesMap pins the herdr_data bytes to what the map form gave, ids and data that need escaping
// included, so the browser sees the same message.
func TestHerdrDataPushMatchesMap(t *testing.T) {
	for _, c := range []struct{ id, data string }{
		{"v1", b64("hello")},
		{"", ""},
		{`v"<&>` + " ", "not base64 <script>"},
	} {
		want, err := json.Marshal(map[string]any{"type": "herdr_data", "id": c.id, "data": c.data})
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(herdrDataPush{Data: c.data, ID: c.id, Type: "herdr_data"})
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("got %s, want %s", got, want)
		}
	}
}

// BenchmarkHerdrClientData measures one 4 KB node output chunk going to a full view's app.
func BenchmarkHerdrClientData(b *testing.B) {
	r := &fullRelay{c: &client{out: make(chan []byte, 1)}, machineID: "m1", id: "v1", nodeID: "hc_1"}
	h := &Hub{byID: map[string]*Machine{}, logger: log.New(io.Discard, "", 0)}
	h.fulls.byNode = map[string]*fullRelay{"hc_1": r}
	params := dataMsg("hc_1", base64.StdEncoding.EncodeToString(make([]byte, 4096)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		h.onHerdrClientData("m1", params)
		<-r.c.out
		r.acked = r.sent // the app keeps up, so the window never closes
	}
}
