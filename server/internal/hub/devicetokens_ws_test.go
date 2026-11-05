package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/db"
)

type blockedDeviceSessionStore struct {
	*meshStore
	entered chan struct{}
	release chan struct{}
}

func (s *blockedDeviceSessionStore) InsertDeviceSession(ctx context.Context, _ db.DeviceSession) (int64, error) {
	close(s.entered)
	select {
	case <-s.release:
		return 1, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func TestDeviceRevokedDuringWebSocketRegistration(t *testing.T) {
	h := newTestHub(t)
	const token = "jd_registration_race"
	const deviceID = "dev_registration_race"
	store := &blockedDeviceSessionStore{
		meshStore: newMeshStore(),
		entered:   make(chan struct{}),
		release:   make(chan struct{}),
	}
	store.tokens[hashToken(token)] = db.DeviceToken{ID: deviceID, Role: roleFull}
	h.store = store

	handlerDone := make(chan struct{})
	mux := h.routes()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		mux.ServeHTTP(w, r)
	}))
	defer server.Close()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(store.release) }) }
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, server.URL+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	select {
	case <-store.entered:
	case <-ctx.Done():
		t.Fatal("WebSocket did not reach device session registration")
	}

	// Authentication and upgrade have completed, but the client is not yet in
	// h.clients, so revocation's sweep cannot find this connection.
	h.mu.Lock()
	clients, connections := len(h.clients), len(h.wsConnections)
	h.mu.Unlock()
	if clients != 0 || connections != 1 {
		t.Fatalf("before revocation: clients=%d, connections=%d; want 0, 1", clients, connections)
	}
	h.revoked(deviceID)
	release()

	if _, data, err := conn.Read(ctx); err == nil {
		t.Fatalf("revoked connection was registered and received %s", data)
	} else if ctx.Err() != nil {
		t.Fatalf("revoked connection was not closed promptly: %v", err)
	}
	select {
	case <-handlerDone:
	case <-ctx.Done():
		t.Fatal("revoked WebSocket handler did not finish")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.clients) != 0 || len(h.wsConnections) != 0 {
		t.Fatalf("revoked connection remains registered: clients=%d, connections=%d", len(h.clients), len(h.wsConnections))
	}
}
