package devices

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"

	"github.com/nwverse/persona/server/internal/proto"
)

// herdrAutostartKey is where a machine's own choice is kept. hub.json's machines[].herdr_autostart is only the
// default now: the switch in Settings › Machines › Engine answers for one machine, and an older hub never wrote this key
// (docs/integrations/engines.md "When herdr is not running").
func herdrAutostartKey(machine string) string { return "herdr_autostart/" + machine }

// herdrAutostart is whether the machine's node may start herdr's server when it is not running: what the user chose
// for this machine, else hub.json's value, else on.
func (m *Machine) herdrAutostart(ctx context.Context) bool {
	if m.d.opts.Store != nil {
		var on bool
		if ok, err := m.d.opts.Store.Setting(ctx, herdrAutostartKey(m.cfg.ID), &on); ok && err == nil {
			return on
		}
	}
	return m.cfg.HerdrAutostart == nil || *m.cfg.HerdrAutostart
}

// setHerdrAutostart stores the machine's choice and hands it to the node, as configureNode does when it connects.
func (m *Machine) setHerdrAutostart(ctx context.Context, on bool) error {
	if m.d.opts.Store == nil {
		return i18n.Err("devices-api.no_store")
	}
	if err := m.d.opts.Store.PutSetting(ctx, herdrAutostartKey(m.cfg.ID), on); err != nil {
		return err
	}
	call, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := m.call(call, "node.configure", map[string]any{"herdr_autostart": on}, nil); err != nil {
		m.d.logf("[%s] configure herdr autostart: %v", m.cfg.ID, err)
	}
	return nil
}

// herdrAutostartState is what GET /api/v1/machines/{mid}/herdr {autostart} answers: the setting in force. Running is
// whether the machine's herdr answers now, and Known false when the node could not say (it is offline, or older).
// Installing: the node is installing herdr (a "Start" on a machine without it, node/herdrinstall.go); InstallError why
// the last install failed.
type herdrAutostartState struct {
	Autostart    bool   `json:"autostart"`
	Running      bool   `json:"running"`
	Known        bool   `json:"known"`
	Installing   bool   `json:"installing,omitempty"`
	InstallError string `json:"install_error,omitempty"`
}

func (d *Devices) herdrAutostartState(ctx context.Context, m *Machine) herdrAutostartState {
	out := herdrAutostartState{Autostart: m.herdrAutostart(ctx)}
	m.mu.Lock()
	out.Running, out.Known = m.info.HerdrRunning, m.info.HerdrRunningKnown
	out.Installing, out.InstallError = m.herdrInstall.installing, m.herdrInstall.err
	m.mu.Unlock()
	return out
}

// herdrStatusFresh is how long an answer from the node stands for the apps reading the row (each Agents list polls).
var herdrStatusFresh = 3 * time.Second

// noteHerdrRunning keeps what the node said about herdr's server, for the next read of the row.
func (m *Machine) noteHerdrRunning(running, known bool) {
	m.mu.Lock()
	m.info.HerdrRunning, m.info.HerdrRunningKnown = running, known
	m.herdrAsked = time.Now()
	m.mu.Unlock()
}

// refreshHerdrRunning asks the node whether herdr's server answers now. node.info only said so when the node
// connected: a node that starts herdr itself a moment later (it waits for node.configure, which comes after
// node.info), a "Start" from another app, or a `herdr server stop` since, all kept the Agents list's notice wrong until
// the node reconnected (#1916). An older node has no herdr.server.status: what node.info said stays.
func (d *Devices) refreshHerdrRunning(ctx context.Context, m *Machine) {
	m.mu.Lock()
	online, fresh := m.online, time.Since(m.herdrAsked) < herdrStatusFresh
	m.mu.Unlock()
	if !online || fresh {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var out struct {
		Running      bool   `json:"running"`
		Known        bool   `json:"known"`
		Installing   bool   `json:"installing"`
		InstallError string `json:"install_error"`
	}
	if err := m.call(ctx, "herdr.server.status", map[string]any{}, &out); err != nil {
		m.mu.Lock()
		m.herdrAsked = time.Now() // an older node: not again on every read
		m.mu.Unlock()
		return
	}
	m.noteHerdrRunning(out.Running, out.Known)
	m.mu.Lock()
	m.herdrInstall.installing, m.herdrInstall.err = out.Installing, out.InstallError
	m.mu.Unlock()
}

// getHerdr: GET /api/v1/machines/{mid}/herdr, the machine's engine row: "Start herdr when needed", and whether it runs now.
func (d *Devices) getHerdr(w http.ResponseWriter, r *http.Request, m *Machine) {
	d.refreshHerdrRunning(r.Context(), m)
	writeJSON(w, d.herdrAutostartState(r.Context(), m))
}

// putHerdr: PUT /api/v1/machines/{mid}/herdr {"autostart": bool}, the switch under Settings › Machines › Engine. Only for a
// machine whose set holds classic herdr: Herdr App runs its own herdr-core and has no server for the node to start.
func (d *Devices) putHerdr(w http.ResponseWriter, r *http.Request, m *Machine) {
	var in struct {
		Autostart *bool `json:"autostart"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Autostart == nil {
		writeErr(w, http.StatusBadRequest, i18n.Err("devices-api.need_autostart"))
		return
	}
	if !slices.Contains(d.engineChoiceOf(m.cfg.ID).Hosts, proto.EngineHerdr) {
		writeErr(w, http.StatusNotFound, i18n.Err("devices-api.not_herdr"))
		return
	}
	if err := m.setHerdrAutostart(r.Context(), *in.Autostart); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d.changed()
	writeJSON(w, d.herdrAutostartState(r.Context(), m))
}

// postHerdrStart: POST /api/v1/machines/{mid}/herdr/start, the "Start" button beside "herdr is not running on this computer". The
// node starts it in a clean login shell (node herdr.server.start); the answer says whether it now answers.
func (d *Devices) postHerdrStart(w http.ResponseWriter, r *http.Request, m *Machine) {
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second) // the node waits up to 20s for its socket
	defer cancel()
	var out struct {
		Running bool `json:"running"`
	}
	if err := m.call(ctx, "herdr.server.start", map[string]any{}, &out); err != nil {
		// An older node has no such method: say that rather than that herdr failed to start.
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	m.noteHerdrRunning(out.Running, true) // the next read of the row says so too, not what node.info said
	d.changed()
	writeJSON(w, herdrAutostartState{Autostart: m.herdrAutostart(ctx), Running: out.Running, Known: true})
}
