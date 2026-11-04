package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// A todo's decision record (#1618, docs/todos.md「决策记录」): the calls the user made on it, written by hand in the
// todo or from their answers to the question cards of the agents working on it, and handed to every agent the todo is
// given to.

const decisionTextMax = 4000 // a question, a choice or a reason

var errNoDecisionStore = errors.New("decisions need the hub's database")

func (h *Hub) todoDecisionRoutes(api *http.ServeMux) {
	// GET /api/v1/todos/{id}/decisions[?all=1]: the decisions in effect, oldest first; all=1 adds the superseded ones.
	api.HandleFunc("GET /api/v1/todos/{id}/decisions", func(w http.ResponseWriter, r *http.Request) {
		id, ok := h.decisionTodo(w, r)
		if !ok {
			return
		}
		all := r.URL.Query().Get("all")
		list, err := h.store.ListTodoDecisions(r.Context(), id, all == "1" || all == "true")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]any{"decisions": list})
	})

	// POST /api/v1/todos/{id}/decisions: write one by hand {question, choice, reason?, supersedes_id?}.
	api.HandleFunc("POST /api/v1/todos/{id}/decisions", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot modify todos"))
			return
		}
		id, ok := h.decisionTodo(w, r)
		if !ok {
			return
		}
		var in struct {
			Question     string `json:"question"`
			Choice       string `json:"choice"`
			Reason       string `json:"reason"`
			SupersedesID string `json:"supersedes_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		row := db.TodoDecisionRow{ID: "dec_" + randomHex(8), TodoID: id, Question: decisionText(in.Question),
			Choice: decisionText(in.Choice), Reason: decisionText(in.Reason), Source: db.DecisionManual,
			SupersedesID: strings.TrimSpace(in.SupersedesID), CreatedAt: decisionNow()}
		if row.Question == "" || row.Choice == "" {
			writeErr(w, http.StatusBadRequest, errors.New("question and choice cannot be empty"))
			return
		}
		if _, err := h.store.InsertTodoDecision(r.Context(), row); err != nil {
			decisionErr(w, err)
			return
		}
		h.decisionsChanged(id, row.CreatedAt)
		writeJSON(w, map[string]any{"decision": row})
	})

	// PATCH /api/v1/todos/{id}/decisions/{did}: {supersedes_id}: mark it as replacing an earlier decision ("" undoes it).
	api.HandleFunc("PATCH /api/v1/todos/{id}/decisions/{did}", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot modify todos"))
			return
		}
		id, ok := h.decisionTodo(w, r)
		if !ok {
			return
		}
		var in struct {
			SupersedesID *string `json:"supersedes_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if in.SupersedesID == nil {
			writeErr(w, http.StatusBadRequest, errors.New("supersedes_id is required"))
			return
		}
		at := decisionNow()
		row, err := h.store.SetTodoDecisionSupersedes(r.Context(), id, r.PathValue("did"), strings.TrimSpace(*in.SupersedesID), at)
		if err != nil {
			decisionErr(w, err)
			return
		}
		h.decisionsChanged(id, at)
		writeJSON(w, map[string]any{"decision": row})
	})
}

// decisionTodo is the request's todo id once the todos are loaded and it is one of them; otherwise it answers.
func (h *Hub) decisionTodo(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.store == nil {
		writeErr(w, http.StatusServiceUnavailable, errNoDecisionStore)
		return "", false
	}
	if err := h.ensureTodosLoaded(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("todos service unavailable"))
		return "", false
	}
	id := r.PathValue("id")
	h.todos.mu.RLock()
	_, ok := h.todos.todos[id]
	h.todos.mu.RUnlock()
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("todo not found"))
		return "", false
	}
	return id, true
}

func decisionErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrTodoNotFound), errors.Is(err, db.ErrDecisionNotFound):
		writeErr(w, http.StatusNotFound, err)
	case errors.Is(err, db.ErrDecisionSupersede):
		writeErr(w, http.StatusBadRequest, err)
	default:
		writeErr(w, http.StatusInternalServerError, err)
	}
}

// decisionNow is now as Postgres keeps it (microseconds), so the order checks compare what was stored.
func decisionNow() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// decisionText is a decision's text as kept: trimmed, at most decisionTextMax runes.
func decisionText(s string) string { return clip(strings.TrimSpace(s), decisionTextMax) }

// cardDecisions: the user answered a question card; when its agent works on a todo, each answered question is a
// decision of that todo. Only Claude Code's option questions (AskUserQuestion) count: approvals and permissions are
// not decisions about the work, and a dismissed or failed card answered nothing.
func (h *Hub) cardDecisions(ix proto.Interaction, r proto.Resolution, status string) {
	if h.store == nil || status != "resolved" || r.Dismiss || ix.Kind != proto.IxQuestion || len(r.Answers) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Which todo the agent works on comes from the index the hub keeps of them (loaded at start; again here when that
	// failed, as the todo routes do).
	if err := h.ensureTodosLoaded(ctx); err != nil {
		h.logf("decisions: card %s: todos not loaded: %v", ix.ID, err)
		return
	}
	h.todos.mu.RLock()
	todoID := h.todos.activeBySession[[2]string{ix.MachineID, ix.SessionID}]
	h.todos.mu.RUnlock()
	if todoID == "" {
		return
	}
	at := decisionNow()
	wrote, last := false, at
	for i, a := range proto.QuestionAnswers(ix, r) {
		q, c := decisionText(a.Question), decisionText(a.Answer)
		if q == "" || c == "" {
			continue
		}
		// A microsecond apart, so they list in the card's order (the list orders by time, then by the random id).
		at := at.Add(time.Duration(i) * time.Microsecond)
		ok, err := h.store.InsertTodoDecision(ctx, db.TodoDecisionRow{ID: "dec_" + randomHex(8), TodoID: todoID,
			Question: q, Choice: c, Source: db.DecisionCard, SourceMachine: ix.MachineID, SourceSession: ix.SessionID,
			SourceMessage: ix.ID, CreatedAt: at})
		if err != nil {
			h.logf("decisions: todo %s, card %s: %v", todoID, ix.ID, err)
			return
		}
		if ok {
			wrote, last = true, at
		}
	}
	if wrote {
		h.decisionsChanged(todoID, last)
	}
}

// decisionsChanged: the store bumped the todo's updated_at with its decisions; the cached todo follows, and the
// clients refetch (they reload a todo's decisions when its updated_at moves).
func (h *Hub) decisionsChanged(todoID string, at time.Time) {
	h.todos.mu.Lock()
	if t, ok := h.todos.todos[todoID]; ok && at.After(t.UpdatedAt) {
		t.UpdatedAt = at
	}
	h.todos.mu.Unlock()
	h.broadcast(map[string]any{"type": "todos"})
}

// todoDecisionsContext is the <jarvis-context> of a todo's decisions in effect that goes before what is handed to an
// agent for it; empty when it has none or they cannot be read (the handoff goes on without them).
func (h *Hub) todoDecisionsContext(ctx context.Context, todoID string) string {
	if h.store == nil || todoID == "" {
		return ""
	}
	list, err := h.store.ListTodoDecisions(ctx, todoID, false)
	if err != nil {
		h.logf("decisions: todo %s: %v", todoID, err)
		return ""
	}
	if len(list) == 0 {
		return ""
	}
	var title string
	agents := map[[2]string]string{}
	h.todos.mu.RLock()
	if t, ok := h.todos.todos[todoID]; ok {
		title = t.Title
		for _, a := range t.Agents {
			agents[[2]string{a.MachineID, a.SessionID}] = a.Title
		}
	}
	h.todos.mu.RUnlock()
	loc := h.userLocation()
	d := prompts.TodoDecisions{Title: contextText(title)}
	for i, r := range list {
		from := ""
		if r.Source == db.DecisionCard {
			from = "在 Agent 的提问里答的"
			if t := strings.TrimSpace(agents[[2]string{r.SourceMachine, r.SourceSession}]); t != "" {
				from = "在「" + contextText(oneLine(t)) + "」的提问里答的"
			}
		}
		d.Decisions = append(d.Decisions, prompts.TodoDecision{N: i + 1, Question: contextText(r.Question),
			Choice: contextText(r.Choice), Reason: contextText(r.Reason), When: r.CreatedAt.In(loc).Format("01-02 15:04"), From: from})
	}
	return prompts.RenderTodoDecisions(d)
}

// closingTag is anything a reader of the message could take for the end of a <jarvis-context> (the apps match it
// loosely: case, spaces).
var closingTag = regexp.MustCompile(`(?i)<\s*/\s*jarvis-context\s*>`)

// contextText keeps a text from closing the <jarvis-context> it is put in (also when taking one out leaves another,
// "</jarvis</jarvis-context>-context>"), and indents its later lines under the item, so a text cannot pass for a
// decision of its own ("\n2. 选了：…").
func contextText(s string) string {
	for closingTag.MatchString(s) {
		s = closingTag.ReplaceAllString(s, "")
	}
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n      ")
}

// withTodoDecisions puts the todo's decisions before text handed to an agent for it; text as it is without any.
func (h *Hub) withTodoDecisions(ctx context.Context, todoID, text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	if c := h.todoDecisionsContext(ctx, todoID); c != "" {
		return c + "\n\n" + text
	}
	return text
}
