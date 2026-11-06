package assistant

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

// trustStore is memStore with what the devices say of the hub's pending computers: the computers in pending, or err
// for every read (the hub has not handed them over).
type trustStore struct {
	*memStore
	mu      sync.Mutex
	pending []string
	err     error
	reads   int
}

func (s *trustStore) PendingMachineIDs(context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if s.err != nil {
		return nil, s.err
	}
	return slices.Clone(s.pending), nil
}

// newServiceTrust is newServiceOn with st as what says which computers are pending (nil: nothing does).
func newServiceTrust(t *testing.T, f *fakeHub, st Store, trust MachineTrust) *Service {
	t.Helper()
	return withSettings(t, f, New(Options{Hub: f, Store: func() Store { return st }, Trust: trust, Logf: testLogf(t)}))
}

func (s *trustStore) set(pending []string, err error) {
	s.mu.Lock()
	s.pending, s.err = pending, err
	s.mu.Unlock()
}

// With no pending computer (nothing says which are, or none is) every online machine gets the managed launchers, as
// before.
func TestNoPendingComputerChangesNothing(t *testing.T) {
	f := newFakeHub().withMachine()
	f.sessions["mac"] = nil
	f.devices = []Device{{ID: "server", Online: true, Reachable: true}, {ID: "mac", Online: true, Reachable: true}}
	for name, trust := range map[string]MachineTrust{"no source": nil, "none pending": &trustStore{memStore: &memStore{}}} {
		t.Run(name, func(t *testing.T) {
			s := newServiceTrust(t, f, &memStore{}, trust)
			f.mu.Lock()
			clear(f.managed)
			f.mu.Unlock()
			s.pushManaged(context.Background(), "")
			f.mu.Lock()
			defer f.mu.Unlock()
			if _, ok := f.managed["server"]; !ok {
				t.Fatal("server got nothing")
			}
			if _, ok := f.managed["mac"]; !ok {
				t.Fatal("mac got nothing")
			}
			if s.machinePending(context.Background(), "mac") {
				t.Fatal("mac counted as pending")
			}
		})
	}
}

// A computer waiting to be confirmed gets no keys; once the hello lists it (confirmed) it gets them.
func TestPendingComputerGetsKeysOnlyOnceConfirmed(t *testing.T) {
	f := newFakeHub().withMachine()
	f.sessions["mac"] = nil
	f.devices = []Device{{ID: "server", Online: true, Reachable: true}, {ID: "mac", Online: true, Reachable: true}}
	st := &trustStore{memStore: &memStore{}, pending: []string{"mac"}}
	s := newServiceTrust(t, f, st, st)
	ctx := context.Background()
	f.mu.Lock()
	clear(f.managed)
	f.mu.Unlock()
	s.pushManaged(ctx, "")
	f.mu.Lock()
	_, server := f.managed["server"]
	_, mac := f.managed["mac"]
	f.mu.Unlock()
	if !server || mac {
		t.Fatalf("pushed to server %v, to the pending mac %v", server, mac)
	}
	if !s.machinePending(ctx, "mac") || s.machinePending(ctx, "server") {
		t.Fatal("pending is the mac alone")
	}

	st.set(nil, nil) // confirmed: the hub's hello lists it now
	s.trustChanged(ctx, []string{"server"}, []string{"server", "mac"})
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		_, mac = f.managed["mac"]
		f.mu.Unlock()
		if mac {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the confirmed mac never got the managed launchers")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A store that never answered counts every computer as pending; after one good read a failure keeps the last.
func TestPendingComputersWhenTheStoreFails(t *testing.T) {
	f := newFakeHub().withMachine()
	st := &trustStore{memStore: &memStore{}, err: errors.New("down")}
	s := newServiceTrust(t, f, st, st)
	ctx := context.Background()
	if err := s.machineTrust(ctx, "server"); !errors.Is(err, errTrustUnknown) {
		t.Fatalf("never read: %v, want the store's own reason", err)
	}
	// A failed read is not tried again at once: the node requests that follow use it.
	for range 5 {
		_ = s.machineTrust(ctx, "server")
	}
	st.mu.Lock()
	reads := st.reads
	st.mu.Unlock()
	if reads != 1 {
		t.Fatalf("%d reads right after a failed one", reads)
	}
	st.set([]string{"mac"}, nil)
	s.trustChanged(ctx, nil, nil) // read again now
	if err := s.machineTrust(ctx, "mac"); !errors.Is(err, errMachinePending) || s.machinePending(ctx, "server") {
		t.Fatalf("after a good read: %v", err)
	}
	st.set(nil, errors.New("down again"))
	s.trustChanged(ctx, nil, nil)
	if !s.machinePending(ctx, "mac") || s.machinePending(ctx, "server") {
		t.Fatal("a failed read lost the last one")
	}
}
