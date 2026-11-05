package hub

import (
	"context"
	"regexp"
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
	return h.todoHandoffText(ctx, assistantwire.TodoHandoff{TodoID: todoID, Text: text})
}

// withTodoStart is withTodoDecisions for a new agent's first prompt: the background of the project its directory (cwd
// on machine mid) belongs to goes first too (#2820).
func (h *Hub) withTodoStart(ctx context.Context, todoID, mid, cwd, text string) string {
	return h.todoHandoffText(ctx, assistantwire.TodoHandoff{TodoID: todoID, Text: text, New: true, MachineID: mid, Cwd: cwd})
}

func (h *Hub) todoHandoffText(ctx context.Context, p assistantwire.TodoHandoff) string {
	todoID, text := p.TodoID, p.Text
	if strings.TrimSpace(text) == "" {
		return text
	}
	res, err := h.asst().TodoHandoff(ctx, p)
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

// freshStarts are the agents started for a todo with nothing to say yet (the apps start Claude in the background and
// send the first message themselves, #2820): their first message is a new agent's first prompt, which gets the
// project background, whether or not it names the todo (iOS and Android do not). Kept in memory for freshStartTTL; a
// hub restarted in between sends that message as it is.
type freshStarts struct {
	mu   sync.Mutex
	byID map[[2]string]freshStart
}

type freshStart struct {
	todoID, cwd string
	at          time.Time
}

const freshStartTTL = 15 * time.Minute

// add remembers agent mid/sid, started for todoID in cwd, until its first message.
func (f *freshStarts) add(mid, sid, todoID, cwd string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	if f.byID == nil {
		f.byID = map[[2]string]freshStart{}
	}
	for k, v := range f.byID {
		if now.Sub(v.at) > freshStartTTL {
			delete(f.byID, k)
		}
	}
	f.byID[[2]string{mid, sid}] = freshStart{todoID: todoID, cwd: cwd, at: now}
}

// peek is agent mid/sid's fresh start, still kept: a first message that does not go through (refused, nothing typed)
// and is sent again gets the background again.
func (f *freshStarts) peek(mid, sid string) (freshStart, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.byID[[2]string{mid, sid}]
	if !ok || time.Since(v.at) > freshStartTTL {
		return freshStart{}, false
	}
	return v, true
}

// done forgets agent mid/sid's fresh start: its first message went through.
func (f *freshStarts) done(mid, sid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byID, [2]string{mid, sid})
}

// startCwd is where a start asked for cwd runs on m: as the node resolves it ("" its home, "~/" under it).
func startCwd(m *Machine, cwd string) string {
	m.mu.Lock()
	home := m.info.Home
	m.mu.Unlock()
	if cwd == "" {
		return home
	}
	return proto.ExpandHome(cwd, home) // as the node's checkStart does
}
