package node

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/transcript"
)

// stateFile remembers which sessions Jarvis started and with which launcher. It lives in the hub's Postgres:
// loaded with `state.load` when the node starts and written through with `state.put` events.
type stateFile struct {
	mu       sync.Mutex
	Sessions map[string]sessionState `json:"sessions"`
	persist  func(id string, v sessionState)
}

type sessionState struct {
	Pane      string    `json:"pane,omitempty"`
	Launcher  string    `json:"launcher"`
	Cwd       string    `json:"cwd"`
	StartedAt time.Time `json:"started_at"`
}

func newState() *stateFile { return &stateFile{Sessions: map[string]sessionState{}} }

// legacyState reads the pre-Postgres ~/.jarvis/sessions.json, if any.
func legacyState(path string) map[string]sessionState {
	var f struct {
		Sessions map[string]sessionState `json:"sessions"`
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &f)
	}
	return f.Sessions
}

// merge adds sessions loaded from the hub without overwriting ones touched since the node started.
func (s *stateFile) merge(m map[string]sessionState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, v := range m {
		if _, ok := s.Sessions[id]; !ok {
			s.Sessions[id] = v
		}
	}
}

func (s *stateFile) get(id string) (sessionState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.Sessions[id]
	return v, ok
}

// panes returns the remembered pane of every session (herdr pane ids are not reused while the server lives).
func (s *stateFile) panes() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for id, v := range s.Sessions {
		if v.Pane != "" {
			out[v.Pane] = id
		}
	}
	return out
}

func (s *stateFile) setPane(id, pane string) {
	s.mu.Lock()
	v, ok := s.Sessions[id]
	if !ok || v.Pane == pane {
		s.mu.Unlock()
		return
	}
	v.Pane = pane
	s.Sessions[id] = v
	s.mu.Unlock()
	s.put(id, v)
}

func (s *stateFile) put(id string, v sessionState) {
	s.mu.Lock()
	s.Sessions[id] = v
	persist := s.persist
	s.mu.Unlock()
	if persist != nil {
		persist(id, v)
	}
}

// quickMeta is a cheap summary of a transcript read from its head and tail only.
type quickMeta struct {
	size     int64
	mtime    time.Time
	title    string
	first    string
	activity time.Time // last real exchange; zero when unknown
	queue    []string
	loop     *proto.LoopInfo
	bg       []proto.BackgroundTask // from backgroundTasks: the tail alone misses tasks started earlier in a long turn
	cwd      string
	model    string
	lastText string
	wake     bool // the latest turn was a background task waking the agent
}

const offlineWindow = 7 * 24 * time.Hour

func (n *Node) projectsDir() string { return filepath.Join(n.home, ".claude", "projects") }

func (n *Node) meta(path string, st os.FileInfo) quickMeta {
	n.mu.Lock()
	m, ok := n.metas[path]
	n.mu.Unlock()
	if ok && m.size == st.Size() && m.mtime.Equal(st.ModTime()) {
		return m
	}
	m = readQuickMeta(path, st)
	m.bg, m.loop = n.backgroundTasks(path)
	n.mu.Lock()
	n.metas[path] = m
	n.mu.Unlock()
	return m
}

func readQuickMeta(path string, st os.FileInfo) quickMeta {
	m := quickMeta{size: st.Size(), mtime: st.ModTime()}
	f, err := os.Open(path)
	if err != nil {
		return m
	}
	defer f.Close()
	var tm transcript.Meta
	head := make([]byte, 32*1024)
	nr, _ := f.Read(head)
	for _, line := range bytes.Split(head[:nr], []byte("\n")) {
		if bytes.Contains(line, []byte(`"cwd"`)) || bytes.Contains(line, []byte(`"type":"user"`)) ||
			bytes.Contains(line, []byte(`"type":"UserMessage"`)) { // Codex rollouts: session_meta, then the first prompt
			transcript.ParseLine(0, line, &tm)
			if tm.Cwd != "" && tm.FirstUser != "" {
				break
			}
		}
	}
	first := tm.FirstUser
	const tail = 384 * 1024
	off := max(st.Size()-tail, 0)
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err == nil || len(buf) > 0 {
		lines := bytes.Split(buf, []byte("\n"))
		if off > 0 && len(lines) > 0 {
			lines = lines[1:] // first line is partial
		}
		for _, line := range lines {
			if len(line) == 0 {
				continue
			}
			// Skip big tool results quickly; small user lines still count as activity.
			if bytes.Contains(line, []byte(`"type":"user"`)) && len(line) > 8<<10 {
				continue
			}
			transcript.ParseLine(0, line, &tm)
		}
	}
	m.title, m.cwd, m.model, m.lastText, m.first, m.activity, m.queue = tm.Title, tm.Cwd, tm.Model, tm.LastText, first, tm.LastActivity, tm.Queue
	m.wake = tm.LastTurnWake
	return m
}

// transcriptPath finds ~/.claude/projects/*/<id>.jsonl.
func (n *Node) transcriptPath(id string) string {
	n.mu.Lock()
	p := n.paths[id]
	n.mu.Unlock()
	if p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if !validID(id) {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(n.projectsDir(), "*", id+".jsonl"))
	if len(matches) == 0 {
		if p := n.codexPath(id); p != "" {
			matches = []string{p}
		} else {
			return ""
		}
	}
	n.mu.Lock()
	n.paths[id] = matches[0]
	n.mu.Unlock()
	return matches[0]
}

var idRe = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

func validID(id string) bool { return idRe.MatchString(id) }

func (n *Node) file(id string) (*transcript.File, error) {
	n.mu.Lock()
	f := n.files[id]
	n.mu.Unlock()
	if f != nil {
		return f, nil
	}
	if _, _, sub := splitSubagent(id); sub {
		var err error
		if f, err = n.subagentFile(id); err != nil {
			return nil, err
		}
	} else {
		p := n.transcriptPath(id)
		if p == "" {
			return nil, fmt.Errorf("transcript for session %s not found", id)
		}
		f = transcript.NewFile(p)
	}
	n.mu.Lock()
	if existing := n.files[id]; existing != nil {
		f = existing
	} else {
		n.files[id] = f
	}
	n.mu.Unlock()
	return f, nil
}

// sessionList returns live herdr sessions plus recent offline transcripts.
func (n *Node) sessionList(ctx context.Context, refreshAgents bool) []proto.Session {
	if refreshAgents {
		_ = n.refreshHerdr(ctx)
	}
	n.mu.Lock()
	agents := make(map[string]herdr.Agent, len(n.agents))
	for k, v := range n.agents {
		agents[k] = v
	}
	n.mu.Unlock()

	var out []proto.Session
	seen := map[string]bool{}
	for id, a := range agents {
		s := proto.Session{
			SessionID: id, Cwd: a.Cwd, Status: a.AgentStatus, PaneID: a.PaneID, TabID: a.TabID,
			WorkspaceID: a.WorkspaceID, AgentName: a.Name, Title: a.Title, Agent: a.Agent,
		}
		if a.Agent == "codex" {
			s.Title = n.codexTitle(id) // the terminal title is "dots | codex | Ready"
		}
		n.placeIn(&s, a)
		if p := n.transcriptPath(id); p != "" {
			if st, err := os.Stat(p); err == nil {
				m := n.meta(p, st)
				if m.title != "" && a.Agent != "codex" {
					s.Title = m.title
				}
				if s.Title == "" || s.Title == "Claude Code" {
					s.Title = ""
					untitled(&s, m)
				}
				s.Model, s.LastText, s.UpdatedAt = m.model, snippet(m.lastText), lastUsed(m, st)
				s.WokeByTask = m.wake
				if a.AgentStatus == proto.StatusWorking || a.AgentStatus == proto.StatusBlocked {
					s.Queued = m.queue // an idle agent has taken everything; stale entries are from outside the tail
				}
				// Background shells keep running after the turn ends; background subagents are listed as subagents.
				s.Background = nil
				for _, t := range m.bg {
					if t.Kind != "agent" {
						s.Background = append(s.Background, t)
					}
				}
				s.Loop = m.loop
				s.Subagents = n.subagents(id)
			}
		}
		n.decorate(&s)
		out = append(out, s)
		seen[id] = true
	}

	for _, s := range n.assistantSessions() {
		if !seen[s.SessionID] {
			out = append(out, s)
			seen[s.SessionID] = true
		}
	}
	for _, s := range n.offline(seen) {
		n.decorate(&s)
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

var offlineCache struct {
	sync.Mutex
	at   time.Time
	list []proto.Session
}

// offline lists recent transcripts without a live agent; the directory scan is cached for 20s.
func (n *Node) offline(live map[string]bool) []proto.Session {
	offlineCache.Lock()
	defer offlineCache.Unlock()
	if time.Since(offlineCache.at) > 20*time.Second {
		offlineCache.list = n.scanOffline()
		offlineCache.at = time.Now()
	}
	var out []proto.Session
	for _, s := range offlineCache.list {
		if !live[s.SessionID] {
			out = append(out, s)
		}
	}
	return out
}

func (n *Node) scanOffline() []proto.Session {
	var out []proto.Session
	files, _ := filepath.Glob(filepath.Join(n.projectsDir(), "*", "*.jsonl"))
	type cand struct {
		path string
		st   os.FileInfo
	}
	var cands []cand
	for _, p := range files {
		id := strings.TrimSuffix(filepath.Base(p), ".jsonl")
		if !validID(id) {
			continue
		}
		st, err := os.Stat(p)
		if err != nil || time.Since(st.ModTime()) > offlineWindow || st.Size() < 2048 {
			continue
		}
		cands = append(cands, cand{p, st})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].st.ModTime().After(cands[j].st.ModTime()) })
	if len(cands) > 80 {
		cands = cands[:80]
	}
	for _, c := range cands {
		id := strings.TrimSuffix(filepath.Base(c.path), ".jsonl")
		m := n.meta(c.path, c.st)
		if m.lastText == "" && m.title == "" {
			continue // sessions that never produced output (e.g. hook probes)
		}
		n.mu.Lock()
		n.paths[id] = c.path
		n.mu.Unlock()
		s := proto.Session{
			SessionID: id, Title: m.title, Cwd: m.cwd, Status: proto.StatusOffline, Model: m.model,
			LastText: snippet(m.lastText), UpdatedAt: lastUsed(m, c.st),
		}
		untitled(&s, m)
		out = append(out, s)
	}
	return out
}

// lastUsed is when the session was last really used: its last exchange, or the file time for transcripts
// without any (only system lines).
func lastUsed(m quickMeta, st os.FileInfo) time.Time {
	if !m.activity.IsZero() {
		return m.activity.UTC()
	}
	return st.ModTime().UTC()
}

// untitled gives sessions without an AI title (headless ones) their first message as title.
func untitled(s *proto.Session, m quickMeta) {
	if s.Title == "" && m.first != "" {
		r := []rune(snippet(m.first))
		if len(r) > 40 {
			r = append(r[:40], '…')
		}
		s.Title = string(r)
	}
}

func (n *Node) decorate(s *proto.Session) {
	if st, ok := n.state.get(s.SessionID); ok {
		s.StartedByJarvis = true
		s.Launcher = st.Launcher
		if s.Cwd == "" {
			s.Cwd = st.Cwd
		}
	}
	if s.Title == "" {
		s.Title = filepath.Base(s.Cwd)
	}
}

func snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}

// setAgents maps live claude agents to session ids: herdr's own report when its claude integration is installed,
// else what our hooks told us about the pane, else the newest transcript in the agent's project directory.
// layout remembers workspace and tab names from the last herdr snapshot.
type layout struct {
	workspaces map[string]herdr.Workspace
	tabs       map[string]herdr.Tab
}

// refreshHerdr takes one snapshot of herdr: agents plus the workspace / tab names shown as space / slot.
func (n *Node) refreshHerdr(ctx context.Context) error {
	snap, err := n.herdr.Snapshot(ctx)
	if err != nil {
		// Older herdr without `api snapshot`: fall back to the agent list.
		agents, aerr := n.herdr.Agents(ctx)
		if aerr != nil {
			return err
		}
		n.setAgents(agents)
		return nil
	}
	l := layout{workspaces: map[string]herdr.Workspace{}, tabs: map[string]herdr.Tab{}}
	for _, w := range snap.Workspaces {
		l.workspaces[w.ID] = w
	}
	for _, t := range snap.Tabs {
		l.tabs[t.ID] = t
	}
	n.mu.Lock()
	n.layout = l
	n.mu.Unlock()
	n.setAgents(snap.Agents)
	return nil
}

// placeIn fills the space / slot fields of a live session.
func (n *Node) placeIn(s *proto.Session, a herdr.Agent) {
	n.mu.Lock()
	w, wok := n.layout.workspaces[a.WorkspaceID]
	t, tok := n.layout.tabs[a.TabID]
	n.mu.Unlock()
	s.SpaceKind, s.SpaceID, s.SlotID = "herdr", a.WorkspaceID, a.TabID
	if wok {
		s.SpaceName, s.SpaceIndex = w.Label, w.Number
	}
	if tok {
		s.SlotName, s.SlotIndex = t.Label, t.Number
	}
}

func (n *Node) setAgents(agents []herdr.Agent) {
	m := map[string]herdr.Agent{}
	var unknown, codex []herdr.Agent
	n.mu.Lock()
	for _, a := range agents {
		if a.Agent == "codex" {
			codex = append(codex, a)
			continue
		}
		if a.Agent != "claude" {
			continue
		}
		id := a.SessionID()
		if id == "" {
			id = n.panes[a.PaneID]
		}
		if id == "" {
			unknown = append(unknown, a)
			continue
		}
		m[id] = a
	}
	n.mu.Unlock()
	for _, a := range unknown {
		// Without herdr's claude integration (example-machine) the pane's claude process says which session it runs; a
		// guess (title, then newest transcript in the directory) is only the last resort — six agents in one repo
		// got each other's tab names that way.
		id := n.sessionFromProcess(a.PaneID)
		if _, taken := m[id]; id == "" || taken {
			id = n.guessSession(a, m)
		}
		if id != "" {
			m[id] = a
		}
	}
	for _, a := range codex {
		// herdr's codex integration reports the session id at start; a Jarvis-started one is bound to its pane;
		// otherwise the newest rollout in its directory nobody else has.
		id := a.SessionID()
		if id == "" {
			n.mu.Lock()
			id = n.panes[a.PaneID]
			n.mu.Unlock()
		}
		if _, taken := m[id]; id == "" || taken {
			id = n.codexSession(a, m)
		}
		if id != "" {
			m[id] = a
		}
	}
	n.mu.Lock()
	n.agents = m
	n.mu.Unlock()
}

// sessionFromProcess reads the session a pane's claude runs from ~/.claude/sessions/<pid>.json, which Claude Code
// keeps current (a /clear moves it to a new session id). The pane's pid is cached for a minute.
func (n *Node) sessionFromProcess(pane string) string {
	n.mu.Lock()
	c, ok := n.panePIDs[pane]
	n.mu.Unlock()
	if !ok || time.Since(c.checked) > time.Minute {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		pids, err := n.herdr.PaneProcesses(ctx, pane)
		cancel()
		if err != nil {
			return ""
		}
		c = panePID{checked: time.Now()}
		for _, pid := range pids {
			if _, err := os.Stat(n.claudeSessionFile(pid)); err == nil {
				c.pid = pid
				break
			}
		}
		n.mu.Lock()
		n.panePIDs[pane] = c
		n.mu.Unlock()
	}
	if c.pid == 0 {
		return ""
	}
	b, err := os.ReadFile(n.claudeSessionFile(c.pid))
	if err != nil {
		n.mu.Lock()
		delete(n.panePIDs, pane) // claude exited / restarted: look again next time
		n.mu.Unlock()
		return ""
	}
	var f struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(b, &f) != nil || !validID(f.SessionID) {
		return ""
	}
	return f.SessionID
}

type panePID struct {
	pid     int
	checked time.Time
}

func (n *Node) claudeSessionFile(pid int) string {
	return filepath.Join(n.home, ".claude", "sessions", strconv.Itoa(pid)+".json")
}

// bindPane records which session runs in a pane (from start() and from hook events).
func (n *Node) bindPane(pane, id string) {
	if pane == "" || !validID(id) {
		return
	}
	n.mu.Lock()
	n.panes[pane] = id
	n.mu.Unlock()
	n.state.setPane(id, pane)
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// guessSession picks a transcript for an agent herdr could not identify. Claude sets the terminal title to the
// session's AI title, so a title match wins; otherwise the newest unclaimed transcript in the project directory.
func (n *Node) guessSession(a herdr.Agent, taken map[string]herdr.Agent) string {
	if a.Cwd == "" {
		return ""
	}
	dir := filepath.Join(n.projectsDir(), nonAlnum.ReplaceAllString(a.Cwd, "-"))
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	title := strings.TrimSpace(a.Title)
	best, bestT := "", time.Time{}
	for _, p := range files {
		id := strings.TrimSuffix(filepath.Base(p), ".jsonl")
		if _, used := taken[id]; used || !validID(id) {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if title != "" && n.meta(p, st).title == title {
			return id
		}
		if st.ModTime().After(bestT) {
			best, bestT = id, st.ModTime()
		}
	}
	return best
}

func (n *Node) agent(id string) (herdr.Agent, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	a, ok := n.agents[id]
	return a, ok
}

// pollAgents refreshes herdr state every second and pushes sessions.changed when anything visible changes.
func (n *Node) pollAgents(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := n.refreshHerdr(ctx); err != nil {
			continue
		}
		list := n.sessionList(ctx, false)
		key := listKey(list)
		n.mu.Lock()
		changed := key != n.lastList
		n.lastList = key
		n.mu.Unlock()
		if changed && n.peer != nil {
			_ = n.peer.Notify("sessions.changed", map[string]any{"sessions": list})
		}
	}
}

// listKey ignores fields that churn without meaning (updated_at of live sessions).
func listKey(list []proto.Session) string {
	var b strings.Builder
	for _, s := range list {
		fmt.Fprintf(&b, "%s|%s|%s|%s|%s|%s|%s|%d|%q\n", s.SessionID, s.Status, s.Title, s.PaneID, s.SpaceName, s.SlotName, s.LastText, s.UpdatedAt.Unix()/10, s.Queued)
		for _, t := range s.Background {
			b.WriteString(t.ID + ",")
		}
		if s.Loop != nil {
			fmt.Fprintf(&b, "loop%d", s.Loop.NextAt.Unix())
		}
		for _, a := range s.Subagents {
			fmt.Fprintf(&b, "%s:%v:%d,", a.ID, a.Running, a.UpdatedAt.Unix()/10)
		}
	}
	return b.String()
}

var launcherCache struct {
	sync.Mutex
	at   time.Time
	list []proto.Launcher
}

// launchers lists the claude wrapper functions defined in ~/.zshrc (via the `_claude_launchers` helper).
func (n *Node) launchers(ctx context.Context) ([]proto.Launcher, error) {
	launcherCache.Lock()
	defer launcherCache.Unlock()
	if time.Since(launcherCache.at) < time.Minute && launcherCache.list != nil {
		return launcherCache.list, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := loginShell(ctx, "_claude_launchers 2>/dev/null || true").Output()
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("list launchers: %w", err)
	}
	list := parseLaunchers(string(out))
	if len(list) == 0 {
		list = []proto.Launcher{{Name: "claude", Model: "默认"}}
	}
	launcherCache.at, launcherCache.list = time.Now(), list
	return list, nil
}

var nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// parseLaunchers parses `_claude_launchers` output: name padded to 26, base URL padded to 40, then model.
func parseLaunchers(out string) []proto.Launcher {
	var list []proto.Launcher
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) == 0 || !nameRe.MatchString(f[0]) {
			continue
		}
		l := proto.Launcher{Name: f[0]}
		rest := ""
		if len(line) > 26 {
			rest = line[26:]
		}
		if len(rest) > 40 {
			l.BaseURL = strings.TrimSpace(rest[:40])
			l.Model = strings.TrimSpace(rest[40:])
		} else {
			l.BaseURL = strings.TrimSpace(rest)
		}
		if l.BaseURL == "-" {
			l.BaseURL = ""
		}
		list = append(list, l)
	}
	return list
}

func (n *Node) dirs(path string) (*proto.DirList, error) {
	if path == "" || path == "~" {
		path = n.home
	} else if strings.HasPrefix(path, "~/") {
		path = filepath.Join(n.home, path[2:])
	}
	path = filepath.Clean(path)
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	res := &proto.DirList{Path: path, Parent: filepath.Dir(path), Entries: []proto.DirEntry{}, Recent: n.recentDirs()}
	if path == "/" {
		res.Parent = ""
	}
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(path, name)
		if !e.IsDir() {
			if e.Type()&os.ModeSymlink == 0 {
				continue
			}
			if st, err := os.Stat(full); err != nil || !st.IsDir() {
				continue
			}
		}
		if strings.Contains(full, "Mobile Documents") { // never touch iCloud Drive: it hangs on locked Macs
			continue
		}
		_, gerr := os.Stat(filepath.Join(full, ".git"))
		res.Entries = append(res.Entries, proto.DirEntry{Name: name, Path: full, IsGit: gerr == nil})
	}
	sort.Slice(res.Entries, func(i, j int) bool {
		return strings.ToLower(res.Entries[i].Name) < strings.ToLower(res.Entries[j].Name)
	})
	return res, nil
}

func (n *Node) recentDirs() []proto.RecentDir {
	n.mu.Lock()
	last := map[string]time.Time{}
	for _, m := range n.metas {
		if m.cwd != "" && m.mtime.After(last[m.cwd]) {
			last[m.cwd] = m.mtime
		}
	}
	for _, a := range n.agents {
		if _, ok := last[a.Cwd]; a.Cwd != "" && !ok {
			last[a.Cwd] = time.Time{} // running but no transcript yet: listed, oldest
		}
	}
	n.mu.Unlock()
	var out []proto.RecentDir
	for p, t := range last {
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			continue
		}
		out = append(out, proto.RecentDir{Path: p, Name: filepath.Base(p), LastUsed: t.UTC()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastUsed.After(out[j].LastUsed) })
	if len(out) > 15 {
		out = out[:15]
	}
	return out
}

// bgScan follows one transcript for background tasks. The quick meta only reads the tail, which in a long turn full
// of screenshots (base64 lines of a megabyte) holds just the last few items; this reads the whole file once and then
// only what was appended, parsing just the lines that can start or finish a background task.
type bgScan struct {
	mu   sync.Mutex
	off  int64
	meta transcript.Meta
}

var bgMarkers = [][]byte{[]byte("ScheduleWakeup"), []byte("Next wakeup scheduled"), []byte("CronCreate"), []byte("CronDelete"),
	[]byte("run_in_background"), []byte("in background with ID"), []byte("moved to the background"), []byte("Async agent launched"),
	[]byte("task-notification"), []byte(`"name":"Agent"`), []byte(`"name":"Task"`), []byte(`"TaskStop"`), []byte(`"KillShell"`)}

func (n *Node) backgroundTasks(path string) ([]proto.BackgroundTask, *proto.LoopInfo) {
	n.mu.Lock()
	sc := n.bgScans[path]
	if sc == nil {
		sc = &bgScan{}
		n.bgScans[path] = sc
	}
	n.mu.Unlock()
	sc.mu.Lock()
	defer sc.mu.Unlock()
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, nil
	}
	if st.Size() < sc.off { // rewritten
		sc.off, sc.meta = 0, transcript.Meta{}
	}
	if st.Size() > sc.off {
		buf := make([]byte, st.Size()-sc.off)
		nr, _ := f.ReadAt(buf, sc.off)
		buf = buf[:nr]
		end := bytes.LastIndexByte(buf, '\n')
		if end >= 0 {
			for _, line := range bytes.Split(buf[:end], []byte("\n")) {
				if len(line) > 64<<10 || !containsAny(line, bgMarkers) { // api-request blobs repeat everything; skip them
					continue
				}
				transcript.ParseLine(0, line, &sc.meta)
			}
			sc.off += int64(end + 1)
		}
	}
	var out []proto.BackgroundTask
	for _, t := range sc.meta.Background {
		if time.Since(t.StartedAt) < 24*time.Hour { // older ones are surely gone (claude restarted)
			out = append(out, t)
		}
	}
	var loop *proto.LoopInfo
	if l := sc.meta.Loop; l != nil {
		// A wakeup loop whose next tick is long overdue died with its claude; cron jobs live until deleted.
		if l.Kind == "cron" || l.NextAt.IsZero() || time.Since(l.NextAt) < 15*time.Minute {
			c := *l
			loop = &c
		}
	}
	return out, loop
}

func containsAny(b []byte, subs [][]byte) bool {
	for _, s := range subs {
		if bytes.Contains(b, s) {
			return true
		}
	}
	return false
}
