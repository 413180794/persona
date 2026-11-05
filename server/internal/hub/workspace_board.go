package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/workspaceboard"
)

// A separate store capability keeps older/test stores explicit: board writes never fall back to memory.
type workspaceBoardStore interface {
	WorkspaceBoard(context.Context) (workspaceboard.State, error)
	SaveWorkspaceBoard(context.Context, int64, workspaceboard.State) (workspaceboard.State, error)
	ListWorkspaceSnapshots(context.Context) ([]db.WorkspaceSnapshotRow, error)
	SaveWorkspaceSnapshot(context.Context, string, json.RawMessage) error
}

func (h *Hub) workspaceBoardRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/workspace-board", h.getWorkspaceBoard)
	api.HandleFunc("POST /api/v1/workspace-board", h.mutateWorkspaceBoard)
	api.HandleFunc("GET /api/v1/workspaces", h.getWorkspaces)
	h.workspaceBoardSettingsRoutes(api)
}

func (h *Hub) boardStore(w http.ResponseWriter) workspaceBoardStore {
	s, ok := h.store.(workspaceBoardStore)
	if !ok {
		writeErrCode(w, 503, "not_supported", i18n.Err("hub-workspace_board.err_no_store"))
		return nil
	}
	return s
}

func (h *Hub) effectiveBoardMachine(id string) string {
	if m := h.localMachine(id); m != nil {
		return m.cfg.ID
	}
	return id
}

// Fleet aliases can be absorbed after a card was bound. Project current routing
// identities without rewriting durable card bindings or merging their workflow state.
func (h *Hub) presentBoard(state workspaceboard.State) workspaceboard.State {
	state.Cards = append([]workspaceboard.Card{}, state.Cards...)
	for i := range state.Cards {
		c := &state.Cards[i]
		machine := h.effectiveBoardMachine(c.MachineID)
		if machine != c.MachineID {
			ws := qualifyWorkspace(machine, proto.Workspace{ID: c.WorkspaceID, Source: c.Source, SourceID: c.SourceID, Cwd: c.Cwd})
			c.MachineID, c.WorkspaceID = machine, ws.ID
		}
	}
	return state
}

func (h *Hub) writeBoard(w http.ResponseWriter, r *http.Request, state workspaceboard.State) {
	writeJSON(w, struct {
		workspaceboard.State
		CanWrite bool `json:"can_write"`
	}{h.presentBoard(state), principalOf(r.Context()).Role == roleFull})
}

func (h *Hub) getWorkspaceBoard(w http.ResponseWriter, r *http.Request) {
	store := h.boardStore(w)
	if store == nil {
		return
	}
	state, err := store.WorkspaceBoard(r.Context())
	if err != nil {
		writeErr(w, 503, err)
		return
	}
	h.writeBoard(w, r, state)
}

func (h *Hub) mutateWorkspaceBoard(w http.ResponseWriter, r *http.Request) {
	if principalOf(r.Context()).Role != roleFull {
		writeErr(w, 403, i18n.Err("hub-workspace_board.err_readonly"))
		return
	}
	store := h.boardStore(w)
	if store == nil {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	var fields map[string]json.RawMessage
	if err != nil || json.Unmarshal(body, &fields) != nil || fields["revision"] == nil || strings.TrimSpace(string(fields["revision"])) == "null" {
		writeBodyErr(w, err, i18n.Err("hub-workspace_board.err_bad_request"))
		return
	}
	var op workspaceboard.Mutation
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&op); err != nil {
		writeErr(w, 400, err)
		return
	}
	state, err := store.WorkspaceBoard(r.Context())
	if err != nil {
		writeErr(w, 503, err)
		return
	}
	if state.Revision != op.Revision {
		writeErrCode(w, 409, "conflict", db.ErrWorkspaceBoardConflict)
		return
	}
	switch op.Action {
	case "add_card":
		if err := h.resolveBoardCard(r.Context(), &op); err != nil {
			writeErr(w, 400, err)
			return
		}
		for _, card := range state.Cards {
			if h.effectiveBoardMachine(card.MachineID) == op.MachineID && workspaceDirectory(card.Cwd) == workspaceDirectory(op.Cwd) {
				writeErr(w, 400, i18n.Err("hub-workspace_board.err_already_bound"))
				return
			}
		}
		op.ID = "card_" + randomHex(12)
	case "add_column":
		op.ID = "col_" + randomHex(8)
	}
	next, err := workspaceboard.Apply(state, op)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	saved, err := store.SaveWorkspaceBoard(r.Context(), op.Revision, next)
	if errors.Is(err, db.ErrWorkspaceBoardConflict) {
		writeErrCode(w, 409, "conflict", err)
		return
	}
	if err != nil {
		writeErr(w, 503, err)
		return
	}
	h.broadcast(map[string]any{"type": "workspace_board", "revision": saved.Revision})
	h.writeBoard(w, r, saved)
}

// Only the node can resolve a directory on its filesystem; the HTTP caller cannot forge discovery metadata.
func (h *Hub) resolveBoardCard(ctx context.Context, op *workspaceboard.Mutation) error {
	m := h.localMachine(op.MachineID)
	if m == nil {
		return i18n.Err("hub-workspace_board.err_unknown_machine")
	}
	if !m.isOnline() {
		return i18n.Err("hub-workspace_board.err_offline")
	}
	op.MachineID = m.cfg.ID
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	var selected *proto.Workspace
	if op.WorkspaceID != "" {
		m.mu.Lock()
		sessions := append([]proto.Session(nil), m.sessions...)
		m.mu.Unlock()
		for _, session := range sessions {
			candidate := workspaceForSession(m.cfg.ID, session)
			if candidate.Cwd != "" && candidate.ID == op.WorkspaceID {
				selected = &candidate
				break
			}
		}
		if selected == nil {
			var found proto.WorkspacesResponse
			if err := m.call(ctx, "workspaces.list", map[string]any{}, &found); err != nil {
				return i18n.Err("hub-workspace_board.err_list_failed", i18n.Args{"error": err.Error()})
			}
			for _, item := range found.Workspaces {
				candidate := qualifyWorkspace(m.cfg.ID, item)
				if candidate.ID == op.WorkspaceID {
					selected = &candidate
					break
				}
			}
		}
		if selected == nil {
			return i18n.Err("hub-workspace_board.err_changed")
		}
		if selected.Cwd == "" {
			return i18n.Err("hub-workspace_board.err_no_directory")
		}
		op.Cwd = selected.Cwd
	}

	if strings.TrimSpace(op.Cwd) == "" {
		return i18n.Err("hub-workspace_board.err_pick_directory")
	}
	var resolved proto.Workspace
	if err := m.call(ctx, "workspace.resolve", map[string]any{"path": op.Cwd}, &resolved); err != nil {
		return i18n.Err("hub-workspace_board.err_resolve_failed", i18n.Args{"error": err.Error()})
	}
	if resolved.Cwd == "" || !resolved.Available {
		return i18n.Err("hub-workspace_board.err_resolve_empty")
	}
	if selected != nil {
		op.Source, op.SourceID, op.Branch = selected.Source, selected.SourceID, selected.Branch
		op.WorkspaceID = selected.ID
	} else {
		resolved = qualifyWorkspace(m.cfg.ID, resolved)
		op.Source, op.SourceID, op.Branch = resolved.Source, resolved.SourceID, resolved.Branch
		op.WorkspaceID = resolved.ID
	}
	op.Cwd = resolved.Cwd
	if op.Title == nil || strings.TrimSpace(*op.Title) == "" {
		title := resolved.Title
		if selected != nil {
			title = selected.Title
		}
		op.Title = &title
	}
	return nil
}

func workspaceDirectory(p string) string {
	if canonical, err := workspaceboard.CanonicalCwd(p); err == nil {
		return canonical
	}
	return p
}

func qualifyWorkspace(machine string, w proto.Workspace) proto.Workspace {
	w.Cwd = workspaceDirectory(w.Cwd)
	if w.Source == "" {
		w.Source = "workspace"
	}
	if w.SourceID == "" {
		w.SourceID = w.ID
	}
	if w.SourceID == "" {
		w.SourceID = w.Cwd
	}
	sum := sha256.Sum256([]byte(machine + "\x00" + w.Source + "\x00" + w.SourceID))
	w.ID, w.MachineID = "ws_"+hex.EncodeToString(sum[:12]), machine
	return w
}

func sessionBoardDirectory(s proto.Session) string {
	if s.WorkspaceCwd != "" {
		return workspaceDirectory(s.WorkspaceCwd)
	}
	return workspaceDirectory(s.Cwd)
}

func workspaceForSession(machine string, s proto.Session) proto.Workspace {
	title := s.SpaceName
	if title == "" {
		title = s.Cwd
	}
	cwd := sessionBoardDirectory(s)
	return qualifyWorkspace(machine, proto.Workspace{Source: "session-directory", SourceID: cwd, Title: title, Cwd: cwd})
}

type workspaceDiscoveryError struct {
	MachineID string `json:"machine_id"`
	Error     string `json:"error"`
}

func (h *Hub) getWorkspaces(w http.ResponseWriter, r *http.Request) {
	l := h.requestLang(r)
	store := h.boardStore(w)
	if store == nil {
		return
	}
	// Serialize discovery so a slow earlier request cannot overwrite a later snapshot in this hub.
	h.workspaceMu.Lock()
	defer h.workspaceMu.Unlock()
	cached, err := store.ListWorkspaceSnapshots(r.Context())
	if err != nil {
		writeErr(w, 503, err)
		return
	}
	byMachine := map[string]db.WorkspaceSnapshotRow{}
	for _, row := range cached {
		machine := h.effectiveBoardMachine(row.MachineID)
		if previous, ok := byMachine[machine]; ok && !row.UpdatedAt.After(previous.UpdatedAt) {
			continue
		}
		var entries []proto.Workspace
		if json.Unmarshal(row.Data, &entries) == nil {
			for i := range entries {
				entries[i] = qualifyWorkspace(machine, entries[i])
			}
			row.Data, _ = json.Marshal(entries)
		}
		byMachine[machine] = row
	}
	state, err := store.WorkspaceBoard(r.Context())
	if err != nil {
		writeErr(w, 503, err)
		return
	}
	state = h.presentBoard(state)
	type discovered struct {
		machine      *Machine
		workspaces   []proto.Workspace
		err          error
		at           time.Time
		directories  map[string]bool
		sourceErrors []proto.WorkspaceSourceError
	}
	machines := h.machinesNow()
	results := make(chan discovered, len(machines))
	for _, m := range machines {
		go func(m *Machine) {
			v := discovered{machine: m, at: time.Now().UTC()}
			if !m.isOnline() {
				results <- v
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
			defer cancel()
			var response proto.WorkspacesResponse
			v.err = m.call(ctx, "workspaces.list", map[string]any{}, &response)
			v.sourceErrors = response.Errors
			if v.err == nil {
				v.workspaces = make([]proto.Workspace, 0, len(response.Workspaces))
				for _, item := range response.Workspaces {
					item = qualifyWorkspace(m.cfg.ID, item)
					item.Online = true
					item.LastSeen = v.at.Format(time.RFC3339Nano)
					v.workspaces = append(v.workspaces, item)
				}
			}
			var paths []string
			for _, c := range state.Cards {
				if c.MachineID == m.cfg.ID {
					paths = append(paths, c.Cwd)
				}
			}
			v.directories = map[string]bool{}
			for start := 0; start < len(paths); start += 200 {
				end := min(start+200, len(paths))
				var stats struct {
					Stats []struct {
						Path string `json:"path"`
						Kind string `json:"kind"`
					} `json:"stats"`
				}
				if err := m.call(ctx, "fs.stat", map[string]any{"paths": paths[start:end]}, &stats); err != nil {
					break
				}
				for _, st := range stats.Stats {
					v.directories[st.Path] = st.Kind == "dir"
				}
			}
			results <- v
		}(m)
	}
	out := []proto.Workspace{}
	errs := []workspaceDiscoveryError{}
	known := map[string]bool{}
	for range machines {
		result := <-results
		m := result.machine
		online := m.isOnline()
		entries := result.workspaces
		if entries == nil {
			if old, ok := byMachine[m.cfg.ID]; ok {
				if err := json.Unmarshal(old.Data, &entries); err != nil {
					errs = append(errs, workspaceDiscoveryError{m.cfg.ID, i18n.T(l, "hub-workspace_board.err_snapshot_read")})
				}
			}
		}
		if len(result.sourceErrors) > 0 {
			failed := map[string]bool{}
			present := map[string]bool{}
			for _, item := range entries {
				present[item.ID] = true
			}
			for _, failure := range result.sourceErrors {
				failed[failure.Source] = true
				errs = append(errs, workspaceDiscoveryError{m.cfg.ID, failure.Source + ": " + failure.Error})
			}
			if old, ok := byMachine[m.cfg.ID]; ok {
				var previous []proto.Workspace
				if json.Unmarshal(old.Data, &previous) == nil {
					for _, item := range previous {
						if failed[item.Source] && !present[item.ID] {
							item.Available = false
							entries = append(entries, item)
						}
					}
				}
			}
		}
		for i := range entries {
			entries[i] = qualifyWorkspace(m.cfg.ID, entries[i])
			entries[i].Online = online
			if result.err != nil || !online {
				entries[i].Available = false
			}
		}
		if result.err != nil {
			errs = append(errs, workspaceDiscoveryError{m.cfg.ID, i18n.Localize(result.err, l)})
		}
		m.mu.Lock()
		sessions := append([]proto.Session(nil), m.sessions...)
		m.mu.Unlock()
		containsDirectory := func(cwd string) bool {
			for _, item := range entries {
				if workspaceDirectory(item.Cwd) == workspaceDirectory(cwd) {
					return true
				}
			}
			return false
		}
		for _, session := range sessions {
			if sessionBoardDirectory(session) == "" || containsDirectory(sessionBoardDirectory(session)) {
				continue
			}
			item := workspaceForSession(m.cfg.ID, session)
			item.Online, item.Available = online, online
			if online {
				item.LastSeen = result.at.Format(time.RFC3339Nano)
			}
			entries = append(entries, item)
		}
		for _, card := range state.Cards {
			if card.MachineID != m.cfg.ID || containsDirectory(card.Cwd) {
				continue
			}
			entries = append(entries, proto.Workspace{ID: card.WorkspaceID, MachineID: card.MachineID, Source: card.Source, SourceID: card.SourceID, Title: card.Title, Cwd: card.Cwd, Branch: card.Branch, Online: online})
		}
		for i := range entries {
			for _, card := range state.Cards {
				if card.MachineID == m.cfg.ID && workspaceDirectory(card.Cwd) == entries[i].Cwd {
					entries[i].Available = online && result.directories[card.Cwd]
					if entries[i].Available {
						entries[i].LastSeen = result.at.Format(time.RFC3339Nano)
					}
					break
				}
			}
		}
		// Persist the merged directory catalog, not only spaces that still have a host terminal.
		if online && (result.err == nil || len(sessions) > 0) {
			raw, _ := json.Marshal(entries)
			if err := store.SaveWorkspaceSnapshot(r.Context(), m.cfg.ID, raw); err != nil {
				errs = append(errs, workspaceDiscoveryError{m.cfg.ID, i18n.T(l, "hub-workspace_board.err_snapshot_save")})
			}
		}
		for _, item := range entries {
			if !known[item.ID] {
				known[item.ID] = true
				out = append(out, item)
			}
		}
	}
	for _, card := range state.Cards {
		found := false
		for _, item := range out {
			if item.MachineID == card.MachineID && workspaceDirectory(item.Cwd) == card.Cwd {
				found = true
				break
			}
		}
		if !found {
			out = append(out, proto.Workspace{ID: card.WorkspaceID, MachineID: card.MachineID, Source: card.Source, SourceID: card.SourceID, Title: card.Title, Cwd: card.Cwd, Branch: card.Branch})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MachineID != out[j].MachineID {
			return out[i].MachineID < out[j].MachineID
		}
		return out[i].ID < out[j].ID
	})
	writeJSON(w, map[string]any{"workspaces": out, "errors": errs})
}
