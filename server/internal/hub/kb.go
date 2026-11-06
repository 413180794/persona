package hub

import (
	"context"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/assistantwire"
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
// index rows. Its sources, the connectors' posts and the kicks are the manager's (internal/extmgr). What is left
// here is the hub's side: hub.json's kb section (KBConfig), whether it is off (kbOff), the settings it hands the
// assistant in its hello (kbSettings), and kbAsk, through which the hub's own features read the knowledge base
// (kbProjectNames).

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
