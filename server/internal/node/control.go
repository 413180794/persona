package node

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// start launches the selected agent in a new herdr tab and waits for its session: `<launcher> --session-id …` (or
// --resume) for Claude Code, waiting until claude is ready; a Codex launcher of the login shell with the first message
// (codex_start.go); a TRAEX, or a launcher the user says runs another agent, without Claude's flags (start_agent.go).
// workspaceID puts the tab into an existing space (e.g. "new tab next to this agent"); otherwise the workspace named
// after the directory is reused or created.
// tabName names the herdr tab (hub picks one from the first message when the user gave none); empty: agent-<n>.
// newID is the id a new session gets (startAsync picked it already); empty: a fresh one.
// agentType is the agent the user picked for the launcher (claude, codex, traex); empty: what the launcher runs.
func (n *Node) start(ctx context.Context, cwd, launcher, prompt, resume, workspaceID, mode, effort, tabName, newID, agentType string, options ...modelExecution) (*proto.Session, error) {
	execution := executionOption(options)
	if err := execution.check(); err != nil {
		return nil, err
	}
	if resume != "" && n.externalCodex(resume) {
		return nil, refusal("codex_read_only", "不能用启动器恢复外部 Codex 会话", nil)
	}
	cwd, launcher, err := n.checkStart(cwd, launcher, resume, mode, effort)
	if err != nil {
		return nil, err
	}
	kind := agentType
	if kind == "" {
		kind = launcherAgentType(launcher)
	}
	if kind != "claude" && kind != "codex" && kind != traexAgent {
		return nil, fmt.Errorf("unsupported agent type %q", kind)
	}
	if execution.Model != "" {
		agent, err := n.launcherModelCheck(ctx, launcher, execution.Model)
		if err != nil {
			return nil, err
		}
		if agent != "claude" && agent != "codex" {
			return nil, errors.New("普通 Agent 的启动只支持 Claude Code 或 Codex 启动器")
		}
	}
	if resume != "" {
		if a, ok := n.agent(resume); ok {
			return n.sessionFor(resume, a), nil // already running
		}
		if r := n.openElsewhereRefusal(resume); r != nil { // open in Claude's desktop app or a terminal (openelsewhere.go)
			return nil, r
		}
	}

	launch, err := hostCap[host.Launcher](n) // new sessions start in the engine (herdr only so far: herdr-core cannot start them)
	if err != nil {
		return nil, err
	}
	ws := workspaceID
	if ws == "" {
		if ws, err = n.workspaceFor(ctx, launch, cwd); err != nil {
			return nil, err
		}
	}
	if argv, ok := n.codexLauncher(ctx, launcher); ok && kind != traexAgent { // codex_start.go
		if resume != "" {
			return nil, refusal("codex_resume", "Codex 会话请在电脑上恢复（codex resume）：从手机只能新建。", nil)
		}
		if execution.Model != "" {
			argv = append(withoutModelArgs(argv), "--model", execution.Model)
		}
		if execution.Effort != "" {
			return nil, errors.New("新建 Codex 的思考强度由其启动器管理，请将此用途的思考强度恢复继承")
		}
		return n.startCodex(ctx, launch, cwd, launcher, argv, prompt, ws, tabName)
	}
	if kind != "claude" { // start_agent.go
		if mode != "" || effort != "" {
			return nil, fmt.Errorf("permission mode and effort are only supported for Claude starts")
		}
		return n.startOtherAgent(ctx, launch, cwd, launcher, kind, prompt, resume, ws, tabName)
	}
	if err := n.codexFunctionLauncher(ctx, launcher, true); err != nil {
		return nil, err
	}
	id := resume
	if id == "" {
		id = newID
	}
	if id == "" {
		id = newUUID()
	}
	var created *host.Created
	if ws == "" {
		if created, err = launch.CreateSpace(ctx, cwd, filepath.Base(cwd)); err == nil && created.SlotID != "" {
			_ = renameSlot(ctx, launch, created.SlotID, tabLabel(tabName, nil)) // its first tab
		}
	} else {
		created, err = launch.CreateSlot(ctx, ws, cwd, tabLabel(tabName, n.tabLabels(ctx, launch, ws)))
	}
	if err != nil {
		return nil, fmt.Errorf("create herdr tab: %w", err)
	}
	pane := created.Pane.String()
	if name := n.starts.tabCreated(id, created.SlotID); name != "" { // named while the tab was being made
		_ = renameSlot(ctx, launch, created.SlotID, name)
	}

	// A session whose first start failed (claude never came up) has no transcript to resume: start it under its id.
	args := "--session-id " + id
	if resume != "" && n.transcriptPath(id) != "" {
		args = "--resume " + id
	}
	// Persona's hooks: a plugin, or in --settings for a claude that loads none (hooksplugin.go).
	settings, hooks := n.claudeStartSettings(ctx, launcher, "claude", cwd)
	cmd := n.launcherCommand(launcher) + " " + args + modeFlags(mode, effort)
	if settings != "" {
		cmd += " --settings " + shellQuote(settings)
	}
	cmd += " --mcp-config " + shellQuote(filepath.Join(n.dir, "mcp.json"))
	if execution.Model != "" {
		cmd += " --model " + shellQuote(execution.Model)
	}
	if n.claudeLoadsPlugins(ctx) {
		cmd += " --plugin-dir " + shellQuote(n.pluginDir()) + " --allowedTools " + shellQuote(officeRule(n.agentOfficeScript()))
	}
	if hooks != "" {
		cmd += " --plugin-dir " + shellQuote(hooks)
	}
	st := sessionState{Pane: pane, Launcher: launcher, Cwd: cwd, StartedAt: time.Now().UTC(), Model: execution.Model, ModelFingerprint: execution.Fingerprint}
	if prev, ok := n.state.get(id); ok && resume != "" {
		st.StartedAt = prev.StartedAt
	}
	n.state.put(id, st)
	n.noteModes(id, mode, effort) // shown before the first prompt writes them down
	// Give the fresh shell a moment to load ~/.zshrc; typed input is buffered either way.
	time.Sleep(300 * time.Millisecond)
	if err := launch.Run(ctx, created.Pane, cmd); err != nil {
		return nil, fmt.Errorf("run launcher: %w", err)
	}

	a, err := n.waitReady(ctx, pane, 60*time.Second)
	if err != nil {
		screen, _ := n.readPane(ctx, pane, "recent-unwrapped", 30)
		return nil, fmt.Errorf("%v\n%s", err, lastLines(screen, 12))
	}
	n.bindPane(pane, id)
	_ = launch.NameAgent(ctx, created.Pane, "j-"+id[:6])
	if prompt != "" {
		if err := n.hostPrompt(ctx, pane, prompt); err != nil {
			return nil, fmt.Errorf("send first message: %w", err)
		}
	}
	return n.sessionFor(id, *a), nil
}

// checkStart validates a start's arguments and returns the directory (~ expanded, home when empty) and launcher.
func (n *Node) checkStart(cwd, launcher, resume, mode, effort string) (string, string, error) {
	if launcher == "" {
		launcher = "claude"
	}
	if !nameRe.MatchString(launcher) {
		return "", "", fmt.Errorf("invalid launcher %q", launcher)
	}
	if resume != "" && !validID(resume) {
		return "", "", fmt.Errorf("invalid session id %q", resume)
	}
	if err := validModes(mode, effort); err != nil {
		return "", "", err
	}
	if strings.HasPrefix(cwd, "~/") {
		cwd = filepath.Join(n.home, cwd[2:])
	}
	if cwd == "" {
		cwd = n.home
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return "", "", fmt.Errorf("directory %s does not exist", cwd)
	}
	return cwd, launcher, nil
}

// tabLabels are the names of the workspace's tabs.
func (n *Node) tabLabels(ctx context.Context, launch host.Launcher, ws string) map[string]bool {
	taken := map[string]bool{}
	h, ok := launch.(host.Host)
	if !ok {
		return taken
	}
	if snap, err := h.Snapshot(ctx); err == nil {
		for _, t := range snap.Slots {
			if t.SpaceID == ws {
				taken[t.Label] = true
			}
		}
	}
	return taken
}

// tabLabel is the name a new tab gets: the given one (one line, at most 32 characters), else agent-<n>; a name the
// workspace already has gets -2, -3….
func tabLabel(name string, taken map[string]bool) string {
	name = strings.Join(strings.Fields(name), " ")
	if r := []rune(name); len(r) > 32 {
		name = strings.TrimSpace(string(r[:32]))
	}
	if name == "" {
		for i := 1; ; i++ {
			if l := fmt.Sprintf("agent-%d", i); !taken[l] {
				return l
			}
		}
	}
	if !taken[name] {
		return name
	}
	for i := 2; ; i++ {
		if l := fmt.Sprintf("%s-%d", name, i); !taken[l] {
			return l
		}
	}
}

// workspaceFor reuses the workspace named after the project directory (the user keeps one workspace per project).
func (n *Node) workspaceFor(ctx context.Context, launch host.Launcher, cwd string) (string, error) {
	wss, err := launch.Spaces(ctx)
	if err != nil {
		// Nothing was started or typed: say so, and what to do, instead of herdr's own words (the app showed
		// "herdr workspace list: exec: \"herdr\": executable file not found").
		name := n.defaultEngineName()
		return "", refusal(engineMissing, fmt.Sprintf("这台电脑上的 %s 没在运行（或没装），不能在这里新开或接着会话，这次没有发送。"+
			"要从手机接着电脑上开着的会话，在那边继续；要从手机新开，先在这台电脑上装好并打开 %s。", name, name),
			map[string]any{"detail": "herdr is not running on this machine: " + err.Error()})
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
func (n *Node) waitReady(ctx context.Context, pane string, timeout time.Duration) (*host.Agent, error) {
	deadline := time.Now().Add(timeout)
	readyStreak := 0
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		screen, err := n.readPane(ctx, pane, "visible", 60)
		if err != nil {
			continue
		}
		if keys := trustKeys(screen); keys != nil {
			_ = n.sendKeys(ctx, pane, keys...)
			readyStreak = 0
			time.Sleep(time.Second)
			continue
		}
		a, err := n.paneAgent(ctx, pane)
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
		strings.Contains(screen, "bypass permissions") || strings.Contains(screen, "esc to interrupt") ||
		// Claude Code 2.1.206 in default (manual) mode: "⏸ manual mode on · ← for agents", no shift+tab hint.
		strings.Contains(screen, "manual mode on") || strings.Contains(screen, "← for agents")
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

func (n *Node) sessionFor(id string, a host.Agent) *proto.Session {
	s := &proto.Session{
		SessionID: id, Cwd: a.Cwd, Status: a.AgentStatus, PaneID: a.PaneID, TabID: a.TabID,
		WorkspaceID: a.WorkspaceID, AgentName: a.Name, Title: a.Title, Agent: a.Agent, UpdatedAt: time.Now().UTC(), // just started
	}
	_ = n.refreshHosts(context.Background())
	n.placeIn(s, a)
	n.decorate(s)
	s.PermissionMode, s.Effort = n.modesOf(id)
	return s
}

// paneFor returns the pane hosting a live session, refreshing herdr state once if needed.
func (n *Node) paneFor(ctx context.Context, id string) (string, error) {
	if a, ok := n.agent(id); ok {
		return a.PaneID, nil
	}
	if err := n.refreshHosts(ctx); err == nil { // the whole snapshot: agents the host does not detect (TRAEX) too
		if a, ok := n.agent(id); ok {
			return a.PaneID, nil
		}
	}
	// A botmux session parked for too long to be listed is still botmux's: a reply goes to botmux, which resumes it,
	// not to a claude --resume of ours.
	if h, ok := n.hosts.Get(host.Botmux).(*botmuxHost); ok {
		if _, a, err := h.row(ctx, id); err == nil {
			return a.PaneID, nil
		}
	}
	return "", fmt.Errorf("session %s is not running", id)
}

func (n *Node) send(ctx context.Context, id, text string, images []proto.Image, opts sendOpts) error {
	// Two sends at once (quick taps, two phones) would both find the box empty and type into each other, or the second
	// would see the first still in the box and be refused as a draft: one at a time.
	defer n.typing.lock(id)()
	if n.externalCodex(id) {
		return n.sendCodex(ctx, id, text, images)
	}
	// Written while the session still starts (the app opens the chat at once): it goes in once claude is ready.
	if err := n.starts.wait(ctx, id); err != nil {
		return err
	}
	n.dropSuggestion(id)
	pane, err := n.paneFor(ctx, id)
	if err != nil && (n.codexPath(id) != "" || n.traexPath(id) != "") {
		return fmt.Errorf("这个 Agent 已经退出：Codex / TRAEX 的会话要在电脑上重新打开")
	}
	kind := "claude"
	if err == nil {
		if a, ok := n.agent(id); ok && a.Agent != "" {
			kind = a.Agent
		}
		if kind == "codex" && n.codexJoined(id) {
			// On its app-server (codex_daemon.go): a turn or a steer, nothing typed — a draft in its box stays as it is.
			return n.sendCodex(ctx, id, text, images)
		}
		if err := n.checkPane(ctx, pane, kind, opts.ClearDraft); err != nil { // sendsafe.go
			return err
		}
	} else {
		if r := n.notRunningRefusal(ctx, id); r != nil {
			return r
		}
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
		mode, effort := n.modesOf(id) // resume with what it ran with
		s, err := n.start(ctx, cwd, launcher, "", id, "", mode, effort, "", "", "")
		if err != nil {
			return err
		}
		pane = s.PaneID
		// start returns early when Claude comes up blocked (a dialog of its own): the text would answer it.
		if err := n.checkPane(ctx, pane, "claude", ""); err != nil {
			return err
		}
	}
	paths, err := n.saveImages(images)
	if err != nil {
		return err
	}
	if len(paths) > 0 {
		// In a tag so it never mixes with what the user typed; the phone shows these as thumbnails.
		text = strings.TrimSpace(text) + "\n\n<attached_images>\n" + strings.Join(paths, "\n") + "\n</attached_images>"
	}
	err = n.prompt(ctx, pane, text)
	if errors.Is(err, host.ErrAgentBlocked) && (kind == "codex" || kind == traexAgent) {
		// herdr refuses to prompt a Codex with an async question open; its box (checked above) takes the message.
		err = n.codexType(ctx, pane, text, func(draft string) bool { return stillPrompted(draft, text) || pastedRE.MatchString(draft) })
	}
	if r := deliveryRefusal(err); r != nil { // the host refused it with nothing typed (herdr-core)
		return r
	}
	if err == nil {
		n.typing.typed(pane, text)
		if kind == "claude" {
			n.ensureSubmitted(ctx, pane, text)
		}
	}
	return uncertain(err)
}

// prompt submits text to the agent in a pane. A host only prompts agents it detects (herdr); for the others (TRAEX)
// the text is typed into the pane and enter pressed.
func (n *Node) prompt(ctx context.Context, pane, text string) error {
	err := n.hostPrompt(ctx, pane, text)
	if err == nil || !errors.Is(err, host.ErrNoAgent) {
		return err
	}
	if err := n.sendText(ctx, pane, text); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond) // the TUI takes the pasted text before the enter that submits it
	return n.sendKeys(ctx, pane, "enter")
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
	n.dropSuggestion(id) // whatever goes in replaces or sends it
	pane, err := n.paneFor(ctx, id)
	if err != nil {
		return err
	}
	if text != "" && !n.canTypeText(pane) {
		// A host that cannot type without submitting: text and then enter is a prompt; anything else was refused
		// before any key was pressed (typingRefusal).
		if len(keys) != 1 || keys[0] != "enter" {
			return refusal(cannotType, cannotTypeMessage, nil)
		}
		err := n.hostPrompt(ctx, pane, text)
		if r := deliveryRefusal(err); r != nil {
			return r
		}
		return err
	}
	if text != "" {
		if err := n.sendText(ctx, pane, text); err != nil {
			return err
		}
	}
	if len(keys) > 0 {
		return n.sendKeys(ctx, pane, keys...)
	}
	return nil
}

// screen reads the session's terminal. source "visible" is the screen; "recent" also reaches back into the pane's
// scrollback (the phone pages it: ask for more lines until fewer come back than were asked for). ansi keeps the colors
// (SGR escapes; rows end in \r\n).
func (n *Node) screen(ctx context.Context, id string, lines int, source string, ansi bool) (string, string, error) {
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
	if ansi && n.plainScreen(a.PaneID) {
		// No colors in this host: the plain rows, ended as colored ones are.
		text, err := n.readPane(ctx, a.PaneID, source, lines)
		if err == nil && text != "" {
			text = strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", "\r\n") + "\r\n"
		}
		return text, a.AgentStatus, err
	}
	if ansi {
		text, err := n.readPaneANSI(ctx, a.PaneID, source, lines)
		return text, a.AgentStatus, err
	}
	text, err := n.readPane(ctx, a.PaneID, source, lines)
	return text, a.AgentStatus, err
}

// close quits any Claude running in the machine's engine (started by Persona or by hand) and closes its pane: herdr
// closes its tab, or in a split tab only its own pane, so the other panes survive; herdr-core ends its terminal.
func (n *Node) close(ctx context.Context, id string) error {
	a, ok := n.agent(id)
	if !ok {
		if _, mine := n.state.get(id); mine {
			return nil // already gone
		}
		return fmt.Errorf("这个 Agent 不在这台电脑的终端里运行，没法从手机关闭")
	}
	org, ref, err := paneCap[host.Organizer](n, a.PaneID) // a host that cannot close it is not interrupted first
	if err != nil {
		return err
	}
	_ = n.sendKeys(ctx, a.PaneID, "ctrl+c")
	time.Sleep(200 * time.Millisecond)
	_ = n.sendKeys(ctx, a.PaneID, "ctrl+c")
	time.Sleep(500 * time.Millisecond)
	return org.Close(ctx, ref, a.WorkspaceID, a.TabID)
}

// watch streams new transcript items of a session to the hub.
type watch struct {
	cancel context.CancelFunc
}

func (n *Node) watch(id string, since int) error {
	f, err := n.file(id)
	if err != nil {
		if !n.startingSession(id) {
			return err
		}
		// A session that just started has no transcript until claude writes its first line: watch once it exists,
		// sending everything written before the watch began (the phone's backlog was empty).
		go func() {
			for i := 0; i < 120; i++ {
				time.Sleep(500 * time.Millisecond)
				f, err := n.file(id)
				if err != nil {
					continue
				}
				mu := n.pushLock(id)
				mu.Lock()
				if _, err := f.Refresh(); err == nil {
					since = n.catchUp(id, f, since)
				}
				mu.Unlock()
				_ = n.watch(id, since)
				return
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
	n.watches[id] = &watch{cancel: cancel}
	n.mu.Unlock()

	// Catch up on lines appended after the hub's last seen seq, then stream. Without a seq (the hub has not seen this
	// session since it started) the lines this Refresh appends still go out — watchLoop only pushes what later
	// Refreshes find — unless it is the first read of the file: that is the whole history, the subscriber's backlog.
	mu := n.pushLock(id)
	mu.Lock()
	readBefore := f.LastSeq() > 0
	if fresh, span, err := f.RefreshSpan(); err == nil {
		if since > 0 {
			n.catchUp(id, f, since)
		} else if readBefore && span.To > span.From {
			n.pushItems(id, fresh, span)
		}
	}
	mu.Unlock()
	go n.watchLoop(ctx, id, f)
	return nil
}

// catchUp pushes every item after since, a page at a time (one page used to be all: after 500 the rest was lost), and
// returns the last seq sent.
func (n *Node) catchUp(id string, f *transcript.File, since int) int {
	for {
		items, more, span := f.After(since, 500)
		n.pushItems(id, f.WithImages(items), span) // the hub sends them to every subscriber, lazy or not
		since = span.To
		if !more {
			return since
		}
	}
}

func (n *Node) pushItems(id string, items []proto.Item, span transcript.Span) {
	if n.peer == nil {
		return
	}
	n.mu.Lock()
	f := n.files[id]
	n.mu.Unlock()
	items = n.echoed(id, f, items)
	_ = n.peer.Notify("session.items", withSpan(map[string]any{"session_id": id, "items": nonNil(items)}, span, true))
}

// pushLock serializes reading a session's new lines with pushing them, so its batches reach the hub in order.
func (n *Node) pushLock(id string) *sync.Mutex {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.pushLocks == nil {
		n.pushLocks = map[string]*sync.Mutex{}
	}
	mu := n.pushLocks[id]
	if mu == nil {
		mu = &sync.Mutex{}
		n.pushLocks[id] = mu
	}
	return mu
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

// refresh parses new transcript lines and pushes them to the hub when the session is watched — also when the new
// lines hold no items (titles, modes): the batch still tells clients they have everything up to its to_seq, so the
// next one does not look like it skipped something.
func (n *Node) refresh(id string, f *transcript.File) {
	mu := n.pushLock(id)
	mu.Lock()
	defer mu.Unlock()
	fresh, span, err := f.RefreshSpan()
	if err != nil || span.To == span.From {
		return
	}
	n.mu.Lock()
	_, watched := n.watches[id]
	n.mu.Unlock()
	if watched {
		n.pushItems(id, fresh, span)
	}
}

// watchLoop pushes new transcript lines four times a second. (It used to read the pane's screen every 400 ms too, a
// herdr call per watched session, for a live preview no view showed; the terminal pane reads the screen itself.)
func (n *Node) watchLoop(ctx context.Context, id string, f *transcript.File) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.refresh(id, f)
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
	if err := n.sendKeys(ctx, pane, "up"); err != nil {
		return nil, err
	}
	time.Sleep(300 * time.Millisecond)
	// ctrl+u only clears the current line: walk back through every line of the pulled-back text.
	lines := 0
	for _, q := range queue {
		lines += strings.Count(q, "\n") + 1
	}
	for i := 0; i < lines; i++ {
		_ = n.sendKeys(ctx, pane, "ctrl+u", "backspace")
	}
	_ = n.sendKeys(ctx, pane, "ctrl+u")
	for _, k := range keep {
		if k >= 0 && k < len(queue) {
			if err := n.prompt(ctx, pane, queue[k]); err != nil {
				return queue, fmt.Errorf("re-queue: %w", err)
			}
			time.Sleep(150 * time.Millisecond)
		}
	}
	return queue, nil
}

// renameSpace renames a herdr workspace (what the phone calls a space).
func (n *Node) renameSpace(ctx context.Context, workspaceID, name string) error {
	name = strings.TrimSpace(name)
	if workspaceID == "" || name == "" {
		return fmt.Errorf("workspace and name are required")
	}
	org, err := hostCap[host.Organizer](n) // space ids carry no host kind yet: the engine's
	if err != nil {
		return err
	}
	return org.RenameSpace(ctx, workspaceID, name)
}

// renameSession renames the herdr tab a session runs in and / or the Claude session itself. A live claude gets
// `/rename <title>` (it records a custom-title, shown in its prompt, /resume and our lists); for one that is not
// running we append the same custom-title line Claude Code writes.
func (n *Node) renameSession(ctx context.Context, id string, tab, title *string) error {
	if tab != nil {
		if name := strings.TrimSpace(*tab); name != "" {
			if done, tabID := n.starts.nameTab(id, name); done {
				tab = nil
			} else if tabID != "" { // a tab start() made: in the host sessions start in
				org, err := hostCap[host.Organizer](n)
				if err != nil {
					return err
				}
				if err := org.RenameSlot(ctx, tabID, name); err != nil {
					return err
				}
				tab = nil
			}
		}
	}
	a, live := n.agent(id)
	if tab != nil {
		name := strings.TrimSpace(*tab)
		if name == "" || !live || a.TabID == "" {
			return fmt.Errorf("the tab can only be renamed while the session runs in herdr")
		}
		org, _, err := paneCap[host.Organizer](n, a.PaneID)
		if err != nil {
			return err
		}
		if err := org.RenameSlot(ctx, a.TabID, name); err != nil {
			return err
		}
	}
	if title != nil {
		t := strings.Join(strings.Fields(*title), " ")
		if t == "" {
			return fmt.Errorf("empty title")
		}
		if live && !n.isAssistantSession(id) {
			return n.prompt(ctx, a.PaneID, "/rename "+t)
		}
		p := n.transcriptPath(id)
		if p == "" {
			return fmt.Errorf("transcript for session %s not found", id)
		}
		line, _ := json.Marshal(map[string]string{"type": "custom-title", "customTitle": t, "sessionId": id})
		f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.Write(append(line, '\n'))
		return err
	}
	return nil
}

// herdr keeps about 1000 lines per pane readable (pane read caps there whatever --lines asks); older output is only
// in Claude's transcript.
const herdrScrollbackLines = 1000

// screenBefore returns up to `lines` lines of the pane's scrollback right before the anchor (the first lines the phone
// already shows). Located by content, not by offset from the end, so output arriving meanwhile shifts nothing.
// loaded is how many lines the phone already has (from the end): Claude's redraws push the same screens into the
// scrollback again and again, so the anchor can match in several places — the one nearest len-loaded is ours.
// With ansi the page keeps its colors; the anchor is still matched on the plain text.
func (n *Node) screenBefore(ctx context.Context, id string, anchor []string, lines, loaded int, ansi bool) (map[string]any, error) {
	if lines <= 0 || lines > 500 {
		lines = 200
	}
	a, ok := n.agent(id)
	if !ok {
		if _, err := n.paneFor(ctx, id); err != nil {
			return nil, err
		}
		a, _ = n.agent(id)
	}
	text, err := n.readPane(ctx, a.PaneID, "recent", herdrScrollbackLines)
	if err != nil {
		return nil, err
	}
	all := strings.Split(strings.TrimRight(text, "\n"), "\n")
	at := nearestBlock(all, anchor, len(all)-loaded)
	if at < 0 { // the anchor scrolled out of the kept window: nothing older can be placed before it
		return map[string]any{"text": "", "has_more": false, "at_limit": len(all) >= herdrScrollbackLines-20}, nil
	}
	start := max(at-lines, 0)
	page := all[start:at]
	if ansi {
		// The same rows with their colors: herdr reads both from one grid, so row i is row i — unless output arrived
		// between the two reads, then the plain page is still right.
		if colored, err := n.readPaneANSI(ctx, a.PaneID, "recent", herdrScrollbackLines); err == nil {
			rows := strings.Split(strings.TrimRight(strings.ReplaceAll(colored, "\r\n", "\n"), "\n"), "\n")
			if len(rows) == len(all) {
				page = rows[start:at]
			}
		}
	}
	return map[string]any{
		"text":     strings.Join(page, "\n"),
		"has_more": start > 0,
		// The start of what herdr keeps: older output was dropped (long panes) or never existed (short ones).
		"at_limit": start == 0 && len(all) >= herdrScrollbackLines-20,
	}, nil
}

// nearestBlock is the match of block whose start is closest to want, -1 when it does not appear.
func nearestBlock(all, block []string, want int) int {
	best := -1
	for i := len(all) - len(block); i >= 0; {
		j := findBlock(all[:i+len(block)], block)
		if j < 0 {
			break
		}
		if best < 0 || abs(j-want) < abs(best-want) {
			best = j
		}
		i = j - 1
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// findBlock returns where the lines of block start in all (the last match), -1 if they do not appear.
func findBlock(all, block []string) int {
	if len(block) == 0 {
		return len(all)
	}
	for i := len(all) - len(block); i >= 0; i-- {
		match := true
		for j, b := range block {
			if strings.TrimRight(all[i+j], " ") != strings.TrimRight(b, " ") {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
