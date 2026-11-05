package hub

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// alwaysStore is memStore with the assistant_always table.
type alwaysStore struct {
	*memStore
	amu  sync.Mutex
	kept map[[2]string]bool // session, key
}

func newAlwaysStore() *alwaysStore {
	return &alwaysStore{memStore: newMemStore(), kept: map[[2]string]bool{}}
}

func (s *alwaysStore) AllowAlways(_ context.Context, session, _ string, keys []string) error {
	s.amu.Lock()
	defer s.amu.Unlock()
	for _, k := range keys {
		s.kept[[2]string{session, k}] = true
	}
	return nil
}

func (s *alwaysStore) AlwaysAllowed(_ context.Context, session string, keys []string) (bool, error) {
	s.amu.Lock()
	defer s.amu.Unlock()
	for _, k := range keys {
		if !s.kept[[2]string{session, k}] {
			return false, nil
		}
	}
	return len(keys) > 0, nil
}

func alwaysIx(id, session string, keys ...string) proto.Interaction {
	return proto.Interaction{ID: id, MachineID: "mac", SessionID: session, Kind: proto.IxPermission, ToolName: "Bash",
		ExpiresAt: time.Now().Add(time.Hour), Resumable: true, AlwaysKeys: keys}
}

// ask is a node's interaction.create, answered with r once its card shows; got is what the node hears.
func ask(t *testing.T, h *Hub, ix proto.Interaction, r *proto.Resolution) (got proto.Resolution) {
	t.Helper()
	out := make(chan proto.Resolution, 1)
	go func() { res, _ := h.nodeInteraction(context.Background(), ix); out <- res }()
	if r != nil {
		deadline := time.Now().Add(5 * time.Second)
		for h.resolve(ix.ID, *r) != nil {
			if time.Now().After(deadline) {
				t.Fatalf("%s never showed", ix.ID)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	select {
	case got = <-out:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: no answer", ix.ID)
	}
	return got
}

// 本会话总是允许 on an assistant conversation's card outlasts the agent's process: the hub answers the same card of the
// resumed process itself (#2202), and only that conversation's, only for what it granted.
func TestAssistantAlwaysAllowKept(t *testing.T) {
	h := newTestHub(t)
	h.store = newAlwaysStore()
	yes, no := true, false

	// Allowed once: asked again.
	ask(t, h, alwaysIx("ix_1", "conv", "tool:Bash"), &proto.Resolution{Allow: &yes})
	waitPending(t, h, 0)
	if _, ok := h.alwaysAllowed(context.Background(), alwaysIx("ix_x", "conv", "tool:Bash")); ok {
		t.Fatal("allowing once was kept as always")
	}
	// A card without keys (an Agents session's) keeps nothing.
	ask(t, h, alwaysIx("ix_2", "agent"), &proto.Resolution{Allow: &yes, Always: true})
	if _, ok := h.alwaysAllowed(context.Background(), alwaysIx("ix_x", "agent", "tool:Bash")); ok {
		t.Fatal("a card without keys kept a grant")
	}
	// Denied with always set is no grant either.
	ask(t, h, alwaysIx("ix_3", "conv", "tool:Edit"), &proto.Resolution{Allow: &no, Always: true})

	// 本会话总是允许: the next card for the same tool is answered by the hub, with always so the new process takes
	// its own session rule, without showing.
	ask(t, h, alwaysIx("ix_4", "conv", "tool:Bash"), &proto.Resolution{Allow: &yes, Always: true})
	waitPending(t, h, 0)
	got := ask(t, h, alwaysIx("ix_5", "conv", "tool:Bash"), nil)
	if got.Allow == nil || !*got.Allow || !got.Always {
		t.Fatalf("kept grant answered %+v", got)
	}
	if n := len(h.pendingInteractions()); n != 0 {
		t.Fatalf("%d cards shown for a granted tool", n)
	}
	// Another tool, another conversation, or a card naming one more key: asked.
	for _, ix := range []proto.Interaction{
		alwaysIx("ix_6", "conv", "tool:Edit"),
		alwaysIx("ix_7", "other", "tool:Bash"),
		alwaysIx("ix_8", "conv", "tool:Bash", "lark:POST /im/v1/messages"),
	} {
		if _, ok := h.alwaysAllowed(context.Background(), ix); ok {
			t.Fatalf("%s allowed without a grant", ix.ID)
		}
	}
}

// A card of the same conversation waiting when another one is answered with 本会话总是允许 is answered with it.
func TestAssistantAlwaysAnswersWaitingCards(t *testing.T) {
	h := newTestHub(t)
	h.store = newAlwaysStore()
	waiting := make(chan proto.Resolution, 1)
	go func() {
		r, _ := h.nodeInteraction(context.Background(), alwaysIx("ix_b", "conv", "tool:Bash"))
		waiting <- r
	}()
	other := make(chan proto.Resolution, 1)
	go func() {
		r, _ := h.nodeInteraction(context.Background(), alwaysIx("ix_c", "conv", "tool:Edit"))
		other <- r
	}()
	waitPending(t, h, 2)
	yes := true
	ask(t, h, alwaysIx("ix_a", "conv", "tool:Bash"), &proto.Resolution{Allow: &yes, Always: true})
	select {
	case r := <-waiting:
		if r.Allow == nil || !*r.Allow {
			t.Fatalf("waiting card got %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting card for the same tool still waits")
	}
	waitPending(t, h, 1) // the other tool's card still asks
	if p := h.pendingInteractions(); p[0].ID != "ix_c" {
		t.Fatalf("pending %+v", p)
	}
}
