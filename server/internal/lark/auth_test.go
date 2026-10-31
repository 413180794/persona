package lark

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const sessionExpired = `{"error":{"code":"AUTH_SESSION_EXPIRED","message":"not logged in to Feishu"}}`

// stubContainer replaces docker inspect for the test; it counts lookups and serves tokens in order.
func stubContainer(t *testing.T, tokens ...string) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	old := containerEnv
	containerEnv = func(ctx context.Context, container string) ([]byte, error) {
		i := int(n.Add(1)) - 1
		if i >= len(tokens) {
			i = len(tokens) - 1
		}
		return []byte("PATH=/bin\nlark-connector_AGENT_TOKEN=" + tokens[i] + "\n"), nil
	}
	t.Cleanup(func() { containerEnv = old })
	return &n
}

func TestConfigTokenSurvivesSessionExpired(t *testing.T) {
	looked := stubContainer(t, "from-docker")
	var loggedIn atomic.Bool
	var seen sync.Map
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Store(r.Header.Get("Authorization"), true)
		if r.Header.Get("Authorization") != "Bearer cfg" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"code":"UNAUTHORIZED","message":"bad token"}}`))
			return
		}
		if !loggedIn.Load() {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(sessionExpired))
			return
		}
		_, _ = w.Write([]byte(`{"id":"u1"}`))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "cfg", Container: "some-container"})

	for i := 0; i < 3; i++ {
		_, err := c.Profile(context.Background(), "u1", "")
		if !errors.Is(err, ErrNotLoggedIn) || !strings.Contains(err.Error(), "未登录飞书") || !strings.Contains(err.Error(), "AUTH_SESSION_EXPIRED") {
			t.Fatalf("call %d: err = %v, want ErrNotLoggedIn", i, err)
		}
	}
	// Media and raw proxies take the same path.
	rec := httptest.NewRecorder()
	if err := c.Media(context.Background(), rec, "/media/k", nil, http.Header{}); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("media err = %v", err)
	}
	if looked.Load() != 0 {
		t.Fatalf("container looked up %d times; the configured token must be kept", looked.Load())
	}
	if _, ok := seen.Load("Bearer from-docker"); ok {
		t.Fatal("fell back to the container token")
	}

	// The user logs in to Feishu again: the next call works without a restart.
	loggedIn.Store(true)
	raw, err := c.Profile(context.Background(), "u1", "")
	if err != nil || !strings.Contains(string(raw), "u1") {
		t.Fatalf("after login: %s %v", raw, err)
	}

	// Even a genuine bad-token 401 never drops the configured token.
	c.unauthorized([]byte(`{"error":{"code":"UNAUTHORIZED","message":"bad token"}}`))
	if tok, err := c.agentToken(context.Background()); tok != "cfg" || err != nil {
		t.Fatalf("token after bad-token 401 = %q %v", tok, err)
	}
}

func TestBadTokenFallsBackToContainer(t *testing.T) {
	looked := stubContainer(t, "old", "new")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer new" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"code":"UNAUTHORIZED","message":"invalid agent token"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"u1"}`))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/agent/v1", Container: "some-container"})

	_, err := c.Profile(context.Background(), "u1", "")
	if err == nil || errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("first call err = %v, want a bad-token error", err)
	}
	// The container was recreated with a new token: it is read again.
	if _, err := c.Profile(context.Background(), "u1", ""); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if looked.Load() != 2 {
		t.Fatalf("container looked up %d times, want 2", looked.Load())
	}
}

func TestSessionExpiredKeepsContainerToken(t *testing.T) {
	looked := stubContainer(t, "tok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(sessionExpired))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/agent/v1", Container: "some-container"})
	for i := 0; i < 3; i++ {
		if _, err := c.Profile(context.Background(), "u1", ""); !errors.Is(err, ErrNotLoggedIn) {
			t.Fatalf("err = %v", err)
		}
	}
	if looked.Load() != 1 {
		t.Fatalf("container looked up %d times, want 1 (a session error is not a bad token)", looked.Load())
	}
}

func TestNoTokenNoContainer(t *testing.T) {
	looked := stubContainer(t, "tok")
	c := New(Config{BaseURL: "http://127.0.0.1:1/agent/v1"})
	if _, err := c.Profile(context.Background(), "u1", ""); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("err = %v", err)
	}
	if looked.Load() != 0 {
		t.Fatal("looked up a container nobody configured")
	}
}

func TestEventsRecoverAfterLogin(t *testing.T) {
	oldB, oldMax := eventsBackoff, eventsMaxBackoff
	eventsBackoff, eventsMaxBackoff = 10*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { eventsBackoff, eventsMaxBackoff = oldB, oldMax })

	var loggedIn atomic.Bool
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cfg" {
			t.Errorf("events with token %q", r.Header.Get("Authorization"))
		}
		if attempts.Add(1); !loggedIn.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(401)
			_, _ = w.Write([]byte(sessionExpired))
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"conversation.upserted","feedId":"f1"}`))
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "cfg"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var logs []string
	got := make(chan Event, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Events(ctx, func(ev Event) { got <- ev }, func(f string, a ...any) {
			mu.Lock()
			logs = append(logs, fmt.Sprintf(f, a...))
			mu.Unlock()
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for attempts.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	loggedIn.Store(true)
	select {
	case ev := <-got:
		if len(ev.FeedIDs) != 1 || ev.FeedIDs[0] != "f1" {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("events never recovered after login")
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	failures := 0
	for _, l := range logs {
		if strings.Contains(l, "retrying") {
			if !strings.Contains(l, "未登录飞书") {
				t.Errorf("log does not name the session error: %q", l)
			}
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("logged the repeated session error %d times, want once: %q", failures, logs)
	}
}
