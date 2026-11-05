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
	if err := decodeJSONBody(w, r, &body, 4<<10); err != nil || body.WorkspaceID == "" {
		writeBodyErr(w, err, errors.New("workspace_id is required"))
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

// The terminal browser's and the sidebar's context menus change things on a computer (docs/terminal.md「节点菜单」):
// close a tab or a pane, split a pane, rename a tab, run an agent in a plain shell pane, fetch / pull / push. Each is
// a route of its own that a full device alone may call; an answer-only device (the watch) and the tokens an extension
// holds are refused here and by the lists of what they may call (devicetokens.go, extension_scopes.go). The body is
// read field by field: only the named string fields reach the node, so a request cannot name a directory or a command.

type nodeAction struct {
	method   string
	timeout  time.Duration
	required []string
	optional []string
}

func (h *Hub) nodeAction(a nodeAction) http.HandlerFunc {
	return h.withMachine(func(w http.ResponseWriter, r *http.Request, m *Machine) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("changing a computer's terminals needs a full device"))
			return
		}
		var body map[string]any
		if err := decodeJSONBody(w, r, &body, 4<<10); err != nil {
			writeBodyErr(w, err, errors.New("bad request body"))
			return
		}
		params := map[string]any{}
		for _, k := range append(append([]string{}, a.required...), a.optional...) {
			if v, ok := body[k].(string); ok && v != "" {
				params[k] = v
			}
		}
		for _, k := range a.required {
			if params[k] == nil {
				writeErr(w, http.StatusBadRequest, errors.New(k+" is required"))
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), a.timeout)
		defer cancel()
		var out json.RawMessage
		if err := m.call(ctx, a.method, params, &out); err != nil {
			callErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if len(out) == 0 {
			out = json.RawMessage(`{}`)
		}
		_, _ = w.Write(out)
	})
}

// nodeActions are the routes of those menu items, by path under /api/v1/machines/{mid}/.
var nodeActions = map[string]nodeAction{
	"panes/close": {method: "panes.close", timeout: 30 * time.Second, required: []string{"pane"}},
	"panes/split": {method: "panes.split", timeout: 30 * time.Second, required: []string{"pane", "direction"}},
	"panes/agent": {method: "panes.agent", timeout: 120 * time.Second, required: []string{"pane", "launcher"}},
	"panes/git":   {method: "panes.git", timeout: 90 * time.Second, required: []string{"op"}, optional: []string{"pane", "workspace_id", "kind"}},
	"tabs/close":  {method: "tabs.close", timeout: 30 * time.Second, required: []string{"tab_id"}, optional: []string{"kind"}},
	"tabs/rename": {method: "tabs.rename", timeout: 30 * time.Second, required: []string{"tab_id", "label"}, optional: []string{"kind"}},
	// #2431 新建 worktree…: the repository of a workspace the host lists, and a Git worktree of it opened as a workspace.
	// The branch and the path are typed in the dialog; the node refuses a path that holds something and never forces.
	"worktree/prepare": {method: "worktree.prepare", timeout: 30 * time.Second, required: []string{"workspace_id"}, optional: []string{"kind"}},
	"worktree/create":  {method: "worktree.create", timeout: 150 * time.Second, required: []string{"workspace_id", "branch", "path"}, optional: []string{"kind"}},
}
