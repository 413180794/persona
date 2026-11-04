package hub

import (
	"context"
	"errors"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Closing the agents of finished tasks (docs/todos.md「做完的任务关掉 Agent」). Once a task opened from an assistant
// conversation is done — by the user or by the hub's judge (tasks_done.go) — its agents that have stayed still for a
// while are retired: the node closes their pane and the agent process with it (node retire.go). The todo keeps the
// agents (machine, session id, directory) and the node keeps how each was started (launcher, model, modes), so reading
// one goes on from its transcript, and a message to it — agent_send, a message on the task's card, the chat — resumes
// it in a new tab with the same launcher and directory before it goes in (claude --resume, codex resume, …).
//
// Never closed: an agent that works, waits for the user (a question, a plan, a permission, any dialog) or has
// background shells, subagents, a /loop or a server running; one linked to a task that is still open; an assistant
// conversation; a session another tool owns; one the node cannot bring back (it refuses: session.retire). Agents the
// user started by hand and never linked to a task are no task's, so nothing here touches them.

var (
	// taskCloseIdle is how long a done task's agent must have been still (and the task done) before it is closed.
	taskCloseIdle = 3 * time.Minute
	// taskCloseEvery is how often the hub looks for agents to close (tests shorten both).
	taskCloseEvery = time.Minute
)

// taskCloseRefused: (machine, session) → the session's updated_at when its node refused to close it, so it is not
// asked again every minute while nothing changed.
type taskCloseRefused map[[2]string]time.Time

func (h *Hub) taskCloseLoop(ctx context.Context) {
	t := time.NewTicker(taskCloseEvery)
	defer t.Stop()
	refused := taskCloseRefused{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		h.closeDoneTaskAgents(ctx, refused)
	}
}

// doneTaskAgent is an agent of a done task, with when the task was done.
type doneTaskAgent struct {
	todo, machine, session string
	doneAt                 time.Time
}

// doneTaskAgents are the agents still linked to tasks that are done, except those that count for an open todo now.
func (h *Hub) doneTaskAgents() []doneTaskAgent {
	h.todos.mu.RLock()
	defer h.todos.mu.RUnlock()
	var out []doneTaskAgent
	for _, t := range h.todos.todos {
		if t.Origin == "" || t.DoneAt == nil {
			continue
		}
		for _, a := range t.Agents {
			if a.ReleasedAt != nil || a.DismissedAt != nil {
				continue
			}
			if _, open := h.todos.activeBySession[[2]string{a.MachineID, a.SessionID}]; open {
				continue
			}
			out = append(out, doneTaskAgent{todo: t.ID, machine: a.MachineID, session: a.SessionID, doneAt: *t.DoneAt})
		}
	}
	return out
}

// retirable says whether s, an agent of a task done at doneAt, may be closed now: running in a pane of the engine,
// still since both the task was done and its last step at least taskCloseIdle ago, and with nothing going on.
func retirable(s proto.Session, doneAt, now time.Time) bool {
	switch {
	case s.Assistant, s.SourceKind != "", s.Starting, !s.StartedByJarvis:
		return false
	case s.Status == proto.StatusOffline || s.PaneID == "":
		return false // not running: nothing to close
	case s.Status == proto.StatusWorking || s.Status == proto.StatusBlocked || s.Status == proto.StatusUnknown:
		return false // working, waiting, or the engine cannot tell
	case s.State == proto.StateWorking || s.State == proto.StateNeedsYou || s.PendingInteractions > 0:
		return false
	case len(s.Queued) > 0 || len(s.Background) > 0 || s.Loop != nil || len(s.Ports) > 0:
		return false // more to come, or something it runs that the user may be using
	}
	for _, sa := range s.Subagents {
		if sa.Running {
			return false
		}
	}
	if s.Agent != "" && s.Agent != "claude" && s.Agent != "codex" && s.Agent != "traex" {
		return false
	}
	last := doneAt
	if s.UpdatedAt.After(last) {
		last = s.UpdatedAt
	}
	return now.Sub(last) >= taskCloseIdle
}

// closeDoneTaskAgents retires the agents of done tasks that may be closed now (retirable); refused remembers the
// sessions a node would not close.
func (h *Hub) closeDoneTaskAgents(ctx context.Context, refused taskCloseRefused) {
	h.todos.mu.RLock()
	loaded := h.todos.loaded
	h.todos.mu.RUnlock()
	if !loaded {
		return
	}
	now := time.Now()
	lists := map[string]map[string]proto.Session{}
	for _, a := range h.doneTaskAgents() {
		m := h.localMachine(a.machine)
		if m == nil || !m.reachable() {
			continue
		}
		list, ok := lists[a.machine]
		if !ok {
			list = map[string]proto.Session{}
			for _, s := range h.decorated(m) {
				list[s.SessionID] = s
			}
			lists[a.machine] = list
		}
		s, ok := list[a.session]
		key := [2]string{a.machine, a.session}
		if !ok || !retirable(s, a.doneAt, now) {
			continue
		}
		if at, ok := refused[key]; ok && at.Equal(s.UpdatedAt) {
			continue
		}
		var res struct {
			Closed bool `json:"closed"`
		}
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := m.call(cctx, "session.retire", map[string]any{"session_id": a.session}, &res)
		cancel()
		var re *rpc.Error
		switch {
		case err == nil:
			delete(refused, key)
			if res.Closed {
				h.logf("task %s: closed its finished agent %s/%s (resumes on the next message)", a.todo, a.machine, a.session)
			}
		case errors.As(err, &re): // the node refused (or is too old to know session.retire): not again until it changes
			refused[key] = s.UpdatedAt
			h.logf("task %s: agent %s/%s left open: %v", a.todo, a.machine, a.session, err)
		default:
			h.logf("task %s: close agent %s/%s: %v", a.todo, a.machine, a.session, err)
		}
	}
}
