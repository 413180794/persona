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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// The assistant (the app's 对话 tab) runs Claude Code headless — `<launcher> -p --input-format stream-json
// --output-format stream-json --include-partial-messages` — instead of in a herdr tab: nothing appears in herdr,
// tokens stream as they are generated, and switching the launcher (model) just restarts the process with --resume.

const assistantIdleTimeout = 20 * time.Minute

type assistantProc struct {
	id       string
	launcher string
	cwd      string
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	exited   chan struct{}

	mu         sync.Mutex
	busy       bool
	lastActive time.Time
	partial    strings.Builder // text of the assistant message being generated
	lastDelta  time.Time
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
func (n *Node) assistantSend(ctx context.Context, id, cwd, launcher, text string, images []proto.Image) (string, error) {
	if launcher != "" && !nameRe.MatchString(launcher) {
		return "", fmt.Errorf("invalid launcher %q", launcher)
	}
	if id != "" && !validID(id) {
		return "", fmt.Errorf("invalid session id %q", id)
	}
	resume := id != ""
	if id == "" {
		id = newUUID()
	}
	st, known := n.state.get(id)
	if launcher == "" {
		launcher = st.Launcher
	}
	if launcher == "" {
		launcher = "ccc"
	}
	if cwd == "" {
		cwd = st.Cwd
	}
	if cwd == "" {
		cwd = filepath.Join(n.home, ".jarvis", "assistant")
	}
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
	if p != nil && (!p.alive() || p.launcher != launcher) {
		n.stopAssistant(id) // model switch or dead process: resume in a fresh one
		p = nil
	}
	if p == nil {
		var err error
		if p, err = n.spawnAssistant(id, cwd, launcher, resume && n.transcriptPath(id) != ""); err != nil {
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
	p.mu.Lock()
	p.busy = true
	p.lastActive = time.Now()
	p.partial.Reset()
	p.mu.Unlock()
	if _, err := p.stdin.Write(append(line, '\n')); err != nil {
		n.stopAssistant(id)
		return "", fmt.Errorf("send to claude: %w", err)
	}
	n.notifySessions()
	return id, nil
}

func (n *Node) spawnAssistant(id, cwd, launcher string, resume bool) (*assistantProc, error) {
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		return nil, err
	}
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--include-partial-messages",
		"--settings", filepath.Join(n.dir, "claude-settings.json"), "--mcp-config", filepath.Join(n.dir, "mcp.json")}
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
	cmd := exec.Command(shell, append([]string{"-ic", launcher + ` "$@"`, "jarvis"}, args...)...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "JARVIS_ASSISTANT=1")
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
	p := &assistantProc{id: id, launcher: launcher, cwd: cwd, cmd: cmd, stdin: stdin, exited: make(chan struct{}), lastActive: time.Now()}
	n.mu.Lock()
	n.procs[id] = p
	n.mu.Unlock()
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
		if n.procs[id] == p {
			delete(n.procs, id)
		}
		n.mu.Unlock()
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
			Type  string `json:"type"`
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
		case "stream_event":
			switch ev.Event.Type {
			case "message_start":
				p.mu.Lock()
				p.partial.Reset()
				p.mu.Unlock()
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
	return nil
}

func (n *Node) stopAssistant(id string) {
	n.mu.Lock()
	p := n.procs[id]
	delete(n.procs, id)
	n.mu.Unlock()
	if p == nil {
		return
	}
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
			n.mu.Unlock()
			for _, id := range ids {
				n.stopAssistant(id)
			}
			return
		case <-t.C:
		}
		n.mu.Lock()
		var idle []string
		for id, p := range n.procs {
			p.mu.Lock()
			if !p.busy && time.Since(p.lastActive) > assistantIdleTimeout {
				idle = append(idle, id)
			}
			p.mu.Unlock()
		}
		n.mu.Unlock()
		for _, id := range idle {
			n.stopAssistant(id)
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
				s.Title, s.Model, s.LastText = m.title, m.model, snippet(m.lastText)
				untitled(&s, m)
				if !p.busy {
					s.UpdatedAt = st.ModTime().UTC()
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
	n.mu.Unlock()
	_ = n.peer.Notify("sessions.changed", map[string]any{"sessions": list})
}
