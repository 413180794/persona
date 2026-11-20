//go:build linux || darwin

package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/admission"
	"github.com/nwverse/persona/server/internal/bus"
)

func TestAdmissionExistingSocketRefusesNewFrameAndKeepsCompletions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gate")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	gate, err := admission.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range [][2]string{{"init", ""}, {"close", "socket-attempt"}} {
		if _, err := gate.Control(command[0], command[1]); err != nil {
			t.Fatal(err)
		}
	}
	h := &Hub{admission: gate}
	c := &client{out: make(chan []byte, 4)}
	h.onClientMessage(context.Background(), c, []byte(`{"type":"terminal_input","text":"new work"}`))
	select {
	case raw := <-c.out:
		var response struct{ Code string }
		if err := json.Unmarshal(raw, &response); err != nil || response.Code != "maintenance" {
			t.Fatalf("expected an explicit maintenance error, got %s", raw)
		}
	default:
		t.Fatal("the existing app connection did not get a retry response")
	}
	if _, ok := h.admitClientFrame(c, "tool_result"); !ok {
		t.Fatal("a completion for already admitted work could not enter")
	}
	for _, tc := range []struct {
		path string
		code int
	}{{"/", 503}, {"/api/v1/health", 204}, {bus.Path, 204}} {
		r := httptest.NewRequest(http.MethodGet, tc.path, nil)
		w := httptest.NewRecorder()
		h.admitHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s returned %d, expected %d", tc.path, w.Code, tc.code)
		}
	}
	w := httptest.NewRecorder()
	h.serveWS(w, httptest.NewRequest(http.MethodGet, "/api/v1/ws", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("closed socket handshake returned %d", w.Code)
	}
}
