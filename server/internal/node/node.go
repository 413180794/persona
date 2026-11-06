// Package node runs on every machine: it drives herdr/claude locally and talks to the hub over stdio RPC.
package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nwverse/persona/server/internal/identity"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/server/internal/assistantskills"
	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/nodeupdate"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/quota"
	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/streams"
	"github.com/nwverse/persona/server/internal/transcript"
)

type Node struct {
	Version string
	updater *nodeupdate.Manager // resident-owned, shared across attach connections

	// decided: calls another PermissionRequest hook answered (hookdecided.go).
	decided decisions

	// bootAt: when this node started. A session whose record says it was started in a pane before this ran in the
	// previous herdr server, so the restart is what ended it (interrupted.go).
	bootAt time.Time

	// sock: the node socket file this node listens on (hooks.go), to tell it from one put in its place.
	sockMu sync.Mutex
	sock   os.FileInfo

	// claudeStart, claudeReady: when set, how long a start waits for the shell to run claude, then for claude's
	// input box, in place of claudeStartWait and claudeReadyWait (tests; control.go).
	claudeStart, claudeReady time.Duration

	// doctorAt replaces the places node.doctor looks at (tests; doctor_rpc.go).
	doctorAt func() DoctorOptions

	home string
	dir  string // ~/.jarvis
	bin  string // absolute path of this executable
	uid  string // the computer's own id (machineuid.go); empty when it could not be read or made
	// managed: launchers kept by the hub (managed.go)
	managed  managedLaunchers
	hosts    *host.Registry // the terminal hosts, by kind (hosts.go): the one of the machine's engine
	eng      engineState    // which engine, and its change stream (engine.go)
	hstart   herdrStarter   // starting herdr's server when it is not running (herdrstart.go)
	hinstall herdrInstaller // installing herdr when there is none (herdrinstall.go)
	// engEpoch counts engine switches (n.mu): a refresh applies its result only when no switch happened meanwhile.
	engEpoch uint64
	peer     *rpc.Peer
	logger   *log.Logger
	// lcu drives amontlabs/lcu for the computer-use plugin when that backend is selected (lcu_mcp.go).
	lcuMu   sync.Mutex
	lcuSess *lcuSession

	// claudeStartDir: the CLAUDE_CONFIG_DIR plain `claude` is started with (hub.json machines[].claude_start_dir,
	// managed.go); "" leaves claude to its default.
	claudeStartDir atomic.Pointer[string]
	fsw            fsWatcher // the folders the web panels have open (fswatch.go)

	mu      sync.Mutex
	state   *stateFile
	files   map[string]*transcript.File // session id -> parsed transcript
	paths   map[string]string           // session id -> transcript path
	pathsAt map[string]time.Time        // when a path not told by a hook was last chosen (transcriptPath)
	watches map[string]*watch
	agents  map[string]host.Agent // session id -> live agent
	// onAgentsList: the parked sessions whose pane is on Claude's agents list, listed in it read-only (claudebg.go).
	onAgentsList map[string]bool
	// lastPanes: the pane each session was last seen live in (it is no longer live when its agent exits or is
	// suspended), to tell the two apart when a send finds it gone on a host that cannot resume it.
	lastPanes map[string]string
	launches  map[string]claudeLaunch    // herdr pane -> how Persona started its Claude, for herdr's restore (herdrresume.go)
	panes     map[string]string          // pane id -> session id (learned from hooks)
	layouts   map[string]layout          // host kind -> space / slot names from its last snapshot
	procs     map[string]*assistantProc  // headless assistant conversations (assistant.go)
	spare     *assistantProc             // warm process waiting for the next new conversation
	akeys     map[string]*assistantProc  // the key a Codex conversation's MCP server reports until its thread exists
	lastList  string                     // serialized last sessions.changed payload
	lastRevs  map[string]int64           // session id -> rev as last pushed (sessions.changed or sessions.revs)
	metas     map[string]quickMeta       // transcript path -> cached meta
	bgScans   map[string]*bgScan         // transcript path -> incremental background-task scan
	panePIDs  map[string]panePID         // pane -> its claude pid (machines without herdr's claude integration)
	paneSeen  map[string]time.Time       // claude pane without a host session id -> first seen (zero: there when node started)
	modes     map[string]sessionModes    // session id -> mode / effort set from the app (modes.go)
	terms     map[string]*termStream     // session id -> its terminal streaming to the hub (terminal.go)
	fulls     map[string]*fullClient     // client id -> a full herdr client running for the web (fullclient.go)
	focus     focusState                 // focus sync for the full views (focus.go)
	suggests  map[string]*suggestState   // session id -> Claude's suggested next message (suggestion.go)
	driving   map[string]bool            // session id -> Persona is working its TUI (a picker it opened is not "blocked")
	dialogs   paneDialogs                // pane -> the dialog its last screen read showed (dialogstatus.go)
	firstWait sync.WaitGroup             // first messages being checked (firstprompt.go)
	engLists  sync.WaitGroup             // new engines' session lists being built in the background (useEngine)
	lostFirst lostPrompts                // session id -> why its first message did not go in (firstprompt.go)
	events    *herdrEvents               // herdr's event stream (herdrevents.go)
	hostSig   map[string]*watcherSignals // change signals of the registered watcher hosts, by host kind (herdrevents.go)
	cdirs     *claudeDirs                // Claude Code config directories in use (claudedirs.go)
	hooks     *hookTurns                 // per-session turn counters from Persona's hooks (hookturn.go)
	codices   *codexRegistry
	usageWin  usageWindowsState // subscription rate-limit windows (usagewindows.go, #2432)
	machineID string
	sends     sentLog         // recent client_msg_ids (sendsafe.go)
	echoes    echoLog         // sends tied to their transcript lines (echo.go)
	typing    typingLocks     // one send at a time per session (sendsafe.go)
	cx        codexCards      // Codex questions and approvals listed to the hub (codexask.go)
	kept      map[string]bool // sessions always listed (node.configure keep_sessions)
	forked    map[string]bool // sessions 分支到新聊天 made (fork.go): listed however short they are
	starts    startLog        // sessions starting in the background (starting.go)
	ups       uploads         // files arriving from the phone (upload.go)
	sysOnce   sync.Once       // system: read once (metrics.go)
	sys       proto.System

	// usageRetried: when a conversation's message was last sent again after its transcript's usage was repaired
	// (assistant_usage.go): once in a while at most. Under mu; nil until the first.
	usageRetried map[string]time.Time

	// runtimes: agent runtimes without a terminal, connected on runtime.sock (runtimes.go); runtimeSock overrides the
	// socket path (tests).
	runtimes    *runtimeRegistry
	runtimeSock string
	// runtimeHost: the runtime socket this Node lists runtimes from — the resident node's, shared by its Nodes
	// (attach.go), or its own for the one connection it serves (runtimehost.go).
	runtimeHost *RuntimeHost
	// keeper: a resident node's conversations between hub connections (assistant_keep.go); nil without one.
	keeper *assistantKeeper

	// codexStarting: panes a Codex start is opening (codex_start.go): no thread is guessed for them meanwhile.
	codexStarting map[string]bool

	expectedTokensMu sync.Mutex
	expectedTokens   map[string]map[string]string
	expectedSynced   bool

	// ctxSent: assistant conversation → the snapshot sections it has seen; ctxTools: the tools its process last
	// started with (assistant_context.go).
	ctxSent  map[string]map[string]string
	ctxTools map[string][]string

	// ports: the agents' listening ports (ports.go); streams: the forwarded connections to them (streams.Mux).
	ports   portState
	streams *streams.Mux

	// hostAgents: each host's agents in its last answer (refreshHosts), kept for a host that does not answer.
	hostAgents map[string][]host.Agent
	// workspaceDirs avoids resolving every listed session directory every second.
	workspaceDirs workspaceDirectoryCache

	// pushLocks: one per session, held from reading its new lines to pushing them (pushLock).
	pushLocks map[string]*sync.Mutex
	// fileUse: when each parsed transcript in files was last asked for (a counter), to drop the least recently used.
	fileUse map[string]uint64
	useTick uint64

	// The Feishu gate for the assistant (larkgate.go): the hub's config, cached, and the writes 本会话总是允许 let through.
	larkCfg     proto.AssistantLark
	larkCfgAt   time.Time
	larkAllowed map[string]bool
	// gp is the proxy that adds the gates' credentials, made at its first use (gateproxy.go, #1317).
	gp *gateProxy
	// lark-cli (larkcligate.go): each command's risk level as lark-cli reported it, and where the binary is.
	larkRisks    map[string]string
	larkCLIPaths map[string]string

	// connectors are the hub's signed in, as the last assistant.prepare said (clicall.go).
	connectors []string
	clis       map[string]string // the connectors' command lines (assistant.prepare): name → connector id

	// claudeV is claude's version as last asked (agentplugin.go).
	claudeV claudeVersionCache

	// agentFilters: the extensions' filters of sessions kept out of the list (node.configure, agentfilters.go)
	agentFilters []proto.AgentFilter
}

func New(version string) (*Node, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	bin, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if p, err := filepath.EvalSymlinks(bin); err == nil {
		bin = p
	}
	// ssh sessions get a minimal PATH; herdr and claude usually live in ~/.local/bin.
	extra := []string{filepath.Join(home, ".local/bin"), filepath.Join(home, "bin"), filepath.Join(home, "go/bin"), "/opt/homebrew/bin", "/usr/local/bin", "/usr/local/go/bin"}
	os.Setenv("PATH", strings.Join(append(extra, os.Getenv("PATH")), string(os.PathListSeparator)))
	dir, err := identity.PrepareHome(home)
	if err != nil {
		return nil, err
	}
	n := &Node{
		Version:  version,
		bootAt:   time.Now().UTC(), // sessions started before this were the previous herdr server's (interrupted.go)
		home:     home,
		dir:      dir,
		bin:      bin,
		logger:   log.New(os.Stderr, "node: ", log.LstdFlags|log.Lmsgprefix),
		files:    map[string]*transcript.File{},
		paths:    map[string]string{},
		watches:  map[string]*watch{},
		agents:   map[string]host.Agent{},
		panes:    map[string]string{},
		procs:    map[string]*assistantProc{},
		akeys:    map[string]*assistantProc{},
		metas:    map[string]quickMeta{},
		bgScans:  map[string]*bgScan{},
		panePIDs: map[string]panePID{},
		modes:    map[string]sessionModes{},
		driving:  map[string]bool{},
		events:   newHerdrEvents(),
		cdirs:    newClaudeDirs(),
		hooks:    &hookTurns{},
		codices:  newCodexRegistry(),
		runtimes: newRuntimeRegistry(),
	}
	if n.uid, err = LoadMachineUID(dir); err != nil {
		n.logger.Printf("machine uid: %v", err) // the hubs then treat this computer as theirs alone
	}
	n.configureCodex(proto.CodexConfig{})
	n.hosts = host.NewRegistry(nodeHosts(proto.EngineHerdr, false, n.logger.Printf)...) // herdr until engine.use, botmux until the hub says
	n.resetSignals()
	n.state = newState()
	n.ports.kick = make(chan struct{}, 1)
	go sweepTerminalUploads(dir, time.Now()) // pasted files older than a week (upload_sweep.go)
	return n, nil
}

// ServeStdio runs the node protocol on stdin/stdout until the hub disconnects.
func (n *Node) ServeStdio(ctx context.Context) error {
	return n.Serve(ctx, os.Stdin, os.Stdout)
}

// Serve runs the node protocol for one hub connection on r / w until it ends. One Node serves one connection:
// `jarvisd node --attach` makes a new one per connection (attach.go).
func (n *Node) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if n.keeper != nil {
		defer n.handOverAssistants() // last, once nothing of this connection runs (assistant_keep.go)
	}
	if err := n.writeClaudeConfig(); err != nil {
		n.logger.Printf("write claude config: %v", err)
	}
	if n.updater != nil {
		w = &updateResponseWriter{Writer: w, responseWritten: n.updater.ResponseWritten}
	}
	n.peer = rpc.New("n", w)
	// A tool call whose caller went away (the assistant's turn stopped, a hook killed) ends on the hub too: an
	// approval it was waiting on is withdrawn, not left to run the call when allowed later (#2597).
	n.peer.Cancels = map[string]bool{"mcp.call": true}
	n.peer.OnRequest = n.handle
	n.streams = streams.New(n.peer.Notify)
	defer n.streams.CloseAll()
	defer n.closeFulls() // none of the web's full herdr clients outlives this hub connection (fullclient.go)
	n.peer.OnEvent = func(event string, params json.RawMessage) {
		if !n.streams.Event(event, params) && !n.onFullEvent(event, params) {
			n.onTerminalEvent(event, params)
		}
	}
	n.fsw.start(ctx, func(c fsChanged) { _ = n.peer.Notify("fs.changed", c) })
	defer n.fsw.close()
	n.cdirs.report = func(dirs []string) { _ = n.peer.Notify("claude.dirs", map[string]any{"dirs": dirs}) }
	n.codices.report = func(cfg proto.CodexConfig) { _ = n.peer.Notify("codex.inventory", cfg) }
	go n.runCodex(ctx)
	var adopted []*assistantProc
	if n.keeper != nil { // before the state loads: a conversation taken over has lost no background task
		adopted = n.keeper.adopt(n)
	}
	go func() {
		n.loadState(ctx)
		n.resumeAssistants(adopted)
	}()
	ln, err := n.listenHookSocket(ctx)
	if err != nil {
		n.logger.Printf("hook socket: %v", err)
	} else {
		defer ln.Close()
	}
	if rh := n.runtimeHost; rh != nil { // a resident node's: runtimes stay connected across hub connections
		rh.attach(n)
		defer rh.detach(n)
	} else if rh, err := n.listenRuntimeSocket(ctx); err != nil { // runtimes (runtimes.go): beside the engine, whichever it is
		n.logger.Printf("runtime socket: %v", err)
	} else {
		defer rh.detach(n)
	}
	go n.startWhenConfigured(ctx) // polling and the engine's events, once the hub said which engine (engine.go)
	go n.warmTranscripts(ctx)
	go n.reapAssistants(ctx)
	go n.meterUsage(ctx)
	go n.runUsageWindows(ctx.Done())
	go n.sampleMetrics(ctx)
	go n.watchPorts(ctx)
	n.startTodoTokensReconciler(ctx)
	return n.peer.Serve(ctx, r)
}

// loadState pulls this machine's session state from the hub (Postgres) and uploads a legacy sessions.json once.
func (n *Node) loadState(ctx context.Context) {
	n.state.mu.Lock()
	n.state.persist = func(id string, v sessionState) {
		_ = n.peer.Notify("state.put", map[string]any{"session_id": id, "state": v})
	}
	n.state.mu.Unlock()
	legacyPath := filepath.Join(n.dir, "sessions.json")
	if legacy := legacyState(legacyPath); len(legacy) > 0 {
		for id, v := range legacy {
			_ = n.peer.Notify("state.put", map[string]any{"session_id": id, "state": v})
		}
		n.state.merge(legacy)
		_ = os.Rename(legacyPath, legacyPath+".migrated")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var res struct {
		Sessions map[string]sessionState `json:"sessions"`
	}
	if err := n.peer.Call(ctx, "state.load", map[string]any{}, &res); err != nil {
		n.logger.Printf("load state from hub: %v", err)
		return
	}
	n.state.merge(res.Sessions)
	go n.recoverAssistantTasks(func(id, text string) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		launcher, execution := n.assistantModel(ctx, id)
		_, err := n.assistantSend(ctx, id, "", launcher, text, nil, "", "", execution)
		return err
	})
	panes := n.state.panes()
	n.mu.Lock()
	for pane, id := range panes {
		if _, ok := n.panes[pane]; !ok {
			n.panes[pane] = id
		}
	}
	n.mu.Unlock()
}

// writeClaudeConfig writes the files the sessions Persona starts load (hooks, MCP, skills; generatedFiles).
func (n *Node) writeClaudeConfig() error { return writeGeneratedConfig(n.dir, n.bin) }

func (n *Node) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if strings.HasPrefix(method, "node.update.") {
		return n.handleUpdate(ctx, method, params)
	}
	switch method {
	case "node.doctor": // the machine page's 这台电脑能做什么: node doctor's report, read-only unless {"fix":true} (doctor_rpc.go)
		return n.doctor(ctx, params, false)
	case "node.doctor.fix": // the same path as `node doctor --fix`: Persona's own generated files only
		return n.doctor(ctx, params, true)
	}
	if strings.HasPrefix(method, "plugin.node.") { // plugins' programs on this computer (plugins.go)
		return n.handlePlugins(ctx, method, params)
	}
	var p struct {
		Path             string        `json:"path"`
		Paths            []string      `json:"paths"`
		Offset           int64         `json:"offset"`
		Stream           bool          `json:"stream"`
		SessionID        string        `json:"session_id"`
		Cwd              string        `json:"cwd"`
		Launcher         string        `json:"launcher"`
		AgentType        string        `json:"agent_type"`
		Model            string        `json:"model"`
		ModelFingerprint string        `json:"model_fingerprint"`
		Runtime          string        `json:"runtime"`
		Prompt           string        `json:"prompt"`
		ResumeSessionID  string        `json:"resume_session_id"`
		WorkspaceID      string        `json:"workspace_id"`
		Text             string        `json:"text"`
		Images           []proto.Image `json:"images"`
		PreKeys          []string      `json:"pre_keys"`
		Keys             []string      `json:"keys"`
		Lines            int           `json:"lines"`
		BeforeSeq        int           `json:"before_seq"`
		AfterSeq         *int          `json:"after_seq"`
		Limit            int           `json:"limit"`
		SinceSeq         int           `json:"since_seq"`
		Source           string        `json:"source"`
		Format           string        `json:"format"`
		Staged           bool          `json:"staged"`
		Hash             string        `json:"hash"`
		Skip             int           `json:"skip"`
		Turns            int           `json:"turns"`
		LazyImages       bool          `json:"lazy_images"`
		FoldWork         bool          `json:"fold_work"`
		FromSeq          int           `json:"from_seq"`
		ThroughSeq       int           `json:"through_seq"`
		Name             string        `json:"name"`
		Pane             string        `json:"pane"`      // panes.*: a pane the host lists
		Direction        string        `json:"direction"` // panes.split: right or down
		TabID            string        `json:"tab_id"`    // tabs.*
		Label            string        `json:"label"`     // tabs.rename
		Kind             string        `json:"kind"`      // tabs.*, panes.git: the host kind ("" the engine's)
		Op               string        `json:"op"`        // panes.git: fetch, pull or push
		Action           string        `json:"action"`    // launchers.repair: which of its fixed repairs
		Branch           string        `json:"branch"`    // worktree.create
		TabName          *string       `json:"tab_name"`
		TZ               string        `json:"tz"` // the user's time zone
		// HTTP is a quota plugin's request to send to a launcher's gateway (launchers.http); nil asks for the gateway.
		HTTP           *quota.Request  `json:"http,omitempty"`
		Async          bool            `json:"async"`
		Title          *string         `json:"title"`
		PermissionMode string          `json:"permission_mode"`
		Effort         string          `json:"effort"`
		ResumeOnly     bool            `json:"resume_only"`
		AtItemID       string          `json:"at_item_id"` // session.fork: the reply to branch off at
		Refresh        bool            `json:"refresh"`    // launchers.models: ask again, not the kept list
		Configured     bool            `json:"configured"` // launchers.models, .probe, .http, .model.check: hub.json names this launcher
		Raw            json.RawMessage `json:"-"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("bad params: %w", err)
		}
	}
	if n.codexBlocked(p.SessionID) && (strings.HasPrefix(method, "session.") || strings.HasPrefix(method, "terminal.") || method == "assistant.send") {
		return nil, refusal("identity_conflict", "会话 ID 与另一来源重复，不能判断操作目标；未读取或发送", nil)
	}
	if n.codexBlocked(p.ResumeSessionID) && method == "session.start" {
		return nil, refusal("identity_conflict", "会话 ID 与另一来源重复，不能恢复", nil)
	}
	if method == "session.start" && n.externalCodex(p.ResumeSessionID) {
		return nil, refusal("codex_read_only", "外部 Codex 会话只能接入原 app-server，不能用启动器恢复", nil)
	}
	if n.externalCodex(p.SessionID) {
		switch method {
		case "session.keys", "session.screen", "session.screen.before", "terminal.watch", "session.close", "session.queue.edit", "session.rename", "assistant.send":
			return nil, refusal("codex_read_only", "这个会话没有终端或启动器控制能力，请在原 app-server 中操作", nil)
		case "session.modes":
			// Its effort, on the app-server running it (codex_effort.go); nothing else.
			if _, _, driven := n.codexEffortThread(p.SessionID); !driven {
				return nil, refusal("codex_read_only", "这个会话没有终端或启动器控制能力，请在原 app-server 中操作", nil)
			}
			if p.Model != "" {
				return nil, fmt.Errorf("Codex 的会话不支持在手机上切换模型")
			}
			return ok(n.setModes(ctx, p.SessionID, p.PermissionMode, p.Effort))
		case "session.interrupt":
			return ok(n.interruptCodex(ctx, p.SessionID))
		}
	}
	if method == "session.retire" { // before the runtimes: a session another tool owns is refused, not handed on
		closed, err := n.retire(ctx, p.SessionID)
		return map[string]any{"closed": closed}, err
	}
	// A session of a runtime (runtimes.go): the runtime answers every call about it, also for an older session it no
	// longer lists; one whose runtime is away is refused, never resumed in a terminal as a Claude session. An
	// assistant conversation a runtime runs is the assistant's to send to, stop and set modes on (its snapshot and
	// mode go with each message: assistant_runtime.go); the rest (pages, watch, images) is the runtime's.
	if assistantCall[method] && n.runtimeAssistant(p.SessionID) {
		// falls through to the assistant's branches below
	} else if h, err := n.runtimeFor(ctx, method, p.SessionID); err != nil {
		return nil, err
	} else if h != nil {
		if res, err, handled := n.runtimeHandle(ctx, h, method, params); handled {
			return res, err
		}
	} else if r, ok := n.terminalOnly(p.SessionID); ok { // a pi or opencode without its extension, any other agent (opencode.go)
		if res, err, handled := n.terminalOnlyCalls(ctx, method, p.SessionID, r); handled {
			return res, err
		}
	}
	switch method {
	case "session.attach":
		return ok(n.attachCodex(ctx, p.SessionID))
	case "ping": // the hub checking a tailcat connection is alive (its userspace TCP has no keepalive)
		return map[string]any{}, nil
	case "node.info":
		return n.info(ctx), nil
	case "herdr.server.start": // the 启动 button beside 「herdr 没在运行」 (herdrstart.go)
		return n.startHerdrServer(ctx)
	case "herdr.server.status": // whether it answers now: the hub asks when an app reads the row (herdrstart.go)
		running, known := n.herdrRunning(ctx)
		installing, installErr := n.herdrInstalling() // herdrinstall.go
		return map[string]any{"running": running, "known": known, "installing": installing, "install_error": installErr}, nil
	case "connector.list", "connector.chats", "connector.older", "connector.changes", "connector.search": // this Mac's own data (connectors.go)
		return localConnector(ctx, method, params)
	case "credential.gh_token": // the GitHub connector's token from this machine's gh login (credential.go)
		var c struct {
			Host string `json:"host"`
		}
		_ = json.Unmarshal(params, &c)
		return ghToken(ctx, c.Host)
	case "engine.use": // the engine the user chose for this machine (engine.go)
		var e struct {
			Engine string `json:"engine"`
		}
		if err := json.Unmarshal(params, &e); err != nil {
			return nil, err
		}
		if err := n.useEngine(ctx, e.Engine); err != nil {
			return nil, err
		}
		if n.runtimeHost != nil { // Paseo's agents come through its bridge, which the node runs (paseobridge.go)
			n.runtimeHost.setPaseoBridge(n.engine() == proto.EnginePaseo)
			// Orca's chats come through a bridge the node runs too (orcabridge.go); its terminals, the Orca host.
			n.runtimeHost.setOrcaBridge(n.engine() == proto.EngineOrca)
		}
		n.configured()
		return map[string]any{"engine": n.engine()}, nil
	case "node.configure":
		var c struct {
			ClaudeDirs []string `json:"claude_dirs"`
			// ClaudeDirsBlocked: patterns of config directories kept out (claudedirs.go); nil leaves them as they are.
			ClaudeDirsBlocked *[]string          `json:"claude_dirs_blocked"`
			Codex             *proto.CodexConfig `json:"codex"`
			MachineID         string             `json:"machine_id"`
			// Keep: sessions always listed, however old or crowded out (the assistant's main conversation).
			Keep *[]string `json:"keep_sessions"`
			// Botmux: list botmux's sessions (hub.json machines[].botmux); nil (a call that says nothing of it, an
			// older hub) leaves it as it is (useBotmux).
			Botmux *bool `json:"botmux"`
			// ClaudeStartDir: the CLAUDE_CONFIG_DIR plain `claude` starts with (hub.json machines[].claude_start_dir);
			// nil (an older hub) leaves it as it is, "" clears it.
			ClaudeStartDir *string `json:"claude_start_dir"`
			// HerdrAutostart: start herdr's server when it is not running (hub.json machines[].herdr_autostart,
			// herdrstart.go); nil (an older hub) leaves it on.
			HerdrAutostart *bool `json:"herdr_autostart"`
			// HerdrAutoinstall: the node's own starts install herdr when there is none (hub.json
			// machines[].herdr_autoinstall, herdrinstall.go); nil (an older hub) leaves it as it is (off).
			HerdrAutoinstall *bool `json:"herdr_autoinstall"`
			// ClaudeGlobalHooks: report-only hooks in the default claude config directory's settings.json
			// (hub.json machines[].claude_global_hooks, globalhooks.go); nil (an older hub) leaves them as they are.
			ClaudeGlobalHooks *bool `json:"claude_global_hooks"`
			// UsageWindows: read the agents' subscription rate-limit windows (hub.json machines[].usage_windows,
			// usagewindows.go, #2432); nil (an older hub) leaves it as it is.
			UsageWindows *bool `json:"usage_windows"`
			// AgentFilters: the extensions' filters of sessions left out (agentfilters.go); nil (an older hub, a call
			// that says nothing of them) leaves them as they are.
			AgentFilters *[]proto.AgentFilter `json:"agent_filters"`
		}
		if err := json.Unmarshal(params, &c); err != nil {
			return nil, err
		}
		if c.ClaudeDirs != nil || c.Keep == nil { // a call that only changes what is kept leaves the directories
			n.setKnownClaudeDirs(c.ClaudeDirs)
		}
		if c.ClaudeDirsBlocked != nil {
			n.setBlockedClaudeDirs(*c.ClaudeDirsBlocked)
		}
		if c.Codex != nil {
			n.configureCodex(*c.Codex)
		}
		if c.MachineID != "" {
			n.mu.Lock()
			n.machineID = c.MachineID
			n.mu.Unlock()
			transcript.LocalMachine.Store(c.MachineID)
		}
		if c.Keep != nil {
			n.setKept(*c.Keep)
		}
		if c.Botmux != nil {
			n.useBotmux(*c.Botmux, false)
		}
		if c.ClaudeStartDir != nil {
			d := strings.TrimSpace(*c.ClaudeStartDir)
			n.claudeStartDir.Store(&d)
		}
		if c.HerdrAutostart != nil {
			n.setHerdrAutostart(*c.HerdrAutostart)
		}
		if c.HerdrAutoinstall != nil {
			n.setHerdrAutoinstall(*c.HerdrAutoinstall)
		}
		if c.ClaudeGlobalHooks != nil {
			n.applyGlobalHooks(*c.ClaudeGlobalHooks)
		}
		if c.UsageWindows != nil {
			n.setUsageWindows(*c.UsageWindows)
		}
		if c.AgentFilters != nil {
			n.setAgentFilters(*c.AgentFilters)
		}
		return map[string]any{"claude_dirs": n.claudeConfigDirs()}, nil
	case "usage.windows": // subscription rate-limit windows read from local files (usagewindows.go, #2432)
		return n.usageSnapshot(false), nil
	case "launchers.list":
		ls, err := n.launchers(ctx)
		if bl := n.runtimes.launchers(); len(bl) > 0 {
			// A runtime's launcher (DSH) is offered even when the login shell's list failed; one named like a shell
			// launcher is not (the shell's runs: runtimeStart).
			ls, err = append(append([]proto.Launcher(nil), ls...), n.runtimes.besideShell(bl, ls)...), nil
		}
		return map[string]any{"launchers": typedLaunchers(ls)}, err
	case "launchers.managed":
		var c struct {
			Launchers []proto.ManagedLauncher `json:"launchers"`
		}
		if err := json.Unmarshal(params, &c); err != nil {
			return nil, err
		}
		n.setManaged(c.Launchers)
		return map[string]any{"ok": true}, nil
	case "launchers.model.check":
		if err := n.runnableLauncher(ctx, p.Name, p.Configured); err != nil {
			return nil, err
		}
		agent, err := n.launcherModelCheck(ctx, p.Name, p.Model)
		return map[string]any{"agent": agent}, err
	case "launchers.zshrc": // the hub importing this machine's claude aliases as providers (zshrc.go)
		return n.zshrcLaunchers()
	case "launchers.show":
		def, err := n.launcherDefinition(ctx, p.Name)
		return map[string]any{"definition": def}, err
	case "launchers.check":
		return n.launcherCheck(ctx, p.Name), nil
	case "claude.auth_status": // whether Claude Code is signed in as the launcher runs it, no model request (claudeauth.go)
		return n.claudeAuthStatus(ctx, p.Name)
	case "launchers.repair": // the hub's launcher health (docs/launcher-health.md)
		if err := n.runnableLauncher(ctx, p.Name, false); err != nil {
			return nil, err
		}
		return n.launcherRepair(ctx, p.Name, p.Action)
	case "launchers.probe":
		if err := n.runnableLauncher(ctx, p.Name, p.Configured); err != nil {
			return nil, err
		}
		return map[string]any{"detail": n.launcherProbeCached(ctx, p.Name)}, nil
	case "launchers.models":
		if !p.Configured && p.AgentType == "" && !n.knownLauncher(ctx, p.Name) { // either: the hub vouches, from hub.json
			return nil, fmt.Errorf("unknown launcher %q", p.Name)
		}
		return n.launcherModels(ctx, p.Name, p.AgentType, p.Refresh)
	case "launchers.http":
		if err := n.runnableLauncher(ctx, p.Name, p.Configured); err != nil {
			return nil, err
		}
		return n.launcherHTTP(ctx, p.Name, p.HTTP)
	case "dirs.list":
		return n.dirs(p.Path)
	case "workspaces.list":
		n.configured()
		n.useBotmux(true, true)
		return n.listWorkspaces(ctx)
	case "workspace.resolve":
		return n.resolveWorkspace(p.Path)
	case "sessions.list":
		n.configured()          // a hub that lists without engine.use keeps herdr (engine.go)
		n.useBotmux(true, true) // and one that lists without saying botmux keeps it, as before (hostbotmux.go)
		return map[string]any{"sessions": n.sessionList(ctx, true)}, nil
	case "session.start":
		tab := ""
		if p.TabName != nil {
			tab = *p.TabName
		}
		rp := runtimeStartParams{Cwd: p.Cwd, Prompt: p.Prompt, Launcher: p.Launcher, Model: p.Model, PermissionMode: p.PermissionMode,
			Effort: p.Effort, WorkspaceID: p.WorkspaceID, Resume: p.ResumeSessionID}
		if p.ResumeSessionID == "" {
			if s, err, ok := n.runtimeStart(ctx, rp); ok { // a runtime's launcher
				return map[string]any{"session": s}, err
			}
		}
		if _, ok := engineRuntimes[n.engine()]; ok { // the engine is a runtime (Paseo): every start is its (runtimecontrol.go)
			s, err := n.startOnEngineRuntime(ctx, rp)
			return map[string]any{"session": s}, err
		}
		// Only Claude Code has a session id before it starts; another agent picks its own (start_agent.go).
		if p.Async && p.ResumeSessionID == "" && (p.AgentType == "" || p.AgentType == "claude") {
			s, err := n.startAsync(p.SessionID, p.Cwd, p.Launcher, p.Prompt, p.WorkspaceID, p.PermissionMode, p.Effort, tab, modelExecution{Model: p.Model, Effort: p.Effort, UseEffort: p.ModelFingerprint != "", Fingerprint: p.ModelFingerprint})
			return map[string]any{"session": s}, err
		}
		s, err := n.start(ctx, p.Cwd, p.Launcher, p.Prompt, p.ResumeSessionID, p.WorkspaceID, p.PermissionMode, p.Effort, tab, "", p.AgentType, modelExecution{Model: p.Model, Effort: p.Effort, UseEffort: p.ModelFingerprint != "", Fingerprint: p.ModelFingerprint})
		return map[string]any{"session": s}, err
	case "session.fork": // 分支到新聊天 (fork.go)
		id, err := n.fork(ctx, p.SessionID, p.AtItemID)
		return map[string]any{"session_id": id}, err
	case "assistant.send":
		if p.ResumeOnly && !n.assistantResumable(p.SessionID) {
			return nil, fmt.Errorf("conversation %s is gone", p.SessionID)
		}
		id, err := n.assistantSend(ctx, p.SessionID, p.Cwd, p.Launcher, p.Text, p.Images, p.PermissionMode, p.Effort, modelExecution{Model: p.Model, Effort: p.Effort, UseEffort: p.ModelFingerprint != "", Fingerprint: p.ModelFingerprint, Runtime: assistantRuntimeOf(p.Runtime)})
		return map[string]any{"session_id": id}, err
	case "session.modes":
		return ok(n.setModelAndModes(ctx, p.SessionID, p.Model, p.PermissionMode, p.Effort)) // model: model_switch.go
	case "assistant.compact": // the user compacting an assistant conversation (assistant_compact.go)
		return ok(n.assistantCompact(ctx, p.SessionID))
	case "assistant.handoff": // what a new main conversation is told about the old one
		return n.assistantHandoff(p.SessionID)
	case "assistant.warm":
		return ok(n.assistantWarm(p.Cwd, p.Launcher, modelExecution{Model: p.Model, Effort: p.Effort, UseEffort: p.ModelFingerprint != "", Fingerprint: p.ModelFingerprint, Runtime: assistantRuntimeOf(p.Runtime)}))
	case "assistant.memory":
		var a memoryOp
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
		return n.assistantMemory(a)
	case "session.send":
		var opts sendOpts
		_ = json.Unmarshal(params, &opts)
		return ok(n.sends.once(opts.ClientMsgID, func() error {
			var after int
			at := time.Now()
			if opts.ClientMsgID != "" {
				after = n.echoBase(p.SessionID)
			}
			var err error
			if n.isAssistantSession(p.SessionID) {
				_, err = n.assistantSend(ctx, p.SessionID, "", p.Launcher, p.Text, p.Images, "", "", modelExecution{Model: p.Model, Effort: p.Effort, UseEffort: p.ModelFingerprint != "", Fingerprint: p.ModelFingerprint, Runtime: n.sessionRuntime(p.SessionID, p.Runtime, p.ModelFingerprint != "")})
			} else {
				err = n.send(ctx, p.SessionID, p.Text, p.Images, opts)
			}
			if opts.ClientMsgID != "" && (err == nil || maybeDelivered(err)) { // echo.go
				n.expectEcho(p.SessionID, opts.ClientMsgID, p.Text, len(p.Images), at, after)
			}
			return err
		}))
	case "session.echoes": // what the hub kept of earlier sends (echo.go), after this node started
		var a struct {
			Echoes []echoRecord `json:"echoes"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
		n.restoreEchoes(a.Echoes)
		return ok(nil)
	case "session.keys":
		if a, ok := n.agent(p.SessionID); ok {
			defer n.forgetDialog(a.PaneID) // the dialog it showed may be gone (dialogstatus.go)
		}
		var dl struct {
			Rev    string `json:"rev"`
			Option string `json:"option"`
			// A multi-select question (dialogmulti.go): the options to have checked, the free-text field's text.
			Multi    bool     `json:"multi"`
			Options  []string `json:"options"`
			FreeText string   `json:"free_text"`
		}
		_ = json.Unmarshal(params, &dl)
		if dl.Multi {
			return ok(n.dialogMulti(ctx, p.SessionID, dl.Rev, dl.Options, dl.FreeText))
		}
		if dl.Rev != "" { // an answer to a dialog card: only into that same dialog (sendsafe.go)
			keys, pre, err := n.dialogKeys(ctx, p.SessionID, dl.Rev, dl.Option)
			if err != nil {
				return nil, err
			}
			if keys != nil {
				p.Keys, p.PreKeys = keys, pre
			}
		}
		if err := n.typingRefusal(p.SessionID, p.Text, p.Keys, p.PreKeys); err != nil {
			return nil, err
		}
		if len(p.PreKeys) > 0 {
			if err := n.keys(ctx, p.SessionID, p.PreKeys, ""); err != nil {
				return nil, err
			}
			time.Sleep(150 * time.Millisecond) // the TUI opens the field the keys picked before the text arrives
		}
		if err := n.keys(ctx, p.SessionID, p.Keys, p.Text); err != nil || dl.Option == "" {
			return ok(err)
		}
		// The last question of several answered: the review tab that follows is submitted (dialogmulti.go).
		if pane, err := n.paneFor(ctx, p.SessionID); err == nil {
			return ok(n.submitReview(ctx, pane))
		}
		return ok(nil)
	case "session.rewind": // Claude Code's /rewind, driven on the screen (rewind.go)
		var o rewindOpts
		if err := json.Unmarshal(params, &o); err != nil {
			return nil, fmt.Errorf("bad params: %w", err)
		}
		return n.rewind(ctx, p.SessionID, o)
	case "codex.answer": // the phone's answer to a Codex question or approval card (codexask.go)
		var a struct {
			ID         string           `json:"id"`
			Resolution proto.Resolution `json:"resolution"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
		return ok(n.codexAnswer(ctx, p.SessionID, a.ID, a.Resolution))
	case "session.interrupt":
		if n.isAssistantSession(p.SessionID) {
			return ok(n.assistantInterrupt(p.SessionID))
		}
		return ok(n.keys(ctx, p.SessionID, []string{"esc"}, ""))
	case "session.screen.before":
		var a struct {
			SessionID string   `json:"session_id"`
			Anchor    []string `json:"anchor"`
			Lines     int      `json:"lines"`
			Loaded    int      `json:"loaded"`
			Format    string   `json:"format"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
		return n.screenBefore(ctx, a.SessionID, a.Anchor, a.Lines, a.Loaded, a.Format == "ansi")
	case "session.screen":
		text, status, err := n.screen(ctx, p.SessionID, p.Lines, p.Source, p.Format == "ansi")
		return map[string]any{"text": text, "status": status}, err
	case "terminal.watch":
		var a struct {
			Control *termControl `json:"control"`
		}
		_ = json.Unmarshal(params, &a)
		return n.terminalWatch(ctx, p.SessionID, a.Control)
	case "panes.list": // the terminal browser: every workspace, tab and pane, agent or shell (panes.go)
		n.configured()
		return n.listPanes(ctx)
	case "panes.new": // an explicit 新建终端 from the browser: a shell tab in a workspace (panes.go)
		var a struct {
			Kind        string `json:"kind"`
			WorkspaceID string `json:"workspace_id"`
		}
		_ = json.Unmarshal(params, &a)
		return n.newShell(ctx, a.Kind, a.WorkspaceID)
	case "panes.close":
		return ok(n.closePane(ctx, p.Pane))
	case "panes.split":
		return n.splitPane(ctx, p.Pane, p.Direction)
	case "panes.agent": // 在这里运行 Agent…: a launcher's agent in a plain shell pane (panes_actions.go)
		s, err := n.runAgentInPane(ctx, p.Pane, p.Launcher)
		if err != nil {
			return nil, err
		}
		return map[string]any{"session_id": s.SessionID}, nil
	case "panes.git":
		dir, err := n.gitDirOf(ctx, p.Pane, p.WorkspaceID, p.Kind)
		if err != nil {
			return nil, err
		}
		return n.gitOp(ctx, dir, p.Op)
	case "worktree.prepare": // 新建 worktree…: what the dialog starts from (worktree.go)
		return n.worktreePrepare(ctx, p.Kind, p.WorkspaceID)
	case "worktree.create": // a Git worktree of a workspace's repository, opened as a workspace (worktree.go)
		return n.worktreeCreate(ctx, p.Kind, p.WorkspaceID, p.Branch, p.Path)
	case "tabs.close":
		return ok(n.closeTab(ctx, p.Kind, p.TabID))
	case "tabs.rename":
		return ok(n.renameTab(ctx, p.Kind, p.TabID, p.Label))
	case "terminal.unwatch":
		n.terminalUnwatch(p.SessionID)
		return ok(nil)
	case "herdr.client.open": // the web's full herdr view: herdr's own client in a PTY (fullclient.go)
		return n.fullOpen(ctx, params)
	case "session.image":
		var a struct {
			ItemID string `json:"item_id"`
		}
		_ = json.Unmarshal(params, &a)
		f, err := n.file(p.SessionID)
		if err != nil {
			return nil, err
		}
		n.refresh(p.SessionID, f)
		it, err := f.Full(a.ItemID)
		if err != nil {
			return nil, fmt.Errorf("session %s: %w", p.SessionID, err)
		}
		if it.Image == nil {
			return nil, fmt.Errorf("no image %s in session %s", a.ItemID, p.SessionID)
		}
		return it.Image, nil
	case "session.item": // one item whole: the text a page cut (full_size)
		var a struct {
			ItemID string `json:"item_id"`
		}
		_ = json.Unmarshal(params, &a)
		f, err := n.file(p.SessionID)
		if err != nil {
			return nil, err
		}
		n.refresh(p.SessionID, f)
		it, err := f.Full(a.ItemID)
		if err != nil {
			return nil, fmt.Errorf("session %s: %w", p.SessionID, err)
		}
		it.Image = nil // images come from session.image
		return n.echoed(p.SessionID, f, []proto.Item{it})[0], nil
	case "session.items":
		f, err := n.file(p.SessionID)
		if err != nil {
			if n.startingSession(p.SessionID) { // nothing written yet: an empty chat, not an error
				return map[string]any{"items": []proto.Item{}, "has_more": false}, nil
			}
			return nil, err
		}
		n.refresh(p.SessionID, f)
		// Items are kept without their image data; a caller that did not ask for lazy images (an older hub) gets it.
		items := func(items []proto.Item) []proto.Item {
			if !p.LazyImages {
				items = f.WithImages(items)
			}
			return nonNil(n.echoed(p.SessionID, f, items))
		}
		limit := p.Limit
		if limit <= 0 || limit > 500 {
			limit = 80
		}
		if p.ThroughSeq > 0 { // a fold being opened: the lines it stands for
			page, more := f.Range(p.FromSeq, p.ThroughSeq, 1000)
			return map[string]any{"items": items(page), "more": more}, nil
		}
		// Pages counted back from the end fold the agent's work for callers that ask (transcript.FoldWork).
		fold := func(page []proto.Item) []proto.Item {
			if p.FoldWork {
				return transcript.FoldWork(page)
			}
			return page
		}
		if p.AfterSeq != nil {
			// more_after: the 500 cap cut it short; the caller asks again after the last seq (or takes the latest page).
			page, more, span := f.After(*p.AfterSeq, 500)
			return withSpan(map[string]any{"items": items(page), "has_more": false, "more_after": more}, span, true), nil
		}
		if p.Turns > 0 { // the phone pages by exchanges (user prompt + answer), not by rows
			page, more, span := f.PageTurns(p.BeforeSeq, min(p.Turns, 10), 2000)
			return withSpan(map[string]any{"items": items(fold(page)), "has_more": more}, span, false), nil
		}
		page, more, span := f.Page(p.BeforeSeq, limit)
		return withSpan(map[string]any{"items": items(fold(page)), "has_more": more}, span, false), nil
	case "session.watch":
		// The stream outlives this request: it follows the connection (rpc.ConnContext), not the reply.
		return ok(n.watchFor(rpc.ConnContext(ctx), p.SessionID, p.SinceSeq))
	case "session.unwatch":
		n.unwatch(p.SessionID)
		return ok(nil)
	case "codex.settings":
		list, err := n.getCodexSettings()
		return map[string]any{"settings": list}, err
	case "codex.settings.set":
		var a struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
		if err := n.setCodexSetting(a.Key, a.Value); err != nil {
			return nil, err
		}
		list, err := n.getCodexSettings()
		return map[string]any{"settings": list}, err
	case "claude.dirs.list": // every config directory the node knows, blocked ones too (claudedirs.go)
		return map[string]any{"dirs": n.listClaudeDirs()}, nil
	case "claude.settings":
		list, err := n.getClaudeSettings()
		return map[string]any{"settings": list}, err
	case "claude.settings.set":
		var a struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
		if err := n.setClaudeSetting(a.Key, a.Value); err != nil {
			return nil, err
		}
		list, err := n.getClaudeSettings()
		return map[string]any{"settings": list}, err
	case "file.upload.begin", "file.upload.chunk", "file.upload.end", "file.upload.abort":
		return n.uploadCall(method, params)
	case "health.reports.list", "health.reports.delete":
		return n.healthReportsCall(method, params)
	case "file.chunk":
		var chunk proto.FileChunkRequest
		if err := json.Unmarshal(params, &chunk); err != nil {
			return nil, err
		}
		return n.readFileChunk(chunk)
	case "git.blob":
		var b proto.GitBlobRequest
		if err := json.Unmarshal(params, &b); err != nil {
			return nil, err
		}
		return n.gitBlob(ctx, b)
	case "file.convert":
		var c proto.FileConvertRequest
		if err := json.Unmarshal(params, &c); err != nil {
			return nil, err
		}
		return n.convertFile(ctx, c)
	case "file.thumb":
		var t proto.FileThumbRequest
		if err := json.Unmarshal(params, &t); err != nil {
			return nil, err
		}
		return n.fileThumb(ctx, t)
	case "file.read":
		return n.readFile(p.Path, p.Cwd, p.Offset, p.Stream)
	case "port.dial":
		return n.portDial(ctx, params)
	case "port.probe":
		return n.portProbeRPC(ctx, params)
	// The built-in programs cua and ego by their old names, for a hub from before builtin_programs (builtin.go).
	case "computer.status", "computer.call", "computer.install", "computer.grant", "computer.cancel", "computer.clipboard",
		"computer.cursor", "computer.nolock", "computer.lock",
		"browser.status", "browser.install", "browser.open", "browser.cancel", "browser.call",
		"browser.cookies.list", "browser.cookies.import", "browser.cookies.clear":
		return legacyBuiltinCall(ctx, method, params, n.lcuComputerCall)
	case "fs.stat":
		return map[string]any{"stats": n.statPaths(p.Paths, p.Cwd)}, nil
	case "space.rename":
		return ok(n.renameSpace(ctx, p.WorkspaceID, p.Name))
	case "session.rename":
		return ok(n.renameSession(ctx, p.SessionID, p.TabName, p.Title))
	case "todo.sync_session_tokens":
		var in struct {
			Tokens map[string]map[string]string `json:"tokens"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		return ok(n.syncTodoTokens(ctx, in.Tokens))
	case "fs.list":
		return n.listDir(p.Path, p.Cwd)
	case "fs.find": // every file under cwd, for the web palette's file search (files_find.go)
		return n.findFiles(ctx, p.Cwd, p.Limit)
	case "fs.watch": // the folders and Git directories the web panels have open (fswatch.go)
		return n.fsWatch(params)
	case "git.status":
		return n.gitStatus(ctx, p.Cwd)
	case "git.pull":
		return n.gitPull(ctx, p.Cwd)
	case "git.diff":
		return n.gitDiff(ctx, p.Cwd, p.Path, p.Staged)
	case "git.show":
		return n.gitShow(ctx, p.Cwd, p.Hash)
	case "git.log":
		list, err := n.gitLog(ctx, p.Cwd, p.Skip, p.Limit)
		return map[string]any{"commits": list}, err
	case "skills.list": // the skills the assistant gets on this machine (docs/hub-update.md「skill 从二进制里拿出来」)
		return map[string]any{"skills": assistantskills.List(assistantskills.UserDir(), n.signedInConnectors())}, nil
	case "assistant.prepare":
		var a struct {
			Dir      string `json:"dir"`
			ClaudeMD string `json:"claude_md"`
			// Connectors are the hub's signed in: the built-in skills that need one go in the directory (Requires).
			Connectors []string `json:"connectors"`
			// CLIs are their command lines (name → connector id), on the assistant's PATH (node/clicall.go).
			CLIs map[string]string `json:"clis"`
			// PluginSkills are the hub's plugins' skills (nil from a hub without plugins: those kept stay).
			PluginSkills []assistantskills.PluginSkill `json:"plugin_skills"`
		}
		if err := json.Unmarshal(params, &a); err != nil || a.Dir == "" || strings.Contains(a.Dir, "..") {
			return nil, fmt.Errorf("bad assistant dir")
		}
		dir := filepath.Join(n.home, a.Dir)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		// The same instructions for both agents: Claude reads CLAUDE.md, Codex AGENTS.md.
		for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(a.ClaudeMD), 0o600); err != nil {
				return nil, err
			}
		}
		// Before every message: a connector signed out since the last one takes its skill (and tg) away.
		n.mu.Lock()
		n.connectors = a.Connectors
		n.clis = a.CLIs
		n.mu.Unlock()
		var need []string
		if a.PluginSkills != nil {
			var err error
			if need, err = assistantskills.ApplyPlugins(assistantskills.PluginDir(), a.PluginSkills); err != nil {
				n.logger.Printf("assistant: plugin skills: %v", err)
			}
		}
		n.syncAssistantSkills(dir)
		return map[string]any{"cwd": dir, "plugin_skills_need": need}, nil
	case "session.info":
		return n.sessionInfo(ctx, p.SessionID)
	case "session.queue.edit":
		var a struct {
			Keep []int `json:"keep"`
		}
		_ = json.Unmarshal(params, &a)
		queue, err := n.editQueue(ctx, p.SessionID, a.Keep)
		return map[string]any{"queue": queue}, err
	case "session.close":
		if n.runtimeAssistant(p.SessionID) { // the runtime keeps the conversation; a running turn stops, as a process would
			if s, _, listed := n.runtimes.rowOf(p.SessionID); listed && s.Status == proto.StatusWorking {
				_ = n.assistantInterruptRuntime(ctx, p.SessionID)
			}
			return ok(nil)
		}
		if n.isAssistantSession(p.SessionID) {
			n.stopAssistant(p.SessionID)
			return ok(nil)
		}
		return ok(n.close(ctx, p.SessionID))
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

func ok(err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// withSpan says where a batch of items sits in the transcript (transcript.Span): history, to_seq, and from_seq for
// batches that continue from a seq (catch-up, live pushes, after_seq) rather than pages counted back from the end.
// sessionInfo is session.info for a session with a transcript here: what the transcript says (counts, model, version,
// branch), its launcher, and the modes it runs with now.
func (n *Node) sessionInfo(ctx context.Context, id string) (map[string]any, error) {
	f, err := n.file(id)
	if err != nil {
		if src, ok := n.codexSource(id); ok {
			effort := ""
			if c, t, driven := n.codexEffortThread(id); driven {
				effort = n.codexEffortNow(id, c, t, "")
			}
			return map[string]any{"session_id": id, "source_kind": "codex_app_server", "source_thread_id": src.thread, "transcript_path": "", "cwd": "", "model": "", "launcher": "", "git_branch": "", "version": "", "permission_mode": "", "effort": effort, "context_tokens": 0, "output_tokens": 0, "compactions": 0, "turns": 0, "history_unavailable": true}, nil
		}
		return nil, err
	}
	n.refresh(id, f)
	m := f.Meta()
	st, _ := n.state.get(id)
	if st.Launcher == "" {
		st.Launcher = n.inferLauncher(id)
	}
	mode, effort := n.modesOf(id)
	if c, t, driven := n.codexEffortThread(id); driven {
		effort = n.codexEffortNow(id, c, t, m.Effort) // what the app-server runs it with (codex_effort.go)
	}
	if pane, err := n.paneFor(ctx, id); err == nil && !n.isAssistantSession(id) {
		if screen, err := n.readPane(ctx, pane, "visible", 60); err == nil && tuiMode(screen) != "" {
			mode = tuiMode(screen) // shift+tab in the terminal shows up in the transcript only with the next prompt
		}
	}
	return map[string]any{
		"session_id": id, "transcript_path": f.Path, "cwd": m.Cwd, "model": m.Model, "launcher": st.Launcher,
		"git_branch": m.GitBranch, "version": m.Version, "permission_mode": mode, "effort": effort,
		"context_tokens": m.ContextTokens, "output_tokens": m.OutputTokens, "compactions": m.Compactions,
		"turns": m.Turns, "started_at": m.StartedAt, "last_activity": m.LastActivity,
	}, nil
}

func withSpan(m map[string]any, sp transcript.Span, continues bool) map[string]any {
	if sp.History != "" {
		m["history"] = sp.History
	}
	m["to_seq"] = sp.To
	if continues {
		m["from_seq"] = sp.From
	}
	return m
}

func nonNil(items []proto.Item) []proto.Item {
	if items == nil {
		return []proto.Item{}
	}
	return items
}

func (n *Node) info(ctx context.Context) proto.NodeInfo {
	host, _ := os.Hostname()
	hv := n.herdrVersion(ctx)
	canUpdate, updateReason := n.updater.Capability()
	running, runningKnown := n.herdrRunning(ctx)
	return proto.NodeInfo{
		NodeUpdate:         canUpdate,
		NodeUpdateReason:   updateReason,
		ModelConfiguration: true,
		Hostname:           host,
		OS:                 runtime.GOOS,
		Arch:               runtime.GOARCH,
		Home:               n.home,
		DataDir:            n.dir,
		HerdrVersion:       hv,
		HerdrRunning:       running,
		HerdrRunningKnown:  runningKnown,
		ClaudeVersion:      n.claudeV.set(claudeVersionOf(ctx)),
		Version:            n.Version,
		System:             n.system(ctx),
		Engine:             n.engine(),
		Engines:            n.engines(ctx, hv),
		MachineUID:         n.uid,
		BuiltinPrograms:    builtinProgramIDs,
	}
}

// herdrRunning says whether classic herdr's server answers on this machine, and whether the node can tell at all: a
// machine driving Herdr App's herdr-core has no classic socket, so nothing is claimed about it (herdrstart.go).
func (n *Node) herdrRunning(ctx context.Context) (running, known bool) {
	h, ok := n.hosts.Get(host.Herdr).(*herdrHost)
	if !ok || h.c == nil {
		return false, false
	}
	return herdrAlive(ctx, h.c.Socket), true
}

func claudeVersion(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := loginShell(ctx, "command claude --version").Output()
	if err != nil {
		return ""
	}
	return parseClaudeVersion(string(out))
}

// parseClaudeVersion takes the version out of `claude --version` run in an interactive shell, whose ~/.zshrc may
// print first (oh-my-zsh warnings, a machine's welcome banner, a terminal's shell-integration escapes on the same
// line): the last line that is one once escape sequences are removed.
func parseClaudeVersion(out string) string {
	lines := strings.Split(strings.TrimSpace(terminalEscapes.ReplaceAllString(out, "")), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if m := claudeVersionLine.FindStringSubmatch(lines[i]); m != nil {
			return m[1]
		}
	}
	return ""
}

// terminalEscapes are OSC (ESC ] … BEL or ESC \\, e.g. iTerm2's shell integration) and CSI (colors) sequences.
var terminalEscapes = regexp.MustCompile(`\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b\[[0-9;?]*[A-Za-z]`)

var claudeVersionLine = regexp.MustCompile(`^\s*v?(\d+\.\d+\.\d+\S*)\s*(\(Claude Code\))?\s*$`)

// interactiveShell is the shell a launcher runs in interactively, so its rc file defines the launcher function:
// loginShellProgram. On Windows that is Git Bash, never a bare bash, which is WSL there (a claude handed to WSL found
// no Claude Code and exited, and the assistant stayed silent, #1994); without Git Bash it says so.
func interactiveShell() (string, error) {
	if sh := loginShellProgram(); sh != "" {
		return sh, nil
	}
	return "", errors.New("这台电脑上没有 Git Bash（Git for Windows）：Claude Code 在 Windows 上也要它，装上后再试")
}

// loginShell runs a command in an interactive zsh so ~/.zshrc functions and PATH are available (loginShellProgram:
// bash without zsh, Git Bash on Windows).
func loginShell(ctx context.Context, script string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, loginShellProgram(), "-ic", script)
	cmd.Stdin = nil
	return cmd
}

func marshalJSON(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

// writeFileAtomic replaces path whole: a reader sees the old file or the new one.
func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// herdrVersion is the herdr host's version ("" without one): node.info's herdr_version.
func (n *Node) herdrVersion(ctx context.Context) string {
	if h := n.hosts.Get(host.Herdr); h != nil {
		return h.Version(ctx)
	}
	return ""
}
