package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

// Each machine's node drives one terminal engine (herdr or herdr-core), picked by the user in the app and kept here:
// one settings key holding machine id → engine, herdr for a machine never set. The hub tells the node with
// engine.use when it connects (before the first session list) and when the user changes it (docs/protocol.md「引擎」).

// enginesKey is the settings key holding map[machineID]engine.
const enginesKey = "machine_engines"

// engineChoices is the engine of each machine, loaded from settings on first use.
type engineChoices struct {
	mu     sync.Mutex
	loaded bool
	byID   map[string]string
}

func (h *Hub) loadEnginesLocked(ctx context.Context) {
	e := &h.engines
	if e.loaded {
		return
	}
	e.byID = map[string]string{}
	if h.store == nil {
		e.loaded = true
		return
	}
	var m map[string]string
	if _, err := h.store.Setting(ctx, enginesKey, &m); err != nil {
		h.logf("engines: %v", err) // retried on the next use
		return
	}
	for id, engine := range m {
		e.byID[id] = engine
	}
	e.loaded = true
}

// engineOf is the machine's engine: what the user chose, else herdr.
func (h *Hub) engineOf(machine string) string {
	h.engines.mu.Lock()
	defer h.engines.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h.loadEnginesLocked(ctx)
	if engine := h.engines.byID[machine]; engine != "" {
		return engine
	}
	return proto.EngineHerdr
}

// errUnknownEngine: not one of proto.EngineNames.
var errUnknownEngine = errors.New("unknown engine")

// setEngine stores the machine's engine.
func (h *Hub) setEngine(ctx context.Context, machine, engine string) error {
	if _, ok := proto.EngineNames[engine]; !ok {
		return fmt.Errorf("%w %q (herdr, herdr-core)", errUnknownEngine, engine)
	}
	e := &h.engines
	e.mu.Lock()
	defer e.mu.Unlock()
	h.loadEnginesLocked(ctx)
	if !e.loaded {
		return errors.New("engines: settings not readable")
	}
	next := map[string]string{}
	for id, v := range e.byID {
		next[id] = v
	}
	next[machine] = engine
	if h.store != nil {
		if err := h.store.PutSetting(ctx, enginesKey, next); err != nil {
			return err
		}
	}
	e.byID = next
	return nil
}

// useEngine tells a connected node which engine to drive. A node too old for engine.use keeps herdr, all it knows.
func (m *Machine) useEngine(ctx context.Context, peer *rpc.Peer) {
	engine := m.hub.engineOf(m.cfg.ID)
	if err := peer.Call(ctx, "engine.use", map[string]any{"engine": engine}, nil); err != nil {
		m.hub.logf("[%s] engine.use %s: %v", m.cfg.ID, engine, err)
	}
}

// putEngine is PUT /api/v1/machines/{mid}/engine {"engine": "herdr-core"}: stored even while the machine is offline
// (applied when it connects), sent to the node at once when online; answers the machine as listed.
func (h *Hub) putEngine(w http.ResponseWriter, r *http.Request, m *Machine) {
	var in struct {
		Engine string `json:"engine"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad body: %w", err))
		return
	}
	if err := h.setEngine(r.Context(), m.cfg.ID, in.Engine); err != nil {
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
	h.broadcastMachines()
	writeJSON(w, m.snapshot())
}
