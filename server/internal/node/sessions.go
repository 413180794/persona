package node

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

// stateFile remembers which sessions Persona started and with which launcher. It lives in the hub's Postgres:
// loaded with `state.load` when the node starts and written through with `state.put` events.
type stateFile struct {
	mu       sync.Mutex
	Sessions map[string]sessionState `json:"sessions"`
	persist  func(id string, v sessionState)
}

type sessionState struct {
	Pane             string    `json:"pane,omitempty"`
	Launcher         string    `json:"launcher"`
	Model            string    `json:"model,omitempty"`
	ModelFingerprint string    `json:"model_fingerprint,omitempty"`
	ModelEffort      string    `json:"model_effort,omitempty"`
	Cwd              string    `json:"cwd"`
	StartedAt        time.Time `json:"started_at"`
	// Tasks: the assistant conversation's background tasks and monitors running right now (node/assistant.go).
	Tasks []string `json:"tasks,omitempty"`
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
	bgWait   bool                   // bg and loop are not known yet (a big first read is running)
	cwd      string
	model    string
	lastText string
	wake     bool // the latest turn was a background task waking the agent
	readAt   time.Time
	mode     string
	modeAt   time.Time
	effort   string
	effortAt time.Time
	sandbox  string // Codex: the turn's sandbox, which with mode (its approval policy) gives the app's mode back
	// turnClosed: a Codex rollout whose tail ends with its turn finished (transcript.Meta.TurnClosed)
	turnClosed bool
}

const offlineWindow = 7 * 24 * time.Hour

// metaMinInterval: how stale a growing transcript's title / last text / background tasks may get in the list.
const metaMinInterval = 3 * time.Second

func (n *Node) meta(path string, st os.FileInfo) quickMeta {
	n.mu.Lock()
	m, ok := n.metas[path]
	n.mu.Unlock()
	// A working agent's transcript grows all the time and every list rebuild (each herdr event) would re-read its
	// 384 KB tail and scan the new bytes: a dozen busy agents with large transcripts kept a node reading ~20 MB/s.
	// A growing file's meta is re-read at most every metaMinInterval; the list's rev comes from stat, so it stays exact.
	if ok && (m.size == st.Size() && m.mtime.Equal(st.ModTime()) || time.Since(m.readAt) < metaMinInterval) {
		if m.bgWait { // the background read may be done now
			m.bg, m.loop, m.bgWait = n.backgroundTasks(path)
			n.mu.Lock()
			n.metas[path] = m
			n.mu.Unlock()
		}
		return m
	}
	m = readQuickMeta(path, st)
	m.readAt = time.Now()
	m.bg, m.loop, m.bgWait = n.backgroundTasks(path)
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
	// The first prompt is usually in the first 32 KB. A Codex rollout puts its instructions first (the assistant's
	// AGENTS.md and snapshot: 74 KB in), so up to 256 KB is read while it has not turned up.
	head := make([]byte, 32*1024)
	nr, _ := f.Read(head)
	for {
		for _, line := range bytes.Split(head[:nr], []byte("\n")) {
			if bytes.Contains(line, []byte(`"cwd"`)) || bytes.Contains(line, []byte(`"type":"user"`)) ||
				bytes.Contains(line, []byte(`"type":"UserMessage"`)) { // Codex rollouts: session_meta, then the first prompt
				transcript.ParseLine(0, line, &tm)
				if tm.Cwd != "" && tm.FirstUser != "" {
					break
				}
			}
		}
		if tm.FirstUser != "" || nr < len(head) || len(head) >= 256*1024 {
			break
		}
		head = make([]byte, len(head)*2)
		nr, _ = f.ReadAt(head, 0)
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
			// Skip parsing big tool results; they still count as activity (a turn that ended on one would otherwise
			// look finished earlier, and a look at it in the app in between would count as having seen the end).
			if bytes.Contains(line, []byte(`"type":"user"`)) && len(line) > 8<<10 {
				if ts := lineTime(line); ts.After(tm.LastActivity) {
					tm.LastActivity = ts
				}
				continue
			}
			transcript.ParseLine(0, line, &tm)
		}
	}
	m.title, m.cwd, m.model, m.lastText, m.first, m.activity, m.queue = tm.Title, tm.Cwd, tm.Model, tm.LastText, first, tm.LastActivity, tm.Queue
	m.wake, m.turnClosed = tm.LastTurnWake, tm.TurnClosed
	m.mode, m.modeAt, m.effort, m.effortAt, m.sandbox = tm.PermissionMode, tm.ModeAt, tm.Effort, tm.EffortAt, tm.CodexSandbox
	return m
}

// codexStaleWorking: how long herdr may say a Codex agent works while its rollout says the turn is over. Codex writes
// task_started the moment a turn begins; herdr reads "working" off the screen and can be a moment ahead of it.
const codexStaleWorking = 30 * time.Second

// codexTurnOver reports whether a Codex session herdr calls working has in fact finished: its rollout ends with the
// turn complete and nothing has been written for a while. herdr has said so for hours-old turns (2026-10-30: a turn
// finished two days earlier showed 「正在工作 45 小时」 for eight minutes, the rollout untouched).
func (n *Node) codexTurnOver(id string, now time.Time) bool {
	p := n.transcriptPath(id)
	if p == "" {
		return false
	}
	st, err := os.Stat(p)
	if err != nil {
		return false
	}
	return n.meta(p, st).turnClosed && now.Sub(st.ModTime()) > codexStaleWorking
}

// lineTime reads a transcript line's own "timestamp" without decoding it (Claude Code writes it after the message, so
// the last one; quotes inside the message are escaped and do not match).
func lineTime(line []byte) time.Time {
	key := []byte(`"timestamp":"`)
	i := bytes.LastIndex(line, key)
	if i < 0 {
		return time.Time{}
	}
	rest := line[i+len(key):]
	j := bytes.IndexByte(rest, '"')
	if j < 0 {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, string(rest[:j]))
	return t
}

// startingSession reports whether a session without a transcript is one that exists but has not written its first
// line yet (a headless conversation, or an agent that is live or that Persona started), rather than an unknown id.
func (n *Node) startingSession(id string) bool {
	if n.isAssistantSession(id) {
		return true
	}
	if _, live := n.agent(id); live {
		return true
	}
	if _, known := n.codexSource(id); known {
		return true
	}
	_, known := n.state.get(id)
	return known
}

// transcriptPath finds <config dir>/projects/*/<id>.jsonl. One session can have a file in two config directories
// (resumed under another profile: the new writes go to the new copy, the old one stays): the newest is the one.
// A path a hook reported is Claude's own word; one found here is looked for again after half a minute.
func (n *Node) transcriptPath(id string) string {
	if n.codexBlocked(id) {
		return ""
	}
	if src, ok := n.codexSource(id); ok && !n.ownedAssistantSource(id) {
		if src.path != "" {
			if _, err := os.Stat(src.path); err == nil {
				return src.path
			}
		}
		return rolloutPath(src.home, src.thread)
	}
	if strings.HasPrefix(id, "c0de-") {
		return ""
	}
	n.mu.Lock()
	p := n.paths[id]
	at, guessed := n.pathsAt[id]
	n.mu.Unlock()
	if p != "" && (!guessed || time.Since(at) < 30*time.Second) {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if !validID(id) {
		return ""
	}
	matches := newestFirst(n.globProjects("*", id+".jsonl"))
	if len(matches) == 0 {
		if p := n.codexPath(id); p != "" {
			matches = []string{p}
		} else if p := n.traexPath(id); p != "" {
			matches = []string{p}
		} else {
			return ""
		}
	}
	n.setGuessedPath(id, matches[0])
	return matches[0]
}

func (n *Node) setGuessedPath(id, p string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.paths[id] = p
	if n.pathsAt == nil {
		n.pathsAt = map[string]time.Time{}
	}
	n.pathsAt[id] = time.Now()
}

// newestFirst orders the copies of a transcript by modification time, newest first.
func newestFirst(paths []string) []string {
	if len(paths) < 2 {
		return paths
	}
	mod := map[string]time.Time{}
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil {
			mod[p] = st.ModTime()
		}
	}
	sort.SliceStable(paths, func(i, j int) bool { return mod[paths[i]].After(mod[paths[j]]) })
	return paths
}

var idRe = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

func validID(id string) bool { return idRe.MatchString(id) }

func (n *Node) file(id string) (*transcript.File, error) {
	n.mu.Lock()
	f := n.files[id]
	if f != nil {
		n.used(id)
	}
	n.mu.Unlock()
	if f != nil {
		if old := f.Where(); !fileExists(old) {
			// Moved since it was opened (a Codex thread archived or restored): it was read from a path that is gone.
			if p := n.transcriptPath(id); p != "" && p != old {
				f.Relocate(p)
			}
		}
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
	n.used(id)
	n.evictFiles()
	n.mu.Unlock()
	return f, nil
}

// warmTranscripts parses the live sessions' transcripts once, one at a time, soon after the node starts. Right
// after a (re)deploy the phone pulls every busy session, and parsing a transcript of a few hundred MB on that first
// request took up to 30 s (the hub gave up with a 502).
func (n *Node) warmTranscripts(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second): // the first herdr snapshot is in
	}
	n.mu.Lock()
	ids := make([]string, 0, len(n.agents))
	for id := range n.agents {
		ids = append(ids, id)
	}
	n.mu.Unlock()
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if f, err := n.file(id); err == nil {
			n.refresh(id, f)
		}
	}
}

// maxFiles caps the transcripts kept parsed in memory: every session ever opened used to stay, items and all.
const maxFiles = 16

// used marks a parsed transcript as just asked for. Callers hold n.mu.
func (n *Node) used(id string) {
	if n.fileUse == nil {
		n.fileUse = map[string]uint64{}
	}
	n.useTick++
	n.fileUse[id] = n.useTick
}

// evictFiles drops the least recently used parsed transcripts of offline sessions nobody watches, down to maxFiles.
// Live sessions stay (warmTranscripts parsed them so the phone never waits for a cold parse). Reopening one parses
// the file again; its history stays the same (same file, same first line). Callers hold n.mu.
func (n *Node) evictFiles() {
	for len(n.files) > maxFiles {
		oldest, at := "", uint64(0)
		for id := range n.files {
			if _, watched := n.watches[id]; watched {
				continue
			}
			if _, live := n.agents[id]; live {
				continue
			}
			if t := n.fileUse[id]; oldest == "" || t < at {
				oldest, at = id, t
			}
		}
		if oldest == "" {
			return // all watched
		}
		delete(n.files, oldest)
		delete(n.fileUse, oldest)
	}
}

// sessionList returns live herdr sessions plus recent offline transcripts.
func (n *Node) sessionList(ctx context.Context, refreshAgents bool) []proto.Session {
	if refreshAgents {
		_ = n.refreshHosts(ctx)
	}
	n.mu.Lock()
	agents := make(map[string]host.Agent, len(n.agents))
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
		switch a.Agent {
		case "codex":
			s.Title = n.codexTitle(id)                        // the terminal title is "dots | codex | Ready"
			s.CodexRoute, s.CodexRouteNote = n.codexRoute(id) // codex_daemon.go
		case traexAgent:
			s.Title = n.traexTitle(id) // the thread's name; the terminal title adds " | <directory>"
			s.CodexRoute, s.CodexRouteNote = codexRouteTerminal, "TRAEX 没有共享的 app-server"
		}
		n.placeIn(&s, a)
		s.Ports = n.portsOf(a.PaneID)
		if a.Agent == "codex" && s.Status == proto.StatusWorking && n.codexTurnOver(id, time.Now()) {
			s.Status = proto.StatusDone
		}
		n.mu.Lock()
		if n.driving[id] && s.Status == proto.StatusBlocked {
			s.Status = proto.StatusIdle
		}
		n.mu.Unlock()
		s.Suggestion = n.noteStatus(id, a.Agent, a.PaneID, s.Status) // suggestion.go
		if p := n.transcriptPath(id); p != "" {
			if st, err := os.Stat(p); err == nil {
				m := n.meta(p, st)
				if m.title != "" && a.Agent != "codex" && a.Agent != traexAgent {
					s.Title = m.title
				}
				if s.Title == "" || s.Title == "Claude Code" {
					s.Title = ""
					untitled(&s, m)
				}
				s.Model, s.LastText, s.UpdatedAt, s.Rev = m.model, snippet(m.lastText), lastUsed(m, st), st.Size()
				if a.Agent != "codex" && a.Agent != traexAgent {
					s.PermissionMode, s.Effort = n.currentModes(id, m)
				}
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
		if a.Agent == piAgent {
			n.mergePiRow(&s) // its conversation is the extension's (pi.go)
		}
		if n.codexBlocked(id) {
			markCodexConflict(&s)
		}
		n.decorate(&s)
		out = append(out, s)
		seen[id] = true
	}

	for _, s := range n.starts.listed(seen) { // starting.go
		n.decorate(&s)
		out = append(out, s)
		seen[s.SessionID] = true
	}
	for _, s := range n.runtimes.sessions() { // runtimes (runtimes.go): listed as they say
		if !seen[s.SessionID] {
			if n.runtimeAssistant(s.SessionID) { // the assistant keeps its mode (assistant_runtime.go)
				s.PermissionMode = n.assistantModeOf(s.SessionID)
			}
			out = append(out, s)
			seen[s.SessionID] = true
		}
	}
	for _, s := range n.assistantSessions() {
		if !seen[s.SessionID] {
			out = append(out, s)
			seen[s.SessionID] = true
		}
	}
	for _, s := range n.codexSessions(seen) {
		out = append(out, s)
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

// setKept replaces the sessions that are always listed; the next list has them.
func (n *Node) setKept(ids []string) {
	m := map[string]bool{}
	for _, id := range ids {
		if validID(id) {
			m[id] = true
		}
	}
	n.mu.Lock()
	n.kept = m
	n.mu.Unlock()
	offlineCache.Lock()
	offlineCache.at = time.Time{}
	offlineCache.Unlock()
}

func (n *Node) keptSessions() map[string]bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.kept
}

func (n *Node) scanOffline() []proto.Session {
	var out []proto.Session
	files := n.globProjects("*", "*.jsonl")
	type cand struct {
		path string
		st   os.FileInfo
	}
	var cands, kept []cand
	keep := n.keptSessions()
	for _, p := range files {
		id := strings.TrimSuffix(filepath.Base(p), ".jsonl")
		if !validID(id) {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if keep[id] { // the main conversation: listed however long it has been quiet and however busy the machine
			kept = append(kept, cand{p, st})
			continue
		}
		if time.Since(st.ModTime()) > offlineWindow || st.Size() < 2048 {
			continue
		}
		cands = append(cands, cand{p, st})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].st.ModTime().After(cands[j].st.ModTime()) })
	if len(cands) > 80 {
		cands = cands[:80]
	}
	cands = append(cands, kept...)
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].st.ModTime().After(cands[j].st.ModTime()) })
	listed := map[string]bool{}
	for _, c := range cands {
		id := strings.TrimSuffix(filepath.Base(c.path), ".jsonl")
		if listed[id] {
			continue // an older copy in another config directory (newest first): it used to win, and list the session twice
		}
		listed[id] = true
		m := n.meta(c.path, c.st)
		if m.lastText == "" && m.title == "" {
			continue // sessions that never produced output (e.g. hook probes)
		}
		n.setGuessedPath(id, c.path)
		s := proto.Session{
			SessionID: id, Title: m.title, Cwd: m.cwd, Status: proto.StatusOffline, Model: m.model,
			LastText: snippet(m.lastText), UpdatedAt: lastUsed(m, c.st), Rev: c.st.Size(),
		}
		s.PermissionMode, s.Effort = n.currentModes(id, m)
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
	if s.Launcher == "" {
		s.Launcher = n.inferLauncher(s.SessionID)
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

// errEngineChanged: the engine was switched while a refresh ran; its result is the old engine's and is dropped.
var errEngineChanged = errors.New("the engine changed during the refresh")

// errNoHost: the node has no terminal host to look at.
var errNoHost = errors.New("no terminal host")

// layout remembers a host's space and slot names from its last snapshot.
type layout struct {
	spaces map[string]host.Space
	slots  map[string]host.Slot
}

// refreshHosts takes one snapshot of every host, or of only the hosts given: agents plus the space / slot names shown
// as space / slot. The hosts are asked concurrently, each bounded by its own client's timeouts, so a slow host never
// holds up the other host of a combined engine. It fails only when no host answered; a host that did not answer keeps
// the agents it gave last. A host that is not running (host.ErrNotRunning: herdr-core without its socket) answered: it
// has no agents.
func (n *Node) refreshHosts(ctx context.Context, only ...host.Host) error {
	n.mu.Lock()
	epoch := n.engEpoch // a refresh that outlives an engine switch must not bring the old engine's agents back
	n.mu.Unlock()
	all := n.hosts.All()
	if len(all) == 0 {
		return errNoHost
	}
	targets := only
	if len(targets) == 0 {
		targets = all
	}
	type outcome struct {
		kind   string
		agents []host.Agent
		full   bool
		err    error
	}
	outs := make([]outcome, len(targets))
	var wg sync.WaitGroup
	for i, h := range targets {
		wg.Add(1)
		go func(i int, h host.Host) {
			defer wg.Done()
			agents, full, err := n.lookAt(ctx, h, epoch)
			outs[i] = outcome{kind: h.Kind(), agents: agents, full: full, err: err}
		}(i, h)
	}
	wg.Wait()

	n.mu.Lock()
	if n.engEpoch != epoch {
		n.mu.Unlock()
		return errEngineChanged
	}
	if n.hostAgents == nil {
		n.hostAgents = map[string][]host.Agent{}
	}
	answered, snapped, firstErr := false, false, error(nil)
	for _, o := range outs {
		if o.err != nil && errors.Is(o.err, host.ErrNotRunning) {
			o.err, o.agents = nil, nil // an answer: nothing runs there
		}
		if o.err != nil {
			if firstErr == nil {
				firstErr = o.err
			}
			o.agents = n.hostAgents[o.kind]
		} else {
			answered, snapped = true, snapped || o.full
			n.hostAgents[o.kind] = o.agents
		}
	}
	union := make([]host.Agent, 0)
	for _, agents := range n.hostAgents { // hosts not in this partial refresh keep what they gave last
		union = append(union, agents...)
	}
	n.mu.Unlock()

	if !answered {
		return firstErr
	}
	if !n.setAgentsAt(union, epoch) {
		return errEngineChanged
	}
	if snapped {
		n.panesChanged()
	}
	return nil
}

// lookAt is one host's agents: from a snapshot (full), which also gives the space / slot names; a host that cannot
// snapshot (older herdr without `api snapshot`) gives its agent list instead.
func (n *Node) lookAt(ctx context.Context, h host.Host, epoch uint64) (agents []host.Agent, full bool, err error) {
	snap, err := h.Snapshot(ctx)
	if err != nil {
		agents, aerr := h.Agents(ctx)
		if aerr != nil {
			return nil, false, err
		}
		return agents, false, nil
	}
	l := layout{spaces: map[string]host.Space{}, slots: map[string]host.Slot{}}
	for _, w := range snap.Spaces {
		l.spaces[w.ID] = w
	}
	for _, t := range snap.Slots {
		l.slots[t.ID] = t
	}
	n.mu.Lock()
	if n.engEpoch != epoch {
		n.mu.Unlock()
		return nil, false, errEngineChanged
	}
	if n.layouts == nil {
		n.layouts = map[string]layout{}
	}
	n.layouts[h.Kind()] = l
	n.mu.Unlock()
	// Panes running an agent the host does not detect (TRAEX) become agents of our own.
	return append(snap.Agents, n.traexAgents(ctx, snap.Panes, snap.Agents)...), true, nil
}

// placeIn fills the space / slot fields of a live session.
func (n *Node) placeIn(s *proto.Session, a host.Agent) {
	kind := n.hosts.Ref(a.PaneID).Kind
	n.mu.Lock()
	l := n.layouts[kind]
	w, wok := l.spaces[a.WorkspaceID]
	t, tok := l.slots[a.TabID]
	n.mu.Unlock()
	s.SpaceKind, s.SpaceID, s.SlotID = kind, a.WorkspaceID, a.TabID
	if wok {
		s.SpaceName, s.SpaceIndex = w.Label, w.Number
	}
	if tok {
		s.SlotName, s.SlotIndex = t.Label, t.Number
	}
	if a.Tokens != nil && a.Tokens["persona_from"] != "" {
		s.SpawnFrom = a.Tokens["persona_from"]
	}
}

// setAgents maps live claude agents to session ids: the host's own report when it knows (herdr's claude integration),
// else what our hooks told us about the pane, else the newest transcript in the agent's project directory.
func (n *Node) setAgents(agents []host.Agent) {
	n.mu.Lock()
	epoch := n.engEpoch
	n.mu.Unlock()
	n.setAgentsAt(agents, epoch)
}

// setAgentsAt is setAgents for a refresh of engine epoch: false (nothing set) when the engine changed since.
func (n *Node) setAgentsAt(agents []host.Agent, epoch uint64) bool {
	m := map[string]host.Agent{}
	var unknown, codex, pi []host.Agent
	n.mu.Lock()
	for _, a := range agents {
		if a.Agent == "codex" {
			codex = append(codex, a)
			continue
		}
		if a.Agent == piAgent { // its session: herdr's pi integration, or the extension in it (pi.go)
			pi = append(pi, a)
			continue
		}
		if a.Agent == traexAgent { // found by its process, which names its session (traex.go)
			if id := a.SessionID(); id != "" {
				m[id] = a
			}
			continue
		}
		if a.Agent != "claude" {
			continue
		}
		if id := a.SessionID(); id != "" {
			m[id] = a
			continue
		}
		unknown = append(unknown, a)
	}
	n.mu.Unlock()
	for _, a := range pi {
		if id := n.piSession(a); id != "" {
			if _, taken := m[id]; !taken {
				m[id] = a
			}
		}
	}
	for _, a := range unknown {
		// Without herdr's claude integration the pane's claude process says which session it runs. A pane binding
		// (our start, a hook) comes after it: the bindings of sessions Persona started are kept in the hub's
		// database and come back when node restarts, long after another claude took the pane (2026-10-31: after a
		// redeploy two panes showed sessions they had run days earlier). A guess (title, then newest transcript in
		// the directory) is the last resort — six agents in one repo got each other's tab names that way.
		id := n.sessionFromProcess(a.PaneID)
		if id == "" {
			n.mu.Lock()
			id = n.panes[a.PaneID]
			n.mu.Unlock()
		}
		if _, taken := m[id]; id == "" || taken {
			id = n.guessSession(a, m)
		}
		if id != "" {
			m[id] = a
		}
	}
	// Codex panes: the thread named in the title (what the pane shows now: a /resume in Codex changes it), else what
	// herdr's codex integration or our own start said; only then, after every pane that could be told has its
	// thread, the newest rollout in the directory nobody else has.
	var unnamed []host.Agent
	for _, a := range codex {
		home := n.codexPaneHome(a.PaneID)
		id := a.SessionID()
		if id != "" {
			id = mainThread(home, id) // a subagent's id stands for its conversation
		}
		if id == "" {
			n.mu.Lock()
			id = n.panes[a.PaneID]
			n.mu.Unlock()
		}
		if named := n.codexByTitle(a, id, m); named != "" {
			id = named
		}
		if id != "" {
			id = n.codexPublicID(home, id)
		}
		if _, taken := m[id]; id == "" || taken {
			unnamed = append(unnamed, a)
			continue
		}
		m[id] = a
	}
	for _, a := range unnamed {
		n.mu.Lock()
		starting := n.codexStarting[a.PaneID]
		n.mu.Unlock()
		if starting {
			continue // a start of ours: its thread is not on disk yet, and an older one in the directory is not it
		}
		if id := n.codexSession(a, m); id != "" {
			m[id] = a
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.engEpoch != epoch {
		return false
	}
	n.agents = m
	if n.lastPanes == nil {
		n.lastPanes = map[string]string{}
	}
	for id, a := range m {
		n.lastPanes[id] = a.PaneID
	}
	return true
}

// sessionFromProcess reads the session a pane's claude runs from ~/.claude/sessions/<pid>.json, which Claude Code
// keeps current (a /clear moves it to a new session id). The pane's pid is cached for a minute; not finding it only
// for a few seconds: right after node starts the claudes' config directories are not all known yet, and a minute of
// "no pid file" let a stale pane binding show another session after every redeploy (2026-10-31).
func (n *Node) sessionFromProcess(pane string) string {
	n.mu.Lock()
	c, ok := n.panePIDs[pane]
	n.mu.Unlock()
	if !ok || time.Since(c.checked) > time.Minute || (c.pid == 0 && time.Since(c.checked) > panePIDRetry) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		pids, err := n.paneProcesses(ctx, pane)
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
	f := n.claudeSessionFile(c.pid)
	if _, err := os.Stat(f); err != nil {
		n.mu.Lock()
		delete(n.panePIDs, pane) // claude exited / restarted: look again next time
		n.mu.Unlock()
		return ""
	}
	return readClaudeSessionID(f)
}

// readClaudeSessionID reads the session id out of a claude's sessions/<pid>.json ("" when missing or invalid).
func readClaudeSessionID(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
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

// panePIDRetry: how soon a pane whose claude had no sessions/<pid>.json is looked at again.
const panePIDRetry = 5 * time.Second

type panePID struct {
	pid     int
	checked time.Time
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
func (n *Node) guessSession(a host.Agent, taken map[string]host.Agent) string {
	if a.Cwd == "" {
		return ""
	}
	files := n.globProjects(nonAlnum.ReplaceAllString(a.Cwd, "-"), "*.jsonl")
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

func (n *Node) agent(id string) (host.Agent, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	a, ok := n.agents[id]
	return a, ok
}

// pollAgents refreshes the hosts' state every second (only the ones due) and pushes sessions.changed when anything
// visible changes. Each watcher host is snapshotted on its own stream's changes and at most every slowSnapshot while
// its stream is live, independently of the other hosts (the combined engine): a herdr-core event never snapshots
// herdr and vice versa.
func (n *Node) pollAgents(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	last := map[string]time.Time{} // host kind → its last snapshot
	var listed time.Time
	for {
		sigs := n.signalSnapshot()
		kicked, ok := nextChange(ctx, t.C, sigs, listed)
		if !ok {
			return
		}
		var due []host.Host
		for _, h := range n.hosts.All() {
			if n.hostDue(h.Kind(), last[h.Kind()], kicked[h.Kind()]) {
				due = append(due, h)
			}
		}
		// The transcripts (revs) are checked every tick regardless; a host that does not answer (not installed or not
		// running) does not stop the list: runtimes, the assistant and the transcripts do not need it. Its agents stay
		// as last seen (refreshHosts).
		if len(due) > 0 {
			_ = n.refreshHosts(ctx, due...)
			now := time.Now()
			for _, h := range due {
				last[h.Kind()] = now
			}
		}
		list := n.sessionList(ctx, false)
		listed = time.Now()
		if revs := n.publish(list); len(revs) > 0 {
			n.kickPorts() // a tool call ran: it may have started (or stopped) a server (ports.go)
		}
	}
}

// publish pushes a rebuilt list to the hub: the whole list when anything it shows changed, else only the revs that
// moved (a transcript grew: a few bytes instead of the list, so the app can pull those sessions). It returns the
// revs that moved. Runtimes (runtimes.go) publish through it too when their list changes.
func (n *Node) publish(list []proto.Session) map[string]int64 {
	key := listKey(list)
	n.mu.Lock()
	changed := key != n.lastList
	n.lastList = key
	revs := n.revsChanged(list)
	n.mu.Unlock()
	if n.peer == nil {
		return revs
	}
	if changed {
		_ = n.peer.Notify("sessions.changed", map[string]any{"sessions": list})
	} else if len(revs) > 0 {
		_ = n.peer.Notify("sessions.revs", map[string]any{"revs": revs})
	}
	return revs
}

// revsChanged records the list's revs and returns those that differ from the last pushed. Callers hold n.mu.
func (n *Node) revsChanged(list []proto.Session) map[string]int64 {
	var out map[string]int64
	next := make(map[string]int64, len(list))
	for _, s := range list {
		next[s.SessionID] = s.Rev
		if s.Rev != 0 && n.lastRevs[s.SessionID] != s.Rev {
			if out == nil {
				out = map[string]int64{}
			}
			out[s.SessionID] = s.Rev
		}
	}
	n.lastRevs = next
	return out
}

// listKey ignores fields that churn without meaning (updated_at of live sessions).
func listKey(list []proto.Session) string {
	var b strings.Builder
	for _, s := range list {
		fmt.Fprintf(&b, "%s|%s|%s|%s|%s|%s|%s|%d|%q|%s|%s\n", s.SessionID, s.Status, s.Title, s.PaneID, s.SpaceName, s.SlotName, s.LastText, s.UpdatedAt.Unix()/10, s.Queued, s.Suggestion, s.SpawnFrom)
		fmt.Fprintf(&b, "source:%s/%s/%s/%s/%v/%v/%s/%s\n", s.SourceKind, s.SourceThreadID, s.SourceInstanceID, s.ReadOnlyReason, s.Capabilities, s.Attached, s.CodexRoute, s.CodexRouteNote)
		for _, t := range s.Background {
			b.WriteString(t.ID + ",")
		}
		if s.Loop != nil {
			fmt.Fprintf(&b, "loop%d", s.Loop.NextAt.Unix())
		}
		for _, a := range s.Subagents {
			fmt.Fprintf(&b, "%s:%v:%d,", a.ID, a.Running, a.UpdatedAt.Unix()/10)
		}
		for _, p := range s.Ports {
			fmt.Fprintf(&b, "port%d/%d/%s,", p.Port, p.PID, p.Title)
		}
	}
	return b.String()
}

var launcherCache struct {
	sync.Mutex
	at       time.Time
	list     []proto.Launcher
	fetch    sync.Mutex // one login shell at a time
	fetching bool       // a background refresh (cachedLaunchers) is running
	// codex: how each Codex launcher runs (codex_start.go), its words as the shell would run them.
	codex map[string][]string
	// settings: the --settings a claude alias brings itself, as its words give it (launcher_settings.go).
	settings map[string]string
}

// launcherScript lists the login shell's launchers in one go (sourcing ~/.zshrc takes a while): what the
// `_claude_launchers` helper prints when ~/.zshrc has one, a GS byte, then every alias as its name and its words
// as zsh itself splits and unquotes them (name US word US … RS), so quoting never has to be parsed here; then, each
// after a GS byte, where codex and traex are on the PATH.
const launcherScript = `_claude_launchers 2>/dev/null || true
printf '\035'
if [ -n "$ZSH_VERSION" ]; then
  for __k __v in "${(@kv)aliases}"; do
    print -rn -- "$__k"$'\x1f'
    for __w in "${(@Q)${(z)__v}}"; do print -rn -- "$__w"$'\x1f'; done
    print -rn -- $'\x1e'
  done
fi
printf '\035'
if [ -n "$ZSH_VERSION" ]; then whence -p codex 2>/dev/null; else command -v codex 2>/dev/null; fi
printf '\035'
if [ -n "$ZSH_VERSION" ]; then whence -p traex 2>/dev/null; else command -v traex 2>/dev/null; fi`

// launchers lists the launchers of this machine's login shell: the `_claude_launchers` helper's (claude wrapper
// functions), then every alias that runs claude (`alias dmcc="ANTHROPIC_BASE_URL=… claude …"`), by name. A name
// the helper lists is not repeated; none at all: plain claude.
func (n *Node) launchers(ctx context.Context) ([]proto.Launcher, error) {
	launcherCache.fetch.Lock()
	defer launcherCache.fetch.Unlock()
	launcherCache.Lock()
	if time.Since(launcherCache.at) < time.Minute && launcherCache.list != nil {
		list := launcherCache.list
		launcherCache.Unlock()
		return list, nil
	}
	launcherCache.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := loginShell(ctx, launcherScript).Output()
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("list launchers: %w", err)
	}
	list, codex, settings := mergeLaunchers(string(out))
	launcherCache.Lock()
	launcherCache.at, launcherCache.list, launcherCache.codex, launcherCache.settings = time.Now(), list, codex, settings
	launcherCache.Unlock()
	return list, nil
}

// mergeLaunchers is launcherScript's output as the launcher list: the helper's, then the aliases it does not name
// (nothing at all: plain claude), then Codex's — plain codex when it is on the PATH, and the aliases that run it (how
// each runs: codex) — then plain traex when it is on the PATH (started without Claude's flags, start_agent.go).
// settings: the claude aliases' own --settings, by name (launcher_settings.go); an alias is what the shell runs for
// its name even when the helper lists the name too.
func mergeLaunchers(out string) (list []proto.Launcher, codex map[string][]string, settings map[string]string) {
	helper, rest, _ := strings.Cut(out, "\x1d")
	aliases, rest, _ := strings.Cut(rest, "\x1d")
	codexPath, traexPath, _ := strings.Cut(rest, "\x1d")
	list = parseLaunchers(helper)
	seen := map[string]bool{}
	for _, l := range list {
		seen[l.Name] = true
	}
	claudeAliases, settings := parseAliasLaunchers(aliases)
	for _, l := range claudeAliases {
		if !seen[l.Name] {
			list = append(list, l)
		}
	}
	if len(list) == 0 {
		list = []proto.Launcher{{Name: "claude", Model: "默认"}}
	}
	codex = map[string][]string{}
	ls, words := parseCodexAliases(aliases)
	if strings.TrimSpace(codexPath) != "" && words["codex"] == nil {
		list = append(list, proto.Launcher{Name: "codex", Model: "默认", Agent: "codex", Source: "zshrc"})
		codex["codex"] = []string{"codex"}
	}
	for _, l := range ls {
		if !seen[l.Name] {
			list = append(list, l)
			codex[l.Name] = words[l.Name]
		}
	}
	if strings.TrimSpace(traexPath) != "" && !seen[traexAgent] && codex[traexAgent] == nil {
		list = append(list, proto.Launcher{Name: traexAgent, Model: "默认", Agent: traexAgent, Source: "zshrc"})
	}
	return list, codex, settings
}

// cachedLaunchers never waits for the login shell: it returns the last list (possibly stale, nil before the first
// one) and starts a refresh in the background when the list is missing or old.
func (n *Node) cachedLaunchers() []proto.Launcher {
	launcherCache.Lock()
	defer launcherCache.Unlock()
	if (launcherCache.list == nil || time.Since(launcherCache.at) >= time.Minute) && !launcherCache.fetching {
		launcherCache.fetching = true
		go func() {
			_, _ = n.launchers(context.Background())
			launcherCache.Lock()
			launcherCache.fetching = false
			launcherCache.Unlock()
		}()
	}
	return launcherCache.list
}

// inferLauncher names the launcher of a claude Persona did not start (typed in a terminal), from the
// ANTHROPIC_BASE_URL / ANTHROPIC_MODEL the scan saw in its environment; "" when unknown.
func (n *Node) inferLauncher(id string) string {
	env, ok := n.sessionEnv(id)
	if !ok {
		return ""
	}
	return matchLauncher(n.cachedLaunchers(), env.BaseURL, env.Model)
}

// matchLauncher picks the launcher that exports this base URL and model. Several launchers can share one endpoint
// (and a launcher whose model is chosen at run time shows a placeholder such as "(claude-choose)"), so: the one
// launcher with both equal; else the one launcher with that base URL; else nothing, never a guess between several.
func matchLauncher(list []proto.Launcher, baseURL, model string) string {
	if baseURL == "" {
		return "" // the default endpoint: nothing to tell launchers apart by
	}
	var byURL, byBoth []string
	for _, l := range list {
		if l.BaseURL != baseURL {
			continue
		}
		byURL = append(byURL, l.Name)
		if l.Model == model {
			byBoth = append(byBoth, l.Name)
		}
	}
	if len(byBoth) == 1 {
		return byBoth[0]
	}
	if len(byURL) == 1 {
		return byURL[0]
	}
	return ""
}

var nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

var assignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// parseAliasLaunchers reads launcherScript's alias records and keeps the aliases that start claude: after the
// variables set in front of it (`A=1 B=2 claude …`), through env / command / exec / noglob, by path or not, with
// `export A=1;` before it in the same alias. Their gateway and model come from ANTHROPIC_BASE_URL and ANTHROPIC_MODEL
// (or --model). Codex's are parseCodexAliases'. Sorted by name: the shell keeps no order. settings: the ones that
// bring their own --settings, by name (launcher_settings.go).
func parseAliasLaunchers(out string) (list []proto.Launcher, settings map[string]string) {
	settings = map[string]string{}
	for _, rec := range strings.Split(out, "\x1e") {
		words := strings.Split(rec, "\x1f")
		if len(words) < 2 {
			continue
		}
		name := words[0]
		if !nameRe.MatchString(name) || name == "claude" {
			continue
		}
		if l, s, ok := aliasLauncher(words[1 : len(words)-1]); ok {
			l.Name = name
			list = append(list, l)
			if s != "" {
				settings[name] = s
			}
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, settings
}

// parseCodexAliases keeps the aliases that run Codex: `export` statements and one codex command (through env /
// command / exec / noglob, with variables set in front). How each runs is its words for codex-start: the variables,
// then the command and its arguments. Its model is -m / --model or -c model=…; an alias that does more (`cd x && codex`)
// is left out.
func parseCodexAliases(out string) ([]proto.Launcher, map[string][]string) {
	var list []proto.Launcher
	words := map[string][]string{}
	for _, rec := range strings.Split(out, "\x1e") {
		w := strings.Split(rec, "\x1f")
		if len(w) < 2 || !nameRe.MatchString(w[0]) {
			continue
		}
		argv, model, ok := codexAliasArgv(w[1 : len(w)-1])
		if !ok {
			continue
		}
		if model == "" {
			model = "默认"
		}
		list = append(list, proto.Launcher{Name: w[0], Model: model, Agent: "codex", Source: "zshrc"})
		words[w[0]] = argv
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, words
}

// codexAliasArgv reads an alias's words as one codex command; ok false when it is anything else.
func codexAliasArgv(words []string) (argv []string, model string, ok bool) {
	var exported []string
	found := false
	for len(words) > 0 {
		end := len(words)
		for i, w := range words {
			if w == ";" || w == "&&" || w == "||" || w == "|" || w == "&" {
				end = i
				break
			}
		}
		cmd := words[:end]
		if end < len(words) {
			words = words[end+1:]
		} else {
			words = nil
		}
		if len(cmd) == 0 {
			continue
		}
		if cmd[0] == "export" {
			for _, w := range cmd[1:] {
				if assignRe.MatchString(w) {
					exported = append(exported, w)
				}
			}
			continue
		}
		if found {
			return nil, "", false // more than one command
		}
		env := map[string]string{}
		i := commandIndex(cmd, env)
		if i < 0 || filepath.Base(cmd[i]) != "codex" {
			return nil, "", false
		}
		found = true
		argv = append([]string(nil), exported...)
		for _, w := range cmd[:i] {
			if assignRe.MatchString(w) {
				argv = append(argv, w)
			}
		}
		argv = append(argv, cmd[i:]...)
		for j := i + 1; j < len(cmd); j++ {
			switch {
			case (cmd[j] == "-m" || cmd[j] == "--model") && j+1 < len(cmd):
				model = cmd[j+1]
			case strings.HasPrefix(cmd[j], "--model="):
				model = strings.TrimPrefix(cmd[j], "--model=")
			case (cmd[j] == "-c" || cmd[j] == "--config") && j+1 < len(cmd) && strings.HasPrefix(cmd[j+1], "model="):
				model = strings.Trim(strings.TrimPrefix(cmd[j+1], "model="), `"'`)
			}
		}
	}
	return argv, model, found
}

// commandIndex is where the command of one simple command starts, past the variables set for it (put in env) and
// the wrappers that run what follows (env, with its -u / -i, command, exec, noglob, nocorrect); -1: there is none.
func commandIndex(cmd []string, env map[string]string) int {
	for i := 0; i < len(cmd); i++ {
		w := cmd[i]
		switch {
		case assignRe.MatchString(w):
			k, v, _ := strings.Cut(w, "=")
			env[k] = v
		case w == "env" || w == "command" || w == "exec" || w == "noglob" || w == "nocorrect":
		case w == "-u" && i > 0 && i+1 < len(cmd): // env -u NAME
			delete(env, cmd[i+1])
			i++
		case strings.HasPrefix(w, "-") && i > 0 && (cmd[i-1] == "env" || cmd[i-1] == "-i"): // env -i
		default:
			return i
		}
	}
	return -1
}

// aliasLauncher finds the claude command in an alias's words and what it is started with; settings is its own
// --settings (the last one, as Claude Code takes it), "" for none: launcher_settings.go.
func aliasLauncher(words []string) (l proto.Launcher, settings string, ok bool) {
	exported := map[string]string{}
	for len(words) > 0 {
		end := len(words)
		for i, w := range words {
			if w == ";" || w == "&&" || w == "||" || w == "|" || w == "&" {
				end = i
				break
			}
		}
		cmd := words[:end]
		if end < len(words) {
			words = words[end+1:]
		} else {
			words = nil
		}
		if len(cmd) > 0 && cmd[0] == "export" {
			for _, w := range cmd[1:] {
				if k, v, ok := strings.Cut(w, "="); ok && assignRe.MatchString(w) {
					exported[k] = v
				}
			}
			continue
		}
		env := map[string]string{}
		for k, v := range exported {
			env[k] = v
		}
		i := commandIndex(cmd, env)
		if i < 0 {
			continue
		}
		if filepath.Base(cmd[i]) != "claude" {
			continue
		}
		l = proto.Launcher{BaseURL: env["ANTHROPIC_BASE_URL"], Model: env["ANTHROPIC_MODEL"], Source: "zshrc"}
		for j := i + 1; j < len(cmd); j++ {
			if cmd[j] == "--model" && j+1 < len(cmd) && l.Model == "" {
				l.Model = cmd[j+1]
			} else if m, ok := strings.CutPrefix(cmd[j], "--model="); ok && l.Model == "" {
				l.Model = m
			}
			if cmd[j] == "--settings" && j+1 < len(cmd) {
				settings = cmd[j+1]
			} else if s, ok := strings.CutPrefix(cmd[j], "--settings="); ok {
				settings = s
			}
		}
		return l, settings, true
	}
	return proto.Launcher{}, "", false
}

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
	mu      sync.Mutex
	off     int64
	meta    transcript.Meta
	pending bool // the first read of a big transcript is running in the background
}

// bgSyncLimit: up to this much unread transcript is scanned for background tasks right away. More than that (the
// first look at a big transcript, every busy one after a node restart) is read in the background, one file at a
// time: building the session list waited on reading gigabytes, and a machine showed no sessions for minutes.
var bgSyncLimit int64 = 4 << 20

// bgSlot lets one big first read run at a time.
var bgSlot = make(chan struct{}, 1)

var bgMarkers = [][]byte{[]byte("ScheduleWakeup"), []byte("Next wakeup scheduled"), []byte("CronCreate"), []byte("CronDelete"),
	[]byte("run_in_background"), []byte("in background with ID"), []byte("moved to the background"), []byte("Async agent launched"),
	[]byte("task-notification"), []byte(`"name":"Agent"`), []byte(`"name":"Task"`), []byte(`"TaskStop"`), []byte(`"KillShell"`)}

func (n *Node) backgroundTasks(path string) (tasks []proto.BackgroundTask, loop *proto.LoopInfo, pending bool) {
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
		return nil, nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, nil, false
	}
	if st.Size() < sc.off { // rewritten
		sc.off, sc.meta = 0, transcript.Meta{}
	}
	if sc.pending {
		return nil, nil, true
	}
	if st.Size()-sc.off > bgSyncLimit {
		sc.pending = true
		go n.scanBackgroundLater(path, sc, st.Size())
		return nil, nil, true
	}
	scanBackground(f, &sc.off, &sc.meta, st.Size())
	for _, t := range sc.meta.Background {
		if time.Since(t.StartedAt) < 24*time.Hour { // older ones are surely gone (claude restarted)
			tasks = append(tasks, t)
		}
	}
	if l := sc.meta.Loop; l != nil {
		// A wakeup loop whose next tick is long overdue died with its claude; cron jobs live until deleted.
		if l.Kind == "cron" || l.NextAt.IsZero() || time.Since(l.NextAt) < 15*time.Minute {
			c := *l
			loop = &c
		}
	}
	return tasks, loop, false
}

// scanBackground reads the marker lines between *off and size into m, streamed (the first look at a 300 MB
// transcript once held all of it in memory). A last line not complete yet is read next time.
func scanBackground(f *os.File, off *int64, m *transcript.Meta, size int64) {
	r := bufio.NewReaderSize(io.NewSectionReader(f, *off, size-*off), 256<<10)
	at, long := *off, false
	for {
		line, err := r.ReadSlice('\n')
		at += int64(len(line))
		if err == bufio.ErrBufferFull {
			long = true
			continue
		}
		if err != nil {
			return
		}
		// api-request blobs repeat everything; skip them
		if !long && len(line) <= 64<<10 && containsAny(line, bgMarkers) {
			transcript.ParseLine(0, bytes.TrimSuffix(line, []byte("\n")), m)
		}
		long = false
		*off = at
	}
}

// scanBackgroundLater does a big first read off the list's path, then hands the result to the next caller.
func (n *Node) scanBackgroundLater(path string, sc *bgScan, size int64) {
	bgSlot <- struct{}{}
	defer func() { <-bgSlot }()
	var off int64
	var m transcript.Meta
	if f, err := os.Open(path); err == nil {
		scanBackground(f, &off, &m, size)
		f.Close()
	}
	sc.mu.Lock()
	sc.off, sc.meta, sc.pending = off, m, false
	sc.mu.Unlock()
}

func containsAny(b []byte, subs [][]byte) bool {
	for _, s := range subs {
		if bytes.Contains(b, s) {
			return true
		}
	}
	return false
}
