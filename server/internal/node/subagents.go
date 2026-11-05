package node

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

// Subagents: Claude Code keeps each agent a session launches in <transcript dir>/<session>/subagents/
// agent-<id>.jsonl (all lines isSidechain) with agent-<id>.meta.json beside it ({agentType, description, toolUseId,
// requestShape, finished, parentAgentId, isFork}). The phone reads one like a session whose id is "<session>@<agent id>".
// A subagent's own subagents are in the same directory, naming their launcher in parentAgentId (absent: the session).

const subagentSep = "@"

func splitSubagent(id string) (parent, agent string, ok bool) {
	parent, agent, ok = strings.Cut(id, subagentSep)
	return parent, agent, ok && validID(parent) && agent != "" && !strings.ContainsAny(agent, "/.\\")
}

func (n *Node) subagentDir(parent string) string {
	p := n.transcriptPath(parent)
	if p == "" {
		return ""
	}
	return filepath.Join(strings.TrimSuffix(p, ".jsonl"), "subagents")
}

// subagentFile opens "<parent>@<agent>" as a transcript.
func (n *Node) subagentFile(id string) (*transcript.File, error) {
	parent, agent, ok := splitSubagent(id)
	if !ok {
		return nil, fmt.Errorf("bad subagent id %q", id)
	}
	dir := n.subagentDir(parent)
	if dir == "" {
		return nil, fmt.Errorf("transcript for session %s not found", parent)
	}
	p := filepath.Join(dir, "agent-"+agent+".jsonl")
	if _, err := os.Stat(p); err != nil {
		if p = workflowAgentFile(dir, agent); p == "" { // one of a workflow run's agents (workflows.go)
			return nil, fmt.Errorf("subagent %s not found", agent)
		}
	}
	return transcript.NewSubagentFile(p), nil
}

type subagentMeta struct {
	AgentType    string `json:"agentType"`
	Description  string `json:"description"`
	ToolUseID    string `json:"toolUseId"`
	RequestShape string `json:"requestShape"`
	Finished     bool   `json:"finished"`
	ParentID     string `json:"parentAgentId"`
	Fork         bool   `json:"isFork"`
}

// subagents lists a session's subagents, newest first; finished ones older than a day are left out (each on its own:
// a nested one stays listed when its parent is left out).
func (n *Node) subagents(parent string) []proto.Subagent {
	dir := n.subagentDir(parent)
	if dir == "" {
		return nil
	}
	metas, _ := filepath.Glob(filepath.Join(dir, "agent-*.meta.json"))
	var out []proto.Subagent
	for _, mp := range metas {
		id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(mp), "agent-"), ".meta.json")
		b, err := os.ReadFile(mp)
		if err != nil {
			continue
		}
		var m subagentMeta
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		jp := filepath.Join(dir, "agent-"+id+".jsonl")
		st, err := os.Stat(jp)
		if err != nil {
			continue
		}
		// A subagent whose file has not moved for 20 minutes is not running any more (its claude went away).
		running := !m.Finished && time.Since(st.ModTime()) < 20*time.Minute
		if !running && time.Since(st.ModTime()) > 24*time.Hour {
			continue
		}
		started, last := subagentEdgesCached(jp, st)
		out = append(out, proto.Subagent{
			ID: id, SessionID: parent + subagentSep + id, Type: m.AgentType, Description: m.Description,
			ToolUseID: m.ToolUseID, ParentID: m.ParentID, Fork: m.Fork, Running: running, Background: m.RequestShape == "background",
			StartedAt: started, UpdatedAt: st.ModTime(), LastText: snippet(last),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Running != out[j].Running {
			return out[i].Running
		}
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

// edgeCache remembers subagentEdges per file. The session list is rebuilt every second and lists each live
// session's subagents of the last day: re-reading the head and a 96 KB tail of every one each time kept a node
// with a few subagent-heavy sessions reading ~20 MB/s. A growing file is re-read at most every metaMinInterval.
var edgeCache = struct {
	sync.Mutex
	m map[string]edgeMemo
}{m: map[string]edgeMemo{}}

type edgeMemo struct {
	size    int64
	mtime   time.Time
	readAt  time.Time
	started time.Time
	last    string
}

func subagentEdgesCached(path string, st os.FileInfo) (time.Time, string) {
	edgeCache.Lock()
	e, ok := edgeCache.m[path]
	edgeCache.Unlock()
	if ok && (e.size == st.Size() && e.mtime.Equal(st.ModTime()) || time.Since(e.readAt) < metaMinInterval) {
		return e.started, e.last
	}
	started, last := subagentEdges(path)
	edgeCache.Lock()
	edgeCache.m[path] = edgeMemo{size: st.Size(), mtime: st.ModTime(), readAt: time.Now(), started: started, last: last}
	edgeCache.Unlock()
	return started, last
}

// subagentEdges reads the first timestamp and the latest assistant text of a subagent transcript.
func subagentEdges(path string) (time.Time, string) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, ""
	}
	defer f.Close()
	var started time.Time
	r := bufio.NewReaderSize(f, 64<<10)
	if line, err := r.ReadBytes('\n'); err == nil || len(line) > 0 {
		var e struct {
			Timestamp time.Time `json:"timestamp"`
		}
		_ = json.Unmarshal(line, &e)
		started = e.Timestamp
	}
	st, _ := f.Stat()
	const tail = 96 << 10
	off := max(st.Size()-tail, 0)
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && len(buf) == 0 {
		return started, ""
	}
	meta := transcript.Meta{Sidechain: true}
	for _, line := range strings.Split(string(buf), "\n") {
		if strings.Contains(line, `"assistant"`) {
			transcript.ParseLine(0, []byte(line), &meta)
		}
	}
	return started, meta.LastText
}
