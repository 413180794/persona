package hub

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"
)

// The assistant's skills live on the machine that runs it, in the user's ~/.jarvis/skills (docs/hub-update.md「skill
// 从二进制里拿出来」): the hub asks that machine's node (skills.list) to show them and to tell the assistant what it has.

var errNoAssistantMachine = errors.New("还没设置助手跑在哪台电脑上")

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

// hasSkill says whether the assistant on m has the skill name. A node too old to list its skills (one that carried
// lark-connector inside itself) is taken to have it, as it did.
func (h *Hub) hasSkill(ctx context.Context, m *Machine, name string) bool {
	list, err := h.machineSkills(ctx, m)
	if err != nil {
		return true
	}
	return slices.ContainsFunc(list, func(s skill) bool { return s.Name == name })
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
