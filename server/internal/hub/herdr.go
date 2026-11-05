package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// herdrAutostartKey is where a machine's own choice is kept. hub.json's machines[].herdr_autostart is only the
// default now: the switch in 设置 › 机器 › 引擎 answers for one machine, and an older hub never wrote this key
// (docs/engines.md「herdr 没在运行时」).
func herdrAutostartKey(machine string) string { return "herdr_autostart/" + machine }

// herdrAutostart is whether the machine's node may start herdr's server when it is not running: what the user chose
// for this machine, else hub.json's value, else on.
func (m *Machine) herdrAutostart(ctx context.Context) bool {
	if m.hub.store != nil {
		var on bool
		if ok, err := m.hub.store.Setting(ctx, herdrAutostartKey(m.cfg.ID), &on); ok && err == nil {
			return on
		}
	}
	return m.cfg.HerdrAutostart == nil || *m.cfg.HerdrAutostart
}

// setHerdrAutostart stores the machine's choice and hands it to the node, as configureNode does when it connects.
func (m *Machine) setHerdrAutostart(ctx context.Context, on bool) error {
	if m.hub.store == nil {
		return fmt.Errorf("没有可保存设置的存储")
	}
	if err := m.hub.store.PutSetting(ctx, herdrAutostartKey(m.cfg.ID), on); err != nil {
		return err
	}
	call, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := m.call(call, "node.configure", map[string]any{"herdr_autostart": on}, nil); err != nil {
		m.hub.logf("[%s] configure herdr autostart: %v", m.cfg.ID, err)
	}
	return nil
}

// herdrAutostartState is what GET /api/v1/machines/{mid}/herdr {autostart} answers: the setting in force. Running is
// whether the machine's herdr answers now, and Known false when the node could not say (it is offline, or older).
type herdrAutostartState struct {
	Autostart bool `json:"autostart"`
	Running   bool `json:"running"`
	Known     bool `json:"known"`
}

func (h *Hub) herdrAutostartState(ctx context.Context, m *Machine) herdrAutostartState {
	out := herdrAutostartState{Autostart: m.herdrAutostart(ctx)}
	m.mu.Lock()
	out.Running, out.Known = m.info.HerdrRunning, m.info.HerdrRunningKnown
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
// node.info), a 启动 from another app, or a `herdr server stop` since, all kept the Agents list's notice wrong until
// the node reconnected (#1916). An older node has no herdr.server.status: what node.info said stays.
func (h *Hub) refreshHerdrRunning(ctx context.Context, m *Machine) {
	m.mu.Lock()
	online, fresh := m.online, time.Since(m.herdrAsked) < herdrStatusFresh
	m.mu.Unlock()
	if !online || fresh {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var out struct {
		Running bool `json:"running"`
		Known   bool `json:"known"`
	}
	if err := m.call(ctx, "herdr.server.status", map[string]any{}, &out); err != nil {
		m.mu.Lock()
		m.herdrAsked = time.Now() // an older node: not again on every read
		m.mu.Unlock()
		return
	}
	m.noteHerdrRunning(out.Running, out.Known)
}

// getHerdr: GET /api/v1/machines/{mid}/herdr, the machine's engine row: 需要时自动启动 herdr, and whether it runs now.
func (h *Hub) getHerdr(w http.ResponseWriter, r *http.Request, m *Machine) {
	h.refreshHerdrRunning(r.Context(), m)
	writeJSON(w, h.herdrAutostartState(r.Context(), m))
}

// putHerdr: PUT /api/v1/machines/{mid}/herdr {"autostart": bool}, the switch under 设置 › 机器 › 引擎. Only for a
// machine whose engine is classic herdr: Herdr App runs its own herdr-core and has no server for the node to start.
func (h *Hub) putHerdr(w http.ResponseWriter, r *http.Request, m *Machine) {
	var in struct {
		Autostart *bool `json:"autostart"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Autostart == nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("需要 autostart"))
		return
	}
	if e := h.engineOf(m.cfg.ID); e != proto.EngineHerdr && e != proto.EngineHerdrBoth {
		writeErr(w, http.StatusNotFound, fmt.Errorf("这台电脑的引擎不是 herdr"))
		return
	}
	if err := m.setHerdrAutostart(r.Context(), *in.Autostart); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	h.broadcastMachines()
	writeJSON(w, h.herdrAutostartState(r.Context(), m))
}

// postHerdrStart: POST /api/v1/machines/{mid}/herdr/start, the 启动 button beside 「这台电脑的 herdr 没在运行」. The
// node starts it in a clean login shell (node herdr.server.start); the answer says whether it now answers.
func (h *Hub) postHerdrStart(w http.ResponseWriter, r *http.Request, m *Machine) {
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
	h.broadcastMachines()
	writeJSON(w, herdrAutostartState{Autostart: m.herdrAutostart(ctx), Running: out.Running, Known: true})
}
