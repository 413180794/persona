package hub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
)

// Tests never start the assistant's process (the test binary would run itself again).
// A test without an assistant does not wait for its tools either.
func init() {
	assistantCommand = func(context.Context) (*exec.Cmd, error) { return nil, errors.New("no assistant process in tests") }
	assistantToolsWait = 0
}

// testAssistant is the assistant (internal/assistant) on the hub's bus, run in the test's process the way `personad
// assistant` runs it: its own Service, the hub's store as its own, a real WebSocket connection to the hub.
type testAssistant struct {
	svc   *assistant.Service
	bh    *assistant.BusHub
	stop  context.CancelFunc
	done  chan struct{}
	token string // its bus token (the manager's relay dials with it too)
}

// connectAssistant connects the assistant to h, on a server with h's bus endpoint alone (identities in memory when
// the hub has no database), and waits until the hub has its tools and its snapshot.
func connectAssistant(t *testing.T, h *Hub) *testAssistant {
	t.Helper()
	if h.busStore() == nil {
		h.bus.idents = &memIdents{}
	}
	mux := http.NewServeMux()
	h.busRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return dialAssistant(t, h, srv.URL, false)
}

// dialAssistant runs the assistant against the hub at url with a new token, and waits as connectAssistant does.
// schedule runs its schedule loop too (as `personad assistant` does; the other tests run due tasks themselves).
func dialAssistant(t *testing.T, h *Hub, url string, schedule bool, opts ...func(*assistant.BusOptions)) *testAssistant {
	t.Helper()
	tok, err := h.createBusToken(context.Background(), assistantwire.AssistantID, "助手")
	if err != nil {
		t.Fatal(err)
	}
	var logMu sync.Mutex
	over := false // t.Logf panics once the test is over
	logf := func(format string, args ...any) {
		logMu.Lock()
		defer logMu.Unlock()
		if !over {
			t.Logf(format, args...)
		}
	}
	o := assistant.BusOptions{Hub: url, Token: tok, Backoff: 50 * time.Millisecond, Version: "test", Deploy: "test", Logf: logf}
	for _, f := range opts {
		f(&o)
	}
	bh := assistant.NewBusHub(o)
	svc := assistant.New(assistant.Options{Hub: bh, Logf: logf, Store: func() assistant.Store {
		s, _ := h.store.(assistant.Store)
		return s
	}, KB: testKBOf(h)}) // the knowledge base a test gave it (kb_test.go)
	ctx, cancel := context.WithCancel(context.Background())
	a := &testAssistant{svc: svc, bh: bh, stop: cancel, done: make(chan struct{}), token: tok}
	testAssistants.Store(h, a)
	go func() {
		defer close(a.done)
		if schedule {
			_ = bh.Run(ctx, svc)
		} else {
			_ = bh.Serve(ctx, svc)
		}
	}()
	t.Cleanup(func() {
		logMu.Lock()
		over = true
		logMu.Unlock()
		testAssistants.CompareAndDelete(h, a)
		cancel()
		<-a.done
		for _, c := range h.stopBus() {
			_ = c.ws.CloseNow()
		}
	})
	a.ready(t, h)
	return a
}

// ready waits until the assistant is connected, the hub has its tools and the snapshot it reports on connecting.
func (a *testAssistant) ready(t *testing.T, h *Hub) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.bh.Wait(ctx); err != nil {
		t.Fatalf("the assistant did not connect: %v", err)
	}
	eventually(t, 10*time.Second, "the assistant's tools and snapshot", func() bool {
		st := &h.assistant
		st.toolsMu.Lock()
		tools := st.tools != nil
		st.toolsMu.Unlock()
		st.mu.RLock()
		got := st.got
		st.mu.RUnlock()
		return tools && (got || h.store == nil)
	})
}

// restartAssistant stops h's assistant and connects a new one (it reads its store again), waiting for its snapshot.
func restartAssistant(t *testing.T, h *Hub) *testAssistant {
	t.Helper()
	old := assistantOf(t, h)
	old.stop()
	<-old.done
	st := &h.assistant
	st.mu.Lock()
	st.got = false
	st.mu.Unlock()
	return connectAssistant(t, h)
}

// helloChanged tells h's assistant its hello answer now (manager.updated), for a test that changed what it says
// behind the hub's back (its store, its machines), and waits until it took it.
func helloChanged(t *testing.T, h *Hub) {
	t.Helper()
	hello, _ := assistantLink{h}.Hello(context.Background())
	if err := h.notifyBus(assistantwire.AssistantID, assistantwire.EventUpdated, hello); err != nil {
		t.Fatal(err)
	}
	settleAssistant(h)
}

// testAssistants are the connected ones, by hub.
var testAssistants sync.Map // *Hub → *testAssistant

// assistantOf is h's connected assistant (connectAssistant).
func assistantOf(t *testing.T, h *Hub) *testAssistant {
	t.Helper()
	a, ok := testAssistants.Load(h)
	if !ok {
		t.Fatal("no assistant connected to this hub")
	}
	return a.(*testAssistant)
}

// settleAssistant waits until h's assistant took the events the hub sent it so far: a call to it is read after them
// (the same connection, in order), and then its event worker is waited for. Nothing when none is connected.
func settleAssistant(h *Hub) {
	v, ok := testAssistants.Load(h)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = h.callBus(ctx, assistantwire.AssistantID, assistantwire.MethodTools, nil, nil)
	v.(*testAssistant).bh.Settle()
}

// memIdents keeps the bus's identities in memory (hub tests without Postgres).
type memIdents struct {
	mu     sync.Mutex
	byID   map[string]db.BusIdentity
	hashes map[string]string // token hash -> id
}

func (m *memIdents) SaveBusIdentity(_ context.Context, b db.BusIdentity, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byID == nil {
		m.byID, m.hashes = map[string]db.BusIdentity{}, map[string]string{}
	}
	for h, id := range m.hashes {
		if id == b.ID {
			delete(m.hashes, h)
		}
	}
	b.CreatedAt = time.Now()
	m.byID[b.ID], m.hashes[hash] = b, b.ID
	return nil
}

func (m *memIdents) BusIdentityByHash(_ context.Context, hash string) (*db.BusIdentity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.hashes[hash]
	if !ok {
		return nil, nil
	}
	b := m.byID[id]
	return &b, nil
}

func (m *memIdents) BusIdentities(context.Context) ([]db.BusIdentity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []db.BusIdentity
	for _, b := range m.byID {
		out = append(out, b)
	}
	return out, nil
}

func (m *memIdents) DeleteBusIdentity(_ context.Context, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.byID[id]
	delete(m.byID, id)
	for h, v := range m.hashes {
		if v == id {
			delete(m.hashes, h)
		}
	}
	return ok, nil
}
