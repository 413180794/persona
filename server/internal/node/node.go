// Package node runs on every machine: it drives herdr/claude locally and talks to the hub over stdio RPC.
package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/host"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
	"github.com/chyroc/jarvis/server/internal/streams"
	"github.com/chyroc/jarvis/server/internal/transcript"
)

type Node struct {
	Version string

	home string
	dir  string // ~/.jarvis
	bin  string // absolute path of this executable
	// managed: launchers kept by the hub (managed.go)
	managed managedLaunchers
	hosts   *host.Registry // the terminal hosts, by kind (hosts.go): the one of the machine's engine
	eng     engineState    // which engine, and its change stream (engine.go)
	// engEpoch counts engine switches (n.mu): a refresh applies its result only when no switch happened meanwhile.
	engEpoch uint64
	peer     *rpc.Peer
	logger   *log.Logger

	mu      sync.Mutex
	state   *stateFile
	files   map[string]*transcript.File // session id -> parsed transcript
	paths   map[string]string           // session id -> transcript path
	pathsAt map[string]time.Time        // when a path not told by a hook was last chosen (transcriptPath)
	watches map[string]*watch
	agents  map[string]host.Agent // session id -> live agent
	// lastPanes: the pane each session was last seen live in (it is no longer live when its agent exits or is
	// suspended), to tell the two apart when a send finds it gone on a host that cannot resume it.
	lastPanes map[string]string
	panes     map[string]string         // pane id -> session id (learned from hooks)
	layouts   map[string]layout         // host kind -> space / slot names from its last snapshot
	procs     map[string]*assistantProc // headless assistant conversations (assistant.go)
	spare     *assistantProc            // warm process waiting for the next new conversation
	akeys     map[string]*assistantProc // the key a Codex conversation's MCP server reports until its thread exists
	lastList  string                    // serialized last sessions.changed payload
	lastRevs  map[string]int64          // session id -> rev as last pushed (sessions.changed or sessions.revs)
	metas     map[string]quickMeta      // transcript path -> cached meta
	bgScans   map[string]*bgScan        // transcript path -> incremental background-task scan
	panePIDs  map[string]panePID        // pane -> its claude pid (machines without herdr's claude integration)
	modes     map[string]sessionModes   // session id -> mode / effort set from the app (modes.go)
	terms     map[string]*termStream    // session id -> its terminal streaming to the hub (terminal.go)
	suggests  map[string]*suggestState  // session id -> Claude's suggested next message (suggestion.go)
	driving   map[string]bool           // session id -> Jarvis is working its TUI (a picker it opened is not "blocked")
	events    *herdrEvents              // herdr's event stream (herdrevents.go)
	cdirs     *claudeDirs               // Claude Code config directories in use (claudedirs.go)
	codices   *codexRegistry
	machineID string
	sends     sentLog         // recent client_msg_ids (sendsafe.go)
	typing    typingLocks     // one send at a time per session (sendsafe.go)
	kept      map[string]bool // sessions always listed (node.configure keep_sessions)
	starts    startLog        // sessions starting in the background (starting.go)
	ups       uploads         // files arriving from the phone (upload.go)
	sysOnce   sync.Once       // system: read once (metrics.go)
	sys       proto.System

	// ctxSent: assistant conversation → the snapshot sections it has seen; ctxTools: the tools its process last
	// started with (assistant_context.go).
	ctxSent  map[string]map[string]string
	ctxTools map[string][]string

	// ports: the agents' listening ports (ports.go); streams: the forwarded connections to them (streams.Mux).
	ports   portState
	streams *streams.Mux

	// hostAgents: each host's agents in its last answer (refreshHosts), kept for a host that does not answer.
	hostAgents map[string][]host.Agent

	// pushLocks: one per session, held from reading its new lines to pushing them (pushLock).
	pushLocks map[string]*sync.Mutex
	// fileUse: when each parsed transcript in files was last asked for (a counter), to drop the least recently used.
	fileUse map[string]uint64
	useTick uint64

	// lark-connector for the assistant (larkgate.go): the hub's config, cached, and the writes 本会话总是允许 let through.
	larkCfg     proto.AssistantLark
	larkCfgAt   time.Time
	larkAllowed map[string]bool
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
	dir := filepath.Join(home, ".jarvis")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	n := &Node{
		Version:  version,
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
		codices:  newCodexRegistry(),
	}
	n.configureCodex(proto.CodexConfig{})
	n.hosts = host.NewRegistry(engineHosts(proto.EngineHerdr, n.logger.Printf)...) // herdr until engine.use
	n.state = newState()
	n.ports.kick = make(chan struct{}, 1)
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
	if err := n.writeClaudeConfig(); err != nil {
		n.logger.Printf("write claude config: %v", err)
	}
	n.peer = rpc.New("n", w)
	n.peer.OnRequest = n.handle
	n.streams = streams.New(n.peer.Notify)
	defer n.streams.CloseAll()
	n.peer.OnEvent = func(event string, params json.RawMessage) {
		if !n.streams.Event(event, params) {
			n.onTerminalEvent(event, params)
		}
	}
	n.cdirs.report = func(dirs []string) { _ = n.peer.Notify("claude.dirs", map[string]any{"dirs": dirs}) }
	n.codices.report = func(cfg proto.CodexConfig) { _ = n.peer.Notify("codex.inventory", cfg) }
	go n.runCodex(ctx)
	go n.loadState(ctx)
	ln, err := n.listenHookSocket(ctx)
	if err != nil {
		n.logger.Printf("hook socket: %v", err)
	} else {
		defer ln.Close()
	}
	go n.startWhenConfigured(ctx) // polling and the engine's events, once the hub said which engine (engine.go)
	go n.warmTranscripts(ctx)
	go n.reapAssistants(ctx)
	go n.meterUsage(ctx)
	go n.sampleMetrics(ctx)
	go n.watchPorts(ctx)
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
		_, err := n.assistantSend(ctx, id, "", "", text, nil, "", "")
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

func (n *Node) writeClaudeConfig() error {
	cmd := shellQuote(n.bin) + " hook"
	blocking := func(matcher string) map[string]any {
		return map[string]any{"matcher": matcher, "hooks": []any{map[string]any{"type": "command", "command": cmd, "timeout": 86400}}}
	}
	notify := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": cmd, "timeout": 10}}}
	settings := map[string]any{"hooks": map[string]any{
		"PreToolUse":        []any{blocking("AskUserQuestion")},
		"PermissionRequest": []any{blocking("*")},
		"Notification":      []any{notify},
		"Stop":              []any{notify},
		"UserPromptSubmit":  []any{notify},
		"SessionStart":      []any{notify},
	}}
	if err := writeJSON(filepath.Join(n.dir, "claude-settings.json"), settings); err != nil {
		return err
	}
	// The assistant's own settings add the lark-connector gate on Bash (larkgate.go); herdr agents never pay for that hook.
	settings["hooks"].(map[string]any)["PreToolUse"] = []any{blocking("AskUserQuestion"), blocking("Bash")}
	if err := writeJSON(filepath.Join(n.dir, "assistant-settings.json"), settings); err != nil {
		return err
	}
	mcp := map[string]any{"mcpServers": map[string]any{"jarvis": map[string]any{"command": n.bin, "args": []string{"mcp"}}}}
	return writeJSON(filepath.Join(n.dir, "mcp.json"), mcp)
}

func (n *Node) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	var p struct {
		Path            string          `json:"path"`
		Paths           []string        `json:"paths"`
		Offset          int64           `json:"offset"`
		SessionID       string          `json:"session_id"`
		Cwd             string          `json:"cwd"`
		Launcher        string          `json:"launcher"`
		Prompt          string          `json:"prompt"`
		ResumeSessionID string          `json:"resume_session_id"`
		WorkspaceID     string          `json:"workspace_id"`
		Text            string          `json:"text"`
		Images          []proto.Image   `json:"images"`
		PreKeys         []string        `json:"pre_keys"`
		Keys            []string        `json:"keys"`
		Lines           int             `json:"lines"`
		BeforeSeq       int             `json:"before_seq"`
		AfterSeq        *int            `json:"after_seq"`
		Limit           int             `json:"limit"`
		SinceSeq        int             `json:"since_seq"`
		Source          string          `json:"source"`
		Format          string          `json:"format"`
		Staged          bool            `json:"staged"`
		Hash            string          `json:"hash"`
		Skip            int             `json:"skip"`
		Turns           int             `json:"turns"`
		LazyImages      bool            `json:"lazy_images"`
		FoldWork        bool            `json:"fold_work"`
		FromSeq         int             `json:"from_seq"`
		ThroughSeq      int             `json:"through_seq"`
		Name            string          `json:"name"`
		TabName         *string         `json:"tab_name"`
		TZ              string          `json:"tz"` // the user's time zone (launchers.quota)
		Async           bool            `json:"async"`
		Title           *string         `json:"title"`
		PermissionMode  string          `json:"permission_mode"`
		Effort          string          `json:"effort"`
		ResumeOnly      bool            `json:"resume_only"`
		Raw             json.RawMessage `json:"-"`
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
		case "session.keys", "session.screen", "session.screen.before", "terminal.watch", "session.close", "session.modes", "session.queue.edit", "session.rename", "assistant.send":
			return nil, refusal("codex_read_only", "这个会话没有终端或启动器控制能力，请在原 app-server 中操作", nil)
		case "session.interrupt":
			return ok(n.interruptCodex(ctx, p.SessionID))
		}
	}
	switch method {
	case "session.attach":
		return ok(n.attachCodex(ctx, p.SessionID))
	case "ping": // the hub checking a tailcat connection is alive (its userspace TCP has no keepalive)
		return map[string]any{}, nil
	case "node.info":
		return n.info(ctx), nil
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
		n.configured()
		return map[string]any{"engine": n.engine()}, nil
	case "node.configure":
		var c struct {
			ClaudeDirs []string           `json:"claude_dirs"`
			Codex      *proto.CodexConfig `json:"codex"`
			MachineID  string             `json:"machine_id"`
			// Keep: sessions always listed, however old or crowded out (the assistant's main conversation).
			Keep *[]string `json:"keep_sessions"`
		}
		if err := json.Unmarshal(params, &c); err != nil {
			return nil, err
		}
		if c.ClaudeDirs != nil || c.Keep == nil { // a call that only changes what is kept leaves the directories
			n.setKnownClaudeDirs(c.ClaudeDirs)
		}
		if c.Codex != nil {
			n.configureCodex(*c.Codex)
		}
		if c.MachineID != "" {
			n.mu.Lock()
			n.machineID = c.MachineID
			n.mu.Unlock()
		}
		if c.Keep != nil {
			n.setKept(*c.Keep)
		}
		return map[string]any{"claude_dirs": n.claudeConfigDirs()}, nil
	case "launchers.list":
		ls, err := n.launchers(ctx)
		return map[string]any{"launchers": ls}, err
	case "launchers.managed":
		var c struct {
			Launchers []proto.ManagedLauncher `json:"launchers"`
		}
		if err := json.Unmarshal(params, &c); err != nil {
			return nil, err
		}
		n.setManaged(c.Launchers)
		return map[string]any{"ok": true}, nil
	case "launchers.show":
		def, err := n.launcherDefinition(ctx, p.Name)
		return map[string]any{"definition": def}, err
	case "launchers.check":
		return n.launcherCheck(ctx, p.Name), nil
	case "launchers.probe":
		return map[string]any{"detail": n.launcherProbeCached(ctx, p.Name)}, nil
	case "launchers.quota":
		r, err := n.launcherQuota(ctx, p.Name, p.TZ)
		return map[string]any{"quota": r}, err
	case "dirs.list":
		return n.dirs(p.Path)
	case "sessions.list":
		n.configured() // a hub that lists without engine.use keeps herdr (engine.go)
		return map[string]any{"sessions": n.sessionList(ctx, true)}, nil
	case "session.start":
		tab := ""
		if p.TabName != nil {
			tab = *p.TabName
		}
		if p.Async && p.ResumeSessionID == "" {
			s, err := n.startAsync(p.SessionID, p.Cwd, p.Launcher, p.Prompt, p.WorkspaceID, p.PermissionMode, p.Effort, tab)
			return map[string]any{"session": s}, err
		}
		s, err := n.start(ctx, p.Cwd, p.Launcher, p.Prompt, p.ResumeSessionID, p.WorkspaceID, p.PermissionMode, p.Effort, tab, "")
		return map[string]any{"session": s}, err
	case "assistant.send":
		if p.ResumeOnly && !n.assistantResumable(p.SessionID) {
			return nil, fmt.Errorf("conversation %s is gone", p.SessionID)
		}
		id, err := n.assistantSend(ctx, p.SessionID, p.Cwd, p.Launcher, p.Text, p.Images, p.PermissionMode, p.Effort)
		return map[string]any{"session_id": id}, err
	case "session.modes":
		return ok(n.setModes(ctx, p.SessionID, p.PermissionMode, p.Effort))
	case "assistant.warm":
		return ok(n.assistantWarm(p.Cwd, p.Launcher))
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
			if n.isAssistantSession(p.SessionID) {
				_, err := n.assistantSend(ctx, p.SessionID, "", p.Launcher, p.Text, p.Images, "", "")
				return err
			}
			return n.send(ctx, p.SessionID, p.Text, p.Images, opts)
		}))
	case "session.keys":
		var dl struct {
			Rev    string `json:"rev"`
			Option string `json:"option"`
		}
		_ = json.Unmarshal(params, &dl)
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
		return ok(n.keys(ctx, p.SessionID, p.Keys, p.Text))
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
	case "terminal.unwatch":
		n.terminalUnwatch(p.SessionID)
		return ok(nil)
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
		return it, nil
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
			if p.LazyImages {
				return nonNil(items)
			}
			return nonNil(f.WithImages(items))
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
		return ok(n.watch(p.SessionID, p.SinceSeq))
	case "session.unwatch":
		n.unwatch(p.SessionID)
		return ok(nil)
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
	case "file.read":
		return n.readFile(p.Path, p.Cwd, p.Offset)
	case "port.dial":
		return n.portDial(ctx, params)
	case "port.probe":
		return n.portProbeRPC(ctx, params)
	case "fs.stat":
		return map[string]any{"stats": n.statPaths(p.Paths, p.Cwd)}, nil
	case "space.rename":
		return ok(n.renameSpace(ctx, p.WorkspaceID, p.Name))
	case "session.rename":
		return ok(n.renameSession(ctx, p.SessionID, p.TabName, p.Title))
	case "fs.list":
		return n.listDir(p.Path, p.Cwd)
	case "git.status":
		return n.gitStatus(ctx, p.Cwd)
	case "git.diff":
		return n.gitDiff(ctx, p.Cwd, p.Path, p.Staged)
	case "git.show":
		return n.gitShow(ctx, p.Cwd, p.Hash)
	case "git.log":
		list, err := n.gitLog(ctx, p.Cwd, p.Skip, p.Limit)
		return map[string]any{"commits": list}, err
	case "assistant.prepare":
		var a struct {
			Dir      string `json:"dir"`
			ClaudeMD string `json:"claude_md"`
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
		return map[string]any{"cwd": dir}, nil
	case "session.info":
		f, err := n.file(p.SessionID)
		if err != nil {
			if src, ok := n.codexSource(p.SessionID); ok {
				return map[string]any{"session_id": p.SessionID, "source_kind": "codex_app_server", "source_thread_id": src.thread, "transcript_path": "", "cwd": "", "model": "", "launcher": "", "git_branch": "", "version": "", "permission_mode": "", "effort": "", "context_tokens": 0, "output_tokens": 0, "compactions": 0, "turns": 0, "history_unavailable": true}, nil
			}
			return nil, err
		}
		n.refresh(p.SessionID, f)
		m := f.Meta()
		st, _ := n.state.get(p.SessionID)
		if st.Launcher == "" {
			st.Launcher = n.inferLauncher(p.SessionID)
		}
		mode, effort := n.modesOf(p.SessionID)
		if pane, err := n.paneFor(ctx, p.SessionID); err == nil && !n.isAssistantSession(p.SessionID) {
			if screen, err := n.readPane(ctx, pane, "visible", 60); err == nil && tuiMode(screen) != "" {
				mode = tuiMode(screen) // shift+tab in the terminal shows up in the transcript only with the next prompt
			}
		}
		return map[string]any{
			"session_id": p.SessionID, "transcript_path": f.Path, "cwd": m.Cwd, "model": m.Model, "launcher": st.Launcher,
			"git_branch": m.GitBranch, "version": m.Version, "permission_mode": mode, "effort": effort,
			"context_tokens": m.ContextTokens, "output_tokens": m.OutputTokens, "compactions": m.Compactions,
			"turns": m.Turns, "started_at": m.StartedAt, "last_activity": m.LastActivity,
		}, nil
	case "session.queue.edit":
		var a struct {
			Keep []int `json:"keep"`
		}
		_ = json.Unmarshal(params, &a)
		queue, err := n.editQueue(ctx, p.SessionID, a.Keep)
		return map[string]any{"queue": queue}, err
	case "session.close":
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
	return proto.NodeInfo{
		Hostname:      host,
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		Home:          n.home,
		HerdrVersion:  hv,
		ClaudeVersion: claudeVersion(ctx),
		Version:       n.Version,
		System:        n.system(ctx),
		Engine:        n.engine(),
		Engines:       n.engines(ctx, hv),
	}
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

// loginShell runs a command in an interactive zsh so ~/.zshrc functions and PATH are available.
func loginShell(ctx context.Context, script string) *exec.Cmd {
	shell := "zsh"
	if _, err := exec.LookPath("zsh"); err != nil {
		shell = "bash"
	}
	cmd := exec.CommandContext(ctx, shell, "-ic", script)
	cmd.Stdin = nil
	return cmd
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
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
