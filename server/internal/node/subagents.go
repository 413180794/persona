package node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
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
		jp := filepath.Join(dir, "agent-"+id+".jsonl")
		st, err := os.Stat(jp)
		if err != nil {
			continue
		}
		// Past a day the file has not moved for 20 minutes either, so it is not running whatever its meta says:
		// left out before the meta is read.
		if time.Since(st.ModTime()) > 24*time.Hour {
			continue
		}
		m, ok := subagentMetaCached(mp)
		if !ok {
			continue
		}
		// A subagent whose file has not moved for 20 minutes is not running any more (its claude went away).
		running := !m.Finished && time.Since(st.ModTime()) < 20*time.Minute
		edges := subagentEdgesCached(jp, st, false)
		out = append(out, proto.Subagent{
			ID: id, SessionID: parent + subagentSep + id, Type: m.AgentType, Description: m.Description,
			ToolUseID: m.ToolUseID, ParentID: m.ParentID, Fork: m.Fork, Running: running, Background: m.RequestShape == "background",
			StartedAt: edges.started, UpdatedAt: st.ModTime(), LastText: edges.last,
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

// metaCache remembers parsed agent-*.meta.json files by size and mtime: the session list asks for every live
// session's subagents each second, and their metas rarely change (once when the subagent finishes).
// Bounded like workspaceDirs: a long-lived node that has seen more metas than that starts over.
var metaCache = struct {
	sync.Mutex
	m map[string]metaMemo
}{m: map[string]metaMemo{}}

const metaCacheMax = 4096

type metaMemo struct {
	size  int64
	mtime time.Time
	meta  subagentMeta
	ok    bool // false: the file did not parse
}

func subagentMetaCached(path string) (subagentMeta, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return subagentMeta{}, false
	}
	metaCache.Lock()
	e, hit := metaCache.m[path]
	metaCache.Unlock()
	if hit && e.size == st.Size() && e.mtime.Equal(st.ModTime()) {
		return e.meta, e.ok
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return subagentMeta{}, false
	}
	var m subagentMeta
	ok := json.Unmarshal(b, &m) == nil
	metaCache.Lock()
	if len(metaCache.m) >= metaCacheMax {
		metaCache.m = map[string]metaMemo{}
	}
	metaCache.m[path] = metaMemo{size: st.Size(), mtime: st.ModTime(), meta: m, ok: ok}
	metaCache.Unlock()
	return m, ok
}

// edgeCache remembers transcript metadata per file. Counts need the whole transcript, so the first read streams
// it once and later reads only appended complete lines, at most every metaMinInterval. Keeping the latest values
// also avoids losing them when a large tool result pushes the last reply beyond the former 96 KB tail window.
var edgeCache = struct {
	sync.Mutex
	m map[string]edgeMemo
}{m: map[string]edgeMemo{}}

type edgeMemo struct {
	size   int64
	mtime  time.Time
	readAt time.Time
	info   os.FileInfo
	off    int64
	head   []byte // detects a rewrite even if the new file is larger
	edges  subagentEdge
}

type subagentEdge struct {
	started, lastAt time.Time
	last, model     string
	latestTool      *proto.WorkflowTool
	tokens, calls   int
	tools           map[string]bool
}

func subagentEdgesCached(path string, st os.FileInfo, fresh bool) subagentEdge {
	edgeCache.Lock()
	e, ok := edgeCache.m[path]
	edgeCache.Unlock()
	if ok && (e.size == st.Size() && e.mtime.Equal(st.ModTime()) || !fresh && time.Since(e.readAt) < metaMinInterval) {
		return e.edges
	}
	readAt := time.Now()
	f, err := os.Open(path)
	if err != nil {
		return e.edges
	}
	defer f.Close()
	head := make([]byte, min(st.Size(), 256))
	n, _ := f.ReadAt(head, 0)
	head = head[:n]
	if !ok || !os.SameFile(e.info, st) || st.Size() <= e.size || !bytes.HasPrefix(head, e.head) {
		e = edgeMemo{}
	}
	e.edges.tools = maps.Clone(e.edges.tools) // cached snapshots may still be in use by another list request
	e.off += eachLine(io.NewSectionReader(f, e.off, st.Size()-e.off), e.edges.readLine)
	// Only remember already consumed bytes: a partial first line can still be changing in place.
	e.head = append([]byte(nil), head[:min(int64(len(head)), e.off)]...)
	e.info, e.size, e.mtime, e.readAt = st, st.Size(), st.ModTime(), readAt
	edgeCache.Lock()
	if len(edgeCache.m) >= metaCacheMax {
		edgeCache.m = map[string]edgeMemo{}
	}
	edgeCache.m[path] = e
	edgeCache.Unlock()
	return e.edges
}

func (e *subagentEdge) readLine(line []byte) {
	var row struct {
		Type      string    `json:"type"`
		Timestamp time.Time `json:"timestamp"`
		Message   *struct {
			Usage *struct {
				Input      int `json:"input_tokens"`
				CacheRead  int `json:"cache_read_input_tokens"`
				CacheWrite int `json:"cache_creation_input_tokens"`
				Output     int `json:"output_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &row) != nil {
		return
	}
	if e.started.IsZero() {
		e.started = row.Timestamp
	}
	if (row.Type == "user" || row.Type == "assistant") && !row.Timestamp.IsZero() {
		e.lastAt = row.Timestamp
	}
	if row.Type != "assistant" || row.Message == nil {
		return
	}
	if u := row.Message.Usage; u != nil {
		// Usage repeats on every content block of one reply; the subagent page shows the latest, never a sum.
		e.tokens = u.Input + u.CacheRead + u.CacheWrite + u.Output
	}
	meta := transcript.Meta{Sidechain: true}
	for _, it := range transcript.ParseLine(0, line, &meta) {
		if it.Kind == proto.KindToolUse {
			if it.ToolUseID == "" || !e.tools[it.ToolUseID] {
				e.calls++
				if it.ToolUseID != "" {
					if e.tools == nil {
						e.tools = map[string]bool{}
					}
					e.tools[it.ToolUseID] = true
				}
			}
			e.latestTool = &proto.WorkflowTool{Name: it.ToolName, Summary: transcript.Activity(it.ToolName, it.ToolInput)}
		}
	}
	if meta.LastText != "" {
		e.last = snippet(meta.LastText)
	}
	if meta.Model != "" {
		e.model = meta.Model
	}
}
