package hub

import (
	"net/http"
	"path"

	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/proto"
)

// The assistant ("Persona" tab) is a process of its own (internal/assistant): its conversations are headless ones a
// node runs in a dedicated directory on one machine; the hub tells them apart from the agents by that directory, and
// relays the assistant's routes to it (assistant_link.go).

const assistantDir = ".jarvis/assistant" // relative to the machine's home

// assistantCwd is the assistant directory on a machine, or "" while the machine has not reported its home.
func (m *Machine) assistantCwd() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.info.Home == "" {
		return ""
	}
	if m.info.DataDir != "" {
		return path.Join(m.info.DataDir, "assistant")
	}
	return path.Join(m.info.Home, assistantDir)
}

// isAssistant identifies headless conversations. A host pane is stronger evidence than a shared working directory.
func isAssistant(s proto.Session, dir string) bool {
	if s.PaneID != "" || dir == "" || s.Cwd == "" {
		return false
	}
	// The working directory may go through a symlinked home; runtimes and offline assistants have no host pane.
	return s.Cwd == dir || identity.IsAssistantDir(s.Cwd)
}

// assistantRoutes are the skills the assistant's machine has and the assistant's own routes, relayed to it.
func (h *Hub) assistantRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/skills", h.serveSkills)
	h.assistantRoutesRelayed(api)
}

// assistantSpace is the space of an assistant conversation working in cwd: "health" for the health space's
// (docs/health.md), "" for the assistant's own.
func assistantSpace(cwd string) string { return identity.SpaceOf(cwd) }
