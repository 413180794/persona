package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// The todos side of the tasks the assistant hands out (internal/assistant, tasks.go): a task is a todo whose origin
// is the conversation. The assistant reaches the todos as an app (app.call todos, todosCall); the hub names the task
// on an interaction its agent raises (withTask) and puts the inbox's drafts about a task into its conversation
// (messageCardOf).

// todosCall answers the assistant's app.call todos.
func (h *Hub) todosCall(ctx context.Context, method string, params json.RawMessage) (any, error) {
	var a struct {
		TodoID    string    `json:"todo_id"`
		Title     string    `json:"title"`
		Notes     string    `json:"notes"`
		Origin    string    `json:"origin"`
		PlanID    string    `json:"plan_id"`
		MachineID string    `json:"machine_id"`
		SessionID string    `json:"session_id"`
		Cwd       string    `json:"cwd"`
		Reason    string    `json:"reason"`
		Work      time.Time `json:"work"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
	}
	switch method {
	case assistantwire.TodosLoad:
		return map[string]any{}, h.ensureTodosLoaded(ctx)
	case assistantwire.TodosCreate:
		t, err := h.createTodo(ctx, a.Title, a.Notes, a.Origin, a.PlanID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"todo": t}, nil
	case assistantwire.TodosLinkAgent:
		return map[string]any{}, h.linkTodoAgent(ctx, a.TodoID, a.MachineID, a.SessionID, a.Title, a.Cwd, "", "")
	case assistantwire.TodosFail:
		return map[string]any{}, h.failTodo(ctx, a.TodoID, a.Reason)
	case assistantwire.TodosAutoDone:
		ok, err := h.autoDoneTodo(ctx, a.TodoID, a.Reason, a.Work)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": ok}, nil
	case assistantwire.TodosGet:
		h.todos.mu.RLock()
		tp := h.todos.todos[a.TodoID]
		var t Todo
		if tp != nil {
			t = copyTodoLocked(tp)
		}
		h.todos.mu.RUnlock()
		return map[string]any{"todo": t, "ok": tp != nil}, nil
	case assistantwire.TodosList:
		h.todos.mu.RLock()
		list := []Todo{}
		for _, t := range h.todos.todos {
			if a.Origin == "*" && t.Origin != "" || a.Origin != "*" && t.Origin == a.Origin {
				list = append(list, copyTodoLocked(t))
			}
		}
		h.todos.mu.RUnlock()
		return map[string]any{"todos": list}, nil
	case assistantwire.TodosOfSession:
		t, ok := h.taskOf(a.MachineID, a.SessionID)
		return map[string]any{"todo": t, "ok": ok}, nil
	case assistantwire.TodosView:
		t, ok := h.todoNow(a.TodoID)
		if !ok {
			return map[string]any{"ok": false}, nil
		}
		return map[string]any{"todo": formatTodo(t), "ok": true}, nil
	case assistantwire.TodosRetry:
		return h.retryTodo(ctx, a.TodoID)
	}
	return nil, fmt.Errorf("app todos has no method %s", method)
}

// retryTodo reopens a task whose agent did not start, for its start again (todos.retry, the assistant's retryTask):
// its error cleared, the same todo; not ok when another hub sharing the store reopened it first.
func (h *Hub) retryTodo(ctx context.Context, id string) (any, error) {
	if err := h.ensureTodosLoaded(ctx); err != nil {
		return nil, err
	}
	cur, ok := h.todoNow(id)
	if !ok {
		return nil, db.ErrTodoNotFound
	}
	now := time.Now().UTC()
	var out Todo
	if h.store != nil {
		row, ok, err := h.store.RetryTodo(ctx, id, now)
		if err != nil {
			return nil, err
		}
		if !ok { // another hub sharing the store reopened it first
			cur, _ := h.todoNow(id)
			return map[string]any{"todo": formatTodo(cur), "ok": false}, nil
		}
		out, _ = h.applyTodoRow(id, *row, nil)
	} else {
		row := db.TodoRow{ID: cur.ID, Title: cur.Title, Notes: cur.Notes, CreatedAt: cur.CreatedAt, UpdatedAt: now,
			Origin: cur.Origin, PlanID: cur.PlanID, ReopenedAt: &now}
		out, _ = h.applyTodoRow(id, row, nil)
	}
	return map[string]any{"todo": formatTodo(out), "ok": true}, nil
}

// todoNow is a copy of todo id as the hub has it.
func (h *Hub) todoNow(id string) (Todo, bool) {
	h.todos.mu.RLock()
	defer h.todos.mu.RUnlock()
	t, ok := h.todos.todos[id]
	if !ok {
		return Todo{}, false
	}
	return copyTodoLocked(t), true
}

// taskOf is the task (a todo opened from an assistant conversation) the agent mid/sid works for.
func (h *Hub) taskOf(mid, sid string) (Todo, bool) {
	h.todos.mu.RLock()
	defer h.todos.mu.RUnlock()
	id, ok := h.todos.activeBySession[[2]string{mid, sid}]
	if !ok {
		return Todo{}, false
	}
	t := h.todos.todos[id]
	if t == nil || t.Origin == "" || t.DoneAt != nil {
		return Todo{}, false
	}
	return copyTodoLocked(t), true
}

// withTask names, on an interaction sent to clients, the task its agent works for (the watch and the cards show it).
func (h *Hub) withTask(ix proto.Interaction) proto.Interaction {
	if t, ok := h.taskOf(ix.MachineID, ix.SessionID); ok {
		ix.TaskTitle = t.Title
	}
	return ix
}

// failTodo ends a todo whose agent did not start: done, with why, and its agents no longer count for it.
func (h *Hub) failTodo(ctx context.Context, id, reason string) error {
	if err := h.ensureTodosLoaded(ctx); err != nil {
		return err
	}
	now := time.Now().UTC()
	doneAt := now
	if h.store != nil {
		row, err := h.store.FailTodo(ctx, id, reason, now)
		if err != nil {
			return err
		}
		if row.DoneAt != nil {
			doneAt = *row.DoneAt
		}
	}
	affected := map[string]bool{}
	h.todos.mu.Lock()
	t := h.todos.todos[id]
	if t != nil {
		if t.DoneAt == nil {
			t.DoneAt = &doneAt
		}
		t.StartError = reason
		t.UpdatedAt = now
		for _, a := range t.Agents {
			key := [2]string{a.MachineID, a.SessionID}
			if h.todos.activeBySession[key] == id {
				delete(h.todos.activeBySession, key)
				affected[a.MachineID] = true
			}
		}
	}
	h.todos.mu.Unlock()
	if t == nil {
		return db.ErrTodoNotFound
	}
	for mid := range affected {
		if m := h.localMachine(mid); m != nil {
			h.sessionsChanged(m)
		}
	}
	h.broadcast(map[string]any{"type": "todos"})
	return nil
}

// autoDoneTodo marks todo id done by the hub when it is still open and was not reopened after work began; false when
// the user got there first (done it, reopened it) or it is gone.
func (h *Hub) autoDoneTodo(ctx context.Context, id, reason string, work time.Time) (bool, error) {
	now := time.Now().UTC()
	var row db.TodoRow
	if h.store != nil {
		r, ok, err := h.store.AutoDoneTodo(ctx, id, reason, now, work)
		if errors.Is(err, db.ErrTodoNotFound) {
			return false, nil
		}
		if err != nil || !ok {
			return false, err
		}
		row = *r
	} else {
		h.todos.mu.RLock()
		t := h.todos.todos[id]
		if t != nil {
			row = db.TodoRow{ID: t.ID, Title: t.Title, Notes: t.Notes, CreatedAt: t.CreatedAt, Origin: t.Origin, PlanID: t.PlanID,
				ReopenedAt: t.ReopenedAt}
		}
		h.todos.mu.RUnlock()
		if t == nil {
			return false, nil
		}
		row.DoneAt, row.DoneBy, row.DoneReason, row.UpdatedAt = &now, db.DoneByAuto, reason, now
	}
	_, ok := h.applyTodoRow(id, row, func(t *Todo) bool {
		if t.UpdatedAt.After(row.UpdatedAt) { // the user changed it after the store did: theirs stands
			return false
		}
		return h.store != nil || t.DoneAt == nil && (t.ReopenedAt == nil || t.ReopenedAt.Before(work))
	})
	return ok, nil
}

// messageCardOf is the card for an item whose draft is about a task; nil when it is about none. The inbox is locked.
func messageCardOf(it *InboxItem) *assistantwire.MessageCard {
	if it.Agent == nil || it.Agent.TodoID == "" || it.Agent.Draft == "" {
		return nil
	}
	return &assistantwire.MessageCard{TodoID: it.Agent.TodoID, Source: it.Source, Account: it.AccountName, Chat: it.Title, Sender: it.Sender,
		Text: clip(strings.TrimSpace(it.Preview), 400), Draft: it.Agent.Draft}
}
