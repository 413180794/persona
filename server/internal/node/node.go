// Package node runs on every machine: it drives herdr/claude locally and talks to the hub over stdio RPC.
package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
	"github.com/chyroc/jarvis/server/internal/transcript"
)

type Node struct {
	Version string

	home   string
	dir    string // ~/.jarvis
	bin    string // absolute path of this executable
	herdr  *herdr.Client
	peer   *rpc.Peer
	logger *log.Logger

	mu       sync.Mutex
	state    *stateFile
	files    map[string]*transcript.File // session id -> parsed transcript
	paths    map[string]string           // session id -> transcript path
	watches  map[string]*watch
	agents   map[string]herdr.Agent    // session id -> live agent
	panes    map[string]string         // pane id -> session id (learned from hooks)
	layout   layout                    // workspace / tab names from the last herdr snapshot
	procs    map[string]*assistantProc // headless assistant conversations (assistant.go)
	spare    *assistantProc            // warm process waiting for the next new conversation
	lastList string                    // serialized last sessions.changed payload
	lastRevs map[string]int64          // session id -> rev as last pushed (sessions.changed or sessions.revs)
	metas    map[string]quickMeta      // transcript path -> cached meta
	bgScans  map[string]*bgScan        // transcript path -> incremental background-task scan
	panePIDs map[string]panePID        // pane -> its claude pid (no herdr integration: example-machine)
	modes    map[string]sessionModes   // session id -> mode / effort set from the app (modes.go)
	driving  map[string]bool           // session id -> Jarvis is working its TUI (a picker it opened is not "blocked")
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
		herdr:    herdr.New(),
		logger:   log.New(os.Stderr, "node: ", log.LstdFlags|log.Lmsgprefix),
		files:    map[string]*transcript.File{},
		paths:    map[string]string{},
		watches:  map[string]*watch{},
		agents:   map[string]herdr.Agent{},
		panes:    map[string]string{},
		procs:    map[string]*assistantProc{},
		metas:    map[string]quickMeta{},
		bgScans:  map[string]*bgScan{},
		panePIDs: map[string]panePID{},
		modes:    map[string]sessionModes{},
		driving:  map[string]bool{},
	}
	n.state = newState()
	return n, nil
}

// ServeStdio runs the node protocol on stdin/stdout until the hub disconnects.
func (n *Node) ServeStdio(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := n.writeClaudeConfig(); err != nil {
		n.logger.Printf("write claude config: %v", err)
	}
	n.peer = rpc.New("n", os.Stdout)
	n.peer.OnRequest = n.handle
	go n.loadState(ctx)
	ln, err := n.listenHookSocket(ctx)
	if err != nil {
		n.logger.Printf("hook socket: %v", err)
	} else {
		defer ln.Close()
	}
	go n.pollAgents(ctx)
	go n.reapAssistants(ctx)
	return n.peer.Serve(ctx, os.Stdin)
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
	mcp := map[string]any{"mcpServers": map[string]any{"jarvis": map[string]any{"command": n.bin, "args": []string{"mcp"}}}}
	return writeJSON(filepath.Join(n.dir, "mcp.json"), mcp)
}

func (n *Node) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	var p struct {
		Path            string          `json:"path"`
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
		Staged          bool            `json:"staged"`
		Hash            string          `json:"hash"`
		Skip            int             `json:"skip"`
		Turns           int             `json:"turns"`
		Name            string          `json:"name"`
		TabName         *string         `json:"tab_name"`
		Title           *string         `json:"title"`
		PermissionMode  string          `json:"permission_mode"`
		Effort          string          `json:"effort"`
		Raw             json.RawMessage `json:"-"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("bad params: %w", err)
		}
	}
	switch method {
	case "node.info":
		return n.info(ctx), nil
	case "launchers.list":
		ls, err := n.launchers(ctx)
		return map[string]any{"launchers": ls}, err
	case "dirs.list":
		return n.dirs(p.Path)
	case "sessions.list":
		return map[string]any{"sessions": n.sessionList(ctx, true)}, nil
	case "session.start":
		s, err := n.start(ctx, p.Cwd, p.Launcher, p.Prompt, p.ResumeSessionID, p.WorkspaceID, p.PermissionMode, p.Effort)
		return map[string]any{"session": s}, err
	case "assistant.send":
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
		if n.isAssistantSession(p.SessionID) {
			_, err := n.assistantSend(ctx, p.SessionID, "", p.Launcher, p.Text, p.Images, "", "")
			return ok(err)
		}
		return ok(n.send(ctx, p.SessionID, p.Text, p.Images))
	case "session.keys":
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
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
		return n.screenBefore(ctx, a.SessionID, a.Anchor, a.Lines, a.Loaded)
	case "session.screen":
		text, status, err := n.screen(ctx, p.SessionID, p.Lines, p.Source)
		return map[string]any{"text": text, "status": status}, err
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
		it, ok := f.Item(a.ItemID)
		if !ok || it.Image == nil {
			return nil, fmt.Errorf("no image %s in session %s", a.ItemID, p.SessionID)
		}
		return it.Image, nil
	case "session.items":
		f, err := n.file(p.SessionID)
		if err != nil {
			if n.startingSession(p.SessionID) { // nothing written yet: an empty chat, not an error
				return map[string]any{"items": []proto.Item{}, "has_more": false}, nil
			}
			return nil, err
		}
		n.refresh(p.SessionID, f)
		limit := p.Limit
		if limit <= 0 || limit > 500 {
			limit = 80
		}
		if p.AfterSeq != nil {
			// more_after: the 500 cap cut it short; the caller asks again after the last seq (or takes the latest page).
			items, more := f.After(*p.AfterSeq, 500)
			return map[string]any{"items": nonNil(items), "has_more": false, "more_after": more}, nil
		}
		if p.Turns > 0 { // the phone pages by exchanges (user prompt + answer), not by rows
			items, more := f.PageTurns(p.BeforeSeq, min(p.Turns, 10), 2000)
			return map[string]any{"items": nonNil(items), "has_more": more}, nil
		}
		items, more := f.Page(p.BeforeSeq, limit)
		return map[string]any{"items": nonNil(items), "has_more": more}, nil
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
	case "file.read":
		return n.readFile(p.Path, p.Cwd)
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
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(a.ClaudeMD), 0o600); err != nil {
			return nil, err
		}
		return map[string]any{"cwd": dir}, nil
	case "session.info":
		f, err := n.file(p.SessionID)
		if err != nil {
			return nil, err
		}
		n.refresh(p.SessionID, f)
		m := f.Meta()
		st, _ := n.state.get(p.SessionID)
		mode, effort := n.modesOf(p.SessionID)
		if pane, err := n.paneFor(ctx, p.SessionID); err == nil && !n.isAssistantSession(p.SessionID) {
			if screen, err := n.herdr.PaneRead(ctx, pane, "visible", 60); err == nil && tuiMode(screen) != "" {
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

func nonNil(items []proto.Item) []proto.Item {
	if items == nil {
		return []proto.Item{}
	}
	return items
}

func (n *Node) info(ctx context.Context) proto.NodeInfo {
	host, _ := os.Hostname()
	return proto.NodeInfo{
		Hostname:      host,
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		Home:          n.home,
		HerdrVersion:  n.herdr.Version(ctx),
		ClaudeVersion: claudeVersion(ctx),
		Version:       n.Version,
	}
}

func claudeVersion(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := loginShell(ctx, "command claude --version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(string(out)), "(Claude Code)"))
}

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
