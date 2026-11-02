package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/herdrcore"
	"github.com/chyroc/jarvis/server/internal/host"
	"github.com/chyroc/jarvis/server/internal/orcart"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// A machine's node drives exactly one terminal host, its engine: herdr (the default, as every node did before),
// herdr-core (the Herdr desktop app) or orca (the Orca desktop app). The user picks it per machine in the app; the hub stores it and tells the node
// with engine.use when it connects and when it changes (docs/protocol.md「引擎」). Running both side by side made each
// slow the other down, so they never are.

// engineState is the engine in use and the change stream watching it.
type engineState struct {
	mu   sync.Mutex
	name string             // "" until engine.use: herdr
	base context.Context    // Serve's context, nil before it
	stop context.CancelFunc // ends the current engine's watcher

	use   sync.Mutex    // one engine.use at a time, applied whole (the last one to arrive wins)
	ready chan struct{} // closed once the hub said which engine (configured)
	once  sync.Once
}

// configWait is how long a new connection waits for the hub to say which engine before it starts on herdr: a hub
// that stores a choice sends engine.use first; one without a choice (or an old hub) lists sessions, which counts too.
var configWait = 3 * time.Second

func (n *Node) readyCh() chan struct{} {
	n.eng.mu.Lock()
	defer n.eng.mu.Unlock()
	if n.eng.ready == nil {
		n.eng.ready = make(chan struct{})
	}
	return n.eng.ready
}

// configured: the engine is settled (engine.use, or the hub listing sessions without it): polling may start.
func (n *Node) configured() {
	ch := n.readyCh()
	n.eng.once.Do(func() { close(ch) })
}

// startWhenConfigured starts polling and the engine's change stream once the engine is settled, or after configWait:
// a node never polls or watches herdr on a machine whose engine is herdr-core, not even for the first seconds.
func (n *Node) startWhenConfigured(ctx context.Context) {
	select {
	case <-n.readyCh():
	case <-time.After(configWait):
	case <-ctx.Done():
		return
	}
	go n.pollAgents(ctx)
	go n.watchCodexCards(ctx)
	n.watchEngine(ctx)
}

// engineHosts are the hosts of an engine: herdr at its default socket (what every node had before engines),
// herdr-core at the socket hc would use, or Orca's runtime. Each logs when it becomes unreachable and reachable again.
func engineHosts(engine string, logf func(format string, args ...any)) []host.Host {
	switch engine {
	case proto.EngineHerdrCore:
		c := herdrcore.New()
		c.Log = logf
		return []host.Host{&herdrCoreHost{c: c}}
	case proto.EngineOrca:
		c := orcart.New()
		c.Log = logf
		return []host.Host{newOrcaHost(c)}
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
// new engine's starts, and a fresh session list goes to the hub once built (in the background: the reply does not wait
// for it). The engine in use already: nothing happens.
//
// Switches take turns (n.eng.use). A refresh running meanwhile drops its result (n.engEpoch), and every terminal
// stream of the old engine ends with terminal.end, so the phone does not hold one of its panes.
func (n *Node) useEngine(ctx context.Context, engine string) error {
	if _, ok := proto.EngineNames[engine]; !ok {
		return fmt.Errorf("unknown engine %q", engine)
	}
	n.eng.use.Lock()
	defer n.eng.use.Unlock()
	if n.engine() == engine {
		return nil
	}
	n.eng.mu.Lock()
	n.hosts.Set(engineHosts(engine, n.logf)...)
	n.eng.name = engine
	n.mu.Lock()
	n.engEpoch++
	n.hostAgents, n.layouts, n.agents, n.panePIDs, n.lastList = nil, nil, map[string]host.Agent{}, map[string]panePID{}, ""
	n.lastPanes = nil
	n.mu.Unlock()
	n.endTermStreams("the machine's terminal engine changed")
	traexCache.Lock()
	traexCache.panes, traexCache.status = map[int]string{}, map[string]traexState{}
	traexCache.Unlock()
	n.startWatchLocked()
	n.eng.mu.Unlock()
	n.logf("engine: now %s", engine)

	// The new engine's list goes to the hub when it is ready, not before the reply: building it asks the engine, scans
	// processes and reads every agent's transcript, which on a machine with dozens of agents and cold caches took
	// longer than the hub's 30 s for engine.use, and the app's engine picker waits on that call (#635).
	bg := context.WithoutCancel(ctx)
	go func() { n.publish(n.sessionList(bg, true)) }()
	return nil
}

// endTermStreams stops every terminal stream and tells the hub why (terminal.end), as when one ends on its own.
func (n *Node) endTermStreams(why string) {
	n.mu.Lock()
	terms := n.terms
	n.terms = nil
	n.mu.Unlock()
	for id, s := range terms {
		s.mu.Lock()
		control := s.ctl != nil
		s.mu.Unlock()
		s.cancel()
		if n.peer != nil {
			_ = n.peer.Notify("terminal.end", map[string]any{"session_id": id, "error": why, "control": control})
		}
	}
}

// herdrCLIVersion is `herdr --version` ("" without herdr): the version without talking to a running herdr.
func herdrCLIVersion(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "herdr", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "herdr"))
}

func (n *Node) logf(format string, args ...any) {
	if n.logger != nil {
		n.logger.Printf(format, args...)
	}
}

// engines is what node.info reports of each engine on this machine. herdr is available when it answers, its socket
// file is there or its binary is on PATH — when herdr is not the engine it is not asked: its version comes from
// `herdr --version`. herdr-core when its socket answers hello (the one herdr-core call a node on herdr makes, once per
// connection). Orca when its runtime metadata is there and its socket answers status.get with a protocol Jarvis speaks
// (Note says why not when it answers with another). herdrVersion is what the herdr host already said ("" when not
// asked).
func (n *Node) engines(ctx context.Context, herdrVersion string) []proto.Engine {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	hv := herdrVersion
	if hv == "" && n.hosts.Get(host.Herdr) == nil {
		hv = herdrCLIVersion(ctx)
	}
	_, statErr := os.Stat(herdr.SocketPath())
	_, pathErr := exec.LookPath("herdr")
	hcv := herdrcore.New().Version(ctx)
	return []proto.Engine{
		{ID: proto.EngineHerdr, Name: proto.EngineNames[proto.EngineHerdr], Available: hv != "" || statErr == nil || pathErr == nil,
			Version: hv},
		{ID: proto.EngineHerdrCore, Name: proto.EngineNames[proto.EngineHerdrCore], Available: hcv != "", Version: hcv},
		orcaEngine(ctx, orcaClient()),
	}
}

// orcaClient is the Orca runtime node.info asks; tests point it at a fake one.
var orcaClient = orcart.New

// orcaEngine is Orca as found on this machine: available when its runtime answers with a protocol Jarvis speaks.
func orcaEngine(ctx context.Context, c *orcart.Client) proto.Engine {
	e := proto.Engine{ID: proto.EngineOrca, Name: proto.EngineNames[proto.EngineOrca]}
	st, err := c.Probe(ctx)
	if st != nil {
		e.Version = st.AppVersion
	}
	switch {
	case err == nil:
		e.Available = true
	case errors.Is(err, orcart.ErrIncompatible):
		e.Note = "这个版本的 Orca 和 Jarvis 不兼容，请把两边都更新到最新版"
	}
	return e
}
