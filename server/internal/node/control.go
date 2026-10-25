package node

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/transcript"
)

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// start launches `<launcher> --session-id …` (or --resume) in a new herdr tab and waits until claude is ready.
// start launches `<launcher> --session-id …` (or --resume) in a new herdr tab and waits until claude is ready.
// workspaceID puts the tab into an existing space (e.g. "new tab next to this agent"); otherwise the workspace named
// after the directory is reused or created.
func (n *Node) start(ctx context.Context, cwd, launcher, prompt, resume, workspaceID string) (*proto.Session, error) {
	if launcher == "" {
		launcher = "claude"
	}
	if !nameRe.MatchString(launcher) {
		return nil, fmt.Errorf("invalid launcher %q", launcher)
	}
	if resume != "" && !validID(resume) {
		return nil, fmt.Errorf("invalid session id %q", resume)
	}
	if strings.HasPrefix(cwd, "~/") {
		cwd = filepath.Join(n.home, cwd[2:])
	}
	if cwd == "" {
		cwd = n.home
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("directory %s does not exist", cwd)
	}
	if resume != "" {
		if a, ok := n.agent(resume); ok {
			return n.sessionFor(resume, a), nil // already running
		}
	}

	ws := workspaceID
	if ws == "" {
		var err error
		if ws, err = n.workspaceFor(ctx, cwd); err != nil {
			return nil, err
		}
	}
	var created *herdr.Created
	var err error
	if ws == "" {
		created, err = n.herdr.WorkspaceCreate(ctx, cwd, filepath.Base(cwd))
	} else {
		created, err = n.herdr.TabCreate(ctx, ws, cwd, "jarvis")
	}
	if err != nil {
		return nil, fmt.Errorf("create herdr tab: %w", err)
	}
	pane := created.RootPane.ID

	id := resume
	args := ""
	if id == "" {
		id = newUUID()
		args = "--session-id " + id
	} else {
		args = "--resume " + id
	}
	cmd := fmt.Sprintf("%s %s --settings %s --mcp-config %s", launcher, args,
		shellQuote(filepath.Join(n.dir, "claude-settings.json")), shellQuote(filepath.Join(n.dir, "mcp.json")))
	st := sessionState{Pane: pane, Launcher: launcher, Cwd: cwd, StartedAt: time.Now().UTC()}
	if prev, ok := n.state.get(id); ok && resume != "" {
		st.StartedAt = prev.StartedAt
	}
	n.state.put(id, st)
	// Give the fresh shell a moment to load ~/.zshrc; typed input is buffered either way.
	time.Sleep(300 * time.Millisecond)
	if err := n.herdr.PaneRun(ctx, pane, cmd); err != nil {
		return nil, fmt.Errorf("run launcher: %w", err)
	}

	a, err := n.waitReady(ctx, pane, 60*time.Second)
	if err != nil {
		screen, _ := n.herdr.PaneRead(ctx, pane, "recent-unwrapped", 30)
		return nil, fmt.Errorf("%v\n%s", err, lastLines(screen, 12))
	}
	n.bindPane(pane, id)
	_ = n.herdr.AgentRename(ctx, pane, "j-"+id[:6])
	if prompt != "" {
		if err := n.herdr.AgentPrompt(ctx, pane, prompt); err != nil {
			return nil, fmt.Errorf("send first message: %w", err)
		}
	}
	return n.sessionFor(id, *a), nil
}

// workspaceFor reuses the workspace named after the project directory (the user keeps one workspace per project).
func (n *Node) workspaceFor(ctx context.Context, cwd string) (string, error) {
	wss, err := n.herdr.Workspaces(ctx)
	if err != nil {
		return "", fmt.Errorf("herdr is not running on this machine: %w", err)
	}
	base := filepath.Base(cwd)
	for _, w := range wss {
		if w.Label == base {
			return w.ID, nil
		}
	}
	return "", nil
}

// waitReady polls until claude's input box is on screen, accepting the folder-trust dialog on the way.
// herdr reports the trust dialog as idle, so the screen is the source of truth.
func (n *Node) waitReady(ctx context.Context, pane string, timeout time.Duration) (*herdr.Agent, error) {
	deadline := time.Now().Add(timeout)
	readyStreak := 0
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		screen, err := n.herdr.PaneRead(ctx, pane, "visible", 60)
		if err != nil {
			continue
		}
		if keys := trustKeys(screen); keys != nil {
			_ = n.herdr.PaneSendKeys(ctx, pane, keys...)
			readyStreak = 0
			time.Sleep(time.Second)
			continue
		}
		a, err := n.herdr.AgentGet(ctx, pane)
		if err != nil || a.Agent != "claude" {
			readyStreak = 0
			continue
		}
		if a.AgentStatus == proto.StatusBlocked {
			return a, nil // some other dialog; the phone shows it as a screen interaction
		}
		if (a.AgentStatus == proto.StatusIdle || a.AgentStatus == proto.StatusDone) && inputBoxVisible(screen) {
			readyStreak++
			if readyStreak >= 2 {
				return a, nil
			}
			continue
		}
		readyStreak = 0
	}
	return nil, fmt.Errorf("claude did not become ready within %s", timeout)
}

// inputBoxVisible reports whether Claude Code's prompt footer is rendered.
func inputBoxVisible(screen string) bool {
	return strings.Contains(screen, "shift+tab") || strings.Contains(screen, "? for shortcuts") ||
		strings.Contains(screen, "bypass permissions") || strings.Contains(screen, "esc to interrupt")
}

// trustKeys returns the keys that select "Yes, I trust this folder" on Claude Code's trust dialog.
func trustKeys(screen string) []string {
	if !strings.Contains(screen, "trust this folder") && !strings.Contains(screen, "Do you trust") {
		return nil
	}
	lines := strings.Split(screen, "\n")
	cur, yes := -1, -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "❯") {
			cur = i
		}
		if strings.Contains(t, "Yes, I trust") || strings.Contains(t, "Yes, proceed") {
			yes = i
		}
	}
	if yes < 0 {
		return nil
	}
	if cur < 0 {
		cur = yes
	}
	var keys []string
	for i := cur; i < yes; i++ {
		keys = append(keys, "down")
	}
	for i := cur; i > yes; i-- {
		keys = append(keys, "up")
	}
	return append(keys, "enter")
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func (n *Node) sessionFor(id string, a herdr.Agent) *proto.Session {
	s := &proto.Session{
		SessionID: id, Cwd: a.Cwd, Status: a.AgentStatus, PaneID: a.PaneID, TabID: a.TabID,
		WorkspaceID: a.WorkspaceID, AgentName: a.Name, Title: a.Title, UpdatedAt: time.Now().UTC(), // just started
	}
	_ = n.refreshHerdr(context.Background())
	n.placeIn(s, a)
	n.decorate(s)
	return s
}

// paneFor returns the pane hosting a live session, refreshing herdr state once if needed.
func (n *Node) paneFor(ctx context.Context, id string) (string, error) {
	if a, ok := n.agent(id); ok {
		return a.PaneID, nil
	}
	if agents, err := n.herdr.Agents(ctx); err == nil {
		n.setAgents(agents)
		if a, ok := n.agent(id); ok {
			return a.PaneID, nil
		}
	}
	return "", fmt.Errorf("session %s is not running", id)
}

func (n *Node) send(ctx context.Context, id, text string, images []proto.Image) error {
	pane, err := n.paneFor(ctx, id)
	if err != nil {
		// Offline: resume with the launcher that started it (or plain claude) and then send.
		st, _ := n.state.get(id)
		launcher := st.Launcher
		if launcher == "" {
			launcher = "claude"
		}
		cwd := st.Cwd
		if cwd == "" {
			if p := n.transcriptPath(id); p != "" {
				if fi, err := os.Stat(p); err == nil {
					cwd = n.meta(p, fi).cwd
				}
			}
		}
		s, err := n.start(ctx, cwd, launcher, "", id, "")
		if err != nil {
			return err
		}
		pane = s.PaneID
	}
	paths, err := n.saveImages(images)
	if err != nil {
		return err
	}
	if len(paths) > 0 {
		// In a tag so it never mixes with what the user typed; the phone shows these as thumbnails.
		text = strings.TrimSpace(text) + "\n\n<attached_images>\n" + strings.Join(paths, "\n") + "\n</attached_images>"
	}
	return n.herdr.AgentPrompt(ctx, pane, text)
}

func (n *Node) saveImages(images []proto.Image) ([]string, error) {
	if len(images) == 0 {
		return nil, nil
	}
	dir := filepath.Join(n.dir, "uploads", time.Now().Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	var paths []string
	for _, img := range images {
		data, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			return nil, fmt.Errorf("bad image data: %w", err)
		}
		ext := ".jpg"
		switch img.Mime {
		case "image/png":
			ext = ".png"
		case "image/heic":
			ext = ".heic"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		}
		p := filepath.Join(dir, newUUID()[:8]+ext)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func (n *Node) keys(ctx context.Context, id string, keys []string, text string) error {
	pane, err := n.paneFor(ctx, id)
	if err != nil {
		return err
	}
	if text != "" {
		if err := n.herdr.PaneSendText(ctx, pane, text); err != nil {
			return err
		}
	}
	if len(keys) > 0 {
		return n.herdr.PaneSendKeys(ctx, pane, keys...)
	}
	return nil
}

// screen reads the session's terminal. source "visible" is the screen; "recent" also reaches back into the pane's
// scrollback (the phone pages it: ask for more lines until fewer come back than were asked for).
func (n *Node) screen(ctx context.Context, id string, lines int, source string) (string, string, error) {
	if n.isAssistantSession(id) {
		return "", proto.StatusIdle, nil // headless: no terminal; live text arrives as session.delta
	}
	if source != "recent" {
		source = "visible"
	}
	if limit := map[string]int{"visible": 400, "recent": 20000}[source]; lines <= 0 || lines > limit {
		lines = min(max(lines, 60), limit)
	}
	a, ok := n.agent(id)
	if !ok {
		if _, err := n.paneFor(ctx, id); err != nil {
			return "", proto.StatusOffline, nil
		}
		a, _ = n.agent(id)
	}
	text, err := n.herdr.PaneRead(ctx, a.PaneID, source, lines)
	return text, a.AgentStatus, err
}

func (n *Node) close(ctx context.Context, id string) error {
	if _, ok := n.state.get(id); !ok {
		return fmt.Errorf("only sessions started by Jarvis can be closed from the phone")
	}
	a, ok := n.agent(id)
	if !ok {
		return nil
	}
	_ = n.herdr.PaneSendKeys(ctx, a.PaneID, "ctrl+c")
	time.Sleep(200 * time.Millisecond)
	_ = n.herdr.PaneSendKeys(ctx, a.PaneID, "ctrl+c")
	time.Sleep(500 * time.Millisecond)
	return n.herdr.TabClose(ctx, a.TabID)
}

// watch streams new transcript items and the live screen of a session to the hub.
type watch struct {
	cancel     context.CancelFunc
	lastScreen string
}

func (n *Node) watch(id string, since int) error {
	f, err := n.file(id)
	if err != nil {
		if !n.isAssistantSession(id) {
			return err
		}
		// A brand-new headless conversation has no transcript until claude writes its first line.
		go func() {
			for i := 0; i < 60; i++ {
				time.Sleep(500 * time.Millisecond)
				if _, err := n.file(id); err == nil {
					_ = n.watch(id, since)
					return
				}
			}
		}()
		return nil
	}
	n.mu.Lock()
	if _, ok := n.watches[id]; ok {
		n.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &watch{cancel: cancel}
	n.watches[id] = w
	n.mu.Unlock()

	// Catch up on lines appended after the client's last seen seq, then stream.
	if _, err := f.Refresh(); err == nil && since > 0 {
		if items := f.After(since, 500); len(items) > 0 {
			_ = n.peer.Notify("session.items", map[string]any{"session_id": id, "items": items})
		}
	}
	go n.watchLoop(ctx, id, f, w)
	return nil
}

func (n *Node) unwatch(id string) {
	n.mu.Lock()
	w := n.watches[id]
	delete(n.watches, id)
	n.mu.Unlock()
	if w != nil {
		w.cancel()
	}
}

// refresh parses new transcript lines and pushes them to the hub when the session is watched.
func (n *Node) refresh(id string, f *transcript.File) {
	fresh, err := f.Refresh()
	if err != nil || len(fresh) == 0 {
		return
	}
	n.mu.Lock()
	_, watched := n.watches[id]
	n.mu.Unlock()
	if watched && n.peer != nil {
		_ = n.peer.Notify("session.items", map[string]any{"session_id": id, "items": fresh})
	}
}

func (n *Node) watchLoop(ctx context.Context, id string, f *transcript.File, w *watch) {
	items := time.NewTicker(250 * time.Millisecond)
	screen := time.NewTicker(400 * time.Millisecond)
	defer items.Stop()
	defer screen.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-items.C:
			n.refresh(id, f)
		case <-screen.C:
			a, ok := n.agent(id)
			if !ok {
				continue
			}
			active := a.AgentStatus == proto.StatusWorking || a.AgentStatus == proto.StatusBlocked
			if !active && w.lastScreen == "" {
				continue
			}
			text, err := n.herdr.PaneRead(ctx, a.PaneID, "visible", 60)
			if err != nil || text == w.lastScreen {
				continue
			}
			w.lastScreen = text
			if !active {
				w.lastScreen = "" // send one final frame after work ends, then stop sampling
			}
			_ = n.peer.Notify("session.screen", map[string]any{"session_id": id, "text": text, "status": a.AgentStatus})
		}
	}
}

// editQueue takes the queued input back out of claude (the TUI's "press up to edit queued messages" pulls every
// queued message into the input box), clears the input, and re-queues the ones listed in keep, in order.
// It returns the messages that were queued so the phone can put the edited one in its composer.
func (n *Node) editQueue(ctx context.Context, id string, keep []int) ([]string, error) {
	pane, err := n.paneFor(ctx, id)
	if err != nil {
		return nil, err
	}
	p := n.transcriptPath(id)
	if p == "" {
		return nil, fmt.Errorf("transcript for session %s not found", id)
	}
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	queue := n.meta(p, st).queue
	if len(queue) == 0 {
		return nil, fmt.Errorf("nothing is queued")
	}
	if err := n.herdr.PaneSendKeys(ctx, pane, "up"); err != nil {
		return nil, err
	}
	time.Sleep(300 * time.Millisecond)
	// ctrl+u only clears the current line: walk back through every line of the pulled-back text.
	lines := 0
	for _, q := range queue {
		lines += strings.Count(q, "\n") + 1
	}
	for i := 0; i < lines; i++ {
		_ = n.herdr.PaneSendKeys(ctx, pane, "ctrl+u", "backspace")
	}
	_ = n.herdr.PaneSendKeys(ctx, pane, "ctrl+u")
	for _, k := range keep {
		if k >= 0 && k < len(queue) {
			if err := n.herdr.AgentPrompt(ctx, pane, queue[k]); err != nil {
				return queue, fmt.Errorf("re-queue: %w", err)
			}
			time.Sleep(150 * time.Millisecond)
		}
	}
	return queue, nil
}
