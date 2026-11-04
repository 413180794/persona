package hub

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// Closing a finished task (docs/todos.md「自动完成」, #1092). When a task's agents have all stopped and its receipt
// went out, the judge model reads the task's title, the prompt it was handed out with and what its agents said last,
// and only "done" closes the todo (done_by auto, with why). Anything else — not finished, waiting on someone,
// needing the user, unsure, no model, a model error — leaves it open. A todo the user reopened is not closed again
// by the same stretch of work.

const (
	verdictDone      = "done"
	verdictNotDone   = "not_done"
	verdictWaiting   = "waiting"
	verdictNeedsUser = "needs_user"
	verdictUnsure    = "unsure"
)

// taskDoneTimeout bounds one judgment (reading the replies and the model).
var taskDoneTimeout = 90 * time.Second

// taskDoneSignals are cheap hints found in the replies, handed to the model (never enough on their own).
type taskDoneSignals struct {
	MergedPR bool `json:"merged_pr"`
	Build    bool `json:"build"`
}

var (
	mergedWords  = regexp.MustCompile(`(?i)\bmerged\b|\bsquash(ed)?\b|已合并|合并了|已合入|合入了|自行合并`)
	buildNumbers = regexp.MustCompile(`(?i)testflight[^\n]{0,40}?\d{3,}|构建号?\s*[:：#]?\s*\d{3,}|\bbuild\s*#?\s*\d{3,}`)
)

func doneSignals(replies []prompts.TaskDoneReply) taskDoneSignals {
	var s taskDoneSignals
	for _, r := range replies {
		if prLink.MatchString(r.Text) && mergedWords.MatchString(r.Text) {
			s.MergedPR = true
		}
		if buildNumbers.MatchString(r.Text) {
			s.Build = true
		}
	}
	return s
}

// taskDoneCheck is one judgment, as POST /api/v1/todos/{id}/done-check answers it.
type taskDoneCheck struct {
	Verdict    string          `json:"verdict"`
	Reason     string          `json:"reason"`
	WouldClose bool            `json:"would_close"`
	Skip       string          `json:"skip"` // why the automatic pass would not judge it now; "" when it would
	Replies    int             `json:"replies"`
	Signals    taskDoneSignals `json:"signals"`
}

// autoDoneSkip says why the hub would not judge t now: "" when it would. work is when one of its agents last started
// a turn (worked false: not known, a hub restarted since).
func autoDoneSkip(t Todo, work time.Time, worked bool) string {
	switch {
	case t.Origin == "":
		return "not_a_task" // a todo the user made by hand: only they close it
	case t.DoneAt != nil:
		return "done"
	case t.ReopenedAt != nil && !(worked && work.After(*t.ReopenedAt)):
		return "reopened" // no new turn since the user reopened it
	}
	return ""
}

// taskReplies are the last replies of t's active agents: the whole last answer read from the agent's transcript, or
// the session list's clipped last_text when that cannot be read.
func (h *Hub) taskReplies(ctx context.Context, t Todo) []prompts.TaskDoneReply {
	var out []prompts.TaskDoneReply
	for _, a := range t.Agents {
		if a.ReleasedAt != nil || a.DismissedAt != nil {
			continue
		}
		m := h.localMachine(a.MachineID)
		if m == nil {
			continue
		}
		text := lastAnswer(ctx, m, a.SessionID)
		if text == "" {
			if s, ok := m.session(a.SessionID); ok {
				text = strings.TrimSpace(s.LastText)
			}
		}
		if text == "" {
			continue
		}
		out = append(out, prompts.TaskDoneReply{Machine: m.cfg.Name, Text: clipMiddle(text, 1500)})
	}
	return out
}

// lastAnswer is the agent's last reply in its latest turn; "" when it cannot be read.
func lastAnswer(ctx context.Context, m *Machine, sid string) string {
	var res struct {
		Items []proto.Item `json:"items"`
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := m.call(cctx, "session.items", map[string]any{"session_id": sid, "turns": 1}, &res); err != nil {
		return ""
	}
	for i := len(res.Items) - 1; i >= 0; i-- {
		if it := res.Items[i]; it.Kind == proto.KindText && it.Role == "assistant" && strings.TrimSpace(it.Text) != "" {
			return strings.TrimSpace(it.Text)
		}
	}
	return ""
}

// clipMiddle keeps a long text's first and last n runes: a final answer says what was done first and what is left last.
func clipMiddle(s string, n int) string {
	if utf8.RuneCountInString(s) <= 2*n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "\n…\n" + string(r[len(r)-n:])
}

// judgeTaskDone asks the judge model whether t's work is finished. An error: no model, or no answer to go by.
func (h *Hub) judgeTaskDone(ctx context.Context, t Todo) (taskDoneCheck, error) {
	replies := h.taskReplies(ctx, t)
	c := taskDoneCheck{Replies: len(replies), Signals: doneSignals(replies)}
	if len(replies) == 0 {
		c.Verdict, c.Reason = verdictUnsure, "没读到 Agent 的回复"
		return c, nil
	}
	system := prompts.RenderTaskDone(prompts.TaskDone{Owner: h.ownerName()})
	input := prompts.RenderTaskDoneInput(prompts.TaskDoneInput{Title: t.Title, Prompt: clipMiddle(strings.TrimSpace(t.Notes), 1500),
		Replies: replies, MergedPR: c.Signals.MergedPR, Build: c.Signals.Build})
	var out struct {
		Verdict string `json:"verdict"`
		Reason  string `json:"reason"`
	}
	if err := h.judgeModel().JSON(ctx, system, input, &out); err != nil {
		return c, err
	}
	switch v := strings.ToLower(strings.TrimSpace(out.Verdict)); v {
	case verdictDone, verdictNotDone, verdictWaiting, verdictNeedsUser:
		c.Verdict = v
	default:
		c.Verdict = verdictUnsure // a verdict it made up is no verdict
	}
	c.Reason = clip(oneLine(out.Reason), 40)
	c.WouldClose = c.Verdict == verdictDone
	return c, nil
}

// autoFinishTask: t's receipt went out (its agents all stopped). work is when one of them last started a turn. The
// todo is marked done when the judge finds the work finished; otherwise it stays as it is.
func (h *Hub) autoFinishTask(t Todo, work time.Time, worked bool) {
	if skip := autoDoneSkip(t, work, worked); skip != "" {
		if skip == "reopened" {
			h.logf("task %s: reopened by the user and no new turn since: not judged", t.ID)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskDoneTimeout)
	defer cancel()
	c, err := h.judgeTaskDone(ctx, t)
	if err != nil {
		if !ai.IsPaused(err) {
			h.logf("task %s: done judge: %v (left open)", t.ID, err)
		}
		return
	}
	if !c.WouldClose {
		h.logf("task %s: not closed: %s (%s)", t.ID, c.Verdict, c.Reason)
		return
	}
	reason := c.Reason
	if reason == "" {
		reason = "做完了"
	}
	if !worked {
		work = t.CreatedAt
	}
	ok, err := h.autoDoneTodo(ctx, t.ID, reason, work)
	switch {
	case err != nil:
		h.logf("task %s: auto done: %v", t.ID, err)
	case ok:
		h.logf("task %s: done (auto): %s", t.ID, reason)
	default:
		h.logf("task %s: changed while judged: left as it is", t.ID)
	}
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

// doneCheckRoute: POST /api/v1/todos/{id}/done-check runs the judgment on a todo as it is and answers it; nothing is
// written (for looking into why a task was or was not closed).
func (h *Hub) doneCheckRoute(api *http.ServeMux) {
	api.HandleFunc("POST /api/v1/todos/{id}/done-check", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot judge todos"))
			return
		}
		if err := h.ensureTodosLoaded(r.Context()); err != nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("todos service unavailable"))
			return
		}
		id := r.PathValue("id")
		h.todos.mu.RLock()
		tp := h.todos.todos[id]
		var t Todo
		if tp != nil {
			t = copyTodoLocked(tp)
		}
		h.todos.mu.RUnlock()
		if tp == nil {
			writeErr(w, http.StatusNotFound, db.ErrTodoNotFound)
			return
		}
		h.tasks.mu.Lock()
		work, worked := h.tasks.working[id]
		h.tasks.mu.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), taskDoneTimeout)
		defer cancel()
		c, err := h.judgeTaskDone(ctx, t)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		c.Skip = autoDoneSkip(t, work, worked)
		if c.Skip != "" {
			c.WouldClose = false
		}
		writeJSON(w, c)
	})
}
