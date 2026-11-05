package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func termClient(h *Hub, role string) *client {
	return &client{hub: h, out: make(chan []byte, 64), subs: map[[2]string]bool{}, role: role}
}

func say(h *Hub, c *client, v map[string]any) {
	b, _ := json.Marshal(v)
	h.onClientMessage(context.Background(), c, b)
}

// firstTerminalError is the error of the first terminal message a client was sent ("" when none).
func firstTerminalError(c *client) string {
	for {
		select {
		case b := <-c.out:
			var m struct {
				Type  string `json:"type"`
				Error string `json:"error"`
			}
			_ = json.Unmarshal(b, &m)
			if m.Type == "terminal" && m.Error != "" {
				return m.Error
			}
		default:
			return ""
		}
	}
}

// A pane is a shell on the computer: only a full device opens its terminal, for watching as well as driving. An
// answer-only device (the watch) still watches the terminal of an agent session as before.
func TestPaneTerminalNeedsAFullDevice(t *testing.T) {
	tn := newTermNode(t)
	h := tn.hub
	answer, full := termClient(h, roleAnswer), termClient(h, roleFull)

	say(h, answer, map[string]any{"type": "terminal_subscribe", "machine_id": "m1", "session_id": "pane:w1:p2"})
	if got := firstTerminalError(answer); !strings.Contains(got, "forbidden") {
		t.Errorf("an answer-only device was told %q", got)
	}
	h.terms.mu.Lock()
	k := len(h.terms.relays)
	h.terms.mu.Unlock()
	if k != 0 {
		t.Fatalf("a relay was made for a device that may not watch: %d", k)
	}

	// Before: an agent session's terminal is still watchable by it.
	say(h, answer, map[string]any{"type": "terminal_subscribe", "machine_id": "m1", "session_id": "s1"})
	if got := tn.await(t, 1); got[0] != "observe" {
		t.Fatalf("node log = %q", got)
	}

	// A full device opens the pane: the node is asked to watch it by its address.
	say(h, full, map[string]any{"type": "terminal_subscribe", "machine_id": "m1", "session_id": "pane:w1:p2"})
	if got := tn.await(t, 2); got[1] != "observe" {
		t.Fatalf("node log = %q", got)
	}
	say(h, full, map[string]any{"type": "terminal_unsubscribe", "machine_id": "m1", "session_id": "pane:w1:p2"})
	say(h, answer, map[string]any{"type": "terminal_unsubscribe", "machine_id": "m1", "session_id": "s1"})
}

func TestPaneScreenReadsNeedAFullDevice(t *testing.T) {
	var reached []string
	next := func(w http.ResponseWriter, r *http.Request) { reached = append(reached, r.PathValue("sid")) }
	guard := paneAddressFullOnly(next)
	call := func(role, sid string) int {
		r := httptest.NewRequest("GET", "/x", nil)
		r.SetPathValue("sid", sid)
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{Role: role, ID: "dev"}))
		w := httptest.NewRecorder()
		guard(w, r)
		return w.Code
	}
	if code := call(roleAnswer, "pane:w1:p2"); code != http.StatusForbidden {
		t.Errorf("answer-only device read a shell: %d", code)
	}
	if code := call(roleExtension, "pane:w1:p2"); code != http.StatusForbidden {
		t.Errorf("an extension token read a shell: %d", code)
	}
	if call(roleFull, "pane:w1:p2") != http.StatusOK || call(roleExtension, "s1") != http.StatusOK {
		t.Error("a full device's read of a pane, or an extension's read of an agent session, was refused")
	}
	if strings.Join(reached, ",") != "pane:w1:p2,s1" {
		t.Errorf("reached = %v", reached)
	}
}

func TestNewShellIsAFullDevicesAndNamesNoDirectory(t *testing.T) {
	tn := newTermNode(t)
	h := tn.hub
	m := h.byID["m1"]
	post := func(role, body string) int {
		r := httptest.NewRequest("POST", "/api/v1/machines/m1/panes", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{Role: role, ID: "dev"}))
		w := httptest.NewRecorder()
		h.newShell(w, r, m)
		return w.Code
	}
	if code := post(roleAnswer, `{"workspace_id":"w1"}`); code != http.StatusForbidden {
		t.Errorf("answer-only device opened a shell: %d", code)
	}
	if code := post(roleFull, `{}`); code != http.StatusBadRequest {
		t.Errorf("no workspace: %d", code)
	}
	if code := post(roleFull, `{"workspace_id":"w1","cwd":"/etc"}`); code != http.StatusOK {
		t.Errorf("full device: %d", code)
	}
	if got := tn.await(t, 1); len(got) != 1 || got[0] != "panes.new" {
		t.Errorf("node log = %q", got)
	}
}

// The routes are not on the lists of what an answer-only device (or a link) may call.
func TestPaneRoutesAreNotForAnswerOnlyDevices(t *testing.T) {
	for _, tc := range [][2]string{
		{http.MethodGet, "/api/v1/machines/m1/panes"}, {http.MethodPost, "/api/v1/machines/m1/panes"},
		{http.MethodGet, "/api/v1/sessions/m1/pane:w1:p2/screen"}, {http.MethodPost, "/api/v1/sessions/m1/pane:w1:p2/screen/before"},
	} {
		if allowed(principal{ID: "dev_watch", Role: roleAnswer}, tc[0], tc[1]) {
			t.Errorf("an answer-only device may %s %s", tc[0], tc[1])
		}
	}
}
