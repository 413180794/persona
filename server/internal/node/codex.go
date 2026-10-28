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

	"github.com/chyroc/jarvis/server/internal/herdr"
)

// Codex support: herdr reports Codex panes as agent "codex"; their conversations are rollouts under
// ~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<session id>.jsonl (parsed by transcript/codex.go). TRAEX, a Codex
// fork, keeps the same layout under ~/.trae/cli (traex.go). herdr's codex
// integration reports the session id when Codex starts; a Codex started before that integration has none, and its
// rollout is found by working directory (the newest one there not already claimed).

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
func (n *Node) codexPath(id string) string    { return rolloutPath(n.codexHome(), id) }
func (n *Node) codexTitle(id string) string   { return threadTitle(n.codexHome(), id) }

func cacheFor(home string) *rolloutCache {
	c := rolloutCaches.byHome[home]
	if c == nil {
		c = &rolloutCache{cwds: map[string]string{}}
		rolloutCaches.byHome[home] = c
	}
	return c
}

// rollouts lists a home's rollouts of the last two weeks (<home>/sessions/YYYY/MM/DD), newest first; cached for 5s.
func rollouts(home string) []codexRollout {
	rolloutCaches.Lock()
	defer rolloutCaches.Unlock()
	c := cacheFor(home)
	if time.Since(c.at) < 5*time.Second {
		return c.rollouts
	}
	var out []codexRollout
	for d := 0; d <= int(codexWindow/(24*time.Hour)); d++ {
		day := time.Now().AddDate(0, 0, -d)
		files, _ := filepath.Glob(filepath.Join(home, "sessions", day.Format("2006"), day.Format("01"), day.Format("02"), "rollout-*.jsonl"))
		for _, p := range files {
			m := rolloutIDRe.FindStringSubmatch(p)
			st, err := os.Stat(p)
			if m == nil || err != nil {
				continue
			}
			cwd, ok := c.cwds[p]
			if !ok {
				cwd = rolloutCwd(p)
				c.cwds[p] = cwd
			}
			out = append(out, codexRollout{id: m[1], path: p, cwd: cwd, mtime: st.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mtime.After(out[j].mtime) })
	c.rollouts, c.at = out, time.Now()
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
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// codexSession picks the rollout of a Codex pane herdr could not identify: the newest in its directory not taken.
func (n *Node) codexSession(a herdr.Agent, taken map[string]herdr.Agent) string {
	cwd := a.ForegroundCwd
	if cwd == "" {
		cwd = a.Cwd
	}
	for _, r := range n.codexRollouts() {
		if _, used := taken[r.id]; !used && r.cwd != "" && (r.cwd == cwd || r.cwd == a.Cwd) {
			return r.id
		}
	}
	return ""
}

// threadTitle is the name the agent gave the thread (<home>/session_index.jsonl, last entry wins); re-read on change.
func threadTitle(home, id string) string {
	path := filepath.Join(home, "session_index.jsonl")
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	rolloutCaches.Lock()
	defer rolloutCaches.Unlock()
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
	return c.titles[id]
}
