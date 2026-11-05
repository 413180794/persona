package hub

import (
	"context"
	"net/http"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// The assistant's skills live on the machine that runs it, in the user's ~/.jarvis/skills (docs/hub-update.md「skill
// 从二进制里拿出来」): the hub asks that machine's node (skills.list) to show them and to tell the assistant what it has.

var errNoAssistantMachine = i18n.Err("hub-skills.no_assistant_machine")

// skill is one skill as the node lists it (assistantskills.Skill).
type skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"` // user | builtin
}

// machineSkills asks m's node for the assistant's skills there.
func (h *Hub) machineSkills(ctx context.Context, m *Machine) ([]skill, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out struct {
		Skills []skill `json:"skills"`
	}
	if err := m.call(ctx, "skills.list", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Skills, nil
}

// serveSkills is GET /api/v1/skills: the skills of the machine that runs the assistant, read only.
func (h *Hub) serveSkills(w http.ResponseWriter, r *http.Request) {
	m := h.localMachine(h.assistantProfile().MachineID)
	if m == nil {
		writeErr(w, http.StatusConflict, errNoAssistantMachine)
		return
	}
	list, err := h.machineSkills(r.Context(), m)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if list == nil {
		list = []skill{}
	}
	writeJSON(w, map[string]any{"machine_id": m.cfg.ID, "machine": m.cfg.Name, "skills": list})
}
