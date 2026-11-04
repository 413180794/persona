package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

type TodoSpace struct {
	MachineID string    `json:"machine_id"`
	Space     string    `json:"space"`
	LinkedAt  time.Time `json:"linked_at"`
}

type TodoAgent struct {
	MachineID   string     `json:"machine_id"`
	SessionID   string     `json:"session_id"`
	LinkedAt    time.Time  `json:"linked_at"`
	ReleasedAt  *time.Time `json:"released_at,omitempty"`
	Title       string     `json:"title"`
	Cwd         string     `json:"cwd"`
	Agent       string     `json:"agent"`
	StartError  string     `json:"start_error,omitempty"`
	Source      string     `json:"source"`
	Via         string     `json:"via,omitempty"`
	DismissedAt *time.Time `json:"dismissed_at,omitempty"`
}

type Todo struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Notes     string     `json:"notes"`
	DoneAt    *time.Time `json:"done_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	// Origin: the assistant conversation that opened it as a task; PlanID: the plan it came from (#930,
	// docs/unified-entry.md). Empty for a todo made by hand.
	Origin string `json:"origin,omitempty"`
	PlanID string `json:"plan_id,omitempty"`
	// StartError: a task whose agent did not start ends done with why (#1077); reopening clears it.
	StartError string `json:"start_error,omitempty"`
	// DoneBy is "auto" when the hub closed the task because its judge found the work finished (#1092,
	// docs/todos.md「自动完成」), with DoneReason; ReopenedAt: when the user last reopened it after it was done.
	DoneBy     string      `json:"done_by,omitempty"`
	DoneReason string      `json:"done_reason,omitempty"`
	ReopenedAt *time.Time  `json:"reopened_at,omitempty"`
	Spaces     []TodoSpace `json:"spaces"`
	Agents     []TodoAgent `json:"agents"`
	// Quiet: a task from the main conversation while assistant.main_tasks_quiet was on (#1731,
	// docs/unified-entry.md「安静的任务」): no pushes, Live Activity or task badges for its agents; the main
	// conversation is told instead. Set when it is made, never changed.
	Quiet bool `json:"quiet"`
}

type todosState struct {
	mu              sync.RWMutex
	loaded          bool
	todos           map[string]*Todo
	activeBySession map[[2]string]string // (machine_id, session_id) -> active todo_id
	snapshotSyncing map[[2]string]bool   // (machine_id, session_id) in flight
	spacesByKey     map[string]string    // "<machine_id>/<space>" -> todo_id
}

// spaceKey computes the unified space key string ("machineID/space").
func spaceKey(machineID, space string) string {
	if machineID == "" || space == "" {
		return ""
	}
	return machineID + "/" + space
}

// sessionSpaceKey computes the unified space key string for a session.
func sessionSpaceKey(machineID string, s proto.Session) string {
	space := s.SpaceName
	if space == "" {
		space = s.SpaceID
	}
	return spaceKey(machineID, space)
}

func newTodosState() todosState {
	return todosState{
		todos:           make(map[string]*Todo),
		activeBySession: make(map[[2]string]string),
		snapshotSyncing: make(map[[2]string]bool),
		spacesByKey:     make(map[string]string),
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
	if h.todos.spacesByKey == nil {
		h.todos.spacesByKey = make(map[string]string)
	}

	if h.store == nil {
		h.todos.loaded = true
		return nil
	}

	tlist, err := h.store.ListTodos(ctx)
	if err != nil {
		return err
	}
	slist, err := h.store.ListAllTodoSpaces(ctx)
	if err != nil {
		return err
	}
	alist, err := h.store.ListAllTodoAgents(ctx)
	if err != nil {
		return err
	}

	tmap := make(map[string]*Todo, len(tlist))
	active := make(map[[2]string]string)
	spaces := make(map[string]string)

	for _, tr := range tlist {
		tmap[tr.ID] = &Todo{
			ID:         tr.ID,
			Title:      tr.Title,
			Notes:      tr.Notes,
			DoneAt:     tr.DoneAt,
			CreatedAt:  tr.CreatedAt,
			UpdatedAt:  tr.UpdatedAt,
			Origin:     tr.Origin,
			PlanID:     tr.PlanID,
			StartError: tr.StartError,
			DoneBy:     tr.DoneBy,
			DoneReason: tr.DoneReason,
			ReopenedAt: tr.ReopenedAt,
			Quiet:      tr.Quiet,
			Spaces:     make([]TodoSpace, 0),
			Agents:     make([]TodoAgent, 0),
		}
	}

	for _, sr := range slist {
		if t, ok := tmap[sr.TodoID]; ok {
			t.Spaces = append(t.Spaces, TodoSpace{
				MachineID: sr.MachineID,
				Space:     sr.Space,
				LinkedAt:  sr.LinkedAt,
			})
			spaces[spaceKey(sr.MachineID, sr.Space)] = sr.TodoID
		}
	}

	for _, ar := range alist {
		t, ok := tmap[ar.TodoID]
		if !ok {
			continue
		}
		t.Agents = append(t.Agents, TodoAgent{
			MachineID:   ar.MachineID,
			SessionID:   ar.SessionID,
			LinkedAt:    ar.LinkedAt,
			ReleasedAt:  ar.ReleasedAt,
			Title:       ar.Title,
			Cwd:         ar.Cwd,
			Agent:       ar.Agent,
			StartError:  ar.StartError,
			Source:      ar.Source,
			Via:         ar.Via,
			DismissedAt: ar.DismissedAt,
		})
		if ar.ReleasedAt == nil && ar.DismissedAt == nil && t.DoneAt == nil {
			active[[2]string{ar.MachineID, ar.SessionID}] = t.ID
		}
	}

	h.todos.todos = tmap
	h.todos.activeBySession = active
	h.todos.spacesByKey = spaces
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

// createTodo makes a todo (origin and planID set when a plan from an assistant conversation opens it as a task)
// and tells the clients; the todos are loaded. A task from the main conversation is quiet while the user wants those
// told only there (mainTasksQuiet, #1731).
func (h *Hub) createTodo(ctx context.Context, title, notes, origin, planID string) (Todo, error) {
	now := time.Now().UTC()
	id := "todo_" + randomHex(8)
	quiet := origin != "" && h.isMain(ctx, origin) && h.mainTasksQuiet(ctx)
	if h.store != nil {
		row := db.TodoRow{ID: id, Title: title, Notes: notes, CreatedAt: now, UpdatedAt: now, Origin: origin, PlanID: planID, Quiet: quiet}
		if err := h.store.CreateTodo(ctx, row); err != nil {
			return Todo{}, err
		}
	}
	tObj := &Todo{ID: id, Title: title, Notes: notes, CreatedAt: now, UpdatedAt: now, Origin: origin, PlanID: planID, Quiet: quiet,
		Spaces: make([]TodoSpace, 0), Agents: make([]TodoAgent, 0)}
	h.todos.mu.Lock()
	h.todos.todos[id] = tObj
	out := formatTodo(copyTodoLocked(tObj))
	h.todos.mu.Unlock()
	h.broadcast(map[string]any{"type": "todos"})
	return out, nil
}

func copyTodoLocked(t *Todo) Todo {
	out := *t
	if t.DoneAt != nil {
		da := *t.DoneAt
		out.DoneAt = &da
	}
	if t.ReopenedAt != nil {
		ra := *t.ReopenedAt
		out.ReopenedAt = &ra
	}
	out.Spaces = make([]TodoSpace, len(t.Spaces))
	copy(out.Spaces, t.Spaces)
	out.Agents = make([]TodoAgent, len(t.Agents))
	for i, a := range t.Agents {
		out.Agents[i] = a
		if a.ReleasedAt != nil {
			ra := *a.ReleasedAt
			out.Agents[i].ReleasedAt = &ra
		}
		if a.DismissedAt != nil {
			da := *a.DismissedAt
			out.Agents[i].DismissedAt = &da
		}
	}
	return out
}

func formatTodo(t Todo) Todo {
	out := t
	if out.Spaces == nil {
		out.Spaces = []TodoSpace{}
	}
	out.Agents = make([]TodoAgent, 0, len(t.Agents))
	var curr, past []TodoAgent
	for _, a := range t.Agents {
		// Dismissed rows are not returned by the API (#762)
		if a.DismissedAt != nil {
			continue
		}
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
	h.todoDecisionRoutes(api)
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
		resTodo, err := h.createTodo(r.Context(), title, in.Notes, "", "")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
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
			h.todos.mu.RLock()
			updatedRow.StartError, updatedRow.DoneBy, updatedRow.DoneReason = cur.StartError, cur.DoneBy, cur.DoneReason
			updatedRow.ReopenedAt = cur.ReopenedAt
			h.todos.mu.RUnlock()
			if in.Done != nil {
				if *in.Done {
					if updatedRow.DoneAt == nil {
						updatedRow.DoneAt = &now
						updatedRow.DoneBy, updatedRow.DoneReason = "", ""
					}
				} else {
					if updatedRow.DoneAt != nil {
						updatedRow.ReopenedAt = &now
					}
					updatedRow.DoneAt = nil
					updatedRow.StartError, updatedRow.DoneBy, updatedRow.DoneReason = "", "", ""
				}
			}
		}

		resTodo, _ := h.applyTodoRow(id, *updatedRow, nil)
		writeJSON(w, map[string]any{"todo": resTodo})
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

		affectedMachines := make(map[string]bool)
		h.todos.mu.Lock()
		if cur, ok := h.todos.todos[id]; ok {
			for _, a := range cur.Agents {
				if a.ReleasedAt == nil && a.DismissedAt == nil {
					delete(h.todos.activeBySession, [2]string{a.MachineID, a.SessionID})
					affectedMachines[a.MachineID] = true
				}
			}
			for _, sp := range cur.Spaces {
				delete(h.todos.spacesByKey, sp.MachineID+"/"+sp.Space)
				affectedMachines[sp.MachineID] = true
			}
			delete(h.todos.todos, id)
		}
		h.todos.mu.Unlock()

		for mid := range affectedMachines {
			if m := h.localMachine(mid); m != nil {
				h.sessionsChanged(m)
			}
		}
		h.broadcast(map[string]any{"type": "todos"})
		writeJSON(w, map[string]any{"ok": true})
	})

	// PUT /api/v1/todos/{id}/spaces/{mid}/{space...}: link whole space to todo
	api.HandleFunc("PUT /api/v1/todos/{id}/spaces/{mid}/{space...}", func(w http.ResponseWriter, r *http.Request) {
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
		space := r.PathValue("space")
		if space == "" {
			writeErr(w, http.StatusBadRequest, errors.New("space is required"))
			return
		}

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

		linkedCount, err := h.linkTodoSpace(r.Context(), todoID, mid, space)
		if err != nil {
			if errors.Is(err, db.ErrTodoNotFound) {
				writeErr(w, http.StatusNotFound, err)
			} else if errors.Is(err, db.ErrTodoCompleted) {
				writeErr(w, http.StatusConflict, err)
			} else {
				writeErr(w, http.StatusInternalServerError, err)
			}
			return
		}

		writeJSON(w, map[string]any{"ok": true, "linked": linkedCount})
	})

	// DELETE /api/v1/todos/{id}/spaces/{mid}/{space...}: unlink space from todo
	api.HandleFunc("DELETE /api/v1/todos/{id}/spaces/{mid}/{space...}", func(w http.ResponseWriter, r *http.Request) {
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
		space := r.PathValue("space")

		h.todos.mu.RLock()
		_, ok := h.todos.todos[todoID]
		h.todos.mu.RUnlock()
		if !ok {
			writeErr(w, http.StatusNotFound, errors.New("todo not found"))
			return
		}

		if err := h.unlinkTodoSpace(r.Context(), todoID, mid, space); err != nil {
			if errors.Is(err, db.ErrTodoNotFound) {
				writeErr(w, http.StatusNotFound, err)
			} else {
				writeErr(w, http.StatusInternalServerError, err)
			}
			return
		}

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
		if m := h.localMachine(mid); m != nil {
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

	// DELETE /api/v1/todos/{id}/agents/{mid}/{sid}: unlink agent (soft unlink: dismissed_at + released_at #762)
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
		target, ok := h.todos.todos[todoID]
		if !ok {
			h.todos.mu.RUnlock()
			writeErr(w, http.StatusNotFound, errors.New("todo not found"))
			return
		}
		agentFound := false
		for _, a := range target.Agents {
			if a.MachineID == mid && a.SessionID == sid {
				agentFound = true
				break
			}
		}
		h.todos.mu.RUnlock()
		if !agentFound {
			writeErr(w, http.StatusNotFound, errors.New("agent link not found"))
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
			for i := range target.Agents {
				if target.Agents[i].MachineID == mid && target.Agents[i].SessionID == sid {
					if target.Agents[i].ReleasedAt == nil {
						wasActive = true
					}
					target.Agents[i].ReleasedAt = &now
					target.Agents[i].DismissedAt = &now
					break
				}
			}
			target.UpdatedAt = now
		}
		if wasActive {
			if curTid, ok := h.todos.activeBySession[[2]string{mid, sid}]; ok && curTid == todoID {
				delete(h.todos.activeBySession, [2]string{mid, sid})
			}
		}
		h.todos.mu.Unlock()

		if m := h.localMachine(mid); m != nil {
			h.sessionsChanged(m)
		}
		h.broadcast(map[string]any{"type": "todos"})
		writeJSON(w, map[string]any{"ok": true})
	})
}

// linkTodoSpace links a space to todoID and attaches all current online agents in that space.
func (h *Hub) linkTodoSpace(ctx context.Context, todoID, mid, space string) (int, error) {
	now := time.Now().UTC()
	spKey := spaceKey(mid, space)

	var oldTodoID string
	if h.store != nil {
		var err error
		oldTodoID, err = h.store.LinkTodoSpace(ctx, todoID, mid, space, now)
		if err != nil {
			return 0, err
		}
	} else {
		h.todos.mu.RLock()
		t, ok := h.todos.todos[todoID]
		h.todos.mu.RUnlock()
		if !ok {
			return 0, db.ErrTodoNotFound
		}
		if t.DoneAt != nil {
			return 0, db.ErrTodoCompleted
		}
		if oldID, ok := h.todos.spacesByKey[spKey]; ok && oldID != todoID {
			oldTodoID = oldID
		}
	}

	h.todos.mu.Lock()
	// Clean up from old todo if space belonged to another todo
	if oldTodoID != "" && oldTodoID != todoID {
		if oldT, ok := h.todos.todos[oldTodoID]; ok {
			var keptSpaces []TodoSpace
			for _, sp := range oldT.Spaces {
				if !(sp.MachineID == mid && sp.Space == space) {
					keptSpaces = append(keptSpaces, sp)
				}
			}
			oldT.Spaces = keptSpaces
			var keptAgents []TodoAgent
			for _, a := range oldT.Agents {
				if a.Source == "space" && a.Via == spKey {
					delete(h.todos.activeBySession, [2]string{a.MachineID, a.SessionID})
					continue
				}
				keptAgents = append(keptAgents, a)
			}
			oldT.Agents = keptAgents
			oldT.UpdatedAt = now
		}
	}

	// Add or update in target todo
	target := h.todos.todos[todoID]
	if target != nil {
		spFound := false
		for i := range target.Spaces {
			if target.Spaces[i].MachineID == mid && target.Spaces[i].Space == space {
				target.Spaces[i].LinkedAt = now
				spFound = true
				break
			}
		}
		if !spFound {
			target.Spaces = append(target.Spaces, TodoSpace{
				MachineID: mid,
				Space:     space,
				LinkedAt:  now,
			})
		}
		h.todos.spacesByKey[spKey] = todoID
		target.UpdatedAt = now
	}
	h.todos.mu.Unlock()

	// Link all online agents currently in this space
	linkedCount := 0
	if m := h.localMachine(mid); m != nil {
		sessions := m.sessionList()
		for _, s := range sessions {
			if sessionSpaceKey(mid, s) == spKey && s.Status != proto.StatusOffline {
				h.todos.mu.RLock()
				curTodoID := h.todos.activeBySession[[2]string{mid, s.SessionID}]
				h.todos.mu.RUnlock()
				if curTodoID == todoID {
					// Keep existing active link untouched (manual or spawned, #873)
					continue
				}
				// Link to target todo
				if err := h.linkTodoAgentWithSource(ctx, todoID, mid, s.SessionID, s.Title, s.Cwd, s.Agent, s.StartError, "space", spKey); err == nil {
					linkedCount++
				}
			}
		}
		h.sessionsChanged(m)
	}

	if oldTodoID != "" {
		if m := h.localMachine(mid); m != nil {
			h.sessionsChanged(m)
		}
	}

	h.broadcast(map[string]any{"type": "todos"})
	return linkedCount, nil
}

// applyTodoRow puts a todo's row as it now is in the store into memory and lets everything that follows from done
// happen as for the user's PATCH: its agents' links count again or no longer do (sessions lose or regain the todo,
// the nodes' persona_todo tokens follow on their next sync), and the clients refetch. guard, when given, is checked
// under the lock first (false: nothing changes); false also when the todo is gone.
func (h *Hub) applyTodoRow(id string, row db.TodoRow, guard func(*Todo) bool) (Todo, bool) {
	affectedMachines := make(map[string]bool)
	h.todos.mu.Lock()
	t, ok := h.todos.todos[id]
	if !ok || guard != nil && !guard(t) {
		h.todos.mu.Unlock()
		return Todo{}, false
	}
	t.Title = row.Title
	t.Notes = row.Notes
	t.DoneAt = row.DoneAt
	t.StartError = row.StartError
	t.DoneBy, t.DoneReason, t.ReopenedAt = row.DoneBy, row.DoneReason, row.ReopenedAt
	t.UpdatedAt = row.UpdatedAt
	for _, a := range t.Agents {
		if a.ReleasedAt == nil && a.DismissedAt == nil {
			key := [2]string{a.MachineID, a.SessionID}
			affectedMachines[a.MachineID] = true
			if t.DoneAt != nil {
				delete(h.todos.activeBySession, key)
			} else {
				h.todos.activeBySession[key] = t.ID
			}
		}
	}
	out := formatTodo(copyTodoLocked(t))
	h.todos.mu.Unlock()

	for mid := range affectedMachines {
		if m := h.localMachine(mid); m != nil {
			h.sessionsChanged(m)
		}
	}
	h.broadcast(map[string]any{"type": "todos"})
	return out, true
}

// unlinkTodoSpace unlinks a space from todoID and physically deletes source=space agents linked via this space.
func (h *Hub) unlinkTodoSpace(ctx context.Context, todoID, mid, space string) error {
	spKey := spaceKey(mid, space)

	h.todos.mu.RLock()
	t, ok := h.todos.todos[todoID]
	if !ok {
		h.todos.mu.RUnlock()
		return db.ErrTodoNotFound
	}
	spFound := false
	for _, sp := range t.Spaces {
		if sp.MachineID == mid && sp.Space == space {
			spFound = true
			break
		}
	}
	h.todos.mu.RUnlock()
	if !spFound {
		return db.ErrTodoNotFound
	}

	if h.store != nil {
		if err := h.store.UnlinkTodoSpace(ctx, todoID, mid, space); err != nil {
			return err
		}
	}

	now := time.Now().UTC()
	h.todos.mu.Lock()
	if cur, ok := h.todos.todos[todoID]; ok {
		var keptSpaces []TodoSpace
		for _, sp := range cur.Spaces {
			if !(sp.MachineID == mid && sp.Space == space) {
				keptSpaces = append(keptSpaces, sp)
			}
		}
		cur.Spaces = keptSpaces
		delete(h.todos.spacesByKey, spKey)

		var keptAgents []TodoAgent
		for _, a := range cur.Agents {
			if a.Source == "space" && a.Via == spKey {
				if actTid, ok := h.todos.activeBySession[[2]string{a.MachineID, a.SessionID}]; ok && actTid == todoID {
					delete(h.todos.activeBySession, [2]string{a.MachineID, a.SessionID})
				}
				continue
			}
			keptAgents = append(keptAgents, a)
		}
		cur.Agents = keptAgents
		cur.UpdatedAt = now
	}
	h.todos.mu.Unlock()

	if m := h.localMachine(mid); m != nil {
		h.sessionsChanged(m)
	}
	h.broadcast(map[string]any{"type": "todos"})
	return nil
}

// renameTodoSpaceInMemory carries todo_spaces and agent via over when space is renamed.
func (h *Hub) renameTodoSpaceInMemory(machineID, oldSpace, newSpace string) {
	oldKey := spaceKey(machineID, oldSpace)
	newKey := spaceKey(machineID, newSpace)
	now := time.Now().UTC()

	h.todos.mu.Lock()
	defer h.todos.mu.Unlock()

	if tid, ok := h.todos.spacesByKey[oldKey]; ok {
		delete(h.todos.spacesByKey, oldKey)
		h.todos.spacesByKey[newKey] = tid
		if t, ok := h.todos.todos[tid]; ok {
			for i := range t.Spaces {
				if t.Spaces[i].MachineID == machineID && t.Spaces[i].Space == oldSpace {
					t.Spaces[i].Space = newSpace
					break
				}
			}
			for i := range t.Agents {
				if t.Agents[i].Source == "space" && t.Agents[i].Via == oldKey {
					t.Agents[i].Via = newKey
				}
			}
			t.UpdatedAt = now
		}
	}
}

// voidTodoSpaceInMemory cleans up a space link and its space-derived agents from memory when voided due to unique conflict (#873).
func (h *Hub) voidTodoSpaceInMemory(machineID, oldSpace string) {
	oldKey := spaceKey(machineID, oldSpace)
	now := time.Now().UTC()

	h.todos.mu.Lock()
	defer h.todos.mu.Unlock()

	if tid, ok := h.todos.spacesByKey[oldKey]; ok {
		delete(h.todos.spacesByKey, oldKey)
		if t, ok := h.todos.todos[tid]; ok {
			var keptSpaces []TodoSpace
			for _, sp := range t.Spaces {
				if !(sp.MachineID == machineID && sp.Space == oldSpace) {
					keptSpaces = append(keptSpaces, sp)
				}
			}
			t.Spaces = keptSpaces
			var keptAgents []TodoAgent
			for _, a := range t.Agents {
				if a.Source == "space" && a.Via == oldKey {
					if actTid, ok := h.todos.activeBySession[[2]string{a.MachineID, a.SessionID}]; ok && actTid == tid {
						delete(h.todos.activeBySession, [2]string{a.MachineID, a.SessionID})
					}
					continue
				}
				keptAgents = append(keptAgents, a)
			}
			t.Agents = keptAgents
			t.UpdatedAt = now
		}
	}
}

// linkTodoAgent links or reactivates an agent manually on todoID.
func (h *Hub) linkTodoAgent(ctx context.Context, todoID, mid, sid, title, cwd, agent, startError string) error {
	return h.linkTodoAgentWithSource(ctx, todoID, mid, sid, title, cwd, agent, startError, "manual", "")
}

// linkTodoAgentWithSource links an agent with a specific source ("manual", "space", "spawned") and via.
func (h *Hub) linkTodoAgentWithSource(ctx context.Context, todoID, mid, sid, title, cwd, agent, startError, source, via string) error {
	if err := h.ensureTodosLoaded(ctx); err != nil {
		return err
	}
	now := time.Now().UTC()
	var releasedOtherTodoIDs []string

	if h.store != nil {
		var err error
		releasedOtherTodoIDs, err = h.store.LinkTodoAgentWithSource(ctx, todoID, mid, sid, title, cwd, agent, startError, source, via, now)
		if err != nil {
			return err
		}
	} else {
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
	// F3: mark active rows in all other todos (including completed) as released
	for oid, other := range h.todos.todos {
		if oid != todoID {
			for i := range other.Agents {
				if other.Agents[i].MachineID == mid && other.Agents[i].SessionID == sid && other.Agents[i].ReleasedAt == nil {
					other.Agents[i].ReleasedAt = &now
					other.UpdatedAt = now
				}
			}
		}
	}

	if target, ok := h.todos.todos[todoID]; ok {
		found := false
		for i := range target.Agents {
			if target.Agents[i].MachineID == mid && target.Agents[i].SessionID == sid {
				target.Agents[i].ReleasedAt = nil
				target.Agents[i].DismissedAt = nil
				target.Agents[i].Source = source
				target.Agents[i].Via = via
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
				MachineID:   mid,
				SessionID:   sid,
				LinkedAt:    now,
				ReleasedAt:  nil,
				DismissedAt: nil,
				Title:       title,
				Cwd:         cwd,
				Agent:       agent,
				StartError:  startError,
				Source:      source,
				Via:         via,
			})
		}
		target.UpdatedAt = now
		h.todos.activeBySession[[2]string{mid, sid}] = todoID
	}
	h.todos.mu.Unlock()

	// F1: notify sessionsChanged on affected computer and broadcast todos
	if m := h.localMachine(mid); m != nil {
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

// pendingAutoLink is a link queued for async database persistence.
type pendingAutoLink struct {
	todoID     string
	machineID  string
	sessionID  string
	title      string
	cwd        string
	agent      string
	startError string
	source     string
	via        string
	linkedAt   time.Time
}

// autoLinkTodos checks sessions for automatic linking (by space and spawned derivation)
// and updates the memory index synchronously BEFORE decorated() generates session list.
func (h *Hub) autoLinkTodos(m *Machine) {
	h.todos.mu.RLock()
	loaded := h.todos.loaded
	h.todos.mu.RUnlock()
	if !loaded {
		return
	}

	sessions := m.sessionList()
	now := time.Now().UTC()

	var pending []pendingAutoLink
	anyLinked := false

	h.todos.mu.Lock()
	for _, s := range sessions {
		if s.Status == proto.StatusOffline {
			continue
		}
		key := [2]string{m.cfg.ID, s.SessionID}

		// 1. Spawned derivation
		if s.SpawnFrom != "" {
			parts := strings.SplitN(s.SpawnFrom, "@", 2)
			if len(parts) == 2 {
				targetTodoID := parts[0]
				parentRef := parts[1] // parent <mid>/<sid>
				if target, ok := h.todos.todos[targetTodoID]; ok && target.DoneAt == nil {
					// Only link child if it has no active link currently
					if h.todos.activeBySession[key] == "" {
						// Check if any row for this (machine, session) exists in target todo (active, released, or dismissed)
						hasAny := false
						for _, a := range target.Agents {
							if a.MachineID == m.cfg.ID && a.SessionID == s.SessionID {
								hasAny = true
								break
							}
						}
						if !hasAny {
							target.Agents = append(target.Agents, TodoAgent{
								MachineID:  m.cfg.ID,
								SessionID:  s.SessionID,
								LinkedAt:   now,
								Title:      s.Title,
								Cwd:        s.Cwd,
								Agent:      s.Agent,
								StartError: s.StartError,
								Source:     "spawned",
								Via:        parentRef,
							})
							h.todos.activeBySession[key] = targetTodoID
							target.UpdatedAt = now
							pending = append(pending, pendingAutoLink{
								todoID:     targetTodoID,
								machineID:  m.cfg.ID,
								sessionID:  s.SessionID,
								title:      s.Title,
								cwd:        s.Cwd,
								agent:      s.Agent,
								startError: s.StartError,
								source:     "spawned",
								via:        parentRef,
								linkedAt:   now,
							})
							anyLinked = true
						}
					}
				}
			}
		}

		// 2. Space auto link
		if h.todos.activeBySession[key] == "" {
			spKey := sessionSpaceKey(m.cfg.ID, s)
			if spKey != "" {
				if targetTodoID, ok := h.todos.spacesByKey[spKey]; ok {
					if target, ok := h.todos.todos[targetTodoID]; ok && target.DoneAt == nil {
						hasAny := false
						for _, a := range target.Agents {
							if a.MachineID == m.cfg.ID && a.SessionID == s.SessionID {
								hasAny = true
								break
							}
						}
						if !hasAny {
							target.Agents = append(target.Agents, TodoAgent{
								MachineID:  m.cfg.ID,
								SessionID:  s.SessionID,
								LinkedAt:   now,
								Title:      s.Title,
								Cwd:        s.Cwd,
								Agent:      s.Agent,
								StartError: s.StartError,
								Source:     "space",
								Via:        spKey,
							})
							h.todos.activeBySession[key] = targetTodoID
							target.UpdatedAt = now
							pending = append(pending, pendingAutoLink{
								todoID:     targetTodoID,
								machineID:  m.cfg.ID,
								sessionID:  s.SessionID,
								title:      s.Title,
								cwd:        s.Cwd,
								agent:      s.Agent,
								startError: s.StartError,
								source:     "space",
								via:        spKey,
								linkedAt:   now,
							})
							anyLinked = true
						}
					}
				}
			}
		}
	}
	h.todos.mu.Unlock()

	if len(pending) > 0 && h.store != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			for _, item := range pending {
				if _, _, err := h.store.AutoLinkTodoAgent(ctx, item.todoID, item.machineID, item.sessionID, item.title, item.cwd, item.agent, item.startError, item.source, item.via, item.linkedAt); err != nil {
					h.logf("todos: auto-link db write failed for %s on %s/%s, rolling back: %v", item.todoID, item.machineID, item.sessionID, err)
					h.todos.mu.Lock()
					key := [2]string{item.machineID, item.sessionID}
					if curTid, ok := h.todos.activeBySession[key]; ok && curTid == item.todoID {
						delete(h.todos.activeBySession, key)
						if target, ok := h.todos.todos[item.todoID]; ok {
							var kept []TodoAgent
							for _, a := range target.Agents {
								if a.MachineID == item.machineID && a.SessionID == item.sessionID && a.LinkedAt.Equal(item.linkedAt) {
									continue
								}
								kept = append(kept, a)
							}
							target.Agents = kept
							target.UpdatedAt = time.Now().UTC()
						}
					}
					h.todos.mu.Unlock()
					h.broadcast(map[string]any{"type": "todos"})
				}
			}
		}()
	}

	if anyLinked {
		h.broadcast(map[string]any{"type": "todos"})
	}
}

func sameTokens(a, b map[string]map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for id, m1 := range a {
		m2, ok := b[id]
		if !ok || len(m1) != len(m2) {
			return false
		}
		for k, v1 := range m1 {
			if v2, ok := m2[k]; !ok || v1 != v2 {
				return false
			}
		}
	}
	return true
}

func cloneTokens(m map[string]map[string]string) map[string]map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]map[string]string, len(m))
	for id, tokens := range m {
		inner := make(map[string]string, len(tokens))
		for k, v := range tokens {
			inner[k] = v
		}
		out[id] = inner
	}
	return out
}

// computeExpectedTokens calculates expected tokens keyed by session ID for online sessions (#873).
func (h *Hub) computeExpectedTokens(m *Machine) map[string]map[string]string {
	h.todos.mu.RLock()
	defer h.todos.mu.RUnlock()
	expected := make(map[string]map[string]string)
	for key, todoID := range h.todos.activeBySession {
		if key[0] == m.cfg.ID {
			if t, ok := h.todos.todos[todoID]; ok && t.DoneAt == nil {
				sessionID := key[1]
				s, ok := m.session(sessionID)
				if !ok || s.Status == proto.StatusOffline || s.PaneID == "" {
					continue
				}
				// F4: only herdr host panes receive metadata tokens
				if s.SpaceKind != "" && s.SpaceKind != host.Herdr {
					continue
				}
				ref := host.ParsePane(s.PaneID)
				if ref.Kind != host.Herdr {
					continue
				}
				expected[sessionID] = map[string]string{
					"persona_todo":     t.Title,
					"persona_todo_ref": fmt.Sprintf("%s@%s/%s", t.ID, m.cfg.ID, sessionID),
				}
			}
		}
	}
	return expected
}

// syncTodoTokensToNode sends expected tokens snapshot to node serialized per machine (#873).
func (h *Hub) syncTodoTokensToNode(m *Machine) {
	if m == nil {
		return
	}

	m.todoTokensMu.Lock()
	m.todoTokensDirty = true
	if m.todoTokensSending {
		m.todoTokensMu.Unlock()
		return
	}
	m.todoTokensSending = true
	m.todoTokensMu.Unlock()

	go func() {
		for {
			m.todoTokensMu.Lock()
			if !m.todoTokensDirty {
				m.todoTokensSending = false
				m.todoTokensMu.Unlock()
				return
			}
			m.todoTokensDirty = false
			m.todoTokensMu.Unlock()

			expected := h.computeExpectedTokens(m)

			m.todoTokensMu.Lock()
			if sameTokens(m.lastTodoTokens, expected) {
				m.todoTokensMu.Unlock()
				continue
			}
			m.todoTokensMu.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := m.call(ctx, "todo.sync_session_tokens", map[string]any{"tokens": expected}, nil)
			cancel()

			m.todoTokensMu.Lock()
			if err != nil {
				h.logf("todos: sync tokens to %s failed: %v", m.cfg.ID, err)
				m.lastTodoTokens = nil
			} else {
				m.lastTodoTokens = cloneTokens(expected)
			}
			m.todoTokensMu.Unlock()
		}
	}()
}

// checkAndUpdateTodoSnapshots checks if any active agent's title or start_error changed and updates db asynchronously.
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
							t.UpdatedAt = time.Now().UTC()
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
