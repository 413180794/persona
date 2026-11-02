package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/db"
	"github.com/chyroc/jarvis/server/internal/proto"
)

type Todo struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"`
	Notes     string      `json:"notes"`
	DoneAt    *time.Time  `json:"done_at"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	Agents    []TodoAgent `json:"agents"`
}

type TodoAgent struct {
	MachineID  string     `json:"machine_id"`
	SessionID  string     `json:"session_id"`
	LinkedAt   time.Time  `json:"linked_at"`
	ReleasedAt *time.Time `json:"released_at,omitempty"`
	Title      string     `json:"title"`
	Cwd        string     `json:"cwd"`
	Agent      string     `json:"agent"`
	StartError string     `json:"start_error,omitempty"`
}

type todosState struct {
	mu              sync.RWMutex
	loaded          bool
	todos           map[string]*Todo
	activeBySession map[[2]string]string // (machine_id, session_id) -> active todo_id
	snapshotSyncing map[[2]string]bool   // (machine_id, session_id) in flight
}

func newTodosState() todosState {
	return todosState{
		todos:           make(map[string]*Todo),
		activeBySession: make(map[[2]string]string),
		snapshotSyncing: make(map[[2]string]bool),
	}
}

func (h *Hub) ensureTodosLoaded(ctx context.Context) error {
	h.todos.mu.RLock()
	if h.todos.loaded {
		h.todos.mu.RUnlock()
		return nil
	}
	h.todos.mu.RUnlock()

	h.todos.mu.Lock()
	defer h.todos.mu.Unlock()
	if h.todos.loaded {
		return nil
	}
	return h.loadTodosLocked(ctx)
}

func (h *Hub) loadTodosLocked(ctx context.Context) error {
	if h.todos.todos == nil {
		h.todos.todos = make(map[string]*Todo)
	}
	if h.todos.activeBySession == nil {
		h.todos.activeBySession = make(map[[2]string]string)
	}
	if h.todos.snapshotSyncing == nil {
		h.todos.snapshotSyncing = make(map[[2]string]bool)
	}

	if h.store == nil {
		h.todos.loaded = true
		return nil
	}

	tlist, err := h.store.ListTodos(ctx)
	if err != nil {
		return err
	}
	alist, err := h.store.ListAllTodoAgents(ctx)
	if err != nil {
		return err
	}

	tmap := make(map[string]*Todo, len(tlist))
	active := make(map[[2]string]string)
	for _, tr := range tlist {
		tmap[tr.ID] = &Todo{
			ID:        tr.ID,
			Title:     tr.Title,
			Notes:     tr.Notes,
			DoneAt:    tr.DoneAt,
			CreatedAt: tr.CreatedAt,
			UpdatedAt: tr.UpdatedAt,
			Agents:    make([]TodoAgent, 0),
		}
	}

	for _, ar := range alist {
		t, ok := tmap[ar.TodoID]
		if !ok {
			continue
		}
		t.Agents = append(t.Agents, TodoAgent{
			MachineID:  ar.MachineID,
			SessionID:  ar.SessionID,
			LinkedAt:   ar.LinkedAt,
			ReleasedAt: ar.ReleasedAt,
			Title:      ar.Title,
			Cwd:        ar.Cwd,
			Agent:      ar.Agent,
			StartError: ar.StartError,
		})
		if ar.ReleasedAt == nil && t.DoneAt == nil {
			active[[2]string{ar.MachineID, ar.SessionID}] = t.ID
		}
	}

	h.todos.todos = tmap
	h.todos.activeBySession = active
	h.todos.loaded = true
	return nil
}

func (h *Hub) initTodos(ctx context.Context) {
	h.todos.mu.Lock()
	defer h.todos.mu.Unlock()
	if err := h.loadTodosLocked(ctx); err != nil {
		h.logf("todos: init load failed: %v", err)
	}
}

func copyTodoLocked(t *Todo) Todo {
	out := *t
	if t.DoneAt != nil {
		da := *t.DoneAt
		out.DoneAt = &da
	}
	out.Agents = make([]TodoAgent, len(t.Agents))
	for i, a := range t.Agents {
		out.Agents[i] = a
		if a.ReleasedAt != nil {
			ra := *a.ReleasedAt
			out.Agents[i].ReleasedAt = &ra
		}
	}
	return out
}

func formatTodo(t Todo) Todo {
	out := t
	out.Agents = make([]TodoAgent, 0, len(t.Agents))
	var curr, past []TodoAgent
	for _, a := range t.Agents {
		if a.ReleasedAt == nil {
			curr = append(curr, a)
		} else {
			past = append(past, a)
		}
	}
	sort.Slice(curr, func(i, j int) bool {
		return curr[i].LinkedAt.After(curr[j].LinkedAt)
	})
	sort.Slice(past, func(i, j int) bool {
		if past[i].ReleasedAt != nil && past[j].ReleasedAt != nil {
			return past[i].ReleasedAt.After(*past[j].ReleasedAt)
		}
		return false
	})
	out.Agents = append(out.Agents, curr...)
	out.Agents = append(out.Agents, past...)
	return out
}

func cleanPromptTitle(prompt string) string {
	s := strings.TrimSpace(prompt)
	if strings.HasPrefix(s, "<jarvis-context") {
		if endIdx := strings.Index(s, "</jarvis-context>"); endIdx != -1 {
			s = s[endIdx+len("</jarvis-context>"):]
		} else if endTagIdx := strings.Index(s, ">"); endTagIdx != -1 {
			s = s[endTagIdx+1:]
		} else {
			s = ""
		}
	}
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.Contains(trimmed, "<jarvis-context") {
			return trimmed
		}
	}
	return ""
}

func (h *Hub) todoRoutes(api *http.ServeMux) {
	// GET /api/v1/todos: list all todos (newest created_at first), agents sorted (active first, then released)
	api.HandleFunc("GET /api/v1/todos", func(w http.ResponseWriter, r *http.Request) {
		if err := h.ensureTodosLoaded(r.Context()); err != nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("todos service unavailable"))
			return
		}
		h.todos.mu.RLock()
		list := make([]Todo, 0, len(h.todos.todos))
		for _, t := range h.todos.todos {
			list = append(list, formatTodo(copyTodoLocked(t)))
		}
		h.todos.mu.RUnlock()

		sort.Slice(list, func(i, j int) bool {
			return list[i].CreatedAt.After(list[j].CreatedAt)
		})
		writeJSON(w, map[string]any{"todos": list})
	})

	// POST /api/v1/todos: create todo {title, notes}
	api.HandleFunc("POST /api/v1/todos", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot modify todos"))
			return
		}
		if err := h.ensureTodosLoaded(r.Context()); err != nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("todos service unavailable"))
			return
		}
		var in struct {
			Title string `json:"title"`
			Notes string `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		title := strings.TrimSpace(in.Title)
		if title == "" {
			writeErr(w, http.StatusBadRequest, errors.New("title cannot be empty"))
			return
		}
		now := time.Now().UTC()
		id := "todo_" + randomHex(8)
		row := db.TodoRow{
			ID:        id,
			Title:     title,
			Notes:     in.Notes,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if h.store != nil {
			if err := h.store.CreateTodo(r.Context(), row); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}

		tObj := &Todo{
			ID:        id,
			Title:     title,
			Notes:     in.Notes,
			CreatedAt: now,
			UpdatedAt: now,
			Agents:    make([]TodoAgent, 0),
		}
		h.todos.mu.Lock()
		h.todos.todos[id] = tObj
		resTodo := formatTodo(copyTodoLocked(tObj))
		h.todos.mu.Unlock()

		h.broadcast(map[string]any{"type": "todos"})
		writeJSON(w, map[string]any{"todo": resTodo})
	})

	// PATCH /api/v1/todos/{id}: update title, notes, done
	api.HandleFunc("PATCH /api/v1/todos/{id}", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot modify todos"))
			return
		}
		if err := h.ensureTodosLoaded(r.Context()); err != nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("todos service unavailable"))
			return
		}
		id := r.PathValue("id")
		h.todos.mu.RLock()
		cur, ok := h.todos.todos[id]
		h.todos.mu.RUnlock()
		if !ok {
			writeErr(w, http.StatusNotFound, errors.New("todo not found"))
			return
		}

		var in struct {
			Title *string `json:"title"`
			Notes *string `json:"notes"`
			Done  *bool   `json:"done"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if in.Title != nil {
			t := strings.TrimSpace(*in.Title)
			if t == "" {
				writeErr(w, http.StatusBadRequest, errors.New("title cannot be empty"))
				return
			}
			in.Title = &t
		}

		now := time.Now().UTC()
		var updatedRow *db.TodoRow
		if h.store != nil {
			var err error
			updatedRow, err = h.store.UpdateTodo(r.Context(), id, in.Title, in.Notes, in.Done, now)
			if err != nil {
				if errors.Is(err, db.ErrTodoNotFound) {
					writeErr(w, http.StatusNotFound, err)
				} else {
					writeErr(w, http.StatusInternalServerError, err)
				}
				return
			}
		} else {
			updatedRow = &db.TodoRow{
				ID:        cur.ID,
				Title:     cur.Title,
				Notes:     cur.Notes,
				DoneAt:    cur.DoneAt,
				CreatedAt: cur.CreatedAt,
				UpdatedAt: now,
			}
			if in.Title != nil {
				updatedRow.Title = *in.Title
			}
			if in.Notes != nil {
				updatedRow.Notes = *in.Notes
			}
			if in.Done != nil {
				if *in.Done {
					if updatedRow.DoneAt == nil {
						updatedRow.DoneAt = &now
					}
				} else {
					updatedRow.DoneAt = nil
				}
			}
		}

		var affectedMachines []string
		var result Todo
		h.todos.mu.Lock()
		if t, ok := h.todos.todos[id]; ok {
			t.Title = updatedRow.Title
			t.Notes = updatedRow.Notes
			t.DoneAt = updatedRow.DoneAt
			t.UpdatedAt = updatedRow.UpdatedAt
			for _, a := range t.Agents {
				if a.ReleasedAt == nil {
					affectedMachines = append(affectedMachines, a.MachineID)
					key := [2]string{a.MachineID, a.SessionID}
					if t.DoneAt != nil {
						delete(h.todos.activeBySession, key)
					} else {
						h.todos.activeBySession[key] = t.ID
					}
				}
			}
			result = formatTodo(copyTodoLocked(t))
		}
		h.todos.mu.Unlock()

		for _, mid := range affectedMachines {
			if m := h.byID[mid]; m != nil {
				h.sessionsChanged(m)
			}
		}
		h.broadcast(map[string]any{"type": "todos"})
		writeJSON(w, map[string]any{"todo": result})
	})

	// DELETE /api/v1/todos/{id}: delete todo
	api.HandleFunc("DELETE /api/v1/todos/{id}", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot modify todos"))
			return
		}
		if err := h.ensureTodosLoaded(r.Context()); err != nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("todos service unavailable"))
			return
		}
		id := r.PathValue("id")
		h.todos.mu.RLock()
		_, ok := h.todos.todos[id]
		h.todos.mu.RUnlock()
		if !ok {
			writeErr(w, http.StatusNotFound, errors.New("todo not found"))
			return
		}

		if h.store != nil {
			if err := h.store.DeleteTodo(r.Context(), id); err != nil {
				if errors.Is(err, db.ErrTodoNotFound) {
					writeErr(w, http.StatusNotFound, err)
				} else {
					writeErr(w, http.StatusInternalServerError, err)
				}
				return
			}
		}

		var affectedMachines []string
		h.todos.mu.Lock()
		if cur, ok := h.todos.todos[id]; ok {
			for _, a := range cur.Agents {
				if a.ReleasedAt == nil {
					affectedMachines = append(affectedMachines, a.MachineID)
					delete(h.todos.activeBySession, [2]string{a.MachineID, a.SessionID})
				}
			}
			delete(h.todos.todos, id)
		}
		h.todos.mu.Unlock()

		for _, mid := range affectedMachines {
			if m := h.byID[mid]; m != nil {
				h.sessionsChanged(m)
			}
		}
		h.broadcast(map[string]any{"type": "todos"})
		writeJSON(w, map[string]any{"ok": true})
	})

	// PUT /api/v1/todos/{id}/agents/{mid}/{sid}: link existing agent
	api.HandleFunc("PUT /api/v1/todos/{id}/agents/{mid}/{sid}", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot modify todos"))
			return
		}
		if err := h.ensureTodosLoaded(r.Context()); err != nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("todos service unavailable"))
			return
		}
		todoID := r.PathValue("id")
		mid := r.PathValue("mid")
		sid := r.PathValue("sid")

		tInfo, err := h.getTodoForLink(r.Context(), todoID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if tInfo == nil {
			writeErr(w, http.StatusNotFound, errors.New("todo not found"))
			return
		}
		if tInfo.DoneAt != nil {
			writeErr(w, http.StatusConflict, errors.New("todo is already completed"))
			return
		}

		var title, cwd, agent string
		if m := h.byID[mid]; m != nil {
			if s, ok := m.session(sid); ok {
				title = s.Title
				cwd = s.Cwd
				agent = s.Agent
			}
		}

		if err := h.linkTodoAgent(r.Context(), todoID, mid, sid, title, cwd, agent, ""); err != nil {
			if errors.Is(err, db.ErrTodoNotFound) {
				writeErr(w, http.StatusNotFound, err)
			} else if errors.Is(err, db.ErrTodoCompleted) {
				writeErr(w, http.StatusConflict, err)
			} else {
				writeErr(w, http.StatusInternalServerError, err)
			}
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	// DELETE /api/v1/todos/{id}/agents/{mid}/{sid}: unlink agent
	api.HandleFunc("DELETE /api/v1/todos/{id}/agents/{mid}/{sid}", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot modify todos"))
			return
		}
		if err := h.ensureTodosLoaded(r.Context()); err != nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("todos service unavailable"))
			return
		}
		todoID := r.PathValue("id")
		mid := r.PathValue("mid")
		sid := r.PathValue("sid")

		h.todos.mu.RLock()
		_, ok := h.todos.todos[todoID]
		h.todos.mu.RUnlock()
		if !ok {
			writeErr(w, http.StatusNotFound, errors.New("todo not found"))
			return
		}

		if h.store != nil {
			if err := h.store.UnlinkTodoAgent(r.Context(), todoID, mid, sid); err != nil {
				if errors.Is(err, db.ErrTodoNotFound) {
					writeErr(w, http.StatusNotFound, err)
				} else {
					writeErr(w, http.StatusInternalServerError, err)
				}
				return
			}
		}

		now := time.Now().UTC()
		wasActive := false
		h.todos.mu.Lock()
		if target, ok := h.todos.todos[todoID]; ok {
			var kept []TodoAgent
			for _, a := range target.Agents {
				if a.MachineID == mid && a.SessionID == sid {
					if a.ReleasedAt == nil {
						wasActive = true
					}
					continue
				}
				kept = append(kept, a)
			}
			target.Agents = kept
			target.UpdatedAt = now
		}
		if wasActive {
			if curTid, ok := h.todos.activeBySession[[2]string{mid, sid}]; ok && curTid == todoID {
				delete(h.todos.activeBySession, [2]string{mid, sid})
			}
		}
		h.todos.mu.Unlock()

		if wasActive {
			if m := h.byID[mid]; m != nil {
				h.sessionsChanged(m)
			}
		}
		h.broadcast(map[string]any{"type": "todos"})
		writeJSON(w, map[string]any{"ok": true})
	})
}

// linkTodoAgent links or reactivates an agent on todoID, releasing any active link elsewhere.
func (h *Hub) linkTodoAgent(ctx context.Context, todoID, mid, sid, title, cwd, agent, startError string) error {
	if err := h.ensureTodosLoaded(ctx); err != nil {
		return err
	}
	now := time.Now().UTC()
	var releasedOtherTodoIDs []string

	if h.store != nil {
		var err error
		releasedOtherTodoIDs, err = h.store.LinkTodoAgent(ctx, todoID, mid, sid, title, cwd, agent, startError, now)
		if err != nil {
			return err
		}
	} else {
		// In-memory fallback
		h.todos.mu.RLock()
		t, ok := h.todos.todos[todoID]
		h.todos.mu.RUnlock()
		if !ok {
			return db.ErrTodoNotFound
		}
		if t.DoneAt != nil {
			return db.ErrTodoCompleted
		}
		for oid, other := range h.todos.todos {
			if oid != todoID {
				for _, a := range other.Agents {
					if a.MachineID == mid && a.SessionID == sid && a.ReleasedAt == nil {
						releasedOtherTodoIDs = append(releasedOtherTodoIDs, oid)
					}
				}
			}
		}
	}

	h.todos.mu.Lock()
	for _, oid := range releasedOtherTodoIDs {
		if other, ok := h.todos.todos[oid]; ok {
			for i := range other.Agents {
				if other.Agents[i].MachineID == mid && other.Agents[i].SessionID == sid && other.Agents[i].ReleasedAt == nil {
					other.Agents[i].ReleasedAt = &now
				}
			}
			other.UpdatedAt = now
		}
	}

	if target, ok := h.todos.todos[todoID]; ok {
		found := false
		for i := range target.Agents {
			if target.Agents[i].MachineID == mid && target.Agents[i].SessionID == sid {
				target.Agents[i].ReleasedAt = nil
				target.Agents[i].LinkedAt = now
				if title != "" {
					target.Agents[i].Title = title
				}
				if cwd != "" {
					target.Agents[i].Cwd = cwd
				}
				if agent != "" {
					target.Agents[i].Agent = agent
				}
				if startError != "" {
					target.Agents[i].StartError = startError
				}
				found = true
				break
			}
		}
		if !found {
			target.Agents = append(target.Agents, TodoAgent{
				MachineID:  mid,
				SessionID:  sid,
				LinkedAt:   now,
				ReleasedAt: nil,
				Title:      title,
				Cwd:        cwd,
				Agent:      agent,
				StartError: startError,
			})
		}
		target.UpdatedAt = now
		h.todos.activeBySession[[2]string{mid, sid}] = todoID
	}
	h.todos.mu.Unlock()

	if m := h.byID[mid]; m != nil {
		h.sessionsChanged(m)
	}
	h.broadcast(map[string]any{"type": "todos"})
	return nil
}

type TodoLinkInfo struct {
	ID     string
	Title  string
	DoneAt *time.Time
}

// getTodoForLink gets a safe copy of todo info for validation before session start or message send.
func (h *Hub) getTodoForLink(ctx context.Context, todoID string) (*TodoLinkInfo, error) {
	if err := h.ensureTodosLoaded(ctx); err != nil {
		return nil, err
	}
	h.todos.mu.RLock()
	defer h.todos.mu.RUnlock()
	t, ok := h.todos.todos[todoID]
	if !ok {
		return nil, nil
	}
	var doneAtCopy *time.Time
	if t.DoneAt != nil {
		da := *t.DoneAt
		doneAtCopy = &da
	}
	return &TodoLinkInfo{
		ID:     t.ID,
		Title:  t.Title,
		DoneAt: doneAtCopy,
	}, nil
}

// checkAndUpdateTodoSnapshots checks if any active agent's title or start_error changed and updates db asynchronously.
// In-flight updates for the same (machine_id, session_id) are deduplicated.
func (h *Hub) checkAndUpdateTodoSnapshots(m *Machine, list []proto.Session) {
	h.todos.mu.Lock()
	if !h.todos.loaded {
		h.todos.mu.Unlock()
		return
	}

	var needUpdate []struct {
		machineID  string
		sessionID  string
		title      string
		startError string
	}

	for _, s := range list {
		key := [2]string{m.cfg.ID, s.SessionID}
		if h.todos.snapshotSyncing[key] {
			continue // deduplicate in-flight writes
		}
		if tid, ok := h.todos.activeBySession[key]; ok {
			if t, ok := h.todos.todos[tid]; ok {
				for _, a := range t.Agents {
					if a.MachineID == m.cfg.ID && a.SessionID == s.SessionID && a.ReleasedAt == nil {
						titleChanged := s.Title != "" && s.Title != a.Title
						errChanged := s.StartError != a.StartError
						if titleChanged || errChanged {
							newTitle := a.Title
							if titleChanged {
								newTitle = s.Title
							}
							h.todos.snapshotSyncing[key] = true
							needUpdate = append(needUpdate, struct {
								machineID  string
								sessionID  string
								title      string
								startError string
							}{m.cfg.ID, s.SessionID, newTitle, s.StartError})
						}
						break
					}
				}
			}
		}
	}
	h.todos.mu.Unlock()

	if len(needUpdate) > 0 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			changed := false
			for _, item := range needUpdate {
				key := [2]string{item.machineID, item.sessionID}
				var err error
				if h.store != nil {
					err = h.store.UpdateTodoAgentSnapshot(ctx, item.machineID, item.sessionID, item.title, item.startError)
				}
				h.todos.mu.Lock()
				delete(h.todos.snapshotSyncing, key)
				if err == nil {
					// Update memory only upon write success
					changed = true
					if tid, ok := h.todos.activeBySession[key]; ok {
						if t, ok := h.todos.todos[tid]; ok {
							for i := range t.Agents {
								if t.Agents[i].MachineID == item.machineID && t.Agents[i].SessionID == item.sessionID && t.Agents[i].ReleasedAt == nil {
									if item.title != "" {
										t.Agents[i].Title = item.title
									}
									t.Agents[i].StartError = item.startError
									break
								}
							}
						}
					}
				} else {
					h.logf("todos: snapshot update %s/%s failed: %v", item.machineID, item.sessionID, err)
				}
				h.todos.mu.Unlock()
			}
			if changed {
				h.broadcast(map[string]any{"type": "todos"})
			}
		}()
	}
}
