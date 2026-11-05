package hub

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// KBConfig tunes the knowledge base under ~/.persona/data (docs/knowledge.md). The knowledge base is the assistant's:
// the hub hands it this section in its hello (kbSettings; also to an assistant on another machine), refuses the
// sources while it is off, and reads ai itself (its model configuration's default for the kb.* purposes).
type KBConfig struct {
	Enabled        *bool      `json:"enabled,omitempty"`         // default on
	TextDays       int        `json:"text_days,omitempty"`       // full text kept (365)
	AttachmentDays int        `json:"attachment_days,omitempty"` // images / files kept (30)
	AI             *ai.Config `json:"ai,omitempty"`              // compression model (default inbox.triage_ai, then ai)
	// ExtraDirs are directories on the assistant's machine that kb_search also reads (Markdown, read only), e.g. a
	// repository of notes the user keeps; "~/" is the home there (docs/knowledge.md).
	ExtraDirs []string `json:"extra_dirs,omitempty"`
}

// The knowledge base is the assistant's (docs/knowledge.md「谁管什么」): its engine, its loop, its files and its
// index rows. The hub keeps the sources (kb_sources.go: the assistant reads the connectors through it), passes on the
// posts the connectors' programs hand it (kb_posts.go), tells it what it sees happen (kbKick, kbAgentDone), and asks
// it for what the hub's own features read of it through kbAsk alone.

// kbLink is what the hub keeps for the knowledge base: the agents' finished turns while the assistant is away.
type kbLink struct {
	mu      sync.Mutex
	pending []assistantwire.KBAgentTurn // at most kbPendingMost, one per agent (its latest)
}

// kbPendingMost bounds the turns kept while the assistant is away (each is one timeline line at most).
const kbPendingMost = 200

// errKBOff is what the knowledge base's sources answer while hub.json turns it off.
var errKBOff = errors.New("这台 hub 没开知识库")

// kbOff: hub.json kb.enabled is false: there is no knowledge base (the assistant opens none, the hub hands nothing over).
func (h *Hub) kbOff() bool {
	return h.cfg != nil && h.cfg.KB.Enabled != nil && !*h.cfg.KB.Enabled
}

// kbSettings is hub.json's kb section for the assistant's hello: the hub's file counts, wherever the assistant runs.
func (h *Hub) kbSettings() *assistantwire.KBSettings {
	if h.cfg == nil {
		return &assistantwire.KBSettings{}
	}
	c := h.cfg.KB
	return &assistantwire.KBSettings{Off: h.kbOff(), TextDays: c.TextDays, AttachmentDays: c.AttachmentDays, ExtraDirs: c.ExtraDirs}
}

// kbAsk is a call to the knowledge base (assistantwire.MethodKB*): assistant_offline while the assistant is not
// connected (callBus gives up after two minutes when ctx has no deadline). Every feature of the hub's that reads the
// knowledge base goes through here.
func (h *Hub) kbAsk(ctx context.Context, method string, params, out any) error {
	return h.asst().call(ctx, method, params, out)
}

// kbAskWaiting is kbAsk that first waits up to wait for the assistant to connect (a hub that just started, an
// assistant restarting): for the connectors' programs, which call as soon as they run.
func (h *Hub) kbAskWaiting(ctx context.Context, wait time.Duration, method string, params, out any) error {
	for until := time.Now().Add(wait); h.busConnOf(assistantwire.AssistantID) == nil && time.Now().Before(until) && ctx.Err() == nil; {
		select {
		case <-ctx.Done():
		case <-time.After(100 * time.Millisecond):
		}
	}
	return h.kbAsk(ctx, method, params, out)
}

// kbOffline: err is the knowledge base not being there now: the assistant not connected, or its knowledge base not
// open yet (the hub answers 503, try again).
func kbOffline(err error) bool {
	var re *rpc.Error
	return errors.As(err, &re) && (re.Code == assistantwire.CodeAssistantOffline || re.Code == assistantwire.CodeKBUnavailable)
}

// kbKick tells the knowledge base a source has something new (it goes round soon). Not connected, it is not told: it
// reads every source when it connects.
func (h *Hub) kbKick() {
	if !h.kbOff() {
		_ = h.notifyBus(assistantwire.AssistantID, assistantwire.EventKBKick, map[string]any{})
	}
}

// kbProjectNames are the names of the projects whose code directories hold cwd on machine (none when the knowledge
// base cannot tell now).
func (h *Hub) kbProjectNames(ctx context.Context, machine, cwd string) []string {
	if h.kbOff() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var res struct {
		Projects []assistantwire.KBProject `json:"projects"`
	}
	if err := h.kbAsk(ctx, assistantwire.MethodKBProjects, map[string]any{"machine_id": machine, "cwd": cwd}, &res); err != nil {
		return nil
	}
	var names []string
	for _, p := range res.Projects {
		names = append(names, p.Name)
	}
	return names
}

// kbAgentDone tells the knowledge base an agent finished a turn (its projects' timelines get what it came to, kb.agent_turn).
// While the assistant is not connected the turn waits here (the latest of each agent) and goes when it is back.
func (h *Hub) kbAgentDone(machine string, s proto.Session) {
	if strings.TrimSpace(s.LastText) == "" || h.kbOff() {
		return
	}
	t := assistantwire.KBAgentTurn{MachineID: machine, SessionID: s.SessionID, Cwd: s.Cwd, Title: s.Title, Line: workLogLine(s)}
	k := &h.kb
	k.mu.Lock()
	k.pending = queueKBTurn(k.pending, t)
	k.mu.Unlock()
	h.kbFlush() // queued first, then sent: one the assistant's connecting flush did not see is sent here
}

// queueKBTurn adds t to list: the latest of each agent, at most kbPendingMost (the oldest go).
func queueKBTurn(list []assistantwire.KBAgentTurn, t assistantwire.KBAgentTurn) []assistantwire.KBAgentTurn {
	list = slices.DeleteFunc(list, func(o assistantwire.KBAgentTurn) bool {
		return o.MachineID == t.MachineID && o.SessionID == t.SessionID
	})
	if len(list) >= kbPendingMost {
		list = list[len(list)-kbPendingMost+1:]
	}
	return append(list, t)
}

// kbFlush sends the turns kept while the assistant was away (it just connected, or a turn was just queued). One that
// cannot be sent goes back into the queue (behind none newer of its agent); when the assistant connected meanwhile
// (its own flush may have run before), it is tried again.
func (h *Hub) kbFlush() {
	k := &h.kb
	for try := 0; try < 3; try++ {
		k.mu.Lock()
		list := k.pending
		k.pending = nil
		k.mu.Unlock()
		failed := -1
		for i, t := range list {
			if h.notifyBus(assistantwire.AssistantID, assistantwire.EventKBAgentTurn, t) != nil {
				failed = i
				break
			}
		}
		if failed < 0 {
			return
		}
		k.mu.Lock()
		newer := k.pending
		k.pending = nil
		for _, t := range list[failed:] {
			k.pending = queueKBTurn(k.pending, t)
		}
		for _, t := range newer {
			k.pending = queueKBTurn(k.pending, t)
		}
		k.mu.Unlock()
		if h.busConnOf(assistantwire.AssistantID) == nil {
			return // its connecting flush sends them
		}
	}
}

func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
