package extmgr

import (
	"encoding/json"
	"slices"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/extension"
)

// todo.changed and todo.decision: the assistant tells of its todos (assistant/todo_events.go); the subscribers granted
// todos.read are told (extension.EventScopes, emit). Only the fields of the typed events are passed on. They have a
// queue of their own (ps.todoEvents), and what nobody is going to be told is not queued at all.
func init() {
	onAssistantEvent(assistantwire.EventTodoChanged, func(h *Manager, raw json.RawMessage) {
		var p assistantwire.TodoChangedEvent
		if json.Unmarshal(raw, &p) != nil || p.ID == "" ||
			!slices.Contains([]string{assistantwire.TodoCreated, assistantwire.TodoUpdated, assistantwire.TodoDeleted}, p.What) {
			return
		}
		if h.extensions.todoWanted(assistantwire.EventTodoChanged) {
			h.extensions.queueOn(&h.extensions.todoEvents, changedNotice(p))
		}
	})
	onAssistantEvent(assistantwire.EventTodoDecision, func(h *Manager, raw json.RawMessage) {
		var p assistantwire.TodoDecisionEvent
		if json.Unmarshal(raw, &p) != nil || p.ID == "" || p.Decision.ID == "" ||
			!slices.Contains([]string{db.DecisionManual, db.DecisionCard, db.DecisionOutcome, db.DecisionAgent, db.DecisionThread}, p.Decision.Source) {
			return
		}
		if h.extensions.todoWanted(assistantwire.EventTodoDecision) {
			h.extensions.queueOn(&h.extensions.todoEvents, extensionNotice{topic: assistantwire.EventTodoDecision, payload: fieldsOf(p)})
		}
	})
}

// todoWanted says whether anyone is going to be told topic: an extension subscribed and granted its scope. (emit
// checks again when it sends; an extension that came in between is told of the next one.) With no registry it says yes.
func (ps *extensions) todoWanted(topic string) bool {
	if ps == nil {
		return false
	}
	return ps.reg == nil || len(ps.reg.EventSubscribers(topic, extension.EventScopes[topic])) > 0
}

// changedNotice is a todo.changed with exactly the typed event's fields.
func changedNotice(p assistantwire.TodoChangedEvent) extensionNotice {
	return extensionNotice{topic: assistantwire.EventTodoChanged, payload: fieldsOf(p), changed: &p}
}

// fieldsOf is v's JSON fields as the extensions are told them, nothing else.
func fieldsOf(v any) map[string]any {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	return fields
}
