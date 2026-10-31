package node

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// The assistant (the app's 对话 tab) runs Claude Code headless — `<launcher> -p --input-format stream-json
// --output-format stream-json --include-partial-messages` — instead of in a herdr tab: nothing appears in herdr,
// tokens stream as they are generated, and switching the launcher (model) just restarts the process with --resume.

const assistantIdleTimeout = 20 * time.Minute

// A process with background tasks or monitors running is kept: they are its children, and their notifications are
// what wakes the conversation up again. Past this long without a turn it is let go anyway (a forgotten `tail -f`).
const assistantTaskMaxIdle = 12 * time.Hour

// assistantAllowedTools run without asking: Jarvis's own tools (the hub asks the phone itself before the ones that
// send something on the user's behalf — mail_send, phone writes; Feishu writes through the lark-connector skill pass larkgate.go). Without this every notify_user or
// schedule_followup waited for a tap on the phone, so a follow-up firing at night just stalled.
const assistantAllowedTools = "mcp__jarvis"

// A spare process started when the app opens the 对话 tab, so the first message skips claude's startup.
// Its context snapshot is taken at spawn time, so it is only adopted while fresh.
const assistantSpareMaxAge = 10 * time.Minute

type assistantProc struct {
	id       string
	launcher string
	cwd      string
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	exited   chan struct{}
	born     time.Time

	wmu    sync.Mutex   // one writer on stdin at a time (messages and control requests)
	reqSeq atomic.Int64 // control request ids

	mu         sync.Mutex
	mode       string // permission mode / effort level already sent (modes.go)
	effort     string
	busy       bool
	lastActive time.Time
	partial    strings.Builder // text of the assistant message being generated
	lastDelta  time.Time
	tasks      []string // running background tasks and monitors (descriptions), from background_tasks_changed
}

func (p *assistantProc) write(line []byte) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_, err := p.stdin.Write(append(line, '\n'))
	return err
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
	n.mu.Unlock()
	if ok {
		return true
	}
	if st, ok := n.state.get(id); ok && strings.HasSuffix(st.Cwd, "/.jarvis/assistant") {
		return true
	}
	if p := n.transcriptPath(id); p != "" && strings.Contains(filepath.Base(filepath.Dir(p)), "-jarvis-assistant") {
		return true
	}
	return false
}

// assistantSend delivers a user message, starting or restarting the headless claude when needed.
// id == "" starts a new conversation and returns its id.
func (n *Node) assistantSend(ctx context.Context, id, cwd, launcher, text string, images []proto.Image, mode, effort string) (string, error) {
	if launcher != "" && !nameRe.MatchString(launcher) {
		return "", fmt.Errorf("invalid launcher %q", launcher)
	}
	if id != "" && !validID(id) {
		return "", fmt.Errorf("invalid session id %q", id)
	}
	if err := validModes(mode, effort); err != nil {
		return "", err
	}
	resume := id != ""
	if id == "" {
		// The spare was started without --permission-mode, and bypassPermissions can't be switched to later.
		if p := n.adoptSpareFor(cwd, launcher, mode); p != nil {
			id, launcher, cwd = p.id, p.launcher, p.cwd
		} else {
			id = newUUID()
		}
	}
	st, known := n.state.get(id)
	if launcher == "" {
		launcher = st.Launcher
	}
	if launcher == "" {
		launcher = "claude"
	}
	if err := n.assistantLauncherOK(ctx, launcher); err != nil {
		return "", err
	}
	if cwd == "" {
		cwd = st.Cwd
	}
	if cwd == "" {
		cwd = filepath.Join(n.home, ".jarvis", "assistant")
	}
	n.noteModes(id, mode, effort)
	if !known || st.Launcher != launcher || st.Cwd != cwd {
		started := st.StartedAt
		if started.IsZero() {
			started = time.Now().UTC()
		}
		n.state.put(id, sessionState{Launcher: launcher, Cwd: cwd, StartedAt: started})
	}

	n.mu.Lock()
	p := n.procs[id]
	n.mu.Unlock()
	if p != nil && (!p.alive() || p.launcher != launcher || (mode == proto.ModeBypass && p.mode != mode)) {
		n.stopAssistant(id) // model switch, dead process or bypassPermissions (start-only): resume in a fresh one
		n.rememberTasks(id, nil)
		p = nil
	}
	if p == nil {
		var err error
		if p, err = n.spawnAssistant(id, cwd, launcher, resume && n.transcriptPath(id) != "", false); err != nil {
			return "", err
		}
	}

	content := []map[string]any{}
	for _, img := range images {
		content = append(content, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": img.Mime, "data": img.Data}})
	}
	if strings.TrimSpace(text) != "" || len(content) == 0 {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	line, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}})
	n.syncAssistantModes(p) // a new or resumed process starts without them
	p.mu.Lock()
	p.busy = true
	p.lastActive = time.Now()
	p.partial.Reset()
	p.mu.Unlock()
	if err := p.write(line); err != nil {
		n.stopAssistant(id)
		return "", fmt.Errorf("send to claude: %w", err)
	}
	n.notifySessions()
	return id, nil
}

func (n *Node) adoptSpareFor(cwd, launcher, mode string) *assistantProc {
	if mode == proto.ModeBypass {
		return nil
	}
	return n.adoptSpare(cwd, launcher)
}

// adoptSpare hands the warm spare process to a new conversation when it fits (same directory and launcher, fresh).
func (n *Node) adoptSpare(cwd, launcher string) *assistantProc {
	n.mu.Lock()
	defer n.mu.Unlock()
	p := n.spare
	if p == nil || !p.alive() || time.Since(p.born) > assistantSpareMaxAge ||
		(launcher != "" && p.launcher != launcher) || (cwd != "" && p.cwd != cwd) {
		return nil
	}
	n.spare = nil
	n.procs[p.id] = p
	return p
}

// assistantWarm makes sure a fresh spare process is waiting for the next new conversation.
func (n *Node) assistantWarm(cwd, launcher string) error {
	if launcher == "" || !nameRe.MatchString(launcher) {
		return fmt.Errorf("invalid launcher %q", launcher)
	}
	n.mu.Lock()
	old := n.spare
	if old != nil && old.alive() && old.launcher == launcher && old.cwd == cwd && time.Since(old.born) < assistantSpareMaxAge/2 {
		n.mu.Unlock()
		return nil
	}
	n.spare = nil
	n.mu.Unlock()
	if old != nil {
		old.stop()
	}
	p, err := n.spawnAssistant(newUUID(), cwd, launcher, false, true)
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

// assistantContext asks the hub for the live snapshot (time, agents waiting, unread chats, today's meetings…)
// that goes into the system prompt of a starting process.
func (n *Node) assistantContext(id string) string {
	if n.peer == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var res struct {
		Text string `json:"text"`
	}
	if err := n.peer.Call(ctx, "assistant.context", map[string]any{"session_id": id}, &res); err != nil {
		n.logger.Printf("assistant context: %v", err)
		return ""
	}
	return res.Text
}

func (n *Node) spawnAssistant(id, cwd, launcher string, resume, spare bool) (*assistantProc, error) {
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		return nil, err
	}
	// Per-process MCP config: the assistant-only tools (ask_user, agents_*, lark_*…) and the conversation they act for.
	mcp, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"jarvis": map[string]any{
		"command": n.bin, "args": []string{"mcp"}, "env": map[string]string{"JARVIS_ASSISTANT": "1", "JARVIS_SESSION_ID": id},
	}}})
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--include-partial-messages",
		"--settings", filepath.Join(n.dir, "assistant-settings.json"), "--mcp-config", string(mcp),
		"--allowedTools", assistantAllowedTools}
	if snap := n.assistantContext(id); snap != "" {
		args = append(args, "--append-system-prompt", snap)
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
	shell := "zsh"
	if _, err := exec.LookPath("zsh"); err != nil {
		shell = "bash"
	}
	// An interactive shell so ~/.zshrc defines the launcher function; "$@" passes our flags through.
	cmd := exec.Command(shell, append([]string{"-ic", n.launcherCommand(launcher) + ` "$@"`, "jarvis"}, args...)...)
	cmd.Dir = cwd
	// Jarvis's own tools are few and used all the time: load them up front instead of behind ToolSearch.
	cmd.Env = append(os.Environ(), "JARVIS_ASSISTANT=1", "ENABLE_TOOL_SEARCH=false")
	cmd.Env = append(cmd.Env, n.assistantLarkEnv(cwd)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // so stop() can kill the whole group
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
	p := &assistantProc{id: id, launcher: launcher, cwd: cwd, cmd: cmd, stdin: stdin, exited: make(chan struct{}),
		born: time.Now(), lastActive: time.Now(), mode: startMode}
	if !spare {
		n.mu.Lock()
		n.procs[id] = p
		n.mu.Unlock()
	}
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if l := sc.Text(); strings.Contains(l, "rror") {
				n.logger.Printf("assistant %s: %s", id[:8], l)
			}
		}
	}()
	go n.readAssistant(p, stdout)
	go func() {
		_ = cmd.Wait()
		close(p.exited)
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

// readAssistant turns the stream-json output into live deltas and turn-finished signals.
// The structured messages themselves come from the transcript, like every other session.
func (n *Node) readAssistant(p *assistantProc, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 || line[0] != '{' { // interactive zsh may print banners
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Tasks   []struct {
				Description string `json:"description"`
			} `json:"tasks"`
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
			} `json:"event"`
		}
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "system":
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
				p.mu.Lock()
				had := p.partial.Len() > 0
				p.partial.Reset()
				resumed := !p.busy
				p.busy = true
				p.lastActive = time.Now()
				p.mu.Unlock()
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
			p.busy = false
			p.lastActive = time.Now()
			p.partial.Reset()
			p.mu.Unlock()
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
// the next message resumes the conversation from its transcript.
func (n *Node) assistantInterrupt(id string) error {
	n.stopAssistant(id)
	n.rememberTasks(id, nil) // stopped on purpose, background tasks included
	return nil
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
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
		go func() {
			select {
			case <-p.exited:
			case <-time.After(5 * time.Second):
				_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			}
		}()
	}
}

// reapable: idle long enough to end; p.mu is held. Background tasks and monitors are children of the process
// (killed with it) and wake it when they report, so a process waiting on them stays (2026-10-30: a 23-minute
// background command was killed at minute 20 and the conversation never heard back).
func (p *assistantProc) reapable(now time.Time) bool {
	if p.busy {
		return false
	}
	if len(p.tasks) > 0 {
		return now.Sub(p.lastActive) > assistantTaskMaxIdle
	}
	return now.Sub(p.lastActive) > assistantIdleTimeout
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
		if len(st.Tasks) > 0 && strings.HasSuffix(st.Cwd, "/.jarvis/assistant") {
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
		text := "⚠️ 运行你的 jarvisd 刚重启过（通常是部署），这段对话里还在跑的后台任务 / 监控随进程一起停了：\n- " +
			strings.Join(tasks, "\n- ") + "\n还需要的就重新启动（命令可能只跑了一半，重跑前先看看输出）；不需要了就简短说一句，不用推送。"
		if err := send(id, text); err != nil {
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
		for id, p := range n.procs {
			p.mu.Lock()
			if p.reapable(time.Now()) {
				idle = append(idle, id)
			}
			p.mu.Unlock()
		}
		n.mu.Unlock()
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
		status := proto.StatusDone // finished turn; the hub turns it into idle once seen
		if p.busy {
			status = proto.StatusWorking
		}
		updated := p.lastActive
		p.mu.Unlock()
		s := proto.Session{SessionID: p.id, Cwd: p.cwd, Status: status, Launcher: p.launcher, UpdatedAt: updated.UTC()}
		if path := n.transcriptPath(p.id); path != "" {
			if st, err := os.Stat(path); err == nil {
				m := n.meta(path, st)
				s.Title, s.Model, s.LastText, s.Rev = m.title, m.model, snippet(m.lastText), st.Size()
				s.PermissionMode, s.Effort = n.currentModes(p.id, m)
				untitled(&s, m)
				if !p.busy {
					s.UpdatedAt = lastUsed(m, st)
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

// assistantLauncherOK refuses a launcher that does not run Claude Code: the assistant is headless claude (-p,
// --input-format stream-json), and a codex launcher fails on its first flag with "unexpected argument
// '--input-format'" (2026-11-01: 设置 › 助手 had been set to cx_gpt and every message failed that way).
func (n *Node) assistantLauncherOK(ctx context.Context, launcher string) error {
	if launcher == "claude" {
		return nil
	}
	if d := n.launcherProbeCached(ctx, launcher); d.Agent != "" && d.Agent != "claude" {
		return fmt.Errorf("启动器 %s 跑的是 %s，Jarvis 助手只能用启动 Claude Code 的启动器：在 设置 › 助手 换一个", launcher, d.Agent)
	}
	return nil
}
