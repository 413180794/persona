package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestClaimedQuestionCancellationSurvivesDisconnectAndExpiry(t *testing.T) {
	ft := newFleetTest(t)
	st := &slowEndMem{fleetMem: ft.st, entered: make(chan struct{}), release: make(chan struct{})}
	s := New(Options{Hub: ft.f, Store: func() Store { return st }, Logf: t.Logf, Devices: ft.d})
	ix := resumable("cancel-race")
	ix.Kind, ix.CanCancel = proto.IxQuestion, true
	ix.ExpiresAt = time.Now().Add(100 * time.Millisecond)
	s.record(ix)
	p := &pending{ix: ix, done: make(chan proto.Resolution, 1)}
	s.fleet.mu.Lock()
	s.ixMapLocked()[ix.ID] = p
	s.fleet.mu.Unlock()
	result := make(chan error, 1)
	go func() { result <- s.resolve(ix.ID, proto.Resolution{Cancel: true}) }()
	<-st.entered // resolve claimed the card, but its outcome has not been persisted
	answers := make(chan proto.Resolution, 3)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, c := range []context.Context{ctx, context.Background(), context.Background()} {
		go func(c context.Context) { r, _ := s.await(c, p); answers <- r }(c)
	}
	// Both a disconnected waiter and overlapping waiters whose deadline expires
	// must leave the claimed cancellation as the only history write.
	time.Sleep(150 * time.Millisecond)
	writes := st.written()
	close(st.release)
	if len(writes) != 1 || writes[0] != "cancelled" {
		t.Fatalf("competing outcome writes: %v", writes)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		select {
		case r := <-answers:
			if !r.IsQuestionCancel() {
				t.Fatalf("waiter received %+v", r)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("overlapping waiter remained blocked")
		}
	}
	status, r, ended := s.outcome(ix.ID)
	if status != "cancelled" || !ended || !r.IsQuestionCancel() {
		t.Fatalf("replay: %s %+v %v", status, r, ended)
	}
}
