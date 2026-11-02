package hub

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
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
