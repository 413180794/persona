package hub

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/proto"
)

// memStore keeps interaction outcomes like the interactions table; other Store methods are not used here.
type memStore struct {
	Store
	mu     sync.Mutex
	status map[string]string
	res    map[string]*proto.Resolution
}

func newMemStore() *memStore {
	return &memStore{status: map[string]string{}, res: map[string]*proto.Resolution{}}
}

func (m *memStore) InsertInteraction(_ context.Context, ix proto.Interaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.status[ix.ID]; !ok {
		m.status[ix.ID] = "pending"
	}
	return nil
}

func (m *memStore) FinishInteraction(_ context.Context, id, status string, res *proto.Resolution) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status[id] == "pending" {
		m.status[id], m.res[id] = status, res
	}
	return nil
}

func (m *memStore) InteractionOutcome(_ context.Context, id string) (string, *proto.Resolution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status[id], m.res[id], nil
}

func (m *memStore) Setting(context.Context, string, any) (bool, error) { return false, nil }

// PutSetting keeps nothing, like Setting finds nothing: loading the schedule writes its one-time task list.
func (m *memStore) PutSetting(context.Context, string, any) error { return nil }

func waitPending(t *testing.T, h *Hub, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(h.pendingInteractions()) != n {
		if time.Now().After(deadline) {
			t.Fatalf("pending = %d, want %d", len(h.pendingInteractions()), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func resumable(id string) proto.Interaction {
	return proto.Interaction{ID: id, MachineID: "mac", SessionID: "s1", Kind: proto.IxPermission,
		ExpiresAt: time.Now().Add(time.Hour), Resumable: true}
}

// The node goes away under a waiting hook; the hook comes back through the next node and gets the answer.
func TestResumableSurvivesNodeRestart(t *testing.T) {
	h := newTestHub(t)
	h.store = newMemStore()
	ctx, nodeGone := context.WithCancel(context.Background())
	first := make(chan proto.Resolution, 1)
	go func() { r, _ := h.waitInteraction(ctx, resumable("ix_r")); first <- r }()
	waitPending(t, h, 1)
	nodeGone()
	<-first
	h.cancelMachineInteractions("mac") // the machine going offline leaves it for the hook
	waitPending(t, h, 1)

	second := make(chan proto.Resolution, 1)
	go func() { r, _ := h.waitInteraction(context.Background(), resumable("ix_r")); second <- r }()
	time.Sleep(20 * time.Millisecond)
	waitPending(t, h, 1) // the same card, not a second one
	yes := true
	if err := h.resolve("ix_r", proto.Resolution{Allow: &yes}); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-second:
		if r.Allow == nil || !*r.Allow {
			t.Fatalf("resumed hook got %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resumed hook never got the answer")
	}
	waitPending(t, h, 0)
}

// Answered while no node was there: the hook that comes back reads the answer.
func TestResumableAnsweredDuringGap(t *testing.T) {
	h := newTestHub(t)
	h.store = newMemStore()
	ctx, nodeGone := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _, _ = h.waitInteraction(ctx, resumable("ix_g")); close(done) }()
	waitPending(t, h, 1)
	nodeGone()
	<-done
	no := false
	if err := h.resolve("ix_g", proto.Resolution{Allow: &no, Message: "不行"}); err != nil {
		t.Fatal(err)
	}
	waitPending(t, h, 0)
	r, err := h.waitInteraction(context.Background(), resumable("ix_g"))
	if err != nil || r.Allow == nil || *r.Allow || r.Message != "不行" {
		t.Fatalf("got %+v %v", r, err)
	}
}

// Nobody claims it in time: it ends as cancelled, and a late hook is told to hand over to the terminal.
func TestResumableGraceEnds(t *testing.T) {
	old := resumeGrace
	resumeGrace = 50 * time.Millisecond
	defer func() { resumeGrace = old }()
	h := newTestHub(t)
	st := newMemStore()
	h.store = st
	ctx, nodeGone := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _, _ = h.waitInteraction(ctx, resumable("ix_x")); close(done) }()
	waitPending(t, h, 1)
	nodeGone()
	<-done
	waitPending(t, h, 0)
	if s, _, _ := st.InteractionOutcome(context.Background(), "ix_x"); s != "cancelled" {
		t.Fatalf("status %q", s)
	}
	r, _ := h.waitInteraction(context.Background(), resumable("ix_x"))
	if !r.Dismiss {
		t.Fatalf("a late hook must dismiss, got %+v", r)
	}
}

// Without the flag (an older node, mail approvals) the old behaviour stays: the node going away cancels it.
func TestNonResumableCancelledWithNode(t *testing.T) {
	h := newTestHub(t)
	ctx, nodeGone := context.WithCancel(context.Background())
	ix := resumable("ix_old")
	ix.Resumable = false
	done := make(chan struct{})
	go func() { _, _ = h.waitInteraction(ctx, ix); close(done) }()
	waitPending(t, h, 1)
	nodeGone()
	<-done
	waitPending(t, h, 0)
}

// Through the manager (devices.request): the node's connection ending ends the hub's wait (devices.request_end) though
// the bus stays up, so the hook that comes back through the next connection is the one that gets the answer.
func TestResumableSurvivesAReconnectThroughTheManager(t *testing.T) {
	h := newTestHub(t)
	h.store = newMemStore()
	m := newMachine(h, MachineConfig{ID: "mac"})
	h.machines, h.byID["mac"] = []*Machine{m}, m
	ask := func(key string) <-chan json.RawMessage {
		params, _ := json.Marshal(devicewire.NodeCall{MachineID: "mac", Method: "interaction.create", Request: key,
			Params: rawOf(t, map[string]any{"interaction": resumable("ix_b")})})
		out := make(chan json.RawMessage, 1)
		go func() {
			res, err := busMethods[devicewire.MethodRequest](h, context.Background(), nil, params)
			if err != nil {
				t.Errorf("%s: %v", key, err)
			}
			b, _ := json.Marshal(res)
			out <- b
		}()
		return out
	}
	end := func(key string) {
		busEvents[devicewire.EventRequestEnd](h, nil, rawOf(t, devicewire.RequestEnd{MachineID: "mac", Request: key}))
	}
	first := ask("k1")
	waitPending(t, h, 1)
	end("k1") // its connection ended: the hub's wait ends, the card stays
	select {
	case <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("the first request still waits after its connection ended")
	}
	waitPending(t, h, 1)
	second := ask("k2")
	time.Sleep(20 * time.Millisecond)
	yes := true
	if err := h.resolve("ix_b", proto.Resolution{Allow: &yes}); err != nil {
		t.Fatal(err)
	}
	select {
	case b := <-second:
		var r proto.Resolution
		if json.Unmarshal(b, &r) != nil || r.Allow == nil || !*r.Allow {
			t.Fatalf("the hook that came back got %s", b)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the hook that came back never got the answer")
	}

	// The end can come before the request is read (an event is handled at once, a call in its own goroutine).
	end("k3")
	params, _ := json.Marshal(devicewire.NodeCall{MachineID: "mac", Method: "interaction.create", Request: "k3",
		Params: rawOf(t, map[string]any{"interaction": resumable("ix_c")})})
	done := make(chan struct{})
	go func() { _, _ = busMethods[devicewire.MethodRequest](h, context.Background(), nil, params); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a request whose end came first still waits")
	}
}

func rawOf(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
