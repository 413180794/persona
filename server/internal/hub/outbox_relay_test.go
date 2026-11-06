package hub

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// What a manager kept while the hub was away is delivered in order, each item once: sent again (the manager did not
// hear the answer), or once live and again from the outbox, it does not push twice.
func TestTheOutboxIsDeliveredOnce(t *testing.T) {
	c := &client{device: "test-device", out: make(chan []byte, 16)}
	h := newTestHub(t)
	h.clients, h.push = map[*client]struct{}{c: {}}, newPusher(t.TempDir(), nil, t.Logf)
	h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","features":["ws_notify"]}`))
	<-c.out
	if err := h.push.setPrefs(c.device, NotifyPrefs{Input: false, Done: DoneOff}); err != nil {
		t.Fatal(err)
	}
	call := func(method string, p any) json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(p)
		out, err := busMethods[method](h, context.Background(), nil, raw)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		b, _ := json.Marshal(out)
		return b
	}
	n := func(id string) *assistantwire.Notification {
		return &assistantwire.Notification{ID: id, Kind: assistantwire.NotifyPush, Title: id}
	}
	todos, _ := json.Marshal(map[string]string{"type": "todos"})
	items := assistantwire.Outbox{Items: []assistantwire.OutboxItem{
		{ID: "ntf_a", At: time.Now(), Notification: n("ntf_a")},
		{ID: "bc_b", At: time.Now(), Message: todos},
		{ID: "ntf_c", At: time.Now(), Notification: n("ntf_c")},
	}}
	if got := string(call(assistantwire.MethodOutbox, items)); got != `{"done":["ntf_a","bc_b","ntf_c"]}` {
		t.Fatalf("done %s", got)
	}
	var seen []string
	for len(seen) < 3 {
		select {
		case raw := <-c.out:
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			switch {
			case m["type"] == "notify":
				seen = append(seen, m["title"].(string))
			case m["type"] == "todos":
				seen = append(seen, "todos")
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("delivered %v", seen)
		}
	}
	if seen[0] != "ntf_a" || seen[1] != "todos" || seen[2] != "ntf_c" {
		t.Fatalf("order %v", seen)
	}
	// Again (the answer was lost), and one of them live: nothing more reaches the apps.
	call(assistantwire.MethodOutbox, items)
	call(assistantwire.MethodNotify, assistantwire.NotifyRequest{Notification: *n("ntf_a")})
	select {
	case raw := <-c.out:
		t.Fatalf("delivered twice: %s", raw)
	case <-time.After(300 * time.Millisecond):
	}
	// A new push live still goes.
	call(assistantwire.MethodNotify, assistantwire.NotifyRequest{Notification: *n("ntf_d")})
	select {
	case raw := <-c.out:
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil || m["title"] != "ntf_d" {
			t.Fatalf("live push: %s", raw)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a new push did not go")
	}
}

// handedOver forgets the oldest past its bound, and anything after its time.
func TestHandedOverIsBounded(t *testing.T) {
	var s seenItems
	now := time.Now()
	if !s.first("a", now) || s.first("a", now) {
		t.Fatal("a twice")
	}
	if !s.first("a", now.Add(handedOverFor+time.Second)) {
		t.Fatal("a not forgotten after its time")
	}
	for i := range handedOverMost + 10 {
		s.first(string(rune('A'+i%26))+time.Duration(i).String(), now)
	}
	if len(s.order) > handedOverMost || len(s.at) > handedOverMost {
		t.Fatalf("kept %d / %d", len(s.order), len(s.at))
	}
}

// A card's push that came before its card is refused and not recorded as handed over (the manager forgets it all the
// same: it is answered done): the same push sent again once the card is known goes.
func TestARefusedOutboxPushIsNotRecorded(t *testing.T) {
	c := &client{device: "test-device", out: make(chan []byte, 16)}
	h := newTestHub(t)
	h.clients, h.push = map[*client]struct{}{c: {}}, newPusher(t.TempDir(), nil, t.Logf)
	h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","features":["ws_notify"]}`))
	<-c.out
	if err := h.push.setPrefs(c.device, NotifyPrefs{Input: true, Done: DoneOff}); err != nil {
		t.Fatal(err)
	}
	ix := proto.Interaction{ID: "ix-1", MachineID: "m", SessionID: "s", Kind: proto.IxQuestion, Summary: "请选择"}
	items := assistantwire.Outbox{Items: []assistantwire.OutboxItem{{ID: "ntf_ix", At: time.Now(),
		Notification: &assistantwire.Notification{ID: "ntf_ix", Kind: assistantwire.NotifyInput, Interaction: &ix}}}}
	raw, _ := json.Marshal(items)
	if _, err := busMethods[assistantwire.MethodOutbox](h, context.Background(), nil, raw); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-c.out:
		t.Fatalf("a card's push before its card: %s", got)
	case <-time.After(200 * time.Millisecond):
	}
	reportInteraction(t, h, ix)
	for drained := false; !drained; { // what the card itself sent the apps
		select {
		case <-c.out:
		case <-time.After(200 * time.Millisecond):
			drained = true
		}
	}
	if _, err := busMethods[assistantwire.MethodOutbox](h, context.Background(), nil, raw); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-c.out:
		var m map[string]any
		if json.Unmarshal(got, &m) != nil || m["interaction_id"] != "ix-1" {
			t.Fatalf("the card's push: %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the push did not go once the card was reported")
	}
}

// What one hub process delivered, the next one does not deliver again (handed_over in its database).
func TestHandedOverOutlivesTheHubProcess(t *testing.T) {
	_, _, store := newBusHub(t)
	id := "ntf_restart_" + randomHex(6) // the records stay in the database for two hours: a test run again needs its own
	deliveries := func() int {
		c := &client{device: "test-device", out: make(chan []byte, 16)}
		h := newTestHub(t)
		h.store = store
		h.clients, h.push = map[*client]struct{}{c: {}}, newPusher(t.TempDir(), nil, t.Logf)
		h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","features":["ws_notify"]}`))
		<-c.out
		_ = h.push.setPrefs(c.device, NotifyPrefs{Input: false, Done: DoneOff})
		raw, _ := json.Marshal(assistantwire.NotifyRequest{Notification: assistantwire.Notification{ID: id,
			Kind: assistantwire.NotifyPush, Title: "once"}})
		if _, err := busMethods[assistantwire.MethodNotify](h, context.Background(), nil, raw); err != nil {
			t.Fatal(err)
		}
		n := 0
		for {
			select {
			case <-c.out:
				n++
			case <-time.After(300 * time.Millisecond):
				return n
			}
		}
	}
	if n := deliveries(); n != 1 {
		t.Fatalf("the first hub delivered %d", n)
	}
	if n := deliveries(); n != 0 {
		t.Fatalf("the next hub delivered it again: %d", n)
	}
}

// flakyHandOver is a hand-over store that does not answer while fail is set.
type flakyHandOver struct {
	Store
	fail bool
	seen map[string]bool
}

func (f *flakyHandOver) HandOverFirst(_ context.Context, manager, id string, _ time.Duration) (bool, error) {
	if f.fail {
		return false, errors.New("database hiccup")
	}
	k := manager + "/" + id
	first := !f.seen[k]
	f.seen[k] = true
	return first, nil
}

func (f *flakyHandOver) HandOverForget(_ context.Context, manager, id string) error {
	delete(f.seen, manager+"/"+id)
	return nil
}

// An item handled while the store did not answer is not delivered again once it does.
func TestHandedOverWhileTheStoreWasDown(t *testing.T) {
	h := newTestHub(t)
	st := &flakyHandOver{fail: true, seen: map[string]bool{}}
	h.store = st
	if !h.firstHandOver(context.Background(), "assistant", "ntf_x") {
		t.Fatal("the first time")
	}
	st.fail = false
	if h.firstHandOver(context.Background(), "assistant", "ntf_x") {
		t.Fatal("delivered again once the store answered")
	}
	if !h.firstHandOver(context.Background(), "assistant", "ntf_y") {
		t.Fatal("a new one")
	}
}

// gatedHandOver is a hand-over store whose answers wait for a gate (a slow database).
type gatedHandOver struct {
	Store
	gate chan struct{}
	mu   sync.Mutex
	seen map[string]bool
}

func (g *gatedHandOver) HandOverFirst(_ context.Context, manager, id string, _ time.Duration) (bool, error) {
	<-g.gate
	g.mu.Lock()
	defer g.mu.Unlock()
	k := manager + "/" + id
	first := !g.seen[k]
	g.seen[k] = true
	return first, nil
}

func (g *gatedHandOver) HandOverForget(context.Context, string, string) error { return nil }

// Two sends of one id at once (a live push still being handled when it comes again from the outbox): exactly one is
// delivered, also with a slow store.
func TestTwoSendsOfOneIDAtOnce(t *testing.T) {
	h := newTestHub(t)
	st := &gatedHandOver{gate: make(chan struct{}), seen: map[string]bool{}}
	h.store = st
	results := make(chan bool, 2)
	for range 2 {
		go func() { results <- h.firstHandOver(context.Background(), "assistant", "ntf_z") }()
	}
	time.Sleep(100 * time.Millisecond)
	close(st.gate)
	a, b := <-results, <-results
	if a == b {
		t.Fatalf("both %v: want exactly one delivery", a)
	}
}
