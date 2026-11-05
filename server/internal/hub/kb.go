package hub

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
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
// posts the connectors' programs hand it (kb_posts.go), tells it what it sees happen (kbKick; an agent's finished turn is the manager's own), and asks
// it for what the hub's own features read of it through kbAsk alone.

// kbLink is what the hub keeps for the knowledge base.
type kbLink struct {
	mu sync.Mutex
}

// errKBOff is what the knowledge base's sources answer while hub.json turns it off.
var errKBOff = i18n.Err("hub-kb.off")

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

func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
