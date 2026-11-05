package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Each machine's node drives a terminal engine (herdr, herdr-core, herdr-both for the two side by side, or orca),
// picked by the user in the app and kept here: one settings key holding machine id → engine, herdr for a machine never
// set. The hub tells the node with engine.use when it connects (before the first session list) and when the user
// changes it (docs/protocol.md「引擎」).

// enginesKey is the settings key holding map[machineID]engine.
const enginesKey = "machine_engines"

// engineChoices is the engine of each machine, loaded from settings on first use.
type engineChoices struct {
	mu       sync.Mutex
	loaded   bool
	failedAt time.Time // the last load that failed: not tried again for engineRetry
	byID     map[string]string
}

// engineRetry: after settings could not be read, how long engineOf answers from what it has (herdr) before trying
// again — it is called while machine lists are built.
const engineRetry = 30 * time.Second

func (d *Devices) loadEnginesLocked(ctx context.Context) {
	e := &d.engines
	if e.loaded || !e.failedAt.IsZero() && time.Since(e.failedAt) < engineRetry {
		return
	}
	e.byID = map[string]string{}
	if d.opts.Store == nil {
		e.loaded = true
		return
	}
	var m map[string]string
	if _, err := d.opts.Store.Setting(ctx, enginesKey, &m); err != nil {
		d.logf("engines: %v (retried in %s)", err, engineRetry)
		e.failedAt = time.Now()
		return
	}
	for id, engine := range m {
		e.byID[id] = engine
	}
	e.loaded = true
}

// engineOf is the machine's engine: what the user chose, else herdr.
func (d *Devices) engineOf(machine string) string {
	if engine, ok := d.storedEngine(machine); ok {
		return engine
	}
	return proto.EngineHerdr
}

// storedEngine is the engine the user chose for the machine; false when never chosen (or settings unreadable).
func (d *Devices) storedEngine(machine string) (string, bool) {
	d.engines.mu.Lock()
	defer d.engines.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d.loadEnginesLocked(ctx)
	engine, ok := d.engines.byID[machine]
	return engine, ok && engine != ""
}

// errUnknownEngine: not one of proto.EngineNames.
var errUnknownEngine = errors.New("unknown engine")

// setEngine stores the machine's engine.
func (d *Devices) setEngine(ctx context.Context, machine, engine string) error {
	if _, ok := proto.EngineNames[engine]; !ok {
		return fmt.Errorf("%w %q (herdr, herdr-core, herdr-both, orca)", errUnknownEngine, engine)
	}
	e := &d.engines
	e.mu.Lock()
	defer e.mu.Unlock()
	e.failedAt = time.Time{} // the user is waiting: try now
	d.loadEnginesLocked(ctx)
	if !e.loaded {
		return errors.New("engines: settings not readable")
	}
	next := map[string]string{}
	for id, v := range e.byID {
		next[id] = v
	}
	next[machine] = engine
	if d.opts.Store != nil {
		if err := d.opts.Store.PutSetting(ctx, enginesKey, next); err != nil {
			return err
		}
	}
	e.byID = next
	return nil
}

// useEngine tells a connected node the engine the user chose, if any: a machine never set is left alone (its node
// starts on herdr, and an old node never sees a method it does not know). Calls take turns per machine and each sends
// the value stored when it runs, so the node ends on the latest choice. A node that refuses it (too old) keeps herdr:
// logged, the stored choice stands.
func (m *Machine) useEngine(ctx context.Context, peer *rpc.Peer) {
	m.engineMu.Lock()
	defer m.engineMu.Unlock()
	engine, ok := m.d.storedEngine(m.cfg.ID)
	if !ok {
		return
	}
	if err := peer.Call(ctx, "engine.use", map[string]any{"engine": engine}, nil); err != nil {
		m.d.logf("[%s] the node did not take engine %s (it stays on its own engine until updated): %v", m.cfg.ID, engine, err)
	}
}

// putEngine is PUT /api/v1/machines/{mid}/engine {"engine": "herdr-core"}: stored even while the machine is offline
// (applied when it connects), sent to the node at once when online; answers the machine as listed.
func (d *Devices) putEngine(w http.ResponseWriter, r *http.Request, m *Machine) {
	var in struct {
		Engine string `json:"engine"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad body: %w", err))
		return
	}
	if err := d.setEngine(r.Context(), m.cfg.ID, in.Engine); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, errUnknownEngine) {
			code = http.StatusBadRequest
		}
		writeErr(w, code, err)
		return
	}
	m.mu.Lock()
	peer := m.peer
	m.mu.Unlock()
	if peer != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		m.useEngine(ctx, peer)
		cancel()
	}
	d.reportNow()
	writeJSON(w, d.decorate(m, m.snapshot()))
}
