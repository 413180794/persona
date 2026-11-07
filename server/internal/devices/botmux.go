package devices

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// botmuxKey is where a machine's own choice is kept. hub.json's machines[].botmux is only the default now: the switch
// "Also show botmux sessions" (the web's machine page header, iOS's Engine) answers for one machine (docs/integrations/engines.md "botmux").
func botmuxKey(machine string) string { return "botmux/" + machine }

// botmux is whether the machine's node lists botmux's sessions: what the user chose for this machine, else hub.json's
// machines[].botmux, else on.
func (m *Machine) botmux(ctx context.Context) bool {
	if m.d.opts.Store != nil {
		var on bool
		if ok, err := m.d.opts.Store.Setting(ctx, botmuxKey(m.cfg.ID), &on); ok && err == nil {
			return on
		}
	}
	return m.cfg.Botmux == nil || *m.cfg.Botmux
}

// setBotmux stores the machine's choice and hands it to the node, as configureNode does when it connects.
func (m *Machine) setBotmux(ctx context.Context, on bool) error {
	if m.d.opts.Store == nil {
		return i18n.Err("devices-api.no_store")
	}
	if err := m.d.opts.Store.PutSetting(ctx, botmuxKey(m.cfg.ID), on); err != nil {
		return err
	}
	call, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	// What is stored when this goes out, not on: of two flips at once, the node ends on the one kept.
	if err := m.call(call, "node.configure", map[string]any{"botmux": m.botmux(call)}, nil); err != nil {
		m.d.logf("[%s] configure botmux: %v", m.cfg.ID, err)
	}
	return nil
}

// botmuxState is what GET / PUT /api/v1/machines/{mid}/botmux answer: the setting in force.
type botmuxState struct {
	On bool `json:"on"`
}

// getBotmux: GET /api/v1/machines/{mid}/botmux.
func (d *Devices) getBotmux(w http.ResponseWriter, r *http.Request, m *Machine) {
	writeJSON(w, botmuxState{On: m.botmux(r.Context())})
}

// putBotmux: PUT /api/v1/machines/{mid}/botmux {"on": bool}, the switch "Also show botmux sessions". Any engine: botmux is
// listed beside whichever one the machine uses. Stored while the machine is offline too (sent when it connects).
func (d *Devices) putBotmux(w http.ResponseWriter, r *http.Request, m *Machine) {
	var in struct {
		On *bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.On == nil {
		writeErr(w, http.StatusBadRequest, i18n.Err("devices-api.need_on"))
		return
	}
	if err := m.setBotmux(r.Context(), *in.On); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d.changed()
	writeJSON(w, botmuxState{On: m.botmux(r.Context())})
}
