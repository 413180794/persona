package hub

import (
	"context"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// The knowledge base is the assistant's (docs/knowledge.md"Responsibilities"): its engine, its loop, its files and its
// index rows, and its kb section, in the manager's manager.json. Its sources, the connectors' posts and the kicks are
// the manager's (internal/extmgr). What is left here is kbAsk, through which the hub's own features read the
// knowledge base (kbProjectNames); with the knowledge base off the manager answers none.

// kbAsk is a call to the knowledge base (assistantwire.MethodKB*): assistant_offline while the assistant is not
// connected (callBus gives up after two minutes when ctx has no deadline). Every feature of the hub's that reads the
// knowledge base goes through here.
func (h *Hub) kbAsk(ctx context.Context, method string, params, out any) error {
	return h.asst().call(ctx, method, params, out)
}

// kbProjectNames are the names of the projects whose code directories hold cwd on machine (none when the knowledge
// base cannot tell now).
func (h *Hub) kbProjectNames(ctx context.Context, machine, cwd string) []string {
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
