package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
)

// failingMutes is a store whose reads fail while fail is set.
type failingMutes struct {
	settingsStore
	fail bool
}

func (s *failingMutes) Setting(ctx context.Context, key string, out any) (bool, error) {
	if s.fail {
		return false, errors.New("database hiccup")
	}
	return s.settingsStore.Setting(ctx, key, out)
}

// Mutes the hub could not read are not handed to the managers (an empty set would unmute every agent there); once
// read they go, and stay the ones read.
func TestMutesNotSentWhenUnreadable(t *testing.T) {
	h := newTestHub(t)
	st := &failingMutes{settingsStore: settingsStore{vals: map[string][]byte{}}, fail: true}
	h.store = st
	if err := st.PutSetting(context.Background(), "mutes", Pins{Spaces: []string{"m/app"}, Sessions: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.mutesEvent(); ok {
		t.Fatal("unreadable mutes would be sent")
	}
	st.fail = false
	m, ok := h.mutesEvent()
	if !ok || m.Mutes == nil || !slices.Equal(m.Mutes.Spaces, []string{"m/app"}) {
		t.Fatalf("mutes once readable: %+v %v", m.Mutes, ok)
	}
	st.fail = true // kept in memory once read
	if m, ok := h.mutesEvent(); !ok || !slices.Equal(m.Mutes.Spaces, []string{"m/app"}) {
		t.Fatalf("mutes read before: %+v %v", m.Mutes, ok)
	}
}

// mutesManager is a manager on bus.Client that records the hello's mutes and every fleet.mutes; during runs inside its
// OnHello (after the hub built the answer, before the manager took it).
type mutesManager struct {
	c     *bus.Client
	hello *assistantwire.Mutes
	mutes chan string
}

func connectMutesManager(t *testing.T, h *Hub, srv *httptest.Server, id string, during func()) *mutesManager {
	t.Helper()
	m := &mutesManager{mutes: make(chan string, 16)}
	m.c = &bus.Client{Hub: srv.URL, Token: busToken(t, h, id, id), HelloMethod: busHello,
		HelloParams: func() any { return map[string]any{"id": id} },
		OnHello: func(res json.RawMessage) error {
			var reply busHelloReply
			if err := json.Unmarshal(res, &reply); err != nil {
				return err
			}
			m.hello = reply.Mutes
			if during != nil {
				during()
			}
			return nil
		},
		OnRequest: echoService,
		OnEvent: func(event string, params json.RawMessage) {
			if event == assistantwire.EventMutes {
				m.mutes <- string(params)
			}
		},
		Backoff: 20 * time.Millisecond, MaxBackoff: 300 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = m.c.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	wctx, wcancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer wcancel()
	if err := m.c.Wait(wctx); err != nil {
		t.Fatal(err)
	}
	return m
}

// next is the next fleet.mutes within d.
func (m *mutesManager) next(d time.Duration) (string, bool) {
	select {
	case s := <-m.mutes:
		return s, true
	case <-time.After(d):
		return "", false
	}
}

// say is the manager's first word after its handshake.
func (m *mutesManager) say(t *testing.T) {
	t.Helper()
	if err := m.c.Peer().Notify("test.event", map[string]any{}); err != nil {
		t.Fatal(err)
	}
}

// putMutes is PUT /api/v1/mutes.
func putMutes(t *testing.T, h *Hub, store settingsPutter, spaces ...string) {
	t.Helper()
	p := Pins{Spaces: spaces, Sessions: []string{}}
	if err := store.PutSetting(context.Background(), "mutes", p); err != nil {
		t.Error(err)
		return
	}
	h.mutesMu.Lock()
	h.mutes = &p
	h.mutesMu.Unlock()
	h.mutesChanged()
}

type settingsPutter interface {
	PutSetting(ctx context.Context, key string, v any) error
}

// A change of the mutes while a manager's hello answer is on its way: the answer (built before) and the change's
// fleet.mutes race at the manager, which takes the answer on another goroutine than its events. Once it says its
// first word (it took the answer by then), the mutes go once more, to every manager (one from before the answer
// carried them has them from here).
func TestMutesGoAgainAfterTheHandshake(t *testing.T) {
	h, srv, store := newBusHub(t)
	t.Cleanup(func() { _, _ = store.TakeSetting(context.Background(), "mutes", &Pins{}) })
	putMutes(t, h, store)

	m := connectMutesManager(t, h, srv, "assistant", func() { putMutes(t, h, store, "m/app") })
	if m.hello == nil || len(m.hello.Spaces) != 0 {
		t.Fatalf("the hello's mutes: %+v", m.hello)
	}
	if s, ok := m.next(10 * time.Second); !ok || !strings.Contains(s, "m/app") {
		t.Fatalf("the change's fleet.mutes: %q %v", s, ok)
	}
	m.say(t)
	if s, ok := m.next(10 * time.Second); !ok || !strings.Contains(s, "m/app") {
		t.Fatalf("the mutes after its first word: %q %v", s, ok)
	}
	m.say(t)
	if s, ok := m.next(300 * time.Millisecond); ok {
		t.Fatalf("the mutes again after a second word: %q", s)
	}

	// Another manager: the mutes in its hello's answer, and once after its first word, of its own machines only (it
	// has none: m is the carrier's).
	d := connectMutesManager(t, h, srv, "desk", nil)
	if d.hello == nil || len(d.hello.Spaces) != 0 || d.hello.Spaces == nil {
		t.Fatalf("a non-default manager's hello: %+v", d.hello)
	}
	d.say(t)
	if s, ok := d.next(10 * time.Second); !ok || strings.Contains(s, "m/app") {
		t.Fatalf("a non-default manager after its first word: %q %v", s, ok)
	}
}

// flakyMutes is the hub's store with the mutes unreadable while fail is set.
type flakyMutes struct {
	Store
	fail atomic.Bool
}

func (s *flakyMutes) Setting(ctx context.Context, key string, out any) (bool, error) {
	if key == "mutes" && s.fail.Load() {
		return false, errors.New("database hiccup")
	}
	return s.Store.Setting(ctx, key, out)
}

// Mutes unreadable at the handshake (and at the manager's first word) go once they can be read.
func TestMutesRetriedWhenUnreadableAtTheHandshake(t *testing.T) {
	h, srv, store := newBusHub(t)
	t.Cleanup(func() { _, _ = store.TakeSetting(context.Background(), "mutes", &Pins{}) })
	if err := store.PutSetting(context.Background(), "mutes", Pins{Spaces: []string{"m/app"}, Sessions: []string{}}); err != nil {
		t.Fatal(err)
	}
	every, tries := mutesRetryEvery, mutesRetries
	mutesRetryEvery, mutesRetries = 50*time.Millisecond, 1200 // a minute, as slow as a runner gets
	t.Cleanup(func() { mutesRetryEvery, mutesRetries = every, tries })
	h.bus.idents = store
	flaky := &flakyMutes{Store: store}
	flaky.fail.Store(true)
	h.store = flaky

	m := connectMutesManager(t, h, srv, "assistant", nil)
	if m.hello != nil {
		t.Fatalf("unreadable mutes in the hello: %+v", m.hello)
	}
	m.say(t)
	if s, ok := m.next(300 * time.Millisecond); ok {
		t.Fatalf("unreadable mutes sent: %q", s)
	}
	flaky.fail.Store(false)
	if s, ok := m.next(10 * time.Second); !ok || !strings.Contains(s, "m/app") {
		t.Fatalf("the mutes once readable: %q %v", s, ok)
	}
}
