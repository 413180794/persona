package hub

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// The todos are the assistant's (internal/assistant, todos.go; docs/todos.md): it keeps them and answers their routes,
// which the hub relays (assistantwire.Routes). The hub shows what the assistant reports (the snapshot's TodoReport):
// the todo on each agent's session (decorated), the task named on its agents' cards (withTask), which agents are
// quiet (quietSession: no pushes, badges or Live Activity). An agent handed work for a todo through the hub's own
// routes (POST /api/v1/sessions, …/messages with todo_id) is checked, given the todo's decisions and linked by the
// assistant (assistant.todo_handoff, assistant.todo_link); a workspace the hub renames is told (todos.space_renamed).

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

var (
	contextLeadRe  = regexp.MustCompile(`^<(?:jarvis|persona)-context\b`)
	contextOpenRe  = regexp.MustCompile(`<(?:jarvis|persona)-context\b`)
	contextCloseRe = regexp.MustCompile(`</(?:jarvis|persona)-context>`)
)

// cleanPromptTitle is a title for an agent from the prompt it was started with: its first line past a
// context block (<persona-context>, or <jarvis-context> as older ones were, #3182).
func cleanPromptTitle(prompt string) string {
	s := strings.TrimSpace(prompt)
	if contextLeadRe.MatchString(s) {
		if loc := contextCloseRe.FindStringIndex(s); loc != nil {
			s = s[loc[1]:]
		} else if endTagIdx := strings.Index(s, ">"); endTagIdx != -1 {
			s = s[endTagIdx+1:]
		} else {
			s = ""
		}
	}
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !contextOpenRe.MatchString(trimmed) {
			return trimmed
		}
	}
	return ""
}

// todoLinks is the assistant's last TodoReport's links (the task notices on the items the apps get name their task,
// injected.go); replaced, never written into.
type todoLinks struct {
	mu    sync.RWMutex
	links map[[2]string]assistantwire.TodoLink
}

// takeTodoReport keeps the assistant's report (nil: it could not read the todos; the last one stands).
func (h *Hub) takeTodoReport(r *assistantwire.TodoReport) {
	if r == nil {
		return
	}
	links := make(map[[2]string]assistantwire.TodoLink, len(r.Links))
	for _, l := range r.Links {
		links[[2]string{l.MachineID, l.SessionID}] = l
	}
	tl := &h.todoLinks
	tl.mu.Lock()
	tl.links = links
	tl.mu.Unlock()
}
