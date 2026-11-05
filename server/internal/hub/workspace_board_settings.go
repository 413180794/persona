package hub

import (
	"context"
	"errors"
	"net/http"
)

// The workflow board is off unless turned on (#2475): one switch for the whole hub, kept in the settings table so every
// browser agrees. Turning it off only hides it; the board and the workspace snapshots stay as they were.
const workspaceBoardSettingsKey = "workspace_board.settings"

type workspaceBoardSettings struct {
	Enabled bool `json:"enabled"`
}

// workspaceBoardUsage says whether the board was ever changed. Only a store that keeps the board can tell.
type workspaceBoardUsage interface {
	WorkspaceBoardInUse(context.Context) (bool, error)
}

func (h *Hub) workspaceBoardSettingsRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/workspace-board/settings", h.getWorkspaceBoardSettings)
	api.HandleFunc("PUT /api/v1/workspace-board/settings", h.putWorkspaceBoardSettings)
}

// workspaceBoardEnabled is the stored switch; before anyone set it, a board that was already used counts as on, so an
// upgrade does not take away a board someone keeps (docs/workspace-board.md「开关」). It is not written back: the first
// explicit change is what gets stored.
func (h *Hub) workspaceBoardEnabled(ctx context.Context) (bool, error) {
	if h.store == nil {
		return false, errors.New("工作流看板的开关需要数据库")
	}
	var s workspaceBoardSettings
	found, err := h.store.Setting(ctx, workspaceBoardSettingsKey, &s)
	if err != nil || found {
		return s.Enabled, err
	}
	if u, ok := h.store.(workspaceBoardUsage); ok {
		return u.WorkspaceBoardInUse(ctx)
	}
	return false, nil
}

func (h *Hub) writeWorkspaceBoardSettings(w http.ResponseWriter, r *http.Request, enabled bool) {
	writeJSON(w, map[string]any{"enabled": enabled, "can_write": principalOf(r.Context()).Role == roleFull})
}

func (h *Hub) getWorkspaceBoardSettings(w http.ResponseWriter, r *http.Request) {
	on, err := h.workspaceBoardEnabled(r.Context())
	if err != nil {
		writeErr(w, 503, err)
		return
	}
	h.writeWorkspaceBoardSettings(w, r, on)
}

func (h *Hub) putWorkspaceBoardSettings(w http.ResponseWriter, r *http.Request) {
	if principalOf(r.Context()).Role != roleFull {
		writeErr(w, 403, errors.New("只读设备不能修改工作流看板的开关"))
		return
	}
	if h.store == nil {
		writeErr(w, 503, errors.New("工作流看板的开关需要数据库"))
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSONBody(w, r, &body, 4<<10); err != nil || body.Enabled == nil {
		writeBodyErr(w, err, errors.New("需要 enabled"))
		return
	}
	if err := h.store.PutSetting(r.Context(), workspaceBoardSettingsKey, workspaceBoardSettings{Enabled: *body.Enabled}); err != nil {
		writeErr(w, 503, err)
		return
	}
	h.broadcast(map[string]any{"type": "workspace_board_settings", "enabled": *body.Enabled})
	h.writeWorkspaceBoardSettings(w, r, *body.Enabled)
}
