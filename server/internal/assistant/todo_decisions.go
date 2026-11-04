package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// A todo's decision record (#1618, docs/todos.md「决策记录」): the calls the user made on it, written by hand in the
// todo or from their answers to the question cards of the agents working on it (interaction.resolved), and handed to
// every agent the todo is given to (assistant.todo_handoff).

const decisionTextMax = 4000 // a question, a choice or a reason

var errNoDecisionStore = errors.New("decisions need the hub's database")

func (s *Service) todoDecisionRoutes() {
	// GET /api/v1/todos/{id}/decisions[?all=1]: the decisions in effect, oldest first; all=1 adds the superseded ones.
	s.handle("GET /api/v1/todos/{id}/decisions", func(w http.ResponseWriter, r *http.Request) {
		st, id, ok := s.decisionTodo(w, r)
		if !ok {
			return
		}
		all := r.URL.Query().Get("all")
		list, err := st.ListTodoDecisions(r.Context(), id, all == "1" || all == "true")
		if err != nil {
			apierr.Write(w, http.StatusInternalServerError, err)
			return
		}
		apierr.WriteJSON(w, map[string]any{"decisions": list})
	})

	// POST /api/v1/todos/{id}/decisions: write one by hand {question, choice, reason?, supersedes_id?}.
	s.handle("POST /api/v1/todos/{id}/decisions", func(w http.ResponseWriter, r *http.Request) {
		if !fullRole(r) {
			apierr.Write(w, http.StatusForbidden, errors.New("read-only role cannot modify todos"))
			return
		}
		st, id, ok := s.decisionTodo(w, r)
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
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		row := db.TodoDecisionRow{ID: "dec_" + randomHex(8), TodoID: id, Question: decisionText(in.Question),
			Choice: decisionText(in.Choice), Reason: decisionText(in.Reason), Source: db.DecisionManual,
			SupersedesID: strings.TrimSpace(in.SupersedesID), CreatedAt: decisionNow()}
		if row.Question == "" || row.Choice == "" {
			apierr.Write(w, http.StatusBadRequest, errors.New("question and choice cannot be empty"))
			return
		}
		if _, err := st.InsertTodoDecision(r.Context(), row); err != nil {
			decisionErr(w, err)
			return
		}
		s.decisionsChanged(r.Context(), id, row.CreatedAt)
		apierr.WriteJSON(w, map[string]any{"decision": row})
	})

	// PATCH /api/v1/todos/{id}/decisions/{did}: {supersedes_id}: mark it as replacing an earlier decision ("" undoes it).
	s.handle("PATCH /api/v1/todos/{id}/decisions/{did}", func(w http.ResponseWriter, r *http.Request) {
		if !fullRole(r) {
			apierr.Write(w, http.StatusForbidden, errors.New("read-only role cannot modify todos"))
			return
		}
		st, id, ok := s.decisionTodo(w, r)
		if !ok {
			return
		}
		var in struct {
			SupersedesID *string `json:"supersedes_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		if in.SupersedesID == nil {
			apierr.Write(w, http.StatusBadRequest, errors.New("supersedes_id is required"))
			return
		}
		at := decisionNow()
		row, err := st.SetTodoDecisionSupersedes(r.Context(), id, r.PathValue("did"), strings.TrimSpace(*in.SupersedesID), at)
		if err != nil {
			decisionErr(w, err)
			return
		}
		s.decisionsChanged(r.Context(), id, at)
		apierr.WriteJSON(w, map[string]any{"decision": row})
	})
}

// decisionTodo is the store and the request's todo id once the todos are loaded and it is one of them; otherwise it
// answers.
func (s *Service) decisionTodo(w http.ResponseWriter, r *http.Request) (todoStore, string, bool) {
	st := s.todoDB()
	if st == nil {
		apierr.Write(w, http.StatusServiceUnavailable, errNoDecisionStore)
		return nil, "", false
	}
	if err := s.ensureTodosLoaded(r.Context()); err != nil {
		apierr.Write(w, http.StatusServiceUnavailable, errTodosUnavailable)
		return nil, "", false
	}
	id := r.PathValue("id")
	if _, ok := s.todoNow(id); !ok {
		apierr.Write(w, http.StatusNotFound, errors.New("todo not found"))
		return nil, "", false
	}
	return st, id, true
}

func decisionErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrTodoNotFound), errors.Is(err, db.ErrDecisionNotFound):
		apierr.Write(w, http.StatusNotFound, err)
	case errors.Is(err, db.ErrDecisionSupersede):
		apierr.Write(w, http.StatusBadRequest, err)
	default:
		apierr.Write(w, http.StatusInternalServerError, err)
	}
}

// decisionNow is now as Postgres keeps it (microseconds), so the order checks compare what was stored.
func decisionNow() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// decisionText is a decision's text as kept: trimmed, at most decisionTextMax runes.
func decisionText(s string) string { return clip(strings.TrimSpace(s), decisionTextMax) }

// cardDecisions: the user answered a question card (interaction.resolved); when its agent works on a todo, each
// answered question is a decision of that todo. Only Claude Code's option questions (AskUserQuestion) count: approvals
// and permissions are not decisions about the work, and a dismissed or failed card answered nothing.
func (s *Service) cardDecisions(ctx context.Context, ix proto.Interaction, r proto.Resolution, status string) {
	st := s.todoDB()
	if st == nil || status != "resolved" || r.Dismiss || ix.Kind != proto.IxQuestion || len(r.Answers) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.ensureTodosLoaded(ctx); err != nil {
		s.logf("decisions: card %s: todos not loaded: %v", ix.ID, err)
		return
	}
	s.todos.mu.RLock()
	todoID := s.todos.activeBySession[[2]string{ix.MachineID, ix.SessionID}]
	s.todos.mu.RUnlock()
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
		ok, err := st.InsertTodoDecision(ctx, db.TodoDecisionRow{ID: "dec_" + randomHex(8), TodoID: todoID,
			Question: q, Choice: c, Source: db.DecisionCard, SourceMachine: ix.MachineID, SourceSession: ix.SessionID,
			SourceMessage: ix.ID, CreatedAt: at})
		if err != nil {
			s.logf("decisions: todo %s, card %s: %v", todoID, ix.ID, err)
			return
		}
		if ok {
			wrote, last = true, at
		}
	}
	if wrote {
		s.decisionsChanged(ctx, todoID, last)
	}
}

// decisionsChanged: the store bumped the todo's updated_at with its decisions; the kept todo follows, and the apps
// refetch (they reload a todo's decisions when its updated_at moves).
func (s *Service) decisionsChanged(ctx context.Context, todoID string, at time.Time) {
	s.todos.mu.Lock()
	if t, ok := s.todos.todos[todoID]; ok && at.After(t.UpdatedAt) {
		t.UpdatedAt = at
	}
	s.todos.mu.Unlock()
	s.todosChanged(ctx)
}

// todoDecisionsContext is the <jarvis-context> of a todo's decisions in effect that goes before what is handed to an
// agent for it; empty when it has none or they cannot be read (the handoff goes on without them).
func (s *Service) todoDecisionsContext(ctx context.Context, todoID string) string {
	st := s.todoDB()
	if st == nil || todoID == "" {
		return ""
	}
	list, err := st.ListTodoDecisions(ctx, todoID, false)
	if err != nil {
		s.logf("decisions: todo %s: %v", todoID, err)
		return ""
	}
	if len(list) == 0 {
		return ""
	}
	var title string
	agents := map[[2]string]string{}
	if t, ok := s.todoNow(todoID); ok {
		title = t.Title
		for _, a := range t.Agents {
			agents[[2]string{a.MachineID, a.SessionID}] = a.Title
		}
	}
	loc := s.location(ctx)
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
func (s *Service) withTodoDecisions(ctx context.Context, todoID, text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	if c := s.todoDecisionsContext(ctx, todoID); c != "" {
		return c + "\n\n" + text
	}
	return text
}
