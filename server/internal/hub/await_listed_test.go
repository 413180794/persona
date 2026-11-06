package hub

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A session the manager lists a little after it started is returned as listed, with its State; one never listed
// gives up after the wait (Hub.listedWait), not later.
func TestAwaitListedReturnsTheSessionOnceListed(t *testing.T) {
	h := newTestHub(t)
	h.listedWait = time.Second
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
	h.machines, h.byID["box"] = []*Machine{m}, m

	go func() {
		time.Sleep(100 * time.Millisecond)
		m.mu.Lock()
		m.sessions = append(m.sessions, proto.Session{SessionID: "late", State: "working"})
		m.mu.Unlock()
	}()
	s, ok := h.awaitListed(context.Background(), m, "late")
	if !ok || s.SessionID != "late" || s.State != "working" {
		t.Fatalf("awaitListed(late) = %+v, %v; want the listed session with State working", s, ok)
	}

	h.listedWait = 150 * time.Millisecond
	start := time.Now()
	if s, ok := h.awaitListed(context.Background(), m, "absent"); ok {
		t.Fatalf("awaitListed(absent) = %+v, true; want false", s)
	}
	if took := time.Since(start); took < h.listedWait || took > h.listedWait+time.Second {
		t.Fatalf("awaitListed(absent) took %v; want about %v", took, h.listedWait)
	}
}
