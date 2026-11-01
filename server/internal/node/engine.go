package node

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/herdrcore"
	"github.com/chyroc/jarvis/server/internal/host"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// A machine's node drives exactly one terminal host, its engine: herdr (the default, as every node did before) or
// herdr-core (the Herdr desktop app). The user picks it per machine in the app; the hub stores it and tells the node
// with engine.use when it connects and when it changes (docs/protocol.md「引擎」). Running both side by side made each
// slow the other down, so they never are.

// engineState is the engine in use and the change stream watching it.
type engineState struct {
	mu   sync.Mutex
	name string             // "" until engine.use: herdr
	base context.Context    // Serve's context, nil before it
	stop context.CancelFunc // ends the current engine's watcher
}

// engineHosts are the hosts of an engine: herdr at its default socket (what every node had before engines), or
// herdr-core at the socket hc would use. Each logs when it becomes unreachable and reachable again.
func engineHosts(engine string, logf func(format string, args ...any)) []host.Host {
	if engine == proto.EngineHerdrCore {
		c := herdrcore.New()
		c.Log = logf
		return []host.Host{&herdrCoreHost{c: c}}
	}
	c := herdr.New()
	c.Log = logf
	return []host.Host{&herdrHost{c: c}}
}

// engine is the engine in use.
func (n *Node) engine() string {
	n.eng.mu.Lock()
	defer n.eng.mu.Unlock()
	if n.eng.name == "" {
		return proto.EngineHerdr
	}
	return n.eng.name
}

// watchEngine starts watching the engine in use for as long as ctx lasts (Serve); engine.use moves it to the new one.
func (n *Node) watchEngine(ctx context.Context) {
	n.eng.mu.Lock()
	defer n.eng.mu.Unlock()
	n.eng.base = ctx
	n.startWatchLocked()
}

// startWatchLocked ends the current engine's change stream and starts the one of the engine in use: herdr's own
// (watchHerdr, as before engines), or the stream of a host that pushes changes (watchHost). n.eng.mu is held.
func (n *Node) startWatchLocked() {
	if n.eng.stop != nil {
		n.eng.stop()
		n.eng.stop = nil
	}
	if n.eng.base == nil {
		return // not serving (tests drive the node without Serve)
	}
	ctx, cancel := context.WithCancel(n.eng.base)
	n.eng.stop = cancel
	if n.eng.name == "" || n.eng.name == proto.EngineHerdr {
		go n.watchHerdr(ctx)
		return
	}
	for _, h := range n.hosts.All() {
		if w, ok := h.(host.Watcher); ok {
			go n.watchHost(ctx, w)
		}
	}
}

// useEngine switches the node to engine: the old host's agents, places and caches go, its change stream stops, the
// new engine's starts, and a fresh session list goes to the hub. The engine in use already: nothing happens.
func (n *Node) useEngine(ctx context.Context, engine string) error {
	if _, ok := proto.EngineNames[engine]; !ok {
		return fmt.Errorf("unknown engine %q", engine)
	}
	if n.engine() == engine {
		return nil
	}
	n.eng.mu.Lock()
	n.hosts.Set(engineHosts(engine, n.logf)...)
	n.eng.name = engine
	n.mu.Lock()
	n.hostAgents, n.layouts, n.agents, n.panePIDs, n.lastList = nil, nil, map[string]host.Agent{}, map[string]panePID{}, ""
	n.mu.Unlock()
	traexCache.Lock()
	traexCache.panes, traexCache.status = map[int]string{}, map[string]traexState{}
	traexCache.Unlock()
	n.startWatchLocked()
	n.eng.mu.Unlock()
	n.logf("engine: now %s", engine)

	list := n.sessionList(ctx, true)
	n.mu.Lock()
	n.lastList = listKey(list)
	n.mu.Unlock()
	if n.peer != nil {
		_ = n.peer.Notify("sessions.changed", map[string]any{"sessions": list})
	}
	return nil
}

func (n *Node) logf(format string, args ...any) {
	if n.logger != nil {
		n.logger.Printf(format, args...)
	}
}

// engines is what node.info reports of each engine on this machine. herdr is available when it answers, its socket
// file is there or its binary is on PATH; herdr-core when its socket answers hello (the one herdr-core call a node
// on herdr makes, once per connection). herdrVersion is what the herdr host already said ("" when not asked).
func (n *Node) engines(ctx context.Context, herdrVersion string) []proto.Engine {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	hv := herdrVersion
	if hv == "" && n.hosts.Get(host.Herdr) == nil {
		hv = herdr.New().Version(ctx)
	}
	_, statErr := os.Stat(herdr.SocketPath())
	_, pathErr := exec.LookPath("herdr")
	hcv := herdrcore.New().Version(ctx)
	return []proto.Engine{
		{ID: proto.EngineHerdr, Name: proto.EngineNames[proto.EngineHerdr], Available: hv != "" || statErr == nil || pathErr == nil,
			Version: hv},
		{ID: proto.EngineHerdrCore, Name: proto.EngineNames[proto.EngineHerdrCore], Available: hcv != "", Version: hcv},
	}
}
