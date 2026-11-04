package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/herdrcore"
	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/orcart"
	"github.com/nwverse/persona/server/internal/proto"
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

	// botmux: whether botmux's sessions are listed next to the engine's (useBotmux); nil until the hub said, or
	// listed sessions without saying. botmuxSaid is the same "not nil", read without the lock (botmuxPending).
	botmux     *bool
	botmuxSaid atomic.Bool
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
// herdr-core at the socket hc would use, Orca's runtime, or herdr and herdr-core together (EngineHerdrBoth): the
// hosts stay isolated from each other — their refreshes run in parallel and each watcher only refreshes its own host
// (sessions.go, herdrevents.go).
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
	case proto.EnginePaseo:
		return nil // not a terminal: its sessions are its runtime's (runtimecontrol.go)
	case proto.EngineHerdrBoth:
		hc := herdr.New()
		hc.Log = logf
		cc := herdrcore.New()
		cc.Log = logf
		return []host.Host{&herdrHost{c: hc}, &herdrCoreHost{c: cc}}
	}
	c := herdr.New()
	c.Log = logf
	return []host.Host{&herdrHost{c: c}}
}

// defaultHostKind is the host that pane-less operations (starting and resuming sessions, renaming, mode menus) go to:
// the engine itself, except for the combined engine, whose full-capability host is classic herdr.
func defaultHostKind(engine string) string {
	if engine == proto.EngineHerdrBoth {
		return host.Herdr
	}
	return engine
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
	// Each registered watcher gets its own stream: herdr's pane-aware subscription, every other host's whole-host
	// stream (watchHost). The combined engine starts both; one host's events never refresh the other host.
	for _, h := range n.hosts.All() {
		w, ok := h.(host.Watcher)
		if !ok {
			continue
		}
		if h.Kind() == host.Herdr {
			go n.watchHerdr(ctx)
		} else {
			go n.watchHost(ctx, w, n.hostSig[h.Kind()]) // resetSignals built one for every Watcher host
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
	n.hosts.Set(nodeHosts(engine, n.eng.botmux != nil && *n.eng.botmux, n.logf)...)
	n.resetSignals()
	n.eng.name = engine
	n.mu.Lock()
	n.engEpoch++
	epoch := n.engEpoch
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
	// longer than the hub's 30 s for engine.use, and the app's engine picker waits on that call (#635). Built outside
	// n.eng.use, so a list finished after a later switch is dropped: it would cover the newer engine's until the next
	// poll (the hub would see sessions vanish and finish).
	go func() {
		list := n.sessionList(ctx, true)
		n.mu.Lock()
		current := n.engEpoch == epoch
		n.mu.Unlock()
		if current {
			n.publish(list)
		}
	}()
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
// file is there or its binary is on PATH — when herdr is not a registered host it is not asked: its version comes from
// `herdr --version`. herdr-core when its socket answers hello (the one herdr-core call a node without it makes, once
// per connection). The combined engine when both are there. Orca when its runtime metadata is there and its socket
// answers status.get with a protocol Persona speaks (Note says why not when it answers with another). herdrVersion is
// what the registered herdr host already said ("" when not registered).
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
	herdrAvailable := hv != "" || statErr == nil || pathErr == nil
	coreAvailable := hcv != ""
	return []proto.Engine{
		{ID: proto.EngineHerdr, Name: proto.EngineNames[proto.EngineHerdr], Available: herdrAvailable, Version: hv},
		{ID: proto.EngineHerdrCore, Name: proto.EngineNames[proto.EngineHerdrCore], Available: coreAvailable, Version: hcv},
		// The combined choice is selectable only when both runtimes are there; its version is left out (the picker
		// already shows each runtime's version on its own row).
		{ID: proto.EngineHerdrBoth, Name: proto.EngineNames[proto.EngineHerdrBoth], Available: herdrAvailable && coreAvailable},
		orcaEngine(ctx, orcaClient()),
		n.paseoEngine(ctx),
	}
}

// orcaClient is the Orca runtime node.info asks; tests point it at a fake one.
var orcaClient = orcart.New

// orcaEngine is Orca as found on this machine: available when its runtime answers with a protocol Persona speaks.
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
		e.Note = "这个版本的 Orca 和 Persona 不兼容，请把两边都更新到最新版"
	}
	return e
}
