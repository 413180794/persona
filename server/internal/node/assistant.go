package node

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/osproc"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// The assistant (the app's 对话 tab) runs Claude Code headless — `<launcher> -p --input-format stream-json
// --output-format stream-json --include-partial-messages` — instead of in a herdr tab: nothing appears in herdr,
// tokens stream as they are generated, and switching the launcher (model) just restarts the process with --resume.
// A launcher that runs Codex gets `codex app-server` instead (assistant_codex.go).

const assistantIdleTimeout = 20 * time.Minute

// A long conversation is compacted (docs/assistant.md「压缩」): every restart after the prompt cache expired (an hour)
// writes the whole context again, half a million tokens for an assistant conversation that had never been compacted.
// Claude Code compacts on its own once the context nears the auto-compact window, also under -p (tested on 2.1.314):
// CLAUDE_CODE_AUTO_COMPACT_WINDOW lowers that from the model's 1M to 200K. Its own idle compaction only runs in
// interactive sessions, so a process about to be let go with more than assistantIdleCompact in context is sent
// /compact first, while its cache is still warm: the next start reads a summary instead, and nobody waits for it.
const (
	assistantCompactWindow = "200000"
	assistantIdleCompact   = 150_000
)

// A process with background tasks or monitors running is kept: they are its children, and their notifications are
// what wakes the conversation up again. Past this long without a turn it is let go anyway (a forgotten `tail -f`).
const assistantTaskMaxIdle = 12 * time.Hour

// assistantAllowedTools run without asking: Persona's own tools (the hub asks the phone itself before the ones that
// send something on the user's behalf — mail_send, phone writes; Feishu writes through the lark-connector skill pass larkgate.go). Without this every notify_user or
// schedule_followup waited for a tap on the phone, so a follow-up firing at night just stalled.
const assistantAllowedTools = "mcp__jarvis"

// A spare process started when the app opens the 对话 tab, so the first message skips claude's startup.
// It is only adopted while fresh (what it loaded at start — CLAUDE.md, memory, tools — may have changed since).
const assistantSpareMaxAge = 10 * time.Minute

// An assistant process that exits within assistantEarlyExit of starting failed to start: its last assistantStderrTail
// stderr lines go to the log.
const (
	assistantEarlyExit  = 30 * time.Second
	assistantStderrTail = 5
)

// assistantStartWait: how long a send that started a fresh process waits for its first output before answering. A
// launcher that cannot start (its node socket gone, a shell error) exits within a second or two; the message written
// to it is lost, so the send fails with what it printed instead of leaving the message at "正在送达" for good.
var assistantStartWait = 8 * time.Second

// A launcher whose command is missing for a moment is started again a few times before the send fails (#2581):
// Claude Code's auto-updater reinstalls it with npm, which moves the old install away and writes the new one about
// 2.5 s later, and a machine with old long-running claude processes reinstalls every minute or two. A start in that
// window got "command not found: claude" and the user saw "send to claude: write |1: file already closed".
var (
	assistantStartRetries    = 3
	assistantStartRetryDelay = 2 * time.Second
)

type assistantProc struct {
	id       string
	agent    string // "claude" (stream-json) or "codex" (app-server, assistant_codex.go)
	cx       *codexConn
	launcher string
	cwd      string
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	exited   chan struct{}
	born     time.Time
	// stderrDone closes when its stderr ends (watchAssistantStderr has read all of it): what a failed start printed
	// is complete only then. nil for Codex.
	stderrDone chan struct{}
	// ready closes at its first JSON line on stdout (claude): it started. nil for Codex.
	ready     chan struct{}
	readyOnce sync.Once

	wmu    sync.Mutex   // one writer on stdin at a time (messages and control requests)
	reqSeq atomic.Int64 // control request ids

	mu          sync.Mutex
	mode        string // permission mode / effort level already sent (modes.go)
	effort      string
	model       modelExecution
	fingerprint string
	canBypass   bool // started with --permission-mode bypassPermissions: only then can it switch there (needsRestartFor)
	busy        bool
	lastActive  time.Time
	partial     strings.Builder // text of the assistant message being generated
	lastDelta   time.Time
	tasks       []string // running background tasks and monitors (descriptions), from background_tasks_changed
	context     int      // tokens in the conversation's context at its last request (claude), or after a compaction
	compacting  bool     // an idle compaction is running (compactIdle)
	compacted   bool     // compactIdle ran in this process: it is let go when idle again
	manual      bool     // the compaction running is the user's (assistant_compact.go): its end is reported
	requested   bool     // its first request was logged
	stderrTail  []string // its last assistantStderrTail stderr lines (watchAssistantStderr)
	runtime     string   // proto.AssistantRuntimeAgentSDK when it runs on the bridge (assistant_sdk.go), "" on the CLI
	fallback    string   // why it runs on the CLI though asked to run on the Agent SDK
	sdkVersion  string   // the bridge's SDK version (its ready line)

	turnError     string // the latest turn failed on the model service (#2571)
	turnErrorKind string // proto.TurnErrorTransient or proto.TurnErrorUser

	// owner: the Node that took it over from an earlier connection of a resident node (assistant_keep.go); nil: the
	// one that started it.
	owner atomic.Pointer[Node]
}

func (p *assistantProc) write(line []byte) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_, err := p.stdin.Write(append(line, '\n'))
	return err
}

// started marks the process as having started: its first JSON line came out.
func (p *assistantProc) started() {
	if p.ready != nil {
		p.readyOnce.Do(func() { close(p.ready) })
	}
}

// awaitStart waits up to d for a freshly spawned process to start; one that exits first failed to start, and the
// error says what it printed. Still starting after d counts as started (a slow SessionStart hook).
func (p *assistantProc) awaitStart(d time.Duration) error {
	if p.ready == nil {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-p.ready:
		return nil
	case <-t.C:
		return nil
	case <-p.exited:
	}
	select {
	case <-p.ready: // it printed and then exited: it did start
		return nil
	default:
	}
	return p.startFailure()
}

// startFailure is the error for a fresh process that exited before it started: how long it lived and its last stderr
// lines. It waits a little for the process to exit and its stderr to be read to the end: a write to it fails as soon
// as it is gone, before the output saying why has been read.
func (p *assistantProc) startFailure() error {
	select {
	case <-p.exited:
	case <-time.After(time.Second):
	}
	if p.stderrDone != nil {
		select {
		case <-p.stderrDone:
		case <-time.After(500 * time.Millisecond):
		}
	}
	p.mu.Lock()
	tail := strings.Join(p.stderrTail, " | ")
	p.mu.Unlock()
	if tail == "" {
		tail = "没有输出"
	}
	return fmt.Errorf("%s 没有启动起来（%s 后退出），消息没有送达：%s", p.launcher, time.Since(p.born).Round(100*time.Millisecond), tail)
}

// missingCommand: the process exited because its shell found no such command (exit status 127, "command not found"),
// as while Claude Code is being reinstalled.
func (p *assistantProc) missingCommand() bool {
	select {
	case <-p.exited:
	default:
		return false
	}
	if p.cmd != nil && p.cmd.ProcessState != nil && p.cmd.ProcessState.ExitCode() == 127 {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.ContainsFunc(p.stderrTail, func(l string) bool { return strings.Contains(l, "command not found") })
}

func (p *assistantProc) alive() bool {
	select {
	case <-p.exited:
		return false
	default:
		return true
	}
}

// isAssistantSession tells assistant conversations (headless) apart from herdr sessions.
func (n *Node) isAssistantSession(id string) bool {
	n.mu.Lock()
	_, ok := n.procs[id]
	a := n.agents[id]
	n.mu.Unlock()
	if ok {
		return true
	}
	// A real host pane is stronger evidence than a working directory: users may resume a terminal session there.
	// Keep owned headless processes above this check, even if a stale host snapshot still mentions the session.
	if a.PaneID != "" {
		return false
	}
	if st, ok := n.state.get(id); ok && identity.IsAssistantDir(st.Cwd) {
		return true
	}
	if p := n.transcriptPath(id); p != "" && (strings.Contains(filepath.Base(filepath.Dir(p)), "-jarvis-assistant") ||
		isAssistantProject(filepath.Base(filepath.Dir(p)), n.assistantProject())) {
		return true
	}
	// A conversation an agent runtime runs (assistant_runtime.go), listed before this node kept its state.
	if s, _, ok := n.runtimes.rowOf(id); ok && identity.IsAssistantDir(s.Cwd) {
		return true
	}
	return false
}

// assistantResumable: conversation id can be continued, its process is running or its transcript is still on disk.
// A scheduled task's own conversation is sent with resume_only, so one that is gone (cleaned up after 30 days, or
// never written) starts a new conversation at the hub instead of a blank one under the old id (docs/assistant.md
// 「定时任务」).
func (n *Node) assistantResumable(id string) bool {
	if n.runtimeAssistant(id) { // a runtime keeps it: still its while the runtime knows it
		_, _, known := n.runtimes.belongs(id)
		return known
	}
	if id == "" || !validID(id) {
		return false
	}
	n.mu.Lock()
	p := n.procs[id]
	n.mu.Unlock()
	return (p != nil && p.alive()) || n.transcriptPath(id) != ""
}

// assistantSend delivers a user message, starting or restarting the headless claude when needed.
// id == "" starts a new conversation and returns its id.
func (n *Node) assistantSend(ctx context.Context, id, cwd, launcher, text string, images []proto.Image, mode, effort string, options ...modelExecution) (string, error) {
	if launcher != "" && !nameRe.MatchString(launcher) {
		return "", fmt.Errorf("invalid launcher %q", launcher)
	}
	if id != "" && !validID(id) && !n.runtimeAssistant(id) {
		return "", fmt.Errorf("invalid session id %q", id)
	}
	if err := validModes(mode, effort); err != nil {
		return "", err
	}
	resume := id != ""
	st, _ := n.state.get(id)
	// An agent runtime's launcher, or a conversation one runs (assistant_runtime.go): it keeps its model; the node
	// keeps the conversation's state, snapshot, mode and memory.
	rl := launcherOr(launcher, st.Launcher)
	if onRuntime, runs := n.assistantRuntime(rl), n.runtimeAssistant(id); onRuntime != nil || runs {
		if id != "" && (onRuntime == nil || !runs) && !(runs && launcher == "") { // another kind of agent than its own
			return "", errors.New("换到另一种 Agent 需要新建对话")
		}
		return n.assistantSendRuntime(ctx, onRuntime, id, cwd, rl, text, images, mode)
	}
	execution := executionOption(options)
	if len(options) == 0 || (execution.Fingerprint == "" && execution.Model == "") {
		if launcher == "" || launcher == st.Launcher {
			execution = modelExecution{Model: st.Model, Effort: st.ModelEffort, UseEffort: st.ModelFingerprint != "", Fingerprint: st.ModelFingerprint, Runtime: st.Runtime}
		}
	}
	if launcher != "" && st.Launcher != "" && launcher != st.Launcher && execution.Fingerprint == "" {
		execution.UseEffort = true
		execution.Fingerprint = "launcher-default"
	}
	if effort != "" {
		execution.Effort = effort
	}
	if err := execution.check(); err != nil {
		return "", err
	}
	if launcher == "" && id != "" {
		launcher = st.Launcher
	}
	if launcher == "" {
		launcher = "claude"
	}
	if execution.Model != "" {
		if _, err := n.launcherModelCheck(ctx, launcher, execution.Model); err != nil {
			return "", err
		}
	}
	agent, err := n.assistantAgent(ctx, launcher)
	if err != nil {
		return "", err
	}
	var p *assistantProc
	if id == "" {
		// The spare was started without --permission-mode, and bypassPermissions can't be switched to later.
		if p = n.adoptSpareFor(cwd, launcher, mode, execution); p != nil {
			id, launcher, cwd = p.id, p.launcher, p.cwd
		} else if agent == "claude" {
			id = newUUID()
		}
	}
	if cwd == "" {
		cwd = st.Cwd
	}
	if cwd == "" {
		cwd = identity.AssistantDir(n.home)
	}
	// The live snapshot the message starts with, asked for while the process starts (assistant_context.go). A new
	// Codex conversation has no id yet: it can't be the main one.
	snapc := make(chan contextAnswer, 1)
	go func(id string) { snapc <- n.assistantContext(id) }(id)
	if id == "" { // a new Codex conversation: the thread's id is Codex's to pick
		if p, err = n.spawnCodex("", cwd, launcher, false, false, execution); err != nil {
			return "", err
		}
		id = p.id
	}

	n.mu.Lock()
	p = n.procs[id]
	n.mu.Unlock()
	// Claude takes bypassPermissions only at start; Codex gets the policy with every turn (codexPolicy).
	if p != nil && p.alive() && p.agent != agent {
		return "", errors.New("换到另一种 Agent 需要新建对话")
	}
	// The mode this message runs in: its own, or the one the app set (persisted, it outlives the node).
	want := mode
	if want == "" && p != nil {
		want, _ = n.modesOf(id)
	}
	if p != nil && (!p.alive() || p.launcher != launcher || p.fingerprint != n.executionFingerprint(launcher, execution) || p.needsRestartFor(want)) {
		p.mu.Lock()
		active := p.busy || len(p.tasks) > 0
		p.mu.Unlock()
		if active && p.alive() {
			// Answering, or running background tasks a new process would kill: the message goes to this process on
			// what it runs, and the switch waits for the first message after it is idle. Refusing it left the user
			// unable to send while the conversation kept starting tasks (#1554).
			p.mu.Lock()
			launcher, execution = p.launcher, p.model
			if p.needsRestartForLocked(mode) {
				mode = "" // start-only: noted as applied, it would never get its new process
			}
			p.mu.Unlock()
		} else {
			n.stopAssistant(id) // model switch, dead process or bypassPermissions (start-only): resume in a fresh one
			n.rememberTasks(id, nil)
			p = nil
		}
	}
	n.noteModes(id, mode, effort)
	if !resume {
		n.persistModes(id, mode, effort) // the mode a new conversation was started in, as if set from the app
	}
	fresh := p == nil
	if p == nil {
		if p, err = n.spawnAssistant(id, cwd, launcher, resume && n.transcriptPath(id) != "", false, execution); err != nil {
			return "", err
		}
	}
	if st, known := n.state.get(id); !known || st.Launcher != launcher || st.Cwd != cwd || st.Model != execution.Model || st.ModelFingerprint != execution.Fingerprint || st.ModelEffort != execution.Effort || st.Runtime != execution.Runtime {
		started := st.StartedAt
		if started.IsZero() {
			started = time.Now().UTC()
		}
		st.Launcher, st.Cwd, st.StartedAt = launcher, cwd, started
		st.Model, st.ModelFingerprint, st.ModelEffort, st.Runtime = execution.Model, execution.Fingerprint, execution.Effort, execution.Runtime
		n.state.put(id, st)
	}

	live, commit := n.turnContext(id, <-snapc)
	if p.agent == "codex" {
		p.mu.Lock()
		p.busy = true
		p.turnError = ""
		p.turnErrorKind = ""
		p.lastActive = time.Now()
		p.mu.Unlock()
		if err := p.cx.deliver(ctx, live, text, images); err != nil {
			p.mu.Lock()
			p.busy = p.cx.turn() != ""
			p.mu.Unlock()
			return "", fmt.Errorf("send to codex: %w", err)
		}
		commit()
		n.notifySessions()
		return id, nil
	}

	content := []map[string]any{}
	if live != "" {
		content = append(content, map[string]any{"type": "text", "text": live})
	}
	for _, img := range images {
		content = append(content, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": img.Mime, "data": img.Data}})
	}
	if strings.TrimSpace(text) != "" || len(images) == 0 {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	line, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}})
	for attempt := 1; ; attempt++ {
		n.syncAssistantModes(p) // a new or resumed process starts without them
		p.mu.Lock()
		p.busy = true
		p.turnError = ""
		p.turnErrorKind = ""
		p.compacted = false // a new turn: compact again when it is idle and long
		p.lastActive = time.Now()
		p.partial.Reset()
		p.mu.Unlock()
		werr := p.write(line)
		switch {
		case werr != nil && !fresh:
			n.stopAssistant(id)
			return "", fmt.Errorf("send to claude: %w", werr)
		case werr != nil:
			werr = p.startFailure() // it is gone already: say why, not that the pipe is closed (#2581)
		case fresh:
			werr = p.awaitStart(assistantStartWait)
		}
		if werr == nil {
			break
		}
		n.stopAssistant(id)
		if !p.missingCommand() || attempt > assistantStartRetries {
			return "", werr
		}
		// Its command is missing, most likely for the moment Claude Code is being reinstalled: start it again.
		n.logf("assistant %s: %s (attempt %d), starting it again in %s", id[:8], werr, attempt, assistantStartRetryDelay)
		select {
		case <-ctx.Done():
			return "", werr
		case <-time.After(assistantStartRetryDelay):
		}
		if p, err = n.spawnAssistant(id, cwd, launcher, resume && n.transcriptPath(id) != "", false, execution); err != nil {
			return "", err
		}
	}
	commit()
	n.notifySessions()
	return id, nil
}

func (n *Node) adoptSpareFor(cwd, launcher, mode string, options ...modelExecution) *assistantProc {
	if mode == proto.ModeBypass { // a Codex spare is fine: its policy goes with each turn
		n.mu.Lock()
		sp := n.spare
		n.mu.Unlock()
		if sp == nil || sp.agent != "codex" {
			return nil
		}
	}
	return n.adoptSpare(cwd, launcher, options...)
}

// adoptSpare hands the warm spare process to a new conversation when it fits (same directory and launcher, fresh).
func (n *Node) adoptSpare(cwd, launcher string, options ...modelExecution) *assistantProc {
	n.mu.Lock()
	defer n.mu.Unlock()
	p := n.spare
	if p == nil || !p.alive() || time.Since(p.born) > assistantSpareMaxAge ||
		(launcher != "" && p.launcher != launcher) || (cwd != "" && p.cwd != cwd) || p.fingerprint != n.executionFingerprint(p.launcher, executionOption(options)) {
		return nil
	}
	n.spare = nil
	n.procs[p.id] = p
	return p
}

// assistantWarm makes sure a fresh spare process is waiting for the next new conversation.
func (n *Node) assistantWarm(cwd, launcher string, options ...modelExecution) error {
	if n.assistantRuntime(launcher) != nil { // a runtime is running already: nothing to start ahead
		return nil
	}
	execution := executionOption(options)
	if err := execution.check(); err != nil {
		return err
	}
	if launcher == "" || !nameRe.MatchString(launcher) {
		return fmt.Errorf("invalid launcher %q", launcher)
	}
	n.mu.Lock()
	old := n.spare
	if old != nil && old.alive() && old.launcher == launcher && old.cwd == cwd && old.fingerprint == n.executionFingerprint(launcher, execution) && time.Since(old.born) < assistantSpareMaxAge/2 {
		n.mu.Unlock()
		return nil
	}
	n.spare = nil
	n.mu.Unlock()
	if old != nil {
		old.stop()
	}
	if _, err := n.assistantAgent(context.Background(), launcher); err != nil {
		return err
	}
	p, err := n.spawnAssistant("", cwd, launcher, false, true, execution)
	if err != nil {
		return err
	}
	n.mu.Lock()
	if n.spare != nil { // a concurrent warm won
		n.mu.Unlock()
		p.stop()
		return nil
	}
	n.spare = p
	n.mu.Unlock()
	return nil
}

// spawnAssistant starts the conversation's process for its launcher's agent: claude -p (spawnClaude) or codex
// app-server (spawnCodex). A new Codex conversation (id "") gets its id from Codex.
func (n *Node) spawnAssistant(id, cwd, launcher string, resume, spare bool, options ...modelExecution) (*assistantProc, error) {
	if n.agentOf(launcher) == "codex" {
		return n.spawnCodex(id, cwd, launcher, resume, spare, options...)
	}
	if id == "" {
		id = newUUID()
	}
	return n.spawnClaude(id, cwd, launcher, resume, spare, options...)
}

func (n *Node) spawnClaude(id, cwd, launcher string, resume, spare bool, options ...modelExecution) (*assistantProc, error) {
	execution := executionOption(options)
	if err := execution.check(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		return nil, err
	}
	// Per-process MCP config: the assistant-only tools (ask_user, agents_*, lark_*…) and the conversation they act for.
	mcp, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"jarvis": map[string]any{
		"command": n.bin, "args": []string{"mcp"}, "env": map[string]string{"PERSONA_ASSISTANT": "1", "JARVIS_ASSISTANT": "1", "PERSONA_SESSION_ID": id, "JARVIS_SESSION_ID": id},
	}}})
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--include-partial-messages"}
	// Persona's hooks: a plugin, or in --settings for a claude that loads none (hooksplugin.go).
	settings, hooks := n.claudeStartSettings(context.Background(), launcher, "assistant", cwd)
	if settings != "" {
		args = append(args, "--settings", settings)
	}
	if hooks != "" {
		args = append(args, "--plugin-dir", hooks)
	}
	args = append(args, "--mcp-config", string(mcp), "--allowedTools", n.assistantTools())
	if execution.Model != "" {
		args = append(args, "--model", execution.Model)
	}
	// bypassPermissions only takes as a flag at start; the other modes are set with a control request (syncAssistantModes).
	var startMode string
	if !spare {
		if mode, _ := n.modesOf(id); mode == proto.ModeBypass {
			startMode = mode
			args = append(args, "--permission-mode", mode)
		}
	}
	if resume {
		args = append(args, "--resume", id)
	} else {
		args = append(args, "--session-id", id)
	}
	// Persona's own tools are few and used all the time: load them up front instead of behind ToolSearch.
	env := identity.Environment(os.Environ(), "PERSONA_ASSISTANT=1", "ENABLE_TOOL_SEARCH=false", "CLAUDE_CODE_AUTO_COMPACT_WINDOW="+assistantCompactWindow)
	gateEnv, releaseGates := n.assistantLarkEnv(cwd) // its addresses at the gate proxy go with the process
	started := false
	defer func() {
		if !started {
			releaseGates()
		}
	}()
	env = identity.Environment(env, gateEnv...)
	var bridge sdkBridge
	var fallback string
	if execution.Runtime == proto.AssistantRuntimeAgentSDK {
		if bridge, fallback = n.sdkPrepare(context.Background(), launcher); fallback != "" {
			n.logger.Printf("assistant %s: running on the CLI, not the Agent SDK: %s", short(id), fallback)
		}
	}
	var cmd *exec.Cmd
	var first []byte // the bridge's persona_start
	if bridge.script != "" {
		var mcpConfig map[string]any
		_ = json.Unmarshal(mcp, &mcpConfig)
		start := sdkStart{id: id, cwd: cwd, model: execution.Model, permissionMode: startMode, resume: resume, mcp: mcpConfig,
			allowedTools: n.assistantTools(), settings: settings, plugin: hooks}
		var err error
		if cmd, first, err = n.sdkCommand(bridge, launcher, start, env); err != nil {
			return nil, err
		}
	} else {
		shell, err := interactiveShell()
		if err != nil {
			return nil, err
		}
		// An interactive shell so ~/.zshrc defines the launcher function; "$@" passes our flags through.
		cmd = exec.Command(shell, append([]string{"-ic", n.launcherCommand(launcher) + ` "$@"`, "jarvis"}, args...)...)
		cmd.Dir = cwd
		cmd.Env = env
	}
	cmd.SysProcAttr = osproc.GroupAttr() // so stop() can kill the whole group
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", launcher, err)
	}
	started = true
	p := &assistantProc{id: id, agent: "claude", launcher: launcher, cwd: cwd, cmd: cmd, stdin: stdin, exited: make(chan struct{}), ready: make(chan struct{}),
		stderrDone: make(chan struct{}), born: time.Now(), lastActive: time.Now(), mode: startMode, canBypass: startMode == proto.ModeBypass, model: execution, fingerprint: n.executionFingerprint(launcher, execution),
		fallback: fallback}
	if first != nil {
		p.runtime = proto.AssistantRuntimeAgentSDK
		if err := p.write(first); err != nil {
			_ = osproc.StopGroup(cmd.Process.Pid, true)
			_ = cmd.Wait()
			releaseGates() // started, but the exit watcher below never runs
			return nil, fmt.Errorf("start the agent sdk bridge: %w", err)
		}
	}
	if !spare {
		n.mu.Lock()
		n.procs[id] = p
		n.mu.Unlock()
	}
	go watchAssistantStderr(n.logger, p, stderr)
	liveAssistants.Store(p, struct{}{})
	go n.readAssistant(p, stdout)
	go func() {
		_ = cmd.Wait()
		releaseGates()
		liveAssistants.Delete(p)
		close(p.exited)
		if n.keeper != nil {
			n.keeper.forget(p) // exited between two connections
		}
		n := p.ownerOr(n)
		n.mu.Lock()
		wasSpare := n.spare == p
		if wasSpare {
			n.spare = nil
		}
		if n.procs[id] == p {
			delete(n.procs, id)
		}
		n.mu.Unlock()
		if wasSpare {
			return
		}
		n.emitDelta(id, "")
		n.notifySessions()
	}()
	return p, nil
}

// watchAssistantStderr logs an assistant process's error lines, and, when its stderr ends (it exited) within
// assistantEarlyExit of born, its last lines whatever they say: it did not start. WSL's "open terminal failed: not a
// terminal" has no "error" in it, and the assistant stayed silent with nothing in any log (#1994).
// The tail is kept on the process too (without blank lines and terminal colors): a send that started it reports it when
// it exits before starting (awaitStart).
func watchAssistantStderr(logger *log.Logger, p *assistantProc, stderr io.Reader) {
	id, launcher, born := p.id, p.launcher, p.born
	if p.stderrDone != nil {
		defer close(p.stderrDone)
	}
	var tail []string
	sc := bufio.NewScanner(stderr)
	for sc.Scan() {
		l := sc.Text()
		if strings.Contains(l, "rror") {
			logger.Printf("assistant %s: %s", id[:8], l)
		}
		if clean := strings.TrimSpace(ansiRe.ReplaceAllString(l, "")); clean != "" {
			if tail = append(tail, clean); len(tail) > assistantStderrTail {
				tail = tail[1:]
			}
			p.mu.Lock()
			p.stderrTail = append([]string(nil), tail...)
			p.mu.Unlock()
		}
	}
	if lived := time.Since(born); lived < assistantEarlyExit && len(tail) > 0 {
		logger.Printf("assistant %s: %s exited %s after it started: %s", id[:8], launcher, lived.Round(time.Millisecond),
			strings.Join(tail, " | "))
	}
}

// ansiRe: terminal color and style sequences (an interactive zsh's prompt plugins print them on stderr).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// readAssistant turns the stream-json output into live deltas and turn-finished signals.
// The structured messages themselves come from the transcript, like every other session.
func (n *Node) readAssistant(p *assistantProc, r io.Reader) {
	started := n
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		n := p.ownerOr(started) // a resident node's next connection may have taken it over (assistant_keep.go)
		line := sc.Bytes()
		if len(line) == 0 || line[0] != '{' { // interactive zsh may print banners
			continue
		}
		// The bridge's own lines: only its ready says it started (an error is the line it prints before it exits).
		if bytes.Contains(line, []byte(`"persona_bridge"`)) && n.sdkBridgeEvent(p, line) {
			continue
		}
		p.started()
		var ev struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Tasks   []struct {
				Description string `json:"description"`
			} `json:"tasks"`
			Tools    []string `json:"tools"`
			Response struct {
				Subtype string `json:"subtype"`
				Error   string `json:"error"`
			} `json:"response"`
			Event struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
				Message struct {
					Usage struct {
						Input       int `json:"input_tokens"`
						CacheRead   int `json:"cache_read_input_tokens"`
						CacheCreate int `json:"cache_creation_input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			} `json:"event"`
			Compact struct {
				Post int `json:"post_tokens"`
			} `json:"compact_metadata"`
			ParentToolUseID *string `json:"parent_tool_use_id"`
			IsError         bool    `json:"is_error"`
			Result          string  `json:"result"`
		}
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "system":
			if ev.Subtype == "init" {
				n.toolsAtStart(p.id, ev.Tools)
				continue
			}
			if ev.Subtype == "compact_boundary" {
				p.mu.Lock()
				p.context = ev.Compact.Post
				manual := p.manual
				p.manual = false
				p.mu.Unlock()
				n.contextCompacted(p.id)
				if manual {
					n.compactionEnded(p.id, "")
				}
				continue
			}
			if ev.Subtype != "background_tasks_changed" {
				continue
			}
			tasks := make([]string, 0, len(ev.Tasks))
			for _, t := range ev.Tasks {
				tasks = append(tasks, t.Description)
			}
			p.mu.Lock()
			p.tasks = tasks
			p.lastActive = time.Now()
			p.mu.Unlock()
			n.rememberTasks(p.id, tasks)
		case "control_response":
			if ev.Response.Subtype == "error" {
				n.logger.Printf("assistant %s: control request failed: %s", p.id[:8], ev.Response.Error)
			}
		case "stream_event":
			switch ev.Event.Type {
			case "message_start":
				// A turn can start without a message from us: claude takes up a finished background task's
				// notification on its own, and a message sent meanwhile waits behind it. The result of that turn
				// cleared busy while claude went on working, so the phone showed the chat as finished.
				// A new message: the last one's text is in the transcript by now. Clear the app's live text, or a
				// message that went on to call tools stays on screen twice (its item, and the live copy) meanwhile.
				u := ev.Event.Message.Usage
				p.mu.Lock()
				had := p.partial.Len() > 0
				p.partial.Reset()
				p.turnError = ""
				p.turnErrorKind = ""
				resumed := !p.busy
				p.busy = true
				p.lastActive = time.Now()
				first := false
				if ev.ParentToolUseID == nil { // a subagent's requests are its own context
					p.context = u.Input + u.CacheRead + u.CacheCreate
					first, p.requested = !p.requested, true
				}
				p.mu.Unlock()
				if first { // what a start costs: the history read from the prompt cache, or written again
					n.logger.Printf("assistant %s: first request since start: %d tokens in context, %d read from the cache, %d written",
						p.id[:8], u.Input+u.CacheRead+u.CacheCreate, u.CacheRead, u.CacheCreate)
				}
				if resumed {
					n.notifySessions()
				}
				if had {
					n.emitDelta(p.id, "")
				}
			case "content_block_delta":
				if ev.Event.Delta.Type != "text_delta" {
					continue
				}
				p.mu.Lock()
				p.partial.WriteString(ev.Event.Delta.Text)
				text := p.partial.String()
				due := time.Since(p.lastDelta) > 80*time.Millisecond
				if due {
					p.lastDelta = time.Now()
				}
				p.mu.Unlock()
				if due {
					n.emitDelta(p.id, text)
				}
			case "message_stop":
				// The finished block lands in the transcript; keep showing it until the item arrives.
				p.mu.Lock()
				text := p.partial.String()
				p.mu.Unlock()
				n.emitDelta(p.id, text)
			}
		case "result":
			p.mu.Lock()
			manual := p.manual
			p.manual = false
			p.compacting = false
			p.busy = false
			p.lastActive = time.Now()
			p.partial.Reset()
			p.mu.Unlock()
			if manual { // the user's /compact ended without a boundary: it did not compact
				n.compactionEnded(p.id, compactFailure(ev.IsError, ev.Result))
			}
			if f, err := n.file(p.id); err == nil {
				n.refresh(p.id, f)
			}
			n.emitDelta(p.id, "")
			n.notifySessions()
		}
	}
}

func (n *Node) emitDelta(id, text string) {
	if n.peer != nil {
		_ = n.peer.Notify("session.delta", map[string]any{"session_id": id, "text": text})
	}
}

// assistantInterrupt stops the current turn. claude -p has no interrupt over stdin, so the process is killed;
// the next message resumes the conversation from its transcript. Codex has turn/interrupt: the process stays.
func (n *Node) assistantInterrupt(id string) error {
	if n.runtimeAssistant(id) {
		return n.assistantInterruptRuntime(context.Background(), id)
	}
	n.mu.Lock()
	p := n.procs[id]
	n.mu.Unlock()
	if p != nil && p.agent == "codex" && p.alive() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := p.cx.interrupt(ctx); err == nil {
			return nil
		}
	}
	n.stopAssistant(id)
	n.rememberTasks(id, nil) // stopped on purpose, background tasks included
	return nil
}

// needsRestartFor: the process can't switch to mode, a new one has to start in it. Claude refuses
// set_permission_mode bypassPermissions unless it was started in that mode (Persona doesn't pass
// --dangerously-skip-permissions), so switching there resumes the conversation in a fresh process.
func (p *assistantProc) needsRestartFor(mode string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.needsRestartForLocked(mode)
}

// needsRestartForLocked is needsRestartFor with p.mu held.
func (p *assistantProc) needsRestartForLocked(mode string) bool {
	return p.agent == "claude" && mode == proto.ModeBypass && !p.canBypass
}

func (n *Node) stopAssistant(id string) {
	n.mu.Lock()
	p := n.procs[id]
	delete(n.procs, id)
	n.mu.Unlock()
	if p != nil {
		p.stop()
	}
}

func (p *assistantProc) stop() {
	_ = p.stdin.Close()
	if p.cmd.Process != nil {
		_ = osproc.StopGroup(p.cmd.Process.Pid, false)
		go func() {
			select {
			case <-p.exited:
			case <-time.After(5 * time.Second):
				_ = osproc.StopGroup(p.cmd.Process.Pid, true)
			}
		}()
	}
}

// reapable: idle long enough to end; p.mu is held. Background tasks and monitors are children of the process
// (killed with it) and wake it when they report, so a process waiting on them stays (2026-10-30: a 23-minute
// background command was killed at minute 20 and the conversation never heard back).
func (p *assistantProc) reapable(now time.Time) bool {
	if p.compacting {
		return now.Sub(p.lastActive) > 10*time.Minute // a compaction that never reported back
	}
	if p.busy {
		return false
	}
	if len(p.tasks) > 0 {
		return now.Sub(p.lastActive) > assistantTaskMaxIdle
	}
	return now.Sub(p.lastActive) > assistantIdleTimeout
}

// idleCompactable: about to be let go with a context worth compacting first (assistantIdleCompact); p.mu is held.
func (p *assistantProc) idleCompactable() bool {
	return p.agent == "claude" && !p.compacting && !p.compacted && p.context >= assistantIdleCompact
}

// compactIdle sends /compact to an idle process. It is not a turn: the conversation stays as it was in the app (the
// transcript shows a notice), and the process is let go when idle again.
func (n *Node) compactIdle(p *assistantProc) {
	line, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "/compact"}})
	p.mu.Lock()
	tokens := p.context
	p.compacting, p.compacted = true, true
	p.lastActive = time.Now()
	p.mu.Unlock()
	if err := p.write(line); err != nil {
		n.logger.Printf("assistant %s: compact: %v", p.id[:8], err)
		n.stopAssistant(p.id)
		return
	}
	n.logger.Printf("assistant %s: idle with %d tokens in context, compacting before letting it go", p.id[:8], tokens)
}

// rememberTasks keeps a conversation's running background tasks in the session state (the hub's Postgres), so a
// node restart — which kills them with their process — can tell the conversation what it lost (recoverAssistantTasks).
func (n *Node) rememberTasks(id string, tasks []string) {
	st, ok := n.state.get(id)
	if !ok || slices.Equal(st.Tasks, tasks) || (len(st.Tasks) == 0 && len(tasks) == 0) {
		return
	}
	st.Tasks = tasks
	n.state.put(id, st)
}

// recoverAssistantTasks runs once the session state is loaded: conversations whose background tasks or monitors
// were running when the node went down (a deploy restarts it, and systemd kills claude and its children with it)
// get a message saying so, which wakes them up to re-arm what still matters. Without it they would wait forever
// for a notification that cannot come. send delivers the message (assistantSend).
func (n *Node) recoverAssistantTasks(send func(id, text string) error) {
	n.state.mu.Lock()
	lost := map[string][]string{}
	for id, st := range n.state.Sessions {
		if len(st.Tasks) > 0 && identity.IsAssistantDir(st.Cwd) {
			lost[id] = st.Tasks
		}
	}
	n.state.mu.Unlock()
	for id, tasks := range lost {
		n.mu.Lock()
		_, running := n.procs[id]
		n.mu.Unlock()
		if running {
			continue
		}
		n.rememberTasks(id, nil)
		if err := send(id, prompts.RenderLostTasks(tasks)); err != nil {
			n.logger.Printf("assistant %s: tell it about lost background tasks: %v", id[:8], err)
		} else {
			n.logger.Printf("assistant %s: %d background tasks lost in the restart, told the conversation", id[:8], len(tasks))
		}
	}
}

// reapAssistants ends processes idle for a while; their conversations resume on the next message.
func (n *Node) reapAssistants(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			n.mu.Lock()
			ids := make([]string, 0, len(n.procs))
			for id := range n.procs {
				ids = append(ids, id)
			}
			spare := n.spare
			n.spare = nil
			n.mu.Unlock()
			if spare != nil {
				spare.stop()
			}
			if n.keeper != nil { // a resident node: the next connection takes them over (assistant_keep.go)
				n.handOverAssistants()
				ids = nil
			}
			for _, id := range ids {
				n.stopAssistant(id)
			}
			return
		case <-t.C:
		}
		n.mu.Lock()
		var stale *assistantProc
		if n.spare != nil && time.Since(n.spare.born) > assistantSpareMaxAge {
			stale, n.spare = n.spare, nil
		}
		var idle []string
		var compact []*assistantProc
		var timedOut []string
		for id, p := range n.procs {
			p.mu.Lock()
			if p.manual && time.Since(p.lastActive) > assistantCompactTimeout {
				p.manual, p.compacting = false, false
				timedOut = append(timedOut, id)
			}
			if p.reapable(time.Now()) {
				if p.idleCompactable() {
					compact = append(compact, p)
				} else {
					idle = append(idle, id)
				}
			}
			p.mu.Unlock()
		}
		n.mu.Unlock()
		for _, id := range timedOut {
			n.compactionEnded(id, "压缩超过 10 分钟没有结果")
		}
		n.sweepCompactions()
		for _, p := range compact {
			n.compactIdle(p)
		}
		for _, id := range idle {
			n.stopAssistant(id)
			n.rememberTasks(id, nil) // let go on purpose: nothing to tell it after a restart
		}
		if stale != nil {
			stale.stop()
		}
	}
}

// assistantSessions lists running headless conversations as live sessions.
func (n *Node) assistantSessions() []proto.Session {
	n.mu.Lock()
	procs := make([]*assistantProc, 0, len(n.procs))
	for _, p := range n.procs {
		procs = append(procs, p)
	}
	n.mu.Unlock()
	var out []proto.Session
	for _, p := range procs {
		p.mu.Lock()
		busy := p.busy
		status := proto.StatusDone // finished turn; the hub turns it into idle once seen
		if busy {
			status = proto.StatusWorking
		}
		updated := p.lastActive
		turnError := p.turnError
		turnErrorKind := p.turnErrorKind
		p.mu.Unlock()
		s := proto.Session{SessionID: p.id, Cwd: p.cwd, Status: status, Launcher: p.launcher, UpdatedAt: updated.UTC(),
			Runtime: p.runtime, RuntimeFallback: p.fallback}
		if !busy {
			s.TurnError = turnError
			s.TurnErrorKind = turnErrorKind
		}
		if p.agent == "codex" {
			s.Agent = "codex"
		}
		if path := n.transcriptPath(p.id); path != "" {
			if st, err := os.Stat(path); err == nil {
				m := n.meta(path, st)
				s.Title, s.Model, s.LastText, s.Rev = m.title, m.model, snippet(m.lastText), st.Size()
				s.PermissionMode, s.Effort = n.currentModes(p.id, m)
				setContext(&s, m)
				untitled(&s, m)
				if !busy {
					s.UpdatedAt = lastUsed(m, st)
					if s.TurnError == "" && m.turnError != "" {
						s.TurnError = m.turnError
						s.TurnErrorKind = m.turnErrorKind
					}
				}
			}
		}
		n.decorate(&s)
		out = append(out, s)
	}
	return out
}

// notifySessions pushes the session list right away instead of waiting for the next poll.
func (n *Node) notifySessions() {
	if n.peer == nil {
		return
	}
	list := n.sessionList(context.Background(), false)
	n.mu.Lock()
	n.lastList = listKey(list)
	n.revsChanged(list)
	n.mu.Unlock()
	_ = n.peer.Notify("sessions.changed", map[string]any{"sessions": list})
}

// assistantAgent is the agent a launcher runs, as far as the assistant goes: claude (headless -p, stream-json) or
// codex (app-server). Anything else is refused with a reason the app shows (2026-11-01: a codex launcher used to fail
// every message on "unexpected argument '--input-format'"; Codex has its own backend since).
func (n *Node) assistantAgent(ctx context.Context, launcher string) (string, error) {
	if launcher == "claude" {
		return "claude", nil
	}
	if n.assistantRuntime(launcher) != nil {
		return assistantOnRuntime, nil
	}
	d := n.launcherProbeCached(ctx, launcher)
	switch d.Agent {
	case "", "claude": // a probe that failed: try it as claude, as before
		return "claude", nil
	case "codex":
		return "codex", nil
	}
	return "", fmt.Errorf("启动器 %s 跑的是 %s，Persona 助手只能用启动 Claude Code、Codex 或能当助手的 runtime 的启动器（比如 DSH 插件的）：在 设置 › 助手 换一个", launcher, d.Agent)
}

// launcherOr is launcher, or the conversation's own when none is given.
func launcherOr(launcher, own string) string {
	if launcher != "" {
		return launcher
	}
	return own
}

// agentOf is assistantAgent from the probe cache, for places that only need to know (no error).
func (n *Node) agentOf(launcher string) string {
	if a, err := n.assistantAgent(context.Background(), launcher); err == nil {
		return a
	}
	return "claude"
}
