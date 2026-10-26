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
// ~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<session id>.jsonl (parsed by transcript/codex.go). herdr's codex
// integration reports the session id when Codex starts; a Codex started before that integration has none, and its
// rollout is found by working directory (the newest one there not already claimed).

const codexWindow = 14 * 24 * time.Hour

type codexRollout struct {
	id    string
	path  string
	cwd   string
	mtime time.Time
}

var codexCache struct {
	sync.Mutex
	at       time.Time
	rollouts []codexRollout
	cwds     map[string]string // path → cwd (never changes once written)
	titles   map[string]string
	titlesAt time.Time
}

var rolloutIDRe = regexp.MustCompile(`([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\.jsonl$`)

func (n *Node) codexDir() string { return filepath.Join(n.home, ".codex", "sessions") }

// codexRollouts lists the rollouts of the last two weeks, newest first; the scan is cached for 5s.
func (n *Node) codexRollouts() []codexRollout {
	codexCache.Lock()
	defer codexCache.Unlock()
	if time.Since(codexCache.at) < 5*time.Second {
		return codexCache.rollouts
	}
	if codexCache.cwds == nil {
		codexCache.cwds = map[string]string{}
	}
	var out []codexRollout
	for d := 0; d <= int(codexWindow/(24*time.Hour)); d++ {
		day := time.Now().AddDate(0, 0, -d)
		files, _ := filepath.Glob(filepath.Join(n.codexDir(), day.Format("2006"), day.Format("01"), day.Format("02"), "rollout-*.jsonl"))
		for _, p := range files {
			m := rolloutIDRe.FindStringSubmatch(p)
			st, err := os.Stat(p)
			if m == nil || err != nil {
				continue
			}
			cwd, ok := codexCache.cwds[p]
			if !ok {
				cwd = rolloutCwd(p)
				codexCache.cwds[p] = cwd
			}
			out = append(out, codexRollout{id: m[1], path: p, cwd: cwd, mtime: st.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mtime.After(out[j].mtime) })
	codexCache.rollouts, codexCache.at = out, time.Now()
	return out
}

// rolloutCwd reads the working directory from a rollout's first line (session_meta, ~20 KB of instructions).
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

// codexPath finds a Codex session's rollout by id.
func (n *Node) codexPath(id string) string {
	for _, r := range n.codexRollouts() {
		if r.id == id {
			return r.path
		}
	}
	if !validID(id) {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(n.codexDir(), "*", "*", "*", "rollout-*-"+id+".jsonl"))
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

// codexTitle is the name Codex gave the thread (~/.codex/session_index.jsonl, last entry wins); re-read on change.
func (n *Node) codexTitle(id string) string {
	path := filepath.Join(n.home, ".codex", "session_index.jsonl")
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	codexCache.Lock()
	defer codexCache.Unlock()
	if codexCache.titles == nil || st.ModTime().After(codexCache.titlesAt) {
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
		codexCache.titles, codexCache.titlesAt = titles, st.ModTime()
	}
	return codexCache.titles[id]
}
