package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// A terminal addressed by pane ("pane:<id>", docs/terminal.md「独立终端」) is a shell on the computer, agent or not.
// Only a full device may open one: the streams of agent sessions are watchable by an answer-only device (the watch
// shows what it is asked about), a shell's screen is more than that. The same goes for tokens an extension holds
// (agents.read opens an agent's screen, not a shell's).

// paneAddress: the session id of a stream is a pane address.
func paneAddress(id string) bool { return strings.HasPrefix(id, "pane:") }

// paneAddressFullOnly guards the HTTP screen reads, which take a session id: a pane address needs a full device.
func paneAddressFullOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if paneAddress(r.PathValue("sid")) && principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("a terminal opened by pane needs a full device"))
			return
		}
		next(w, r)
	}
}

// newShell opens a shell tab in one of a machine's workspaces. Only the user's click gets here; the node picks the
// directory (the workspace's own), the request cannot name one.
func (h *Hub) newShell(w http.ResponseWriter, r *http.Request, m *Machine) {
	if principalOf(r.Context()).Role != roleFull {
		writeErr(w, http.StatusForbidden, errors.New("opening a terminal needs a full device"))
		return
	}
	var body struct {
		Kind        string `json:"kind"`
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil || body.WorkspaceID == "" {
		writeErr(w, http.StatusBadRequest, errors.New("workspace_id is required"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var out json.RawMessage
	if err := m.call(ctx, "panes.new", body, &out); err != nil {
		callErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(out)
}
