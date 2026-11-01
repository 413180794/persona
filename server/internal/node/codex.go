package node

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/host"
)

// Codex support: herdr reports Codex panes as agent "codex"; their conversations are rollouts under
// ~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<session id>.jsonl (parsed by transcript/codex.go). TRAEX, a Codex
// fork, keeps the same layout under ~/.trae/cli (traex.go). herdr's codex
// integration reports the session id when Codex starts; a Codex started before that integration has none. Codex puts
// the thread's name in the terminal title ("<name> | <directory>"), which says which thread the pane shows now; the
// newest unclaimed rollout in the pane's directory is only the last resort (2026-10-31: five Codex panes in one
// repository, four of them showed another pane's thread, and the one at work looked idle).

const codexWindow = 14 * 24 * time.Hour

type codexRollout struct {
	id    string
	path  string
	cwd   string
	mtime time.Time
}

// rolloutCache is one rollout home's scan (Codex: ~/.codex, TRAEX: ~/.trae/cli — the same layout).
type rolloutCache struct {
	at       time.Time
	rollouts []codexRollout
	older    []string // rollouts in day directories before the window, listed once a minute
	olderAt  time.Time
	cwds     map[string]string // path → cwd (never changes once written)
	titles   map[string]string
	titlesAt time.Time
}

var rolloutCaches = struct {
	sync.Mutex
	byHome map[string]*rolloutCache
}{byHome: map[string]*rolloutCache{}}

var rolloutIDRe = regexp.MustCompile(`([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\.jsonl$`)

func (n *Node) codexHome() string { return filepath.Join(n.home, ".codex") }

func (n *Node) codexRollouts() []codexRollout { return rollouts(n.codexHome()) }
func (n *Node) codexPath(id string) string { if s,ok:=n.codexSource(id);ok{return rolloutPath(s.home,s.thread)};return rolloutPath(n.codexHome(),id) }
func (n *Node) codexTitle(id string) string { if s,ok:=n.codexSource(id);ok{return threadTitle(s.home,s.thread)};return threadTitle(n.codexHome(),id) }

func cacheFor(home string) *rolloutCache {
	c := rolloutCaches.byHome[home]
	if c == nil {
		c = &rolloutCache{cwds: map[string]string{}}
		rolloutCaches.byHome[home] = c
	}
	return c
}

// rollouts lists a home's rollouts written in the last two weeks, newest first; cached for 5s. A rollout lives in the
// directory of the day its thread started (<home>/sessions/YYYY/MM/DD) and grows there for as long as the thread is
// resumed: one started six weeks earlier and in use every day was not found at all (2026-10-31), so the older days
// are listed too, once a minute, and kept when written lately.
func rollouts(home string) []codexRollout {
	rolloutCaches.Lock()
	defer rolloutCaches.Unlock()
	c := cacheFor(home)
	now := time.Now()
	if now.Sub(c.at) < 5*time.Second {
		return c.rollouts
	}
	var paths []string
	recent := map[string]bool{}
	for d := 0; d <= int(codexWindow/(24*time.Hour)); d++ {
		day := now.AddDate(0, 0, -d)
		dir := filepath.Join(home, "sessions", day.Format("2006"), day.Format("01"), day.Format("02"))
		recent[dir] = true
		files, _ := filepath.Glob(filepath.Join(dir, "rollout-*.jsonl"))
		paths = append(paths, files...)
	}
	if now.Sub(c.olderAt) > time.Minute {
		all, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*.jsonl"))
		archived, _ := filepath.Glob(filepath.Join(home,"archived_sessions","rollout-*.jsonl"))
		all = append(all, archived...)
		c.older = c.older[:0]
		for _, p := range all {
			if !recent[filepath.Dir(p)] {
				c.older = append(c.older, p)
			}
		}
		c.olderAt = now
	}
	firstOlder := len(paths)
	paths = append(paths, c.older...)
	var out []codexRollout
	for i, p := range paths {
		m := rolloutIDRe.FindStringSubmatch(p)
		st, err := os.Stat(p)
		if m == nil || err != nil || (i >= firstOlder && now.Sub(st.ModTime()) > codexWindow) {
			continue
		}
		cwd, ok := c.cwds[p]
		if !ok {
			cwd = rolloutCwd(p)
			c.cwds[p] = cwd
		}
		out = append(out, codexRollout{id: m[1], path: p, cwd: cwd, mtime: st.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mtime.After(out[j].mtime) })
	c.rollouts, c.at = out, now
	return out
}

// rolloutCwd reads the working directory from a rollout's first line (session_meta, tens of KB of instructions).
func rolloutCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 256<<10)
	line, _ := r.ReadBytes('\n')
	var l struct {
		Type    string `json:"type"`
		Payload struct {
			Cwd string `json:"cwd"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &l) != nil || l.Type != "session_meta" {
		return ""
	}
	return l.Payload.Cwd
}

// rolloutPath finds a session's rollout under a home by id.
func rolloutPath(home, id string) string {
	for _, r := range rollouts(home) {
		if r.id == id {
			return r.path
		}
	}
	if !validID(id) {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*-"+id+".jsonl"))
	if len(matches) == 0 { matches,_ = filepath.Glob(filepath.Join(home,"archived_sessions","rollout-*-"+id+".jsonl")) }
	if len(matches) == 0 {
		return ""
	}
	return newestFirst(matches)[0]
}

// codexSession picks the rollout of a Codex pane herdr could not identify: the newest in its directory not taken.
func (n *Node) codexSession(a host.Agent, taken map[string]host.Agent) string {
	cwd := a.ForegroundCwd
	if cwd == "" {
		cwd = a.Cwd
	}
	home := n.codexPaneHome(a.PaneID)
	for _, r := range rollouts(home) {
		id := n.codexPublicID(home,r.id)
		if _, used := taken[id]; !used && r.cwd != "" && (r.cwd == cwd || r.cwd == a.Cwd) {
			return id
		}
	}
	return ""
}

// codexByTitle finds the thread a Codex pane shows by the name in its title ("<name> | <directory>"; either part may
// be cut short with "..."). Of several threads with that name it keeps prefer (what herdr or a start said), else takes
// the most recently written one not taken. "" when the title carries no name (a new thread) or no thread has it.
func (n *Node) codexByTitle(a host.Agent, prefer string, taken map[string]host.Agent) string {
	i := strings.LastIndex(a.Title, " | ")
	if i <= 0 {
		return ""
	}
	name := strings.TrimSpace(a.Title[:i])
	cut := strings.TrimSuffix(strings.TrimSuffix(name, "..."), "…")
	home := n.codexPaneHome(a.PaneID)
	var best string
	var bestAt time.Time
	for _, id := range threadsNamed(home, func(t string) bool { return t == name || (cut != name && strings.HasPrefix(t, cut)) }) {
		if _, used := taken[n.codexPublicID(home,id)]; used {
			continue
		}
		if id == prefer {
			return id
		}
		p := rolloutPath(home, id)
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && (best == "" || st.ModTime().After(bestAt)) {
			best, bestAt = id, st.ModTime()
		}
	}
	return best
}

// threadTitle is the name the agent gave the thread (<home>/session_index.jsonl, last entry wins); re-read on change.
func threadTitle(home, id string) string {
	rolloutCaches.Lock()
	defer rolloutCaches.Unlock()
	return threadTitles(home)[id]
}

// threadsNamed lists the threads whose name passes match.
func threadsNamed(home string, match func(string) bool) []string {
	rolloutCaches.Lock()
	defer rolloutCaches.Unlock()
	var out []string
	for id, t := range threadTitles(home) {
		if match(t) {
			out = append(out, id)
		}
	}
	return out
}

// threadTitles reads a home's session_index.jsonl when it changed; the caller holds rolloutCaches.
func threadTitles(home string) map[string]string {
	path := filepath.Join(home, "session_index.jsonl")
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	c := cacheFor(home)
	if c.titles == nil || st.ModTime().After(c.titlesAt) {
		titles := map[string]string{}
		if f, err := os.Open(path); err == nil {
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 64<<10), 1<<20)
			for sc.Scan() {
				var e struct {
					ID   string `json:"id"`
					Name string `json:"thread_name"`
				}
				if json.Unmarshal(sc.Bytes(), &e) == nil && e.ID != "" && strings.TrimSpace(e.Name) != "" {
					titles[e.ID] = strings.TrimSpace(e.Name)
				}
			}
			f.Close()
		}
		c.titles, c.titlesAt = titles, st.ModTime()
	}
	return c.titles
}
