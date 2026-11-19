//go:build linux || darwin

package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/nwverse/persona/server/internal/admission"
	"github.com/nwverse/persona/server/internal/assistantwire"
)

func TestAdmissionLiveKeepAliveAndEstablishedWebSocket(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "admission")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	g, err := admission.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	control := func(g *admission.Gate, action, attempt string) admission.State {
		t.Helper()
		state, err := g.Control(action, attempt)
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	control(g, "init", "")
	const attempt = "transport-attempt"
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()

	h := newTestHub(t)
	h.admission = g
	store := newMeshStore()
	h.store = store
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/ws", h.serveWS)
	h.pinRoutes(mux)
	srv := httptest.NewServer(h.admitHTTP(mux))
	put := func(srv *httptest.Server, pin string, expected int, retry, reuse bool) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, srv.URL+"/api/v1/pins", strings.NewReader(`{"spaces":["`+pin+`"]}`))
		if err != nil {
			t.Fatal(err)
		}
		trace := &httptrace.ClientTrace{GotConn: func(got httptrace.GotConnInfo) {
			if reuse && !got.Reused {
				t.Error("closed HTTP request did not use the already-established connection")
			}
		}}
		res, err := client.Do(req.WithContext(httptrace.WithClientTrace(req.Context(), trace)))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if _, err := io.Copy(io.Discard, res.Body); err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != expected || (res.Header.Get("Retry-After") != "") != retry {
			t.Fatalf("PUT pins status %d, retry header %q", res.StatusCode, res.Header.Get("Retry-After"))
		}
	}
	ws, _, err := websocket.Dial(ctx, srv.URL+"/api/v1/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	defer ws.CloseNow()

	read := func(want string) map[string]any {
		t.Helper()
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			if result["type"] == want {
				return result
			}
		}
	}
	send := func(body string) {
		t.Helper()
		if err := ws.Write(ctx, websocket.MessageText, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	read("hello")
	put(srv, "accepted", http.StatusOK, false, false)
	control(g, "close", attempt)
	put(srv, "blocked", http.StatusServiceUnavailable, true, true)
	send(`{"type":"terminal_input","text":"refuse after close"}`)
	if result := read("error"); result["code"] != "maintenance" {
		t.Fatalf("write frame error: %+v", result)
	}
	send(`{"type":"ping","id":"still-open"}`)
	if result := read("pong"); result["id"] != "still-open" {
		t.Fatalf("ping failed on the original socket: %+v", result)
	}
	restarted, err := admission.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A replacement listener over the same durable gate still refuses work.
	other := newTestHub(t)
	other.admission, other.store = restarted, store
	next := http.NewServeMux()
	next.HandleFunc("/api/v1/ws", other.serveWS)
	other.pinRoutes(next)
	second := httptest.NewServer(other.admitHTTP(next))
	defer second.Close()
	put(second, "blocked-restart", http.StatusServiceUnavailable, true, false)
	_, rejected, err := websocket.Dial(ctx, second.URL+"/api/v1/ws", nil)
	if err == nil || rejected == nil || rejected.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("replacement listener accepted a new socket: %v %v", rejected, err)
	}
	if state := control(restarted, "check", attempt); state.Phase != "drained" {
		t.Fatalf("idle socket falsely counted as admitted work: %+v", state)
	}
	control(restarted, "open", attempt)
	var retained Pins
	if _, err := store.Setting(ctx, "pins", &retained); err != nil || len(retained.Spaces) != 1 || retained.Spaces[0] != "accepted" {
		t.Fatalf("closed PUT changed the previously accepted pin: %+v %v", retained, err)
	}
	put(second, "reopened", http.StatusOK, false, true)
}

type failedHandOver struct {
	*meshStore
	writes int
}

func (s *failedHandOver) HandOverFirst(context.Context, string, string, time.Duration) (bool, error) {
	s.writes++
	return false, errors.New("lost handover database response")
}

func (s *failedHandOver) HandOverForget(context.Context, string, string) error { return nil }

func TestAdmissionOutboxHandoffStaysPendingAfterStoreFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "admission")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	g, err := admission.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	const attempt = "outbox-hub-attempt"
	if _, err := g.Control("init", ""); err != nil {
		t.Fatal(err)
	}
	h := newTestHub(t)
	store := &failedHandOver{meshStore: newMeshStore()}
	h.admission, h.store = g, store
	call := func(p assistantwire.Outbox) (any, error) {
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		return busMethods[assistantwire.MethodOutbox](h, t.Context(), nil, raw)
	}
	batch := assistantwire.Outbox{Items: []assistantwire.OutboxItem{{ID: "known-operation"}}}
	key, ids := assistantwire.OutboxBatchKey(batch.Items)
	finish, err := g.AdmitExclusivePayload("manager-outbox", key, ids)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Control("close", attempt); err != nil {
		t.Fatal(err)
	}
	other := assistantwire.Outbox{Items: []assistantwire.OutboxItem{{ID: "unrelated-operation"}}}
	if _, err := call(other); err == nil || store.writes != 0 {
		t.Fatalf("unrelated work crossed the fence: writes=%d error=%v", store.writes, err)
	}
	if _, err := call(batch); err == nil || store.writes != 1 {
		t.Fatalf("durable store failure appeared successful: writes=%d error=%v", store.writes, err)
	}
	if _, err := call(batch); err == nil || store.writes != 1 {
		t.Fatalf("unsettled batch was repeated: writes=%d error=%v", store.writes, err)
	}
	finish()
	if state, err := g.Control("check", attempt); err != nil || state.Phase != "draining" || state.Active["hub-outbox"] != 1 {
		t.Fatalf("unsettled Hub continuation was lost: %+v %v", state, err)
	}
}
