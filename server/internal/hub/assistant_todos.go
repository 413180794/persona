package hub

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// The todos are the assistant's (internal/assistant, todos.go; docs/todos.md): it keeps them and answers their routes,
// which the hub relays (assistantwire.Routes). The hub shows what the assistant reports (the snapshot's TodoReport):
// the todo on each agent's session (decorated), the task named on its agents' cards (withTask), which agents are
// quiet (quietSession: no pushes, badges or Live Activity). An agent handed work for a todo through the hub's own
// routes (POST /api/v1/sessions, …/messages with todo_id) is checked, given the todo's decisions and linked by the
// assistant (assistant.todo_handoff, assistant.todo_link); a workspace the hub renames is told (todos.space_renamed).

// todoLinks is the assistant's last TodoReport, by (machine, session); replaced, never written into.
type todoLinks struct {
	mu    sync.RWMutex
	links map[[2]string]assistantwire.TodoLink
	quiet map[[2]string]bool
}

// takeTodoReport keeps the assistant's report (nil: it could not read the todos; the last one stands) and says the
// machines whose sessions look different now.
func (h *Hub) takeTodoReport(r *assistantwire.TodoReport) []string {
	if r == nil {
		return nil
	}
	links := make(map[[2]string]assistantwire.TodoLink, len(r.Links))
	for _, l := range r.Links {
		links[[2]string{l.MachineID, l.SessionID}] = l
	}
	quiet := make(map[[2]string]bool, len(r.Quiet))
	for _, q := range r.Quiet {
		quiet[[2]string{q.MachineID, q.SessionID}] = true
	}
	tl := &h.todoLinks
	tl.mu.Lock()
	oldLinks, oldQuiet := tl.links, tl.quiet
	tl.links, tl.quiet = links, quiet
	tl.mu.Unlock()
	var out []string
	add := func(key [2]string) {
		if !slices.Contains(out, key[0]) {
			out = append(out, key[0])
		}
	}
	for key, l := range links {
		if oldLinks[key] != l {
			add(key)
		}
	}
	for key := range oldLinks {
		if _, ok := links[key]; !ok {
			add(key)
		}
	}
	for key := range quiet {
		if !oldQuiet[key] {
			add(key)
		}
	}
	for key := range oldQuiet {
		if !quiet[key] {
			add(key)
		}
	}
	return out
}

// todoLinkOf is the open todo the agent mid/sid works for, as the assistant reported it.
func (h *Hub) todoLinkOf(mid, sid string) (assistantwire.TodoLink, bool) {
	tl := &h.todoLinks
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	l, ok := tl.links[[2]string{mid, sid}]
	return l, ok
}

// decorateTodos puts on machine mid's sessions the todo each works for and whether it is quiet.
func (h *Hub) decorateTodos(mid string, list []proto.Session) {
	tl := &h.todoLinks
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	for i := range list {
		key := [2]string{mid, list[i].SessionID}
		if l, ok := tl.links[key]; ok {
			list[i].Todo = &proto.TodoRef{ID: l.TodoID, Title: l.Title}
		}
		list[i].Quiet = tl.quiet[key]
	}
}

// quietSession: mid/sid works (or worked) for a quiet task: the main conversation hears about it, nothing else does.
func (h *Hub) quietSession(mid, sid string) bool {
	tl := &h.todoLinks
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	return tl.quiet[[2]string{mid, sid}]
}

// withTask names, on an interaction sent to clients, the task its agent works for (the watch and the cards show it).
func (h *Hub) withTask(ix proto.Interaction) proto.Interaction {
	if l, ok := h.todoLinkOf(ix.MachineID, ix.SessionID); ok && l.Task {
		ix.TaskTitle = l.Title
	}
	return ix
}

// ---- handing an agent work for a todo (POST /api/v1/sessions, …/messages with todo_id) ----

// TodoLinkInfo is the todo an agent is about to be handed work for.
type TodoLinkInfo struct {
	ID     string
	Title  string
	DoneAt *time.Time
}

// getTodoForLink is the todo todoID before an agent is handed work for it (assistant.todo_handoff); nil when there is
// no such todo; an error when the assistant could not say (assistant_offline while it is not connected).
func (h *Hub) getTodoForLink(ctx context.Context, todoID string) (*TodoLinkInfo, error) {
	res, err := h.asst().TodoHandoff(ctx, assistantwire.TodoHandoff{TodoID: todoID})
	if err != nil {
		return nil, err
	}
	if !res.Found {
		return nil, nil
	}
	return &TodoLinkInfo{ID: todoID, Title: res.Title, DoneAt: res.DoneAt}, nil
}

// withTodoDecisions puts the todo's decisions before text handed to an agent for it; text as it is without any (or
// when the assistant could not say: the handoff goes on without them).
func (h *Hub) withTodoDecisions(ctx context.Context, todoID, text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	res, err := h.asst().TodoHandoff(ctx, assistantwire.TodoHandoff{TodoID: todoID, Text: text})
	if err != nil {
		h.logf("decisions: todo %s: %v", todoID, err)
		return text
	}
	if !res.Found || res.Text == "" {
		return text
	}
	return res.Text
}

// linkTodoAgent: the agent mid/sid was handed work for todoID; it works for it from now on (assistant.todo_link). The
// assistant reports the link (the session lists follow).
func (h *Hub) linkTodoAgent(ctx context.Context, todoID, mid, sid, title, cwd, agent, _ string) error {
	return h.asst().TodoLink(ctx, assistantwire.TodoLinkParams{TodoID: todoID, MachineID: mid, SessionID: sid, Title: title,
		Cwd: cwd, Agent: agent})
}

// renameTodoSpace tells the assistant the hub renamed a workspace (a todo linked to it by name follows; not while the
// assistant is not connected).
func (h *Hub) renameTodoSpace(mid, old, name string) {
	h.asst().TodoSpaceRenamed(context.Background(), assistantwire.TodoSpaceRenamed{MachineID: mid, Old: old, New: name})
}

// cleanPromptTitle is a title for an agent from the prompt it was started with: its first line past a
// <jarvis-context>.
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
